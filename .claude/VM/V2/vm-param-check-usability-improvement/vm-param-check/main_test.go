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
