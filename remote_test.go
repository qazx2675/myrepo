package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// 표식 환경변수가 정확히 1 일 때만 askpass 모드.
func TestAskpassMode(t *testing.T) {
	for v, want := range map[string]bool{"1": true, "": false, "0": false, "yes": false} {
		got := askpassMode(func(k string) string {
			if k == askpassEnv {
				return v
			}
			return ""
		})
		if got != want {
			t.Errorf("%s=%q → %v, want %v", askpassEnv, v, got, want)
		}
	}
}

// ssh 환경: 기존 askpass 관련 값은 바꾸고, DISPLAY 는 있으면 유지·없으면 :0, 비밀번호는 넣지 않는다.
func TestSSHEnv(t *testing.T) {
	base := []string{"PATH=/usr/bin", "SSH_ASKPASS=/old", "SSH_ASKPASS_REQUIRE=never", askpassEnv + "=0", "HOME=/root"}
	got := sshEnv(base, "/opt/new_setup")
	want := []string{"PATH=/usr/bin", "HOME=/root", "DISPLAY=:0", "SSH_ASKPASS=/opt/new_setup", "SSH_ASKPASS_REQUIRE=force", askpassEnv + "=1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sshEnv=%q\nwant %q", got, want)
	}
	got = sshEnv([]string{"DISPLAY=host:10.0"}, "/x")
	if got[0] != "DISPLAY=host:10.0" || len(got) != 4 {
		t.Errorf("DISPLAY 유지 실패: %q", got)
	}
	for _, kv := range append(got, sshEnv(base, "/x")...) {
		if strings.Contains(kv, remotePassword) {
			t.Errorf("환경변수에 비밀번호가 들어 있습니다: %q", kv)
		}
	}
}

// ssh 인자: 비밀번호 인증 옵션, root@<B>, 마지막은 sh -c '<script>'. 비밀번호는 없다.
func TestSSHArgv(t *testing.T) {
	a := sshArgv("b1", "echo 'x'")
	n := len(a)
	if a[n-2] != "root@b1" || a[n-1] != `sh -c 'echo '\''x'\'''` {
		t.Errorf("대상·명령 = %q %q", a[n-2], a[n-1])
	}
	j := strings.Join(a, " ")
	for _, s := range []string{"PreferredAuthentications=password,keyboard-interactive", "PubkeyAuthentication=no", "StrictHostKeyChecking=no",
		"UserKnownHostsFile=/dev/null", "NumberOfPasswordPrompts=1", "ConnectTimeout=15", "ServerAliveInterval=30", "ServerAliveCountMax=4"} {
		if !strings.Contains(j, s) {
			t.Errorf("ssh 옵션 %q 없음: %s", s, j)
		}
	}
	if strings.Contains(j, remotePassword) || strings.Contains(j, "BatchMode") {
		t.Errorf("ssh 인자에 비밀번호·BatchMode 가 있으면 안 됩니다: %s", j)
	}
}

func TestStressArgs(t *testing.T) {
	cases := []struct {
		steps, end, conf string
		ign              bool
		want             string
	}{
		{"a", "2026,1,2,3,4", "", false, "-user u -hosts /l -steps a -batch"},
		{"b", "2026,1,2,3,4", "/c", false, "-user u -hosts /l -steps b -batch -end 2026,1,2,3,4 -conf /c"},
		{"a", "", "/c", true, "-user u -hosts /l -steps a -batch -conf /c -ignore-offline"},
		{"b", "", "", true, "-user u -hosts /l -steps b -batch -ignore-offline"},
	}
	for _, c := range cases {
		if got := strings.Join(stressArgs("u", "/l", c.steps, c.end, c.conf, c.ign), " "); got != c.want {
			t.Errorf("stressArgs(%s,%s,%s,%v)=%q want %q", c.steps, c.end, c.conf, c.ign, got, c.want)
		}
	}
}

// 원격 스크립트: 인자마다 작은따옴표, pid 기록 뒤 exec. 표시 줄은 따옴표·비밀번호 없이.
func TestRemoteStressScript(t *testing.T) {
	c := config{StressTest: "/opt/st/stress-test", StressTestConf: "/opt/st/conf/u's.conf", StressRemote: "b1", StressIgnoreOffline: true}
	script, disp := remoteStressScript(c, "hong", "b", "/tmp/new_setup_hong/stress.txt", "2026,11,10,9,0")
	wantS := `cd '/opt/st' && echo $$ > '/tmp/new_setup_hong/b.pid' && exec '/opt/st/stress-test' '-user' 'hong' '-hosts' '/tmp/new_setup_hong/stress.txt'` +
		` '-steps' 'b' '-batch' '-end' '2026,11,10,9,0' '-conf' '/opt/st/conf/u'\''s.conf' '-ignore-offline'`
	if script != wantS {
		t.Errorf("script=\n%s\nwant\n%s", script, wantS)
	}
	wantD := "ssh root@b1 (cd /opt/st && /opt/st/stress-test -user hong -hosts /tmp/new_setup_hong/stress.txt -steps b -batch -end 2026,11,10,9,0 -conf /opt/st/conf/u's.conf -ignore-offline)"
	if disp != wantD {
		t.Errorf("disp=%s\nwant %s", disp, wantD)
	}
	// -yaml-state 에는 -ignore-offline 을 넣지 않는다
	ys, yd := remoteYamlScript(c, "hong")
	if ys != `cd '/opt/st' && exec '/opt/st/stress-test' '-user' 'hong' '-yaml-state' '-conf' '/opt/st/conf/u'\''s.conf'` {
		t.Errorf("yaml script=%s", ys)
	}
	if yd != "ssh root@b1 (cd /opt/st && /opt/st/stress-test -user hong -yaml-state -conf /opt/st/conf/u's.conf)" {
		t.Errorf("yaml disp=%s", yd)
	}
	if got := remoteCopyScript("/tmp/new_setup_hong/cpu.txt"); got != `mkdir -p '/tmp/new_setup_hong' && cat > '/tmp/new_setup_hong/cpu.txt'` {
		t.Errorf("copy=%s", got)
	}
	if got := remoteKillScript("hong", "a"); got != `kill -TERM $(cat '/tmp/new_setup_hong/a.pid' 2>/dev/null) 2>/dev/null; rm -f '/tmp/new_setup_hong/a.pid'` {
		t.Errorf("kill=%s", got)
	}
	if got := remoteDir("a/b"); got != "/tmp/new_setup_a_b" {
		t.Errorf("remoteDir=%s", got)
	}
}

// 이상한 문자(공백·따옴표·$)가 든 경로·사용자도 sh 에서 인자 그대로 전달되는지 실제 sh 로 확인(ssh 의 원격 셸 흉내).
func TestRemoteScriptRunsInSh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만")
	}
	d := filepath.Join(t.TempDir(), "s t'$x")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(d, "args")
	bin := filepath.Join(d, "st")
	os.WriteFile(bin, []byte("#!/bin/sh\npwd > '"+strings.ReplaceAll(out, "'", `'\''`)+".pwd'\nprintf '%s\\n' \"$@\" > '"+strings.ReplaceAll(out, "'", `'\''`)+"'\necho $$ >> '"+strings.ReplaceAll(out, "'", `'\''`)+".pid'\n"), 0o755)
	user := fmt.Sprintf("ut%d q'$u", os.Getpid())
	t.Cleanup(func() { os.RemoveAll(remoteDir(user)) })
	list := remoteDir(user) + "/cpu.txt"
	sh := func(script, stdin string) {
		t.Helper()
		a := sshArgv("b1", script)
		cmd := exec.Command("sh", "-c", a[len(a)-1])
		cmd.Stdin = strings.NewReader(stdin)
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("sh 실패: %v %s", err, o)
		}
	}
	sh(remoteCopyScript(list), "h1\nh2\n")
	if b, _ := os.ReadFile(list); string(b) != "h1\nh2\n" {
		t.Errorf("복사 내용=%q", b)
	}
	c := config{StressTest: bin, StressTestConf: "/c f/x'.conf", StressRemote: "b1"}
	script, _ := remoteStressScript(c, user, "a", list, "")
	sh(script, "")
	b, _ := os.ReadFile(out)
	want := strings.Join([]string{"-user", user, "-hosts", list, "-steps", "a", "-batch", "-conf", "/c f/x'.conf"}, "\n") + "\n"
	if string(b) != want {
		t.Errorf("전달된 인자=%q\nwant %q", b, want)
	}
	if b, _ := os.ReadFile(out + ".pwd"); strings.TrimSpace(string(b)) != d {
		t.Errorf("작업 폴더=%q want %q", b, d)
	}
	// pid 파일의 pid 는 exec 로 바뀐 stress-test 자신의 pid
	pf, _ := os.ReadFile(remotePidFile(user, "a"))
	pb, _ := os.ReadFile(out + ".pid")
	if strings.TrimSpace(string(pf)) == "" || strings.TrimSpace(string(pf)) != strings.TrimSpace(string(pb)) {
		t.Errorf("pid 파일=%q 실제=%q", pf, pb)
	}
	// 끝난 pid 가 다른 프로세스에 재사용됐을 수 있으므로 없는 pid 로 바꿔 두고 kill: 조용히 지나가고 pid 파일을 지운다
	os.WriteFile(remotePidFile(user, "a"), []byte("999999999\n"), 0o644)
	sh(remoteKillScript(user, "a"), "")
	if _, err := os.Stat(remotePidFile(user, "a")); err == nil {
		t.Errorf("kill 뒤 pid 파일이 남아 있습니다")
	}
}

// 원격 모드: STRESS_TEST·STRESS_TEST_CONF 는 B 의 경로라 ~ 를 풀지 않고, 설정 파일 폴더 기준으로도 바꾸지 않는다.
func TestParseConfRemote(t *testing.T) {
	c := parseConf("STRESS_TEST=/opt/st/stress-test\nSTRESS_TEST_CONF=\"~/x.conf\"\nSTRESS_REMOTE= \"b1\" \nSTRESS_IGNORE_OFFLINE=Y\n")
	if c.StressRemote != "b1" || !c.StressIgnoreOffline || c.StressTest != "/opt/st/stress-test" || c.StressTestConf != "~/x.conf" {
		t.Errorf("원격 설정 = %+v", c)
	}
	if err := remoteConfErr(c); err == nil || !strings.Contains(err.Error(), "STRESS_TEST_CONF") {
		t.Errorf("~ 경로는 원격 모드에서 오류여야 합니다: %v", err)
	}
	// 키 순서가 바뀌어도(STRESS_REMOTE 가 뒤) 같다
	c = parseConf("STRESS_TEST=~/st\nSTRESS_REMOTE=b1\n")
	if c.StressTest != "~/st" {
		t.Errorf("STRESS_TEST=%s", c.StressTest)
	}
	// 로컬 모드는 지금과 같다
	c = parseConf("STRESS_TEST=st\nSTRESS_IGNORE_OFFLINE=n\n")
	if c.StressRemote != "" || c.StressIgnoreOffline || remoteConfErr(c) != nil {
		t.Errorf("로컬 설정 = %+v", c)
	}
	c = parseConf("STRESS_IGNORE_OFFLINE=maybe\n")
	if c.StressIgnoreOffline || len(c.Warnings) != 1 || !strings.Contains(c.Warnings[0], "STRESS_IGNORE_OFFLINE") {
		t.Errorf("잘못된 값 → n + 경고: %v %q", c.StressIgnoreOffline, c.Warnings)
	}
}

func TestRemoteConfErr(t *testing.T) {
	cases := []struct {
		c  config
		ok bool
	}{
		{config{StressTest: "rel/st"}, true}, // 로컬 모드는 검사하지 않음
		{config{StressRemote: "b1", StressTest: "/opt/st"}, true},
		{config{StressRemote: "10.0.0.5", StressTest: "/opt/st", StressTestConf: "/opt/c.conf"}, true},
		{config{StressRemote: "b1", StressTest: "rel/st"}, false},
		{config{StressRemote: "b1", StressTest: "/opt/st", StressTestConf: "c.conf"}, false},
		{config{StressRemote: "-oProxyCommand=x", StressTest: "/opt/st"}, false},
		{config{StressRemote: "u@b1", StressTest: "/opt/st"}, false},
		{config{StressRemote: "b1 b2", StressTest: "/opt/st"}, false},
	}
	for _, c := range cases {
		if err := remoteConfErr(c.c); (err == nil) != c.ok {
			t.Errorf("remoteConfErr(%+v)=%v, ok=%v 기대", c.c, err, c.ok)
		}
	}
}

// 원격 모드 설정 파일: 상대경로는 설정 폴더 기준으로 바꾸지 않고 오류, 절대경로는 그대로.
func TestLoadRunConfRemote(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "new_setup.conf")
	os.WriteFile(p, []byte("STRESS_REMOTE=b1\nSTRESS_TEST=stress-test/stress-test\n"), 0o644)
	if _, err := loadRunConf(p); err == nil || !strings.Contains(err.Error(), "절대경로") {
		t.Errorf("상대경로 → 오류여야 합니다: %v", err)
	}
	os.WriteFile(p, []byte("STRESS_REMOTE=b1\nSTRESS_TEST=/root/st/stress-test\nSTRESS_TEST_CONF=/root/st/conf/a.conf\n"), 0o644)
	c, err := loadRunConf(p)
	if err != nil || c.StressTest != "/root/st/stress-test" || c.StressTestConf != "/root/st/conf/a.conf" {
		t.Errorf("절대경로 그대로: %+v %v", c, err)
	}
}

// ssh 종료코드 255 만 접속 실패로 본다.
func TestIsSSHFail(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만")
	}
	for code, want := range map[int]bool{255: true, 1: false, 0: false} {
		err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
		if got := isSSHFail(err); got != want {
			t.Errorf("exit %d → %v", code, got)
		}
	}
	if isSSHFail(errors.New("x")) || isSSHFail(nil) {
		t.Errorf("ExitError 가 아니면 false")
	}
	if e := sshFailErr("b1").Error(); e != "B서버(b1) ssh 접속 실패(종료코드 255)" {
		t.Errorf("문구=%s", e)
	}
}

// doctor 원격 모드: 로컬 STRESS_TEST 를 보지 않고 B 접속 확인 결과를 한 줄로, 비밀번호는 출력하지 않는다.
func TestDoctorStressRemote(t *testing.T) {
	x := newDocTest(t)
	x.d.lookPath = func(n string) (string, error) { return "/usr/bin/" + n, nil }
	var gotHost, gotScript string
	x.d.remoteRun = func(host, script string) (string, error) {
		gotHost, gotScript = host, script
		return "ok\n", nil
	}
	conf := config{StressRemote: "b1", StressTest: "/root/st/stress-test", StressTestConf: "/root/st/c.conf", StressIgnoreOffline: true}
	r := &docReport{}
	if h := docStress(r, conf, "tester", x.d); h != "" {
		t.Errorf("원격 모드 history = %q", h)
	}
	text := r.b.String()
	mustContain(t, text, "STRESS_REMOTE=b1", "ssh 접속 확인, STRESS_TEST 실행 가능: /root/st/stress-test", "STRESS_TEST_CONF 파일이 없습니다: /root/st/c.conf", "-ignore-offline")
	mustNotContain(t, text, remotePassword, "STRESS_TEST 사용 불가")
	if gotHost != "b1" || !strings.Contains(gotScript, "test -x '/root/st/stress-test'") {
		t.Errorf("확인 명령 %s %s", gotHost, gotScript)
	}
	// 접속 실패(255)
	x.d.remoteRun = func(string, string) (string, error) {
		return "Permission denied", exec.Command("sh", "-c", "exit 255").Run()
	}
	if runtime.GOOS == "linux" {
		r = &docReport{}
		docStress(r, conf, "tester", x.d)
		mustContain(t, r.b.String(), "B서버(b1) ssh 접속 실패(종료코드 255)")
	}
	// confDump 에 새 키
	mustContain(t, confDump(conf), "STRESS_REMOTE", "STRESS_IGNORE_OFFLINE")
	mustNotContain(t, confDump(conf), remotePassword)
}
