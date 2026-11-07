package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLdapCmdCshSafe(t *testing.T) {
	got := ldapCmd("/user/asdf/ldap_check.sh")
	want := "bash /user/asdf/ldap_check.sh; lscpu | grep -i 'per core'"
	if got != want {
		t.Errorf("got %s", got)
	}
	if strings.ContainsAny(got, "${}!*()") {
		t.Errorf("csh 에서 문제될 문자: %s", got)
	}
	if got := ldapCmd("/a b/x.sh"); !strings.HasPrefix(got, "bash '/a b/x.sh';") {
		t.Errorf("공백 경로: %s", got)
	}
}

func lines(s ...string) string { return strings.Join(s, "\n") + "\n" }

func TestParseSummarizeLdap(t *testing.T) {
	out := lines(
		// ok1: 전부 OK, H/T 2
		"ok1: OK\t/zxcv\tresolv.conf",
		"ok1: OK\t/zxcv\tchrony.conf\tchrony",
		"ok1: OK\t/zxcv\tldap.conf\ta1",
		"ok1: OK\t/zxcv\tautofs.conf",
		"ok1: OK\t/zxcv\tautofs_ldap_auth.conf",
		"ok1: OK\t/zxcv\tsssd.conf",
		"ok1: OK\t/zxcv\tauto.appl\ta1",
		"ok1: Thread(s) per core:    2",
		// bad: autofs.conf FAIL, nslcd/ntp, H/T 1
		"bad: OK\t/zxcv\tresolv.conf",
		"bad: OK\t/zxcv\tntp.conf\tntp",
		"bad: OK\t/zxcv\tldap.conf\ta1",
		"bad: FAIL\t/zxcv\tautofs.conf",
		"bad: OK\t/zxcv\tautofs_ldap_auth.conf",
		"bad: OK\t/zxcv\tnslcd.conf",
		"bad: OK\t/zxcv\tauto.appl\ta1",
		"bad: Thread(s) per core:    1",
		// missing: ldap_check 출력 없음(스크립트 없음), H/T 만
		"missing: bash: /user/asdf/ldap_check.sh: No such file or directory",
		"missing: Thread(s) per core:    4",
		// ldap FAIL 한 줄(sssd) + auto.appl-wappl FAIL
		"two: FAIL\t/zxcv\tsssd.conf",
		"two: OK\t/zxcv\tldap.conf\ta1",
		"two: FAIL\t/zxcv\tauto.appl-wappl\ta1",
		"two: OK\t/zxcv\tauto.appl\ta1",
		// /unknown 불일치
		"mix: FAIL\t/unknown\tinfra-mismatch\tdns+ntp=zxcv ldap=zxcv appl=qwer",
		"mix: FAIL\t/unknown\tresolv.conf",
		"mix: Thread(s) per core:    2",
		// 현재 ldap_check.sh 한 줄 요약 + gossh ERROR 접두사
		"one: INFO\tLDAP\tzxcv\ta1",
		"one: Thread(s) per core:    2",
		"onefail: ERROR: FAIL\tLDAP\tUNDEFINED",
		"onefail: Thread(s) per core:    1",
		// H/T 를 못 읽음
		"noht: OK\t/zxcv\tresolv.conf",
		"garbage line",
		"down: SSH 접속 실패: dial tcp down:22: i/o timeout",
	)
	res := parseLdapOutput(out)
	if _, ok := res["down"]; ok {
		t.Error("접속 실패 호스트가 결과에 있음")
	}
	want := map[string]Check{
		"ok1":     {LDAP: "OK", DNSNTP: "OK", Autofs: "OK", HT: "ON"},
		"bad":     {LDAP: "OK", DNSNTP: "OK", Autofs: "FAIL", HT: "OFF"},
		"missing": {LDAP: "-", DNSNTP: "-", Autofs: "-", HT: "ON"},
		"two":     {LDAP: "FAIL", DNSNTP: "-", Autofs: "FAIL", HT: "-"},
		"mix": {LDAP: "FAIL", DNSNTP: "FAIL", Autofs: "FAIL", HT: "ON",
			Note: "인프라 불일치 (dns+ntp=zxcv ldap=zxcv appl=qwer)"},
		"one":     {LDAP: "OK", DNSNTP: "OK", Autofs: "OK", HT: "ON"},
		"onefail": {LDAP: "FAIL", DNSNTP: "FAIL", Autofs: "FAIL", HT: "OFF", Note: "ldap_check UNDEFINED"},
		"noht":    {LDAP: "-", DNSNTP: "OK", Autofs: "-", HT: "-"},
	}
	if len(res) != len(want) {
		t.Errorf("호스트 수 %d, want %d", len(res), len(want))
	}
	for h, w := range want {
		a := res[h]
		if a == nil {
			t.Errorf("%s 없음", h)
			continue
		}
		if got := a.summarize(); got != w {
			t.Errorf("%s: got %+v want %+v", h, got, w)
		}
	}
}

// ldapEnv 는 스텁 gossh 와 호출 기록 파일을 준비한 Config 와 gossh 호출 수를 세는 함수를 돌려준다.
func ldapEnv(t *testing.T, state string) (*Config, func() int) {
	t.Helper()
	cfg, dir := stubEnv(t, state)
	cfg.LdapCheckSh = "/user/asdf/ldap_check.sh"
	logf := filepath.Join(dir, "calls.log")
	t.Setenv("STUB_GOSSH_LOG", logf)
	return cfg, func() int {
		b, _ := os.ReadFile(logf)
		return strings.Count(string(b), "\n")
	}
}

func doneCollector(t *testing.T, cfg *Config, hosts []string, done ...string) (*Collector, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.json")
	col, err := NewCollector(cfg, hosts, path, defaultDeps())
	if err != nil {
		t.Fatal(err)
	}
	col.mu.Lock()
	for _, h := range done {
		col.recs[h] = Record{Name: h, Vendor: cfg.Vendor(h), Status: StatusDone}
	}
	col.mu.Unlock()
	col.publish(false)
	return col, path
}

func TestLdapQueryStub(t *testing.T) {
	cfg, _ := ldapEnv(t, "a done\nb done\nc done\nd done\n")
	t.Setenv("STUB_LDAP_FAIL", "b")
	t.Setenv("STUB_LDAP_UNKNOWN", "c")
	t.Setenv("STUB_HT_OFF", "a")
	res, err := LdapQuery(cfg, []string{"a", "b", "c", "d", "e"}) // e 는 ssh 실패
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Check{
		"a": {LDAP: "OK", DNSNTP: "OK", Autofs: "OK", HT: "OFF"},
		"b": {LDAP: "OK", DNSNTP: "OK", Autofs: "FAIL", HT: "ON"},
		"c": {LDAP: "FAIL", DNSNTP: "FAIL", Autofs: "FAIL", HT: "ON",
			Note: "인프라 불일치 (dns+ntp=zxcv ldap=zxcv appl=qwer)"},
		"d": {LDAP: "OK", DNSNTP: "OK", Autofs: "OK", HT: "ON"},
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %+v\nwant %+v", res, want)
	}
}

func TestLDAPReportCache(t *testing.T) {
	cfg, calls := ldapEnv(t, "a done\nb done\nc done\n") // d 는 stub 상태에 없음 = ssh 실패
	t.Setenv("STUB_LDAP_FAIL", "b")
	col, path := doneCollector(t, cfg, []string{"a", "b", "c", "d", "i"}, "a", "b", "d")
	// i 는 설치중이라 대상이 아니다
	if n := col.LDAPPending(); n != 3 {
		t.Fatalf("대상 %d대, want 3", n)
	}
	if got := col.ReportBusy('l'); got != "LDAP 점검중... (3대)" {
		t.Errorf("busy = %q", got)
	}
	out := col.LDAPReport()
	want := "hostname\tLDAP\tDNS · NTP\tautofs\tH/T\n" +
		"a\tOK\tOK\tOK\tON\n" +
		"b\tOK\tOK\tFAIL\tON\n" +
		"(점검 결과 없음 1대 - ssh 실패 등, 다시 열면 재시도)"
	if out != want {
		t.Errorf("1차 표:\n%q\nwant\n%q", out, want)
	}
	if calls() != 1 {
		t.Fatalf("gossh %d 번", calls())
	}
	// ssh 실패 호스트(d)는 캐시되지 않고 다음에 다시 점검된다. 저장 파일에는 a, b 결과가 있다.
	if n := col.LDAPPending(); n != 1 || col.ldapTargets()[0] != "d" {
		t.Fatalf("남은 대상 %v", col.ldapTargets())
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"autofs": "FAIL"`) {
		t.Errorf("state.json 에 결과가 저장되지 않음:\n%s", b)
	}
	// d 가 살아나면 d 만 점검한다(a, b 는 캐시)
	st := filepath.Join(filepath.Dir(os.Getenv("STUB_GOSSH_STATE")), "state")
	os.WriteFile(st, []byte("a done\nb done\nc done\nd done\n"), 0o644)
	out = col.LDAPReport()
	if calls() != 2 {
		t.Fatalf("gossh %d 번, want 2", calls())
	}
	if b, _ := os.ReadFile(os.Getenv("STUB_GOSSH_LOG")); !strings.HasSuffix(strings.TrimSpace(string(b)), "ldap_check.sh; lscpu | grep -i 'per core'") {
		t.Errorf("gossh 호출: %s", b)
	}
	if !strings.Contains(out, "\nd\tOK\tOK\tOK\tON") || strings.Contains(out, "점검 결과 없음") {
		t.Errorf("2차 표:\n%s", out)
	}
	// 모두 캐시되면 gossh 를 실행하지 않는다
	out2 := col.LDAPReport()
	if calls() != 2 || out2 != out {
		t.Errorf("캐시 실패: gossh %d 번\n%s", calls(), out2)
	}
	// 다시 실행(새 Collector, 같은 state.json)해도 캐시가 유지된다
	col2, err := NewCollector(cfg, []string{"a", "b", "c", "d", "i"}, path, defaultDeps())
	if err != nil {
		t.Fatal(err)
	}
	if n := col2.LDAPPending(); n != 0 {
		t.Errorf("재시작 후 대상 %d대, want 0", n)
	}
}

func TestLDAPReportUnsetAndEmpty(t *testing.T) {
	cfg, calls := ldapEnv(t, "a done\n")
	cfg.LdapCheckSh = ""
	col, _ := doneCollector(t, cfg, []string{"a"}, "a")
	if got := col.LDAPReport(); got != "ldap_check_sh 미설정" || calls() != 0 {
		t.Errorf("미설정: %q gossh %d 번", got, calls())
	}
	cfg.LdapCheckSh = "/x/ldap_check.sh"
	col2, _ := doneCollector(t, cfg, []string{"a"})
	want := "hostname\tLDAP\tDNS · NTP\tautofs\tH/T\n(설치완료 서버가 없습니다)"
	if got := col2.LDAPReport(); got != want || calls() != 0 {
		t.Errorf("대상 없음: %q gossh %d 번", got, calls())
	}
}

func TestLdapTableSortedWithNote(t *testing.T) {
	snap := &Snapshot{Records: []Record{
		{Name: "z", Status: StatusDone, Check: Check{LDAP: "OK", DNSNTP: "OK", Autofs: "OK", HT: "ON"}},
		{Name: "m", Status: StatusRebooted, Check: Check{LDAP: "FAIL", DNSNTP: "FAIL", Autofs: "FAIL", HT: "-", Note: "인프라 불일치 (x)"}},
		{Name: "o", Status: StatusOff, Check: Check{LDAP: "OK"}}, // 완료후꺼짐은 표에서 제외
		{Name: "i", Status: StatusInstalling},
	}}
	want := "hostname\tLDAP\tDNS · NTP\tautofs\tH/T\n" +
		"m\tFAIL\tFAIL\tFAIL\t-\t인프라 불일치 (x)\n" +
		"z\tOK\tOK\tOK\tON"
	if got := ldapTable(snap); got != want {
		t.Errorf("got %q", got)
	}
}

func TestExpandTabsWrapText(t *testing.T) {
	got := wrapText("hostname\tLDAP\tDNS · NTP\tautofs\tH/T\nsvr001\tOK\tOK\tOK\tON", 40)
	want := []string{" hostname  LDAP  DNS · NTP  autofs  H/T", " svr001    OK    OK         OK      ON"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}
