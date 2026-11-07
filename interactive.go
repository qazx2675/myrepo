package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// 대화형 접속이 끝났을 때의 안내(ssh 와 같은 문구). -h -i 예시도 이 문구를 쓴다.
const connClosedFmt = "Connection to %s closed.\n"

// runInteractive는 "gossh -i <호스트>" 처럼 -w·명령 없이 호스트 하나만 주면 "ssh <호스트>" 처럼
// 대화형 셸로 접속한다. 키 인증을 먼저 쓰고(자동 탐색), 키로 안 되면 -p 비밀번호, -p 도 없으면
// ssh 처럼 비밀번호를 물어본다. 접속 설정은 newSSHClientConfig 를 같이 써서 구형 서버(arcfour 등)
// 에도 접속된다. 돌려주는 값은 원격 셸의 종료코드(접속 실패는 255, ssh 와 같음).
func runInteractive(host, user, port, password string, timeout time.Duration) int {
	target := host
	if net.ParseIP(host) != nil || !strings.Contains(host, ":") {
		target = net.JoinHostPort(host, port)
	}

	auth, err := getAuthMethods("", password)
	if err != nil && password != "" {
		fmt.Fprintf(os.Stderr, "SSH 인증 설정 오류: %v\n", err)
		return 255
	}
	if password == "" {
		// ★ 키가 없거나 키로 안 되는 서버는 ssh 처럼 비밀번호를 물어본다(화면에 안 보이게 입력).
		auth = append(auth,
			ssh.PasswordCallback(func() (string, error) {
				return promptPassword(fmt.Sprintf("%s@%s's password: ", user, host))
			}),
			ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				if name != "" || instruction != "" {
					fmt.Fprintln(os.Stderr, strings.TrimSpace(name+" "+instruction))
				}
				answers := make([]string, len(questions))
				for i, q := range questions {
					if i < len(echos) && echos[i] {
						fmt.Fprint(os.Stderr, q)
						s, _ := stdinReader.ReadString('\n')
						answers[i] = strings.TrimRight(s, "\r\n")
						continue
					}
					a, err := promptPassword(q)
					if err != nil {
						return nil, err
					}
					answers[i] = a
				}
				return answers, nil
			}),
		)
	}

	// ★ -t 는 TCP 연결에만 적용한다. 로그인 단계는 사람이 비밀번호를 치는 동안 기다려야 해서
	// 제한시간을 두지 않는다(멈추면 Ctrl+C 로 중단).
	conn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: 접속 실패: %v\n", host, err)
		return 255
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, target, newSSHClientConfig(user, auth, timeout))
	if err != nil {
		conn.Close()
		fmt.Fprintf(os.Stderr, "%s: SSH 접속 실패: %v\n", host, err)
		return 255
	}
	client := ssh.NewClient(c, chans, reqs)
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: 세션 생성 실패: %v\n", host, err)
		return 255
	}
	defer sess.Close()
	sess.Stdout = os.Stdout
	sess.Stderr = os.Stderr
	// ★ sess.Stdin 에 바로 넣으면 원격 셸이 끝나도 Wait 가 키 입력 하나를 더 기다린다.
	// 파이프로 따로 복사하고 Wait 는 출력만 기다리게 한다. 표준입력 리더(stdinReader)를 써서
	// 비밀번호 입력 때 미리 읽힌 글자도 그대로 넘긴다.
	in, err := sess.StdinPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: 세션 생성 실패: %v\n", host, err)
		return 255
	}

	restore := func() {}
	fd := int(os.Stdin.Fd())
	if r, rerr := enableRaw(fd); rerr == nil {
		// ★ 터미널이면 ssh 처럼 원격에 터미널(pty)을 열고 로컬 키 입력을 그대로(raw) 넘긴다.
		// Ctrl+C·방향키·vi 등도 원격에서 처리된다. 터미널이 아니면(파이프 입력) pty 없이 셸만 연다.
		restore = r
		cols, rows := terminalSize(fd)
		if cols == 0 || rows == 0 {
			cols, rows = 80, 24
		}
		termType := os.Getenv("TERM")
		if termType == "" {
			termType = "xterm"
		}
		modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
		if err := sess.RequestPty(termType, rows, cols, modes); err != nil {
			restore()
			fmt.Fprintf(os.Stderr, "%s: 터미널 요청 실패: %v\n", host, err)
			return 255
		}
		stop := watchResize(fd, func(cols, rows int) { _ = sess.WindowChange(rows, cols) })
		defer stop()
	}

	go func() {
		_, _ = io.Copy(in, stdinReader)
		in.Close()
	}()

	if err := sess.Shell(); err != nil {
		restore()
		fmt.Fprintf(os.Stderr, "%s: 셸 시작 실패: %v\n", host, err)
		return 255
	}
	err = sess.Wait()
	restore()

	var exitErr *ssh.ExitError
	var missing *ssh.ExitMissingError
	switch {
	case err == nil:
		fmt.Fprintf(os.Stderr, connClosedFmt, host)
		return 0
	case errors.As(err, &exitErr):
		fmt.Fprintf(os.Stderr, connClosedFmt, host)
		return exitErr.ExitStatus()
	case errors.As(err, &missing):
		fmt.Fprintf(os.Stderr, connClosedFmt, host)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "%s: 연결이 끊어졌습니다: %v\n", host, err)
		return 255
	}
}
