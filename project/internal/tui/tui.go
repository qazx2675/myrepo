// Package tui 는 실행 중 진행률을 보여줍니다. 실행 60초까지는 한 줄짜리
// "[완료]/[대상] xx%" 진행률만 갱신하고, 60초를 넘으면 터미널을 raw 모드로 바꿔
// 전체 대상 목록을 화면에 그리고 화살표(↑/↓)로 고른 뒤 Enter 로 상세 단계를
// 봅니다. 아무 키나 누르면 목록으로 돌아갑니다.
//
// raw 모드 전환에 실패하면(터미널이 아닌 곳으로 리다이렉트된 경우 등) 대화형
// 목록은 건너뛰고 진행률 줄만 계속 갱신합니다 — 로그 파일로 리다이렉트해서
// 돌리는 경우에도 죽지 않게 하기 위함입니다.
package tui

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"

	"vm-ip-change/internal/status"
)

const listThreshold = 60 * time.Second

// Run 은 done 이 닫힐 때까지 화면을 그립니다. done 이 닫히면 raw 모드를 원복하고
// 최종 결과 표를 출력한 뒤 반환합니다. main 고루틴에서 워커들을 기다리는 동안
// 별도 고루틴으로 돌리세요.
func Run(t *status.Tracker, done <-chan struct{}) {
	fd := int(os.Stdin.Fd())
	interactive := false
	var oldState *term.State
	selected := 0
	inDetail := false

	restore := func() {
		if oldState != nil {
			_ = term.Restore(fd, oldState)
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

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	triedInteractive := false

	// esc 는 방향키(ESC [ A/B)를 판별하기 위한 미완성 이스케이프 시퀀스 버퍼입니다.
	var esc []byte

	render := func() {
		if !inDetail {
			renderList(t, interactive, selected)
			return
		}
		renderDetail(t.VMs[selected])
	}

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
				if s, err := term.MakeRaw(fd); err == nil {
					oldState = s
					interactive = true
					startKeyReader()
				}
				// MakeRaw 실패(TTY 아님, 리다이렉트 등) 시 대화형 목록 없이
				// 한 줄 진행률만 계속 갱신한다. 다시 시도하지 않는다.
			}
			render()

		case b := <-keys:
			if inDetail {
				// 상세 화면에서는 어떤 키를 눌러도 목록으로 돌아갑니다.
				inDetail = false
				esc = nil
				render()
				continue
			}

			esc = append(esc, b)
			switch {
			case len(esc) == 1 && esc[0] == 0x1b:
				// ESC 시작 — 다음 바이트를 더 기다림.
				continue
			case len(esc) == 2 && esc[0] == 0x1b && esc[1] == '[':
				continue
			case len(esc) == 3 && esc[0] == 0x1b && esc[1] == '[':
				switch esc[2] {
				case 'A': // ↑
					if selected > 0 {
						selected--
					}
				case 'B': // ↓
					if selected < len(t.VMs)-1 {
						selected++
					}
				}
				esc = nil
			case len(esc) == 1 && (esc[0] == '\r' || esc[0] == '\n'):
				inDetail = true
				esc = nil
			default:
				esc = nil
			}
			render()
		}
	}
}

func renderList(t *status.Tracker, interactive bool, selected int) {
	done, total := t.Counts()
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}

	if !interactive {
		fmt.Printf("\r진행: %d/%d (%d%%)   ", done, total, pct)
		return
	}

	fmt.Print("\x1b[H\x1b[2J")
	fmt.Printf("=== vm-ip-change 진행 중 === 경과 %s\n", fmtDuration(time.Since(t.Start)))
	fmt.Printf("완료 %d/%d (%d%%)\n\n", done, total, pct)
	fmt.Println("  ↑/↓ 로 선택, Enter 로 상세보기")
	fmt.Println()
	for i, v := range t.VMs {
		snap := v.Snapshot()
		marker := "  "
		if i == selected {
			marker = "> "
		}
		fmt.Printf("%s%-24s %-12s %s\n", marker, v.Hostname, v.Label(), fmtDuration(snap.Elapsed))
	}
}

func renderDetail(v *status.VM) {
	snap := v.Snapshot()

	fmt.Print("\x1b[H\x1b[2J")
	fmt.Printf("=== %s 상세 ===\n", v.Hostname)
	fmt.Printf("새 IP: %s (GW %s)\n", v.NewIP, v.Gateway)
	fmt.Printf("경과: %s\n\n", fmtDuration(snap.Elapsed))

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
			mark = "[x]" // 이 단계는 이미 지나갔다(성공/실패와 무관하게 시도는 끝남)
		case snap.Phase == step.phase:
			if snap.Outcome == status.OutcomeFailed {
				mark = "[!]" // 바로 이 단계에서 실패했다
			} else {
				mark = "[>]" // 지금 이 단계 진행 중
			}
		}
		fmt.Printf("  %s %s\n", mark, step.label)
	}

	fmt.Printf("\n상태: %s\n", v.Label())
	if snap.Err != nil {
		fmt.Printf("오류: %v\n", snap.Err)
	}
	fmt.Println("\n(아무 키나 누르면 목록으로 돌아갑니다)")
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
		case status.OutcomeCancelled:
			fmt.Printf("[취소] %-24s: 설정 변경 전에 취소됨\n", v.Hostname)
			failCount++
		case status.OutcomeRolledBack:
			fmt.Printf("[롤백] %-24s: Ctrl+C 취소로 원래 설정으로 되돌림\n", v.Hostname)
			failCount++
		default:
			fmt.Printf("[?]    %-24s: %s\n", v.Hostname, v.Label())
			failCount++
		}
	}
	if failCount > 0 {
		fmt.Printf("\n실패/취소/롤백: %d건\n", failCount)
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
