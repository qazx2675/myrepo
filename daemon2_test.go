// daemon2_test.go - 2차 단위 테스트: stage 전이·정체 60분 경계, LDAP 훅 호출 시점, 2차 체크 선별·병합·stage, 재기동 시 transient 복원, pid/stopDaemon
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// xworld: world + ssh 무응답 호스트, 가짜 LdapBackup/Second, 호출 순서 기록
type xworld struct {
	*world
	mu         sync.Mutex
	noSSH      map[string]bool
	events     []string
	secondFn   func(targets []string) SecondResult
	secondGate chan struct{}
	applyDisk  map[string]string // Apply 시점에 디스크 job 파일의 stage
}

func newX(hosts map[string]*simHost) *xworld {
	return &xworld{world: newWorld(hosts), noSSH: map[string]bool{}, applyDisk: map[string]string{}}
}

func (x *xworld) ev(f string, a ...interface{}) {
	x.mu.Lock()
	x.events = append(x.events, fmt.Sprintf(f, a...))
	x.mu.Unlock()
}

func (x *xworld) Events() []string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]string(nil), x.events...)
}

func (x *xworld) Check(route string, hosts []string) (map[string]CheckResult, error) {
	res, err := x.world.Check(route, hosts)
	for h := range res {
		if x.noSSH[h] {
			res[h] = CheckResult{}
		}
	}
	return res, err
}

func (x *xworld) Run(j *Job, hosts []string, hasOS6 bool) (RunResult, error) {
	x.ev("run %s@%d", strings.Join(hosts, ","), x.off())
	return x.world.Run(j, hosts, hasOS6)
}

// Second
func (x *xworld) Run2(j *Job, first RunResult, targets []string) (SecondResult, error) {
	x.ev("second %s@%d code=%s", strings.Join(targets, ","), x.off(), first.Code)
	if x.secondGate != nil {
		<-x.secondGate
	}
	if x.secondFn != nil {
		return x.secondFn(targets), nil
	}
	return SecondResult{Processed: targets}, nil
}

type fakeSecond struct{ x *xworld }

func (f fakeSecond) Run(j *Job, first RunResult, targets []string) (SecondResult, error) {
	return f.x.Run2(j, first, targets)
}

type fakeLdap struct{ x *xworld }

func (f fakeLdap) Backup(j *Job, hosts []string) map[string]LdapState {
	f.x.ev("backup %s@%d", strings.Join(hosts, ","), f.x.off())
	out := map[string]LdapState{}
	for _, h := range hosts {
		out[h] = LdapState{Backup: LdapBackupOK}
	}
	return out
}

func (f fakeLdap) Apply(j *Job, host string) LdapState {
	f.x.ev("apply %s@%d", host, f.x.off())
	if dj, err := loadJobFile(jobFile(j.ID)); err == nil {
		f.x.mu.Lock()
		f.x.applyDisk[host] = dj.Hosts[host].Stage
		f.x.mu.Unlock()
	}
	return LdapState{Bindpw: BindpwDiff, Applied: true}
}

func newXDaemon(x *xworld, ldap bool, second bool) *Daemon {
	d := &Daemon{Clock: x.clock, Pinger: x.world, Checker: x, Runner: x, Notifier: x.world, Lookup: x.world.lookup}
	if ldap {
		d.Ldap = fakeLdap{x}
	}
	if second {
		d.Second = fakeSecond{x}
	}
	d.onStart()
	return d
}

func hostStage(t *testing.T, d *Daemon, id, h string) Stage {
	t.Helper()
	if j := d.jobs[id]; j != nil {
		return Stage(j.Hosts[h].Stage)
	}
	return Stage(doneJob(t, id).Hosts[h].Stage)
}

func withOS6(t *testing.T, mgmt, osCheck string) {
	t.Helper()
	a, b := os6_mgmt, os6_os_check_sh
	os6_mgmt, os6_os_check_sh = mgmt, osCheck
	t.Cleanup(func() { os6_mgmt, os6_os_check_sh = a, b })
}

// stage 전이 전체 + LDAP: 백업은 전달 직후 ping O 호스트만 1회, 적용은 READY 직후·run 전(디스크 stage=ldap)
func TestStageTransitionsAndLdap(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600), "h2": install(0, 125, 400, 600)})
	id := submit(t, "u1", 0, "h1", "h2")
	d := newXDaemon(x, true, false)
	var seq []Stage
	for o := int64(0); o <= 700; o += 5 {
		d = drive(t, x.world, d, o, o)
		if s := hostStage(t, d, id, "h1"); len(seq) == 0 || seq[len(seq)-1] != s {
			seq = append(seq, s)
		}
	}
	want := []Stage{StageQueued, StageDeploying, StageBooting, StageInstalling, StageChecking, StageDone}
	if !reflect.DeepEqual(seq, want) {
		t.Fatalf("h1 stage 전이 불일치\n got: %v\nwant: %v", seq, want)
	}
	ev := x.Events()
	wantEv := []string{"backup h1@0", "apply h1@600", "run h1,h2@600"}
	if !reflect.DeepEqual(ev, wantEv) {
		t.Fatalf("LDAP 호출 순서 불일치\n got: %v\nwant: %v", ev, wantEv)
	}
	if x.applyDisk["h1"] != string(StageLdap) {
		t.Fatalf("Apply 시점 디스크 stage = %q, ldap 이어야 함", x.applyDisk["h1"])
	}
	j := doneJob(t, id)
	h1, h2 := j.Hosts["h1"], j.Hosts["h2"]
	if !reflect.DeepEqual(h1.Ldap, &LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true}) {
		t.Fatalf("h1 ldap 이상: %+v", h1.Ldap)
	}
	if h2.Ldap == nil || h2.Ldap.Backup != LdapBackupNone {
		t.Fatalf("h2 (전달 시점 ping X) 는 backup none 이어야 함: %+v", h2.Ldap)
	}
	if h1.DownAt != base+70 || h1.BootAt != base+600 || h1.StageAt != base+605 || h1.DoneSrc != DoneSrcRun {
		t.Fatalf("h1 시각/출처 이상: %+v", h1)
	}
}

// 정체 60분 경계: 59:59 설치중 / 60:00 설치중 / 60:01 정체 (READY 되면 해제)
func TestEffectiveStageStuckBoundary(t *testing.T) {
	h := &Host{SeenDown: true, DownAt: 1000, Stage: string(StageInstalling)}
	cases := map[int64]Stage{1000 + 3599: StageInstalling, 1000 + 3600: StageInstalling, 1000 + 3601: StageStuck}
	for now, want := range cases {
		if got := effectiveStage(h, now); got != want {
			t.Fatalf("now=+%d: %s, want %s", now-1000, got, want)
		}
	}
	h.Stage = string(StageStuck)
	if got := effectiveStage(h, 1000+10); got != StageDeploying {
		t.Fatalf("기준 미달 stuck → deploying 이어야 함: %s", got)
	}
	h.ReadyAt, h.Stage = 5000, string(StageReady)
	if got := effectiveStage(h, 9999); got != StageReady {
		t.Fatalf("READY 는 정체 아님: %s", got)
	}
	h.Processed = "0001"
	if got := effectiveStage(h, 9999); got != StageDone {
		t.Fatalf("processed → done: %s", got)
	}
	old := &Host{SeenDown: true} // 1차 JSON (stage/down_at 없음)
	if got := effectiveStage(old, 1<<40); got != StageDeploying {
		t.Fatalf("구버전 호스트: %s", got)
	}
}

// 데몬: anaconda 가 60분 넘게 끝나지 않으면 정체(저장), 이후 READY → run → 완료
func TestDaemonStuckThenReady(t *testing.T) {
	setupDir(t)
	x := newX(map[string]*simHost{"h1": install(60, 125, 3800, 3900)})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, x.world, newXDaemon(x, false, false), 0, 3665) // down_at=70 → 3595초 경과
	if s := hostStage(t, d, id, "h1"); s != StageInstalling {
		t.Fatalf("59분 55초: %s, 설치중이어야 함", s)
	}
	d = drive(t, x.world, d, 3670, 3675) // 3605초
	if s := hostStage(t, d, id, "h1"); s != StageStuck {
		t.Fatalf("60분 5초: %s, 정체여야 함", s)
	}
	if dj := readJobFile(t, jobFile(id)); dj.Hosts["h1"].Stage != string(StageStuck) || dj.Hosts["h1"].StageAt != base+3675 {
		t.Fatalf("정체가 저장되지 않음: %+v", dj.Hosts["h1"])
	}
	drive(t, x.world, d, 3680, 4000)
	wantRuns(t, x.world, []runCall{{At: 3900, Hosts: []string{"h1"}}})
	if s := doneJob(t, id).Hosts["h1"].Stage; s != string(StageDone) {
		t.Fatalf("최종 stage %s", s)
	}
}

func TestSelectSecondTargets(t *testing.T) {
	routes := map[string]string{"a": "local", "b": "local", "c": "local", "d": "os6", "e": "local", "f": "local"}
	res := RunResult{Processed: []string{"a", "c", "f"}, Failed: []string{"c", "d"}}
	got := selectSecondTargets(res, []string{"f", "c", "b", "a", "d"}, routes)
	// a: OK, b: 접속불가, c: FAIL, d: os6 제외, e: READY 아님(ready 에 없음), f: OK
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Fatalf("선별 이상: %v", got)
	}
	res.Abnormal = true
	if got := selectSecondTargets(res, []string{"b"}, routes); got != nil {
		t.Fatalf("비정상 종료면 2차 없음: %v", got)
	}
	if got := selectSecondTargets(RunResult{}, nil, routes); got != nil {
		t.Fatalf("빈 입력: %v", got)
	}
}

func secondScenario(t *testing.T, osCheck string) (*xworld, string) {
	setupDir(t)
	withOS6(t, "mgmt", osCheck)
	h3 := install(60, 125, 400, 600)
	h3.remote = true
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600), "h2": install(60, 125, 400, 600), "h3": h3})
	x.runFn = func(n int, o int64, hosts []string) RunResult {
		if n == 1 { // h1 은 FAIL 포함 처리, h2·h3 접속불가
			return RunResult{Processed: []string{"h1"}, Failed: []string{"h1"}}
		}
		return RunResult{Processed: hosts}
	}
	x.secondFn = func(tg []string) SecondResult { return SecondResult{Processed: tg, Fail: []string{"h1"}} }
	id := submit(t, "u1", 0, "h1", "h2", "h3")
	drive(t, x.world, newXDaemon(x, false, true), 0, 1000)
	return x, id
}

// 2차: route=local 의 접속불가·FAIL 만 1회, 최종 처리 = 1차 OK 또는 2차 OK, os6 호스트는 늦은 묶음으로
func TestSecondCheckMerge(t *testing.T) {
	x, id := secondScenario(t, "/os6/os_check.sh")
	ev := x.Events()
	want := []string{"run h1,h2,h3@600", "second h1,h2@600 code=0001", "run h3@785"}
	if !reflect.DeepEqual(ev, want) {
		t.Fatalf("호출 순서 불일치\n got: %v\nwant: %v", ev, want)
	}
	j := doneJob(t, id)
	if j.Hosts["h1"].Processed != "0001" || j.Hosts["h2"].Processed != "0001" || j.Hosts["h3"].Processed != "0002" {
		t.Fatalf("processed 이상: %+v %+v %+v", j.Hosts["h1"], j.Hosts["h2"], j.Hosts["h3"])
	}
	if j.Hosts["h1"].Second != SecondFail || j.Hosts["h2"].Second != SecondOK || j.Hosts["h3"].Second != "" {
		t.Fatalf("second 표시 이상: %q %q %q", j.Hosts["h1"].Second, j.Hosts["h2"].Second, j.Hosts["h3"].Second)
	}
	if len(x.walls) != 2 || !strings.Contains(x.walls[0], "\nh1 h2 (2대)\n") {
		t.Fatalf("wall 이상: %q", x.walls)
	}
}

// os6_os_check_sh 가 비면 2차 생략 → 1차 동작 그대로 (접속불가 h2 는 늦은 묶음 재시도)
func TestSecondSkippedWhenUnset(t *testing.T) {
	x, id := secondScenario(t, "")
	want := []string{"run h1,h2,h3@600", "run h2,h3@785"}
	if ev := x.Events(); !reflect.DeepEqual(ev, want) {
		t.Fatalf("호출 순서 불일치\n got: %v\nwant: %v", ev, want)
	}
	if j := doneJob(t, id); j.Hosts["h2"].Processed != "0002" || j.Hosts["h2"].Second != "" {
		t.Fatalf("h2 이상: %+v", j.Hosts["h2"])
	}
}

// 2차 진행 중 stage=second (저장됨), 끝나면 완료
func TestSecondStageVisible(t *testing.T) {
	setupDir(t)
	withOS6(t, "mgmt", "/os6/os_check.sh")
	x := newX(map[string]*simHost{"h1": install(60, 125, 400, 600)})
	x.runFn = func(n int, o int64, hosts []string) RunResult { return RunResult{} } // 접속불가
	x.secondGate = make(chan struct{})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, x.world, newXDaemon(x, false, true), 0, 595)
	step := func(o int64) {
		now := time.Unix(base+o, 0)
		x.clock.Set(now)
		d.step(now)
	}
	step(600)
	waitFor(t, "2차 시작", func() bool { return len(d.progCh) == 1 })
	step(605)
	if h := d.jobs[id].Hosts["h1"]; h.Stage != string(StageSecond) || h.Second != SecondRunning {
		t.Fatalf("2차 진행 표시 이상: %+v", h)
	}
	if h := readJobFile(t, jobFile(id)).Hosts["h1"]; h.Stage != string(StageSecond) {
		t.Fatalf("2차 stage 가 저장되지 않음: %+v", h)
	}
	close(x.secondGate)
	waitFor(t, "run 종료", func() bool { return len(d.doneCh) == 1 })
	step(610)
	if h := doneJob(t, id).Hosts["h1"]; h.Stage != string(StageDone) || h.Second != SecondOK || h.Processed != "0001" {
		t.Fatalf("2차 후 최종 이상: %+v", h)
	}
}

// 재기동: transient 단계(체크중/2차/LDAP확인)는 baseStage 로, second=running 은 비움
func TestRestartResetsTransient(t *testing.T) {
	setupDir(t)
	j := &Job{ID: "a-u", User: "u", Submitted: base, Hosts: map[string]*Host{
		"h1": {IP: "x", Route: "local", SeenDown: true, ReadyAt: base + 10, Stage: string(StageChecking), StageAt: base + 10},
		"h2": {IP: "x", Route: "local", SeenDown: true, ReadyAt: base + 10, Stage: string(StageSecond), Second: SecondRunning},
		"h3": {IP: "x", Route: "local", SeenDown: true, DownAt: base + 5, Stage: string(StageInstalling), StageAt: base + 5},
	}, Runs: []Run{}}
	if _, err := saveJob(j); err != nil {
		t.Fatal(err)
	}
	x := newX(map[string]*simHost{"h1": oldOS(), "h2": oldOS(), "h3": oldOS()})
	x.clock.Set(time.Unix(base+100, 0))
	d := newXDaemon(x, false, false)
	h := d.jobs["a-u"].Hosts
	if h["h1"].Stage != string(StageReady) || h["h1"].StageAt != base+100 || h["h2"].Stage != string(StageReady) || h["h2"].Second != "" {
		t.Fatalf("transient 복원 이상: %+v %+v", h["h1"], h["h2"])
	}
	if h["h3"].Stage != string(StageInstalling) || h["h3"].StageAt != base+5 {
		t.Fatalf("설치중은 그대로여야 함: %+v", h["h3"])
	}
}

// ---- pid / stopDaemon ----

func startFakeDaemon(t *testing.T, script string) *exec.Cmd {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "trap_ready")
	// $0=daemon → cmdline 에 "\x00daemon". trap 설치 후 $1 파일 생성 → 그 뒤에 신호를 보낸다
	cmd := exec.Command("bash", "-c", strings.Replace(script, "; while", `; : > "$1"; while`, 1), "daemon", marker)
	if err := cmd.Start(); err != nil {
		t.Skip("bash 없음:", err)
	}
	waitFor(t, "trap 설치", func() bool { _, err := os.Stat(marker); return err == nil })
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})
	if err := os.WriteFile(pidPath(), []byte(fmt.Sprintf("%d 1759386600\n", cmd.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestPidFileAndStopDaemon(t *testing.T) {
	setupDir(t)
	if err := stopDaemon(time.Second); err != errNotRunning {
		t.Fatalf("pid 파일 없음 → errNotRunning: %v", err)
	}
	if err := writePid(); err != nil {
		t.Fatal(err)
	}
	pid, started, err := readPid()
	if err != nil || pid != os.Getpid() || started <= 0 {
		t.Fatalf("readPid 이상: %d %d %v", pid, started, err)
	}
	if _, ok := isRunning(); ok {
		t.Fatal("daemon 인자가 없는 프로세스(테스트 바이너리)는 데몬으로 보지 않아야 함")
	}
	removePid()
	if _, err := os.Stat(pidPath()); !os.IsNotExist(err) {
		t.Fatal("removePid 후 pid 파일이 남음")
	}

	cmd := startFakeDaemon(t, `trap "exit 0" TERM; while :; do sleep 0.1; done`)
	if pid, ok := isRunning(); !ok || pid != cmd.Process.Pid {
		t.Fatalf("isRunning 이상: %d %v", pid, ok)
	}
	if s := BuildSnapshot(dataDir(), time.Unix(base, 0)); !s.Daemon.Running || s.Daemon.PID != cmd.Process.Pid || s.Daemon.Started != 1759386600 {
		t.Fatalf("스냅샷 데몬 정보 이상: %+v", s.Daemon)
	}
	if err := stopDaemon(5 * time.Second); err != nil {
		t.Fatalf("stopDaemon: %v", err)
	}
	if _, ok := isRunning(); ok {
		t.Fatal("stop 후에도 실행 중")
	}
}

func TestStopDaemonTimeoutNoKill(t *testing.T) {
	setupDir(t)
	cmd := startFakeDaemon(t, `trap "" TERM; while :; do sleep 0.1; done`)
	err := stopDaemon(500 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "SIGKILL 은 보내지 않음") {
		t.Fatalf("timeout 에러여야 함: %v", err)
	}
	if !processAlive(cmd.Process.Pid) {
		t.Fatal("SIGKILL 을 보내면 안 됨 (프로세스가 살아 있어야 함)")
	}
}
