package checker

import (
	"strconv"
	"strings"

	"vm-param-check/model"
)

const numaKey = "numa.vcpu.maxPerVirtualNode"
const coresKey = "cpuid.coresPerSocket"

// CoresExpect/NumaExpect는 그룹별(ev01/미분류는 Base 필수, ev02·ev03는 옵션) 기대값이다.
// SharesExpect와 동일한 패턴 — EV02/EV03이 nil이면 "해당 그룹 옵션이 아예 주어지지 않음".
type CoresExpect struct {
	Base int
	EV02 *int
	EV03 *int
}

type NumaExpect struct {
	Base int
	EV02 *int
	EV03 *int
}

// CheckTopology는 3-3 CPU 토폴로지 체크(코어수/NUMA 각 2곳 비교)를 수행한다.
// group/singleVMMode로 ev02/ev03 그룹 옵션 유무에 따라 스킵 여부를 정한다
// (checkShares와 동일한 3-0/3-5 규칙 — ev01/미분류는 Base로 항상 체크,
// ev02/ev03는 해당 옵션이 있고 singleVMMode가 아닐 때만 체크).
// isVcsim이 true이면 vcsim에서 지원하지 않는 필드를 "[미지원]"으로 표시한다.
//
// NUMA "UI 값"에 대한 참고: govmomi vim25 타입 조사 결과, vSphere Client의
// "CPU 토폴로지 > NUMA 노드당 코어 수" UI는 ExtraConfig의 numa.vcpu.maxPerVirtualNode가
// 아니라 config.numaInfo.coresPerNumaNode(VirtualMachineConfigInfo.NumaInfo.CoresPerNumaNode)를
// 읽고 쓴다 — vSphere API 8.0.0.1+ 에서 노출되는 별개 필드다(실측 vCenter는 8.0.3.0으로 지원 확인).
// 이 값이 nil이면 해당 VM에 커스텀 vNUMA 설정이 없다는 뜻이라 "설정없음"으로 처리한다.
// vcsim에서는 이 필드를 미지원하므로 "[미지원]"으로 표시한다.
func CheckTopology(vm model.VMInfo, cores CoresExpect, numa NumaExpect, group string, singleVMMode bool, isVcsim bool) []model.Finding {
	var findings []model.Finding

	if expectCores, ok := resolveGroupExpect(cores.Base, cores.EV02, cores.EV03, group, singleVMMode); ok {
		// 코어수 (1) Advanced Config: cpuid.coresPerSocket
		// vcsim에서는 미지원 필드이므로 "[미지원]"으로 표시.
		coresAdvActual, coresAdvExists := vm.ExtraConfig[coresKey]
		f1 := model.Finding{VM: vm.Name, Source: "-", Key: coresKey, Expected: strconv.Itoa(expectCores)}
		if isVcsim {
			f1.Result = "미지원"
		} else if !coresAdvExists {
			f1.Result = "설정없음"
		} else {
			f1.Actual = coresAdvActual
			if n, err := strconv.Atoi(strings.TrimSpace(coresAdvActual)); err == nil && n == expectCores {
				f1.Result = "OK"
			} else {
				f1.Result = "FAIL"
			}
		}
		findings = append(findings, f1)

		// 코어수 (2) CPU 토폴로지 UI: VirtualMachineConfigInfo.Hardware.NumCoresPerSocket
		f2 := model.Finding{
			VM: vm.Name, Source: "-",
			Key:      "hardware.numCoresPerSocket (CPU 토폴로지 UI)",
			Expected: strconv.Itoa(expectCores),
			Actual:   strconv.Itoa(int(vm.NumCoresPerSocket)),
		}
		if int(vm.NumCoresPerSocket) == expectCores {
			f2.Result = "OK"
		} else {
			f2.Result = "FAIL"
		}
		findings = append(findings, f2)
	}

	if expectNuma, ok := resolveGroupExpect(numa.Base, numa.EV02, numa.EV03, group, singleVMMode); ok {
		// NUMA (1) Advanced Config: numa.vcpu.maxPerVirtualNode
		numaActual, numaExists := vm.ExtraConfig[numaKey]
		f3 := model.Finding{VM: vm.Name, Source: "-", Key: numaKey, Expected: strconv.Itoa(expectNuma)}
		if !numaExists {
			f3.Result = "설정없음"
		} else {
			f3.Actual = numaActual
			if n, err := strconv.Atoi(strings.TrimSpace(numaActual)); err == nil && n == expectNuma {
				f3.Result = "OK"
			} else {
				f3.Result = "FAIL"
			}
		}
		findings = append(findings, f3)

		// NUMA (2) CPU 토폴로지 UI: config.numaInfo.coresPerNumaNode (ExtraConfig와 별개 API 필드)
		// vcsim에서는 미지원 필드이므로 "[미지원]"으로 표시.
		f4 := model.Finding{
			VM: vm.Name, Source: "-",
			Key:      "config.numaInfo.coresPerNumaNode (CPU 토폴로지 UI)",
			Expected: strconv.Itoa(expectNuma),
		}
		if isVcsim {
			f4.Result = "미지원"
		} else if vm.NumaAutoCoresPerNode != nil && *vm.NumaAutoCoresPerNode {
			f4.Actual = "자동(전원을 켤 때 할당됨)"
			f4.Result = "설정없음"
			f4.Note = "NUMA 노드당 코어 수가 자동(Auto)으로 설정되어 있어 전원을 켤 때마다 재계산됨 — 고정값이 아니므로 수동 설정 필요"
		} else if vm.NumaCoresPerNode == nil {
			f4.Result = "설정없음"
		} else {
			f4.Actual = strconv.Itoa(int(*vm.NumaCoresPerNode))
			if int(*vm.NumaCoresPerNode) == expectNuma {
				f4.Result = "OK"
			} else {
				f4.Result = "FAIL"
			}
		}
		findings = append(findings, f4)
	}

	return findings
}
