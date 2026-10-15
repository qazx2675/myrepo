package main

// tui_view.go — BIOS 점검 결과 TUI (계획서_v0.2_TUI.md "TUI 키"·"흐름"). Go 표준 라이브러리만 사용.
//
// 사용법 (단계 6 흐름이 순서대로 부르는 방식 — 모든 메서드는 호출한 goroutine 에서 화면을 그리고 키를 기다린다):
//
//	ui, err := tuiOpenTTY()            // /dev/tty raw 모드 + 대체 화면. 실패하면 tuiWritePlain 으로 평문 출력
//	defer ui.Close()                   // 대체 화면을 나가고 터미널을 되돌린다 (여러 번 불러도 됨)
//	hosts, ok := ui.InputLines("대상 hostname 붙여넣기")   // Enter 줄 추가 · Ctrl+D 완료 · Ctrl+X 취소(ok=false)
//	i, ok := ui.Choose("프로파일 선택", names)              // ↑↓ Enter · 숫자 · Esc/q/Ctrl+X 취소(ok=false)
//	err = ui.Progress("접속·조회", func(j *TUIJob) error { // 작업은 다른 goroutine 에서 돈다
//		j.Set("[2/4 접속·조회 12/100]")                  // 진행 줄 갱신 (200ms 마다·바뀔 때 다시 그림)
//		select { case <-j.Canceled(): ... }             // Ctrl+X 를 누르면 닫힌다
//		return nil })
//	ui.SetTabs([]string{"프로파일 점검", "전체 설정 비교"})  // 결과 보기 위 보기 탭 (Tab 으로 전환)
//	ui.SetResults("BIOS 점검 결과 — 프로파일 VM", groupLines(lines))
//	switch ui.Browse() {                                  // 벤더 줄에서 q 두 번, e, Tab, r 까지 결과 보기
//	case TUITab: ui.SwitchTab(1); ...처음이면 비교 후 ui.SetResults("전체비교 — 기준 host01", vgs2)...
//	case TUIEdit: ...수정 메뉴...; case TUIQuit: return }
//
// API
//   - tuiOpen(env tuiEnv) (*TUI, error) / tuiOpenTTY() (*TUI, error) / (*TUI).Close()
//   - SetResults(title, vgs) 지금 보기 탭의 결과 보기 내용 교체 (벤더·그룹·필터·스크롤 처음으로), SetText(title, lines)
//     글 보기로 교체 (미리보기·Pending 목록 등), SetMessage(msg) 맨 아래 안내 줄
//   - SetTabs(names) / SwitchTab(i) / Tab() 보기 탭: 탭마다 결과 보기 상태(벤더·그룹·단계·필터·스크롤)를 따로 둔다.
//     SetRefKey(on) 지금 보기 탭에서 r 키(기준 다시 선택)를 받음
//   - Browse() TUIAction        결과 보기 키 루프: q 는 한 단계 뒤로 — 관리망 그룹 줄이면 벤더 줄로, 글 보기면 TUIQuit
//     (부른 쪽이 결과 보기로 돌아감), 벤더 줄(맨 위)이면 종료 확인 안내 후 q 를 한 번 더 → TUIQuit (Esc·다른 키는 계속).
//     Esc 는 관리망 그룹 줄·글 보기에서 q 와 같고 벤더 줄에서는 종료 확인만 취소. e → TUIEdit,
//     Tab/Shift+Tab → TUITab (보기 탭 2개 이상), r → TUIRef (SetRefKey) (Ctrl+X 는 안내만)
//   - Ask(q) TUIAnswer          y → TUIYes, n → TUINo, Ctrl+X/Esc → TUIAbort (↑↓ PgUp/PgDn ←→ Enter 로 결과는 계속 볼 수 있음).
//     묻기 전에 이미 들어와 쌓인 키(미리 친 키)는 버린다 — 확인 질문이 앞서 친 y 로 넘어가지 않게
//   - InputLine(prompt, init) (string, bool), InputLines(title) ([]string, bool), Choose(title, items) (int, bool)
//   - Progress(title, fn) error 진행 줄을 띄우고 fn 이 끝날 때까지 결과 보기 키를 받는다. Ctrl+X → j.Canceled() 닫힘.
//     q/Esc 는 무시("작업 중"). fn 이 패닉하면 화면을 되돌릴 수 있게 호출한 goroutine 에서 다시 패닉한다.
//   - Quit() bool               SIGTERM/SIGHUP/SIGINT 를 받았거나 입력이 끝났음 — 이후 모든 메서드는 곧바로
//     돌아온다 (Browse=TUIQuit, Ask=TUIAbort, 입력=ok false, Progress 는 fn 을 취소 상태로 실행). 흐름은 Quit 이면 Close 후 끝낸다.
//     Progress 가 취소한 작업(진행 중 호스트)을 기다리는 동안 시그널을 또 받으면 기다리지 않고 터미널을 되돌린 뒤 바로 끝낸다(tuiExit).
//
// 모든 키는 raw 모드(ISIG 끔)로 받아 Ctrl+C/Ctrl+Z 는 종료하지 않고 안내만 한다. 그리기는 대체 화면 안에서
// 커서를 맨 위로 옮겨 덮어쓰는 제자리 갱신이다. TUI 가 열려 있는 동안 표준출력·표준오류에 쓰면 화면이 깨지므로
// 진행 상황은 TUIJob.Set 으로만 보낸다. TUIJob 외의 메서드는 호출한 goroutine 한 곳에서만 부른다.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tuiEnterScreen = "\x1b[?1049h\x1b[?25l" // 대체 화면 + 커서 숨김
	tuiLeaveScreen = "\x1b[?25h\x1b[?1049l"
	tuiTickEvery   = 200 * time.Millisecond // Progress 중 다시 그리는 주기
)

// tuiEnv 는 TUI 의 바깥 의존입니다 (테스트에서 가짜로 바꾼다).
type tuiEnv struct {
	In      io.Reader
	Out     io.Writer
	Size    func() (w, h int)
	MakeRaw func() (restore func(), err error)
	Tick    func(d time.Duration) (c <-chan time.Time, stop func()) // nil 이면 time.Ticker
	Signals <-chan os.Signal                                        // 끝내야 하는 시그널 (nil 가능)
	Resize  <-chan os.Signal                                        // 창 크기 변경 (nil 가능)
	Color   bool
	Close   func() // Close 때 마지막으로 부름 (시그널 해제·tty 닫기, nil 가능)
	Ready   func() // 키 루프가 화면을 그리고 키를 기다리기 시작할 때 부름 (테스트가 키를 넣는 곳, nil 가능)
}

// tuiExit 는 끝내는 중에 시그널을 또 받았을 때 부르는 종료입니다 (테스트에서 바꿔 끼운다).
var tuiExit = os.Exit

// tuiExitSignal 은 두 번째 시그널로 바로 끝낼 때의 종료 코드입니다.
const tuiExitSignal = 1

// tuiQuitConfirm 은 결과 보기 맨 위(벤더 줄)에서 q 를 한 번 눌렀을 때의 안내입니다.
const tuiQuitConfirm = "종료하려면 q 를 한 번 더 누르세요 (Esc: 계속)"

// TUIAction 은 Browse 가 끝난 까닭입니다.
type TUIAction int

const (
	TUIQuit TUIAction = iota // 벤더 줄에서 q 두 번(종료 확인), 글 보기에서 q/Esc(돌아가기), 시그널, 입력 끝
	TUIEdit                  // e 수정 메뉴
	TUITab                   // Tab/Shift+Tab 보기 탭 전환 (탭이 2개라 방향은 따로 없음)
	TUIRef                   // r 기준 다시 선택 (SetRefKey 로 켠 보기에서만)
)

// TUIAnswer 는 Ask 의 답입니다. 0 값이 TUIAbort 라서 모르면 "취소" 로 다룬다.
type TUIAnswer int

const (
	TUIAbort TUIAnswer = iota // Ctrl+X / Esc / 시그널 / 입력 끝
	TUIYes
	TUINo
)

type tuiPromptKind int

const (
	tpYesNo tuiPromptKind = iota + 1
	tpLine
	tpLines
	tpChoose
)

// tuiPrompt 는 화면 아래쪽(키 안내 줄 위)에 뜨는 질문·입력 상자입니다.
type tuiPrompt struct {
	kind  tuiPromptKind
	title string
	buf   []rune   // 지금 입력 중인 줄
	lines []string // 여러 줄 입력에서 Enter 로 확정된 줄
	items []string // 고르기 목록
	cur   int      // 고르기 커서
	num   string   // 숫자로 고르는 중
}

// TUIJob 은 Progress 로 돌리는 작업에 주는 손잡이입니다 (작업 goroutine 에서 불러도 된다).
type TUIJob struct {
	mu     sync.Mutex
	status string
	wake   chan struct{} // 진행 줄이 바뀌었음 (버퍼 1)
	cancel chan struct{}
	once   sync.Once
}

// Set 은 진행 줄 글을 바꿉니다.
func (j *TUIJob) Set(status string) {
	j.mu.Lock()
	j.status = status
	j.mu.Unlock()
	select {
	case j.wake <- struct{}{}:
	default:
	}
}

// Canceled 는 Ctrl+X(또는 시그널)로 취소를 요청하면 닫히는 채널입니다.
func (j *TUIJob) Canceled() <-chan struct{} { return j.cancel }

// IsCanceled 는 취소 요청이 있었는지 봅니다.
func (j *TUIJob) IsCanceled() bool {
	select {
	case <-j.cancel:
		return true
	default:
		return false
	}
}

func (j *TUIJob) doCancel() { j.once.Do(func() { close(j.cancel) }) }

func (j *TUIJob) text() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

// TUI 는 결과 보기 화면 1개와 그 위에 뜨는 질문·입력·진행 줄입니다.
type TUI struct {
	env     tuiEnv
	restore func()
	keys    chan tuiKey
	done    chan struct{}
	quit    bool
	closed  bool

	title    string
	vgs      []VendorGroups
	text     []string
	textMode bool
	vi, gi   int  // 선택한 벤더·관리망 그룹
	inGroup  bool // ←→ 가 움직이는 줄: false 벤더 줄, true 관리망 그룹 줄 (Enter 로 들어가고 q/Esc 로 나옴)
	filter   rune // tuiFilterAll ...
	top      int  // 본문 첫 줄 위치
	refKey   bool // r 키(기준 다시 선택)를 받음
	quitArm  bool // 벤더 줄에서 q 를 한 번 눌러 종료 확인 중 (다음 q 면 종료, 다른 키면 취소)
	bodyH    int  // 마지막으로 그린 본문 높이 (PgUp/PgDn 단위)
	help     bool
	msg      string
	prompt   *tuiPrompt
	job      *TUIJob

	tabs  []string      // 보기 탭 이름 (2개 이상이면 줄을 그리고 Tab 으로 전환)
	ti    int           // 지금 보기 탭
	saved []tuiResState // 보기 탭마다 저장한 결과 보기 상태 (지금 탭 것은 위 필드에 있음)
}

// tuiResState 는 보기 탭 1개의 결과 보기 상태입니다.
type tuiResState struct {
	title       string
	vgs         []VendorGroups
	vi, gi, top int
	inGroup     bool
	filter      rune
	refKey      bool
}

// tuiOpen 은 raw 모드·대체 화면으로 들어가고 키 읽기를 시작합니다. raw 모드 실패면 아무것도 쓰지 않고 오류.
func tuiOpen(env tuiEnv) (*TUI, error) {
	restore, err := env.MakeRaw()
	if err != nil {
		if env.Close != nil {
			env.Close()
		}
		return nil, err
	}
	io.WriteString(env.Out, tuiEnterScreen)
	u := &TUI{env: env, restore: restore, keys: make(chan tuiKey, 256), done: make(chan struct{}), filter: tuiFilterAll}
	_, h := env.Size()
	u.bodyH = tuiMaxInt(h-8, 1)
	go u.readKeys()
	return u, nil
}

// tuiOpenTTY 는 제어 터미널(/dev/tty)로 TUI 를 엽니다. 표준입력이 파이프여도 키는 터미널에서 받는다.
// SIGINT/SIGTERM/SIGHUP 을 받으면 메서드가 돌아오고(Quit) 호출한 쪽의 Close 가 터미널을 되돌린다.
func tuiOpenTTY() (*TUI, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	// f.Fd() 는 파일을 블로킹 모드로 바꿔 Close 가 읽기를 깨우지 못하게 하므로 SyscallConn 으로 fd 만 얻는다
	fd := -1
	if rc, err := f.SyscallConn(); err == nil {
		rc.Control(func(p uintptr) { fd = int(p) })
	}
	if fd < 0 || !ttyIsTerminal(fd) {
		f.Close()
		return nil, errors.New("터미널이 아닙니다")
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, tuiQuitSignals()...)
	winch := make(chan os.Signal, 1)
	tuiNotifyResize(winch)
	return tuiOpen(tuiEnv{
		In:      f,
		Out:     f,
		Size:    func() (int, int) { return tuiTermSize(fd) },
		MakeRaw: func() (func(), error) { return tuiMakeRaw(fd) },
		Signals: sig,
		Resize:  winch,
		Color:   os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb",
		Close: func() {
			signal.Stop(sig)
			signal.Stop(winch)
			f.Close()
		},
	})
}

// Close 는 대체 화면을 나가고 터미널을 되돌립니다. 여러 번 불러도 된다.
func (u *TUI) Close() {
	if u.closed {
		return
	}
	u.closed = true
	close(u.done)
	io.WriteString(u.env.Out, tuiLeaveScreen)
	u.restore()
	if u.env.Close != nil {
		u.env.Close()
	}
}

// Quit 은 시그널을 받았거나 입력이 끝났는지 봅니다.
func (u *TUI) Quit() bool { return u.quit }

func (u *TUI) readKeys() {
	buf := make([]byte, 4096)
	for {
		n, err := u.env.In.Read(buf)
		for _, k := range tuiParseKeys(buf[:n]) {
			select {
			case u.keys <- k:
			case <-u.done:
				return
			}
		}
		if err != nil {
			select {
			case u.keys <- tuiKey{k: tkEOF}:
			case <-u.done:
			}
			return
		}
	}
}

// ---- 내용 ----

// SetResults 는 지금 보기 탭의 결과 보기 내용을 바꿉니다 (벤더·그룹·단계·필터·스크롤은 처음으로, r 키 끔).
func (u *TUI) SetResults(title string, vgs []VendorGroups) {
	u.title, u.vgs, u.text, u.textMode = title, vgs, nil, false
	u.vi, u.gi, u.top, u.filter, u.inGroup, u.refKey = 0, 0, 0, tuiFilterAll, false, false
}

// SetRefKey 는 지금 보기 탭에서 r 키(Browse → TUIRef)를 받을지 정합니다.
func (u *TUI) SetRefKey(on bool) { u.refKey = on }

// SetTabs 는 결과 보기 위의 보기 탭입니다 (지금 내용은 0번 탭, 다른 탭은 빈 결과 보기로 시작).
func (u *TUI) SetTabs(names []string) {
	u.tabs, u.ti, u.saved = names, 0, make([]tuiResState, len(names))
}

// Tab 은 지금 보기 탭 번호입니다.
func (u *TUI) Tab() int { return u.ti }

// SwitchTab 은 지금 결과 보기 상태를 저장하고 i 번 보기 탭의 상태로 바꿉니다 (글 보기였어도 결과 보기로 돌아감).
func (u *TUI) SwitchTab(i int) {
	if i < 0 || i >= len(u.tabs) {
		return
	}
	if i != u.ti {
		u.saved[u.ti] = tuiResState{u.title, u.vgs, u.vi, u.gi, u.top, u.inGroup, u.filter, u.refKey}
		s := u.saved[i]
		if s.title == "" {
			s.title, s.filter = u.tabs[i], tuiFilterAll
		}
		u.ti = i
		u.title, u.vgs, u.vi, u.gi, u.top, u.inGroup, u.filter, u.refKey = s.title, s.vgs, s.vi, s.gi, s.top, s.inGroup, s.filter, s.refKey
	}
	u.text, u.textMode = nil, false
}

// SetText 는 글 줄 보기로 바꿉니다 (탭·필터 없음, 스크롤만).
func (u *TUI) SetText(title string, lines []string) {
	u.title, u.text, u.textMode = title, lines, true
	u.top = 0
}

// SetMessage 는 맨 아래 안내 줄입니다 (다음 키를 누르면 지워진다).
func (u *TUI) SetMessage(msg string) { u.msg = msg }

// ---- 루프 ----

// run 은 화면을 그리고 키·tick·진행 줄 변경·창 크기 변경·시그널을 기다립니다. handle 이 true 를 돌려주거나
// stop 이 닫히거나 시그널·입력 끝(u.quit)이면 돌아옵니다.
func (u *TUI) run(handle func(k tuiKey) bool, tick <-chan time.Time, wake <-chan struct{}, stop <-chan struct{}) {
	if u.quit {
		return
	}
	u.draw()
	if u.env.Ready != nil {
		u.env.Ready()
	}
	for {
		select {
		case k := <-u.keys:
			if u.key(k, handle) {
				return
			}
			// 여러 줄 붙여넣기 중에는 이미 들어와 쌓인 키를 모두 처리한 뒤 한 번만 그린다
			for more := u.prompt != nil && u.prompt.kind == tpLines; more; {
				select {
				case k := <-u.keys:
					if u.key(k, handle) {
						return
					}
				default:
					more = false
				}
			}
		case <-tick:
		case <-wake:
		case <-stop:
			return
		case <-u.env.Resize: // 이전 프레임이 reflow 된 채로 남지 않게 지우고 새 크기로 다시 그림
			io.WriteString(u.env.Out, "\x1b[2J")
		case <-u.env.Signals:
			u.quit = true
			return
		}
		u.draw()
	}
}

// key 는 공통 키(입력 끝·도움말 닫기·Ctrl+C/Ctrl+Z)를 처리하고 나머지를 handle 에 넘깁니다.
func (u *TUI) key(k tuiKey, handle func(tuiKey) bool) bool {
	if k.k == tkEOF {
		u.quit = true
		return true
	}
	if u.help { // 도움말은 아무 키로 닫힘 (그 키는 버린다)
		u.help = false
		return false
	}
	if k.isRune(tuiCtrlC, tuiCtrlZ) {
		u.msg = "종료는 q 를 두 번 누르세요"
		if u.prompt != nil || u.job != nil {
			u.msg = "취소는 Ctrl+X 를 누르세요"
		}
		return false
	}
	u.msg = ""
	return handle(k)
}

func (u *TUI) tick(d time.Duration) (<-chan time.Time, func()) {
	if u.env.Tick != nil {
		return u.env.Tick(d)
	}
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// Browse 는 결과 보기 키 루프입니다: q 는 한 단계 뒤로 — 관리망 그룹 줄이면 벤더 줄로, 글 보기면 TUIQuit(돌아가기),
// 벤더 줄(맨 위)이면 종료 확인 안내를 띄우고 q 를 한 번 더 누르면 TUIQuit (Esc·다른 키는 확인 취소, 계속 보기).
// Esc 는 벤더 줄에서 끝내지 않는다. e → TUIEdit, Tab/Shift+Tab → TUITab (보기 탭 2개 이상), r → TUIRef (SetRefKey 로 켠 보기).
func (u *TUI) Browse() TUIAction {
	act := TUIQuit
	u.quitArm = false
	defer func() { u.quitArm = false }()
	u.run(func(k tuiKey) bool {
		armed := u.quitArm
		u.quitArm = false // 종료 확인은 바로 다음 키 하나에만 (key() 가 안내 줄도 지움)
		switch {
		case k.isRune('q', 'Q') && !u.textMode && !u.inGroup:
			if armed {
				act = TUIQuit
				return true
			}
			u.quitArm, u.msg = true, tuiQuitConfirm
			return false
		case k.k == tkEsc && !u.textMode && !u.inGroup: // 맨 위의 Esc 는 종료 확인 취소만
			return false
		case k.k == tkEsc || k.isRune('q', 'Q'):
			if u.back() {
				return false
			}
			act = TUIQuit // 글 보기: 결과 보기로 돌아가기
			return true
		case k.isRune(tuiCtrlX):
			u.msg = "Ctrl+X 는 진행 중 작업 취소입니다 — 종료는 q 를 두 번 누르세요"
			return false
		case k.isRune('e', 'E'):
			act = TUIEdit
			return true
		case (k.isRune(tuiTab) || k.k == tkBackTab) && !u.textMode && len(u.tabs) > 1:
			act = TUITab
			return true
		case k.isRune('r', 'R') && !u.textMode && u.refKey:
			act = TUIRef
			return true
		}
		u.navKey(k) // Ctrl+X 등 나머지는 무시
		return false
	}, nil, nil, nil)
	return act
}

// Progress 는 fn 을 다른 goroutine 에서 돌리며 진행 줄을 보여 줍니다. 그동안 결과 보기 키는 그대로 동작하고
// Ctrl+X 는 취소 요청(j.Canceled 닫힘), q/Esc 는 관리망 그룹 줄에서 벤더 줄로 돌아가기만 하고 끝내지 않습니다
// (Tab·e·r 도 무시). fn 이 끝날 때까지 기다려 그 오류를 돌려줍니다.
func (u *TUI) Progress(title string, fn func(j *TUIJob) error) error {
	j := &TUIJob{status: title, wake: make(chan struct{}, 1), cancel: make(chan struct{})}
	if u.quit {
		j.doCancel()
	}
	fin := make(chan struct{})
	var err error
	var pv interface{}
	go func() {
		defer close(fin)
		defer func() { pv = recover() }()
		err = fn(j)
	}()
	u.job = j
	tick, stop := u.tick(tuiTickEvery)
	u.run(func(k tuiKey) bool {
		switch {
		case k.isRune(tuiCtrlX):
			j.doCancel()
			u.msg = "취소를 요청했습니다 — 작업이 멈추기를 기다리는 중"
		case (k.k == tkEsc || k.isRune('q', 'Q')) && u.back():
		case k.k == tkEsc || k.isRune('q', 'Q', 'e', 'E'):
			u.msg = "작업 중입니다 — 취소는 Ctrl+X"
		default:
			u.navKey(k)
		}
		return false
	}, tick, j.wake, fin)
	stop()
	if u.quit {
		j.doCancel()
	}
	// 취소한 작업(진행 중 호스트)이 끝나기를 기다리는 동안에도 시그널을 받는다: 끝내는 중에 또 받으면
	// 기다리지 않고 터미널을 되돌린 뒤 바로 끝낸다 (시그널이 버려져 종료가 늦어지지 않게).
	for waiting := true; waiting; {
		select {
		case <-fin:
			waiting = false
		case <-u.env.Signals:
			if !u.quit { // 첫 시그널: 작업 취소 후 계속 기다림
				u.quit = true
				j.doCancel()
				continue
			}
			u.Close()
			tuiExit(tuiExitSignal)
		}
	}
	u.job = nil
	if pv != nil {
		panic(pv)
	}
	return err
}

// Ask 는 y/n 질문입니다. 방향키·PgUp/PgDn·Enter 로 뒤의 결과는 계속 볼 수 있다.
// 질문을 띄우기 전에 이미 들어와 쌓인 키는 버린다 (앞 화면에서 미리 친 y 가 확인으로 쓰이지 않게).
func (u *TUI) Ask(question string) TUIAnswer {
	u.drainKeys()
	ans := TUIAbort
	u.prompt = &tuiPrompt{kind: tpYesNo, title: question}
	defer func() { u.prompt = nil }()
	u.run(func(k tuiKey) bool {
		switch {
		case k.isRune('y', 'Y'):
			ans = TUIYes
			return true
		case k.isRune('n', 'N'):
			ans = TUINo
			return true
		case k.isRune(tuiCtrlX) || k.k == tkEsc:
			ans = TUIAbort
			return true
		case k.k != tkRune || k.isRune(tuiTab):
			u.navKey(k)
		default:
			u.msg = "y 또는 n 을 누르세요 (취소 Ctrl+X)"
		}
		return false
	}, nil, nil, nil)
	return ans
}

// drainKeys 는 이미 들어와 쌓인 키를 버립니다 (입력 끝이면 Quit).
func (u *TUI) drainKeys() {
	for {
		select {
		case k := <-u.keys:
			if k.k == tkEOF {
				u.quit = true
			}
		default:
			return
		}
	}
}

// InputLine 은 한 줄 입력입니다 (Enter 확인, Ctrl+X/Esc 취소). 앞뒤 공백은 지운다.
func (u *TUI) InputLine(prompt, initial string) (string, bool) {
	p := &tuiPrompt{kind: tpLine, title: prompt, buf: []rune(initial)}
	u.prompt = p
	defer func() { u.prompt = nil }()
	ok := false
	u.run(func(k tuiKey) bool {
		switch {
		case k.k == tkEnter:
			ok = true
			return true
		case k.isRune(tuiCtrlX) || k.k == tkEsc:
			return true
		default:
			p.edit(k)
		}
		return false
	}, nil, nil, nil)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(string(p.buf)), true
}

// InputLines 는 여러 줄 입력(붙여넣기)입니다: Enter 줄 추가, Backspace 지우기(빈 줄이면 앞 줄로), Ctrl+D 완료,
// Ctrl+X 취소. 앞뒤 공백을 지운 빈 줄이 아닌 줄만 돌려준다.
func (u *TUI) InputLines(title string) ([]string, bool) {
	p := &tuiPrompt{kind: tpLines, title: title}
	u.prompt = p
	defer func() { u.prompt = nil }()
	ok := false
	push := func() {
		if s := strings.TrimSpace(string(p.buf)); s != "" {
			p.lines = append(p.lines, s)
		}
		p.buf = nil
	}
	u.run(func(k tuiKey) bool {
		switch {
		case k.k == tkEnter:
			push()
		case k.isRune(tuiCtrlD):
			push()
			ok = true
			return true
		case k.isRune(tuiCtrlX):
			return true
		case k.isRune(tuiBackspace) && len(p.buf) == 0 && len(p.lines) > 0:
			p.buf = []rune(p.lines[len(p.lines)-1])
			p.lines = p.lines[:len(p.lines)-1]
		case k.k == tkEsc:
			u.msg = "완료는 Ctrl+D, 취소는 Ctrl+X 입니다"
		default:
			p.edit(k)
		}
		return false
	}, nil, nil, nil)
	if !ok {
		return nil, false
	}
	return p.lines, true
}

// Choose 는 목록 고르기입니다: ↑↓(k j) Enter, 숫자(번호가 더 길어질 수 없으면 바로 선택, 아니면 Enter),
// Esc/q/Ctrl+X 취소. 목록이 비면 바로 (-1, false).
func (u *TUI) Choose(title string, items []string) (int, bool) {
	if len(items) == 0 {
		return -1, false
	}
	p := &tuiPrompt{kind: tpChoose, title: title, items: items}
	u.prompt = p
	defer func() { u.prompt = nil }()
	pick := -1
	u.run(func(k tuiKey) bool {
		switch {
		case k.k == tkUp || k.isRune('k'):
			p.cur, p.num = tuiMaxInt(p.cur-1, 0), ""
		case k.k == tkDown || k.isRune('j'):
			p.cur, p.num = tuiMinInt(p.cur+1, len(items)-1), ""
		case k.k == tkHome:
			p.cur, p.num = 0, ""
		case k.k == tkEnd:
			p.cur, p.num = len(items)-1, ""
		case k.k == tkEnter:
			pick = p.cur
			return true
		case k.k == tkEsc || k.isRune('q', 'Q', tuiCtrlX):
			return true
		case k.isRune(tuiBackspace) && p.num != "":
			p.num = p.num[:len(p.num)-1]
		case k.k == tkRune && k.r >= '0' && k.r <= '9':
			v, _ := strconv.Atoi(p.num + string(k.r))
			if v < 1 || v > len(items) {
				p.num = ""
				u.msg = fmt.Sprintf("1~%d 사이 번호를 누르세요", len(items))
				return false
			}
			p.num, p.cur = p.num+string(k.r), v-1
			if v*10 > len(items) { // 숫자를 더 붙일 수 없으면 바로 선택
				pick = p.cur
				return true
			}
		}
		return false
	}, nil, nil, nil)
	return pick, pick >= 0
}

// edit 은 한 줄 편집 키입니다 (Backspace, 보이는 문자 추가).
func (p *tuiPrompt) edit(k tuiKey) {
	switch {
	case k.isRune(tuiBackspace):
		if len(p.buf) > 0 {
			p.buf = p.buf[:len(p.buf)-1]
		}
	case k.k == tkRune && k.r >= 0x20 && k.r != tuiBackspace:
		p.buf = append(p.buf, k.r)
	}
}

// ---- 결과 보기 키 ----

// navKey 는 결과 보기 이동(벤더·관리망 그룹·스크롤)·필터·도움말 키입니다. 보기 탭(Tab)·뒤로(q/Esc)는 부르는 쪽에서.
func (u *TUI) navKey(k tuiKey) {
	page := tuiMaxInt(u.bodyH, 1)
	switch {
	case k.isRune('?'):
		u.help = true
	case k.k == tkUp || k.isRune('k'):
		u.top--
	case k.k == tkDown || k.isRune('j'):
		u.top++
	case k.k == tkPgUp:
		u.top -= page
	case k.k == tkPgDn:
		u.top += page
	case k.k == tkHome || k.isRune('g'):
		u.top = 0
	case k.k == tkEnd || k.isRune('G'):
		u.top = u.bodyCount()
	case u.textMode || len(u.vgs) == 0:
	case k.k == tkEnter && !u.inGroup: // 벤더 안으로: 이제 ←→ 가 관리망 그룹을 움직인다
		if len(u.vgs[u.vi].Groups) > 1 {
			u.inGroup = true
		} else {
			u.msg = "이 벤더는 관리망 그룹이 하나뿐입니다"
		}
	case u.inGroup && k.k == tkLeft && u.gi > 0:
		u.gi, u.top = u.gi-1, 0
	case u.inGroup && k.k == tkRight && u.gi < len(u.vgs[u.vi].Groups)-1:
		u.gi, u.top = u.gi+1, 0
	case !u.inGroup && k.k == tkLeft && u.vi > 0:
		u.vi, u.gi, u.top = u.vi-1, 0, 0
	case !u.inGroup && k.k == tkRight && u.vi < len(u.vgs)-1:
		u.vi, u.gi, u.top = u.vi+1, 0, 0
	case k.isRune(tuiFilterAll, tuiFilterOK, tuiFilterDiff, tuiFilterNA, tuiFilterSum):
		u.filter, u.top = k.r, 0
	}
	u.clampTop()
}

// back 은 관리망 그룹 줄에서 벤더 줄로 한 단계 돌아갑니다 (돌아갔으면 true, 이미 맨 위 단계면 false).
func (u *TUI) back() bool {
	if u.textMode || !u.inGroup {
		return false
	}
	u.inGroup = false
	return true
}

func (u *TUI) clampTop() {
	maxTop := tuiMaxInt(u.bodyCount()-u.bodyH, 0)
	u.top = tuiMaxInt(tuiMinInt(u.top, maxTop), 0)
}

// group 은 선택한 관리망 그룹입니다 (결과가 없으면 nil).
func (u *TUI) group() *LineGroup {
	if u.vi >= len(u.vgs) || u.gi >= len(u.vgs[u.vi].Groups) {
		return nil
	}
	return &u.vgs[u.vi].Groups[u.gi]
}

// bodyCount 는 본문 줄 수입니다 (머리 줄 제외).
func (u *TUI) bodyCount() int {
	if u.textMode {
		return len(u.text)
	}
	g := u.group()
	switch {
	case g == nil:
		return 1
	case u.filter == tuiFilterSum:
		return len(tuiSummarize(*g))
	}
	return len(tuiFilterLines(g.Lines, u.filter))
}

// ---- 그리기 ----

func (u *TUI) draw() {
	w, h := u.env.Size()
	// 마지막 칸은 비워 둔다 (자동 줄바꿈 대기 상태에서 \x1b[K 가 마지막 글자를 지우지 않게)
	lines := u.frame(tuiMaxInt(w-1, 20), tuiMaxInt(h, 6))
	io.WriteString(u.env.Out, "\x1b[H"+strings.Join(lines, "\x1b[K\r\n")+"\x1b[K\x1b[J")
}

// frame 은 화면 한 장(줄 목록)입니다: 머리(제목·탭·표 머리) + 본문(스크롤) + 꼬리(진행·질문·키 안내·안내 줄).
func (u *TUI) frame(w, h int) []string {
	color := u.env.Color
	if u.help {
		var out []string
		for i, l := range tuiHelpLines() {
			st := ""
			if i == 0 {
				st = tuiStBold
			}
			out = append(out, tuiSegLine(color, w, tuiSeg{l, st}))
		}
		if len(out) > h {
			out = out[:h]
		}
		return out
	}
	var head, body []string
	g := u.group()
	if !u.textMode && len(u.tabs) > 1 {
		head = append(head, tuiTabsLine(color, w, "화면", u.tabs, u.ti, "Tab 전환", false))
	}
	switch {
	case u.textMode:
		for _, l := range u.text {
			body = append(body, tuiSegLine(color, w, tuiSeg{l, ""}))
		}
	case g == nil:
		body = []string{" (결과 없음)"}
	default:
		names := make([]string, len(u.vgs))
		for i, vg := range u.vgs {
			names[i] = tuiDash(vg.Vendor)
		}
		head = append(head, tuiTabsLine(color, w, "벤더", names, u.vi, "", !u.inGroup))
		hosts := fmt.Sprintf("호스트 %d대", len(g.Hosts))
		if gs := u.vgs[u.vi].Groups; len(gs) > 1 {
			gn := make([]string, len(gs))
			for i, x := range gs {
				gn[i] = tuiDash(x.Name)
			}
			head = append(head, tuiTabsLine(color, w, "관리망", gn, u.gi, hosts, u.inGroup))
		} else {
			head = append(head, tuiSegLine(color, w, tuiSeg{"   관리망  ", tuiStBold}, tuiSeg{tuiDash(g.Name), ""}, tuiSeg{"   " + hosts, tuiStGray}))
		}
		var tbl []string
		if u.filter == tuiFilterSum {
			tbl = tuiSummaryTable(color, w, *g)
		} else {
			tbl = tuiLineTable(color, w, tuiFilterLines(g.Lines, u.filter))
		}
		head = append(head, tbl[0])
		body = tbl[1:]
	}
	foot := u.footLines(w, h)
	bodyH := tuiMaxInt(h-1-len(head)-len(foot), 1)
	u.bodyH = bodyH
	u.clampTop()
	end := tuiMinInt(u.top+bodyH, len(body))
	out := []string{u.titleLine(w, g, len(body), end)}
	out = append(out, head...)
	if u.top < end {
		out = append(out, body[u.top:end]...)
	}
	for i := end - u.top; i < bodyH; i++ { // 꼬리가 맨 아래에 붙도록 빈 줄
		out = append(out, "")
	}
	out = append(out, foot...)
	if len(out) > h {
		out = out[:h]
	}
	return out
}

// titleLine 은 " 제목   [보기: 전체]  OK 3  difference 1  N/A 0   줄 1-20/45" 입니다.
func (u *TUI) titleLine(w int, g *LineGroup, n, end int) string {
	segs := []tuiSeg{{" " + u.title, tuiStBold}}
	if !u.textMode && g != nil {
		ok, diff, na := tuiCount(g.Lines)
		segs = append(segs, tuiSeg{"   [보기: " + tuiFilterName(u.filter) + "]  ", tuiStCyan})
		segs = append(segs, tuiCountSegs(ok, diff, na)...)
	}
	if n > 0 {
		segs = append(segs, tuiSeg{fmt.Sprintf("   줄 %d-%d/%d", u.top+1, end, n), tuiStGray})
	}
	return tuiSegLine(u.env.Color, w, segs...)
}

// footLines 는 진행 줄 · 질문·입력 상자 · 키 안내 · 안내 줄입니다.
func (u *TUI) footLines(w, h int) []string {
	color := u.env.Color
	var out []string
	if u.job != nil {
		out = append(out, tuiSegLine(color, w, tuiSeg{" [진행] ", tuiStCyan}, tuiSeg{u.job.text(), tuiStYellow}))
	}
	if u.prompt != nil {
		out = append(out, u.promptLines(w, h)...)
	}
	out = append(out, tuiWrapItems(color, w, u.hintItems())...)
	out = append(out, tuiSegLine(color, w, tuiSeg{" " + u.msg, "1;33"}))
	return out
}

func (u *TUI) hintItems() []string {
	if p := u.prompt; p != nil {
		switch p.kind {
		case tpYesNo:
			return []string{"y 예", "n 아니오", "Ctrl+X 취소", "↑↓/PgUp/PgDn 결과 보기"}
		case tpLine:
			return []string{"Enter 확인", "Backspace 지우기", "Ctrl+X 취소"}
		case tpLines:
			return []string{"Enter 줄 추가", "Backspace 지우기", "Ctrl+D 완료", "Ctrl+X 취소"}
		case tpChoose:
			return []string{"↑↓ 이동", "Enter 선택", "숫자 바로 선택", "Esc 취소"}
		}
	}
	nav := []string{"↑↓ 한 줄", "PgUp/PgDn 한 화면"}
	quit := "q 두 번 종료" // 맨 위(벤더 줄)는 종료 확인
	if u.textMode {
		quit = "q/Esc 돌아가기"
	} else {
		var lv []string // 지금 단계(벤더 줄 / 관리망 그룹 줄)의 키를 앞에
		if len(u.tabs) > 1 && u.job == nil {
			lv = append(lv, "Tab 화면 전환")
		}
		switch {
		case u.inGroup:
			lv, quit = append(lv, "←→ 관리망"), "q/Esc 벤더로"
		case len(u.vgs) > 0:
			lv = append(lv, "←→ 벤더")
			if u.vi < len(u.vgs) && len(u.vgs[u.vi].Groups) > 1 {
				lv = append(lv, "Enter 관리망 고르기")
			}
		}
		nav = append(lv, nav...)
		nav = append(nav, "a 전체", "o OK만", "f difference만", "n N/A만", "s 요약")
		if u.refKey && u.job == nil {
			nav = append(nav, "r 기준 다시 선택")
		}
	}
	if u.job != nil {
		if u.inGroup {
			nav = append(nav, quit)
		}
		return append(nav, "Ctrl+X 작업 취소", "? 도움말")
	}
	return append(nav, "e 수정 메뉴", "? 도움말", quit)
}

// promptLines 는 질문·입력 상자 줄입니다 (여러 줄 입력·고르기는 화면 높이의 1/3·1/2 까지).
func (u *TUI) promptLines(w, h int) []string {
	color := u.env.Color
	p := u.prompt
	switch p.kind {
	case tpYesNo:
		return []string{tuiSegLine(color, w, tuiSeg{" " + p.title + " [y/n] ", "1;33"})}
	case tpLine:
		lead := " " + p.title + ": "
		return []string{tuiSegLine(color, w, tuiSeg{lead, tuiStBold}, tuiSeg{tuiTail(string(p.buf), w-tuiStrWidth(lead)-1) + "_", ""})}
	case tpLines:
		out := []string{tuiSegLine(color, w, tuiSeg{fmt.Sprintf(" %s  (%d줄 입력됨)", p.title, len(p.lines)), tuiStBold})}
		k := tuiMaxInt(h/3-2, 1)
		from := tuiMaxInt(len(p.lines)-k, 0)
		for _, l := range p.lines[from:] {
			out = append(out, tuiSegLine(color, w, tuiSeg{"   " + l, ""}))
		}
		return append(out, tuiSegLine(color, w, tuiSeg{" > ", tuiStCyan}, tuiSeg{tuiTail(string(p.buf), w-4) + "_", ""}))
	}
	// tpChoose
	t := " " + p.title
	if p.num != "" {
		t += "  번호: " + p.num
	}
	out := []string{tuiSegLine(color, w, tuiSeg{t, tuiStBold})}
	k := tuiMinInt(len(p.items), tuiMaxInt(h/2-1, 3))
	from := tuiMaxInt(tuiMinInt(p.cur-k/2, len(p.items)-k), 0)
	for i := from; i < from+k && i < len(p.items); i++ {
		item := fmt.Sprintf("%2d) %s", i+1, p.items[i])
		switch {
		case i == p.cur && color:
			out = append(out, tuiSegLine(color, w, tuiSeg{"   ", ""}, tuiSeg{item, tuiStRev}))
		case i == p.cur:
			out = append(out, tuiSegLine(color, w, tuiSeg{" > " + item, ""}))
		default:
			out = append(out, tuiSegLine(color, w, tuiSeg{"   " + item, ""}))
		}
	}
	return out
}

// tuiTail 은 s 의 뒤쪽을 표시 폭 w 안으로 남깁니다 (입력 줄이 길면 커서 쪽을 보이게).
func tuiTail(s string, w int) string {
	s = tuiClean(s)
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	used, i := 0, len(r)
	for i > 0 && used+tuiRuneWidth(r[i-1]) <= w {
		used += tuiRuneWidth(r[i-1])
		i--
	}
	return string(r[i:])
}
