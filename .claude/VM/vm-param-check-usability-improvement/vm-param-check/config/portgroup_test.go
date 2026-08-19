package config

import "testing"

func TestExtractFolderFromPortgroup(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"기본 예시", "TST-CAE001-SAMP48c-QRST-cae-10-1-2-3", "TST-CAE001-SAMP48c-QRST", true},
		{"두자리 옥텟", "DEV-CAE002-SAMB16c-UVWX-cae-10-5-6-7", "DEV-CAE002-SAMB16c-UVWX", true},
		{"대문자 CAE", "TST-CAE001-SAMP48c-QRST-CAE-10-1-2-3", "TST-CAE001-SAMP48c-QRST", true},
		{"cae 접미사 없음", "TST-CAE001-SAMP48c-QRST", "", false},
		{"옥텟 부족", "TST-CAE001-SAMP48c-QRST-cae-10-1-2", "", false},
		{"완전히 무관한 이름", "VM Network", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractFolderFromPortgroup(tt.input)
			if ok != tt.ok || got != tt.want {
				t.Errorf("ExtractFolderFromPortgroup(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}
