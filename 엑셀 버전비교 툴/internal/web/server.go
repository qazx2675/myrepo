package web

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"excel-diff/internal/report"
)

//go:embed static
var staticFS embed.FS

const (
	// 창이 백그라운드·최소화되면 브라우저가 타이머를 1분에 1번 수준으로 늦추므로 한도는 넉넉히 둔다.
	heartbeatTimeout = 10 * time.Minute
	// 창 닫힘 신호 뒤 이 시간 동안 새 요청이 없으면 종료한다(새로고침 구분).
	closeGrace    = 10 * time.Second
	watchInterval = 2 * time.Second
)

// Config 는 New 의 입력이다.
type Config struct {
	Host        string                  // 허용하는 Host 헤더("127.0.0.1:포트")
	OutDir      string                  // 결과 폴더
	FallbackDir string                  // OutDir 에 쓸 수 없을 때의 폴더
	Open        func(path string) error // 결과 파일·폴더 열기
}

// Server 는 화면과 API 를 제공한다.
type Server struct {
	cfg     Config
	token   string
	tempDir string
	run     func(report.Options) (*report.Output, error)

	work sync.Mutex // 읽기·비교는 한 번에 하나만 한다

	mu       sync.Mutex
	files    [2]*upload // 0 이전, 1 최신
	job      *job
	nextID   int
	active   int       // 진행 중인 업로드·읽기·비교 수. 0 이 아니면 종료하지 않는다
	lastSeen time.Time // 마지막 요청 또는 작업 종료 시각
	closedAt time.Time // 창 닫힘 신호 시각. 0 이면 없음
}

// New 는 임시 폴더와 실행별 토큰을 만든다. 다 쓰면 Close 를 불러야 한다.
func New(cfg Config) (*Server, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "excel-diff-*")
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, token: hex.EncodeToString(b), tempDir: dir, run: report.Run, lastSeen: time.Now()}, nil
}

// URL 은 브라우저로 열 주소이다. 토큰은 서버로 전송되지 않는 조각(#)에 담는다.
func (s *Server) URL() string { return "http://" + s.cfg.Host + "/#" + s.token }

// Close 는 임시 파일을 지운다.
func (s *Server) Close() error { return os.RemoveAll(s.tempDir) }

// Handler 는 Host 와 토큰을 확인하는 HTTP 처리기이다.
func (s *Server) Handler() http.Handler {
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embed 한 폴더라 일어나지 않는다
	}
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	api := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			// sendBeacon 은 헤더를 붙일 수 없어 창 닫힘 신호만 질의 문자열도 받는다.
			token := r.Header.Get("X-Token")
			if token == "" && r.URL.Path == "/api/closing" {
				token = r.URL.Query().Get("t")
			}
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) != 1 {
				fail(w, http.StatusForbidden, "허용되지 않은 요청입니다. 프로그램을 다시 실행하세요")
				return
			}
			if r.URL.Path != "/api/closing" {
				s.mu.Lock()
				s.lastSeen, s.closedAt = time.Now(), time.Time{}
				s.mu.Unlock()
			}
			h(w, r)
		})
	}
	api("GET /api/state", s.handleState)
	api("POST /api/upload", s.handleUpload)
	api("POST /api/compare", s.handleCompare)
	api("POST /api/open", s.handleOpen)
	api("POST /api/closing", s.handleClosing)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 다른 도메인 이름이 127.0.0.1 을 가리키게 하는 공격(DNS rebinding)을 막는다.
		if r.Host != s.cfg.Host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src data:; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("요청 처리 패닉: %v\n%s", rec, debug.Stack())
				fail(w, http.StatusInternalServerError, "내부 오류가 발생했습니다")
			}
		}()
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) handleClosing(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.closedAt = time.Now()
	s.mu.Unlock()
	reply(w, http.StatusOK, map[string]any{})
}

// expired 는 프로그램을 끝내도 되는지 알려준다. 작업 중에는 항상 false 이다.
func (s *Server) expired(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active > 0 {
		return false
	}
	return now.Sub(s.lastSeen) > heartbeatTimeout || !s.closedAt.IsZero() && now.Sub(s.closedAt) > closeGrace
}

// Wait 는 창이 닫혔거나 응답이 오래 없어 종료해야 할 때까지 기다린다.
func (s *Server) Wait() {
	last := time.Now()
	for range time.Tick(watchInterval) {
		// Round(0) 은 단조 시계를 떼어 절전 시간도 간격에 포함시킨다.
		now := time.Now()
		if now.Round(0).Sub(last.Round(0)) > 30*time.Second { // 절전에서 깨어난 경우 화면의 요청을 다시 기다린다
			s.mu.Lock()
			s.lastSeen = now
			s.mu.Unlock()
		}
		last = now
		if s.expired(now) {
			return
		}
	}
}

// done 은 작업 하나가 끝났음을 기록한다.
func (s *Server) done() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active--
	s.lastSeen = time.Now()
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("응답 전송 실패: %v", err)
	}
}

func fail(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]string{"error": message})
}
