package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// StatusNoHostsEntry 는 /etc/hosts 에서 관리망 이름(hostname-m)을 찾지 못한 상태입니다.
const StatusNoHostsEntry = "NO_HOSTS_ENTRY"

// Target 은 user.txt 한 줄을 해석한 결과입니다.
//
// Hostname 은 보고서·TSV 에 쓰는 이름으로, 입력 이름에서 관리망 접미사 -m 을
// 뗀 기본 이름입니다 (IP 로 입력했으면 IP 그대로). IP 는 접속 주소입니다.
// 해석에 실패하면 IP 는 비고 Err 에 상태 문자열(StatusNoHostsEntry)이 들어갑니다.
//
// 입력에 포트가 붙어 있으면(`127.0.0.1:8443`, `[::1]:8443`, `이름-m:8443`) Port 에 보관합니다.
// 이때 Hostname 에도 ":포트" 가 붙어 같은 IP 의 다른 포트 대상이 구분됩니다 (mock 여러 대 시험용).
// 포트가 없으면 Port 는 비고 접속은 기본 443 입니다.
type Target struct {
	Input    string // user.txt 에 적힌 원문
	Hostname string
	IP       string // 접속 주소 (포트·대괄호 없음)
	Port     string // 입력에 포트가 있을 때만
	Err      string
}

// BaseURL 은 접속 기준 URL(`https://ip[:port]`)을 만듭니다. IPv6 는 대괄호로 감쌉니다.
// 해석 실패(IP 없음)면 빈 문자열입니다.
func (t Target) BaseURL() string {
	if t.IP == "" {
		return ""
	}
	host := t.IP
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if t.Port != "" {
		host += ":" + t.Port
	}
	return "https://" + host
}

// splitPort 는 입력 끝의 ":포트" 를 떼어 냅니다. 포트가 없어도 `[::1]` 의 대괄호는 벗깁니다.
// 맨몸 IPv6(`::1`)처럼 콜론이 여러 개인 값은 포트로 보지 않고 ok=false 입니다.
func splitPort(in string) (host, port string, ok bool) {
	if strings.HasPrefix(in, "[") {
		if strings.HasSuffix(in, "]") {
			return in[1 : len(in)-1], "", true
		}
		h, p, err := net.SplitHostPort(in)
		if err != nil || !validPort(p) {
			return "", "", false
		}
		return h, p, true
	}
	if strings.Count(in, ":") != 1 {
		return "", "", false
	}
	i := strings.Index(in, ":")
	if i == 0 || !validPort(in[i+1:]) {
		return "", "", false
	}
	return in[:i], in[i+1:], true
}

func validPort(p string) bool {
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535 && p == strconv.Itoa(n)
}

// parseList 는 대상 목록 텍스트를 해석합니다.
// 빈 줄과 # 주석은 무시하고 앞뒤 공백을 제거하며, 중복은 처음 것만 남깁니다.
func parseList(text string) []string {
	text = strings.TrimPrefix(text, "\ufeff")
	var out []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(text, "\n") {
		line := raw
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line) // CRLF 의 \r 도 함께 제거
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

// loadList 는 user.txt 같은 대상 목록 파일을 읽습니다.
func loadList(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("대상 목록 %s 이(가) 없습니다. user.txt.example 을 복사해 만드십시오", path)
		}
		return nil, fmt.Errorf("대상 목록 %s: %w", path, err)
	}
	return parseList(string(data)), nil
}

// loadRetryList 는 이전 결과 폴더의 재시도 목록을 읽습니다 (-retry-from).
// pathOrDir 는 폴더 경로 또는 retry.txt 파일 경로를 받습니다. 폴더에 merged_retry.txt 가 있으면(재시도 결과 폴더)
// 이번 재시도뿐 아니라 병합본 전체의 남은 대상이 들어 있으므로 그쪽을 읽고, 없으면 retry.txt 를 읽습니다.
// 목록이 없거나 비어 있으면 errNoRetryTargets 를 감싼 오류("재시도할 대상이 없습니다 ...")를 돌려줍니다 (호출자가 정상 종료로 처리).
// 폴더/파일 자체가 없는 것은 일반 오류입니다.
func loadRetryList(pathOrDir string) ([]string, error) {
	var retryPath string

	// 경로가 폴더인지 파일인지 판단
	fi, err := os.Stat(pathOrDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("결과 폴더/파일 %s 이(가) 없습니다", pathOrDir)
		}
		return nil, fmt.Errorf("결과 폴더/파일 %s: %w", pathOrDir, err)
	}

	if fi.IsDir() {
		retryPath = filepath.Join(pathOrDir, "retry.txt")
		if _, err := os.Stat(filepath.Join(pathOrDir, "merged_retry.txt")); err == nil {
			retryPath = filepath.Join(pathOrDir, "merged_retry.txt")
		}
	} else {
		retryPath = pathOrDir // 파일이면 그대로 사용 (이름은 따지지 않음)
	}

	data, err := os.ReadFile(retryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w (%s 없음)", errNoRetryTargets, retryPath)
		}
		return nil, fmt.Errorf("재시도 목록 %s: %w", retryPath, err)
	}

	inputs := parseList(string(data))
	if len(inputs) == 0 {
		return nil, fmt.Errorf("%w (%s 비어 있음)", errNoRetryTargets, retryPath)
	}
	return inputs, nil
}

// splitTargets 는 -targets 인자(쉼표 또는 공백 구분)를 대상 목록으로 바꿉니다.
func splitTargets(s string) []string {
	return parseList(strings.NewReplacer(",", "\n", " ", "\n", "\t", "\n").Replace(s))
}

// parseHosts 는 hosts 파일 텍스트에서 별칭(소문자) → IP 표를 만듭니다.
// 한 줄에 별칭이 여러 개여도 모두 등록하고, 같은 이름이 또 나오면 먼저 나온 줄을 씁니다.
func parseHosts(text string) map[string]string {
	m := map[string]string{}
	for _, raw := range strings.Split(text, "\n") {
		if i := strings.Index(raw, "#"); i >= 0 {
			raw = raw[:i]
		}
		f := strings.Fields(raw)
		if len(f) < 2 || net.ParseIP(f[0]) == nil {
			continue
		}
		for _, name := range f[1:] {
			key := strings.ToLower(name)
			if _, ok := m[key]; !ok {
				m[key] = f[0]
			}
		}
	}
	return m
}

// resolveTargets 는 입력 목록을 접속 대상으로 해석합니다.
//
//   - IP 리터럴            → 그대로
//   - 이름-m               → hosts 에서 그 이름 조회
//   - 이름 (접미사 없음)   → hosts 에서 이름-m 조회
//
// 각 형태 뒤에 ":포트" 를 붙일 수 있습니다 (`127.0.0.1:8443`, `[::1]:8443`, `이름-m:8443`).
// 못 찾은 대상은 Err=StatusNoHostsEntry 로 표시해 목록에 남깁니다.
// hosts 파일은 이름 입력이 하나라도 있을 때만 읽습니다.
func resolveTargets(inputs []string, hostsPath string) ([]Target, error) {
	var hosts map[string]string
	out := make([]Target, 0, len(inputs))
	for _, in := range inputs {
		if net.ParseIP(in) != nil {
			out = append(out, Target{Input: in, Hostname: in, IP: in})
			continue
		}
		name, port := in, ""
		if h, p, ok := splitPort(in); ok {
			name, port = h, p
		}
		if net.ParseIP(name) != nil {
			out = append(out, Target{Input: in, Hostname: in, IP: name, Port: port})
			continue
		}
		if hosts == nil {
			data, err := os.ReadFile(hostsPath)
			if err != nil {
				return nil, fmt.Errorf("hosts 파일 %s: %w", hostsPath, err)
			}
			hosts = parseHosts(string(data))
		}
		mgmt := name
		base := name
		if hasSuffixFold(name, "-m") {
			base = name[:len(name)-2]
		} else {
			mgmt = name + "-m"
		}
		t := Target{Input: in, Hostname: base, Port: port}
		if port != "" {
			t.Hostname = base + ":" + port
		}
		if ip, ok := hosts[strings.ToLower(mgmt)]; ok {
			t.IP = ip
		} else {
			t.Err = StatusNoHostsEntry
		}
		out = append(out, t)
	}
	return out, nil
}

func hasSuffixFold(s, suffix string) bool {
	return len(s) >= len(suffix) && strings.EqualFold(s[len(s)-len(suffix):], suffix)
}
