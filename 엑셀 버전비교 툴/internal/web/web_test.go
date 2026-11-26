package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"excel-diff/internal/report"
)

type client struct {
	t      *testing.T
	s      *Server
	base   string
	opened []string
}

type stateBody struct {
	Old, New *upload
	Pairs    []pair
	Job      *job
}

func start(t *testing.T) *client {
	t.Helper()
	ts := httptest.NewUnstartedServer(nil)
	c := &client{t: t}
	s, err := New(Config{Host: ts.Listener.Addr().String(), OutDir: t.TempDir(),
		Open: func(p string) error { c.opened = append(c.opened, p); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = s.Handler()
	ts.Start()
	t.Cleanup(func() {
		c.idle()
		ts.Close()
		if err := s.Close(); err != nil {
			t.Error(err)
		}
		if _, err := os.Stat(s.tempDir); !os.IsNotExist(err) {
			t.Error("임시 폴더가 남았다", err)
		}
	})
	c.s, c.base = s, ts.URL
	return c
}

func (c *client) do(method, path string, body io.Reader, edit func(*http.Request)) (int, string) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("X-Token", c.s.token)
	if edit != nil {
		edit(req)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return res.StatusCode, string(b)
}

func (c *client) want(status int, method, path string, body io.Reader) string {
	c.t.Helper()
	got, text := c.do(method, path, body, nil)
	if got != status {
		c.t.Fatalf("%s %s: %d %s, want %d", method, path, got, text, status)
	}
	return text
}

func (c *client) state() stateBody {
	c.t.Helper()
	var s stateBody
	if err := json.Unmarshal([]byte(c.want(200, "GET", "/api/state", nil)), &s); err != nil {
		c.t.Fatal(err)
	}
	return s
}

// idle 은 진행 중인 작업이 없을 때까지 기다린다.
func (c *client) idle() {
	c.t.Helper()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		c.s.mu.Lock()
		n := c.s.active
		c.s.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			c.t.Fatal("작업이 끝나지 않는다")
		}
	}
}

func (c *client) upload(side, name string, data []byte) {
	c.t.Helper()
	c.want(200, "POST", "/api/upload?"+url.Values{"side": {side}, "name": {name}}.Encode(), bytes.NewReader(data))
	c.idle()
}

func book(t *testing.T, rows ...[]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	b, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (c *client) ready() {
	c.t.Helper()
	c.upload("old", "이전.xlsx", book(c.t, []any{"ID", "상태"}, []any{"a", "대기"}))
	c.upload("new", "최신.xlsx", book(c.t, []any{"ID", "상태"}, []any{"a", "완료"}))
}

const onePair = `{"pairs":[{"old":"Sheet1","new":"Sheet1","oldKey":1,"newKey":1}]}`

func TestAccessControl(t *testing.T) {
	c := start(t)
	for name, edit := range map[string]func(*http.Request){
		"다른 Host":     func(r *http.Request) { r.Host = "evil.example:80" },
		"localhost":   func(r *http.Request) { r.Host = strings.Replace(r.Host, "127.0.0.1", "localhost", 1) },
		"토큰 없음":       func(r *http.Request) { r.Header.Del("X-Token") },
		"틀린 토큰":       func(r *http.Request) { r.Header.Set("X-Token", "x") },
		"질의 토큰(닫힘 외)": func(r *http.Request) { r.Header.Del("X-Token"); r.URL.RawQuery = "t=" + c.s.token },
	} {
		for _, p := range [][2]string{{"GET", "/api/state"}, {"POST", "/api/open?what=file"}, {"POST", "/api/compare"}} {
			if got, _ := c.do(p[0], p[1], nil, edit); got != 403 {
				t.Errorf("%s %s: %d", name, p[1], got)
			}
		}
	}
	if got, _ := c.do("GET", "/", nil, func(r *http.Request) { r.Host = "evil.example" }); got != 403 {
		t.Error("정적 파일의 Host 확인", got)
	}
	for _, p := range []string{"/", "/app.js", "/style.css"} {
		c.want(200, "GET", p, nil)
	}
}

func TestFlow(t *testing.T) {
	c := start(t)
	c.want(409, "POST", "/api/open?what=file", nil)
	c.want(409, "POST", "/api/compare", strings.NewReader(onePair))
	c.ready()
	st := c.state()
	if st.Old.Status != statusReady || st.New.Name != "최신.xlsx" || len(st.Pairs) != 1 || st.Pairs[0].Old != "Sheet1" {
		t.Fatalf("%+v %+v %v", st.Old, st.New, st.Pairs)
	}
	if col := st.Old.Sheets[0].Columns[1]; col.Letter != "B" || col.Header != "상태" {
		t.Fatalf("%+v", col)
	}
	if left, _ := os.ReadDir(c.s.tempDir); len(left) != 0 {
		t.Error("읽은 뒤 업로드 파일이 남았다", left)
	}
	c.want(400, "POST", "/api/compare", strings.NewReader(`{"pairs":[]}`))

	wb := c.s.files[0].wb
	var paths []string
	for range 2 { // 다시 비교해도 다시 읽지 않고 새 파일을 만든다
		c.want(202, "POST", "/api/compare", strings.NewReader(onePair))
		c.idle()
		j := c.state().Job
		if j.Status != statusDone || j.Result.Items != 1 || j.Result.Summary.ModifiedCells != 1 || j.Result.OldName != "이전.xlsx" {
			t.Fatalf("%+v %+v", j, j.Result)
		}
		paths = append(paths, j.Result.Path)
	}
	if paths[0] == paths[1] || c.s.files[0].wb != wb {
		t.Fatal(paths)
	}
	f, err := excelize.OpenFile(paths[1])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if v, _ := f.GetCellValue("요약", "B2"); v != "이전.xlsx" {
		t.Error("요약의 파일명", v)
	}

	// 열기는 요청에 담긴 경로를 무시한다.
	c.want(200, "POST", "/api/open?what=file&path=C:/Windows/notepad.exe", nil)
	c.want(200, "POST", "/api/open?what=folder", nil)
	c.want(400, "POST", "/api/open?what=C:/x", nil)
	if len(c.opened) != 2 || c.opened[0] != paths[1] || c.opened[1] != filepath.Dir(paths[1]) {
		t.Fatal(c.opened)
	}
	// 새 파일을 올리면 이전 결과는 버린다.
	c.upload("new", "x.xlsx", book(t, []any{"ID"}))
	if c.state().Job != nil {
		t.Error("결과가 남았다")
	}
	c.want(409, "POST", "/api/open?what=file", nil)
}

func TestBadFiles(t *testing.T) {
	c := start(t)
	for name, wantText := range map[string]string{"a.xls": ".xls", "깨진.xlsx": "손상", `..\..\b.csv`: "CSV"} {
		c.upload("old", name, []byte("not excel"))
		u := c.state().Old
		base := name[strings.LastIndex(name, `\`)+1:]
		if u.Status != statusError || !strings.HasPrefix(u.Error, base+":") || !strings.Contains(u.Error, wantText) {
			t.Errorf("%s: %+v", name, u)
		}
	}
	c.want(400, "POST", "/api/upload?side=other&name=a.xlsx", nil)
	c.want(400, "POST", "/api/upload?side=old&name=", nil)
}

func TestRunningJob(t *testing.T) {
	c := start(t)
	release := make(chan struct{})
	c.s.run = func(o report.Options) (*report.Output, error) {
		o.Progress(42, "시험 단계")
		<-release
		panic("시험 패닉")
	}
	c.ready()
	c.want(202, "POST", "/api/compare", strings.NewReader(onePair))
	for c.state().Job.Percent != 42 {
		time.Sleep(10 * time.Millisecond)
	}
	c.want(409, "POST", "/api/compare", strings.NewReader(onePair))
	c.want(409, "POST", "/api/upload?side=old&name=a.xlsx", strings.NewReader("data"))
	// 작업 중에는 창을 닫았거나 응답이 오래 없어도 끝내지 않는다.
	c.want(200, "POST", "/api/closing", nil)
	if c.s.expired(time.Now().Add(24 * time.Hour)) {
		t.Fatal("작업 중 종료")
	}
	close(release)
	c.idle()
	if !c.s.expired(time.Now().Add(closeGrace + time.Second)) {
		t.Error("작업이 끝나면 창 닫힘 신호로 종료해야 한다")
	}
	j := c.state().Job
	if j.Status != statusError || !strings.Contains(j.Error, "내부 오류") || !strings.Contains(j.Error, "시험 패닉") {
		t.Fatalf("%+v", j)
	}
	c.s.run = report.Run // 패닉 뒤에도 다시 비교할 수 있다
	c.want(202, "POST", "/api/compare", strings.NewReader(onePair))
	c.idle()
	if j := c.state().Job; j.Status != statusDone {
		t.Fatalf("%+v", j)
	}
}

func TestExpiry(t *testing.T) {
	c := start(t)
	c.state()
	now := time.Now()
	if c.s.expired(now.Add(heartbeatTimeout-time.Minute)) || !c.s.expired(now.Add(heartbeatTimeout+time.Minute)) {
		t.Error("하트비트 한도")
	}
	// sendBeacon 처럼 질의 문자열의 토큰
	if got, _ := c.do("POST", "/api/closing?t="+c.s.token, nil, func(r *http.Request) { r.Header.Del("X-Token") }); got != 200 {
		t.Fatal(got)
	}
	later := time.Now().Add(closeGrace + time.Second)
	if c.s.expired(time.Now()) || !c.s.expired(later) {
		t.Error("창 닫힘 유예")
	}
	c.state() // 새로고침하면 닫힘 신호를 취소한다
	if c.s.expired(later) {
		t.Error("새 요청 뒤에도 종료")
	}
}
