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
	if !strings.HasPrefix(got, "bash /user/asdf/ldap_check.sh; lscpu | grep -i 'per core'; ") {
		t.Errorf("앞부분: %s", got)
	}
	// csh/tcsh 에서 문제되는 것: 변수, 중괄호, 명령 치환, stderr 만 리다이렉트, 히스토리 !
	for _, bad := range []string{"$", "{", "}", "2>", "!", "`"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q 포함: %s", bad, got)
		}
	}
	for _, want := range []string{"PM_DNS", "PM_NTPC", "PM_NTPN", "PM_CHRONY", "PM_APPL", "test -f /etc/resolv.conf &&", "test -f /etc/auto.appl &&"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q 없음: %s", want, got)
		}
	}
	if got := ldapCmd("/a b/x.sh"); !strings.HasPrefix(got, "bash '/a b/x.sh';") {
		t.Errorf("공백 경로: %s", got)
	}
}

func lines(s ...string) string { return strings.Join(s, "\n") + "\n" }

// sum 은 한 호스트의 출력 줄들을 요약한다.
func sum(t *testing.T, ls ...string) Check {
	t.Helper()
	var sb strings.Builder
	for _, l := range ls {
		sb.WriteString("h: " + l + "\n")
	}
	a := parseLdapOutput(sb.String())["h"]
	if a == nil {
		t.Fatalf("호스트 없음: %q", ls)
	}
	return a.summarize()
}

func TestSummarizeLdapColumn(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		ldap string
		note string
	}{
		{"INFO 한 줄: infra 만", []string{"INFO\tLDAP\tzxcv\ta1"}, "zxcv", ""},
		{"OK 한 줄", []string{"OK\tLDAP\tqwer\ta1"}, "qwer", ""},
		{"infra 만(site 없음)", []string{"INFO\tLDAP\tqwer"}, "qwer", ""},
		{"FAIL UNDEFINED", []string{"FAIL\tLDAP\tUNDEFINED"}, "FAIL", "ldap_check UNDEFINED"},
		{"gossh ERROR 접두사", []string{"ERROR: FAIL\tLDAP\tUNDEFINED"}, "FAIL", "ldap_check UNDEFINED"},
		{"출력 없음", []string{"bash: /x/ldap_check.sh: No such file or directory", "Thread(s) per core: 2"}, "-", ""},
		{"옛 형식: ldap.conf 의 infra", []string{"OK\t/zxcv\tresolv.conf", "OK\t/qwer\tldap.conf\ta1", "OK\t/zxcv\tautofs.conf"}, "qwer", ""},
		{"옛 형식: sssd 만", []string{"OK\t/zxcv\tsssd.conf"}, "zxcv", ""},
		{"옛 형식: FAIL 하나", []string{"OK\t/zxcv\tldap.conf\ta1", "FAIL\t/zxcv\tsssd.conf"}, "FAIL", ""},
		{"옛 형식: LDAP 아닌 파일의 FAIL 은 무시", []string{"OK\t/zxcv\tldap.conf\ta1", "FAIL\t/zxcv\tautofs.conf"}, "zxcv", ""},
		{"옛 형식: unknown", []string{"FAIL\t/unknown\tinfra-mismatch\ta=1 b=2", "FAIL\t/unknown\tresolv.conf"}, "FAIL", "인프라 불일치 (a=1 b=2)"},
	}
	for _, tc := range tests {
		got := sum(t, tc.in...)
		if got.LDAP != tc.ldap || got.Note != tc.note {
			t.Errorf("%s: LDAP=%q note=%q, want %q %q", tc.name, got.LDAP, got.Note, tc.ldap, tc.note)
		}
		if got.Ver != 2 {
			t.Errorf("%s: Ver=%d", tc.name, got.Ver)
		}
	}
}

func TestSummarizeDnsNtp(t *testing.T) {
	d := func(ips ...string) []string {
		var o []string
		for _, ip := range ips {
			o = append(o, "PM_DNS nameserver "+ip)
		}
		return o
	}
	n := func(ips ...string) []string {
		o := []string{"PM_CHRONY"}
		for _, ip := range ips {
			o = append(o, "PM_NTPC server "+ip+" iburst")
		}
		return o
	}
	cat := func(parts ...[]string) []string {
		var o []string
		for _, p := range parts {
			o = append(o, p...)
		}
		return o
	}
	tests := []struct {
		name string
		in   []string
		want string
		note string
	}{
		{"같음", cat(d("10.1.1.10", "10.1.1.11"), n("10.1.1.10", "10.1.1.11")), "10.1.1.10/10.1.1.11", ""},
		{"순서 무시", cat(d("10.1.1.11", "10.1.1.10"), n("10.1.1.10", "10.1.1.11")), "10.1.1.10/10.1.1.11", ""},
		{"중복 무시", cat(d("10.1.1.10", "10.1.1.10", "10.1.1.11"), n("10.1.1.11", "10.1.1.10", "10.1.1.11")), "10.1.1.10/10.1.1.11", ""},
		{"IPv4 숫자 정렬", cat(d("10.10.1.10", "10.10.1.9"), n("10.10.1.9", "10.10.1.10")), "10.10.1.9/10.10.1.10", ""},
		{"하나씩 같음", cat(d("1.1.1.1"), n("1.1.1.1")), "1.1.1.1", ""},
		{"집합이 다름", cat(d("10.1.1.10", "10.1.1.11"), n("10.1.2.20")), "다름", "DNS 10.1.1.10/10.1.1.11 NTP 10.1.2.20"},
		{"다름: 각 쪽 오름차순", cat(d("10.0.0.20", "10.0.0.3"), n("9.9.9.9", "10.0.0.1")), "다름", "DNS 10.0.0.3/10.0.0.20 NTP 9.9.9.9/10.0.0.1"},
		{"NTP 비어 있음", d("10.1.1.10"), "다름", "DNS 10.1.1.10 NTP -"},
		{"DNS 비어 있음", n("10.1.1.10"), "다름", "DNS - NTP 10.1.1.10"},
		{"둘 다 비어 있음", []string{"Thread(s) per core: 2"}, "-", ""},
		{"chrony 있고 server 없음 + DNS", cat(d("10.1.1.10"), []string{"PM_CHRONY"}), "다름", "DNS 10.1.1.10 NTP -"},
		{"IP 아닌 이름은 IP 뒤에 사전순", cat(d("b.example", "10.1.1.10", "a.example"), n("a.example", "b.example", "10.1.1.10")), "10.1.1.10/a.example/b.example", ""},
		{"chrony.conf 없으면 ntp.conf", []string{"PM_DNS nameserver 10.1.1.10", "PM_NTPN server 10.1.1.10"}, "10.1.1.10", ""},
		{"chrony.conf 가 있으면 ntp.conf 무시", []string{"PM_DNS nameserver 10.1.1.10", "PM_CHRONY", "PM_NTPC pool 10.1.1.10", "PM_NTPN server 9.9.9.9"}, "10.1.1.10", ""},
	}
	for _, tc := range tests {
		got := sum(t, tc.in...)
		if got.DNSNTP != tc.want || got.Note != tc.note {
			t.Errorf("%s: DNSNTP=%q note=%q, want %q %q", tc.name, got.DNSNTP, got.Note, tc.want, tc.note)
		}
	}
}

func TestSummarizeAutofsAndNote(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
		note string
	}{
		{"둘 다 같음: appl 값", []string{"PM_APPL appl -rw nas01:/export/appl", "PM_APPL wappl -rw nas01:/export/appl"}, "nas01:/export/appl", ""},
		{"앞의 / 있어도 됨", []string{"PM_APPL /appl\t-rw\tnas01:/a", "PM_APPL /wappl\t-rw\tnas01:/a"}, "nas01:/a", ""},
		{"다름", []string{"PM_APPL appl -rw nas01:/export/appl", "PM_APPL /wappl -rw nas02:/export/wappl"}, "다름", "/appl nas01:/export/appl /wappl nas02:/export/wappl"},
		{"appl 만", []string{"PM_APPL appl -rw nas01:/export/appl"}, "nas01:/export/appl", ""},
		{"wappl 만", []string{"PM_APPL wappl -rw nas03:/w"}, "nas03:/w", ""},
		{"없음", []string{"INFO\tLDAP\tzxcv"}, "-", ""},
		{"다른 키는 무시", []string{"PM_APPL site1 -rw nas:/x"}, "-", ""},
	}
	for _, tc := range tests {
		got := sum(t, tc.in...)
		if got.Autofs != tc.want || got.Note != tc.note {
			t.Errorf("%s: autofs=%q note=%q, want %q %q", tc.name, got.Autofs, got.Note, tc.want, tc.note)
		}
	}
	// 비고는 원인들을 "; " 로 잇고 순서는 LDAP, DNS·NTP, autofs
	got := sum(t, "FAIL\tLDAP\tUNDEFINED", "PM_DNS nameserver 10.1.1.10", "PM_CHRONY", "PM_NTPC server 10.1.2.20",
		"PM_APPL appl -rw a:/x", "PM_APPL wappl -rw b:/y", "Thread(s) per core: 1")
	want := Check{Ver: 2, LDAP: "FAIL", DNSNTP: "다름", Autofs: "다름", HT: "OFF",
		Note: "ldap_check UNDEFINED; DNS 10.1.1.10 NTP 10.1.2.20; /appl a:/x /wappl b:/y"}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	// H/T
	for tpc, ht := range map[string]string{"2": "ON", "4": "ON", "1": "OFF"} {
		if g := sum(t, "Thread(s) per core:    "+tpc); g.HT != ht {
			t.Errorf("tpc %s: %s", tpc, g.HT)
		}
	}
	if g := sum(t, "INFO\tLDAP\tzxcv"); g.HT != "-" {
		t.Errorf("H/T 모름: %s", g.HT)
	}
}

func TestParseLdapOutputHostsAndGarbage(t *testing.T) {
	res := parseLdapOutput(lines(
		"a: INFO\tLDAP\tzxcv\ta1",
		"a: Thread(s) per core:    2",
		"garbage line",
		"down: SSH 접속 실패: dial tcp down:22: i/o timeout",
	))
	if len(res) != 1 || res["a"] == nil {
		t.Errorf("hosts: %v", res)
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
	cfg, _ := ldapEnv(t, "a done\nb done\nc done\nd done\nf done\ng done\n")
	t.Setenv("STUB_LDAP_FAIL", "b")
	t.Setenv("STUB_LDAP_UNKNOWN", "c")
	t.Setenv("STUB_HT_OFF", "a")
	t.Setenv("STUB_DNS_DIFF", "f")
	t.Setenv("STUB_APPL_DIFF", "g")
	res, err := LdapQuery(cfg, []string{"a", "b", "c", "d", "e", "f", "g"}) // e 는 ssh 실패
	if err != nil {
		t.Fatal(err)
	}
	const ips, mnt = "10.10.1.10/10.10.1.11", "nas01:/export/appl"
	want := map[string]Check{
		"a": {Ver: 2, LDAP: "zxcv", DNSNTP: ips, Autofs: mnt, HT: "OFF"},
		"b": {Ver: 2, LDAP: "FAIL", DNSNTP: ips, Autofs: mnt, HT: "ON"},
		"c": {Ver: 2, LDAP: "FAIL", DNSNTP: ips, Autofs: mnt, HT: "ON",
			Note: "인프라 불일치 (dns+ntp=zxcv ldap=zxcv appl=qwer)"},
		"d": {Ver: 2, LDAP: "zxcv", DNSNTP: ips, Autofs: mnt, HT: "ON"},
		"f": {Ver: 2, LDAP: "zxcv", DNSNTP: "다름", Autofs: mnt, HT: "ON", Note: "DNS 10.10.1.10/10.10.1.11 NTP 10.10.1.10/10.10.2.20"},
		"g": {Ver: 2, LDAP: "zxcv", DNSNTP: ips, Autofs: "다름", HT: "ON", Note: "/appl nas01:/export/appl /wappl nas02:/export/wappl"},
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got %+v\nwant %+v", res, want)
	}
}

func TestLdapQueryStubOneline(t *testing.T) {
	cfg, _ := ldapEnv(t, "a done\nb done\n")
	t.Setenv("STUB_LDAP_ONELINE", "1")
	t.Setenv("STUB_LDAP_FAIL", "b")
	res, err := LdapQuery(cfg, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	const ips, mnt = "10.10.1.10/10.10.1.11", "nas01:/export/appl"
	want := map[string]Check{
		"a": {Ver: 2, LDAP: "zxcv", DNSNTP: ips, Autofs: mnt, HT: "ON"},
		"b": {Ver: 2, LDAP: "FAIL", DNSNTP: ips, Autofs: mnt, HT: "ON", Note: "ldap_check UNDEFINED"},
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
	want := "hostname\tLDAP\tDNS · NTP\tautofs\tH/T\t비고\n" +
		"a\tzxcv\t10.10.1.10/10.10.1.11\tnas01:/export/appl\tON\t\n" +
		"b\tFAIL\t10.10.1.10/10.10.1.11\tnas01:/export/appl\tON\t\n" +
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
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"ldap": "FAIL"`) || !strings.Contains(string(b), `"ver": 2`) {
		t.Errorf("state.json 에 결과가 저장되지 않음:\n%s", b)
	}
	// d 가 살아나면 d 만 점검한다(a, b 는 캐시)
	st := filepath.Join(filepath.Dir(os.Getenv("STUB_GOSSH_STATE")), "state")
	os.WriteFile(st, []byte("a done\nb done\nc done\nd done\n"), 0o644)
	out = col.LDAPReport()
	if calls() != 2 {
		t.Fatalf("gossh %d 번, want 2", calls())
	}
	if b, _ := os.ReadFile(os.Getenv("STUB_GOSSH_LOG")); !strings.Contains(strings.TrimSpace(string(b)), "ldap_check.sh; lscpu | grep -i 'per core'; test -f /etc/resolv.conf") {
		t.Errorf("gossh 호출: %s", b)
	}
	if !strings.Contains(out, "\nd\tzxcv\t") || strings.Contains(out, "점검 결과 없음") {
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

// v0.1.x 로 저장된 결과(Ver 없음)는 새 형식이 아니므로 다시 조사한다.
func TestLDAPReportOldVersionRequery(t *testing.T) {
	cfg, calls := ldapEnv(t, "a done\nb done\n")
	col, _ := doneCollector(t, cfg, []string{"a", "b"}, "a", "b")
	col.mu.Lock()
	r := col.recs["a"]
	r.Check = Check{LDAP: "OK", DNSNTP: "OK", Autofs: "OK", HT: "ON"} // 옛 형식
	col.recs["a"] = r
	r = col.recs["b"]
	r.Check = Check{Ver: 2, LDAP: "zxcv", DNSNTP: "-", Autofs: "-", HT: "ON"}
	col.recs["b"] = r
	col.mu.Unlock()
	col.publish(false)
	if tg := col.ldapTargets(); len(tg) != 1 || tg[0] != "a" {
		t.Fatalf("대상 %v, want [a]", tg)
	}
	// 옛 형식 값은 표에 나오지 않고 (결과 없음)으로 센다
	if tbl := ldapTable(col.Snapshot()); strings.Contains(tbl, "\na\t") || !strings.Contains(tbl, "점검 결과 없음 1대") {
		t.Errorf("표:\n%s", tbl)
	}
	out := col.LDAPReport()
	if calls() != 1 || !strings.Contains(out, "\na\tzxcv\t10.10.1.10/10.10.1.11\t") || !strings.Contains(out, "\nb\tzxcv\t-\t-\tON\t") {
		t.Errorf("calls=%d\n%s", calls(), out)
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
	want := "hostname\tLDAP\tDNS · NTP\tautofs\tH/T\t비고\n(설치완료 서버가 없습니다)"
	if got := col2.LDAPReport(); got != want || calls() != 0 {
		t.Errorf("대상 없음: %q gossh %d 번", got, calls())
	}
}

func TestLdapTableSortedWithNote(t *testing.T) {
	snap := &Snapshot{Records: []Record{
		{Name: "z", Status: StatusDone, Check: Check{Ver: 2, LDAP: "zxcv", DNSNTP: "10.1.1.10/10.1.1.11", Autofs: "nas01:/export/appl", HT: "ON"}},
		{Name: "m", Status: StatusRebooted, Check: Check{Ver: 2, LDAP: "FAIL", DNSNTP: "다름", Autofs: "nas01:/export/appl", HT: "-", Note: "ldap_check UNDEFINED; DNS 10.1.1.10 NTP -"}},
		{Name: "o", Status: StatusOff, Check: Check{Ver: 2, LDAP: "zxcv"}}, // 완료후꺼짐은 표에서 제외
		{Name: "i", Status: StatusInstalling},
	}}
	want := "hostname\tLDAP\tDNS · NTP\tautofs\tH/T\t비고\n" +
		"m\tFAIL\t다름\tnas01:/export/appl\t-\tldap_check UNDEFINED; DNS 10.1.1.10 NTP -\n" +
		"z\tzxcv\t10.1.1.10/10.1.1.11\tnas01:/export/appl\tON\t"
	if got := ldapTable(snap); got != want {
		t.Errorf("got %q", got)
	}
}

func TestExpandTabsWrapText(t *testing.T) {
	got := wrapText("hostname\tLDAP\tDNS · NTP\tautofs\tH/T\t비고\nsvr001\tOK\tOK\tOK\tON\t", 40)
	want := []string{" hostname  LDAP  DNS · NTP  autofs  H/T  비고", " svr001    OK    OK         OK      ON   "}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

// 옛 gossh 는 명령 앞뒤의 따옴표를 잘라 내므로 명령이 따옴표로 끝나면 안 된다.
func TestLdapCmdNoTrailingQuote(t *testing.T) {
	got := ldapCmd("/user/asdf/ldap_check.sh")
	if strings.HasSuffix(got, "'") || strings.HasPrefix(got, "'") {
		t.Errorf("따옴표로 끝남: %s", got)
	}
}
