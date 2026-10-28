package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pci-evNN=<vendor> 는 다른 옵션처럼 이름=값으로 읽힌다.
func TestParseSpecFilePCI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x_spec.txt")
	if err := os.WriteFile(path, []byte("cpu=4\n--pci-ev01=mellanox # ev01 Mellanox\npci-ev02=0x10de pci-ev03=\"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, err := ParseSpecFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, o := range opts {
		got[o.Name] = o.Value
	}
	if got["pci-ev01"] != "mellanox" || got["pci-ev02"] != "0x10de" {
		t.Errorf("파싱 결과: %v", got)
	}
	if _, ok := got["pci-ev03"]; ok {
		t.Errorf("따옴표째 빈 pci-ev03 은 건너뛰어야 함: %v", got)
	}
}

// pci-evNN 은 값 그대로(이름=값) 내보내져야 하고, 정의된 그룹에 붙은 pci 는 그룹 수에 영향이 없다.
func TestExportSpecPCI(t *testing.T) {
	m := writeSpec(t, "cpu=4 mem=8 disk=100 shares-ev01=normal\n"+
		"cpu-ev02=2 mem-ev02=4 disk-ev02=50 shares-ev02=1000\n"+
		"pci-ev01=mellanox\npci-ev02=Nvidia   # 주석\n")
	lines, err := ExportSpec(m)
	if err != nil {
		t.Fatal(err)
	}
	got := exportMap(lines)
	if got["pci-ev01"] != "mellanox" || got["pci-ev02"] != "Nvidia" {
		t.Errorf("pci 값 = %q / %q", got["pci-ev01"], got["pci-ev02"])
	}
	if got["groups"] != "2" {
		t.Errorf("groups = %q, want 2", got["groups"])
	}
}

// pci 값을 따옴표째 비워 둔(pci-ev01="") 줄은 지정 안 함이라 내보내지 않는다.
func TestExportSpecPCIEmptyQuotedSkipped(t *testing.T) {
	m := writeSpec(t, "cpu=4 mem=8 disk=100 shares-ev01=normal pci-ev01=\"\"\n")
	lines, err := ExportSpec(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := exportMap(lines)["pci-ev01"]; ok {
		t.Errorf("빈 pci 가 내보내졌습니다: %v", lines)
	}
}

// pci-evNN 만 있고 그 그룹의 cpu/mem/disk/shares 가 없으면 다른 키(예: affinity-ev03)와 똑같이 에러다.
func TestExportSpecPCIAloneStillErrors(t *testing.T) {
	// ev02 까지 있는데 ev03 에 pci 만 있는 경우 -> 생성 필수값 누락
	m := writeSpec(t, "cpu=4 mem=8 disk=100 shares-ev01=1000\n"+
		"cpu-ev02=2 mem-ev02=4 disk-ev02=50 shares-ev02=1000\npci-ev03=mellanox\n")
	if _, err := ExportSpec(m); err == nil || !strings.Contains(err.Error(), "cpu-ev03") {
		t.Fatalf("cpu-ev03 누락 에러가 나야 합니다: %v", err)
	}
	// ev02 없이 pci-ev03 만 있으면 연속 규칙 위반
	m = writeSpec(t, "cpu=4 mem=8 disk=100 shares-ev01=1000\npci-ev03=mellanox\n")
	if _, err := ExportSpec(m); err == nil || !strings.Contains(err.Error(), "연속") {
		t.Fatalf("연속 규칙 에러가 나야 합니다: %v", err)
	}
}
