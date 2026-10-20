package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// redfish.go 는 BMC 와 통신하는 유일한 HTTP 클라이언트입니다 (계획서 3장 로그 안전장치).
//
// 이 패키지의 모든 HTTP 요청은 (*Client).do → roundTrip 한 경로만 통과하며,
// roundTrip 은 전송 직전에 checkAllowed(메서드·경로·모드)와 checkBody(본문)를 검사합니다.
// 허용목록에 없는 호출(ClearLog, ComputerSystem.Reset, 로그 조회, 남의 세션/Job 삭제 등)은
// 네트워크로 나가지 않고 ErrNotAllowed 를 돌려줍니다.
// 다른 파일이 net/http 를 직접 쓰거나 Client 내부를 건드리지 못하게 하는 구조 가드는
// redfish_test.go 의 TestNoRawHTTPOutsideRedfish 가 검사합니다.

// 상태 코드 (계획서 4장 "결과 상태" 와 같은 문자열).
const (
	StatusUnreachable = "UNREACHABLE" // 연결 거부 / no route / DNS
	StatusTimeout     = "TIMEOUT"
	StatusBMCError    = "BMC_ERROR"   // 5xx
	StatusAuthFail    = "AUTH_FAIL"   // 401, 로그인 403
	StatusUnsupported = "UNSUPPORTED" // /redfish/v1 404 (Redfish 미지원)
	StatusHTTPError   = "HTTP_ERROR"  // 그 외 4xx·3xx 등 (계획서 표에 없는 보조 상태)
)

const (
	maxBody        = 8 << 20 // 응답 본문 상한 8MB
	detailLimit    = 200     // RFError.Detail 최대 글자 수
	defaultGap     = 100 * time.Millisecond
	defaultTimeout = 20 * time.Second
	backoffBase    = 300 * time.Millisecond
	backoffMax     = 5 * time.Second // 백오프 대기 상한
	maxRetries     = 5               // Retries 상한 (BMC 부하·잠금 방지)
	sessionsPath   = "/redfish/v1/SessionService/Sessions"
)

// Mode 는 클라이언트가 보낼 수 있는 호출 범위입니다.
type Mode int

const (
	ModeReadOnly Mode = iota // 점검/덤프 (기본): GET + 세션 생성/삭제만
	ModeSet                  // 설정: + BIOS Settings/Pending PATCH, Dell 설정 Job POST
	ModeAccount              // 계정: + 계정 컬렉션 POST, 허용 등록된 계정 멤버 PATCH/DELETE (account*.go 만 사용)
)

// ErrNotAllowed 는 허용목록 밖의 호출이라 전송하지 않았음을 뜻합니다 (errors.Is 로 확인).
var ErrNotAllowed = errors.New("허용목록에 없는 Redfish 호출이라 보내지 않았습니다")

var errClosed = errors.New("Redfish 클라이언트가 이미 닫혔습니다 (Close 이후 호출)")

// RFError 는 BMC 통신 실패를 계획서 상태 코드로 분류한 오류입니다.
// Detail 에는 비밀번호·토큰이 들어가지 않으며 200자로 잘립니다.
type RFError struct {
	Status string // StatusXxx
	HTTP   int    // HTTP 상태 코드 (전송 실패면 0)
	Detail string
}

func (e *RFError) Error() string {
	if e.HTTP != 0 {
		return fmt.Sprintf("%s (HTTP %d): %s", e.Status, e.HTTP, e.Detail)
	}
	return e.Status + ": " + e.Detail
}

var (
	reJob      = regexp.MustCompile(`^/redfish/v1/managers/[^/]+/jobs$`)
	reBiosSet  = regexp.MustCompile(`^/redfish/v1/systems/[^/]+/bios/(settings|pending)$`)
	reSession  = regexp.MustCompile(`^/redfish/v1/sessionservice/sessions/[^/]+$`)
	reJobBiosT = regexp.MustCompile(`^/redfish/v1/systems/[^/]+/bios/settings$`)
	// 계정 컬렉션(생성 POST)과 멤버(PATCH/DELETE). AccountService/Accounts 와 Dell 의 Managers/<id>/Accounts 만.
	reAcctColl   = regexp.MustCompile(`^/redfish/v1/(accountservice/accounts|managers/[^/]+/accounts)$`)
	reAcctMember = regexp.MustCompile(`^/redfish/v1/(accountservice/accounts|managers/[^/]+/accounts)/[^/]+$`)
)

// GET 거부 세그먼트: 로그 조회는 BMC 부하·기록을 만들 수 있고 Actions 는 조회할 이유가 없다.
// sse(Server-Sent Events 스트림)·logentries 도 거부.
// 이 밖에 "...log", "...logs" 로 끝나는 세그먼트(EventLog, Lclog, AuditLog, Dell 구형 /Logs 등)도 거부하되,
// 로그가 아닌데 접미사만 같은 allowedLogSuffixSegs(UpdateService/Catalog)는 허용한다.
// 세그먼트 비교 전에 끝의 "." 와 "~" 를 뗀다 (Windows 계열 BMC 의 "LogServices." 우회 방지).
var deniedSegs = map[string]bool{
	"logservices": true, "logservice": true, "sel": true, "entries": true, "actions": true,
	"sse": true, "logentries": true,
}

var allowedLogSuffixSegs = map[string]bool{"catalog": true}

// deniedGetSeg 는 GET 경로의 세그먼트(소문자)가 로그/Actions/스트림 거부 대상인지 봅니다.
func deniedGetSeg(s string) bool {
	s = strings.TrimRight(s, ".~")
	if deniedSegs[s] {
		return true
	}
	return (strings.HasSuffix(s, "log") || strings.HasSuffix(s, "logs")) && !allowedLogSuffixSegs[s]
}

// normalizePath 는 요청 경로를 검사·전송용 정규형으로 만듭니다.
//
// 쿼리·프래그먼트는 버린다(전송하지도 않음 → $expand 로 로그를 끌어오는 우회도 막힘).
// 퍼센트 인코딩은 최대 3번까지 풀고, 그래도 % 가 남거나 제어문자·공백·비ASCII·\ ; ? # 가
// 있으면 거부한다. 그 뒤 path.Clean 으로 .. 와 // 를 정리한다.
// clean 은 비교용(끝 슬래시 없음), send 는 실제 전송 경로(원래 끝 슬래시 유지)입니다.
func normalizePath(raw string) (clean, send string, err error) {
	p := raw
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	for i := 0; i < 3 && strings.Contains(p, "%"); i++ {
		d, uerr := url.PathUnescape(p)
		if uerr != nil {
			return "", "", errors.New("잘못된 퍼센트 인코딩")
		}
		p = d
	}
	if strings.Contains(p, "%") {
		return "", "", errors.New("다중 퍼센트 인코딩")
	}
	if !strings.HasPrefix(p, "/") {
		return "", "", errors.New("/ 로 시작하는 경로가 아님 (절대 URL 불가)")
	}
	for i := 0; i < len(p); i++ {
		ch := p[i]
		if ch <= 0x20 || ch >= 0x7f || ch == '\\' || ch == ';' || ch == '?' || ch == '#' {
			return "", "", errors.New("경로에 허용되지 않은 문자")
		}
	}
	clean = path.Clean(p)
	send = clean
	if strings.HasSuffix(p, "/") && clean != "/" {
		send += "/"
	}
	return clean, send, nil
}

func notAllowed(method, rawPath, reason string) error {
	if utf8.RuneCountInString(rawPath) > detailLimit {
		rawPath = string([]rune(rawPath)[:detailLimit]) + "..."
	}
	return fmt.Errorf("%w: %q %q (%s)", ErrNotAllowed, method, rawPath, reason)
}

// checkAllowed 는 호출 허용목록입니다. 허용이면 실제로 보낼 경로를 돌려줍니다.
// sessPath 는 이 Client 가 직접 만든 세션 URI(정규형)이며 DELETE 는 그것 하나만 허용합니다.
// 경로 비교는 대소문자를 무시하고, 정규식은 모두 ^...$ 로 고정합니다.
func checkAllowed(method, rawPath string, mode Mode, sessPath string) (string, error) {
	clean, send, err := normalizePath(rawPath)
	if err != nil {
		return "", notAllowed(method, rawPath, err.Error())
	}
	lc := strings.ToLower(clean)
	if lc != "/redfish/v1" && !strings.HasPrefix(lc, "/redfish/v1/") {
		return "", notAllowed(method, rawPath, "/redfish/v1 밖의 경로")
	}
	switch method {
	case http.MethodGet:
		for _, s := range strings.Split(lc, "/") {
			if deniedGetSeg(s) {
				return "", notAllowed(method, rawPath, "로그/Actions 경로는 조회하지 않음")
			}
		}
		return send, nil
	case http.MethodPost:
		if lc == "/redfish/v1/sessionservice/sessions" {
			return send, nil
		}
		if reJob.MatchString(lc) {
			if mode == ModeSet {
				return send, nil
			}
			return "", notAllowed(method, rawPath, "읽기 전용 모드에서는 Job 생성 불가")
		}
		if reAcctColl.MatchString(lc) {
			if mode == ModeAccount {
				return send, nil
			}
			return "", notAllowed(method, rawPath, "계정 모드가 아니면 계정 생성 불가")
		}
		return "", notAllowed(method, rawPath, "세션 생성·Dell 설정 Job·계정 생성 외 POST(Actions 등) 금지")
	case http.MethodPatch:
		if reAcctMember.MatchString(lc) {
			if mode == ModeAccount {
				return send, nil
			}
			return "", notAllowed(method, rawPath, "계정 모드가 아니면 계정 PATCH 불가")
		}
		if !reBiosSet.MatchString(lc) {
			return "", notAllowed(method, rawPath, "Bios/Settings·Bios/Pending·계정 멤버 외 PATCH 금지")
		}
		if mode != ModeSet {
			return "", notAllowed(method, rawPath, "읽기 전용 모드에서는 PATCH 불가")
		}
		return send, nil
	case http.MethodDelete:
		if sessPath != "" && clean == sessPath && reSession.MatchString(lc) {
			return send, nil
		}
		if reAcctMember.MatchString(lc) && mode == ModeAccount {
			return send, nil // 이번 실행이 만든 멤버만 보낸다 (Client.do 가 acctOK 로 다시 확인)
		}
		return "", notAllowed(method, rawPath, "자기가 만든 세션·계정 외 DELETE 금지")
	}
	return "", notAllowed(method, rawPath, "허용되지 않은 메서드")
}

// reDangerAttr 는 BIOS 설정 PATCH 에 실어 보내면 안 되는 속성명입니다 (비밀번호·기본값 복원·부팅순서·초기화 계열).
var reDangerAttr = regexp.MustCompile(`(?i)password|passwd|pwd|restore|factory|resetbios|resetcmos|clearcmos|bootorder|bootseq|bootsequence|clear|defaults?$`)

// checkAttributes 는 PATCH 의 Attributes 내부를 검사합니다.
// 키는 "@" 로 시작할 수 없고 위험 속성명(reDangerAttr)·내장 보호 속성(protect.go builtinProtect)이 아니어야 하며,
// 값은 비어 있지 않은 문자열·숫자·불리언뿐입니다 (객체/배열/null/빈값 금지).
// 프로파일 허용목록은 단계 6 에서 추가로 강제한다 (여기서는 위험 형태만 걸러냄).
func checkAttributes(attrs map[string]json.RawMessage) error {
	if len(attrs) == 0 {
		return errors.New("Attributes 가 비어 있음")
	}
	for name, raw := range attrs {
		if name == "" || strings.HasPrefix(name, "@") {
			return fmt.Errorf("Attributes 키 %q 는 허용되지 않음", name)
		}
		if reDangerAttr.MatchString(name) {
			return fmt.Errorf("위험 속성명 %q 는 설정할 수 없음", name)
		}
		if p := builtinProtectSet.match(name); p != "" { // 내장 보호 목록 (protect.go, 끌 수 없음)
			return fmt.Errorf("보호 속성 %q (패턴 %s) 는 설정할 수 없음", name, p)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v interface{}
		if dec.Decode(&v) != nil {
			return fmt.Errorf("Attributes %q 값을 해석할 수 없음", name)
		}
		ok := false
		switch x := v.(type) {
		case string:
			ok = strings.TrimSpace(x) != ""
		case json.Number, bool:
			ok = true
		}
		if !ok {
			return fmt.Errorf("Attributes %q 값은 비어 있지 않은 문자열·숫자·불리언만 허용", name)
		}
	}
	return nil
}

// checkBody 는 설정 요청 본문을 검사하고, 중복 키 등을 없앤 정규형 본문을 돌려줍니다.
//   - PATCH: {"Attributes":{...}} 필수, "@Redfish.SettingsApplyTime" 은 {"ApplyTime":"OnReset"} 만.
//     (ApplyTime=Immediate 는 BMC 가 즉시 재부팅할 수 있어 금지)
//   - Dell Job POST: TargetSettingsURI(Systems/<id>/Bios/Settings) 필수,
//     ScheduledStartTime·UntilTime 만 추가 허용 (재부팅 Job 유형 등 다른 키 금지).
//
// 그 외 메서드는 본문을 그대로 돌려줍니다 (세션 POST 본문은 login 이 직접 만든다).
func checkBody(method, send string, body []byte) ([]byte, error) {
	lc := strings.ToLower(path.Clean(send))
	switch {
	case method == http.MethodPost && reAcctColl.MatchString(lc):
		return checkAccountCreateBody(body)
	case method == http.MethodPatch && reAcctMember.MatchString(lc):
		return checkAccountPatchBody(body)
	case method == http.MethodPatch:
		var m map[string]json.RawMessage
		if json.Unmarshal(body, &m) != nil || m == nil {
			return nil, errors.New("PATCH 본문은 JSON 객체여야 함")
		}
		out := map[string]interface{}{}
		for k, v := range m {
			switch k {
			case "Attributes":
				var attrs map[string]json.RawMessage
				if json.Unmarshal(v, &attrs) != nil || attrs == nil {
					return nil, errors.New("Attributes 는 JSON 객체여야 함")
				}
				if err := checkAttributes(attrs); err != nil {
					return nil, err
				}
				out[k] = attrs
			case "@Redfish.SettingsApplyTime":
				var at map[string]json.RawMessage
				if json.Unmarshal(v, &at) != nil || len(at) != 1 || string(at["ApplyTime"]) != `"OnReset"` {
					return nil, errors.New(`@Redfish.SettingsApplyTime 은 {"ApplyTime":"OnReset"} 만 허용`)
				}
				out[k] = map[string]string{"ApplyTime": "OnReset"}
			default:
				return nil, fmt.Errorf("PATCH 본문에 허용되지 않은 키 %q", k)
			}
		}
		if out["Attributes"] == nil {
			return nil, errors.New("PATCH 본문에 Attributes 가 없음")
		}
		return json.Marshal(out)
	case method == http.MethodPost && reJob.MatchString(lc):
		var m map[string]json.RawMessage
		if json.Unmarshal(body, &m) != nil || m == nil {
			return nil, errors.New("Job 본문은 JSON 객체여야 함")
		}
		out := map[string]string{}
		for k, v := range m {
			var s string
			if json.Unmarshal(v, &s) != nil {
				return nil, fmt.Errorf("Job 본문 %q 는 문자열이어야 함", k)
			}
			switch k {
			case "TargetSettingsURI":
				// 검사한 정규형과 원문이 정확히 같을 때만 허용한다 (.., #, ?, %XX 로 BMC 가 다르게
				// 해석할 여지를 없앰). 전송도 이 정규형이다.
				c, _, err := normalizePath(s)
				if err != nil || c != s || !reJobBiosT.MatchString(strings.ToLower(c)) {
					return nil, errors.New("TargetSettingsURI 는 정규형 Systems/<id>/Bios/Settings 만 허용")
				}
				s = c
			case "ScheduledStartTime", "UntilTime":
			default:
				return nil, fmt.Errorf("Job 본문에 허용되지 않은 키 %q", k)
			}
			out[k] = s
		}
		if out["TargetSettingsURI"] == "" {
			return nil, errors.New("Job 본문에 TargetSettingsURI 가 없음")
		}
		return json.Marshal(out)
	}
	return body, nil
}

// ClientOpts 는 NewClient 옵션입니다.
type ClientOpts struct {
	BaseURL  string // https://<BMC IP 또는 이름>[:포트] (경로는 무시)
	User     string
	Pass     string
	Insecure bool          // TLS 인증서 검증 생략 (자체서명 BMC)
	Timeout  time.Duration // 요청 1건 타임아웃 (0 이면 20초)
	Retries  int           // 일시 오류 자동 재시도 횟수 (GET·세션 연결 실패만)
	Mode     Mode
	Gap      time.Duration // 같은 호스트 연속 요청 최소 간격 (0 이면 100ms, 음수면 간격 없음)
}

// CallStats 는 실제로 전송한 요청 수입니다 (거부된 호출은 세지 않음, 재시도는 1건씩 셈).
type CallStats struct {
	Gets, Posts, Patches, Deletes int
	Sessions                      int    // 생성에 성공한 세션 수 (호스트당 0 또는 1)
	AuthMode                      string // "session" | "basic" | "" (로그인 전/실패)
	Fallback                      bool   // 1순위 계정은 인증 거부되어 2순위(rf.conf) 계정으로 로그인함 (차단기 웜업이 집계)
}

// credentials 는 클로저 안에만 둔다 (Client.cred). fmt 는 어떤 동사(%v/%+v/%#v/%s/%x …)로
// 찍어도 함수 값은 주소만 출력하므로 Client 를 통째로 출력해도 비밀번호·토큰이 드러나지 않는다.
// (구조체 포인터로 두면 %s 같은 잘못된 동사에서 fmt 가 역참조해 내용을 찍는다)
type credentials struct {
	user, pass, token string
}

// Client 는 BMC 1대(호스트 1개)용 Redfish 클라이언트입니다.
// 메서드는 내부 잠금으로 직렬화되므로 같은 호스트 요청은 하나씩, Gap 간격을 두고 나갑니다.
type Client struct {
	mu        sync.Mutex
	base      *url.URL
	baseErr   error
	httpc     *http.Client
	cred      func() *credentials
	rfMode    Mode
	retries   int
	gap       time.Duration
	sleep     func(time.Duration) // 테스트에서 주입
	last      time.Time           // 직전 요청 완료 시각
	sessClean string              // 직접 만든 세션 URI (비교용 정규형)
	sessSend  string              // 같은 URI 의 전송형 (끝 슬래시 유지)
	loginDone bool
	loginErr  error
	closed    bool
	stats     CallStats
	acctOK    map[string]bool // ModeAccount: PATCH/DELETE 해도 되는 계정 멤버 경로 (소문자 정규형). AllowAccountTarget 이나 자기가 만든 POST 로만 채움
	extra     []string        // 오류 Detail 에서 지울 추가 비밀(새 계정 비밀번호)
	authMode  string          // "session" | "basic" (세션 생성 404/405/501 폴백) | "" (로그인 전). 읽기는 Stats().AuthMode 로만
}

// NewClient 는 클라이언트를 만듭니다. 접속은 하지 않습니다.
// BaseURL 오류는 첫 호출에서 돌려줍니다 (https 만, 계정정보 포함 URL 금지).
func NewClient(o ClientOpts) *Client {
	cr := &credentials{user: o.User, pass: o.Pass}
	c := &Client{
		cred:    func() *credentials { return cr },
		rfMode:  o.Mode,
		retries: o.Retries,
		gap:     o.Gap,
		sleep:   time.Sleep,
	}
	if c.retries > maxRetries {
		c.retries = maxRetries
	}
	if c.retries < 0 {
		c.retries = 0
	}
	if c.gap == 0 {
		c.gap = defaultGap
	} else if c.gap < 0 {
		c.gap = 0
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	// url.Parse 오류 문자열에는 URL 원문이 들어가므로 메시지에 싣지 않는다.
	u, err := url.Parse(o.BaseURL)
	switch {
	case err != nil:
		c.baseErr = errors.New("BaseURL 을 해석할 수 없습니다")
	case u.User != nil:
		c.baseErr = errors.New("BaseURL 에 계정 정보를 넣을 수 없습니다")
	case !strings.EqualFold(u.Scheme, "https") || u.Host == "":
		c.baseErr = errors.New("BaseURL 은 https://<호스트> 형식이어야 합니다 (평문 http 금지)")
	default:
		c.base = &url.URL{Scheme: "https", Host: u.Host}
	}
	c.httpc = &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                  nil, // 환경변수 프록시를 거치지 않는다 (관리망 직접 접속)
			DialContext:            (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:    timeout,
			TLSClientConfig:        &tls.Config{InsecureSkipVerify: o.Insecure},
			MaxIdleConnsPerHost:    1,
			IdleConnTimeout:        30 * time.Second, // Close 를 못 부른 경우에도 유휴 연결(FD)이 남지 않게
			MaxResponseHeaderBytes: 64 << 10,         // 비정상 BMC 의 거대한 응답 헤더 차단
		},
		// 리다이렉트는 따라가지 않는다: 자격증명이 다른 곳으로 가거나 허용목록 밖 경로로 가는 것을 막음.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c
}

// Login 은 세션을 만듭니다 (POST SessionService/Sessions 1회). 이미 시도했으면 그 결과를 돌려줍니다.
// 세션 생성이 404/405/501 이면 Basic 인증으로 폴백합니다 (Stats().AuthMode="basic").
// 401/403 은 AUTH_FAIL 로 즉시 반환하며 재시도하지 않습니다.
// 다른 메서드는 필요할 때 Login 을 자동으로 1번 호출합니다.
func (c *Client) Login() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseErr != nil {
		return c.baseErr
	}
	if c.closed {
		return errClosed
	}
	return c.ensureLogin()
}

// GetJSON 은 GET 응답을 v 로 해석합니다.
func (c *Client) GetJSON(p string, v interface{}) error {
	b, err := c.GetRaw(p)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.rfErr(StatusHTTPError, 0, "응답 JSON 해석 실패: "+err.Error())
	}
	return nil
}

// GetRaw 는 GET 응답 본문을 그대로 돌려줍니다.
func (c *Client) GetRaw(p string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _, b, err := c.do(http.MethodGet, p, nil)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Patch 는 BIOS Settings/Pending 에 PATCH 합니다 (ModeSet 전용, 자동 재시도 없음).
// 오류여도 HTTP 상태 코드와 본문을 함께 돌려줍니다.
func (c *Client) Patch(p string, body interface{}) (int, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, _, b, err := c.do(http.MethodPatch, p, body)
	return st, b, err
}

// PostJob 은 Dell 설정 Job 을 만듭니다 (Managers/<id>/Jobs, ModeSet 전용, 자동 재시도 없음).
func (c *Client) PostJob(p string, body interface{}) (int, http.Header, []byte, error) {
	// 세션 경로도 POST 허용이므로, PostJob 으로 세션을 추가로 만들지 못하게 Job 경로만 받는다.
	clean, _, err := normalizePath(p)
	if err != nil || !reJob.MatchString(strings.ToLower(clean)) {
		return 0, nil, nil, notAllowed(http.MethodPost, p, "PostJob 은 Managers/<id>/Jobs 전용")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.do(http.MethodPost, p, body)
}

// AddSecret 은 오류 Detail 에서 지울 비밀 값(새 계정 비밀번호)을 등록합니다. 값은 어디에도 출력하지 않는다.
func (c *Client) AddSecret(s string) {
	if s == "" {
		return
	}
	c.mu.Lock()
	c.extra = append(c.extra, s)
	c.mu.Unlock()
}

// AllowAccountTarget 은 ModeAccount 에서 PATCH/DELETE 해도 되는 계정 멤버 경로를 등록합니다
// (호출자가 빈 슬롯·대상 계정임을 확인한 뒤에만 부른다). 멤버 경로가 아니면 오류입니다.
func (c *Client) AllowAccountTarget(p string) error {
	clean, _, err := normalizePath(p)
	lc := strings.ToLower(clean)
	if err != nil || !reAcctMember.MatchString(lc) {
		return notAllowed("ALLOW", p, "계정 멤버 경로가 아님")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rfMode != ModeAccount {
		return notAllowed("ALLOW", p, "계정 모드가 아님")
	}
	if c.acctOK == nil {
		c.acctOK = map[string]bool{}
	}
	c.acctOK[lc] = true
	return nil
}

// AccountPost 는 계정 컬렉션에 계정을 만듭니다 (ModeAccount 전용, 재시도 없음).
// 응답 Location 이 계정 멤버 경로이면 이후 DELETE(롤백)가 가능하도록 자동 등록하고 그 경로를 돌려줍니다.
func (c *Client) AccountPost(p string, body interface{}) (status int, member string, resp []byte, err error) {
	clean, _, nerr := normalizePath(p)
	if nerr != nil || !reAcctColl.MatchString(strings.ToLower(clean)) {
		return 0, "", nil, notAllowed(http.MethodPost, p, "AccountPost 는 계정 컬렉션 전용")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	st, hdr, b, derr := c.do(http.MethodPost, p, body)
	if derr != nil {
		return st, "", b, derr
	}
	loc := hdr.Get("Location")
	if loc == "" {
		var od struct {
			ID string `json:"@odata.id"`
		}
		_ = json.Unmarshal(b, &od)
		loc = od.ID
	}
	if u, perr := url.Parse(loc); perr == nil {
		loc = u.EscapedPath()
	}
	if cl, _, e := normalizePath(loc); e == nil && reAcctMember.MatchString(strings.ToLower(cl)) {
		if c.acctOK == nil {
			c.acctOK = map[string]bool{}
		}
		c.acctOK[strings.ToLower(cl)] = true
		member = cl
	}
	return st, member, b, nil
}

// AccountPatch 는 허용 등록된 계정 멤버를 PATCH 합니다 (ModeAccount 전용, 재시도 없음).
func (c *Client) AccountPatch(p string, body interface{}) (int, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, _, b, err := c.do(http.MethodPatch, p, body)
	return st, b, err
}

// AccountDelete 는 이번 실행이 만든(등록된) 계정 멤버를 DELETE 합니다 (ModeAccount 전용, 롤백용).
func (c *Client) AccountDelete(p string) (int, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, _, b, err := c.do(http.MethodDelete, p, nil)
	return st, b, err
}

// Close 는 이 Client 가 만든 세션만 DELETE 합니다 (best-effort, 멱등, 자동 재시도 없음).
// 이후의 모든 호출은 errClosed 로 거부됩니다 (다시 로그인하지 않음).
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	var err error
	if c.sessClean != "" && c.cred().token != "" && c.baseErr == nil {
		_, _, _, err = c.do(http.MethodDelete, c.sessSend, nil)
	}
	c.closed = true
	c.cred().token, c.cred().pass = "", ""
	c.httpc.CloseIdleConnections()
	return err
}

// Stats 는 호출 통계를 돌려줍니다.
func (c *Client) Stats() CallStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.stats
	s.AuthMode = c.authMode
	return s
}

// ensureLogin 은 로그인을 최대 1번만 시도합니다 (실패도 기억해 다시 POST 하지 않음). c.mu 보유 상태로 호출.
func (c *Client) ensureLogin() error {
	if c.loginDone {
		return c.loginErr
	}
	c.loginDone = true
	c.loginErr = c.login()
	return c.loginErr
}

func (c *Client) login() error {
	body := map[string]string{"UserName": c.cred().user, "Password": c.cred().pass}
	status, hdr, b, err := c.do(http.MethodPost, sessionsPath, body)
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		// 세션 서비스 미지원 → Basic 폴백 (요청마다 인증 기록이 남을 수 있음)
		c.authMode = "basic"
		return nil
	}
	if err != nil {
		return err
	}
	token := hdr.Get("X-Auth-Token")
	if token == "" {
		return c.rfErr(StatusHTTPError, status, "세션 응답에 X-Auth-Token 이 없습니다")
	}
	c.cred().token = token
	c.authMode = "session"
	c.stats.Sessions++

	// 세션 URI: Location(절대 URL 이면 경로만, 호스트는 항상 BaseURL) → 없으면 본문 @odata.id.
	loc := hdr.Get("Location")
	if loc == "" {
		var od struct {
			ID string `json:"@odata.id"`
		}
		_ = json.Unmarshal(b, &od)
		loc = od.ID
	}
	if u, perr := url.Parse(loc); perr == nil {
		loc = u.EscapedPath()
	}
	// SessionService/Sessions/<id> 꼴이 아니면 보관하지 않는다 (BMC 가 엉뚱한 URI 를 주면 DELETE 안 함).
	if clean, send, nerr := normalizePath(loc); nerr == nil && reSession.MatchString(strings.ToLower(clean)) {
		c.sessClean, c.sessSend = clean, send
	}
	return nil
}

// do 는 이 패키지의 유일한 요청 경로입니다. c.mu 보유 상태로 호출합니다.
// 허용 검사 → (필요 시) 로그인 → 간격 대기 → roundTrip → 상태 분류 → 재시도 판단.
// 재시도: GET 은 UNREACHABLE/TIMEOUT/BMC_ERROR, 세션 POST 는 연결 수립 전(dial) 실패만.
// PATCH·Job POST·DELETE 는 재시도하지 않고, AUTH_FAIL 은 어떤 경우에도 재시도하지 않습니다.
func (c *Client) do(method, p string, body interface{}) (int, http.Header, []byte, error) {
	if c.baseErr != nil {
		return 0, nil, nil, c.baseErr
	}
	if c.closed {
		return 0, nil, nil, errClosed
	}
	// 거부될 호출이면 로그인(세션 POST)조차 하지 않도록 먼저 검사한다.
	send, err := checkAllowed(method, p, c.rfMode, c.sessClean)
	if err != nil {
		return 0, nil, nil, err
	}
	var payload []byte
	if method == http.MethodPost || method == http.MethodPatch {
		if payload, err = json.Marshal(body); err != nil {
			return 0, nil, nil, fmt.Errorf("요청 본문 JSON 변환 실패: %v", err)
		}
		if payload, err = checkBody(method, send, payload); err != nil {
			return 0, nil, nil, notAllowed(method, p, err.Error())
		}
	}
	clean := path.Clean(send)
	isSession := method == http.MethodPost && strings.EqualFold(clean, sessionsPath)
	lcClean := strings.ToLower(clean)
	if c.rfMode == ModeAccount && (method == http.MethodPatch || method == http.MethodDelete) && reAcctMember.MatchString(lcClean) && !c.acctOK[lcClean] {
		return 0, nil, nil, notAllowed(method, p, "이번 실행이 허용 등록하지 않은 계정 멤버 (빈 슬롯·생성한 계정·대상 계정만)")
	}
	// 세션 삭제(DELETE)는 로그인 상태와 무관하게 보낸다: 토큰 인증이라 계정 잠금 위험이 없고,
	// GET 이 AUTH_FAIL 을 받은 뒤에도 Close 가 만든 세션은 지워야 BMC 세션 슬롯이 새지 않는다.
	// (계정 멤버 DELETE 는 로그인된 세션이 있어야 한다)
	if !isSession && !(method == http.MethodDelete && reSession.MatchString(lcClean)) {
		if err := c.ensureLogin(); err != nil {
			return 0, nil, nil, err
		}
	}
	attempts := 1
	if method == http.MethodGet || isSession {
		attempts += c.retries
	}
	for i := 0; ; i++ {
		c.waitGap()
		status, hdr, b, dialFail, err := c.roundTrip(method, send, payload, isSession)
		c.last = time.Now()
		lc := strings.ToLower(clean)
		// GET 은 같은 BMC 안의 리다이렉트를 최대 maxRedirects 번 따라간다 (예: /redfish/v1 → /redfish/v1/ 308).
		// 새 경로도 roundTrip 이 허용목록을 다시 검사하므로 로그/Actions 등으로는 갈 수 없다.
		for hops := 0; err == nil && method == http.MethodGet && isRedirectStatus(status) && hops < maxRedirects; hops++ {
			next, ok := c.redirectTarget(hdr.Get("Location"))
			if !ok {
				break
			}
			if _, aerr := checkAllowed(method, next, c.rfMode, c.sessClean); aerr != nil {
				break // 허용목록 밖(로그/Actions 등)으로 가는 리다이렉트는 따르지 않고 3xx 그대로 HTTP_ERROR
			}
			c.waitGap()
			status, hdr, b, dialFail, err = c.roundTrip(method, next, nil, false)
			c.last = time.Now()
			lc = strings.ToLower(path.Clean(next))
		}
		if err == nil {
			err = c.statusErr(status, lc, isSession, b)
		}
		if err == nil {
			return status, hdr, b, nil
		}
		var rf *RFError
		if errors.As(err, &rf) && rf.Status == StatusAuthFail {
			// 인증 실패를 기억해 이후 호출은 보내지 않는다 (Basic 폴백·토큰 만료에서
			// 요청마다 401 이 쌓여 계정이 잠기는 것을 막음).
			// 세션 DELETE 의 401 은 기록하지 않는다 (이미 끝난 로그인 결과를 덮어쓰지 않음).
			if method != http.MethodDelete {
				c.loginDone, c.loginErr = true, err
			}
			return status, hdr, b, err
		}
		retry := false
		if rf != nil {
			switch {
			case method == http.MethodGet:
				retry = rf.Status == StatusUnreachable || rf.Status == StatusTimeout || rf.Status == StatusBMCError
			case isSession:
				retry = dialFail
			}
		}
		if !retry || i+1 >= attempts {
			return status, hdr, b, err
		}
		c.sleep(backoffFor(i))
	}
}

const maxRedirects = 2 // GET 리다이렉트를 따라가는 최대 횟수

func isRedirectStatus(s int) bool {
	return s == http.StatusMovedPermanently || s == http.StatusFound || s == http.StatusSeeOther ||
		s == http.StatusTemporaryRedirect || s == http.StatusPermanentRedirect
}

// redirectTarget 은 Location 에서 따라갈 경로를 고릅니다. 같은 호스트(상대 경로 또는 같은 host:port 의 https)만 허용하고,
// 경로만 취합니다 (쿼리·프래그먼트는 버림). 다른 호스트·http 로의 리다이렉트는 따라가지 않아 자격증명이 새지 않습니다.
func (c *Client) redirectTarget(loc string) (string, bool) {
	if loc == "" {
		return "", false
	}
	u, err := url.Parse(loc)
	if err != nil {
		return "", false
	}
	if u.Host != "" && !strings.EqualFold(u.Host, c.base.Host) {
		return "", false
	}
	if u.Scheme != "" && !strings.EqualFold(u.Scheme, "https") {
		return "", false
	}
	p := u.EscapedPath()
	if p == "" || !strings.HasPrefix(p, "/") {
		return "", false
	}
	return p, true
}

// ServiceRootNoAuth 는 인증 정보(토큰·Basic) 없이 ServiceRoot(GET /redfish/v1/)를 1회 읽습니다.
// 로그인 상태와 무관하게 보내며 세션을 만들지 않고 재시도하지 않습니다 (Redfish 규격상 ServiceRoot 는 비인증 허용).
// 1순위 계정이 AUTH_FAIL 일 때 2순위 계정을 고르려고 벤더만 알아내는 용도입니다 (fallback.go).
func (c *Client) ServiceRootNoAuth() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseErr != nil {
		return nil, c.baseErr
	}
	if c.closed {
		return nil, errClosed
	}
	send, err := checkAllowed(http.MethodGet, rootPath+"/", c.rfMode, c.sessClean)
	if err != nil {
		return nil, err
	}
	c.waitGap()
	status, _, b, _, err := c.roundTrip(http.MethodGet, send, nil, true)
	c.last = time.Now()
	if err == nil {
		err = c.statusErr(status, strings.ToLower(path.Clean(send)), false, b)
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// roundTrip 은 요청 1건을 실제로 보냅니다. 전송 직전에 허용목록·본문을 다시 검사하고,
// 검사를 통과한 정규형 경로만 BaseURL 호스트로 보냅니다 (쿼리 없음, 헤더는 아래 고정 목록뿐).
// noAuth 면 인증 헤더(토큰·Basic)를 싣지 않습니다 (세션 생성 POST, 비인증 ServiceRoot GET).
func (c *Client) roundTrip(method, send string, payload []byte, noAuth bool) (status int, hdr http.Header, body []byte, dialFail bool, err error) {
	if send, err = checkAllowed(method, send, c.rfMode, c.sessClean); err != nil {
		return 0, nil, nil, false, err
	}
	if payload != nil {
		if payload, err = checkBody(method, send, payload); err != nil {
			return 0, nil, nil, false, notAllowed(method, send, err.Error())
		}
	}
	u := url.URL{Scheme: c.base.Scheme, Host: c.base.Host, Path: send}
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, u.String(), rd)
	if err != nil {
		return 0, nil, nil, false, c.rfErr(StatusHTTPError, 0, "요청 생성 실패")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("OData-Version", "4.0")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if !noAuth {
		if c.authMode == "basic" {
			req.SetBasicAuth(c.cred().user, c.cred().pass)
		} else {
			req.Header.Set("X-Auth-Token", c.cred().token)
		}
	}
	switch method {
	case http.MethodGet:
		c.stats.Gets++
	case http.MethodPost:
		c.stats.Posts++
	case http.MethodPatch:
		c.stats.Patches++
	case http.MethodDelete:
		c.stats.Deletes++
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return 0, nil, nil, isDialErr(err), c.transportErr(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return resp.StatusCode, resp.Header, nil, false, c.transportErr(err)
	}
	if len(b) > maxBody {
		return resp.StatusCode, resp.Header, nil, false, c.rfErr(StatusHTTPError, resp.StatusCode, "응답 본문이 8MB 를 넘습니다")
	}
	return resp.StatusCode, resp.Header, b, false, nil
}

// backoffFor 는 i 번째(0부터) 재시도 전 대기 시간입니다: 300ms×2^i, 최대 backoffMax (시프트 오버플로 없음).
func backoffFor(i int) time.Duration {
	d := backoffBase
	for ; i > 0 && d < backoffMax; i-- {
		d *= 2
	}
	if d > backoffMax {
		d = backoffMax
	}
	return d
}

func (c *Client) waitGap() {
	if c.gap <= 0 || c.last.IsZero() {
		return
	}
	if d := c.gap - time.Since(c.last); d > 0 {
		c.sleep(d)
	}
}

// statusErr 는 HTTP 상태를 분류합니다 (2xx 면 nil).
func (c *Client) statusErr(status int, lc string, isLogin bool, body []byte) error {
	var st string
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized, status == http.StatusForbidden && isLogin:
		st = StatusAuthFail
	case status >= 500:
		st = StatusBMCError
	case status == http.StatusNotFound && lc == "/redfish/v1":
		st = StatusUnsupported
	default:
		st = StatusHTTPError
	}
	detail := string(body)
	if status >= 300 && status < 400 {
		detail = "리다이렉트는 따라가지 않습니다"
	}
	if isLogin {
		// 로그인 응답 본문에는 비밀번호가 되돌아올 수 있으므로 본문을 싣지 않는다.
		detail = fmt.Sprintf("세션 생성 요청이 실패했습니다 (HTTP %d)", status)
	}
	return c.rfErr(st, status, detail)
}

// transportErr 는 전송 실패를 TIMEOUT / UNREACHABLE 로 분류합니다.
// url.Error 껍데기(메서드·URL)는 벗기고 내부 원인만 Detail 에 담는다.
func (c *Client) transportErr(err error) error {
	inner := err
	var ue *url.Error
	if errors.As(err, &ue) {
		inner = ue.Err
	}
	st := StatusUnreachable
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		st = StatusTimeout
	}
	return c.rfErr(st, 0, inner.Error())
}

// isDialErr 는 연결 수립 전(DNS·dial) 실패인지 봅니다. 이 경우 요청은 BMC 에 도달하지 않았다.
func isDialErr(err error) bool {
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Op == "dial" {
		return true
	}
	var de *net.DNSError
	return errors.As(err, &de)
}

// rfErr 는 RFError 를 만듭니다. Detail 에서 비밀번호·토큰(원문, JSON 이스케이프 변형, Basic 헤더 값)을
// 지우고, 제어문자·비출력 문자(ESC, BEL, NUL 등)는 "?" 로, 공백류는 공백 1칸으로 바꾼 뒤 200자로 자릅니다.
func (c *Client) rfErr(status string, code int, detail string) *RFError {
	var secrets []string
	if c.cred().pass != "" {
		secrets = append(secrets, passwordForms(c.cred().pass)...)
		secrets = append(secrets, base64.StdEncoding.EncodeToString([]byte(c.cred().user+":"+c.cred().pass)))
	}
	if c.cred().token != "" {
		secrets = append(secrets, c.cred().token)
	}
	for _, x := range c.extra {
		secrets = append(secrets, passwordForms(x)...)
	}
	for _, s := range secrets {
		detail = strings.ReplaceAll(detail, s, "***")
	}
	detail = sanitizeDetail(detail)
	if utf8.RuneCountInString(detail) > detailLimit {
		detail = string([]rune(detail)[:detailLimit])
	}
	return &RFError{Status: status, HTTP: code, Detail: detail}
}

// sanitizeDetail 은 터미널 제어 시퀀스를 무력화합니다: 공백류(탭·개행 포함)는 공백 1칸으로 접고,
// 그 밖의 제어문자·비출력 문자는 "?" 로 바꾼다.
func sanitizeDetail(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case !unicode.IsPrint(r):
			return '?'
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// reUEscape 는 JSON 의 \uXXXX 이스케이프입니다 (16진 대소문자를 둘 다 만들기 위해 씀).
var reUEscape = regexp.MustCompile(`\x5cu[0-9a-fA-F]{4}`)

// passwordForms 는 BMC 가 오류 본문에 비밀번호를 되돌릴 수 있는 형태를 모두 만듭니다:
// 원문, JSON 이스케이프(HTML 이스케이프 유무), "\/" 이스케이프, 비ASCII 의 \uXXXX(16진 소·대문자),
// 전체 \uXXXX. 긴 것부터 지워야 짧은 형태가 긴 형태의 일부만 남기지 않는다.
func passwordForms(pass string) []string {
	jsonInner := func(escapeHTML bool) string {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(escapeHTML)
		_ = enc.Encode(pass)
		s := strings.TrimSuffix(b.String(), "\n")
		return s[1 : len(s)-1] // 앞뒤 따옴표만 제거 (비밀번호 끝의 \" 는 유지)
	}
	uEsc := func(s string, all bool) string { // 비ASCII(all 이면 전부)를 소문자 \uXXXX 로
		var b strings.Builder
		for _, r := range s {
			switch {
			case r < 0x80 && !all:
				b.WriteRune(r)
			case r > 0xFFFF:
				r1, r2 := utf16.EncodeRune(r)
				fmt.Fprintf(&b, `\u%04x\u%04x`, r1, r2)
			default:
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		}
		return b.String()
	}
	upperU := func(s string) string {
		return reUEscape.ReplaceAllStringFunc(s, func(m string) string { return m[:2] + strings.ToUpper(m[2:]) })
	}
	seen := map[string]bool{}
	var forms []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			forms = append(forms, s)
		}
	}
	addAll := func(s string) {
		add(s)
		add(upperU(s))
	}
	for _, base := range []string{pass, jsonInner(true), jsonInner(false)} {
		for _, v := range []string{base, strings.ReplaceAll(base, "/", `\/`)} {
			addAll(v)
			addAll(uEsc(v, false))
		}
	}
	addAll(uEsc(pass, true))
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
}

// ---- Prober: trs(접속 장애 진단) 전용 좁은 관문 ----
//
// trs 는 403 의 원인을 가르려고 요청 헤더·인증 방식을 바꿔 보내야 해서 Client 의 고정 헤더로는 부족합니다.
// 그래도 네트워크 호출은 이 파일 밖에서 하지 못하게(TestNoRawHTTPOutsideRedfish) 한 곳에 두고 허용 범위를 좁힙니다:
// GET/HEAD/OPTIONS(/redfish 아래, 로그·Actions 경로 제외), 세션 생성 POST 1종, 이 Prober 가 만든 세션의 DELETE 뿐입니다.
// PATCH·계정 변경·Job·Actions 는 코드상 보낼 수 없습니다. 리다이렉트는 따라가지 않습니다.

// ProbeReq 는 Prober 요청 1건입니다.
type ProbeReq struct {
	Method, Path string
	Auth         string // "" | "basic" | "token"
	User, Pass   string
	Token        string
	Body         []byte
	UA           string
	UASet        bool   // UA 가 "" 여도 지정한 것으로 봄 (User-Agent 생략 시험)
	NoOData      bool   // OData-Version 헤더 생략
	NoAccept     bool   // Accept 헤더 생략
	ContentType  string // 본문이 있을 때 Content-Type (비면 application/json)
}

// ProbeResp 는 Prober 응답입니다. Err 가 nil 이 아니면 요청이 나가지 않았거나 전송에 실패한 것입니다.
type ProbeResp struct {
	Status  int
	Proto   string
	Header  http.Header
	Body    []byte
	TLS     *tls.ConnectionState
	Err     error
	Elapsed time.Duration
	Sent    string // 실제 전송한 경로 (정규형)
}

// Prober 는 BMC 1대용 진단 클라이언트입니다 (연결 재사용 없음, 요청 간 잠금 없음 — 한 고루틴에서만 씁니다).
type Prober struct {
	hostport string
	cli      *http.Client
	timeout  time.Duration
	sess     string // 이 Prober 가 만든 세션 URI (정규형)
}

var reProbeSession = regexp.MustCompile(`(?i)^/redfish/v1/sessionservice/sessions/[^/]+$`)

// NewProber 는 hostport("ip:포트" 또는 "ip") 로 접속하는 Prober 를 만듭니다. 접속은 하지 않습니다.
func NewProber(hostport string, insecure bool, timeout time.Duration) *Prober {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Prober{
		hostport: hostport,
		timeout:  timeout,
		cli: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:                  nil,
				DialContext:            (&net.Dialer{Timeout: timeout}).DialContext,
				TLSHandshakeTimeout:    timeout,
				TLSClientConfig:        &tls.Config{InsecureSkipVerify: insecure},
				DisableKeepAlives:      true,
				MaxResponseHeaderBytes: 64 << 10,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Close 는 유휴 연결을 닫습니다.
func (p *Prober) Close() { p.cli.CloseIdleConnections() }

// EnableKeepAlive 는 연결 재사용을 켭니다 (기본은 요청마다 새 연결). 실제 Client 와 같은 조건을 재현할 때 씁니다.
func (p *Prober) EnableKeepAlive() {
	if tr, ok := p.cli.Transport.(*http.Transport); ok {
		tr.DisableKeepAlives = false
	}
}

// Dial 은 TCP 연결만 시험합니다 (hostport 에 포트가 없으면 443).
func (p *Prober) Dial() (time.Duration, error) {
	addr := p.hostport
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(strings.Trim(addr, "[]"), "443")
	}
	start := time.Now()
	c, err := net.DialTimeout("tcp", addr, p.timeout)
	if err != nil {
		return 0, err
	}
	_ = c.Close()
	return time.Since(start), nil
}

// SetSession 은 방금 만든 세션 URI 를 등록합니다 (SessionService/Sessions/<id> 꼴만 받음). 등록해야 DELETE 가 나갑니다.
func (p *Prober) SetSession(loc string) bool {
	if i := strings.Index(loc, "/redfish/"); i >= 0 {
		loc = loc[i:]
	}
	clean, _, err := normalizePath(loc)
	if err != nil || !reProbeSession.MatchString(clean) {
		return false
	}
	p.sess = clean
	return true
}

// Session 은 등록된 세션 URI 입니다 (없으면 "").
func (p *Prober) Session() string { return p.sess }

func (p *Prober) allowed(method, raw string) (string, error) {
	clean, send, err := normalizePath(raw)
	if err != nil {
		return "", notAllowed(method, raw, err.Error())
	}
	lc := strings.ToLower(clean)
	if lc != "/redfish" && !strings.HasPrefix(lc, "/redfish/") {
		return "", notAllowed(method, raw, "/redfish 밖의 경로")
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		for _, s := range strings.Split(lc, "/") {
			if deniedGetSeg(s) {
				return "", notAllowed(method, raw, "로그/Actions 경로는 조회하지 않음")
			}
		}
	case http.MethodPost:
		if lc != "/redfish/v1/sessionservice/sessions" {
			return "", notAllowed(method, raw, "세션 생성 외 POST 금지")
		}
	case http.MethodDelete:
		if p.sess == "" || clean != p.sess || !reProbeSession.MatchString(lc) {
			return "", notAllowed(method, raw, "이 진단이 만든 세션 외 DELETE 금지")
		}
	default:
		return "", notAllowed(method, raw, "허용되지 않은 메서드")
	}
	return send, nil
}

// Send 는 허용 검사를 거쳐 요청 1건을 보냅니다. 응답 본문은 4MB 까지 읽습니다.
func (p *Prober) Send(r ProbeReq) ProbeResp {
	send, err := p.allowed(r.Method, r.Path)
	if err != nil {
		return ProbeResp{Err: err}
	}
	out := ProbeResp{Sent: send}
	var rd io.Reader
	if r.Body != nil {
		rd = bytes.NewReader(r.Body)
	}
	u := url.URL{Scheme: "https", Host: p.hostport, Path: send}
	req, err := http.NewRequest(r.Method, u.String(), rd)
	if err != nil {
		out.Err = errors.New("요청 생성 실패")
		return out
	}
	if !r.NoAccept {
		req.Header.Set("Accept", "application/json")
	}
	if !r.NoOData {
		req.Header.Set("OData-Version", "4.0")
	}
	ua := "biostool-trs/1"
	if r.UASet {
		ua = r.UA
	}
	req.Header.Set("User-Agent", ua)
	if r.Body != nil {
		ct := r.ContentType
		if ct == "" {
			ct = "application/json"
		}
		req.Header.Set("Content-Type", ct)
	}
	switch r.Auth {
	case "basic":
		req.SetBasicAuth(r.User, r.Pass)
	case "token":
		req.Header.Set("X-Auth-Token", r.Token)
	}
	start := time.Now()
	resp, err := p.cli.Do(req)
	out.Elapsed = time.Since(start)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		out.Err = err
		return out
	}
	defer resp.Body.Close()
	out.Status, out.Proto, out.Header, out.TLS = resp.StatusCode, resp.Proto, resp.Header, resp.TLS
	out.Body, _ = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return out
}
