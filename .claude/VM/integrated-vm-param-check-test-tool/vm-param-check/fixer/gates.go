// gates.go: 실제 설정을 바꾸기 전에 반드시 통과해야 하는 두 안전장치.
//   - 그룹 동질성: ev01끼리, ev02끼리, ev03끼리 스펙이 전부 같아야 하고 그룹 간 대수도 같아야 함
//   - 전원 OFF: 대상 VM이 전부 꺼져 있어야 함 (CPU 토폴로지를 직접 바꾸기 때문)
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
	infoByName := map[string]model.VMInfo{}
	for _, vm := range vms {
		infoByName[vm.Name] = vm
	}

	var homogErr, powerErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		homogErr = checkHomogeneity(targets, infoByName)
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

// checkHomogeneity는 대상 VM을 ev01/ev02/ev03 그룹으로 나눠서, 같은 그룹 안의 모든 VM이
// vCPU/코어수/메모리/디스크/CPU Shares/NUMA/HT가 전부 동일한지, 그리고 그룹 간 VM 대수가
// 서로 같은지(1:1:1 구성인지) 확인한다.
func checkHomogeneity(targets []string, infoByName map[string]model.VMInfo) error {
	groups := map[string][]model.VMInfo{}
	for _, name := range targets {
		info, ok := infoByName[name]
		if !ok {
			return fmt.Errorf("VM %q의 조회 정보가 없습니다", name)
		}
		grp := GroupOf(name)
		groups[grp] = append(groups[grp], info)
	}

	var groupNames []string
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)

	if err := checkGroupCounts(groups, groupNames); err != nil {
		return err
	}

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

// checkGroupCounts는 ev01/ev02/ev03 그룹이 둘 이상 섞여 있을 때 각 그룹의 VM 대수가
// 서로 같은지 확인한다("기타"는 접미사가 없는 VM이라 대수 비교 대상에서 제외).
func checkGroupCounts(groups map[string][]model.VMInfo, groupNames []string) error {
	var known []string
	for _, g := range groupNames {
		if g == "ev01" || g == "ev02" || g == "ev03" {
			known = append(known, g)
		}
	}
	if len(known) < 2 {
		return nil
	}
	baseCount := len(groups[known[0]])
	var parts []string
	mismatch := false
	for _, g := range known {
		parts = append(parts, fmt.Sprintf("%s=%d대", g, len(groups[g])))
		if len(groups[g]) != baseCount {
			mismatch = true
		}
	}
	if mismatch {
		return fmt.Errorf("그룹별 VM 대수가 다릅니다: %s", strings.Join(parts, ", "))
	}
	return nil
}

func diffSpec(a, b model.VMInfo) string {
	var diffs []string
	if a.NumCPU != b.NumCPU {
		diffs = append(diffs, fmt.Sprintf("vCPU %d≠%d", a.NumCPU, b.NumCPU))
	}
	if a.NumCoresPerSocket != b.NumCoresPerSocket {
		diffs = append(diffs, fmt.Sprintf("코어/소켓 %d≠%d", a.NumCoresPerSocket, b.NumCoresPerSocket))
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
	if numaSetting(a) != numaSetting(b) {
		diffs = append(diffs, fmt.Sprintf("NUMA(numa.vcpu.maxPerVirtualNode) %q≠%q", numaSetting(a), numaSetting(b)))
	}
	if htOn(a) != htOn(b) {
		diffs = append(diffs, fmt.Sprintf("HT %v≠%v", htOn(a), htOn(b)))
	}
	return strings.Join(diffs, ", ")
}

func describeSpec(v model.VMInfo) string {
	return fmt.Sprintf("vCPU=%d 코어/소켓=%d 메모리MB=%d 디스크GB=%d NUMA=%q HT=%v",
		v.NumCPU, v.NumCoresPerSocket, v.MemoryMB, roundGB(v.DiskGB), numaSetting(v), htOn(v))
}

func numaSetting(v model.VMInfo) string {
	return v.ExtraConfig["numa.vcpu.maxPerVirtualNode"]
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
