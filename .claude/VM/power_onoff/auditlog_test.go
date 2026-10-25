package main

import (
	"path/filepath"
	"testing"
)

// 같은 초에 같은 기능을 두 번 열어도 둘 다 기록 파일을 갖는다(_2, _3 …).
func TestOpenAuditSameSecond(t *testing.T) {
	dir := t.TempDir()
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		a, err := OpenAudit(filepath.Join(dir, "run_t"), ActPowerOff)
		if err != nil {
			t.Fatalf("OpenAudit #%d: %v", i, err)
		}
		if seen[a.Path()] {
			t.Fatalf("같은 경로를 두 번 줌: %s", a.Path())
		}
		seen[a.Path()] = true
		a.Close()
	}
}
