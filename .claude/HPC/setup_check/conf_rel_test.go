package main

import (
	"os"
	"path/filepath"
	"testing"
)

// STRESS_TEST·STRESS_TEST_CONF 상대경로는 설정 파일 폴더 기준, 절대경로·빈 값은 그대로.
func TestRelToConf(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "new_setup.conf")
	os.WriteFile(p, []byte("STRESS_TEST=stress-test/stress-test\nSTRESS_TEST_CONF=/abs/x.conf\n"), 0o644)
	c, err := loadRunConf(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.StressTest != filepath.Join(d, "stress-test/stress-test") || c.StressTestConf != "/abs/x.conf" {
		t.Errorf("STRESS_TEST=%s STRESS_TEST_CONF=%s", c.StressTest, c.StressTestConf)
	}
	// 실행 중 고친 값: 다시 읽으면 반영(비우면 빈 값)
	os.WriteFile(p, []byte("STRESS_TEST_CONF=\n"), 0o644)
	if c, _ = loadRunConf(p); c.StressTestConf != "" {
		t.Errorf("비운 값 = %q", c.StressTestConf)
	}
	if c = relToConf(config{StressTestConf: "a.conf"}, ""); c.StressTestConf != "a.conf" {
		t.Errorf("설정 파일 없음 = %q", c.StressTestConf)
	}
}
