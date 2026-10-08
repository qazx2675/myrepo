// daemon_test.go - 상태기계·준비확인·7분/3분 스케줄·중복 전달·재기동 복원 단위 테스트 (가짜 시계·pinger·checker·runner)
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const base = int64(1759386600) // 전달 시각 (테스트 시각은 base 기준 오프셋 초)
const never = int64(1 << 40)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// simHost: 설치 경과 (오프셋 초). o<dd: 기존 OS, [ana,reboot): anaconda, o>=boot: 새 OS
type simHost struct {
	dd, ana, reboot, boot int64
	downs                 [][2]int64 // 추가 down 구간
	remote                bool       // 로컬 ICMP 불가 (os6 경로로만 보임)
	unknown               bool       // 항상 Known=false
	noLocalSSH            bool       // 로컬 준비확인(gossh) 무응답 (ping 은 됨, os6 경유로만 응답)
}

func oldOS() *simHost { return &simHost{dd: never, ana: never, reboot: never, boot: never} }

func install(dd, ana, reboot, boot int64) *simHost {
	return &simHost{dd: dd, ana: ana, reboot: reboot, boot: boot}
}

func (s *simHost) up(o int64) bool {
	for _, w := range s.downs {
		if o >= w[0] && o < w[1] {
			return false
		}
	}
	return o < s.dd || (o >= s.ana && o < s.reboot) || o >= s.boot
}

func (s *simHost) check(o int64) CheckResult {
	switch {
	case !s.up(o):
		return CheckResult{}
	case o < s.dd:
		return CheckResult{Responded: true, Uptime: 9e6}
	case o >= s.boot:
		return CheckResult{Responded: true, Uptime: float64(o - s.boot)}
	}
	return CheckResult{Responded: true, Anaconda: true, Uptime: float64(o - s.ana)}
}

type runCall struct {
	At    int64
	Hosts []string
	OS6   bool
	Mode  string // Job.RunMode: ModeCheck = 설정체크만
}

// world: 가짜 Pinger/Checker/Runner/Notifier + 이름 해석
type world struct {
	mu         sync.Mutex
	clock      *fakeClock
	hosts      map[string]*simHost
	checks     []string // "오프셋 route h1,h2"
	runs       []runCall
	walls      []string
	runFn      func(n int, o int64, hosts []string) RunResult // nil → 새 OS 로 부팅된 호스트 processed
	lookupFail map[string]int
	lookups    map[string]int
}

func newWorld(hosts map[string]*simHost) *world {
	return &world{clock: &fakeClock{t: time.Unix(base, 0)}, hosts: hosts,
		lookupFail: map[string]int{}, lookups: map[string]int{}}
}

func (w *world) off() int64 { return w.clock.Now().Unix() - base }

func (w *world) Ping(ts []PingTarget) map[string]PingResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]PingResult{}
	for _, t := range ts {
		s := w.hosts[t.Host]
		switch {
		case s.unknown:
			out[t.Host] = PingResult{}
		case s.remote && t.Route == "local":
			out[t.Host] = PingResult{Known: true}
		case t.Route == "both": // realPinger.mergeBoth 와 같은 규칙 (os6 세션은 항상 응답)
			up := s.up(w.off())
			if !s.remote && up {
				out[t.Host] = PingResult{Up: true, Known: true, Via: "local"}
			} else {
				out[t.Host] = PingResult{Up: up, Known: true, Via: "os6"}
			}
		default:
			out[t.Host] = PingResult{Up: s.up(w.off()), Known: true}
		}
	}
	return out
}

func (w *world) Check(route string, hosts []string) (map[string]CheckResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.checks = append(w.checks, fmt.Sprintf("%d %s %s", w.off(), route, strings.Join(hosts, ",")))
	out := map[string]CheckResult{}
	for _, h := range hosts {
		if s := w.hosts[h]; route == "local" && (s.noLocalSSH || s.remote) {
			out[h] = CheckResult{}
			continue
		}
		out[h] = w.hosts[h].check(w.off())
	}
	return out, nil
}

func (w *world) Run(j *Job, hosts []string, hasOS6 bool) (RunResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	o := w.off()
	w.runs = append(w.runs, runCall{At: o, Hosts: append([]string(nil), hosts...), OS6: hasOS6, Mode: j.RunMode})
	code := fmt.Sprintf("%04d", len(w.runs))
	if w.runFn != nil {
		r := w.runFn(len(w.runs), o, hosts)
		r.Code = code
		return r, nil
	}
	r := RunResult{Code: code}
	for _, h := range hosts {
		if s := w.hosts[h]; o >= s.boot && s.up(o) {
			r.Processed = append(r.Processed, h)
		}
	}
	return r, nil
}

func (w *world) Wall(msg string) {
	w.mu.Lock()
	w.walls = append(w.walls, msg)
	w.mu.Unlock()
}

func (w *world) lookup(h string) ([]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lookups[h]++
	if w.lookups[h] <= w.lookupFail[h] {
		return nil, errors.New("no such host")
	}
	return []string{"ip-" + h}, nil
}

func newTestDaemon(w *world) *Daemon {
	d := &Daemon{Clock: w.clock, Pinger: w, Checker: w, Runner: w, Notifier: w, Lookup: w.lookup}
	d.onStart()
	return d
}

func jobIDAt(user string, o int64) string {
	return time.Unix(base+o, 0).Format("20060102-150405") + "-" + user
}

func submit(t *testing.T, user string, o int64, hosts ...string) string {
	t.Helper()
	p := filepath.Join(queueDir(), fmt.Sprintf("%d_%s_1.job", base+o, user))
	body := fmt.Sprintf("user=%s\ntime=%d\n%s\n", user, base+o, strings.Join(hosts, "\n"))
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return jobIDAt(user, o)
}

// drive: from~to 오프셋을 5초(queuePoll) 간격으로 step. restarts 시각에는 새 데몬으로 복원 후 진행.
func drive(t *testing.T, w *world, d *Daemon, from, to int64, restarts ...int64) *Daemon {
	t.Helper()
	for o := from; o <= to; o += 5 {
		for _, r := range restarts {
			if r == o {
				d = newTestDaemon(w)
			}
		}
		now := time.Unix(base+o, 0)
		w.clock.Set(now)
		d.step(now)
		if d.cur != nil { // 가짜 runner 가 끝날 때까지 기다림 (결과 반영은 다음 step)
			for i := 0; len(d.doneCh) == 0; i++ {
				if i > 5000 {
					t.Fatal("runner 응답 없음")
				}
				time.Sleep(time.Millisecond)
			}
		}
	}
	return d
}

func readJobFile(t *testing.T, p string) *Job {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("job 파일 없음: %v", err)
	}
	j := &Job{}
	if err := json.Unmarshal(b, j); err != nil {
		t.Fatal(err)
	}
	return j
}

func doneJob(t *testing.T, id string) *Job {
	t.Helper()
	if _, err := os.Stat(jobFile(id)); err == nil {
		t.Fatalf("job %s 가 아직 jobs/ 에 남아 있음", id)
	}
	return readJobFile(t, filepath.Join(doneDir(), id+".json"))
}

func wantRuns(t *testing.T, w *world, want []runCall) {
	t.Helper()
	if !reflect.DeepEqual(w.runs, want) {
		t.Fatalf("run 호출 불일치\n got: %+v\nwant: %+v", w.runs, want)
	}
}

// 정상: O→X→O(anaconda, 미준비)→X→O(READY) 전원 → 즉시 1회 run, 전체 목록
func TestNormalAllReadyImmediate(t *testing.T) {
	setupDir(t)
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600), "h2": install(60, 150, 420, 620)})
	id := submit(t, "u1", 0, "h1", "h2")
	drive(t, w, newTestDaemon(w), 0, 1200)

	wantRuns(t, w, []runCall{{At: 630, Hosts: []string{"h1", "h2"}}})
	ana := false
	for _, c := range w.checks {
		var o int64
		fmt.Sscan(c, &o)
		if o >= 150 && o < 400 {
			ana = true
		}
	}
	if !ana {
		t.Fatalf("anaconda 구간에 준비확인이 없음: %v", w.checks)
	}
	want := "[auto_setup] OS 설치 + 설정체크 완료 (user=u1)\nh1 h2 (2대)\ncode : 0001   →  auto_setup code 0001\n"
	if len(w.walls) != 1 || w.walls[0] != want {
		t.Fatalf("wall 불일치: %q", w.walls)
	}
	j := doneJob(t, id)
	if j.FirstReady != base+600 || !j.FirstRunDone || j.Hosts["h1"].Processed != "0001" || j.Hosts["h2"].Processed != "0001" {
		t.Fatalf("job 상태 이상: %+v", j)
	}
}

func scenario7(t *testing.T, restarts ...int64) (*world, *Job) {
	setupDir(t)
	w := newWorld(map[string]*simHost{
		"h1": install(60, 120, 400, 600),
		"h2": install(60, 120, 400, 1075),
		"h3": install(60, 120, 400, 1135),
	})
	id := submit(t, "u1", 0, "h1", "h2", "h3")
	drive(t, w, newTestDaemon(w), 0, 1600, restarts...)
	return w, doneJob(t, id)
}

// 7분 규칙: 1대 READY → 7분 후 전체 목록 run, 늦은 2대(1분 간격) → 3분 묶음 1회에 2대만
func TestBootWaitAndLateBatch(t *testing.T) {
	w, j := scenario7(t)
	wantRuns(t, w, []runCall{
		{At: 1020, Hosts: []string{"h1", "h2", "h3"}},
		{At: 1260, Hosts: []string{"h2", "h3"}},
	})
	if len(w.walls) != 2 || !strings.Contains(w.walls[0], "\nh1 (1대)\n") || !strings.Contains(w.walls[1], "\nh2 h3 (2대)\n") {
		t.Fatalf("wall 불일치: %q", w.walls)
	}
	if j.Hosts["h1"].Processed != "0001" || j.Hosts["h2"].Processed != "0002" || j.Hosts["h3"].Processed != "0002" {
		t.Fatalf("processed 이상: %+v %+v %+v", j.Hosts["h1"], j.Hosts["h2"], j.Hosts["h3"])
	}
}

// 재기동 복원: seen_down 전/후, 7분 대기 중, 늦은 묶음 대기 중 재기동해도 같은 결과·같은 시각
func TestRestartRestore(t *testing.T) {
	var bw *world
	var bj *Job
	t.Run("baseline", func(t *testing.T) { bw, bj = scenario7(t) })
	for _, rs := range [][]int64{{30}, {90}, {900}, {1200}, {30, 90, 900, 1200}} {
		rs := rs
		t.Run(fmt.Sprint(rs), func(t *testing.T) {
			w, j := scenario7(t, rs...)
			if !reflect.DeepEqual(w.runs, bw.runs) || !reflect.DeepEqual(w.walls, bw.walls) {
				t.Fatalf("run/wall 불일치\n got: %+v %q\nwant: %+v %q", w.runs, w.walls, bw.runs, bw.walls)
			}
			if !reflect.DeepEqual(j, bj) {
				a, _ := json.Marshal(j)
				b, _ := json.Marshal(bj)
				t.Fatalf("최종 job 불일치\n got: %s\nwant: %s", a, b)
			}
		})
	}
}

// dd 전 기존 OS (ping O, uptime 큼) → READY 아님. 순간 끊김(seen_down)이나 기동 직후 확인이 있어도 마찬가지.
func TestOldOSNotReady(t *testing.T) {
	setupDir(t)
	blip := oldOS()
	blip.downs = [][2]int64{{100, 130}}
	w := newWorld(map[string]*simHost{"h1": blip, "h2": oldOS()})
	id := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, w, newTestDaemon(w), 0, 1500, 600)
	if len(w.runs) != 0 {
		t.Fatalf("run 이 실행됨: %+v", w.runs)
	}
	h2checks := 0
	for _, c := range w.checks {
		if strings.Contains(c, "h2") {
			h2checks++
		}
	}
	if h2checks != 1 {
		t.Fatalf("h2 는 기동 직후 1회만 확인돼야 함: %v", w.checks)
	}
	j := d.jobs[id]
	if j == nil || j.Hosts["h1"].ReadyAt != 0 || j.Hosts["h2"].ReadyAt != 0 || !j.Hosts["h1"].SeenDown || j.FirstReady != 0 {
		t.Fatalf("job 상태 이상: %+v", j)
	}
}

// 중복 전달: 진행 중 job 의 호스트를 새 job 이 가져가고, 비면 이전 job 종료
func TestDuplicateTransfer(t *testing.T) {
	setupDir(t)
	w := newWorld(map[string]*simHost{"h1": oldOS(), "h2": oldOS(), "h3": oldOS()})
	a := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, w, newTestDaemon(w), 0, 20)
	b := submit(t, "u2", 30, "h2", "h3")
	d = drive(t, w, d, 30, 40)
	keys := func(j *Job) []string {
		var ks []string
		for k := range j.Hosts {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	if ja := readJobFile(t, jobFile(a)); !reflect.DeepEqual(keys(ja), []string{"h1"}) || !reflect.DeepEqual(keys(d.jobs[a]), []string{"h1"}) {
		t.Fatalf("이전 job 에 h2 가 남아 있음: %v", keys(ja))
	}
	if jb := readJobFile(t, jobFile(b)); !reflect.DeepEqual(keys(jb), []string{"h2", "h3"}) {
		t.Fatalf("새 job 호스트 이상: %v", keys(jb))
	}
	c := submit(t, "u3", 60, "h1")
	d = drive(t, w, d, 60, 70)
	doneJob(t, a)
	if d.jobs[a] != nil || d.jobs[b] == nil || d.jobs[c] == nil {
		t.Fatalf("메모리 job 이상: %v", d.jobs)
	}
}

// postapply 에 없는 READY 호스트 → 미처리 유지, 3분 뒤 그 호스트만 재시도
func TestPostapplyMissingRetried(t *testing.T) {
	setupDir(t)
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600), "h2": install(60, 120, 400, 600)})
	w.runFn = func(n int, o int64, hosts []string) RunResult {
		if n == 1 {
			return RunResult{Processed: []string{"h1"}}
		}
		return RunResult{Processed: hosts}
	}
	id := submit(t, "u1", 0, "h1", "h2")
	drive(t, w, newTestDaemon(w), 0, 1200)
	wantRuns(t, w, []runCall{{At: 600, Hosts: []string{"h1", "h2"}}, {At: 785, Hosts: []string{"h2"}}})
	j := doneJob(t, id)
	if j.Hosts["h1"].Processed != "0001" || j.Hosts["h2"].Processed != "0002" || len(j.Runs) != 2 {
		t.Fatalf("job 상태 이상: %+v %+v", j.Hosts["h1"], j.Hosts["h2"])
	}
}

// Known=false 는 상태 불변 (경로 판별·miss·seen_down 모두 안 바뀜)
func TestKnownFalseIgnored(t *testing.T) {
	setupDir(t)
	s := install(60, 120, 400, 600)
	s.unknown = true
	w := newWorld(map[string]*simHost{"h1": s})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 1500)
	h := d.jobs[id].Hosts["h1"]
	if len(w.checks) != 0 || len(w.runs) != 0 || h.Miss != 0 || h.SeenDown || h.Route != "" || h.IP == "" {
		t.Fatalf("Known=false 인데 상태가 바뀜: %+v checks=%v", h, w.checks)
	}
}

// os_check 비정상 종료 연속 3회 → 실패 제외, 무한 재시도 없음
func TestFailsExcluded(t *testing.T) {
	setupDir(t)
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600)})
	w.runFn = func(n int, o int64, hosts []string) RunResult {
		return RunResult{Processed: hosts, Abnormal: true}
	}
	id := submit(t, "u1", 0, "h1")
	drive(t, w, newTestDaemon(w), 0, 2500)
	wantRuns(t, w, []runCall{{At: 600, Hosts: []string{"h1"}}, {At: 785, Hosts: []string{"h1"}}, {At: 970, Hosts: []string{"h1"}}})
	for _, m := range w.walls {
		if !strings.Contains(m, "\n(0대)\n") || !strings.HasSuffix(m, "[!] os_check 비정상 종료\n") {
			t.Fatalf("비정상 wall 형식 이상: %q", m)
		}
	}
	j := doneJob(t, id)
	if h := j.Hosts["h1"]; h.Processed != "" || h.Fails != 3 || hostState(h) != "실패" {
		t.Fatalf("실패 제외 이상: %+v", h)
	}
}

// READY 후 다시 ping down(재부팅) → READY 해제, 7분은 첫 READY 기준 고정
func TestReadyRevokedOnReboot(t *testing.T) {
	setupDir(t)
	h1 := install(60, 120, 400, 600)
	h1.downs = [][2]int64{{700, 760}}
	w := newWorld(map[string]*simHost{"h1": h1, "h2": install(60, 120, 400, never)})
	id := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, w, newTestDaemon(w), 0, 740)
	if j := d.jobs[id]; j.Hosts["h1"].ReadyAt != 0 || j.FirstReady != base+600 {
		t.Fatalf("재부팅 후 READY 해제 안 됨: %+v first=%d", j.Hosts["h1"], j.FirstReady)
	}
	drive(t, w, d, 745, 1100)
	wantRuns(t, w, []runCall{{At: 1020, Hosts: []string{"h1", "h2"}}})
	j := readJobFile(t, jobFile(id))
	if j.Hosts["h1"].ReadyAt != base+780 || j.Hosts["h1"].Processed != "0001" || j.Hosts["h2"].Processed != "" {
		t.Fatalf("job 상태 이상: %+v %+v", j.Hosts["h1"], j.Hosts["h2"])
	}
}

// 이름 해석 실패 → 다음 주기 재시도 (그동안 ping 안 함), 로컬 무응답 → route=os6, 준비확인은 route 별 호출
func TestResolveRetryAndOS6Route(t *testing.T) {
	setupDir(t)
	old := os6_mgmt
	os6_mgmt = "mgmt"
	defer func() { os6_mgmt = old }()
	h1 := oldOS()
	h1.remote = true
	w := newWorld(map[string]*simHost{"h1": h1, "h2": oldOS()})
	w.lookupFail["h1"] = 2
	id := submit(t, "u1", 0, "h1", "h2")
	d := drive(t, w, newTestDaemon(w), 0, 15)
	if h := d.jobs[id].Hosts["h1"]; h.IP != "" || h.Miss != 0 || h.Route != "" {
		t.Fatalf("미해결 호스트 상태 이상: %+v", h)
	}
	d = drive(t, w, d, 20, 45)
	j := d.jobs[id]
	if j.Hosts["h1"].Route != "os6" || j.Hosts["h2"].Route != "local" || j.Hosts["h1"].Miss != 0 {
		t.Fatalf("경로 판별 이상: %+v %+v", j.Hosts["h1"], j.Hosts["h2"])
	}
	drive(t, w, d, 60, 60, 60)
	want := []string{"60 local h2", "60 os6 h1"}
	if !reflect.DeepEqual(w.checks, want) {
		t.Fatalf("route 별 준비확인 이상: %v", w.checks)
	}
}

// cancel 로 jobs/<id>.json 이 done 으로 이동 → 메모리에서도 제거, 되살리지 않음
func TestCancelDropsJob(t *testing.T) {
	setupDir(t)
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600)})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 5)
	if err := cancelJob(id); err != nil {
		t.Fatal(err)
	}
	d = drive(t, w, d, 10, 200)
	if len(d.jobs) != 0 {
		t.Fatalf("cancel 후 메모리에 남음: %v", d.jobs)
	}
	if _, err := os.Stat(jobFile(id)); !os.IsNotExist(err) {
		t.Fatal("cancel 된 job 파일이 되살아남")
	}
}
