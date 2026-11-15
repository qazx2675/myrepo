// hold_test.go - 늦은 READY 보류 (conf stuck_hold_hours): down_at 뒤 N시간을 넘겨 READY 된 호스트는 LDAP 복원·자동 run 을 하지 않고
// 보류로 두며(재기동해도 유지, job 은 열린 채), h→u(request release) 로 자동 흐름에 되돌리고, 수동 run·수동 완료는 그대로 된다.
package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStuckHoldConf(t *testing.T) {
	saveConfVars(t)
	for _, c := range []struct {
		in   string
		want time.Duration
	}{
		{"", 24 * time.Hour}, {" ", 24 * time.Hour}, {"abc", 24 * time.Hour}, {"-1", 24 * time.Hour}, {"1.5", 24 * time.Hour},
		{"0", 0}, {"1", time.Hour}, {" 48 ", 48 * time.Hour},
	} {
		stuck_hold_hours = c.in
		if got := stuckHold(); got != c.want {
			t.Errorf("stuck_hold_hours=%q: %v, 기대 %v", c.in, got, c.want)
		}
	}
	p := writeConf(t, "stuck_hold_hours = 0\n")
	stuck_hold_hours = ""
	loadConf([]string{p})
	if got := stuckHold(); got != 0 {
		t.Errorf("conf 에서 읽은 0: %v", got)
	}
}

func withHoldHours(t *testing.T, v string) {
	t.Helper()
	old := stuck_hold_hours
	stuck_hold_hours = v
	t.Cleanup(func() { stuck_hold_hours = old })
}

func holdLog(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(logPath())
	return string(b)
}

// holdLdap: 전달 시점 백업은 모두 ok, Apply 호출(호스트)을 기록
type holdLdap struct {
	mu      sync.Mutex
	applied []string
}

func (l *holdLdap) Backup(_ *Job, hosts []string) map[string]LdapState {
	out := map[string]LdapState{}
	for _, h := range hosts {
		out[h] = LdapState{Backup: LdapBackupOK}
	}
	return out
}

func (l *holdLdap) Apply(_ *Job, host string) LdapState {
	l.mu.Lock()
	l.applied = append(l.applied, host)
	l.mu.Unlock()
	return LdapState{Backup: LdapBackupOK, Bindpw: BindpwSame}
}

// h2 는 ping X(≈70) 뒤 4000 에 READY — stuck_hold_hours=1 이면 보류, 0(끔)·기본 24 면 종전처럼 늦은 묶음 run
func TestLateReadyHold(t *testing.T) {
	for _, c := range []struct {
		hours string
		held  bool
	}{{"1", true}, {"0", false}, {"", false}} {
		t.Run("hours="+c.hours, func(t *testing.T) {
			setupDir(t)
			withRoute6(t, "", "")
			withHoldHours(t, c.hours)
			w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 600), "h2": install(60, 120, 400, 4000)})
			id := submit(t, "u1", 0, "h1", "h2")
			d := drive(t, w, newTestDaemon(w), 0, 4500)
			if !c.held {
				if len(w.runs) != 2 || strings.Join(w.runs[1].Hosts, ",") != "h2" {
					t.Fatalf("보류 없이 늦은 묶음 run 이어야 함: %+v", w.runs)
				}
				if j := doneJob(t, id); j.Hosts["h2"].Hold != "" || j.Hosts["h2"].Processed == "" {
					t.Fatalf("h2 상태: %+v", j.Hosts["h2"])
				}
				if strings.Contains(holdLog(t), "늦은 READY 보류") {
					t.Fatal("보류 로그가 없어야 함")
				}
				return
			}
			wantRuns(t, w, []runCall{{At: 1020, Hosts: []string{"h1", "h2"}}})
			h2 := d.jobs[id].Hosts["h2"]
			if h2.Hold == "" || h2.HoldAt == 0 || h2.ReadyAt == 0 || effectiveStage(h2, base+4500) != StageHeld {
				t.Fatalf("h2 가 보류여야 함: %+v", h2)
			}
			if !strings.Contains(holdLog(t), "늦은 READY 보류: h2 (정체 1시간, 기준 stuck_hold_hours=1") {
				t.Fatalf("보류 로그:\n%s", holdLog(t))
			}
			// job 은 열린 채 (closeFinished 가 닫지 않음), 저장된 JSON 에 보류가 남는다
			fj := readJobFile(t, jobFile(id))
			if fj.Hosts["h2"].Hold == "" {
				t.Fatalf("저장된 job 에 hold 없음: %+v", fj.Hosts["h2"])
			}
			// 스냅샷: 단계 보류, 집계 held, 비고·hold 사유
			s := BuildSnapshot(dataDir(), time.Unix(base+4500, 0))
			sh := s.Jobs[0].Groups[0].Hosts[1]
			if sh.Host != "h2" || sh.Stage != StageHeld || sh.Label != "보류" || sh.Hold == "" || !strings.Contains(sh.Note, "보류(") ||
				s.Totals.Held != 1 || s.Jobs[0].Closed {
				t.Fatalf("스냅샷: %+v totals=%+v", sh, s.Totals)
			}
		})
	}
}

// 보류 호스트는 1차 대기·1차 목록·LDAP 복원에서 빠지고, 재기동해도 보류가 유지되며, release 로 lateWait 뒤 자동 run 된다
func TestHeldSkippedThenReleased(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	withHoldHours(t, "1")
	w := newWorld(map[string]*simHost{
		"h1": install(60, 120, 400, 3700),     // ≈3720 READY, down_at ≈70 → 보류
		"h2": install(3000, 3050, 3300, 3800), // ≈3810 READY, 정상
	})
	ld := &holdLdap{}
	id := submit(t, "u1", 0, "h1", "h2")
	d := newTestDaemon(w)
	d.Ldap = ld
	d = drive(t, w, d, 0, 3745)
	d = drive(t, w, newTestDaemon(w), 3750, 4200) // 보류 뒤 재기동 (LDAP 훅은 새 데몬에 다시 넣음)
	_ = d
	// 재기동한 데몬은 Ldap 이 기본(nop)이라, LDAP 확인은 첫 데몬 구간으로 따로 본다
	if !held(readJobFile(t, jobFile(id)).Hosts["h1"]) {
		t.Fatal("재기동 뒤에도 h1 보류가 유지되어야 함")
	}
	// h1 은 보류라 1차 대기 대상이 아니므로 h2 READY 즉시 1차 run, 목록에도 h1 없음
	wantRuns(t, w, []runCall{{At: 3810, Hosts: []string{"h2"}}})
	if len(ld.applied) != 0 {
		t.Fatalf("보류 호스트에 LDAP 복원이 호출됨: %v", ld.applied)
	}
	if n := strings.Count(holdLog(t), "늦은 READY 보류: h1"); n != 1 {
		t.Fatalf("보류 로그 %d회", n)
	}

	// 보류 해제(h→u 와 같은 요청) → READY 시각을 지금으로, 1차 뒤라 lateWait(3분) 뒤 늦은 묶음 run
	d2 := newTestDaemon(w)
	d2.Ldap = ld
	if _, err := writeRequest(ReqRelease, map[string]string{"jobid": id, "yml": "*"}); err != nil {
		t.Fatal(err)
	}
	drive(t, w, d2, 4205, 4600)
	wantRuns(t, w, []runCall{{At: 3810, Hosts: []string{"h2"}}, {At: 4385, Hosts: []string{"h1"}}})
	if strings.Join(ld.applied, ",") != "h1" {
		t.Fatalf("해제 뒤 LDAP 복원이 1회 있어야 함: %v", ld.applied)
	}
	j := doneJob(t, id)
	if j.Hosts["h1"].Hold != "" || j.Hosts["h1"].Processed != "0002" {
		t.Fatalf("h1 해제·처리 이상: %+v", j.Hosts["h1"])
	}
	if !strings.Contains(holdLog(t), "보류 해제: job "+id+" 그룹 * 1대 (h1)") {
		t.Fatalf("해제 로그:\n%s", holdLog(t))
	}
}

// 보류 호스트가 없으면 release 는 거부, 수동 run(h→c)은 보류 호스트에도 실행되어 완료 처리된다
func TestHeldManualRunAndReleaseReject(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	withHoldHours(t, "1")
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 3700)})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 3800)
	if !held(d.jobs[id].Hosts["h1"]) || len(w.runs) != 0 {
		t.Fatalf("보류·run 없음 전제: %+v %+v", d.jobs[id].Hosts["h1"], w.runs)
	}
	if r := d.releaseHold(id, "없는.yml", time.Unix(base+3800, 0)); r != "그룹 없음" {
		t.Fatalf("없는 그룹: %q", r)
	}
	if _, err := writeRequest(ReqManualRun, map[string]string{"jobid": id, "yml": allGroup}); err != nil {
		t.Fatal(err)
	}
	drive(t, w, d, 3805, 3900)
	if len(w.runs) != 1 || strings.Join(w.runs[0].Hosts, ",") != "h1" {
		t.Fatalf("수동 run 이 보류 호스트에 실행되어야 함: %+v", w.runs)
	}
	j := doneJob(t, id)
	if j.Hosts["h1"].Processed == "" {
		t.Fatalf("수동 run 으로 완료되어야 함: %+v", j.Hosts["h1"])
	}
	if r := d.releaseHold(id, "*", time.Unix(base+3900, 0)); r == "" {
		t.Fatal("종료된 job 의 release 는 거부되어야 함")
	}
}

// 수동 완료(done)는 보류 호스트에도 그대로 → job 종료
func TestHeldManualDone(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	withHoldHours(t, "1")
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 3700)})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 3800)
	if err := markDone([]string{"h1"}, DoneSrcManual); err != nil {
		t.Fatal(err)
	}
	drive(t, w, d, 3805, 3850)
	if j := doneJob(t, id); j.Hosts["h1"].DoneSrc != DoneSrcManual || len(w.runs) != 0 {
		t.Fatalf("수동 완료: %+v runs=%+v", j.Hosts["h1"], w.runs)
	}
}

// 보류 중 재부팅(READY 해제 → down_at 새로) 뒤 다시 READY 가 되어도 보류는 유지된다
func TestHoldSurvivesReboot(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	withHoldHours(t, "1")
	h := install(60, 120, 400, 3700)
	h.downs = [][2]int64{{3800, 3900}}
	w := newWorld(map[string]*simHost{"h1": h})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 4300)
	hh := d.jobs[id].Hosts["h1"]
	if !held(hh) || hh.ReadyAt < base+3900 || len(w.runs) != 0 || effectiveStage(hh, base+4300) != StageHeld {
		t.Fatalf("재부팅 뒤에도 보류 유지: %+v runs=%+v", hh, w.runs)
	}
	if !strings.Contains(holdLog(t), "READY 해제(재부팅): h1") {
		t.Fatal("전제: 재부팅 감지")
	}
}

// TUI: 보류 호스트가 있을 때만 h 메뉴에 u, h→u → y/n → release 요청(그룹), 없으면 안내만
func TestReleaseKeyTUI(t *testing.T) {
	heldHost := tsHost("a3", StageHeld, 30, "보류(정체 30시간, h→u 해제)")
	snap := tsSnap(true, tsJob("J1", "user1", false,
		tsGroup("a.yml", "ib", "rhel8", "pxe", "y", tsHost("a1", StageDone, 10, ""), heldHost)))
	src := &fakeSrc{snap: snap, local: true}
	r := runKeys(t, src, "\rhuy")
	if len(src.reqs) != 1 || src.reqs[0].kind != ReqRelease || src.reqs[0].payload["jobid"] != "J1" || src.reqs[0].payload["yml"] != "a.yml" {
		t.Fatalf("release 요청: %+v", src.reqs)
	}
	out := r.out.String()
	for _, want := range []string{"u 보류 해제", "보류 1대를 해제", "[y/n]", "보류 해제를 요청했습니다", "보류 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
	// n 이면 요청하지 않음
	src = &fakeSrc{snap: snap, local: true}
	runKeys(t, src, "\run")
	if len(src.reqs) != 0 {
		t.Fatalf("n 인데 요청됨: %+v", src.reqs)
	}
	// 보류가 없으면 메뉴에 u 가 없고, u 를 눌러도 안내만
	src = &fakeSrc{snap: twoGroupSnap(), local: true}
	r = runKeys(t, src, "\rhu")
	if len(src.reqs) != 0 || strings.Contains(r.out.String(), "u 보류 해제") {
		t.Fatalf("보류 없음: 요청·메뉴 항목이 없어야 함: %+v", src.reqs)
	}
	src = &fakeSrc{snap: twoGroupSnap(), local: true}
	r = runKeys(t, src, "\ru")
	if len(src.reqs) != 0 || !strings.Contains(r.out.String(), "보류된 호스트가 없습니다") {
		t.Fatalf("보류 없음 안내: %+v", src.reqs)
	}
}

// CLI·원격 중계: request release <jobid> <yml|*>
func TestReleaseRequestArgs(t *testing.T) {
	if !requestArgsOK([]string{ReqRelease, "J1", "*"}) || requestArgsOK([]string{ReqRelease, "J1"}) {
		t.Fatal("requestArgsOK release")
	}
	a, err := requestArgs(ReqRelease, map[string]string{"jobid": "J1", "yml": "a.yml"})
	if err != nil || strings.Join(a, " ") != "request release J1 a.yml" {
		t.Fatalf("requestArgs: %v %v", a, err)
	}
	setupDir(t)
	if rc := cmdRequest([]string{ReqRelease, "J1", "*"}); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	names := listReqFiles(requestsDir())
	sort.Strings(names)
	if len(names) != 1 || !strings.HasSuffix(names[0], "_release.req") {
		t.Fatalf("요청 파일: %v", names)
	}
	b, _ := os.ReadFile(filepath.Join(requestsDir(), names[0]))
	if pl := parseRequest(b); pl["jobid"] != "J1" || pl["yml"] != "*" {
		t.Fatalf("payload: %v", pl)
	}
}

// 실행 중인 run(수동 포함)·os6 직접 실행에 들어 있는 보류 호스트는 release 가 건드리지 않는다
func TestReleaseSkipsBusyHost(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	withHoldHours(t, "1")
	w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 3700)})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 3800)
	now := time.Unix(base+3800, 0)
	d.runs[999] = &inflight{id: 999, jobID: id, hosts: []string{"h1"}, manual: true}
	if r := d.releaseHold(id, "*", now); !strings.Contains(r, "실행 중") || !held(d.jobs[id].Hosts["h1"]) {
		t.Fatalf("run 중 호스트: %q %+v", r, d.jobs[id].Hosts["h1"])
	}
	delete(d.runs, 999)
	d.os6Hold = map[string]string{"h1": "lk1"}
	if r := d.releaseHold(id, "*", now); r == "" || !held(d.jobs[id].Hosts["h1"]) || d.forceCheck["h1"] {
		t.Fatalf("os6 직접 실행 중 호스트: %q", r)
	}
	if !strings.Contains(holdLog(t), "[!] 보류 해제 보류: job "+id+" 1대") {
		t.Fatalf("건너뜀 로그:\n%s", holdLog(t))
	}
}

// 보류 호스트의 수동 run(h→c) 직전에 LDAP 비교·복원을 한 번 한다 (설정체크만 t 는 하지 않음)
func TestHeldManualRunAppliesLdap(t *testing.T) {
	for _, mode := range []string{"", ModeCheck} {
		setupDir(t)
		withRoute6(t, "", "")
		withHoldHours(t, "1")
		w := newWorld(map[string]*simHost{"h1": install(60, 120, 400, 3700)})
		ld := &holdLdap{}
		id := submit(t, "u1", 0, "h1")
		d := newTestDaemon(w)
		d.Ldap = ld
		d = drive(t, w, d, 0, 3800)
		if len(ld.applied) != 0 || !held(d.jobs[id].Hosts["h1"]) {
			t.Fatalf("전제: 보류·LDAP 안 함 %v", ld.applied)
		}
		pl := map[string]string{"jobid": id, "yml": allGroup}
		if mode != "" {
			pl["mode"] = mode
		}
		if _, err := writeRequest(ReqManualRun, pl); err != nil {
			t.Fatal(err)
		}
		drive(t, w, d, 3805, 3850)
		want := "h1"
		if mode == ModeCheck {
			want = ""
		}
		if len(w.runs) != 1 || strings.Join(ld.applied, ",") != want {
			t.Fatalf("mode=%q: runs=%+v ldap=%v", mode, w.runs, ld.applied)
		}
	}
}

// ping X 를 못 본(down_at=0) 호스트는 job 전달 시각을 기준으로 보류 판정
func TestHoldFallbackSubmitted(t *testing.T) {
	setupDir(t)
	withHoldHours(t, "1")
	w := newWorld(map[string]*simHost{})
	d := newTestDaemon(w)
	for _, c := range []struct {
		after int64
		held  bool
	}{{3601, true}, {3600, false}, {1800, false}} {
		j := &Job{ID: "20251002-153000-u1", User: "u1", Submitted: base, Hosts: map[string]*Host{"h1": {Route: "local", Stage: string(StageQueued)}}}
		d.jobs = map[string]*Job{j.ID: j}
		d.applyCheck(j, "h1", CheckResult{Responded: true, Uptime: 60}, base+c.after)
		h := j.Hosts["h1"]
		if h.ReadyAt == 0 || held(h) != c.held {
			t.Fatalf("전달 뒤 %d초: %+v", c.after, h)
		}
	}
	withHoldHours(t, "0")
	j := &Job{ID: "20251002-153000-u1", User: "u1", Submitted: base, Hosts: map[string]*Host{"h1": {Route: "local"}}}
	d.jobs = map[string]*Job{j.ID: j}
	d.applyCheck(j, "h1", CheckResult{Responded: true, Uptime: 60}, base+99999)
	if held(j.Hosts["h1"]) {
		t.Fatal("0 이면 보류 안 함")
	}
}

// 보류 중 재부팅된 호스트를 해제하면 down_at 이 지금으로 잡혀 다음 READY 에서 다시 보류되지 않고 자동 run 된다
func TestReleaseAfterRebootNotReheld(t *testing.T) {
	setupDir(t)
	withRoute6(t, "", "")
	withHoldHours(t, "1")
	h := install(60, 120, 400, 3700)
	h.downs = [][2]int64{{3800, 7600}} // 보류 뒤 재부팅(down_at≈3810)해 오래 꺼져 있다가 해제(7500) 직후 켜짐
	w := newWorld(map[string]*simHost{"h1": h})
	id := submit(t, "u1", 0, "h1")
	d := drive(t, w, newTestDaemon(w), 0, 7500)
	if hh := d.jobs[id].Hosts["h1"]; !held(hh) || hh.ReadyAt != 0 {
		t.Fatalf("전제: 보류 중 재부팅(READY 해제): %+v", hh)
	}
	if r := d.releaseHold(id, "*", time.Unix(base+7500, 0)); r != "" {
		t.Fatal(r)
	}
	drive(t, w, d, 7505, 8200)
	if len(w.runs) != 1 || w.runs[0].Hosts[0] != "h1" {
		t.Fatalf("해제 뒤 자동 run 이어야 함: %+v", w.runs)
	}
	if j := doneJob(t, id); j.Hosts["h1"].Hold != "" {
		t.Fatalf("다시 보류됨: %+v", j.Hosts["h1"])
	}
}
