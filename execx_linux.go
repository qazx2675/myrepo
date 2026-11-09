//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// setProcessGroup 은 명령을 새 프로세스 그룹으로 띄우게 한다.
// 이미 새 세션(setSetsid, daemon_linux.go)으로 정해 둔 명령은 그대로 둔다: 새 세션도 새 프로세스 그룹(그룹 번호 = pid)이라
// signalGroup 이 그대로 통하고, Setsid 와 Setpgid 를 함께 켜면 시작이 EPERM 으로 실패한다.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setsid {
		return
	}
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
