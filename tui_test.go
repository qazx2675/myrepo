package main

import (
	"errors"
	"strings"
	"testing"
)

type sent struct {
	answers []string // "id:choice"
	stops   int
	fail    bool
}

func (s *sent) actions() tuiActions {
	return tuiActions{
		answer: func(id, ch string, vals map[string]string) error {
			if s.fail {
				return errors.New("쓰기 실패")
			}
			s.answers = append(s.answers, id+":"+ch)
			return nil
		},
		stop: func() error {
			if s.fail {
				return errors.New("쓰기 실패")
			}
			s.stops++
			return nil
		},
	}
}

func press(v *viewState, st *runState, alive bool, a tuiActions, keys ...string) (quit bool) {
	for _, k := range keys {
		if handleKey(v, st, alive, k, 100, 20, t0, a) {
			return true
		}
	}
	return false
}

func TestParseKeys(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"q", "q"},
		{"cr", "c,r"},
		{"\x1b[A\x1b[B", "up,down"},
		{"\x1b[5~\x1b[6~", "pgup,pgdn"},
		{"\x1b[H\x1b[F\x1bOH", "home,end,home"},
		{"\x1b", "esc"},
		{"\x03", "ctrl-c"},
		{"\r", "enter"},
		{"가S", "가,S"},
		{"\x1b[1;5C", ""}, // 모르는 열은 버림
		{"\x1b[", ""},     // 끝이 잘린 열
	}
	for _, c := range cases {
		if got := strings.Join(parseKeys([]byte(c.in)), ","); got != c.want {
			t.Errorf("parseKeys(%q)=%q, 기대 %q", c.in, got, c.want)
		}
	}
}

func askState() *runState {
	st := sampleState()
	st.Question = &question{ID: "q7", Stage: stageLoad, Text: "t",
		Choices: []choice{{"c", "계속"}, {"r", "재시도"}, {"x", "제외"}, {"s", "중지"}}}
	return st
}

func TestHandleKeyAnswer(t *testing.T) {
	var s sent
	v := viewState{}
	st := askState()
	press(&v, st, true, s.actions(), "c")
	if len(s.answers) != 1 || s.answers[0] != "q7:c" || !strings.Contains(v.Flash, "답을 보냈습니다") {
		t.Errorf("c: %v flash=%q", s.answers, v.Flash)
	}
	// 질문에 없는 선택지(대문자 등)는 무시
	press(&v, st, true, s.actions(), "z", "C", "S")
	if len(s.answers) != 1 || s.stops != 0 {
		t.Errorf("엉뚱한 키가 전송됨: %v %d", s.answers, s.stops)
	}
	// 질문에 x 가 없으면 x 는 무시
	st.Question.Choices = []choice{{"c", "계속"}, {"s", "중지"}}
	press(&v, st, true, s.actions(), "x", "r")
	if len(s.answers) != 1 {
		t.Errorf("없는 선택지가 전송됨: %v", s.answers)
	}
	// 워커가 없으면 보내지 않음
	press(&v, st, false, s.actions(), "c")
	if len(s.answers) != 1 || !strings.Contains(v.Flash, "워커가 실행 중이 아니어서") {
		t.Errorf("워커 없음: %v %q", s.answers, v.Flash)
	}
	// 쓰기 실패 알림
	s.fail = true
	press(&v, st, true, s.actions(), "c")
	if !strings.Contains(v.Flash, "실패") {
		t.Errorf("flash=%q", v.Flash)
	}
}

func TestHandleKeyStopFromQuestion(t *testing.T) {
	var s sent
	v := viewState{}
	st := askState()
	press(&v, st, true, s.actions(), "s")
	if v.Confirm != "s" || v.ConfirmQ != "q7" || len(s.answers) != 0 {
		t.Fatalf("확인 대기 아님: %+v %v", v, s.answers)
	}
	press(&v, st, true, s.actions(), "n") // 취소
	if v.Confirm != "" || len(s.answers) != 0 || v.Flash != "취소했습니다" {
		t.Errorf("취소: %+v %v", v, s.answers)
	}
	press(&v, st, true, s.actions(), "s", "y")
	if len(s.answers) != 1 || s.answers[0] != "q7:s" || v.Confirm != "" {
		t.Errorf("y: %+v %v", v, s.answers)
	}
}

func TestHandleKeyStopWithoutQuestion(t *testing.T) {
	var s sent
	v := viewState{}
	st := sampleState()
	// 질문이 없으면 s 는 아무 일 없음, S 는 확인 후 stop.req
	press(&v, st, true, s.actions(), "s")
	if v.Confirm != "" || s.stops != 0 {
		t.Errorf("s: %+v", v)
	}
	press(&v, st, true, s.actions(), "S")
	if v.Confirm != "S" {
		t.Fatalf("S 확인 대기 아님: %+v", v)
	}
	press(&v, st, true, s.actions(), "x") // y 가 아니면 취소
	if s.stops != 0 || v.Confirm != "" {
		t.Errorf("취소 실패: %+v %d", v, s.stops)
	}
	press(&v, st, true, s.actions(), "S", "Y")
	if s.stops != 1 {
		t.Errorf("stop.req 요청 안 함: %d", s.stops)
	}
	// 워커 없음 / 끝난 작업
	press(&v, st, false, s.actions(), "S")
	if v.Confirm != "" || !strings.Contains(v.Flash, "실행 중이 아닙니다") {
		t.Errorf("워커 없음: %+v", v)
	}
	done := sampleState()
	done.Stage = stageFinish
	press(&v, done, true, s.actions(), "S")
	if v.Confirm != "" || !strings.Contains(v.Flash, "끝난 작업") {
		t.Errorf("끝난 작업: %+v", v)
	}
	// 질문이 떠 있을 때 S 는 안내만
	press(&v, askState(), true, s.actions(), "S")
	if v.Confirm != "" || !strings.Contains(v.Flash, "질문에 답하세요") {
		t.Errorf("질문 중 S: %+v", v)
	}
}

func TestHandleKeyViewKeys(t *testing.T) {
	var s sent
	st := sampleState()
	st.Hosts = nil
	for i := 0; i < 40; i++ {
		st.Hosts = append(st.Hosts, hostRow{Host: "h" + formatNum(i, 2), Status: stDone, Check: "O", CPU: "O", Stress: cpuNotRun})
	}
	v := viewState{}
	capRows := tableCapacity(st, v, 100, 20, t0, true)
	press(&v, st, true, s.actions(), "down", "down")
	if v.Scroll != 2 {
		t.Errorf("down: %d", v.Scroll)
	}
	press(&v, st, true, s.actions(), "pgdn")
	if v.Scroll != 2+capRows {
		t.Errorf("pgdn: %d (capacity %d)", v.Scroll, capRows)
	}
	press(&v, st, true, s.actions(), "end")
	if v.Scroll != 40-capRows {
		t.Errorf("end: %d", v.Scroll)
	}
	press(&v, st, true, s.actions(), "down")
	if v.Scroll != 40-capRows {
		t.Errorf("끝에서 down: %d", v.Scroll)
	}
	press(&v, st, true, s.actions(), "home", "up")
	if v.Scroll != 0 {
		t.Errorf("home/up: %d", v.Scroll)
	}
	press(&v, st, true, s.actions(), "down", "f")
	if !v.OnlyBad || v.Scroll != 0 {
		t.Errorf("f: %+v", v)
	}
	press(&v, st, true, s.actions(), "f")
	if v.OnlyBad {
		t.Error("f 를 다시 누르면 전체")
	}
	// 도움말: ? 로 열고 아무 키로 닫음(닫는 키는 다른 동작을 하지 않는다)
	press(&v, st, true, s.actions(), "?")
	if !v.Help {
		t.Fatal("? 로 도움말이 안 열림")
	}
	if press(&v, st, true, s.actions(), "q") || v.Help {
		t.Errorf("도움말 중 q 는 도움말만 닫아야 함: %+v", v)
	}
	// q / Ctrl-C 는 TUI 만 끝냄
	if !press(&v, st, true, s.actions(), "q") || !press(&v, st, true, s.actions(), "ctrl-c") {
		t.Error("q 로 끝나지 않음")
	}
	if len(s.answers) != 0 || s.stops != 0 {
		t.Error("나가기가 작업에 영향을 줌")
	}
	// 상태가 아직 없어도(nil) 죽지 않음
	press(&viewState{}, nil, false, s.actions(), "down", "c", "S")
}

func TestToggleResultKey(t *testing.T) {
	st := sampleState()
	v := viewState{}
	press(&v, st, true, (&sent{}).actions(), "v")
	if !v.Result {
		t.Errorf("v 로 켜져야 함")
	}
	press(&v, st, true, (&sent{}).actions(), "v")
	if v.Result {
		t.Errorf("v 로 다시 꺼져야 함")
	}
	// 질문이 떠 있어도 v 는 화면 토글이고 답으로 가지 않는다(선택지 키 c r x s y n 과 겹치지 않음)
	st.Question = &question{ID: "q4", Stage: stageLoad, Choices: []choice{{Key: "c"}, {Key: "r"}, {Key: "x"}, {Key: "s"}}}
	s := &sent{}
	press(&v, st, true, s.actions(), "v")
	if !v.Result || len(s.answers) != 0 {
		t.Errorf("질문 중 v: Result=%v answers=%v", v.Result, s.answers)
	}
	// 상태를 아직 못 읽었어도(st == nil) 토글된다
	v = viewState{}
	press(&v, nil, true, (&sent{}).actions(), "v")
	if !v.Result {
		t.Errorf("st nil 에서도 v 가 켜져야 함")
	}
}
