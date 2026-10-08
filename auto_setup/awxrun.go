// awxrun.go - w 키: awx_dir 의 01.AWX_nodeinfo_V2.sh 를 TUI 를 잠시 떠나 터미널에서 직접 실행한다.
// 프로파일(awx_profile_N)이 있으면 "AWX auto 실행? [y/n]" → 번호 선택 후 AWX_AUTO* 환경변수로 자동 응답을 켠다.
//
// 실행 중 처리 (runAwx):
//  1. 키 읽기 goroutine 을 멈춘다. 블로킹 Read 는 중단할 수 없으므로, goroutine 이 매번 InReady(poll/select, 100ms)로
//     읽을 데이터가 있을 때만 Read 하고 poll+Read 구간에서만 inMu 를 잡는다. runAwx 가 inMu 를 Lock 하면
//     늦어도 100ms 안에 goroutine 이 Read 밖으로 나와 멈추고, 자식이 끝날 때까지 stdin 을 건드리지 않는다.
//  2. 대체 화면 종료 + TUI 가 raw 로 바꾸기 전 termios 로 복원 → AwxPrep 으로 cooked + intr=^X + susp 비활성.
//  3. 자식(bash 01...)은 새 프로세스 그룹을 만들지 않으므로 터미널의 foreground 그룹이라 tty 를 읽고 ^X 의 SIGINT 도 받는다.
//     auto_setup 쪽 SIGINT 는 signal.Ignore 가 아니라 기존 Notify 채널로 받아 버린다 — Ignore 하면 SIG_IGN 이 exec 를
//     넘어 자식에게 상속되어 ^X 가 듣지 않기 때문 (Notify 로 잡힌 핸들러는 exec 시 기본 동작으로 돌아간다).
//  4. 종료 후 termios 원복(남은 입력 버림) → raw + 대체 화면 재진입 → 새로 그림.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	awxScript    = "01.AWX_nodeinfo_V2.sh"
	awxIntrRc    = 130
	inPoll       = 100 * time.Millisecond
	awxStartLine = "[auto_setup] AWX 실행 — 취소: Ctrl+X (즉시), 끝나면 auto_setup 화면으로 돌아갑니다"
)

const (
	awxAsk  = iota // AWX auto 실행? [y/n]
	awxPick        // 프로파일 번호 선택
)

type awxState struct {
	step     int
	profiles []AwxProfile
	problems []string
}

// awxExec: bash 01 을 dir 에서 env 로 실행하고 종료 코드를 돌려준다 (SIGINT 로 죽으면 130). 테스트에서 가짜로 교체.
var awxExec = realAwxExec

func realAwxExec(dir string, env []string) (int, error) {
	cmd := exec.Command("bash", awxScript)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal()), nil
		}
		return ee.ExitCode(), nil
	}
	return -1, err
}

// startAwx: w 키
func (st *tuiState) startAwx() {
	if awx_dir == "" {
		st.sel.Msg = "awx_dir 가 비었거나 " + awxScript + " 가 없습니다 (/etc/auto_setup/auto_setup.conf)"
		return
	}
	if fi, err := os.Stat(filepath.Join(awx_dir, awxScript)); err != nil || fi.IsDir() {
		st.sel.Msg = "awx_dir 가 비었거나 " + awxScript + " 가 없습니다 (/etc/auto_setup/auto_setup.conf)"
		return
	}
	valid, problems := parseAwxProfiles()
	if len(valid) == 0 {
		st.oo = st.newUserStep(true, nil)
		return
	}
	st.awx = &awxState{step: awxAsk, profiles: valid, problems: problems}
}

func (st *tuiState) handleAwx(ev keyEv) {
	a := st.awx
	switch a.step {
	case awxAsk:
		switch {
		case ev.k == kEsc:
			st.awx = nil
			st.sel.Msg = "취소했습니다"
		case ev.k == kRune && (ev.r == 'y' || ev.r == 'Y'):
			a.step = awxPick
		case ev.k == kRune && (ev.r == 'n' || ev.r == 'N'):
			st.awx = nil
			st.oo = st.newUserStep(true, nil)
		}
	case awxPick:
		switch {
		case ev.k == kEsc:
			a.step = awxAsk
		case ev.k == kRune && ev.r >= '1' && ev.r <= '9':
			for i := range a.profiles {
				if a.profiles[i].No == int(ev.r-'0') {
					p := a.profiles[i]
					st.awx = nil
					st.oo = st.newUserStep(true, &p)
					return
				}
			}
		}
	}
}

// awxEnv: 기본 환경 + (auto 프로파일이면) AWX_AUTO 변수
func awxEnv(p *AwxProfile, user, verify string) []string {
	env := os.Environ()
	if p != nil {
		env = append(env, "AWX_AUTO=1", "AWX_AUTO_NODEINFO="+p.Nodeinfo, "AWX_AUTO_OS="+p.OS)
	}
	if user != "" {
		env = append(env, "AWX_USER="+user) // 01 이 user 메뉴를 건너뜀
	}
	if verify != "" {
		env = append(env, "AWX_VERIFY_FILE="+verify) // 01 의 "등록 후 확인" 에 자동 입력
	}
	return env
}

// runAwx: TUI 를 떠나 01 을 실행하고 돌아온다 (패키지 주석 참고)
func (st *tuiState) runAwx(p *AwxProfile, user, verify string) {
	out := st.env.Out
	st.inMu.Lock() // 키 읽기 goroutine 정지 (자식이 끝날 때까지)
	io.WriteString(out, leaveScreen)
	st.restore()
	endPrep := func() {}
	if st.env.AwxPrep != nil {
		if r, err := st.env.AwxPrep(); err == nil {
			endPrep = r
		}
	}
	fmt.Fprint(out, awxStartLine+"\n")
	rc, err := awxExec(awx_dir, awxEnv(p, user, verify))
	endPrep()
	// 자식이 받은 ^X 의 SIGINT 가 Notify 채널에 남아 있으면 TUI 가 종료되므로 비운다
	if st.env.Signals != nil {
		for drained := false; !drained; {
			select {
			case <-st.env.Signals:
			default:
				drained = true
			}
		}
	}
	msg := ""
	if r, rerr := st.env.MakeRaw(); rerr == nil {
		st.restore = r
	} else {
		st.restore = func() {}
		msg = "[X] 터미널을 raw 모드로 되돌리지 못했습니다: " + rerr.Error() + " — q 로 종료하세요. "
	}
	io.WriteString(out, enterScreen)
	st.inMu.Unlock()
	switch {
	case err != nil:
		msg += "[X] AWX 실행 실패: " + err.Error()
	case rc == awxIntrRc:
		msg += "AWX 취소됨 (Ctrl+X)"
	default:
		msg += fmt.Sprintf("AWX 종료 (rc=%d)", rc)
	}
	st.refresh()
	st.sel.Msg = msg
}

// ---- 화면 ----

func renderAwx(a *awxState, width, height int, color bool) string {
	width = clampWidth(width)
	var title, hint string
	var body []seg
	switch a.step {
	case awxAsk:
		title = "AWX 실행"
		body = []seg{{" AWX auto 실행? [y/n]", ""}, {"", ""},
			{" yml 마다 OS 버전이 다르면 N 을 입력하세요", stYellow}}
		hint = " y auto 프로파일 선택  n 일반 실행 (01 의 질문에 직접 응답)  Esc 취소"
	case awxPick:
		title = "AWX 실행 - 프로파일 선택"
		for _, p := range a.profiles {
			l := fmt.Sprintf(" %d) nodeinfo=%s  OS=%s", p.No, p.Nodeinfo, p.OS)
			if p.Desc != "" {
				l += "  " + p.Desc
			}
			body = append(body, seg{l, ""})
		}
		if len(a.problems) > 0 {
			body = append(body, seg{"", ""}, seg{" 사용할 수 없는 프로파일:", stGray})
			for _, pr := range a.problems {
				body = append(body, seg{"   " + pr, stGray})
			}
		}
		hint = " 번호를 누르면 바로 실행합니다  Esc 뒤로"
	}
	out := []string{segLine(color, width, redSegs(" "+title, stBold)...), sepLine(width)}
	for _, s := range body {
		out = append(out, segLine(color, width, s))
	}
	foot := []string{sepLine(width), segLine(color, width, hintSegs(hint)...)}
	if height > 0 {
		for len(out) < height-len(foot) {
			out = append(out, "")
		}
		if len(out) > height-len(foot) {
			out = out[:rMax(height-len(foot), 1)]
		}
	}
	return strings.Join(append(out, foot...), "\n")
}
