package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// protect.go 시험: 보호 속성(서버 고유값·부팅·보안·RAID·PXE·NIC)은 계획·적용·PATCH 본문 검사 어디서든 막히고,
// 프로파일 표준 속성(HT·WorkloadProfile·LLC·SNC·전원·VT·SR-IOV)은 막히지 않는다.

// hpe11Registry 는 testdata hpe-dl360gen11 의 속성 레지스트리 경로입니다.
const hpe11Registry = "/redfish/v1/registrystore/registries/en/biosattributeregistryu54.v1_4_0"

// setRegAttr 는 mock 레지스트리의 속성 1개를 고칩니다 (없으면 새로 넣음).
func setRegAttr(t *testing.T, s *mockbmc.Server, regPath, name string, fn func(a map[string]interface{})) {
	t.Helper()
	must(t, s.Modify(regPath, func(m map[string]interface{}) {
		re := m["RegistryEntries"].(map[string]interface{})
		list, _ := re["Attributes"].([]interface{})
		for _, a := range list {
			if am := a.(map[string]interface{}); am["AttributeName"] == name {
				fn(am)
				return
			}
		}
		am := map[string]interface{}{"AttributeName": name, "ReadOnly": false}
		fn(am)
		re["Attributes"] = append(list, am)
	}))
}

// 요청한 분류마다 대표 이름이 내장 목록에 걸린다 (testdata·bios_json 의 실제 벤더 이름 포함).
func TestProtectBuiltinCategories(t *testing.T) {
	cats := map[string][]string{
		"iSCSI/IQN": {"IscsiInitiatorName", "iSCSISoftwareInitiator", "IscsiDev1Con1TargetName", "Iqn", "NvmeofHostNqn"},
		"IP/네트워크": {"Ipv4Address", "Ipv6Gateway", "Ip4Addr", "Ip6Prefix", "Ipv4SubnetMask", "Ipv4PrimaryDNS", "Dhcpv4",
			"HttpDev1VlanId", "NicMacAddress1", "IpAddress"},
		"이름/식별": {"ServerName", "HostName", "ServerAssetTag", "AssetTag", "SystemInfo_AssetTag", "ServiceTag", "SerialNumber",
			"SystemInfo_SystemSerialNumber", "SystemUuid", "AdminName", "AdminEmail", "AdminPhone", "AdminOtherInfo",
			"ServerOtherInfo", "ServerPrimaryOs", "ServerConfigLockStatus", "SysMfrContactInfo"},
		"부팅": {"BootMode", "BootModes_SystemBootMode", "UEFIBootMode", "BiosBootSeq", "UefiBootSeq", "SetBootOrderEn", "BootOrder",
			"UefiBootOption1", "LegacyBootProto", "UrlBootFile", "UefiShellStartupUrl", "OneTimeBootMode", "OneTimeUefiBootPath",
			"PxeDev1EnDis", "IPv4 PXE Support", "NetworkStackSettings_IPv4PXESupport", "NetworkBootRetry", "HttpBootUri",
			"HttpDev1Uri", "EmbeddedIpxe", "IpxeScriptAutoStart", "QuietBoot", "HddSeq", "IntNic1Port1BootProto", "Slot1NicBoot1"},
		"보안": {"SecureBoot", "SecureBootStatus", "SecureBootPolicy", "TpmSecurity", "TpmState", "Tpm2Hierarchy", "TcmActivation",
			"AdminPassword", "PowerOnPassword", "SetupPwdExpirationDate", "PersistentMemPassphrase", "SgxFactoryReset"},
		"스토리지/RAID": {"EmbSata", "EmbSata1Enable", "EmbeddedSata", "DevicesandIOPorts_OnboardSATAMode", "NvmeRaid", "NvmeMode",
			"IntegratedRaid", "EmbSas1Enable", "IntegratedSas", "StorageMode", "IntelVrocSupport", "VmdEnable", "IntelVmdSupport",
			"SecureErase00", "NvmeFormat1"},
		"NIC": {"EmbNicEnable", "EmbNic1Nic2", "NicBoot1", "FlexLom1Enable", "OcpAEnable", "Ocp1AuxiliaryPower", "NetworkPort1",
			"IntegratedNetwork1", "WakeOnLan", "Wake-On-LAN", "Wol", "PciSlot1OptionROM", "EmbNicPCIeOptionROM", "Slot1StorageBoot"},
	}
	for cat, names := range cats {
		for _, n := range names {
			if builtinProtectSet.match(n) == "" {
				t.Errorf("[%s] %s 가 보호되지 않음", cat, n)
			}
		}
	}
}

// profiles/*.tsv·testdata 프로파일의 모든 속성과 성능·전원·가상화 속성은 보호되지 않는다 (막히면 표준 설정을 못 함).
func TestProtectProfileAttrsNotBlocked(t *testing.T) {
	var files []string
	for _, g := range []string{"../../profiles/*.tsv", "../../testdata/profiles-test/*.tsv", "../../reference/*.tsv"} {
		m, err := filepath.Glob(g)
		must(t, err)
		files = append(files, m...)
	}
	if len(files) < 2 {
		t.Fatalf("프로파일 파일을 찾지 못함: %v", files)
	}
	n := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		must(t, err)
		for i, line := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
			fs := strings.Split(line, "\t")
			if i == 0 || strings.HasPrefix(line, "#") || len(fs) < 4 || fs[3] == "attribute" || fs[3] == "" {
				continue
			}
			n++
			if p := builtinProtectSet.match(fs[3]); p != "" {
				t.Errorf("%s: 프로파일 속성 %q 가 보호 패턴 %s 에 걸림", filepath.Base(f), fs[3], p)
			}
		}
	}
	if n == 0 {
		t.Fatal("프로파일 속성을 하나도 읽지 못함")
	}
	for _, a := range []string{
		"ProcHyperthreading", "LogicalProc", "Hyper-Threading[ALL]", "Processors_SMTMode", "SMT Control",
		"WorkloadProfile", "SysProfile", "PowerProfile", "Power Profile Selection", "OperatingModes_ChooseOperatingMode",
		"Power_WorkloadConfiguration", "CPUPerformance", "PowerTechnology",
		"LlcPrefetch", "Processors_LLCPrefetch", "SubNumaCluster", "SubNumaClustering", "Processors_SNC", "SNC",
		"ProcPwrPerf", "EnhancedProcPerf", "FixedSocPstate", "Processors_SOCP_states", "ProcCStates", "Processors_CStates",
		"MinProcIdlePower", "ProcessorCState", "CStates", "EnergyPerfBias", "PowerRegulator", "ProcTurbo", "ProcTurboMode",
		"Processors_TurboMode", "UncoreFrequency", "ProcX2Apic", "NumaGroupSizeOpt", "ThermalConfig",
		"ProcVirtualization", "Processors_VirtualizationTechnology", "IntelVirtualizationTechnology", "VMXEnable",
		"IntelVTForDirectedIo", "Processors_IntelVTforDirectedIO", "Sriov", "SriovGlobalEnable", "SRIOV", "Devices_SRIOV",
		"SRIOVSupport", "Memory_SEV_ESASIDSpaceLimit", "MemoryRefreshRate", "NodeInterleave", "PcieAspmL1",
	} {
		if p := builtinProtectSet.match(a); p != "" {
			t.Errorf("성능·전원·가상화 속성 %q 가 보호 패턴 %s 에 걸림", a, p)
		}
	}
}

// bios.conf protect_attrs 는 내장 목록에 더하기만 한다 (내장 목록은 끌 수 없음).
func TestProtectConfExtends(t *testing.T) {
	c, err := parseConf("user=a\nprotect_attrs= MemTest | *Watchdog* ||\nexclude_attrs=BootMode\n", "t.conf")
	must(t, err)
	if fmt.Sprint(c.ProtectAttrs) != "[MemTest *Watchdog*]" {
		t.Fatalf("ProtectAttrs = %q", c.ProtectAttrs)
	}
	ps := protectFor(c)
	for _, n := range []string{"memtest", "OsWatchdogTimer", "BootMode", "SecureBoot"} {
		if ps.match(n) == "" {
			t.Errorf("%s 가 보호되지 않음 (conf 추가·내장 목록)", n)
		}
	}
	if builtinProtectSet.match("MemTest") != "" {
		t.Error("conf 추가 패턴이 내장 목록을 바꿈")
	}
	for _, cfg := range []*Config{nil, {}, {ProtectAttrs: []string{""}}} {
		if protectFor(cfg).match("BootMode") == "" {
			t.Errorf("conf %+v 에서 내장 보호가 꺼짐", cfg)
		}
	}
	ex, err := loadConf("../../bios.conf.example")
	must(t, err)
	if len(ex.ProtectAttrs) != 0 {
		t.Errorf("bios.conf.example protect_attrs = %q, 기대 빈 값", ex.ProtectAttrs)
	}
	// ignore_attrs.txt 가 있어도(전체비교는 그 파일만 씀) 설정 제외 목록에는 내장 기본 목록이 항상 들어간다.
	ign := setIgnore(&Config{ExcludeAttrs: []string{"MyExcl"}}, &compareResult{Ignore: parseIgnore("OnlyInFile\n", "ignore_attrs.txt")})
	for _, n := range []string{"ServiceTag", "SystemUuid", "OnlyInFile", "myexcl"} {
		if !ign.match(n) {
			t.Errorf("설정 제외 목록에 %s 가 없음", n)
		}
	}
}

// PATCH 본문 검사(checkAttributes)는 내장 보호 속성이 하나라도 있으면 본문 전체를 거부한다.
func TestCheckAttributesRefusesProtected(t *testing.T) {
	for _, body := range []string{
		`{"Attributes":{"ProcTurbo":"Enabled","BootMode":"LegacyBios"}}`,
		`{"Attributes":{"IscsiInitiatorName":"iqn.x"}}`,
		`{"Attributes":{"EmbNicEnable":"Disabled"}}`,
	} {
		if _, err := checkBody("PATCH", "/redfish/v1/Systems/1/Bios/Settings", []byte(body)); err == nil || !strings.Contains(err.Error(), "보호 속성") {
			t.Errorf("%s: err = %v, 기대 보호 속성 거부", body, err)
		}
	}
	if _, err := checkBody("PATCH", "/redfish/v1/Systems/1/Bios/Settings", []byte(`{"Attributes":{"ProcTurbo":"Enabled"}}`)); err != nil {
		t.Errorf("일반 속성 거부: %v", err)
	}
}

// PATCH 직전 검사: 본문에 conf 보호 속성이 들어 있으면 보내지 않는다 (Client 를 쓰기 전에 끝남).
func TestSendRefusesProtectedBody(t *testing.T) {
	it := &applyItem{Attr: "ProcTurbo", Expected: "Enabled", Compare: true}
	h := &setHost{Target: Target{Hostname: "h"}, Items: []*applyItem{it},
		Plan: &hostPlan{Items: []*applyItem{it}, Path: "/redfish/v1/Systems/1/Bios/Settings", Body: []byte(`{"Attributes":{"ProcTurbo":"Enabled"}}`)}}
	e := &setEnv{protect: newProtectSet([]string{"ProcTurbo"})}
	e.send(h, nil) // 막히지 않으면 nil Client 로 패닉
	if it.Status != StatusProtectedAttr || h.Tried || h.Sent {
		t.Errorf("상태 %s (%s) Tried=%v Sent=%v, 기대 PROTECTED_ATTR·보내지 않음", it.Status, it.Detail, h.Tried, h.Sent)
	}
}

// 계획 단계: 보호 속성(프로파일·전체비교·conf 추가)은 설정 항목이 아니고 Protected (수정제외), 확인 화면에 보임.
// 적용해도 PATCH 본문에 들어가지 않는다.
func TestSetPlanProtectedNeverItems(t *testing.T) {
	prof := setProfText + "HPE,DL360 Gen11,boot_mode,BootMode,Uefi,Y\n"
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"BootMode": "LegacyBios", "TpmState": "PresentDisabled", "ProcTurbo": "Disabled",
			"ProcX2Apic": "Disabled", "ProcHyperthreading": "Disabled"})
	})
	rc := checkRCFor(t, 2, "", ua, ub)
	rc.Conf.AllcheckSet = true
	rc.Conf.ProtectAttrs = []string{"ProcX2Apic"}
	fastCheck(t)
	u, err := runUnified(rc, unifiedOpts{Profile: setProf(t, prof), ResultDir: t.TempDir(), HostsPath: writeHosts(t, ""), Now: checkNow})
	if err != nil {
		t.Fatal(err)
	}
	p := buildSetPlan(rc.Conf, u, compareT(t, u, compareRef{Hosts: []string{ua}}))
	want := []string{ub + "|ProcHyperthreading|Disabled|Enabled|profile|false", ub + "|ProcTurbo|Disabled|Enabled|compare|false"}
	if got := planKeys(p); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("항목 = %v", got)
	}
	for _, at := range []string{"BootMode", "TpmState", "ProcX2Apic"} {
		if !hasItem(p.Protected, ub, at) {
			t.Errorf("보호 목록에 %s 없음: %+v", at, p.Protected)
		}
	}
	nBoot := 0
	for _, it := range p.Protected {
		if it.Attr == "BootMode" {
			nBoot++
		}
	}
	if nBoot != 1 {
		t.Errorf("BootMode 보호 줄 %d개 (프로파일·전체비교 둘 다 걸려도 1줄)", nBoot)
	}
	cl := strings.Join(p.ConfirmLines(), "\n")
	for _, w := range []string{
		ub + "  ProcTurbo  Disabled -> Enabled (수정대상)", ub + "  ProcHyperthreading  Disabled -> Enabled (수정대상)",
		ub + "  BootMode  LegacyBios -> Uefi (수정제외: 보호 속성 (패턴 ", ub + "  TpmState  PresentDisabled -> PresentEnabled (수정제외: 보호 속성",
		ub + "  ProcX2Apic  Disabled -> Enabled (수정제외: 보호 속성 (패턴 ProcX2Apic)", "수정대상 2건 · 수정제외 3건",
	} {
		if !strings.Contains(cl, w) {
			t.Errorf("확인 화면에 %q 없음:\n%s", w, cl)
		}
	}
	ex := strings.Join(p.ExcludedLines(), "\n")
	if !strings.Contains(ex, "아래 항목은 변경하지 않습니다") || !strings.Contains(ex, "BootMode  LegacyBios -> Uefi (수정제외") || strings.Contains(ex, "수정대상") {
		t.Errorf("수정제외 화면:\n%s", ex)
	}
	applyT(t, rc, u, p, false, nil, nil)
	pb := patchBodies(b)
	if len(pb) != 1 {
		t.Fatalf("b PATCH %d회", len(pb))
	}
	for _, no := range []string{"BootMode", "TpmState", "ProcX2Apic"} {
		if strings.Contains(pb[0], no) {
			t.Errorf("PATCH 에 보호 속성 %s: %s", no, pb[0])
		}
	}
	assertClean(t, a)
	assertSetSafe(t, b)
}

// 적용 단계 방어: 계획에 보호 속성이 끼어 들어오면 그 호스트는 접속도 PATCH 도 하지 않고 통째로 PROTECTED_ATTR.
func TestApplySetPlanRefusesInjectedProtected(t *testing.T) {
	for _, c := range []struct {
		name, attr string
		conf       []string
	}{
		{"내장", "BootMode", nil},
		{"conf 추가", "ProcX2Apic", []string{"ProcX2Apic"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
				setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
			})
			rc := checkRCFor(t, 1, "", ub)
			u := runUnifiedSet(t, rc)
			p := buildSetPlan(rc.Conf, u, nil)
			if len(p.Items) != 1 {
				t.Fatalf("항목 = %v", planKeys(p))
			}
			p.Items = append(p.Items, setItem{Host: ub, Attr: c.attr, From: "x", To: "Disabled", Source: SetSrcCompare, std: SetSrcCompare})
			rc.Conf.ProtectAttrs = c.conf
			before := b.CallCount()
			out, _ := applyT(t, rc, u, p, false, nil, nil)
			if b.CallCount() != before || b.Writes() != 0 {
				t.Errorf("보호 속성이 낀 호스트에 접속·쓰기 (요청 %d→%d, 쓰기 %d)", before, b.CallCount(), b.Writes())
			}
			for _, at := range []string{"ProcHyperthreading", c.attr} {
				if st, det := itemStatus(t, u, ub, at); st != StatusProtectedAttr || !strings.Contains(det, c.attr) {
					t.Errorf("%s = %s (%s), 기대 PROTECTED_ATTR", at, st, det)
				}
			}
			if out.OK != 0 || out.Fail != 1 {
				t.Errorf("결과 %+v", out)
			}
		})
	}
}

// #1 비교서버 값은 레지스트리 형식이 Enumeration·Integer·Boolean 일 때만: String·레지스트리에 없는 속성은 수정제외.
func TestSetPlanCompareStringTypeExcluded(t *testing.T) {
	mod := func(v string) func(s *mockbmc.Server) {
		return func(s *mockbmc.Server) {
			setRegAttr(t, s, hpe11Registry, "MyStr", func(a map[string]interface{}) { a["Type"] = "String" })
			setAttrs(t, s, map[string]interface{}{"MyStr": "str-" + v, "MyNoReg": "v-" + v})
		}
	}
	a, ua := startAllSrv(t, "hpe-dl360gen11", mod("a"))
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		mod("b")(s)
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled"})
	})
	rc := checkRCFor(t, 2, "", ua, ub)
	rc.Conf.AllcheckSet = true
	u := runUnifiedSet(t, rc)
	p := buildSetPlan(rc.Conf, u, compareT(t, u, compareRef{Hosts: []string{ua}}))
	if len(p.Items) != 3 {
		t.Fatalf("항목 = %v (MyNoReg·MyStr·ProcTurbo 기대)", planKeys(p))
	}
	if !strings.Contains(strings.Join(p.ConfirmLines(), "\n"), "레지스트리 형식을 다시 확인") {
		t.Error("확인 화면에 비교서버 값 형식 확인 안내 없음")
	}
	applyT(t, rc, u, p, false, nil, nil)
	if pb := patchBodies(b); len(pb) != 1 || fmt.Sprint(patchAttrs(t, pb[0])) != "map[ProcTurbo:Enabled]" {
		t.Errorf("b PATCH = %v (문자열·형식 불명은 빠져야 함)", pb)
	}
	for at, w := range map[string]string{"MyStr": "문자열/형식 불명", "MyNoReg": "레지스트리에 없는"} {
		if st, det := itemStatus(t, u, ub, at); st != StatusInvalidValue || !strings.Contains(det, w) || !strings.Contains(det, "수정제외") {
			t.Errorf("%s = %s (%s)", at, st, det)
		}
	}
	assertClean(t, a)
	assertSetSafe(t, b)
}

// #11 BIOS 버전이 비교서버와 다른 호스트(BIOS_VER_DIFF)는 비교서버 값으로 설정하지 않는다 (프로파일 항목은 그대로).
func TestSetPlanBiosVerDiffExcluded(t *testing.T) {
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	_, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setSys(t, s, map[string]interface{}{"BiosVersion": "U54 v9.99 (01/01/2026)"})
		setAttrs(t, s, map[string]interface{}{"ProcTurbo": "Disabled", "ProcHyperthreading": "Disabled"})
	})
	rc := checkRCFor(t, 2, "", ua, ub)
	rc.Conf.AllcheckSet = true
	u := runUnifiedSet(t, rc)
	cmp := compareT(t, u, compareRef{Hosts: []string{ua}})
	if h := cmpHostOf(t, cmp, ub); !h.hasNote(noteBiosVerDiff) || !h.sameAttrNames() {
		t.Fatalf("시험 준비: notes %v", h.Notes)
	}
	p := buildSetPlan(rc.Conf, u, cmp)
	if got := planKeys(p); fmt.Sprint(got) != fmt.Sprint([]string{ub + "|ProcHyperthreading|Disabled|Enabled|profile|false"}) {
		t.Errorf("항목 = %v", got)
	}
	if !hasItem(p.Excluded, ub, "ProcTurbo") || !hasLine(p.ExcludedLines(), ub, "ProcTurbo  Disabled -> Enabled (수정제외: BIOS 버전이 비교서버와 달라(BIOS_VER_DIFF)") {
		t.Errorf("수정제외 = %v", p.ExcludedLines())
	}
}

// #2 Dell 에 남의 Pending 이 있으면 포함 여부를 묻지 않고 그 호스트는 설정에서 뺀다 (포함하기로 해도 쓰지 않음).
func TestSetPlanDellForeignPendingExcluded(t *testing.T) {
	d, ud := startAllSrv(t, "dell-r660", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"LogicalProc": "Disabled"})
	})
	setPending(t, d, dellSettings, map[string]interface{}{"ProcTurboMode": "Disabled"})
	rc := checkRCFor(t, 1, "", ud)
	u := runUnifiedSet(t, rc)
	p := buildSetPlan(rc.Conf, u, nil)
	if !p.Empty() || len(p.Pending) != 0 {
		t.Fatalf("항목 %v Pending %v, 기대 둘 다 없음", planKeys(p), p.Pending)
	}
	if !hasLine(p.ExcludedLines(), ud, "LogicalProc  Disabled -> Enabled (수정제외: Dell 은 설정 Job 이 남의 Pending 까지 적용하므로 제외)") ||
		!hasLine(p.ConfirmLines(), ud, "ProcTurboMode=Disabled") {
		t.Errorf("확인 화면:\n%s", strings.Join(p.ConfirmLines(), "\n"))
	}

	// 방어 중첩: 계획에 Dell 남의 Pending 호스트가 끼어 와도(포함=y) 쓰지 않는다.
	hc := u.Check.Hosts[0]
	inj := &setPlan{
		Items:   []setItem{{Host: ud, Attr: "LogicalProc", From: "Disabled", To: "Enabled", Source: SetSrcProfile, std: "hyper_threading", PendingHost: true}},
		Pending: []pendingInfo{{Host: ud, Attr: "ProcTurboMode", Pending: "Disabled"}},
	}
	if hc.Info.Vendor != "Dell" {
		t.Fatalf("시험 준비: 벤더 %s", hc.Info.Vendor)
	}
	before := d.CallCount()
	out, _ := applyT(t, rc, u, inj, true, nil, nil)
	if d.CallCount() != before || d.Writes() != 0 {
		t.Errorf("Dell 남의 Pending 호스트에 접속·쓰기 (쓰기 %d)", d.Writes())
	}
	if st, det := itemStatus(t, u, ud, "LogicalProc"); st != StatusPendingExists || !strings.Contains(det, "Dell") {
		t.Errorf("LogicalProc = %s (%s)", st, det)
	}
	if out.Skipped != 1 {
		t.Errorf("결과 %+v", out)
	}
}

// #3 시스템 프로파일 성격 속성이 여러 개 (실장비 확인 사례: WorkloadProfile + PowerProfile=Custom + ProcTurbo).
func TestSetPlanProfileDependencyMulti(t *testing.T) {
	// 계획: 프로파일 행 WorkloadProfile 만 프로파일 항목으로, 비교서버 PowerProfile 은 수정제외
	items := []setItem{
		{Host: "h", Attr: "WorkloadProfile", Source: SetSrcProfile, std: profileStd},
		{Host: "h", Attr: "PowerProfile", To: "Custom", Source: SetSrcCompare, std: profileStd},
		{Host: "h", Attr: "ProcTurbo", To: "Enabled", Source: SetSrcCompare, std: SetSrcCompare},
	}
	p := &setPlan{}
	got := p.oneProfile(items)
	if len(got) != 2 || got[0].Attr != "WorkloadProfile" || got[1].Attr != "ProcTurbo" || !hasItem(p.Excluded, "h", "PowerProfile") {
		t.Errorf("남은 항목 %v, 수정제외 %v", got, p.Excluded)
	}
	// 프로파일 행 항목이 없으면 프로파일 성격 비교서버 항목은 모두 제외
	p = &setPlan{}
	got = p.oneProfile(items[1:2:2])
	if len(got) != 1 {
		t.Errorf("하나뿐이면 그대로: %v", got)
	}
	got = p.oneProfile([]setItem{items[1], {Host: "h", Attr: "CpuPerfProfile", To: "Max", Source: SetSrcCompare, std: profileStd}, items[2]})
	if len(got) != 1 || got[0].Attr != "ProcTurbo" || len(p.Excluded) != 2 {
		t.Errorf("남은 항목 %v, 수정제외 %v", got, p.Excluded)
	}

	// buildPlan(적용 계층) 방어: 한 PATCH 에 프로파일 성격 속성 둘을 넣지 않는다.
	info := &SystemInfo{Vendor: "HPE", Model: "DL360 Gen11", SettingsPath: "/redfish/v1/systems/1/bios/settings/"}
	mk := func(std, attr, val string, cmp bool) *applyItem {
		b, _ := json.Marshal(val)
		return &applyItem{Std: std, Attr: attr, Value: val, Expected: val, Compare: cmp, raw: b}
	}
	wl := mk(profileStd, "WorkloadProfile", "Virtualization-MaxPerformance", false)
	pp := mk(profileStd, "PowerProfile", "Custom", true)
	pt := mk(SetSrcCompare, "ProcTurbo", "Enabled", true)
	hp := buildPlan(info, true, true, "", []*applyItem{pt, pp, wl})
	if len(hp.Items) != 1 || hp.Items[0] != wl || len(hp.Deferred) != 2 || !strings.Contains(hp.DeferWhy, "여러 개") {
		t.Errorf("buildPlan 항목 %v 미룸 %d (%s)", hp.Items, len(hp.Deferred), hp.DeferWhy)
	}
	if strings.Contains(string(hp.Body), "PowerProfile") || strings.Contains(string(hp.Body), "ProcTurbo") {
		t.Errorf("본문 = %s", hp.Body)
	}
	// Custom 프로파일 하나뿐이면 기존 규칙 그대로 (레지스트리 있으면 함께 보냄)
	if hp := buildPlan(info, true, true, "", []*applyItem{pt, pp}); len(hp.Items) != 2 {
		t.Errorf("Custom 하나: %v", hp.Items)
	}
	// 프로파일 행 항목 없이 프로파일 성격 비교서버 항목 둘 → 아무것도 보내지 않음
	pp2 := mk(profileStd, "CpuPerfProfile", "Max", true)
	if hp := buildPlan(info, true, true, "", []*applyItem{pp, pp2, pt}); hp.ready() || len(hp.Deferred) != 3 {
		t.Errorf("프로파일 행 없음: 항목 %v 미룸 %d", hp.Items, len(hp.Deferred))
	}

	// mock 통합: 비교서버 a(PowerProfile=Custom) 기준, b 는 WorkloadProfile·PowerProfile·ProcTurbo 가 다름
	a, ua := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"PowerProfile": "Custom"})
	})
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"PowerProfile": "MaxPerf", "WorkloadProfile": "Custom", "ProcTurbo": "Disabled"})
	})
	rc := checkRCFor(t, 2, "", ua, ub)
	rc.Conf.AllcheckSet = true
	u := runUnifiedSet(t, rc)
	plan := buildSetPlan(rc.Conf, u, compareT(t, u, compareRef{Hosts: []string{ua}}))
	want := []string{ub + "|WorkloadProfile|Custom|GeneralPowerEfficientCompute|GeneralThroughputCompute|profile|false", ub + "|ProcTurbo|Disabled|Enabled|compare|false"}
	if got := planKeys(plan); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("항목 = %v", got)
	}
	if !hasItem(plan.Excluded, ub, "PowerProfile") {
		t.Errorf("PowerProfile 이 수정제외가 아님: %v", plan.Excluded)
	}
	applyT(t, rc, u, plan, false, nil, nil)
	if pb := patchBodies(b); len(pb) != 1 || fmt.Sprint(patchAttrs(t, pb[0])) != "map[WorkloadProfile:GeneralPowerEfficientCompute]" {
		t.Errorf("b PATCH = %v (프로파일 항목만, ProcTurbo 는 미룸)", pb)
	}
	if st, _ := itemStatus(t, u, ub, "ProcTurbo"); st != StatusDependsOnProfile {
		t.Errorf("ProcTurbo = %s", st)
	}
	assertClean(t, a)
	assertSetSafe(t, b)
}

// #9 취소(CANCELED)로 시작하지 않은 호스트도 apply_failed.txt 에 남는다 (다시 점검·설정 대상).
func TestApplyFailedIncludesCanceled(t *testing.T) {
	dir := t.TempDir()
	hosts := []*setHost{
		{Target: Target{Hostname: "h1", Input: "h1-in"}, Items: []*applyItem{{Attr: "X", Status: StatusCanceled}}},
		{Target: Target{Hostname: "h2", Input: "h2-in"}, Items: []*applyItem{{Attr: "X", Status: StatusApplied}}},
	}
	must(t, writeApplyFiles(dir, hosts))
	b, err := os.ReadFile(filepath.Join(dir, "apply_failed.txt"))
	must(t, err)
	if got := strings.TrimSpace(string(b)); got != "h1-in" {
		t.Errorf("apply_failed.txt = %q", got)
	}
}

// 미리보기·확인 화면은 값을 자르지 않는다 (bv 60자 제한 없음).
func TestSetPlanPreviewFullValues(t *testing.T) {
	long := strings.Repeat("V", 100)
	p := &setPlan{Items: []setItem{{Host: "h", Attr: "A", From: "x", To: long, Source: SetSrcCompare}},
		Excluded: []setItem{{Host: "h", Attr: "B", From: long + "F", To: "y", Reason: "r"}}}
	for name, lines := range map[string][]string{"Preview": p.Preview(), "ConfirmLines": p.ConfirmLines()} {
		s := strings.Join(lines, "\n")
		if !strings.Contains(s, long) || strings.Contains(s, "...") {
			t.Errorf("%s 가 값을 자름:\n%s", name, s)
		}
	}
	if !hasLine(p.ConfirmLines(), "h  A  x -> "+long+" (수정대상)") || !hasLine(p.ExcludedLines(), "h  B  "+long+"F -> y (수정제외: r)") {
		t.Errorf("확인 줄:\n%s\n%s", strings.Join(p.ConfirmLines(), "\n"), strings.Join(p.ExcludedLines(), "\n"))
	}
}
