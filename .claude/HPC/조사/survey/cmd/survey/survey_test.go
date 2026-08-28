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
	re := regexp.MustCompile(`^INFO\s+\S+\s+(\S+)`)
	fb := regexp.MustCompile(`ou=([^,[:space:]]+)`)
	cases := []struct {
		re   *regexp.Regexp
		out  string
		want string
	}{
		{re, "INFO ldap infra site match", "infra"},    // 정상 → 3번째 토큰
		{re, "INFO\tLDAP\t[infra]", "[infra]"},         // 탭 구분도 매칭
		{re, "FAIL LDAP 확인필요", ""},                 // INFO 로 시작 안 함 → 폴백
		{re, "FAIL LDAP undefined", ""},
		{fb, `binddn cn=proxy,ou=SDC,dc=corp`, "SDC"},  // 폴백: binddn 에서 ou 추출
		{nil, "그대로", "그대로"},
		{re, "", ""},
	}
	for _, c := range cases {
		if got := applyInfraRegex(c.re, c.out); got != c.want {
			t.Errorf("applyInfraRegex(%v, %q) = %q, want %q", c.re, c.out, got, c.want)
		}
	}
}

func TestSplitSDC(t *testing.T) {
	rows := [][]string{
		{"h1", "loc", "st", "cv", "업무망", "O", ""},
		{"h2", "loc", "st", "cv", "SDC", "O", ""},
		{"h3", "loc", "st", "cv", " SDC ", "X", ""},
	}
	sdc, rest := splitSDC(nil, rows)
	if len(sdc) != 2 || len(rest) != 1 || rest[0][0] != "h1" {
		t.Errorf("splitSDC = sdc %d, rest %d (rest0=%v)", len(sdc), len(rest), rest[0])
	}
}

func TestMatchInfraLines(t *testing.T) {
	re := regexp.MustCompile(`^INFO\s+\S+\s+(\S+)`)
	// 두 줄 중 INFO 로 시작하는 줄만 고른다
	if got := matchInfraLines(re, []string{"FAIL ldap_site", "INFO ldap infra"}); got != "infra" {
		t.Errorf("matchInfraLines(2줄) = %q, want infra", got)
	}
	if got := matchInfraLines(re, []string{"INFO ldap infra site match"}); got != "infra" {
		t.Errorf("matchInfraLines(1줄) = %q, want infra", got)
	}
	if got := matchInfraLines(re, []string{"FAIL ldap_site", "FAIL LDAP 확인필요"}); got != "" {
		t.Errorf("matchInfraLines(매칭없음) = %q, want empty", got)
	}
	// re 가 nil 이면 첫 비어있지 않은 줄
	if got := matchInfraLines(nil, []string{"", "  ", "second"}); got != "second" {
		t.Errorf("matchInfraLines(nil) = %q, want second", got)
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
		{"dial tcp: lookup bm01ev09 on 10.0.0.1:53: no such host", "DNS 미등록"},
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

func TestMapInfraValue(t *testing.T) {
	if got := mapInfraValue("VIP"); got != "SLSI_VIP" {
		t.Errorf("mapInfraValue(VIP) = %q, want SLSI_VIP", got)
	}
	if got := mapInfraValue(" VIP "); got != "SLSI_VIP" {
		t.Errorf("mapInfraValue( VIP ) = %q, want SLSI_VIP", got)
	}
	if got := mapInfraValue("infra"); got != "infra" {
		t.Errorf("mapInfraValue(infra) = %q, want infra", got)
	}
}

func TestFallbackRegexUidOu(t *testing.T) {
	re := regexp.MustCompile(`(?:uid|ou)=([^,\s]+)`)
	cases := map[string]string{
		"binddn uid=svc_ldap,ou=SDC,dc=corp": "svc_ldap",
		`binddn "cn=mgr,ou=SDC,dc=corp"`:     "SDC",
		"binddn cn=mgr,dc=corp":              "",
	}
	for in, want := range cases {
		if got := matchInfraLines(re, []string{in}); got != want {
			t.Errorf("uid/ou 정규식(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeField(t *testing.T) {
	if got := normalizeField("a\tb\nc\r"); got != "a b c" {
		t.Errorf("normalizeField = %q, want %q", got, "a b c")
	}
}
