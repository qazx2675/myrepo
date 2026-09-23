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

// evSuffix 는 한 물리서버(BM)에 동거하는 두 VM(ev01/ev02)의 이름 규칙입니다:
// VM 이름은 항상 "<BM이름>ev01" / "<BM이름>ev02" 형태입니다.
const (
	ev01Suffix = "ev01"
	ev02Suffix = "ev02"
)

// PairHostname 은 ev02 대상 hostname 에서 같은 BM 을 쓰는 ev01 대상의
// hostname 을 계산합니다. hostname 이 ev02 로 끝나지 않으면 ok=false 입니다.
func PairHostname(hostname string) (pair string, ok bool) {
	if !strings.HasSuffix(hostname, ev02Suffix) {
		return "", false
	}
	return strings.TrimSuffix(hostname, ev02Suffix) + ev01Suffix, true
}

// BareMetalName 은 ev02 대상 hostname 에서 BM(물리서버) 이름을 계산합니다.
// vCenter 인벤토리에 이 이름의 HostSystem(ESXi 호스트)이 있다고 전제합니다.
func BareMetalName(hostname string) (bm string, ok bool) {
	if !strings.HasSuffix(hostname, ev02Suffix) {
		return "", false
	}
	return strings.TrimSuffix(hostname, ev02Suffix), true
}

// Gateway 는 새 IP 의 마지막 옥텟을 1 로 바꾼 게이트웨이 주소를 계산합니다.
// (서브넷마스크는 255.255.255.0 / prefix 24 고정 전제)
func Gateway(ip string) string {
	i := strings.LastIndexByte(ip, '.')
	return ip[:i+1] + "1"
}

// SanitizeID 는 호스트네임을 게스트 안 임시 파일 이름(vsphere.CheckConnection 등이
// 연결 이름/되돌리기 스크립트를 저장하는 경로)으로 안전하게 쓸 수 있는 문자열로
// 바꿉니다. 영문/숫자/-/_ 이외의 문자는 모두 '_' 로 치환합니다.
func SanitizeID(hostname string) string {
	var b strings.Builder
	for _, r := range hostname {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
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
