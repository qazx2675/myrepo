package main

// -run 화면의 리포트(p 키): 키 처리(handleReportKey)·화면(reportLines)·파일 접근(reportIO).
// 글 만들기·기준 읽기는 report.go 의 순수 함수를 쓴다. 계획서_report.md 3.4·3.6·3.7 참고.
// 파일·설정·시계는 reportIO 로 주입해 tty·디스크 없이 시험한다.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// reportIO 는 리포트가 밖과 주고받는 동작(시험에서는 가짜를 넣는다). 함수가 nil 이면 그 동작이 없는 것으로 본다.
type reportIO struct {
	// conf 는 설정 파일(confPath, 빈 값이면 기본값)에서 벤더 규칙·REPORT_ETA_DAYS·경고를 얻는다. 읽기 실패는 err 로 알린다(규칙 없음으로 계속).
	conf func(confPath string) (rules []vendorRule, etaDays int, warns []string, err error)
	// baseline 은 방식 1: 상태 폴더에서 오늘보다 앞의 가장 최근 스냅샷을 읽어 기준을 만든다(daysAgo = 며칠 전 저장본인지).
	baseline func(cur []repHost, rules []vendorRule, today time.Time) (rb *repBase, daysAgo int, err error)
	// readFile 은 코멘트 첫 줄의 @경로 파일을 읽는다.
	readFile func(path string) (string, error)
	// save 는 오늘 스냅샷과 report_YYYYMMDD.txt(+ 두 본이면 _B)를 쓴다. 쓴 txt 경로를 돌려준다.
	save func(today time.Time, snap repSnapshot, a, b string) (path string, err error)
}

const (
	repPick    = "pick"
	repInput   = "input"
	repConfirm = "confirm"
	repShow    = "show"
	repErr     = "err"

	flashRepCancel = "취소했습니다"
	repTitleA      = "[본 A: 0대 줄과 조치완료 적음]"
	repTitleB      = "[본 B: 접속불가 블록 적지 않음]"
	repNoVendor    = "VENDOR_ 규칙이 없어 모두 기타 로 묶었습니다"
)

// startReport 는 p 키: 설정을 읽고 기준 고르기 화면으로 간다. 호스트가 없으면 오류 화면.
func startReport(v *viewState, st *runState, now time.Time, act tuiActions) {
	v.RepInput, v.RepBase, v.RepNotes, v.RepLines, v.RepScroll, v.RepPath, v.RepLabel = nil, nil, nil, nil, 0, "", ""
	v.RepDate = now
	if len(st.Hosts) == 0 {
		repFail(v, "호스트가 없어 리포트를 만들 수 없습니다")
		return
	}
	v.RepRules, v.RepETA, v.RepWarns = nil, defaultReportETADays, nil
	if act.report.conf != nil {
		rules, eta, warns, err := act.report.conf(st.ConfPath)
		v.RepRules, v.RepETA, v.RepWarns = rules, eta, warns
		if err != nil {
			v.RepWarns = append(v.RepWarns, "설정 파일을 읽을 수 없습니다: "+err.Error())
		}
	}
	if len(v.RepRules) == 0 {
		v.RepWarns = append(v.RepWarns, repNoVendor)
	}
	v.Rep = repPick
}

func repFail(v *viewState, msg string) {
	v.Rep, v.RepLines = repErr, []string{msg}
}

func repCancel(v *viewState) {
	v.Rep = ""
	v.Flash = flashRepCancel
}

// repKnown 은 코멘트 해석기에 넘기는 벤더 이름: 설정의 이름들과 '기타'.
func repKnown(rules []vendorRule) []string {
	return append(vendorOrder(rules), vendorOther)
}

// handleReportKey 는 리포트 화면의 키 하나를 처리한다(normKey 를 거치지 않은 원래 키). quit 이 true 면 TUI 를 끝낸다.
// 입력 상태에서만 원래 키(공백·대소문자·한글 그대로)를 쓰고, 그 밖의 상태에서는 normKey 를 거친 키를 쓴다.
func handleReportKey(v *viewState, st *runState, key string, h int, now time.Time, act tuiActions) (quit bool) {
	if key == "ctrl-c" { // 입력 중에도 Ctrl-C 는 TUI 를 끝낸다(평소 동작과 같다)
		return true
	}
	if v.Rep == repInput {
		switch key {
		case "esc":
			repCancel(v)
		case "enter":
			v.RepInput = append(v.RepInput, '\n')
		case "tab":
			v.RepInput = append(v.RepInput, ' ')
		case "backspace":
			if n := len(v.RepInput); n > 0 {
				v.RepInput = v.RepInput[:n-1]
			}
		case "ctrl-d":
			finishInput(v, st, act)
		default:
			if utf8.RuneCountInString(key) == 1 { // 글자 하나(공백·대문자·한글 포함). 이름이 있는 키(up 등)는 버린다
				r, _ := utf8.DecodeRuneInString(key)
				v.RepInput = append(v.RepInput, r)
			}
		}
		return false
	}
	key = normKey(key)
	switch v.Rep {
	case repPick:
		switch key {
		case "1":
			if st == nil {
				repCancel(v)
				return false
			}
			cur := newRepHosts(st.Hosts, v.RepRules)
			if act.report.baseline == nil {
				repFail(v, "전일 저장본을 읽을 수 없습니다. 2(코멘트 입력)를 쓰십시오.")
				return false
			}
			rb, days, err := act.report.baseline(cur, v.RepRules, now)
			if err != nil {
				repFail(v, err.Error())
				return false
			}
			label := rb.Label
			if days > 1 {
				label += fmt.Sprintf("(%d일 전)", days)
			}
			makeReport(v, st, rb, label, now, act)
		case "2":
			v.Rep, v.RepInput = repInput, nil
		default:
			repCancel(v)
		}
	case repConfirm:
		if key == "y" && st != nil {
			makeReport(v, st, v.RepBase, v.RepBase.Label, now, act)
		} else {
			repCancel(v)
		}
	case repShow:
		cp := repBodyCap(v, h)
		maxScroll := len(v.RepLines) - cp
		switch key {
		case "up":
			v.RepScroll--
		case "down":
			v.RepScroll++
		case "pgup":
			v.RepScroll -= cp
		case "pgdn":
			v.RepScroll += cp
		case "home":
			v.RepScroll = 0
		case "end":
			v.RepScroll = 1 << 30
		default:
			v.Rep = ""
			return false
		}
		if v.RepScroll > maxScroll {
			v.RepScroll = maxScroll
		}
		if v.RepScroll < 0 {
			v.RepScroll = 0
		}
	default: // repErr: 아무 키나 누르면 닫힘
		v.Rep = ""
	}
	return false
}

// finishInput 은 Ctrl-D: 입력을 끝내고 코멘트를 해석해 확인 화면으로 간다. 첫 줄이 @경로 이면 그 파일을 코멘트로 읽는다.
func finishInput(v *viewState, st *runState, act tuiActions) {
	text := string(v.RepInput)
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "@") {
			p := strings.TrimSpace(l[1:])
			if p == "" || act.report.readFile == nil {
				repFail(v, "코멘트 파일을 읽을 수 없습니다: 경로가 없습니다")
				return
			}
			b, err := act.report.readFile(p)
			if err != nil {
				repFail(v, "코멘트 파일을 읽을 수 없습니다: "+err.Error())
				return
			}
			text = b
		}
		break
	}
	if err := checkCommentSize(text); err != nil {
		repFail(v, err.Error())
		return
	}
	known := repKnown(v.RepRules)
	rb, notes := parseComment(text, known)
	if st != nil {
		if n := unknownCommentNames(rb, newRepHosts(st.Hosts, v.RepRules)); n > 0 {
			notes = append(notes, fmt.Sprintf("현재 목록에 없는 이름 %d개", n))
		}
	}
	v.RepBase, v.RepNotes = rb, notes
	v.RepLines = confirmText(rb, known, notes)
	v.Rep = repConfirm
}

func numText(p *int) string {
	if p == nil {
		return "없음"
	}
	return fmt.Sprintf("%d", *p)
}

// confirmText 는 확인 화면의 글: 해석한 내용을 벤더마다 한 줄로(계획서 3.4).
func confirmText(rb *repBase, known []string, notes []string) []string {
	out := []string{"코멘트에서 읽은 내용 (y 계속 / 다른 키 취소)"}
	for _, name := range known {
		bv := rb.Vendors[strings.ToLower(name)]
		if bv == nil {
			out = append(out, fmt.Sprintf("  %s  전일 정보 없음", name))
			continue
		}
		l := fmt.Sprintf("  %s  OS완료 %s  부하완료 %s  접속불가 %s", name, numText(bv.OSDone), numText(bv.LoadDone), numText(bv.Unreach))
		if bv.HasNames && len(bv.Names) > 0 {
			l += " (" + strings.Join(bv.Names, " ") + ")"
		}
		out = append(out, l)
	}
	for _, n := range notes {
		out = append(out, "알림: "+n)
	}
	return out
}

// makeReport 는 현재 값과 기준으로 리포트를 만들고 저장한 뒤 보이기 화면으로 간다. 저장이 실패해도 리포트는 그대로 보인다.
func makeReport(v *viewState, st *runState, rb *repBase, label string, now time.Time, act tuiActions) {
	if len(st.Hosts) == 0 {
		repFail(v, "호스트가 없어 리포트를 만들 수 없습니다")
		return
	}
	cur := newRepHosts(st.Hosts, v.RepRules)
	a, b := buildReport(cur, vendorOrder(v.RepRules), rb, now, v.RepETA)
	v.RepPath = ""
	if act.report.save != nil {
		p, err := act.report.save(now, makeSnapshot(cur, now), a, b)
		if err != nil {
			v.RepPath = "저장 실패: " + err.Error()
		} else {
			v.RepPath = p
		}
	}
	v.RepLines = reportBody(a, b)
	v.RepLabel, v.RepDate, v.RepScroll, v.Rep = label, now, 0, repShow
}

// textLines 는 글을 줄로 나눈다(끝의 개행 하나는 줄을 만들지 않는다).
func textLines(s string) []string {
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// reportBody 는 화면에 보일 리포트 줄. 두 본이면 제목을 붙여 위아래로 보인다(제목은 파일에 쓰지 않는다).
func reportBody(a, b string) []string {
	if b == "" {
		return textLines(a)
	}
	out := []string{repTitleA}
	out = append(out, textLines(a)...)
	out = append(out, "", repTitleB)
	return append(out, textLines(b)...)
}

// repBodyCap 은 리포트 화면에서 글이 쓸 수 있는 줄 수(첫 줄·경고·빈 줄·아래 안내 줄을 뺀 나머지).
func repBodyCap(v *viewState, h int) int {
	c := h - 3 - len(v.RepWarns)
	if c < 1 {
		c = 1
	}
	return c
}

// reportLines 는 리포트 화면(정확히 h 줄). 상태 5가지를 각각 그린다.
func reportLines(st *runState, view viewState, w, h int) []string {
	nc := view.NoColor
	var out []string
	add := func(s, code string) { out = append(out, lineOf(w, nc, seg{cleanLogLine(s), code})) }
	switch view.Rep {
	case repPick:
		add("리포트 — 전일대비 기준을 고르십시오", cBold)
		add("", "")
		add("  1  전일 저장본(상태 폴더의 report_YYYYMMDD.json)", cQKey)
		add("  2  코멘트 입력(전날 리포트 글 붙여넣기 또는 @파일)", cQKey)
		add("  다른 키  취소", "")
		for _, wn := range view.RepWarns {
			add("! "+wn, cYellow)
		}
	case repInput:
		add("전날 리포트 글을 붙여넣으십시오 — Ctrl-D 완료, Esc 취소, 파일은 첫 줄에 @경로", cBold)
		text := strings.Split(string(view.RepInput), "\n")
		n := 0
		if len(view.RepInput) > 0 {
			n = len(text)
		}
		text[len(text)-1] += "_" // 커서
		room := h - 2
		if room < 1 {
			room = 1
		}
		if len(text) > room {
			text = text[len(text)-room:]
		}
		for _, l := range text {
			add(l, "")
		}
		for len(out) < h-1 {
			out = append(out, "")
		}
		add(fmt.Sprintf("%d줄 입력됨", n), cDim)
	case repConfirm:
		for i, l := range view.RepLines {
			code := ""
			switch {
			case i == 0:
				code = cBold
			case strings.HasPrefix(l, "알림:"):
				code = cYellow
			}
			add(l, code)
		}
	case repShow:
		head := "리포트 " + view.RepDate.Format("2006-01-02") + " — 기준: " + view.RepLabel
		if !st.Updated.IsZero() {
			head += " — 판정 시각 " + st.Updated.Format("01/02 15:04:05")
		}
		if view.RepPath != "" {
			if strings.HasPrefix(view.RepPath, "저장 실패") {
				head += " — " + view.RepPath
			} else {
				head += " — 저장: " + view.RepPath
			}
		}
		add(head, cBold)
		for _, wn := range view.RepWarns {
			add("! "+wn, cYellow)
		}
		add("", "")
		capRows := repBodyCap(&view, h)
		start := view.RepScroll
		if start > len(view.RepLines)-capRows {
			start = len(view.RepLines) - capRows
		}
		if start < 0 {
			start = 0
		}
		end := start + capRows
		if end > len(view.RepLines) {
			end = len(view.RepLines)
		}
		for _, l := range view.RepLines[start:end] { // 복사한 글에 다른 글자가 섞이지 않게 색·표시를 붙이지 않는다
			add(l, "")
		}
		for len(out) < h-1 {
			out = append(out, "")
		}
		first := start + 1
		if first > len(view.RepLines) {
			first = len(view.RepLines)
		}
		add(fmt.Sprintf("↑↓ PgUp/PgDn Home/End 스크롤 · 그 밖의 키 닫기 · %d-%d / %d줄", first, end, len(view.RepLines)), cDim)
	default: // repErr
		add("리포트를 만들 수 없습니다", cRed)
		add("", "")
		for _, m := range view.RepLines {
			for _, l := range wrapW(m, w-2) {
				add("  "+l, "")
			}
		}
		for len(out) < h-1 {
			out = append(out, "")
		}
		add("아무 키나 누르면 닫힘", cDim)
	}
	return out
}

// ---------------------------------------------------------------------------
// 실제 reportIO
// ---------------------------------------------------------------------------

// newReportIO 는 상태 폴더 dir 을 쓰는 실제 reportIO.
func newReportIO(dir string) reportIO {
	return reportIO{
		conf: func(confPath string) ([]vendorRule, int, []string, error) {
			if confPath == "" { // 기본값으로 시작한 작업: 규칙 없음
				return nil, defaultReportETADays, nil, nil
			}
			// loadConf 는 stderr 에 글을 내서 TUI 화면을 깨므로 쓰지 않는다.
			b, err := os.ReadFile(confPath)
			if err != nil {
				return nil, defaultReportETADays, nil, err
			}
			c := parseConf(string(b))
			var warns []string
			for _, wn := range c.Warnings {
				if strings.Contains(wn, "VENDOR_") || strings.Contains(wn, "REPORT_ETA_DAYS") {
					warns = append(warns, wn)
				}
			}
			return c.Vendors, c.ReportETADays, warns, nil
		},
		baseline: func(cur []repHost, rules []vendorRule, today time.Time) (*repBase, int, error) {
			return loadBaseline(dir, cur, rules, today)
		},
		readFile: func(path string) (string, error) {
			b, err := os.ReadFile(path)
			return string(b), err
		},
		save: func(today time.Time, snap repSnapshot, a, b string) (string, error) {
			return saveReportFiles(dir, today, snap, a, b)
		},
	}
}

// saveReportFiles 는 dir 에 스냅샷 json 과 report_YYYYMMDD.txt 를 쓴다(같은 날 파일은 덮어쓴다).
// 두 본이면 report_YYYYMMDD_B.txt 도 쓰고, 본이 하나면 옛 _B 파일을 지운다. 쓴 txt 경로를 돌려준다.
func saveReportFiles(dir string, today time.Time, snap repSnapshot, a, b string) (string, error) {
	if _, err := writeSnapshot(dir, snap, today); err != nil {
		return "", err
	}
	pa := filepath.Join(dir, reportTextFileName(today, false))
	if err := writeFileAtomic(pa, []byte(a)); err != nil {
		return "", err
	}
	pb := filepath.Join(dir, reportTextFileName(today, true))
	if b != "" {
		if err := writeFileAtomic(pb, []byte(b)); err != nil {
			return "", err
		}
	} else if err := os.Remove(pb); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return pa, nil
}
