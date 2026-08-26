// Package dhcp는 /user/caedhcp/{3옥텟 대역} 형식의 isc-dhcp-server 설정 파일에서
// "host <hostname> { hardware ethernet ...; fixed-address ...; }" 블록을 파싱한다. (PLAN.md 4장)
// 파일명은 IP의 앞 3옥텟까지만 쓴다 (예: 10.10.10.15 → 파일명 "10.10.10", 뒤에 ".0" 안 붙임).
package dhcp

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
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

// SubnetPrefix는 IPv4 주소의 앞 3옥텟을 돌려준다 (예: "10.10.10.15" -> "10.10.10").
// DHCP 대역 파일명 규칙이 이 3옥텟까지만 쓰기 때문에, IP만 있으면 -subnet 없이 파일 경로를 만들 수 있다.
func SubnetPrefix(ip string) (string, error) {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return "", fmt.Errorf("IPv4 형식이 아님: %s", ip)
	}
	return strings.Join(parts[:3], "."), nil
}

// Resolve는 hostname을 DNS로 조회해 소속 대역을 알아낸 뒤, root/{3옥텟} 파일을 읽어
// 그 안의 hostname 레코드를 돌려준다. -subnet 옵션 없이 대역 파일을 자동으로 특정하기 위한 함수.
// DNS 조회 실패, 대역 파일 없음, 파일 안에 hostname 블록이 없음은 모두 에러로 반환된다 —
// 호출부는 이 경우 해당 hostname을 검증 실패(Block) 처리해야 한다.
func Resolve(root, hostname string) (Record, error) {
	ips, err := net.LookupHost(hostname)
	if err != nil || len(ips) == 0 {
		return Record{}, fmt.Errorf("DNS에서 %s의 IP를 찾지 못함: %v", hostname, err)
	}

	prefix, err := SubnetPrefix(ips[0])
	if err != nil {
		return Record{}, fmt.Errorf("%s의 DNS 응답(%s) 처리 실패: %w", hostname, ips[0], err)
	}

	path := root + "/" + prefix
	recs, err := parseFileCached(path)
	if err != nil {
		return Record{}, err
	}

	rec, ok := recs[hostname]
	if !ok {
		return Record{}, fmt.Errorf("%s 안에 %s 호스트 블록이 없음", path, hostname)
	}
	return rec, nil
}

// 아래는 대역 파일 파싱 결과 캐시다. Resolve는 hostname 1개마다 불리는데, 보통 수많은
// hostname이 같은 대역(=같은 파일)에 속한다. 캐시가 없으면 그 파일을 hostname 수만큼
// 반복해서 읽고 정규식으로 다시 파싱하게 되어, 대상이 많아질수록 그대로 느려진다.
// 실행 1회 동안만 유지되는 캐시이고, 반환되는 맵은 파싱 후 읽기 전용으로만 쓴다.
var (
	fileCacheMu sync.Mutex
	fileCache   = map[string]cachedParse{}
)

type cachedParse struct {
	recs map[string]Record
	err  error
}

// parseFileCached는 같은 경로에 대해 ParseFile을 한 번만 수행한다(에러도 함께 캐시).
func parseFileCached(path string) (map[string]Record, error) {
	fileCacheMu.Lock()
	defer fileCacheMu.Unlock()

	if c, ok := fileCache[path]; ok {
		return c.recs, c.err
	}
	recs, err := ParseFile(path)
	fileCache[path] = cachedParse{recs: recs, err: err}
	return recs, err
}
