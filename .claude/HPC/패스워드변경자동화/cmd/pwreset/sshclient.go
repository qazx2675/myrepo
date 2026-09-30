package main

import (
	"io"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshclient 는 대상 한 대에 접속해 비밀번호 변경 프롬프트에 응답합니다.
//
// 두 가지 프롬프트 경로를 모두 지원합니다.
//  1. SSH 인증 단계: keyboard-interactive 콜백에서 Current/New/Retype 프롬프트에 응답
//  2. 로그인 성공 후 셸 단계: pty 세션을 열고 출력 스트림에서 동일 프롬프트를
//     정규식으로 매칭해 stdin 에 입력 (promptFSM 을 공유)
//
// keyboard-interactive 와 일반 password 인증을 함께 제시하고, 인증이 일반
// password 로 성공하면 pty 셸 경로 상태머신으로 이어서 대기합니다.

const (
	dialTimeout = 8 * time.Second  // 접속/인증 타임아웃
	readTimeout = 8 * time.Second  // 각 read 단계 타임아웃
	sessTimeout = 30 * time.Second // 전체 셸 세션 타임아웃
	sshPort     = "22"
)

// tryCandidate 는 후보 비밀번호 하나로 접속을 시도합니다.
//
// 반환값 authFailed 가 true 면 인증 실패(또는 접속 실패)이므로 호출부에서
// 다음 후보로 넘어가야 합니다. false 면 접속에 성공한 것이며 applied 로 새
// 비밀번호 적용 여부를 알립니다.
func tryCandidate(ip, candidate, newPass string) (applied bool, authFailed bool) {
	fsm := newPromptFSM(candidate, newPass)

	ki := ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i, q := range questions {
			answers[i] = fsm.answer(q)
		}
		return answers, nil
	})

	cfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ki, ssh.Password(candidate)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         dialTimeout,
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(ip, sshPort), cfg)
	if err != nil {
		// 인증 실패·접속 실패 모두 다음 후보로 넘어가도록 처리합니다.
		return false, true
	}
	defer client.Close()

	// keyboard-interactive 경로에서 이미 변경을 마친 경우.
	if fsm.completed() {
		return true, false
	}

	// 일반 password 로 인증에 성공한 경우: pty 셸에서 프롬프트를 이어 처리합니다.
	shellFSM := newPromptFSM(candidate, newPass)
	return runShell(client, shellFSM), false
}

// runShell 은 pty 셸을 열고 출력 스트림에서 비밀번호 변경 프롬프트를 처리합니다.
//
// 재입력까지 마치면(변경 적용) true 를 돌려줍니다. 변경 후 세션이 즉시 끊기는
// 경우(EOF)와 유지되는 경우 모두 성공으로 봅니다. 프롬프트가 나타나지 않고
// 셸만 열린 경우(변경할 것이 없음)에는 false 를 돌려줍니다.
func runShell(client *ssh.Client, fsm *promptFSM) bool {
	session, err := client.NewSession()
	if err != nil {
		return false
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
		return false
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return false
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return false
	}
	if err := session.Shell(); err != nil {
		return false
	}

	chunks := make(chan []byte)
	readDone := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				c := make([]byte, n)
				copy(c, buf[:n])
				chunks <- c
			}
			if err != nil {
				close(readDone)
				return
			}
		}
	}()

	overall := time.After(sessTimeout)
	for {
		select {
		case c := <-chunks:
			for _, resp := range fsm.feed(string(c)) {
				io.WriteString(stdin, resp+"\n")
			}
			if fsm.completed() {
				return true
			}
		case <-readDone:
			// 세션 종료. 재입력까지 마쳤다면 세션종료형 성공입니다.
			return fsm.completed()
		case <-time.After(readTimeout):
			return fsm.completed()
		case <-overall:
			return fsm.completed()
		}
	}
}
