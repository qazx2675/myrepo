//go:build !linux

package main

import "os/exec"

// detach 는 Linux 가 아니면 아무것도 하지 않는다(대상 환경은 RHEL, 다른 OS 는 컴파일 확인용).
func detach(cmd *exec.Cmd) {}
