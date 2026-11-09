package main

// B서버 원격 실행: 설정 STRESS_REMOTE=<B주소> 가 있으면 stress-test(-steps·-yaml-state)를 B 에서 ssh 로 실행한다.
// 판정·CPU 감시(Probe)·설정수정은 지금처럼 A(이 서버)에서 한다. STRESS_REMOTE 가 비면 지금처럼 A 로컬 실행.
// 계정은 root 고정, 비밀번호는 아래 상수. 비밀번호는 명령줄·로그에 넣지 않고 SSH_ASKPASS 로만 넘긴다:
// ssh 를 띄울 때 SSH_ASKPASS=<이 바이너리>, NEW_SETUP_ASKPASS=1 을 환경변수로 주면 ssh 가 이 바이너리를 다시 부르고,
// main() 맨 앞에서 그 표식을 보고 비밀번호만 stdout 에 낸 뒤 끝난다(askpassMode).

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// remoteUser 는 B서버 접속 계정(고정).
const remoteUser = "root"

// remotePassword 는 B서버 root 비밀번호.
// ★ 사용자 결정 2026-11-09: 코드에 하드코딩한다(공개 저장소 노출 감수).
// 명령줄·로그·doctor 출력에는 절대 넣지 않는다. ssh 가 SSH_ASKPASS 로 이 바이너리를 부를 때만 stdout 으로 낸다.
const remotePassword = "abc135!!"

// askpassEnv 는 "비밀번호만 출력하고 끝내라"는 표식 환경변수(ssh 의 환경에만 넣고, ssh 가 부르는 askpass 자식이 물려받는다).
const askpassEnv = "NEW_SETUP_ASKPASS"

// remoteShortTimeout 은 목록 복사·원격 중지(kill)·doctor 접속 확인 같은 짧은 ssh 한 번의 시간 제한.
const remoteShortTimeout = 2 * time.Minute

// askpassMode 는 이번 실행이 SSH_ASKPASS 로 불린 것인지(표식 환경변수가 1) 본다.
func askpassMode(getenv func(string) string) bool {
	return getenv(askpassEnv) == "1"
}

// sshEnv 는 ssh 에 줄 환경변수: base 에서 SSH_ASKPASS·SSH_ASKPASS_REQUIRE·표식을 빼고 새로 넣는다.
// DISPLAY 는 있으면 그대로, 비어 있으면 ":0"(OpenSSH 8.0 은 DISPLAY 가 없으면 askpass 를 쓰지 않는다).
// 비밀번호 자체는 넣지 않는다(askpass 자식이 바이너리 안의 상수를 낸다).
func sshEnv(base []string, exe string) []string {
	display := ""
	var env []string
	for _, kv := range base {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", askpassEnv:
			continue
		case "DISPLAY":
			display = v
			continue
		}
		env = append(env, kv)
	}
	if display == "" {
		display = ":0"
	}
	return append(env, "DISPLAY="+display, "SSH_ASKPASS="+exe, "SSH_ASKPASS_REQUIRE=force", askpassEnv+"=1")
}

// sshArgv 는 ssh 인자: 비밀번호 인증만, 호스트 키 확인 안 함, 접속·응답 시간 제한. 원격 명령은 sh -c '<script>' 로 넘긴다
// (B 의 root 로그인 셸이 csh 여도 같은 문법으로 돌도록).
func sshArgv(host, script string) []string {
	return []string{
		"-T",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "PreferredAuthentications=password,keyboard-interactive",
		"-o", "PubkeyAuthentication=no",
		"-o", "NumberOfPasswordPrompts=1",
		"-o", "ConnectTimeout=15",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=4",
		remoteUser + "@" + host,
		"sh -c " + shQuote(script),
	}
}

// remoteConfErr 는 원격 모드 설정을 검사한다. STRESS_REMOTE 가 비면 nil.
// 원격 모드에서 STRESS_TEST·STRESS_TEST_CONF 는 B 의 경로이므로 절대경로('/' 로 시작)여야 한다.
func remoteConfErr(c config) error {
	if c.StressRemote == "" {
		return nil
	}
	if strings.HasPrefix(c.StressRemote, "-") || strings.ContainsAny(c.StressRemote, " \t'\"@;$`\\") {
		return fmt.Errorf("설정 STRESS_REMOTE 값 %q 이(가) 올바른 호스트 이름·IP 가 아닙니다", c.StressRemote)
	}
	for _, kv := range [][2]string{{"STRESS_TEST", c.StressTest}, {"STRESS_TEST_CONF", c.StressTestConf}} {
		if kv[1] != "" && !path.IsAbs(kv[1]) {
			return fmt.Errorf("STRESS_REMOTE 를 쓰면 %s 는 B서버의 절대경로('/' 로 시작)여야 합니다: %s", kv[0], kv[1])
		}
	}
	return nil
}

// remoteDir 는 B 에서 목록 파일·pid 파일을 두는 폴더(/tmp/new_setup_<user>). user 의 '/' 는 '_' 로 바꾼다.
func remoteDir(user string) string {
	return "/tmp/new_setup_" + strings.ReplaceAll(user, "/", "_")
}

// remotePidFile 은 B 에서 stress-test -steps <steps> 의 pid 를 적는 파일.
func remotePidFile(user, steps string) string {
	return remoteDir(user) + "/" + steps + ".pid"
}

// stressArgs 는 stress-test -steps 실행 인자(로컬·원격 공통). end 는 steps 에 b 가 있을 때만, conf 는 비어 있지 않을 때만 넣는다.
// ignoreOffline 이면 -ignore-offline(접속체크에 응답 없는 호스트도 launch)을 붙인다.
func stressArgs(user, list, steps, end, conf string, ignoreOffline bool) []string {
	args := []string{"-user", user, "-hosts", list, "-steps", steps, "-batch"}
	if strings.Contains(steps, "b") && end != "" {
		args = append(args, "-end", end)
	}
	if conf != "" {
		args = append(args, "-conf", conf)
	}
	if ignoreOffline {
		args = append(args, "-ignore-offline")
	}
	return args
}

// shJoin 은 인자마다 shQuote 해서 공백으로 잇는다.
func shJoin(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = shQuote(a)
	}
	return strings.Join(q, " ")
}

// remoteStressScript 는 B 에서 돌릴 stress-test -steps 스크립트와 worker.log 에 남길 표시용 줄(비밀번호 없음)을 만든다.
// 스크립트는 바이너리 폴더로 가서 자기 pid 를 pid 파일에 적고 exec 로 stress-test 가 된다(그래서 그 pid 에 SIGTERM 을 보낼 수 있다).
func remoteStressScript(c config, user, steps, list, end string) (script, disp string) {
	bin := c.StressTest
	dir := path.Dir(bin)
	args := stressArgs(user, list, steps, end, c.StressTestConf, c.StressIgnoreOffline)
	script = "cd " + shQuote(dir) + " && echo $$ > " + shQuote(remotePidFile(user, steps)) + " && exec " + shQuote(bin) + " " + shJoin(args)
	disp = fmt.Sprintf("ssh %s@%s (cd %s && %s %s)", remoteUser, c.StressRemote, dir, bin, strings.Join(args, " "))
	return script, disp
}

// remoteYamlScript 는 B 에서 돌릴 stress-test -yaml-state 스크립트와 표시용 줄. -ignore-offline 은 넣지 않는다.
func remoteYamlScript(c config, user string) (script, disp string) {
	bin := c.StressTest
	dir := path.Dir(bin)
	args := []string{"-user", user, "-yaml-state"}
	if c.StressTestConf != "" {
		args = append(args, "-conf", c.StressTestConf)
	}
	script = "cd " + shQuote(dir) + " && exec " + shQuote(bin) + " " + shJoin(args)
	disp = fmt.Sprintf("ssh %s@%s (cd %s && %s %s)", remoteUser, c.StressRemote, dir, bin, strings.Join(args, " "))
	return script, disp
}

// remoteCopyScript 는 stdin 을 B 의 remotePath 로 쓰는 스크립트(폴더가 없으면 만든다).
func remoteCopyScript(remotePath string) string {
	return "mkdir -p " + shQuote(path.Dir(remotePath)) + " && cat > " + shQuote(remotePath)
}

// remoteKillScript 는 B 의 stress-test(-steps <steps>)에 SIGTERM 을 보내는 스크립트(템플릿 인벤토리를 되돌리게). 없으면 조용히 끝난다.
func remoteKillScript(user, steps string) string {
	p := shQuote(remotePidFile(user, steps))
	return "kill -TERM $(cat " + p + " 2>/dev/null) 2>/dev/null; rm -f " + p
}

// sshFailErr 는 ssh 자체 실패(종료코드 255)의 오류.
func sshFailErr(host string) error {
	return fmt.Errorf("B서버(%s) ssh 접속 실패(종료코드 255)", host)
}

// isSSHFail 은 err 가 ssh 종료코드 255(접속·인증 실패 등)인지 본다.
func isSSHFail(err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == 255
}

// sshCommand 는 B 에서 script 를 돌리는 ssh 명령을 만든다. 비밀번호는 SSH_ASKPASS(이 바이너리)로만 넘긴다.
// ssh 는 새 세션(setsid, 제어 터미널 없음)으로 띄워야 터미널 대신 askpass 를 쓴다(새 세션도 새 프로세스 그룹이라 그룹 종료는 그대로).
func sshCommand(host, script string) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("SSH_ASKPASS 로 쓸 실행 파일 경로를 알 수 없습니다: %v", err)
	}
	if a, err := filepath.Abs(exe); err == nil {
		exe = a
	}
	cmd := exec.Command("ssh", sshArgv(host, script)...)
	cmd.Env = sshEnv(os.Environ(), exe)
	setSetsid(cmd) // 제어 터미널 없음(daemon_linux.go). runCmd 의 setProcessGroup 은 이 설정을 덮어쓰지 않는다
	return cmd, nil
}

// remoteCopy 는 로컬 목록 파일을 B 의 remotePath 로 복사한다(ssh 의 stdin 으로 보냄).
func (e *realExec) remoteCopy(local, remotePath string) error {
	host := e.conf.StressRemote
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd, err := sshCommand(host, remoteCopyScript(remotePath))
	if err != nil {
		return err
	}
	cmd.Stdin = f
	fmt.Fprintf(e.log, "+ ssh %s@%s (목록 복사 %s → %s)\n", remoteUser, host, local, remotePath)
	out, err := runCmd(cmd, remoteShortTimeout)
	io.WriteString(e.log, out)
	switch {
	case err == nil:
		return nil
	case isSSHFail(err):
		return sshFailErr(host)
	case errors.Is(err, errTimeout), errors.Is(err, errCanceled):
		return err
	}
	return fmt.Errorf("B서버(%s)로 목록 파일 복사 실패: %v", host, err)
}

// remoteKill 은 B 의 stress-test(-steps <steps>)에 SIGTERM 을 보낸다(중지·시간초과 뒤, 실패해도 무시).
// ★ 중지 요청 뒤에는 cancelCh 가 이미 닫혀 있으므로 취소 채널 없이 짧은 시간 제한으로만 돌린다.
func (e *realExec) remoteKill(steps string) {
	host := e.conf.StressRemote
	cmd, err := sshCommand(host, remoteKillScript(e.user, steps))
	if err != nil {
		fmt.Fprintf(e.log, "경고: B서버 stress-test 중지 신호를 보내지 못했습니다: %v\n", err)
		return
	}
	fmt.Fprintf(e.log, "+ ssh %s@%s (kill -TERM $(cat %s))\n", remoteUser, host, remotePidFile(e.user, steps))
	out, err := runCmdCancel(cmd, remoteShortTimeout, nil)
	io.WriteString(e.log, out)
	if err != nil {
		fmt.Fprintf(e.log, "경고: B서버 stress-test 중지 신호 실패(무시): %s\n", errText(err))
	}
}

// remoteStressTest 는 목록 파일을 B 로 복사한 뒤 B 에서 stress-test -steps 를 ssh 로 실행한다.
// stdout 감시 표식·stdin 답은 로컬 실행과 같다. 중지·시간초과면 로컬 ssh 를 끝낸 뒤 B 의 stress-test 에도 SIGTERM 을 보낸다.
func (e *realExec) remoteStressTest(steps, listFile, end string, launched func(), registered func(yml string, hosts []string), busy func(msg string) string) (int, error) {
	host := e.conf.StressRemote
	remoteList := remoteDir(e.user) + "/" + filepath.Base(listFile)
	if err := e.remoteCopy(listFile, remoteList); err != nil {
		return -1, err
	}
	script, disp := remoteStressScript(e.conf, e.user, steps, remoteList, end)
	cmd, err := sshCommand(host, script)
	if err != nil {
		return 2, err
	}
	fmt.Fprintf(e.log, "+ %s\n", disp)
	err = e.watchStress(cmd, launched, registered, busy)
	if err == nil {
		return 0, nil
	}
	if errors.Is(err, errTimeout) || errors.Is(err, errCanceled) {
		e.remoteKill(steps)
		return -1, err
	}
	if isSSHFail(err) {
		return -1, sshFailErr(host)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}
