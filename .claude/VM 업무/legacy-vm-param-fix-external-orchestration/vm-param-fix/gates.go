// gates.go: 실제 설정 변경 전에 반드시 통과해야 하는 두 안전장치.
// - 그룹 동질성: ev01끼리, ev02끼리, ev03끼리 vCPU/코어수/메모리/디스크/Shares/NUMA/HT가
//   전부 같아야 하고, ev01/ev02/ev03 그룹 간 VM 대수도 서로 같아야 함
// - 전원 OFF: 대상 VM이 전부 꺼져 있어야 함 (lpage 태그가 하드웨어 토폴로지를 직접 바꾸기 때문)
package main

import (
	"fmt"
	"sort"
	"strings"
)

// checkHomogeneity는 target 안의 VM들을 ev01/ev02/ev03 그룹으로 나눠서, 같은 그룹 안의
// 모든 VM이 NumCPU/코어수/메모리/디스크/CPU Shares/NUMA/HT가 전부 동일한지, 그리고
// ev01/ev02/ev03 그룹 간 VM 대수가 서로 같은지(1:1:1 구성인지) 확인한다.
// (참고: vm-param-check는 ev01과 ev02가 서로 다른 기대값(-cpu-ev02 등)을 가질 수 있지만,
// "같은 그룹 안"에서는 여전히 전부 동일해야 한다는 전제는 그대로다 — 이 검증은 그 전제가
// 실제로 vCenter 상에서도 깨지지 않았는지(예: 누군가 중간에 손으로 바꿔놨는지) 재확인하는 역할이다.)
func checkHomogeneity(target map[string]bool, specs map[string]VMSpec) error {
	groups := map[string][]VMSpec{} // "ev01" | "ev02" | "ev03" -> specs
	for vm := range target {
		spec, ok := specs[vm]
		if !ok || !spec.Found {
			return fmt.Errorf("VM %q을(를) vCenter에서 찾을 수 없습니다", vm)
		}
		grp := groupOf(vm)
		groups[grp] = append(groups[grp], spec)
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
		specs := groups[grp]
		base := specs[0]
		for _, s := range specs[1:] {
			if diff := diffSpec(base, s); diff != "" {
				return fmt.Errorf("%s 그룹 내 스펙 불일치: %q(%s)와 %q(%s)가 다릅니다",
					grp, base.Name, describeSpec(base), s.Name, describeSpec(s)+" — "+diff)
			}
		}
	}
	return nil
}

// checkGroupCounts는 ev01/ev02/ev03 그룹이 둘 이상 섞여 있을 때 각 그룹의 VM 대수가
// 서로 같은지 확인한다("기타"는 접미사가 없는 VM이라 대수 비교 대상에서 제외).
func checkGroupCounts(groups map[string][]VMSpec, groupNames []string) error {
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

func groupOf(vmName string) string {
	switch {
	case strings.Contains(vmName, "ev01"):
		return "ev01"
	case strings.Contains(vmName, "ev02"):
		return "ev02"
	case strings.Contains(vmName, "ev03"):
		return "ev03"
	default:
		return "기타"
	}
}

func diffSpec(a, b VMSpec) string {
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
	if round(a.DiskGB) != round(b.DiskGB) {
		diffs = append(diffs, fmt.Sprintf("디스크GB %d≠%d", round(a.DiskGB), round(b.DiskGB)))
	}
	if a.CPUSharesLevel != b.CPUSharesLevel || a.CPUShares != b.CPUShares {
		diffs = append(diffs, fmt.Sprintf("CPU Shares %s/%d≠%s/%d", a.CPUSharesLevel, a.CPUShares, b.CPUSharesLevel, b.CPUShares))
	}
	if a.NumaSetting != b.NumaSetting {
		diffs = append(diffs, fmt.Sprintf("NUMA(numa.vcpu.maxPerVirtualNode) %q≠%q", a.NumaSetting, b.NumaSetting))
	}
	if a.HTOn != b.HTOn {
		diffs = append(diffs, fmt.Sprintf("HT %v≠%v", a.HTOn, b.HTOn))
	}
	return strings.Join(diffs, ", ")
}

func describeSpec(s VMSpec) string {
	return fmt.Sprintf("vCPU=%d 코어/소켓=%d 메모리MB=%d 디스크GB=%d NUMA=%q HT=%v",
		s.NumCPU, s.NumCoresPerSocket, s.MemoryMB, round(s.DiskGB), s.NumaSetting, s.HTOn)
}

func round(f float64) int {
	return int(f + 0.5)
}

// checkAllPoweredOff는 target 안의 VM이 단 한 대라도 켜져 있으면 즉시 에러를 반환한다.
func checkAllPoweredOff(target map[string]bool, specs map[string]VMSpec) error {
	var poweredOn []string
	for vm := range target {
		spec, ok := specs[vm]
		if !ok || !spec.Found {
			return fmt.Errorf("VM %q을(를) vCenter에서 찾을 수 없습니다", vm)
		}
		if spec.PoweredOn {
			poweredOn = append(poweredOn, vm)
		}
	}
	if len(poweredOn) > 0 {
		sort.Strings(poweredOn)
		return fmt.Errorf("아래 VM이 켜져 있어 작업할 수 없습니다: %s", strings.Join(poweredOn, ", "))
	}
	return nil
}
