// gossh.go - gossh 서브프로세스 호출과 출력("호스트: 줄") 파싱
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gosshBin 은 실행할 gossh 이름이다. 비어 있으면 PATH 에서 찾는다(테스트에서 가짜 도구를 앞에 둘 수 있게).
func gosshBin(cfg *Config) string {
	if cfg.Gossh != "" {
		return cfg.Gossh
	}
	return "gossh"
}

// runGossh 는 hosts 를 임시 목록 파일에 쓰고 `gossh -script -w 목록 cmd` 를 root 로 실행한다.
// 임시 폴더는 끝나면 지운다(gossh 가 목록 옆에 _res_off 같은 결과 파일을 남기므로 전용 폴더를 쓴다).
// 일부 호스트 실패로 종료코드가 0 이 아니어도 출력이 있으면 출력으로 판정한다.
func runGossh(cfg *Config, hosts []string, cmd string, timeout time.Duration) (stdout, stderr string, err error) {
	dir, err := os.MkdirTemp("", "gpu_install_")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(dir)
	list := filepath.Join(dir, "hosts")
	if err := os.WriteFile(list, []byte(strings.Join(hosts, "\n")+"\n"), 0o600); err != nil {
		return "", "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var so, se bytes.Buffer
	c := exec.CommandContext(ctx, gosshBin(cfg), "-script", "-u", "root", "-w", list, cmd)
	c.Dir = dir
	c.Stdout = &so
	c.Stderr = &se
	c.WaitDelay = 5 * time.Second
	err = c.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ctx.Err() == nil && (so.Len() > 0 || se.Len() > 0) {
		err = nil
	}
	return so.String(), se.String(), err
}

// splitHostLine 은 "host: 내용" 줄을 나눈다. 형식이 아니면 ok=false.
// gossh 는 원격 종료코드 != 0 이면 별도 줄 "host: ERROR: 종료코드 N" 을 덧붙인다.
// "ERROR: " 접두사는 떼지 않고 그대로 돌려준다(호출하는 쪽에서 판단).
func splitHostLine(line string) (host, val string, ok bool) {
	line = strings.TrimRight(line, "\r")
	i := strings.Index(line, ": ")
	if i <= 0 {
		return "", "", false
	}
	return line[:i], strings.TrimSpace(line[i+2:]), true
}

// hostReasons 는 stderr 에서 호스트별 첫 줄(접속 실패 사유 등)을 모은다.
func hostReasons(stderr string) map[string]string {
	r := map[string]string{}
	for _, l := range strings.Split(stderr, "\n") {
		if h, v, ok := splitHostLine(l); ok && v != "" {
			if _, seen := r[h]; !seen {
				r[h] = v
			}
		}
	}
	return r
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// gossh 위험 명령 가드가 부분일치로 막는 단어(gossh README 3.6). base64 문자열에 우연히 섞이면 안 된다.
var gosshDanger = []string{"reboot", "poweroff", "shutdown", "halt", "ddc", "init 0", "init 6"}

// remoteBash 는 bash 스크립트를 base64 로 감싸 `echo .. | base64 -d | bash` 한 줄로 만든다.
// root 셸이 csh/tcsh 여도 돌고, gossh 가 따옴표를 건드릴 일이 없다.
// 인코딩 결과에 위험 단어가 생기면 맨 앞에 '#' 줄을 붙여 정렬을 바꿔 다시 만든다.
func remoteBash(script string) string {
	for pad := 0; ; pad++ {
		s := script
		if pad > 0 {
			s = strings.Repeat("#", pad) + "\n" + script
		}
		b := base64.StdEncoding.EncodeToString([]byte(s))
		low := strings.ToLower(b)
		bad := false
		for _, w := range gosshDanger {
			if strings.Contains(low, w) {
				bad = true
				break
			}
		}
		if !bad {
			return "echo " + b + " | base64 -d | bash"
		}
	}
}
