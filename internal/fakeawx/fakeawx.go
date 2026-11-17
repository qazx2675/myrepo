// Package fakeawx 는 테스트용 가짜 AWX API 다. 인벤토리 GET/PATCH 와
// job_templates / inventory_sources 목록만 흉내 낸다.
package fakeawx

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
)

type Server struct {
	User, Pass string
	MaxBody    int64 // 0 이면 무제한. 넘으면 413

	mu      sync.Mutex
	vars    map[int]string
	patches map[string]int
}

func New(user, pass string) *Server {
	return &Server{User: user, Pass: pass, vars: map[int]string{5: ""}, patches: map[string]int{}}
}

func (s *Server) Vars(id int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vars[id]
}

func (s *Server) SetVars(id int, v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vars[id] = v
}

// Patches 는 경로별 PATCH 횟수다 (인벤토리 외 변경이 없는지 확인용).
func (s *Server) Patches() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]int{}
	for k, v := range s.patches {
		m[k] = v
	}
	return m
}

var invRe = regexp.MustCompile(`^/api/v2/inventories/(\d+)/$`)

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u, p, ok := r.BasicAuth()
	if !ok || u != s.User || p != s.Pass {
		writeJSON(w, 401, map[string]string{"detail": "Authentication credentials were not provided."})
		return
	}
	if r.Method == http.MethodPatch {
		s.mu.Lock()
		s.patches[r.URL.Path]++
		s.mu.Unlock()
	}
	switch r.URL.Path {
	case "/api/v2/job_templates/", "/api/v2/inventory_sources/":
		if r.Method != http.MethodGet {
			writeJSON(w, 405, map[string]string{"detail": "not allowed in fake"})
			return
		}
		writeJSON(w, 200, map[string]interface{}{"count": 1, "next": nil,
			"results": []map[string]interface{}{{"id": 1, "name": "x", "modified": "2026-01-01T00:00:00Z"}}})
		return
	}
	m := invRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		writeJSON(w, 404, map[string]string{"detail": "Not found."})
		return
	}
	id, _ := strconv.Atoi(m[1])
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]interface{}{"id": id, "variables": s.Vars(id)})
	case http.MethodPatch:
		var rd io.Reader = r.Body
		if s.MaxBody > 0 {
			rd = io.LimitReader(r.Body, s.MaxBody+1)
		}
		body, _ := io.ReadAll(rd)
		if s.MaxBody > 0 && int64(len(body)) > s.MaxBody {
			writeJSON(w, 413, map[string]string{"detail": "Request body too large"})
			return
		}
		var in map[string]string
		if err := json.Unmarshal(body, &in); err != nil {
			writeJSON(w, 400, map[string]string{"detail": "JSON parse error"})
			return
		}
		v, ok := in["variables"]
		if !ok || len(in) != 1 {
			writeJSON(w, 400, map[string]string{"detail": "fake accepts only variables"})
			return
		}
		s.SetVars(id, v)
		writeJSON(w, 200, map[string]interface{}{"id": id, "variables": v})
	default:
		writeJSON(w, 405, map[string]string{"detail": "Method not allowed"})
	}
}
