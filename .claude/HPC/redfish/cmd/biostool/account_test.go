package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// account*.go 시험. 실제 BMC 는 쓰지 않고 internal/mockbmc(계정 서비스)만 쓴다.
// fbUser/fbPass(fallback_test.go)는 rf.conf 의 admin 계정이고, testUser/testPass 는 만들 1순위 계정이다.

const newPW = `N3w@Pa!ss#1<>&"`

func acctMock(t *testing.T, tree string, ao *mockbmc.AccountOptions) *mockbmc.Server {
	t.Helper()
	return startMockTree(t, tree, func(o *mockbmc.Options) {
		o.User, o.Pass, o.OpenServiceRoot, o.Accounts = fbUser, fbPass, true, ao
	})
}

func acctEnvFor(t *testing.T, vendor string) *acctEnv {
	t.Helper()
	return &acctEnv{
		conf:     &Config{User: testUser, Timeout: 5 * time.Second, Insecure: true, Retries: 0},
		password: testPass,
		fb:       fbCreds(t, fbConf(vendor)),
		gap:      -1,
	}
}

func acctTarget(t *testing.T, s *mockbmc.Server) Target {
	t.Helper()
	return checkRCFor(t, 1, "", s.URL()).Targets[0]
}

func findMockAcct(s *mockbmc.Server, name string) (mockbmc.Account, bool) {
	for _, a := range s.Accounts() {
		if a.UserName == name {
			return a, true
		}
	}
	return mockbmc.Account{}, false
}

// ---- 허용목록·본문 검사 ----

func TestCheckAllowedAccountPaths(t *testing.T) {
	coll := "/redfish/v1/AccountService/Accounts"
	mem := coll + "/5"
	dellColl := "/redfish/v1/Managers/iDRAC.Embedded.1/Accounts"
	cases := []struct {
		method, p string
		mode      Mode
		ok        bool
	}{
		{"POST", coll, ModeAccount, true},
		{"POST", coll + "/", ModeAccount, true},
		{"POST", dellColl, ModeAccount, true},
		{"POST", coll, ModeReadOnly, false},
		{"POST", coll, ModeSet, false},
		{"PATCH", mem, ModeAccount, true},
		{"PATCH", mem, ModeReadOnly, false},
		{"PATCH", mem, ModeSet, false},
		{"DELETE", mem, ModeAccount, true},
		{"DELETE", mem, ModeSet, false},
		{"DELETE", mem, ModeReadOnly, false},
		{"PATCH", "/redfish/v1/AccountService", ModeAccount, false},
		{"PATCH", "/redfish/v1/AccountService/Roles/Administrator", ModeAccount, false},
		{"POST", "/redfish/v1/AccountService/Roles", ModeAccount, false},
		{"POST", "/redfish/v1/Systems/1/Actions/ComputerSystem.Reset", ModeAccount, false},
		{"PATCH", "/redfish/v1/Systems/1/Bios/Settings", ModeAccount, false},
		{"PATCH", coll + "/5/extra", ModeAccount, false},
		{"DELETE", "/redfish/v1/Managers/1/Accounts", ModeAccount, false}, // 컬렉션 DELETE 금지
		{"DELETE", coll, ModeAccount, false},
	}
	for _, c := range cases {
		_, err := checkAllowed(c.method, c.p, c.mode, "")
		if (err == nil) != c.ok {
			t.Errorf("%s %s mode=%d: err=%v, 허용 기대 %t", c.method, c.p, c.mode, err, c.ok)
		}
	}
}

func TestCheckAccountBodies(t *testing.T) {
	createOK := []string{
		`{"UserName":"u","Password":"p@s!s","RoleId":"Administrator","Enabled":true}`,
		`{"UserName":"u","Password":"p","RoleId":"Admin"}`,
		`{"UserName":"u","Password":"p","RoleId":"admin","Enabled":true}`,
		`{"UserName":"u","Password":"p","RoleId":"Administrator","PasswordChangeRequired":false}`,
		`{"UserName":"u","Password":"p","Oem":{"Hpe":{"LoginName":"u","Privileges":{"LoginPriv":true,"iLOConfigPriv":true}}}}`,
		`{"UserName":"u","Password":"p","RoleId":"Operator","Enabled":true}`, // 두 번째 계정(user2_role)
		`{"UserName":"u","Password":"p","RoleId":"ReadOnly"}`,
		`{"UserName":"u","Password":"p","RoleId":"read-only"}`,
	}
	for _, b := range createOK {
		if _, err := checkAccountCreateBody([]byte(b)); err != nil {
			t.Errorf("생성 본문 거부됨: %s: %v", b, err)
		}
	}
	createBad := []string{
		`{"UserName":"u","Password":"p","RoleId":"Supervisor"}`, // 후보 밖의 역할(사용자 정의 등)
		`{"UserName":"u","Password":"p","RoleId":"None"}`,
		`{"UserName":"u","Password":"p","RoleId":"Administrator","Enabled":false}`,
		`{"UserName":"u","RoleId":"Administrator"}`,
		`{"Password":"p","RoleId":"Administrator"}`,
		`{"UserName":"u","Password":"p"}`,
		`{"UserName":"u","Password":"p","RoleId":"Administrator","Links":{}}`,
		`{"UserName":"u","Password":"p","RoleId":"Administrator","PasswordChangeRequired":true}`,
		`{"UserName":"u","Password":"p","Oem":{"Dell":{}}}`,
		`{"UserName":"u","Password":"p","Oem":{"Hpe":{"Privileges":{"LoginPriv":false}}}}`,
		`{"UserName":"u","Password":"p","Oem":{"Hpe":{"Privileges":{"Evil":true}}}}`,
		"{\"UserName\":\"u\",\"Password\":\"p\\u0000\",\"RoleId\":\"Administrator\"}",
		`[]`,
	}
	for _, b := range createBad {
		if _, err := checkAccountCreateBody([]byte(b)); err == nil {
			t.Errorf("생성 본문이 허용됨: %s", b)
		}
	}
	patchOK := []string{
		`{"Password":"x"}`,
		`{"UserName":"u","Password":"p","RoleId":"Administrator","Enabled":true}`,
		`{"UserName":"u","Password":"p","RoleId":"Operator","Enabled":true}`,
		`{"Enabled":false,"RoleId":"None"}`,
		`{"UserName":""}`,
	}
	for _, b := range patchOK {
		if _, err := checkAccountPatchBody([]byte(b)); err != nil {
			t.Errorf("PATCH 본문 거부됨: %s: %v", b, err)
		}
	}
	patchBad := []string{
		`{"Password":""}`,
		`{"Password":"x","RoleId":"Administrator"}`,
		`{"RoleId":"Administrator"}`,
		`{"Enabled":true}`,
		`{"UserName":"newname"}`,
		`{"UserName":"u","Password":"p","RoleId":"Supervisor","Enabled":true}`,
		`{"Locked":false}`,
		`{"Enabled":false,"RoleId":"Administrator"}`,
		`{}`,
	}
	for _, b := range patchBad {
		if _, err := checkAccountPatchBody([]byte(b)); err == nil {
			t.Errorf("PATCH 본문이 허용됨: %s", b)
		}
	}
}

// 계정 모드 Client 도 등록(AllowAccountTarget)하지 않은 멤버에는 PATCH/DELETE 를 보내지 않는다.
func TestAccountClientNeedsRegistration(t *testing.T) {
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: []mockbmc.Account{{UserName: "keep", Password: "k", RoleID: "Administrator", Enabled: true}}})
	c := NewClient(ClientOpts{BaseURL: s.URL(), User: fbUser, Pass: fbPass, Insecure: true, Mode: ModeAccount, Gap: -1, Timeout: 5 * time.Second})
	defer c.Close()
	_, _, err := c.AccountPatch("/redfish/v1/AccountService/Accounts/1", map[string]interface{}{"Password": "x"})
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("미등록 PATCH 가 막히지 않음: %v", err)
	}
	if _, _, err := c.AccountDelete("/redfish/v1/AccountService/Accounts/1"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("미등록 DELETE 가 막히지 않음: %v", err)
	}
	if n := s.Count("PATCH", "/") + s.Count("DELETE", "/redfish/v1/AccountService"); n != 0 {
		t.Errorf("쓰기 %d 건이 BMC 에 도달함", n)
	}
	if a, _ := findMockAcct(s, "keep"); a.Password != "k" {
		t.Error("기존 계정이 바뀜")
	}
	// 읽기 전용 Client 는 계정 경로 POST 자체가 막힌다.
	ro := NewClient(ClientOpts{BaseURL: s.URL(), User: fbUser, Pass: fbPass, Insecure: true, Mode: ModeReadOnly, Gap: -1})
	defer ro.Close()
	if _, _, _, err := ro.AccountPost("/redfish/v1/AccountService/Accounts", map[string]interface{}{"UserName": "x", "Password": "y", "RoleId": "Administrator"}); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("읽기 전용 Client 가 계정 POST 를 보냄: %v", err)
	}
}

// ---- create ----

func TestCreatePostStyleHPE(t *testing.T) {
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	env := acctEnvFor(t, "hpe")
	h := newAcctHost(acctTarget(t, s), "create")
	env.createOnHost(h)
	if h.Status != acctCreated {
		t.Fatalf("status %s (%s)", h.Status, h.Detail)
	}
	a, ok := findMockAcct(s, testUser)
	if !ok || a.Password != testPass || a.RoleID != "Administrator" || !a.Enabled {
		t.Fatalf("생성된 계정 %+v", a)
	}
	if !strings.HasPrefix(h.Method, "POST ") {
		t.Errorf("방식 %q", h.Method)
	}
	// 같은 id 가 이미 있으면 다시 만들지 않고 건너뛴다 (덮어쓰기 금지).
	posts := s.Count("POST", "/redfish/v1/AccountService/Accounts")
	h2 := newAcctHost(acctTarget(t, s), "create")
	env.createOnHost(h2)
	if h2.Status != acctExists {
		t.Errorf("재실행 status %s", h2.Status)
	}
	if n := s.Count("POST", "/redfish/v1/AccountService/Accounts"); n != posts {
		t.Errorf("이미 있는데 POST %d→%d", posts, n)
	}
	row := strings.Join(h.row(), "\t") + strings.Join(h2.row(), "\t")
	for _, secret := range []string{testPass, fbPass, fbUser} {
		if strings.Contains(row, secret) {
			t.Errorf("결과 행에 비밀 값 노출: %q", secret)
		}
	}
}

func TestCreateSlotStyleDell(t *testing.T) {
	slots := []mockbmc.Account{
		{}, // 1: 익명 (빈 이름이지만 Dell 은 3 미만 슬롯을 쓰지 않는다)
		{UserName: "root", Password: "calvin", RoleID: "Administrator", Enabled: true},
		{}, {},
	}
	s := acctMock(t, "dell-r660", &mockbmc.AccountOptions{Slots: slots, SlotStyle: true})
	env := acctEnvFor(t, "dell")
	h := newAcctHost(acctTarget(t, s), "create")
	env.createOnHost(h)
	if h.Status != acctCreated {
		t.Fatalf("status %s (%s)", h.Status, h.Detail)
	}
	acs := s.Accounts()
	if acs[0].UserName != "" || acs[2].UserName != testUser || acs[3].UserName != "" {
		t.Errorf("슬롯 사용이 틀림: %+v", acs)
	}
	if s.Count("POST", "/redfish/v1/AccountService/Accounts") != 0 {
		t.Error("슬롯 방식인데 컬렉션 POST 를 보냄")
	}
	if !strings.HasPrefix(h.Method, "PATCH ") {
		t.Errorf("방식 %q", h.Method)
	}
}

func TestCreateVerifyFailRollsBackOnlyOurs(t *testing.T) {
	for _, slotStyle := range []bool{false, true} {
		slots := []mockbmc.Account{{UserName: "other", Password: "o", RoleID: "Administrator", Enabled: true}, {}, {}, {}}
		s := acctMock(t, "dell-r660", &mockbmc.AccountOptions{Slots: slots, SlotStyle: slotStyle, DenyNewLogin: true})
		env := acctEnvFor(t, "dell")
		h := newAcctHost(acctTarget(t, s), "create")
		env.createOnHost(h)
		if h.Status != acctVerifyFailRB || h.Rollback != "완료" {
			t.Fatalf("slotStyle=%t status %s rollback %s (%s)", slotStyle, h.Status, h.Rollback, h.Detail)
		}
		if _, ok := findMockAcct(s, testUser); ok {
			t.Errorf("slotStyle=%t 롤백 뒤에도 계정이 남음", slotStyle)
		}
		if a, ok := findMockAcct(s, "other"); !ok || a.Password != "o" {
			t.Errorf("slotStyle=%t 롤백이 다른 계정을 건드림", slotStyle)
		}
	}
}

func TestCreateDryRunWritesNothing(t *testing.T) {
	s := acctMock(t, "lenovo-sr650v3", &mockbmc.AccountOptions{})
	env := acctEnvFor(t, "lenovo")
	env.dryRun = true
	h := newAcctHost(acctTarget(t, s), "create")
	env.createOnHost(h)
	if h.Status != acctWouldCreate {
		t.Fatalf("status %s (%s)", h.Status, h.Detail)
	}
	if strings.Contains(h.Detail, testPass) || !strings.Contains(h.Detail, "*****") {
		t.Errorf("dry-run 본문이 비밀번호를 가리지 않음: %s", h.Detail)
	}
	if s.Count("POST", "/redfish/v1/AccountService/Accounts") != 0 || len(s.Accounts()) != 0 {
		t.Error("dry-run 인데 쓰기가 나감")
	}
}

func TestCreateNoAdminForVendor(t *testing.T) {
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	env := acctEnvFor(t, "dell") // HPE 장비인데 dell admin 만 있음
	h := newAcctHost(acctTarget(t, s), "create")
	env.createOnHost(h)
	if h.Status != acctNoAdmin {
		t.Fatalf("status %s (%s)", h.Status, h.Detail)
	}
	if s.Count("POST", "/redfish/v1/SessionService/Sessions") != 0 {
		t.Error("admin 계정이 없는데 로그인을 시도함")
	}
}

func TestProbeCreateSourceSplit(t *testing.T) {
	// 1순위 계정이 mock 에 있음 → PRIMARY_OK
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: []mockbmc.Account{{UserName: testUser, Password: testPass, RoleID: "Administrator", Enabled: true}}})
	env := acctEnvFor(t, "hpe")
	h := env.probeCreate(acctTarget(t, s))
	if h.Status != acctPrimaryOK || h.Source != credPrimary {
		t.Errorf("status %s source %s", h.Status, h.Source)
	}
	// 없음 → 2순위로 로그인 = NEED_CREATE
	s2 := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	h2 := env.probeCreate(acctTarget(t, s2))
	if h2.Status != acctNeedCreate || h2.Source != credFallback {
		t.Errorf("status %s source %s (%s)", h2.Status, h2.Source, h2.Detail)
	}
	if s2.Count("POST", "/redfish/v1/AccountService/Accounts") != 0 {
		t.Error("점검 단계에서 쓰기가 나감")
	}
}

// ---- change ----

func TestChangePasswordAndRollback(t *testing.T) {
	seed := []mockbmc.Account{{UserName: testUser, Password: testPass, RoleID: "Administrator", Enabled: true}}
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: seed})
	env := acctEnvFor(t, "hpe")
	env.newPass = newPW
	h := newAcctHost(acctTarget(t, s), "change")
	env.changeOnHost(h)
	if h.Status != acctChanged {
		t.Fatalf("status %s (%s)", h.Status, h.Detail)
	}
	if a, _ := findMockAcct(s, testUser); a.Password != newPW {
		t.Errorf("비밀번호가 바뀌지 않음")
	}
	// 새 비밀번호 로그인이 거부되는 BMC → 이전 비밀번호로 되돌린다.
	s2 := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: seed, DenyNewLogin: true})
	h2 := newAcctHost(acctTarget(t, s2), "change")
	env.changeOnHost(h2)
	if h2.Status != acctVerifyFailRB || h2.Rollback != "완료" {
		t.Fatalf("status %s rollback %s (%s)", h2.Status, h2.Rollback, h2.Detail)
	}
	if a, _ := findMockAcct(s2, testUser); a.Password != testPass {
		t.Error("롤백 뒤 이전 비밀번호가 아님")
	}
	// id 가 없으면 NO_ACCOUNT, 쓰기 없음
	s3 := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	h3 := newAcctHost(acctTarget(t, s3), "change")
	env.changeOnHost(h3)
	if h3.Status != StatusAuthFail || s3.Count("PATCH", "/") != 0 {
		t.Errorf("status %s PATCH %d", h3.Status, s3.Count("PATCH", "/"))
	}
	for _, text := range []string{strings.Join(h.row(), " "), strings.Join(h2.row(), " "), h2.Detail} {
		if strings.Contains(text, newPW) || strings.Contains(text, testPass) {
			t.Errorf("결과에 비밀번호 노출: %q", text)
		}
	}
}

// ---- 입력 ----

func chunkSeq(chunks []string, gap time.Duration) chunkSource {
	i := 0
	at := time.Unix(1000, 0)
	return func() ([]byte, time.Time, error) {
		if i >= len(chunks) {
			return nil, at, io.EOF
		}
		c := chunks[i]
		i++
		at = at.Add(gap)
		return []byte(c), at, nil
	}
}

func TestScanSecret(t *testing.T) {
	var out bytes.Buffer
	// 한 글자씩 천천히: 정상
	s, pasted, err := scanSecret(chunkSeq([]string{"a", "b", "!", "한", "x", "\x7f", "c", "\r"}, 50*time.Millisecond), &out, pasteMinGap)
	if err != nil || pasted || s != "ab!한c" {
		t.Fatalf("정상 입력: %q pasted=%t err=%v", s, pasted, err)
	}
	if strings.Contains(out.String(), "ab!") || strings.Count(out.String(), "*") < 5 {
		t.Errorf("화면 표시가 *가 아님: %q", out.String())
	}
	// 한 번에 여러 글자(붙여넣기)
	if _, pasted, _ := scanSecret(chunkSeq([]string{"abcdef\r"}, 50*time.Millisecond), &bytes.Buffer{}, pasteMinGap); !pasted {
		t.Error("한 덩어리 입력을 붙여넣기로 보지 않음")
	}
	// 글자 간격이 너무 짧음
	if _, pasted, _ := scanSecret(chunkSeq([]string{"a", "b", "c", "\r"}, time.Millisecond), &bytes.Buffer{}, pasteMinGap); !pasted {
		t.Error("빠른 연속 입력을 붙여넣기로 보지 않음")
	}
	// bracketed paste
	if _, pasted, _ := scanSecret(chunkSeq([]string{"\x1b[200~secret\x1b[201~"}, 50*time.Millisecond), &bytes.Buffer{}, pasteMinGap); !pasted {
		t.Error("bracketed paste 를 붙여넣기로 보지 않음")
	}
	// 화살표 키는 글자로 넣지 않는다
	s, pasted, _ = scanSecret(chunkSeq([]string{"a", "\x1b[A", "b", "\n"}, 50*time.Millisecond), &bytes.Buffer{}, pasteMinGap)
	if pasted || s != "ab" {
		t.Errorf("화살표 처리: %q pasted=%t", s, pasted)
	}
	// Ctrl-C
	if _, _, err := scanSecret(chunkSeq([]string{"a", "\x03"}, 50*time.Millisecond), &bytes.Buffer{}, pasteMinGap); !errors.Is(err, errSecretInterrupt) {
		t.Errorf("Ctrl-C: %v", err)
	}
}

func TestCshQuote(t *testing.T) {
	cases := map[string]string{
		"/home/u/redfish": `'/home/u/redfish'`,
		"/home/my dir/x":  `'/home/my dir/x'`,
		"a'b":             `'a'\''b'`,
		"hi!there":        `'hi'\!'there'`,
		"":                `''`,
		`/p/$HOME/"q"/*`:  `'/p/$HOME/"q"/*'`,
		"!!":              `''\!''\!''`,
		"/x/한글/dir":       `'/x/한글/dir'`,
	}
	for in, want := range cases {
		if got := cshQuote(in); got != want {
			t.Errorf("cshQuote(%q) = %s, 기대 %s", in, got, want)
		}
	}
}

func TestReadNewPasswordRejectsNonTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	must(t, err)
	defer r.Close()
	defer w.Close()
	if _, err := readNewPassword(r, io.Discard); !errors.Is(err, errNotTerminal) {
		t.Errorf("파이프 입력이 거부되지 않음: %v", err)
	}
}

// ---- 병렬·웜업·확인 ----

func TestRunAcctPoolWarmupStopsOnFailures(t *testing.T) {
	var ran, skipped int
	runAcctPool(40, 4, 10,
		func(i int) string { ran++; return acctRejected },
		func(i int) { skipped++ })
	if ran != 10 || skipped != 30 {
		t.Errorf("웜업 중단: ran %d skipped %d", ran, skipped)
	}
	ran, skipped = 0, 0
	var okCount int
	runAcctPool(40, 4, 10,
		func(i int) string { return acctCreated },
		func(i int) { skipped++ })
	_ = okCount
	if skipped != 0 {
		t.Errorf("성공인데 건너뜀 %d", skipped)
	}
}

func TestAskYesNo(t *testing.T) {
	cases := []struct {
		in         string
		defaultYes bool
		want       bool
	}{
		{"\n", true, true}, {"y\n", false, true}, {"Y\n", false, true}, {"n\n", true, false},
		{"\n", false, false}, {"", true, false}, {"yes\n", false, true}, {"x\n", true, false},
	}
	for _, c := range cases {
		var out bytes.Buffer
		if got := askYesNo(strings.NewReader(c.in), &out, "? ", c.defaultYes); got != c.want {
			t.Errorf("%q default=%t: %t, 기대 %t", c.in, c.defaultYes, got, c.want)
		}
	}
	// 두 번 연속 질문해도 첫 질문이 뒤 입력을 삼키지 않는다.
	r := strings.NewReader("y\nn\n")
	if !askYesNo(r, io.Discard, "", false) || askYesNo(r, io.Discard, "", true) {
		t.Error("연속 질문 입력이 섞임")
	}
}

func TestAcctWorkers(t *testing.T) {
	if acctWorkers(0, 500) != 100 || acctWorkers(0, 20) != 20 || acctWorkers(1000, 20) != 300 || acctWorkers(50, 500) != 50 {
		t.Error("동시 처리 수 결정이 틀림")
	}
}

// check 끝 제안: 2순위로 접속한 호스트가 있으면 기본 Y 로 계정을 만든다.
func TestOfferAccountCreateDefaultYes(t *testing.T) {
	fastCheck(t)
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	rc := checkRCFor(t, 1, "", s.URL())
	rc.Fallback = fbCreds(t, fbConf("hpe"))
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	if len(run.fallbackHosts()) != 1 {
		t.Fatalf("2순위 접속 호스트 %d", len(run.fallbackHosts()))
	}
	old := stdinIsTerminal
	stdinIsTerminal = func(*os.File) bool { return true }
	defer func() { stdinIsTerminal = old }()
	pr, pw, err := os.Pipe()
	must(t, err)
	_, _ = pw.WriteString("\n") // 빈 입력 = 기본 Y
	pw.Close()
	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, false, false, false, pr, &out, reportOpts{}))
	if _, ok := findMockAcct(s, testUser); !ok {
		t.Fatalf("기본 Y 인데 계정이 만들어지지 않음\n%s", out.String())
	}
	b, err := os.ReadFile(filepath.Join(run.Dir, "account_result.tsv"))
	must(t, err)
	if !strings.Contains(string(b), acctCreated) || strings.Contains(string(b), testPass) || strings.Contains(string(b), fbPass) {
		t.Errorf("account_result.tsv: %s", b)
	}
	// 비대화형(-stdin-ok 없음)이면 묻지 않고 만들지 않는다.
	s2 := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	rc2 := checkRCFor(t, 1, "", s2.URL())
	rc2.Fallback = fbCreds(t, fbConf("hpe"))
	run2, err := doCheck(rc2, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
	must(t, err)
	stdinIsTerminal = func(*os.File) bool { return false }
	pr2, pw2, _ := os.Pipe()
	_, _ = pw2.WriteString("y\n")
	pw2.Close()
	out.Reset()
	must(t, offerAccountCreate(rc2, run2, false, false, false, pr2, &out, reportOpts{}))
	if _, ok := findMockAcct(s2, testUser); ok {
		t.Error("비대화형 입력으로 계정이 만들어짐")
	}
	// -no-prompt: 안내만
	out.Reset()
	must(t, offerAccountCreate(rc2, run2, false, true, true, pr2, &out, reportOpts{}))
	if _, ok := findMockAcct(s2, testUser); ok || !strings.Contains(out.String(), "account_create.sh") {
		t.Errorf("-no-prompt: %s", out.String())
	}
}
