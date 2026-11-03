// sessionuser_test.go - 세션 user: needSessionUser(TUI), 헤더 표시, askUserCLI(CLI)
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// saveSessionState: forceUser·sessionUser·awx_dir·oneoffCmdOut 를 저장했다가 되돌린다 (forceUser·sessionUser·awx_dir 는 비워서 시작)
func saveSessionState(t *testing.T) {
	t.Helper()
	oF, oS, oA, oC := forceUser, sessionUser, awx_dir, oneoffCmdOut
	forceUser, sessionUser, awx_dir = "", "", ""
	t.Cleanup(func() { forceUser, sessionUser, awx_dir, oneoffCmdOut = oF, oS, oA, oC })
}

// useUserMenu: info_mn.sh/info.sh 를 가진 가짜 awx_dir — 1) alice 2) bob 3) ..(이름 규칙은 통과하지만 디렉터리로는 불가)
func useUserMenu(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "info_mn.sh"), []byte("echo '1) alice'\necho '2) bob'\necho '3) dot'\n"), 0755)
	os.WriteFile(filepath.Join(d, "info.sh"), []byte("case $1 in 1) echo alice;; 2) echo ' bob ';; 3) echo ..;; esac\n"), 0755)
	awx := t.TempDir()
	os.WriteFile(filepath.Join(awx, "01.AWX_nodeinfo_V2.sh"), []byte("main() {\n    user_route=\""+d+"\"\n}\n"), 0644)
	awx_dir = awx
}

func newSessionTUI(t *testing.T) *tuiState {
	t.Helper()
	env, _ := newRig(strings.NewReader(""))
	return &tuiState{src: &fakeSrc{snap: twoGroupSnap(), local: true}, env: env, pending: map[string]time.Time{}}
}

func typeKeys(st *tuiState, s string) {
	for _, ev := range parseKeys([]byte(s)) {
		st.handle(ev)
	}
}

func TestNeedSessionUserForceUser(t *testing.T) {
	saveSessionState(t)
	forceUser = "carol"
	st := newSessionTUI(t)
	var got []string
	st.needSessionUser(func(u string) { got = append(got, u) })
	if len(got) != 1 || got[0] != "carol" || st.oo != nil {
		t.Fatalf("got=%v oo=%v — -user 면 묻지 않고 바로 이어져야 함", got, st.oo)
	}
	if sessionUser != "" {
		t.Errorf("-user 는 세션 user 로 기억하지 않음: %q", sessionUser)
	}
}

func TestNeedSessionUserAsksOnceAndRemembers(t *testing.T) {
	saveSessionState(t)
	useUserMenu(t)
	st := newSessionTUI(t)
	var got []string
	next := func(u string) { got = append(got, u) }

	st.needSessionUser(next)
	if st.oo == nil || st.oo.step != ooUser || st.oo.onUser == nil || st.oo.menu == nil {
		t.Fatalf("user 선택 단계가 열려야 함: %+v", st.oo)
	}
	if len(got) != 0 {
		t.Fatalf("선택 전에는 이어지면 안 됨: %v", got)
	}
	if fr := renderOneoff(st.oo, st.env.Now(), 100, 24, false); !strings.Contains(fr, "이 세션에서 사용할 user 선택") || !strings.Contains(fr, "2) bob") {
		t.Errorf("선택 화면:\n%s", fr)
	}
	typeKeys(st, "2\r")
	if len(got) != 1 || got[0] != "bob" || sessionUser != "bob" || st.oo != nil {
		t.Fatalf("got=%v sessionUser=%q oo=%v", got, sessionUser, st.oo)
	}

	// 두 번째 호출은 묻지 않고 바로 이어진다
	st.needSessionUser(next)
	if len(got) != 2 || got[1] != "bob" || st.oo != nil {
		t.Fatalf("두 번째: got=%v oo=%v", got, st.oo)
	}
}

func TestNeedSessionUserNextMayOpenScreen(t *testing.T) {
	saveSessionState(t)
	useUserMenu(t)
	st := newSessionTUI(t)
	st.needSessionUser(func(u string) { st.oo = &oneoffState{step: ooHosts, user: u} }) // S5/S6 처럼 next 가 다음 화면을 연다
	typeKeys(st, "1\r")
	if st.oo == nil || st.oo.step != ooHosts || st.oo.user != "alice" || st.oo.onUser != nil {
		t.Fatalf("next 가 연 화면이 유지되어야 함: %+v", st.oo)
	}
}

func TestNeedSessionUserCancelAndInvalid(t *testing.T) {
	saveSessionState(t)
	useUserMenu(t)
	st := newSessionTUI(t)
	called := false
	st.needSessionUser(func(string) { called = true })
	typeKeys(st, "9\r") // info.sh 가 빈 값 → 이름 규칙 위반
	if st.oo == nil || !strings.Contains(st.oo.msg, "올바르지 않습니다") || called {
		t.Fatalf("잘못된 user 안내 후 선택 화면 유지: %+v called=%v", st.oo, called)
	}
	typeKeys(st, "3\r") // .. 는 디렉터리 이름으로 쓰이므로 불가
	if st.oo == nil || !strings.Contains(st.oo.msg, "..") || called || sessionUser != "" {
		t.Fatalf(".. 거부: %+v called=%v session=%q", st.oo, called, sessionUser)
	}
	typeKeys(st, "\x1b") // Esc 취소
	if st.oo != nil || called || sessionUser != "" {
		t.Fatalf("취소: oo=%v called=%v session=%q", st.oo, called, sessionUser)
	}
}

func TestNeedSessionUserDirectNameWithoutMenu(t *testing.T) {
	saveSessionState(t) // awx_dir 비어 있음 → 메뉴 없이 이름 직접 입력
	st := newSessionTUI(t)
	var got string
	st.needSessionUser(func(u string) { got = u })
	if st.oo == nil || st.oo.menu != nil || !strings.Contains(st.oo.msg, "직접 입력") {
		t.Fatalf("직접 입력 단계: %+v", st.oo)
	}
	typeKeys(st, "dave\r")
	if got != "dave" || sessionUser != "dave" {
		t.Errorf("got=%q session=%q", got, sessionUser)
	}
}

func TestOneoffUserStepUnchanged(t *testing.T) {
	// x/w/m 의 일반 user 단계는 세션 user 를 건드리지 않고 매번 묻는다
	saveSessionState(t)
	useUserMenu(t)
	sessionUser = "zed"
	st := newSessionTUI(t)
	st.oo = st.newUserStep(false, nil)
	if st.oo.step != ooUser || st.oo.onUser != nil {
		t.Fatalf("%+v", st.oo)
	}
	typeKeys(st, "1\r")
	if st.oo == nil || st.oo.step != ooHosts || st.oo.user != "alice" || sessionUser != "zed" {
		t.Fatalf("oo=%+v session=%q", st.oo, sessionUser)
	}
}

func TestHeaderUser(t *testing.T) {
	saveSessionState(t)
	hdr := func() string { return strings.Join(titleLines(Snapshot{}, 100, false), "\n") }
	if strings.Contains(hdr(), "[user:") {
		t.Errorf("user 없으면 표시 없음:\n%s", hdr())
	}
	sessionUser = "bob"
	if !strings.Contains(hdr(), "[user: bob]") {
		t.Errorf("세션 user 표시:\n%s", hdr())
	}
	forceUser = "alice" // -user 가 우선
	if h := hdr(); !strings.Contains(h, "[user: alice]") || strings.Contains(h, "bob") {
		t.Errorf("forceUser 우선:\n%s", h)
	}
}

func TestSessionUserResetOnTUIStart(t *testing.T) {
	saveSessionState(t)
	sessionUser = "stale"
	runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "q")
	if sessionUser != "" {
		t.Errorf("TUI 시작 때 비워져야 함: %q", sessionUser)
	}
}

func TestAskUserCLI(t *testing.T) {
	t.Run("-user 면 묻지 않음", func(t *testing.T) {
		saveSessionState(t)
		forceUser = "carol"
		var out strings.Builder
		u, err := askUserCLI(strings.NewReader("ignored\n"), &out)
		if u != "carol" || err != nil || out.Len() != 0 {
			t.Errorf("u=%q err=%v out=%q", u, err, out.String())
		}
	})
	t.Run("메뉴에서 번호 선택, 뒤따르는 입력은 소비하지 않음", func(t *testing.T) {
		saveSessionState(t)
		useUserMenu(t)
		in := strings.NewReader("2\nhost1 10.0.0.1 aa:bb\n")
		var out strings.Builder
		u, err := askUserCLI(in, &out)
		if u != "bob" || err != nil || sessionUser != "bob" {
			t.Fatalf("u=%q err=%v session=%q", u, err, sessionUser)
		}
		rest := make([]byte, 64)
		n, _ := in.Read(rest)
		if string(rest[:n]) != "host1 10.0.0.1 aa:bb\n" {
			t.Errorf("남은 입력 %q", rest[:n])
		}
		for _, w := range []string{"1) alice", "2) bob", "번호 입력 >"} {
			if !strings.Contains(out.String(), w) {
				t.Errorf("출력에 %q 없음:\n%s", w, out.String())
			}
		}
		// 같은 프로세스에서 다시 부르면 묻지 않음
		out.Reset()
		if u2, _ := askUserCLI(strings.NewReader(""), &out); u2 != "bob" || out.Len() != 0 {
			t.Errorf("재호출: %q out=%q", u2, out.String())
		}
	})
	t.Run("메뉴 없으면 이름 직접 입력 (CRLF 허용, 잘못된 입력 뒤 재시도)", func(t *testing.T) {
		saveSessionState(t)
		var out strings.Builder
		u, err := askUserCLI(strings.NewReader("a b\r\n\r\ndave\r\n"), &out)
		if u != "dave" || err != nil {
			t.Fatalf("u=%q err=%v", u, err)
		}
		if !strings.Contains(out.String(), "직접 입력") || !strings.Contains(out.String(), "올바르지 않습니다") {
			t.Errorf("안내:\n%s", out.String())
		}
	})
	t.Run("입력 없이 끝나면 오류", func(t *testing.T) {
		saveSessionState(t)
		var out strings.Builder
		if u, err := askUserCLI(strings.NewReader(""), &out); err == nil || u != "" {
			t.Errorf("u=%q err=%v", u, err)
		}
	})
	t.Run("3번 틀리면 오류", func(t *testing.T) {
		saveSessionState(t)
		var out strings.Builder
		if u, err := askUserCLI(strings.NewReader("a b\nc d\n..\n"), &out); err == nil || u != "" || sessionUser != "" {
			t.Errorf("u=%q err=%v session=%q", u, err, sessionUser)
		}
	})
	t.Run("info.sh 실패", func(t *testing.T) {
		saveSessionState(t)
		useUserMenu(t)
		oneoffCmdOut = func(dir, script string, args ...string) (string, error) {
			if strings.HasSuffix(script, "info_mn.sh") {
				return "1) alice\n", nil
			}
			return "", errors.New("boom")
		}
		var out strings.Builder
		u, err := askUserCLI(strings.NewReader("1\n"), &out)
		if err == nil || u != "" || !strings.Contains(out.String(), "user 조회 실패") {
			t.Errorf("u=%q err=%v out=%s", u, err, out.String())
		}
	})
}
