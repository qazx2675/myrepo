// oneoff_test.go - x 체크스크립트 단독 실행: 호스트 정규화, user_route 파싱, 키 시퀀스(가짜 실행기), 임시 디렉터리 삭제
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeOneoff struct {
	user   string
	mode   string
	hosts  []string
	starts int
	dir    string
	done   chan struct{}
	killed bool
	rc     int
}

func (f *fakeOneoff) Done() <-chan struct{} { return f.done }
func (f *fakeOneoff) Rc() int               { return f.rc }
func (f *fakeOneoff) LogPath() string       { return filepath.Join(f.dir, "os_check.log") }
func (f *fakeOneoff) Dir() string           { return f.dir }
func (f *fakeOneoff) Kill() {
	if !f.killed {
		f.killed = true
		close(f.done)
	}
}

// useFakeOneoff: os_check_sh 를 채우고 oneoffStart 를 가짜로 교체. finish=true 면 시작 즉시 종료한다.
func useFakeOneoff(t *testing.T, finish bool) *fakeOneoff {
	t.Helper()
	oldOS, oldAWX, oldStart := os_check_sh, awx_dir, oneoffStart
	t.Cleanup(func() { os_check_sh, awx_dir, oneoffStart = oldOS, oldAWX, oldStart })
	os_check_sh, awx_dir = "/x/os_check.sh", ""
	f := &fakeOneoff{done: make(chan struct{})}
	oneoffStart = func(osCheck, user, mode string, hosts []string) (oneoffProc, error) {
		dir, err := os.MkdirTemp("", "as_oneoff_test_")
		if err != nil {
			return nil, err
		}
		f.dir, f.user, f.mode, f.hosts = dir, user, mode, hosts
		f.starts++
		os.WriteFile(filepath.Join(dir, "os_check.log"), []byte("진행 로그 한 줄\nline2\n"), 0644)
		res := "check.res_" + user + "_postapply"
		if mode == ModeCheck {
			res = "check.res_" + user
		}
		os.WriteFile(filepath.Join(dir, res), []byte("h1 : FAIL ntp\nh2 : ok\n"), 0644)
		if finish {
			close(f.done)
		}
		return f, nil
	}
	t.Cleanup(func() {
		if f.dir != "" {
			os.RemoveAll(f.dir)
		}
	})
	return f
}

func TestNormalizeHosts(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"h1,h2 h3|h1\n", []string{"h1", "h2", "h3"}},
		{"  a\tb\r\nc,,d||  ", []string{"a", "b", "c", "d"}},
		{"", []string{}},
		{" , | \n", []string{}},
	}
	for _, c := range cases {
		if got := normalizeHosts(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("normalizeHosts(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestParseUserRoute(t *testing.T) {
	d := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(d, "01.sh")
		os.WriteFile(p, []byte(body), 0644)
		return p
	}
	cases := []struct{ body, want string }{
		{"a=1\nuser_route=\"/opt/u\"\nbash $user_route/x\n", "/opt/u"},
		{"f() {\n    user_route='/opt/v'\n}\n", "/opt/v"},
		{"user_route=\"\"\n", ""},
		{"echo nothing\n", ""},
		{"user_route=\"/first\"\nuser_route=\"/second\"\n", "/first"},
	}
	for _, c := range cases {
		if got := parseUserRoute(write(c.body)); got != c.want {
			t.Errorf("body=%q got %q want %q", c.body, got, c.want)
		}
	}
	if got := parseUserRoute(filepath.Join(d, "none")); got != "" {
		t.Errorf("없는 파일: %q", got)
	}
}

func TestParseKeysOneoffKeys(t *testing.T) {
	got := parseKeys([]byte("\x04\x18\x7f\x08\t"))
	want := []keyEv{{k: kRune, r: runeCtrlD}, {k: kRune, r: runeCtrlX}, {k: kRune, r: runeBackspace},
		{k: kRune, r: runeBackspace}, {k: kRune, r: '\t'}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestOneoffEmptyOSCheck(t *testing.T) {
	old := os_check_sh
	os_check_sh = ""
	defer func() { os_check_sh = old }()
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "x")
	if f := lastFrame(r); !strings.Contains(f, "os_check_sh 가 비어 있어 실행할 수 없습니다 (/etc/auto_setup/auto_setup.conf)") {
		t.Errorf("안내 없음:\n%s", f)
	}
}

func TestOneoffFlowCheckMode(t *testing.T) {
	f := useFakeOneoff(t, true)
	// user 직접 입력(user_route 없음) → 붙여넣기 → Ctrl+D → t
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "xalicX\x7fe\rh1,h2 h3|h1\n\x04t")
	if f.starts != 1 || f.user != "alice" || f.mode != ModeCheck || !reflect.DeepEqual(f.hosts, []string{"h1", "h2", "h3"}) {
		t.Fatalf("starts=%d user=%q mode=%q hosts=%v", f.starts, f.user, f.mode, f.hosts)
	}
	o := r.out.String()
	for _, want := range []string{
		"user 메뉴를 찾을 수 없습니다 (awx_dir 의 01 user_route 확인) — user 이름을 직접 입력하세요",
		"호스트를 입력하거나 붙여넣은 뒤 Ctrl+D 를 누르세요",
		"(공백·쉼표·탭·| 는 줄바꿈으로 바뀝니다, Esc 취소)",
		"입력된 호스트 3대",
		"t 설정체크만 (환경설정 수정 안 함)",
		"c 설정체크 + 설정수정 (환경설정 수정함)",
	} {
		if !strings.Contains(o, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
	fr := lastFrame(r)
	for _, want := range []string{"체크스크립트 단독 실행 결과 [설정체크만 (설정 수정 안 함)] - user alice 3대", "완료 (rc=0)", "h1 : FAIL ntp"} {
		if !strings.Contains(fr, want) {
			t.Errorf("결과 화면에 %q 없음:\n%s", want, fr)
		}
	}
	if _, err := os.Stat(f.dir); !os.IsNotExist(err) {
		t.Errorf("TUI 종료 후 임시 디렉터리가 남음: %s", f.dir)
	}
}

func TestOneoffApplyModeUsesPostapply(t *testing.T) {
	f := useFakeOneoff(t, true)
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "xbob\rhq\x04c")
	if f.mode != "" || !reflect.DeepEqual(f.hosts, []string{"hq"}) { // 입력창 안의 q 는 글자
		t.Fatalf("mode=%q hosts=%v", f.mode, f.hosts)
	}
	if fr := lastFrame(r); !strings.Contains(fr, "[설정체크 + 설정수정]") {
		t.Errorf("모드 표시 없음:\n%s", fr)
	}
}

func TestOneoffRunningIgnoresQEscAndCancel(t *testing.T) {
	f := useFakeOneoff(t, false)
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "xbob\rh1\x04c"+"q\x1bq"+"\x18n"+"\x18y")
	o := r.out.String()
	for _, want := range []string{
		"체크스크립트 단독 실행 [설정체크 + 설정수정] - user bob 1대",
		"실행 중에는 닫을 수 없습니다 — 취소는 Ctrl+X",
		"실행을 취소할까요? [y/n]",
		"진행 로그 한 줄",
	} {
		if !strings.Contains(o, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
	if f.starts != 1 || !f.killed {
		t.Errorf("starts=%d killed=%v", f.starts, f.killed)
	}
	if fr := lastFrame(r); !strings.Contains(fr, "취소됨") || !strings.Contains(fr, "line2") {
		t.Errorf("취소 결과 화면:\n%s", fr)
	}
}

func TestOneoffCancelNoKeepsRunning(t *testing.T) {
	useFakeOneoff(t, false)
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "xbob\rh1\x04t"+"\x18n")
	// (TUI 종료 시 정리로 프로세스는 종료되지만) n 이면 취소 결과 화면이 열리지 않고 실행 화면이 유지된다
	if fr := lastFrame(r); strings.Contains(fr, "취소됨") || !strings.Contains(fr, "실행 중") {
		t.Errorf("n 인데 취소됨:\n%s", fr)
	}
}

func TestOneoffUserMenuScripts(t *testing.T) {
	f := useFakeOneoff(t, true)
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "info_mn.sh"), []byte("echo '1) alice'\necho '2) bob'\n"), 0755)
	os.WriteFile(filepath.Join(d, "info.sh"), []byte("case $1 in 1) echo alice;; 2) echo ' bob ';; esac\n"), 0755)
	awx := t.TempDir()
	os.WriteFile(filepath.Join(awx, "01.AWX_nodeinfo_V2.sh"), []byte("x=1\nmain() {\n    user_route=\""+d+"\"\n}\n"), 0644)
	awx_dir = awx
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "x2\rh1\x04t")
	if !strings.Contains(r.out.String(), "2) bob") {
		t.Error("user 메뉴가 보이지 않음")
	}
	if f.user != "bob" {
		t.Errorf("user=%q want bob", f.user)
	}
}

func TestOneoffInvalidUserAndCancel(t *testing.T) {
	f := useFakeOneoff(t, true)
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "xa b\r\x1b")
	if !strings.Contains(r.out.String(), "user 이름이 올바르지 않습니다") {
		t.Error("잘못된 user 안내 없음")
	}
	if f.starts != 0 {
		t.Error("실행되면 안 됨")
	}
	if fr := lastFrame(r); strings.Contains(fr, "user 이름 입력") {
		t.Errorf("Esc 후에도 입력 화면:\n%s", fr)
	}
}

func TestOneoffQCancelsOnlyWhenEmptyAndNoHosts(t *testing.T) {
	f := useFakeOneoff(t, true)
	// 호스트 입력창이 비었을 때 q → 취소 (화면 1 로), 그 뒤 q 는 TUI 종료
	r := runKeys(t, &fakeSrc{snap: twoGroupSnap(), local: true}, "xu\r\x04q")
	if !strings.Contains(r.out.String(), "호스트가 없습니다") {
		t.Error("0대 안내 없음")
	}
	if f.starts != 0 {
		t.Error("실행되면 안 됨")
	}
}

func TestCloseViewRemovesDir(t *testing.T) {
	d := t.TempDir()
	sub := filepath.Join(d, "as_oneoff_x")
	os.Mkdir(sub, 0755)
	st := &tuiState{view: &viewState{kind: viewOneoff, cleanup: sub}}
	st.closeView()
	if st.view != nil {
		t.Error("view 가 닫히지 않음")
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Error("임시 디렉터리가 남음")
	}
}
