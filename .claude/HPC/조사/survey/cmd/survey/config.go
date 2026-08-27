package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// defaultGosshConc 는 conf 에 concurrency 가 없을 때 쓰는 gossh 동시 실행 수(-c)다.
const defaultGosshConc = 4000

// MountRule 은 설정값의 mountpoint 이름별 "정상 위치" 를 나타낸다.
type MountRule struct {
	Name     string
	Location string
}

// Config 는 conf.toml 에서 읽어들인 실행 설정이다.
type Config struct {
	AssetFile    string // [input].asset_file    : 표1 텍스트 경로
	GosshBin     string // [gossh].bin           : gossh 실행 파일
	GosshConc    int    // [gossh].concurrency   : gossh 동시 실행 수 (-c). 설정값 조사에만 적용
	GosshTimeout int    // [gossh].timeout       : gossh 타임아웃 초 (-t). 0 이면 지정 안 함
	GosshArgs    string // [gossh].extra_args    : gossh 추가 플래그
	ConfigValue  string // [scripts].config_value: 설정값(appl) 원격 커맨드 (gossh -script)
	InfraNet     string // [scripts].infra_net   : 인프라망 조사 대상. gossh -script "bash <이 값>" 으로 실행
	InfraRegex   string // [scripts].infra_regex : gossh 출력값(hostname: 뒤)에서 값 추출용 정규식(캡처 그룹 1)
	InfraRe      *regexp.Regexp // InfraRegex 를 컴파일한 것 (없으면 nil)
	Mounts       []MountRule
}

// LoadConfig 는 conf.toml 의 고정된 하위집합만 파싱한다.
// 지원 구문: [section], [[mountpoint]], key = "value" 또는 key = value, '#' 주석.
func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &Config{GosshBin: "gossh", GosshConc: defaultGosshConc}
	sc := bufio.NewScanner(f)
	section := ""
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[[mountpoint]]" {
			cfg.Mounts = append(cfg.Mounts, MountRule{})
			section = "mountpoint"
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		key, val, ok := splitKV(line)
		if !ok {
			return nil, fmt.Errorf("conf %d행 파싱 실패: %q", lineNo, line)
		}
		switch section {
		case "input":
			if key == "asset_file" {
				cfg.AssetFile = val
			}
		case "gossh":
			switch key {
			case "bin":
				cfg.GosshBin = val
			case "concurrency":
				n, cerr := strconv.Atoi(val)
				if cerr != nil || n <= 0 {
					return nil, fmt.Errorf("conf %d행: concurrency 는 양의 정수여야 합니다: %q", lineNo, val)
				}
				cfg.GosshConc = n
			case "timeout":
				n, cerr := strconv.Atoi(val)
				if cerr != nil || n <= 0 {
					return nil, fmt.Errorf("conf %d행: timeout 은 양의 정수(초)여야 합니다: %q", lineNo, val)
				}
				cfg.GosshTimeout = n
			case "extra_args":
				cfg.GosshArgs = val
			}
		case "scripts":
			switch key {
			case "config_value":
				cfg.ConfigValue = val
			case "infra_net":
				cfg.InfraNet = val
			case "infra_regex":
				cfg.InfraRegex = val
			}
		case "mountpoint":
			if len(cfg.Mounts) == 0 {
				return nil, fmt.Errorf("conf %d행: [[mountpoint]] 선언 전에 %q 등장", lineNo, key)
			}
			m := &cfg.Mounts[len(cfg.Mounts)-1]
			switch key {
			case "name":
				m.Name = val
			case "location":
				m.Location = val
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if cfg.AssetFile == "" {
		return nil, errors.New("conf: [input].asset_file 가 필요합니다")
	}
	if cfg.ConfigValue == "" {
		return nil, errors.New("conf: [scripts].config_value 가 필요합니다")
	}
	if cfg.InfraRegex != "" {
		re, rerr := regexp.Compile(cfg.InfraRegex)
		if rerr != nil {
			return nil, fmt.Errorf("conf: infra_regex 컴파일 실패: %w", rerr)
		}
		cfg.InfraRe = re
	}
	return cfg, nil
}

// splitKV 는 "key = value" 한 줄을 분해한다. 따옴표 문자열과 인라인 '#' 주석을 처리한다.
func splitKV(line string) (key, val string, ok bool) {
	i := strings.Index(line, "=")
	if i < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:i])
	rest := strings.TrimSpace(line[i+1:])
	if key == "" {
		return "", "", false
	}
	if rest == "" {
		return key, "", true
	}
	if rest[0] == '"' || rest[0] == '\'' {
		q := rest[0]
		j := strings.IndexByte(rest[1:], q)
		if j < 0 {
			return "", "", false
		}
		return key, rest[1 : 1+j], true
	}
	if c := strings.Index(rest, "#"); c >= 0 {
		rest = strings.TrimSpace(rest[:c])
	}
	return key, rest, true
}
