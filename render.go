// render.go - TUI/--plain 화면 렌더링 순수 함수(Snapshot → 문자열): 한글 폭 계산, 색(ANSI), 총계·그룹 행·호스트표·도움말
package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
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

// stripANSI: 테스트·길이 계산용 ANSI SGR 제거
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
	stGreen  = "32"   // 완료
	stCyan   = "36"   // 진행
	stGray   = "90"   // 대기·비활성
	stYellow = "33"   // 경고
	stRed    = "1;31" // 정체·실패
	stBold   = "1"
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
		t := truncW(cleanText(s.t), left)
		left -= strWidth(t)
		sb.WriteString(paint(color, s.st, t))
	}
	return sb.String()
}

// selLine: 선택 행 — 색이 있으면 반전(폭 전체), 없으면 그대로(마커 '>' 로 구분)
func selLine(color bool, width int, segs []seg) string {
	if !color {
		return segLine(false, width, segs...)
	}
	t := truncW(segsText(segs), width)
	if n := width - strWidth(t); n > 0 {
		t += strings.Repeat(" ", n)
	}
	return "\x1b[7m" + t + "\x1b[0m"
}

func stageStyle(st Stage) string {
	switch st {
	case StageDone:
		return stGreen
	case StageQueued:
		return stGray
	case StageStuck, StageFailed:
		return stRed
	}
	return stCyan
}

// ---- 시간 표시 ----

// fmtDur: 경과 초 → 45s / 12m05s / 1h02m / 2d03h
func fmtDur(sec int64) string {
	switch {
	case sec < 0:
		sec = 0
		fallthrough
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm%02ds", sec/60, sec%60)
	case sec < 86400:
		return fmt.Sprintf("%dh%02dm", sec/3600, sec%3600/60)
	}
	return fmt.Sprintf("%dd%02dh", sec/86400, sec%86400/3600)
}

// fmtClock: epoch → 스냅샷과 같은 시간대의 시각 문자열 (NowText 와 Now 의 차이로 UTC 오프셋을 구함 — 시간대에 무관하게 결정적)
func fmtClock(s Snapshot, t int64, layout string) string {
	var off int64
	if tm, err := time.Parse(snapTimeLayout, s.NowText); err == nil {
		off = tm.Unix() - s.Now
	}
	return time.Unix(t+off, 0).UTC().Format(layout)
}

// ---- 선택 상태·조회 ----

// selection: 화면 상태 (렌더 입력)
type selection struct {
	Row     int    // 화면 1: 그룹 행 번호 (전 job 의 그룹을 이어서 센 번호)
	Detail  bool   // 화면 2 (호스트표)
	JobID   string // 화면 2 대상 job
	Yml     string // 화면 2 대상 그룹
	Host    int    // 화면 2: (필터 적용된) 호스트 행 번호
	Filter  bool   // 화면 2: 정체·실패만
	Confirm bool   // 수동 실행 y/n 확인 중
	Msg     string // 하단 안내 한 줄
	DateFilter bool   // 화면 1: 날짜별 보기 (TUI 만 켬 — 날짜 줄 표시, 그 날짜의 그룹만)
	Date       string // 화면 1: 보고 있는 날짜 YYYYMMDD ("" = 가장 최근 날짜를 따라감)
}

type grpRef struct{ Job, Grp int }

// ---- 날짜별 보기 ----

// yml 이름의 14자리 시각(YYYYMMDDhhmmss) 예: infra_inventory-20261008084159_4ea.yml → 20261008
var ymlDateRe = regexp.MustCompile(`(?:^|\D)(\d{8})\d{6}(?:\D|$)`)

const dateLayout = "20060102"

// groupDate: 그룹의 날짜(YYYYMMDD) — yml 이름의 시각, 없으면 job 전달일
func groupDate(s Snapshot, j *SnapJob, g *SnapGroup) string {
	if m := ymlDateRe.FindStringSubmatch(g.Yml); m != nil {
		if _, err := time.Parse(dateLayout, m[1]); err == nil {
			return m[1]
		}
	}
	return fmtClock(s, j.Submitted, dateLayout)
}

// dateRange: 그룹이 있는 가장 이른/늦은 날짜 (그룹이 없으면 ok=false)
func dateRange(s Snapshot) (lo, hi string, ok bool) {
	for ji := range s.Jobs {
		for gi := range s.Jobs[ji].Groups {
			d := groupDate(s, &s.Jobs[ji], &s.Jobs[ji].Groups[gi])
			if !ok || d < lo {
				lo = d
			}
			if !ok || d > hi {
				hi = d
			}
			ok = true
		}
	}
	return
}

// effDate: 보여줄 날짜 — 지정이 없으면 가장 최근 날짜 (그룹이 없으면 "")
func effDate(s Snapshot, d string) string {
	if d != "" {
		return d
	}
	_, hi, _ := dateRange(s)
	return hi
}

// shiftDate: 하루 단위 이동, 그룹이 있는 날짜 범위 안으로 제한. 가장 최근 날짜에 닿으면 ""(최근을 따라감).
func shiftDate(s Snapshot, cur string, delta int) string {
	lo, hi, ok := dateRange(s)
	if !ok {
		return ""
	}
	t, err := time.Parse(dateLayout, effDate(s, cur))
	if err != nil {
		return ""
	}
	n := t.AddDate(0, 0, delta).Format(dateLayout)
	if n < lo {
		n = lo
	}
	if n >= hi {
		return ""
	}
	return n
}

// visibleGroups: 화면 1 에서 보이는 그룹 행 (날짜별 보기면 그 날짜의 그룹만)
func visibleGroups(s Snapshot, sel selection) []grpRef {
	all := flatGroups(s)
	if !sel.DateFilter {
		return all
	}
	d := effDate(s, sel.Date)
	var out []grpRef
	for _, r := range all {
		if groupDate(s, &s.Jobs[r.Job], &s.Jobs[r.Job].Groups[r.Grp]) == d {
			out = append(out, r)
		}
	}
	return out
}

var weekdayKo = [...]string{"일", "월", "화", "수", "목", "금", "토"}

// dateRowText: "<    2026-10-08 (목)    >   N개 그룹"
func dateRowText(d string, n int) string {
	t, err := time.Parse(dateLayout, d)
	if err != nil {
		return "<    ????-??-??    >"
	}
	return fmt.Sprintf("<    %s (%s)    >   %d개 그룹", t.Format("2006-01-02"), weekdayKo[t.Weekday()], n)
}

// flatGroups: 화면 1 의 그룹 행 순서
func flatGroups(s Snapshot) []grpRef {
	var out []grpRef
	for ji, j := range s.Jobs {
		for gi := range j.Groups {
			out = append(out, grpRef{ji, gi})
		}
	}
	return out
}

func findGroup(s Snapshot, jobID, yml string) (*SnapJob, *SnapGroup) {
	for ji := range s.Jobs {
		if s.Jobs[ji].ID != jobID {
			continue
		}
		if yml == allView {
			if g := mergedGroup(&s.Jobs[ji]); g != nil {
				return &s.Jobs[ji], g
			}
			return nil, nil
		}
		for gi := range s.Jobs[ji].Groups {
			if s.Jobs[ji].Groups[gi].Yml == yml {
				return &s.Jobs[ji], &s.Jobs[ji].Groups[gi]
			}
		}
	}
	return nil, nil
}

// visibleHosts: 화면 2 에 보이는 호스트 (filter 면 정체·실패만)
func visibleHosts(g *SnapGroup, filter bool) []SnapHost {
	if !filter {
		return g.Hosts
	}
	var out []SnapHost
	for _, h := range g.Hosts {
		if h.Stage == StageStuck || h.Stage == StageFailed {
			out = append(out, h)
		}
	}
	return out
}

func progressCount(c StageCounts) int {
	return c.Deploying + c.Installing + c.Booting + c.Ready + c.Ldap + c.Checking + c.Second
}

func pct(c StageCounts) int {
	if c.Total <= 0 {
		return 0
	}
	return c.Done * 100 / c.Total
}

func specText(g SnapGroup) string {
	var p []string
	for _, v := range []string{g.Infra, g.OS, g.Boot, g.Splunk} {
		if v != "" {
			p = append(p, v)
		}
	}
	if len(p) == 0 {
		return "-"
	}
	return strings.Join(p, "/")
}

// ---- 막대 ----

// barSegs: 단계별 건수 미니 막대 — 완료 '#'(초록) 진행 '+'(청록) 대기 '.'(회색) 정체·실패 '!'(빨강), 비율대로 w 칸 (0 보다 큰 구간은 최소 1칸)
func barSegs(c StageCounts, w int) []seg {
	if c.Total <= 0 || w <= 0 {
		return []seg{{strings.Repeat(" ", rMax(w, 0)), ""}}
	}
	n := [4]int{c.Done, progressCount(c), c.Queued, c.Stuck + c.Failed}
	chars := [4]string{"#", "+", ".", "!"}
	sty := [4]string{stGreen, stCyan, stGray, stRed}
	total := n[0] + n[1] + n[2] + n[3]
	if total == 0 {
		return []seg{{strings.Repeat(" ", w), ""}}
	}
	var alloc, frac [4]int
	sum := 0
	for i := range n {
		alloc[i] = n[i] * w / total
		frac[i] = n[i] * w % total
		sum += alloc[i]
	}
	for ; sum < w; sum++ { // 나머지 칸은 소수부가 큰 순서로 1칸씩
		best := -1
		for i := range n {
			if n[i] > 0 && (best < 0 || frac[i] > frac[best]) {
				best = i
			}
		}
		alloc[best]++
		frac[best] = -1
	}
	for i := range n { // 0 보다 큰 구간은 최소 1칸
		if n[i] > 0 && alloc[i] == 0 {
			big := 0
			for k := range alloc {
				if alloc[k] > alloc[big] {
					big = k
				}
			}
			alloc[big]--
			alloc[i] = 1
		}
	}
	var out []seg
	for i := range alloc {
		if alloc[i] > 0 {
			out = append(out, seg{strings.Repeat(chars[i], alloc[i]), sty[i]})
		}
	}
	return out
}

// ---- 공통 조각 ----

func sepLine(width int) string { return strings.Repeat("-", width) }

func clampWidth(width int) int {
	if width < 40 {
		return 40
	}
	return width
}

// titleLines: 상단 2줄 — 제목·갱신 시각·데몬 상태 / 총계
func titleLines(s Snapshot, width int, color bool) []string {
	left := []seg{{" auto_setup 상태 리포트", stBold}}
	var dm seg
	if s.Daemon.Running {
		t := "● 데몬 실행"
		if s.Daemon.PID > 0 {
			t += fmt.Sprintf(" (pid %d)", s.Daemon.PID)
		}
		dm = seg{t, stGreen}
	} else {
		dm = seg{"○ 데몬 중지", stRed}
	}
	right := []seg{{"갱신 " + clockOf(s), ""}, {"  ", ""}, dm}
	gap := width - strWidth(segsText(left)) - strWidth(segsText(right)) - 1
	if gap < 2 {
		gap = 2
	}
	l1 := append(append(left, seg{strings.Repeat(" ", gap), ""}), right...)

	t := s.Totals
	stuck := seg{fmt.Sprintf("정체 %d", t.Stuck), stGray}
	if t.Stuck > 0 {
		stuck.st = stRed
	}
	fail := seg{fmt.Sprintf("실패 %d", t.Failed), stGray}
	if t.Failed > 0 {
		fail.st = stRed
	}
	l2 := []seg{{" ", ""}, {fmt.Sprintf("전체 %d", t.Total), stBold}, {"  ", ""},
		{fmt.Sprintf("완료 %d", t.Done), stGreen}, {"  ", ""},
		{fmt.Sprintf("진행 %d", progressCount(t)), stCyan}, {"  ", ""},
		{fmt.Sprintf("대기 %d", t.Queued), stGray}, {"  ", ""}, stuck, {"  ", ""}, fail}
	return []string{segLine(color, width, l1...), segLine(color, width, l2...)}
}

func clockOf(s Snapshot) string {
	if len(s.NowText) >= 19 {
		return s.NowText[11:19]
	}
	return s.NowText
}

// window: 본문 lines 중 cursor 가 보이는 구간 [top, top+avail)
func window(lines []string, cursor, avail int) []string {
	if avail <= 0 || len(lines) <= avail {
		return lines
	}
	top := 0
	if cursor >= avail {
		top = cursor - avail + 1
	}
	if top+avail > len(lines) {
		top = len(lines) - avail
	}
	return lines[top : top+avail]
}

func msgLine(sel selection, width int, color bool) string {
	if sel.Msg == "" {
		return ""
	}
	st := stYellow
	if strings.HasPrefix(sel.Msg, "요청됨") {
		st = stGreen
	}
	return segLine(color, width, seg{" " + sel.Msg, st})
}

// ---- 화면 1 ----

// renderOverview: 화면 1 — 총계 + job 별 헤더 + yml 그룹 행. height<=0 이면 자르지 않고 키 안내 줄도 생략(--plain).
func renderOverview(s Snapshot, sel selection, width, height int, color bool) string {
	width = clampWidth(width)
	head := titleLines(s, width, color)
	head = append(head, sepLine(width))
	var body []string
	cursor, idx := 0, 0
	if len(s.Jobs) == 0 {
		body = append(body, truncW(" 진행 중인 작업 없음 (01 에서 OS 설치를 전달하면 표시됩니다)", width))
	}
	date := ""
	if sel.DateFilter && len(s.Jobs) > 0 {
		date = effDate(s, sel.Date)
		segs := []seg{{"  ", ""}, {dateRowText(date, len(visibleGroups(s, sel))), stBold}}
		if sel.Row == -1 {
			segs[0].t = "> "
			body = append(body, selLine(color, width, segs))
		} else {
			body = append(body, segLine(color, width, segs...))
		}
		if len(visibleGroups(s, sel)) == 0 {
			body = append(body, segLine(color, width, seg{"  이 날짜에는 작업이 없습니다 (← → 로 날짜 이동)", stGray}))
		}
	}
	// 열 폭: 마커2 + 이름 + 구성 + 대수5 + 막대 + 경과7 + 완료율5 + 공백
	barW := 12
	if width >= 110 {
		barW = 20
	}
	rest := width - 2 - 5 - barW - 7 - 5 - 6
	nameW := rest * 55 / 100
	specW := rest - nameW
	for ji, j := range s.Jobs {
		if date != "" {
			any := false
			for gi := range j.Groups {
				any = any || groupDate(s, &s.Jobs[ji], &s.Jobs[ji].Groups[gi]) == date
			}
			if !any {
				continue
			}
		}
		hs := stBold
		tag := ""
		if j.Closed {
			hs, tag = stGray, " [종료]"
		}
		title := fmt.Sprintf("job %s  %s  전달 %s  %d대  완료 %d/%d%s", j.ID, j.User,
			fmtClock(s, j.Submitted, "01-02 15:04"), j.Counts.Total, j.Counts.Done, j.Counts.Total, tag)
		if j.AllYml != "" {
			title += "  all=" + j.AllYml
		}
		body = append(body, segLine(color, width, seg{" " + title, hs}))
		for gi, g := range j.Groups {
			if date != "" && groupDate(s, &s.Jobs[ji], &s.Jobs[ji].Groups[gi]) != date {
				continue
			}
			elapsed := "-"
			if g.MaxElapsed > 0 {
				elapsed = fmtDur(g.MaxElapsed)
			}
			segs := []seg{{"  ", ""}, {padR(g.Yml, nameW), ""}, {" ", ""}, {padR(specText(g), specW), stGray},
				{" ", ""}, {padL(fmt.Sprintf("%d대", g.Counts.Total), 5), ""}, {" ", ""}}
			segs = append(segs, barSegs(g.Counts, barW)...)
			segs = append(segs, seg{" ", ""}, seg{padL(elapsed, 7), ""}, seg{" ", ""})
			ps := stCyan
			if g.Complete {
				ps = stGreen
			} else if g.Counts.Stuck+g.Counts.Failed > 0 {
				ps = stRed
			}
			segs = append(segs, seg{padL(fmt.Sprintf("%d%%", pct(g.Counts)), 5), ps})
			if idx == sel.Row {
				segs[0].t = "> "
				cursor = len(body)
				body = append(body, selLine(color, width, segs))
			} else {
				body = append(body, segLine(color, width, segs...))
			}
			idx++
		}
	}
	hint := " ↑↓ 이동  ←→ 작업 전환  Enter 상세  a 전체 보기  r 새로고침  ? 도움말  q 종료"
	if sel.DateFilter {
		hint = " ↑↓ 이동  ←→ 작업 전환 (맨 위 날짜 줄에서는 날짜 이동)  Enter 상세  a 전체 보기  r 새로고침  ? 도움말  q 종료"
	}
	return finish(head, body, cursor, height, width, color, []string{
		msgLine(sel, width, color),
		segLine(color, width, seg{hint, stGray}),
	})
}

// finish: 머리 + (스크롤된) 본문 + 꼬리 조립. height<=0 이면 꼬리 없이 전체.
func finish(head, body []string, cursor, height, width int, color bool, foot []string) string {
	if height <= 0 {
		return strings.Join(append(head, body...), "\n")
	}
	avail := rMax(height-len(head)-len(foot), 1)
	lines := append(append([]string{}, head...), window(body, cursor, avail)...)
	return strings.Join(append(lines, foot...), "\n")
}

// ---- 화면 2 ----

func hostNoteText(h SnapHost, now int64) string {
	var p []string
	switch h.Stage {
	case StageInstalling:
		p = append(p, "anaconda")
	case StageBooting:
		p = append(p, "READY 대기")
	case StageStuck:
		p = append(p, "READY 안 됨")
	}
	if h.Stage.installPhase() && h.DownAt > 0 && now > h.DownAt {
		p = append(p, "설치 "+fmtDur(now-h.DownAt))
	}
	if h.Note != "" {
		p = append(p, h.Note)
	}
	return strings.Join(p, ", ")
}

func noteStyle(h SnapHost) string {
	n := h.Note
	if strings.Contains(n, "적용실패") || strings.Contains(n, "FAIL") || strings.Contains(n, "이름 미해결") || strings.Contains(n, "실패") {
		return stYellow
	}
	return ""
}

// hostTable: 호스트표 — 머리 줄, 행 목록, 커서 행 번호(host<0 이면 커서 없음)
func hostTable(hosts []SnapHost, host int, now int64, width int, color bool) (string, []string, int) {
	hostW := 8
	for _, h := range hosts {
		hostW = rMax(hostW, rMin(strWidth(cleanText(h.Host)), 28))
	}
	const stageW, elapsedW = 8, 7
	ymlW := 0 // 전체 보기(호스트에 Yml 이 있음)면 그룹 yml 열을 추가
	for _, h := range hosts {
		if h.Yml != "" {
			ymlW = rMax(ymlW, rMin(strWidth(cleanText(h.Yml)), 22))
		}
	}
	if ymlW > 0 {
		ymlW = rMax(ymlW, 9)
	}
	ymlCol := func(y string) string {
		if ymlW == 0 {
			return ""
		}
		return padR(truncW(y, ymlW), ymlW) + "  "
	}
	noteW := rMax(width-2-hostW-2-stageW-2-elapsedW-2, 0)
	if ymlW > 0 {
		noteW = rMax(noteW-ymlW-2, 0)
	}
	hdr := segLine(color, width, seg{"  " + padR("호스트", hostW) + "  " + ymlCol("그룹(yml)") + padR("단계", stageW) + "  " +
		padL("경과", elapsedW) + "  " + padR("비고", noteW), stBold})
	var lines []string
	cursor := 0
	for i, h := range hosts {
		el := "-"
		if h.Elapsed > 0 && h.Stage != StageDone {
			el = fmtDur(h.Elapsed)
		}
		mark := "  "
		if i == host {
			mark = "> "
		}
		label := h.Label
		if label == "" {
			label = h.Stage.Label()
		}
		segs := []seg{{mark, ""}, {padR(h.Host, hostW), ""}, {"  ", ""}, {ymlCol(h.Yml), stGray}, {padR(label, stageW), stageStyle(h.Stage)},
			{"  ", ""}, {padL(el, elapsedW), ""}, {"  ", ""}, {padR(hostNoteText(h, now), noteW), noteStyle(h)}}
		if i == host {
			cursor = len(lines)
			lines = append(lines, selLine(color, width, segs))
		} else {
			lines = append(lines, segLine(color, width, segs...))
		}
	}
	return hdr, lines, cursor
}

// renderDetail: 화면 2 — 선택 그룹의 호스트표, 수동 실행 안내. height<=0 이면 자르지 않음.
func renderDetail(s Snapshot, sel selection, width, height int, color bool) string {
	width = clampWidth(width)
	j, g := findGroup(s, sel.JobID, sel.Yml)
	if g == nil {
		head := titleLines(s, width, color)
		return strings.Join(append(head, sepLine(width), truncW(" 선택한 그룹을 찾을 수 없습니다 (작업이 종료되었을 수 있음). ← 로 돌아가세요.", width)), "\n")
	}
	state := ""
	if j.Closed {
		state = " [종료된 작업]"
	}
	title := fmt.Sprintf(" job %s / %s  (%s)  완료 %d/%d%s", j.ID, g.Yml, specText(*g), g.Counts.Done, g.Counts.Total, state)
	if sel.Yml == allView {
		title = fmt.Sprintf(" job %s / 전체 호스트 (그룹 %d개)  완료 %d/%d%s", j.ID, len(j.Groups), g.Counts.Done, g.Counts.Total, state)
	}
	head := []string{segLine(color, width, seg{title, stBold})}
	cs := []seg{{" ", ""}}
	for _, st := range StageOrder {
		if n := g.Counts.Get(st); n > 0 {
			cs = append(cs, seg{fmt.Sprintf("%s %d", st.Label(), n), stageStyle(st)}, seg{"  ", ""})
		}
	}
	if len(cs) == 1 {
		cs = append(cs, seg{"호스트 없음", stGray})
	}
	if sel.Filter {
		cs = append(cs, seg{"[필터: 정체·실패만]", stYellow})
	}
	head = append(head, segLine(color, width, cs...), sepLine(width))

	hosts := visibleHosts(g, sel.Filter)
	var body []string
	cursor := 0
	if len(hosts) == 0 {
		if sel.Filter {
			body = []string{" (정체·실패 호스트 없음)"}
		} else {
			body = []string{" (호스트 없음)"}
		}
	} else {
		var hdr string
		hdr, body, cursor = hostTable(hosts, sel.Host, s.Now, width, color)
		head = append(head, hdr)
	}

	foot := []string{sepLine(width), runInfoLine(s, j, g, width, color), manualLine(sel, g, width, color),
		msgLine(sel, width, color)}
	hint := " ↑↓ 이동  ← 목록  f 정체·실패만  c 수동 실행  g 재확인  a 전체 그룹  r 새로고침  ? 도움말  q 복귀"
	if sel.Yml == allView {
		hint = " ↑↓ 이동  ← 목록  f 정체·실패만  g 재확인  a 그룹별 보기  r 새로고침  ? 도움말  q 복귀"
	}
	foot = append(foot, segLine(color, width, seg{hint, stGray}))
	if height <= 0 {
		return strings.Join(append(head, body...), "\n")
	}
	return finish(head, body, cursor, height, width, color, foot)
}

// runInfoLine: 이 그룹의 수동 run 대기·진행 / 최근 결과 code
func runInfoLine(s Snapshot, j *SnapJob, g *SnapGroup, width int, color bool) string {
	if g.Yml == allView {
		return ""
	}
	for _, m := range j.Manual {
		if m.Yml != g.Yml {
			continue
		}
		if m.State == "running" {
			return segLine(color, width, seg{" 수동 run 진행 중 (체크중) — 요청 " + fmtClock(s, m.Requested, "15:04:05"), stCyan})
		}
		return segLine(color, width, seg{" 수동 run 대기 중 — 데몬이 곧 시작합니다 (요청 " + fmtClock(s, m.Requested, "15:04:05") + ")", stCyan})
	}
	for i := len(j.Runs) - 1; i >= 0; i-- {
		r := j.Runs[i]
		if r.Manual && r.Yml == g.Yml {
			return segLine(color, width, seg{fmt.Sprintf(" 최근 수동 run: code %s (%s) → auto_setup code %s", r.Code,
				fmtClock(s, r.At, "15:04:05"), r.Code), stGreen})
		}
	}
	return ""
}

// manualLine: [c] OS 체크 수동 실행 — 완료 여부와 상관없이 활성(그룹 전체 시도, 접속불가는 wall 에 표시), 확인 중이면 y/n 질문
func manualLine(sel selection, g *SnapGroup, width int, color bool) string {
	if g.Yml == allView {
		return segLine(color, width, seg{" 전체 보기 - 수동 실행(c)은 그룹별 화면에서 하세요 (a 로 돌아가 그룹 선택)", stGray})
	}
	if sel.Confirm {
		extra := ""
		if n := g.Counts.Total - g.Counts.Done; n > 0 {
			extra = fmt.Sprintf(", 미완료 %d대 포함", n)
		}
		return segLine(color, width, seg{fmt.Sprintf(" %s (%d대%s)에 OS 체크(이중체크)를 실행합니다. 계속할까요? [y/n]", g.Yml, g.Counts.Total, extra), stYellow})
	}
	return segLine(color, width, seg{" [c] OS 체크 수동 실행(이중체크) - 미완료 호스트도 시도, 접속불가는 알림", "1;32"})
}

// ---- 도움말 ----

func renderHelp(width, height int, color bool) string {
	width = clampWidth(width)
	lines := []string{
		segLine(color, width, seg{" auto_setup 상태 리포트 - 도움말", stBold}),
		sepLine(width),
		" 화면 1 (작업·그룹 목록)",
		"   ↑ ↓ (k j)   그룹 행 이동",
		"   ← →         이전/다음 작업(job) 으로 이동",
		"   ↑ (맨 위)   날짜 줄 선택 → ← 전날 / → 다음날 (그룹 yml 이름의 시각 기준, 기본은 가장 최근 날짜)",
		"   Enter       선택한 그룹의 호스트표(화면 2)",
		"   a           선택한 job 의 모든 그룹(yml) 호스트를 한 표로 (그룹 열 표시)",
		"   r           바로 새로고침 + 데몬에 즉시 ping·준비확인 요청 (자동: 로컬 2초, 원격 5초)",
		"               정체 호스트는 ping 상태와 상관없이 즉시 다시 확인(수동 재시도)",
		"   q / Esc     종료",
		" 화면 2 (호스트표)",
		"   ↑ ↓ (k j)   호스트 행 이동",
		"   ← / Esc / q 화면 1 로 복귀",
		"   f           정체·실패 호스트만 보기 (토글)",
		"   c           OS 체크 수동 실행(이중체크) - 완료 여부와 상관없이 그룹 전체 시도, 접속불가는 알림, y/n 확인",
		"   g           서버 상태 수동 재확인 - 완료 제외 모든 호스트를 os8_mgmt 에서 확인, 안 되면 os6_mgmt 경유, 둘 다 안 되면 접속불가(로그)",
		" 색: 완료 초록 / 진행 청록 / 대기 회색 / 경고 노랑 / 정체·실패 빨강. NO_COLOR 설정 시 색 없음",
		" 막대: # 완료  + 진행  . 대기  ! 정체·실패",
		" %: 완료 대수 / 전체 (배포중·설치중·부팅확인·체크중은 + 로만 보이고 % 는 완료될 때 오름)",
		"",
		segLine(color, width, seg{" 아무 키나 누르면 닫힙니다", stGray}),
	}
	for i, l := range lines {
		lines[i] = truncW(l, width)
		if strings.Contains(l, "\x1b") {
			lines[i] = l
		}
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// ---- 비 tty / --plain ----

// renderPlain: 색 없는 텍스트 한 번 출력 — 화면 1 전체 + 정체·실패 호스트가 있는 그룹의 호스트표
func renderPlain(s Snapshot, width int) string {
	var sb strings.Builder
	sb.WriteString(renderOverview(s, selection{Row: -1}, width, 0, false))
	sb.WriteByte('\n')
	width = clampWidth(width)
	for _, j := range s.Jobs {
		for _, g := range j.Groups {
			g := g
			hosts := visibleHosts(&g, true)
			if len(hosts) == 0 {
				continue
			}
			sb.WriteString(fmt.Sprintf("\n[정체·실패] job %s / %s\n", cleanText(j.ID), cleanText(g.Yml)))
			hdr, rows, _ := hostTable(hosts, -1, s.Now, width, false)
			lines := append([]string{hdr}, rows...)
			sb.WriteString(strings.Join(lines, "\n"))
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// allView: 화면 2 의 특수 그룹 이름 — 한 job 의 모든 그룹(yml) 호스트를 한 표로 본다 (TUI a 키)
const allView = "*"

// mergedGroup: job 의 모든 그룹 호스트를 그룹 순서대로 이어 붙인 가상 그룹 (그룹이 없으면 nil).
// 호스트마다 Yml 을 채워 표에 yml 열이 나온다. 수동 실행 대상이 아니다(그룹별로 실행).
func mergedGroup(j *SnapJob) *SnapGroup {
	if len(j.Groups) == 0 {
		return nil
	}
	m := &SnapGroup{Yml: allView, Hosts: []SnapHost{}, Complete: true}
	for _, g := range j.Groups {
		m.Counts.merge(g.Counts)
		if g.MaxElapsed > m.MaxElapsed {
			m.MaxElapsed = g.MaxElapsed
		}
		if !g.Complete {
			m.Complete = false
		}
		for _, h := range g.Hosts {
			h.Yml = g.Yml
			m.Hosts = append(m.Hosts, h)
		}
	}
	return m
}
