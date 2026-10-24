// Package server 는 내장 프론트엔드 정적 파일과 REST API 를 제공하는 로컬 HTTP 서버.
package server

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
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
	return mux, tracker
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
