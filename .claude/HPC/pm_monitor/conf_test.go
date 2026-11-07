package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMissingConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(dir)
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Warning == "" || cfg.Path != "" {
		t.Errorf("경고 필요: warning=%q path=%q", cfg.Warning, cfg.Path)
	}
	if cfg.IntervalSec != 60 || cfg.DelayMin != 60 || cfg.PingTimeoutMs != 1500 ||
		cfg.PingPPS != 500 || cfg.TCPParallel != 100 || cfg.Gossh != "gossh" ||
		cfg.VendorOther != "기타" || cfg.DHCPServer != "" || cfg.LdapCheckSh != "" {
		t.Errorf("기본값 불일치: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.DiskPatterns, []string{defaultDiskPattern}) {
		t.Errorf("disk_pattern 기본값: %v", cfg.DiskPatterns)
	}
}

func TestCwdFirstAndOverride(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(dir)
	writeFile(t, "pm_monitor.conf", "interval_sec=30\n")
	other := filepath.Join(dir, "other.conf")
	writeFile(t, other, "interval_sec=45\n")

	cfg, err := LoadConfig("")
	if err != nil || cfg.IntervalSec != 30 || cfg.Warning != "" {
		t.Fatalf("cwd 설정 우선 실패: %v %+v", err, cfg)
	}
	cfg, err = LoadConfig(other)
	if err != nil || cfg.IntervalSec != 45 {
		t.Fatalf("-c 지정 실패: %v %+v", err, cfg)
	}
	cfg, err = LoadConfig(filepath.Join(dir, "nope.conf"))
	if err != nil || cfg.Warning == "" {
		t.Fatalf("없는 -c 파일은 경고: %v %+v", err, cfg)
	}
}

func TestParseFull(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.conf")
	writeFile(t, p, strings.Join([]string{
		"# 주석", "", "  gossh = /usr/local/bin/gossh ", "interval_sec=10", "delay_min=90",
		"disk_pattern=pat one", "disk_pattern=pat=two",
		"vendor=^(a|b)=X=1", "vendor=^c=Y", "vendor_other=zz",
		"dhcp_server=dhcp1", "ldap_check_sh=/x/ldap_check.sh",
		"ping_timeout_ms=2000", "ping_pps=200", "tcp_parallel=50",
	}, "\n"))
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gossh != "/usr/local/bin/gossh" || cfg.IntervalSec != 10 || cfg.DelayMin != 90 ||
		cfg.DHCPServer != "dhcp1" || cfg.LdapCheckSh != "/x/ldap_check.sh" ||
		cfg.PingTimeoutMs != 2000 || cfg.PingPPS != 200 || cfg.TCPParallel != 50 ||
		cfg.VendorOther != "zz" || cfg.Path != p || cfg.Warning != "" {
		t.Errorf("파싱 결과 불일치: %+v", cfg)
	}
	if !reflect.DeepEqual(cfg.DiskPatterns, []string{"pat one", "pat=two"}) {
		t.Errorf("disk_pattern: %v", cfg.DiskPatterns)
	}
	// 마지막 '=' 기준으로 이름을 자른다
	if len(cfg.Vendors) != 2 || cfg.Vendors[0].Pattern != "^(a|b)=X" || cfg.Vendors[0].Name != "1" {
		t.Errorf("vendors: %+v", cfg.Vendors)
	}
	if cfg.Vendor("c1") != "Y" || cfg.Vendor("s1") != "zz" {
		t.Errorf("custom vendor 판별 실패")
	}
}

func TestBadValues(t *testing.T) {
	bad := []string{
		"interval_sec=0", "interval_sec=-5", "delay_min=abc", "ping_pps=0", "tcp_parallel=x",
		"ping_timeout_ms=", "vendor=^a", "vendor=^a=", "vendor=(=X", "unknown_key=1",
		"no_equal_line", "disk_pattern=", "vendor_other=",
	}
	dir := t.TempDir()
	for _, line := range bad {
		p := filepath.Join(dir, "b.conf")
		writeFile(t, p, line+"\n")
		if _, err := LoadConfig(p); err == nil {
			t.Errorf("오류여야 함: %q", line)
		}
	}
}

func TestParseArgsAndHosts(t *testing.T) {
	if _, _, ok := parseArgs(nil); ok {
		t.Error("인자 없음은 실패")
	}
	if _, _, ok := parseArgs([]string{"-c"}); ok {
		t.Error("-c 값 없음은 실패")
	}
	if _, _, ok := parseArgs([]string{"-x", "f"}); ok {
		t.Error("모르는 옵션은 실패")
	}
	c, h, ok := parseArgs([]string{"-c", "my.conf", "hosts.txt"})
	if !ok || c != "my.conf" || h != "hosts.txt" {
		t.Errorf("parseArgs: %q %q %v", c, h, ok)
	}
	c, h, ok = parseArgs([]string{"hosts.txt"})
	if !ok || c != "" || h != "hosts.txt" {
		t.Errorf("parseArgs: %q %q %v", c, h, ok)
	}

	p := filepath.Join(t.TempDir(), "hosts")
	writeFile(t, p, "a1\n\n# x\n  b1  \na1\r\nc1")
	got, err := readHosts(p)
	if err != nil || !reflect.DeepEqual(got, []string{"a1", "b1", "c1"}) {
		t.Errorf("readHosts: %v %v", got, err)
	}
}
