package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// GuardError 는 안전장치 취소. Reason 은 사유만 담는다(화면엔 "[취소] <Reason>").
type GuardError struct{ Reason string }

func (e *GuardError) Error() string { return "[취소] " + e.Reason }

// GuardOpts 는 안전장치 설정. 0/nil 이면 기본값.
type GuardOpts struct {
	InputTimeout time.Duration     // 3단계 전체 입력 제한(기본 60s, 기존 방식에서만)
	StepTimeout  time.Duration     // 1단계·2단계(기존 방식 Enter)·4단계 질문 한 번의 제한(기본 input_timeout_sec 기본값 10분). 넘기면 취소
	Countdown    int               // 5단계 카운트다운 시작 숫자(기본 10)
	Tick         time.Duration     // 카운트다운 간격(기본 1s)
	ShowTargets  func(w io.Writer) // 1단계 통과 후·4단계 ls 때 대상 목록 출력(nil 이면 한 줄에 하나씩). "총 N대" 는 RunGuard 가 출력
	OnList       func()            // 4단계에서 ls 를 입력할 때마다 호출(감사 로그 횟수용, nil 이면 무시)

	// 교차검증 방식(cross_check=on): Checker 와 CrossCheck 를 둘 다 주면
	// 2단계 = 확인자 번호 선택(Checker), 3단계 = VM 이름 입력 대신 확인자 승인 대기(CrossCheck).
	// 둘 중 하나라도 nil 이면 기존 방식(2단계 Enter, 3단계 VM 이름 입력).
	Checker    func(ctx context.Context, in *LineReader, out io.Writer) (string, error) // 오류 문구 = 취소 사유
	CrossCheck func(ctx context.Context, checker string) error                          // 승인 대기. nil 이 아니면 미승인
	// AfterCountdown 은 5단계 카운트다운이 끝난 직후 호출된다(교차검증의 --start). 오류면 취소.
	AfterCountdown func(ctx context.Context) error
	// ConfirmDeadline 은 교차검증 방식 4단계(YeS) 입력의 마감 시각(승인 유효시간 마감)이다.
	// nil 이거나 0 시각을 돌려주면(마감을 모름) StepTimeout 을 쓴다. 마감을 넘기면 "승인 유효시간 초과" 로 취소.
	ConfirmDeadline func() time.Time
}

// 교차검증 방식의 취소 사유
const (
	reasonCCNotApproved = "교차검증 미승인"
	reasonCCStartFailed = "교차검증 승인 유효시간 초과 또는 거부"
	reasonCCExpired     = "승인 유효시간 초과" // 4/5 에서 승인 마감까지 YeS 를 받지 못함
)

func (o GuardOpts) withDefaults() GuardOpts {
	if o.InputTimeout <= 0 {
		o.InputTimeout = 60 * time.Second
	}
	if o.StepTimeout <= 0 {
		o.StepTimeout = DefaultInputTimeout
	}
	if o.Countdown <= 0 {
		o.Countdown = 10
	}
	if o.Tick <= 0 {
		o.Tick = time.Second
	}
	return o
}

// 1/5 단계와 VWP 담당자 확인에서 그대로 입력해야 하는 고정 문구.
// 한글 안내(괄호 안)를 영문 자판으로 친 문자열이며, 비교는 앞뒤 공백만 무시하고 대소문자까지 정확히 같아야 한다.
// 1/5 문구는 전원 작업 종류(Power Off/On/재부팅)와 관계없이 같다.
const (
	WorkPhrase       = "woqnxldwkrdjq" // 재부팅작업
	WorkPhraseHint   = "재부팅작업"
	VWPPhrase        = "VWPekaekdwkghkrdls" // VWP담당자확인
	VWPPhraseHint    = "VWP담당자확인"
	reasonWorkPhrase = "작업 확인 문구 불일치"
	reasonVWPPhrase  = "VWP 담당자 확인 문구 불일치"
)

const reasonInterrupt = "Ctrl+C 로 중단"

// cancelGuard 는 취소 사유를 빨간색으로 출력하고 *GuardError 를 돌려준다.
func cancelGuard(out io.Writer, reason string) error {
	if reason == reasonInterrupt {
		fmt.Fprintln(out) // 프롬프트/카운트다운 줄을 끝내고 출력
	}
	fmt.Fprintln(out, Red("[취소] "+reason))
	return &GuardError{Reason: reason}
}

// secs 는 시간을 "60초" 형태로(초 단위가 아니면 Go 표기).
func secs(d time.Duration) string {
	if d%time.Second == 0 {
		return strconv.Itoa(int(d/time.Second)) + "초"
	}
	return d.String()
}

// isLs 는 4단계의 대상 보기 명령(ls, 앞뒤 공백·대소문자 무시)인지 본다.
func isLs(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "ls") }

// printTargetList 는 1단계 통과 후와 4단계 ls 에서 같은 모양으로 대상 목록과 "총 N대" 를 출력한다.
func printTargetList(out io.Writer, targets []string, opts GuardOpts) {
	fmt.Fprintln(out, "작업 대상 호스트 목록:")
	if opts.ShowTargets != nil {
		opts.ShowTargets(out)
	} else {
		for _, t := range targets {
			fmt.Fprintln(out, t)
		}
	}
	fmt.Fprintln(out, Bold(fmt.Sprintf("총 %d대", len(targets))))
}

// RunGuard 는 계획서 10장의 안전장치 1~5단계. 통과하면 nil, 취소면 *GuardError(화면 출력까지 끝낸 상태).
// opts.Checker/CrossCheck 가 있으면 2·3단계가 교차검증 방식(확인자 선택 → 승인 대기)으로 바뀐다.
func RunGuard(ctx context.Context, in *LineReader, out io.Writer, targets []string, opts GuardOpts) error {
	opts = opts.withDefaults()
	interrupted := func(err error) bool {
		return err != nil && ctx.Err() != nil && !errors.Is(err, io.EOF) && !errors.Is(err, ErrTimeout)
	}
	ccMode := opts.Checker != nil && opts.CrossCheck != nil

	// 1단계: 작업 확인 문구(고정, 작업 종류와 무관)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s %s(%s)을 그대로 입력하세요.\n > ", Bold("[1/5]"), Bold(Yellow(WorkPhrase)), WorkPhraseHint)
	line, err := in.ReadLineLimit(ctx, opts.StepTimeout)
	if interrupted(err) {
		return cancelGuard(out, reasonInterrupt)
	}
	if err != nil {
		fmt.Fprintln(out)
	}
	if errors.Is(err, ErrTimeout) {
		return cancelGuard(out, inputTimeoutReason(opts.StepTimeout))
	}
	if err != nil || strings.TrimSpace(line) != WorkPhrase {
		return cancelGuard(out, reasonWorkPhrase)
	}
	fmt.Fprintln(out, Green("[INFO] 작업 확인 문구 일치"))
	printTargetList(out, targets, opts)

	if ccMode {
		if err := ccStages(ctx, in, out, opts); err != nil {
			return err
		}
	} else if err := classicStages(ctx, in, out, targets, opts, interrupted); err != nil {
		return err
	}

	// 4단계: YeS (ls = 대상 목록 다시 보기, 몇 번이든 취소 없이 다시 묻는다)
	// 제한: 교차검증 방식은 승인 마감 시각까지(ls 를 해도 늘어나지 않음), 마감을 모르거나 기존 방식이면 질문마다 StepTimeout.
	var deadline time.Time
	if ccMode && opts.ConfirmDeadline != nil {
		deadline = opts.ConfirmDeadline()
	}
	for {
		fmt.Fprintf(out, "%s 정말 작업을 진행하시겠습니까? (YeS/No, ls=대상 보기): ", Bold("[4/5]"))
		var ans string
		var err error
		if !deadline.IsZero() {
			ans, err = in.ReadLineTimeout(ctx, time.Until(deadline))
		} else {
			ans, err = in.ReadLineLimit(ctx, opts.StepTimeout)
		}
		if interrupted(err) {
			return cancelGuard(out, reasonInterrupt)
		}
		if errors.Is(err, ErrTimeout) {
			fmt.Fprintln(out)
			if !deadline.IsZero() {
				return cancelGuard(out, reasonCCExpired)
			}
			return cancelGuard(out, inputTimeoutReason(opts.StepTimeout))
		}
		if err != nil {
			fmt.Fprintln(out)
			return cancelGuard(out, "YeS 미입력 (입력값: EOF)")
		}
		if isLs(ans) {
			if opts.OnList != nil {
				opts.OnList()
			}
			printTargetList(out, targets, opts)
			continue
		}
		ans = strings.TrimSpace(ans)
		if ans != "YeS" {
			shown := ans
			if shown == "" {
				shown = "빈 입력"
			}
			return cancelGuard(out, "YeS 미입력 (입력값: "+shown+")")
		}
		break
	}

	// 5단계: 카운트다운(한 줄에 가로로)
	fmt.Fprintf(out, "%s (Ctrl+C 로 취소 가능)\n", Bold("[5/5]"))
	t := time.NewTimer(opts.Tick)
	defer t.Stop()
	for i := opts.Countdown; i >= 1; i-- {
		if i != opts.Countdown {
			fmt.Fprint(out, " .. ")
		}
		fmt.Fprint(out, strconv.Itoa(i))
		t.Reset(opts.Tick)
		select {
		case <-ctx.Done():
			return cancelGuard(out, reasonInterrupt)
		case <-t.C:
		}
	}
	fmt.Fprintln(out)

	// 카운트다운 직후(교차검증: 승인 유효시간 안인지 --start 로 확인)
	if opts.AfterCountdown != nil {
		if err := opts.AfterCountdown(ctx); err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(out)
			}
			fmt.Fprintln(out, Red("[취소] "+reasonCCStartFailed))
			return &GuardError{Reason: reasonCCStartFailed}
		}
	}
	return nil
}

// ccStages 는 교차검증 방식의 2·3단계: 확인자 번호 선택 → 확인자 승인 대기(VM 이름 입력 없음).
func ccStages(ctx context.Context, in *LineReader, out io.Writer, opts GuardOpts) error {
	// 2단계: 확인자 선택 (Checker 가 메뉴 출력·재입력까지 맡는다)
	fmt.Fprintln(out)
	checker, err := opts.Checker(ctx, in, out)
	if err != nil {
		if ctx.Err() != nil {
			return cancelGuard(out, reasonInterrupt)
		}
		return cancelGuard(out, err.Error())
	}

	// 3단계: 교차검증 승인 대기 (CrossCheck 가 진행 출력을 맡는다)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s 교차검증 — 확인자 %s 의 승인을 받습니다 (VM 이름 입력 대신)\n", Bold("[3/5]"), checker)
	if err := opts.CrossCheck(ctx, checker); err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(out) // 승인 대기 줄을 끝내고 출력
		}
		fmt.Fprintln(out, Red("[취소] "+reasonCCNotApproved))
		return &GuardError{Reason: reasonCCNotApproved}
	}
	return nil
}

// classicStages 는 기존 방식의 2·3단계: 준비 확인(Enter) → 제한 시간 안에 VM 이름 입력.
func classicStages(ctx context.Context, in *LineReader, out io.Writer, targets []string, opts GuardOpts, interrupted func(error) bool) error {
	// 2단계: 준비 확인(제한 StepTimeout, 입력 내용은 버림)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s 다음 단계는 1분(60초)의 입력 제한 시간이 있습니다. 호스트명을 입력할 준비가 완료되면 Enter를 눌러 다음 단계로 진행하세요.\n", Bold("[2/5]"))
	if _, err := in.ReadLineLimit(ctx, opts.StepTimeout); err != nil {
		if interrupted(err) {
			return cancelGuard(out, reasonInterrupt)
		}
		if errors.Is(err, ErrTimeout) {
			return cancelGuard(out, inputTimeoutReason(opts.StepTimeout))
		}
		return cancelGuard(out, "입력 종료(EOF)")
	}

	// 3단계: 호스트명 입력(시작부터 전체 제한 시간)
	fmt.Fprintf(out, "%s 작업 대상 호스트명 %d개를 입력하세요 (공백/탭/줄바꿈 구분, 순서 무관, %s 이내):\n",
		Bold("[3/5]"), len(targets), secs(opts.InputTimeout))
	deadline := time.Now().Add(opts.InputTimeout)
	var tokens []string
	for {
		line, err := in.ReadLineTimeout(ctx, time.Until(deadline))
		if errors.Is(err, ErrTimeout) {
			fmt.Fprintln(out)
			return cancelGuard(out, "입력 시간 "+secs(opts.InputTimeout)+" 초과")
		}
		if interrupted(err) {
			return cancelGuard(out, reasonInterrupt)
		}
		if err != nil { // EOF: 지금까지 입력으로 판정
			break
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			if len(tokens) > 0 {
				break
			}
			continue
		}
		tokens = append(tokens, f...)
		if len(tokens) >= len(targets) {
			break
		}
		left := time.Until(deadline)
		fmt.Fprintf(out, "(남은 시간 %d초)\n", int((left+time.Second-1)/time.Second))
	}
	if msgs := compareHosts(targets, tokens); len(msgs) > 0 {
		for _, m := range msgs {
			fmt.Fprintln(out, Yellow("  "+m))
		}
		return cancelGuard(out, "호스트 목록 불일치")
	}
	fmt.Fprintln(out, Green(fmt.Sprintf("[INFO] 호스트 목록 일치 (%d대)", len(targets))))
	return nil
}

// compareHosts 는 입력 토큰과 대상을 비교해 차이 설명을 돌려준다(같으면 nil). 대소문자 정확 비교.
func compareHosts(targets, tokens []string) []string {
	want := map[string]bool{}
	for _, t := range targets {
		want[t] = true
	}
	cnt := map[string]int{}
	var order []string
	for _, t := range tokens {
		if cnt[t] == 0 {
			order = append(order, t)
		}
		cnt[t]++
	}
	var missing, extra, dup []string
	for _, t := range targets {
		if cnt[t] == 0 && !contains(missing, t) {
			missing = append(missing, t)
		}
	}
	for _, t := range order {
		if !want[t] {
			extra = append(extra, t)
		}
		if cnt[t] > 1 {
			dup = append(dup, fmt.Sprintf("%s(%d회)", t, cnt[t]))
		}
	}
	var msgs []string
	if len(missing) > 0 {
		msgs = append(msgs, "누락: "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		msgs = append(msgs, "대상 아님: "+strings.Join(extra, ", "))
	}
	if len(dup) > 0 {
		msgs = append(msgs, "중복: "+strings.Join(dup, ", "))
	}
	return msgs
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// ConfirmPhrase 는 VWP 담당자 확인용: prompt 출력 후 한 줄(앞뒤 공백 무시)이 phrase 와 같으면 nil.
// timeout(> 0) 안에 입력이 없으면 "입력 시간 N분 초과" 로 취소한다.
func ConfirmPhrase(ctx context.Context, in *LineReader, out io.Writer, prompt string, phrase string, timeout time.Duration) error {
	fmt.Fprint(out, prompt)
	line, err := in.ReadLineLimit(ctx, timeout)
	if err != nil && ctx.Err() != nil && !errors.Is(err, io.EOF) && !errors.Is(err, ErrTimeout) {
		return cancelGuard(out, reasonInterrupt)
	}
	if err != nil {
		fmt.Fprintln(out)
	}
	if errors.Is(err, ErrTimeout) {
		return cancelGuard(out, inputTimeoutReason(timeout))
	}
	if err != nil || strings.TrimSpace(line) != phrase {
		return cancelGuard(out, reasonVWPPhrase)
	}
	return nil
}

// demoTargets 는 --demo 샘플 대상.
var demoTargets = []string{"web-prd-01", "web-prd-02", "was-prd-01"}

// RunDemo 는 --demo: 샘플 3대로 안전장치만 돌린다. 통과 0, 취소 1.
// PWONOFF_TEST_* 환경변수는 시험용으로 데모에서만 읽는다(실제 모드에선 읽지 않음).
func RunDemo(ctx context.Context, in *LineReader, out io.Writer) int {
	var opts GuardOpts
	if v, err := strconv.Atoi(os.Getenv("PWONOFF_TEST_INPUT_TIMEOUT")); err == nil && v > 0 {
		opts.InputTimeout = time.Duration(v) * time.Second
	}
	if v, err := strconv.Atoi(os.Getenv("PWONOFF_TEST_TICK_MS")); err == nil && v > 0 {
		opts.Tick = time.Duration(v) * time.Millisecond
	}
	// 1/5·2/5·4/5 질문마다의 제한(실제 실행의 input_timeout_sec 에 해당, 데모 기본 10분)
	if v, err := strconv.Atoi(os.Getenv("PWONOFF_TEST_STEP_TIMEOUT")); err == nil && v > 0 {
		opts.StepTimeout = time.Duration(v) * time.Second
	}
	fmt.Fprintln(out, Cyan("[DEMO] vCenter 없이 샘플 대상 3대로 안전장치만 시험합니다"))
	fmt.Fprintln(out, Cyan("[DEMO] 데모는 기존 방식입니다. 실제 실행에서 cross_check=on(기본)이면 2/5 는 확인자 번호 선택, 3/5 는 VM 이름 입력 대신 확인자 승인 대기입니다"))
	if err := RunGuard(ctx, in, out, demoTargets, opts); err != nil {
		return ExitFail
	}
	fmt.Fprintln(out, Green("[DEMO] 실제 전원 작업은 하지 않습니다"))
	return ExitOK
}
