package checker

import (
	"testing"

	"vm-param-check/model"
)

func TestExpectNoLpage(t *testing.T) {
	defer func() { ExpectNoLpage = false }()
	ExpectNoLpage = true
	vm := model.VMInfo{Name: "vm1", ExtraConfig: map[string]string{
		"sched.mem.prealloc":        "TRUE", // 남아 있음 -> FAIL
		"sched.swap.vmxSwapEnabled": "  ",   // 빈 값 -> 삭제로 본다
	}}
	got := map[string]string{}
	for _, f := range CheckFixed(vm) {
		got[f.Key] = f.Result
		if f.Expected != model.ExpectNone {
			t.Errorf("%s 기대값이 %q", f.Key, f.Expected)
		}
	}
	if got["sched.mem.prealloc"] != "FAIL" || got["sched.swap.vmxSwapEnabled"] != "OK" ||
		got["sched.mem.lpage.enable1GPage"] != "OK" || got["sched.mem.prealloc.pinnedMainMem"] != "OK" {
		t.Errorf("결과 %v", got)
	}
}

func TestExpectNoLpageOffKeepsOld(t *testing.T) {
	vm := model.VMInfo{Name: "vm1", ExtraConfig: map[string]string{}}
	for _, f := range CheckFixed(vm) {
		if f.Result != "설정없음" {
			t.Errorf("옵션 없이는 기존대로 설정없음이어야 함: %v", f)
		}
	}
}

func TestCheckAffinityAbsent(t *testing.T) {
	vm := model.VMInfo{Name: "vm1", ExtraConfig: map[string]string{"sched.vcpu1.affinity": "2,3", "sched.vcpu0.affinity": "", "sched.cpu.affinity": "all"}}
	fs := CheckAffinityAbsent(vm, "ev01")
	if len(fs) != 2 || fs[0].Result != "FAIL" {
		t.Errorf("남은 2개가 FAIL 이어야 함: %v", fs)
	}
	clean := CheckAffinityAbsent(model.VMInfo{Name: "vm2", ExtraConfig: map[string]string{"sched.vcpu0.affinity": ""}}, "ev01")
	if len(clean) != 1 || clean[0].Result != "OK" {
		t.Errorf("모두 지워졌으면 OK 한 줄: %v", clean)
	}
}
