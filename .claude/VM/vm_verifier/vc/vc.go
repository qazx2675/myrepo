// Package vc는 govmomi로 vCenter에 접속해 대상 VM들의 vNIC MAC 주소를 조회한다.
// VM 생성 직후(파워온/OS 설치 전)에 쓰는 도구라 Guest OS 정보(Tools/hostname/IP)는
// 아예 조회하지 않는다 — 그 시점엔 어차피 Tools가 켜져 있을 수 없다.
package vc

import (
	"context"
	"fmt"
	"net/url"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
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

// FetchByNames는 지정한 VM 이름들의 정보만 조회한다.
func FetchByNames(ctx context.Context, client *govmomi.Client, names []string) (map[string]VMInfo, error) {
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	return FetchMatching(ctx, client, func(name string) bool { return wanted[name] })
}

// FetchAll은 이 vCenter의 모든 VM 정보를 가져온다.
// 인벤토리가 크면 그만큼 무거우므로, 대상이 정해져 있다면 FetchMatching을 쓰는 편이 훨씬 빠르다.
func FetchAll(ctx context.Context, client *govmomi.Client) (map[string]VMInfo, error) {
	return FetchMatching(ctx, client, nil)
}

// FetchMatching은 match(name)이 true인 VM만 조회한다(match가 nil이면 전체).
//
// 2단계로 나눠 조회하는 것이 핵심이다:
//
//	1단계 — 인벤토리 전체에서 "name" 하나만 가볍게 훑는다.
//	2단계 — match를 통과한 VM들의 MoRef에 대해서만 config.hardware.device를 배치 조회한다.
//
// config.hardware.device는 디스크/컨트롤러/NIC/비디오카드까지 다 들어있는 아주 무거운
// 속성이라, 예전처럼 인벤토리 전체에 대해 이 속성을 받아오면 실제로 필요한 건 몇 대뿐인데도
// 응답 크기가 VM 수에 비례해 커진다. 대규모 vCenter에서 조회가 느렸던 주된 원인이다.
func FetchMatching(ctx context.Context, client *govmomi.Client, match func(string) bool) (map[string]VMInfo, error) {
	m := view.NewManager(client.Client)
	cv, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("ContainerView 생성 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	var named []mo.VirtualMachine
	if err := cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name"}, &named); err != nil {
		return nil, fmt.Errorf("VM 이름 조회 실패: %w", err)
	}

	result := make(map[string]VMInfo)
	refToName := make(map[types.ManagedObjectReference]string)
	var refs []types.ManagedObjectReference
	for _, vm := range named {
		if match != nil && !match(vm.Name) {
			continue
		}
		result[vm.Name] = VMInfo{Name: vm.Name}
		refs = append(refs, vm.Self)
		refToName[vm.Self] = vm.Name
	}
	if len(refs) == 0 {
		return result, nil
	}

	var detailed []mo.VirtualMachine
	pc := property.DefaultCollector(client.Client)
	if err := pc.Retrieve(ctx, refs, []string{"config.hardware.device"}, &detailed); err != nil {
		return nil, fmt.Errorf("VM 장치 정보 조회 실패: %w", err)
	}
	for _, vm := range detailed {
		name, ok := refToName[vm.Self]
		if !ok {
			continue
		}
		info := result[name]
		if vm.Config != nil {
			info.MACs = macsFromDevices(vm.Config.Hardware.Device)
		}
		result[name] = info
	}
	return result, nil
}
