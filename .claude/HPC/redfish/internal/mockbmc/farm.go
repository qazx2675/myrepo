package mockbmc

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// farm.go 는 한 TLS 리스너로 가상 BMC 수천 대를 흉내 내는 Farm 입니다 (수천 대 규모 시험용, 개발·검증 전용).
//
// 접속한 로컬 IP(http.LocalAddrContextKey)로 가상 BMC 를 구분합니다. 리눅스는 127.0.0.0/8 전체가
// 루프백이라 `127.0.x.y:포트` 3,000개 주소를 리스너 FD 3,000개 없이 쓸 수 있습니다 (리스너는 0.0.0.0:포트 1개).
// 트리(Tree)는 호스트 간에 공유하고, 호스트별 변경(속성 덮어쓰기·Pending·세션·호출 기록·장애 주입)만
// 개별로 보관해 메모리를 아낍니다. 요청 처리 로직은 단일 Server 와 같은 Host 를 씁니다.
// macOS·Windows 는 127.0.0.1 외의 루프백 주소가 기본으로 없어 쓸 수 없습니다.

// FarmOptions 는 NewFarm 옵션입니다.
type FarmOptions struct {
	// UnknownStatus 는 등록되지 않은 로컬 IP 로 접속했을 때의 동작입니다.
	// 0 이면 TLS 핸드셰이크 전에 TCP 연결을 바로 끊고 (클라이언트는 UNREACHABLE 에 가까운 전송 오류),
	// 0 이 아니면 그 HTTP 상태 코드(404 등)로 응답합니다.
	UnknownStatus int
}

// HostOptions 는 AddHost 옵션입니다 (Options 의 부분집합 + 속성 덮어쓰기).
type HostOptions struct {
	User, Pass string // 필수. 클라이언트 계정과 다르게 주면 그 호스트는 로그인 401 이 된다
	// AttrOverrides 는 이 호스트의 현재 Bios Attributes 값을 덮어씁니다 (FAIL 시나리오 만들기).
	// 값이 nil 이면 속성을 지웁니다. 다른 호스트·공유 트리에는 영향이 없습니다.
	AttrOverrides map[string]interface{}
	Fail          map[string]Fault // Options.Fail 과 같다
	NoSessions    bool
}

// FarmTotals 는 Farm 전체 합계입니다.
type FarmTotals struct {
	Hosts    int
	Calls    int
	Writes   int
	LogHits  int
	Sessions SessionStats
}

// Farm 은 가상 BMC 여러 대를 한 리스너에 올립니다. 모든 메서드는 동시에 호출해도 안전합니다.
type Farm struct {
	opts FarmOptions
	done chan struct{}
	once sync.Once

	mu    sync.RWMutex
	hosts map[string]*Host // 정규화된 IP → 호스트
	hs    *http.Server
	port  int
	serve chan error

	open, maxOpen, accepted, dropped atomic.Int64
}

// NewFarm 은 빈 Farm 을 만듭니다 (네트워크는 아직 열지 않음).
func NewFarm(opts FarmOptions) *Farm {
	return &Farm{opts: opts, done: make(chan struct{}), hosts: map[string]*Host{}, serve: make(chan error, 1)}
}

// AddHost 는 ip 로 접속하면 tree 로 응답하는 가상 BMC 를 등록합니다. 같은 IP 를 두 번 등록할 수 없습니다.
// Listen 전후 어느 때든 부를 수 있습니다. tree 는 호스트 사이에 공유되며 수정하지 않습니다.
func (f *Farm) AddHost(ip string, tree *Tree, o HostOptions) error {
	key, err := canonIP(ip)
	if err != nil {
		return err
	}
	if tree == nil {
		return errors.New("mockbmc: tree 가 필요합니다")
	}
	if o.User == "" || o.Pass == "" {
		return errors.New("mockbmc: HostOptions.User/Pass 가 필요합니다")
	}
	h := newHost(tree, Options{User: o.User, Pass: o.Pass, NoSessions: o.NoSessions, Fail: o.Fail}, f.done)
	for name, v := range o.AttrOverrides {
		if err := h.SetBiosAttr(name, v); err != nil {
			return fmt.Errorf("mockbmc: %s: %w", ip, err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.hosts[key]; dup {
		return fmt.Errorf("mockbmc: 이미 등록된 IP 입니다: %s", ip)
	}
	f.hosts[key] = h
	return nil
}

func canonIP(ip string) (string, error) {
	p := net.ParseIP(strings.TrimSpace(ip))
	if p == nil {
		return "", fmt.Errorf("mockbmc: IP 형식이 아닙니다: %q", ip)
	}
	return p.String(), nil
}

// Host 는 ip 에 등록된 호스트입니다 (없으면 nil). 호스트별 Calls/Count/Writes/LogHits/Sessions 등을 조회합니다.
func (f *Farm) Host(ip string) *Host {
	key, err := canonIP(ip)
	if err != nil {
		return nil
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.hosts[key]
}

// Totals 는 등록된 모든 호스트의 합계입니다.
func (f *Farm) Totals() FarmTotals {
	f.mu.RLock()
	hs := make([]*Host, 0, len(f.hosts))
	for _, h := range f.hosts {
		hs = append(hs, h)
	}
	f.mu.RUnlock()
	t := FarmTotals{Hosts: len(hs)}
	for _, h := range hs {
		t.Calls += h.CallCount()
		t.Writes += h.Writes()
		t.LogHits += h.LogHits()
		ss := h.Sessions()
		t.Sessions.Created += ss.Created
		t.Sessions.Deleted += ss.Deleted
		t.Sessions.LoginFailed += ss.LoginFailed
	}
	return t
}

// Listen 은 addr(예: "0.0.0.0:0")에 자체서명 인증서로 TLS 서비스를 시작하고 실제 열린 주소를 돌려줍니다.
// 등록되지 않은 로컬 IP 로 온 접속은 FarmOptions.UnknownStatus 규칙을 따릅니다. Close 로 멈춥니다.
func (f *Farm) Listen(addr string) (string, error) {
	cert, err := selfSignedCert()
	if err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	hs := &http.Server{
		Handler:           f,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}},
		TLSNextProto:      map[string]func(*http.Server, *tls.Conn, http.Handler){}, // HTTP/1.1 만 (Hijack)
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0), // 클라이언트가 먼저 끊을 때의 핸드셰이크 오류 로그 억제
	}
	f.mu.Lock()
	f.hs = hs
	if ta, ok := ln.Addr().(*net.TCPAddr); ok {
		f.port = ta.Port
	}
	f.mu.Unlock()
	go func() { f.serve <- hs.ServeTLS(&farmListener{Listener: ln, f: f}, "", "") }()
	return ln.Addr().String(), nil
}

// Port 는 Listen 으로 열린 포트입니다 (열기 전이면 0).
func (f *Farm) Port() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.port
}

// Close 는 서버를 멈춥니다 (여러 번 불러도 됨).
func (f *Farm) Close() {
	f.once.Do(func() { close(f.done) })
	f.mu.RLock()
	hs := f.hs
	f.mu.RUnlock()
	if hs != nil {
		hs.Close()
	}
}

// OpenConns 는 지금 열려 있는 TCP 연결 수, MaxConns 는 지금까지의 최대 동시 연결 수입니다.
func (f *Farm) OpenConns() int { return int(f.open.Load()) }
func (f *Farm) MaxConns() int  { return int(f.maxOpen.Load()) }

// Accepted 는 받아들인 연결 총수, Dropped 는 등록되지 않은 IP 라 핸드셰이크 전에 끊은 연결 수입니다.
func (f *Farm) Accepted() int { return int(f.accepted.Load()) }
func (f *Farm) Dropped() int  { return int(f.dropped.Load()) }

// hostByAddr 는 연결의 로컬 주소로 호스트를 찾습니다.
func (f *Farm) hostByAddr(a net.Addr) *Host {
	var ip net.IP
	switch t := a.(type) {
	case *net.TCPAddr:
		ip = t.IP
	case nil:
		return nil
	default:
		h, _, err := net.SplitHostPort(a.String())
		if err != nil {
			return nil
		}
		ip = net.ParseIP(h)
	}
	if ip == nil {
		return nil
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.hosts[ip.String()]
}

// ServeHTTP 는 접속한 로컬 IP 로 호스트를 골라 처리를 맡깁니다.
func (f *Farm) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	la, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	h := f.hostByAddr(la)
	if h == nil {
		st := f.opts.UnknownStatus
		if st == 0 {
			st = http.StatusNotFound // 보통은 리스너가 먼저 끊으므로 도달하지 않는다
		}
		writeErr(w, st, "ResourceMissingAtURI", "no such virtual BMC")
		return
	}
	h.ServeHTTP(w, r)
}

// farmListener 는 등록되지 않은 IP 의 연결을 끊고, 동시 연결 수를 셉니다.
type farmListener struct {
	net.Listener
	f *Farm
}

func (l *farmListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.f.opts.UnknownStatus == 0 && l.f.hostByAddr(c.LocalAddr()) == nil {
			l.f.dropped.Add(1)
			c.Close()
			continue
		}
		l.f.accepted.Add(1)
		n := l.f.open.Add(1)
		for {
			m := l.f.maxOpen.Load()
			if n <= m || l.f.maxOpen.CompareAndSwap(m, n) {
				break
			}
		}
		return &countConn{Conn: c, f: l.f}, nil
	}
}

type countConn struct {
	net.Conn
	f    *Farm
	once sync.Once
}

func (c *countConn) Close() error {
	c.once.Do(func() { c.f.open.Add(-1) })
	return c.Conn.Close()
}
