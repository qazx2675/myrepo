package checker

import (
	"strconv"
	"strings"

	"vm-param-check/model"
)

const numaKey = "numa.vcpu.maxPerVirtualNode"
const coresKey = "cpuid.coresPerSocket"

// CheckTopology는 3-3 CPU 토폴로지 체크(코어수/NUMA 각 2곳 비교)를 수행한다.
//
// NUMA "UI 값"에 대한 참고: govmomi vim25 타입 조사 결과, vSphere Client의
// "CPU 토폴로지 > NUMA 노드당 코어 수" UI는 ExtraConfig의 numa.vcpu.maxPerVirtualNode가
// 아니라 config.numaInfo.coresPerNumaNode(VirtualMachineConfigInfo.NumaInfo.CoresPerNumaNode)를
// 읽고 쓴다 — vSphere API 8.0.0.1+ 에서 노출되는 별개 필드다(실측 vCenter는 8.0.3.0으로 지원 확인).
// 이 값이 nil이면 해당 VM에 커스텀 vNUMA 설정이 없다는 뜻이라 "설정없음"으로 처리한다.
func CheckTopology(vm model.VMInfo, expectCores, expectNuma int) []model.Finding {
	var findings []model.Finding

	// 코어수 (1) Advanced Config: cpuid.coresPerSocket
	coresAdvActual, coresAdvExists := vm.ExtraConfig[coresKey]
	f1 := model.Finding{VM: vm.Name, Source: "-", Key: coresKey, Expected: strconv.Itoa(expectCores)}
	if !coresAdvExists {
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
	f4 := model.Finding{
		VM: vm.Name, Source: "-",
		Key:      "config.numaInfo.coresPerNumaNode (CPU 토폴로지 UI)",
		Expected: strconv.Itoa(expectNuma),
	}
	if vm.NumaCoresPerNode == nil {
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

	return findings
}
