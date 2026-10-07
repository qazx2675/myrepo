// Package mockbmc 는 Redfish BMC 를 흉내 내는 mock 서버입니다 (개발·검증 전용).
//
// 실장비 없이 check/set/dump/all_bios_check 와 바이너리 e2e 를 검증하기 위한 기반이며,
// 테스트(httptest)와 별도 실행파일(cmd/mockbmc)이 같이 씁니다. 실제 BMC 에 접속하지 않습니다.
//
// # 트리 파일 규칙 (덤프 디렉터리 레이아웃과 동일)
//
// treeDir 아래의 JSON 파일을 요청 경로에 대응시킵니다. 경로 뒤에 ".json" 을 붙인 것이 파일입니다.
//
//	요청 경로                                  파일
//	/redfish/v1                                <treeDir>/redfish/v1.json
//	/redfish/v1/Systems/1/Bios                 <treeDir>/redfish/v1/Systems/1/Bios.json
//	/redfish/v1/Systems/1/Bios/Settings        <treeDir>/redfish/v1/Systems/1/Bios/Settings.json
//	/redfish/v1/Registries/X.v1_0_0.json       <treeDir>/redfish/v1/Registries/X.v1_0_0.json.json
//
// 조회는 끝 슬래시와 대소문자를 무시합니다 (HPE iLO 의 "/redfish/v1/systems/1/bios/" 처럼).
// 이 규칙은 xml.sh(dump) 가 저장하는 덤프 디렉터리 레이아웃과 같아서, 덤프 디렉터리를 그대로
// treeDir 로 넘겨 mock 으로 재생할 수 있습니다. /redfish/v1 밖의 파일과 .json 이 아닌 파일은 무시합니다.
//
// # 동작 요약
//
//   - 세션: POST SessionService/Sessions (201 + X-Auth-Token + Location), DELETE 세션, 토큰 없음/틀림은 401.
//     Basic 인증도 받으며, Options.NoSessions 면 세션 POST 가 404 라서 클라이언트의 Basic 폴백을 시험할 수 있다.
//   - 쓰기(상태 유지): Bios 리소스의 @Redfish.Settings.SettingsObject 로 선언된 경로에 대한 PATCH 의
//     Attributes 를 메모리 pending 에 병합한다 (GET Settings 에 반영, 현재 Bios 의 Attributes 는 불변).
//     레지스트리(RegistryEntries.Attributes) 의 이름·ReadOnly·허용값을 어기면 400.
//     SettingsObject 가 없는 곳(Supermicro 등)에 대한 PATCH 는 405.
//     Managers/<id>/Jobs 가 트리에 있으면 POST 로 Job 을 만들 수 있다 (Dell 방식).
//   - 모든 요청을 기록한다 (Calls/Count/Writes/Sessions/LogHits).
//   - 장애 주입: 경로별 N회 5xx, 지연, 항상 401, 연결 끊기 (Fault).
//   - Farm(farm.go): 한 리스너가 접속한 로컬 IP(127.0.x.y)로 가상 BMC 수천 대를 구분한다 (트리는 공유).
package mockbmc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	root         = "/redfish/v1"
	sessionsPath = "/redfish/v1/sessionservice/sessions"
	maxReqBody   = 1 << 20
)

var reJobs = regexp.MustCompile(`^/redfish/v1/managers/[^/]+/jobs$`)

// Fault 는 경로별 장애 주입 규칙입니다. 효과는 매칭된 처음 Times 건에만 적용되고
// (0 이면 계속), 그 뒤에는 정상 동작합니다. 효과 적용 순서는 Delay → Drop → Status 입니다.
type Fault struct {
	Status int           // 0 이 아니면 이 상태 코드로 응답 (503 등. 401 이면 "항상 401")
	Times  int           // 적용 횟수 (0 이면 무제한)
	Delay  time.Duration // 응답 전 지연 (클라이언트 타임아웃 시험)
	Drop   bool          // 응답 없이 연결을 끊는다
}

// Options 는 New 옵션입니다.
type Options struct {
	User, Pass string // 필수. 세션 생성·Basic 인증에 쓰는 계정

	// NoSessions 는 세션 서비스가 없는 BMC 를 흉내 낸다 (세션 POST → 404, Basic 인증만 가능).
	NoSessions bool

	// Fail 의 키는 "경로" 또는 "METHOD 경로" 이고 대소문자·끝 슬래시를 무시합니다.
	// 끝에 "*" 를 붙이면 그 문자열로 시작하는 모든 경로에 적용합니다.
	// "/redfish/v1/Systems/*" 는 하위 경로만, "/redfish/v1/Systems*" 는 Systems 자신도 포함합니다.
	Fail map[string]Fault

	// Logf 가 있으면 요청이 끝날 때마다 "METHOD 경로 -> 상태" 를 남긴다 (비밀번호·본문 없음).
	Logf func(format string, args ...interface{})
}

// Call 은 mock 이 받은 요청 1건의 기록입니다.
type Call struct {
	Method string
	Path   string // 받은 그대로의 경로 (대소문자·끝 슬래시 유지)
	Query  string
	Status int    // 응답 상태. 0 은 연결을 끊었거나 응답 전에 클라이언트가 포기한 경우
	Body   string // PATCH 와 세션 아닌 POST 의 본문만 기록 (세션 POST 는 비밀번호가 있어 제외)
}

// SessionStats 는 세션 집계입니다.
type SessionStats struct {
	Created, Deleted, LoginFailed int
}

// Job 은 POST Managers/<id>/Jobs 로 만들어진 Job 기록입니다.
type Job struct {
	ID       string // JID_000000000001
	Location string // 받은 경로 기준의 Job URI
	Target   string // TargetSettingsURI
	Body     string
	Type     string // JobType (비면 BIOSConfiguration)
	State    string // JobState (비면 Scheduled)
}

type session struct {
	id, user string
}

type faultRule struct {
	method string // "" 이면 모든 메서드
	path   string // 소문자, 끝 슬래시 없음
	prefix bool
	f      Fault
	used   int
}

// Tree 는 파싱이 끝난 mock 트리입니다. 읽기 전용이라 여러 Host(Farm 의 수천 대)가 공유합니다.
type Tree struct {
	m         map[string][]byte // 소문자 경로 → JSON (불변)
	biosPaths []string          // 트리에 있는 .../systems/<id>/bios 경로
}

// LoadTree 는 treeDir 의 JSON 트리를 한 번 읽어 Tree 로 만듭니다.
func LoadTree(treeDir string) (*Tree, error) {
	m, err := loadTree(treeDir)
	if err != nil {
		return nil, err
	}
	t := &Tree{m: m}
	for k := range m {
		if strings.HasPrefix(k, root+"/systems/") && strings.HasSuffix(k, "/bios") && strings.Count(k, "/") == 5 {
			t.biosPaths = append(t.biosPaths, k)
		}
	}
	sort.Strings(t.biosPaths)
	return t, nil
}

// Host 는 mock BMC 1대의 상태와 요청 처리 로직입니다 (단일 Server 와 Farm 이 공유).
// 트리는 공유(읽기 전용)하고, 호스트별로 달라지는 것(덮어쓰기·Pending·세션·호출 기록·장애 주입)만 개별로 둡니다.
// 모든 메서드는 동시에 호출해도 안전합니다.
type Host struct {
	opts      Options
	tree      map[string][]byte // 소문자 경로 → JSON (불변, Tree 와 공유)
	biosPaths []string          // 트리에 있는 .../systems/<id>/bios 경로 (Tree 와 공유)
	done      chan struct{}     // 닫히면 지연 중인 요청을 끝낸다 (소유자가 닫음)

	mu        sync.Mutex
	over      map[string][]byte // 런타임 덮어쓰기 (nil 값이면 삭제된 리소스)
	calls     []Call
	logHits   int
	sessions  map[string]*session // 토큰 → 세션
	sessSeq   int
	sessStats SessionStats
	pending   map[string]map[string]interface{} // 소문자 Settings 경로 → 속성
	jobs      []Job
	rules     []*faultRule
}

// Server 는 mock BMC 1대를 단독 서버로 띄우는 래퍼입니다 (기록 조회·상태 변경은 Host 의 메서드).
type Server struct {
	*Host
	once     sync.Once
	ts       *httptest.Server // ts, hs 는 Host.mu 로 보호
	hs       *http.Server
	serveErr chan error
}

func newHost(t *Tree, opts Options, done chan struct{}) *Host {
	h := &Host{
		opts:      opts,
		tree:      t.m,
		biosPaths: t.biosPaths,
		done:      done,
		over:      map[string][]byte{},
		sessions:  map[string]*session{},
		pending:   map[string]map[string]interface{}{},
	}
	for k, f := range opts.Fail {
		h.addRule(k, f)
	}
	return h
}

// New 는 treeDir 의 JSON 트리를 읽어 서버를 만듭니다 (네트워크는 아직 열지 않음).
func New(treeDir string, opts Options) (*Server, error) {
	if opts.User == "" || opts.Pass == "" {
		return nil, errors.New("mockbmc: Options.User/Pass 가 필요합니다")
	}
	tree, err := LoadTree(treeDir)
	if err != nil {
		return nil, err
	}
	return &Server{Host: newHost(tree, opts, make(chan struct{})), serveErr: make(chan error, 1)}, nil
}

// loadTree 는 위 "트리 파일 규칙" 대로 파일을 읽는다.
func loadTree(dir string) (map[string][]byte, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("mockbmc: 트리 디렉터리를 열 수 없습니다: %s", dir)
	}
	tree := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".json") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		key := strings.ToLower("/" + rel[:len(rel)-len(".json")])
		if key != root && !strings.HasPrefix(key, root+"/") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !json.Valid(b) {
			return fmt.Errorf("mockbmc: JSON 형식 오류: %s", rel)
		}
		if _, dup := tree[key]; dup {
			return fmt.Errorf("mockbmc: 대소문자만 다른 중복 파일: %s", rel)
		}
		tree[key] = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, ok := tree[root]; !ok {
		return nil, fmt.Errorf("mockbmc: ServiceRoot(redfish/v1.json) 가 없습니다: %s", dir)
	}
	return tree, nil
}

// normPath 는 비교용 경로 (소문자, 끝 슬래시 없음).
func normPath(p string) string {
	if p == "" {
		return "/"
	}
	return path.Clean(strings.ToLower(p))
}

// isLogPath 는 로그 관련 경로인지 본다 (redfish.go 의 GET 거부 규칙과 같은 기준).
func isLogPath(np string) bool {
	for _, seg := range strings.Split(np, "/") {
		switch seg {
		case "logservices", "logservice", "sel", "entries":
			return true
		}
		if strings.HasSuffix(seg, "log") || strings.HasSuffix(seg, "logs") {
			return true
		}
	}
	return false
}

// ---- 기록 조회 ----

// Calls 는 받은 요청 전체의 복사본입니다 (도착 순서).
func (s *Host) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// CallCount 는 받은 요청의 총 수입니다.
func (s *Host) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// Count 는 method(빈 문자열이면 전부)와 경로 접두(대소문자 무시)가 맞는 요청 수입니다.
func (s *Host) Count(method, pathPrefix string) int {
	pre := strings.ToLower(pathPrefix)
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		if (method == "" || strings.EqualFold(c.Method, method)) && strings.HasPrefix(strings.ToLower(c.Path), pre) {
			n++
		}
	}
	return n
}

// Writes 는 BMC 에 변경을 시도한 요청 수입니다: PATCH, 세션 생성이 아닌 POST, 세션 삭제가 아닌 DELETE.
// 상태 코드와 무관하게 mock 에 도달한 것을 센다 ("쓰기 호출 0건" 확인용).
func (s *Host) Writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		np := normPath(c.Path)
		switch strings.ToUpper(c.Method) {
		case http.MethodPatch, http.MethodPut:
			n++
		case http.MethodPost:
			if np != sessionsPath {
				n++
			}
		case http.MethodDelete:
			if !strings.HasPrefix(np, sessionsPath+"/") {
				n++
			}
		}
	}
	return n
}

// LogHits 는 로그 경로(LogServices, SEL, Entries, *Log, *Logs 세그먼트)에 온 요청 수입니다.
// 메서드·인증 여부와 무관하게 센다. 정상 클라이언트라면 항상 0 이어야 한다.
func (s *Host) LogHits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logHits
}

// Sessions 는 세션 생성/삭제/로그인 실패 수입니다.
func (s *Host) Sessions() SessionStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessStats
}

// Pending 은 Settings 경로(소문자)별 pending 속성의 복사본입니다.
func (s *Host) Pending() map[string]map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]map[string]interface{}{}
	for k, m := range s.pending {
		c := map[string]interface{}{}
		for a, v := range m {
			c[a] = v
		}
		out[k] = c
	}
	return out
}

// Jobs 는 만들어진 Job 기록입니다.
func (s *Host) Jobs() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Job(nil), s.jobs...)
}

// ---- 테스트용 상태 변경 (BMC 동작이 아니라 시나리오 준비용) ----

// Modify 는 리소스(JSON 객체)를 읽어 fn 으로 고친 뒤 메모리에 덮어씁니다 (트리 파일은 그대로).
func (s *Host) Modify(p string, fn func(m map[string]interface{})) error {
	np := normPath(p)
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.resourceLocked(np)
	if !ok {
		return fmt.Errorf("mockbmc: 리소스가 없습니다: %s", p)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		return fmt.Errorf("mockbmc: JSON 객체가 아닙니다: %s", p)
	}
	fn(m)
	nb, err := json.Marshal(m)
	if err != nil {
		return err
	}
	s.over[np] = nb
	return nil
}

// SetBiosAttr 은 현재 Bios 의 Attributes 값을 바꿉니다 (value 가 nil 이면 속성 삭제).
func (s *Host) SetBiosAttr(name string, value interface{}) error {
	if len(s.biosPaths) == 0 {
		return errors.New("mockbmc: Bios 리소스가 없습니다")
	}
	for _, bp := range s.biosPaths {
		err := s.Modify(bp, func(m map[string]interface{}) {
			attrs, _ := m["Attributes"].(map[string]interface{})
			if attrs == nil {
				attrs = map[string]interface{}{}
				m["Attributes"] = attrs
			}
			if value == nil {
				delete(attrs, name)
			} else {
				attrs[name] = value
			}
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// AddJob 은 Jobs 컬렉션(jobsPath)에 Job 을 미리 만들어 둡니다 (호출 기록·Writes 에 남지 않음).
// jobType·state 가 비면 BIOSConfiguration / Scheduled 입니다. 다른 도구가 만든 Job 이 있는 상태를 흉내 낼 때 씁니다.
func (s *Host) AddJob(jobsPath, target, jobType, state string) Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fmt.Sprintf("JID_%012d", len(s.jobs)+1)
	j := Job{ID: id, Location: strings.TrimRight(jobsPath, "/") + "/" + id, Target: target, Type: jobType, State: state}
	s.jobs = append(s.jobs, j)
	return j
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Remove 는 리소스를 없앤 것처럼 404 로 응답하게 합니다.
func (s *Host) Remove(p string) {
	s.mu.Lock()
	s.over[normPath(p)] = nil
	s.mu.Unlock()
}

// SetFault 는 장애 주입 규칙을 추가하거나(같은 키면 교체) 바꿉니다. 키 형식은 Options.Fail 과 같다.
func (s *Host) SetFault(key string, f Fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addRuleLocked(key, f)
}

// ClearFaults 는 모든 장애 주입 규칙을 지웁니다.
func (s *Host) ClearFaults() {
	s.mu.Lock()
	s.rules = nil
	s.mu.Unlock()
}

func (s *Host) addRule(key string, f Fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addRuleLocked(key, f)
}

func (s *Host) addRuleLocked(key string, f Fault) {
	r := &faultRule{f: f}
	k := strings.TrimSpace(key)
	if i := strings.IndexByte(k, ' '); i > 0 && k[i+1:] != "" && strings.HasPrefix(strings.TrimSpace(k[i+1:]), "/") {
		r.method = strings.ToUpper(k[:i])
		k = strings.TrimSpace(k[i+1:])
	}
	if strings.HasSuffix(k, "*") { // 접두 규칙은 끝 슬래시를 그대로 둔다: ".../Systems/*" 는 하위 경로만, ".../Systems*" 는 자신 포함
		r.prefix = true
		r.path = strings.ToLower(strings.TrimSuffix(k, "*"))
	} else {
		r.path = strings.TrimRight(strings.ToLower(k), "/")
	}
	for i, o := range s.rules {
		if o.method == r.method && o.path == r.path && o.prefix == r.prefix {
			s.rules[i] = r
			return
		}
	}
	s.rules = append(s.rules, r)
	// 더 구체적인 규칙(긴 경로, 정확 일치)을 먼저 본다.
	sort.SliceStable(s.rules, func(i, j int) bool {
		a, b := s.rules[i], s.rules[j]
		if len(a.path) != len(b.path) {
			return len(a.path) > len(b.path)
		}
		return !a.prefix && b.prefix
	})
}

// ---- 서버 실행 ----

// Start 는 httptest TLS 서버로 실행하고 기준 URL(https://127.0.0.1:포트)을 돌려줍니다 (테스트용).
func (s *Server) Start() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ts == nil {
		s.ts = httptest.NewUnstartedServer(s)
		s.ts.StartTLS()
	}
	return s.ts.URL
}

// URL 은 Start 로 실행했을 때의 기준 URL 입니다 (실행 전이면 빈 문자열).
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ts == nil {
		return ""
	}
	return s.ts.URL
}

// ListenTLS 는 addr 에 자체서명 인증서(런타임 생성)로 TLS 서비스를 시작하고 바로 돌려줍니다.
// 돌려주는 값은 실제로 열린 주소입니다 (포트 0 지정 시 확인용). Close 로 멈춥니다.
func (s *Server) ListenTLS(addr string) (string, error) {
	cert, err := selfSignedCert()
	if err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	hs := &http.Server{
		Handler:           s,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}},
		TLSNextProto:      map[string]func(*http.Server, *tls.Conn, http.Handler){}, // HTTP/1.1 만 (연결 끊기 주입용 Hijack)
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.mu.Lock()
	s.hs = hs
	s.mu.Unlock()
	go func() { s.serveErr <- hs.ServeTLS(ln, "", "") }()
	return ln.Addr().String(), nil
}

// ListenAndServeTLS 는 ListenTLS 후 Close 될 때까지 막힙니다 (정상 종료면 nil).
func (s *Server) ListenAndServeTLS(addr string) error {
	if _, err := s.ListenTLS(addr); err != nil {
		return err
	}
	err := <-s.serveErr
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close 는 서버를 멈춥니다 (여러 번 불러도 됨). 지연 중인 요청은 즉시 끝난다.
func (s *Server) Close() {
	s.once.Do(func() { close(s.done) })
	s.mu.Lock()
	ts, hs := s.ts, s.hs
	s.mu.Unlock()
	if ts != nil {
		ts.Close()
	}
	if hs != nil {
		hs.Close()
	}
}

func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 100))
	if err != nil {
		return tls.Certificate{}, err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "mockbmc (self-signed)"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// ---- 요청 처리 ----

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// ServeHTTP 는 http.Handler 구현입니다.
func (s *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	np := normPath(r.URL.Path)
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(io.LimitReader(r.Body, maxReqBody))
	}
	c := Call{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery}
	if r.Method == http.MethodPatch || (r.Method == http.MethodPost && np != sessionsPath) {
		c.Body = string(body)
	}
	s.mu.Lock()
	s.calls = append(s.calls, c)
	idx := len(s.calls) - 1
	if isLogPath(np) {
		s.logHits++
	}
	s.mu.Unlock()

	sw := &statusWriter{ResponseWriter: w}
	s.handle(sw, r, np, body)

	s.mu.Lock()
	s.calls[idx].Status = sw.status
	s.mu.Unlock()
	if s.opts.Logf != nil {
		s.opts.Logf("%s %s -> %d", r.Method, r.URL.Path, sw.status)
	}
}

func (s *Host) handle(w *statusWriter, r *http.Request, np string, body []byte) {
	if s.applyFault(w, r, np) {
		return
	}
	if np != root && !strings.HasPrefix(np, root+"/") {
		writeErr(w, http.StatusNotFound, "ResourceMissingAtURI", "not found")
		return
	}
	if r.Method == http.MethodPost && np == sessionsPath {
		s.createSession(w, np, body)
		return
	}
	if !s.authenticated(r) {
		writeErr(w, http.StatusUnauthorized, "NoValidSession", "authentication required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.get(w, np)
	case http.MethodPatch:
		s.patch(w, np, body)
	case http.MethodPost:
		if reJobs.MatchString(np) {
			s.createJob(w, r, np, body)
			return
		}
		writeErr(w, http.StatusMethodNotAllowed, "ActionNotSupported", "POST not supported")
	case http.MethodDelete:
		if strings.HasPrefix(np, sessionsPath+"/") {
			s.deleteSession(w, strings.TrimPrefix(np, sessionsPath+"/"))
			return
		}
		writeErr(w, http.StatusMethodNotAllowed, "ActionNotSupported", "DELETE not supported")
	default:
		writeErr(w, http.StatusMethodNotAllowed, "ActionNotSupported", "method not supported")
	}
}

// applyFault 는 장애 규칙이 응답을 대신했으면 true 를 돌려준다.
func (s *Host) applyFault(w *statusWriter, r *http.Request, np string) bool {
	var f Fault
	hit := false
	s.mu.Lock()
	for _, rule := range s.rules {
		if rule.method != "" && rule.method != strings.ToUpper(r.Method) {
			continue
		}
		if rule.f.Times > 0 && rule.used >= rule.f.Times {
			continue
		}
		if (rule.prefix && strings.HasPrefix(np, rule.path)) || (!rule.prefix && np == rule.path) {
			rule.used++
			f, hit = rule.f, true
			break
		}
	}
	s.mu.Unlock()
	if !hit {
		return false
	}
	if f.Delay > 0 {
		t := time.NewTimer(f.Delay)
		defer t.Stop()
		select {
		case <-t.C:
		case <-r.Context().Done():
			return true
		case <-s.done:
			return true
		}
	}
	if f.Drop {
		if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				conn.Close()
				return true
			}
		}
		return true
	}
	if f.Status != 0 {
		writeErr(w, f.Status, "InternalError", "injected fault")
		return true
	}
	return false
}

func (s *Host) checkCreds(user, pass string) bool {
	u := subtle.ConstantTimeCompare([]byte(user), []byte(s.opts.User))
	p := subtle.ConstantTimeCompare([]byte(pass), []byte(s.opts.Pass))
	return u&p == 1
}

// authenticated: X-Auth-Token 이 있으면 그것만 본다 (틀리면 Basic 으로 넘어가지 않음).
func (s *Host) authenticated(r *http.Request) bool {
	if t := r.Header.Get("X-Auth-Token"); t != "" {
		s.mu.Lock()
		_, ok := s.sessions[t]
		s.mu.Unlock()
		return ok
	}
	if u, p, ok := r.BasicAuth(); ok {
		return s.checkCreds(u, p)
	}
	return false
}

func (s *Host) createSession(w *statusWriter, np string, body []byte) {
	if s.opts.NoSessions {
		writeErr(w, http.StatusNotFound, "ResourceMissingAtURI", "session service not available")
		return
	}
	var req struct{ UserName, Password string }
	if json.Unmarshal(body, &req) != nil {
		writeErr(w, http.StatusBadRequest, "MalformedJSON", "malformed JSON")
		return
	}
	if !s.checkCreds(req.UserName, req.Password) {
		s.mu.Lock()
		s.sessStats.LoginFailed++
		s.mu.Unlock()
		writeErr(w, http.StatusUnauthorized, "NoValidSession", "invalid credentials")
		return
	}
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	token := hex.EncodeToString(raw[:])
	s.mu.Lock()
	s.sessSeq++
	id := strconv.Itoa(s.sessSeq)
	s.sessions[token] = &session{id: id, user: req.UserName}
	s.sessStats.Created++
	s.mu.Unlock()

	loc := "/redfish/v1/SessionService/Sessions/" + id
	w.Header().Set("X-Auth-Token", token)
	w.Header().Set("Location", loc)
	writeJSON(w, http.StatusCreated, mustJSON(map[string]interface{}{
		"@odata.id": loc, "Id": id, "Name": "User Session", "UserName": req.UserName,
		"Oem": map[string]interface{}{"MockData": true},
	}))
}

func (s *Host) deleteSession(w *statusWriter, id string) {
	s.mu.Lock()
	found := false
	for tok, ss := range s.sessions {
		if ss.id == id {
			delete(s.sessions, tok)
			s.sessStats.Deleted++
			found = true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		writeErr(w, http.StatusNotFound, "ResourceMissingAtURI", "no such session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resourceLocked 는 덮어쓴 값 → 트리 순으로 리소스를 찾는다 (s.mu 보유).
func (s *Host) resourceLocked(np string) ([]byte, bool) {
	if b, ok := s.over[np]; ok {
		return b, b != nil
	}
	b, ok := s.tree[np]
	return b, ok
}

func (s *Host) get(w *statusWriter, np string) {
	s.mu.Lock()
	out, status := s.getLocked(np)
	s.mu.Unlock()
	if status != http.StatusOK {
		writeErr(w, status, "ResourceMissingAtURI", "resource not found")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Host) getLocked(np string) ([]byte, int) {
	// 동적 리소스: 세션 인스턴스, Job 인스턴스
	if strings.HasPrefix(np, sessionsPath+"/") {
		id := strings.TrimPrefix(np, sessionsPath+"/")
		for _, ss := range s.sessions {
			if ss.id == id {
				return mustJSON(map[string]interface{}{
					"@odata.id": "/redfish/v1/SessionService/Sessions/" + id, "Id": id, "UserName": ss.user,
					"Oem": map[string]interface{}{"MockData": true},
				}), http.StatusOK
			}
		}
		return nil, http.StatusNotFound
	}
	for _, j := range s.jobs {
		if normPath(j.Location) == np {
			return mustJSON(map[string]interface{}{
				"@odata.id": j.Location, "@odata.type": "#Job.v1_0_0.Job", "Id": j.ID,
				"Name": "BIOS configuration job (mock)", "JobType": orStr(j.Type, "BIOSConfiguration"), "JobState": orStr(j.State, "Scheduled"),
				"PercentComplete": 0, "TargetSettingsURI": j.Target,
				"Oem": map[string]interface{}{"MockData": true},
			}), http.StatusOK
		}
	}
	b, ok := s.resourceLocked(np)
	if !ok {
		return nil, http.StatusNotFound
	}
	// Job 컬렉션: 만들어진 Job 을 Members 에 합친다.
	if reJobs.MatchString(np) && len(s.jobs) > 0 {
		var m map[string]interface{}
		if json.Unmarshal(b, &m) == nil && m != nil {
			members, _ := m["Members"].([]interface{})
			for _, j := range s.jobs {
				if strings.HasPrefix(normPath(j.Location), np+"/") {
					members = append(members, map[string]interface{}{"@odata.id": j.Location})
				}
			}
			m["Members"], m["Members@odata.count"] = members, len(members)
			return mustJSON(m), http.StatusOK
		}
	}
	// Settings 객체: 파일의 Attributes 에 pending 을 덮어 보여 준다.
	// (pending 이 있을 때만 settingsForLocked 를 부른다: 호스트 수천 대에서 GET 마다 Bios 를 파싱하지 않게)
	if len(s.pending[np]) > 0 {
		if _, ok := s.settingsForLocked(np); ok {
			var m map[string]interface{}
			if json.Unmarshal(b, &m) == nil && m != nil {
				attrs, _ := m["Attributes"].(map[string]interface{})
				if attrs == nil {
					attrs = map[string]interface{}{}
				}
				for k, v := range s.pending[np] {
					attrs[k] = v
				}
				m["Attributes"] = attrs
				return mustJSON(m), http.StatusOK
			}
		}
	}
	return b, http.StatusOK
}

// settingsForLocked 는 np 가 어느 Bios 의 SettingsObject 인지 찾는다 (s.mu 보유).
func (s *Host) settingsForLocked(np string) (string, bool) {
	for _, bp := range s.biosPaths {
		b, ok := s.resourceLocked(bp)
		if !ok {
			continue
		}
		var d struct {
			Settings struct {
				SettingsObject struct {
					ID string `json:"@odata.id"`
				} `json:"SettingsObject"`
			} `json:"@Redfish.Settings"`
		}
		if json.Unmarshal(b, &d) != nil {
			continue
		}
		if id := d.Settings.SettingsObject.ID; id != "" && normPath(id) == np {
			return bp, true
		}
	}
	return "", false
}

type regAttr struct {
	typ      string
	readOnly bool
	values   map[string]bool
	lower    *float64
	upper    *float64
	maxLen   int
}

// registryLocked 는 Bios 의 AttributeRegistry → Registries/<id> → Location[].Uri 로 레지스트리를 읽는다.
// 찾지 못하면 ok=false 이며 이때는 속성 검증을 하지 않는다.
func (s *Host) registryLocked(biosPath string) (map[string]regAttr, bool) {
	b, ok := s.resourceLocked(biosPath)
	if !ok {
		return nil, false
	}
	var bios struct{ AttributeRegistry string }
	if json.Unmarshal(b, &bios) != nil || bios.AttributeRegistry == "" {
		return nil, false
	}
	db, ok := s.resourceLocked(normPath(root + "/Registries/" + bios.AttributeRegistry))
	if !ok {
		return nil, false
	}
	var desc struct {
		Location []struct{ Uri string }
	}
	if json.Unmarshal(db, &desc) != nil || len(desc.Location) == 0 {
		return nil, false
	}
	rb, ok := s.resourceLocked(normPath(desc.Location[0].Uri))
	if !ok {
		return nil, false
	}
	var reg struct {
		RegistryEntries struct {
			Attributes []struct {
				AttributeName string
				Type          string
				ReadOnly      bool
				LowerBound    *float64
				UpperBound    *float64
				MaxLength     int
				Value         []struct{ ValueName string }
			}
		}
	}
	if json.Unmarshal(rb, &reg) != nil {
		return nil, false
	}
	out := map[string]regAttr{}
	for _, a := range reg.RegistryEntries.Attributes {
		ra := regAttr{typ: a.Type, readOnly: a.ReadOnly, lower: a.LowerBound, upper: a.UpperBound, maxLen: a.MaxLength}
		if len(a.Value) > 0 {
			ra.values = map[string]bool{}
			for _, v := range a.Value {
				ra.values[v.ValueName] = true
			}
		}
		out[a.AttributeName] = ra
	}
	return out, true
}

type attrErr struct{ msgID, msg string }

// validateAttr 는 속성 1개의 이름·ReadOnly·값을 레지스트리 기준으로 검사한다.
func validateAttr(name string, v interface{}, reg map[string]regAttr) *attrErr {
	a, ok := reg[name]
	if !ok {
		return &attrErr{"PropertyUnknown", fmt.Sprintf("attribute %q is not in the registry", name)}
	}
	if a.readOnly {
		return &attrErr{"PropertyNotWritable", fmt.Sprintf("attribute %q is read-only", name)}
	}
	bad := &attrErr{"PropertyValueNotInList", fmt.Sprintf("value %v is not allowed for attribute %q", v, name)}
	switch a.typ {
	case "Enumeration":
		str, isStr := v.(string)
		if !isStr || !a.values[str] {
			return bad
		}
	case "Integer":
		f, isNum := v.(float64)
		if !isNum || f != math.Trunc(f) || (a.lower != nil && f < *a.lower) || (a.upper != nil && f > *a.upper) {
			return bad
		}
	case "Boolean":
		if _, isBool := v.(bool); !isBool {
			return bad
		}
	case "String", "Password":
		str, isStr := v.(string)
		if !isStr || (a.maxLen > 0 && len(str) > a.maxLen) {
			return bad
		}
	}
	return nil
}

func (s *Host) patch(w *statusWriter, np string, body []byte) {
	s.mu.Lock()
	biosPath, ok := s.settingsForLocked(np)
	s.mu.Unlock()
	if !ok {
		w.Header().Set("Allow", "GET")
		writeErr(w, http.StatusMethodNotAllowed, "ActionNotSupported", "PATCH not supported on this resource")
		return
	}
	var req struct {
		Attributes map[string]interface{} `json:"Attributes"`
	}
	if json.Unmarshal(body, &req) != nil {
		writeErr(w, http.StatusBadRequest, "MalformedJSON", "malformed JSON")
		return
	}
	if len(req.Attributes) == 0 {
		writeErr(w, http.StatusBadRequest, "PropertyMissing", "Attributes is required")
		return
	}
	names := make([]string, 0, len(req.Attributes))
	for n := range req.Attributes {
		names = append(names, n)
	}
	sort.Strings(names)

	s.mu.Lock()
	reg, regOK := s.registryLocked(biosPath)
	var errs []*attrErr
	if regOK {
		for _, n := range names {
			if e := validateAttr(n, req.Attributes[n], reg); e != nil {
				errs = append(errs, e)
			}
		}
	}
	if len(errs) == 0 {
		if s.pending[np] == nil {
			s.pending[np] = map[string]interface{}{}
		}
		for n, v := range req.Attributes {
			s.pending[np][n] = v
		}
	}
	s.mu.Unlock()

	if len(errs) > 0 { // 하나라도 잘못이면 전체 거부 (pending 은 그대로)
		info := make([]map[string]interface{}, 0, len(errs))
		for _, e := range errs {
			info = append(info, map[string]interface{}{"MessageId": "Base.1.12." + e.msgID, "Message": e.msg, "Severity": "Warning"})
		}
		writeJSON(w, http.StatusBadRequest, mustJSON(map[string]interface{}{"error": map[string]interface{}{
			"code": "Base.1.12.GeneralError", "message": "One or more properties were not valid.",
			"@Message.ExtendedInfo": info}}))
		return
	}
	writeJSON(w, http.StatusOK, mustJSON(map[string]interface{}{
		"@Message.ExtendedInfo": []map[string]interface{}{{
			"MessageId": "Base.1.12.Success", "Message": "The request completed successfully; changes apply at next reset.",
			"Severity": "OK"}}}))
}

func (s *Host) createJob(w *statusWriter, r *http.Request, np string, body []byte) {
	s.mu.Lock()
	_, jobsExist := s.resourceLocked(np)
	s.mu.Unlock()
	if !jobsExist { // 트리에 Jobs 컬렉션이 없는 벤더는 Job 생성을 지원하지 않는다.
		w.Header().Set("Allow", "GET")
		writeErr(w, http.StatusMethodNotAllowed, "ActionNotSupported", "POST not supported on this resource")
		return
	}
	var req struct{ TargetSettingsURI string }
	if json.Unmarshal(body, &req) != nil {
		writeErr(w, http.StatusBadRequest, "MalformedJSON", "malformed JSON")
		return
	}
	s.mu.Lock()
	_, isSettings := s.settingsForLocked(normPath(req.TargetSettingsURI))
	if !isSettings || req.TargetSettingsURI == "" {
		s.mu.Unlock()
		writeErr(w, http.StatusBadRequest, "PropertyValueNotInList", "TargetSettingsURI is not a BIOS settings resource")
		return
	}
	id := fmt.Sprintf("JID_%012d", len(s.jobs)+1)
	loc := strings.TrimRight(r.URL.Path, "/") + "/" + id
	s.jobs = append(s.jobs, Job{ID: id, Location: loc, Target: req.TargetSettingsURI, Body: string(body)})
	s.mu.Unlock()

	w.Header().Set("Location", loc)
	writeJSON(w, http.StatusAccepted, mustJSON(map[string]interface{}{
		"@odata.id": loc, "Id": id, "JobState": "Scheduled", "Oem": map[string]interface{}{"MockData": true}}))
}

// ---- 응답 도우미 ----

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // 고정 구조만 넘기므로 도달 불가
	}
	return b
}

func writeJSON(w http.ResponseWriter, status int, b []byte) {
	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	w.Header().Set("OData-Version", "4.0")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// writeErr 는 Redfish 오류 형식으로 응답한다. 요청 본문(비밀번호 등)은 되돌려 보내지 않는다.
func writeErr(w http.ResponseWriter, status int, msgID, msg string) {
	writeJSON(w, status, mustJSON(map[string]interface{}{"error": map[string]interface{}{
		"code": "Base.1.12.GeneralError", "message": msg,
		"@Message.ExtendedInfo": []map[string]interface{}{{"MessageId": "Base.1.12." + msgID, "Message": msg, "Severity": "Critical"}},
	}}))
}
