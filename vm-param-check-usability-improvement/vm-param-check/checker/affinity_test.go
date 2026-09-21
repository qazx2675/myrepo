package checker

import (
	"testing"

	"vm-param-check/model"
)

// affinity는 "이 vCPU를 돌릴 수 있는 물리 CPU 목록"이라 순서에 의미가 없다.
// 순서만 다른 값은 같은 설정으로 봐야 한다.
func TestSameAffinity(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		// 사용자가 든 실제 예시 — 내림차순/오름차순만 다른 같은 집합.
		{"역순", "31,29,27,25,23,21,19,17", "17,19,21,23,25,27,29,31", true},
		{"완전히 동일", "16,17", "16,17", true},
		{"두 개 순서 바뀜", "17,16", "16,17", true},
		{"공백 섞임", " 16 , 17 ", "16,17", true},
		{"단일값", "8", "8", true},
		// 자릿수가 다른 숫자를 문자열로 정렬하면 "10"<"2"가 되어 틀리므로 숫자 정렬이어야 한다.
		{"자릿수 다른 숫자", "10,2", "2,10", true},
		// 값 자체가 다르면 당연히 다르다.
		{"값 다름", "16,18", "16,17", false},
		{"개수 다름", "16", "16,17", false},
		// 중복은 조용히 통과시키지 않는다(오상일 가능성이 높아서).
		{"중복 있음", "16,17,17", "16,17", false},
		// 숫자가 아닌 값도 순서만 다르면 같게 본다.
		{"비숫자 토큰", "all", "all", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameAffinity(tt.a, tt.b); got != tt.want {
				t.Errorf("sameAffinity(%q, %q) = %v, 기대 %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// CheckAffinity를 통해서도 순서 무시가 동작하고, 리포트에는 원본 문자열이 그대로 남아야 한다.
func TestCheckAffinityIgnoresOrder(t *testing.T) {
	vm := model.VMInfo{
		Name: "host01ev01",
		ExtraConfig: map[string]string{
			"sched.vcpu0.affinity": "31,29,27,25,23,21,19,17",
		},
	}
	expected := map[string]string{"sched.vcpu0.affinity": "17,19,21,23,25,27,29,31"}

	got := CheckAffinity(vm, expected, "ev01")
	if len(got) != 1 {
		t.Fatalf("Finding %d개, 기대 1개: %+v", len(got), got)
	}
	if got[0].Result != "OK" {
		t.Errorf("Result = %q, 기대 OK (순서만 달라도 같은 설정)", got[0].Result)
	}
	// 표시는 vCenter에 실제로 박힌 값 그대로여야 한다(정렬해서 보여주지 않는다).
	if got[0].Actual != "31,29,27,25,23,21,19,17" {
		t.Errorf("Actual = %q, 원본이 그대로 남아야 합니다", got[0].Actual)
	}
	if got[0].Expected != "17,19,21,23,25,27,29,31" {
		t.Errorf("Expected = %q, 원본이 그대로 남아야 합니다", got[0].Expected)
	}
}

// 설정 자체가 없으면 순서 무시와 무관하게 "설정없음"이어야 한다 — 기존 동작 유지.
func TestCheckAffinityMissingUnchanged(t *testing.T) {
	vm := model.VMInfo{Name: "host01ev01", ExtraConfig: map[string]string{}}
	got := CheckAffinity(vm, map[string]string{"sched.vcpu0.affinity": "0,1"}, "ev01")
	if len(got) != 1 || got[0].Result != "설정없음" {
		t.Errorf("Result = %+v, 기대 설정없음", got)
	}
}
