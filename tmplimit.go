package main

// -tmp / -limit / -tl : 대상 서버들의 설정 파일을 원격에서 수정/복사하는 옵션.
// 질문은 전부 먼저 받고(프롬프트), 그 결과로 원격에서 실행할 스크립트를 만들어
// 기존 명령 실행 흐름(runSSHCommand)에 "명령어"로 넘긴다. 결과가 같은 호스트는 -b 처럼 묶어서 보여준다.

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	tmpConfPath    = "/etc/tmpfiles.d/custom-tmp.conf"
	limitsConfPath = "/etc/security/limits.conf"
	// 원격에 넘기는 명령 한 줄의 길이 한계(약 128KB)에 base64 증가분을 감안한 상한.
	maxLimitsBytes = 60000
)

// 프롬프트용 표준입력 리더(여러 곳에서 각자 만들면 파이프 입력이 버퍼에 먹혀 사라질 수 있어 하나만 쓴다).
var stdinReader = bufio.NewReader(os.Stdin)

var tmpValueRegex = regexp.MustCompile(`^[0-9]+$`)

func promptLine(msg string) string {
	fmt.Fprint(os.Stderr, msg)
	s, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(s)
}

// tmpLinePattern은 변경/삭제 대상 줄("q /tmp 1777 root root @@h")을 찾는 정규식(ERE)이다.
const tmpLinePattern = `^q[[:space:]]+/tmp[[:space:]]+1777[[:space:]]+root[[:space:]]+root[[:space:]]+`

// buildTmpScript는 -tmp 원격 스크립트를 만든다. del이면 해당 줄 삭제, 아니면 hours 시간으로 변경(없으면 1행에 추가).
func buildTmpScript(hours string, del bool) string {
	var b strings.Builder
	b.WriteString("F=" + tmpConfPath + "\n")
	b.WriteString("P='" + tmpLinePattern + "'\n")
	b.WriteString("mkdir -p /etc/tmpfiles.d\n")
	b.WriteString("if grep -Eq \"$P\" \"$F\" 2>/dev/null; then HAS=1; else HAS=0; fi\n")
	if del {
		b.WriteString("if [ $HAS = 1 ]; then sed -r -i \"\\|$P|d\" \"$F\"; fi\n")
		b.WriteString("if grep -Eq \"$P\" \"$F\" 2>/dev/null; then echo \"[tmp] 삭제 실패\"; " +
			"elif [ $HAS = 1 ]; then echo \"[tmp] 삭제 완료\"; " +
			"else echo \"[tmp] 해당 줄 없음 (변경 없음)\"; fi\n")
		return b.String()
	}
	line := "q /tmp 1777 root root " + hours + "h"
	b.WriteString("if [ $HAS = 1 ]; then\n")
	b.WriteString("  sed -r -i 's|" + tmpLinePattern + ".*$|" + line + "|' \"$F\"\n")
	b.WriteString("else\n")
	b.WriteString("  { echo '" + line + "'; cat \"$F\" 2>/dev/null; true; } > \"$F.gossh.new\" && mv -f \"$F.gossh.new\" \"$F\" || rm -f \"$F.gossh.new\"\n")
	b.WriteString("fi\n")
	b.WriteString("L=$(grep -E \"$P\" \"$F\" 2>/dev/null | head -1)\n")
	b.WriteString("echo \"[tmp] ${L:-실패: 줄을 확인할 수 없음}\"\n")
	return b.String()
}

// buildLimitScript는 -limit 원격 스크립트를 만든다: .bak 백업(있으면 덮어씀) → 복사 → sha256 동일 확인.
func buildLimitScript(content []byte) string {
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	b64 := base64.StdEncoding.EncodeToString(content)
	var b strings.Builder
	b.WriteString("F=" + limitsConfPath + "\n")
	b.WriteString("OKB=1\n")
	b.WriteString("if [ -e \"$F\" ]; then cp -pf \"$F\" \"$F.bak\" || OKB=0; fi\n")
	b.WriteString("if [ $OKB = 1 ] && printf '%s' '" + b64 + "' | base64 -d > \"$F.gossh.new\" && mv -f \"$F.gossh.new\" \"$F\"; then\n")
	b.WriteString("  S=$(sha256sum \"$F\" | cut -d' ' -f1)\n")
	b.WriteString("  if [ \"$S\" = '" + sha + "' ]; then echo \"[limit] 완료: 비교서버와 동일 (sha256 " + sha[:12] + ")\"; " +
		"else echo \"[limit] 불일치: 복사 후 내용이 비교서버와 다름\"; fi\n")
	b.WriteString("else\n")
	b.WriteString("  rm -f \"$F.gossh.new\"\n")
	b.WriteString("  echo \"[limit] 실패: 백업 또는 복사 오류\"\n")
	b.WriteString("fi\n")
	return b.String()
}

// fetchRemoteFile은 비교서버에 접속해 파일 내용을 그대로 읽어온다.
func fetchRemoteFile(host, path, user string, auth []ssh.AuthMethod, port string, timeout time.Duration) ([]byte, error) {
	target := host
	if net.ParseIP(host) != nil || !strings.Contains(host, ":") {
		target = net.JoinHostPort(host, port)
	}
	conn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(timeout))
	cfg := &ssh.ClientConfig{User: user, Auth: auth, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: timeout}
	c, chans, reqs, err := ssh.NewClientConn(conn, target, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	client := ssh.NewClient(c, chans, reqs)
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	var out, errb bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &errb
	if err := sess.Run("cat " + path); err != nil {
		return nil, fmt.Errorf("%v %s", err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// planSpecial은 -tmp/-limit/-tl 의 질문을 한 번에 모두 받고, 대상 서버에서 실행할 명령(스크립트)을 돌려준다.
func planSpecial(doTmp, doLimit bool, user string, auth []ssh.AuthMethod, port string, timeout time.Duration) string {
	var tmpHours string
	var tmpDel bool
	var refHost string
	limitOK := false

	// 1) 질문은 순서대로 전부 먼저 받는다.
	if doTmp {
		ans := strings.ToLower(promptLine("변경할 설정값 (시간 또는 d (삭제)) : "))
		if ans == "d" {
			tmpDel = true
		} else if n, err := strconv.Atoi(ans); tmpValueRegex.MatchString(ans) && err == nil && n > 0 {
			tmpHours = strconv.Itoa(n)
		} else {
			fmt.Fprintln(os.Stderr, "설정값은 1 이상의 숫자(시간) 또는 d(삭제) 만 입력할 수 있습니다.")
			os.Exit(1)
		}
	}
	if doLimit {
		refHost = promptLine("비교서버 입력 ( " + limitsConfPath + ") : ")
		if refHost == "" {
			fmt.Fprintln(os.Stderr, "비교서버가 입력되지 않았습니다.")
			os.Exit(1)
		}
		yn := strings.ToLower(promptLine("비교서버의 " + limitsConfPath + "를 대상서버로 복사 진행 (y,n) : "))
		limitOK = yn == "y" || yn == "yes"
		if !limitOK {
			fmt.Fprintln(os.Stderr, "limits.conf 복사는 취소되었습니다.")
		}
	}

	// 2) 비교서버 파일을 읽고 가드(비어 있거나 너무 크면 중단)
	var limitScript string
	if limitOK {
		content, err := fetchRemoteFile(refHost, limitsConfPath, user, auth, port, timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "비교서버(%s)에서 %s 를 읽을 수 없습니다: %v\n", refHost, limitsConfPath, err)
			os.Exit(1)
		}
		if len(bytes.TrimSpace(content)) == 0 {
			fmt.Fprintf(os.Stderr, "[가드] 비교서버(%s)의 %s 가 비어 있어 복사를 중단합니다.\n", refHost, limitsConfPath)
			os.Exit(1)
		}
		if len(content) > maxLimitsBytes {
			fmt.Fprintf(os.Stderr, "[가드] 비교서버의 %s 가 너무 큽니다(%d바이트 > %d).\n", limitsConfPath, len(content), maxLimitsBytes)
			os.Exit(1)
		}
		limitScript = buildLimitScript(content)
	}

	var script strings.Builder
	if doTmp {
		script.WriteString(buildTmpScript(tmpHours, tmpDel))
	}
	script.WriteString(limitScript)
	if script.Len() == 0 {
		fmt.Fprintln(os.Stderr, "실행할 작업이 없어 종료합니다.")
		os.Exit(0)
	}
	script.WriteString("true\n")
	b64 := base64.StdEncoding.EncodeToString([]byte(script.String()))
	return "printf '%s' '" + b64 + "' | base64 -d | sh"
}
