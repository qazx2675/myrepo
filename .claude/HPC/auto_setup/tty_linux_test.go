//go:build linux

// tty_linux_test.go - termios 복원 회귀: AWX(01) 실행용 설정(intr=^X)을 적용했다가 되돌리면 원래 값(Ctrl+C=intr)이어야 하고,
// raw 모드도 같다. v0.8.5 이전에는 tcsetsf 가 TIOCSTI(0x5412) 라 복원되지 않아 auto_setup 종료 뒤 Ctrl+C 가 안 먹혔다.
package main

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// openPty: 새 pty 의 슬레이브 (실패하면 건너뜀)
func openPty(t *testing.T) *os.File {
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
	s, err := os.OpenFile("/dev/pts/"+itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("슬레이브 열기 실패: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func getTermios(t *testing.T, f *os.File) syscall.Termios {
	t.Helper()
	var tm syscall.Termios
	if err := ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&tm)); err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestTermiosRestoredAfterAwxAndRaw(t *testing.T) {
	s := openPty(t)
	orig := getTermios(t, s)
	if orig.Cc[syscall.VINTR] != 0x03 || orig.Lflag&syscall.ISIG == 0 {
		t.Skip("pty 기본 설정이 예상과 다름")
	}
	end, err := withAwxTermios(s)
	if err != nil {
		t.Fatal(err)
	}
	if got := getTermios(t, s); got.Cc[syscall.VINTR] != 0x18 {
		t.Fatalf("AWX 설정 중 intr=^X 여야 함: %#x", got.Cc[syscall.VINTR])
	}
	end()
	if got := getTermios(t, s); got != orig {
		t.Fatalf("AWX 종료 뒤 termios 가 복원돼야 함 (Ctrl+C=intr):\n got %+v\nwant %+v", got, orig)
	}
	rest, err := makeRaw(s)
	if err != nil {
		t.Fatal(err)
	}
	if got := getTermios(t, s); got.Lflag&syscall.ISIG != 0 {
		t.Fatal("raw 중에는 ISIG 가 꺼져 있어야 함")
	}
	rest()
	if got := getTermios(t, s); got != orig {
		t.Fatalf("raw 종료 뒤 termios 가 복원돼야 함:\n got %+v\nwant %+v", got, orig)
	}
}

// 이전 실행이 비정상 종료해 intr=^X·raw 로 남은 터미널: 다시 실행하고 종료하면 정상으로 돌아온다
func TestTermiosBrokenStateIsRepaired(t *testing.T) {
	s := openPty(t)
	orig := getTermios(t, s)
	broken := orig
	broken.Cc[syscall.VINTR], broken.Cc[syscall.VSUSP] = 0x18, 0
	broken.Lflag &^= syscall.ICANON | syscall.ECHO
	if err := ioctl(s.Fd(), syscall.TCSETS, unsafe.Pointer(&broken)); err != nil {
		t.Fatal(err)
	}
	rest, err := makeRaw(s)
	if err != nil {
		t.Fatal(err)
	}
	rest()
	got := getTermios(t, s)
	if got.Cc[syscall.VINTR] != 0x03 || got.Cc[syscall.VSUSP] != 0x1a || got.Lflag&syscall.ISIG == 0 || got.Lflag&syscall.ICANON == 0 || got.Lflag&syscall.ECHO == 0 {
		t.Fatalf("깨진 설정이 정상으로 고쳐져야 함: intr=%#x susp=%#x lflag=%#x", got.Cc[syscall.VINTR], got.Cc[syscall.VSUSP], got.Lflag)
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
