package main

// -run TUI: raw tty 입력 처리와 화면 갱신 루프.
// 키 해석(parseKeys)과 키 처리(handleKey)는 tty 없이 시험할 수 있게 순수하게 둔다.
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
// 방향키 등은 "up" "down" "pgup" "pgdn" "home" "end", 단독 ESC 는 "esc", Ctrl-C 는 "ctrl-c", 엔터는 "enter",
// 그 밖에는 그 글자 그대로. 모르는 이스케이프 열은 버린다.
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

// tuiActions 는 키 처리가 밖에 내는 동작(시험에서는 가짜를 넣는다).
type tuiActions struct {
	answer func(id, choice string, values map[string]string) error // answer.json 쓰기(values 는 입력 질문의 값, 없으면 nil)
	stop   func() error                                            // stop.req 쓰기
}

// handleKey 는 키 하나를 처리해 view 를 바꾼다. quit 이 true 면 TUI 를 끝낸다(작업은 계속).
// st 는 마지막으로 읽은 상태(nil 가능), alive 는 워커 생존 여부, w·h 는 화면 크기(스크롤 단위 계산용).
func handleKey(v *viewState, st *runState, alive bool, key string, w, h int, now time.Time, act tuiActions) (quit bool) {
	v.Flash = ""
	if v.InputQ != "" { // 값 입력 중(줄 입력 모드)
		return handleInputKey(v, st, key, now)
	}
	if v.Help { // 도움말은 아무 키나 누르면 닫힘
		v.Help = false
		return false
	}
	if v.Confirm != "" { // y/N 확인 중
		kind, qid := v.Confirm, v.ConfirmQ
		v.Confirm, v.ConfirmQ = "", ""
		if key != "y" && key != "Y" {
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
	if q := st.Question; q != nil {
		for _, c := range q.Choices {
			if c.Key != key {
				continue
			}
			if !alive {
				v.Flash = "워커가 실행 중이 아니어서 답을 받을 수 없습니다"
				return false
			}
			if key == ansStop { // 중지는 y/N 확인 뒤에 보낸다
				v.Confirm, v.ConfirmQ = "s", q.ID
				return false
			}
			var vals map[string]string
			if len(q.Inputs) > 0 && key == ansCont { // 입력 질문은 값을 다 입력한 뒤에만 제출
				if v.ReadyQ != q.ID {
					v.Flash = "먼저 값을 입력하세요(i 또는 Enter)"
					return false
				}
				vals = v.ReadyVals
				v.ReadyQ, v.ReadyVals = "", nil
				if err := checkEndAll(vals, now); err != nil {
					v.Flash = "입력값 오류: " + err.Error() + " (i 로 다시 입력)"
					return false
				}
			}
			if err := act.answer(q.ID, key, vals); err != nil {
				v.Flash = "답 전송 실패: " + err.Error()
			} else {
				v.Flash = "답을 보냈습니다: [" + key + "] " + c.Label
			}
			return false
		}
		switch {
		case (key == "i" || key == "enter") && len(q.Inputs) > 0:
			if !alive {
				v.Flash = "워커가 실행 중이 아니어서 답을 받을 수 없습니다"
			} else {
				v.InputQ, v.InputIdx, v.InputBuf, v.InputErr, v.InputVals = q.ID, 0, "", "", map[string]string{}
				v.ReadyQ, v.ReadyVals = "", nil
			}
		case key == "S":
			switch {
			case hasChoice(q, ansStop):
				v.Flash = "질문에 답하세요(중지는 s)"
			case st.Finished():
				v.Flash = "이미 끝난 작업입니다"
			case !alive:
				v.Flash = "워커가 실행 중이 아닙니다"
			default: // 인벤토리 사용 중 질문처럼 s 선택지가 없으면 S 로 작업 전체를 중지
				v.Confirm = "S"
			}
		}
		return false
	}
	if key == "S" {
		switch {
		case st.Finished():
			v.Flash = "이미 끝난 작업입니다"
		case !alive:
			v.Flash = "워커가 실행 중이 아닙니다"
		default:
			v.Confirm = "S"
		}
	}
	return false
}

// readKeys 는 f 에서 읽은 키를 ch 로 보낸다(읽기가 끝나면 "q").
func readKeys(f *os.File, ch chan<- string) {
	buf := make([]byte, 64)
	for {
		n, err := f.Read(buf)
		for _, k := range parseKeys(buf[:n]) {
			ch <- k
		}
		if err != nil {
			ch <- "q"
			return
		}
	}
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
	}
	view := viewState{NoColor: os.Getenv("NO_COLOR") != ""}
	var st *runState
	var flashAt time.Time
	alive := false
	refresh := func() {
		if s, err := loadState(dir); err == nil {
			st = s
		}
		alive = workerAlive(dir)
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
			refresh()
			w, h := termSize(fd)
			if handleKey(&view, st, alive, k, w, h, time.Now(), act) {
				return nil
			}
			if k == "l" { // 로그 창을 켠 직후 바로 내용이 보이게
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

// clearInput 은 값 입력 모드를 끝낸다(확정한 값 ReadyVals 는 건드리지 않는다).
func (v *viewState) clearInput() {
	v.InputQ, v.InputIdx, v.InputBuf, v.InputErr, v.InputVals = "", 0, "", "", nil
}

// handleInputKey 는 값 입력 모드(InputQ != "")의 키 처리: 숫자 입력, Backspace 지우기, Enter 다음 항목, Esc 취소.
// 항목마다 checkEndField 로 검사해 틀리면 오류를 보이고 같은 항목을 다시 묻는다. 마지막 항목 뒤에는 합친 시각도 검사해
// 틀리면 처음부터 다시 묻고, 통과하면 입력 모드를 끝내고 ReadyQ/ReadyVals 에 두어 c 로 제출할 수 있게 한다.
func handleInputKey(v *viewState, st *runState, key string, now time.Time) (quit bool) {
	var q *question
	if st != nil {
		q = st.Question
	}
	if key == "ctrl-c" {
		return true
	}
	if q == nil || q.ID != v.InputQ || v.InputIdx >= len(q.Inputs) {
		v.clearInput()
		v.Flash = "질문이 바뀌어 입력을 취소했습니다"
		return false
	}
	f := q.Inputs[v.InputIdx]
	switch key {
	case "esc":
		v.clearInput()
		v.Flash = "입력을 취소했습니다(i 또는 Enter 로 다시 입력)"
	case "backspace":
		if r := []rune(v.InputBuf); len(r) > 0 {
			v.InputBuf = string(r[:len(r)-1])
		}
	case "enter":
		if err := checkEndField(f.Key, v.InputBuf, now); err != nil {
			v.InputErr = f.Key + ": " + err.Error()
			v.InputBuf = ""
			return false
		}
		v.InputVals[f.Key] = v.InputBuf
		v.InputBuf, v.InputErr = "", ""
		v.InputIdx++
		if v.InputIdx < len(q.Inputs) {
			return false
		}
		if err := checkEndAll(v.InputVals, now); err != nil {
			v.InputErr = err.Error() + " — 처음부터 다시 입력하세요"
			v.InputIdx, v.InputVals = 0, map[string]string{}
			return false
		}
		v.ReadyQ, v.ReadyVals = q.ID, v.InputVals
		v.clearInput()
		v.Flash = "입력을 마쳤습니다. c 로 이 값으로 계속, i 로 다시 입력, s 로 중지"
	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' && len(v.InputBuf) < 4 {
			v.InputBuf += key
		}
	}
	return false
}
