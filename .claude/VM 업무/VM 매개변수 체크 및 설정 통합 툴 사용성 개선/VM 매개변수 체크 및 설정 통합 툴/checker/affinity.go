package checker

import (
	"fmt"
	"strings"

	"vm-param-check/model"
)

// GenerateExpectedAffinityEV01은 ev01 그룹의 자동계산 기대 affinity 맵을 만든다.
// numCPU는 vCenter에 실제 설정된 vCPU 수를 쓴다(입력 플래그가 아니라 실측값 기준 —
// FAIL인 VM에서도 "실제 vCPU 개수만큼" 정확히 몇 개의 affinity 항목이 있어야 하는지
// 판단하기 위함).
//
// HT ON: vcpu i (0-based)에 대해 물리 코어 쌍 [2i, 2i+1] -> "2i,2i+1"
// HT OFF: vcpu i에 대해 단일 코어 [i] -> "i"
func GenerateExpectedAffinityEV01(numCPU int32, htOn bool) map[string]string {
	expected := map[string]string{}
	for i := int32(0); i < numCPU; i++ {
		key := fmt.Sprintf("sched.vcpu%d.affinity", i)
		if htOn {
			expected[key] = fmt.Sprintf("%d,%d", 2*i, 2*i+1)
		} else {
			expected[key] = fmt.Sprintf("%d", i)
		}
	}
	return expected
}

// CheckAffinity는 expected(자동계산 or 파일에서 읽은 맵)를 vm.ExtraConfig 실제값과 비교한다.
// ev01/ev02/ev03 공통으로 쓰는 비교 로직 — 기대값 산출 방식만 호출부에서 다르게 넘겨준다.
func CheckAffinity(vm model.VMInfo, expected map[string]string, source string) []model.Finding {
	var findings []model.Finding
	for key, exp := range expected {
		actual, exists := vm.ExtraConfig[key]
		f := model.Finding{VM: vm.Name, Source: source, Key: key, Expected: exp}
		if !exists {
			f.Result = "설정없음"
		} else {
			f.Actual = actual
			if strings.EqualFold(strings.TrimSpace(actual), exp) {
				f.Result = "OK"
			} else {
				f.Result = "FAIL"
			}
		}
		findings = append(findings, f)
	}
	return findings
}
