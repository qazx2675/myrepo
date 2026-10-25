package tui

// render.go - 전체 화면 렌더링 공통 부품(순수 함수): 동아시아 폭 계산, 제어문자 제거, 색 조각(seg), 줄바꿈, 스크롤 창, 키 안내 줄.
// auto_setup render.go 의 같은 이름 함수들을 옮겨 온 것(같은 작성자). 외부 라이브러리(go-runewidth 등) 없이 자체 표를 쓴다.

import (
	"strings"
)

// ---- 표시 폭 (동아시아 너비: 한글/CJK/전각 = 2칸) ----

// runeWidth: 터미널 표시 칸 수 (제어문자·결합문자·제로폭 0, 한글/CJK/전각 2, 나머지 1)
func runeWidth(r rune) int {
	switch {
	case r == 0 || r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case r >= 0x0300 && r <= 0x036f, r >= 0x200b && r <= 0x200f, r >= 0xfe00 && r <= 0xfe0f, r == 0xfeff:
		return 0
	case r >= 0x1100 && r <= 0x115f, // 한글 자모
		r >= 0x2e80 && r <= 0x303e, // CJK 부수·기호
		r >= 0x3041 && r <= 0x33ff, // 가나·한글 호환 자모·CJK 호환
		r >= 0x3400 && r <= 0x4dbf,
		r >= 0x4e00 && r <= 0x9fff, // CJK 통합 한자
		r >= 0xa000 && r <= 0xa4cf,
		r >= 0xa960 && r <= 0xa97f,
		r >= 0xac00 && r <= 0xd7a3, // 한글 음절
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff00 && r <= 0xff60, // 전각
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f,
		r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

func strWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// cleanText: 제어문자(ESC 포함)를 '?' 로 바꿔 터미널 제어 시퀀스 주입을 막는다 (데몬에서 온 이름·사유·caller 는 외부 값)
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return '?'
		}
		return r
	}, s)
}

// truncW: 표시 폭 w 안으로 줄이고 잘렸으면 끝을 … 로
func truncW(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if strWidth(s) <= w {
		return s
	}
	var sb strings.Builder
	used := 0
	for _, r := range s {
		rw := runeWidth(r)
		if used+rw > w-1 {
			break
		}
		sb.WriteRune(r)
		used += rw
	}
	sb.WriteString("…")
	return sb.String()
}

// padR / padL: 폭 w 로 줄이고(…) 공백 채움
func padR(s string, w int) string {
	s = truncW(cleanText(s), w)
	if n := w - strWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

func padL(s string, w int) string {
	s = truncW(cleanText(s), w)
	if n := w - strWidth(s); n > 0 {
		s = strings.Repeat(" ", n) + s
	}
	return s
}

func rMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func rMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// stripANSI: 테스트·길이 계산용 ANSI 시퀀스(CSI) 제거
func stripANSI(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

// ---- 색 ----

const (
	stGreen  = "32"       // 승인/작업완료, 성공
	stCyan   = "36"       // 승인(시작 전)
	stCyanB  = "1;36"     // 승인/작업중
	stGray   = "90"       // 0 개·비활성
	stYellow = "33"       // 대기, 안내
	stOrange = "38;5;208" // 승인/작업중단
	stRed    = "1;31"     // 중단, 오류
	stBold   = "1"        // 제목·전체
	stHead   = "1;90"     // 표 머리
	stMagent = "1;35"     // 패스워드 변경 키
	stBlue   = "1;34"     // 전체 보기 키
	stGreenB = "1;32"     // 데몬 실행
	stYelB   = "1;33"     // 사용자 표시
	stSel    = "\x1b[7m"  // 선택 행 시작(반전)
	stReset  = "\x1b[0m"  // 끝
)

type seg struct{ t, st string }

func paint(color bool, st, t string) string {
	if !color || st == "" || t == "" {
		return t
	}
	return "\x1b[" + st + "m" + t + "\x1b[0m"
}

// segsText: 세그먼트 연결(색 없음)
func segsText(segs []seg) string {
	var sb strings.Builder
	for _, s := range segs {
		sb.WriteString(s.t)
	}
	return sb.String()
}

// segLine: 세그먼트를 폭 width 안으로 줄여 한 줄로
func segLine(color bool, width int, segs ...seg) string {
	var sb strings.Builder
	left := width
	for _, s := range segs {
		if left <= 0 {
			break
		}
		full := cleanText(s.t)
		t := truncW(full, left)
		sb.WriteString(paint(color, s.st, t))
		if t != full { // 잘렸으면 뒤 세그먼트는 버린다 (… 가 반복되지 않게)
			break
		}
		left -= strWidth(t)
	}
	return sb.String()
}

// selLine: 선택 행 — 색이 있으면 반전(폭 전체), 없으면 그대로(마커 '>' 로 구분)
func selLine(color bool, width int, segs []seg) string {
	if !color {
		return segLine(false, width, segs...)
	}
	t := truncW(cleanText(segsText(segs)), width)
	if n := width - strWidth(t); n > 0 {
		t += strings.Repeat(" ", n)
	}
	return stSel + t + stReset
}

// ---- 줄바꿈 (좁은 화면에서 … 로 자르지 않고 다음 줄로) ----

// atom: 줄바꿈 단위 — glue(앞 간격, 줄 첫머리에 오면 버림) + 내용(한 줄보다 길 때만 글자 단위로 나눔)
type atom struct {
	glue seg
	segs []seg
}

// splitAtoms: 세그먼트 한 줄을 gap 칸 이상 이어진 공백마다 atom 으로 나눈다 (gap 1 = 단어, 2 = 두 칸 간격 항목).
// 줄 맨 앞 공백은 lead 로 돌려주고, 줄 끝 공백은 버린다. 제어문자는 cleanText 로 바꾼다.
func splitAtoms(segs []seg, gap int) (lead string, atoms []atom) {
	var cur atom
	var sp strings.Builder // 아직 어디에 붙을지 모르는 공백
	spSt, started := "", false
	add := func(t, st string) {
		if n := len(cur.segs); n > 0 && cur.segs[n-1].st == st {
			cur.segs[n-1].t += t
			return
		}
		cur.segs = append(cur.segs, seg{t, st})
	}
	for _, s := range segs {
		for _, r := range cleanText(s.t) {
			if r == ' ' {
				if sp.Len() == 0 {
					spSt = s.st
				}
				sp.WriteRune(r)
				continue
			}
			if sp.Len() > 0 {
				switch {
				case !started:
					lead = sp.String()
				case sp.Len() >= gap:
					atoms = append(atoms, cur)
					cur = atom{glue: seg{sp.String(), spSt}}
				default:
					add(sp.String(), spSt)
				}
				sp.Reset()
			}
			started = true
			add(string(r), s.st)
		}
	}
	if started {
		atoms = append(atoms, cur)
	}
	return lead, atoms
}

// mergeSegs: 빈 세그먼트를 버리고 같은 색이 이어지면 하나로
func mergeSegs(segs []seg) []seg {
	var out []seg
	for _, s := range segs {
		if s.t == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].st == s.st {
			out[n-1].t += s.t
			continue
		}
		out = append(out, s)
	}
	return out
}

// wrapAtoms: atom 을 폭 width 안의 줄들로 배치한다. 첫 줄은 first, 이어지는 줄은 cont(공백) 로 시작한다.
// 빈 줄에도 안 들어가는 atom 은 글자 단위로 나눈다 (… 로 자르지 않음).
func wrapAtoms(width int, first, cont string, atoms []atom) [][]seg {
	var out [][]seg
	cur := []seg{{first, ""}}
	used, empty := strWidth(first), true
	newLine := func() {
		out = append(out, mergeSegs(cur))
		cur = []seg{{cont, ""}}
		used, empty = strWidth(cont), true
	}
	for _, a := range atoms {
		w := strWidth(segsText(a.segs))
		if !empty && used+strWidth(a.glue.t)+w <= width {
			cur = append(append(cur, a.glue), a.segs...)
			used += strWidth(a.glue.t) + w
			continue
		}
		if !empty {
			newLine()
		}
		if used+w <= width {
			cur = append(cur, a.segs...)
			used += w
			empty = false
			continue
		}
		for _, s := range a.segs {
			var sb strings.Builder
			for _, r := range s.t {
				if rw := runeWidth(r); used+rw > width && !empty {
					cur = append(cur, seg{sb.String(), s.st})
					sb.Reset()
					newLine()
				}
				sb.WriteRune(r)
				used += runeWidth(r)
				empty = false
			}
			cur = append(cur, seg{sb.String(), s.st})
		}
	}
	return append(out, mergeSegs(cur))
}

// wrapSegLines: 세그먼트 한 줄이 폭에 들어가면 그대로, 아니면 gap 칸 이상 공백(항목 경계)에서 줄바꿈.
// 이어지는 줄은 cont 로 시작하고, 한 줄보다 긴 항목은 단어로, 긴 단어는 글자 단위로 나눈다.
func wrapSegLines(width, gap int, cont string, segs []seg) [][]seg {
	if strWidth(cleanText(segsText(segs))) <= width {
		return [][]seg{segs}
	}
	lead, atoms := splitAtoms(segs, gap)
	if gap > 1 {
		max := width - strWidth(cont)
		var out []atom
		for _, a := range atoms {
			if strWidth(segsText(a.segs)) <= max {
				out = append(out, a)
				continue
			}
			_, sub := splitAtoms(a.segs, 1)
			if len(sub) > 0 {
				sub[0].glue = a.glue
			}
			out = append(out, sub...)
		}
		atoms = out
	}
	return wrapAtoms(width, lead, cont, atoms)
}

// wrapSegs: wrapSegLines 의 각 줄을 렌더
func wrapSegs(color bool, width, gap int, cont string, segs ...seg) []string {
	var out []string
	for _, l := range wrapSegLines(width, gap, cont, segs) {
		out = append(out, segLine(color, width, l...))
	}
	return out
}

// wrapText: 한 가지 색의 글 한 줄을 단어 단위로 줄바꿈 (이어지는 줄은 cont 로 시작)
func wrapText(color bool, width int, st, cont, text string) []string {
	return wrapSegs(color, width, 1, cont, seg{text, st})
}

// ---- 공통 조각 ----

func sepLine(width int) string { return strings.Repeat("-", width) }

// clampWidth: 최소 폭 40 (그보다 좁아도 40칸으로 그린다 — 터미널이 줄을 접는다)
func clampWidth(width int) int {
	if width < 40 {
		return 40
	}
	return width
}

// window: 본문 블록(행 하나 = 블록 하나, 줄바꿈된 행은 여러 줄) 중 cursor 블록 전체가 보이는 avail 줄.
// 블록 중간에서 시작하지 않는다 (커서 블록이 avail 보다 길면 그 블록의 처음부터).
func window(blocks [][]string, cursor, avail int) []string {
	var lines []string
	var starts []int
	for _, b := range blocks {
		starts = append(starts, len(lines))
		lines = append(lines, b...)
	}
	if avail <= 0 || len(lines) <= avail {
		return lines
	}
	cs, ce := 0, 0
	if cursor >= 0 && cursor < len(blocks) {
		cs = starts[cursor]
		ce = cs + len(blocks[cursor])
	}
	top := 0
	if ce > avail {
		top = ce - avail
	}
	if top > cs {
		top = cs
	}
	if top+avail > len(lines) {
		top = len(lines) - avail
	}
	for _, st := range starts {
		if st >= top {
			top = rMin(st, cs)
			break
		}
	}
	return lines[top:rMin(top+avail, len(lines))]
}

func flatLines(blocks [][]string) []string {
	var out []string
	for _, b := range blocks {
		out = append(out, b...)
	}
	return out
}

// finish: 머리 + (스크롤된) 본문 + 꼬리 조립. height<=0 이면 꼬리 없이 전체.
// 머리·꼬리만으로 높이를 넘는 아주 작은 창에서는 아래를 잘라 화면이 밀리지 않게 한다.
func finish(head []string, body [][]string, cursor, height int, foot []string) string {
	if height <= 0 {
		return strings.Join(append(head, flatLines(body)...), "\n")
	}
	foot = strings.Split(strings.Join(foot, "\n"), "\n")
	avail := rMax(height-len(head)-len(foot), 1)
	lines := append(append([]string{}, head...), window(body, cursor, avail)...)
	for len(lines) < height-len(foot) { // 꼬리(키 안내)는 항상 화면 맨 아래
		lines = append(lines, "")
	}
	lines = append(lines, foot...)
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// ---- 키 안내 줄 색 ----

// hintStyles: 키 안내 항목(첫 토큰 = 키)별 색. 이동 = 청록, c(취소) = 빨강, p = 자홍, a = 파랑, 나머지 = 굵게.
var hintStyles = map[string]string{
	"↑↓": "1;36", "←→": "1;36", "←": "1;36", "Enter": "1;36", "Esc": "1;36", "Ctrl+D": "1;36",
	"c": stRed, "d": stRed, "x": stRed, "p": stMagent, "a": stBlue, "s": "1;33",
}

// hintSegs: " ↑↓ 이동  Enter 상세 …" 를 항목(두 칸 간격)별로 나눠 색을 입힌다 (색이 꺼지면 글자만 이어 붙임)
func hintSegs(hint string) []seg {
	var out []seg
	for i, item := range strings.Split(strings.TrimSpace(hint), "  ") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		sep := "  "
		if i == 0 {
			sep = " "
		}
		style := stBold
		if f := strings.Fields(item); len(f) > 0 {
			if s, ok := hintStyles[f[0]]; ok {
				style = s
			}
		}
		out = append(out, seg{sep, ""}, seg{item, style})
	}
	return out
}

// hintLines: 키 안내를 폭에 맞춰 줄로 나눈다. hint 의 "\n" 은 묶음 경계다. 전체가 한 줄에 들어가면 한 줄로 합치고,
// 아니면 묶음마다 한 줄로 두며, 묶음이 폭보다 길면 항목(두 칸 간격) 단위로 줄바꿈한다.
func hintLines(color bool, width int, hint string) []string {
	groups := strings.Split(hint, "\n")
	flat := strings.Join(groups, "  ")
	if len(groups) == 1 || strWidth(flat)+1 <= width {
		return wrapHint(color, width, flat)
	}
	var out []string
	for _, g := range groups {
		out = append(out, wrapHint(color, width, g)...)
	}
	return out
}

// wrapHint: 항목(두 칸 간격) 단위로 줄바꿈 (이어지는 줄은 한 칸 들여씀, 한 줄보다 긴 항목은 단어 단위)
func wrapHint(color bool, width int, hint string) []string {
	return wrapSegs(color, width, 2, " ", hintSegs(hint)...)
}
