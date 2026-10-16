// Package mockucsm 은 Cisco UCSM/CIMC XML API(POST /nuova) 흉내 서버입니다 (시연·검증용, 실제 장비 아님).
// aaaLogin / configResolveClass / aaaLogout 만 처리하고 나머지(configConfMo 등)는 errorCode 로 거절합니다.
package mockucsm

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Server 는 JSON 으로 정의한 서버 목록을 UCSM XML API 로 서빙합니다.
type Server struct {
	User     string  `json:"user"`
	Password string  `json:"password"`
	Servers  []Entry `json:"servers"`

	// Logf 가 있으면 요청마다 한 줄 남깁니다.
	Logf func(string, ...interface{}) `json:"-"`

	mu      sync.Mutex
	cookies map[string]bool
	calls   []string
	hs      *http.Server
}

// Entry 는 서버 1대입니다. Class 는 computeBlade 또는 computeRackUnit 입니다.
// Bios 는 biosVf 클래스 이름 → 속성 이름 → 값 입니다.
type Entry struct {
	DN     string                       `json:"dn"`
	Class  string                       `json:"class"`
	Model  string                       `json:"model"`
	SPName string                       `json:"sp_name"`
	Bios   map[string]map[string]string `json:"bios"`
}

// Load 는 JSON 파일을 읽어 서버를 만듭니다.
func Load(path string) (*Server, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse 는 JSON 본문으로 서버를 만듭니다.
func Parse(b []byte) (*Server, error) {
	s := &Server{}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("JSON 해석 실패: %v", err)
	}
	if s.User == "" || s.Password == "" {
		return nil, fmt.Errorf("user / password 가 필요합니다")
	}
	for i, e := range s.Servers {
		if e.DN == "" {
			return nil, fmt.Errorf("servers[%d]: dn 이 필요합니다", i)
		}
		if e.Class != "computeBlade" && e.Class != "computeRackUnit" {
			return nil, fmt.Errorf("servers[%d]: class 는 computeBlade 또는 computeRackUnit 이어야 합니다", i)
		}
	}
	s.cookies = map[string]bool{}
	return s, nil
}

// Calls 는 받은 요청을 "메서드 [classId]" 형태로 돌려줍니다.
func (s *Server) Calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *Server) logf(f string, a ...interface{}) {
	if s.Logf != nil {
		s.Logf(f, a...)
	}
}

func esc(v string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(v))
	return b.String()
}

// ServeHTTP 는 POST /nuova 만 받습니다.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/nuova" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var req struct {
		XMLName    xml.Name
		InName     string `xml:"inName,attr"`
		InPassword string `xml:"inPassword,attr"`
		Cookie     string `xml:"cookie,attr"`
		ClassID    string `xml:"classId,attr"`
		InCookie   string `xml:"inCookie,attr"`
	}
	if err := xml.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	m := req.XMLName.Local
	call := m
	if req.ClassID != "" {
		call += " " + req.ClassID
	}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()
	s.logf("mockucsm: %s", call)

	switch m {
	case "aaaLogin":
		if req.InName != s.User || req.InPassword != s.Password {
			fmt.Fprint(w, `<aaaLogin cookie="" response="yes" errorCode="551" invocationResult="unidentified-fail" errorDescr="Authentication failed"> </aaaLogin>`)
			return
		}
		ck, err := newCookie()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.mu.Lock()
		s.cookies[ck] = true
		s.mu.Unlock()
		fmt.Fprintf(w, `<aaaLogin cookie="" response="yes" outCookie="%s" outRefreshPeriod="600" outPriv="read-only" outVersion="4.2(3b)"> </aaaLogin>`, ck)
	case "configResolveClass":
		if !s.valid(req.Cookie) {
			fmt.Fprintf(w, `<configResolveClass cookie="" response="yes" classId="%s" errorCode="552" errorDescr="Authorization required"/>`, esc(req.ClassID))
			return
		}
		fmt.Fprintf(w, `<configResolveClass cookie="%s" response="yes" classId="%s"><outConfigs>%s</outConfigs></configResolveClass>`,
			esc(req.Cookie), esc(req.ClassID), s.objects(req.ClassID))
	case "aaaLogout":
		s.mu.Lock()
		delete(s.cookies, req.InCookie)
		s.mu.Unlock()
		fmt.Fprint(w, `<aaaLogout cookie="" response="yes" outStatus="success"> </aaaLogout>`)
	default:
		s.logf("mockucsm: 지원하지 않는 요청 거절: %s", m)
		fmt.Fprintf(w, `<%s cookie="" response="yes" errorCode="103" invocationResult="unidentified-fail" errorDescr="mockucsm: %s 는 지원하지 않습니다 (읽기 전용 모의 서버)"/>`,
			esc(m), esc(m))
	}
}

func (s *Server) valid(ck string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ck != "" && s.cookies[ck]
}

func newCookie() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d/ck-%x", time.Now().Unix(), b), nil
}

// objects 는 classId 의 인스턴스 XML 조각을 만듭니다 (모르는 클래스는 빈 문자열).
func (s *Server) objects(class string) string {
	var sb strings.Builder
	switch class {
	case "computeBlade", "computeRackUnit":
		for _, e := range s.Servers {
			if e.Class == class {
				fmt.Fprintf(&sb, `<%s dn="%s" model="%s"/>`, class, esc(e.DN), esc(e.Model))
			}
		}
	case "lsServer":
		for _, e := range s.Servers {
			if e.SPName != "" {
				fmt.Fprintf(&sb, `<lsServer dn="org-root/ls-%s" name="%s" pnDn="%s"/>`, esc(e.SPName), esc(e.SPName), esc(e.DN))
			}
		}
	default:
		if !strings.HasPrefix(class, "biosVf") {
			return ""
		}
		for _, e := range s.Servers {
			attrs, ok := e.Bios[class]
			if !ok {
				continue
			}
			names := make([]string, 0, len(attrs))
			for k := range attrs {
				names = append(names, k)
			}
			sort.Strings(names)
			fmt.Fprintf(&sb, `<%s dn="%s/bios/bios-settings/%s"`, class, esc(e.DN), esc(strings.TrimPrefix(class, "biosVf")))
			for _, k := range names {
				fmt.Fprintf(&sb, ` %s="%s"`, k, esc(attrs[k]))
			}
			sb.WriteString(`/>`)
		}
	}
	return sb.String()
}

// ListenTLS 는 자체서명 인증서로 TLS 서비스를 시작하고 실제 주소를 돌려줍니다.
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
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.mu.Lock()
	s.hs = hs
	s.mu.Unlock()
	go func() { _ = hs.ServeTLS(ln, "", "") }()
	return ln.Addr().String(), nil
}

// Close 는 서버를 멈춥니다.
func (s *Server) Close() {
	s.mu.Lock()
	hs := s.hs
	s.mu.Unlock()
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
		Subject:               pkix.Name{CommonName: "mockucsm (self-signed)"},
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
