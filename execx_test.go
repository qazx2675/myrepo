package main

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCmdOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	out, err := runCmd(exec.Command("sh", "-c", "echo a; echo b 1>&2"), 5*time.Second)
	if err != nil || !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, err := runCmd(exec.Command("sh", "-c", "exit 3"), 5*time.Second); err == nil || errors.Is(err, errTimeout) {
		t.Fatalf("비정상 종료는 ExitError 여야 함: %v", err)
	}
}

func TestRunCmdTimeoutKillsGroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	// sh 가 자식(sleep)을 띄우고 기다린다 → 그룹 전체가 끝나야 Wait 가 돌아온다
	start := time.Now()
	_, err := runCmd(exec.Command("sh", "-c", "sleep 30 & wait"), 300*time.Millisecond)
	if !errors.Is(err, errTimeout) {
		t.Fatalf("errTimeout 기대, got %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("너무 오래 걸림: %v", d)
	}
}

func TestRunCmdSigkillAfterGrace(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	old := killGrace
	killGrace = 300 * time.Millisecond
	defer func() { killGrace = old }()
	// SIGTERM 을 무시하는 명령은 유예 뒤 SIGKILL 로 끝난다
	_, err := runCmd(exec.Command("sh", "-c", "trap '' TERM; while :; do sleep 1; done"), 300*time.Millisecond)
	if !errors.Is(err, errTimeout) {
		t.Fatalf("errTimeout 기대, got %v", err)
	}
}

func TestRunCmdCancelKillsGroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	cancel := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		close(cancel)
	}()
	start := time.Now()
	// 제한 시간은 길게 두고 취소만으로 끝나는지 본다(자식 sleep 포함)
	_, err := runCmdCancel(exec.Command("sh", "-c", "sleep 30 & wait"), time.Minute, cancel)
	if !errors.Is(err, errCanceled) {
		t.Fatalf("errCanceled 기대, got %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("너무 오래 걸림: %v", d)
	}
}
