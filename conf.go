package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// defaultConfName 은 실행한 디렉터리(cwd)에서 찾는 기본 설정 파일 이름이다.
const defaultConfName = "pm_monitor.conf"

// defaultDiskPattern 은 setup_check 의 기본 DISK_PATTERN 이다.
const defaultDiskPattern = `Disk "" given in clearpart command does not exist`

// Config 는 pm_monitor.conf 내용이다.
type Config struct {
	Gossh         string       // gossh 경로 (기본 "gossh", PATH 에서 찾음)
	IntervalSec   int          // 갱신 주기(초)
	DelayMin      int          // 지연 기준(분)
	DiskPatterns  []string     // 디스크미인식 문구(여러 줄)
	Vendors       []VendorRule // 벤더 규칙(순서대로)
	VendorOther   string       // 어느 규칙에도 안 맞을 때 이름
	DHCPServer    string
	LdapCheckSh   string
	PingTimeoutMs int
	PingPPS       int
	TCPParallel   int

	Path    string // 실제로 읽은 설정 파일 경로(없으면 "")
	Warning string // TUI 에 띄울 경고 한 줄(없으면 "")
}

// defaultConfig 는 기본값 설정을 만든다.
func defaultConfig() *Config {
	return &Config{
		Gossh:         "gossh",
		IntervalSec:   60,
		DelayMin:      60,
		DiskPatterns:  []string{defaultDiskPattern},
		Vendors:       defaultVendorRules(),
		VendorOther:   "기타",
		PingTimeoutMs: 1500,
		PingPPS:       500,
		TCPParallel:   100,
	}
}

// LoadConfig 는 설정 파일을 읽는다. path 가 비어 있으면 cwd 의 pm_monitor.conf 를 쓴다.
// 파일이 없으면 기본값과 Warning 을 돌려준다. 값이 잘못되면 error 를 돌려준다.
func LoadConfig(path string) (*Config, error) {
	cfg := defaultConfig()
	if path == "" {
		path = defaultConfName
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.Warning = fmt.Sprintf("설정 파일 %s 없음 - 기본값 사용", path)
			return cfg, nil
		}
		return nil, fmt.Errorf("설정 파일 열기 실패: %v", err)
	}
	defer f.Close()
	cfg.Path = path
	if err := cfg.parse(bufio.NewScanner(f)); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return cfg, nil
}

func (c *Config) parse(sc *bufio.Scanner) error {
	diskSet, vendorSet := false, false
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), string(rune(0xFEFF))))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, "=")
		if i < 0 {
			return fmt.Errorf("%d번째 줄: key=value 형식이 아님", n)
		}
		key := strings.TrimSpace(line[:i])
		val := strings.TrimSpace(line[i+1:])
		if err := c.set(key, val, &diskSet, &vendorSet); err != nil {
			return fmt.Errorf("%d번째 줄: %v", n, err)
		}
	}
	return sc.Err()
}

func (c *Config) set(key, val string, diskSet, vendorSet *bool) error {
	switch key {
	case "gossh":
		if val != "" {
			c.Gossh = val
		}
	case "interval_sec":
		return posInt(key, val, &c.IntervalSec)
	case "delay_min":
		return posInt(key, val, &c.DelayMin)
	case "ping_timeout_ms":
		return posInt(key, val, &c.PingTimeoutMs)
	case "ping_pps":
		return posInt(key, val, &c.PingPPS)
	case "tcp_parallel":
		return posInt(key, val, &c.TCPParallel)
	case "disk_pattern":
		if val == "" {
			return fmt.Errorf("disk_pattern 값이 비어 있음")
		}
		if !*diskSet { // 첫 줄이 기본값을 대체하고 이후 줄은 추가
			c.DiskPatterns = nil
			*diskSet = true
		}
		c.DiskPatterns = append(c.DiskPatterns, val)
	case "vendor":
		j := strings.LastIndex(val, "=")
		if j <= 0 || strings.TrimSpace(val[j+1:]) == "" {
			return fmt.Errorf("vendor 는 vendor=<정규식>=<이름> 형식이어야 함")
		}
		pat, name := strings.TrimSpace(val[:j]), strings.TrimSpace(val[j+1:])
		re, err := regexp.Compile(pat)
		if err != nil {
			return fmt.Errorf("vendor 정규식 오류: %v", err)
		}
		if !*vendorSet { // vendor 줄이 하나라도 있으면 기본 규칙 대체
			c.Vendors = nil
			*vendorSet = true
		}
		c.Vendors = append(c.Vendors, VendorRule{Pattern: pat, Name: name, re: re})
	case "vendor_other":
		if val == "" {
			return fmt.Errorf("vendor_other 값이 비어 있음")
		}
		c.VendorOther = val
	case "dhcp_server":
		c.DHCPServer = val
	case "ldap_check_sh":
		c.LdapCheckSh = val
	default:
		return fmt.Errorf("알 수 없는 키: %s", key)
	}
	return nil
}

func posInt(key, val string, dst *int) error {
	v, err := strconv.Atoi(val)
	if err != nil || v <= 0 {
		return fmt.Errorf("%s 는 1 이상의 정수여야 함: %q", key, val)
	}
	*dst = v
	return nil
}
