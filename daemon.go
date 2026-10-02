// daemon.go - 데몬: queue 수거·이름 해석·경로 판별·ping 감시·준비확인·7분/3분 스케줄·run 직렬 실행·재기동 복원
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxFails       = 3  // 같은 호스트 연속 실패 횟수 → 제외
	lookupParallel = 32 // net.LookupHost 동시 실행 상한
)

// 실제 구현체 훅 (테스트는 Daemon 필드에 가짜를 직접 주입)
var (
	newPinger   = func() Pinger { return NewRealPinger() }
	newChecker  = func() Checker { return NewRealChecker() }
	newRunner   = func() Runner { return NewRealRunner() }
	newNotifier = func() Notifier { return wallNotifier{} }
)

type Daemon struct {
	Clock    Clock
	Pinger   Pinger
	Checker  Checker
	Runner   Runner
	Notifier Notifier
	Lookup   func(host string) ([]string, error) // nil 이면 net.LookupHost

	jobs      map[string]*Job
	rt        map[string]*hostRT // 키=호스트 (진행 중 job 사이에서 유일), 저장하지 않는 런타임 상태
	dirty     map[string]bool    // 저장이 필요한 job id
	lastPing  time.Time
	lastCheck time.Time
	cur       *inflight // 실행 중인 run (동시에 1개)
	doneCh    chan runDone
	runRetry  time.Time // Runner 오류 후 재시도 가능 시각
	runErr    string    // 마지막 Runner 오류 (같은 오류 반복 로그 방지)
	checkErr  map[string]string
}

type hostRT struct {
	up            bool // 마지막 ping 응답
	startCheck    bool // 기동 직후 1회 준비확인
	resolveLogged bool
}

type inflight struct {
	jobID string
	user  string
	hosts []string        // Runner 에 넘긴 목록
	ready map[string]bool // run 시작 시 READY 였던 미처리 호스트
	first bool
	at    int64
}

type runDone struct {
	res RunResult
	err error
}

func newDefaultDaemon() *Daemon {
	return &Daemon{
		Clock:    realClock{},
		Pinger:   newPinger(),
		Checker:  newChecker(),
		Runner:   newRunner(),
		Notifier: newNotifier(),
	}
}

func runDaemon() int {
	if err := ensureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, "[X] 디렉터리 생성 실패:", err)
		return 1
	}
	lock, ok, err := tryLock()
	if err != nil {
		logf("[X] daemon: lock 실패: %v", err)
		return 1
	}
	if !ok {
		return 0 // 이미 다른 데몬이 실행 중
	}
	defer lock.Close() // 종료까지 유지
	_ = os.Setenv("PATH", withStdPath(os.Getenv("PATH")))

	logf("daemon 시작 pid=%d", os.Getpid())
	if os_check_sh == "" {
		logf("[X] os_check_sh 가 비어 있습니다 (run 단계에서 작업 대기)")
	}

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		close(stop)
	}()

	newDefaultDaemon().loop(stop)
	logf("daemon 종료 pid=%d", os.Getpid())
	return 0
}

func (d *Daemon) loop(stop <-chan struct{}) {
	d.onStart()
	for {
		d.step(d.Clock.Now())
		select {
		case <-stop:
			return
		case <-time.After(queuePoll):
		}
	}
}

func (d *Daemon) init() {
	if d.jobs != nil {
		return
	}
	d.jobs = map[string]*Job{}
	d.rt = map[string]*hostRT{}
	d.dirty = map[string]bool{}
	d.doneCh = make(chan runDone, 1)
	d.checkErr = map[string]string{}
}

func (d *Daemon) hrt(name string) *hostRT {
	r := d.rt[name]
	if r == nil {
		r = &hostRT{}
		d.rt[name] = r
	}
	return r
}

func active(h *Host) bool { return h.Processed == "" && h.Fails < maxFails }

func jobFile(id string) string { return filepath.Join(jobsDir(), id+".json") }

func secs(t time.Duration) int64 { return int64(t / time.Second) }

// onStart: jobs/*.json 복원, 미처리 호스트에 "기동 직후 1회 준비확인" 플래그
func (d *Daemon) onStart() {
	d.init()
	jobs, err := loadJobs()
	if err != nil {
		logf("[X] jobs 복원 실패: %v", err)
		return
	}
	for _, j := range jobs { // 전달시각 순 → 중복 호스트는 나중 job 이 가져감
		d.addJob(j, false)
		for name, h := range j.Hosts {
			if active(h) {
				d.hrt(name).startCheck = true
			}
		}
	}
	if len(jobs) > 0 {
		logf("복원: job %d건", len(jobs))
	}
}

// addJob: 메모리에 등록. 같은 호스트가 다른 진행 중 job 에 있으면 이전 job 에서 제거.
func (d *Daemon) addJob(j *Job, fresh bool) {
	d.init()
	moved := map[string]int{}
	for name := range j.Hosts {
		for _, o := range d.jobs {
			if o.ID == j.ID {
				continue
			}
			if _, ok := o.Hosts[name]; ok {
				delete(o.Hosts, name)
				d.dirty[o.ID] = true
				moved[o.ID]++
			}
		}
		if fresh {
			delete(d.rt, name)
		}
	}
	for id, n := range moved {
		logf("중복 전달: job %s 에서 %d대 제거 → job %s", id, n, j.ID)
	}
	d.jobs[j.ID] = j
}

func (d *Daemon) removeJob(id string) {
	if j := d.jobs[id]; j != nil {
		for name := range j.Hosts {
			delete(d.rt, name)
		}
	}
	delete(d.jobs, id)
	delete(d.dirty, id)
}

func (d *Daemon) sortedJobs() []*Job {
	js := make([]*Job, 0, len(d.jobs))
	for _, j := range d.jobs {
		js = append(js, j)
	}
	sort.Slice(js, func(a, b int) bool {
		if js[a].Submitted != js[b].Submitted {
			return js[a].Submitted < js[b].Submitted
		}
		return js[a].ID < js[b].ID
	})
	return js
}

// step: 한 주기 (loop 는 queuePoll 마다 호출). ping/준비확인은 각자 주기가 됐을 때만 수행.
func (d *Daemon) step(now time.Time) {
	d.init()
	select {
	case r := <-d.doneCh:
		d.finishRun(r, now)
	default:
	}
	if n, err := d.collectQueue(); err != nil {
		logf("[X] queue 수거 실패: %v", err)
	} else if n > 0 {
		logf("queue 수거: %d건", n)
	}
	d.dropCancelled()
	if now.Sub(d.lastPing) >= pingInterval {
		d.lastPing = now
		d.resolve()
		d.ping()
	}
	if now.Sub(d.lastCheck) >= checkInterval {
		d.lastCheck = now
		d.check(now)
	}
	d.schedule(now)
	d.closeFinished()
	d.saveDirty()
}

// dropCancelled: jobs/<id>.json 이 없어진(cancel) job 을 메모리에서 제거
func (d *Daemon) dropCancelled() {
	for id := range d.jobs {
		if _, err := os.Stat(jobFile(id)); os.IsNotExist(err) {
			d.removeJob(id)
			logf("job 제거(cancel): %s", id)
		}
	}
}

// resolve: IP 미해결 호스트 net.LookupHost (동시 32), 실패는 다음 주기 재시도
func (d *Daemon) resolve() {
	var names []string
	var hosts []*Host
	var ids []string
	for _, j := range d.jobs {
		for name, h := range j.Hosts {
			if h.IP == "" && active(h) {
				names = append(names, name)
				hosts = append(hosts, h)
				ids = append(ids, j.ID)
			}
		}
	}
	if len(names) == 0 {
		return
	}
	lookup := d.Lookup
	if lookup == nil {
		lookup = net.LookupHost
	}
	ips := make([]string, len(names))
	errs := make([]error, len(names))
	sem := make(chan struct{}, lookupParallel)
	var wg sync.WaitGroup
	for i := range names {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			addrs, err := lookup(names[i])
			ips[i], errs[i] = pickIP(addrs), err
		}(i)
	}
	wg.Wait()
	for i, name := range names {
		if ips[i] == "" {
			if r := d.hrt(name); !r.resolveLogged {
				r.resolveLogged = true
				logf("[!] 이름 해석 실패: %s (%v) - 다음 주기 재시도", name, errs[i])
			}
			continue
		}
		hosts[i].IP = ips[i]
		d.dirty[ids[i]] = true
	}
}

// pickIP: IPv4 우선, 없으면 첫 주소
func pickIP(addrs []string) string {
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
			return a
		}
	}
	if len(addrs) > 0 {
		return addrs[0]
	}
	return ""
}

// ping: 감시 1주기. 경로 미판별 호스트는 로컬 ICMP 로 판별(무응답 → os6).
func (d *Daemon) ping() {
	type ref struct {
		j     *Job
		h     *Host
		probe bool
	}
	refs := map[string]ref{}
	var targets []PingTarget
	for _, j := range d.jobs {
		for name, h := range j.Hosts {
			if !active(h) || h.IP == "" {
				continue
			}
			route, probe := h.Route, h.Route == ""
			if probe {
				route = "local"
			}
			targets = append(targets, PingTarget{Host: name, IP: h.IP, Route: route})
			refs[name] = ref{j, h, probe}
		}
	}
	if len(targets) == 0 {
		return
	}
	sort.Slice(targets, func(a, b int) bool { return targets[a].Host < targets[b].Host })
	res := d.Pinger.Ping(targets)
	nLocal, nOS6 := 0, 0
	for _, t := range targets {
		r := refs[t.Host]
		pr, ok := res[t.Host]
		if !ok || !pr.Known {
			continue // 정보 없음 → 상태 불변
		}
		h, name := r.h, t.Host
		if r.probe {
			d.dirty[r.j.ID] = true
			if !pr.Up && os6_mgmt != "" {
				h.Route = "os6"
				nOS6++
				continue // 로컬 무응답은 os6 경로의 ping 상태가 아님
			}
			h.Route = "local"
			nLocal++
		}
		rt := d.hrt(name)
		if pr.Up {
			rt.up = true
			if h.Miss != 0 {
				h.Miss = 0
				d.dirty[r.j.ID] = true
			}
			continue
		}
		rt.up = false
		if h.Miss < downMisses {
			h.Miss++
			d.dirty[r.j.ID] = true
		}
		if h.Miss >= downMisses {
			if !h.SeenDown {
				h.SeenDown = true
				d.dirty[r.j.ID] = true
				logf("ping X: %s (job %s, 설치 시작으로 간주)", name, r.j.ID)
			}
			if h.ReadyAt != 0 {
				h.ReadyAt = 0
				d.dirty[r.j.ID] = true
				logf("READY 해제(재부팅): %s (job %s)", name, r.j.ID)
			}
		}
	}
	if nLocal+nOS6 > 0 {
		logf("경로 판별: local %d대, os6 %d대", nLocal, nOS6)
	}
}

// check: 준비확인 1주기. 후보 = 미처리 && ping up && (seen_down || 기동 직후 1회), route 별로 묶어 호출.
func (d *Daemon) check(now time.Time) {
	owner := map[string]*Job{}
	groups := map[string][]string{}
	for _, j := range d.jobs {
		for name, h := range j.Hosts {
			r := d.rt[name]
			if !active(h) || h.ReadyAt != 0 || h.Route == "" || r == nil || !r.up {
				continue
			}
			if !h.SeenDown && !r.startCheck {
				continue
			}
			owner[name] = j
			groups[h.Route] = append(groups[h.Route], name)
		}
	}
	routes := make([]string, 0, len(groups))
	for route := range groups {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		hosts := groups[route]
		sort.Strings(hosts)
		res, err := d.Checker.Check(route, hosts)
		if err != nil {
			if msg := err.Error(); msg != d.checkErr[route] {
				d.checkErr[route] = msg
				logf("[X] 준비확인 실패(%s): %v", route, err)
			}
			continue // 기동 직후 플래그 유지 → 다음 주기 재시도
		}
		d.checkErr[route] = ""
		for _, name := range hosts {
			d.hrt(name).startCheck = false
			cr, ok := res[name]
			j := owner[name]
			if !ok || !cr.Responded || cr.Anaconda || cr.Uptime >= float64(now.Unix()-j.Submitted) {
				continue
			}
			j.Hosts[name].ReadyAt = now.Unix()
			d.dirty[j.ID] = true
			logf("READY: %s (job %s, uptime %.0fs)", name, j.ID, cr.Uptime)
			if !j.FirstRunDone {
				if j.FirstReady == 0 {
					j.FirstReady = now.Unix()
				}
			} else if j.LateFirstReady == 0 && (d.cur == nil || d.cur.jobID != j.ID) {
				j.LateFirstReady = now.Unix()
			}
		}
	}
}

// schedule: run 이 없을 때만 다음 run 을 결정 (직렬). 1차: 전부 READY 또는 첫 READY+bootWait,
// 이후: 첫 늦은 READY+lateWait 에 새로 READY 된 미처리 호스트만.
func (d *Daemon) schedule(now time.Time) {
	if d.cur != nil || now.Before(d.runRetry) {
		return
	}
	t := now.Unix()
	for _, j := range d.sortedJobs() {
		pending := 0
		var ready []string
		for name, h := range j.Hosts {
			if !active(h) {
				continue
			}
			pending++
			if h.ReadyAt > 0 {
				ready = append(ready, name)
			}
		}
		if len(ready) == 0 {
			continue
		}
		sort.Strings(ready)
		if !j.FirstRunDone {
			if j.FirstReady == 0 {
				j.FirstReady = t
				d.dirty[j.ID] = true
			}
			if len(ready) < pending && t < j.FirstReady+secs(bootWait) {
				continue
			}
			all := make([]string, 0, len(j.Hosts))
			for name := range j.Hosts {
				all = append(all, name)
			}
			sort.Strings(all)
			d.startRun(j, all, ready, true, t)
			return
		}
		if j.LateFirstReady == 0 {
			j.LateFirstReady = t
			d.dirty[j.ID] = true
		}
		if t < j.LateFirstReady+secs(lateWait) {
			continue
		}
		d.startRun(j, ready, ready, false, t)
		return
	}
}

func (d *Daemon) startRun(j *Job, hosts, ready []string, first bool, t int64) {
	hasOS6 := false
	for _, name := range hosts {
		if h := j.Hosts[name]; h != nil && h.Route == "os6" {
			hasOS6 = true
		}
	}
	c := &inflight{jobID: j.ID, user: j.User, hosts: hosts, ready: map[string]bool{}, first: first, at: t}
	for _, name := range ready {
		c.ready[name] = true
	}
	d.cur = c
	kind := "늦은 묶음"
	if first {
		kind = "1차"
	}
	if d.runErr == "" { // Runner 오류 재시도(30초마다) 중에는 반복 로그 생략
		logf("run 시작: job %s %s %d대 (READY %d대)", j.ID, kind, len(hosts), len(ready))
	}
	snap := cloneJob(j)
	r, ch := d.Runner, d.doneCh
	go func() {
		res, err := r.Run(snap, hosts, hasOS6)
		ch <- runDone{res, err}
	}()
}

// cloneJob: Runner 고루틴에 넘길 사본 (데몬은 원본을 계속 수정)
func cloneJob(j *Job) *Job {
	b, _ := json.Marshal(j)
	c := &Job{}
	_ = json.Unmarshal(b, c)
	return c
}

// finishRun: run 결과 반영 → processed/fails, runs 기록, 다음 늦은 묶음 시각, wall
func (d *Daemon) finishRun(r runDone, now time.Time) {
	c := d.cur
	d.cur = nil
	if c == nil {
		return
	}
	if r.err != nil {
		if msg := r.err.Error(); msg != d.runErr {
			d.runErr = msg
			logf("[X] run 실패(job %s): %v - 작업 대기", c.jobID, r.err)
		}
		d.runRetry = now.Add(checkInterval)
		return
	}
	d.runErr = ""
	res := r.res
	var done []string
	if j := d.jobs[c.jobID]; j != nil {
		if !res.Abnormal {
			for _, name := range res.Processed {
				if h := j.Hosts[name]; h != nil && h.Processed == "" {
					h.Processed = res.Code
					h.Fails = 0
					done = append(done, name)
				}
			}
		}
		for name := range c.ready {
			if h := j.Hosts[name]; h != nil && h.Processed == "" {
				h.Fails++
				if h.Fails >= maxFails {
					logf("[X] %s: 연속 %d회 실패 → 제외 (job %s)", name, h.Fails, j.ID)
				}
			}
		}
		if c.first {
			j.FirstRunDone = true
		}
		j.Runs = append(j.Runs, Run{Code: res.Code, Hosts: c.hosts, At: c.at})
		// 다음 늦은 묶음: run 중 새로 READY 된 호스트는 그 READY 시각, 재시도 호스트는 지금부터
		var late int64
		for name, h := range j.Hosts {
			if !active(h) || h.ReadyAt == 0 {
				continue
			}
			t := h.ReadyAt
			if c.ready[name] {
				t = now.Unix()
			}
			if late == 0 || t < late {
				late = t
			}
		}
		j.LateFirstReady = late
		d.dirty[j.ID] = true
	} else if !res.Abnormal {
		done = append(done, res.Processed...) // run 중 cancel 된 job
	}
	sort.Strings(done)
	note := ""
	if res.Abnormal {
		note = " (os_check 비정상 종료)"
	}
	logf("run 완료: job %s code %s 처리 %d대%s", c.jobID, res.Code, len(done), note)
	d.Notifier.Wall(wallMessage(c.user, done, res.Code, res.Abnormal))
}

func wallMessage(user string, hosts []string, code string, abnormal bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[auto_setup] OS 설치 + 설정체크 완료 (user=%s)\n", user)
	if len(hosts) > 0 {
		b.WriteString(strings.Join(hosts, " ") + " ")
	}
	fmt.Fprintf(&b, "(%d대)\n", len(hosts))
	fmt.Fprintf(&b, "code : %s   →  auto_setup code %s\n", code, code)
	if abnormal {
		b.WriteString("[!] os_check 비정상 종료\n")
	}
	return b.String()
}

// closeFinished: 모든 호스트가 processed 또는 제외된 job → jobs/done
func (d *Daemon) closeFinished() {
	for id, j := range d.jobs {
		if d.cur != nil && d.cur.jobID == id {
			continue
		}
		nProc, nFail := 0, 0
		open := false
		for _, h := range j.Hosts {
			switch {
			case h.Processed != "":
				nProc++
			case h.Fails >= maxFails:
				nFail++
			default:
				open = true
			}
		}
		if open {
			continue
		}
		if _, err := os.Stat(jobFile(id)); err == nil {
			if _, err := saveJob(j); err != nil {
				logf("[X] job 저장 실패(%s): %v", id, err)
				continue
			}
			if err := archiveJob(id); err != nil {
				logf("[X] job 종료 실패(%s): %v", id, err)
				continue
			}
		}
		d.removeJob(id)
		logf("job 종료: %s (처리완료 %d, 실패 %d)", id, nProc, nFail)
	}
}

// saveDirty: 상태가 바뀐 job 만 저장. 파일이 없어졌으면(cancel) 되살리지 않고 제거.
func (d *Daemon) saveDirty() {
	for id := range d.dirty {
		j := d.jobs[id]
		if j == nil {
			delete(d.dirty, id)
			continue
		}
		if _, err := os.Stat(jobFile(id)); os.IsNotExist(err) {
			d.removeJob(id)
			logf("job 제거(cancel): %s", id)
			continue
		}
		if _, err := saveJob(j); err != nil {
			logf("[X] job 저장 실패(%s): %v", id, err)
			continue
		}
		delete(d.dirty, id)
	}
}

// collectQueue: queue/*.job → jobs/<jobid>.json 저장 후 .job 삭제, 메모리 등록(중복 전달 처리). 수거 건수 반환.
func (d *Daemon) collectQueue() (int, error) {
	d.init()
	ents, err := os.ReadDir(queueDir())
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range ents {
		m := jobFileRe.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		p := filepath.Join(queueDir(), e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		user, t, hosts, err := parseJobFile(b)
		if err != nil {
			logf("[X] queue 파일 무시(%s): %v", e.Name(), err)
			_ = os.Rename(p, p+".bad")
			continue
		}
		if t == 0 {
			t, _ = strconv.ParseInt(m[1], 10, 64)
		}
		if t == 0 {
			t = d.Clock.Now().Unix()
		}
		j := &Job{ID: newJobID(user, t), User: user, Submitted: t, Hosts: map[string]*Host{}, Runs: []Run{}}
		for _, h := range hosts {
			j.Hosts[h] = &Host{}
		}
		if _, err := saveJob(j); err != nil {
			return n, err
		}
		if err := os.Remove(p); err != nil {
			return n, err
		}
		logf("job 수거: %s (%d대)", j.ID, len(hosts))
		d.addJob(j, true)
		n++
	}
	return n, nil
}
