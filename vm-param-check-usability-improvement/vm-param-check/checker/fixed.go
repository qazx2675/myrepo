package checker

import (
	"regexp"
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

// ExpectNoLpage 가 true 면 HugePage 계열 4개 키는 "값이 있어야" 가 아니라 "없어야(삭제됨)" 정상으로 본다.
// vm_setup.sh -del_lpage 로 일부러 지운 VM 용(-expectNoLpage). 나머지 항목은 평소대로 체크한다.
var ExpectNoLpage bool

// ExpectNoAffinity 가 true 면 affinity 값(sched.cpu.affinity / sched.vcpuN.affinity)은 하나도 없어야 정상이다.
// vm_setup.sh -del_affinity 로 지운 VM 용(-expectNoAffinity). 스펙의 affinity 기대값은 비교하지 않는다.
var ExpectNoAffinity bool

// "삭제됨" = 키가 없거나 값이 빈 문자열(삭제 도구가 빈 값을 보내므로 vCenter 가 빈 키를 남길 수 있다).
func isDeleted(vm model.VMInfo, key string) (actual string, deleted bool) {
	actual, exists := vm.ExtraConfig[key]
	return actual, !exists || strings.TrimSpace(actual) == ""
}

// CheckFixed는 3-2 고정값 체크를 수행한다.
func CheckFixed(vm model.VMInfo) []model.Finding {
	var findings []model.Finding
	for _, f := range fixedExpect {
		if ExpectNoLpage {
			finding := model.Finding{VM: vm.Name, Source: "-", Key: f.Key, Expected: model.ExpectNone, Result: "OK"}
			if actual, deleted := isDeleted(vm, f.Key); !deleted {
				finding.Actual = actual
				finding.Result = "FAIL"
			}
			findings = append(findings, finding)
			continue
		}
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

var anyAffinityKeyRe = regexp.MustCompile(`^sched\.(cpu|vcpu\d+)\.affinity$`)

// CheckAffinityAbsent는 affinity 값이 하나도 남아 있지 않은지 본다. 남은 키마다 FAIL, 없으면 OK 한 줄.
func CheckAffinityAbsent(vm model.VMInfo, source string) []model.Finding {
	var findings []model.Finding
	for key, val := range vm.ExtraConfig {
		if anyAffinityKeyRe.MatchString(key) && strings.TrimSpace(val) != "" {
			findings = append(findings, model.Finding{VM: vm.Name, Source: source, Key: key, Expected: model.ExpectNone, Actual: val, Result: "FAIL"})
		}
	}
	if len(findings) == 0 {
		return []model.Finding{{VM: vm.Name, Source: source, Key: "sched.vcpuN.affinity(전체)", Expected: model.ExpectNone, Result: "OK"}}
	}
	return findings
}
