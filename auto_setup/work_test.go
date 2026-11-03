// work_test.go - v0.8.0 gossh 작업 기록(work.go): 붙여넣기·3자리 이름·같은 목록·경로 규칙(1000/1001·os6 미설정·원격 클라이언트)·
// os6 원격 명령 인용(특수문자, bash/tcsh)·script 래퍼·출력 판정·기록 저장(동시 추가·메모·파생·검색)·정리(7일·200MB·/tmp)·중계·CLI.
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// workTestEnv: 작업 기록 전역(디렉터리·/tmp·임의값·실행기·conf)을 테스트 값으로 바꾸고 끝나면 되돌린다
func workTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AUTO_SETUP_DIR", t.TempDir())
	oTmp, oRand, oScript, oRemote, oPause, oLess, oCap := workTmpDir, workRand, workScript, workRemoteRun, workPause, workLess, workUserCap
	o6m, o6g, o8, oOver := os6_mgmt, os6_gossh, os8_mgmt, work_os6_over
	t.Cleanup(func() {
		workTmpDir, workRand, workScript, workRemoteRun, workPause, workLess, workUserCap = oTmp, oRand, oScript, oRemote, oPause, oLess, oCap
		os6_mgmt, os6_gossh, os8_mgmt, work_os6_over = o6m, o6g, o8, oOver
	})
	workTmpDir = t.TempDir()
	workRand = rand.New(rand.NewSource(1))
	workScript = func([]string, string) (int, error) { t.Fatal("workScript 가 불리면 안 됨"); return 0, nil }
	workRemoteRun = func([]string) (remoteResult, error) {
		t.Fatal("workRemoteRun 이 불리면 안 됨")
		return remoteResult{}, nil
	}
	workPause = func(io.Reader, io.Writer) {}
	os6_mgmt, os6_gossh, os8_mgmt, work_os6_over = "", "", "", ""
}

func userStore(t *testing.T, user string) localWorkStore {
	t.Helper()
	dir, err := workUserDir(user)
	if err != nil {
		t.Fatal(err)
	}
	return localWorkStore{dir}
}

// ---- 붙여넣기·이름 ----

func TestWorkParseHosts(t *testing.T) {
	got := workParseHosts("svr001 svr002,svr003\n\n# 주석 svr999\n svr002\tsvr004 , svr[008-010]\r\nweb[1-2]x\n")
	want := []string{"svr001", "svr002", "svr003", "svr004", "svr008", "svr009", "svr010", "web1x", "web2x"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := workParseHosts("a[x-y] b[3-1]"); strings.Join(got, " ") != "ax-y b1 b2 b3" { // gossh expandHostLine 과 같은 결과
		t.Errorf("숫자가 아닌 범위·역순: %v", got)
	}
}

func TestWorkNewNameAvoidsCollisions(t *testing.T) {
	workTestEnv(t)
	db := &workDB{Lists: map[string][]string{}}
	re := regexp.MustCompile(`^[a-z]{3}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		n, err := workNewName(db)
		if err != nil || !re.MatchString(n) {
			t.Fatalf("이름 %q err=%v", n, err)
		}
		if seen[n] {
			t.Fatalf("이미 쓴 이름 %q 를 다시 줌", n)
		}
		seen[n] = true
		if i%2 == 0 {
			os.WriteFile(filepath.Join(workTmpDir, n), nil, 0644) // /tmp 에 이미 있는 이름
		} else {
			db.Lists[n] = []string{"h"} // 같은 user 기록에 있는 이름
		}
	}
	// 하나만 남기고 모두 사용 중 → 그 하나 (무작위 200회가 실패해도 전수 탐색으로 찾음)
	db = &workDB{Lists: map[string][]string{}}
	for a := 'a'; a <= 'z'; a++ {
		for b := 'a'; b <= 'z'; b++ {
			for c := 'a'; c <= 'z'; c++ {
				if n := string([]rune{a, b, c}); n != "qkz" {
					db.Lists[n] = nil
				}
			}
		}
	}
	if n, err := workNewName(db); err != nil || n != "qkz" {
		t.Fatalf("남은 이름 qkz 기대: %q %v", n, err)
	}
	os.WriteFile(filepath.Join(workTmpDir, "qkz"), nil, 0644)
	if _, err := workNewName(db); err == nil {
		t.Fatal("남은 이름이 없으면 오류")
	}
}

func TestWorkSameListReuse(t *testing.T) {
	db := &workDB{Lists: map[string][]string{"abc": {"a", "b", "c"}, "old": {"c", "b", "a"}, "xyz": {"a", "b"}},
		Runs: []*workRec{{File: "old", At: 1}, {File: "abc", At: 5}, {File: "xyz", At: 9}}}
	if f := db.sameList([]string{"c", "a", "b"}); f != "abc" {
		t.Errorf("같은 집합 중 최근 것: %q", f)
	}
	if f := db.sameList([]string{"a", "b", "d"}); f != "" {
		t.Errorf("다른 집합: %q", f)
	}
	if f := db.sameList([]string{"a"}); f != "" {
		t.Errorf("부분 집합은 다름: %q", f)
	}
}

// ---- 경로 규칙 ----

func TestWorkPlan(t *testing.T) {
	workTestEnv(t)
	os6_mgmt, os6_gossh = "os6mgmt", "/opt/gossh/gossh"
	cases := []struct {
		n      int
		client bool
		over   string
		want   workPlanT
	}{
		{1000, false, "", workPlanT{Side: "os8", Retry: true}},
		{1001, false, "", workPlanT{Side: "os6 전량", Remote: true}},
		{5000, true, "", workPlanT{Side: "os6"}}, // 원격 클라이언트: 대수 무관 os6 gossh 로 바로
		{1, true, "", workPlanT{Side: "os6"}},
		{5, false, "5", workPlanT{Side: "os8", Retry: true}},
		{6, false, "5", workPlanT{Side: "os6 전량", Remote: true}},
		{1001, false, "abc", workPlanT{Side: "os6 전량", Remote: true}}, // 잘못된 값 → 1000
	}
	for _, c := range cases {
		work_os6_over = c.over
		if got := workPlan(c.n, c.client); got != c.want {
			t.Errorf("n=%d client=%v over=%q: %+v want %+v", c.n, c.client, c.over, got, c.want)
		}
	}
	// os6 미설정: 재실행·전량 os6 불가 → os8 로만, 전량 대상이면 실행 전 확인
	work_os6_over = ""
	for _, unset := range []func(){func() { os6_mgmt = "" }, func() { os6_mgmt, os6_gossh = "os6mgmt", "" }} {
		unset()
		if p := workPlan(1000, false); p.Side != "os8" || p.Retry || p.Remote || p.Note != workNoOS6 || p.Ask != "" {
			t.Errorf("미설정 1000대: %+v", p)
		}
		if p := workPlan(1001, false); p.Side != "os8" || p.Remote || !strings.Contains(p.Ask, "1,001대") || !strings.Contains(p.Ask, "[y/n]") {
			t.Errorf("미설정 1001대: %+v", p)
		}
	}
	if b := workGosshBin(true); b != "gossh" {
		t.Errorf("os6_gossh 없으면 PATH 의 gossh: %q", b)
	}
	os6_gossh = "/opt/g/gossh"
	if workGosshBin(true) != "/opt/g/gossh" || workGosshBin(false) != "gossh" {
		t.Error("원격 클라이언트는 os6_gossh, os8 은 gossh")
	}
}

// ---- os6 원격 명령: 특수문자가 원격 gossh 인자 하나로 그대로, 목록은 원격 파일 경로만 ----

const fakeArgsGossh = `#!/bin/bash
out="$WORK_FAKE_OUT"
: > "$out/args"
for a in "$@"; do printf '%s\n--ARG--\n' "$a" >> "$out/args"; done
cp "$2" "$out/hosts"
echo "$2" > "$out/path"
exit 7
`

func TestWorkOS6CommandQuoting(t *testing.T) {
	workTestEnv(t)
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 없음")
	}
	dir := filepath.Join(t.TempDir(), "a b'c") // os6_gossh 경로에 공백·' 가 있어도
	os.MkdirAll(dir, 0755)
	os6_gossh = filepath.Join(dir, "gossh")
	os.WriteFile(os6_gossh, []byte(fakeArgsGossh), 0755)
	os6_mgmt = "os6mgmt"
	outDir := t.TempDir()
	t.Setenv("WORK_FAKE_OUT", outDir)
	hosts := []string{"svr001", "svr002.example.com", "10.0.0.3"}
	cmds := []string{
		`echo "a b" 'c' $HOME | grep -v x; echo ${PATH} ` + "`id`" + ` !x && ls * > /dev/null`,
		`awk '{print $1}' /etc/hosts | sort -u`,
		`printf '%s\n' "it's" \"q\" \\ ;`,
		`reboot`, // 위험어: 확인 옵션이 붙는다
	}
	shells := [][]string{{"sh", "-c"}}
	if _, err := exec.LookPath("tcsh"); err == nil { // os6_mgmt 로그인 셸이 tcsh 인 경우 (ssh 는 명령 문자열을 로그인 셸 -c 로 넘김)
		shells = append(shells, []string{"tcsh", "-c"})
	}
	for _, sh := range shells {
		for _, c := range cmds {
			danger := workDanger(c)
			argv := workOS6Argv(hosts, c, danger)
			if argv[0] != "ssh" || argv[1] != "-t" || strings.Join(argv[2:6], " ") != "-o BatchMode=yes -o ConnectTimeout=10" || argv[6] != "os6mgmt" {
				t.Fatalf("ssh 인자: %q", argv[:7])
			}
			remote := argv[7]
			if strings.Contains(remote, "\n") {
				t.Fatal("원격 명령에 줄바꿈 금지 (csh)")
			}
			cmd := exec.Command(sh[0], sh[1], remote)
			cmd.Env = append(os.Environ(), "WORK_FAKE_OUT="+outDir)
			o, err := cmd.CombinedOutput()
			if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 7 {
				t.Fatalf("%s: 종료코드 7(가짜 gossh) 이 그대로 와야 함: %v\n%s", sh[0], err, o)
			}
			b, _ := os.ReadFile(filepath.Join(outDir, "args"))
			args := strings.Split(strings.TrimSuffix(string(b), "\n--ARG--\n"), "\n--ARG--\n")
			want := []string{"-w", "<path>"}
			if danger {
				want = append(want, workDangerFlag)
			}
			want = append(want, c)
			p, _ := os.ReadFile(filepath.Join(outDir, "path"))
			want[1] = strings.TrimSpace(string(p))
			if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
				t.Errorf("%s: 원격 gossh 인자\n got %q\nwant %q", sh[0], args, want)
			}
			for _, a := range args { // gossh 인자에 base64 목록이 들어가지 않음 (위험어 오탐 방지)
				if strings.Contains(a, "base64") || (a != want[1] && len(a) > len(c)+40) {
					t.Errorf("gossh 인자에 목록이 섞임: %q", a)
				}
			}
			hb, _ := os.ReadFile(filepath.Join(outDir, "hosts"))
			if strings.Fields(string(hb))[0] != "svr001" || strings.Join(strings.Fields(string(hb)), " ") != strings.Join(hosts, " ") {
				t.Errorf("원격 목록 파일: %q", hb)
			}
			if _, err := os.Stat(want[1]); !os.IsNotExist(err) {
				t.Errorf("원격 임시 파일이 지워져야 함: %s", want[1])
			}
		}
	}
	// 로컬 gossh: 명령은 인자 하나 그대로, 위험어면 확인 옵션
	if a := workLocalArgv("gossh", "/tmp/qkz", cmds[0], false); strings.Join(a, "\x00") != strings.Join([]string{"gossh", "-w", "/tmp/qkz", cmds[0]}, "\x00") {
		t.Errorf("로컬 인자: %q", a)
	}
	if a := workLocalArgv("gossh", "/tmp/qkz", "init 6", true); a[3] != workDangerFlag || a[4] != "init 6" {
		t.Errorf("위험 옵션: %q", a)
	}
}

func TestWorkDanger(t *testing.T) {
	for _, c := range []string{"reboot", "sudo REBOOT", "poweroff", "shutdown -h now", "halt", "init 0", "init 6", "ddcutil"} {
		if !workDanger(c) {
			t.Errorf("%q 는 위험 명령", c)
		}
	}
	for _, c := range []string{"uptime", "cat /proc/uptime", "init 3", "systemctl is-active sssd"} {
		if workDanger(c) {
			t.Errorf("%q 는 위험 명령 아님", c)
		}
	}
}

// ---- script 래퍼 (RHEL6 util-linux-ng 2.17: -q -f -c 만, -e 없음 → 종료코드는 래퍼 셸이 파일로) ----

func TestWorkShellLineAndEnv(t *testing.T) {
	l := workShellLine([]string{"gossh", "-w", "/tmp/qkz", "echo 'x' $y"}, "/tmp/r c")
	if l != `'gossh' '-w' '/tmp/qkz' 'echo '\''x'\'' $y'; echo $? > '/tmp/r c'` {
		t.Errorf("한 줄: %s", l)
	}
	env := workScriptEnv([]string{"A=1", "SHELL=/bin/tcsh", "B=2"})
	if strings.Join(env, " ") != "A=1 B=2 SHELL=/bin/sh" {
		t.Errorf("SHELL 고정: %v", env)
	}
}

func TestWorkScriptReal(t *testing.T) {
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script 없음")
	}
	if _, err := exec.LookPath("tcsh"); err == nil {
		t.Setenv("SHELL", "/bin/tcsh") // 로그인 셸이 csh 여도 래퍼는 /bin/sh 로
	}
	log := filepath.Join(t.TempDir(), "x.log")
	rc, err := realWorkScript([]string{"sh", "-c", `printf '%s\n' "it's ok"; echo $((1+2)); exit 3`}, log)
	if err != nil || rc != 3 {
		t.Fatalf("rc=%d err=%v", rc, err)
	}
	raw, _ := os.ReadFile(log)
	b := string(stripScriptLog(raw))
	if !strings.Contains(b, "it's ok") || !strings.Contains(b, "3") || strings.Contains(b, "Script started") || strings.Contains(b, "Script done") {
		t.Errorf("로그(머리·꼬리 제거):\n%q\n원본:\n%q", b, raw)
	}
	if _, err := os.Stat(log + ".rc"); !os.IsNotExist(err) {
		t.Error("종료코드 파일은 지워야 함")
	}
	// 종료코드 파일 자리에 남이 먼저 둔 링크가 있으면 따라가 쓰지 않고 실행하지 않는다
	victim := filepath.Join(t.TempDir(), "victim")
	os.WriteFile(victim, []byte("keep\n"), 0644)
	log2 := filepath.Join(t.TempDir(), "y.log")
	if err := os.Symlink(victim, log2+".rc"); err != nil {
		t.Fatal(err)
	}
	if rc, err := realWorkScript([]string{"true"}, log2); err == nil || rc != -1 {
		t.Errorf("링크가 있는데 실행함: rc=%d err=%v", rc, err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep\n" {
		t.Errorf("링크 대상이 바뀜: %q", b)
	}
}

func TestStripScriptLog(t *testing.T) {
	cases := map[string]string{
		// util-linux 2.32 (-q 여도 머리·꼬리를 파일에 씀)
		"Script started on 2026-10-12 12:29:50-04:00\nhi\r\nthere\r\n\nScript done on 2026-10-12 12:29:50-04:00\n": "hi\r\nthere\r\n\n",
		// util-linux-ng 2.17 (-q 면 꼬리 없음)
		"Script started on Mon Oct 12 12:29:50 2026\nhi\r\n": "hi\r\n",
		"no header\r\n":                           "no header\r\n",
		"Script started on x\n":                   "",
		"Script started on x\nScript done on y\n": "",
		"a\r\nScript done on y but more\nb\n":     "a\r\nScript done on y but more\nb\n", // 마지막 줄이 아니면 그대로
	}
	for in, want := range cases {
		if got := string(stripScriptLog([]byte(in))); got != want {
			t.Errorf("%q → %q want %q", in, got, want)
		}
	}
}

// ---- 출력 판정 ----

// gosshOutput: 실제 gossh 형식의 가짜 출력 (색·진행률·요약 포함)
func gosshOutput(lines []string, retry bool, summary bool) string {
	var b strings.Builder
	b.WriteString("\x1b[?25l\r진행: 0/4 (0%)\x1b[K")
	for _, l := range lines {
		col := "32"
		if strings.Contains(l, "ERROR") || strings.Contains(l, "취소") {
			col = "31"
		}
		b.WriteString("\r\x1b[K\x1b[" + col + "m" + l + "\x1b[0m\r\n진행: 2/4 (50%)\x1b[K")
	}
	b.WriteString("\r\n")
	if retry {
		b.WriteString("재검증 중(2대, 낮은 동시성으로 재시도)...\r\n")
	}
	if summary {
		b.WriteString("\r\n\x1b[1;36m================= 작업 요약 =================\x1b[0m\r\n총 대상 서버 : 4 대 (소요시간: 1s)\r\n 동시 접속 수 : 1000\r\n\x1b[32m 정상 접속 가능 : 2 대\x1b[0m\r\n")
	}
	return b.String()
}

func TestJudgeWork(t *testing.T) {
	hosts := []string{"svr001", "svr002", "svr003", "svr004", "svr005", "svr006"}
	fail := "ERROR: SSH 접속 실패: dial tcp 10.0.0.3:22: i/o timeout"
	out := gosshOutput([]string{
		"svr001: 14:02 up 1 day", // 출력 있음 → 성공
		"svr002: ERROR: 종료코드 1",
		"svr003: " + fail,
		"svr005: ERROR: DNS 조회 실패: lookup svr005: no such host",
		"svr006: 사용자 취소",
		"svr003: " + fail, // 재검증에서도 실패 (2번)
		// svr004: 출력 없는 성공 (예 true) — 아무 줄도 없음
		// svr005: 재검증에서 접속됨 + 출력 없음 → 실패 줄 1번뿐
	}, true, true)
	r := judgeWork(out, hosts, nil)
	if r.Ok != 3 || strings.Join(r.Fail, ",") != "svr002,svr006" || strings.Join(r.Unr, ",") != "svr003" || r.NoRun {
		t.Fatalf("출력만으로 판정: %+v", r)
	}
	// 결과 파일이 있으면(로컬 gossh 정상 종료) 접속불가는 파일 기준
	r = judgeWork(out, hosts, map[string][]string{"off": {"svr005"}, "refused": {"svr003"}})
	if r.Ok != 2 || strings.Join(r.Unr, ",") != "svr003,svr005" || strings.Join(r.Fail, ",") != "svr002,svr006" {
		t.Fatalf("결과 파일 기준: %+v", r)
	}
	// 결과 파일이 비었으면(이번 실행에 접속불가 없음) 실패 줄이 있어도 재검증에서 접속된 것
	r = judgeWork(out, hosts, map[string][]string{})
	if r.Ok != 4 || len(r.Unr) != 0 {
		t.Fatalf("빈 결과 파일: %+v", r)
	}
	// 재검증이 없었으면(구버전 gossh) 실패 줄 1번이어도 접속불가
	r = judgeWork(gosshOutput([]string{"svr001: " + fail}, false, true), []string{"svr001", "svr002"}, nil)
	if r.Ok != 1 || strings.Join(r.Unr, ",") != "svr001" {
		t.Fatalf("재검증 없음: %+v", r)
	}
	// 요약이 없음(중단): 줄이 없는 호스트는 결과 없음 → 실패, 접속 실패 1번은 그대로 접속불가
	r = judgeWork(gosshOutput([]string{"svr001: ok", "svr002: " + fail}, true, false), []string{"svr001", "svr002", "svr003"}, nil)
	if r.Ok != 1 || strings.Join(r.Unr, ",") != "svr002" || strings.Join(r.Fail, ",") != "svr003" || r.NoRun {
		t.Fatalf("중단: %+v", r)
	}
	// Ctrl+C 세 번(전체 중단): 요약은 나오지만 시작 못 한 호스트(svr003)는 줄이 없음 → 성공이 아니라 실패
	ab := gosshOutput([]string{"svr001: ok", "svr002: 사용자 취소"}, false, true) +
		"\x1b[33m 사용자 취소(Ctrl+C) : 1 대\x1b[0m\r\n\x1b[33m 전체 중단됨 : 미실행 1 대\x1b[0m\r\n"
	r = judgeWork(ab, []string{"svr001", "svr002", "svr003"}, map[string][]string{"cancel": {"svr002"}})
	if r.Ok != 1 || strings.Join(r.Fail, ",") != "svr002,svr003" || len(r.Unr) != 0 || r.NoRun {
		t.Fatalf("전체 중단: %+v", r)
	}
	// 위험 명령 거부·y/N 취소: 호스트 줄이 하나도 없음 → NoRun
	refused := "\x1b[31m================================================================\x1b[0m\r\n\x1b[31m [경고] 위험 작업(시스템 종료/재부팅)이 감지되었습니다!\x1b[0m\r\n 감지된 명령어 : reboot\r\n"
	r = judgeWork(refused, []string{"a", "b"}, nil)
	if !r.NoRun || r.Ok != 0 || len(r.Fail) != 2 {
		t.Fatalf("NoRun: %+v", r)
	}
	// FQDN ↔ 짧은 이름, 출력이 ANSI 색이어도
	r = judgeWork(gosshOutput([]string{"svr001: x", "svr002.example.com: ERROR: 종료코드 2"}, false, true),
		[]string{"svr001.example.com", "svr002"}, nil)
	if r.Ok != 1 || strings.Join(r.Fail, ",") != "svr002" {
		t.Fatalf("FQDN: %+v", r)
	}
	rec := &workRec{Res: &workRes{Ok: 18, Unr: []string{"a", "b"}}, O6: &workRes{Ok: 2}}
	if s := workResText(rec); s != "성공 18 접속불가 2(os6 재실행 → 성공 2)" {
		t.Errorf("요약: %q", s)
	}
	rec = &workRec{Res: &workRes{Ok: 1198, Unr: []string{"a", "b"}}, O6Note: workNoOS6}
	if s := workResText(rec); s != "성공 1,198 접속불가 2(os6_mgmt 미설정)" {
		t.Errorf("요약: %q", s)
	}
	if s := workResText(&workRec{RC: 1, Res: &workRes{NoRun: true, Fail: []string{"a"}}}); s != "실행 안 됨 (rc=1)" {
		t.Errorf("요약: %q", s)
	}
}

// ---- 기록 저장 ----

func TestWorkStoreSaveLoadMemoDerived(t *testing.T) {
	workTestEnv(t)
	s := userStore(t, "alice")
	gz := gzipBytes([]byte("svr001: up\n"))
	if err := s.AddRun(workRec{ID: "20261012-140200-0001", File: "qkz", At: 100, Cmd: "uptime", N: 2, Res: &workRes{Ok: 1, Unr: []string{"b"}}}, []string{"a", "b"}, gz); err != nil {
		t.Fatal(err)
	}
	s.AddRun(workRec{ID: "20261012-141000-0002", File: "qkz", At: 200, Cmd: "true", N: 2, Res: &workRes{Ok: 2}}, []string{"x", "y"}, nil) // 사본은 처음 것 유지
	s.AddRun(workRec{ID: "20261012-142000-0003", File: "fbw", Parent: "qkz", At: 300, Cmd: "uptime", N: 1, Res: &workRes{Ok: 1}}, []string{"b"}, nil)
	s.SetMemo("qkz", "첫 메모")
	s.SetMemo("qkz", "LDAP 재적용 대상")
	s.SetMemo("fbw", "x")
	s.SetMemo("fbw", "") // 빈 값 = 삭제
	// 디렉터리 0700, 파일 0600
	for _, p := range []string{s.dir, filepath.Join(s.dir, "lists"), filepath.Join(s.dir, "out")} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0700 {
			t.Errorf("%s: %v %v", p, fi.Mode(), err)
		}
	}
	for _, p := range []string{s.index(), s.list("qkz"), s.outPath("20261012-140200-0001")} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0600 {
			t.Errorf("%s: %v %v", p, fi.Mode(), err)
		}
	}
	db, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(db.Runs) != 3 || db.Memo["qkz"] != "LDAP 재적용 대상" || db.Memo["fbw"] != "" || strings.Join(db.Lists["qkz"], ",") != "a,b" {
		t.Fatalf("db: runs=%d memo=%v lists=%v", len(db.Runs), db.Memo, db.Lists)
	}
	gs := db.groups()
	if len(gs) != 2 || gs[0].File != "fbw" || gs[0].Parent != "qkz" || gs[1].File != "qkz" || gs[1].Runs[0].ID != "20261012-141000-0002" {
		t.Fatalf("묶음(최근 파일이 위, 묶음 안도 최신이 위·파생): %+v", gs)
	}
	if b, err := s.Output("20261012-140200-0001"); err != nil || !bytes.Equal(b, gz) {
		t.Error("출력 읽기")
	}
	if _, err := s.Output("20261012-141000-0002"); !os.IsNotExist(err) {
		t.Error("출력 없는 실행은 NotExist")
	}
	if _, err := s.Output("../../etc/passwd"); err == nil {
		t.Error("id 검증")
	}
	// 검색: 시간순, 짧은 이름으로도
	if f := db.search("b"); len(f) != 3 || f[0].At != 100 || f[2].At != 300 {
		t.Errorf("검색: %d", len(f))
	}
	if f := db.search("a.example.com"); len(f) != 2 {
		t.Errorf("짧은 이름 검색: %d", len(f))
	}
	// 깨진 줄은 건너뜀
	f, _ := os.OpenFile(s.index(), os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("{broken\n{\"t\":\"run\",\"id\":\"bad\",\"file\":\"qkz\"}\n")
	f.Close()
	if db, _ := s.Load(); len(db.Runs) != 3 {
		t.Errorf("깨진 줄 무시: %d", len(db.Runs))
	}
	if _, err := workUserDir("../x"); err == nil {
		t.Error("user 검증")
	}
}

func TestWorkStoreConcurrentAppend(t *testing.T) {
	workTestEnv(t)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			s := userStore(t, "bob") // 각자 따로 연 저장소 (터미널 여러 개)
			fail := make([]string, 300)
			for i := range fail {
				fail[i] = fmt.Sprintf("host%04d", i)
			}
			for i := 0; i < 25; i++ {
				s.AddRun(workRec{ID: fmt.Sprintf("20261012-1400%02d-%04x", i, g), File: "abc", At: int64(i), Res: &workRes{Fail: fail}}, []string{"h"}, nil)
			}
		}(g)
	}
	wg.Wait()
	db, _ := userStore(t, "bob").Load()
	if len(db.Runs) != 200 {
		t.Fatalf("동시 추가 200줄 기대: %d", len(db.Runs))
	}
}

func TestWorkRetryHostsAndStatus(t *testing.T) {
	list := []string{"a", "b", "c", "d", "e"}
	r := &workRec{Res: &workRes{Ok: 2, Fail: []string{"d"}, Unr: []string{"b", "e"}}, O6: &workRes{Ok: 1, Unr: []string{"e"}}}
	if got := workRetryHosts(r, list); strings.Join(got, ",") != "d,e" {
		t.Errorf("os6 재실행 반영: %v", got)
	}
	r.O6 = nil
	if got := workRetryHosts(r, list); strings.Join(got, ",") != "b,d,e" {
		t.Errorf("os6 재실행 없음: %v", got)
	}
	r.O6 = &workRes{Ok: 1, Unr: []string{"e"}}
	want := map[string]string{"a": "성공", "b": "접속불가 → os6 성공", "d": "실패", "e": "접속불가 → os6 접속불가"}
	for h, w := range want {
		if s := workHostStatus(r, h); s != w {
			t.Errorf("%s: %q want %q", h, s, w)
		}
	}
}

// ---- 정리 (가짜 시계) ----

func TestWorkCleanOut(t *testing.T) {
	workTestEnv(t)
	s := userStore(t, "alice")
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	mk := func(id string, age time.Duration, size int) string {
		p := s.outPath(id)
		os.MkdirAll(filepath.Dir(p), 0700)
		os.WriteFile(p, make([]byte, size), 0600)
		os.Chtimes(p, now.Add(-age), now.Add(-age))
		return p
	}
	old := mk("20261012-000000-0001", workKeep+time.Hour, 10)
	keep := mk("20261019-000000-0002", 24*time.Hour, 10)
	part := filepath.Join(s.dir, "out", "x.log.gz.tmp1")
	os.WriteFile(part, nil, 0600)
	os.Chtimes(part, now.Add(-48*time.Hour), now.Add(-48*time.Hour))
	os.MkdirAll(filepath.Join(s.dir, "relay"), 0700)
	relay := filepath.Join(s.dir, "relay", "x.out")
	os.WriteFile(relay, nil, 0600)
	os.Chtimes(relay, now.Add(-48*time.Hour), now.Add(-48*time.Hour))
	if n := workCleanOut(s.dir, now); n != 1 {
		t.Errorf("7일 지난 출력 1개: %d", n)
	}
	for p, want := range map[string]bool{old: false, keep: true, part: false, relay: false} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s 남음=%v 기대 %v", p, err == nil, want)
		}
	}
	// 200MB(테스트는 100바이트) 넘으면 오래된 것부터
	workUserCap = 100
	a := mk("20261015-000000-0003", 5*24*time.Hour, 60)
	b := mk("20261016-000000-0004", 4*24*time.Hour, 40)
	c := mk("20261017-000000-0005", 3*24*time.Hour, 40)
	workCleanOut(s.dir, now) // 합 150(keep 10 포함) → 가장 오래된 a 삭제 → 90
	for p, want := range map[string]bool{a: false, b: true, c: true, keep: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("상한: %s 남음=%v 기대 %v", filepath.Base(p), err == nil, want)
		}
	}
	// 데몬 정리(모든 user)
	workUserCap = 200 << 20
	old2 := mk("20261001-000000-0006", 20*24*time.Hour, 1)
	workCleanAll(now)
	if _, err := os.Stat(old2); !os.IsNotExist(err) {
		t.Error("workCleanAll 이 오래된 출력을 지워야 함")
	}
}

func TestWorkCleanTmp(t *testing.T) {
	workTestEnv(t)
	host, _ := os.Hostname()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	oldT := now.Add(-workKeep - time.Hour)
	db := &workDB{Lists: map[string][]string{"aaa": {"h1", "h2"}, "bbb": {"h1"}, "ccc": {"h1"}, "ddd": {"h1"}, "eee": {"h1"}}}
	db.Runs = []*workRec{
		{File: "aaa", At: oldT.Unix(), Server: host},  // 삭제 대상
		{File: "bbb", At: oldT.Unix(), Server: host},  // 내용이 다름 → 유지
		{File: "ccc", At: now.Unix(), Server: host},   // 최근 실행 → 유지
		{File: "ddd", At: oldT.Unix(), Server: "os6"}, // 다른 서버에서 실행 → 유지
		{File: "eee", At: oldT.Unix(), Server: host},  // 파일이 최근에 수정됨 → 유지
	}
	mk := func(n, body string, mt time.Time) string {
		p := filepath.Join(workTmpDir, n)
		os.WriteFile(p, []byte(body), 0644)
		os.Chtimes(p, mt, mt)
		return p
	}
	a := mk("aaa", "h1\nh2\n", oldT)
	aRes := mk("aaa_res_off", "h2\n", oldT)
	b := mk("bbb", "h1\nhX\n", oldT)
	c := mk("ccc", "h1\n", oldT)
	d := mk("ddd", "h1\n", oldT)
	e := mk("eee", "h1\n", now)
	z := mk("zzz", "h1\n", oldT) // 기록에 없는 파일 → 유지
	if n := workCleanTmp(db, now); n != 1 {
		t.Errorf("1개 삭제 기대: %d", n)
	}
	for p, want := range map[string]bool{a: false, aRes: false, b: true, c: true, d: true, e: true, z: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s 남음=%v 기대 %v", filepath.Base(p), err == nil, want)
		}
	}
}

// v0.8.3 전 버전이 /tmp/<3자리> 에 남긴 목록도 같은 조건(기록에 있음·7일 지남·내용 같음)이면 지운다
func TestWorkCleanTmpLegacy(t *testing.T) {
	workTestEnv(t)
	oldL := workLegacyDir
	workLegacyDir = t.TempDir()
	t.Cleanup(func() { workLegacyDir = oldL })
	host, _ := os.Hostname()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	oldT := now.Add(-workKeep - time.Hour)
	db := &workDB{Lists: map[string][]string{"aaa": {"h1", "h2"}, "bbb": {"h1"}}}
	db.Runs = []*workRec{{File: "aaa", At: oldT.Unix(), Server: host}, {File: "bbb", At: oldT.Unix(), Server: host}}
	mk := func(n, body string) string {
		p := filepath.Join(workLegacyDir, n)
		os.WriteFile(p, []byte(body), 0644)
		os.Chtimes(p, oldT, oldT)
		return p
	}
	a, aRes, b := mk("aaa", "h1\nh2\n"), mk("aaa_res_off", "h2\n"), mk("bbb", "사용자 파일\n")
	if n := workCleanTmp(db, now); n != 1 {
		t.Errorf("1개 삭제 기대: %d", n)
	}
	for p, want := range map[string]bool{a: false, aRes: false, b: true} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s 남음=%v 기대 %v", filepath.Base(p), err == nil, want)
		}
	}
}

// ---- 압축 상한 ----

func TestWorkCapGz(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	raw := make([]byte, 400<<10)
	for i := range raw {
		raw[i] = byte('a' + r.Intn(26)) // 잘 압축되지 않는 글
	}
	copy(raw, "HEAD-MARK")
	copy(raw[len(raw)-9:], "TAIL-MARK")
	gz := workCapGz(raw, 100<<10)
	if len(gz) > 100<<10 {
		t.Fatalf("상한 초과: %d", len(gz))
	}
	b, _ := gunzipBytes(gz)
	if !bytes.HasPrefix(b, []byte("HEAD-MARK")) || !bytes.HasSuffix(b, []byte("TAIL-MARK")) || !bytes.Contains(b, []byte("생략")) {
		t.Errorf("앞·뒤·생략 표시: %d바이트", len(b))
	}
	if small := workCapGz([]byte("abc"), 100); !bytes.Equal(small, gzipBytes([]byte("abc"))) {
		t.Error("상한 안이면 그대로")
	}
}

// ---- os6_mgmt → os8_mgmt 중계 ----

// relayInProcess: remoteRun 대신 같은 프로세스의 cmdWorkRelay 를 부른다 (stdout 은 임시 파일로)
func relayInProcess(t *testing.T, calls *[][]string) func([]string) (remoteResult, error) {
	return func(args []string) (remoteResult, error) {
		*calls = append(*calls, args)
		full := remoteCommand(args)
		if workDanger(full) || strings.Contains(full, "/user/") {
			t.Errorf("gossh 원샷 명령에 위험어·/user/ 가 생김: %.200s", full)
		}
		if args[0] != "work-relay" {
			t.Fatalf("args: %q", args)
		}
		f, _ := os.CreateTemp(t.TempDir(), "o")
		e, _ := os.CreateTemp(t.TempDir(), "e")
		oldO, oldE := os.Stdout, os.Stderr
		os.Stdout, os.Stderr = f, e
		rc := cmdWorkRelay(args[1:])
		os.Stdout, os.Stderr = oldO, oldE
		f.Close()
		e.Close()
		ob, _ := os.ReadFile(f.Name())
		eb, _ := os.ReadFile(e.Name())
		return remoteResult{Stdout: string(ob), Stderr: string(eb), RC: rc}, nil
	}
}

func TestWorkRelayRoundTrip(t *testing.T) {
	workTestEnv(t)
	var calls [][]string
	workRemoteRun = relayInProcess(t, &calls)
	rs := remoteWorkStore{user: "alice"}
	r := rand.New(rand.NewSource(5))
	out := make([]byte, workRelayChunk*2+123) // 조각 3개
	r.Read(out)
	hosts := []string{"svr001", "svr002"}
	rec := workRec{ID: "20261012-140200-00ab", File: "ddc", Parent: "qkz", At: 1, Cmd: "reboot; halt; cat /user/x", N: 2, Server: "os6", Side: "os6",
		Res: &workRes{Ok: 2}}
	if err := rs.AddRun(rec, hosts, out); err != nil {
		t.Fatal(err)
	}
	puts := 0
	for _, c := range calls {
		if c[1] == "put" {
			puts++
		}
	}
	if puts != 4 { // rec 1 + out 3
		t.Errorf("조각 수: %d", puts)
	}
	if err := rs.SetMemo("ddc", "reboot 대상 /user/x"); err != nil {
		t.Fatal(err)
	}
	db, err := rs.Load()
	if err != nil || len(db.Runs) != 1 || db.Runs[0].Cmd != rec.Cmd || db.Memo["ddc"] != "reboot 대상 /user/x" || strings.Join(db.Lists["ddc"], ",") != "svr001,svr002" {
		t.Fatalf("os8 기록: %+v %v", db, err)
	}
	if b, err := rs.Output(rec.ID); err != nil || !bytes.Equal(b, out) {
		t.Fatalf("출력 왕복: %v len=%d", err, len(b))
	}
	if _, err := rs.Output("20261012-140200-0000"); err == nil {
		t.Error("없는 출력")
	}
	// os8 쪽 파일 권한, 조각 정리
	dir, _ := workUserDir("alice")
	if ents, _ := os.ReadDir(filepath.Join(dir, "relay")); len(ents) != 0 {
		t.Errorf("relay 조각이 남음: %d", len(ents))
	}
	if fi, _ := os.Stat(filepath.Join(dir, "out", rec.ID+".log.gz")); fi == nil || fi.Mode().Perm() != 0600 {
		t.Error("출력 0600")
	}
	// 잘못된 인자 거부
	bad := [][]string{{"put", "alice", "../x", "rec", "0", "a"}, {"commit", "alice", "20261012-140200-0099"}, {"memo", "alice", "../../etc", "x"}, {"load", "../x"}, {"nope", "alice"}}
	for _, b := range bad {
		enc := []string{b[0]}
		for _, a := range b[1:] {
			enc = append(enc, wkEnc(a))
		}
		var cs [][]string
		if res, _ := relayInProcess(t, &cs)(append([]string{"work-relay"}, enc...)); res.RC == 0 {
			t.Errorf("%v 는 거부해야 함", b)
		}
	}
}

func TestWkEncNoDangerWords(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	b := make([]byte, 1<<20)
	r.Read(b)
	enc := wkEnc(string(b))
	if workDanger(enc) || strings.Contains(enc, "/user/") {
		t.Fatal("인코딩에 위험어")
	}

	if d, err := wkDec(enc); err != nil || d != string(b) {
		t.Fatal("왕복")
	}
}

// ---- 실행 한 건 (가짜 script) ----

var os6B64Re = regexp.MustCompile(`echo ([A-Za-z0-9+/=]+) \| base64 -d \| gzip -dc`)

// fakeGosshRun: argv 가 로컬 gossh 면 -w 파일에서, os6 ssh 면 원격 명령의 base64 에서 목록을 꺼내 unr 에 든 호스트는 접속불가로 만든 출력
func fakeGosshRun(t *testing.T, calls *[][]string, unr map[string]bool, summary bool) func([]string, string) (int, error) {
	return func(argv []string, logPath string) (int, error) {
		*calls = append(*calls, argv)
		var hosts []string
		if argv[0] == "ssh" {
			m := os6B64Re.FindStringSubmatch(argv[len(argv)-1])
			if m == nil {
				t.Fatalf("os6 명령에 목록이 없음: %q", argv)
			}
			zb, _ := base64.StdEncoding.DecodeString(m[1])
			b, err := gunzipBytes(zb)
			if err != nil {
				t.Fatal(err)
			}
			hosts = strings.Fields(string(b))
		} else {
			b, err := os.ReadFile(argv[2])
			if err != nil {
				t.Fatalf("목록 파일: %v", err)
			}
			hosts = strings.Fields(string(b))
		}
		var lines, off []string
		retry := false
		for _, h := range hosts {
			if unr[h] && argv[0] != "ssh" {
				lines = append(lines, h+": ERROR: SSH 접속 실패: timeout", h+": ERROR: SSH 접속 실패: timeout")
				off = append(off, h)
				retry = true
			} else {
				lines = append(lines, h+": up "+argv[0])
			}
		}
		if argv[0] != "ssh" { // gossh 처럼 결과 파일: 시작 때 지우고 접속불가가 있으면 씀
			os.Remove(argv[2] + "_res_off")
			if len(off) > 0 {
				os.WriteFile(argv[2]+"_res_off", []byte(strings.Join(off, "\n")+"\n"), 0644)
			}
		}
		os.WriteFile(logPath, []byte("Script started on X\n"+gosshOutput(lines, retry, summary)+"\nScript done on Y\n"), 0600)
		return 0, nil
	}
}

func TestWorkExecuteRoutes(t *testing.T) {
	workTestEnv(t)
	os6_mgmt, os6_gossh = "os6mgmt", "/opt/gossh/gossh"
	var calls [][]string
	workScript = fakeGosshRun(t, &calls, map[string]bool{"h3": true}, true)
	s := userStore(t, "alice")
	hosts := []string{"h1", "h2", "h3"}
	workWriteTmp("qkz", hosts, true)
	now := func() time.Time { return time.Date(2026, 10, 12, 14, 2, 0, 0, time.UTC) }
	var term bytes.Buffer
	req := workRunReq{User: "alice", File: "qkz", Hosts: hosts, Cmd: "cat /proc/uptime", Plan: workPlan(3, false)}
	rec, msg, ok := workExecute(req, s, nil, &term, now)
	if !ok || len(calls) != 2 || calls[0][0] != "gossh" || calls[1][0] != "ssh" {
		t.Fatalf("os8 → 접속불가만 os6: ok=%v calls=%v", ok, calls)
	}
	if !strings.Contains(term.String(), "접속불가 1대 → os6_mgmt 경유로 재실행") {
		t.Errorf("터미널 안내:\n%s", term.String())
	}
	if rec.Side != "os8" || rec.Res.Ok != 2 || strings.Join(rec.Res.Unr, ",") != "h3" || rec.O6 == nil || rec.O6.Ok != 1 || msg != "qkz: 성공 2 접속불가 1(os6 재실행 → 성공 1)" {
		t.Fatalf("기록: %+v o6=%+v msg=%q", rec.Res, rec.O6, msg)
	}
	db, _ := s.Load()
	gz, _ := s.Output(db.Runs[0].ID)
	b, _ := gunzipBytes(gz)
	if !strings.Contains(string(b), "h1: up gossh") || !strings.Contains(string(b), "h3: up ssh") || strings.Contains(string(b), "Script started") {
		t.Errorf("출력(두 실행 합침, script 머리 제거):\n%s", b)
	}
	lb, _ := os.ReadFile(logPath())
	if !strings.Contains(string(lb), "gossh 작업 "+rec.ID) || strings.Contains(string(lb), "cat /proc/uptime") {
		t.Errorf("auto_setup.log 에는 명령 원문 없이 id·대수·결과만:\n%s", lb)
	}
	// 1001대 초과 → 전량 os6 (ssh 한 번)
	calls = nil
	work_os6_over = "2"
	req.Plan = workPlan(3, false)
	rec, _, _ = workExecute(req, s, nil, &term, now)
	if len(calls) != 1 || calls[0][0] != "ssh" || rec.Side != "os6 전량" || rec.Res.Ok != 3 {
		t.Fatalf("전량 os6: %v %+v", calls, rec)
	}
	// os6 미설정 → os8 만, 재실행 안 함
	calls = nil
	work_os6_over, os6_mgmt = "", ""
	req.Plan = workPlan(3, false)
	rec, msg, _ = workExecute(req, s, nil, &term, now)
	if len(calls) != 1 || rec.O6 != nil || rec.O6Note != workNoOS6 || !strings.Contains(msg, "(os6_mgmt 미설정)") {
		t.Fatalf("os6 미설정: %v %+v %q", calls, rec, msg)
	}
	// 원격 클라이언트: 이 서버의 os6_gossh, 기록은 os8 로 중계 — 전송 실패면 이 서버에 보관
	calls = nil
	os6_gossh, os8_mgmt = "/opt/os6/gossh", "os8mgmt"
	workRemoteRun = func([]string) (remoteResult, error) { return remoteResult{}, fmt.Errorf("접속 불가") }
	req.Plan, req.Client = workPlan(3, true), true
	local := userStore(t, "alice")
	rec, msg, ok = workExecute(req, remoteWorkStore{user: "alice"}, local, &term, now)
	if ok || len(calls) != 1 || calls[0][0] != "/opt/os6/gossh" || rec.Side != "os6" || !strings.Contains(msg, "os8_mgmt 로 기록 전송 실패") {
		t.Fatalf("원격 클라이언트: ok=%v %v %q", ok, calls, msg)
	}
	if db, _ := local.Load(); db.Runs[len(db.Runs)-1].ID != rec.ID {
		t.Error("전송 실패 시 이 서버에 보관")
	}
}

func TestWorkPrepareTmp(t *testing.T) {
	workTestEnv(t)
	db := &workDB{Lists: map[string][]string{"abc": {"a", "b"}}}
	// 기존 목록: 없으면 사본으로 다시 만듦
	d := workDraft{File: "abc", Hosts: []string{"a", "b"}}
	if note, err := workPrepareTmp(db, &d); err != nil || note != "" || d.File != "abc" {
		t.Fatalf("재생성: %q %v %+v", note, err, d)
	}
	if b, _ := os.ReadFile(filepath.Join(workTmpDir, "abc")); string(b) != "a\nb\n" {
		t.Errorf("내용: %q", b)
	}
	if fi, _ := os.Stat(filepath.Join(workTmpDir, "abc")); fi.Mode().Perm() != 0644 {
		t.Errorf("/tmp 파일 0644: %v", fi.Mode())
	}
	// 같으면 그대로
	if note, err := workPrepareTmp(db, &d); err != nil || note != "" || d.File != "abc" || d.NewList {
		t.Fatalf("같음: %q %v", note, err)
	}
	// 누가 고쳤으면 건드리지 않고 새 이름(파생)
	os.WriteFile(filepath.Join(workTmpDir, "abc"), []byte("a\nzzz\n"), 0644)
	note, err := workPrepareTmp(db, &d)
	if err != nil || d.File == "abc" || d.Parent != "abc" || !strings.Contains(note, "내용이 기록과 달라") {
		t.Fatalf("파생: %q %v %+v", note, err, d)
	}
	if b, _ := os.ReadFile(filepath.Join(workTmpDir, "abc")); string(b) != "a\nzzz\n" {
		t.Error("고친 파일은 그대로")
	}
	// 새 목록인데 그새 같은 이름이 생기면 다른 이름
	os.WriteFile(filepath.Join(workTmpDir, "new"), nil, 0644)
	d = workDraft{File: "new", NewList: true, Hosts: []string{"x"}}
	if _, err := workPrepareTmp(db, &d); err != nil || d.File == "new" {
		t.Fatalf("이름 충돌: %v %+v", err, d)
	}
}

// ---- CLI ----

func TestWorkCLI(t *testing.T) {
	workTestEnv(t)
	saveSessionState(t)
	oldErr := os.Stderr
	f, _ := os.CreateTemp(t.TempDir(), "e")
	os.Stderr = f
	defer func() { os.Stderr = oldErr }()
	if rc := run([]string{"s", "extra"}); rc != 1 {
		t.Errorf("s extra → 사용법: %d", rc)
	}
	if rc := run([]string{"s", "-user", "alice"}); rc != 1 || forceUser != "alice" { // 테스트는 tty 가 아님
		t.Errorf("s -user: rc=%d forceUser=%q", rc, forceUser)
	}
	os8_mgmt = "os8mgmt"
	if rc := run([]string{"work-relay", "load", wkEnc("alice")}); rc != 1 {
		t.Errorf("원격 클라이언트에서 work-relay 거부: %d", rc)
	}
	os.Stderr = oldErr
	f.Close()
	b, _ := os.ReadFile(f.Name())
	if !strings.Contains(string(b), "auto_setup s 는 터미널(tty)에서 실행하세요") || !strings.Contains(string(b), "work-relay 는 os8_mgmt 에서만") {
		t.Errorf("오류 안내:\n%s", b)
	}
	if !strings.Contains(usageText, "s [-user <이름>]") {
		t.Error("usage 에 s")
	}
}
