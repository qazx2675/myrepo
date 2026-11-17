package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeConf(t *testing.T, dir, file, user, inv string) string {
	t.Helper()
	_ = os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, file)
	body := "url = http://x:1\nusername = " + user + "\npassword = SECRET-PW\ninventory_id = " + inv + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func newSel(t *testing.T, osUser string, tty bool, input string) (*selector, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	d := t.TempDir()
	return &selector{
		Dir: filepath.Join(d, "conf"), Legacy: filepath.Join(d, "legacy.conf"), OSUser: osUser, IsTTY: tty,
		In: bufio.NewReader(strings.NewReader(input)), Out: &out, Err: &errb,
		ReadPassword: func(in *bufio.Reader, _ io.Writer) (string, error) {
			l, err := in.ReadString('\n')
			return strings.TrimRight(l, "\r\n"), err
		},
	}, &out, &errb
}

func TestSelectOSUserFile(t *testing.T) {
	s, out, _ := newSel(t, "alice", false, "")
	writeConf(t, s.Dir, "alice.conf", "alice", "7")
	writeConf(t, s.Dir, "bob.conf", "bob", "8")
	cfg, _, err := s.resolveConfig("", "")
	if err != nil || cfg.InventoryID != 7 {
		t.Fatalf("%v %+v", err, cfg)
	}
	if out.Len() != 0 {
		t.Fatal("확인 질문/출력이 있으면 안 됨:", out.String())
	}
}

func TestSelectByUsernameRecord(t *testing.T) {
	s, _, _ := newSel(t, "carol", false, "")
	writeConf(t, s.Dir, "x.conf", "other", "1")
	writeConf(t, s.Dir, "y.conf", "carol", "9") // 파일명은 다르지만 username 이 같다
	cfg, _, err := s.resolveConfig("", "")
	if err != nil || cfg.InventoryID != 9 {
		t.Fatalf("%v %+v", err, cfg)
	}
}

func TestSelectLegacyByUsername(t *testing.T) {
	s, _, _ := newSel(t, "dave", false, "")
	writeConf(t, filepath.Dir(s.Legacy), filepath.Base(s.Legacy), "dave", "4")
	cfg, _, err := s.resolveConfig("", "")
	if err != nil || cfg.InventoryID != 4 {
		t.Fatalf("%v %+v", err, cfg)
	}
}

func TestNoMatchNoTTY(t *testing.T) {
	s, out, errb := newSel(t, "zed", false, "")
	writeConf(t, s.Dir, "a.conf", "ann", "1")
	cfg, code, err := s.resolveConfig("", "")
	if err == nil || cfg != nil || code != 2 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	e := errb.String()
	if !strings.Contains(e, "a.conf") || !strings.Contains(e, "ann") || !strings.Contains(e, "--user") || !strings.Contains(e, "--config") {
		t.Fatalf("안내문 부족: %s", e)
	}
	if strings.Contains(e+out.String(), "SECRET-PW") {
		t.Fatal("비밀번호 출력")
	}
}

func TestPriorityConfigAndUser(t *testing.T) {
	s, _, _ := newSel(t, "alice", false, "")
	writeConf(t, s.Dir, "alice.conf", "alice", "7")
	writeConf(t, s.Dir, "bob.conf", "bob", "8")
	other := writeConf(t, t.TempDir(), "any.conf", "zzz", "3")
	if cfg, _, err := s.resolveConfig(other, "bob"); err != nil || cfg.InventoryID != 3 {
		t.Fatalf("--config 이 최우선이어야 함: %v %+v", err, cfg)
	}
	if cfg, _, err := s.resolveConfig("", "bob"); err != nil || cfg.InventoryID != 8 {
		t.Fatalf("--user 가 OS 사용자보다 우선이어야 함: %v %+v", err, cfg)
	}
	if _, code, err := s.resolveConfig("", "nobody"); err == nil || code != 2 {
		t.Fatal("없는 사용자는 코드 2 오류여야 함")
	}
	if _, _, err := s.resolveConfig("", "../x"); err == nil {
		t.Fatal("경로 이동 이름 거부")
	}
}

func TestMenuSelectAndNoPassword(t *testing.T) {
	s, out, errb := newSel(t, "zed", true, "2\n")
	writeConf(t, s.Dir, "a.conf", "ann", "1")
	writeConf(t, s.Dir, "b.conf", "bob", "2")
	cfg, _, err := s.resolveConfig("", "")
	if err != nil || cfg.Username != "bob" {
		t.Fatalf("%v %+v", err, cfg)
	}
	o := out.String()
	for _, w := range []string{"1)", "a.conf", "ann", "http://x:1", "2)", "b.conf", "3) 새 설정 만들기"} {
		if !strings.Contains(o, w) {
			t.Errorf("메뉴에 %q 없음:\n%s", w, o)
		}
	}
	if strings.Contains(o+errb.String(), "SECRET-PW") {
		t.Fatal("비밀번호 출력")
	}
	var b bytes.Buffer
	s.listUsers(&b)
	if !strings.Contains(b.String(), "ann") || strings.Contains(b.String(), "SECRET-PW") {
		t.Fatal("list-users 출력 이상")
	}
}

func TestMenuCreateAndOverwrite(t *testing.T) {
	// 후보 없음 → 1번이 새 설정 만들기. 저장 뒤 바로 사용.
	s, out, _ := newSel(t, "zed", true, "1\nhttp://h:9/\nnewu\nNEW-PW\n12\n")
	cfg, _, err := s.resolveConfig("", "")
	if err != nil || cfg.Username != "newu" || cfg.InventoryID != 12 || cfg.Password != "NEW-PW" || cfg.URL != "http://h:9" {
		t.Fatalf("%v %+v", err, cfg)
	}
	p := filepath.Join(s.Dir, "newu.conf")
	if st, err := os.Stat(p); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o600) {
		t.Fatalf("저장/권한 이상: %v", err)
	}
	if strings.Contains(out.String(), "NEW-PW") {
		t.Fatal("입력한 비밀번호가 화면에 나옴")
	}
	// 같은 이름: n 이면 덮어쓰지 않는다.
	s2, _, _ := newSel(t, "zed", true, "2\nhttp://h:9\nnewu\nOTHER\n5\nn\n")
	s2.Dir = s.Dir
	writeConf(t, s2.Dir, "keep.conf", "keep", "1") // 목록 1개 이상이면 새 설정은 2번
	s2.In = bufio.NewReader(strings.NewReader("3\nhttp://h:9\nnewu\nOTHER\n5\nn\n"))
	if _, _, err := s2.resolveConfig("", ""); err == nil {
		t.Fatal("덮어쓰기 거부가 오류여야 함")
	}
	if c, _ := loadConfig(p); c == nil || c.Password != "NEW-PW" {
		t.Fatal("덮어써짐")
	}
}
