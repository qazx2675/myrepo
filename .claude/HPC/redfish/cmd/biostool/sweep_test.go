package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// AUTH_FAIL 차단기(sweep.go) 시험. 순수 순회 로직은 sweepHosts 단위 시험으로,
// 실제 로그인 시도 횟수는 mockbmc.Farm (127.0.x.y 가상 BMC) 으로 확인한다. Farm 시험은 리눅스에서만 돈다.

// ---- sweepHosts 단위 ----

func TestSweepWarmupIsSerialThenParallel(t *testing.T) {
	var started, inFlight, maxInFlight int32
	sweepHosts(40, 8, 3, func(i int) hostOutcome {
		n := atomic.AddInt32(&started, 1)
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			m := atomic.LoadInt32(&maxInFlight)
			if cur <= m || atomic.CompareAndSwapInt32(&maxInFlight, m, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		if i == 0 && atomic.LoadInt32(&started) != n {
			t.Error("웜업 중(첫 호스트 처리 중)에 다른 호스트가 시작됨")
		}
		atomic.AddInt32(&inFlight, -1)
		return outLoggedIn // 첫 호스트가 로그인에 성공하면 바로 병렬 전환
	}, func(i int) { t.Errorf("차단 안 됐는데 skip(%d)", i) })
	if maxInFlight < 2 {
		t.Errorf("웜업 통과 후 병렬이어야 함: 최대 동시 %d", maxInFlight)
	}
}

func TestSweepStopsAfterWarmupAllAuthFail(t *testing.T) {
	var worked, skipped int32
	sweepHosts(100, 10, 3, func(i int) hostOutcome {
		atomic.AddInt32(&worked, 1)
		return outAuthFail
	}, func(i int) { atomic.AddInt32(&skipped, 1) })
	if worked != 3 || skipped != 97 {
		t.Errorf("처리 %d / 건너뜀 %d, 기대 3 / 97", worked, skipped)
	}
}

func TestSweepNoneOutcomeDoesNotUseWarmup(t *testing.T) {
	// 앞의 해석 실패(outNone)는 웜업 개수에 넣지 않는다: 그 뒤 3개가 연속 AUTH_FAIL 이면 멈춘다.
	var order []int
	var mu sync.Mutex
	var skipped int32
	sweepHosts(20, 10, 3, func(i int) hostOutcome {
		mu.Lock()
		order = append(order, i)
		mu.Unlock()
		if i < 4 {
			return outNone
		}
		return outAuthFail
	}, func(i int) { atomic.AddInt32(&skipped, 1) })
	if len(order) != 7 || skipped != 13 {
		t.Errorf("처리 %v / 건너뜀 %d, 기대 0..6 / 13", order, skipped)
	}
}

func TestSweepDisabledProcessesAll(t *testing.T) {
	var worked int32
	sweepHosts(50, 5, 0, func(i int) hostOutcome {
		atomic.AddInt32(&worked, 1)
		return outAuthFail
	}, func(i int) { t.Errorf("auth_fail_stop=0 인데 skip(%d)", i) })
	if worked != 50 {
		t.Errorf("처리 %d, 기대 50", worked)
	}
}

func TestSweepParallelPhaseStopsNewStarts(t *testing.T) {
	var worked, skipped, fails int32
	sweepHosts(300, 4, 3, func(i int) hostOutcome {
		atomic.AddInt32(&worked, 1)
		if i == 0 {
			return outLoggedIn // 웜업 통과
		}
		atomic.AddInt32(&fails, 1)
		time.Sleep(2 * time.Millisecond)
		return outAuthFail
	}, func(i int) { atomic.AddInt32(&skipped, 1) })
	// 멈춘 뒤에도 진행 중이던 호스트(최대 workers-1)는 끝까지 처리한다.
	if fails < 3 || fails > 3+3 || worked+skipped != 300 || skipped == 0 {
		t.Errorf("AUTH_FAIL %d (3..6 기대), 처리 %d, 건너뜀 %d", fails, worked, skipped)
	}
}

// ---- Farm 기반 ----

func farmIP(i int) string { return fmt.Sprintf("127.0.%d.%d", i/250+1, i%250+1) }

func newFarmOrSkip(t *testing.T, fo mockbmc.FarmOptions) (*mockbmc.Farm, int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("127.0.0.0/8 전체를 루프백으로 쓰는 리눅스에서만 시험한다")
	}
	f := mockbmc.NewFarm(fo)
	if _, err := f.Listen("0.0.0.0:0"); err != nil {
		t.Fatalf("Farm.Listen: %v", err)
	}
	t.Cleanup(f.Close)
	return f, f.Port()
}

func loadTreeT(t *testing.T, name string) *mockbmc.Tree {
	t.Helper()
	tr, err := mockbmc.LoadTree("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

// farmRC 는 ip 목록(포트 포함)으로 runContext 를 만듭니다.
func farmRC(t *testing.T, ips []string, port, conc, authStop int) *runContext {
	t.Helper()
	in := make([]string, len(ips))
	for i, ip := range ips {
		in[i] = fmt.Sprintf("%s:%d", ip, port)
	}
	ts, err := resolveTargets(in, filepath.Join(t.TempDir(), "hosts-없어도-IP-만이면-읽지-않음"))
	if err != nil {
		t.Fatal(err)
	}
	return &runContext{
		Conf: &Config{User: testUser, Concurrency: conc, Timeout: 10 * time.Second, Insecure: true,
			Retries: 0, AuthFailStop: authStop, DumpDir: "dumps"},
		Targets:  ts,
		Password: testPass,
	}
}

// addFarmHosts 는 n 대를 등록합니다 (badFrom <= i < badTo 는 비밀번호가 달라 401).
func addFarmHosts(t *testing.T, f *mockbmc.Farm, tree *mockbmc.Tree, n, badFrom, badTo int) []string {
	t.Helper()
	ips := make([]string, n)
	for i := range ips {
		ips[i] = farmIP(i)
		pass := testPass
		if i >= badFrom && i < badTo {
			pass = "서버쪽-다른-비밀번호"
		}
		if err := f.AddHost(ips[i], tree, mockbmc.HostOptions{User: testUser, Pass: pass}); err != nil {
			t.Fatal(err)
		}
	}
	return ips
}

func tallyOf(run *checkRun) map[string]int {
	h, _ := run.tally()
	return h
}

func readLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func runFarmCheck(t *testing.T, rc *runContext) *checkRun {
	t.Helper()
	fastCheck(t)
	run, err := doCheck(rc, checkOpts{Profile: testProf(t), ResultDir: t.TempDir(), Now: checkNow})
	if err != nil {
		t.Fatalf("doCheck: %v", err)
	}
	return run
}

func TestAuthStopAllUnauthorizedOnlyNLogins(t *testing.T) {
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	const total = 300
	ips := addFarmHosts(t, f, loadTreeT(t, "dell-r660"), total, 0, total)
	run := runFarmCheck(t, farmRC(t, ips, port, 50, 3))

	if tot := f.Totals(); tot.Sessions.LoginFailed != 3 || tot.Sessions.Created != 0 {
		t.Errorf("BMC 로그인 시도 = %+v, 기대 실패 3 (전 호스트 401 인데 N 회만 시도)", tot.Sessions)
	}
	h := tallyOf(run)
	if h[StatusAuthFail] != 3 || h[StatusSkippedAuthStop] != total-3 {
		t.Errorf("상태 집계 = %v, 기대 AUTH_FAIL 3 / SKIPPED_AUTH_STOP %d", h, total-3)
	}
	// retry.txt: SKIPPED 만 (AUTH_FAIL 호스트는 넣지 않는다)
	retry := readLines(t, filepath.Join(run.Dir, "retry.txt"))
	if len(retry) != total-3 {
		t.Fatalf("retry.txt %d줄, 기대 %d", len(retry), total-3)
	}
	authFailed := map[string]bool{}
	for _, hc := range run.Hosts {
		if hc.Status == StatusAuthFail {
			authFailed[hc.Target.Input] = true
		}
	}
	for _, l := range retry {
		if authFailed[l] {
			t.Errorf("retry.txt 에 AUTH_FAIL 호스트 %s 가 있음", l)
		}
	}
	// 앞 3대가 AUTH_FAIL (직렬 웜업), 나머지가 SKIPPED
	for i, hc := range run.Hosts {
		want := StatusSkippedAuthStop
		if i < 3 {
			want = StatusAuthFail
		}
		if hc.Status != want {
			t.Fatalf("호스트 %d 상태 %s, 기대 %s", i, hc.Status, want)
		}
	}
	// result.tsv: 호스트마다 단위 오류 1행
	if rows := readLines(t, filepath.Join(run.Dir, "result.tsv")); len(rows) != 1+total {
		t.Errorf("result.tsv %d줄, 기대 %d", len(rows), 1+total)
	}
	// 리포트 경고
	var b strings.Builder
	writeReport(&b, run, reportOpts{ListMax: 100, FailMax: 200})
	want := fmt.Sprintf("계정 잠금 방지를 위해 3대에서 AUTH_FAIL → 나머지 %d대 중단. 계정/비밀번호 확인 후 retry.txt 로 재실행", total-3)
	if !strings.Contains(b.String(), want) {
		t.Errorf("리포트에 경고가 없음 (%q)\n%s", want, b.String())
	}
	if info := run.runInfo(); !strings.Contains(info, want) || !strings.Contains(info, "auth_fail_stop=3") {
		t.Errorf("run_info.txt 에 차단 기록이 없음:\n%s", info)
	}
	if !isRetryStatus(StatusSkippedAuthStop) || isRetryStatus(StatusAuthFail) {
		t.Error("retry 대상: SKIPPED_AUTH_STOP 은 포함, AUTH_FAIL 은 제외여야 함")
	}
}

func TestAuthStopWarmupPassesThenContinues(t *testing.T) {
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	const total = 120
	// 앞 2대만 401, 그 뒤는 정상: N=3 웜업 중 세 번째에서 성공 → 병렬로 끝까지 진행.
	ips := addFarmHosts(t, f, loadTreeT(t, "dell-r660"), total, 0, 2)
	run := runFarmCheck(t, farmRC(t, ips, port, 20, 3))

	h := tallyOf(run)
	if h[StatusAuthFail] != 2 || h[StatusSkippedAuthStop] != 0 || h[StatusOK] != total-2 {
		t.Errorf("상태 집계 = %v, 기대 AUTH_FAIL 2 / OK %d / 건너뜀 0", h, total-2)
	}
	tot := f.Totals()
	if tot.Sessions.LoginFailed != 2 || tot.Sessions.Created != total-2 || tot.Sessions.Deleted != total-2 {
		t.Errorf("세션 = %+v", tot.Sessions)
	}
	var b strings.Builder
	writeReport(&b, run, reportOpts{ListMax: 100, FailMax: 200})
	if strings.Contains(b.String(), "계정 잠금 방지를 위해") {
		t.Error("건너뛴 호스트가 없으면 차단 경고를 내지 않아야 함")
	}
}

func TestAuthStopParallelPhaseStopsNewHosts(t *testing.T) {
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	const total, conc = 400, 5
	// 호스트 0 은 정상(웜업 통과), 1.. 은 전부 401 → 병렬 단계에서 누적 3 이상이 되면 새 시작 중단.
	ips := addFarmHosts(t, f, loadTreeT(t, "dell-r660"), total, 1, total)
	run := runFarmCheck(t, farmRC(t, ips, port, conc, 3))

	h := tallyOf(run)
	fails, skipped := h[StatusAuthFail], h[StatusSkippedAuthStop]
	if fails < 3 || fails > 3+conc-1 || skipped == 0 || h[StatusOK] != 1 || fails+skipped+h[StatusOK] != total {
		t.Errorf("상태 집계 = %v (AUTH_FAIL 3..%d, 건너뜀>0 기대)", h, 3+conc-1)
	}
	if got := f.Totals().Sessions.LoginFailed; got != fails {
		t.Errorf("BMC 로그인 실패 %d회 ≠ AUTH_FAIL 호스트 %d", got, fails)
	}
}

func TestAuthStopZeroDisables(t *testing.T) {
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	const total = 100
	ips := addFarmHosts(t, f, loadTreeT(t, "dell-r660"), total, 0, total)
	run := runFarmCheck(t, farmRC(t, ips, port, 20, 0))

	h := tallyOf(run)
	if h[StatusAuthFail] != total || h[StatusSkippedAuthStop] != 0 {
		t.Errorf("상태 집계 = %v, 기대 전부 AUTH_FAIL", h)
	}
	if got := f.Totals().Sessions.LoginFailed; got != total {
		t.Errorf("로그인 시도 %d회, 기대 %d (차단기 꺼짐)", got, total)
	}
}

func TestAuthStopKeepsNameResolutionErrors(t *testing.T) {
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	ips := addFarmHosts(t, f, loadTreeT(t, "dell-r660"), 10, 0, 10)
	rc := farmRC(t, ips, port, 5, 3)
	hostsFile := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(hostsFile, []byte("# 비어 있음\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	extra, err := resolveTargets([]string{"이름없는호스트"}, hostsFile)
	if err != nil {
		t.Fatal(err)
	}
	rc.Targets = append(rc.Targets, extra...) // 맨 끝: 접속 대상이 아니므로 차단돼도 NO_HOSTS_ENTRY 유지
	run := runFarmCheck(t, rc)
	if last := run.Hosts[len(run.Hosts)-1]; last.Status != StatusNoHostsEntry {
		t.Errorf("이름 해석 실패 호스트 상태 = %s, 기대 %s", last.Status, StatusNoHostsEntry)
	}
	if h := tallyOf(run); h[StatusSkippedAuthStop] != 7 {
		t.Errorf("집계 %v, 기대 SKIPPED 7", h)
	}
}

func TestAuthStopDumpAlsoStops(t *testing.T) {
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	const total = 200
	ips := addFarmHosts(t, f, loadTreeT(t, "dell-r660"), total, 0, total)
	old := dumpClientGap
	dumpClientGap = -1
	t.Cleanup(func() { dumpClientGap = old })
	out := t.TempDir()
	rs, err := runDump(farmRC(t, ips, port, 40, 3), dumpOpts{Out: out})
	if err != nil {
		t.Fatal(err)
	}
	var fails, skipped int
	for _, r := range rs {
		switch r.Status {
		case StatusAuthFail:
			fails++
		case StatusSkippedAuthStop:
			skipped++
		}
	}
	if fails != 3 || skipped != total-3 {
		t.Errorf("덤프 상태: AUTH_FAIL %d / SKIPPED %d, 기대 3 / %d", fails, skipped, total-3)
	}
	if got := f.Totals().Sessions.LoginFailed; got != 3 {
		t.Errorf("덤프의 BMC 로그인 시도 %d회, 기대 3", got)
	}
	var b strings.Builder
	writeDumpSummary(&b, rs, dumpOpts{Out: out})
	if !strings.Contains(b.String(), fmt.Sprintf("계정 잠금 방지를 위해 3대에서 AUTH_FAIL → 나머지 %d대 중단", total-3)) {
		t.Errorf("덤프 요약에 경고가 없음:\n%s", b.String())
	}
}
