// Package fields는 vc-test-env가 추적하는 VM 설정 항목의 레지스트리다.
//
// 새 항목이 필요해지면 VMFields 슬라이스에 하나 추가하기만 하면
// extract/build/diff 전체에 자동으로 반영된다 — inventory/builder/recipe
// 패키지는 손댈 필요 없다.
package fields

import (
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// Field는 추적 대상 VM 설정 항목 하나를 정의한다.
type Field struct {
	Key     string
	Extract func(vm mo.VirtualMachine) (value string, present bool)
	Apply   func(spec *types.VirtualMachineConfigSpec, value string)
}

func extraConfigString(vm mo.VirtualMachine, key string) (string, bool) {
	if vm.Config == nil {
		return "", false
	}
	for _, ec := range vm.Config.ExtraConfig {
		opt := ec.GetOptionValue()
		if opt == nil || opt.Key != key {
			continue
		}
		if s, ok := opt.Value.(string); ok {
			return s, true
		}
	}
	return "", false
}

func setExtraConfig(spec *types.VirtualMachineConfigSpec, key, value string) {
	spec.ExtraConfig = append(spec.ExtraConfig, &types.OptionValue{Key: key, Value: value})
}

func extraConfigField(key string) Field {
	return Field{
		Key:     key,
		Extract: func(vm mo.VirtualMachine) (string, bool) { return extraConfigString(vm, key) },
		Apply:   func(spec *types.VirtualMachineConfigSpec, v string) { setExtraConfig(spec, key, v) },
	}
}

// VMFields — 1차 범위. 키 이름은 vmx 어드밴스드 옵션 관례를 따랐으므로,
// 실 vCenter(마일스톤 0)에서 extract로 실제 값이 이 키로 잡히는지 검증 필요.
// 다르면 여기 Key 문자열만 고치면 된다.
var VMFields = []Field{
	extraConfigField("sched.mem.lpage.enable1GPage"),
	extraConfigField("sched.mem.prealloc"),
	extraConfigField("sched.mem.prealloc.min"),
	extraConfigField("sched.swap.vmxSwapEnabled"),
	extraConfigField("numa.vcpu.maxPerVirtualNode"),
}

// HostFields — 호스트(BM) 레벨 추적 항목. 지금은 전원정책만 추적한다.
// (전원정책은 ExtraConfig가 아니라 HostSystem.Config.PowerSystemInfo에서
// 읽으므로 inventory 패키지에서 직접 처리하고, 여기 레지스트리는 향후
// 다른 host-level 항목이 필요해질 때 확장 지점으로 남겨둔다.)
var HostFields = []Field{}
