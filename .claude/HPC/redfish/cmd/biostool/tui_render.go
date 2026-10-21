package main

// tui_render.go — TUI·평문 화면 조각을 만드는 순수 함수: 한글 표시 폭, 색, 결과 줄·요약 표(칸 맞춤), 탭 줄,
// 키 안내 줄바꿈, 도움말, 비 tty 평문 출력. 표시 폭 함수는 auto_setup render.go 이식 (이름 앞에 tui).

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ---- 표시 폭 (동아시아 너비: 한글/CJK/전각 = 2칸) ----

// tuiRuneWidth 는 터미널 표시 칸 수입니다 (제어문자·결합문자·제로폭 0, 한글/CJK/전각 2, 나머지 1).
func tuiRuneWidth(r rune) int {
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

func tuiStrWidth(s string) int {
	w := 0
	for _, r := range s {
		w += tuiRuneWidth(r)
	}
	return w
}

// tuiClean 은 제어문자(ESC·TAB 포함)를 '?' 로 바꿔 터미널 제어 시퀀스 주입을 막습니다 (값은 BMC·파일에서 온 것).
func tuiClean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return '?'
		}
		return r
	}, s)
}

// tuiTrunc 는 표시 폭 w 안으로 줄이고 잘렸으면 끝을 … 로 바꿉니다.
func tuiTrunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if tuiStrWidth(s) <= w {
		return s
	}
	var sb strings.Builder
	used := 0
	for _, r := range s {
		rw := tuiRuneWidth(r)
		if used+rw > w-1 {
			break
		}
		sb.WriteRune(r)
		used += rw
	}
	sb.WriteString("…")
	return sb.String()
}

// tuiPadR 는 폭 w 로 줄이고(…) 오른쪽을 공백으로 채웁니다.
func tuiPadR(s string, w int) string {
	s = tuiTrunc(tuiClean(s), w)
	if n := w - tuiStrWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// tuiStripANSI 는 ANSI 시퀀스를 지웁니다 (테스트·폭 계산용).
func tuiStripANSI(s string) string {
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

func tuiMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func tuiMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---- 색 ----

const (
	tuiStGreen  = "32"
	tuiStYellow = "33"
	tuiStGray   = "90"
	tuiStRed    = "31"
	tuiStBold   = "1"
	tuiStCyan   = "1;36"
	tuiStRev    = "7"
	tuiStUnder  = "1;4" // 굵은 밑줄 (지금 움직이지 않는 탭 줄의 선택 탭)
)

// tuiSeg 는 색 하나를 가진 글자 조각입니다.
type tuiSeg struct{ t, st string }

func tuiPaint(color bool, st, t string) string {
	if !color || st == "" || t == "" {
		return t
	}
	return "\x1b[" + st + "m" + t + "\x1b[0m"
}

// tuiSegLine 은 조각들을 폭 width 안으로 줄여 한 줄로 만듭니다 (width<=0 이면 자르지 않음).
func tuiSegLine(color bool, width int, segs ...tuiSeg) string {
	var sb strings.Builder
	left := width
	for _, s := range segs {
		full := tuiClean(s.t)
		if width <= 0 {
			sb.WriteString(tuiPaint(color, s.st, full))
			continue
		}
		if left <= 0 {
			break
		}
		t := tuiTrunc(full, left)
		sb.WriteString(tuiPaint(color, s.st, t))
		if t != full { // 잘렸으면 뒤 조각은 버린다 (… 가 반복되지 않게)
			break
		}
		left -= tuiStrWidth(t)
	}
	return sb.String()
}

// ---- 결과 줄·요약 표 ----

// 필터 (TUI 키와 같은 글자)
const (
	tuiFilterAll  = 'a'
	tuiFilterOK   = 'o'
	tuiFilterDiff = 'f'
	tuiFilterNA   = 'n'
	tuiFilterSum  = 's'
)

func tuiFilterName(f rune) string {
	switch f {
	case tuiFilterOK:
		return "OK만"
	case tuiFilterDiff:
		return "difference만"
	case tuiFilterNA:
		return "N/A만"
	case tuiFilterSum:
		return "요약"
	}
	return "전체"
}

// tuiFilterLines 는 필터에 맞는 줄만 고릅니다 (전체·요약은 모두).
func tuiFilterLines(lines []ResultLine, f rune) []ResultLine {
	want := ""
	switch f {
	case tuiFilterOK:
		want = TagOK
	case tuiFilterDiff:
		want = TagDiff
	case tuiFilterNA:
		want = TagNA
	default:
		return lines
	}
	var out []ResultLine
	for _, l := range lines {
		if l.Tag == want {
			out = append(out, l)
		}
	}
	return out
}

// tuiCount 는 태그별 줄 수입니다.
func tuiCount(lines []ResultLine) (ok, diff, na int) {
	for _, l := range lines {
		switch l.Tag {
		case TagOK:
			ok++
		case TagDiff:
			diff++
		default:
			na++
		}
	}
	return
}

// tuiTagStyle 은 태그·현재값 색입니다: 호스트 오류 빨강, OK 초록, difference 노랑, N/A 회색 (XML 비교 제외 줄도 회색).
func tuiTagStyle(l ResultLine) string {
	if lineIsError(l) {
		return tuiStRed
	}
	switch l.Tag {
	case TagOK:
		return tuiStGreen
	case TagDiff:
		return tuiStYellow
	}
	return tuiStGray
}

func tuiDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

var tuiLineHead = []string{"태그", "host", "ip", "vendor", "model", "bios", "attr", "기준값", "현재값", "비고"}

// tuiLineRow 는 결과 줄 1개를 칸으로 나눕니다. 기준값은 항상 초록(호스트 오류 줄도), 태그·현재값은 태그 색,
// 호스트 오류 줄은 태그·현재값·비고가 빨강.
func tuiLineRow(l ResultLine) []tuiSeg {
	tag := tuiTagStyle(l)
	note := ""
	if lineIsError(l) {
		note = tuiStRed
	}
	return []tuiSeg{
		{"[" + l.Tag + "]", tag},
		{tuiDash(l.Host), ""}, {tuiDash(l.IP), ""}, {tuiDash(l.Vendor), ""}, {tuiDash(l.Model), ""},
		{tuiDash(l.BiosVersion), ""}, {tuiDash(l.Attr), ""},
		{showValue(l.Expected), tuiStGreen},
		{showValue(l.Current), tag},
		{tuiDash(l.Note), note},
	}
}

// tuiHostSum 은 요약(s) 화면의 호스트 1줄입니다.
type tuiHostSum struct {
	Host, IP, Model, Mgmt string
	OK, Diff, NA          int
	Err                   string // 호스트 오류 비고 (없으면 "")
}

// tuiSummarize 는 그룹의 줄을 호스트별로 셉니다 (g.Hosts 순서).
func tuiSummarize(g LineGroup) []tuiHostSum {
	idx := map[string]int{}
	out := make([]tuiHostSum, 0, len(g.Hosts))
	for _, h := range g.Hosts {
		idx[h] = len(out)
		out = append(out, tuiHostSum{Host: h})
	}
	for _, l := range g.Lines {
		i, ok := idx[l.Host]
		if !ok {
			idx[l.Host] = len(out)
			i = len(out)
			out = append(out, tuiHostSum{Host: l.Host})
		}
		s := &out[i]
		if s.IP == "" {
			s.IP = l.IP
		}
		if s.Model == "" {
			s.Model = l.Model
		}
		if s.Mgmt == "" {
			s.Mgmt = l.MgmtVersion
			if s.Mgmt == "" || s.Mgmt == "-" {
				s.Mgmt = l.MgmtModel
			}
		}
		if l.HostError {
			s.Err = tuiDash(l.Note)
		}
		switch l.Tag {
		case TagOK:
			s.OK++
		case TagDiff:
			s.Diff++
		default:
			s.NA++
		}
	}
	return out
}

var tuiSummaryHead = []string{"host", "ip", "model", "mgmt", "OK", "difference", "N/A", "비고"}

func tuiSummaryRow(s tuiHostSum) []tuiSeg {
	num := func(n int, st string) tuiSeg {
		if n == 0 {
			st = tuiStGray
		}
		return tuiSeg{strconv.Itoa(n), st}
	}
	plain, errSt := "", tuiStRed
	if s.Err == xmlCompareNote { // XML 대상 비교 제외는 오류가 아님
		errSt = tuiStGray
	} else if s.Err != "" {
		plain = tuiStRed
	}
	return []tuiSeg{
		{tuiDash(s.Host), plain}, {tuiDash(s.IP), plain}, {tuiDash(s.Model), plain}, {tuiDash(s.Mgmt), plain},
		num(s.OK, tuiStGreen), num(s.Diff, tuiStYellow), num(s.NA, tuiStGray),
		{s.Err, errSt},
	}
}

// tuiTable 은 머리 줄과 행들을 칸 너비(표시 폭)에 맞춰 줄로 만듭니다 (칸 사이 2칸, 머리 줄은 굵게).
// width>0 이면 칸 너비 상한을 줄여 가며(최소 6칸, 넘는 값은 …) 폭 안에 맞추고 그래도 넘치면 줄 끝을 자른다.
// width<=0 이면 자르지 않는다 (평문 출력).
func tuiTable(color bool, width int, head []string, rows [][]tuiSeg) []string {
	cw := tuiColWidths(width, head, rows)
	hs := make([]tuiSeg, len(head))
	for c, h := range head {
		hs[c] = tuiSeg{h, tuiStBold}
	}
	out := []string{tuiTableRow(color, width, cw, hs)}
	for _, r := range rows {
		out = append(out, tuiTableRow(color, width, cw, r))
	}
	return out
}

// tuiColWidths 는 tuiTable 의 칸 너비입니다 (TUI 는 이것만 미리 구해 두고 보이는 줄만 tuiTableRow 로 그린다).
func tuiColWidths(width int, head []string, rows [][]tuiSeg) []int {
	n := len(head)
	cw := make([]int, n)
	for c, h := range head {
		cw[c] = tuiStrWidth(h)
	}
	for _, r := range rows {
		for c := 0; c < n && c < len(r); c++ {
			cw[c] = tuiMaxInt(cw[c], tuiStrWidth(tuiClean(r[c].t)))
		}
	}
	if width > 0 {
		total := func(limit int) int {
			t := 2 * (n - 1)
			for _, w := range cw {
				t += tuiMinInt(w, limit)
			}
			return t
		}
		limit := 0
		for _, w := range cw {
			limit = tuiMaxInt(limit, w)
		}
		for limit > 6 && total(limit) > width {
			limit--
		}
		for c := range cw {
			cw[c] = tuiMinInt(cw[c], limit)
		}
	}
	return cw
}

// tuiTableRow 는 칸 너비 cw 에 맞춘 표 한 줄입니다.
func tuiTableRow(color bool, width int, cw []int, cells []tuiSeg) string {
	n := len(cw)
	segs := make([]tuiSeg, 0, 2*n)
	for c := 0; c < n; c++ {
		var cell tuiSeg
		if c < len(cells) {
			cell = cells[c]
		}
		t := tuiPadR(cell.t, cw[c])
		if c == n-1 { // 마지막 칸은 뒤 공백을 채우지 않는다
			t = strings.TrimRight(t, " ")
		}
		if c > 0 {
			segs = append(segs, tuiSeg{"  ", ""})
		}
		segs = append(segs, tuiSeg{t, cell.st})
	}
	return strings.TrimRight(tuiSegLine(color, width, segs...), " ")
}

// tuiLineTable 은 결과 줄 표(머리 줄 + 줄들)입니다.
func tuiLineTable(color bool, width int, lines []ResultLine) []string {
	rows := make([][]tuiSeg, len(lines))
	for i, l := range lines {
		rows[i] = tuiLineRow(l)
	}
	return tuiTable(color, width, tuiLineHead, rows)
}

// tuiSummaryTable 은 호스트별 요약 표(머리 줄 + 호스트들)입니다.
func tuiSummaryTable(color bool, width int, g LineGroup) []string {
	sums := tuiSummarize(g)
	rows := make([][]tuiSeg, len(sums))
	for i, s := range sums {
		rows[i] = tuiSummaryRow(s)
	}
	return tuiTable(color, width, tuiSummaryHead, rows)
}

// tuiCountSegs 는 "OK 3  difference 1  N/A 0" 조각입니다.
func tuiCountSegs(ok, diff, na int) []tuiSeg {
	return []tuiSeg{
		{"OK " + strconv.Itoa(ok), tuiStGreen}, {"  ", ""},
		{"difference " + strconv.Itoa(diff), tuiStYellow}, {"  ", ""},
		{"N/A " + strconv.Itoa(na), tuiStGray},
	}
}

// ---- 탭·키 안내 ----

// tuiTabsLine 은 "   벤더  [HPE] [DELL]" 같은 탭 줄입니다. cur 면 지금 ←→ 로 움직이는 줄이라 앞에 ">>" 를 붙이고
// 이름을 청록으로 칠한다. 선택 탭은 색이 있으면 cur 줄은 반전·아니면 굵은 밑줄, 색이 없으면 앞에 '>'.
func tuiTabsLine(color bool, width int, label string, names []string, sel int, tail string, cur bool) string {
	lead, lst, selSt := "   ", tuiStBold, tuiStUnder
	if cur {
		lead, lst, selSt = ">> ", tuiStCyan, tuiStRev
	}
	segs := []tuiSeg{{lead + label + "  ", lst}}
	for i, n := range names {
		if i > 0 {
			segs = append(segs, tuiSeg{" ", ""})
		}
		t := "[" + n + "]"
		switch {
		case i == sel && color:
			segs = append(segs, tuiSeg{t, selSt})
		case i == sel:
			segs = append(segs, tuiSeg{">" + t, ""})
		default:
			segs = append(segs, tuiSeg{t, ""})
		}
	}
	if tail != "" {
		segs = append(segs, tuiSeg{"   " + tail, tuiStGray})
	}
	return tuiSegLine(color, width, segs...)
}

// tuiWrapItems 는 키 안내 항목("q 종료" 처럼 첫 단어가 키)을 두 칸 간격으로 이어 폭에 맞춰 줄바꿈합니다.
// 키 부분은 청록 굵게. 한 항목이 폭보다 길면 그 줄에서 잘린다.
func tuiWrapItems(color bool, width int, items []string) []string {
	var out []string
	var cur []tuiSeg
	used := 0
	for _, it := range items {
		w := tuiStrWidth(it)
		if len(cur) > 0 && used+2+w > width {
			out = append(out, tuiSegLine(color, width, cur...))
			cur, used = nil, 0
		}
		sep := " "
		if len(cur) > 0 {
			sep = "  "
		}
		key, rest := it, ""
		if i := strings.IndexByte(it, ' '); i > 0 {
			key, rest = it[:i], it[i:]
		}
		cur = append(cur, tuiSeg{sep, ""}, tuiSeg{key, tuiStCyan}, tuiSeg{rest, ""})
		used += len(sep) + w
	}
	if len(cur) > 0 {
		out = append(out, tuiSegLine(color, width, cur...))
	}
	return out
}

// ---- 도움말 ----

func tuiHelpLines() []string {
	return []string{
		" BIOS 점검 결과 화면 — 도움말",
		"",
		"   Tab / Shift+Tab  화면 전환 (프로파일 점검 ↔ 전체 설정 비교, 처음이면 기준 고르고 비교)",
		"   ← →           벤더 이동 ('>>' 줄이 지금 움직이는 줄, 화면마다 위치·필터 유지)",
		"   Enter         벤더 안으로 — ← → 로 관리망(관리 컨트롤러) 그룹 이동",
		"   q             한 단계 뒤로: 관리망 그룹 → 벤더 줄, 미리보기·설정 결과 → 결과 화면",
		"   q q           벤더 줄(맨 위)에서 두 번 — 종료 (설정할 항목이 있으면 먼저 수정 단계, 거기서 취소하면 결과 화면으로)",
		"   Esc           q 와 같이 뒤로, 벤더 줄에서는 종료 확인만 취소 (끝내지 않음)",
		"   r             전체 설정 비교 기준 다시 선택 (전체 설정 비교 화면에서)",
		"   ↑ ↓ (k j)     한 줄 위/아래",
		"   PgUp / PgDn   한 화면 위/아래",
		"   Home / End (g G)  맨 위 / 맨 아래",
		"   a o f n s     보기: 전체 / OK 만 / difference 만 / N/A 만(접속·조회 실패 포함) / 요약(호스트마다 한 줄 건수)",
		"   e             수정 메뉴 (어디서든, 취소하면 결과 화면으로)",
		"   Ctrl+X        진행 중 작업 취소 (결과 화면에서는 끝내지 않음)",
		"   ?             이 도움말   (Ctrl+C / Ctrl+Z 는 무시됩니다)",
		"",
		" 색: [OK] 초록 / [difference] 노랑 / [N/A] 회색 / 접속·조회 실패 빨강. 기준값은 항상 초록. (NO_COLOR 면 색 없음)",
		" 전체 설정 비교에서 기준 서버 자신은 자기 값을 기준값·현재값으로 [OK] (비고 REFERENCE) 로 보입니다.",
		" 그룹: 같은 벤더 안에서 관리 컨트롤러 모델별로 나누되 BIOS 속성 이름 목록이 같으면 하나로 합칩니다 (예: iLO 4+iLO 5).",
		"",
		" 아무 키나 누르면 닫힙니다",
	}
}

// ---- 비 tty 평문 ----

// tuiWritePlain 은 TUI 를 열 수 없을 때(tty 아님 등) 같은 내용을 평문으로 씁니다:
// 제목과 전체 건수, 그리고 벤더/그룹마다 머리 줄 · 호스트 요약표 · 모든 결과 줄. color 가 false 면 ANSI 없음.
func tuiWritePlain(w io.Writer, title string, vgs []VendorGroups, color bool) {
	var all []ResultLine
	for _, vg := range vgs {
		for _, g := range vg.Groups {
			all = append(all, g.Lines...)
		}
	}
	ok, diff, na := tuiCount(all)
	fmt.Fprintln(w, tuiSegLine(color, 0, tuiSeg{title, tuiStBold}))
	fmt.Fprintln(w, tuiSegLine(color, 0, append([]tuiSeg{{"전체: ", ""}}, tuiCountSegs(ok, diff, na)...)...))
	for _, vg := range vgs {
		for _, g := range vg.Groups {
			ok, diff, na := tuiCount(g.Lines)
			head := fmt.Sprintf("== %s / %s (호스트 %d대) ", tuiDash(vg.Vendor), tuiDash(g.Name), len(g.Hosts))
			segs := append([]tuiSeg{{head, tuiStBold}}, tuiCountSegs(ok, diff, na)...)
			fmt.Fprintln(w)
			fmt.Fprintln(w, tuiSegLine(color, 0, append(segs, tuiSeg{" ==", tuiStBold})...))
			fmt.Fprintln(w, "[요약]")
			for _, l := range tuiSummaryTable(color, 0, g) {
				fmt.Fprintln(w, l)
			}
			fmt.Fprintln(w, "[결과]")
			for _, l := range tuiLineTable(color, 0, g.Lines) {
				fmt.Fprintln(w, l)
			}
		}
	}
}
