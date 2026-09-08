// Package config 는 ldap_config.conf (flat key=value 형식) 를 읽어들입니다.
//
// 대상 노드에 jq 가 없기 때문에 JSON 대신 평문 key=value 를 씁니다.
// 같은 파일을 bash 체크 스크립트도 grep 만으로 읽습니다.
package config

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Site 는 한 인프라 안의 사이트(a1~a4) 하나입니다.
type Site struct {
	Name       string
	URIOrder   []string // "uri1","uri2","uri3" 같은 키 이름의 나열
	Storage    string   // auto.appl 의 storage 이름 (사이트 판별 키)
	Mountpoint string   // auto.appl 의 /appl 원격 경로
	WapplMount string   // auto.appl 의 /wappl 원격 경로. 비어 있으면 /wappl 줄을 만들지 않음(선택 항목)
}

// Infra 는 DNS/NTP 로 구분되는 인프라 하나입니다.
type Infra struct {
	Name   string
	DNS    []string
	NTP    []string
	URI    map[string]string // "uri1" -> "ldap://.../" , 값이 NONE 이면 제외 대상
	BindDN string
	BindPW string
	Sites  map[string]Site
}

// S4Rule 은 hostname 이 특정 접두사로 시작할 때의 강제 오버라이드 규칙입니다.
type S4Rule struct {
	Enabled  bool
	Prefix   string
	Services []string // "nslcd", "ntp"
}

// Config 는 ldap_config.conf 전체입니다.
type Config struct {
	Infras map[string]Infra
	S4     S4Rule
	raw    map[string]string
}

// Load 는 지정한 경로의 설정 파일을 읽어 Config 로 만듭니다.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	raw := map[string]string{}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: '=' 가 없는 줄입니다: %q", path, line, s)
		}
		raw[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return build(raw)
}

func build(raw map[string]string) (*Config, error) {
	c := &Config{Infras: map[string]Infra{}, raw: raw}

	c.S4 = S4Rule{
		Enabled:  raw["s4.enabled"] == "true",
		Prefix:   raw["s4.prefix"],
		Services: splitList(raw["s4.services"]),
	}
	if c.S4.Enabled && c.S4.Prefix == "" {
		return nil, fmt.Errorf("s4.enabled=true 인데 s4.prefix 가 비어 있습니다")
	}

	// infra.<이름>.* 키에서 인프라 이름을 모읍니다.
	for k := range raw {
		parts := strings.Split(k, ".")
		if len(parts) < 3 || parts[0] != "infra" {
			continue
		}
		name := parts[1]
		if _, ok := c.Infras[name]; ok {
			continue
		}
		in, err := buildInfra(raw, name)
		if err != nil {
			return nil, err
		}
		c.Infras[name] = in
	}
	if len(c.Infras) == 0 {
		return nil, fmt.Errorf("infra.<이름>.* 항목이 하나도 없습니다")
	}
	return c, nil
}

func buildInfra(raw map[string]string, name string) (Infra, error) {
	p := "infra." + name + "."
	in := Infra{
		Name:   name,
		DNS:    splitList(raw[p+"dns"]),
		NTP:    splitList(raw[p+"ntp"]),
		URI:    map[string]string{},
		BindDN: raw[p+"binddn"],
		BindPW: raw[p+"bindpw"],
		Sites:  map[string]Site{},
	}
	for _, key := range []string{"uri1", "uri2", "uri3"} {
		if v, ok := raw[p+key]; ok && v != "" {
			in.URI[key] = v
		}
	}

	for k := range raw {
		// infra.<이름>.site.<사이트>.<속성>
		rest, ok := strings.CutPrefix(k, p+"site.")
		if !ok {
			continue
		}
		site, _, ok := strings.Cut(rest, ".")
		if !ok || site == "" {
			continue
		}
		if _, done := in.Sites[site]; done {
			continue
		}
		sp := p + "site." + site + "."
		in.Sites[site] = Site{
			Name:       site,
			URIOrder:   splitList(raw[sp+"uri_order"]),
			Storage:    raw[sp+"storage"],
			Mountpoint: raw[sp+"mountpoint"],
			WapplMount: raw[sp+"wappl_mount"],
		}
	}

	if err := in.validate(); err != nil {
		return Infra{}, fmt.Errorf("infra %q: %w", name, err)
	}
	return in, nil
}

func (in Infra) validate() error {
	if len(in.DNS) == 0 {
		return fmt.Errorf("dns 가 비어 있습니다")
	}
	if len(in.NTP) == 0 {
		return fmt.Errorf("ntp 가 비어 있습니다")
	}
	if in.BindDN == "" || in.BindPW == "" {
		return fmt.Errorf("binddn/bindpw 가 비어 있습니다")
	}
	if _, ok := in.URI["uri1"]; !ok {
		return fmt.Errorf("uri1 이 없습니다")
	}
	if len(in.Sites) == 0 {
		return fmt.Errorf("site 정의가 하나도 없습니다")
	}
	seen := map[string]string{}
	for _, s := range in.SiteNames() {
		site := in.Sites[s]
		if site.Storage == "" {
			return fmt.Errorf("site %q: storage 가 비어 있습니다", s)
		}
		if site.Mountpoint == "" {
			return fmt.Errorf("site %q: mountpoint 가 비어 있습니다", s)
		}
		if len(site.URIOrder) == 0 {
			return fmt.Errorf("site %q: uri_order 가 비어 있습니다", s)
		}
		// storage 는 사이트 판별 키라서 인프라 안에서 고유해야 합니다.
		if prev, dup := seen[site.Storage]; dup {
			return fmt.Errorf("storage %q 가 site %q 와 site %q 에 중복됩니다", site.Storage, prev, s)
		}
		seen[site.Storage] = s
		for _, key := range site.URIOrder {
			if _, ok := in.URI[key]; !ok {
				return fmt.Errorf("site %q: uri_order 의 %q 가 정의되어 있지 않습니다", s, key)
			}
		}
	}
	return nil
}

// SiteNames 는 사이트 이름을 정렬해서 돌려줍니다(출력 순서 고정용).
func (in Infra) SiteNames() []string {
	out := make([]string, 0, len(in.Sites))
	for k := range in.Sites {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// URIList 는 해당 사이트의 URI 를 순서대로, NONE 인 것은 빼고 돌려줍니다.
func (in Infra) URIList(site string) []string {
	s, ok := in.Sites[site]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(s.URIOrder))
	for _, key := range s.URIOrder {
		v := in.URI[key]
		if v == "" || v == "NONE" {
			continue
		}
		out = append(out, v)
	}
	return out
}

// InfraNames 는 인프라 이름을 정렬해서 돌려줍니다.
func (c *Config) InfraNames() []string {
	out := make([]string, 0, len(c.Infras))
	for k := range c.Infras {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
