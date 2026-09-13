// Package config는 copy_setting.conf 형식의 평문 설정 파일을 읽는다.
// 외부 의존성 없는 폐쇄망 빌드 원칙에 따라 "key = value" 형태의 자체 포맷을 사용한다.
// ETX(송신)와 Windows(수신) 양쪽이 같은 conf 스키마를 공유한다.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config는 copy-send / copy-widget 실행에 필요한 전체 설정값을 담는다.
type Config struct {
	// AWX 접속 정보
	AWXURL      string
	Username    string
	Password    string
	InsecureTLS bool

	// 브릿지로 사용할 Job Template. ID(숫자 문자열) 또는 이름.
	Template string

	// ETX -> AWX 전송 시 이 줄 수를 넘으면 거부한다 (AWX 웹페이지 부하 방지).
	MaxLines int

	// 로드된 실제 경로 (진단 출력용)
	SourcePath string
}

func (c *Config) fieldSetters() map[string]func(string) {
	return map[string]func(string){
		"awx_url":      func(v string) { c.AWXURL = v },
		"username":     func(v string) { c.Username = v },
		"password":     func(v string) { c.Password = v },
		"insecure_tls": func(v string) { c.InsecureTLS = parseBool(v) },
		"template":     func(v string) { c.Template = v },
		"max_lines": func(v string) {
			if n, err := strconv.Atoi(v); err == nil {
				c.MaxLines = n
			}
		},
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
		MaxLines:   200,
		SourcePath: path,
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
	if c.AWXURL == "" || c.Username == "" || c.Template == "" {
		return nil, fmt.Errorf("%s: awx_url, username, template 은 필수입니다", path)
	}
	return c, nil
}

// ResolvePath는 -conf 플래그, 실행 위치의 conf 폴더, 실행 파일 위치, 홈 디렉터리
// 순으로 copy_setting.conf 를 탐색해 최초로 존재하는 경로를 반환한다.
func ResolvePath(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit, nil
		}
		return "", fmt.Errorf("지정한 설정 파일이 없습니다: %s", explicit)
	}

	const filename = "copy_setting.conf"
	candidates := []string{
		filepath.Join(".", "conf", filename),
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "conf", filename))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".copy-bridge", filename))
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s 파일을 찾지 못했습니다 (확인한 경로: %s)", filename, strings.Join(candidates, ", "))
}
