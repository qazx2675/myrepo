// daemon_par_test.go - run 병렬 실행(max_runs) 단위 테스트: 상한, 멈춘 run 이 다른 job 을 막지 않음, 실행 중 호스트 제외(늦은 묶음·수동 run), max_runs=1 직렬
package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func withMaxRuns(t *testing.T, v string) {
	t.Helper()
	old := max_runs
	max_runs = v
	t.Cleanup(func() { max_runs = old })
}

// gateRunner: 가짜 Runner. 대상에 gates 의 호스트가 있으면 그 채널이 닫힐 때까지 멈춘다 (멈춘 gossh 흉내). 대상 전부 처리.
type gateRunner struct {
	mu    sync.Mutex
	calls [][]string
	gates map[string]chan struct{}
}

func (g *gateRunner) Run(j *Job, hosts []string, hasOS6 bool) (RunResult, error) {
	g.mu.Lock()
	g.calls = append(g.calls, append([]string(nil), hosts...))
	code := fmt.Sprintf("%04d", len(g.calls))
	var gate chan struct{}
	for _, h := range hosts {
		if c := g.gates[h]; c != nil {
			gate = c
		}
	}
	g.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return RunResult{Code: code, Processed: hosts}, nil
}

func (g *gateRunner) Calls() [][]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([][]string(nil), g.calls...)
}

type upPinger struct{}

func (upPinger) Ping(ts []PingTarget) map[string]PingResult {
	out := map[string]PingResult{}
	for _, t := range ts {
		out[t.Host] = PingResult{Up: true, Known: true}
	}
	return out
}

type noChecker struct{}

func (noChecker) Check(string, []string) (map[string]CheckResult, error) {
	return map[string]CheckResult{}, nil
}

// parJob: 호스트 → READY 시각(0 = 아직 READY 아님) 인 job 을 jobs/ 에 저장
func parJob(t *testing.T, id string, firstDone bool, late int64, hosts map[string]int64) {
	t.Helper()
	j := &Job{ID: id, User: "u1", Submitted: base, FirstRunDone: firstDone, LateFirstReady: late, Hosts: map[string]*Host{}, Runs: []Run{}}
	for h, r := range hosts {
		st := StageReady
		if r == 0 {
			st = StageBooting
		}
		j.Hosts[h] = &Host{IP: "10.0.0.1", Route: "local", SeenDown: true, ReadyAt: r, Stage: string(st), StageAt: base}
	}
	if _, err := saveJob(j); err != nil {
		t.Fatal(err)
	}
}

func parDaemon(t *testing.T, g *gateRunner, clock *fakeClock) *Daemon {
	t.Helper()
	d := &Daemon{Clock: clock, Pinger: upPinger{}, Checker: noChecker{}, Runner: g, Notifier: nopNotifier{}}
	d.onStart()
	return d
}

func parStep(d *Daemon, clock *fakeClock, o int64) {
	now := time.Unix(base+o, 0)
	clock.Set(now)
	d.step(now)
}

func waitRunsDone(t *testing.T, d *Daemon, n int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("run 결과 %d건", n), func() bool { return len(d.doneCh) >= n })
}

// 기본 max_runs(4): 서로 다른 job 6개가 동시에 READY → 4개만 동시에 실행, 끝나면 나머지 2개
func TestParallelRunsCapped(t *testing.T) {
	setupDir(t)
	withMaxRuns(t, "")
	gate := make(chan struct{})
	g := &gateRunner{gates: map[string]chan struct{}{}}
	for i := 1; i <= 6; i++ {
		h := fmt.Sprintf("h%d", i)
		g.gates[h] = gate
		parJob(t, fmt.Sprintf("2026100%d-100000-u1", i), false, 0, map[string]int64{h: base})
	}
	clock := &fakeClock{}
	d := parDaemon(t, g, clock)
	parStep(d, clock, 10)
	waitFor(t, "run 4개 시작", func() bool { return len(g.Calls()) == 4 })
	parStep(d, clock, 15)
	time.Sleep(50 * time.Millisecond)
	if n := len(g.Calls()); n != 4 || len(d.runs) != 4 {
		t.Fatalf("상한 4 를 넘김: 호출 %d, 실행 중 %d", n, len(d.runs))
	}
	close(gate)
	waitRunsDone(t, d, 4)
	parStep(d, clock, 20)
	waitFor(t, "나머지 2개", func() bool { return len(g.Calls()) == 6 })
	waitRunsDone(t, d, 2)
	parStep(d, clock, 25)
	var got []string
	for _, c := range g.Calls() {
		got = append(got, strings.Join(c, ","))
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"h1", "h2", "h3", "h4", "h5", "h6"}) || len(d.runs) != 0 {
		t.Fatalf("run 호출: %v (실행 중 %d)", got, len(d.runs))
	}
}

// max_runs=1: 종전처럼 한 번에 하나
func TestMaxRunsOneSerial(t *testing.T) {
	setupDir(t)
	withMaxRuns(t, "1")
	gate := make(chan struct{})
	g := &gateRunner{gates: map[string]chan struct{}{"h1": gate, "h2": gate}}
	parJob(t, "20261001-100000-u1", false, 0, map[string]int64{"h1": base})
	parJob(t, "20261002-100000-u1", false, 0, map[string]int64{"h2": base})
	clock := &fakeClock{}
	d := parDaemon(t, g, clock)
	parStep(d, clock, 10)
	parStep(d, clock, 15)
	waitFor(t, "run 시작", func() bool { return len(g.Calls()) == 1 })
	time.Sleep(50 * time.Millisecond)
	if n := len(g.Calls()); n != 1 || len(d.runs) != 1 {
		t.Fatalf("max_runs=1 인데 동시 실행: 호출 %d", n)
	}
	close(gate)
	waitRunsDone(t, d, 1)
	parStep(d, clock, 20)
	waitFor(t, "다음 run", func() bool { return len(g.Calls()) == 2 })
	if c := g.Calls(); !reflect.DeepEqual(c, [][]string{{"h1"}, {"h2"}}) {
		t.Fatalf("순서: %v", c)
	}
	waitRunsDone(t, d, 1)
	parStep(d, clock, 25)
}

// 한 job 의 run 이 멈춰 있어도 다른 job 은 실행·완료된다
func TestHungRunDoesNotBlockOtherJob(t *testing.T) {
	setupDir(t)
	withMaxRuns(t, "")
	gate := make(chan struct{})
	defer close(gate)
	g := &gateRunner{gates: map[string]chan struct{}{"hA": gate}}
	parJob(t, "20261001-100000-u1", false, 0, map[string]int64{"hA": base})
	parJob(t, "20261002-100000-u2", false, 0, map[string]int64{"hB": base})
	clock := &fakeClock{}
	d := parDaemon(t, g, clock)
	parStep(d, clock, 10)
	waitFor(t, "두 run 시작", func() bool { return len(g.Calls()) == 2 })
	waitRunsDone(t, d, 1)
	parStep(d, clock, 15)
	j := doneJob(t, "20261002-100000-u2")
	if j.Hosts["hB"].Processed != "0002" && j.Hosts["hB"].Processed != "0001" {
		t.Fatalf("job B 미완료: %+v", j.Hosts["hB"])
	}
	if len(d.runs) != 1 || d.jobs["20261001-100000-u1"] == nil {
		t.Fatalf("멈춘 job A 의 run 은 계속 실행 중이어야 함: %d", len(d.runs))
	}
	if h := d.jobs["20261001-100000-u1"].Hosts["hA"]; h.Stage != string(StageChecking) {
		t.Fatalf("job A 단계: %+v", h)
	}
}

// 같은 job 의 앞 묶음이 실행 중이어도 그 뒤 READY 된 호스트는 자기 READY+lateWait 에 따로 시작 (실행 중 호스트는 제외),
// 수동 run 은 대상 호스트가 다른 run 에 있으면 그 run 이 끝날 때까지 기다린다
func TestLateBatchWhileEarlierBatchRuns(t *testing.T) {
	setupDir(t)
	withMaxRuns(t, "")
	gate := make(chan struct{})
	g := &gateRunner{gates: map[string]chan struct{}{"h1": gate}}
	id := "20261001-100000-u1"
	parJob(t, id, true, base, map[string]int64{"h1": base, "h2": 0, "h3": 0})
	clock := &fakeClock{}
	d := parDaemon(t, g, clock)
	d.jobs[id].Groups = map[string]*Group{"a.yml": {Hosts: []string{"h1", "h3"}}}
	parStep(d, clock, 175)
	if len(g.Calls()) != 0 {
		t.Fatal("lateWait 전에 시작")
	}
	parStep(d, clock, 180)
	waitFor(t, "h1 묶음", func() bool { return len(g.Calls()) == 1 })
	// h1 묶음 실행 중 h2 가 READY (base+200)
	d.jobs[id].Hosts["h2"].ReadyAt = base + 200
	parStep(d, clock, 200)
	parStep(d, clock, 375)
	if len(g.Calls()) != 1 {
		t.Fatalf("h2 는 READY+lateWait(380) 전에 시작하면 안 됨: %v", g.Calls())
	}
	// 수동 run: a.yml(h1,h3) 은 h1 이 실행 중이라 대기
	if _, err := writeRequest(ReqManualRun, map[string]string{"jobid": id, "yml": "a.yml"}); err != nil {
		t.Fatal(err)
	}
	parStep(d, clock, 380)
	waitFor(t, "h2 묶음", func() bool { return len(g.Calls()) == 2 })
	waitRunsDone(t, d, 1)
	parStep(d, clock, 385)
	time.Sleep(50 * time.Millisecond)
	if c := g.Calls(); !reflect.DeepEqual(c, [][]string{{"h1"}, {"h2"}}) {
		t.Fatalf("run 호출(h1 실행 중 → h2 만, 수동 run 은 대기): %v", c)
	}
	if d.jobs[id].Hosts["h2"].Processed == "" {
		t.Fatal("h2 미처리")
	}
	close(gate)
	waitRunsDone(t, d, 1)
	parStep(d, clock, 390)
	waitFor(t, "수동 run", func() bool { return len(g.Calls()) == 3 })
	if c := g.Calls(); !reflect.DeepEqual(c[2], []string{"h1", "h3"}) {
		t.Fatalf("수동 run 대상: %v", c)
	}
	waitRunsDone(t, d, 1)
	parStep(d, clock, 395)
	if len(d.runs) != 0 {
		t.Fatalf("남은 run %d", len(d.runs))
	}
}

func TestMaxRunsHangLimitConf(t *testing.T) {
	saveConfVars(t)
	for _, c := range []struct {
		in   string
		want int
	}{{"", 4}, {"x", 4}, {"1.5", 4}, {"0", 1}, {"-2", 1}, {"1", 1}, {" 8 ", 8}} {
		max_runs = c.in
		if got := maxRuns(); got != c.want {
			t.Errorf("max_runs=%q: %d, 기대 %d", c.in, got, c.want)
		}
	}
	for _, c := range []struct {
		in   string
		want time.Duration
	}{{"", 7 * time.Minute}, {"x", 7 * time.Minute}, {"-1", 7 * time.Minute}, {"0", 0}, {"3", 3 * time.Minute}} {
		hang_min = c.in
		if got := hangLimit(); got != c.want {
			t.Errorf("hang_min=%q: %v, 기대 %v", c.in, got, c.want)
		}
	}
	p := writeConf(t, "max_runs = 2\nhang_min=0\n")
	max_runs, hang_min = "", ""
	loadConf([]string{p})
	if maxRuns() != 2 || hangLimit() != 0 {
		t.Errorf("conf 에서 읽은 값: %d %v", maxRuns(), hangLimit())
	}
}
