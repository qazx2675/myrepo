// ldaptest_test.go - auto_setup ldap-test (ldaptest.go): 가짜 gossh(ldapbk_test.go 의 lbSetup·호스트별 가짜 루트)로
// list → out/<호스트>/{backup,rhel7,rhel8} 구조·권한·내용, 화면 요약·비밀값 미노출, target 모드 비교·복원, 종료코드
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func ltRun(stdin string, args ...string) (int, string, string) {
	var o, e bytes.Buffer
	c := cmdLdapTest(args, strings.NewReader(stdin), &o, &e)
	return c, o.String(), e.String()
}

func ltList(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "list")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ltHosts: OS7 1대(h7: nslcd/ntp)·OS8 1대(h8: sssd/chrony + autofs 파일)
func ltHosts(t *testing.T, e lbEnv) {
	t.Helper()
	e.mkHost(t, "h7", "CentOS Linux release 7.9 (Core)", lbOS7Old(lbNslcd7, lbNtp7))
	e.mkHost(t, "h8", "Rocky Linux release 8.9 (x)", lbOS8Conv(lbSSSD8, lbChrony8))
}

// ltBase: 가짜 루트 파일 표(etc/...) → 백업 이름 표 (이름은 경로의 마지막 요소)
func ltBase(m map[string]string) map[string]string {
	r := map[string]string{}
	for p, c := range m {
		r[filepath.Base(p)] = c
	}
	return r
}

// ltWith: 공통 파일(인증·시각 파일 제외)에 extra 를 더한 표
func ltWith(m map[string]string, extra map[string]string) map[string]string {
	r := map[string]string{}
	for n, c := range m {
		if cl, _ := kindOfName(n); cl == "" {
			r[n] = c
		}
	}
	for n, c := range extra {
		r[n] = c
	}
	return r
}

func ltConv(t *testing.T, fn func([]byte) ([]byte, error), src string) string {
	t.Helper()
	b, err := fn([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ltCheckDir: dir 의 설정 파일이 want 와 정확히 같은지 (extra: 함께 있어야 하는 부가 파일)
func ltCheckDir(t *testing.T, dir string, want map[string]string, extra ...string) {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got, exp []string
	for _, d := range des {
		got = append(got, d.Name())
	}
	for n := range want {
		exp = append(exp, n)
	}
	exp = append(exp, extra...)
	sort.Strings(got)
	sort.Strings(exp)
	if strings.Join(got, " ") != strings.Join(exp, " ") {
		t.Fatalf("%s: 파일 %v (want %v)", dir, got, exp)
	}
	for n, c := range want {
		if g := mustRead(t, filepath.Join(dir, n)); g != c {
			t.Fatalf("%s/%s 내용 다름:\n%s\n--- want ---\n%s", dir, n, lbMask(g), lbMask(c))
		}
	}
}

func TestLdapTestListOut(t *testing.T) {
	e := lbSetup(t)
	ltHosts(t, e)
	out := filepath.Join(t.TempDir(), "out")
	list := ltList(t, "# 테스트 목록\r\nh7, h8\r\n\r\n  h7\t# 중복\r\n")
	code, so, se := ltRun("", "-out", out, list)
	if code != 0 {
		t.Fatalf("code %d\n%s%s", code, lbMask(so), lbMask(se))
	}
	for _, w := range []string{"h7 ", "OS7 ", "nslcd/ntp", "OS8 ", "sssd/chrony", "→ " + filepath.Join(out, "h8"),
		"합계: 2대 — 성공 2, 실패 0 (backup 실패 0, rhel7 실패 0, rhel8 실패 0)"} {
		if !strings.Contains(so, w) {
			t.Fatalf("화면에 %q 없음:\n%s", w, lbMask(so))
		}
	}
	if strings.Count(so, "backup OK  rhel7 OK  rhel8 OK") != 2 {
		t.Fatalf("호스트 줄이 2개가 아님(중복 제거):\n%s", lbMask(so))
	}

	b7, b8 := ltBase(lbOS7Old(lbNslcd7, lbNtp7)), ltBase(lbOS8Conv(lbSSSD8, lbChrony8))
	want := map[string]map[string]string{
		"h7/backup": b7,
		"h7/rhel7":  b7,
		"h7/rhel8":  ltWith(b7, map[string]string{"sssd.conf": ltConv(t, nslcdToSSSD, lbNslcd7), "chrony.conf": ltConv(t, ntpToChrony, lbNtp7)}),
		"h8/backup": b8,
		"h8/rhel7":  ltWith(b8, map[string]string{"nslcd.conf": ltConv(t, sssdToNslcd, lbSSSD8), "ntp.conf": ltConv(t, chronyToNtp, lbChrony8)}),
		"h8/rhel8":  b8,
	}
	for d, w := range want {
		if strings.HasSuffix(d, "backup") {
			ltCheckDir(t, filepath.Join(out, d), w, lbMetaName)
		} else {
			ltCheckDir(t, filepath.Join(out, d), w, "files.txt", "notes.txt")
		}
	}
	// 공통 파일(ldap.conf·resolv.conf·autofs.conf·autofs_ldap_auth.conf)은 backup·rhel7·rhel8 이 같다
	if want["h8/rhel7"]["autofs_ldap_auth.conf"] != b8["autofs_ldap_auth.conf"] || want["h7/rhel8"]["ldap.conf"] != b7["ldap.conf"] {
		t.Fatal("공통 파일 기대값 오류")
	}
	for d, n := range map[string]string{
		"h7/rhel7": "변환 없음(원본 그대로)", "h7/rhel8": "변환: nslcd.conf→sssd.conf, ntp.conf→chrony.conf",
		"h8/rhel7": "변환: sssd.conf→nslcd.conf, chrony.conf→ntp.conf", "h8/rhel8": "변환 없음(원본 그대로)"} {
		if g := mustRead(t, filepath.Join(out, d, "notes.txt")); g != n+"\n" {
			t.Fatalf("%s/notes.txt %q", d, g)
		}
		fl := mustRead(t, filepath.Join(out, d, "files.txt"))
		for name := range want[d] {
			if !strings.Contains(fl, "\n"+name+"\t"+lbFilePaths[name]+"\t") {
				t.Fatalf("%s/files.txt 에 %s 없음:\n%s", d, name, fl)
			}
		}
	}
	if !strings.Contains(mustRead(t, filepath.Join(out, "h7/rhel8/files.txt")), "\nsssd.conf\t/etc/sssd/sssd.conf\t600\troot\troot\t") {
		t.Fatal("변환 파일의 mode·owner")
	}
	for h, w := range map[string]ldapMeta{"h7": {Job: ltJob, Host: "h7", OSMajor: 7, Auth: "nslcd", Time: "ntp"},
		"h8": {Job: ltJob, Host: "h8", OSMajor: 8, Auth: "sssd", Time: "chrony"}} {
		var m ldapMeta
		if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(out, h, "backup", lbMetaName))), &m); err != nil {
			t.Fatal(err)
		}
		if m.Job != w.Job || m.Host != w.Host || m.OSMajor != w.OSMajor || m.Auth != w.Auth || m.Time != w.Time || len(m.Files) != len(want[h+"/backup"]) {
			t.Fatalf("%s meta %+v", h, m)
		}
		for _, f := range m.Files {
			if f.Path != lbFilePaths[f.Name] {
				t.Fatalf("%s meta 경로 %+v", h, f)
			}
		}
	}

	// 권한: 디렉터리 0700, 파일 0600 / 화면·files.txt·notes.txt·meta.json 에 비밀값 없음
	plain := so + se
	filepath.Walk(out, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if w := os.FileMode(0600); fi.IsDir() {
			if fi.Mode().Perm() != 0700 {
				t.Fatalf("%s mode %v", p, fi.Mode().Perm())
			}
		} else if fi.Mode().Perm() != w {
			t.Fatalf("%s mode %v", p, fi.Mode().Perm())
		}
		if n := fi.Name(); n == "files.txt" || n == "notes.txt" || n == lbMetaName {
			plain += mustRead(t, p)
		}
		return nil
	})
	if strings.Contains(plain, lbSecret) || strings.Contains(plain, "BINDPW") {
		t.Fatal("화면·files.txt·notes.txt·meta.json 에 bindpw")
	}
	if _, err := os.Stat(ldapBakDir()); err == nil {
		t.Fatal("ldap-test 가 ldapbak 을 만들면 안 됨")
	}

	// 같은 호스트를 다시 실행하면 out/<호스트> 를 비우고 다시 만든다 (다른 호스트는 그대로)
	os.WriteFile(filepath.Join(out, "h7", "stale"), []byte("x"), 0600)
	if code, so, _ = ltRun("", "-out", out, ltList(t, "h7\n")); code != 0 {
		t.Fatalf("재실행 code %d\n%s", code, lbMask(so))
	}
	if _, err := os.Stat(filepath.Join(out, "h7", "stale")); err == nil {
		t.Fatal("이전 결과가 남음")
	}
	ltCheckDir(t, filepath.Join(out, "h8/rhel8"), b8, "files.txt", "notes.txt")

	// -out 이 없으면 현재 디렉터리의 ./out
	cwd, _ := os.Getwd()
	wd := t.TempDir()
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })
	if code, so, _ = ltRun("", list); code != 0 || !strings.Contains(so, "→ out/h7") {
		t.Fatalf("기본 out: code %d\n%s", code, lbMask(so))
	}
	ltCheckDir(t, filepath.Join(wd, "out/h7/rhel7"), b7, "files.txt", "notes.txt")
}

func TestLdapTestFailures(t *testing.T) {
	e := lbSetup(t)
	// hbad: sssd.conf 에 uri·binddn 이 없어 nslcd.conf 변환 실패(rhel7) / hnone: 무응답 / hnoldap: ldap.conf 없음
	e.mkHost(t, "hbad", "Rocky Linux release 8.9 (x)", map[string]string{"etc/openldap/ldap.conf": lbConf(lbSecret),
		"etc/sssd/sssd.conf": "[domain/x]\nldap_default_authtok = " + lbSecret + "\n", "etc/chrony.conf": lbChrony8})
	e.mkHost(t, "hnoldap", "Rocky Linux release 8.9 (x)", map[string]string{"etc/sssd/sssd.conf": "x\n", "etc/chrony.conf": "server c\n"})
	out := filepath.Join(t.TempDir(), "out")
	code, so, se := ltRun("", "-out", out, ltList(t, "hbad hnone\nhnoldap\n"))
	if code != 1 {
		t.Fatalf("code %d\n%s%s", code, lbMask(so), lbMask(se))
	}
	for _, w := range []string{"backup OK  rhel7 FAIL(변환 실패: nslcd.conf)  rhel8 OK", "backup FAIL(무응답)", "backup FAIL(ldap.conf 없음)",
		"rhel7 FAIL(백업 없음)", "합계: 3대 — 성공 0, 실패 3 (backup 실패 2, rhel7 실패 3, rhel8 실패 2)"} {
		if !strings.Contains(so, w) {
			t.Fatalf("화면에 %q 없음:\n%s", w, lbMask(so))
		}
	}
	if strings.Contains(so+se, lbSecret) {
		t.Fatal("화면에 bindpw")
	}
	if _, err := os.Stat(filepath.Join(out, "hbad", "rhel7")); err == nil {
		t.Fatal("변환 실패한 세트 디렉터리가 생김")
	}
	if _, err := os.Stat(filepath.Join(out, "hbad", "rhel8", "sssd.conf")); err != nil {
		t.Fatal("rhel8 세트 없음")
	}
	// target 모드: 원본 백업 실패면 비교하지 않음
	if code, so, _ = ltRun("", "-out", out, "-y", ltList(t, "hnone\n"), "hbad"); code != 1 || !strings.Contains(so, "백업 실패로 비교하지 않음") {
		t.Fatalf("code %d\n%s", code, lbMask(so))
	}
}

func TestLdapTestUsage(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	two := ltList(t, "h1\nh2\n")
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "사용법"},
		{[]string{filepath.Join(dir, "nope")}, "list 파일을 읽을 수 없습니다"},
		{[]string{ltList(t, "# 주석만\n\n")}, "호스트가 없습니다"},
		{[]string{ltList(t, "ok1\nbad;host\n")}, "list 2행: 잘못된 호스트 이름"},
		{[]string{"-z", two}, "알 수 없는 옵션: -z"},
		{[]string{two, "a", "b"}, "사용법"},
		{[]string{two, "-out"}, "-out 뒤에 디렉터리가 필요합니다"},
		{[]string{ltList(t, "h1\n"), "bad/target"}, "잘못된 target 서버 이름"},
		{[]string{"-out", out, two, "t1"}, "정확히 1대여야 합니다 (현재 2대)"},
	} {
		code, so, se := ltRun("", c.args...)
		if code != 2 || so != "" || !strings.Contains(se, c.want) || !strings.Contains(se, "사용법: auto_setup ldap-test") {
			t.Fatalf("%v: code %d stdout %q stderr %q", c.args, code, so, se)
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("사용법 오류인데 결과 디렉터리를 만듦(수집함)")
	}
	if code, so, _ := ltRun("", "-h"); code != 0 || !strings.Contains(so, "사용법: auto_setup ldap-test") {
		t.Fatalf("-h: %d %q", code, so)
	}
	if !strings.Contains(usageText, "ldap-test [-out <dir>] [-y] <list파일> [target서버]") {
		t.Fatal("auto_setup 사용법에 ldap-test 없음")
	}
}

// OS8 백업(h8) → OS7 target(t7): 확인에 yes 가 아니면 쓰지 않고, yes 면 sssd→nslcd·chrony→ntp 변환 복원 (auto_setup 과 같은 결과)
func TestLdapTestTargetRestore8to7(t *testing.T) {
	e := lbSetup(t)
	ltHosts(t, e)
	e.mkHost(t, "t7", "CentOS Linux release 7.9 (Core)", map[string]string{
		"etc/openldap/ldap.conf": lbConfU("other", "o"), "etc/nslcd.conf": "n\n", "etc/ntp.conf": "n\n"})
	out := filepath.Join(t.TempDir(), "out")
	list := ltList(t, "h8\n")

	code, so, _ := ltRun("no\n", "-out", out, list, "t7")
	for _, w := range []string{"== target t7 : h8 의 백업으로 비교", "비교: 복원 필요 — binddn uid 다름",
		"변환: 8→7 (sssd.conf→nslcd.conf,chrony.conf→ntp.conf)", "/etc/nslcd.conf", "계속하려면 yes 입력", "취소 — 대상에 아무것도 쓰지 않았습니다"} {
		if !strings.Contains(so, w) {
			t.Fatalf("화면에 %q 없음:\n%s", w, lbMask(so))
		}
	}
	if code != 1 || e.read(t, "t7", "etc/nslcd.conf") != "n\n" || e.shareLeft(t) != 0 {
		t.Fatalf("취소인데 code %d 또는 파일이 바뀜", code)
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "t7/restart.log")); err == nil {
		t.Fatal("취소인데 재시작")
	}

	code, so, se := ltRun("yes\n", "-out", out, list, "t7")
	if code != 0 {
		t.Fatalf("code %d\n%s%s", code, lbMask(so), lbMask(se))
	}
	if e.read(t, "t7", "etc/nslcd.conf") != ltConv(t, sssdToNslcd, lbSSSD8) || e.read(t, "t7", "etc/ntp.conf") != ltConv(t, chronyToNtp, lbChrony8) {
		t.Fatal("nslcd.conf·ntp.conf 가 변환 복원되지 않음")
	}
	for _, p := range []string{"etc/openldap/ldap.conf", "etc/resolv.conf", "etc/autofs.conf", "etc/autofs_ldap_auth.conf"} {
		if e.read(t, "t7", p) != lbOS8Old[p] {
			t.Fatalf("공통 파일 %s 가 복원되지 않음", p)
		}
	}
	for _, p := range []string{"etc/sssd/sssd.conf", "etc/chrony.conf"} {
		if _, err := os.Stat(filepath.Join(e.hosts, "t7", p)); err == nil {
			t.Fatalf("OS7 에 %s 를 쓰면 안 됨", p)
		}
	}
	lg := mustRead(t, filepath.Join(e.hosts, "t7/restart.log"))
	if !strings.HasSuffix(lg, "restart nslcd\nrestart autofs\nrestart ntpd\nrestart nscd\n") {
		t.Fatalf("restart.log:\n%s", lg)
	}
	for _, w := range []string{"  nslcd.conf             OK", "  autofs_ldap_auth.conf  OK", "서비스 재시작: nslcd ok, autofs ok, ntpd ok, nscd ok",
		"복원 후 binddn uid: 백업과 같음", "결과: 복원 완료 (변환 sssd.conf→nslcd.conf,chrony.conf→ntp.conf)"} {
		if !strings.Contains(so, w) {
			t.Fatalf("화면에 %q 없음:\n%s", w, lbMask(so))
		}
	}
	if strings.Contains(so, "FAIL") || strings.Contains(so+se, lbSecret) || e.shareLeft(t) != 0 {
		t.Fatalf("FAIL·bindpw·공유 임시 디렉터리:\n%s", lbMask(so))
	}
	if _, err := os.Stat(filepath.Join(out, "h8", "rhel7", "nslcd.conf")); err != nil {
		t.Fatal("target 모드에서도 list 서버의 out 을 만든다")
	}
}

func TestLdapTestTargetSameAnd7to8(t *testing.T) {
	e := lbSetup(t)
	ltHosts(t, e)
	out := filepath.Join(t.TempDir(), "out")
	list7 := ltList(t, "h7\n")

	// 같음: 같은 서버(uid·종류 동일) → 복원하지 않음, 확인 질문도 없음
	code, so, _ := ltRun("", "-out", out, list7, "h7")
	if code != 0 || !strings.Contains(so, "비교: 같음") || strings.Contains(so, "yes 입력") {
		t.Fatalf("code %d\n%s", code, lbMask(so))
	}
	if _, err := os.Stat(filepath.Join(e.hosts, "h7/restart.log")); err == nil {
		t.Fatal("같음인데 재시작")
	}

	// ldap_share_dir 미설정: auto_setup 과 같은 '수동 복원 필요' — 쓰지 않음
	e.mkHost(t, "t8", "Rocky Linux release 8.9 (x)", map[string]string{
		"etc/openldap/ldap.conf": lbConfU("svc_old", "o"), "etc/sssd/sssd.conf": "n\n", "etc/chrony.conf": "n\n"})
	ldap_share_dir = ""
	code, so, _ = ltRun("", "-y", "-out", out, list7, "t8")
	if code != 1 || !strings.Contains(so, "비교: 복원하지 않음 — "+ldapManualPrefix+"(ldap_share_dir 미설정, 백업: "+filepath.Join(out, "h7", "backup")+")") {
		t.Fatalf("code %d\n%s", code, lbMask(so))
	}
	if e.read(t, "t8", "etc/sssd/sssd.conf") != "n\n" {
		t.Fatal("수동 복원인데 파일이 바뀜")
	}

	// OS7 백업 → OS8 target, -y: 확인 없이 nslcd→sssd·ntp→chrony 변환 복원 (uid 같음 → 사유 'OS 종류 변경')
	ldap_share_dir = e.share
	code, so, se := ltRun("", "-y", "-out", out, list7, "t8")
	if code != 0 || strings.Contains(so, "yes 입력") {
		t.Fatalf("code %d\n%s%s", code, lbMask(so), lbMask(se))
	}
	for _, w := range []string{"비교: 복원 필요 — OS 종류 변경", "변환: 7→8 (nslcd.conf→sssd.conf,ntp.conf→chrony.conf)",
		"서비스 재시작: sssd ok, autofs ok, chronyd ok, nscd ok", "결과: 복원 완료"} {
		if !strings.Contains(so, w) {
			t.Fatalf("화면에 %q 없음:\n%s", w, lbMask(so))
		}
	}
	if e.read(t, "t8", "etc/sssd/sssd.conf") != ltConv(t, nslcdToSSSD, lbNslcd7) || e.read(t, "t8", "etc/chrony.conf") != ltConv(t, ntpToChrony, lbNtp7) {
		t.Fatal("sssd.conf·chrony.conf 가 변환 복원되지 않음")
	}
	if strings.Contains(so+se, lbSecret) {
		t.Fatal("화면에 bindpw")
	}
}
