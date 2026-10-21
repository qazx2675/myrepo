package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// 두 번째 공용 계정(bios.conf user2 + pass2.enc, user2_role) 시험. 실제 BMC 없이 mockbmc 계정 서비스만 쓴다.

const (
	user2Name = "ops1"
	user2Pass = `0ps!Pa#2<&>`
)

// twoSpecs 는 1순위(관리자) + 두 번째 계정(role) 입니다.
func twoSpecs(env *acctEnv, role string) {
	env.specs = []acctSpec{
		{User: testUser, Pass: testPass, Role: roleAdministrator},
		{User: user2Name, Pass: user2Pass, Role: role, Second: true},
	}
}

func TestCreateGroupMakesBothAccounts(t *testing.T) {
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	env := acctEnvFor(t, "hpe")
	twoSpecs(env, roleOperator)
	hs := newCreateHosts(acctTarget(t, s), env.accounts())
	env.createGroup(hs)
	for _, h := range hs {
		if h.Status != acctCreated {
			t.Fatalf("%s: status %s (%s)", h.User, h.Status, h.Detail)
		}
	}
	a1, ok1 := findMockAcct(s, testUser)
	a2, ok2 := findMockAcct(s, user2Name)
	if !ok1 || a1.RoleID != "Administrator" || !ok2 || a2.RoleID != "Operator" || a2.Password != user2Pass {
		t.Fatalf("만든 계정 %+v / %+v", a1, a2)
	}
	// admin 로그인 1회 + 새 계정 검증 2회 = 세션 3개
	if n := s.Count("POST", "/redfish/v1/SessionService/Sessions"); n != 3 {
		t.Errorf("세션 생성 %d회 (기대 3)", n)
	}
	row := strings.Join(hs[0].row(), "\t") + strings.Join(hs[1].row(), "\t")
	for _, secret := range []string{testPass, user2Pass, fbPass} {
		if strings.Contains(row, secret) {
			t.Errorf("결과 행에 비밀 값 노출: %q", secret)
		}
	}
	if hs[1].row()[len(acctResultHeader)-1] != user2Name || hs[1].name() != hs[1].Target.Hostname+"("+user2Name+")" {
		t.Errorf("account 열/이름: %v %q", hs[1].row(), hs[1].name())
	}
}

// Dell 슬롯 방식: 두 계정이 서로 다른 빈 슬롯을 쓴다 (앞 계정이 쓴 슬롯을 다시 읽어 피함).
func TestCreateGroupSlotStyleUsesTwoSlots(t *testing.T) {
	slots := []mockbmc.Account{{}, {UserName: "root", Password: "calvin", RoleID: "Administrator", Enabled: true}, {}, {}}
	s := acctMock(t, "dell-r660", &mockbmc.AccountOptions{Slots: slots, SlotStyle: true})
	env := acctEnvFor(t, "dell")
	twoSpecs(env, roleReadOnly)
	hs := newCreateHosts(acctTarget(t, s), env.accounts())
	env.createGroup(hs)
	if hs[0].Status != acctCreated || hs[1].Status != acctCreated {
		t.Fatalf("status %s/%s (%s / %s)", hs[0].Status, hs[1].Status, hs[0].Detail, hs[1].Detail)
	}
	acs := s.Accounts()
	if acs[2].UserName != testUser || acs[3].UserName != user2Name || acs[3].RoleID != "ReadOnly" {
		t.Errorf("슬롯 사용이 틀림: %+v", acs)
	}
}

// 점검: 두 번째 계정은 로그인하지 않고 목록으로만 본다 (있으면 PRESENT, 없으면 NEED_CREATE). 쓰기 없음.
func TestProbeSecondAccountByList(t *testing.T) {
	seed := []mockbmc.Account{{UserName: testUser, Password: testPass, RoleID: "Administrator", Enabled: true}}
	env := acctEnvFor(t, "hpe")
	twoSpecs(env, roleOperator)

	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: seed})
	hs := env.probeCreateAll(acctTarget(t, s))
	if hs[0].Status != acctPrimaryOK || hs[1].Status != acctNeedCreate {
		t.Errorf("user2 없음: %s / %s (%s)", hs[0].Status, hs[1].Status, hs[1].Detail)
	}
	seed2 := append(seed, mockbmc.Account{UserName: user2Name, Password: "different", RoleID: "Operator", Enabled: true})
	s2 := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: seed2})
	hs2 := env.probeCreateAll(acctTarget(t, s2))
	if hs2[0].Status != acctPrimaryOK || hs2[1].Status != acctPresent {
		t.Errorf("user2 있음: %s / %s", hs2[0].Status, hs2[1].Status)
	}
	for _, m := range []*mockbmc.Server{s, s2} {
		if m.Count("POST", "/redfish/v1/AccountService/Accounts") != 0 {
			t.Error("점검 단계에서 쓰기가 나감")
		}
	}
	// 1순위 계정은 있고 두 번째만 없으면 그 칸만 생성 대상이 된다.
	need := needCreateGroups([][]*acctHost{hs, hs2})
	if len(need) != 1 || len(need[0]) != 1 || need[0][0].User != user2Name {
		t.Errorf("생성 대상 %v", need)
	}
	if got := createWhat(need); got != user2Name+"(Operator) 1대" {
		t.Errorf("createWhat %q", got)
	}
}

// 점검 끝 제안: user2 가 conf 에 있으면 pass2.enc 를 읽어 같은 호스트에 두 계정을 만든다.
func TestOfferAccountCreateWithUser2(t *testing.T) {
	fastCheck(t)
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	rc := checkRCFor(t, 1, "", s.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	dir := t.TempDir()
	rc.Conf.KeyFile, rc.Conf.Pass2File = filepath.Join(dir, "key.bin"), filepath.Join(dir, "pass2.enc")
	rc.Conf.User2, rc.Conf.User2Role = user2Name, roleReadOnly
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)

	old := stdinIsTerminal
	stdinIsTerminal = func(*os.File) bool { return true }
	defer func() { stdinIsTerminal = old }()
	feed := func() *os.File {
		pr, pw, err := os.Pipe()
		must(t, err)
		_, _ = pw.WriteString("\n")
		pw.Close()
		return pr
	}
	// pass2.enc 가 없으면 만들지 않고 안내만 (오류로 끝내지 않음).
	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, false, false, false, feed(), &out, reportOpts{}))
	if len(s.Accounts()) != 0 || !strings.Contains(out.String(), "encrypt.sh 2") {
		t.Fatalf("pass2.enc 없음: %s", out.String())
	}

	must(t, encryptFromReader(strings.NewReader(user2Pass), rc.Conf.Pass2File, rc.Conf.KeyFile))
	out.Reset()
	must(t, offerAccountCreate(rc, run, false, false, false, feed(), &out, reportOpts{}))
	a2, ok := findMockAcct(s, user2Name)
	if _, ok1 := findMockAcct(s, testUser); !ok1 || !ok || a2.RoleID != "ReadOnly" || a2.Password != user2Pass {
		t.Fatalf("두 계정이 만들어지지 않음 %+v\n%s", s.Accounts(), out.String())
	}
	if !strings.Contains(out.String(), user2Name+"(ReadOnly) 1대") {
		t.Errorf("확인 질문에 두 번째 계정이 없음:\n%s", out.String())
	}
	b, err := os.ReadFile(filepath.Join(run.Dir, "account_result.tsv"))
	must(t, err)
	if strings.Count(string(b), acctCreated) != 2 || strings.Contains(string(b), user2Pass) || strings.Contains(string(b), testPass) {
		t.Errorf("account_result.tsv: %s", b)
	}
}

func TestPlanCreateRoleCandidates(t *testing.T) {
	env := acctEnvFor(t, "cisco")
	sp := acctSpec{User: "u", Pass: "p", Role: roleOperator}
	p := env.planCreate(sp, "Cisco", "/redfish/v1/AccountService/Accounts", nil, "")
	if len(p.steps) != 2 || p.steps[0].body["RoleId"] != "user" || p.steps[1].body["RoleId"] != "Operator" {
		t.Errorf("Cisco Operator 후보 %v", p.steps)
	}
	// HPE iLO4 Oem 권한은 권한에 맞게 줄어든다 (읽기 전용 = 로그인만).
	p = env.planCreate(acctSpec{User: "u", Pass: "p", Role: roleReadOnly}, "HPE", "/redfish/v1/AccountService/Accounts", nil, "ReadOnly")
	last := p.steps[len(p.steps)-1].body["Oem"].(map[string]interface{})["Hp"].(map[string]interface{})["Privileges"].(map[string]bool)
	if len(last) != 1 || !last["LoginPriv"] {
		t.Errorf("HPE ReadOnly 권한 %v", last)
	}
	for _, st := range p.steps {
		b, err := jsonBody(st.body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := checkAccountCreateBody(b); err != nil {
			t.Errorf("계획 본문이 허용목록에서 거부됨: %v", err)
		}
	}
}

func jsonBody(m map[string]interface{}) ([]byte, error) { return []byte(jsonNoHTMLEscape(m)), nil }
