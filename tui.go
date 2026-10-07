// tui.go - 상태 TUI 루프: raw tty, 키 파싱, 화면 전환, 자동 갱신, 수동 OS 체크 요청 (입출력·시계·tick 은 tuiEnv 로 주입)
package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
)

type keyEv struct {
	k keyKind
	r rune
}

const runeCtrlC = 3

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
		case c == runeCtrlC:
			out = append(out, keyEv{k: kRune, r: runeCtrlC})
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
	Color   bool
}

const (
	enterScreen    = "\x1b[?1049h\x1b[?25l" // 대체 화면 + 커서 숨김
	leaveScreen    = "\x1b[?25h\x1b[?1049l"
	reqHold        = 20 * time.Second // 수동 실행 요청 후 같은 그룹 재요청 차단 시간
	refreshLocal   = 2 * time.Second
	refreshAway    = 5 * time.Second
	refreshReqHold = 5 * time.Second // r 키 즉시 갱신 요청 최소 간격 (데몬 step 주기와 같음)
)

// runTUI: 무인자 `auto_setup` 진입점. tty 가 아니거나 plain 이면 색 없는 텍스트를 한 번 출력하고 끝낸다.
func runTUI(src SnapshotSource, plain bool) int {
	in, out := os.Stdin, os.Stdout
	tty := isTTY(in) && isTTY(out)
	if plain || !tty {
		return printPlain(src, out)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sig)
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
		Color:   os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb",
	}
	return runTUILoop(src, env)
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
	src     SnapshotSource
	env     tuiEnv
	snap    Snapshot
	err     string
	sel     selection
	help    bool
	pending map[string]time.Time // 수동 실행 요청 시각 (jobid\x00yml)
	lastReq time.Time            // 마지막 즉시 갱신(refresh) 요청 시각
}

// runTUILoop: raw 모드 진입 → 그리기·키·tick 루프 → (정상·패닉·시그널 어느 경로든) 화면·termios 복원
func runTUILoop(src SnapshotSource, env tuiEnv) int {
	restore, err := env.MakeRaw()
	if err != nil {
		return printPlain(src, env.Out)
	}
	io.WriteString(env.Out, enterScreen)
	defer func() { // 패닉 시에도 defer 가 돌아 복원된다
		io.WriteString(env.Out, leaveScreen)
		restore()
	}()

	st := &tuiState{src: src, env: env, pending: map[string]time.Time{}}
	keys := make(chan keyEv, 64)
	done := make(chan struct{})
	defer close(done)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := env.In.Read(buf)
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
	st.draw()
	for {
		select {
		case ev := <-keys:
			if ev.k == kEOF || st.handle(ev) {
				return 0
			}
		case <-tick:
			st.refresh()
		case <-env.Signals:
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
}

// clamp: 새 스냅샷에 맞춰 선택 위치 보정 (그룹이 사라지면 화면 1 로)
func (st *tuiState) clamp() {
	n := len(flatGroups(st.snap))
	st.sel.Row = rMax(rMin(st.sel.Row, n-1), 0)
	if st.sel.Detail {
		_, g := findGroup(st.snap, st.sel.JobID, st.sel.Yml)
		if g == nil {
			st.sel.Detail, st.sel.Confirm = false, false
			return
		}
		st.sel.Host = rMax(rMin(st.sel.Host, len(visibleHosts(g, st.sel.Filter))-1), 0)
	}
}

func (st *tuiState) draw() {
	w, h := st.env.Size()
	var s string
	switch {
	case st.help:
		s = renderHelp(w, h, st.env.Color)
	case st.sel.Detail:
		s = renderDetail(st.snap, st.withErr(), w, h, st.env.Color)
	default:
		s = renderOverview(st.snap, st.withErr(), w, h, st.env.Color)
	}
	io.WriteString(st.env.Out, "\x1b[H"+strings.ReplaceAll(s, "\n", "\x1b[K\r\n")+"\x1b[K\x1b[J")
}

// withErr: 갱신 오류가 있으면 안내 줄에 표시
func (st *tuiState) withErr() selection {
	sel := st.sel
	if st.err != "" && sel.Msg == "" {
		sel.Msg = "[X] 상태 갱신 실패: " + st.err
	}
	return sel
}

// handle: 키 처리. 종료하면 true.
func (st *tuiState) handle(ev keyEv) bool {
	if ev.k == kRune && ev.r == runeCtrlC {
		return true
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
	if st.sel.Detail {
		st.handleDetail(ev)
		return false
	}
	return st.handleOverview(ev)
}

func (st *tuiState) handleOverview(ev keyEv) bool {
	refs := flatGroups(st.snap)
	switch {
	case ev.k == kUp || (ev.k == kRune && ev.r == 'k'):
		st.sel.Row = rMax(st.sel.Row-1, 0)
	case ev.k == kDown || (ev.k == kRune && ev.r == 'j'):
		st.sel.Row = rMax(rMin(st.sel.Row+1, len(refs)-1), 0)
	case ev.k == kLeft || ev.k == kRight:
		st.sel.Row = jumpJob(refs, st.sel.Row, ev.k == kRight)
	case ev.k == kEnter:
		if st.sel.Row < len(refs) {
			j := st.snap.Jobs[refs[st.sel.Row].Job]
			st.sel.Detail, st.sel.JobID, st.sel.Yml = true, j.ID, j.Groups[refs[st.sel.Row].Grp].Yml
			st.sel.Host, st.sel.Filter = 0, false
		}
	case ev.k == kRune && (ev.r == 'a' || ev.r == 'A'):
		if st.sel.Row < len(refs) {
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
		st.startManual(g)
	}
}

// startManual: c — 완료 여부와 상관없이 확인(y/n) 단계로 (이미 대기·진행 중이면 안내)
func (st *tuiState) startManual(g *SnapGroup) {
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
		st.sel.Confirm = true
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
	err := st.src.Request(ReqManualRun, map[string]string{"jobid": st.sel.JobID, "yml": st.sel.Yml})
	if err != nil {
		st.sel.Msg = "[X] 요청 실패: " + err.Error()
		return
	}
	st.pending[st.pendKey()] = st.env.Now()
	st.sel.Msg = "요청됨 — 데몬이 수거하면 상태가 '체크중' 으로 바뀝니다"
}

func init() { runReport = runTUI }
