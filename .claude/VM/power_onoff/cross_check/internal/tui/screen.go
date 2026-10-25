package tui

// screen.go - 전체 화면 TUI 루프(auto_setup tui.go 의 runTUILoopWith 와 같은 구조):
// raw 모드 → 대체 화면·커서 숨김·붙여넣기 구간 표시(bracketed paste) 켜기 → 키 읽기 goroutine + 하나의 select 루프
// (키 / 2초 자동 갱신 / 창 크기 변경 / 종료 시그널) → 이벤트마다 프레임 전체를 한 번의 write 로 다시 그림 →
// 정상·패닉·시그널 어느 경로든 defer 로 화면과 termios 를 되돌린다. 입출력·시계·tick 은 Env 로 주입(테스트에서 가짜).

import (
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Env 는 전체 화면 루프의 외부 의존(테스트에서 가짜로 바꾼다).
type Env struct {
	In      io.Reader
	Out     io.Writer
	Size    func() (w, h int)
	MakeRaw func() (restore func(), err error)
	Tick    func(d time.Duration) (c <-chan time.Time, stop func())
	Now     func() time.Time
	Loc     *time.Location   // 시각 표시 시간대(nil = time.Local)
	Signals <-chan os.Signal // 종료 시그널(SIGTERM·SIGHUP·SIGINT). 없으면 nil
	Resize  <-chan os.Signal // 창 크기 변경(SIGWINCH) — 오면 화면을 지우고 바로 다시 그린다. 없으면 nil
	Color   bool
	// InReady: d 안에 In 에서 읽을 입력이 있으면 true (nil = 항상 바로 Read). 실제 tty 에서는 100ms poll 로
	// 단독 ESC(Esc 키)와 ESC 로 시작하는 시퀀스를 구분한다(조용하면 들고 있던 단독 ESC 를 Esc 로 내보냄).
	InReady func(d time.Duration) bool
}

const (
	// enterScreen: 대체 화면 + 커서 숨김 + 붙여넣기 구간 표시(ESC[200~ … ESC[201~) 켜기
	enterScreen = "\x1b[?1049h\x1b[?25l\x1b[?2004h"
	// leaveScreen: 위를 모두 되돌림
	leaveScreen = "\x1b[?2004l\x1b[?25h\x1b[?1049l"

	refreshEvery = 2 * time.Second        // 자동 갱신 주기
	inPoll       = 100 * time.Millisecond // 키 읽기 poll (단독 ESC 판정 시간)
)

// FullScreenAvailable 은 in·out 이 모두 터미널이고 이 빌드가 전체 화면을 지원하는지(linux 만).
// 아니면(파이프·테스트·e2e·비 linux) 줄 입력 화면(Session)을 쓴다.
func FullScreenAvailable(in, out *os.File) bool {
	return fullScreenOS && isTTY(in) && isTTY(out)
}

// RealEnv 는 실제 tty 용 환경(시그널·창 크기·poll). cleanup 은 시그널 설정을 되돌린다.
func RealEnv(in, out *os.File, color bool) (Env, func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	ignoreStop()
	winch := make(chan os.Signal, 1)
	notifyResize(winch)
	cleanup := func() {
		signal.Stop(winch)
		resetStop()
		signal.Stop(sig)
	}
	return Env{
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
		Color:   color,
		InReady: func(d time.Duration) bool { return waitReadable(in, d) },
	}, cleanup
}

// model 은 전체 화면 하나(사용자 또는 관리자). 루프가 부른다.
type model interface {
	refresh()                           // 데몬에서 다시 읽기(2초마다·r 키)
	render(w, h int, color bool) string // 프레임 한 장(줄은 "\n" 으로 구분, 줄 수 ≤ h)
	key(ev keyEv) bool                  // 키 하나 처리. 끝내야 하면 true
	batching() bool                     // 붙여넣기 단계: 쌓인 키를 모두 처리한 뒤 한 번만 그린다
	exitCode() int                      // 끝날 때 돌려줄 종료 코드
}

// errRawFailed 는 raw 모드로 바꾸지 못했을 때(호출자는 줄 입력 화면으로 대신한다).
type rawFailed struct{ err error }

func (e rawFailed) Error() string { return "raw 모드 실패: " + e.err.Error() }

// runLoop 는 전체 화면을 돌린다. raw 모드 실패면 (0, rawFailed) — 화면에 아무것도 쓰지 않았다.
func runLoop(env Env, m model) (code int, err error) {
	restore, err := env.MakeRaw()
	if err != nil {
		return 0, rawFailed{err}
	}
	io.WriteString(env.Out, enterScreen)
	defer func() { // 패닉 시에도 defer 가 돌아 복원된다
		io.WriteString(env.Out, leaveScreen)
		restore()
	}()

	keys := make(chan keyEv, 256)
	done := make(chan struct{})
	readerGone := make(chan struct{})
	defer func() {
		close(done)
		// 실제 tty(poll 사용)에서는 키 읽기 goroutine 이 inPoll 안에 끝나므로 기다린 뒤 터미널을 되돌린다
		// (복원 뒤에 남은 goroutine 이 입력을 가로채지 않게). poll 이 없으면(테스트의 막히는 Read) 기다리지 않는다.
		if env.InReady != nil {
			select {
			case <-readerGone:
			case <-time.After(time.Second):
			}
		}
	}()
	go func() {
		defer close(readerGone)
		readKeys(env, keys, done)
	}()

	tick, stop := env.Tick(refreshEvery)
	defer stop()

	draw := func() {
		w, h := env.Size()
		s := m.render(w, h, env.Color)
		io.WriteString(env.Out, "\x1b[H"+strings.ReplaceAll(s, "\n", "\x1b[K\r\n")+"\x1b[K\x1b[J")
	}
	m.refresh()
	draw()
	for {
		select {
		case ev := <-keys:
			if ev.k == kEOF || m.key(ev) {
				return m.exitCode(), nil
			}
			// 붙여넣기 단계에서만: 이미 들어와 쌓인 키는 모두 처리한 뒤 한 번만 그린다 (대량 붙여넣기에서 키마다 다시 쓰지 않게)
			for more := true; more && m.batching(); {
				select {
				case ev := <-keys:
					if ev.k == kEOF || m.key(ev) {
						return m.exitCode(), nil
					}
				default:
					more = false
				}
			}
		case <-tick:
			m.refresh()
		case <-env.Resize: // 터미널이 이전 프레임을 reflow 한 채로 남지 않게 지우고 새 폭으로 바로 다시 그림
			io.WriteString(env.Out, "\x1b[2J")
		case <-env.Signals:
			return ExitInterrupt, nil
		}
		draw()
	}
}

// readKeys 는 키 읽기 goroutine: poll → Read → decoder → keys. 끝나면 kEOF.
func readKeys(env Env, keys chan<- keyEv, done <-chan struct{}) {
	var dec keyDecoder
	send := func(evs []keyEv) bool {
		for _, ev := range evs {
			select {
			case keys <- ev:
			case <-done:
				return false
			}
		}
		return true
	}
	buf := make([]byte, 4096)
	for {
		select {
		case <-done:
			return
		default:
		}
		if env.InReady != nil && !env.InReady(inPoll) {
			if dec.hasLoneEsc() && !send(dec.flush()) { // 조용해졌다: 들고 있던 단독 ESC 는 Esc 키
				return
			}
			continue
		}
		n, err := env.In.Read(buf)
		evs := dec.feed(buf[:n])
		if env.InReady == nil && dec.hasLoneEsc() { // poll 이 없으면(테스트) 읽기 끝의 단독 ESC 는 바로 Esc
			evs = append(evs, dec.flush()...)
		}
		if !send(evs) {
			return
		}
		if err != nil {
			send(append(dec.flush(), keyEv{k: kEOF}))
			return
		}
	}
}
