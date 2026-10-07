package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"biostool/internal/mockbmc"
)

// inspect.go (detectSystem · 레지스트리 해석 · 표준 4항목 후보 탐색) 시험. 실제 BMC 없이 mock 만 쓴다.

func TestNormalizeVendor(t *testing.T) {
	cases := map[string]string{
		"Dell Inc.":                  "Dell",
		"DELL":                       "Dell",
		"HPE":                        "HPE",
		"Hewlett Packard Enterprise": "HPE",
		"Lenovo":                     "Lenovo",
		"Cisco Systems Inc":          "Cisco",
		"Supermicro":                 "Supermicro",
		"Super Micro Computer":       "Supermicro",
		"  Inspur  ":                 "Inspur",
		"":                           "",
	}
	for in, want := range cases {
		if got := normalizeVendor(in); got != want {
			t.Errorf("normalizeVendor(%q) = %q, 기대 %q", in, got, want)
		}
	}
}

func TestDetectSystemAllTrees(t *testing.T) {
	want := map[string]SystemInfo{
		"dell-r660": {Vendor: "Dell", Model: "PowerEdge R660", BiosVersion: "1.8.2",
			SystemPath: "/redfish/v1/Systems/System.Embedded.1", BiosPath: "/redfish/v1/Systems/System.Embedded.1/Bios",
			SettingsPath: "/redfish/v1/Systems/System.Embedded.1/Bios/Settings", SettingsFrom: "SettingsObject",
			ManagerPath: "/redfish/v1/Managers/iDRAC.Embedded.1", RegistryPath: "/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0.json"},
		"hpe-dl360gen11": {Vendor: "HPE", Model: "ProLiant DL360 Gen11", BiosVersion: "U54 v1.40 (01/15/2025)",
			SystemPath: "/redfish/v1/systems/1/", BiosPath: "/redfish/v1/systems/1/bios/",
			SettingsPath: "/redfish/v1/systems/1/bios/settings/", SettingsFrom: "SettingsObject",
			ManagerPath: "/redfish/v1/managers/1/", RegistryPath: "/redfish/v1/registrystore/registries/en/biosattributeregistryu54.v1_4_0/"},
		"hpe-dl360gen10": {Vendor: "HPE", Model: "ProLiant DL360 Gen10", BiosVersion: "U32 v2.80 (07/15/2023)",
			SystemPath: "/redfish/v1/systems/1/", BiosPath: "/redfish/v1/systems/1/bios/",
			SettingsPath: "/redfish/v1/systems/1/bios/settings/", SettingsFrom: "SettingsObject",
			ManagerPath: "/redfish/v1/managers/1/", RegistryPath: "/redfish/v1/registrystore/registries/en/biosattributeregistryu32.v1_2_0/"},
		"lenovo-sr650v3": {Vendor: "Lenovo", Model: "ThinkSystem SR650 V3", BiosVersion: "ESE105F-1.10 (mock)",
			SystemPath: "/redfish/v1/Systems/1", BiosPath: "/redfish/v1/Systems/1/Bios",
			SettingsPath: "/redfish/v1/Systems/1/Bios/Pending", SettingsFrom: "SettingsObject",
			ManagerPath: "/redfish/v1/Managers/1", RegistryPath: "/redfish/v1/RegistryStore/Registries/en/BiosAttributeRegistry.v1_0_0"},
		"cisco-c220m7": {Vendor: "Cisco", Model: "UCSC-C220-M7S", BiosVersion: "C220M7.4.3.2 (mock)",
			SystemPath: "/redfish/v1/Systems/MOCK-C220M7", BiosPath: "/redfish/v1/Systems/MOCK-C220M7/Bios",
			SettingsPath: "/redfish/v1/Systems/MOCK-C220M7/Bios/Settings", SettingsFrom: "SettingsObject",
			ManagerPath: "/redfish/v1/Managers/CIMC", RegistryPath: "/redfish/v1/RegistryStore/Registries/en/BiosAttributeRegistry.v1_0_0"},
		"supermicro-x12": {Vendor: "Supermicro", Model: "X12DPi-N6", BiosVersion: "2.0 (mock)",
			SystemPath: "/redfish/v1/Systems/1", BiosPath: "/redfish/v1/Systems/1/Bios",
			SettingsPath: "", SettingsFrom: "",
			ManagerPath: "/redfish/v1/Managers/1", RegistryPath: "/redfish/v1/RegistryStore/Registries/en/BiosAttributeRegistry"},
	}
	for _, name := range mockTrees {
		name := name
		t.Run(name, func(t *testing.T) {
			s := startMockTree(t, name, nil)
			c, _ := mockClient(s, ModeReadOnly, 0)
			info, err := detectSystem(c)
			if err != nil {
				t.Fatalf("detectSystem: %v", err)
			}
			_ = c.Close()
			w := want[name]
			got := SystemInfo{Vendor: info.Vendor, Model: info.Model, BiosVersion: info.BiosVersion,
				SystemPath: info.SystemPath, BiosPath: info.BiosPath, SettingsPath: info.SettingsPath,
				SettingsFrom: info.SettingsFrom, ManagerPath: info.ManagerPath, RegistryPath: info.RegistryPath}
			if !reflect.DeepEqual(got, w) {
				t.Errorf("got  = %+v\nwant = %+v", got, w)
			}
			if len(info.Systems) != 1 || len(info.Notes) != 0 {
				t.Errorf("Systems=%v Notes=%v", info.Systems, info.Notes)
			}
			// 판별은 읽기 전용 호출만 쓴다: 쓰기·로그 0, 세션 1·1
			if s.Writes() != 0 || s.LogHits() != 0 {
				t.Errorf("Writes=%d LogHits=%d", s.Writes(), s.LogHits())
			}
			if ss := s.Sessions(); ss.Created != 1 || ss.Deleted != 1 {
				t.Errorf("세션 %+v", ss)
			}
			// ServiceRoot, Systems, System, Bios, (Settings 후보), Registries/<id> — 레지스트리 JSON 본문은 읽지 않는다
			if g := s.Count("GET", "/"); g > 9 {
				t.Errorf("판별에 GET %d회 (과다)", g)
			}
		})
	}
}

// SettingsObject 선언이 없으면 <Bios>/Settings, <Bios>/Pending 을 후보로 찾는다 (Lenovo 는 Pending 먼저).
func TestDetectSettingsProbe(t *testing.T) {
	strip := func(s *mockbmc.Server, biosPath string) {
		if err := s.Modify(biosPath, func(m map[string]interface{}) { delete(m, "@Redfish.Settings") }); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("lenovo-Pending", func(t *testing.T) {
		s := startMockTree(t, "lenovo-sr650v3", nil)
		strip(s, "/redfish/v1/Systems/1/Bios")
		c, _ := mockClient(s, ModeReadOnly, 0)
		defer c.Close()
		info, err := detectSystem(c)
		if err != nil || info.SettingsPath != "/redfish/v1/Systems/1/Bios/Pending" || info.SettingsFrom != "추정" {
			t.Fatalf("%v %+v", err, info)
		}
		// Lenovo 는 Pending 이 먼저라 Settings 는 요청하지 않는다
		if n := s.Count("GET", "/redfish/v1/Systems/1/Bios/Settings"); n != 0 {
			t.Errorf("Settings 후보를 불필요하게 요청함 %d회", n)
		}
	})
	t.Run("dell-Settings", func(t *testing.T) {
		s := startMockTree(t, "dell-r660", nil)
		strip(s, "/redfish/v1/Systems/System.Embedded.1/Bios")
		c, _ := mockClient(s, ModeReadOnly, 0)
		defer c.Close()
		info, err := detectSystem(c)
		if err != nil || info.SettingsPath != "/redfish/v1/Systems/System.Embedded.1/Bios/Settings" || info.SettingsFrom != "추정" {
			t.Fatalf("%v %+v", err, info)
		}
	})
	t.Run("supermicro-없음", func(t *testing.T) {
		s := startMockTree(t, "supermicro-x12", nil)
		c, _ := mockClient(s, ModeReadOnly, 0)
		defer c.Close()
		info, err := detectSystem(c)
		if err != nil || info.SettingsPath != "" || len(info.Notes) != 0 {
			t.Fatalf("Settings 가 없는 BMC 는 오류도 노트도 아니어야 함: %v %+v", err, info)
		}
		// 후보 2개를 한 번씩만 요청하고 둘 다 404
		if n := s.Count("GET", "/redfish/v1/Systems/1/Bios/"); n != 2 {
			t.Errorf("후보 요청 %d회, 기대 2", n)
		}
	})
}

// 오류는 RFError 상태 코드를 보존한다.
func TestDetectSystemErrorsKeepStatus(t *testing.T) {
	t.Run("ServiceRoot 503", func(t *testing.T) {
		s := startMockTree(t, "dell-r660", func(o *mockbmc.Options) {
			o.Fail = map[string]mockbmc.Fault{"/redfish/v1": {Status: 503}}
		})
		c, _ := mockClient(s, ModeReadOnly, 0)
		defer c.Close()
		info, err := detectSystem(c)
		var rf *RFError
		if info != nil || !asRF(err, &rf) || rf.Status != StatusBMCError || rf.HTTP != 503 {
			t.Errorf("info=%v err=%v", info, err)
		}
	})
	t.Run("Systems 503 은 부분 info 와 함께", func(t *testing.T) {
		s := startMockTree(t, "dell-r660", func(o *mockbmc.Options) {
			o.Fail = map[string]mockbmc.Fault{"/redfish/v1/Systems": {Status: 503}}
		})
		c, _ := mockClient(s, ModeReadOnly, 0)
		defer c.Close()
		info, err := detectSystem(c)
		var rf *RFError
		if !asRF(err, &rf) || rf.Status != StatusBMCError || info == nil || info.Vendor != "Dell" || info.Model != "" {
			t.Errorf("info=%+v err=%v", info, err)
		}
	})
	t.Run("401", func(t *testing.T) {
		s := startMockTree(t, "dell-r660", nil)
		c := NewClient(ClientOpts{BaseURL: s.URL(), User: testUser, Pass: "틀림", Insecure: true, Gap: -1})
		defer c.Close()
		_, err := detectSystem(c)
		var rf *RFError
		if !asRF(err, &rf) || rf.Status != StatusAuthFail || rf.HTTP != 401 {
			t.Errorf("err=%v", err)
		}
	})
	t.Run("레지스트리 404 는 오류가 아니라 노트", func(t *testing.T) {
		s := startMockTree(t, "dell-r660", nil)
		s.Remove("/redfish/v1/Registries/BiosAttributeRegistry.v1_0_0")
		c, _ := mockClient(s, ModeReadOnly, 0)
		defer c.Close()
		info, err := detectSystem(c)
		if err != nil || info.RegistryPath != "" || len(info.Notes) != 1 || !strings.Contains(info.Notes[0], "HTTP 404") {
			t.Errorf("err=%v info=%+v", err, info)
		}
	})
}

func asRF(err error, rf **RFError) bool { return errors.As(err, rf) }

// ---- 속성·레지스트리·후보 ----

func readTreeFile(t *testing.T, tree string, rel ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{"../../testdata", tree}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseAttrsAndRegistry(t *testing.T) {
	attrs, err := parseAttrs(readTreeFile(t, "dell-r660", "redfish/v1/Systems/System.Embedded.1/Bios.json"))
	if err != nil || attrs["SysProfile"] != "PerfOptimized" || attrs["LogicalProc"] != "Enabled" {
		t.Fatalf("parseAttrs: %v %v", err, attrs)
	}
	reg, err := parseRegistry(readTreeFile(t, "dell-r660", "redfish/v1/Registries/BiosAttributeRegistry.v1_0_0.json.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := reg.lookup("SysProfile")
	if a == nil || a.Type != "Enumeration" || a.ReadOnly || !reflect.DeepEqual(a.Values,
		[]string{"PerfPerWattOptimizedDapc", "PerfPerWattOptimizedOs", "PerfOptimized", "Custom"}) {
		t.Errorf("SysProfile 레지스트리: %+v", a)
	}
	if reg.lookup("sysprofile") != a || reg.lookup("없는속성") != nil {
		t.Errorf("대소문자 무시 조회 / 없는 속성")
	}
	var nilReg *attrRegistry
	if nilReg.lookup("x") != nil {
		t.Errorf("nil 레지스트리 조회는 nil")
	}

	// 숫자·불리언·객체 값도 문자열로
	m, err := parseAttrs([]byte(`{"Attributes":{"A":1,"B":true,"C":"x","D":{"k":[1, 2]},"E":null}}`))
	if err != nil || m["A"] != "1" || m["B"] != "true" || m["C"] != "x" || m["D"] != `{"k":[1,2]}` || m["E"] != "null" {
		t.Errorf("parseAttrs 값 표기: %v %v", err, m)
	}
	if _, err := parseAttrs([]byte("not json")); err == nil {
		t.Errorf("JSON 이 아니면 오류")
	}
	// 해석 불가 레지스트리 항목은 건너뛴다
	rg, err := parseRegistry([]byte(`{"RegistryEntries":{"Attributes":[{"AttributeName":"A","Type":"Integer","LowerBound":0,"UpperBound":9},"junk",{"AttributeName":"B","ReadOnly":true}]}}`))
	if err != nil || rg.lookup("A").Lower != "0" || rg.lookup("A").Upper != "9" || !rg.lookup("B").ReadOnly {
		t.Errorf("parseRegistry: %v %+v", err, rg)
	}
}

func TestFindStdCandidates(t *testing.T) {
	// 트리별 표준 4항목의 기대 후보 (testdata/README.md 의 표). "-" 는 후보 없음.
	want := map[string][4]string{
		"dell-r660":      {"SysProfile", "LogicalProc", "LlcPrefetch", "SubNumaCluster"},
		"hpe-dl360gen11": {"WorkloadProfile", "ProcHyperthreading", "LlcPrefetch", "SubNumaClustering"},
		"hpe-dl360gen10": {"WorkloadProfile", "ProcHyperthreading", "-", "SubNumaClustering"},
		"lenovo-sr650v3": {"OperatingModes_ChooseOperatingMode", "Processors_HyperThreading", "Processors_LLCPrefetch", "Processors_SNC"},
		"cisco-c220m7":   {"CPUPerformance", "IntelHyperThread", "LLCPrefetch", "SNC"},
		"supermicro-x12": {"PowerTechnology", "Hyper-Threading[ALL]", "LLCPrefetch", "SNC"},
	}
	for _, name := range mockTrees {
		name := name
		t.Run(name, func(t *testing.T) {
			s := startMockTree(t, name, nil)
			c, _ := mockClient(s, ModeReadOnly, 0)
			defer c.Close()
			info, err := detectSystem(c)
			if err != nil {
				t.Fatal(err)
			}
			bb, err := c.GetRaw(info.BiosPath)
			if err != nil {
				t.Fatal(err)
			}
			attrs, _ := parseAttrs(bb)
			rb, err := c.GetRaw(info.RegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			reg, err := parseRegistry(rb)
			if err != nil {
				t.Fatal(err)
			}
			ms := findStdCandidates(attrs, reg)
			stds := []string{"system_profile", "hyper_threading", "llc_prefetch", "sub_numa_cluster"}
			if len(ms) != 4 {
				t.Fatalf("항목 %d개", len(ms))
			}
			for i, m := range ms {
				if m.Std != stds[i] {
					t.Errorf("항목 순서 %d: %s", i, m.Std)
				}
				var names []string
				for _, c := range m.Cands {
					names = append(names, c.Name)
					if c.Reg == nil {
						t.Errorf("%s: 레지스트리에 없는 후보 %s", m.Std, c.Name)
					}
				}
				w := want[name][i]
				if w == "-" {
					if len(names) != 0 {
						t.Errorf("%s: 후보가 없어야 하는데 %v", m.Std, names)
					}
					continue
				}
				found := false
				for _, n := range names {
					found = found || n == w
				}
				if !found {
					t.Errorf("%s: 후보 %v 에 %s 없음", m.Std, names, w)
				}
			}
		})
	}
}

func TestNormAttrName(t *testing.T) {
	if got := normAttrName("Hyper-Threading[ALL]"); got != "hyperthreadingall" {
		t.Errorf("normAttrName = %q", got)
	}
}
