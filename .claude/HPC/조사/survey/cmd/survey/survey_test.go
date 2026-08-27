package main

import (
	"regexp"
	"testing"
)

func TestParsePdshLine(t *testing.T) {
	cases := []struct {
		in         string
		host, rest string
		ok         bool
	}{
		{"web01: /appl -ro nas-a:/appl2/appl2", "web01", "/appl -ro nas-a:/appl2/appl2", true},
		{"web01:/appl", "web01", "/appl", true},
		{"db-02: ssh: connect to host db-02 port 22: Connection refused", "db-02", "ssh: connect to host db-02 port 22: Connection refused", true},
		{"no colon here", "", "", false},
		{": leading", "", "", false},
		{"has space: x", "", "", false},
	}
	for _, c := range cases {
		h, r, ok := parsePdshLine(c.in)
		if ok != c.ok || h != c.host || r != c.rest {
			t.Errorf("parsePdshLine(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, h, r, ok, c.host, c.rest, c.ok)
		}
	}
}

func TestExtractConfigValue(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"/appl -ro,soft nas-a:/appl2/appl2"}, "nas-a:/appl2/appl2"},
		{[]string{"# comment", "/appl  -fstype=nfs  nas-b:/appl2/appl2"}, "nas-b:/appl2/appl2"},
		{[]string{""}, ""},
		{[]string{"/appl nomount"}, "nomount"},
		{[]string{"ERROR: grep exited 1"}, ""},              // '/' 로 시작 안 함 → 설정값 없음
		{[]string{"bash: cat: command not found"}, ""},      // 에러 라인 무시
	}
	for _, c := range cases {
		if got := extractConfigValue(c.in); got != c.want {
			t.Errorf("extractConfigValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestApplStatus(t *testing.T) {
	mounts := []MountRule{{Name: "nas-a", Location: "A센터"}, {Name: "nas-b", Location: "B센터"}}
	cases := []struct {
		cv, loc    string
		mark, note string
	}{
		{"nas-a:/appl2/appl2", "A센터", "O", ""},
		{"nas-a:/appl2/appl2", "B센터", "X", ""},
		{"nas-x:/appl2/appl2", "A센터", "X", "mountpoint 미정의"},
		{"", "A센터", "", ""},
	}
	for _, c := range cases {
		m, n := ApplStatus(mounts, c.cv, c.loc)
		if m != c.mark || n != c.note {
			t.Errorf("ApplStatus(%q,%q) = (%q,%q), want (%q,%q)", c.cv, c.loc, m, n, c.mark, c.note)
		}
	}
}

func TestApplyInfraRegex(t *testing.T) {
	// gossh 출력 "hostname: 출력값" 에서 파싱된 '출력값' 에 적용된다.
	re := regexp.MustCompile(`INFO\s+(.+)`)
	cases := []struct {
		re   *regexp.Regexp
		out  string
		want string
	}{
		{re, "INFO ldap infra site", "ldap infra site"},
		{re, "WARN something", ""},                    // 매칭 안 됨 → 미조사
		{nil, "ldap infra site", "ldap infra site"},   // 정규식 없으면 출력값 그대로
		{re, "", ""},
	}
	for _, c := range cases {
		if got := applyInfraRegex(c.re, c.out); got != c.want {
			t.Errorf("applyInfraRegex(%v, %q) = %q, want %q", c.re, c.out, got, c.want)
		}
	}
}

func TestFirstNonEmptyLine(t *testing.T) {
	if got := firstNonEmptyLine("\n  \r\n ldap infra site \n more"); got != "ldap infra site" {
		t.Errorf("firstNonEmptyLine = %q", got)
	}
}

func TestDetectError(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ssh: Could not resolve hostname bm01ev09: Name or service not known", "DNS 미등록"},
		{"ssh: connect to host x port 22: Connection timed out", "타임아웃"},
		{"ssh: connect to host x port 22: Connection refused", "접속불가"},
		{"/appl -ro nas-a:/x", ""},
	}
	for _, c := range cases {
		if got := detectError([]string{c.in}); got != c.want {
			t.Errorf("detectError(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestVMName(t *testing.T) {
	if got := vmName("bm01", 2); got != "bm01ev02" {
		t.Errorf("vmName(bm01,2) = %q, want bm01ev02", got)
	}
}

func TestNormalizeField(t *testing.T) {
	if got := normalizeField("a\tb\nc\r"); got != "a b c" {
		t.Errorf("normalizeField = %q, want %q", got, "a b c")
	}
}
