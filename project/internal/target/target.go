// Package target 은 vcenter.txt / list.txt 두 입력 파일을 읽습니다.
package target

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// Entry 는 list.txt 한 줄: 바꿀 대상 VM 과 그 VM 에 설정할 새 IP 입니다.
type Entry struct {
	Hostname string // vCenter 인벤토리상의 VM 이름 (도메인 없음)
	NewIP    string
}

// LoadVCenters 는 vcenter.txt(대상 vCenter 주소, 한 줄에 하나)를 읽습니다.
// 빈 줄과 '#' 로 시작하는 줄은 무시합니다.
func LoadVCenters(path string) ([]string, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%s 에 대상 vCenter 가 없습니다", path)
	}
	return lines, nil
}

// LoadEntries 는 list.txt("호스트네임 IP", 공백 구분, 한 줄에 하나)를 읽습니다.
func LoadEntries(path string) ([]Entry, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("%s: 형식 오류(호스트네임과 IP 2개 필드가 필요합니다): %q", path, line)
		}
		host, ip := fields[0], fields[1]
		if net.ParseIP(ip) == nil || strings.Contains(ip, ":") {
			return nil, fmt.Errorf("%s: 올바르지 않은 IPv4 주소: %q", path, ip)
		}
		entries = append(entries, Entry{Hostname: host, NewIP: ip})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s 에 작업 대상이 없습니다", path)
	}
	return entries, nil
}

// Gateway 는 새 IP 의 마지막 옥텟을 1 로 바꾼 게이트웨이 주소를 계산합니다.
// (서브넷마스크는 255.255.255.0 / prefix 24 고정 전제)
func Gateway(ip string) string {
	i := strings.LastIndexByte(ip, '.')
	return ip[:i+1] + "1"
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = strings.TrimPrefix(line, string(rune(0xFEFF))) // UTF-8 BOM
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, scanner.Err()
}
