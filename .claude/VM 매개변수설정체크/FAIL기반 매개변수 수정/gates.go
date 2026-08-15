// gates.go: 실제 설정 변경 전에 반드시 통과해야 하는 두 안전장치.
// - 그룹 동질성: ev01끼리, ev02끼리 스펙이 전부 같아야 함
// - 전원 OFF: 대상 VM이 전부 꺼져 있어야 함 (lpage 태그가 하드웨어 토폴로지를 직접 바꾸기 때문)
package main

import (
	"fmt"
	"sort"
	"strings"
)

// checkHomogeneity는 target 안의 VM들을 ev01/ev02/ev03 그룹으로 나눠서, 같은 그룹 안의
// 모든 VM이 NumCPU/코어수/메모리/디스크/CPU Shares가 전부 동일한지 확인한다.
// vm-param-check 자체가 이미 전체 VM에 단일 기대값(--cpu/--cores 등)을 적용하는 도구라,
// 이 도구에 들어온 CSV는 원래 "동일해야 하는" 대상만 담고 있는 게 정상이다 — 이 검증은
// 그 전제가 실제로 vCenter 상에서도 깨지지 않았는지(예: 누군가 중간에 손으로 바꿔놨는지)
// 재확인하는 역할이다.
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
	return strings.Join(diffs, ", ")
}

func describeSpec(s VMSpec) string {
	return fmt.Sprintf("vCPU=%d 코어/소켓=%d 메모리MB=%d 디스크GB=%d",
		s.NumCPU, s.NumCoresPerSocket, s.MemoryMB, round(s.DiskGB))
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
