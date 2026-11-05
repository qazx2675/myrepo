package main

// -run TUI 화면 그리기. 상태 → 문자열 순수 함수(tty 없이 시험할 수 있다).
// 줄 끝 처리·커서 이동은 tui.go 가 하고, 여기서는 "\n" 으로 이은 정확히 h 줄을 만든다.
// 한글 등 전각 문자는 2칸으로 센다. 색은 viewState.NoColor 가 아니면 ANSI 코드로 입힌다.

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// viewState 는 화면에서만 쓰는 상태(스크롤·필터·도움말·확인 등).
type viewState struct {
	Scroll   int    // 표 첫 줄 위치(0 부터)
	OnlyBad  bool   // f: O 아닌 호스트만
	Help     bool   // ? 도움말 겹쳐 보이기
	Confirm  string // "" / "s"(질문에서 중지 선택, 확인 중) / "S"(질문 없이 작업 중지, 확인 중)
	ConfirmQ string // 확인 중인 질문 id
	Flash    string // 잠깐 보이는 알림
	NoColor  bool
}

// heartbeatStale 은 heartbeat 가 이보다 오래되면 워커를 중단된 것으로 본다.
const heartbeatStale = 15 * time.Second

// ---------------------------------------------------------------------------
// 폭 계산·자르기
// ---------------------------------------------------------------------------

// runeWidth 는 표시 칸 수(전각 2, 그 밖 1).
func runeWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6:
		return 2
	}
	return 1
}

func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

// truncW 는 s 를 w 칸 안으로 자른다(전각 문자를 쪼개지 않음).
func truncW(s string, w int) string {
	if w <= 0 {
		return ""
	}
	n := 0
	for i, r := range s {
		rw := runeWidth(r)
		if n+rw > w {
			return s[:i]
		}
		n += rw
	}
	return s
}

// padW 는 s 를 w 칸으로 맞춘다(길면 자르고, 짧으면 공백을 채움).
func padW(s string, w int) string {
	s = truncW(s, w)
	if d := w - displayWidth(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// wrapW 는 s 를 줄바꿈 문자와 폭 w 기준으로 나눈다.
func wrapW(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.TrimRight(para, "\r")
		if para == "" {
			out = append(out, "")
			continue
		}
		cur, n := "", 0
		for _, r := range para {
			rw := runeWidth(r)
			if n+rw > w {
				out = append(out, cur)
				cur, n = "", 0
			}
			cur += string(r)
			n += rw
		}
		out = append(out, cur)
	}
	return out
}

// ---------------------------------------------------------------------------
// 색 입힌 줄 만들기
// ---------------------------------------------------------------------------

// seg 는 한 줄을 이루는 조각(문자열과 SGR 코드, 코드가 빈 값이면 색 없음).
type seg struct{ s, code string }

// lineOf 는 조각들을 이어 w 칸 안으로 자른 한 줄을 만든다.
func lineOf(w int, noColor bool, segs ...seg) string {
	var b strings.Builder
	left := w
	for _, sg := range segs {
		if left <= 0 {
			break
		}
		t := truncW(sg.s, left)
		left -= displayWidth(t)
		if t == "" {
			continue
		}
		if sg.code != "" && !noColor {
			b.WriteString("\x1b[" + sg.code + "m" + t + "\x1b[0m")
		} else {
			b.WriteString(t)
		}
	}
	return b.String()
}

const (
	cBold   = "1"
	cDim    = "2"
	cRed    = "31"
	cGreen  = "32"
	cYellow = "33"
	cCyan   = "36"
	cRev    = "1;7"
)

// ---------------------------------------------------------------------------
// 시간·상태 문구
// ---------------------------------------------------------------------------

// fmtDur 는 길이를 "45초" / "12분 03초" / "1시간 02분" 으로 쓴다.
func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Round(time.Second) / time.Second)
	switch {
	case s < 60:
		return fmt.Sprintf("%d초", s)
	case s < 3600:
		return fmt.Sprintf("%d분 %02d초", s/60, s%60)
	}
	return fmt.Sprintf("%d시간 %02d분", s/3600, s%3600/60)
}

// workerState 는 머리줄의 워커 상태: 완료 / 중지됨 / 실행중 / 중단됨(워커 없음 또는 heartbeat 15초 이상 지남).
func workerState(st *runState, alive bool, now time.Time) (text, code string) {
	switch {
	case st.Stopped:
		return "중지됨", cYellow
	case st.Stage == stageFinish:
		return "완료", cGreen
	case alive && now.Sub(st.Heartbeat) < heartbeatStale:
		return "실행중", cGreen
	}
	return "중단됨", cRed
}

// notAllO 는 "O 아닌 호스트만" 필터용: 완료가 아니거나 세 열 중 O(설정체크 '-' 는 설정체크를 안 하는 설정에서만 통과)가 아닌 것이 있음.
func notAllO(r *hostRow, checkOff bool) bool {
	return r.Status != stDone || !checkPass(checkOff, r.Check) || r.CPU != "O" || r.Stress != "O"
}

func visibleRows(st *runState, onlyBad bool) []*hostRow {
	var out []*hostRow
	for i := range st.Hosts {
		if r := &st.Hosts[i]; !onlyBad || notAllO(r, st.CheckOff) {
			out = append(out, r)
		}
	}
	return out
}

// after 는 t 가 now 로부터 얼마 뒤인지("47분 후" / "지남").
func after(t, now time.Time) string {
	if !t.After(now) {
		return "지남"
	}
	d := t.Sub(now) // 시간표는 초를 빼고 분 단위로
	switch m := int(d / time.Minute); {
	case m < 1:
		return fmtDur(d) + " 후"
	case m < 60:
		return fmt.Sprintf("%d분 후", m)
	default:
		return fmt.Sprintf("%d시간 %02d분 후", m/60, m%60)
	}
}

// resultPathOf 는 결과 파일 경로.
func resultPathOf(st *runState) string { return filepath.Join(st.runDir(), resultFile) }

// ---------------------------------------------------------------------------
// 화면 만들기
// ---------------------------------------------------------------------------

// topLines 는 표 위쪽(머리·단계 줄·진행·CPU 시간표).
func topLines(st *runState, view viewState, w int, now time.Time, alive bool) []string {
	nc := view.NoColor
	var out []string

	wt, wc := workerState(st, alive, now)
	end := now
	if st.Finished() && !st.Updated.IsZero() {
		end = st.Updated
	}
	out = append(out, lineOf(w, nc,
		seg{"new_setup -run", cBold}, seg{" | 사용자 " + st.User, ""},
		seg{" | 호스트파일 " + filepath.Base(strings.TrimPrefix(st.HostArg, "^")), ""},
		seg{" | 워커 ", ""}, seg{wt, wc}, seg{" | 경과 " + fmtDur(end.Sub(st.Created)), ""}))

	var bar []seg
	for n := stageJudge; n <= stageFinish; n++ {
		label := fmt.Sprintf("%d %s", n, stageName(n))
		if n > stageJudge {
			bar = append(bar, seg{" > ", cDim})
		}
		switch {
		case n == st.Stage:
			bar = append(bar, seg{"[" + label + "]", cRev})
		case n < st.Stage:
			bar = append(bar, seg{" " + label + " ", cGreen})
		default:
			bar = append(bar, seg{" " + label + " ", ""})
		}
	}
	out = append(out, lineOf(w, nc, bar...))

	detail := []seg{{"진행: " + st.Detail, ""}}
	if st.LastError != "" {
		detail = append(detail, seg{"  ! " + strings.ReplaceAll(st.LastError, "\n", " / "), cRed})
	}
	out = append(out, lineOf(w, nc, detail...))

	if st.Stage == stageLoad && !st.CPULaunch.IsZero() {
		if st.CPUWaitDone {
			out = append(out, lineOf(w, nc, seg{"CPU: launch " + st.CPULaunch.Format("15:04") + " | 감시 끝", cCyan}))
		} else {
			out = append(out, lineOf(w, nc, seg{fmt.Sprintf("CPU: launch %s | 확인 시작 %s(%s) | 다음 확인 %s(%s) | 마감 %s(%s)",
				st.CPULaunch.Format("15:04"),
				st.CheckStart.Format("15:04"), after(st.CheckStart, now),
				st.NextCheck.Format("15:04"), after(st.NextCheck, now),
				st.Deadline.Format("15:04"), after(st.Deadline, now)), cCyan}))
		}
	}
	return out
}

// bottomLines 는 표 아래쪽(질문 상자·알림·안내 줄).
func bottomLines(st *runState, view viewState, w int, alive bool) []string {
	nc := view.NoColor
	var out []string
	if q := st.Question; q != nil {
		out = append(out, lineOf(w, nc, seg{fmt.Sprintf("-- 질문 %s (%s 단계) --", q.ID, stageName(q.Stage)), cYellow}))
		lines := wrapW(q.Text, w-2)
		const maxQ = 8
		if len(lines) > maxQ {
			lines = append(lines[:maxQ-1], "...")
		}
		for _, l := range lines {
			out = append(out, lineOf(w, nc, seg{"  " + l, ""}))
		}
		// 선택지는 한 개씩 끊지 않고 줄을 채워 나열
		var cur []seg
		curW := 2
		flush := func() {
			if len(cur) > 0 {
				out = append(out, lineOf(w, nc, append([]seg{{"  ", ""}}, cur...)...))
			}
			cur, curW = nil, 2
		}
		for _, c := range q.Choices {
			key, label := "["+c.Key+"]", " "+c.Label+"  "
			cw := displayWidth(key) + displayWidth(label)
			if curW+cw > w && len(cur) > 0 {
				flush()
			}
			cur = append(cur, seg{key, cBold}, seg{label, ""})
			curW += cw
		}
		flush()
	} else if st.Finished() {
		out = append(out, lineOf(w, nc, seg{"-- 작업 끝 -- 결과 파일: " + resultPathOf(st), cGreen}))
	}
	if !alive && !st.Finished() {
		out = append(out, lineOf(w, nc, seg{"! 워커가 실행 중이 아닙니다. q 로 나간 뒤 같은 명령을 다시 실행하면 이어서 진행할 수 있습니다.", cRed}))
	}
	switch {
	case view.Confirm != "":
		out = append(out, lineOf(w, nc, seg{"작업을 중지할까요? 실행 중인 명령을 끝내고 결과를 저장합니다. (y/N)", cYellow}))
	case view.Flash != "":
		out = append(out, lineOf(w, nc, seg{view.Flash, cYellow}))
	}
	var keys []string
	if st.Question != nil {
		keys = append(keys, "[선택지 키] 답하기")
	} else if alive && !st.Finished() {
		keys = append(keys, "S 작업 중지")
	}
	keys = append(keys, "f O 아닌 것만", "↑↓ PgUp/PgDn 스크롤", "q 나가기(작업은 계속)", "? 도움말")
	out = append(out, lineOf(w, nc, seg{strings.Join(keys, "  "), cDim}))
	return out
}

// tableCapacity 는 이 화면 크기에서 표 데이터가 보일 수 있는 줄 수.
func tableCapacity(st *runState, view viewState, w, h int, now time.Time, alive bool) int {
	n := h - len(topLines(st, view, w, now, alive)) - len(bottomLines(st, view, w, alive)) - 2 // 표 제목·아래 안내 한 줄씩
	if n < 1 {
		return 1
	}
	return n
}

// tableLines 는 표 제목 + 보이는 행들(capacity 줄 이내) + 아래 안내 한 줄.
func tableLines(st *runState, view viewState, w, capacity int) []string {
	nc := view.NoColor
	rows := visibleRows(st, view.OnlyBad)
	cols := []string{"hostname", "상태", "설정체크", "CPU", "stress-ng", "비고"}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = displayWidth(c)
	}
	cells := func(r *hostRow) []string {
		return []string{r.Host, r.Status, r.Check, r.CPU, r.Stress, r.noteText()}
	}
	for _, r := range rows {
		for i, c := range cells(r) {
			if cw := displayWidth(c); cw > widths[i] {
				widths[i] = cw
			}
		}
	}
	if widths[0] > 28 {
		widths[0] = 28
	}
	// 폭이 모자라면 비고 → 호스트 이름 순으로 줄인다. 비고가 4칸도 못 남으면 비고 열을 뺀다.
	const gap = 3 // " | "
	nCols := len(cols)
	fixed := func() int {
		t := 0
		for i := 0; i < nCols-1; i++ {
			t += widths[i] + gap
		}
		return t
	}
	if left := w - fixed(); left < widths[nCols-1] {
		if left < 4 {
			nCols--
		} else {
			widths[nCols-1] = left
		}
	}
	if nCols == len(cols)-1 {
		for fixed()-gap > w && widths[0] > 6 {
			widths[0]--
		}
	}
	mkline := func(vals []string, codes []string) string {
		var sg []seg
		for i := 0; i < nCols; i++ {
			v := vals[i]
			if i < nCols-1 {
				v = padW(v, widths[i])
			}
			code := ""
			if codes != nil {
				code = codes[i]
			}
			sg = append(sg, seg{v, code})
			if i < nCols-1 {
				sg = append(sg, seg{" | ", cDim})
			}
		}
		return lineOf(w, nc, sg...)
	}

	out := []string{mkline(cols, []string{cBold, cBold, cBold, cBold, cBold, cBold})}
	max := len(rows) - capacity
	if max < 0 {
		max = 0
	}
	start := view.Scroll
	if start > max {
		start = max
	}
	if start < 0 {
		start = 0
	}
	end := start + capacity
	if end > len(rows) {
		end = len(rows)
	}
	for _, r := range rows[start:end] {
		codes := []string{"", "", valColor(r.Check), valColor(r.CPU), valColor(r.Stress), ""}
		if r.Status != stDone {
			codes[1] = cDim
		}
		if r.Excluded {
			codes[0] = cDim
		}
		out = append(out, mkline(cells(r), codes))
	}
	info := fmt.Sprintf("호스트 %d대", len(rows))
	if len(rows) > 0 {
		info = fmt.Sprintf("%d-%d / %d대", start+1, end, len(rows))
	}
	if view.OnlyBad {
		info += " (O 아닌 호스트만, f 로 전체)"
	}
	if len(rows) == 0 && !view.OnlyBad {
		info += " (판정 결과를 기다리는 중)"
	}
	out = append(out, lineOf(w, nc, seg{info, cDim}))
	return out
}

func valColor(v string) string {
	switch v {
	case "O":
		return cGreen
	case "X", noOutput:
		return cRed
	case cpuNotRun:
		return cYellow
	}
	return ""
}

// helpLines 는 ? 도움말 화면.
func helpLines(w int, noColor bool) []string {
	txt := []string{
		"new_setup -run 도움말",
		"",
		"질문이 떠 있을 때",
		"  c  계속(다음 단계)      r  재시도(같은 단계 다시)",
		"  x  O 아닌 호스트를 제외하고 계속      s  작업 중지(y/N 확인)",
		"질문이 없을 때",
		"  S  작업 중지(y/N 확인) - 실행 중인 외부 명령을 끝내고 결과를 저장합니다",
		"화면",
		"  f  O 아닌 호스트만 보기(다시 누르면 전체)",
		"  ↑ ↓ PgUp PgDn Home End  표 스크롤",
		"  q  화면만 나가기(작업은 계속됨. 같은 경로에서 같은 명령으로 다시 붙음)",
		"  ?  이 도움말(아무 키나 누르면 닫힘)",
	}
	out := make([]string, 0, len(txt))
	for i, l := range txt {
		code := ""
		if i == 0 {
			code = cBold
		}
		out = append(out, lineOf(w, noColor, seg{l, code}))
	}
	return out
}

// renderRun 은 화면 전체를 w x h 칸(정확히 h 줄, "\n" 으로 이음)으로 그린다. st 가 nil 이면 읽는 중 화면.
func renderRun(st *runState, view viewState, w, h int, now time.Time, alive bool) string {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	var lines []string
	switch {
	case st == nil:
		lines = []string{lineOf(w, view.NoColor, seg{"상태 파일을 읽는 중...", ""})}
	case view.Help:
		lines = helpLines(w, view.NoColor)
	default:
		top := topLines(st, view, w, now, alive)
		bottom := bottomLines(st, view, w, alive)
		table := tableLines(st, view, w, tableCapacity(st, view, w, h, now, alive))
		lines = append(lines, top...)
		lines = append(lines, table...)
		for len(lines)+len(bottom) < h { // 질문·안내 줄은 맨 아래에 붙인다
			lines = append(lines, "")
		}
		lines = append(lines, bottom...)
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// renderPlain 은 tty 가 아닐 때 한 번 내는 상태 글(머리 줄들 + `|` 표). 색·화면 제어 없음.
func renderPlain(st *runState, alive bool, now time.Time) string {
	var b strings.Builder
	wt, _ := workerState(st, alive, now)
	end := now
	if st.Finished() && !st.Updated.IsZero() {
		end = st.Updated
	}
	fmt.Fprintf(&b, "사용자: %s\n호스트파일: %s\n", st.User, st.HostFile)
	fmt.Fprintf(&b, "단계: %d %s\n워커: %s\n경과: %s\n진행: %s\n", st.Stage, stageName(st.Stage), wt, fmtDur(end.Sub(st.Created)), st.Detail)
	if st.LastError != "" {
		fmt.Fprintf(&b, "오류: %s\n", strings.ReplaceAll(st.LastError, "\n", " / "))
	}
	if st.Stage == stageLoad && !st.CPULaunch.IsZero() {
		if st.CPUWaitDone {
			fmt.Fprintf(&b, "CPU: launch %s, 감시 끝\n", st.CPULaunch.Format("15:04"))
		} else {
			fmt.Fprintf(&b, "CPU: launch %s, 확인 시작 %s, 다음 확인 %s, 마감 %s\n", st.CPULaunch.Format("15:04"),
				st.CheckStart.Format("15:04"), st.NextCheck.Format("15:04"), st.Deadline.Format("15:04"))
		}
	}
	if q := st.Question; q != nil {
		fmt.Fprintf(&b, "질문 %s (%s 단계): %s\n", q.ID, stageName(q.Stage), strings.ReplaceAll(q.Text, "\n", " / "))
		var ch []string
		for _, c := range q.Choices {
			ch = append(ch, "["+c.Key+"] "+c.Label)
		}
		fmt.Fprintf(&b, "선택지: %s\n", strings.Join(ch, "  "))
	}
	if st.Finished() {
		fmt.Fprintf(&b, "결과 파일: %s\n", resultPathOf(st))
	}
	b.WriteString(resultText(st))
	return b.String()
}
