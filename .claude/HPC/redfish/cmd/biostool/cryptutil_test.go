package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k, err := loadOrCreateKey(filepath.Join(t.TempDir(), "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	k := testKey(t)
	ct, err := encrypt([]byte("p@ss w0rd!"), k)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte("p@ss")) {
		t.Error("암호문에 평문이 보이면 안 됨")
	}
	pt, err := decrypt(ct, k)
	if err != nil || string(pt) != "p@ss w0rd!" {
		t.Errorf("왕복 실패: %q %v", pt, err)
	}
}

func TestEncryptNonceUnique(t *testing.T) {
	k := testKey(t)
	a, _ := encrypt([]byte("same"), k)
	b, _ := encrypt([]byte("same"), k)
	if bytes.Equal(a, b) {
		t.Error("같은 평문이라도 nonce 가 달라 암호문이 달라야 함")
	}
}

func TestBadKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 24, 31, 33} {
		if _, err := encrypt([]byte("x"), make([]byte, n)); err == nil {
			t.Errorf("키 %d바이트: encrypt 가 실패해야 함 (AES-256 만 허용)", n)
		}
		if _, err := decrypt(make([]byte, 64), make([]byte, n)); err == nil {
			t.Errorf("키 %d바이트: decrypt 가 실패해야 함", n)
		}
	}
	p := filepath.Join(t.TempDir(), "short.bin")
	if err := os.WriteFile(p, make([]byte, 16), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadKey(p); err == nil || !strings.Contains(err.Error(), "32바이트") {
		t.Errorf("loadKey 길이 오류: %v", err)
	}
	if _, err := loadOrCreateKey(p); err == nil {
		t.Error("loadOrCreateKey 는 잘못된 길이의 기존 파일을 덮어쓰지 말고 오류를 내야 함")
	}
}

func TestDecryptTampered(t *testing.T) {
	k := testKey(t)
	ct, _ := encrypt([]byte("secret"), k)
	for _, idx := range []int{0, 12, len(ct) - 1} { // nonce / 본문 / 태그
		bad := append([]byte(nil), ct...)
		bad[idx] ^= 0x01
		if _, err := decrypt(bad, k); err == nil {
			t.Errorf("%d번째 바이트 변조가 탐지돼야 함", idx)
		}
	}
	if _, err := decrypt(ct[:5], k); err == nil {
		t.Error("너무 짧은 암호문은 실패해야 함")
	}
}

func TestDecryptWrongKey(t *testing.T) {
	ct, _ := encrypt([]byte("secret"), testKey(t))
	if _, err := decrypt(ct, testKey(t)); err == nil {
		t.Error("다른 키로는 복호화되면 안 됨")
	}
}

func TestLoadOrCreateKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key.bin")
	k1, err := loadOrCreateKey(p)
	if err != nil || len(k1) != keySize {
		t.Fatalf("생성 실패: %v len=%d", err, len(k1))
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("키 파일 권한 %v, 0600 이어야 함", fi.Mode().Perm())
		}
	}
	k2, err := loadOrCreateKey(p)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Error("두 번째 호출은 기존 키를 그대로 돌려줘야 함")
	}
}

func TestEncryptFromReaderAndReadPassword(t *testing.T) {
	dir := t.TempDir()
	out, key := filepath.Join(dir, "pass.enc"), filepath.Join(dir, "key.bin")
	if err := encryptFromReader(strings.NewReader("Sup3r-Secret\r\n"), out, key); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(out)
	if bytes.Contains(raw, []byte("Sup3r-Secret")) {
		t.Error("pass.enc 에 평문이 있으면 안 됨")
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(out); fi.Mode().Perm() != 0o600 {
			t.Errorf("pass.enc 권한 %v", fi.Mode().Perm())
		}
	}
	pw, err := readPassword(out, key)
	if err != nil || pw != "Sup3r-Secret" {
		t.Errorf("끝 개행 제거 후 복호화돼야 함: %q %v", pw, err)
	}
}

func TestEncryptFromReaderOverwritePerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("권한 비트 없음")
	}
	dir := t.TempDir()
	out, key := filepath.Join(dir, "pass.enc"), filepath.Join(dir, "key.bin")
	if err := os.WriteFile(out, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := encryptFromReader(strings.NewReader("pw"), out, key); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(out); fi.Mode().Perm() != 0o600 {
		t.Errorf("덮어쓴 파일도 0600 이어야 함: %v", fi.Mode().Perm())
	}
}

func TestEncryptFromReaderEmpty(t *testing.T) {
	dir := t.TempDir()
	for _, in := range []string{"", "\n", "\r\n"} {
		err := encryptFromReader(strings.NewReader(in), filepath.Join(dir, "p.enc"), filepath.Join(dir, "k.bin"))
		if err == nil {
			t.Errorf("빈 비밀번호(%q)는 거부해야 함", in)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "p.enc")); err == nil {
		t.Error("실패 시 pass.enc 가 생기면 안 됨")
	}
}

func TestReadPasswordWrongKey(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "pass.enc")
	if err := encryptFromReader(strings.NewReader("pw"), out, filepath.Join(dir, "k1.bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateKey(filepath.Join(dir, "k2.bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := readPassword(out, filepath.Join(dir, "k2.bin")); err == nil {
		t.Error("짝이 안 맞는 키는 복호화 실패해야 함")
	}
}
