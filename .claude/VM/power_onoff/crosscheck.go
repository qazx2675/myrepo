package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// 교차검증(cross_check): conf 의 cross_check=on(기본)일 때 안전장치 단계에 끼어든다.
//   2/5 확인자(다른 user) 번호 선택(-c 면 미리 선택) → 3/5 VM 이름 입력 대신 cross_check --run 으로 확인자 승인 대기
//   → 4/5 YeS → 5/5 카운트다운 → 곧바로 --start 로 승인 유효시간(cross_check.conf 의 approval_ttl_sec, 기본 2분) 안에 시작했음을 데몬에 알린다.
//   → 본 작업(Runner)이 끝나면 --finish 로 결과를 알린다:
//      done    = Runner 가 취소 없이 끝까지 실행됨(VM 별 성공·실패는 보지 않는다) → 승인/작업완료
//      aborted = 승인 뒤 4/5 에서 YeS 가 아님·Ctrl+C, 카운트다운 중 Ctrl+C, 본 작업 중 Ctrl+C → 승인/작업중단
//      (--start 가 거부/만료되면 데몬이 이미 중단으로 기록했으므로 보내지 않는다. kill -9 등으로 죽으면 보낼 수 없고
//       데몬의 work_timeout_sec 가 처리한다)
// 토큰은 cross_check 와 주고받는 용도로만 쓰고(관문 안에만 보관, --finish 뒤 지움) 화면·감사 로그에는 절대 남기지 않는다.

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

// --finish 의 result 값과 그 호출 제한 시간(Ctrl+C 로 취소됐을 수 있는 ctx 대신 새 ctx 로 부른다).
const (
	ccResultDone    = "done"
	ccResultAborted = "aborted"
	ccFinishTimeout = 15 * time.Second
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
	// Finish 는 본 작업 결과(result = done | aborted)를 알린다.
	Finish(ctx context.Context, jobID, token, result string) error
}

// newCrossChecker 는 실제 실행에서 쓰는 구현을 만든다(시험에서 교체 가능).
// --run 의 --caller 에는 이 프로그램의 실제 실행 파일 이름(보통 vm_power_onoff)을 넘긴다.
var newCrossChecker = func(bin string) CrossChecker { return &execCrossChecker{Bin: bin, Caller: selfName()} }

// selfName 은 이 프로그램의 실행 파일 이름(os.Executable 의 마지막 요소, 안 되면 os.Args[0] 의 것)이다.
func selfName() string {
	if p, err := os.Executable(); err == nil && p != "" {
		return filepath.Base(p)
	}
	if len(os.Args) > 0 && os.Args[0] != "" {
		return filepath.Base(os.Args[0])
	}
	return ""
}

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

// ccInstallHint 는 cross_check 를 쓸 수 없을 때 붙이는 해결 방법이다.
const ccInstallHint = " — bash setup.sh 로 설치하거나 pwonoff.conf 에 cross_check=off 를 지정하세요"

// CheckCrossCheckBin 은 cross_check 실행 파일이 쓸 수 있는 상태인지 미리 확인한다(vCenter 접속·안전장치에 들어가기 전에 실패시키려는 것).
// cross_check 는 기본 on 이므로 설치되지 않은 서버에서는 여기서 바로 멈추고 끄는 방법을 알려 준다.
func CheckCrossCheckBin(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cross_check 가 설치되어 있지 않습니다: %s%s", path, ccInstallHint)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("cross_check 가 설치되어 있지 않습니다: %s (일반 파일이 아님)%s", path, ccInstallHint)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("cross_check 가 설치되어 있지 않습니다: %s (실행 권한 없음)%s", path, ccInstallHint)
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

// checkerCandidates 는 확인자로 고를 수 있는 user 다: conf users 순서 그대로, 작업자 본인 제외.
func checkerCandidates(cfg *Config, user string) ([]string, error) {
	var cands []string
	for _, u := range cfg.Users {
		if u != user {
			cands = append(cands, u)
		}
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("확인자로 선택할 user 가 없습니다 (pwonoff.conf 의 users 에 작업자 %s 외의 user 가 필요합니다. 교차검증을 쓰지 않으려면 cross_check=off)", user)
	}
	return cands, nil
}

// checkerTries 는 2단계 확인자 번호 입력 기회(틀리면 다시 묻고, 모두 틀리면 취소).
const checkerTries = 3

// 안전장치 2·3·5단계에 끼우는 교차검증 관문 ---------------------------------------

// CrossCheckGate 는 cross_check=on 일 때 안전장치에 끼우는 교차검증 관문이다.
// Hook 으로 GuardOpts 에 SelectChecker(2단계)·Approve(3단계)·Start(5단계 카운트다운 직후)를 건다.
type CrossCheckGate struct {
	Cfg        *Config
	CC         CrossChecker
	Out        io.Writer // 화면
	ErrOut     io.Writer // cross_check 의 stderr (보통 os.Stderr)
	Audit      *Audit
	User       string   // 작업자
	OptChecker string   // -c 값 (비우면 번호 메뉴)
	Targets    []string // 승인 받을 VM 이름 = 실제 전원 작업 대상
	// InputTimeout 은 2단계 확인자 번호 입력 한 번의 제한(input_timeout_sec). 0 이면 제한 없음
	InputTimeout time.Duration

	// Approve 가 채우고 Start·Finish 가 쓴다. token 은 화면·로그에 내지 않고 --finish 를 보냈거나(성공·실패 모두)
	// 보낼 필요가 없어지면(--start 거부) 지운다. started 는 --start 가 통과했는지(두 번 시작 방지).
	checker, job, token string
	started             bool
	// deadline 은 승인 안내(cross_check --run 의 "승인 유효시간: … — HH:MM:SS 까지")에서 읽은 마감 시각(모르면 0).
	deadline time.Time
}

// ccConfirmMargin 은 4/5 입력을 승인 마감보다 이만큼 먼저 끊는 여유다. 안내의 마감 시각은 초 단위로 잘려 실제보다
// 최대 1초 이르고, 여기에 1초를 더 빼서 데몬이 승인을 만료시키기 전에 --finish aborted(승인/작업중단)를 보낼 수 있게 한다.
const ccConfirmMargin = time.Second

// Hook 은 RunGuard 가 교차검증 방식으로 돌도록 opts 에 관문 단계를 건다.
func (g *CrossCheckGate) Hook(opts *GuardOpts) {
	opts.Checker = g.SelectChecker
	opts.CrossCheck = g.Approve
	opts.AfterCountdown = g.Start
	opts.ConfirmDeadline = g.ConfirmDeadline
}

// ConfirmDeadline 은 4/5 YeS 입력의 마감(승인 마감 - ccConfirmMargin)이다. 마감 시각을 모르면 0(→ input_timeout_sec).
func (g *CrossCheckGate) ConfirmDeadline() time.Time {
	if g.deadline.IsZero() {
		return time.Time{}
	}
	return g.deadline.Add(-ccConfirmMargin)
}

// SelectChecker 는 2단계: -c 가 있으면 그 확인자(미리 선택), 없으면 본인을 뺀 users 번호 메뉴.
// 잘못된 번호는 checkerTries 번까지 다시 묻는다. 오류 문구는 그대로 취소 사유가 된다(Ctrl+C 는 ctx 로 RunGuard 가 판단).
func (g *CrossCheckGate) SelectChecker(ctx context.Context, in *LineReader, out io.Writer) (string, error) {
	fail := func(why string, err error) (string, error) {
		g.Audit.Printf("교차검증 확인자 선택 실패: %s", why)
		return "", err
	}
	if g.OptChecker != "" {
		if err := ValidateChecker(g.Cfg, g.User, g.OptChecker); err != nil {
			fmt.Fprintln(out, Red("[오류] "+err.Error()))
			return fail(err.Error(), errors.New("확인자 선택 실패 (-c 값이 올바르지 않음)"))
		}
		fmt.Fprintf(out, "%s 확인자: %s (-c)\n", Bold("[2/5]"), g.OptChecker)
		g.Audit.Printf("교차검증 확인자 선택 확인자=%s 방법=-c", g.OptChecker)
		return g.OptChecker, nil
	}
	cands, err := checkerCandidates(g.Cfg, g.User)
	if err != nil {
		fmt.Fprintln(out, Red("[오류] "+err.Error()))
		return fail(err.Error(), errors.New("확인자 선택 실패 (선택할 user 없음)"))
	}
	fmt.Fprintf(out, "%s 확인자를 선택하세요 (교차검증 승인자, 작업자 %s 제외):\n", Bold("[2/5]"), g.User)
	for i, u := range cands {
		fmt.Fprintf(out, "  %d) %s\n", i+1, u)
	}
	for try := 1; ; try++ {
		fmt.Fprint(out, "번호 입력: ")
		s, err := in.ReadLineLimit(ctx, g.InputTimeout)
		if err != nil {
			if ctx.Err() != nil {
				g.Audit.Printf("교차검증 확인자 선택 중 Ctrl+C")
				return "", ctx.Err()
			}
			fmt.Fprintln(out)
			if errors.Is(err, ErrTimeout) {
				why := inputTimeoutReason(g.InputTimeout)
				return fail(why, errors.New(why))
			}
			return fail("입력 종료(EOF)", errors.New("입력 종료(EOF)"))
		}
		s = strings.TrimSpace(s)
		if n, e := strconv.Atoi(s); e == nil && n >= 1 && n <= len(cands) {
			fmt.Fprintf(out, "확인자: %s\n", cands[n-1])
			g.Audit.Printf("교차검증 확인자 선택 확인자=%s 방법=번호(%d번째 입력)", cands[n-1], try)
			return cands[n-1], nil
		}
		if try >= checkerTries {
			return fail(fmt.Sprintf("잘못된 입력 %d회", checkerTries), fmt.Errorf("확인자 선택 실패 (잘못된 입력 %d회)", checkerTries))
		}
		fmt.Fprintln(out, Yellow(fmt.Sprintf("[경고] 잘못된 선택입니다: %q (1~%d 중 번호) — 다시 입력하세요 (남은 기회 %d번)", s, len(cands), checkerTries-try)))
	}
}

// Approve 는 3단계: 확인자에게 승인을 요청하고 승인될 때까지 기다린다(VM 이름 입력 대신). 승인되면 nil.
// 미승인이면 감사 기록까지 남긴 오류를 돌려주고, 화면의 "[취소] 교차검증 미승인" 은 RunGuard 가 출력한다.
func (g *CrossCheckGate) Approve(ctx context.Context, checker string) error {
	g.checker, g.job, g.token, g.started, g.deadline = checker, "", "", false, time.Time{}
	// 대기 전에 승인 유효시간 정책부터 알린다
	fmt.Fprintln(g.Out, Yellow(crossCheckNotice))
	fmt.Fprintf(g.Out, "[INFO] 확인자 %s 에게 교차검증을 요청합니다 (대상 %d대). 승인을 기다리는 중... (Ctrl+C 로 취소)\n", checker, len(g.Targets))
	g.Audit.Printf("교차검증 요청 작업자=%s 확인자=%s 대상=%d대", g.User, checker, len(g.Targets))
	// cross_check 의 진행 출력(stderr)은 그대로 화면에 흘리면서 승인 안내의 마감 시각 줄만 읽어 둔다
	sn := &ccDeadlineSniffer{w: g.ErrOut}
	jobID, token, err := g.CC.Run(ctx, g.User, checker, g.Targets, g.Out, sn)
	if err == nil && (jobID == "" || token == "" || !ccJobRe.MatchString(jobID)) {
		err = &CrossCheckError{Code: ccExitError, Msg: "승인 응답에 작업 ID/토큰이 없거나 올바르지 않습니다"}
	}
	if err != nil {
		class := crossCheckClass(err)
		if ctx.Err() != nil {
			class = (&CrossCheckError{Code: ccExitInterrupt}).Class()
		}
		g.Audit.Printf("교차검증 미승인 확인자=%s 분류=%s", checker, class)
		return fmt.Errorf("교차검증 미승인: %s", class)
	}
	g.job, g.token = jobID, token
	g.Audit.Printf("교차검증 승인 확인자=%s job=%s", checker, jobID)
	fmt.Fprintln(g.Out, Green(fmt.Sprintf("[INFO] 교차검증 승인됨 (확인자 %s, 작업 ID %s)", checker, jobID)))
	fmt.Fprintln(g.Out, Yellow("[INFO] 승인 유효시간 안에 4/5 YeS 입력과 5/5 카운트다운을 마쳐야 전원 작업이 시작됩니다 (cross_check --start 는 카운트다운 직후 자동 호출, 넘기면 승인 자동 취소)"))
	if dl, ok := sn.deadline(time.Now()); ok {
		g.deadline = dl
		g.Audit.Printf("교차검증 승인 마감 %s (4/5 입력은 그 전까지)", dl.Format("15:04:05"))
		fmt.Fprintln(g.Out, Yellow(fmt.Sprintf("[INFO] 4/5 입력 마감: %s — 그때까지 YeS 를 입력하지 않으면 자동 취소(승인/작업중단)", dl.Format("15:04:05"))))
	} else {
		g.Audit.Printf("교차검증 승인 마감 시각을 알 수 없음 — 4/5 입력 제한은 input_timeout_sec(%s)", durText(g.InputTimeout))
	}
	return nil
}

// ccDeadlineRe 는 cross_check --run 이 승인 직후 stderr 에 내는 마감 안내 줄이다.
//
//	"  승인 유효시간: 2분 — 15:04:05 까지 아래 --start 를 호출해야 합니다"
var (
	ccDeadlineRe = regexp.MustCompile(`승인 유효시간: .*?(\d{1,2}):(\d{2}):(\d{2}) 까지`)
	ansiRe       = regexp.MustCompile("\x1b\\[[0-9;]*m")
)

// ccDeadlineSniffer 는 받은 출력을 그대로 w 로 넘기면서 줄 단위로 마감 안내를 찾아 마지막 것을 기억한다.
type ccDeadlineSniffer struct {
	w       io.Writer
	partial []byte
	h, m, s int
	found   bool
}

func (c *ccDeadlineSniffer) Write(p []byte) (int, error) {
	if c.w != nil {
		c.w.Write(p)
	}
	c.partial = append(c.partial, p...)
	for {
		i := bytes.IndexByte(c.partial, '\n')
		if i < 0 {
			break
		}
		c.scan(c.partial[:i])
		c.partial = c.partial[i+1:]
	}
	if len(c.partial) > 4096 { // 줄바꿈 없는 긴 출력은 버린다
		c.partial = c.partial[:0]
	}
	return len(p), nil
}

func (c *ccDeadlineSniffer) scan(line []byte) {
	m := ccDeadlineRe.FindSubmatch(ansiRe.ReplaceAll(line, nil))
	if m == nil {
		return
	}
	h, _ := strconv.Atoi(string(m[1]))
	mi, _ := strconv.Atoi(string(m[2]))
	s, _ := strconv.Atoi(string(m[3]))
	if h > 23 || mi > 59 || s > 59 {
		return
	}
	c.h, c.m, c.s, c.found = h, mi, s, true
}

// deadline 은 찾은 시:분:초를 now 기준의 가장 가까운 시각으로 바꾼다(자정 넘김 처리).
// 승인 유효시간 허용 범위(최대 1시간)를 크게 벗어나면 믿지 않는다(ok=false → input_timeout_sec 사용).
func (c *ccDeadlineSniffer) deadline(now time.Time) (time.Time, bool) {
	if len(c.partial) > 0 { // 줄바꿈 없이 끝난 마지막 줄
		c.scan(c.partial)
		c.partial = c.partial[:0]
	}
	if !c.found {
		return time.Time{}, false
	}
	t := time.Date(now.Year(), now.Month(), now.Day(), c.h, c.m, c.s, 0, now.Location())
	switch {
	case t.Sub(now) > 12*time.Hour:
		t = t.Add(-24 * time.Hour)
	case now.Sub(t) > 12*time.Hour:
		t = t.Add(24 * time.Hour)
	}
	if d := t.Sub(now); d > time.Hour+time.Minute || d < -time.Minute {
		return time.Time{}, false
	}
	return t, true
}

// Start 는 5단계 카운트다운 직후: 승인 유효시간 창이 아직 열려 있는지 cross_check --start 로 확인하며 시작을 알린다.
// 통과하면 nil. 거부(유효시간 초과 등)면 감사 기록·사유 한 줄 출력 후 오류(화면의 "[취소] …" 는 RunGuard 가 출력).
// 거부되면 데몬이 이미 중단으로 기록했으므로 token 을 지워 --finish 를 보내지 않는다. 단 --start 도중 Ctrl+C 면
// 시작이 기록됐는지 알 수 없으므로 token 을 남겨 두어 뒤이은 Finish(aborted)가 작업중단을 알리게 한다.
func (g *CrossCheckGate) Start(ctx context.Context) error {
	job, token := g.job, g.token
	if job == "" || token == "" || g.started { // 시작은 한 번만
		g.Audit.Printf("교차검증 시작 거부: 승인된 작업이 없음")
		return &CrossCheckError{Code: ccExitError, Msg: "승인된 작업이 없습니다"}
	}
	if err := g.CC.Start(ctx, job, token); err != nil {
		class := crossCheckClass(err)
		if ctx.Err() != nil {
			class = (&CrossCheckError{Code: ccExitInterrupt}).Class()
		} else {
			g.token = ""
		}
		g.Audit.Printf("교차검증 시작 거부 확인자=%s job=%s 분류=%s", g.checker, job, class)
		var ce *CrossCheckError
		if errors.As(err, &ce) && ce.Msg != "" && ctx.Err() == nil {
			fmt.Fprintln(g.Out, Yellow("  사유: "+strings.ReplaceAll(ce.Msg, token, "***")))
		}
		return fmt.Errorf("교차검증 시작 거부: %s", class)
	}
	g.started = true
	g.Audit.Printf("교차검증 시작 허용 확인자=%s job=%s", g.checker, job)
	fmt.Fprintln(g.Out, Green("[INFO] 교차검증 통과 — 전원 작업을 시작합니다"))
	return nil
}

// Abort 는 승인 뒤 본 작업을 시작하지 않고 멈췄을 때(4/5 에서 YeS 가 아님·Ctrl+C, 카운트다운 중 Ctrl+C) 부른다:
// 승인된 작업이 남아 있으면 --finish --result aborted(승인/작업중단). 승인 전·--start 거부 뒤에는 아무것도 하지 않는다.
func (g *CrossCheckGate) Abort() { g.Finish(ccResultAborted) }

// ReportRun 은 본 작업(Runner)이 돌아온 직후 부른다: ctx 가 취소됐으면(Ctrl+C·종료 신호) aborted, 아니면 done.
// VM 별 성공·실패는 보지 않는다(일부 VM 이 실패해도 끝까지 실행됐으면 작업완료).
func (g *CrossCheckGate) ReportRun(ctx context.Context) {
	if ctx.Err() != nil {
		g.Finish(ccResultAborted)
		return
	}
	g.Finish(ccResultDone)
}

// ccResultLabel 은 result 를 데몬의 상태 표기로 바꾼다.
func ccResultLabel(result string) string {
	if result == ccResultDone {
		return "승인/작업완료"
	}
	return "승인/작업중단"
}

// Finish 는 cross_check --finish 로 본 작업 결과(done|aborted)를 데몬에 알린다. 승인된 작업이 없거나 이미 보고했으면 nil.
// 이미 취소됐을 수 있는 ctx 대신 짧은 제한 시간의 새 ctx 로 부른다. 실패해도 vm_power_onoff 의 종료 코드는 바꾸지 않고
// [경고] 한 줄과 감사 로그만 남긴다(반환값은 시험·호출자 참고용). 성공·실패와 관계없이 token 은 지운다.
func (g *CrossCheckGate) Finish(result string) error {
	job, token := g.job, g.token
	g.token = ""
	if job == "" || token == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), ccFinishTimeout)
	defer cancel()
	if err := g.CC.Finish(ctx, job, token, result); err != nil {
		class := crossCheckClass(err)
		why := ""
		var ce *CrossCheckError
		if errors.As(err, &ce) && ce.Msg != "" {
			why = " — " + strings.ReplaceAll(ce.Msg, token, "***")
		}
		g.Audit.Printf("교차검증 결과 보고 실패: %s job=%s 분류=%s", result, job, class)
		fmt.Fprintln(g.Out, Yellow(fmt.Sprintf("[경고] 교차검증 결과(%s)를 cross_check 데몬에 알리지 못했습니다 (작업 %s, %s)%s", result, job, class, why)))
		return err
	}
	g.Audit.Printf("교차검증 결과 보고: %s job=%s", result, job)
	fmt.Fprintf(g.Out, "[INFO] 교차검증 결과 보고: %s (작업 %s)\n", ccResultLabel(result), job)
	return nil
}

// exec 구현 ---------------------------------------------------------------

// execCrossChecker 는 cross_check 실행 파일을 직접 띄운다(셸을 거치지 않는다).
type execCrossChecker struct {
	Bin    string
	Caller string   // --run --caller 값(이 프로그램의 실행 파일 이름). 비우면 넘기지 않는다
	pre    []string // 시험용: 실행 파일 뒤 맨 앞에 끼워 넣는 인자
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
	if errors.Is(ctx.Err(), context.DeadlineExceeded) { // --finish 의 제한 시간(ccFinishTimeout)
		return &CrossCheckError{Code: ccExitError, Msg: what + " 제한 시간 초과"}
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
	args := []string{"--run", "--user", user, "--checker", checker}
	if c.Caller != "" {
		args = append(args, "--caller", c.Caller)
	}
	cmd := c.command(ctx, args...)
	cmd.Stdin = strings.NewReader(strings.Join(targets, "\n") + "\n")
	f := &ccFilter{out: out}
	cmd.Stdout = f
	cmd.Stderr = errOut
	// stderr 를 마감 시각을 읽으려고 파이프로 받으면 cross_check 가 색을 끄므로, 원래 터미널이었다면 색을 그대로 켜 둔다
	if sn, ok := errOut.(*ccDeadlineSniffer); ok {
		if tf, ok := sn.w.(*os.File); ok && isTerminal(tf) && os.Getenv("CROSS_CHECK_COLOR") == "" {
			if _, no := os.LookupEnv("NO_COLOR"); !no {
				cmd.Env = append(os.Environ(), "CROSS_CHECK_COLOR=always")
			}
		}
	}
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
	return c.runWithToken(ctx, "--start", token, "--start", "--job", jobID)
}

func (c *execCrossChecker) Finish(ctx context.Context, jobID, token, result string) error {
	if !ccJobRe.MatchString(jobID) {
		return &CrossCheckError{Code: ccExitError, Msg: "작업 ID 형식이 올바르지 않습니다"}
	}
	if result != ccResultDone && result != ccResultAborted {
		return &CrossCheckError{Code: ccExitError, Msg: "결과 값이 올바르지 않습니다: " + result}
	}
	return c.runWithToken(ctx, "--finish", token, "--finish", "--job", jobID, "--result", result)
}

// runWithToken 은 token 을 자식 프로세스의 환경(CROSS_CHECK_TOKEN)에만 넣어 cross_check 를 실행한다(부모 환경·인자·화면에는 없음).
// 출력은 화면으로 보내지 않고, 실패하면 그 첫 줄(token 은 가림)을 사유로 붙인다.
func (c *execCrossChecker) runWithToken(ctx context.Context, what, token string, args ...string) error {
	cmd := c.command(ctx, args...)
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
		ce := ccWaitError(ctx, err, what).(*CrossCheckError)
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
