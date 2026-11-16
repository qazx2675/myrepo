package main

// profile_add.go — 프로파일(profiles/<이름>.tsv)에 태그·모델별 설정 행을 더하는 핵심 로직 (수정계획서_대표서버_기준맞추기.md 3.D).
// 화면(tui_profadd.go)과 명령(profile_add_cmd.go, `biostool profile add`)이 같이 씁니다. BMC 에는 접속하지 않습니다 (이미 읽은 스냅샷만 봄).
//
//   - 범위: 모델 1종(벤더+모델) 또는 태그(그 태그 서버들이 보고한 모델 하나하나). 모델마다 같은 설정으로 행을 하나씩 만든다.
//   - 행: vendor·model 은 BMC 가 보고한 값(앞뒤 공백 정리), attribute 는 그 모델 서버 스냅샷의 BMC 표기(대소문자 무시로 찾음),
//     std_name 은 준 값 → stdKeywords 에 걸리는 표준 항목 → 속성 이름의 소문자·밑줄 변환 순서, value 는 입력 그대로 (A|B 허용),
//     verified 는 항상 N (Y = 쓰기 허가는 사람이 파일을 직접 고쳐 승격한다).
//   - 보호 속성 이름은 행으로 만들지 않는다. 그 모델에 없는 속성이면 그 모델의 행만 건너뛴다.
//   - 충돌: 같은 (벤더, 모델 키, std_name, 속성) 행이 이미 있으면 값이 같을 때 "이미 있음", 다르면 "충돌" (덮어쓸지는 부르는 쪽이 정함).
//   - 쓰기: `# [redfish]` 구역 끝에 행을 넣고 (`# [xml]` 줄 앞, 없으면 파일 끝) 주석·기존 행·구역은 그대로, parseProfile 로 검증한 뒤
//     <이름>.tsv.bak.<일시> 로 백업하고 임시 파일 + rename 으로 바꾼다 (writeFileAtomic, 파일 권한 유지).

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

// profAddNoteWild 는 모델 지정 행이 같은 std_name 의 벤더 공통(*) 행을 가리게 될 때 미리보기에 붙이는 알림입니다 (rowsFor 동작).
const profAddNoteWild = "※ 벤더 공통(*) 행 대신 이 행이 쓰입니다"

// profAddSet 은 사용자가 입력한 설정 1개입니다. Std 가 비면 속성 이름으로 정합니다.
type profAddSet struct {
	Std   string
	Attr  string
	Value string
}

// profAddModel 은 행을 만들 모델 1종입니다.
type profAddModel struct {
	Vendor string // BMC 가 보고한 값 (공백 정리)
	Model  string
	Idx    []int    // 범위(태그·그룹) 안에서 이 모델인 서버의 스냅샷(= 점검 결과) 번호
	Hosts  []string // 그 서버 이름 (입력 순서)
	All    []int    // 범위와 상관없이 읽은 서버 중 이 모델인 전부 — 행이 모델 기준이라 재점검은 이 서버들이 대상 (비면 Idx)

	attrs []map[string]string // 이 모델 서버들의 Bios 속성 (속성 이름 확인용). 비면 접속하지 않아 입력한 이름을 그대로 씀
}

func (m *profAddModel) label() string { return strings.TrimSpace(m.Vendor + " " + m.Model) }

func (m *profAddModel) has(si int) bool {
	for _, i := range m.Idx {
		if i == si {
			return true
		}
	}
	return false
}

// profAddClean 은 표의 한 칸에 넣을 글자로 정리합니다 (탭·줄바꿈 등 공백류를 한 칸으로).
func profAddClean(s string) string { return strings.Join(strings.Fields(s), " ") }

// profAddKey 는 (벤더, 모델) 비교 키입니다 (로더와 같은 vendorKey + normalizeModel).
func profAddKey(vendor, model string) string { return vendorKey(vendor) + "|" + normalizeModel(model) }

// profAddModelsOf 는 keep 이 참인 스냅샷을 (벤더, 모델) 별로 묶습니다 (처음 나온 순서). 읽기에 실패했거나 모델을 모르는 스냅샷은
// 빠지고 unreadable 로 셉니다. matched 는 keep 이 참이었던 스냅샷 수입니다.
func profAddModelsOf(snaps []*biosSnap, keep func(s *biosSnap) bool) (models []*profAddModel, matched, unreadable int) {
	byKey := map[string]*profAddModel{}
	all := map[string][]int{}
	for i, s := range snaps {
		if s == nil || !s.ok() {
			continue
		}
		if vendor, model := profAddClean(s.Vendor), profAddClean(s.Model); vendor != "" && model != "" {
			k := profAddKey(vendor, model)
			all[k] = append(all[k], i)
		}
	}
	defer func() {
		for k, m := range byKey {
			m.All = all[k]
		}
	}()
	for i, s := range snaps {
		if s == nil || !keep(s) {
			continue
		}
		matched++
		vendor, model := profAddClean(s.Vendor), profAddClean(s.Model)
		if !s.ok() || vendor == "" || model == "" {
			unreadable++
			continue
		}
		k := profAddKey(vendor, model)
		m := byKey[k]
		if m == nil {
			m = &profAddModel{Vendor: vendor, Model: model}
			byKey[k] = m
			models = append(models, m)
		}
		m.Idx = append(m.Idx, i)
		m.Hosts = append(m.Hosts, s.Target.Hostname)
		m.attrs = append(m.attrs, s.Attrs)
	}
	return models, matched, unreadable
}

// profAddModelsForTag 는 그 태그를 가진 서버들의 모델입니다.
func profAddModelsForTag(snaps []*biosSnap, tag string) (models []*profAddModel, matched, unreadable int) {
	return profAddModelsOf(snaps, func(s *biosSnap) bool { return s.Target.Tag == tag })
}

// profAddModelsForHosts 는 이름이 hosts 에 있는 서버들의 모델입니다 (지금 보는 그룹).
func profAddModelsForHosts(snaps []*biosSnap, hosts []string) (models []*profAddModel, matched, unreadable int) {
	in := map[string]bool{}
	for _, h := range hosts {
		in[h] = true
	}
	return profAddModelsOf(snaps, func(s *biosSnap) bool { return in[s.Target.Hostname] })
}

// profAddModelFromSpec 은 모델 1종을 사람이 쓴 이름으로 정합니다. 읽은 스냅샷에 같은 모델(키 일치) 서버가 있으면 그 서버들의
// 표기·속성을 쓰고, 없으면(접속하지 않음) 입력한 이름 그대로이며 속성 이름은 확인하지 못합니다 (snaps 가 nil 이어도 됨).
func profAddModelFromSpec(vendor, model string, snaps []*biosSnap) *profAddModel {
	vendor, model = profAddClean(vendor), profAddClean(model)
	want := profAddKey(vendor, model)
	ms, _, _ := profAddModelsOf(snaps, func(s *biosSnap) bool { return profAddKey(s.Vendor, s.Model) == want })
	if len(ms) > 0 {
		return ms[0]
	}
	return &profAddModel{Vendor: vendor, Model: model}
}

// profAddRow 는 미리보기·쓰기에 쓰는 행 1개입니다 (verified 는 항상 N 이라 칸이 없음).
type profAddRow struct {
	Vendor, Model, Std, Attr, Value string
	Idx                             []int
	Hosts                           []string
	WildNote                        bool // 같은 std_name 의 * 행을 이 모델에서 가리게 됨
	Unchecked                       bool // 속성 이름을 스냅샷으로 확인하지 못함 (접속하지 않음)

	// 충돌일 때만: 기존 행
	Old         string
	OldVerified bool
	OldLine     int    // 프로파일 파일의 줄 번호 (1부터)
	WantStd     string // 사용자가 다른 std_name 을 적어 충돌이 된 경우 그 이름 (Std 는 기존 행의 std_name)
}

func (r profAddRow) text() string {
	return fmt.Sprintf("%s %s  %s  %s = %s", r.Vendor, r.Model, r.Std, r.Attr, r.Value)
}

// profAddSkip 은 만들지 않은 행과 사유입니다.
type profAddSkip struct {
	Vendor, Model, Attr, Reason string
}

// profAddPlan 은 추가 계획입니다 (파일은 아직 안 건드림).
type profAddPlan struct {
	Profile   string
	Add       []profAddRow  // 새로 넣을 행
	Same      []profAddRow  // 같은 값으로 이미 있음 (건너뜀)
	Conflicts []profAddRow  // 같은 행이 다른 값으로 있음 (덮어쓸 때만 바꿈)
	Skipped   []profAddSkip // 만들 수 없어 건너뜀 (그 모델에 없는 속성 등)
	Rejected  []string      // 입력 자체를 받지 않음 (보호 속성·형식 오류)
}

// profAddValueErr 는 value 가 프로파일 칸에 쓸 수 없는 이유입니다 (쓸 수 있으면 "").
func profAddValueErr(v string) string {
	if v == "" {
		return "값이 비어 있음"
	}
	if strings.IndexFunc(v, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		return "값에 제어문자(탭 포함)가 들어 있음"
	}
	for _, seg := range strings.Split(v, "|") {
		if strings.TrimSpace(seg) == "" {
			return "A|B 형식에 빈 허용값이 있음"
		}
	}
	return ""
}

// lowerSnake 는 속성 이름을 소문자·밑줄 이름으로 바꿉니다 (ProcTurbo → proc_turbo). 비면 "attr".
func lowerSnake(s string) string {
	var b []rune
	prevLower := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			if prevLower {
				b = append(b, '_')
			}
			b = append(b, r-'A'+'a')
			prevLower = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b = append(b, r)
			prevLower = true
		default:
			if len(b) > 0 && b[len(b)-1] != '_' {
				b = append(b, '_')
			}
			prevLower = false
		}
	}
	out := strings.Trim(string(b), "_")
	if out == "" {
		return "attr"
	}
	return out
}

// profAddStdFor 는 std_name 을 안 줬을 때의 이름입니다: stdKeywords 에 걸리는 표준 항목, 아니면 속성 이름의 소문자·밑줄 변환.
func profAddStdFor(attr string) string {
	for _, m := range findStdCandidates(map[string]string{attr: ""}, nil) {
		if len(m.Cands) > 0 {
			return m.Std
		}
	}
	return lowerSnake(attr)
}

// buildProfileAdd 는 모델마다 sets 로 행을 만들고 prof 의 기존 행과 견줍니다 (파일은 건드리지 않음, BMC 접속 없음).
// prot 가 nil 이면 내장 보호 목록만 씁니다.
func buildProfileAdd(prof *Profile, models []*profAddModel, sets []profAddSet, prot *protectSet) *profAddPlan {
	plan := &profAddPlan{Profile: prof.Name}
	var valid []profAddSet
	seen := map[string]bool{}
	for _, s := range sets {
		s.Std, s.Attr, s.Value = strings.TrimSpace(s.Std), strings.TrimSpace(s.Attr), strings.TrimSpace(s.Value)
		switch {
		case s.Attr == "":
			plan.Rejected = append(plan.Rejected, "속성 이름이 비어 있음")
			continue
		case strings.IndexFunc(s.Attr, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0:
			plan.Rejected = append(plan.Rejected, fmt.Sprintf("속성 이름 %q: 제어문자(탭 포함)가 들어 있음", s.Attr))
			continue
		}
		if why := profAddValueErr(s.Value); why != "" {
			plan.Rejected = append(plan.Rejected, fmt.Sprintf("%s: %s", s.Attr, why))
			continue
		}
		if s.Std != "" && !reStdName.MatchString(s.Std) {
			plan.Rejected = append(plan.Rejected, fmt.Sprintf("%s: std_name %q 은(는) 소문자·숫자·밑줄만 쓸 수 있음", s.Attr, s.Std))
			continue
		}
		if why := prot.why(s.Attr); why != "" {
			plan.Rejected = append(plan.Rejected, fmt.Sprintf("%s: %s", s.Attr, why))
			continue
		}
		k := s.Std + "|" + strings.ToLower(s.Attr)
		if seen[k] {
			plan.Rejected = append(plan.Rejected, fmt.Sprintf("%s: 같은 속성이 두 번 입력됨", s.Attr))
			continue
		}
		seen[k] = true
		valid = append(valid, s)
	}

	for _, m := range models {
		vk, mk := vendorKey(m.Vendor), normalizeModel(m.Model)
		skip := func(attr, why string) {
			plan.Skipped = append(plan.Skipped, profAddSkip{Vendor: m.Vendor, Model: m.Model, Attr: attr, Reason: why})
		}
		if vk == "" || mk == "" || m.Model == "*" {
			for _, s := range valid {
				skip(s.Attr, "벤더·모델 이름이 올바르지 않음 (모델은 * 가 아니라 이름을 적어야 함)")
			}
			continue
		}
		used := map[string]bool{}
		for _, s := range valid {
			attr := s.Attr
			unchecked := len(m.attrs) == 0
			if !unchecked {
				found := false
				for _, a := range m.attrs {
					if n, ok := lookupAttr(a, s.Attr); ok {
						attr, found = n, true
						break
					}
				}
				if !found {
					skip(s.Attr, "이 모델에 없는 속성")
					continue
				}
				if why := prot.why(attr); why != "" {
					skip(attr, why)
					continue
				}
			}
			// 같은 벤더·모델(없으면 *)·속성의 행이 다른 std_name 으로 이미 있으면: std_name 을 안 적었을 때는 그 이름을 쓰고,
			// 다른 이름을 적었을 때는 (같은 속성 행을 둘 만들지 않고) 충돌로 다룬다 (덮어쓰기 때 기존 행의 값을 바꿈).
			var exactAny, wildAny *profRow
			for i := range prof.Rows {
				r := &prof.Rows[i]
				if r.vendorKey != vk || !strings.EqualFold(r.Attr, attr) {
					continue
				}
				switch {
				case r.modelKey == mk && (exactAny == nil || r.Std == s.Std):
					exactAny = r
				case r.modelKey == "*" && wildAny == nil:
					wildAny = r
				}
			}
			std := s.Std
			var other *profRow // 다른 std_name 으로 이미 있는 같은 속성 행
			switch {
			case exactAny != nil && std == "":
				std = exactAny.Std
			case exactAny != nil && exactAny.Std != std:
				other = exactAny
			case exactAny == nil && wildAny != nil && std == "":
				std = wildAny.Std
			}
			if std == "" {
				std = profAddStdFor(attr)
			}
			key := std + "|" + strings.ToLower(attr)
			if used[key] {
				skip(attr, "같은 행이 두 번 입력됨")
				continue
			}
			used[key] = true

			row := profAddRow{Vendor: m.Vendor, Model: m.Model, Std: std, Attr: attr, Value: s.Value, Idx: m.Idx, Hosts: m.Hosts, Unchecked: unchecked}
			if len(m.All) > 0 {
				row.Idx = m.All
			}
			if other != nil {
				row.Std, row.WantStd = other.Std, std
				row.Old, row.OldVerified, row.OldLine = other.Value, other.Verified, other.Line
				plan.Conflicts = append(plan.Conflicts, row)
				continue
			}
			var exact *profRow
			hasExactStd, hasWild := false, false
			for i := range prof.Rows {
				r := &prof.Rows[i]
				if r.vendorKey != vk || r.Std != std {
					continue
				}
				if r.modelKey == "*" {
					hasWild = true
					continue
				}
				if r.modelKey != mk {
					continue
				}
				hasExactStd = true
				if strings.EqualFold(r.Attr, attr) {
					exact = r
				}
			}
			switch {
			case exact != nil && strings.TrimSpace(exact.Value) == s.Value:
				plan.Same = append(plan.Same, row)
			case exact != nil:
				row.Old, row.OldVerified, row.OldLine = exact.Value, exact.Verified, exact.Line
				plan.Conflicts = append(plan.Conflicts, row)
			default:
				row.WildNote = hasWild && !hasExactStd
				plan.Add = append(plan.Add, row)
			}
		}
	}
	return plan
}

// Lines 는 미리보기 줄입니다 (TUI 글 보기·CLI 출력 공용).
func (p *profAddPlan) Lines() []string {
	out := []string{fmt.Sprintf("프로파일 %s — 추가 %d행 · 이미 있음 %d행 · 충돌 %d행 · 건너뜀 %d행", p.Profile, len(p.Add), len(p.Same), len(p.Conflicts), len(p.Skipped))}
	section := func(title string, n int) bool {
		if n == 0 {
			return false
		}
		out = append(out, "", title)
		return true
	}
	if section("[추가할 행] (verified 는 항상 N — 쓰기 허가 Y 는 사람이 파일에서 직접 고칩니다)", len(p.Add)) {
		for _, r := range p.Add {
			out = append(out, "  "+r.text()+profAddHostsNote(r))
			if r.WildNote {
				out = append(out, "    "+profAddNoteWild)
			}
		}
	}
	if section("[이미 있음] 같은 값의 행이 이미 있어 건너뜀", len(p.Same)) {
		for _, r := range p.Same {
			out = append(out, "  "+r.text())
		}
	}
	if section("[충돌] 같은 행이 다른 값으로 이미 있음 — 덮어쓸지 따로 묻습니다 (덮어쓰면 verified 는 N)", len(p.Conflicts)) {
		for _, r := range p.Conflicts {
			s := fmt.Sprintf("  %s %s  %s  %s: 기존 %s → 새 %s", r.Vendor, r.Model, r.Std, r.Attr, r.Old, r.Value)
			if r.WantStd != "" {
				s += fmt.Sprintf(" (입력한 std_name %s 는 기존 행의 %s 와 다름 — 같은 속성 행을 따로 만들지 않고 기존 행을 바꿈)", r.WantStd, r.Std)
			}
			if r.OldVerified {
				s += " (기존 verified=Y 는 N 으로 바뀜)"
			}
			out = append(out, s)
		}
	}
	if section("[건너뜀]", len(p.Skipped)) {
		for _, s := range p.Skipped {
			out = append(out, fmt.Sprintf("  %s %s  %s  (건너뜀: %s)", s.Vendor, s.Model, s.Attr, s.Reason))
		}
	}
	if section("[입력 거절] 이 설정은 행으로 만들지 않습니다", len(p.Rejected)) {
		for _, s := range p.Rejected {
			out = append(out, "  "+s)
		}
	}
	for _, r := range append(append([]profAddRow{}, p.Add...), p.Conflicts...) {
		if r.Unchecked {
			out = append(out, "", "※ 이 모델 서버를 읽지 않아 속성 이름이 BMC 에 실제로 있는지는 확인하지 못했습니다")
			break
		}
	}
	return out
}

func profAddHostsNote(r profAddRow) string {
	switch n := len(r.Hosts); {
	case n == 0:
		return ""
	case n == 1:
		return "   (서버 " + r.Hosts[0] + ")"
	default:
		return fmt.Sprintf("   (서버 %d대)", n)
	}
}

// Affected 는 행이 바뀌어 다시 점검해야 하는 서버의 스냅샷 번호입니다 (정렬·중복 제거). overwrite 면 덮어쓴 충돌 행의 서버도 넣습니다.
func (p *profAddPlan) Affected(overwrite bool) []int {
	set := map[int]bool{}
	rows := p.Add
	if overwrite {
		rows = append(append([]profAddRow{}, rows...), p.Conflicts...)
	}
	for _, r := range rows {
		for _, i := range r.Idx {
			set[i] = true
		}
	}
	out := make([]int, 0, len(set))
	for i := range set {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// profAddInsertAt 은 새 행을 넣을 줄 위치입니다: [redfish] 구역의 마지막 내용 줄 바로 뒤 (그 뒤에 `# [xml]` 이 있으면 그 줄·앞의 빈 줄 앞).
func profAddInsertAt(lines []string) int {
	section := profSecRedfish
	last := -1
	for i, raw := range lines {
		t := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if sec, ok := profileSection(t); ok {
			section = sec
			if sec == profSecRedfish {
				last = i
			}
			continue
		}
		if section == profSecRedfish && t != "" {
			last = i
		}
	}
	if last < 0 {
		return len(lines)
	}
	return last + 1
}

// profAddResult 는 쓰기 결과입니다.
type profAddResult struct {
	Added, Replaced int
	Backup          string
	Idx             []int // 다시 점검할 서버
}

// profAddWrite 는 plan 의 Add 행을 path 에 넣고 (overwrite 면 Conflicts 행의 값도 새 값·verified=N 으로 바꿈) 씁니다.
// 쓰기 전에 parseProfile 로 결과를 검증하고 백업을 만들며, 어느 단계든 실패하면 원본은 그대로입니다.
func profAddWrite(path string, plan *profAddPlan, overwrite bool, now time.Time) (*profAddResult, error) {
	orig, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("프로파일 %s 읽기 실패: %w", path, err)
	}
	perm := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil && st.Mode().Perm() != 0 {
		perm = st.Mode().Perm()
	}
	oldProf, err := parseProfile(string(orig), path)
	if err != nil {
		return nil, fmt.Errorf("현재 프로파일이 올바르지 않아 추가하지 않았습니다: %w", err)
	}
	text := string(orig)
	const bomMark = "\xef\xbb\xbf" // UTF-8 BOM (parseProfile 도 앞의 BOM 은 무시함)
	bom := strings.HasPrefix(text, bomMark)
	text = strings.TrimPrefix(text, bomMark)
	cr := ""
	if strings.Contains(text, "\r\n") { // 파일이 CRLF 면 새 줄도 맞춘다 (한 파일에 줄바꿈이 섞이지 않게)
		cr = "\r"
	}
	lines := strings.Split(text, "\n")
	res := &profAddResult{Idx: plan.Affected(overwrite)}

	if overwrite {
		for _, c := range plan.Conflicts {
			i := c.OldLine - 1
			if i < 0 || i >= len(lines) {
				return nil, fmt.Errorf("프로파일이 그 사이 바뀐 것 같습니다 (%d 줄이 없음) — 다시 시도하십시오", c.OldLine)
			}
			raw := strings.TrimSuffix(lines[i], "\r")
			cols := strings.Split(raw, "\t")
			if len(cols) < len(profileHeader) ||
				vendorKey(strings.TrimSpace(cols[0])) != vendorKey(c.Vendor) || normalizeModel(cols[1]) != normalizeModel(c.Model) ||
				strings.TrimSpace(cols[2]) != c.Std || !strings.EqualFold(strings.TrimSpace(cols[3]), c.Attr) {
				return nil, fmt.Errorf("프로파일이 그 사이 바뀐 것 같습니다 (%d 줄이 기존 행과 다름) — 다시 시도하십시오", c.OldLine)
			}
			cols[4], cols[5] = c.Value, "N"
			lines[i] = strings.Join(cols, "\t") + strings.TrimPrefix(lines[i], raw)
			res.Replaced++
		}
	}
	var add []string
	for _, r := range plan.Add {
		add = append(add, strings.Join([]string{r.Vendor, r.Model, r.Std, r.Attr, r.Value, "N"}, "\t")+cr)
	}
	res.Added = len(add)
	if res.Added == 0 && res.Replaced == 0 {
		return nil, fmt.Errorf("프로파일에 쓸 행이 없습니다")
	}
	at := profAddInsertAt(lines)
	out := make([]string, 0, len(lines)+len(add))
	out = append(out, lines[:at]...)
	out = append(out, add...)
	out = append(out, lines[at:]...)
	newText := strings.Join(out, "\n")
	if bom {
		newText = bomMark + newText
	}

	// 쓰기 전 검증: 로더가 그대로 읽고, 행 수가 맞고, 넣은·바꾼 행이 verified=N 인 새 값이어야 한다.
	np, err := parseProfile(newText, path)
	if err != nil {
		return nil, fmt.Errorf("추가한 결과가 올바르지 않아 쓰지 않았습니다: %w", err)
	}
	if len(np.Rows) != len(oldProf.Rows)+res.Added {
		return nil, fmt.Errorf("추가한 결과의 행 수가 맞지 않아 쓰지 않았습니다 (기대 %d, 실제 %d)", len(oldProf.Rows)+res.Added, len(np.Rows))
	}
	check := append([]profAddRow{}, plan.Add...)
	if overwrite {
		check = append(check, plan.Conflicts...)
	}
	for _, r := range check {
		vk, mk, ok := vendorKey(r.Vendor), normalizeModel(r.Model), false
		for _, nr := range np.Rows {
			if nr.vendorKey == vk && nr.modelKey == mk && nr.Std == r.Std && strings.EqualFold(nr.Attr, r.Attr) {
				ok = nr.Value == r.Value && !nr.Verified
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("추가한 결과에서 %s 행을 확인하지 못해 쓰지 않았습니다", r.text())
		}
	}

	bak := path + ".bak." + now.Format("20060102-150405")
	for i := 1; ; i++ {
		if _, err := os.Lstat(bak); os.IsNotExist(err) {
			break
		}
		bak = fmt.Sprintf("%s.bak.%s.%d", path, now.Format("20060102-150405"), i)
	}
	if err := writeFileAtomic(bak, orig, perm); err != nil {
		return nil, fmt.Errorf("백업 %s 을(를) 만들지 못해 쓰지 않았습니다: %w", bak, err)
	}
	if err := writeFileAtomic(path, []byte(newText), perm); err != nil {
		return nil, fmt.Errorf("프로파일 %s 쓰기 실패 (원본은 그대로, 백업 %s): %w", path, bak, err)
	}
	res.Backup = bak
	return res, nil
}

// profAddPrefill 은 대표서버의 표준 항목 현재값을 "std_name 속성 값" 줄로 만듭니다 (화면 입력 상자의 처음 내용).
// 보호 속성·서버 고유값 모양·JSON 객체 값은 넣지 않습니다.
func profAddPrefill(rep *biosSnap, prot *protectSet) []string {
	if rep == nil || !rep.ok() {
		return nil
	}
	var out []string
	for _, m := range findStdCandidates(rep.Attrs, nil) {
		for _, c := range m.Cands {
			v := profAddClean(c.Value)
			if v == "" || strings.ContainsAny(v, "|{[") || looksUnique(v) || prot.why(c.Name) != "" {
				continue
			}
			out = append(out, m.Std+" "+c.Name+" "+v)
			break
		}
	}
	return out
}

// parseProfAddLines 는 화면 입력 줄을 해석합니다: `<속성> <값>` 또는 `<std_name> <속성> <값>` (값에는 공백이 들어갈 수 있음).
// 첫 칸이 std_name 모양(소문자·숫자·밑줄)이고 뒤에 속성·값이 더 있으면 앞의 형식으로 봅니다.
func parseProfAddLines(lines []string) (sets []profAddSet, errs []string) {
	cut := func(s string) (string, string) {
		k := strings.IndexFunc(s, unicode.IsSpace)
		if k < 0 {
			return s, ""
		}
		return s[:k], strings.TrimSpace(s[k:])
	}
	for n, raw := range lines {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		t1, rest := cut(s)
		if rest == "" {
			errs = append(errs, fmt.Sprintf("%d번째 줄 %q: 설정값이 없음 (속성 이름 뒤에 공백, 그다음 설정값)", n+1, s))
			continue
		}
		if reStdName.MatchString(t1) {
			if t2, val := cut(rest); val != "" {
				sets = append(sets, profAddSet{Std: t1, Attr: t2, Value: val})
				continue
			}
		}
		sets = append(sets, profAddSet{Attr: t1, Value: rest})
	}
	return sets, errs
}

// parseProfAddFlag 는 -set "속성=값" 또는 -set "std_name:속성=값" 입니다.
func parseProfAddFlag(s string) (profAddSet, error) {
	k := strings.Index(s, "=")
	if k <= 0 {
		return profAddSet{}, fmt.Errorf(`-set %q: "속성=값" 또는 "std_name:속성=값" 형식이어야 합니다`, s)
	}
	left, val := strings.TrimSpace(s[:k]), strings.TrimSpace(s[k+1:])
	set := profAddSet{Attr: left, Value: val}
	if j := strings.Index(left, ":"); j >= 0 {
		set.Std, set.Attr = strings.TrimSpace(left[:j]), strings.TrimSpace(left[j+1:])
		if !reStdName.MatchString(set.Std) {
			return profAddSet{}, fmt.Errorf("-set %q: std_name %q 은(는) 소문자·숫자·밑줄만 쓸 수 있습니다", s, set.Std)
		}
	}
	if set.Attr == "" || set.Value == "" {
		return profAddSet{}, fmt.Errorf(`-set %q: 속성 이름과 값이 모두 있어야 합니다`, s)
	}
	return set, nil
}
