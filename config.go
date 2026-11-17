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
	InventoryID int
	ChunkSize   int
	Timeout     time.Duration
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
			cfg.URL = strings.TrimRight(val, "/")
		case "username":
			cfg.Username = val
		case "password":
			cfg.Password = val
		case "inventory_id":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("%s:%d: inventory_id 는 양의 정수여야 합니다", path, ln)
			}
			cfg.InventoryID = n
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
