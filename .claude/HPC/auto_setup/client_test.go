// client_test.go - 원격 클라이언트 모드: 가짜 gossh(sh 스텁)로 로컬 출력 == 원격 클라이언트 출력, quoting, 응답 파싱
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// 가짜 gossh: `-pm -script -w FILE CMD` 를 받아 CMD 를 로컬 sh 로 실행하고, 실제 gossh 처럼 줄마다 공백을 다듬고
// 빈 줄을 버린 뒤 "host: " 접두를 붙여 stdout 으로 낸다. 호출 인자는 $STUB_LOG 에 한 줄씩 남긴다.
const stubGossh = `#!/bin/sh
[ "$1 $2 $3" = "-pm -script -w" ] || { echo "bad args: $*" >&2; exit 2; }
f="$4"; cmd="$5"
host=$(head -n1 "$f")
[ -n "$STUB_LOG" ] && printf '%s\n' "$cmd" >> "$STUB_LOG"
sh -c "$cmd" 2>/dev/null | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' | grep -v '^$' | sed "s/^/$host: /"
exit 0
`

const testMgmt = "mgmt01"

var (
	buildOnce           sync.Once
	localBin, clientBin string
	buildErr            error
	buildDir            string
)

// buildBins: 로컬 바이너리 1개 + 원격 클라이언트 바이너리(os8_mgmt/os8_autosetup 를 -ldflags -X 로 채움) 빌드
func buildBins(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go 없음")
	}
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "as_bins_")
		if buildErr != nil {
			return
		}
		localBin = filepath.Join(buildDir, "auto_setup")
		clientBin = filepath.Join(buildDir, "auto_setup_client")
		if out, err := exec.Command("go", "build", "-o", localBin, ".").CombinedOutput(); err != nil {
			buildErr = &buildFailure{string(out)}
			return
		}
		ld := "-X main.os8_mgmt=" + testMgmt + " -X main.os8_autosetup=" + localBin
		if out, err := exec.Command("go", "build", "-ldflags", ld, "-o", clientBin, ".").CombinedOutput(); err != nil {
			buildErr = &buildFailure{string(out)}
		}
	})
	if buildErr != nil {
		t.Fatalf("빌드 실패: %v", buildErr)
	}
	return localBin, clientBin
}

type buildFailure struct{ msg string }

func (b *buildFailure) Error() string { return b.msg }

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// stubEnv: PATH 맨 앞에 가짜 gossh 를 둔 환경 (+ 상태 디렉터리·고정 시각)
func stubEnv(t *testing.T, dir string) (env []string, logFile string) {
	t.Helper()
	sd := t.TempDir()
	if err := os.WriteFile(filepath.Join(sd, "gossh"), []byte(stubGossh), 0755); err != nil {
		t.Fatal(err)
	}
	logFile = filepath.Join(sd, "calls.log")
	env = append(os.Environ(), "PATH="+sd+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AUTO_SETUP_DIR="+dir, "AUTO_SETUP_NOW=4700", "STUB_LOG="+logFile, "NO_COLOR=1")
	return env, logFile
}

type cliOut struct {
	Out, Err string
	RC       int
}

func runBin(t *testing.T, bin string, env []string, args ...string) cliOut {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	rc := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("%s %v: %v", bin, args, err)
		}
		rc = ee.ExitCode()
	}
	return cliOut{o.String(), e.String(), rc}
}

// 상태 디렉터리 준비: 스냅샷 fixture + 앞뒤 공백·빈 줄·줄바꿈 없는 끝을 가진 code 파일
func prepState(t *testing.T) string {
	dir := t.TempDir()
	snapFixture(t, dir)
	body := "  들여쓴 줄\n\n\t탭 줄  \n마지막 줄(개행 없음)"
	if err := os.WriteFile(filepath.Join(dir, "codes", "1234.txt"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRemoteEqualsLocal(t *testing.T) {
	lb, cb := buildBins(t)
	dir := prepState(t)
	env, _ := stubEnv(t, dir)
	cases := [][]string{
		{"status"},
		{"snapshot"},
		{"code", "1234"},
		{"code", "9999"},
		{"code", "12"},
		{"cancel", "nojob"},
		{"cancel"},
		{"snapshot", "x"},
		{"request", "bogus"},
		{"done"},
	}
	for _, args := range cases {
		l := runBin(t, lb, env, args...)
		r := runBin(t, cb, env, args...)
		if l != r {
			t.Errorf("%v 로컬/원격 불일치\n로컬: %+v\n원격: %+v", args, l, r)
		}
	}
	// 의미 있는 출력이었는지(빈 비교 방지)
	if l := runBin(t, lb, env, "code", "1234"); l.Out != "  들여쓴 줄\n\n\t탭 줄  \n마지막 줄(개행 없음)" {
		t.Fatalf("code 출력: %q", l.Out)
	}
	if l := runBin(t, lb, env, "status"); !strings.Contains(l.Out, "20251002-153000-u1") {
		t.Fatalf("status 출력: %q", l.Out)
	}
	if l := runBin(t, lb, env, "snapshot"); !strings.Contains(l.Out, `"now": 4700`) {
		t.Fatalf("snapshot 출력: %q", l.Out)
	}
	if r := runBin(t, cb, env, "code", "9999"); r.RC != 1 || r.Out != "[X] code 없음\n" {
		t.Fatalf("원격 종료코드/출력 전달: %+v", r)
	}
	if r := runBin(t, cb, env, "cancel", "nojob"); r.RC != 1 || !strings.HasPrefix(r.Err, "[X]") {
		t.Fatalf("원격 stderr 전달: %+v", r)
	}
}

func TestRemoteEmptyState(t *testing.T) {
	lb, cb := buildBins(t)
	env, _ := stubEnv(t, t.TempDir())
	for _, args := range [][]string{{"status"}, {"snapshot"}} {
		if l, r := runBin(t, lb, env, args...), runBin(t, cb, env, args...); l != r {
			t.Errorf("%v 불일치\n%+v\n%+v", args, l, r)
		}
	}
	if r := runBin(t, cb, env, "status"); r.Out != "진행 중인 작업 없음\n" {
		t.Fatalf("status: %q", r.Out)
	}
}

func TestRemoteRejectsDaemonControl(t *testing.T) {
	_, cb := buildBins(t)
	env, logFile := stubEnv(t, t.TempDir())
	for _, a := range []string{"--start", "start", "--stop", "stop", "--restart", "restart", "daemon", "ensure"} {
		r := runBin(t, cb, env, a)
		if r.RC != 1 || r.Out != "" || !strings.Contains(r.Err, "[X] 데몬은 os8_mgmt 에서만 실행합니다") {
			t.Errorf("%s 거부 아님: %+v", a, r)
		}
	}
	if b, _ := os.ReadFile(logFile); len(b) != 0 {
		t.Fatalf("거부인데 gossh 호출됨: %s", b)
	}
}

func TestRemoteDoneAndRequest(t *testing.T) {
	_, cb := buildBins(t)
	dir := prepState(t)
	env, _ := stubEnv(t, dir)
	if r := runBin(t, cb, env, "done", "h1", "h2"); r.RC != 0 || !strings.Contains(r.Out, "[O] 완료 처리 요청: 2대") {
		t.Fatalf("done: %+v", r)
	}
	b, err := os.ReadFile(filepath.Join(dir, "done", "h2"))
	if err != nil || len(strings.Fields(string(b))) != 4 || !strings.HasSuffix(strings.TrimSpace(string(b)), " manual") {
		t.Fatalf("done 기록: %q %v", b, err)
	}
	if r := runBin(t, cb, env, "request", "manual-run", "20251002-153000-u1", "a.yml"); r.RC != 0 || !strings.HasPrefix(r.Out, "[O] 요청 등록: ") {
		t.Fatalf("request: %+v", r)
	}
	m, _ := filepath.Glob(filepath.Join(dir, "requests", "*_manual-run*.req"))
	if len(m) != 1 {
		t.Fatalf("요청 파일: %v", m)
	}
	body, _ := os.ReadFile(m[0])
	if string(body) != "jobid=20251002-153000-u1\nyml=a.yml\n" {
		t.Fatalf("요청 내용: %q", body)
	}
}

// remoteSource: 같은 상태에 대해 localSource 와 같은 스냅샷·code·요청
func TestRemoteSource(t *testing.T) {
	lb, _ := buildBins(t)
	dir := prepState(t)
	env, _ := stubEnv(t, dir)
	for _, kv := range env {
		if i := strings.Index(kv, "="); i > 0 {
			t.Setenv(kv[:i], kv[i+1:])
		}
	}
	oldM, oldA := os8_mgmt, os8_autosetup
	os8_mgmt, os8_autosetup = testMgmt, lb
	defer func() { os8_mgmt, os8_autosetup = oldM, oldA }()

	var rs SnapshotSource = newSource()
	if rs.Local() {
		t.Fatal("os8_mgmt 설정인데 Local()")
	}
	got, err := rs.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := localSource{}.Snapshot()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("스냅샷 불일치\n%+v\n%+v", got, want)
	}
	if c, err := rs.Code("1234"); err != nil || !strings.HasPrefix(c, "  들여쓴 줄\n\n") {
		t.Fatalf("code: %q %v", c, err)
	}
	if _, err := rs.Code("9999"); err == nil {
		t.Fatal("없는 code 는 에러여야 함")
	}
	if err := rs.Request(ReqCancel, map[string]string{"jobid": "20251002-153000-u1"}); err != nil {
		t.Fatal(err)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "requests", "*_cancel*.req")); len(m) != 1 {
		t.Fatalf("cancel 요청 파일: %v", m)
	}
	if err := rs.Request("nope", nil); err == nil {
		t.Fatal("지원하지 않는 kind 는 에러여야 함")
	}
	os8_mgmt = ""
	if !newSource().Local() {
		t.Fatal("os8_mgmt 비었는데 원격")
	}
}

// quoting: 공백·작은따옴표·셸 메타문자가 원격 인자로 그대로 전달된다
func TestRemoteCommandQuoting(t *testing.T) {
	dir := t.TempDir()
	argv := filepath.Join(dir, "argv")
	bin := filepath.Join(dir, "fake bin")
	os.WriteFile(bin, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '<%s>\\n' \"$a\" >> "+argv+"; done\nprintf 'out\\n'\nprintf 'err\\n' >&2\nexit 3\n"), 0755)
	oldM, oldA := os8_mgmt, os8_autosetup
	os8_mgmt, os8_autosetup = testMgmt, bin
	defer func() { os8_mgmt, os8_autosetup = oldM, oldA }()
	oldRun := runExternal
	var gotArgs []string
	runExternal = func(stdin string, name string, args ...string) (string, error) {
		gotArgs = append([]string{name}, args...)
		out, _ := exec.Command("sh", "-c", args[len(args)-1]).Output()
		var sb strings.Builder
		for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			sb.WriteString(testMgmt + ": " + l + "\n")
		}
		return sb.String(), nil
	}
	defer func() { runExternal = oldRun }()

	weird := []string{"a b", "it's", `x"y`, "$HOME;rm -rf /", "`id`", "*"}
	r, err := remoteRun(weird)
	if err != nil {
		t.Fatal(err)
	}
	if r.RC != 3 || r.Stdout != "out\n" || r.Stderr != "err\n" {
		t.Fatalf("결과: %+v", r)
	}
	b, _ := os.ReadFile(argv)
	want := "<a b>\n<it's>\n<x\"y>\n<$HOME;rm -rf />\n<`id`>\n<*>\n"
	if string(b) != want {
		t.Fatalf("원격 인자:\n%q\n%q", b, want)
	}
	if len(gotArgs) != 6 || gotArgs[0] != "gossh" || !reflect.DeepEqual(gotArgs[1:4], []string{"-pm", "-script", "-w"}) {
		t.Fatalf("gossh 호출 형식: %v", gotArgs)
	}
	if strings.ContainsAny(gotArgs[5], "\n") {
		t.Fatalf("원격 명령은 한 줄이어야 함(csh 로그인 셸 대비): %q", gotArgs[5])
	}
	if m, _ := filepath.Glob(gotArgs[4] + "*"); len(m) != 0 {
		t.Fatalf("임시 목록 파일이 남음: %v", m)
	}
}

func TestParseRemote(t *testing.T) {
	good := "banner line\nh: ==RC==2\nh: ==O==\nh: aGVs\nh: bG8K\nh: ==E==\nh: ZXJyCg==\nother: ==RC==9\n"
	r, err := parseRemote("h", good)
	if err != nil || r.RC != 2 || r.Stdout != "hello\n" || r.Stderr != "err\n" {
		t.Fatalf("정상 파싱: %+v %v", r, err)
	}
	for name, in := range map[string]string{
		"빈 출력":      "",
		"표지 누락":     "h: ==RC==0\nh: ==O==\n",
		"접속 불가":     "",
		"RC 형식":     "h: ==RC==x\nh: ==O==\nh: ==E==\n",
		"base64 깨짐": "h: ==RC==0\nh: ==O==\nh: !!!\nh: ==E==\n",
		"다른 호스트":    "z: ==RC==0\nz: ==O==\nz: ==E==\n",
	} {
		if _, err := parseRemote("h", in); err == nil {
			t.Errorf("%s: 에러여야 함", name)
		}
	}
	// 출력이 비어 있어도(빈 stdout/stderr) 정상
	r, err = parseRemote("h", "h: ==RC==0\nh: ==O==\nh: ==E==\n")
	if err != nil || r.Stdout != "" || r.Stderr != "" || r.RC != 0 {
		t.Fatalf("빈 출력: %+v %v", r, err)
	}
}

func TestRemoteTransportFailure(t *testing.T) {
	_, cb := buildBins(t)
	sd := t.TempDir()
	os.WriteFile(filepath.Join(sd, "gossh"), []byte("#!/bin/sh\necho 'mgmt01: ERROR: dial' >&2\nexit 1\n"), 0755)
	env := append(os.Environ(), "PATH="+sd+string(os.PathListSeparator)+os.Getenv("PATH"), "AUTO_SETUP_DIR="+t.TempDir(), "NO_COLOR=1")
	r := runBin(t, cb, env, "status")
	if r.RC != 1 || r.Out != "" || !strings.Contains(r.Err, "[X] "+testMgmt+" 원격 실행 실패") {
		t.Fatalf("전송 실패: %+v", r)
	}
}
