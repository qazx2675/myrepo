//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDPAPIRoundTrip(t *testing.T) {
	for _, pw := range []string{"p", "비밀 번호!@# \t", strings.Repeat("x", 1000)} {
		enc, err := dpapiEncrypt(pw)
		if err != nil {
			t.Fatal(err)
		}
		if len(pw) >= 8 && strings.Contains(enc, pw) {
			t.Fatal("암호문에 평문")
		}
		got, err := dpapiDecrypt(enc)
		if err != nil || got != pw {
			t.Fatalf("왕복: %v %q", err, got)
		}
	}
	if _, err := dpapiDecrypt("AAAA"); err == nil {
		t.Fatal("잘못된 암호문은 실패")
	}
	// 설정파일: password_enc 로 쓰고 읽는다. 평문 password 도 읽는다.
	d := t.TempDir()
	p := filepath.Join(d, "u.conf")
	if err := writeConfFile(p, "http://h:1/", "u", "S3CRET-값", 5, 0, 0); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "S3CRET") || !strings.Contains(string(b), "password_enc = ") {
		t.Fatalf("암호화 저장: %s", b)
	}
	c, err := loadConfig(p)
	if err != nil || c.Password != "S3CRET-값" || !c.PasswordEnc || c.URL != "http://h:1" {
		t.Fatalf("읽기: %v %+v", err, c)
	}
	_ = os.WriteFile(p, []byte("url=http://h:1\nusername=u\npassword=plain\ninventory_id=5\n"), 0o600)
	if c, err := loadConfig(p); err != nil || c.Password != "plain" || c.PasswordEnc {
		t.Fatalf("평문 읽기: %v", err)
	}
	_ = os.WriteFile(p, []byte("url=http://h:1\nusername=u\npassword_enc=AAAA\ninventory_id=5\n"), 0o600)
	if _, err := loadConfig(p); err == nil || strings.Contains(err.Error(), "AAAA") {
		t.Fatalf("깨진 password_enc: %v", err)
	}
}

func TestSingleInstanceMutex(t *testing.T) {
	name := `Local\AWX_copy_test_` + t.Name()
	rel, already, err := acquireSingleInstance(name)
	if err != nil || already {
		t.Fatal(err, already)
	}
	_, already2, _ := acquireSingleInstance(name)
	if !already2 {
		t.Fatal("두 번째는 이미 실행 중이어야 합니다")
	}
	rel()
	rel3, already3, _ := acquireSingleInstance(name)
	if already3 {
		t.Fatal("해제 뒤에는 다시 잡혀야 합니다")
	}
	rel3()
}
