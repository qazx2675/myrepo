package main

// conf 파서·user 결정·conf 탐색 규칙 (awxkit config 패키지에서 필요한 부분만 복사)

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TemplateConf 는 템플릿 하나(cpu / stress)의 설정이다.
type TemplateConf struct {
	Template    string // AWX job template ID 또는 이름
	HostnameKey string // hostname 목록(줄바꿈 텍스트)을 넘길 survey 변수명 (비우면 넘기지 않음 = 인벤토리 전체 대상)
	ExtraVars   string // "key=value, key2=value2"
}

// defaultEndKeys 는 stress-ng 종료 시각 survey 변수명 기본값(연,월,일,시,분 순)이다.
var defaultEndKeys = []string{"end_year", "end_month", "end_day", "end_hour", "end_min"}

// Config 는 ${user}_setting.conf 의 값을 담는다.
type Config struct {
	AWXURL      string
	Username    string
	Password    string
	InsecureTLS bool

	Gossh string

	Repohost string
	RepoDir  string
	SvrDir   string
	GitDir   string // 01 의 git_upload: 비우면 생략, ${user} 는 사용자로 치환

	InventoryDeleteHost string // 01 의 inventory_delete: 첫 등록 전에 ssh <host> "bash /root/server/delhost_<user>" 1번. 비우면 생략

	InventorySource string
	Inventory       string
	SourceField     string

	CPU    TemplateConf
	Stress TemplateConf

	DefaultInventory string   // 평소 템플릿 인벤토리 이름 (실행 때 username 인벤토리로 바꿨다가 되돌림)
	StressEndKeys    []string // stress-ng 종료 시각 survey 변수명 5개 (연,월,일,시,분)

	MonitorSec      int
	MonitorInterval int
	LoadThreshold   float64

	OutputDir    string
	PollInterval int
	HistoryFile  string

	SourcePath string
}

func (c *Config) setters() map[string]func(string) error {
	str := func(p *string) func(string) error { return func(v string) error { *p = v; return nil } }
	num := func(key string, p *int) func(string) error {
		return func(v string) error {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fmt.Errorf("%s 는 1 이상의 정수여야 합니다: %q", key, v)
			}
			*p = n
			return nil
		}
	}
	return map[string]func(string) error{
		"awx_url":      str(&c.AWXURL),
		"username":     str(&c.Username),
		"password":     str(&c.Password),
		"insecure_tls": func(v string) error { c.InsecureTLS = parseBool(v); return nil },

		"gossh": str(&c.Gossh),

		"repohost": str(&c.Repohost),
		"repo_dir": str(&c.RepoDir),
		"svr_dir":  str(&c.SvrDir),
		"git_dir":  str(&c.GitDir),

		"inventory_delete_host": str(&c.InventoryDeleteHost),

		"inventory_source": str(&c.InventorySource),
		"inventory":        str(&c.Inventory),
		"source_field":     str(&c.SourceField),

		"cpu_template":        str(&c.CPU.Template),
		"cpu_hostname_key":    str(&c.CPU.HostnameKey),
		"cpu_extra_vars":      str(&c.CPU.ExtraVars),
		"stress_template":     str(&c.Stress.Template),
		"stress_hostname_key": str(&c.Stress.HostnameKey),
		"stress_extra_vars":   str(&c.Stress.ExtraVars),
		"default_inventory":   str(&c.DefaultInventory),
		"stress_end_keys": func(v string) error {
			var keys []string
			for _, k := range strings.Split(v, ",") {
				if k = strings.TrimSpace(k); k == "" {
					return fmt.Errorf("stress_end_keys 에 빈 항목이 있습니다: %q", v)
				}
				keys = append(keys, k)
			}
			if len(keys) != 5 {
				return fmt.Errorf("stress_end_keys 는 연,월,일,시,분 순서로 5개여야 합니다 (%d개): %q", len(keys), v)
			}
			c.StressEndKeys = keys
			return nil
		},

		"monitor_sec":      num("monitor_sec", &c.MonitorSec),
		"monitor_interval": num("monitor_interval", &c.MonitorInterval),
		"load_threshold": func(v string) error {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f <= 0 {
				return fmt.Errorf("load_threshold 는 0 보다 큰 숫자여야 합니다: %q", v)
			}
			c.LoadThreshold = f
			return nil
		},
		"output_dir":    str(&c.OutputDir),
		"poll_interval": num("poll_interval", &c.PollInterval),
		"history_file":  str(&c.HistoryFile),
	}
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "y", "1", "on":
		return true
	}
	return false
}

// parseConfig 는 "key = value" 형식(# 이후 주석)의 conf 본문을 읽는다. 모르는 키는 무시한다.
func parseConfig(text, path string) (*Config, error) {
	c := &Config{
		RepoDir:         "/root/Inventory",
		SourceField:     "source_path",
		MonitorSec:      120,
		MonitorInterval: 10,
		LoadThreshold:   3,
		OutputDir:       "./output",
		PollInterval:    3,
		HistoryFile:     "./stress-test_history.log",
		SourcePath:      path,

		DefaultInventory: "DEFAULT",
		StressEndKeys:    append([]string(nil), defaultEndKeys...),
	}
	set := c.setters()
	sc := bufio.NewScanner(strings.NewReader(strings.TrimPrefix(text, "\xef\xbb\xbf")))
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
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
		if strings.HasPrefix(key, "cpu_intel_") || strings.HasPrefix(key, "cpu_amd_") {
			return nil, fmt.Errorf("%s:%d: %s 키는 없어졌습니다 — cpu_template 하나로 바뀌었습니다 (cpu_template, cpu_hostname_key, cpu_extra_vars 로 옮기고 cpu_intel_*/cpu_amd_* 줄은 지우세요. bash setup.sh 로 다시 저장해도 됩니다)", path, lineNo, key)
		}
		if s, ok := set[key]; ok && val != "" {
			if err := s(val); err != nil {
				return nil, fmt.Errorf("%s:%d: %v", path, lineNo, err)
			}
		}
	}
	return c, sc.Err()
}

// loadConfig 는 conf 파일을 읽어 파싱한다.
func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일을 열 수 없습니다 (%s): %w", path, err)
	}
	return parseConfig(string(b), path)
}

// validate 는 선택한 단계에 꼭 필요한 키가 비어 있지 않은지 확인한다.
// *_hostname_key 는 선택(비우면 hostname 목록을 넘기지 않음).
func (c *Config) validate(steps string) error {
	var miss []string
	need := func(key, v string) {
		if strings.TrimSpace(v) == "" {
			miss = append(miss, key)
		}
	}
	need("awx_url", c.AWXURL)
	need("username", c.Username)
	need("password", c.Password)
	need("repohost", c.Repohost)
	need("repo_dir", c.RepoDir)
	need("svr_dir", c.SvrDir)
	if c.InventorySource == "" && c.Inventory == "" {
		miss = append(miss, "inventory_source 또는 inventory")
	}
	need("default_inventory", c.DefaultInventory)
	if strings.Contains(steps, "a") {
		need("cpu_template", c.CPU.Template)
	}
	if strings.Contains(steps, "b") {
		need("stress_template", c.Stress.Template)
	}
	if len(miss) > 0 {
		return fmt.Errorf("설정 파일(%s)에 비어 있는 값이 있습니다: %s", c.SourcePath, strings.Join(miss, ", "))
	}
	for _, t := range []struct {
		name string
		tc   TemplateConf
	}{{"cpu", c.CPU}, {"stress", c.Stress}} {
		if _, err := parseKeyValues(t.tc.ExtraVars); err != nil {
			return fmt.Errorf("설정 파일(%s)의 %s_extra_vars %v", c.SourcePath, t.name, err)
		}
	}
	return nil
}

var userRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// resolveUser 는 -user 플래그 → STRESS_USER 환경변수 → $USER 순으로 사용자를 정한다.
func resolveUser(flagUser string) string {
	if flagUser != "" {
		return flagUser
	}
	if u := os.Getenv("STRESS_USER"); u != "" {
		return u
	}
	return os.Getenv("USER")
}

// resolveConfPath 는 -conf 가 있으면 그 경로를, 없으면
// ./conf → ~/.stress-test → <바이너리 위치>/conf 순으로 ${user}_setting.conf 를 찾는다.
func resolveConfPath(explicit, user string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("지정한 설정 파일이 없습니다: %s", explicit)
		}
		return explicit, nil
	}
	name := user + "_setting.conf"
	cands := []string{filepath.Join(".", "conf", name)}
	if home, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(home, ".stress-test", name))
	}
	if exe, err := os.Executable(); err == nil {
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		cands = append(cands, filepath.Join(filepath.Dir(exe), "conf", name))
	}
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s 파일을 찾지 못했습니다 (확인한 경로: %s)", name, strings.Join(cands, ", "))
}

// parseKeyValues 는 "key=value, key2=value2" 형태의 값을 map 으로 나눈다.
func parseKeyValues(raw string) (map[string]string, error) {
	res := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, v, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("형식이 올바르지 않습니다 (key=value): %q", pair)
		}
		res[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return res, nil
}

// appendHistory 는 실행 이력을 history_file 에 한 줄 추가한다.
func appendHistory(c *Config, line string) {
	if c == nil || c.HistoryFile == "" {
		return
	}
	f, err := os.OpenFile(c.HistoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Printf("%s 이력 파일 기록 실패 (%s): %v\n", markWarn, c.HistoryFile, err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), line)
}
