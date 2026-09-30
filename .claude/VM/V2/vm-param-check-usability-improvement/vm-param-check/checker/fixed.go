package checker

import (
	"strings"

	"vm-param-check/model"
)

// fixedExpect는 계획서 3-2의 모든 VM 공통 고정 기대값이다.
// sched.mem.pin은 vCenter Advanced Config에 실제 기록되지 않는 파라미터라 계획서 지시대로 제외했다.
var fixedExpect = []struct {
	Key      string
	Expected string
}{
	{"sched.mem.lpage.enable1GPage", "TRUE"},
	{"sched.mem.prealloc", "TRUE"},
	{"sched.mem.prealloc.pinnedMainMem", "TRUE"},
	{"sched.swap.vmxSwapEnabled", "FALSE"},
}

// SkipLpage 가 true 면 HugePage 계열 4개 키(lpage/prealloc/pinnedMainMem/vmxSwapEnabled)는 체크하지 않는다.
// vm_setup.sh 를 -del_lpage 로 실행해 일부러 지운 경우에 쓴다(-skipLpage).
var SkipLpage bool

// CheckFixed는 3-2 고정값 체크를 수행한다.
func CheckFixed(vm model.VMInfo) []model.Finding {
	var findings []model.Finding
	if SkipLpage {
		return nil
	}
	for _, f := range fixedExpect {
		actual, exists := vm.ExtraConfig[f.Key]
		finding := model.Finding{VM: vm.Name, Source: "-", Key: f.Key, Expected: f.Expected}
		if !exists {
			finding.Actual = ""
			finding.Result = "설정없음"
		} else {
			finding.Actual = actual
			if strings.EqualFold(strings.TrimSpace(actual), f.Expected) {
				finding.Result = "OK"
			} else {
				finding.Result = "FAIL"
			}
		}
		findings = append(findings, finding)
	}
	return findings
}
