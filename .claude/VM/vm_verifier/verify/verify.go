// Package verify는 vCenter vNIC MAC과 DHCP 예약 MAC을 대조해서 오설치(DHCP MAC
// 오기입으로 인한 역설치)를 탐지한다. VM 생성 직후(파워온 전)에 쓰는 도구라 Guest OS
// 상태에 의존하는 검증(hostname/IP/DNS/UUID 이력)은 하지 않는다 — 이 시점엔 애초에
// 확인할 게스트 정보 자체가 없다.
package verify

import (
	"fmt"
	"strings"

	"vm-verifier/dhcp"
	"vm-verifier/vc"
)

type Status string

const (
	Pass Status = "PASS"
	Fail Status = "FAIL"
)

type Result struct {
	Hostname string
	Status   Status
	Detail   string
}

// ContainsFold는 list 안에 target과 대소문자 무시하고 일치하는 값이 있는지 본다.
// main.go의 교차 스왑 탐지(같은 BM 그룹 형제끼리 MAC 대조)에서도 재사용한다.
func ContainsFold(list []string, target string) bool {
	for _, v := range list {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}

// Check는 hostname 1개(예: svr01ev01)의 vCenter vNIC MAC이 DHCP 예약 MAC과 일치하는지 본다.
// dhcpRecord/dhcpErr: DNS로 대역을 자동 판별해 읽은(dhcp.Resolve) 이 호스트의 정적 MAC/IP.
// dhcpErr가 있으면 DNS 조회/파일 로드/호스트 블록 중 하나를 못 찾은 것이라 무조건 Fail.
// swapNote: 같은 BM 그룹 형제와 MAC이 뒤바뀐 교차 설치(역설치)가 확인되면 그 설명, 아니면 "".
func Check(hostname string, dhcpRecord dhcp.Record, dhcpErr error, swapNote string, vmInfo vc.VMInfo) Result {
	switch {
	case dhcpErr != nil:
		return Result{hostname, Fail, dhcpErr.Error()}
	case ContainsFold(vmInfo.MACs, dhcpRecord.MAC):
		return Result{hostname, Pass, dhcpRecord.MAC}
	case swapNote != "":
		return Result{hostname, Fail,
			fmt.Sprintf("DHCP=%s, vCenter vNIC=%v — %s", dhcpRecord.MAC, vmInfo.MACs, swapNote)}
	default:
		return Result{hostname, Fail,
			fmt.Sprintf("DHCP=%s, vCenter vNIC=%v", dhcpRecord.MAC, vmInfo.MACs)}
	}
}
