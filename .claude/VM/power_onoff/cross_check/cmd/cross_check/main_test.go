package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseTargets(t *testing.T) {
	in := "# 주석\nvm01ev01\tvm02ev01  vm03ev01\n\n  vm04ev01 # 뒤 주석\r\nvm05ev01"
	got, err := parseTargets(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"vm01ev01", "vm02ev01", "vm03ev01", "vm04ev01", "vm05ev01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestUsageExitCodes(t *testing.T) {
	t.Setenv("CROSS_CHECK_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	cases := []struct {
		args []string
		want int
	}{
		{nil, exitUsage},                                  // 사용자 미지정
		{[]string{"-h"}, exitOK},                          // 도움말
		{[]string{"--version"}, exitOK},                   // 버전
		{[]string{"--run", "--admin"}, exitUsage},         // 모드 둘
		{[]string{"--user", "a", "extra"}, exitUsage},     // 남는 인자
		{[]string{"--bogus"}, exitUsage},                  // 모르는 플래그
		{[]string{"--run", "--user", "alice"}, exitUsage}, // 확인자 없음
		{[]string{"--start"}, exitUsage},                  // job 없음
		{[]string{"--start", "--job", "x"}, exitUsage},    // token 없음
		{[]string{"--user", "Bad-Name"}, exitUsage},       // 이름 규칙
		{[]string{"--user", "admin"}, exitUsage},          // 예약 이름
		{[]string{"--user", "alice", "--checker", "bob"}, exitUsage},
		{[]string{"--user", "alice"}, exitError}, // 데몬 없음
		{[]string{"--start", "--job", "x", "--token", "t"}, exitError},
	}
	for _, c := range cases {
		if got := realMain(c.args); got != c.want {
			t.Errorf("%v → %d, want %d", c.args, got, c.want)
		}
	}
}

func TestCheckerTypoAlias(t *testing.T) {
	t.Setenv("CROSS_CHECK_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	// --cechker 도 확인자로 받아 "확인자 없음"(2)이 아니라 같은 사람 → 중단(1)이 된다
	if got := realMain([]string{"--run", "--user", "alice", "--cechker", "alice", "--targets-file", filepath.Join(t.TempDir(), "x")}); got != exitFail {
		t.Fatalf("got %d", got)
	}
	if got := realMain([]string{"-run", "-user", "alice", "-checker", "alice", "--targets-file", filepath.Join(t.TempDir(), "x")}); got != exitFail {
		t.Fatalf("got %d", got)
	}
}
