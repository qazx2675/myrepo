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
	// 인자 해석은 main_test.go 의 TestParseArgs 가 본다
	o, ok := parseArgs([]string{"-c", "my.conf", "hosts.txt"})
	if !ok || o.conf != "my.conf" || o.hostfile != "hosts.txt" || o.mode != modeAttach {
		t.Errorf("parseArgs: %+v %v", o, ok)
	}

	p := filepath.Join(t.TempDir(), "hosts")
	writeFile(t, p, "a1\n\n# x\n  b1  \na1\r\nc1")
	got, err := readHosts(p)
	if err != nil || !reflect.DeepEqual(got, []string{"a1", "b1", "c1"}) {
		t.Errorf("readHosts: %v %v", got, err)
	}
}

func TestYumDelayMin(t *testing.T) {
	if got := defaultConfig().YumDelayMin; got != 60 {
		t.Errorf("yum_delay_min 기본값=%d, want 60", got)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "c.conf")
	writeFile(t, p, "# yum\n yum_delay_min = 30 \ndelay_min=90\n")
	cfg, err := LoadConfig(p)
	if err != nil || cfg.YumDelayMin != 30 || cfg.DelayMin != 90 {
		t.Fatalf("yum_delay_min 읽기: %v %+v", err, cfg)
	}
	// 설정 파일에 없으면 기본값, delay_min 과 따로 움직인다
	writeFile(t, p, "delay_min=15\n")
	cfg, err = LoadConfig(p)
	if err != nil || cfg.YumDelayMin != 60 || cfg.DelayMin != 15 {
		t.Fatalf("yum_delay_min 기본값 유지: %v %+v", err, cfg)
	}
	for _, line := range []string{"yum_delay_min=0", "yum_delay_min=-3", "yum_delay_min=abc", "yum_delay_min="} {
		writeFile(t, p, line+"\n")
		_, err := LoadConfig(p)
		if err == nil || !strings.Contains(err.Error(), "yum_delay_min 는 1 이상의 정수여야 함") {
			t.Errorf("%q 는 한글 오류여야 함: %v", line, err)
		}
	}
	// 견본 설정 파일은 주석을 풀어도 읽혀야 한다
	b, err := os.ReadFile("pm_monitor.conf.sample")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "#yum_delay_min=60") {
		t.Error("pm_monitor.conf.sample 에 yum_delay_min 예시가 없음")
	}
}
