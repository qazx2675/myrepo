// Package vc는 govmomi로 vCenter에 접속해 대상 VM들의 vNIC MAC, UUID,
// Guest OS hostname/IP(VMware Tools가 이미 보고하는 값)를 조회한다.
// Guest Operations API(별도 게스트 로그인)는 쓰지 않는다 — Tools 하트비트 정보만으로
// 충분하고, 그래야 진짜 에이전트리스가 된다. (PLAN.md 3장 대비 단순화된 부분)
package vc

import (
	"context"
	"fmt"
	"net/url"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
)

// VMInfo는 검증 1~5단계에 필요한 VM 1대의 조회 결과다.
type VMInfo struct {
	Name             string
	UUID             string // Config.Uuid (BIOS/DMI Product UUID, 게스트 내부 dmidecode 값과 동일)
	MACs             []string
	ToolsRunning     bool
	GuestHostname    string
	GuestIPAddresses []string
}

// Connect는 vCenter에 로그인한다. insecure(자체서명 인증서 허용)는 기존 govmomi 도구들과 동일하게 true 고정.
func Connect(ctx context.Context, address, user, pass string) (*govmomi.Client, error) {
	u := &url.URL{Scheme: "https", Host: address, Path: "/sdk"}
	u.User = url.UserPassword(user, pass)
	return govmomi.NewClient(ctx, u, true)
}

// FetchByNames는 지정한 VM 이름들(예: svr01ev01, svr01ev02, svr01ev03)의 정보를
// ContainerView + PropertyCollector 벌크 조회로 한 번에 가져온다.
func FetchByNames(ctx context.Context, client *govmomi.Client, names []string) (map[string]VMInfo, error) {
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	return fetch(ctx, client, wanted)
}

// FetchAll은 이 vCenter의 모든 VM 정보를 가져온다. 배치 모드(-vcenterList + -f)에서
// vCenter별로 VM 전체를 긁어온 뒤, 호출부가 BM 접두어 패턴(예: {prefix}ev\d+)으로 매칭한다.
func FetchAll(ctx context.Context, client *govmomi.Client) (map[string]VMInfo, error) {
	return fetch(ctx, client, nil)
}

// fetch는 wanted가 nil이면 전체, 아니면 wanted에 있는 이름만 걸러서 반환한다.
func fetch(ctx context.Context, client *govmomi.Client, wanted map[string]bool) (map[string]VMInfo, error) {
	m := view.NewManager(client.Client)
	cv, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("ContainerView 생성 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	var vms []mo.VirtualMachine
	err = cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{
		"name", "config.uuid", "config.hardware.device",
		"guest.toolsRunningStatus", "guest.hostName", "guest.net",
	}, &vms)
	if err != nil {
		return nil, fmt.Errorf("VM 벌크 조회 실패: %w", err)
	}

	result := make(map[string]VMInfo)
	for _, vm := range vms {
		if wanted != nil && !wanted[vm.Name] {
			continue
		}
		info := VMInfo{
			Name:         vm.Name,
			ToolsRunning: vm.Guest != nil && vm.Guest.ToolsRunningStatus == "guestToolsRunning",
		}
		if vm.Config != nil {
			info.UUID = vm.Config.Uuid
			info.MACs = macsFromDevices(vm.Config.Hardware.Device)
		}
		if vm.Guest != nil {
			info.GuestHostname = vm.Guest.HostName
			for _, nic := range vm.Guest.Net {
				info.GuestIPAddresses = append(info.GuestIPAddresses, nic.IpAddress...)
			}
		}
		result[vm.Name] = info
	}
	return result, nil
}
