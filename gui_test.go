package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"awx_copy/internal/fakeawx"
)

func TestLifecycle(t *testing.T) {
	t0 := time.Unix(1000, 0)
	l := newLifecycle(t0)
	if ok, _ := l.shouldExit(t0.Add(30 * time.Second)); ok {
		t.Fatal("창이 붙기 전 30초에는 종료하지 않는다")
	}
	if ok, why := l.shouldExit(t0.Add(61 * time.Second)); !ok || why != "창이 열리지 않음" {
		t.Fatal("창이 60초 안에 붙지 않으면 종료")
	}
	// 정상 닫힘: bye 뒤 1초
	l = newLifecycle(t0)
	l.ping("a", t0)
	l.bye("a", t0.Add(time.Second))
	if ok, _ := l.shouldExit(t0.Add(1500 * time.Millisecond)); ok {
		t.Fatal("bye 직후에는 새로고침을 기다린다")
	}
	if ok, why := l.shouldExit(t0.Add(2100 * time.Millisecond)); !ok || why != "창 닫힘" {
		t.Fatalf("bye 1초 뒤 종료: %s", why)
	}
	// 새로고침: bye 뒤 다른 id 로 ping
	l = newLifecycle(t0)
	l.ping("a", t0)
	l.bye("a", t0.Add(time.Second))
	l.ping("b", t0.Add(1300*time.Millisecond))
	if ok, _ := l.shouldExit(t0.Add(3 * time.Second)); ok {
		t.Fatal("새로고침은 종료가 아니다")
	}
	// 창이 둘(스크린샷 창 등): 하나만 닫히면 계속
	l.ping("c", t0.Add(3*time.Second))
	l.bye("c", t0.Add(4*time.Second))
	l.ping("b", t0.Add(4*time.Second))
	if ok, _ := l.shouldExit(t0.Add(6 * time.Second)); ok {
		t.Fatal("남은 창이 있으면 계속")
	}
	// 비정상 종료: ping 15초 끊김
	if ok, why := l.shouldExit(t0.Add(20 * time.Second)); !ok || !strings.Contains(why, "ping") {
		t.Fatalf("ping 끊김 종료: %v %s", ok, why)
	}
}

func TestRecvStore(t *testing.T) {
	s := &recvStore{path: filepath.Join(t.TempDir(), "received", "history.jsonl")}
	for i, ts := range []string{"2026-11-17T10:00:00+09:00", "2026-11-17T12:00:00+09:00", "2026-11-17T11:00:00+09:00"} {
		if err := s.append(recvRec{ID: string(rune('a' + i)), TS: ts, FileName: "한글.txt"}); err != nil {
			t.Fatal(err)
		}
	}
	rs, total, err := s.list(2)
	if err != nil || total != 3 || len(rs) != 2 || rs[0].ID != "b" || rs[1].ID != "c" {
		t.Fatalf("list: %v %d %+v", err, total, rs)
	}
	if err := s.update("a", func(r *recvRec) { r.Deleted = true }); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.get("a"); !r.Deleted || r.FileName != "한글.txt" {
		t.Fatalf("update: %+v", r)
	}
	if err := s.update("zz", func(*recvRec) {}); err == nil {
		t.Fatal("없는 기록")
	}
	b, _ := os.ReadFile(s.path)
	if !strings.Contains(string(b), "한글.txt") { // UTF-8 그대로
		t.Fatal("UTF-8 기록")
	}
}

// guiEnv 는 가짜 AWX 와 창 앱 서버를 만든다.
type guiEnv struct {
	t   *testing.T
	fs  *fakeawx.Server
	cfg *Config
	app *guiApp
	srv *httptest.Server
}

func newGUIEnv(t *testing.T) *guiEnv {
	t.Helper()
	fs, cfg := setup(t, 4096)
	fs.TrimRight = true
	fs.SetVars(5, "orig: gui")
	base := t.TempDir()
	app := newGUIApp(base)
	app.log = io.Discard
	if err := writeConfFile(filepath.Join(app.confDir, "kim.conf"), cfg.URL, cfg.Username, cfg.Password, 5, 4096, 20); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.handler())
	t.Cleanup(func() { app.shutdown(5 * time.Second); srv.Close() })
	return &guiEnv{t: t, fs: fs, cfg: cfg, app: app, srv: srv}
}

func (e *guiEnv) do(method, path string, body interface{}, header bool) (int, map[string]interface{}) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if header {
		req.Header.Set("X-AWX-Fetch", "1")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func (e *guiEnv) waitIdle() guiState {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if s := e.app.state(); !s.Receiving {
			return s
		}
		time.Sleep(50 * time.Millisecond)
	}
	e.t.Fatal("수신이 끝나지 않습니다")
	return guiState{}
}

func (e *guiEnv) upload(name string, data []byte) error {
	src := filepath.Join(e.t.TempDir(), name)
	_ = os.WriteFile(src, data, 0o600)
	return sendFile(context.Background(), newClient(e.cfg), e.cfg, src, "txt", io.Discard)
}

func TestGUIHandlers(t *testing.T) {
	e := newGUIEnv(t)
	if code, _ := e.do("GET", "/", nil, false); code != 200 {
		t.Fatalf("index %d", code)
	}
	code, m := e.do("GET", "/api/configs", nil, false)
	cs, _ := m["configs"].([]interface{})
	if code != 200 || len(cs) != 1 || strings.Contains(toJSON(m), e.cfg.Password) {
		t.Fatalf("configs: %d %v", code, m)
	}
	if code, _ := e.do("POST", "/api/select", map[string]string{"user": "kim"}, false); code != 403 {
		t.Fatalf("헤더 없는 POST 는 거부: %d", code)
	}
	if code, m := e.do("POST", "/api/select", map[string]string{"user": "../x"}, true); code != 400 || m["ok"] != false {
		t.Fatal("잘못된 이름 거부")
	}
	if code, m := e.do("POST", "/api/fetch", map[string]string{}, true); code != 409 || !strings.Contains(toJSON(m), "사용자") {
		t.Fatal("사용자 선택 전 수신 거부")
	}
	if code, m := e.do("POST", "/api/select", map[string]string{"user": "kim"}, true); code != 200 {
		t.Fatalf("select: %v", m)
	}
	if runtime.GOOS == "windows" {
		b, _ := os.ReadFile(filepath.Join(e.app.confDir, "kim.conf"))
		if !strings.Contains(string(b), "password_enc") || strings.Contains(string(b), e.cfg.Password) {
			t.Fatal("conf 는 DPAPI 암호문이어야 합니다")
		}
	}

	// 수신 정상: 한글 파일명, 윈도우 금지 문자, 탭·CRLF·끝 공백, 여러 조각
	data := []byte(strings.Repeat("이름\t값  \r\n", 900) + "끝  ")
	if code, _ := e.do("POST", "/api/fetch", map[string]string{}, true); code != 200 {
		t.Fatal("fetch 시작")
	}
	if err := e.upload("한글 a:b.txt", data); err != nil {
		t.Fatal(err)
	}
	s := e.waitIdle()
	if !s.OK || e.fs.Vars(5) != "orig: gui" {
		t.Fatalf("수신 결과: %+v vars=%q", s, e.fs.Vars(5))
	}
	_, h := e.do("GET", "/api/history", nil, false)
	recs := h["records"].([]interface{})
	r0 := recs[0].(map[string]interface{})
	p := r0["path"].(string)
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, data) || filepath.Base(p) != "한글 a_b.txt" || r0["status"] != "정상" || r0["sha_ok"] != true || r0["chunks"].(float64) < 2 {
		t.Fatalf("기록/파일: %v", r0)
	}
	// 같은 이름은 _1
	_, _ = e.do("POST", "/api/fetch", map[string]string{}, true)
	_ = e.upload("한글 a:b.txt", data)
	e.waitIdle()
	_, h = e.do("GET", "/api/history", nil, false)
	if p2 := h["records"].([]interface{})[0].(map[string]interface{})["path"].(string); filepath.Base(p2) != "한글 a_b_1.txt" {
		t.Fatalf("중복 이름: %s", p2)
	}

	// 수신 실패(텍스트가 아닌 바이트): 저장하지 않고 실패 기록, 업로더도 실패
	_, _ = e.do("POST", "/api/fetch", map[string]string{}, true)
	uerr := e.upload("bad.txt", []byte("ok\x07\n"))
	s = e.waitIdle()
	_, h = e.do("GET", "/api/history", nil, false)
	rb := h["records"].([]interface{})[0].(map[string]interface{})
	if uerr == nil || s.OK || rb["status"] != "실패" || rb["path"] != "" || rb["sha_ok"] != false {
		t.Fatalf("실패 처리: uerr=%v rec=%v", uerr, rb)
	}
	if _, err := os.Stat(filepath.Join(e.app.settings.SaveDir, "bad.txt")); err == nil {
		t.Fatal("실패 파일을 저장했습니다")
	}

	// 삭제: 파일만 지우고 기록은 "삭제됨"
	id := r0["id"].(string)
	if code, m := e.do("POST", "/api/history/action", map[string]string{"id": id, "action": "delete"}, true); code != 200 {
		t.Fatalf("삭제: %v", m)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("파일이 남았습니다")
	}
	if r, _ := e.app.store.get(id); !r.Deleted || !strings.Contains(r.Message, "삭제됨") {
		t.Fatalf("삭제 기록: %+v", r)
	}
	if code, _ := e.do("POST", "/api/history/action", map[string]string{"id": id, "action": "delete"}, true); code != 400 {
		t.Fatal("두 번 삭제는 오류")
	}
	if code, _ := e.do("POST", "/api/history/action", map[string]string{"id": id, "action": "rm -rf"}, true); code != 400 {
		t.Fatal("알 수 없는 동작")
	}

	// 저장 폴더 변경
	if code, _ := e.do("POST", "/api/settings", map[string]string{"save_dir": "relative\\dir"}, true); code != 400 {
		t.Fatal("상대 경로 거부")
	}
	nd := filepath.Join(t.TempDir(), "새 폴더")
	if code, m := e.do("POST", "/api/settings", map[string]string{"save_dir": nd}, true); code != 200 {
		t.Fatalf("폴더 변경: %v", m)
	}
	if b, _ := os.ReadFile(e.app.settingsPath); !strings.Contains(string(b), "새 폴더") {
		t.Fatal("설정 파일 저장")
	}

	// 자동 감시: 업로드를 감지해 새 폴더로 받는다
	if code, _ := e.do("POST", "/api/watch", map[string]bool{"on": true}, true); code != 200 {
		t.Fatal("감시 켜기")
	}
	if err := e.upload("auto.txt", []byte("자동\t감시\r\n")); err != nil {
		t.Fatal(err)
	}
	e.waitIdle()
	if _, err := os.Stat(filepath.Join(nd, "auto.txt")); err != nil {
		t.Fatalf("자동 감시 수신: %v", err)
	}
	_, _ = e.do("POST", "/api/watch", map[string]bool{"on": false}, true)
	if e.fs.Vars(5) != "orig: gui" {
		t.Fatalf("원복: %q", e.fs.Vars(5))
	}
	for p := range e.fs.Patches() {
		if p != "/api/v2/inventories/5/" {
			t.Fatalf("허용되지 않은 PATCH: %s", p)
		}
	}
}

// TestGUIStopRestores 는 수신 중 종료(창 닫힘)가 자기 세션을 원복하는지 본다.
func TestGUIStopRestores(t *testing.T) {
	e := newGUIEnv(t)
	if err := e.app.selectUser("kim"); err != nil {
		t.Fatal(err)
	}
	// 업로더가 1번 조각을 쓰고 사라진 상황 (3조각 중 1번)
	b := Block{"version": protoVersion, "session": "sX", "state": stateSending, "file_name": "x.txt", "type": "txt",
		"encoding": encText, "eol": eolLF, "final_newline": "false", "total_bytes": "3", "sha256": sha256Hex([]byte("abc")),
		"chunk_total": "3", "chunk_index": "1", "chunk_sha256": sha256Hex([]byte("a")), "chunk_text": "a", "ack": "0",
		"backup_b64": "b3JpZzogZ3Vp", "updated_at": nowStamp()}
	e.fs.SetVars(5, b.Render())
	if err := e.app.startReceive(false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for e.app.state().K < 1 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	start := time.Now()
	e.app.shutdown(5 * time.Second)
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("종료가 느립니다: %s", el)
	}
	if v := e.fs.Vars(5); v != "orig: gui" {
		t.Fatalf("원복 안 됨: %q", v)
	}
	if r, _, _ := e.app.store.list(1); len(r) != 1 || r[0].Status != "실패" {
		t.Fatalf("중단 기록: %+v", r)
	}
}

func TestGUIGuard(t *testing.T) {
	a := newGUIApp(t.TempDir())
	a.port = 18999
	h := a.handler()
	for _, c := range []struct {
		host, origin string
		want         int
	}{
		{"127.0.0.1:18999", "", 200},
		{"localhost:18999", "http://localhost:18999", 200},
		{"evil.example:18999", "", 403},
		{"127.0.0.1:18999", "http://evil.example", 403},
	} {
		req := httptest.NewRequest("GET", "/api/state", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != c.want {
			t.Errorf("%s %s → %d, 기대 %d", c.host, c.origin, w.Code, c.want)
		}
	}
	req := httptest.NewRequest("DELETE", "/api/state", nil)
	req.Host = "127.0.0.1:18999"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("DELETE → %d", w.Code)
	}
}

// TestGUINewConfig 는 새 설정 만들기(연결 테스트 → 저장 → 선택)를 본다.
func TestGUINewConfig(t *testing.T) {
	e := newGUIEnv(t)
	body := map[string]interface{}{"url": e.cfg.URL + "/", "username": e.cfg.Username, "password": "wrong", "inventory_id": "5"}
	if code, m := e.do("POST", "/api/configs/save", body, true); code != 400 || !strings.Contains(toJSON(m), "연결 테스트 실패") {
		t.Fatalf("틀린 비밀번호: %v", m)
	}
	body["password"] = e.cfg.Password
	body["test_only"] = true
	if code, _ := e.do("POST", "/api/configs/save", body, true); code != 200 {
		t.Fatal("연결 테스트")
	}
	if _, err := os.Stat(filepath.Join(e.app.confDir, e.cfg.Username+".conf")); err == nil {
		t.Fatal("테스트만 할 때는 저장하지 않는다")
	}
	body["test_only"] = false
	if code, m := e.do("POST", "/api/configs/save", body, true); code != 200 {
		t.Fatalf("저장: %v", m)
	}
	if e.app.state().User != e.cfg.Username {
		t.Fatal("저장 뒤 바로 선택")
	}
	if code, m := e.do("POST", "/api/configs/save", body, true); code != 400 || !strings.Contains(toJSON(m), "이미") {
		t.Fatal("덮어쓰기 확인")
	}
	for _, bad := range []map[string]interface{}{
		{"url": "ftp://x", "username": "a", "password": "p", "inventory_id": "1"},
		{"url": "http://x", "username": "../a", "password": "p", "inventory_id": "1"},
		{"url": "http://x", "username": "a", "password": "p", "inventory_id": "0"},
	} {
		if code, _ := e.do("POST", "/api/configs/save", bad, true); code != 400 {
			t.Errorf("입력 검사: %v", bad)
		}
	}
	b, _ := os.ReadFile(filepath.Join(e.app.confDir, e.cfg.Username+".conf"))
	if runtime.GOOS == "windows" && (strings.Contains(string(b), e.cfg.Password) || !strings.Contains(string(b), "password_enc")) {
		t.Fatal("DPAPI 저장")
	}
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
