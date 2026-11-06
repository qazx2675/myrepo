package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 부하 단계 전 CPU X 호스트 재실행 질문
// ---------------------------------------------------------------------------

func xWorld() map[string]*fakeHost {
	return map[string]*fakeHost{
		"low":  {status: stDone, check: "O", cpu: "X", stress: cpuNotRun, cpuReady: 10 * time.Minute},
		"high": {status: stDone, check: "O", cpu: "X", stress: cpuNotRun, cpuReady: 10 * time.Minute},
		"nr":   {status: stDone, check: "O", cpu: "X", stress: cpuNotRun, cpuReady: 10 * time.Minute},
	}
}

func xLoads() map[string]float64 { return map[string]float64{"low": 0.5, "high": 5} } // nr 은 응답 없음

func TestParseLoadOutput(t *testing.T) {
	out := "ev01: 0.52 0.30 0.20 1/234 5678\r\nev02: 12.00 9.00 5.00 3/400 99\nev03: command not found\nev04: abc 0 0 1/2 3\nnoise\nev05: 0.01 0.02\n"
	got := parseLoadOutput(out)
	if len(got) != 2 || got["ev01"] != 0.52 || got["ev02"] != 12 {
		t.Errorf("parseLoadOutput = %v", got)
	}
}

// y: 부하 낮은 X 호스트만 CPU 재실행에 들어가 O 가 되고, 높음·응답없음은 그대로 X
func TestWorkerXRetryYes(t *testing.T) {
	f := newFake(t, []string{"low", "high", "nr"}, xWorld(), "c", "y", "c", "c")
	f.loads = xLoads()
	w := runFake(t, f, nil)

	if !w.st.Finished() || w.st.Stopped {
		t.Fatalf("완료가 아님: %+v", w.st.Detail)
	}
	eqList(t, "질문", f.asked, []string{"판정:crs=c", "부하:yns=y", "부하:crs=c", "stress-ng:crs=c"})
	if f.count("loads:") != 1 || f.idx("loads:low,high,nr") < 0 {
		t.Errorf("부하 확인은 X 후보 전체에 한 번: %v", f.events)
	}
	if f.count("start:a") != 1 || f.idx("start:a:low") < 0 {
		t.Errorf("CPU 는 low 만: %v", f.events)
	}
	low, high, nr := row(t, w, "low"), row(t, w, "high"), row(t, w, "nr")
	if low.CPU != "O" || low.CPURetry || !low.CPUTarget {
		t.Errorf("low: %+v", *low)
	}
	if high.CPU != "X" || high.CPUTarget || high.CPURetry || nr.CPU != "X" || nr.CPUTarget {
		t.Errorf("high/nr 는 그대로여야 함: %+v %+v", *high, *nr)
	}
	// 질문 로그에 부하가 보이고 건너뛴 호스트 수가 적힌다
	logText := w.log.(*strings.Builder).String()
	if !strings.Contains(logText, "low(0.50)") || !strings.Contains(logText, "건너뜀: 부하 높음 1대, 응답 없음 1대") {
		t.Errorf("질문 로그에 부하·건너뜀이 없음:\n%s", logText)
	}
}

// n: 모두 제외 → CPU 를 돌리지 않고(할 일 없음) 부하 단계는 질문 없이 넘어가 완료
func TestWorkerXRetryNo(t *testing.T) {
	f := newFake(t, []string{"low", "high", "nr"}, xWorld(), "c", "n")
	f.loads = xLoads()
	w := runFake(t, f, nil)

	eqList(t, "질문", f.asked, []string{"판정:crs=c", "부하:yns=n"})
	if w.st.Stage != stageFinish || w.st.Stopped {
		t.Fatalf("완료가 아님: stage=%d stopped=%v", w.st.Stage, w.st.Stopped)
	}
	if f.count("start:") != 0 {
		t.Errorf("아무것도 돌리면 안 됨: %v", f.events)
	}
	if r := row(t, w, "low"); r.CPU != "X" || r.CPURetry || r.CPUTarget {
		t.Errorf("low: %+v", *r)
	}
}

// 모두 부하 높음/응답 없음 → 질문 없이 부하 단계를 건너뜀. 부하 확인 실패도 같다
func TestWorkerXRetryNoLowOrError(t *testing.T) {
	f := newFake(t, []string{"low", "high", "nr"}, xWorld(), "c")
	f.loads = map[string]float64{"low": 3, "high": 9} // 3 은 기준 미만이 아님
	w := runFake(t, f, nil)
	eqList(t, "질문", f.asked, []string{"판정:crs=c"})
	if w.st.Stage != stageFinish || f.count("start:") != 0 || f.loadCalls != 1 {
		t.Errorf("stage=%d events=%v", w.st.Stage, f.events)
	}

	f2 := newFake(t, []string{"low", "high", "nr"}, xWorld(), "c")
	f2.loadErr = errTimeout
	w2 := runFake(t, f2, nil)
	eqList(t, "질문(오류)", f2.asked, []string{"판정:crs=c"})
	if w2.st.Stage != stageFinish || f2.count("start:") != 0 {
		t.Errorf("stage=%d events=%v", w2.st.Stage, f2.events)
	}
}

// s: 중지
func TestWorkerXRetryStop(t *testing.T) {
	f := newFake(t, []string{"low", "high", "nr"}, xWorld(), "c", "s")
	f.loads = xLoads()
	w := runFake(t, f, nil)
	if !w.st.Stopped || f.count("start:") != 0 {
		t.Errorf("중지 기대: stopped=%v events=%v", w.st.Stopped, f.events)
	}
}

// NOTRUN 호스트와 X 호스트가 함께 있으면 y 일 때 같은 CPU 묶음에 들어간다.
// 같은 단계에서 한 번만 묻는다(r 로 다시 해도 다시 묻지 않음).
func TestWorkerXRetryWithNotRunAskOnce(t *testing.T) {
	world := xWorld()
	world["new"] = &fakeHost{status: stDone, check: "O", cpu: cpuNotRun, stress: cpuNotRun, cpuReady: 10 * time.Minute}
	f := newFake(t, []string{"new", "low", "high", "nr"}, world, "c", "y", "r", "c")
	f.loads = xLoads()
	w := runFake(t, f, nil)
	eqList(t, "질문", f.asked, []string{"판정:crs=c", "부하:yns=y", "부하:crs=r", "부하:crs=c"})
	if f.idx("start:a:new,low") < 0 {
		t.Errorf("CPU 묶음에 new,low: %v", f.events)
	}
	if f.loadCalls != 1 {
		t.Errorf("부하 확인 %d번(1 기대)", f.loadCalls)
	}
	if !w.st.Finished() {
		t.Errorf("완료가 아님")
	}
}

// CPU 대상 선정: CPURetry 표시가 있는 X 호스트도 cpuTargets 에 들어가고, 후보(xCandidates)에서는 빠진다.
// nextStage 는 후보만 있어도 부하로 간다.
func TestXTargetsAndNextStage(t *testing.T) {
	w := newWorker(t.TempDir(), &runState{Hosts: []hostRow{
		{Host: "x1", Status: stDone, Check: "O", CPU: "X", Stress: cpuNotRun},
		{Host: "x2", Status: stDone, Check: "O", CPU: "X", Stress: cpuNotRun, CPURetry: true},
		{Host: "x3", Status: stDone, Check: "O", CPU: "X", Stress: cpuNotRun, Excluded: true},
		{Host: "x4", Status: stDone, Check: "X", CPU: "X", Stress: cpuNotRun},
		{Host: "o1", Status: stDone, Check: "O", CPU: "O", Stress: "O"},
	}}, newFake(t, nil, nil), testConf(), nil, &strings.Builder{})
	eqList(t, "cpu", w.cpuTargets(), []string{"x2"})
	eqList(t, "후보", w.xCandidates(), []string{"x1"})
	w.st.Hosts[1].CPURetry = false
	eqList(t, "cpu(표시 없음)", w.cpuTargets(), nil)
	if n := w.nextStage(stageFix); n != stageLoad {
		t.Errorf("후보만 있어도 부하로 가야 함: %d", n)
	}
	w.st.Hosts[0].CPU = "O"
	w.st.Hosts[1].CPU = "O"
	w.st.Hosts[0].Stress, w.st.Hosts[1].Stress = "O", "O"
	if n := w.nextStage(stageFix); n != stageFinish {
		t.Errorf("후보가 없으면 완료: %d", n)
	}
}

// ---------------------------------------------------------------------------
// 로그 창
// ---------------------------------------------------------------------------

func TestTailOfAndTailLines(t *testing.T) {
	if got := tailOf("a\nb\nc\n", 2, false); strings.Join(got, ",") != "b,c" {
		t.Errorf("lastLines = %v", got)
	}
	if got := tailOf("끊긴줄\nb\nc\r\n\n", 5, true); strings.Join(got, ",") != "b,c" {
		t.Errorf("partial = %v", got)
	}
	if got := tailOf("", 3, false); len(got) != 0 {
		t.Errorf("빈 입력 = %v", got)
	}
	p := filepath.Join(t.TempDir(), "worker.log")
	if tailLines(p, 3) != nil {
		t.Errorf("없는 파일은 nil")
	}
	// 64KB 보다 큰 파일: 끝 줄들만 읽고 중간에서 잘린 첫 줄은 버린다
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		b.WriteString("줄번호 ")
		b.WriteString(strings.Repeat("가", 10))
		b.WriteString(" ")
		b.WriteString(time.Duration(i).String())
		b.WriteString("\n")
	}
	if b.Len() <= logTailBytes {
		t.Fatalf("시험 파일이 작음")
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got := tailLines(p, 3)
	if len(got) != 3 || !strings.HasSuffix(got[2], time.Duration(4999).String()) || !strings.HasPrefix(got[0], "줄번호 ") {
		t.Errorf("tailLines = %q", got)
	}
}

func TestCleanLogLine(t *testing.T) {
	if got := cleanLogLine("a\tb\x1b[31mc\x00"); got != "a  b[31mc" {
		t.Errorf("cleanLogLine = %q", got)
	}
}

func TestLogHeight(t *testing.T) {
	v := viewState{}
	if n := logHeight(v, 40, 5, 2); n != 1+maxLogLines {
		t.Errorf("큰 화면 = %d", n)
	}
	// 남는 자리 = h - top - bottom - 2 - minTableRows
	if n := logHeight(v, 5+2+2+minTableRows+4, 5, 2); n != 4 {
		t.Errorf("작은 화면 = %d", n)
	}
	if n := logHeight(v, 5+2+2+minTableRows+1, 5, 2); n != 0 {
		t.Errorf("자리가 1줄이면 창을 뺀다 = %d", n)
	}
	if n := logHeight(viewState{NoLog: true}, 40, 5, 2); n != 0 {
		t.Errorf("끄면 0 = %d", n)
	}
}

func TestLogPaneRender(t *testing.T) {
	st := sampleState()
	v := viewState{NoColor: true, LogLines: []string{"첫 줄", "2026-11-05 09:00:00 질문 q1 (c/r/s): " + strings.Repeat("긴글", 60), "마지막\t줄\x1b[2J"}}
	frame := renderRun(st, v, 60, 24, t0, true)
	lines := checkFrame(t, "로그 창", frame, 60, 24)
	// 로그 창은 맨 아래, 안내 줄은 그 위
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "마지막  줄[2J") {
		t.Errorf("로그 끝 줄이 없음:\n%s", joined)
	}
	if !strings.Contains(joined, "-- worker.log (l 로 끄기) --") || !strings.Contains(joined, "첫 줄") || !strings.Contains(joined, "l 로그 켜기/끄기") {
		t.Errorf("로그 창·안내가 없음:\n%s", joined)
	}
	if strings.Contains(joined, "\x1b") {
		t.Errorf("제어 문자가 남음")
	}
	// 끄면 창이 없고 마지막 줄은 안내 줄
	v.NoLog = true
	off := checkFrame(t, "끔", renderRun(st, v, 60, 24, t0, true), 60, 24)
	if strings.Contains(strings.Join(off, "\n"), "worker.log") || !strings.Contains(off[len(off)-1], "l 로그 켜기/끄기") {
		t.Errorf("끈 화면:\n%s", strings.Join(off, "\n"))
	}
	// 로그가 없으면 안내 문구
	v.NoLog, v.LogLines = false, nil
	if f := renderRun(st, v, 60, 24, t0, true); !strings.Contains(f, "(로그 없음)") {
		t.Errorf("로그 없음 안내가 없음")
	}
}

// 질문·선택지는 로그 창 때문에 밀려나지 않고, 작은 화면에서는 로그 창이 줄거나 빠진다
func TestLogPaneKeepsQuestion(t *testing.T) {
	st := sampleState()
	st.Question = &question{ID: "q9", Stage: stageLoad, Text: "CPU X 호스트를 다시 돌릴까요?",
		Choices: []choice{{Key: "y", Label: "모두 CPU 재실행에 포함"}, {Key: "n", Label: "모두 제외"}, {Key: "s", Label: "중지"}}}
	v := viewState{NoColor: true, LogLines: []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8"}}
	for _, h := range []int{18, 20, 22, 30, 40} {
		frame := renderRun(st, v, 80, h, t0, true)
		lines := checkFrame(t, "작은 화면", frame, 80, h)
		j := strings.Join(lines, "\n")
		if !strings.Contains(j, "[y] 모두 CPU 재실행에 포함") || !strings.Contains(j, "[s] 중지") {
			t.Errorf("h=%d 선택지가 밀려남:\n%s", h, j)
		}
		if !strings.Contains(j, "hostname") {
			t.Errorf("h=%d 표 제목이 없음", h)
		}
	}
}

func TestToggleLogKey(t *testing.T) {
	st := sampleState()
	v := viewState{}
	press(&v, st, true, (&sent{}).actions(), "l")
	if !v.NoLog {
		t.Errorf("l 로 꺼져야 함")
	}
	press(&v, st, true, (&sent{}).actions(), "l")
	if v.NoLog {
		t.Errorf("l 로 다시 켜져야 함")
	}
	// y/n 선택지 질문에서도 키가 답으로 전달된다
	st.Question = &question{ID: "q3", Stage: stageLoad, Choices: []choice{{Key: "y"}, {Key: "n"}, {Key: "s"}}}
	s := &sent{}
	press(&v, st, true, s.actions(), "y", "n")
	if strings.Join(s.answers, ",") != "q3:y,q3:n" {
		t.Errorf("답 = %v", s.answers)
	}
}

// ---------------------------------------------------------------------------
// -new
// ---------------------------------------------------------------------------

func TestRunNewFlagArchives(t *testing.T) {
	h := newHarness(t)
	dir := h.writeState(stageLoad, false)
	if err := os.WriteFile(filepath.Join(dir, "worker.log"), []byte("옛 로그\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := h.deps()
	d.fresh = true
	if code := runEntry("hosts.txt", "", "", "u1", d); code != 0 {
		t.Fatalf("code=%d err=%s", code, h.errBuf.String())
	}
	if h.picked != 1 || h.started != 1 || h.asked != 0 {
		t.Errorf("새 작업으로 시작해야 함: picked=%d started=%d asked=%d", h.picked, h.started, h.asked)
	}
	olds, _ := filepath.Glob(dir + ".old-*")
	if len(olds) != 1 {
		t.Fatalf("보관 폴더 %v", olds)
	}
	if b, err := os.ReadFile(filepath.Join(olds[0], "worker.log")); err != nil || string(b) != "옛 로그\n" {
		t.Errorf("보관 폴더 내용이 그대로여야 함: %v %q", err, b)
	}
	st, err := loadState(dir)
	if err != nil || st.User != "tester" || st.Stage != stageJudge {
		t.Errorf("새 상태: %v %+v", err, st)
	}
	if !strings.Contains(h.errBuf.String(), olds[0]) {
		t.Errorf("보관 위치 안내가 없음: %s", h.errBuf.String())
	}
}

func TestRunNewFlagRefusesWhenAlive(t *testing.T) {
	h := newHarness(t)
	dir := h.writeState(stageLoad, false)
	h.alive = true
	d := h.deps()
	d.fresh = true
	if code := runEntry("hosts.txt", "", "", "", d); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if h.started != 0 || h.shown != 0 {
		t.Errorf("거절해야 함: started=%d shown=%d", h.started, h.shown)
	}
	if olds, _ := filepath.Glob(dir + ".old-*"); len(olds) != 0 {
		t.Errorf("이름을 바꾸면 안 됨: %v", olds)
	}
	if _, err := loadState(dir); err != nil {
		t.Errorf("상태가 그대로여야 함: %v", err)
	}
	if !strings.Contains(h.errBuf.String(), "S 로") {
		t.Errorf("중지 안내가 없음: %s", h.errBuf.String())
	}
}

func TestRunNewFlagNoPreviousAndWithout(t *testing.T) {
	h := newHarness(t)
	d := h.deps()
	d.fresh = true
	if code := runEntry("hosts.txt", "", "", "", d); code != 0 || h.started != 1 {
		t.Fatalf("이전 상태가 없으면 그냥 시작: code=%d started=%d", code, h.started)
	}
	if olds, _ := filepath.Glob(stateDirFor("hosts.txt") + ".old-*"); len(olds) != 0 {
		t.Errorf("보관할 것이 없음: %v", olds)
	}
	// -new 없이: 끝난 작업은 그대로 보이고 이름을 바꾸지 않는다
	h2 := newHarness(t)
	dir := h2.writeState(stageFinish, false)
	if code := runEntry("hosts.txt", "", "", "", h2.deps()); code != 0 || h2.started != 0 {
		t.Fatalf("code=%d started=%d", code, h2.started)
	}
	if olds, _ := filepath.Glob(dir + ".old-*"); len(olds) != 0 {
		t.Errorf("-new 없이는 보관하지 않음: %v", olds)
	}
}

func TestArchiveDirCollision(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 11, 6, 9, 8, 7, 0, time.UTC)
	var got []string
	for i := 0; i < 3; i++ {
		d := filepath.Join(tmp, "hosts_run")
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		dst, err := archiveDir(d, now)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.Base(dst))
	}
	want := "hosts_run.old-20261106-090807,hosts_run.old-20261106-090807-2,hosts_run.old-20261106-090807-3"
	if strings.Join(got, ",") != want {
		t.Errorf("archiveDir = %v", got)
	}
}
