package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"biostool/internal/ucsmxml"
)

// ucsm.go 는 `biostool ucsm` 의 본체입니다: Cisco UCS Manager(XML API)가 관리하는 블레이드의
// 실효 BIOS 설정을 매핑 TSV(standard_bios_map_xml.tsv)의 기대값과 비교합니다 (읽기 전용, 설정 기능 없음).
//
// 대상(user.txt 한 줄)은 블레이드 BMC 가 아니라 UCSM(Fabric Interconnect VIP) 주소입니다. UCSM 1개마다
//
//	aaaLogin 1회 → configResolveClass(computeBlade) → 매핑 TSV 의 서로 다른 xml_class 마다 configResolveClass 1회 → aaaLogout
//
// 순서로 읽습니다. UCSM 과의 통신은 internal/ucsmxml 패키지만 하며, 그 패키지가 보낼 수 있는 요청은
// aaaLogin·configResolveClass(computeBlade·bios* 클래스, inHierarchical="false")·aaaLogout(자기 쿠키) 뿐입니다
// (configConfMo 등 설정·변경 계열은 만들 수 있는 함수가 없고, 전송 직전 관문이 한 번 더 막음).
// 결과는 Redfish 의 result.tsv 와 섞지 않고 results/<일시>/ucsm_result.tsv 에 따로 씁니다.

const (
	ucsmStatusXMLError  = ucsmxml.StatusXMLError // 응답이 XML 아님·errorCode·HTTP 오류 (호스트 단위)
	ucsmDefaultMap      = "bios_json/standard_map/standard_bios_map_xml.tsv"
	ucsmVendor          = "Cisco"
	ucsmPlatformDefault = "platform-default"
	ucsmSettingsSeg     = "/bios/bios-settings/"
	ucsmResultFile      = "ucsm_result.tsv"
	ucsmUnmappedPrefix  = "# UNMAPPED "
	ucsmAuthStopRerun   = "해당 UCSM 만 다시 실행"
	ucsmPlatformNote    = "플랫폼 기본값이라 실제 적용값은 biosPlatformDefaults 확인 필요"
)

var ucsmMapHeader = []string{"vendor", "model", "std_name", "xml_class", "xml_attribute", "value", "protocol", "xml_example", "verified"}

var ucsmResultHeader = []string{"ucsm_host", "ip", "protocol", "mgmt_version", "server_dn", "vendor", "model", "std_name", "xml_class", "attribute", "expected", "current", "result"}

var (
	// xml_class 는 BIOS 클래스(bios*)만 받는다: 매핑 TSV 로 aaaUser 같은 다른 클래스를 읽게 만들 수 없게.
	reUcsmClass = regexp.MustCompile(`^bios[A-Za-z0-9]+$`)
	reUcsmAttr  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
)

// ucsmModelKey 는 매핑 TSV 의 model(`UCS B200 M5`, `UCS X210c M7`)과 UCSM 이 보고한 PID
// (`UCSB-B200-M5`, `UCSX-210C-M7`)를 같은 키로 맞추는 한곳의 규칙입니다.
//
//	소문자 → 영숫자만 → 앞의 "ucs" 제거 → PID 의 `ucsb-b200` 에서 생기는 중복 "bb" 를 "b" 하나로.
//	"UCS B200 M5" / "UCSB-B200-M5" → "b200m5",  "UCS X210c M7" / "UCSX-210C-M7" → "x210cm7"
//
// 이 규칙으로 맞지 않는 모델은 추측하지 않고 MAPPING_MISSING 으로 둡니다.
func ucsmModelKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	k := strings.TrimPrefix(b.String(), "ucs")
	if strings.HasPrefix(k, "bb") {
		k = k[1:]
	}
	return k
}

// ---- 매핑 TSV ----

// ucsmMapRow 는 매핑 TSV 한 줄입니다.
type ucsmMapRow struct {
	Line     int
	Model    string
	Std      string
	Class    string // xml_class (biosVf*)
	Attr     string // xml_attribute (vp*)
	Value    string // 원문 (A|B 포함)
	Allowed  []string
	Verified bool
	modelKey string
}

// ucsmMap 은 로드된 매핑 TSV 입니다.
type ucsmMap struct {
	Rows     []ucsmMapRow
	Unmapped map[string]string // "모델키|std_name" → `# UNMAPPED` 줄의 사유
	Items    []string          // 점검 항목 순서: 표준 4항목, 그다음 추가 항목(등장 순)
	Classes  []string          // 조회할 서로 다른 xml_class (등장 순)
}

// rowFor 는 (모델키, 항목) 의 첫 행입니다 (없으면 nil).
func (m *ucsmMap) rowFor(modelKey, std string) *ucsmMapRow {
	if rows := m.rowsFor(modelKey, std); len(rows) > 0 {
		return rows[0]
	}
	return nil
}

// rowsFor 는 (모델키, 항목) 의 모든 행을 파일 순서대로 돌려줍니다. 한 항목에 클래스·속성이 다른 행이 여럿이면
// (예: UCSM system_profile 의 vpCPUPerformance 와 vpEnergyPerformance) 모두 각각 점검합니다.
func (m *ucsmMap) rowsFor(modelKey, std string) []*ucsmMapRow {
	if modelKey == "" {
		return nil
	}
	var out []*ucsmMapRow
	for i := range m.Rows {
		if m.Rows[i].modelKey == modelKey && m.Rows[i].Std == std {
			out = append(out, &m.Rows[i])
		}
	}
	return out
}

// loadUcsmMap 은 매핑 TSV 파일을 읽습니다.
func loadUcsmMap(path string) (*ucsmMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("UCSM 매핑 TSV %s 이(가) 없습니다 (-xml-map 으로 지정)", path)
		}
		return nil, fmt.Errorf("UCSM 매핑 TSV %s: %w", path, err)
	}
	return parseUcsmMap(string(data), path)
}

// parseUcsmMap 은 매핑 TSV 텍스트를 해석합니다. file 은 오류 메시지용 이름입니다.
//
// 형식: 탭 구분, 첫 비주석 줄은 머리글 `vendor model std_name xml_class xml_attribute value protocol xml_example verified`.
// `#` 로 시작하는 줄과 빈 줄은 무시하되, `# UNMAPPED <벤더> <모델> <std_name>: <사유>` 줄은 그 모델·항목의
// 매핑이 비어 있다는 표시로 읽습니다(→ MAPPING_MISSING 사유). vendor 는 CISCO, protocol 은 xml 만 받습니다.
// (모델, std_name, 클래스, 속성) 당 행은 1개이며(한 항목에 클래스·속성이 다른 행이 여럿이면 각각 점검),
// value 의 `A|B` 는 허용값 여러 개입니다 (대소문자까지 정확히 비교).
func parseUcsmMap(text, file string) (*ucsmMap, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	m := &ucsmMap{Unmapped: map[string]string{}}
	seen := map[string]int{} // 모델키|std → 처음 줄 번호
	headerDone := false
	for i, raw := range strings.Split(text, "\n") {
		n := i + 1
		line := strings.TrimRight(raw, "\r")
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, ucsmUnmappedPrefix) {
			key, why, err := parseUcsmUnmapped(t)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %v", file, n, err)
			}
			if _, ok := m.Unmapped[key]; !ok {
				m.Unmapped[key] = why
			}
			continue
		}
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		for len(cols) > len(ucsmMapHeader) && strings.TrimSpace(cols[len(cols)-1]) == "" {
			cols = cols[:len(cols)-1]
		}
		if len(cols) != len(ucsmMapHeader) {
			return nil, fmt.Errorf("%s:%d: 탭으로 구분된 %d개 열이어야 합니다 (현재 %d개). 공백이 아니라 탭인지 확인하십시오",
				file, n, len(ucsmMapHeader), len(cols))
		}
		for j := range cols {
			cols[j] = strings.TrimSpace(cols[j])
		}
		if !headerDone {
			for j, h := range ucsmMapHeader {
				if !strings.EqualFold(cols[j], h) {
					return nil, fmt.Errorf("%s:%d: 첫 줄(주석 제외)은 머리글 %q 이어야 합니다", file, n, strings.Join(ucsmMapHeader, "\t"))
				}
			}
			headerDone = true
			continue
		}
		for _, j := range []int{0, 1, 2, 3, 4, 5, 6, 8} { // xml_example(7) 은 참고용이라 비어도 된다
			if cols[j] == "" {
				return nil, fmt.Errorf("%s:%d: %s 이(가) 비어 있습니다", file, n, ucsmMapHeader[j])
			}
		}
		r := ucsmMapRow{Line: n, Model: cols[1], Std: cols[2], Class: cols[3], Attr: cols[4], Value: cols[5]}
		if !strings.EqualFold(cols[0], "cisco") {
			return nil, fmt.Errorf("%s:%d: vendor 는 CISCO 만 쓸 수 있습니다 (현재 %q)", file, n, cols[0])
		}
		if !strings.EqualFold(cols[6], "xml") {
			return nil, fmt.Errorf("%s:%d: protocol 은 xml 이어야 합니다 (현재 %q, Redfish 행은 profiles/*.tsv 에 둡니다)", file, n, cols[6])
		}
		if !reStdName.MatchString(r.Std) {
			return nil, fmt.Errorf("%s:%d: std_name %q 은(는) 소문자·숫자·밑줄만 쓸 수 있습니다", file, n, r.Std)
		}
		if !reUcsmClass.MatchString(r.Class) {
			return nil, fmt.Errorf("%s:%d: xml_class %q 은(는) bios 로 시작하는 영숫자만 쓸 수 있습니다 (읽기 허용목록)", file, n, r.Class)
		}
		if !reUcsmAttr.MatchString(r.Attr) {
			return nil, fmt.Errorf("%s:%d: xml_attribute %q 은(는) 영숫자만 쓸 수 있습니다", file, n, r.Attr)
		}
		for _, v := range strings.Split(r.Value, "|") {
			if v = strings.TrimSpace(v); v == "" {
				return nil, fmt.Errorf("%s:%d: value %q 에 빈 허용값이 있습니다 (A|B 형식)", file, n, r.Value)
			}
			r.Allowed = append(r.Allowed, v)
		}
		switch strings.ToUpper(cols[8]) {
		case "Y":
			r.Verified = true
		case "N":
		default:
			return nil, fmt.Errorf("%s:%d: verified 는 Y 또는 N 이어야 합니다 (현재 %q)", file, n, cols[8])
		}
		if r.modelKey = ucsmModelKey(r.Model); r.modelKey == "" {
			return nil, fmt.Errorf("%s:%d: model %q 에서 비교할 글자가 남지 않습니다", file, n, r.Model)
		}
		key := r.modelKey + "|" + r.Std + "|" + r.Class + "|" + r.Attr
		if first, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s:%d: %s %s %s %s 행이 %d 줄과 중복입니다 (모델·항목·클래스·속성당 1행)", file, n, r.Model, r.Std, r.Class, r.Attr, first)
		}
		seen[key] = n
		m.Rows = append(m.Rows, r)
	}
	if !headerDone {
		return nil, fmt.Errorf("%s: 머리글(%s)이 없습니다", file, strings.Join(ucsmMapHeader, " "))
	}
	if len(m.Rows) == 0 {
		return nil, fmt.Errorf("%s: 항목 행이 하나도 없습니다", file)
	}
	have := map[string]bool{}
	for _, kw := range stdKeywords {
		m.Items = append(m.Items, kw.Std)
		have[kw.Std] = true
	}
	cls := map[string]bool{}
	for _, r := range m.Rows {
		if !have[r.Std] {
			have[r.Std] = true
			m.Items = append(m.Items, r.Std)
		}
		if !cls[r.Class] {
			cls[r.Class] = true
			m.Classes = append(m.Classes, r.Class)
		}
	}
	return m, nil
}

// parseUcsmUnmapped 는 `# UNMAPPED CISCO UCS B200 M5 llc_prefetch: none_of_candidates` 를
// ("b200m5|llc_prefetch", "none_of_candidates") 로 읽습니다.
func parseUcsmUnmapped(t string) (key, why string, err error) {
	bad := errors.New("# UNMAPPED 줄은 `# UNMAPPED <벤더> <모델> <std_name>: <사유>` 형식이어야 합니다")
	body := strings.TrimPrefix(t, ucsmUnmappedPrefix)
	i := strings.Index(body, ":")
	if i < 0 {
		return "", "", bad
	}
	f := strings.Fields(body[:i])
	if len(f) < 3 || !reStdName.MatchString(f[len(f)-1]) {
		return "", "", bad
	}
	mk := ucsmModelKey(strings.Join(f[1:len(f)-1], " "))
	if mk == "" {
		return "", "", bad
	}
	why = strings.TrimSpace(body[i+1:])
	if why == "" {
		why = "UNMAPPED"
	}
	return mk + "|" + f[len(f)-1], why, nil
}

// ---- 점검 ----

// ucsmItem 은 서버 1대의 항목 1개 판정입니다.
type ucsmItem struct {
	Std      string
	Class    string // 매핑 행이 없으면 "-"
	Attr     string
	Expected string
	Current  string // 응답에 없으면 "-"
	Result   string
	Reason   string // 사람이 읽는 사유 (터미널 요약용, TSV 에는 없음)
}

// ucsmServer 는 UCSM 이 관리하는 서버(computeBlade) 1대입니다. 식별정보(serial 등)는 담지 않습니다.
type ucsmServer struct {
	DN    string // sys/chassis-N/blade-M
	Model string // UCSM 이 보고한 PID (예: UCSB-B200-M5)
	Items []ucsmItem
}

// ucsmHost 는 UCSM 1개(user.txt 한 줄)의 점검 결과입니다. Status 가 비어 있으면 점검이 끝난 것이고
// (Servers 참고), 아니면 호스트 단위 오류 상태입니다.
type ucsmHost struct {
	Target   Target
	Status   string
	Detail   string // 오류 요약 (비밀번호·쿠키 없음)
	Servers  []*ucsmServer
	Notes    []string
	loggedIn bool
}

func (h *ucsmHost) setErr(err error) {
	var xe *ucsmxml.Error
	switch {
	case errors.As(err, &xe):
		h.Status, h.Detail = xe.Status, xe.Detail
	default: // 허용목록 거부·BaseURL 오류 등 (메시지에 비밀번호·쿠키가 들어가지 않음)
		h.Status, h.Detail = ucsmStatusXMLError, sanitizeDetail(err.Error())
	}
}

// outcome 은 AUTH_FAIL 차단기(sweep.go)용 결과입니다.
func (h *ucsmHost) outcome() hostOutcome {
	switch {
	case h.Status == StatusAuthFail:
		return outAuthFail
	case h.Target.Err != "":
		return outNone
	case h.loggedIn:
		return outLoggedIn
	}
	return outOther
}

// ucsmRun 은 ucsm 점검 1회의 전체 결과입니다.
type ucsmRun struct {
	Now   time.Time
	Dir   string      // results/<일시>
	Hosts []*ucsmHost // 입력 순서
}

type ucsmOpts struct {
	Map       *ucsmMap
	ResultDir string
	Now       time.Time // 결과 폴더 이름·리포트 시각 (0 이면 지금)
}

// doUcsm 은 대상 UCSM 전체를 점검하고 ucsm_result.tsv 를 씁니다. 파일 쓰기에 실패해도 점검 결과(run)는 돌려줍니다.
// 같은 계정으로 여러 UCSM 에 로그인하므로 check 와 같은 AUTH_FAIL 차단기(auth_fail_stop)를 씁니다.
func doUcsm(rc *runContext, o ucsmOpts) (*ucsmRun, error) {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	dir, err := newRunDir(o.ResultDir, now)
	if err != nil {
		return nil, fmt.Errorf("결과 폴더: %w", err)
	}
	run := &ucsmRun{Now: now, Dir: dir, Hosts: make([]*ucsmHost, len(rc.Targets))}
	sweepHosts(len(rc.Targets), rc.Conf.Concurrency, rc.Conf.AuthFailStop,
		func(i int) hostOutcome {
			h := ucsmCheckHost(rc.Conf, rc.Password, o.Map, rc.Targets[i])
			run.Hosts[i] = h
			return h.outcome()
		},
		func(i int) {
			h := &ucsmHost{Target: rc.Targets[i], Status: StatusSkippedAuthStop}
			if h.Target.Err != "" {
				h.Status = h.Target.Err // 이름 해석 실패는 원래 상태를 유지한다
			}
			run.Hosts[i] = h
		})
	return run, writeUcsmResult(run)
}

// ucsmCheckHost 는 UCSM 1개를 점검합니다 (로그인 1회, 자동 재시도 없음, 로그아웃은 항상 시도).
func ucsmCheckHost(conf *Config, password string, m *ucsmMap, t Target) *ucsmHost {
	h := &ucsmHost{Target: t}
	if t.Err != "" {
		h.Status = t.Err
		return h
	}
	c := ucsmxml.New(ucsmxml.Options{
		BaseURL: t.BaseURL(), User: conf.User, Pass: password, Insecure: conf.Insecure,
		Timeout: conf.Timeout, Classes: m.Classes,
	})
	// 로그아웃은 항상 시도한다 (로그인에 실패해 쿠키가 없으면 아무것도 보내지 않는다).
	defer func() {
		if err := c.Logout(); err != nil {
			h.Notes = append(h.Notes, "aaaLogout 실패: "+ucsmErrBrief(err))
		}
	}()
	if err := c.Login(); err != nil {
		h.setErr(err)
		return h
	}
	h.loggedIn = true

	blades, err := c.ResolveClass(ucsmxml.BladeClass)
	if err != nil {
		h.setErr(err)
		return h
	}
	byDN := map[string]*ucsmServer{}
	for _, b := range blades {
		dn := b.Attrs["dn"]
		if dn == "" || byDN[dn] != nil {
			continue
		}
		s := &ucsmServer{DN: dn, Model: b.Attrs["model"]}
		byDN[dn] = s
		h.Servers = append(h.Servers, s)
	}
	if len(h.Servers) == 0 {
		h.Status, h.Detail = StatusUnsupported, "UCSM 에 computeBlade 가 없습니다"
		return h
	}

	// 서버 dn → xml_class → 속성. 클래스마다 configResolveClass 1회 (도메인 전체 인스턴스가 한 번에 온다).
	values := map[string]map[string]map[string]string{}
	classErr := map[string]string{} // UCSM 이 errorCode 를 돌려준 클래스 → 사유
	for _, cls := range m.Classes {
		objs, err := c.ResolveClass(cls)
		if err != nil {
			var xe *ucsmxml.Error
			if errors.As(err, &xe) && xe.Code != "" {
				classErr[cls] = xe.Detail
				continue
			}
			h.setErr(err) // 전송 실패·XML 아님 등은 호스트 단위 오류 (부분 결과로 판정하지 않음)
			h.Servers = nil
			return h
		}
		for _, ob := range objs {
			sdn := ucsmServerOf(ob.Attrs["dn"])
			if byDN[sdn] == nil {
				continue
			}
			if values[sdn] == nil {
				values[sdn] = map[string]map[string]string{}
			}
			if _, ok := values[sdn][cls]; !ok {
				values[sdn][cls] = ob.Attrs
			}
		}
	}
	for _, s := range h.Servers {
		s.Items = ucsmJudge(m, s, values[s.DN], classErr)
	}
	return h
}

// ucsmServerOf 는 biosVf* 인스턴스 dn 에서 서버 dn 을 꺼냅니다
// (`sys/chassis-1/blade-1/bios/bios-settings/CPU-Performance` → `sys/chassis-1/blade-1`).
// 서버에 실제로 적용된 BIOS 설정(서버 dn 아래 bios/bios-settings)이 아니면 빈 문자열입니다.
//
// 같은 클래스의 BIOS 정책 인스턴스(org-root/.../bios-prof-*)는 "적용하려는 값" 이라 서비스 프로파일 연결·
// 재부팅 전에는 서버의 실효값과 다를 수 있고, 어느 서버에 연결됐는지도 lsServer 를 따로 읽어야 알 수 있어
// 이번 범위(실효값 점검)에서는 무시합니다. 플랫폼 기본값(capability catalogue) 인스턴스도 서버 값이 아니므로 무시합니다.
func ucsmServerOf(dn string) string {
	i := strings.Index(dn, ucsmSettingsSeg)
	if i <= 0 || !strings.HasPrefix(dn, "sys/") {
		return ""
	}
	return dn[:i]
}

// ucsmJudge 는 서버 1대의 항목별 판정입니다. vals 는 그 서버의 xml_class → 속성입니다.
//
//   - 매핑 행이 없거나 `# UNMAPPED` → MAPPING_MISSING (모델 규칙이 맞지 않는 PID 도 여기, 추측하지 않음)
//   - 클래스 조회 오류(errorCode), 이 서버의 인스턴스 없음, 인스턴스에 속성 없음 → UNSUPPORTED (현재값 "-")
//   - 현재값이 허용값 중 하나와 정확히 같음(대소문자 포함) → OK
//   - 불일치: verified≠Y → UNVERIFIED, verified=Y → FAIL. 현재값이 platform-default 면 사유에 확인 안내를 붙인다
//     (플랫폼 기본값이 기대값과 같은지는 여기서 알 수 없으므로 OK 로 추측하지 않음).
func ucsmJudge(m *ucsmMap, s *ucsmServer, vals map[string]map[string]string, classErr map[string]string) []ucsmItem {
	mk := ucsmModelKey(s.Model)
	out := make([]ucsmItem, 0, len(m.Items))
	for _, std := range m.Items {
		rows := m.rowsFor(mk, std)
		if len(rows) == 0 {
			it := ucsmItem{Std: std, Class: "-", Attr: "-", Expected: "-", Current: "-"}
			it.Result = StatusMappingMissing
			if why, ok := m.Unmapped[mk+"|"+std]; ok && mk != "" {
				it.Reason = "매핑 TSV 에서 UNMAPPED (" + why + ")"
			} else {
				it.Reason = "매핑 TSV 에 " + orDefault(s.Model, "-") + " 의 " + std + " 행이 없음"
			}
			out = append(out, it)
			continue
		}
		for _, row := range rows {
			it := ucsmItem{Std: std, Class: "-", Attr: "-", Expected: "-", Current: "-"}
			it.Class, it.Attr, it.Expected = row.Class, row.Attr, row.Value
			attrs, hasInst := vals[row.Class]
			cur, hasAttr := attrs[row.Attr]
			why, classBad := classErr[row.Class]
			switch {
			case classBad:
				it.Result = StatusUnsupported
				it.Reason = "UCSM 이 클래스 조회에 오류를 돌려줌 (" + why + ")"
			case !hasInst:
				it.Result = StatusUnsupported
				it.Reason = "이 서버의 bios-settings 아래에 " + row.Class + " 가 없음"
			case !hasAttr:
				it.Result = StatusUnsupported
				it.Reason = row.Class + " 에 속성 " + row.Attr + " 이(가) 없음"
			default:
				it.Current = cur
				switch {
				case ucsmMatch(row.Allowed, cur):
					it.Result = StatusOK
				case !row.Verified:
					it.Result = StatusUnverified
					it.Reason = "매핑 행이 verified=N"
				default:
					it.Result = StatusFail
				}
				if cur == ucsmPlatformDefault && it.Result != StatusOK {
					if it.Reason != "" {
						it.Reason += "; "
					}
					it.Reason += ucsmPlatformNote
				}
			}
			out = append(out, it)
		}
	}
	return out
}

// ucsmMatch 는 v 가 허용값 중 하나와 정확히(대소문자 포함) 같은지 봅니다.
func ucsmMatch(allowed []string, v string) bool {
	for _, a := range allowed {
		if a == v {
			return true
		}
	}
	return false
}

// ucsmErrBrief 는 오류를 "상태(HTTP 코드)" 한 토막으로 줄입니다.
func ucsmErrBrief(err error) string {
	var xe *ucsmxml.Error
	if errors.As(err, &xe) {
		if xe.HTTP != 0 {
			return fmt.Sprintf("%s(HTTP %d)", xe.Status, xe.HTTP)
		}
		return xe.Status
	}
	if errors.Is(err, ucsmxml.ErrNotAllowed) {
		return "NOT_ALLOWED"
	}
	return ucsmStatusXMLError
}

// ---- 결과 파일 ----

// rows 는 UCSM 1개의 ucsm_result.tsv 행입니다: 서버·항목마다 1행, 호스트 단위 오류는 server_dn·std_name "-" 1행.
func (r *ucsmRun) rows(h *ucsmHost) [][]string {
	base := []string{tsvField(h.Target.Hostname), tsvField(h.Target.IP), "xml", "UCSM"}
	row := func(tail ...string) []string {
		out := append([]string(nil), base...)
		for _, s := range tail {
			out = append(out, tsvField(s))
		}
		return out
	}
	if h.Status != "" {
		return [][]string{row("-", "-", "-", "-", "-", "-", "-", "-", h.Status)}
	}
	var out [][]string
	for _, s := range h.Servers {
		for _, it := range s.Items {
			out = append(out, row(s.DN, ucsmVendor, s.Model, it.Std, it.Class, it.Attr, it.Expected, it.Current, it.Result))
		}
	}
	return out
}

// writeUcsmResult 는 results/<일시>/ucsm_result.tsv 를 씁니다 (0600, check.go 의 writeTSV).
func writeUcsmResult(r *ucsmRun) error {
	var all [][]string
	for _, h := range r.Hosts {
		all = append(all, r.rows(h)...)
	}
	return writeTSV(filepath.Join(r.Dir, ucsmResultFile), ucsmResultHeader, all)
}

// ---- 터미널 요약 ----

type ucsmReportOpts struct {
	ListMax int  // FAIL·UNVERIFIED 서버, MAPPING_MISSING·UNSUPPORTED 묶음을 나열할 최대 수
	Color   bool // ANSI 색
}

// ucsmItemOrder 는 항목 판정 집계의 표시 순서입니다.
var ucsmItemOrder = []string{StatusOK, StatusFail, StatusUnverified, StatusMappingMissing, StatusUnsupported}

// writeUcsmReport 는 대상 수·서버 수·판정별 건수·FAIL/UNVERIFIED 서버 목록·호스트 오류를 출력합니다.
// 출력에는 UCSM 주소·서버 dn·모델·속성·값만 나오며 비밀번호·쿠키는 없습니다.
func writeUcsmReport(w io.Writer, run *ucsmRun, o ucsmReportOpts) {
	p := painter{o.Color}
	items := map[string]int{}
	hostErr := map[string]int{}
	servers, done := 0, 0
	for _, h := range run.Hosts {
		if h.Status != "" {
			hostErr[h.Status]++
			continue
		}
		done++
		servers += len(h.Servers)
		for _, s := range h.Servers {
			for _, it := range s.Items {
				items[it.Result]++
			}
		}
	}
	fmt.Fprintf(w, "== UCSM BIOS 점검 결과 (XML API, 읽기 전용 / %s) ==\n", run.Now.Format("2006-01-02 15:04"))
	fmt.Fprintf(w, "대상 UCSM %s개 (점검 완료 %s개, 오류 %s개), 서버(블레이드) %s대\n",
		commaInt(len(run.Hosts)), commaInt(done), commaInt(len(run.Hosts)-done), commaInt(servers))
	fmt.Fprintln(w, "항목 판정:")
	for _, st := range ucsmItemOrder {
		n := items[st]
		line := fmt.Sprintf("  %-16s %7s", st, commaInt(n))
		switch {
		case st == StatusOK && n > 0:
			line = p.green(line)
		case st == StatusFail && n > 0:
			line = p.red(line)
		case n > 0 && st != StatusOK:
			line = p.yellow(line)
		}
		fmt.Fprintln(w, line)
	}
	if warn := authStopWarning(hostErr[StatusAuthFail], hostErr[StatusSkippedAuthStop], ucsmAuthStopRerun); warn != "" {
		fmt.Fprintln(w, p.boldRed("!! "+warn))
	}
	resultPath := filepath.ToSlash(filepath.Join(run.Dir, ucsmResultFile))
	ucsmWriteServers(w, run, StatusFail, p.red("[FAIL]"), o, resultPath)
	ucsmWriteServers(w, run, StatusUnverified, p.yellow("[UNVERIFIED]"), o, resultPath)
	ucsmWriteGroups(w, run, StatusMappingMissing, p.yellow("[MAPPING_MISSING]"), o, resultPath)
	ucsmWriteGroups(w, run, StatusUnsupported, p.yellow("[UNSUPPORTED]"), o, resultPath)
	ucsmWriteHostErrors(w, run, hostErr, p)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s 결과 파일: %s\n", p.cyan("*"), resultPath)
}

// ucsmWriteServers 는 st 항목이 있는 서버를 최대 ListMax 대까지, 그 항목과 함께 보여 줍니다.
func ucsmWriteServers(w io.Writer, run *ucsmRun, st, title string, o ucsmReportOpts, resultPath string) {
	type entry struct {
		host string
		s    *ucsmServer
	}
	var list []entry
	n := 0
	for _, h := range run.Hosts {
		if h.Status != "" {
			continue
		}
		for _, s := range h.Servers {
			k := 0
			for _, it := range s.Items {
				if it.Result == st {
					k++
				}
			}
			if k > 0 {
				list = append(list, entry{h.Target.Hostname, s})
				n += k
			}
		}
	}
	if len(list) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s 서버 %s대 (항목 %s건)\n", title, commaInt(len(list)), commaInt(n))
	for i, e := range list {
		if i == o.ListMax {
			fmt.Fprintf(w, "  ... 외 %s대 → %s\n", commaInt(len(list)-i), resultPath)
			break
		}
		fmt.Fprintf(w, "  %s  %s  %s\n", e.host, e.s.DN, orDefault(e.s.Model, "-"))
		for _, it := range e.s.Items {
			if it.Result != st {
				continue
			}
			line := fmt.Sprintf("    %-16s %s.%s  기대=%s  현재=%s", it.Std, it.Class, it.Attr, it.Expected, it.Current)
			if it.Reason != "" {
				line += "  (" + it.Reason + ")"
			}
			fmt.Fprintln(w, line)
		}
	}
}

// ucsmWriteGroups 는 st 항목을 (모델, 항목, 사유) 로 묶어 서버 수와 함께 최대 ListMax 줄 보여 줍니다.
func ucsmWriteGroups(w io.Writer, run *ucsmRun, st, title string, o ucsmReportOpts, resultPath string) {
	counts := map[string]int{}
	var keys []string
	total := 0
	for _, h := range run.Hosts {
		if h.Status != "" {
			continue
		}
		for _, s := range h.Servers {
			for _, it := range s.Items {
				if it.Result != st {
					continue
				}
				k := orDefault(s.Model, "-") + "  " + it.Std
				if it.Class != "-" {
					k += "  " + it.Class + "." + it.Attr
				}
				if it.Reason != "" {
					k += "  (" + it.Reason + ")"
				}
				if counts[k] == 0 {
					keys = append(keys, k)
				}
				counts[k]++
				total++
			}
		}
	}
	if total == 0 {
		return
	}
	sort.Strings(keys)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s 항목 %s건 (모델·항목별)\n", title, commaInt(total))
	for i, k := range keys {
		if i == o.ListMax {
			fmt.Fprintf(w, "  ... 외 %d종 → %s\n", len(keys)-i, resultPath)
			break
		}
		fmt.Fprintf(w, "  %s — 서버 %s대\n", k, commaInt(counts[k]))
	}
}

// ucsmWriteHostErrors 는 UCSM 단위 오류(AUTH_FAIL·UNREACHABLE·TIMEOUT·XML_ERROR 등)와 참고 사항을 보여 줍니다.
func ucsmWriteHostErrors(w io.Writer, run *ucsmRun, hostErr map[string]int, p painter) {
	sts := otherStatuses(hostErr, map[string]bool{})
	if len(sts) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, p.yellow("[UCSM 오류]"))
	}
	for _, st := range sts {
		var names []string
		detail := ""
		for _, h := range run.Hosts {
			if h.Status == st {
				names = append(names, h.Target.Hostname)
				if detail == "" && h.Detail != "" {
					detail = h.Detail
				}
			}
		}
		more := ""
		if len(names) > 10 {
			more = fmt.Sprintf(" ... 외 %d개", len(names)-10)
			names = names[:10]
		}
		fmt.Fprintf(w, "  %-17s %s개  %s%s\n", st, commaInt(hostErr[st]), strings.Join(names, " "), more)
		switch st {
		case StatusAuthFail:
			fmt.Fprintln(w, "    계정 잠금을 막으려고 재시도하지 않았습니다. 계정/비밀번호를 확인한 뒤 해당 UCSM 만 다시 실행하십시오.")
		case StatusSkippedAuthStop:
			fmt.Fprintln(w, "    AUTH_FAIL 차단기(auth_fail_stop)가 작동해 접속하지 않았습니다.")
		case StatusNoHostsEntry:
			fmt.Fprintln(w, "    /etc/hosts 에 이름-m 항목이 없습니다 (UCSM 은 IP 로 적거나 -hosts 로 다른 파일 지정).")
		}
		if detail != "" && st != StatusSkippedAuthStop {
			fmt.Fprintf(w, "    예: %s\n", detail)
		}
	}
	first := true
	for _, h := range run.Hosts {
		for _, n := range h.Notes {
			if first {
				fmt.Fprintln(w)
				fmt.Fprintln(w, p.cyan("[참고]"))
				first = false
			}
			fmt.Fprintf(w, "  %s  %s\n", h.Target.Hostname, n)
		}
	}
}

// ---- 서브커맨드 ----

// cmdUcsm 은 `biostool ucsm` 입니다. 공통 옵션(-conf -user -hosts)과 -xml-map, -list-max 를 받습니다.
func cmdUcsm(args []string) error {
	fs, o := newFlagSet("ucsm")
	xmlMap := fs.String("xml-map", ucsmDefaultMap, "UCSM XML 매핑 TSV (vendor model std_name xml_class xml_attribute value protocol xml_example verified)")
	listMax := fs.Int("list-max", 20, "FAIL·UNVERIFIED 서버, MAPPING_MISSING·UNSUPPORTED 묶음을 나열할 최대 수")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if o.profile != "" || o.dryRun || o.fromDump != "" || o.retryFrom != "" {
		return fmt.Errorf("ucsm 은 읽기 전용 UCSM XML 점검이라 -profile, -dry-run, -from-dump, -retry-from 을 쓰지 않습니다 (기대값은 -xml-map)")
	}
	if *listMax < 0 {
		return fmt.Errorf("-list-max 는 0 이상이어야 합니다")
	}
	m, err := loadUcsmMap(*xmlMap)
	if err != nil {
		return err
	}
	rc, err := prepare(o, "", "")
	if err != nil {
		return err
	}
	printSummary(rc, o, "")
	fmt.Printf("UCSM XML 매핑: %s (행 %d개, 조회 클래스 %d개: %s)\n", *xmlMap, len(m.Rows), len(m.Classes), strings.Join(m.Classes, ", "))
	fmt.Println("대상은 UCSM(Fabric Interconnect VIP) 주소입니다. 호출: aaaLogin → configResolveClass(읽기) → aaaLogout")

	run, err := doUcsm(rc, ucsmOpts{Map: m, ResultDir: rc.Conf.ResultDir})
	if run == nil {
		return err
	}
	fmt.Println()
	writeUcsmReport(os.Stdout, run, ucsmReportOpts{ListMax: *listMax, Color: useColor()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "경고: 결과 파일 기록 중 오류: "+err.Error())
	}
	return err
}
