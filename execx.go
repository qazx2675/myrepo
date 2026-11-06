package main

// 외부 명령(gossh·config_check·stress-test 등)을 시간 제한과 함께 실행한다.
// 명령은 자기만의 프로세스 그룹으로 띄우고, 시간이 지나면 그룹 전체에 SIGTERM, 5초 뒤에도 남아 있으면 SIGKILL 을 보낸다.

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"time"
)

// errTimeout 은 시간 제한을 넘겨 명령을 끝냈다는 뜻이다(errors.Is 로 구분).
var errTimeout = errors.New("시간 초과")

// errCanceled 는 취소 신호(중지 요청)를 받아 명령을 끝냈다는 뜻이다(errors.Is 로 구분).
var errCanceled = errors.New("중지 요청으로 중단")

// cancelCh 는 워커 프로세스가 정하는 전역 취소 신호. 닫히면 runCmd 로 돌고 있는 모든 외부 명령이 끝난다.
// (판정·config_check·stress-test 호출이 여러 겹이라 인자로 넘기지 않고 runCmd 가 이 값을 본다. -run 이 아닌 경로는 nil)
var cancelCh <-chan struct{}

// killGrace 는 SIGTERM 을 보낸 뒤 SIGKILL 을 보내기까지 기다리는 시간.
var killGrace = 5 * time.Second

// runCmd 는 cmd 를 timeout 안에 실행한다(timeout <= 0 이면 제한 없음).
// stdout·stderr 는 cmd 에 이미 지정돼 있지 않으면 합쳐서 돌려주는 버퍼로 모은다. 지정한 것이 있으면 그쪽에도 쓴다.
// 시간이 넘으면 errTimeout 을 돌려준다. 실행 자체가 안 되거나 비정상 종료면 그 오류(*exec.ExitError 등)를 돌려준다.
func runCmd(cmd *exec.Cmd, timeout time.Duration) (string, error) {
	return runCmdCancel(cmd, timeout, cancelCh)
}

// runCmdCancel 은 runCmd 에 취소 채널을 더한 것: cancel 이 닫히면 같은 방식(SIGTERM → 5초 뒤 SIGKILL)으로 끝내고 errCanceled 를 돌려준다.
func runCmdCancel(cmd *exec.Cmd, timeout time.Duration, cancel <-chan struct{}) (string, error) {
	return runCmdHold(cmd, timeout, cancel, nil)
}

// runCmdHold 는 runCmdCancel 에 시간 제한 일시정지 채널을 더한 것: hold 로 true 가 오면 시간 제한 타이머를 멈추고,
// false 가 오면 처음부터 다시 timeout 만큼 잰다(사용자 답을 기다리는 동안 stress-test 시간 제한이 흐르지 않게).
// hold 는 버퍼가 있는 채널이어야 하고(보내는 쪽이 막히지 않게), nil 이면 쓰지 않는다.
func runCmdHold(cmd *exec.Cmd, timeout time.Duration, cancel <-chan struct{}, hold <-chan bool) (string, error) {
	var out bytes.Buffer
	if cmd.Stdout != nil {
		cmd.Stdout = io.MultiWriter(cmd.Stdout, &out)
	} else {
		cmd.Stdout = &out
	}
	if cmd.Stderr != nil {
		cmd.Stderr = io.MultiWriter(cmd.Stderr, &out)
	} else {
		cmd.Stderr = &out
	}
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return out.String(), err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var tm *time.Timer
	var timer <-chan time.Time
	startTimer := func() {
		if timeout > 0 {
			tm = time.NewTimer(timeout)
			timer = tm.C
		}
	}
	stopTimer := func() {
		if tm != nil {
			tm.Stop()
			tm, timer = nil, nil
		}
	}
	startTimer()
	defer stopTimer()
	reason := errTimeout
wait:
	for {
		select {
		case err := <-done:
			return out.String(), err
		case <-timer:
			break wait
		case <-cancel:
			reason = errCanceled
			break wait
		case h := <-hold:
			stopTimer()
			if !h {
				startTimer()
			}
		}
	}
	signalGroup(cmd, false)
	select {
	case <-done:
	case <-time.After(killGrace):
		signalGroup(cmd, true)
		<-done
	}
	return out.String(), reason
}
