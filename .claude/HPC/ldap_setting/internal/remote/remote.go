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
	Root        string // apply 스크립트의 ROOT 환경변수
	DryRun      bool
}

// Result 는 노드 한 대의 실행 결과입니다.
type Result struct {
	Host  string
	Lines []string
}

// WriteHostFile 은 호스트 목록을 임시 파일로 떨궈 gossh -w 에 넘길 경로를 돌려줍니다.
func WriteHostFile(hosts []string) (string, func(), error) {
	f, err := os.CreateTemp("", "ldap-hosts-*.txt")
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
// 스크립트에 bindpw 가 들어 있으므로 퍼미션을 600 으로 만들고,
// 실행이 끝나면 성공/실패와 무관하게 지웁니다.
func BuildCommand(script string, o Options) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(script))
	path := o.RemotePath
	if path == "" {
		path = "/root/ldap_apply.sh"
	}

	env := ""
	if o.Root != "" {
		env += fmt.Sprintf("ROOT=%s ", o.Root)
	}
	if o.DryRun {
		env += "DRYRUN=1 "
	}

	return fmt.Sprintf(
		"umask 077; echo '%s' | base64 -d > %s && chmod 600 %s && %sbash %s; rc=$?; rm -f %s; exit $rc",
		b64, path, path, env, path, path)
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

// Summary 는 결과 줄에서 "RESULT|host|상태|..." 를 찾아 상태만 뽑습니다.
func (r Result) Summary() string {
	for _, l := range r.Lines {
		if strings.HasPrefix(l, "RESULT|") {
			f := strings.Split(l, "|")
			if len(f) >= 3 {
				return f[2]
			}
		}
	}
	return "NORESULT"
}
