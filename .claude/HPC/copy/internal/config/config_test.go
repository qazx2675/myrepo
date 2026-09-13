package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConf(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "copy_setting.conf")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("설정 파일 작성 실패: %v", err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	path := writeTempConf(t, `
# 주석
awx_url = http://10.20.30.40
username = admin
password = secret
insecure_tls = true
template = closed-network-copy
max_lines = 50
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load 실패: %v", err)
	}
	if c.AWXURL != "http://10.20.30.40" || c.Username != "admin" || c.Password != "secret" {
		t.Errorf("AWX 접속 정보 파싱 오류: %+v", c)
	}
	if !c.InsecureTLS {
		t.Errorf("insecure_tls=true 가 파싱되지 않음")
	}
	if c.Template != "closed-network-copy" {
		t.Errorf("template 파싱 오류: %q", c.Template)
	}
	if c.MaxLines != 50 {
		t.Errorf("max_lines 파싱 오류: %d", c.MaxLines)
	}
}

func TestLoadDefaultMaxLines(t *testing.T) {
	path := writeTempConf(t, `
awx_url = http://x
username = u
password = p
template = t
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load 실패: %v", err)
	}
	if c.MaxLines != 200 {
		t.Errorf("max_lines 기본값이 200이 아님: %d", c.MaxLines)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	path := writeTempConf(t, `
awx_url = http://x
username = u
`)
	if _, err := Load(path); err == nil {
		t.Fatal("template 누락인데 오류가 나지 않음")
	}
}

func TestLoadBadSyntax(t *testing.T) {
	path := writeTempConf(t, "this line has no equals sign\n")
	if _, err := Load(path); err == nil {
		t.Fatal("'=' 없는 줄인데 오류가 나지 않음")
	}
}

func TestResolvePathExplicitMissing(t *testing.T) {
	if _, err := ResolvePath(filepath.Join(t.TempDir(), "nope.conf")); err == nil {
		t.Fatal("존재하지 않는 -conf 경로인데 오류가 나지 않음")
	}
}

func TestResolvePathExplicitFound(t *testing.T) {
	path := writeTempConf(t, "awx_url = http://x\nusername = u\npassword = p\ntemplate = t\n")
	got, err := ResolvePath(path)
	if err != nil {
		t.Fatalf("ResolvePath 실패: %v", err)
	}
	if got != path {
		t.Errorf("경로 불일치: got=%q want=%q", got, path)
	}
}
