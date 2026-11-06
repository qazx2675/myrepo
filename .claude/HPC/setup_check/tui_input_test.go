package main

import (
	"strings"
	"testing"
)

// 종료 시각 입력 질문의 TUI 처리(줄 입력 모드)와 화면, 인벤토리 사용 중 질문의 S 키.

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
		Choices: []choice{{"c", "입력한 값으로 계속"}, {"s", "중지"}}}
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

func TestInputFlow(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := inputState()

	// 값 없이 c → 먼저 입력하라는 안내, 전송 안 함
	press(&v, st, true, a, "c")
	if len(s.answers) != 0 || !strings.Contains(v.Flash, "먼저 값을 입력") {
		t.Fatalf("입력 없이 c: %v %q", s.answers, v.Flash)
	}
	// i → 입력 모드. 연도: 2026
	press(&v, st, true, a, "i")
	if v.InputQ != "q9" || v.InputIdx != 0 {
		t.Fatalf("입력 모드 아님: %+v", v)
	}
	typeText(&v, st, a, "2a0b2x6") // 숫자 아닌 글자는 무시
	if v.InputBuf != "2026" {
		t.Fatalf("buf=%q", v.InputBuf)
	}
	typeText(&v, st, a, "9") // 4자리 넘으면 무시
	if v.InputBuf != "2026" {
		t.Fatalf("5자리가 들어감: %q", v.InputBuf)
	}
	press(&v, st, true, a, "backspace")
	if v.InputBuf != "202" {
		t.Fatalf("backspace: %q", v.InputBuf)
	}
	press(&v, st, true, a, "enter") // 202 는 연도 아님 → 오류, 같은 항목 다시
	if v.InputIdx != 0 || v.InputBuf != "" || !strings.Contains(v.InputErr, "end_year") || !strings.Contains(v.InputErr, "4자리") {
		t.Fatalf("연도 오류 처리: %+v", v)
	}
	typeText(&v, st, a, "2026")
	press(&v, st, true, a, "enter")
	if v.InputIdx != 1 || v.InputErr != "" || v.InputVals["end_year"] != "2026" {
		t.Fatalf("다음 항목으로 못 감: %+v", v)
	}
	typeText(&v, st, a, "13")
	press(&v, st, true, a, "enter") // 월 13 → 오류, 같은 항목
	if v.InputIdx != 1 || !strings.Contains(v.InputErr, "1-12") {
		t.Fatalf("월 오류: %+v", v)
	}
	// 나머지: 11 / 07 / 18 / 00 (입력한 글자 그대로)
	for _, x := range []string{"11", "07", "18", "00"} {
		typeText(&v, st, a, x)
		press(&v, st, true, a, "enter")
	}
	if v.InputQ != "" || v.ReadyQ != "q9" || v.ReadyVals["end_day"] != "07" || v.ReadyVals["end_min"] != "00" {
		t.Fatalf("입력 완료 상태 아님: %+v", v)
	}
	// c 로 제출 → 값 포함
	press(&v, st, true, a, "c")
	if len(s.answers) != 1 || s.answers[0] != "q9:c" || s.vals[0]["end_hour"] != "18" || s.vals[0]["end_month"] != "11" || v.ReadyQ != "" {
		t.Fatalf("제출 오류: %v %v %+v", s.answers, s.vals, v)
	}
}

func TestInputEscAndEnterKey(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := inputState()
	press(&v, st, true, a, "enter") // Enter 로도 입력 시작
	typeText(&v, st, a, "20")
	press(&v, st, true, a, "esc")
	if v.InputQ != "" || v.InputBuf != "" || !strings.Contains(v.Flash, "취소") {
		t.Fatalf("esc: %+v", v)
	}
	// 입력 모드에서 q 는 종료가 아니라 무시, Ctrl-C 는 종료
	press(&v, st, true, a, "i")
	if press(&v, st, true, a, "q") {
		t.Error("입력 중 q 로 끝나면 안 됨")
	}
	if !press(&v, st, true, a, "ctrl-c") {
		t.Error("입력 중에도 Ctrl-C 는 끝냄")
	}
	// 워커 없음 → 입력 시작 안 함
	v = viewState{}
	press(&v, st, false, a, "i")
	if v.InputQ != "" || !strings.Contains(v.Flash, "워커가 실행 중이 아니어서") {
		t.Errorf("워커 없음: %+v", v)
	}
}

func TestInputQuestionChangedAndCombinedError(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := inputState()
	press(&v, st, true, a, "i")
	st.Question = &question{ID: "q10", Stage: stageLoad, Text: "다른 질문", Choices: []choice{{"c", "계속"}}}
	press(&v, st, true, a, "2")
	if v.InputQ != "" || !strings.Contains(v.Flash, "질문이 바뀌어") {
		t.Errorf("질문이 바뀌면 입력 취소: %+v", v)
	}
	// 합친 시각이 지금(t0 = 2026-11-05 09:00)보다 앞이면 처음부터 다시
	st = inputState()
	v = viewState{}
	press(&v, st, true, a, "i")
	for _, x := range []string{"2026", "11", "05", "08", "00"} {
		typeText(&v, st, a, x)
		press(&v, st, true, a, "enter")
	}
	if v.InputQ != "q9" || v.InputIdx != 0 || !strings.Contains(v.InputErr, "지금보다 뒤") || v.ReadyQ != "" {
		t.Errorf("합친 시각 오류 처리: %+v", v)
	}
	// 없는 날짜
	for _, x := range []string{"2027", "02", "31", "10", "00"} {
		typeText(&v, st, a, x)
		press(&v, st, true, a, "enter")
	}
	if !strings.Contains(v.InputErr, "없는 날짜") || v.InputIdx != 0 {
		t.Errorf("없는 날짜: %+v", v)
	}
}

// 입력 질문에서 s → 중지 확인(y/N) 뒤 중지 답
func TestInputQuestionStop(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := inputState()
	press(&v, st, true, a, "s", "y")
	if len(s.answers) != 1 || s.answers[0] != "q9:s" || s.vals[0] != nil {
		t.Errorf("중지 답: %v %v", s.answers, s.vals)
	}
}

// 인벤토리 사용 중 질문(r/x, s 없음): S 로 작업 중지 요청
func TestBusyQuestionStopKey(t *testing.T) {
	var s sentV
	a := s.actions()
	v := viewState{}
	st := sampleState()
	st.Question = &question{ID: "q4", Stage: stageLoad, Kind: "busy", Text: "사용 중",
		Choices: []choice{{"r", "다시 확인"}, {"x", "이 묶음 중단"}}}
	press(&v, st, true, a, "r")
	press(&v, st, true, a, "x")
	if strings.Join(s.answers, ",") != "q4:r,q4:x" {
		t.Errorf("answers=%v", s.answers)
	}
	press(&v, st, true, a, "S")
	if v.Confirm != "S" {
		t.Fatalf("S 확인 대기 아님: %+v", v)
	}
	press(&v, st, true, a, "y")
	if s.stops != 1 {
		t.Errorf("stop.req 요청 안 함")
	}
	// 입력 질문(c/s)에서 S 는 안내만
	v = viewState{}
	press(&v, inputState(), true, a, "S")
	if v.Confirm != "" || !strings.Contains(v.Flash, "질문에 답하세요") {
		t.Errorf("s 선택지가 있으면 S 는 안내만: %+v", v)
	}
}

func TestRenderInputQuestion(t *testing.T) {
	st := inputState()
	st.StressEnd = "2026,11,07,18,00"
	// 아직 입력 안 함: 안내 줄
	frame := renderRun(st, viewState{NoColor: true}, 100, 24, t0, true)
	lines := checkFrame(t, "입력 안내", frame, 100, 24)
	text := strings.Join(lines, "\n")
	for _, w := range []string{"-- 질문 q9 (부하 단계) --", "[c] 입력한 값으로 계속", "[s] 중지", "[i/Enter] 값 입력: end_year(연도(4자리))", "i/Enter 값 입력", "stress-ng 종료 시각: 2026-11-07 18:00"} {
		if !strings.Contains(text, w) {
			t.Errorf("없음: %q\n%s", w, text)
		}
	}
	// 입력 중: 지금 항목·확정한 값·오류
	v := viewState{NoColor: true, InputQ: "q9", InputIdx: 2, InputBuf: "0", InputErr: "end_day: 일은 1-31 입니다",
		InputVals: map[string]string{"end_year": "2026", "end_month": "11"}}
	frame = renderRun(st, v, 100, 24, t0, true)
	lines = checkFrame(t, "입력 중", frame, 100, 24)
	text = strings.Join(lines, "\n")
	for _, w := range []string{"  ! end_day: 일은 1-31 입니다", "end_year=2026 end_month=11", "end_day: 0_", "일(1-31)", "Enter 다음 항목", "Esc 입력 취소"} {
		if !strings.Contains(text, w) {
			t.Errorf("없음: %q\n%s", w, text)
		}
	}
	// 입력을 마침: 확정한 값과 제출 안내
	v = viewState{NoColor: true, ReadyQ: "q9", ReadyVals: evals("2026", "11", "07", "18", "00")}
	frame = renderRun(st, v, 120, 24, t0, true)
	lines = checkFrame(t, "입력 완료", frame, 120, 24)
	text = strings.Join(lines, "\n")
	if !strings.Contains(text, "입력됨: end_year=2026 end_month=11 end_day=07 end_hour=18 end_min=00") || !strings.Contains(text, "c 로 제출") {
		t.Errorf("입력 완료 줄 없음:\n%s", text)
	}
	// 좁은 화면
	checkFrame(t, "좁은 입력", renderRun(st, viewState{NoColor: true, InputQ: "q9", InputBuf: "20"}, 40, 14, t0, true), 40, 14)
}

func TestRenderBusyQuestion(t *testing.T) {
	st := sampleState()
	st.Question = &question{ID: "q4", Stage: stageLoad, Kind: "busy",
		Text:    "stress-ng 테스트: 작업 템플릿의 인벤토리가 DEFAULT 가 아니어서 멈췄습니다\n템플릿 stress(7) 인벤토리가 hong 입니다",
		Choices: []choice{{"r", "다시 확인"}, {"x", "이 묶음 중단"}}}
	lines := checkFrame(t, "busy", renderRun(st, viewState{NoColor: true}, 100, 20, t0, true), 100, 20)
	text := strings.Join(lines, "\n")
	for _, w := range []string{"-- 질문 q4 (부하 단계) --", "템플릿 stress(7) 인벤토리가 hong 입니다", "[r] 다시 확인", "[x] 이 묶음 중단", "S 작업 중지"} {
		if !strings.Contains(text, w) {
			t.Errorf("없음: %q\n%s", w, text)
		}
	}
	plain := renderPlain(inputState(), true, t0)
	if !strings.Contains(plain, "입력 항목: end_year(연도(4자리))") {
		t.Errorf("plain 에 입력 항목 안내 없음:\n%s", plain)
	}
}
