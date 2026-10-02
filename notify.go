// notify.go - wall 알림 (root 로 PATH 의 wall 실행, 메시지는 stdin, 실패 시 로그만)
package main

import (
	"os/exec"
	"strings"
)

type wallNotifier struct{}

func (wallNotifier) Wall(msg string) {
	cmd := exec.Command("wall")
	cmd.Stdin = strings.NewReader(msg)
	if out, err := cmd.CombinedOutput(); err != nil {
		logf("[X] wall 실패: %v %s", err, strings.TrimSpace(string(out)))
	}
}
