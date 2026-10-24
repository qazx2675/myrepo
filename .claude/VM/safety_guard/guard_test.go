package safetyguard

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	os.Setenv("SAFETYGUARD_COLOR", "never")
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
		NewCode:      func() string { return "ABC123" },
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

func TestNewCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		c := NewCode()
		if len(c) != 6 {
			t.Fatalf("길이 %d: %q", len(c), c)
		}
		for _, r := range c {
			if !strings.ContainsRune(codeChars, r) {
				t.Fatalf("문자셋 밖: %q", c)
			}
		}
		seen[c] = true
	}
	if len(seen) < 490 {
		t.Fatalf("코드가 너무 자주 겹침: %d", len(seen))
	}
}

const okHosts = "svr1ev01\nsvr1ev02\nsvr2ev01\n"

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
			err, out := runGuardStr(t, "ABC123\n\n"+hosts+"YeS\n", testOpts())
			if err != nil {
				t.Fatalf("통과 기대: %v\n%s", err, out)
			}
			for _, m := range []string{"svr1ev01\nsvr1ev02\nsvr2ev01\n총 3대", "1분(60초)의 입력 제한 시간", "정말 작업을 진행하시겠습니까? (YeS/No): "} {
				if !strings.Contains(out, m) {
					t.Fatalf("출력에 %q 없음\n%s", m, out)
				}
			}
		})
	}
}

func TestGuardStep1Mismatch(t *testing.T) {
	err, out := runGuardStr(t, "abc123\n\n"+okHosts+"YeS\n", testOpts())
	wantCancel(t, err, out, "확인 코드 불일치")
	if strings.Contains(out, "총 3대") {
		t.Fatal("불일치인데 대상 목록 출력")
	}
	err, out = runGuardStr(t, "", testOpts())
	wantCancel(t, err, out, "확인 코드 불일치")
}

func TestGuardShowTargets(t *testing.T) {
	o := testOpts()
	o.ShowTargets = func(w io.Writer) { io.WriteString(w, "BM svr1: ...\n") }
	err, out := runGuardStr(t, "ABC123\n\n"+okHosts+"YeS\n", o)
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
			err, out := runGuardStr(t, "ABC123\n\n"+c.hosts+"YeS\n", testOpts())
			wantCancel(t, err, out, "호스트 목록 불일치", c.more...)
			if strings.Contains(out, "YeS/No") {
				t.Fatal("불일치인데 4단계 진행")
			}
		})
	}
}

func TestGuardStep3RemainTime(t *testing.T) {
	err, out := runGuardStr(t, "ABC123\n\nsvr1ev01\nsvr1ev02\nsvr2ev01\nYeS\n", testOpts())
	if err != nil || !strings.Contains(out, "(남은 시간 ") {
		t.Fatalf("남은 시간 표시 없음: %v\n%s", err, out)
	}
}

func TestGuardStep3Timeout(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	go func() {
		io.WriteString(pw, "ABC123\n\nsvr1ev01\n")
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
			err, out := runGuardStr(t, "ABC123\n\n"+okHosts+in, testOpts())
			wantCancel(t, err, out, "YeS 미입력 (입력값: "+shown+")")
			if strings.Contains(out, "Ctrl+C 로 취소 가능") {
				t.Fatal("취소인데 5단계 진행")
			}
		})
	}
}

func TestGuardCountdownLine(t *testing.T) {
	err, out := runGuardStr(t, "ABC123\n\n"+okHosts+"YeS\n", testOpts())
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
		"2단계": "ABC123\n",
		"3단계": "ABC123\n\nsvr1ev01\n",
		"4단계": "ABC123\n\n" + okHosts,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			err, out := runCancel(t, in, time.Millisecond, 50*time.Millisecond)
			wantCancel(t, err, out, "Ctrl+C 로 중단")
		})
	}
	t.Run("5단계", func(t *testing.T) {
		err, out := runCancel(t, "ABC123\n\n"+okHosts+"YeS\n", 40*time.Millisecond, 130*time.Millisecond)
		wantCancel(t, err, out, "Ctrl+C 로 중단", "10 .. 9")
		if strings.Contains(out, " .. 1\n") {
			t.Fatal("카운트다운이 끝까지 감")
		}
		if !strings.Contains(out, "\n[취소] Ctrl+C 로 중단") {
			t.Fatalf("줄바꿈 후 취소 출력 아님\n%q", out)
		}
	})
}

func TestConfirmCode(t *testing.T) {
	out := &syncBuf{}
	in := NewLineReader(strings.NewReader("XY12Z9\nxy12z9\n"))
	if err := ConfirmCode(context.Background(), in, out, "VWP 코드: ", "XY12Z9"); err != nil {
		t.Fatal(err)
	}
	err := ConfirmCode(context.Background(), in, out, "VWP 코드: ", "XY12Z9")
	wantCancel(t, err, out.String(), "VWP 확인 코드 불일치")
	err = ConfirmCode(context.Background(), in, out, "VWP 코드: ", "XY12Z9") // EOF
	wantCancel(t, err, out.String(), "VWP 확인 코드 불일치")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pr, pw := io.Pipe()
	defer pw.Close()
	out2 := &syncBuf{}
	err = ConfirmCode(ctx, NewLineReader(pr), out2, "VWP 코드: ", "XY12Z9")
	wantCancel(t, err, out2.String(), "Ctrl+C 로 중단")
}

func TestRunDemo(t *testing.T) {
	t.Setenv("SAFETYGUARD_TEST_CODE", "ABC123")
	t.Setenv("SAFETYGUARD_TEST_TICK_MS", "1")
	out := &syncBuf{}
	rc := RunDemo(context.Background(), NewLineReader(strings.NewReader("ABC123\n\nwas-prd-01 web-prd-02 web-prd-01\nYeS\n")), out)
	if rc != 0 || !strings.Contains(out.String(), "[DEMO] 실제 전원 작업은 하지 않습니다") {
		t.Fatalf("rc=%d\n%s", rc, out)
	}
	out = &syncBuf{}
	rc = RunDemo(context.Background(), NewLineReader(strings.NewReader("ABC123\n\nwas-prd-01 web-prd-02 web-prd-01\nyes\n")), out)
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
		t.Fatal("SAFETYGUARD_COLOR=never 인데 색상 적용")
	}
	t.Setenv("SAFETYGUARD_COLOR", "always")
	if Red("x") == "x" {
		t.Fatal("always 인데 색상 없음")
	}
}
