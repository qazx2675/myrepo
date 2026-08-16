// describe.go: 실제로 바꾸기 전에 "무엇을 어떻게 바꿀지"를 사람이 읽을 수 있게 출력한다.
// 여기 출력된 내용과 실제 적용 값은 같은 VMFix에서 나오므로 항상 일치한다.
package fixer

import (
	"fmt"
	"io"
	"sort"

	"vm-param-check/model"
)

// Describe는 dry-run 출력을 쓴다. 사용자 확인(y/N) 직전에 호출하는 용도다.
func Describe(w io.Writer, plan *Plan) {
	fmt.Fprintln(w, "=== 변경 예정 내역 (dry-run) ===")

	if len(plan.Fixes) == 0 {
		fmt.Fprintln(w, "자동교정 대상이 없습니다.")
	}

	total := 0
	for _, fix := range plan.Fixes {
		fmt.Fprintf(w, "\n[%s] (%s) 변경 %d건\n", fix.VM, fix.Group, len(fix.Changes))
		for _, c := range fix.Changes {
			fmt.Fprintf(w, "  - %s %s: %s -> %s\n", c.Where, c.Key, displayFrom(c.From), c.To)
		}
		total += len(fix.Changes)
	}

	fmt.Fprintf(w, "\n합계: VM %d대, 변경 %d건\n", len(plan.Fixes), total)

	for _, warn := range plan.Warnings {
		fmt.Fprintf(w, "[경고] %s\n", warn)
	}

	if len(plan.Manual) > 0 {
		fmt.Fprintf(w, "\n=== 자동교정 대상 아님 — 수동조치 필요 (%d건) ===\n", len(plan.Manual))
		for _, line := range summarizeManual(plan.Manual) {
			fmt.Fprintf(w, "  - %s\n", line)
		}
	}
}

func displayFrom(from string) string {
	if from == "" {
		return "(설정없음)"
	}
	return from
}

// summarizeManual은 수동조치 항목을 "항목Key (N대)" 형태로 묶는다 — 대수가 많을 때
// VM별로 전부 나열하면 dry-run 출력이 수백 줄이 되어 정작 변경 내역이 묻힌다.
func summarizeManual(manual []model.Finding) []string {
	count := map[string]int{}
	for _, f := range manual {
		count[f.Key]++
	}
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s (%d대)", k, count[k]))
	}
	return out
}
