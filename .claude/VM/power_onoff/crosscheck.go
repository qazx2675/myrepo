package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// 교차검증(cross_check): conf 의 cross_check=on 일 때 안전장치 1~5단계 통과 직후, 실제 전원 작업 직전에
// 확인자(다른 user)의 승인을 받는다. 승인 → 곧바로 --start 로 승인 유효시간(cross_check.conf 의 approval_ttl_sec, 기본 2분) 안에 시작했음을 데몬에 알린다.
// 토큰은 cross_check 와 주고받는 용도로만 쓰고 화면·감사 로그에는 절대 남기지 않는다.

// DefaultCrossCheckBin 은 cross_check_bin 의 기본값이다.
const DefaultCrossCheckBin = "/usr/local/bin/cross_check"

// crossCheckNotice 는 승인을 기다리기 전에 반드시 보여 주는 정책 안내다.
const crossCheckNotice = "※ cross_check 정책: 확인자 승인 후 설정된 시간(cross_check.conf 의 approval_ttl_sec, 기본 2분) 내에 본 작업을 진행하지 않으면 승인이 자동 취소됩니다."

// cross_check 와의 약속: --run 은 승인 시 stdout 에 아래 두 줄을 찍고, 토큰은 --start 에 환경변수로만 넘긴다.
const (
	ccJobPrefix   = "CROSS_CHECK_JOB="
	ccTokenPrefix = "CROSS_CHECK_TOKEN="
	ccEnvToken    = "CROSS_CHECK_TOKEN"
	ccMarker      = "CROSS_CHECK_" // 위 두 접두사의 공통 앞부분
)

// cross_check 종료 코드. ccExitSpawn 은 프로세스를 띄우지 못한 경우(코드 아님).
const (
	ccExitRefused   = 1   // 중단/거부 (사유는 cross_check 가 이미 출력)
	ccExitError     = 3   // 데몬·연결·검증 오류
	ccExitInterrupt = 130 // Ctrl+C
	ccExitSpawn     = -2
)

// jobID 는 인자로 넘기므로 '-' 로 시작하거나 특수문자가 있으면 거부한다.
var ccJobRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// CrossChecker 는 cross_check CLI 와의 접점이다. 시험에서는 가짜로 바꾼다.
type CrossChecker interface {
	// Run 은 targets(VM 이름)를 확인자에게 올리고 승인될 때까지 기다린다. 사람이 볼 진행 출력은 out/errOut 으로 그대로 흘린다.
	// 승인되면 작업 ID 와 일회용 토큰을 돌려준다.
	Run(ctx context.Context, user, checker string, targets []string, out, errOut io.Writer) (jobID, token string, err error)
	// Start 는 승인 유효시간 창이 아직 열려 있는지 확인하고 시작을 알린다. nil 이면 시작 가능.
	Start(ctx context.Context, jobID, token string) error
}

// newCrossChecker 는 실제 실행에서 쓰는 구현을 만든다(시험에서 교체 가능).
var newCrossChecker = func(bin string) CrossChecker { return &execCrossChecker{Bin: bin} }

// CrossCheckError 는 cross_check 가 승인하지 않았거나 실패한 경우다. Msg 에는 토큰을 담지 않는다.
type CrossCheckError struct {
	Code int
	Msg  string
}

func (e *CrossCheckError) Error() string { return "cross_check " + e.Class() + ": " + e.Msg }

// Class 는 감사 로그에 남기는 사유 분류다.
func (e *CrossCheckError) Class() string {
	switch e.Code {
	case ccExitRefused:
		return "거부/중단(exit 1)"
	case ccExitError:
		return "데몬/연결/검증 오류(exit 3)"
	case ccExitInterrupt:
		return "Ctrl+C(exit 130)"
	case ccExitSpawn:
		return "실행 실패"
	case -1:
		return "신호로 종료"
	}
	return "기타 종료(exit " + strconv.Itoa(e.Code) + ")"
}

// crossCheckClass 는 어떤 오류든 감사 로그용 분류 문구로 바꾼다.
func crossCheckClass(err error) string {
	var ce *CrossCheckError
	if errors.As(err, &ce) {
		return ce.Class()
	}
	return "알 수 없는 오류"
}

// crossCheckNeeded 는 이번 실행에서 교차검증을 거쳐야 하는지다: cross_check=on 이고 실제 전원 작업일 때만.
// 계획만 보는 -n 에서는 부르지 않는다(--demo 는 run() 에 오지 않는다).
func crossCheckNeeded(cfg *Config, planOnly bool) bool { return cfg.CrossCheck && !planOnly }

// CheckCrossCheckBin 은 cross_check 실행 파일이 쓸 수 있는 상태인지 미리 확인한다(안전장치에 들어가기 전에 실패시키려는 것).
func CheckCrossCheckBin(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cross_check 실행 파일을 찾을 수 없습니다 (cross_check_bin=%s): %v", path, err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("cross_check 실행 파일이 일반 파일이 아닙니다: %s", path)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("cross_check 실행 파일에 실행 권한이 없습니다: %s", path)
	}
	return nil
}

// 확인자 선택 -------------------------------------------------------------

// ValidateChecker 는 확인자가 users 에 있고 작업자 본인이 아닌지 확인한다.
func ValidateChecker(cfg *Config, user, checker string) error {
	if err := ValidateUser(cfg, checker); err != nil {
		return fmt.Errorf("확인자가 허용되지 않은 user 입니다: %q (허용: %s)", checker, strings.Join(cfg.Users, ", "))
	}
	if checker == user {
		return fmt.Errorf("확인자는 작업자 본인(%s)이 될 수 없습니다", user)
	}
	return nil
}

// pickChecker 는 -c 값을 검증하거나, 없으면 본인을 뺀 users 번호 메뉴로 고른다.
func pickChecker(ctx context.Context, in *LineReader, out io.Writer, cfg *Config, user, opt string) (string, error) {
	if opt != "" {
		if err := ValidateChecker(cfg, user, opt); err != nil {
			return "", err
		}
		return opt, nil
	}
	var cands []string
	for _, u := range cfg.Users {
		if u != user {
			cands = append(cands, u)
		}
	}
	if len(cands) == 0 {
		return "", errors.New("확인자로 선택할 user 가 없습니다 (conf users 에 본인 외 user 가 필요합니다)")
	}
	fmt.Fprintln(out, "\n교차검증 확인자를 선택하세요:")
	for i, u := range cands {
		fmt.Fprintf(out, "  %d) %s\n", i+1, u)
	}
	s, err := readChoice(ctx, in, "번호 입력: ")
	if err != nil {
		return "", err
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > len(cands) {
		return "", fmt.Errorf("잘못된 선택입니다: %q (1~%d)", s, len(cands))
	}
	return cands[n-1], nil
}

// 안전장치 직후의 교차검증 단계 ------------------------------------------------

// CrossCheckGate 는 전원 작업 직전의 마지막 관문이다.
type CrossCheckGate struct {
	Cfg    *Config
	CC     CrossChecker
	In     *LineReader
	Out    io.Writer // 화면
	ErrOut io.Writer // cross_check 의 stderr (보통 os.Stderr)
	Audit  *Audit
}

// Run 은 확인자를 정하고 승인을 받아 시작 신호까지 보낸다. 통과하면 nil.
// 오류면 화면 출력·감사 기록까지 끝낸 상태이므로 호출자는 전원 작업 없이 ExitFail 로 끝내면 된다.
func (g *CrossCheckGate) Run(ctx context.Context, user, optChecker string, targets []string) error {
	cancel := func(class string) error {
		if ctx.Err() != nil {
			fmt.Fprintln(g.Out) // 승인 대기 줄을 끝내고 출력
		}
		fmt.Fprintln(g.Out, Red("[취소] 교차검증 미승인"))
		return fmt.Errorf("교차검증 미승인: %s", class)
	}

	// (a) 확인자
	checker, err := pickChecker(ctx, g.In, g.Out, g.Cfg, user, optChecker)
	if err != nil {
		g.Audit.Printf("교차검증 확인자 선택 실패: %v", err)
		fmt.Fprintln(g.Out, Red("[오류] "+err.Error()))
		return cancel("확인자 선택 실패")
	}

	// (b) 승인 요청 — 대기 전에 승인 유효시간 정책부터 알린다
	fmt.Fprintln(g.Out)
	fmt.Fprintln(g.Out, Yellow(crossCheckNotice))
	fmt.Fprintf(g.Out, "[INFO] 확인자 %s 에게 교차검증을 요청합니다 (대상 %d대). 승인을 기다리는 중... (Ctrl+C 로 취소)\n", checker, len(targets))
	g.Audit.Printf("교차검증 요청 작업자=%s 확인자=%s 대상=%d대", user, checker, len(targets))
	jobID, token, err := g.CC.Run(ctx, user, checker, targets, g.Out, g.ErrOut)
	if err == nil && (jobID == "" || token == "" || !ccJobRe.MatchString(jobID)) {
		err = &CrossCheckError{Code: ccExitError, Msg: "승인 응답에 작업 ID/토큰이 없거나 올바르지 않습니다"}
	}
	if err != nil {
		class := crossCheckClass(err)
		if ctx.Err() != nil {
			class = (&CrossCheckError{Code: ccExitInterrupt}).Class()
		}
		g.Audit.Printf("교차검증 미승인 확인자=%s 분류=%s", checker, class)
		return cancel(class)
	}
	g.Audit.Printf("교차검증 승인 확인자=%s job=%s", checker, jobID)
	fmt.Fprintln(g.Out, Green(fmt.Sprintf("[INFO] 교차검증 승인됨 (확인자 %s, 작업 ID %s)", checker, jobID)))

	// (c) 승인 후 유효시간 창이 열려 있는지 확인하며 시작을 알린다
	if err := g.CC.Start(ctx, jobID, token); err != nil {
		class := crossCheckClass(err)
		g.Audit.Printf("교차검증 시작 거부 확인자=%s job=%s 분류=%s", checker, jobID, class)
		if ctx.Err() != nil {
			fmt.Fprintln(g.Out)
		}
		fmt.Fprintln(g.Out, Red("[취소] 교차검증 승인 유효시간 초과 또는 거부"))
		return fmt.Errorf("교차검증 시작 거부: %s", class)
	}
	g.Audit.Printf("교차검증 시작 허용 확인자=%s job=%s", checker, jobID)
	fmt.Fprintln(g.Out, Green("[INFO] 교차검증 통과 — 전원 작업을 시작합니다"))
	return nil
}

// exec 구현 ---------------------------------------------------------------

// execCrossChecker 는 cross_check 실행 파일을 직접 띄운다(셸을 거치지 않는다).
type execCrossChecker struct {
	Bin string
	pre []string // 시험용: 실행 파일 뒤 맨 앞에 끼워 넣는 인자
}

func (c *execCrossChecker) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Bin, append(append([]string{}, c.pre...), args...)...)
	// Ctrl+C 때는 먼저 정상 종료(SIGINT)를 요청해 cross_check 가 데몬에 중단을 알리게 하고, 안 끝나면 강제 종료한다.
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

// ccWaitError 는 Wait 결과를 CrossCheckError 로 바꾼다. ctx 가 취소됐으면 Ctrl+C 로 본다.
func ccWaitError(ctx context.Context, err error, what string) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return &CrossCheckError{Code: ccExitInterrupt, Msg: what + " 중 Ctrl+C"}
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return &CrossCheckError{Code: ee.ExitCode(), Msg: what + " 종료 코드 " + strconv.Itoa(ee.ExitCode())}
	}
	return &CrossCheckError{Code: ccExitSpawn, Msg: what + " 실행 실패: " + err.Error()}
}

func (c *execCrossChecker) Run(ctx context.Context, user, checker string, targets []string, out, errOut io.Writer) (string, string, error) {
	cmd := c.command(ctx, "--run", "--user", user, "--checker", checker)
	cmd.Stdin = strings.NewReader(strings.Join(targets, "\n") + "\n")
	f := &ccFilter{out: out}
	cmd.Stdout = f
	cmd.Stderr = errOut
	err := cmd.Run()
	f.flush()
	if err != nil {
		return "", "", ccWaitError(ctx, err, "--run")
	}
	if f.job == "" || f.token == "" {
		return "", "", &CrossCheckError{Code: ccExitError, Msg: "--run 이 승인했지만 작업 ID/토큰 출력이 없습니다"}
	}
	if !ccJobRe.MatchString(f.job) || strings.ContainsAny(f.token, " \t\r\n\x00") {
		return "", "", &CrossCheckError{Code: ccExitError, Msg: "--run 이 돌려준 작업 ID/토큰 형식이 올바르지 않습니다"}
	}
	return f.job, f.token, nil
}

func (c *execCrossChecker) Start(ctx context.Context, jobID, token string) error {
	if !ccJobRe.MatchString(jobID) {
		return &CrossCheckError{Code: ccExitError, Msg: "작업 ID 형식이 올바르지 않습니다"}
	}
	cmd := c.command(ctx, "--start", "--job", jobID)
	// 토큰은 자식 프로세스의 환경에만 둔다(부모 환경·인자·화면에는 없음)
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, ccEnvToken+"=") {
			env = append(env, e)
		}
	}
	cmd.Env = append(env, ccEnvToken+"="+token)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		ce := ccWaitError(ctx, err, "--start").(*CrossCheckError)
		// 사유 한 줄만 덧붙인다(토큰이 섞여 있으면 가린다)
		if msg := strings.TrimSpace(strings.ReplaceAll(buf.String(), token, "***")); msg != "" {
			if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
				msg = msg[:i]
			}
			if len(msg) > 200 {
				msg = msg[:200]
			}
			ce.Msg += ": " + msg
		}
		return ce
	}
	return nil
}

// ccFilter 는 --run 의 stdout 을 그대로 화면으로 보내되 CROSS_CHECK_JOB=/CROSS_CHECK_TOKEN= 두 줄은 삼켜서 값만 보관한다.
// 줄바꿈 없는 프롬프트도 지연 없이 보이도록, 표식이 될 수 없는 줄은 완성되기 전에 바로 내보낸다.
type ccFilter struct {
	out        io.Writer
	buf        []byte // 아직 내보내지 않은 줄의 앞부분
	mid        bool   // 이미 일부를 내보낸 줄의 나머지를 통과시키는 중
	job, token string
}

func (f *ccFilter) Write(p []byte) (int, error) {
	f.buf = append(f.buf, p...)
	for len(f.buf) > 0 {
		i := bytes.IndexByte(f.buf, '\n')
		switch {
		case f.mid && i < 0:
			f.out.Write(f.buf)
			f.buf = f.buf[:0]
		case f.mid:
			f.out.Write(f.buf[:i+1])
			f.buf = f.buf[i+1:]
			f.mid = false
		case i >= 0:
			f.line(f.buf[:i+1])
			f.buf = f.buf[i+1:]
		case maybeMarker(f.buf) && len(f.buf) < 1<<20:
			return len(p), nil // 표식일 수 있으니 줄이 끝날 때까지 보류
		default:
			f.out.Write(f.buf)
			f.buf = f.buf[:0]
			f.mid = true
		}
	}
	return len(p), nil
}

// flush 는 줄바꿈 없이 끝난 마지막 조각을 처리한다.
func (f *ccFilter) flush() {
	if len(f.buf) > 0 && !f.mid {
		f.line(f.buf)
	} else if len(f.buf) > 0 {
		f.out.Write(f.buf)
	}
	f.buf = nil
}

// maybeMarker 는 b 가 CROSS_CHECK_ 로 시작하는 줄이거나 그 앞부분일 수 있는지 본다.
func maybeMarker(b []byte) bool {
	if len(b) <= len(ccMarker) {
		return strings.HasPrefix(ccMarker, string(b))
	}
	return bytes.HasPrefix(b, []byte(ccMarker))
}

func (f *ccFilter) line(l []byte) {
	s := strings.TrimRight(string(l), "\r\n")
	switch {
	case strings.HasPrefix(s, ccJobPrefix):
		f.job = strings.TrimSpace(s[len(ccJobPrefix):])
	case strings.HasPrefix(s, ccTokenPrefix):
		f.token = strings.TrimSpace(s[len(ccTokenPrefix):])
	default:
		f.out.Write(l)
	}
}
