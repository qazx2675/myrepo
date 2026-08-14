package checker

import (
	"fmt"
	"math"
	"strconv"

	"vm-param-check/model"
)

// SharesExpect는 그룹별(ev01 필수/ev02·ev03 옵션) 기대 CPU Shares(ratio) 값이다.
// 값이 nil이면 "해당 그룹 옵션이 아예 주어지지 않음"을 의미한다.
type SharesExpect struct {
	EV01 int
	EV02 *int
	EV03 *int
}

// CheckHardware는 3-4 가상 하드웨어 체크(vCPU/메모리/디스크/메모리예약/Shares)를 수행한다.
// group은 3-0 분류 결과("ev01"|"ev02"|"ev03"|""), singleVMMode는 이번 실행의 조사 대상이
// 총 1개인지 여부 — 계획서 3-0/3-4/3-5의 "VM이 1개뿐이면 ev02/ev03는 옵션이 있어도 스킵" 규칙 적용용.
func CheckHardware(vm model.VMInfo, expectCPU, expectMemGB, expectDiskGB int, shares SharesExpect, group string, singleVMMode bool) []model.Finding {
	var findings []model.Finding

	// vCPU
	f := model.Finding{VM: vm.Name, Source: "-", Key: "config.hardware.numCPU", Expected: strconv.Itoa(expectCPU), Actual: strconv.Itoa(int(vm.NumCPU))}
	if int(vm.NumCPU) == expectCPU {
		f.Result = "OK"
	} else {
		f.Result = "FAIL"
	}
	findings = append(findings, f)

	// 메모리 (GB)
	actualMemGB := int(vm.MemoryMB) / 1024
	f = model.Finding{VM: vm.Name, Source: "-", Key: "config.hardware.memoryMB (GB 환산)", Expected: strconv.Itoa(expectMemGB), Actual: strconv.Itoa(actualMemGB)}
	if actualMemGB == expectMemGB {
		f.Result = "OK"
	} else {
		f.Result = "FAIL"
	}
	findings = append(findings, f)

	// 디스크 (GB, 반올림)
	actualDiskGB := int(math.Round(vm.DiskGB))
	f = model.Finding{VM: vm.Name, Source: "-", Key: "disk total capacity (GB 환산, 반올림)", Expected: strconv.Itoa(expectDiskGB), Actual: strconv.Itoa(actualDiskGB)}
	if actualDiskGB == expectDiskGB {
		f.Result = "OK"
	} else {
		f.Result = "FAIL"
	}
	findings = append(findings, f)

	// 모든 게스트 메모리 예약 (고정 기대값: 항상 true)
	f = model.Finding{VM: vm.Name, Source: "-", Key: "config.memoryReservationLockedToMax (Reserve all guest memory)", Expected: "true"}
	if vm.MemoryReservationLockedToMax == nil {
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
