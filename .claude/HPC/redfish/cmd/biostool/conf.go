package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 는 bios.conf 의 내용입니다.
//
// 형식은 key=value 한 줄씩이며 # 로 시작하는 줄과 빈 줄은 무시합니다.
// 줄 끝 주석은 지원하지 않습니다 (값에 # 이 들어갈 수 있으므로).
// 같은 키가 여러 번 나오면 마지막 값이 적용됩니다.
// 비밀번호는 conf 에 두지 않고 pass_file(AES-256-GCM)로만 다룹니다.
type Config struct {
	User         string        // BMC 계정 ID (필수)
	PassFile     string        // 암호화된 비밀번호 파일
	KeyFile      string        // AES-256 키 파일
	Concurrency  int           // 동시 접속 호스트 수
	Timeout      time.Duration // 요청 1건당 타임아웃
	Insecure     bool          // true 면 TLS 인증서 검증 생략 (자체서명 BMC)
	Retries      int           // 일시 오류 자동 재시도 횟수
	AuthFailStop int           // AUTH_FAIL 이 이만큼 쌓이면 새 호스트 시작을 멈춘다 (0 이면 비활성, 계정 잠금 방지)
	ResultDir    string        // 결과 TSV 저장 디렉터리
	DumpDir      string        // 사전조사 덤프 저장 디렉터리
	ProfileDir   string        // 프로파일(.tsv) 디렉터리
}

func defaultConfig() Config {
	return Config{
		PassFile:     "pass.enc",
		KeyFile:      "key.bin",
		Concurrency:  100,
		Timeout:      20 * time.Second,
		Insecure:     true,
		Retries:      2,
		AuthFailStop: 3,
		ResultDir:    "results",
		DumpDir:      "dumps",
		ProfileDir:   "profiles",
	}
}

// loadConf 는 conf 파일을 읽어 기본값과 합친 뒤 필수값을 검증합니다.
func loadConf(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("설정 파일 %s 이(가) 없습니다. bios.conf.example 을 복사해 만드십시오", path)
		}
		return nil, fmt.Errorf("설정 파일 %s: %w", path, err)
	}
	return parseConf(string(data), path)
}

// parseConf 는 conf 텍스트를 해석합니다. name 은 오류 메시지용 파일명입니다.
func parseConf(text, name string) (*Config, error) {
	c := defaultConfig()
	text = strings.TrimPrefix(text, "\ufeff") // 윈도우 메모장 BOM
	for i, raw := range strings.Split(text, "\n") {
		lineNo := i + 1
		line := strings.TrimSpace(raw) // CRLF 의 \r 도 함께 제거
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("%s:%d: key=value 형식이 아닙니다", name, lineNo)
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := strings.TrimSpace(line[eq+1:])

		var err error
		switch key {
		case "user":
			c.User = val
		case "pass_file":
			setIfNotEmpty(&c.PassFile, val)
		case "key_file":
			setIfNotEmpty(&c.KeyFile, val)
		case "result_dir":
			setIfNotEmpty(&c.ResultDir, val)
		case "dump_dir":
			setIfNotEmpty(&c.DumpDir, val)
		case "profile_dir":
			setIfNotEmpty(&c.ProfileDir, val)
		case "concurrency":
			c.Concurrency, err = parseIntAtLeast(name, lineNo, key, val, 1)
		case "retries":
			c.Retries, err = parseIntAtLeast(name, lineNo, key, val, 0)
		case "auth_fail_stop":
			c.AuthFailStop, err = parseIntAtLeast(name, lineNo, key, val, 0)
		case "timeout":
			var sec int
			sec, err = parseIntAtLeast(name, lineNo, key, val, 1)
			c.Timeout = time.Duration(sec) * time.Second
		case "insecure":
			c.Insecure, err = strconv.ParseBool(val)
			if err != nil {
				err = fmt.Errorf("%s:%d: insecure 값은 true 또는 false 여야 합니다", name, lineNo)
			}
		case "pass", "password", "passwd", "pw":
			// 값은 메시지에 싣지 않는다.
			return nil, fmt.Errorf("%s:%d: 비밀번호는 conf 에 둘 수 없습니다. encrypt.sh 로 pass.enc 를 만드십시오", name, lineNo)
		default:
			return nil, fmt.Errorf("%s:%d: 알 수 없는 키 %q", name, lineNo, key)
		}
		if err != nil {
			return nil, err
		}
	}
	if c.User == "" {
		return nil, fmt.Errorf("%s: 필수 항목 user (BMC 계정 ID) 가 없습니다", name)
	}
	return &c, nil
}

func setIfNotEmpty(dst *string, val string) {
	if val != "" {
		*dst = val
	}
}

func parseIntAtLeast(name string, lineNo int, key, val string, lo int) (int, error) {
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("%s:%d: %s 값 %q 은(는) 정수가 아닙니다", name, lineNo, key, val)
	}
	if n < lo {
		return 0, fmt.Errorf("%s:%d: %s 은(는) %d 이상이어야 합니다 (현재 %d)", name, lineNo, key, lo, n)
	}
	return n, nil
}
