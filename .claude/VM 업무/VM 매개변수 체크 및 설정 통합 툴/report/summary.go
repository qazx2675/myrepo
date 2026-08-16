// summary.go: findings를 VM 단위로 집계하는 공통 로직.
// console.go(화면 요약 표)와 csv.go(요약 CSV, -onlyFail 필터링)가 전부 이걸 재사용한다
// — 세 군데서 "VM별로 묶어서 OK/FAIL/설정없음/정보 세는" 로직을 따로 구현하면
// 나중에 하나만 고치고 나머지를 놓치기 쉬워서 여기 한 곳으로 모았다.
package report

import (
	"sort"

	"vm-param-check/model"
)

// VMStatus는 VM 1대의 findings를 집계한 결과다.
type VMStatus struct {
	VM      string
	OK      int
	Fail    int
	NoValue int
	Info    int
	Overall string // "PASS" | "FAIL" (FAIL/설정없음이 하나라도 있으면 FAIL)
}

// Summarize는 findings를 VM별로 묶어 VMStatus 목록을 VM명 오름차순으로 반환한다.
func Summarize(findings []model.Finding) []VMStatus {
	order := []string{}
	byVM := map[string]*VMStatus{}
	for _, f := range findings {
		s, ok := byVM[f.VM]
		if !ok {
			s = &VMStatus{VM: f.VM}
			byVM[f.VM] = s
			order = append(order, f.VM)
		}
		switch f.Result {
		case "OK":
			s.OK++
		case "FAIL":
			s.Fail++
		case "설정없음":
			s.NoValue++
		case "정보":
			s.Info++
		}
	}
	sort.Strings(order)

	result := make([]VMStatus, 0, len(order))
	for _, vm := range order {
		s := byVM[vm]
		if s.Fail > 0 || s.NoValue > 0 {
			s.Overall = "FAIL"
		} else {
			s.Overall = "PASS"
		}
		result = append(result, *s)
	}
	return result
}

// FilterOnlyFail은 -onlyFail 모드용 — PASS인 VM의 findings를 통째로 제거하고,
// 문제(FAIL/설정없음)가 하나라도 있는 VM의 findings만 남긴다.
func FilterOnlyFail(findings []model.Finding) []model.Finding {
	statuses := Summarize(findings)
	failVMs := map[string]bool{}
	for _, s := range statuses {
		if s.Overall == "FAIL" {
			failVMs[s.VM] = true
		}
	}
	var out []model.Finding
	for _, f := range findings {
		if failVMs[f.VM] {
			out = append(out, f)
		}
	}
	return out
}
