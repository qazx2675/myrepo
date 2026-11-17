package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 소켓 경로가 길어지지 않게 짧은 임시 폴더를 쓰고, daemonDir 를 그리로 바꾼다.
func withDaemonDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "pmd")
	if err != nil {
		t.Fatal(err)
	}
	old := daemonDir
	daemonDir = d
	t.Cleanup(func() {
		daemonDir = old
		os.RemoveAll(d)
	})
	return d
}

// withCwd 는 state 파일이 임시 폴더에 생기도록 작업 폴더를 옮긴다.
func withCwd(t *testing.T, dir string) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func waitUntil(t *testing.T, what string, d time.Duration, f func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("시간 초과: %s", what)
}

// 서버·클라이언트 왕복: 스냅샷(맵 키·벤더 묶음 포함)·갱신 신호·c 확인·조회 안내·중지 요청·끊김.
func TestDaemonProtocolRoundTrip(t *testing.T) {
	dir := withDaemonDir(t)
	f := newFake()
	f.pmDone["a"], f.pmInst["b"] = true, true
	col, err := NewCollector(testCfg(), []string{"a", "b", "c"}, filepath.Join(dir, "s.json"), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exp := time.Now().Add(time.Hour)
	srv := newDaemonServer(ctx, col, daemonInfo{PID: 7, Hostfile: "/x/hosts", Expires: exp, FP: "fp1"})
	sock := filepath.Join(dir, "t.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	go srv.serve(ln)

	in, err := queryInfo(sock)
	if err != nil || in.PID != 7 || in.FP != "fp1" || in.Total != 3 {
		t.Fatalf("info = %+v, %v", in, err)
	}
	rs, err := attachRemote(sock)
	if err != nil {
		t.Fatal(err)
	}
	rctx, rcancel := context.WithCancel(context.Background())
	defer rcancel()
	go rs.Run(rctx)
	if rs.Snapshot().Total != 3 || !rs.Expires().Equal(in.Expires) {
		t.Fatalf("첫 스냅샷/만료 시각 이상: %+v", rs.Snapshot())
	}

	// 회차가 돌면 갱신 신호가 오고 스냅샷이 같다(Counts 의 Status 키, Vendors 의 Hosts 맵)
	if err := col.Round(ctx); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "원격 스냅샷 갱신", 3*time.Second, func() bool {
		return rs.Snapshot().Counts[StatusDone] == 1 && rs.Snapshot().Round == 1
	})
	local, remote := col.Snapshot(), rs.Snapshot()
	if remote.Counts[StatusInstalling] != local.Counts[StatusInstalling] || len(remote.Vendors) != len(local.Vendors) {
		t.Fatalf("원격 스냅샷이 다름: %+v vs %+v", remote.Counts, local.Counts)
	}
	for i, g := range local.Vendors {
		for st, hs := range g.Hosts {
			if strings.Join(remote.Vendors[i].Hosts[st], ",") != strings.Join(hs, ",") {
				t.Fatalf("벤더 묶음이 다름: %v", g.Vendor)
			}
		}
	}
	select {
	case <-rs.Updates():
	case <-time.After(time.Second):
		t.Fatal("갱신 신호 없음")
	}

	// c 확인(완료 서버 ping)과 조회 안내
	f.pingDown["a"] = true
	if err := rs.CheckCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status(col, "a") != StatusOff {
		t.Fatalf("원격 c 확인이 반영 안 됨: %v", status(col, "a"))
	}
	if got := rs.ReportBusy(panelDHCP); !strings.Contains(got, "DHCP") {
		t.Fatalf("ReportBusy = %q", got)
	}
	if got := rs.Report('x'); !strings.Contains(got, "알 수 없는 조회") {
		t.Fatalf("알 수 없는 조회 = %q", got)
	}

	// 중지 요청 -> stopReq, 연결을 끊으면 화면 쪽 gone
	if err := rs.Stop(); err != nil || !rs.WasStopped() {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-srv.stopReq:
	case <-time.After(time.Second):
		t.Fatal("중지 요청이 서버에 안 옴")
	}
	srv.closeConns()
	select {
	case <-rs.Gone():
	case <-time.After(2 * time.Second):
		t.Fatal("연결이 끊겼는데 gone 이 안 닫힘")
	}
	ln.Close()
}

// 같은 호스트파일의 데몬은 하나만: 두 번째는 바로 끝난다. 12시간(여기서는 짧게)이 지나면 스스로 끝나고 소켓을 치운다.
func TestRunDaemonLockAndExpire(t *testing.T) {
	dir := withDaemonDir(t)
	withCwd(t, dir)
	oldMax, oldWait := maxRun, stopWait
	maxRun, stopWait = 1500*time.Millisecond, 500*time.Millisecond
	defer func() { maxRun, stopWait = oldMax, oldWait }()
	os.WriteFile("hosts.txt", []byte("127.0.0.1\n"), 0o644)
	cfg := testCfg()

	res := make(chan int, 1)
	go func() { res <- runDaemon(cfg, "hosts.txt", []string{"127.0.0.1"}) }()
	st, _ := stateAbs("hosts.txt")
	sock := sockPath(daemonID(st))
	waitUntil(t, "데몬 소켓", 3*time.Second, func() bool { _, err := queryInfo(sock); return err == nil })
	in, _ := queryInfo(sock)
	if got := in.Expires.Sub(in.Started); got != maxRun {
		t.Fatalf("만료까지 = %v, 기대 %v", got, maxRun)
	}
	if in.FP != fingerprint("hosts.txt", cfg.Path) {
		t.Fatal("지문이 다름")
	}

	// 같은 호스트파일 두 번째 데몬: 잠금 때문에 바로 0 으로 끝나고 첫 데몬은 멀쩡하다
	second := make(chan int, 1)
	go func() { second <- runDaemon(cfg, "hosts.txt", []string{"127.0.0.1"}) }()
	select {
	case c := <-second:
		if c != 0 {
			t.Fatalf("두 번째 데몬 종료 코드 %d", c)
		}
	case <-time.After(time.Second):
		t.Fatal("두 번째 데몬이 끝나지 않음")
	}
	if _, err := queryInfo(sock); err != nil {
		t.Fatal("두 번째 데몬 때문에 첫 데몬이 망가짐")
	}

	// 붙어 있는 화면은 만료 때 끊긴다
	rs, err := attachRemote(sock)
	if err != nil {
		t.Fatal(err)
	}
	go rs.Run(context.Background())
	select {
	case <-rs.Gone():
	case <-time.After(5 * time.Second):
		t.Fatal("만료됐는데 화면 연결이 안 끊김")
	}
	select {
	case c := <-res:
		if c != 0 {
			t.Fatalf("만료 종료 코드 %d", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("만료됐는데 데몬이 안 끝남")
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatal("끝난 데몬의 소켓이 남음")
	}
	if _, err := os.Stat("hosts.txt.pm_state.json"); err != nil {
		t.Fatal("상태 파일이 없음(기록 유지 실패)")
	}
}

// --stop 과 같은 중지 요청으로도 끝난다.
func TestRunDaemonStopRequest(t *testing.T) {
	dir := withDaemonDir(t)
	withCwd(t, dir)
	oldWait := stopWait
	stopWait = 500 * time.Millisecond
	defer func() { stopWait = oldWait }()
	res := make(chan int, 1)
	go func() { res <- runDaemon(testCfg(), "h.txt", []string{"127.0.0.1"}) }()
	st, _ := stateAbs("h.txt")
	sock := sockPath(daemonID(st))
	waitUntil(t, "데몬 소켓", 3*time.Second, func() bool { _, err := queryInfo(sock); return err == nil })
	if cmdStop("h.txt") != 0 {
		t.Fatal("cmdStop 실패")
	}
	select {
	case <-res:
	case <-time.After(5 * time.Second):
		t.Fatal("중지 요청 뒤에도 데몬이 안 끝남")
	}
	if cmdStop("h.txt") != 0 { // 이미 없어도 0
		t.Fatal("없는 수집을 stop 하면 0 이어야 함")
	}
}

func TestFingerprintChanges(t *testing.T) {
	dir := t.TempDir()
	h, c := filepath.Join(dir, "hosts"), filepath.Join(dir, "conf")
	os.WriteFile(h, []byte("a\nb\n"), 0o644)
	os.WriteFile(c, []byte("interval_sec=30\n"), 0o644)
	base := fingerprint(h, c)
	if base != fingerprint(h, c) {
		t.Fatal("같은 내용인데 지문이 다름")
	}
	os.WriteFile(h, []byte("a\nb\nc\n"), 0o644)
	if fingerprint(h, c) == base {
		t.Fatal("호스트파일이 바뀌었는데 지문이 같음")
	}
	os.WriteFile(h, []byte("a\nb\n"), 0o644)
	os.WriteFile(c, []byte("interval_sec=45\n"), 0o644)
	if fingerprint(h, c) == base {
		t.Fatal("설정이 바뀌었는데 지문이 같음")
	}
	if fingerprint(h, "") == base {
		t.Fatal("설정 파일 유무가 지문에 안 들어감")
	}
}

func TestDaemonIDPerStateFile(t *testing.T) {
	if daemonID("/a/hosts.pm_state.json") == daemonID("/b/hosts.pm_state.json") {
		t.Fatal("폴더가 다르면 데몬도 달라야 함")
	}
	if daemonID("/a/hosts.pm_state.json") != daemonID("/a/hosts.pm_state.json") {
		t.Fatal("같은 상태 파일은 같은 데몬이어야 함")
	}
}

// 호스트파일 두 번째 칸(yum)이 바뀌면 지문이 달라져 데몬이 재시작한다(계획서 4.8).
func TestFingerprintChangesOnYumMode(t *testing.T) {
	h := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(h, []byte("a\nb\n"), 0o644)
	base := fingerprint(h, "")
	os.WriteFile(h, []byte("a yum\nb\n"), 0o644)
	withYum := fingerprint(h, "")
	if withYum == base {
		t.Fatal("OS -> yum 으로 바뀌었는데 지문이 같음")
	}
	os.WriteFile(h, []byte("a\nb\n"), 0o644)
	if fingerprint(h, "") != base {
		t.Fatal("원래대로 돌리면 지문도 같아야 함")
	}
}
