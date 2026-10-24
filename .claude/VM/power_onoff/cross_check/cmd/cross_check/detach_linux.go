//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// detach 는 --ensure 가 띄우는 데몬을 새 세션(setsid)으로 떼어 낸다:
// cron·로그인 세션이 끝나거나 SIGHUP 이 와도 데몬이 따라 죽지 않는다.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
