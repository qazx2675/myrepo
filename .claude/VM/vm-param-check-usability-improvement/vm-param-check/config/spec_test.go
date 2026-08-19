package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeFolderName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		// 실제 현업 폴더명 예시 — 차수(CAE001)만 정규화되고 나머지는 그대로여야 한다.
		{"실예시1", "TST-CAE001-SAMP48c-QRST", "TST-CAE-SAMP48c-QRST", true},
		{"실예시2", "DEV-CAE001-SAMB16c-UVWX", "DEV-CAE-SAMB16c-UVWX", true},
		{"실예시3 (+ 포함)", "DEV-CAE001-SAMC16c-UVWX+VDT", "DEV-CAE-SAMC16c-UVWX+VDT", true},
		// 차수만 다른 건 같은 스펙으로 모여야 한다.
		{"차수 다름", "TST-CAE003-SAMP48c-QRST", "TST-CAE-SAMP48c-QRST", true},
		{"차수 자릿수 다름", "TST-CAE12-SAMP48c-QRST", "TST-CAE-SAMP48c-QRST", true},
		// LSI 접두어도 CAE와 같은 규칙으로 동작해야 한다. 단, 접두어 자체는 남는다.
		{"LSI 접두어", "TST-LSI001-SAMP48c-QRST", "TST-LSI-SAMP48c-QRST", true},
		{"LSI 차수 다름", "TST-LSI003-SAMP48c-QRST", "TST-LSI-SAMP48c-QRST", true},
		{"LSI 소문자", "TST-lsi001-SAMP48c-QRST", "TST-LSI-SAMP48c-QRST", true},
		{"LSI 숫자 없음", "TST-LSI-SAMP48c-QRST", "", false},
		// 규칙에서 벗어나면 매칭 대상이 아니다.
		{"레코드 3개", "TST-CAE001-QRST", "", false},
		{"레코드 5개", "TST-CAE001-SAMP48c-QRST-X", "", false},
		{"2번째가 CAE/LSI 아님", "TST-XYZ001-SAMP48c-QRST", "", false},
		{"2번째에 숫자 없음", "TST-CAE-SAMP48c-QRST", "", false},
		{"Task 폴더", "Task", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeFolderName(tt.input)
			if ok != tt.ok || got != tt.want {
				t.Errorf("NormalizeFolderName(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// 서로 다른 스펙끼리는 절대 섞이면 안 된다 — 사용자가 준 3개 예시는 전부 다른 스펙이다.
func TestNormalizeFolderNameDistinct(t *testing.T) {
	samples := []string{
		"TST-CAE001-SAMP48c-QRST",
		"DEV-CAE001-SAMB16c-UVWX",
		"DEV-CAE001-SAMC16c-UVWX+VDT",
		// 접두어만 다른 폴더는 서로 다른 스펙이다 — 정규화 때 접두어를 CAE로 뭉개면 여기서 잡힌다.
		"TST-LSI001-SAMP48c-QRST",
	}
	seen := map[string]string{}
	for _, s := range samples {
		got, ok := NormalizeFolderName(s)
		if !ok {
			t.Fatalf("%q 정규화 실패", s)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%q 와 %q 가 같은 스펙(%q)으로 묶였습니다 — 서로 달라야 합니다", prev, s, got)
		}
		seen[got] = s
	}
}

func TestParseSpecFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s_spec.txt")
	content := "ht=on --cores=20 --numa=20\n" +
		"-cpu=40   # 주석은 무시\n" +
		"\n" +
		"# 통째로 주석인 줄\n" +
		"affinity-ev02=affinity-ev02.txt\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ParseSpecFile(path)
	if err != nil {
		t.Fatalf("ParseSpecFile 실패: %v", err)
	}
	want := []SpecOption{
		{"ht", "on"}, {"cores", "20"}, {"numa", "20"},
		{"cpu", "40"},
		{"affinity-ev02", "affinity-ev02.txt"},
	}
	if len(got) != len(want) {
		t.Fatalf("옵션 %d개, 기대 %d개: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("옵션[%d] = %+v, 기대 %+v", i, got[i], want[i])
		}
	}
}

func TestParseSpecFileRejectsBadToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_spec.txt")
	if err := os.WriteFile(path, []byte("cores 20\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSpecFile(path); err == nil {
		t.Error("이름=값 형태가 아닌 토큰인데 에러가 나지 않았습니다")
	}
}

func TestFindSpec(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "TST-CAE001-SAMP48c-QRST")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specFile := filepath.Join(specDir, "TST-CAE001-SAMP48c-QRST_spec.txt")
	if err := os.WriteFile(specFile, []byte("ht=on --cores=20\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 관계없는 스펙도 하나 둬서 엉뚱한 게 잡히지 않는지 본다.
	other := filepath.Join(root, "DEV-CAE001-SAMB16c-UVWX")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}

	// 차수가 달라도(CAE007) 같은 스펙으로 찾아야 한다.
	m, err := FindSpec(root, "TST-CAE007-SAMP48c-QRST")
	if err != nil {
		t.Fatalf("FindSpec 실패: %v", err)
	}
	if m == nil {
		t.Fatal("차수만 다른 폴더인데 스펙을 못 찾았습니다")
	}
	if m.SpecFile != specFile {
		t.Errorf("SpecFile = %q, 기대 %q", m.SpecFile, specFile)
	}

	// 대응하는 스펙이 없으면 (nil, nil)
	m, err = FindSpec(root, "ETC-CAE001-XXXX-YYYY")
	if err != nil {
		t.Fatalf("FindSpec 실패: %v", err)
	}
	if m != nil {
		t.Errorf("없는 스펙인데 %+v 가 매칭됐습니다", m)
	}

	// 규칙에 안 맞는 폴더명은 에러
	if _, err := FindSpec(root, "Task"); err == nil {
		t.Error("CAE 규칙에 맞지 않는 폴더명인데 에러가 나지 않았습니다")
	}
}
