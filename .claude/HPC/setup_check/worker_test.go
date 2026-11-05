package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 가짜 실행기: 호스트 상태를 메모리에 두고 가짜 시각으로 움직인다.
// ---------------------------------------------------------------------------

type fakeHost struct {
	status, check, cpu, stress string
	fixable                    bool          // 설정수정하면 설정체크 O
	cpuNever                   bool          // CPU 가 끝내 O 가 안 됨
	cpuReady                   time.Duration // launch 뒤 이 시간이 지나면 CPU O
	rebootAt                   time.Duration // >0 이면 launch 뒤 이 시각부터 btime 이 바뀜
	offFrom, offTo             time.Duration // launch 기준 [offFrom, offTo) 동안 응답 없음(offTo 0 = 끝까지)
	noStress                   bool          // stress-test b 를 해도 stress-ng O 가 안 됨
	launched                   bool
}

type fakeExec struct {
	t     *testing.T
	mu    sync.Mutex
	now   time.Time
	order []string
	world map[string]*fakeHost
	w     *worker

	launchAt  time.Time
	answers   []string
	asked     []string // "단계:선택지들=답"
	events    []string
	probeRel  []time.Duration
	fixCalls  int
	judgeErr  []error // 앞에서부터 Judge 호출마다 꺼내 씀
	cpuErr    []error // 앞에서부터 CPU stress-test 호출마다 꺼내 씀
	cpuExit   int
	ngExit    int
	ngErr     error
	cpuNoLnch bool
	sleeps    int
	blockA    chan struct{} // nil 아니면 CPU stress-test 가 시작 때 이 채널을 닫고 취소될 때까지 멈춘다
	stopOnAsk bool          // 질문이 뜨면 답 대신 stop.req 를 쓴다
}

func newFake(t *testing.T, order []string, world map[string]*fakeHost, answers ...string) *fakeExec {
	return &fakeExec{t: t, now: time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC), order: order, world: world, answers: answers}
}

func (f *fakeExec) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Sleep 은 시각만 넘기고, 질문이 떠 있으면 다음 답을 answer.json 에 쓴다.
func (f *fakeExec) Sleep(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.sleeps++
	n := f.sleeps
	f.mu.Unlock()
	if n > 200000 {
		f.t.Fatalf("Sleep 이 너무 많음(무한 루프?)")
	}
	q := f.w.st.Question
	if q == nil {
		return
	}
	if f.stopOnAsk {
		_ = os.WriteFile(filepath.Join(f.w.dir, stopReqFile), []byte("x"), 0o644)
		return
	}
	if len(f.answers) == 0 {
		f.t.Fatalf("답이 남아 있지 않은 질문: [%d] %s", q.Stage, q.Text)
	}
	a := f.answers[0]
	f.answers = f.answers[1:]
	var keys []string
	for _, c := range q.Choices {
		keys = append(keys, c.Key)
	}
	f.asked = append(f.asked, stageName(q.Stage)+":"+strings.Join(keys, "")+"="+a)
	if err := writeAnswer(f.w.dir, q.ID, a); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeExec) rel() time.Duration { return f.now.Sub(f.launchAt) }

// cpuVal 은 지금 시각 기준 CPU 열 값(잠금 안에서 부름).
func (f *fakeExec) cpuVal(h *fakeHost) string {
	if h.launched && !h.cpuNever && f.rel() >= h.cpuReady {
		return "O"
	}
	return h.cpu
}

func (f *fakeExec) off(h *fakeHost) bool {
	if !h.launched || (h.offFrom == 0 && h.offTo == 0) {
		return false
	}
	r := f.rel()
	return r >= h.offFrom && (h.offTo == 0 || r < h.offTo)
}

func (f *fakeExec) Judge() (*judgeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, "judge")
	if len(f.judgeErr) > 0 {
		e := f.judgeErr[0]
		f.judgeErr = f.judgeErr[1:]
		if e != nil {
			return nil, e
		}
	}
	jr := &judgeResult{Count: map[string]int{}, CheckCount: map[string]int{}}
	for _, name := range f.order {
		h := f.world[name]
		r := hostResult{Host: name, Status: h.status, Check: "-", CPU: "-", Stress: "-", HT: "-", OS: "-"}
		if h.status == stDone {
			r.Check, r.CPU, r.Stress, r.HT, r.OS = h.check, f.cpuVal(h), h.stress, "ON", "4.18"
		}
		jr.Hosts = append(jr.Hosts, r)
	}
	return jr, nil
}

func (f *fakeExec) ConfigFix(hosts []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fixCalls++
	f.events = append(f.events, "fix:"+strings.Join(hosts, ","))
	for _, h := range hosts {
		if f.world[h].fixable {
			f.world[h].check = "O"
		}
	}
	return nil
}

func (f *fakeExec) StressTest(steps, listFile string, hosts []string, launched func()) (int, error) {
	b, err := os.ReadFile(listFile)
	if err != nil || strings.Join(parseHostLines(string(b)), ",") != strings.Join(hosts, ",") {
		f.t.Errorf("목록 파일 %s 내용이 대상과 다름: %q / %v", listFile, b, hosts)
	}
	f.mu.Lock()
	f.events = append(f.events, "start:"+steps+":"+strings.Join(hosts, ","))
	if steps == "a" {
		if f.blockA != nil {
			f.mu.Unlock()
			close(f.blockA)
			select {
			case <-f.w.stopCh:
				return -1, errCanceled
			case <-time.After(10 * time.Second):
				f.t.Error("취소되지 않음")
				return -1, errTimeout
			}
		}
		if len(f.cpuErr) > 0 {
			e := f.cpuErr[0]
			f.cpuErr = f.cpuErr[1:]
			if e != nil {
				f.mu.Unlock()
				return -1, e
			}
		}
		if f.cpuNoLnch {
			f.mu.Unlock()
			return f.cpuExit, nil
		}
		f.mu.Unlock()
		time.Sleep(30 * time.Millisecond) // 등록·launch 가 오래 걸려도 b 가 먼저 시작하지 않는지 본다
		f.mu.Lock()
		f.launchAt = f.now
		for _, h := range hosts {
			f.world[h].launched = true
		}
		f.events = append(f.events, "launched:a")
		f.mu.Unlock()
		launched()
		return f.cpuExit, nil
	}
	defer f.mu.Unlock()
	if f.ngErr != nil {
		return -1, f.ngErr
	}
	for _, h := range hosts {
		if !f.world[h].noStress {
			f.world[h].stress = "O"
		}
	}
	f.events = append(f.events, "end:b")
	return f.ngExit, nil
}

func (f *fakeExec) Probe(hosts []string) (map[string]probeInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probeRel = append(f.probeRel, f.rel())
	res := map[string]probeInfo{}
	for _, name := range hosts {
		h := f.world[name]
		if f.off(h) {
			continue
		}
		bt := int64(1000)
		if h.rebootAt > 0 && h.launched && f.rel() >= h.rebootAt {
			bt = 5000
		}
		res[name] = probeInfo{Btime: bt + int64(len(f.probeRel)%2), CPU: f.cpuVal(h), Stress: h.stress}
	}
	return res, nil
}

func (f *fakeExec) idx(prefix string) int {
	for i, e := range f.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

func (f *fakeExec) count(prefix string) int {
	n := 0
	for _, e := range f.events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func testConf() config {
	c := parseConf("")
	return c
}

// runFake 는 새 상태로 워커를 끝까지 돌린다.
func runFake(t *testing.T, f *fakeExec, st *runState) *worker {
	t.Helper()
	dir := t.TempDir()
	if st == nil {
		st = &runState{Version: stateVersion, User: "tester", Stage: stageJudge}
	}
	w := newWorker(dir, st, f, testConf(), map[string]string{"a": "팬 소음"}, &strings.Builder{})
	f.w = w
	w.run()
	return w
}

func row(t *testing.T, w *worker, h string) *hostRow {
	t.Helper()
	r := w.st.row(h)
	if r == nil {
		t.Fatalf("행 없음: %s", h)
	}
	return r
}

func eqList(t *testing.T, name string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: got %v want %v", name, got, want)
	}
}

// ---------------------------------------------------------------------------
// 시험
// ---------------------------------------------------------------------------

// 1→2→3→4→5 전체 흐름 + 등록 순서(CPU launch 뒤 stress-ng 시작)
func TestWorkerHappyPath(t *testing.T) {
	world := map[string]*fakeHost{
		"a": {status: stDone, check: "X", fixable: true, cpu: cpuNotRun, stress: cpuNotRun, cpuReady: 50 * time.Minute},
		"b": {status: stDone, check: "O", cpu: "O", stress: cpuNotRun},
		"c": {status: stUnreach},
	}
	f := newFake(t, []string{"a", "b", "c"}, world, "c", "c", "c", "c")
	w := runFake(t, f, nil)

	eqList(t, "질문", f.asked, []string{"판정:crxs=c", "설정수정:crs=c", "부하:crs=c", "stress-ng:crs=c"})
	if w.st.Stage != stageFinish || w.st.Stopped {
		t.Fatalf("완료가 아님: stage=%d stopped=%v", w.st.Stage, w.st.Stopped)
	}
	for _, h := range []string{"a", "b"} {
		r := row(t, w, h)
		if r.Check != "O" || r.CPU != "O" || r.Stress != "O" || r.Progress != "" {
			t.Errorf("%s: %+v", h, *r)
		}
	}
	if !row(t, w, "a").CPUTarget || row(t, w, "b").CPUTarget {
		t.Errorf("CPUTarget 표시 오류")
	}
	// 등록 순서
	la, sb := f.idx("launched:a"), f.idx("start:b:b")
	if la < 0 || sb < 0 || sb < la {
		t.Errorf("stress-ng 가 CPU launch 전에 시작함: %v", f.events)
	}
	if f.idx("start:b:a") < 0 {
		t.Errorf("stress-ng 단계가 a 에 돌지 않음: %v", f.events)
	}
	// 47분 전에는 CPU 열을 보지 않고, 47분부터 2분 간격 → 51분에 O
	if got := f.probeRel[len(f.probeRel)-1]; got != 51*time.Minute {
		t.Errorf("마지막 CPU 확인 %v (51m 기대), 전체 %v", got, f.probeRel)
	}
	// 결과 파일
	b, err := os.ReadFile(filepath.Join(w.dir, resultFile))
	if err != nil {
		t.Fatal(err)
	}
	want := header + "\n" +
		"a|완료|O|O|O|ON|4.18|팬 소음\n" +
		"b|완료|O|O|O|ON|4.18|\n" +
		"c|접속불가|-|-|-|-|-|\n"
	if string(b) != want {
		t.Errorf("result.txt:\n%s\nwant:\n%s", b, want)
	}
	// state.json 이 다시 읽히는지
	st, err := loadState(w.dir)
	if err != nil || st.Stage != stageFinish || len(st.Hosts) != 3 {
		t.Errorf("state.json 읽기: %v %+v", err, st)
	}
}

// r(재시도) 와 x(제외하고 계속)
func TestWorkerRetryAndExclude(t *testing.T) {
	world := map[string]*fakeHost{
		"a": {status: stDone, check: noOutput, cpu: cpuNotRun, stress: cpuNotRun}, // 고쳐지지 않음
		"b": {status: stDone, check: "O", cpu: cpuNotRun, stress: cpuNotRun},
	}
	f := newFake(t, []string{"a", "b"}, world, "c", "r", "x", "c", "c")
	w := runFake(t, f, nil)

	eqList(t, "질문", f.asked, []string{"판정:crxs=c", "설정수정:crxs=r", "설정수정:crxs=x", "부하:crs=c", "stress-ng:crs=c"})
	if f.fixCalls != 2 {
		t.Errorf("설정수정 %d번(2 기대)", f.fixCalls)
	}
	a := row(t, w, "a")
	if !a.Excluded || a.Progress != prgExcluded || a.CPUTarget {
		t.Errorf("a: %+v", *a)
	}
	if f.idx("start:a:b") < 0 || f.count("start:a") != 1 {
		t.Errorf("CPU 는 b 만: %v", f.events)
	}
	b, _ := os.ReadFile(filepath.Join(w.dir, resultFile))
	if !strings.Contains(string(b), "a|완료|출력없음|NOTRUN|NOTRUN|ON|4.18|팬 소음, 제외\n") {
		t.Errorf("result.txt 제외 표시:\n%s", b)
	}
}

// CPU·stress-ng 대상 선정(겹침 제외, 제외·미완료 호스트 빠짐)
func TestWorkerTargets(t *testing.T) {
	w := newWorker(t.TempDir(), &runState{Hosts: []hostRow{
		{Host: "n1", Status: stDone, Check: "O", CPU: cpuNotRun, Stress: cpuNotRun},
		{Host: "n2", Status: stDone, Check: "O", CPU: "O", Stress: cpuNotRun},
		{Host: "n3", Status: stDone, Check: "O", CPU: "O", Stress: "O"},
		{Host: "n4", Status: stDone, Check: "X", CPU: cpuNotRun, Stress: cpuNotRun},
		{Host: "n5", Status: stDone, Check: "O", CPU: cpuNotRun, Stress: cpuNotRun, Excluded: true},
		{Host: "n6", Status: stInstall, Check: "-", CPU: "-", Stress: "-"},
		{Host: "n7", Status: stDone, Check: "O", CPU: "X", Stress: cpuNotRun},
		{Host: "n8", Status: stDone, Check: "O", CPU: "O", Stress: cpuNotRun, CPUTarget: true},
		{Host: "n9", Status: stDone, Check: noOutput, CPU: "O", Stress: cpuNotRun},
	}}, newFake(t, nil, nil), testConf(), nil, &strings.Builder{})
	cpu := w.cpuTargets()
	eqList(t, "cpu", cpu, []string{"n1"})
	eqList(t, "stress", w.stressTargets(cpu), []string{"n2", "n8"})
	eqList(t, "stress(겹침)", w.stressTargets([]string{"n2"}), []string{"n8"})
	eqList(t, "fix", w.fixTargets(), []string{"n4", "n9"})
	eqList(t, "ng", w.ngTargets(), []string{"n8"})
	if n := w.nextStage(stageJudge); n != stageFix {
		t.Errorf("next(1)=%d", n)
	}
	if n := w.nextStage(stageFix); n != stageLoad {
		t.Errorf("next(2)=%d", n)
	}
}

// 재부팅·응답없음 표시, CPU 확인 시각(47분 시작, 2분 간격, 30분 마감)
func TestWorkerRebootNoRespDeadline(t *testing.T) {
	world := map[string]*fakeHost{
		"a": {status: stDone, check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuReady: 50 * time.Minute,
			rebootAt: 10 * time.Minute, offFrom: 20 * time.Minute, offTo: 25 * time.Minute},
		"b": {status: stDone, check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuNever: true, offFrom: 30 * time.Minute},
	}
	f := newFake(t, []string{"a", "b"}, world, "c", "s")
	w := runFake(t, f, nil)

	eqList(t, "질문", f.asked, []string{"판정:crs=c", "부하:crxs=s"})
	a, b := row(t, w, "a"), row(t, w, "b")
	if !a.Rebooted || a.NoResp || a.CPU != "O" {
		t.Errorf("a: %+v", *a)
	}
	if a.Progress != prgRebooted {
		t.Errorf("a 진행비고 %q", a.Progress)
	}
	if b.Rebooted || !b.NoResp || b.CPU != cpuNotRun || b.Progress != prgNoResp {
		t.Errorf("b: %+v", *b)
	}
	if !w.st.Stopped || !w.st.CPUWaitDone {
		t.Errorf("중지·감시끝 아님: %+v", w.st)
	}
	// launch 직후 btime 기록(0) → 2,4,…,46 → 47 → 49,…,75 → 77(마감)
	var want []time.Duration
	want = append(want, 0)
	for m := 2; m <= 46; m += 2 {
		want = append(want, time.Duration(m)*time.Minute)
	}
	for m := 47; m <= 77; m += 2 {
		want = append(want, time.Duration(m)*time.Minute)
	}
	if len(f.probeRel) != len(want) {
		t.Fatalf("확인 시각 %v\nwant %v", f.probeRel, want)
	}
	for i := range want {
		if f.probeRel[i] != want[i] {
			t.Fatalf("확인 시각 %v\nwant %v", f.probeRel, want)
		}
	}
	if _, err := os.Stat(filepath.Join(w.dir, resultFile)); err != nil {
		t.Errorf("중지해도 result.txt 저장: %v", err)
	}
}

// stress-test 시간초과 → r/s 질문, stress-ng 는 띄우지 않음, r 이면 다시 실행
func TestWorkerStressTimeout(t *testing.T) {
	world := map[string]*fakeHost{
		"a": {status: stDone, check: "O", cpu: cpuNotRun, stress: cpuNotRun},
		"b": {status: stDone, check: "O", cpu: "O", stress: cpuNotRun},
	}
	f := newFake(t, []string{"a", "b"}, world, "c", "r", "s")
	f.cpuErr = []error{errTimeout}
	w := runFake(t, f, nil)

	eqList(t, "질문", f.asked, []string{"판정:crs=c", "부하:rs=r", "부하:crs=s"})
	if f.count("start:a") != 2 {
		t.Errorf("CPU 두 번 기대: %v", f.events)
	}
	// 첫 회차(시간초과)에서는 stress-ng 를 띄우지 않았어야 함: 첫 start:b 는 두 번째 launch 뒤
	if f.idx("start:b") < f.idx("launched:a") {
		t.Errorf("시간초과 회차에 stress-ng 시작: %v", f.events)
	}
	if !strings.Contains(w.st.LastError, "시간초과") {
		t.Errorf("last_error=%q", w.st.LastError)
	}
}

// stress-ng 쪽 종료코드 2 → r/s, 종료코드 1 → 안내만
func TestWorkerStressExitCodes(t *testing.T) {
	world := map[string]*fakeHost{
		"b": {status: stDone, check: "O", cpu: "O", stress: cpuNotRun, noStress: true},
	}
	f := newFake(t, []string{"b"}, world, "c", "s")
	f.ngExit = 2
	w := runFake(t, f, nil)
	eqList(t, "질문(2)", f.asked, []string{"판정:crs=c", "부하:rs=s"})
	if !strings.Contains(w.st.LastError, "종료코드 2") {
		t.Errorf("last_error=%q", w.st.LastError)
	}

	world["b"].stress = cpuNotRun
	f = newFake(t, []string{"b"}, world, "c", "s")
	f.ngExit = 1
	runFake(t, f, nil)
	eqList(t, "질문(1)", f.asked, []string{"판정:crs=c", "부하:crxs=s"})
}

// CPU 가 launch 되지 않고 끝나면(묶음 모두 실패) 감시 없이 r/s
func TestWorkerCPUNotLaunched(t *testing.T) {
	world := map[string]*fakeHost{"a": {status: stDone, check: "O", cpu: cpuNotRun, stress: cpuNotRun}}
	f := newFake(t, []string{"a"}, world, "c", "s")
	f.cpuNoLnch, f.cpuExit = true, 1
	w := runFake(t, f, nil)
	eqList(t, "질문", f.asked, []string{"판정:crs=c", "부하:rs=s"})
	if len(f.probeRel) != 0 || !strings.Contains(w.st.LastError, "launch") {
		t.Errorf("probe=%v last_error=%q", f.probeRel, w.st.LastError)
	}
}

// 판정 실패 → r/s, r 이면 다시 판정
func TestWorkerJudgeError(t *testing.T) {
	world := map[string]*fakeHost{"a": {status: stDone, check: "O", cpu: "O", stress: "O"}}
	f := newFake(t, []string{"a"}, world, "r", "c")
	f.judgeErr = []error{errors.New("gossh 실행 실패")}
	w := runFake(t, f, nil)
	eqList(t, "질문", f.asked, []string{"판정:rs=r", "판정:crs=c"})
	if w.st.Stage != stageFinish {
		t.Errorf("stage=%d", w.st.Stage)
	}
}

// 재기동: 부하 단계 CPU 감시 중이던 상태 → 다시 launch 하지 않고 감시를 이어 간 뒤 질문
func TestWorkerResumeLoad(t *testing.T) {
	world := map[string]*fakeHost{
		"a": {status: stDone, check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuReady: 50 * time.Minute, launched: true},
	}
	f := newFake(t, []string{"a"}, world, "s")
	launch := f.now.Add(-40 * time.Minute)
	f.launchAt = launch
	conf := testConf()
	st := &runState{Version: stateVersion, User: "tester", Stage: stageLoad, Busy: true,
		CPUList: []string{"a"}, CPULaunch: launch, CheckStart: launch.Add(conf.CPUWaitStart),
		Deadline: launch.Add(conf.CPUWaitStart + conf.CPUWaitMax), NextCheck: launch.Add(40 * time.Minute),
		Hosts: []hostRow{{Host: "a", Status: stDone, Check: "O", CPU: cpuNotRun, Stress: cpuNotRun, CPUTarget: true, Btime: 1000, Running: prgCPURun}}}
	w := runFake(t, f, st)

	if f.count("start:") != 0 {
		t.Errorf("재기동에서 stress-test 를 다시 실행함: %v", f.events)
	}
	eqList(t, "질문", f.asked, []string{"부하:crs=s"})
	// 40(밀린 확인) → 42 … 46 → 47 → 49 → 51(O)
	if got := f.probeRel; len(got) == 0 || got[0] != 40*time.Minute || got[len(got)-1] != 51*time.Minute {
		t.Errorf("확인 시각 %v", got)
	}
	if r := row(t, w, "a"); r.CPU != "O" || r.Running != "" || r.Rebooted {
		t.Errorf("a: %+v", *r)
	}
}

// 재기동: 설정수정 중이던 상태 → config_check 를 다시 돌리지 않고 재체크 후 같은 단계 질문
func TestWorkerResumeFix(t *testing.T) {
	world := map[string]*fakeHost{"a": {status: stDone, check: "X", fixable: true, cpu: "O", stress: "O"}}
	f := newFake(t, []string{"a"}, world, "s")
	st := &runState{Version: stateVersion, User: "tester", Stage: stageFix, Busy: true,
		Hosts: []hostRow{{Host: "a", Status: stDone, Check: "X", CPU: "O", Stress: "O"}}}
	runFake(t, f, st)
	if f.fixCalls != 0 || f.idx("judge") != 0 {
		t.Errorf("재기동에서 설정수정을 다시 함: %v", f.events)
	}
	eqList(t, "질문", f.asked, []string{"설정수정:crxs=s"})
}

// 다른 id·허용되지 않은 답은 무시
func TestWorkerIgnoresWrongAnswer(t *testing.T) {
	world := map[string]*fakeHost{"a": {status: stDone, check: "O", cpu: "O", stress: "O"}}
	f := newFake(t, []string{"a"}, world, "x", "c") // 첫 질문에는 x 가 없음 → 무시 후 c
	w := runFake(t, f, nil)
	if w.st.Stage != stageFinish || len(f.asked) != 2 {
		t.Errorf("stage=%d asked=%v", w.st.Stage, f.asked)
	}
}

func TestLineWatch(t *testing.T) {
	var out strings.Builder
	n := 0
	lw := &lineWatch{w: &out, mark: []byte(stressMonitorMark), fn: func() { n++ }}
	lw.Write([]byte("[i] cpu(intel) 묶음 2대 등록 시작\n[감시] cpu 경"))
	if n != 0 {
		t.Fatal("줄이 끝나기 전에 호출됨")
	}
	lw.Write([]byte("과 10/120초\n[감시] cpu 경과 20/120초\n"))
	if n != 1 || !strings.Contains(out.String(), "20/120") {
		t.Errorf("n=%d out=%q", n, out.String())
	}
}

func TestParseProbeOutput(t *testing.T) {
	out := "a: btime 1730000000\na: vendor_id\t: GenuineIntel\na: " + mkCPUIntel + "\na: " + mkStress + "\n" +
		"b: btime 1730000100\nb: vendor_id\t: AuthenticAMD\nb: " + mkCPUAMD + mkNoLogSfx + "\n" +
		"c: vendor_id\t: GenuineIntel\n"
	got := parseProbeOutput(out)
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if p := got["a"]; p.Btime != 1730000000 || p.CPU != "O" || p.Stress != "O" {
		t.Errorf("a: %+v", p)
	}
	if p := got["b"]; p.CPU != cpuNotRun || p.Stress != cpuNotRun {
		t.Errorf("b: %+v", p)
	}
	if isDangerousCommand(probeCmd(testConf())) {
		t.Errorf("확인 명령이 위험어에 걸림")
	}
	if c := probeCmd(testConf()); strings.ContainsAny(strings.Split(c, "'")[0], "{}!$") {
		t.Errorf("csh 에 안전하지 않은 확인 명령 앞부분: %s", c)
	}
}

func TestStateFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "h_run")
	st := &runState{Version: stateVersion, Stage: stageLoad, User: "u",
		Hosts:    []hostRow{{Host: "a", Excluded: true, Rebooted: true, Running: prgCPURun}},
		Question: &question{ID: "q3", Stage: stageLoad, Text: "t", Choices: []choice{{Key: "c", Label: "계속"}}}}
	if err := saveState(dir, st); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(dir)
	if err != nil || got.Question == nil || got.Question.ID != "q3" || got.Hosts[0].Progress != "제외, 실행도중 재부팅됨, CPU 실행중" {
		t.Fatalf("%v %+v", err, got)
	}
	if takeAnswer(dir) != nil {
		t.Error("답이 없어야 함")
	}
	if err := writeAnswer(dir, "q3", "c"); err != nil {
		t.Fatal(err)
	}
	if a := takeAnswer(dir); a == nil || a.ID != "q3" || a.Choice != "c" {
		t.Errorf("answer=%+v", a)
	}
	if _, err := os.Stat(filepath.Join(dir, answerFile)); !os.IsNotExist(err) {
		t.Error("읽은 답은 지워야 함")
	}
	if d := stateDirFor("^sub/hosts.txt"); filepath.Base(d) != "hosts.txt_run" || !filepath.IsAbs(d) {
		t.Errorf("stateDirFor=%s", d)
	}
	ns, err := newRunState("^hosts", "", "er.txt", "u")
	if err != nil || ns.Stage != stageJudge || !filepath.IsAbs(ns.HostFile) || ns.ConfPath != "" || !filepath.IsAbs(ns.ErrPath) {
		t.Errorf("newRunState %+v %v", ns, err)
	}
}

func TestLockState(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	dir := t.TempDir()
	if workerAlive(dir) {
		t.Fatal("lock 파일이 없으면 false")
	}
	rel, err := lockState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !workerAlive(dir) {
		t.Error("lock 중이면 true")
	}
	if _, err := lockState(dir); err != errLocked {
		t.Errorf("두 번째 lock: %v", err)
	}
	rel()
	if workerAlive(dir) {
		t.Error("풀린 뒤 false")
	}
}

// 중지 요청: CPU stress-test 실행 도중 stop.req → 명령 취소, 재체크·질문 없이 중지, result.txt 작성
func TestWorkerStopMidCommand(t *testing.T) {
	old := stopPoll
	stopPoll = 5 * time.Millisecond
	defer func() { stopPoll = old }()
	world := map[string]*fakeHost{"a": {status: stDone, check: "O", cpu: cpuNotRun, stress: "O"}}
	f := newFake(t, []string{"a"}, world, "c")
	f.blockA = make(chan struct{})
	dir := t.TempDir()
	st := &runState{Version: stateVersion, User: "tester", Stage: stageJudge}
	w := newWorker(dir, st, f, testConf(), nil, &strings.Builder{})
	f.w = w
	go func() {
		<-f.blockA
		_ = writeStopReq(dir)
	}()
	w.run()

	if !w.st.Stopped || !strings.Contains(w.st.Detail, "중지") {
		t.Errorf("중지 안 됨: stopped=%v detail=%q", w.st.Stopped, w.st.Detail)
	}
	if f.count("launched") != 0 || f.count("judge") != 1 {
		t.Errorf("중지 뒤 재체크·launch 가 있음: %v", f.events)
	}
	if r := row(t, w, "a"); r.Running != "" {
		t.Errorf("Running 이 남음: %+v", *r)
	}
	if _, err := os.Stat(filepath.Join(dir, resultFile)); err != nil {
		t.Errorf("result.txt 없음: %v", err)
	}
	if stopRequested(dir) {
		t.Error("stop.req 는 지워야 함")
	}
	if st2, err := loadState(dir); err != nil || !st2.Stopped {
		t.Errorf("state.json: %v %+v", err, st2)
	}
}

// 중지 요청: 시작 전에 이미 있으면 아무것도 실행하지 않고 중지
func TestWorkerStopBeforeStart(t *testing.T) {
	world := map[string]*fakeHost{"a": {status: stDone, check: "O", cpu: "O", stress: "O"}}
	f := newFake(t, []string{"a"}, world)
	dir := t.TempDir()
	_ = writeStopReq(dir)
	w := newWorker(dir, &runState{Version: stateVersion, User: "t", Stage: stageJudge}, f, testConf(), nil, &strings.Builder{})
	f.w = w
	w.run()
	if !w.st.Stopped || f.count("judge") != 0 {
		t.Errorf("stopped=%v events=%v", w.st.Stopped, f.events)
	}
}

// 중지 요청: 질문 대기 중에도 받는다
func TestWorkerStopWhileAsking(t *testing.T) {
	world := map[string]*fakeHost{"a": {status: stDone, check: "O", cpu: "O", stress: "O"}}
	f := newFake(t, []string{"a"}, world)
	f.stopOnAsk = true
	w := runFake(t, f, nil)
	if !w.st.Stopped || w.st.Question != nil {
		t.Errorf("stopped=%v question=%v", w.st.Stopped, w.st.Question)
	}
}

// 설정체크를 안 하는 설정(OS_CHECK_SH 비어 있음): 설정체크 '-' 는 통과 — 설정수정 단계를 건너뛰고 바로 부하
func TestWorkerCheckDisabled(t *testing.T) {
	world := map[string]*fakeHost{
		"a": {status: stDone, check: "-", cpu: cpuNotRun, stress: cpuNotRun, cpuReady: 50 * time.Minute},
		"b": {status: stDone, check: "-", cpu: "O", stress: cpuNotRun},
	}
	f := newFake(t, []string{"a", "b"}, world, "c", "c", "c")
	w := runFake(t, f, nil)
	if f.fixCalls != 0 {
		t.Errorf("설정수정을 함: %v", f.events)
	}
	eqList(t, "질문", f.asked, []string{"판정:crs=c", "부하:crs=c", "stress-ng:crs=c"})
	if w.st.Stage != stageFinish || row(t, w, "a").CPU != "O" || row(t, w, "b").Stress != "O" {
		t.Errorf("stage=%d a=%+v b=%+v", w.st.Stage, *row(t, w, "a"), *row(t, w, "b"))
	}
}

// 설정체크를 하는 설정(OS_CHECK_SH 있음)에서 '-' 는 체크 실행 실패이므로 통과가 아니다:
// 설정수정 대상이고, x(제외) 목록에 들며, CPU 대상이 아니다
func TestWorkerCheckDashWithOSCheck(t *testing.T) {
	conf := testConf()
	conf.OSCheckSh = "/x/config_check.sh"
	w := newWorker(t.TempDir(), &runState{Hosts: []hostRow{
		{Host: "n1", Status: stDone, Check: "-", CPU: cpuNotRun, Stress: cpuNotRun},
		{Host: "n2", Status: stDone, Check: "O", CPU: cpuNotRun, Stress: cpuNotRun},
	}}, newFake(t, nil, nil), conf, nil, &strings.Builder{})
	eqList(t, "cpu", w.cpuTargets(), []string{"n2"})
	eqList(t, "fix", w.fixTargets(), []string{"n1"})
	eqList(t, "notOK", w.notOK(stageJudge), []string{"n1"})

	// 흐름: '-' → 설정수정 단계 → 재체크 O
	world := map[string]*fakeHost{"a": {status: stDone, check: "-", fixable: true, cpu: "O", stress: "O"}}
	f := newFake(t, []string{"a"}, world, "c", "c", "c")
	dir := t.TempDir()
	ww := newWorker(dir, &runState{Version: stateVersion, User: "t", Stage: stageJudge}, f, conf, nil, &strings.Builder{})
	f.w = ww
	ww.run()
	if f.fixCalls != 1 || ww.st.CheckOff {
		t.Errorf("fixCalls=%d checkOff=%v events=%v", f.fixCalls, ww.st.CheckOff, f.events)
	}
	eqList(t, "질문", f.asked, []string{"판정:crxs=c", "설정수정:crs=c"})
	if r := row(t, ww, "a"); r.Check != "O" || ww.st.Stage != stageFinish {
		t.Errorf("a=%+v stage=%d", *r, ww.st.Stage)
	}
}
