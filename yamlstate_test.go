package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// -yaml-state: 소스의 현재 source_path 와 인벤토리 호스트 이름(정렬)을 [yaml상태] key=value 줄로 낸다.
func TestYamlStateRun(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.srcPath, f.hosts = "other_3ea.yml", []string{"n2", "n1", "n3"}
	var out bytes.Buffer
	if code := yamlStateRun(&out, a.cfg, a.client); code != 0 {
		t.Fatalf("code=%d", code)
	}
	want := "[yaml상태] yml=other_3ea.yml\n[yaml상태] source=11 inventory=7\n[yaml상태] count=3\n[yaml상태] hosts=n1,n2,n3\n"
	if out.String() != want {
		t.Errorf("출력\n기대: %q\n실제: %q", want, out.String())
	}
	if f.patch != nil {
		t.Errorf("읽기 전용인데 PATCH 가 나감: %v", f.patch)
	}
	for k := range f.calls {
		if !strings.HasPrefix(k, "GET ") {
			t.Errorf("GET 이 아닌 요청: %s", k)
		}
	}
}

// 호스트가 여러 쪽(next)이어도 다 읽는다. 호스트가 없으면 count=0, hosts= 빈 값.
func TestYamlStatePagesAndEmpty(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.srcPath, f.hosts, f.hostPages = "x.yml", []string{"b", "a", "c"}, true
	st, err := a.client.yamlState(a.cfg)
	if err != nil || strings.Join(st.Hosts, ",") != "a,b,c" || st.Yml != "x.yml" || st.Inventory != 7 || st.SourceID != 11 {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	f.hosts = nil
	var out bytes.Buffer
	if code := yamlStateRun(&out, a.cfg, a.client); code != 0 || !strings.Contains(out.String(), "count=0\n[yaml상태] hosts=\n") {
		t.Errorf("code=%d out=%q", code, out.String())
	}
}

// 소스 조회가 실패하면 종료코드 2.
func TestYamlStateError(t *testing.T) {
	a, f, _ := newTestApp(t)
	f.failSrc = true
	oldErr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	code := yamlStateRun(io.Discard, a.cfg, a.client)
	w.Close()
	os.Stderr = oldErr
	msg, _ := io.ReadAll(r)
	if code != 2 || !strings.Contains(string(msg), "소스(11) 조회 실패") {
		t.Errorf("code=%d stderr=%q", code, msg)
	}
}

// 등록이 끝나면 [등록] yml=<yml> hosts=<등록 직후 인벤토리 호스트> 줄을 낸다. 호스트를 못 읽으면 yml 만 낸다(경고 포함).
func TestRegisterPrintsRegisteredLine(t *testing.T) {
	a, f, _ := newTestApp(t)
	var out bytes.Buffer
	a.out = &out
	f.hosts = []string{"n2", "n1"}
	yml, err := a.register("hong_cpu.yaml", twoTargets())
	if err != nil || yml != "hong_cpu_2ea.yml" {
		t.Fatalf("yml=%q err=%v", yml, err)
	}
	if !strings.Contains(out.String(), "\n[등록] yml=hong_cpu_2ea.yml hosts=n1,n2\n") {
		t.Errorf("[등록] 줄 없음:\n%s", out.String())
	}

	out.Reset()
	f.failSrc = true
	if _, err := a.register("hong_cpu.yaml", twoTargets()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\n[등록] yml=hong_cpu_2ea.yml\n") || !strings.Contains(out.String(), "호스트를 읽지 못했습니다") {
		t.Errorf("호스트를 못 읽을 때:\n%s", out.String())
	}
}

// 바이너리 수준: -yaml-state 는 -hosts 없이도 돌고, 알 수 없는 인자는 거부한다(종료코드 2).
func TestYamlStateFlag(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go 없음")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "st")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("빌드: %v\n%s", err, out)
	}
	c := exec.Command(bin, "-yaml-state", "-conf", filepath.Join(dir, "none.conf"))
	out, err := c.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 || !strings.Contains(string(out), "설정 파일이 없습니다") {
		t.Errorf("없는 conf: %v\n%s", err, out)
	}
	conf := filepath.Join(dir, "x.conf")
	os.WriteFile(conf, []byte("awx_url = http://127.0.0.1:1\nusername = u\npassword = p\n"), 0o644)
	out, err = exec.Command(bin, "-yaml-state", "-conf", conf).CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 || !strings.Contains(string(out), "inventory_source 또는 inventory") {
		t.Errorf("inventory 없음: %v\n%s", err, out)
	}
	out, err = exec.Command(bin, "-yaml-state", "-conf", conf, "extra").CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 || !strings.Contains(string(out), "알 수 없는 인자") {
		t.Errorf("여분 인자: %v\n%s", err, out)
	}
}
