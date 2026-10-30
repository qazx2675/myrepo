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
	User2        string        // 두 번째 공용 계정 ID (선택). 계정 생성에서만 쓰고 점검 접속에는 쓰지 않는다
	Pass2File    string        // 두 번째 계정의 암호화된 비밀번호 파일 (기본 pass2.enc, key_file 로 복호화)
	RFEncFile    string        // 2순위(rf.conf 벤더 admin) 계정의 암호화된 비밀번호 파일 (기본 rf.enc, key_file 로 복호화)
	User2Role    string        // 두 번째 계정 권한: Administrator | Operator | ReadOnly (user2 가 있으면 필수, 벤더별 값이 없을 때의 기본)
	Concurrency  int           // 동시 접속 호스트 수
	Timeout      time.Duration // 요청 1건당 타임아웃
	Insecure     bool          // true 면 TLS 인증서 검증 생략 (자체서명 BMC)
	Retries      int           // 일시 오류 자동 재시도 횟수
	AuthFailStop int           // AUTH_FAIL 이 이만큼 쌓이면 새 호스트 시작을 멈춘다 (0 이면 비활성, 계정 잠금 방지)
	ResultDir    string        // 결과 TSV 저장 디렉터리
	DumpDir      string        // 사전조사 덤프 저장 디렉터리
	ProfileDir   string        // 프로파일(.tsv) 디렉터리
	ExcludeAttrs []string      // 점검·전체비교에서 뺄 속성 패턴 (| 구분, * 와일드카드, 대소문자 무시). 비면 없음
	AllcheckSet  bool          // true 면 전체비교 difference 도 설정 대상에 넣는다 (기본 false)
	RefsDir      string        // 기준 속성 JSON(bios_export / -export-attrs 결과) 폴더
	AutoProfile  string        // 비대화형(bios -auto / -auto-check)에서 -profile 이 없을 때 쓸 프로파일 이름. 비면 없음
	ProtectAttrs []string      // 절대 설정하지 않을 속성 패턴 추가분 (| 구분, * 와일드카드, 대소문자 무시). 내장 보호 목록(protect.go)에 더하기만 함
	// XMLHostPrefix 는 bios 에서 XML API(UCSM/CIMC, 읽기 전용)로 점검할 hostname 접두어입니다 (소문자, 쉼표 구분, 기본 c).
	// 비면(xml_host_prefix=) 이름으로는 XML 로 보내지 않습니다 (-user-xml 목록만 XML). IP 대상은 접두어와 관계없이 Redfish.
	XMLHostPrefix []string
	// User2Roles 는 벤더별 두 번째 계정 권한입니다 (user2_role_<vendor>, 키는 fallbackVendors 소문자). 없는 벤더·모르는 벤더는 User2Role.
	User2Roles map[string]string
}

func defaultConfig() Config {
	return Config{
		PassFile:      "pass.enc",
		KeyFile:       "key.bin",
		Pass2File:     "pass2.enc",
		RFEncFile:     "rf.enc",
		Concurrency:   100,
		Timeout:       20 * time.Second,
		Insecure:      true,
		Retries:       2,
		AuthFailStop:  3,
		ResultDir:     "results",
		DumpDir:       "dumps",
		ProfileDir:    "profiles",
		RefsDir:       "refs",
		XMLHostPrefix: []string{"c"},
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
		case "user2":
			c.User2 = val
		case "pass2_file":
			setIfNotEmpty(&c.Pass2File, val)
		case "rf_enc_file":
			setIfNotEmpty(&c.RFEncFile, val)
		case "user2_role":
			if c.User2Role = acctRoleName(val); val != "" && c.User2Role == "" {
				err = fmt.Errorf("%s:%d: user2_role 은 Administrator, Operator, ReadOnly 중 하나여야 합니다", name, lineNo)
			}
		case "result_dir":
			setIfNotEmpty(&c.ResultDir, val)
		case "dump_dir":
			setIfNotEmpty(&c.DumpDir, val)
		case "profile_dir":
			setIfNotEmpty(&c.ProfileDir, val)
		case "refs_dir":
			setIfNotEmpty(&c.RefsDir, val)
		case "auto_profile":
			if val != "" {
				if err = validProfileName(val); err != nil {
					err = fmt.Errorf("%s:%d: auto_profile: %w", name, lineNo, err)
				}
			}
			c.AutoProfile = val
		case "exclude_attrs":
			c.ExcludeAttrs = splitExclude(val)
		case "protect_attrs":
			c.ProtectAttrs = splitExclude(val)
		case "xml_host_prefix":
			c.XMLHostPrefix = splitPrefixes(val)
		case "allcheck_set":
			c.AllcheckSet, err = parseOnOff(name, lineNo, key, val)
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
			v := strings.TrimPrefix(key, "user2_role_")
			if v == key {
				return nil, fmt.Errorf("%s:%d: 알 수 없는 키 %q", name, lineNo, key)
			}
			err = c.setUser2VendorRole(name, lineNo, key, v, val)
		}
		if err != nil {
			return nil, err
		}
	}
	if c.User == "" {
		return nil, fmt.Errorf("%s: 필수 항목 user (BMC 계정 ID) 가 없습니다", name)
	}
	if c.User2 != "" {
		if c.User2 == c.User {
			return nil, fmt.Errorf("%s: user2 는 user 와 달라야 합니다", name)
		}
		if c.User2Role == "" {
			return nil, fmt.Errorf("%s: user2 를 쓰려면 user2_role (Administrator, Operator, ReadOnly 중 하나) 이 필요합니다", name)
		}
	}
	return &c, nil
}

// setUser2VendorRole 은 user2_role_<vendor> 한 줄을 읽습니다. 벤더는 rf.conf 와 같은 fallbackVendors, 값은 user2_role 과 같은
// 세 가지입니다. 빈 값이면 그 벤더 설정을 지웁니다 (user2_role= 과 같음).
func (c *Config) setUser2VendorRole(name string, lineNo int, key, vendor, val string) error {
	known := false
	for _, v := range fallbackVendors {
		known = known || v == vendor
	}
	if !known {
		return fmt.Errorf("%s:%d: %s 의 벤더는 %s 중 하나여야 합니다", name, lineNo, key, strings.Join(fallbackVendors, ", "))
	}
	if val == "" {
		delete(c.User2Roles, vendor)
		return nil
	}
	r := acctRoleName(val)
	if r == "" {
		return fmt.Errorf("%s:%d: %s 은 Administrator, Operator, ReadOnly 중 하나여야 합니다", name, lineNo, key)
	}
	if c.User2Roles == nil {
		c.User2Roles = map[string]string{}
	}
	c.User2Roles[vendor] = r
	return nil
}

// user2RoleFor 는 벤더(아무 표기, fallbackVendorKey 로 바꿈)에 적용할 두 번째 계정 권한입니다:
// user2_role_<vendor> 가 있으면 그 값, 없거나 모르는 벤더면 user2_role.
func (c *Config) user2RoleFor(vendor string) string {
	if r := c.User2Roles[fallbackVendorKey(vendor)]; r != "" {
		return r
	}
	return c.User2Role
}

// user2RolesText 는 설정 요약용 "기본권한, 벤더=권한 ..." 입니다 (벤더별 값이 없으면 기본 권한만).
func (c *Config) user2RolesText() string {
	parts := []string{c.User2Role}
	for _, v := range fallbackVendors {
		if r := c.User2Roles[v]; r != "" {
			parts = append(parts, v+"="+r)
		}
	}
	return strings.Join(parts, ", ")
}

func setIfNotEmpty(dst *string, val string) {
	if val != "" {
		*dst = val
	}
}

// splitExclude 는 exclude_attrs 값을 | 로 나눕니다 (앞뒤 공백·빈 칸 제거). 빈 값이면 nil.
func splitExclude(val string) []string {
	var out []string
	for _, p := range strings.Split(val, "|") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitPrefixes 는 xml_host_prefix 값을 쉼표로 나눠 소문자로 돌려줍니다 (앞뒤 공백·빈 칸 제거). 빈 값이면 nil.
func splitPrefixes(val string) []string {
	var out []string
	for _, p := range strings.Split(val, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseOnOff 는 on/off 값을 읽습니다 (대소문자 무시).
func parseOnOff(name string, lineNo int, key, val string) (bool, error) {
	switch strings.ToLower(val) {
	case "on":
		return true, nil
	case "off":
		return false, nil
	}
	return false, fmt.Errorf("%s:%d: %s 값은 on 또는 off 여야 합니다", name, lineNo, key)
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
