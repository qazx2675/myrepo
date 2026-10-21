package main

// lineout.go — 결과 줄(ResultLine)을 색상 있는 한 줄 텍스트로 만든다 (계획서_v0.2_TUI.md "출력 줄 형식").
// 형식: [태그] host<TAB>ip<TAB>vendor<TAB>model<TAB>bios_version<TAB>attribute<TAB>기준값<TAB>현재값<TAB>비고

import (
	"bufio"
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
	if lineIsError(l) {
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

// lineSafe 는 BMC·호스트에서 온 글자 안의 제어 문자(ESC 를 포함한 C0, DEL, C1 — TAB·CR·LF 도)를 공백으로 바꿉니다.
// 터미널 이스케이프 주입을 막고, 칸 구분(TAB)과 한 줄 형식을 지키기 위한 것입니다. 색 코드는 이 뒤에 formatLine 이 붙인다.
func lineSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
}

// formatLine 은 결과 줄 1개를 TAB 으로 나눈 한 줄로 만듭니다 (개행 없음). 기준값 칸은 항상 초록입니다
// (호스트 오류 줄도 — 그 줄은 태그·현재값·비고만 빨강). 기준값·현재값의 "-"·빈 값은 (N/A) 로 보여 줍니다.
// 모든 칸은 lineSafe 로 제어 문자를 지운 뒤 색을 입힙니다.
func formatLine(l ResultLine, color bool) string {
	paint := func(c, s string) string {
		if !color {
			return s
		}
		return c + s + lnReset
	}
	c := lineColor(l)
	note := lineSafe(l.Note)
	if lineIsError(l) {
		note = paint(lnRed, note)
	}
	cols := []string{
		paint(c, "["+lineSafe(l.Tag)+"]") + " " + lineSafe(l.Host),
		lineSafe(l.IP), lineSafe(l.Vendor), lineSafe(l.Model), lineSafe(l.BiosVersion), lineSafe(l.Attr),
		paint(lnGreen, lineSafe(showValue(l.Expected))),
		paint(c, lineSafe(showValue(l.Current))),
		note,
	}
	return strings.Join(cols, "\t")
}

// writeResultLines 는 줄들을 한 줄씩 w 에 씁니다 (TUI 가 아닌 평문 출력용).
// 이름이 writeLines 가 아닌 것은 check.go 의 writeLines(파일 쓰기)와 겹치지 않게 하려는 것입니다.
// 줄마다 write 를 부르지 않게 버퍼로 모아 끝에 한 번에 내보낸다 (전체비교 7.5만 줄을 ssh·파이프로 볼 때).
func writeResultLines(w io.Writer, lines []ResultLine, color bool) {
	bw := bufio.NewWriterSize(w, 64<<10)
	for _, l := range lines {
		fmt.Fprintln(bw, formatLine(l, color))
	}
	bw.Flush()
}

// colorEnabled 는 f 에 색을 써도 되는지 봅니다: NO_COLOR 가 있거나 터미널이 아니면 false.
func colorEnabled(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || f == nil {
		return false
	}
	return ttyIsTerminal(int(f.Fd()))
}
