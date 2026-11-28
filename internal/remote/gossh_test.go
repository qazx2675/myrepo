package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkExec(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// 찾는 순서: -gossh, 환경변수, 실행 파일 옆(gossh_os8 가 gossh 보다 먼저), PATH(같은 순서).
func TestFindGosshSearchOrder(t *testing.T) {
	flagDir, envDir, pathDir := t.TempDir(), t.TempDir(), t.TempDir()
	flagBin := mkExec(t, flagDir, "gossh_flag")
	envBin := mkExec(t, envDir, "gossh_env")
	pathOS8 := mkExec(t, pathDir, "gossh_os8")
	pathPlain := mkExec(t, pathDir, "gossh")
	t.Setenv("PATH", pathDir)
	t.Setenv("IP_CHANGE_GOSSH", envBin)

	// 1. -gossh 가 환경변수보다 먼저
	if got, err := FindGossh(flagBin); err != nil || got != flagBin {
		t.Fatalf("-gossh: %q, %v", got, err)
	}
	// 2. 환경변수가 PATH 보다 먼저
	if got, err := FindGossh(""); err != nil || got != envBin {
		t.Fatalf("환경변수: %q, %v", got, err)
	}
	// 4. PATH 에서는 gossh_os8 가 gossh 보다 먼저
	t.Setenv("IP_CHANGE_GOSSH", "")
	if got, err := FindGossh(""); err != nil || got != pathOS8 {
		t.Fatalf("PATH(gossh_os8 우선): %q, %v", got, err)
	}
	os.Remove(pathOS8)
	if got, err := FindGossh(""); err != nil || got != pathPlain {
		t.Fatalf("PATH(gossh): %q, %v", got, err)
	}
	// 3. 실행 파일 옆이 PATH 보다 먼저, 거기서도 gossh_os8 가 먼저
	exe, err := os.Executable()
	if err != nil {
		t.Skip("실행 파일 경로를 알 수 없습니다")
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	exeDir := filepath.Dir(exe)
	var made []string
	for _, n := range []string{"gossh", "gossh_os8"} {
		p := filepath.Join(exeDir, n)
		if _, err := os.Stat(p); err == nil {
			t.Skipf("실행 파일 옆에 이미 %s 가 있어 건너뜁니다", p)
		}
		mkExec(t, exeDir, n)
		made = append(made, p)
	}
	t.Cleanup(func() {
		for _, p := range made {
			os.Remove(p)
		}
	})
	if got, err := FindGossh(""); err != nil || got != filepath.Join(exeDir, "gossh_os8") {
		t.Fatalf("실행 파일 옆(gossh_os8 우선): %q, %v", got, err)
	}
	os.Remove(filepath.Join(exeDir, "gossh_os8"))
	if got, err := FindGossh(""); err != nil || got != filepath.Join(exeDir, "gossh") {
		t.Fatalf("실행 파일 옆(gossh): %q, %v", got, err)
	}
}

// 지정한 경로가 틀리면 다음 단계로 넘어가지 않고 그 경로를 오류로 알린다.
func TestFindGosshExplicitWrongPathDoesNotFallThrough(t *testing.T) {
	dir := t.TempDir()
	good := mkExec(t, dir, "gossh")
	t.Setenv("PATH", dir)
	t.Setenv("IP_CHANGE_GOSSH", good)

	bad := filepath.Join(dir, "없음", "gossh")
	if _, err := FindGossh(bad); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("-gossh 틀린 경로: %v", err)
	}
	t.Setenv("IP_CHANGE_GOSSH", bad)
	if _, err := FindGossh(""); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("환경변수 틀린 경로: %v", err)
	}
	// 이름만 준 경우는 PATH 에서 찾는다
	t.Setenv("IP_CHANGE_GOSSH", "")
	if got, err := FindGossh("gossh"); err != nil || got != good {
		t.Errorf("이름으로 지정: %q, %v", got, err)
	}
	if _, err := FindGossh("없는이름"); err == nil || !strings.Contains(err.Error(), "PATH 에서 찾지 못했습니다") {
		t.Errorf("없는 이름: %v", err)
	}
	// 실행 권한이 없는 파일은 쓰지 않는다
	noExec := filepath.Join(dir, "plain")
	if err := os.WriteFile(noExec, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := FindGossh(noExec); err == nil {
		t.Errorf("실행 권한이 없는 파일을 받아들였습니다")
	}
}

// 어디에도 없으면 안내 문구 하나로 실패한다.
func TestFindGosshNotFoundMessage(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("IP_CHANGE_GOSSH", "")
	_, err := FindGossh("")
	if err == nil || err.Error() != GosshNotFound {
		t.Fatalf("err = %v", err)
	}
	for _, w := range []string{"gossh 를 찾지 못했습니다", "-gossh", "IP_CHANGE_GOSSH"} {
		if !strings.Contains(GosshNotFound, w) {
			t.Errorf("안내 문구에 %q 가 없습니다: %s", w, GosshNotFound)
		}
	}
}

// 샘플 표준에러 줄로 키 인증 실패를 알아본다 (노드 출력이 없을 때만).
func TestAuthFailureDetectionFromSampleStderr(t *testing.T) {
	line := "node01: ERROR: SSH 접속 실패: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain\n"
	res := attachErrs(parse("node02: RESULT|OK|node02\n"), line+"node02: 진행 중\n")
	if len(res) != 2 || res[0].Host != "node01" || res[1].Host != "node02" {
		t.Fatalf("결과: %+v", res)
	}
	if !res[0].AuthFailed() {
		t.Errorf("node01 은 인증 실패여야 합니다: %+v", res[0])
	}
	if res[1].AuthFailed() {
		t.Errorf("node02 는 노드 출력이 있으므로 인증 실패가 아닙니다: %+v", res[1])
	}
	if len(res[0].Errs) != 1 || !strings.HasPrefix(res[0].Errs[0], "ERROR: SSH 접속 실패:") {
		t.Errorf("Errs = %q", res[0].Errs)
	}
}
