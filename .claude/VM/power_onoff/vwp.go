package main

import (
	"fmt"
	"io"
	"strings"
)

// VWPHit 는 VWP 에 해당하는 VM 한 대와 그 사유다.
type VWPHit struct {
	VM      *VMInfo
	Reasons []string // 예: "폴더: DC1/vm/VWP-Prod/svr1 ('VWP-Prod')", "포트그룹: vwp-cae-10-1-2-0"
}

func hasVWP(s string) bool { return strings.Contains(strings.ToLower(s), "vwp") }

// FindVWP 는 폴더 체인(각 폴더 이름)과 포트그룹 이름에 vwp(대소문자 무시)가 든 VM 만 입력 순서대로 돌려준다.
func FindVWP(vms []*VMInfo) []VWPHit {
	var hits []VWPHit
	for _, vm := range vms {
		if vm == nil {
			continue
		}
		var reasons []string
		for _, f := range strings.Split(vm.FolderPath, "/") {
			if f != "" && hasVWP(f) {
				reasons = append(reasons, fmt.Sprintf("폴더: %s ('%s')", vm.FolderPath, f))
			}
		}
		for _, pg := range vm.PortGroups {
			if hasVWP(pg) {
				reasons = append(reasons, "포트그룹: "+pg)
			}
		}
		if len(reasons) > 0 {
			hits = append(hits, VWPHit{VM: vm, Reasons: reasons})
		}
	}
	return hits
}

// displayWidth 는 터미널 표시 폭(한글 등 넓은 문자는 2칸)을 대략 계산한다.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1100 {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// PrintVWPBanner 는 계획서 9장 형식의 경고 배너를 출력한다(담당자 확인 문구 프롬프트는 호출자 몫).
func PrintVWPBanner(w io.Writer, hits []VWPHit, total int, color bool) {
	lines := []string{
		"[경고] vwp대상 장비가 포함되어있습니다.",
		"       vwp장비의 경우 vwp담당자에게 확인이 필요합니다.",
	}
	inner := 0
	for _, l := range lines {
		if n := displayWidth(l); n > inner {
			inner = n
		}
	}
	inner += 2
	border := strings.Repeat("!", inner+6)
	const on, off = "\x1b[1;97;41m", "\x1b[0m"
	emit := func(s string) {
		if color {
			fmt.Fprintf(w, "%s%s%s\n", on, s, off)
		} else {
			fmt.Fprintln(w, s)
		}
	}
	emit(border)
	for _, l := range lines {
		emit("!!  " + l + strings.Repeat(" ", inner-2-displayWidth(l)) + "!!")
	}
	emit(border)

	nameW := 0
	for _, h := range hits {
		if n := displayWidth(h.VM.Name); n > nameW {
			nameW = n
		}
	}
	for _, h := range hits {
		pad := strings.Repeat(" ", nameW-displayWidth(h.VM.Name))
		for i, r := range h.Reasons {
			if i == 0 {
				fmt.Fprintf(w, "  %s%s   %s\n", h.VM.Name, pad, r)
			} else {
				fmt.Fprintf(w, "  %s   %s\n", strings.Repeat(" ", nameW), r)
			}
		}
	}
	fmt.Fprintf(w, "  (VWP 해당 %d대 / 전체 %d대)\n", len(hits), total)
}
