// daemon.go - 수집 데몬(백그라운드)과 화면(클라이언트) 사이의 통신, 데몬 시작·중지·상태.
// 수집(Collector)은 데몬 프로세스가 돌리고, TUI 는 유닉스 소켓으로 붙어 스냅샷을 받는다.
// 화면을 닫아도 수집은 계속되고, 같은 호스트파일로 다시 실행하면 이어서 붙는다. 데몬은 시작 12시간 뒤 스스로 끝난다.
// 통신은 JSON 한 줄씩이다: 요청 하나당 연결 하나(watch 만 스냅샷을 계속 받는다).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// 조정 가능한 값(테스트에서 바꾼다)
var (
	daemonDir = "/run/install_mn" // 소켓·잠금·로그 폴더(root 전용, 재부팅하면 비워진다)
	maxRun    = 12 * time.Hour    // 데몬 최대 실행 시간(시작 시각 기준 고정)
	stopWait  = 5 * time.Second   // 종료 신호 뒤 회차가 끝나길 기다리는 시간
)

const (
	maxRunEnv   = "PM_MAX_RUN_SEC" // 시험용: 설정하면 maxRun 대신 이 초를 쓴다
	opWatch     = "watch"
	opInfo      = "info"
	opCheck     = "check"
	opReport    = "report"
	opBusy      = "busy"
	opStop      = "stop"
	rpcShort    = 3 * time.Second
	startupWait = 10 * time.Second
)

// ---- 프로토콜 ----

type request struct {
	Op   string `json:"op"`
	Kind string `json:"kind,omitempty"` // report·busy: d 또는 l
}

type daemonInfo struct {
	PID      int       `json:"pid"`
	Hostfile string    `json:"hostfile"`
	Started  time.Time `json:"started"`
	Expires  time.Time `json:"expires"`
	FP       string    `json:"fp"`    // 호스트파일·설정 파일 지문. 바뀌면 다시 시작한다
	Total    int       `json:"total"` // OS 서버 수(yum 서버는 YumTotal 에 따로)
	Done     int       `json:"done"`  // OS 설치완료 수
	Round    int       `json:"round"`
	// yum update 모드 서버(계획서 4.8). yum 서버가 없으면 json 에 나오지 않아 v0.3.0 과 같다.
	YumTotal int `json:"yum_total,omitempty"`
	YumDone  int `json:"yum_done,omitempty"` // yum_update/접속가능(커널 변경 완료) 수
}

type response struct {
	Err  string      `json:"err,omitempty"`
	Text string      `json:"text,omitempty"`
	Info *daemonInfo `json:"info,omitempty"`
	Snap *Snapshot   `json:"snap,omitempty"`
}

// ---- 경로·지문 ----

// daemonID 는 상태 파일의 절대 경로로 데몬을 구분한다(같은 폴더·같은 호스트파일 이름이면 같은 데몬).
func daemonID(stateAbs string) string {
	h := sha256.Sum256([]byte(stateAbs))
	return hex.EncodeToString(h[:6])
}

func sockPath(id string) string { return filepath.Join(daemonDir, id+".sock") }
func lockPath(id string) string { return filepath.Join(daemonDir, id+".lock") }
func logPath(id string) string  { return filepath.Join(daemonDir, id+".log") }

// stateAbs 는 호스트파일에 대응하는 state 파일의 절대 경로다.
func stateAbs(hostfile string) (string, error) {
	return filepath.Abs(stateFileName(hostfile))
}

// programVersion 은 프로그램 버전이다. 지문에 들어가므로 바이너리를 교체한 뒤 다시 실행하면
// 옛 데몬을 멈추고 새로 시작한다(스냅샷 필드가 늘어도 화면과 데몬이 어긋나지 않게). 테스트가 바꿀 수 있게 변수로 둔다.
var programVersion = "0.5.0"

// fingerprint 는 프로그램 버전과 호스트파일·설정 파일 내용, oklist(절대 경로·내용)의 지문이다. 하나라도 바뀌면 달라진다.
// oklist 가 없으면("") oklist 부분은 넣지 않는다.
func fingerprint(hostfile, conf, oklist string) string {
	h := sha256.New()
	h.Write([]byte(programVersion))
	h.Write([]byte{0})
	for _, p := range []string{hostfile, conf} {
		if p != "" {
			if b, err := os.ReadFile(p); err == nil {
				h.Write(b)
			}
		}
		h.Write([]byte{0})
	}
	if oklist != "" {
		if abs, err := filepath.Abs(oklist); err == nil {
			oklist = abs
		}
		h.Write([]byte("oklist\x00" + oklist + "\x00"))
		if b, err := os.ReadFile(oklist); err == nil {
			s := sha256.Sum256(b)
			h.Write(s[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// ---- 서버(데몬 쪽) ----

type daemonServer struct {
	col     *Collector
	ctx     context.Context
	info    daemonInfo
	stopReq chan struct{}
	stopOne sync.Once

	mu       sync.Mutex
	watchers map[chan struct{}]struct{}
	conns    map[net.Conn]struct{}
	repMu    map[string]*sync.Mutex // 조회(d·l)는 종류마다 하나씩만 돈다(관리서버 부하)
}

func newDaemonServer(ctx context.Context, col *Collector, info daemonInfo) *daemonServer {
	return &daemonServer{
		col: col, ctx: ctx, info: info, stopReq: make(chan struct{}),
		watchers: map[chan struct{}]struct{}{}, conns: map[net.Conn]struct{}{},
		repMu: map[string]*sync.Mutex{"d": {}, "l": {}},
	}
}

func (s *daemonServer) requestStop() { s.stopOne.Do(func() { close(s.stopReq) }) }

// serve 는 연결을 받아 처리한다. ln 이 닫히면 돌아온다.
func (s *daemonServer) serve(ln net.Listener) {
	go s.fanOut()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		go s.handle(c)
	}
}

// fanOut 은 수집기의 갱신 신호를 붙어 있는 화면 모두에게 알린다.
func (s *daemonServer) fanOut() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.col.Updates():
			s.mu.Lock()
			for ch := range s.watchers {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
			s.mu.Unlock()
		}
	}
}

// closeConns 는 붙어 있는 연결을 모두 끊는다(화면은 끊김을 보고 닫힌다).
func (s *daemonServer) closeConns() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.Close()
	}
}

func (s *daemonServer) liveInfo() *daemonInfo {
	in := s.info
	if sn := s.col.Snapshot(); sn != nil {
		in.Total, in.Done, in.Round = sn.Total-sn.Yum.Total, sn.Done(), sn.Round
		in.YumTotal, in.YumDone = sn.Yum.Total, sn.Yum.Done
	}
	return &in
}

func (s *daemonServer) handle(c net.Conn) {
	defer func() {
		c.Close()
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
	}()
	c.SetReadDeadline(time.Now().Add(rpcShort))
	var req request
	if err := json.NewDecoder(c).Decode(&req); err != nil {
		return
	}
	c.SetReadDeadline(time.Time{})
	enc := json.NewEncoder(c)
	reply := func(r response) {
		c.SetWriteDeadline(time.Now().Add(15 * time.Second))
		enc.Encode(r)
	}
	switch req.Op {
	case opInfo:
		reply(response{Info: s.liveInfo()})
	case opWatch:
		s.watch(c, reply)
	case opCheck:
		if err := s.col.CheckCompleted(s.ctx); err != nil {
			reply(response{Err: err.Error()})
			return
		}
		reply(response{})
	case opReport:
		m := s.repMu[req.Kind]
		if m == nil {
			reply(response{Err: "알 수 없는 조회: " + req.Kind})
			return
		}
		m.Lock()
		text := s.col.Report([]rune(req.Kind)[0])
		m.Unlock()
		reply(response{Text: text})
	case opBusy:
		if k := []rune(req.Kind); len(k) == 1 {
			reply(response{Text: s.col.ReportBusy(k[0])})
			return
		}
		reply(response{})
	case opStop:
		reply(response{})
		s.requestStop()
	default:
		reply(response{Err: "알 수 없는 요청: " + req.Op})
	}
}

// watch 는 연결된 동안 스냅샷을 보낸다: 처음 한 번, 그다음은 갱신될 때마다(밀리면 합친다).
func (s *daemonServer) watch(c net.Conn, reply func(response)) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.watchers[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.watchers, ch)
		s.mu.Unlock()
	}()
	gone := make(chan struct{})
	go func() { // 화면이 닫히면 읽기가 끝난다
		io.Copy(io.Discard, c)
		close(gone)
	}()
	reply(response{Snap: s.col.Snapshot()})
	for {
		select {
		case <-ch:
			reply(response{Snap: s.col.Snapshot()})
		case <-gone:
			return
		case <-s.ctx.Done():
			return
		}
	}
}

// runDaemon 은 `--daemon` 으로 시작된 프로세스의 본체다. 시작 12시간 뒤, 중지 요청, 종료 신호 중 먼저 오는 때 끝난다.
func runDaemon(cfg *Config, hostfile, oklist string, hosts []string, opts ...CollectorOpt) int {
	st, err := stateAbs(hostfile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	id := daemonID(st)
	if err := os.MkdirAll(daemonDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "데몬 폴더를 만들 수 없음:", err)
		return 1
	}
	release, err := lockExclusive(lockPath(id))
	if err != nil {
		return 0 // 같은 호스트파일의 데몬이 이미 있다
	}
	defer release()
	if oklist != "" {
		if ok, err := loadOklist(oklist); err == nil {
			fmt.Fprintf(os.Stderr, "oklist %s: 서버 %d대, 경고(무시한 줄) %d줄\n", oklist, ok.Count, ok.Warn)
		}
	}
	opts = append(opts, WithResFile(resFileName(hostfile), func(msg string) { fmt.Fprintln(os.Stderr, msg) }))
	col, err := NewCollector(cfg, hosts, stateFileName(hostfile), defaultDeps(), opts...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	limit := maxRun
	if v, err := strconv.Atoi(os.Getenv(maxRunEnv)); err == nil && v > 0 {
		limit = time.Duration(v) * time.Second
	}
	sp := sockPath(id)
	os.Remove(sp) // 잠금을 쥐고 있으니 남은 소켓은 죽은 데몬의 것이다
	ln, err := net.Listen("unix", sp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "소켓을 열 수 없음:", err)
		return 1
	}
	os.Chmod(sp, 0o600)
	defer os.Remove(sp)

	hostAbs, _ := filepath.Abs(hostfile)
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	srv := newDaemonServer(ctx, col, daemonInfo{
		PID: os.Getpid(), Hostfile: hostAbs, Started: now, Expires: now.Add(limit),
		FP: fingerprint(hostfile, cfg.Path, oklist),
	})
	go srv.serve(ln)
	runDone := make(chan struct{})
	go func() {
		col.Run(ctx)
		close(runDone)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	select {
	case <-ctx.Done(): // 12시간
	case <-srv.stopReq:
	case <-sig:
	}
	cancel()
	ln.Close()
	srv.closeConns()
	select { // 진행 중인 회차가 마무리되길 잠깐 기다린다
	case <-runDone:
	case <-time.After(stopWait):
	}
	return 0
}

// ---- 클라이언트 ----

// rpc 는 요청 하나를 보내고 응답 하나를 받는다. timeout 이 0 이면 기다리는 시간 제한이 없다(ctx 만 적용).
func rpc(ctx context.Context, sock string, req request, timeout time.Duration) (response, error) {
	var d net.Dialer
	dctx, dcancel := context.WithTimeout(ctx, rpcShort)
	defer dcancel()
	c, err := d.DialContext(dctx, "unix", sock)
	if err != nil {
		return response{}, err
	}
	defer c.Close()
	done := make(chan struct{})
	defer close(done)
	go func() { // ctx 가 끝나면 연결을 닫아 읽기를 깨운다
		select {
		case <-ctx.Done():
			c.Close()
		case <-done:
		}
	}()
	if timeout > 0 {
		c.SetDeadline(time.Now().Add(timeout))
	}
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return response{}, err
	}
	var resp response
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		return response{}, err
	}
	return resp, nil
}

// queryInfo 는 데몬 정보를 가져온다. 데몬이 없으면 error 다.
func queryInfo(sock string) (*daemonInfo, error) {
	resp, err := rpc(context.Background(), sock, request{Op: opInfo}, rpcShort)
	if err != nil {
		return nil, err
	}
	if resp.Info == nil {
		return nil, errors.New("응답 형식 오류")
	}
	return resp.Info, nil
}

// waitGone 은 데몬이 사라질 때까지 기다린다.
func waitGone(sock string, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if _, err := queryInfo(sock); err != nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// startDaemon 은 자기 자신을 `--daemon` 으로 다시 실행해 터미널과 분리한다.
func startDaemon(conf, hostfile, oklist string, id string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(daemonDir, 0o700); err != nil {
		return err
	}
	args := []string{"--daemon"}
	if conf != "" {
		if abs, err := filepath.Abs(conf); err == nil {
			conf = abs
		}
		args = append(args, "-c", conf)
	}
	args = append(args, hostfile)
	if oklist != "" {
		if abs, err := filepath.Abs(oklist); err == nil {
			oklist = abs
		}
		args = append(args, oklist)
	}
	lf, err := os.OpenFile(logPath(id), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer lf.Close()
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// ensureDaemon 은 수집 데몬이 돌고 있게 한다. 이미 있으면 그대로 두고, 호스트파일·설정이 바뀌었으면 다시 시작한다.
// 다시 시작했으면 notice 에 안내 문구를 돌려준다.
func ensureDaemon(conf, hostfile, oklist string, cfg *Config) (sock, notice string, err error) {
	hostAbs, err := filepath.Abs(hostfile)
	if err != nil {
		return "", "", err
	}
	st, err := stateAbs(hostfile)
	if err != nil {
		return "", "", err
	}
	id := daemonID(st)
	sock = sockPath(id)
	fp := fingerprint(hostfile, cfg.Path, oklist)
	if info, qerr := queryInfo(sock); qerr == nil {
		if info.FP == fp {
			return sock, "", nil
		}
		if _, err := rpc(context.Background(), sock, request{Op: opStop}, rpcShort); err != nil {
			return "", "", fmt.Errorf("실행 중인 수집을 멈추지 못함: %v", err)
		}
		if !waitGone(sock, stopWait+10*time.Second) {
			return "", "", errors.New("실행 중인 수집이 멈추지 않음")
		}
		notice = "호스트파일·설정·oklist 가 바뀌어 수집을 다시 시작했습니다(자동 종료 12시간도 새로 셈)"
	}
	if err := startDaemon(conf, hostAbs, oklist, id); err != nil {
		return "", "", fmt.Errorf("수집을 시작하지 못함: %v", err)
	}
	end := time.Now().Add(startupWait)
	for time.Now().Before(end) {
		if _, err := queryInfo(sock); err == nil {
			return sock, notice, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	msg := "수집이 시작되지 않음"
	if b, err := os.ReadFile(logPath(id)); err == nil && len(b) > 0 {
		msg += ": " + string(b)
	}
	return "", "", errors.New(msg)
}

// ---- 화면 쪽 source ----

// remoteSource 는 소켓으로 데몬에 붙는 source 다(TUI 는 Collector 와 똑같이 쓴다).
type remoteSource struct {
	sock    string
	conn    net.Conn
	dec     *json.Decoder
	info    daemonInfo
	snap    atomic.Pointer[Snapshot]
	notify  chan struct{}
	gone    chan struct{} // 데몬 쪽에서 연결이 끊기면 닫힌다
	goneOne sync.Once
	stopped atomic.Bool // 화면에서 수집 종료를 요청했다
}

// attachRemote 는 데몬에 붙고 첫 스냅샷을 받는다.
func attachRemote(sock string) (*remoteSource, error) {
	info, err := queryInfo(sock)
	if err != nil {
		return nil, err
	}
	c, err := net.DialTimeout("unix", sock, rpcShort)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(c).Encode(request{Op: opWatch}); err != nil {
		c.Close()
		return nil, err
	}
	r := &remoteSource{sock: sock, conn: c, dec: json.NewDecoder(c), info: *info,
		notify: make(chan struct{}, 1), gone: make(chan struct{})}
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var first response
	if err := r.dec.Decode(&first); err != nil || first.Snap == nil {
		c.Close()
		return nil, fmt.Errorf("첫 스냅샷을 받지 못함: %v", err)
	}
	c.SetReadDeadline(time.Time{})
	r.snap.Store(first.Snap)
	return r, nil
}

func (r *remoteSource) Snapshot() *Snapshot      { return r.snap.Load() }
func (r *remoteSource) Updates() <-chan struct{} { return r.notify }
func (r *remoteSource) Gone() <-chan struct{}    { return r.gone }
func (r *remoteSource) Expires() time.Time       { return r.info.Expires }
func (r *remoteSource) WasStopped() bool         { return r.stopped.Load() }
func (r *remoteSource) markGone()                { r.goneOne.Do(func() { close(r.gone) }) }

func (r *remoteSource) isGone() bool {
	select {
	case <-r.gone:
		return true
	default:
		return false
	}
}

// Run 은 ctx 가 끝나거나 연결이 끊길 때까지 스냅샷을 받는다.
func (r *remoteSource) Run(ctx context.Context) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			r.conn.Close()
		case <-done:
		}
	}()
	for {
		var resp response
		if err := r.dec.Decode(&resp); err != nil {
			break
		}
		if resp.Snap != nil {
			r.snap.Store(resp.Snap)
			select {
			case r.notify <- struct{}{}:
			default:
			}
		}
	}
	r.conn.Close()
	if ctx.Err() == nil {
		r.markGone()
	}
}

func (r *remoteSource) CheckCompleted(ctx context.Context) error {
	resp, err := rpc(ctx, r.sock, request{Op: opCheck}, 0)
	if err != nil {
		return err
	}
	if resp.Err != "" {
		return errors.New(resp.Err)
	}
	return nil
}

func (r *remoteSource) Report(kind rune) string {
	resp, err := rpc(context.Background(), r.sock, request{Op: opReport, Kind: string(kind)}, 0)
	if err != nil {
		return "조회 실패: 수집 프로세스와 연결할 수 없음 (" + err.Error() + ")"
	}
	if resp.Err != "" {
		return "조회 실패: " + resp.Err
	}
	return resp.Text
}

func (r *remoteSource) ReportBusy(kind rune) string {
	resp, err := rpc(context.Background(), r.sock, request{Op: opBusy, Kind: string(kind)}, rpcShort)
	if err != nil || resp.Text == "" {
		return "조회중..."
	}
	return resp.Text
}

// Stop 은 수집 데몬을 끝내 달라고 요청한다.
func (r *remoteSource) Stop() error {
	r.stopped.Store(true)
	_, err := rpc(context.Background(), r.sock, request{Op: opStop}, rpcShort)
	return err
}

// ---- 명령: --status, --stop ----

// listDaemons 는 실행 중인 데몬 정보를 모은다(호스트파일 이름 순).
func listDaemons() []daemonInfo {
	files, _ := filepath.Glob(filepath.Join(daemonDir, "*.sock"))
	var out []daemonInfo
	for _, f := range files {
		if in, err := queryInfo(f); err == nil {
			out = append(out, *in)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostfile < out[j].Hostfile })
	return out
}

func describeDaemon(in daemonInfo, now time.Time) string {
	left := in.Expires.Sub(now)
	if left < 0 {
		left = 0
	}
	yum := ""
	if in.YumTotal > 0 { // OS 설치완료 수와 섞이지 않게 yum 서버는 따로 센다(없으면 v0.3.0 출력 그대로)
		yum = fmt.Sprintf(" · yum %d/%d", in.YumDone, in.YumTotal)
	}
	return fmt.Sprintf("%s\n  pid %d · 서버 %d대 중 설치완료 %d%s · 회차 %d\n  시작 %s · 자동 종료 %s (남은 %s)",
		in.Hostfile, in.PID, in.Total, in.Done, yum, in.Round,
		in.Started.Format("01-02 15:04"), in.Expires.Format("01-02 15:04"), fmtDur(left))
}

// cmdStatus 는 hostfile 이 비어 있으면 실행 중인 수집 전부를, 아니면 그 호스트파일의 수집을 보여 준다.
func cmdStatus(hostfile string) int {
	var list []daemonInfo
	if hostfile == "" {
		list = listDaemons()
	} else {
		st, err := stateAbs(hostfile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if in, err := queryInfo(sockPath(daemonID(st))); err == nil {
			list = append(list, *in)
		}
	}
	if len(list) == 0 {
		fmt.Println("실행 중인 수집이 없습니다.")
		return 0
	}
	for _, in := range list {
		fmt.Println(describeDaemon(in, time.Now()))
	}
	return 0
}

// cmdStop 은 호스트파일의 수집을 끝낸다. 실행 중이 아니면 안내만 하고 0 으로 끝난다.
func cmdStop(hostfile string) int {
	st, err := stateAbs(hostfile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sock := sockPath(daemonID(st))
	if _, err := queryInfo(sock); err != nil {
		fmt.Println("실행 중인 수집이 없습니다.")
		return 0
	}
	if _, err := rpc(context.Background(), sock, request{Op: opStop}, rpcShort); err != nil {
		fmt.Fprintln(os.Stderr, "수집 종료 요청 실패:", err)
		return 1
	}
	if !waitGone(sock, stopWait+10*time.Second) {
		fmt.Fprintln(os.Stderr, "수집이 아직 끝나지 않았습니다. 잠시 뒤 install_mn --status 로 확인하세요.")
		return 1
	}
	fmt.Println("수집을 종료했습니다. (상태 기록은 유지됩니다)")
	return 0
}
