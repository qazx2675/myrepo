package main

import "regexp"

// VendorRule 은 hostname 정규식 하나와 벤더 표시 이름이다.
type VendorRule struct {
	Pattern string
	Name    string
	re      *regexp.Regexp
}

// defaultVendorRules 는 config_check VENDOR_AWK 순서에 사용자 이름을 붙인 기본값이다.
func defaultVendorRules() []VendorRule {
	defs := [][2]string{
		{`^(c|h|sh|s2h|s3h|s4h)`, "D"},
		{`^s`, "sds"},
		{`^p`, "L"},
		{`^l`, "j"},
		{`^d`, "t"},
		{`^m`, "W"},
	}
	rules := make([]VendorRule, 0, len(defs))
	for _, d := range defs {
		rules = append(rules, VendorRule{Pattern: d[0], Name: d[1], re: regexp.MustCompile(d[0])})
	}
	return rules
}

// Vendor 는 hostname 의 벤더 표시 이름을 돌려준다.
// 위에서부터 처음 맞는 규칙을 쓰고, 없으면 VendorOther 를 쓴다.
func (c *Config) Vendor(host string) string {
	for _, r := range c.Vendors {
		if r.re != nil && r.re.MatchString(host) {
			return r.Name
		}
	}
	return c.VendorOther
}

// VendorOrder 는 코멘트 출력 순서용 벤더 이름 목록이다(설정 순서, 중복 제거, 마지막에 기타).
func (c *Config) VendorOrder() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range c.Vendors {
		if !seen[r.Name] {
			seen[r.Name] = true
			out = append(out, r.Name)
		}
	}
	if !seen[c.VendorOther] {
		out = append(out, c.VendorOther)
	}
	return out
}
