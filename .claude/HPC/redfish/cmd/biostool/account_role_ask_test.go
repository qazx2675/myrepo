package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// user2 권한이 설정에 없을 때 묻는 부분(account_role_ask.go) 시험. 입력은 os.Pipe, 터미널 여부는 stdinIsTerminal 로 바꾼다
// (실제 표준입력은 읽지 않음). 실제 BMC 는 쓰지 않고 mockbmc 만 쓴다.

// feedText 는 s 를 담고 닫힌 파이프의 읽는 쪽입니다.
func feedText(t *testing.T, s string) *os.File {
	t.Helper()
	pr, pw, err := os.Pipe()
	must(t, err)
	_, _ = pw.WriteString(s)
	pw.Close()
	t.Cleanup(func() { pr.Close() })
	return pr
}

func setTerminal(t *testing.T, on bool) {
	t.Helper()
	old := stdinIsTerminal
	stdinIsTerminal = func(*os.File) bool { return on }
	t.Cleanup(func() { stdinIsTerminal = old })
}

// roleEnv 는 user2 권한이 설정에 없는 (user2_role 비어 있음) 환경입니다. vendorRoles 는 user2_role_<vendor> 설정.
func roleEnv(t *testing.T, base string, vendorRoles map[string]string) *acctEnv {
	t.Helper()
	env := acctEnvFor(t, "hpe")
	env.conf.User2, env.conf.User2Role, env.conf.User2Roles = user2Name, base, vendorRoles
	env.asked = map[string]string{}
	twoSpecs(env, base)
	return env
}

// u2Groups 는 벤더마다 호스트 1대씩 user2 생성 대상 칸 (점검에서 NEED_CREATE, 벤더 기준으로 권한을 정한 상태)입니다.
func u2Groups(env *acctEnv, vendors ...string) [][]*acctHost {
	var gs [][]*acctHost
	for i, v := range vendors {
		g := newCreateHosts(Target{Hostname: "h" + string(rune('a'+i))}, env.accounts()[1:])
		g[0].Status, g[0].Vendor = acctNeedCreate, v
		env.setRoles(g, v)
		gs = append(gs, g)
	}
	return gs
}

func TestRoleFromConfNeverAsks(t *testing.T) {
	setTerminal(t, true)
	// 설정에 해석되는 권한이 있으면 묻지 않는다: 벤더별 값이 일반 값보다 앞선다.
	env := roleEnv(t, roleOperator, map[string]string{"hpe": roleAdministrator})
	gs := u2Groups(env, "HPE", "Dell", "")
	var out bytes.Buffer
	if err := env.askUser2Roles(feedText(t, ""), &out, gs, &accountFlags{}); err != nil || out.Len() != 0 {
		t.Fatalf("설정이 있는데 물음: %v %q", err, out.String())
	}
	for i, want := range []string{roleAdministrator, roleOperator, roleOperator} {
		if got := gs[i][0].Role; got != want || gs[i][0].roleAsked {
			t.Errorf("호스트 %d 권한 %q (기대 %q, asked=%t)", i, got, want, gs[i][0].roleAsked)
		}
	}
	// user2_role 만 없고 벤더별 값이 있으면: 그 벤더는 묻지 않고 나머지만 묻는다.
	env = roleEnv(t, "", map[string]string{"hpe": roleReadOnly})
	gs = u2Groups(env, "HPE", "Dell")
	if gs[0][0].Role != roleReadOnly || gs[1][0].Role != "" {
		t.Fatalf("벤더별 값 해석: %q %q", gs[0][0].Role, gs[1][0].Role)
	}
	out.Reset()
	must(t, env.askUser2Roles(feedText(t, "2\n"), &out, gs, &accountFlags{}))
	if strings.Count(out.String(), "선택 [1-3]") != 1 || !strings.Contains(out.String(), "벤더: "+vendorLabel("dell")) || gs[1][0].Role != roleOperator || gs[0][0].Role != roleReadOnly {
		t.Errorf("나머지 벤더만 물어야 함:\n%s", out.String())
	}
}

func TestRoleAskChoices(t *testing.T) {
	setTerminal(t, true)
	cases := []struct{ name, input, want, shows string }{
		{"1 Administrator", "1\n", roleAdministrator, ""},
		{"2 Operator", "2\n", roleOperator, ""},
		{"3 직접 입력 ReadOnly", "3\nreadonly\n", roleReadOnly, ""},
		{"3 직접 입력 대소문자 무시", "3\n  oPeRaToR  \n", roleOperator, ""},
		{"3 RoleId 별칭", "3\nread-only\n", roleReadOnly, "read-only 는 ReadOnly 로 처리"},
		{"번호 대신 이름", "Operator\n", roleOperator, ""},
		{"잘못된 번호 뒤 다시", "9\n\n2\n", roleOperator, "1, 2, 3 중에서 고르십시오"},
		{"빈 직접 입력은 다시", "3\n\n  \nAdministrator\n", roleAdministrator, "비어 있습니다"},
		{"모르는 권한은 안내 뒤 선택으로", "3\nsuperuser\n1\n", roleAdministrator, "알 수 없는 권한입니다: superuser"},
	}
	for _, tc := range cases {
		env := roleEnv(t, "", nil)
		gs := u2Groups(env, "Dell")
		var out bytes.Buffer
		if err := env.askUser2Roles(feedText(t, tc.input), &out, gs, &accountFlags{}); err != nil {
			t.Errorf("%s: %v\n%s", tc.name, err, out.String())
			continue
		}
		h := gs[0][0]
		if h.Role != tc.want || !h.roleAsked || env.asked["dell"] != tc.want {
			t.Errorf("%s: Role=%q asked=%t memo=%v (기대 %q)", tc.name, h.Role, h.roleAsked, env.asked, tc.want)
		}
		if tc.shows != "" && !strings.Contains(out.String(), tc.shows) {
			t.Errorf("%s: 안내 %q 없음:\n%s", tc.name, tc.shows, out.String())
		}
		if !strings.Contains(out.String(), "user2_role_dell 또는 user2_role") || !strings.Contains(out.String(), "호스트 1대") {
			t.Errorf("%s: 질문에 벤더·호스트 수·설정 안내가 없음:\n%s", tc.name, out.String())
		}
	}
}

func TestRoleAskRepeatsAndEOF(t *testing.T) {
	setTerminal(t, true)
	env := roleEnv(t, "", nil)
	gs := u2Groups(env, "Dell")
	var out bytes.Buffer
	// 입력이 끝나면(EOF) 권한을 정하지 못해 오류이고 Role 은 비어 있다.
	if err := env.askUser2Roles(feedText(t, ""), &out, gs, &accountFlags{}); err == nil || gs[0][0].Role != "" {
		t.Errorf("EOF: %v role=%q", err, gs[0][0].Role)
	}
	// 계속 틀리면 중단한다.
	out.Reset()
	if err := env.askUser2Roles(feedText(t, strings.Repeat("x\n", 10)), &out, gs, &accountFlags{}); err == nil || gs[0][0].Role != "" || len(env.asked) != 0 {
		t.Errorf("반복 오류: %v role=%q memo=%v", err, gs[0][0].Role, env.asked)
	}
}

// 같은 벤더는 한 번만 묻고, 나중에 만든 같은 벤더 칸에도 그 답을 쓴다 (벤더가 다르면 따로 묻는다).
func TestRoleAskOncePerVendor(t *testing.T) {
	setTerminal(t, true)
	env := roleEnv(t, "", nil)
	gs := u2Groups(env, "HPE", "Dell", "hpe", "Hewlett Packard Enterprise", "")
	var out bytes.Buffer
	// 벤더 3종(hpe, dell, 모름) → 질문 3번.
	must(t, env.askUser2Roles(feedText(t, "2\n1\n3\nreadonly\n"), &out, gs, &accountFlags{}))
	if n := strings.Count(out.String(), "선택 [1-3]"); n != 3 {
		t.Errorf("질문 %d번 (기대 3)\n%s", n, out.String())
	}
	for i, want := range []string{roleOperator, roleAdministrator, roleOperator, roleOperator, roleReadOnly} {
		if gs[i][0].Role != want || !gs[i][0].roleAsked {
			t.Errorf("호스트 %d: %q asked=%t (기대 %q)", i, gs[i][0].Role, gs[i][0].roleAsked, want)
		}
	}
	for _, want := range []string{"벤더: " + vendorLabel("hpe") + ", 호스트 3대", "벤더: " + vendorLabel("dell") + ", 호스트 1대", "벤더 미확인, 호스트 1대", "bios.conf 에 user2_role 을 적으면"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("질문에 %q 없음:\n%s", want, out.String())
		}
	}
	// 같은 실행의 다음 흐름(예: 관리자 직접 입력 단계)에서 같은 벤더가 또 나와도 다시 묻지 않는다.
	more := u2Groups(env, "Dell", "HPE")
	if more[0][0].Role != roleAdministrator || more[1][0].Role != roleOperator || !more[0][0].roleAsked {
		t.Errorf("setRoles 가 답을 다시 쓰지 않음: %q %q", more[0][0].Role, more[1][0].Role)
	}
	late := newCreateHosts(Target{Hostname: "late"}, env.accounts()[1:]) // 권한을 아직 못 정한 칸 (setRoles 전)
	late[0].Status, late[0].vkey = acctNeedCreate, "dell"
	out.Reset()
	must(t, env.askUser2Roles(feedText(t, ""), &out, [][]*acctHost{late}, &accountFlags{}))
	if out.Len() != 0 || late[0].Role != roleAdministrator {
		t.Errorf("이미 답한 벤더를 다시 물음: %q role=%q", out.String(), late[0].Role)
	}
}

// 터미널이 아니거나 -no-prompt 면 묻지 않고 오류 (설정 안내 포함), 권한은 비어 있고 입력은 읽지 않는다.
func TestRoleAskNonInteractive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		tty     bool
		af      *accountFlags
		wantAsk bool
	}{
		{"터미널 아님", false, &accountFlags{}, false},
		{"-no-prompt", true, &accountFlags{noPrompt: true}, false},
		{"-stdin-ok 면 파이프에서도 묻는다", false, &accountFlags{stdinOK: true}, true},
	} {
		setTerminal(t, tc.tty)
		env := roleEnv(t, "", nil)
		gs := u2Groups(env, "HPE", "Dell")
		in := feedText(t, "1\n1\n")
		var out bytes.Buffer
		err := env.askUser2Roles(in, &out, gs, tc.af)
		if tc.wantAsk {
			if err != nil || gs[0][0].Role != roleAdministrator || gs[1][0].Role != roleAdministrator {
				t.Errorf("%s: %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "user2_role_<벤더>") || !strings.Contains(err.Error(), "user2_role=") ||
			!strings.Contains(err.Error(), vendorLabel("hpe")+" 1대") || out.Len() != 0 || gs[0][0].Role != "" {
			t.Errorf("%s: err=%v out=%q role=%q", tc.name, err, out.String(), gs[0][0].Role)
		}
		if n, _ := in.Read(make([]byte, 8)); n != 4 {
			t.Errorf("%s: 입력을 읽었음 (남은 %d바이트, 기대 4)", tc.name, n)
		}
	}
}

// 권한이 없는 칸은 쓰기 단계까지 오지 못한다 (Cisco 후보 계산·UCSM 관리자 역할로 새지 않게 막음).
func TestCreateRefusesEmptyRole(t *testing.T) {
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	env := roleEnv(t, "", nil)
	hs := newCreateHosts(acctTarget(t, s), env.accounts())
	env.createGroup(hs)
	if hs[0].Status != acctCreated || hs[1].Status != acctRejected || !strings.Contains(hs[1].Detail, "user2_role") {
		t.Fatalf("상태 %s / %s (%s)", hs[0].Status, hs[1].Status, hs[1].Detail)
	}
	if _, ok := findMockAcct(s, user2Name); ok {
		t.Error("권한이 없는데 user2 가 만들어짐")
	}
	// 같은 칸에 답을 넣고 다시 만들면 설정 값과 같은 길로 만들어진다 (HPE POST RoleId).
	s2 := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
	hs2 := newCreateHosts(acctTarget(t, s2), env.accounts())
	env.asked["hpe"] = roleReadOnly // askUser2Roles 가 넣는 것과 같음 (createGroup 이 벤더로 권한을 다시 정함)
	env.setRoles(hs2, "hpe")
	env.createGroup(hs2)
	if a, ok := findMockAcct(s2, user2Name); !ok || a.RoleID != "ReadOnly" || hs2[1].Status != acctCreated {
		t.Errorf("답한 권한으로 만들지 못함: %+v %s", a, hs2[1].Detail)
	}
}

// 설정에 없어 고른 권한은 이미 있는 user2 의 권한을 바꾸지 않는다 (설정 값일 때만 권한 변경).
func TestAskedRoleDoesNotChangeExisting(t *testing.T) {
	seed := func() []mockbmc.Account {
		return []mockbmc.Account{{UserName: user2Name, Password: "x", RoleID: "Administrator", Enabled: true}}
	}
	s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: seed()})
	env := roleEnv(t, "", nil)
	hs := newCreateHosts(acctTarget(t, s), env.accounts()[1:])
	env.asked["hpe"] = roleOperator
	env.setRoles(hs, "hpe")
	if hs[0].Role != roleOperator || !hs[0].roleAsked {
		t.Fatalf("setRoles: %q asked=%t", hs[0].Role, hs[0].roleAsked)
	}
	env.createGroup(hs)
	if a, _ := findMockAcct(s, user2Name); a.RoleID != "Administrator" || hs[0].Status != acctExists || !strings.Contains(hs[0].Detail, "바꾸지 않음") {
		t.Errorf("질문으로 고른 권한으로 기존 계정을 바꿈: %+v %s %s", a, hs[0].Status, hs[0].Detail)
	}
	// 설정 값이면 기존처럼 권한을 바꾼다.
	s2 := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{Slots: seed()})
	env2 := roleEnv(t, roleOperator, nil)
	hs2 := newCreateHosts(acctTarget(t, s2), env2.accounts()[1:])
	env2.setRoles(hs2, "hpe")
	env2.createGroup(hs2)
	if a, _ := findMockAcct(s2, user2Name); a.RoleID != "Operator" || hs2[0].roleAsked {
		t.Errorf("설정 값 권한 변경이 달라짐: %+v asked=%t", a, hs2[0].roleAsked)
	}
}

// 설정에 권한이 없으면 기존 user2 의 권한 비교는 하지 않는다 (기준이 없음).
func TestUser2RoleDiffersEmptyWant(t *testing.T) {
	if user2RoleDiffers(&acctMember{RoleID: "Administrator"}, "") {
		t.Error("want 가 비었는데 권한 변경 대상")
	}
	if !user2RoleDiffers(&acctMember{RoleID: "Administrator"}, roleOperator) || user2RoleDiffers(nil, roleOperator) {
		t.Error("기존 판단이 달라짐")
	}
}

// 점검 끝 제안 전체 흐름: user2 만 있고 권한 설정이 없으면 묻고, 답한 권한으로 user2 가 만들어진다.
func TestOfferAccountCreateAsksUser2Role(t *testing.T) {
	fastCheck(t)
	setup := func(t *testing.T) (*mockbmc.Server, *runContext, *checkRun) {
		s := acctMock(t, "hpe-dl360gen10", &mockbmc.AccountOptions{})
		rc := checkRCFor(t, 1, "", s.URL())
		rc.Fallback = fbCreds(t, fbConf("hpe"))
		dir := t.TempDir()
		rc.Conf.KeyFile, rc.Conf.Pass2File = filepath.Join(dir, "key.bin"), filepath.Join(dir, "pass2.enc")
		rc.Conf.User2, rc.Conf.User2Role = user2Name, ""
		must(t, encryptFromReader(strings.NewReader(user2Pass), rc.Conf.Pass2File, rc.Conf.KeyFile))
		run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
		must(t, err)
		return s, rc, run
	}
	// 2 Operator → 빈 줄(기본 Y)
	s, rc, run := setup(t)
	setTerminal(t, true)
	var out bytes.Buffer
	must(t, offerAccountCreate(rc, run, false, false, false, feedText(t, "2\n\n"), &out, reportOpts{}))
	a2, ok := findMockAcct(s, user2Name)
	if _, ok1 := findMockAcct(s, testUser); !ok1 || !ok || a2.RoleID != "Operator" || a2.Password != user2Pass {
		t.Fatalf("답한 권한으로 만들지 못함 %+v\n%s", s.Accounts(), out.String())
	}
	if !strings.Contains(out.String(), "user2_role_hpe 또는 user2_role") || !strings.Contains(out.String(), user2Name+"(Operator) 1대") {
		t.Errorf("질문·확인 문구:\n%s", out.String())
	}
	b, err := os.ReadFile(filepath.Join(run.Dir, "account_result.tsv"))
	must(t, err)
	if strings.Contains(string(b), user2Pass) || strings.Count(string(b), acctCreated) != 2 {
		t.Errorf("account_result.tsv: %s", b)
	}
	// 3 직접 입력 ReadOnly
	s, rc, run = setup(t)
	out.Reset()
	must(t, offerAccountCreate(rc, run, false, false, false, feedText(t, "3\nReadOnly\n\n"), &out, reportOpts{}))
	if a, ok := findMockAcct(s, user2Name); !ok || a.RoleID != "ReadOnly" {
		t.Errorf("직접 입력 권한: %+v\n%s", a, out.String())
	}
	// 비대화형: 묻지 않고 아무것도 만들지 않으며 설정 안내를 보인다.
	s, rc, run = setup(t)
	setTerminal(t, false)
	out.Reset()
	must(t, offerAccountCreate(rc, run, false, false, false, feedText(t, "2\n\n"), &out, reportOpts{}))
	if len(s.Accounts()) != 0 || strings.Contains(out.String(), "선택 [1-3]") ||
		!strings.Contains(out.String(), "user2_role_<벤더>") || !strings.Contains(out.String(), "계정을 만들지 않습니다") {
		t.Errorf("비대화형:\n%s", out.String())
	}
	// 설정에 user2_role 이 있으면 묻지 않는다 (기존 동작).
	s, rc, run = setup(t)
	rc.Conf.User2Role = roleReadOnly
	setTerminal(t, true)
	out.Reset()
	must(t, offerAccountCreate(rc, run, false, false, false, feedText(t, "\n"), &out, reportOpts{}))
	if a, ok := findMockAcct(s, user2Name); !ok || a.RoleID != "ReadOnly" || strings.Contains(out.String(), "선택 [1-3]") {
		t.Errorf("설정 값이 있는데 달라짐: %+v\n%s", a, out.String())
	}
}
