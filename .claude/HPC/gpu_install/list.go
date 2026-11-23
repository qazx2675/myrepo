// list.go - list 파일 파싱
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// Entry 는 list 의 한 줄이다. Version 이 비어 있으면 나중에 한 번 물어 채운다.
type Entry struct {
	Host    string
	Version string
}

// 드라이버 버전 문자열 (경로 문자가 섞이지 않게 제한한다).
var versionRe = regexp.MustCompile(`^[0-9][0-9A-Za-z._-]*$`)

// ParseList 는 "hostname" 또는 "hostname 버전" 줄을 읽는다.
// '#' 뒤는 주석, 빈 줄은 무시하고 같은 hostname 이 두 번 나오면 오류다.
func ParseList(r io.Reader) ([]Entry, error) {
	var out []Entry
	seen := map[string]int{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) > 2 {
			return nil, fmt.Errorf("%d번째 줄: 형식은 'hostname [버전]' 입니다: %q", n, strings.TrimSpace(line))
		}
		e := Entry{Host: f[0]}
		if len(f) == 2 {
			if !versionRe.MatchString(f[1]) {
				return nil, fmt.Errorf("%d번째 줄: 버전 형식이 올바르지 않습니다: %q", n, f[1])
			}
			e.Version = f[1]
		}
		if p, ok := seen[e.Host]; ok {
			return nil, fmt.Errorf("%d번째 줄: hostname 중복 (%s, %d번째 줄과 같음)", n, e.Host, p)
		}
		seen[e.Host] = n
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("대상 호스트가 없습니다")
	}
	return out, nil
}

// LoadList 는 파일을 열어 ParseList 한다.
func LoadList(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	es, err := ParseList(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return es, nil
}
