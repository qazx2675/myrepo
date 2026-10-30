package main

import (
	"os"
	"path/filepath"
	"reflect"
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
		User: "admin", PassFile: "pass.enc", KeyFile: "key.bin", Pass2File: "pass2.enc", RFEncFile: "rf.enc", Concurrency: 100,
		Timeout: 20 * time.Second, Insecure: true, Retries: 2, AuthFailStop: 3,
		ResultDir: "results", DumpDir: "dumps", ProfileDir: "profiles", RefsDir: "refs", XMLHostPrefix: []string{"c"},
	}
	if !reflect.DeepEqual(*c, want) {
		t.Errorf("기본값 불일치\n got=%+v\nwant=%+v", *c, want)
	}
}

func TestParseConfAllKeys(t *testing.T) {
	text := "\ufeff# 주석\r\n\r\n" +
		"user = root \r\n" +
		"pass_file=a.enc\r\nkey_file=a.key\r\n" +
		"user2=ops1\r\nuser2_role=operator\r\nuser2_role_HPE=readonly\r\npass2_file=b.enc\r\nrf_enc_file=c.enc\r\n" +
		"concurrency=7\r\ntimeout=5\r\ninsecure=false\r\nretries=0\r\nauth_fail_stop=5\r\n" +
		"result_dir=r\r\ndump_dir=d\r\nprofile_dir=p\r\n" +
		"exclude_attrs= A | *Uuid* ||B \r\nallcheck_set=ON\r\nrefs_dir=myrefs\r\n"
	c, err := parseConf(text, "bios.conf")
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		User: "root", PassFile: "a.enc", KeyFile: "a.key", User2: "ops1", Pass2File: "b.enc", RFEncFile: "c.enc", User2Role: "Operator", User2Roles: map[string]string{"hpe": "ReadOnly"}, Concurrency: 7,
		Timeout: 5 * time.Second, Insecure: false, Retries: 0, AuthFailStop: 5,
		ResultDir: "r", DumpDir: "d", ProfileDir: "p",
		ExcludeAttrs: []string{"A", "*Uuid*", "B"}, AllcheckSet: true, RefsDir: "myrefs", XMLHostPrefix: []string{"c"},
	}
	if !reflect.DeepEqual(*c, want) {
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
		{"allcheck_set 값", "user=a\nallcheck_set=yes\n", "on 또는 off"},
		{"user2 권한 없음", "user=a\nuser2=b\n", "user2_role"},
		{"user2 권한 값", "user=a\nuser2=b\nuser2_role=root\n", "Administrator, Operator, ReadOnly"},
		{"user2 같은 id", "user=a\nuser2=a\nuser2_role=ReadOnly\n", "user 와 달라야"},
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

func TestParseConfV02Keys(t *testing.T) {
	c, err := parseConf("user=a\nexclude_attrs=\nallcheck_set=off\nrefs_dir=\n", "bios.conf")
	if err != nil {
		t.Fatal(err)
	}
	if c.ExcludeAttrs != nil || c.AllcheckSet || c.RefsDir != "refs" {
		t.Errorf("빈 exclude_attrs·off·빈 refs_dir 은 기본값이어야 함: %+v", c)
	}
	c, err = parseConf("user=a\nexclude_attrs=X\nexclude_attrs=Y|Z\n", "bios.conf")
	if err != nil || !reflect.DeepEqual(c.ExcludeAttrs, []string{"Y", "Z"}) {
		t.Errorf("exclude_attrs 는 마지막 줄이 적용돼야 함: %v %+v", err, c)
	}
}
