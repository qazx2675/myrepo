// ldapbk_test.go - ldapbk.go 단위 테스트: 가짜 gossh/ssh 스텁(PATH 주입, 호스트별 가짜 루트)으로 수집·저장·비교·복원·평문 미노출
package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const lbSecret = "S3cr3tPW-xyz"

type lbEnv struct {
	hosts, fs, log, share, shareStat string
}

// lbSetup: 가짜 gossh(호스트 목록 파일의 호스트마다 호스트 디렉터리를 가짜 루트로 연결해 명령 실행, 디렉터리 없으면 무응답),
// 가짜 hostname, 가짜 ssh(os6 경유). 데이터 디렉터리·ldapRoot 도 설정한다.
func lbSetup(t *testing.T) lbEnv {
	t.Helper()
	needBash(t)
	for _, c := range []string{"base64", "stat", "awk", "find"} {
		if _, err := exec.LookPath(c); err != nil {
			t.Skip(c + " 없음")
		}
	}
	tmp := t.TempDir()
	e := lbEnv{hosts: filepath.Join(tmp, "hosts"), fs: filepath.Join(tmp, "fs"), log: filepath.Join(tmp, "stub.log"),
		share: filepath.Join(tmp, "share"), shareStat: filepath.Join(tmp, "share.stat")}
	bin := filepath.Join(tmp, "bin")
	os.MkdirAll(bin, 0755)
	os.MkdirAll(e.hosts, 0755)
	os.MkdirAll(e.share, 0755)
	os.WriteFile(filepath.Join(bin, "gossh"), []byte(`#!/bin/bash
echo "gossh $@" >> "$STUB_LOG"
while [ $# -gt 1 ]; do case $1 in -w) wf=$2; shift 2;; -pm|-script) shift;; *) break;; esac; done
cmd=$1
# 공유경로 관찰 훅: 대상이 명령을 실행하기 직전 공유경로 항목의 권한 기록 / 대상이 공유경로를 못 보는 상황 흉내
[ -n "$STUB_SHARE" ] && find "$STUB_SHARE" -mindepth 1 -printf '%m %P\n' >> "$STUB_SHARE_STAT" 2>/dev/null
[ -n "$STUB_HIDE_SHARE" ] && rm -rf "$STUB_SHARE"/.as_ldap_*
while read -r h; do
  [ -n "$h" ] || continue
  [ -d "$STUB_HOSTS/$h" ] || continue
  ln -sfn "$STUB_HOSTS/$h" "$STUB_FS"
  AS_HOST=$h bash -c "$cmd" 2>/dev/null | sed "s/^/$h: /"
done < "$wf"
`), 0755)
	os.WriteFile(filepath.Join(bin, "hostname"), []byte("#!/bin/sh\necho \"$AS_HOST\"\n"), 0755)
	os.WriteFile(filepath.Join(bin, "ssh"), []byte(`#!/bin/bash
echo "ssh $1" >> "$STUB_LOG"
shift
exec bash -c "$1"
`), 0755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STUB_LOG", e.log)
	t.Setenv("STUB_HOSTS", e.hosts)
	t.Setenv("STUB_FS", e.fs)
	t.Setenv("STUB_SHARE", e.share)
	t.Setenv("STUB_SHARE_STAT", e.shareStat)
	t.Setenv("STUB_HIDE_SHARE", "")
	t.Setenv("AUTO_SETUP_DIR", filepath.Join(tmp, "data"))
	t.Setenv("AUTO_SETUP_LDAP_S4_PREFIX", "")
	oldRoot, oldM, oldG, oldS := ldapRoot, os6_mgmt, os6_gossh, ldap_share_dir
	ldapRoot, ldap_share_dir = e.fs, e.share
	t.Cleanup(func() { ldapRoot, os6_mgmt, os6_gossh, ldap_share_dir = oldRoot, oldM, oldG, oldS })
	return e
}

func (e lbEnv) mkHost(t *testing.T, name, rel string, files map[string]string) {
	t.Helper()
	d := filepath.Join(e.hosts, name)
	os.RemoveAll(d)
	os.MkdirAll(filepath.Join(d, "etc"), 0755)
	if rel != "" {
		os.WriteFile(filepath.Join(d, "etc", "redhat-release"), []byte(rel+"\n"), 0644)
	}
	for p, c := range files {
		fp := filepath.Join(d, p)
		os.MkdirAll(filepath.Dir(fp), 0755)
		if err := os.WriteFile(fp, []byte(c), 0600); err != nil {
			t.Fatal(err)
		}
		os.Chmod(fp, 0640)
	}
}

func (e lbEnv) read(t *testing.T, host, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.hosts, host, p))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func lbConf(pw string) string { return lbConfU("svc_old", pw) }

// lbConfU: binddn uid 가 uid 인 ldap.conf
func lbConfU(uid, pw string) string {
	s := "URI ldap://a ldap://b\nBINDDN uid=" + uid + ",ou=user,dc=x\n"
	if pw != "" {
		s += "BINDPW " + pw + "\n"
	}
	return s
}

func lbJob(id string, hosts ...string) *Job {
	j := &Job{ID: id, Hosts: map[string]*Host{}}
	for _, h := range hosts {
		j.Hosts[h] = &Host{Route: "local"}
	}
	return j
}

func TestBackupCollectsAndStores(t *testing.T) {
	e := lbSetup(t)
	t.Setenv("AUTO_SETUP_LDAP_S4_PREFIX", "s4")
	e.mkHost(t, "n7", "CentOS Linux release 7.9.2009 (Core)", map[string]string{
		"etc/openldap/ldap.conf": lbConf(lbSecret), "etc/resolv.conf": "nameserver 1.1.1.1\n",
		"etc/nslcd.conf": "uri ldap://a\nbindpw " + lbSecret + "\n", "etc/ntp.conf": "server n1\n",
		"etc/sssd/sssd.conf": "[domain/x]\n", "etc/chrony.conf": "server stale\n"})
	e.mkHost(t, "n8", "Rocky Linux release 8.9 (Green Obsidian)", map[string]string{
		"etc/openldap/ldap.conf": lbConf(lbSecret), "etc/resolv.conf": "nameserver 2.2.2.2\n",
		"etc/sssd/sssd.conf": "[domain/x]\nldap_default_authtok = " + lbSecret + "\n", "etc/chrony.conf": "server c1\n",
		"etc/nslcd.conf": "stale\n"})
	e.mkHost(t, "s4n01", "Rocky Linux release 8.9 (Green Obsidian)", map[string]string{
		"etc/openldap/ldap.conf": lbConf(lbSecret), "etc/nslcd.conf": "uri ldap://a\n", "etc/ntp.conf": "server n1\n"})
	e.mkHost(t, "nofile", "Rocky Linux release 8.9 (Green Obsidian)", map[string]string{"etc/resolv.conf": "x\n"})
	// down 은 호스트 디렉터리 없음(무응답)

	j := lbJob("job1", "n7", "n8", "s4n01", "nofile", "down")
	res := NewRealLdapBackup().Backup(j, []string{"n7", "n8", "s4n01", "nofile", "down"})
	for _, h := range []string{"n7", "n8", "s4n01"} {
		if res[h].Backup != LdapBackupOK {
			t.Fatalf("%s: %+v", h, res[h])
		}
	}
	if res["nofile"].Backup != LdapBackupNone || res["nofile"].Reason != "ldap.conf 없음" {
		t.Fatalf("nofile: %+v", res["nofile"])
	}
	if res["down"].Backup != LdapBackupNone || res["down"].Reason != "무응답" {
		t.Fatalf("down: %+v", res["down"])
	}
	// gossh 는 한 번만 호출 (배치)
	lg, _ := os.ReadFile(e.log)
	if n := strings.Count(string(lg), "gossh "); n != 1 {
		t.Fatalf("gossh 호출 %d회", n)
	}

	base := filepath.Join(dataDir(), "ldapbak", "job1")
	// OS 분기별 파일 세트
	want := map[string][]string{
		"n7":    {"ldap.conf", "meta.json", "nslcd.conf", "ntp.conf", "resolv.conf"},
		"n8":    {"chrony.conf", "ldap.conf", "meta.json", "resolv.conf", "sssd.conf"},
		"s4n01": {"ldap.conf", "meta.json", "nslcd.conf", "ntp.conf"},
	}
	for h, names := range want {
		ents, err := os.ReadDir(filepath.Join(base, h))
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, en := range ents {
			got = append(got, en.Name())
			if st, _ := en.Info(); st.Mode().Perm() != 0600 {
				t.Fatalf("%s/%s 권한 %v", h, en.Name(), st.Mode().Perm())
			}
		}
		if strings.Join(got, ",") != strings.Join(names, ",") {
			t.Fatalf("%s 파일 %v, want %v", h, got, names)
		}
	}
	for _, d := range []string{filepath.Join(dataDir(), "ldapbak"), base, filepath.Join(base, "n7")} {
		if st, _ := os.Stat(d); st.Mode().Perm() != 0700 {
			t.Fatalf("%s 권한 %v", d, st.Mode().Perm())
		}
	}
	// meta: 원경로·mode·owner·os, bindpw 평문 없음
	mb, _ := os.ReadFile(filepath.Join(base, "n7", "meta.json"))
	var m ldapMeta
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatal(err)
	}
	if m.OSMajor != 7 || m.Auth != "nslcd" || m.Time != "ntp" || m.Host != "n7" || m.Collected == "" {
		t.Fatalf("meta: %+v", m)
	}
	found := false
	for _, f := range m.Files {
		if f.Name == "ldap.conf" {
			found = f.Path == ldapConfPath && f.Mode == "640" && f.Owner != "" && f.Group != ""
		}
	}
	if !found {
		t.Fatalf("meta files: %+v", m.Files)
	}
	if strings.Contains(string(mb), lbSecret) {
		t.Fatal("meta.json 에 bindpw 평문")
	}
	if b, _ := os.ReadFile(filepath.Join(base, "n7", "ldap.conf")); string(b) != lbConf(lbSecret) {
		t.Fatalf("백업 내용 불일치: %q", b)
	}
	// 반환값·gossh 호출 인자에도 평문 없음
	rb, _ := json.Marshal(res)
	if strings.Contains(string(rb), lbSecret) || strings.Contains(string(lg), lbSecret) {
		t.Fatal("반환값/명령줄에 bindpw 평문")
	}
}

func TestBackupOS6Route(t *testing.T) {
	e := lbSetup(t)
	os6_mgmt, os6_gossh = "mgmtx", "gossh"
	e.mkHost(t, "a1", "Rocky Linux release 8.9 (x)", map[string]string{"etc/openldap/ldap.conf": lbConf("pw1"), "etc/sssd/sssd.conf": "s\n", "etc/chrony.conf": "c\n"})
	e.mkHost(t, "b1", "Rocky Linux release 8.9 (x)", map[string]string{"etc/openldap/ldap.conf": lbConf("pw1"), "etc/sssd/sssd.conf": "s\n", "etc/chrony.conf": "c\n"})
	j := lbJob("job6", "a1", "b1")
	j.Hosts["b1"].Route = "os6"
	res := NewRealLdapBackup().Backup(j, []string{"a1", "b1"})
	if res["a1"].Backup != LdapBackupOK || res["b1"].Backup != LdapBackupOK {
		t.Fatalf("%+v", res)
	}
	lg, _ := os.ReadFile(e.log)
	if strings.Count(string(lg), "ssh mgmtx") != 1 || strings.Count(string(lg), "gossh ") != 2 {
		t.Fatalf("호출 로그:\n%s", lg)
	}
	// os6 설정이 비면 해당 호스트만 none
	os6_mgmt = ""
	res = NewRealLdapBackup().Backup(j, []string{"b1"})
	if res["b1"].Backup != LdapBackupNone {
		t.Fatalf("%+v", res["b1"])
	}
}

func lbBackup(t *testing.T, e lbEnv, jobID, host, rel string, files map[string]string) *Job {
	t.Helper()
	e.mkHost(t, host, rel, files)
	j := lbJob(jobID, host)
	if r := NewRealLdapBackup().Backup(j, []string{host}); r[host].Backup != LdapBackupOK {
		t.Fatalf("백업 실패: %+v", r[host])
	}
	return j
}

var lbOS8Old = map[string]string{
	"etc/openldap/ldap.conf": lbConf(lbSecret), "etc/resolv.conf": "nameserver 9.9.9.9\n",
	"etc/sssd/sssd.conf": "[domain/x]\nldap_default_authtok = " + lbSecret + "\n", "etc/chrony.conf": "server old\n"}

func TestBinddnUID(t *testing.T) {
	cases := []struct {
		name, conf, want string
		ok               bool
	}{
		{"기본", "binddn uid=asdf,ou=user,dc=x\n", "asdf", true},
		{"대문자 키워드", "BINDDN uid=asdf,ou=user,dc=x\n", "asdf", true},
		{"혼합 대소문자 키워드", "BindDN\tuid=asdf,ou=user\n", "asdf", true},
		{"앞 공백·탭", "  \t binddn   uid=asdf,ou=user\n", "asdf", true},
		{"큰따옴표", "binddn \"uid=asdf,ou=user,dc=x\"\n", "asdf", true},
		{"작은따옴표", "binddn 'uid=asdf,ou=user,dc=x'\n", "asdf", true},
		{"UID 대문자", "binddn UID=asdf,ou=user\n", "asdf", true},
		{"uid 가 앞이 아님", "binddn cn=x, uid=asdf ,ou=user\n", "asdf", true},
		{"쉼표 없이 끝", "binddn uid=asdf\n", "asdf", true},
		{"CRLF", "binddn uid=asdf,ou=user\r\nfoo\r\n", "asdf", true},
		{"여러 binddn 줄은 uid 가 있는 첫 줄", "binddn cn=admin,dc=x\nbinddn uid=first,ou=u\nbinddn uid=second,ou=u\n", "first", true},
		{"주석은 무시", "# binddn uid=no,ou=u\nbinddn uid=yes,ou=u\n", "yes", true},
		{"bindpw 는 보지 않음", "bindpw uid=notme,x\nbinddn uid=real,x\n", "real", true},
		{"binddn 없음", "URI ldap://a\nbindpw x\n", "", false},
		{"uid 없음", "binddn cn=admin,dc=x\n", "", false},
		{"uid 값 비어 있음", "binddn uid=,ou=u\n", "", false},
		{"binddn 값 없음", "binddn\n", "", false},
	}
	for _, c := range cases {
		got, ok := binddnUID([]byte(c.conf))
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got (%q,%v) want (%q,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// 원격 awk 규칙(binddnSnippet)과 Go 파서(binddnUID)가 같은 결과를 내야 한다
func TestBinddnSnippetMatchesGo(t *testing.T) {
	needBash(t)
	if _, err := exec.LookPath("awk"); err != nil {
		t.Skip("awk 없음")
	}
	confs := []string{
		"binddn uid=asdf,ou=user,dc=x\n", "BINDDN \"uid=asdf,ou=user\"\n", "BindDN\t'uid=asdf,ou=user'\n",
		"binddn cn=x, uid=asdf ,ou=user\n", "binddn cn=admin,dc=x\nbinddn uid=first,ou=u\nbinddn uid=second\n",
		"# binddn uid=no\nbinddn uid=yes,ou=u\n", "binddn uid=asdf\r\n", "URI x\nbindpw uid=no,x\n", "binddn uid=,ou=u\n", "",
	}
	for _, conf := range confs {
		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, "etc/openldap"), 0755)
		os.WriteFile(filepath.Join(root, "etc/openldap/ldap.conf"), []byte(conf), 0644)
		out, err := exec.Command("bash", "-c", "R="+shq(root)+"\n"+binddnSnippet).Output()
		if err != nil {
			t.Fatal(err)
		}
		in := parseInfo(strings.Split(strings.TrimSpace(string(out)), "\n"))
		want, _ := binddnUID([]byte(conf))
		if !in.BindSet || in.BindUID != want {
			t.Errorf("conf %q: awk=%q go=%q", conf, in.BindUID, want)
		}
		if strings.Contains(strings.ToLower(string(out)), "bindpw") {
			t.Errorf("출력에 bindpw: %q", out)
		}
	}
}

// shareLeft: 공유경로 아래 남은 항목 수
func (e lbEnv) shareLeft(t *testing.T) int {
	t.Helper()
	es, err := os.ReadDir(e.share)
	if err != nil {
		t.Fatal(err)
	}
	return len(es)
}

// stubCmds: 가짜 gossh 호출 기록에서 원격 명령(echo ... | bash 형태)만 뽑는다
func stubCmds(t *testing.T, logFile string) []string {
	t.Helper()
	var cmds []string
	for _, l := range strings.Split(mustRead(t, logFile), "\n") {
		if i := strings.Index(l, " echo "); strings.HasPrefix(l, "gossh ") && i >= 0 {
			cmds = append(cmds, l[i+1:])
		}
	}
	return cmds
}

func newFiles(uid, pw string) map[string]string {
	return map[string]string{
		"etc/openldap/ldap.conf": lbConfU(uid, pw), "etc/resolv.conf": "nameserver 5.5.5.5\n",
		"etc/sssd/sssd.conf": "new\n", "etc/chrony.conf": "server new\n"}
}

func TestApplySame(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	// 새 OS: binddn uid 가 같으면 bindpw·다른 파일이 달라도 건드리지 않음
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newFiles("svc_old", "totally-other-pw"))
	st := NewRealLdapBackup().Apply(j, "h1")
	if st.Bindpw != BindpwSame || st.Applied || st.Backup != LdapBackupOK || st.Reason != "" {
		t.Fatalf("%+v", st)
	}
	if e.read(t, "h1", "etc/resolv.conf") != "nameserver 5.5.5.5\n" {
		t.Fatal("same 인데 파일이 바뀜")
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "h1/restart.log")); err == nil {
		t.Fatal("same 인데 재시작")
	}
	if n := e.shareLeft(t); n != 0 {
		t.Fatalf("same 인데 공유경로 사용: %d", n)
	}
}

func TestApplyDiffRestoresViaShare(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newFiles("svc_new", "otherpw"))
	os.Chmod(filepath.Join(e.hosts, "h1/etc/resolv.conf"), 0600)
	st := NewRealLdapBackup().Apply(j, "h1")
	if st.Bindpw != BindpwDiff || !st.Applied || st.Backup != LdapBackupOK || st.Reason != "" {
		t.Fatalf("%+v", st)
	}
	for p, c := range lbOS8Old {
		if got := e.read(t, "h1", p); got != c {
			t.Fatalf("%s = %q", p, got)
		}
	}
	// 원 mode 복원(백업 때 640)
	if fi, _ := os.Stat(filepath.Join(e.hosts, "h1/etc/resolv.conf")); fi.Mode().Perm() != 0640 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	// 재시작: sssd -> chronyd, restorecon 은 파일마다 (순서: resolv, ldap, sssd, chrony)
	lg, _ := os.ReadFile(filepath.Join(e.hosts, "h1/restart.log"))
	want := "restorecon /etc/resolv.conf\nrestorecon /etc/openldap/ldap.conf\nrestorecon /etc/sssd/sssd.conf\nrestorecon /etc/chrony.conf\nrestart sssd\nrestart chronyd\n"
	if string(lg) != want {
		t.Fatalf("restart.log:\n%s", lg)
	}
	// 공유경로: 대상이 명령을 실행하는 시점에 디렉터리 0700·파일 0600 이었고, 끝난 뒤에는 흔적이 없다
	stat := mustRead(t, e.shareStat)
	var dirs, files int
	for _, l := range strings.Split(strings.TrimSpace(stat), "\n") {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		switch n := strings.Count(f[1], "/"); n {
		case 0, 1: // .as_ldap_<hex> , .as_ldap_<hex>/h1
			if !strings.HasPrefix(f[1], ".as_ldap_") || (n == 1 && !strings.HasSuffix(f[1], "/h1")) || f[0] != "700" {
				t.Fatalf("디렉터리 %s %s", f[0], f[1])
			}
			dirs++
		default:
			if f[0] != "600" {
				t.Fatalf("파일 권한 %s %s", f[0], f[1])
			}
			files++
		}
	}
	if dirs != 2 || files != 4 {
		t.Fatalf("공유경로 관찰: dirs=%d files=%d\n%s", dirs, files, stat)
	}
	if n := e.shareLeft(t); n != 0 {
		t.Fatalf("공유 임시 디렉터리가 남음: %d", n)
	}
}

func TestApplyNAWhenBinddnUnknown(t *testing.T) {
	e := lbSetup(t)
	// 새 OS 에 binddn uid 가 없으면 na (복원하지 않음)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", map[string]string{
		"etc/openldap/ldap.conf": "URI ldap://new\nBINDDN cn=admin,dc=x\n", "etc/sssd/sssd.conf": "new\n", "etc/chrony.conf": "x\n"})
	st := NewRealLdapBackup().Apply(j, "h1")
	if st.Bindpw != BindpwNA || st.Applied || st.Backup != LdapBackupOK || st.Reason != "binddn 확인불가" {
		t.Fatalf("새 OS binddn 없음 → na: %+v", st)
	}
	if e.read(t, "h1", "etc/chrony.conf") != "x\n" || e.shareLeft(t) != 0 {
		t.Fatal("na 인데 파일/공유경로 변경")
	}
	// 백업 쪽에 binddn uid 가 없으면 na
	j2 := lbBackup(t, e, "j2", "h2", "Rocky Linux release 8.9 (x)", map[string]string{
		"etc/openldap/ldap.conf": "URI x\nBINDPW pw\n", "etc/sssd/sssd.conf": "s\n", "etc/chrony.conf": "c\n"})
	e.mkHost(t, "h2", "Rocky Linux release 8.9 (x)", map[string]string{"etc/openldap/ldap.conf": lbConf("zz")})
	st = NewRealLdapBackup().Apply(j2, "h2")
	if st.Bindpw != BindpwNA || st.Applied || st.Backup != LdapBackupOK || st.Reason != "binddn 확인불가" {
		t.Fatalf("%+v", st)
	}
	if e.read(t, "h2", "etc/openldap/ldap.conf") != lbConf("zz") {
		t.Fatal("na 인데 파일이 바뀜")
	}
}

func TestApplyManualWhenShareDirEmpty(t *testing.T) {
	e := lbSetup(t)
	ldap_share_dir = ""
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	cur := newFiles("svc_new", "otherpw")
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", cur)
	st := NewRealLdapBackup().Apply(j, "h1")
	dir, _ := ldapHostDir("j", "h1")
	if st.Bindpw != BindpwDiff || st.Applied || st.Backup != LdapBackupOK ||
		st.Reason != "수동 복원 필요(ldap_share_dir 미설정, 백업: "+dir+")" {
		t.Fatalf("%+v", st)
	}
	for p, c := range cur {
		if e.read(t, "h1", p) != c {
			t.Fatalf("수동 복원인데 %s 가 바뀜", p)
		}
	}
	// 복원 호출이 없다: probe 1회뿐
	if n := strings.Count(mustRead(t, e.log), "gossh "); n != 2 { // 백업 수집 1 + probe 1
		t.Fatalf("gossh 호출 %d회", n)
	}
	// 같은 uid 면 변수가 비어도 same
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newFiles("svc_old", "x"))
	if st = NewRealLdapBackup().Apply(j, "h1"); st.Bindpw != BindpwSame || st.Reason != "" {
		t.Fatalf("%+v", st)
	}
}

func TestApplyShareNotVisibleToTarget(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	cur := newFiles("svc_new", "otherpw")
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", cur)
	t.Setenv("STUB_HIDE_SHARE", "1") // 대상이 공유경로를 못 보는 상황
	st := NewRealLdapBackup().Apply(j, "h1")
	if st.Applied || st.Bindpw != BindpwDiff || st.Reason != "공유경로 접근 실패(수동 확인)" {
		t.Fatalf("%+v", st)
	}
	for p, c := range cur {
		if e.read(t, "h1", p) != c {
			t.Fatalf("접근 실패인데 %s 가 바뀜", p)
		}
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "h1/restart.log")); err == nil {
		t.Fatal("접근 실패인데 재시작")
	}
	if n := e.shareLeft(t); n != 0 {
		t.Fatalf("공유 임시 디렉터리가 남음: %d", n)
	}
}

func TestApplyShareDirUnwritable(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	cur := newFiles("svc_new", "otherpw")
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", cur)
	ldap_share_dir = filepath.Join(e.share, "does-not-exist")
	st := NewRealLdapBackup().Apply(j, "h1")
	if st.Applied || st.Reason != "공유경로 쓰기 실패(수동 확인)" {
		t.Fatalf("%+v", st)
	}
	ldap_share_dir = "relative/dir"
	if st = NewRealLdapBackup().Apply(j, "h1"); st.Applied || st.Reason != "ldap_share_dir 경로 오류(수동 확인)" {
		t.Fatalf("%+v", st)
	}
	for p, c := range cur {
		if e.read(t, "h1", p) != c {
			t.Fatalf("%s 가 바뀜", p)
		}
	}
}

func TestApplyOS7Files(t *testing.T) {
	e := lbSetup(t)
	old := map[string]string{"etc/openldap/ldap.conf": lbConf(lbSecret), "etc/nslcd.conf": "uri a\nbindpw " + lbSecret + "\n", "etc/ntp.conf": "server o\n"}
	j := lbBackup(t, e, "j", "h7", "CentOS release 6.10 (Final)", old)
	e.mkHost(t, "h7", "CentOS Linux release 7.9 (Core)", map[string]string{"etc/openldap/ldap.conf": lbConfU("other", "o"), "etc/nslcd.conf": "n\n", "etc/ntp.conf": "n\n"})
	st := NewRealLdapBackup().Apply(j, "h7")
	if !st.Applied || st.Bindpw != BindpwDiff {
		t.Fatalf("%+v", st)
	}
	lg, _ := os.ReadFile(filepath.Join(e.hosts, "h7/restart.log"))
	if !strings.HasSuffix(string(lg), "restart nslcd\nrestart ntpd\n") {
		t.Fatalf("restart.log:\n%s", lg)
	}
}

func TestApplyOSBranchMismatch(t *testing.T) {
	e := lbSetup(t)
	old := map[string]string{"etc/openldap/ldap.conf": lbConf(lbSecret), "etc/nslcd.conf": "uri a\n", "etc/ntp.conf": "server o\n"}
	j := lbBackup(t, e, "j", "h1", "CentOS Linux release 7.9 (Core)", old)
	newF := map[string]string{"etc/openldap/ldap.conf": lbConfU("other", "o"), "etc/sssd/sssd.conf": "n\n", "etc/chrony.conf": "n\n"}
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newF)
	st := NewRealLdapBackup().Apply(j, "h1")
	if st.Applied || st.Reason != "OS 분기 불일치(수동 확인)" || st.Bindpw != BindpwDiff {
		t.Fatalf("%+v", st)
	}
	for p, c := range newF {
		if e.read(t, "h1", p) != c {
			t.Fatalf("불일치인데 %s 가 바뀜", p)
		}
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "h1/etc/nslcd.conf")); err == nil {
		t.Fatal("nslcd.conf 를 만들면 안 됨")
	}
}

func TestApplyNoResponseAndNoBackup(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	os.RemoveAll(filepath.Join(e.hosts, "h1")) // 새 OS 무응답
	st := NewRealLdapBackup().Apply(j, "h1")
	if st.Applied || st.Bindpw != BindpwDiff || !strings.Contains(st.Reason, "무응답") {
		t.Fatalf("%+v", st)
	}
	st = NewRealLdapBackup().Apply(lbJob("nojob", "zz"), "zz")
	if st.Backup != LdapBackupNone {
		t.Fatalf("%+v", st)
	}
	// 이름 검증: 경로 탈출 시도
	if st = NewRealLdapBackup().Apply(lbJob("j", "../x"), "../x"); st.Backup != LdapBackupNone {
		t.Fatalf("%+v", st)
	}
}

func TestApplyPartialFailureReason(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	// 새 OS 에 etc/sssd 디렉터리가 없어 sssd.conf 쓰기 실패 (chrony/resolv/ldap 은 성공해야 함)
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", map[string]string{"etc/openldap/ldap.conf": lbConfU("other", "o")})
	st := NewRealLdapBackup().Apply(j, "h1")
	if st.Applied || st.Reason != "복원 실패: sssd.conf" {
		t.Fatalf("%+v", st)
	}
	if e.read(t, "h1", "etc/chrony.conf") != lbOS8Old["etc/chrony.conf"] {
		t.Fatal("나머지 파일은 진행되어야 함")
	}
	if n := e.shareLeft(t); n != 0 {
		t.Fatalf("실패해도 공유 임시 디렉터리는 삭제: %d", n)
	}
}

// decodeCmd: buildRemoteCmd 결과를 풀어 내부 스크립트 텍스트를 돌려준다
func decodeCmd(t *testing.T, cmd string) string {
	t.Helper()
	end := strings.Index(cmd, " | tr -d . | base64 -d | bash")
	if !strings.HasPrefix(cmd, "echo ") || end < 0 {
		t.Fatalf("형식: %.60s", cmd)
	}
	outer, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(cmd[5:end], ".", ""))
	if err != nil {
		t.Fatal(err)
	}
	in := string(outer)
	i := strings.Index(in, "echo ")
	j := strings.Index(in, " | base64 -d")
	sc, err := base64.StdEncoding.DecodeString(in[i+5 : j])
	if err != nil {
		t.Fatal(err)
	}
	return string(sc)
}

func TestRestoreCommandShape(t *testing.T) {
	es := []lbEntry{
		{Name: "resolv.conf", Mode: "644", Owner: "root", Group: "root", Data: []byte("nameserver 1\n")},
		{Name: "ldap.conf", Mode: "644", Owner: "root", Group: "root", Data: []byte(lbConf(lbSecret))},
		{Name: "sssd.conf", Mode: "600", Owner: "root", Group: "root", Data: []byte("s")},
		{Name: "chrony.conf", Mode: "644", Owner: "root", Group: "root", Data: []byte("c")},
	}
	const share = "/share/.as_ldap_abcd/h1"
	cmd := buildRemoteCmd(restoreScript(share, es))
	if strings.Contains(cmd, lbSecret) || strings.Contains(cmd, "'") || strings.Contains(cmd, "\n") {
		t.Fatal("명령줄에 평문/따옴표/개행")
	}
	// 위험어가 우연히 들어가지 않음 (3글자 이상 이어진 조각이 없음)
	b64 := strings.TrimPrefix(strings.SplitN(cmd, " | ", 2)[0], "echo ")
	for _, p := range strings.Split(b64, ".") {
		if len(p) > 2 {
			t.Fatalf("조각 길이 %d: %q", len(p), p)
		}
	}
	sc := decodeCmd(t, cmd)
	if strings.Contains(sc, lbSecret) {
		t.Fatal("스크립트에 평문 bindpw")
	}
	for _, e := range es {
		if strings.Contains(sc, base64.StdEncoding.EncodeToString(e.Data)) {
			t.Fatalf("파일 내용(base64)이 스크립트에 있으면 안 됨: %s", e.Name)
		}
	}
	// 경로·mode·owner·group 만: 파일마다 공유경로 원본과 대상 경로가 있다
	for _, want := range []string{
		"put resolv.conf '/etc/resolv.conf' 644 root root '" + share + "/resolv.conf'",
		"put ldap.conf '/etc/openldap/ldap.conf' 644 root root '" + share + "/ldap.conf'",
		"put sssd.conf '/etc/sssd/sssd.conf' 600 root root '" + share + "/sssd.conf' && A=1",
		"put chrony.conf '/etc/chrony.conf' 644 root root '" + share + "/chrony.conf' && T=1",
		`[ -r "$s" ] || { echo "==SHARE err"; exit 3; }`, `cat "$6" > "$t"`,
	} {
		if !strings.Contains(sc, want) {
			t.Fatalf("스크립트에 %q 없음:\n%s", want, sc)
		}
	}
	for _, bad := range []string{"base64 -d >", "printf '%s' \"$6\"", "BINDPW", "bindpw", "sha256"} {
		if strings.Contains(sc, bad) {
			t.Fatalf("스크립트에 %q 가 있으면 안 됨", bad)
		}
	}
	order := []string{"put resolv.conf ", "put ldap.conf ", "put sssd.conf ", "put chrony.conf ", `svc "$AK"`, "svc ntpd", "svc chronyd", "==BINDDN"}
	prev := -1
	for _, s := range order {
		i := strings.Index(sc[prev+1:], s)
		if i < 0 {
			t.Fatalf("%q 위치/순서 오류:\n%s", s, sc)
		}
		prev += i + 1
	}
}

func TestNoSecretAnywhere(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newFiles("svc_new_uid", "other-bindpw-"+lbSecret+"-new"))
	st := NewRealLdapBackup().Apply(j, "h1")
	if !st.Applied {
		t.Fatalf("%+v", st)
	}
	// 실패 경로(공유경로 접근 실패·무응답·수동 복원)도 포함
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newFiles("svc_new_uid", "o"))
	t.Setenv("STUB_HIDE_SHARE", "1")
	st2 := NewRealLdapBackup().Apply(j, "h1")
	os.RemoveAll(filepath.Join(e.hosts, "h1"))
	st3 := NewRealLdapBackup().Apply(j, "h1")
	ldap_share_dir = ""
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newFiles("svc_new_uid", "o"))
	st4 := NewRealLdapBackup().Apply(j, "h1")

	var all strings.Builder
	for _, s := range []LdapState{st, st2, st3, st4} {
		b, _ := json.Marshal(s)
		all.Write(b)
	}
	all.WriteString(mustRead(t, e.log))
	if b, err := os.ReadFile(logPath()); err == nil {
		all.Write(b)
	}
	// 비밀(bindpw)과 uid 값은 상태·명령줄·로그에 남지 않는다
	for _, secret := range []string{lbSecret, "other-bindpw", "svc_old", "svc_new_uid"} {
		if strings.Contains(all.String(), secret) {
			t.Fatalf("상태/명령줄/로그에 %q", secret)
		}
	}
	// 실제로 실행된 모든 원격 명령: 디코드한 스크립트에도 비밀·파일 내용이 없다
	cmds := stubCmds(t, e.log)
	if len(cmds) < 4 {
		t.Fatalf("원격 명령 기록 %d개", len(cmds))
	}
	for _, c := range cmds {
		sc := decodeCmd(t, c)
		for _, secret := range []string{lbSecret, "BINDPW", "bindpw", "ldap_default_authtok", base64.StdEncoding.EncodeToString([]byte(lbConf(lbSecret)))} {
			if strings.Contains(c, secret) || strings.Contains(sc, secret) {
				t.Fatalf("원격 명령에 %q:\n%s", secret, sc)
			}
		}
	}
	if e.shareLeft(t) != 0 {
		t.Fatal("공유경로 정리 안 됨")
	}
}

func TestParseFileLineRejects(t *testing.T) {
	for _, s := range []string{"/etc/passwd 644 root root", "/etc/resolv.conf 9999 root root", "/etc/resolv.conf 644 ro;ot root", "/etc/resolv.conf 644 root root !!!notb64"} {
		if _, ok := parseFileLine(s); ok {
			t.Fatalf("거부해야 함: %q", s)
		}
	}
	if e, ok := parseFileLine("/etc/resolv.conf 644 root root"); !ok || len(e.Data) != 0 {
		t.Fatal("빈 파일 허용")
	}
}

func TestInitWiresNewLdap(t *testing.T) {
	if _, ok := newLdap().(realLdapBackup); !ok {
		t.Fatalf("newLdap = %T", newLdap())
	}
}
