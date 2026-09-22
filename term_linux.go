//go:build linux

package main

import "golang.org/x/sys/unix"

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
