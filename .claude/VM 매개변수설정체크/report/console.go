package report

import (
	"fmt"
	"io"
	"sort"

	"vm-param-check/model"
)

// ANSI 색상 코드. color=false(-noColor)면 아래 함수들이 그냥 원본 문자열을 반환한다.
const (
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiReset  = "\033[0m"
)

func colorize(s, code string, color bool) string {
	if !color {
		return s
	}
	return code + s + ansiReset
}

// PrintConsole은 [1] VM별 요약 표 + [2] FAIL/설정없음만 모은 상세 섹션을 출력한다.
// color=true면 FAIL은 빨간색, OK/PASS는 초록색, 설정없음은 노란색으로 강조한다
// (컬러 미지원 터미널이나 파일로 리다이렉트할 때는 -noColor로 끌 수 있음).
func PrintConsole(w io.Writer, findings []model.Finding, color bool) {
	statuses := Summarize(findings)

	byVM := map[string][]model.Finding{}
	for _, f := range findings {
		byVM[f.VM] = append(byVM[f.VM], f)
	}

	fmt.Fprintln(w, "=== [1] VM별 요약 ===")
	fmt.Fprintf(w, "%-30s %-8s %6s %6s %8s %6s\n", "VM", "전체결과", "OK", "FAIL", "설정없음", "정보")
	fmt.Fprintln(w, "----------------------------------------------------------------------")
	pass := 0
	for _, s := range statuses {
		// 정렬이 깨지지 않도록 폭 맞춤(Sprintf)을 먼저 하고, 그 결과 문자열 전체를
		// 컬러 코드로 감싼다 — ANSI 이스케이프는 터미널에서 폭을 차지하지 않지만
		// Printf의 %-Ns는 이스케이프 바이트까지 문자 수로 세서 먼저 감싸면 정렬이 깨진다.
		overallPlain := fmt.Sprintf("%-8s", s.Overall)
		var overallDisp string
		if s.Overall == "PASS" {
			pass++
			overallDisp = colorize(overallPlain, ansiGreen, color)
		} else {
			overallDisp = colorize(overallPlain, ansiRed, color)
		}
		failPlain := fmt.Sprintf("%6d", s.Fail)
		failDisp := colorize(failPlain, ansiRed, color && s.Fail > 0)
		noValuePlain := fmt.Sprintf("%8d", s.NoValue)
		noValueDisp := colorize(noValuePlain, ansiYellow, color && s.NoValue > 0)

		fmt.Fprintf(w, "%-30s %s %6d %s %s %6d\n", s.VM, overallDisp, s.OK, failDisp, noValueDisp, s.Info)
	}
	fmt.Fprintf(w, "\n총 %d대 중 PASS %d대, FAIL %d대\n", len(statuses), pass, len(statuses)-pass)

	problemVMs := 0
	for _, s := range statuses {
		if s.Overall == "FAIL" {
			problemVMs++
		}
	}
	if problemVMs == 0 {
		return
	}

	fmt.Fprintln(w, "\n=== [2] 문제 항목 상세 (FAIL / 설정없음만) ===")
	for _, s := range statuses {
		if s.Overall != "FAIL" {
			continue
		}
		fmt.Fprintf(w, "\n▶ %s (FAIL %d건, 설정없음 %d건)\n", s.VM, s.Fail, s.NoValue)
		items := byVM[s.VM]
		sort.SliceStable(items, func(i, j int) bool { return items[i].Key < items[j].Key })
		for _, f := range items {
			if f.Result != "FAIL" && f.Result != "설정없음" {
				continue
			}
			var tag string
			if f.Result == "설정없음" {
				tag = colorize("[설정없음]    ", ansiYellow, color)
			} else {
				tag = colorize("[FAIL]        ", ansiRed, color)
			}
			src := ""
			if f.Source != "-" {
				src = fmt.Sprintf(" (%s)", f.Source)
			}
			if f.Result == "설정없음" {
				fmt.Fprintf(w, "  %s%s%s: 기대값=%s, 실제값 없음\n", tag, f.Key, src, f.Expected)
			} else {
				fmt.Fprintf(w, "  %s%s%s: 기대값=%s, 실제값=%s\n", tag, f.Key, src, f.Expected, f.Actual)
			}
		}
	}
}
