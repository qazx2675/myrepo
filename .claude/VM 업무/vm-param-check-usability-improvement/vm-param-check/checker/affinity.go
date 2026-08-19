package checker

import (
	"fmt"
	"sort"
	"strconv"
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
			if sameAffinity(actual, exp) {
				f.Result = "OK"
			} else {
				f.Result = "FAIL"
			}
		}
		findings = append(findings, f)
	}
	return findings
}

// sameAffinity는 affinity 값 두 개가 같은 설정인지 판정한다.
//
// sched.vcpuN.affinity는 "이 vCPU를 돌릴 수 있는 물리 CPU 목록"이라 순서에 의미가 없다
// (우선순위가 아니고, ESXi 스케줄러가 그 안에서 알아서 배치한다). 그래서 "31,29,27"과
// "27,29,31"은 동일한 설정으로 봐야 한다 — 순서 때문에 FAIL이 나면 실제로는 정상인 VM을
// 고치려 들게 된다.
//
// 다만 개수는 맞춰서 본다(다중집합 비교) — "16,17"과 "16,17,17"을 같다고 하면 잘못 들어간
// 중복을 조용히 통과시켜 버린다.
//
// 값이 숫자가 아닌 경우(예: "all")는 쪼개서 정렬한 뒤 문자열로 비교한다.
func sameAffinity(a, b string) bool {
	return affinityKey(a) == affinityKey(b)
}

// affinityKey는 비교용 정규화 문자열을 만든다. 공백을 없애고, 쉼표로 쪼개서
// 숫자면 숫자 순으로(그래서 2가 10보다 앞), 아니면 문자열 순으로 정렬해 다시 잇는다.
func affinityKey(s string) string {
	parts := strings.Split(strings.TrimSpace(s), ",")
	tokens := make([]string, 0, len(parts))
	allNum := true
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		tokens = append(tokens, p)
		if allNum {
			n, err := strconv.Atoi(p)
			if err != nil {
				allNum = false
			} else {
				nums = append(nums, n)
			}
		}
	}
	if allNum {
		sort.Ints(nums)
		strs := make([]string, len(nums))
		for i, n := range nums {
			strs[i] = strconv.Itoa(n)
		}
		return strings.Join(strs, ",")
	}
	sort.Strings(tokens)
	return strings.Join(tokens, ",")
}
