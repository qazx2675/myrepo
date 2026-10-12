// tcsh_live_test.go - 진짜 ssh 로 로그인 셸이 tcsh/csh 인 계정에 접속해 os6_mgmt 경유 경로들을 확인하는 랩 전용 테스트.
// 환경변수 AS_TEST_LOGIN_MGMT=<user@host> (root 키 인증으로 접속되는 계정, 예: ascsh@localhost) 가 있을 때만 실행한다.
// gossh 는 가짜 스크립트(대상 계정의 /tmp 아래에 둠) — 호스트 목록 파일의 각 호스트마다 명령을 bash -c 로 실행하고 "host: " 를 붙여 출력한다.
// 확인하는 것: 명령에 ! 가 들어 있어도(tcsh 의 history 치환) 준비확인·LDAP gosshBatch·2차 체크·gossh 작업·원격 클라이언트가 그대로 동작한다.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const liveFakeGossh = `#!/bin/bash
# 가짜 gossh: gossh [-pm] [-script] [-dnl…] -w <목록> '<명령>'  (os6 쪽 gossh 흉내)
w=""; cmd=""
while [ $# -gt 0 ]; do
  case "$1" in
    -w) w=$2; shift 2;;
    -pm|-script|-dnlgjawkrdjqghkrdls) shift;;
    *) cmd=$1; shift;;
  esac
done
while read -r h; do
  [ -n "$h" ] || continue
  bash -c "$cmd" </dev/null 2>&1 | sed "s/^/$h: /"
done < "$w"
`

// 로컬 gossh 흉내 (os8 쪽 PATH 의 gossh): 목록의 호스트(=ssh 대상)에 명령을 ssh 로 실행하고 "host: " 를 붙인다. 계정 로그인 셸이 명령을 해석한다.
const liveLocalGossh = `#!/bin/bash
w=""; cmd=""
while [ $# -gt 0 ]; do
  case "$1" in
    -w) w=$2; shift 2;;
    -pm|-script) shift;;
    *) cmd=$1; shift;;
  esac
done
while read -r h; do
  [ -n "$h" ] || continue
  ssh -n -o BatchMode=yes "$h" "$cmd" 2>&1 | sed "s/^/$h: /"
done < "$w"
`

func liveSetup(t *testing.T) (mgmt, dir string) {
	t.Helper()
	mgmt = os.Getenv("AS_TEST_LOGIN_MGMT")
	if mgmt == "" {
		t.Skip("AS_TEST_LOGIN_MGMT 없음 (랩 전용)")
	}
	needBash(t)
	d, err := os.MkdirTemp("", "as_live_")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	os.Chmod(d, 0755) // 대상 계정이 읽고 실행할 수 있게
	return mgmt, d
}

func liveWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(p, 0755)
}

// 준비확인(checkOS6) · LDAP gosshBatch(os6) · 2차 체크 원격 명령 · gossh 작업(workOS6Argv)
func TestOS6ViaLoginShell(t *testing.T) {
	mgmt, dir := liveSetup(t)
	gossh := filepath.Join(dir, "gossh")
	liveWrite(t, gossh, liveFakeGossh)
	oldM, oldG := os6_mgmt, os6_gossh
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })
	os6_mgmt, os6_gossh = mgmt, gossh
	hosts := []string{"h1", "h2"}

	// 1. 준비확인
	res, err := checkOS6(hosts)
	if err != nil {
		t.Fatalf("checkOS6: %v", err)
	}
	for _, h := range hosts {
		if r := res[h]; !r.Responded || r.Uptime <= 0 {
			t.Errorf("checkOS6 %s: %+v", h, r)
		}
	}

	// 2. LDAP gosshBatch — 명령에 ! 포함 (직접 + buildRemoteCmd 로 감싼 스크립트)
	cmd := `echo "it's! ok"; [ ! -f /nonexistent_as_live ] && echo nofile`
	out, err := gosshBatch("os6", hosts, cmd, 30*time.Second)
	if err != nil {
		t.Fatalf("gosshBatch: %v", err)
	}
	for _, h := range hosts {
		if got := strings.Join(out[h], "|"); got != "it's! ok|nofile" {
			t.Errorf("gosshBatch %s: %q", h, got)
		}
	}
	out, err = gosshBatch("os6", hosts[:1], buildRemoteCmd(applRemoteScript), 30*time.Second)
	if err != nil || len(out["h1"]) == 0 || !strings.Contains(strings.Join(out["h1"], "\n"), "APPL|") {
		t.Errorf("gosshBatch(buildRemoteCmd): %v %v", out, err)
	}

	// 3. 2차 체크 — 원격 stdin(목록)·임시 디렉터리·tar 회수 (사용자 이름에 ! 가 있어도)
	osCheck := filepath.Join(dir, "os_check.sh")
	liveWrite(t, osCheck, `#!/bin/bash
# 가짜 os_check: -auto <user> <targets>
echo "run $*! done" > os_check.log
echo "$(cat "$3" | paste -sd,)" > "check.res_${2}_postapply"
`)
	oldC := os6_os_check_sh
	t.Cleanup(func() { os6_os_check_sh = oldC })
	os6_os_check_sh = osCheck
	user := "al!ce"
	so, err := secondSSH(hostList(hosts), secondRunScript(user))
	if err != nil {
		t.Fatalf("secondSSH: %v\n%s", err, so)
	}
	rdir := ""
	for _, l := range strings.Split(so, "\n") {
		if strings.HasPrefix(l, secondDirMarker) {
			rdir = strings.TrimSpace(strings.TrimPrefix(l, secondDirMarker))
		}
	}
	if rdir == "" || !secondDirRe.MatchString(rdir) || !strings.Contains(so, secondRCMarker+"0") {
		t.Fatalf("2차 실행 출력:\n%s", so)
	}
	tarOut, err := secondSSH("", secondFetchScript(rdir, user))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	td := t.TempDir()
	if err := extractSecondTar(tarOut, td); err != nil {
		t.Fatalf("tar: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(td, "os_check.log")); string(b) != "run -auto al!ce targets.txt! done\n" && !strings.Contains(string(b), "al!ce") {
		t.Errorf("os_check.log: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(td, "check.res_al!ce_postapply")); string(b) != "h1,h2\n" {
		t.Errorf("목록이 원격 stdin 으로 전달되어야 함: %q", b)
	}

	// 4. gossh 작업 — os6_mgmt 경유 (ssh -t), 사용자 명령에 ! 와 따옴표
	wcmd := `echo "a!b it's"; [ ! -z x ] && echo done; echo 'x!y' "$((1+2))"`
	argv := workOS6Argv(hosts, wcmd, false)
	b, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		t.Fatalf("workOS6Argv: %v\n%s", err, b)
	}
	got := strings.ReplaceAll(string(b), "\r", "")
	for _, h := range hosts {
		for _, w := range []string{h + ": a!b it's", h + ": done", h + ": x!y 3"} {
			if !strings.Contains(got, w+"\n") {
				t.Errorf("gossh 작업 출력에 %q 없음:\n%s", w, got)
			}
		}
	}
}

// 래퍼 두 개(runner.go / changerun.go)가 보내는 원격 명령 — gossh 인자에 ! 가 있어도
func TestOS6WrappersViaLoginShell(t *testing.T) {
	mgmt, dir := liveSetup(t)
	gossh := filepath.Join(dir, "gossh")
	liveWrite(t, gossh, liveFakeGossh)
	oldM, oldG := os6_mgmt, os6_gossh
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })
	os6_mgmt, os6_gossh = mgmt, gossh
	list := filepath.Join(t.TempDir(), "list.txt")
	os.WriteFile(list, []byte("h1\nh2\n"), 0644)
	chg := filepath.Join(dir, "chg.sh")
	liveWrite(t, chg, chgOS6WrapperScript(mgmt, gossh))
	run := filepath.Join(dir, "run.sh")
	liveWrite(t, run, os6WrapperScript())
	for name, w := range map[string]string{"os6WrapperScript": run, "chgOS6WrapperScript": chg} {
		b, err := exec.Command("bash", w, "-pm", "-script", "-w", list, `echo "q!r it's"; [ ! -z 1 ] && echo $((2*3))`).Output()
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, b)
		}
		if got := string(b); got != "h1: q!r it's\nh1: 6\nh2: q!r it's\nh2: 6\n" {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// 원격 클라이언트(client.go remoteRun): os8_mgmt 에서 auto_setup 을 gossh 원샷으로 — 인자에 ! 가 있어도
func TestRemoteRunViaLoginShell(t *testing.T) {
	mgmt, dir := liveSetup(t)
	liveWrite(t, filepath.Join(dir, "gossh"), liveLocalGossh)
	stub := filepath.Join(dir, "auto_setup")
	liveWrite(t, stub, "#!/bin/bash\nfor a in \"$@\"; do echo \"[$a]\"; done\necho err! >&2\nexit 7\n")
	oldM, oldB := os8_mgmt, os8_autosetup
	t.Cleanup(func() { os8_mgmt, os8_autosetup = oldM, oldB })
	os8_mgmt, os8_autosetup = mgmt, stub
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, err := remoteRun([]string{"request", "cancel", "a!b", "it's $x", "reboot", "ddc"})
	if err != nil {
		t.Fatalf("remoteRun: %v", err)
	}
	if r.RC != 7 || r.Stdout != "[request]\n[cancel]\n[a!b]\n[it's $x]\n[reboot]\n[ddc]\n" || r.Stderr != "err!\n" {
		t.Errorf("remoteRun: %+v", r)
	}
}
