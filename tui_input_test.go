package main

import (
	"strings"
	"testing"
	"time"
)

// 종료 시각 입력 질문의 TUI 처리(칸 입력, 모드 없음)와 화면, 키 정규화(한글·대문자), 보낸 답 대기, 이스케이프 열 나뉨.

type sentV struct {
	sent
	vals []map[string]string
}

func (s *sentV) actions() tuiActions {
	a := s.sent.actions()
	inner := a.answer
	a.answer = func(id, ch string, vals map[string]string) error {
		s.vals = append(s.vals, vals)
		return inner(id, ch, vals)
	}
	return a
}

func inputState() *runState {
	st := sampleState()
	st.Question = &question{ID: "q9", Stage: stageLoad, Text: "stress-ng 종료 시각을 입력하세요", Inputs: endFields,
		Choices: []choice{{"c", "입력한 값으로 계속"}, {"k", "stress-ng 만 건너뜀"}, {"s", "중지"}}}
	return st
}

func typeText(v *viewState, st *runState, a tuiActions, text string) {
	for _, r := range text {
		press(v, st, true, a, string(r))
	}
}

func TestParseKeysBackspace(t *testing.T) {
	if got := strings.Join(parseKeys([]byte("1\x7f\x082")), ","); got != "1,backspace,backspace,2" {
		t.Errorf("got %q", got)
	}
}

// A4: 질문이 뜨면 기본값(t0 = 2026-11-05 09:00)으로 채우고 첫 칸에 둔다
func TestInputDefaults(t *testing.T) {
	v := viewState{}
	st := inputState()
	v.sync(st, t0)
	want := evals("2026", "11", "05", "09", "00")
	for k, x := range want {
		if v.InVals[k] != x {
			t.Errorf("%s 기본값 %q, 기대 %q", k, v.InVals[k], x)
		}
	}
	if v.InQ != "q9" || v.InIdx != 0 || !v.InFresh {
		t.Errorf("초기 상태: %+v", v)
	}
	// 같은 질문이면 다시 채우지 않는다
	v.InVals["end_hour"] = "18"
	v.sync(st, t0.Add(time.Hour))
	if v.InVals["end_hour"] != "18" {
		t.Error("같은 질문인데 값을 다시 채움")
	}
	// 질문이 바뀌면 새 기본값, 입력 질문이 아니면 칸 상태를 지운다
	st.Question.ID = "q10"
	v.sync(st, t0.Add(time.Hour))
	if v.InQ != "q10" || v.InVals["end_hour"] != "10" {
		t.Errorf("새 질문: %+v", v)
	}
	st.Question = &question{ID: "q11", Choices: []choice{{"c", "계속"}}}
	v.sync(st, t0)
	if v.InQ != "" || v.InVals != nil {
		t.Errorf("입력 질문이 아니면 칸 상태를 지워야 함: %+v", v)
	}
}

// A3·A6: 바로 입력, 칸이 차면 다음 칸, 마지막 칸 Enter 로 제출. 값은 입력한 글자 그대로.
func TestInputFlow(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := inputState()
	typeText(&v, st, a, "2026") // 기본값 대신 첫 숫자부터, 4자리면 월로
	if v.InIdx != 1 || v.InVals["end_year"] != "2026" || !v.InFresh {
		t.Fatalf("연도 뒤 월로 안 감: %+v", v)
	}
	typeText(&v, st, a, "11")
	typeText(&v, st, a, "07")
	typeText(&v, st, a, "18")
	if v.InIdx != 4 {
		t.Fatalf("분 칸 아님: %+v", v)
	}
	typeText(&v, st, a, "009") // 마지막 칸은 다음으로 안 가고, 꽉 차면 더 받지 않음
	if v.InIdx != 4 || v.InVals["end_min"] != "00" || len(s.answers) != 0 {
		t.Fatalf("마지막 칸: %+v %v", v, s.answers)
	}
	press(&v, st, true, a, "enter") // 마지막 칸 Enter = 제출
	if len(s.answers) != 1 || s.answers[0] != "q9:c" {
		t.Fatalf("제출 안 됨: %v %+v", s.answers, v)
	}
	got := s.vals[0]
	for k, x := range evals("2026", "11", "07", "18", "00") {
		if got[k] != x {
			t.Errorf("보낸 %s=%q, 기대 %q", k, got[k], x)
		}
	}
	if len(got) != 5 || !strings.Contains(v.Flash, "답을 보냈습니다: [c]") || v.SentQ != "q9" || v.SentKey != "c" {
		t.Errorf("제출 뒤 상태: %v %+v", got, v)
	}
	// 보낸 뒤 같은 질문에는 다시 답하지 않음(입력 키·선택지 키 모두)
	press(&v, st, true, a, "enter")
	press(&v, st, true, a, "c")
	press(&v, st, true, a, "5")
	if len(s.answers) != 1 || v.Flash != flashSent || v.InVals["end_min"] != "00" {
		t.Errorf("보낸 뒤 다시 답함: %v %+v", s.answers, v)
	}
}

// A3: 기본값 대신·Backspace·←→ Tab Shift-Tab·Esc
func TestInputEditing(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := inputState()
	press(&v, st, true, a, "2")
	if v.InVals["end_year"] != "2" || v.InFresh {
		t.Fatalf("첫 숫자가 기본값을 대신하지 않음: %+v", v)
	}
	press(&v, st, true, a, "backspace")
	press(&v, st, true, a, "backspace") // 빈 첫 칸에서 Backspace: 그대로
	if v.InVals["end_year"] != "" || v.InIdx != 0 {
		t.Fatalf("backspace: %+v", v)
	}
	press(&v, st, true, a, "tab") // 빈 칸은 넘어가지 않음
	if v.InIdx != 0 || !strings.Contains(v.InErr, "값을 입력하세요") {
		t.Fatalf("빈 칸 tab: %+v", v)
	}
	typeText(&v, st, a, "2027")
	if v.InIdx != 1 || v.InErr != "" {
		t.Fatalf("연도 뒤: %+v", v)
	}
	press(&v, st, true, a, "1") // 월 기본값 11 → 1
	press(&v, st, true, a, "right")
	if v.InVals["end_month"] != "1" || v.InIdx != 2 {
		t.Fatalf("→ 다음 칸: %+v", v)
	}
	press(&v, st, true, a, "left")
	if v.InIdx != 1 || !v.InFresh {
		t.Fatalf("← 이전 칸: %+v", v)
	}
	typeText(&v, st, a, "03") // 다시 들어온 칸도 첫 숫자가 대신, 차면 다음 칸
	if v.InVals["end_month"] != "03" || v.InIdx != 2 {
		t.Fatalf("월 다시 입력: %+v", v)
	}
	press(&v, st, true, a, "shift-tab")
	if v.InIdx != 1 {
		t.Fatalf("shift-tab: %+v", v)
	}
	press(&v, st, true, a, "tab", "backspace", "backspace") // 일 05 를 지움
	if v.InVals["end_day"] != "" || v.InIdx != 2 {
		t.Fatalf("일 지움: %+v", v)
	}
	press(&v, st, true, a, "backspace") // 빈 칸에서 Backspace → 이전 칸(이어서 지울 수 있게)
	press(&v, st, true, a, "backspace")
	if v.InIdx != 1 || v.InVals["end_month"] != "0" {
		t.Fatalf("빈 칸 backspace: %+v", v)
	}
	press(&v, st, true, a, "esc") // 모든 칸 기본값
	if v.InIdx != 0 || v.InVals["end_year"] != "2026" || v.InVals["end_month"] != "11" || v.InVals["end_day"] != "05" || !strings.Contains(v.Flash, "기본값") {
		t.Fatalf("esc: %+v", v)
	}
	press(&v, st, true, a, "enter", "enter", "enter") // 기본값 칸은 Enter 로 넘어감
	if v.InIdx != 3 || len(s.answers) != 0 {
		t.Errorf("enter 로 다음 칸: %+v %v", v, s.answers)
	}
}

// A5: 칸 검사에 걸리면 글자를 둔 채 그 칸에 머문다
func TestInputFieldError(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := inputState()
	typeText(&v, st, a, "2025") // 올해보다 앞: 4자리지만 넘어가지 않음
	if v.InIdx != 0 || v.InVals["end_year"] != "2025" || !strings.Contains(v.InErr, "올해(2026) 이상") {
		t.Fatalf("연도 오류: %+v", v)
	}
	press(&v, st, true, a, "backspace")
	typeText(&v, st, a, "6")
	if v.InIdx != 1 || v.InErr != "" {
		t.Fatalf("고친 뒤 다음 칸: %+v", v)
	}
	typeText(&v, st, a, "13")
	if v.InIdx != 1 || v.InVals["end_month"] != "13" || !strings.Contains(v.InErr, "1-12") {
		t.Fatalf("월 오류: %+v", v)
	}
	press(&v, st, true, a, "enter")
	press(&v, st, true, a, "tab")
	if v.InIdx != 1 || v.InVals["end_month"] != "13" || !strings.Contains(v.InErr, "월(1-12)") {
		t.Errorf("오류 칸에서 넘어감: %+v", v)
	}
}

// A5: 전체 검사 오류는 값을 두고 고칠 칸으로(지금보다 앞 = 시, 없는 날짜 = 일, 칸 오류 = 그 칸)
func TestInputSubmitErrors(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := inputState()
	press(&v, st, true, a, "c") // 기본값 = 지금 → 지금보다 뒤가 아님
	if len(s.answers) != 0 || v.InIdx != 3 || !strings.Contains(v.InErr, "지금보다 뒤") || v.InVals["end_year"] != "2026" || !v.InFresh {
		t.Fatalf("지금보다 앞: %v %+v", s.answers, v)
	}
	typeText(&v, st, a, "10") // 시를 고쳐 분 칸으로
	press(&v, st, true, a, "enter")
	if len(s.answers) != 1 || s.vals[0]["end_hour"] != "10" || s.vals[0]["end_min"] != "00" {
		t.Fatalf("고친 뒤 제출: %v %v", s.answers, s.vals)
	}
	// 없는 날짜
	v = viewState{}
	st = inputState()
	st.Question.ID = "q12"
	typeText(&v, st, a, "2027")
	typeText(&v, st, a, "02")
	typeText(&v, st, a, "31")
	press(&v, st, true, a, "c")
	if len(s.answers) != 1 || v.InIdx != 2 || !strings.Contains(v.InErr, "없는 날짜") || v.InVals["end_day"] != "31" {
		t.Fatalf("없는 날짜: %+v", v)
	}
	// 칸 오류가 남은 채로 c
	v.InVals["end_month"] = "13"
	press(&v, st, true, a, "c")
	if v.InIdx != 1 || !strings.Contains(v.InErr, "end_month") {
		t.Errorf("칸 오류 제출: %+v", v)
	}
	// 워커가 없으면 제출하지 않음(값은 그대로)
	v = viewState{}
	st = inputState()
	st.Question.ID = "q13"
	typeText(&v, st, a, "2027")
	press(&v, st, false, a, "c")
	if len(s.answers) != 1 || !strings.Contains(v.Flash, "워커가 실행 중이 아니어서") || v.InVals["end_year"] != "2027" {
		t.Errorf("워커 없음: %v %+v", s.answers, v)
	}
}

// A3: 입력 중에도 k·s 선택지와 q·Ctrl-C·화면 키는 그대로
func TestInputOtherKeys(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := inputState()
	press(&v, st, true, a, "2", "k")
	if len(s.answers) != 1 || s.answers[0] != "q9:k" || s.vals[0] != nil {
		t.Fatalf("k: %v %v", s.answers, s.vals)
	}
	v = viewState{}
	st.Question.ID = "q14"
	press(&v, st, true, a, "s", "y")
	if len(s.answers) != 2 || s.answers[1] != "q14:s" || s.vals[1] != nil {
		t.Errorf("중지 답: %v %v", s.answers, s.vals)
	}
	v = viewState{}
	st.Question.ID = "q15"
	press(&v, st, true, a, "f")
	if !v.OnlyBad {
		t.Error("입력 질문 중 f")
	}
	if !press(&v, st, true, a, "q") || !press(&v, st, true, a, "ctrl-c") {
		t.Error("입력 질문 중에도 q·Ctrl-C 는 나가기")
	}
	press(&v, st, true, a, "a") // 숫자도 명령도 아님
	if v.Flash != flashUnknownKey {
		t.Errorf("모르는 키 알림: %q", v.Flash)
	}
}

// A1·A2: 질문 두 줄, 한 줄 양식(지금 칸 반전/NoColor 는 > <), 칸 설명, 오류 줄
func TestRenderInputQuestion(t *testing.T) {
	st := inputState()
	st.Question.Text = "부하 대상 3대에 stress-ng 를 돌립니다.\n끝낼 시각을 입력하세요 (지금보다 뒤)."
	st.StressEnd = "2026,11,07,18,00"
	frame := renderRun(st, viewState{NoColor: true}, 100, 24, t0, true)
	lines := checkFrame(t, "입력 기본", frame, 100, 24)
	text := strings.Join(lines, "\n")
	for _, w := range []string{"-- 질문 q9 (부하 단계) --", "  부하 대상 3대에 stress-ng 를 돌립니다.", "  끝낼 시각을 입력하세요 (지금보다 뒤).",
		"[c] 입력한 값으로 계속", "[s] 중지", "  종료 시각  >2026< - [11] - [05]   [09] : [00]", "  지금 칸: 연도(4자리)",
		"q 나가기", "? 도움말", "Esc 기본값", "stress-ng 종료 시각: 2026-11-07 18:00"} {
		if !strings.Contains(text, w) {
			t.Errorf("없음: %q\n%s", w, text)
		}
	}
	if strings.Contains(text, "i/Enter") {
		t.Errorf("입력 모드 안내가 남음:\n%s", text)
	}
	// 지금 칸·오류
	v := viewState{NoColor: true, InQ: "q9", InIdx: 2, InVals: evals("2026", "11", "4", "09", "00"), InErr: "일(1-31): 일은 1-31 입니다"}
	text = strings.Join(checkFrame(t, "입력 중", renderRun(st, v, 100, 24, t0, true), 100, 24), "\n")
	for _, w := range []string{"[2026] - [11] - >4_<   [09] : [00]", "  ! 일(1-31): 일은 1-31 입니다", "  지금 칸: 일(1-31)"} {
		if !strings.Contains(text, w) {
			t.Errorf("없음: %q\n%s", w, text)
		}
	}
	if strings.Index(text, "  ! 일(1-31)") > strings.Index(text, "  지금 칸:") {
		t.Error("오류 줄은 칸 설명 위")
	}
	// 색: 지금 칸 반전, 나머지 초록
	color := renderRun(st, viewState{}, 100, 24, t0, true)
	if !strings.Contains(color, "\x1b[1;7m[2026]\x1b[0m") || !strings.Contains(color, "\x1b[32m[11]\x1b[0m") {
		t.Errorf("칸 색:\n%q", color)
	}
	// 좁은 화면
	checkFrame(t, "좁은 입력", renderRun(st, viewState{NoColor: true}, 40, 14, t0, true), 40, 14)
}

// B4: 보낸 답 대기 — 선택지 줄 자리에 안내, 같은 질문은 5초 동안 다시 답하지 않음, 질문이 바뀌거나 5초 뒤 풀림
func TestSentPending(t *testing.T) {
	var s sent
	a := s.actions()
	v := viewState{}
	st := askState()
	press(&v, st, true, a, "r")
	press(&v, st, true, a, "c")
	if strings.Join(s.answers, ",") != "q7:r" || v.Flash != flashSent {
		t.Fatalf("보낸 뒤 다른 선택지: %v %q", s.answers, v.Flash)
	}
	text := stripANSI(renderRun(st, v, 100, 20, t0.Add(time.Second), true))
	if !strings.Contains(text, "답 [r] 보냄 — 워커가 받는 중…") || strings.Contains(text, "[x] 제외") || strings.Contains(text, "[선택지 키]") {
		t.Errorf("보낸 답 안내:\n%s", text)
	}
	if !strings.Contains(stripANSI(renderRun(st, v, 100, 20, t0.Add(6*time.Second), true)), "[x] 제외") {
		t.Error("5초 뒤에는 선택지가 다시 보여야 함")
	}
	// 5초가 지나면 다시 답할 수 있음
	handleKey(&v, st, true, "c", 100, 20, t0.Add(6*time.Second), a)
	if strings.Join(s.answers, ",") != "q7:r,q7:c" {
		t.Fatalf("5초 뒤: %v", s.answers)
	}
	// 질문이 바뀌면 바로 풀림
	st.Question.ID = "q8"
	handleKey(&v, st, true, "x", 100, 20, t0.Add(6*time.Second), a)
	if strings.Join(s.answers, ",") != "q7:r,q7:c,q8:x" {
		t.Errorf("질문이 바뀐 뒤: %v", s.answers)
	}
	// 중지(s, y)도 보낸 답으로 기록
	v = viewState{}
	st.Question.ID = "q20"
	press(&v, st, true, a, "s", "y", "c")
	if v.SentQ != "q20" || v.SentKey != "s" || s.answers[len(s.answers)-1] != "q20:s" {
		t.Errorf("중지 답 대기: %+v %v", v, s.answers)
	}
}

// B1·B2: 한글 자모·대문자 → 같은 자리 영문 소문자, g/G 는 접기 전에 표 처음/끝, Space/b 쪽 넘김
func TestNormKey(t *testing.T) {
	cases := map[string]string{
		"ㅊ": "c", "ㄱ": "r", "ㅏ": "k", "ㅌ": "x", "ㄴ": "s", "ㅂ": "q", "ㄹ": "f", "ㅣ": "l", "ㅍ": "v", "ㅛ": "y", "ㅜ": "n", "ㅑ": "i",
		"ㅃ": "q", "ㄲ": "r", "ㅆ": "t", "ㅒ": "o", "ㅖ": "p",
		"C": "c", "S": "s", "Y": "y", "Q": "q",
		"g": "home", "G": "end", "ㅎ": "home", " ": "pgdn", "b": "pgup", "B": "pgup", "ㅠ": "pgup",
		"0": "0", "7": "7", "enter": "enter", "esc": "esc", "가": "가",
	}
	for in, want := range cases {
		if got := normKey(in); got != want {
			t.Errorf("normKey(%q)=%q, 기대 %q", in, got, want)
		}
	}
	// 두벌식 표는 영문 26자를 모두 덮는다
	seen := map[string]bool{}
	for _, k := range jamoKey {
		seen[strings.ToLower(k)] = true
	}
	for c := 'a'; c <= 'z'; c++ {
		if !seen[string(c)] {
			t.Errorf("자모 표에 %c 자리가 없음", c)
		}
	}
}

func TestKoreanAndCapsKeys(t *testing.T) {
	var s sent
	a := s.actions()
	v := viewState{}
	st := askState()
	press(&v, st, true, a, "ㅊ")
	if strings.Join(s.answers, ",") != "q7:c" {
		t.Fatalf("ㅊ: %v", s.answers)
	}
	v = viewState{}
	st.Question.ID = "q8"
	press(&v, st, true, a, "R")
	if s.answers[len(s.answers)-1] != "q8:r" {
		t.Fatalf("R: %v", s.answers)
	}
	v = viewState{}
	st.Question.ID = "q9"
	press(&v, st, true, a, "ㄴ")
	if v.Confirm != "s" {
		t.Fatalf("ㄴ → 중지 확인: %+v", v)
	}
	press(&v, st, true, a, "ㅛ")
	if s.answers[len(s.answers)-1] != "q9:s" {
		t.Errorf("ㅛ 로 확인: %v", s.answers)
	}
	// 입력 질문에서도 숫자는 그대로
	var s2 sentV
	v = viewState{}
	in := inputState()
	press(&v, in, true, s2.actions(), "ㅊ") // 기본값 = 지금이라 전체 검사 오류, 시 칸으로
	if v.InIdx != 3 || len(s2.answers) != 0 {
		t.Errorf("입력 질문 ㅊ: %+v", v)
	}
	press(&v, in, true, s2.actions(), "1", "2")
	if v.InVals["end_hour"] != "12" {
		t.Errorf("숫자 입력: %+v", v)
	}
}

// B3: 쓸 수 없는 키 알림(Esc·Ctrl-L 은 조용히)
func TestUnknownKeyFlash(t *testing.T) {
	var s sent
	for _, st := range []*runState{sampleState(), askState()} {
		v := viewState{}
		press(&v, st, true, s.actions(), "z")
		if v.Flash != flashUnknownKey {
			t.Errorf("z: %q", v.Flash)
		}
		for _, k := range []string{"esc", "ctrl-l"} {
			press(&v, st, true, s.actions(), k)
			if v.Flash != "" {
				t.Errorf("%s 에 알림: %q", k, v.Flash)
			}
		}
	}
	if len(s.answers) != 0 || s.stops != 0 {
		t.Errorf("모르는 키가 전송됨: %v", s.answers)
	}
}

// B5: 읽기 경계에서 나뉜 이스케이프 열·UTF-8 은 꼬리로 남겨 다음 읽기와 잇는다
func TestSplitKeys(t *testing.T) {
	cases := []struct{ in, done, tail string }{
		{"abc", "abc", ""},
		{"a\x1b", "a", "\x1b"},
		{"\x1b[", "", "\x1b["},
		{"x\x1b[1;5", "x", "\x1b[1;5"},
		{"\x1b[A\x1bO", "\x1b[A", "\x1bO"},
		{"\x1bx", "\x1bx", ""}, // ESC 다음 다른 글자는 끝까지 온 것(esc + x)
		{"q" + "ㅊ"[:2], "q", "ㅊ"[:2]},
	}
	for _, c := range cases {
		d, tl := splitKeys([]byte(c.in))
		if string(d) != c.done || string(tl) != c.tail {
			t.Errorf("splitKeys(%q) = %q,%q 기대 %q,%q", c.in, d, tl, c.done, c.tail)
		}
	}
	// 이어 붙이면 방향키 하나
	_, tl := splitKeys([]byte("\x1b["))
	d, tl2 := splitKeys(append(tl, 'A'))
	if got := strings.Join(parseKeys(d), ","); got != "up" || len(tl2) != 0 {
		t.Errorf("이어 붙인 열: %q %q", got, tl2)
	}
	// 50ms 안에 안 오면: ESC 꼬리는 esc, 덜 온 한글은 버림
	if got := strings.Join(flushTail([]byte("\x1b")), ","); got != "esc" {
		t.Errorf("flush esc: %q", got)
	}
	if got := strings.Join(flushTail([]byte("\x1b[")), ","); got != "esc" {
		t.Errorf("flush esc[: %q", got)
	}
	if got := flushTail([]byte("ㅊ"[:2])); got != nil {
		t.Errorf("flush utf8: %q", got)
	}
}

// B6: 새 키 이름
func TestParseKeysNew(t *testing.T) {
	if got := strings.Join(parseKeys([]byte("\x1b[C\x1b[D\t\x1b[Z\x0c \x1bOC")), ","); got != "right,left,tab,shift-tab,ctrl-l, ,right" {
		t.Errorf("got %q", got)
	}
}

// B7: 하단 안내는 지금 쓸 수 있는 키만, q·? 먼저, 항목 단위로 두 줄까지
func TestKeyHints(t *testing.T) {
	items := []string{"q 나가기", "? 도움말", "가나다라", "abc", "defgh"}
	got := wrapItems(items, 20, 2)
	if len(got) != 2 || got[0] != "q 나가기  ? 도움말" || got[1] != "가나다라  abc  defgh" {
		t.Errorf("wrapItems: %q", got)
	}
	if got := wrapItems(items, 10, 2); len(got) != 2 || got[0] != "q 나가기" || got[1] != "? 도움말" {
		t.Errorf("두 줄 넘침: %q", got)
	}
	st := sampleState()
	h := keyHints(st, viewState{}, true)
	if h[0] != "q 나가기" || h[1] != "? 도움말" || !contains(h, "s 작업 중지") || contains(h, "[선택지 키] 답하기") {
		t.Errorf("질문 없음: %q", h)
	}
	if contains(keyHints(st, viewState{}, false), "s 작업 중지") {
		t.Error("워커가 없으면 s 작업 중지 안내 없음")
	}
	h = keyHints(inputState(), viewState{}, true)
	if !contains(h, "숫자 입력") || !contains(h, "[선택지 키] 답하기") || contains(h, "s 작업 중지") {
		t.Errorf("입력 질문: %q", h)
	}
	h = keyHints(askState(), viewState{SentQ: "q7"}, true)
	if contains(h, "[선택지 키] 답하기") {
		t.Errorf("보낸 뒤에는 답하기 안내 없음: %q", h)
	}
	if h := keyHints(st, viewState{Confirm: "S"}, true); strings.Join(h, ",") != "y 중지,다른 키 취소" {
		t.Errorf("확인 중: %q", h)
	}
	// 좁은 화면에서도 두 줄 안, 맨 앞은 q 나가기
	lines := checkFrame(t, "좁은 안내", renderRun(inputState(), viewState{NoColor: true}, 40, 30, t0, true), 40, 30)
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "q 나가기") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("안내 줄 첫 항목이 q 나가기 아님:\n%s", strings.Join(lines, "\n"))
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// 인벤토리 사용 중 질문(r/x, s 없음): s 로 작업 전체 중지 요청
func TestBusyQuestionStopKey(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := sampleState()
	st.Question = &question{ID: "q4", Stage: stageLoad, Kind: "busy", Text: "사용 중",
		Choices: []choice{{"r", "다시 확인"}, {"x", "이 묶음 중단"}}}
	press(&v, st, true, a, "r")
	v = viewState{}
	press(&v, st, true, a, "x")
	if strings.Join(s.answers, ",") != "q4:r,q4:x" {
		t.Errorf("answers=%v", s.answers)
	}
	press(&v, st, true, a, "s")
	if v.Confirm != "S" {
		t.Fatalf("s 확인 대기 아님: %+v", v)
	}
	press(&v, st, true, a, "y")
	if s.stops != 1 {
		t.Errorf("stop.req 요청 안 함")
	}
	// 입력 질문(c/k/s)에서 S(Caps Lock) 는 그 질문의 중지
	v = viewState{}
	press(&v, inputState(), true, a, "S")
	if v.Confirm != "s" || v.ConfirmQ != "q9" {
		t.Errorf("s 선택지가 있으면 질문의 중지: %+v", v)
	}
}

func TestRenderBusyQuestion(t *testing.T) {
	st := sampleState()
	st.Question = &question{ID: "q4", Stage: stageLoad, Kind: "busy",
		Text:    "stress-ng 테스트: 작업 템플릿의 인벤토리가 DEFAULT 가 아니어서 멈췄습니다\n템플릿 stress(7) 인벤토리가 hong 입니다",
		Choices: []choice{{"r", "다시 확인"}, {"x", "이 묶음 중단"}}}
	lines := checkFrame(t, "busy", renderRun(st, viewState{NoColor: true}, 100, 20, t0, true), 100, 20)
	text := strings.Join(lines, "\n")
	for _, w := range []string{"-- 질문 q4 (부하 단계) --", "템플릿 stress(7) 인벤토리가 hong 입니다", "[r] 다시 확인", "[x] 이 묶음 중단", "s 작업 중지"} {
		if !strings.Contains(text, w) {
			t.Errorf("없음: %q\n%s", w, text)
		}
	}
	plain := renderPlain(inputState(), true, t0)
	if !strings.Contains(plain, "입력 항목: end_year(연도(4자리))") {
		t.Errorf("plain 에 입력 항목 안내 없음:\n%s", plain)
	}
}
