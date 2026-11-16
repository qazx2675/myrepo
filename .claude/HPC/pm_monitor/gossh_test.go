package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestParsePmOutput(t *testing.T) {
	stdout := "n01: PM_OK\nn02: motd line\nn05: PM_OK\n"
	stderr := "n02: OS 설치중 (~/.profile anaconda 감지)\n" +
		"n03: SSH 접속 실패: dial tcp n03:22: i/o timeout\n" +
		"n03: ERROR: 두번째 줄\n"
	hosts := []string{"n01", "n02", "n03", "n04", "n05"}
	done, inst, other := parsePmOutput(stdout, stderr, hosts)
	if !reflect.DeepEqual(done, []string{"n01", "n05"}) {
		t.Errorf("done = %v", done)
	}
	if !reflect.DeepEqual(inst, []string{"n02"}) {
		t.Errorf("installing = %v", inst)
	}
	if other["n03"] != "SSH 접속 실패: dial tcp n03:22: i/o timeout" || other["n04"] != "응답 없음" || len(other) != 2 {
		t.Errorf("other = %v", other)
	}
}

func TestParseDiskOutput(t *testing.T) {
	out := "a: NS_DISK\na: NS_DONE\nb: NS_DONE\nc: NS_NOLOG\nc: NS_DONE\nd: NS_DISK\nd: NS_DISK\ngarbage\n"
	got := parseDiskOutput(out)
	if !reflect.DeepEqual(got, []string{"a", "d"}) {
		t.Errorf("diskFail = %v", got)
	}
}

func TestBuildDiskCmd(t *testing.T) {
	got := buildDiskCmd([]string{`Disk "" given in clearpart command does not exist`, "it's"})
	want := `[ -r /tmp/anaconda.log ] || echo NS_NOLOG; ` +
		`[ -r /tmp/anaconda.log ] && grep -qF -e 'Disk "" given in clearpart command does not exist' -e 'it'\''s' /tmp/anaconda.log && echo NS_DISK; echo NS_DONE`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// stubEnv 는 스텁 gossh 를 임시 폴더에 복사(실행권한 부여)하고 상태 파일을 만든 Config 를 돌려준다.
func stubEnv(t *testing.T, state string) (*Config, string) {
	t.Helper()
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join("test", "stub_gossh.sh"))
	if err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "stub_gossh.sh")
	if err := os.WriteFile(stub, src, 0o755); err != nil {
		t.Fatal(err)
	}
	st := filepath.Join(dir, "state")
	if err := os.WriteFile(st, []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STUB_GOSSH_STATE", st)
	cfg := defaultConfig()
	cfg.Gossh = stub
	return cfg, dir
}

func TestPmCheckStub(t *testing.T) {
	cfg, dir := stubEnv(t, "a done\nb installing\nc disk\nd done\n")
	nf := filepath.Join(dir, "nice.txt")
	t.Setenv("STUB_GOSSH_NICE", nf)
	done, inst, other, err := PmCheck(cfg, []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(done, []string{"a", "d"}) || !reflect.DeepEqual(inst, []string{"b", "c"}) {
		t.Errorf("done=%v inst=%v", done, inst)
	}
	if !strings.HasPrefix(other["e"], "SSH 접속 실패") || len(other) != 1 {
		t.Errorf("other = %v", other)
	}
	b, _ := os.ReadFile(nf)
	if strings.TrimSpace(string(b)) != "10" {
		t.Errorf("nice 값 = %q, want 10", b)
	}
}

func TestDiskCheckStub(t *testing.T) {
	cfg, _ := stubEnv(t, "b installing\nc disk\nd disk\n")
	fail, err := DiskCheck(cfg, []string{"b", "c", "d", "e"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fail, []string{"c", "d"}) {
		t.Errorf("diskFail = %v", fail)
	}
}

func TestEmptyHostsNoRun(t *testing.T) {
	cfg := defaultConfig()
	cfg.Gossh = "/nonexistent/gossh"
	if d, i, o, err := PmCheck(cfg, nil); err != nil || d != nil || i != nil || len(o) != 0 {
		t.Errorf("PmCheck(nil) = %v %v %v %v", d, i, o, err)
	}
	if f, err := DiskCheck(cfg, nil); err != nil || f != nil {
		t.Errorf("DiskCheck(nil) = %v %v", f, err)
	}
}

func TestAtMostOneGossh(t *testing.T) {
	cfg, dir := stubEnv(t, "a done\nb installing\n")
	lock := filepath.Join(dir, "lock")
	t.Setenv("STUB_GOSSH_LOCK", lock)
	t.Setenv("STUB_GOSSH_SLEEP", "0.2")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var err error
			if i%2 == 0 {
				_, _, _, err = PmCheck(cfg, []string{"a", "b"})
			} else {
				_, err = DiskCheck(cfg, []string{"a", "b"})
			}
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if _, err := os.Stat(lock + ".overlap"); err == nil {
		t.Error("gossh 가 동시에 둘 이상 실행됨")
	}
}

func TestTempDirCleaned(t *testing.T) {
	cfg, _ := stubEnv(t, "a installing\n")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if _, _, _, err := PmCheck(cfg, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(tmp)
	if len(ents) != 0 {
		t.Errorf("임시 파일이 남음: %v", ents)
	}
}
