package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// profile.go 는 profiles/<이름>.tsv (표준값 파일)를 읽습니다.
//
// 형식: 탭 구분, 첫 줄은 머리글 `vendor model std_name attribute value verified`.
// `#` 로 시작하는 줄과 빈 줄은 무시합니다 (줄 끝 주석은 없음).
//
//   - 한 std_name 에 여러 행이 있으면 속성 이름 후보입니다 (파일 순서가 우선순위).
//     실제 BMC 의 Bios.Attributes 에 있는 첫 후보를 쓰고, 그 행의 value/verified 를 적용합니다.
//   - value 에 `A|B` 를 쓰면 허용값 여러 개입니다 (하나라도 현재값과 같으면 OK).
//     값 비교는 대소문자까지 정확히 같아야 합니다 (Cisco 처럼 소문자 값을 쓰는 벤더는 행에 그대로 적는다).
//   - model 은 normalizeModel 로 비교하며, 같은 std_name 에서 정확히 맞는 행이 있으면 그것만 쓰고
//     없으면 model 이 `*` 인 (벤더 공통) 행을 씁니다. `*` 행은 점검 비교용이라 verified=Y 를 쓸 수 없습니다(로드 오류).
//   - 표준 4항목(system_profile, hyper_threading, llc_prefetch, sub_numa_cluster)은 프로파일에 행이
//     없어도 항상 점검 항목에 들어가고(→ MAPPING_MISSING), 그 밖의 std_name 행은 추가 항목이 됩니다.

var profileHeader = []string{"vendor", "model", "std_name", "attribute", "value", "verified"}

var reStdName = regexp.MustCompile(`^[a-z0-9_]+$`)

// profRow 는 프로파일 한 줄입니다.
type profRow struct {
	Line     int
	Vendor   string
	Model    string
	Std      string
	Attr     string
	Value    string   // 원문 (A|B 포함)
	Allowed  []string // Value 를 | 로 나눈 허용값
	Verified bool

	vendorKey string // 소문자 정규화 벤더
	modelKey  string // "*" 또는 normalizeModel(Model)
}

// Profile 은 로드된 프로파일입니다.
type Profile struct {
	Name  string
	Rows  []profRow
	Items []string // 점검 항목(std_name) 순서: 표준 4항목, 그다음 추가 항목(등장 순)
}

// vendorKey 는 벤더 이름 비교 키입니다 (DELL / Dell Inc. → dell).
func vendorKey(v string) string { return strings.ToLower(normalizeVendor(v)) }

// validProfileName 은 프로파일 이름이 파일명으로 안전한지 검사합니다.
func validProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("-profile <이름> 이 필요합니다")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("프로파일 이름에는 경로를 쓸 수 없습니다: %s", name)
	}
	return nil
}

// listProfileNames 는 dir 아래 *.tsv 의 이름(확장자 제외)을 정렬해 돌려줍니다.
func listProfileNames(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tsv") {
			out = append(out, strings.TrimSuffix(e.Name(), ".tsv"))
		}
	}
	sort.Strings(out)
	return out
}

// loadProfile 은 프로파일 파일을 읽습니다. name 은 보고서에 쓰는 이름입니다.
func loadProfile(path, name string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			msg := fmt.Sprintf("프로파일 %s 을(를) 찾을 수 없습니다", path)
			if names := listProfileNames(filepath.Dir(path)); len(names) > 0 {
				msg += " (사용 가능: " + strings.Join(names, ", ") + ")"
			}
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("프로파일 %s: %w", path, err)
	}
	p, err := parseProfile(string(data), path)
	if err != nil {
		return nil, err
	}
	p.Name = name
	return p, nil
}

// parseProfile 은 프로파일 텍스트를 해석합니다. file 은 오류 메시지용 이름입니다.
func parseProfile(text, file string) (*Profile, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	p := &Profile{}
	seen := map[string]int{} // 같은 (벤더, 모델, 항목, 속성) 중복 검사 → 처음 줄 번호
	headerDone := false
	for i, raw := range strings.Split(text, "\n") {
		n := i + 1
		line := strings.TrimRight(raw, "\r")
		if t := strings.TrimSpace(line); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		// 엑셀에서 붙여넣다 생기는 끝의 빈 열은 무시한다.
		for len(cols) > len(profileHeader) && strings.TrimSpace(cols[len(cols)-1]) == "" {
			cols = cols[:len(cols)-1]
		}
		if len(cols) != len(profileHeader) {
			return nil, fmt.Errorf("%s:%d: 탭으로 구분된 %d개 열이어야 합니다 (현재 %d개). 공백이 아니라 탭인지 확인하십시오",
				file, n, len(profileHeader), len(cols))
		}
		for j := range cols {
			cols[j] = strings.TrimSpace(cols[j])
		}
		if !headerDone {
			for j, h := range profileHeader {
				if !strings.EqualFold(cols[j], h) {
					return nil, fmt.Errorf("%s:%d: 첫 줄은 머리글 %q 이어야 합니다", file, n, strings.Join(profileHeader, "\t"))
				}
			}
			headerDone = true
			continue
		}
		r := profRow{Line: n, Vendor: cols[0], Model: cols[1], Std: cols[2], Attr: cols[3], Value: cols[4]}
		for j, name := range profileHeader[:5] {
			if cols[j] == "" {
				return nil, fmt.Errorf("%s:%d: %s 이(가) 비어 있습니다", file, n, name)
			}
		}
		if !reStdName.MatchString(r.Std) {
			return nil, fmt.Errorf("%s:%d: std_name %q 은(는) 소문자·숫자·밑줄만 쓸 수 있습니다", file, n, r.Std)
		}
		for _, v := range strings.Split(r.Value, "|") {
			if v = strings.TrimSpace(v); v == "" {
				return nil, fmt.Errorf("%s:%d: value %q 에 빈 허용값이 있습니다 (A|B 형식)", file, n, r.Value)
			}
			r.Allowed = append(r.Allowed, v)
		}
		switch strings.ToUpper(cols[5]) {
		case "Y":
			r.Verified = true
		case "N":
		default:
			return nil, fmt.Errorf("%s:%d: verified 는 Y 또는 N 이어야 합니다 (현재 %q)", file, n, cols[5])
		}
		r.vendorKey = vendorKey(r.Vendor)
		if r.Model == "*" {
			// * 행은 검증되지 않은 모델에도 맞으므로 쓰기 허가(verified=Y)를 줄 수 없다 (set.go verifyItem 도 다시 막음).
			if r.Verified {
				return nil, fmt.Errorf("%s:%d: verified=Y 는 모델을 지정한 행에만 쓸 수 있습니다 — * 행은 점검 비교용 (verified=N 으로 두고, 설정할 모델은 모델명을 적은 행을 추가하십시오)", file, n)
			}
			r.modelKey = "*"
		} else if r.modelKey = normalizeModel(r.Model); r.modelKey == "" {
			return nil, fmt.Errorf("%s:%d: model %q 에서 비교할 글자가 남지 않습니다", file, n, r.Model)
		}
		key := r.vendorKey + "|" + r.modelKey + "|" + r.Std + "|" + strings.ToLower(r.Attr)
		if first, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s:%d: %s %s %s %s 행이 %d 줄과 중복입니다", file, n, r.Vendor, r.Model, r.Std, r.Attr, first)
		}
		seen[key] = n
		p.Rows = append(p.Rows, r)
	}
	if !headerDone {
		return nil, fmt.Errorf("%s: 머리글(%s)이 없습니다", file, strings.Join(profileHeader, " "))
	}
	if len(p.Rows) == 0 {
		return nil, fmt.Errorf("%s: 항목 행이 하나도 없습니다", file)
	}

	have := map[string]bool{}
	for _, kw := range stdKeywords {
		p.Items = append(p.Items, kw.Std)
		have[kw.Std] = true
	}
	for _, r := range p.Rows {
		if !have[r.Std] {
			have[r.Std] = true
			p.Items = append(p.Items, r.Std)
		}
	}
	return p, nil
}

// rowsFor 는 (벤더, 모델) 호스트의 std 항목 후보 행을 우선순위(파일 순서)대로 돌려줍니다.
// 모델이 정확히 맞는 행이 하나라도 있으면 그것만, 없으면 model=`*` 행을 돌려줍니다 (항목별로 따로 판단).
func (p *Profile) rowsFor(vendor, model, std string) []profRow {
	vk, mk := vendorKey(vendor), normalizeModel(model)
	var exact, wild []profRow
	for _, r := range p.Rows {
		if r.Std != std || r.vendorKey != vk {
			continue
		}
		switch {
		case r.modelKey == "*":
			wild = append(wild, r)
		case mk != "" && r.modelKey == mk:
			exact = append(exact, r)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return wild
}
