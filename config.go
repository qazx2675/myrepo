package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// 기본값. chunk_size 기본값은 docs/limits.md 측정 한도 x 0.5 이하로 정한다.
const (
	defaultChunkSize = 1048576 // 조각당 문자 수 (1 MiB). 본문 추정 3 MiB 상한도 함께 적용된다 (maxChunkBytes)
	defaultTimeout   = 300     // 초
	minChunkSize     = 4
)

// Config 는 설정파일 내용이다. Password 는 절대 출력하지 않는다.
type Config struct {
	Path        string
	URL         string
	Username    string
	Password    string
	PasswordEnc bool // password_enc(DPAPI)에서 읽었다
	InventoryID int
	ChunkSize   int
	Timeout     time.Duration
	VerifyTLS   bool // true 면 https 인증서를 검증한다. 기본 false: 사내 AWX 자체서명 인증서(x509 오류) 허용
}

// String 은 비밀번호를 가린 요약을 돌려준다.
func (c *Config) String() string {
	return fmt.Sprintf("url=%s username=%s password=*** inventory_id=%d chunk_size=%d timeout=%s",
		c.URL, c.Username, c.InventoryID, c.ChunkSize, c.Timeout)
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Dir(exe)
}

// etcDir 은 리눅스 설정 폴더다. histDir 은 업로드 기록 폴더다. 테스트에서 바꾼다.
var (
	etcDir  = "/etc/awx_copy"
	histDir = defaultHistDir()
)

func defaultHistDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.TempDir(), ".awx_copy")
	}
	return "/tmp/.awx_copy" // 모든 사용자가 같은 곳을 본다 (TMPDIR 무시)
}

// defaultConfigPath 는 호환용 단일 설정파일이다. 윈도우만 쓴다(실행파일 옆 awx_copy.conf).
// 리눅스는 /etc/awx_copy/{user}.conf 만 쓴다.
func defaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(exeDir(), "awx_copy.conf")
	}
	return ""
}

func loadConfig(path string) (*Config, error) {
	if path == "" {
		path = defaultConfigPath()
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("설정파일을 열 수 없습니다: %s (%v). awx_copy.conf.example 을 참고해 만드세요", path, err)
	}
	if runtime.GOOS != "windows" {
		if err := checkOwnerPerm(path, st, false); err != nil {
			return nil, fmt.Errorf("설정파일 %s 을(를) 쓸 수 없습니다: %v", path, err)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("설정파일을 열 수 없습니다: %s: %v", path, err)
	}
	defer f.Close()

	cfg := &Config{Path: path, ChunkSize: defaultChunkSize, Timeout: defaultTimeout * time.Second}
	sc := bufio.NewScanner(f)
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\uFEFF"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		i := strings.IndexAny(line, "=:")
		if i < 0 {
			return nil, fmt.Errorf("%s:%d: 'key = value' 형식이 아닙니다", path, ln)
		}
		key := strings.ToLower(strings.TrimSpace(line[:i]))
		val := strings.TrimSpace(line[i+1:])
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		switch key {
		case "url":
			var uinv int
			cfg.URL, uinv = parseAwxURL(val)
			if uinv > 0 && cfg.InventoryID == 0 {
				cfg.InventoryID = uinv
			}
		case "username":
			cfg.Username = val
		case "password":
			if cfg.Password == "" || !cfg.PasswordEnc {
				cfg.Password = val
			}
		case "password_enc": // 윈도우 DPAPI(현재 사용자) 암호문. 평문 password 보다 우선한다.
			pw, err := dpapiDecrypt(val)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: password_enc 를 풀 수 없습니다 (다른 PC/사용자가 만든 값이면 설정을 다시 저장하세요): %v", path, ln, err)
			}
			cfg.Password, cfg.PasswordEnc = pw, true
		case "inventory_id":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("%s:%d: inventory_id 는 양의 정수여야 합니다", path, ln)
			}
			cfg.InventoryID = n
		case "verify_tls":
			cfg.VerifyTLS = val == "true" || val == "1" || val == "yes"
		case "chunk_size":
			n, err := strconv.Atoi(val)
			if err != nil || n < minChunkSize {
				return nil, fmt.Errorf("%s:%d: chunk_size 는 %d 이상 정수여야 합니다", path, ln, minChunkSize)
			}
			cfg.ChunkSize = n // 평문 문자 수 (v2)
		case "timeout":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("%s:%d: timeout 은 양의 정수(초)여야 합니다", path, ln)
			}
			cfg.Timeout = time.Duration(n) * time.Second
		default:
			return nil, fmt.Errorf("%s:%d: 알 수 없는 키 %q", path, ln, key)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("설정파일 읽기 오류: %v", err)
	}
	var miss []string
	if cfg.URL == "" {
		miss = append(miss, "url")
	}
	if cfg.Username == "" {
		miss = append(miss, "username")
	}
	if cfg.Password == "" {
		miss = append(miss, "password")
	}
	if cfg.InventoryID == 0 {
		miss = append(miss, "inventory_id")
	}
	if len(miss) > 0 {
		return nil, fmt.Errorf("설정파일 %s 에 값이 없습니다: %s", path, strings.Join(miss, ", "))
	}
	return cfg, nil
}

// parseAwxURL 은 사용자가 브라우저 주소를 그대로 붙여 넣은 경우를 처리한다.
// 예: https://host/awx/#/inventories/inventory/33/details → base https://host/awx, 인벤토리 33.
// '#' 뒤(화면 경로)는 버리고, 끝의 /api, /api/v2 도 걷어 낸다.
func parseAwxURL(u string) (string, int) {
	u = strings.TrimSpace(u)
	inv := 0
	if i := strings.Index(u, "#"); i >= 0 {
		frag := u[i+1:]
		u = u[:i]
		if j := strings.Index(frag, "inventor"); j >= 0 {
			parts := strings.Split(frag[j:], "/")
			for k := 0; k+1 < len(parts); k++ {
				if strings.HasPrefix(parts[k], "inventor") {
					if n, err := strconv.Atoi(parts[k+1]); err == nil && n > 0 {
						inv = n
						break
					}
				}
			}
		}
	}
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimRight(u, "/")
	for _, suf := range []string{"/api/v2", "/api"} {
		u = strings.TrimSuffix(u, suf)
	}
	return strings.TrimRight(u, "/"), inv
}
