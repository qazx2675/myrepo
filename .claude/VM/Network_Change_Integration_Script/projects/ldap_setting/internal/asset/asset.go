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
	seen := map[string]int{} // host -> out 안의 위치
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
			return nil, fmt.Errorf("%s:%d: 탭(\\t) 구분자가 없습니다 — 스페이스는 구분자로 인정하지 않습니다.\n"+
				"  내용(스페이스=·, 탭=→로 표시): %s",
				path, line, visualizeWhitespace(s))
		}
		host = strings.TrimSpace(host)
		site = strings.TrimSpace(site)
		if host == "" || site == "" {
			return nil, fmt.Errorf("%s:%d: hostname 또는 site 가 비어 있습니다: %q", path, line, s)
		}
		// 같은 호스트가 여러 줄 있으면 마지막 줄을 최신 정보로 봅니다.
		// 자산현황은 사람이 이어 붙여 쓰는 파일이라, 나중에 적은 줄이 옳습니다.
		// (예전에는 중복을 오류로 막았지만, 그러면 갱신된 자산현황을 못 쓰게 됩니다.)
		if idx, dup := seen[host]; dup {
			fmt.Fprintf(os.Stderr,
				"경고: %s:%d 호스트 %q 중복 — %d 번째 줄 대신 이 줄의 site %q 를 씁니다\n",
				path, line, host, out[idx].Line, site)
			out[idx] = Entry{Host: host, Site: site, Line: line}
			continue
		}
		seen[host] = len(out)
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

// evSuffixes 는 VM 이름 끝에 붙는 가상화 인스턴스 접미사입니다(구분자 없음).
//
// 자산현황에는 물리 호스트(BM)만 등재되어 있고 그 위에 올라간 VM 은 없을 수
// 있습니다. site 는 VM 이 올라가 있는 BM 기준으로 판정하면 되므로, VM 이름으로
// 조회가 안 될 때 이 접미사만 떼고 한 번 더 찾습니다 (svr001ev01 -> svr001).
//
// 목록을 ev01~ev03 세 개로 못박아 둔 것은 의도적입니다. "ev + 숫자" 를 통째로
// 지우면, 이름에 우연히 그런 조각이 들어간 호스트를 엉뚱한 BM 으로 축약할 수
// 있습니다.
var evSuffixes = []string{"ev01", "ev02", "ev03"}

// StripEV 는 호스트 이름 끝의 ev01~ev03 을 떼어냅니다.
// 해당 접미사가 없거나 떼면 빈 문자열이면 두 번째 반환값이 false 입니다.
func StripEV(host string) (string, bool) {
	for _, s := range evSuffixes {
		if base, ok := strings.CutSuffix(host, s); ok && base != "" {
			return base, true
		}
	}
	return "", false
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

// visualizeWhitespace 는 스페이스와 탭을 눈에 보이는 기호로 바꿔, 사용자가
// "탭인 줄 알았는데 사실 스페이스였다" 를 에러 메시지만 보고 바로 알 수 있게 합니다.
func visualizeWhitespace(s string) string {
	return strings.NewReplacer(" ", "·", "\t", "→").Replace(s)
}

// GroupBySite 는 사이트별로 호스트를 묶습니다. 배포는 사이트 단위로 나가기 때문입니다.
func GroupBySite(entries []Entry) map[string][]string {
	out := map[string][]string{}
	for _, e := range entries {
		out[e.Site] = append(out[e.Site], e.Host)
	}
	return out
}
