// second_test.go - second.go 단위 테스트: 가짜 ssh 스텁(PATH 주입)으로 원격 실행·tar 회수·파싱, 비활성 no-op, 비정상 종료, code 병합
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// secondEnv: 가짜 ssh(옵션·호스트 건너뛰고 sh -c 로 로컬 실행) + 가짜 os_check(2차용) 준비
func secondEnv(t *testing.T, osCheckBody string) {
	t.Helper()
	needBash(t)
	setupDir(t)
	oldM, oldS := os6_mgmt, os6_os_check_sh
	t.Cleanup(func() { os6_mgmt, os6_os_check_sh = oldM, oldS })

	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bin")
	os.MkdirAll(bin, 0755)
	os.WriteFile(filepath.Join(bin, "ssh"), []byte(`#!/bin/sh
echo "$@" >> "$SSH_LOG"
while [ "$1" = "-o" ]; do shift 2; done
shift
exec sh -c "$1"
`), 0755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("SSH_LOG", filepath.Join(tmp, "ssh.log"))
	osCheck := filepath.Join(tmp, "os check 'x'.sh") // 공백·따옴표 quoting 확인
	os.WriteFile(osCheck, []byte("#!/bin/bash\n"+osCheckBody), 0755)
	os.WriteFile(filepath.Join(tmp, "dhcp.sh"), []byte("#dhcp\n"), 0644)
	os6_mgmt, os6_os_check_sh = "mgmtstub", osCheck
}

func firstCode(t *testing.T, body string) RunResult {
	t.Helper()
	code, err := newCode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codePath(code), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return RunResult{Code: code, Processed: []string{"host01", "host03"}, Failed: []string{"host03"}}
}

const firstBody = "1차 본문\n\n원본 : /x/os_check.log\n"

func TestSecondRunEndToEnd(t *testing.T) {
	td, _ := filepath.Abs("testdata")
	secondEnv(t, "echo \"$@\" > args.txt; cat '"+td+"/os_check.log'\n"+
		"printf 'host02: OK\\nhost03: FAIL x\\n' > check.res_user1_postapply\n"+
		"printf 'other\\n' > check.res_user2_postapply\nexit 5\n")
	first := firstCode(t, firstBody)
	j := &Job{ID: "j1", User: "user1"}
	res, err := NewRealSecond().Run(j, first, []string{"host02", "host03", "host04"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Processed, ",") != "host02,host03" || strings.Join(res.Fail, ",") != "host03" {
		t.Fatalf("결과: %+v", res)
	}
	dir := filepath.Join(runsDir(), first.Code, "second")
	for _, f := range []string{"os_check.log", "check.res_user1_postapply", "targets.txt"} {
		if !fileExists(filepath.Join(dir, f)) {
			t.Fatalf("%s 없음", f)
		}
	}
	for _, f := range []string{"check.res_user2_postapply", "args.txt"} {
		if fileExists(filepath.Join(dir, f)) {
			t.Fatalf("%s 는 회수되면 안 됨", f)
		}
	}
	// 원격 명령 형식
	lg, _ := os.ReadFile(os.Getenv("SSH_LOG"))
	if strings.Count(string(lg), "-o BatchMode=yes -o ConnectTimeout=10 mgmtstub") != 2 {
		t.Fatalf("ssh 호출:\n%s", lg)
	}
	run := ""
	for _, l := range strings.Split(string(lg), "\n") {
		if strings.Contains(l, "mktemp -d") {
			run = l
		}
	}
	for _, w := range []string{`cat > "$d/targets.txt"`, "-auto 'user1' targets.txt > os_check.log 2>&1; echo ==RC==$?", `'\''x'\''`} {
		if !strings.Contains(run, w) {
			t.Fatalf("원격 실행 명령에 %q 없음:\n%s", w, run)
		}
	}
	if !strings.Contains(string(lg), "tar cf - os_check.log $(ls check.res_'user1'*") || !strings.Contains(string(lg), "rm -rf ") {
		t.Fatalf("회수·삭제 명령:\n%s", lg)
	}
	// code 병합
	b, _ := readCode(first.Code)
	text := string(b)
	if !strings.HasPrefix(text, firstBody+"\n### 2차 체크 (os6_mgmt)\n대상 : host02 host03 host04 (3대)\n") {
		t.Fatalf("code:\n%s", text)
	}
	for _, w := range []string{reportStart, "host03: FAIL x", "NO FAIL : host02 (1대)",
		"최종 판정 (1차 OK 또는 2차 OK)\nFAIL : host03 host04 (2대)\nNO FAIL : host01 host02 (2대)",
		"원본 : " + filepath.Join(dir, "os_check.log") + "\n"} {
		if !strings.Contains(text, w) {
			t.Fatalf("%q 없음:\n%s", w, text)
		}
	}
}

func TestSecondRunAbnormal(t *testing.T) {
	secondEnv(t, "echo crashed\nexit 1\n")
	first := firstCode(t, firstBody)
	res, err := NewRealSecond().Run(&Job{User: "user1"}, first, []string{"host02"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Processed) != 0 || len(res.Fail) != 0 {
		t.Fatalf("비정상 종료는 호스트 미처리: %+v", res)
	}
	b, _ := readCode(first.Code)
	if !strings.Contains(string(b), "[!] 2차 os_check 비정상 종료") || !strings.Contains(string(b), "FAIL : host02 host03 (2대)") {
		t.Fatalf("code:\n%s", b)
	}
}

func TestSecondRunDisabledNoop(t *testing.T) {
	secondEnv(t, "exit 0\n")
	first := firstCode(t, firstBody)
	for _, c := range []struct{ m, s string }{{"", "x"}, {"m", ""}} {
		os6_mgmt, os6_os_check_sh = c.m, c.s
		res, err := NewRealSecond().Run(&Job{User: "user1"}, first, []string{"host02"})
		if err != nil || len(res.Processed) != 0 || len(res.Fail) != 0 {
			t.Fatalf("비활성: %+v %v", res, err)
		}
	}
	os6_mgmt, os6_os_check_sh = "m", "x"
	if res, err := NewRealSecond().Run(&Job{User: "user1"}, first, nil); err != nil || len(res.Processed) != 0 {
		t.Fatalf("대상 없음: %+v %v", res, err)
	}
	b, _ := readCode(first.Code)
	if string(b) != firstBody {
		t.Fatalf("code 파일 변화:\n%s", b)
	}
	if fileExists(filepath.Join(runsDir(), first.Code, "second")) {
		t.Fatal("second 디렉터리가 만들어짐")
	}
	if lg, _ := os.ReadFile(os.Getenv("SSH_LOG")); len(lg) != 0 {
		t.Fatalf("ssh 호출됨: %s", lg)
	}
}

func TestSecondRunSSHFailure(t *testing.T) {
	secondEnv(t, "exit 0\n")
	old := runExternal
	t.Cleanup(func() { runExternal = old })
	runExternal = func(string, string, ...string) (string, error) { return "", os.ErrPermission }
	first := firstCode(t, firstBody)
	if _, err := NewRealSecond().Run(&Job{User: "user1"}, first, []string{"h"}); err == nil {
		t.Fatal("ssh 실패는 에러")
	}
	if b, _ := readCode(first.Code); string(b) != firstBody {
		t.Fatalf("실패 시 code 불변이어야 함:\n%s", b)
	}
}

func TestBuildSecondSectionFinal(t *testing.T) {
	first := RunResult{Processed: []string{"a", "b"}, Failed: []string{"b"}}
	post := "c: OK\nb: OK\n"
	res := SecondResult{Processed: []string{"c", "b"}}
	s := buildSecondSection(res, first, []string{"b", "c", "d"}, "", post, true, "/l")
	want := "### 2차 체크 (os6_mgmt)\n대상 : b c d (3대)\n\n[!] 2차 os_check 비정상 종료 - 2차 호스트 미처리\n\n" +
		"설정체크 (설정 수정 후 재점검)\nNO FAIL : c b (2대)\n\n" +
		"최종 판정 (1차 OK 또는 2차 OK)\nFAIL : d (1대)\nNO FAIL : a b c (3대)\n\n원본 : /l\n"
	if s != want {
		t.Fatalf("got:\n%s\nwant:\n%s", s, want)
	}
}

func TestExtractSecondTarBroken(t *testing.T) {
	if err := extractSecondTar("not a tar", t.TempDir()); err == nil {
		t.Fatal("깨진 tar 는 에러")
	}
}

func TestParsePostapplyFailed(t *testing.T) {
	post := readTestdata(t, "check.res_user1_postapply")
	got := parsePostapplyFailed(post, []string{"host01", "host02", "host03", "host04"})
	if strings.Join(got, ",") != "host02" {
		t.Fatalf("failed: %v", got)
	}
}

func TestRunnerRunFillsFailed(t *testing.T) {
	needBash(t)
	setupDir(t)
	oldC := os_check_sh
	t.Cleanup(func() { os_check_sh = oldC })
	td, _ := filepath.Abs("testdata")
	os_check_sh = filepath.Join(t.TempDir(), "os_check_stub.sh")
	os.WriteFile(os_check_sh, []byte("#!/bin/bash\ncat '"+td+"/os_check.log'\ncp '"+td+"/check.res_user1_postapply' check.res_user1_postapply\n"), 0755)
	res, err := NewRealRunner().Run(&Job{ID: "j1", User: "user1"}, []string{"host01", "host02", "host03", "host04"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Failed, ",") != "host02" {
		t.Fatalf("Failed: %v", res.Failed)
	}
}
