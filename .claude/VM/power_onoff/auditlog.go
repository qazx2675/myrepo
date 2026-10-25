package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Audit 는 실행 기록이다: allowed_dir/run_<user>/YYYYmmdd_HHMMSS_<기능>.log (디렉터리 0700, 파일 0600).
// 비밀번호와 확인 문구 입력값은 기록하지 않는다. nil 이어도 안전하다(기록 안 함).
type Audit struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

func OpenAudit(runDir string, act Action) (*Audit, error) {
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	base := time.Now().Format("20060102_150405") + "_" + act.Slug()
	// 같은 초에 같은 기능을 또 실행해도 기록이 사라지지 않게 _2, _3 … 을 붙인다.
	var (
		p   string
		f   *os.File
		err error
	)
	for i := 1; i <= 100; i++ {
		name := base + ".log"
		if i > 1 {
			name = fmt.Sprintf("%s_%d.log", base, i)
		}
		p = filepath.Join(runDir, name)
		f, err = os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	return &Audit{f: f, path: p}, nil
}

// openAuditOrWarn 은 실패해도 작업을 막지 않고 경고만 한다.
func openAuditOrWarn(out io.Writer, runDir string, act Action) *Audit {
	a, err := OpenAudit(runDir, act)
	if err != nil {
		fmt.Fprintln(out, Yellow("[경고] 감사 로그를 남기지 못합니다: "+err.Error()))
		return nil
	}
	return a
}

func (a *Audit) Path() string {
	if a == nil {
		return ""
	}
	return a.path
}

// Write 는 RunLog 의 tee 로 쓰인다(시각은 RunLog 가 붙인다).
func (a *Audit) Write(p []byte) (int, error) {
	if a == nil {
		return len(p), nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.f.Write(p)
}

func (a *Audit) Printf(format string, args ...any) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	fmt.Fprintf(a.f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}

func (a *Audit) Close() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.f.Close()
}

// osUser 는 실제 실행한 계정이다(sudo 로 실행했으면 SUDO_USER).
func osUser() string {
	for _, k := range []string{"SUDO_USER", "USER", "USERNAME", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "unknown"
}
