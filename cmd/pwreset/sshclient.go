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
//
// 어느 경로로 바꿨든, 마지막에 **새 비밀번호로 재접속이 되는지** 확인해서
// 실제 반영 여부를 판정합니다(서버가 변경 직후 세션을 끊는 경우가 있어
// 변경 성공/실패를 접속 결과만으로는 구분할 수 없기 때문).

const (
	dialTimeout = 8 * time.Second  // 접속/인증 타임아웃
	readTimeout = 8 * time.Second  // 각 read 단계 타임아웃
	sessTimeout = 30 * time.Second // 전체 셸 세션 타임아웃
	sshPort     = "22"
)

func clientConfig(auth []ssh.AuthMethod) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            "root",
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         dialTimeout,
	}
}

// verifyLogin 은 주어진 비밀번호로 실제 로그인이 되는지 확인합니다.
// 변경이 반영됐다면 새 비밀번호로 접속이 됩니다 — 이것이 성공의 권위 기준입니다.
func verifyLogin(ip, pass string) bool {
	cfg := clientConfig([]ssh.AuthMethod{ssh.Password(pass)})
	c, err := ssh.Dial("tcp", net.JoinHostPort(ip, sshPort), cfg)
	if c != nil {
		c.Close()
	}
	return err == nil
}

// tryCandidate 는 후보 비밀번호 하나로 접속을 시도해 변경을 시도합니다.
//
// 반환값:
//   - applied: 새 비밀번호로 재접속이 확인돼 변경이 실제로 반영됨
//   - authFailed: 접속/인증 자체가 안 됨(다음 후보로 넘어가야 함)
//   - detail: 결과 사유(진단용). 성공 시 "verified", 그 외 실패 사유 문자열
func tryCandidate(ip, candidate, newPass string) (applied bool, authFailed bool, detail string) {
	fsm := newPromptFSM(candidate, newPass)

	ki := ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i, q := range questions {
			answers[i] = fsm.answer(q)
		}
		return answers, nil
	})

	cfg := clientConfig([]ssh.AuthMethod{ki, ssh.Password(candidate)})

	client, err := ssh.Dial("tcp", net.JoinHostPort(ip, sshPort), cfg)
	if err == nil {
		// keyboard-interactive 단계에서 이미 변경을 마치지 않았다면 pty 셸에서 처리.
		if !fsm.completed() {
			shellFSM := newPromptFSM(candidate, newPass)
			runShell(client, shellFSM)
		}
		client.Close()
	} else {
		detail = err.Error()
	}

	// 권위 기준: 새 비밀번호로 실제 로그인되면 변경 성공으로 확정.
	// (변경 직후 세션이 끊겨 Dial 이 오류를 내는 서버에서도 올바르게 판정됨.)
	if verifyLogin(ip, newPass) {
		return true, false, "verified"
	}
	if err == nil {
		// 접속은 됐지만 새 비밀번호가 반영되지 않음(프롬프트가 없었거나 변경 거부).
		return false, false, "connected-but-not-applied"
	}
	return false, true, detail
}

// runShell 은 pty 셸을 열고 출력 스트림에서 비밀번호 변경 프롬프트를 처리합니다.
//
// 재입력을 보낸 뒤 서버가 변경을 반영했다는 확인(성공 문구 또는 셸 프롬프트)까지
// 기다립니다 — 재입력 직후 바로 끊으면 서버가 변경을 처리하기 전에 연결이 닫혀
// 변경이 유실됩니다. 성공 문구 없이 세션이 종료(EOF)되는 서버도 있으므로,
// 최종 반영 여부는 호출부의 재접속 검증(verifyLogin)으로 확정합니다.
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
			// 재입력 직후 바로 끊지 않고, 변경 반영 확인(성공 문구/셸 프롬프트)까지 대기.
			if fsm.success {
				return true
			}
		case <-readDone:
			// 세션 종료. 성공 문구를 봤다면 성공.
			return fsm.success
		case <-time.After(readTimeout):
			return fsm.success
		case <-overall:
			return fsm.success
		}
	}
}
