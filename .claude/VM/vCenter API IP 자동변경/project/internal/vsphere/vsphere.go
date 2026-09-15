// Package vsphere 는 vCenter 접속, VM 조회, 그리고 VMware Tools Guest
// Operations API 를 통한 게스트 내부 IP 변경을 담당합니다.
//
// Guest Operations API 는 vCenter -> ESXi -> vmtoolsd 경로(VMX 채널)로 동작하며
// 대상 VM 의 네트워크 스택을 거치지 않습니다. 그래서 VM 의 IP 가 잘못 할당되어
// 관리서버에서 네트워크로 접속할 수 없는 상황에서도 동작합니다.
package vsphere

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/guest"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// Connect 는 vCenter 에 접속합니다. 인증서 검증은 하지 않습니다(폐쇄망 자체서명 전제).
func Connect(ctx context.Context, addr, user, pass string) (*govmomi.Client, error) {
	u := &url.URL{Scheme: "https", Host: addr, Path: "/sdk"}
	u.User = url.UserPassword(user, pass)

	c, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		return nil, fmt.Errorf("%s 접속 실패: %w", addr, err)
	}
	return c, nil
}

// FindVMs 는 이 vCenter 인벤토리 전체 VM 중 want 에 있는 이름과 일치하는
// VM 을 찾아 이름->참조 맵으로 돌려줍니다. want 에 없는 VM 은 조회하지 않습니다.
func FindVMs(ctx context.Context, c *govmomi.Client, want map[string]bool) (map[string]mo.VirtualMachine, error) {
	m := view.NewManager(c.Client)
	cv, err := m.CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("컨테이너 뷰 생성 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	var vms []mo.VirtualMachine
	if err := cv.Retrieve(ctx, []string{"VirtualMachine"},
		[]string{"name", "runtime.powerState", "guest.toolsRunningStatus"}, &vms); err != nil {
		return nil, fmt.Errorf("VM 목록 조회 실패: %w", err)
	}

	found := make(map[string]mo.VirtualMachine)
	for _, vm := range vms {
		if want[vm.Name] {
			found[vm.Name] = vm
		}
	}
	return found, nil
}

// ChangeIP 는 Guest Operations API 로 게스트(RHEL 8.10, NetworkManager) 안에서
// 현재 활성 연결의 IPv4 주소/게이트웨이를 static 으로 재설정합니다.
// 네트워크 서비스 재시작이 아니라 해당 연결(nmcli connection)만 다시 올립니다.
func ChangeIP(ctx context.Context, c *govmomi.Client, vmRef types.ManagedObjectReference, newIP, gateway, guestUser, guestPass string) error {
	opMgr := guest.NewOperationsManager(c.Client, vmRef)
	procMgr, err := opMgr.ProcessManager(ctx)
	if err != nil {
		return fmt.Errorf("ProcessManager 조회 실패: %w", err)
	}

	auth := &types.NamePasswordAuthentication{Username: guestUser, Password: guestPass}

	script := fmt.Sprintf(`set -e
CONN=$(nmcli -t -f NAME,DEVICE con show --active | grep -v '^lo:' | head -n1 | cut -d: -f1)
if [ -z "$CONN" ]; then echo "활성 네트워크 연결을 찾지 못했습니다" >&2; exit 1; fi
nmcli con mod "$CONN" ipv4.method manual ipv4.addresses %s/24 ipv4.gateway %s
nmcli con up "$CONN"
`, newIP, gateway)

	spec := &types.GuestProgramSpec{
		ProgramPath: "/bin/bash",
		Arguments:   "-c " + shellQuote(script),
	}

	pid, err := procMgr.StartProgram(ctx, auth, spec)
	if err != nil {
		return fmt.Errorf("게스트 명령 실행 실패: %w", err)
	}

	for {
		procs, err := procMgr.ListProcesses(ctx, auth, []int64{pid})
		if err != nil {
			return fmt.Errorf("게스트 명령 상태 조회 실패: %w", err)
		}
		if len(procs) == 0 {
			return fmt.Errorf("게스트 명령(pid=%d)을 찾을 수 없습니다", pid)
		}
		if procs[0].EndTime != nil {
			if procs[0].ExitCode != 0 {
				return fmt.Errorf("게스트 명령이 실패했습니다(exit=%d) — nmcli 연결 이름/권한을 확인하세요", procs[0].ExitCode)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// shellQuote 는 bash -c 인자로 넘길 스크립트를 작은따옴표로 안전하게 감쌉니다.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
