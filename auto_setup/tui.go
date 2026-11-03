// tui.go - 상태 TUI 루프: raw tty, 키 파싱, 화면 전환, 자동 갱신, 수동 OS 체크 요청 (입출력·시계·tick 은 tuiEnv 로 주입)
package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
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
	kLeft
	kRight
	kEnter
	kEsc
	kRune
	kEOF
	kPgUp
	kPgDn
)

type keyEv struct {
	k keyKind
	r rune
}

const (
	runeCtrlC = 3
	runeCtrlZ = 0x1a
)

// parseKeys: 터미널에서 한 번에 읽은 바이트를 키 이벤트로 (ESC [ A / ESC O A 방향키, 단독 ESC, CR/LF, Ctrl-C, 일반 문자)
func parseKeys(b []byte) []keyEv {
	var out []keyEv
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
				case '~': // ESC [ 5 ~ = PageUp, ESC [ 6 ~ = PageDown
					switch string(b[i+2 : j]) {
					case "5":
						out = append(out, keyEv{k: kPgUp})
					case "6":
						out = append(out, keyEv{k: kPgDn})
					}
				case 'A':
					out = append(out, keyEv{k: kUp})
				case 'B':
					out = append(out, keyEv{k: kDown})
				case 'C':
					out = append(out, keyEv{k: kRight})
				case 'D':
					out = append(out, keyEv{k: kLeft})
				}
				i = j + 1
				continue
			}
			out = append(out, keyEv{k: kEsc})
			i++
		case c == '\r' || c == '\n':
			out = append(out, keyEv{k: kEnter})
			i++
			if c == '\r' && i < len(b) && b[i] == '\n' {
				i++
			}
		case c == runeCtrlC || c == runeCtrlZ || c == runeCtrlD || c == runeCtrlX || c == '\t':
			out = append(out, keyEv{k: kRune, r: rune(c)})
			i++
		case c == 0x7f || c == 0x08:
			out = append(out, keyEv{k: kRune, r: runeBackspace})
			i++
		case c >= 0x20 && c < 0x7f:
			out = append(out, keyEv{k: kRune, r: rune(c)})
			i++
		case c >= 0x80:
			r, n := utf8.DecodeRune(b[i:])
			out = append(out, keyEv{k: kRune, r: r})
			i += n
		default:
			i++
		}
	}
	return out
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
	Signals <-chan os.Signal // 없으면 nil
	Resize  <-chan os.Signal // 창 크기 변경(SIGWINCH) — 오면 화면을 지우고 바로 다시 그린다. 없으면 nil
	Color   bool
	// InReady: d 안에 In 에서 읽을 입력이 있으면 true (nil = 항상 바로 Read). 키 읽기 goroutine 을
	// AWX 실행 동안 안전하게 멈추려면 Read 가 오래 막히지 않아야 해서 실제 tty 에서는 poll 방식으로 쓴다.
	InReady func(d time.Duration) bool
	// AwxPrep: AWX 실행 직전 호출 — 터미널을 cooked + intr=^X + susp 비활성으로 바꾸고 원복 함수를 돌려준다 (nil = 아무것도 안 함)
	AwxPrep func() (restore func(), err error)
}

const (
	enterScreen    = "\x1b[?1049h\x1b[?25l" // 대체 화면 + 커서 숨김
	leaveScreen    = "\x1b[?25h\x1b[?1049l"
	reqHold        = 20 * time.Second // 수동 실행 요청 후 같은 그룹 재요청 차단 시간
	refreshLocal   = 2 * time.Second
	refreshAway    = 10 * time.Second
	refreshReqHold = 5 * time.Second // r 키 즉시 갱신 요청 최소 간격 (데몬 step 주기와 같음)
)

// runTUI: 무인자 `auto_setup` 진입점. tty 가 아니거나 plain 이면 색 없는 텍스트를 한 번 출력하고 끝낸다.
func runTUI(src SnapshotSource, plain bool) int {
	in, out := os.Stdin, os.Stdout
	tty := isTTY(in) && isTTY(out)
	if plain || !tty {
		return printPlain(src, out)
	}
	env, cleanup := realTUIEnv(in, out)
	defer cleanup()
	return runTUILoop(src, env)
}

// realTUIEnv: 실제 tty 용 환경 (시그널·창 크기·poll·AWX termios). cleanup 은 시그널 설정을 되돌린다 (무인자 TUI·auto_setup newhost 공용).
func realTUIEnv(in, out *os.File) (tuiEnv, func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	signal.Ignore(syscall.SIGTSTP) // Ctrl+Z 로 TUI 가 멈추지 않게
	winch := make(chan os.Signal, 1)
	notifyResize(winch)
	cleanup := func() {
		signal.Stop(winch)
		signal.Reset(syscall.SIGTSTP)
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
		AwxPrep: func() (func(), error) { return withAwxTermios(in) },
	}
	return env, cleanup
}

// printPlain: 스냅샷 1회 텍스트 출력 (색 없음). 폭은 COLUMNS 또는 120.
func printPlain(src SnapshotSource, out io.Writer) int {
	s, err := src.Snapshot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "[X] 상태 읽기 실패:", err)
		return 1
	}
	w := 120
	if v, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && v >= 40 {
		w = v
	}
	fmt.Fprint(out, renderPlain(s, w))
	return 0
}

// ---- 루프 ----

type tuiState struct {
	src         SnapshotSource
	env         tuiEnv
	snap        Snapshot
	err         string
	sel         selection
	help        bool
	pending     map[string]time.Time // 수동 실행 요청 시각 (jobid\x00yml)
	lastReq     time.Time            // 마지막 즉시 갱신(refresh) 요청 시각
	lastRecheck time.Time            // 마지막 재확인(g) 요청 시각
	view        *viewState           // 전체 화면 보기 (수동 실행 결과·재확인 진행), nil = 없음
	oo          *oneoffState         // x 체크스크립트 단독 실행 입력·실행 화면, nil = 없음
	fastTick    <-chan time.Time     // 단독 실행 중 1초 갱신 tick (실행 중에만)
	fastStop    func()
	awx         *awxState        // w AWX 실행 확인·프로파일 선택 화면, nil = 없음
	chgMenu     []changeMenuItem // m 변경 메뉴 (켜진 항목, 안내 줄 아래 인라인), nil = 없음 (change_tui.go)
	menu        *inlineMenu      // h 호스트 작업·w AWX 항목별 메뉴 (안내 줄 아래 인라인), nil = 없음 (menu_tui.go)
	done        *doneState       // d 수동 완료 프롬프트 (안내 줄 아래 인라인), nil = 없음 (done_tui.go)
	chg         *changeState     // m 변경 흐름 (메뉴에서 고른 뒤), nil = 없음 — 실행 중에는 fastTick 사용
	nh          *newhostState    // w→2 신규 설치 양식 흐름, nil = 없음 (newhost_tui.go) — DNS 조회 중에는 fastTick 사용
	wk          *workState       // s gossh 작업 화면, nil = 없음 (work_tui.go)
	oneShot     bool             // CLI auto_setup newhost: 흐름이 닫히면(AWX 실행 포함) 루프를 끝낸다
	nhSaved     bool             // 이번 실행에서 신규 설치 양식을 저장했음 (CLI 종료 코드)
	restore     func()           // 현재 raw 모드를 되돌리는 함수 (AWX 실행 후 raw 재진입 때 교체됨)
	inMu        sync.Mutex       // 키 읽기 goroutine 이 poll+Read 하는 동안 잡고 있음 — AWX 실행 중 Lock 으로 멈춘다
}

// runTUILoop: raw 모드 진입 → 그리기·키·tick 루프 → (정상·패닉·시그널 어느 경로든) 화면·termios 복원
func runTUILoop(src SnapshotSource, env tuiEnv) int { return runTUILoopWith(src, env, nil) }

// runTUILoopWith: runTUILoop 에 시작 hook 을 더한 것 — start 는 첫 그리기 전에 한 번 불린다
// (CLI auto_setup newhost 가 oneShot 으로 신규 설치 양식 흐름만 연다). start 가 있으면 raw 모드 실패 시 표 대신 오류로 끝낸다.
func runTUILoopWith(src SnapshotSource, env tuiEnv, start func(st *tuiState)) int {
	restore, err := env.MakeRaw()
	if err != nil {
		if start != nil {
			fmt.Fprintln(os.Stderr, "[X] 터미널을 raw 모드로 바꾸지 못했습니다:", err)
			return 1
		}
		return printPlain(src, env.Out)
	}
	io.WriteString(env.Out, enterScreen)
	st := &tuiState{src: src, env: env, pending: map[string]time.Time{}, restore: restore}
	defer func() { // 패닉 시에도 defer 가 돌아 복원된다
		io.WriteString(env.Out, leaveScreen)
		st.restore()
	}()

	st.sel.DateFilter = true
	sessionUser = "" // 세션 user 는 이 TUI 가 끝날 때까지만 기억
	defer st.oneoffShutdown()
	defer st.changeShutdown()
	keys := make(chan keyEv, 64)
	done := make(chan struct{})
	defer close(done)
	go func() {
		buf := make([]byte, 256)
		for {
			select {
			case <-done:
				return
			default:
			}
			st.inMu.Lock() // AWX 실행 중에는 runAwx 가 이 락을 쥐고 있어 입력을 가로채지 않는다
			if env.InReady != nil && !env.InReady(inPoll) {
				st.inMu.Unlock()
				continue
			}
			n, err := env.In.Read(buf)
			st.inMu.Unlock()
			for _, ev := range parseKeys(buf[:n]) {
				select {
				case keys <- ev:
				case <-done:
					return
				}
			}
			if err != nil {
				select {
				case keys <- keyEv{k: kEOF}:
				case <-done:
				}
				return
			}
		}
	}()

	every := refreshLocal
	if !src.Local() {
		every = refreshAway
	}
	tick, stop := env.Tick(every)
	defer stop()

	st.refresh()
	if start != nil {
		start(st)
	}
	st.draw()
	// key: 키 하나 처리, 끝내야 하면 true (oneShot 은 흐름이 닫히면 끝)
	key := func(ev keyEv) bool {
		if ev.k == kEOF || st.handle(ev) {
			return true
		}
		return st.oneShot && st.nh == nil && st.awx == nil && st.oo == nil && st.wk == nil
	}
	for {
		select {
		case ev := <-keys:
			if key(ev) {
				return 0
			}
			// 신규 설치 양식·gossh 작업의 붙여넣기 단계에서만: 이미 들어와 쌓인 키는 모두 처리한 뒤 한 번만 그린다
			// (대량 붙여넣기에서 키마다 화면 전체를 다시 쓰지 않게 — 다른 화면의 키 처리·그리기 순서는 그대로)
			for more := true; more && ((st.nh != nil && st.nh.step == nhPaste) || (st.wk != nil && st.wk.step == wkPaste)); {
				select {
				case ev := <-keys:
					if key(ev) {
						return 0
					}
				default:
					more = false
				}
			}
		case <-tick:
			st.refresh()
		case <-st.fastTick:
		case <-env.Resize: // 터미널이 이전 프레임을 reflow 한 채로 남지 않게 지우고 새 폭으로 바로 다시 그림
			io.WriteString(env.Out, "\x1b[2J")
		case <-env.Signals:
			return 0
		}
		st.pollOneoff()
		st.pollChange()
		st.pollNewhost()
		st.pollWork()
		if st.oneShot && st.nh == nil && st.awx == nil && st.oo == nil && st.wk == nil {
			return 0
		}
		st.draw()
	}
}

func (st *tuiState) refresh() {
	s, err := st.src.Snapshot()
	if err != nil {
		st.err = err.Error()
	} else {
		st.snap, st.err = s, ""
	}
	st.clamp()
	st.updateView()
}

// clamp: 새 스냅샷에 맞춰 선택 위치 보정 (그룹이 사라지면 화면 1 로)
func (st *tuiState) clamp() {
	n := len(visibleGroups(st.snap, st.sel))
	if st.sel.Row >= 0 { // -1 = 날짜 줄
		st.sel.Row = rMax(rMin(st.sel.Row, n-1), 0)
	}
	if st.sel.Detail {
		_, g := findGroup(st.snap, st.sel.JobID, st.sel.Yml)
		if g == nil {
			st.sel.Detail, st.sel.Confirm, st.done = false, false, nil
			if st.menu != nil && st.menu.Open == 'h' { // 호스트 작업 메뉴도 함께 닫기 (w 메뉴는 화면 1 에서도 열리므로 유지)
				st.menu = nil
			}
			return
		}
		st.sel.Host = rMax(rMin(st.sel.Host, len(visibleHosts(g, st.sel.Filter))-1), 0)
	}
}

func (st *tuiState) draw() {
	w, h := st.env.Size()
	var s string
	switch {
	case st.nh != nil:
		s = renderNewhost(st.nh, st.env.Now(), w, h, st.env.Color)
	case st.wk != nil:
		s = renderWork(st.wk, w, h, st.env.Color)
	case st.awx != nil:
		s = renderAwx(st.awx, w, h, st.env.Color)
	case st.oo != nil:
		s = renderOneoff(st.oo, st.env.Now(), w, h, st.env.Color)
	case st.chg != nil:
		s = renderChange(st.chg, w, h, st.env.Color)
	case st.view != nil:
		if st.view.kind == viewRecheck && st.sel.Detail && h >= 24 {
			s = renderRecheckSplit(st.snap, st.withErr(), st.view, w, h, st.env.Color)
		} else {
			s = renderView(st.view, w, h, st.env.Color)
		}
	case st.help:
		s = renderHelp(w, h, st.env.Color)
	case st.sel.Detail:
		s = renderDetail(st.snap, st.inlineSel(w, h), w, h, st.env.Color)
	default:
		s = renderOverview(st.snap, st.inlineSel(w, h), w, h, st.env.Color)
	}
	io.WriteString(st.env.Out, "\x1b[H"+strings.ReplaceAll(s, "\n", "\x1b[K\r\n")+"\x1b[K\x1b[J")
}

// inlineSel: withErr + 키 안내 줄 아래에 붙는 인라인 줄 (m 변경 메뉴 · h/w 항목별 메뉴 · d 수동 완료 프롬프트, 동시에 열리지 않음)
func (st *tuiState) inlineSel(w, h int) selection {
	sel := st.withErr()
	switch {
	case st.chgMenu != nil && st.chg == nil:
		sel.Inline = changeMenuLines(st.chgMenu, w, h, st.env.Color)
	case st.menu != nil:
		sel.Inline = menuLines(st.menu, w, st.env.Color)
	case st.done != nil:
		sel.Inline = st.doneLines(w, st.env.Color)
	}
	return sel
}

// withErr: 갱신 오류가 있으면 안내 줄에 표시 (+ m 변경이 사용 가능하면 키 안내 줄에 `m 변경`)
func (st *tuiState) withErr() selection {
	sel := st.sel
	sel.Change = st.changeAvailable()
	if st.err != "" && sel.Msg == "" {
		sel.Msg = "[X] 상태 갱신 실패: " + st.err
	}
	return sel
}

// handle: 키 처리. 종료하면 true.
func (st *tuiState) handle(ev keyEv) bool {
	if st.nh != nil { // 신규 설치 양식 흐름: 모든 키를 그쪽이 처리 (Ctrl+C/Ctrl+Z 무시, 취소는 Ctrl+X)
		st.handleNewhost(ev)
		return false
	}
	if st.wk != nil { // gossh 작업 화면: 모든 키를 그쪽이 처리 (Ctrl+C/Ctrl+Z 무시)
		st.handleWork(ev)
		return false
	}
	if st.chg != nil || st.chgMenu != nil { // m 변경 메뉴·흐름: Ctrl+C/Ctrl+Z 무시, 나머지 키는 그쪽이 처리
		st.handleChange(ev)
		return false
	}
	if st.menu != nil { // h·w 항목별 메뉴: 글자/번호 선택·Esc/q 닫기, 그 밖의 키는 무시 (m 메뉴와 같은 규칙)
		st.handleMenu(ev)
		return false
	}
	if st.done != nil { // d 수동 완료 프롬프트: 1/2 선택·y 확인 외의 키는 여기서 소비 (x/w/m 과 동시에 열리지 않음)
		st.handleDone(ev)
		return false
	}
	if ev.k == kRune && (ev.r == runeCtrlC || ev.r == runeCtrlZ) {
		st.sel.Msg = "종료는 q 를 누르세요"
		return false
	}
	if st.awx != nil {
		st.handleAwx(ev)
		return false
	}
	if st.oo != nil {
		st.handleOneoff(ev)
		return false
	}
	if st.view != nil {
		st.handleView(ev)
		return false
	}
	if st.help {
		st.help = false
		return false
	}
	if st.sel.Confirm {
		st.sel.Confirm = false
		if ev.k == kRune && (ev.r == 'y' || ev.r == 'Y') {
			st.requestManual()
		} else {
			st.sel.Msg = "취소했습니다"
		}
		return false
	}
	st.sel.Msg = ""
	if ev.k == kRune && ev.r == '?' {
		st.help = true
		return false
	}
	if ev.k == kRune && (ev.r == 'r' || ev.r == 'R') {
		st.requestRefresh()
		st.refresh()
		return false
	}
	if ev.k == kRune && (ev.r == 'x' || ev.r == 'X') {
		st.startOneoff()
		return false
	}
	if ev.k == kRune && (ev.r == 'w' || ev.r == 'W') { // w 는 AWX 메뉴 (ww / w Enter / w 1 = 예전의 AWX 실행)
		st.openMenu('w')
		return false
	}
	if ev.k == kRune && (ev.r == 's' || ev.r == 'S') { // gossh 작업 화면 (메뉴 없음, 화면 1·2 어디서나)
		st.startWork()
		return false
	}
	if ev.k == kRune && (ev.r == 'h' || ev.r == 'H') && st.sel.Detail { // h 호스트 작업 메뉴 (화면 2 에서만)
		st.openMenu('h')
		return false
	}
	if ev.k == kRune && (ev.r == 'm' || ev.r == 'M') && st.startChange() { // 꺼져 있으면 키 무반응 (다른 화면 키와 같이 아래로)
		return false
	}
	if st.sel.Detail {
		st.handleDetail(ev)
		return false
	}
	return st.handleOverview(ev)
}

// pageRows: PageUp/PageDown 이 한 번에 움직일 행 수 (화면 높이 - 머리·꼬리, 최소 1)
func (st *tuiState) pageRows() int {
	_, h := st.env.Size()
	return rMax(h-8, 1)
}

func (st *tuiState) handleOverview(ev keyEv) bool {
	refs := visibleGroups(st.snap, st.sel)
	switch {
	case ev.k == kUp || (ev.k == kRune && ev.r == 'k'):
		if len(st.snap.Jobs) > 0 { // 맨 위에서 한 번 더 ↑ → 날짜 줄
			st.sel.Row = rMax(st.sel.Row-1, -1)
		}
	case ev.k == kDown || (ev.k == kRune && ev.r == 'j'):
		st.sel.Row = rMax(rMin(st.sel.Row+1, len(refs)-1), 0)
	case ev.k == kPgUp:
		if st.sel.Row > 0 {
			st.sel.Row = rMax(st.sel.Row-st.pageRows(), 0)
		}
	case ev.k == kPgDn:
		st.sel.Row = rMax(rMin(st.sel.Row+st.pageRows(), len(refs)-1), 0)
	case (ev.k == kLeft || ev.k == kRight) && st.sel.Row == -1: // 날짜 줄: ← 전날, → 다음날
		delta := 1
		if ev.k == kLeft {
			delta = -1
		}
		st.sel.Date = shiftDate(st.snap, st.sel.Date, delta)
	case ev.k == kLeft || ev.k == kRight:
		st.sel.Row = jumpJob(refs, st.sel.Row, ev.k == kRight)
	case ev.k == kEnter:
		if st.sel.Row >= 0 && st.sel.Row < len(refs) {
			j := st.snap.Jobs[refs[st.sel.Row].Job]
			st.sel.Detail, st.sel.JobID, st.sel.Yml = true, j.ID, j.Groups[refs[st.sel.Row].Grp].Yml
			st.sel.Host, st.sel.Filter = 0, false
		}
	case ev.k == kRune && (ev.r == 'a' || ev.r == 'A'):
		if forceUser != "" { // -user: a = 내 job 만 ↔ 전체 user 보기 (그룹 전체 보기는 Enter 후 a)
			showAllUsers = !showAllUsers
			st.refresh()
			if showAllUsers {
				st.sel.Msg = "전체 user 의 job 을 보여 줍니다 (a: 내 job 만)"
			} else {
				st.sel.Msg = "user " + forceUser + " 의 job 만 보여 줍니다"
			}
		} else if st.sel.Row >= 0 && st.sel.Row < len(refs) {
			j := st.snap.Jobs[refs[st.sel.Row].Job]
			st.sel.Detail, st.sel.JobID, st.sel.Yml = true, j.ID, allView
			st.sel.Host, st.sel.Filter = 0, false
		}
	case ev.k == kEsc || (ev.k == kRune && ev.r == 'q'):
		return true
	}
	return false
}

// jumpJob: 이전/다음 job 의 첫 그룹 행 (끝에서는 그대로)
func jumpJob(refs []grpRef, row int, next bool) int {
	if row < 0 || row >= len(refs) {
		return row
	}
	cur := refs[row].Job
	if next {
		for i := row; i < len(refs); i++ {
			if refs[i].Job != cur {
				return i
			}
		}
		return row
	}
	first := row // 현재 job 의 첫 행
	for first > 0 && refs[first-1].Job == cur {
		first--
	}
	if row > first { // 같은 job 안이면 먼저 그 job 의 첫 행
		return first
	}
	if first == 0 {
		return row
	}
	prev := refs[first-1].Job
	for first > 0 && refs[first-1].Job == prev {
		first--
	}
	return first
}

func (st *tuiState) handleDetail(ev keyEv) {
	_, g := findGroup(st.snap, st.sel.JobID, st.sel.Yml)
	if g == nil {
		st.sel.Detail = false
		return
	}
	n := len(visibleHosts(g, st.sel.Filter))
	switch {
	case ev.k == kUp || (ev.k == kRune && ev.r == 'k'):
		st.sel.Host = rMax(st.sel.Host-1, 0)
	case ev.k == kDown || (ev.k == kRune && ev.r == 'j'):
		st.sel.Host = rMax(rMin(st.sel.Host+1, n-1), 0)
	case ev.k == kPgUp:
		st.sel.Host = rMax(st.sel.Host-st.pageRows(), 0)
	case ev.k == kPgDn:
		st.sel.Host = rMax(rMin(st.sel.Host+st.pageRows(), n-1), 0)
	case ev.k == kLeft || ev.k == kEsc || (ev.k == kRune && ev.r == 'q'):
		st.sel.Detail = false
	case ev.k == kRune && (ev.r == 'f' || ev.r == 'F'):
		st.sel.Filter = !st.sel.Filter
		st.sel.Host = 0
	case ev.k == kRune && (ev.r == 'a' || ev.r == 'A'): // 그룹별 ↔ 전체 보기
		if st.sel.Yml == allView {
			st.sel.Detail = false
		} else {
			st.sel.Yml = allView
			st.sel.Host, st.sel.Filter = 0, false
		}
	case ev.k == kRune && (ev.r == 'c' || ev.r == 'C'):
		st.startManual(g, "")
	case ev.k == kRune && (ev.r == 't' || ev.r == 'T'):
		st.startManual(g, ModeCheck)
	case ev.k == kRune && (ev.r == 'v' || ev.r == 'V'):
		st.openLatestResult()
	case ev.k == kRune && (ev.r == 'g' || ev.r == 'G'):
		st.requestRecheck()
	case ev.k == kRune && (ev.r == 'd' || ev.r == 'D'):
		st.startDone()
	}
}

// requestRecheck: g 키 — 이 그룹(전체 보기면 job 전체)의 완료 제외 호스트를 데몬이 지금 직접 재확인 (os8 → os6_mgmt → 접속불가)
func (st *tuiState) requestRecheck() {
	now := st.env.Now()
	if !st.lastRecheck.IsZero() && now.Sub(st.lastRecheck) < refreshReqHold {
		st.sel.Msg = "방금 재확인을 요청했습니다 — 잠시 후 다시 눌러 주세요"
		return
	}
	st.lastRecheck = now
	if err := st.src.Request(ReqRecheck, map[string]string{"jobid": st.sel.JobID, "yml": st.sel.Yml}); err != nil {
		st.sel.Msg = "[X] 재확인 요청 실패: " + err.Error()
		return
	}
	st.openRecheckView()
}

// startManual: c(설정체크 + 설정수정) / t(설정체크만) — 완료 여부와 상관없이 확인(y/n) 단계로 (이미 대기·진행 중이면 안내)
func (st *tuiState) startManual(g *SnapGroup, mode string) {
	if g.Yml == allView {
		st.sel.Msg = "전체 보기에서는 수동 실행을 할 수 없습니다 - a 로 그룹별 화면으로 가서 c 를 누르세요"
		return
	}
	j, _ := findGroup(st.snap, st.sel.JobID, st.sel.Yml)
	switch {
	case j != nil && hasManual(j, g.Yml):
		st.sel.Msg = "이미 이 그룹의 수동 run 이 대기·진행 중입니다"
	case st.recentlyRequested():
		st.sel.Msg = "방금 요청했습니다 — 데몬이 수거할 때까지 기다려 주세요"
	default:
		st.sel.Confirm, st.sel.Mode = true, mode
	}
}

func hasManual(j *SnapJob, yml string) bool {
	for _, m := range j.Manual {
		if m.Yml == yml {
			return true
		}
	}
	return false
}

func (st *tuiState) pendKey() string { return st.sel.JobID + "\x00" + st.sel.Yml }

func (st *tuiState) recentlyRequested() bool {
	t, ok := st.pending[st.pendKey()]
	return ok && st.env.Now().Sub(t) < reqHold
}

// requestRefresh: r 키 — 데몬에 즉시 ping·준비확인 요청 (os8 무응답 호스트는 데몬이 os6_mgmt 경유로 확인).
// 연타로 요청이 쌓이지 않게 refreshReqHold 안의 재요청은 생략, 결과는 다음 자동 갱신에 보인다.
func (st *tuiState) requestRefresh() {
	now := st.env.Now()
	if !st.lastReq.IsZero() && now.Sub(st.lastReq) < refreshReqHold {
		return
	}
	st.lastReq = now
	if err := st.src.Request(ReqRefresh, nil); err != nil {
		st.sel.Msg = "[X] 즉시 갱신 요청 실패: " + err.Error()
	}
}

// requestManual: y 확인 후 manual-run 요청 (요청 파일만 남김 — 데몬이 5초 주기로 수거)
func (st *tuiState) requestManual() {
	payload := map[string]string{"jobid": st.sel.JobID, "yml": st.sel.Yml}
	if st.sel.Mode != "" {
		payload["mode"] = st.sel.Mode
	}
	err := st.src.Request(ReqManualRun, payload)
	if err != nil {
		st.sel.Msg = "[X] 요청 실패: " + err.Error()
		return
	}
	st.pending[st.pendKey()] = st.env.Now()
	st.openRunView(st.sel.Mode)
}

func init() { runReport = runTUI }
