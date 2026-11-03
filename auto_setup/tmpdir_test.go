// tmpdir_test.go - 임시 파일은 /tmp 가 아니라 <데이터 디렉터리>/tmp 로 간다
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// 체크스크립트는 이번 run 의 tmp 를 TMPDIR 로 받고, 스크립트가 지우지 못하고 끝나도(강제 종료 흉내) run 이 끝나면 지워진다
func TestRunnerRunTmpRemoved(t *testing.T) {
	needBash(t)
	setupDir(t)
	oldC := os_check_sh
	t.Cleanup(func() { os_check_sh = oldC })
	os_check_sh = filepath.Join(t.TempDir(), "oc.sh")
	os.WriteFile(os_check_sh, []byte("#!/bin/bash\nd=$(mktemp -d \"${TMPDIR:-/tmp}/config_check.XXXXXX\")\necho \"T=$TMPDIR\"\necho \"D=$d\"\n"), 0755)
	j := &Job{ID: "j1", User: "u1", Hosts: map[string]*Host{"a1": {Route: "local"}}}
	res, err := NewRealRunner().Run(j, []string{"a1"}, false)
	if err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(runsDir(), res.Code)
	b, _ := os.ReadFile(filepath.Join(run, "os_check.log"))
	if !strings.Contains(string(b), "T="+filepath.Join(run, "tmp")+"\n") || !strings.Contains(string(b), "D="+filepath.Join(run, "tmp", "config_check.")) {
		t.Fatalf("TMPDIR 이 run 의 tmp 가 아님:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(run, "tmp")); !os.IsNotExist(err) {
		t.Fatalf("run 이 끝나도 tmp 가 남음: %v", err)
	}
}

// 데몬의 하루 정리: <데이터 디렉터리>/tmp 에서 tmpKeep 보다 오래된 항목만 지운다
func TestCleanStaleTmp(t *testing.T) {
	setupDir(t)
	d := filepath.Join(os.Getenv("AUTO_SETUP_DIR"), "tmp")
	os.MkdirAll(filepath.Join(d, "as.olddir"), 0755)
	os.WriteFile(filepath.Join(d, "as.olddir", "x"), []byte("h1\n"), 0644)
	os.WriteFile(filepath.Join(d, "tmp.old_res_off"), []byte("h1\n"), 0644)
	os.WriteFile(filepath.Join(d, "as_work_new.log"), []byte("x"), 0600)
	now := time.Now()
	old := now.Add(-tmpKeep - time.Hour)
	os.Chtimes(filepath.Join(d, "as.olddir"), old, old)
	os.Chtimes(filepath.Join(d, "tmp.old_res_off"), old, old)
	if n := cleanStaleTmp(now); n != 2 {
		t.Fatalf("2개 삭제 기대: %d", n)
	}
	ents, _ := os.ReadDir(d)
	if len(ents) != 1 || ents[0].Name() != "as_work_new.log" {
		t.Fatalf("남은 항목: %v", ents)
	}
}
