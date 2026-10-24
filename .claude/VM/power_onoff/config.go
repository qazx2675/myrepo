package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	userNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)
	hostNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// defaultConfig 는 기본값이 채워진 설정을 돌려준다.
func defaultConfig() *Config {
	return &Config{
		VCID:             "lscsystems@vsphere.local",
		OS6User:          "root",
		Concurrency:      200,
		ShutdownTimeout:  300 * time.Second,
		BootTimeout:      600 * time.Second,
		BootPollInterval: 10 * time.Second,
		PortCheckTimeout: 3 * time.Second,
		CrossCheckBin:    DefaultCrossCheckBin,
	}
}

// splitList 는 콤마 목록을 공백 제거 후 나눈다. 빈 항목/중복은 오류.
func splitList(val string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(val, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("목록에 빈 항목이 있습니다")
		}
		if seen[p] {
			return nil, fmt.Errorf("목록에 중복 항목이 있습니다: %s", p)
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, nil
}

func parsePositive(val string) (int, error) {
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("1 이상의 정수여야 합니다: %q", val)
	}
	return n, nil
}

// ParseConfig 는 pwonoff.conf 내용을 해석한다. 오류에는 줄 번호가 붙는다.
func ParseConfig(r io.Reader) (*Config, error) {
	cfg := defaultConfig()
	seen := map[string]int{}
	sc := bufio.NewScanner(r)
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 줄 끝 주석(값 뒤 '#')은 버린다
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("설정 %d번째 줄: '키=값' 형식이 아닙니다: %q", ln, line)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("설정 %d번째 줄: 중복된 키 %q (처음 %d번째 줄)", ln, key, prev)
		}
		bad := func(err error) (*Config, error) {
			return nil, fmt.Errorf("설정 %d번째 줄: %s: %v", ln, key, err)
		}
		switch key {
		case "allowed_dir":
			if !filepath.IsAbs(val) {
				return bad(fmt.Errorf("절대경로여야 합니다: %q", val))
			}
			cfg.AllowedDir = filepath.Clean(val)
		case "users":
			list, err := splitList(val)
			if err != nil {
				return bad(err)
			}
			for _, u := range list {
				if !userNameRe.MatchString(u) {
					return bad(fmt.Errorf("잘못된 user 이름 %q (영문 소문자/숫자/_ 만)", u))
				}
			}
			cfg.Users = list
		case "vcenters":
			list, err := splitList(val)
			if err != nil {
				return bad(err)
			}
			cfg.VCenters = list
		case "vc_id":
			if val == "" {
				return bad(fmt.Errorf("비어 있습니다"))
			}
			cfg.VCID = val
		case "secret_lib":
			cfg.SecretLib = val
		case "os6_host":
			if val != "" && !hostNameRe.MatchString(val) {
				return bad(fmt.Errorf("허용되지 않는 문자가 있습니다: %q", val))
			}
			cfg.OS6Host = val
		case "os6_user":
			if val != "" && !hostNameRe.MatchString(val) {
				return bad(fmt.Errorf("허용되지 않는 문자가 있습니다: %q", val))
			}
			if val != "" {
				cfg.OS6User = val
			}
		case "cross_check":
			switch val {
			case "on":
				cfg.CrossCheck = true
			case "off":
				cfg.CrossCheck = false
			default:
				return bad(fmt.Errorf("on 또는 off 여야 합니다: %q", val))
			}
		case "cross_check_bin":
			// 비우면 기본 경로를 쓰고, 값을 주면 절대경로여야 한다
			if val != "" {
				if !filepath.IsAbs(val) {
					return bad(fmt.Errorf("절대경로여야 합니다: %q", val))
				}
				cfg.CrossCheckBin = filepath.Clean(val)
			}
		case "concurrency":
			n, err := parsePositive(val)
			if err != nil {
				return bad(err)
			}
			cfg.Concurrency = n
		case "shutdown_timeout", "boot_timeout", "boot_poll_interval", "port_check_timeout":
			n, err := parsePositive(val)
			if err != nil {
				return bad(err)
			}
			d := time.Duration(n) * time.Second
			switch key {
			case "shutdown_timeout":
				cfg.ShutdownTimeout = d
			case "boot_timeout":
				cfg.BootTimeout = d
			case "boot_poll_interval":
				cfg.BootPollInterval = d
			default:
				cfg.PortCheckTimeout = d
			}
		default:
			return nil, fmt.Errorf("설정 %d번째 줄: 알 수 없는 키 %q", ln, key)
		}
		seen[key] = ln
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("설정 읽기 실패: %v", err)
	}
	if cfg.AllowedDir == "" {
		return nil, fmt.Errorf("설정 오류: allowed_dir 이 필요합니다")
	}
	if len(cfg.Users) == 0 {
		return nil, fmt.Errorf("설정 오류: users 가 비어 있습니다")
	}
	return cfg, nil
}

// LoadConfig 는 보안 검사(root 소유 등) 후 설정 파일을 읽는다.
func LoadConfig(path string) (*Config, error) { return loadConfig(path, true) }

// loadConfig 는 requireRoot=false 로 소유자 검사를 끌 수 있다(시험용).
func loadConfig(path string, requireRoot bool) (*Config, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일을 열 수 없습니다: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("설정 파일이 심볼릭 링크입니다: %s", path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("설정 파일이 일반 파일이 아닙니다: %s", path)
	}
	if fi.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("설정 파일에 group/other 쓰기 권한이 있습니다 (%04o): %s", fi.Mode().Perm(), path)
	}
	if requireRoot {
		if err := checkOwnerRoot(fi, path); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("설정 파일을 열 수 없습니다: %v", err)
	}
	defer f.Close()
	return ParseConfig(f)
}

// CheckAllowedDir 는 현재 디렉터리가 allowed_dir 과 같은지 확인한다.
func CheckAllowedDir(cfg *Config) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("현재 디렉터리를 알 수 없습니다: %v", err)
	}
	return checkAllowedDirAt(cfg.AllowedDir, cwd)
}

// checkAllowedDirAt 은 심볼릭 링크를 해석한 뒤 allowed 와 cwd 를 비교한다.
func checkAllowedDirAt(allowed, cwd string) error {
	a, errA := filepath.EvalSymlinks(allowed)
	c, errC := filepath.EvalSymlinks(cwd)
	if errA != nil || errC != nil || filepath.Clean(a) != filepath.Clean(c) {
		return fmt.Errorf("이 명령은 %s 에서만 실행할 수 있습니다 (현재: %s)", allowed, cwd)
	}
	return nil
}
