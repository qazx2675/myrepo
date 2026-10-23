package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goodConf = `# 주석
allowed_dir=/home/power_onoff
users = lsh, ljh ,dhk
vcenters=192.168.0.50,192.168.0.51
secret_lib=/home/V2/secret_lib.sh
concurrency=5   # 동시
shutdown_timeout=100
`

func TestParseConfigOK(t *testing.T) {
	cfg, err := ParseConfig(strings.NewReader(goodConf))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AllowedDir != "/home/power_onoff" || strings.Join(cfg.Users, "|") != "lsh|ljh|dhk" {
		t.Errorf("파싱 결과 이상: %+v", cfg)
	}
	if len(cfg.VCenters) != 2 || cfg.Concurrency != 5 || cfg.ShutdownTimeout != 100*time.Second {
		t.Errorf("값 이상: %+v", cfg)
	}
	// 기본값
	if cfg.VCID != "lscsystems@vsphere.local" || cfg.OS6User != "root" ||
		cfg.BootTimeout != 600*time.Second || cfg.BootPollInterval != 10*time.Second ||
		cfg.PortCheckTimeout != 3*time.Second || cfg.OS6Host != "" {
		t.Errorf("기본값 이상: %+v", cfg)
	}
}

func TestParseConfigErrors(t *testing.T) {
	base := "allowed_dir=/a\nusers=x\n"
	cases := map[string]string{
		"알 수 없는 키":    base + "foo=1\n",
		"중복 키":        base + "concurrency=1\nconcurrency=2\n",
		"숫자 0":        base + "boot_timeout=0\n",
		"숫자 음수":       base + "concurrency=-3\n",
		"숫자 문자":       base + "port_check_timeout=abc\n",
		"users 중복":    "allowed_dir=/a\nusers=a,b,a\n",
		"user 대문자":    "allowed_dir=/a\nusers=Abc\n",
		"user 특수문자":   "allowed_dir=/a\nusers=a;b\n",
		"users 빈 항목":  "allowed_dir=/a\nusers=a,,b\n",
		"users 없음":    "allowed_dir=/a\n",
		"users 빈값":    "allowed_dir=/a\nusers=\n",
		"allowed 없음":  "users=a\n",
		"allowed 상대":  "allowed_dir=rel/dir\nusers=a\n",
		"os6_host 주입": base + "os6_host=a;rm -rf /\n",
		"os6_host 공백": base + "os6_host=a b\n",
		"형식 오류":       base + "novalue\n",
	}
	for name, in := range cases {
		if _, err := ParseConfig(strings.NewReader(in)); err == nil {
			t.Errorf("%s: 오류가 나야 함", name)
		}
	}
	// 줄 번호 포함 확인
	_, err := ParseConfig(strings.NewReader(base + "\nfoo=1\n"))
	if err == nil || !strings.Contains(err.Error(), "4번째 줄") {
		t.Errorf("줄 번호가 없음: %v", err)
	}
}

func TestParseConfigOS6(t *testing.T) {
	cfg, err := ParseConfig(strings.NewReader("allowed_dir=/a\nusers=x\nos6_host=os6-mgmt.local\nos6_user=admin\n"))
	if err != nil || cfg.OS6Host != "os6-mgmt.local" || cfg.OS6User != "admin" {
		t.Fatalf("%v %+v", err, cfg)
	}
}

func writeConf(t *testing.T, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pwonoff.conf")
	if err := os.WriteFile(p, []byte(goodConf), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigPerm(t *testing.T) {
	if _, err := loadConfig(writeConf(t, 0o644), false); err != nil {
		t.Errorf("0644 은 통과해야 함: %v", err)
	}
	for _, m := range []os.FileMode{0o666, 0o664, 0o646} {
		if _, err := loadConfig(writeConf(t, m), false); err == nil || !strings.Contains(err.Error(), "쓰기 권한") {
			t.Errorf("%04o 는 거부되어야 함: %v", m, err)
		}
	}
}

func TestLoadConfigNotRegular(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadConfig(dir, false); err == nil {
		t.Error("디렉터리는 거부되어야 함")
	}
	real := writeConf(t, 0o644)
	link := filepath.Join(dir, "link.conf")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("심볼릭 링크 생성 불가")
	}
	if _, err := loadConfig(link, false); err == nil || !strings.Contains(err.Error(), "심볼릭") {
		t.Errorf("심볼릭 링크는 거부되어야 함: %v", err)
	}
	if _, err := loadConfig(filepath.Join(dir, "none"), false); err == nil {
		t.Error("없는 파일은 오류")
	}
}

func TestLoadConfigOwner(t *testing.T) {
	// 일반 사용자로 만든 파일은 root 소유가 아니므로 거부. root 로 돌리면 통과.
	_, err := LoadConfig(writeConf(t, 0o644))
	if os.Geteuid() == 0 {
		if err != nil {
			t.Errorf("root 소유는 통과해야 함: %v", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), "root 소유") {
		t.Errorf("root 소유 아님이 거부되어야 함: %v", err)
	}
}

func TestCheckAllowedDirAt(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	other := filepath.Join(root, "other")
	for _, d := range []string{allowed, other} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkAllowedDirAt(allowed, allowed); err != nil {
		t.Errorf("같은 경로는 통과: %v", err)
	}
	err := checkAllowedDirAt(allowed, other)
	want := "이 명령은 " + allowed + " 에서만 실행할 수 있습니다 (현재: " + other + ")"
	if err == nil || err.Error() != want {
		t.Errorf("불일치 오류 문구: %v", err)
	}
	// 심볼릭 링크로 허용 경로를 가리키면 실제 경로가 같으므로 통과
	link := filepath.Join(root, "link_to_allowed")
	if os.Symlink(allowed, link) == nil {
		if err := checkAllowedDirAt(allowed, link); err != nil {
			t.Errorf("같은 실제 경로의 링크는 통과: %v", err)
		}
	}
	// 허용 경로 자체가 다른 곳을 가리키는 링크이면 실제 경로 기준으로 비교
	evil := filepath.Join(root, "evil")
	if os.Symlink(other, evil) == nil {
		if err := checkAllowedDirAt(allowed, evil); err == nil {
			t.Error("다른 디렉터리를 가리키는 링크는 우회 불가여야 함")
		}
	}
	// 없는 경로
	if err := checkAllowedDirAt(filepath.Join(root, "nope"), allowed); err == nil {
		t.Error("없는 allowed_dir 은 오류")
	}
}
