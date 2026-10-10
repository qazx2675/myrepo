package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
)

// fallback.go 는 "2순위(예비) 계정" 기능입니다.
//
// 공용(1순위) 계정이 일부 BMC 에 없을 때를 위해, 1순위 로그인이 AUTH_FAIL(401·로그인 403)일 때만
// 비인증 ServiceRoot 로 BMC 벤더를 식별하고 rf.conf 에 적힌 그 벤더의 예비 계정으로 정확히 1회 다시 로그인합니다.
//   - 1순위 성공: 기존과 같음 (ServiceRoot 비인증 조회도, 예비 계정 조회도 하지 않음)
//   - AUTH_FAIL 이 아닌 실패(UNREACHABLE·TIMEOUT·BMC_ERROR·HTTP_ERROR 등): 예비 계정을 시도하지 않음
//   - 벤더를 식별하지 못하거나 rf.conf 에 그 벤더 계정이 없으면 시도하지 않음 (다른 벤더 계정을 돌려가며 시험하지 않음)
//   - 예비 계정 로그인은 Retries=0 (재시도 없음) → 같은 BMC 에 로그인 시도는 최대 2회
//
// rf.conf 는 평문이지만 비밀번호는 클로저 안에만 두고 어떤 출력·로그·결과 파일·오류 메시지에도 싣지 않습니다.

// rf.conf 경로: 플래그 -rf-conf > 환경변수 RF_TOOL_CONF > 기본값.
const (
	defaultRFConf = "/etc/rf_tool/rf.conf"
	envRFConf     = "RF_TOOL_CONF"
)

// 로그인에 성공한 계정 출처 (hostCheck.CredSource).
const (
	credPrimary  = "primary"
	credFallback = "fallback"
)

// fallbackVendors 는 rf.conf 키의 벤더 이름(소문자)입니다. normalizeVendor 결과를 소문자로 바꾼 값과 같습니다.
var fallbackVendors = []string{"dell", "hpe", "lenovo", "supermicro", "cisco"}

type fallbackAcct struct{ user, pass string }

// fallbackCreds 는 rf.conf 의 벤더별 예비 계정입니다. 계정은 클로저 안에만 두어
// fmt 로 구조체를 통째로 찍어도 함수 주소만 나옵니다 (redfish.go 의 credentials 와 같은 방식).
type fallbackCreds struct {
	accts func() map[string]fallbackAcct // 소문자 벤더 → 계정 (user 가 빈 벤더는 없음)
}

// lookup 은 벤더(소문자 키)의 예비 계정입니다.
func (f *fallbackCreds) lookup(vendor string) (user, pass string, ok bool) {
	if f == nil {
		return "", "", false
	}
	a, ok := f.accts()[vendor]
	return a.user, a.pass, ok
}

// vendors 는 예비 계정이 있는 벤더 목록입니다 (fallbackVendors 순서, 계정명 없음).
func (f *fallbackCreds) vendors() []string {
	var out []string
	if f == nil {
		return out
	}
	m := f.accts()
	for _, v := range fallbackVendors {
		if _, ok := m[v]; ok {
			out = append(out, v)
		}
	}
	return out
}

// rfConfPath 는 사용할 rf.conf 경로와 그것이 명시(플래그·환경변수)된 것인지를 돌려줍니다.
func rfConfPath(flagVal string) (string, bool) {
	if flagVal != "" {
		return flagVal, true
	}
	if v := os.Getenv(envRFConf); v != "" {
		return v, true
	}
	return defaultRFConf, false
}

// loadRFConf 는 rf.conf 를 읽습니다. 파일이 없으면 (nil, "", nil) — 기능 꺼짐.
// 읽을 수 없거나 형식 오류면 오류입니다. warn 은 그룹/기타 권한이 열려 있을 때의 경고 한 줄입니다 (비밀번호 없음).
// rfConfMaxBytes 는 rf.conf 크기 상한입니다 (벤더 5개 계정에는 충분).
const rfConfMaxBytes = 64 * 1024

// reRFKeyShape 는 rf.conf 의 키 모양(소문자·숫자·밑줄)입니다. 오류 메시지에 키를 보여 줄지 정할 때만 씁니다.
var reRFKeyShape = regexp.MustCompile(`^[a-z0-9]{1,16}(_[a-z0-9]{1,16})?$`)

func loadRFConf(path string) (fb *fallbackCreds, warn string, err error) {
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("2순위 계정 파일 %s: %w", path, err)
	}
	if !st.Mode().IsRegular() {
		return nil, "", fmt.Errorf("2순위 계정 파일 %s 은(는) 일반 파일이 아닙니다", path)
	}
	if st.Size() > rfConfMaxBytes {
		return nil, "", fmt.Errorf("2순위 계정 파일 %s 이(가) 너무 큽니다 (%d 바이트 초과)", path, rfConfMaxBytes)
	}
	if st.IsDir() {
		return nil, "", fmt.Errorf("2순위 계정 파일 %s 이(가) 폴더입니다", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("2순위 계정 파일 %s 을(를) 읽을 수 없습니다: %w", path, err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
		warn = fmt.Sprintf("2순위 계정 파일 %s 권한이 그룹/기타에 열려 있습니다 (%04o). chmod 600 을 권장합니다", path, st.Mode().Perm())
	}
	fb, err = parseRFConf(string(data), path)
	return fb, warn, err
}

// parseRFConf 는 rf.conf 텍스트를 해석합니다 (순수 함수). 형식은 bios.conf 와 같은 key=value, # 주석.
// 키는 <vendor>_user, <vendor>_pass (vendor 는 fallbackVendors, 대소문자 무시)이고 그 밖의 키는 오류입니다.
// user 가 빈 벤더는 예비 계정이 없습니다. user 가 있는데 pass 가 비면 오류입니다.
// 오류 메시지에는 키 이름과 줄 번호만 넣고 값은 넣지 않습니다.
func parseRFConf(text, name string) (*fallbackCreds, error) {
	known := map[string]bool{}
	for _, v := range fallbackVendors {
		known[v] = true
	}
	users, passes := map[string]string{}, map[string]string{}
	text = strings.TrimPrefix(text, "\xef\xbb\xbf") // 윈도우 메모장 BOM
	for i, raw := range strings.Split(text, "\n") {
		lineNo := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("%s:%d: key=value 형식이 아닙니다", name, lineNo)
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		val := strings.TrimSpace(line[eq+1:])
		vendor, field := key, ""
		if j := strings.LastIndex(key, "_"); j > 0 {
			vendor, field = key[:j], key[j+1:]
		}
		if !known[vendor] || (field != "user" && field != "pass") {
			shown := "(표시 안 함)" // 키 자리에 비밀번호가 섞였을 수 있어 키 모양일 때만 이름을 보여 준다
			if reRFKeyShape.MatchString(key) {
				shown = fmt.Sprintf("%q", key)
			}
			return nil, fmt.Errorf("%s:%d: 알 수 없는 키 %s (<vendor>_user, <vendor>_pass 만, vendor=%s)", name, lineNo, shown, strings.Join(fallbackVendors, ","))
		}
		if field == "user" {
			users[vendor] = val
		} else {
			passes[vendor] = val
		}
	}
	m := map[string]fallbackAcct{}
	for _, v := range fallbackVendors {
		if users[v] == "" {
			continue
		}
		if passes[v] == "" {
			return nil, fmt.Errorf("%s: %s_user 가 있는데 %s_pass 가 비어 있습니다", name, v, v)
		}
		m[v] = fallbackAcct{user: users[v], pass: passes[v]}
	}
	return &fallbackCreds{accts: func() map[string]fallbackAcct { return m }}, nil
}

// fallbackVendorKey 는 벤더 문자열을 rf.conf 벤더 키(소문자)로 바꿉니다. 5개 벤더가 아니면 "".
func fallbackVendorKey(s string) string {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "hp") { // HPE iLO4 의 Oem 키
		return "hpe"
	}
	k := strings.ToLower(normalizeVendor(s))
	for _, v := range fallbackVendors {
		if k == v {
			return v
		}
	}
	return ""
}

// vendorFromServiceRoot 는 비인증 ServiceRoot 응답에서 벤더 키를 찾습니다 (순수 함수).
// Vendor → Oem 키(5개 벤더 중 정확히 하나일 때만) → Product → Name 순서로 보고, 식별하지 못하면 "".
func vendorFromServiceRoot(b []byte) string {
	var root struct {
		Vendor, Product, Name string
		Oem                   map[string]json.RawMessage
	}
	if json.Unmarshal(b, &root) != nil {
		return ""
	}
	if v := fallbackVendorKey(root.Vendor); v != "" {
		return v
	}
	oem := ""
	for k := range root.Oem {
		v := fallbackVendorKey(k)
		if v == "" {
			continue
		}
		if oem != "" && oem != v {
			return "" // 여러 벤더 Oem → 판단하지 않음
		}
		oem = v
	}
	if oem != "" {
		return oem
	}
	if v := fallbackVendorKey(root.Product); v != "" {
		return v
	}
	return fallbackVendorKey(root.Name)
}

// vendorLabel 은 벤더 키의 표시 이름입니다 (dell → Dell).
func vendorLabel(key string) string { return normalizeVendor(key) }

// loginInfo 는 loginWithFallback 의 결과 정보입니다 (계정명·비밀번호 없음).
type loginInfo struct {
	Source string    // credPrimary / credFallback / "" (로그인 실패)
	Vendor string    // 2순위 계정을 시도한 벤더 키 (시도하지 않았으면 "")
	prior  CallStats // 2순위로 바꿨을 때 1순위 Client 가 쓴 호출 수
}

// stats 는 1순위·2순위 Client 의 호출 수를 합친 값입니다 (AuthMode 는 최종 Client 기준).
func (li loginInfo) stats(c *Client) CallStats {
	s := c.Stats()
	s.Gets += li.prior.Gets
	s.Posts += li.prior.Posts
	s.Patches += li.prior.Patches
	s.Deletes += li.prior.Deletes
	s.Sessions += li.prior.Sessions
	s.Fallback = li.Source == credFallback
	return s
}

// loginWithFallback 은 o(1순위 계정)로 로그인하고, AUTH_FAIL 이면 fb 의 벤더 예비 계정으로 1회 다시 로그인합니다.
// 돌려주는 Client 는 항상 nil 이 아니며(실패해도) 호출자가 Close 합니다. err 는 최종 로그인 오류입니다.
// fb 가 nil(rf.conf 없음)이면 NewClient(o)+Login() 과 완전히 같습니다.
// 2순위 Client 는 TLS·타임아웃·간격·모드가 1순위와 같고 Retries 만 0 입니다.
func loginWithFallback(o ClientOpts, fb *fallbackCreds) (*Client, loginInfo, error) {
	c := NewClient(o)
	err := c.Login()
	if err == nil {
		return c, loginInfo{Source: credPrimary}, nil
	}
	if fb == nil {
		return c, loginInfo{}, err
	}
	var rf *RFError
	if !errors.As(err, &rf) || rf.Status != StatusAuthFail {
		return c, loginInfo{}, err
	}
	const primary = "1순위 계정 인증 실패"
	if len(fb.vendors()) == 0 {
		return c, loginInfo{}, &RFError{Status: StatusAuthFail, HTTP: rf.HTTP, Detail: primary + " — rf.conf 에 예비 계정 없음"}
	}
	vendor := ""
	if b, gerr := c.ServiceRootNoAuth(); gerr == nil {
		vendor = vendorFromServiceRoot(b)
	}
	if vendor == "" {
		return c, loginInfo{}, &RFError{Status: StatusAuthFail, HTTP: rf.HTTP, Detail: primary + " — BMC 벤더를 식별하지 못해 2순위 계정은 시도하지 않음"}
	}
	user, pass, ok := fb.lookup(vendor)
	if !ok {
		return c, loginInfo{}, &RFError{Status: StatusAuthFail, HTTP: rf.HTTP, Detail: primary + " — rf.conf 에 " + vendorLabel(vendor) + " 예비 계정 없음"}
	}
	li := loginInfo{Vendor: vendor, prior: c.Stats()}
	_ = c.Close() // 세션이 없으므로 DELETE 는 나가지 않는다
	o2 := o
	o2.User, o2.Pass, o2.Retries = user, pass, 0
	c2 := NewClient(o2)
	if err2 := c2.Login(); err2 != nil {
		// 2순위가 인증이 아닌 이유(타임아웃 등)로 실패해도 최종 상태는 AUTH_FAIL 이다 (1순위가 이미 인증 거부됨).
		st, code := errStatus(err2)
		detail := primary + ", 2순위 계정도 실패 (" + vendorLabel(vendor) + ")"
		if st != StatusAuthFail {
			detail = primary + ", 2순위 계정도 실패 (" + vendorLabel(vendor) + ", " + errBrief(err2) + ")"
			code = rf.HTTP
		}
		return c2, li, &RFError{Status: StatusAuthFail, HTTP: code, Detail: detail}
	}
	li.Source = credFallback
	return c2, li, nil
}
