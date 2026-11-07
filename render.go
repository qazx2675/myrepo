// render.go - TUI 화면 렌더(순수 함수): ViewState + Snapshot -> 한 화면 문자열. 한글 폭 계산 포함.
// 렌더는 보이는 줄만 만든다(가상 스크롤). 필터 결과·패널 줄바꿈은 prepare 가 바뀔 때만 다시 계산한다.
package main

import (
	"fmt"
	"strings"
	"time"
)

// ---- 표시 폭 (auto_setup render.go 에서 옮김: 한글/CJK/전각 = 2칸) ----

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

// cleanText: 제어문자(ESC 포함)를 '?' 로 바꿔 터미널 제어 시퀀스 주입을 막는다 (호스트명·비고는 파일에서 온 값)
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

// padR: 폭 w 로 줄이고(…) 오른쪽을 공백으로 채움
func padR(s string, w int) string {
	s = truncW(cleanText(s), w)
	if n := w - strWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// ---- 색 ----

const (
	stGreen  = "32"
	stCyan   = "36"
	stGray   = "90"
	stYellow = "33"
	stRed    = "1;31"
	stBold   = "1"
)

func paint(color bool, st, t string) string {
	if !color || st == "" || t == "" {
		return t
	}
	return "\x1b[" + st + "m" + t + "\x1b[0m"
}

func statusStyle(s Status) string {
	switch s {
	case StatusInstalling:
		return stCyan
	case StatusDelayed, StatusOff:
		return stYellow
	case StatusDisk, StatusPingX:
		return stRed
	case StatusDone, StatusRebooted:
		return stGreen
	}
	return stGray
}

// ---- 화면 상태 ----

// 패널 종류(키와 같은 글자)
const (
	panelInstall = 'c'
	panelDelay   = 'r'
	panelDHCP    = 'd'
	panelLDAP    = 'l'
)

const busyText = "완료 서버 ping 확인중..."

// ViewState 는 TUI 화면 상태다. 렌더 입력이자 키 처리기(handleKey)의 대상이다.
type ViewState struct {
	Filter      Status // 0 = 필터 없음, 1~7 = 그 상태만
	Search      string // 적용된 hostname 검색어(부분 문자열, 대소문자 무시)
	Searching   bool   // 검색어 입력 중
	SearchBuf   string // 입력 중인 검색어
	Scroll      int    // 표의 첫 줄(필터 결과 기준)
	Panel       rune   // 열린 패널(0 = 표)
	PanelScroll int
	Busy        bool   // c: 완료 서버 ping 확인 중
	CheckErr    string // c 의 ping 확인이 실패했을 때 이유(다음 확인 때까지 표시)
	Warning     string // cfg.Warning
	Now         time.Time
	Color       bool
	W, H        int // 마지막 화면 크기(handleKey 의 쪽 크기 계산용)

	// Reports 는 d·l 패널 본문(조회 중 안내 또는 결과)이다. 백그라운드 조회가 끝나면 reportDone 이 채운다.
	Reports    map[rune]string
	reportBusy map[rune]bool // 조회 진행 중(패널마다 하나만)
	reportReq  rune          // handleKey 가 조회 시작을 요청한 패널(루프가 읽고 지운다)

	// PanelText 는 패널 본문을 만든다(nil 이면 defaultPanelText).
	PanelText func(kind rune, snap Snapshot) string

	rows   []int // 필터를 통과한 snap.Records 의 인덱스
	rowsOK bool
	pLines []string // 패널 본문을 폭에 맞춰 나눈 줄
	pW     int
	pOK    bool
}

// invalidate 는 새 스냅샷이 왔거나 필터·검색이 바뀌었을 때 캐시를 버린다.
func (v *ViewState) invalidate() { v.rowsOK, v.pOK = false, false }

// defaultPanelText 는 패널 본문 기본 제공자다. d·l 은 Reports 에 조회 결과가 들어오기 전에는 준비중이다.
func defaultPanelText(kind rune, snap Snapshot) string {
	switch kind {
	case panelInstall:
		return InstallComment(snap)
	case panelDelay:
		return DelayComment(snap)
	}
	return "준비중"
}

func panelTitle(kind rune) string {
	switch kind {
	case panelInstall:
		return "설치현황"
	case panelDelay:
		return "점검요청"
	case panelDHCP:
		return "DHCP"
	case panelLDAP:
		return "LDAP·H/T"
	}
	return ""
}

func (v *ViewState) panelBody(snap Snapshot) string {
	if v.Panel == panelInstall && v.Busy {
		return busyText
	}
	if t, ok := v.Reports[v.Panel]; ok && (v.Panel == panelDHCP || v.Panel == panelLDAP) {
		return t
	}
	if v.PanelText != nil {
		return v.PanelText(v.Panel, snap)
	}
	return defaultPanelText(v.Panel, snap)
}

// prepare 는 비어 있는 캐시(필터 결과, 패널 줄)를 채운다. 값 복사본에서 불러도 안전하다.
func (v *ViewState) prepare(snap Snapshot, w int) {
	if !v.rowsOK {
		rows := make([]int, 0, len(snap.Records))
		needle := strings.ToLower(v.Search)
		for i := range snap.Records {
			r := &snap.Records[i]
			if v.Filter != 0 && r.Status != v.Filter {
				continue
			}
			if needle != "" && !strings.Contains(strings.ToLower(r.Name), needle) {
				continue
			}
			rows = append(rows, i)
		}
		v.rows, v.rowsOK = rows, true
	}
	if v.Panel != 0 && (!v.pOK || v.pW != w) {
		v.pLines = wrapText(v.panelBody(snap), w-1)
		v.pW, v.pOK = w, true
	}
}

// wrapText 는 text 를 줄 단위로 나눠 폭 width 안으로 단어 단위 줄바꿈한다. 앞에 한 칸을 들여 쓴다.
func wrapText(text string, width int) []string {
	if width < 4 {
		width = 4
	}
	var out []string
	var tabs []string // 이어지는 탭 구분 표 줄(칸을 맞춰 한 줄씩, 줄바꿈 없음)
	flushTabs := func() {
		for _, l := range alignTabRows(tabs) {
			out = append(out, " "+l)
		}
		tabs = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "\t") {
			tabs = append(tabs, line)
			continue
		}
		flushTabs()
		line = cleanText(line)
		if strings.TrimSpace(line) == "" {
			out = append(out, "")
			continue
		}
		cur, curW := "", 0
		flush := func() {
			out = append(out, " "+cur)
			cur, curW = "", 0
		}
		for _, word := range strings.Fields(line) {
			ww := strWidth(word)
			for ww > width { // 한 단어가 폭보다 길면 글자 단위로 자른다
				if cur != "" {
					flush()
				}
				var sb strings.Builder
				used := 0
				for _, r := range word {
					rw := runeWidth(r)
					if used+rw > width {
						break
					}
					sb.WriteRune(r)
					used += rw
				}
				cur, curW = sb.String(), used
				flush()
				word = word[len(sb.String()):]
				ww = strWidth(word)
			}
			switch {
			case word == "":
			case cur == "":
				cur, curW = word, ww
			case curW+1+ww <= width:
				cur += " " + word
				curW += 1 + ww
			default:
				flush()
				cur, curW = word, ww
			}
		}
		if cur != "" {
			flush()
		}
	}
	flushTabs()
	return out
}

// ---- 레이아웃 ----

// fmtDur 는 경과 시간을 짧게 쓴다(45m, 1h05m, 2d03h).
func fmtDur(d time.Duration) string {
	sec := int64(d / time.Second)
	switch {
	case sec < 60:
		return "0m"
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh%02dm", sec/3600, sec%3600/60)
	}
	return fmt.Sprintf("%dd%02dh", sec/86400, sec%86400/3600)
}

// wrapItems 는 항목을 두 칸 간격으로 이어 폭 width 에 맞게 줄바꿈한다. 각 줄은 한 칸 들여 쓴다.
func wrapItems(items []string, width int) []string {
	var out []string
	cur, curW := "", 0
	for _, it := range items {
		iw := strWidth(it)
		switch {
		case cur == "":
			cur, curW = it, iw
		case curW+2+iw <= width-1:
			cur += "  " + it
			curW += 2 + iw
		default:
			out = append(out, " "+cur)
			cur, curW = it, iw
		}
	}
	if cur != "" {
		out = append(out, " "+cur)
	}
	return out
}

// headerLines 는 맨 위 줄들이다(제목, 상태별 개수, 갱신 시각·회차, 경고, 구분선).
func (v *ViewState) headerLines(snap Snapshot, w int) []string {
	var out []string
	out = append(out, paint(v.Color, stBold, truncW(fmt.Sprintf(" install_mn  OS 설치 현황  전체 %d대", snap.Total), w)))
	var items []string
	for st := StatusInstalling; st < statusCount; st++ {
		it := fmt.Sprintf("%d %s %d", int(st), st, snap.Counts[st])
		if v.Filter == st {
			it = "[" + it + "]"
		}
		items = append(items, it)
	}
	if n := snap.Counts[StatusNone]; n > 0 {
		items = append(items, fmt.Sprintf("%s %d", StatusNone, n))
	}
	for _, l := range wrapItems(items, w) {
		out = append(out, truncW(l, w))
	}
	upd := "갱신 전"
	if !snap.Updated.IsZero() {
		upd = "갱신 " + snap.Updated.Format("15:04:05")
		if !v.Now.IsZero() {
			upd += " (" + fmtDur(v.Now.Sub(snap.Updated)) + " 전)"
		}
	}
	run := "대기중"
	if snap.Running {
		i := int(v.Now.Unix() & 3)
		run = "수집중 " + "|/-\\"[i:i+1]
	}
	out = append(out, truncW(fmt.Sprintf(" %s · 회차 %d · %s", upd, snap.Round, run), w))
	if v.Warning != "" {
		out = append(out, paint(v.Color, stYellow, truncW(" 경고: "+cleanText(v.Warning), w)))
	}
	if snap.LastErr != "" {
		out = append(out, paint(v.Color, stRed, truncW(" 오류: "+cleanText(snap.LastErr), w)))
	}
	out = append(out, strings.Repeat("-", w))
	return out
}

// footerLines 는 맨 아래 키 안내 줄이다.
func (v *ViewState) footerLines(w int) []string {
	var items []string
	switch {
	case v.Searching:
		items = []string{"Enter 적용", "Esc 취소", "Backspace 지움"}
	case v.Panel != 0:
		items = []string{"↑↓ PgUp PgDn Home End 스크롤", fmt.Sprintf("%c 닫기", v.Panel), "c r d l 전환", "q 종료"}
	default:
		items = []string{"↑↓ PgUp PgDn Home End 이동", "/ 검색", "1~7 필터", "0 해제",
			"c 설치현황", "r 점검요청", "d DHCP", "l LDAP", "q 종료"}
	}
	var out []string
	for _, l := range wrapItems(items, w) {
		out = append(out, truncW(l, w))
	}
	return out
}

// bodySize 는 헤더·푸터를 뺀 본문 줄 수다(상태 줄 또는 열 제목 줄 포함).
func (v *ViewState) bodySize(snap Snapshot, w, h int) int {
	n := h - len(v.headerLines(snap, w)) - len(v.footerLines(w))
	if n < 2 {
		return 2
	}
	return n
}

// ---- 렌더 ----

type cols struct{ host, vendor, status, elapsed int }

func colWidths(w int) cols {
	switch {
	case w >= 110:
		return cols{20, 10, 12, 8}
	case w >= 76:
		return cols{16, 8, 12, 8}
	}
	return cols{14, 6, 12, 7}
}

func normSize(w, h int) (int, int) {
	if w < 20 {
		w = 20
	}
	if h < 6 {
		h = 6
	}
	return w, h
}

// render 는 화면 전체를 한 문자열로 만든다(줄 사이는 "\n", 마지막에 줄바꿈 없음, 정확히 h 줄).
// 순수 함수다: view 는 값으로 받으며 캐시가 비어 있으면 복사본에서 채운다.
func render(view ViewState, snap Snapshot, w, h int) string {
	w, h = normSize(w, h)
	view.prepare(snap, w)
	lines := view.headerLines(snap, w)
	foot := view.footerLines(w)
	body := h - len(lines) - len(foot)
	if body < 2 {
		body = 2
	}
	if view.Panel != 0 {
		lines = append(lines, view.panelLines(w, body)...)
	} else {
		lines = append(lines, view.tableLines(snap, w, body)...)
	}
	lines = append(lines, foot...)
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func clampInt(x, lo, hi int) int {
	if x > hi {
		x = hi
	}
	if x < lo {
		x = lo
	}
	return x
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// panelLines 는 열린 패널의 본문(첫 줄은 상태 줄)이다. body 줄로 맞춘다.
func (v *ViewState) panelLines(w, body int) []string {
	rows := body - 1
	top := clampInt(v.PanelScroll, 0, len(v.pLines)-rows)
	end := minInt(top+rows, len(v.pLines))
	status := fmt.Sprintf(" [%c] %s  %d-%d/%d줄", v.Panel, panelTitle(v.Panel), minInt(top+1, end), end, len(v.pLines))
	if v.CheckErr != "" && v.Panel == panelInstall {
		status += "  ping 확인 실패: " + cleanText(v.CheckErr)
	}
	out := []string{paint(v.Color, stBold, truncW(status, w))}
	for _, l := range v.pLines[top:end] {
		out = append(out, truncW(l, w))
	}
	for len(out) < body {
		out = append(out, "")
	}
	return out
}

// tableLines 는 표 본문(상태 줄, 열 제목, 보이는 행만)이다. body 줄로 맞춘다.
func (v *ViewState) tableLines(snap Snapshot, w, body int) []string {
	n := len(v.rows)
	rows := body - 2 // 상태 줄 + 열 제목 줄
	if rows < 1 {
		rows = 1
	}
	top := clampInt(v.Scroll, 0, n-rows)
	end := minInt(top+rows, n)
	var st string
	if v.Searching {
		st = " 검색> " + cleanText(v.SearchBuf) + "_"
	} else {
		flt := "없음"
		if v.Filter != 0 {
			flt = v.Filter.String()
		}
		srch := "-"
		if v.Search != "" {
			srch = cleanText(v.Search)
		}
		st = fmt.Sprintf(" 표시 %d-%d/%d대  필터: %s  검색: %s", minInt(top+1, end), end, n, flt, srch)
		if v.CheckErr != "" {
			st += "  ping 확인 실패: " + cleanText(v.CheckErr)
		}
	}
	out := []string{paint(v.Color, stBold, truncW(st, w))}
	c := colWidths(w)
	note := w - 1 - c.host - c.vendor - c.status - c.elapsed - 4
	if note < 4 {
		note = 4
	}
	head := " " + padR("호스트명", c.host) + " " + padR("벤더", c.vendor) + " " + padR("상태", c.status) + " " + padR("경과", c.elapsed) + " 비고"
	out = append(out, paint(v.Color, stGray, truncW(head, w)))
	if n == 0 {
		out = append(out, "  (표시할 서버가 없습니다)")
	}
	for _, i := range v.rows[top:end] {
		r := &snap.Records[i]
		el := ""
		if (r.Status == StatusInstalling || r.Status == StatusDelayed || r.Status == StatusDisk) &&
			!r.FirstInstallingAt.IsZero() && !v.Now.IsZero() {
			el = fmtDur(v.Now.Sub(r.FirstInstallingAt))
		}
		line := " " + padR(r.Name, c.host) + " " + padR(r.Vendor, c.vendor) + " " +
			paint(v.Color, statusStyle(r.Status), padR(r.Status.String(), c.status)) + " " +
			padR(el, c.elapsed) + " " + padR(r.Reason, note)
		out = append(out, strings.TrimRight(line, " "))
	}
	for len(out) < body {
		out = append(out, "")
	}
	return out
}

// alignTabRows 는 탭으로 나눈 줄 묶음의 칸을 맞춰 공백 두 칸 간격의 줄로 만든다(각 줄의 마지막 칸은 채우지 않음).
func alignTabRows(lines []string) []string {
	rows := make([][]string, len(lines))
	var widths []int
	for i, l := range lines {
		rows[i] = strings.Split(cleanTabText(l), "\t")
		for j, c := range rows[i][:len(rows[i])-1] {
			for len(widths) <= j {
				widths = append(widths, 0)
			}
			if w := strWidth(c); w > widths[j] {
				widths[j] = w
			}
		}
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		var sb strings.Builder
		for j, c := range r {
			sb.WriteString(c)
			if j < len(r)-1 {
				sb.WriteString(strings.Repeat(" ", widths[j]-strWidth(c)+2))
			}
		}
		out[i] = sb.String()
	}
	return out
}

// cleanTabText 는 탭을 남기고 나머지 제어문자만 '?' 로 바꾼다.
func cleanTabText(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\t' && (r < 0x20 || (r >= 0x7f && r < 0xa0)) {
			return '?'
		}
		return r
	}, s)
}
