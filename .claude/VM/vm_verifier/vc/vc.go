// Package vc는 govmomi로 vCenter에 접속해 대상 VM들의 vNIC MAC 주소를 조회한다.
// VM 생성 직후(파워온/OS 설치 전)에 쓰는 도구라 Guest OS 정보(Tools/hostname/IP)는
// 아예 조회하지 않는다 — 그 시점엔 어차피 Tools가 켜져 있을 수 없다.
package vc

import (
	"context"
	"fmt"
	"net/url"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
)

// VMInfo는 검증에 필요한 VM 1대의 조회 결과다.
type VMInfo struct {
	Name string
	MACs []string
}

// Connect는 vCenter에 로그인한다. insecure(자체서명 인증서 허용)는 기존 govmomi 도구들과 동일하게 true 고정.
func Connect(ctx context.Context, address, user, pass string) (*govmomi.Client, error) {
	u := &url.URL{Scheme: "https", Host: address, Path: "/sdk"}
	u.User = url.UserPassword(user, pass)
	return govmomi.NewClient(ctx, u, true)
}

// FetchByNames는 지정한 VM 이름들의 정보를 ContainerView + PropertyCollector 벌크 조회로 한 번에 가져온다.
func FetchByNames(ctx context.Context, client *govmomi.Client, names []string) (map[string]VMInfo, error) {
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	return fetch(ctx, client, wanted)
}

// FetchAll은 이 vCenter의 모든 VM 정보를 가져온다. 호출부가 BM 접두어 패턴(예: {prefix}ev\d+)으로 매칭한다.
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
	err = cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "config.hardware.device"}, &vms)
	if err != nil {
		return nil, fmt.Errorf("VM 벌크 조회 실패: %w", err)
	}

	result := make(map[string]VMInfo)
	for _, vm := range vms {
		if wanted != nil && !wanted[vm.Name] {
			continue
		}
		info := VMInfo{Name: vm.Name}
		if vm.Config != nil {
			info.MACs = macsFromDevices(vm.Config.Hardware.Device)
		}
		result[vm.Name] = info
	}
	return result, nil
}
