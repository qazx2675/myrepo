// dupdaemon_test.go - 중복 데몬 방지 회귀 테스트: lock 교체 감지·저장 중단, 요청 1회 선점, /proc 스캔, 경고 표시
// (증상: 같은 디렉터리를 쓰는 데몬이 둘이라 진행률이 0%↔100% 로 번갈아 보이고, g 재확인이 "job 을 찾을 수 없습니다" 로 끝남)
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 데몬이 쥔 lock 이 지워졌다 다시 만들어지거나(inode 변경) pid 파일이 남의 것이면 사유를 돌려준다
func TestLockHeldCheck(t *testing.T) {
	setupDir(t)
	lock, ok, err := tryLock()
	if err != nil || !ok {
		t.Fatalf("tryLock: %v %v", ok, err)
	}
	defer lock.Close()
	if err := writePid(); err != nil {
		t.Fatal(err)
	}
	check := lockHeldCheck(lock)
	if why := check(); why != "" {
		t.Fatalf("정상인데 사유: %q", why)
	}

	// pid 파일만 지워짐 → 자기 것으로 복구하고 정상
	os.Remove(pidPath())
	if why := check(); why != "" {
		t.Fatalf("pid 파일 없음은 복구해야 함: %q", why)
	}
	if pid, _, err := readPid(); err != nil || pid != os.Getpid() {
		t.Fatalf("pid 파일 복구 안 됨: %d %v", pid, err)
	}

	// 다른 데몬이 pid 파일을 덮어씀
	if err := os.WriteFile(pidPath(), []byte(fmt.Sprintf("%d 1\n", os.Getpid()+1)), 0644); err != nil {
		t.Fatal(err)
	}
	if why := check(); !strings.Contains(why, "다른 데몬") {
		t.Fatalf("남의 pid 를 못 잡음: %q", why)
	}
	if err := writePid(); err != nil {
		t.Fatal(err)
	}

	// lock 파일 삭제 후 재생성 (cron ensure 가 두 번째 데몬을 띄울 수 있는 상태)
	os.Remove(lockPath())
	if why := check(); why == "" {
		t.Fatal("lock 파일 삭제를 못 잡음")
	}
	if err := os.WriteFile(lockPath(), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if why := check(); !strings.Contains(why, "inode") {
		t.Fatalf("lock inode 교체를 못 잡음: %q", why)
	}
}

// 두 번째 데몬(lock 교체 감지)은 queue 를 건드리지 않고 job 파일도 덮어쓰지 않고 loop 를 끝낸다
func TestFencedDaemonStopsWithoutSaving(t *testing.T) {
	setupDir(t)
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600)})
	id := submit(t, "u1", 0, "h1")
	d := newTestDaemon(w)
	bad := ""
	d.ownerCheck = func() string { return bad }
	d = drive(t, w, d, 0, 5)
	jf := jobFile(id)
	before, err := os.ReadFile(jf)
	if err != nil {
		t.Fatalf("정상일 때는 저장되어야 함: %v", err)
	}

	// 다른 데몬이 나타남: 메모리 상태를 바꿨어도(더럽힘) 디스크 파일은 그대로
	d.jobs[id].Hosts["h1"].Processed = "9999"
	d.dirty[id] = true
	bad = "lock 파일이 교체됨(inode 다름)"
	d.step(time.Unix(base+10, 0))
	if !d.lost {
		t.Fatal("lost 가 설정되지 않음")
	}
	if after, _ := os.ReadFile(jf); string(after) != string(before) {
		t.Fatalf("fence 된 데몬이 job 파일을 덮어씀:\n%s", after)
	}

	// 새 queue 파일도 수거하지 않음
	p := filepath.Join(queueDir(), fmt.Sprintf("%d_u1_2.job", base+11))
	if err := os.WriteFile(p, []byte(fmt.Sprintf("user=u1\ntime=%d\nh2\n", base+11)), 0644); err != nil {
		t.Fatal(err)
	}
	d.step(time.Unix(base+15, 0))
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fence 된 데몬이 queue 를 수거함: %v", err)
	}

	// loop 는 stop 없이도 바로 끝난다
	d2 := newTestDaemon(w)
	d2.ownerCheck = func() string { return "x" }
	done := make(chan struct{})
	go func() { d2.loop(make(chan struct{})); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fence 된 loop 가 끝나지 않음")
	}
}

// 같은 recheck 요청을 두 데몬이 모두 수거해도 한 번만 처리: 재확인 1회, rejected/ 기록 없음, requests/·claimed/ 비움
func TestRequestClaimedOnce(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600)})
	id := submit(t, "u1", 0, "h1")
	d1 := drive(t, w, newTestDaemon(w), 0, 10)
	d2 := newTestDaemon(w) // 같은 jobs 파일을 읽은 두 번째 데몬
	w.checks = nil
	if _, err := writeRequest(ReqRecheck, map[string]string{"jobid": id, "yml": allView}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(base+15, 0)
	d1.collectRequests(now)
	d2.collectRequests(now)
	if len(w.checks) != 1 {
		t.Fatalf("recheck 가 한 번만 실행되어야 함: %v", w.checks)
	}
	if ents, _ := os.ReadDir(reqRejectedDir()); len(ents) != 0 {
		t.Fatalf("rejected 기록이 생김: %v", ents)
	}
	if fs := listReqFiles(requestsDir()); len(fs) != 0 {
		t.Fatalf("요청 파일 남음: %v", fs)
	}
	if fs := listReqFiles(reqClaimedDir()); len(fs) != 0 {
		t.Fatalf("claimed 에 남음: %v", fs)
	}
}

// 다른 데몬이 먼저 선점한 요청은 조용히 건너뛴다: job 이 없는 요청이라도 "job 을 찾을 수 없습니다" 기록·rejected 를 만들지 않음
func TestRequestAlreadyClaimedIsSkipped(t *testing.T) {
	setupDir(t)
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600)})
	d := newTestDaemon(w)
	name := req(t, ReqRecheck, map[string]string{"jobid": "20200101-000000-nobody", "yml": allView})
	if _, ok := claimRequest(name); !ok { // 다른 데몬이 먼저 가져감
		t.Fatal("선점 실패")
	}
	if _, ok := claimRequest(name); ok {
		t.Fatal("같은 요청을 두 번 선점함")
	}
	d.collectRequests(time.Unix(base+15, 0))
	if ents, _ := os.ReadDir(reqRejectedDir()); len(ents) != 0 {
		t.Fatalf("rejected 기록이 생김: %v", ents)
	}
	if rc := readRecheck(dataDir(), "20200101-000000-nobody"); rc != nil {
		t.Fatalf("재확인 기록이 생김: %+v", rc)
	}
	if _, err := os.Stat(filepath.Join(reqClaimedDir(), name)); err != nil {
		t.Fatalf("다른 데몬이 처리 중인 claimed 파일이 사라짐: %v", err)
	}
}

// 선점 후 거부·수락 경로: 거부는 rejected/ 로, 수락한 manual-run 은 active/ 로 (claimed/ 는 비움)
func TestClaimedRejectAndAccept(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600), "h2": oldOS()})
	id := submitBody(t, 0, fmt.Sprintf(groupJobBody, base))
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 10)
	bad := req(t, ReqManualRun, map[string]string{"jobid": id, "yml": "없는.yml"})
	ok := req(t, ReqManualRun, map[string]string{"jobid": id, "yml": "a.yml"})
	d.collectRequests(time.Unix(base+15, 0))
	if r := rejectedReason(t, bad); r != "그룹 없음" {
		t.Fatalf("거부 사유 이상: %q", r)
	}
	if _, err := os.Stat(filepath.Join(reqActiveDir(), ok)); err != nil {
		t.Fatalf("수락한 manual-run 이 active 에 없음: %v", err)
	}
	if ents, _ := os.ReadDir(reqClaimedDir()); len(ents) != 0 {
		t.Fatalf("claimed 에 남음: %v", ents)
	}
}

func writeFakeProc(t *testing.T, root string, pid int, cmdline []string, env []string) {
	t.Helper()
	d := filepath.Join(root, fmt.Sprint(pid))
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	if cmdline != nil {
		os.WriteFile(filepath.Join(d, "cmdline"), []byte(strings.Join(cmdline, "\x00")+"\x00"), 0644)
	}
	if env != nil {
		os.WriteFile(filepath.Join(d, "environ"), []byte(strings.Join(env, "\x00")+"\x00"), 0644)
	}
}

// /proc 스캔: 같은 데이터 디렉터리의 auto_setup daemon 만, 자기 자신·다른 디렉터리·daemon 아닌 프로세스는 제외
func TestFindDaemonPids(t *testing.T) {
	root := t.TempDir()
	dir := "/data/as1"
	d := []string{"/usr/local/bin/auto_setup", "daemon"}
	writeFakeProc(t, root, 100, d, []string{"PATH=/bin", "AUTO_SETUP_DIR=" + dir})
	writeFakeProc(t, root, 101, d, []string{"AUTO_SETUP_DIR=/data/other"})                                           // 다른 디렉터리
	writeFakeProc(t, root, 102, d, []string{"PATH=/bin"})                                                            // 기본 디렉터리
	writeFakeProc(t, root, 103, d, []string{"AUTO_SETUP_DIR=" + dir})                                                // 자기 자신
	writeFakeProc(t, root, 104, []string{"auto_setup", "status"}, []string{"AUTO_SETUP_DIR=" + dir})                 // daemon 아님
	writeFakeProc(t, root, 105, []string{"/usr/bin/vim", "daemon"}, []string{"AUTO_SETUP_DIR=" + dir})               // 다른 프로그램
	writeFakeProc(t, root, 106, d, nil)                                                                              // environ 읽기 불가
	writeFakeProc(t, root, 107, []string{"/root/auto_setup_os6", "daemon"}, []string{"AUTO_SETUP_DIR=" + dir + "/"}) // 바이너리 이름 변형, 끝 슬래시
	writeFakeProc(t, root, 108, []string{"auto_setup"}, []string{"AUTO_SETUP_DIR=" + dir})                           // 인자 없음
	if err := os.MkdirAll(filepath.Join(root, "self"), 0755); err != nil {                                           // 숫자 아닌 항목
		t.Fatal(err)
	}
	if got := findDaemonPids(root, dir, 103); !reflect.DeepEqual(got, []int{100, 107}) {
		t.Fatalf("같은 디렉터리 데몬: %v", got)
	}
	if got := findDaemonPids(root, defaultDataDir, 103); !reflect.DeepEqual(got, []int{102}) {
		t.Fatalf("기본 디렉터리 데몬: %v", got)
	}
	if got := findDaemonPids(filepath.Join(root, "없음"), dir, 1); got != nil {
		t.Fatalf("/proc 없으면 nil: %v", got)
	}
}

// 시작 시 정리: 같은 디렉터리 데몬에만 SIGTERM (가짜 /proc 이 실제 자식 프로세스를 가리킴)
func TestStopStrayDaemons(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skip("sleep 실행 불가:", err)
	}
	defer cmd.Process.Kill()
	other := exec.Command("sleep", "60")
	if err := other.Start(); err != nil {
		t.Skip("sleep 실행 불가:", err)
	}
	defer other.Process.Kill()
	setupDir(t)
	root := t.TempDir()
	d := []string{"auto_setup", "daemon"}
	writeFakeProc(t, root, cmd.Process.Pid, d, []string{"AUTO_SETUP_DIR=" + dataDir()})
	writeFakeProc(t, root, other.Process.Pid, d, []string{"AUTO_SETUP_DIR=/data/other"})
	sent := stopStrayDaemons(root, dataDir(), os.Getpid())
	if !reflect.DeepEqual(sent, []int{cmd.Process.Pid}) {
		t.Fatalf("SIGTERM 대상: %v", sent)
	}
	errc := make(chan error, 1)
	go func() { errc <- cmd.Wait() }()
	select {
	case err := <-errc:
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
			t.Fatalf("SIGTERM 으로 종료되지 않음: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("대상 프로세스가 종료되지 않음")
	}
	if err := syscall.Kill(other.Process.Pid, 0); err != nil {
		t.Fatalf("다른 디렉터리 프로세스까지 종료됨: %v", err)
	}
	if lg, _ := os.ReadFile(logPath()); !strings.Contains(string(lg), fmt.Sprintf("pid=%d", cmd.Process.Pid)) {
		t.Fatalf("로그에 pid 없음:\n%s", lg)
	}
}

// 중복 데몬 경고는 프로세스가 2개 이상일 때만 (0·1 은 기존 화면 그대로)
func TestRenderDuplicateDaemonWarning(t *testing.T) {
	s := sampleNormal()
	text := func(procs int) string {
		s.Daemon.Procs = procs
		return strings.Join(titleLines(s, 120, false), "\n")
	}
	for _, n := range []int{0, 1} {
		if strings.Contains(text(n), "auto_setup --restart") {
			t.Fatalf("procs=%d 인데 경고가 보임", n)
		}
	}
	if !strings.Contains(text(2), "데몬 2개 실행 중 — auto_setup --restart 필요") {
		t.Fatalf("procs=2 경고 없음:\n%s", text(2))
	}
	s.Daemon.Procs = 3
	if red := strings.Join(titleLines(s, 120, true), "\n"); !strings.Contains(red, "\x1b[1;31m데몬 3개 실행 중") {
		t.Fatalf("경고는 빨강이어야 함: %q", red)
	}
	s.Daemon.Procs = 2
	if narrow := strings.Join(titleLines(s, 50, false), "\n"); !strings.Contains(narrow, "--restart") {
		t.Fatalf("좁은 폭에서 경고가 사라짐:\n%s", narrow)
	}
}
