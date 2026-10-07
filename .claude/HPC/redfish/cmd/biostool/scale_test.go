package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// 수천 대 규모 시험 (계획서 성공기준 6): mockbmc.Farm 으로 가상 BMC 3,000대(127.0.x.y)를 한 리스너에 올리고
// doCheck 를 동시 100 으로 돌려 완료·결과 행 수·세션 수·호출 안전·자원 사용(고루틴/연결/FD/힙)을 확인한다.
//
// 환경변수 BIOSTOOL_SCALE 로 호스트 수를 바꾼다 (기본 3000. -race 로 돌릴 때는 줄여서 쓴다).
// BIOSTOOL_SCALE_CONC 로 동시 접속 수를 바꾼다 (기본 100). testing.Short() 면 건너뛴다. 리눅스 전용.
//
// 호스트 구성 (i = 0..N-1, i%100 으로 분류):
//
//	i%100 == 7          미등록 IP → 연결이 끊겨 UNREACHABLE (1%)
//	i%100 == 13         Systems 경로가 항상 503 → BMC_ERROR (1%)
//	i%100 in {20,21,22} 현재값을 덮어써서 FAIL (3%, Dell/HPE 트리만 — 프로파일에서 verified=Y 인 트리)
//	그 밖               6개 트리를 돌아가며 사용 (정상)

func scaleEnvInt(t *testing.T, name string, def int) int {
	t.Helper()
	s := os.Getenv(name)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		t.Fatalf("%s=%q 는 1 이상의 정수여야 함", name, s)
	}
	return n
}

// resSampler 는 힙·고루틴·FD 최대치를 주기적으로 재 둡니다.
type resSampler struct {
	stop                 chan struct{}
	wg                   sync.WaitGroup
	maxHeap, maxSys      uint64
	maxGoroutines, maxFD int
}

func startSampler(every time.Duration) *resSampler {
	s := &resSampler{stop: make(chan struct{})}
	sample := func() {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		if ms.HeapAlloc > s.maxHeap {
			s.maxHeap = ms.HeapAlloc
		}
		if ms.Sys > s.maxSys {
			s.maxSys = ms.Sys
		}
		if g := runtime.NumGoroutine(); g > s.maxGoroutines {
			s.maxGoroutines = g
		}
		if ents, err := os.ReadDir("/proc/self/fd"); err == nil && len(ents) > s.maxFD {
			s.maxFD = len(ents)
		}
	}
	sample()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		tk := time.NewTicker(every)
		defer tk.Stop()
		for {
			select {
			case <-s.stop:
				sample()
				return
			case <-tk.C:
				sample()
			}
		}
	}()
	return s
}

func (s *resSampler) Stop() { close(s.stop); s.wg.Wait() }

// openFilesLimit 은 /proc/self/limits 의 "Max open files" 줄입니다 (없으면 "?").
func openFilesLimit() string {
	b, err := os.ReadFile("/proc/self/limits")
	if err != nil {
		return "?"
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "Max open files") {
			return strings.Join(strings.Fields(strings.TrimPrefix(l, "Max open files")), " ")
		}
	}
	return "?"
}

func mib(b uint64) string { return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20)) }

// scaleSig 는 호스트 1대 결과의 비교용 서명입니다 (호스트 상태 + 항목별 결과).
func scaleSig(h *hostCheck) string {
	var b strings.Builder
	b.WriteString(h.state())
	if h.Status != "" {
		return b.String()
	}
	for _, it := range h.Items {
		b.WriteString("|" + it.Std + "=" + it.Result)
	}
	return b.String()
}

var scaleTrees = []string{"dell-r660", "hpe-dl360gen11", "hpe-dl360gen10", "lenovo-sr650v3", "cisco-c220m7", "supermicro-x12"}

// 현재값을 덮어써 FAIL 로 만드는 속성 (프로파일에서 verified=Y 인 Dell·HPE 만 사용).
var scaleFailTrees = []struct {
	tree string
	attr string
}{{"dell-r660", "LogicalProc"}, {"hpe-dl360gen11", "ProcHyperthreading"}, {"hpe-dl360gen10", "ProcHyperthreading"}}

func scaleClass(i int) (kind string, idx int) {
	switch r := i % 100; {
	case r == 7:
		return "unr", 0
	case r == 13:
		return "5xx", 0
	case r >= 20 && r <= 22:
		return "f", i % len(scaleFailTrees)
	}
	return "n", i % len(scaleTrees)
}

func scaleTreeFor(kind string, idx int) (tree string, over map[string]interface{}, fail map[string]mockbmc.Fault) {
	switch kind {
	case "f":
		return scaleFailTrees[idx].tree, map[string]interface{}{scaleFailTrees[idx].attr: "Disabled"}, nil
	case "5xx":
		return "dell-r660", nil, map[string]mockbmc.Fault{"/redfish/v1/Systems*": {Status: 503}}
	}
	return scaleTrees[idx], nil, nil
}

func TestScaleCheckThousands(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: 수천 대 규모 시험 생략")
	}
	if runtime.GOOS != "linux" {
		t.Skip("127.0.0.0/8 전체를 루프백으로 쓰는 리눅스에서만 시험한다")
	}
	n := scaleEnvInt(t, "BIOSTOOL_SCALE", 3000)
	conc := scaleEnvInt(t, "BIOSTOOL_SCALE_CONC", 100)
	// 이 시험은 같은 프로세스에 클라이언트·서버가 함께 있어 동시 연결당 FD 가 2개 필요하다 (실사용은 클라이언트 쪽 1개).
	if fields := strings.Fields(openFilesLimit()); len(fields) > 0 {
		if soft, err := strconv.Atoi(fields[0]); err == nil && soft < 2*conc+100 {
			t.Skipf("ulimit -n %d 이(가) 낮습니다 (동시 %d 에는 %d 이상 필요: ulimit -n 을 올려 다시 실행)", soft, conc, 2*conc+100)
		}
	}
	fastCheck(t)
	prof := testProf(t)

	trees := map[string]*mockbmc.Tree{}
	loadTree := func(name string) *mockbmc.Tree {
		if trees[name] == nil {
			trees[name] = loadTreeT(t, name)
		}
		return trees[name]
	}

	// ---- 기준선: 같은 구성을 소수 호스트로 1대씩(직렬) 돌려 클래스별 기대 서명을 얻는다 ----
	baseFarm, basePort := newFarmOrSkip(t, mockbmc.FarmOptions{})
	want := map[string]string{} // 클래스 키 → 서명
	var baseIPs []string
	var baseKeys []string
	addBase := func(key string, kind string, idx int) {
		ip := farmIP(len(baseIPs))
		if kind != "unr" {
			tree, over, fail := scaleTreeFor(kind, idx)
			if err := baseFarm.AddHost(ip, loadTree(tree), mockbmc.HostOptions{User: testUser, Pass: testPass, AttrOverrides: over, Fail: fail}); err != nil {
				t.Fatal(err)
			}
		}
		baseIPs = append(baseIPs, ip)
		baseKeys = append(baseKeys, key)
	}
	for i := range scaleTrees {
		addBase(fmt.Sprintf("n%d", i), "n", i)
	}
	for i := range scaleFailTrees {
		addBase(fmt.Sprintf("f%d", i), "f", i)
	}
	addBase("5xx", "5xx", 0)
	addBase("unr", "unr", 0)
	baseRun, err := doCheck(farmRC(t, baseIPs, basePort, 1, 0), checkOpts{Profile: prof, ResultDir: t.TempDir(), Now: checkNow})
	if err != nil {
		t.Fatal(err)
	}
	baseRows := map[string]int{}
	for i, h := range baseRun.Hosts {
		want[baseKeys[i]] = scaleSig(h)
		baseRows[baseKeys[i]] = len(baseRun.rows(h))
		t.Logf("기준선 %-4s %s", baseKeys[i], want[baseKeys[i]])
	}
	if want["5xx"] != StatusBMCError || want["unr"] != StatusUnreachable {
		t.Fatalf("기준선: 5xx=%q unr=%q, 기대 BMC_ERROR / UNREACHABLE", want["5xx"], want["unr"])
	}
	for i := range scaleFailTrees {
		if s := want[fmt.Sprintf("f%d", i)]; !strings.HasPrefix(s, StatusFail+"|") {
			t.Fatalf("기준선: 덮어쓴 호스트 f%d 가 FAIL 이 아님: %s", i, s)
		}
	}
	baseFarm.Close()

	// ---- 본 시험 ----
	f, port := newFarmOrSkip(t, mockbmc.FarmOptions{})
	ips := make([]string, n)
	keys := make([]string, n)
	wantHosts := map[string]int{} // 기대 호스트 대표 상태 집계
	wantRows, registered := 0, 0
	nFail, nUnr, n5xx := 0, 0, 0
	for i := 0; i < n; i++ {
		kind, idx := scaleClass(i)
		ips[i] = farmIP(i)
		keys[i] = kind
		if kind == "n" || kind == "f" {
			keys[i] = fmt.Sprintf("%s%d", kind, idx)
		}
		switch kind {
		case "f":
			nFail++
		case "unr":
			nUnr++
		case "5xx":
			n5xx++
		}
		if kind != "unr" {
			tree, over, fail := scaleTreeFor(kind, idx)
			if err := f.AddHost(ips[i], loadTree(tree), mockbmc.HostOptions{User: testUser, Pass: testPass, AttrOverrides: over, Fail: fail}); err != nil {
				t.Fatal(err)
			}
			registered++
		}
		sig := want[keys[i]]
		wantHosts[strings.SplitN(sig, "|", 2)[0]]++
		wantRows += baseRows[keys[i]]
	}
	rc := farmRC(t, ips, port, conc, 3)

	runtime.GC()
	var ms0 runtime.MemStats
	runtime.ReadMemStats(&ms0)
	goBefore := runtime.NumGoroutine()
	smp := startSampler(25 * time.Millisecond)
	t0 := time.Now()
	run, err := doCheck(rc, checkOpts{Profile: prof, ResultDir: t.TempDir(), Now: checkNow})
	elapsed := time.Since(t0)
	smp.Stop()
	if err != nil {
		t.Fatalf("doCheck: %v", err)
	}

	// 결과 정리 대기: 고루틴이 시험 전 수준으로 돌아와야 한다 (연결·고루틴 누수 없음).
	goAfter := runtime.NumGoroutine()
	for end := time.Now().Add(10 * time.Second); goAfter > goBefore+10 && time.Now().Before(end); {
		time.Sleep(50 * time.Millisecond)
		goAfter = runtime.NumGoroutine()
	}

	t.Logf("규모: 호스트 %d대 (등록 %d, 미등록 %d, 5xx %d, FAIL 주입 %d), 동시 %d, 트리 %d종", n, registered, nUnr, n5xx, nFail, conc, len(scaleTrees))
	t.Logf("소요 시간: doCheck %s (호스트당 평균 %s, 초당 %.0f대)", elapsed.Round(time.Millisecond),
		(elapsed / time.Duration(n)).Round(10*time.Microsecond), float64(n)/elapsed.Seconds())
	t.Logf("peak 메모리: HeapAlloc 최대 %s (시작 전 %s), 프로세스 Sys 최대 %s", mib(smp.maxHeap), mib(ms0.HeapAlloc), mib(smp.maxSys))
	t.Logf("고루틴: 시작 전 %d → 최대 %d → 종료 후 %d", goBefore, smp.maxGoroutines, goAfter)
	t.Logf("연결: Farm 최대 동시 %d (종료 후 열린 연결 %d), 받은 연결 %d, 미등록이라 끊은 연결 %d", f.MaxConns(), f.OpenConns(), f.Accepted(), f.Dropped())
	t.Logf("FD: 최대 %d개 (프로세스 한도: %s) — 클라이언트·서버가 같은 프로세스라 양쪽 소켓이 모두 포함됨", smp.maxFD, openFilesLimit())

	// 결과 행 수 = 정상·FAIL 호스트 × 항목 + 호스트 단위 오류 1행씩
	rows := readLines(t, filepath.Join(run.Dir, "result.tsv"))
	if len(rows)-1 != wantRows {
		t.Errorf("result.tsv 데이터 행 %d, 기대 %d", len(rows)-1, wantRows)
	}
	// 상태별 개수
	got, _ := run.tally()
	for st, c := range wantHosts {
		if got[st] != c {
			t.Errorf("호스트 상태 %s = %d, 기대 %d", st, got[st], c)
		}
	}
	for st, c := range got {
		if wantHosts[st] == 0 && c > 0 {
			t.Errorf("예상 밖 상태 %s = %d", st, c)
		}
	}
	if got[StatusFail] != nFail || got[StatusUnreachable] != nUnr || got[StatusBMCError] != n5xx {
		t.Errorf("FAIL/UNREACHABLE/BMC_ERROR = %d/%d/%d, 기대 %d/%d/%d",
			got[StatusFail], got[StatusUnreachable], got[StatusBMCError], nFail, nUnr, n5xx)
	}
	// 호스트마다 서명(항목별 결과)이 기준선과 같다
	bad := 0
	for i, h := range run.Hosts {
		if s := scaleSig(h); s != want[keys[i]] {
			if bad++; bad <= 5 {
				t.Errorf("호스트 %d (%s, %s) 서명 %q, 기대 %q", i, ips[i], keys[i], s, want[keys[i]])
			}
		}
	}
	if bad > 5 {
		t.Errorf("... 서명 불일치 총 %d대", bad)
	}
	if got[StatusSkippedAuthStop] != 0 || got[StatusAuthFail] != 0 {
		t.Errorf("AUTH_FAIL 차단기가 작동하면 안 됨: %v", got)
	}

	// 호스트당 세션 생성 1 / 삭제 1 (등록 호스트 전부, 5xx 호스트 포함), 미등록은 접속 자체가 끊김
	tot := f.Totals()
	if tot.Hosts != registered || tot.Sessions.Created != registered || tot.Sessions.Deleted != registered || tot.Sessions.LoginFailed != 0 {
		t.Errorf("Farm 합계 = %+v, 기대 호스트 %d / 세션 생성·삭제 %d", tot, registered, registered)
	}
	badSess := 0
	for i := 0; i < n; i++ {
		if keys[i] == "unr" {
			continue
		}
		if ss := f.Host(ips[i]).Sessions(); ss.Created != 1 || ss.Deleted != 1 || ss.LoginFailed != 0 {
			if badSess++; badSess <= 5 {
				t.Errorf("호스트 %s 세션 = %+v, 기대 생성 1 / 삭제 1", ips[i], ss)
			}
		}
	}
	if tot.Writes != 0 || tot.LogHits != 0 {
		t.Errorf("Writes = %d, LogHits = %d, 기대 0 / 0", tot.Writes, tot.LogHits)
	}
	if f.Dropped() != nUnr {
		t.Errorf("미등록 호스트 연결 끊김 %d, 기대 %d", f.Dropped(), nUnr)
	}

	// 자원: 폭주하지 않고, 끝나면 원복
	if lim := goBefore + conc*15 + 200; smp.maxGoroutines > lim {
		t.Errorf("고루틴 최대 %d > 허용 %d (동시 %d 기준)", smp.maxGoroutines, lim, conc)
	}
	if goAfter > goBefore+10 {
		t.Errorf("종료 후 고루틴 %d, 시작 전 %d (누수?)", goAfter, goBefore)
	}
	if f.MaxConns() > conc*2 {
		t.Errorf("Farm 최대 동시 연결 %d > 동시 접속 %d 의 2배", f.MaxConns(), conc)
	}
	if smp.maxHeap > 1<<30 {
		t.Errorf("힙 최대 %s, 1GiB 초과", mib(smp.maxHeap))
	}
}
