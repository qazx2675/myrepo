package main

import (
	"context"
	"crypto/rand"
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
	Countdown    int               // 5단계 카운트다운 시작 숫자(기본 10)
	Tick         time.Duration     // 카운트다운 간격(기본 1s)
	NewCode      func() string     // nil 이면 NewCode()
	ShowTargets  func(w io.Writer) // 1단계 통과 후·4단계 ls 때 대상 목록 출력(nil 이면 한 줄에 하나씩). "총 N대" 는 RunGuard 가 출력
	OnList       func()            // 4단계에서 ls 를 입력할 때마다 호출(감사 로그 횟수용, nil 이면 무시)

	// 교차검증 방식(cross_check=on): Checker 와 CrossCheck 를 둘 다 주면
	// 2단계 = 확인자 번호 선택(Checker), 3단계 = VM 이름 입력 대신 확인자 승인 대기(CrossCheck).
	// 둘 중 하나라도 nil 이면 기존 방식(2단계 Enter, 3단계 VM 이름 입력).
	Checker    func(ctx context.Context, in *LineReader, out io.Writer) (string, error) // 오류 문구 = 취소 사유
	CrossCheck func(ctx context.Context, checker string) error                          // 승인 대기. nil 이 아니면 미승인
	// AfterCountdown 은 5단계 카운트다운이 끝난 직후 호출된다(교차검증의 --start). 오류면 취소.
	AfterCountdown func(ctx context.Context) error
}

// 교차검증 방식의 취소 사유
const (
	reasonCCNotApproved = "교차검증 미승인"
	reasonCCStartFailed = "교차검증 승인 유효시간 초과 또는 거부"
)

func (o GuardOpts) withDefaults() GuardOpts {
	if o.InputTimeout <= 0 {
		o.InputTimeout = 60 * time.Second
	}
	if o.Countdown <= 0 {
		o.Countdown = 10
	}
	if o.Tick <= 0 {
		o.Tick = time.Second
	}
	if o.NewCode == nil {
		o.NewCode = NewCode
	}
	return o
}

const codeChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// NewCode 는 crypto/rand 로 [A-Z0-9] 6자리 확인 코드를 만든다.
func NewCode() string {
	out := make([]byte, 0, 6)
	buf := make([]byte, 16)
	for len(out) < 6 {
		if _, err := rand.Read(buf); err != nil {
			panic("crypto/rand 실패: " + err.Error())
		}
		for _, b := range buf {
			// 252 = 36*7, 그 이상은 버려 치우침 방지
			if b < 252 && len(out) < 6 {
				out = append(out, codeChars[b%36])
			}
		}
	}
	return string(out)
}

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

	// 1단계: 확인 코드
	code := opts.NewCode()
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s 확인 코드: %s\n", Bold("[1/5]"), Bold(Yellow(code)))
	fmt.Fprint(out, "위 확인 코드를 그대로 입력하세요: ")
	line, err := in.ReadLine(ctx)
	if interrupted(err) {
		return cancelGuard(out, reasonInterrupt)
	}
	if err != nil {
		fmt.Fprintln(out)
	}
	if err != nil || strings.TrimSpace(line) != code {
		return cancelGuard(out, "확인 코드 불일치")
	}
	fmt.Fprintln(out, Green("[INFO] 확인 코드 일치"))
	printTargetList(out, targets, opts)

	if ccMode {
		if err := ccStages(ctx, in, out, opts); err != nil {
			return err
		}
	} else if err := classicStages(ctx, in, out, targets, opts, interrupted); err != nil {
		return err
	}

	// 4단계: YeS (ls = 대상 목록 다시 보기, 몇 번이든 취소 없이 다시 묻는다)
	for {
		fmt.Fprintf(out, "%s 정말 작업을 진행하시겠습니까? (YeS/No, ls=대상 보기): ", Bold("[4/5]"))
		ans, err := in.ReadLine(ctx)
		if interrupted(err) {
			return cancelGuard(out, reasonInterrupt)
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
	// 2단계: 준비 확인(시간 제한 없음, 입력 내용은 버림)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s 다음 단계는 1분(60초)의 입력 제한 시간이 있습니다. 호스트명을 입력할 준비가 완료되면 Enter를 눌러 다음 단계로 진행하세요.\n", Bold("[2/5]"))
	if _, err := in.ReadLine(ctx); err != nil {
		if interrupted(err) {
			return cancelGuard(out, reasonInterrupt)
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

// ConfirmCode 는 VWP 확인용: prompt 출력 후 한 줄이 code 와 같으면 nil.
func ConfirmCode(ctx context.Context, in *LineReader, out io.Writer, prompt string, code string) error {
	fmt.Fprint(out, prompt)
	line, err := in.ReadLine(ctx)
	if err != nil && ctx.Err() != nil && !errors.Is(err, io.EOF) {
		return cancelGuard(out, reasonInterrupt)
	}
	if err != nil {
		fmt.Fprintln(out)
	}
	if err != nil || strings.TrimSpace(line) != code {
		return cancelGuard(out, "VWP 확인 코드 불일치")
	}
	return nil
}

// demoTargets 는 --demo 샘플 대상.
var demoTargets = []string{"web-prd-01", "web-prd-02", "was-prd-01"}

// RunDemo 는 --demo: 샘플 3대로 안전장치만 돌린다. 통과 0, 취소 1.
// PWONOFF_TEST_* 환경변수는 시험용으로 데모에서만 읽는다(실제 모드에선 읽지 않음).
func RunDemo(ctx context.Context, in *LineReader, out io.Writer) int {
	var opts GuardOpts
	if c := os.Getenv("PWONOFF_TEST_CODE"); c != "" {
		opts.NewCode = func() string { return c }
	}
	if v, err := strconv.Atoi(os.Getenv("PWONOFF_TEST_INPUT_TIMEOUT")); err == nil && v > 0 {
		opts.InputTimeout = time.Duration(v) * time.Second
	}
	if v, err := strconv.Atoi(os.Getenv("PWONOFF_TEST_TICK_MS")); err == nil && v > 0 {
		opts.Tick = time.Duration(v) * time.Millisecond
	}
	fmt.Fprintln(out, Cyan("[DEMO] vCenter 없이 샘플 대상 3대로 안전장치만 시험합니다"))
	fmt.Fprintln(out, Cyan("[DEMO] 데모는 기존 방식입니다. 실제 실행에서 cross_check=on(기본)이면 2/5 는 확인자 번호 선택, 3/5 는 VM 이름 입력 대신 확인자 승인 대기입니다"))
	if err := RunGuard(ctx, in, out, demoTargets, opts); err != nil {
		return ExitFail
	}
	fmt.Fprintln(out, Green("[DEMO] 실제 전원 작업은 하지 않습니다"))
	return ExitOK
}
