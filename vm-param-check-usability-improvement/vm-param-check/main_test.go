package main

import (
	"testing"

	"vm-param-check/model"
)

// pci-ev01~ev99 는 스펙 파일이 지정할 수 있는 옵션이어야 한다(check 모드의 applySpecOptions 가 거부하지 않도록).
func TestSpecSettablePCI(t *testing.T) {
	for _, g := range model.GroupNames() {
		if !specSettableFlags["pci-"+g] {
			t.Errorf("pci-%s 가 specSettableFlags 에 없음", g)
		}
	}
}

// evaluateVM 이 pci 기대값을 체크 항목으로 포함하는지: 값이 있으면 1건, 없으면 0건.
func TestEvaluateVMPCI(t *testing.T) {
	vm := model.VMInfo{Name: "host01ev01", Hostname: "host01ev01", ExtraConfig: map[string]string{},
		PCIPassthrough: []model.PCIPassthroughDevice{{ID: "0000:3b:00.0", VendorID: 0x15b3, DeviceName: "ConnectX-5"}}}
	count := func(e expectSet) (n int, res string) {
		for _, f := range evaluateVM(vm, e, false) {
			if f.Key == "PCI 패스스루 (config.hardware.device)" {
				n++
				res = f.Result
			}
		}
		return
	}
	if n, _ := count(expectSet{}); n != 0 {
		t.Errorf("pci 기대값이 없는데 체크됨: %d건", n)
	}
	if n, res := count(expectSet{PCI: map[string]string{"ev01": "mellanox"}}); n != 1 || res != "OK" {
		t.Errorf("mellanox: %d건 %s", n, res)
	}
	if n, res := count(expectSet{PCI: map[string]string{"ev01": "nvidia"}}); n != 1 || res != "FAIL" {
		t.Errorf("nvidia: %d건 %s", n, res)
	}
}

// 스펙의 del_affinity / del_lpage 로 제외된 ev 그룹은 해당 값이 "없어야" 정상이고, 다른 그룹은 평소대로 체크한다.
func TestEvaluateVMDelGroups(t *testing.T) {
	lpageKey := "sched.mem.prealloc"
	run := func(name string, extra map[string]string, e expectSet) (lpage, aff []model.Finding) {
		vm := model.VMInfo{Name: name, Hostname: name, NumCPU: 4, ExtraConfig: extra}
		for _, f := range evaluateVM(vm, e, false) {
			if f.Key == lpageKey {
				lpage = append(lpage, f)
			}
			if f.Source == "ev01" || f.Source == "ev02" {
				aff = append(aff, f)
			}
		}
		return
	}
	del := expectSet{DelLpage: map[string]bool{"ev02": true}, DelAffinity: map[string]bool{"ev02": true}}

	// 제외 그룹 + 키 남음 -> ExpectNone FAIL
	l, a := run("host01ev02", map[string]string{lpageKey: "TRUE", "sched.vcpu0.affinity": "1"}, del)
	if len(l) != 1 || l[0].Expected != model.ExpectNone || l[0].Result != "FAIL" {
		t.Errorf("제외 그룹 lpage 잔존: %v", l)
	}
	if len(a) != 1 || a[0].Result != "FAIL" {
		t.Errorf("제외 그룹 affinity 잔존: %v", a)
	}
	// 제외 그룹 + 키 없음 -> OK
	l, a = run("host01ev02", map[string]string{}, del)
	if len(l) != 1 || l[0].Result != "OK" || len(a) != 1 || a[0].Result != "OK" {
		t.Errorf("제외 그룹 키 없음: %v %v", l, a)
	}
	// 다른 그룹(ev01) -> 평소대로: lpage 는 설정없음, affinity 는 자동계산 기대값과 비교
	l, a = run("host01ev01", map[string]string{}, del)
	if len(l) != 1 || l[0].Expected == model.ExpectNone || l[0].Result != "설정없음" {
		t.Errorf("비제외 그룹 lpage 가 달라짐: %v", l)
	}
	if len(a) == 0 || a[0].Expected == model.ExpectNone {
		t.Errorf("비제외 그룹 affinity 가 달라짐: %v", a)
	}
	// 제외 없음 -> 기존과 동일
	l, _ = run("host01ev02", map[string]string{}, expectSet{})
	if len(l) != 1 || l[0].Expected == model.ExpectNone {
		t.Errorf("제외 없음인데 달라짐: %v", l)
	}
}
