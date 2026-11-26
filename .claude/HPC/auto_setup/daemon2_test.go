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
		if s := f.x.world.hosts[h]; s != nil && !s.up(f.x.off()) {
			out[h] = noneState(ldapNoResp) // 실제처럼: 꺼져 있으면 수집 무응답
			continue
		}
		out[h] = LdapState{Backup: LdapBackupOK}
	}
	return out
}

// Probe: 비교만 (READY 직후 기본) — 복원이 필요하다고 보고 '복원 대기' 를 돌려준다
func (f fakeLdap) Probe(j *Job, host string) LdapState {
	f.record("probe", j, host)
	return pendingState(nil)
}

// Restore: 비교 후 복원 (ldap_auto_restore=y 의 READY 직후·request ldap-restore)
func (f fakeLdap) Restore(j *Job, host string) LdapState {
	f.record("apply", j, host)
	return LdapState{Bindpw: BindpwDiff, Applied: true}
}

func (f fakeLdap) record(kind string, j *Job, host string) {
	f.x.ev("%s %s@%d", kind, host, f.x.off())
	if dj, err := loadJobFile(jobFile(j.ID)); err == nil {
		f.x.mu.Lock()
		f.x.applyDisk[host] = dj.Hosts[host].Stage
		f.x.mu.Unlock()
	}
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

// stage 전이 전체 + LDAP: 백업은 전달 직후 ping O 호스트만 1회, 확인은 READY 직후·run 전(디스크 stage=ldap).
// 기본(ldap_auto_restore 꺼짐)은 비교만(probe) → 복원 대기로 두고 run 은 그대로 진행, request ldap-restore 로 복원(apply).
// ldap_auto_restore=y 면 READY 직후 비교 후 바로 복원(apply).
func TestStageTransitionsAndLdap(t *testing.T) {
	for _, auto := range []string{"", "y"} {
		t.Run("ldap_auto_restore="+auto, func(t *testing.T) { testStageTransitionsAndLdap(t, auto) })
	}
}

func testStageTransitionsAndLdap(t *testing.T, auto string) {
	old := ldap_auto_restore
	ldap_auto_restore = auto
	t.Cleanup(func() { ldap_auto_restore = old })
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
	kind, wantLdap := "probe", &LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Reason: ldapPendingPrefix}
	if auto == "y" {
		kind, wantLdap = "apply", &LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true}
	}
	wantEv := []string{"backup h1@0", kind + " h1@600", "run h1,h2@600"}
	if !reflect.DeepEqual(ev, wantEv) {
		t.Fatalf("LDAP 호출 순서 불일치\n got: %v\nwant: %v", ev, wantEv)
	}
	if x.applyDisk["h1"] != string(StageLdap) {
		t.Fatalf("Apply 시점 디스크 stage = %q, ldap 이어야 함", x.applyDisk["h1"])
	}
	j := doneJob(t, id)
	h1, h2 := j.Hosts["h1"], j.Hosts["h2"]
	if !reflect.DeepEqual(h1.Ldap, wantLdap) {
		t.Fatalf("h1 ldap 이상: %+v", h1.Ldap)
	}
	if h2.Ldap == nil || h2.Ldap.Backup != LdapBackupNone {
		t.Fatalf("h2 (전달 시점 ping X) 는 backup none 이어야 함: %+v", h2.Ldap)
	}
	if h1.DownAt != base+70 || h1.BootAt != base+600 || h1.StageAt != base+605 || h1.DoneSrc != DoneSrcRun {
		t.Fatalf("h1 시각/출처 이상: %+v", h1)
	}
	if auto == "y" {
		return
	}
	// 복원은 명령으로: 종료된 job 도 request ldap-restore '*' → 복원 대기 호스트(h1)만 복원, 백업 none(h2)은 건너뜀
	if _, err := writeRequest(ReqLdapRestore, map[string]string{"jobid": id, "yml": "*"}); err != nil {
		t.Fatal(err)
	}
	d = drive(t, x.world, d, 705, 705)
	if ev := x.Events(); !reflect.DeepEqual(ev, append(wantEv, "apply h1@705")) {
		t.Fatalf("ldap-restore 요청 뒤 호출: %v", ev)
	}
	if l := doneJob(t, id).Hosts["h1"].Ldap; !reflect.DeepEqual(l, &LdapState{Backup: LdapBackupOK, Bindpw: BindpwDiff, Applied: true}) {
		t.Fatalf("복원 뒤 h1 ldap(jobs/done): %+v", l)
	}
	if lg, _ := os.ReadFile(logPath()); !strings.Contains(string(lg), "LDAP 복원(요청): h1 (job "+id+") binddn=diff applied=true") {
		t.Fatalf("복원 로그 없음:\n%s", lg)
	}
	if names := listReqFiles(requestsDir()); len(names) != 0 {
		t.Fatalf("요청이 처리되지 않음: %v", names)
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

// retryLdap: 호스트별 okAt 오프셋부터 ok(ldapbak 에 meta.json 저장), 그 전(또는 okAt 없음)은 일시적 실패("무응답: SSH 접속 실패: …").
// Backup 호출을 "호스트@오프셋" 으로 기록
type retryLdap struct {
	w       *world
	mu      sync.Mutex
	okAt    map[string]int64
	calls   []string
	applied []string
}

const retryWhy = ldapNoResp + ": SSH 접속 실패: connection refused"

func (l *retryLdap) Backup(j *Job, hosts []string) map[string]LdapState {
	l.mu.Lock()
	defer l.mu.Unlock()
	o := l.w.off()
	out := map[string]LdapState{}
	for _, h := range hosts {
		l.calls = append(l.calls, fmt.Sprintf("%s@%d", h, o))
		if at, ok := l.okAt[h]; ok && o >= at {
			in := lbInfo{Files: []lbEntry{{Name: "ldap.conf", Mode: "0644", Owner: "root", Group: "root", Data: []byte("BINDDN uid=a,dc=x\n")}}}
			if err := saveBackup(j.ID, h, in); err != nil {
				out[h] = noneState("저장 실패")
				continue
			}
			out[h] = LdapState{Backup: LdapBackupOK}
			continue
		}
		out[h] = noneState(retryWhy)
	}
	return out
}

func (l *retryLdap) Probe(j *Job, host string) LdapState { return l.Restore(j, host) }

func (l *retryLdap) Restore(_ *Job, host string) LdapState {
	l.mu.Lock()
	l.applied = append(l.applied, host)
	l.mu.Unlock()
	return LdapState{Backup: LdapBackupOK, Bindpw: BindpwSame}
}

func (l *retryLdap) Calls() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func withLdapRetry(t *testing.T, max int) {
	t.Helper()
	a, b := ldapRetryMax, ldapRetryGap
	ldapRetryMax, ldapRetryGap = max, 60*time.Second
	t.Cleanup(func() { ldapRetryMax, ldapRetryGap = a, b })
}

// 일시적 실패(무응답·수집 호출 실패)는 none 으로 확정하지 않고 ldapRetryGap(60초) 뒤 다시 시도 → 성공하면 ok
func TestLdapBackupRetryAfterTransient(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	withLdapRetry(t, 10)
	w := newWorld(map[string]*simHost{"h1": oldOS()})
	l := &retryLdap{w: w, okAt: map[string]int64{"h1": 30}}
	id := submit(t, "u1", 0, "h1")
	d := newTestDaemon(w)
	d.Ldap = l
	d = drive(t, w, d, 0, 150)
	if got := strings.Join(l.Calls(), " "); got != "h1@0 h1@60" {
		t.Fatalf("재시도 호출 불일치: %s (0 실패 → 60 재시도 성공, 그 뒤 호출 없음)", got)
	}
	if st := d.jobs[id].Hosts["h1"].Ldap; !reflect.DeepEqual(st, &LdapState{Backup: LdapBackupOK}) {
		t.Fatalf("재시도 성공 뒤 ok 여야 함: %+v", st)
	}
	if !strings.Contains(holdLog(t), "LDAP 백업 재시도 예정: h1") {
		t.Fatal("재시도 예정 로그 없음")
	}
}

// 재시도 중 seen_down(설치 시작) 이 되면 그때 마지막 사유로 none 확정 (더 시도하지 않음)
func TestLdapBackupSeenDownKeepsReason(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	withLdapRetry(t, 10)
	w := newWorld(map[string]*simHost{"h1": install(30, 1000, 2000, 3000)})
	l := &retryLdap{w: w}
	id := submit(t, "u1", 0, "h1")
	d := newTestDaemon(w)
	d.Ldap = l
	d = drive(t, w, d, 0, 200)
	if got := strings.Join(l.Calls(), " "); got != "h1@0" {
		t.Fatalf("seen_down 뒤에는 재시도하지 않아야 함: %s", got)
	}
	h := d.jobs[id].Hosts["h1"]
	if !h.SeenDown || !reflect.DeepEqual(h.Ldap, &LdapState{Backup: LdapBackupNone, Reason: retryWhy}) {
		t.Fatalf("seen_down 뒤 none(사유 유지) 여야 함: seen_down=%v %+v", h.SeenDown, h.Ldap)
	}
}

// 상한(ldapRetryMax) 회 모두 일시적 실패면 마지막 사유로 none 확정, 이후 호출 없음
func TestLdapBackupRetryLimit(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	withLdapRetry(t, 3)
	w := newWorld(map[string]*simHost{"h1": oldOS()})
	l := &retryLdap{w: w}
	id := submit(t, "u1", 0, "h1")
	d := newTestDaemon(w)
	d.Ldap = l
	d = drive(t, w, d, 0, 400)
	if got := strings.Join(l.Calls(), " "); got != "h1@0 h1@60 h1@120" {
		t.Fatalf("상한 3회 호출 불일치: %s", got)
	}
	if st := d.jobs[id].Hosts["h1"].Ldap; !reflect.DeepEqual(st, &LdapState{Backup: LdapBackupNone, Reason: retryWhy}) {
		t.Fatalf("상한 뒤 none(사유 유지) 여야 함: %+v", st)
	}
}

// READY 이후(NoPing·수동 재시도 확인 등으로 ping X 를 못 본) 호스트·설치 진행 이후 단계·재기동 직후 준비확인 전 호스트는
// 백업 후보가 아니다. READY 호스트는 "전달 시점 백업 없음" 으로 명시 기록 (ldapApply 도 조용히 건너뛰지 않음)
func TestLdapBackupSkipsReadyHosts(t *testing.T) {
	setupDir(t)
	w := newWorld(map[string]*simHost{})
	l := &retryLdap{w: w, okAt: map[string]int64{"rdy": 0, "chk": 0, "rst": 0, "q": 0, "rdy2": 0}}
	d := &Daemon{Clock: w.clock, Ldap: l}
	d.init()
	j := &Job{ID: "j1", Submitted: base, Hosts: map[string]*Host{
		"rdy":  {Route: "local", ReadyAt: base + 50, Stage: string(StageReady)},
		"chk":  {Route: "local", Stage: string(StageChecking)},
		"rst":  {Route: "local", Stage: string(StageQueued)},
		"q":    {Route: "local", Stage: string(StageQueued)},
		"rdy2": {Route: "local", Stage: string(StageQueued)},
	}}
	d.jobs[j.ID] = j
	for n := range j.Hosts {
		d.hrt(n).up = true
	}
	d.rt["rst"].startCheck = true                  // 재기동 직후: 기동 직후 준비확인이 READY 여부를 정하기 전
	d.rt["rdy2"].ldapNext = time.Unix(base+200, 0) // 재시도 대기 중
	d.ldapBackup(time.Unix(base+100, 0))
	if got := strings.Join(l.Calls(), " "); got != "q@0" {
		t.Fatalf("백업 후보는 전달 직후 단계(q)뿐이어야 함: %s", got)
	}
	want := map[string]*LdapState{"rdy": {Backup: LdapBackupNone, Reason: "전달 시점 백업 없음"}, "chk": nil, "rst": nil,
		"q": {Backup: LdapBackupOK}, "rdy2": nil}
	for n, st := range want {
		if !reflect.DeepEqual(j.Hosts[n].Ldap, st) {
			t.Fatalf("%s: %+v, want %+v", n, j.Hosts[n].Ldap, st)
		}
	}
	// 재시도 대기 중이던 호스트가 그대로 READY → ldapApply 가 마지막 사유로 none 기록, Probe·Restore 는 호출하지 않음
	d.rt["rdy2"].ldapWhy = retryWhy
	j.Hosts["rdy2"].ReadyAt = base + 150
	d.ldapApply(j, "rdy2", base+150)
	if !reflect.DeepEqual(j.Hosts["rdy2"].Ldap, &LdapState{Backup: LdapBackupNone, Reason: retryWhy}) || len(l.applied) != 0 {
		t.Fatalf("READY 시 기록 이상: %+v applied=%v", j.Hosts["rdy2"].Ldap, l.applied)
	}
	// 사유 없이 READY (재시도 기록 없음) → "전달 시점 백업 없음"
	j.Hosts["chk"].ReadyAt = base + 160
	d.ldapApply(j, "chk", base+160)
	if !reflect.DeepEqual(j.Hosts["chk"].Ldap, &LdapState{Backup: LdapBackupNone, Reason: "전달 시점 백업 없음"}) {
		t.Fatalf("READY 시 기록 이상: %+v", j.Hosts["chk"].Ldap)
	}
}

// 같은 호스트 재전달: 이전 job 의 전달 시점 백업(ok)을 새 job 으로 옮겨 이어받고 새로 백업하지 않는다
func TestLdapBackupInheritOnRedeliver(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	w := newWorld(map[string]*simHost{"h1": oldOS(), "h2": oldOS(), "h3": oldOS()})
	l := &retryLdap{w: w, okAt: map[string]int64{"h1": 0, "h2": 0, "h3": 0}}
	id1 := submit(t, "u1", 0, "h1", "h2")
	d := newTestDaemon(w)
	d.Ldap = l
	d = drive(t, w, d, 0, 20)
	id2 := submit(t, "u2", 30, "h1", "h3")
	d = drive(t, w, d, 30, 60)
	if got := strings.Join(l.Calls(), " "); got != "h1@0 h2@0 h3@30" {
		t.Fatalf("이어받은 h1 은 다시 백업하지 않아야 함: %s", got)
	}
	if st := d.jobs[id2].Hosts["h1"].Ldap; !reflect.DeepEqual(st, &LdapState{Backup: LdapBackupOK}) {
		t.Fatalf("새 job h1 ldap: %+v", st)
	}
	if _, ok := d.jobs[id1].Hosts["h1"]; ok {
		t.Fatal("이전 job 에서 h1 이 빠져야 함")
	}
	newDir, _ := ldapHostDir(id2, "h1")
	oldDir, _ := ldapHostDir(id1, "h1")
	if _, err := os.Stat(filepath.Join(newDir, lbMetaName)); err != nil {
		t.Fatalf("새 job 백업 디렉터리 없음: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("이전 job 백업 디렉터리는 옮겨져야 함: %v", err)
	}
	if _, _, err := loadBackup(id2, "h1"); err != nil {
		t.Fatalf("이어받은 백업 읽기: %v", err)
	}
	if dj := readJobFile(t, jobFile(id2)); dj.Hosts["h1"].Ldap == nil || dj.Hosts["h1"].Ldap.Backup != LdapBackupOK {
		t.Fatalf("이어받은 ldap 이 저장되지 않음: %+v", dj.Hosts["h1"].Ldap)
	}
}
