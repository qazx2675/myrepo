package checker

import (
	"strconv"

	"vm-param-check/model"
)

const expectedPowerPolicy = "High Performance"

// CheckHostPower는 3-1 호스트 전원 관리 체크 — 기대값은 항상 High Performance로 고정.
func CheckHostPower(vm model.VMInfo) model.Finding {
	f := model.Finding{VM: vm.Name, Source: "host", Key: "host power policy", Expected: expectedPowerPolicy}
	if vm.HostName == "" {
		f.Result = "설정없음"
		f.Note = "VM의 runtime.host를 확인할 수 없음"
		return f
	}
	f.Actual = vm.HostPowerPolicy
	f.Note = "host=" + vm.HostName
	if vm.HostPowerPolicy == expectedPowerPolicy {
		f.Result = "OK"
	} else {
		f.Result = "FAIL"
	}
	return f
}

// CheckNetwork는 3-6 네트워크 포트그룹 체크 — OK/FAIL 판정 없이 정보성으로만 기록한다.
// 포트그룹 이름은 어댑터의 커넥트/디스커넥트 상태와 무관하게 항상 조사하고, 그 상태도
// 비고란에 함께 남긴다 — 디스커넥트라고 어댑터 자체가 없는 게 아니기 때문이다.
func CheckNetwork(vm model.VMInfo) []model.Finding {
	if len(vm.Networks) == 0 {
		return []model.Finding{{VM: vm.Name, Source: "network", Key: "portgroup", Result: "설정없음", Note: "연결된 네트워크 어댑터 없음"}}
	}
	var findings []model.Finding
	for i, nic := range vm.Networks {
		findings = append(findings, model.Finding{
			VM: vm.Name, Source: "network",
			Key:    formatNicKey(i),
			Actual: nic.Portgroup,
			Result: "정보",
			Note:   connectNote(nic.Connected),
		})
	}
	return findings
}

func connectNote(connected bool) string {
	if connected {
		return "커넥트"
	}
	return "디스커넥트"
}

func formatNicKey(i int) string {
	return "network adapter " + strconv.Itoa(i+1) + " portgroup"
}
