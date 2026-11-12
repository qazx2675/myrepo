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
	Scroll   int      // 표 첫 줄 위치(0 부터)
	OnlyBad  bool     // f: O 아닌 호스트만
	Help     bool     // ? 도움말 겹쳐 보이기
	Confirm  string   // "" / "s"(질문에서 중지 선택, 확인 중) / "S"(질문 없이 작업 중지, 확인 중)
	ConfirmQ string   // 확인 중인 질문 id
	Flash    string   // 잠깐 보이는 알림
	NoLog    bool     // l: 아래 로그 창 끄기(기본은 켜짐)
	Result   bool     // v: 호스트표 자리에 결과 판 보이기(기본은 꺼짐)
	LogLines []string // worker.log 끝부분(runTUI 가 읽어 넣는다. 화면 그리기는 이 값만 본다)
	NoColor  bool

	// 입력 질문(question.Inputs)의 칸 입력 상태. 질문이 뜨면 sync 가 기본값(지금 시각)으로 채운다(입력 모드 구분 없음).
	InQ     string            // 칸 값이 속한 질문 id (빈 값 = 입력 질문 없음)
	InIdx   int               // 지금 칸 번호
	InVals  map[string]string // 칸마다 입력한 글자(키 = question.Inputs 의 Key, 입력한 그대로 보낸다)
	InFresh bool              // 지금 칸에 아직 안 씀: 첫 숫자가 칸 값을 대신한다
	InErr   string            // 칸·전체 검사 오류(고치면 사라짐)

	// 답을 보낸 뒤 워커가 받을 때까지(같은 질문이 sentHold 안에 떠 있는 동안) 그 질문에 다시 답하지 않는다
	SentQ   string    // 답을 보낸 질문 id
	SentKey string    // 보낸 선택지 키
	SentAt  time.Time // 보낸 시각

	// p 리포트(report_tui.go). Rep 이 빈 값이 아니면 화면 전체를 리포트가 쓴다.
	Rep       string    // "" / "pick"(기준 고르기) / "input"(코멘트 입력) / "confirm"(읽은 내용 확인) / "show"(리포트) / "err"(오류)
	RepInput  []rune    // 코멘트 입력 글
	RepBase   *repBase  // 확인 중인 기준
	RepNotes  []string  // 확인 화면의 알림
	RepLines  []string  // 보이는 글(확인 화면·리포트·오류 문구)
	RepScroll int       // 리포트 화면 스크롤
	RepPath   string    // 저장한 report_YYYYMMDD.txt 경로(저장 실패면 "저장 실패: 사유", 저장 기능이 없으면 빈 값)
	RepLabel  string    // 리포트 첫 줄에 보일 기준 이름
	RepDate   time.Time // 리포트를 만든 시각(첫 줄의 날짜)
	RepRules  []vendorRule
	RepETA    int
	RepWarns  []string // 리포트 화면 위쪽에 보일 설정 경고·알림
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
	cQText  = "1;36" // 질문 본문 (굵은 청록)
	cQKey   = "1;33" // 선택지 키·입력 항목 이름 (굵은 노랑)
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
	title := "new_setup -run"
	if st.Auto != nil {
		title += " -auto"
	}
	out = append(out, lineOf(w, nc,
		seg{title, cBold}, seg{" | 사용자 " + st.User, ""},
		seg{" | 호스트파일 " + filepath.Base(strings.TrimPrefix(st.HostArg, "^")), ""},
		seg{" | 워커 ", ""}, seg{wt, wc}, seg{" | 경과 " + fmtDur(end.Sub(st.Created)), ""}))

	if st.Auto != nil { // -auto 는 단계가 판정에 머물러 있으므로 단계 줄 대신 자동 진행 줄(남은 시간·대수)을 보인다
		out = append(out, lineOf(w, nc, seg{"[자동 진행]", cRev}, seg{" " + autoRemain(st, now) + " | " + strings.ReplaceAll(countsText(st), sep, " | "), ""}))
	}
	var bar []seg
	for n := stageJudge; n <= stageFinish && st.Auto == nil; n++ {
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
	if st.Auto == nil {
		out = append(out, lineOf(w, nc, bar...))
	}

	detail := []seg{{"진행: " + st.Detail, ""}}
	if st.LastError != "" {
		detail = append(detail, seg{"  ! " + strings.ReplaceAll(st.LastError, "\n", " / "), cRed})
	}
	out = append(out, lineOf(w, nc, detail...))

	if st.StressEnd != "" {
		out = append(out, lineOf(w, nc, seg{"stress-ng 종료 시각: " + endText(st.StressEnd), cCyan}))
	}
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
			out = append(out, lineOf(w, nc, seg{"  " + l, cQText}))
		}
		// 선택지는 한 개씩 끊지 않고 줄을 채워 나열. 답을 보낸 뒤 워커가 받기 전이면 그 자리에 보낸 답을 보인다.
		sent := view.SentQ == q.ID
		if sent {
			out = append(out, lineOf(w, nc, seg{"  답 [" + view.SentKey + "] 보냄 — 워커가 받는 중…", cYellow}))
		}
		var cur []seg
		curW := 2
		flush := func() {
			if len(cur) > 0 {
				out = append(out, lineOf(w, nc, append([]seg{{"  ", ""}}, cur...)...))
			}
			cur, curW = nil, 2
		}
		for _, c := range q.Choices {
			if sent {
				break
			}
			key, label := "["+c.Key+"]", " "+c.Label+"  "
			cw := displayWidth(key) + displayWidth(label)
			if curW+cw > w && len(cur) > 0 {
				flush()
			}
			cur = append(cur, seg{key, cQKey}, seg{label, cCyan})
			curW += cw
		}
		flush()
		out = append(out, inputLines(q, view, w)...)
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
	for _, l := range wrapItems(keyHints(st, view, alive), w, 2) {
		out = append(out, lineOf(w, nc, seg{l, cDim}))
	}
	return out
}

// keyHints 는 하단 안내 줄에 보일, 지금 쓸 수 있는 키(앞쪽이 먼저 보인다).
func keyHints(st *runState, view viewState, alive bool) []string {
	if view.Confirm != "" {
		return []string{"y 중지", "다른 키 취소"}
	}
	keys := []string{"q 나가기", "? 도움말"}
	q := st.Question
	if q != nil && view.SentQ != q.ID {
		if len(q.Inputs) > 0 {
			keys = append(keys, "숫자 입력", "Enter·Tab·→ 다음 칸", "←·Shift-Tab 이전 칸", "Esc 기본값", "마지막 칸 Enter 또는 c 제출")
		}
		keys = append(keys, "[선택지 키] 답하기")
	}
	if alive && !st.Finished() && (q == nil || !hasChoice(q, ansStop)) {
		keys = append(keys, "s 작업 중지")
	}
	return append(keys, "f O 아닌 것만", "l 로그 켜기/끄기", "v 결과 판", "p 리포트", "↑↓ 스크롤")
}

// wrapItems 는 항목들을 "  " 로 이어 폭 w 안에서 항목 단위로 줄바꿈한다(최대 maxLines 줄, 넘치는 항목은 뺀다).
func wrapItems(items []string, w, maxLines int) []string {
	var out []string
	cur := ""
	for _, it := range items {
		switch {
		case cur == "":
			cur = it
		case displayWidth(cur)+2+displayWidth(it) <= w:
			cur += "  " + it
		default:
			out = append(out, cur)
			if len(out) == maxLines {
				return out
			}
			cur = it
		}
	}
	if cur != "" && len(out) < maxLines {
		out = append(out, cur)
	}
	return out
}

// 로그 창: 제목 한 줄 + 로그 끝 최대 maxLogLines 줄. 표가 최소 minTableRows 줄은 보이도록 남는 자리만 쓴다.
const (
	maxLogLines  = 8
	minTableRows = 3
)

// logHeight 는 로그 창이 차지할 줄 수(제목 포함, 0 이면 창을 그리지 않음).
// top·bottom 은 표 위·아래 줄 수. 질문·선택지(bottom)는 밀어내지 않고 모자라면 로그 창을 줄이거나 뺀다.
func logHeight(view viewState, h, top, bottom int) int {
	if view.NoLog {
		return 0
	}
	n := 1 + maxLogLines
	if avail := h - top - bottom - 2 - minTableRows; avail < n { // 표 제목·아래 안내 한 줄씩(2) 과 표 최소 줄 수를 남긴다
		n = avail
	}
	if n < 2 {
		return 0
	}
	return n
}

// cleanLogLine 은 로그 한 줄을 화면에 안전하게 보이도록 탭을 공백으로, 그 밖의 제어 문자(ESC 포함)를 뺀다.
func cleanLogLine(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString("  ")
		case r < 0x20 || r == 0x7f || r == 0xfffd:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// 로그 줄 종류. worker.log 에는 worker 자신의 메시지("2006-01-02 15:04:05 메시지"),
// 외부 명령 줄("+ 명령"), 외부 프로그램 출력(접두 없음)이 섞여 있어 색으로 구분한다.
const (
	logOut  = iota // 외부 프로그램 출력 (회색)
	logCmd         // "+ " 로 시작하는 실행 명령 (굵은 청록)
	logMsg         // worker 메시지 (기본색)
	logWarn        // 경고 (노랑)
	logErr         // 오류 (빨강)
	logQA          // 질문·답 (자홍)
)

// workerPrefixLen 은 logf 의 "2006-01-02 15:04:05 " 접두 길이(20). 접두가 없으면 0.
func workerPrefixLen(l string) int {
	if len(l) < 20 || l[4] != '-' || l[7] != '-' || l[10] != ' ' || l[13] != ':' || l[16] != ':' || l[19] != ' ' {
		return 0
	}
	for _, i := range []int{0, 1, 2, 3, 5, 6, 8, 9, 11, 12, 14, 15, 17, 18} {
		if l[i] < '0' || l[i] > '9' {
			return 0
		}
	}
	return 20
}

// logLineKind 는 로그 한 줄의 종류를 판별한다. 우선순위: 명령 > 질문·답 > 오류 > 경고 > 메시지/출력.
func logLineKind(l string) int {
	if strings.HasPrefix(l, "+ ") {
		return logCmd
	}
	p := workerPrefixLen(l)
	msg := l[p:]
	if p > 0 && (strings.HasPrefix(msg, "질문 ") || strings.HasPrefix(msg, "답 ")) {
		return logQA
	}
	if containsAny(msg, "오류", "실패", "시간초과", "ERROR", "error", "launch 되지 않았", "중지") {
		return logErr
	}
	if p == 0 {
		return logOut // 외부 출력은 [감시] 등도 회색, 오류 낱말만 빨강
	}
	if containsAny(msg, "경고", "⚠", "무시한 답", "건너뜀", "응답 없음") {
		return logWarn
	}
	return logMsg
}

func containsAny(s string, subs ...string) bool {
	for _, t := range subs {
		if strings.Contains(s, t) {
			return true
		}
	}
	return false
}

// logKindCode 는 로그 종류에 대응하는 SGR 코드(빈 값이면 기본색).
func logKindCode(k int) string {
	switch k {
	case logCmd:
		return "1;36"
	case logQA:
		return "35"
	case logErr:
		return cRed
	case logWarn:
		return cYellow
	case logOut:
		return "90"
	}
	return ""
}

// logPane 은 로그 창을 정확히 n 줄(제목 + n-1 줄)로 만든다. 줄은 폭 w 칸(전각 2칸)에 맞춰 자른다.
func logPane(view viewState, w, n int) []string {
	if n < 1 {
		return nil
	}
	nc := view.NoColor
	out := []string{lineOf(w, nc, seg{"-- worker.log (l 로 끄기) --", cDim})}
	body := view.LogLines
	if len(body) > n-1 {
		body = body[len(body)-(n-1):]
	}
	if len(body) == 0 && n > 1 {
		out = append(out, lineOf(w, nc, seg{"(로그 없음)", cDim}))
	}
	for _, l := range body {
		l = cleanLogLine(l)
		out = append(out, lineOf(w, nc, seg{l, logKindCode(logLineKind(l))}))
	}
	for len(out) < n {
		out = append(out, "")
	}
	return out
}

// tableCapacity 는 이 화면 크기에서 표 데이터가 보일 수 있는 줄 수.
func tableCapacity(st *runState, view viewState, w, h int, now time.Time, alive bool) int {
	top, bottom := len(topLines(st, view, w, now, alive)), len(bottomLines(st, view, w, alive))
	n := h - top - bottom - 2 - logHeight(view, h, top, bottom) // 표 제목·아래 안내 한 줄씩
	if view.Result {
		n -= panelFixed(st)
	}
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

// autoLimitOf 는 -auto 제한 시간(기록이 없으면 기본값).
func autoLimitOf(au *autoState) time.Duration {
	if au.Limit > 0 {
		return au.Limit
	}
	return defaultAutoLimit
}

// autoRemain 은 -auto 의 남은 시간 문구.
func autoRemain(st *runState, now time.Time) string {
	au := st.Auto
	switch {
	case st.Finished():
		return "자동 진행 끝"
	case au.Ending:
		return "제한 시간 지남(진행 중인 것만 마치는 중)"
	case au.Start.IsZero():
		return "시작 중"
	}
	left := au.Start.Add(autoLimitOf(au)).Sub(now)
	if left < 0 {
		left = 0
	}
	return "남은 시간 " + fmtDur(left) + "(제한 " + fmtDur(autoLimitOf(au)) + ")"
}

// autoNextCheck 는 -auto 가 다음에 확인할 시각(CPU 묶음 판정 / 미완료 재판정 / CPU 대기열 / stress-ng 재시도 중 가장 이른 것).
func autoNextCheck(st *runState, now time.Time) (time.Time, bool) {
	au := st.Auto
	var best time.Time
	soon := func(t time.Time) {
		if t.After(now) && (best.IsZero() || t.Before(best)) {
			best = t
		}
	}
	if len(au.Batch) > 0 {
		soon(au.BatchNext)
	}
	if !au.Ending && !st.Finished() {
		for i := range st.Hosts {
			if st.Hosts[i].Status != stDone && st.Hosts[i].GaveUp == "" {
				soon(au.NextRecheck)
				break
			}
		}
		if len(au.Queue) > 0 {
			soon(au.QueueWait)
		}
		soon(au.NGWait)
	}
	return best, !best.IsZero()
}

// panelFixed 는 결과 판에서 표 위에 고정으로 쓰는 줄 수(대수 줄, -auto 면 시간·지금 하는 일 두 줄 더).
func panelFixed(st *runState) int {
	if st.Auto != nil {
		return 3
	}
	return 1
}

// resultPanel 은 v 결과 판: 호스트표 자리(표 제목 + 행 capacity 줄 이내 + 아래 안내 한 줄)에 대수와 `|` 구분 표를 그린다.
// 열은 공백이 아니라 `|` 로만 나누고(result.txt 와 같은 모양, H/T·OS 열은 뺌) -auto 이면 시도(수정/CPU/stress-ng)·마지막 오류 열이 붙는다.
func resultPanel(st *runState, view viewState, w, capacity int, now time.Time) []string {
	nc := view.NoColor
	out := []string{lineOf(w, nc, seg{countsText(st), cBold})}
	auto := st.Auto != nil
	if auto {
		next := "다음 확인 -"
		if t, ok := autoNextCheck(st, now); ok {
			next = "다음 확인 " + t.Format("15:04") + "(" + after(t, now) + ")"
		}
		out = append(out, lineOf(w, nc, seg{autoRemain(st, now) + sep + next, cCyan}))
		out = append(out, lineOf(w, nc, seg{"지금 하는 일: " + st.Detail, ""}))
	}
	cols := []string{"hostname", "상태", "설정체크", "CPU테스트", "stress-ng"}
	if auto {
		cols = append(cols, "시도(수정/CPU/ng)")
	}
	cols = append(cols, "비고")
	if auto {
		cols = append(cols, "마지막오류")
	}
	out = append(out, lineOf(w, nc, seg{strings.Join(cols, sep), cBold}))

	rows := visibleRows(st, view.OnlyBad)
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
		cells := []string{r.Host, r.Status, r.Check, r.CPU, r.Stress}
		codes := []string{"", "", valColor(r.Check), valColor(r.CPU), valColor(r.Stress)}
		if auto {
			cells = append(cells, r.autoTryText())
			codes = append(codes, "")
		}
		cells = append(cells, strings.ReplaceAll(r.noteText(), sep, "/"))
		codes = append(codes, "")
		if auto {
			cells = append(cells, strings.ReplaceAll(strings.ReplaceAll(r.AutoErr, "\n", " / "), sep, "/"))
			codes = append(codes, cRed)
		}
		var sg []seg
		for i, c := range cells {
			if i > 0 {
				sg = append(sg, seg{sep, cDim})
			}
			sg = append(sg, seg{c, codes[i]})
		}
		out = append(out, lineOf(w, nc, sg...))
	}
	info := fmt.Sprintf("호스트 %d대", len(rows))
	if len(rows) > 0 {
		info = fmt.Sprintf("%d-%d / %d대", start+1, end, len(rows))
	}
	info += " (결과 판, v 로 호스트표)"
	if view.OnlyBad {
		info += " (O 아닌 호스트만, f 로 전체)"
	}
	return append(out, lineOf(w, nc, seg{info, cDim}))
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
		"new_setup -run 도움말 (아무 키나 누르면 닫힘)",
		"  q  화면만 나가기(작업은 계속됨. 같은 경로에서 같은 명령으로 다시 붙음)      ?  이 도움말",
		"질문이 떠 있을 때",
		"  c  계속(다음 단계)   r  재시도(같은 단계 다시)   k  stress-ng 만 건너뜀",
		"  x  O 아닌 호스트를 제외하고 계속      s  작업 중지(y/N 확인)",
		"  y  CPU X 호스트 재실행 질문: 부하 낮은 X 호스트 모두 CPU 재실행에 포함      n  모두 제외(이번엔 X 그대로)",
		"  r  인벤토리 사용 중 질문: 다시 확인      x  그 묶음 중단(s 로 작업 전체 중지)",
		"  답을 보내면 워커가 받을 때까지(최대 5초) 같은 질문에는 다시 답하지 않습니다",
		"stress-ng 종료 시각 입력 - 질문이 뜨면 칸이 지금 시각으로 채워져 있고 바로 입력합니다",
		"  숫자  지금 칸에 입력(기본값은 첫 숫자로 바뀜, 칸이 차면 다음 칸)",
		"  Enter Tab →  다음 칸(마지막 칸의 Enter 는 제출)   ← Shift-Tab  이전 칸",
		"  Backspace  지움(빈 칸이면 이전 칸)   Esc  모든 칸 기본값   c  제출",
		"질문이 없을 때(또는 중지 선택지가 없는 질문)",
		"  s  작업 중지(y/N 확인) - 실행 중인 외부 명령을 끝내고 결과를 저장합니다",
		"화면",
		"  f  O 아닌 호스트만 보기(다시 누르면 전체)",
		"  l  아래 worker.log 끝부분 창 켜기/끄기(기본 켜짐, 화면이 작으면 줄이거나 숨김)",
		"  v  결과 판 켜기/끄기(호스트표 자리에 | 구분 표·완료/남음/포기 대수 표시. -auto 이면 단계별 시도 횟수·마지막 오류·지금 하는 일·다음 확인·남은 시간도)",
		"  p  벤더별 리포트(전일대비 기준을 1 저장본 / 2 코멘트 붙여넣기(Ctrl-D 끝, 첫 줄 @파일)로 골라 만들고 report_날짜.txt 로 저장)",
		"  ↑ ↓ PgUp PgDn Home End  표 스크롤      g / G  처음 / 끝      Space / b  한 화면 아래 / 위      Ctrl-L  화면 다시 그리기",
		"키는 한글 입력 상태(두벌식, 예 ㅊ=c ㄴ=s)·Caps Lock 에서도 같게 동작합니다. 쓸 수 없는 키를 누르면 알림이 뜹니다",
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
	view.sync(st, now) // 첫 화면부터 입력 칸 기본값이 보이게(view 는 복사본)
	var lines []string
	switch {
	case st == nil:
		lines = []string{lineOf(w, view.NoColor, seg{"상태 파일을 읽는 중...", ""})}
	case view.Help:
		lines = helpLines(w, view.NoColor)
	case view.Rep != "":
		lines = reportLines(st, view, w, h)
	default:
		top := topLines(st, view, w, now, alive)
		bottom := bottomLines(st, view, w, alive)
		table := tableLines(st, view, w, tableCapacity(st, view, w, h, now, alive))
		if view.Result {
			table = resultPanel(st, view, w, tableCapacity(st, view, w, h, now, alive), now)
		}
		logH := logHeight(view, h, len(top), len(bottom))
		lines = append(lines, top...)
		lines = append(lines, table...)
		for len(lines)+len(bottom)+logH < h { // 질문·안내 줄은 로그 창 바로 위에 붙이고, 로그 창은 맨 아래에 둔다
			lines = append(lines, "")
		}
		lines = append(lines, bottom...)
		lines = append(lines, logPane(view, w, logH)...)
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
	if st.StressEnd != "" {
		fmt.Fprintf(&b, "stress-ng 종료 시각: %s\n", endText(st.StressEnd))
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
		if len(q.Inputs) > 0 {
			fmt.Fprintf(&b, "입력 항목: %s (TUI 에서는 칸에 바로 입력, 비대화형이면 answer.json 의 values 로 답)\n", endFieldNames())
		}
	}
	if st.Finished() {
		fmt.Fprintf(&b, "결과 파일: %s\n", resultPathOf(st))
	}
	b.WriteString(resultText(st))
	return b.String()
}

// inputLines 는 입력 질문의 입력 칸: 한 줄 양식(`종료 시각  [2026] - [11] - [11]   [18] : [05]`, 지금 칸은 반전),
// 오류가 있으면 그 아래 오류 줄, 맨 아래 지금 칸 설명 줄. NoColor 이면 지금 칸을 > < 로 표시한다.
func inputLines(q *question, view viewState, w int) []string {
	if len(q.Inputs) == 0 {
		return nil
	}
	nc := view.NoColor
	segs := []seg{{"  종료 시각  ", cQKey}}
	for i, f := range q.Inputs {
		switch f.Key {
		case "end_month", "end_day":
			segs = append(segs, seg{" - ", ""})
		case "end_hour":
			segs = append(segs, seg{"   ", ""})
		case "end_min":
			segs = append(segs, seg{" : ", ""})
		default:
			if i > 0 {
				segs = append(segs, seg{" ", ""})
			}
		}
		val := view.InVals[f.Key]
		if d := fieldWidth(f.Key) - len(val); d > 0 {
			val += strings.Repeat("_", d)
		}
		switch {
		case i != view.InIdx || view.InQ != q.ID:
			segs = append(segs, seg{"[" + val + "]", cGreen})
		case nc:
			segs = append(segs, seg{">" + val + "<", ""})
		default:
			segs = append(segs, seg{"[" + val + "]", cRev})
		}
	}
	out := []string{lineOf(w, nc, segs...)}
	if view.InErr != "" {
		out = append(out, lineOf(w, nc, seg{"  ! " + view.InErr, cRed}))
	}
	if view.InIdx < len(q.Inputs) {
		out = append(out, lineOf(w, nc, seg{"  지금 칸: " + q.Inputs[view.InIdx].Label, cCyan}))
	}
	return out
}
