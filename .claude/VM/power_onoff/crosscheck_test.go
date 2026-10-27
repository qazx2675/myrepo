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

// fakeFinishCall 은 Finish 호출 기록: 받은 ctx 가 그때 이미 취소돼 있었는지, 제한 시간이 있었는지도 남긴다.
type fakeFinishCall struct {
	job, token, result string
	ctxDone, deadline  bool
}

// fakeCC 는 CrossChecker 가짜: 호출을 기록하고 정해 둔 결과를 돌려준다.
type fakeCC struct {
	mu         sync.Mutex
	runs       []fakeRunCall
	starts     []fakeStartCall
	finishes   []fakeFinishCall
	order      []string
	job, token string
	runErr     error
	startErr   error
	finishErr  error
	block      bool                            // true 면 ctx 가 취소될 때까지 대기
	onStart    func(ctx context.Context) error // nil 이 아니면 Start 가 startErr 대신 이것을 부른다
	errLines   string                          // Run 이 errOut(cross_check 의 stderr)으로 내는 진행 출력(승인 마감 안내 등)
}

func (f *fakeCC) Run(ctx context.Context, user, checker string, targets []string, out, errOut io.Writer) (string, string, error) {
	f.mu.Lock()
	f.runs = append(f.runs, fakeRunCall{user, checker, append([]string(nil), targets...)})
	f.order = append(f.order, "run")
	block, job, token, err, errLines := f.block, f.job, f.token, f.runErr, f.errLines
	f.mu.Unlock()
	io.WriteString(out, "가짜 cross_check 진행 출력\n")
	if errLines != "" {
		io.WriteString(errOut, errLines)
	}
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
	if f.onStart != nil {
		return f.onStart(ctx)
	}
	return f.startErr
}

func (f *fakeCC) Finish(ctx context.Context, jobID, token, result string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, dl := ctx.Deadline()
	f.finishes = append(f.finishes, fakeFinishCall{jobID, token, result, ctx.Err() != nil, dl})
	f.order = append(f.order, "finish:"+result)
	return f.finishErr
}

// finishResults 는 Finish 로 보낸 result 들(순서대로).
func (f *fakeCC) finishResults() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rs []string
	for _, c := range f.finishes {
		rs = append(rs, c.result)
	}
	return strings.Join(rs, ",")
}

func approvedFake() *fakeCC { return &fakeCC{job: "J-100", token: ccTestToken} }

var ccUsers = []string{"lsh", "ljh", "dhk"}

type gateEnv struct {
	g     *CrossCheckGate
	out   *syncBuf
	errw  *syncBuf
	audit *Audit
}

// newGateEnv 는 작업자 user, -c 값 optChecker 로 관문을 만든다(대상은 ccTargets).
func newGateEnv(t *testing.T, cc CrossChecker, user, optChecker string) *gateEnv {
	t.Helper()
	a, err := OpenAudit(t.TempDir(), ActPowerOff)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	e := &gateEnv{out: &syncBuf{}, errw: &syncBuf{}, audit: a}
	e.g = &CrossCheckGate{
		Cfg: &Config{Users: ccUsers}, CC: cc, Out: e.out, ErrOut: e.errw, Audit: a,
		User: user, OptChecker: optChecker, Targets: ccTargets,
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

// guard 는 관문을 건 RunGuard 를 문자열 입력으로 돌린다(1단계 입력은 WorkPhrase).
func (e *gateEnv) guard(t *testing.T, ctx context.Context, input string) error {
	t.Helper()
	o := testOpts()
	e.g.Hook(&o)
	return RunGuard(ctx, NewLineReader(strings.NewReader(input)), e.out, ccTargets, o)
}

var ccTargets = []string{"svr1ev01", "svr1ev02", "svr2ev01"}

// 3단계(Approve) ----------------------------------------------------------

func TestCrossCheckGateApprove(t *testing.T) {
	f := approvedFake()
	e := newGateEnv(t, f, "lsh", "dhk")
	if err := e.g.Approve(context.Background(), "dhk"); err != nil {
		t.Fatalf("승인 경로는 nil 이어야 함: %v", err)
	}
	if len(f.runs) != 1 || f.runs[0].user != "lsh" || f.runs[0].checker != "dhk" ||
		strings.Join(f.runs[0].targets, " ") != "svr1ev01 svr1ev02 svr2ev01" {
		t.Errorf("Run 호출 이상: %+v", f.runs)
	}
	if len(f.starts) != 0 {
		t.Errorf("3단계에서는 --start 를 부르지 않아야 함: %+v", f.starts)
	}
	out := e.out.String()
	if !strings.Contains(out, crossCheckNotice) {
		t.Errorf("승인 유효시간 정책 안내가 없음:\n%s", out)
	}
	if strings.Index(out, crossCheckNotice) > strings.Index(out, "가짜 cross_check 진행 출력") {
		t.Errorf("정책 안내는 승인 대기(진행 출력)보다 먼저여야 함:\n%s", out)
	}
	if !strings.Contains(out, "교차검증 승인됨") || !strings.Contains(out, "4/5 YeS 입력과 5/5 카운트다운") {
		t.Errorf("승인 안내 이상:\n%s", out)
	}
	// 그다음 Start(카운트다운 직후)가 같은 job/token 으로 한 번
	if err := e.g.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(f.starts) != 1 || f.starts[0].job != "J-100" || f.starts[0].token != ccTestToken {
		t.Errorf("Start 호출 이상: %+v", f.starts)
	}
	if strings.Join(f.order, ",") != "run,start" {
		t.Errorf("호출 순서: %v", f.order)
	}
	// token 은 일회용: 두 번째 Start 는 cross_check 를 부르지 않고 거부
	if err := e.g.Start(context.Background()); err == nil || len(f.starts) != 1 {
		t.Errorf("두 번째 Start 는 거부되어야 함: %v %+v", err, f.starts)
	}
	au := e.auditText(t)
	for _, w := range []string{"교차검증 요청 작업자=lsh 확인자=dhk", "교차검증 승인 확인자=dhk job=J-100", "교차검증 시작 허용 확인자=dhk job=J-100"} {
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
		e := newGateEnv(t, f, "lsh", "dhk")
		if err := e.g.Approve(context.Background(), "dhk"); err == nil {
			t.Fatalf("exit %d: 오류여야 함", tc.code)
		}
		if err := e.g.Start(context.Background()); err == nil || len(f.starts) != 0 {
			t.Errorf("exit %d: 미승인인데 Start 가 통과/호출됨", tc.code)
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
	e := newGateEnv(t, f, "lsh", "dhk")
	if err := e.g.Approve(context.Background(), "dhk"); err == nil {
		t.Errorf("일반 오류도 미승인이어야 함")
	}
}

func TestCrossCheckGateBadApproval(t *testing.T) {
	for name, f := range map[string]*fakeCC{
		"토큰 없음":   {job: "J-1"},
		"job 없음":  {token: ccTestToken},
		"job 하이픈": {job: "-rf", token: ccTestToken},
		"job 공백":  {job: "a b", token: ccTestToken},
	} {
		e := newGateEnv(t, f, "lsh", "dhk")
		if err := e.g.Approve(context.Background(), "dhk"); err == nil {
			t.Errorf("%s: 오류여야 함", name)
		}
		e.g.Start(context.Background())
		if len(f.starts) != 0 {
			t.Errorf("%s: Start 호출됨", name)
		}
		e.noToken(t)
	}
}

func TestCrossCheckGateStartRefused(t *testing.T) {
	f := approvedFake()
	f.startErr = &CrossCheckError{Code: 1, Msg: "--start 종료 코드 1: [거부] 승인 후 2분 내 미진행 " + ccTestToken}
	e := newGateEnv(t, f, "lsh", "dhk")
	if err := e.g.Approve(context.Background(), "dhk"); err != nil {
		t.Fatal(err)
	}
	if err := e.g.Start(context.Background()); err == nil {
		t.Fatal("시작 거부는 오류여야 함")
	}
	if !strings.Contains(e.out.String(), "사유: --start 종료 코드 1: [거부] 승인 후 2분 내 미진행 ***") {
		t.Errorf("거부 사유(토큰 가림)가 화면에 없음:\n%s", e.out.String())
	}
	au := e.auditText(t)
	if !strings.Contains(au, "교차검증 승인 확인자=dhk job=J-100") || !strings.Contains(au, "교차검증 시작 거부 확인자=dhk job=J-100") || strings.Contains(au, "시작 허용") {
		t.Errorf("감사 로그 이상:\n%s", au)
	}
	e.noToken(t)
}

// 2단계(SelectChecker) ----------------------------------------------------

// selectChecker 는 SelectChecker 를 문자열 입력으로 돌린다.
func selectChecker(e *gateEnv, ctx context.Context, input string) (string, error) {
	return e.g.SelectChecker(ctx, NewLineReader(strings.NewReader(input)), e.out)
}

func TestCrossCheckSelectCheckerMenu(t *testing.T) {
	// 작업자 ljh → 메뉴는 lsh, dhk (본인 제외, conf 순서). 2번 = dhk
	e := newGateEnv(t, approvedFake(), "ljh", "")
	c, err := selectChecker(e, context.Background(), "2\n")
	if err != nil || c != "dhk" {
		t.Fatalf("2번은 dhk: %q %v", c, err)
	}
	menu := e.out.String()
	if !strings.Contains(menu, "[2/5] 확인자를 선택하세요") || !strings.Contains(menu, "  1) lsh\n  2) dhk\n") || strings.Contains(menu, ") ljh") {
		t.Errorf("메뉴에 본인이 있거나 목록 이상:\n%s", menu)
	}
	if !strings.Contains(e.auditText(t), "교차검증 확인자 선택 확인자=dhk 방법=번호") {
		t.Errorf("감사 로그에 확인자 선택 없음:\n%s", e.auditText(t))
	}

	// 앞뒤 공백 허용, 1번 = lsh
	e = newGateEnv(t, approvedFake(), "ljh", "")
	if c, err := selectChecker(e, context.Background(), " 1 \n"); err != nil || c != "lsh" {
		t.Errorf("1번은 lsh: %q %v", c, err)
	}

	// 두 번 틀리고 세 번째에 맞으면 통과(남은 기회 안내)
	e = newGateEnv(t, approvedFake(), "ljh", "")
	if c, err := selectChecker(e, context.Background(), "x\n\n2\n"); err != nil || c != "dhk" {
		t.Errorf("재입력 후 통과해야 함: %q %v\n%s", c, err, e.out.String())
	}
	if out := e.out.String(); !strings.Contains(out, "남은 기회 2번") || !strings.Contains(out, "남은 기회 1번") {
		t.Errorf("재입력 안내 없음:\n%s", out)
	}
}

func TestCrossCheckSelectCheckerInvalid(t *testing.T) {
	// 3번 모두 틀리면 취소 (범위 밖·0·숫자 아님·빈 줄)
	for _, in := range []string{"3\n0\nx\n", "\n\n\n", "-1\n9\nlsh\n"} {
		e := newGateEnv(t, approvedFake(), "ljh", "")
		c, err := selectChecker(e, context.Background(), in+"1\n")
		if err == nil || c != "" || err.Error() != "확인자 선택 실패 (잘못된 입력 3회)" {
			t.Errorf("입력 %q: 3회 틀리면 취소여야 함: %q %v", in, c, err)
		}
		if !strings.Contains(e.auditText(t), "교차검증 확인자 선택 실패: 잘못된 입력 3회") {
			t.Errorf("입력 %q: 감사 로그 없음", in)
		}
	}
	// 입력 끝(EOF)
	e := newGateEnv(t, approvedFake(), "ljh", "")
	if _, err := selectChecker(e, context.Background(), ""); err == nil || err.Error() != "입력 종료(EOF)" {
		t.Errorf("EOF: %v", err)
	}
}

func TestCrossCheckSelectCheckerInterrupt(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	e := newGateEnv(t, approvedFake(), "ljh", "")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	c, err := e.g.SelectChecker(ctx, NewLineReader(pr), e.out)
	if err == nil || c != "" || ctx.Err() == nil {
		t.Errorf("Ctrl+C 는 오류: %q %v", c, err)
	}
}

func TestCrossCheckSelectCheckerPreset(t *testing.T) {
	// -c 로 미리 선택: 메뉴 없이 입력도 읽지 않는다
	e := newGateEnv(t, approvedFake(), "lsh", "dhk")
	in := NewLineReader(strings.NewReader("남은 입력\n"))
	c, err := e.g.SelectChecker(context.Background(), in, e.out)
	if err != nil || c != "dhk" {
		t.Fatalf("-c dhk: %q %v", c, err)
	}
	if out := e.out.String(); !strings.Contains(out, "[2/5] 확인자: dhk (-c)") || strings.Contains(out, "번호 입력") {
		t.Errorf("-c 출력 이상:\n%s", out)
	}
	if s, _ := in.ReadLine(context.Background()); s != "남은 입력" {
		t.Errorf("-c 인데 입력을 읽음: %q", s)
	}
	if !strings.Contains(e.auditText(t), "교차검증 확인자 선택 확인자=dhk 방법=-c") {
		t.Errorf("감사 로그 없음:\n%s", e.auditText(t))
	}
	// 잘못된 -c (users 에 없음 / 본인 / 대소문자)
	for _, bad := range []string{"zzz", "lsh", "DHK"} {
		e := newGateEnv(t, approvedFake(), "lsh", bad)
		if _, err := selectChecker(e, context.Background(), "1\n"); err == nil {
			t.Errorf("-c %s: 오류여야 함", bad)
		}
	}
	cfg := &Config{Users: ccUsers}
	if ValidateChecker(cfg, "lsh", "ljh") != nil || ValidateChecker(cfg, "lsh", "lsh") == nil || ValidateChecker(cfg, "lsh", "x") == nil {
		t.Error("ValidateChecker 이상")
	}
}

func TestCrossCheckSelectCheckerFewUsers(t *testing.T) {
	// 본인 외 1명: 메뉴는 1개, 1 을 고른다
	e := newGateEnv(t, approvedFake(), "lsh", "")
	e.g.Cfg = &Config{Users: []string{"lsh", "ljh"}}
	if c, err := selectChecker(e, context.Background(), "1\n"); err != nil || c != "ljh" {
		t.Errorf("한 명뿐: %q %v", c, err)
	}
	if out := e.out.String(); !strings.Contains(out, "  1) ljh\n") || strings.Contains(out, "2)") {
		t.Errorf("메뉴 이상:\n%s", out)
	}
	// 본인뿐: 분명한 오류
	e = newGateEnv(t, approvedFake(), "lsh", "")
	e.g.Cfg = &Config{Users: []string{"lsh"}}
	c, err := selectChecker(e, context.Background(), "1\n")
	if err == nil || c != "" || !strings.Contains(err.Error(), "선택할 user 없음") {
		t.Errorf("본인뿐이면 오류: %q %v", c, err)
	}
	if !strings.Contains(e.out.String(), "[오류] 확인자로 선택할 user 가 없습니다") {
		t.Errorf("오류 안내 없음:\n%s", e.out.String())
	}
	if _, err := checkerCandidates(&Config{Users: []string{"lsh"}}, "lsh"); err == nil || !strings.Contains(err.Error(), "cross_check=off") {
		t.Errorf("checkerCandidates 오류 안내: %v", err)
	}
}

// 관문을 건 RunGuard 전체 흐름 ------------------------------------------------

func TestCrossCheckGuardFlow(t *testing.T) {
	// 작업 확인 문구 → 확인자 2번(dhk) → 승인 → ls 두 번 → YeS → 카운트다운 → --start
	f := approvedFake()
	e := newGateEnv(t, f, "ljh", "")
	if err := e.guard(t, context.Background(), WorkPhrase+"\n2\nls\n LS \nYeS\n"); err != nil {
		t.Fatalf("통과해야 함: %v\n%s", err, e.out.String())
	}
	out := e.out.String()
	if len(f.runs) != 1 || f.runs[0].checker != "dhk" || len(f.starts) != 1 || strings.Join(f.order, ",") != "run,start" {
		t.Errorf("호출 이상: runs=%+v starts=%+v order=%v", f.runs, f.starts, f.order)
	}
	if strings.Count(out, "svr1ev01\nsvr1ev02\nsvr2ev01\n총 3대") != 3 {
		t.Errorf("1단계 + ls 2회 = 목록 3번이어야 함:\n%s", out)
	}
	for _, w := range []string{"[2/5] 확인자를 선택하세요", "[3/5] 교차검증 — 확인자 dhk", "(YeS/No, ls=대상 보기): ", "10 .. 9", "교차검증 통과 — 전원 작업을 시작합니다"} {
		if !strings.Contains(out, w) {
			t.Errorf("출력에 %q 없음:\n%s", w, out)
		}
	}
	for _, w := range []string{"1분(60초)", "호스트명", "[취소]"} {
		if strings.Contains(out, w) {
			t.Errorf("교차검증 방식인데 %q 가 나옴:\n%s", w, out)
		}
	}
	// 순서: 승인 → 4단계 → 카운트다운 → 시작 허용
	i1, i2, i3, i4 := strings.Index(out, "교차검증 승인됨"), strings.Index(out, "[4/5]"), strings.Index(out, "[5/5]"), strings.Index(out, "교차검증 통과")
	if !(i1 < i2 && i2 < i3 && i3 < i4) {
		t.Errorf("순서 이상 %d %d %d %d:\n%s", i1, i2, i3, i4, out)
	}
	au := e.auditText(t)
	for _, w := range []string{"확인자 선택 확인자=dhk", "교차검증 승인 확인자=dhk", "교차검증 시작 허용"} {
		if !strings.Contains(au, w) {
			t.Errorf("감사 로그에 %q 없음:\n%s", w, au)
		}
	}
	e.noToken(t)
}

func TestCrossCheckGuardCancels(t *testing.T) {
	// 확인자 미승인 → [취소] 교차검증 미승인, 4·5단계·--start 없음
	f := approvedFake()
	f.runErr = &CrossCheckError{Code: 1, Msg: "확인자 취소"}
	e := newGateEnv(t, f, "lsh", "dhk")
	err := e.guard(t, context.Background(), WorkPhrase+"\nYeS\n")
	wantCancel(t, err, e.out.String(), "교차검증 미승인")
	if strings.Contains(e.out.String(), "[4/5]") || len(f.starts) != 0 {
		t.Errorf("미승인인데 4단계/--start 진행:\n%s", e.out.String())
	}

	// 승인 후 4단계에서 yes → 취소, --start 안 부름 (승인은 유효시간이 지나면 데몬이 자동 취소)
	f = approvedFake()
	e = newGateEnv(t, f, "lsh", "dhk")
	err = e.guard(t, context.Background(), WorkPhrase+"\nls\nyes\n")
	wantCancel(t, err, e.out.String(), "YeS 미입력 (입력값: yes)")
	if len(f.starts) != 0 || strings.Contains(e.out.String(), "[5/5]") {
		t.Errorf("yes 인데 진행:\n%s", e.out.String())
	}

	// 교차검증 방식에서는 VM 이름을 묻지 않는다: 이름 줄은 4단계 답으로 읽혀 취소
	f = approvedFake()
	e = newGateEnv(t, f, "lsh", "dhk")
	err = e.guard(t, context.Background(), WorkPhrase+"\nsvr1ev01 svr1ev02 svr2ev01\nYeS\n")
	wantCancel(t, err, e.out.String(), "YeS 미입력 (입력값: svr1ev01 svr1ev02 svr2ev01)")

	// 카운트다운 뒤 --start 거부(승인 유효시간 초과) → 취소
	f = approvedFake()
	f.startErr = &CrossCheckError{Code: 1, Msg: "승인 유효시간 초과"}
	e = newGateEnv(t, f, "lsh", "dhk")
	err = e.guard(t, context.Background(), WorkPhrase+"\nYeS\n")
	wantCancel(t, err, e.out.String(), "교차검증 승인 유효시간 초과 또는 거부", "10 .. 9 .. 8")
	if len(f.starts) != 1 || strings.Contains(e.out.String(), "교차검증 통과") {
		t.Errorf("--start 거부 처리 이상: %+v\n%s", f.starts, e.out.String())
	}

	// 확인자 3회 잘못 입력 → 취소, cross_check 호출 없음
	f = approvedFake()
	e = newGateEnv(t, f, "ljh", "")
	err = e.guard(t, context.Background(), WorkPhrase+"\n9\n9\n9\nYeS\n")
	wantCancel(t, err, e.out.String(), "확인자 선택 실패 (잘못된 입력 3회)")
	if len(f.runs) != 0 {
		t.Error("확인자 선택 실패인데 cross_check 호출됨")
	}

	// 본인뿐인 conf → 취소
	f = approvedFake()
	e = newGateEnv(t, f, "lsh", "")
	e.g.Cfg = &Config{Users: []string{"lsh"}}
	err = e.guard(t, context.Background(), WorkPhrase+"\n1\nYeS\n")
	wantCancel(t, err, e.out.String(), "확인자 선택 실패 (선택할 user 없음)")
	if len(f.runs) != 0 {
		t.Error("확인자 없음인데 cross_check 호출됨")
	}
}

func TestCrossCheckGuardInterrupt(t *testing.T) {
	// 승인 대기 중 Ctrl+C → [취소] 교차검증 미승인 (Ctrl+C 분류), --start 없음
	f := approvedFake()
	f.block = true
	e := newGateEnv(t, f, "lsh", "dhk")
	pr, pw := io.Pipe()
	defer pw.Close()
	go io.WriteString(pw, WorkPhrase+"\n")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	o := testOpts()
	e.g.Hook(&o)
	done := make(chan error, 1)
	go func() { done <- RunGuard(ctx, NewLineReader(pr), e.out, ccTargets, o) }()
	select {
	case err := <-done:
		wantCancel(t, err, e.out.String(), "교차검증 미승인")
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 취소 후에도 반환하지 않음")
	}
	if len(f.starts) != 0 || !strings.Contains(e.auditText(t), "Ctrl+C(exit 130)") {
		t.Errorf("Ctrl+C 처리 이상: starts=%d\n%s", len(f.starts), e.auditText(t))
	}

	// 확인자 메뉴에서 Ctrl+C → [취소] Ctrl+C 로 중단
	f = approvedFake()
	e = newGateEnv(t, f, "ljh", "")
	pr2, pw2 := io.Pipe()
	defer pw2.Close()
	go io.WriteString(pw2, WorkPhrase+"\n")
	ctx2, cancel2 := context.WithCancel(context.Background())
	time.AfterFunc(80*time.Millisecond, cancel2)
	o2 := testOpts()
	e.g.Hook(&o2)
	err := RunGuard(ctx2, NewLineReader(pr2), e.out, ccTargets, o2)
	wantCancel(t, err, e.out.String(), "Ctrl+C 로 중단")
	if len(f.runs) != 0 {
		t.Error("메뉴에서 Ctrl+C 인데 cross_check 호출됨")
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
	// 기본 conf(키 없음)는 on 이므로 실제 실행에는 관문이 필요하다
	if !crossCheckNeeded(defaultConfig(), false) || crossCheckNeeded(defaultConfig(), true) {
		t.Error("기본값(on): 실제 실행에서만 교차검증")
	}
}

func TestCheckCrossCheckBin(t *testing.T) {
	dir := t.TempDir()
	none := filepath.Join(dir, "none")
	if err := CheckCrossCheckBin(none); err == nil {
		t.Error("없는 파일은 오류")
	} else if want := "cross_check 가 설치되어 있지 않습니다: " + none + " — bash setup.sh 로 설치하거나 pwonoff.conf 에 cross_check=off 를 지정하세요"; err.Error() != want {
		t.Errorf("안내 문구 다름:\n got %q\nwant %q", err.Error(), want)
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
		if err := CheckCrossCheckBin(q); err == nil || !strings.Contains(err.Error(), "실행 권한 없음") || !strings.Contains(err.Error(), "cross_check=off") {
			t.Errorf("실행 권한이 없으면 오류(끄는 방법 안내): %v", err)
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
	return &execCrossChecker{Bin: os.Args[0], Caller: "vm_power_onoff", pre: []string{"-test.run=^TestCCHelperProcess$", "--"}}
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
	case "finish":
		a := strings.Join(args, " ")
		if (a == "--finish --job J-77 --result done" || a == "--finish --job J-77 --result aborted") && os.Getenv("CROSS_CHECK_TOKEN") == ccTestToken {
			os.Stderr.WriteString("[완료] 기록됨\n")
			os.Exit(0)
		}
		os.Stdout.WriteString("환경/인자 불일치: " + a + "\n")
		os.Exit(1)
	case "finish_refused":
		os.Stderr.WriteString("[거부] 결과 기록 불가 token=" + os.Getenv("CROSS_CHECK_TOKEN") + "\n두번째 줄\n")
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
	if !strings.Contains(out.String(), "args=--run --user lsh --checker dhk --caller vm_power_onoff stdin=svr1ev01,svr1ev02,svr2ev01") {
		t.Errorf("인자/표준입력 이상(--caller 포함): %q", out.String())
	}
	// Caller 가 비면 --caller 를 넘기지 않는다(cross_check 가 스스로 감지)
	var out2 syncBuf
	c := ccHelperChecker()
	c.Caller = ""
	if _, _, err := c.Run(context.Background(), "lsh", "dhk", ccTargets, &out2, io.Discard); err != nil || strings.Contains(out2.String(), "--caller") {
		t.Errorf("Caller 없음: %v %q", err, out2.String())
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

// 결과 보고(--finish) ------------------------------------------------------
// main.run 과 같은 순서: RunGuard 가 취소면 gate.Abort(), 통과면 executeRun(Runner → ReportRun) → reportResults.

// waitFor 는 출력에 s 가 나올 때까지 기다린다.
func waitFor(t *testing.T, b *syncBuf, s string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(b.String(), s) {
		if time.Now().After(deadline) {
			t.Fatalf("%q 를 기다리다 시간 초과:\n%s", s, b.String())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// wantFinish 는 Finish 가 정확히 한 번 result 로, 승인 받은 job/token 과 취소되지 않은 새 ctx(제한 시간 있음)로 불렸는지 본다.
func wantFinish(t *testing.T, f *fakeCC, e *gateEnv, result string) {
	t.Helper()
	if got := f.finishResults(); got != result {
		t.Fatalf("Finish 결과 %q 기대, 실제 %q (order=%v)\n%s", result, got, f.order, e.out.String())
	}
	c := f.finishes[0]
	if c.job != "J-100" || c.token != ccTestToken {
		t.Errorf("Finish job/token 이상: %+v", c)
	}
	if c.ctxDone || !c.deadline {
		t.Errorf("Finish 는 취소되지 않은 새 ctx(제한 시간 있음)로 불러야 함: %+v", c)
	}
	if !strings.Contains(e.auditText(t), "교차검증 결과 보고: "+result+" job=J-100") {
		t.Errorf("감사 로그에 결과 보고 없음:\n%s", e.auditText(t))
	}
	if !strings.Contains(e.out.String(), "[INFO] 교차검증 결과 보고: "+ccResultLabel(result)+" (작업 J-100)") {
		t.Errorf("화면에 결과 보고 없음:\n%s", e.out.String())
	}
	// token 은 보고 뒤 지워진다: 다시 불러도 cross_check 를 부르지 않는다
	e.g.Abort()
	e.g.ReportRun(context.Background())
	if len(f.finishes) != 1 {
		t.Errorf("보고는 한 번만: %+v", f.finishes)
	}
	e.noToken(t)
}

func TestCrossCheckFinishAbortAfterApproval(t *testing.T) {
	// 승인 뒤 4/5 에서 YeS 가 아니면(No·빈 입력·소문자·EOF) 승인/작업중단(aborted), --start 는 없음
	for in, reason := range map[string]string{
		WorkPhrase + "\nNo\n":        "YeS 미입력 (입력값: No)",
		WorkPhrase + "\n\n":          "YeS 미입력 (입력값: 빈 입력)",
		WorkPhrase + "\nls\nyes\n":   "YeS 미입력 (입력값: yes)",
		WorkPhrase + "\n":            "YeS 미입력 (입력값: EOF)",
		WorkPhrase + "\nls\nls\nN\n": "YeS 미입력 (입력값: N)",
	} {
		f := approvedFake()
		e := newGateEnv(t, f, "lsh", "dhk")
		err := e.guard(t, context.Background(), in)
		wantCancel(t, err, e.out.String(), reason)
		e.g.Abort()
		if len(f.starts) != 0 {
			t.Errorf("%q: --start 를 부르면 안 됨", in)
		}
		wantFinish(t, f, e, "aborted")
	}
}

func TestCrossCheckFinishNotSentWithoutApproval(t *testing.T) {
	// 미승인(거부·Ctrl+C·오류) → 보고 없음
	f := approvedFake()
	f.runErr = &CrossCheckError{Code: 1, Msg: "확인자 취소"}
	e := newGateEnv(t, f, "lsh", "dhk")
	wantCancel(t, e.guard(t, context.Background(), WorkPhrase+"\nYeS\n"), e.out.String(), "교차검증 미승인")
	e.g.Abort()
	e.g.ReportRun(context.Background())
	if len(f.finishes) != 0 {
		t.Errorf("미승인인데 Finish 호출: %+v", f.finishes)
	}
	// 확인자 선택 실패 → cross_check 를 전혀 부르지 않음
	f = approvedFake()
	e = newGateEnv(t, f, "ljh", "")
	wantCancel(t, e.guard(t, context.Background(), WorkPhrase+"\n9\n9\n9\n"), e.out.String(), "확인자 선택 실패 (잘못된 입력 3회)")
	e.g.Abort()
	if len(f.order) != 0 {
		t.Errorf("cross_check 호출됨: %v", f.order)
	}
}

func TestCrossCheckFinishNotSentWhenStartRefused(t *testing.T) {
	// --start 거부/만료: 데몬이 이미 중단으로 기록 → --finish 를 보내지 않는다
	f := approvedFake()
	f.startErr = &CrossCheckError{Code: 1, Msg: "--start 종료 코드 1: [거부] 승인 유효시간 초과 " + ccTestToken}
	e := newGateEnv(t, f, "lsh", "dhk")
	err := e.guard(t, context.Background(), WorkPhrase+"\nYeS\n")
	wantCancel(t, err, e.out.String(), "교차검증 승인 유효시간 초과 또는 거부")
	e.g.Abort()
	e.g.ReportRun(context.Background())
	if len(f.finishes) != 0 || strings.Join(f.order, ",") != "run,start" {
		t.Errorf("--start 거부 뒤 Finish 호출: %v", f.order)
	}
	e.noToken(t)
}

func TestCrossCheckFinishCtrlCAtConfirm(t *testing.T) {
	// 4/5 질문에서 Ctrl+C → 승인/작업중단 (취소된 ctx 가 아니라 새 ctx 로 보고)
	f := approvedFake()
	e := newGateEnv(t, f, "lsh", "dhk")
	pr, pw := io.Pipe()
	defer pw.Close()
	go io.WriteString(pw, WorkPhrase+"\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := testOpts()
	e.g.Hook(&o)
	done := make(chan error, 1)
	go func() { done <- RunGuard(ctx, NewLineReader(pr), e.out, ccTargets, o) }()
	waitFor(t, e.out, "[4/5]")
	cancel()
	err := <-done
	wantCancel(t, err, e.out.String(), "Ctrl+C 로 중단")
	e.g.Abort()
	if len(f.starts) != 0 {
		t.Error("4/5 Ctrl+C 인데 --start 호출")
	}
	wantFinish(t, f, e, "aborted")
}

func TestCrossCheckFinishCtrlCInCountdown(t *testing.T) {
	// 카운트다운 중 Ctrl+C → 승인/작업중단, --start 없음
	f := approvedFake()
	e := newGateEnv(t, f, "lsh", "dhk")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o := testOpts()
	o.Tick = 50 * time.Millisecond
	e.g.Hook(&o)
	done := make(chan error, 1)
	go func() {
		done <- RunGuard(ctx, NewLineReader(strings.NewReader(WorkPhrase+"\nYeS\n")), e.out, ccTargets, o)
	}()
	waitFor(t, e.out, "10 .. 9")
	cancel()
	err := <-done
	wantCancel(t, err, e.out.String(), "Ctrl+C 로 중단")
	e.g.Abort()
	if len(f.starts) != 0 {
		t.Error("카운트다운 Ctrl+C 인데 --start 호출")
	}
	wantFinish(t, f, e, "aborted")
}

func TestCrossCheckFinishCtrlCDuringStart(t *testing.T) {
	// --start 도중 Ctrl+C: 시작 기록 여부를 알 수 없으므로 작업중단을 보고한다(거부와 달리)
	f := approvedFake()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onStart = func(context.Context) error {
		cancel()
		return &CrossCheckError{Code: ccExitInterrupt, Msg: "--start 중 Ctrl+C"}
	}
	e := newGateEnv(t, f, "lsh", "dhk")
	err := e.guard(t, ctx, WorkPhrase+"\nYeS\n")
	wantCancel(t, err, e.out.String(), "교차검증 승인 유효시간 초과 또는 거부")
	e.g.Abort()
	wantFinish(t, f, e, "aborted")
}

// cancelOnOps 는 PowerOn 이 불리면 ctx 를 취소한다(본 작업 중 Ctrl+C 흉내).
type cancelOnOps struct {
	*fakeOps
	cancel context.CancelFunc
}

func (c cancelOnOps) PowerOn(ctx context.Context, vm *VMInfo) error {
	c.cancel()
	return c.fakeOps.PowerOn(ctx, vm)
}

// passGuard 는 승인 → YeS → 카운트다운 → --start 까지 통과시킨다.
func passGuard(t *testing.T, e *gateEnv, ctx context.Context) {
	t.Helper()
	if err := e.guard(t, ctx, WorkPhrase+"\nYeS\n"); err != nil {
		t.Fatalf("통과해야 함: %v\n%s", err, e.out.String())
	}
}

func TestCrossCheckFinishDoneEvenWithFailedVMs(t *testing.T) {
	// 끝까지 실행되면 일부 VM 이 실패해도 승인/작업완료(done). 종료 코드는 실패 VM 때문에 ExitFail
	f := approvedFake()
	e := newGateEnv(t, f, "lsh", "dhk")
	passGuard(t, e, context.Background())
	ops := newFake()
	ops.onErr["svr1ev02"] = errors.New("boom")
	r, _, _ := testRunner(ops)
	vms := append(vmsOf("poweredOff", true, "svr1ev01", "svr1ev02"), vmsOf("suspended", false, "svr2ev01")...)
	results := executeRun(context.Background(), r, ActPowerOnAll, vms, e.g)
	if statusOf(results, "svr1ev02").Status != StFail || statusOf(results, "svr2ev01").Status != StFail || statusOf(results, "svr1ev01").Status != StOK {
		t.Fatalf("실패 VM 이 있어야 하는 시험: %+v", results)
	}
	if strings.Join(f.order, ",") != "run,start,finish:done" {
		t.Errorf("호출 순서: %v", f.order)
	}
	wantFinish(t, f, e, "done")
	if code := reportResults(context.Background(), e.out, e.audit, ActPowerOnAll, results, time.Now()); code != ExitFail {
		t.Errorf("실패 VM 이 있으면 ExitFail: %d", code)
	}
}

func TestCrossCheckFinishAbortedOnCtrlCInRunner(t *testing.T) {
	// 본 작업 중 Ctrl+C(ctx 취소) → 승인/작업중단(aborted), 보고는 새 ctx 로
	f := approvedFake()
	e := newGateEnv(t, f, "lsh", "dhk")
	passGuard(t, e, context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ops := newFake()
	r, _, _ := testRunner(ops)
	r.Ops = cancelOnOps{ops, cancel}
	results := executeRun(ctx, r, ActPowerOnAll, vmsOf("poweredOff", false, "svr1ev01", "svr1ev02"), e.g)
	if ctx.Err() == nil {
		t.Fatal("시험 설정: ctx 가 취소돼야 함")
	}
	wantFinish(t, f, e, "aborted")
	if code := reportResults(ctx, e.out, e.audit, ActPowerOnAll, results, time.Now()); code != ExitFail {
		t.Errorf("Ctrl+C 면 ExitFail: %d", code)
	}
}

func TestCrossCheckFinishErrorKeepsExitCode(t *testing.T) {
	// 결과 보고가 실패해도 [경고]·감사 로그만 남고 종료 코드는 그대로(전부 성공 → ExitOK)
	for _, ferr := range []error{
		&CrossCheckError{Code: 1, Msg: "--finish 종료 코드 1: [거부] 결과 기록 불가 " + ccTestToken},
		&CrossCheckError{Code: ccExitError, Msg: "--finish 제한 시간 초과"},
		errors.New("exec 실패"),
	} {
		f := approvedFake()
		f.finishErr = ferr
		e := newGateEnv(t, f, "lsh", "dhk")
		passGuard(t, e, context.Background())
		ops := newFake()
		r, _, _ := testRunner(ops)
		results := executeRun(context.Background(), r, ActPowerOnAll, vmsOf("poweredOff", false, "svr1ev01", "svr1ev02"), e.g)
		if code := reportResults(context.Background(), e.out, e.audit, ActPowerOnAll, results, time.Now()); code != ExitOK {
			t.Errorf("%v: 보고 실패가 종료 코드를 바꿈: %d\n%s", ferr, code, e.out.String())
		}
		out, au := e.out.String(), e.auditText(t)
		if !strings.Contains(out, "[경고] 교차검증 결과(done)를 cross_check 데몬에 알리지 못했습니다 (작업 J-100") || strings.Contains(out, "[INFO] 교차검증 결과 보고") {
			t.Errorf("%v: 경고 이상:\n%s", ferr, out)
		}
		if !strings.Contains(au, "교차검증 결과 보고 실패: done job=J-100 분류=") || strings.Contains(au, "교차검증 결과 보고: done") {
			t.Errorf("%v: 감사 로그 이상:\n%s", ferr, au)
		}
		if !strings.Contains(out, "[완료] ") {
			t.Errorf("%v: 작업 완료 표시가 없어짐:\n%s", ferr, out)
		}
		// 실패했어도 token 은 지워져 다시 보내지 않는다
		e.g.Abort()
		if len(f.finishes) != 1 {
			t.Errorf("%v: 보고 재시도됨: %+v", ferr, f.finishes)
		}
		e.noToken(t)
	}
	f := approvedFake()
	f.finishErr = &CrossCheckError{Code: 1, Msg: "x " + ccTestToken}
	e := newGateEnv(t, f, "lsh", "dhk")
	passGuard(t, e, context.Background())
	e.g.ReportRun(context.Background())
	if !strings.Contains(e.out.String(), "— x ***") || !strings.Contains(e.out.String(), "거부/중단(exit 1)") {
		t.Errorf("거부 사유(토큰 가림)가 경고에 없음:\n%s", e.out.String())
	}
}

func TestCrossCheckFinishWithoutStartRefusedNoToken(t *testing.T) {
	// Approve 전·Start 거부 뒤의 Finish 는 nil(아무것도 안 함)
	f := approvedFake()
	e := newGateEnv(t, f, "lsh", "dhk")
	if err := e.g.Finish(ccResultDone); err != nil || len(f.finishes) != 0 {
		t.Errorf("승인 전 Finish: %v %+v", err, f.finishes)
	}
}

func TestExecCrossCheckerFinish(t *testing.T) {
	t.Setenv("CC_HELPER_MODE", "finish")
	for _, r := range []string{"done", "aborted"} {
		if err := ccHelperChecker().Finish(context.Background(), "J-77", ccTestToken, r); err != nil {
			t.Errorf("%s: 인자·자식 환경 token 이 맞아야 함: %v", r, err)
		}
	}
	if os.Getenv("CROSS_CHECK_TOKEN") != "" {
		t.Error("부모 환경에 토큰이 남음")
	}
	if err := ccHelperChecker().Finish(context.Background(), "J-77", "다른토큰", "done"); err == nil {
		t.Error("토큰이 다르면 거부")
	}
	for _, bad := range [][2]string{{"-bad", "done"}, {"J-77", "ok"}, {"J-77", ""}, {"J-77", "--admin"}} {
		var ce *CrossCheckError
		if err := ccHelperChecker().Finish(context.Background(), bad[0], ccTestToken, bad[1]); !errors.As(err, &ce) || ce.Code != ccExitError {
			t.Errorf("%v: 실행하지 않고 거부해야 함: %v", bad, err)
		}
	}

	t.Setenv("CC_HELPER_MODE", "finish_refused")
	err := ccHelperChecker().Finish(context.Background(), "J-77", ccTestToken, "done")
	var ce *CrossCheckError
	if !errors.As(err, &ce) || ce.Code != 1 {
		t.Fatalf("exit 1 기대: %v", err)
	}
	if strings.Contains(err.Error(), ccTestToken) || !strings.Contains(err.Error(), "--finish 종료 코드 1: [거부] 결과 기록 불가 token=***") || strings.Contains(err.Error(), "두번째") {
		t.Errorf("오류 문구에 토큰이 있거나 사유 한 줄이 아님: %v", err)
	}

	// 제한 시간: 응답이 없으면 제한 시간 초과(Ctrl+C 아님)
	t.Setenv("CC_HELPER_MODE", "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	err = ccHelperChecker().Finish(ctx, "J-77", ccTestToken, "aborted")
	if !errors.As(err, &ce) || ce.Code != ccExitError || !strings.Contains(ce.Msg, "제한 시간 초과") {
		t.Errorf("제한 시간 초과 기대: %v", err)
	}
	if time.Since(t0) > 15*time.Second {
		t.Errorf("제한 시간 뒤 너무 오래 걸림: %v", time.Since(t0))
	}
}

func TestCrossCheckCallerName(t *testing.T) {
	// --caller 는 고정 문자열이 아니라 실제 실행 파일 이름(os.Executable 의 basename)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got := selfName(); got != filepath.Base(exe) || got == "" || strings.ContainsAny(got, `/\`) {
		t.Errorf("selfName=%q, 실행 파일 %q", got, exe)
	}
	c, ok := newCrossChecker("/usr/local/bin/cross_check").(*execCrossChecker)
	if !ok || c.Bin != "/usr/local/bin/cross_check" || c.Caller != filepath.Base(exe) {
		t.Errorf("newCrossChecker: %+v", c)
	}
}

// 입력 제한 시간(계속 대기 금지) -------------------------------------------------

// deadlineLine 은 cross_check --run 이 승인 직후 stderr 에 내는 마감 안내 줄(시각은 초 단위로 잘림)이다.
func deadlineLine(t time.Time) string {
	return "  승인 유효시간: 2분 — " + t.Format("15:04:05") + " 까지 아래 --start 를 호출해야 합니다\n"
}

func TestCCDeadlineSniffer(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 0, 0, 0, time.Local)
	var w strings.Builder
	sn := &ccDeadlineSniffer{w: &w}
	// 조각난 쓰기·색 코드가 섞여도 줄 단위로 찾고, 원래 출력은 그대로 넘긴다
	full := "[승인] 작업 J — other 가 승인했습니다\n\x1b[1m정책\x1b[0m\n  승인 유효시간: \x1b[33m2분\x1b[0m — 13:02:00 까지 아래 --start 를 호출해야 합니다\n  본 작업 직전에: …\n"
	for i := 0; i < len(full); i += 7 {
		end := i + 7
		if end > len(full) {
			end = len(full)
		}
		sn.Write([]byte(full[i:end]))
	}
	if w.String() != full {
		t.Fatalf("출력이 그대로 넘어가지 않음:\n%q", w.String())
	}
	if dl, ok := sn.deadline(now); !ok || !dl.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("마감 13:02:00 기대: %v %v", dl, ok)
	}
	// 자정 넘김: 23:59:30 에 승인, 마감 00:01:30 → 다음 날
	late := time.Date(2026, 10, 26, 23, 59, 30, 0, time.Local)
	sn = &ccDeadlineSniffer{}
	sn.Write([]byte("  승인 유효시간: 2분 — 00:01:30 까지 아래 --start 를 호출해야 합니다")) // 줄바꿈 없이 끝나도 읽는다
	if dl, ok := sn.deadline(late); !ok || !dl.Equal(late.Add(2*time.Minute)) {
		t.Fatalf("자정 넘김: %v %v", dl, ok)
	}
	// 안내가 없거나, 시각이 이상하거나, 허용 범위(1시간)를 크게 벗어나면 모름(→ input_timeout_sec)
	for _, s := range []string{"", "승인 유효시간 없음\n", "  승인 유효시간: 2분 — 25:00:00 까지\n", "  승인 유효시간: 9시간 — 22:00:00 까지\n"} {
		sn = &ccDeadlineSniffer{}
		sn.Write([]byte(s))
		if dl, ok := sn.deadline(now); ok {
			t.Errorf("%q: 모름이어야 함: %v", s, dl)
		}
	}
}

// 2/5 확인자 번호를 input_timeout_sec 안에 입력하지 않으면 취소(사유·감사 로그).
func TestCrossCheckSelectCheckerTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	e := newGateEnv(t, approvedFake(), "ljh", "")
	e.g.InputTimeout = 60 * time.Millisecond
	start := time.Now()
	c, err := e.g.SelectChecker(context.Background(), NewLineReader(pr), e.out)
	if err == nil || c != "" || err.Error() != "입력 시간 60ms 초과" {
		t.Fatalf("시간 초과 취소 기대: %q %v", c, err)
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("계속 기다림: %v", el)
	}
	if !strings.Contains(e.auditText(t), "교차검증 확인자 선택 실패: 입력 시간 60ms 초과") {
		t.Errorf("감사 로그에 사유 없음:\n%s", e.auditText(t))
	}
}

// guardPipe 는 관문을 건 RunGuard 를 파이프 입력(first 를 넣은 뒤 열어 둠)으로 돌린다.
func (e *gateEnv) guardPipe(t *testing.T, first string, step time.Duration) (error, time.Duration) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	go io.WriteString(pw, first)
	o := testOpts()
	o.StepTimeout = step
	e.g.Hook(&o)
	start := time.Now()
	err := RunGuard(context.Background(), NewLineReader(pr), e.out, ccTargets, o)
	return err, time.Since(start)
}

// 승인 뒤 4/5 에서 승인 마감까지 YeS 가 없으면 "[취소] 승인 유효시간 초과" → Abort 가 aborted(승인/작업중단)를 보고.
// ls 를 해도 마감은 늘어나지 않는다. --start 는 부르지 않는다.
func TestCrossCheckConfirmDeadline(t *testing.T) {
	f := approvedFake()
	f.errLines = deadlineLine(time.Now().Add(3 * time.Second)) // 4/5 마감 = 이 시각(초 단위) - 1초 → 1~2초 뒤
	e := newGateEnv(t, f, "lsh", "dhk")
	e.g.InputTimeout = time.Hour
	err, el := e.guardPipe(t, WorkPhrase+"\nls\n", time.Hour)
	wantCancel(t, err, e.out.String(), "승인 유효시간 초과")
	if el < 900*time.Millisecond || el > 3*time.Second {
		t.Fatalf("승인 마감(1~2초)에 끊겨야 함: %v", el)
	}
	out := e.out.String()
	if !strings.Contains(out, "[INFO] 4/5 입력 마감: ") || strings.Count(out, "총 3대") != 2 {
		t.Errorf("마감 안내 또는 ls 목록 이상:\n%s", out)
	}
	if !strings.Contains(e.errw.String(), "승인 유효시간: 2분 — ") {
		t.Errorf("cross_check 진행 출력이 화면(stderr)으로 그대로 가지 않음:\n%s", e.errw.String())
	}
	if !strings.Contains(e.auditText(t), "교차검증 승인 마감 ") {
		t.Errorf("감사 로그에 승인 마감 없음:\n%s", e.auditText(t))
	}
	e.g.Abort() // main.go: RunGuard 취소 → gate.Abort()
	if len(f.starts) != 0 {
		t.Errorf("--start 를 부르면 안 됨: %+v", f.starts)
	}
	wantFinish(t, f, e, "aborted")
}

// 마감 시각을 모르면(안내 줄 없음) 4/5 는 input_timeout_sec 로 끊는다.
func TestCrossCheckConfirmNoDeadlineUsesInputTimeout(t *testing.T) {
	f := approvedFake()
	e := newGateEnv(t, f, "lsh", "dhk")
	err, el := e.guardPipe(t, WorkPhrase+"\n", 80*time.Millisecond)
	wantCancel(t, err, e.out.String(), "입력 시간 80ms 초과")
	if el > 2*time.Second {
		t.Fatalf("계속 기다림: %v", el)
	}
	if !strings.Contains(e.auditText(t), "교차검증 승인 마감 시각을 알 수 없음") {
		t.Errorf("감사 로그에 마감 모름 기록 없음:\n%s", e.auditText(t))
	}
	e.g.Abort()
	wantFinish(t, f, e, "aborted")
}

// 3/5 승인 대기 중 데몬이 작업을 중단 처리하면(cross_check --run exit 1) 작업자 쪽도 곧바로 "[취소] 교차검증 미승인".
func TestCrossCheckDaemonAbortsWaiting(t *testing.T) {
	f := &fakeCC{runErr: &CrossCheckError{Code: ccExitRefused, Msg: "--run 종료 코드 1"}}
	e := newGateEnv(t, f, "lsh", "dhk")
	err, el := e.guardPipe(t, WorkPhrase+"\n", time.Hour)
	wantCancel(t, err, e.out.String(), "교차검증 미승인")
	if el > time.Second || strings.Contains(e.out.String(), "[4/5]") {
		t.Fatalf("미승인인데 진행/대기: %v\n%s", el, e.out.String())
	}
	if !strings.Contains(e.auditText(t), "교차검증 미승인 확인자=dhk 분류=거부/중단(exit 1)") {
		t.Errorf("감사 로그:\n%s", e.auditText(t))
	}
	e.g.Abort()
	if len(f.finishes) != 0 {
		t.Errorf("승인 전이면 --finish 를 보내지 않음: %+v", f.finishes)
	}
}
