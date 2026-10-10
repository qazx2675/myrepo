// Package ucsmxml 은 Cisco UCS Manager XML API(POST https://<UCSM>/nuova)용 읽기 전용 클라이언트입니다.
//
// biostool ucsm(cmd/biostool/ucsm.go)만 씁니다. 이 패키지가 UCSM 으로 보낼 수 있는 요청은
// aaaLogin(로그인 1회) · configResolveClass(computeBlade 와 허용된 bios* 클래스, inHierarchical="false") ·
// aaaLogout(자기 쿠키) 세 가지뿐입니다.
//
//   - 밖에 공개된 호출은 Login·ResolveClass·Logout 뿐이고, 요청 본문은 이 패키지 안에서만 만든다.
//     (configConfMo·configConfMos·configResolveDn 같은 설정·변경·임의 조회 호출을 만들 수 있는 공개 함수가 없음)
//   - 모든 요청은 post 한 경로만 지나며, post 는 전송 직전에 gate(단일 관문)로 본문을 다시 해석해
//     루트 메서드·속성 목록·쿠키·classId 를 검사한다. 통과하지 못하면 네트워크로 나가지 않고 ErrNotAllowed.
//   - 비밀번호·쿠키는 클로저 안에만 두고(fmt 로 Client 를 찍어도 드러나지 않음), 오류 Detail 에서 지운다.
//
// cmd/biostool 의 Redfish 클라이언트(redfish.go)와는 독립입니다: Redfish 경로(/redfish/v1)로는 보낼 수 없고
// 경로는 항상 /nuova 하나입니다.
package ucsmxml

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// 호스트(UCSM) 단위 오류 상태. cmd/biostool 의 같은 이름 상태와 같은 문자열입니다.
const (
	StatusAuthFail    = "AUTH_FAIL"   // aaaLogin 거부(errorCode) 또는 HTTP 401/403
	StatusUnreachable = "UNREACHABLE" // 연결 거부 / no route / DNS
	StatusTimeout     = "TIMEOUT"
	StatusXMLError    = "XML_ERROR" // 응답이 XML 이 아님, 루트가 다름, errorCode, HTTP 오류
)

// 허용된 XML API 메서드와 항상 허용되는 서버 식별 클래스.
const (
	MethodLogin   = "aaaLogin"
	MethodResolve = "configResolveClass"
	MethodLogout  = "aaaLogout"
	MethodConf    = "configConfMo" // 계정 모드(Options.Account)에서만, aaaUser 생성·비밀번호 변경·삭제로 제한
	BladeClass    = "computeBlade"
	UserClass     = "aaaUser" // 계정 모드에서만 configResolveClass 로 읽을 수 있음
	UserRole      = "admin"   // 생성하는 계정에 부여하는 UCSM 역할 (관리자)
)

const (
	endpoint       = "/nuova"
	maxBody        = 8 << 20 // 응답 본문 상한 8MB
	detailLimit    = 200     // Error.Detail 최대 글자 수
	defaultTimeout = 20 * time.Second
)

// ErrNotAllowed 는 허용목록 밖의 요청이라 보내지 않았음을 뜻합니다 (errors.Is 로 확인).
var ErrNotAllowed = errors.New("허용목록에 없는 UCSM XML 호출이라 보내지 않았습니다")

// ErrNotLoggedIn 은 로그인(쿠키) 없이 조회하려 했음을 뜻합니다.
var ErrNotLoggedIn = errors.New("UCSM 에 로그인하지 않은 상태입니다")

// reBiosClass 는 configResolveClass 로 읽어도 되는 추가 클래스 형식입니다 (BIOS 토큰·설정 클래스만).
var reBiosClass = regexp.MustCompile(`^bios[A-Za-z0-9]+$`)

// Error 는 UCSM 통신 실패를 상태 코드로 분류한 오류입니다.
// Detail 에는 비밀번호·쿠키가 들어가지 않으며 200자로 잘립니다.
type Error struct {
	Status string // StatusXxx
	HTTP   int    // HTTP 상태 코드 (전송 실패·정상 200 이면 0)
	Code   string // UCSM 이 돌려준 errorCode (API 오류일 때만, 아니면 "")
	Detail string
}

func (e *Error) Error() string {
	if e.HTTP != 0 {
		return fmt.Sprintf("%s (HTTP %d): %s", e.Status, e.HTTP, e.Detail)
	}
	return e.Status + ": " + e.Detail
}

// Options 는 New 옵션입니다.
type Options struct {
	BaseURL  string // https://<UCSM 주소>[:포트] (경로는 무시)
	User     string
	Pass     string
	Insecure bool          // TLS 인증서 검증 생략 (자체서명 UCSM)
	Timeout  time.Duration // 요청 1건 타임아웃 (0 이면 20초)
	// Classes 는 configResolveClass 로 읽을 추가 classId 입니다. `bios` 로 시작하는 영숫자만 받아들이고
	// 나머지는 조용히 버립니다 (버린 클래스는 ResolveClass 에서 ErrNotAllowed). computeBlade 는 항상 허용.
	Classes []string
	// Account 는 계정 모드입니다 (biostool account -ucsm 만 켠다). 켜면 aaaUser 조회와
	// configConfMo(sys/user-ext/user-<이름> 의 생성·비밀번호 변경·삭제)만 추가로 허용됩니다.
	Account bool
}

// Object 는 configResolveClass 응답의 인스턴스 1개입니다 (속성 이름 → 값).
type Object struct {
	Class string
	Attrs map[string]string
}

// secrets 는 클로저 안에만 둔다 (redfish.go 의 credentials 와 같은 이유: fmt 가 함수 값은 주소만 찍음).
type secrets struct {
	user, pass, cookie string
}

// Client 는 UCSM 1개용 클라이언트입니다. 메서드는 내부 잠금으로 직렬화됩니다.
type Client struct {
	mu        sync.Mutex
	base      *url.URL
	baseErr   error
	hc        *http.Client
	sec       func() *secrets
	allowed   map[string]bool // configResolveClass classId 허용목록
	loginDone bool
	loginErr  error
	account   bool            // 계정 모드 (configConfMo 허용)
	targets   map[string]bool // 비밀번호 변경이 허용 등록된 사용자 이름
	created   map[string]bool // 이번 클라이언트가 만든 사용자 (삭제 롤백 허용)
	extra     []string        // 오류 Detail 에서 지울 추가 비밀 (새 계정 비밀번호)
}

// New 는 클라이언트를 만듭니다. 접속은 하지 않습니다. BaseURL 오류는 Login 에서 돌려줍니다.
func New(o Options) *Client {
	s := &secrets{user: o.User, pass: o.Pass}
	c := &Client{
		sec:     func() *secrets { return s },
		allowed: map[string]bool{BladeClass: true},
	}
	for _, k := range o.Classes {
		if reBiosClass.MatchString(k) {
			c.allowed[k] = true
		}
	}
	if o.Account {
		c.account = true
		c.allowed[UserClass] = true
		c.targets = map[string]bool{}
		c.created = map[string]bool{}
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
	c.hc = &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                  nil, // 환경변수 프록시를 거치지 않는다 (관리망 직접 접속)
			DialContext:            (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:    timeout,
			TLSClientConfig:        &tls.Config{InsecureSkipVerify: o.Insecure},
			MaxIdleConnsPerHost:    1,
			IdleConnTimeout:        30 * time.Second,
			MaxResponseHeaderBytes: 64 << 10,
		},
		// 리다이렉트는 따라가지 않는다 (자격증명이 다른 곳으로 가는 것을 막음).
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c
}

// Login 은 aaaLogin 을 최대 1번만 보냅니다 (실패도 기억해 다시 보내지 않음 → 계정 잠금 방지).
// 로그인 거부(errorCode, HTTP 401/403)는 StatusAuthFail 이며 재시도하지 않습니다.
func (c *Client) Login() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseErr != nil {
		return c.baseErr
	}
	if c.loginDone {
		return c.loginErr
	}
	c.loginDone = true
	c.loginErr = c.login()
	return c.loginErr
}

func (c *Client) login() error {
	s := c.sec()
	body := `<aaaLogin inName="` + esc(s.user) + `" inPassword="` + esc(s.pass) + `"/>`
	root, err := c.post(MethodLogin, []byte(body))
	if err != nil {
		return err
	}
	ck := root.attr("outCookie")
	if ck == "" {
		return c.newErr(StatusXMLError, 0, "", "aaaLogin 응답에 outCookie 가 없습니다")
	}
	s.cookie = ck
	return nil
}

// ResolveClass 는 configResolveClass(inHierarchical="false") 1회로 classID 의 인스턴스를 모두 읽습니다.
// 허용목록에 없는 classID 는 로그인 여부와 관계없이 아무것도 보내지 않고 ErrNotAllowed 입니다.
// UCSM 이 errorCode 를 돌려주면 Status=XML_ERROR, Code=errorCode 인 *Error 입니다.
func (c *Client) ResolveClass(classID string) ([]Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.allowed[classID] {
		return nil, notAllowed(MethodResolve, "classId "+clip(classID)+" 는 허용목록에 없음")
	}
	if c.baseErr != nil {
		return nil, c.baseErr
	}
	ck := c.sec().cookie
	if ck == "" {
		if c.loginErr != nil {
			return nil, c.loginErr
		}
		return nil, ErrNotLoggedIn
	}
	body := `<configResolveClass cookie="` + esc(ck) + `" inHierarchical="false" classId="` + esc(classID) + `"/>`
	root, err := c.post(MethodResolve, []byte(body))
	if err != nil {
		return nil, err
	}
	var out []Object
	for _, ch := range root.Children {
		if ch.XMLName.Local != "outConfigs" {
			continue
		}
		for _, o := range ch.Children {
			if o.XMLName.Local == classID {
				out = append(out, Object{Class: classID, Attrs: o.attrMap()})
			}
		}
	}
	return out, nil
}

// ---- 계정 모드 (Options.Account) ----

var errNotAccountMode = errors.New("계정 모드가 아닌 클라이언트입니다")

// AddSecret 은 오류 Detail 에서 지울 비밀 값(새 계정 비밀번호)을 등록합니다.
func (c *Client) AddSecret(s string) {
	if s == "" {
		return
	}
	c.mu.Lock()
	c.extra = append(c.extra, s)
	c.mu.Unlock()
}

// ListUsers 는 UCSM 로컬 사용자 이름 목록입니다 (configResolveClass aaaUser, 계정 모드 전용).
func (c *Client) ListUsers() ([]string, error) {
	if !c.account {
		return nil, errNotAccountMode
	}
	objs, err := c.ResolveClass(UserClass)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, o := range objs {
		if n := o.Attrs["name"]; n != "" {
			out = append(out, n)
		}
	}
	return out, nil
}

// AllowUserTarget 은 비밀번호 변경을 허용할 사용자 이름을 등록합니다 (계정 모드 전용).
func (c *Client) AllowUserTarget(name string) error {
	if !c.account {
		return errNotAccountMode
	}
	if !reUserName.MatchString(name) {
		return notAllowed(MethodConf, "사용자 이름 형식이 아님")
	}
	c.mu.Lock()
	c.targets[name] = true
	c.mu.Unlock()
	return nil
}

// confBody 는 aaaUser 1개에 대한 configConfMo 요청 본문입니다 (속성 값은 모두 이스케이프).
func (c *Client) confBody(name, status, inner string, attrs string) []byte {
	dn := "sys/user-ext/user-" + name
	return []byte(`<configConfMo cookie="` + esc(c.sec().cookie) + `" dn="` + esc(dn) + `" inHierarchical="false">` +
		`<inConfig><aaaUser name="` + esc(name) + `" ` + attrs + `dn="` + esc(dn) + `" status="` + status + `">` + inner + `</aaaUser></inConfig></configConfMo>`)
}

// CreateUser 는 로컬 사용자를 새로 만듭니다 (status="created": 같은 이름이 있으면 UCSM 이 거부, 역할 admin).
// 성공하면 이 클라이언트가 만든 사용자로 기억해 DeleteUser(롤백)를 허용합니다.
func (c *Client) CreateUser(name, pwd string) error {
	if !c.account {
		return errNotAccountMode
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseErr != nil {
		return c.baseErr
	}
	if c.sec().cookie == "" {
		return ErrNotLoggedIn
	}
	c.extra = append(c.extra, pwd)
	body := c.confBody(name, "created", `<aaaUserRole name="`+UserRole+`" status="created"/>`,
		`pwd="`+esc(pwd)+`" accountStatus="active" expires="no" firstName="" lastName="" email="" phone="" `)
	if _, err := c.post(MethodConf, body); err != nil {
		return err
	}
	c.created[name] = true
	return nil
}

// SetUserPassword 는 허용 등록된(AllowUserTarget) 사용자의 비밀번호만 바꿉니다 (status="modified").
func (c *Client) SetUserPassword(name, pwd string) error {
	if !c.account {
		return errNotAccountMode
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseErr != nil {
		return c.baseErr
	}
	if c.sec().cookie == "" {
		return ErrNotLoggedIn
	}
	c.extra = append(c.extra, pwd)
	body := c.confBody(name, "modified", "", `pwd="`+esc(pwd)+`" `)
	_, err := c.post(MethodConf, body)
	return err
}

// DeleteUser 는 이 클라이언트가 CreateUser 로 만든 사용자만 삭제합니다 (롤백 전용).
func (c *Client) DeleteUser(name string) error {
	if !c.account {
		return errNotAccountMode
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseErr != nil {
		return c.baseErr
	}
	if c.sec().cookie == "" {
		return ErrNotLoggedIn
	}
	body := c.confBody(name, "deleted", "", "")
	_, err := c.post(MethodConf, body)
	return err
}

// Logout 은 자기 쿠키로 aaaLogout 을 보냅니다 (로그인하지 않았으면 아무것도 보내지 않음, 멱등).
// 끝나면 쿠키·비밀번호를 지우고 유휴 연결을 닫습니다.
func (c *Client) Logout() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sec()
	if c.baseErr != nil || s.cookie == "" {
		return nil
	}
	body := `<aaaLogout inCookie="` + esc(s.cookie) + `"/>`
	_, err := c.post(MethodLogout, []byte(body))
	s.cookie, s.pass = "", ""
	c.hc.CloseIdleConnections()
	return err
}

// ---- 단일 관문 ----

func notAllowed(method, reason string) error {
	return fmt.Errorf("%w: %s (%s)", ErrNotAllowed, clip(method), reason)
}

// clip 은 메시지에 넣을 이름을 64글자로 자르고 따옴표로 감쌉니다.
func clip(s string) string {
	if utf8.RuneCountInString(s) > 64 {
		s = string([]rune(s)[:64]) + "..."
	}
	return fmt.Sprintf("%q", s)
}

// reqNode 는 gate 가 해석한 요청 XML 의 요소입니다 (속성과 자식 요소만).
type reqNode struct {
	name  string
	attrs map[string]string
	kids  []*reqNode
}

// parseRequest 는 요청 본문을 요소 트리로 해석합니다. 루트는 정확히 1개이고, 주석·처리명령·DOCTYPE·
// 네임스페이스·중복 속성·요소 사이 문자 데이터는 모두 거부합니다.
func parseRequest(body []byte) (*reqNode, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var stack []*reqNode
	var root *reqNode
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, notAllowed("?", "요청 본문을 XML 로 해석할 수 없음")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != "" {
				return nil, notAllowed(t.Name.Local, "네임스페이스 요소 금지")
			}
			n := &reqNode{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space != "" {
					return nil, notAllowed(n.name, "네임스페이스 속성 금지")
				}
				if _, dup := n.attrs[a.Name.Local]; dup {
					return nil, notAllowed(n.name, "중복 속성 금지")
				}
				n.attrs[a.Name.Local] = a.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, notAllowed(n.name, "요청에는 루트 요소가 정확히 1개만 있어야 함")
				}
				root = n
			} else {
				p := stack[len(stack)-1]
				p.kids = append(p.kids, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(bytes.TrimSpace(t)) != 0 {
				return nil, notAllowed("?", "요소 밖·안의 문자 데이터 금지")
			}
		default: // 주석, 처리명령(<?xml ...?>), DOCTYPE 등
			return nil, notAllowed("?", "주석·처리명령·DOCTYPE 금지")
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, notAllowed("?", "요청 요소가 없거나 닫히지 않음")
	}
	return root, nil
}

func (n *reqNode) keysAre(keys ...string) bool {
	if len(n.attrs) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := n.attrs[k]; !ok {
			return false
		}
	}
	return true
}

// gate 는 전송 직전의 단일 관문입니다. 보낼 본문을 다시 해석해
//
//   - 루트가 aaaLogin / configResolveClass / aaaLogout / configConfMo(계정 모드 한정) 중 하나이고
//   - 속성이 메서드별 고정 목록과 정확히 같으며 (aaaLogin: inName·inPassword,
//     configResolveClass: cookie·classId·inHierarchical, aaaLogout: inCookie,
//     configConfMo: cookie·dn·inHierarchical)
//   - 앞의 세 메서드는 자식 요소가 없고, configResolveClass 는 자기 쿠키 · inHierarchical="false" ·
//     허용된 classId, aaaLogout 은 자기 쿠키일 때만 nil 입니다.
//   - configConfMo 는 계정 모드에서만, sys/user-ext/user-<이름> 의 aaaUser 하나에 대한
//     생성(created, 역할 admin) · 비밀번호 변경(modified, 허용 등록된 이름만) · 삭제(deleted, 이번에 만든 이름만)로 제한됩니다.
//
// 그 밖(configConfMos, configResolveDn, configResolveChildren, 전원·재부팅 계열 등)은 모두 거부합니다.
// 거부 사유에는 속성 값(비밀번호·쿠키)을 넣지 않습니다.
func (c *Client) gate(body []byte) error {
	root, err := parseRequest(body)
	if err != nil {
		return err
	}
	method := root.name
	ck := c.sec().cookie
	switch method {
	case MethodLogin:
		if !root.keysAre("inName", "inPassword") || len(root.kids) != 0 {
			return notAllowed(method, "속성은 inName·inPassword 뿐이고 자식 요소가 없어야 함")
		}
	case MethodResolve:
		if !root.keysAre("cookie", "classId", "inHierarchical") || len(root.kids) != 0 {
			return notAllowed(method, "속성은 cookie·classId·inHierarchical 뿐이고 자식 요소가 없어야 함")
		}
		if ck == "" || root.attrs["cookie"] != ck {
			return notAllowed(method, "자기 세션 쿠키가 아님")
		}
		if root.attrs["inHierarchical"] != "false" {
			return notAllowed(method, `inHierarchical 은 "false" 만 허용`)
		}
		if !c.allowed[root.attrs["classId"]] {
			return notAllowed(method, "classId "+clip(root.attrs["classId"])+" 는 허용목록에 없음")
		}
	case MethodLogout:
		if !root.keysAre("inCookie") || len(root.kids) != 0 {
			return notAllowed(method, "속성은 inCookie 뿐이고 자식 요소가 없어야 함")
		}
		if ck == "" || root.attrs["inCookie"] != ck {
			return notAllowed(method, "자기 세션 쿠키가 아님")
		}
	case MethodConf:
		return c.gateConf(root, ck)
	default:
		return notAllowed(method, "aaaLogin·configResolveClass·aaaLogout 외 메서드 금지 (계정 모드에서만 configConfMo)")
	}
	return nil
}

// reUserName 은 UCSM 로컬 사용자 이름 형식입니다 (영문 시작, 최대 32자).
var reUserName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,31}$`)

// gateConf 는 configConfMo 요청을 검사합니다 (계정 모드 한정).
func (c *Client) gateConf(root *reqNode, ck string) error {
	const method = MethodConf
	if !c.account {
		return notAllowed(method, "계정 모드가 아니면 configConfMo(설정 변경) 금지")
	}
	if !root.keysAre("cookie", "dn", "inHierarchical") {
		return notAllowed(method, "속성은 cookie·dn·inHierarchical 뿐이어야 함")
	}
	if ck == "" || root.attrs["cookie"] != ck {
		return notAllowed(method, "자기 세션 쿠키가 아님")
	}
	if root.attrs["inHierarchical"] != "false" {
		return notAllowed(method, `inHierarchical 은 "false" 만 허용`)
	}
	dn := root.attrs["dn"]
	const prefix = "sys/user-ext/user-"
	if !strings.HasPrefix(dn, prefix) || !reUserName.MatchString(dn[len(prefix):]) {
		return notAllowed(method, "dn 은 sys/user-ext/user-<이름> 만 허용")
	}
	name := dn[len(prefix):]
	if len(root.kids) != 1 || root.kids[0].name != "inConfig" || len(root.kids[0].attrs) != 0 || len(root.kids[0].kids) != 1 {
		return notAllowed(method, "inConfig 안에 aaaUser 하나만 허용")
	}
	u := root.kids[0].kids[0]
	if u.name != "aaaUser" {
		return notAllowed(method, "aaaUser 외 객체 금지")
	}
	if u.attrs["name"] != name || u.attrs["dn"] != dn {
		return notAllowed(method, "aaaUser 의 name·dn 이 요청 dn 과 다름")
	}
	switch u.attrs["status"] {
	case "created":
		if !u.keysAre("name", "pwd", "accountStatus", "expires", "firstName", "lastName", "email", "phone", "dn", "status") ||
			u.attrs["pwd"] == "" || u.attrs["accountStatus"] != "active" || u.attrs["expires"] != "no" {
			return notAllowed(method, "생성 속성이 허용 형태가 아님")
		}
		if len(u.kids) != 1 || u.kids[0].name != "aaaUserRole" || !u.kids[0].keysAre("name", "status") ||
			u.kids[0].attrs["name"] != UserRole || u.kids[0].attrs["status"] != "created" || len(u.kids[0].kids) != 0 {
			return notAllowed(method, `생성에는 aaaUserRole name="admin" 하나만 허용`)
		}
	case "modified":
		if !u.keysAre("name", "pwd", "dn", "status") || u.attrs["pwd"] == "" || len(u.kids) != 0 {
			return notAllowed(method, "변경은 name·pwd·dn·status 뿐이어야 함 (비밀번호만)")
		}
		if !c.targets[name] {
			return notAllowed(method, "허용 등록되지 않은 사용자")
		}
	case "deleted":
		if !u.keysAre("name", "dn", "status") || len(u.kids) != 0 {
			return notAllowed(method, "삭제 속성이 허용 형태가 아님")
		}
		if !c.created[name] {
			return notAllowed(method, "이번 실행이 만든 사용자만 삭제 가능")
		}
	default:
		return notAllowed(method, "status 는 created / modified / deleted 만 허용")
	}
	return nil
}

func sameKeys(m map[string]string, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

// ---- 전송 ----

// elem 은 응답 XML 의 요소 1개입니다 (속성과 자식 요소만, 문자 데이터는 버림).
type elem struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []elem     `xml:",any"`
}

func (e *elem) attr(name string) string {
	for _, a := range e.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func (e *elem) attrMap() map[string]string {
	m := make(map[string]string, len(e.Attrs))
	for _, a := range e.Attrs {
		if _, ok := m[a.Name.Local]; !ok {
			m[a.Name.Local] = a.Value
		}
	}
	return m
}

// parseResponse 는 응답의 첫 요소를 해석합니다. encoding 선언이 UTF-8 이 아니어도 그대로 읽는다 (값은 ASCII).
func parseResponse(b []byte) (*elem, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var e elem
	if err := dec.Decode(&e); err != nil {
		return nil, err
	}
	return &e, nil
}

// post 는 이 패키지의 유일한 전송 경로입니다. c.mu 보유 상태로 호출합니다.
// gate 검사 → POST /nuova 1회(자동 재시도 없음) → HTTP 상태·XML·루트 이름·errorCode 분류.
// method 는 응답 루트로 기대하는 메서드 이름이며, aaaLogin 의 거부는 StatusAuthFail 입니다.
func (c *Client) post(method string, body []byte) (*elem, error) {
	if err := c.gate(body); err != nil {
		return nil, err
	}
	if c.base == nil {
		return nil, c.baseErr
	}
	u := url.URL{Scheme: c.base.Scheme, Host: c.base.Host, Path: endpoint}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, c.newErr(StatusXMLError, 0, "", "요청 생성 실패")
	}
	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("Accept", "text/xml")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, c.transportErr(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, c.transportErr(err)
	}
	if len(b) > maxBody {
		return nil, c.newErr(StatusXMLError, resp.StatusCode, "", "응답 본문이 8MB 를 넘습니다")
	}
	isLogin := method == MethodLogin
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		st := StatusXMLError
		if isLogin && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			st = StatusAuthFail
		}
		// 본문은 싣지 않는다 (로그인 응답에는 입력값이 되돌아올 수 있음).
		return nil, c.newErr(st, resp.StatusCode, "", fmt.Sprintf("%s 요청이 HTTP %d 로 실패했습니다", method, resp.StatusCode))
	}
	root, err := parseResponse(b)
	if err != nil {
		return nil, c.newErr(StatusXMLError, 0, "", method+" 응답이 XML 이 아닙니다")
	}
	code := root.attr("errorCode")
	if root.XMLName.Local != method {
		d := fmt.Sprintf("%s 요청의 응답 루트가 %q 입니다", method, root.XMLName.Local)
		if code != "" {
			d += fmt.Sprintf(" (errorCode=%s: %s)", code, root.attr("errorDescr"))
		}
		return nil, c.newErr(StatusXMLError, 0, code, d)
	}
	if code != "" {
		st := StatusXMLError
		if isLogin {
			// 계정 잠금 방지: 로그인 거부는 사유와 관계없이 AUTH_FAIL 로 보고 다시 시도하지 않는다.
			st = StatusAuthFail
		}
		return nil, c.newErr(st, 0, code, fmt.Sprintf("%s errorCode=%s: %s", method, code, root.attr("errorDescr")))
	}
	return root, nil
}

// transportErr 는 전송 실패를 TIMEOUT / UNREACHABLE 로 분류합니다 (url.Error 껍데기는 벗김).
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
	return c.newErr(st, 0, "", inner.Error())
}

// newErr 는 Error 를 만듭니다. Detail 에서 비밀번호·쿠키(원문과 XML 이스케이프 형태)를 지우고,
// 제어문자·비출력 문자는 "?" 로, 공백류는 공백 1칸으로 바꾼 뒤 200자로 자릅니다.
func (c *Client) newErr(status string, code int, apiCode, detail string) *Error {
	s := c.sec()
	var secretForms []string
	for _, v := range append([]string{s.pass, s.cookie}, c.extra...) {
		if v != "" {
			secretForms = append(secretForms, esc(v), escQuot(v), v)
		}
	}
	for _, v := range secretForms {
		if v != "" {
			detail = strings.ReplaceAll(detail, v, "***")
		}
	}
	detail = sanitize(detail)
	if utf8.RuneCountInString(detail) > detailLimit {
		detail = string([]rune(detail)[:detailLimit])
	}
	return &Error{Status: status, HTTP: code, Code: sanitize(apiCode), Detail: detail}
}

// esc 는 XML 속성 값 이스케이프입니다 (encoding/xml 의 EscapeText: " ' & < > 탭·개행 포함).
func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// escQuot 는 장비가 &quot; / &apos; 형태로 되돌리는 경우를 위한 이스케이프입니다 (마스킹 전용).
func escQuot(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

func sanitize(s string) string {
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
