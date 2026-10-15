package main

// lineout.go — 결과 줄(ResultLine)을 색상 있는 한 줄 텍스트로 만든다 (계획서_v0.2_TUI.md "출력 줄 형식").
// 형식: [태그] host<TAB>ip<TAB>vendor<TAB>model<TAB>bios_version<TAB>attribute<TAB>기준값<TAB>현재값<TAB>비고

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI 색 (밝은 검정 90 = 회색)
const (
	lnGreen  = "\x1b[32m"
	lnYellow = "\x1b[33m"
	lnGray   = "\x1b[90m"
	lnRed    = "\x1b[31m"
	lnReset  = "\x1b[0m"
)

// lineColor 는 줄의 태그·현재값 색입니다: 호스트 오류는 빨강, 그 외는 태그별 (OK 초록 / difference 노랑 / N/A 회색).
func lineColor(l ResultLine) string {
	if l.HostError {
		return lnRed
	}
	switch l.Tag {
	case TagOK:
		return lnGreen
	case TagDiff:
		return lnYellow
	default:
		return lnGray
	}
}

// formatLine 은 결과 줄 1개를 TAB 으로 나눈 한 줄로 만듭니다 (개행 없음). 기준값 칸은 항상 초록입니다.
// 기준값·현재값의 "-"·빈 값은 (N/A) 로 보여 줍니다.
func formatLine(l ResultLine, color bool) string {
	paint := func(c, s string) string {
		if !color {
			return s
		}
		return c + s + lnReset
	}
	c := lineColor(l)
	cols := []string{
		paint(c, "["+l.Tag+"]") + " " + l.Host,
		l.IP, l.Vendor, l.Model, l.BiosVersion, l.Attr,
		paint(lnGreen, showValue(l.Expected)),
		paint(c, showValue(l.Current)),
		l.Note,
	}
	return strings.Join(cols, "\t")
}

// writeResultLines 는 줄들을 한 줄씩 w 에 씁니다 (TUI 가 아닌 평문 출력용).
// 이름이 writeLines 가 아닌 것은 check.go 의 writeLines(파일 쓰기)와 겹치지 않게 하려는 것입니다.
func writeResultLines(w io.Writer, lines []ResultLine, color bool) {
	for _, l := range lines {
		fmt.Fprintln(w, formatLine(l, color))
	}
}

// colorEnabled 는 f 에 색을 써도 되는지 봅니다: NO_COLOR 가 있거나 터미널이 아니면 false.
func colorEnabled(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || f == nil {
		return false
	}
	return ttyIsTerminal(int(f.Fd()))
}
