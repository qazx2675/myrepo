// Package config는 ${user}_setting.conf 형식의 평문 설정 파일을 읽는다.
// 외부 의존성 없는 폐쇄망 빌드 원칙에 따라 YAML/TOML 파서 대신
// "key = value" 형태의 자체 포맷을 사용한다.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config는 awxkit 실행에 필요한 전체 설정값을 담는다.
type Config struct {
	// AWX 접속 정보
	AWXURL      string
	Username    string
	Password    string
	InsecureTLS bool

	// [S1] NodeInfo
	S1Template    string
	S1HostnameKey string
	S1ExtraVars   string // "key=value, key2=value2" 형태. 템플릿의 다른 필수 survey 항목을 채울 때 사용
	S1Fetch       string // artifacts | stdout | remote
	S1ArtifactKey string // s1_fetch=artifacts 일 때 결과가 담긴 artifacts의 키. 비우면 전체 artifacts를 저장
	S1RemotePath  string
	S1OutputDir   string

	// [S2] 인벤토리 동기화
	S2InventorySource string
	S2Inventory       string

	// [S3] DHCP
	S3Template     string
	S3InfraKey     string
	S3InfraChoices string

	// [S4] PXE
	S4Template        string
	S4InfraKey        string
	S4InfraChoices    string
	S4OSVerKey        string
	S4OSVerChoices    string
	S4BootModeKey     string
	S4BootModeChoices string
	S4SplunkKey       string
	S4SplunkChoices   string
	S4Inventory       string

	// 공통 동작
	PollIntervalSec int
	HistoryFile     string

	// 로드된 실제 경로 (진단 출력용)
	SourcePath string
}

// 원본 key -> Config 필드 매핑 목록. 새 키를 추가할 때 여기만 늘리면 된다.
func (c *Config) fieldSetters() map[string]func(string) {
	return map[string]func(string){
		"awx_url":      func(v string) { c.AWXURL = v },
		"username":     func(v string) { c.Username = v },
		"password":     func(v string) { c.Password = v },
		"insecure_tls": func(v string) { c.InsecureTLS = parseBool(v) },

		"s1_template":     func(v string) { c.S1Template = v },
		"s1_hostname_key": func(v string) { c.S1HostnameKey = v },
		"s1_extra_vars":   func(v string) { c.S1ExtraVars = v },
		"s1_fetch":        func(v string) { c.S1Fetch = v },
		"s1_artifact_key": func(v string) { c.S1ArtifactKey = v },
		"s1_remote_path":  func(v string) { c.S1RemotePath = v },
		"s1_output_dir":   func(v string) { c.S1OutputDir = v },

		"s2_inventory_source": func(v string) { c.S2InventorySource = v },
		"s2_inventory":        func(v string) { c.S2Inventory = v },

		"s3_template":      func(v string) { c.S3Template = v },
		"s3_infra_key":     func(v string) { c.S3InfraKey = v },
		"s3_infra_choices": func(v string) { c.S3InfraChoices = v },

		"s4_template":         func(v string) { c.S4Template = v },
		"s4_infra_key":        func(v string) { c.S4InfraKey = v },
		"s4_infra_choices":    func(v string) { c.S4InfraChoices = v },
		"s4_osver_key":        func(v string) { c.S4OSVerKey = v },
		"s4_osver_choices":    func(v string) { c.S4OSVerChoices = v },
		"s4_bootmode_key":     func(v string) { c.S4BootModeKey = v },
		"s4_bootmode_choices": func(v string) { c.S4BootModeChoices = v },
		"s4_splunk_key":       func(v string) { c.S4SplunkKey = v },
		"s4_splunk_choices":   func(v string) { c.S4SplunkChoices = v },
		"s4_inventory":        func(v string) { c.S4Inventory = v },

		"poll_interval": func(v string) {
			if n, err := strconv.Atoi(v); err == nil {
				c.PollIntervalSec = n
			}
		},
		"history_file": func(v string) { c.HistoryFile = v },
	}
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "y", "1", "on":
		return true
	default:
		return false
	}
}

// Load는 지정된 경로의 conf 파일을 파싱한다.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일을 열 수 없습니다 (%s): %w", path, err)
	}
	defer f.Close()

	c := &Config{
		S1Fetch:         "artifacts",
		S1OutputDir:     "./output",
		PollIntervalSec: 3,
		HistoryFile:     "./awxkit_history.log",
		SourcePath:      path,
	}
	setters := c.fieldSetters()

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("%s:%d: '=' 형식이 아닙니다: %q", path, lineNo, line)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if setter, ok := setters[key]; ok {
			setter(val)
		}
		// 모르는 키는 향후 단계에서 쓸 수 있으므로 조용히 무시한다.
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("설정 파일 읽기 오류 (%s): %w", path, err)
	}

	return c, nil
}

// ResolvePath는 -conf 플래그, 사용자별 conf, 사용자 홈 디렉터리, 실행 파일 위치 순으로
// 설정 파일을 탐색해 최초로 존재하는 경로를 반환한다.
func ResolvePath(explicit, user string) (string, error) {
	if user == "" && explicit == "" {
		return "", fmt.Errorf("사용자를 식별할 수 없습니다 (-user 플래그, AWXKIT_USER 환경변수, 또는 config.CurrentUser()를 확인하세요)")
	}
	return ResolveNamedPath(explicit, user+"_setting.conf")
}

// ResolveNamedPath는 -conf류 명시적 경로가 있으면 그대로 쓰고, 없으면 ${user}_setting.conf와
// 동일한 탐색 순서(./conf/<filename> → ~/.awxkit/<filename> → <실행파일>/conf/<filename>)로 찾는다.
// hostlist(${user}.txt) 등 conf와 같은 규칙을 따르는 다른 파일에도 재사용한다.
func ResolveNamedPath(explicit, filename string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		return "", fmt.Errorf("지정한 파일이 없습니다: %s", explicit)
	}

	candidates := []string{
		filepath.Join(".", "conf", filename),
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".awxkit", filename))
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "conf", filename))
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s 파일을 찾지 못했습니다 (확인한 경로: %s)", filename, strings.Join(candidates, ", "))
}

// ReadHostList는 ${user}.txt 형식의 hostname 목록 파일을 읽는다.
// 한 줄에 hostname 하나, '#' 이후는 주석, 빈 줄은 무시한다.
func ReadHostList(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("호스트 목록 파일을 열 수 없습니다 (%s): %w", path, err)
	}
	defer f.Close()

	var hosts []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		hosts = append(hosts, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("호스트 목록 파일 읽기 오류 (%s): %w", path, err)
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("%s에 유효한 hostname이 없습니다", path)
	}
	return hosts, nil
}

// ParseKeyValues는 "key=value, key2=value2" 형태의 conf 값을 map으로 나눈다.
func ParseKeyValues(raw string) (map[string]string, error) {
	result := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return result, nil
	}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("형식이 올바르지 않습니다 (key=value): %q", pair)
		}
		result[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return result, nil
}
