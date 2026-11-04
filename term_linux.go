//go:build linux

package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

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
