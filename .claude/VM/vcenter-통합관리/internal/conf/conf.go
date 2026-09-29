// Package conf 는 수집기와 런처가 공유하는 vcportal.conf(INI) 를 읽는다.
package conf

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/korean"
)

type VCenter struct {
	ID   string
	URL  string
	Host string // hostname[:port]
}

type Config struct {
	Path                       string
	OutputDir, WorkDir, LogDir string
	Parallel                   int
	Timeout                    time.Duration
	User, Password             string
	Browser                    string // edge | chrome
	VCenters                   []VCenter
}

var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// CleanPath 는 탐색기에서 복사한 경로를 정리한다.
// 앞뒤 공백/따옴표 한 쌍/끝의 \ 또는 / 를 제거하되 C:\ 와 / 루트는 유지한다.
func CleanPath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	for len(s) > 0 && (s[len(s)-1] == '\\' || s[len(s)-1] == '/') {
		if len(s) == 3 && s[1] == ':' { // C:\
			break
		}
		if len(s) == 1 { // /
			break
		}
		s = strings.TrimSpace(s[:len(s)-1])
	}
	return s
}

func decode(b []byte) (string, error) {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(b) {
		return string(b), nil
	}
	out, err := korean.EUCKR.NewDecoder().Bytes(b)
	if err != nil {
		return "", fmt.Errorf("문자 인코딩을 해석할 수 없습니다(UTF-8 또는 CP949 필요): %w", err)
	}
	return string(out), nil
}

// Load 는 conf 파일을 읽어 검증한다.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("conf 파일을 읽을 수 없습니다: %w", err)
	}
	text, err := decode(raw)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	c := &Config{Path: path, Parallel: 5, Timeout: 300 * time.Second, Browser: "edge"}
	section := ""
	hosts := map[string]string{}
	ids := map[string]int{}

	for i, line := range strings.Split(text, "\n") {
		n := i + 1
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			if line[len(line)-1] != ']' {
				return nil, fmt.Errorf("%s %d번째 줄: 섹션 형식이 잘못되었습니다: %s", name, n, line)
			}
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			switch section {
			case "paths", "collect", "account", "browser", "vcenters":
			default:
				return nil, fmt.Errorf("%s %d번째 줄: 알 수 없는 섹션 [%s]", name, n, section)
			}
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("%s %d번째 줄: '키 = 값' 형식이 아닙니다: %s", name, n, line)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if section == "" {
			return nil, fmt.Errorf("%s %d번째 줄: 섹션([paths] 등) 밖에 있는 설정입니다", name, n)
		}
		if section == "vcenters" {
			if !idRe.MatchString(key) {
				return nil, fmt.Errorf("%s %d번째 줄: vCenter id '%s' 는 영문/숫자/_/- 만 사용할 수 있습니다", name, n, key)
			}
			u, host, err := normalizeURL(val)
			if err != nil {
				return nil, fmt.Errorf("%s %d번째 줄: %s 의 URL 이 잘못되었습니다: %v", name, n, key, err)
			}
			if p, ok := ids[strings.ToLower(key)]; ok {
				return nil, fmt.Errorf("%s %d번째 줄: vCenter id '%s' 가 %d번째 줄과 중복됩니다", name, n, key, p)
			}
			if other, ok := hosts[strings.ToLower(host)]; ok {
				return nil, fmt.Errorf("%s %d번째 줄: 호스트 '%s' 가 '%s' 와 중복됩니다", name, n, host, other)
			}
			ids[strings.ToLower(key)] = n
			hosts[strings.ToLower(host)] = key
			c.VCenters = append(c.VCenters, VCenter{ID: key, URL: u, Host: host})
			continue
		}
		switch section + "." + strings.ToLower(key) {
		case "paths.output_dir":
			c.OutputDir = CleanPath(val)
		case "paths.work_dir":
			c.WorkDir = CleanPath(val)
		case "paths.log_dir":
			c.LogDir = CleanPath(val)
		case "collect.parallel":
			v, err := strconv.Atoi(val)
			if err != nil || v < 1 {
				return nil, fmt.Errorf("%s %d번째 줄: parallel 은 1 이상의 정수여야 합니다: %s", name, n, val)
			}
			c.Parallel = v
		case "collect.timeout":
			v, err := strconv.Atoi(val)
			if err != nil || v < 1 {
				return nil, fmt.Errorf("%s %d번째 줄: timeout 은 1 이상의 정수(초)여야 합니다: %s", name, n, val)
			}
			c.Timeout = time.Duration(v) * time.Second
		case "account.user":
			c.User = val
		case "account.password":
			c.Password = val
		case "browser.type":
			b := strings.ToLower(val)
			if b != "edge" && b != "chrome" {
				return nil, fmt.Errorf("%s %d번째 줄: browser type 은 edge 또는 chrome 이어야 합니다: %s", name, n, val)
			}
			c.Browser = b
		default:
			return nil, fmt.Errorf("%s %d번째 줄: 알 수 없는 키 [%s] %s", name, n, section, key)
		}
	}

	if c.OutputDir == "" {
		return nil, fmt.Errorf("%s: [paths] output_dir 이 필요합니다", name)
	}
	if c.User == "" {
		return nil, fmt.Errorf("%s: [account] user 가 필요합니다", name)
	}
	if c.Password == "" {
		return nil, fmt.Errorf("%s: [account] password 가 필요합니다", name)
	}
	if len(c.VCenters) == 0 {
		return nil, fmt.Errorf("%s: [vcenters] 에 vCenter 가 하나 이상 필요합니다", name)
	}
	if c.WorkDir == "" {
		c.WorkDir = filepath.Join(os.TempDir(), "vcportal-work")
	}
	return c, nil
}

func normalizeURL(s string) (full, host string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("값이 비어 있습니다")
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	s = strings.TrimRight(s, "/")
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("해석할 수 없습니다: %s", s)
	}
	return s, u.Host, nil
}

// VCenterByHost 는 hostname[:port] 로 vCenter 를 찾는다(대소문자 무시).
func (c *Config) VCenterByHost(host string) *VCenter {
	for i := range c.VCenters {
		if strings.EqualFold(c.VCenters[i].Host, host) {
			return &c.VCenters[i]
		}
	}
	return nil
}
