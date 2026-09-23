// gates.go: 실제 설정을 바꾸기 전에 반드시 통과해야 하는 두 안전장치.
//   - 그룹 동질성: ev01끼리, ev02끼리, ev03끼리 스펙이 같아야 함(교정 대상끼리 비교)
//   - 전원 OFF: 대상 VM이 전부 꺼져 있어야 함 (CPU 토폴로지를 직접 바꾸기 때문)
//
// ev01/ev02/ev03 그룹 간 VM 대수가 맞는지(짝이 맞는지)는 게이트가 아니다. 다르면
// GroupCountWarning이 경고 문구만 돌려주고 교정은 그대로 진행한다.
//
// 검증에 필요한 값은 체크 단계에서 이미 조회한 model.VMInfo에 전부 들어있어서
// vCenter를 다시 조회하지 않는다.
package fixer

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"vm-param-check/model"
)

// CheckGates는 두 게이트를 동시에 돌리고, 하나라도 실패하면 에러를 돌려준다.
// 서로 독립적인 검사라 병렬로 실행한다.
func CheckGates(targets []string, vms []model.VMInfo) error {
	return CheckGatesBySpec(targets, vms, nil)
}

// CheckGatesBySpec은 CheckGates와 같지만, 동질성을 "같은 스펙의 같은 그룹"끼리만 비교한다.
// specOf는 VM 이름 -> 스펙 파일(-specRoot 로 VM마다 다른 스펙을 쓸 때). nil 이면 전부 한 스펙으로 본다.
// 스펙이 다르면 ev01끼리도 값이 다른 게 정상이라, 스펙을 구분하지 않으면 여러 스펙이 섞인 교정이 항상 막힌다.
func CheckGatesBySpec(targets []string, vms []model.VMInfo, specOf map[string]string) error {
	infoByName := map[string]model.VMInfo{}
	for _, vm := range vms {
		infoByName[vm.Name] = vm
	}

	var homogErr, powerErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		homogErr = checkHomogeneity(targets, infoByName, specOf)
	}()
	go func() {
		defer wg.Done()
		powerErr = checkAllPoweredOff(targets, infoByName)
	}()
	wg.Wait()

	if homogErr != nil {
		return fmt.Errorf("[동질성 검증 실패] %w", homogErr)
	}
	if powerErr != nil {
		return fmt.Errorf("[전원 OFF 검증 실패] %w", powerErr)
	}
	return nil
}

// checkHomogeneity는 같은 그룹 안의 교정 대상끼리 스펙이 같은지 확인한다(diffSpec 참고).
// specOf 가 있으면 스펙 파일별로 따로 묶는다(그룹 이름 앞에 "<스펙 파일> " 을 붙여 구분).
func checkHomogeneity(targets []string, infoByName map[string]model.VMInfo, specOf map[string]string) error {
	groups := map[string][]model.VMInfo{}
	for _, name := range targets {
		info, ok := infoByName[name]
		if !ok {
			return fmt.Errorf("VM %q의 조회 정보가 없습니다", name)
		}
		grp := GroupOf(name)
		if s := specOf[name]; s != "" {
			grp = s + " " + grp
		}
		groups[grp] = append(groups[grp], info)
	}
	groupNames := sortedGroupNames(groups)

	for _, grp := range groupNames {
		members := groups[grp]
		base := members[0]
		for _, m := range members[1:] {
			if diff := diffSpec(base, m); diff != "" {
				return fmt.Errorf("%s 그룹 내 스펙 불일치: %q(%s)와 %q(%s)가 다릅니다 — %s",
					grp, base.Name, describeSpec(base), m.Name, describeSpec(m), diff)
			}
		}
	}
	return nil
}

func sortedGroupNames(groups map[string][]model.VMInfo) []string {
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Strings(names)
	return names
}

// GroupCountWarning은 ev01/ev02/ev03 그룹이 둘 이상 섞여 있을 때 각 그룹의 VM 대수가
// 서로 다르면 경고 문구를 돌려준다(같거나 비교할 그룹이 하나뿐이면 "").
//
// 예전에는 이게 게이트라서 대수가 다르면 교정이 막혔다. 하지만 짝이 안 맞는다고 교정이
// 잘못되는 건 아니라서, 지금은 호출부가 이 문구를 출력만 하고 그대로 진행한다.
// 대수는 교정 대상이 아니라 PASS/FAIL 무관하게 조회한 VM 전부로 센다. "기타"(접미사가
// 없는 VM)는 비교 대상이 아니다.
func GroupCountWarning(vms []model.VMInfo) string {
	counts := map[string]int{}
	for _, vm := range vms {
		counts[GroupOf(vm.Name)]++
	}
	var known []string
	for _, g := range []string{"ev01", "ev02", "ev03"} {
		if counts[g] > 0 {
			known = append(known, g)
		}
	}
	if len(known) < 2 {
		return ""
	}
	var parts []string
	mismatch := false
	for _, g := range known {
		parts = append(parts, fmt.Sprintf("%s=%d대", g, counts[g]))
		if counts[g] != counts[known[0]] {
			mismatch = true
		}
	}
	if !mismatch {
		return ""
	}
	return fmt.Sprintf("그룹별 VM 대수가 다릅니다(PASS/FAIL 무관, 조회된 VM 전부 기준): %s — 짝이 맞지 않지만 교정은 그대로 진행합니다",
		strings.Join(parts, ", "))
}

// diffSpec은 두 VM의 스펙 차이를 돌려준다(같으면 빈 문자열).
//
// 코어/소켓(hardware.numCoresPerSocket)과 NUMA(numa.vcpu.maxPerVirtualNode)는 일부러
// 비교하지 않는다. 둘 다 이 도구가 -fix로 기대값에 맞춰 직접 고치는 항목이라, FAIL인 VM은
// 값이 다를 수밖에 없다 — 여기서 비교하면 "고치려는 바로 그 불일치" 때문에 교정이 막힌다.
// 교정 후에는 전부 같은 기대값이 되므로 동질성은 교정이 보장한다. 반대로 메모리/디스크/Shares는
// 이 도구가 못 고치는(수동조치) 항목이라 다르면 진짜로 다른 스펙이므로 계속 비교한다.
func diffSpec(a, b model.VMInfo) string {
	var diffs []string
	if a.NumCPU != b.NumCPU {
		diffs = append(diffs, fmt.Sprintf("vCPU %d≠%d", a.NumCPU, b.NumCPU))
	}
	if a.MemoryMB != b.MemoryMB {
		diffs = append(diffs, fmt.Sprintf("메모리MB %d≠%d", a.MemoryMB, b.MemoryMB))
	}
	if roundGB(a.DiskGB) != roundGB(b.DiskGB) {
		diffs = append(diffs, fmt.Sprintf("디스크GB %d≠%d", roundGB(a.DiskGB), roundGB(b.DiskGB)))
	}
	if a.CPUSharesLevel != b.CPUSharesLevel || a.CPUShares != b.CPUShares {
		diffs = append(diffs, fmt.Sprintf("CPU Shares %s/%d≠%s/%d", a.CPUSharesLevel, a.CPUShares, b.CPUSharesLevel, b.CPUShares))
	}
	if htOn(a) != htOn(b) {
		diffs = append(diffs, fmt.Sprintf("HT %v≠%v", htOn(a), htOn(b)))
	}
	return strings.Join(diffs, ", ")
}

// describeSpec은 diffSpec이 실제로 비교하는 값만 보여준다 — 비교하지 않는 코어/소켓·NUMA가
// 여기 섞여 있으면 값이 다른 걸 보고 그게 불일치 원인이라고 오해하기 쉽다.
func describeSpec(v model.VMInfo) string {
	return fmt.Sprintf("vCPU=%d 메모리MB=%d 디스크GB=%d HT=%v",
		v.NumCPU, v.MemoryMB, roundGB(v.DiskGB), htOn(v))
}

// htOn은 sched.vcpu0.affinity에 콤마로 구분된 pCPU가 2개 이상이면 HT 페어 핀닝으로 본다
// (기존 fail-based-param-fix 도구와 동일한 판정 방식).
func htOn(v model.VMInfo) bool {
	return strings.Contains(v.ExtraConfig["sched.vcpu0.affinity"], ",")
}

func roundGB(f float64) int {
	return int(math.Round(f))
}

// checkAllPoweredOff는 대상 VM이 단 한 대라도 켜져 있으면 에러를 반환한다.
func checkAllPoweredOff(targets []string, infoByName map[string]model.VMInfo) error {
	var poweredOn []string
	for _, name := range targets {
		info, ok := infoByName[name]
		if !ok {
			return fmt.Errorf("VM %q의 조회 정보가 없습니다", name)
		}
		if info.PoweredOn {
			poweredOn = append(poweredOn, name)
		}
	}
	if len(poweredOn) > 0 {
		sort.Strings(poweredOn)
		return fmt.Errorf("아래 VM이 켜져 있어 작업할 수 없습니다: %s", strings.Join(poweredOn, ", "))
	}
	return nil
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), substr)
}
