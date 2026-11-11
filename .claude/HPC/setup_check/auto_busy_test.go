package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 인벤토리 사용 중 질문(계획서_auto_막힘.md 4.3) 시험: 질문이 떠 있어도 스케줄러는 멈추지 않고 그 launch 만 보류한다.
// x 는 환경 실패(시도 안 셈)로 센다. 제한 시간이 지나면 질문을 취소하고 x 로 답한다. 중지 요청이면 바로 끝난다.

// answerAfter 는 질문이 d 동안 떠 있으면 choice 로 답하는 질문 처리기다.
func answerAfter(d time.Duration, choice string) func(f *autoFake, q *question) {
	return func(f *autoFake, q *question) {
		if f.qAge(q) >= d {
			f.answerQ(q.ID, choice)
		}
	}
}

func hasAny(l []string, sub string) bool {
	for _, s := range l {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// (a)(b) 질문이 30분 떠 있는 동안 재판정·설정수정·stress-ng launch 가 계속되고, r 이면 launch 되어 묶음이 기록된다.
func TestAutoBusyNoFreeze(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"b": {unreachUntil: 15 * time.Minute, check: "X", fixOn: 1, cpu: "O", stress: "O"},
		"c": {unreachUntil: 15 * time.Minute, check: "O", cpu: "O", stress: cpuNotRun},
	}
	f := newAutoFake(t, autoT0, []string{"a", "b", "c"}, hosts)
	f.busyN = map[string]int{"a": 1}
	f.onBusy = answerAfter(30*time.Minute, ansRetry)
	w := runAutoFake(t, f, testConf())
	want := "judge:@0 busy:a@0 judge:@10 judge:@20 fix:b@20 judge:@20 ng:c@20 cpu:a@30 probe:a@80 ng:a@80"
	if got := f.pick("judge", "fix", "busy", "cpu", "ng", "probe"); got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	eqList(t, "답", f.busyAns, []string{"a=r@30"})
	if !hasAny(f.qDetail, "CPU 템플릿 인벤토리 사용 중, 답을 기다리는 중(launch 보류)") {
		t.Errorf("질문 동안 진행 설명: %q", f.qDetail)
	}
	if len(f.qTexts) != 1 || !strings.HasPrefix(f.qTexts[0], "CPU 테스트: 작업 템플릿의 인벤토리가 DEFAULT 가 아니어서") {
		t.Errorf("질문 문구: %q", f.qTexts)
	}
	log := f.logb.String()
	for _, s := range []string{"CPU launch 보류(1대)", "질문 q1 (r/x, CPU 인벤토리 사용 중, launch 보류)", "답 q1: r", "CPU 묶음 launch 완료 1대: 첫 판정 10:20:05"} { // 답은 autoPoll(5초)마다 확인
		if !strings.Contains(log, s) {
			t.Errorf("로그에 %q 없음:\n%s", s, log)
		}
	}
	if a := w.st.row("a"); a.CPUTries != 1 || a.CPU != "O" || a.Stress != "O" || a.EnvFails != 0 {
		t.Errorf("a: %+v", *a)
	}
	if w.st.Stage != stageFinish || w.st.Question != nil || !strings.Contains(w.st.Detail, "모두 O 3대") {
		t.Errorf("끝 상태: %s %+v", w.st.Detail, w.st.Question)
	}
}

// (c) x: 그 묶음은 중단되고 환경 실패로 센다(시도 안 셈). 5분 뒤 다시 launch 한다.
func TestAutoBusyAbort(t *testing.T) {
	hosts := map[string]*autoHost{"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute}}
	f := newAutoFake(t, autoT0, []string{"a"}, hosts)
	f.busyN = map[string]int{"a": 1}
	f.onBusy = answerAfter(5*time.Minute, ansExclude)
	w := runAutoFake(t, f, testConf())
	if got, want := f.pick("busy", "cpu"), "busy:a@0 cpu:a@10"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	eqList(t, "답", f.busyAns, []string{"a=x@5"})
	log := f.logb.String()
	if !strings.Contains(log, "환경 실패(시도 안 셈): CPU launch 1대 — CPU 템플릿 인벤토리 사용 중 질문에 x(이 묶음 중단)") {
		t.Errorf("로그:\n%s", log)
	}
	if a := w.st.row("a"); a.CPUTries != 1 || a.CPU != "O" || a.GaveUp != "" || a.EnvFails != 0 {
		t.Errorf("a: %+v", *a)
	}
}

// (d) 제한 시간(60분)이 지나면 답을 기다리던 질문을 취소하고 x 로 답한다. 그동안 재판정은 10분마다 돌았다. 그 뒤 끝난다.
func TestAutoBusyLimitCancel(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuAfter: 45 * time.Minute},
		"u": {unreachUntil: 1000 * time.Hour},
	}
	f := newAutoFake(t, autoT0, []string{"a", "u"}, hosts)
	f.busyN = map[string]int{"a": 1}
	f.onBusy = func(*autoFake, *question) {} // 아무도 답하지 않는다
	conf := testConf()
	conf.AutoLimit = 60 * time.Minute
	w := runAutoFake(t, f, conf)
	if got, want := f.pick("judge", "busy", "cpu"), "judge:@0 busy:a@0 judge:@10 judge:@20 judge:@30 judge:@40 judge:@50"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	eqList(t, "답", f.busyAns, []string{"a=x@60"})
	if log := f.logb.String(); !strings.Contains(log, "질문 q1 취소: 제한 시간이 지나") {
		t.Errorf("로그:\n%s", log)
	}
	if got := f.Now().Sub(autoT0); got != 60*time.Minute {
		t.Errorf("끝난 시각: 시작 뒤 %s", got)
	}
	if a := w.st.row("a"); a.CPUTries != 0 || a.Running != "" {
		t.Errorf("a: %+v", *a)
	}
	if w.st.Stage != stageFinish || w.st.Question != nil || !w.st.Auto.Ending || !strings.Contains(w.st.Detail, "제한 시간 지남") {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
}

// (e) 질문이 떠 있는 동안 중지 요청: 바로 중지되고 stress-test 는 x 로 끝난다(고루틴이 남지 않음 = 기다리던 결과를 받고 끝남).
func TestAutoBusyStop(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: cpuNotRun},
		"u": {unreachUntil: 1000 * time.Hour},
	}
	f := newAutoFake(t, autoT0, []string{"a", "u"}, hosts)
	f.busyN = map[string]int{"a": 1}
	f.onBusy = func(f *autoFake, q *question) {
		if f.qAge(q) >= 15*time.Minute {
			_ = os.WriteFile(filepath.Join(f.w.dir, stopReqFile), []byte("x"), 0o644)
		}
	}
	done := make(chan *worker, 1)
	go func() { done <- runAutoFake(t, f, testConf()) }()
	var w *worker
	select {
	case w = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("중지 요청 뒤 끝나지 않음(막힘)")
	}
	f.mu.Lock()
	ans := append([]string(nil), f.busyAns...)
	f.mu.Unlock()
	eqList(t, "답", ans, []string{"a=x@15"})
	if !w.st.Stopped || w.st.Question != nil {
		t.Errorf("끝 상태: stopped=%v %+v", w.st.Stopped, w.st.Question)
	}
	if log := f.logb.String(); !strings.Contains(log, "질문 q1 취소: 중지 요청") {
		t.Errorf("로그:\n%s", log)
	}
}

// (f) stress-ng·CPU 둘 다 보류되면 질문은 하나씩 묻는다(앞 질문의 답을 받은 뒤 다음 질문).
func TestAutoBusyTwoKinds(t *testing.T) {
	hosts := map[string]*autoHost{
		"a": {check: "O", cpu: cpuNotRun, stress: "O", cpuAfter: 45 * time.Minute},
		"c": {check: "O", cpu: "O", stress: cpuNotRun},
	}
	f := newAutoFake(t, autoT0, []string{"a", "c"}, hosts)
	f.busyN = map[string]int{"a": 1, "b": 1}
	f.onBusy = answerAfter(10*time.Minute, ansRetry)
	w := runAutoFake(t, f, testConf())
	if got, want := f.pick("busy", "cpu", "ng"), "busy:c@0 busy:a@0 ng:c@10 cpu:a@20"; got != want {
		t.Errorf("사건\n기대: %s\n실제: %s", want, got)
	}
	eqList(t, "답", f.busyAns, []string{"b=r@10", "a=r@20"})
	if len(f.qTexts) != 2 || !strings.HasPrefix(f.qTexts[0], "stress-ng 테스트") || !strings.HasPrefix(f.qTexts[1], "CPU 테스트") {
		t.Errorf("질문 차례: %q", f.qTexts)
	}
	if !hasAny(f.qDetail, "CPU 템플릿 인벤토리 사용 중, 앞 질문 뒤에 물음(launch 보류)") {
		t.Errorf("진행 설명: %q", f.qDetail)
	}
	log := f.logb.String()
	if i, j := strings.Index(log, "답 q1: r"), strings.Index(log, "질문 q2"); i < 0 || j < i {
		t.Errorf("q2 가 q1 의 답보다 먼저 나옴:\n%s", log)
	}
	if w.st.Stage != stageFinish || !strings.Contains(w.st.Detail, "모두 O 2대") {
		t.Errorf("끝 상태: %s", w.st.Detail)
	}
}
