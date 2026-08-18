package config

import "testing"

func TestExtractFolderFromPortgroup(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{"기본 예시", "RND-CAE001-LICE48c-TCAD-cae-10-111-111-0", "RND-CAE001-LICE48c-TCAD", true},
		{"두자리 옥텟", "MEM-CAE002-CPSR16c-ECAD-cae-172-16-1-100", "MEM-CAE002-CPSR16c-ECAD", true},
		{"대문자 CAE", "RND-CAE001-LICE48c-TCAD-CAE-10-111-111-0", "RND-CAE001-LICE48c-TCAD", true},
		{"cae 접미사 없음", "RND-CAE001-LICE48c-TCAD", "", false},
		{"옥텟 부족", "RND-CAE001-LICE48c-TCAD-cae-10-111-111", "", false},
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
