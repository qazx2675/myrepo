package main

// 윈도우 창 앱 (awx_fetch.exe). 로컬 HTTP 서버(127.0.0.1) + Edge/Chrome --app 창.
// 화면: 사용자 선택, 새 설정, 수신(지금 가져오기 / 자동 감시), 관리 공간(받은 파일 기록).
// 변경 대상은 인벤토리 variables 뿐이다. 수신은 fetch.go 를 그대로 쓴다.

import (
	"bufio"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed webui
var webFS embed.FS

const (
	guiTitle       = "AWX_copy 수신기"
	guiDefaultPort = 18780
	guiMutexName   = `Local\AWX_copy_awx_fetch`
	guiWatchEvery  = 2 * time.Second
	histMaxList    = 1000
	recTSFormat    = "2006-01-02T15:04:05.000Z07:00" // 고정 길이(문자열 정렬 = 시간 정렬)
)

// ---------- 수명 관리 ----------

// lifecycle 은 창(클라이언트)들의 ping 을 기록하고 종료 시점을 정한다.
type lifecycle struct {
	mu         sync.Mutex
	clients    map[string]time.Time
	seen       bool
	emptySince time.Time
	started    time.Time
	byeGrace   time.Duration // bye 뒤 새 ping(새로고침)을 기다리는 시간
	stale      time.Duration // ping 이 끊긴 창을 닫힌 것으로 보는 시간
	noClient   time.Duration // 창이 한 번도 붙지 않을 때 종료까지 시간 (0 이면 무한)
}

func newLifecycle(now time.Time) *lifecycle {
	return &lifecycle{clients: map[string]time.Time{}, started: now, byeGrace: time.Second, stale: 15 * time.Second, noClient: 60 * time.Second}
}

func (l *lifecycle) ping(id string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if id == "" {
		id = "-"
	}
	l.clients[id] = now
	l.seen = true
}

func (l *lifecycle) bye(id string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if id == "" {
		id = "-"
	}
	delete(l.clients, id)
	if len(l.clients) == 0 {
		l.emptySince = now
	}
}

// shouldExit 는 종료할지와 이유를 돌려준다.
func (l *lifecycle) shouldExit(now time.Time) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for id, t := range l.clients {
		if now.Sub(t) >= l.stale {
			delete(l.clients, id)
			if len(l.clients) == 0 {
				return true, "창 응답 없음(ping 끊김)"
			}
		}
	}
	if l.seen && len(l.clients) == 0 && now.Sub(l.emptySince) >= l.byeGrace {
		return true, "창 닫힘"
	}
	if !l.seen && l.noClient > 0 && now.Sub(l.started) >= l.noClient {
		return true, "창이 열리지 않음"
	}
	return false, ""
}

func (l *lifecycle) hasSeen() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seen
}

// ---------- 받은 파일 기록 ----------

// recvRec 은 받은 파일 기록 한 줄이다 (received\history.jsonl, UTF-8).
type recvRec struct {
	ID       string  `json:"id"`
	TS       string  `json:"ts"`
	User     string  `json:"user"`
	FileName string  `json:"file_name"`
	Size     int64   `json:"size"`
	Chunks   int     `json:"chunks"`
	SHAOK    bool    `json:"sha_ok"`
	Status   string  `json:"status"` // 정상 | 실패
	Path     string  `json:"path"`
	Seconds  float64 `json:"seconds"`
	Message  string  `json:"message"`
	Session  string  `json:"session"`
	Deleted  bool    `json:"deleted"`
}

type recvStore struct {
	mu   sync.Mutex
	path string
}

func (s *recvStore) readAll() ([]recvRec, error) {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []recvRec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var r recvRec
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.ID != "" {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}

func (s *recvStore) append(r recvRec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(r)
	_, err = f.Write(append(b, '\n'))
	return err
}

// list 는 최신 순 최대 limit 건이다.
func (s *recvStore) list(limit int) ([]recvRec, int, error) {
	s.mu.Lock()
	rs, err := s.readAll()
	s.mu.Unlock()
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 { // 같은 시각이면 나중에 쓴 줄이 먼저
		rs[i], rs[j] = rs[j], rs[i]
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].TS > rs[j].TS })
	total := len(rs)
	if limit > 0 && len(rs) > limit {
		rs = rs[:limit]
	}
	return rs, total, err
}

func (s *recvStore) get(id string) (recvRec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rs, _ := s.readAll()
	for _, r := range rs {
		if r.ID == id {
			return r, true
		}
	}
	return recvRec{}, false
}

// update 는 한 줄을 바꾸고 파일을 원자적으로 다시 쓴다.
func (s *recvStore) update(id string, f func(*recvRec)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rs, err := s.readAll()
	if err != nil {
		return err
	}
	found := false
	var sb strings.Builder
	for i := range rs {
		if rs[i].ID == id {
			f(&rs[i])
			found = true
		}
		b, _ := json.Marshal(rs[i])
		sb.Write(b)
		sb.WriteByte('\n')
	}
	if !found {
		return errors.New("기록이 없습니다")
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ---------- 로그 ----------

// fileLog 는 UTF-8 로그 파일이다. 5 MiB 를 넘으면 .1 로 돌린다. 줄마다 시각을 붙인다.
type fileLog struct {
	mu   sync.Mutex
	path string
}

func (l *fileLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if st, err := os.Stat(l.path); err == nil && st.Size() > 5<<20 {
		_ = os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return len(p), nil // 로그 실패로 수신을 멈추지 않는다
	}
	defer f.Close()
	ts := time.Now().Format("2006-01-02 15:04:05 ")
	for _, line := range strings.SplitAfter(string(p), "\n") {
		if line != "" {
			_, _ = f.WriteString(ts + line)
		}
	}
	return len(p), nil
}

// ---------- 앱 ----------

type guiSettings struct {
	SaveDir string `json:"save_dir"`
}

type guiApp struct {
	baseDir      string
	confDir      string
	settingsPath string
	store        *recvStore
	log          io.Writer
	lc           *lifecycle
	port         int

	rootCtx  context.Context
	rootStop context.CancelFunc

	mu        sync.Mutex
	settings  guiSettings
	cfg       *Config
	user      string
	client    *Client
	recvStop  context.CancelFunc
	recvDone  chan struct{}
	obs       *fetchObs
	auto      bool // 현재 수신이 자동 감시로 시작됨
	watch     bool
	watchStop chan struct{}
	lastMsg   string
	lastOK    bool
	histRev   int64
	focusReq  bool

	exitOnce sync.Once
	exitCh   chan struct{}
	exitWhy  string
}

func newGUIApp(baseDir string) *guiApp {
	ctx, stop := context.WithCancel(context.Background())
	a := &guiApp{
		baseDir: baseDir, confDir: filepath.Join(baseDir, "conf"),
		settingsPath: filepath.Join(baseDir, "awx_fetch.json"),
		store:        &recvStore{path: filepath.Join(baseDir, "received", "history.jsonl")},
		log:          &fileLog{path: filepath.Join(baseDir, "awx_fetch.log")},
		lc:           newLifecycle(time.Now()),
		rootCtx:      ctx, rootStop: stop,
		exitCh: make(chan struct{}),
	}
	a.settings.SaveDir = filepath.Join(baseDir, "received")
	if b, err := os.ReadFile(a.settingsPath); err == nil {
		var s guiSettings
		if json.Unmarshal(b, &s) == nil && s.SaveDir != "" {
			a.settings = s
		}
	}
	return a
}

func (a *guiApp) logf(format string, args ...interface{}) {
	fmt.Fprintf(a.log, format+"\n", args...)
}

// requestExit 는 한 번만 종료를 시작한다.
func (a *guiApp) requestExit(why string) {
	a.exitOnce.Do(func() {
		a.exitWhy = why
		close(a.exitCh)
	})
}

// shutdown 은 진행 중 수신을 멈추고(자기 세션이면 원복) 감시를 끈다. 최대 wait 만큼 기다린다.
func (a *guiApp) shutdown(wait time.Duration) {
	a.mu.Lock()
	stop, done := a.recvStop, a.recvDone
	if a.watchStop != nil {
		close(a.watchStop)
		a.watchStop = nil
	}
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
	a.rootStop()
	if done != nil {
		select {
		case <-done:
		case <-time.After(wait):
			a.logf("경고: 수신 정리가 %s 안에 끝나지 않았습니다", wait)
		}
	}
}

// watchLifecycle 은 250ms 마다 창 상태를 보고 종료를 요청한다. relaunch 는 창이 붙지 않을 때 한 번 부른다.
func (a *guiApp) watchLifecycle(relaunch func()) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	relaunched := false
	for {
		select {
		case <-a.exitCh:
			return
		case now := <-t.C:
			if !relaunched && relaunch != nil && !a.lc.hasSeen() && now.Sub(a.lc.started) > 20*time.Second {
				relaunched = true
				relaunch()
			}
			if ok, why := a.lc.shouldExit(now); ok {
				a.requestExit(why)
				return
			}
		}
	}
}

// ---------- 설정 ----------

type confInfo struct {
	User        string `json:"user"`
	URL         string `json:"url"`
	InventoryID string `json:"inventory_id"`
	Username    string `json:"username"`
	Encrypted   bool   `json:"encrypted"`
}

func confHasEnc(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(b), "password_enc")
}

func (a *guiApp) listConfs() []confInfo {
	var out []confInfo
	for _, c := range listCandidates(a.confDir, "") {
		out = append(out, confInfo{User: c.User(), URL: c.URL, InventoryID: c.InventoryID, Username: c.Username, Encrypted: confHasEnc(c.Path)})
	}
	return out
}

// testConn 은 인벤토리 GET 한 번으로 연결과 인증을 확인한다 (variables 를 바꾸지 않는다).
func testConn(ctx context.Context, cfg *Config) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := newClient(cfg).GetVars(ctx)
	return err
}

func (a *guiApp) selectUser(user string) error {
	if !validConfName(user) {
		return fmt.Errorf("사용자 이름이 올바르지 않습니다: %q", user)
	}
	p := filepath.Join(a.confDir, user+".conf")
	cfg, err := loadConfig(p)
	if err != nil {
		return err
	}
	if err := testConn(a.rootCtx, cfg); err != nil {
		return fmt.Errorf("연결 실패: %v", err)
	}
	if !cfg.PasswordEnc { // 평문 password → DPAPI 암호문으로 바꿔 저장
		if werr := writeConfFile(p, cfg.URL, cfg.Username, cfg.Password, cfg.InventoryID, cfg.ChunkSize, int(cfg.Timeout/time.Second)); werr == nil {
			a.logf("%s: 평문 password 를 password_enc(DPAPI)로 바꿨습니다", p)
		} else {
			a.logf("%s: 암호화 저장 실패: %v", p, werr)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.recvStop != nil {
		return errors.New("수신 중에는 사용자를 바꿀 수 없습니다")
	}
	a.cfg, a.user, a.client = cfg, user, newClient(cfg)
	a.lastMsg, a.lastOK = fmt.Sprintf("%s 선택 (inventory %d)", user, cfg.InventoryID), true
	a.logf("사용자 선택: %s (%s, inventory %d)", user, cfg.URL, cfg.InventoryID)
	return nil
}

type newConfReq struct {
	URL         string `json:"url"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	InventoryID string `json:"inventory_id"`
	Overwrite   bool   `json:"overwrite"`
	TestOnly    bool   `json:"test_only"`
}

func (r newConfReq) config() (*Config, error) {
	u := strings.TrimRight(strings.TrimSpace(r.URL), "/")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return nil, errors.New("url 은 http:// 또는 https:// 로 시작해야 합니다")
	}
	if !validConfName(r.Username) {
		return nil, errors.New("username 이 올바르지 않습니다")
	}
	if r.Password == "" {
		return nil, errors.New("password 가 필요합니다")
	}
	id, err := strconv.Atoi(strings.TrimSpace(r.InventoryID))
	if err != nil || id <= 0 {
		return nil, errors.New("inventory_id 는 양의 정수여야 합니다")
	}
	return &Config{URL: u, Username: r.Username, Password: r.Password, InventoryID: id, ChunkSize: defaultChunkSize, Timeout: defaultTimeout * time.Second}, nil
}

func (a *guiApp) saveNewConf(r newConfReq) error {
	cfg, err := r.config()
	if err != nil {
		return err
	}
	if err := testConn(a.rootCtx, cfg); err != nil {
		return fmt.Errorf("연결 테스트 실패: %v", err)
	}
	if r.TestOnly {
		return nil
	}
	p := filepath.Join(a.confDir, r.Username+".conf")
	if _, err := os.Stat(p); err == nil && !r.Overwrite {
		return fmt.Errorf("%s.conf 가 이미 있습니다. 덮어쓰기를 선택하세요", r.Username)
	}
	if err := writeConfFile(p, cfg.URL, cfg.Username, cfg.Password, cfg.InventoryID, cfg.ChunkSize, defaultTimeout); err != nil {
		return err
	}
	a.logf("새 설정 저장: %s (password_enc)", p)
	return a.selectUser(r.Username)
}

func (a *guiApp) setSaveDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" || !filepath.IsAbs(dir) {
		return errors.New("저장 폴더는 절대 경로여야 합니다 (예: D:\\받은파일)")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("폴더를 만들 수 없습니다: %v", err)
	}
	a.mu.Lock()
	a.settings.SaveDir = filepath.Clean(dir)
	s := a.settings
	a.mu.Unlock()
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(a.settingsPath, b, 0o644)
}

// ---------- 수신 ----------

var errNoUser = errors.New("사용자를 먼저 고르세요")

func newRecID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return time.Now().Format("20060102T150405.000") + "-" + hex.EncodeToString(b[:])
}

// startReceive 는 수신 한 건을 시작한다 (비동기).
func (a *guiApp) startReceive(auto bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return errNoUser
	}
	if a.recvStop != nil {
		return errors.New("이미 수신 중입니다")
	}
	ctx, cancel := context.WithCancel(a.rootCtx)
	obs := &fetchObs{}
	done := make(chan struct{})
	cfg := *a.cfg
	c, user, saveDir := a.client, a.user, a.settings.SaveDir
	a.recvStop, a.recvDone, a.obs, a.auto = cancel, done, obs, auto
	a.lastMsg, a.lastOK = "업로드를 기다립니다", true
	go func() {
		defer close(done)
		defer cancel()
		start := time.Now()
		a.logf("수신 시작 (%s, auto=%v)", user, auto)
		saved, err := fetchFile(withFetchObs(ctx, obs), c, &cfg, saveDir, a.log)
		a.finishReceive(user, start, obs.snapshot(), saved, err)
	}()
	return nil
}

func (a *guiApp) finishReceive(user string, start time.Time, s obsSnap, saved string, err error) {
	msg := "SHA-256 일치"
	ok := err == nil
	if err != nil {
		msg = oneLine(err.Error(), 300)
		if errors.Is(err, context.Canceled) {
			msg = "수신 중단 (창 닫힘 또는 [수신 중단]). 자기 세션이면 variables 원복"
		}
	}
	if s.Session != "" { // 세션을 받기 시작했을 때만 기록한다
		rec := recvRec{
			ID: newRecID(), TS: start.Format(recTSFormat), User: user, FileName: s.FileName, Size: s.Total,
			Chunks: s.N, SHAOK: ok, Status: map[bool]string{true: "정상", false: "실패"}[ok], Path: saved,
			Seconds: time.Since(start).Seconds(), Message: msg, Session: s.Session,
		}
		if !ok {
			rec.Path = "" // 해시 불일치 등 실패면 저장하지 않는다
		}
		if werr := a.store.append(rec); werr != nil {
			a.logf("기록 저장 실패: %v", werr)
		}
	}
	a.logf("수신 끝: ok=%v %s", ok, msg)
	a.mu.Lock()
	a.recvStop, a.recvDone = nil, nil
	a.lastOK = ok
	if ok {
		a.lastMsg = fmt.Sprintf("받음: %s (%s)", s.FileName, humanSize(s.Total))
	} else {
		a.lastMsg = "실패: " + msg
	}
	a.histRev++
	a.mu.Unlock()
}

func (a *guiApp) stopReceive() bool {
	a.mu.Lock()
	stop := a.recvStop
	a.mu.Unlock()
	if stop != nil {
		stop()
		return true
	}
	return false
}

// uploadStarting 은 variables 에 새 업로드(1번 조각)가 올라왔는지 GET 한 번으로 본다.
func uploadStarting(ctx context.Context, c *Client) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	v, err := c.GetVars(ctx)
	if err != nil {
		return false
	}
	b, ok := parseBlock(v)
	return ok && b["state"] == stateSending && b.int("chunk_index") == 1
}

func (a *guiApp) setWatch(on bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if on && a.client == nil {
		return errNoUser
	}
	if on == a.watch {
		return nil
	}
	a.watch = on
	if !on {
		if a.watchStop != nil {
			close(a.watchStop)
			a.watchStop = nil
		}
		return nil
	}
	stop := make(chan struct{})
	a.watchStop = stop
	go a.watchLoop(stop)
	return nil
}

func (a *guiApp) watchLoop(stop chan struct{}) {
	t := time.NewTicker(guiWatchEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-a.rootCtx.Done():
			return
		case <-t.C:
			a.mu.Lock()
			c, busy := a.client, a.recvStop != nil
			a.mu.Unlock()
			if c == nil || busy {
				continue
			}
			if uploadStarting(a.rootCtx, c) {
				if err := a.startReceive(true); err == nil {
					a.logf("자동 감시: 새 업로드 감지")
				}
			}
		}
	}
}

// ---------- HTTP ----------

type guiState struct {
	User      string  `json:"user"`
	URL       string  `json:"url"`
	Inventory int     `json:"inventory_id"`
	Receiving bool    `json:"receiving"`
	Auto      bool    `json:"auto"`
	Watch     bool    `json:"watch"`
	File      string  `json:"file"`
	K         int     `json:"k"`
	N         int     `json:"n"`
	Bytes     int64   `json:"bytes"`
	Total     int64   `json:"total"`
	Seconds   float64 `json:"seconds"`
	KiBps     float64 `json:"kib_per_sec"`
	Message   string  `json:"message"`
	OK        bool    `json:"ok"`
	HistRev   int64   `json:"hist_rev"`
	SaveDir   string  `json:"save_dir"`
	Focus     bool    `json:"focus"`
	Version   string  `json:"version"`
}

func (a *guiApp) state() guiState {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := guiState{User: a.user, Receiving: a.recvStop != nil, Auto: a.auto, Watch: a.watch, Message: a.lastMsg, OK: a.lastOK,
		HistRev: a.histRev, SaveDir: a.settings.SaveDir, Focus: a.focusReq, Version: version}
	a.focusReq = false
	if a.cfg != nil {
		s.URL, s.Inventory = a.cfg.URL, a.cfg.InventoryID
	}
	if s.Receiving && a.obs != nil {
		o := a.obs.snapshot()
		s.File, s.K, s.N, s.Bytes, s.Total = o.FileName, o.K, o.N, o.Bytes, o.Total
		if !o.Started.IsZero() {
			s.Seconds = time.Since(o.Started).Seconds()
			s.KiBps = float64(o.Bytes) / 1024 / maxf(s.Seconds, 0.001)
		}
	}
	return s
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]interface{}{"ok": false, "error": err.Error()})
}

func readJSON(r *http.Request, v interface{}) error {
	body := http.MaxBytesReader(nil, r.Body, 64<<10)
	defer body.Close()
	if err := json.NewDecoder(body).Decode(v); err != nil && err != io.EOF {
		return fmt.Errorf("요청 형식 오류: %v", err)
	}
	return nil
}

// guard 는 다른 사이트의 요청(CSRF, DNS rebinding)을 막는다.
//   - Host 는 127.0.0.1:{포트} 또는 localhost:{포트} 만
//   - Origin 이 있으면 같은 출처만
//   - 상태를 바꾸는 POST 는 X-AWX-Fetch 헤더 필수 (ping/bye 비콘 제외)
func (a *guiApp) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		okHost := host == fmt.Sprintf("127.0.0.1:%d", a.port) || host == fmt.Sprintf("localhost:%d", a.port)
		if a.port == 0 { // 테스트
			okHost = true
		}
		if !okHost {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && a.port != 0 && o != fmt.Sprintf("http://127.0.0.1:%d", a.port) && o != fmt.Sprintf("http://localhost:%d", a.port) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		beacon := r.URL.Path == "/api/ping" || r.URL.Path == "/api/bye"
		if r.Method == http.MethodPost && !beacon && r.Header.Get("X-AWX-Fetch") != "1" {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *guiApp) handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "webui")
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		static.ServeHTTP(w, r)
	})
	post := func(path string, h func(w http.ResponseWriter, r *http.Request)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			h(w, r)
		})
	}
	post("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		a.lc.ping(r.URL.Query().Get("id"), time.Now())
		w.WriteHeader(http.StatusNoContent)
	})
	post("/api/bye", func(w http.ResponseWriter, r *http.Request) {
		a.lc.bye(r.URL.Query().Get("id"), time.Now())
		if r.URL.Query().Get("force") == "1" && r.Header.Get("X-AWX-Fetch") == "1" {
			go a.requestExit("강제 종료 요청")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	post("/api/focus", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.focusReq = true
		a.mu.Unlock()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.state()) })
	mux.HandleFunc("/api/configs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"ok": true, "configs": a.listConfs(), "conf_dir": a.confDir})
	})
	post("/api/select", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			User string `json:"user"`
		}
		if err := readJSON(r, &req); err != nil {
			jsonErr(w, 400, err)
			return
		}
		if err := a.selectUser(req.User); err != nil {
			jsonErr(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]interface{}{"ok": true, "state": a.state()})
	})
	post("/api/configs/save", func(w http.ResponseWriter, r *http.Request) {
		var req newConfReq
		if err := readJSON(r, &req); err != nil {
			jsonErr(w, 400, err)
			return
		}
		if err := a.saveNewConf(req); err != nil {
			jsonErr(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	post("/api/fetch", func(w http.ResponseWriter, r *http.Request) {
		if err := a.startReceive(false); err != nil {
			jsonErr(w, 409, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	post("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]bool{"ok": true, "stopped": a.stopReceive()})
	})
	post("/api/watch", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			On bool `json:"on"`
		}
		if err := readJSON(r, &req); err != nil {
			jsonErr(w, 400, err)
			return
		}
		if err := a.setWatch(req.On); err != nil {
			jsonErr(w, 409, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 200
		}
		if limit > histMaxList {
			limit = histMaxList
		}
		rs, total, err := a.store.list(limit)
		if err != nil {
			jsonErr(w, 500, err)
			return
		}
		if rs == nil {
			rs = []recvRec{}
		}
		a.mu.Lock()
		rev := a.histRev
		a.mu.Unlock()
		writeJSON(w, 200, map[string]interface{}{"ok": true, "records": rs, "total": total, "rev": rev, "path": a.store.path})
	})
	post("/api/history/action", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if err := readJSON(r, &req); err != nil {
			jsonErr(w, 400, err)
			return
		}
		if err := a.historyAction(req.ID, req.Action); err != nil {
			jsonErr(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	post("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		var req guiSettings
		if err := readJSON(r, &req); err != nil {
			jsonErr(w, 400, err)
			return
		}
		if err := a.setSaveDir(req.SaveDir); err != nil {
			jsonErr(w, 400, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	post("/api/open-save-dir", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		d := a.settings.SaveDir
		a.mu.Unlock()
		_ = os.MkdirAll(d, 0o755)
		if err := openPath(d); err != nil {
			jsonErr(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	return a.guard(mux)
}

// historyAction 은 열기, 폴더 열기, 삭제(파일만, 기록은 "삭제됨")를 한다.
func (a *guiApp) historyAction(id, action string) error {
	rec, ok := a.store.get(id)
	if !ok {
		return errors.New("기록이 없습니다")
	}
	switch action {
	case "open":
		if rec.Deleted || rec.Path == "" {
			return errors.New("저장된 파일이 없습니다")
		}
		if _, err := os.Stat(rec.Path); err != nil {
			return errors.New("파일이 없습니다: " + rec.Path)
		}
		return openPath(rec.Path)
	case "folder":
		p := rec.Path
		if p == "" || rec.Deleted {
			a.mu.Lock()
			p = a.settings.SaveDir
			a.mu.Unlock()
		}
		return openFolderSelect(p)
	case "delete":
		if rec.Deleted {
			return errors.New("이미 삭제됨")
		}
		note := ""
		if rec.Path != "" {
			st, err := os.Lstat(rec.Path)
			switch {
			case err != nil:
				note = " (파일이 이미 없었음)"
			case !st.Mode().IsRegular():
				return errors.New("일반 파일이 아니라 지우지 않습니다")
			default:
				if err := os.Remove(rec.Path); err != nil {
					return fmt.Errorf("삭제 실패: %v", err)
				}
			}
		}
		err := a.store.update(id, func(r *recvRec) {
			r.Deleted = true
			r.Message = strings.TrimSpace(r.Message + " / 삭제됨 " + time.Now().Format("2006-01-02 15:04:05") + note)
		})
		a.mu.Lock()
		a.histRev++
		a.mu.Unlock()
		a.logf("삭제: %s%s", rec.Path, note)
		return err
	}
	return fmt.Errorf("알 수 없는 동작 %q", action)
}

// ---------- 실행 ----------

func portFilePath() string { return filepath.Join(os.TempDir(), "awx_fetch.port") }

// listenLocal 은 127.0.0.1:{기본 포트}, 안 되면 빈 포트에 바인딩한다.
func listenLocal(port int) (net.Listener, int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, 0, err
		}
	}
	return ln, ln.Addr().(*net.TCPAddr).Port, nil
}

func findBrowser() string {
	if b := os.Getenv("AWX_FETCH_BROWSER"); b != "" {
		if st, err := os.Stat(b); err == nil && !st.IsDir() {
			return b
		}
	}
	for _, p := range browserCandidates() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func browserArgs(url, profile string) []string {
	return []string{
		"--app=" + url, "--user-data-dir=" + profile, "--window-size=1100,820",
		"--no-first-run", "--no-default-browser-check", "--disable-extensions", "--disable-default-apps",
		"--disable-sync", "--disable-background-mode", "--disable-renderer-backgrounding",
		"--disable-background-timer-throttling", "--disable-features=Translate,msEdgeSidebarV2",
		"--proxy-server=direct://", "--proxy-bypass-list=<-loopback>",
	}
}

// launchBrowser 는 앱 창을 연다. 브라우저 프로세스는 곧 끝날 수 있다(Startup Boost). 수명은 ping/bye 로 본다.
func (a *guiApp) launchBrowser(url, profile string) error {
	b := findBrowser()
	if b == "" {
		a.logf("Edge/Chrome 을 찾지 못해 기본 브라우저로 엽니다")
		return openPath(url)
	}
	_ = os.MkdirAll(profile, 0o700)
	cmd := exec.Command(b, browserArgs(url, profile)...)
	if err := startHidden(cmd, false); err != nil {
		return err
	}
	a.logf("창 실행: %s (profile %s)", filepath.Base(b), profile)
	go func() { _ = cmd.Wait() }()
	return nil
}

// notifyExisting 은 이미 실행 중인 인스턴스의 창을 앞으로 가져온다.
func notifyExisting() {
	if b, err := os.ReadFile(portFilePath()); err == nil {
		if port, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			hc := &http.Client{Timeout: 2 * time.Second}
			req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/api/focus", port), nil)
			req.Header.Set("X-AWX-Fetch", "1")
			if resp, err := hc.Do(req); err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}
	}
	focusWindowByTitle(guiTitle)
}

// runGUI 는 창 앱을 실행한다. noBrowser 면 창을 띄우지 않고 서버만 돈다(시험용, 창이 없어도 종료하지 않음).
func runGUI(noBrowser bool) int {
	release, already, err := acquireSingleInstance(guiMutexName)
	if err == nil && already {
		notifyExisting()
		return 0
	}
	defer release()
	a := newGUIApp(exeDir())
	ln, port, err := listenLocal(guiDefaultPort)
	if err != nil {
		a.logf("서버 시작 실패: %v", err)
		return 1
	}
	a.port = port
	_ = os.WriteFile(portFilePath(), []byte(strconv.Itoa(port)), 0o644)
	defer os.Remove(portFilePath())
	srv := &http.Server{
		Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	a.logf("시작: awx_fetch %s, %s", version, url)
	profile := filepath.Join(os.TempDir(), "awx-copy-profile")
	var relaunch func()
	if noBrowser {
		a.lc.noClient = 0
	} else {
		if err := a.launchBrowser(url, profile); err != nil {
			a.logf("창 실행 실패: %v", err)
		}
		// 프로필 충돌(이전 창이 잠금을 잡고 있는 경우 등)로 창이 붙지 않으면 새 프로필로 한 번 더 연다.
		relaunch = func() {
			alt := fmt.Sprintf("%s-%d", profile, time.Now().Unix())
			a.logf("창이 연결되지 않아 새 프로필로 다시 엽니다: %s", alt)
			_ = a.launchBrowser(url, alt)
		}
	}
	go a.watchLifecycle(relaunch)
	<-a.exitCh
	a.logf("종료 시작: %s", a.exitWhy)
	a.shutdown(5 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = srv.Shutdown(ctx)
	cancel()
	a.logf("종료")
	return 0
}
