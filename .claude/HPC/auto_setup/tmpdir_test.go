// tmpdir_test.go - 임시 파일은 /tmp 가 아니라 <데이터 디렉터리>/tmp 로 간다
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUseDataTmpRedirectsTempFiles(t *testing.T) {
	setupDir(t)
	dir := os.Getenv("AUTO_SETUP_DIR")
	t.Setenv("TMPDIR", "/tmp")
	useDataTmp()
	want := filepath.Join(dir, "tmp")
	if got := os.TempDir(); got != want {
		t.Fatalf("TempDir=%q want %q", got, want)
	}
	f, err := os.CreateTemp("", "as_x_")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if !strings.HasPrefix(f.Name(), want+string(os.PathSeparator)) {
		t.Fatalf("임시 파일 위치: %s", f.Name())
	}
	// 하위 프로세스(mktemp·config_check)도 같은 곳을 쓴다
	out, err := exec.Command("sh", "-c", `mktemp -d "${TMPDIR:-/tmp}/config_check.XXXXXX"`).Output()
	if err != nil {
		t.Fatal(err)
	}
	if p := strings.TrimSpace(string(out)); !strings.HasPrefix(p, want+"/config_check.") {
		t.Fatalf("하위 프로세스 임시 디렉터리: %s", p)
	}
	if fi, err := os.Stat(want); err != nil || fi.Mode()&os.ModeSticky == 0 {
		t.Fatalf("sticky 아님: %v %v", fi, err)
	}
}

func TestUseDataTmpKeepsTMPDIRWhenUnwritable(t *testing.T) {
	setupDir(t)
	dir := os.Getenv("AUTO_SETUP_DIR")
	if err := os.WriteFile(filepath.Join(dir, "tmp"), []byte("x"), 0600); err != nil { // 디렉터리를 만들 수 없는 상황
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", "/var/tmp")
	useDataTmp()
	if got := os.Getenv("TMPDIR"); got != "/var/tmp" {
		t.Fatalf("TMPDIR 이 바뀜: %q", got)
	}
}
