// check.go - 준비 확인: gossh -pm 으로 uptime 조회 (local 직접 / os6 는 os6_mgmt 경유)
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
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

// readyCmd: 준비확인 명령 — uptime, 대상 서버의 현재 시각, 설치 기록(/root/anaconda-ks.cfg) 수정 시각.
// 설치 기록이 이번 부팅보다 훨씬 오래됐으면 새로 설치되지 않은 기존 OS 로 본다(접수 후 기존 OS 로 잠깐 부팅된 것을 READY 로 보지 않게).
// 파일이 없으면 stat 이 실패해도 "; :" 로 종료코드 0 (gossh 가 출력 전체를 ERROR 로 돌리지 않게). sh·bash·csh 모두 같은 문법.
const readyCmd = "cat /proc/uptime; date +%s; stat -c %Y /root/anaconda-ks.cfg; :"

// oldOSSlack: 설치 기록 나이가 uptime 보다 이만큼 이상 길면 기존 OS (설치기·OS 의 시간대 차이·%post 시간을 넉넉히 흡수)
const oldOSSlack = 48 * 3600

// checkTimeout: 준비확인 호출(gossh / ssh os6_mgmt) 1회 전체 상한. 60초 고정(conf 변수 없음) — 테스트만 줄인다.
var checkTimeout = 60 * time.Second

const (
	// whyTimeout: 시간 제한에 걸려 응답을 못 받은 호스트의 사유
	whyTimeout = "시간 초과(60초)"
	// whyMismatch: 출력은 있는데 호스트를 하나도 인식하지 못함
	whyMismatch = "출력 형식 불일치"
)

// errCheckTimeout: 준비확인 호출이 checkTimeout 을 넘겨 프로세스 그룹을 종료함 (받은 출력까지는 함께 돌려준다)
var errCheckTimeout = errors.New("시간 초과")

// runExternalDiag: 준비확인 전용 — stdout 과 stderr 를 한 스트림으로 돌려준다 (gossh 의 "SSH 접속 실패: …"
// 같은 실패 사유는 stderr 로 나오므로 진단에 쓴다). checkTimeout 을 넘기면 프로세스 그룹 전체(gossh/ssh 와 자식)를 죽이고
// 그때까지의 출력과 errCheckTimeout 을 돌려준다 (데몬 tick·g 가 동기로 부르므로 hang 방지). 테스트에서 가짜로 교체.
var runExternalDiag = func(stdin string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = oneoffSysProcAttr() // 별도 프로세스 그룹 → 시간 초과 때 그룹째 종료
	cmd.Cancel = func() error {
		killGroup(cmd.Process.Pid, syscall.SIGKILL)
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 2 * time.Second // 손자 프로세스가 출력 파이프를 쥐고 있어도 기다림을 끝낸다
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return out.String(), errCheckTimeout
	}
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

	out, err := runExternalDiag("", "gossh", "-pm", "-script", "-w", p, readyCmd)
	if err != nil && err != errCheckTimeout && strings.TrimSpace(out) == "" {
		if _, ok := err.(*exec.ExitError); !ok {
			return nil, err // gossh 실행 자체 실패
		}
	}
	osi, _ := os.ReadFile(p + "_os_install")
	res := parseCheck(hosts, out, string(osi))
	if err == errCheckTimeout {
		markTimeout(res)
	}
	return res, nil
}

// markTimeout: 시간 초과로 끝난 호출에서 응답을 못 받은 호스트(gossh 가 따로 사유를 내지 않은 호스트)의 사유를 시간 초과로
func markTimeout(res map[string]CheckResult) {
	for h, r := range res {
		if !r.Responded && (r.Why == "" || r.Why == whyMismatch) {
			r.Why = whyTimeout
			res[h] = r
		}
	}
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
	// exec 로 직접 ssh 를 호출하므로 로컬 셸 확장이 없다 → 원격에서 bash 가 $ 를 확장하도록 remoteBash 로 감싼다
	// (os6_mgmt 의 로그인 셸이 csh 계열이어도 동작: $(...) 는 csh 에서 오류). os6_gossh 경로는 shq 로 인용.
	inner := "f=$(mktemp); cat > $f; " + shq(os6_gossh) + ` -pm -script -w $f ` + shq(readyCmd) + `; ` +
		"echo " + osiMarker + "; cat ${f}_os_install 2>/dev/null; rm -f $f ${f}_*"
	out, err := runExternalDiag(hostList(hosts), "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", os6_mgmt, remoteBash(inner))
	if !strings.Contains(out, osiMarker) {
		if err == errCheckTimeout { // 구분선 전에 끊김: 받은 데까지 해석하고 나머지는 시간 초과
			res := parseCheck(hosts, out, "")
			markTimeout(res)
			return res, nil
		}
		if err == nil {
			err = errors.New("os6 응답 형식 오류")
		}
		// ssh 가 낸 실패 사유(Permission denied, Could not resolve hostname, 원격 bash 오류 등)는 out 에 있다 — 함께 보여 준다
		if h := diagHead(out); h != "" {
			return nil, fmt.Errorf("os6 준비확인 실패: %v — %s", err, h)
		}
		return nil, fmt.Errorf("os6 준비확인 실패: %v", err)
	}
	parts := strings.SplitN(out, osiMarker, 2)
	res := parseCheck(hosts, parts[0], parts[1])
	if err == errCheckTimeout {
		markTimeout(res)
	}
	return res, nil
}

// diagHead: 외부 명령 출력에서 사람이 읽을 첫 몇 줄 (ANSI 제거, 빈 줄 제외, 최대 3줄·160자). 진단 메시지용
func diagHead(out string) string {
	var ls []string
	for _, l := range strings.Split(ansiRe.ReplaceAllString(out, ""), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			ls = append(ls, l)
			if len(ls) == 3 {
				break
			}
		}
	}
	s := strings.Join(ls, " / ")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
}

// parseCheck: gossh 출력("host: 123.45 678.9", 사유 줄 포함) + _os_install(한 줄 한 호스트) → 결과.
// 줄 해석·호스트 이름 맞추기는 parseGossh. 출력은 있는데 호스트를 하나도 못 알아보면 로그에 남기고 사유를 whyMismatch 로.
func parseCheck(hosts []string, stdout, osInstall string) map[string]CheckResult {
	want := map[string]bool{}
	for _, h := range hosts {
		want[h] = true
	}
	g := parseGossh(stdout, hosts)
	logGosshUnparsed("준비확인", g, true)
	res := map[string]CheckResult{}
	why := map[string]string{}
	for h, lines := range g.Lines {
		var ints []int64 // readyCmd 의 date·stat 줄 (정수 하나): 현재 시각, 설치 기록 시각
		for _, body := range lines {
			f := strings.Fields(body)
			if len(f) == 0 {
				continue
			}
			if len(f) == 1 {
				if n, err := strconv.ParseInt(f[0], 10, 64); err == nil && n > 1e9 {
					ints = append(ints, n)
					continue
				}
			}
			up, err := strconv.ParseFloat(f[0], 64)
			if err != nil {
				if _, seen := why[h]; !seen { // 첫 메시지만 (예: 원격 stderr, OS 설치중)
					why[h] = clipWhy(body)
				}
				continue
			}
			res[h] = CheckResult{Responded: true, Uptime: up}
		}
		if r, ok := res[h]; ok && len(ints) >= 2 && ints[0]-ints[1] > int64(r.Uptime)+oldOSSlack {
			r.OldOS = true
			res[h] = r
		}
	}
	for _, line := range strings.Split(osInstall, "\n") {
		h := strings.TrimSpace(line)
		if want[h] {
			res[h] = CheckResult{Responded: true, Anaconda: true}
		}
	}
	for _, h := range hosts {
		if _, ok := res[h]; !ok {
			w := why[h]
			if w == "" {
				w = clipWhy(g.Why[h]) // SSH 접속 실패: … / ERROR: 종료코드 N
			}
			if w == "" && g.Head != nil {
				w = whyMismatch
			}
			res[h] = CheckResult{Why: w}
		}
	}
	return res
}

func clipWhy(m string) string {
	m = strings.TrimSpace(m)
	if r := []rune(m); len(r) > 120 {
		m = string(r[:120]) + "…"
	}
	return m
}

// ---- gossh 출력 해석 (준비확인·LDAP·변경 엔진 공통) ----

// gosshOut: parseGossh 결과. 호스트 이름은 요청 목록의 이름으로 맞춰져 있다.
type gosshOut struct {
	Lines map[string][]string // 호스트별 본문 줄 (나온 순서, 사유 줄 제외)
	Why   map[string]string   // 호스트별 첫 사유 줄: "SSH 접속 실패: …" / "ERROR: 종료코드 N"
	Head  []string            // 출력은 있는데 호스트를 하나도 인식 못 했을 때만: 원본 앞 5줄 (CR·ANSI 제거)
	Ev    []gosshLine         // 인식한 줄 전체 (나온 순서, 사유 줄 포함) — gossh 작업 판정(judgeWork)이 순서·횟수를 본다
}

// gosshLine: 인식한 한 줄 (Why = 사유 줄)
type gosshLine struct {
	Host, Text string
	Why        bool
}

// parseGossh: gossh 출력(stdout 또는 stdout+stderr)을 "호스트: 내용" 으로 나눈다.
//   - ANSI 색·줄 끝 CR 을 지우고, 줄 안의 CR(진행률 덮어쓰기)은 마지막 CR 뒤만 본다.
//   - 호스트 자리(첫 ':' 앞)가 비었거나 공백을 포함하면 안내 줄로 보고 버린다.
//   - 호스트 이름은 정확히 같은 이름 우선, 없으면 짧은 이름(첫 '.' 앞)으로 맞춘다.
//     짧은 이름이 요청 목록의 둘 이상과 겹치면(또는 IP 주소면) 맞추지 않는다.
//   - 요청 목록에 없는 호스트 줄은 버린다.
func parseGossh(out string, hosts []string) gosshOut {
	exact := map[string]bool{}
	short := map[string][]string{}
	for _, h := range hosts {
		if h == "" || exact[h] {
			continue
		}
		exact[h] = true
		if s := shortHost(h); s != "" {
			short[s] = append(short[s], h)
		}
	}
	match := func(n string) string {
		if exact[n] {
			return n
		}
		if s := shortHost(n); s != "" && len(short[s]) == 1 {
			return short[s][0]
		}
		return ""
	}
	g := gosshOut{Lines: map[string][]string{}, Why: map[string]string{}}
	var head []string
	seen := false
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		if i := strings.LastIndexByte(l, '\r'); i >= 0 {
			l = l[i+1:]
		}
		l = stripANSI(l)
		if strings.TrimSpace(l) != "" && len(head) < 5 {
			head = append(head, l)
		}
		i := strings.IndexByte(l, ':')
		if i <= 0 {
			continue
		}
		name := strings.TrimSpace(l[:i])
		if name == "" || strings.ContainsAny(name, " \t") {
			continue
		}
		h := match(name)
		if h == "" {
			continue
		}
		seen = true
		body := strings.TrimPrefix(l[i+1:], " ")
		if t := strings.TrimSpace(body); strings.HasPrefix(t, "SSH 접속 실패") || strings.HasPrefix(t, "ERROR: ") {
			g.Ev = append(g.Ev, gosshLine{Host: h, Text: t, Why: true})
			if _, ok := g.Why[h]; !ok {
				g.Why[h] = t
			}
			continue
		}
		g.Lines[h] = append(g.Lines[h], body)
		g.Ev = append(g.Ev, gosshLine{Host: h, Text: body})
	}
	if !seen && len(head) > 0 {
		g.Head = head
	}
	return g
}

// shortHost: 짧은 이름(첫 '.' 앞). IP 주소는 짧은 이름 비교 대상이 아니므로 "".
func shortHost(h string) string {
	if net.ParseIP(h) != nil {
		return ""
	}
	if i := strings.IndexByte(h, '.'); i >= 0 {
		return h[:i]
	}
	return h
}

var (
	gosshLogMu   sync.Mutex
	gosshLogLast string // 같은 해석 실패가 tick 마다 반복될 때 로그를 한 번만 남긴다
)

// logGosshUnparsed: 해석 실패(g.Head 있음)를 auto_setup.log 에 남긴다. showLines=false 면 원본 줄은 남기지 않는다
// (LDAP 수집 응답에는 설정 파일 base64 가 들어 있어 bindpw 노출을 막기 위해).
func logGosshUnparsed(what string, g gosshOut, showLines bool) {
	if g.Head == nil {
		gosshLogMu.Lock()
		gosshLogLast = ""
		gosshLogMu.Unlock()
		return
	}
	msg := fmt.Sprintf("[!] gossh 출력 해석 실패 (%s): 응답에서 요청한 호스트를 하나도 찾지 못함", what)
	if showLines {
		q := make([]string, len(g.Head))
		for i, l := range g.Head {
			q[i] = strconv.Quote(l)
		}
		msg += " — 원본 앞 " + strconv.Itoa(len(q)) + "줄: " + strings.Join(q, " | ")
	} else {
		msg += " (원본 내용은 기록하지 않음)"
	}
	gosshLogMu.Lock()
	dup := msg == gosshLogLast
	gosshLogLast = msg
	gosshLogMu.Unlock()
	if !dup {
		logf("%s", msg)
	}
}
