// Package csvproc 는 JS validator.js(validateCsvAllocations)의 Go 포팅이다.
// 센터 접두사 파싱, 1~48U 자동/수동 할당, 48U 초과·충돌 검증, 신규 랙 자동 생성을 수행한다.
package csvproc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"datacenter-viewer/internal/model"
)

// Row 는 헤더 순서를 보존한 CSV 한 행.
type Row struct {
	Keys []string          // 헤더 순서
	V    map[string]string // 헤더 -> 값
}

// Options 는 검증 옵션.
type Options struct {
	DefaultSiteID          string
	AllSites               []model.Site
	ExistingDevices        []model.Device
	AutoCreateMissingRacks bool
	IsSimulation           bool
}

var (
	reRackXY    = regexp.MustCompile(`(?i)^X(\d+)Y(\d+)$`)
	reBracket   = regexp.MustCompile(`^\[([^\]]+)\]\s*(.+)$`)
	rePrefix    = regexp.MustCompile(`^([^\d_:/\s-][^_:/]*?)[_:/](.+)$`)
	reModelTail = regexp.MustCompile(`^(.+?)[_:/](\d{2,4}_\d{2,4})$`)
	reAllDigits = regexp.MustCompile(`^\d+$`)
	reTrailingU = regexp.MustCompile(`(?i)u$`)
)

var (
	aliasCenter  = []string{"센터이름", "센터명", "센터", "데이터센터위치", "데이터센터", "dc_location", "site", "site_id", "center"}
	aliasRack    = []string{"랙라벨", "랙위치", "rack_label", "rack_location", "rack", "rack_id", "cabinet", "랙"}
	aliasModel   = []string{"모델코드", "모델", "model_code", "model", "device_model", "모델명"}
	aliasHost    = []string{"hostname", "호스트네임", "host", "호스트명"}
	aliasFF      = []string{"몇U", "폼팩터", "form_factor", "u_size", "height", "size", "크기"}
	aliasStartU  = []string{"수동지정U", "시작U", "랙내위치", "장착위치", "u_position", "start_u", "slot", "u위치"}
	aliasOrient  = []string{"방향", "orientation", "face", "view", "앞뒤"}
	knownHeaders = map[string]bool{
		"센터이름": true, "센터명": true, "센터": true, "데이터센터위치": true, "데이터센터": true,
		"dc_location": true, "site": true, "site_id": true, "center": true,
		"랙위치": true, "호스트네임": true, "폼팩터": true, "모델": true, "방향": true, "시뮬레이션": true,
		"모델코드": true, "랙라벨": true, "hostname": true, "몇U": true,
	}
)

// pick 은 별칭 목록 중 값이 비어있지 않은 첫 항목을 반환한다 (JS의 `a || b || c` 와 동일).
func pick(r Row, aliases []string) string {
	for _, a := range aliases {
		if v, ok := r.V[a]; ok && v != "" {
			return v
		}
	}
	return ""
}

// pickHost 는 hostname 은 빈 문자열도 "존재"로 취급한다 (JS `!== undefined`).
func pickHost(r Row) string {
	for _, a := range aliasHost {
		if v, ok := r.V[a]; ok {
			return v
		}
	}
	return ""
}

// DetermineVendor 는 호스트네임 첫 글자 규칙으로 벤더를 판별한다.
func DetermineVendor(hostname, mdl string) string {
	h := strings.TrimSpace(hostname)
	if h == "" || h == "(임시할당)" || strings.HasPrefix(h, "[임시") {
		return "TEMP"
	}
	switch strings.ToLower(h)[0] {
	case 'p':
		return "HP"
	case 'd':
		return "DELL"
	case 'c':
		return "CISCO"
	case 'h':
		return "CUSTOM"
	case 'm':
		return "SUPERMICRO"
	case 'l':
		return "LENOVO"
	}
	m := strings.ToLower(mdl)
	switch {
	case strings.Contains(m, "hp") || strings.Contains(m, "dl3"):
		return "HP"
	case strings.Contains(m, "dell") || strings.Contains(m, "r7") || strings.Contains(m, "r6"):
		return "DELL"
	case strings.Contains(m, "cisco") || strings.Contains(m, "switch"):
		return "CISCO"
	case strings.Contains(m, "supermicro"):
		return "SUPERMICRO"
	case strings.Contains(m, "lenovo"):
		return "LENOVO"
	}
	return "OTHER"
}

// DetermineDeviceType 은 server | switch 를 판별한다.
func DetermineDeviceType(mdl, hostname string) string {
	m := strings.ToLower(mdl)
	h := strings.ToLower(hostname)
	if strings.Contains(m, "switch") || strings.Contains(m, "스위치") || strings.Contains(m, "네트워크") ||
		strings.Contains(m, "tor") || strings.HasPrefix(h, "csw") || strings.Contains(h, "switch") || strings.Contains(h, "-sw") {
		return "switch"
	}
	return "server"
}

// ParseIntegerWithUnit 은 "4U", " 15 " 등에서 정수를 추출한다. 실패 시 ok=false.
func ParseIntegerWithUnit(val string) (int, bool) {
	s := strings.TrimSpace(val)
	if s == "" || strings.EqualFold(s, "AUTO") {
		return 0, false
	}
	s = strings.TrimSpace(reTrailingU.ReplaceAllString(s, ""))
	// JS parseInt: 앞쪽 숫자만 취함
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9' || (end == 0 && (s[end] == '-' || s[end] == '+'))) {
		end++
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0, false
	}
	return n, true
}

// ParseRackXY 는 X001Y002 -> (1,2).
func ParseRackXY(rackID string) (x, y int, ok bool) {
	m := reRackXY.FindStringSubmatch(strings.TrimSpace(rackID))
	if m == nil {
		return 0, 0, false
	}
	x, _ = strconv.Atoi(m[1])
	y, _ = strconv.Atoi(m[2])
	return x, y, true
}

// CenterModel 은 센터/모델 분리 결과.
type CenterModel struct {
	CenterName        string
	HasExplicitCenter bool
	ModelCode         string
}

// ParseCenterAndModel 은 센터이름 열 또는 모델코드 앞 접두사에서 센터와 모델을 분리한다.
func ParseCenterAndModel(rawCenter, rawModel, fallback string) CenterModel {
	center := strings.TrimSpace(rawCenter)
	mdl := strings.TrimSpace(rawModel)
	if center == "" && mdl != "" {
		if m := reBracket.FindStringSubmatch(mdl); m != nil {
			center, mdl = strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		} else if m := rePrefix.FindStringSubmatch(mdl); m != nil {
			center, mdl = strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		} else if m := reModelTail.FindStringSubmatch(mdl); m != nil && !reAllDigits.MatchString(m[1]) {
			center, mdl = strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		}
	}
	res := CenterModel{CenterName: center, HasExplicitCenter: center != "", ModelCode: mdl}
	if res.CenterName == "" {
		res.CenterName = fallback
		if res.CenterName == "" {
			res.CenterName = "SITE-A"
		}
	}
	if res.ModelCode == "" {
		res.ModelCode = "012_001"
	}
	return res
}

type occ struct {
	hostname     string
	startU, endU int
	fromCSV      bool
	rowNum       int
}

func overlap(a1, a2, b1, b2 int) []int {
	s, e := max(a1, b1), min(a2, b2)
	if s > e {
		return nil
	}
	out := make([]int, 0, e-s+1)
	for u := s; u <= e; u++ {
		out = append(out, u)
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Validate 는 JS validateCsvAllocations 와 동일한 규칙으로 행들을 검증/할당한다.
func Validate(rows []Row, opt Options) model.ValidationResult {
	res := model.ValidationResult{
		ValidList: []model.Device{}, InvalidList: []model.InvalidItem{},
		NewlyCreatedRacks: []model.Rack{}, TargetSites: []string{},
	}
	defSite := opt.DefaultSiteID
	if defSite == "" {
		defSite = "SITE-A"
	}
	targetSet := map[string]bool{}

	resolveSite := func(raw string) string {
		clean := strings.TrimSpace(raw)
		if clean == "" {
			return defSite
		}
		up := strings.ToUpper(clean)
		for _, s := range opt.AllSites {
			if strings.ToUpper(s.SiteID) == up || strings.ToUpper(s.Name) == up ||
				(up == "SITE A" && strings.ToUpper(s.SiteID) == "SITE-A") {
				return s.SiteID
			}
		}
		return clean
	}

	rackMap := map[string]model.Rack{}
	for _, s := range opt.AllSites {
		for _, r := range s.Racks {
			r.SiteID = s.SiteID
			rackMap[s.SiteID+"::"+strings.ToUpper(r.RackID)] = r
		}
	}

	occByRack := map[string][]occ{}
	for _, d := range opt.ExistingDevices {
		sid := d.SiteID
		if sid == "" {
			sid = defSite
		}
		k := sid + "::" + strings.ToUpper(d.RackID)
		h := d.Hostname
		if h == "" {
			h = "[임시할당]"
		}
		occByRack[k] = append(occByRack[k], occ{hostname: h, startU: d.StartU, endU: d.EndU})
	}

	fail := func(rowNum int, dc, rack, urange, host, mdl, reason string) {
		if host == "" {
			host = "[임시할당]"
		}
		res.InvalidList = append(res.InvalidList, model.InvalidItem{
			RowNum: rowNum, DcName: dc, RackID: rack, URange: urange, Hostname: host, Model: mdl, Reason: reason,
		})
	}

	for idx, row := range rows {
		rowNum := idx + 1
		rawDc := pick(row, aliasCenter)
		rawRack := pick(row, aliasRack)
		rawModel := pick(row, aliasModel)
		rawHost := pickHost(row)
		rawFF := pick(row, aliasFF)
		rawStart := pick(row, aliasStartU)
		if rawStart == "" && len(row.Keys) >= 5 {
			for i, k := range row.Keys {
				if i >= 4 && !knownHeaders[k] {
					rawStart = row.V[k]
					break
				}
			}
		}
		orient := "front"
		switch strings.ToLower(strings.TrimSpace(pick(row, aliasOrient))) {
		case "back", "rear", "뒤", "b":
			orient = "back"
		}

		cm := ParseCenterAndModel(rawDc, rawModel, defSite)
		siteID := resolveSite(cm.CenterName)
		if cm.HasExplicitCenter {
			targetSet[siteID] = true
		}
		rackID := strings.ToUpper(strings.TrimSpace(rawRack))
		host := strings.TrimSpace(rawHost)
		isTemp := host == ""
		ff, ffOK := ParseIntegerWithUnit(rawFF)
		manual, manualOK := ParseIntegerWithUnit(rawStart)
		devType := DetermineDeviceType(cm.ModelCode, host)
		vendor := DetermineVendor(host, cm.ModelCode)

		if rackID == "" || !ffOK || ff < 1 {
			rid, ffs := rackID, rawFF
			if rid == "" {
				rid = "(누락)"
			}
			show := func(s string, def string) string {
				if s == "" {
					return def
				}
				return s
			}
			fail(rowNum, siteID, rid, fmt.Sprintf("크기: %sU", show(rawFF, "?")), host, cm.ModelCode,
				fmt.Sprintf("[할당 불가: 필수 입력값 누락 또는 폼팩터(몇U) 크기 오류 (센터: '%s', 랙라벨: '%s', 몇U: '%s')]",
					siteID, show(rackID, "누락"), show(ffs, "누락")))
			continue
		}

		key := siteID + "::" + rackID
		rack, found := rackMap[key]
		if !found {
			if !opt.AutoCreateMissingRacks {
				a := rawStart
				if a == "" {
					a = "AUTO"
				}
				fail(rowNum, siteID, rackID, fmt.Sprintf("%s (%dU)", a, ff), host, cm.ModelCode,
					fmt.Sprintf("[할당 불가: 센터 '%s'에 존재하지 않는 랙 위치 ('%s')]", siteID, rackID))
				continue
			}
			rowIdx, colIdx := 1, len(rackMap)+1
			if x, y, ok := ParseRackXY(rackID); ok {
				rowIdx, colIdx = x, y
			}
			rack = model.Rack{
				SiteID: siteID, RackID: rackID, Name: rackID, RackX: rowIdx, RackY: colIdx,
				GridX: 3 + (colIdx - 1), GridZ: 3 + (rowIdx-1)*4, WidthTiles: 1, DepthTiles: 2,
				MaxU: 52, ServerMaxU: 48, RotationDeg: 0,
			}
			rackMap[key] = rack
			res.NewlyCreatedRacks = append(res.NewlyCreatedRacks, rack)
		}

		maxU := rack.MaxU
		if maxU == 0 {
			maxU = 52
		}
		serverMaxU := rack.ServerMaxU
		if serverMaxU == 0 {
			serverMaxU = max(1, maxU-4)
		}
		effMax := serverMaxU
		if devType == "switch" {
			effMax = maxU
		}

		cur := occByRack[key]
		used := map[int]bool{}
		for _, d := range cur {
			for u := d.startU; u <= d.endU; u++ {
				used[u] = true
			}
		}
		fits := func(c int) bool {
			for o := 0; o < ff; o++ {
				if used[c+o] {
					return false
				}
			}
			return true
		}

		startU, auto := manual, false
		if !manualOK || startU < 1 {
			startU = 0
			from := 1
			if devType == "switch" {
				from = serverMaxU + 1
			}
			for c := from; c <= effMax-ff+1; c++ {
				if fits(c) {
					startU, auto = c, true
					break
				}
			}
			if startU < 1 && devType == "switch" {
				for c := 1; c <= maxU-ff+1; c++ {
					if fits(c) {
						startU, auto = c, true
						break
					}
				}
			}
			if startU < 1 {
				limit := fmt.Sprintf("서버 장착 허용 범위(1U~%dU, 상단 %d~%dU는 스위치/네트워크 전용)", serverMaxU, serverMaxU+1, maxU)
				if devType == "switch" {
					limit = fmt.Sprintf("랙 최대 높이(%dU)", maxU)
				}
				fail(rowNum, siteID, rackID, fmt.Sprintf("%dU (자동할당 불가)", ff), host, cm.ModelCode,
					fmt.Sprintf("[할당 불가: 센터 '%s' 랙 '%s'의 %s 내에 %dU 연속 빈 슬롯이 없습니다 (용량 초과)]", siteID, rackID, limit, ff))
				continue
			}
		}

		endU := startU + ff - 1
		urange := fmt.Sprintf("%dU", startU)
		if ff > 1 {
			urange += fmt.Sprintf("~%dU", endU)
		}
		mode := "수동"
		if auto {
			mode = "자동"
		}
		urange += fmt.Sprintf(" (%dU, %s)", ff, mode)

		if endU > maxU {
			fail(rowNum, siteID, rackID, urange, host, cm.ModelCode,
				fmt.Sprintf("[할당 불가: 랙 최대 높이 초과 (요청 위치: %dU~%dU, 랙 최대: %dU)]", startU, endU, maxU))
			continue
		}
		if devType != "switch" && endU > serverMaxU {
			fail(rowNum, siteID, rackID, urange, host, cm.ModelCode,
				fmt.Sprintf("[할당 불가: 서버 장착 가능 범위(1U~%dU) 초과 — %dU~%dU 영역은 스위치/네트워크 전용입니다 (요청: %dU~%dU)]",
					serverMaxU, serverMaxU+1, maxU, startU, endU))
			continue
		}

		collided := false
		for _, d := range cur {
			ov := overlap(d.startU, d.endU, startU, endU)
			if len(ov) == 0 {
				continue
			}
			parts := make([]string, len(ov))
			for i, u := range ov {
				parts[i] = fmt.Sprintf("%dU", u)
			}
			ovs := strings.Join(parts, ", ")
			if d.fromCSV {
				fail(rowNum, siteID, rackID, urange, host, cm.ModelCode,
					fmt.Sprintf("[할당 불가: CSV 내 중복 할당 - CSV %d행의 '%s'(%dU~%dU) 장비와 %s 위치가 겹칩니다]", d.rowNum, d.hostname, d.startU, d.endU, ovs))
			} else {
				fail(rowNum, siteID, rackID, urange, host, cm.ModelCode,
					fmt.Sprintf("[할당 불가: U 위치 중복 충돌 - %s 랙의 %s에 이미 '%s'(%dU~%dU) 장비가 마운트되어 있습니다]", rackID, ovs, d.hostname, d.startU, d.endU))
			}
			collided = true
			break
		}
		if collided {
			continue
		}

		dev := model.Device{
			DeviceID: fmt.Sprintf("DEV-%s-%d", siteID, rowNum), SiteID: siteID, RackID: rackID,
			StartU: startU, EndU: endU, FormFactorU: ff, Hostname: host, IsTemp: isTemp,
			Vendor: vendor, Model: cm.ModelCode, DeviceType: devType, Orientation: orient,
			IsSimulation: opt.IsSimulation, RowNum: rowNum,
		}
		dispHost := host
		if dispHost == "" {
			dispHost = "[임시할당]"
		}
		occByRack[key] = append(occByRack[key], occ{hostname: dispHost, startU: startU, endU: endU, fromCSV: true, rowNum: rowNum})
		res.ValidList = append(res.ValidList, dev)
	}

	for s := range targetSet {
		res.TargetSites = append(res.TargetSites, s)
	}
	res.Summary = model.Summary{Total: len(rows), SuccessCount: len(res.ValidList), FailCount: len(res.InvalidList)}
	return res
}
