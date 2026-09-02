package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadLines 는 한 줄에 하나씩 적힌 목록 파일을 읽습니다.
// 빈 줄과 # 로 시작하는 줄은 건너뜁니다. (vm-param-check 의 config.LoadLines 와 동일한 규약)
func LoadLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, sc.Err()
}

// loadVCenterList 는 vcenter.txt 를 읽어 vCenter 주소 목록을 만듭니다.
// 파일에는 주소만 한 줄에 하나씩 적습니다. 계정 정보는 환경 변수로 받습니다.
// 중복 주소는 제거하고 제거한 개수를 함께 돌려줍니다.
func loadVCenterList(path string) ([]string, int, error) {
	lines, err := LoadLines(path)
	if err != nil {
		return nil, 0, err
	}
	if len(lines) == 0 {
		return nil, 0, fmt.Errorf("%s 에 유효한 vCenter 주소가 없습니다", path)
	}
	return dedup(lines)
}

// readVMList 는 대상 VM 이름 목록을 읽습니다. 중복은 제거합니다.
func readVMList(path string) ([]string, int, error) {
	lines, err := LoadLines(path)
	if err != nil {
		return nil, 0, err
	}
	return dedup(lines)
}

// dedup 은 순서를 유지하면서 중복을 제거하고, 제거한 개수를 함께 돌려줍니다.
func dedup(in []string) ([]string, int, error) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	removed := 0
	for _, v := range in {
		if seen[v] {
			removed++
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, removed, nil
}

// loadCredentials 는 vCenter 계정을 환경 변수에서 읽습니다.
// 설정 파일에 비밀번호를 적지 않기 위한 것으로, vm-param-check 와 같은 변수명을 씁니다.
//
//	VC_USER / VC_PASS         (우선)
//	VCENTER_USER / VCENTER_PASS (대체)
//
// 목록에 적힌 모든 vCenter 에 같은 계정으로 접속합니다.
func loadCredentials() (string, string, error) {
	user := firstNonEmpty(os.Getenv("VC_USER"), os.Getenv("VCENTER_USER"))
	pass := firstNonEmpty(os.Getenv("VC_PASS"), os.Getenv("VCENTER_PASS"))
	if user == "" || pass == "" {
		return "", "", fmt.Errorf("VC_USER/VC_PASS (또는 VCENTER_USER/VCENTER_PASS) 환경 변수가 설정되지 않았습니다")
	}
	return user, pass, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
