package main

import (
	"reflect"
	"strings"
	"testing"
)

func setProfiles(t *testing.T, v ...string) {
	t.Helper()
	ptrs := []*string{&awx_profile_1, &awx_profile_2, &awx_profile_3, &awx_profile_4, &awx_profile_5,
		&awx_profile_6, &awx_profile_7, &awx_profile_8, &awx_profile_9}
	old := make([]string, len(ptrs))
	for i, p := range ptrs {
		old[i] = *p
		*p = ""
	}
	t.Cleanup(func() {
		for i, p := range ptrs {
			*p = old[i]
		}
	})
	for i, s := range v {
		*ptrs[i] = s
	}
}

func TestParseAwxProfiles(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		valid   []AwxProfile
		problem []string // 각 항목이 problems 에 포함된 문자열이어야 함
	}{
		{"모두 비어 있음", nil, nil, nil},
		{"기본", []string{"y|2025|설명"}, []AwxProfile{{1, "y", "2025", "설명"}}, nil},
		{"대문자 nodeinfo 는 소문자로", []string{"N|2024|x"}, []AwxProfile{{1, "n", "2024", "x"}}, nil},
		{"설명 비어 있음 (구분자 2개)", []string{"y|default|"}, []AwxProfile{{1, "y", "default", ""}}, nil},
		{"설명 필드 없음", []string{"n|2026"}, []AwxProfile{{1, "n", "2026", ""}}, nil},
		{"설명에 공백·|", []string{"y|2026-OPC_MDP|OPC MDP | 신규 장비"}, []AwxProfile{{1, "y", "2026-OPC_MDP", "OPC MDP | 신규 장비"}}, nil},
		{"앞뒤 공백", []string{" y | 2026-ECAD_TCAD | 설명 "}, []AwxProfile{{1, "y", "2026-ECAD_TCAD", "설명"}}, nil},
		{"빈 칸 건너뜀(번호 유지)", []string{"", "y|2025|b", "", "n|default|d"}, []AwxProfile{{2, "y", "2025", "b"}, {4, "n", "default", "d"}}, nil},
		{"OS 불허", []string{"y|2025|a", "y|2024|b", "y|2027|c"}, []AwxProfile{{1, "y", "2025", "a"}, {2, "y", "2024", "b"}},
			[]string{"awx_profile_3: OS 값 2027 은 허용되지 않음 (2024 2025 2026 2026-OPC_MDP 2026-ECAD_TCAD default)"}},
		{"필드 부족", []string{"y"}, nil, []string{"awx_profile_1: 형식 오류"}},
		{"nodeinfo 가 y/n 아님", []string{"x|2025|a"}, nil, []string{"awx_profile_1: nodeinfo 값 x"}},
		{"OS 대소문자 구분", []string{"y|DEFAULT|a"}, nil, []string{"awx_profile_1: OS 값 DEFAULT"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setProfiles(t, c.in...)
			valid, problems := parseAwxProfiles()
			if !reflect.DeepEqual(valid, c.valid) {
				t.Errorf("valid = %+v, want %+v", valid, c.valid)
			}
			if len(problems) != len(c.problem) {
				t.Fatalf("problems = %q, want %d건", problems, len(c.problem))
			}
			for i, p := range c.problem {
				if !strings.Contains(problems[i], p) {
					t.Errorf("problems[%d] = %q, want 포함 %q", i, problems[i], p)
				}
			}
		})
	}
}

func TestParseAwxProfilesNine(t *testing.T) {
	v := make([]string, 9)
	for i := range v {
		v[i] = "y|2025|p"
	}
	setProfiles(t, v...)
	valid, problems := parseAwxProfiles()
	if len(valid) != 9 || len(problems) != 0 || valid[8].No != 9 {
		t.Errorf("valid=%d problems=%v", len(valid), problems)
	}
}

func TestConfVarsHasAwxProfiles(t *testing.T) {
	m := confVars()
	for _, k := range []string{"awx_profile_1", "awx_profile_5", "awx_profile_9"} {
		if m[k] == nil {
			t.Errorf("confVars 에 %s 없음", k)
		}
	}
}
