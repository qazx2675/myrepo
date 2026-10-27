package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// 점검 끝 계정 생성의 3번(AUTH_FAIL → 관리자 계정 직접 입력)·2번(1순위 접속 + user2 없음) 시험.

const (
	mgrUser = "mgr-adm"
	mgrPass = `Mgr!Pa$s#7<&>`
)

// adminMock 은 관리자 계정이 user/pass 인 mock 입니다 (rf.conf 의 fbUser 로는 로그인되지 않음).
func adminMock(t *testing.T, user, pass string, ao *mockbmc.AccountOptions) *mockbmc.Server {
	t.Helper()
	return startMockTree(t, "hpe-dl360gen10", func(o *mockbmc.Options) {
		o.User, o.Pass, o.OpenServiceRoot, o.Accounts = user, pass, true, ao
	})
}

// withUser2 는 rc 에 user2(+pass2.enc)를 설정합니다.
func withUser2(t *testing.T, rc *runContext) {
	t.Helper()
	dir := t.TempDir()
	rc.Conf.KeyFile, rc.Conf.Pass2File = filepath.Join(dir, "key.bin"), filepath.Join(dir, "pass2.enc")
	rc.Conf.User2, rc.Conf.User2Role = user2Name, roleOperator
	must(t, encryptFromReader(strings.NewReader(user2Pass), rc.Conf.Pass2File, rc.Conf.KeyFile))
}

// feedStdin 은 터미널로 보이는 입력 파이프입니다.
func feedStdin(t *testing.T, text string) *os.File {
	t.Helper()
	old := stdinIsTerminal
	stdinIsTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { stdinIsTerminal = old })
	pr, pw, err := os.Pipe()
	must(t, err)
	_, _ = pw.WriteString(text)
	pw.Close()
	return pr
}

// fakeAdminPassword 는 비밀번호 입력을 pw 로 바꾸고 호출 횟수를 셉니다.
func fakeAdminPassword(t *testing.T, pw string) *int {
	t.Helper()
	n := new(int)
	old := readAdminPassword
	readAdminPassword = func(in *os.File, out io.Writer, label string) (string, error) {
		*n++
		return pw, nil
	}
	t.Cleanup(func() { readAdminPassword = old })
	return n
}

func noMgrSecret(t *testing.T, dir, out string) {
	t.Helper()
	noSecretsUnder(t, dir, out)
	if strings.Contains(out, mgrPass) {
		t.Error("출력에 관리자 비밀번호 노출")
	}
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), mgrPass) {
				t.Errorf("%s 에 관리자 비밀번호 노출", p)
			}
		}
		return nil
	})
}

// AUTH_FAIL 호스트 3대: 직접 입력한 관리자 계정으로 빈 호스트는 두 계정 생성, user2 가 있는 호스트는 1순위만 생성,
// 관리자 계정이 다른 호스트는 로그인 1회 실패 후 다시 목록에 나오고 빈 입력으로 끝. 1순위를 만든 2대만 재점검.
func TestOfferAccountCreateManualAdmin(t *testing.T) {
	fastCheck(t)
	sNew := adminMock(t, mgrUser, mgrPass, &mockbmc.AccountOptions{})
	sU2 := adminMock(t, mgrUser, mgrPass, &mockbmc.AccountOptions{Slots: []mockbmc.Account{
		{UserName: user2Name, Password: "other-pw", RoleID: "Operator", Enabled: true}}})
	sWrong := adminMock(t, "other-adm", "other-pass", &mockbmc.AccountOptions{})
	rc := checkRCFor(t, 1, "", sNew.URL(), sU2.URL(), sWrong.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe")) // hpe 계정이 있지만 거부됨 → AUTH_FAIL
	withUser2(t, rc)
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow, ProbeUser2: true})
	must(t, err)
	for i, h := range run.Hosts {
		if h.Status != StatusAuthFail || h.authVendor != "hpe" {
			t.Fatalf("%d번 점검 준비가 틀림: %s %q", i, h.Status, h.authVendor)
		}
	}
	wrongBefore := sessCount(sWrong)
	calls := fakeAdminPassword(t, mgrPass)
	// ID → 확인(빈 입력 = Y) → 남은 호스트에 다시 묻는 ID 에 빈 입력 (끝)
	in := feedStdin(t, mgrUser+"\n\n\n")
	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, false, false, false, in, &out, reportOpts{}))
	o := out.String()

	if *calls != 1 {
		t.Errorf("비밀번호 입력 %d회 (기대 1)", *calls)
	}
	for _, name := range []string{testUser, user2Name} {
		if _, ok := findMockAcct(sNew, name); !ok {
			t.Errorf("sNew 에 %s 가 만들어지지 않음\n%s", name, o)
		}
	}
	if a, ok := findMockAcct(sU2, testUser); !ok || a.Password != testPass {
		t.Errorf("sU2 에 1순위 계정이 만들어지지 않음 %+v", sU2.Accounts())
	}
	if a, _ := findMockAcct(sU2, user2Name); a.Password != "other-pw" {
		t.Error("이미 있던 user2 를 덮어씀")
	}
	if len(sWrong.Accounts()) != 0 {
		t.Errorf("로그인되지 않은 호스트에 계정이 생김 %+v", sWrong.Accounts())
	}
	if d := sessCount(sWrong) - wrongBefore; d != 1 {
		t.Errorf("틀린 관리자 계정 호스트 로그인 시도 %d회 (기대 1)", d)
	}
	b, err := os.ReadFile(filepath.Join(run.Dir, "account_result.tsv"))
	must(t, err)
	if strings.Count(string(b), acctCreated) != 3 || !strings.Contains(string(b), acctExists) || !strings.Contains(string(b), StatusAuthFail) {
		t.Errorf("account_result.tsv:\n%s", b)
	}
	for _, want := range []string{"rf.conf 계정으로는 계정을 만들 수 없는 호스트 3대", "HPE", "관리자 ID (빈 입력: 건너뜀): ",
		"관리자 계정 " + mgrUser + " 로 3대", "로그인되지 않았거나 시도하지 않은 호스트 1대", "관리자 계정 직접 입력을 건너뜁니다.",
		"== 재점검 (1순위 계정, 2대) =="} {
		if !strings.Contains(o, want) {
			t.Errorf("출력에 %q 없음:\n%s", want, o)
		}
	}
	noMgrSecret(t, run.Dir, o)
}

// 직접 입력 횟수 상한: 계속 틀리면 manualMaxRounds 번만 로그인하고 끝낸다.
func TestOfferAccountCreateManualRoundLimit(t *testing.T) {
	fastCheck(t)
	s := adminMock(t, "other-adm", "other-pass", &mockbmc.AccountOptions{})
	rc := checkRCFor(t, 1, "", s.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	before := sessCount(s)
	calls := fakeAdminPassword(t, mgrPass)
	in := feedStdin(t, strings.Repeat(mgrUser+"\n\n", 5))
	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, false, false, false, in, &out, reportOpts{}))
	if *calls != manualMaxRounds || sessCount(s)-before != manualMaxRounds {
		t.Errorf("입력 %d회, 로그인 %d회 (기대 %d)\n%s", *calls, sessCount(s)-before, manualMaxRounds, out.String())
	}
	if !strings.Contains(out.String(), "번까지입니다") || len(s.Accounts()) != 0 {
		t.Errorf("상한 안내가 없거나 계정이 생김:\n%s", out.String())
	}
	noMgrSecret(t, run.Dir, out.String())
}

// 1순위 계정으로 접속했지만 user2 가 없는 호스트: 점검 중 계정 목록으로 찾고, 1순위 계정으로 로그인해 user2 만 만든다.
// user2 가 이미 있는 호스트는 대상이 아니고, 1순위 계정은 이미 있으므로 재점검도 하지 않는다.
func TestOfferAccountCreatePrimaryMissingUser2(t *testing.T) {
	fastCheck(t)
	seed := []mockbmc.Account{{UserName: testUser, Password: testPass, RoleID: "Administrator", Enabled: true}}
	sMiss := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: seed})
	sHave := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: append(append([]mockbmc.Account(nil), seed...),
		mockbmc.Account{UserName: user2Name, Password: "other-pw", RoleID: "Operator", Enabled: true})})
	rc := checkRCFor(t, 1, "", sMiss.URL(), sHave.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	withUser2(t, rc)

	// ProbeUser2 가 없으면 계정 목록을 읽지 않는다 (-auto 등).
	run0, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	if run0.Hosts[0].user2 != "" || sMiss.Count("GET", "/redfish/v1/AccountService/Accounts") != 0 {
		t.Fatalf("ProbeUser2 없이 계정 목록을 읽음 (%q)", run0.Hosts[0].user2)
	}

	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow, ProbeUser2: true})
	must(t, err)
	if h0, h1 := run.Hosts[0], run.Hosts[1]; h0.CredSource != credPrimary || h0.user2 != acctNeedCreate || h1.user2 != acctPresent {
		t.Fatalf("user2 확인이 틀림: %s %q / %q", h0.CredSource, h0.user2, h1.user2)
	}
	haveBefore := sessCount(sHave)
	pwCalls := fakeAdminPassword(t, mgrPass)
	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, false, false, false, feedStdin(t, "\n"), &out, reportOpts{}))
	o := out.String()
	if a, ok := findMockAcct(sMiss, user2Name); !ok || a.Password != user2Pass || a.RoleID != "Operator" {
		t.Errorf("user2 가 만들어지지 않음 %+v\n%s", sMiss.Accounts(), o)
	}
	if len(sMiss.Accounts()) != 2 {
		t.Errorf("user2 말고 다른 계정도 건드림 %+v", sMiss.Accounts())
	}
	if a, _ := findMockAcct(sHave, user2Name); a.Password != "other-pw" || sessCount(sHave) != haveBefore {
		t.Error("user2 가 있는 호스트에 접속·변경함")
	}
	if *pwCalls != 0 {
		t.Error("AUTH_FAIL 호스트가 없는데 관리자 비밀번호를 물음")
	}
	if _, err := os.Stat(filepath.Join(run.Dir, acctTargetsFile)); !os.IsNotExist(err) {
		t.Errorf("2순위 대상 목록이 생김 (%v)", err)
	}
	ul, err := os.ReadFile(filepath.Join(run.Dir, acctUser2File))
	must(t, err)
	if got := parseList(string(ul)); len(got) != 1 || got[0] != strings.TrimPrefix(sMiss.URL(), "https://") {
		t.Errorf("%s: %v", acctUser2File, got)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "recheck")); !os.IsNotExist(err) {
		t.Error("user2 만 만들었는데 재점검함")
	}
	for _, want := range []string{"두 번째 계정 " + user2Name + " 가 없는 호스트 1대", user2Name + "(Operator) 1대", acctCreated} {
		if !strings.Contains(o, want) {
			t.Errorf("출력에 %q 없음:\n%s", want, o)
		}
	}
	noMgrSecret(t, run.Dir, o)
}

// dry-run·-no-prompt: AUTH_FAIL 호스트는 목록만 (비밀번호를 묻지 않고 로그인하지 않음), user2 대상은 dry-run 이면 계획만.
func TestOfferAccountCreateManualDryRunNoPrompt(t *testing.T) {
	fastCheck(t)
	sFail := adminMock(t, mgrUser, mgrPass, &mockbmc.AccountOptions{})
	sMiss := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: []mockbmc.Account{
		{UserName: testUser, Password: testPass, RoleID: "Administrator", Enabled: true}}})
	rc := checkRCFor(t, 1, "", sFail.URL(), sMiss.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	withUser2(t, rc)
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow, ProbeUser2: true})
	must(t, err)
	failBefore := sessCount(sFail)
	calls := fakeAdminPassword(t, mgrPass)
	coll := "/redfish/v1/AccountService/Accounts"
	for _, c := range []struct {
		name          string
		dry, noPrompt bool
		want, wantNot string
	}{
		{"dry-run", true, false, acctWouldCreate, "== 재점검"},
		{"no-prompt", false, true, "-no-prompt 이므로 묻지 않습니다", acctCreated},
	} {
		var out bytes.Buffer
		must(t, offerAccountCreate(rc, run, c.dry, c.noPrompt, false, feedStdin(t, mgrUser+"\n\n"), &out, reportOpts{}))
		o := out.String()
		if !strings.Contains(o, c.want) || strings.Contains(o, c.wantNot) {
			t.Errorf("%s 출력:\n%s", c.name, o)
		}
		noMgrSecret(t, run.Dir, o)
	}
	if *calls != 0 || sessCount(sFail) != failBefore {
		t.Errorf("dry-run/-no-prompt 인데 관리자 비밀번호를 묻거나(%d) 로그인함(%d)", *calls, sessCount(sFail)-failBefore)
	}
	if sMiss.Count("POST", coll) != 0 || sFail.Count("POST", coll) != 0 || len(sMiss.Accounts()) != 1 || len(sFail.Accounts()) != 0 {
		t.Errorf("dry-run/-no-prompt 인데 계정을 만듦 %+v %+v", sMiss.Accounts(), sFail.Accounts())
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "recheck")); !os.IsNotExist(err) {
		t.Error("재점검 폴더가 생김")
	}
}
