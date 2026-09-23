// Package tui 는 실행 중 진행률을 보여줍니다. 실행 60초까지는 한 줄짜리
// "[완료]/[대상] xx%" 진행률만 갱신하고, 60초를 넘으면 터미널을 raw 모드 +
// 대체 화면 버퍼(alternate screen buffer)로 바꿔 전체 대상 목록을 페이지
// 단위로 보여줍니다. ↑/↓ 로 대상 선택, ←/→(또는 PgUp/PgDn) 로 페이지 이동,
// Enter 로 상세 단계 보기, 상세 화면에서 아무 키나 누르면 목록으로 돌아갑니다.
//
// 대상이 1000대 이상이어도 가볍게 돌도록:
//   - 한 번에 화면(페이지)에 들어가는 줄만 그린다(전체 목록을 찍지 않음).
//   - 화면 전체를 지우지 않고 커서를 맨 위로 옮겨 줄 단위로 덮어쓴다.
//   - 직전 프레임과 내용이 같으면 아예 출력하지 않는다.
//   - 갱신 주기는 1초.
//
// 그래서 초당 터미널로 나가는 양은 대상 수와 무관하게 "한 화면 분량" 이하다.
// 대체 화면 버퍼를 쓰므로 스크롤백에도 쌓이지 않고, 끝나면 실행 전 화면으로
// 돌아간 뒤 전체 결과표를 출력한다.
//
// raw 모드 전환에 실패하면(터미널이 아닌 곳으로 리다이렉트된 경우 등) 대화형
// 목록은 건너뛰고 진행률 줄만 계속 갱신합니다.
//
// Ctrl+C 는 5초 안에 3번 눌러야 즉시 종료합니다(되돌리기 없음). 1~2번째는
// 안내만 띄웁니다. raw 모드에서는 Ctrl+C 가 SIGINT 가 아니라 0x03 키 입력으로
// 들어오므로 두 경로를 모두 여기서 센다. 종료 전에 터미널을 원복하고, IP 를
// 바꾸던 도중이던 대상과 수동 복구용 게스트 스크립트 경로를 출력한다.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"vm-ip-change/internal/status"
	"vm-ip-change/internal/target"
	"vm-ip-change/internal/vsphere"
)

const (
	listThreshold = 60 * time.Second
	tickInterval  = time.Second
	headerLines   = 4 // 제목, 요약, 조작법, 안내(평소엔 빈 줄)

	interruptPresses = 3
	interruptWindow  = 5 * time.Second
)

// Run 은 done 이 닫힐 때까지 화면을 그립니다. done 이 닫히면 raw 모드/대체
// 화면 버퍼를 원복하고 최종 결과 표를 출력한 뒤 반환합니다. sigint 로 들어오는
// SIGINT 와 raw 모드의 Ctrl+C 키를 세어 5초 안에 3번이면 os.Exit(130) 합니다.
// main 고루틴에서 워커들을 기다리는 동안 별도 고루틴으로 돌리세요.
func Run(t *status.Tracker, done <-chan struct{}, sigint <-chan os.Signal) {
	inFd := int(os.Stdin.Fd())
	outFd := int(os.Stdout.Fd())
	interactive := false
	var oldState *term.State
	selected := 0
	inDetail := false
	lastFrame := ""

	var presses []time.Time
	notice := ""
	var noticeUntil time.Time

	enterInteractive := func() bool {
		s, err := term.MakeRaw(inFd)
		if err != nil {
			return false
		}
		oldState = s
		// 대체 화면 버퍼 진입 + 한 번만 전체 지우기 + 커서 숨김
		fmt.Print("\x1b[?1049h\x1b[2J\x1b[?25l")
		return true
	}

	restore := func() {
		if oldState != nil {
			fmt.Print("\x1b[?25h\x1b[?1049l") // 커서 표시 + 원래 화면으로 복귀
			_ = term.Restore(inFd, oldState)
			oldState = nil
		}
	}
	defer restore()

	keys := make(chan byte, 32)
	stopReader := make(chan struct{})
	startKeyReader := func() {
		go func() {
			buf := make([]byte, 1)
			for {
				n, err := os.Stdin.Read(buf)
				if err != nil || n == 0 {
					return
				}
				select {
				case keys <- buf[0]:
				case <-stopReader:
					return
				}
			}
		}()
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	triedInteractive := false

	pageSize := func() int {
		_, h, err := term.GetSize(outFd)
		if err != nil || h <= 0 {
			h = 24
		}
		// 마지막 줄은 비워둔다 — 맨 아래 줄에서 줄바꿈하면 화면이 한 줄 밀린다.
		if n := h - headerLines - 1; n > 0 {
			return n
		}
		return 1
	}

	render := func() {
		if !interactive {
			renderProgressLine(t)
			return
		}
		if notice != "" && time.Now().After(noticeUntil) {
			notice = ""
		}
		var lines []string
		if inDetail {
			lines = detailLines(t.VMs[selected], notice)
		} else {
			lines = listLines(t, selected, pageSize(), notice)
		}
		frame := buildFrame(lines)
		if frame == lastFrame {
			return // 바뀐 게 없으면 아무것도 출력하지 않는다
		}
		lastFrame = frame
		fmt.Print(frame)
	}

	onInterrupt := func() {
		now := time.Now()
		kept := presses[:0]
		for _, p := range presses {
			if now.Sub(p) < interruptWindow {
				kept = append(kept, p)
			}
		}
		presses = append(kept, now)

		if len(presses) >= interruptPresses {
			restore()
			fmt.Print("\n")
			renderAbort(t)
			os.Exit(130)
		}

		left := interruptPresses - len(presses)
		notice = fmt.Sprintf("!! Ctrl+C %d/%d — 5초 안에 %d번 더 누르면 즉시 종료합니다(되돌리기 없음)",
			len(presses), interruptPresses, left)
		noticeUntil = now.Add(interruptWindow)
		if !interactive {
			fmt.Printf("\n%s\n", notice)
		}
		render()
	}

	move := func(delta int) {
		selected += delta
		if selected < 0 {
			selected = 0
		}
		if selected > len(t.VMs)-1 {
			selected = len(t.VMs) - 1
		}
	}

	// esc 는 방향키/PgUp/PgDn 이스케이프 시퀀스를 모으는 버퍼입니다.
	// ESC [ A/B/C/D, ESC O A/B/C/D(응용 커서 모드), ESC [ 5 ~, ESC [ 6 ~
	var esc []byte

	for {
		select {
		case <-done:
			close(stopReader)
			restore()
			fmt.Print("\n")
			renderFinal(t)
			return

		case <-ticker.C:
			if !interactive && !triedInteractive && time.Since(t.Start) >= listThreshold {
				triedInteractive = true
				if enterInteractive() {
					interactive = true
					startKeyReader()
				}
			}
			render()

		case <-sigint:
			onInterrupt()

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
				switch b {
				case '\r', '\n':
					inDetail = true
				case 's', 'S':
					if path, err := saveSnapshot(t); err != nil {
						notice = fmt.Sprintf("!! 저장 실패: %v", err)
					} else {
						notice = fmt.Sprintf("저장됨: %s", path)
					}
					noticeUntil = time.Now().Add(interruptWindow)
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
				continue // ESC [ 5 ~ 같은 4바이트 시퀀스 — 다음 바이트 대기
			}

			ps := pageSize()
			switch string(esc[1:]) {
			case "[A", "OA":
				move(-1)
			case "[B", "OB":
				move(1)
			case "[D", "OD", "[5~":
				move(-ps)
			case "[C", "OC", "[6~":
				move(ps)
			}
			esc = nil
			render()
		}
	}
}

// buildFrame 은 커서를 맨 위로 옮긴 뒤 줄마다 "내용 + 줄 끝까지 지우기"로
// 덮어쓰고, 마지막에 그 아래 남은 부분을 지운다. 화면 전체를 지우지 않으므로
// 깜빡이지 않는다. raw 모드라 줄바꿈은 \r\n 으로 명시한다.
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

type counts struct {
	done, running, pending, failed, total int
}

func countAll(t *status.Tracker) counts {
	var c counts
	c.total = len(t.VMs)
	for _, v := range t.VMs {
		s := v.Snapshot()
		switch {
		case s.Outcome == status.OutcomeDone:
			c.done++
		case s.Outcome == status.OutcomeFailed:
			c.done++
			c.failed++
		case s.Phase == status.PhasePending:
			c.pending++
		default:
			c.running++
		}
	}
	return c
}

func renderProgressLine(t *status.Tracker) {
	done, total := t.Counts()
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}
	fmt.Printf("\r진행: %d/%d (%d%%)   ", done, total, pct)
}

func listLines(t *status.Tracker, selected, pageSize int, notice string) []string {
	c := countAll(t)
	pct := 0
	if c.total > 0 {
		pct = c.done * 100 / c.total
	}
	pages := (c.total + pageSize - 1) / pageSize
	if pages == 0 {
		pages = 1
	}
	page := selected / pageSize
	start := page * pageSize
	end := start + pageSize
	if end > c.total {
		end = c.total
	}

	lines := make([]string, 0, headerLines+pageSize)
	lines = append(lines,
		fmt.Sprintf("=== vm-ip-change 진행 중 === 경과 %s", fmtDuration(time.Since(t.Start))),
		fmt.Sprintf("완료 %d/%d (%d%%)  진행중 %d  대기 %d  실패 %d",
			c.done, c.total, pct, c.running, c.pending, c.failed),
		fmt.Sprintf("↑/↓ 선택  ←/→ 또는 PgUp/PgDn 페이지  Enter 상세  s 목록 저장   [페이지 %d/%d]", page+1, pages),
		notice,
	)
	for i := start; i < end; i++ {
		v := t.VMs[i]
		marker := "  "
		if i == selected {
			marker = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%-24s %-12s %s",
			marker, v.Hostname, v.Label(), fmtDuration(v.Snapshot().Elapsed)))
	}
	return lines
}

func detailLines(v *status.VM, notice string) []string {
	snap := v.Snapshot()
	lines := []string{
		fmt.Sprintf("=== %s 상세 ===", v.Hostname),
		fmt.Sprintf("새 IP: %s (GW %s)", v.NewIP, v.Gateway),
		fmt.Sprintf("경과: %s", fmtDuration(snap.Elapsed)),
		"",
	}

	steps := []struct {
		phase status.Phase
		label string
	}{
		{status.PhaseChecking, "연결 확인"},
		{status.PhaseApplying, "IP 설정 적용"},
		{status.PhaseRestarting, "연결 재기동"},
	}
	for _, step := range steps {
		mark := "[ ]"
		switch {
		case snap.Phase > step.phase:
			mark = "[x]"
		case snap.Phase == step.phase:
			if snap.Outcome == status.OutcomeFailed {
				mark = "[!]"
			} else {
				mark = "[>]"
			}
		}
		lines = append(lines, fmt.Sprintf("  %s %s", mark, step.label))
	}

	lines = append(lines, "", fmt.Sprintf("상태: %s", v.Label()))
	if snap.Err != nil {
		lines = append(lines, fmt.Sprintf("오류: %v", snap.Err))
	}
	lines = append(lines, "", "(아무 키나 누르면 목록으로 돌아갑니다)")
	if notice != "" {
		lines = append(lines, "", notice)
	}
	return lines
}

// saveSnapshot 은 목록 화면에서 's' 를 누르면 그 시점의 완료/실패/진행중 전체
// 목록을 파일로 저장합니다(작업 도중 현황을 따로 보관하고 싶을 때 사용).
func saveSnapshot(t *status.Tracker) (string, error) {
	path := fmt.Sprintf("vm-ip-change-snapshot-%s.txt", time.Now().Format("20060102-150405"))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	c := countAll(t)
	fmt.Fprintf(f, "=== vm-ip-change 진행 상황 (%s, 경과 %s) ===\n",
		time.Now().Format("2006-01-02 15:04:05"), fmtDuration(time.Since(t.Start)))
	fmt.Fprintf(f, "완료 %d/%d  진행중 %d  대기 %d  실패 %d\n\n", c.done, c.total, c.running, c.pending, c.failed)
	for _, v := range t.VMs {
		fmt.Fprintf(f, "%-24s %-12s %s\n", v.Hostname, v.Label(), fmtDuration(v.Snapshot().Elapsed))
	}
	return path, nil
}

func renderFinal(t *status.Tracker) {
	fmt.Println("=== 결과 ===")
	failCount := 0
	for _, v := range t.VMs {
		snap := v.Snapshot()
		switch snap.Outcome {
		case status.OutcomeDone:
			fmt.Printf("[OK]   %-24s -> %s (GW %s)\n", v.Hostname, v.NewIP, v.Gateway)
		case status.OutcomeFailed:
			fmt.Printf("[FAIL] %-24s: %v\n", v.Hostname, snap.Err)
			failCount++
		default:
			fmt.Printf("[?]    %-24s: %s\n", v.Hostname, v.Label())
			failCount++
		}
	}
	if failCount > 0 {
		fmt.Printf("\n실패: %d건\n", failCount)
	}
}

// renderAbort 는 Ctrl+C 3번으로 즉시 종료할 때 출력합니다. 끝난 대상은 개수만,
// 도중에 끊긴 대상은 이름과 단계, 수동 복구 방법을 보여줍니다.
func renderAbort(t *status.Tracker) {
	c := countAll(t)
	fmt.Println("=== Ctrl+C 3회: 즉시 종료 (되돌리기 없음) ===")
	fmt.Printf("완료(OK) %d  실패 %d  시작 안 함 %d  도중 중단 %d  / 전체 %d\n",
		c.done-c.failed, c.failed, c.pending, c.running, c.total)
	if c.running == 0 {
		return
	}

	fmt.Println("\n도중에 중단된 대상:")
	for _, v := range t.VMs {
		snap := v.Snapshot()
		if snap.Outcome != status.OutcomeNone || snap.Phase == status.PhasePending {
			continue
		}
		if snap.Phase == status.PhaseChecking {
			fmt.Printf("  %-24s 연결 확인 중 중단 — 설정 변경 전(변경 없음)\n", v.Hostname)
			continue
		}
		fmt.Printf("  %-24s %s 중 중단 — IP 가 바뀌었을 수 있음. 원래 IP 복구: 게스트에서 bash %s\n",
			v.Hostname, snap.Phase.Label(), vsphere.RevertScriptPath(target.SanitizeID(v.Hostname)))
	}
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
