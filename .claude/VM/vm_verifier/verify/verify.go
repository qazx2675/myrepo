// Package verify는 PLAN.md 5장의 5단계 정합성 검증 알고리즘을 구현한다.
// 한 단계가 실패해도 중단하지 않고 전 단계를 계속 실행해 결과를 함께 보고한다.
package verify

import (
	"fmt"
	"net"
	"strings"

	"vm-verifier/dhcp"
	"vm-verifier/vc"
)

type Status string

const (
	Pass         Status = "PASS"
	Fail         Status = "FAIL"
	Warn         Status = "WARN"
	Inconclusive Status = "INCONCLUSIVE"
)

type StepResult struct {
	Step   int
	Name   string
	Status Status
	Detail string
}

type Result struct {
	Hostname string
	Steps    []StepResult
}

// Overall은 FAIL이 하나라도 있으면 FAIL, 없고 WARN이 있으면 WARN, 아니면 PASS.
func (r Result) Overall() Status {
	overall := Pass
	for _, s := range r.Steps {
		if s.Status == Fail {
			return Fail
		}
		if s.Status == Warn {
			overall = Warn
		}
	}
	return overall
}

// ContainsFold는 list 안에 target과 대소문자 무시하고 일치하는 값이 있는지 본다.
// main.go의 교차 스왑 탐지(같은 BM 그룹 형제끼리 MAC 대조)에서도 재사용한다.
func ContainsFold(list []string, target string) bool {
	return containsFold(list, target)
}

func containsFold(list []string, target string) bool {
	for _, v := range list {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}

// Check는 hostname 1개(예: svr01ev01)에 대해 5단계를 순서대로 실행한다.
// dhcpRecord/dhcpErr: DNS로 대역을 자동 판별해 읽은(dhcp.Resolve) 이 호스트의 정적 MAC/IP.
// dhcpErr가 있으면 DNS 조회/파일 로드/호스트 블록 중 하나를 못 찾은 것이라 1단계는 무조건 Fail.
// swapNote: 같은 BM 그룹 형제와 MAC이 뒤바뀐 교차 설치(역설치)가 확인되면 그 설명, 아니면 "".
// vmInfo: vCenter에서 조회한 이 VM의 정보.
// prevUUID: 이전 실행에서 기록된 UUID(없으면 "").
func Check(hostname string, dhcpRecord dhcp.Record, dhcpErr error, swapNote string, vmInfo vc.VMInfo, prevUUID string) Result {
	r := Result{Hostname: hostname}

	// 1단계: vCenter vNIC MAC ↔ DHCP MAC
	switch {
	case dhcpErr != nil:
		r.Steps = append(r.Steps, StepResult{1, "vCenter MAC ↔ DHCP", Fail, dhcpErr.Error()})
	case containsFold(vmInfo.MACs, dhcpRecord.MAC):
		r.Steps = append(r.Steps, StepResult{1, "vCenter MAC ↔ DHCP", Pass, dhcpRecord.MAC})
	case swapNote != "":
		r.Steps = append(r.Steps, StepResult{1, "vCenter MAC ↔ DHCP", Fail,
			fmt.Sprintf("DHCP=%s, vCenter vNIC=%v — %s", dhcpRecord.MAC, vmInfo.MACs, swapNote)})
	default:
		r.Steps = append(r.Steps, StepResult{1, "vCenter MAC ↔ DHCP", Fail,
			fmt.Sprintf("DHCP=%s, vCenter vNIC=%v", dhcpRecord.MAC, vmInfo.MACs)})
	}

	// Tools 미기동이면 2~4단계는 게스트 정보에 의존하므로 판정 불가
	if !vmInfo.ToolsRunning {
		inconclusiveSteps := []StepResult{
			{2, "OS Hostname ↔ VM Name", Inconclusive, "VMware Tools 미기동 — 게스트 정보 없음"},
			{3, "실제 할당 IP ↔ DHCP/DNS", Inconclusive, "VMware Tools 미기동 — 게스트 정보 없음"},
			{4, "DNS 역방향 Lookup", Inconclusive, "VMware Tools 미기동 — 게스트 정보 없음"},
		}
		r.Steps = append(r.Steps, inconclusiveSteps...)
	} else {
		// 2단계: OS Hostname ↔ VM Name/DHCP 식별자
		guestHost := strings.ToLower(strings.SplitN(vmInfo.GuestHostname, ".", 2)[0])
		expected := strings.ToLower(hostname)
		if guestHost == expected {
			r.Steps = append(r.Steps, StepResult{2, "OS Hostname ↔ VM Name", Pass, guestHost})
		} else {
			r.Steps = append(r.Steps, StepResult{2, "OS Hostname ↔ VM Name", Fail,
				fmt.Sprintf("기대=%s, 실제 Guest hostname=%s", expected, vmInfo.GuestHostname)})
		}

		// 3단계: 실제 할당 IP ↔ DHCP fixed-address / DNS A 레코드
		if dhcpErr != nil {
			r.Steps = append(r.Steps, StepResult{3, "실제 할당 IP ↔ DHCP/DNS", Fail, "DHCP 조회 실패 — 1단계 사유 참고"})
		} else if dhcpRecord.IP == "" {
			r.Steps = append(r.Steps, StepResult{3, "실제 할당 IP ↔ DHCP/DNS", Fail, "DHCP fixed-address 없음"})
		} else if !containsFold(vmInfo.GuestIPAddresses, dhcpRecord.IP) {
			r.Steps = append(r.Steps, StepResult{3, "실제 할당 IP ↔ DHCP/DNS", Fail,
				fmt.Sprintf("DHCP fixed-address=%s, Guest IP=%v", dhcpRecord.IP, vmInfo.GuestIPAddresses)})
		} else {
			dnsIPs, err := net.LookupHost(hostname)
			if err != nil {
				r.Steps = append(r.Steps, StepResult{3, "실제 할당 IP ↔ DHCP/DNS", Warn,
					fmt.Sprintf("DHCP/Guest IP는 일치(%s)하나 DNS A 레코드 조회 실패: %v", dhcpRecord.IP, err)})
			} else if containsFold(dnsIPs, dhcpRecord.IP) {
				r.Steps = append(r.Steps, StepResult{3, "실제 할당 IP ↔ DHCP/DNS", Pass, dhcpRecord.IP})
			} else {
				r.Steps = append(r.Steps, StepResult{3, "실제 할당 IP ↔ DHCP/DNS", Fail,
					fmt.Sprintf("DHCP/Guest IP=%s, DNS A 레코드=%v", dhcpRecord.IP, dnsIPs)})
			}
		}

		// 4단계: DNS 역방향(PTR) Lookup
		if dhcpRecord.IP == "" {
			r.Steps = append(r.Steps, StepResult{4, "DNS 역방향 Lookup", Fail, "대조할 IP 없음(DHCP fixed-address 없음)"})
		} else {
			names, err := net.LookupAddr(dhcpRecord.IP)
			if err != nil {
				r.Steps = append(r.Steps, StepResult{4, "DNS 역방향 Lookup", Warn, fmt.Sprintf("PTR 조회 실패: %v", err)})
			} else {
				matched := false
				for _, n := range names {
					if strings.EqualFold(strings.TrimSuffix(strings.SplitN(n, ".", 2)[0], "."), expected) {
						matched = true
						break
					}
				}
				if matched {
					r.Steps = append(r.Steps, StepResult{4, "DNS 역방향 Lookup", Pass, strings.Join(names, ",")})
				} else {
					r.Steps = append(r.Steps, StepResult{4, "DNS 역방향 Lookup", Fail,
						fmt.Sprintf("기대=%s, PTR 결과=%v", expected, names)})
				}
			}
		}
	}

	// 5단계: VM UUID 이력 대조 — Fail이 아니라 Warn으로 분리 보고
	switch {
	case vmInfo.UUID == "":
		r.Steps = append(r.Steps, StepResult{5, "VM UUID 이력 대조", Inconclusive, "vCenter에서 UUID를 가져오지 못함"})
	case prevUUID == "":
		r.Steps = append(r.Steps, StepResult{5, "VM UUID 이력 대조", Pass, fmt.Sprintf("최초 기록: %s", vmInfo.UUID)})
	case prevUUID == vmInfo.UUID:
		r.Steps = append(r.Steps, StepResult{5, "VM UUID 이력 대조", Pass, vmInfo.UUID})
	default:
		r.Steps = append(r.Steps, StepResult{5, "VM UUID 이력 대조", Warn,
			fmt.Sprintf("이전=%s, 현재=%s (재설치/복제 이력 가능성)", prevUUID, vmInfo.UUID)})
	}

	return r
}
