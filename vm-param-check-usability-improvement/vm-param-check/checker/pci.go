package checker

import (
	"fmt"
	"strconv"
	"strings"

	"vm-param-check/model"
)

// pciKey는 PCI 패스스루 체크 항목의 Key 다. 자동교정(-fix) 대상이 아니라서 FAIL 이면 수동조치로 안내된다.
const pciKey = "PCI 패스스루 (config.hardware.device)"

const (
	vendorIDMellanox uint16 = 0x15b3 // 5555
	vendorIDNvidia   uint16 = 0x10de // 4318
)

// MatchPCIVendor는 스펙의 pci-evNN 값(vendor)이 패스스루 장치 1개와 맞는지 판정한다.
//
// 이 규칙은 VM 에 PCI 를 연결하는 별도 도구(VMsetup/pci_passthrough)와 완전히 같아야 한다.
// 규칙은 대소문자를 구분하지 않으며 위에서부터 먼저 맞는 것을 쓴다:
//
//  1. "mellanox"        -> VendorId 0x15b3 (5555)
//  2. "nvidia"          -> VendorId 0x10de (4318)
//  3. 10진 숫자만       -> VendorId 가 그 10진수와 같음      (예: 5555)
//  4. "0x" 로 시작하는 16진수, 또는 영문자(a-f)가 들어간 4자리 16진수
//     -> VendorId 가 그 16진수와 같음                       (예: 0x15b3, 15b3)
//  5. 그 밖의 값        -> 장치 이름(DeviceName)의 부분 문자열(대소문자 무시)
//
// VendorId 는 govmomi 에서 int16 이라 0x8086 처럼 최상위 비트가 켜진 값은 음수로 온다.
// 호출하는 쪽이 uint16 으로 바꿔서 넘기므로 여기서는 부호 문제가 없다.
func MatchPCIVendor(vendor string, vendorID uint16, deviceName string) bool {
	v := strings.ToLower(strings.TrimSpace(vendor))
	if v == "" {
		return false
	}
	switch v {
	case "mellanox":
		return vendorID == vendorIDMellanox
	case "nvidia":
		return vendorID == vendorIDNvidia
	}
	if id, ok := parsePCIVendorID(v); ok {
		return uint64(vendorID) == id
	}
	return strings.Contains(strings.ToLower(deviceName), v)
}

// parsePCIVendorID는 v(소문자, 공백 제거됨)가 VendorId 숫자 표기이면 그 값을 돌려준다.
// 10진 숫자만 / 0x 16진수 / a-f 가 들어간 4자리 16진수만 숫자로 보고, 나머지는 ok=false 다
// (그러면 장치 이름 부분 문자열로 찾는다). 값이 너무 커서 읽지 못하는 숫자도 어떤 VendorId 와도 안 맞게 ok=true, 최댓값을 준다.
func parsePCIVendorID(v string) (uint64, bool) {
	if strings.HasPrefix(v, "0x") {
		n, err := strconv.ParseUint(v[2:], 16, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	if isAllDigits(v) {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return ^uint64(0), true // 64비트를 넘는 숫자 — 어떤 VendorId 와도 같지 않다
		}
		return n, true
	}
	if len(v) == 4 && isHexString(v) { // 위에서 10진 숫자만인 경우는 걸렀으므로 a-f 가 하나는 들어 있다
		n, _ := strconv.ParseUint(v, 16, 64)
		return n, true
	}
	return 0, false
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isHexString(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return s != ""
}

// CheckPCI는 spec 의 pci-evNN 기대값을 체크한다.
//
// expect 는 그룹("ev01"~"ev99") -> vendor 문자열이다. VM 의 그룹에 기대값이 없으면(또는 미분류 VM이면)
// 이 항목은 체크하지 않고 nil 을 돌려준다 — 다른 옵션 기대값과 같다. ev02~ev99 는 shares/affinity 와
// 같은 3-0 규칙으로 VM 이 1개뿐이면 스킵한다.
//
// VM 에 붙은 패스스루 장치 중 하나라도 vendor 규칙(MatchPCIVendor)에 맞으면 OK, 아니면 FAIL 이다.
// Actual 은 붙어 있는 패스스루 장치 목록 "PCI주소(벤더:장치이름)" 이고 하나도 없으면 "없음" 이다.
func CheckPCI(vm model.VMInfo, expect map[string]string, group string, singleVMMode bool) []model.Finding {
	if group == "" || (group != "ev01" && singleVMMode) {
		return nil
	}
	vendor := strings.TrimSpace(expect[group])
	if vendor == "" {
		return nil
	}

	f := model.Finding{VM: vm.Name, Source: group, Key: pciKey, Expected: vendor, Result: "FAIL"}
	var actual []string
	for _, d := range vm.PCIPassthrough {
		actual = append(actual, fmt.Sprintf("%s(%04x:%s)", d.ID, d.VendorID, d.DeviceName))
		if f.Result != "OK" && MatchPCIVendor(vendor, d.VendorID, d.DeviceName) {
			f.Result = "OK"
			f.Note = "일치: " + d.ID
		}
	}
	if len(actual) == 0 {
		f.Actual = "없음"
		f.Note = "VM 에 붙은 PCI 패스스루 장치가 없음"
	} else {
		f.Actual = strings.Join(actual, ", ")
		if f.Result != "OK" {
			f.Note = fmt.Sprintf("붙은 패스스루 장치 중 %q 에 맞는 것이 없음", vendor)
		}
	}
	return []model.Finding{f}
}
