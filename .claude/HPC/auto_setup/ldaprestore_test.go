// ldaprestore_test.go - LDAP 복원은 명령으로만 (Step 5): READY 직후 비교만(Probe)·복원 대기 표시, conf ldap_auto_restore,
// request ldap-restore 대상 선택(*·yml·호스트)·처리, CLI·os6 원격 중계 인자, TUI l 키 → 요청
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// recLdap: Probe·Restore 호출(호스트)을 기록. Restore 는 항상 적용 성공
type recLdap struct {
	probed, restored []string
}

func (l *recLdap) Backup(*Job, []string) map[string]LdapState { return nil }

func (l *recLdap) Probe(_ *Job, host string) LdapState {
	l.probed = append(l.probed, host)
	return pendingState(nil)
}

func (l *recLdap) Restore(_ *Job, host string) LdapState {
	l.restored = append(l.restored, host)
	return LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true}
}

func TestLdapAutoRestoreConf(t *testing.T) {
	saveConfVars(t)
	for _, c := range []struct {
		in   string
		want bool
	}{{"", false}, {"n", false}, {"no", false}, {"y", true}, {"YES", true}, {"1", true}, {"true", true}} {
		ldap_auto_restore = c.in
		if got := ldapAutoRestore(); got != c.want {
			t.Errorf("ldap_auto_restore=%q: %v, 기대 %v", c.in, got, c.want)
		}
	}
	ldap_auto_restore = ""
	loadConf([]string{writeConf(t, "ldap_auto_restore=y\n")})
	if !ldapAutoRestore() {
		t.Fatal("conf 에서 읽지 못함")
	}
}

// pending 상태: Bindpw=diff·Applied=false·머리말 → 비고는 '적용실패' 가 아니라 'LDAP 복원 대기'
func TestLdapPendingState(t *testing.T) {
	st := pendingState([]string{"OS 종류 변경", "변환 nslcd.conf→sssd.conf"})
	if !ldapPending(&st) || st.Reason != "복원 대기(명령 필요): OS 종류 변경; 변환 nslcd.conf→sssd.conf" {
		t.Fatalf("%+v", st)
	}
	if p := pendingState(nil); p.Reason != ldapPendingPrefix || p.Applied || p.Bindpw != BindpwDiff || p.Backup != LdapBackupOK {
		t.Fatalf("%+v", p)
	}
	for _, l := range []*LdapState{nil, {Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true, Reason: ldapPendingPrefix},
		{Backup: LdapBackupOK, Bindpw: BindpwDiff, Reason: "복원 실패: x"}, {Backup: LdapBackupOK, Bindpw: BindpwSame}} {
		if ldapPending(l) {
			t.Fatalf("복원 대기가 아님: %+v", l)
		}
	}
	n := hostNote(&Host{Route: "local", IP: "1.1.1.1", Ldap: &st})
	if !strings.Contains(n, "LDAP 복원 대기") || strings.Contains(n, "적용실패") {
		t.Fatalf("비고: %q", n)
	}
}

// 비교 전 실패는 unknown, 백업 읽기 실패는 Backup=ok 유지(확인실패), 백업 디렉터리 없음은 none('백업 파일 없음')
func TestCompareFailStates(t *testing.T) {
	e := lbSetup(t)
	j := lbBackup(t, e, "j", "h1", "Rocky Linux release 8.9 (x)", lbOS8Old)
	dir, _ := ldapHostDir("j", "h1")
	if err := os.WriteFile(filepath.Join(dir, lbMetaName), []byte("{깨짐"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := NewRealLdapBackup().Probe(j, "h1")
	if st.Backup != LdapBackupOK || st.Bindpw != BindpwUnknown || st.Applied || !strings.HasPrefix(st.Reason, "백업 읽기 실패: ") {
		t.Fatalf("meta 해석 실패: %+v", st)
	}
	if n := hostNote(&Host{Route: "local", IP: "1.1.1.1", Ldap: &st}); !strings.Contains(n, "LDAP 확인실패(백업 읽기 실패: ") || strings.Contains(n, "적용실패") {
		t.Fatalf("비고: %q", n)
	}
	os.RemoveAll(dir)
	st = NewRealLdapBackup().Probe(j, "h1")
	if st.Backup != LdapBackupNone || st.Reason != "백업 파일 없음" {
		t.Fatalf("백업 디렉터리 없음: %+v", st)
	}
}

// 비고 문구: 백업됨(비교 전)·확인실패·복원(재시작 실패·7→8/8→7 변환)·백업 대기(시도 전, 진행 중 호스트만)
func TestHostNoteLdapWording(t *testing.T) {
	cases := []struct {
		l    *LdapState
		want string
	}{
		{&LdapState{Backup: LdapBackupOK}, "LDAP 백업됨"},
		{&LdapState{Backup: LdapBackupOK, Bindpw: BindpwUnknown, Reason: "새 OS 무응답(수동 확인)"}, "LDAP 확인실패(새 OS 무응답)"},
		{&LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true}, "LDAP 복원"},
		{&LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true, Reason: "변환 nslcd.conf→sssd.conf"}, "LDAP 복원(7→8 변환)"},
		{&LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true, Reason: "OS 종류 변경; 변환 sssd.conf→nslcd.conf,chrony.conf→ntp.conf"}, "LDAP 복원(8→7 변환)"},
		{&LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true, Reason: "변환 ntp.conf→chrony.conf; 서비스 재시작 실패: sssd"}, "LDAP 복원(서비스 재시작 실패: sssd)"},
		{&LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Reason: "복원 실패: x"}, "LDAP 적용실패"},
	}
	for _, c := range cases {
		if n := hostNote(&Host{Route: "local", IP: "1.1.1.1", Ldap: c.l}); !strings.Contains(n, c.want) {
			t.Errorf("%+v → %q (want %q)", c.l, n, c.want)
		}
	}
	if n := hostNote(&Host{Route: "local", IP: "1.1.1.1"}); !strings.Contains(n, "LDAP 백업 대기") {
		t.Errorf("시도 전: %q", n)
	}
	if n := hostNote(&Host{Route: "local", IP: "1.1.1.1", Processed: "1234"}); strings.Contains(n, "LDAP") {
		t.Errorf("끝난 호스트: %q", n)
	}
}

// lrJob: 그룹 a.yml(p1 대기·s1 same·n1 대기지만 READY 전·nb 백업 없음), b.yml(p2 대기)
func lrJob(id string) *Job {
	pend := func() *LdapState { s := pendingState(nil); return &s }
	return &Job{ID: id, Hosts: map[string]*Host{
		"p1": {Route: "local", ReadyAt: base + 10, Ldap: pend()},
		"s1": {Route: "local", ReadyAt: base + 10, Ldap: &LdapState{Backup: LdapBackupOK, Bindpw: BindpwSame}},
		"n1": {Route: "local", Ldap: pend()},
		"nb": {Route: "local", ReadyAt: base + 10, Ldap: &LdapState{Backup: LdapBackupNone, Reason: "전달 시점 ping X"}},
		"p2": {Route: "local", ReadyAt: base + 10, Ldap: pend()},
	}, Groups: map[string]*Group{"a.yml": {Hosts: []string{"p1", "s1", "n1", "nb"}}, "b.yml": {Hosts: []string{"p2"}}}}
}

func lrDaemon(l LdapBackup, j *Job) *Daemon {
	d := &Daemon{Ldap: l}
	d.init()
	d.jobs[j.ID] = j
	return d
}

// 대상 선택: '*'·yml 은 READY 를 거친 백업 ok 호스트 중 복원 대기만, 호스트 지정은 다시 비교 후 복원(same 이어도 호출)
func TestLdapRestoreTargets(t *testing.T) {
	setupDir(t)
	for _, c := range []struct {
		target, reason string
		want           []string
	}{
		{"*", "", []string{"p1", "p2"}},
		{"a.yml", "", []string{"p1"}},
		{"b.yml", "", []string{"p2"}},
		{"s1", "", []string{"s1"}},
		{"p1", "", []string{"p1"}},
		{"n1", "복원 대상 아님", nil},
		{"nb", "복원 대상 아님", nil},
		{"zz", "그룹·호스트 없음", nil},
		{"", "jobid/대상 없음", nil},
	} {
		l := &recLdap{}
		j := lrJob("J1")
		d := lrDaemon(l, j)
		r := d.ldapRestoreReq("J1", c.target)
		if (c.reason == "") != (r == "") || !strings.Contains(r, c.reason) {
			t.Errorf("%q: 사유 %q, 기대 %q", c.target, r, c.reason)
		}
		if !reflect.DeepEqual(l.restored, c.want) || len(l.probed) != 0 {
			t.Errorf("%q: 복원 %v, 기대 %v (probe %v)", c.target, l.restored, c.want, l.probed)
		}
		for _, h := range c.want {
			if !j.Hosts[h].Ldap.Applied {
				t.Errorf("%q: %s 결과가 반영되지 않음: %+v", c.target, h, j.Hosts[h].Ldap)
			}
		}
		if len(c.want) > 0 && !d.dirty["J1"] {
			t.Errorf("%q: dirty 아님", c.target)
		}
	}
	// 복원 대기가 없는 그룹·이미 복원된 뒤 '*' → 거부(호출 없음)
	l := &recLdap{}
	j := lrJob("J1")
	d := lrDaemon(l, j)
	if r := d.ldapRestoreReq("J1", "*"); r != "" {
		t.Fatal(r)
	}
	if r := d.ldapRestoreReq("J1", "*"); r != "복원 대기 호스트 없음" || len(l.restored) != 2 {
		t.Fatalf("두 번째 '*': %q %v", r, l.restored)
	}
	// job 없음·LDAP 꺼짐(nop)
	if r := d.ldapRestoreReq("NOJOB", "*"); r != "job 없음" {
		t.Fatalf("job 없음: %q", r)
	}
	if r := lrDaemon(nopLdap{}, lrJob("J2")).ldapRestoreReq("J2", "*"); !strings.Contains(r, "꺼져") {
		t.Fatalf("nop: %q", r)
	}
}

// 실행 중인 run 에 들어 있는 호스트는 건드리지 않는다
func TestLdapRestoreSkipsBusy(t *testing.T) {
	setupDir(t)
	l := &recLdap{}
	d := lrDaemon(l, lrJob("J1"))
	d.runs[1] = &inflight{jobID: "J1", hosts: []string{"p1"}}
	if r := d.ldapRestoreReq("J1", "a.yml"); !strings.Contains(r, "실행 중") || len(l.restored) != 0 {
		t.Fatalf("%q %v", r, l.restored)
	}
	if r := d.ldapRestoreReq("J1", "*"); r != "" || !reflect.DeepEqual(l.restored, []string{"p2"}) {
		t.Fatalf("%q %v", r, l.restored)
	}
}

// 요청 큐: request ldap-restore 를 데몬이 수거해 처리, 거부는 rejected/ 에 사유
func TestLdapRestoreRequestQueue(t *testing.T) {
	setupDir(t)
	l := &recLdap{}
	d := lrDaemon(l, lrJob("J1"))
	if _, err := writeRequest(ReqLdapRestore, map[string]string{"jobid": "J1", "yml": "b.yml"}); err != nil {
		t.Fatal(err)
	}
	if _, err := writeRequest(ReqLdapRestore, map[string]string{"jobid": "J1", "yml": "nb"}); err != nil {
		t.Fatal(err)
	}
	d.collectRequests(time.Unix(base+100, 0))
	if !reflect.DeepEqual(l.restored, []string{"p2"}) {
		t.Fatalf("복원 호출: %v", l.restored)
	}
	if n := listReqFiles(requestsDir()); len(n) != 0 {
		t.Fatalf("남은 요청: %v", n)
	}
	rej := listReqFiles(reqRejectedDir())
	if len(rej) != 1 {
		t.Fatalf("거부 요청: %v", rej)
	}
	b, _ := os.ReadFile(filepath.Join(reqRejectedDir(), rej[0]))
	if pl := parseRequest(b); !strings.Contains(pl["reason"], "복원 대상 아님") {
		t.Fatalf("거부 사유: %q", b)
	}
}

// CLI·os6 원격 중계: request ldap-restore <jobid> <호스트|yml|*>
func TestLdapRestoreRequestArgs(t *testing.T) {
	if !requestArgsOK([]string{ReqLdapRestore, "J1", "*"}) || requestArgsOK([]string{ReqLdapRestore, "J1"}) ||
		requestArgsOK([]string{ReqLdapRestore, "J1", "h1", "x"}) || !relayArgsOK([]string{"request", ReqLdapRestore, "J1", "h1"}) {
		t.Fatal("requestArgsOK ldap-restore")
	}
	a, err := requestArgs(ReqLdapRestore, map[string]string{"jobid": "J1", "yml": "h1"})
	if err != nil || strings.Join(a, " ") != "request ldap-restore J1 h1" {
		t.Fatalf("requestArgs: %v %v", a, err)
	}
	setupDir(t)
	if rc := cmdRequest([]string{ReqLdapRestore, "J1", "a.yml"}); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	names := listReqFiles(requestsDir())
	sort.Strings(names)
	if len(names) != 1 || !strings.HasSuffix(names[0], "_ldap-restore.req") {
		t.Fatalf("요청 파일: %v", names)
	}
	b, _ := os.ReadFile(filepath.Join(requestsDir(), names[0]))
	if pl := parseRequest(b); pl["jobid"] != "J1" || pl["yml"] != "a.yml" {
		t.Fatalf("payload: %v", pl)
	}
}

// TUI: 화면 2 에서 l → 확인 → y(선택 호스트) / a(그룹의 복원 대기) 요청, 그 밖의 키는 취소. 복원 대기가 있으면 키 안내에 l
func TestLdapRestoreKeyTUI(t *testing.T) {
	pend := pendingState(nil)
	p := tsHost("a1", StageDone, 10, "LDAP 복원 대기")
	p.Ldap = &pend
	q := tsHost("a2", StageDone, 10, "LDAP 복원 대기")
	q.Ldap = &pend
	snap := tsSnap(true, tsJob("J1", "user1", false, tsGroup("a.yml", "ib", "rhel8", "pxe", "y", p, q)))
	// y = 선택 호스트(첫 행 a1)
	src := &fakeSrc{snap: snap, local: true}
	r := runKeys(t, src, "\rly")
	if len(src.reqs) != 1 || src.reqs[0].kind != ReqLdapRestore || src.reqs[0].payload["jobid"] != "J1" || src.reqs[0].payload["yml"] != "a1" {
		t.Fatalf("y 요청: %+v", src.reqs)
	}
	out := r.out.String()
	for _, want := range []string{"l LDAP 복원(대기 2)", "[y] a1", "[a]", "LDAP 복원을 요청했습니다"} {
		if !strings.Contains(out, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
	// a = 그룹, h 메뉴의 l 도 같은 동작
	src = &fakeSrc{snap: snap, local: true}
	r = runKeys(t, src, "\rhla")
	if len(src.reqs) != 1 || src.reqs[0].payload["yml"] != "a.yml" || !strings.Contains(r.out.String(), "l LDAP 복원") {
		t.Fatalf("a 요청: %+v", src.reqs)
	}
	// 전체 보기에서 a = job 전체("*")
	src = &fakeSrc{snap: snap, local: true}
	runKeys(t, src, "\ralA")
	if len(src.reqs) != 1 || src.reqs[0].payload["yml"] != "*" {
		t.Fatalf("전체 보기 a 요청: %+v", src.reqs)
	}
	// 그 밖의 키는 취소
	src = &fakeSrc{snap: snap, local: true}
	runKeys(t, src, "\rln")
	if len(src.reqs) != 0 {
		t.Fatalf("n 인데 요청됨: %+v", src.reqs)
	}
	// 백업 없음·복원 대기 없음 → 안내만, 키 안내에 l 없음
	src = &fakeSrc{snap: twoGroupSnap(), local: true}
	r = runKeys(t, src, "\rly")
	if len(src.reqs) != 0 || !strings.Contains(r.out.String(), "LDAP 복원 대상이 없습니다") || strings.Contains(r.out.String(), "l LDAP 복원(대기") {
		t.Fatalf("대상 없음: %+v", src.reqs)
	}
	// 복원 대기가 없으면 h 메뉴는 종전과 같다 (l 항목 없음)
	src = &fakeSrc{snap: twoGroupSnap(), local: true}
	if r = runKeys(t, src, "\rh"); strings.Contains(r.out.String(), "l LDAP 복원") {
		t.Fatal("복원 대기 없음: h 메뉴에 l 이 없어야 함")
	}
}
