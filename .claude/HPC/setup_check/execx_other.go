//go:build !linux

package main

import "os/exec"

// 리눅스가 아니면 프로세스 그룹 없이 그 프로세스만 끝낸다(개발용 빌드).
func setProcessGroup(cmd *exec.Cmd) {}

func signalGroup(cmd *exec.Cmd, kill bool) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
