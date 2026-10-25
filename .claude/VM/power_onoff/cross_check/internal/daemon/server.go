package daemon

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"cross_check/internal/store"
	"cross_check/internal/verify"
)

// Config 는 데몬 설정. 0/nil 인 값은 기본값으로 채운다.
type Config struct {
	SocketPath        string                            // 기본 DefaultSocketPath
	StorePath         string                            // 기본 DefaultStorePath
	AuthPath          string                            // 비밀번호 파일, 기본 DefaultAuthPath
	AuthMode          string                            // AuthRequired(기본) | AuthOff
	AuthSkipPermCheck bool                              // 비밀번호 파일 권한(mode&077) 검사 생략(Windows·테스트용)
	TargetDir         string                            // 사용자별 on_off_<user>.txt 위치. AuthRequired 에서는 필수, AuthOff 에서 비면 대상 검사 생략
	AllowedUsers      []string                          // users= 목록. AuthRequired 에서는 필수(register·run·승인·취소·비밀번호 변경은 이 이름만). AuthOff 는 무시
	ApprovalTTL       time.Duration                     // 승인 후 start 허용 시간(기본 2분 = 120초, cross_check.conf 의 approval_ttl_sec). 실행 중 SIGHUP 으로 바꿀 수 있음(ReloadApprovalTTL)
	WorkTimeout       time.Duration                     // 본 작업(작업중) 제한 시간(기본 24시간, cross_check.conf 의 work_timeout_sec). SIGHUP 으로 바꿀 수 있음
	SweepInterval     time.Duration                     // 승인 만료·작업 제한 검사 주기(기본 1초)
	AuthMaxFailures   int                               // 이 횟수만큼 연속 실패하면 그 이름의 시도를 지연(기본 5)
	AuthDelay         time.Duration                     // 지연 시간(기본 1초)
	GlobalFailLimit   int                               // GlobalFailWindow 안의 전체 실패가 이보다 많으면 실패 응답마다 지연(기본 100)
	GlobalFailWindow  time.Duration                     // 전체 실패를 세는 구간(기본 1분)
	Sleep             func(time.Duration)               // 인증 지연에 쓰는 sleep(테스트 주입용). nil 이면 time.Sleep
	MaxConns          int                               // 동시 연결 수 한도(기본 256)
	ReadTimeout       time.Duration                     // run 이 아닌 연결의 요청 간 읽기 제한(기본 60초)
	MaxWaitingPerUser int                               // 작업자별 대기 작업 한도(기본 50)
	MaxJobs           int                               // 저장 작업 전체 한도(기본 20000)
	PruneInterval     time.Duration                     // 오래된 중단 작업 정리 주기(기본 1시간)
	PruneAge          time.Duration                     // 이보다 오래 전에 중단된 작업을 지움(기본 30일)
	PeerUID           func(net.Conn) (uid int, ok bool) // 감사 기록용 상대 uid. nil 이면 OS 기본(Linux SO_PEERCRED)
	Now               func() time.Time                  // 시계(테스트 주입용). nil 이면 time.Now
	Logf              func(string, ...any)              // 로그. nil 이면 log.Printf
}

// withDefaults 는 비어 있는 설정을 기본값으로 채운다.
func (c Config) withDefaults() (Config, error) {
	if c.SocketPath == "" {
		c.SocketPath = DefaultSocketPath
	}
	if c.StorePath == "" {
		c.StorePath = DefaultStorePath
	}
	if c.AuthPath == "" {
		c.AuthPath = DefaultAuthPath
	}
	switch c.AuthMode {
	case "":
		c.AuthMode = AuthRequired
	case AuthRequired, AuthOff:
	default:
		return c, fmt.Errorf("AuthMode 는 %q 또는 %q 여야 함: %q", AuthRequired, AuthOff, c.AuthMode)
	}
	if c.AuthMode == AuthRequired && c.TargetDir == "" {
		return c, errors.New("TargetDir(target_dir) 가 없음 — AuthMode=required 에서는 사용자별 대상 파일 위치가 필수")
	}
	if c.AuthMode == AuthRequired && len(c.AllowedUsers) == 0 {
		return c, errors.New("AllowedUsers(cross_check.conf 의 users=) 가 없음 — AuthMode=required 에서는 작업자·확인자 이름 목록이 필수(예: users=hong,kim)")
	}
	seen := map[string]bool{}
	for _, u := range c.AllowedUsers {
		if err := checkAllowedName(u); err != nil {
			return c, err
		}
		if seen[u] {
			return c, fmt.Errorf("users 에 %s 가 두 번 나옴", u)
		}
		seen[u] = true
	}
	if c.ApprovalTTL <= 0 {
		c.ApprovalTTL = DefaultApprovalTTL
	}
	if c.WorkTimeout <= 0 {
		c.WorkTimeout = DefaultWorkTimeout
	}
	if c.SweepInterval <= 0 {
		c.SweepInterval = DefaultSweepInterval
	}
	if c.AuthMaxFailures <= 0 {
		c.AuthMaxFailures = DefaultAuthMaxFailures
	}
	if c.AuthDelay <= 0 {
		c.AuthDelay = DefaultAuthDelay
	}
	if c.GlobalFailLimit <= 0 {
		c.GlobalFailLimit = DefaultGlobalFailLimit
	}
	if c.GlobalFailWindow <= 0 {
		c.GlobalFailWindow = DefaultGlobalFailWindow
	}
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}
	if c.MaxConns <= 0 {
		c.MaxConns = DefaultMaxConns
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = DefaultReadTimeout
	}
	if c.MaxWaitingPerUser <= 0 {
		c.MaxWaitingPerUser = DefaultMaxWaitingPerUser
	}
	if c.MaxJobs <= 0 {
		c.MaxJobs = DefaultMaxJobs
	}
	if c.PruneInterval <= 0 {
		c.PruneInterval = DefaultPruneInterval
	}
	if c.PruneAge <= 0 {
		c.PruneAge = DefaultPruneAge
	}
	if c.PeerUID == nil {
		c.PeerUID = osPeerUID
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logf == nil {
		c.Logf = log.Printf
	}
	return c, nil
}

// Server 는 교차검증 데몬. NewServer 로 만들고 Serve 로 돌린다.
type Server struct {
	cfg     Config
	allowed map[string]bool // cfg.AllowedUsers(바뀌지 않음)
	st      *store.Store
	auth    *AuthStore
	thr     *throttle
	ln      net.Listener

	// mu 는 작업 상태 전이(조회 → 검사 → 갱신 → 알림)를 한 줄로 세운다.
	mu      sync.Mutex
	ttl     time.Duration                // 지금 적용 중인 승인 유효시간(cfg.ApprovalTTL 로 시작, SIGHUP 으로 바뀔 수 있어 mu 로 보호)
	work    time.Duration                // 지금 적용 중인 본 작업 제한 시간(cfg.WorkTimeout 로 시작, SIGHUP 으로 바뀔 수 있음)
	waiters map[string]*waiter           // 작업 ID → 이벤트를 받을 작업자(run) 연결
	pending map[string]time.Time         // 승인됐지만 아직 start 안 한 작업 ID → 승인 시각(스위퍼 대상)
	working map[string]time.Time         // 작업중(start 받음, finish 전)인 작업 ID → 시작 시각(작업 제한 시간 대상)
	tokens  map[string][sha256.Size]byte // 작업 ID → sha256(token). run 부터 finish 까지 유지. 메모리에만 두고 파일·로그에 남기지 않음

	connMu  sync.Mutex
	conns   map[net.Conn]bool // 열린 연결(true = 작업자 run 연결)
	closing bool
	wg      sync.WaitGroup // 연결 처리 고루틴

	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// NewServer 는 소켓을 열고(살아 있는 데몬이 있으면 실패) 저장소·비밀번호 파일을 읽은 뒤
// 남아 있던 대기/승인 작업을 "데몬 재시작" 으로 중단 처리한다. Serve 를 부르기 전에도 소켓은 이미 열려 있다.
// 여러 데몬이 같은 저장 파일을 쓰지 않도록, 호출자는 먼저 AcquireLock 을 잡는 것이 좋다.
func NewServer(cfg Config) (*Server, error) {
	cfg, err := cfg.withDefaults()
	if err != nil {
		return nil, err
	}
	// 소켓을 먼저 연다: 다른 데몬이 살아 있으면 그 데몬의 작업을 AbortOpen 으로 망가뜨리면 안 된다.
	ln, err := listenUnix(cfg.SocketPath)
	if err != nil {
		return nil, err
	}
	// 상태 디렉터리는 없으면 0700 으로 만든다(store.Open 은 0755 로 만들기 때문에 먼저 만든다).
	if err := os.MkdirAll(filepath.Dir(cfg.StorePath), 0o700); err != nil {
		ln.Close()
		return nil, fmt.Errorf("상태 디렉터리 생성 실패: %w", err)
	}
	st, err := store.Open(cfg.StorePath)
	if err != nil {
		ln.Close()
		return nil, err
	}
	auth := &AuthStore{SkipPermCheck: cfg.AuthSkipPermCheck}
	if err := auth.Load(cfg.AuthPath); err != nil && cfg.AuthMode != AuthOff {
		// 파일이 없거나 잘못되면 모두 거부(fail closed)한 채 기동하고, 파일이 고쳐지면 자동으로 다시 읽는다.
		cfg.Logf("[경고] %v — 고칠 때까지 승인·취소·관리자 요청은 모두 거부됨", err)
	}
	allowed := map[string]bool{}
	for _, u := range cfg.AllowedUsers {
		allowed[u] = true
	}
	s := &Server{
		cfg:     cfg,
		allowed: allowed,
		st:      st,
		auth:    auth,
		thr:     newThrottle(cfg.AuthMaxFailures, cfg.AuthDelay, cfg.GlobalFailLimit, cfg.GlobalFailWindow, cfg.Sleep),
		ln:      ln,
		ttl:     cfg.ApprovalTTL,
		work:    cfg.WorkTimeout,
		waiters: map[string]*waiter{},
		pending: map[string]time.Time{},
		working: map[string]time.Time{},
		tokens:  map[string][sha256.Size]byte{},
		conns:   map[net.Conn]bool{},
		done:    make(chan struct{}),
	}
	if n := st.AbortOpen(ReasonRestart); n > 0 {
		s.logf("재시작 전 남아 있던 대기/승인(시작 전)/작업중 작업 %d건을 정리(%s: 대기·시작 전은 중단, 작업중은 승인/작업중단)", n, ReasonRestart)
	}
	if cfg.AuthMode == AuthOff {
		s.logf("[경고] AuthMode=off: 비밀번호·token·users 목록 검사를 하지 않음(시험용)")
	} else if ok, err := auth.Registered(AdminUser); err == nil && !ok {
		s.logf("[경고] auth.conf 에 admin 항목이 없음 — 관리자 기능은 모두 거부됨(install.sh 실행 또는 admin=<패스워드> 줄 추가)")
	}
	s.logf("데몬 시작(pid %d): 소켓 %s, 저장소 %s, 승인 유효 %s(%d초), 작업 제한 %s(%d초), 허용 사용자 %d명", os.Getpid(), cfg.SocketPath, cfg.StorePath,
		FormatTTLDuration(cfg.ApprovalTTL), int(cfg.ApprovalTTL/time.Second), FormatTTLDuration(cfg.WorkTimeout), int(cfg.WorkTimeout/time.Second),
		len(cfg.AllowedUsers))
	return s, nil
}

// TTL 은 지금 적용 중인 승인 유효시간.
func (s *Server) TTL() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ttl
}

// ttlSec 은 TTL 을 초로(1초 미만이면 0 → 응답에서 필드 생략).
func (s *Server) ttlSec() int { return int(s.TTL() / time.Second) }

// WorkTimeout 은 지금 적용 중인 본 작업 제한 시간.
func (s *Server) WorkTimeout() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.work
}

// approvedEventLocked 는 승인 이벤트(만료 시각 포함)를 만든다. mu 를 잡고 부른다.
func (s *Server) approvedEventLocked(reason, by string, at time.Time) Event {
	exp := at.Add(s.ttl)
	return Event{Event: EventStatus, Status: StatusApproved, Reason: reason, By: by,
		TTLSec: int(s.ttl / time.Second), ExpiresAt: &exp}
}

// ReloadApprovalTTL 은 confPath 를 다시 읽어 approval_ttl_sec 와 work_timeout_sec 만 바꾼다(SIGHUP 처리용).
// 설정 파일에 오류가 있거나 값이 범위 밖이면 기존 값을 그대로 두고 오류를 돌려준다(로그에도 남김).
// users·target_dir·auth 는 바뀌어도 반영하지 않고(재시작 필요) 달라졌다는 경고만 남긴다.
// 이미 승인되어 start 를 기다리는 작업에도 새 값이 적용된다(만료 시각 = 승인 시각 + 새 값). 작업 제한 시간도 같다.
// 돌려주는 old/now 는 승인 유효시간이다.
func (s *Server) ReloadApprovalTTL(confPath string) (old, now time.Duration, err error) {
	c, err := LoadConf(confPath)
	s.mu.Lock()
	old = s.ttl
	oldWork := s.work
	if err != nil {
		s.mu.Unlock()
		s.logf("[경고] 설정 다시 읽기(SIGHUP) 실패 — 승인 유효시간 %s 유지, 작업 제한 %s 유지: %v", FormatTTLDuration(old), FormatTTLDuration(oldWork), err)
		return old, old, err
	}
	s.ttl = c.ApprovalTTL
	s.work = c.WorkTimeout
	s.mu.Unlock()
	if oldWork != c.WorkTimeout {
		s.logf("설정 다시 읽기(SIGHUP): 작업 제한 시간 %s(%d초) → %s(%d초)",
			FormatTTLDuration(oldWork), int(oldWork/time.Second), FormatTTLDuration(c.WorkTimeout), int(c.WorkTimeout/time.Second))
	}
	if old == c.ApprovalTTL {
		s.logf("설정 다시 읽기(SIGHUP): 승인 유효시간 변경 없음(%s)", FormatTTLDuration(old))
	} else {
		s.logf("설정 다시 읽기(SIGHUP): 승인 유효시간 %s(%d초) → %s(%d초)",
			FormatTTLDuration(old), int(old/time.Second), FormatTTLDuration(c.ApprovalTTL), int(c.ApprovalTTL/time.Second))
	}
	if c.AuthMode != s.cfg.AuthMode || c.TargetDir != s.cfg.TargetDir || !sameStrings(c.Users, s.cfg.AllowedUsers) {
		s.logf("[알림] users·target_dir·auth 의 변경은 SIGHUP 으로 반영되지 않습니다 — 반영하려면 데몬을 다시 띄우세요(pkill -f 'cross_check --daemon')")
	}
	return old, c.ApprovalTTL, nil
}

// sameStrings 는 두 목록이 순서까지 같은지.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listenUnix 는 소켓 디렉터리(없으면 0755)를 만들고, 남은 소켓 파일은 연결이 안 될 때만 지운 뒤 듣고 0666 으로 연다.
func listenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("소켓 디렉터리 생성 실패: %w", err)
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.IsDir() {
			return nil, fmt.Errorf("소켓 경로가 디렉터리임: %s", path)
		}
		if fi.Mode().IsRegular() && fi.Size() > 0 {
			// 소켓 파일은 크기가 0 이다. 내용 있는 일반 파일을 실수로 지우지 않는다.
			return nil, fmt.Errorf("소켓 경로에 일반 파일이 있음(지우지 않음): %s", path)
		}
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			c.Close()
			return nil, fmt.Errorf("다른 데몬이 이미 소켓을 사용 중: %s", path)
		}
		// 연결이 안 되면 죽은 데몬이 남긴 소켓 파일
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("남은 소켓 파일 삭제 실패: %w", err)
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("소켓 열기 실패(%s): %w", path, err)
	}
	// 소켓은 누구나 접속 가능(권한 판단은 데몬이 비밀번호로 함)
	if err := os.Chmod(path, 0o666); err != nil {
		ln.Close()
		return nil, fmt.Errorf("소켓 권한 설정 실패: %w", err)
	}
	return ln, nil
}

func (s *Server) logf(format string, args ...any) { s.cfg.Logf(format, args...) }

// Addr 는 듣고 있는 소켓 경로.
func (s *Server) Addr() string { return s.cfg.SocketPath }

// Serve 는 연결을 받아 처리한다. ctx 가 끝나거나 Close 가 불리면 nil 을 돌려준다. 한 번만 부른다.
func (s *Server) Serve(ctx context.Context) error {
	go s.sweepLoop()
	go s.pruneLoop()
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-s.done:
		}
	}()
	backoff := 10 * time.Millisecond
	for {
		c, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
			}
			// EMFILE 등 일시적 오류: 잠시 쉬고 계속(데몬이 죽지 않게)
			s.logf("accept 오류: %v", err)
			time.Sleep(backoff)
			if backoff < time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 10 * time.Millisecond
		switch s.track(c) {
		case trackOK:
			go s.handleConn(c)
		case trackFull:
			// 한도를 넘은 연결은 오류 한 줄만 주고 바로 닫는다(빈 소켓 버퍼라 쓰기는 막히지 않지만 짧은 제한을 둔다).
			if b, err := encodeLine(fail(fmt.Sprintf("%s(최대 %d개)", msgTooManyConns, s.cfg.MaxConns))); err == nil {
				c.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
				c.Write(b)
			}
			c.Close()
		default:
			c.Close()
		}
	}
}

// Close 는 소켓을 닫고, 열린 작업(대기·승인 후 미시작)을 "데몬 종료" 로 중단 처리해 작업자에게 알린 뒤
// 모든 연결을 닫는다. 여러 번 불러도 안전하다.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.closeErr = s.ln.Close()

		s.mu.Lock()
		ids := map[string]bool{}
		for id := range s.waiters {
			ids[id] = true
		}
		for id := range s.pending {
			ids[id] = true
		}
		for id := range ids {
			if j, ok := s.st.Get(id); ok && (j.Status == StatusWaiting || j.Status == StatusApproved && j.StartedAt == nil) {
				s.abortLocked(id, ReasonShutdown, BySystem, nil)
			}
		}
		s.mu.Unlock()

		// 작업자 연결은 위에서 넣은 마지막 이벤트를 보내고 스스로 닫는다. 나머지는 바로 닫는다.
		s.connMu.Lock()
		s.closing = true
		for c, worker := range s.conns {
			if !worker {
				c.Close()
			}
		}
		s.connMu.Unlock()

		done := make(chan struct{})
		go func() { s.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			s.connMu.Lock()
			for c := range s.conns {
				c.Close()
			}
			s.connMu.Unlock()
			<-done
		}
		s.logf("데몬 종료")
	})
	return s.closeErr
}

// track 결과.
const (
	trackOK      = iota // 등록함
	trackClosing        // 종료 중
	trackFull           // 동시 연결 한도 초과
)

// track 은 연결을 등록한다. 종료 중이거나 한도를 넘으면 등록하지 않는다
// (wg.Add 와 Close 의 wg.Wait 가 겹치지 않게 connMu 안에서 판단).
func (s *Server) track(c net.Conn) int {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.closing {
		return trackClosing
	}
	if len(s.conns) >= s.cfg.MaxConns {
		return trackFull
	}
	s.conns[c] = false
	s.wg.Add(1)
	return trackOK
}

func (s *Server) markWorker(c net.Conn) {
	s.connMu.Lock()
	if _, ok := s.conns[c]; ok {
		s.conns[c] = true
	}
	s.connMu.Unlock()
}

func (s *Server) untrack(c net.Conn) {
	c.Close()
	s.connMu.Lock()
	delete(s.conns, c)
	s.connMu.Unlock()
	s.wg.Done()
}

// writeLine 은 v 를 한 줄로 쓴다. 상대가 읽지 않아 영원히 막히지 않게 쓰기 제한 시간을 둔다.
func (s *Server) writeLine(c net.Conn, v any) error {
	b, err := encodeLine(v)
	if err != nil {
		return err
	}
	c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, err = c.Write(b)
	return err
}

func fail(msg string) Response { return Response{OK: false, Error: msg} }

// handleConn 은 연결 하나를 처리한다. run 이 오면 그 연결은 작업 전용이 된다.
func (s *Server) handleConn(c net.Conn) {
	defer s.untrack(c)
	uid, uidOK := s.cfg.PeerUID(c) // 감사 기록용(권한 판단에 쓰지 않음)
	if !uidOK {
		uid = -1
	}
	r := bufio.NewReaderSize(c, 64<<10)
	for {
		// 요청마다 읽기 제한을 새로 건다: 아무것도 안 보내거나 줄을 끝내지 않는 연결이 자리를 차지하지 않게.
		// (run 이 되면 handleRun 이 제한을 푼다 — 작업자 연결은 승인까지 몇 시간이고 기다릴 수 있다.)
		c.SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
		line, err := readLine(r, MaxLineSize)
		if err != nil {
			if errors.Is(err, errLineTooLong) {
				s.writeLine(c, fail(msgTooLarge))
				// 남은 입력을 조금 비워 상대가 오류 응답을 읽기 전에 연결이 리셋되지 않게 한다.
				c.SetReadDeadline(time.Now().Add(time.Second))
				io.CopyN(io.Discard, r, 8<<20)
			}
			return
		}
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			// json 오류 문구에도 입력 내용이 섞일 수 있어 고정 문구만 준다.
			if s.writeLine(c, fail("요청 JSON 형식 오류")) != nil {
				return
			}
			continue
		}
		if req.Op == OpRun {
			c.SetReadDeadline(time.Time{})
			s.handleRun(c, r, req, uid)
			return
		}
		if s.writeLine(c, s.dispatch(req, uid)) != nil {
			return
		}
	}
}

// dispatch 는 run 이외의 op 를 처리한다.
func (s *Server) dispatch(req Request, uid int) Response {
	switch req.Op {
	case OpPing:
		return Response{OK: true, TTLSec: s.ttlSec(), WorkSec: int(s.WorkTimeout() / time.Second), PID: os.Getpid()}
	case OpList:
		if !ValidName(req.User) {
			return fail("사용자 이름이 올바르지 않음(영소문자·숫자·_ 만): " + clip(req.User))
		}
		return s.opList(req.User)
	case OpGet:
		if req.Password != "" {
			if m := s.authUser(req.User, req.Password); m != "" {
				return fail(m)
			}
		}
		return s.opGet(req)
	case OpApprove:
		if m := s.authUser(req.User, req.Password); m != "" {
			return fail(m)
		}
		return s.opApprove(req, uid)
	case OpCancel:
		if m := s.authUser(req.User, req.Password); m != "" {
			return fail(m)
		}
		return s.opCancel(req, uid)
	case OpStart:
		return s.opStart(req)
	case OpFinish:
		return s.opFinish(req)
	case OpStatusUser:
		if !ValidName(req.User) {
			return fail("사용자 이름이 올바르지 않음(영소문자·숫자·_ 만): " + clip(req.User))
		}
		ok, err := s.auth.Registered(req.User)
		if err != nil {
			return fail("인증 불가: " + err.Error())
		}
		return Response{OK: true, Registered: &ok}
	case OpRegister:
		return s.opRegister(req)
	case OpChangePassword:
		if m := s.authUser(req.User, req.Password); m != "" {
			return fail(m)
		}
		if err := s.auth.SetPassword(req.User, req.NewPassword); err != nil {
			return fail("비밀번호 변경 실패: " + err.Error())
		}
		s.logf("비밀번호 변경: %s", req.User)
		return Response{OK: true}
	case OpAdminList, OpAdminSet, OpAdminDelete, OpAdminClear, OpAdminSetPassword:
		if m := s.authAdmin(req.Password); m != "" {
			return fail(m)
		}
		switch req.Op {
		case OpAdminList:
			return Response{OK: true, AllJobs: s.st.List()}
		case OpAdminSet:
			return s.opAdminSet(req)
		case OpAdminDelete:
			return s.opAdminDelete(req)
		case OpAdminClear:
			return s.opAdminClear()
		default:
			return s.opAdminSetPassword(req)
		}
	case "":
		return fail("op 가 비어 있음")
	default:
		return fail("알 수 없는 op: " + clip(req.Op))
	}
}

// isAllowed 는 user 가 users= 목록에 있는지(AuthOff 면 항상 true). admin 은 목록에 없다.
func (s *Server) isAllowed(user string) bool {
	return s.cfg.AuthMode == AuthOff || s.allowed[user]
}

// authUser 는 user 의 비밀번호를 확인한다. 통과면 "", 실패면 응답에 쓸 오류 문구.
// user 는 users= 목록의 이름이거나 admin 이어야 하고, 아니면 비밀번호와 상관없이 같은 "인증 실패" 다.
// 실패 문구는 사용자 항목이 있는지 드러내지 않고, 로그에도 비밀번호는 남기지 않는다.
// 연속 실패가 쌓인 이름은 지연(throttle) 뒤에 검사한다. 이 함수는 서버 mu 를 잡지 않은 채로 불려야 한다(sleep 함).
// 비밀번호 파일 자체를 쓸 수 없으면(없음·권한 느슨함·형식 오류) 그 이유를 알려 주고 거부한다.
// admin 항목이 없으면 관리자 op 는 MsgNoAdmin 으로 거부한다(admin 은 register 로 만들 수 없다).
func (s *Server) authUser(user, password string) string { return s.authCheck(user, password, false) }

// authAdmin 은 관리자 op 의 admin 비밀번호를 확인한다(authUser 와 같은 규칙, admin 항목이 없으면 MsgNoAdmin).
func (s *Server) authAdmin(password string) string { return s.authCheck(AdminUser, password, true) }

// authCheck 는 authUser/authAdmin 공통. asAdmin 이 아니면 admin 이라는 이름도 목록 밖 이름으로 본다.
func (s *Server) authCheck(user, password string, asAdmin bool) string {
	if s.cfg.AuthMode == AuthOff {
		return ""
	}
	if !ValidName(user) {
		return MsgAuthFailed
	}
	if asAdmin {
		reg, err := s.auth.Registered(AdminUser)
		if err != nil {
			s.logf("인증 불가: %v", err)
			return "인증 불가: " + err.Error()
		}
		if !reg {
			s.logf("관리자 요청 거부: auth.conf 에 admin 항목 없음")
			return MsgNoAdmin
		}
	} else if err := s.auth.Err(); err != nil {
		s.logf("인증 불가: %v", err)
		return "인증 불가: " + err.Error()
	}
	listed := asAdmin || s.allowed[user]
	release := s.thr.begin(user, listed)
	ok := listed && s.auth.Verify(user, password)
	release()
	if ok {
		s.thr.ok(user)
		return ""
	}
	over := s.thr.failed(user, s.cfg.Now(), listed)
	if listed {
		if n := s.thr.failures(user); n >= s.cfg.AuthMaxFailures {
			s.logf("인증 실패(%s) — 연속 %d번, 이후 시도는 %s 씩 지연", user, n, s.cfg.AuthDelay)
		} else {
			s.logf("인증 실패(%s)", user)
		}
	} else {
		s.logf("인증 실패(%s, users 목록에 없는 이름)", user)
	}
	if over {
		s.thr.globalDelay()
	}
	return MsgAuthFailed
}

// opRegister 는 users= 목록에 있고 auth.conf 에 아직 항목이 없는 사용자를 등록한다.
// admin 은 언제나 거부한다(관리자 항목은 install.sh 가 auth.conf 에 직접 쓴다).
func (s *Server) opRegister(req Request) Response {
	if !ValidName(req.User) {
		return fail("사용자 이름이 올바르지 않음(영소문자·숫자·_ 만): " + clip(req.User))
	}
	if req.User == AdminUser {
		s.logf("등록 거부: admin")
		return fail(AdminUser + " 은 등록할 수 없음 — 관리자 항목은 install.sh 가 auth.conf 에 만든다(없으면 root 가 'admin=<패스워드>' 줄을 직접 추가)")
	}
	if !s.isAllowed(req.User) {
		s.logf("등록 거부(%s): users 목록에 없음", req.User)
		return fail(req.User + " 은 cross_check.conf 의 users= 목록에 없어 등록할 수 없음 — 관리자에게 추가를 요청하세요")
	}
	if err := CheckPassword(req.Password); err != nil {
		return fail(err.Error())
	}
	if err := s.auth.Register(req.User, req.Password); err != nil {
		if errors.Is(err, ErrAlreadyRegistered) {
			return fail(ErrAlreadyRegistered.Error())
		}
		return fail("등록 실패: " + err.Error())
	}
	s.logf("사용자 등록: %s", req.User)
	return Response{OK: true}
}

// newToken 은 128비트 랜덤 1회용 token(hex 32자)을 만든다.
func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand 실패: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// capLocked 는 새 작업을 저장해도 되는지 한도를 검사한다. 넘으면 오류 문구, 아니면 "". mu 를 잡고 부른다.
// waiting 이면 작업자 user 의 대기 작업 수 한도도 본다.
func (s *Server) capLocked(user string, waiting bool) string {
	if n := s.st.Len(); n >= s.cfg.MaxJobs {
		s.logf("작업 등록 거부(%s): 저장 작업 %d건으로 한도 %d건", user, n, s.cfg.MaxJobs)
		return fmt.Sprintf("저장된 작업이 한도(%d건)에 닿아 새 작업을 받을 수 없음 — 관리자에게 정리(cross_check --admin)를 요청하세요", s.cfg.MaxJobs)
	}
	if waiting {
		n := s.st.Count(func(j *store.Job) bool { return j.User == user && j.Status == StatusWaiting })
		if n >= s.cfg.MaxWaitingPerUser {
			s.logf("작업 등록 거부(%s): 대기 작업 %d건으로 한도 %d건", user, n, s.cfg.MaxWaitingPerUser)
			return fmt.Sprintf("작업자 %s 의 대기 작업이 한도(%d건)에 닿음 — 기존 대기 작업이 끝난 뒤 다시 시도하세요", user, s.cfg.MaxWaitingPerUser)
		}
	}
	return ""
}

// handleRun 은 작업을 등록하고, 이 연결로 상태 변화 이벤트를 밀어준다.
// 대기 중 연결이 끊기면 즉시 중단 처리한다(Ctrl+C·세션 끊김·kill -9 모두 커널이 EOF 로 알려줌).
func (s *Server) handleRun(c net.Conn, r *bufio.Reader, req Request, uid int) {
	if err := validateRun(req); err != nil {
		s.writeLine(c, fail(err.Error()))
		return
	}
	// 작업자·확인자 모두 users= 목록에 있고 auth.conf 에 등록되어 있어야 한다(비밀번호는 묻지 않음: 스크립트에서 부르기 때문).
	// 목록 검사가 없으면 작업자가 아무 이름(가짜 확인자)이나 등록해 자기 작업을 스스로 승인할 수 있다.
	if s.cfg.AuthMode != AuthOff {
		if !s.allowed[req.User] {
			s.writeLine(c, fail("작업자 "+req.User+" 는 cross_check.conf 의 users= 목록에 없음 — 관리자에게 추가를 요청하세요"))
			return
		}
		if !s.allowed[req.Checker] {
			s.writeLine(c, fail("확인자 "+req.Checker+" 는 cross_check.conf 의 users= 목록에 없음 — 목록에 있는 다른 사람을 확인자로 지정하세요"))
			return
		}
		if ok, err := s.auth.Registered(req.User); err != nil {
			s.writeLine(c, fail("인증 불가: "+err.Error()))
			return
		} else if !ok {
			s.writeLine(c, fail("작업자 미등록: cross_check --user "+req.User+" 로 먼저 등록"))
			return
		}
		if ok, _ := s.auth.Registered(req.Checker); !ok && req.Checker != req.User {
			s.writeLine(c, fail("확인자 미등록: "+req.Checker+" 가 cross_check --user "+req.Checker+" 로 먼저 등록해야 함"))
			return
		}
	}
	// 작업자는 자기에게 등록된 대상(<target_dir>/on_off_<user>.txt)만 올릴 수 있다. 위반이면 작업을 만들지 않는다.
	if s.cfg.TargetDir != "" {
		if err := checkOwnTargets(s.cfg.TargetDir, req.User, req.Targets); err != nil {
			s.writeLine(c, fail(err.Error()))
			return
		}
	}
	now := s.cfg.Now()
	base := store.Job{
		User: req.User, Checker: req.Checker, Targets: req.Targets, Caller: SanitizeCaller(req.Caller),
		CreatedAt: now, UpdatedAt: now, WorkerUID: uid, CheckerUID: -1,
	}

	// 동일인: 기록은 남기고(중단) 즉시 거부
	if req.User == req.Checker {
		base.Status, base.Reason = StatusAborted, ReasonSameUser
		s.mu.Lock()
		msg := s.capLocked(req.User, false)
		var j store.Job
		var err error
		if msg == "" {
			j, err = s.st.Add(base)
		}
		s.mu.Unlock()
		if msg != "" {
			s.writeLine(c, fail(msg))
			return
		}
		if err != nil {
			s.writeLine(c, fail("저장 실패: "+err.Error()))
			return
		}
		s.logf("작업 %s 중단: %s (%s)", j.ID, ReasonSameUser, req.User)
		s.writeLine(c, Response{OK: false, Error: ReasonSameUser, ID: j.ID})
		return
	}
	token := newToken()
	w := newWaiter()
	base.Status = StatusWaiting
	s.mu.Lock()
	// 한도 검사와 추가를 같은 mu 안에서 해야 동시에 들어온 run 들이 한도를 넘지 못한다.
	if msg := s.capLocked(req.User, true); msg != "" {
		s.mu.Unlock()
		s.writeLine(c, fail(msg))
		return
	}
	j, err := s.st.Add(base)
	if err == nil {
		s.waiters[j.ID] = w
		s.tokens[j.ID] = sha256.Sum256([]byte(token))
		// 등록 이벤트는 mu 안에서 넣어야 곧바로 들어온 승인 이벤트보다 앞선다.
		w.push(Event{Event: EventStatus, Status: StatusWaiting, Reason: reasonWaiting(req.Checker), By: BySystem}, false)
	}
	s.mu.Unlock()
	if err != nil {
		s.writeLine(c, fail("저장 실패: "+err.Error()))
		return
	}
	s.markWorker(c)
	s.logf("작업 %s 등록: 작업자 %s, 확인자 %s, 대상 %d개, 호출 %q", j.ID, j.User, j.Checker, len(j.Targets), j.Caller)

	// 작업자가 보내는 것은 더 없다. 읽기는 연결 끊김(EOF) 감지용.
	readDone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, r)
		close(readDone)
	}()
	if err := s.writeLine(c, Response{OK: true, ID: j.ID, Token: token, TTLSec: s.ttlSec()}); err != nil {
		s.workerGone(j.ID, w)
		return
	}
	for {
		select {
		case <-w.wake:
			evs, final := w.take()
			for _, ev := range evs {
				if err := s.writeLine(c, ev); err != nil {
					s.workerGone(j.ID, w)
					return
				}
			}
			if final {
				return
			}
		case <-readDone:
			s.workerGone(j.ID, w)
			return
		}
	}
}

// workerGone 은 작업자 연결이 끊겼을 때 부른다. 아직 대기면 즉시 중단, 승인 후라면 연결만 정리
// (승인 후 미시작 건은 스위퍼가 승인 유효시간이 지나면 중단한다).
func (s *Server) workerGone(id string, w *waiter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.waiters[id] != w {
		return // 이미 마지막 이벤트를 넣고 정리됨
	}
	delete(s.waiters, id)
	if j, ok := s.st.Get(id); ok && j.Status == StatusWaiting {
		s.abortLocked(id, ReasonWorkerGone, BySystem, nil)
	}
}

// waiter 는 작업자 연결로 보낼 이벤트 대기열. push 는 mu(서버) 안에서 불려도 막히지 않는다.
type waiter struct {
	mu    sync.Mutex
	queue []Event
	final bool          // 마지막 이벤트를 넣었음(보낸 뒤 연결 종료)
	wake  chan struct{} // 크기 1: 보낼 것이 생겼다는 신호
}

func newWaiter() *waiter { return &waiter{wake: make(chan struct{}, 1)} }

func (w *waiter) push(ev Event, final bool) {
	w.mu.Lock()
	w.queue = append(w.queue, ev)
	if final {
		w.final = true
	}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *waiter) take() ([]Event, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	q := w.queue
	w.queue = nil
	return q, w.final
}

// notifyLocked 는 작업자 연결이 있으면 이벤트를 넣는다. final 이면 그 연결을 정리 대상으로 뺀다. mu 를 잡고 부른다.
func (s *Server) notifyLocked(id string, ev Event, final bool) {
	w, ok := s.waiters[id]
	if !ok {
		return
	}
	w.push(ev, final)
	if final {
		delete(s.waiters, id)
	}
}

// abortLocked 는 작업을 중단(reason)으로 저장하고 작업자에게 알린다. extra 가 있으면 같은 갱신에서 함께 고친다.
// 저장에 실패하면 상태·대기열을 건드리지 않고 오류를 돌려준다. mu 를 잡고 부른다.
func (s *Server) abortLocked(id, reason, by string, extra func(*store.Job)) error {
	_, err := s.st.Update(id, func(j *store.Job) {
		j.Status = StatusAborted
		j.Reason = reason
		if extra != nil {
			extra(j)
		}
	})
	if err != nil {
		s.logf("작업 %s 중단 저장 실패(%s): %v", id, reason, err)
		return err
	}
	delete(s.pending, id)
	delete(s.working, id)
	// token 은 지우지 않는다: 중단된 작업은 어차피 시작할 수 없고, 작업자가 나중에 start 하면
	// "인증 실패" 대신 실제 중단 사유(예: 승인 후 {TTL} 내 미진행)를 받아야 한다(스위퍼 만료 뒤 start).
	s.notifyLocked(id, Event{Event: EventStatus, Status: StatusAborted, Reason: reason, By: by}, true)
	s.logf("작업 %s 중단: %s", id, reason)
	return nil
}

// opList 는 user 가 작업자 또는 확인자인 작업을 돌려준다(대상 목록은 user 가 작업자인 작업에만 — viewOf).
func (s *Server) opList(user string) Response {
	out := []JobView{}
	ttl := s.TTL()
	for _, j := range s.st.List() {
		if j.User == user || j.Checker == user {
			out = append(out, viewOf(j, ttl, user))
		}
	}
	return Response{OK: true, Jobs: out}
}

// opGet 은 작업 하나를 돌려준다. user 가 그 작업의 작업자 또는 확인자여야 한다.
func (s *Server) opGet(req Request) Response {
	j, ok := s.st.Get(req.ID)
	if !ok || !ValidName(req.User) || (j.User != req.User && j.Checker != req.User) {
		return fail(msgNotMember)
	}
	v := viewOf(j, s.TTL(), req.User)
	return Response{OK: true, Job: &v}
}

// opApprove 는 확인자 본인(비밀번호 확인은 dispatch 에서 끝남)이 붙여넣은 대상(req.Pasted)을 작업 대상과 대조해
// 일치하면 승인하고, 다르면(빠짐·남음·중복·빈 입력) 작업을 중단한다(계획서 4-1 "확인자가 붙여넣은 대상이 일치하지 않는 경우").
// 응답·사유·로그에는 개수만 넣는다(확인자는 대상 이름을 알 수 없어야 한다).
func (s *Server) opApprove(req Request, uid int) Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.st.Get(req.ID)
	if !ok || j.Checker != req.User {
		return fail(msgNoJob)
	}
	if j.Status != StatusWaiting {
		return fail(fmt.Sprintf("대기 상태가 아니라 승인할 수 없음(현재 %s)", StatusLabel(j.Status, j.Phase)))
	}
	if r := verify.Compare(j.Targets, verify.Parse(req.Pasted)); !r.OK() {
		reason := MismatchReason(len(r.Missing), len(r.Extra), len(r.Duplicate), r.Empty)
		if err := s.abortLocked(j.ID, reason, j.Checker, func(x *store.Job) { x.CheckerUID = uid }); err != nil {
			return fail("저장 실패: " + err.Error())
		}
		return fail(reason + " — 작업이 중단되었습니다(작업자 스크립트도 중단)")
	}
	now := s.cfg.Now()
	if _, err := s.st.Update(j.ID, func(x *store.Job) {
		x.Status = StatusApproved
		x.Phase = PhaseNone
		x.Reason = ""
		x.ApprovedAt = &now
		x.StartedAt = nil
		x.CheckerUID = uid
	}); err != nil {
		return fail("저장 실패: " + err.Error())
	}
	s.pending[j.ID] = now
	s.notifyLocked(j.ID, s.approvedEventLocked(reasonApproved(j.Checker), j.Checker, now), false)
	s.logf("작업 %s 승인: 확인자 %s", j.ID, j.Checker)
	return Response{OK: true}
}

// opCancel 은 확인자가 대기 작업을 취소한다.
func (s *Server) opCancel(req Request, uid int) Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.st.Get(req.ID)
	if !ok || j.Checker != req.User {
		return fail(msgNoJob)
	}
	if j.Status != StatusWaiting {
		return fail(fmt.Sprintf("대기 상태가 아니라 취소할 수 없음(현재 %s)", j.Status))
	}
	if err := s.abortLocked(j.ID, ReasonCancel(j.Checker), j.Checker, func(x *store.Job) { x.CheckerUID = uid }); err != nil {
		return fail("저장 실패: " + err.Error())
	}
	return Response{OK: true}
}

// checkTokenLocked 는 start/finish 의 token 을 확인한다. 통과면 "", 아니면 응답 문구. mu 를 잡고 부른다.
// token 이 이미 폐기된(끝난) 작업이면 상태를 그대로 알려 준다(상태는 list 로도 보이므로 숨길 것이 없음).
func (s *Server) checkTokenLocked(req Request) string {
	if s.cfg.AuthMode == AuthOff {
		return ""
	}
	h, ok := s.tokens[req.ID]
	if !ok {
		if j, found := s.st.Get(req.ID); found && req.Token != "" && j.Status == StatusApproved {
			if j.Phase == PhaseDone || j.Phase == PhaseAborted {
				return fmt.Sprintf("이미 끝난 작업(현재 %s)", StatusLabel(j.Status, j.Phase))
			}
			if j.StartedAt != nil {
				return "이미 시작 신호를 받은 작업"
			}
		}
		return MsgAuthFailed
	}
	sum := sha256.Sum256([]byte(req.Token))
	if req.Token == "" || subtle.ConstantTimeCompare(sum[:], h[:]) != 1 {
		return MsgAuthFailed
	}
	return ""
}

// opStart 는 본 작업 직전 신호. run 이 준 token 이 맞고, 승인 후 TTL 이내이며, 처음일 때만 통과한다(phase → 작업중).
// token 은 finish 까지 남긴다(같은 token 으로 두 번 시작은 "이미 시작"). 중단된 작업의 token 도 남겨 두어
// 작업자가 늦게 start 해도 "인증 실패" 대신 실제 중단 사유를 받게 한다.
func (s *Server) opStart(req Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.checkTokenLocked(req); m != "" {
		return fail(m)
	}
	j, ok := s.st.Get(req.ID)
	if !ok {
		return fail(MsgAuthFailed)
	}
	if j.Status != StatusApproved {
		msg := fmt.Sprintf("승인 상태가 아니라 시작할 수 없음(현재 %s", j.Status)
		if j.Reason != "" {
			msg += ": " + j.Reason
		}
		return fail(msg + ")")
	}
	if j.StartedAt != nil {
		return fail("이미 시작 신호를 받은 작업")
	}
	if j.Phase != PhaseNone {
		return fail(fmt.Sprintf("이미 끝난 작업(현재 %s)", StatusLabel(j.Status, j.Phase)))
	}
	now := s.cfg.Now()
	if j.ApprovedAt == nil || now.Sub(*j.ApprovedAt) > s.ttl {
		reason := ReasonExpired(s.ttl)
		s.abortLocked(j.ID, reason, BySystem, nil)
		return fail(reason)
	}
	if _, err := s.st.Update(j.ID, func(x *store.Job) { x.StartedAt = &now; x.Phase = PhaseWorking }); err != nil {
		return fail("저장 실패: " + err.Error())
	}
	delete(s.pending, j.ID)
	s.working[j.ID] = now
	s.notifyLocked(j.ID, Event{Event: EventStatus, Status: StatusApproved, Reason: ReasonStarted, By: j.User}, true)
	s.logf("작업 %s 본 작업 시작(작업중)", j.ID)
	return Response{OK: true}
}

// opFinish 는 본 작업 결과. token 이 맞아야 하고:
//   - result done: phase 작업중 → 작업완료 (작업자 스크립트가 취소 없이 끝까지 실행됨. VM 별 성공 여부는 보지 않는다)
//   - result aborted: 승인 시작 전(phase 없음) 또는 작업중 → 작업중단 (최종 확인에서 No, Ctrl+C 등)
//
// 끝나면 token 을 지우고 더 바꿀 수 없다.
func (s *Server) opFinish(req Request) Response {
	if req.Result != ResultDone && req.Result != ResultAborted {
		return fail(fmt.Sprintf("result 는 %s 또는 %s: %q", ResultDone, ResultAborted, clip(req.Result)))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.checkTokenLocked(req); m != "" {
		return fail(m)
	}
	j, ok := s.st.Get(req.ID)
	if !ok {
		return fail(MsgAuthFailed)
	}
	if j.Status != StatusApproved {
		msg := fmt.Sprintf("승인 상태가 아니라 결과를 기록할 수 없음(현재 %s", j.Status)
		if j.Reason != "" {
			msg += ": " + j.Reason
		}
		return fail(msg + ")")
	}
	var phase store.Phase
	var reason string
	switch {
	case j.Phase == PhaseDone || j.Phase == PhaseAborted:
		return fail(fmt.Sprintf("이미 끝난 작업(현재 %s)", StatusLabel(j.Status, j.Phase)))
	case req.Result == ResultDone && j.Phase != PhaseWorking:
		return fail("본 작업을 시작하지 않아(--start 전) 완료로 기록할 수 없음 — 멈췄다면 --result aborted")
	case req.Result == ResultDone:
		phase = PhaseDone
	case j.Phase == PhaseWorking:
		phase, reason = PhaseAborted, ReasonWorkerAborted
	default:
		phase, reason = PhaseAborted, ReasonWorkerAbortedBeforeStart
	}
	if err := s.finishPhaseLocked(j.ID, phase, reason); err != nil {
		return fail("저장 실패: " + err.Error())
	}
	return Response{OK: true}
}

// finishPhaseLocked 는 승인 작업의 단계를 끝낸다(작업완료/작업중단): phase·종료 시각·사유를 저장하고
// 만료·작업 제한 추적과 token 을 정리한다(이후 start/finish 불가). finish·작업 제한 시간·관리자가 쓴다. mu 를 잡고 부른다.
func (s *Server) finishPhaseLocked(id string, phase store.Phase, reason string) error {
	now := s.cfg.Now()
	if _, err := s.st.Update(id, func(x *store.Job) {
		x.Phase = phase
		x.FinishedAt = &now
		if reason != "" {
			x.Reason = reason
		}
	}); err != nil {
		s.logf("작업 %s → %s 저장 실패: %v", id, StatusLabel(StatusApproved, phase), err)
		return err
	}
	delete(s.pending, id)
	delete(s.working, id)
	delete(s.tokens, id)
	if reason != "" {
		s.logf("작업 %s → %s(%s)", id, StatusLabel(StatusApproved, phase), reason)
	} else {
		s.logf("작업 %s → %s", id, StatusLabel(StatusApproved, phase))
	}
	return nil
}

// sweepLoop 는 주기적으로 승인 후 유효시간(approval_ttl_sec)이 지나도록 start 가 없는 작업을 중단하고,
// 작업중으로 작업 제한 시간(work_timeout_sec)을 넘긴 작업을 승인/작업중단("작업자 응답 없음")으로 바꾼다.
// (본 작업을 시작한 작업은 승인 유효시간으로 만료되지 않는다.)
func (s *Server) sweepLoop() {
	t := time.NewTicker(s.cfg.SweepInterval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.sweep()
		}
	}
}

// sweep 은 만료 검사 한 번. 저장에 실패한 건은 pending 에 남아 다음 주기에 다시 시도된다.
func (s *Server) sweep() {
	now := s.cfg.Now()
	s.mu.Lock()
	for id, at := range s.pending {
		if now.Sub(at) > s.ttl {
			s.abortLocked(id, ReasonExpired(s.ttl), BySystem, nil)
		}
	}
	for id, at := range s.working {
		if now.Sub(at) > s.work {
			s.finishPhaseLocked(id, PhaseAborted, ReasonWorkTimeout)
		}
	}
	s.mu.Unlock()
	s.thr.prune(now)
}

// opAdminSet 은 관리자가 상태를 바꾼다. 허용: 대기→승인, 대기→중단, 승인→중단.
// 대기로 되돌리는 것은 막고(기다리는 작업자 연결이 없을 수 있음), 중단된 작업은 되살릴 수 없다
// (중단 뒤에 남은 token 으로 다시 시작되는 것을 막음). 이미 승인된 작업을 다시 승인하지도 않는다.
// 승인으로 바꾸면 ApprovedAt 을 지금으로 정해 승인 유효시간(approval_ttl_sec) 규칙을 적용한다.
func (s *Server) opAdminSet(req Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.st.Get(req.ID)
	if !ok {
		return fail("작업을 찾을 수 없음: " + clip(req.ID))
	}
	reason := ReasonAdminSet
	if req.Reason != "" {
		reason += ": " + req.Reason
	}
	switch req.Status {
	case StatusAborted:
		if j.Status == StatusAborted {
			return fail("이미 중단된 작업")
		}
		if err := s.abortLocked(j.ID, reason, ByAdmin, nil); err != nil {
			return fail("저장 실패: " + err.Error())
		}
	case StatusApproved:
		if req.Phase != PhaseNone {
			return s.adminSetPhaseLocked(j, req.Phase, reason)
		}
		switch j.Status {
		case StatusAborted:
			return fail(MsgNoResurrect)
		case StatusApproved:
			return fail("이미 승인된 작업(다시 승인할 수 없음)")
		}
		now := s.cfg.Now()
		if _, err := s.st.Update(j.ID, func(x *store.Job) {
			x.Status = StatusApproved
			x.Phase = PhaseNone
			x.Reason = reason
			x.ApprovedAt = &now
			x.StartedAt = nil
		}); err != nil {
			return fail("저장 실패: " + err.Error())
		}
		s.pending[j.ID] = now
		s.notifyLocked(j.ID, s.approvedEventLocked(reason, ByAdmin, now), false)
		s.logf("작업 %s 관리자 승인", j.ID)
	case StatusWaiting:
		return fail("대기로는 되돌릴 수 없음(작업자를 다시 실행하세요)")
	default:
		return fail("상태는 승인 또는 중단만 가능: " + clip(string(req.Status)))
	}
	return Response{OK: true}
}

// adminSetPhaseLocked 는 관리자가 승인 작업의 단계를 끝낸다: 작업중 → 작업완료/작업중단, 시작 전 → 작업중단.
// 작업중으로 바꾸는 것은 작업자의 --start 만 할 수 있고, 이미 끝난 단계는 바꾸지 않는다. mu 를 잡고 부른다.
func (s *Server) adminSetPhaseLocked(j store.Job, phase store.Phase, reason string) Response {
	if j.Status != StatusApproved {
		return fail(fmt.Sprintf("승인 상태가 아니라 작업 단계를 바꿀 수 없음(현재 %s)", j.Status))
	}
	switch phase {
	case PhaseDone:
		if j.Phase != PhaseWorking {
			return fail(fmt.Sprintf("작업중인 작업만 작업완료로 바꿀 수 있음(현재 %s)", StatusLabel(j.Status, j.Phase)))
		}
	case PhaseAborted:
		if j.Phase != PhaseNone && j.Phase != PhaseWorking {
			return fail(fmt.Sprintf("이미 끝난 작업(현재 %s)", StatusLabel(j.Status, j.Phase)))
		}
	case PhaseWorking:
		return fail("작업중은 작업자의 --start 로만 바꿀 수 있음")
	default:
		return fail("작업 단계는 작업완료 또는 작업중단만 가능: " + clip(string(phase)))
	}
	if err := s.finishPhaseLocked(j.ID, phase, reason); err != nil {
		return fail("저장 실패: " + err.Error())
	}
	s.notifyLocked(j.ID, Event{Event: EventStatus, Status: StatusApproved, Reason: reason, By: ByAdmin}, true)
	return Response{OK: true}
}

// opAdminSetPassword 는 관리자가 users= 목록에 있는 사용자의 비밀번호를 재설정한다(항목이 없으면 새로 만든다).
// 목록 밖 이름과 admin 은 거부한다(admin 비밀번호는 root 가 auth.conf 를 직접 고친다).
func (s *Server) opAdminSetPassword(req Request) Response {
	if !ValidName(req.User) {
		return fail("사용자 이름이 올바르지 않음(영소문자·숫자·_ 만): " + clip(req.User))
	}
	if req.User == AdminUser {
		return fail(AdminUser + " 비밀번호는 여기서 바꿀 수 없음 — root 가 auth.conf 의 admin= 줄을 직접 고치세요")
	}
	if !s.isAllowed(req.User) {
		return fail(req.User + " 은 cross_check.conf 의 users= 목록에 없음 — 먼저 목록에 추가하세요")
	}
	if err := s.auth.SetPassword(req.User, req.NewPassword); err != nil {
		return fail("비밀번호 설정 실패: " + err.Error())
	}
	s.thr.ok(req.User)
	s.logf("관리자가 비밀번호 재설정: %s", req.User)
	return Response{OK: true}
}

// pruneLoop 는 PruneInterval 마다 오래된 중단 작업을 지운다.
func (s *Server) pruneLoop() {
	t := time.NewTicker(s.cfg.PruneInterval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.prune()
		}
	}
}

// prune 은 중단된 지 PruneAge 가 지난 작업을 지우고(남은 token 도) 지운 개수를 돌려준다.
// 중단 시각은 UpdatedAt(없으면 CreatedAt)으로 본다. 대기·승인 작업은 건드리지 않는다.
func (s *Server) prune() int {
	cutoff := s.cfg.Now().Add(-s.cfg.PruneAge)
	s.mu.Lock()
	defer s.mu.Unlock()
	ids, err := s.st.DeleteWhere(func(j *store.Job) bool {
		if j.Status != StatusAborted {
			return false
		}
		if _, waiting := s.waiters[j.ID]; waiting {
			return false
		}
		at := j.UpdatedAt
		if at.IsZero() {
			at = j.CreatedAt
		}
		return at.Before(cutoff)
	})
	if err != nil {
		s.logf("오래된 중단 작업 정리 실패: %v", err)
		return 0
	}
	for _, id := range ids {
		delete(s.tokens, id)
		delete(s.pending, id)
		delete(s.working, id)
	}
	if len(ids) > 0 {
		s.logf("오래된 중단 작업 %d건 정리(%s 이전)", len(ids), cutoff.Format("2006-01-02 15:04:05"))
	}
	return len(ids)
}

// opAdminDelete 는 작업 하나를 지운다. 기다리던 작업자에게는 중단으로 알린다.
func (s *Server) opAdminDelete(req Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.st.Delete(req.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fail("작업을 찾을 수 없음: " + clip(req.ID))
		}
		return fail("저장 실패: " + err.Error())
	}
	delete(s.pending, req.ID)
	delete(s.working, req.ID)
	delete(s.tokens, req.ID)
	s.notifyLocked(req.ID, Event{Event: EventStatus, Status: StatusAborted, Reason: ReasonAdminDelete, By: ByAdmin}, true)
	s.logf("작업 %s 관리자 삭제", req.ID)
	return Response{OK: true}
}

// opAdminClear 는 모든 작업을 지운다. 기다리던 작업자 모두에게 중단으로 알린다.
func (s *Server) opAdminClear() Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.st.Clear()
	if err != nil {
		return fail("저장 실패: " + err.Error())
	}
	for id := range s.waiters {
		s.notifyLocked(id, Event{Event: EventStatus, Status: StatusAborted, Reason: ReasonAdminClear, By: ByAdmin}, true)
	}
	s.pending = map[string]time.Time{}
	s.working = map[string]time.Time{}
	s.tokens = map[string][sha256.Size]byte{}
	s.logf("관리자 전체 정리: %d건 삭제", n)
	return Response{OK: true, Count: n}
}
