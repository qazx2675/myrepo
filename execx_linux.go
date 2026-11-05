//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// setProcessGroup 은 명령을 새 프로세스 그룹으로 띄우게 한다.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup 은 명령의 프로세스 그룹 전체에 SIGTERM(kill 이 true 면 SIGKILL)을 보낸다.
func signalGroup(cmd *exec.Cmd, kill bool) {
	if cmd.Process == nil {
		return
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}
