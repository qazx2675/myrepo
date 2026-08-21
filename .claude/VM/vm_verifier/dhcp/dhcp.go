// Package dhcp는 /user/caedhcp/{/24대역} 형식의 isc-dhcp-server 설정 파일에서
// "host <hostname> { hardware ethernet ...; fixed-address ...; }" 블록을 파싱한다. (PLAN.md 4장)
package dhcp

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Record는 DHCP 파일에 선언된 호스트 1개의 정적 MAC/IP 쌍이다.
type Record struct {
	Hostname string
	MAC      string
	IP       string
}

var hostBlockRe = regexp.MustCompile(`host\s+(\S+)\s*\{([^}]*)\}`)
var macRe = regexp.MustCompile(`hardware\s+ethernet\s+([0-9a-fA-F:]+)\s*;`)
var fixedAddrRe = regexp.MustCompile(`fixed-address\s+([0-9.]+)\s*;`)

// ParseFile은 DHCP 설정 파일을 읽어 hostname -> Record 맵으로 구조화한다.
// 파일이 없거나 읽기 실패하면 에러를 반환한다 — 호출부는 이 경우 무조건 검증 실패(Block) 처리해야 한다.
func ParseFile(path string) (map[string]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("DHCP 파일 로드 실패(%s): %w", path, err)
	}

	result := make(map[string]Record)
	for _, m := range hostBlockRe.FindAllStringSubmatch(string(data), -1) {
		hostname := strings.TrimSpace(m[1])
		block := m[2]

		mac := ""
		if mm := macRe.FindStringSubmatch(block); mm != nil {
			mac = strings.ToLower(mm[1])
		}
		ip := ""
		if im := fixedAddrRe.FindStringSubmatch(block); im != nil {
			ip = im[1]
		}
		result[hostname] = Record{Hostname: hostname, MAC: mac, IP: ip}
	}
	return result, nil
}

// PathForSubnet은 /24 대역 문자열(예: 10.10.10.0)로부터 DHCP 파일 경로를 만든다.
func PathForSubnet(root, subnet string) string {
	return root + "/" + subnet
}
