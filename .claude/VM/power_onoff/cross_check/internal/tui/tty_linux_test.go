//go:build linux

// tty_linux_test.go - 실제 pty(/dev/ptmx)에서 termios 회귀: raw 모드로 바꿨다가 되돌리면 원래 값이어야 하고,
// 깨진 상태(raw 로 남음)는 정상으로 고쳐 되돌린다. isTTY·ttySize·waitReadable 도 실제 ioctl·select 로 확인한다.
// (auto_setup tty_linux_test.go 와 같은 방식)
package tui

import (
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// openPty: 새 pty 의 (마스터, 슬레이브) (실패하면 건너뜀)
func openPty(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("pty 를 열 수 없음: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	var unlock int32
	if err := ioctl(m.Fd(), 0x40045431, unsafe.Pointer(&unlock)); err != nil { // TIOCSPTLCK
		t.Skipf("unlockpt 실패: %v", err)
	}
	var n uint32
	if err := ioctl(m.Fd(), 0x80045430, unsafe.Pointer(&n)); err != nil { // TIOCGPTN
		t.Skipf("ptsname 실패: %v", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("슬레이브 열기 실패: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return m, s
}

func getTermios(t *testing.T, f *os.File) syscall.Termios {
	t.Helper()
	var tm syscall.Termios
	if err := ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&tm)); err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestTermiosRawAndRestore(t *testing.T) {
	m, s := openPty(t)
	if !isTTY(s) {
		t.Fatal("pty 슬레이브는 tty")
	}
	if p, w, _ := os.Pipe(); isTTY(p) {
		t.Fatal("파이프는 tty 가 아님")
	} else {
		p.Close()
		w.Close()
	}
	orig := getTermios(t, s)
	if orig.Lflag&syscall.ISIG == 0 || orig.Lflag&syscall.ICANON == 0 {
		t.Skip("pty 기본 설정이 예상과 다름")
	}
	rest, err := makeRaw(s)
	if err != nil {
		t.Fatal(err)
	}
	raw := getTermios(t, s)
	if raw.Lflag&(syscall.ISIG|syscall.ICANON|syscall.ECHO) != 0 || raw.Oflag&syscall.OPOST != 0 || raw.Iflag&syscall.ICRNL != 0 {
		t.Fatalf("raw 중에는 ISIG·ICANON·ECHO·OPOST·ICRNL 이 꺼져야 함: %+v", raw)
	}
	rest()
	if got := getTermios(t, s); got != orig {
		t.Fatalf("raw 종료 뒤 termios 가 복원돼야 함:\n got %+v\nwant %+v", got, orig)
	}
	// 창 크기(TIOCSWINSZ → TIOCGWINSZ)
	ws := struct{ Row, Col, X, Y uint16 }{Row: 33, Col: 111}
	if err := ioctl(m.Fd(), syscall.TIOCSWINSZ, unsafe.Pointer(&ws)); err != nil {
		t.Fatal(err)
	}
	if w, h := ttySize(s); w != 111 || h != 33 {
		t.Fatalf("ttySize = %dx%d", w, h)
	}
	// 입력 대기(select): 없으면 false, 마스터에 쓰면 true
	if waitReadable(s, 20*time.Millisecond) {
		t.Fatal("입력이 없는데 읽을 수 있다고 함")
	}
	rest, _ = makeRaw(s)
	defer rest()
	m.Write([]byte("x"))
	if !waitReadable(s, time.Second) {
		t.Fatal("입력이 있는데 못 봄")
	}
}

// 이전 실행이 비정상 종료해 raw 로 남은 터미널: 다시 실행하고 종료하면 정상(ISIG·ICANON·ECHO)으로 돌아온다
func TestTermiosBrokenStateIsRepaired(t *testing.T) {
	_, s := openPty(t)
	orig := getTermios(t, s)
	broken := orig
	broken.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.ISIG
	if err := ioctl(s.Fd(), syscall.TCSETS, unsafe.Pointer(&broken)); err != nil {
		t.Fatal(err)
	}
	rest, err := makeRaw(s)
	if err != nil {
		t.Fatal(err)
	}
	rest()
	got := getTermios(t, s)
	if got.Lflag&syscall.ISIG == 0 || got.Lflag&syscall.ICANON == 0 || got.Lflag&syscall.ECHO == 0 {
		t.Fatalf("깨진 설정이 정상으로 고쳐져야 함: lflag=%#x", got.Lflag)
	}
}

func TestSanitizeTermiosKeepsUserSettings(t *testing.T) {
	var tm syscall.Termios
	tm.Lflag = syscall.ISIG | syscall.ICANON | syscall.ECHO
	tm.Cc[syscall.VINTR] = 0x07 // 사용자가 stty intr ^G 로 바꾼 설정은 그대로
	if sanitizeTermios(&tm) || tm.Cc[syscall.VINTR] != 0x07 {
		t.Fatal("정상 설정은 건드리지 않아야 함")
	}
}

// 실제 pty 위에서 전체 화면 루프를 돌린다: 키(↓ → Enter …)는 마스터로 쓰고, 끝나면 termios 가 원래대로.
func TestFullScreenOnRealPty(t *testing.T) {
	m, s := openPty(t)
	orig := getTermios(t, s)
	f := aliceJobFake()
	env, cleanup := RealEnv(s, s, false)
	defer cleanup()
	env.Now = func() time.Time { return t0.Add(30 * time.Second) }
	go func() {
		buf := make([]byte, 64<<10)
		for { // 화면 출력은 마스터에서 읽어 버린다(안 읽으면 pty 버퍼가 차서 쓰기가 막힘)
			if _, err := m.Read(buf); err != nil {
				return
			}
		}
	}()
	done := make(chan int, 1)
	go func() {
		code, _ := RunUserFull(env, f, "bob")
		done <- code
	}()
	step := func(b string) {
		time.Sleep(150 * time.Millisecond)
		m.Write([]byte(b))
	}
	step("\x1b[C")                                           // → 확인자 구역
	step("\r")                                               // 승인 1/3
	step("\x1b[200~svr2ev01\nsvr1ev01\tsvr1ev02\n\x1b[201~") // 붙여넣기
	step("\r")                                               // 빈 줄 Enter
	step("bob12345\r")                                       // 2/3 패스워드
	step("\r")                                               // 3/3 → 목록
	step("\x1b")                                             // 단독 Esc(100ms 뒤 Esc 로 처리) → 종료
	select {
	case code := <-done:
		if code != ExitOK {
			t.Fatalf("code=%d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("단독 Esc 로 끝나지 않음")
	}
	if f.jobs[0].Status != "승인" {
		t.Fatalf("실제 pty 승인: %+v", f.jobs[0])
	}
	if got := getTermios(t, s); got != orig {
		t.Fatalf("종료 뒤 termios 복원:\n got %+v\nwant %+v", got, orig)
	}
}
