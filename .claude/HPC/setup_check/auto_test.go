package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// -auto 가짜 실행기: 호스트마다 launch 시각을 따로 두고 가짜 시각으로 움직인다.
// 사건은 "<종류>:<호스트들>@<시작 뒤 분>" 으로 쌓는다.
// ---------------------------------------------------------------------------

type autoHost struct {
	unreachUntil time.Duration // >0 이면 시작 뒤 이 시각 전까지 접속불가
	check        string
	cpu, stress  string
	fixOn        int           // 이 번째 설정수정에서 설정체크 O(0 = 고쳐지지 않음)
	cpuAfter     time.Duration // launch 뒤 이만큼 지나면 CPU O(0 = 끝내 O 가 안 됨)
	load         float64       // Loads 가 낼 1분 부하
	ngFail       bool          // stress-ng 가 launch 되지 않음
	vendorX      bool          // CPU 벤더가 Intel/AMD 가 아님(판정 결과 VendorUnknown)
	cpuLateAt    time.Duration // >0 이면 시작 뒤 이 시각부터 launch 와 상관없이 CPU O(앞 테스트가 늦게 끝남)

	fixes    int
	launchAt time.Time
}

type autoFake struct {
	t          *testing.T
	mu         sync.Mutex
	start, now time.Time
	order      []string
	hosts      map[string]*autoHost
	w          *worker
	events     []string
	ends       []string
	sleeps     int

	// AWX 인벤토리 소스·호스트 흉내: StressTest 가 등록하면 바뀌고 YamlState 가 읽는다.
	awxYml     string
	awxHosts   []string
	regSeq     int
	regs       []string                       // 등록 기록 "<yml>@<분>"
	yamlCalls  []string                       // YamlState 호출 "@<분>"
	yamlErr    error                          // YamlState 오류
	noRegHosts bool                           // [등록] 줄에 호스트를 싣지 않음(호스트를 못 읽은 경우)
	noReg      bool                           // [등록] 줄을 내지 않음(옛 stress-test)
	onReg      func(f *autoFake, n int)       // n 번째 등록 직후(다른 사람이 바꾼 것을 흉내)
	onQ        func(f *autoFake, q *question) // 'yaml 바뀜' 질문이 떠 있는 동안 Sleep 마다 부름(답은 writeAnswer 로)
	qFirst     map[string]time.Time           // 질문 id → 처음 본 시각
	qDetail    []string                       // 질문이 떠 있는 동안 본 진행 설명(중복 없이)
	qTexts     []string                       // 본 질문 문구
	logb       *strings.Builder               // 워커 로그(runAutoFake 가 채운다)

	// 환경 실패 흉내(auto_env_test.go): StressTest 호출마다 앞에서 하나씩 꺼내 true 면 launch 전에 실행 오류로 끝난다.
	failPlan []bool
	fixErrN  int      // 앞의 이 횟수만큼 ConfigFix 가 오류를 낸다(고치지 않음)
	seenErr  []string // 실패 흉내를 낸 StressTest 호출 때 첫 호스트의 AutoErr
	seenTry  []int    // 그때 첫 호스트의 CPUTries+NGTries

	// 조용히 남는 호스트 흉내(auto_stuck_test.go)
	judgeFail []bool            // Judge 호출마다 앞에서 하나씩 꺼내 true 면 오류를 낸다(사건은 남긴다)
	probeErrN int               // 앞의 이 횟수만큼 Probe 가 오류를 낸다
	onProbe   func(f *autoFake) // Probe 호출 때(워커 고루틴에서) 부름

	// 인벤토리 사용 중 흉내(auto_busy_test.go): busyN[steps] 번만큼 등록 전에 busy 를 부른다(답 하나마다 하나 줄어든다).
	// r 이면 다시 확인, 그 밖(x)이면 그 묶음을 중단하고 종료코드 2 로 끝난다.
	busyN   map[string]int
	busyAns []string                       // "<steps>=<답>@<분>"
	onBusy  func(f *autoFake, q *question) // 인벤토리 사용 중 질문이 떠 있는 동안 Sleep 마다 부름(답은 writeAnswer 로)
}

func (f *autoFake) mins() int { return int(f.now.Sub(f.start) / time.Minute) }

// register 는 등록을 흉내 낸다: AWX 소스·호스트를 이 묶음의 것으로 바꾸고 [등록] 줄로 알릴 값을 돌려준다(f.mu 를 잡고 부른다).
func (f *autoFake) register(hosts []string) (yml string, report []string) {
	f.regSeq++
	yml = fmt.Sprintf("auto_%d_%dea.yml", f.regSeq, len(hosts))
	f.awxYml = yml
	f.awxHosts = append([]string(nil), hosts...)
	f.regs = append(f.regs, fmt.Sprintf("%s@%d", yml, f.mins()))
	if !f.noRegHosts {
		report = append([]string(nil), hosts...)
	}
	return yml, report
}

func (f *autoFake) YamlState() (yamlInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.yamlCalls = append(f.yamlCalls, fmt.Sprintf("@%d", f.mins()))
	if f.yamlErr != nil {
		return yamlInfo{}, f.yamlErr
	}
	return yamlInfo{Yml: f.awxYml, Hosts: append([]string(nil), f.awxHosts...)}, nil
}

// answerQ 는 질문 id 에 choice 로 답한다(answer.json).
func (f *autoFake) answerQ(id, choice string) {
	if err := writeAnswer(f.w.dir, id, choice); err != nil {
		f.t.Fatalf("답 쓰기: %v", err)
	}
}

// qAge 는 질문을 처음 본 뒤 흐른 시간.
func (f *autoFake) qAge(q *question) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now.Sub(f.qFirst[q.ID])
}

func newAutoFake(t *testing.T, start time.Time, order []string, hosts map[string]*autoHost) *autoFake {
	return &autoFake{t: t, start: start, now: start, order: order, hosts: hosts}
}

func (f *autoFake) ev(kind string, hosts []string) {
	f.events = append(f.events, fmt.Sprintf("%s:%s@%d", kind, strings.Join(hosts, ","), int(f.now.Sub(f.start)/time.Minute)))
}

func (f *autoFake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Sleep 은 시각만 넘긴다. -auto 는 질문을 내면 안 되므로 질문이 보이면 실패로 하고 중지시킨다.
func (f *autoFake) Sleep(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.sleeps++
	n := f.sleeps
	f.mu.Unlock()
	if n > 100000 {
		f.t.Fatalf("Sleep 이 너무 많음(무한 루프?)")
	}
	f.w.mu.Lock()
	q := f.w.st.Question
	detail := f.w.st.Detail
	f.w.mu.Unlock()
	if q == nil {
		return
	}
	handler := f.onQ
	if q.Kind == "busy" {
		handler = f.onBusy
	} else if q.Kind != "yaml" {
		handler = nil
	}
	if handler != nil { // 사람이 답하는 질문은 시험이 답한다
		f.mu.Lock()
		if f.qFirst == nil {
			f.qFirst = map[string]time.Time{}
		}
		if _, ok := f.qFirst[q.ID]; !ok {
			f.qFirst[q.ID] = f.now
			f.qTexts = append(f.qTexts, q.Text)
		}
		if len(f.qDetail) == 0 || f.qDetail[len(f.qDetail)-1] != detail {
			f.qDetail = append(f.qDetail, detail)
		}
		f.mu.Unlock()
		handler(f, q)
		return
	}
	f.t.Errorf("-auto 에서 질문이 나옴: %s", q.Text)
	_ = os.WriteFile(filepath.Join(f.w.dir, stopReqFile), []byte("x"), 0o644)
}

func (f *autoFake) up(h *autoHost) bool { return f.now.Sub(f.start) >= h.unreachUntil }

func (f *autoFake) cpuVal(h *autoHost) string {
	if h.cpuLateAt > 0 && f.now.Sub(f.start) >= h.cpuLateAt {
		return "O"
	}
	if !h.launchAt.IsZero() && h.cpuAfter > 0 && f.now.Sub(h.launchAt) >= h.cpuAfter {
		return "O"
	}
	return h.cpu
}

func (f *autoFake) Judge() (*judgeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ev("judge", nil)
	if len(f.judgeFail) > 0 {
		fail := f.judgeFail[0]
		f.judgeFail = f.judgeFail[1:]
		if fail {
			return nil, fmt.Errorf("gossh 실행 실패")
		}
	}
	jr := &judgeResult{Count: map[string]int{}, CheckCount: map[string]int{}}
	for _, name := range f.order {
		h := f.hosts[name]
		r := hostResult{Host: name, Status: stUnreach, Check: "-", CPU: "-", Stress: "-", HT: "-", OS: "-"}
		if f.up(h) {
			r.Status, r.Check, r.CPU, r.Stress, r.HT, r.OS = stDone, h.check, f.cpuVal(h), h.stress, "ON", "4.18"
			r.VendorUnknown = h.vendorX
		}
		jr.Hosts = append(jr.Hosts, r)
	}
	return jr, nil
}

func (f *autoFake) ConfigFix(hosts []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ev("fix", hosts)
	if f.fixErrN > 0 {
		f.fixErrN--
		return fmt.Errorf("config_check 시간초과")
	}
	for _, n := range hosts {
		h := f.hosts[n]
		h.fixes++
		if h.fixOn > 0 && h.fixes >= h.fixOn {
			h.check = "O"
		}
	}
	return nil
}

func (f *autoFake) StressTest(steps, listFile string, hosts []string, end string, launched func(), registered func(yml string, hosts []string), busy func(msg string) string) (int, error) {
	b, err := os.ReadFile(listFile)
	if err != nil || strings.Join(parseHostLines(string(b)), ",") != strings.Join(hosts, ",") {
		f.t.Errorf("목록 파일 %s 내용이 대상과 다름: %q / %v", listFile, b, hosts)
	}
	f.mu.Lock()
	if len(f.failPlan) > 0 {
		fail := f.failPlan[0]
		f.failPlan = f.failPlan[1:]
		if fail {
			steps2 := "ng"
			if steps == "a" {
				steps2 = "cpu"
			}
			f.ev(steps2, hosts)
			f.mu.Unlock()
			f.w.mu.Lock()
			r := f.w.st.row(hosts[0])
			ae, tr := r.AutoErr, r.CPUTries+r.NGTries
			f.w.mu.Unlock()
			f.mu.Lock()
			f.seenErr = append(f.seenErr, ae)
			f.seenTry = append(f.seenTry, tr)
			f.mu.Unlock()
			return 255, fmt.Errorf("ssh 접속 실패")
		}
	}
	for f.busyN[steps] > 0 {
		f.ev("busy", hosts)
		f.mu.Unlock()
		ans := busy("템플릿 stress_" + steps + " 의 인벤토리 inv_user1 을 다른 사용자가 쓰는 중")
		f.mu.Lock()
		f.busyN[steps]--
		f.busyAns = append(f.busyAns, fmt.Sprintf("%s=%s@%d", steps, ans, f.mins()))
		if ans != ansRetry {
			f.mu.Unlock()
			return 2, nil
		}
	}
	yml, report := f.register(hosts) // 등록·동기화는 launch 보다 먼저(진짜 stress-test 와 같은 순서)
	n := f.regSeq
	f.mu.Unlock()
	if registered != nil && !f.noReg {
		registered(yml, report)
	}
	if f.onReg != nil {
		f.mu.Lock()
		f.onReg(f, n)
		f.mu.Unlock()
	}
	f.mu.Lock()
	if steps == "a" {
		f.ev("cpu", hosts)
		if end != "" {
			f.t.Errorf("CPU 에 -end 가 넘어감: %s", end)
		}
		for _, n := range hosts {
			f.hosts[n].launchAt = f.now
		}
		f.mu.Unlock()
		launched()
		return 0, nil
	}
	f.ev("ng", hosts)
	f.ends = append(f.ends, end)
	for _, n := range hosts {
		if f.hosts[n].ngFail {
			f.mu.Unlock()
			return 1, nil
		}
	}
	f.mu.Unlock()
	launched() // stress-ng 값은 바꾸지 않는다(launch 만으로 O 가 되는지 본다)
	return 0, nil
}

func (f *autoFake) Probe(hosts []string) (map[string]probeInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ev("probe", hosts)
	if f.onProbe != nil {
		f.onProbe(f)
	}
	if f.probeErrN > 0 {
		f.probeErrN--
		return nil, fmt.Errorf("gossh 시간초과")
	}
	res := map[string]probeInfo{}
	for _, n := range hosts {
		if h := f.hosts[n]; f.up(h) {
			res[n] = probeInfo{Btime: 1000, CPU: f.cpuVal(h), Stress: h.stress}
		}
	}
	return res, nil
}

func (f *autoFake) Loads(hosts []string) (map[string]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ev("loads", hosts)
	res := map[string]float64{}
	for _, n := range hosts {
		res[n] = f.hosts[n].load
	}
	return res, nil
}

// pick 은 사건 중 종류가 kinds 에 든 것만 공백으로 이어 준다.
func (f *autoFake) pick(kinds ...string) string {
	var out []string
	for _, e := range f.events {
		for _, k := range kinds {
			if strings.HasPrefix(e, k+":") {
				out = append(out, e)
			}
		}
	}
	return strings.Join(out, " ")
}

var autoT0 = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

// runAutoFake 는 -auto 새 상태로 워커를 끝까지 돌린다.
func runAutoFake(t *testing.T, f *autoFake, conf config) *worker {
	t.Helper()
	st := &runState{Version: stateVersion, User: "tester", Stage: stageJudge, Auto: &autoState{}}
	f.logb = &strings.Builder{}
	w := newWorker(t.TempDir(), st, f, conf, nil, f.logb)
	f.w = w
	w.run()
	return w
}

func okHost() *autoHost { return &autoHost{check: "O", cpu: "O", stress: "O"} }

// ① 판정 → ② 설정수정 → ③④ CPU 묶음(50분 판정) → ⑤ stress-ng(launch 즉시 O, 재판정해도 O) → ⑥ 10분 재판정으로 늦게 붙은 호스트 → 할 일 끝
func TestAutoHappyPath(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"b": {check: "X", fixOn: 1, cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"c": {check: "O", cpu: "O", stress: cpuNotRun},
		"d": okHost(),
		"e": {unreachUntil: 25 * time.Minute, check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
	}
	f := newAutoFake(t, autoT0, []string{"a", "b", "c", "d", "e"}, hosts)
	w := runAutoFake(t, f, testConf())
	want := "judge:@0 fix:b@0 judge:@0 ng:c@0 cpu:a,b@0 judge:@10 judge:@20 judge:@30 probe:a,b@50 ng:a,b@50 cpu:e@50 probe:e@100 ng:e@100"
	if got := strings.Join(f.events, " "); got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	if w.st.Stage != stageFinish || !strings.Contains(w.st.Detail, "할 일이 끝남") || !strings.Contains(w.st.Detail, "모두 O 5대") {
		t.Errorf("끝 상태: %d %s", w.st.Stage, w.st.Detail)
	}
	for _, h := range f.order {
		r := w.st.row(h)
		if r.Status != stDone || r.Check != "O" || r.CPU != "O" || r.Stress != "O" || r.GaveUp != "" || r.Running != "" {
			t.Errorf("%s: %+v", h, *r)
		}
	}
	if r := w.st.row("b"); r.FixTries != 1 || r.CPUTries != 1 || r.NGTries != 1 {
		t.Errorf("b 시도 횟수: fix=%d cpu=%d ng=%d", r.FixTries, r.CPUTries, r.NGTries)
	}
	eqList(t, "stress-ng 종료 시각(시작 날 +2일 00:00)", f.ends, []string{"2026,11,07,00,00", "2026,11,07,00,00", "2026,11,07,00,00"})
	if w.st.StressEnd != "2026,11,07,00,00" {
		t.Errorf("stress_end: %s", w.st.StressEnd)
	}
	if b, err := os.ReadFile(filepath.Join(w.dir, resultFile)); err != nil || !strings.Contains(string(b), "e|완료|O|O|O") {
		t.Errorf("result.txt: %v\n%s", err, b)
	}
}

// ④ 50분 뒤 O 아님 → 5분마다 30분 더(먼저 O 가 된 호스트는 바로 stress-ng) → 실패 1회로 대기열 뒤로 → 3회 실패면 포기
func TestAutoCPURecheckAndGiveUp(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 62 * time.Minute},
		"n": {check: "O", cpu: cpuNotRun, stress: cpuNotRun},
	}
	f := newAutoFake(t, autoT0, []string{"a", "n"}, hosts)
	w := runAutoFake(t, f, testConf())
	want := []string{"cpu:a,n@0", "probe:a,n@50", "probe:a,n@55", "probe:a,n@60", "probe:a,n@65", "ng:a@65", "probe:n@70", "probe:n@75", "probe:n@80"}
	for _, l := range []int{80, 160} {
		want = append(want, fmt.Sprintf("cpu:n@%d", l))
		for m := 50; m <= 80; m += 5 {
			want = append(want, fmt.Sprintf("probe:n@%d", l+m))
		}
	}
	if got := f.pick("cpu", "probe", "ng"); got != strings.Join(want, " ") {
		t.Errorf("사건\n기대: %s\n실제: %s", strings.Join(want, " "), got)
	}
	n := w.st.row("n")
	if n.GaveUp != "CPU" || n.CPUTries != 3 || !strings.Contains(n.AutoErr, "CPU 판정") || !strings.Contains(n.progressText(), "포기(CPU)") {
		t.Errorf("n: %+v", *n)
	}
	if a := w.st.row("a"); a.CPU != "O" || a.Stress != "O" || a.CPUTries != 1 {
		t.Errorf("a: %+v", *a)
	}
	if w.st.Stage != stageFinish || !strings.Contains(w.st.Detail, "포기 1대") || len(w.st.Auto.Queue) != 0 {
		t.Errorf("끝 상태: %s 대기열 %v", w.st.Detail, w.st.Auto.Queue)
	}
}

// ② 설정수정 3회 뒤에도 X → 포기(이후 CPU 대상 아님)
func TestAutoFixGiveUp(t *testing.T) {
	hosts := map[string]*autoHost{"a": okHost(), "b": {check: "X", cpu: cpuNotRun, stress: cpuNotRun}}
	f := newAutoFake(t, autoT0, []string{"a", "b"}, hosts)
	w := runAutoFake(t, f, testConf())
	if got, want := strings.Join(f.events, " "), "judge:@0 fix:b@0 judge:@0 fix:b@0 judge:@0 fix:b@0 judge:@0"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	b := w.st.row("b")
	if b.GaveUp != "설정수정" || b.FixTries != 3 || b.AutoErr != "설정체크 X" {
		t.Errorf("b: %+v", *b)
	}
	if w.st.Stage != stageFinish || !strings.Contains(w.st.Detail, "모두 O 1대, 남음 0대, 포기 1대") {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
	if b, _ := os.ReadFile(filepath.Join(w.dir, resultFile)); !strings.Contains(string(b), "포기(설정수정)") {
		t.Errorf("result.txt 에 포기 표시 없음:\n%s", b)
	}
}

// 12시간 제한(시험은 60분): 그 뒤로는 재판정·stress-ng 를 새로 시작하지 않고, 이미 launch 한 CPU 판정만 마친 뒤 끝낸다
func TestAutoLimit(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 70 * time.Minute},
		"u": {unreachUntil: 1000 * time.Hour},
	}
	f := newAutoFake(t, autoT0, []string{"a", "u"}, hosts)
	conf := testConf()
	conf.AutoLimit = 60 * time.Minute
	w := runAutoFake(t, f, conf)
	want := "judge:@0 cpu:a@0 judge:@10 judge:@20 judge:@30 judge:@40 probe:a@50 judge:@50 probe:a@55 probe:a@60 probe:a@65 probe:a@70"
	if got := strings.Join(f.events, " "); got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	a := w.st.row("a")
	if a.CPU != "O" || a.Stress != cpuNotRun || a.NGTries != 0 {
		t.Errorf("a: %+v", *a)
	}
	if w.st.Stage != stageFinish || !w.st.Auto.Ending || !strings.Contains(w.st.Detail, "제한 시간 지남") {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
	if got := f.Now().Sub(autoT0); got != 70*time.Minute {
		t.Errorf("끝난 시각: 시작 뒤 %s", got)
	}
}

// 기본값 12시간: 끝내 접속 안 되는 호스트가 있으면 10분마다 재판정하다가 12시간에 끝낸다
func TestAutoLimitDefault12h(t *testing.T) {
	hosts := map[string]*autoHost{"a": okHost(), "u": {unreachUntil: 1000 * time.Hour}}
	f := newAutoFake(t, autoT0, []string{"a", "u"}, hosts)
	w := runAutoFake(t, f, testConf())
	if got := f.Now().Sub(autoT0); got != 12*time.Hour {
		t.Errorf("끝난 시각: 시작 뒤 %s", got)
	}
	if n := strings.Count(f.pick("judge"), "judge:"); n != 72 { // 0, 10, …, 710분
		t.Errorf("재판정 %d회(기대 72)", n)
	}
	if !strings.Contains(w.st.Detail, "제한 시간 지남") || !strings.Contains(w.st.Detail, "남음 1대") {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
}

// CPU X 호스트: 부하 3 미만이면 묶음에 넣고(자동 '다시'), 부하가 높으면 이번엔 건너뛴다
func TestAutoCPUXLoad(t *testing.T) {
	hosts := map[string]*autoHost{
		"x": {check: "O", cpu: "X", stress: cpuNotRun, load: 0.5, cpuAfter: 45 * time.Minute},
		"y": {check: "O", cpu: "X", stress: cpuNotRun, load: 5},
	}
	f := newAutoFake(t, autoT0, []string{"x", "y"}, hosts)
	conf := testConf()
	conf.AutoLimit = 30 * time.Minute
	w := runAutoFake(t, f, conf)
	if got, want := f.pick("loads", "cpu", "probe"), "probe:x,y@0 loads:x,y@0 cpu:x@0 probe:x@50"; got != want { // 부하 확인 앞에서 CPU 열도 다시 읽는다(P6)
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	if x := w.st.row("x"); x.CPU != "O" || x.CPUTries != 1 {
		t.Errorf("x: %+v", *x)
	}
	if y := w.st.row("y"); y.CPU != "X" || y.CPUTries != 0 {
		t.Errorf("y: %+v", *y)
	}
	eqList(t, "끝날 때 대기열", w.st.Auto.Queue, []string{"y"})
}

// ⑤ stress-ng launch 실패 → 5분 뒤 다시, 3회면 포기
func TestAutoNGGiveUp(t *testing.T) {
	hosts := map[string]*autoHost{"c": {check: "O", cpu: "O", stress: cpuNotRun, ngFail: true}}
	f := newAutoFake(t, autoT0, []string{"c"}, hosts)
	w := runAutoFake(t, f, testConf())
	if got, want := f.pick("ng"), "ng:c@0 ng:c@5 ng:c@10"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	c := w.st.row("c")
	if c.GaveUp != "stress-ng" || c.NGTries != 3 || c.Stress != cpuNotRun || !strings.Contains(c.AutoErr, "종료코드 1") {
		t.Errorf("c: %+v", *c)
	}
	if w.st.Stage != stageFinish {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
}

// stress-ng 종료 시각: launch 날 +2일 00:00(월·연 넘어감, 윤년)
func TestAutoEndArg(t *testing.T) {
	loc := time.FixedZone("KST", 9*3600)
	for _, c := range []struct {
		now  time.Time
		want string
	}{
		{time.Date(2026, 11, 5, 9, 0, 0, 0, loc), "2026,11,07,00,00"},
		{time.Date(2026, 11, 29, 23, 59, 0, 0, loc), "2026,12,01,00,00"},
		{time.Date(2026, 11, 30, 0, 0, 0, 0, loc), "2026,12,02,00,00"},
		{time.Date(2026, 12, 30, 15, 0, 0, 0, loc), "2027,01,01,00,00"},
		{time.Date(2026, 12, 31, 1, 0, 0, 0, loc), "2027,01,02,00,00"},
		{time.Date(2027, 2, 27, 12, 0, 0, 0, loc), "2027,03,01,00,00"},
		{time.Date(2028, 2, 28, 12, 0, 0, 0, loc), "2028,03,01,00,00"},
	} {
		if got := autoEndArg(c.now); got != c.want {
			t.Errorf("%s: %s (기대 %s)", c.now, got, c.want)
		}
		vals := map[string]string{}
		for i, v := range strings.Split(autoEndArg(c.now), ",") {
			vals[endFields[i].Key] = v
		}
		if err := checkEndAll(vals, c.now); err != nil {
			t.Errorf("%s: 입력 검사 규칙에 맞지 않음: %v", c.now, err)
		}
	}
}

// 재기동: 기록된 CPU 묶음 판정 시간표와 시도 횟수를 이어 간다(다시 launch 하지 않음)
func TestAutoResumeBatch(t *testing.T) {
	hosts := map[string]*autoHost{"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute}}
	f := newAutoFake(t, autoT0, []string{"a"}, hosts)
	hosts["a"].launchAt = autoT0.Add(-20 * time.Minute)
	st := &runState{Version: stateVersion, User: "tester", Stage: stageJudge, Busy: true,
		Hosts: []hostRow{{Host: "a", Status: stDone, Check: "O", CPU: cpuNotRun, Stress: cpuNotRun, CPUTries: 1, Running: prgCPURun}},
		Auto: &autoState{Start: autoT0.Add(-25 * time.Minute), Batch: []string{"a"},
			BatchLaunch: autoT0.Add(-20 * time.Minute), BatchNext: autoT0.Add(30 * time.Minute), NextRecheck: autoT0}}
	w := newWorker(t.TempDir(), st, f, testConf(), nil, &strings.Builder{})
	f.w = w
	w.run()
	if got, want := f.pick("cpu", "probe", "ng"), "probe:a@30 ng:a@30"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	if a := w.st.row("a"); a.CPUTries != 1 || a.CPU != "O" || a.Stress != "O" {
		t.Errorf("a: %+v", *a)
	}
}

// 옛 state.json(auto 필드 없음)도 읽히고 -auto 아님으로 본다. auto 필드는 저장·읽기가 된다.
func TestAutoStateCompat(t *testing.T) {
	dir := t.TempDir()
	old := `{"version":1,"host_arg":"h","stage":3,"hosts":[{"host":"a","status":"완료","check":"O","cpu":"X","stress":"NOTRUN","excluded":false,"running":""}],"question":null}`
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := loadState(dir)
	if err != nil || st.Auto != nil || st.Hosts[0].FixTries != 0 || st.Hosts[0].GaveUp != "" {
		t.Fatalf("옛 상태: %v %+v", err, st)
	}
	st.Auto = &autoState{Start: autoT0, Queue: []string{"a"}, Yml: "x.yml", YmlHosts: []string{"a"}}
	st.Hosts[0].CPUTries, st.Hosts[0].GaveUp = 2, "CPU"
	if err := saveState(dir, st); err != nil {
		t.Fatal(err)
	}
	st2, err := loadState(dir)
	if err != nil || st2.Auto == nil || !st2.Auto.Start.Equal(autoT0) || st2.Auto.Yml != "x.yml" || st2.Hosts[0].CPUTries != 2 || st2.Hosts[0].Progress != "포기(CPU)" {
		t.Fatalf("저장 뒤: %v %+v", err, st2)
	}
	// -auto 가 아닌 상태는 auto 키를 쓰지 않는다
	st2.Auto = nil
	st2.Hosts[0].CPUTries, st2.Hosts[0].GaveUp = 0, ""
	_ = saveState(dir, st2)
	b, _ := os.ReadFile(filepath.Join(dir, stateFile))
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["auto"]; ok || strings.Contains(string(b), "cpu_tries") || strings.Contains(string(b), "gave_up") {
		t.Errorf("-auto 아닌 상태에 auto 필드가 저장됨:\n%s", b)
	}
}

func TestAutoConfKeys(t *testing.T) {
	c := parseConf("")
	if c.AutoLimit != 12*time.Hour || c.AutoRecheck != 10*time.Minute || c.AutoCPUWait != 50*time.Minute || c.AutoCPUInt != 5*time.Minute || c.AutoCPUMax != 30*time.Minute {
		t.Errorf("기본값: %+v", c)
	}
	c = parseConf("AUTO_LIMIT=30s\nAUTO_RECHECK=2s\nAUTO_CPU_WAIT=3s\nAUTO_CPU_INTERVAL=1s\nAUTO_CPU_MAX=4s\nAUTO_CPU_MAX=x\n")
	if c.AutoLimit != 30*time.Second || c.AutoRecheck != 2*time.Second || c.AutoCPUWait != 3*time.Second || c.AutoCPUInt != time.Second || c.AutoCPUMax != 4*time.Second {
		t.Errorf("설정값: %+v", c)
	}
	if len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "AUTO_CPU_MAX") {
		t.Errorf("경고: %v", c.Warnings)
	}
}

// -run -auto: 새 작업은 auto 상태로 만들고, 이미 있는 -auto 아닌 작업에는 붙이지 않는다(안내만)
func TestRunEntryAuto(t *testing.T) {
	h := newHarness(t)
	d := h.deps()
	d.auto = true
	if code := runEntry("hosts.txt", "", "", "u1", d); code != 0 {
		t.Fatalf("code=%d err=%s", code, h.errBuf.String())
	}
	st, err := loadState(filepath.Join(h.dir, "hosts.txt_run"))
	if err != nil || st.Auto == nil {
		t.Fatalf("auto 상태 없음: %v %+v", err, st)
	}

	h2 := newHarness(t)
	h2.writeState(stageFix, false)
	h2.alive = true
	d2 := h2.deps()
	d2.auto = true
	if code := runEntry("hosts.txt", "", "", "", d2); code != 0 || h2.started != 0 {
		t.Fatalf("code=%d started=%d", code, h2.started)
	}
	if !strings.Contains(h2.errBuf.String(), "-auto 를 무시") {
		t.Errorf("안내 없음: %s", h2.errBuf.String())
	}
	st2, _ := loadState(filepath.Join(h2.dir, "hosts.txt_run"))
	if st2.Auto != nil {
		t.Errorf("-auto 아닌 작업이 auto 로 바뀜")
	}
}
