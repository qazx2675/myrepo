package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// 두 번째 계정(user2) 벤더별 권한(user2_role_<vendor>)과 이미 있는 user2 의 권한(RoleId) 변경 시험. mockbmc 만 쓴다.

const acctColl = "/redfish/v1/AccountService/Accounts"

// patchCalls 는 mock 이 받은 계정 PATCH 들입니다.
func patchCalls(s *mockbmc.Server) []mockbmc.Call {
	var out []mockbmc.Call
	for _, c := range s.Calls() {
		if c.Method == "PATCH" {
			out = append(out, c)
		}
	}
	return out
}

func TestParseConfUser2VendorRoles(t *testing.T) {
	c, err := parseConf("user=a\nuser2=b\nuser2_role=Operator\nUSER2_ROLE_HPE = administrator\nuser2_role_cray=readonly\n"+
		"user2_role_dell=ReadOnly\nuser2_role_dell=\n", "bios.conf")
	must(t, err)
	if len(c.User2Roles) != 2 || c.User2Roles["hpe"] != roleAdministrator || c.User2Roles["cray"] != roleReadOnly {
		t.Errorf("User2Roles %v", c.User2Roles)
	}
	for v, want := range map[string]string{
		"hpe": roleAdministrator, "HPE": roleAdministrator, "Hewlett Packard Enterprise": roleAdministrator, "Hp": roleAdministrator,
		"Cray Inc.": roleReadOnly, "Dell": roleOperator, "": roleOperator, "Fujitsu": roleOperator,
	} {
		if got := c.user2RoleFor(v); got != want {
			t.Errorf("user2RoleFor(%q) = %q, 기대 %q", v, got, want)
		}
	}
	if got := c.user2RolesText(); got != "Operator, hpe=Administrator, cray=ReadOnly" {
		t.Errorf("user2RolesText %q", got)
	}
	// user2 없이 벤더별 값만 있어도 user2_role 만 있을 때처럼 오류가 아니다.
	if _, err := parseConf("user=a\nuser2_role_hpe=Operator\n", "bios.conf"); err != nil {
		t.Errorf("user2 없는 벤더별 값: %v", err)
	}
	cases := []struct{ name, text, want string }{
		{"모르는 벤더", "user=a\nuser2=b\nuser2_role=Operator\nuser2_role_ibm=Operator\n", "bios.conf:4: user2_role_ibm 의 벤더는 dell, hpe, lenovo, supermicro, cisco, cray 중 하나"},
		{"벤더 빈칸", "user=a\nuser2_role_=Operator\n", "bios.conf:2: user2_role_ 의 벤더는"},
		{"권한 값", "user=a\nuser2=b\nuser2_role=Operator\nuser2_role_hpe=root\n", "bios.conf:4: user2_role_hpe 은 Administrator, Operator, ReadOnly 중 하나"},
		{"기본 권한 필수", "user=a\nuser2=b\nuser2_role_hpe=Operator\n", "user2 를 쓰려면 user2_role"},
	}
	for _, tc := range cases {
		_, err := parseConf(tc.text, "bios.conf")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: 오류 %v 에 %q 없음", tc.name, err, tc.want)
		}
	}
}

func TestUser2RoleForVendor(t *testing.T) {
	env := acctEnvFor(t, "hpe")
	env.conf.User2Roles = map[string]string{"hpe": roleAdministrator, "cisco": roleReadOnly}
	sp := acctSpec{User: user2Name, Role: roleOperator, Second: true}
	for v, want := range map[string]string{
		"hpe": roleAdministrator, "HPE": roleAdministrator, "cisco": roleReadOnly, "Cisco": roleReadOnly,
		"dell": roleOperator, "": roleOperator, "Fujitsu": roleOperator,
	} {
		if got := env.roleFor(sp, v); got != want {
			t.Errorf("roleFor(%q) = %q, 기대 %q", v, got, want)
		}
	}
	if got := env.roleFor(acctSpec{User: testUser, Role: roleAdministrator}, "cisco"); got != roleAdministrator {
		t.Errorf("1순위 계정 권한이 벤더별 값으로 바뀜: %q", got)
	}
	// 같은 user2 라도 권한이 다르면 확인 질문에서 따로 센다.
	mk := func(role, st string) *acctHost {
		h := newCreateHosts(Target{Hostname: "h"}, []acctSpec{sp})[0]
		h.Role, h.Status = role, st
		return h
	}
	g := [][]*acctHost{{mk(roleOperator, acctNeedCreate)}, {mk(roleAdministrator, acctNeedCreate)}, {mk(roleOperator, acctNeedCreate)}, {mk(roleAdministrator, acctNeedRoleUpdate)}}
	if got := createWhat(g); got != "ops1(Operator) 2대, ops1(Administrator) 1대, ops1 권한 변경(Administrator) 1대" {
		t.Errorf("createWhat %q", got)
	}
}

// HPE 호스트에는 user2_role_hpe 로 만든다 (spec 의 기본 권한은 그대로).
func TestCreateGroupUser2VendorRole(t *testing.T) {
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	env := acctEnvFor(t, "hpe")
	env.conf.User2Roles = map[string]string{"hpe": roleAdministrator, "dell": roleReadOnly}
	twoSpecs(env, roleOperator)
	hs := newCreateHosts(acctTarget(t, s), env.accounts())
	env.createGroup(hs)
	if hs[0].Status != acctCreated || hs[1].Status != acctCreated {
		t.Fatalf("status %s/%s (%s)", hs[0].Status, hs[1].Status, hs[1].Detail)
	}
	if a, _ := findMockAcct(s, user2Name); a.RoleID != "Administrator" {
		t.Errorf("user2 권한 %q (기대 Administrator)", a.RoleID)
	}
	if r := hs[1].row(); r[len(r)-1] != roleAdministrator || hs[0].row()[len(r)-1] != roleAdministrator {
		t.Errorf("role 열 %v / %v", hs[0].row(), r)
	}
	if !strings.Contains(hs[1].label(), "("+user2Name+"/Administrator)") || env.accounts()[1].Role != roleOperator {
		t.Errorf("label %q, spec 권한 %q", hs[1].label(), env.accounts()[1].Role)
	}
}

// roleSeed 는 1순위 계정 + 다른 계정 + 권한이 role 인 user2 입니다 (user2 는 3번 슬롯).
func roleSeed(role string) []mockbmc.Account {
	return []mockbmc.Account{
		{UserName: testUser, Password: testPass, RoleID: "Administrator", Enabled: true},
		{UserName: "keep", Password: "k", RoleID: "ReadOnly", Enabled: true},
		{UserName: user2Name, Password: "other-pw", RoleID: role, Enabled: true},
	}
}

// 이미 있는 user2 의 권한이 다르면 그 계정의 RoleId 만 PATCH 하고 다시 읽어 확인한다 (다른 계정·비밀번호는 그대로).
func TestUpdateRoleExistingUser2(t *testing.T) {
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: roleSeed("ReadOnly")})
	env := acctEnvFor(t, "hpe")
	twoSpecs(env, roleOperator)
	hs := env.probeCreateAll(acctTarget(t, s))
	if hs[0].Status != acctPrimaryOK || hs[1].Status != acctNeedRoleUpdate || hs[1].curRole != "ReadOnly" {
		t.Fatalf("점검 %s / %s (%q)", hs[0].Status, hs[1].Status, hs[1].curRole)
	}
	need := needCreateGroups([][]*acctHost{hs})
	if len(need) != 1 || len(need[0]) != 1 || createWhat(need) != user2Name+" 권한 변경(Operator) 1대" {
		t.Fatalf("대상 %v / %q", need, createWhat(need))
	}
	var b bytes.Buffer
	printAcctTargets(&b, flattenAcct(need), 10)
	if !strings.Contains(b.String(), "권한 변경 ReadOnly → Operator") {
		t.Errorf("대상 목록: %s", b.String())
	}
	if len(patchCalls(s)) != 0 {
		t.Fatal("점검 단계에서 PATCH 가 나감")
	}
	env.createGroup(need[0])
	h := need[0][0]
	if h.Status != acctRoleUpdated || h.Rollback != "-" {
		t.Fatalf("status %s (%s)", h.Status, h.Detail)
	}
	pc := patchCalls(s)
	if len(pc) != 1 || pc[0].Path != acctColl+"/3" || pc[0].Body != `{"RoleId":"Operator"}` {
		t.Errorf("PATCH %+v", pc)
	}
	acs := s.Accounts()
	if acs[2].RoleID != "Operator" || acs[2].Password != "other-pw" || !acs[2].Enabled || acs[2].UserName != user2Name {
		t.Errorf("user2 %+v", acs[2])
	}
	if acs[0].RoleID != "Administrator" || acs[1].RoleID != "ReadOnly" || acs[1].Password != "k" {
		t.Errorf("다른 계정이 바뀜 %+v", acs)
	}
	if s.Count("POST", acctColl) != 0 || s.Count("DELETE", acctColl) != 0 {
		t.Error("권한 변경에서 생성·삭제가 나감")
	}
	var rep bytes.Buffer
	writeAcctReport(&rep, need[0], 20, painter{})
	if !strings.Contains(rep.String(), "["+acctRoleUpdated+"] 1대") || !strings.Contains(rep.String(), "("+user2Name+"/Operator)") ||
		!strings.Contains(rep.String(), "권한 ReadOnly → Operator") {
		t.Errorf("요약:\n%s", rep.String())
	}
}

// 다시 읽은 권한이 바뀌지 않았으면 이전 RoleId 로 되돌리고 실패로 남긴다.
func TestUpdateRoleVerifyFailRestores(t *testing.T) {
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: roleSeed("ReadOnly"), IgnoreRoleChange: true})
	env := acctEnvFor(t, "hpe")
	twoSpecs(env, roleOperator)
	hs := newCreateHosts(acctTarget(t, s), env.accounts()[1:])
	env.createGroup(hs)
	if hs[0].Status != acctRoleUpdateFail || hs[0].Rollback != "완료" || !strings.Contains(hs[0].Detail, "이전 권한 ReadOnly 으로 되돌림") {
		t.Fatalf("status %s/%s (%s)", hs[0].Status, hs[0].Rollback, hs[0].Detail)
	}
	pc := patchCalls(s)
	if len(pc) != 2 || pc[0].Body != `{"RoleId":"Operator"}` || pc[1].Body != `{"RoleId":"ReadOnly"}` || pc[1].Path != acctColl+"/3" {
		t.Errorf("PATCH %+v", pc)
	}
	if a, _ := findMockAcct(s, user2Name); a.RoleID != "ReadOnly" || a.Password != "other-pw" {
		t.Errorf("user2 %+v", a)
	}
}

// 같은 권한이면 SKIP_EXISTS (PATCH 없음), 허용 범위 밖 권한은 바꾸지 않음, dry-run 은 계획만.
func TestUpdateRoleSkipAndDryRun(t *testing.T) {
	for _, tc := range []struct {
		name, seedRole string
		dry, asUser2   bool
		want, detail   string
	}{
		{"같은 권한", "Operator", false, false, acctExists, "덮어쓰지 않음"},
		{"범위 밖 권한", "Supervisor", false, false, acctExists, "계열이 아니라 권한을 바꾸지 않음"},
		{"로그인 계정이 user2", "ReadOnly", false, true, acctExists, "로그인한 관리자 계정과 같아"},
		{"dry-run", "ReadOnly", true, false, acctWouldRoleUpdate, `ReadOnly → Operator: PATCH ` + acctColl + `/3 {"RoleId":"Operator"}`},
	} {
		s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: roleSeed(tc.seedRole)})
		env := acctEnvFor(t, "hpe")
		env.dryRun = tc.dry
		if tc.asUser2 {
			env.admin = &fallbackAcct{user: user2Name, pass: "other-pw"}
		}
		twoSpecs(env, roleOperator)
		hs := newCreateHosts(acctTarget(t, s), env.accounts()[1:])
		env.createGroup(hs)
		if hs[0].Status != tc.want || !strings.Contains(hs[0].Detail, tc.detail) {
			t.Errorf("%s: %s (%s)", tc.name, hs[0].Status, hs[0].Detail)
		}
		if n := len(patchCalls(s)); n != 0 {
			t.Errorf("%s: PATCH %d 건", tc.name, n)
		}
		if a, _ := findMockAcct(s, user2Name); a.RoleID != tc.seedRole {
			t.Errorf("%s: 권한이 바뀜 %q", tc.name, a.RoleID)
		}
	}
}

// 권한 변경 등록(AllowAccountRoleTarget)은 그 멤버에 {"RoleId": R}(허용 범위)만 허용하고, 그 본문은 등록한 멤버에만 보낸다.
func TestAccountRoleTargetGate(t *testing.T) {
	good := []string{`{"RoleId":"Operator"}`, `{"RoleId":"read-only"}`, `{"RoleId":"Admin"}`}
	bad := []string{`{"RoleId":"None"}`, `{"RoleId":"Supervisor"}`, `{"RoleId":""}`, `{"RoleId":"Operator","Enabled":true}`,
		`{"Password":"x"}`, `{"Locked":false}`, `{}`, `{"RoleId":1}`}
	for _, b := range good {
		if _, err := checkAccountRolePatchBody([]byte(b)); err != nil {
			t.Errorf("거부됨: %s: %v", b, err)
		}
	}
	for _, b := range bad {
		if _, err := checkAccountRolePatchBody([]byte(b)); err == nil {
			t.Errorf("허용됨: %s", b)
		}
	}

	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: roleSeed("ReadOnly")})
	c := NewClient(ClientOpts{BaseURL: s.URL(), User: fbUser, Pass: fbPass, Insecure: true, Mode: ModeAccount, Gap: -1, Timeout: 5 * time.Second})
	defer c.Close()
	m2, m3 := acctColl+"/2", acctColl+"/3"
	must(t, c.AllowAccountRoleTarget(m3))
	for _, b := range []map[string]interface{}{{"Password": "x"}, {"RoleId": "None"}, {"RoleId": "Supervisor"},
		{"RoleId": "Operator", "Enabled": true}, {"Enabled": false, "RoleId": "None"}, {"UserName": ""}} {
		if _, _, err := c.AccountPatch(m3, b); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("권한 등록 멤버에 %v 가 막히지 않음: %v", b, err)
		}
	}
	if _, _, err := c.AccountDelete(m3); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("권한 등록 멤버 DELETE 가 막히지 않음: %v", err)
	}
	must(t, c.AllowAccountTarget(m2)) // 생성·슬롯용 등록으로는 RoleId PATCH 를 보낼 수 없다
	if _, _, err := c.AccountPatch(m2, map[string]interface{}{"RoleId": "Operator"}); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("권한 등록하지 않은 멤버에 RoleId PATCH 가 막히지 않음: %v", err)
	}
	if len(patchCalls(s)) != 0 || s.Count("DELETE", acctColl) != 0 {
		t.Fatalf("막혀야 할 쓰기가 BMC 에 도달함 %+v", patchCalls(s))
	}
	if _, _, err := c.AccountPatch(m3, map[string]interface{}{"RoleId": "Operator"}); err != nil {
		t.Fatalf("허용 형태가 거부됨: %v", err)
	}
	if acs := s.Accounts(); acs[2].RoleID != "Operator" || acs[1].RoleID != "ReadOnly" {
		t.Errorf("계정 %+v", acs)
	}
	ro := NewClient(ClientOpts{BaseURL: s.URL(), User: fbUser, Pass: fbPass, Insecure: true, Mode: ModeReadOnly, Gap: -1})
	defer ro.Close()
	if err := ro.AllowAccountRoleTarget(m3); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("읽기 전용 Client 가 권한 변경 등록을 받음: %v", err)
	}
}

// 점검 끝 제안: 1순위로 접속했고 user2 가 있는데 권한이 user2_role_hpe 와 다르면 확인 후 권한만 바꾼다 (dry-run 은 계획만).
func TestOfferAccountCreateUser2RoleUpdate(t *testing.T) {
	fastCheck(t)
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: roleSeed("Operator")})
	rc := checkRCFor(t, 1, "", s.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	withUser2(t, rc) // 기본 Operator
	rc.Conf.User2Roles = map[string]string{"hpe": roleAdministrator}
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow, ProbeUser2: true})
	must(t, err)
	if h := run.Hosts[0]; h.user2 != acctPresent || h.user2Role != "Operator" || h.CredSource != credPrimary {
		t.Fatalf("점검 user2 %q %q %s", h.user2, h.user2Role, h.CredSource)
	}

	var dry bytes.Buffer
	must(t, offerAccountCreate(rc, run, true, false, false, feedStdin(t, ""), &dry, reportOpts{}))
	if len(patchCalls(s)) != 0 || !strings.Contains(dry.String(), "["+acctWouldRoleUpdate+"] 1대") {
		t.Fatalf("dry-run 에 PATCH 가 나가거나 계획이 없음:\n%s", dry.String())
	}

	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, false, false, false, feedStdin(t, "\n"), &out, reportOpts{}))
	o := out.String()
	if a, _ := findMockAcct(s, user2Name); a.RoleID != "Administrator" || a.Password != "other-pw" {
		t.Errorf("user2 %+v\n%s", a, o)
	}
	pc := patchCalls(s)
	if len(pc) != 1 || pc[0].Body != `{"RoleId":"Administrator"}` || pc[0].Path != acctColl+"/3" {
		t.Errorf("PATCH %+v", pc)
	}
	for _, want := range []string{"권한이 설정(Operator, hpe=Administrator)과 다른 호스트 1대", "권한 변경 Operator → Administrator",
		"계정을 만들거나 권한을 바꿀까요? [" + user2Name + " 권한 변경(Administrator) 1대]", "[" + acctRoleUpdated + "] 1대"} {
		if !strings.Contains(o, want) {
			t.Errorf("출력에 %q 없음:\n%s", want, o)
		}
	}
	tsv, err := os.ReadFile(filepath.Join(run.Dir, "account_result.tsv"))
	must(t, err)
	if !strings.Contains(string(tsv), "\taccount\trole\n") || !strings.Contains(string(tsv), acctRoleUpdated) ||
		!strings.Contains(string(tsv), "\t"+user2Name+"\t"+roleAdministrator+"\n") {
		t.Errorf("account_result.tsv:\n%s", tsv)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "recheck")); !os.IsNotExist(err) {
		t.Error("user2 권한만 바꿨는데 재점검함")
	}
	noSecretsUnder(t, run.Dir, o)
}

// UCSM: user2 역할은 user2_role_cisco(없으면 user2_role)를 UCSM 역할 이름으로 바꿔 쓴다.
func TestUcsmUser2Role(t *testing.T) {
	for role, want := range map[string]string{roleAdministrator: "admin", roleOperator: "operations", roleReadOnly: "read-only"} {
		if got := ucsmRole(role); got != want {
			t.Errorf("ucsmRole(%s) = %q", role, got)
		}
	}
	env := acctEnvFor(t, "cisco")
	twoSpecs(env, roleOperator)
	env.conf.User2Roles = map[string]string{"cisco": roleReadOnly, "hpe": roleAdministrator}
	hs := env.ucsmProbe(Target{Hostname: "ucsm1", Err: StatusUnreachable})
	if hs[0].acctRole() != roleAdministrator || hs[1].acctRole() != roleReadOnly || ucsmRole(env.specOf(hs[1]).Role) != "read-only" {
		t.Errorf("UCSM 권한 %s / %s", hs[0].acctRole(), hs[1].acctRole())
	}
	env.conf.User2Roles = nil
	if hs = env.ucsmProbe(Target{Hostname: "ucsm1", Err: StatusUnreachable}); ucsmRole(env.specOf(hs[1]).Role) != "operations" {
		t.Errorf("기본 권한 %s", hs[1].acctRole())
	}
}
