// daemon.go - 데몬: queue·requests·완료기록 수거, 이름 해석·경로 판별·ping 감시·준비확인, stage/정체, LDAP·2차 훅, 7분/3분 스케줄·run 직렬 실행·재기동 복원, pid 제어
package main

import (
	"bytes"
	"encoding/json"
	"errors"
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

// 실제 구현체 훅 (테스트는 Daemon 필드에 가짜를 직접 주입).
// newLdap/newSecond 는 구현 파일(ldapbk.go / second.go)의 init() 에서 교체한다.
var (
	newPinger   = func() Pinger { return NewRealPinger() }
	newChecker  = func() Checker { return NewRealChecker() }
	newRunner   = func() Runner { return NewRealRunner() }
	newNotifier = func() Notifier { return nopNotifier{} } // wall 알림은 끔 (되살리려면 wallNotifier{})
	newLdap     = func() LdapBackup { return nopLdap{} }
	newSecond   = func() Second { return nopSecond{} }
)

type Daemon struct {
	Clock    Clock
	Pinger   Pinger
	Checker  Checker
	Runner   Runner
	Notifier Notifier
	Lookup   func(host string) ([]string, error) // nil 이면 net.LookupHost
	Ldap     LdapBackup                          // nil·nopLdap → LDAP 단계 생략
	Second   Second                              // nil·nopSecond → 2차 체크 생략

	jobs       map[string]*Job
	rt         map[string]*hostRT // 키=호스트 (진행 중 job 사이에서 유일), 저장하지 않는 런타임 상태
	dirty      map[string]bool    // 저장이 필요한 job id
	lastPing   time.Time
	lastCheck  time.Time
	cur        *inflight // 실행 중인 run (동시에 1개)
	doneCh     chan runDone
	progCh     chan runProgress // run 고루틴 → 2차 체크 시작 알림
	runRetry   time.Time        // Runner 오류 후 재시도 가능 시각
	runErr     string           // 마지막 Runner 오류 (같은 오류 반복 로그 방지)
	checkErr   map[string]string
	forceCheck map[string]bool // r 키 수동 재시도: 정체 호스트를 ping 상태와 상관없이 다음 준비확인에 포함 (check 가 소비)
	doneLogged map[string]bool // 거부한 완료기록(호스트+내용) — 로그 1회
}

type hostRT struct {
	up            bool // 마지막 ping 응답
	localDown     bool // route=local 인데 마지막 로컬 ping 무응답 → 다음 ping 은 os6 에도 확인(both)
	startCheck    bool // 기동 직후 1회 준비확인
	resolveLogged bool
}

type inflight struct {
	jobID string
	user  string
	hosts []string        // Runner 에 넘긴 목록
	ready map[string]bool // run 시작 시 READY 였던 미처리 호스트 (수동 run 은 그룹 호스트 전체)
	first bool
	at    int64

	manual  bool   // 수동 run (requests manual-run)
	reqPath string // 수동 run 의 requests/active 파일 (끝나면 삭제)
	yml     string // 수동 run 그룹
	closed  bool   // 수동 run 대상이 jobs/done 의 종료된 job
	mode    string // 수동 run 모드: ModeCheck = 설정체크만 (job·호스트 상태를 바꾸지 않음), 비면 설정체크 + 설정수정
}

type runDone struct {
	res       RunResult
	err       error
	targets   []string      // 2차 대상 (없으면 2차 안 함)
	second    *SecondResult // 2차 결과 (호출 안 했으면 nil)
	secondErr error
}

type runProgress struct{ targets []string }

func newDefaultDaemon() *Daemon {
	return &Daemon{
		Clock:    realClock{},
		Pinger:   newPinger(),
		Checker:  newChecker(),
		Runner:   newRunner(),
		Notifier: newNotifier(),
		Ldap:     newLdap(),
		Second:   newSecond(),
	}
}

func (d *Daemon) ldapOn() bool {
	if d.Ldap == nil {
		return false
	}
	_, nop := d.Ldap.(nopLdap)
	return !nop
}

// secondImpl: 2차 체크를 할 수 있으면 구현, 아니면 nil (os6_mgmt 또는 os6OSCheckPath() 가 비었거나 nop)
func (d *Daemon) secondImpl() Second {
	if d.Second == nil || os6_mgmt == "" || os6OSCheckPath() == "" {
		return nil
	}
	if _, nop := d.Second.(nopSecond); nop {
		return nil
	}
	return d.Second
}

// selectSecondTargets: 2차 체크 대상 (순수 함수). 1차 run 시작 시 READY 였던 호스트(ready) 중
// route=local 이면서 (a) 접속불가(postapply 에 없음) 또는 (b) postapply FAIL(res.Failed).
// route=os6(이미 os6 경유)·미READY 호스트는 제외, os_check 비정상 종료(Abnormal) 면 없음. 이름순.
func selectSecondTargets(res RunResult, ready []string, routes map[string]string) []string {
	if res.Abnormal {
		return nil
	}
	got := map[string]bool{}
	for _, h := range res.Processed {
		got[h] = true
	}
	fail := map[string]bool{}
	for _, h := range res.Failed {
		fail[h] = true
	}
	var out []string
	for _, h := range uniq(ready) {
		if routes[h] == "local" && (!got[h] || fail[h]) {
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}

// setStage: 단계 갱신(정체 판정 포함). 바뀐 경우에만 stage_at 갱신·저장 표시.
func (d *Daemon) setStage(j *Job, name string, s Stage, t int64) {
	h := j.Hosts[name]
	if h == nil {
		return
	}
	tmp := *h
	tmp.Stage = string(s)
	e := string(effectiveStage(&tmp, t))
	if e != h.Stage {
		h.Stage, h.StageAt = e, t
		d.dirty[j.ID] = true
	}
}

// refreshStages: 시간 경과에 따른 단계(정체) 재계산 + 구버전 JSON 의 빈 stage 채움
func (d *Daemon) refreshStages(now time.Time) {
	t := now.Unix()
	for _, j := range d.jobs {
		for name, h := range j.Hosts {
			s := Stage(h.Stage)
			if s == "" {
				s = baseStage(h)
			}
			d.setStage(j, name, s, t)
		}
	}
}

// ---- pid 파일 (데몬 제어 훅: CLI --start/--stop/--restart 는 ctl.go 가 연결) ----

var errNotRunning = errors.New("데몬이 실행 중이 아닙니다")

func pidPath() string             { return pidPathIn(dataDir()) }
func pidPathIn(dir string) string { return filepath.Join(dir, "auto_setup.pid") }

// writePid: "<pid> <기동 epoch>" 원자 기록 (데몬이 lock 을 잡은 뒤 호출)
func writePid() error {
	p := pidPath()
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("%d %d\n", os.Getpid(), time.Now().Unix())), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// removePid: pid 파일이 자기 pid 일 때만 삭제 (데몬 종료 시)
func removePid() {
	if pid, _, err := readPid(); err == nil && pid == os.Getpid() {
		os.Remove(pidPath())
	}
}

// readPid: pid 파일 (pid, 기동 epoch)
func readPid() (pid int, started int64, err error) { return readPidIn(dataDir()) }

func readPidIn(dir string) (pid int, started int64, err error) {
	b, err := os.ReadFile(pidPathIn(dir))
	if err != nil {
		return 0, 0, err
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, 0, errors.New("pid 파일 형식 오류")
	}
	pid, err = strconv.Atoi(f[0])
	if err != nil || pid <= 0 {
		return 0, 0, errors.New("pid 파일 형식 오류")
	}
	if len(f) > 1 {
		started, _ = strconv.ParseInt(f[1], 10, 64)
	}
	return pid, started, nil
}

// isRunning: pid 파일의 프로세스가 살아 있는 auto_setup 데몬인지
func isRunning() (pid int, ok bool) {
	pid, _, err := readPid()
	if err != nil {
		return 0, false
	}
	return pid, processAlive(pid)
}

// processAlive: kill -0 성공 && (/proc 가 있으면) cmdline 에 daemon 인자 — pid 재사용·좀비 오판 방지
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && err != syscall.EPERM {
		return false
	}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		return bytes.Contains(b, []byte("\x00daemon"))
	}
	return true
}

// stopDaemon: SIGTERM → timeout 까지 종료 대기. 안 끝나면 에러(SIGKILL 은 보내지 않음). 실행 중이 아니면 errNotRunning.
func stopDaemon(timeout time.Duration) error {
	pid, ok := isRunning()
	if !ok {
		return errNotRunning
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("SIGTERM 실패(pid %d): %v", pid, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		if !processAlive(pid) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("pid %d 가 %s 안에 종료되지 않았습니다 (SIGKILL 은 보내지 않음)", pid, timeout)
		}
		time.Sleep(100 * time.Millisecond)
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
	if err := writePid(); err != nil {
		logf("[X] pid 파일 기록 실패: %v", err)
	}
	defer removePid()
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
	d.progCh = make(chan runProgress, 1)
	d.checkErr = map[string]string{}
	d.forceCheck = map[string]bool{}
	d.doneLogged = map[string]bool{}
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
	t := d.Clock.Now().Unix()
	for _, j := range jobs { // 전달시각 순 → 중복 호스트는 나중 job 이 가져감
		d.addJob(j, false)
		for name, h := range j.Hosts {
			if active(h) {
				d.hrt(name).startCheck = true
			}
			// 프로세스 안에서만 의미 있는 단계(LDAP확인/체크중/2차체크)는 되돌린다 → run 은 스케줄이 다시 잡음
			if Stage(h.Stage).transient() {
				d.setStage(j, name, baseStage(h), t)
			}
			if h.Second == SecondRunning {
				h.Second = ""
				d.dirty[j.ID] = true
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
	case p := <-d.progCh:
		d.onProgress(p, now)
	default:
	}
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
	d.collectRequests(now)
	d.dropCancelled()
	if now.Sub(d.lastPing) >= pingInterval {
		d.lastPing = now
		d.resolve()
		d.ping(now)
		d.ldapBackup()
	}
	if now.Sub(d.lastCheck) >= checkInterval {
		d.lastCheck = now
		d.check(now)
	}
	d.acceptDoneRecords(now)
	d.refreshStages(now)
	d.schedule(now)
	d.scheduleManual(now)
	d.closeFinished()
	d.saveDirty()
}

// onProgress: 1차 run 이 끝나고 2차 체크가 시작됨 → 대상 호스트 stage=second
func (d *Daemon) onProgress(p runProgress, now time.Time) {
	if d.cur == nil {
		return
	}
	j := d.jobs[d.cur.jobID]
	if j == nil {
		return
	}
	logf("2차 체크 시작: job %s %d대 (%s)", j.ID, len(p.targets), strings.Join(p.targets, " "))
	for _, n := range p.targets {
		if h := j.Hosts[n]; h != nil {
			h.Second = SecondRunning
			d.dirty[j.ID] = true
			if !d.cur.manual {
				d.setStage(j, n, StageSecond, now.Unix())
			}
		}
	}
}

// ldapBackup: 전달 시점 LDAP 백업 — 아직 ldap 상태가 없는 호스트 중 ping O·seen_down 아님 → Backup(job 당 1회 호출, 직렬)
func (d *Daemon) ldapBackup() {
	if !d.ldapOn() {
		return
	}
	for _, j := range d.sortedJobs() {
		var cand []string
		for name, h := range j.Hosts {
			if h.Ldap != nil || !active(h) {
				continue
			}
			if h.SeenDown {
				h.Ldap = &LdapState{Backup: LdapBackupNone, Reason: "전달 시점 ping X"}
				d.dirty[j.ID] = true
				continue
			}
			if r := d.rt[name]; r != nil && r.up {
				cand = append(cand, name)
			}
		}
		if len(cand) == 0 {
			continue
		}
		sort.Strings(cand)
		res := d.Ldap.Backup(j, cand)
		nOK := 0
		for _, n := range cand {
			st, ok := res[n]
			if !ok || st.Backup == "" {
				st = LdapState{Backup: LdapBackupNone, Reason: "백업 실패"}
			}
			if st.Backup == LdapBackupOK {
				nOK++
			} else {
				logf("[!] LDAP 백업 불가: %s (job %s) - %s", n, j.ID, st.Reason)
			}
			s := st
			j.Hosts[n].Ldap = &s
		}
		d.dirty[j.ID] = true
		logf("LDAP 백업: job %s %d대 중 %d대", j.ID, len(cand), nOK)
	}
}

// ldapApply: READY 직후 LDAP 비교·적용 (os_check run 전). stage=ldap 를 저장해 두고 호출.
func (d *Daemon) ldapApply(j *Job, name string, t int64) {
	h := j.Hosts[name]
	if !d.ldapOn() || h.Ldap == nil || h.Ldap.Backup != LdapBackupOK || h.Ldap.Bindpw != "" {
		return
	}
	d.setStage(j, name, StageLdap, t)
	if _, err := os.Stat(jobFile(j.ID)); err == nil {
		if _, err := saveJob(j); err != nil {
			logf("[X] job 저장 실패(%s): %v", j.ID, err)
		}
	}
	st := d.Ldap.Apply(j, name)
	if st.Backup == "" {
		st.Backup = h.Ldap.Backup
	}
	h.Ldap = &st
	d.dirty[j.ID] = true
	msg := fmt.Sprintf("LDAP 확인: %s (job %s) binddn=%s applied=%v", name, j.ID, st.Bindpw, st.Applied)
	if st.Reason != "" {
		msg += " (" + st.Reason + ")"
	}
	logf("%s", msg)
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
func (d *Daemon) ping(now time.Time) {
	ts := now.Unix()
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
			} else if r := d.rt[name]; route == "local" && os6_mgmt != "" && r != nil && r.localDown {
				route = "both" // 로컬 무응답 중이면 os6 에도 물어 os8 에서 안 보이는 호스트를 놓치지 않음
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
		h, name := r.h, t.Host
		if !ok || !pr.Known {
			if h.NoPing == 0 {
				h.NoPing = ts
				d.dirty[r.j.ID] = true
			}
			continue // 정보 없음 → 상태 불변
		}
		if h.NoPing != 0 {
			h.NoPing = 0
			d.dirty[r.j.ID] = true
		}
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
		if t.Route == "both" && pr.Via == "os6" && pr.Up {
			h.Route = "os6"
			d.dirty[r.j.ID] = true
			logf("경로 전환: %s local → os6 (os8 ping 무응답, os6_mgmt 응답, job %s)", name, r.j.ID)
		}
		rt := d.hrt(name)
		rt.localDown = h.Route == "local" && !pr.Up
		if pr.Up {
			rt.up = true
			if h.Miss != 0 {
				h.Miss = 0
				d.dirty[r.j.ID] = true
			}
			if h.SeenDown && h.ReadyAt == 0 && Stage(h.Stage) == StageDeploying {
				d.setStage(r.j, name, StageBooting, ts)
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
				h.DownAt = ts
				d.dirty[r.j.ID] = true
				logf("ping X: %s (job %s, 설치 시작으로 간주)", name, r.j.ID)
			}
			if h.ReadyAt != 0 {
				h.ReadyAt = 0
				h.DownAt = ts // 정체 판정은 이 재부팅부터 다시
				d.dirty[r.j.ID] = true
				logf("READY 해제(재부팅): %s (job %s)", name, r.j.ID)
			}
			if Stage(h.Stage) != StageInstalling { // anaconda 후 재부팅 ping X 는 설치중 유지
				d.setStage(r.j, name, StageDeploying, ts)
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
			if d.forceCheck[name] && active(h) && h.ReadyAt == 0 { // 수동 재시도(정체 호스트): ping·경로 판별 결과와 무관하게 확인
				route := h.Route
				if route == "" {
					route = "local"
				}
				owner[name] = j
				groups[route] = append(groups[route], name)
				continue
			}
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
	d.forceCheck = map[string]bool{} // 수동 재시도 표시는 이번 확인 한 번만 유효
	sort.Strings(routes)
	// os8 에서 준비확인 무응답인 local 호스트는 같은 주기에 os6_mgmt 경유로 다시 확인 (응답하면 route=os6 로 전환)
	fallback := os6_mgmt != "" && os6_gossh != ""
	t := now.Unix()
	for _, route := range routes {
		hosts := groups[route]
		sort.Strings(hosts)
		res, ok := d.checkRoute(route, route, hosts)
		if !ok {
			continue // 기동 직후 플래그 유지 → 다음 주기 재시도
		}
		var retry []string
		for _, name := range hosts {
			d.hrt(name).startCheck = false
			cr, ok := res[name]
			if !ok || !cr.Responded {
				if route == "local" && fallback {
					retry = append(retry, name)
				}
				continue
			}
			d.applyCheck(owner[name], name, cr, t)
		}
		if len(retry) == 0 {
			continue
		}
		res, ok = d.checkRoute("os6(대체)", "os6", retry)
		if !ok {
			continue
		}
		for _, name := range retry {
			cr, ok := res[name]
			if !ok || !cr.Responded {
				continue
			}
			j := owner[name]
			j.Hosts[name].Route = "os6"
			d.dirty[j.ID] = true
			logf("경로 전환: %s local → os6 (os8 준비확인 무응답, os6_mgmt 응답, job %s)", name, j.ID)
			d.applyCheck(j, name, cr, t)
		}
	}
}

// checkRoute: Checker 호출 1회. 실패는 key 별로 같은 메시지를 한 번만 로그하고 ok=false.
func (d *Daemon) checkRoute(key, route string, hosts []string) (map[string]CheckResult, bool) {
	res, err := d.Checker.Check(route, hosts)
	if err != nil {
		if msg := err.Error(); msg != d.checkErr[key] {
			d.checkErr[key] = msg
			logf("[X] 준비확인 실패(%s): %v", key, err)
		}
		return nil, false
	}
	d.checkErr[key] = ""
	return res, true
}

// applyCheck: 응답한 호스트 1대의 준비확인 결과 반영 (설치중 / 부팅확인 / READY)
func (d *Daemon) applyCheck(j *Job, name string, cr CheckResult, t int64) {
	h := j.Hosts[name]
	if cr.Anaconda {
		if h.SeenDown {
			d.setStage(j, name, StageInstalling, t)
		}
		return
	}
	// 마지막 부팅 시각 (완료기록 인정 규칙용). 반올림 흔들림(±2초)은 저장하지 않음.
	if boot := t - int64(cr.Uptime); boot-h.BootAt > 2 || h.BootAt-boot > 2 {
		h.BootAt = boot
		d.dirty[j.ID] = true
	}
	if cr.Uptime >= float64(t-j.Submitted) {
		if h.SeenDown {
			d.setStage(j, name, StageBooting, t)
		}
		return
	}
	h.ReadyAt = t
	d.dirty[j.ID] = true
	logf("READY: %s (job %s, uptime %.0fs)", name, j.ID, cr.Uptime)
	if !j.FirstRunDone {
		if j.FirstReady == 0 {
			j.FirstReady = t
		}
	} else if j.LateFirstReady == 0 && (d.cur == nil || d.cur.jobID != j.ID) {
		j.LateFirstReady = t
	}
	d.ldapApply(j, name, t)
	d.setStage(j, name, StageReady, t)
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
			for name, h := range j.Hosts {
				if h.Processed == "" { // 완료기록으로 이미 처리된 호스트는 제외
					all = append(all, name)
				}
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
		d.setStage(j, name, StageChecking, t)
	}
	kind := "늦은 묶음"
	if first {
		kind = "1차"
	}
	if d.runErr == "" { // Runner 오류 재시도(30초마다) 중에는 반복 로그 생략
		logf("run 시작: job %s %s %d대 (READY %d대)", j.ID, kind, len(hosts), len(ready))
	}
	d.launch(c, j, hasOS6)
}

// launch: run 고루틴 시작 (동시 1개). 1차 Runner.Run → (조건 충족 시) 2차 Second.Run 1회 → doneCh.
func (d *Daemon) launch(c *inflight, j *Job, hasOS6 bool) {
	d.cur = c
	snap := cloneJob(j)
	snap.RunMode = c.mode
	hosts := c.hosts
	ready := make([]string, 0, len(c.ready))
	for n := range c.ready {
		ready = append(ready, n)
	}
	sort.Strings(ready)
	r, sec, ch, prog := d.Runner, d.secondImpl(), d.doneCh, d.progCh
	go func() {
		res, err := r.Run(snap, hosts, hasOS6)
		out := runDone{res: res, err: err}
		if err == nil && sec != nil && snap.RunMode != ModeCheck { // 설정체크만 실행은 2차 체크를 하지 않음
			routes := map[string]string{}
			for n, h := range snap.Hosts {
				routes[n] = h.Route
			}
			if tg := selectSecondTargets(res, ready, routes); len(tg) > 0 {
				select {
				case prog <- runProgress{targets: tg}:
				default:
				}
				sr, serr := sec.Run(snap, res, tg)
				out.targets, out.second, out.secondErr = tg, &sr, serr
			}
		}
		ch <- out
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
	select { // 같은 run 의 늦게 도착한 2차 시작 알림은 버림
	case <-d.progCh:
	default:
	}
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
	if c.manual {
		d.finishManual(c, r, now)
		return
	}
	res := r.res
	t := now.Unix()
	var done []string
	if j := d.jobs[c.jobID]; j != nil {
		if !res.Abnormal {
			proc := append([]string(nil), res.Processed...)
			if r.second != nil { // 최종 처리 = 1차 OK 또는 2차 OK
				proc = append(proc, r.second.Processed...)
			}
			for _, name := range proc {
				if h := j.Hosts[name]; h != nil && h.Processed == "" {
					h.Processed = res.Code
					h.DoneSrc = DoneSrcRun
					h.Fails = 0
					done = append(done, name)
				}
			}
		}
		d.applySecond(j, r)
		for name := range c.ready {
			if h := j.Hosts[name]; h != nil && h.Processed == "" {
				h.Fails++
				if h.Fails >= maxFails {
					logf("[X] %s: 연속 %d회 실패 → 제외 (job %s)", name, h.Fails, j.ID)
				}
			}
		}
		for name := range c.ready {
			if h := j.Hosts[name]; h != nil {
				d.setStage(j, name, baseStage(h), t)
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

// applySecond: 2차 대상의 host.second 갱신 (ok = 2차 postapply 에 나오고 FAIL 없음)
func (d *Daemon) applySecond(j *Job, r runDone) {
	if len(r.targets) == 0 {
		return
	}
	if r.secondErr != nil {
		logf("[X] 2차 체크 실패(job %s): %v", j.ID, r.secondErr)
	}
	got, fail := map[string]bool{}, map[string]bool{}
	if r.second != nil {
		for _, n := range r.second.Processed {
			got[n] = true
		}
		for _, n := range r.second.Fail {
			fail[n] = true
		}
	}
	nOK := 0
	for _, n := range r.targets {
		h := j.Hosts[n]
		if h == nil {
			continue
		}
		h.Second = SecondFail
		if r.secondErr == nil && got[n] && !fail[n] {
			h.Second = SecondOK
			nOK++
		}
		d.dirty[j.ID] = true
	}
	logf("2차 체크 완료: job %s %d대 중 OK %d대", j.ID, len(r.targets), nOK)
}

// finishManual: 수동 run 결과 — processed/fails/스케줄은 건드리지 않고 runs 기록·2차 표시·wall, 요청 파일 삭제
func (d *Daemon) finishManual(c *inflight, r runDone, now time.Time) {
	res := r.res
	var j *Job
	if c.closed {
		j, _ = loadJobFile(filepath.Join(doneDir(), c.jobID+".json"))
	} else {
		j = d.jobs[c.jobID]
	}
	if j != nil {
		if c.mode != ModeCheck {
			d.applySecond(j, r)
		}
		j.Runs = append(j.Runs, Run{Code: res.Code, Hosts: c.hosts, At: c.at, Manual: true, Yml: c.yml, Mode: c.mode})
		if c.closed {
			if _, err := writeJobFile(filepath.Join(doneDir(), c.jobID+".json"), j); err != nil {
				logf("[X] job 저장 실패(%s): %v", c.jobID, err)
			}
		} else {
			d.dirty[j.ID] = true
		}
	}
	if err := os.Remove(c.reqPath); err != nil && !os.IsNotExist(err) {
		logf("[X] 요청 파일 삭제 실패(%s): %v", c.reqPath, err)
	}
	var done []string
	if !res.Abnormal {
		want := map[string]bool{}
		for _, n := range c.hosts {
			want[n] = true
		}
		proc := append([]string(nil), res.Processed...)
		if r.second != nil {
			proc = append(proc, r.second.Processed...)
		}
		for _, n := range uniq(proc) {
			if want[n] {
				done = append(done, n)
			}
		}
	}
	sort.Strings(done)
	// 수동 run 이 정상 실행되어 체크 결과가 나온 호스트는 현재 단계(정체·설치중 등)와 상관없이 완료 처리한다
	if j != nil && !res.Abnormal && c.mode != ModeCheck { // 설정체크만 실행은 완료 처리하지 않는다
		marked := 0
		for _, n := range done {
			if h := j.Hosts[n]; h != nil && h.Processed == "" {
				h.Processed, h.DoneSrc, h.Fails = res.Code, DoneSrcRun, 0
				if !c.closed {
					d.setStage(j, n, baseStage(h), now.Unix())
				}
				marked++
			}
		}
		if marked > 0 {
			logf("수동 run 으로 완료 처리: job %s 그룹 %s %d대 (code %s)", c.jobID, c.yml, marked, res.Code)
			if c.closed {
				if _, err := writeJobFile(filepath.Join(doneDir(), c.jobID+".json"), j); err != nil {
					logf("[X] job 저장 실패(%s): %v", c.jobID, err)
				}
			} else {
				d.dirty[j.ID] = true
			}
		}
	}
	note := ""
	if res.Abnormal {
		note = " (os_check 비정상 종료)"
	}
	// 처리(체크 결과가 나온) 호스트가 아닌 것 = 접속불가·미응답 → 로그에 호스트 목록을 남긴다
	got := map[string]bool{}
	for _, n := range done {
		got[n] = true
	}
	var unreachable []string
	if !res.Abnormal {
		for _, n := range uniq(c.hosts) {
			if !got[n] {
				unreachable = append(unreachable, n)
			}
		}
		sort.Strings(unreachable)
	}
	logf("수동 run 완료: job %s 그룹 %s code %s %d대%s 접속불가 %d대 (%s)", c.jobID, c.yml, res.Code, len(done), note, len(unreachable), modeLabel(c.mode))
	if len(unreachable) > 0 {
		logf("[!] 수동 run 접속불가·미응답 %d대 (job %s 그룹 %s code %s): %s", len(unreachable), c.jobID, c.yml, res.Code, strings.Join(unreachable, " "))
	}
	d.Notifier.Wall(wallMessage(c.user, done, res.Code, res.Abnormal) + unreachableLine(unreachable))
}

// unreachableLine: 수동 run 에서 체크 결과가 없는(접속불가·미응답) 호스트 안내 줄 (없으면 빈 문자열)
func unreachableLine(hosts []string) string {
	if len(hosts) == 0 {
		return ""
	}
	return fmt.Sprintf("[!] 접속불가·미응답 %d대: %s\n", len(hosts), strings.Join(hosts, " "))
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
		spec, err := parseJobSpec(b)
		if err != nil {
			logf("[X] queue 파일 무시(%s): %v", e.Name(), err)
			_ = os.Rename(p, p+".bad")
			continue
		}
		user, t, hosts := spec.User, spec.Time, spec.Hosts
		if t == 0 {
			t, _ = strconv.ParseInt(m[1], 10, 64)
		}
		now := d.Clock.Now().Unix()
		if t == 0 {
			t = now
		}
		j := &Job{ID: newJobID(user, t), User: user, Submitted: t, Hosts: map[string]*Host{}, Runs: []Run{},
			Groups: spec.Groups, AllYml: spec.AllYml}
		for _, h := range hosts {
			j.Hosts[h] = &Host{Stage: string(StageQueued), StageAt: now}
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
