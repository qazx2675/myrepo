package main

import (
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLUTime(t *testing.T) {
	good := map[string]int{
		"2026111900": 2026111900,
		"2026111923": 2026111923,
		"2024022900": 2024022900, // 윤년
		"2025123123": 2025123123,
	}
	for in, want := range good {
		if v, msg := parseLUTime(in); msg != "" || v != want {
			t.Errorf("parseLUTime(%q) = %d, %q", in, v, msg)
		}
	}
	bad := []string{
		"", "abcdefghij", "202611190", "20261119000", "2026-11-19", " 2026111900",
		"2026131900", "2026001900", "2026113200", "2026110000", "2026023000", "2025022900", "2026111924",
	}
	for _, in := range bad {
		if v, msg := parseLUTime(in); msg == "" {
			t.Errorf("parseLUTime(%q) = %d, 오류여야 함", in, v)
		}
	}
}

func TestLURange(t *testing.T) {
	r := luRange{2026111900, 2026111923}
	if r.wrap() || r.startMDH() != 111900 || r.endMDH() != 111923 {
		t.Errorf("같은 해 범위 = wrap %v %d %d", r.wrap(), r.startMDH(), r.endMDH())
	}
	r = luRange{2025123100, 2026010100}
	if !r.wrap() || r.startMDH() != 123100 || r.endMDH() != 10100 {
		t.Errorf("연말 범위 = wrap %v %d %d", r.wrap(), r.startMDH(), r.endMDH())
	}
}

func TestAskLURangeRetries(t *testing.T) {
	var r luRange
	out := captureOutput(func() {
		withInput([]string{"abc", "202611190", "2026131900", "2026111900", "2026111800", "2026111923"}, func() { r = askLURange() })
	})
	if r.start != 2026111900 || r.end != 2026111923 {
		t.Errorf("범위 = %+v", r)
	}
	for _, want := range []string{
		"1. 시작 시간 (YYYYMMDDHH, 예: 2026111900) : ", "2. 끝 시간 (YYYYMMDDHH, 예: 2026111923) : ",
		"숫자 10자리", "없는 월 또는 시간", "끝 시간은 시작 시간(2026111900)보다 빠를 수 없습니다",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("출력에 %q 없음:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "1. 시작 시간"); n != 4 {
		t.Errorf("시작 시간 질문 %d번, 4번이어야 함", n)
	}
	// 끝 시간이 시작과 같으면 통과(>=).
	withInput([]string{"2026111905", "2026111905"}, func() {
		captureOutput(func() { r = askLURange() })
	})
	if r.start != r.end {
		t.Errorf("같은 시각 = %+v", r)
	}
}

func TestLUScriptHeader(t *testing.T) {
	s := luScript(luRange{2026111900, 2026111923}, "")
	if !strings.HasPrefix(s, "LU_START=111900\nLU_END=111923\nLU_WRAP=0\n") || strings.Contains(s, "GOSSH_LU_LOGDIR=") {
		t.Errorf("머리 = %q", s[:60])
	}
	s = luScript(luRange{2025123100, 2026010100}, "/tmp/it's")
	if !strings.HasPrefix(s, "LU_START=123100\nLU_END=010100\nLU_WRAP=1\nGOSSH_LU_LOGDIR='/tmp/it'\\''s'\n") {
		t.Errorf("머리 = %q", s[:90])
	}
	// 기존 안전장치(위험 명령 거부, /user/ 병렬 제한)에 걸리지 않아야 한다.
	if isDangerousCommand(s) || isAutofsUserPath(s) {
		t.Errorf("조회 스크립트가 위험 명령 또는 /user/ 경로로 판정됨")
	}
}

// 가짜 로그 폴더를 만들어 실제 스크립트를 bash 로 돌린다(bash·awk 가 없으면 건너뜀).
func runLUScript(t *testing.T, dir string, r luRange) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash 없음")
	}
	if _, err := exec.LookPath("awk"); err != nil {
		t.Skip("awk 없음")
	}
	cmd := exec.Command(bash, "-s")
	cmd.Stdin = strings.NewReader(surveyScript(luScript(r, filepath.ToSlash(dir))))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Run()
	return out.String()
}

func writeLULog(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeLUGz(t *testing.T, dir, name, text string) {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	zw.Write([]byte(text))
	zw.Close()
	if err := os.WriteFile(filepath.Join(dir, name), b.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}

const luCurrentLog = `Nov 19 09:01:02 host sshd[123]: Accepted publickey for kim from 10.0.0.5 port 5522 ssh2
Nov 19 10:15:00 host sshd[124]: Accepted password for lee from 10.0.0.6 port 5523 ssh2
Nov 19 11:00:00 host sshd[125]: Accepted keyboard-interactive/pam for park from 10.0.0.7 port 5524 ssh2
Nov 19 12:00:00 host sshd[126]: Accepted publickey for root from 10.0.0.8 port 5525 ssh2
Nov 19 12:30:00 host sshd[127]: Failed password for kim from 10.0.0.9 port 5526 ssh2
Nov 19 12:31:00 host sshd[128]: Failed password for invalid user admin from 10.0.0.9 port 5527 ssh2
Nov 19 12:32:00 host sshd[129]: Invalid user admin from 10.0.0.9 port 5527
Nov 19 12:40:00 host sudo[130]: kim : TTY=pts/0 ; PWD=/root ; USER=root ; COMMAND=/bin/ls
Nov 19 12:41:00 host su[131]: (to root) kim on pts/0
Nov 19 12:42:00 host sshd[132]: pam_unix(sshd:session): session opened for user choi by (uid=0)
Nov 19 15:59:59 host sshd[133]: Accepted publickey for kim from 10.0.0.5 port 5530 ssh2
Nov 19 16:00:00 host sshd[134]: Accepted publickey for late from 10.0.0.5 port 5531 ssh2
Nov 18 23:59:59 host sshd[135]: Accepted publickey for early from 10.0.0.5 port 5532 ssh2
Nov  5 09:01:02 host sshd[136]: Accepted publickey for pad from 10.0.0.5 port 5533 ssh2
`

func TestLUScriptSameYear(t *testing.T) {
	dir := t.TempDir()
	writeLULog(t, dir, "secure", luCurrentLog)
	writeLUGz(t, dir, "secure-20261110.gz", "Nov 19 09:30:00 host sshd[1]: Accepted password for gz from 1.1.1.1 port 1 ssh2\nNov 19 08:00:00 host sshd[2]: Accepted password for kim from 1.1.1.1 port 1 ssh2\n")
	writeLULog(t, dir, "secure-20261101", "Nov 19 14:00:00 host sshd[1]: Accepted password for rot from 1.1.1.1 port 1 ssh2\n")
	writeLULog(t, dir, "messages", "Nov 19 10:00:00 host sshd[1]: Accepted password for other from 1.1.1.1 port 1 ssh2\n")

	got := runLUScript(t, dir, luRange{2026111909, 2026111915})
	want := "gz 1119_0930\nkim 1119_1559\nlee 1119_1015\npark 1119_1100\nrot 1119_1400\n"
	if got != want {
		t.Errorf("같은 해 범위 결과:\n%s\nwant:\n%s", got, want)
	}

	// 하루 전체: 패딩된 날짜(Nov  5)는 범위 밖, 18일 23시와 16시는 범위 안/밖 확인.
	got = runLUScript(t, dir, luRange{2026111800, 2026111916})
	for _, u := range []string{"early 1118_2359", "late 1119_1600", "kim 1119_1559"} {
		if !strings.Contains(got, u+"\n") {
			t.Errorf("%q 없음:\n%s", u, got)
		}
	}
	if strings.Contains(got, "pad") || strings.Contains(got, "root") || strings.Contains(got, "other") || strings.Contains(got, "choi") || strings.Contains(got, "admin") {
		t.Errorf("나오면 안 되는 user:\n%s", got)
	}

	// 두 자리 공백으로 채운 날짜(Nov  5)는 5일 범위에서 잡힌다.
	if got := runLUScript(t, dir, luRange{2026110509, 2026110509}); got != "pad 1105_0901\n" {
		t.Errorf("Nov  5 결과 = %q", got)
	}
	// 범위 안에 로그인이 없으면 빈 출력(#NOREAD 아님).
	if got := runLUScript(t, dir, luRange{2026120100, 2026120123}); got != "" {
		t.Errorf("빈 범위 결과 = %q", got)
	}
}

func TestLUScriptYearWrap(t *testing.T) {
	dir := t.TempDir()
	writeLULog(t, dir, "secure", `Jan  1 00:30:00 host sshd[1]: Accepted publickey for a from 1.1.1.1 port 1 ssh2
Jan  1 00:59:59 host sshd[2]: Accepted publickey for b from 1.1.1.1 port 1 ssh2
Jan  1 01:00:00 host sshd[3]: Accepted publickey for c from 1.1.1.1 port 1 ssh2
Jun 15 12:00:00 host sshd[4]: Accepted publickey for d from 1.1.1.1 port 1 ssh2
`)
	writeLULog(t, dir, "secure-20251231", `Dec 30 23:59:59 host sshd[1]: Accepted publickey for e from 1.1.1.1 port 1 ssh2
Dec 31 00:00:00 host sshd[2]: Accepted publickey for f from 1.1.1.1 port 1 ssh2
Dec 31 23:10:00 host sshd[3]: Accepted publickey for a from 1.1.1.1 port 1 ssh2
`)
	// a 는 12/31 23:10 과 1/1 00:30 둘 다 범위 안: 연말 뒤(1월)가 더 나중이다.
	got := runLUScript(t, dir, luRange{2025123100, 2026010100})
	want := "a 0101_0030\nb 0101_0059\nf 1231_0000\n"
	if got != want {
		t.Errorf("연말 범위 결과:\n%s\nwant:\n%s", got, want)
	}
}

func TestLUScriptNoRead(t *testing.T) {
	dir := t.TempDir()
	if got := runLUScript(t, dir, luRange{2026111900, 2026111923}); got != luNoReadMark+"\n" {
		t.Errorf("로그 없음 = %q", got)
	}
	writeLULog(t, dir, "messages", "x\n")
	if got := runLUScript(t, dir, luRange{2026111900, 2026111923}); got != luNoReadMark+"\n" {
		t.Errorf("secure 없음 = %q", got)
	}
	// 읽을 수 없는 파일만 있으면 못 읽음(root 로 돌리면 권한이 무시되므로 일반 사용자일 때만 확인).
	if os.Geteuid() != 0 && os.Geteuid() != -1 {
		writeLULog(t, dir, "secure", "Nov 19 09:00:00 host sshd[1]: Accepted password for a from 1.1.1.1 port 1 ssh2\n")
		os.Chmod(filepath.Join(dir, "secure"), 0)
		if got := runLUScript(t, dir, luRange{2026111900, 2026111923}); got != luNoReadMark+"\n" {
			t.Errorf("읽기 권한 없음 = %q", got)
		}
	}
}

func TestParseLUOutput(t *testing.T) {
	users, noRead := parseLUOutput("lee 1119_1432\r\nkim 1119_0901\n\nbad line here\nx 12\n")
	if noRead || len(users) != 2 || users[0] != (luUser{"kim", "1119_0901"}) || users[1] != (luUser{"lee", "1119_1432"}) {
		t.Errorf("users = %+v noRead = %v", users, noRead)
	}
	if _, noRead := parseLUOutput("#NOREAD\n"); !noRead {
		t.Errorf("#NOREAD 를 못 읽음으로 보지 않음")
	}
	if users, noRead := parseLUOutput(""); noRead || len(users) != 0 {
		t.Errorf("빈 출력 = %+v %v", users, noRead)
	}
}

func luTestData() ([]string, map[string]surveyHost) {
	ok := func(out string) surveyHost {
		return surveyHost{reachable: true, cells: []surveyCell{{out: out, ok: true}}}
	}
	hosts := []string{"svr1", "svr2", "svr3", "svr4", "svr5", "svr6"}
	return hosts, map[string]surveyHost{
		"svr1": ok("park 1119_2250\nkim 1119_0901\nlee 1119_1432\n"),
		"svr2": ok(""),
		"svr3": {err: "Timeout"},
		"svr4": ok(luNoReadMark + "\n"),
		"svr5": {reachable: true, cells: []surveyCell{{ok: false}}},
		// svr6 은 결과 없음(맵에 없음)
	}
}

func TestBuildLoginUserTable(t *testing.T) {
	hosts, res := luTestData()
	table, down, unread := buildLoginUserTable(hosts, res)
	want := "svr1|1119_0901|kim\nsvr1|1119_1432|lee\nsvr1|1119_2250|park\nsvr2||\nsvr3|-|-\nsvr4|!|!\nsvr5|-|-\nsvr6|-|-\n"
	if table != want {
		t.Errorf("표:\n%s\nwant:\n%s", table, want)
	}
	if strings.Join(down, ",") != "svr3,svr5,svr6" || strings.Join(unread, ",") != "svr4" {
		t.Errorf("down = %v unread = %v", down, unread)
	}
}

func TestPrintLoginUserDone(t *testing.T) {
	dir := t.TempDir()
	hostFile := filepath.Join(dir, "hosts")
	hosts, res := luTestData()
	table, _, _ := buildLoginUserTable(hosts, res)
	// captureOutput 은 표준출력·표준에러를 찍힌 순서대로 모은다: 표가 맨 앞에 그대로 나오고 안내는 그 뒤다.
	all := captureOutput(func() { printLoginUserDone(hosts, res, hostFile) })

	file, err := os.ReadFile(hostFile + sufLoginUser)
	if err != nil {
		t.Fatal(err)
	}
	if string(file) != table || table == "" {
		t.Errorf("파일과 표가 다름:\nfile=%q\ntable=%q", file, table)
	}
	if !strings.HasPrefix(all, table) || strings.Contains(table, "hostname") {
		t.Errorf("표준출력이 표로 시작하지 않음:\n%s", all)
	}
	for _, want := range []string{hostFile + sufLoginUser, "[경고]", "svr4", "접속불가·실행 실패 3대"} {
		if !strings.Contains(all, want) {
			t.Errorf("안내에 %q 없음:\n%s", want, all)
		}
	}
}

func TestLoginUserFlowUsesScript(t *testing.T) {
	hosts := []string{"svr1"}
	var gotCmds []string
	fake := func(hs, cmds []string, full bool) map[string]surveyHost {
		gotCmds = cmds
		if !full {
			t.Errorf("전체 실행(재검증 포함)이어야 함")
		}
		return map[string]surveyHost{}
	}
	captureOutput(func() {
		withInput([]string{"2025123100", "2026010100"}, func() { loginUserFlow(hosts, fake, "") })
	})
	if len(gotCmds) != 1 || !strings.HasPrefix(gotCmds[0], "LU_START=123100\nLU_END=010100\nLU_WRAP=1\n") {
		t.Errorf("원격 스크립트 = %q", gotCmds)
	}
}

// -lu 예시는 질문·표·경고·파일 경로를 보여주고 현재 폴더를 건드리지 않는다.
func TestHelpExampleLoginUser(t *testing.T) {
	d, ok := findOptionDoc("-lu")
	if !ok {
		t.Fatal("-lu 옵션 설명 없음")
	}
	before, _ := os.ReadDir(".")
	text := helpExampleText(d, false)
	for _, want := range []string{
		"1. 시작 시간 (YYYYMMDDHH, 예: 2026111900) : 2026111900", "2. 끝 시간 (YYYYMMDDHH, 예: 2026111923) : 2026111923",
		"svr1|1119_0901|kim\nsvr1|1119_1432|lee\nsvr1|1119_2250|park\n", "svr2|1119_1020|kim\n", "svr3||\n", "svr4|-|-", "svr5|!|!", "[경고]", exampleHostFile + sufLoginUser,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("-lu 예시에 %q 없음:\n%s", want, text)
		}
	}
	if after, _ := os.ReadDir("."); len(after) != len(before) {
		t.Errorf("예시 실행 후 현재 폴더 파일 수가 바뀜")
	}
}
