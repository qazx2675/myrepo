package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

var t0 = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

// sampleState 는 시험용 상태: 호스트 4대(완료 3 + 접속불가 1), 부하 단계.
func sampleState() *runState {
	return &runState{
		Version: stateVersion, User: "dhkim", HostArg: "^hosts.txt", HostFile: "/work/hosts.txt", WorkDir: "/work",
		Created: t0.Add(-75 * time.Minute), Stage: stageLoad, Detail: "CPU 감시 중", Heartbeat: t0.Add(-2 * time.Second),
		Hosts: []hostRow{
			{Host: "ev01", Status: stDone, Check: "O", CPU: "O", Stress: "O", Note: "팬 소음"},
			{Host: "ev02", Status: stDone, Check: "O", CPU: cpuNotRun, Stress: cpuNotRun, CPUTarget: true, Running: prgCPURun},
			{Host: "ev03", Status: stDone, Check: "X", CPU: "-", Stress: "-", Excluded: true},
			{Host: "ev04-긴이름서버", Status: stUnreach, Check: "-", CPU: "-", Stress: "-"},
		},
		CPULaunch:  t0.Add(-10 * time.Minute),
		CheckStart: t0.Add(37 * time.Minute),
		NextCheck:  t0.Add(2 * time.Minute),
		Deadline:   t0.Add(67 * time.Minute),
	}
}

func checkFrame(t *testing.T, name, frame string, w, h int) []string {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Errorf("%s: 줄 수 %d, 기대 %d", name, len(lines), h)
	}
	for i, l := range lines {
		if dw := displayWidth(stripANSI(l)); dw > w {
			t.Errorf("%s: %d번째 줄이 폭 %d 를 넘음(%d): %q", name, i+1, w, dw, l)
		}
	}
	return lines
}

func TestRenderBasic(t *testing.T) {
	st := sampleState()
	frame := renderRun(st, viewState{NoColor: true}, 100, 20, t0, true)
	lines := checkFrame(t, "기본", frame, 100, 20)
	want := []string{
		"new_setup -run | 사용자 dhkim | 호스트파일 hosts.txt | 워커 실행중 | 경과 1시간 15분",
		" 1 판정  >  2 설정수정  > [3 부하] >  4 stress-ng  >  5 완료 ",
		"진행: CPU 감시 중",
		"CPU: launch 08:50 | 확인 시작 09:37(37분 후) | 다음 확인 09:02(2분 후) | 마감 10:07(1시간 07분 후)",
		"hostname        | 상태     | 설정체크 | CPU    | stress-ng | 비고",
		"ev01            | 완료     | O        | O      | O         | 팬 소음",
		"ev02            | 완료     | O        | NOTRUN | NOTRUN    | CPU 실행중",
		"ev03            | 완료     | X        | -      | -         | 제외",
		"ev04-긴이름서버 | 접속불가 | -        | -      | -         |",
		"1-4 / 4대",
	}
	for i, w := range want {
		if i >= len(lines) {
			t.Fatalf("줄이 모자람")
		}
		if strings.TrimRight(lines[i], " ") != strings.TrimRight(w, " ") {
			t.Errorf("%d번째 줄\n got: %q\nwant: %q", i+1, lines[i], w)
		}
	}
	if j := strings.Join(lines, "\n"); !strings.Contains(j, "f O 아닌 것만") && !strings.Contains(j, "s 작업 중지") {
		t.Errorf("안내 줄이 없음:\n%s", j)
	}
}

func TestRenderQuestion(t *testing.T) {
	st := sampleState()
	st.Stage = stageFix
	st.CPULaunch = time.Time{}
	st.Question = &question{ID: "q3", Stage: stageFix, Text: "완료 3대(제외 1): 설정체크 O 2, X 1\nstress-test: 미실행/실패 호스트가 있습니다",
		Choices: []choice{{"c", "계속 → 부하"}, {"r", "재시도(설정수정 다시)"}, {"x", "O 아닌 1대 제외하고 계속"}, {"s", "중지"}}}
	frame := renderRun(st, viewState{NoColor: true}, 80, 18, t0, true)
	lines := checkFrame(t, "질문", frame, 80, 18)
	text := strings.Join(lines, "\n")
	for _, w := range []string{"-- 질문 q3 (설정수정 단계) --", "  완료 3대(제외 1): 설정체크 O 2, X 1", "[c] 계속 → 부하", "[r] 재시도(설정수정 다시)", "[x] O 아닌 1대 제외하고 계속", "[s] 중지", "[선택지 키] 답하기"} {
		if !strings.Contains(text, w) {
			t.Errorf("없음: %q\n%s", w, text)
		}
	}
	if strings.Contains(text, "CPU: launch") {
		t.Error("부하 단계가 아니면 CPU 시간표가 없어야 함")
	}
	// 질문 상자는 표 아래, 안내 줄 위(맨 아래쪽)
	if idx := strings.Index(text, "-- 질문"); idx < strings.Index(text, "1-4 / 4대") {
		t.Error("질문 상자가 표보다 위에 있음")
	}
}

func TestRenderWorkerStates(t *testing.T) {
	st := sampleState()
	cases := []struct {
		name  string
		alive bool
		hb    time.Time
		mut   func(s *runState)
		want  string
	}{
		{"실행중", true, t0.Add(-3 * time.Second), nil, "워커 실행중"},
		{"워커없음", false, t0.Add(-3 * time.Second), nil, "워커 중단됨"},
		{"heartbeat 오래됨", true, t0.Add(-20 * time.Second), nil, "워커 중단됨"},
		{"완료", false, t0.Add(-time.Hour), func(s *runState) { s.Stage = stageFinish }, "워커 완료"},
		{"중지", false, t0.Add(-time.Hour), func(s *runState) { s.Stopped = true }, "워커 중지됨"},
	}
	for _, c := range cases {
		s := *st
		s.Heartbeat = c.hb
		if c.mut != nil {
			c.mut(&s)
		}
		first := strings.Split(renderRun(&s, viewState{NoColor: true}, 100, 12, t0, c.alive), "\n")[0]
		if !strings.Contains(first, c.want) {
			t.Errorf("%s: %q 에 %q 없음", c.name, first, c.want)
		}
	}
	// 중단됨이고 미완료면 경고 줄
	s := *st
	if f := renderRun(&s, viewState{NoColor: true}, 100, 14, t0, false); !strings.Contains(f, "워커가 실행 중이 아닙니다") {
		t.Error("중단 경고 줄이 없음")
	}
	// 완료면 결과 파일 경로
	s.Stage = stageFinish
	if f := renderRun(&s, viewState{NoColor: true}, 100, 14, t0, false); !strings.Contains(f, "결과 파일: /work/hosts.txt_run/result.txt") {
		t.Errorf("결과 파일 경로가 없음:\n%s", f)
	}
}

func TestRenderNarrowAndSmall(t *testing.T) {
	st := sampleState()
	st.Question = &question{ID: "q1", Stage: stageLoad, Text: strings.Repeat("긴 문장 ", 30),
		Choices: []choice{{"c", "계속"}, {"r", "재시도"}, {"s", "중지"}}}
	for _, sz := range [][2]int{{40, 20}, {24, 12}, {60, 6}, {10, 3}, {1, 1}} {
		frame := renderRun(st, viewState{NoColor: true}, sz[0], sz[1], t0, true)
		checkFrame(t, "작은 화면", frame, sz[0], sz[1])
	}
	// 폭 40: 비고 열이 줄거나 빠져도 호스트 이름은 보인다
	f := renderRun(st, viewState{NoColor: true}, 40, 30, t0, true)
	if !strings.Contains(f, "ev01") || !strings.Contains(f, "ev02") {
		t.Errorf("좁은 화면에서 호스트가 안 보임:\n%s", f)
	}
}

func TestRenderScrollAndFilter(t *testing.T) {
	st := sampleState()
	st.Stage = stageFix
	st.CPULaunch = time.Time{}
	st.Hosts = nil
	for i := 0; i < 30; i++ {
		st.Hosts = append(st.Hosts, hostRow{Host: "h" + formatNum(i, 2), Status: stDone, Check: "O", CPU: "O", Stress: map[bool]string{true: "O", false: cpuNotRun}[i%3 != 0]})
	}
	v := viewState{NoColor: true}
	f := renderRun(st, v, 90, 14, t0, true)
	if !strings.Contains(f, "h00") || strings.Contains(f, "h29") {
		t.Errorf("처음 화면:\n%s", f)
	}
	capRows := tableCapacity(st, v, 90, 14, t0, true)
	v.Scroll = 1 << 30 // 범위를 넘어도 끝으로 맞춤
	f = renderRun(st, v, 90, 14, t0, true)
	if !strings.Contains(f, "h29") || strings.Contains(f, "h00 ") {
		t.Errorf("끝 화면:\n%s", f)
	}
	if !strings.Contains(f, "-30 / 30대") {
		t.Errorf("스크롤 위치 안내:\n%s", f)
	}
	// 필터: stress-ng NOTRUN 인 10대(i%3==0)만
	v = viewState{NoColor: true, OnlyBad: true}
	f = renderRun(st, v, 90, 40, t0, true)
	if !strings.Contains(f, "h00") || strings.Contains(f, "h01 ") || !strings.Contains(f, "10대 (O 아닌 호스트만") {
		t.Errorf("필터 화면:\n%s", f)
	}
	if capRows < 1 {
		t.Errorf("capacity=%d", capRows)
	}
}

func TestRenderColorAndHelp(t *testing.T) {
	st := sampleState()
	f := renderRun(st, viewState{}, 100, 14, t0, true)
	if !strings.Contains(f, "\x1b[32mO ") || !strings.Contains(f, "\x1b[1;7m[3 부하]\x1b[0m") {
		t.Errorf("색이 안 들어감:\n%q", f)
	}
	if strings.Contains(renderRun(st, viewState{NoColor: true}, 100, 14, t0, true), "\x1b") {
		t.Error("NoColor 인데 ANSI 가 있음")
	}
	h := renderRun(st, viewState{NoColor: true, Help: true}, 80, 16, t0, true)
	checkFrame(t, "도움말", h, 80, 16)
	if !strings.Contains(h, "s  작업 중지") || !strings.Contains(h, "q  화면만 나가기") || !strings.Contains(h, "Esc  모든 칸 기본값") {
		t.Errorf("도움말 내용:\n%s", h)
	}
	if n := renderRun(nil, viewState{NoColor: true}, 40, 5, t0, false); !strings.Contains(n, "읽는 중") {
		t.Errorf("nil 상태: %q", n)
	}
	// 확인 중 / 알림
	c := renderRun(st, viewState{NoColor: true, Confirm: "S"}, 100, 14, t0, true)
	if !strings.Contains(c, "작업을 중지할까요?") {
		t.Errorf("확인 줄 없음:\n%s", c)
	}
}

func TestWidthHelpers(t *testing.T) {
	if displayWidth("a가b") != 4 {
		t.Error("displayWidth")
	}
	if truncW("가나다", 5) != "가나" || truncW("abc", 0) != "" {
		t.Error("truncW")
	}
	if got := padW("가", 4); got != "가  " {
		t.Errorf("padW=%q", got)
	}
	if got := wrapW("가나다라\nab", 4); strings.Join(got, "/") != "가나/다라/ab" {
		t.Errorf("wrapW=%v", got)
	}
	if fmtDur(75*time.Minute) != "1시간 15분" || fmtDur(3*time.Minute+5*time.Second) != "3분 05초" || fmtDur(-time.Second) != "0초" {
		t.Error("fmtDur")
	}
}

func TestRenderPlain(t *testing.T) {
	st := sampleState()
	st.Question = &question{ID: "q2", Stage: stageLoad, Text: "a\nb", Choices: []choice{{"c", "계속"}, {"s", "중지"}}}
	p := renderPlain(st, true, t0)
	for _, w := range []string{"사용자: dhkim", "단계: 3 부하", "워커: 실행중", "CPU: launch 08:50", "질문 q2 (부하 단계): a / b", "선택지: [c] 계속  [s] 중지",
		header + "\n", "ev02|완료|O|NOTRUN|NOTRUN|"} {
		if !strings.Contains(p, w) {
			t.Errorf("없음 %q\n%s", w, p)
		}
	}
	if strings.Contains(p, "\x1b") {
		t.Error("글 출력에 ANSI 가 있음")
	}
}

func autoSampleState() *runState {
	st := sampleState()
	st.Stage = stageJudge
	st.Detail = "자동 진행 — 남은 시간 11시간 00분, CPU 묶음 1대 다음 판정 09:20"
	st.Auto = &autoState{Start: t0.Add(-time.Hour), Limit: 12 * time.Hour, Batch: []string{"ev02"}, BatchNext: t0.Add(20 * time.Minute),
		NextRecheck: t0.Add(7 * time.Minute)}
	st.Hosts[1].CPUTries = 1
	st.Hosts[2].FixTries, st.Hosts[2].GaveUp, st.Hosts[2].AutoErr = 3, "설정수정", "설정체크 X 가|3회 수정 뒤에도 그대로"
	return st
}

func TestResultPanelPlain(t *testing.T) {
	st := sampleState()
	frame := renderRun(st, viewState{NoColor: true, Result: true}, 100, 24, t0, true)
	lines := checkFrame(t, "결과 판", frame, 100, 24)
	j := strings.Join(lines, "\n")
	for _, w := range []string{
		"완료(OOO) 1대|남음 3대|포기 0대",
		"hostname|상태|설정체크|CPU테스트|stress-ng|비고",
		"ev01|완료|O|O|O|팬 소음",
		"ev02|완료|O|NOTRUN|NOTRUN|CPU 실행중",
		"1-4 / 4대 (결과 판, v 로 호스트표)",
		"v 결과 판",
	} {
		if !strings.Contains(j, w) {
			t.Errorf("%q 가 없음:\n%s", w, j)
		}
	}
	if strings.Contains(j, "시도(수정") || strings.Contains(j, "|다음 확인 ") || strings.Contains(j, "[자동 진행]") {
		t.Errorf("-auto 가 아닌데 자동 진행 정보가 보임:\n%s", j)
	}
	if strings.Contains(j, "hostname        | 상태") {
		t.Errorf("결과 판은 공백 열이 아니라 | 로 나눠야 함")
	}
	// 끄면 원래 호스트표
	if f := renderRun(st, viewState{NoColor: true}, 100, 24, t0, true); strings.Contains(f, "결과 판,") || !strings.Contains(f, "hostname        | 상태") {
		t.Errorf("꺼진 화면이 호스트표가 아님")
	}
}

func TestResultPanelAuto(t *testing.T) {
	st := autoSampleState()
	frame := renderRun(st, viewState{NoColor: true, Result: true}, 120, 24, t0, true)
	lines := checkFrame(t, "auto 결과 판", frame, 120, 24)
	j := strings.Join(lines, "\n")
	for _, w := range []string{
		"new_setup -run -auto |",
		"[자동 진행] 남은 시간 11시간 00분(제한 12시간 00분) | 완료(OOO) 1대 | 남음 2대 | 포기 1대",
		"남은 시간 11시간 00분(제한 12시간 00분)|다음 확인 09:07(7분 후)",
		"지금 하는 일: 자동 진행 — 남은 시간 11시간 00분",
		"hostname|상태|설정체크|CPU테스트|stress-ng|시도(수정/CPU/ng)|비고|마지막오류",
		"ev02|완료|O|NOTRUN|NOTRUN|0/1/0|CPU 실행중|",
		"ev03|완료|X|-|-|3/0/0|제외, 포기(설정수정)|설정체크 X 가/3회 수정 뒤에도 그대로",
	} {
		if !strings.Contains(j, w) {
			t.Errorf("%q 가 없음:\n%s", w, j)
		}
	}
	if strings.Contains(j, "1 판정") {
		t.Errorf("auto 머리에 단계 줄이 남음:\n%s", j)
	}
	// 결과 판이 꺼져도 머리는 auto 표시
	off := renderRun(st, viewState{NoColor: true}, 120, 24, t0, true)
	if !strings.Contains(off, "[자동 진행] 남은 시간") || !strings.Contains(off, "hostname        | 상태") {
		t.Errorf("꺼진 auto 화면:\n%s", off)
	}
	// 작은 화면에서도 줄 수·폭이 맞는다
	for _, h := range []int{12, 16, 20} {
		checkFrame(t, "작은 auto 결과 판", renderRun(st, viewState{NoColor: true, Result: true}, 60, h, t0, true), 60, h)
	}
	// 제한 시간 지남 / 끝
	st.Auto.Ending = true
	if g := autoRemain(st, t0); !strings.Contains(g, "제한 시간 지남") {
		t.Errorf("Ending = %q", g)
	}
	st.Stage = stageFinish
	if g := autoRemain(st, t0); g != "자동 진행 끝" {
		t.Errorf("끝 = %q", g)
	}
}

func TestResultTextAuto(t *testing.T) {
	st := autoSampleState()
	got := resultText(st)
	for _, w := range []string{
		header + "\n",
		"\n\n완료(OOO) 1대|남음 2대|포기 1대\n",
		"hostname|설정수정시도|CPU시도|stress-ng시도|포기|마지막오류\n",
		"ev02|0|1|0||\n",
		"ev03|3|0|0|설정수정|설정체크 X 가/3회 수정 뒤에도 그대로\n",
	} {
		if !strings.Contains(got, w) {
			t.Errorf("%q 가 없음:\n%s", w, got)
		}
	}
	if strings.Contains(got, "ev01|0|") {
		t.Errorf("시도·포기·오류가 없는 호스트는 뺀다:\n%s", got)
	}
	// -auto 가 아니면 예전과 같다(표만)
	plain := resultText(sampleState())
	if strings.Contains(plain, "완료(OOO)") || strings.Count(plain, "\n") != 5 {
		t.Errorf("non-auto result.txt 가 바뀜:\n%s", plain)
	}
}
