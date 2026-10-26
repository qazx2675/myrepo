package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// 점검 끝 계정 생성 → 생성한 호스트만 재점검 시험 (account_check.go). 실제 BMC 없이 mockbmc 계정 서비스만 쓴다.

// yesStdin 은 터미널인 척하는 표준입력에 빈 줄(기본 Y)을 넣어 둡니다.
func yesStdin(t *testing.T) *os.File {
	t.Helper()
	old := stdinIsTerminal
	stdinIsTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { stdinIsTerminal = old })
	pr, pw, err := os.Pipe()
	must(t, err)
	_, _ = pw.WriteString("\n")
	pw.Close()
	return pr
}

// noSecretsUnder 는 dir 아래 모든 파일과 out 에 비밀번호가 없는지 봅니다.
func noSecretsUnder(t *testing.T, dir, out string) {
	t.Helper()
	secrets := []string{testPass, user2Pass, fbPass}
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("출력에 비밀 값 노출: %q", s)
		}
	}
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, rerr := os.ReadFile(p)
		must(t, rerr)
		for _, s := range secrets {
			if strings.Contains(string(b), s) {
				t.Errorf("%s 에 비밀 값 노출: %q", p, s)
			}
		}
		return nil
	})
}

func sessCount(s *mockbmc.Server) int { return s.Count("POST", "/redfish/v1/SessionService/Sessions") }

// 1순위 없음 → 두 계정 생성 → 재점검 OK / user2 만 이미 있음 → user2 스킵·재점검 / 1순위 id 가 이미 있음(비밀번호 다름) →
// 스킵·재점검 안 함 / rf.conf 에 벤더 계정 없음 → 생성 불가 목록.
func TestOfferAccountCreateRecheck(t *testing.T) {
	fastCheck(t)
	sNew := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	sU2 := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: []mockbmc.Account{
		{UserName: user2Name, Password: "other-pw", RoleID: "Operator", Enabled: true}}})
	sDup := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: []mockbmc.Account{
		{UserName: testUser, Password: "other-pw", RoleID: "Administrator", Enabled: true}}})
	sNo := acctMock(t, "dell-r660", &mockbmc.AccountOptions{}) // rf.conf 에 dell 계정 없음 → AUTH_FAIL
	rc := checkRCFor(t, 1, "", sNew.URL(), sU2.URL(), sDup.URL(), sNo.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	dir := t.TempDir()
	rc.Conf.KeyFile, rc.Conf.Pass2File = filepath.Join(dir, "key.bin"), filepath.Join(dir, "pass2.enc")
	rc.Conf.User2, rc.Conf.User2Role = user2Name, roleOperator
	must(t, encryptFromReader(strings.NewReader(user2Pass), rc.Conf.Pass2File, rc.Conf.KeyFile))
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	if len(run.fallbackHosts()) != 3 || run.Hosts[3].Status != StatusAuthFail {
		t.Fatalf("점검 준비가 틀림: 2순위 %d대, 4번째 %s", len(run.fallbackHosts()), run.Hosts[3].Status)
	}
	dupBefore, noBefore := sessCount(sDup), sessCount(sNo)

	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, false, false, false, yesStdin(t), &out, reportOpts{}))
	o := out.String()

	if _, ok := findMockAcct(sNew, user2Name); !ok {
		t.Errorf("sNew 에 user2 가 만들어지지 않음 %+v", sNew.Accounts())
	}
	if a, _ := findMockAcct(sU2, user2Name); a.Password != "other-pw" {
		t.Error("이미 있던 user2 를 덮어씀")
	}
	if a, _ := findMockAcct(sDup, testUser); a.Password != "other-pw" {
		t.Error("이미 있던 1순위 계정을 덮어씀")
	}
	// 1순위가 SKIP_EXISTS 인 호스트는 admin 로그인 1회 + user2 검증 1회 뿐 (재점검으로 1순위 인증 실패를 늘리지 않음),
	// 생성 불가는 다시 접속 안 함.
	if d := sessCount(sDup) - dupBefore; d != 2 {
		t.Errorf("sDup 세션 시도 %d회 (기대 2: admin 로그인 + user2 검증, 재점검 없음)", d)
	}
	if d := sessCount(sNo) - noBefore; d != 0 {
		t.Errorf("생성 불가 호스트에 다시 접속함 (%d회)", d)
	}

	// 대상 목록 파일 (원문·이름·IP 만, 0600)
	readList := func(name string) string {
		p := filepath.Join(run.Dir, name)
		b, err := os.ReadFile(p)
		must(t, err)
		if fi, _ := os.Stat(p); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
			t.Errorf("%s 권한 %v", name, fi.Mode().Perm())
		}
		return string(b)
	}
	in := func(s *mockbmc.Server) string { return strings.TrimPrefix(s.URL(), "https://") }
	tg := readList(acctTargetsFile)
	for _, s := range []*mockbmc.Server{sNew, sU2, sDup} {
		if !strings.Contains(tg, in(s)+"\t# ") {
			t.Errorf("%s 에 %s 없음:\n%s", acctTargetsFile, in(s), tg)
		}
	}
	if strings.Contains(tg, in(sNo)) {
		t.Errorf("생성 불가 호스트가 대상 목록에 들어감:\n%s", tg)
	}
	if got := parseList(tg); len(got) != 3 || got[0] != in(sNew) {
		t.Errorf("대상 목록을 user.txt 로 다시 읽은 결과 %v", got)
	}
	uf := readList(acctUnfixableFile)
	if got := parseList(uf); len(got) != 1 || got[0] != in(sNo) {
		t.Errorf("%s: %v\n%s", acctUnfixableFile, got, uf)
	}

	// 재점검: 1순위를 만든 2대만, 1순위 계정으로 (2순위 접속 0).
	subs, _ := filepath.Glob(filepath.Join(run.Dir, "recheck", "*"))
	if len(subs) != 1 {
		t.Fatalf("재점검 폴더 %v\n%s", subs, o)
	}
	ri, err := os.ReadFile(filepath.Join(subs[0], "run_info.txt"))
	must(t, err)
	if !strings.Contains(string(ri), "2순위 계정(rf.conf)으로 접속: 0") {
		t.Errorf("재점검이 1순위 계정으로 되지 않음:\n%s", ri)
	}
	merged, _ := filepath.Glob(filepath.Join(subs[0], "profile_merged_*"))
	if len(merged) == 0 {
		t.Error("합친 결과 CSV 가 없음")
	}
	for _, want := range []string{"계정을 만들 수 없는 호스트 1대", acctUnfixableFile, "생성 대상 목록:", "== 재점검 (1순위 계정, 2대) ==", "합친 결과 (전체 4대, 재점검 2대 반영)", "재점검 결과 폴더:", acctExists} {
		if !strings.Contains(o, want) {
			t.Errorf("출력에 %q 없음:\n%s", want, o)
		}
	}
	noSecretsUnder(t, run.Dir, o)
}

// -dry-run: 계획만 (BMC 에 쓰지 않음), 재점검도 하지 않음. 대상 목록 파일은 남긴다.
func TestOfferAccountCreateDryRunNoRecheck(t *testing.T) {
	fastCheck(t)
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	rc := checkRCFor(t, 1, "", s.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, true, false, false, yesStdin(t), &out, reportOpts{}))
	if len(s.Accounts()) != 0 || s.Count("POST", "/redfish/v1/AccountService/Accounts") != 0 {
		t.Errorf("dry-run 인데 계정을 만듦 %+v", s.Accounts())
	}
	if !strings.Contains(out.String(), acctWouldCreate) || strings.Contains(out.String(), "== 재점검") {
		t.Errorf("dry-run 출력:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "recheck")); !os.IsNotExist(err) {
		t.Errorf("dry-run 인데 재점검 폴더가 생김 (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, acctTargetsFile)); err != nil {
		t.Errorf("대상 목록 파일 없음: %v", err)
	}
	noSecretsUnder(t, run.Dir, out.String())
}

// 거절(n)·비대화형이면 만들지도, 재점검하지도 않는다 (목록 파일만).
func TestOfferAccountCreateDeclineNoRecheck(t *testing.T) {
	fastCheck(t)
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	rc := checkRCFor(t, 1, "", s.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	old := stdinIsTerminal
	stdinIsTerminal = func(*os.File) bool { return false }
	defer func() { stdinIsTerminal = old }()
	pr, pw, _ := os.Pipe()
	_, _ = pw.WriteString("y\n")
	pw.Close()
	before := sessCount(s)
	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, false, false, false, pr, &out, reportOpts{}))
	if len(s.Accounts()) != 0 || sessCount(s) != before {
		t.Errorf("비대화형인데 접속·생성함")
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "recheck")); !os.IsNotExist(err) {
		t.Error("재점검 폴더가 생김")
	}
}
