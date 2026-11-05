package main

// -run TUI: raw tty 입력 처리와 화면 갱신 루프.
// 키 해석(parseKeys)과 키 처리(handleKey)는 tty 없이 시험할 수 있게 순수하게 둔다.
// 화면은 1초마다 state.json 을 다시 읽어 그리고, 키를 누르면 바로 다시 그린다.

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
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
	answer func(id, choice string) error // answer.json 쓰기
	stop   func() error                  // stop.req 쓰기
}

// handleKey 는 키 하나를 처리해 view 를 바꾼다. quit 이 true 면 TUI 를 끝낸다(작업은 계속).
// st 는 마지막으로 읽은 상태(nil 가능), alive 는 워커 생존 여부, w·h 는 화면 크기(스크롤 단위 계산용).
func handleKey(v *viewState, st *runState, alive bool, key string, w, h int, now time.Time, act tuiActions) (quit bool) {
	v.Flash = ""
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
			err = act.answer(qid, ansStop)
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
			if err := act.answer(q.ID, key); err != nil {
				v.Flash = "답 전송 실패: " + err.Error()
			} else {
				v.Flash = "답을 보냈습니다: [" + key + "] " + c.Label
			}
			return false
		}
		if key == "S" {
			v.Flash = "질문에 답하세요(중지는 s)"
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
		answer: func(id, ch string) error { return writeAnswer(dir, id, ch) },
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
			if view.Flash != "" {
				flashAt = time.Now()
			}
			draw()
		}
	}
}
