package checker

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"vm-param-check/model"
)

// SharesExpect는 그룹별(ev01 필수/ev02·ev03 옵션) 기대 CPU Shares 값이다.
// EV02/EV03가 nil이면 "해당 그룹 옵션이 아예 주어지지 않음"을 의미한다.
//
// ev01만 Level이 Normal인 경우를 기대값으로 쓸 수 있다(EV01Normal). 현업에서 Shares를
// Custom ratio로 박지 않고 Normal 그대로 두는 스펙이 있어서, 그때는 ratio 숫자가 아니라
// Level 자체를 비교해야 한다. ev02/ev03는 지금까지처럼 ratio 숫자만 받는다.
type SharesExpect struct {
	EV01       int
	EV01Normal bool // true면 EV01 무시하고 "Level이 normal인가"로 판정
	EV02       *int
	EV03       *int
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

func checkShares(vm model.VMInfo, shares SharesExpect, group string, singleVMMode bool) []model.Finding {
	var expected *int
	switch group {
	case "ev01":
		if shares.EV01Normal {
			return checkSharesNormal(vm, group)
		}
		v := shares.EV01
		expected = &v
	case "ev02":
		if singleVMMode || shares.EV02 == nil {
			return nil // 옵션 없거나 단일 VM 예외 -> 스킵 (Finding 자체를 만들지 않음)
		}
		expected = shares.EV02
	case "ev03":
		if singleVMMode || shares.EV03 == nil {
			return nil
		}
		expected = shares.EV03
	default:
		return nil // ev01/02/03 어디에도 속하지 않는 VM은 shares 비교 대상 아님
	}

	cpu := model.Finding{VM: vm.Name, Source: group, Key: "cpuAllocation.shares (CPU Shares Ratio)", Expected: strconv.Itoa(*expected)}
	if vm.CPUSharesLevel != "custom" {
		cpu.Actual = fmt.Sprintf("level=%s (custom 아님, ratio 값 무의미)", vm.CPUSharesLevel)
		cpu.Result = "FAIL"
	} else {
		cpu.Actual = strconv.Itoa(int(vm.CPUShares))
		if int(vm.CPUShares) == *expected {
			cpu.Result = "OK"
		} else {
			cpu.Result = "FAIL"
		}
	}

	mem := model.Finding{VM: vm.Name, Source: group, Key: "memoryAllocation.shares (Memory Shares Ratio)", Expected: strconv.Itoa(*expected)}
	if vm.MemorySharesLevel != "custom" {
		mem.Actual = fmt.Sprintf("level=%s (custom 아님, ratio 값 무의미)", vm.MemorySharesLevel)
		mem.Result = "FAIL"
	} else {
		mem.Actual = strconv.Itoa(int(vm.MemoryShares))
		if int(vm.MemoryShares) == *expected {
			mem.Result = "OK"
		} else {
			mem.Result = "FAIL"
		}
	}

	return []model.Finding{cpu, mem}
}

// checkSharesNormal은 -shares-ev01=normal 일 때의 판정이다. ratio 숫자는 VM 사양에 따라
// vCenter가 알아서 계산하므로 비교 대상이 아니고, Level이 normal인지만 본다(CPU/메모리 둘 다).
func checkSharesNormal(vm model.VMInfo, group string) []model.Finding {
	cpu := model.Finding{VM: vm.Name, Source: group, Key: "cpuAllocation.shares (CPU Shares Level)", Expected: "normal"}
	cpu.Actual = fmt.Sprintf("level=%s", vm.CPUSharesLevel)
	if vm.CPUSharesLevel == "normal" {
		cpu.Result = "OK"
	} else {
		cpu.Result = "FAIL"
	}

	mem := model.Finding{VM: vm.Name, Source: group, Key: "memoryAllocation.shares (Memory Shares Level)", Expected: "normal"}
	mem.Actual = fmt.Sprintf("level=%s", vm.MemorySharesLevel)
	if vm.MemorySharesLevel == "normal" {
		mem.Result = "OK"
	} else {
		mem.Result = "FAIL"
	}

	return []model.Finding{cpu, mem}
}
