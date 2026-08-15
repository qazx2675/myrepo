// vcenter.go: 동질성 검증 + 전원상태 검증에 필요한 최소한의 govmomi 조회.
// affinity_setting/lpage_setting/power_setting을 대신하는 게 아니라, 그 도구들을 실행해도
// 안전한 상태인지("전부 동일 스펙" + "전부 꺼짐")만 확인하는 용도라 필요한 필드만 가져온다.
package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

func connect(ctx context.Context, address, id, password string) (*govmomi.Client, error) {
	u := &url.URL{Scheme: "https", Host: address, Path: "/sdk"}
	u.User = url.UserPassword(id, password)
	return govmomi.NewClient(ctx, u, true)
}

// VMSpec은 동질성/전원상태 판단에 필요한 값만 담는다.
type VMSpec struct {
	Name              string
	NumCPU            int32
	NumCoresPerSocket int32
	MemoryMB          int32
	DiskGB            float64
	CPUSharesLevel    string
	CPUShares         int32
	NumaSetting       string // extraConfig "numa.vcpu.maxPerVirtualNode" 값 (NUMA 노드 구성), 미설정이면 ""
	HTOn              bool   // extraConfig "sched.vcpu0.affinity" 값에 콤마로 구분된 pCPU가 2개 이상이면 true (HT 페어 핀닝)
	PoweredOn         bool
	Found             bool
}

// fetchVMSpecs는 vmNames에 해당하는 VM들의 스펙을 ContainerView(RootFolder, recursive)로
// 한 번에 가져온다 — vm-param-check와 동일하게 데이터센터 무관 전체 조회.
func fetchVMSpecs(ctx context.Context, client *govmomi.Client, vmNames map[string]bool) (map[string]VMSpec, error) {
	m := view.NewManager(client.Client)
	cv, err := m.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("VM ContainerView 생성 실패: %w", err)
	}
	defer cv.Destroy(ctx)

	props := []string{"name", "config.hardware", "config.cpuAllocation", "config.extraConfig", "runtime.powerState"}
	var vms []mo.VirtualMachine
	if err := cv.Retrieve(ctx, []string{"VirtualMachine"}, props, &vms); err != nil {
		return nil, fmt.Errorf("VM 벌크 조회 실패: %w", err)
	}

	result := map[string]VMSpec{}
	for _, vm := range vms {
		if !vmNames[vm.Name] || vm.Config == nil {
			continue
		}
		spec := VMSpec{Name: vm.Name, Found: true}
		spec.NumCPU = vm.Config.Hardware.NumCPU
		spec.NumCoresPerSocket = vm.Config.Hardware.NumCoresPerSocket
		spec.MemoryMB = vm.Config.Hardware.MemoryMB
		spec.PoweredOn = vm.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn

		var diskKB float64
		for _, dev := range vm.Config.Hardware.Device {
			if disk, ok := dev.(*types.VirtualDisk); ok {
				diskKB += float64(disk.CapacityInKB)
			}
		}
		spec.DiskGB = diskKB / 1024 / 1024

		if vm.Config.CpuAllocation != nil && vm.Config.CpuAllocation.Shares != nil {
			spec.CPUSharesLevel = string(vm.Config.CpuAllocation.Shares.Level)
			spec.CPUShares = vm.Config.CpuAllocation.Shares.Shares
		}

		spec.NumaSetting = extraConfigValue(vm.Config.ExtraConfig, "numa.vcpu.maxPerVirtualNode")
		spec.HTOn = strings.Count(extraConfigValue(vm.Config.ExtraConfig, "sched.vcpu0.affinity"), ",") > 0

		result[vm.Name] = spec
	}
	return result, nil
}

// extraConfigValue는 VM의 extraConfig(advanced settings) 목록에서 key에 해당하는 값을 찾는다.
// 없으면 빈 문자열을 반환한다.
func extraConfigValue(ec []types.BaseOptionValue, key string) string {
	for _, ov := range ec {
		opt := ov.GetOptionValue()
		if opt == nil || opt.Key != key {
			continue
		}
		if s, ok := opt.Value.(string); ok {
			return s
		}
	}
	return ""
}
