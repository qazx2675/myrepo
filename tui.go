package main

// 터미널 UI: 선택 목록, 기록 표, 업로드 진행 줄. ANSI 와 stty(raw 모드)만 쓴다.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// stdinR 는 표준 입력 공용 버퍼다 (여러 곳에서 따로 감싸면 읽은 바이트가 사라진다).
var stdinR = bufio.NewReader(os.Stdin)

var errTUIUnavailable = errors.New("터미널 UI 를 쓸 수 없습니다")

const (
	keyNone = iota
	keyUp
	keyDown
	keyPgUp
	keyPgDn
	keyHome
	keyEnd
	keyEnter
	keyQuit
	keyDigit
)

// readKey 는 키 하나를 읽는다. 숫자면 두 번째 값이 그 숫자다.
func readKey(r *bufio.Reader) (int, int, error) {
	b, err := r.ReadByte()
	if err != nil {
		return keyQuit, 0, err
	}
	switch {
	case b == '\r' || b == '\n':
		return keyEnter, 0, nil
	case b == 'q' || b == 'Q' || b == 3 || b == 4: // Ctrl+C, Ctrl+D
		return keyQuit, 0, nil
	case b == 'k':
		return keyUp, 0, nil
	case b == 'j':
		return keyDown, 0, nil
	case b >= '1' && b <= '9':
		return keyDigit, int(b - '0'), nil
	case b == 0x1b:
		b2, err := r.ReadByte()
		if err != nil || (b2 != '[' && b2 != 'O') {
			return keyNone, 0, err
		}
		b3, err := r.ReadByte()
		if err != nil {
			return keyNone, 0, err
		}
		switch b3 {
		case 'A':
			return keyUp, 0, nil
		case 'B':
			return keyDown, 0, nil
		case 'H':
			return keyHome, 0, nil
		case 'F':
			return keyEnd, 0, nil
		case '1', '4', '5', '6', '7', '8':
			_, _ = r.ReadByte() // '~'
			switch b3 {
			case '5':
				return keyPgUp, 0, nil
			case '6':
				return keyPgDn, 0, nil
			case '1', '7':
				return keyHome, 0, nil
			default:
				return keyEnd, 0, nil
			}
		}
	}
	return keyNone, 0, nil
}

// dispWidth 는 터미널 표시 폭이다 (한글/한자/전각 2칸).
func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func runeWidth(r rune) int {
	switch {
	case r < 0x20:
		return 0
	case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6, r >= 0x1f300 && r <= 0x1faff, r >= 0x20000 && r <= 0x3fffd:
		return 2
	}
	return 1
}

// fit 은 표시 폭 w 에 맞춰 자르거나 공백으로 채운다. 제어문자는 공백으로 바꾼다.
func fit(s string, w int) string {
	var sb strings.Builder
	used := 0
	for _, r := range s {
		if r < 0x20 || r == 0x7f || !utf8.ValidRune(r) {
			r = ' '
		}
		rw := runeWidth(r)
		if used+rw > w {
			break
		}
		sb.WriteRune(r)
		used += rw
	}
	for ; used < w; used++ {
		sb.WriteByte(' ')
	}
	return sb.String()
}

// withRaw 는 raw 모드와 대체 화면에서 f 를 실행한다.
func withRaw(out io.Writer, f func() error) error {
	restore, err := termRaw()
	if err != nil {
		return errTUIUnavailable
	}
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l")
	defer func() {
		fmt.Fprint(out, "\x1b[?25h\x1b[?1049l")
		restore()
	}()
	return f()
}

// ttyChoose 는 화살표로 고르는 목록이다. 취소하면 -1.
func ttyChoose(r *bufio.Reader, out io.Writer, title string, items []string) (int, error) {
	sel := -1
	err := withRaw(out, func() error {
		rows, cols := termSize()
		var e error
		sel, e = chooseLoop(r, out, title, items, rows, cols)
		return e
	})
	if err == errTUIUnavailable {
		return -1, err
	}
	return sel, err
}

// chooseLoop 는 키 입력으로 항목을 고른다 (테스트에서 직접 부른다).
func chooseLoop(r *bufio.Reader, out io.Writer, title string, items []string, rows, cols int) (int, error) {
	cur, top := 0, 0
	h := rows - 4
	if h < 1 {
		h = 1
	}
	for {
		if cur < top {
			top = cur
		}
		if cur >= top+h {
			top = cur - h + 1
		}
		var sb strings.Builder
		sb.WriteString("\x1b[H\x1b[2J")
		sb.WriteString(fit(title, cols-1) + "\r\n\r\n")
		for i := top; i < len(items) && i < top+h; i++ {
			line := fit(fmt.Sprintf("%2d) %s", i+1, items[i]), cols-3)
			if i == cur {
				sb.WriteString("\x1b[7m> " + line + "\x1b[0m\r\n")
			} else {
				sb.WriteString("  " + line + "\r\n")
			}
		}
		sb.WriteString("\r\n" + fit("위/아래: 이동  Enter: 선택  숫자: 바로 선택  q: 취소", cols-1))
		fmt.Fprint(out, sb.String())
		k, d, err := readKey(r)
		if err != nil {
			return -1, nil
		}
		switch k {
		case keyUp:
			if cur > 0 {
				cur--
			}
		case keyDown:
			if cur < len(items)-1 {
				cur++
			}
		case keyPgUp:
			cur -= h
			if cur < 0 {
				cur = 0
			}
		case keyPgDn:
			cur += h
			if cur > len(items)-1 {
				cur = len(items) - 1
			}
		case keyHome:
			cur = 0
		case keyEnd:
			cur = len(items) - 1
		case keyEnter:
			return cur, nil
		case keyDigit:
			if d <= len(items) {
				return d - 1, nil
			}
		case keyQuit:
			return -1, nil
		}
	}
}

// showTable 은 표를 보여 준다. 터미널이면 스크롤 TUI, 아니면 그대로 출력한다.
func showTable(out io.Writer, tty bool, title, header string, rows []string) {
	if tty {
		err := withRaw(out, func() error {
			h, cols := termSize()
			tableLoop(stdinR, out, title, header, rows, h, cols)
			return nil
		})
		if err == nil {
			return
		}
	}
	fmt.Fprintln(out, title)
	fmt.Fprintln(out, header)
	for _, r := range rows {
		fmt.Fprintln(out, r)
	}
}

// tableLoop 은 스크롤 표다. 위/아래, PgUp/PgDn, Home/End, q.
func tableLoop(r *bufio.Reader, out io.Writer, title, header string, rows []string, height, cols int) {
	top := 0
	h := height - 5
	if h < 1 {
		h = 1
	}
	maxTop := len(rows) - h
	if maxTop < 0 {
		maxTop = 0
	}
	for {
		var sb strings.Builder
		sb.WriteString("\x1b[H\x1b[2J")
		sb.WriteString(fit(title, cols-1) + "\r\n")
		sb.WriteString("\x1b[1m" + fit(header, cols-1) + "\x1b[0m\r\n")
		if len(rows) == 0 {
			sb.WriteString("(기록 없음)\r\n")
		}
		for i := top; i < len(rows) && i < top+h; i++ {
			sb.WriteString(fit(rows[i], cols-1) + "\r\n")
		}
		end := top + h
		if end > len(rows) {
			end = len(rows)
		}
		sb.WriteString("\r\n" + fit(fmt.Sprintf("%d-%d / %d건   위/아래 PgUp/PgDn Home/End 이동, q 닫기", minInt(top+1, end), end, len(rows)), cols-1))
		fmt.Fprint(out, sb.String())
		k, _, err := readKey(r)
		if err != nil {
			return
		}
		switch k {
		case keyUp:
			top--
		case keyDown:
			top++
		case keyPgUp:
			top -= h
		case keyPgDn:
			top += h
		case keyHome:
			top = 0
		case keyEnd:
			top = maxTop
		case keyQuit, keyEnter:
			return
		}
		if top < 0 {
			top = 0
		}
		if top > maxTop {
			top = maxTop
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// progressSink 는 업로드 진행 알림이다.
type progressSink interface {
	Begin(n int, total int64)
	Update(k, n int, done int64, status string)
}

// ttyProgress 는 한 줄 진행 막대다. 로그 줄은 그 위에 찍고 막대를 다시 그린다.
type ttyProgress struct {
	mu     sync.Mutex
	w      io.Writer
	name   string
	k, n   int
	done   int64
	total  int64
	status string
	start  time.Time
	stop   chan struct{}
	wg     sync.WaitGroup
}

func newTTYProgress(w io.Writer, name string) *ttyProgress {
	return &ttyProgress{w: w, name: name, start: time.Now(), status: "준비"}
}

func (p *ttyProgress) Begin(n int, total int64) {
	p.mu.Lock()
	p.n, p.total, p.start = n, total, time.Now()
	p.stop = make(chan struct{})
	p.draw()
	p.mu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-t.C:
				p.mu.Lock()
				p.draw()
				p.mu.Unlock()
			}
		}
	}()
}

func (p *ttyProgress) Update(k, n int, done int64, status string) {
	p.mu.Lock()
	p.k, p.n, p.done, p.status = k, n, done, status
	p.draw()
	p.mu.Unlock()
}

// Write 는 로그 줄을 막대 위에 찍는다.
func (p *ttyProgress) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprint(p.w, "\r\x1b[2K")
	n, err := p.w.Write(b)
	if p.stop != nil {
		p.draw()
	}
	return n, err
}

// End 는 갱신을 멈추고 마지막 막대를 남긴다.
func (p *ttyProgress) End() {
	p.mu.Lock()
	if p.stop != nil {
		close(p.stop)
	}
	p.mu.Unlock()
	p.wg.Wait()
	p.mu.Lock()
	if p.stop != nil {
		p.draw()
		fmt.Fprint(p.w, "\n")
	}
	p.stop = nil
	p.mu.Unlock()
}

func (p *ttyProgress) line() string {
	el := time.Since(p.start).Seconds()
	pct := 100.0
	if p.total > 0 {
		pct = float64(p.done) * 100 / float64(p.total)
	}
	const bw = 16
	fill := int(pct / 100 * bw)
	if fill > bw {
		fill = bw
	}
	bar := strings.Repeat("#", fill) + strings.Repeat("-", bw-fill)
	speed := float64(p.done) / 1024 / maxf(el, 0.001)
	return fmt.Sprintf("[%d/%d] %s [%s] %5.1f%% %.1f초 %.1f KiB/s %s", p.k, p.n, p.status, bar, pct, el, speed, p.name)
}

func (p *ttyProgress) draw() {
	_, cols := termSizeCached()
	fmt.Fprint(p.w, "\r\x1b[2K"+fit(p.line(), cols-1))
}

var (
	tsOnce        sync.Once
	tsRows, tsCol int
)

// termSizeCached 는 진행 막대용 터미널 크기다 (stty 를 매번 부르지 않는다).
func termSizeCached() (int, int) {
	tsOnce.Do(func() { tsRows, tsCol = termSize() })
	return tsRows, tsCol
}
