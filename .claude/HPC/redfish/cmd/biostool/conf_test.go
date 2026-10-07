package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConfDefaults(t *testing.T) {
	c, err := parseConf("user=admin\n", "bios.conf")
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		User: "admin", PassFile: "pass.enc", KeyFile: "key.bin", Concurrency: 100,
		Timeout: 20 * time.Second, Insecure: true, Retries: 2, AuthFailStop: 3,
		ResultDir: "results", DumpDir: "dumps", ProfileDir: "profiles",
	}
	if *c != want {
		t.Errorf("기본값 불일치\n got=%+v\nwant=%+v", *c, want)
	}
}

func TestParseConfAllKeys(t *testing.T) {
	text := "\ufeff# 주석\r\n\r\n" +
		"user = root \r\n" +
		"pass_file=a.enc\r\nkey_file=a.key\r\n" +
		"concurrency=7\r\ntimeout=5\r\ninsecure=false\r\nretries=0\r\nauth_fail_stop=5\r\n" +
		"result_dir=r\r\ndump_dir=d\r\nprofile_dir=p\r\n"
	c, err := parseConf(text, "bios.conf")
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		User: "root", PassFile: "a.enc", KeyFile: "a.key", Concurrency: 7,
		Timeout: 5 * time.Second, Insecure: false, Retries: 0, AuthFailStop: 5,
		ResultDir: "r", DumpDir: "d", ProfileDir: "p",
	}
	if *c != want {
		t.Errorf("불일치\n got=%+v\nwant=%+v", *c, want)
	}
}

func TestParseConfLastWins(t *testing.T) {
	c, err := parseConf("user=a\nuser=b\n", "x")
	if err != nil || c.User != "b" {
		t.Errorf("마지막 값이 적용돼야 함: %v %+v", err, c)
	}
}

func TestParseConfErrors(t *testing.T) {
	cases := []struct {
		name, text, want string
	}{
		{"user 없음", "concurrency=5\n", "필수 항목 user"},
		{"user 빈값", "user=\n", "필수 항목 user"},
		{"숫자 아님", "user=a\nconcurrency=abc\n", "정수가 아닙니다"},
		{"0 동시접속", "user=a\nconcurrency=0\n", "1 이상"},
		{"음수 retries", "user=a\nretries=-1\n", "0 이상"},
		{"음수 auth_fail_stop", "user=a\nauth_fail_stop=-1\n", "0 이상"},
		{"auth_fail_stop 숫자 아님", "user=a\nauth_fail_stop=x\n", "정수가 아닙니다"},
		{"timeout 0", "user=a\ntimeout=0\n", "1 이상"},
		{"bool 아님", "user=a\ninsecure=maybe\n", "true 또는 false"},
		{"모르는 키", "user=a\nconcurency=5\n", "알 수 없는 키"},
		{"= 없음", "user=a\nfoo\n", "key=value"},
		{"비밀번호 키", "user=a\npassword=s3cret-value\n", "비밀번호는 conf 에 둘 수 없습니다"},
	}
	for _, tc := range cases {
		_, err := parseConf(tc.text, "bios.conf")
		if err == nil {
			t.Errorf("%s: 오류가 나야 함", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: 메시지 %q 에 %q 없음", tc.name, err.Error(), tc.want)
		}
	}
}

func TestParseConfPasswordNotEchoed(t *testing.T) {
	_, err := parseConf("user=a\npassword=s3cret-value\n", "bios.conf")
	if err == nil || strings.Contains(err.Error(), "s3cret-value") {
		t.Errorf("비밀번호 값이 오류 메시지에 노출되면 안 됨: %v", err)
	}
}

func TestParseConfErrorHasLine(t *testing.T) {
	_, err := parseConf("user=a\n\nconcurrency=x\n", "bios.conf")
	if err == nil || !strings.Contains(err.Error(), "bios.conf:3:") {
		t.Errorf("파일명:줄번호 가 있어야 함: %v", err)
	}
}

func TestLoadConf(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadConf(filepath.Join(dir, "none.conf")); err == nil || !strings.Contains(err.Error(), "없습니다") {
		t.Errorf("없는 파일 메시지: %v", err)
	}
	p := filepath.Join(dir, "bios.conf")
	if err := os.WriteFile(p, []byte("user=admin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := loadConf(p)
	if err != nil || c.User != "admin" {
		t.Errorf("로드 실패: %v %+v", err, c)
	}
}

func TestParseConfAuthFailStopZeroDisables(t *testing.T) {
	c, err := parseConf("user=a\nauth_fail_stop=0\n", "bios.conf")
	if err != nil || c.AuthFailStop != 0 {
		t.Errorf("auth_fail_stop=0 은 비활성(0) 으로 읽혀야 함: %v %+v", err, c)
	}
}
