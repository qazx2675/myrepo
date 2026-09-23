//go:build !linux

package monitor

import (
	"errors"
	"os"
	"os/exec"
)

// 이 엔진은 Linux 관리서버용입니다. 다른 OS 에서는 화면 없이(조용히) 동작합니다.

func makeRaw(*os.File) (func(), error) { return nil, errors.New("지원하지 않는 OS") }

func ttyRows(*os.File) int { return 24 }

func setProcessGroup(*exec.Cmd) {}

func killProcessGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}
