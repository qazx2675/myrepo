// Package auditlog는 검증 결과 중 불일치(FAIL/WARN)가 감지된 경우에만
// 실행 디렉토리의 LOG/ 폴더에 기록을 남긴다. 별도 중앙 로그 서버는 쓰지 않는다
// (PLAN.md 6장 "감사 로그 저장소" 항목 확정 내용).
package auditlog

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vm-verifier/verify"
)

// Write는 result가 PASS/INCONCLUSIVE뿐이면 아무것도 하지 않는다.
// FAIL 또는 WARN 단계가 하나라도 있으면 dir/LOG/vm-verifier-YYYYMMDD.log에 상세 내용을 append한다.
func Write(dir string, now time.Time, result verify.Result) error {
	hasDifference := false
	for _, s := range result.Steps {
		if s.Status == verify.Fail || s.Status == verify.Warn {
			hasDifference = true
			break
		}
	}
	if !hasDifference {
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

	fmt.Fprintf(f, "[%s] %s : %s\n", now.Format(time.RFC3339), result.Hostname, result.Overall())
	for _, s := range result.Steps {
		if s.Status == verify.Fail || s.Status == verify.Warn {
			fmt.Fprintf(f, "  [%d] %-24s %-6s %s\n", s.Step, s.Name, s.Status, s.Detail)
		}
	}
	fmt.Fprintln(f)
	return nil
}
