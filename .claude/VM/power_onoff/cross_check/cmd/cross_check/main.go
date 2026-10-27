// cross_check 는 2인 1조 교차검증 데몬과 그 클라이언트(작업자 --run/--start/--finish, 사용자·관리자 TUI)를 담은 단일 바이너리다.
//
// --user/--admin 은 stdin·stdout 이 모두 터미널이고 linux 이면 전체 화면(auto_setup 과 같은 방식), 아니면(파이프·스크립트·
// 테스트) 또는 --plain 이면 같은 동작의 줄 입력 화면이다.
//
// 종료 코드: 0 성공/승인, 1 거부/중단, 2 사용법 오류, 3 연결·데몬·입력 오류, 130 Ctrl+C.
//
// --run 의 출력 규약(래퍼 스크립트용):
//   - 사람이 읽는 글(대기·이벤트·내 작업 리스트·정책 안내)은 모두 stderr 로 나간다.
//   - stdout 에는 승인됐을 때만 정확히 두 줄이 나간다:
//     CROSS_CHECK_JOB=<작업ID>
//     CROSS_CHECK_TOKEN=<1회용 token>
//     (CROSS_CHECK_JOB/TOKEN 두 줄은 승인됐을 때만 나온다. 환경변수 CROSS_CHECK_TOKEN 을 --start 가 읽는다.)
//     eval "$(...)" 로 바로 받으면 --run 이 실패해도 종료 코드가 0 이 되므로, 한 번 변수에 받는 안전한 형태로 쓴다:
//     out=$(cross_check --run --user U --checker C < targets) || exit 1; eval "$out"; export CROSS_CHECK_TOKEN; cross_check --start --job "$CROSS_CHECK_JOB"
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"cross_check/internal/daemon"
	"cross_check/internal/tui"
)

const version = "0.3.1"

// 종료 코드.
const (
	exitOK        = 0
	exitFail      = 1
	exitUsage     = 2
	exitError     = 3
	exitInterrupt = 130
)

// 격리 시험·비표준 설치용 환경변수(기본값은 daemon.Default*).
const (
	envConf  = "CROSS_CHECK_CONF"  // cross_check.conf 경로
	envAuth  = "CROSS_CHECK_AUTH"  // auth.conf 경로
	envState = "CROSS_CHECK_STATE" // jobs.json 경로
	envLock  = "CROSS_CHECK_LOCK"  // 데몬 잠금 파일(없으면 소켓을 바꿨을 때 그 디렉터리의 daemon.lock)
	envLog   = "CROSS_CHECK_LOG"   // 데몬 로그 파일
	envToken = "CROSS_CHECK_TOKEN" // --start/--finish 의 token
	// --run 의 호출 스크립트 이름은 envCaller(CROSS_CHECK_CALLER, caller.go)
	// 소켓은 daemon.EnvSocket(CROSS_CHECK_SOCKET)
)

// defaultLogPath 는 데몬 로그 기본 경로(쓸 수 없으면 stderr 만).
const defaultLogPath = "/var/log/cross_check.log"

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func confPath() string  { return envOr(envConf, daemon.DefaultConfPath) }
func authPath() string  { return envOr(envAuth, daemon.DefaultAuthPath) }
func statePath() string { return envOr(envState, daemon.DefaultStorePath) }
func logPath() string   { return envOr(envLog, defaultLogPath) }

func lockPath() string {
	if v := os.Getenv(envLock); v != "" {
		return v
	}
	if os.Getenv(daemon.EnvSocket) != "" {
		return filepath.Join(filepath.Dir(daemon.SocketFromEnv()), "daemon.lock")
	}
	return daemon.DefaultLockPath
}

// errP 는 stderr 용 색칠기.
var errP = tui.Painter{On: tui.ColorEnabled(os.Stderr)}

func errorf(format string, a ...any) {
	fmt.Fprintln(os.Stderr, errP.Red("[오류] "+fmt.Sprintf(format, a...)))
}

func usage() {
	ttlText := daemon.FormatTTLDuration(approvalTTL()) // 이 서버 설정(읽을 수 없으면 기본 2분)
	fmt.Fprintf(os.Stderr, `cross_check %s — 원격 2인 1조 교차검증(작업자 대기 → 확인자가 대상 붙여넣기+패스워드로 승인 → 승인 유효시간 내 본 작업)

사용법(항상 사용자를 지정):
  cross_check --user <user> [--plain]                사용자 화면: 첫 실행 시 패스워드 등록(6자 이상),
                                                     내 작업·내가 확인자인 작업 목록. 확인자는 대기 작업을 골라 Enter →
                                                     작업자에게 전달받은 대상 붙여넣기 → 패스워드 → 승인(대상이 다르면 중단)
                                                     (터미널이면 전체 화면, 파이프·--plain 이면 줄 입력 화면.
                                                      작업자·확인자 이름은 설정 파일 users= 목록에 있어야 함)
  cross_check --run --user <user> --checker <확인자>  작업 등록 후 확인자 승인까지 대기(작업 스크립트 안에서)
        [--targets-file <파일>]                       대상 목록(없으면 stdin). 줄바꿈·탭·공백 구분, '#' 주석
        [--caller <이름>]                             호출 스크립트 이름(정보용. 없으면 env %s, 그것도 없으면 부모 프로세스에서 감지)
  cross_check --start --job <ID> [--token <T>]        본 작업 직전 신호(승인 후 %s 이내 → 승인/작업중, token 은 env %s 도 가능)
  cross_check --finish --job <ID> --result done|aborted [--token <T>]
                                                     본 작업 결과: done = 스크립트가 끝까지 실행됨(승인/작업완료),
                                                     aborted = 중단·No·Ctrl+C(승인/작업중단, 시작 전에도 가능)
  cross_check --admin [--plain]                      관리자 화면(목록·상태 변경·삭제·정리·패스워드 재설정)
                                                     (auth.conf 의 admin= 항목은 install.sh 가 만든다)
  cross_check --daemon                               데몬을 포그라운드로 실행
  cross_check --ensure                               데몬이 없으면 백그라운드로 띄움(crontab 매분 실행용)
  cross_check --version | -h

  --checker 는 오타 --cechker 도 같은 뜻으로 받는다. -x 와 --x 모두 된다.

--run 출력 규약: 사람이 읽는 글은 모두 stderr, 승인되면 stdout 에 정확히 두 줄
  CROSS_CHECK_JOB=<ID>
  CROSS_CHECK_TOKEN=<token>
  (JOB/TOKEN 두 줄은 승인됐을 때만 나온다. eval "$(...)" 로 바로 받으면 실패해도 종료 코드가 0 이 되므로 아래처럼 쓴다)
  예) out=$(cross_check --run --user hong --checker kim < targets.txt) || exit 1; eval "$out"; export CROSS_CHECK_TOKEN
      cross_check --start --job "$CROSS_CHECK_JOB" || exit 1
      ... 본 작업 ...
      cross_check --finish --job "$CROSS_CHECK_JOB" --result done     (중단하면 --result aborted)
  작업중으로 work_timeout_sec(기본 86400초 = 24시간)를 넘기면 데몬이 승인/작업중단("작업자 응답 없음")으로 바꾼다.
  대기로 wait_timeout_sec(기본 600초 = 10분) 안에 승인되지 않으면 데몬이 중단("승인 대기 시간 초과")하고 --run 은 종료 코드 1.

종료 코드: 0 승인·성공, 1 중단·거부, 2 사용법 오류, 3 연결·데몬·입력 오류, 130 Ctrl+C

[필수 정책] 확인자의 승인 완료 후 %s 내에 본 작업(--start)을 진행하지 않으면 해당 작업 승인은 자동으로 취소(중단)된다.
  (승인 유효시간은 cross_check.conf 의 approval_ttl_sec 로 바꾼다: 기본 120초 = 2분, 허용 10~3600. 데몬에 SIGHUP 또는 재시작으로 반영)

경로(환경변수로 바꿀 수 있음):
  소켓 %s (%s)   설정 %s (%s)
  비밀번호 %s (%s)   작업 기록 %s (%s)
`, version, envCaller, ttlText, envToken, ttlText,
		daemon.DefaultSocketPath, daemon.EnvSocket, daemon.DefaultConfPath, envConf,
		daemon.DefaultAuthPath, envAuth, daemon.DefaultStorePath, envState)
}

func main() {
	os.Exit(realMain(os.Args[1:]))
}

func realMain(args []string) int {
	fs := flag.NewFlagSet("cross_check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		user, checker, job, token, targetsFile, caller, result  string
		run, start, finish, admin, daemonMode, ensure, showVers bool
		plain                                                   bool
	)
	fs.StringVar(&user, "user", "", "사용자")
	fs.StringVar(&checker, "checker", "", "확인자")
	fs.StringVar(&checker, "cechker", "", "확인자(오타 허용)")
	fs.StringVar(&job, "job", "", "작업 ID")
	fs.StringVar(&token, "token", "", "start token")
	fs.StringVar(&targetsFile, "targets-file", "", "대상 목록 파일")
	fs.BoolVar(&run, "run", false, "작업 등록·대기")
	fs.BoolVar(&start, "start", false, "본 작업 시작 신호")
	fs.BoolVar(&finish, "finish", false, "본 작업 결과")
	fs.StringVar(&result, "result", "", "본 작업 결과(done|aborted)")
	fs.StringVar(&caller, "caller", "", "호출 스크립트 이름")
	fs.BoolVar(&plain, "plain", false, "줄 입력 화면 강제")
	fs.BoolVar(&admin, "admin", false, "관리자 화면")
	fs.BoolVar(&daemonMode, "daemon", false, "데몬 실행")
	fs.BoolVar(&ensure, "ensure", false, "데몬 보장")
	fs.BoolVar(&showVers, "version", false, "버전")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage()
			return exitOK
		}
		usage()
		errorf("%v", err)
		return exitUsage
	}
	if fs.NArg() > 0 {
		usage()
		errorf("알 수 없는 인자: %s", strings.Join(fs.Args(), " "))
		return exitUsage
	}
	modes := 0
	for _, m := range []bool{run, start, finish, admin, daemonMode, ensure, showVers} {
		if m {
			modes++
		}
	}
	if modes > 1 {
		usage()
		errorf("--run/--start/--finish/--admin/--daemon/--ensure/--version 은 하나만 지정")
		return exitUsage
	}
	if caller != "" && !run {
		usage()
		errorf("--caller 는 --run 과 함께만 씀")
		return exitUsage
	}
	if result != "" && !finish {
		usage()
		errorf("--result 는 --finish 와 함께만 씀")
		return exitUsage
	}
	if plain && (run || start || finish || daemonMode || ensure || showVers) {
		usage()
		errorf("--plain 은 --user/--admin 화면에만 씀")
		return exitUsage
	}

	switch {
	case showVers:
		fmt.Println("cross_check", version)
		return exitOK
	case daemonMode:
		return cmdDaemon()
	case ensure:
		return cmdEnsure()
	case start:
		return cmdStart(job, token)
	case finish:
		return cmdFinish(job, token, result)
	case admin:
		return cmdAdmin(plain)
	case run:
		return cmdRun(user, checker, targetsFile, caller)
	}
	if user == "" {
		usage()
		errorf("항상 사용자를 지정하세요: cross_check --user <user>")
		return exitUsage
	}
	if checker != "" || job != "" || token != "" || targetsFile != "" {
		usage()
		errorf("--checker/--job/--token/--targets-file 은 --run/--start 와 함께만 씀")
		return exitUsage
	}
	return cmdUser(user, plain)
}

// checkUserName 은 --user 이름을 미리 검사한다(데몬도 검사하지만 메시지를 친절하게).
func checkUserName(what, name string) bool {
	if !daemon.ValidName(name) {
		errorf("%s 이름이 올바르지 않습니다(영소문자·숫자·_ 만, %d자 이하): %q", what, daemon.MaxNameLen, name)
		return false
	}
	if name == daemon.AdminUser {
		errorf("%s 은 관리자용 예약 이름입니다(관리자 화면은 cross_check --admin)", daemon.AdminUser)
		return false
	}
	return true
}

// dial 은 데몬에 연결한다. 안 되면 --ensure 를 안내한다.
func dial() (*daemon.Client, bool) {
	c, err := daemon.Dial(daemon.SocketFromEnv())
	if err != nil {
		errorf("cross_check 데몬에 연결할 수 없습니다(%s): %v", daemon.SocketFromEnv(), err)
		fmt.Fprintln(os.Stderr, "  데몬 기동: cross_check --ensure  (root crontab 의 '* * * * * /usr/local/bin/cross_check --ensure' 확인)")
		return nil, false
	}
	return c, true
}

// approvalTTL 은 설정 파일(cross_check.conf)에서 읽은 승인 유효시간(읽을 수 없으면 기본 2분).
// 실제로 적용 중인 값은 데몬이 알고 있으므로(SIGHUP 으로 바뀔 수 있음) 데몬에 연결된 뒤에는 effectiveTTL 을 쓴다.
func approvalTTL() time.Duration {
	if c, err := daemon.LoadConf(confPath()); err == nil && c.ApprovalTTL > 0 {
		return c.ApprovalTTL
	}
	return daemon.DefaultApprovalTTL
}

// effectiveTTL 은 화면·메시지에 쓸 승인 유효시간: hintSec(데몬이 이벤트·응답으로 준 값) → 데몬에 ping → 설정 파일 → 기본값.
func effectiveTTL(c *daemon.Client, hintSec int) time.Duration {
	if hintSec > 0 {
		return time.Duration(hintSec) * time.Second
	}
	if c != nil {
		if d, err := c.ApprovalTTL(); err == nil && d > 0 {
			return d
		}
	}
	return approvalTTL()
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
}

// ---- --user / --admin ----

func tuiOptions(c *daemon.Client) tui.Options {
	opt := tui.Options{
		Color:       tui.ColorEnabled(os.Stdout),
		HideInput:   tui.EchoOff,
		ApprovalTTL: effectiveTTL(c, 0),
	}
	if c != nil {
		if info, err := c.Info(); err == nil {
			opt.WaitTimeout = info.WaitTimeout // 구버전 데몬은 0(표시 안 함)
		}
	}
	return opt
}

// fullScreen 은 전체 화면을 쓸지: --plain 이 아니고, stdin·stdout 이 모두 터미널이고, linux 이고, TERM 이 dumb 가 아님.
func fullScreen(plain bool) bool {
	return !plain && os.Getenv("TERM") != "dumb" && tui.FullScreenAvailable(os.Stdin, os.Stdout)
}

// screenColor 는 전체 화면 색(NO_COLOR·CROSS_CHECK_COLOR=never 이면 끔).
func screenColor() bool { return tui.ColorEnabled(os.Stdout) }

func cmdUser(user string, plain bool) int {
	if !checkUserName("사용자", user) {
		return exitUsage
	}
	c, ok := dial()
	if !ok {
		return exitError
	}
	defer c.Close()
	if fullScreen(plain) {
		env, cleanup := tui.RealEnv(os.Stdin, os.Stdout, screenColor())
		code, done := tui.RunUserFull(env, c, user)
		cleanup()
		if done {
			return code
		}
		// raw 모드로 바꾸지 못함: 아래 줄 입력 화면으로
	}
	ctx, stop := signalContext()
	defer stop()
	return tui.New(os.Stdin, os.Stdout, c, tuiOptions(c)).RunUser(ctx, user)
}

func cmdAdmin(plain bool) int {
	c, ok := dial()
	if !ok {
		return exitError
	}
	defer c.Close()
	if fullScreen(plain) {
		env, cleanup := tui.RealEnv(os.Stdin, os.Stdout, screenColor())
		code, done := tui.RunAdminFull(env, c)
		cleanup()
		if done {
			return code
		}
	}
	ctx, stop := signalContext()
	defer stop()
	return tui.New(os.Stdin, os.Stdout, c, tuiOptions(c)).RunAdmin(ctx)
}

// ---- --run ----

// parseTargets 는 데몬과 같은 규칙으로 대상 목록을 나눈다: 줄바꿈·탭·공백 구분, 줄에서 '#' 뒤는 주석.
func parseTargets(r io.Reader) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), daemon.MaxLineSize)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		out = append(out, strings.Fields(line)...)
	}
	return out, sc.Err()
}

func readTargets(file string) ([]string, error) {
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("대상 파일을 열 수 없음: %v", err)
		}
		defer f.Close()
		return parseTargets(f)
	}
	if tui.IsTerminal(os.Stdin) {
		fmt.Fprintln(os.Stderr, "작업 대상 목록을 입력하세요(줄바꿈·탭·공백 구분, 끝나면 Ctrl+D):")
	}
	return parseTargets(os.Stdin)
}

func stamp() string { return time.Now().Format("15:04:05") }

// printEvent 는 run 연결로 온 상태 이벤트 한 줄을 그대로 보여준다(시각, 상태, 사유, 주체).
func printEvent(ev daemon.Event) {
	by := ""
	if ev.By != "" {
		by = " (by " + ev.By + ")"
	}
	reason := ev.Reason
	if reason == "" {
		reason = "-"
	}
	fmt.Fprintf(os.Stderr, "  %s %s — %s%s\n", stamp(), errP.Status(ev.Status, "["+string(ev.Status)+"]"), reason, by)
}

func cmdRun(user, checker, targetsFile, callerFlag string) int {
	if user == "" || checker == "" {
		usage()
		errorf("--run 에는 --user 와 --checker(또는 --cechker)가 모두 필요")
		return exitUsage
	}
	if !checkUserName("작업자", user) || !checkUserName("확인자", checker) {
		return exitError
	}
	caller := resolveCaller(callerFlag)
	if user == checker {
		return runSameUser(user, targetsFile, caller)
	}
	targets, err := readTargets(targetsFile)
	if err != nil {
		errorf("%v", err)
		return exitError
	}
	if len(targets) == 0 {
		errorf("작업 대상이 비어 있습니다(--targets-file 또는 stdin 으로 전달)")
		return exitError
	}

	c, ok := dial()
	if !ok {
		return exitError
	}
	defer c.Close()

	h, err := c.RunCaller(user, checker, targets, caller)
	if err != nil {
		var de *daemon.DaemonError
		if errors.As(err, &de) {
			errorf("작업 등록 거부: %s", de.Msg)
			return exitError
		}
		errorf("%v", err)
		return exitError
	}
	defer h.Close()

	ctx, stop := signalContext()
	defer stop()
	fmt.Fprintf(os.Stderr, "%s 작업 %s — 확인자 %s 의 승인을 기다립니다 (Ctrl+C 로 취소, 호출: %s)\n", errP.Yellow("[대기]"), h.ID, checker, caller)
	if line := waitLimitLine(h, time.Now()); line != "" {
		fmt.Fprintln(os.Stderr, line)
	}
	// 확인자에게 그대로 전달해 붙여넣게 하는 목록: 쉼표 없이 공백·줄바꿈으로만 구분(확인자 화면은 공백·탭·줄바꿈을 구분자로 본다)
	fmt.Fprintf(os.Stderr, "  대상 %d개:\n", len(targets))
	for i := 0; i < len(targets); i += 8 {
		end := i + 8
		if end > len(targets) {
			end = len(targets)
		}
		fmt.Fprintf(os.Stderr, "    %s\n", strings.Join(targets[i:end], " "))
	}
	fmt.Fprintf(os.Stderr, "  확인자 안내: 위 대상 목록을 확인자 %s 에게 전달하세요(확인자 화면에는 대상이 보이지 않습니다).\n", checker)
	fmt.Fprintf(os.Stderr, "             확인자는 cross_check --user %s 에서 작업 %s 를 골라 Enter → 대상 붙여넣기 → 패스워드로 승인합니다.\n", checker, h.ID)

	events := h.Events()
	for {
		select {
		case <-ctx.Done():
			h.Close() // 연결이 끊기면 데몬이 즉시 "작업자 스크립트 종료" 로 중단한다
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, errP.Red(fmt.Sprintf("[중단] 작업 %s — Ctrl+C/종료 신호로 취소(%s)", h.ID, daemon.ReasonWorkerGone)))
			return exitInterrupt
		case ev, open := <-events:
			if !open {
				werr := h.Wait(context.Background())
				var ab *daemon.AbortedError
				if errors.As(werr, &ab) {
					fmt.Fprintln(os.Stderr, errP.Red(fmt.Sprintf("[중단] 작업 %s — %s", h.ID, ab.Reason)))
					return exitFail
				}
				errorf("작업 %s: %v", h.ID, werr)
				return exitError
			}
			printEvent(ev)
			switch ev.Status {
			case daemon.StatusAborted:
				fmt.Fprintln(os.Stderr, errP.Red(fmt.Sprintf("[중단] 작업 %s — %s", h.ID, ev.Reason)))
				return exitFail
			case daemon.StatusApproved:
				return approved(c, h, user, checker, ev)
			}
		}
	}
}

// waitLimitLine 은 --run 대기 안내의 "승인 대기 제한 10분 — 15:04 까지 …" 한 줄.
// 만료 시각은 데몬이 준 wait_expires_at 을 우선하고, 없으면 now + wait_timeout_sec. 구버전 데몬(값 없음)이면 "".
func waitLimitLine(h *daemon.RunHandle, now time.Time) string {
	if h == nil || h.WaitSec <= 0 {
		return ""
	}
	wait := time.Duration(h.WaitSec) * time.Second
	exp := now.Add(wait)
	if h.WaitExpiresAt != nil {
		exp = *h.WaitExpiresAt
	}
	return fmt.Sprintf("  승인 대기 제한 %s — %s 까지 (이 시각까지 승인되지 않으면 자동 중단)",
		daemon.FormatTTLDuration(wait), exp.Local().Format("15:04"))
}

// runSameUser 는 작업자 = 확인자: 기다리지 않고 바로 중단(종료 코드 1). 데몬이 떠 있으면 중단 기록을 남긴다.
// 터미널 stdin 은 기다리지 않도록 읽지 않는다(파일·파이프로 온 대상만 기록에 담는다).
func runSameUser(user, targetsFile, caller string) int {
	var targets []string
	if targetsFile != "" || !tui.IsTerminal(os.Stdin) {
		targets, _ = readTargets(targetsFile)
	}
	note := "(데몬에 기록하지 못함)"
	if c, err := daemon.Dial(daemon.SocketFromEnv()); err == nil {
		h, err := c.RunCaller(user, user, targets, caller)
		var de *daemon.DaemonError
		switch {
		case err == nil: // 일어나면 안 되지만, 열린 작업을 남기지 않게 바로 닫는다(데몬이 중단 처리)
			h.Close()
		case errors.As(err, &de) && de.Msg == daemon.ReasonSameUser && de.ID != "":
			note = "(작업 " + de.ID + " 중단으로 기록)"
		}
		c.Close()
	}
	fmt.Fprintln(os.Stderr, errP.Red(fmt.Sprintf("[중단] %s — 작업자 %s 와 확인자 %s 가 같아 즉시 취소 %s",
		daemon.ReasonSameUser, user, user, note)))
	return exitFail
}

// approved 는 승인 직후: 승인 안내, 내 작업 리스트, 승인 유효시간 정책, stdout 의 기계용 두 줄.
func approved(c *daemon.Client, h *daemon.RunHandle, user, checker string, ev daemon.Event) int {
	hint := ev.TTLSec
	if hint <= 0 {
		hint = h.TTLSec
	}
	ttl := effectiveTTL(c, hint)
	by := ev.By
	if by == "" {
		by = checker
	}
	fmt.Fprintf(os.Stderr, "%s 작업 %s — %s 가 승인했습니다 (%s)\n", errP.Green("[승인]"), h.ID, by, stamp())
	fmt.Fprintln(os.Stderr)
	if jobs, err := c.List(user); err == nil {
		tui.WriteWorkerList(os.Stderr, errP, user, jobs, ttl, time.Now(), 10)
	} else {
		errorf("내 작업 리스트 조회 실패: %v", err)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, errP.Bold(tui.PolicyText(ttl)))
	exp := time.Now().Add(ttl)
	if ev.ExpiresAt != nil {
		exp = *ev.ExpiresAt
	}
	fmt.Fprintf(os.Stderr, "  승인 유효시간: %s — %s 까지 아래 --start 를 호출해야 합니다\n", daemon.FormatTTLDuration(ttl), exp.Local().Format("15:04:05"))
	fmt.Fprintf(os.Stderr, "  본 작업 직전에: cross_check --start --job %s  (token 은 %s 환경변수로 전달)\n", h.ID, envToken)
	fmt.Fprintf(os.Stderr, "  본 작업이 끝나면: cross_check --finish --job %s --result done  (멈추면 --result aborted)\n", h.ID)
	fmt.Printf("CROSS_CHECK_JOB=%s\nCROSS_CHECK_TOKEN=%s\n", h.ID, h.Token)
	return exitOK
}

// ---- --start ----

func cmdStart(job, token string) int {
	if job == "" {
		usage()
		errorf("--start 에는 --job <ID> 가 필요")
		return exitUsage
	}
	if token == "" {
		token = os.Getenv(envToken)
	}
	if token == "" {
		errorf("token 이 없습니다: --token 또는 환경변수 %s (cross_check --run 이 stdout 으로 준 값)", envToken)
		return exitUsage
	}
	c, ok := dial()
	if !ok {
		return exitError
	}
	defer c.Close()
	if err := c.Start(job, token); err != nil {
		var de *daemon.DaemonError
		if errors.As(err, &de) {
			fmt.Fprintln(os.Stderr, errP.Red(fmt.Sprintf("[거부] 작업 %s 시작 불가 — %s", job, de.Msg)))
			if daemon.IsExpiredReason(de.Msg) {
				fmt.Fprintln(os.Stderr, "  "+tui.PolicyText(effectiveTTL(c, 0)))
			}
			return exitFail
		}
		errorf("%v", err)
		return exitError
	}
	fmt.Fprintf(os.Stderr, "%s 작업 %s — 본 작업 시작이 기록되었습니다(승인/작업중) (%s)\n", errP.Green("[시작]"), job, stamp())
	return exitOK
}

// ---- --finish ----

// cmdFinish 는 본 작업 결과를 기록한다: done(작업중 → 승인/작업완료) / aborted(시작 전·작업중 → 승인/작업중단).
func cmdFinish(job, token, result string) int {
	if job == "" {
		usage()
		errorf("--finish 에는 --job <ID> 가 필요")
		return exitUsage
	}
	if result != daemon.ResultDone && result != daemon.ResultAborted {
		usage()
		errorf("--finish 에는 --result done 또는 --result aborted 가 필요")
		return exitUsage
	}
	if token == "" {
		token = os.Getenv(envToken)
	}
	if token == "" {
		errorf("token 이 없습니다: --token 또는 환경변수 %s (cross_check --run 이 stdout 으로 준 값)", envToken)
		return exitUsage
	}
	c, ok := dial()
	if !ok {
		return exitError
	}
	defer c.Close()
	if err := c.Finish(job, token, result); err != nil {
		var de *daemon.DaemonError
		if errors.As(err, &de) {
			fmt.Fprintln(os.Stderr, errP.Red(fmt.Sprintf("[거부] 작업 %s 결과 기록 불가 — %s", job, de.Msg)))
			return exitFail
		}
		errorf("%v", err)
		return exitError
	}
	if result == daemon.ResultDone {
		fmt.Fprintf(os.Stderr, "%s 작업 %s — 승인/작업완료 로 기록되었습니다 (%s)\n", errP.Green("[완료]"), job, stamp())
	} else {
		fmt.Fprintf(os.Stderr, "%s 작업 %s — 승인/작업중단 으로 기록되었습니다 (%s)\n", errP.Yellow("[작업중단]"), job, stamp())
	}
	return exitOK
}

// ---- --daemon ----

func cmdDaemon() int {
	conf, err := daemon.LoadConf(confPath())
	if err != nil {
		errorf("%v", err)
		return exitError
	}
	// 로그: 로그 파일을 쓸 수 있으면 거기에, 터미널에서 직접 띄웠으면 화면에도.
	var outs []io.Writer
	if f, err := os.OpenFile(logPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o640); err == nil {
		defer f.Close()
		outs = append(outs, f)
	}
	if len(outs) == 0 || tui.IsTerminal(os.Stderr) {
		outs = append(outs, os.Stderr)
	}
	logger := log.New(io.MultiWriter(outs...), "cross_check ", log.LstdFlags)

	release, err := daemon.AcquireLock(lockPath())
	if err != nil {
		if errors.Is(err, daemon.ErrLocked) {
			fmt.Fprintln(os.Stderr, "cross_check 데몬이 이미 실행 중입니다.")
			return exitOK
		}
		errorf("%v", err)
		return exitError
	}
	defer release()

	cfg := daemon.Config{
		SocketPath: daemon.SocketFromEnv(),
		StorePath:  statePath(),
		AuthPath:   authPath(),
		Logf:       logger.Printf,
	}
	conf.Apply(&cfg)
	srv, err := daemon.NewServer(cfg)
	if err != nil {
		logger.Printf("기동 실패: %v", err)
		errorf("%v", err)
		return exitError
	}
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// SIGHUP: 종료하지 않고 cross_check.conf 의 approval_ttl_sec 만 다시 읽는다(잘못된 값이면 기존 값 유지 + 로그).
	// 터미널이 닫혀 SIGHUP 이 와도 데몬은 죽지 않고 설정만 다시 읽는다.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				srv.ReloadApprovalTTL(confPath())
			}
		}
	}()
	if err := srv.Serve(ctx); err != nil {
		logger.Printf("데몬 오류: %v", err)
		return exitError
	}
	return exitOK
}

// ---- --ensure ----

// ping 은 데몬이 응답하는지 확인한다.
func ping() bool {
	c, err := daemon.Dial(daemon.SocketFromEnv())
	if err != nil {
		return false
	}
	defer c.Close()
	c.Timeout = 2 * time.Second
	return c.Ping() == nil
}

// cmdEnsure 는 데몬이 응답하면 조용히 0, 아니면 "<자신> --daemon" 을 세션에서 떼어 띄우고 최대 3초 기다린다.
// 여러 곳에서 동시에 불려도 데몬 잠금(flock) 때문에 하나만 남고 나머지는 "이미 실행 중" 으로 끝난다.
func cmdEnsure() int {
	if ping() {
		return exitOK
	}
	exe, err := os.Executable()
	if err != nil {
		errorf("실행 파일 경로를 알 수 없음: %v", err)
		return exitFail
	}
	out, err := os.OpenFile(logPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o640)
	if err != nil {
		out, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			errorf("%v", err)
			return exitFail
		}
	}
	defer out.Close()
	cmd := exec.Command(exe, "--daemon")
	cmd.Stdin = nil
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = "/"
	detach(cmd)
	if err := cmd.Start(); err != nil {
		errorf("데몬 기동 실패: %v", err)
		return exitFail
	}
	// 기다리지 않고 놓아 준다(이 프로세스가 끝나면 init 이 거둔다). 잠금을 못 잡아 바로 끝난 경우에도
	// 다른 곳에서 막 띄운 데몬이 곧 응답하므로 남은 시간 동안 계속 확인한다.
	cmd.Process.Release()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ping() {
			return exitOK
		}
		time.Sleep(100 * time.Millisecond)
	}
	errorf("데몬이 3초 안에 응답하지 않습니다 — 로그 확인: %s", logPath())
	return exitFail
}
