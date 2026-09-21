package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSpec(t *testing.T, body string) *SpecMatch {
	t.Helper()
	root := t.TempDir()
	name := "TST-CAE001-SAMP48c-QRST"
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+"_spec.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := FindSpec(root, "TST-CAE003-SAMP48c-QRST")
	if err != nil || m == nil {
		t.Fatalf("FindSpec: %v %v", m, err)
	}
	return m
}

func exportMap(lines []ExportLine) map[string]string {
	m := map[string]string{}
	for _, l := range lines {
		m[l.Name] = l.Value
	}
	return m
}

// ev01 이름 없는 키가 -ev01로 정규화되고, 값이 있는 ev만 groups에 잡혀야 한다.
func TestExportSpecNormalizesAndCounts(t *testing.T) {
	m := writeSpec(t, "ht=on cpu=4 mem=8 disk=100 shares-ev01=normal cores=4 numa=4\n"+
		"cpu-ev02=2 mem-ev02=4 disk-ev02=50 shares-ev02=1000\n"+
		"cpu-ev03=\"1\" mem-ev03=\"1\" disk-ev03=\"20\" shares-ev03=\"normal\"\n"+
		"cpu-ev04=\"\" mem-ev04=\"\"   # 비워 둔 ev04는 실행 안 함\n"+
		"affinity-ev02=affinity_ev02.txt\n")
	lines, err := ExportSpec(m)
	if err != nil {
		t.Fatal(err)
	}
	got := exportMap(lines)
	if lines[0].Name != "groups" || got["groups"] != "3" {
		t.Errorf("groups = %q, want 3 (첫 줄이어야 함)", got["groups"])
	}
	for k, want := range map[string]string{"cpu-ev01": "4", "disk-ev01": "100", "shares-ev01": "normal", "cores-ev01": "4", "cpu-ev03": "1", "shares-ev03": "normal", "ht": "on"} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	if _, ok := got["cpu-ev04"]; ok {
		t.Errorf("빈 값 ev04가 내보내졌습니다: %v", got)
	}
	if a := got["affinity-ev02"]; !filepath.IsAbs(a) || !strings.HasSuffix(a, "affinity_ev02.txt") {
		t.Errorf("affinity 경로가 절대경로로 풀리지 않았습니다: %q", a)
	}
}

// ev02 없이 ev03만 있으면 연속 규칙 위반으로 에러여야 한다.
func TestExportSpecRejectsGap(t *testing.T) {
	m := writeSpec(t, "cpu=4 mem=8 disk=100 shares-ev01=1000\ncpu-ev03=1 mem-ev03=1 disk-ev03=20 shares-ev03=1000\n")
	_, err := ExportSpec(m)
	if err == nil || !strings.Contains(err.Error(), "연속") {
		t.Fatalf("연속 규칙 에러가 나야 합니다: %v", err)
	}
}

// 정의된 그룹에 생성 필수값(cpu/mem/disk/shares)이 빠지면 에러여야 한다.
func TestExportSpecRequiresCreateValues(t *testing.T) {
	m := writeSpec(t, "cpu=4 mem=8 disk=100 shares-ev01=1000\ncpu-ev02=2\n")
	_, err := ExportSpec(m)
	if err == nil || !strings.Contains(err.Error(), "mem-ev02") {
		t.Fatalf("mem-ev02 누락 에러가 나야 합니다: %v", err)
	}
}

// ev01 값이 없으면 에러여야 한다.
func TestExportSpecRequiresEV01(t *testing.T) {
	m := writeSpec(t, "ht=on\n")
	if _, err := ExportSpec(m); err == nil {
		t.Fatal("ev01이 없는데 에러가 나지 않았습니다")
	}
}

// ev01~ev10 전부 채운 스펙은 groups=10이어야 한다.
func TestExportSpecTenGroups(t *testing.T) {
	var b strings.Builder
	b.WriteString("cpu=4 mem=8 disk=100 shares-ev01=1000\n")
	for _, g := range []string{"ev02", "ev03", "ev04", "ev05", "ev06", "ev07", "ev08", "ev09", "ev10"} {
		b.WriteString("cpu-" + g + "=1 mem-" + g + "=1 disk-" + g + "=1 shares-" + g + "=normal\n")
	}
	lines, err := ExportSpec(writeSpec(t, b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if got := exportMap(lines)["groups"]; got != "10" {
		t.Errorf("groups = %q, want 10", got)
	}
}
