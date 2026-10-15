package main

// protect.go — 절대 바꾸지 않는 BIOS 속성 (보호 속성).
//
// 서버마다 고유한 값(IQN·IP·ServerName·자산번호 등)과 부팅·보안·스토리지(RAID)·PXE·NIC 관련 속성은 프로파일이든
// 전체비교(비교서버 값)든 설정하지 않는다. 내장 목록(builtinProtect)은 코드에 고정이며 bios.conf·ignore_attrs.txt 로
// 끌 수 없다. bios.conf protect_attrs 는 이 목록에 더하기만 한다.
//
// 막는 곳 (겹겹이):
//  1. 설정 계획(setplan.go buildSetPlan): 보호 속성은 설정 항목이 되지 않고 setPlan.Protected 로 (수정제외) 표시.
//  2. 적용(set.go runHost): 항목 중 하나라도 보호 속성이면 그 호스트는 접속도 하지 않고 통째로 PROTECTED_ATTR.
//     PATCH 직전(set.go send)에 실제 본문의 속성을 다시 검사해 하나라도 걸리면 보내지 않는다.
//  3. PATCH 본문 검사(redfish.go checkAttributes): 내장 목록에 걸리는 속성이 있으면 본문 전체 거부.
//
// 패턴: * 는 0글자 이상 와일드카드, 대소문자 무시. 속성 이름 그대로와, 영숫자만 남긴 이름(예: "Wake-On-LAN" →
// "wakeonlan") 둘 다 비교한다 (영숫자만 남긴 비교는 패턴에 영숫자·* 외 글자가 없을 때만).
//
// 짧은 부분 일치는 성능 속성을 잘못 막을 수 있어 좁혀 두었다 (protect_test.go 가 profiles/*.tsv 속성이 막히지 않는지 확인):
//   예) *Ocp* 는 ProcPwrPerf(Dell CPU 전원 관리)·EnhancedProcPerf 에 걸려 Ocp* / *_Ocp* 로,
//       *Tcm* 은 SataPortCModel 에 걸려 Tcm* 로, *Sas* 는 AMD SEV ASID 속성에 걸려 EmbSas* / IntegratedSas* 로.

import "strings"

// builtinProtect 는 코드에 고정된 보호 속성 패턴입니다 (bios.conf·ignore_attrs.txt 로 끌 수 없음).
var builtinProtect = []string{
	// iSCSI·NVMe-oF 식별자
	"*Iscsi*", "*Iqn*", "*Initiator*", "*Nqn*",
	// IP·네트워크 주소
	"*Ipv4*", "*Ipv6*", "*IpAddress*", "*Ip4*", "*Ip6*", "*Subnet*", "*Gateway*", "*Dns*", "*Dhcp*", "*Vlan*", "*MacAddr*",
	// 서버 이름·식별 정보
	"ServerName", "*HostName*", "*ServerAsset*", "*AssetTag*", "*ServiceTag*", "*SerialNumber*", "*SerialNum*", "*Uuid*",
	"Admin*", "ServerOtherInfo", "ServerPrimaryOs", "ServerConfigLock*", "*ContactInfo*",
	// 부팅 (부팅 모드·순서·옵션·PXE·HTTP/URL 부팅·iPXE)
	"BootMode", "*Boot*", "*BootSeq*", "*BootOrder*", "*BootOption*", "UefiBoot*", "LegacyBoot*", "*BootFile*", "*UrlBoot*",
	"*Url*", "OneTimeBoot*", "*Pxe*", "*NetworkBoot*", "*HttpBoot*", "*Ipxe*", "*Http*", "*Network*", "*Hdd*",
	// 보안
	"SecureBoot*", "*Secur*", "*Tpm*", "Tcm*", "*Password*", "*Passw*", "*Pwd*", "*Passphrase*", "*Cert*", "*Sgx*",
	// 스토리지·RAID
	"*Raid*", "*Sata*", "*Nvme*", "*Nvme*Raid*", "EmbSata*", "IntegratedRaid*", "EmbSas*", "IntegratedSas*",
	"*Storage*Mode*", "*Storage*", "*Vroc*", "*Vmd*", "*Erase*",
	// NIC·OCP·슬롯·Option ROM
	"*Nic*", "*Lom*", "*EmbNic*", "*FlexLom*", "Ocp*", "*_Ocp*", "*NetworkPort*", "*Wol*", "*WakeOnLan*",
	"*OptionRom*", "*Oprom*", "*Slot*",
}

// protectSet 은 보호 속성 패턴입니다 (내장 목록 + bios.conf protect_attrs).
type protectSet struct {
	pats []string // 소문자
	orig []string // 사유에 보일 원래 표기
}

var builtinProtectSet = newProtectSet(nil)

// newProtectSet 은 내장 목록에 extra 를 더한 보호 패턴입니다 (extra 는 더하기만, 빈 칸·중복 무시).
func newProtectSet(extra []string) *protectSet {
	s := &protectSet{}
	seen := map[string]bool{}
	for _, p := range append(append([]string(nil), builtinProtect...), extra...) {
		p = strings.TrimSpace(p)
		l := strings.ToLower(p)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		s.pats = append(s.pats, l)
		s.orig = append(s.orig, p)
	}
	return s
}

// protectFor 는 설정에 쓸 보호 패턴입니다 (cfg 가 nil 이어도 내장 목록은 항상).
func protectFor(cfg *Config) *protectSet {
	if cfg == nil || len(cfg.ProtectAttrs) == 0 {
		return builtinProtectSet
	}
	return newProtectSet(cfg.ProtectAttrs)
}

// alnumLower 는 영숫자만 남긴 소문자입니다 (keepStar 면 * 도 남김).
func alnumLower(s string, keepStar bool) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (keepStar && r == '*') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// match 는 이름이 보호 패턴에 걸리면 그 패턴(원래 표기)을 돌려줍니다 (안 걸리면 "").
func (s *protectSet) match(name string) string {
	if s == nil {
		s = builtinProtectSet
	}
	l := strings.ToLower(strings.TrimSpace(name))
	n := alnumLower(name, false)
	for i, p := range s.pats {
		if globMatch(p, l) {
			return s.orig[i]
		}
		if np := alnumLower(p, true); np == p && n != "" && globMatch(np, n) {
			return s.orig[i]
		}
	}
	return ""
}

// why 는 보호 속성이면 사유 문구, 아니면 "" 입니다.
func (s *protectSet) why(name string) string {
	if p := s.match(name); p != "" {
		return "보호 속성 (패턴 " + p + ") — 서버 고유값·부팅·보안·RAID·PXE·NIC 관련은 설정하지 않음"
	}
	return ""
}
