package main

// -run TUI: raw tty 입력 처리와 화면 갱신 루프.
// 키 해석(parseKeys·splitKeys·normKey)과 키 처리(handleKey)는 tty 없이 시험할 수 있게 순수하게 둔다.
// 화면은 1초마다 state.json 을 다시 읽어 그리고, 키를 누르면 바로 다시 그린다.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// parseKeys 는 터미널에서 읽은 바이트를 키 이름으로 나눈다.
// 방향키 등은 "up" "down" "left" "right" "pgup" "pgdn" "home" "end", Shift-Tab 은 "shift-tab", 단독 ESC 는 "esc",
// Ctrl-C 는 "ctrl-c", Ctrl-D 는 "ctrl-d"(리포트 코멘트 입력 끝), Ctrl-L 은 "ctrl-l", Tab 은 "tab", 엔터는 "enter", 그 밖에는 그 글자 그대로. 모르는 이스케이프 열은 버린다.
func parseKeys(b []byte) []string {
	var keys []string
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			if i+1 >= len(b) || (b[i+1] != '[' && b[i+1] != 'O') {
				keys = append(keys, "esc")
				i++
				continue
			}
			j := i + 2
			for j < len(b) && !(b[j] >= 0x40 && b[j] <= 0x7e) {
				j++
			}
			if j >= len(b) {
				i = len(b)
				continue
			}
			switch string(b[i+2 : j+1]) {
			case "A":
				keys = append(keys, "up")
			case "B":
				keys = append(keys, "down")
			case "C":
				keys = append(keys, "right")
			case "D":
				keys = append(keys, "left")
			case "Z":
				keys = append(keys, "shift-tab")
			case "5~":
				keys = append(keys, "pgup")
			case "6~":
				keys = append(keys, "pgdn")
			case "H", "1~", "7~":
				keys = append(keys, "home")
			case "F", "4~", "8~":
				keys = append(keys, "end")
			}
			i = j + 1
		case c == 0x03:
			keys = append(keys, "ctrl-c")
			i++
		case c == 0x09:
			keys = append(keys, "tab")
			i++
		case c == 0x0c:
			keys = append(keys, "ctrl-l")
			i++
		case c == 0x04:
			keys = append(keys, "ctrl-d")
			i++
		case c == 0x7f || c == 0x08:
			keys = append(keys, "backspace")
			i++
		case c == '\r' || c == '\n':
			keys = append(keys, "enter")
			i++
		case c < 0x20:
			i++
		default:
			r, n := utf8.DecodeRune(b[i:])
			if r == utf8.RuneError && n <= 1 {
				i++
				continue
			}
			keys = append(keys, string(r))
			i += n
		}
	}
	return keys
}

// splitKeys 는 읽은 바이트를 끝까지 온 부분(done)과 끝이 덜 온 꼬리(tail)로 나눈다.
// 꼬리는 끝에 홀로 남은 ESC, 마지막 글자가 아직 안 온 ESC [ … / ESC O …, 바이트가 모자란 UTF-8 글자(한글 등)다.
// readKeys 는 꼬리를 다음 읽기와 이어 붙이고, escWait 안에 안 오면 flushTail 로 처리한다.
func splitKeys(b []byte) (done, tail []byte) {
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			if i+1 >= len(b) {
				return b[:i], b[i:]
			}
			if b[i+1] != '[' && b[i+1] != 'O' {
				i++
				continue
			}
			j := i + 2
			for j < len(b) && !(b[j] >= 0x40 && b[j] <= 0x7e) {
				j++
			}
			if j >= len(b) {
				return b[:i], b[i:]
			}
			i = j + 1
		case c >= 0x80:
			if !utf8.FullRune(b[i:]) {
				return b[:i], b[i:]
			}
			_, n := utf8.DecodeRune(b[i:])
			i += n
		default:
			i++
		}
	}
	return b, nil
}

// flushTail 은 escWait 안에 이어지지 않은 꼬리의 키: ESC 로 시작하면 "esc"(단독 Esc 키), 덜 온 UTF-8 은 버린다.
func flushTail(tail []byte) []string {
	if len(tail) > 0 && tail[0] == 0x1b {
		return []string{"esc"}
	}
	return nil
}

// escWait 는 덜 온 이스케이프 열의 나머지를 기다리는 시간.
const escWait = 50 * time.Millisecond

// jamoKey 는 두벌식 자판의 호환 자모 → 같은 자리 영문 키. 쌍자음·ㅒ·ㅖ 는 Shift 자리라 대문자(뒤에서 소문자로 접힌다).
var jamoKey = map[string]string{
	"ㅂ": "q", "ㅈ": "w", "ㄷ": "e", "ㄱ": "r", "ㅅ": "t", "ㅛ": "y", "ㅕ": "u", "ㅑ": "i", "ㅐ": "o", "ㅔ": "p",
	"ㅁ": "a", "ㄴ": "s", "ㅇ": "d", "ㄹ": "f", "ㅎ": "g", "ㅗ": "h", "ㅓ": "j", "ㅏ": "k", "ㅣ": "l",
	"ㅋ": "z", "ㅌ": "x", "ㅊ": "c", "ㅍ": "v", "ㅠ": "b", "ㅜ": "n", "ㅡ": "m",
	"ㅃ": "Q", "ㅉ": "W", "ㄸ": "E", "ㄲ": "R", "ㅆ": "T", "ㅒ": "O", "ㅖ": "P",
}

// normKey 는 키를 처리하기 전에 한 가지로 맞춘다: 한글 자모 → 같은 자리 영문, g/G → 표 처음/끝(대소문자를 접기 전에),
// 영문 대문자 → 소문자(Caps Lock), Space/b → 한 화면 아래/위. 숫자·특수 키 이름은 그대로.
func normKey(key string) string {
	if k, ok := jamoKey[key]; ok {
		key = k
	}
	switch key {
	case "g":
		return "home"
	case "G":
		return "end"
	}
	if len(key) == 1 && key[0] >= 'A' && key[0] <= 'Z' {
		key = string(key[0] + 'a' - 'A')
	}
	switch key {
	case " ":
		return "pgdn"
	case "b":
		return "pgup"
	}
	return key
}

// tuiActions 는 키 처리가 밖에 내는 동작(시험에서는 가짜를 넣는다).
type tuiActions struct {
	answer func(id, choice string, values map[string]string) error // answer.json 쓰기(values 는 입력 질문의 값, 없으면 nil)
	stop   func() error                                            // stop.req 쓰기
	report reportIO                                                // p 리포트의 파일·설정 접근(빈 값이면 설정 없음·저장 안 함)
}

// 화면 알림 문구
const (
	flashUnknownKey = "쓸 수 없는 키입니다 — ? 도움말"
	flashSent       = "이미 답을 보냈습니다 — 워커가 받을 때까지 기다리세요"
)

// handleKey 는 키 하나를 처리해 view 를 바꾼다. quit 이 true 면 TUI 를 끝낸다(작업은 계속).
// st 는 마지막으로 읽은 상태(nil 가능), alive 는 워커 생존 여부, w·h 는 화면 크기(스크롤 단위 계산용).
// 입력 질문이 떠 있으면 숫자·Enter·Tab·←→·Backspace·Esc 는 종료 시각 칸 입력으로 가고, 그 밖의 키는 평소대로 동작한다.
func handleKey(v *viewState, st *runState, alive bool, key string, w, h int, now time.Time, act tuiActions) (quit bool) {
	v.Flash = ""
	v.sync(st, now)
	if v.Rep != "" { // 리포트 화면: 코멘트 입력은 공백·대소문자가 그대로여야 하므로 normKey 보다 먼저 가른다
		return handleReportKey(v, st, key, h, now, act)
	}
	key = normKey(key)
	if v.Help { // 도움말은 아무 키나 누르면 닫힘
		v.Help = false
		return false
	}
	if v.Confirm != "" { // y/N 확인 중
		kind, qid := v.Confirm, v.ConfirmQ
		v.Confirm, v.ConfirmQ = "", ""
		if key != "y" {
			v.Flash = "취소했습니다"
			return false
		}
		var err error
		if kind == "s" {
			err = act.answer(qid, ansStop, nil)
		} else {
			err = act.stop()
		}
		if err != nil {
			v.Flash = "전송 실패: " + err.Error()
		} else {
			v.Flash = "중지를 요청했습니다"
			if kind == "s" {
				v.markSent(qid, ansStop, now)
			}
		}
		return false
	}
	switch key {
	case "q", "ctrl-c":
		return true
	case "?":
		v.Help = true
		return false
	case "f":
		v.OnlyBad = !v.OnlyBad
		v.Scroll = 0
		return false
	case "l":
		v.NoLog = !v.NoLog
		return false
	case "v":
		v.Result = !v.Result
		return false
	case "ctrl-l": // 화면 다시 그리기(runTUI 가 화면을 지우고 그린다)
		return false
	case "p": // 벤더별 리포트(상태를 아직 못 읽었으면 아무것도 안 함)
		if st != nil {
			startReport(v, st, now, act)
		}
		return false
	case "up", "down", "pgup", "pgdn", "home", "end":
		if st == nil {
			return false
		}
		capRows := tableCapacity(st, *v, w, h, now, alive)
		switch key {
		case "up":
			v.Scroll--
		case "down":
			v.Scroll++
		case "pgup":
			v.Scroll -= capRows
		case "pgdn":
			v.Scroll += capRows
		case "home":
			v.Scroll = 0
		case "end":
			v.Scroll = 1 << 30
		}
		maxScroll := len(visibleRows(st, v.OnlyBad)) - capRows
		if v.Scroll > maxScroll {
			v.Scroll = maxScroll
		}
		if v.Scroll < 0 {
			v.Scroll = 0
		}
		return false
	}
	if st == nil {
		return false
	}
	q := st.Question
	if q != nil && len(q.Inputs) > 0 && isInputKey(key) {
		if v.SentQ == q.ID {
			v.Flash = flashSent
			return false
		}
		handleInputKey(v, q, alive, key, now, act)
		return false
	}
	if q != nil && hasChoice(q, key) {
		if v.SentQ == q.ID {
			v.Flash = flashSent
			return false
		}
		if !alive {
			v.Flash = "워커가 실행 중이 아니어서 답을 받을 수 없습니다"
			return false
		}
		if key == ansStop { // 중지는 y/N 확인 뒤에 보낸다
			v.Confirm, v.ConfirmQ = "s", q.ID
			return false
		}
		if len(q.Inputs) > 0 && key == ansCont { // 입력 질문의 c 는 칸 값을 검사해 함께 보낸다
			submitInput(v, q, alive, now, act)
			return false
		}
		if err := act.answer(q.ID, key, nil); err != nil {
			v.Flash = "답 전송 실패: " + err.Error()
		} else {
			v.Flash = "답을 보냈습니다: [" + key + "] " + choiceLabel(q, key)
			v.markSent(q.ID, key, now)
		}
		return false
	}
	if key == "s" { // 중지 선택지가 없는 질문(인벤토리 사용 중 등)이거나 질문이 없을 때: 작업 전체 중지
		switch {
		case st.Finished():
			v.Flash = "이미 끝난 작업입니다"
		case !alive:
			v.Flash = "워커가 실행 중이 아닙니다"
		default:
			v.Confirm = "S"
		}
		return false
	}
	if key != "esc" {
		v.Flash = flashUnknownKey
	}
	return false
}

// sentHold 는 답을 보낸 뒤 같은 질문에 다시 답하지 않고 기다리는 최대 시간.
const sentHold = 5 * time.Second

// markSent 는 질문 id 에 답 key 를 보냈다고 기록한다(sync 가 질문이 바뀌거나 sentHold 가 지나면 푼다).
func (v *viewState) markSent(id, key string, now time.Time) {
	v.SentQ, v.SentKey, v.SentAt = id, key, now
}

// sync 는 화면 상태를 지금 질문에 맞춘다. runTUI(state.json 을 읽은 뒤)·handleKey·renderRun 이 부른다.
//   - 보낸 답 기록: 질문이 바뀌었거나 사라졌거나 sentHold 가 지났으면 푼다.
//   - 입력 질문: 처음 보는 질문이면 칸을 기본값(now 의 연·월·일·시·분)으로 채우고 첫 칸에 둔다. 입력 질문이 없으면 칸 상태를 지운다.
func (v *viewState) sync(st *runState, now time.Time) {
	var q *question
	if st != nil {
		q = st.Question
	}
	if v.SentQ != "" && (q == nil || q.ID != v.SentQ || now.Sub(v.SentAt) >= sentHold) {
		v.SentQ, v.SentKey, v.SentAt = "", "", time.Time{}
	}
	if q == nil || len(q.Inputs) == 0 {
		if v.InQ != "" {
			v.InQ, v.InIdx, v.InVals, v.InFresh, v.InErr = "", 0, nil, false, ""
		}
		return
	}
	if v.InQ != q.ID || v.InIdx >= len(q.Inputs) || v.InVals == nil {
		v.InQ, v.InIdx, v.InVals, v.InFresh, v.InErr = q.ID, 0, endDefaults(q.Inputs, now), true, ""
	}
}

// endDefaults 는 입력 칸의 기본값: 연 4자리, 월·일·시·분 두 자리(예 "07").
func endDefaults(inputs []inputField, now time.Time) map[string]string {
	m := map[string]string{}
	for _, f := range inputs {
		switch f.Key {
		case "end_year":
			m[f.Key] = fmt.Sprintf("%04d", now.Year())
		case "end_month":
			m[f.Key] = fmt.Sprintf("%02d", int(now.Month()))
		case "end_day":
			m[f.Key] = fmt.Sprintf("%02d", now.Day())
		case "end_hour":
			m[f.Key] = fmt.Sprintf("%02d", now.Hour())
		case "end_min":
			m[f.Key] = fmt.Sprintf("%02d", now.Minute())
		default:
			m[f.Key] = ""
		}
	}
	return m
}

// fieldWidth 는 입력 칸의 자리 수(연 4, 나머지 2). 칸이 이만큼 차면 다음 칸으로 넘어간다.
func fieldWidth(key string) int {
	if key == "end_year" {
		return 4
	}
	return 2
}

// isInputKey 는 입력 질문에서 칸 입력으로 가는 키인지.
func isInputKey(key string) bool {
	switch key {
	case "enter", "tab", "shift-tab", "left", "right", "backspace", "esc":
		return true
	}
	return len(key) == 1 && key[0] >= '0' && key[0] <= '9'
}

// choiceLabel 은 질문 q 의 선택지 key 의 설명.
func choiceLabel(q *question, key string) string {
	for _, c := range q.Choices {
		if c.Key == key {
			return c.Label
		}
	}
	return ""
}

// handleInputKey 는 입력 질문의 칸 입력: 숫자(기본값이 든 칸은 첫 숫자가 대신, 칸이 차면 다음 칸), Enter·Tab·→ 다음 칸
// (마지막 칸의 Enter 는 제출), ←·Shift-Tab 이전 칸, Backspace 한 글자 지움(빈 칸이면 이전 칸), Esc 모든 칸을 기본값으로.
// 앞으로 넘어갈 때 칸을 검사해 틀리면 글자를 둔 채 그 칸에 머문다.
func handleInputKey(v *viewState, q *question, alive bool, key string, now time.Time, act tuiActions) {
	last := len(q.Inputs) - 1
	f := q.Inputs[v.InIdx]
	cur := v.InVals[f.Key]
	switch key {
	case "enter":
		if !v.checkField(f, now) {
			return
		}
		if v.InIdx == last {
			submitInput(v, q, alive, now, act)
			return
		}
		v.moveField(v.InIdx + 1)
	case "tab", "right":
		if v.InIdx < last && v.checkField(f, now) {
			v.moveField(v.InIdx + 1)
		}
	case "shift-tab", "left":
		if v.InIdx > 0 {
			v.moveField(v.InIdx - 1)
		}
	case "backspace":
		v.InErr = ""
		if cur != "" {
			v.InVals[f.Key] = cur[:len(cur)-1]
			v.InFresh = false
		} else if v.InIdx > 0 {
			v.moveField(v.InIdx - 1)
			v.InFresh = false // 이어서 Backspace 로 앞 칸을 지울 수 있게
		}
	case "esc":
		v.InVals, v.InIdx, v.InFresh, v.InErr = endDefaults(q.Inputs, now), 0, true, ""
		v.Flash = "모든 칸을 기본값(지금 시각)으로 되돌렸습니다"
	default: // 숫자
		switch {
		case v.InFresh:
			cur = key
			v.InFresh = false
		case len(cur) < fieldWidth(f.Key):
			cur += key
		default:
			return // 칸이 꽉 참(마지막 칸)
		}
		v.InVals[f.Key] = cur
		v.InErr = ""
		if len(cur) == fieldWidth(f.Key) && v.InIdx < last && v.checkField(f, now) {
			v.moveField(v.InIdx + 1)
		}
	}
}

// checkField 는 지금 칸 값을 검사한다. 틀리면 오류를 보이고(글자는 그대로) false.
func (v *viewState) checkField(f inputField, now time.Time) bool {
	if err := checkEndField(f.Key, v.InVals[f.Key], now); err != nil {
		v.InErr = f.Label + ": " + err.Error()
		return false
	}
	return true
}

// moveField 는 칸 i 로 옮긴다(새 칸은 첫 숫자가 값을 대신한다).
func (v *viewState) moveField(i int) {
	v.InIdx, v.InFresh, v.InErr = i, true, ""
}

// submitInput 은 칸 값 전체를 검사해 c(계속)로 보낸다. 틀리면 값은 그대로 두고 고칠 칸으로 옮긴다
// (칸 오류 = 그 칸, 없는 날짜 = 일, 지금보다 앞 = 시).
func submitInput(v *viewState, q *question, alive bool, now time.Time, act tuiActions) {
	if !alive {
		v.Flash = "워커가 실행 중이 아니어서 답을 받을 수 없습니다"
		return
	}
	vals := map[string]string{}
	for _, f := range q.Inputs {
		vals[f.Key] = v.InVals[f.Key]
	}
	if err := checkEndAll(vals, now); err != nil {
		v.InErr = err.Error()
		v.InIdx, v.InFresh = fixField(q, vals, now), true
		return
	}
	if err := act.answer(q.ID, ansCont, vals); err != nil {
		v.Flash = "답 전송 실패: " + err.Error()
		return
	}
	v.Flash = "답을 보냈습니다: [" + ansCont + "] " + choiceLabel(q, ansCont)
	v.markSent(q.ID, ansCont, now)
}

// fixField 는 전체 검사에 걸린 값에서 고쳐야 할 칸 번호.
func fixField(q *question, vals map[string]string, now time.Time) int {
	idx := func(key string) int {
		for i, f := range q.Inputs {
			if f.Key == key {
				return i
			}
		}
		return 0
	}
	for i, f := range q.Inputs {
		if checkEndField(f.Key, vals[f.Key], now) != nil {
			return i
		}
	}
	if _, err := endTime(vals, now.Location()); err != nil {
		return idx("end_day")
	}
	return idx("end_hour")
}

// readKeys 는 f 에서 읽은 키를 ch 로 보낸다(읽기가 끝나면 "q").
// 이스케이프 열·UTF-8 글자가 읽기 경계에서 나뉘면 꼬리를 다음 읽기와 이어 붙이고, escWait 안에 안 오면 flushTail 로 낸다.
func readKeys(f *os.File, ch chan<- string) {
	type chunk struct {
		b   []byte
		err error
	}
	rc := make(chan chunk)
	go func() {
		for {
			buf := make([]byte, 256)
			n, err := f.Read(buf)
			rc <- chunk{buf[:n], err}
			if err != nil {
				return
			}
		}
	}()
	var pend []byte
	var wait <-chan time.Time
	for {
		select {
		case c := <-rc:
			done, tail := splitKeys(append(pend, c.b...))
			for _, k := range parseKeys(done) {
				ch <- k
			}
			pend = append([]byte(nil), tail...)
			if c.err != nil {
				for _, k := range flushTail(pend) {
					ch <- k
				}
				ch <- "q"
				return
			}
			wait = nil
			if len(pend) > 0 {
				wait = time.After(escWait)
			}
		case <-wait:
			for _, k := range flushTail(pend) {
				ch <- k
			}
			pend, wait = nil, nil
		}
	}
}

// stateCache 는 state.json 을 읽어 둔 결과. 수정 시각·크기가 그대로면 다시 읽어 파싱하지 않고 같은 포인터를 돌려준다
// (워커가 atomic rename 으로 쓰므로 수정 시각+크기면 바뀜을 알아보기에 충분하다). 대수가 많을 때 틱마다 드는 파싱 비용을 던다.
type stateCache struct {
	mtime time.Time
	size  int64
	st    *runState
}

// load 는 dir 의 상태를 돌려준다. 파일이 그대로면 이전에 파싱한 값을 그대로 쓰고, 읽기·파싱에 실패하면 캐시를 버리고 오류를 돌려준다.
func (c *stateCache) load(dir string) (*runState, error) {
	fi, err := os.Stat(filepath.Join(dir, stateFile))
	if err != nil {
		c.st = nil
		return nil, err
	}
	if c.st != nil && fi.Size() == c.size && fi.ModTime().Equal(c.mtime) {
		return c.st, nil
	}
	st, err := loadState(dir)
	if err != nil {
		c.st = nil
		return nil, err
	}
	c.mtime, c.size, c.st = fi.ModTime(), fi.Size(), st
	return st, nil
}

// drainKeys 는 first 와 채널에 이미 쌓여 있는 키(기다리지 않고)를 최대 max 개까지 순서대로 모아 돌려준다.
// 키를 누르고 있어 쌓인 만큼을 한 번에 처리하고 한 번만 그리기 위함이다.
func drainKeys(first string, ch <-chan string, max int) []string {
	ks := []string{first}
	for len(ks) < max {
		select {
		case k := <-ch:
			ks = append(ks, k)
		default:
			return ks
		}
	}
	return ks
}

// runTUI 는 dir 의 상태를 보여 주고 키를 처리한다. q·Ctrl-C·SIGTERM 으로 끝나며 터미널은 항상 되돌린다.
func runTUI(dir string) error {
	fd := int(os.Stdin.Fd())
	restore, err := enterRaw(fd)
	if err != nil {
		return err
	}
	out := bufio.NewWriter(os.Stdout)
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l") // 대체 화면, 커서 숨김
	out.Flush()
	defer func() {
		fmt.Fprint(out, "\x1b[?25h\x1b[?1049l")
		out.Flush()
		restore()
	}()

	keys := make(chan string, 32)
	go readKeys(os.Stdin, keys)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	winch := make(chan os.Signal, 1)
	notifyResize(winch)

	act := tuiActions{
		answer: func(id, ch string, vals map[string]string) error { return writeAnswerValues(dir, id, ch, vals) },
		stop:   func() error { return writeStopReq(dir) },
		report: newReportIO(dir),
	}
	view := viewState{NoColor: os.Getenv("NO_COLOR") != ""}
	var st *runState
	var cache stateCache
	var flashAt time.Time
	alive := false
	refresh := func() {
		if s, err := cache.load(dir); err == nil {
			st = s
		}
		alive = workerAlive(dir)
		view.sync(st, time.Now()) // 새 질문이면 입력 칸 기본값을 채우고, 답을 보낸 질문이 바뀌었으면 기록을 푼다
		if view.NoLog {
			view.LogLines = nil
		} else {
			view.LogLines = tailLines(filepath.Join(dir, workerLog), maxLogLines)
		}
	}
	draw := func() {
		w, h := termSize(fd)
		frame := renderRun(st, view, w, h, time.Now(), alive)
		fmt.Fprint(out, "\x1b[H"+strings.ReplaceAll(frame, "\n", "\x1b[K\r\n")+"\x1b[K\x1b[J")
		out.Flush()
	}
	refresh()
	draw()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-sig:
			return nil
		case <-winch:
			draw()
		case <-tick.C:
			refresh()
			if view.Flash != "" && time.Since(flashAt) > 4*time.Second {
				view.Flash = ""
			}
			draw()
		case k := <-keys:
			// 스크롤 같은 보기 키에서는 state.json 을 다시 읽지 않는다(1초 틱이 최신으로 유지). 쌓인 키는 모두 처리하고 한 번만 그린다.
			w, h := termSize(fd)
			logToggled, clear := false, false
			for _, kk := range drainKeys(k, keys, 256) {
				if handleKey(&view, st, alive, kk, w, h, time.Now(), act) {
					return nil
				}
				switch normKey(kk) {
				case "ctrl-l": // 화면을 지우고 처음부터 다시 그림
					clear = true
				case "l":
					logToggled = !logToggled
				}
			}
			if clear {
				fmt.Fprint(out, "\x1b[2J")
			}
			if logToggled { // 로그 창을 켠 직후 바로 내용이 보이게
				refresh()
			}
			if view.Flash != "" {
				flashAt = time.Now()
			}
			draw()
		}
	}
}

// logTailBytes 는 로그 끝에서 읽을 최대 바이트(줄 n 개를 얻기에 넉넉한 만큼만 읽는다).
const logTailBytes = 64 * 1024

// tailLines 는 path 의 마지막 n 줄을 돌려준다. 파일 끝쪽 logTailBytes 만 읽으므로 로그가 아무리 커도 가볍다.
// 파일이 없거나 읽을 수 없으면 nil. 중간에서 잘린 첫 줄은 버린다.
func tailLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	off := fi.Size() - logTailBytes
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(f, logTailBytes))
	if err != nil {
		return nil
	}
	return tailOf(string(b), n, off > 0)
}

// tailOf 는 s 의 마지막 n 줄. partial 이면 s 가 파일 중간에서 시작한 것이므로 첫 줄(잘렸을 수 있음)을 버린다.
func tailOf(s string, n int, partial bool) []string {
	lines := strings.Split(s, "\n")
	if partial && len(lines) > 0 {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimRight(lines[len(lines)-1], "\r") == "" { // 끝의 빈 줄(마지막 개행)
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, "\r")
	}
	return out
}

func hasChoice(q *question, key string) bool {
	for _, c := range q.Choices {
		if c.Key == key {
			return true
		}
	}
	return false
}
