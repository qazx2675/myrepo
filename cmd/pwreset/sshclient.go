package main

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshclient 는 대상 한 대에 접속해 비밀번호 변경 프롬프트에 응답합니다.
//
// 인증은 **키(공개키) 인증을 먼저** 시도하고, 후보 비밀번호는 폴백으로 둡니다
// (gossh 와 동일). 대상 서버가 `PasswordAuthentication no` 로 password 인증을
// 막아둔 경우에도 키로 접속이 되기 때문입니다. 접속 후에는 pty 셸을 열어
// 만료 비밀번호 강제변경 프롬프트(Current/New/Retype)를 promptFSM 으로 처리합니다.
// kbd-interactive 가 켜진 서버라면 인증 단계 콜백으로도 같은 프롬프트를 처리합니다.
//
// 변경 성공의 권위 기준은 서버가 출력하는
// "all authentication tokens updated successfully" (promptFSM.success) 입니다.
// password 인증이 꺼진 서버에서는 새 비밀번호 재접속 검증이 불가하므로 이 문구를 기준으로 삼습니다.

const (
	dialTimeout = 8 * time.Second  // 접속/인증 타임아웃
	readTimeout = 8 * time.Second  // 각 read 단계 타임아웃
	sessTimeout = 30 * time.Second // 전체 셸 세션 타임아웃
	sshPort     = "22"
)

// loadSigners 는 키 인증에 쓸 개인키를 읽어들입니다.
//
// keyPath 가 주어지면 그 키만, 비어 있으면 ~/.ssh 의 표준 키들을 순서대로 시도합니다.
// 하나도 못 읽어도 오류가 아니며(키 없이 비밀번호만으로 동작), 읽은 것만 돌려줍니다.
func loadSigners(keyPath string) []ssh.Signer {
	home, _ := os.UserHomeDir()
	var paths []string
	if keyPath != "" {
		if strings.HasPrefix(keyPath, "~/") {
			keyPath = filepath.Join(home, keyPath[2:])
		}
		paths = []string{keyPath}
	} else {
		paths = []string{
			filepath.Join(home, ".ssh", "id_ed25519"),
			filepath.Join(home, ".ssh", "id_ecdsa"),
			filepath.Join(home, ".ssh", "id_rsa"),
		}
	}
	var signers []ssh.Signer
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s, err := ssh.ParsePrivateKey(b)
		if err != nil {
			continue
		}
		signers = append(signers, s)
	}
	return signers
}

func clientConfig(auth []ssh.AuthMethod) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            "root",
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         dialTimeout,
	}
}

// verifyLogin 은 주어진 비밀번호로 실제 로그인이 되는지 확인합니다(보조 검증).
// 성공하면 변경이 확실히 반영된 것입니다. 단, 서버가 password 인증을 막아둔
// 경우엔 새 비밀번호로도 접속이 안 되므로 이 검증은 실패할 수 있습니다(그때는
// 서버의 "updated successfully" 문구를 권위 기준으로 씁니다).
func verifyLogin(ip, pass string) bool {
	cfg := clientConfig([]ssh.AuthMethod{ssh.Password(pass)})
	c, err := ssh.Dial("tcp", net.JoinHostPort(ip, sshPort), cfg)
	if c != nil {
		c.Close()
	}
	return err == nil
}

// tryCandidate 는 후보 비밀번호 하나로 접속해 변경을 시도합니다.
//
// 반환값:
//   - applied: 변경이 서버에 반영됨("updated successfully" 확인, 또는 새 비번 재접속 확인)
//   - connected: 접속/인증에는 성공(변경 여부와 무관)
//   - detail: 사유(진단용)
//
// connected=false 면 접속 자체가 안 된 것이므로 호출부가 다음 후보로 넘어갑니다.
// connected=true, applied=false 면 접속은 됐으나 현재 비밀번호(후보) 불일치 등으로
// 변경이 안 된 것이므로, 역시 다음 후보를 시도해볼 수 있습니다.
func tryCandidate(ip string, signers []ssh.Signer, candidate, newPass string) (applied bool, connected bool, detail string) {
	fsm := newPromptFSM(candidate, newPass)

	ki := ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		answers := make([]string, len(questions))
		for i, q := range questions {
			answers[i] = fsm.answer(q)
		}
		return answers, nil
	})

	// 키 인증 먼저, 비밀번호(및 kbd-interactive)는 폴백.
	var auth []ssh.AuthMethod
	if len(signers) > 0 {
		auth = append(auth, ssh.PublicKeys(signers...))
	}
	auth = append(auth, ki, ssh.Password(candidate))

	client, err := ssh.Dial("tcp", net.JoinHostPort(ip, sshPort), clientConfig(auth))
	if err != nil {
		// 인증 단계에서 이미 변경을 마친 뒤 서버가 끊은 경우가 있으니, 새 비번 재접속으로 확인.
		if verifyLogin(ip, newPass) {
			return true, true, "verified-after-disconnect"
		}
		return false, false, err.Error()
	}
	defer client.Close()

	changed := false
	if fsm.completed() {
		// kbd-interactive 단계에서 New/Retype 까지 응답한 경우.
		changed = true
	} else {
		shellFSM := newPromptFSM(candidate, newPass)
		changed = runShell(client, shellFSM)
	}

	if changed {
		if verifyLogin(ip, newPass) {
			return true, true, "verified"
		}
		return true, true, "updated-confirmed"
	}
	return false, true, "connected-but-not-applied"
}

// runShell 은 pty 셸을 열고 출력 스트림에서 비밀번호 변경 프롬프트를 처리합니다.
//
// 재입력을 보낸 뒤 서버가 변경을 반영했다는 확인(성공 문구 또는 셸 프롬프트)까지
// 기다립니다 — 재입력 직후 바로 끊으면 서버가 변경을 처리하기 전에 연결이 닫혀
// 변경이 유실됩니다. "all authentication tokens updated successfully" 를 봤을 때만
// true 를 돌려줍니다.
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
			return fsm.success
		case <-time.After(readTimeout):
			return fsm.success
		case <-overall:
			return fsm.success
		}
	}
}
