// main.go: vm-verifier — OS 설치 직후 작업자가 수동 실행해서, vCenter/DHCP/DNS/Guest OS
// 상태를 교차 대조해 오설치(DHCP MAC 오기입 등)를 탐지한다. (PLAN.md 참고)
//
// 실행 예:
//
//	VC_USER=administrator@vsphere.local VC_PASS='...' \
//	  ./vm-verifier -vc 192.168.0.50 -prefix svr01 -subnet 10.10.10.0
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"vm-verifier/auditlog"
	"vm-verifier/dhcp"
	"vm-verifier/history"
	"vm-verifier/vc"
	"vm-verifier/verify"
)

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func main() {
	vcAddr := flag.String("vc", "", "vCenter 주소 (필수, 예: 192.168.0.50)")
	prefix := flag.String("prefix", "", "검증 대상 호스트명 접두어 (필수, 예: svr01 → svr01ev01/02/03 대조)")
	subnet := flag.String("subnet", "", "대상 IP가 속한 /24 대역 (필수, 예: 10.10.10.0)")
	dhcpRoot := flag.String("dhcp-root", "/user/caedhcp", "DHCP 설정 파일 루트 경로")
	historyPath := flag.String("history", "vm-verifier-uuid-history.json", "UUID 이력 저장 파일 경로")
	groups := flag.String("groups", "ev01,ev02,ev03", "검증할 그룹 접미어 목록(쉼표 구분)")
	flag.Parse()

	if *vcAddr == "" || *prefix == "" || *subnet == "" {
		log.Fatal("-vc, -prefix, -subnet 은 필수입니다")
	}

	vcUser := firstNonEmpty(os.Getenv("VC_USER"), os.Getenv("VCENTER_USER"))
	vcPass := firstNonEmpty(os.Getenv("VC_PASS"), os.Getenv("VCENTER_PASS"))
	if vcUser == "" || vcPass == "" {
		log.Fatal("VC_USER/VC_PASS (또는 VCENTER_USER/VCENTER_PASS) 환경변수가 필요합니다")
	}

	var groupSuffixes []string
	cur := ""
	for _, c := range *groups + "," {
		if c == ',' {
			if cur != "" {
				groupSuffixes = append(groupSuffixes, cur)
			}
			cur = ""
		} else {
			cur += string(c)
		}
	}

	var hostnames []string
	for _, g := range groupSuffixes {
		hostnames = append(hostnames, *prefix+g)
	}

	dhcpPath := dhcp.PathForSubnet(*dhcpRoot, *subnet)
	records, err := dhcp.ParseFile(dhcpPath)
	if err != nil {
		// PLAN.md 4장: DHCP 파일 로드 실패는 무조건 검증 실패(Block). 우회 통과 금지.
		log.Fatalf("[BLOCK] %v", err)
	}

	ctx := context.Background()
	client, err := vc.Connect(ctx, *vcAddr, vcUser, vcPass)
	if err != nil {
		log.Fatalf("vCenter 접속 실패: %v", err)
	}
	defer client.Logout(ctx)

	vmInfos, err := vc.FetchByNames(ctx, client, hostnames)
	if err != nil {
		log.Fatalf("VM 조회 실패: %v", err)
	}

	hist, err := history.Load(*historyPath)
	if err != nil {
		log.Fatalf("UUID 이력 로드 실패: %v", err)
	}

	now := time.Now()
	exitFail := false
	for _, hostname := range hostnames {
		vmInfo, found := vmInfos[hostname]
		if !found {
			result := verify.Result{Hostname: hostname, Steps: []verify.StepResult{
				{Step: 0, Name: "vCenter VM 존재 여부", Status: verify.Fail, Detail: "vCenter에 해당 이름의 VM이 없음"},
			}}
			fmt.Printf("[%s] FAIL — vCenter에 해당 이름의 VM이 없음\n", hostname)
			if err := auditlog.Write(".", now, result); err != nil {
				log.Printf("감사 로그 기록 실패(치명적이지 않음): %v", err)
			}
			exitFail = true
			continue
		}
		rec := records[hostname]
		result := verify.Check(hostname, rec, vmInfo, hist[hostname])

		fmt.Printf("=== %s : %s ===\n", hostname, result.Overall())
		for _, s := range result.Steps {
			fmt.Printf("  [%d] %-24s %-12s %s\n", s.Step, s.Name, s.Status, s.Detail)
		}
		if err := auditlog.Write(".", now, result); err != nil {
			log.Printf("감사 로그 기록 실패(치명적이지 않음): %v", err)
		}
		if result.Overall() == verify.Fail {
			exitFail = true
		}
		if vmInfo.UUID != "" {
			hist[hostname] = vmInfo.UUID
		}
	}

	if err := history.Save(*historyPath, hist); err != nil {
		log.Printf("UUID 이력 저장 실패(치명적이지 않음): %v", err)
	}

	if exitFail {
		os.Exit(1)
	}
}
