package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// inspect.go 는 dump 가 쓰고 check·all_bios_check 도 재사용하는 공용 부분입니다.
//   - detectSystem: ServiceRoot → Systems → Bios → Settings 경로 → 속성 레지스트리 위치
//   - Bios 속성/레지스트리 해석과 표준 4항목 이름 후보 탐색

const (
	rootPath             = "/redfish/v1"
	defaultSystemsPath   = "/redfish/v1/Systems"
	defaultManagersPath  = "/redfish/v1/Managers"
	defaultRegistriesDir = "/redfish/v1/Registries"
)

// odataLink 는 {"@odata.id": "..."} 객체입니다.
type odataLink struct {
	ID string `json:"@odata.id"`
}

// getFunc 는 GET 한 번을 추상화한 함수입니다. detectSystem 은 (*Client).GetRaw 를 넘기고,
// dump 는 응답을 저장·캐시하는 함수를 넘겨 같은 자원을 두 번 읽지 않게 합니다.
type getFunc func(p string) ([]byte, error)

// SystemInfo 는 BMC 1대의 시스템 판별 결과입니다.
// 경로는 BMC 가 알려 준 그대로입니다 (HPE 는 소문자·끝 슬래시 유지). 알 수 없으면 빈 문자열.
type SystemInfo struct {
	Vendor       string // 정규화: Dell / HPE / Lenovo / Cisco / Supermicro / 그 밖은 Manufacturer 원문
	Manufacturer string // Manufacturer 원문
	Model        string
	BiosVersion  string

	SystemPath   string // 사용한 System (여러 개면 첫 번째)
	BiosPath     string
	SettingsPath string // Bios 의 SettingsObject, 없으면 추정(Settings/Pending)한 경로. 없으면 빈 문자열
	SettingsFrom string // "SettingsObject" | "추정" | ""
	ManagerPath  string // System 을 관리하는 Manager (Links.ManagedBy[0], 없으면 Managers 컬렉션 첫 번째)
	RegistryPath string // 속성 레지스트리 JSON 의 위치 (Registries/<id> 의 Location[].Uri)

	RegistryID       string   // Bios.AttributeRegistry
	RegistryFilePath string   // /redfish/v1/Registries/<id>
	Systems          []string // Systems 컬렉션의 전체 멤버 (여러 개면 모두 목록화)
	SystemsPath      string
	ManagersPath     string
	RegistriesPath   string
	Notes            []string // 치명적이지 않은 조회 실패 (예: 레지스트리 파일 404)
}

// detectSystem 은 BMC 에서 시스템 정보를 읽습니다. 호출은 GET 뿐입니다.
//
// 오류는 *RFError 를 그대로 돌려줘 상태 코드가 보존됩니다. ServiceRoot 를 못 읽으면 info 는 nil 이지만,
// 그 뒤 단계에서 실패하면 그때까지 알아낸 info 를 오류와 함께 돌려줍니다 (dump 가 부분 결과를 쓰기 위해).
// 레지스트리·Settings 후보 조회 실패는 오류가 아니라 info.Notes 에 남깁니다.
func detectSystem(c *Client) (*SystemInfo, error) {
	return detectSystemWith(c.GetRaw)
}

func detectSystemWith(get getFunc) (*SystemInfo, error) {
	b, err := get(rootPath)
	if err != nil {
		return nil, err
	}
	var root struct {
		Vendor     string
		Systems    odataLink
		Managers   odataLink
		Registries odataLink
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, badJSON("ServiceRoot")
	}
	info := &SystemInfo{
		Vendor:         normalizeVendor(root.Vendor),
		SystemsPath:    orDefault(root.Systems.ID, defaultSystemsPath),
		ManagersPath:   orDefault(root.Managers.ID, defaultManagersPath),
		RegistriesPath: orDefault(root.Registries.ID, defaultRegistriesDir),
	}
	members, err := collectionMembers(get, info.SystemsPath)
	if err != nil {
		return info, err
	}
	if len(members) == 0 {
		return info, &RFError{Status: StatusUnsupported, Detail: "Systems 컬렉션에 멤버가 없습니다"}
	}
	info.Systems = members
	return info, info.fillSystem(get, members[0])
}

// fillSystem 은 System 1개의 정보(제조사·모델·BIOS·Settings·레지스트리·Manager)를 채웁니다.
// info 의 SystemsPath/ManagersPath/RegistriesPath 가 먼저 채워져 있어야 합니다.
func (info *SystemInfo) fillSystem(get getFunc, sysPath string) error {
	info.SystemPath = sysPath
	b, err := get(sysPath)
	if err != nil {
		return err
	}
	var sys struct {
		Manufacturer string
		Model        string
		BiosVersion  string
		Bios         odataLink
		Links        struct{ ManagedBy []odataLink }
	}
	if err := json.Unmarshal(b, &sys); err != nil {
		return badJSON("System")
	}
	info.Manufacturer = strings.TrimSpace(sys.Manufacturer)
	if info.Manufacturer != "" {
		info.Vendor = normalizeVendor(info.Manufacturer)
	}
	info.Model = strings.TrimSpace(sys.Model)
	info.BiosVersion = strings.TrimSpace(sys.BiosVersion)

	if len(sys.Links.ManagedBy) > 0 && sys.Links.ManagedBy[0].ID != "" {
		info.ManagerPath = sys.Links.ManagedBy[0].ID
	} else if ms, err := collectionMembers(get, info.ManagersPath); err == nil && len(ms) > 0 {
		info.ManagerPath = ms[0]
	}

	if sys.Bios.ID == "" {
		info.Notes = append(info.Notes, "System 에 Bios 링크가 없습니다")
		return nil
	}
	info.BiosPath = sys.Bios.ID
	bb, err := get(info.BiosPath)
	if err != nil {
		return err
	}
	var bios struct {
		AttributeRegistry string
		Settings          struct{ SettingsObject odataLink } `json:"@Redfish.Settings"`
	}
	if err := json.Unmarshal(bb, &bios); err != nil {
		return badJSON("Bios")
	}

	if id := bios.Settings.SettingsObject.ID; id != "" {
		info.SettingsPath, info.SettingsFrom = id, "SettingsObject"
	} else {
		info.probeSettings(get)
	}

	info.RegistryID = strings.TrimSpace(bios.AttributeRegistry)
	if info.RegistryID != "" {
		info.RegistryFilePath = strings.TrimRight(info.RegistriesPath, "/") + "/" + info.RegistryID
		rb, err := get(info.RegistryFilePath)
		if err != nil {
			info.Notes = append(info.Notes, "레지스트리 파일 조회 실패: "+errBrief(err))
		} else if uri := registryLocation(rb); uri != "" {
			info.RegistryPath = uri
		} else {
			info.Notes = append(info.Notes, "레지스트리 파일에 Location Uri 가 없습니다")
		}
	}
	return nil
}

// probeSettings 는 SettingsObject 선언이 없는 BMC 의 Settings 경로를 후보로 찾습니다.
// 후보: <Bios>/Settings, <Bios>/Pending (Lenovo 는 Pending 먼저). 404 면 다음 후보로 넘어가고,
// 그 밖의 오류는 노트에 남기고 멈춥니다. 모두 없으면 SettingsPath 는 빈 문자열 (쓰기 미지원 BMC).
func (info *SystemInfo) probeSettings(get getFunc) {
	base := strings.TrimRight(info.BiosPath, "/")
	cands := []string{base + "/Settings", base + "/Pending"}
	if info.Vendor == "Lenovo" {
		cands[0], cands[1] = cands[1], cands[0]
	}
	for _, c := range cands {
		_, err := get(c)
		if err == nil {
			info.SettingsPath, info.SettingsFrom = c, "추정"
			return
		}
		var rf *RFError
		if errors.Is(err, ErrNotAllowed) || (errors.As(err, &rf) && rf.HTTP == 404) {
			continue
		}
		info.Notes = append(info.Notes, "Settings 후보 조회 실패: "+errBrief(err))
		return
	}
}

// registryLocation 은 레지스트리 파일 리소스에서 JSON 위치(Location[].Uri)를 고릅니다.
// Language 가 en 인 것을 우선하고 없으면 Uri 가 있는 첫 번째입니다. 절대 URL 이면 경로만 씁니다.
func registryLocation(b []byte) string {
	var rf struct {
		Location []struct {
			Language string
			Uri      string
		}
	}
	if json.Unmarshal(b, &rf) != nil {
		return ""
	}
	pick := ""
	for _, l := range rf.Location {
		if l.Uri == "" {
			continue
		}
		if pick == "" {
			pick = l.Uri
		}
		if strings.HasPrefix(strings.ToLower(l.Language), "en") {
			pick = l.Uri
			break
		}
	}
	if u, err := url.Parse(pick); err == nil && u.Scheme != "" {
		return u.EscapedPath()
	}
	return pick
}

// collectionMembers 는 컬렉션의 멤버 @odata.id 목록을 돌려줍니다.
func collectionMembers(get getFunc, p string) ([]string, error) {
	b, err := get(p)
	if err != nil {
		return nil, err
	}
	return membersOf(b)
}

func membersOf(b []byte) ([]string, error) {
	var col struct{ Members []odataLink }
	if err := json.Unmarshal(b, &col); err != nil {
		return nil, badJSON("컬렉션")
	}
	var out []string
	for _, m := range col.Members {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

func badJSON(what string) error {
	return &RFError{Status: StatusHTTPError, Detail: what + " 응답을 JSON 으로 해석할 수 없습니다"}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// errBrief 는 오류를 "상태(HTTP 코드)" 한 토막으로 줄입니다. 본문·경로가 들어가지 않아 그대로 출력해도 안전합니다.
func errBrief(err error) string {
	var rf *RFError
	switch {
	case errors.As(err, &rf):
		if rf.HTTP != 0 {
			return fmt.Sprintf("%s(HTTP %d)", rf.Status, rf.HTTP)
		}
		return rf.Status
	case errors.Is(err, ErrNotAllowed):
		return "NOT_ALLOWED"
	case errors.Is(err, errDumpLimit):
		return "LIMIT"
	}
	return "ERROR"
}

// errStatus 는 오류의 상태 문자열과 HTTP 코드를 돌려줍니다 (RFError 가 아니면 HTTP_ERROR, 0).
func errStatus(err error) (string, int) {
	var rf *RFError
	if errors.As(err, &rf) {
		return rf.Status, rf.HTTP
	}
	return StatusHTTPError, 0
}

// normalizeVendor 는 Manufacturer 문자열을 5개 벤더 이름으로 정규화합니다. 모르는 값은 원문 그대로입니다.
func normalizeVendor(m string) string {
	m = strings.TrimSpace(m)
	l := strings.ToLower(m)
	switch {
	case strings.Contains(l, "dell"):
		return "Dell"
	case strings.Contains(l, "hpe"), strings.Contains(l, "hewlett"):
		return "HPE"
	case strings.Contains(l, "lenovo"):
		return "Lenovo"
	case strings.Contains(l, "cisco"):
		return "Cisco"
	case strings.Contains(l, "supermicro"), strings.Contains(l, "super micro"):
		return "Supermicro"
	}
	return m
}

// ---- Bios 속성 / 레지스트리 해석 ----

// parseAttrs 는 Bios(또는 Settings) 리소스의 Attributes 를 이름 → 표시 문자열로 바꿉니다.
// 문자열은 따옴표 없이, 숫자·불리언·객체는 JSON 표기 그대로입니다.
func parseAttrs(b []byte) (map[string]string, error) {
	var doc struct{ Attributes map[string]json.RawMessage }
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, badJSON("Bios")
	}
	out := make(map[string]string, len(doc.Attributes))
	for k, raw := range doc.Attributes {
		out[k] = rawToString(raw)
	}
	return out, nil
}

func rawToString(raw json.RawMessage) string {
	var s string
	if string(raw) != "null" && json.Unmarshal(raw, &s) == nil { // null 은 문자열로 해석되면 ""가 되므로 따로 둔다
		return s
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) == nil {
		return buf.String()
	}
	return string(raw)
}

// regAttr 은 속성 레지스트리의 속성 1개입니다.
type regAttr struct {
	Name     string
	Type     string // Enumeration / Integer / String / Boolean / Password ...
	ReadOnly bool
	Values   []string // Enumeration 의 허용값 (ValueName)
	Lower    string   // Integer 범위 (있을 때)
	Upper    string
}

// attrRegistry 는 속성 레지스트리 (이름은 정확히 일치 우선, 없으면 대소문자 무시).
type attrRegistry struct {
	byName  map[string]*regAttr
	byLower map[string]*regAttr
}

func (r *attrRegistry) lookup(name string) *regAttr {
	if r == nil {
		return nil
	}
	if a, ok := r.byName[name]; ok {
		return a
	}
	return r.byLower[strings.ToLower(name)]
}

// parseRegistry 는 RegistryEntries.Attributes 를 읽습니다. 해석할 수 없는 항목은 건너뜁니다.
func parseRegistry(b []byte) (*attrRegistry, error) {
	var doc struct {
		RegistryEntries struct{ Attributes []json.RawMessage }
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, badJSON("레지스트리")
	}
	r := &attrRegistry{byName: map[string]*regAttr{}, byLower: map[string]*regAttr{}}
	for _, raw := range doc.RegistryEntries.Attributes {
		var a struct {
			AttributeName string
			Type          string
			ReadOnly      bool
			LowerBound    json.RawMessage
			UpperBound    json.RawMessage
			Value         []struct{ ValueName string }
		}
		if json.Unmarshal(raw, &a) != nil || a.AttributeName == "" {
			continue
		}
		ra := &regAttr{Name: a.AttributeName, Type: a.Type, ReadOnly: a.ReadOnly}
		for _, v := range a.Value {
			ra.Values = append(ra.Values, v.ValueName)
		}
		ra.Lower, ra.Upper = rawToString(a.LowerBound), rawToString(a.UpperBound)
		r.byName[ra.Name] = ra
		if _, ok := r.byLower[strings.ToLower(ra.Name)]; !ok {
			r.byLower[strings.ToLower(ra.Name)] = ra
		}
	}
	return r, nil
}

// ---- 표준 4항목 이름 후보 탐색 ----

// stdKeyword 는 표준 항목 1개를 찾는 키워드입니다.
type stdKeyword struct {
	Std  string
	Keys []string
}

// stdKeywords 는 표준 4항목(계획서 4장 프로파일의 std_name)의 속성 이름 후보 키워드 표입니다 (한곳에 모음).
// 속성 이름에서 영숫자만 남겨 소문자로 만든 뒤 부분 일치로 찾습니다
// ("Hyper-Threading[ALL]" → "hyperthreadingall"). 벤더별 실제 이름은 첫 실장비 덤프로 확정합니다.
var stdKeywords = []stdKeyword{
	{"system_profile", []string{
		"profile", "workload", // Dell SysProfile, HPE WorkloadProfile
		"operatingmode", "cpuperformance", "powertechnology", // Lenovo / Cisco / Supermicro 후보
	}},
	{"hyper_threading", []string{"hyperthread", "logical"}}, // ProcHyperthreading, LogicalProc, Hyper-Threading
	{"llc_prefetch", []string{"llc"}},
	{"sub_numa_cluster", []string{"numa", "snc"}}, // SubNumaCluster, SubNumaClustering, SNC
}

// attrCand 는 후보 속성 1개입니다. Reg 는 레지스트리에 있을 때만 채워집니다.
type attrCand struct {
	Name  string
	Value string
	Reg   *regAttr
}

// stdMatch 는 표준 항목 1개의 후보 목록입니다 (없으면 Cands 가 비어 있음).
type stdMatch struct {
	Std   string
	Cands []attrCand
}

func normAttrName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// findStdCandidates 는 Bios 속성에서 표준 4항목의 이름 후보를 키워드로 찾습니다 (reg 는 nil 이어도 됨).
// 결과는 항상 stdKeywords 순서의 4개이며, 후보는 이름순입니다.
func findStdCandidates(attrs map[string]string, reg *attrRegistry) []stdMatch {
	names := make([]string, 0, len(attrs))
	for n := range attrs {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]stdMatch, 0, len(stdKeywords))
	for _, kw := range stdKeywords {
		m := stdMatch{Std: kw.Std}
		for _, n := range names {
			nn := normAttrName(n)
			for _, k := range kw.Keys {
				if strings.Contains(nn, k) {
					m.Cands = append(m.Cands, attrCand{Name: n, Value: attrs[n], Reg: reg.lookup(n)})
					break
				}
			}
		}
		out = append(out, m)
	}
	return out
}
