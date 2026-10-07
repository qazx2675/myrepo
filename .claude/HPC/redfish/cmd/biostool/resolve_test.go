package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeHosts(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const testHosts = `# 주석 줄
127.0.0.1   localhost localhost.localdomain
192.0.2.10  host0001 host0001-m.example host0001-m   # 별칭 여러 개
192.0.2.11  host0002-m
192.0.2.12  HOST0003-M
192.0.2.99  host0001-m
bad-line-without-ip
not.an.ip  host0004-m
`

func TestParseList(t *testing.T) {
	text := "\ufeff# 머리말\r\n\r\n  host0001  \r\n192.0.2.5\nhost0001\n# 끝\nhost0002-m # 인라인 주석\n\t\nhost0001\n"
	got := parseList(text)
	want := []string{"host0001", "192.0.2.5", "host0002-m"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestParseListEmpty(t *testing.T) {
	if got := parseList("\n# 주석만\n\n"); len(got) != 0 {
		t.Errorf("빈 목록이어야 함: %q", got)
	}
}

func TestSplitTargets(t *testing.T) {
	got := splitTargets("host0001, 192.0.2.5 host0001\thost0002-m,,")
	want := []string{"host0001", "192.0.2.5", "host0002-m"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestParseHosts(t *testing.T) {
	m := parseHosts(testHosts)
	if m["host0001-m"] != "192.0.2.10" {
		t.Errorf("먼저 나온 줄이 우선이어야 함: %q", m["host0001-m"])
	}
	if m["host0001-m.example"] != "192.0.2.10" || m["host0001"] != "192.0.2.10" {
		t.Errorf("별칭 매칭 실패: %v", m)
	}
	if m["host0003-m"] != "192.0.2.12" {
		t.Errorf("대소문자 무시 실패: %v", m)
	}
	if _, ok := m["host0004-m"]; ok {
		t.Errorf("IP 형식이 아닌 줄은 무시해야 함")
	}
}

func TestResolveTargets(t *testing.T) {
	hosts := writeHosts(t, testHosts)
	in := []string{"192.0.2.5", "host0001", "host0002-m", "host0003", "nohost", "nohost2-m", "2001:db8::1"}
	got, err := resolveTargets(in, hosts)
	if err != nil {
		t.Fatal(err)
	}
	want := []Target{
		{Input: "192.0.2.5", Hostname: "192.0.2.5", IP: "192.0.2.5"},
		{Input: "host0001", Hostname: "host0001", IP: "192.0.2.10"},
		{Input: "host0002-m", Hostname: "host0002", IP: "192.0.2.11"},
		{Input: "host0003", Hostname: "host0003", IP: "192.0.2.12"},
		{Input: "nohost", Hostname: "nohost", Err: StatusNoHostsEntry},
		{Input: "nohost2-m", Hostname: "nohost2", Err: StatusNoHostsEntry},
		{Input: "2001:db8::1", Hostname: "2001:db8::1", IP: "2001:db8::1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%+v\nwant=%+v", got, want)
	}
}

func TestResolveNameOnlyMatchesMgmtName(t *testing.T) {
	// 이름 입력은 반드시 '-m' 이름으로 조회한다. 기본 이름만 hosts 에 있으면 찾지 못한다.
	hosts := writeHosts(t, "192.0.2.20  onlybase\n")
	got, err := resolveTargets([]string{"onlybase"}, hosts)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Err != StatusNoHostsEntry {
		t.Errorf("onlybase-m 이 없으므로 NO_HOSTS_ENTRY 여야 함: %+v", got[0])
	}
}

func TestResolveIPOnlyDoesNotNeedHosts(t *testing.T) {
	got, err := resolveTargets([]string{"192.0.2.5"}, filepath.Join(t.TempDir(), "없음"))
	if err != nil || got[0].IP != "192.0.2.5" {
		t.Errorf("IP 만 있으면 hosts 를 읽지 않아야 함: %v %+v", err, got)
	}
}

func TestResolveMissingHostsFile(t *testing.T) {
	_, err := resolveTargets([]string{"host0001"}, filepath.Join(t.TempDir(), "없음"))
	if err == nil || !strings.Contains(err.Error(), "hosts 파일") {
		t.Errorf("hosts 읽기 오류가 나야 함: %v", err)
	}
}

func TestSplitPort(t *testing.T) {
	cases := []struct {
		in, host, port string
		ok             bool
	}{
		{"127.0.0.1:8443", "127.0.0.1", "8443", true},
		{"[::1]:8443", "::1", "8443", true},
		{"[::1]", "::1", "", true},
		{"host0001-m:443", "host0001-m", "443", true},
		{"host0001", "", "", false},
		{"::1", "", "", false},         // 맨몸 IPv6 는 포트로 보지 않음
		{"2001:db8::1", "", "", false}, //
		{"host:0", "", "", false},      // 범위 밖
		{"host:65536", "", "", false},  //
		{"host:abc", "", "", false},    //
		{"host:0443", "", "", false},   // 앞자리 0
		{":8443", "", "", false},       // 호스트 없음
		{"[::1]:abc", "", "", false},   //
		{"[::1]x:8443", "", "", false}, //
	}
	for _, c := range cases {
		h, p, ok := splitPort(c.in)
		if h != c.host || p != c.port || ok != c.ok {
			t.Errorf("splitPort(%q) = %q,%q,%t 기대 %q,%q,%t", c.in, h, p, ok, c.host, c.port, c.ok)
		}
	}
}

func TestResolveTargetsWithPort(t *testing.T) {
	hosts := writeHosts(t, testHosts)
	in := []string{"127.0.0.1:8443", "[::1]:8443", "[::1]", "host0001-m:8443", "host0002:9443", "nohost:8443", "192.0.2.5"}
	got, err := resolveTargets(in, hosts)
	if err != nil {
		t.Fatal(err)
	}
	want := []Target{
		{Input: "127.0.0.1:8443", Hostname: "127.0.0.1:8443", IP: "127.0.0.1", Port: "8443"},
		{Input: "[::1]:8443", Hostname: "[::1]:8443", IP: "::1", Port: "8443"},
		{Input: "[::1]", Hostname: "[::1]", IP: "::1"},
		{Input: "host0001-m:8443", Hostname: "host0001:8443", IP: "192.0.2.10", Port: "8443"},
		{Input: "host0002:9443", Hostname: "host0002:9443", IP: "192.0.2.11", Port: "9443"},
		{Input: "nohost:8443", Hostname: "nohost:8443", Port: "8443", Err: StatusNoHostsEntry},
		{Input: "192.0.2.5", Hostname: "192.0.2.5", IP: "192.0.2.5"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%+v\nwant=%+v", got, want)
	}
}

func TestTargetBaseURL(t *testing.T) {
	cases := map[string]Target{
		"https://192.0.2.5":      {IP: "192.0.2.5"},
		"https://192.0.2.5:8443": {IP: "192.0.2.5", Port: "8443"},
		"https://[::1]:8443":     {IP: "::1", Port: "8443"},
		"https://[2001:db8::1]":  {IP: "2001:db8::1"},
		"":                       {Err: StatusNoHostsEntry},
	}
	for want, tg := range cases {
		if got := tg.BaseURL(); got != want {
			t.Errorf("%+v.BaseURL() = %q, 기대 %q", tg, got, want)
		}
	}
}
