package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strings"

	"biostool/internal/ucsmxml"
)

// trs_cisco.go 는 `biostool trs-cisco` — Cisco 장비(예: UCSX-210C-M7 블레이드)가 bios.sh 에서
// "UNSUPPORTED [XML 실패 → Redfish]" 로 나올 때 어느 쪽이 왜 막히는지 좁히는 진단 명령입니다.
//
//   - bios.sh 는 Cisco 를 XML API(UCSM/CIMC)로 먼저 읽고, 로그인 전에 실패하면 Redfish 로 한 번 더 읽습니다.
//     Redfish 에서 System 에 Bios 리소스가 없거나 속성이 비면 UNSUPPORTED 입니다. 이 명령은 두 경로를 모두 보여 줍니다.
//   - 읽기 전용입니다. Redfish 는 trs 와 같은 Prober(GET/HEAD/OPTIONS + 이번 실행의 세션 생성·삭제)만 쓰고,
//     XML 은 (1) GET / (2) 쿠키 없는 configResolveClass 한 번(인증 요구 응답만 받음) (3) 필요할 때 aaaLogin 1회와
//     configResolveClass 조회·aaaLogout 만 보냅니다 (ucsmxml 허용목록).
//   - 계정 잠금 방지: Redfish 에서 401 이 나왔으면 같은 계정으로 XML 로그인을 하지 않습니다. XML 로그인은 1번뿐이고,
//     XML API 가 없는 주소(응답이 XML 이 아님)에는 비밀번호를 보내지 않습니다.
//   - 비밀번호·토큰은 출력·저장하지 않습니다 (trs 와 같은 규칙).

const (
	ciscoMaxSystems = 8  // 읽어 보는 Systems 멤버 수 상한
	ciscoMaxChassis = 6  // 읽어 보는 Chassis 멤버 수 상한
	ciscoMaxManager = 4  // 읽어 보는 Managers 멤버 수 상한
	ciscoMaxFW      = 3  // 자세히 읽는 BIOS 펌웨어 항목 수 상한
	ciscoCrawlMax   = 60 // Bios 속성을 찾으려고 읽는 하위 리소스 수 상한
	ciscoCrawlDepth = 3  // 그 탐색 깊이
	ciscoShowLinks  = 24 // System 이 가진 링크를 보여 주는 최대 수
	ciscoMaxServers = 12 // XML 로 읽은 서버를 나열하는 최대 수

)

type ciscoSys struct {
	path, manufacturer, model, sku, biosVersion, power string
	biosLink                                           string
	biosStatus                                         int // Bios GET 상태 (0 = 시도 안 함)
	attrs                                              int
	registry                                           string
	links                                              []string // System 이 가진 하위 링크 (탐색 출발점)
	cands                                              []string // Bios 링크가 없을 때 후보 경로 결과 "경로 → 상태"
}

type ciscoXML struct {
	skipped                     string // 로그인을 건너뛴 사유 ("" = 건너뛰지 않음)
	rootStatus                  int
	rootErr                     string
	nuovaStatus                 int
	nuovaErr                    string
	kind                        string // "xml"(XML API 응답) | "html" | "other" | "" (응답 없음)
	rootName, errCode, errDescr string
	loginTried, loggedIn        bool
	loginStatus, loginCode      string
	loginHTTP                   int
	loginDetail                 string
	servers                     []*ucsmServer
	picked                      []*ucsmServer
	classInfo                   map[string]string // xml_class → "N개" 또는 오류 사유
	classOrder                  []string
	serverErr                   string
}

type ciscoHost struct {
	*trsHost
	systems  []ciscoSys
	biosLike []string // 표준 Bios 링크 밖에서 찾은 BIOS 속성 리소스 "경로 (속성 N개)"
	fwBios   []string // 펌웨어 목록의 BIOS 항목 "이름 버전"
	biosReg  []string // 레지스트리 목록의 BIOS 관련 항목
	crawlN   int
	x        ciscoXML
}

// cmdTrsCisco 는 `biostool trs-cisco` 입니다. 공통 옵션(-conf -user -hosts -rf-conf -profile)과 -targets -path 를 받습니다.
// -profile 이 있으면 그 프로파일의 # [xml] 구역 클래스를 XML 로 읽어 봅니다 (없으면 기본 VM, 없어도 진행).
func cmdTrsCisco(args []string) error {
	return trsMain("trs-cisco", "_trs_cisco", args, func(t Target, rc *runContext, paths []string) string {
		return ciscoRunHost(t, rc, paths)
	})
}

func ciscoRunHost(t Target, rc *runContext, extra []string) string {
	th := newTrsHost(t, rc.Conf, rc.Password, rc.Fallback)
	defer th.pr.Close()
	h := &ciscoHost{trsHost: th}
	h.run(extra)
	return h.out.String()
}

// run 은 진단 전체 단계를 순서대로 실행하고 리포트에 [진단] 을 덧붙입니다.
func (h *ciscoHost) run(extra []string) {
	h.ciscoRouting()
	h.stepNetwork()
	h.stepRoot()
	if h.f.rootStatus != 0 {
		h.stepAuth()
		h.ciscoRedfish(extra)
		h.stepReplay()
	}
	h.stepLogout()
	h.ciscoXMLStep()
	h.log("")
	h.log("[진단] 요청 %d건", h.n)
	for _, s := range ciscoDiagnose(h) {
		h.log("  %s", s)
	}
}

// ---- 0. bios.sh 가 이 대상을 어떻게 보는지 ----

func (h *ciscoHost) ciscoRouting() {
	name := targetBaseName(h.t)
	h.log("")
	h.log("[0] bios.sh 가 이 대상을 분류하는 방식")
	isIP := net.ParseIP(strings.Trim(name, "[]")) != nil
	switch {
	case isIP:
		h.log("  대상 %s 은 IP 입니다 — bios.sh 는 IP 대상을 접두어와 관계없이 Redfish 로 읽습니다 (Cisco XML 은 이름이 xml_host_prefix 로 시작하거나 -user-xml 목록일 때).", h.clean(name))
	case hasXMLPrefix(h.conf, name):
		h.log("  이름 %s 이 xml_host_prefix(%s)로 시작합니다 — bios.sh 는 XML API 를 먼저 시도하고, 로그인 전에 실패하면 Redfish 로 한 번 더 읽습니다 (비고 \"XML 실패 → Redfish\").", h.clean(name), strings.Join(h.conf.XMLHostPrefix, ","))
	default:
		h.log("  이름 %s 은 xml_host_prefix(%s)로 시작하지 않아 bios.sh 는 Redfish 로 읽습니다.", h.clean(name), strings.Join(h.conf.XMLHostPrefix, ","))
	}
	h.log("  진단 대상 주소: %s (hostname-m 이 가리키는 주소 — XML 과 Redfish 모두 이 주소로 접속)", h.hostport)
}

// ---- 4. Cisco Redfish 구조 ----

func (h *ciscoHost) quiet(fn func()) {
	saved := h.out
	h.out = &bytes.Buffer{}
	defer func() { h.out = saved }()
	fn()
}

func isOK(r trsRes) bool { return r.err == nil && r.status >= 200 && r.status < 300 }

// jsonLinks 는 문서 안의 모든 @odata.id 문자열입니다 (Actions 아래는 제외, 등장 순, 중복 제거).
func jsonLinks(b []byte) []string {
	var v interface{}
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	var walk func(x interface{}, inActions bool)
	walk = func(x interface{}, inActions bool) {
		switch t := x.(type) {
		case map[string]interface{}:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				val := t[k]
				if k == "@odata.id" {
					if s, ok := val.(string); ok && !inActions && !seen[s] && strings.HasPrefix(s, "/redfish/") {
						seen[s] = true
						out = append(out, s)
					}
					continue
				}
				walk(val, inActions || k == "Actions")
			}
		case []interface{}:
			for _, e := range t {
				walk(e, inActions)
			}
		}
	}
	walk(v, false)
	return out
}

func rawStr(doc map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := doc[key]; ok && json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func rawLink(doc map[string]json.RawMessage, key string) string {
	var l trsLink
	if raw, ok := doc[key]; ok && json.Unmarshal(raw, &l) == nil {
		return l.ID
	}
	return ""
}

func memberIDs(b []byte) []string {
	var v struct{ Members []trsLink }
	var out []string
	if json.Unmarshal(b, &v) == nil {
		for _, m := range v.Members {
			if m.ID != "" {
				out = append(out, m.ID)
			}
		}
	}
	return out
}

func (h *ciscoHost) ciscoRedfish(extra []string) {
	if !h.f.tokenOK && !h.f.basicOK {
		h.log("")
		h.log("[4] Cisco Redfish 구조 — 인증이 되지 않아 건너뜁니다 (위 [3] 확인)")
		return
	}
	h.log("")
	h.log("[4] Cisco Redfish 구조 (System·Bios 링크 → Chassis·Manager → 레지스트리·펌웨어 목록 → Bios 속성 탐색)")
	get := func(label, p string) trsRes {
		r := h.authed(label, p)
		h.note403(p, r)
		return r
	}
	r := get("Systems", "/redfish/v1/Systems")
	if !isOK(r) {
		h.log("  Systems 컬렉션을 읽지 못했습니다")
		return
	}
	members := memberIDs(r.body)
	h.log("        Systems 멤버 %d개: %s", len(members), h.clean(strings.Join(members, ", ")))
	for i, sp := range members {
		if i >= ciscoMaxSystems {
			h.log("        (나머지 %d개는 읽지 않음)", len(members)-i)
			break
		}
		h.ciscoSystem(get, sp)
	}
	h.ciscoChassisManager(get)
	h.ciscoRegistries(get)
	h.ciscoFirmware(get)
	h.ciscoCrawl()
	for _, p := range extra {
		get("사용자 지정", p)
	}
}

func (h *ciscoHost) ciscoSystem(get func(label, p string) trsRes, sp string) {
	s := ciscoSys{path: sp}
	r := get("System", sp)
	if !isOK(r) {
		h.systems = append(h.systems, s)
		return
	}
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(r.body, &doc)
	s.manufacturer, s.model, s.sku = rawStr(doc, "Manufacturer"), rawStr(doc, "Model"), rawStr(doc, "SKU")
	s.biosVersion, s.power = rawStr(doc, "BiosVersion"), rawStr(doc, "PowerState")
	s.biosLink = rawLink(doc, "Bios")
	s.links = jsonLinks(r.body)
	h.log("        Manufacturer=%s Model=%s SKU=%s BiosVersion=%s PowerState=%s", h.clean(s.manufacturer), h.clean(s.model), h.clean(s.sku), h.clean(s.biosVersion), h.clean(s.power))
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h.log("        System 최상위 항목: %s", h.clean(strings.Join(keys, ", ")))
	show := s.links
	if len(show) > ciscoShowLinks {
		show = show[:ciscoShowLinks]
	}
	h.log("        System 이 가리키는 링크 %d개: %s", len(s.links), h.clean(strings.Join(show, ", ")))
	if oem, ok := doc["Oem"]; ok {
		var om map[string]json.RawMessage
		if json.Unmarshal(oem, &om) == nil {
			for vendor, raw := range om {
				var sub map[string]json.RawMessage
				if json.Unmarshal(raw, &sub) == nil {
					ks := make([]string, 0, len(sub))
					for k := range sub {
						ks = append(ks, k)
					}
					sort.Strings(ks)
					h.log("        Oem.%s 항목: %s", h.clean(vendor), h.clean(strings.Join(ks, ", ")))
				}
			}
		}
	}
	if s.biosLink != "" {
		b := get("Bios (System 이 가리키는 링크)", s.biosLink)
		if b.err == nil {
			s.biosStatus = b.status
		}
		if isOK(b) {
			var bi struct {
				Attributes        map[string]json.RawMessage
				AttributeRegistry string
			}
			_ = json.Unmarshal(b.body, &bi)
			s.attrs, s.registry = len(bi.Attributes), bi.AttributeRegistry
			h.log("        BIOS 속성 %d개, AttributeRegistry=%s", s.attrs, h.clean(s.registry))
		}
	} else {
		h.log("  ✘ System 에 Bios 링크가 없습니다 — 흔한 위치를 직접 확인합니다 (후보, 404 가 정상일 수 있음):")
		for _, suf := range []string{"/Bios", "/BIOS", "/Bios/Settings", "/Oem/Cisco/Bios"} {
			p := strings.TrimRight(sp, "/") + suf
			c := h.authed("Bios 후보", p)
			st := 0
			if c.err == nil {
				st = c.status
			}
			s.cands = append(s.cands, fmt.Sprintf("%s → %d", p, st))
			if isOK(c) {
				n := ciscoAttrCount(c.body)
				h.biosLike = append(h.biosLike, fmt.Sprintf("%s (속성 %d개)", p, n))
			}
		}
	}
	h.systems = append(h.systems, s)
}

func ciscoAttrCount(b []byte) int {
	var v struct{ Attributes map[string]json.RawMessage }
	if json.Unmarshal(b, &v) == nil {
		return len(v.Attributes)
	}
	return 0
}

func (h *ciscoHost) ciscoChassisManager(get func(label, p string) trsRes) {
	if r := get("Chassis", "/redfish/v1/Chassis"); isOK(r) {
		for i, cp := range memberIDs(r.body) {
			if i >= ciscoMaxChassis {
				break
			}
			if c := get("Chassis 항목", cp); isOK(c) {
				var doc map[string]json.RawMessage
				_ = json.Unmarshal(c.body, &doc)
				h.log("        %s: ChassisType=%s Manufacturer=%s Model=%s SKU=%s PartNumber=%s", h.clean(lastSeg(cp)), h.clean(rawStr(doc, "ChassisType")),
					h.clean(rawStr(doc, "Manufacturer")), h.clean(rawStr(doc, "Model")), h.clean(rawStr(doc, "SKU")), h.clean(rawStr(doc, "PartNumber")))
			}
		}
	}
	if r := get("Managers", "/redfish/v1/Managers"); isOK(r) {
		for i, mp := range memberIDs(r.body) {
			if i >= ciscoMaxManager {
				break
			}
			if m := get("Manager", mp); isOK(m) {
				var doc map[string]json.RawMessage
				_ = json.Unmarshal(m.body, &doc)
				h.log("        %s: ManagerType=%s Model=%s FirmwareVersion=%s", h.clean(lastSeg(mp)), h.clean(rawStr(doc, "ManagerType")), h.clean(rawStr(doc, "Model")), h.clean(rawStr(doc, "FirmwareVersion")))
			}
		}
	}
}

func (h *ciscoHost) ciscoRegistries(get func(label, p string) trsRes) {
	r := get("Registries", "/redfish/v1/Registries")
	if !isOK(r) {
		return
	}
	ids := memberIDs(r.body)
	for _, id := range ids {
		if strings.Contains(strings.ToLower(id), "bios") {
			h.biosReg = append(h.biosReg, id)
		}
	}
	h.log("        Registries %d개 중 BIOS 관련: %s", len(ids), h.clean(orDefault(strings.Join(h.biosReg, ", "), "없음")))
}

func (h *ciscoHost) ciscoFirmware(get func(label, p string) trsRes) {
	fwPath := "/redfish/v1/UpdateService/FirmwareInventory"
	if u := get("UpdateService", "/redfish/v1/UpdateService"); isOK(u) {
		var doc map[string]json.RawMessage
		_ = json.Unmarshal(u.body, &doc)
		if l := rawLink(doc, "FirmwareInventory"); l != "" {
			fwPath = l
		}
	}
	r := get("FirmwareInventory", fwPath)
	if !isOK(r) {
		return
	}
	ids := memberIDs(r.body)
	var bios []string
	for _, id := range ids {
		if strings.Contains(strings.ToLower(id), "bios") {
			bios = append(bios, id)
		}
	}
	h.log("        FirmwareInventory %d개 중 BIOS 이름이 든 항목: %s", len(ids), h.clean(orDefault(strings.Join(bios, ", "), "없음")))
	for i, id := range bios {
		if i >= ciscoMaxFW {
			break
		}
		if f := get("펌웨어 항목", id); isOK(f) {
			var doc map[string]json.RawMessage
			_ = json.Unmarshal(f.body, &doc)
			h.fwBios = append(h.fwBios, fmt.Sprintf("%s %s", rawStr(doc, "Name"), rawStr(doc, "Version")))
		}
	}
}

// ciscoCrawl 은 System 아래(와 Oem·bios·boot·setting 이 든 경로)를 얕게 따라가며 BIOS 속성을 가진 리소스를 찾습니다.
// 읽기(GET)만 하고, 로그/Actions 경로는 Prober 가 막습니다. 요청 줄은 접어서 요약만 보여 줍니다.
func (h *ciscoHost) ciscoCrawl() {
	if len(h.systems) == 0 || (!h.f.tokenOK && !h.f.basicOK) {
		return
	}
	type node struct {
		p string
		d int
	}
	visited := map[string]bool{}
	var queue []node
	std := map[string]bool{}
	var roots []string
	for _, s := range h.systems {
		visited[s.path] = true
		roots = append(roots, s.path)
		if s.biosLink != "" {
			std[s.biosLink] = true
		}
	}
	for _, s := range h.systems {
		for _, l := range s.links {
			queue = append(queue, node{l, 1})
		}
	}
	allowed := func(p string) bool {
		if p == "" || visited[p] {
			return false
		}
		lc := strings.ToLower(p)
		for _, seg := range strings.Split(lc, "/") {
			if deniedGetSeg(seg) {
				return false
			}
		}
		for _, r := range roots {
			if strings.HasPrefix(p, strings.TrimRight(r, "/")+"/") {
				return true
			}
		}
		for _, k := range []string{"oem", "bios", "boot", "setting"} {
			if strings.Contains(lc, k) {
				return true
			}
		}
		return false
	}
	codes := map[int]int{}
	var odd []string
	for len(queue) > 0 && h.crawlN < ciscoCrawlMax {
		n := queue[0]
		queue = queue[1:]
		if !allowed(n.p) {
			continue
		}
		visited[n.p] = true
		var res trsRes
		h.quiet(func() { res = h.authed("탐색", n.p) })
		h.crawlN++
		st := 0
		if res.err == nil {
			st = res.status
		}
		codes[st]++
		if st != 200 && st != 404 && len(odd) < 8 {
			odd = append(odd, fmt.Sprintf("%s → %d", n.p, st))
		}
		if !isOK(res) {
			continue
		}
		if cnt := ciscoAttrCount(res.body); cnt >= 5 && !std[n.p] {
			h.biosLike = append(h.biosLike, fmt.Sprintf("%s (속성 %d개)", n.p, cnt))
		} else if strings.Contains(strings.ToLower(n.p), "bios") && cnt == 0 && !std[n.p] {
			h.biosLike = append(h.biosLike, fmt.Sprintf("%s (BIOS 이름의 리소스, 속성 없음)", n.p))
		}
		if n.d < ciscoCrawlDepth {
			for _, l := range jsonLinks(res.body) {
				queue = append(queue, node{l, n.d + 1})
			}
		}
	}
	var cs []string
	var ks []int
	for k := range codes {
		ks = append(ks, k)
	}
	sort.Ints(ks)
	for _, k := range ks {
		cs = append(cs, fmt.Sprintf("%d:%d건", k, codes[k]))
	}
	h.log("  하위 리소스 탐색 %d건 (최대 %d건·깊이 %d): %s", h.crawlN, ciscoCrawlMax, ciscoCrawlDepth, orDefault(strings.Join(cs, ", "), "읽은 것 없음"))
	if len(odd) > 0 {
		h.log("        200·404 가 아닌 경로: %s", h.clean(strings.Join(odd, "; ")))
	}
	if len(h.biosLike) == 0 {
		h.log("        표준 Bios 링크 밖에서 BIOS 속성을 가진 리소스는 찾지 못했습니다")
	}
	for _, b := range h.biosLike {
		h.log("        ◎ BIOS 속성 후보: %s", h.clean(b))
	}
}

// ---- 7. XML API ----

func (h *ciscoHost) ciscoXMLStep() {
	x := &h.x
	h.log("")
	h.log("[7] Cisco XML API (UCSM/CIMC, bios.sh 의 Cisco 경로 — 같은 주소 %s)", h.hostport)
	if h.f.tcpErr != "" {
		x.skipped = "TCP 연결 실패"
		h.log("  TCP 연결이 되지 않아 건너뜁니다")
		return
	}
	// (1)(2) 인증 없는 확인: GET / 와 쿠키 없는 configResolveClass 한 번 (ucsmxml.Probe)
	p := ucsmxml.Probe(h.t.BaseURL(), h.conf.Insecure, h.conf.Timeout)
	h.n += p.Requests
	x.rootStatus, x.nuovaStatus, x.kind = p.RootStatus, p.APIStatus, p.Kind
	x.rootName, x.errCode, x.errDescr = p.RootElement, p.ErrCode, h.clean(p.ErrDescr)
	x.nuovaErr = h.clean(p.APIErr)
	if p.RootErr != "" {
		h.log("  GET /  → 실패: %s", h.clean(p.RootErr))
	} else {
		h.log("  GET /  → %d  Server=%s Location=%s Content-Type=%s 페이지 제목=%q", p.RootStatus, h.clean(p.RootServer), h.clean(p.RootLocation), h.clean(p.RootContentType), h.clean(p.RootTitle))
	}
	if p.APIErr != "" {
		h.log("  POST /nuova (쿠키 없는 확인 요청) → 실패: %s", x.nuovaErr)
	} else {
		h.log("  POST /nuova (쿠키 없는 확인 요청) → %d  Content-Type=%s  응답 종류=%s 루트=%s errorCode=%s %s",
			p.APIStatus, h.clean(p.APIContentType), map[string]string{ucsmxml.KindXML: "XML API", ucsmxml.KindHTML: "웹 페이지(XML 아님)", ucsmxml.KindOther: "기타"}[p.Kind], h.clean(p.RootElement), h.clean(p.ErrCode), x.errDescr)
	}
	if x.kind != "xml" {
		x.skipped = "XML API 응답이 없는 주소라 비밀번호를 보내지 않음"
		h.log("  → 이 주소는 XML API(/nuova) 로 응답하지 않습니다. 비밀번호를 보내는 로그인은 하지 않습니다.")
		return
	}
	if h.f.halted {
		x.skipped = "Redfish 단계에서 401 (같은 계정 잠금 방지)"
		h.log("  → Redfish 에서 401 이 나온 계정이라 잠금 방지를 위해 XML 로그인을 하지 않습니다")
		return
	}
	// (3) aaaLogin 1회 + 조회
	h.log("  XML API 가 응답합니다 — aaaLogin 1회로 서버 목록을 읽습니다 (읽기 전용, 끝에 aaaLogout)")
	var classes []string
	if prof := ciscoProfile(h.conf); prof != nil && prof.XML != nil {
		classes = prof.XML.Classes
	}
	c := ucsmxml.New(ucsmxml.Options{BaseURL: h.t.BaseURL(), User: h.user, Pass: h.pass, Insecure: h.conf.Insecure, Timeout: h.conf.Timeout, Classes: classes})
	defer c.Logout()
	x.loginTried = true
	h.n++
	if err := c.Login(); err != nil {
		var xe *ucsmxml.Error
		if errors.As(err, &xe) {
			x.loginStatus, x.loginHTTP, x.loginCode, x.loginDetail = xe.Status, xe.HTTP, xe.Code, h.clean(xe.Detail)
		} else {
			x.loginStatus, x.loginDetail = "ERROR", h.clean(err.Error())
		}
		h.log("  ✘ aaaLogin 실패: %s HTTP=%d errorCode=%s — %s", x.loginStatus, x.loginHTTP, h.clean(x.loginCode), x.loginDetail)
		return
	}
	x.loggedIn = true
	h.log("  ✔ aaaLogin 성공")
	byDN := map[string]*ucsmServer{}
	for _, cls := range []string{ucsmxml.BladeClass, ucsmxml.RackClass} {
		h.n++
		objs, err := c.ResolveClass(cls)
		if err != nil {
			h.log("    %s 조회: %s", cls, h.clean(ucsmErrBrief(err)))
			if !ucsmAPIError(err) {
				x.serverErr = h.clean(ucsmErrBrief(err))
				return
			}
			continue
		}
		h.log("    %s: %d개", cls, len(objs))
		for _, o := range objs {
			dn := o.Attrs["dn"]
			if dn == "" || byDN[dn] != nil {
				continue
			}
			s := &ucsmServer{DN: dn, Model: o.Attrs["model"]}
			byDN[dn] = s
			x.servers = append(x.servers, s)
		}
	}
	h.n++
	if sps, err := c.ResolveClass(ucsmxml.ServiceProfileClass); err == nil {
		h.log("    %s(서비스 프로파일): %d개", ucsmxml.ServiceProfileClass, len(sps))
		for _, sp := range sps {
			if s := byDN[sp.Attrs["pnDn"]]; s != nil && s.SP == "" {
				s.SP = sp.Attrs["name"]
			}
		}
	} else {
		h.log("    %s 조회: %s", ucsmxml.ServiceProfileClass, h.clean(ucsmErrBrief(err)))
	}
	name := targetBaseName(h.t)
	x.picked = xmlPickServers(x.servers, name)
	for i, s := range x.servers {
		if i >= ciscoMaxServers {
			h.log("      (나머지 %d대는 나열하지 않음)", len(x.servers)-i)
			break
		}
		h.log("      서버 dn=%s model=%s 서비스프로파일=%s", h.clean(s.DN), h.clean(orDefault(s.Model, "-")), h.clean(orDefault(s.SP, "-")))
	}
	switch {
	case len(x.servers) == 0:
		h.log("  ✘ computeBlade·computeRackUnit 서버가 없습니다 (bios.sh 라면 UNSUPPORTED)")
	case len(x.servers) > 1 && len(x.picked) == len(x.servers):
		h.log("  ! 서비스 프로파일 이름이 호스트 이름(%s)과 같은 서버가 없어 모든 서버를 '<호스트>/<dn>' 으로 보고하게 됩니다", h.clean(name))
	default:
		h.log("  호스트 이름(%s)에 해당하는 서버 %d대: %s", h.clean(name), len(x.picked), ciscoDNs(x.picked))
	}
	x.classInfo = map[string]string{}
	for _, cls := range classes {
		h.n++
		objs, err := c.ResolveClass(cls)
		x.classOrder = append(x.classOrder, cls)
		if err != nil {
			x.classInfo[cls] = h.clean(ucsmErrBrief(err))
			continue
		}
		x.classInfo[cls] = fmt.Sprintf("%d개", len(objs))
	}
	for _, cls := range x.classOrder {
		h.log("    프로파일 클래스 %s: %s", cls, x.classInfo[cls])
	}
	if len(classes) == 0 {
		h.log("    (프로파일 # [xml] 구역을 읽지 못해 bios* 클래스는 조회하지 않았습니다 — -profile 로 프로파일을 지정하면 클래스까지 확인합니다)")
	}
}

func ciscoDNs(ss []*ucsmServer) string {
	var out []string
	for _, s := range ss {
		out = append(out, s.DN)
	}
	return strings.Join(out, ", ")
}

// ciscoProfile 은 -profile(기본 VM)의 프로파일을 읽습니다 (실패하면 nil, 진단은 계속).
func ciscoProfile(conf *Config) *Profile {
	p, err := loadProfile(filepath.Join(conf.ProfileDir, ucsmDefaultProfile+".tsv"), ucsmDefaultProfile)
	if err != nil {
		return nil
	}
	return p
}

// ---- 진단 ----

// ciscoDiagnose 는 관찰 결과에서 원인 후보를 만듭니다 (확정이 아니라 가능성이 높은 순서).
func ciscoDiagnose(h *ciscoHost) []string {
	f := &h.f
	x := &h.x
	var out []string
	add := func(s string) { out = append(out, s) }
	authed := f.tokenOK || f.basicOK
	switch {
	case f.rootStatus == 404 && f.tcpErr == "" && f.tlsErr == "":
		add("● 이 주소(" + h.hostport + ")에는 Redfish 서비스가 없습니다 (/redfish/v1 → 404). UCSM/CIMC XML 전용 주소이거나 Redfish 가 꺼진 BMC 입니다 — 아래 XML 결과를 보십시오.")
	case f.tcpErr != "" || f.tlsErr != "" || f.rootStatus == 0 || f.halted || !authed:
		out = append(out, trsDiagnose(f)...)
	}

	// Redfish 쪽: BIOS 속성을 읽을 수 있는가
	if authed && len(h.systems) > 0 {
		var good *ciscoSys
		for i := range h.systems {
			if s := &h.systems[i]; s.biosStatus >= 200 && s.biosStatus < 300 && s.attrs > 0 {
				good = s
				break
			}
		}
		switch {
		case good != nil:
			add(fmt.Sprintf("● Redfish 로는 BIOS 를 읽을 수 있습니다: %s (속성 %d개, System 모델 %s). 그런데도 bios.sh 가 UNSUPPORTED 였다면 아래 [6] 재현 결과와 비고 전체 문구를 알려 주십시오.", good.biosLink, good.attrs, orDefault(good.model, "-")))
		default:
			s := &h.systems[0]
			for i := range h.systems {
				if h.systems[i].model != "" {
					s = &h.systems[i]
					break
				}
			}
			switch {
			case s.biosLink != "" && (s.biosStatus < 200 || s.biosStatus >= 300):
				add(fmt.Sprintf("● System 에 Bios 링크(%s)는 있으나 읽기가 %d 입니다 — 계정 권한(Role)이나 호스트 전원/POST 상태를 확인하십시오 (위 [4] 의 오류 메시지 참고).", s.biosLink, s.biosStatus))
			case s.biosLink != "":
				add(fmt.Sprintf("● Bios 리소스(%s)는 있으나 Attributes 가 비어 있습니다 (PowerState=%s). 호스트가 꺼져 있거나 BIOS 가 아직 값을 올리지 않은 경우입니다 — 전원을 켜고 POST 가 끝난 뒤 다시 확인하십시오. bios.sh 의 UNSUPPORTED(Bios Attributes 가 비어 있습니다)와 같은 상태입니다.", s.biosLink, orDefault(s.power, "-")))
			case len(h.biosLike) > 0:
				add("● System 의 표준 Bios 링크는 없지만 다음 경로에 BIOS 속성이 있습니다: " + strings.Join(h.biosLike, "; ") + " — 도구가 이 경로를 읽도록 수정할 대상입니다. 이 결과를 그대로 전달해 주십시오.")
			default:
				msg := "● 이 BMC 의 Redfish 에는 BIOS 설정 리소스가 없습니다: System(" + orDefault(s.model, "-") + ")에 Bios 링크가 없고, 흔한 후보 경로와 Oem·하위 리소스 탐색(" + fmt.Sprint(h.crawlN) + "건)에서도 BIOS 속성을 찾지 못했습니다. bios.sh 의 \"UNSUPPORTED — System 에 Bios 리소스가 없습니다\" 가 바로 이것입니다."
				add(msg)
				if len(h.fwBios) > 0 {
					add("  (펌웨어 목록에는 BIOS 항목이 있습니다 — " + strings.Join(h.fwBios, "; ") + ". BIOS 는 있지만 설정 값을 Redfish 로 노출하지 않는 장비/모드입니다. System 의 BiosVersion 은 이 값에서 옵니다.)")
				}
				if len(h.biosReg) == 0 {
					add("  (Registries 에도 BIOS 속성 레지스트리가 없습니다 — Redfish 로 BIOS 설정을 노출하지 않는 구성입니다.)")
				}
			}
		}
	}

	// XML 쪽: 왜 XML 이 안 됐는가
	switch {
	case x.skipped == "TCP 연결 실패":
	case x.kind != "xml":
		st := x.nuovaStatus
		why := fmt.Sprintf("HTTP %d", st)
		if x.nuovaErr != "" {
			why = x.nuovaErr
		}
		add("● 이 주소(" + h.hostport + ")는 XML API(/nuova)를 제공하지 않습니다 (" + why + "). bios.sh 가 XML 로 시도했다가 로그인 전에 실패해 Redfish 로 넘어간 것이 \"XML 실패 → Redfish\" 입니다. 이 진단은 비밀번호를 보내지 않았고, 같은 응답이면 bios.sh 의 XML 로그인도 계정 거부(AUTH_FAIL)가 아니라 XML 이 아닌 응답으로 실패했을 것입니다 — 계정 문제가 아닙니다.")
		add("  원인 후보: 이 IP 가 UCSM(Fabric Interconnect 관리 VIP)이나 단독 CIMC 가 아니라 서버 개별 BMC 주소입니다. Cisco XML API 는 UCSM VIP 에서 제공됩니다 — UCS X-Series 블레이드가 UCSM 관리 모드라면 hosts 의 해당 이름-m 주소를 UCSM VIP 로 두고(서비스 프로파일 이름 = 호스트 이름), Intersight 관리 모드(IMM)라면 UCSM 이 없어 XML 로는 읽을 수 없습니다.")
	case x.loginTried && !x.loggedIn:
		if x.loginStatus == StatusAuthFail {
			add(fmt.Sprintf("● XML API 는 있으나 로그인이 거부되었습니다 (%s HTTP=%d errorCode=%s). bios.sh 는 AUTH_FAIL 이면 Redfish 로 넘기지 않고 중단합니다 — 이 장비에서 \"XML 실패 → Redfish\" 가 나왔다면 로그인 전 실패(연결·XML 아님)였으므로 이 경우와 다릅니다. 계정(bios.conf user)과 pass.enc 가 UCSM 계정과 같은지 확인하십시오.", x.loginStatus, x.loginHTTP, orDefault(x.loginCode, "-")))
		} else {
			add(fmt.Sprintf("● XML API 로그인 전 실패: %s HTTP=%d — %s. 이것이 bios.sh 의 \"XML 실패 → Redfish\" 원인입니다.", x.loginStatus, x.loginHTTP, x.loginDetail))
		}
	case x.loggedIn && x.serverErr != "":
		add("● XML 로그인은 되었으나 서버 목록 조회가 실패했습니다: " + x.serverErr)
	case x.loggedIn && len(x.servers) == 0:
		add("● XML 로그인은 되었으나 computeBlade·computeRackUnit 서버가 없습니다 — 이 주소가 서버를 관리하는 UCSM 이 아닙니다 (bios.sh 라면 UNSUPPORTED).")
	case x.loggedIn:
		add(fmt.Sprintf("● XML API 가 정상 동작합니다 (서버 %d대). 그런데도 bios.sh 가 \"XML 실패\" 로 Redfish 에 넘어갔다면 hosts 의 이 이름-m 주소가 지금 진단한 주소와 다른지 확인하십시오.", len(x.servers)))
		if len(x.servers) > 1 && len(x.picked) == len(x.servers) {
			add("  서비스 프로파일 이름이 호스트 이름과 같은 서버가 없어 모든 서버가 '<호스트>/<dn>' 으로 나열됩니다 — 서비스 프로파일 이름을 호스트 이름과 맞추거나 비고의 dn 으로 구분하십시오.")
		}
		var bad []string
		for _, cls := range x.classOrder {
			if !strings.HasSuffix(x.classInfo[cls], "개") {
				bad = append(bad, cls+"("+x.classInfo[cls]+")")
			}
		}
		if len(bad) > 0 {
			add("  프로파일 [xml] 클래스 중 조회 오류: " + strings.Join(bad, "; ") + " — 해당 항목은 UNSUPPORTED 가 됩니다. 클래스·속성 이름이 이 모델/펌웨어에 맞는지 확인할 대상입니다.")
		}
	}

	// 종합
	redfishBios := false
	for _, s := range h.systems {
		if s.attrs > 0 {
			redfishBios = true
		}
	}
	if authed && !redfishBios && x.kind != "xml" && x.skipped != "TCP 연결 실패" {
		add("● 종합: 이 주소로는 Redfish·XML 어느 쪽으로도 BIOS 속성을 읽을 수 없습니다 (vendor·모델·BIOS 버전은 Redfish System 정보에서 읽히지만 설정 값은 아닙니다). UCSM VIP 를 알면 그 주소로 XML 조회(bios.sh 의 -user-xml 또는 hosts 의 이름-m)를 쓰고, 그래도 안 되면 이 결과 전체를 전달해 주십시오.")
	}
	if len(out) == 0 {
		add("● 원인을 특정하지 못했습니다 — 위 [2]~[7] 의 상태 코드와 응답 내용을 전달해 주십시오.")
	}
	return out
}
