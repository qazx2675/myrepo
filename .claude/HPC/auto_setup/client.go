// client.go - SnapshotSource(로컬 직접 / gossh 원샷 원격) 와 원격 클라이언트 모드 전송
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// SnapshotSource: 리포트/TUI 가 상태를 읽고 요청을 남기는 통로.
//
//	Snapshot: 현재 스냅샷 (호출 1회 = 로컬 파일 읽기 또는 gossh 1회)
//	Request : 요청 파일 생성. kind=ReqManualRun{jobid,yml} | ReqCancel{jobid} | ReqRefresh{}
//	Code    : codes/NNNN.txt 내용
//	Local   : 이 프로세스가 상태 디렉터리를 직접 읽으면 true (원격이면 false)
type SnapshotSource interface {
	Snapshot() (Snapshot, error)
	Request(kind string, payload map[string]string) error
	Code(n string) (string, error)
	Local() bool
}

// newSource: os8_mgmt 가 비어 있지 않으면 원격, 아니면 로컬
func newSource() SnapshotSource {
	if os8_mgmt != "" {
		return remoteSource{}
	}
	return localSource{}
}

// snapshotNow: 스냅샷 기준 시각. 테스트 결정성을 위해 AUTO_SETUP_NOW(epoch 초)가 있으면 그 값을 쓴다.
func snapshotNow() time.Time {
	if v := os.Getenv("AUTO_SETUP_NOW"); v != "" {
		if e, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(e, 0)
		}
	}
	return time.Now()
}

type localSource struct{}

func (localSource) Snapshot() (Snapshot, error) { return BuildSnapshot(dataDir(), snapshotNow()), nil }

func (localSource) Request(kind string, payload map[string]string) error {
	_, err := writeRequest(kind, payload)
	return err
}

func (localSource) Code(n string) (string, error) {
	b, err := readCode(n)
	return string(b), err
}

func (localSource) Local() bool { return true }

type remoteSource struct{}

func (remoteSource) Snapshot() (Snapshot, error) {
	r, err := remoteRun([]string{"snapshot"})
	if err != nil {
		return Snapshot{}, err
	}
	if r.RC != 0 {
		return Snapshot{}, remoteFail("snapshot", r)
	}
	var s Snapshot
	if err := json.Unmarshal([]byte(r.Stdout), &s); err != nil {
		return Snapshot{}, fmt.Errorf("snapshot JSON 해석 실패: %v", err)
	}
	return s, nil
}

func (remoteSource) Request(kind string, payload map[string]string) error {
	args, err := requestArgs(kind, payload)
	if err != nil {
		return err
	}
	r, err := remoteRun(args)
	if err != nil {
		return err
	}
	if r.RC != 0 {
		return remoteFail("request", r)
	}
	return nil
}

func (remoteSource) Code(n string) (string, error) {
	r, err := remoteRun([]string{"code", n})
	if err != nil {
		return "", err
	}
	if r.RC != 0 {
		return "", errors.New("code 없음")
	}
	return r.Stdout, nil
}

func (remoteSource) Local() bool { return false }

func remoteFail(what string, r remoteResult) error {
	m := strings.TrimSpace(r.Stdout + r.Stderr)
	if m == "" {
		m = "종료코드 " + strconv.Itoa(r.RC)
	}
	return fmt.Errorf("원격 %s 실패: %s", what, m)
}

// requestArgs: Request(kind, payload) → 원격 `auto_setup request …` 인자
func requestArgs(kind string, payload map[string]string) ([]string, error) {
	switch kind {
	case ReqManualRun:
		args := []string{"request", ReqManualRun, payload["jobid"], payload["yml"]}
		if payload["mode"] != "" {
			args = append(args, payload["mode"])
		}
		return args, nil
	case ReqRecheck:
		return []string{"request", ReqRecheck, payload["jobid"], payload["yml"]}, nil
	case ReqCancel:
		return []string{"request", ReqCancel, payload["jobid"]}, nil
	case ReqRefresh:
		return []string{"request", ReqRefresh}, nil
	}
	return nil, fmt.Errorf("지원하지 않는 요청 종류: %q", kind)
}

// ---- gossh 원샷 ----

const (
	markRC = "==RC=="
	markO  = "==O=="
	markE  = "==E=="
)

// remoteResult: 원격 auto_setup 실행 결과 (stdout/stderr 는 바이트 그대로)
type remoteResult struct {
	Stdout, Stderr string
	RC             int
}

func remoteBin() string {
	if os8_autosetup != "" {
		return os8_autosetup
	}
	return "/usr/local/bin/auto_setup"
}

// remoteCommand: gossh 에 넘길 원격 명령.
// gossh 는 줄마다 공백을 다듬고 빈 줄을 버리며 stdout/stderr 를 따로 내보내므로, 바이트 동일을 위해
// 원격에서 stdout·stderr 를 파일로 받아 base64 로 싣고 종료코드를 표지줄로 보낸다.
// 로그인 셸이 csh 여도 되도록 전체를 한 줄 `sh -c '…'` 로 만든다.
func remoteCommand(args []string) string {
	var sb strings.Builder
	sb.WriteString(shQuote(remoteBin()))
	for _, a := range args {
		sb.WriteString(" " + shQuote(a))
	}
	script := "t=$(mktemp -d) || exit 97; " + sb.String() + " </dev/null >$t/o 2>$t/e; r=$?; " +
		"echo " + markRC + "$r; echo " + markO + "; base64 $t/o; echo " + markE + "; base64 $t/e; rm -rf $t"
	return "sh -c " + shQuote(script)
}

// remoteRun: os8_mgmt 에서 `<os8_autosetup> args…` 를 gossh 원샷으로 실행. err 은 전송 실패(응답 없음/형식 오류)만.
func remoteRun(args []string) (remoteResult, error) {
	f, err := os.CreateTemp("", "as_remote_")
	if err != nil {
		return remoteResult{}, err
	}
	p := f.Name()
	defer cleanupTemp(p)
	_, werr := f.WriteString(os8_mgmt + "\n")
	f.Close()
	if werr != nil {
		return remoteResult{}, werr
	}
	out, err := runExternal("", "gossh", "-pm", "-script", "-w", p, remoteCommand(args))
	res, perr := parseRemote(os8_mgmt, out)
	if perr != nil {
		if err != nil {
			return remoteResult{}, fmt.Errorf("%s 원격 실행 실패: %v", os8_mgmt, err)
		}
		return remoteResult{}, fmt.Errorf("%s 원격 실행 실패: %v", os8_mgmt, perr)
	}
	return res, nil
}

// parseRemote: gossh stdout("host: 줄") 에서 host 접두를 떼고 표지·base64 를 복원
func parseRemote(host, out string) (remoteResult, error) {
	prefix := host + ": "
	var r remoteResult
	var ob, eb strings.Builder
	mode := 0 // 0 표지 전, 1 stdout, 2 stderr
	haveRC, haveO, haveE := false, false, false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		body := line[len(prefix):]
		switch {
		case strings.HasPrefix(body, markRC):
			n, err := strconv.Atoi(strings.TrimSpace(body[len(markRC):]))
			if err != nil {
				return r, errors.New("종료코드 형식 오류")
			}
			r.RC, haveRC = n, true
		case body == markO:
			mode, haveO = 1, true
		case body == markE:
			mode, haveE = 2, true
		case mode == 1:
			ob.WriteString(body)
		case mode == 2:
			eb.WriteString(body)
		}
	}
	if !haveRC || !haveO || !haveE {
		return r, errors.New("응답 없음 또는 형식 오류 (접속 불가?)")
	}
	so, err1 := base64.StdEncoding.DecodeString(ob.String())
	se, err2 := base64.StdEncoding.DecodeString(eb.String())
	if err1 != nil || err2 != nil {
		return r, errors.New("출력 복원 실패")
	}
	r.Stdout, r.Stderr = string(so), string(se)
	return r, nil
}

// relayRemote: 같은 하위명령을 os8_mgmt 에서 실행해 stdout/stderr/종료코드를 그대로 중계
func relayRemote(args []string) int {
	r, err := remoteRun(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, ctlPaint("[X]", 31, ctlIsTTY(os.Stderr)), err)
		return 1
	}
	os.Stdout.WriteString(r.Stdout)
	os.Stderr.WriteString(r.Stderr)
	return r.RC
}
