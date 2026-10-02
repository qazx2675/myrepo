// check.go - 준비 확인: gossh -pm 으로 uptime 조회 (local 직접 / os6 는 os6_mgmt 경유)
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// osiMarker: os6 원격 출력에서 gossh stdout 과 _os_install 내용을 나누는 구분선
const osiMarker = "==OSI=="

// runExternal: 외부 명령 실행 (테스트에서 가짜로 교체). stdout 만 돌려준다(stderr 는 버림).
var runExternal = func(stdin string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

type realChecker struct{}

func NewRealChecker() Checker { return realChecker{} }

func (realChecker) Check(route string, hosts []string) (map[string]CheckResult, error) {
	if len(hosts) == 0 {
		return map[string]CheckResult{}, nil
	}
	switch route {
	case "local":
		return checkLocal(hosts)
	case "os6":
		return checkOS6(hosts)
	}
	return nil, fmt.Errorf("알 수 없는 route: %q", route)
}

func hostList(hosts []string) string { return strings.Join(hosts, "\n") + "\n" }

func checkLocal(hosts []string) (map[string]CheckResult, error) {
	f, err := os.CreateTemp("", "as_check_")
	if err != nil {
		return nil, err
	}
	p := f.Name()
	defer cleanupTemp(p)
	if _, err := f.WriteString(hostList(hosts)); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()

	out, err := runExternal("", "gossh", "-pm", "-script", "-w", p, "cat /proc/uptime")
	if err != nil && strings.TrimSpace(out) == "" {
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, err // gossh 실행 자체 실패
		}
	}
	osi, _ := os.ReadFile(p + "_os_install")
	return parseCheck(hosts, out, string(osi)), nil
}

// cleanupTemp: 임시 목록 파일과 gossh 결과 파일(<f>_*) 삭제
func cleanupTemp(p string) {
	os.Remove(p)
	if m, err := filepath.Glob(p + "_*"); err == nil {
		for _, x := range m {
			os.Remove(x)
		}
	}
}

func checkOS6(hosts []string) (map[string]CheckResult, error) {
	if os6_mgmt == "" || os6_gossh == "" {
		return nil, errors.New("os6_mgmt/os6_gossh 가 비어 있습니다")
	}
	// exec 로 직접 ssh 를 호출하므로 로컬 셸 확장이 없다 → 원격 셸이 $ 를 확장하도록 그대로 전달
	remote := "f=$(mktemp); cat > $f; " + os6_gossh + " -pm -script -w $f 'cat /proc/uptime'; " +
		"echo " + osiMarker + "; cat ${f}_os_install 2>/dev/null; rm -f $f ${f}_*"
	out, err := runExternal(hostList(hosts), "ssh", os6_mgmt, remote)
	if !strings.Contains(out, osiMarker) {
		if err == nil {
			err = errors.New("os6 응답 형식 오류")
		}
		return nil, fmt.Errorf("os6 준비확인 실패: %v", err)
	}
	parts := strings.SplitN(out, osiMarker, 2)
	return parseCheck(hosts, parts[0], parts[1]), nil
}

// parseCheck: gossh stdout("host: 123.45 678.9") + _os_install(한 줄 한 호스트) → 결과
func parseCheck(hosts []string, stdout, osInstall string) map[string]CheckResult {
	want := map[string]bool{}
	for _, h := range hosts {
		want[h] = true
	}
	res := map[string]CheckResult{}
	for _, line := range strings.Split(stdout, "\n") {
		i := strings.Index(line, ":")
		if i <= 0 {
			continue
		}
		h := strings.TrimSpace(line[:i])
		if !want[h] {
			continue
		}
		f := strings.Fields(line[i+1:])
		if len(f) == 0 {
			continue
		}
		up, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			continue
		}
		res[h] = CheckResult{Responded: true, Uptime: up}
	}
	for _, line := range strings.Split(osInstall, "\n") {
		h := strings.TrimSpace(line)
		if want[h] {
			res[h] = CheckResult{Responded: true, Anaconda: true}
		}
	}
	for _, h := range hosts {
		if _, ok := res[h]; !ok {
			res[h] = CheckResult{}
		}
	}
	return res
}
