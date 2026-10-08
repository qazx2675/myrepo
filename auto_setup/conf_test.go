// conf_test.go - conf.go 단위 테스트
package main

import (
	"os"
	"path/filepath"
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
