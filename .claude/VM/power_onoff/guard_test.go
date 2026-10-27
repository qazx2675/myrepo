package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	os.Setenv("PWONOFF_COLOR", "never")
	os.Exit(m.Run())
}

// syncBuf 는 고루틴에서 함께 써도 안전한 출력 버퍼.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var tTargets = []string{"svr1ev01", "svr1ev02", "svr2ev01"}

func testOpts() GuardOpts {
	return GuardOpts{
		InputTimeout: 2 * time.Second,
		Tick:         time.Millisecond,
	}
}

// runGuardStr 는 문자열 입력으로 RunGuard 를 돌린다.
func runGuardStr(t *testing.T, input string, opts GuardOpts) (error, string) {
	t.Helper()
	out := &syncBuf{}
	err := RunGuard(context.Background(), NewLineReader(strings.NewReader(input)), out, tTargets, opts)
	return err, out.String()
}

func wantCancel(t *testing.T, err error, out, reason string, more ...string) {
	t.Helper()
	var ge *GuardError
	if !errors.As(err, &ge) {
		t.Fatalf("GuardError 기대, got %v\n%s", err, out)
	}
	if ge.Reason != reason {
		t.Fatalf("사유 %q 기대, got %q\n%s", reason, ge.Reason, out)
	}
	if !strings.Contains(out, "[취소] "+reason) {
		t.Fatalf("화면에 [취소] %s 없음\n%s", reason, out)
	}
	for _, m := range more {
		if !strings.Contains(out, m) {
			t.Fatalf("화면에 %q 없음\n%s", m, out)
		}
	}
}

const okHosts = "svr1ev01\nsvr1ev02\nsvr2ev01\n"

// promptYeS 는 4단계 질문(두 방식 공통).
const promptYeS = "정말 작업을 진행하시겠습니까? (YeS/No, ls=대상 보기): "

func TestGuardPass(t *testing.T) {
	cases := map[string]string{
		"개행":    "svr1ev01\nsvr1ev02\nsvr2ev01\n",
		"공백":    "svr1ev01 svr1ev02 svr2ev01\n",
		"탭혼합":   "svr1ev01\t svr1ev02\nsvr2ev01\n",
		"순서바꿈":  "svr2ev01 svr1ev01\nsvr1ev02\n",
		"CRLF":  "svr1ev01\r\nsvr1ev02 svr2ev01\r\n",
		"빈줄종료":  "\nsvr1ev01 svr1ev02 svr2ev01\n",
		"앞빈줄무시": "\n\nsvr1ev01\nsvr1ev02\nsvr2ev01\n",
	}
	for name, hosts := range cases {
		t.Run(name, func(t *testing.T) {
			err, out := runGuardStr(t, WorkPhrase+"\n\n"+hosts+"YeS\n", testOpts())
			if err != nil {
				t.Fatalf("통과 기대: %v\n%s", err, out)
			}
			for _, m := range []string{"svr1ev01\nsvr1ev02\nsvr2ev01\n총 3대", "1분(60초)의 입력 제한 시간", promptYeS} {
				if !strings.Contains(out, m) {
					t.Fatalf("출력에 %q 없음\n%s", m, out)
				}
			}
		})
	}
}

func TestGuardStep1Mismatch(t *testing.T) {
	// 한글 원문·대소문자 다름·옛 랜덤 코드 형태는 모두 불일치
	for _, in := range []string{"재부팅작업", "WOQNXLDWKRDJQ", "woqnxldwkrdjq(재부팅작업)", "ABC123", "woqnxldwkrdj"} {
		err, out := runGuardStr(t, in+"\n\n"+okHosts+"YeS\n", testOpts())
		wantCancel(t, err, out, reasonWorkPhrase)
		if strings.Contains(out, "총 3대") {
			t.Fatalf("%q: 불일치인데 대상 목록 출력", in)
		}
	}
	err, out := runGuardStr(t, "", testOpts())
	wantCancel(t, err, out, reasonWorkPhrase)
}

func TestGuardStep1Phrase(t *testing.T) {
	// 안내문은 고정 문구 + 괄호 안 한글, 앞뒤 공백은 무시
	err, out := runGuardStr(t, " \t"+WorkPhrase+"  \n\n"+okHosts+"YeS\n", testOpts())
	if err != nil {
		t.Fatalf("앞뒤 공백은 무시해야 함: %v\n%s", err, out)
	}
	for _, m := range []string{"[1/5] woqnxldwkrdjq(재부팅작업)을 그대로 입력하세요.\n > ", "[INFO] 작업 확인 문구 일치"} {
		if !strings.Contains(out, m) {
			t.Fatalf("출력에 %q 없음\n%s", m, out)
		}
	}
	if strings.Contains(out, "확인 코드") {
		t.Fatalf("'확인 코드' 표현이 남음\n%s", out)
	}
}

func TestGuardShowTargets(t *testing.T) {
	o := testOpts()
	o.ShowTargets = func(w io.Writer) { io.WriteString(w, "BM svr1: ...\n") }
	err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+"YeS\n", o)
	if err != nil || !strings.Contains(out, "BM svr1: ...\n총 3대") {
		t.Fatalf("ShowTargets 미사용: %v\n%s", err, out)
	}
}

func TestGuardStep3Mismatch(t *testing.T) {
	cases := []struct {
		name, hosts string
		more        []string
	}{
		{"누락", "svr1ev01 svr1ev02\n\n", []string{"누락: svr2ev01"}},
		{"누락EOF", "svr1ev01\n", []string{"누락: svr1ev02, svr2ev01"}},
		{"초과", "svr1ev01 svr1ev02 svr2ev01 svr3ev01\n", []string{"대상 아님: svr3ev01"}},
		{"다른이름", "svr1ev01 svr1ev02 svr9ev01\n", []string{"누락: svr2ev01", "대상 아님: svr9ev01"}},
		{"중복", "svr1ev01 svr1ev01 svr1ev02 svr2ev01\n", []string{"중복: svr1ev01(2회)"}},
		{"중복으로개수채움", "svr1ev01 svr1ev01 svr1ev02\n", []string{"중복: svr1ev01(2회)", "누락: svr2ev01"}},
		{"대소문자", "SVR1EV01 svr1ev02 svr2ev01\n", []string{"누락: svr1ev01", "대상 아님: SVR1EV01"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err, out := runGuardStr(t, WorkPhrase+"\n\n"+c.hosts+"YeS\n", testOpts())
			wantCancel(t, err, out, "호스트 목록 불일치", c.more...)
			if strings.Contains(out, "YeS/No") {
				t.Fatal("불일치인데 4단계 진행")
			}
		})
	}
}

func TestGuardStep3RemainTime(t *testing.T) {
	err, out := runGuardStr(t, WorkPhrase+"\n\nsvr1ev01\nsvr1ev02\nsvr2ev01\nYeS\n", testOpts())
	if err != nil || !strings.Contains(out, "(남은 시간 ") {
		t.Fatalf("남은 시간 표시 없음: %v\n%s", err, out)
	}
}

func TestGuardStep3Timeout(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() {
		io.WriteString(pw, WorkPhrase+"\n\nsvr1ev01\n")
		time.Sleep(80 * time.Millisecond)
		io.WriteString(pw, "svr1ev02\n") // 줄마다 타이머가 새로 시작되지 않는지(총 시간) 확인
	}()
	o := testOpts()
	o.InputTimeout = 150 * time.Millisecond
	out := &syncBuf{}
	start := time.Now()
	err := RunGuard(context.Background(), NewLineReader(pr), out, tTargets, o)
	wantCancel(t, err, out.String(), "입력 시간 150ms 초과")
	if el := time.Since(start); el > 400*time.Millisecond {
		t.Fatalf("총 시간 제한이 아님: %v", el)
	}
}

func TestSecs(t *testing.T) {
	if secs(60*time.Second) != "60초" || secs(2*time.Second) != "2초" {
		t.Fatal(secs(60 * time.Second))
	}
}

func TestGuardStep4(t *testing.T) {
	cases := map[string]string{
		"yes\n": "yes", "YES\n": "YES", "y\n": "y", "No\n": "No", "\n": "빈 입력", "": "EOF",
	}
	for in, shown := range cases {
		t.Run(shown, func(t *testing.T) {
			err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+in, testOpts())
			wantCancel(t, err, out, "YeS 미입력 (입력값: "+shown+")")
			if strings.Contains(out, "Ctrl+C 로 취소 가능") {
				t.Fatal("취소인데 5단계 진행")
			}
		})
	}
}

func TestGuardCountdownLine(t *testing.T) {
	err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+"YeS\n", testOpts())
	if err != nil {
		t.Fatal(err)
	}
	want := "(Ctrl+C 로 취소 가능)\n10 .. 9 .. 8 .. 7 .. 6 .. 5 .. 4 .. 3 .. 2 .. 1\n"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("카운트다운 형식 다름:\n%q", out)
	}
}

// ctx 취소: 입력을 stage 까지 넣고 cancelAfter 뒤 취소.
func runCancel(t *testing.T, input string, tick, cancelAfter time.Duration) (error, string) {
	t.Helper()
	pr, pw := io.Pipe()
	defer pw.Close()
	go io.WriteString(pw, input)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(cancelAfter, cancel)
	o := testOpts()
	o.Tick = tick
	out := &syncBuf{}
	start := time.Now()
	err := RunGuard(ctx, NewLineReader(pr), out, tTargets, o)
	if el := time.Since(start); el > cancelAfter+300*time.Millisecond {
		t.Fatalf("취소가 즉시 반영되지 않음: %v", el)
	}
	return err, out.String()
}

func TestGuardCancel(t *testing.T) {
	cases := map[string]string{
		"1단계": "",
		"2단계": WorkPhrase + "\n",
		"3단계": WorkPhrase + "\n\nsvr1ev01\n",
		"4단계": WorkPhrase + "\n\n" + okHosts,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			err, out := runCancel(t, in, time.Millisecond, 50*time.Millisecond)
			wantCancel(t, err, out, "Ctrl+C 로 중단")
		})
	}
	t.Run("5단계", func(t *testing.T) {
		err, out := runCancel(t, WorkPhrase+"\n\n"+okHosts+"YeS\n", 40*time.Millisecond, 130*time.Millisecond)
		wantCancel(t, err, out, "Ctrl+C 로 중단", "10 .. 9")
		if strings.Contains(out, " .. 1\n") {
			t.Fatal("카운트다운이 끝까지 감")
		}
		if !strings.Contains(out, "\n[취소] Ctrl+C 로 중단") {
			t.Fatalf("줄바꿈 후 취소 출력 아님\n%q", out)
		}
	})
}

// 4단계 ls: 대상 목록과 대수를 다시 보여 주고 취소하지 않고 다시 묻는다(기존 방식).
func TestGuardStep4Ls(t *testing.T) {
	for _, ls := range []string{"ls", "LS", " ls ", "Ls\t"} {
		t.Run(ls, func(t *testing.T) {
			n := 0
			o := testOpts()
			o.OnList = func() { n++ }
			err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+ls+"\n"+ls+"\nYeS\n", o)
			if err != nil {
				t.Fatalf("ls 뒤 YeS 는 통과: %v\n%s", err, out)
			}
			if c := strings.Count(out, "작업 대상 호스트 목록:\nsvr1ev01\nsvr1ev02\nsvr2ev01\n총 3대\n"); c != 3 {
				t.Errorf("1단계 + ls 2회 = 목록 3번이어야 함 (%d)\n%s", c, out)
			}
			if c := strings.Count(out, promptYeS); c != 3 || n != 2 {
				t.Errorf("질문 %d번, OnList %d번 (3, 2 기대)\n%s", c, n, out)
			}
			if !strings.HasSuffix(out, "10 .. 9 .. 8 .. 7 .. 6 .. 5 .. 4 .. 3 .. 2 .. 1\n") || strings.Contains(out, "[취소]") {
				t.Errorf("카운트다운까지 가야 함\n%s", out)
			}
		})
	}
	// ShowTargets(실제 실행의 BM별 목록)를 ls 에서도 그대로 쓴다
	o := testOpts()
	o.ShowTargets = func(w io.Writer) { io.WriteString(w, "BM 묶음 목록\n") }
	err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+"ls\nYeS\n", o)
	if err != nil || strings.Count(out, "BM 묶음 목록\n총 3대") != 2 {
		t.Errorf("ls 가 ShowTargets 를 쓰지 않음: %v\n%s", err, out)
	}
	// ls 뒤에도 YeS 가 아니면 취소, ls 와 비슷한 말은 ls 가 아님
	for in, shown := range map[string]string{"ls\nyes\n": "yes", "ls\n\n": "빈 입력", "ls\n": "EOF", "ls -l\n": "ls -l", "lss\n": "lss"} {
		err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+in, testOpts())
		wantCancel(t, err, out, "YeS 미입력 (입력값: "+shown+")")
		if strings.Contains(out, "Ctrl+C 로 취소 가능") {
			t.Fatalf("%q: 취소인데 5단계 진행", in)
		}
	}
}

// 기존 방식에서 AfterCountdown 은 카운트다운 뒤에 한 번 불리고, 오류면 취소한다.
func TestGuardAfterCountdown(t *testing.T) {
	calls := 0
	o := testOpts()
	o.AfterCountdown = func(context.Context) error { calls++; return nil }
	if err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+"YeS\n", o); err != nil || calls != 1 {
		t.Fatalf("통과 + 1회 호출: %v %d\n%s", err, calls, out)
	}
	o.AfterCountdown = func(context.Context) error { calls++; return errors.New("거부") }
	err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+"YeS\n", o)
	wantCancel(t, err, out, "교차검증 승인 유효시간 초과 또는 거부", " .. 1\n")
	err, out = runGuardStr(t, WorkPhrase+"\n\n"+okHosts+"No\n", o)
	wantCancel(t, err, out, "YeS 미입력 (입력값: No)")
	if calls != 2 {
		t.Errorf("4단계 취소 때는 부르지 않아야 함: %d", calls)
	}
}

// ccHooks 는 교차검증 방식(Checker/CrossCheck/AfterCountdown)을 흉내 낸다.
type ccHooks struct {
	checker                    string
	checkerErr, ccErr, failAft error
	calls                      []string
}

func (h *ccHooks) opts() GuardOpts {
	o := testOpts()
	o.Checker = func(ctx context.Context, in *LineReader, out io.Writer) (string, error) {
		h.calls = append(h.calls, "checker")
		fmt.Fprintln(out, "[2/5] 가짜 확인자 메뉴")
		return h.checker, h.checkerErr
	}
	o.CrossCheck = func(ctx context.Context, c string) error {
		h.calls = append(h.calls, "approve:"+c)
		return h.ccErr
	}
	o.AfterCountdown = func(context.Context) error {
		h.calls = append(h.calls, "start")
		return h.failAft
	}
	return o
}

func TestGuardCrossCheckMode(t *testing.T) {
	h := &ccHooks{checker: "dhk"}
	err, out := runGuardStr(t, WorkPhrase+"\nls\nYeS\n", h.opts())
	if err != nil {
		t.Fatalf("통과해야 함: %v\n%s", err, out)
	}
	if strings.Join(h.calls, ",") != "checker,approve:dhk,start" {
		t.Errorf("호출 순서: %v", h.calls)
	}
	for _, w := range []string{"[2/5] 가짜 확인자 메뉴", "[3/5] 교차검증 — 확인자 dhk 의 승인을 받습니다", promptYeS, "10 .. 9"} {
		if !strings.Contains(out, w) {
			t.Errorf("출력에 %q 없음:\n%s", w, out)
		}
	}
	if strings.Contains(out, "1분(60초)") || strings.Contains(out, "호스트명") || strings.Count(out, "총 3대") != 2 {
		t.Errorf("교차검증 방식인데 이름 입력 단계가 나오거나 ls 목록 이상:\n%s", out)
	}

	// 확인자 선택 실패 → 그 사유로 취소, 승인 요청 없음
	h = &ccHooks{checkerErr: errors.New("확인자 선택 실패 (잘못된 입력 3회)")}
	err, out = runGuardStr(t, WorkPhrase+"\nYeS\n", h.opts())
	wantCancel(t, err, out, "확인자 선택 실패 (잘못된 입력 3회)")
	if strings.Join(h.calls, ",") != "checker" {
		t.Errorf("호출: %v", h.calls)
	}

	// 미승인 → [취소] 교차검증 미승인, 4단계 없음
	h = &ccHooks{checker: "dhk", ccErr: errors.New("미승인")}
	err, out = runGuardStr(t, WorkPhrase+"\nYeS\n", h.opts())
	wantCancel(t, err, out, "교차검증 미승인")
	if strings.Contains(out, "[4/5]") || strings.Join(h.calls, ",") != "checker,approve:dhk" {
		t.Errorf("미승인인데 진행: %v\n%s", h.calls, out)
	}

	// 승인 후 yes → 취소, --start 없음
	h = &ccHooks{checker: "dhk"}
	err, out = runGuardStr(t, WorkPhrase+"\nyes\n", h.opts())
	wantCancel(t, err, out, "YeS 미입력 (입력값: yes)")
	if strings.Join(h.calls, ",") != "checker,approve:dhk" {
		t.Errorf("yes 인데 --start: %v", h.calls)
	}

	// 카운트다운 뒤 --start 거부 → 취소
	h = &ccHooks{checker: "dhk", failAft: errors.New("만료")}
	err, out = runGuardStr(t, WorkPhrase+"\nYeS\n", h.opts())
	wantCancel(t, err, out, "교차검증 승인 유효시간 초과 또는 거부")

	// Checker 나 CrossCheck 하나만 있으면 기존 방식 그대로
	o := (&ccHooks{checker: "dhk"}).opts()
	o.CrossCheck = nil
	o.AfterCountdown = nil
	if err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+"YeS\n", o); err != nil || !strings.Contains(out, "1분(60초)") {
		t.Errorf("하나만 있으면 기존 방식: %v\n%s", err, out)
	}
}

func TestConfirmPhrase(t *testing.T) {
	out := &syncBuf{}
	in := NewLineReader(strings.NewReader("  " + VWPPhrase + " \n" + strings.ToLower(VWPPhrase) + "\n"))
	if err := ConfirmPhrase(context.Background(), in, out, "VWP 확인: ", VWPPhrase, 0); err != nil {
		t.Fatal(err)
	}
	err := ConfirmPhrase(context.Background(), in, out, "VWP 확인: ", VWPPhrase, 0)
	wantCancel(t, err, out.String(), reasonVWPPhrase)
	err = ConfirmPhrase(context.Background(), in, out, "VWP 확인: ", VWPPhrase, 0) // EOF
	wantCancel(t, err, out.String(), reasonVWPPhrase)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pr, pw := io.Pipe()
	defer pw.Close()
	out2 := &syncBuf{}
	err = ConfirmPhrase(ctx, NewLineReader(pr), out2, "VWP 확인: ", VWPPhrase, 0)
	wantCancel(t, err, out2.String(), "Ctrl+C 로 중단")
}

func TestRunDemo(t *testing.T) {
	t.Setenv("PWONOFF_TEST_TICK_MS", "1")
	out := &syncBuf{}
	rc := RunDemo(context.Background(), NewLineReader(strings.NewReader(WorkPhrase+"\n\nwas-prd-01 web-prd-02 web-prd-01\nYeS\n")), out)
	if rc != 0 || !strings.Contains(out.String(), "[DEMO] 실제 전원 작업은 하지 않습니다") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if !strings.Contains(out.String(), "cross_check=on(기본)이면 2/5 는 확인자 번호 선택, 3/5 는 VM 이름 입력 대신 확인자 승인 대기") {
		t.Errorf("데모에 교차검증 방식 안내 없음:\n%s", out)
	}
	// 데모도 4단계 ls 지원 (데모 대상 3대)
	out = &syncBuf{}
	rc = RunDemo(context.Background(), NewLineReader(strings.NewReader(WorkPhrase+"\n\nwas-prd-01 web-prd-02 web-prd-01\nls\nYeS\n")), out)
	if rc != 0 || strings.Count(out.String(), "web-prd-01\nweb-prd-02\nwas-prd-01\n총 3대") != 2 {
		t.Fatalf("데모 ls: rc=%d\n%s", rc, out)
	}
	out = &syncBuf{}
	rc = RunDemo(context.Background(), NewLineReader(strings.NewReader(WorkPhrase+"\n\nwas-prd-01 web-prd-02 web-prd-01\nyes\n")), out)
	if rc != 1 || strings.Contains(out.String(), "실제 전원 작업은") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
}

func TestLineReaderTimeoutNoLoss(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	lr := NewLineReader(pr)
	ctx := context.Background()
	if _, err := lr.ReadLineTimeout(ctx, 20*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("ErrTimeout 기대: %v", err)
	}
	go io.WriteString(pw, "hello\r\nworld\n")
	if s, err := lr.ReadLine(ctx); err != nil || s != "hello" {
		t.Fatalf("got %q %v", s, err)
	}
	// 줄이 이미 들어온 뒤 시간 초과로 포기해도 다음 ReadLine 이 받는다
	time.Sleep(20 * time.Millisecond)
	if s, err := lr.ReadLine(ctx); err != nil || s != "world" {
		t.Fatalf("got %q %v", s, err)
	}
	pw.Close()
	if _, err := lr.ReadLine(ctx); err != io.EOF {
		t.Fatalf("EOF 기대: %v", err)
	}
	if _, err := lr.ReadLine(ctx); err != io.EOF {
		t.Fatalf("EOF 반복 기대: %v", err)
	}
}

func TestLineReaderLastLineNoNewline(t *testing.T) {
	lr := NewLineReader(strings.NewReader("a\nb"))
	ctx := context.Background()
	for _, w := range []string{"a", "b"} {
		if s, err := lr.ReadLine(ctx); err != nil || s != w {
			t.Fatalf("got %q %v", s, err)
		}
	}
	if _, err := lr.ReadLine(ctx); err != io.EOF {
		t.Fatal(err)
	}
}

func TestColorDisabled(t *testing.T) {
	if Red("x") != "x" {
		t.Fatal("PWONOFF_COLOR=never 인데 색상 적용")
	}
	t.Setenv("PWONOFF_COLOR", "always")
	if Red("x") == "x" {
		t.Fatal("always 인데 색상 없음")
	}
}

// 입력 제한 시간(계속 대기 금지) -------------------------------------------------

// runGuardPipe 는 first 를 넣은 뒤 입력을 열어 둔 채(사용자가 자리를 비운 상태) RunGuard 를 돌린다.
func runGuardPipe(t *testing.T, first string, opts GuardOpts) (error, string, time.Duration) {
	t.Helper()
	pr, pw := io.Pipe()
	defer pw.Close()
	go io.WriteString(pw, first)
	out := &syncBuf{}
	start := time.Now()
	err := RunGuard(context.Background(), NewLineReader(pr), out, tTargets, opts)
	return err, out.String(), time.Since(start)
}

func TestGuardStepTimeout(t *testing.T) {
	cases := map[string]struct {
		first, notIn string
	}{
		"1단계":        {"", "총 3대"},
		"2단계(Enter)": {WorkPhrase + "\n", "[3/5]"},
		"4단계":        {WorkPhrase + "\n\n" + okHosts, "[5/5]"},
		"4단계 ls 뒤":   {WorkPhrase + "\n\n" + okHosts + "ls\n", "[5/5]"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			o := testOpts()
			o.StepTimeout = 80 * time.Millisecond
			err, out, el := runGuardPipe(t, c.first, o)
			wantCancel(t, err, out, "입력 시간 80ms 초과")
			if el > 2*time.Second {
				t.Fatalf("계속 기다림: %v", el)
			}
			if strings.Contains(out, c.notIn) {
				t.Fatalf("시간 초과인데 다음 단계(%s) 진행:\n%s", c.notIn, out)
			}
			if !strings.Contains(out, "\n[취소] 입력 시간 80ms 초과") {
				t.Fatalf("프롬프트 줄을 끝내고 취소 출력해야 함:\n%q", out)
			}
		})
	}
	// 질문마다 따로 잰다: 각 단계가 제한보다 짧으면 합이 길어도 통과
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() {
		for _, s := range []string{WorkPhrase + "\n", "\n", okHosts, "ls\n", "YeS\n"} {
			time.Sleep(60 * time.Millisecond)
			io.WriteString(pw, s)
		}
	}()
	o := testOpts()
	o.StepTimeout = 150 * time.Millisecond
	out := &syncBuf{}
	if err := RunGuard(context.Background(), NewLineReader(pr), out, tTargets, o); err != nil {
		t.Fatalf("질문마다 제한 안이면 통과: %v\n%s", err, out)
	}
}

// 교차검증 방식 4단계: 마감 시각까지만 기다리고(ls 로 늘어나지 않음) 넘기면 "승인 유효시간 초과", 모르면 StepTimeout.
func TestGuardConfirmDeadline(t *testing.T) {
	h := &ccHooks{checker: "dhk"}
	o := h.opts()
	o.StepTimeout = time.Hour
	dl := time.Now().Add(200 * time.Millisecond)
	o.ConfirmDeadline = func() time.Time { return dl }
	err, out, el := runGuardPipe(t, WorkPhrase+"\nls\n", o)
	wantCancel(t, err, out, "승인 유효시간 초과")
	if el > time.Second || strings.Contains(out, "[5/5]") || strings.Count(out, "총 3대") != 2 {
		t.Fatalf("마감에 끊겨야 함(ls 1회 포함): %v\n%s", el, out)
	}
	if strings.Join(h.calls, ",") != "checker,approve:dhk" {
		t.Errorf("--start 를 부르면 안 됨: %v", h.calls)
	}
	// 이미 지난 마감 → 바로 취소(이미 온 YeS 보다 시간 초과가 우선)
	h = &ccHooks{checker: "dhk"}
	o = h.opts()
	o.ConfirmDeadline = func() time.Time { return time.Now().Add(-time.Second) }
	err, out = runGuardStr(t, WorkPhrase+"\nYeS\n", o)
	wantCancel(t, err, out, "승인 유효시간 초과")
	// 마감 모름(0) → StepTimeout
	h = &ccHooks{checker: "dhk"}
	o = h.opts()
	o.StepTimeout = 80 * time.Millisecond
	o.ConfirmDeadline = func() time.Time { return time.Time{} }
	err, out, _ = runGuardPipe(t, WorkPhrase+"\n", o)
	wantCancel(t, err, out, "입력 시간 80ms 초과")
	// 마감 안이면 그대로 통과
	h = &ccHooks{checker: "dhk"}
	o = h.opts()
	o.ConfirmDeadline = func() time.Time { return time.Now().Add(time.Minute) }
	if err, out := runGuardStr(t, WorkPhrase+"\nYeS\n", o); err != nil {
		t.Fatalf("마감 전 YeS 는 통과: %v\n%s", err, out)
	}
	// 기존 방식에서는 ConfirmDeadline 을 쓰지 않는다
	o = testOpts()
	o.ConfirmDeadline = func() time.Time { return time.Now().Add(-time.Second) }
	if err, out := runGuardStr(t, WorkPhrase+"\n\n"+okHosts+"YeS\n", o); err != nil {
		t.Fatalf("기존 방식은 마감 무시: %v\n%s", err, out)
	}
}

func TestConfirmPhraseTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	out := &syncBuf{}
	start := time.Now()
	err := ConfirmPhrase(context.Background(), NewLineReader(pr), out, "VWP 확인: ", VWPPhrase, 80*time.Millisecond)
	wantCancel(t, err, out.String(), "입력 시간 80ms 초과")
	if time.Since(start) > time.Second {
		t.Fatal("계속 기다림")
	}
}

func TestDurText(t *testing.T) {
	for d, w := range map[time.Duration]string{
		600 * time.Second: "10분", time.Minute: "1분", 90 * time.Second: "90초", 30 * time.Second: "30초", 3600 * time.Second: "60분",
	} {
		if got := durText(d); got != w {
			t.Errorf("%v: %q 기대, 실제 %q", d, w, got)
		}
	}
	if inputTimeoutReason(600*time.Second) != "입력 시간 10분 초과" {
		t.Error(inputTimeoutReason(600 * time.Second))
	}
	if (GuardOpts{}).withDefaults().StepTimeout != DefaultInputTimeout {
		t.Error("StepTimeout 기본값은 10분")
	}
}

// 데모: PWONOFF_TEST_STEP_TIMEOUT(초)로 질문마다의 제한을 줄여 시험한다(실제 실행에서는 읽지 않음).
func TestRunDemoStepTimeout(t *testing.T) {
	t.Setenv("PWONOFF_TEST_STEP_TIMEOUT", "1")
	pr, pw := io.Pipe()
	defer pw.Close()
	go io.WriteString(pw, WorkPhrase+"\n")
	out := &syncBuf{}
	start := time.Now()
	rc := RunDemo(context.Background(), NewLineReader(pr), out)
	if rc != ExitFail || !strings.Contains(out.String(), "[취소] 입력 시간 1초 초과") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("계속 기다림: %v", el)
	}
}
