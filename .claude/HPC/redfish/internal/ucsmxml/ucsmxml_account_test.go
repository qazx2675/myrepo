package ucsmxml

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 계정 모드(Options.Account) 시험. 가짜 UCSM(httptest)만 쓴다.

const newUserPW = `N3w<"&'>p@ss!`

func accountReply(body string) string {
	switch {
	case strings.HasPrefix(body, "<aaaLogin"):
		return fmt.Sprintf(`<aaaLogin cookie="" response="yes" outCookie="%s"> </aaaLogin>`, tCookie)
	case strings.HasPrefix(body, "<configResolveClass") && strings.Contains(body, `classId="aaaUser"`):
		return `<configResolveClass cookie="" response="yes" classId="aaaUser"><outConfigs>` +
			`<aaaUser name="admin" dn="sys/user-ext/user-admin"/><aaaUser name="ops1" dn="sys/user-ext/user-ops1"/></outConfigs></configResolveClass>`
	case strings.HasPrefix(body, "<configConfMo") && strings.Contains(body, `status="created"`):
		return `<configConfMo cookie="" response="yes"><outConfig><aaaUser name="x" pwdSet="yes"/></outConfig></configConfMo>`
	case strings.HasPrefix(body, "<configConfMo"):
		return `<configConfMo cookie="" response="yes"><outConfig><aaaUser name="x" pwdSet="yes"/></outConfig></configConfMo>`
	}
	return `<aaaLogout cookie="" response="yes" outStatus="success"/>`
}

func newAcct(url string) *Client {
	return New(Options{BaseURL: url, User: tUser, Pass: tPass, Insecure: true, Timeout: 5 * time.Second, Account: true})
}

func TestAccountGateRejects(t *testing.T) {
	s, ts := newSrv(t, accountReply)
	c := newAcct(ts.URL)
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	c.targets["alice"] = true
	c.created["alice"] = true
	ck := tCookie
	user := func(name, attrs, kids, status string) string {
		dn := "sys/user-ext/user-" + name
		return `<configConfMo cookie="` + ck + `" dn="` + dn + `" inHierarchical="false"><inConfig><aaaUser name="` + name + `" ` + attrs +
			`dn="` + dn + `" status="` + status + `">` + kids + `</aaaUser></inConfig></configConfMo>`
	}
	okAttrs := `pwd="p" accountStatus="active" expires="no" firstName="" lastName="" email="" phone="" `
	role := `<aaaUserRole name="admin" status="created"/>`
	bad := []string{
		user("alice", okAttrs, `<aaaUserRole name="server-equipment" status="created"/>`, "created"),
		user("alice", okAttrs, `<aaaUserRole name="aaa" status="created"/>`, "created"),
		user("alice", okAttrs, role+role, "created"),
		user("alice", okAttrs, "", "created"),
		user("alice", okAttrs, role, "created,modified"),
		user("alice", okAttrs, role, "deleted"),
		user("alice", `pwd="p" accountStatus="active" expires="no" `, role, "created"),
		user("alice", okAttrs+`clearPwdHistory="yes" `, role, "created"),
		user("bob", `pwd="p" `, "", "modified"), // 허용 등록되지 않은 이름
		user("alice", `pwd="p" accountStatus="inactive" `, "", "modified"),
		user("bob", "", "", "deleted"),           // 이번에 만들지 않은 이름
		user("1bad", okAttrs, role, "created"),   // 이름 형식
		user("alice", `pwd="" `, "", "modified"), // 빈 비밀번호
		`<configConfMo cookie="` + ck + `" dn="sys/user-ext/pwd-profile" inHierarchical="false"><inConfig><aaaPwdProfile historyCount="5" status="modified"/></inConfig></configConfMo>`,
		`<configConfMo cookie="` + ck + `" dn="org-root/bios-prof-x" inHierarchical="false"><inConfig><biosVProfile name="x" status="modified"/></inConfig></configConfMo>`,
		`<configConfMo cookie="` + ck + `" dn="sys/user-ext/user-alice" inHierarchical="true"><inConfig><aaaUser name="alice" pwd="p" dn="sys/user-ext/user-alice" status="modified"/></inConfig></configConfMo>`,
		`<configConfMo cookie="other" dn="sys/user-ext/user-alice" inHierarchical="false"><inConfig><aaaUser name="alice" pwd="p" dn="sys/user-ext/user-alice" status="modified"/></inConfig></configConfMo>`,
		`<configConfMos cookie="` + ck + `" inHierarchical="false"><inConfigs/></configConfMos>`,
		`<configConfMo cookie="` + ck + `" dn="sys/user-ext/user-alice" inHierarchical="false"><inConfig><aaaUser name="alice" pwd="p" dn="sys/user-ext/user-alice" status="modified"/><aaaUser name="bob" pwd="p" dn="sys/user-ext/user-bob" status="modified"/></inConfig></configConfMo>`,
		`<configConfMo cookie="` + ck + `" dn="sys/user-ext/user-alice" inHierarchical="false"><inConfig/></configConfMo>`,
		`<configConfMo cookie="` + ck + `" dn="sys/user-ext/user-alice" inHierarchical="false"/>`,
	}
	before := s.n()
	for _, b := range bad {
		if _, err := c.post("test", []byte(b)); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("거부되어야 함: %s → %v", b, err)
		}
	}
	if n := s.n() - before; n != 0 {
		t.Errorf("거부된 요청인데 서버가 %d건을 받음", n)
	}
	// 허용되는 형태 (대조군)
	good := []string{
		user("alice", okAttrs, role, "created"),
		user("alice", `pwd="p" `, "", "modified"),
		user("alice", "", "", "deleted"),
	}
	for _, b := range good {
		if err := c.gate([]byte(b)); err != nil {
			t.Errorf("허용되어야 함: %s → %v", b, err)
		}
	}
}

func TestNonAccountClientCannotWrite(t *testing.T) {
	s, ts := newSrv(t, accountReply)
	c := newC(ts.URL) // Account=false
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	before := s.n()
	if err := c.CreateUser("alice", "p"); err == nil {
		t.Error("계정 모드가 아닌데 CreateUser 가 성공")
	}
	if _, err := c.ListUsers(); err == nil {
		t.Error("계정 모드가 아닌데 ListUsers 가 성공")
	}
	body := `<configConfMo cookie="` + tCookie + `" dn="sys/user-ext/user-alice" inHierarchical="false"><inConfig><aaaUser name="alice" pwd="p" dn="sys/user-ext/user-alice" status="modified"/></inConfig></configConfMo>`
	if _, err := c.post("test", []byte(body)); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("읽기 전용 모드에서 configConfMo 가 허용됨: %v", err)
	}
	if s.n() != before {
		t.Error("쓰기 요청이 서버에 도달함")
	}
}

func TestAccountCreateChangeDeleteFlow(t *testing.T) {
	s, ts := newSrv(t, accountReply)
	c := newAcct(ts.URL)
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	users, err := c.ListUsers()
	if err != nil || len(users) != 2 || users[0] != "admin" {
		t.Fatalf("ListUsers %v %v", users, err)
	}
	// 등록 전에는 비밀번호 변경·삭제가 막힌다.
	if err := c.SetUserPassword("ops1", newUserPW); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("미등록 사용자 변경이 허용됨: %v", err)
	}
	if err := c.DeleteUser("ops1"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("만들지 않은 사용자 삭제가 허용됨: %v", err)
	}
	if err := c.CreateUser("newadm", newUserPW); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := c.AllowUserTarget("ops1"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetUserPassword("ops1", newUserPW); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	if err := c.DeleteUser("newadm"); err != nil {
		t.Fatalf("DeleteUser(만든 사용자): %v", err)
	}
	// 요청 본문: 비밀번호는 XML 이스케이프되어 나가고 원문 특수문자는 속성 안에 그대로 있지 않다.
	s.mu.Lock()
	var create string
	for _, b := range s.bodies {
		if strings.Contains(b, `status="created"`) {
			create = b
		}
	}
	s.mu.Unlock()
	if create == "" || strings.Contains(create, newUserPW) || !strings.Contains(create, `<aaaUserRole name="admin" status="created"/>`) {
		t.Errorf("생성 본문이 이상함: %s", create)
	}
	if err := c.Logout(); err != nil {
		t.Fatal(err)
	}
}

// 두 번째 계정(user2_role): operations / read-only 역할로도 만들 수 있고, 그 밖의 역할은 보내지 않는다.
func TestCreateUserRole(t *testing.T) {
	s, ts := newSrv(t, accountReply)
	c := newAcct(ts.URL)
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateUserRole("ro1", newUserPW, "read-only"); err != nil {
		t.Fatalf("read-only: %v", err)
	}
	before := s.n()
	if err := c.CreateUserRole("x1", newUserPW, "aaa"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("허용목록 밖의 역할이 허용됨: %v", err)
	}
	if s.n() != before {
		t.Error("거부된 역할 요청이 서버에 도달함")
	}
	s.mu.Lock()
	found := false
	for _, b := range s.bodies {
		found = found || strings.Contains(b, `<aaaUserRole name="read-only" status="created"/>`)
	}
	s.mu.Unlock()
	if !found {
		t.Error("read-only 역할 본문이 없음")
	}
	if err := c.DeleteUser("ro1"); err != nil {
		t.Errorf("만든 사용자 롤백 삭제: %v", err)
	}
}

func TestAccountErrorsMaskNewPassword(t *testing.T) {
	_, ts := newSrv(t, func(b string) string {
		if strings.HasPrefix(b, "<configConfMo") {
			return fmt.Sprintf(`<configConfMo cookie="" response="yes" errorCode="103" errorDescr="bad password %s / %s"/>`, esc(newUserPW), escQuot(newUserPW))
		}
		return accountReply(b)
	})
	c := newAcct(ts.URL)
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	err := c.CreateUser("newadm", newUserPW)
	if err == nil {
		t.Fatal("거부 응답인데 성공")
	}
	for _, leak := range []string{newUserPW, esc(newUserPW), escQuot(newUserPW), tPass} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("오류에 비밀번호 노출: %q", leak)
		}
	}
	if !strings.Contains(err.Error(), "103") {
		t.Errorf("errorCode 가 보이지 않음: %v", err)
	}
}
