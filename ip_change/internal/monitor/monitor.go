// Package monitor 는 엔진이 gossh 를 돌리는 동안 대상 호스트별 진행 상황을
// 보여주고, Ctrl+C 를 5초 안에 3번 눌러야 종료되게 합니다.
//
// 엔진의 표준출력은 통합 스크립트(lib/stages.sh)가 파일로 받아 파싱하므로,
// 화면은 표준출력이 아니라 제어 터미널(/dev/tty)에 직접 그립니다. 그래서
// 파싱 계약(엔진 출력 형식)은 전혀 바뀌지 않습니다.
//
// 동작:
//   - 시작 후 60초까지는 아무것도 그리지 않습니다(기존 동작 그대로).
//   - 60초가 지나도 안 끝나면 /dev/tty 를 raw 모드 + 대체 화면 버퍼로 바꿔 전체
//     대상을 페이지 단위로 보여줍니다. ↑/↓ 선택(페이지 경계 넘어감), ←/→ 또는
//     PgUp/PgDn 페이지 이동, Enter 로 그 호스트의 출력 보기, 아무 키나 누르면
//     목록으로 돌아갑니다. 끝나면 원래 화면으로 돌아갑니다(스크롤백 오염 없음).
//   - 1000대 이상이어도 가볍도록 보이는 페이지만 그리고, 화면 전체를 지우지 않고
//     줄 단위로 덮어쓰며, 직전 프레임과 같으면 출력하지 않습니다.
//   - 제어 터미널이 없으면(cron, nohup 등) 화면 없이 조용히 동작합니다.
//
// gossh 는 호스트 한 대가 끝나야 그 호스트의 출력을 한꺼번에 내보내므로, 끝나지
// 않은 호스트는 "진행 중"과 "동시접속 제한으로 대기 중"을 구분할 수 없습니다.
// 화면에는 둘을 합쳐 "진행/대기" 로 표시합니다.
//
// Ctrl+C: 5초 안에 3번 눌러야 즉시 종료합니다(되돌리기 없음, 종료코드 130).
// 1~2번째는 안내만 합니다. raw 모드에서는 Ctrl+C 가 시그널이 아니라 0x03 키로
// 들어오므로 두 경로를 함께 셉니다. 종료 시 gossh 를 끝내고 터미널을 원복한 뒤
// 끝난/안 끝난 호스트 요약을 터미널과 표준출력(로그) 양쪽에 남깁니다.
//
// 이 파일은 ip_change 와 ldap_setting 에 똑같이 들어 있습니다(패키지 경로만 다름).
package monitor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	listThreshold    = 60 * time.Second
	tickInterval     = time.Second
	headerLines      = 4 // 제목, 요약, 조작법, 안내(평소엔 빈 줄)
	interruptPresses = 3
	interruptWindow  = 5 * time.Second
	pendingLabel     = "진행/대기"
)

// Label 은 호스트 한 대의 출력 줄로 화면에 보일 상태와 실패 여부를 정합니다.
type Label func(lines []string) (label string, failed bool)

type hostState struct {
	name     string
	lines    []string // gossh 표준출력(원격 명령 출력)
	errs     []string // gossh 표준에러 중 이 호스트 줄(접속 실패 등)
	finished time.Time
}

// Monitor 는 한 번의 엔진 실행 동안 대상 호스트 상태를 모읍니다.
type Monitor struct {
	title string
	label Label
	out   io.Writer
	start time.Time

	mu    sync.Mutex
	hosts []*hostState
	idx   map[string]*hostState
	procs map[int]struct{}

	stop    chan struct{}
	stopped chan struct{}
	sigCh   chan os.Signal
	once    sync.Once
	now     func() time.Time
}

// New 는 대상 호스트 목록으로 모니터를 만듭니다. out 에는 Ctrl+C 로 중단할 때
// 요약이 기록됩니다(보통 엔진 표준출력 — 통합 스크립트의 로그에 남음).
func New(title string, hosts []string, label Label, out io.Writer) *Monitor {
	m := &Monitor{
		title: title,
		label: label,
		out:   out,
		start: time.Now(),
		idx:   make(map[string]*hostState, len(hosts)),
		procs: map[int]struct{}{},
		stop:  make(chan struct{}),
		now:   time.Now,
	}
	for _, h := range hosts {
		if _, dup := m.idx[h]; dup {
			continue
		}
		s := &hostState{name: h}
		m.hosts = append(m.hosts, s)
		m.idx[h] = s
	}
	return m
}

// Start 는 Ctrl+C 처리와 화면 갱신을 시작합니다.
func (m *Monitor) Start() {
	m.sigCh = make(chan os.Signal, 8)
	signal.Notify(m.sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	m.stopped = make(chan struct{})
	go m.run()
}

// Stop 은 화면을 원래대로 돌려놓고 시그널 처리를 해제합니다. 여러 번 불러도 됩니다.
func (m *Monitor) Stop() {
	if m == nil {
		return
	}
	m.once.Do(func() {
		close(m.stop)
		if m.stopped != nil {
			<-m.stopped
		}
		if m.sigCh != nil {
			signal.Stop(m.sigCh)
		}
	})
}

// HostLine 은 gossh 표준출력의 "<host>: <내용>" 한 줄을 기록합니다.
// gossh 는 호스트가 끝나야 출력을 내보내므로 줄이 들어온 호스트는 끝난 것으로 봅니다.
func (m *Monitor) HostLine(host, body string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.idx[host]; ok {
		s.lines = append(s.lines, body)
		s.finished = m.now()
	}
}

// HostError 는 gossh 표준에러의 "<host>: <내용>" 한 줄을 기록합니다.
// "ERROR" 가 들어간 줄(접속 실패, 종료코드 등)만 그 호스트를 끝난 것으로 봅니다.
func (m *Monitor) HostError(host, body string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.idx[host]; ok {
		s.errs = append(s.errs, body)
		if strings.Contains(body, "ERROR") {
			s.finished = m.now()
		}
	}
}

// Prepare 는 gossh 를 별도 프로세스 그룹으로 띄우게 합니다. 그래야 터미널에서
// 누른 Ctrl+C 가 gossh 에 직접 가지 않고(gossh 자체의 Ctrl+C 동작 — 두 번이면
// 진행 중 호스트 취소 — 이 섞이지 않게) 이 모니터만 셉니다.
func (m *Monitor) Prepare(cmd *exec.Cmd) {
	if m == nil {
		return
	}
	setProcessGroup(cmd)
}

// Track / Untrack 은 Ctrl+C 로 중단할 때 끝낼 gossh 프로세스를 등록/해제합니다.
func (m *Monitor) Track(pid int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.procs[pid] = struct{}{}
	m.mu.Unlock()
}

func (m *Monitor) Untrack(pid int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.procs, pid)
	m.mu.Unlock()
}

func (m *Monitor) killProcs() {
	m.mu.Lock()
	pids := make([]int, 0, len(m.procs))
	for p := range m.procs {
		pids = append(pids, p)
	}
	m.mu.Unlock()
	for _, p := range pids {
		killProcessGroup(p)
	}
}

// ── 상태 요약 ───────────────────────────────────────────────────────────────

type row struct {
	name    string
	label   string
	failed  bool
	done    bool
	elapsed time.Duration
	lines   []string
	errs    []string
}

type counts struct{ done, failed, pending, total int }

func (m *Monitor) snapshot() ([]row, counts) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	rows := make([]row, len(m.hosts))
	var c counts
	c.total = len(m.hosts)
	for i, s := range m.hosts {
		r := row{name: s.name, lines: s.lines, errs: s.errs}
		switch {
		case s.finished.IsZero():
			r.label = pendingLabel
			r.elapsed = now.Sub(m.start)
			c.pending++
		default:
			r.done = true
			r.elapsed = s.finished.Sub(m.start)
			if len(s.lines) > 0 {
				r.label, r.failed = m.label(s.lines)
			} else {
				r.label, r.failed = "실패(접속)", true
			}
			c.done++
			if r.failed {
				c.failed++
			}
		}
		rows[i] = r
	}
	return rows, c
}

// ── 화면 ────────────────────────────────────────────────────────────────────

type screen struct {
	tty      *os.File
	restore  func()
	lastSeen string
}

func (m *Monitor) run() {
	defer close(m.stopped)

	var ui *screen
	tried := false
	selected, inDetail := 0, false
	var presses []time.Time
	notice := ""
	var noticeUntil time.Time
	keys := make(chan byte, 64)
	var esc []byte

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	closeUI := func() {
		if ui != nil {
			ui.tty.WriteString("\x1b[?25h\x1b[?1049l") // 커서 표시 + 원래 화면 복귀
			ui.restore()
			ui.tty.Close()
			ui = nil
		}
	}
	defer closeUI()

	render := func() {
		if ui == nil {
			return
		}
		if notice != "" && m.now().After(noticeUntil) {
			notice = ""
		}
		rows, c := m.snapshot()
		if selected > len(rows)-1 {
			selected = len(rows) - 1
		}
		if selected < 0 {
			selected = 0
		}
		h := ttyRows(ui.tty)
		var lines []string
		if inDetail && len(rows) > 0 {
			lines = detailLines(m.title, rows[selected], h, notice)
		} else {
			lines = listLines(m.title, rows, c, selected, pageSize(h), m.now().Sub(m.start), notice)
		}
		frame := buildFrame(lines)
		if frame == ui.lastSeen {
			return
		}
		ui.lastSeen = frame
		ui.tty.WriteString(frame)
	}

	onInterrupt := func() {
		now := m.now()
		kept := presses[:0]
		for _, p := range presses {
			if now.Sub(p) < interruptWindow {
				kept = append(kept, p)
			}
		}
		presses = append(kept, now)
		if len(presses) >= interruptPresses {
			closeUI()
			m.abort(130, "Ctrl+C 3회: 즉시 종료 (되돌리기 없음)")
		}
		notice = fmt.Sprintf("!! Ctrl+C %d/%d — 5초 안에 %d번 더 누르면 즉시 종료합니다(되돌리기 없음)",
			len(presses), interruptPresses, interruptPresses-len(presses))
		noticeUntil = now.Add(interruptWindow)
		if ui == nil {
			writeTTY("\r\n" + notice + "\r\n")
		}
		render()
	}

	for {
		select {
		case <-m.stop:
			return

		case <-ticker.C:
			if ui == nil && !tried && m.now().Sub(m.start) >= listThreshold {
				tried = true
				ui = openScreen()
				if ui != nil {
					go readKeys(ui.tty, keys)
				}
			}
			render()

		case s := <-m.sigCh:
			switch s {
			case syscall.SIGINT:
				onInterrupt()
			case syscall.SIGHUP:
				closeUI()
				m.abort(129, "SIGHUP(터미널 끊김): 종료")
			default:
				closeUI()
				m.abort(143, "SIGTERM: 종료")
			}

		case b := <-keys:
			if b == 0x03 { // raw 모드의 Ctrl+C
				esc = nil
				onInterrupt()
				continue
			}
			if inDetail {
				inDetail = false
				esc = nil
				render()
				continue
			}
			if len(esc) == 0 && b != 0x1b {
				if b == '\r' || b == '\n' {
					inDetail = true
				}
				render()
				continue
			}
			esc = append(esc, b)
			if len(esc) < 3 {
				if len(esc) == 2 && esc[1] != '[' && esc[1] != 'O' {
					esc = nil
				}
				continue
			}
			if esc[2] >= '0' && esc[2] <= '9' && len(esc) < 4 {
				continue // ESC [ 5 ~ 같은 4바이트 시퀀스
			}
			ps := pageSize(ttyRows(ui.tty))
			switch string(esc[1:]) {
			case "[A", "OA":
				selected--
			case "[B", "OB":
				selected++
			case "[D", "OD", "[5~":
				selected -= ps
			case "[C", "OC", "[6~":
				selected += ps
			}
			esc = nil
			render()
		}
	}
}

// abort 는 gossh 를 끝내고 요약을 남긴 뒤 프로세스를 종료합니다.
func (m *Monitor) abort(code int, reason string) {
	m.killProcs()
	summary := m.abortSummary(reason)
	writeTTY("\r\n" + strings.ReplaceAll(summary, "\n", "\r\n"))
	if m.out != nil {
		fmt.Fprint(m.out, "\n"+summary)
	}
	os.Exit(code)
}

func (m *Monitor) abortSummary(reason string) string {
	rows, c := m.snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s — %s ===\n", m.title, reason)
	fmt.Fprintf(&b, "완료 %d (그중 실패 %d)  안 끝남 %d  / 전체 %d\n",
		c.done, c.failed, c.pending, c.total)
	if c.pending > 0 {
		b.WriteString("안 끝난 호스트 (변경이 일부 적용됐을 수 있음 — 직접 확인 필요):\n")
		for _, r := range rows {
			if !r.done {
				fmt.Fprintf(&b, "  %s\n", r.name)
			}
		}
	}
	for _, r := range rows {
		if r.done {
			fmt.Fprintf(&b, "  %s %s\n", padRight(r.name, 24), r.label)
		}
	}
	return b.String()
}

func openScreen() *screen {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	restore, err := makeRaw(tty)
	if err != nil {
		tty.Close()
		return nil
	}
	// 대체 화면 버퍼 진입 + 한 번만 전체 지우기 + 커서 숨김
	tty.WriteString("\x1b[?1049h\x1b[2J\x1b[?25l")
	return &screen{tty: tty, restore: restore}
}

func readKeys(tty *os.File, keys chan<- byte) {
	buf := make([]byte, 1)
	for {
		n, err := tty.Read(buf)
		if err != nil || n == 0 {
			return
		}
		keys <- buf[0]
	}
}

// writeTTY 는 제어 터미널이 있으면 거기에 안내를 씁니다(없으면 조용히 무시).
func writeTTY(s string) {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return
	}
	tty.WriteString(s)
	tty.Close()
}

func pageSize(termRows int) int {
	// 마지막 줄은 비워둔다 — 맨 아래 줄에서 줄바꿈하면 화면이 한 줄 밀린다.
	if n := termRows - headerLines - 1; n > 0 {
		return n
	}
	return 1
}

// buildFrame 은 커서를 맨 위로 옮긴 뒤 줄마다 "내용 + 줄 끝까지 지우기"로 덮어쓰고,
// 마지막에 그 아래를 지운다. 화면 전체를 지우지 않으므로 깜빡이지 않는다.
// raw 모드라 줄바꿈은 \r\n 으로 명시한다.
func buildFrame(lines []string) string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, l := range lines {
		b.WriteString(l)
		b.WriteString("\x1b[K")
		if i < len(lines)-1 {
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\x1b[J")
	return b.String()
}

func listLines(title string, rows []row, c counts, selected, ps int, elapsed time.Duration, notice string) []string {
	pct := 0
	if c.total > 0 {
		pct = c.done * 100 / c.total
	}
	pages := (c.total + ps - 1) / ps
	if pages == 0 {
		pages = 1
	}
	page := 0
	if selected > 0 {
		page = selected / ps
	}
	start := page * ps
	end := start + ps
	if end > len(rows) {
		end = len(rows)
	}

	lines := make([]string, 0, headerLines+ps)
	lines = append(lines,
		fmt.Sprintf("=== %s 진행 중 === 경과 %s", title, fmtDuration(elapsed)),
		fmt.Sprintf("완료 %d/%d (%d%%)  진행/대기 %d  실패 %d", c.done, c.total, pct, c.pending, c.failed),
		fmt.Sprintf("↑/↓ 선택  ←/→ 또는 PgUp/PgDn 페이지  Enter 상세  Ctrl+C 3회(5초 안) 종료   [페이지 %d/%d]", page+1, pages),
		notice,
	)
	for i := start; i < end; i++ {
		r := rows[i]
		marker := "  "
		if i == selected {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s %s %s", marker, padRight(r.name, 24), padRight(r.label, 16), fmtDuration(r.elapsed)))
	}
	return lines
}

// padRight 는 화면 폭 기준으로 오른쪽을 공백으로 채웁니다(한글 등은 2칸으로 셈).
func padRight(s string, width int) string {
	w := 0
	for _, r := range s {
		if r >= 0x1100 {
			w += 2
		} else {
			w++
		}
	}
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func detailLines(title string, r row, termRows int, notice string) []string {
	lines := []string{
		fmt.Sprintf("=== %s — %s ===", title, r.name),
		fmt.Sprintf("상태: %s   경과: %s", r.label, fmtDuration(r.elapsed)),
		"",
	}
	var body []string
	if !r.done {
		body = append(body, "아직 결과가 오지 않았습니다(gossh 는 호스트가 끝나야 출력을 보냅니다).")
	}
	for _, l := range r.errs {
		body = append(body, "[stderr] "+l)
	}
	body = append(body, r.lines...)
	// 화면에 들어가는 만큼만(뒤쪽 = 최근 줄 우선)
	room := termRows - len(lines) - 4
	if room < 1 {
		room = 1
	}
	if len(body) > room {
		body = append([]string{fmt.Sprintf("... (앞 %d줄 생략)", len(body)-room+1)}, body[len(body)-room+1:]...)
	}
	lines = append(lines, body...)
	lines = append(lines, "", "(아무 키나 누르면 목록으로 돌아갑니다)")
	if notice != "" {
		lines = append(lines, notice)
	}
	return lines
}

func fmtDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	mi := d / time.Minute
	d -= mi * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, mi, s)
}
