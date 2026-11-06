package main

import (
	"os"
	"path/filepath"
	"testing"
)

// -doctor: stress-ng 종료 시각(stress_end)과 입력·인벤토리 사용 중 질문을 읽기 전용으로 보여 준다.
func TestDoctorStressEndAndInputQuestion(t *testing.T) {
	x := newDocTest(t)
	hf, dir := x.state(func(s *runState) {
		s.Busy = false
		s.StressEnd = "2026,11,07,18,00"
		s.Question = &question{ID: "q5", Stage: stageLoad, Text: "stress-ng 종료 시각을 입력하세요", Asked: docNow, Inputs: endFields,
			Choices: []choice{{"c", "입력한 값으로 계속"}, {"s", "중지"}}}
	})
	x.alive = true
	_, text := x.run(hf)
	mustContain(t, text, "stress-ng 종료 시각(stress-test -end): 2026-11-07 18:00", "입력값 2026,11,07,18,00",
		"입력 질문입니다", "end_year(연도(4자리))", `"values"`)
	if _, err := os.Stat(filepath.Join(dir, stateFile)); err != nil {
		t.Error(err)
	}
}

func TestDoctorBusyQuestion(t *testing.T) {
	x := newDocTest(t)
	hf, _ := x.state(func(s *runState) {
		s.Busy = false
		s.Question = &question{ID: "q6", Stage: stageLoad, Kind: "busy", Text: "CPU 템플릿 인벤토리 사용 중", Asked: docNow,
			Choices: []choice{{"r", "다시 확인"}, {"x", "이 묶음 중단"}}}
	})
	x.alive = true
	_, text := x.run(hf)
	mustContain(t, text, "질문 q6 에 대한 답을 기다리는 중", "인벤토리 사용 중이라 멈춰 있습니다", "STRESS_TIMEOUT 은 일시정지", "r 다시 확인 / x 이 묶음 중단")
	mustNotContain(t, text, "입력 질문입니다")
}
