// ctl.go - CLI 보조: 데몬 제어(--start/--stop/--restart), snapshot/done/request 명령, 색 메시지
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// 조정 가능한 값
var (
	daemonStartWait = 5 * time.Second  // 기동 후 pid 확인 대기
	daemonStopWait  = 10 * time.Second // SIGTERM 후 종료 대기
)

const remoteOnlyMsg = "데몬은 os8_mgmt 에서만 실행합니다"

// ctlIsTTY: 문자 장치(터미널)인지
func ctlIsTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// ctlPaint: on 이면 ANSI 색(31 빨강, 32 초록, 33 노랑)을 입힌다. NO_COLOR 가 있으면 끈다.
func ctlPaint(s string, code int, on bool) string {
	if !on || os.Getenv("NO_COLOR") != "" {
		return s
	}
	return fmt.Sprintf("\x1b[%dm%s\x1b[0m", code, s)
}

func msgOK(format string, a ...interface{}) {
	fmt.Println(ctlPaint("[O]", 32, ctlIsTTY(os.Stdout)), fmt.Sprintf(format, a...))
}

func msgWarn(format string, a ...interface{}) {
	fmt.Fprintln(os.Stderr, ctlPaint("[!]", 33, ctlIsTTY(os.Stderr)), fmt.Sprintf(format, a...))
}

func msgErr(format string, a ...interface{}) {
	fmt.Fprintln(os.Stderr, ctlPaint("[X]", 31, ctlIsTTY(os.Stderr)), fmt.Sprintf(format, a...))
}

// cmdReport: 인자 없음/--plain. tty 가 아니면 plain.
func cmdReport(plain bool) int {
	if !plain {
		plain = !ctlIsTTY(os.Stdout)
	}
	return runReport(newSource(), plain)
}

// relayArgsOK: 원격 중계 대상 하위명령의 인자 개수 검증 (로컬·원격 공통)
func relayArgsOK(args []string) bool {
	switch args[0] {
	case "status":
		return true
	case "snapshot":
		return len(args) == 1
	case "code", "cancel":
		return len(args) == 2
	case "done":
		return len(args) >= 2
	case "request":
		return requestArgsOK(args[1:])
	}
	return false
}

func requestArgsOK(a []string) bool {
	if len(a) == 0 {
		return false
	}
	switch a[0] {
	case ReqManualRun:
		return len(a) == 3
	case ReqCancel:
		return len(a) == 2
	case ReqRefresh:
		return len(a) == 1
	}
	return false
}

// cmdSnapshot: 스냅샷 JSON 출력
func cmdSnapshot() int {
	b, err := snapshotJSON(BuildSnapshot(dataDir(), snapshotNow()))
	if err != nil {
		msgErr("snapshot 실패: %v", err)
		return 1
	}
	os.Stdout.Write(b)
	return 0
}

// cmdDone: auto_setup done <host...> (출처 manual)
func cmdDone(hosts []string) int {
	if err := markDone(hosts, DoneSrcManual); err != nil {
		msgErr("%v", err)
		return 1
	}
	msgOK("완료 처리 요청: %d대", len(hosts))
	msgWarn("수동 완료는 부팅 시각 검증 없이 인정됩니다 (데몬이 5초 내 수거)")
	return 0
}

// cmdRequest: auto_setup request manual-run <jobid> <yml> | cancel <jobid> | refresh
func cmdRequest(a []string) int {
	var payload map[string]string
	switch a[0] {
	case ReqManualRun:
		payload = map[string]string{"jobid": a[1], "yml": a[2]}
	case ReqCancel:
		payload = map[string]string{"jobid": a[1]}
	}
	p, err := writeRequest(a[0], payload)
	if err != nil {
		msgErr("%v", err)
		return 1
	}
	msgOK("요청 등록: %s", filepath.Base(p))
	return 0
}

// startDaemon: 데몬이 없으면 `auto_setup daemon` 을 setsid 로 기동하고 pid 확인
func startDaemon() int {
	if pid, ok := isRunning(); ok {
		msgOK("이미 실행 중 (pid %d)", pid)
		return 0
	}
	if err := ensureDirs(); err != nil {
		msgErr("디렉터리 생성 실패: %v", err)
		return 1
	}
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	cmd := exec.Command(exe, "daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		msgErr("데몬 기동 실패: %v", err)
		return 1
	}
	_ = cmd.Process.Release()
	deadline := time.Now().Add(daemonStartWait)
	for {
		if pid, ok := isRunning(); ok {
			logf("start: 데몬 기동 pid=%d", pid)
			msgOK("데몬 기동 (pid %d)", pid)
			return 0
		}
		if !time.Now().Before(deadline) {
			msgErr("데몬 기동을 확인하지 못했습니다 (%s 로그 확인)", logPath())
			return 1
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// stopDaemonCmd: 실행 중이 아니면 안내만(성공). stopped 는 실제로 종료했는지.
func stopDaemonCmd() (rc int, stopped bool) {
	pid, ok := isRunning()
	if !ok {
		msgWarn("데몬이 실행 중이 아닙니다")
		return 0, false
	}
	if err := stopDaemon(daemonStopWait); err != nil {
		msgErr("%v", err)
		return 1, false
	}
	logf("stop: 데몬 종료 pid=%d", pid)
	msgOK("데몬 종료 (pid %d)", pid)
	return 0, true
}

// cmdDaemonCtl: start | stop | restart
func cmdDaemonCtl(name string) int {
	switch name {
	case "start":
		return startDaemon()
	case "stop":
		rc, _ := stopDaemonCmd()
		return rc
	}
	if rc, _ := stopDaemonCmd(); rc != 0 {
		return rc
	}
	return startDaemon()
}
