// gossh.go - gossh 연동 (-pm 설치완료/설치중 확인, anaconda.log 디스크 확인, 출력 파싱)
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 원격 명령이 찍는 표식 (setup_check 와 같은 이름).
const (
	pmOKMark   = "PM_OK"
	mkNoLog    = "NS_NOLOG"
	mkDisk     = "NS_DISK"
	mkDone     = "NS_DONE"
	osInstText = "OS 설치중" // gossh -pm 이 설치중 호스트에 stderr 로 찍는 문구
	niceLevel  = "10"
)

// childMu 는 자식 프로세스(gossh/ssh)를 한 번에 하나만 실행하도록 막는다(5.1 관리서버 부하).
var childMu sync.Mutex

// runNice 는 `nice -n 10 name args...` 를 실행하고 stdout, stderr 를 따로 돌려준다.
// 패키지 전체에서 한 번에 하나만 실행된다. 비정상 종료는 *exec.ExitError 로 돌려준다.
func runNice(ctx context.Context, name string, args []string, stdin io.Reader) (stdout, stderr []byte, err error) {
	return runNiceIn(ctx, "", name, args, stdin)
}

func runNiceIn(ctx context.Context, dir, name string, args []string, stdin io.Reader) ([]byte, []byte, error) {
	childMu.Lock()
	defer childMu.Unlock()
	var so, se bytes.Buffer
	cmd := exec.CommandContext(ctx, "nice", append([]string{"-n", niceLevel, name}, args...)...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Stdout = &so
	cmd.Stderr = &se
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	return so.Bytes(), se.Bytes(), err
}

// gosshTimeout 은 호스트 수에 비례한 제한 시간이다.
func gosshTimeout(n int) time.Duration {
	return 2*time.Minute + time.Duration(n)*50*time.Millisecond
}

// runGosshList 는 hosts 를 임시 목록 파일에 쓰고 gossh 를 실행한다. 임시 폴더는 끝나면 지운다
// (gossh 가 목록 옆에 *_os_install 같은 결과 파일을 남기기 때문에 전용 폴더를 쓴다).
func runGosshList(cfg *Config, hosts []string, flags []string, cmdline string) ([]byte, []byte, error) {
	dir, err := os.MkdirTemp("", "pm_monitor_")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	list := filepath.Join(dir, "hosts")
	if err := os.WriteFile(list, []byte(strings.Join(hosts, "\n")+"\n"), 0o600); err != nil {
		return nil, nil, err
	}
	args := append([]string{}, flags...)
	args = append(args, "-script", "-w", list, cmdline)
	ctx, cancel := context.WithTimeout(context.Background(), gosshTimeout(len(hosts)))
	defer cancel()
	so, se, err := runNiceIn(ctx, dir, cfg.Gossh, args, nil)
	var ee *exec.ExitError
	if errors.As(err, &ee) && (len(so) > 0 || len(se) > 0) {
		// 일부 호스트 실패로 종료코드가 0 이 아닐 수 있다. 출력으로 판정한다.
		// 출력이 전혀 없으면(gossh 없음 등) 실행 실패로 본다.
		err = nil
	}
	return so, se, err
}

// PmCheck 는 `gossh -pm` 으로 hosts 를 확인한다.
// done: PM_OK 가 찍힌 호스트(설치완료), installing: OS 설치중으로 판정된 호스트,
// other: 그 밖의 호스트와 이유(ssh 접속 실패 등).
func PmCheck(cfg *Config, hosts []string) (done, installing []string, other map[string]string, err error) {
	if len(hosts) == 0 {
		return nil, nil, map[string]string{}, nil
	}
	so, se, err := runGosshList(cfg, hosts, []string{"-pm"}, "echo "+pmOKMark)
	if err != nil {
		return nil, nil, nil, err
	}
	done, installing, other = parsePmOutput(string(so), string(se), hosts)
	return done, installing, other, nil
}

// splitHostLine 은 "host: 내용" 줄을 나눈다. 형식이 아니면 ok=false.
func splitHostLine(line string) (host, val string, ok bool) {
	line = strings.TrimRight(line, "\r")
	i := strings.Index(line, ": ")
	if i <= 0 {
		return "", "", false
	}
	return line[:i], strings.TrimSpace(line[i+2:]), true
}

// parsePmOutput 은 gossh -pm -script 의 stdout/stderr 를 호스트별로 나눈다(결과는 hosts 순서).
// stdout "host: PM_OK" -> done, stderr "host: OS 설치중 ..." -> installing,
// 나머지는 stderr 의 첫 "host: 이유" 줄(없으면 "응답 없음")과 함께 other.
func parsePmOutput(stdout, stderr string, hosts []string) (done, installing []string, other map[string]string) {
	okSet := map[string]bool{}
	for _, l := range strings.Split(stdout, "\n") {
		if h, v, ok := splitHostLine(l); ok && v == pmOKMark {
			okSet[h] = true
		}
	}
	instSet := map[string]bool{}
	reason := map[string]string{}
	for _, l := range strings.Split(stderr, "\n") {
		h, v, ok := splitHostLine(l)
		if !ok {
			continue
		}
		if strings.HasPrefix(v, osInstText) {
			instSet[h] = true
		} else if _, seen := reason[h]; !seen {
			reason[h] = v
		}
	}
	other = map[string]string{}
	for _, h := range hosts {
		switch {
		case instSet[h]:
			installing = append(installing, h)
		case okSet[h]:
			done = append(done, h)
		default:
			r := reason[h]
			if r == "" {
				r = "응답 없음"
			}
			other[h] = r
		}
	}
	return done, installing, other
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildDiskCmd 는 anaconda.log 에서 DiskPatterns 를 고정 문자열로 찾아 표식을 찍는 명령이다
// (setup_check buildRemoteCmd 의 디스크 부분과 같다).
// root 셸이 csh/tcsh 여도 돌도록 변수·중괄호·exit 없이 `;` `&&` `||` 만 쓴다.
func buildDiskCmd(patterns []string) string {
	const f = "/tmp/anaconda.log"
	var args strings.Builder
	for _, p := range patterns {
		args.WriteString("-e " + shQuote(p) + " ")
	}
	return `[ -r ` + f + ` ] || echo ` + mkNoLog + `; ` +
		`[ -r ` + f + ` ] && grep -qF ` + args.String() + f + ` && echo ` + mkDisk + `; ` +
		`echo ` + mkDone
}

// DiskCheck 는 hosts(설치중이고 ssh 가 되는 서버)에서 디스크 미인식 문구를 찾는다.
// diskFail 은 NS_DISK 표식이 나온 호스트다.
func DiskCheck(cfg *Config, hosts []string) (diskFail []string, err error) {
	if len(hosts) == 0 || len(cfg.DiskPatterns) == 0 {
		return nil, nil
	}
	so, _, err := runGosshList(cfg, hosts, nil, buildDiskCmd(cfg.DiskPatterns))
	if err != nil {
		return nil, err
	}
	return parseDiskOutput(string(so)), nil
}

// parseDiskOutput 은 gossh stdout 에서 "host: NS_DISK" 인 호스트를 나타난 순서대로 돌려준다.
func parseDiskOutput(stdout string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range strings.Split(stdout, "\n") {
		if h, v, ok := splitHostLine(l); ok && v == mkDisk && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}
