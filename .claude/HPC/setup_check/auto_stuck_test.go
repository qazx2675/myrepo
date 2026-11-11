package main

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// 조용히 남는 호스트(계획서_auto_막힘.md 4.4, P4~P7) 시험.

// autoExclFake 는 첫 launch 때 [제외] 줄로 호스트를 빼는 stress-test 를 흉내 낸다(eventStresser).
// excl[steps] 의 호스트는 그 단계의 첫 launch 에서만 빠진다(launch 되지 않았으므로 launchAt 도 되돌린다).
type autoExclFake struct {
	*autoFake
	excl map[string][]string
}

func (f *autoExclFake) StressTestEv(steps, listFile string, hosts []string, end string, launched func(), registered func(yml string, hosts []string), busy func(msg string) string, ev func(stressEvent)) (int, error) {
	f.mu.Lock()
	out := f.excl[steps]
	delete(f.excl, steps)
	f.mu.Unlock()
	if len(out) > 0 && ev != nil {
		ev(stressEvent{Kind: stressExcludedMark, Stage: steps, Hosts: out, Reason: "접속불가"})
	}
	wrapped := func() {
		f.mu.Lock()
		for _, h := range out {
			f.hosts[h].launchAt = time.Time{}
		}
		f.mu.Unlock()
		launched()
	}
	return f.autoFake.StressTest(steps, listFile, hosts, end, wrapped, registered, busy)
}

// P4 CPU 벤더 미인식: 판정 때 바로 포기(CPU launch 0회, 부하 확인 안 함), 다른 호스트는 그대로 진행한다.
func TestAutoVendorUnknownGivesUpAtJudge(t *testing.T) {
	hosts := map[string]*autoHost{
		"v": {check: "O", cpu: "X", stress: cpuNotRun, vendorX: true, load: 0.5},
		"b": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
	}
	f := newAutoFake(t, autoT0, []string{"v", "b"}, hosts)
	w := runAutoFake(t, f, testConf())
	v := w.st.row("v")
	if v.GaveUp != "CPU" || v.AutoErr != "CPU 벤더 미인식" || v.CPUTries != 0 || !strings.Contains(v.progressText(), "포기(CPU)") {
		t.Errorf("v: %+v", *v)
	}
	if got, want := f.pick("loads", "cpu", "probe", "ng"), "cpu:b@0 probe:b@50 ng:b@50"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	if b := w.st.row("b"); b.CPU != "O" || b.Stress != "O" || b.GaveUp != "" {
		t.Errorf("b: %+v", *b)
	}
	if !strings.Contains(f.logb.String(), "CPU 벤더가 Intel/AMD 가 아니어서 CPU 테스트 없이 포기 1대: v") {
		t.Errorf("로그에 벤더 포기 줄이 없음:\n%s", f.logb.String())
	}
	if w.st.Stage != stageFinish || !strings.Contains(w.st.Detail, "포기 1대") || len(w.st.Auto.Queue) != 0 {
		t.Errorf("끝 상태: %s 대기열 %v", w.st.Detail, w.st.Auto.Queue)
	}
}

// 판정 결과에 벤더 정보가 없으면(VendorUnknown=false, 옛 경로) 지금까지처럼 CPU 대기열에 들어간다.
func TestAutoVendorUnknownAbsentKeepsBehaviour(t *testing.T) {
	hosts := map[string]*autoHost{"x": {check: "O", cpu: "X", stress: cpuNotRun, load: 0.5}}
	f := newAutoFake(t, autoT0, []string{"x"}, hosts)
	c := testConf()
	c.AutoLimit = 30 * time.Minute
	w := runAutoFake(t, f, c)
	if x := w.st.row("x"); x.GaveUp != "" || x.CPUTries != 1 {
		t.Errorf("x: %+v", *x)
	}
}

// vendor_id 줄이 Intel/AMD 가 아니면 vendorX 가 켜지고, 줄이 없거나 정보가 없으면 켜지지 않는다.
func TestJudgeVendorUnknownParse(t *testing.T) {
	info := parseInfoOutput("a: vendor_id\t: HygonGenuine\nb: vendor_id\t: GenuineIntel\nc: Thread(s) per core: 2\n")
	if !info["a"].vendorX || info["a"].vendor != "" {
		t.Errorf("a: %+v", *info["a"])
	}
	if info["b"].vendorX || info["b"].vendor != "GenuineIntel" {
		t.Errorf("b: %+v", *info["b"])
	}
	if info["c"].vendorX {
		t.Errorf("c: %+v", *info["c"])
	}
}

// P5 CPU: stress-test 가 [제외] 로 뺀 호스트는 묶음(Batch)에 안 들어가고 시도도 안 세며 대기열 뒤에서 다시 launch 된다.
func TestAutoExcludedCPU(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"b": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"c": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
	}
	f := newAutoFake(t, autoT0, []string{"a", "b", "c"}, hosts)
	var seen []string // 판정 때마다 묶음/대기열/a 의 AutoErr/a 의 CPU 시도
	f.onProbe = func(f *autoFake) {
		a := f.w.st.row("a")
		seen = append(seen, strings.Join(f.w.st.Auto.Batch, ",")+"/"+strings.Join(f.w.st.Auto.Queue, ",")+"/"+a.AutoErr+"/"+strconv.Itoa(a.CPUTries))
	}
	w := runAutoEv(t, f, &autoExclFake{autoFake: f, excl: map[string][]string{"a": {"a"}}}, testConf())
	if got, want := f.pick("cpu", "probe"), "cpu:a,b,c@0 probe:b,c@50 cpu:a@50 probe:a@100"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	if len(seen) == 0 || seen[0] != "b,c/a/환경 실패 1회: stress-test 접속불가/0" {
		t.Errorf("첫 판정 때 묶음/대기열/AutoErr/시도: %q", seen)
	}
	a := w.st.row("a")
	if a.CPUTries != 1 || a.GaveUp != "" || a.CPU != "O" || a.Stress != "O" || a.EnvFails != 0 {
		t.Errorf("a: %+v", *a)
	}
	if !strings.Contains(f.logb.String(), "stress-test 가 접속불가로 뺀 1대는 묶음에 넣지 않고 대기열 뒤로") {
		t.Errorf("로그:\n%s", f.logb.String())
	}
}

// P5 stress-ng: [제외] 로 빠진 호스트는 launch 된 것으로(NGLaunched·stress-ng O) 보지 않고 시도도 안 세며 5분 뒤 다시 launch 된다.
func TestAutoExcludedNG(t *testing.T) {
	hosts := map[string]*autoHost{
		"c": {check: "O", cpu: "O", stress: cpuNotRun},
		"d": {check: "O", cpu: "O", stress: cpuNotRun},
	}
	f := newAutoFake(t, autoT0, []string{"c", "d"}, hosts)
	w := runAutoEv(t, f, &autoExclFake{autoFake: f, excl: map[string][]string{"b": {"d"}}}, testConf())
	if got, want := f.pick("ng"), "ng:c,d@0 ng:d@5"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	c, d := w.st.row("c"), w.st.row("d")
	if !c.NGLaunched || c.NGTries != 1 || !d.NGLaunched || d.NGTries != 1 || d.GaveUp != "" || d.EnvFails != 0 || d.Stress != "O" || d.AutoErr != "" {
		t.Errorf("c: %+v\nd: %+v", *c, *d)
	}
	if !strings.Contains(f.logb.String(), "stress-ng launch 에서 stress-test 가 접속불가로 뺀 1대는 launch 된 것으로 보지 않음") {
		t.Errorf("로그:\n%s", f.logb.String())
	}
}

// P6 늦게 끝난 CPU: 대기열의 CPU X 호스트가 launch 직전 Probe 에서 O 이면 다시 돌리지 않고(부하 확인도 안 함) stress-ng 대상이 된다.
func TestAutoLateCPUDone(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"x": {unreachUntil: 5 * time.Minute, check: "O", cpu: "X", stress: cpuNotRun, load: 0.5, cpuLateAt: 30 * time.Minute},
	}
	f := newAutoFake(t, autoT0, []string{"a", "x"}, hosts)
	w := runAutoFake(t, f, testConf())
	if got, want := f.pick("loads", "cpu", "probe", "ng"), "cpu:a@0 probe:a@50 ng:a@50 probe:x@50 ng:x@50"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	x := w.st.row("x")
	if x.CPU != "O" || x.CPUTries != 0 || x.Stress != "O" || x.GaveUp != "" {
		t.Errorf("x: %+v", *x)
	}
	if !strings.Contains(f.logb.String(), "CPU 열이 O 로 바뀐 1대는 CPU 를 다시 돌리지 않고 stress-ng 대상으로 넘김: x") {
		t.Errorf("로그:\n%s", f.logb.String())
	}
	if len(w.st.Auto.Queue) != 0 || w.st.Stage != stageFinish {
		t.Errorf("끝 상태: 대기열 %v %s", w.st.Auto.Queue, w.st.Detail)
	}
}

// P6 Probe 가 실패하면 기록만 하고 지금까지처럼(부하 확인, launch) 진행한다.
func TestAutoLateCPUProbeError(t *testing.T) {
	hosts := map[string]*autoHost{"x": {check: "O", cpu: "X", stress: cpuNotRun, load: 0.5, cpuAfter: 45 * time.Minute}}
	f := newAutoFake(t, autoT0, []string{"x"}, hosts)
	f.probeErrN = 1
	c := testConf()
	c.AutoLimit = 30 * time.Minute
	w := runAutoFake(t, f, c)
	if got, want := f.pick("loads", "cpu", "probe"), "probe:x@0 loads:x@0 cpu:x@0 probe:x@50"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	if !strings.Contains(f.logb.String(), "CPU X 호스트 CPU 열 확인 실패(그대로 진행)") {
		t.Errorf("로그:\n%s", f.logb.String())
	}
	if x := w.st.row("x"); x.CPUTries != 1 || x.CPU != "O" {
		t.Errorf("x: %+v", *x)
	}
}

// P7 판정이 실패해도 미완료 호스트가 없는 상태에서 AUTO_RECHECK 마다 다시 판정하고, 그동안 설정수정을 다시 하지 않는다(시도를 헛되이 쓰지 않음).
func TestAutoJudgeFailRecheck(t *testing.T) {
	hosts := map[string]*autoHost{"x": {check: "X", fixOn: 1, cpu: "O", stress: "O"}}
	f := newAutoFake(t, autoT0, []string{"x"}, hosts)
	f.judgeFail = []bool{false, true, true, true, false} // 처음 성공, 설정수정 뒤 3회 실패, 그다음 성공
	c := testConf()
	w := runAutoFake(t, f, c)
	r := int(c.AutoRecheck / time.Minute)
	if got, want := eventMins(f, "judge"), "0,0,"+strconv.Itoa(r)+","+strconv.Itoa(2*r)+","+strconv.Itoa(3*r); got != want {
		t.Errorf("판정 시각(분): %s (기대 %s)", got, want)
	}
	if n := len(strings.Fields(f.pick("fix"))); n != 1 {
		t.Errorf("설정수정 %d회(판정이 실패하는 동안 다시 하면 안 됨): %s", n, f.pick("fix"))
	}
	x := w.st.row("x")
	if x.FixTries != 1 || x.Check != "O" || x.GaveUp != "" || w.st.Stage != stageFinish {
		t.Errorf("x: %+v stage=%d", *x, w.st.Stage)
	}
}

// P7 판정이 끝내 실패하면 "할 일 없음" 으로 일찍 끝나지 않고 제한 시간까지 재판정하다 남음으로 끝난다.
func TestAutoJudgeFailRunsToLimit(t *testing.T) {
	hosts := map[string]*autoHost{"x": {check: "X", fixOn: 1, cpu: "O", stress: "O"}}
	f := newAutoFake(t, autoT0, []string{"x"}, hosts)
	f.judgeFail = append([]bool{false}, plan(100000)...)
	c := testConf()
	c.AutoLimit = 3 * time.Hour
	w := runAutoFake(t, f, c)
	if got := f.Now().Sub(autoT0); got < 3*time.Hour {
		t.Errorf("제한 시간 전에 끝남: 시작 뒤 %s", got)
	}
	if n := len(strings.Fields(f.pick("fix"))); n != 1 {
		t.Errorf("설정수정 %d회", n)
	}
	if x := w.st.row("x"); x.FixTries != 1 || x.GaveUp != "" {
		t.Errorf("x: %+v", *x)
	}
	if !strings.Contains(w.st.Detail, "제한 시간 지남") || w.st.Stage != stageFinish {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
}

// 부하가 계속 높은 CPU X 호스트: 일정은 그대로(launch 0회)이고 끝날 때 결과에 `부하 높아 재실행 안 함` 이 남는다(AutoErr 가 비었을 때만).
func TestAutoHighLoadNote(t *testing.T) {
	hosts := map[string]*autoHost{
		"y": {check: "O", cpu: "X", stress: cpuNotRun, load: 5},
		"z": {check: "O", cpu: "X", stress: cpuNotRun, load: 5},
	}
	f := newAutoFake(t, autoT0, []string{"y", "z"}, hosts)
	c := testConf()
	c.AutoLimit = time.Hour
	// z 는 앞서 남은 오류가 있으면 덮어쓰지 않는다(판정 결과를 적용할 때 이름이 같은 행의 값은 이어진다).
	st := &runState{Version: stateVersion, User: "tester", Stage: stageJudge, Auto: &autoState{}, Hosts: []hostRow{{Host: "z", AutoErr: "앞선 오류"}}}
	f.logb = &strings.Builder{}
	w := newWorker(t.TempDir(), st, f, c, nil, f.logb)
	f.w = w
	w.run()
	if strings.Contains(f.pick("cpu"), "cpu:") {
		t.Errorf("CPU launch 가 있음: %s", f.pick("cpu"))
	}
	if y := w.st.row("y"); y.AutoErr != "부하 높아 재실행 안 함" || y.CPUTries != 0 || y.GaveUp != "" {
		t.Errorf("y: %+v", *y)
	}
	if z := w.st.row("z"); z.AutoErr != "앞선 오류" {
		t.Errorf("z: %+v", *z)
	}
	if !strings.Contains(resultText(w.st), "부하 높아 재실행 안 함") {
		t.Errorf("결과에 비고 없음:\n%s", resultText(w.st))
	}
}
