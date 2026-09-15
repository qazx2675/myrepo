// vm-ip-change 는 vcenter.txt 에 적힌 vCenter 들을 순회하며 list.txt 에 적힌
// "호스트네임 새IP" 대상을 찾아, VMware Tools Guest Operations API 로 게스트
// 안에서 직접 IP 를 재설정합니다. vCenter API 경로만 쓰므로 대상 VM 에
// 네트워크로 접속하지 못하는 상황(IP 오할당으로 인한 접속 불가)에서도 동작합니다.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/vmware/govmomi/vim25/types"

	"vm-ip-change/internal/target"
	"vm-ip-change/internal/vsphere"
)

func main() {
	vcenterFile := flag.String("vcenter", "vcenter.txt", "대상 vCenter 목록 파일")
	listFile := flag.String("list", "list.txt", "작업 대상(호스트네임/새IP) 목록 파일")
	flag.Parse()

	vcUser := os.Getenv("VC_USER")
	vcPass := os.Getenv("VC_PASSWORD")
	guestUser := os.Getenv("GUEST_USER")
	guestPass := os.Getenv("GUEST_PASSWORD")
	if vcUser == "" || vcPass == "" || guestUser == "" || guestPass == "" {
		fmt.Fprintln(os.Stderr, "환경변수 VC_USER, VC_PASSWORD, GUEST_USER, GUEST_PASSWORD 가 모두 필요합니다")
		os.Exit(1)
	}

	vcenters, err := target.LoadVCenters(*vcenterFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	entries, err := target.LoadEntries(*listFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Println("작업 대상:")
	for _, e := range entries {
		fmt.Printf("  %-24s -> %s (GW %s)\n", e.Hostname, e.NewIP, target.Gateway(e.NewIP))
	}
	fmt.Println(strings.Repeat("-", 60))

	want := make(map[string]bool, len(entries))
	for _, e := range entries {
		want[e.Hostname] = true
	}

	ctx := context.Background()
	done := make(map[string]bool, len(entries))
	failCount := 0

	for _, addr := range vcenters {
		remaining := make(map[string]bool)
		for h := range want {
			if !done[h] {
				remaining[h] = true
			}
		}
		if len(remaining) == 0 {
			break
		}

		c, err := vsphere.Connect(ctx, addr, vcUser, vcPass)
		if err != nil {
			fmt.Printf("[FAIL] %s: %v\n", addr, err)
			continue
		}

		vms, err := vsphere.FindVMs(ctx, c, remaining)
		if err != nil {
			fmt.Printf("[FAIL] %s: %v\n", addr, err)
			c.Logout(ctx)
			continue
		}

		for _, e := range entries {
			vm, ok := vms[e.Hostname]
			if !ok {
				continue
			}
			done[e.Hostname] = true

			if vm.Runtime.PowerState != types.VirtualMachinePowerStatePoweredOn {
				fmt.Printf("[FAIL] %-24s: VM 전원이 꺼져 있습니다(%s)\n", e.Hostname, vm.Runtime.PowerState)
				failCount++
				continue
			}
			if vm.Guest == nil || vm.Guest.ToolsRunningStatus != "guestToolsRunning" {
				fmt.Printf("[FAIL] %-24s: VMware Tools 가 실행 중이 아닙니다(Guest Operations 불가)\n", e.Hostname)
				failCount++
				continue
			}

			gw := target.Gateway(e.NewIP)
			if err := vsphere.ChangeIP(ctx, c, vm.Reference(), e.NewIP, gw, guestUser, guestPass); err != nil {
				fmt.Printf("[FAIL] %-24s: %v\n", e.Hostname, err)
				failCount++
				continue
			}
			fmt.Printf("[OK]   %-24s -> %s (GW %s) [%s]\n", e.Hostname, e.NewIP, gw, addr)
		}

		c.Logout(ctx)
	}

	for _, e := range entries {
		if !done[e.Hostname] {
			fmt.Printf("[FAIL] %-24s: 어느 vCenter 에서도 찾지 못했습니다\n", e.Hostname)
			failCount++
		}
	}

	if failCount > 0 {
		os.Exit(1)
	}
}
