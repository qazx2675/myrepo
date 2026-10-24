//go:build linux || darwin

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 두 번째 잠금은 ErrLocked, 풀고 나면 다시 잡힌다(flock 은 열린 파일 단위라 같은 프로세스에서도 시험 가능).
func TestAcquireLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "daemon.lock")
	release, err := AcquireLock(p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.TrimSpace(string(b)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("잠금 파일 내용 = %q", b)
	}
	if _, err := AcquireLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("두 번째 잠금 = %v, ErrLocked 기대", err)
	}
	release()
	release() // 두 번 불러도 안전
	r2, err := AcquireLock(p)
	if err != nil {
		t.Fatalf("해제 후 잠금 실패: %v", err)
	}
	r2()
}
