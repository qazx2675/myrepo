//go:build linux || darwin

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
)

// AcquireLock 은 path 에 flock(LOCK_EX|LOCK_NB) 을 걸어 데몬이 하나만 뜨게 한다(INTERVIEW Q10, crontab --ensure).
// 이미 다른 프로세스가 쥐고 있으면 ErrLocked. 성공하면 파일에 pid 를 적고, release 로 푼다(여러 번 불러도 안전).
// 잠금 파일은 지우지 않는다(지우면 다른 프로세스가 새 파일에 잠금을 걸어 둘이 동시에 뜰 수 있음).
func AcquireLock(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("잠금 디렉터리 생성 실패: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("잠금 파일 열기 실패: %w", err)
	}
	fd := int(f.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("잠금 실패: %w", err)
	}
	// 정보용 pid 기록(실패해도 잠금 자체는 유효)
	if err := f.Truncate(0); err == nil {
		f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			syscall.Flock(fd, syscall.LOCK_UN)
			f.Close()
		})
	}, nil
}
