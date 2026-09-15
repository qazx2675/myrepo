// Package vsphere 는 vCenter 접속, VM 조회, 그리고 VMware Tools Guest
// Operations API 를 통한 게스트 내부 IP 변경을 담당합니다.
//
// Guest Operations API 는 vCenter -> ESXi -> vmtoolsd 경로(VMX 채널)로 동작하며
// 대상 VM 의 네트워크 스택을 거치지 않습니다. 그래서 VM 의 IP 가 잘못 할당되어
// 관리서버에서 네트워크로 접속할 수 없는 상황에서도 동작합니다.
//
// IP 변경은 3단계로 나눠 각각 별도의 게스트 명령으로 실행합니다(CheckConnection ->
// ApplyIP -> RestartConnection). 단계를 나눈 이유는 각 단계 사이에서 오케스트레이터
// (cmd/vm-ip-change)가 현재 진행 상태를 기록해 진행률 화면에 보여주고, Ctrl+C 로
// 취소됐을 때 "아직 설정을 바꾸지 않았으면 그냥 중단, 이미 바꿨으면 되돌리기"를
// 판단할 수 있게 하기 위해서입니다. CheckConnection 이 캡처해 둔 원래 설정은
// 게스트 안 되돌리기 스크립트(Revert 가 실행)로 보관되며, RestartConnection 이
// 성공적으로 끝나면 더 이상 되돌릴 수 없도록 그 스크립트를 지웁니다.
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

// tmpDir 은 게스트 안에서 CheckConnection 이 찾은 연결 이름과 되돌리기 스크립트를
// 임시로 보관하는 경로입니다.
const tmpDir = "/tmp/vm-ip-change"

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

// IsReady 는 Guest Operations 를 쓸 수 있는 상태인지(전원 켜짐 + VMware Tools
// 실행 중) 확인합니다. 아니면 그 이유를 담은 에러를 돌려줍니다.
func IsReady(vm mo.VirtualMachine) error {
	if vm.Runtime.PowerState != types.VirtualMachinePowerStatePoweredOn {
		return fmt.Errorf("VM 전원이 꺼져 있습니다(%s)", vm.Runtime.PowerState)
	}
	if vm.Guest == nil || vm.Guest.ToolsRunningStatus != "guestToolsRunning" {
		return fmt.Errorf("VMware Tools 가 실행 중이 아닙니다(Guest Operations 불가)")
	}
	return nil
}

func guestAuth(user, pass string) types.BaseGuestAuthentication {
	return &types.NamePasswordAuthentication{Username: user, Password: pass}
}

func connFile(id string) string   { return fmt.Sprintf("%s/%s.conn", tmpDir, id) }
func revertFile(id string) string { return fmt.Sprintf("%s/%s.revert.sh", tmpDir, id) }

// CheckConnection 은 1단계("연결 확인")입니다. 게스트 안에서 현재 활성 상태인
// NetworkManager 연결을 찾고, 그 연결의 현재 ipv4 설정을 캡처해 되돌리기
// 스크립트로 저장합니다. 이 단계는 아직 아무 설정도 바꾸지 않습니다.
func CheckConnection(ctx context.Context, c *govmomi.Client, vmRef types.ManagedObjectReference, guestUser, guestPass, id string) error {
	script := fmt.Sprintf(`set -e
mkdir -p %s
CONN=$(nmcli -t -f NAME,DEVICE con show --active | grep -v '^lo:' | head -n1 | cut -d: -f1)
if [ -z "$CONN" ]; then echo "활성 네트워크 연결을 찾지 못했습니다" >&2; exit 1; fi
ORIG_METHOD=$(nmcli -g ipv4.method con show "$CONN")
ORIG_ADDR=$(nmcli -g ipv4.addresses con show "$CONN")
ORIG_GW=$(nmcli -g ipv4.gateway con show "$CONN")
printf '%%s\n' "$CONN" > %s
cat > %s <<REVERTEOF
#!/bin/bash
nmcli con mod "$CONN" ipv4.method "$ORIG_METHOD" ipv4.addresses "$ORIG_ADDR" ipv4.gateway "$ORIG_GW"
nmcli con up "$CONN"
REVERTEOF
chmod 600 %s
`, tmpDir, connFile(id), revertFile(id), revertFile(id))

	return runGuestScript(ctx, c, vmRef, guestAuth(guestUser, guestPass), script)
}

// ApplyIP 는 2단계("IP 설정 적용")입니다. CheckConnection 이 찾아 둔 연결에
// 새 static IPv4 주소/게이트웨이를 설정합니다. 이 단계까지는 연결을 다시
// 올리지 않으므로(nmcli con up 을 안 함) 게스트의 실제 통신은 아직 영향받지
// 않습니다 — RestartConnection 이 실제로 반영합니다.
func ApplyIP(ctx context.Context, c *govmomi.Client, vmRef types.ManagedObjectReference, guestUser, guestPass, id, newIP, gateway string) error {
	script := fmt.Sprintf(`set -e
CONN=$(cat %s)
nmcli con mod "$CONN" ipv4.method manual ipv4.addresses %s/24 ipv4.gateway %s
`, connFile(id), newIP, gateway)

	return runGuestScript(ctx, c, vmRef, guestAuth(guestUser, guestPass), script)
}

// RestartConnection 은 3단계("연결 재기동")입니다. ApplyIP 가 설정한 내용을
// nmcli con up 으로 반영하고(서비스 전체 재시작 아님), 게스트에 남겨둔 임시
// 파일(연결 이름/되돌리기 스크립트)을 지웁니다. 이 단계가 성공하면 더 이상
// Revert 로 되돌릴 수 없습니다(되돌리기 스크립트가 이미 삭제됨) — Ctrl+C 취소가
// 이 단계 실행 중에 들어와도 이미 시작된 게스트 명령은 끝까지 실행되며, 끝나면
// 그대로 완료(OK) 처리됩니다.
func RestartConnection(ctx context.Context, c *govmomi.Client, vmRef types.ManagedObjectReference, guestUser, guestPass, id string) error {
	script := fmt.Sprintf(`set -e
CONN=$(cat %s)
nmcli con up "$CONN"
rm -f %s %s
`, connFile(id), connFile(id), revertFile(id))

	return runGuestScript(ctx, c, vmRef, guestAuth(guestUser, guestPass), script)
}

// Revert 는 CheckConnection 이 저장해 둔 되돌리기 스크립트를 실행해 원래
// ipv4 설정으로 복원합니다. ApplyIP 이후 ~ RestartConnection 이 끝나기 전에
// Ctrl+C 로 취소된 VM 에 대해서만 호출합니다. 되돌리기 스크립트가 없으면
// (아직 CheckConnection 도 끝나지 않은 상태) 아무 것도 하지 않습니다.
func Revert(ctx context.Context, c *govmomi.Client, vmRef types.ManagedObjectReference, guestUser, guestPass, id string) error {
	script := fmt.Sprintf(`set -e
if [ -f %s ]; then
  bash %s
fi
rm -f %s %s
`, revertFile(id), revertFile(id), connFile(id), revertFile(id))

	return runGuestScript(ctx, c, vmRef, guestAuth(guestUser, guestPass), script)
}

// runGuestScript 는 게스트 안에서 /bin/bash -c script 를 실행하고 끝날 때까지
// 2초 간격으로 폴링합니다. 의도적으로 자체 타임아웃이 없습니다 — 물리 코어를
// 과점유당한 VM 은 게스트 명령 자체가 느려질 뿐 실패하는 것이 아니므로, 여기서
// 포기시키지 않고 끝까지 기다립니다. 여러 VM 을 동시에 처리(cmd/vm-ip-change 의
// 워커 풀)하므로 한 VM 이 느려도 다른 VM 을 막지 않습니다. ctx 가 취소되면(현재
// 오케스트레이터는 정상 종료 시에만 취소) 그 자리에서 반환합니다.
func runGuestScript(ctx context.Context, c *govmomi.Client, vmRef types.ManagedObjectReference, auth types.BaseGuestAuthentication, script string) error {
	opMgr := guest.NewOperationsManager(c.Client, vmRef)
	procMgr, err := opMgr.ProcessManager(ctx)
	if err != nil {
		return fmt.Errorf("ProcessManager 조회 실패: %w", err)
	}

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
