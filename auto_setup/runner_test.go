// runner_test.go - runner.go 단위 테스트: code 파일 구성, FAIL/NO FAIL, Processed, Abnormal, os6 래퍼 quoting, Run 통합
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func needBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash 없음")
	}
}

func TestBuildCodeTextOrderAndContent(t *testing.T) {
	logText := readTestdata(t, "os_check.log")
	post := readTestdata(t, "check.res_user1_postapply")
	hosts := []string{"host01", "host02", "host03", "host04"}
	text, abnormal := buildCodeText(logText, post, true, hosts, "/x/runs/1234/os_check.log")
	if abnormal {
		t.Fatal("정상 로그가 비정상으로 판정됨")
	}
	order := []string{
		reportStart, "[담당자 문구] case6", reportEnd,
		"설정체크 (설정 수정 후 재점검)", "host02: FAIL selinux enforcing", "host02: FAIL ntp not synced", "NO FAIL : host01 host03 (2대)",
		"===== LDAP 정보", "INFO ldap infra", "===== SPLUNK 정보", "===== 커널 버전", "===== SDS infra 커널 버전",
		"원본 : /x/runs/1234/os_check.log",
	}
	prev := -1
	for _, s := range order {
		i := strings.Index(text, s)
		if i < 0 || i <= prev {
			t.Fatalf("%q 위치 오류(i=%d prev=%d):\n%s", s, i, prev, text)
		}
		prev = i
	}
	for _, s := range []string{"OS 체크 결과 요약", "환경설정 점검결과", "host01: OK"} {
		if strings.Contains(text, s) {
			t.Fatalf("%q 는 포함되면 안 됨:\n%s", s, text)
		}
	}
	if !strings.HasSuffix(text, "원본 : /x/runs/1234/os_check.log\n") {
		t.Fatalf("마지막 줄: %q", text)
	}
}

func TestBuildCodeTextAllNoFail(t *testing.T) {
	post := "h1: OK\nh2: OK\n"
	text, _ := buildCodeText("", post, true, []string{"h1", "h2", "h3"}, "/l")
	if !strings.Contains(text, "설정체크 (설정 수정 후 재점검)\nNO FAIL : h1 h2 (2대)\n") {
		t.Fatalf("NO FAIL 한 줄만 있어야 함:\n%s", text)
	}
	if strings.Count(text, "FAIL") != 1 {
		t.Fatalf("FAIL 줄이 섞임:\n%s", text)
	}
}

func TestBuildCodeTextAbnormal(t *testing.T) {
	// 결과 리포트 블록 없음 → 비정상, 있는 것(LDAP)만 발췌
	logText := "===== LDAP 정보 (접속 가능 서버) =====\nINFO ldap infra\n=====================================\n"
	text, abnormal := buildCodeText(logText, "", false, []string{"h1"}, "/l")
	if !abnormal {
		t.Fatal("결과 리포트가 없으면 Abnormal")
	}
	if !strings.Contains(text, "(재점검 결과 없음)") || !strings.Contains(text, "INFO ldap infra") || !strings.HasSuffix(text, "원본 : /l\n") {
		t.Fatalf("본문:\n%s", text)
	}
	// 시작줄만 있고 끝이 없는 불완전 블록도 비정상 + 있는 것까지 발췌
	text, abnormal = buildCodeText(reportStart+"\nabc\n", "", false, nil, "/l")
	if !abnormal || !strings.Contains(text, "abc") {
		t.Fatalf("불완전 블록: %v\n%s", abnormal, text)
	}
}

func TestParsePostapplyHosts(t *testing.T) {
	post := readTestdata(t, "check.res_user1_postapply")
	got := parsePostapplyHosts(post, []string{"host01", "host02", "host03", "host04"})
	if strings.Join(got, ",") != "host01,host02,host03" {
		t.Fatalf("processed: %v", got)
	}
	// 요청하지 않은 호스트는 제외
	got = parsePostapplyHosts(post, []string{"host03"})
	if strings.Join(got, ",") != "host03" {
		t.Fatalf("필터: %v", got)
	}
}

func TestOS6WrapperQuoting(t *testing.T) {
	needBash(t)
	setupDir(t)
	oldM, oldG := os6_mgmt, os6_gossh
	t.Cleanup(func() { os6_mgmt, os6_gossh = oldM, oldG })

	tmp := t.TempDir()
	fakeGossh := filepath.Join(tmp, "fakegossh")
	os.WriteFile(fakeGossh, []byte(`#!/bin/bash
prev=
for a in "$@"; do
  echo "[$a]"
  if [ "$prev" = "-w" ]; then echo "LIST:$(cat "$a")"; fi
  prev=$a
done
exit 3
`), 0755)
	fakeBin := filepath.Join(tmp, "bin")
	os.MkdirAll(fakeBin, 0755)
	os.WriteFile(filepath.Join(fakeBin, "ssh"), []byte("#!/bin/sh\nshift\nexec bash -c \"$1\"\n"), 0755)

	os6_mgmt, os6_gossh = "mgmtstub", fakeGossh
	if err := writeOS6Wrapper(); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(binDir(), "gossh")
	if st, err := os.Stat(wrapper); err != nil || st.Mode().Perm() != 0755 {
		t.Fatalf("래퍼 권한: %v %v", st, err)
	}
	list := filepath.Join(tmp, "list.txt")
	os.WriteFile(list, []byte("h1\nh2\n"), 0644)

	weird := `it's "q" $HOME x;y`
	cmd := exec.Command("bash", wrapper, "-pm", "-w", list, weird, "cat /proc/uptime", "-script")
	cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.Output()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 3 {
		t.Fatalf("종료코드 3 이 전달되어야 함: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	// [-pm] [-w] [<tmp>] LIST:h1\nh2 → 줄바꿈으로 나뉨 / [weird] [cat /proc/uptime] [-script]
	if len(lines) < 8 || lines[0] != "[-pm]" || lines[1] != "[-w]" || !strings.HasPrefix(lines[2], "[") || lines[3] != "LIST:h1" || lines[4] != "h2" {
		t.Fatalf("출력:\n%s", out)
	}
	if lines[5] != "["+weird+"]" || lines[6] != "[cat /proc/uptime]" || lines[7] != "[-script]" {
		t.Fatalf("인자 quoting 불일치:\n%s", out)
	}
	if lines[2] == "["+list+"]" {
		t.Fatalf("-w 는 원격 임시 파일로 바뀌어야 함: %s", lines[2])
	}
}

func TestRunnerRunEndToEnd(t *testing.T) {
	needBash(t)
	setupDir(t)
	oldC := os_check_sh
	t.Cleanup(func() { os_check_sh = oldC })

	td, _ := filepath.Abs("testdata")
	tmp := t.TempDir()
	os_check_sh = filepath.Join(tmp, "os_check_stub.sh")
	os.WriteFile(filepath.Join(tmp, "dhcp.sh"), []byte("#dhcp\n"), 0644)
	os.WriteFile(os_check_sh, []byte("#!/bin/bash\necho \"$@\" > args.txt\ncat '"+td+"/os_check.log'\ncp '"+td+"/check.res_user1_postapply' check.res_user1_postapply\n"), 0755)

	j := &Job{ID: "j1", User: "user1"}
	hosts := []string{"host01", "host02", "host03", "host04"}
	res, err := NewRealRunner().Run(j, hosts, false)
	if err != nil {
		t.Fatal(err)
	}
	if !validCode(res.Code) || res.Abnormal {
		t.Fatalf("결과: %+v", res)
	}
	if strings.Join(res.Processed, ",") != "host01,host02,host03" {
		t.Fatalf("processed: %v", res.Processed)
	}
	dir := filepath.Join(runsDir(), res.Code)
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	if strings.TrimSpace(string(args)) != "-auto user1 targets.txt" {
		t.Fatalf("os_check 인자: %q", args)
	}
	tg, _ := os.ReadFile(filepath.Join(dir, "targets.txt"))
	if string(tg) != "host01\nhost02\nhost03\nhost04\n" {
		t.Fatalf("targets: %q", tg)
	}
	if _, err := os.Lstat(filepath.Join(dir, "dhcp.sh")); err != nil {
		t.Fatalf("dhcp.sh 링크 없음: %v", err)
	}
	if _, err := os.Stat(filepath.Join(binDir(), "gossh")); err == nil {
		t.Fatal("hasOS6=false 인데 래퍼가 생성됨")
	}
	b, err := readCode(res.Code)
	if err != nil || !strings.Contains(string(b), "NO FAIL : host01 host03 (2대)") ||
		!strings.HasSuffix(string(b), "원본 : "+filepath.Join(dir, "os_check.log")+"\n") {
		t.Fatalf("code 파일: %v\n%s", err, b)
	}
}

func TestRunnerEmptyOsCheckSh(t *testing.T) {
	setupDir(t)
	old := os_check_sh
	os_check_sh = ""
	t.Cleanup(func() { os_check_sh = old })
	_, err := NewRealRunner().Run(&Job{User: "u"}, []string{"h"}, false)
	if err == nil || !strings.Contains(err.Error(), "os_check_sh 가 비어 있습니다") {
		t.Fatalf("에러: %v", err)
	}
}
