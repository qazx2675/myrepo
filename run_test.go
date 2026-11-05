package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runHarness 는 임시 작업 경로(cwd)에서 가짜 의존성으로 runEntry 를 돌린다.
type runHarness struct {
	t        *testing.T
	dir      string // 작업 경로
	alive    bool
	yes      bool
	started  int
	picked   int
	asked    int
	shown    int
	userGot  string
	errBuf   bytes.Buffer
	startErr error
}

func newHarness(t *testing.T) *runHarness {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	if err := os.WriteFile("hosts.txt", []byte("ev01\nev02\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("new_setup.conf", []byte("# 시험\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &runHarness{t: t, dir: tmp}
}

func (h *runHarness) deps() runDeps {
	return runDeps{
		alive: func(string) bool { return h.alive },
		start: func(string) (int, error) {
			h.started++
			return 4242, h.startErr
		},
		pickUser: func(c config, u string) (string, error) {
			h.picked++
			h.userGot = u
			return "tester", nil
		},
		confirm: func(string) bool { h.asked++; return h.yes },
		show:    func(string) { h.shown++ },
		tty:     true,
		out:     &bytes.Buffer{},
		err:     &h.errBuf,
	}
}

func (h *runHarness) writeState(stage int, stopped bool) string {
	h.t.Helper()
	dir := stateDirFor("hosts.txt")
	st, err := newRunState("hosts.txt", "", "", "olduser")
	if err != nil {
		h.t.Fatal(err)
	}
	st.Stage, st.Stopped = stage, stopped
	if err := saveState(dir, st); err != nil {
		h.t.Fatal(err)
	}
	return dir
}

// (a) 상태 없음: 사용자 선택 → 상태 생성 → 워커 기동 → 화면
func TestRunCaseNew(t *testing.T) {
	h := newHarness(t)
	if code := runEntry("hosts.txt", "", "", "u1", h.deps()); code != 0 {
		t.Fatalf("code=%d err=%s", code, h.errBuf.String())
	}
	if h.picked != 1 || h.userGot != "u1" || h.started != 1 || h.asked != 0 || h.shown != 1 {
		t.Errorf("picked=%d user=%q started=%d asked=%d shown=%d", h.picked, h.userGot, h.started, h.asked, h.shown)
	}
	st, err := loadState(filepath.Join(h.dir, "hosts.txt_run"))
	if err != nil || st.User != "tester" || st.Stage != stageJudge || st.ConfPath != filepath.Join(h.dir, "new_setup.conf") {
		t.Errorf("상태: %v %+v", err, st)
	}
	if !strings.Contains(h.errBuf.String(), "설정 파일") && !strings.Contains(h.errBuf.String(), "워커 시작") {
		t.Errorf("안내 없음: %s", h.errBuf.String())
	}
}

func TestRunCaseNewErrors(t *testing.T) {
	h := newHarness(t)
	if code := runEntry("nohost.txt", "", "", "", h.deps()); code != 2 || h.started != 0 {
		t.Errorf("없는 호스트 파일: code=%d started=%d", code, h.started)
	}
	os.WriteFile("empty.txt", []byte("# 없음\n"), 0o644)
	if code := runEntry("empty.txt", "", "", "", h.deps()); code != 2 || h.started != 0 {
		t.Errorf("빈 호스트 파일: code=%d", code)
	}
	if code := runEntry("hosts.txt", "nope.conf", "", "", h.deps()); code != 2 || h.started != 0 {
		t.Errorf("없는 -c: code=%d", code)
	}
	if code := runEntry("hosts.txt", "", "nope.txt", "", h.deps()); code != 2 || h.started != 0 {
		t.Errorf("없는 -er: code=%d", code)
	}
	h.startErr = os.ErrPermission
	if code := runEntry("hosts.txt", "", "", "", h.deps()); code != 2 {
		t.Errorf("워커 기동 실패: code=%d", code)
	}
	if h.shown != 0 {
		t.Error("오류인데 화면을 보임")
	}
	// 깨진 상태 파일
	dir := stateDirFor("hosts.txt")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, stateFile), []byte("{깨짐"), 0o644)
	if code := runEntry("hosts.txt", "", "", "", h.deps()); code != 2 {
		t.Errorf("깨진 상태: code=%d", code)
	}
}

// (b) 워커 살아 있음: 화면만 붙고 user 는 다시 묻지 않는다(-user 가 다르면 알림)
func TestRunCaseAttach(t *testing.T) {
	h := newHarness(t)
	h.writeState(stageLoad, false)
	h.alive = true
	if code := runEntry("hosts.txt", "", "", "other", h.deps()); code != 0 {
		t.Fatal(code)
	}
	if h.started != 0 || h.picked != 0 || h.asked != 0 || h.shown != 1 {
		t.Errorf("started=%d picked=%d asked=%d shown=%d", h.started, h.picked, h.asked, h.shown)
	}
	if !strings.Contains(h.errBuf.String(), "-user 는 무시") || !strings.Contains(h.errBuf.String(), "olduser") {
		t.Errorf("-user 무시 알림이 없음: %s", h.errBuf.String())
	}
	if !strings.Contains(h.errBuf.String(), "다시 붙으려면") {
		t.Errorf("재접속 안내가 없음: %s", h.errBuf.String())
	}
}

// (c) 워커 없음 + 미완료: y 면 재기동, N 이면 마지막 상태만
func TestRunCaseResume(t *testing.T) {
	h := newHarness(t)
	h.writeState(stageLoad, false)
	h.yes = true
	if code := runEntry("hosts.txt", "", "", "", h.deps()); code != 0 {
		t.Fatal(code)
	}
	if h.asked != 1 || h.started != 1 || h.shown != 1 || h.picked != 0 {
		t.Errorf("y: asked=%d started=%d shown=%d picked=%d", h.asked, h.started, h.shown, h.picked)
	}
	h2 := newHarness(t)
	h2.writeState(stageFix, false)
	h2.yes = false
	if code := runEntry("hosts.txt", "", "", "", h2.deps()); code != 0 {
		t.Fatal(code)
	}
	if h2.asked != 1 || h2.started != 0 || h2.shown != 1 {
		t.Errorf("N: asked=%d started=%d shown=%d", h2.asked, h2.started, h2.shown)
	}
	if !strings.Contains(h2.errBuf.String(), "상태 폴더를 지우세요") {
		t.Errorf("처음부터 다시 하는 법 안내가 없음: %s", h2.errBuf.String())
	}
}

// (d) 완료·중지됨: 묻지 않고 결과 화면, 결과 파일 경로 안내
func TestRunCaseFinished(t *testing.T) {
	for _, tc := range []struct {
		stage   int
		stopped bool
	}{{stageFinish, false}, {stageLoad, true}} {
		h := newHarness(t)
		dir := h.writeState(tc.stage, tc.stopped)
		if code := runEntry("hosts.txt", "", "", "", h.deps()); code != 0 {
			t.Fatal(code)
		}
		if h.asked != 0 || h.started != 0 || h.shown != 1 {
			t.Errorf("stage=%d: asked=%d started=%d shown=%d", tc.stage, h.asked, h.started, h.shown)
		}
		if !strings.Contains(h.errBuf.String(), filepath.Join(dir, resultFile)) {
			t.Errorf("결과 파일 경로 안내가 없음: %s", h.errBuf.String())
		}
	}
}

// tty 가 아니면 화면 뒤 안내(재접속 방법 등)를 내지 않는다
func TestRunNonTTYNoFooter(t *testing.T) {
	h := newHarness(t)
	h.writeState(stageLoad, false)
	h.alive = true
	d := h.deps()
	d.tty = false
	runEntry("hosts.txt", "", "", "", d)
	if strings.Contains(h.errBuf.String(), "다시 붙으려면") {
		t.Errorf("tty 가 아닌데 안내를 냄: %s", h.errBuf.String())
	}
}

func TestPrintPlain(t *testing.T) {
	h := newHarness(t)
	dir := h.writeState(stageFix, false)
	var b bytes.Buffer
	printPlain(&b, dir, func(string) bool { return true })
	if !strings.Contains(b.String(), "단계: 2 설정수정") || !strings.Contains(b.String(), header) {
		t.Errorf("글 출력:\n%s", b.String())
	}
	b.Reset()
	printPlain(&b, filepath.Join(h.dir, "없음"), func(string) bool { return false })
	if !strings.Contains(b.String(), "상태를 읽을 수 없습니다") {
		t.Errorf("오류 출력: %s", b.String())
	}
}

// user 선택: USER_ROUTE 가 없으면 -user → $USER
func TestSelectUserFlagAndEnv(t *testing.T) {
	if u, err := selectUser(config{}, "kim"); err != nil || u != "kim" {
		t.Errorf("-user: %q %v", u, err)
	}
	old := os.Getenv("USER")
	defer os.Setenv("USER", old)
	os.Setenv("USER", "envuser")
	if u, err := selectUser(config{}, ""); err != nil || u != "envuser" {
		t.Errorf("$USER: %q %v", u, err)
	}
	os.Setenv("USER", "")
	if _, err := selectUser(config{}, ""); err == nil {
		t.Error("user 를 알 수 없으면 오류여야 함")
	}
}

// user 선택: USER_ROUTE 의 info_mn.sh / info.sh 를 stress_run.sh 처럼 쓴다
func TestUserFromRoute(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 시험")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 없음")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "info_mn.sh"), []byte("echo '1) kim'\necho '2) lee'\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "info.sh"), []byte("case $1 in 1) echo kim;; 2) echo '  lee  ';; esac\n"), 0o644)
	var out bytes.Buffer
	u, err := userFromRoute(dir, bufio.NewReader(strings.NewReader("2\n")), &out, &out)
	if err != nil || u != "lee" {
		t.Errorf("user=%q err=%v", u, err)
	}
	if !strings.Contains(out.String(), "1) kim") || !strings.Contains(out.String(), "Input Number: ") {
		t.Errorf("메뉴 출력: %q", out.String())
	}
	// 잘못된 번호 → 빈 값 → 오류
	if _, err := userFromRoute(dir, bufio.NewReader(strings.NewReader("9\n")), &out, &out); err == nil {
		t.Error("빈 user 는 오류여야 함")
	}
	// selectUser 는 USER_ROUTE 가 있으면 -user 보다 우선(여기서는 stdin 을 못 바꾸므로 경로 유무만 확인)
	if _, err := userFromRoute(filepath.Join(dir, "없음"), bufio.NewReader(strings.NewReader("")), &out, &out); err == nil {
		t.Error("없는 폴더는 오류여야 함")
	}
}

func TestLoadConfAndErrList(t *testing.T) {
	h := newHarness(t)
	_, p, err := loadConf("")
	if err != nil || p != filepath.Join(h.dir, "new_setup.conf") {
		t.Errorf("자동 탐색: %q %v", p, err)
	}
	os.WriteFile("x.conf", []byte("GOSSH=/bin/true\n"), 0o644)
	c, p, err := loadConf("x.conf")
	if err != nil || p != filepath.Join(h.dir, "x.conf") || c.Gossh != "/bin/true" {
		t.Errorf("-c: %q %v %+v", p, err, c)
	}
	if _, _, err := loadConf("nope.conf"); err == nil {
		t.Error("-c 가 없으면 오류")
	}
	if _, p, err := loadErrList(""); err != nil || p != "" {
		t.Errorf("error_list 없음: %q %v", p, err)
	}
	os.WriteFile("error_list.txt", []byte("ev01\t팬\n"), 0o644)
	n, p, err := loadErrList("")
	if err != nil || p != filepath.Join(h.dir, "error_list.txt") || n["ev01"] != "팬" {
		t.Errorf("자동: %q %v %v", p, err, n)
	}
	if _, _, err := loadErrList("nope.txt"); err == nil {
		t.Error("-er 가 없으면 오류")
	}
}
