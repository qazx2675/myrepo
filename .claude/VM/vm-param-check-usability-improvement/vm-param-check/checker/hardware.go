package checker

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"vm-param-check/model"
)

// SharesItem은 -shares-evNN 하나에 콤마로 나열 가능한 허용값 중 한 항목이다.
// Normal이 true면 "Level 자체가 normal인가"를 보고, 아니면 "Level이 custom이고
// ratio가 Ratio와 같은가"를 본다. 콤마로 여러 개(예: "4000,normal")를 주면 그 중
// 하나라도 맞으면 OK로 판정한다.
type SharesItem struct {
	Ratio  int
	Normal bool
}

// RatioShares는 ratio 숫자 하나만 허용값으로 갖는 SharesItem 목록을 만든다
// (데모/스케일테스트처럼 고정 정수 기대값을 코드에서 직접 구성할 때 사용).
func RatioShares(ratio int) []SharesItem {
	return []SharesItem{{Ratio: ratio}}
}

// SharesExpect는 그룹별(ev01 필수/ev02·ev03 옵션) 기대 CPU Shares 허용값 목록이다.
// EV02/EV03가 빈 슬라이스(nil)면 "해당 그룹 옵션이 아예 주어지지 않음"을 의미한다.
type SharesExpect struct {
	EV01 []SharesItem
	EV02 []SharesItem
	EV03 []SharesItem
}

// CPUExpect/MemExpect/DiskExpect는 SharesExpect와 동일한 패턴 — Base는 ev01/미분류
// VM에 적용되는 필수값, EV02/EV03는 옵션(nil이면 해당 그룹 체크 스킵).
type CPUExpect struct {
	Base int
	EV02 *int
	EV03 *int
}

type MemExpect struct {
	Base int
	EV02 *int
	EV03 *int
}

// DiskExpect만 값이 슬라이스다 — 같은 스펙인데도 디스크 총량이 환산/파티션 차이로 1024,
// 1026처럼 몇 GB 갈리는 경우가 있어서, 허용값을 여러 개 둘 수 있게 했다.
// 하나만 쓸 거면 원소 1개 슬라이스를 주면 된다.
type DiskExpect struct {
	Base []int
	EV02 []int
	EV03 []int
}

// resolveGroupExpect는 그룹(ev01/ev02/ev03/미분류)에 따라 기대값을 정한다.
// ev01과 미분류("")는 항상 base를 그대로 쓴다(기존 동작 유지, 필수).
// ev02/ev03는 override가 있고 singleVMMode가 아닐 때만 쓰고, 없으면 ok=false로
// "이 항목은 체크 자체를 스킵"을 알린다 — checkShares/checkAffinity와 동일한 3-0/3-5 규칙.
func resolveGroupExpect(base int, ev02, ev03 *int, group string, singleVMMode bool) (int, bool) {
	switch group {
	case "ev02":
		if singleVMMode || ev02 == nil {
			return 0, false
		}
		return *ev02, true
	case "ev03":
		if singleVMMode || ev03 == nil {
			return 0, false
		}
		return *ev03, true
	default:
		return base, true
	}
}

// resolveGroupExpectList는 resolveGroupExpect와 같은 규칙의 슬라이스 버전이다(디스크 전용).
// ev02/ev03는 "옵션 없음"을 nil이 아니라 빈 슬라이스로도 표현할 수 있어서 len으로 판단한다.
func resolveGroupExpectList(base, ev02, ev03 []int, group string, singleVMMode bool) ([]int, bool) {
	switch group {
	case "ev02":
		if singleVMMode || len(ev02) == 0 {
			return nil, false
		}
		return ev02, true
	case "ev03":
		if singleVMMode || len(ev03) == 0 {
			return nil, false
		}
		return ev03, true
	default:
		return base, true
	}
}

// formatExpectList는 허용값 목록을 리포트에 적을 문자열로 만든다.
// 1개면 지금까지와 똑같이 숫자만 나오고(기존 CSV/화면과 동일), 여러 개면 "1024 또는 1026".
func formatExpectList(values []int) string {
	strs := make([]string, len(values))
	for i, v := range values {
		strs[i] = strconv.Itoa(v)
	}
	return strings.Join(strs, " 또는 ")
}

func containsInt(values []int, want int) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// CheckHardware는 3-4 가상 하드웨어 체크(vCPU/메모리/디스크/메모리예약/Shares)를 수행한다.
// group은 3-0 분류 결과("ev01"|"ev02"|"ev03"|""), singleVMMode는 이번 실행의 조사 대상이
// 총 1개인지 여부 — 계획서 3-0/3-4/3-5의 "VM이 1개뿐이면 ev02/ev03는 옵션이 있어도 스킵" 규칙 적용용.
// cpu/mem/disk도 shares와 동일하게 ev02/ev03 옵션이 없으면 그 항목만 스킵한다(있으면 체크, 없으면 패스).
// isVcsim이 true이면 vcsim에서 지원하지 않는 필드를 "[미지원]"으로 표시한다.
func CheckHardware(vm model.VMInfo, cpu CPUExpect, mem MemExpect, disk DiskExpect, shares SharesExpect, group string, singleVMMode bool, isVcsim bool) []model.Finding {
	var findings []model.Finding

	// vCPU
	if expectCPU, ok := resolveGroupExpect(cpu.Base, cpu.EV02, cpu.EV03, group, singleVMMode); ok {
		f := model.Finding{VM: vm.Name, Source: "-", Key: "config.hardware.numCPU", Expected: strconv.Itoa(expectCPU), Actual: strconv.Itoa(int(vm.NumCPU))}
		if int(vm.NumCPU) == expectCPU {
			f.Result = "OK"
		} else {
			f.Result = "FAIL"
		}
		findings = append(findings, f)
	}

	// 메모리 (GB)
	if expectMemGB, ok := resolveGroupExpect(mem.Base, mem.EV02, mem.EV03, group, singleVMMode); ok {
		actualMemGB := int(vm.MemoryMB) / 1024
		f := model.Finding{VM: vm.Name, Source: "-", Key: "config.hardware.memoryMB (GB 환산)", Expected: strconv.Itoa(expectMemGB), Actual: strconv.Itoa(actualMemGB)}
		if actualMemGB == expectMemGB {
			f.Result = "OK"
		} else {
			f.Result = "FAIL"
		}
		findings = append(findings, f)
	}

	// 디스크 (GB, 반올림)
	if expectDiskGB, ok := resolveGroupExpectList(disk.Base, disk.EV02, disk.EV03, group, singleVMMode); ok {
		actualDiskGB := int(math.Round(vm.DiskGB))
		f := model.Finding{VM: vm.Name, Source: "-", Key: "disk total capacity (GB 환산, 반올림)", Expected: formatExpectList(expectDiskGB), Actual: strconv.Itoa(actualDiskGB)}
		if containsInt(expectDiskGB, actualDiskGB) {
			f.Result = "OK"
		} else {
			f.Result = "FAIL"
		}
		findings = append(findings, f)
	}

	// 모든 게스트 메모리 예약 (고정 기대값: 항상 true) — 그룹과 무관, 기존 동작 유지.
	// vcsim에서는 미지원 필드이므로 "[미지원]"으로 표시.
	f := model.Finding{VM: vm.Name, Source: "-", Key: "config.memoryReservationLockedToMax (Reserve all guest memory)", Expected: "true"}
	if isVcsim {
		f.Result = "미지원"
	} else if vm.MemoryReservationLockedToMax == nil {
		f.Result = "설정없음"
	} else {
		f.Actual = strconv.FormatBool(*vm.MemoryReservationLockedToMax)
		if *vm.MemoryReservationLockedToMax {
			f.Result = "OK"
		} else {
			f.Result = "FAIL"
		}
	}
	findings = append(findings, f)

	// Shares (Ratio) — CPU/Memory 둘 다, 그룹별. ev01은 항상 필수, ev02/ev03는 해당 그룹
	// 옵션이 주어지고 singleVMMode가 아닐 때만 체크(계획서 3-0/3-5의 단일 VM 예외 규칙을 여기도 동일 적용).
	// 계획서가 CPU/메모리 Shares를 구분하지 않아서, --shares-evNN 값 하나를 두 항목(CPU/메모리)에
	// 동일한 기대값으로 적용한다 — 서로 다른 기대값이 필요하면 알려달라고 별도 보고.
	findings = append(findings, checkShares(vm, shares, group, singleVMMode)...)

	return findings
}

// resolveGroupExpectShares는 checkShares 전용 그룹 분기다. 기존 동작을 그대로 유지해
// group이 ev01/ev02/ev03가 아니면(미분류 "") shares는 아예 체크하지 않는다.
func resolveGroupExpectShares(ev01, ev02, ev03 []SharesItem, group string, singleVMMode bool) ([]SharesItem, bool) {
	switch group {
	case "ev01":
		return ev01, true
	case "ev02":
		if singleVMMode || len(ev02) == 0 {
			return nil, false // 옵션 없거나 단일 VM 예외 -> 스킵 (Finding 자체를 만들지 않음)
		}
		return ev02, true
	case "ev03":
		if singleVMMode || len(ev03) == 0 {
			return nil, false
		}
		return ev03, true
	default:
		return nil, false // ev01/02/03 어디에도 속하지 않는 VM은 shares 비교 대상 아님
	}
}

// formatSharesItems는 허용값 목록을 리포트에 적을 문자열로 만든다("4000 또는 normal").
func formatSharesItems(items []SharesItem) string {
	strs := make([]string, len(items))
	for i, it := range items {
		if it.Normal {
			strs[i] = "normal"
		} else {
			strs[i] = strconv.Itoa(it.Ratio)
		}
	}
	return strings.Join(strs, " 또는 ")
}

// sharesMatch는 허용값 목록 중 하나라도 실제 level/ratio와 맞으면 true다.
// normal 항목은 level=="normal"인지만 보고, ratio 항목은 level=="custom"이고
// ratio가 같은지를 본다(레벨이 custom이 아니면 ratio 숫자 자체가 무의미하므로 매칭 안 됨).
func sharesMatch(items []SharesItem, level string, ratio int) bool {
	for _, it := range items {
		if it.Normal {
			if level == "normal" {
				return true
			}
			continue
		}
		if level == "custom" && ratio == it.Ratio {
			return true
		}
	}
	return false
}

func checkShares(vm model.VMInfo, shares SharesExpect, group string, singleVMMode bool) []model.Finding {
	items, ok := resolveGroupExpectShares(shares.EV01, shares.EV02, shares.EV03, group, singleVMMode)
	if !ok {
		return nil
	}
	expected := formatSharesItems(items)

	cpu := model.Finding{VM: vm.Name, Source: group, Key: "cpuAllocation.shares (CPU Shares)", Expected: expected}
	if vm.CPUSharesLevel == "custom" {
		cpu.Actual = fmt.Sprintf("level=custom (ratio=%d)", int(vm.CPUShares))
	} else {
		cpu.Actual = fmt.Sprintf("level=%s", vm.CPUSharesLevel)
	}
	if sharesMatch(items, vm.CPUSharesLevel, int(vm.CPUShares)) {
		cpu.Result = "OK"
	} else {
		cpu.Result = "FAIL"
	}

	mem := model.Finding{VM: vm.Name, Source: group, Key: "memoryAllocation.shares (Memory Shares)", Expected: expected}
	if vm.MemorySharesLevel == "custom" {
		mem.Actual = fmt.Sprintf("level=custom (ratio=%d)", int(vm.MemoryShares))
	} else {
		mem.Actual = fmt.Sprintf("level=%s", vm.MemorySharesLevel)
	}
	if sharesMatch(items, vm.MemorySharesLevel, int(vm.MemoryShares)) {
		mem.Result = "OK"
	} else {
		mem.Result = "FAIL"
	}

	return []model.Finding{cpu, mem}
}
