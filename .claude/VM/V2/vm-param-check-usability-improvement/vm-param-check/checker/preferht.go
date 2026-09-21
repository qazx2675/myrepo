package checker

import (
	"strings"

	"vm-param-check/model"
)

// preferHTKey는 numa.vcpu.preferHT 고급 설정 키다. 모든 VM에 공통으로 적용되는 단일 항목이라
// (그룹별로 값이 달라질 이유가 없어) NumaExpect 같은 그룹별 구조체 없이 문자열 기대값 하나만 받는다.
const preferHTKey = "numa.vcpu.preferHT"

// CheckPreferHT는 numa.vcpu.preferHT를 체크한다. expectPreferHT는 SPEC_DIR 스펙 파일(또는
// -preferHT 플래그)로 명시적으로 주어진 기대값이며, 보통 "TRUE"다.
//
// expectPreferHT가 빈 문자열이면(스펙/플래그 어디에도 이 옵션이 없으면) 이 항목 자체를
// 체크하지 않고 nil을 돌려준다 — 다른 항목처럼 "설정없음" Finding을 만들지 않는다. 옵션이
// 아예 주어지지 않았다는 것은 "이 VM 그룹에는 이 설정이 적용 대상이 아니다"는 뜻이지,
// "설정이 빠져 있다"는 뜻이 아니기 때문이다.
//
// expectPreferHT가 주어졌는데 VM의 ExtraConfig에 키가 없는 경우는 "설정없음"이 아니라
// FAIL로 취급한다 — 이 항목은 단순 TRUE/FALSE 토글이라 값이 없다는 것 자체가 곧
// "기대값(TRUE)이 아니다"라는 뜻이기 때문이다(코어수/NUMA처럼 "설정 자체가 없다"는 것이
// 별도로 의미 있는 진단 정보가 아님).
func CheckPreferHT(vm model.VMInfo, expectPreferHT string) []model.Finding {
	if strings.TrimSpace(expectPreferHT) == "" {
		return nil
	}

	f := model.Finding{VM: vm.Name, Source: "-", Key: preferHTKey, Expected: expectPreferHT}
	if actual, exists := vm.ExtraConfig[preferHTKey]; exists {
		f.Actual = actual
		if strings.EqualFold(strings.TrimSpace(actual), strings.TrimSpace(expectPreferHT)) {
			f.Result = "OK"
		} else {
			f.Result = "FAIL"
		}
	} else {
		f.Result = "FAIL"
	}
	return []model.Finding{f}
}
