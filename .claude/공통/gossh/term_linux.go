//go:build linux

package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// 프로그램이 처음 만든 표준입력 리더. -h 예시(withInput)가 가짜 입력으로 바꿔 끼운 동안에는
// readConfirmTTY 가 실제 터미널을 읽지 않게 이 값과 비교한다.
var origStdinReader = stdinReader

// confirmPasteWait 는 빈 줄을 받은 뒤 붙여넣기의 다음 조각이 오는지 기다리는 시간(ms)이다.
// 브래킷 붙여넣기를 지원하지 않는 터미널에서 목록 안의 빈 줄로 입력이 끝나지 않게 하는 보조 장치다.
const confirmPasteWait = 200

// readConfirmTTY 는 위험 작업 확인의 호스트 재입력을 터미널에서 직접 읽는다(줄 조립은 confirmLineEditor).
//   - 줄 버퍼링(ICANON)을 끄므로 한 줄 4095자 제한이 없고, CR→LF 변환(ICRNL)을 꺼서 CRLF 를 줄바꿈 하나로 본다.
//   - 시그널 키(ISIG)도 꺼서 Ctrl+C·Ctrl+\ 는 바이트로 받아 터미널을 되돌린 뒤 취소하고, Ctrl+Z 는 무시한다.
//   - 브래킷 붙여넣기를 켜서 붙여 넣은 목록 안의 빈 줄은 무시한다. 빈 줄 뒤 입력이 없거나 한 바이트(키 입력)만
//     오면 입력을 끝내고, 여러 바이트가 한꺼번에 오면(붙여넣기의 다음 조각) 계속 읽는다.
//   - 끝날 때 남은 입력을 버린다(TCSETSF). 일찍 끝나도 남은 붙여넣기가 y/N 답이나 셸 명령으로 들어가지 않는다.
//
// 표준입력이 터미널이 아니거나 리더가 바뀌었거나 이미 버퍼에 입력이 있으면 ok=false(호출한 쪽이 readConfirmLines 사용).
func readConfirmTTY() (lines []string, ok bool) {
	if stdinReader != origStdinReader || stdinReader.Buffered() > 0 {
		return nil, false
	}
	fd := int(os.Stdin.Fd())
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, false
	}
	t := *old
	t.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG
	t.Iflag &^= unix.ICRNL | unix.INLCR | unix.IGNCR
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &t); err != nil {
		return nil, false
	}
	// 에코는 표준에러가 아니라 터미널(fd 0)에 직접 쓴다(2> 로 돌려도 입력이 보이게). 못 쓰면 표준에러.
	toTTY := true
	if _, err := unix.Write(fd, []byte("\x1b[?2004h")); err != nil { // 브래킷 붙여넣기 켜기
		toTTY = false
	}
	echo := func(s string) {
		if s == "" {
			return
		}
		if !toTTY {
			fmt.Fprint(os.Stderr, s)
		} else if _, err := unix.Write(fd, []byte(s)); err != nil {
			toTTY = false
			fmt.Fprint(os.Stderr, s)
		}
	}
	var restoreOnce sync.Once
	restore := func() {
		restoreOnce.Do(func() {
			if toTTY {
				_, _ = unix.Write(fd, []byte("\x1b[?2004l"))
			}
			_ = unix.IoctlSetTermios(fd, unix.TCSETSF, old) // 되돌리면서 남은 입력은 버린다
		})
	}
	defer restore()
	cancel := func(code int) {
		restore()
		fmt.Fprintln(os.Stderr, "\n작업이 취소되었습니다.")
		os.Exit(code)
	}

	// kill 등 밖에서 온 시그널도 터미널을 되돌린 뒤 끝낸다(이 단계에서는 아무것도 실행하지 않았다).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	defer func() { signal.Stop(sigCh); close(done) }()
	go func() {
		select {
		case s := <-sigCh:
			code := 130
			if s == syscall.SIGTERM {
				code = 143
			} else if s == syscall.SIGHUP {
				code = 129
			}
			cancel(code)
		case <-done:
		}
	}()

	var ed confirmLineEditor
	buf := make([]byte, 4096)
	pendingBlank := false // 마지막 사건이 빈 줄이고 그 뒤로 내용 있는 입력이 없었다
	for {
		n, err := unix.Read(fd, buf)
		if err == unix.EINTR {
			continue
		}
		if n <= 0 || err != nil {
			return ed.finish(), true
		}
		var out strings.Builder
		for i := 0; i < n; i++ {
			e, ev := ed.feed(buf[i])
			out.WriteString(e)
			switch {
			case ev == confirmIntr:
				echo(out.String())
				cancel(130)
			case ev == confirmEOF:
				echo(out.String())
				return ed.finish(), true
			case ev == confirmBlank:
				pendingBlank = true
			case e != "":
				pendingBlank = false
			}
		}
		echo(out.String())
		if pendingBlank && !ed.inPaste && !pasteContinues(fd, confirmPasteWait) {
			return ed.finish(), true
		}
	}
}

// pasteContinues 는 ms 안에 여러 바이트가 한꺼번에 들어오는지(붙여넣기의 다음 조각인지) 본다.
// 한 바이트만 왔으면 사람이 친 키(y/N 을 미리 친 y 등)로 보고 false.
func pasteContinues(fd, ms int) bool {
	for {
		n, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, ms)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n <= 0 {
			return false
		}
		avail, err := unix.IoctlGetInt(fd, unix.TIOCINQ) // 읽을 수 있는 바이트 수(FIONREAD)
		return err == nil && avail > 1
	}
}

// terminalSize는 fd가 가리키는 터미널의 가로/세로 크기를 돌려준다(모르면 0, 0).
func terminalSize(fd int) (cols, rows int) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0
	}
	return int(ws.Col), int(ws.Row)
}

// enableCbreak은 입력 줄 버퍼링(ICANON)과 에코(ECHO)만 끈다. Enter 한 번을 바로 읽을 수 있고
// 누른 키가 화면에 찍히지 않는다. 출력 처리(\n -> \r\n)와 Ctrl+C 시그널은 그대로라서 기존
// 출력/Ctrl+C 동작은 바뀌지 않는다(완전한 raw 모드를 쓰면 줄바꿈이 계단식으로 깨진다).
func enableCbreak(fd int) (restore func(), err error) {
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	t := *old
	t.Lflag &^= unix.ICANON | unix.ECHO
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &t); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, unix.TCSETS, old) }, nil
}

// enableRaw는 -i 대화형 접속용으로 터미널을 완전한 raw 모드로 바꾼다(cfmakeraw 와 같음). 키 입력을
// 한 글자씩 그대로 원격에 넘기고, 에코·줄바꿈 변환·Ctrl+C 시그널은 원격 터미널이 처리한다.
func enableRaw(fd int) (restore func(), err error) {
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, err
	}
	t := *old
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &t); err != nil {
		return nil, err
	}
	return func() { _ = unix.IoctlSetTermios(fd, unix.TCSETS, old) }, nil
}

// watchResize는 터미널 창 크기가 바뀔 때마다(SIGWINCH) fn 을 부른다. 돌려준 함수로 감시를 멈춘다.
func watchResize(fd int, fn func(cols, rows int)) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				if cols, rows := terminalSize(fd); cols > 0 && rows > 0 {
					fn(cols, rows)
				}
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(ch); close(done) }
}

// promptPassword는 prompt 를 표준에러에 찍고, 터미널이면 에코를 끈 채 한 줄을 읽는다(ssh 비밀번호 입력과 같음).
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	if old, err := unix.IoctlGetTermios(fd, unix.TCGETS); err == nil {
		t := *old
		t.Lflag &^= unix.ECHO
		t.Lflag |= unix.ICANON | unix.ISIG
		if err := unix.IoctlSetTermios(fd, unix.TCSETS, &t); err == nil {
			defer func() {
				_ = unix.IoctlSetTermios(fd, unix.TCSETS, old)
				fmt.Fprintln(os.Stderr)
			}()
		}
	}
	s, err := stdinReader.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimRight(s, "\r\n"), nil
}
