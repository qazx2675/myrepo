// Package target 은 {user}.txt (작업 대상 목록) 를 읽어들입니다.
//
// 형식은 "hostname 변경될ip" 공백 구분 한 줄입니다. site/인프라 같은 부가 정보는
// 없습니다 — 이 파일이 곧 유일한 작업 대상 지정 수단입니다.
package target

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// Entry 는 대상 호스트 한 대와 변경할 IP 입니다.
type Entry struct {
	Host  string
	NewIP string
}

// Load 는 지정한 경로의 대상 목록 파일을 읽어 Entry 목록으로 만듭니다.
//
// 빈 줄과 "#" 로 시작하는 줄은 건너뜁니다. 같은 hostname 이 두 번 나오면 오류입니다
// (어느 IP 로 바꿀지 모호해지는 사고를 막기 위함).
func Load(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	seen := map[string]bool{}
	var out []Entry

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s:%d: \"hostname 변경될ip\" 형식이 아닙니다: %q", path, lineNo, line)
		}
		host, ip := fields[0], fields[1]

		if net.ParseIP(ip) == nil || !strings.Contains(ip, ".") {
			return nil, fmt.Errorf("%s:%d: 유효한 IPv4 가 아닙니다: %q", path, lineNo, ip)
		}
		if seen[host] {
			return nil, fmt.Errorf("%s:%d: hostname %q 이 중복됩니다", path, lineNo, host)
		}
		seen[host] = true

		out = append(out, Entry{Host: host, NewIP: ip})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: 대상이 하나도 없습니다", path)
	}
	return out, nil
}

// Gateway 는 IP 의 마지막 옥텟을 "1" 로 바꾼 게이트웨이를 돌려줍니다.
// 모든 대상이 /24 라는 전제 하에 넷마스크는 검증하지 않습니다.
func Gateway(ip string) (string, error) {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return "", fmt.Errorf("IPv4 가 아닙니다: %q", ip)
	}
	return fmt.Sprintf("%s.%s.%s.1", parts[0], parts[1], parts[2]), nil
}
