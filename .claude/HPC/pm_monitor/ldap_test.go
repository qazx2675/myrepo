package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ---- 원격 명령 ----

func TestLdapCmdCshSafe(t *testing.T) {
	got := ldapCmd("/user/asdf/ldap_check.sh")
	if !strings.HasPrefix(got, "uname -r | sed 's/^/PM_KERN /'; lscpu | grep -i 'per core' | sed 's/^/PM_HT /'; ") {
		t.Errorf("앞부분: %s", got)
	}
	// csh/tcsh 에서 문제되는 것: 변수, 중괄호, 명령 치환, stderr 만 리다이렉트, 히스토리 !
	for _, bad := range []string{"$", "{", "}", "2>", "!", "`"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q 포함: %s", bad, got)
		}
	}
	for _, want := range []string{"PM_KERN", "PM_HT", "PM_DNS", "PM_APPL", "PM_LDC", "echo PM_LC; bash /user/asdf/ldap_check.sh; echo PM_LCE",
		"test -f /etc/resolv.conf &&", "test -f /etc/auto.appl &&", "test -f /etc/openldap/ldap.conf &&",
		"-e '^[[:space:]]*URI[[:space:]]' -e '^[[:space:]]*binddn[[:space:]]'"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q 없음: %s", want, got)
		}
	}
	if !strings.HasSuffix(got, "; echo PM_END") {
		t.Errorf("끝이 PM_END 가 아님: %s", got)
	}
	if got := ldapCmd("/a b/x.sh"); !strings.Contains(got, "bash '/a b/x.sh';") {
		t.Errorf("공백 경로: %s", got)
	}
	// ldap_check_sh 미설정이면 ldap_check 부분이 없다(설정 조사는 그대로)
	if got := ldapCmd(""); strings.Contains(got, "PM_LC") || strings.Contains(got, "bash") || !strings.Contains(got, "PM_LDC") {
		t.Errorf("미설정 명령: %s", got)
	}
}

// bindpw 는 절대 읽지 않는다: 명령 글자에 bindpw 가 없고, ldap.conf 는 URI·binddn 패턴으로만 grep 한다.
func TestLdapCmdNoBindpw(t *testing.T) {
	for _, sh := range []string{"/user/asdf/ldap_check.sh", ""} {
		got := ldapCmd(sh)
		if strings.Contains(strings.ToLower(got), "bindpw") {
			t.Fatalf("명령에 bindpw: %s", got)
		}
		if strings.Contains(got, "cat ") || strings.Contains(got, "grep -v") {
			t.Errorf("파일 전체를 읽는 명령: %s", got)
		}
	}
}

// 옛 gossh 는 명령 앞뒤의 따옴표를 잘라 내므로 명령이 따옴표로 시작·끝나면 안 된다.
func TestLdapCmdNoTrailingQuote(t *testing.T) {
	for _, sh := range []string{"/user/asdf/ldap_check.sh", "", "/a b/x.sh"} {
		got := ldapCmd(sh)
		if strings.HasSuffix(got, "'") || strings.HasPrefix(got, "'") {
			t.Errorf("따옴표로 시작·끝남: %s", got)
		}
	}
}

// ---- 출력 읽기 ----

func lines(s ...string) string { return strings.Join(s, "\n") + "\n" }

// sum 은 한 호스트의 출력 줄들을 요약한다(ldap_check_sh 설정됨).
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
	return a.summarize("/x/ldap_check.sh")
}

func TestUriHost(t *testing.T) {
	for in, want := range map[string]string{
		"ldap://server1/":          "server1",
		"ldap://server1":           "server1",
		"ldaps://ldap.cae.com:636": "ldap.cae.com",
		"ldaps://s2:636/":          "s2",
		"ldap://10.1.1.10:389/x":   "10.1.1.10",
		"ldapi://%2Fvar%2Frun/":    "%2Fvar%2Frun",
		"server3":                  "server3",
		"ldap://[fe80::1]:389/":    "fe80::1",
	} {
		if got := uriHost(in); got != want {
			t.Errorf("uriHost(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestParseLdapOutputAll(t *testing.T) {
	got := sum(t,
		"PM_KERN 4.18.0-553.el8.x86_64",
		"PM_HT Thread(s) per core:    2",
		"PM_DNS nameserver 10.1.1.11", // 적힌 순서 그대로(정렬하지 않음)
		"PM_DNS nameserver 10.1.1.10",
		"PM_APPL /appl\t-rw,soft\tnas01:/export/appl",
		"PM_APPL /appl\t-rw\tnas09:/second", // 첫 줄만
		"PM_LDC URI ldap://server1/ ldaps://server2:636/",
		"PM_LDC uri ldap://lower/", // 소문자 uri 는 무시
		"PM_LDC   URI ldap://server3",
		"PM_LDC BINDDN uid=upper", // 대문자 BINDDN 은 무시
		"PM_LDC binddn   uid=x,ou=user,ou=ldap_user,dc=cae,dc=com  ",
		"PM_LDC binddn uid=second", // 첫 binddn 만
		"PM_LC",
		"INFO\tLDAP\tzxcv\ta1",
		"PM_LCE",
		"PM_END",
	)
	want := Check{Ver: ldapCmdVer, Kernel: "4.18.0-553.el8.x86_64", Appl: "nas01:/export/appl", DNS: "10.1.1.11 10.1.1.10",
		LdapURI: "server1 server2 server3", BindDN: "uid=x,ou=user,ou=ldap_user,dc=cae,dc=com", HT: "ON", LdapCheck: "INFO LDAP zxcv a1"}
	if got != want {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseLdapCheckLines(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"한 줄 그대로(탭은 공백 하나)", []string{"PM_LC", "INFO\t\tLDAP\tzxcv", "PM_LCE"}, "INFO LDAP zxcv"},
		{"여러 줄은 ' ; '", []string{"PM_LC", "OK\t/zxcv\tldap.conf", "FAIL\t/zxcv\tsssd.conf", "PM_LCE"}, "OK /zxcv ldap.conf ; FAIL /zxcv sssd.conf"},
		{"gossh ERROR 접두사", []string{"PM_LC", "ERROR: FAIL\tLDAP\tUNDEFINED", "ERROR: PM_LCE"}, "FAIL LDAP UNDEFINED"},
		{"줄바꿈 없이 끝남", []string{"PM_LC", "INFO\tLDAP\tqwerPM_LCE"}, "INFO LDAP qwer"},
		{"출력 없음", []string{"PM_LC", "PM_LCE"}, "(출력 없음)"},
		{"표식 밖의 줄은 무시", []string{"INFO\tLDAP\tzz", "PM_LC", "INFO\tLDAP\tok", "PM_LCE", "garbage"}, "INFO LDAP ok"},
		{"ldap_check 안의 PM_ 모양 줄도 원문", []string{"PM_LC", "PM_KERN fake", "PM_LCE", "PM_KERN 1.2"}, "PM_KERN fake"},
	}
	for _, tc := range tests {
		if got := sum(t, tc.in...); got.LdapCheck != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got.LdapCheck, tc.want)
		}
	}
	if got := sum(t, "PM_LC", "PM_KERN fake", "PM_LCE", "PM_KERN 1.2"); got.Kernel != "1.2" {
		t.Errorf("ldap_check 출력이 커널로 읽힘: %q", got.Kernel)
	}
	// 미설정이면 ldap_check결과는 미설정 문구
	a := parseLdapOutput("h: PM_KERN 1\nh: PM_END\n")["h"]
	if got := a.summarize(""); got.LdapCheck != ldapUnset {
		t.Errorf("미설정: %q", got.LdapCheck)
	}
}

func TestParseHTAndMissing(t *testing.T) {
	for tpc, ht := range map[string]string{"2": "ON", "4": "ON", "1": "OFF"} {
		if g := sum(t, "PM_HT Thread(s) per core:    "+tpc); g.HT != ht {
			t.Errorf("tpc %s: %s", tpc, g.HT)
		}
	}
	// 파일이 없으면(줄이 없으면) 그 칸은 비어 있다(오류 아님)
	g := sum(t, "PM_END")
	if g.Kernel != "" || g.Appl != "" || g.DNS != "" || g.LdapURI != "" || g.BindDN != "" || g.HT != "" || !g.current() {
		t.Errorf("빈 칸: %+v", g)
	}
	// /wappl 은 더 쓰지 않는다
	if g := sum(t, "PM_APPL /wappl\t-rw\tnas02:/w", "PM_END"); g.Appl != "" {
		t.Errorf("wappl 을 읽음: %q", g.Appl)
	}
}

func TestParseLdapOutputHostsAndGarbage(t *testing.T) {
	res := parseLdapOutput(lines(
		"a: PM_KERN 1.0",
		"a: PM_END",
		"garbage line",
		"down: SSH 접속 실패: dial tcp down:22: i/o timeout",
	))
	if len(res) != 1 || res["a"] == nil || !res["a"].end {
		t.Errorf("hosts: %v", res)
	}
}

// ---- 스텁 gossh 로 조사 ----

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

// stubCfgFile 은 STUB_CFG 파일(호스트별 값)을 쓴다.
func stubCfgFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cfg.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STUB_CFG", p)
	return p
}

// defCheck 는 스텁 기본값으로 조사한 Check 다.
func defCheck() Check {
	return Check{Ver: ldapCmdVer, Kernel: "4.18.0-553.el8.x86_64", Appl: "nas01:/export/appl", DNS: "10.10.1.10 10.10.1.11",
		LdapURI: "ldap01.demo ldap02.demo", BindDN: "uid=pmuser,ou=user,ou=ldap_user,dc=cae,dc=com", HT: "ON", LdapCheck: "INFO LDAP zxcv a1"}
}

func TestLdapQueryStub(t *testing.T) {
	cfg, _ := ldapEnv(t, "a done\nb installing\nc done\nf done\n")
	stubCfgFile(t, "c\t5.14.0-1\t-\t10.0.0.2 10.0.0.1\tldap://s2/ ldap://s1/|ldaps://s3:636\tuid=c\t1\tOK /zxcv ldap.conf\n")
	t.Setenv("STUB_LDAP_FAIL", "b")
	t.Setenv("STUB_HT_OFF", "f")
	res, fails, err := LdapQuery(cfg, []string{"a", "b", "c", "e", "f"}) // e 는 ssh 실패
	if err != nil {
		t.Fatal(err)
	}
	b := defCheck()
	b.LdapCheck = "FAIL LDAP UNDEFINED"
	f := defCheck()
	f.HT = "OFF"
	want := map[string]Check{
		"a": defCheck(),
		"b": b,
		"c": {Ver: ldapCmdVer, Kernel: "5.14.0-1", DNS: "10.0.0.2 10.0.0.1", LdapURI: "s2 s1 s3", BindDN: "uid=c", HT: "OFF", LdapCheck: "OK /zxcv ldap.conf"},
		"f": f,
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("got  %+v\nwant %+v", res, want)
	}
	if len(fails) != 1 || !strings.Contains(fails["e"], "SSH 접속 실패") {
		t.Errorf("fails=%v", fails)
	}
}

func TestLdapQueryStubMultiLineAndUnset(t *testing.T) {
	cfg, _ := ldapEnv(t, "a done\n")
	stubCfgFile(t, "a\t1.0\tn:/a\t10.0.0.1\tldap://s1/\tuid=a\t2\tline1|line2\n")
	res, _, err := LdapQuery(cfg, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res["a"].LdapCheck; got != "line1 ; line2" {
		t.Errorf("여러 줄: %q", got)
	}
	cfg.LdapCheckSh = ""
	res, _, err = LdapQuery(cfg, []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res["a"]; got.LdapCheck != ldapUnset || got.Kernel != "1.0" {
		t.Errorf("미설정: %+v", got)
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

// l 은 대상 전체를 다시 조사한다. 응답하지 않은 서버는 이전 값을 그대로 두고 비고에 이유만 남긴다.
func TestLDAPReportReinvestigateKeepsOld(t *testing.T) {
	cfg, calls := ldapEnv(t, "a done\nb done\nc done\n") // d 는 stub 상태에 없음 = ssh 실패
	col, path := doneCollector(t, cfg, []string{"a", "b", "c", "d", "i"}, "a", "b", "c", "d")
	old := Check{Ver: ldapCmdVer, Kernel: "old-k", HT: "OFF", LdapCheck: "x"}
	col.mu.Lock()
	r := col.recs["d"]
	r.Check = old
	col.recs["d"] = r
	r = col.recs["i"] // 설치중인 OS 모드 서버도 l 대상이다(계획서 4.9)
	r.Status = StatusInstalling
	col.recs["i"] = r
	col.mu.Unlock()
	col.publish(false)
	if n := col.LDAPPending(); n != 5 {
		t.Fatalf("대상 %d대, want 5", n)
	}
	if got := col.ReportBusy('l'); got != "LDAP·설정 점검중... (5대)" {
		t.Errorf("busy = %q", got)
	}
	out := col.LDAPReport()
	if !strings.Contains(out, "응답 3대 · 무응답 2대(이전 값 유지): d i") || calls() != 1 {
		t.Fatalf("안내 %q, gossh %d 번", out, calls())
	}
	if got := recOf(col, "a").Check; got != defCheck() {
		t.Errorf("a: %+v", got)
	}
	d := recOf(col, "d").Check
	if d.Kernel != "old-k" || d.HT != "OFF" || !d.current() || !strings.HasPrefix(d.Note, "최근 조사 실패: SSH 접속 실패") {
		t.Errorf("d 는 이전 값 유지 + 비고여야 함: %+v", d)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"kernel": "4.18.0-553.el8.x86_64"`) || !strings.Contains(string(b), `"ver": 4`) {
		t.Errorf("state.json 에 결과가 저장되지 않음:\n%s", b)
	}
	// 다시 누르면 이미 결과가 있어도 전체를 다시 조사한다. d 가 살아나면 새 값, 비고는 지워진다
	st := os.Getenv("STUB_GOSSH_STATE")
	os.WriteFile(st, []byte("a done\nb done\nc done\nd done\ni installing\n"), 0o644)
	out = col.LDAPReport()
	if calls() != 2 || !strings.Contains(out, "응답 5대 · 무응답 0대") {
		t.Fatalf("2차: %q gossh %d 번", out, calls())
	}
	if got := recOf(col, "d").Check; got != defCheck() {
		t.Errorf("d 새 값: %+v", got)
	}
	// 다시 실행(새 Collector, 같은 state.json)해도 결과가 유지된다
	col2, err := NewCollector(cfg, []string{"a", "b", "c", "d", "i"}, path, defaultDeps())
	if err != nil {
		t.Fatal(err)
	}
	if got := recOf(col2, "d").Check; got != defCheck() {
		t.Errorf("재시작 후 d: %+v", got)
	}
}

// v0.4.0 이하(Ver 3 이하)로 저장된 결과는 새 형식이 아니므로 표에 값이 나오지 않고 자동 조사 대상이 된다.
func TestOldCheckVersionRequery(t *testing.T) {
	if (Check{Ver: 3, Kernel: "x"}).current() {
		t.Error("Ver 3 을 현재 형식으로 봄")
	}
	if !(Check{Ver: ldapCmdVer}).current() {
		t.Error("현재 Ver 를 옛 형식으로 봄")
	}
	path := filepath.Join(t.TempDir(), "h.pm_state.json")
	writeState(t, path, `{"version":1,"hosts":[{"name":"a","vendor":"D","status":"설치완료","check":{"ver":3,"ldap":"zxcv","dns_ntp":"다름","autofs":"x","ht":"ON"}}]}`)
	col, err := NewCollector(testCfg(), []string{"a"}, path, Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if tg := col.autoTargets(); len(tg) != 1 || tg[0] != "a" {
		t.Fatalf("자동 대상 %v", tg)
	}
	if tbl := ldapTable(col.Snapshot()); !strings.Contains(tbl, "\na\t\t\t\t\t\t\t(조사 전)") {
		t.Errorf("표:\n%s", tbl)
	}
}

func TestLDAPReportNoTargets(t *testing.T) {
	cfg, calls := ldapEnv(t, "")
	col, _ := doneCollector(t, cfg, nil)
	if got := col.LDAPReport(); got != "조사할 서버가 없습니다" || calls() != 0 {
		t.Errorf("대상 없음: %q gossh %d 번", got, calls())
	}
}

// ---- 자동 첫 조사(수집 회차) ----

// cfgFake 는 Deps.Cfg 가짜다: fail 에 있는 서버는 응답하지 않는다. 호출마다 대상 목록을 남긴다.
type cfgFake struct {
	fail  map[string]bool
	calls [][]string
}

func (c *cfgFake) query(_ *Config, hosts []string) (map[string]Check, map[string]string, error) {
	c.calls = append(c.calls, append([]string{}, hosts...))
	res, fails := map[string]Check{}, map[string]string{}
	for _, h := range hosts {
		if c.fail[h] {
			fails[h] = "응답 없음"
			continue
		}
		ck := defCheck()
		ck.Kernel = "k-" + h
		res[h] = ck
	}
	return res, fails, nil
}

func TestAutoFirstInvestigation(t *testing.T) {
	f := newFake()
	cf := &cfgFake{fail: map[string]bool{"b": true}}
	deps := f.deps()
	deps.Cfg = cf.query
	f.pmDone["a"], f.pmDone["b"] = true, true
	f.pmInst["inst"] = true
	f.pingDown["down"] = true
	f.refused["ref"] = true
	col, err := NewCollector(testCfg(), []string{"a", "b", "inst", "down", "ref", "y"}, filepath.Join(t.TempDir(), "s.json"), deps,
		WithYum(map[string]bool{"y": true}))
	if err != nil {
		t.Fatal(err)
	}
	col.Round(context.Background())
	// l 을 누르지 않아도 ping 이 닿는 OS 모드 서버(설치완료·설치중·22 거부)를 조사한다. ping X·yum 접속가능 전은 아니다
	if len(cf.calls) != 1 || strings.Join(cf.calls[0], ",") != "a,b,inst,ref" {
		t.Fatalf("1회차 조사 %v", cf.calls)
	}
	if got := recOf(col, "a").Check; !got.current() || got.Kernel != "k-a" {
		t.Errorf("a: %+v", got)
	}
	if recOf(col, "b").Check.current() {
		t.Error("실패한 b 에 결과가 생김")
	}
	// 다음 회차: 실패한 b 만 다시(성공한 서버는 다시 조사하지 않는다)
	f.advance(time.Minute)
	col.Round(context.Background())
	if len(cf.calls) != 2 || strings.Join(cf.calls[1], ",") != "b" {
		t.Fatalf("2회차 조사 %v", cf.calls)
	}
	// b 가 살아나면 채워지고, 그 뒤로는 자동 조사가 없다
	cf.fail["b"] = false
	f.advance(time.Minute)
	col.Round(context.Background())
	f.advance(time.Minute)
	col.Round(context.Background())
	if len(cf.calls) != 3 || strings.Join(cf.calls[2], ",") != "b" || !recOf(col, "b").Check.current() {
		t.Fatalf("3·4회차 조사 %v", cf.calls)
	}
	// ping X 서버는 자동으로 조사하지 않지만 l 로는 조사한다
	if recOf(col, "down").Check.current() {
		t.Fatal("ping X 서버를 자동 조사함")
	}
	col.LDAPReport()
	last := cf.calls[len(cf.calls)-1]
	if strings.Join(last, ",") != "a,b,inst,down,ref" || !recOf(col, "down").Check.current() {
		t.Errorf("l 재조사 대상 %v", last)
	}
}

// 한 회차의 자동 조사는 대수 제한 없이 대상 전부를 한 번에 조사한다.
func TestAutoInvestigationNoCap(t *testing.T) {
	f := newFake()
	cf := &cfgFake{fail: map[string]bool{}}
	deps := f.deps()
	deps.Cfg = cf.query
	var hosts []string
	for i := 0; i < 130; i++ {
		h := fmt.Sprintf("n%04d", i)
		hosts = append(hosts, h)
		f.pmDone[h] = true
	}
	col, _ := NewCollector(testCfg(), hosts, filepath.Join(t.TempDir(), "s.json"), deps)
	col.Round(context.Background())
	if len(cf.calls) != 1 || len(cf.calls[0]) != 130 || cf.calls[0][0] != hosts[0] {
		t.Fatalf("1회차 호출 %d회", len(cf.calls))
	}
	col.Round(context.Background())
	if len(cf.calls) != 1 { // 모두 조사했으니 2회차는 대상 없음
		t.Fatalf("2회차에 또 조사함 %d회", len(cf.calls))
	}
}
