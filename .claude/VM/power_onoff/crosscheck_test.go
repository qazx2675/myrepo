package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const ccTestToken = "SECRET-TOKEN-XYZ"

type fakeRunCall struct {
	user, checker string
	targets       []string
}

type fakeStartCall struct{ job, token string }

// fakeCC 는 CrossChecker 가짜: 호출을 기록하고 정해 둔 결과를 돌려준다.
type fakeCC struct {
	mu         sync.Mutex
	runs       []fakeRunCall
	starts     []fakeStartCall
	order      []string
	job, token string
	runErr     error
	startErr   error
	block      bool // true 면 ctx 가 취소될 때까지 대기
}

func (f *fakeCC) Run(ctx context.Context, user, checker string, targets []string, out, errOut io.Writer) (string, string, error) {
	f.mu.Lock()
	f.runs = append(f.runs, fakeRunCall{user, checker, append([]string(nil), targets...)})
	f.order = append(f.order, "run")
	block, job, token, err := f.block, f.job, f.token, f.runErr
	f.mu.Unlock()
	io.WriteString(out, "가짜 cross_check 진행 출력\n")
	if block {
		<-ctx.Done()
		return "", "", ctx.Err()
	}
	if err != nil {
		return "", "", err
	}
	return job, token, nil
}

func (f *fakeCC) Start(ctx context.Context, jobID, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, fakeStartCall{jobID, token})
	f.order = append(f.order, "start")
	return f.startErr
}

func approvedFake() *fakeCC { return &fakeCC{job: "J-100", token: ccTestToken} }

var ccUsers = []string{"lsh", "ljh", "dhk"}

type gateEnv struct {
	g     *CrossCheckGate
	out   *syncBuf
	errw  *syncBuf
	audit *Audit
}

func newGateEnv(t *testing.T, cc CrossChecker, stdin string) *gateEnv {
	t.Helper()
	a, err := OpenAudit(t.TempDir(), ActPowerOff)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	e := &gateEnv{out: &syncBuf{}, errw: &syncBuf{}, audit: a}
	e.g = &CrossCheckGate{
		Cfg: &Config{Users: ccUsers}, CC: cc, In: NewLineReader(strings.NewReader(stdin)),
		Out: e.out, ErrOut: e.errw, Audit: a,
	}
	return e
}

func (e *gateEnv) auditText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(e.audit.Path())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// noToken 은 화면과 감사 로그 어디에도 토큰이 없는지 확인한다.
func (e *gateEnv) noToken(t *testing.T) {
	t.Helper()
	if strings.Contains(e.out.String(), ccTestToken) || strings.Contains(e.errw.String(), ccTestToken) {
		t.Errorf("토큰이 화면에 나옴:\n%s", e.out.String())
	}
	if strings.Contains(e.auditText(t), ccTestToken) {
		t.Errorf("토큰이 감사 로그에 남음:\n%s", e.auditText(t))
	}
}

var ccTargets = []string{"svr1ev01", "svr1ev02", "svr2ev01"}

func TestCrossCheckGateApprove(t *testing.T) {
	f := approvedFake()
	e := newGateEnv(t, f, "")
	if err := e.g.Run(context.Background(), "lsh", "dhk", ccTargets); err != nil {
		t.Fatalf("승인 경로는 nil 이어야 함: %v", err)
	}
	if len(f.runs) != 1 || f.runs[0].user != "lsh" || f.runs[0].checker != "dhk" ||
		strings.Join(f.runs[0].targets, " ") != "svr1ev01 svr1ev02 svr2ev01" {
		t.Errorf("Run 호출 이상: %+v", f.runs)
	}
	if len(f.starts) != 1 || f.starts[0].job != "J-100" || f.starts[0].token != ccTestToken {
		t.Errorf("Start 호출 이상: %+v", f.starts)
	}
	if strings.Join(f.order, ",") != "run,start" {
		t.Errorf("호출 순서: %v", f.order)
	}
	out := e.out.String()
	if !strings.Contains(out, crossCheckNotice) {
		t.Errorf("승인 유효시간 정책 안내가 없음:\n%s", out)
	}
	if strings.Index(out, crossCheckNotice) > strings.Index(out, "가짜 cross_check 진행 출력") {
		t.Errorf("정책 안내는 승인 대기(진행 출력)보다 먼저여야 함:\n%s", out)
	}
	if !strings.Contains(out, "교차검증 승인됨") || strings.Contains(out, "[취소]") {
		t.Errorf("승인 안내 이상:\n%s", out)
	}
	au := e.auditText(t)
	for _, w := range []string{"교차검증 요청", "확인자=dhk", "교차검증 승인", "job=J-100", "교차검증 시작 허용"} {
		if !strings.Contains(au, w) {
			t.Errorf("감사 로그에 %q 없음:\n%s", w, au)
		}
	}
	e.noToken(t)
}

func TestCrossCheckGateAbort(t *testing.T) {
	for _, tc := range []struct {
		code  int
		class string
	}{{1, "거부/중단(exit 1)"}, {3, "데몬/연결/검증 오류(exit 3)"}, {7, "기타 종료(exit 7)"}} {
		f := approvedFake()
		f.runErr = &CrossCheckError{Code: tc.code, Msg: "시험 " + ccTestToken} // Msg 가 새도 로그에는 분류만
		e := newGateEnv(t, f, "")
		err := e.g.Run(context.Background(), "lsh", "dhk", ccTargets)
		if err == nil {
			t.Fatalf("exit %d: 오류여야 함", tc.code)
		}
		if len(f.starts) != 0 {
			t.Errorf("exit %d: 미승인인데 Start 호출됨", tc.code)
		}
		if !strings.Contains(e.out.String(), "[취소] 교차검증 미승인") {
			t.Errorf("exit %d: 취소 문구 없음:\n%s", tc.code, e.out.String())
		}
		au := e.auditText(t)
		if !strings.Contains(au, "교차검증 미승인") || !strings.Contains(au, tc.class) {
			t.Errorf("exit %d: 감사 로그에 사유 분류 없음:\n%s", tc.code, au)
		}
		if strings.Contains(au, "교차검증 승인 ") || strings.Contains(au, "시작 허용") {
			t.Errorf("exit %d: 미승인인데 승인으로 기록됨:\n%s", tc.code, au)
		}
		e.noToken(t)
	}
	// cross_check 를 실행하지 못한 일반 오류도 미승인
	f := approvedFake()
	f.runErr = errors.New("exec: not found")
	e := newGateEnv(t, f, "")
	if err := e.g.Run(context.Background(), "lsh", "dhk", ccTargets); err == nil || len(f.starts) != 0 {
		t.Errorf("일반 오류도 미승인이어야 함: %v", err)
	}
}

func TestCrossCheckGateInterrupt(t *testing.T) {
	f := approvedFake()
	f.block = true
	e := newGateEnv(t, f, "")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	done := make(chan error, 1)
	go func() { done <- e.g.Run(ctx, "lsh", "dhk", ccTargets) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Ctrl+C 는 오류여야 함")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 취소 후에도 반환하지 않음")
	}
	if len(f.starts) != 0 {
		t.Error("Ctrl+C 후 Start 호출됨")
	}
	if !strings.Contains(e.auditText(t), "Ctrl+C(exit 130)") || !strings.Contains(e.out.String(), "[취소] 교차검증 미승인") {
		t.Errorf("Ctrl+C 기록/출력 이상:\n%s\n%s", e.out.String(), e.auditText(t))
	}
}

func TestCrossCheckGateStartRefused(t *testing.T) {
	f := approvedFake()
	f.startErr = &CrossCheckError{Code: 1, Msg: "승인 만료 " + ccTestToken}
	e := newGateEnv(t, f, "")
	if err := e.g.Run(context.Background(), "lsh", "dhk", ccTargets); err == nil {
		t.Fatal("시작 거부는 오류여야 함")
	}
	if len(f.runs) != 1 || len(f.starts) != 1 {
		t.Errorf("호출 횟수 이상: run=%d start=%d", len(f.runs), len(f.starts))
	}
	out := e.out.String()
	if !strings.Contains(out, "[취소] 교차검증 승인 유효시간 초과 또는 거부") {
		t.Errorf("시작 거부 문구 없음:\n%s", out)
	}
	au := e.auditText(t)
	if !strings.Contains(au, "교차검증 승인 확인자=dhk job=J-100") || !strings.Contains(au, "시작 거부") || strings.Contains(au, "시작 허용") {
		t.Errorf("감사 로그 이상:\n%s", au)
	}
	e.noToken(t)
}

func TestCrossCheckGateBadApproval(t *testing.T) {
	for name, f := range map[string]*fakeCC{
		"토큰 없음":   {job: "J-1"},
		"job 없음":  {token: ccTestToken},
		"job 하이픈": {job: "-rf", token: ccTestToken},
		"job 공백":  {job: "a b", token: ccTestToken},
	} {
		e := newGateEnv(t, f, "")
		if err := e.g.Run(context.Background(), "lsh", "dhk", ccTargets); err == nil {
			t.Errorf("%s: 오류여야 함", name)
		}
		if len(f.starts) != 0 {
			t.Errorf("%s: Start 호출됨", name)
		}
		e.noToken(t)
	}
}

func TestCrossCheckCheckerValidation(t *testing.T) {
	// -c 검증: users 에 없음 / 작업자 본인 → Run 호출 없음
	for name, checker := range map[string]string{"users 에 없음": "zzz", "본인": "lsh", "대소문자": "DHK"} {
		f := approvedFake()
		e := newGateEnv(t, f, "")
		if err := e.g.Run(context.Background(), "lsh", checker, ccTargets); err == nil {
			t.Errorf("%s: 오류여야 함", name)
		}
		if len(f.runs) != 0 || len(f.starts) != 0 {
			t.Errorf("%s: 잘못된 확인자인데 cross_check 가 호출됨", name)
		}
		if !strings.Contains(e.out.String(), "[취소] 교차검증 미승인") {
			t.Errorf("%s: 취소 문구 없음:\n%s", name, e.out.String())
		}
	}
	cfg := &Config{Users: ccUsers}
	if ValidateChecker(cfg, "lsh", "ljh") != nil || ValidateChecker(cfg, "lsh", "lsh") == nil || ValidateChecker(cfg, "lsh", "x") == nil {
		t.Error("ValidateChecker 이상")
	}
}

func TestCrossCheckCheckerMenu(t *testing.T) {
	// 작업자 ljh → 메뉴는 lsh, dhk (본인 제외). 2번 = dhk
	f := approvedFake()
	e := newGateEnv(t, f, "2\n")
	if err := e.g.Run(context.Background(), "ljh", "", ccTargets); err != nil {
		t.Fatal(err)
	}
	if len(f.runs) != 1 || f.runs[0].checker != "dhk" {
		t.Errorf("메뉴 2번은 dhk 여야 함: %+v", f.runs)
	}
	menu := e.out.String()
	if !strings.Contains(menu, "1) lsh") || !strings.Contains(menu, "2) dhk") || strings.Contains(menu, ") ljh") {
		t.Errorf("메뉴에 본인이 있거나 목록 이상:\n%s", menu)
	}

	// 1번 = lsh
	f = approvedFake()
	e = newGateEnv(t, f, "1\n")
	if err := e.g.Run(context.Background(), "ljh", "", ccTargets); err != nil || f.runs[0].checker != "lsh" {
		t.Errorf("1번은 lsh: %v %+v", err, f.runs)
	}

	// 범위 밖·숫자 아님·입력 끝(EOF) → 호출 없이 취소
	for _, in := range []string{"3\n", "0\n", "x\n", "\n", ""} {
		f = approvedFake()
		e = newGateEnv(t, f, in)
		if err := e.g.Run(context.Background(), "ljh", "", ccTargets); err == nil {
			t.Errorf("입력 %q: 오류여야 함", in)
		}
		if len(f.runs) != 0 {
			t.Errorf("입력 %q: 확인자 선택 실패인데 cross_check 호출됨", in)
		}
	}

	// users 에 본인뿐이면 확인자를 고를 수 없다
	f = approvedFake()
	e = newGateEnv(t, f, "1\n")
	e.g.Cfg = &Config{Users: []string{"lsh"}}
	if err := e.g.Run(context.Background(), "lsh", "", ccTargets); err == nil || len(f.runs) != 0 {
		t.Errorf("본인뿐이면 취소여야 함: %v", err)
	}
}

// 계획만(-n) / 교차검증 off 에서는 관문을 쓰지 않는다. 데모(--demo)는 run() 까지 오지 않는다.
func TestCrossCheckNeeded(t *testing.T) {
	for _, tc := range []struct {
		on, plan, want bool
	}{{true, false, true}, {true, true, false}, {false, false, false}, {false, true, false}} {
		if got := crossCheckNeeded(&Config{CrossCheck: tc.on}, tc.plan); got != tc.want {
			t.Errorf("cross_check=%v -n=%v → %v", tc.on, tc.plan, got)
		}
	}
}

func TestCheckCrossCheckBin(t *testing.T) {
	dir := t.TempDir()
	if CheckCrossCheckBin(filepath.Join(dir, "none")) == nil {
		t.Error("없는 파일은 오류")
	}
	if CheckCrossCheckBin(dir) == nil {
		t.Error("디렉터리는 오류")
	}
	p := filepath.Join(dir, "cross_check")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CheckCrossCheckBin(p); err != nil {
		t.Errorf("실행 파일은 통과: %v", err)
	}
	if runtime.GOOS != "windows" {
		q := filepath.Join(dir, "noexec")
		if err := os.WriteFile(q, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if CheckCrossCheckBin(q) == nil {
			t.Error("실행 권한이 없으면 오류")
		}
	}
}

// ccFilter -------------------------------------------------------------

func TestCCFilter(t *testing.T) {
	const in = "승인 대기 중...\r\nCROSS_CHECK_JOB=J-9\nCROSS_CHECK_TOKEN=" + ccTestToken + "\r\nCROSS_CHECK_OTHER 그대로\n프롬프트> "
	const want = "승인 대기 중...\r\nCROSS_CHECK_OTHER 그대로\n프롬프트> "
	for _, chunk := range []int{len(in), 1, 3, 7} {
		var out syncBuf
		f := &ccFilter{out: &out}
		for i := 0; i < len(in); i += chunk {
			end := i + chunk
			if end > len(in) {
				end = len(in)
			}
			if n, err := f.Write([]byte(in[i:end])); err != nil || n != end-i {
				t.Fatalf("Write: %d %v", n, err)
			}
		}
		f.flush()
		if out.String() != want {
			t.Errorf("chunk=%d 통과 출력 이상: %q", chunk, out.String())
		}
		if f.job != "J-9" || f.token != ccTestToken {
			t.Errorf("chunk=%d 값 이상: job=%q token=%q", chunk, f.job, f.token)
		}
	}
	// 줄바꿈 없이 끝난 표식, 그리고 줄바꿈 없는 프롬프트는 바로 보인다
	var out syncBuf
	f := &ccFilter{out: &out}
	f.Write([]byte("계속? (y/n) "))
	if out.String() != "계속? (y/n) " {
		t.Errorf("프롬프트가 지연됨: %q", out.String())
	}
	f.Write([]byte("y\nCROSS_CHECK_TOKEN=abc"))
	f.flush()
	if f.token != "abc" || strings.Contains(out.String(), "abc") || out.String() != "계속? (y/n) y\n" {
		t.Errorf("끝 표식 처리 이상: token=%q out=%q", f.token, out.String())
	}
}

// exec 구현 (이 시험 바이너리를 cross_check 대역으로 다시 실행) ----------------------

func ccHelperChecker() *execCrossChecker {
	return &execCrossChecker{Bin: os.Args[0], pre: []string{"-test.run=^TestCCHelperProcess$", "--"}}
}

// TestCCHelperProcess 는 CC_HELPER_MODE 가 있을 때만 cross_check 대역으로 동작한다.
func TestCCHelperProcess(t *testing.T) {
	mode := os.Getenv("CC_HELPER_MODE")
	if mode == "" {
		return
	}
	args := os.Args
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	switch mode {
	case "approve":
		b, _ := io.ReadAll(os.Stdin)
		os.Stdout.WriteString("대기 중 args=" + strings.Join(args, " ") + " stdin=" + strings.Join(strings.Fields(string(b)), ",") + "\n")
		os.Stderr.WriteString("err-progress\n")
		os.Stdout.WriteString("CROSS_CHECK_JOB=J-77\nCROSS_CHECK_TOKEN=" + ccTestToken + "\n")
		os.Exit(0)
	case "refuse":
		os.Stdout.WriteString("사유: 확인자가 거부\n")
		os.Exit(1)
	case "error":
		os.Exit(3)
	case "nomarker":
		os.Stdout.WriteString("승인인데 표식 없음\n")
		os.Exit(0)
	case "start":
		if strings.Join(args, " ") == "--start --job J-77" && os.Getenv("CROSS_CHECK_TOKEN") == ccTestToken {
			os.Exit(0)
		}
		os.Stdout.WriteString("환경/인자 불일치\n")
		os.Exit(1)
	case "start_expired":
		os.Stdout.WriteString("만료됨 token=" + os.Getenv("CROSS_CHECK_TOKEN") + "\n두번째 줄\n")
		os.Exit(1)
	case "hang":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(99)
}

func TestExecCrossCheckerRun(t *testing.T) {
	t.Setenv("CC_HELPER_MODE", "approve")
	var out, errOut syncBuf
	job, token, err := ccHelperChecker().Run(context.Background(), "lsh", "dhk", ccTargets, &out, &errOut)
	if err != nil || job != "J-77" || token != ccTestToken {
		t.Fatalf("승인 경로: %q %q %v", job, token, err)
	}
	if !strings.Contains(out.String(), "args=--run --user lsh --checker dhk stdin=svr1ev01,svr1ev02,svr2ev01") {
		t.Errorf("인자/표준입력 이상: %q", out.String())
	}
	if strings.Contains(out.String(), ccTestToken) || strings.Contains(out.String(), "CROSS_CHECK_") {
		t.Errorf("표식 줄이 화면으로 나감: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "err-progress") {
		t.Errorf("stderr 가 전달되지 않음: %q", errOut.String())
	}
}

func TestExecCrossCheckerRunFailures(t *testing.T) {
	for mode, code := range map[string]int{"refuse": 1, "error": 3, "nomarker": 3} {
		t.Setenv("CC_HELPER_MODE", mode)
		var out syncBuf
		_, token, err := ccHelperChecker().Run(context.Background(), "lsh", "dhk", ccTargets, &out, io.Discard)
		var ce *CrossCheckError
		if !errors.As(err, &ce) || ce.Code != code || token != "" {
			t.Errorf("%s: 코드 %d 기대, 실제 %v", mode, code, err)
		}
	}
	t.Setenv("CC_HELPER_MODE", "refuse")
	var out syncBuf
	ccHelperChecker().Run(context.Background(), "lsh", "dhk", ccTargets, &out, io.Discard)
	if !strings.Contains(out.String(), "확인자가 거부") {
		t.Errorf("거부 사유 출력이 전달되지 않음: %q", out.String())
	}
	// 실행 파일이 없으면 실행 실패
	_, _, err := (&execCrossChecker{Bin: filepath.Join(t.TempDir(), "none")}).Run(context.Background(), "a", "b", ccTargets, io.Discard, io.Discard)
	var ce *CrossCheckError
	if !errors.As(err, &ce) || ce.Code != ccExitSpawn {
		t.Errorf("실행 실패 분류: %v", err)
	}
}

func TestExecCrossCheckerInterrupt(t *testing.T) {
	t.Setenv("CC_HELPER_MODE", "hang")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(500 * time.Millisecond); cancel() }()
	t0 := time.Now()
	_, _, err := ccHelperChecker().Run(ctx, "lsh", "dhk", ccTargets, io.Discard, io.Discard)
	var ce *CrossCheckError
	if !errors.As(err, &ce) || ce.Code != ccExitInterrupt {
		t.Errorf("Ctrl+C 는 130 이어야 함: %v", err)
	}
	if time.Since(t0) > 15*time.Second {
		t.Errorf("취소 후 너무 오래 걸림: %v", time.Since(t0))
	}
}

func TestExecCrossCheckerStart(t *testing.T) {
	t.Setenv("CC_HELPER_MODE", "start")
	if err := ccHelperChecker().Start(context.Background(), "J-77", ccTestToken); err != nil {
		t.Errorf("토큰은 자식 환경으로 전달되어야 함: %v", err)
	}
	if os.Getenv("CROSS_CHECK_TOKEN") != "" {
		t.Error("부모 환경에 토큰이 남음")
	}
	if err := ccHelperChecker().Start(context.Background(), "J-77", "다른토큰"); err == nil {
		t.Error("토큰이 다르면 거부되어야 함")
	}
	if err := ccHelperChecker().Start(context.Background(), "-bad", ccTestToken); err == nil {
		t.Error("잘못된 job ID 는 실행하지 않고 거부")
	}

	t.Setenv("CC_HELPER_MODE", "start_expired")
	err := ccHelperChecker().Start(context.Background(), "J-77", ccTestToken)
	var ce *CrossCheckError
	if !errors.As(err, &ce) || ce.Code != 1 {
		t.Fatalf("exit 1 기대: %v", err)
	}
	if strings.Contains(err.Error(), ccTestToken) || !strings.Contains(err.Error(), "만료됨") || strings.Contains(err.Error(), "두번째") {
		t.Errorf("오류 문구에 토큰이 있거나 사유 한 줄이 아님: %v", err)
	}
}
