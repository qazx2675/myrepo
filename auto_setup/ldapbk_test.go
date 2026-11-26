// ldapbk_test.go - ldapbk.go 단위 테스트: 가짜 gossh/ssh 스텁(PATH 주입, 호스트별 가짜 루트)으로 수집·저장·비교·복원·평문 미노출
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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
  case " $STUB_SSHFAIL " in *" $h "*) echo "$h: SSH 접속 실패: connection refused"; continue;; esac
  [ -d "$STUB_HOSTS/$h" ] || continue
  case " $STUB_NOLOCAL " in *" $h "*) [ -n "$AS_VIA" ] || continue;; esac
  ln -sfn "$STUB_HOSTS/$h" "$STUB_FS"
  AS_HOST=$h bash -c "$cmd" 2>/dev/null | sed "s/^/$h: /"
done < "$wf"
`), 0755)
	os.WriteFile(filepath.Join(bin, "hostname"), []byte("#!/bin/sh\necho \"$AS_HOST\"\n"), 0755)
	os.WriteFile(filepath.Join(bin, "ssh"), []byte(`#!/bin/bash
echo "ssh $1" >> "$STUB_LOG"
shift
AS_VIA=os6 exec bash -c "$1"
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
		"etc/sssd/sssd.conf": "[domain/x]\n", "etc/chrony.conf": "server stale\n",
		"etc/autofs.conf": "[ autofs ]\n", "etc/autofs_ldap_auth.conf": "secret=\"" + lbSecret + "\"\n"})
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
		"n7":    {"autofs.conf", "autofs_ldap_auth.conf", "ldap.conf", "meta.json", "nslcd.conf", "ntp.conf", "resolv.conf"},
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
	"etc/sssd/sssd.conf": "[domain/x]\nldap_default_authtok = " + lbSecret + "\n", "etc/chrony.conf": "server old\n",
	"etc/autofs.conf": "[ autofs ]\nbrowse_mode = no\n", "etc/autofs_ldap_auth.conf": "<autofs_ldap_sasl_conf usertype=\"user\" user=\"svc_old\" secret=\"" + lbSecret + "\" />\n"}

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
	st := NewRealLdapBackup().Restore(j, "h1")
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
	st := NewRealLdapBackup().Restore(j, "h1")
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
	// 재시작: sssd -> chronyd, restorecon 은 파일마다 (순서: resolv, ldap, autofs, autofs_ldap_auth, sssd, chrony)
	lg, _ := os.ReadFile(filepath.Join(e.hosts, "h1/restart.log"))
	want := "restorecon /etc/resolv.conf\nrestorecon /etc/openldap/ldap.conf\nrestorecon /etc/autofs.conf\nrestorecon /etc/autofs_ldap_auth.conf\nrestorecon /etc/sssd/sssd.conf\nrestorecon /etc/chrony.conf\nrestart sssd\nrestart autofs\nrestart chronyd\nrestart nscd\n"
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
	if dirs != 2 || files != 6 {
		t.Fatalf("공유경로 관찰: dirs=%d files=%d\n%s", dirs, files, stat)
	}
	if n := e.shareLeft(t); n != 0 {
		t.Fatalf("공유 임시 디렉터리가 남음: %d", n)
	}
}

// 기본(ldap_auto_restore 꺼짐): READY 직후 데몬은 비교만(Probe) — 복원 대기로 두고 대상 파일·공유경로·서비스는 건드리지 않는다.
// 복원은 request ldap-restore(ldapRestoreReq) 로, ldap_auto_restore=y 면 READY 직후 바로 복원.
func TestProbeComparesOnlyThenRestoreByRequest(t *testing.T) {
	e := lbSetup(t)
	old := ldap_auto_restore
	t.Cleanup(func() { ldap_auto_restore = old })
	ldap_auto_restore = ""
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	newF := newFiles("svc_new", "otherpw")
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newF)
	j.Hosts["h1"].Ldap = &LdapState{Backup: LdapBackupOK}
	j.Hosts["h1"].ReadyAt = base + 600
	d := &Daemon{Ldap: NewRealLdapBackup()}
	d.init()
	d.jobs[j.ID] = j
	d.ldapApplyNow(j, "h1", base+600)
	if st := j.Hosts["h1"].Ldap; !reflect.DeepEqual(st, &LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Reason: ldapPendingPrefix}) {
		t.Fatalf("비교만: %+v", st)
	}
	for p, c := range newF {
		if e.read(t, "h1", p) != c {
			t.Fatalf("비교만인데 %s 가 바뀜", p)
		}
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "h1/restart.log")); err == nil {
		t.Fatal("비교만인데 재시작")
	}
	if b, _ := os.ReadFile(e.shareStat); strings.Contains(string(b), ".as_ldap_") || e.shareLeft(t) != 0 {
		t.Fatalf("비교만인데 공유경로 사용:\n%s", b)
	}
	if lg := mustRead(t, logPath()); !strings.Contains(lg, "auto_setup request ldap-restore j h1") {
		t.Fatalf("복원 명령 안내 로그 없음:\n%s", lg)
	}
	// 이미 비교한 호스트는 READY 를 다시 처리해도 다시 비교하지 않는다 (복원 대기 유지)
	d.ldapApplyNow(j, "h1", base+700)
	if !ldapPending(j.Hosts["h1"].Ldap) {
		t.Fatalf("%+v", j.Hosts["h1"].Ldap)
	}

	// request ldap-restore → 비교 후 복원
	if r := d.ldapRestoreReq(j.ID, "h1"); r != "" {
		t.Fatal(r)
	}
	if st := j.Hosts["h1"].Ldap; !st.Applied || st.Bindpw != BindpwDiff || st.Reason != "" {
		t.Fatalf("복원: %+v", st)
	}
	for p, c := range lbOS8Old {
		if got := e.read(t, "h1", p); got != c {
			t.Fatalf("%s 복원 안 됨", p)
		}
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "h1/restart.log")); err != nil {
		t.Fatal("복원했는데 재시작 기록 없음")
	}
	// 같은 호스트를 다시 지정하면 다시 비교 → 이제 same
	if r := d.ldapRestoreReq(j.ID, "h1"); r != "" || j.Hosts["h1"].Ldap.Bindpw != BindpwSame {
		t.Fatalf("재요청: %q %+v", r, j.Hosts["h1"].Ldap)
	}

	// ldap_auto_restore=y: READY 직후 바로 복원
	ldap_auto_restore = "y"
	j2 := lbBackup(t, e, "j2", "h2", "Rocky Linux release 8.9 (x)", lbOS8Old)
	e.mkHost(t, "h2", "Rocky Linux release 8.9 (x)", newFiles("svc_new", "otherpw"))
	j2.Hosts["h2"].Ldap = &LdapState{Backup: LdapBackupOK}
	d.jobs[j2.ID] = j2
	d.ldapApplyNow(j2, "h2", base+600)
	if st := j2.Hosts["h2"].Ldap; !st.Applied || st.Bindpw != BindpwDiff {
		t.Fatalf("자동 복원: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "h2/restart.log")); err != nil {
		t.Fatal("자동 복원인데 재시작 기록 없음")
	}
}

// Probe: 7→8·8→7 모두 복원 대기에 변환 예정 내역
func TestProbeReasons(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "CentOS Linux release 7.9 (Core)", lbOS7Old(lbNslcd7, lbNtp7))
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", map[string]string{
		"etc/openldap/ldap.conf": lbConfU("svc_old", "newpw"), "etc/sssd/sssd.conf": "n\n", "etc/chrony.conf": "n\n"})
	st := NewRealLdapBackup().Probe(j, "h1")
	if !ldapPending(&st) || st.Reason != ldapPendingPrefix+": OS 종류 변경; 변환 nslcd.conf→sssd.conf,ntp.conf→chrony.conf" {
		t.Fatalf("%s", lbMask(fmt.Sprintf("%+v", st)))
	}
	if e.read(t, "h1", "etc/sssd/sssd.conf") != "n\n" {
		t.Fatal("Probe 가 파일을 씀")
	}
	j = lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Conv(lbSSSD8, lbChrony8))
	e.mkHost(t, "h1", "CentOS Linux release 7.9 (Core)", map[string]string{"etc/openldap/ldap.conf": lbConfU("svc_old", "o"), "etc/nslcd.conf": "n\n"})
	if st := NewRealLdapBackup().Probe(j, "h1"); !ldapPending(&st) || st.Reason != ldapPendingPrefix+": OS 종류 변경; 변환 sssd.conf→nslcd.conf,chrony.conf→ntp.conf" {
		t.Fatalf("8→7: %s", lbMask(fmt.Sprintf("%+v", st)))
	}
	if e.read(t, "h1", "etc/nslcd.conf") != "n\n" {
		t.Fatal("Probe 가 파일을 씀(8→7)")
	}
	// ldap_share_dir 가 없으면 명령으로도 복원할 수 없으므로 종전처럼 수동 복원 필요
	j = lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newFiles("svc_new", "o"))
	ldap_share_dir = ""
	if st := NewRealLdapBackup().Probe(j, "h1"); ldapPending(&st) || !strings.HasPrefix(st.Reason, ldapManualPrefix) {
		t.Fatalf("share 없음: %+v", st)
	}
}

func TestApplyRestoresWhenNewBinddnMissing(t *testing.T) {
	e := lbSetup(t)
	// 새 OS ldap.conf 에 binddn uid 가 없으면(uid= 없는 binddn·binddn 줄 없음) 복원한다
	for i, conf := range []string{"URI ldap://new\nBINDDN cn=admin,dc=x\n", "URI ldap://new\n"} {
		j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
		e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", map[string]string{
			"etc/openldap/ldap.conf": conf, "etc/sssd/sssd.conf": "new\n", "etc/chrony.conf": "x\n"})
		st := NewRealLdapBackup().Restore(j, "h1")
		if st.Bindpw != BindpwDiff || !st.Applied || st.Backup != LdapBackupOK || st.Reason != "새 OS binddn 없음" {
			t.Fatalf("%d: 새 OS binddn 없음 → 복원: %+v", i, st)
		}
		for p, c := range lbOS8Old {
			if e.read(t, "h1", p) != c {
				t.Fatalf("%d: %s 가 복원되지 않음", i, p)
			}
		}
		if e.shareLeft(t) != 0 {
			t.Fatal("공유 임시 디렉터리가 남음")
		}
	}
}

func TestApplyNAWhenBinddnUnknown(t *testing.T) {
	e := lbSetup(t)
	// 백업 쪽에 binddn uid 가 없으면 na
	j2 := lbBackup(t, e, "j2", "h2", "Rocky Linux release 8.9 (x)", map[string]string{
		"etc/openldap/ldap.conf": "URI x\nBINDPW pw\n", "etc/sssd/sssd.conf": "s\n", "etc/chrony.conf": "c\n"})
	e.mkHost(t, "h2", "Rocky Linux release 8.9 (x)", map[string]string{"etc/openldap/ldap.conf": lbConf("zz")})
	st := NewRealLdapBackup().Restore(j2, "h2")
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
	st := NewRealLdapBackup().Restore(j, "h1")
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
	if st = NewRealLdapBackup().Restore(j, "h1"); st.Bindpw != BindpwSame || st.Reason != "" {
		t.Fatalf("%+v", st)
	}
}

func TestApplyShareNotVisibleToTarget(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	cur := newFiles("svc_new", "otherpw")
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", cur)
	t.Setenv("STUB_HIDE_SHARE", "1") // 대상이 공유경로를 못 보는 상황
	st := NewRealLdapBackup().Restore(j, "h1")
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
	st := NewRealLdapBackup().Restore(j, "h1")
	if st.Applied || st.Reason != "공유경로 쓰기 실패(수동 확인)" {
		t.Fatalf("%+v", st)
	}
	ldap_share_dir = "relative/dir"
	if st = NewRealLdapBackup().Restore(j, "h1"); st.Applied || st.Reason != "ldap_share_dir 경로 오류(수동 확인)" {
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
	st := NewRealLdapBackup().Restore(j, "h7")
	if !st.Applied || st.Bindpw != BindpwDiff {
		t.Fatalf("%+v", st)
	}
	lg, _ := os.ReadFile(filepath.Join(e.hosts, "h7/restart.log"))
	if !strings.HasSuffix(string(lg), "restart nslcd\nrestart autofs\nrestart ntpd\nrestart nscd\n") {
		t.Fatalf("restart.log:\n%s", lg)
	}
}

// lbOS7Old: OS7 백업(nslcd/ntp) — 7→8 변환 시나리오용
func lbOS7Old(nslcd, ntp string) map[string]string {
	return map[string]string{"etc/openldap/ldap.conf": lbConf(lbSecret), "etc/resolv.conf": "nameserver 7.7.7.7\n",
		"etc/nslcd.conf": nslcd, "etc/ntp.conf": ntp}
}

const lbNslcd7 = "# nslcd\nuri ldap://l1 ldap://l2\nbase dc=x\nbinddn uid=svc_old,ou=user,dc=x\nbindpw " + lbSecret + "\nssl start_tls\ntls_reqcert never\nmap passwd homeDirectory /h\n"
const lbNtp7 = "driftfile /var/lib/ntp/drift\nrestrict default nomodify\nserver n1 iburst\nserver n2\n"

func lbMask(s string) string { return strings.ReplaceAll(s, lbSecret, "<bindpw>") }

func TestApplyOS7to8Converts(t *testing.T) {
	e := lbSetup(t)
	wantSSSD, err := nslcdToSSSD([]byte(lbNslcd7))
	if err != nil {
		t.Fatal(err)
	}
	wantChrony := "# generated by auto_setup from backup ntp.conf (RHEL7 -> RHEL8)\nserver n1 iburst\nserver n2\n" +
		"driftfile /var/lib/chrony/drift\nmakestep 1.0 3\nrtcsync\nlogdir /var/log/chrony\n"
	// uid 가 같아도(OS 종류 변경) / 달라도 변환 복원
	for _, c := range []struct{ uid, reason string }{
		{"svc_old", "OS 종류 변경; 변환 nslcd.conf→sssd.conf,ntp.conf→chrony.conf"},
		{"other", "변환 nslcd.conf→sssd.conf,ntp.conf→chrony.conf"},
	} {
		j := lbBackup(t, e, "j", "h1", "CentOS Linux release 7.9 (Core)", lbOS7Old(lbNslcd7, lbNtp7))
		e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", map[string]string{
			"etc/openldap/ldap.conf": lbConfU(c.uid, "newpw"), "etc/sssd/sssd.conf": "n\n", "etc/chrony.conf": "n\n"})
		st := NewRealLdapBackup().Restore(j, "h1")
		if st.Bindpw != BindpwDiff || !st.Applied || st.Backup != LdapBackupOK || st.Reason != c.reason {
			t.Fatalf("%s: %s", c.uid, lbMask(fmt.Sprintf("%+v", st)))
		}
		if e.read(t, "h1", "etc/sssd/sssd.conf") != string(wantSSSD) {
			t.Fatalf("%s: sssd.conf 내용이 변환 결과와 다름:\n%s", c.uid, lbMask(e.read(t, "h1", "etc/sssd/sssd.conf")))
		}
		if got := e.read(t, "h1", "etc/chrony.conf"); got != wantChrony {
			t.Fatalf("%s: chrony.conf:\n%s", c.uid, got)
		}
		if e.read(t, "h1", "etc/openldap/ldap.conf") != lbConf(lbSecret) || e.read(t, "h1", "etc/resolv.conf") != "nameserver 7.7.7.7\n" {
			t.Fatalf("%s: 공통 파일이 복원되지 않음", c.uid)
		}
		for p, m := range map[string]os.FileMode{"etc/sssd/sssd.conf": 0600, "etc/chrony.conf": 0644} {
			if fi, err := os.Stat(filepath.Join(e.hosts, "h1", p)); err != nil || fi.Mode().Perm() != m {
				t.Fatalf("%s: %s mode %v (want %v)", c.uid, p, fi.Mode().Perm(), m)
			}
		}
		for _, p := range []string{"etc/nslcd.conf", "etc/ntp.conf"} {
			if _, err := os.Stat(filepath.Join(e.hosts, "h1", p)); err == nil {
				t.Fatalf("%s: 새 OS 에 %s 를 쓰면 안 됨", c.uid, p)
			}
		}
		lg, _ := os.ReadFile(filepath.Join(e.hosts, "h1/restart.log"))
		want := "restorecon /etc/resolv.conf\nrestorecon /etc/openldap/ldap.conf\nrestorecon /etc/sssd/sssd.conf\nrestorecon /etc/chrony.conf\nrestart sssd\nrestart autofs\nrestart chronyd\nrestart nscd\n"
		if string(lg) != want {
			t.Fatalf("%s: restart.log:\n%s", c.uid, lg)
		}
		if e.shareLeft(t) != 0 {
			t.Fatal("공유 임시 디렉터리가 남음")
		}
		// bindpw 는 생성 파일에만: 상태·명령 기록·로그에는 없다
		b, _ := json.Marshal(st)
		lgAll := string(b) + mustRead(t, e.log)
		if d, err := os.ReadFile(logPath()); err == nil {
			lgAll += string(d)
		}
		if strings.Contains(lgAll, lbSecret) {
			t.Fatal("상태/명령 기록/로그에 bindpw")
		}
	}
}

func TestApplyOS7to8S4KeepsNslcd(t *testing.T) {
	e := lbSetup(t)
	// s4 호스트(OS8 이지만 nslcd 사용): nslcd.conf 는 그대로, ntp.conf 만 chrony.conf 로 변환
	t.Setenv("AUTO_SETUP_LDAP_S4_PREFIX", "s4")
	t.Setenv("AUTO_SETUP_LDAP_S4_SERVICES", "nslcd")
	j := lbBackup(t, e, "j", "s4h", "CentOS Linux release 7.9 (Core)", lbOS7Old(lbNslcd7, lbNtp7))
	e.mkHost(t, "s4h", "Rocky Linux release 8.9 (x)", map[string]string{
		"etc/openldap/ldap.conf": lbConfU("other", "o"), "etc/nslcd.conf": "n\n", "etc/chrony.conf": "n\n"})
	st := NewRealLdapBackup().Restore(j, "s4h")
	if !st.Applied || st.Reason != "변환 ntp.conf→chrony.conf" {
		t.Fatalf("%s", lbMask(fmt.Sprintf("%+v", st)))
	}
	if e.read(t, "s4h", "etc/nslcd.conf") != lbNslcd7 {
		t.Fatal("nslcd.conf 가 그대로 복원되지 않음")
	}
	if !strings.HasPrefix(e.read(t, "s4h", "etc/chrony.conf"), "# generated by auto_setup") {
		t.Fatal("chrony.conf 가 변환되지 않음")
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "s4h/etc/sssd/sssd.conf")); err == nil {
		t.Fatal("sssd.conf 를 만들면 안 됨")
	}
}

func TestApplyConvertFail(t *testing.T) {
	e := lbSetup(t)
	for _, c := range []struct{ nslcd, ntp, reason string }{
		{"uri ldap://a\nbinddn uid=svc_old,dc=x\nbindpw\n", lbNtp7, "변환 실패: sssd.conf(수동 확인)"},
		{lbNslcd7, "restrict default\nserver 127.127.1.0\n", "변환 실패: chrony.conf(수동 확인)"},
	} {
		j := lbBackup(t, e, "j", "h1", "CentOS Linux release 7.9 (Core)", lbOS7Old(c.nslcd, c.ntp))
		cur := map[string]string{"etc/openldap/ldap.conf": lbConfU("other", "o"), "etc/sssd/sssd.conf": "n\n", "etc/chrony.conf": "n\n"}
		e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", cur)
		st := NewRealLdapBackup().Restore(j, "h1")
		if st.Applied || st.Bindpw != BindpwDiff || st.Reason != c.reason {
			t.Fatalf("%s", lbMask(fmt.Sprintf("%+v", st)))
		}
		for p, v := range cur {
			if e.read(t, "h1", p) != v {
				t.Fatalf("변환 실패인데 %s 가 바뀜", p)
			}
		}
		if _, err := os.Stat(filepath.Join(e.hosts, "h1/restart.log")); err == nil {
			t.Fatal("변환 실패인데 재시작")
		}
		if e.shareLeft(t) != 0 {
			t.Fatal("공유경로 사용")
		}
	}
	// 8→7 방향: uri·binddn·bindpw 없는 sssd.conf, server/pool 없는 chrony.conf
	for _, c := range []struct{ sssd, chrony, reason string }{
		{"[domain/d]\nldap_uri = ldap://a\nldap_default_bind_dn = uid=svc_old,dc=x\nldap_default_authtok = " + lbSecret + "\nldap_default_authtok_type = obfuscated_password\n",
			lbChrony8, "변환 실패: nslcd.conf(수동 확인)"},
		{lbSSSD8, "refclock PHC /dev/ptp0\nmakestep 1.0 3\n", "변환 실패: ntp.conf(수동 확인)"},
	} {
		j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Conv(c.sssd, c.chrony))
		cur := map[string]string{"etc/openldap/ldap.conf": lbConfU("other", "o"), "etc/nslcd.conf": "n\n", "etc/ntp.conf": "n\n"}
		e.mkHost(t, "h1", "CentOS Linux release 7.9 (Core)", cur)
		st := NewRealLdapBackup().Restore(j, "h1")
		if st.Applied || st.Bindpw != BindpwDiff || st.Reason != c.reason {
			t.Fatalf("%s", lbMask(fmt.Sprintf("%+v", st)))
		}
		for p, v := range cur {
			if e.read(t, "h1", p) != v {
				t.Fatalf("8→7 변환 실패인데 %s 가 바뀜", p)
			}
		}
		if _, err := os.Stat(filepath.Join(e.hosts, "h1/restart.log")); err == nil {
			t.Fatal("8→7 변환 실패인데 재시작")
		}
		if e.shareLeft(t) != 0 {
			t.Fatal("공유경로 사용")
		}
	}
}

// lbSSSD8·lbChrony8: 변환 가능한 OS8 백업(sssd/chrony) — 8→7 변환 시나리오용
const lbSSSD8 = "[sssd]\nservices = nss, pam\ndomains = default\n\n[domain/default]\nid_provider = ldap\nldap_uri = ldap://l1, ldap://l2\n" +
	"ldap_search_base = dc=x\nldap_id_use_start_tls = True\nldap_tls_reqcert = never\nldap_default_bind_dn = uid=svc_old,ou=user,dc=x\n" +
	"ldap_default_authtok_type = password\nldap_default_authtok = " + lbSecret + "\n"
const lbChrony8 = "pool 2.rocky.pool.ntp.org iburst\nserver c1 iburst maxdelay 0.5\ndriftfile /var/lib/chrony/drift\nmakestep 1.0 3\n"

// lbOS8Conv: lbOS8Old 에서 sssd.conf·chrony.conf 만 바꾼 OS8 백업
func lbOS8Conv(sssd, chrony string) map[string]string {
	m := map[string]string{}
	for k, v := range lbOS8Old {
		m[k] = v
	}
	m["etc/sssd/sssd.conf"], m["etc/chrony.conf"] = sssd, chrony
	return m
}

func TestApplyOS8to7Converts(t *testing.T) {
	e := lbSetup(t)
	wantNslcd, err := sssdToNslcd([]byte(lbSSSD8))
	if err != nil {
		t.Fatal(err)
	}
	wantNtp := "# generated by auto_setup from backup chrony.conf (RHEL8 -> RHEL7)\ndriftfile /var/lib/ntp/drift\n" +
		"restrict default nomodify notrap nopeer noquery\nrestrict 127.0.0.1\nrestrict ::1\npool 2.rocky.pool.ntp.org iburst\nserver c1 iburst\n"
	// 8→7(sssd→nslcd, chrony→ntp)도 변환 복원 — uid 가 같아도(OS 종류 변경) / 달라도
	for _, c := range []struct{ uid, reason string }{
		{"svc_old", "OS 종류 변경; 변환 sssd.conf→nslcd.conf,chrony.conf→ntp.conf"},
		{"other", "변환 sssd.conf→nslcd.conf,chrony.conf→ntp.conf"},
	} {
		j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Conv(lbSSSD8, lbChrony8))
		e.mkHost(t, "h1", "CentOS Linux release 7.9 (Core)", map[string]string{
			"etc/openldap/ldap.conf": lbConfU(c.uid, "o"), "etc/nslcd.conf": "n\n", "etc/ntp.conf": "n\n"})
		st := NewRealLdapBackup().Restore(j, "h1")
		if st.Bindpw != BindpwDiff || !st.Applied || st.Backup != LdapBackupOK || st.Reason != c.reason {
			t.Fatalf("%s: %s", c.uid, lbMask(fmt.Sprintf("%+v", st)))
		}
		if n := hostNote(&Host{Route: "local", IP: "1.1.1.1", Ldap: &st}); !strings.Contains(n, "LDAP 복원(8→7 변환)") {
			t.Fatalf("%s: 비고 %q", c.uid, n)
		}
		if got := e.read(t, "h1", "etc/nslcd.conf"); got != string(wantNslcd) {
			t.Fatalf("%s: nslcd.conf 내용이 변환 결과와 다름:\n%s", c.uid, lbMask(got))
		}
		if got := e.read(t, "h1", "etc/ntp.conf"); got != wantNtp {
			t.Fatalf("%s: ntp.conf:\n%s", c.uid, got)
		}
		for _, p := range []string{"etc/openldap/ldap.conf", "etc/resolv.conf", "etc/autofs.conf", "etc/autofs_ldap_auth.conf"} {
			if e.read(t, "h1", p) != lbOS8Old[p] {
				t.Fatalf("%s: 공통 파일 %s 가 복원되지 않음", c.uid, p)
			}
		}
		for p, m := range map[string]os.FileMode{"etc/nslcd.conf": 0600, "etc/ntp.conf": 0644} {
			if fi, err := os.Stat(filepath.Join(e.hosts, "h1", p)); err != nil || fi.Mode().Perm() != m {
				t.Fatalf("%s: %s mode %v (want %v)", c.uid, p, fi.Mode().Perm(), m)
			}
		}
		for _, p := range []string{"etc/sssd/sssd.conf", "etc/chrony.conf"} {
			if _, err := os.Stat(filepath.Join(e.hosts, "h1", p)); err == nil {
				t.Fatalf("%s: OS7 에 %s 를 쓰면 안 됨", c.uid, p)
			}
		}
		lg, _ := os.ReadFile(filepath.Join(e.hosts, "h1/restart.log"))
		want := "restorecon /etc/resolv.conf\nrestorecon /etc/openldap/ldap.conf\nrestorecon /etc/autofs.conf\nrestorecon /etc/autofs_ldap_auth.conf\n" +
			"restorecon /etc/nslcd.conf\nrestorecon /etc/ntp.conf\nrestart nslcd\nrestart autofs\nrestart ntpd\nrestart nscd\n"
		if string(lg) != want {
			t.Fatalf("%s: restart.log:\n%s", c.uid, lg)
		}
		if e.shareLeft(t) != 0 {
			t.Fatal("공유 임시 디렉터리가 남음")
		}
		// bindpw 는 생성 파일에만: 상태·명령 기록·로그에는 없다
		b, _ := json.Marshal(st)
		lgAll := string(b) + mustRead(t, e.log)
		if d, err := os.ReadFile(logPath()); err == nil {
			lgAll += string(d)
		}
		if strings.Contains(lgAll, lbSecret) {
			t.Fatal("상태/명령 기록/로그에 bindpw")
		}
	}
}

func TestApplyOS8toS4ConvertsSSSD(t *testing.T) {
	e := lbSetup(t)
	// TestApplyOS7to8S4KeepsNslcd 의 대칭: OS8 백업(sssd/chrony) → s4 호스트(OS8 이지만 nslcd 사용)
	// sssd.conf 만 nslcd.conf 로 변환, chrony.conf 는 그대로
	j := lbBackup(t, e, "j", "s4h", "Rocky Linux release 8.9 (x)", lbOS8Conv(lbSSSD8, lbChrony8)) // 백업 때는 일반 OS8(sssd)
	t.Setenv("AUTO_SETUP_LDAP_S4_PREFIX", "s4")
	t.Setenv("AUTO_SETUP_LDAP_S4_SERVICES", "nslcd")
	e.mkHost(t, "s4h", "Rocky Linux release 8.9 (x)", map[string]string{
		"etc/openldap/ldap.conf": lbConfU("other", "o"), "etc/nslcd.conf": "n\n", "etc/chrony.conf": "n\n"})
	st := NewRealLdapBackup().Restore(j, "s4h")
	if !st.Applied || st.Reason != "변환 sssd.conf→nslcd.conf" {
		t.Fatalf("%s", lbMask(fmt.Sprintf("%+v", st)))
	}
	if !strings.HasPrefix(e.read(t, "s4h", "etc/nslcd.conf"), "# generated by auto_setup from backup sssd.conf") {
		t.Fatal("nslcd.conf 가 변환되지 않음")
	}
	if e.read(t, "s4h", "etc/chrony.conf") != lbChrony8 {
		t.Fatal("chrony.conf 가 그대로 복원되지 않음")
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "s4h/etc/sssd/sssd.conf")); err == nil {
		t.Fatal("sssd.conf 를 만들면 안 됨")
	}
	lg, _ := os.ReadFile(filepath.Join(e.hosts, "s4h/restart.log"))
	if !strings.HasSuffix(string(lg), "restart nslcd\nrestart autofs\nrestart chronyd\nrestart nscd\n") {
		t.Fatalf("restart.log:\n%s", lg)
	}
}

// compareEntries·restoreEntries 는 job·ldapbak 없이 백업 entries + 대상 호스트 + route 만으로 동작한다 (ldap-test 진입점용)
func TestCompareRestoreEntriesNoJob(t *testing.T) {
	e := lbSetup(t)
	e.mkHost(t, "h1", "CentOS Linux release 7.9 (Core)", map[string]string{
		"etc/openldap/ldap.conf": lbConfU("other", "o"), "etc/nslcd.conf": "n\n", "etc/ntp.conf": "n\n"})
	es := []lbEntry{{Name: "ldap.conf", Mode: "644", Owner: "root", Group: "root", Data: []byte(lbConf(lbSecret))},
		{Name: "sssd.conf", Mode: "600", Owner: "root", Group: "root", Data: []byte(lbSSSD8)},
		{Name: "chrony.conf", Mode: "644", Owner: "root", Group: "root", Data: []byte(lbChrony8)}}
	st, p, route := compareEntries("local", "h1", "/x", es)
	if p == nil || route != "local" || strings.Join(p.notes, "; ") != "변환 sssd.conf→nslcd.conf,chrony.conf→ntp.conf" {
		t.Fatalf("%+v %v %s", st, p != nil, route)
	}
	if e.read(t, "h1", "etc/nslcd.conf") != "n\n" {
		t.Fatal("비교만 했는데 파일이 바뀜")
	}
	st = restoreEntries("h1", p)
	if !st.Applied || st.Bindpw != BindpwDiff {
		t.Fatalf("%s", lbMask(fmt.Sprintf("%+v", st)))
	}
	if !strings.Contains(e.read(t, "h1", "etc/nslcd.conf"), "\nbinddn uid=svc_old,ou=user,dc=x\n") {
		t.Fatal("nslcd.conf 가 복원되지 않음")
	}
	if _, err := os.Stat(ldapBakDir()); err == nil {
		t.Fatal("ldapbak 을 만들면 안 됨")
	}
	// 대상 종류(nslcd/ntp)로 변환한 세트로 다시 비교하면 같음 (uid·종류 모두 같아짐)
	es7, _, _ := convertEntriesFor(es, "nslcd", "ntp")
	if st, p, _ = compareEntries("local", "h1", "/x", es7); p != nil || st.Bindpw != BindpwSame {
		t.Fatalf("재비교: %+v", st)
	}
}

func TestNslcdToSSSD(t *testing.T) {
	in := "# comment line\n  # indented comment\nURI \"ldap://a.x\" ldap://b.x\nuri ldaps://c.x\nBase \"dc=x,dc=y\"\n" +
		"base passwd ou=People,dc=x\nbase shadow ou=Shadow,dc=x\nbase   group 'ou=Group,dc=x'\nBINDDN uid=svc_old,ou=user,dc=x\n" +
		"BindPW \"" + lbSecret + "\"\r\nSSL start_tls\ntls_reqcert Demand\ntls_cacertdir /etc/openldap/cacerts\ntls_cacertfile /etc/pki/ca.pem\n" +
		"filter passwd (objectClass=posixAccount)\nmap passwd homeDirectory \"/home/$uid\"\nuid nslcd\r\ngid ldap\r\n"
	want := "# generated by auto_setup from backup nslcd.conf (RHEL7 -> RHEL8)\n[sssd]\nservices = nss, pam\ndomains = default\n\n" +
		"[nss]\nhomedir_substring = /home\n\n[pam]\n\n[domain/default]\nid_provider = ldap\nauth_provider = ldap\nchpass_provider = ldap\n" +
		"ldap_uri = ldap://a.x, ldap://b.x, ldaps://c.x\nldap_search_base = dc=x,dc=y\nldap_user_search_base = ou=People,dc=x\n" +
		"ldap_group_search_base = ou=Group,dc=x\nldap_id_use_start_tls = True\nldap_tls_reqcert = demand\n" +
		"ldap_tls_cacertdir = /etc/openldap/cacerts\nldap_tls_cacert = /etc/pki/ca.pem\nldap_default_bind_dn = uid=svc_old,ou=user,dc=x\n" +
		"ldap_default_authtok_type = password\nldap_default_authtok = " + lbSecret + "\ncache_credentials = True\n"
	got, err := nslcdToSSSD([]byte(in))
	if err != nil || string(got) != want {
		t.Fatalf("err=%v\n%s", err, lbMask(string(got)))
	}
	// 평문 ldap:// (TLS 없음): sssd 가 인증을 거부하지 않게 하는 줄, shadow 만 있으면 user base 로
	got, err = nslcdToSSSD([]byte("uri ldap://p\nbase shadow ou=S,dc=x\nbinddn cn=b\nbindpw " + lbSecret + "\n"))
	if err != nil || !strings.Contains(string(got), "ldap_user_search_base = ou=S,dc=x\n") ||
		!strings.Contains(string(got), "ldap_auth_disable_tls_never_use_in_production = true\n") ||
		strings.Contains(string(got), "start_tls") || strings.Contains(string(got), "ldap_search_base") {
		t.Fatalf("err=%v\n%s", err, lbMask(string(got)))
	}
	for name, src := range map[string]string{
		"uri 없음":       "base dc=x\nbinddn cn=a\nbindpw " + lbSecret + "\n",
		"uri 형식":       "uri DNS\nbinddn cn=a\nbindpw " + lbSecret + "\n",
		"binddn 없음":    "uri ldap://a\nbindpw " + lbSecret + "\n",
		"bindpw 빈 값":   "uri ldap://a\nbinddn cn=a\nbindpw   \n",
		"bindpw 빈 따옴표": "uri ldap://a\nbinddn cn=a\nbindpw \"\"\n",
	} {
		out, err := nslcdToSSSD([]byte(src))
		if err == nil || out != nil {
			t.Fatalf("%s: 오류여야 함:\n%s", name, lbMask(string(out)))
		}
		if strings.Contains(err.Error(), lbSecret) {
			t.Fatalf("%s: 오류 문자열에 bindpw", name)
		}
	}
}

func TestNtpToChrony(t *testing.T) {
	in := "# ntp.conf\ndriftfile /var/lib/ntp/drift\nrestrict default nomodify notrap nopeer noquery\nrestrict 127.0.0.1\n" +
		"SERVER 0.rhel.pool.ntp.org iburst # 주석\nserver -4 ntp1.x minpoll 4 maxpoll 10 key 3 prefer autokey\npool pool.x IBURST\n" +
		"server 127.127.1.0\nfudge 127.127.1.0 stratum 10\n  server \"ntp2.x\"\r\n#server old.x iburst\n"
	want := "# generated by auto_setup from backup ntp.conf (RHEL7 -> RHEL8)\nserver 0.rhel.pool.ntp.org iburst\n" +
		"server ntp1.x minpoll 4 maxpoll 10 prefer\npool pool.x iburst\nserver ntp2.x\n" +
		"driftfile /var/lib/chrony/drift\nmakestep 1.0 3\nrtcsync\nlogdir /var/log/chrony\n"
	if got, err := ntpToChrony([]byte(in)); err != nil || string(got) != want {
		t.Fatalf("err=%v\n%s", err, got)
	}
	if out, err := ntpToChrony([]byte("restrict default\nserver 127.127.1.0\n# server x\n")); err == nil || out != nil {
		t.Fatalf("server 없음 → 오류여야 함: %s", out)
	}
}

func TestSssdToNslcd(t *testing.T) {
	in := "# sssd\n[sssd]\nservices = nss, pam\ndomains = LDAP2, other\n\n[domain/other]\nldap_uri = ldap://wrong\n" +
		"ldap_default_bind_dn = cn=wrong\nldap_default_authtok = wrong\n\n[domain/ldap2]\n; comment\nid_provider = ldap\n" +
		"LDAP_URI = ldap://a.x, ldaps://b.x ,ldap://c.x\nldap_uri = ldap://dup\nldap_search_base = dc=x,dc=y\n" +
		"ldap_user_search_base = ou=People,dc=x?subtree?(objectClass=posixAccount)\nldap_group_search_base = ou=Group,dc=x\n" +
		"ldap_id_use_start_tls = TRUE\nldap_tls_reqcert = Demand\nldap_tls_cacertdir = /etc/openldap/cacerts\nldap_tls_cacert = /etc/pki/ca.pem\n" +
		"ldap_default_bind_dn = uid=svc_old,ou=user,dc=x\nldap_default_authtok_type = password\nldap_default_authtok = " + lbSecret + "\r\n" +
		"cache_credentials = True\n"
	want := "# generated by auto_setup from backup sssd.conf (RHEL8 -> RHEL7)\nuid nslcd\ngid ldap\nuri ldap://a.x ldaps://b.x ldap://c.x\n" +
		"base dc=x,dc=y\nbase passwd ou=People,dc=x\nbase group ou=Group,dc=x\nbinddn uid=svc_old,ou=user,dc=x\nbindpw " + lbSecret + "\n" +
		"ssl start_tls\ntls_reqcert demand\ntls_cacertdir /etc/openldap/cacerts\ntls_cacertfile /etc/pki/ca.pem\n"
	got, err := sssdToNslcd([]byte(in))
	if err != nil || string(got) != want {
		t.Fatalf("err=%v\n%s", err, lbMask(string(got)))
	}
	// [sssd] domains 가 없으면 첫 [domain/*], start_tls 가 아니면 ssl 줄 없음
	got, err = sssdToNslcd([]byte("[domain/a]\nldap_uri = ldap://p\nldap_default_bind_dn = cn=b\nldap_default_authtok = " + lbSecret +
		"\nldap_id_use_start_tls = false\n[domain/b]\nldap_uri = ldap://q\n"))
	if err != nil || string(got) != "# generated by auto_setup from backup sssd.conf (RHEL8 -> RHEL7)\nuid nslcd\ngid ldap\nuri ldap://p\nbinddn cn=b\nbindpw "+lbSecret+"\n" {
		t.Fatalf("err=%v\n%s", err, lbMask(string(got)))
	}
	ok := "ldap_uri = ldap://a\nldap_default_bind_dn = cn=a\nldap_default_authtok = " + lbSecret + "\n"
	for name, src := range map[string]string{
		"domain 없음":   "[sssd]\ndomains = x\n[domain/y]\n" + ok,
		"domain 절 없음": "[nss]\nfilter_users = root\n",
		"uri 없음":      "[domain/d]\nldap_default_bind_dn = cn=a\nldap_default_authtok = " + lbSecret + "\n",
		"uri 형식":      "[domain/d]\nldap_uri = DNS\nldap_default_bind_dn = cn=a\nldap_default_authtok = " + lbSecret + "\n",
		"binddn 없음":   "[domain/d]\nldap_uri = ldap://a\nldap_default_authtok = " + lbSecret + "\n",
		"bindpw 없음":   "[domain/d]\nldap_uri = ldap://a\nldap_default_bind_dn = cn=a\nldap_default_authtok =  \n",
		"authtok 종류":  "[domain/d]\n" + ok + "ldap_default_authtok_type = obfuscated_password\n",
	} {
		out, err := sssdToNslcd([]byte(src))
		if err == nil || out != nil {
			t.Fatalf("%s: 오류여야 함:\n%s", name, lbMask(string(out)))
		}
		if strings.Contains(err.Error(), lbSecret) {
			t.Fatalf("%s: 오류 문자열에 bindpw", name)
		}
	}
}

func TestChronyToNtp(t *testing.T) {
	in := "# chrony\n! old comment\n;server bad.x\nSERVER 0.rocky.pool.ntp.org iburst # c\n" +
		"server ntp1.x minpoll 4 maxpoll 10 key 3 maxdelay 0.3 prefer nts\npool pool.x IBURST maxsources 3\n" +
		"refclock PHC /dev/ptp0 poll 3\ndriftfile /var/lib/chrony/drift\nmakestep 1.0 3\n  server ntp2.x\r\n"
	want := "# generated by auto_setup from backup chrony.conf (RHEL8 -> RHEL7)\ndriftfile /var/lib/ntp/drift\n" +
		"restrict default nomodify notrap nopeer noquery\nrestrict 127.0.0.1\nrestrict ::1\n" +
		"server 0.rocky.pool.ntp.org iburst\nserver ntp1.x minpoll 4 maxpoll 10 prefer\npool pool.x iburst\nserver ntp2.x\n"
	if got, err := chronyToNtp([]byte(in)); err != nil || string(got) != want {
		t.Fatalf("err=%v\n%s", err, got)
	}
	if out, err := chronyToNtp([]byte("refclock PHC /dev/ptp0\n# server x\n! server y\n")); err == nil || out != nil {
		t.Fatalf("server 없음 → 오류여야 함: %s", out)
	}
}

// 왕복(7→8→7): 핵심 값(uri, base, binddn, bindpw, server)이 유지된다. convertEntriesFor 의 이름·순서·오류 문자열도 확인
func TestLdapConvertRoundTrip(t *testing.T) {
	es := []lbEntry{{Name: "ldap.conf", Mode: "644", Owner: "root", Group: "root", Data: []byte(lbConf(lbSecret))},
		{Name: "nslcd.conf", Mode: "600", Owner: "root", Group: "ldap", Data: []byte(lbNslcd7)},
		{Name: "ntp.conf", Mode: "644", Owner: "root", Group: "root", Data: []byte(lbNtp7)}}
	es8, conv, err := convertEntriesFor(es, "sssd", "chrony")
	if err != nil || strings.Join(conv, ",") != "nslcd.conf→sssd.conf,ntp.conf→chrony.conf" {
		t.Fatalf("7→8: %v %v", err, conv)
	}
	es7, conv, err := convertEntriesFor(es8, "nslcd", "ntp")
	if err != nil || strings.Join(conv, ",") != "sssd.conf→nslcd.conf,chrony.conf→ntp.conf" {
		t.Fatalf("8→7: %v %v", err, conv)
	}
	if len(es7) != 3 || es7[0].Name != "ldap.conf" || string(es7[0].Data) != lbConf(lbSecret) || es7[1].Name != "nslcd.conf" ||
		es7[1].Mode != "600" || es7[1].Owner != "root" || es7[1].Group != "root" || es7[2].Name != "ntp.conf" || es7[2].Mode != "644" {
		t.Fatalf("왕복 세트: %d개 %s/%s/%s", len(es7), es7[0].Name, es7[1].Name, es7[2].Name)
	}
	nslcd := string(es7[1].Data)
	for _, w := range []string{"\nuri ldap://l1 ldap://l2\n", "\nbase dc=x\n", "\nbinddn uid=svc_old,ou=user,dc=x\n", "\nbindpw " + lbSecret + "\n",
		"\nssl start_tls\n", "\ntls_reqcert never\n"} {
		if !strings.Contains(nslcd, w) {
			t.Fatalf("왕복 nslcd.conf 에 %q 없음:\n%s", lbMask(w), lbMask(nslcd))
		}
	}
	if ntp := string(es7[2].Data); !strings.Contains(ntp, "\nserver n1 iburst\nserver n2\n") {
		t.Fatalf("왕복 ntp.conf:\n%s", ntp)
	}
	// 8→7→8 도 sssd 핵심 값 유지
	n7, err := sssdToNslcd([]byte(lbSSSD8))
	if err != nil {
		t.Fatal(err)
	}
	s8, err := nslcdToSSSD(n7)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"\nldap_uri = ldap://l1, ldap://l2\n", "\nldap_search_base = dc=x\n", "\nldap_default_bind_dn = uid=svc_old,ou=user,dc=x\n",
		"\nldap_default_authtok = " + lbSecret + "\n", "\nldap_id_use_start_tls = True\n"} {
		if !strings.Contains(string(s8), w) {
			t.Fatalf("왕복 sssd.conf 에 %q 없음", lbMask(w))
		}
	}
	// 같은 종류면 그대로(변환 없음), 종류 값이 틀리면 오류, 변환 실패 오류는 파일 이름만
	if same, conv, err := convertEntriesFor(es, "nslcd", "ntp"); err != nil || len(conv) != 0 || !reflect.DeepEqual(same, es) {
		t.Fatalf("같은 종류: %v %v", err, conv)
	}
	if _, _, err := convertEntriesFor(es, "x", "ntp"); err == nil {
		t.Fatal("종류 오류여야 함")
	}
	bad := []lbEntry{{Name: "sssd.conf", Mode: "600", Owner: "root", Group: "root",
		Data: []byte("[domain/d]\nldap_uri = ldap://a\nldap_default_authtok = " + lbSecret + "\n")}}
	if _, _, err := convertEntriesFor(bad, "nslcd", "ntp"); err == nil || err.Error() != "변환 실패: nslcd.conf" {
		t.Fatalf("변환 실패 오류: %v", err)
	}
}

func TestApplyNoResponseAndNoBackup(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	os.RemoveAll(filepath.Join(e.hosts, "h1")) // 새 OS 무응답
	st := NewRealLdapBackup().Restore(j, "h1")
	if st.Applied || st.Bindpw != BindpwUnknown || st.Backup != LdapBackupOK || !strings.Contains(st.Reason, "무응답") { // 비교 전 실패 — diff 가 아님
		t.Fatalf("%+v", st)
	}
	st = NewRealLdapBackup().Restore(lbJob("nojob", "zz"), "zz")
	if st.Backup != LdapBackupNone {
		t.Fatalf("%+v", st)
	}
	// 이름 검증: 경로 탈출 시도
	if st = NewRealLdapBackup().Restore(lbJob("j", "../x"), "../x"); st.Backup != LdapBackupNone {
		t.Fatalf("%+v", st)
	}
}

func TestApplyPartialFailureReason(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	// 새 OS 에 etc/sssd 디렉터리가 없어 sssd.conf 쓰기 실패 (chrony/resolv/ldap 은 성공해야 함)
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", map[string]string{"etc/openldap/ldap.conf": lbConfU("other", "o")})
	st := NewRealLdapBackup().Restore(j, "h1")
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
		"put resolv.conf '/etc/resolv.conf' 644 root root '" + share + "/resolv.conf' && W=1",
		"put ldap.conf '/etc/openldap/ldap.conf' 644 root root '" + share + "/ldap.conf' && W=1",
		"put sssd.conf '/etc/sssd/sssd.conf' 600 root root '" + share + "/sssd.conf' && W=1",
		"put chrony.conf '/etc/chrony.conf' 644 root root '" + share + "/chrony.conf' && W=1",
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
	order := []string{"put resolv.conf ", "put ldap.conf ", "put sssd.conf ", "put chrony.conf ", "TS=ntpd", "TS=chronyd", `svc "$AK"`, "svc autofs", `svc "$TS"`, "svc nscd", "==BINDDN"}
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
	st := NewRealLdapBackup().Restore(j, "h1")
	if !st.Applied {
		t.Fatalf("%+v", st)
	}
	// 실패 경로(공유경로 접근 실패·무응답·수동 복원)도 포함
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newFiles("svc_new_uid", "o"))
	t.Setenv("STUB_HIDE_SHARE", "1")
	st2 := NewRealLdapBackup().Restore(j, "h1")
	os.RemoveAll(filepath.Join(e.hosts, "h1"))
	st3 := NewRealLdapBackup().Restore(j, "h1")
	ldap_share_dir = ""
	e.mkHost(t, "h1", "Rocky Linux release 8.9 (x)", newFiles("svc_new_uid", "o"))
	st4 := NewRealLdapBackup().Restore(j, "h1")

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

func TestParseInfoSvcSkip(t *testing.T) {
	in := parseInfo([]string{"==SVC sssd ok", "==SVC autofs err", "==SVC nscd skip", "==SVC chronyd skip"})
	if !in.Svc["sssd"] || in.Svc["autofs"] {
		t.Fatalf("Svc: %v", in.Svc)
	}
	if _, ok := in.Svc["autofs"]; !ok {
		t.Fatal("err 는 Svc 에 false 로 남아야 함")
	}
	if _, ok := in.Svc["nscd"]; ok || !in.SvcSkip["nscd"] || !in.SvcSkip["chronyd"] {
		t.Fatalf("skip 은 Svc 가 아니라 SvcSkip: %v %v", in.Svc, in.SvcSkip)
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

// os8 에서 gossh 가 닿지 않는 호스트(STUB_NOLOCAL)는 os6_mgmt 경유로 다시 수집하고 route=os6 로 바꾼다 (os8 에서 되는 호스트는 그대로)
func TestBackupFallsBackToOS6(t *testing.T) {
	e := lbSetup(t)
	os6_mgmt, os6_gossh = "mgmtx", "gossh"
	files := map[string]string{"etc/openldap/ldap.conf": lbConf("pw1"), "etc/sssd/sssd.conf": "s\n", "etc/chrony.conf": "c\n"}
	e.mkHost(t, "a1", "Rocky Linux release 8.9 (x)", files)
	e.mkHost(t, "b1", "Rocky Linux release 8.9 (x)", files)
	e.mkHost(t, "gone", "Rocky Linux release 8.9 (x)", files)
	t.Setenv("STUB_NOLOCAL", "b1 gone")
	os.RemoveAll(filepath.Join(e.hosts, "gone")) // os6 에서도 무응답
	j := lbJob("jobfb", "a1", "b1", "gone")
	res := NewRealLdapBackup().Backup(j, []string{"a1", "b1", "gone"})
	if res["a1"].Backup != LdapBackupOK || res["b1"].Backup != LdapBackupOK {
		t.Fatalf("%+v", res)
	}
	if res["gone"].Backup != LdapBackupNone || res["gone"].Reason != "무응답" {
		t.Fatalf("양쪽 무응답은 none: %+v", res["gone"])
	}
	if j.Hosts["a1"].Route != "local" || j.Hosts["b1"].Route != "os6" || j.Hosts["gone"].Route != "local" {
		t.Fatalf("route: a1=%q b1=%q gone=%q", j.Hosts["a1"].Route, j.Hosts["b1"].Route, j.Hosts["gone"].Route)
	}
	lg, _ := os.ReadFile(e.log)
	if strings.Count(string(lg), "ssh mgmtx") != 1 {
		t.Fatalf("os6 재수집은 한 번의 배치여야 함:\n%s", lg)
	}
	// os6 미설정이면 대체 없음 (종전 동작)
	os6_mgmt, os6_gossh = "", ""
	j2 := lbJob("jobfb2", "b1")
	if r := NewRealLdapBackup().Backup(j2, []string{"b1"}); r["b1"].Backup != LdapBackupNone || j2.Hosts["b1"].Route != "local" {
		t.Fatalf("os6 미설정: %+v route=%q", r["b1"], j2.Hosts["b1"].Route)
	}
}

// 새 OS 확인(Apply)도 os8 에서 무응답이면 os6_mgmt 경유로 한 번 더, 같은 경로로 복원까지
func TestApplyFallsBackToOS6(t *testing.T) {
	e := lbSetup(t)
	os6_mgmt, os6_gossh = "mgmtx", "gossh"
	old := map[string]string{"etc/openldap/ldap.conf": lbConfU("svc_old", "pw1"), "etc/sssd/sssd.conf": "s\n", "etc/chrony.conf": "c\n"}
	e.mkHost(t, "b1", "Rocky Linux release 8.9 (x)", old)
	j := lbJob("jobap", "b1")
	if r := NewRealLdapBackup().Backup(j, []string{"b1"}); r["b1"].Backup != LdapBackupOK {
		t.Fatalf("%+v", r)
	}
	e.mkHost(t, "b1", "Rocky Linux release 9.2 (x)", map[string]string{"etc/openldap/ldap.conf": lbConfU("svc_new", "pw2"), "etc/sssd/sssd.conf": "n\n", "etc/chrony.conf": "n\n"})
	t.Setenv("STUB_NOLOCAL", "b1")
	st := NewRealLdapBackup().Restore(j, "b1")
	if st.Bindpw != BindpwDiff || !st.Applied || j.Hosts["b1"].Route != "os6" {
		t.Fatalf("os6 경유 복원 기대: %+v route=%q", st, j.Hosts["b1"].Route)
	}
	if !strings.Contains(e.read(t, "b1", "etc/openldap/ldap.conf"), "svc_old") {
		t.Fatal("백업본으로 복원되지 않음")
	}
}

// 수집 무응답 사유 보존: gossh 의 호스트별 접속 실패 줄("SSH 접속 실패: …")을 "무응답: …" 으로 남긴다 (데몬은 HasPrefix 로 일시적 실패 판정)
func TestBackupKeepsSSHFailReason(t *testing.T) {
	e := lbSetup(t)
	os6_mgmt, os6_gossh = "", ""
	e.mkHost(t, "ok1", "Rocky Linux release 8.9 (x)", map[string]string{"etc/openldap/ldap.conf": lbConf(lbSecret)})
	e.mkHost(t, "f1", "Rocky Linux release 8.9 (x)", map[string]string{"etc/openldap/ldap.conf": lbConf(lbSecret)})
	t.Setenv("STUB_SSHFAIL", "f1")
	res := NewRealLdapBackup().Backup(lbJob("jobw", "ok1", "f1", "gone"), []string{"ok1", "f1", "gone"})
	if res["ok1"].Backup != LdapBackupOK {
		t.Fatalf("ok1: %+v", res["ok1"])
	}
	if want := noneState(ldapNoResp + ": SSH 접속 실패: connection refused"); res["f1"] != want {
		t.Fatalf("f1 사유 보존: %+v, want %+v", res["f1"], want)
	}
	if res["gone"] != noneState(ldapNoResp) {
		t.Fatalf("사유 줄 없는 무응답: %+v", res["gone"])
	}
	for h, st := range res {
		if strings.Contains(st.Reason, lbSecret) {
			t.Fatalf("%s 사유에 비밀값: %q", h, st.Reason)
		}
		if h != "ok1" && !ldapTransient(st) {
			t.Fatalf("%s 는 일시적 실패여야 함: %+v", h, st)
		}
	}
}

// local(os8) 경로도 os6 경로처럼 sshLimit(ssh_timeout 반영) 상한으로 gossh 를 부르고, 출력 없는 시간 초과는 "시간 초과"
func TestLdapLocalSSHLimitAndTimeout(t *testing.T) {
	setupDir(t)
	oldExec, oldSSH, oldM, oldG := ldapExec, ssh_timeout, os6_mgmt, os6_gossh
	t.Cleanup(func() { ldapExec, ssh_timeout, os6_mgmt, os6_gossh = oldExec, oldSSH, oldM, oldG })
	ssh_timeout, os6_mgmt, os6_gossh = "30", "", ""
	var got []time.Duration
	ldapExec = func(timeout time.Duration, stdin string, name string, args ...string) (string, error) {
		got = append(got, timeout)
		return "", errLdapTimeout
	}
	j := lbJob("jobt", "h1")
	res := NewRealLdapBackup().Backup(j, []string{"h1"})
	if want := noneState(ldapCallFail + ": 시간 초과"); res["h1"] != want {
		t.Fatalf("시간 초과 사유: %+v, want %+v", res["h1"], want)
	}
	in := lbInfo{Files: []lbEntry{{Name: "ldap.conf", Mode: "0644", Owner: "root", Group: "root", Data: []byte(lbConfU("svc", ""))}}}
	if err := saveBackup("jobt", "h1", in); err != nil {
		t.Fatal(err)
	}
	if st := NewRealLdapBackup().Restore(j, "h1"); !strings.Contains(st.Reason, "확인 호출 실패") {
		t.Fatalf("Apply 시간 초과: %+v", st)
	}
	if len(got) != 2 || got[0] != 130*time.Second || got[1] != 85*time.Second {
		t.Fatalf("local gossh 상한은 sshLimit(90s/45s) = 130s/85s 여야 함: %v", got)
	}
}
