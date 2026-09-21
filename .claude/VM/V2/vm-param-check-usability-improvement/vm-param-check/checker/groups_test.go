package checker

import (
	"testing"

	"vm-param-check/model"
)

func intp(v int) *int { return &v }

// ev04~ev10도 ev02/ev03과 같은 규칙이어야 한다: 값이 있으면 그 값으로 체크, 없으면 스킵.
func TestCheckHardwareGroupsBeyondEV03(t *testing.T) {
	cpu := CPUExpect{Base: 8, Groups: map[string]*int{"ev07": intp(2)}}
	shares := SharesExpect{EV01: RatioShares(4000), Groups: map[string][]SharesItem{"ev10": {{Normal: true}}}}

	find := func(fs []model.Finding, key string) *model.Finding {
		for i := range fs {
			if fs[i].Key == key {
				return &fs[i]
			}
		}
		return nil
	}

	// ev07: cpu-ev07=2가 있으니 2로 체크된다.
	vm := model.VMInfo{Name: "hostev07", NumCPU: 2}
	got := CheckHardware(vm, cpu, MemExpect{}, DiskExpect{}, shares, "ev07", false, false)
	if f := find(got, "config.hardware.numCPU"); f == nil || f.Expected != "2" || f.Result != "OK" {
		t.Errorf("ev07 vCPU 체크 결과가 이상합니다: %+v", f)
	}

	// ev05: 아무 값도 없으니 vCPU/Shares 체크 자체가 없어야 한다.
	got = CheckHardware(model.VMInfo{Name: "hostev05", NumCPU: 99}, cpu, MemExpect{}, DiskExpect{}, shares, "ev05", false, false)
	if f := find(got, "config.hardware.numCPU"); f != nil {
		t.Errorf("ev05는 cpu 값이 없는데 체크됐습니다: %+v", f)
	}
	if f := find(got, "cpuAllocation.shares (CPU Shares)"); f != nil {
		t.Errorf("ev05는 shares 값이 없는데 체크됐습니다: %+v", f)
	}

	// ev10: shares-ev10=normal로 체크된다. VM이 1대뿐이면(singleVMMode) 스킵.
	vm10 := model.VMInfo{Name: "hostev10", CPUSharesLevel: "normal", MemorySharesLevel: "normal"}
	got = CheckHardware(vm10, cpu, MemExpect{}, DiskExpect{}, shares, "ev10", false, false)
	if f := find(got, "cpuAllocation.shares (CPU Shares)"); f == nil || f.Result != "OK" {
		t.Errorf("ev10 shares 체크 결과가 이상합니다: %+v", f)
	}
	got = CheckHardware(vm10, cpu, MemExpect{}, DiskExpect{}, shares, "ev10", true, false)
	if f := find(got, "cpuAllocation.shares (CPU Shares)"); f != nil {
		t.Errorf("singleVMMode인데 ev10 shares가 체크됐습니다: %+v", f)
	}
}
