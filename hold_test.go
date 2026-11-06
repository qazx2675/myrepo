package main

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCmdHoldPausesTimeout(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	hold := make(chan bool, 8)
	hold <- true // 시작하자마자 일시정지
	go func() {
		time.Sleep(900 * time.Millisecond) // 제한(600ms)보다 오래 멈춰 있어도
		hold <- false                      // 재개하면 처음부터 600ms 다시 잰다
	}()
	start := time.Now()
	_, err := runCmdHold(exec.Command("sh", "-c", "sleep 1.2"), 600*time.Millisecond, nil, hold)
	if err != nil {
		t.Fatalf("일시정지 중에는 시간초과가 아니어야 함: %v (%v)", err, time.Since(start))
	}
	if d := time.Since(start); d < 1100*time.Millisecond {
		t.Errorf("명령이 끝나기 전에 돌아옴: %v", d)
	}
}

func TestRunCmdHoldResumeFreshTimeout(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	hold := make(chan bool, 8)
	hold <- true
	go func() {
		time.Sleep(300 * time.Millisecond)
		hold <- false
	}()
	start := time.Now()
	_, err := runCmdHold(exec.Command("sh", "-c", "sleep 30 & wait"), 400*time.Millisecond, nil, hold)
	if !errors.Is(err, errTimeout) {
		t.Fatalf("재개 뒤 제한을 넘기면 errTimeout 이어야 함: %v", err)
	}
	// 300ms 멈춤 + 재개 뒤 400ms = 최소 700ms (일시정지가 없었다면 400ms 에 끝났을 것)
	if d := time.Since(start); d < 650*time.Millisecond || d > 6*time.Second {
		t.Errorf("재개 뒤 새 제한이 아님: %v", d)
	}
}

func TestRunCmdHoldCancelWhilePaused(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	hold := make(chan bool, 8)
	hold <- true
	cancel := make(chan struct{})
	go func() {
		time.Sleep(500 * time.Millisecond)
		close(cancel)
	}()
	_, err := runCmdHold(exec.Command("sh", "-c", "sleep 30 & wait"), 100*time.Millisecond, cancel, hold)
	if !errors.Is(err, errCanceled) {
		t.Fatalf("일시정지 중에도 취소는 되고, 시간초과는 아니어야 함: %v", err)
	}
}

func TestRunCmdStdinPipe(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	// stdin 파이프로 넣은 답을 읽고 끝난다(프로세스가 끝나면 파이프는 exec 이 닫는다)
	cmd := exec.Command("sh", "-c", "read a; echo got=$a")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(100 * time.Millisecond); in.Write([]byte("r\n")) }()
	out, err := runCmd(cmd, 5*time.Second)
	if err != nil || !strings.Contains(out, "got=r") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestLineWatchBusy(t *testing.T) {
	var out strings.Builder
	var msgs []string
	lw := &lineWatch{w: &out, busyMark: []byte(stressBusyMark), busy: func(m string) { msgs = append(msgs, m) }}
	lw.Write([]byte("[i] 등록\n[인벤토리사용중] 템플릿 stress(7) 인벤토리가 hong 입니다"))
	if len(msgs) != 0 {
		t.Fatal("줄이 끝나기 전에 호출됨")
	}
	lw.Write([]byte("(DEFAULT 아님)\n[감시] a 경과\n  [인벤토리사용중] 두 번째 \n"))
	if len(msgs) != 2 || msgs[0] != "템플릿 stress(7) 인벤토리가 hong 입니다(DEFAULT 아님)" || msgs[1] != "두 번째" {
		t.Errorf("msgs=%q", msgs)
	}
	if !strings.Contains(out.String(), "[감시] a 경과") {
		t.Errorf("출력이 그대로 전달돼야 함: %q", out.String())
	}
}
