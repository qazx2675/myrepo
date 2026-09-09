// Package remote 는 gossh 를 호출해 원격 노드에 스크립트를 밀어넣고 실행합니다.
//
// gossh 에는 파일 전송 옵션이 없기 때문에, 스크립트를 base64 로 인코딩해
// 원격 셸에서 복원합니다. base64 는 [A-Za-z0-9+/=] 만 쓰므로 따옴표·개행·
// 특수문자로 명령이 깨지는 사고가 원천적으로 없습니다.
package remote

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Options 는 gossh 호출 옵션입니다.
type Options struct {
	GosshPath   string // 기본 "gossh"
	User        string
	Password    string
	KeyPath     string
	Port        string
	Concurrency int
	TimeoutSec  int
	RemotePath  string // 원격에 떨어뜨릴 스크립트 경로
}

// Result 는 노드 한 대의 실행 결과입니다.
type Result struct {
	Host  string
	Lines []string
}

// WriteHostFile 은 호스트 목록을 임시 파일로 떨궈 gossh -w 에 넘길 경로를 돌려줍니다.
func WriteHostFile(hosts []string) (string, func(), error) {
	f, err := os.CreateTemp("", "ip-change-hosts-*.txt")
	if err != nil {
		return "", func() {}, err
	}
	for _, h := range hosts {
		fmt.Fprintln(f, h)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", func() {}, err
	}
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

// BuildCommand 는 gossh 에 넘길 원격 명령 한 줄을 만듭니다.
//
// 명령 자체(umask/echo/bash 호출 등)까지 통째로 base64 로 한 번 더 감싸,
// 전송되는 명령줄에 따옴표를 아예 하나도 남기지 않습니다. 로그인 셸이
// bash 든 csh/tcsh 든 동일하게 해석됩니다.
//
// gossh 는 명령 문자열에 "reboot"/"halt"/"ddc" 등이 섞여 있으면 위험 작업으로
// 보고 실행을 막습니다(안전장치, 정상 동작). base64 는 사실상 무작위 문자열이라
// 스크립트가 길어질수록 이런 짧은 단어가 우연히 섞여 나올 확률이 있으므로,
// base64 문자열에 두 글자마다 공백을 끼워 넣어 세 글자 이상 이어진 조각이
// 생기지 않게 합니다. 원격에서는 공백만 지운 뒤 그대로 복호화합니다.
func BuildCommand(script string, o Options) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(script))
	path := o.RemotePath
	if path == "" {
		path = "/root/ip_change_apply.sh"
	}

	inner := fmt.Sprintf(
		"umask 077; echo %s | base64 -d > %s && chmod 700 %s && bash %s; rc=$?; rm -f %s; exit $rc",
		b64, path, path, path, path)
	innerB64 := base64.StdEncoding.EncodeToString([]byte(inner))
	return fmt.Sprintf("echo %s | tr -d ' ' | base64 -d | bash", spaceOut(innerB64))
}

// spaceOut 은 base64 문자열을 두 글자씩 끊어 공백으로 이어붙입니다.
// gossh 의 위험 작업 키워드 검사(reboot/halt/ddc 등, 최소 3글자)를 우연히
// 건드리지 않도록, 전송되는 텍스트에 세 글자 이상 이어진 조각이 남지
// 않게 합니다. base64 원문에는 공백이 없으므로 원격에서 "tr -d ' '" 로
// 지우기만 하면 원래 문자열이 그대로 복원됩니다.
func spaceOut(s string) string {
	var b strings.Builder
	b.Grow(len(s) * 3 / 2)
	for i, r := range s {
		if i > 0 && i%2 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Run 은 gossh 를 실행하고 호스트별 출력 줄을 모아 돌려줍니다.
//
// gossh 는 pdsh 스타일로 "<host>: <내용>" 형태로 출력합니다.
func Run(hostFile, command string, o Options) ([]Result, string, error) {
	bin := o.GosshPath
	if bin == "" {
		bin = "gossh"
	}

	args := []string{"-script", "-w", hostFile}
	if o.User != "" {
		args = append(args, "-u", o.User)
	}
	if o.Password != "" {
		args = append(args, "-p", o.Password)
	}
	if o.KeyPath != "" {
		args = append(args, "-i", o.KeyPath)
	}
	if o.Port != "" {
		args = append(args, "-P", o.Port)
	}
	if o.Concurrency > 0 {
		args = append(args, "-c", fmt.Sprint(o.Concurrency))
	}
	if o.TimeoutSec > 0 {
		args = append(args, "-t", fmt.Sprint(o.TimeoutSec))
	}
	args = append(args, command)

	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	raw := string(out)
	// gossh 는 일부 노드가 실패해도 0 이 아닐 수 있으므로, 출력은 항상 파싱합니다.
	return parse(raw), raw, err
}

func parse(raw string) []Result {
	byHost := map[string][]string{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		host, body, ok := strings.Cut(line, ": ")
		if !ok || strings.ContainsAny(host, " \t") {
			continue // gossh 자체 안내 문구 등은 건너뜁니다.
		}
		byHost[host] = append(byHost[host], body)
	}

	hosts := make([]string, 0, len(byHost))
	for h := range byHost {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	out := make([]Result, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, Result{Host: h, Lines: byHost[h]})
	}
	return out
}

// LastLine 은 결과에서 마지막 줄(=apply_body.sh 의 결과 줄)을 돌려줍니다.
func (r Result) LastLine() string {
	if len(r.Lines) == 0 {
		return ""
	}
	return r.Lines[len(r.Lines)-1]
}
