package remote

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 가짜 gossh: -w 호스트 파일을 읽어 호스트마다 한 줄을 냅니다. node01 은 패스워드(-p)가
// $FAKE_PW 와 같을 때만 성공하고, 아니면 키 인증 실패 문구를 표준에러로 냅니다.
// 원격 명령은 실행하지 않습니다. 호출 기록은 $FAKE_LOG 에 "pw=<값> hosts=<목록>" 로 남깁니다.
const fakeGossh = "#!/bin/sh\n" + `hostfile=""; pw=""
while [ $# -gt 0 ]; do
  case "$1" in
    -w) hostfile="$2"; shift 2 ;;
    -p) pw="$2"; shift 2 ;;
    -script) shift ;;
    -u|-i|-P|-c|-t) shift 2 ;;
    *) shift ;;
  esac
done
hosts=""
while read -r h; do
  [ -n "$h" ] || continue
  hosts="$hosts $h"
  if [ "$h" = node01 ] && [ "$pw" != "$FAKE_PW" ]; then
    echo "$h: ERROR: SSH 접속 실패: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain" >&2
  else
    echo "$h: RESULT|OK|$h"
  fi
done < "$hostfile"
echo "pw=$pw hosts=$hosts" >> "$FAKE_LOG"
`

func newRunner(t *testing.T, fakePW string, ask bool, prompt func() (string, error)) (*Runner, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("리눅스에서만 합니다")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gossh")
	if err := os.WriteFile(bin, []byte(fakeGossh), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_LOG", log)
	t.Setenv("FAKE_PW", fakePW)
	return &Runner{Opts: Options{GosshPath: bin, User: "root"}, AskPassword: ask, Prompt: prompt}, log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func byHost(res []Result) map[string]Result {
	m := map[string]Result{}
	for _, r := range res {
		m[r.Host] = r
	}
	return m
}

// 키 인증이 모두 되면 패스워드를 묻지 않고 gossh 를 한 번만 부른다.
func TestRunnerKeyAuthOnlyNoPrompt(t *testing.T) {
	asked := 0
	r, log := newRunner(t, "secret", true, func() (string, error) { asked++; return "secret", nil })
	res, _, err := r.Run([]string{"node02", "node03"}, "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if asked != 0 || len(calls(t, log)) != 1 || len(res) != 2 {
		t.Fatalf("묻기=%d 호출=%v 결과=%+v", asked, calls(t, log), res)
	}
}

// -p 가 없으면 키 인증이 실패한 노드가 있어도 묻지 않고 그대로 알린다.
func TestRunnerAuthFailureWithoutPasswordOption(t *testing.T) {
	asked := 0
	r, log := newRunner(t, "secret", false, func() (string, error) { asked++; return "secret", nil })
	res, _, _ := r.Run([]string{"node01", "node02"}, "echo hi")
	m := byHost(res)
	if asked != 0 || len(calls(t, log)) != 1 || !m["node01"].AuthFailed() || m["node01"].PasswordTried {
		t.Fatalf("묻기=%d 호출=%v 결과=%+v", asked, calls(t, log), res)
	}
}

// -p 이면 키 인증이 실패한 노드만 패스워드로 다시 실행하고, 한 번 받은 패스워드는 다시 묻지 않는다.
func TestRunnerPasswordRetryOnlyFailedHosts(t *testing.T) {
	asked := 0
	r, log := newRunner(t, "secret", true, func() (string, error) { asked++; return "secret", nil })
	res, _, err := r.Run([]string{"node01", "node02"}, "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	m := byHost(res)
	if len(res) != 2 {
		t.Fatalf("같은 호스트가 두 번 나왔습니다: %+v", res)
	}
	if m["node01"].AuthFailed() || m["node01"].LastLine() != "RESULT|OK|node01" || !m["node01"].PasswordTried {
		t.Errorf("node01 = %+v", m["node01"])
	}
	if m["node02"].PasswordTried || m["node02"].LastLine() != "RESULT|OK|node02" {
		t.Errorf("node02 = %+v", m["node02"])
	}
	got := calls(t, log)
	if len(got) != 2 || got[0] != "pw= hosts= node01 node02" || got[1] != "pw=secret hosts= node01" {
		t.Errorf("gossh 호출 = %q", got)
	}
	// 두 번째 Run (예: 게이트웨이 고침) 은 패스워드를 다시 묻지 않는다
	if _, _, err := r.Run([]string{"node01"}, "echo again"); err != nil {
		t.Fatal(err)
	}
	if asked != 1 {
		t.Errorf("패스워드를 %d 번 물었습니다 (1번이어야 합니다)", asked)
	}
}

// 패스워드가 틀리면 PasswordTried 로 표시되고 여전히 인증 실패다.
func TestRunnerWrongPassword(t *testing.T) {
	r, _ := newRunner(t, "secret", true, func() (string, error) { return "wrong", nil })
	res, _, _ := r.Run([]string{"node01"}, "echo hi")
	if len(res) != 1 || !res[0].AuthFailed() || !res[0].PasswordTried {
		t.Fatalf("결과 = %+v", res)
	}
}

// 패스워드를 입력받지 못하면 두 번째 실행 없이 사유를 남긴다.
func TestRunnerPasswordPromptFails(t *testing.T) {
	r, log := newRunner(t, "secret", true, func() (string, error) { return "", errors.New("터미널(/dev/tty)을 열 수 없습니다") })
	res, _, _ := r.Run([]string{"node01"}, "echo hi")
	if len(res) != 1 || !strings.Contains(res[0].Note, "패스워드를 입력받지 못했습니다: 터미널(/dev/tty)을 열 수 없습니다") {
		t.Fatalf("결과 = %+v", res)
	}
	if n := len(calls(t, log)); n != 1 {
		t.Errorf("gossh 를 %d 번 불렀습니다", n)
	}
}
