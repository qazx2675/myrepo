package checker

import (
	"strings"
	"testing"

	"vm-param-check/model"
)

// MatchPCIVendor 의 모든 분기. 0x8086 은 govmomi 에서 int16 음수로 오므로 uint16 으로 바꿔 넘기는 경우도 본다.
func TestMatchPCIVendor(t *testing.T) {
	var intel int16 = -0x7f7a // 0x8086 을 int16 으로 읽은 값
	tests := []struct {
		name     string
		vendor   string
		vendorID uint16
		device   string
		want     bool
	}{
		{"mellanox 일치", "mellanox", 0x15b3, "", true},
		{"mellanox 대소문자", "MeLLaNoX", 0x15b3, "", true},
		{"mellanox 불일치", "mellanox", 0x10de, "Mellanox", false}, // 키워드는 VendorId 로만 판정(이름은 보지 않음)
		{"nvidia 일치", "nvidia", 0x10de, "", true},
		{"nvidia 불일치", "nvidia", 0x15b3, "", false},
		{"10진 일치", "5555", 0x15b3, "", true},
		{"10진 불일치", "5555", 0x10de, "", false},
		{"10진 4318=nvidia", "4318", 0x10de, "", true},
		{"10진 범위 밖", "99999999999999999999999", 0xffff, "", false},
		{"0x 16진", "0x15b3", 0x15b3, "", true},
		{"0X 대문자", "0X15B3", 0x15b3, "", true},
		{"0x 16진 불일치", "0x15b3", 0x10de, "", false},
		{"0x 짧은 16진", "0xab", 0x00ab, "", true},
		{"4자리 16진(문자 포함)", "15b3", 0x15b3, "", true},
		{"4자리 16진 대문자", "15B3", 0x15b3, "", true},
		{"4자리 16진 불일치", "10de", 0x15b3, "", false},
		{"장치 이름 부분 문자열", "connectx", 0x15b3, "Mellanox ConnectX-5 Ex", true},
		{"장치 이름 대소문자", "CONNECTX", 0x1234, "mellanox connectx-5", true},
		{"장치 이름 불일치", "a100", 0x10de, "GP102 [TITAN X]", false},
		{"4자리지만 16진 아님은 이름으로", "dell", 0x1028, "Dell PowerEdge", true},
		{"0x 뒤가 16진 아님은 이름으로", "0xzz", 0x1234, "board 0xzz", true},
		{"빈 값", "", 0x15b3, "x", false},
		{"앞뒤 공백", " Mellanox ", 0x15b3, "", true},
		{"숫자만이면 10진(8086 은 0x1f96)", "8086", uint16(intel), "", false},
		{"int16 음수 VendorId 0x 접두", "0x8086", uint16(intel), "", true},
		{"int16 음수 VendorId 10진", "32902", uint16(intel), "", true},
		{"int16 음수 VendorId 불일치", "0x8087", uint16(intel), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchPCIVendor(tt.vendor, tt.vendorID, tt.device); got != tt.want {
				t.Errorf("MatchPCIVendor(%q, %#04x, %q) = %v, want %v", tt.vendor, tt.vendorID, tt.device, got, tt.want)
			}
		})
	}
}

func pciVM(devs ...model.PCIPassthroughDevice) model.VMInfo {
	return model.VMInfo{Name: "host01ev01", PCIPassthrough: devs}
}

var mellanoxDev = model.PCIPassthroughDevice{ID: "0000:3b:00.0", VendorID: 0x15b3, DeviceName: "MT27800 Family [ConnectX-5]"}
var nvidiaDev = model.PCIPassthroughDevice{ID: "0000:af:00.0", VendorID: 0x10de, DeviceName: "GA100 [A100]"}

func TestCheckPCIPassFail(t *testing.T) {
	expect := map[string]string{"ev01": "mellanox"}

	pass := CheckPCI(pciVM(nvidiaDev, mellanoxDev), expect, "ev01", false)
	if len(pass) != 1 || pass[0].Result != "OK" || pass[0].Key != pciKey || pass[0].Expected != "mellanox" || pass[0].Source != "ev01" {
		t.Fatalf("PASS 기대: %+v", pass)
	}

	wrong := CheckPCI(pciVM(nvidiaDev), expect, "ev01", false)
	if len(wrong) != 1 || wrong[0].Result != "FAIL" || wrong[0].Actual != "0000:af:00.0(10de:GA100 [A100])" {
		t.Fatalf("다른 벤더는 FAIL + 실제값 목록: %+v", wrong)
	}

	none := CheckPCI(pciVM(), expect, "ev01", false)
	if len(none) != 1 || none[0].Result != "FAIL" || none[0].Actual != "없음" {
		t.Fatalf("장치 없음은 FAIL + 없음: %+v", none)
	}

	two := CheckPCI(pciVM(nvidiaDev, mellanoxDev), map[string]string{"ev01": "nvidia"}, "ev01", false)
	if two[0].Result != "OK" {
		t.Fatalf("여러 개 중 하나만 맞아도 OK: %+v", two)
	}
	multi := CheckPCI(pciVM(nvidiaDev, mellanoxDev), map[string]string{"ev01": "intel"}, "ev01", false)
	if multi[0].Result != "FAIL" || !strings.Contains(multi[0].Actual, ", ") {
		t.Fatalf("목록은 ', ' 로 이어야 함: %+v", multi)
	}
}

// 음수 int16 VendorId(0x8086)가 client 에서 uint16 으로 바뀐 뒤에도 16진 표기가 4자리로 찍힌다.
func TestCheckPCIIntelActualFormat(t *testing.T) {
	var neg int16 = -0x7f7a
	vm := pciVM(model.PCIPassthroughDevice{ID: "0000:18:00.0", VendorID: uint16(neg), DeviceName: "X710"})
	got := CheckPCI(vm, map[string]string{"ev01": "0x8086"}, "ev01", false)
	if got[0].Result != "OK" || got[0].Actual != "0000:18:00.0(8086:X710)" {
		t.Fatalf("%+v", got)
	}
}

// 기대값이 없거나 대상 그룹이 아니면 항목 자체를 만들지 않는다(다른 옵션 기대값과 같은 처리).
func TestCheckPCISkips(t *testing.T) {
	vm := pciVM()
	if got := CheckPCI(vm, nil, "ev01", false); got != nil {
		t.Errorf("기대값 없음 -> 체크 안 함: %+v", got)
	}
	if got := CheckPCI(vm, map[string]string{"ev01": "  "}, "ev01", false); got != nil {
		t.Errorf("빈 기대값 -> 체크 안 함: %+v", got)
	}
	if got := CheckPCI(vm, map[string]string{"ev02": "mellanox"}, "ev01", false); got != nil {
		t.Errorf("다른 그룹 기대값은 무시: %+v", got)
	}
	if got := CheckPCI(vm, map[string]string{"ev01": "mellanox"}, "", false); got != nil {
		t.Errorf("미분류 VM -> 체크 안 함: %+v", got)
	}
	// ev02~ev99 는 VM 1개뿐이면 스킵(shares/affinity 와 같은 규칙), ev01 은 항상 체크
	if got := CheckPCI(vm, map[string]string{"ev02": "mellanox"}, "ev02", true); got != nil {
		t.Errorf("단일 VM 에서 ev02 는 스킵: %+v", got)
	}
	if got := CheckPCI(vm, map[string]string{"ev02": "mellanox"}, "ev02", false); len(got) != 1 {
		t.Errorf("여러 VM 이면 ev02 도 체크: %+v", got)
	}
	if got := CheckPCI(vm, map[string]string{"ev01": "mellanox"}, "ev01", true); len(got) != 1 {
		t.Errorf("단일 VM 이어도 ev01 은 체크: %+v", got)
	}
}
