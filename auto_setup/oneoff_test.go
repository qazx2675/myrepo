// oneoff_test.go - x 체크스크립트 단독 실행: 호스트 정규화, user_route 파싱, 키 시퀀스(가짜 실행기), 임시 디렉터리 삭제
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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

// ---- 좁은 화면 줄바꿈 (S5b): x(체크스크립트 단독 실행)·w(AWX 실행) 화면 ----

const (
	wrapUser = "kim.donghyeon-with-a-very-long-account-name-for-wrapping-test_0123456789"
	wrapHost = "compute-rack-abcdefghijklmnopqrstuvwxyz-0123456789-and-a-very-long-suffix-without-spaces"
	wrapDir  = "/opt/hpc/awx/very/long/directory/name/for/the/awx_nodeinfo_scripts/current"
)

var wrapNow = time.Unix(1700000000, 0)

type wrapCase struct {
	name string
	text string
}

func wrapMenu(n int) []string {
	m := []string{"1) alice", "2) bob  (홍길동 책임 - 인프라팀 / 설정 담당)",
		"3) " + wrapUser + "  (아주 긴 설명이 붙은 사용자 항목입니다 - 줄바꿈 확인용)"}
	for i := 4; i <= n; i++ {
		m = append(m, fmt.Sprintf("%d) user%02d  (설명 %d)", i, i, i))
	}
	return m
}

// oneoffWrapRenders: x·w 의 user 메뉴/이름 입력, 호스트 입력(일반·AWX), 모드 선택, 실행 중(취소 확인 포함), 결과 창 렌더
func oneoffWrapRenders(t *testing.T, width, height int, color bool) []wrapCase {
	t.Helper()
	oldDir := awx_dir
	awx_dir = wrapDir
	t.Cleanup(func() { awx_dir = oldDir })

	logDir := t.TempDir()
	var lg strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&lg, "[%02d] %s 점검 중 — %s\n", i, wrapHost, strings.Repeat("긴 로그 줄 ", 6))
	}
	os.WriteFile(filepath.Join(logDir, "os_check.log"), []byte(lg.String()), 0644)
	proc := &fakeOneoff{dir: logDir, done: make(chan struct{})}

	var many strings.Builder
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&many, "node%03d.cluster.example.com ", i)
	}
	awxIn := "h1 h2\n" +
		"10.0.0.1 255.255.255.0 10.0.0.254 " + wrapHost + " rhel8 x86 gpu ib 100g y n 비고\n" +
		"10.0.0.2 255.255.255.0 10.0.0.254 node-b rhel8 x86 gpu ib 100g y n " + strings.Repeat("긴비고 ", 12) + "끝"
	runLong := &oneoffState{step: ooRun, user: wrapUser, hosts: make([]string, 40), proc: proc, started: wrapNow.Add(-75 * time.Second),
		msg: "실행 중에는 닫을 수 없습니다 — 취소는 Ctrl+X"}
	var states []wrapCase
	add := func(name string, o *oneoffState) {
		states = append(states, wrapCase{name, renderOneoff(o, wrapNow, width, height, color)})
	}
	add("user-menu", &oneoffState{step: ooUser, menu: wrapMenu(3), in: []rune("3"), msg: longMsg})
	add("user-menu-many", &oneoffState{step: ooUser, menu: wrapMenu(60), in: []rune("57")})
	add("user-name", &oneoffState{step: ooUser, in: []rune(wrapUser + wrapUser),
		msg: "user 메뉴를 찾을 수 없습니다 (awx_dir 의 01 user_route 확인) — user 이름을 직접 입력하세요"})
	add("hosts", &oneoffState{step: ooHosts, user: wrapUser,
		in: []rune("h1,h2 h3|h1\n" + wrapHost + " h4 h5\n" + strings.Repeat("host-abc ", 12) + "tail-END-0123")})
	add("hosts-many", &oneoffState{step: ooHosts, user: "bob", in: []rune(many.String() + "\n" + wrapHost + "-LAST")})
	add("hosts-awx", &oneoffState{step: ooHosts, user: wrapUser, awx: true, in: []rune(awxIn)})
	add("hosts-awx-empty", &oneoffState{step: ooHosts, user: "bob", awx: true, msg: longMsg})
	add("user-awx", &oneoffState{step: ooUser, awx: true, in: []rune(wrapUser), menu: wrapMenu(3)})
	add("mode", &oneoffState{step: ooMode, user: wrapUser, hosts: make([]string, 12)})
	add("run", runLong)
	ask := *runLong
	ask.cancelAsk = true
	add("run-cancel", &ask)

	v := &viewState{kind: viewOneoff, done: true, status: "완료 (rc=0)",
		title: fmt.Sprintf("체크스크립트 단독 실행 결과 [%s] - user %s %d대", modeLabel(""), wrapUser, 40),
		lines: []string{"작업 : 설정체크 + 설정수정", wrapHost + " : FAIL ntp 설정이 서버와 다릅니다 (기대값 chrony, 현재값 ntpd) — 수정 후 다시 확인",
			"h2 : ok", "최종 결과 : NO FAIL"}}
	states = append(states, wrapCase{"result", renderView(v, width, height, color)})
	return states
}

func checkWrapLayout(t *testing.T, what string, w, h int, color bool, r string) {
	t.Helper()
	lines := strings.Split(r, "\n")
	if h > 0 && len(lines) > h {
		t.Fatalf("%s 폭 %d 높이 %d: %d줄로 높이 초과\n%s", what, w, h, len(lines), stripANSI(r))
	}
	for _, l := range lines {
		p := stripANSI(l)
		if strWidth(p) > w {
			t.Fatalf("%s 폭 %d color=%v: 폭 초과(%d): %q", what, w, color, strWidth(p), p)
		}
		if w >= 60 && strings.Contains(p, "…") {
			t.Fatalf("%s 폭 %d color=%v: … 로 잘림: %q\n%s", what, w, color, p, stripANSI(r))
		}
		if i := strings.LastIndex(l, "\x1b["); i >= 0 && !strings.HasPrefix(l[i:], "\x1b[0m") {
			t.Fatalf("%s 폭 %d: 줄 끝 리셋 없음: %q", what, w, l)
		}
	}
}

// 폭 ≥ 60 이면 … 로 잘리지 않고 어떤 줄도 폭을 넘지 않는다 (40칸도 폭은 넘지 않는다)
func TestOneoffLayoutNoEllipsis(t *testing.T) {
	for _, w := range []int{40, 60, 61, 79, 80, 120} {
		for _, color := range []bool{false, true} {
			for _, h := range []int{0, 24, 40} {
				for _, c := range oneoffWrapRenders(t, w, h, color) {
					checkWrapLayout(t, "x/w "+c.name, w, h, color, c.text)
				}
			}
		}
	}
}

// 입력 줄(커서가 있는 마지막 줄)·실행 중 로그의 끝은 목록이 넘쳐도 화면 안에 남는다
func TestOneoffLayoutKeepsInputVisible(t *testing.T) {
	squash := func(s string) string { return strings.Join(strings.Fields(stripANSI(s)), "") }
	want := map[string]string{
		"user-menu-many": "57_", "user-name": wrapUser[len(wrapUser)-12:] + "_", "hosts": "tail-END-0123_",
		"hosts-many": "-LAST_", "hosts-awx": "끝_", "user-awx": wrapUser[len(wrapUser)-12:] + "_", "run": "긴로그줄",
	}
	for _, w := range []int{40, 60, 80, 120} {
		for _, h := range []int{14, 24} {
			for _, c := range oneoffWrapRenders(t, w, h, false) {
				if tail, ok := want[c.name]; ok && !strings.Contains(squash(c.text), tail) {
					t.Errorf("%s 폭 %d 높이 %d: %q 가 화면 밖\n%s", c.name, w, h, tail, c.text)
				}
			}
		}
	}
	// 로그는 최근 줄(29번)이 보이고, 생략된 앞쪽은 안내 줄로 표시. user 메뉴는 처음부터 보이고 아래를 생략 표시
	for _, c := range oneoffWrapRenders(t, 60, 24, false) {
		if c.name == "run" && (!strings.Contains(c.text, "[29]") || !strings.Contains(c.text, "(위 ")) {
			t.Errorf("실행 중 로그 꼬리/생략 안내 이상:\n%s", c.text)
		}
		if c.name == "run" { // 생략 안내 바로 아래는 로그 한 줄의 처음 (줄바꿈된 줄 중간에서 시작하지 않음)
			lines := strings.Split(c.text, "\n")
			for i, l := range lines {
				if strings.Contains(l, "(위 ") && !strings.HasPrefix(lines[i+1], " [") {
					t.Errorf("로그가 줄 중간에서 시작: %q\n%s", lines[i+1], c.text)
				}
			}
		}
		if c.name == "user-menu-many" && (!strings.Contains(c.text, "1) alice") || !strings.Contains(c.text, "(아래 ")) {
			t.Errorf("user 메뉴 처음/생략 안내 이상:\n%s", c.text)
		}
	}
}

// 한 줄에 들어가는 화면은 종전과 같은 출력 (모드 선택 화면 전체를 그대로 비교)
func TestOneoffLayoutWideSameAsBefore(t *testing.T) {
	o := &oneoffState{step: ooMode, user: "alice", hosts: []string{"h1", "h2", "h3"}}
	got := renderOneoff(o, wrapNow, 80, 10, false)
	sep := strings.Repeat("-", 80)
	want := strings.Join([]string{
		" 체크스크립트 단독 실행 - 모드 선택", sep,
		" user alice   호스트 3대", " ",
		"   t 설정체크만 (환경설정 수정 안 함)", "   c 설정체크 + 설정수정 (환경설정 수정함)",
		"", "", sep, " t / c 를 누르면 바로 실행합니다  Esc 취소"}, "\n")
	if got != want {
		t.Errorf("모드 선택 화면이 종전과 다름\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	// 색 on: t 줄은 초록, c 줄의 설정수정은 빨강
	c := renderOneoff(o, wrapNow, 80, 10, true)
	if !strings.Contains(c, "\x1b[32m   t 설정체크만 (환경설정 수정 안 함)\x1b[0m") ||
		!strings.Contains(c, "\x1b[32m   c 설정체크 + \x1b[0m\x1b[1;31m설정수정 (환경설정 수정함)\x1b[0m") {
		t.Errorf("모드 색:\n%s", goldenText(c))
	}
	u := &oneoffState{step: ooUser, in: []rune("ali")}
	lines := strings.Split(renderOneoff(u, wrapNow, 80, 8, false), "\n")
	if lines[2] != " user 이름 입력 > ali_" || len(lines) != 8 {
		t.Errorf("user 입력 화면:\n%s", strings.Join(lines, "\n"))
	}
}

// 호스트 에코는 종전의 이어 붙이기(폭 안에서 호스트 단위로 다음 줄)와 같은 줄바꿈
func TestOneoffLayoutHostEchoSameAsPacking(t *testing.T) {
	hosts := []string{"web01", "web02-long-name", "db1", "node-0001.example.com", "a", "bb", "ccc", "gpu-node-000123.cluster", "x",
		"login1", "login2", "storage-a.hpc.example.com", "n7", "n8", "n9"}
	pack := func(width int) []string { // 종전 구현 그대로 (본문 앞 한 칸 + 두 칸 들여쓰기)
		line := ""
		var rows []string
		for _, h := range hosts {
			if line != "" && strWidth(line)+1+strWidth(h) > width-3 {
				rows = append(rows, "   "+line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += h
		}
		if line != "" {
			rows = append(rows, "   "+line)
		}
		return rows
	}
	for w := 40; w <= 130; w += 9 {
		o := &oneoffState{step: ooHosts, user: "u", in: []rune(strings.Join(hosts, "\n"))}
		var got []string
		in := false
		for _, l := range strings.Split(renderOneoff(o, wrapNow, w, 200, false), "\n") {
			switch {
			case strings.HasPrefix(l, " 입력된 호스트 "):
				in = true
			case in && l == " ":
				in = false
			case in:
				got = append(got, l)
			}
		}
		if want := pack(w); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("폭 %d 호스트 에코 불일치\n--- got ---\n%s\n--- want ---\n%s", w, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
}

// 골든: 폭 60 (색 off/on) — user 메뉴·호스트 입력·AWX 입력·모드·실행 중·결과를 한 파일에
func TestOneoffLayoutGolden(t *testing.T) {
	for _, color := range []bool{false, true} {
		mode := "plain"
		if color {
			mode = "color"
		}
		var sb strings.Builder
		for _, c := range oneoffWrapRenders(t, 60, 24, color) {
			if c.name == "user-menu-many" || c.name == "hosts-awx-empty" || c.name == "run-cancel" {
				continue
			}
			sb.WriteString("== " + c.name + " ==\n" + c.text + "\n")
		}
		got := goldenText(sb.String())
		name := "tui_oneoff_" + mode + "_60.golden"
		path := filepath.Join("testdata", name)
		if *updateGolden {
			if err := os.WriteFile(path, []byte(got), 0644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("골든 없음 %s (go test -run OneoffLayoutGolden -update): %v", name, err)
		}
		if string(want) != got {
			t.Errorf("%s 불일치\n--- got ---\n%s", name, got)
		}
	}
}
