// tmpclean_linux_test.go - 원격 임시 파일(remoteTmpSh)이 ssh 가 끊겨도 지워지는지 (프로세스 그룹 신호라 linux 전용)
package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// startRemoteTmp: remoteTmpSh 로 만든 경로를 첫 줄로 출력한 뒤 body 를 실행하는 bash 를 새 프로세스 그룹으로 띄운다
func startRemoteTmp(t *testing.T, v string, dir, keep bool, body string) (*exec.Cmd, io.ReadCloser, string) {
	t.Helper()
	cmd := exec.Command("bash", "-c", remoteTmpSh(v, dir, keep)+`echo "$`+v+`"; `+body)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("경로 줄: %q %v", line, err)
	}
	p := strings.TrimSpace(line)
	base := filepath.Join(os.Getenv("AUTO_SETUP_DIR"), "tmp")
	if !strings.HasPrefix(p, base+"/as.") {
		t.Fatalf("임시 경로가 %s 아래가 아님: %q", base, p)
	}
	return cmd, out, p
}

func waitGoneT(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatal("원격 bash 가 끝나지 않음")
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestRemoteTmpShCleanup(t *testing.T) {
	needBash(t)
	setupDir(t)
	loop := `echo h > "$f"; echo x > "${f}_res_off"; sh -c 'while :; do echo y; sleep 0.1; done'; echo MARK`

	// 정상 종료
	cmd, out, p := startRemoteTmp(t, "f", false, false, `echo h > "$f"; echo x > "${f}_res_off"; echo MARK`)
	io.Copy(io.Discard, out)
	waitGoneT(t, cmd)
	if exists(p) || exists(p+"_res_off") {
		t.Fatalf("정상 종료 뒤 남음: %s", p)
	}

	// ssh -t 끊김: 원격 세션(프로세스 그룹)에 SIGHUP
	cmd, out, p = startRemoteTmp(t, "f", false, false, loop)
	time.Sleep(300 * time.Millisecond)
	if !exists(p) || !exists(p+"_res_off") {
		t.Fatalf("실행 중에는 있어야 함: %s", p)
	}
	syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
	go io.Copy(io.Discard, out)
	waitGoneT(t, cmd)
	if exists(p) || exists(p+"_res_off") {
		t.Fatalf("SIGHUP 뒤 남음: %s", p)
	}

	// 출력을 받던 쪽(로컬 ssh)이 죽음: 원격 출력이 SIGPIPE
	cmd, out, p = startRemoteTmp(t, "f", false, false, loop)
	time.Sleep(300 * time.Millisecond)
	out.Close()
	waitGoneT(t, cmd)
	if exists(p) || exists(p+"_res_off") {
		t.Fatalf("SIGPIPE 뒤 남음: %s", p)
	}

	// keep(2차 체크 디렉터리): 정상 종료면 남기고(회수용), 끊기면 지운다
	cmd, out, p = startRemoteTmp(t, "d", true, true, `echo x > "$d/targets.txt"; echo MARK`)
	io.Copy(io.Discard, out)
	waitGoneT(t, cmd)
	if !exists(filepath.Join(p, "targets.txt")) {
		t.Fatalf("keep: 정상 종료면 남아야 함: %s", p)
	}
	os.RemoveAll(p)
	cmd, out, p = startRemoteTmp(t, "d", true, true, `echo x > "$d/targets.txt"; sh -c 'while :; do echo y; sleep 0.1; done'`)
	time.Sleep(300 * time.Millisecond)
	syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
	go io.Copy(io.Discard, out)
	waitGoneT(t, cmd)
	if exists(p) {
		t.Fatalf("keep: SIGHUP 뒤 남음: %s", p)
	}
}
