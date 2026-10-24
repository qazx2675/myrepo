// Package server 는 내장 프론트엔드 정적 파일과 REST API 를 제공하는 로컬 HTTP 서버.
package server

import (
	"encoding/json"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"datacenter-viewer/internal/csvproc"
	"datacenter-viewer/internal/model"
	"datacenter-viewer/internal/storage"
)

// Tracker 는 브라우저 앱 창의 연결/생존(Heartbeat) 상태를 추적하여
// Edge/Chrome 이 기존 백그라운드 프로세스로 창 생성을 위임하고 즉시 종료되더라도
// 실제 브라우저 창이 열려 있는 동안 Go 서버가 절대 죽지 않도록 보장한다.
type Tracker struct {
	connected    atomic.Bool
	lastPingNano atomic.Int64
	byeRequested atomic.Bool
}

func (t *Tracker) Touch() {
	t.connected.Store(true)
	t.byeRequested.Store(false)
	t.lastPingNano.Store(time.Now().UnixNano())
}

func (t *Tracker) Bye() {
	t.byeRequested.Store(true)
	t.lastPingNano.Store(time.Now().UnixNano())
}

func (t *Tracker) IsConnected() bool {
	return t.connected.Load()
}

func (t *Tracker) ShouldExit() bool {
	if !t.connected.Load() {
		return false
	}
	last := time.Unix(0, t.lastPingNano.Load())
	elapsed := time.Since(last)
	if t.byeRequested.Load() && elapsed > 1500*time.Millisecond {
		return true
	}
	return elapsed > 12*time.Second
}

// New 는 정적 파일(static)과 API 를 묶은 http.Handler 와 세션 Tracker 를 만든다.
func New(static fs.FS, st *storage.Store) (http.Handler, *Tracker) {
	mux := http.NewServeMux()
	tracker := &Tracker{}

	// 서버 자체 준비 상태 확인용 (브라우저 실행 전 셀프 체크)
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ready": true})
	})

	// 브라우저 창 생존 하트비트 (2초 주기로 호출됨)
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		tracker.Touch()
		writeJSON(w, map[string]any{"alive": true})
	})

	// 브라우저 창 닫힘 알림 (beforeunload / pagehide 에서 호출됨)
	mux.HandleFunc("/api/bye", func(w http.ResponseWriter, r *http.Request) {
		tracker.Bye()
		writeJSON(w, map[string]any{"bye": true})
	})

	mux.HandleFunc("/api/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		tracker.Touch()
		layout, csv, err := st.Load()
		if err != nil {
			httpErr(w, err)
			return
		}
		var raw json.RawMessage
		if len(layout) > 0 && json.Valid(layout) {
			raw = layout
		}
		writeJSON(w, map[string]any{
			"goBackend": true,
			"layout":    raw,
			"csv":       csv,
			"dataDir":   st.Dir(),
			"info":      st.Info(),
		})
	})

	// 네트워크 공유 폴더 조회 및 변경 API
	mux.HandleFunc("/api/config/data-dir", func(w http.ResponseWriter, r *http.Request) {
		tracker.Touch()
		if r.Method == http.MethodGet {
			writeJSON(w, st.Info())
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "GET or POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			DataDir string `json:"dataDir"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := st.SetDir(req.DataDir); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// 변경된 공유 폴더에서 기존 데이터 로드 시도
		layout, csv, _ := st.Load()
		var raw json.RawMessage
		if len(layout) > 0 && json.Valid(layout) {
			raw = layout
		}
		writeJSON(w, map[string]any{
			"ok":      true,
			"dataDir": st.Dir(),
			"layout":  raw,
			"csv":     csv,
			"info":    st.Info(),
		})
	})

	// 공유 폴더 최신 데이터 다시 불러오기(동기화) API
	mux.HandleFunc("/api/sync", func(w http.ResponseWriter, r *http.Request) {
		tracker.Touch()
		layout, csv, err := st.Load()
		if err != nil {
			httpErr(w, err)
			return
		}
		var raw json.RawMessage
		if len(layout) > 0 && json.Valid(layout) {
			raw = layout
		}
		writeJSON(w, map[string]any{
			"ok":      true,
			"dataDir": st.Dir(),
			"layout":  raw,
			"csv":     csv,
			"info":    st.Info(),
		})
	})

	mux.HandleFunc("/api/save", func(w http.ResponseWriter, r *http.Request) {
		tracker.Touch()
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<20))
		if err != nil {
			httpErr(w, err)
			return
		}
		var req struct {
			Layout json.RawMessage `json:"layout"`
			CSV    string          `json:"csv"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := st.Save(req.Layout, req.CSV); err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/export/allocated-csv", func(w http.ResponseWriter, r *http.Request) {
		tracker.Touch()
		csv, err := st.CSV()
		if err != nil {
			httpErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="allocated_hosts.csv"`)
		_, _ = io.WriteString(w, csv)
	})

	// Go 검증기 (JS validator.js 와 동일 규칙). 프론트는 기본적으로 JS 검증기를 사용하며 이 API 는 대용량/패리티 검증용.
	mux.HandleFunc("/api/csv/validate", func(w http.ResponseWriter, r *http.Request) {
		tracker.Touch()
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Text            string         `json:"text"`
			DefaultSiteID   string         `json:"defaultSiteId"`
			AllSites        []model.Site   `json:"allSites"`
			ExistingDevices []model.Device `json:"existingDevices"`
			IsSimulation    bool           `json:"isSimulation"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<20)).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res := csvproc.Validate(csvproc.ParseFlexibleCsvOrTsv(req.Text), csvproc.Options{
			DefaultSiteID: req.DefaultSiteID, AllSites: req.AllSites, ExistingDevices: req.ExistingDevices,
			AutoCreateMissingRacks: true, IsSimulation: req.IsSimulation,
		})
		writeJSON(w, res)
	})

	fileServer := http.FileServer(http.FS(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tracker.Touch()
		w.Header().Set("Cache-Control", "no-store")
		fileServer.ServeHTTP(w, r)
	})
	return guard(mux), tracker
}

// contentSecurityPolicy 는 외부 스크립트/프레임 삽입을 막는다. 3D 엔진·UI 는 모두 같은 출처의 파일만 사용하고
// 화면 요소는 인라인 style 속성을 쓰므로 style-src 에만 'unsafe-inline' 을 허용한다.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; connect-src 'self'; worker-src 'self' blob:; object-src 'none'; " +
	"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// isLoopbackHost 는 Host/Origin 의 호스트 부분이 이 PC 자신(127.0.0.1, localhost, ::1)인지 확인한다.
func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost")
}

// guard 는 다른 웹사이트가 사용자의 브라우저를 통해 로컬 API 를 호출하는 공격을 막는다.
//   - Host 검사: DNS 리바인딩(공격자 도메인을 127.0.0.1 로 바꿔 데이터를 읽어가는 공격) 차단
//   - Origin/Sec-Fetch-Site 검사: 외부 사이트가 몰래 보내는 저장/폴더 변경 요청(CSRF) 차단
//   - POST 는 application/json 만 허용: 브라우저가 사전 확인(preflight) 없이 보낼 수 있는 폼/텍스트 요청 차단
//     (/api/bye 는 창 닫힘 비콘이라 본문이 없고 상태 변경도 없어 예외)
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
			u, err := url.Parse(origin)
			if err != nil || !isLoopbackHost(u.Host) || u.Host != r.Host {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
			http.Error(w, "forbidden cross-site request", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.URL.Path != "/api/bye" {
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" {
				http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
				return
			}
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
