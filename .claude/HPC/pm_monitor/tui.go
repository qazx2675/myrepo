// tui.go - TUI: 키 파싱, 키 처리 상태기계(handleKey), 이벤트 루프, raw tty 진입·복구.
// auto_setup 의 tui.go 구조(tuiEnv 주입, 키 읽기 고루틴, 한 번의 write)를 따라 pm_monitor 에 맞게 줄였다.
// 키 입력은 수집과 상관없이 즉시 반응한다: 수집은 src.Run 고루틴이, c 키의 ping 확인은 별도 고루틴이 한다.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// ---- 키 ----

type keyKind int

const (
	kUp keyKind = iota + 1
	kDown
	kPgUp
	kPgDn
	kHome
	kEnd
	kEnter
	kEsc
	kBackspace
	kRune
	kEOF
)

// Key 는 키 입력 하나다. 글자 키는 Kind 가 kRune 이고 R 에 글자가 들어 있다(Ctrl+C 는 R=3).
type Key struct {
	Kind keyKind
	R    rune
}

const runeCtrlC = 3

// parseKeys: 터미널에서 한 번에 읽은 바이트를 키 이벤트로 (ESC [ A / ESC O A 방향키, PgUp/PgDn/Home/End, 단독 ESC, CR/LF, 일반 문자)
func parseKeys(b []byte) []Key {
	var out []Key
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			if i+1 < len(b) && (b[i+1] == '[' || b[i+1] == 'O') {
				j := i + 2
				for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
					j++
				}
				if j >= len(b) { // 끝이 잘린 시퀀스는 버림
					return out
				}
				switch b[j] {
				case '~':
					switch string(b[i+2 : j]) {
					case "1", "7":
						out = append(out, Key{Kind: kHome})
					case "4", "8":
						out = append(out, Key{Kind: kEnd})
					case "5":
						out = append(out, Key{Kind: kPgUp})
					case "6":
						out = append(out, Key{Kind: kPgDn})
					}
				case 'A':
					out = append(out, Key{Kind: kUp})
				case 'B':
					out = append(out, Key{Kind: kDown})
				case 'H':
					out = append(out, Key{Kind: kHome})
				case 'F':
					out = append(out, Key{Kind: kEnd})
				}
				i = j + 1
				continue
			}
			out = append(out, Key{Kind: kEsc})
			i++
		case c == '\r' || c == '\n':
			out = append(out, Key{Kind: kEnter})
			i++
			if c == '\r' && i < len(b) && b[i] == '\n' {
				i++
			}
		case c == runeCtrlC:
			out = append(out, Key{Kind: kRune, R: rune(c)})
			i++
		case c == 0x7f || c == 0x08:
			out = append(out, Key{Kind: kBackspace})
			i++
		case c >= 0x20 && c < 0x7f:
			out = append(out, Key{Kind: kRune, R: rune(c)})
			i++
		case c >= 0x80:
			r, n := utf8.DecodeRune(b[i:])
			out = append(out, Key{Kind: kRune, R: r})
			i += n
		default:
			i++
		}
	}
	return out
}

// ---- 키 처리(순수 상태기계) ----

type action int

const (
	actNone   action = iota
	actQuit          // 종료
	actCheck         // c 패널을 열었다: 완료 서버 ping 확인을 시작해야 한다
	actReport        // d·l 패널을 열었다: 조회를 시작해야 한다(ViewState.reportReq 가 패널)
)

// scrollTarget 은 지금 스크롤되는 대상(열린 패널 또는 표)의 위치·전체 줄 수·한 쪽 줄 수다.
func (v *ViewState) scrollTarget(snap Snapshot) (cur *int, total, page int) {
	w, h := normSize(v.W, v.H)
	v.prepare(snap, w)
	body := v.bodySize(snap, w, h)
	if v.Panel != 0 {
		return &v.PanelScroll, len(v.pLines), maxInt(body-1, 1)
	}
	return &v.Scroll, len(v.rows), maxInt(body-2, 1)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (v *ViewState) scroll(k Key, snap Snapshot) {
	cur, total, page := v.scrollTarget(snap)
	top := maxInt(total-page, 0)
	p := clampInt(*cur, 0, top)
	switch k.Kind {
	case kUp:
		p--
	case kDown:
		p++
	case kPgUp:
		p -= page
	case kPgDn:
		p += page
	case kHome:
		p = 0
	case kEnd:
		p = top
	}
	*cur = clampInt(p, 0, top)
}

// handleKey 는 키 하나를 처리한다. 종료·c 확인 시작이 필요하면 그 동작을 돌려준다.
func (v *ViewState) handleKey(k Key, snap Snapshot) action {
	if k.Kind == kEOF || (k.Kind == kRune && k.R == runeCtrlC) {
		return actQuit
	}
	if v.Searching {
		v.searchKey(k)
		return actNone
	}
	switch k.Kind {
	case kUp, kDown, kPgUp, kPgDn, kHome, kEnd:
		v.scroll(k, snap)
	case kEsc:
		v.Panel = 0
	case kRune:
		return v.runeKey(k.R)
	}
	return actNone
}

func (v *ViewState) runeKey(r rune) action {
	switch {
	case r == 'q':
		if v.Panel != 0 { // 패널에서는 뒤로가기(기본 화면), 기본 화면에서는 종료
			v.Panel = 0
			return actNone
		}
		return actQuit
	case r == panelInstall || r == panelDelay || r == panelDHCP || r == panelLDAP:
		if v.Panel == r { // 같은 키를 다시 누르면 닫는다
			v.Panel = 0
			return actNone
		}
		v.Panel, v.PanelScroll, v.pOK = r, 0, false
		if (r == panelDHCP || r == panelLDAP) && !v.reportBusy[r] { // 조회는 패널마다 하나만
			v.reportReq = r
			return actReport
		}
		if r == panelInstall && !v.Busy {
			v.Busy, v.CheckErr = true, ""
			return actCheck
		}
	case r == '/':
		v.Panel, v.Searching, v.SearchBuf = 0, true, v.Search
	case r >= '0' && r <= '7':
		v.Panel, v.Filter, v.Scroll = 0, Status(r-'0'), 0
		v.invalidate()
	}
	return actNone
}

func (v *ViewState) searchKey(k Key) {
	switch k.Kind {
	case kEnter:
		v.Search, v.Searching, v.Scroll = strings.TrimSpace(v.SearchBuf), false, 0
		v.invalidate()
	case kEsc:
		v.Searching = false
	case kBackspace:
		if n := len(v.SearchBuf); n > 0 {
			_, sz := utf8.DecodeLastRuneInString(v.SearchBuf)
			v.SearchBuf = v.SearchBuf[:n-sz]
		}
	case kRune:
		if k.R >= 0x20 && k.R != 0x7f {
			v.SearchBuf += string(k.R)
		}
	}
}

// checkDone 은 c 의 ping 확인이 끝났을 때 부른다. 패널은 새 스냅샷으로 다시 만든다.
func (v *ViewState) checkDone(err error) {
	v.Busy = false
	if err != nil {
		v.CheckErr = err.Error()
	}
	v.invalidate()
}

// reportSource 는 d·l 패널 조회를 하는 source 의 선택 기능이다(*Collector 가 구현한다).
// Report 는 오래 걸릴 수 있어 TUI 가 별도 고루틴에서 부른다.
type reportSource interface {
	Report(kind rune) string     // 조회를 실행하고 패널 본문을 돌려준다
	ReportBusy(kind rune) string // 조회 중에 보일 안내
}

type reportRes struct {
	kind rune
	text string
}

// setReport 는 d·l 패널 본문을 바꾼다. busy 는 조회 진행 여부다.
func (v *ViewState) setReport(kind rune, text string, busy bool) {
	if v.Reports == nil {
		v.Reports = map[rune]string{}
		v.reportBusy = map[rune]bool{}
	}
	v.Reports[kind] = text
	v.reportBusy[kind] = busy
	v.invalidate()
}

// ---- 환경 주입 ----

// tuiEnv: 테스트에서 가짜로 바꿀 수 있는 외부 의존
type tuiEnv struct {
	In      io.Reader
	Out     io.Writer
	Size    func() (w, h int)
	MakeRaw func() (restore func(), err error)
	Tick    func(d time.Duration) (c <-chan time.Time, stop func())
	Now     func() time.Time
	Signals <-chan os.Signal // 종료 신호(SIGTERM·SIGHUP·SIGINT). 없으면 nil
	Resize  <-chan os.Signal // 창 크기 변경(SIGWINCH). 없으면 nil
	Color   bool
	// InReady: d 안에 In 에서 읽을 입력이 있으면 true (nil = 항상 바로 Read). 키 읽기 고루틴이 종료 때 멈출 수 있게 poll 방식으로 쓴다.
	InReady func(d time.Duration) bool
}

const (
	enterScreen = "\x1b[?1049h\x1b[?25l" // 대체 화면 + 커서 숨김
	leaveScreen = "\x1b[?25h\x1b[?1049l"
	inPoll      = 200 * time.Millisecond
)

// realTUIEnv: 실제 tty 용 환경. cleanup 은 시그널 설정을 되돌린다.
func realTUIEnv(in, out *os.File) (tuiEnv, func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	unsuspend := ignoreSuspend() // Ctrl+Z 로 TUI 가 멈추지 않게
	winch := make(chan os.Signal, 1)
	notifyResize(winch)
	cleanup := func() {
		signal.Stop(winch)
		unsuspend()
		signal.Stop(sig)
	}
	env := tuiEnv{
		In:      in,
		Out:     out,
		Size:    func() (int, int) { return ttySize(out) },
		MakeRaw: func() (func(), error) { return makeRaw(in) },
		Tick: func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		},
		Now:     time.Now,
		Signals: sig,
		Resize:  winch,
		Color:   os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb",
		InReady: func(d time.Duration) bool { return waitReadable(in, d) },
	}
	return env, cleanup
}

// source 는 TUI 가 쓰는 수집기 쪽 동작이다(*Collector 가 구현한다).
type source interface {
	Snapshot() *Snapshot
	Updates() <-chan struct{}
	CheckCompleted(ctx context.Context) error
	Run(ctx context.Context)
}

// runTUI: tty 가 아니면 안내하고 1 로 끝낸다. 터미널은 어떤 경우에도(정상·신호·패닉) 복구한다.
func runTUI(src source, cfg *Config) int {
	in, out := os.Stdin, os.Stdout
	if !isTTY(in) || !isTTY(out) {
		fmt.Fprintln(os.Stderr, "install_mn 은 터미널(tty)에서 실행해야 합니다.")
		return 1
	}
	env, cleanup := realTUIEnv(in, out)
	defer cleanup()
	return runTUILoop(src, env, cfg.Warning, nil)
}

// ---- 루프 ----

// runTUILoop 는 화면을 그리고 키·스냅샷·tick·신호를 처리한다. panelText 는 패널 본문 제공자(nil 이면 기본).
func runTUILoop(src source, env tuiEnv, warning string, panelText func(kind rune, snap Snapshot) string) int {
	restore, err := env.MakeRaw()
	if err != nil {
		fmt.Fprintln(os.Stderr, "터미널을 raw 모드로 바꾸지 못했습니다:", err)
		return 1
	}
	io.WriteString(env.Out, enterScreen)
	var once sync.Once
	restoreTerm := func() { // 여러 번 불려도 한 번만
		once.Do(func() {
			io.WriteString(env.Out, leaveScreen)
			restore()
		})
	}
	defer restoreTerm() // 정상 종료와 메인 고루틴 패닉
	protect := func() { // 다른 고루틴의 패닉: 터미널부터 복구한 뒤 그대로 다시 패닉
		if r := recover(); r != nil {
			restoreTerm()
			panic(r)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer protect()
		src.Run(ctx)
	}()

	keys := make(chan Key, 64)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer protect()
		buf := make([]byte, 256)
		for {
			select {
			case <-done:
				return
			default:
			}
			if env.InReady != nil && !env.InReady(inPoll) {
				continue
			}
			n, err := env.In.Read(buf)
			for _, k := range parseKeys(buf[:n]) {
				select {
				case keys <- k:
				case <-done:
					return
				}
			}
			if err != nil {
				select {
				case keys <- Key{Kind: kEOF}:
				case <-done:
				}
				return
			}
		}
	}()

	v := &ViewState{Warning: warning, Color: env.Color, PanelText: panelText}
	snap := *src.Snapshot()
	checkDone := make(chan error, 1)
	reportCh := make(chan reportRes, 2) // d·l 조회 결과(패널마다 하나씩만 진행)
	var lastFrame string
	var lastW, lastH int
	draw := func(force bool) {
		w, h := env.Size()
		w, h = normSize(w, h)
		if w != lastW || h != lastH {
			force = true
		}
		v.W, v.H, v.Now = w, h, env.Now()
		v.prepare(snap, w)
		frame := render(*v, snap, w, h)
		if !force && frame == lastFrame {
			return // 바뀐 것이 없으면 쓰지 않는다
		}
		out := "\x1b[H" + strings.ReplaceAll(frame, "\n", "\x1b[K\r\n") + "\x1b[K\x1b[J"
		if force {
			out = "\x1b[2J" + out
		}
		io.WriteString(env.Out, out) // 한 번의 write
		lastFrame, lastW, lastH = frame, w, h
	}

	tick, stopTick := env.Tick(time.Second)
	defer stopTick()
	draw(true)
	for {
		force := false
		select {
		case k := <-keys:
			handle := func(k Key) bool {
				switch v.handleKey(k, snap) {
				case actQuit:
					return true
				case actCheck:
					go func() {
						defer protect()
						checkDone <- src.CheckCompleted(ctx)
					}()
				case actReport:
					kind := v.reportReq
					v.reportReq = 0
					if rs, ok := src.(reportSource); ok {
						v.setReport(kind, rs.ReportBusy(kind), true)
						go func() {
							defer protect()
							reportCh <- reportRes{kind, rs.Report(kind)}
						}()
					}
				}
				return false
			}
			if handle(k) {
				return 0
			}
			for more := true; more; { // 쌓인 키는 모두 처리한 뒤 한 번만 그린다
				select {
				case k := <-keys:
					if handle(k) {
						return 0
					}
				default:
					more = false
				}
			}
		case <-src.Updates():
			snap = *src.Snapshot()
			v.invalidate()
		case rr := <-reportCh:
			snap = *src.Snapshot() // l 점검이 저장한 결과가 스냅샷에 반영돼 있다
			v.setReport(rr.kind, rr.text, false)
		case err := <-checkDone:
			snap = *src.Snapshot()
			v.checkDone(err)
		case <-tick: // 시계 글자만 바뀐다
		case <-env.Resize:
			force = true
		case <-env.Signals:
			return 0
		}
		draw(force)
	}
}
