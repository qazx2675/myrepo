// Package auditlog는 FAIL이 감지된 경우에만 실행 디렉토리의 LOG/ 폴더에 기록을 남긴다.
// 별도 중앙 로그 서버는 쓰지 않는다 (PLAN.md 6장 "감사 로그 저장소" 항목 확정 내용).
package auditlog

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vm-verifier/verify"
)

// Write는 result가 PASS면 아무것도 하지 않는다.
// FAIL이면 dir/LOG/vm-verifier-YYYYMMDD.log에 상세 내용을 append한다.
func Write(dir string, now time.Time, result verify.Result) error {
	if result.Status != verify.Fail {
		return nil
	}

	logDir := filepath.Join(dir, "LOG")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("LOG 폴더 생성 실패: %w", err)
	}

	logPath := filepath.Join(logDir, "vm-verifier-"+now.Format("20060102")+".log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("LOG 파일 열기 실패: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "[%s] %s : %s — %s\n", now.Format(time.RFC3339), result.Hostname, result.Status, result.Detail)
	return nil
}
