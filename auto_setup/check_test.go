// check_test.go - check.go 단위 테스트: gossh 출력 파싱(응답/anaconda/무응답), local·os6 호출 형태, 임시파일 정리
package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeExternal(t *testing.T, fn func(stdin, name string, args ...string) (string, error)) {
	t.Helper()
	old, oldD := runExternal, runExternalDiag
	runExternal, runExternalDiag = fn, fn
	t.Cleanup(func() { runExternal, runExternalDiag = old, oldD })
}

func TestParseCheck(t *testing.T) {
	hosts := []string{"h1", "h2", "h3", "h4"}
	out := "h1: 123.45 678.9\nunknown: 1.0 2.0\nh2 : 5.5 6.6\nh1: extra line\nbad line\n"
	res := parseCheck(hosts, out, "h3\nnope\n")
	if r := res["h1"]; !r.Responded || r.Anaconda || r.Uptime != 123.45 {
		t.Fatalf("h1: %+v", r)
	}
	if r := res["h2"]; !r.Responded || r.Uptime != 5.5 {
		t.Fatalf("h2: %+v", r)
	}
	if r := res["h3"]; !r.Responded || !r.Anaconda {
		t.Fatalf("h3(anaconda): %+v", r)
	}
	if r := res["h4"]; r.Responded || r.Anaconda {
		t.Fatalf("h4(무응답): %+v", r)
	}
	if _, ok := res["unknown"]; ok {
		t.Fatal("요청하지 않은 호스트가 결과에 포함됨")
	}
}

func TestCheckLocal(t *testing.T) {
	var listFile string
	fakeExternal(t, func(stdin, name string, args ...string) (string, error) {
		if name != "gossh" {
			t.Fatalf("명령: %s", name)
		}
		want := []string{"-pm", "-script", "-w"}
		if len(args) != 5 || args[0] != want[0] || args[1] != want[1] || args[2] != want[2] || args[4] != readyCmd {
			t.Fatalf("인자: %v", args)
		}
		listFile = args[3]
		b, err := os.ReadFile(listFile)
		if err != nil || string(b) != "a\nb\nc\n" {
			t.Fatalf("목록 파일: %q %v", b, err)
		}
		os.WriteFile(listFile+"_os_install", []byte("b\n"), 0644)
		os.WriteFile(listFile+"_res_off", []byte("c\n"), 0644)
		return "a: 10.5 20.0\n", nil
	})
	res, err := NewRealChecker().Check("local", []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if !res["a"].Responded || res["a"].Uptime != 10.5 || res["a"].Anaconda {
		t.Fatalf("a: %+v", res["a"])
	}
	if !res["b"].Anaconda {
		t.Fatalf("b: %+v", res["b"])
	}
	if res["c"].Responded {
		t.Fatalf("c: %+v", res["c"])
	}
	if m, _ := filepath.Glob(listFile + "*"); len(m) != 0 {
		t.Fatalf("임시 파일이 남음: %v", m)
	}
}

func TestCheckLocalExecFailure(t *testing.T) {
	fakeExternal(t, func(stdin, name string, args ...string) (string, error) {
		return "", errors.New("gossh 없음")
	})
	if _, err := NewRealChecker().Check("local", []string{"a"}); err == nil {
		t.Fatal("gossh 실행 실패는 에러여야 함")
	}
}

func TestCheckOS6(t *testing.T) {
	oldM, oldG := os6_mgmt, os6_gossh
	os6_mgmt, os6_gossh = "mgmtstub", "/stub/gossh"
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })

	fakeExternal(t, func(stdin, name string, args ...string) (string, error) {
		if name != "ssh" || len(args) != 6 || strings.Join(args[:5], " ") != "-o BatchMode=yes -o ConnectTimeout=10 mgmtstub" {
			t.Fatalf("호출: %s %v", name, args)
		}
		if stdin != "a\nb\n" {
			t.Fatalf("stdin: %q", stdin)
		}
		r := args[5]
		if !strings.HasPrefix(r, "bash -c '") {
			t.Fatalf("bash -c 로 감싸야 함: %s", r)
		}
		for _, s := range []string{`f=$(mktemp "$_t/as.XXXXXXXX")`, `trap "rm -f \"\$f\" \"\$f\"_*" EXIT`, "cat > $f", `'\''/stub/gossh'\'' -pm -script -w $f '\''` + readyCmd + `'\''`,
			"echo ==OSI==", "cat ${f}_os_install", "rm -f $f ${f}_*"} {
			if !strings.Contains(r, s) {
				t.Fatalf("원격 문자열에 %q 없음: %s", s, r)
			}
		}
		return "a: 77.7 1.0\n==OSI==\nb\n", nil
	})
	res, err := NewRealChecker().Check("os6", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !res["a"].Responded || res["a"].Uptime != 77.7 || !res["b"].Anaconda {
		t.Fatalf("결과: %+v", res)
	}
}

func TestCheckOS6BadOutput(t *testing.T) {
	oldM, oldG := os6_mgmt, os6_gossh
	os6_mgmt, os6_gossh = "mgmtstub", "/stub/gossh"
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })
	fakeExternal(t, func(stdin, name string, args ...string) (string, error) {
		return "", errors.New("ssh 실패")
	})
	if _, err := NewRealChecker().Check("os6", []string{"a"}); err == nil {
		t.Fatal("구분선 없는 응답은 에러여야 함")
	}
}

func TestCheckUnknownRouteAndEmpty(t *testing.T) {
	if _, err := NewRealChecker().Check("x", []string{"a"}); err == nil {
		t.Fatal("알 수 없는 route 는 에러")
	}
	if res, err := NewRealChecker().Check("local", nil); err != nil || len(res) != 0 {
		t.Fatalf("빈 목록: %v %v", res, err)
	}
}

// 무응답 호스트의 사유(gossh 가 출력한 메시지)를 Why 로 보존한다
func TestParseCheckWhy(t *testing.T) {
	out := "a: 10.5 20.0\nb: SSH 접속 실패: dial tcp 10.0.0.2:22: i/o timeout\n\x1b[33mc: OS 설치중\x1b[0m\nz: 무관\n"
	res := parseCheck([]string{"a", "b", "c", "d"}, out, "")
	if !res["a"].Responded || res["a"].Why != "" {
		t.Fatalf("a: %+v", res["a"])
	}
	if res["b"].Responded || res["b"].Why != "SSH 접속 실패: dial tcp 10.0.0.2:22: i/o timeout" {
		t.Fatalf("b: %+v", res["b"])
	}
	if res["c"].Why != "OS 설치중" || res["d"].Why != "" {
		t.Fatalf("c/d: %+v %+v", res["c"], res["d"])
	}
}

func TestWhySuffix(t *testing.T) {
	if whySuffix(CheckResult{}) != "" || whySuffix(CheckResult{Why: "x"}) != " (x)" {
		t.Fatal("whySuffix")
	}
}

// ---- v0.8.0 §1: gossh 출력 해석 통일 · 시간 제한 · bash -c 감싸기 ----

func TestParseGossh(t *testing.T) {
	hosts := []string{"n01.lab.local", "n02", "c1.a", "c1.b", "10.0.0.5", "web"}
	out := "\x1b[32mn01: line1\x1b[0m\r\n" + // 짧은 이름 출력 → FQDN 요청에 맞춤 (ANSI·CR 제거)
		"n02.lab.local: x\n" + // FQDN 출력 → 짧은 이름 요청에 맞춤
		"c1: amb\n" + // 짧은 이름이 c1.a·c1.b 둘과 겹침 → 맞추지 않음
		"10.0.0.9: ip\n" + // IP 는 짧은 이름 비교 대상 아님
		"10.0.0.5: ipok\n" +
		"진행 3/6\x1b[K\rweb: after-cr\n" + // 진행률 덮어쓰기: 마지막 CR 뒤만
		"n02.lab.local: ERROR: 종료코드 2\n" +
		"web: SSH 접속 실패: dial tcp: i/o timeout\n" +
		"web: ERROR: 종료코드 255\n" +
		"[안전장치] 안내: 무시\n" +
		"other: 무관\n"
	g := parseGossh(out, hosts)
	if g.Head != nil {
		t.Fatalf("인식한 호스트가 있으면 Head 없음: %q", g.Head)
	}
	want := map[string]string{"n01.lab.local": "line1", "n02": "x", "10.0.0.5": "ipok", "web": "after-cr"}
	if len(g.Lines) != len(want) {
		t.Fatalf("Lines: %q", g.Lines)
	}
	for h, l := range want {
		if len(g.Lines[h]) != 1 || g.Lines[h][0] != l {
			t.Fatalf("%s: %q", h, g.Lines[h])
		}
	}
	if g.Why["n02"] != "ERROR: 종료코드 2" || g.Why["web"] != "SSH 접속 실패: dial tcp: i/o timeout" || len(g.Why) != 2 {
		t.Fatalf("Why: %q", g.Why)
	}

	// 출력은 있는데 호스트를 하나도 못 알아봄 → 원본 앞 5줄
	g = parseGossh("Usage: gossh\n\nfoo bar: x\n1\n2\n3\n4\n", hosts)
	if len(g.Head) != 5 || g.Head[0] != "Usage: gossh" || g.Head[1] != "foo bar: x" {
		t.Fatalf("Head: %q", g.Head)
	}
	if g = parseGossh(" \n\r\n", hosts); g.Head != nil {
		t.Fatalf("빈 출력은 해석 실패 아님: %q", g.Head)
	}
}

func TestParseCheckFQDNAndMismatch(t *testing.T) {
	setupDir(t)
	res := parseCheck([]string{"n01.lab", "n02"}, "n01: 12.5 1.0\nn02.lab: ERROR: 종료코드 1\n", "")
	if !res["n01.lab"].Responded || res["n01.lab"].Uptime != 12.5 || res["n02"].Why != "ERROR: 종료코드 1" {
		t.Fatalf("FQDN·사유: %+v", res)
	}

	gosshLogLast = ""
	bad := "\x1b[31mUsage: gossh [옵션]\x1b[0m\n알 수 없는 옵션 -pm\n"
	for i := 0; i < 2; i++ { // 같은 실패가 반복돼도 로그는 한 번
		res = parseCheck([]string{"a", "b"}, bad, "")
		if res["a"].Responded || res["a"].Why != whyMismatch || res["b"].Why != whyMismatch {
			t.Fatalf("형식 불일치: %+v", res)
		}
	}
	b, _ := os.ReadFile(logPath())
	if n := strings.Count(string(b), "[!] gossh 출력 해석 실패 (준비확인)"); n != 1 || !strings.Contains(string(b), `"Usage: gossh [옵션]"`) {
		t.Fatalf("로그(%d회):\n%s", n, b)
	}
}

// fakeCheckBin: PATH 맨 앞에 가짜 ssh(로그인 셸 흉내: -o 옵션·호스트를 건너뛰고 $LOGIN_SH -c "<원격 명령>") 를 둔다.
// gossh 는 공백·' 가 든 경로에 만들고 bin/gossh 로도 연결한다 (os6_gossh 인용 확인 / local 경로).
// 가짜 gossh 는 a 에 uptime, b 는 _os_install, HANG(자식 pid 파일) 이면 자식을 남기고 멈춤.
func fakeCheckBin(t *testing.T) (gossh string) {
	t.Helper()
	needBash(t)
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bin")
	gdir := filepath.Join(tmp, "g dir 'q' $x")
	os.MkdirAll(bin, 0755)
	os.MkdirAll(gdir, 0755)
	os.WriteFile(filepath.Join(bin, "ssh"), []byte(`#!/bin/bash
while [ "$1" = -o ]; do shift 2; done
shift
exec "${LOGIN_SH:-sh}" -c "$1"
`), 0755)
	gossh = filepath.Join(gdir, "it's gossh")
	os.WriteFile(gossh, []byte(`#!/bin/bash
w=""; last=""
while [ $# -gt 0 ]; do case $1 in -w) w=$2; shift 2;; *) last=$1; shift;; esac; done
[ "$last" = "cat /proc/uptime; date +%s; stat -c %Y /root/anaconda-ks.cfg; :" ] || { echo "bad cmd: $last" >&2; exit 9; }
grep -qx a "$w" && echo "a: 3.5 1.0"
grep -qx b "$w" && echo b > "${w}_os_install"
if [ -n "$HANG" ]; then sleep 30 & echo $! > "$HANG"; sleep 30; fi
exit 0
`), 0755)
	if err := os.Symlink(gossh, filepath.Join(bin, "gossh")); err != nil { // local 경로(PATH 의 gossh)도 같은 가짜
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return gossh
}

// os6 준비확인 원격 명령: 로그인 셸이 sh 든 tcsh 든 bash 가 $(mktemp)·${f} 를 해석하고, os6_gossh 의 공백·' 가 깨지지 않는다
func TestCheckOS6LoginShells(t *testing.T) {
	setupDir(t)
	gossh := fakeCheckBin(t)
	oldM, oldG := os6_mgmt, os6_gossh
	os6_mgmt, os6_gossh = "mgmtstub", gossh
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })
	shells := []string{"sh"}
	if _, err := exec.LookPath("tcsh"); err == nil {
		shells = append(shells, "tcsh")
	} else {
		t.Log("tcsh 없음 — sh 로만 확인")
	}
	for _, sh := range shells {
		t.Setenv("LOGIN_SH", sh)
		res, err := NewRealChecker().Check("os6", []string{"a", "b", "c"})
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if !res["a"].Responded || res["a"].Uptime != 3.5 || !res["b"].Anaconda || res["c"].Responded {
			t.Fatalf("%s: %+v", sh, res)
		}
	}
}

// 시간 제한: gossh 가 멈추면 checkTimeout 뒤 프로세스 그룹(자식 포함)을 죽이고, 받은 데까지 반영 + 나머지는 시간 초과
func TestCheckTimeoutKillsGroup(t *testing.T) {
	setupDir(t) // 가짜 ssh 의 "원격" bash 도 같은 프로세스 그룹이라 함께 SIGKILL 된다 — 원격 임시 파일이 테스트 디렉터리에 남게
	gossh := fakeCheckBin(t)
	old := checkTimeout
	checkTimeout = 500 * time.Millisecond
	t.Cleanup(func() { checkTimeout = old })
	oldM, oldG := os6_mgmt, os6_gossh
	os6_mgmt, os6_gossh = "mgmtstub", gossh
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })

	for _, route := range []string{"local", "os6"} {
		pidf := filepath.Join(t.TempDir(), "child")
		t.Setenv("HANG", pidf)
		start := time.Now()
		res, err := NewRealChecker().Check(route, []string{"a", "c"})
		if el := time.Since(start); el > 5*time.Second {
			t.Fatalf("%s: 시간 제한이 듣지 않음 (%v)", route, el)
		}
		if err != nil {
			t.Fatalf("%s: 시간 초과는 에러가 아니라 호스트별 사유: %v", route, err)
		}
		if !res["a"].Responded || res["a"].Uptime != 3.5 || res["c"].Responded || res["c"].Why != whyTimeout {
			t.Fatalf("%s: %+v", route, res)
		}
		pid, _ := os.ReadFile(pidf)
		p := strings.TrimSpace(string(pid))
		if p == "" {
			t.Fatalf("%s: 자식 pid 없음", route)
		}
		dead := false
		for i := 0; i < 40 && !dead; i++ {
			dead = exec.Command("kill", "-0", p).Run() != nil
			if !dead {
				time.Sleep(50 * time.Millisecond)
			}
		}
		if !dead {
			exec.Command("kill", "-9", p).Run()
			t.Fatalf("%s: 손자 프로세스(sleep %s)가 남음 — 프로세스 그룹 종료 실패", route, p)
		}
	}
}

// remoteBash: ' 가 든 명령도 sh·tcsh 로그인 셸을 거쳐 bash 가 그대로 받는다
func TestRemoteBashQuoting(t *testing.T) {
	needBash(t)
	inner := `f=$(mktemp); printf '%s|' "it's" "${f:+tmp}" "$((1+2))"; rm -f $f; echo`
	if got := remoteBash(inner); got != `bash -c 'f=$(mktemp); printf '\''%s|'\'' "it'\''s" "${f:+tmp}" "$((1+2))"; rm -f $f; echo'` {
		t.Fatalf("형태: %s", got)
	}
	shells := []string{"sh"}
	if _, err := exec.LookPath("tcsh"); err == nil {
		shells = append(shells, "tcsh")
	}
	for _, sh := range shells {
		out, err := exec.Command(sh, "-c", remoteBash(inner)).CombinedOutput()
		if err != nil || string(out) != "it's|tmp|3|\n" {
			t.Fatalf("%s: %q %v", sh, out, err)
		}
	}
}

// remoteBash: ! 가 든 inner 는 base64 형태로 가서 sh·bash·csh·tcsh 로그인 셸이 모두 같은 결과 (tcsh 의 history 치환 우회).
// 원격 stdin 은 inner 가 그대로 읽는다.
func TestRemoteBashBang(t *testing.T) {
	needBash(t)
	inner := `read -r l; printf '%s|' "$l" 'a!b' "it's" '\!x' '$HOME' "q\"" 'p|q'; [ ! -z "$l" ] && echo 'ok!'; echo $((1+2))`
	got := remoteBash(inner)
	if strings.ContainsAny(got, "!'$\\") || strings.Contains(got, "\n") || !strings.HasPrefix(got, `bash -c "`+"`echo ") {
		t.Fatalf("형태: %s", got)
	}
	// gossh 로 가는 경로(client.go remoteRun)에서 위험어·/user/ 가 우연히 생기면 안 된다
	long := remoteBash(strings.Repeat("echo reboot! halt ddc /user/ init 0 \x00\xff", 40))
	for _, w := range []string{"reboot", "poweroff", "shutdown", "halt", "init 0", "init 6", "ddc", "/user/"} {
		if strings.Contains(strings.ToLower(got), w) || strings.Contains(strings.ToLower(long), w) {
			t.Fatalf("위험어 %q 포함", w)
		}
	}
	const want = "stdin line|a!b|it's|\\!x|$HOME|q\"|p|q|ok!\n3\n"
	shells := []string{"sh", "bash", "csh", "tcsh"}
	ran := 0
	for _, sh := range shells {
		if _, err := exec.LookPath(sh); err != nil {
			continue
		}
		cmd := exec.Command(sh, "-c", got)
		cmd.Stdin = strings.NewReader("stdin line\n")
		out, err := cmd.CombinedOutput()
		if err != nil || string(out) != want {
			t.Fatalf("%s: %q %v", sh, out, err)
		}
		ran++
	}
	if ran == 0 {
		t.Skip("셸 없음")
	}
	// ! 가 없으면 기존 형태 그대로
	if got := remoteBash("echo hi"); got != "bash -c 'echo hi'" {
		t.Fatalf("! 없음: %s", got)
	}
}

// os6 ssh 가 실패하면 ssh 가 낸 사유(출력)를 오류 메시지에 함께 보여 준다
func TestCheckOS6ErrorShowsReason(t *testing.T) {
	oldM, oldG := os6_mgmt, os6_gossh
	os6_mgmt, os6_gossh = "mgmtstub", "/stub/gossh"
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })
	fakeExternal(t, func(stdin, name string, args ...string) (string, error) {
		return "Permission denied (publickey,password).\n", errors.New("exit status 255")
	})
	_, err := NewRealChecker().Check("os6", []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "exit status 255") || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("사유가 오류에 없음: %v", err)
	}
}

func TestDiagHead(t *testing.T) {
	if got := diagHead("\n  a \x1b[31mb\x1b[0m\n\nc\nd\ne\n"); got != "a b / c / d" {
		t.Fatalf("diagHead: %q", got)
	}
	if diagHead(" \n") != "" {
		t.Fatal("빈 출력")
	}
}
