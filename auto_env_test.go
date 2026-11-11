package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// 환경 실패(계획서_auto_막힘.md 4.2) 시험: 시도에 세지 않고, 간격이 5→10→20→30→30분으로 늘고, 성공하면 5분으로 돌아온다.

// autoEvFake 는 stress-test 이벤트([job] 줄)를 낼 수 있는 가짜 실행기다(eventStresser). jobFirst 면 launch 전에 [job] 을 낸다.
type autoEvFake struct {
	*autoFake
	jobFirst bool
}

func (f *autoEvFake) StressTestEv(steps, listFile string, hosts []string, end string, launched func(), registered func(yml string, hosts []string), busy func(msg string) string, ev func(stressEvent)) (int, error) {
	if f.jobFirst && ev != nil {
		ev(stressEvent{Kind: stressJobMark, Stage: steps, JobID: 1})
	}
	return f.autoFake.StressTest(steps, listFile, hosts, end, launched, registered, busy)
}

// runAutoEv 는 runAutoFake 와 같지만 실행기를 따로 받는다.
func runAutoEv(t *testing.T, f *autoFake, ex executor, conf config) *worker {
	t.Helper()
	st := &runState{Version: stateVersion, User: "tester", Stage: stageJudge, Auto: &autoState{}}
	f.logb = &strings.Builder{}
	w := newWorker(t.TempDir(), st, ex, conf, nil, f.logb)
	f.w = w
	w.run()
	return w
}

func plan(n int, tail ...bool) []bool {
	p := make([]bool, n)
	for i := range p {
		p[i] = true
	}
	return append(p, tail...)
}

func eventMins(f *autoFake, kind string) string {
	var m []string
	for _, e := range strings.Fields(f.pick(kind)) {
		m = append(m, e[strings.LastIndex(e, "@")+1:])
	}
	return strings.Join(m, ",")
}

// (a)(b) CPU launch 환경 실패 5회: 포기 0대, 시도 안 셈, 간격 5·10·20·30·30분. 성공하면 5분으로 돌아온다.
func TestAutoEnvCPULaunch(t *testing.T) {
	hosts := map[string]*autoHost{"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute}}
	f := newAutoFake(t, autoT0, []string{"a"}, hosts)
	f.failPlan = plan(5, false)
	w := runAutoFake(t, f, testConf())
	if got := eventMins(f, "cpu"); got != "0,5,15,35,65,95" {
		t.Errorf("CPU launch 시각(분): %s", got)
	}
	for i, n := range f.seenTry {
		if n != 1 { // 호출 중에는 이번 시도가 1 로 올라 있고, 실패 뒤 되돌려져 다음 호출에도 1 이어야 한다
			t.Errorf("%d 번째 실패 때 시도 횟수 %d(1 이어야 함)", i+1, n)
		}
	}
	for i, e := range f.seenErr[1:] {
		if want := fmt.Sprintf("환경 실패 %d회: ", i+1); !strings.HasPrefix(e, want) {
			t.Errorf("%d 번째 실패 뒤 AutoErr %q", i+1, e)
		}
	}
	r := w.st.row("a")
	if r.GaveUp != "" || r.CPUTries != 1 || r.EnvFails != 0 || r.AutoErr != "" || w.st.Auto.EnvFail != 0 {
		t.Errorf("a: %+v env=%d", *r, w.st.Auto.EnvFail)
	}
	if r.CPU != "O" || r.Stress != "O" || w.st.Stage != stageFinish {
		t.Errorf("끝 상태: %+v", *r)
	}
	if n := strings.Count(f.logb.String(), "환경 실패(시도 안 셈)"); n != 5 {
		t.Errorf("환경 실패 로그 %d줄", n)
	}
}

// (b) 성공하면 대기가 처음(5분)으로 돌아온다: CPU 환경 실패 2회(5,10분) → 성공 → stress-ng 환경 실패 1회는 20분이 아니라 5분.
func TestAutoEnvResetAfterSuccess(t *testing.T) {
	hosts := map[string]*autoHost{"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute}}
	f := newAutoFake(t, autoT0, []string{"a"}, hosts)
	f.failPlan = []bool{true, true, false, true, false}
	w := runAutoFake(t, f, testConf())
	if got := eventMins(f, "cpu"); got != "0,5,15" {
		t.Errorf("CPU launch 시각(분): %s", got)
	}
	if got := eventMins(f, "ng"); got != "65,70" {
		t.Errorf("stress-ng launch 시각(분): %s (처음 간격 5분이어야 함)", got)
	}
	r := w.st.row("a")
	if r.CPUTries != 1 || r.NGTries != 1 || r.GaveUp != "" || r.Stress != "O" || r.EnvFails != 0 {
		t.Errorf("a: %+v", *r)
	}
}

// (e) stress-ng launch 환경 실패도 같다.
func TestAutoEnvNGLaunch(t *testing.T) {
	hosts := map[string]*autoHost{"a": {check: "O", cpu: "O", stress: cpuNotRun}}
	f := newAutoFake(t, autoT0, []string{"a"}, hosts)
	f.failPlan = plan(5, false)
	w := runAutoFake(t, f, testConf())
	if got := eventMins(f, "ng"); got != "0,5,15,35,65,95" {
		t.Errorf("stress-ng launch 시각(분): %s", got)
	}
	for i, n := range f.seenTry {
		if n != 1 {
			t.Errorf("%d 번째 실패 때 시도 횟수 %d", i+1, n)
		}
	}
	r := w.st.row("a")
	if r.GaveUp != "" || r.NGTries != 1 || r.Stress != "O" || r.Running != "" {
		t.Errorf("a: %+v", *r)
	}
}

// 12시간 제한이 지나도록 환경 실패가 이어지면 포기하지 않고 남음으로 끝나고, 결과에 마지막 환경 실패 사유가 적힌다.
func TestAutoEnvLimitKeepsReason(t *testing.T) {
	hosts := map[string]*autoHost{"a": {check: "O", cpu: "O", stress: cpuNotRun}}
	f := newAutoFake(t, autoT0, []string{"a"}, hosts)
	f.failPlan = plan(1000)
	c := testConf()
	c.AutoLimit = 3 * time.Hour
	w := runAutoFake(t, f, c)
	r := w.st.row("a")
	if r.GaveUp != "" || r.NGTries != 0 || r.EnvFails < 5 {
		t.Errorf("a: %+v", *r)
	}
	if !strings.Contains(w.st.Detail, "제한 시간 지남") || !strings.Contains(w.st.Detail, "남음 1대") || !strings.Contains(w.st.Detail, "마지막 환경 실패") {
		t.Errorf("Detail: %s", w.st.Detail)
	}
}

// (c) 이벤트를 아는 실행기에서 [job] 이 나온 뒤의 launch 실패는 호스트 실패: 시도로 세고 3회째에 포기.
func TestAutoEnvHostFailAfterJob(t *testing.T) {
	hosts := map[string]*autoHost{"a": {check: "O", cpu: "O", stress: cpuNotRun, ngFail: true}}
	f := newAutoFake(t, autoT0, []string{"a"}, hosts)
	w := runAutoEv(t, f, &autoEvFake{autoFake: f, jobFirst: true}, testConf())
	r := w.st.row("a")
	if r.NGTries != 3 || r.GaveUp != "stress-ng" || r.EnvFails != 0 || w.st.Auto.EnvFail != 0 {
		t.Errorf("a: %+v env=%d", *r, w.st.Auto.EnvFail)
	}
	if got := eventMins(f, "ng"); got != "0,5,10" {
		t.Errorf("stress-ng launch 시각(분): %s", got)
	}
}

// 같은 실행기에서 [job] 이 나오기 전의 launch 실패는 환경 실패(옛 stress-test 도 같다).
func TestAutoEnvNoJobIsEnv(t *testing.T) {
	hosts := map[string]*autoHost{"a": {check: "O", cpu: "O", stress: cpuNotRun, ngFail: true}}
	f := newAutoFake(t, autoT0, []string{"a"}, hosts)
	c := testConf()
	c.AutoLimit = time.Hour
	w := runAutoEv(t, f, &autoEvFake{autoFake: f}, c)
	r := w.st.row("a")
	if r.NGTries != 0 || r.GaveUp != "" || r.EnvFails < 3 {
		t.Errorf("a: %+v", *r)
	}
}

// (d) ConfigFix 오류(환경 실패)는 FixTries 를 올리지 않고 5·10·20·30분 간격으로 다시 한다.
func TestAutoEnvFixError(t *testing.T) {
	hosts := map[string]*autoHost{"x": {check: "X", fixOn: 1, cpu: "O", stress: "O"}}
	f := newAutoFake(t, autoT0, []string{"x"}, hosts)
	f.fixErrN = 5
	w := runAutoFake(t, f, testConf())
	if got := eventMins(f, "fix"); got != "0,5,15,35,65,95" {
		t.Errorf("설정수정 시각(분): %s", got)
	}
	r := w.st.row("x")
	if r.FixTries != 1 || r.GaveUp != "" || r.Check != "O" || r.EnvFails != 0 || w.st.Auto.EnvFail != 0 || w.st.Stage != stageFinish {
		t.Errorf("x: %+v", *r)
	}
}

// (d) ConfigFix 는 끝났는데 계속 X 면 호스트 실패: 시도로 세고 3회에 포기.
func TestAutoEnvFixStillXCounts(t *testing.T) {
	hosts := map[string]*autoHost{"x": {check: "X", cpu: "O", stress: "O"}}
	f := newAutoFake(t, autoT0, []string{"x"}, hosts)
	w := runAutoFake(t, f, testConf())
	r := w.st.row("x")
	if r.FixTries != 3 || r.GaveUp != "설정수정" || r.EnvFails != 0 || w.st.Auto.EnvFail != 0 {
		t.Errorf("x: %+v", *r)
	}
	if n := len(strings.Fields(f.pick("fix"))); n != 3 {
		t.Errorf("설정수정 %d회", n)
	}
}
