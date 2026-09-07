// Package asset 는 자산현황 텍스트 파일을 읽습니다.
//
// 형식: 한 줄에 "hostname<TAB>site" 하나씩.
//
//	svr001	a1
//	svr002	a3
package asset

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Entry 는 자산현황 한 줄입니다.
type Entry struct {
	Host string
	Site string
	Line int
}

// Load 는 자산현황 파일을 읽어 호스트 목록을 돌려줍니다.
// 빈 줄과 '#' 주석 줄은 건너뜁니다.
func Load(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Entry
	seen := map[string]int{}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimRight(sc.Text(), " \t\r")
		if strings.TrimSpace(s) == "" || strings.HasPrefix(strings.TrimSpace(s), "#") {
			continue
		}

		host, site, ok := strings.Cut(s, "\t")
		if !ok {
			return nil, fmt.Errorf("%s:%d: 탭 구분자가 없습니다: %q", path, line, s)
		}
		host = strings.TrimSpace(host)
		site = strings.TrimSpace(site)
		if host == "" || site == "" {
			return nil, fmt.Errorf("%s:%d: hostname 또는 site 가 비어 있습니다: %q", path, line, s)
		}
		if prev, dup := seen[host]; dup {
			return nil, fmt.Errorf("%s:%d: 호스트 %q 가 %d 번째 줄과 중복됩니다", path, line, host, prev)
		}
		seen[host] = line
		out = append(out, Entry{Host: host, Site: site, Line: line})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: 유효한 항목이 없습니다", path)
	}
	return out, nil
}

// LoadHostList 는 순수 호스트 목록을 읽습니다. 한 줄에 hostname 하나, site 정보는
// 없습니다. -host-file 로 "이 호스트들만 처리" 를 지정할 때 씁니다 — site 판정은
// 항상 자산현황(-assets)에서 이뤄지고, 이 목록은 그중 어떤 호스트를 고를지만 정합니다.
func LoadHostList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		h := strings.TrimSpace(sc.Text())
		if h == "" || strings.HasPrefix(h, "#") {
			continue
		}
		if seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: 유효한 호스트가 없습니다", path)
	}
	return out, nil
}

// GroupBySite 는 사이트별로 호스트를 묶습니다. 배포는 사이트 단위로 나가기 때문입니다.
func GroupBySite(entries []Entry) map[string][]string {
	out := map[string][]string{}
	for _, e := range entries {
		out[e.Site] = append(out[e.Site], e.Host)
	}
	return out
}
