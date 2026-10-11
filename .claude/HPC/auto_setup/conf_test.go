// conf_test.go - conf.go 단위 테스트
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func saveConfVars(t *testing.T) {
	t.Helper()
	saved := map[string]string{}
	for k, p := range confVars() {
		saved[k] = *p
		*p = ""
	}
	t.Cleanup(func() {
		for k, p := range confVars() {
			*p = saved[k]
		}
	})
}

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), confFileName)
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConf(t *testing.T) {
	saveConfVars(t)
	os6_gossh = "/built/gossh" // 빌드 주입값은 유지
	p := writeConf(t, "# c\n\n os6_mgmt = host6 \nos6_gossh=/conf/gossh\nawx_dir=\"/a b/awx\"\nldap_share_dir='/share'\nunknown=x\nbadline\nos8_mgmt=\n")
	if got := loadConf([]string{filepath.Join(t.TempDir(), "none"), p}); got != p {
		t.Fatalf("읽은 파일 %q", got)
	}
	cases := map[string]string{
		"os6_mgmt": "host6", "os6_gossh": "/built/gossh", "awx_dir": "/a b/awx",
		"ldap_share_dir": "/share", "os8_mgmt": "", "os_check_sh": "",
	}
	for k, want := range cases {
		if got := *confVars()[k]; got != want {
			t.Errorf("%s = %q want %q", k, got, want)
		}
	}
}

func TestLoadConfNone(t *testing.T) {
	saveConfVars(t)
	if got := loadConf([]string{filepath.Join(t.TempDir(), "none")}); got != "" || os6_mgmt != "" {
		t.Errorf("파일 없음: %q %q", got, os6_mgmt)
	}
}

func TestLoadConfFirstFileOnly(t *testing.T) {
	saveConfVars(t)
	a := writeConf(t, "os6_mgmt=a\n")
	b := writeConf(t, "os6_mgmt=b\nawx_dir=/b\n")
	loadConf([]string{a, b})
	if os6_mgmt != "a" || awx_dir != "" {
		t.Errorf("os6_mgmt=%q awx_dir=%q", os6_mgmt, awx_dir)
	}
}

func TestConfPathsEnvFirst(t *testing.T) {
	t.Setenv("AUTO_SETUP_CONF", "/x/conf")
	if p := confPaths(); p[0] != "/x/conf/"+confFileName {
		t.Errorf("%v", p)
	}
}

// ---- v0.7.0 변경 메뉴: conf 키 · engineBin · changeConfDir · 가시성 · --admin ----

// saveChangeState: conf 변수를 비우고 adminMode·confLoadedFrom·engineExeDir 를 테스트 끝에 되돌린다
func saveChangeState(t *testing.T) {
	t.Helper()
	saveConfVars(t)
	a, f, e := adminMode, confLoadedFrom, engineExeDir
	t.Cleanup(func() { adminMode, confLoadedFrom, engineExeDir = a, f, e })
	adminMode, confLoadedFrom = false, ""
}

func TestLoadConfChangeKeys(t *testing.T) {
	saveChangeState(t)
	ip_engine = "/built/ip" // 빌드 주입값 유지
	p := writeConf(t, "enable_ip_change = Yes\nenable_ldap_change=\nip_engine=/conf/ip\nldap_engine=\"/opt/l e/ldap\"\n")
	loadConf([]string{p})
	cases := map[string]string{
		"enable_ip_change": "Yes", "enable_ldap_change": "", "ip_engine": "/built/ip", "ldap_engine": "/opt/l e/ldap",
	}
	for k, want := range cases {
		if got := *confVars()[k]; got != want {
			t.Errorf("%s = %q want %q", k, got, want)
		}
	}
}

func TestConfTrue(t *testing.T) {
	for v, want := range map[string]bool{
		"y": true, "Y": true, "yes": true, "YES": true, "Yes": true, "1": true, "true": true, "TRUE": true, " y ": true,
		"": false, "n": false, "no": false, "0": false, "false": false, "on": false, "2": false, "yy": false, "t": false,
	} {
		if got := confTrue(v); got != want {
			t.Errorf("confTrue(%q) = %v want %v", v, got, want)
		}
	}
}

func TestChangeEnabled(t *testing.T) {
	saveChangeState(t)
	if ipChangeEnabled() || ldapChangeEnabled() {
		t.Fatal("기본(빈 값, admin 아님)은 꺼짐이어야 한다")
	}
	enable_ip_change = "true"
	if !ipChangeEnabled() || ldapChangeEnabled() {
		t.Errorf("ip 만 켜짐: ip=%v ldap=%v", ipChangeEnabled(), ldapChangeEnabled())
	}
	enable_ip_change, enable_ldap_change = "no", "1"
	if ipChangeEnabled() || !ldapChangeEnabled() {
		t.Errorf("ldap 만 켜짐: ip=%v ldap=%v", ipChangeEnabled(), ldapChangeEnabled())
	}
	enable_ip_change, enable_ldap_change = "", ""
	adminMode = true
	if !ipChangeEnabled() || !ldapChangeEnabled() {
		t.Errorf("--admin 이면 conf 와 무관하게 둘 다 켜짐: ip=%v ldap=%v", ipChangeEnabled(), ldapChangeEnabled())
	}
}

func TestEngineBin(t *testing.T) {
	saveChangeState(t)
	exeDir, pathDir := t.TempDir(), t.TempDir()
	engineExeDir = func() string { return exeDir }
	t.Setenv("PATH", pathDir)
	mk := func(dir, name string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if got := engineBin(ipEngineName); got != "" {
		t.Fatalf("어디에도 없으면 빈 문자열: %q", got)
	}
	// PATH
	inPath := mk(pathDir, ipEngineName, 0755)
	if got := engineBin(ipEngineName); got != inPath {
		t.Errorf("PATH: %q want %q", got, inPath)
	}
	// 실행 파일 디렉터리가 PATH 보다 우선
	inExe := mk(exeDir, ipEngineName, 0755)
	if got := engineBin(ipEngineName); got != inExe {
		t.Errorf("exe dir 우선: %q want %q", got, inExe)
	}
	// 실행 권한이 없는 파일·디렉터리는 건너뛰고 PATH 로
	if err := os.Chmod(inExe, 0644); err != nil {
		t.Fatal(err)
	}
	if got := engineBin(ipEngineName); got != inPath {
		t.Errorf("실행 권한 없음 → PATH: %q want %q", got, inPath)
	}
	if err := os.MkdirAll(filepath.Join(exeDir, ldapEngineName), 0755); err != nil {
		t.Fatal(err)
	}
	if got := engineBin(ldapEngineName); got != "" {
		t.Errorf("디렉터리는 엔진이 아님: %q", got)
	}
	// conf 값은 존재 확인 없이 최우선, 두 엔진은 각자의 변수만 본다
	ip_engine = "/conf/ip-engine"
	if got := engineBin(ipEngineName); got != "/conf/ip-engine" {
		t.Errorf("ip_engine: %q", got)
	}
	if got := engineBin(ldapEngineName); got != "" {
		t.Errorf("ip_engine 이 ldap 엔진에 영향: %q", got)
	}
	ldap_engine = "/conf/ldap-engine"
	if got := engineBin(ldapEngineName); got != "/conf/ldap-engine" {
		t.Errorf("ldap_engine: %q", got)
	}
	// exe 디렉터리를 알 수 없어도 PATH 탐색은 동작
	ip_engine = ""
	engineExeDir = func() string { return "" }
	if got := engineBin(ipEngineName); got != inPath {
		t.Errorf("exe 디렉터리 없음 → PATH: %q want %q", got, inPath)
	}
}

func TestChangeConfDir(t *testing.T) {
	saveChangeState(t)
	if d := changeConfDir(); d != "/etc/auto_setup" {
		t.Errorf("읽은 conf 없음 → /etc/auto_setup: %q", d)
	}
	if p := changeConfPath("ldap_config.conf"); p != "/etc/auto_setup/ldap_config.conf" {
		t.Errorf("changeConfPath: %q", p)
	}
	// loadConf 가 읽은 파일의 디렉터리 (첫 번째로 존재하는 파일)
	p := writeConf(t, "os6_mgmt=x\n")
	loadConf([]string{filepath.Join(t.TempDir(), "none"), p})
	if d := changeConfDir(); d != filepath.Dir(p) {
		t.Errorf("changeConfDir = %q want %q", d, filepath.Dir(p))
	}
	for _, name := range []string{"ip_change.conf", "ldap_config.conf", "assets.txt"} {
		if got, want := changeConfPath(name), filepath.Join(filepath.Dir(p), name); got != want {
			t.Errorf("changeConfPath(%q) = %q want %q", name, got, want)
		}
	}
	// 다시 읽었는데 파일이 없으면 기본값으로 돌아간다
	loadConf([]string{filepath.Join(t.TempDir(), "none")})
	if d := changeConfDir(); d != "/etc/auto_setup" {
		t.Errorf("재로드 후 파일 없음: %q", d)
	}
}

func TestSplitAdmin(t *testing.T) {
	cases := []struct {
		in    []string
		rest  []string
		admin bool
	}{
		{nil, nil, false},
		{[]string{"--admin"}, nil, true},
		{[]string{"--admin", "--admin"}, nil, true},
		{[]string{"--plain"}, []string{"--plain"}, false},
		{[]string{"--admin", "--plain"}, []string{"--plain"}, false},
		{[]string{"--plain", "--admin"}, []string{"--plain"}, false},
		{[]string{"--admin", "--start"}, []string{"--start"}, false},
		{[]string{"--admin", "request", "refresh"}, []string{"request", "refresh"}, false},
		{[]string{"--admin", "-h"}, []string{"-h"}, false},
		{[]string{"start", "--admin"}, []string{"start", "--admin"}, false}, // 앞쪽이 아니면 그대로(→ 사용법 오류)
		{[]string{"done", "--admin"}, []string{"done", "--admin"}, false},   // 호스트 이름 자리는 건드리지 않음
		{[]string{"--plain", "x", "--admin"}, []string{"--plain", "x", "--admin"}, false},
	}
	for _, c := range cases {
		rest, admin := splitAdmin(c.in)
		if len(rest) != len(c.rest) || (len(rest) > 0 && !reflect.DeepEqual(rest, c.rest)) || admin != c.admin {
			t.Errorf("splitAdmin(%q) = %q, %v want %q, %v", c.in, rest, admin, c.rest, c.admin)
		}
	}
}

// run: --admin 은 TUI(무인자 리포트) 에서만 adminMode 를 켠다. 데몬 명령은 실행하지 않고 훅만 확인한다.
func TestRunAdminFlag(t *testing.T) {
	saveChangeState(t)
	old := runReport
	var gotPlain bool
	var called int
	runReport = func(src SnapshotSource, plain bool) int { gotPlain = plain; called++; return 7 }
	defer func() { runReport = old }()

	if rc := run([]string{"--admin"}); rc != 7 || called != 1 || !adminMode {
		t.Errorf("--admin: rc=%d called=%d adminMode=%v", rc, called, adminMode)
	}
	adminMode = false
	for _, args := range [][]string{{"--admin", "--plain"}, {"--plain", "--admin"}} {
		gotPlain = false
		if rc := run(args); rc != 7 || !gotPlain || adminMode {
			t.Errorf("%v: rc=%d plain=%v adminMode=%v (--plain 에는 영향 없어야 함)", args, rc, gotPlain, adminMode)
		}
	}
	// 플래그 없는 실행은 adminMode 를 끈다 (이전 호출 상태가 남지 않음)
	adminMode = true
	if rc := run([]string{"--plain"}); rc != 7 || adminMode {
		t.Errorf("--plain: rc=%d adminMode=%v", rc, adminMode)
	}
}

// 실제 바이너리: --admin 이 다른 명령의 인자 검사·출력을 바꾸지 않는다 (데몬은 건드리지 않는 명령만 사용)
func TestCLIAdminFlag(t *testing.T) {
	lb, _ := buildBins(t)
	env := localEnv(t, t.TempDir())
	if r := runBin(t, lb, env, "--admin", "-h"); r.RC != 0 || !strings.Contains(r.Out, "--admin") || r.Err != "" {
		t.Errorf("--admin -h: %+v", r)
	}
	p := runBin(t, lb, env, "--plain")
	for _, args := range [][]string{{"--admin", "--plain"}, {"--plain", "--admin"}} {
		if r := runBin(t, lb, env, args...); r != p {
			t.Errorf("%v 는 --plain 과 같아야 한다:\n%+v\n%+v", args, r, p)
		}
	}
	// 비 tty 의 --admin 은 plain 표 (무인자와 같음)
	if r, n := runBin(t, lb, env, "--admin"), runBin(t, lb, env); r != n {
		t.Errorf("--admin(비 tty) 와 무인자 불일치:\n%+v\n%+v", r, n)
	}
	// 앞쪽이 아닌 --admin 은 종전처럼 사용법 오류
	for _, args := range [][]string{{"--start", "--admin"}, {"code", "--admin", "1"}, {"--admin", "bogus"}} {
		if r := runBin(t, lb, env, args...); r.RC != 1 || r.Out != "" || !strings.Contains(r.Err, "사용법: auto_setup") {
			t.Errorf("%v: 사용법+exit 1 아님: %+v", args, r)
		}
	}
}
