package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Config 는 vcenter.txt 에서 읽어들이는 vCenter 접속 정보입니다.
type Config struct {
	Host       string
	User       string
	Password   string
	Datacenter string
	Insecure   bool
}

// loadVCenterConfig 는 KEY=VALUE 형식의 설정 파일을 읽어 Config 를 만듭니다.
// 필수 항목이 비어 있으면 에러를 돌려줍니다.
func loadVCenterConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &Config{Insecure: true}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "VCENTER_HOST":
			cfg.Host = val
		case "VCENTER_USER":
			cfg.User = val
		case "VCENTER_PASS":
			cfg.Password = val
		case "VCENTER_DATACENTER":
			cfg.Datacenter = val
		case "VCENTER_INSECURE":
			cfg.Insecure = !strings.EqualFold(val, "false")
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	var missing []string
	if cfg.Host == "" {
		missing = append(missing, "VCENTER_HOST")
	}
	if cfg.User == "" {
		missing = append(missing, "VCENTER_USER")
	}
	if cfg.Password == "" {
		missing = append(missing, "VCENTER_PASS")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s 에 필수 항목이 없습니다: %s", path, strings.Join(missing, ", "))
	}
	return cfg, nil
}

// readVMList 는 한 줄에 하나씩 적힌 VM 이름 목록을 읽습니다.
// 빈 줄과 # 주석은 건너뛰고, 중복된 이름은 제거하면서 몇 개를 제거했는지 함께 돌려줍니다.
func readVMList(path string) ([]string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	seen := make(map[string]bool)
	var vms []string
	dup := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if seen[line] {
			dup++
			continue
		}
		seen[line] = true
		vms = append(vms, line)
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}
	return vms, dup, nil
}
