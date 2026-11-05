//go:build !linux

package main

import (
	"errors"
	"os/exec"
)

// 리눅스가 아니면(개발용 빌드) 세션 분리·lock 없이 동작한다.
func setSetsid(cmd *exec.Cmd) {}

var errLocked = errors.New("다른 워커가 이미 실행 중입니다")

func lockState(dir string) (func(), error) { return func() {}, nil }

func workerAlive(dir string) bool { return false }
