package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)

const testDelay = 60 * time.Minute

func TestStatusLabelsAndOrder(t *testing.T) {
	want := []string{"확인전", "설치중", "지연", "디스크미인식", "설치완료", "ping X", "재부팅됨", "완료후꺼짐"}
	for i, w := range want {
		if Status(i).String() != w {
			t.Errorf("Status(%d)=%q, want %q", i, Status(i), w)
		}
	}
	b, _ := StatusDisk.MarshalText()
	var s Status
	if err := s.UnmarshalText(b); err != nil || s != StatusDisk {
		t.Errorf("round trip: %v %v", s, err)
	}
	if err := s.UnmarshalText([]byte("없음")); err == nil {
		t.Error("unknown label must fail")
	}
}

func TestApplyTable(t *testing.T) {
	first := t0.Add(-30 * time.Minute)
	rec := func(st Status, f time.Time) Record { return Record{Name: "h", Status: st, FirstInstallingAt: f} }
	tests := []struct {
		name      string
		prev      Record
		obs       Observation
		want      Status
		wantFirst time.Time // zero 이면 "여전히 zero" 가 아니라 t0 로 새로 찍힘을 뜻하는 sentinel 은 아래 newFirst 로
		newFirst  bool      // FirstInstallingAt 이 now 로 찍혀야 함
	}{
		{"새 서버 ping X", rec(StatusNone, time.Time{}), Observation{}, StatusPingX, time.Time{}, false},
		{"ping X 는 이력 유지", rec(StatusInstalling, first), Observation{}, StatusPingX, first, false},
		{"설치중 -> ping X -> 다시 설치중이어도 처음 본 시각 유지", rec(StatusPingX, first), Observation{Ping: true, Port: PortRefused}, StatusInstalling, first, false},
		{"22 거부 -> 설치중", rec(StatusNone, time.Time{}), Observation{Ping: true, Port: PortRefused}, StatusInstalling, time.Time{}, true},
		{"22 응답 없음 -> 설치중", rec(StatusNone, time.Time{}), Observation{Ping: true, Port: PortTimeout}, StatusInstalling, time.Time{}, true},
		{"pm 설치중", rec(StatusNone, time.Time{}), Observation{Ping: true, Port: PortOpen, Pm: PmInstalling}, StatusInstalling, time.Time{}, true},
		{"pm 설치중 + 디스크", rec(StatusInstalling, first), Observation{Ping: true, Port: PortOpen, Pm: PmInstalling, Disk: true}, StatusDisk, first, false},
		{"pm 그 밖 -> 설치중", rec(StatusNone, time.Time{}), Observation{Ping: true, Port: PortOpen, Pm: PmOther, PmReason: "Permission denied"}, StatusInstalling, time.Time{}, true},
		{"pm 확인 못 함 -> 이전 상태 유지", rec(StatusInstalling, first), Observation{Ping: true, Port: PortOpen, Pm: PmUnchecked}, StatusInstalling, first, false},
		{"pm 설치완료", rec(StatusInstalling, first), Observation{Ping: true, Port: PortOpen, Pm: PmDone}, StatusDone, first, false},
		{"지연 경계 직전(59m59s)", rec(StatusInstalling, t0.Add(-testDelay+time.Second)), Observation{Ping: true, Port: PortRefused}, StatusInstalling, t0.Add(-testDelay + time.Second), false},
		{"지연 경계(60m)", rec(StatusInstalling, t0.Add(-testDelay)), Observation{Ping: true, Port: PortRefused}, StatusDelayed, t0.Add(-testDelay), false},
		{"지연 유지", rec(StatusDelayed, t0.Add(-3*time.Hour)), Observation{Ping: true, Port: PortOpen, Pm: PmInstalling}, StatusDelayed, t0.Add(-3 * time.Hour), false},
		{"디스크가 지연보다 우선", rec(StatusDelayed, t0.Add(-3*time.Hour)), Observation{Ping: true, Port: PortOpen, Pm: PmInstalling, Disk: true}, StatusDisk, t0.Add(-3 * time.Hour), false},
		{"지연 서버 완료", rec(StatusDelayed, t0.Add(-3*time.Hour)), Observation{Ping: true, Port: PortOpen, Pm: PmDone}, StatusDone, t0.Add(-3 * time.Hour), false},
		{"디스크 확인 실패 시 이전 디스크미인식 유지", rec(StatusDisk, first), Observation{Ping: true, Port: PortOpen, Pm: PmInstalling, DiskError: true}, StatusDisk, first, false},
		{"디스크 확인 실패 시 설치중은 그대로", rec(StatusInstalling, first), Observation{Ping: true, Port: PortOpen, Pm: PmInstalling, DiskError: true}, StatusInstalling, first, false},
		{"디스크 표식이 사라지면 설치중", rec(StatusDisk, first), Observation{Ping: true, Port: PortOpen, Pm: PmInstalling}, StatusInstalling, first, false},
		// 완료 계열은 평소 회차에서 바뀌지 않는다(실제로는 ping 대상도 아님).
		{"설치완료 유지", rec(StatusDone, first), Observation{}, StatusDone, first, false},
		{"재부팅됨 유지", rec(StatusRebooted, first), Observation{}, StatusRebooted, first, false},
		{"완료후꺼짐 유지", rec(StatusOff, first), Observation{Ping: true, Port: PortRefused}, StatusOff, first, false},
	}
	for _, tc := range tests {
		got := apply(tc.prev, tc.obs, t0, testDelay)
		if got.Status != tc.want {
			t.Errorf("%s: status=%v, want %v", tc.name, got.Status, tc.want)
		}
		wf := tc.wantFirst
		if tc.newFirst {
			wf = t0
		}
		if !got.FirstInstallingAt.Equal(wf) {
			t.Errorf("%s: firstInstallingAt=%v, want %v", tc.name, got.FirstInstallingAt, wf)
		}
		if tc.want == StatusDone && tc.prev.Status != StatusDone && !got.DoneAt.Equal(t0) {
			t.Errorf("%s: doneAt=%v", tc.name, got.DoneAt)
		}
		if !got.LastSeen.Equal(t0) {
			t.Errorf("%s: lastSeen not updated", tc.name)
		}
	}
}

func TestApplyCompletedPing(t *testing.T) {
	later := t0.Add(time.Hour)
	tests := []struct {
		from   Status
		pingOK bool
		want   Status
	}{
		{StatusDone, true, StatusDone},
		{StatusDone, false, StatusOff},
		{StatusRebooted, true, StatusRebooted},
		{StatusRebooted, false, StatusOff},
		{StatusOff, false, StatusOff},
		{StatusOff, true, StatusRebooted},
		{StatusInstalling, false, StatusInstalling},
		{StatusPingX, true, StatusPingX},
	}
	for _, tc := range tests {
		got := ApplyCompletedPing(Record{Name: "h", Status: tc.from}, tc.pingOK, later)
		if got.Status != tc.want {
			t.Errorf("%v ping=%v: got %v, want %v", tc.from, tc.pingOK, got.Status, tc.want)
		}
	}
	off := ApplyCompletedPing(Record{Status: StatusDone}, false, later)
	if !off.OffAt.Equal(later) {
		t.Error("offAt not set")
	}
	re := ApplyCompletedPing(off, true, later.Add(time.Hour))
	if !re.RebootedAt.Equal(later.Add(time.Hour)) || !re.Status.CountsAsDone() {
		t.Errorf("rebooted: %+v", re)
	}
}

// fakeEnv 는 호스트별 가짜 응답으로 Deps 를 만든다.
type fakeEnv struct {
	mu       sync.Mutex
	now      time.Time
	pingDown map[string]bool // ping X
	refused  map[string]bool
	pmDone   map[string]bool
	pmInst   map[string]bool
	diskBad  map[string]bool

	pinged   [][]string
	pmCalls  [][]string
	diskCall [][]string
}

func newFake() *fakeEnv {
	return &fakeEnv{now: t0, pingDown: map[string]bool{}, refused: map[string]bool{}, pmDone: map[string]bool{}, pmInst: map[string]bool{}, diskBad: map[string]bool{}}
}

func (f *fakeEnv) deps() Deps {
	return Deps{
		Ping: func(hosts []string, _ time.Duration, _ int) (map[string]bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.pinged = append(f.pinged, append([]string{}, hosts...))
			m := map[string]bool{}
			for _, h := range hosts {
				m[h] = !f.pingDown[h]
			}
			return m, nil
		},
		Port: func(hosts []string, _ int, _ time.Duration) map[string]PortState {
			m := map[string]PortState{}
			for _, h := range hosts {
				if f.refused[h] {
					m[h] = PortRefused
				} else {
					m[h] = PortOpen
				}
			}
			return m
		},
		Pm: func(_ *Config, hosts []string) ([]string, []string, map[string]string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.pmCalls = append(f.pmCalls, append([]string{}, hosts...))
			var done, inst []string
			other := map[string]string{}
			for _, h := range hosts {
				switch {
				case f.pmDone[h]:
					done = append(done, h)
				case f.pmInst[h]:
					inst = append(inst, h)
				default:
					other[h] = "응답 없음"
				}
			}
			return done, inst, other, nil
		},
		Disk: func(_ *Config, hosts []string) ([]string, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.diskCall = append(f.diskCall, append([]string{}, hosts...))
			var bad []string
			for _, h := range hosts {
				if f.diskBad[h] {
					bad = append(bad, h)
				}
			}
			return bad, nil
		},
		Now: func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now },
	}
}

func (f *fakeEnv) advance(d time.Duration) { f.mu.Lock(); f.now = f.now.Add(d); f.mu.Unlock() }

func testCfg() *Config {
	c := defaultConfig()
	c.IntervalSec = 1
	return c
}

func status(c *Collector, h string) Status {
	for _, r := range c.Snapshot().Records {
		if r.Name == h {
			return r.Status
		}
	}
	return -1
}

func TestRoundPipelineAndCompletedExcluded(t *testing.T) {
	f := newFake()
	hosts := []string{"c1", "c2", "s1", "s2", "p1", "p2", "z1"}
	f.pmDone["c1"] = true
	f.pmInst["c2"], f.diskBad["c2"] = true, true
	f.pmInst["s1"] = true
	f.refused["s2"] = true
	f.pingDown["p1"] = true
	f.pmDone["p2"] = true
	f.pmInst["z1"] = true
	col, err := NewCollector(testCfg(), hosts, filepath.Join(t.TempDir(), "st.json"), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	if err := col.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]Status{"c1": StatusDone, "c2": StatusDisk, "s1": StatusInstalling, "s2": StatusInstalling, "p1": StatusPingX, "p2": StatusDone, "z1": StatusInstalling}
	for h, w := range want {
		if got := status(col, h); got != w {
			t.Errorf("%s: %v, want %v", h, got, w)
		}
	}
	s := col.Snapshot()
	if s.Counts[StatusDone] != 2 || s.Counts[StatusInstalling] != 3 || s.Counts[StatusDisk] != 1 || s.Counts[StatusPingX] != 1 || s.Total != 7 || s.Done() != 2 {
		t.Errorf("counts: %v", s.Counts)
	}
	if s.Round != 1 || s.Running || s.Updated.IsZero() || s.LastErr != "" {
		t.Errorf("snapshot meta: %+v", s)
	}
	// 단계 입력: pm 은 22 열린 서버만(s2 거부, p1 ping X 제외), 디스크는 pm 설치중만.
	if len(f.pmCalls) != 1 || len(f.pmCalls[0]) != 5 {
		t.Errorf("pm calls: %v", f.pmCalls)
	}
	if len(f.diskCall) != 1 || len(f.diskCall[0]) != 3 {
		t.Errorf("disk calls: %v", f.diskCall)
	}
	// 2회차: 완료(c1,p2)는 ping 하지 않는다. 대상이 없으면 gossh 도 부르지 않는다.
	f.pmCalls, f.diskCall, f.pinged = nil, nil, nil
	f.pingDown["c1"] = true // 완료 서버가 꺼져도 평소엔 모른다
	if err := col.Round(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.pinged) != 1 || len(f.pinged[0]) != 5 {
		t.Errorf("round2 ping targets: %v", f.pinged)
	}
	for _, h := range f.pinged[0] {
		if h == "c1" || h == "p2" {
			t.Errorf("completed host %s pinged", h)
		}
	}
	if status(col, "c1") != StatusDone {
		t.Error("c1 must stay 설치완료 in normal rounds")
	}
	// 모두 완료/접속불가면 gossh 호출 없음
	f2 := newFake()
	f2.pingDown["a"] = true
	c2, _ := NewCollector(testCfg(), []string{"a"}, filepath.Join(t.TempDir(), "s.json"), f2.deps())
	c2.Round(context.Background())
	if len(f2.pmCalls) != 0 || len(f2.diskCall) != 0 {
		t.Error("gossh must not run without targets")
	}
}

func TestRoundDelayAndCheckCompleted(t *testing.T) {
	f := newFake()
	f.refused["a"] = true
	f.pmDone["b"] = true
	f.pmDone["d"] = true
	col, _ := NewCollector(testCfg(), []string{"a", "b", "d"}, filepath.Join(t.TempDir(), "s.json"), f.deps())
	ctx := context.Background()
	col.Round(ctx)
	if status(col, "a") != StatusInstalling {
		t.Fatal("a should be installing")
	}
	f.advance(59 * time.Minute)
	col.Round(ctx)
	if status(col, "a") != StatusInstalling {
		t.Error("59분은 아직 설치중")
	}
	f.advance(time.Minute)
	col.Round(ctx)
	if status(col, "a") != StatusDelayed {
		t.Error("60분이면 지연")
	}
	// c 키: b 는 꺼짐, d 는 그대로
	f.pingDown["b"] = true
	f.pinged = nil
	if err := col.CheckCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.pinged) != 1 || len(f.pinged[0]) != 2 {
		t.Errorf("c ping targets: %v", f.pinged)
	}
	if status(col, "b") != StatusOff || status(col, "d") != StatusDone {
		t.Errorf("b=%v d=%v", status(col, "b"), status(col, "d"))
	}
	if col.Snapshot().Counts[StatusOff] != 1 {
		t.Error("완료후꺼짐 count")
	}
	// 다시 켜짐 -> 재부팅됨(완료 수에 포함)
	delete(f.pingDown, "b")
	col.CheckCompleted(ctx)
	if status(col, "b") != StatusRebooted || col.Snapshot().Done() != 2 {
		t.Errorf("b=%v done=%d", status(col, "b"), col.Snapshot().Done())
	}
	// 일반 회차는 완료후꺼짐/재부팅됨을 건드리지 않는다
	f.pingDown["b"] = true
	col.Round(ctx)
	if status(col, "b") != StatusRebooted {
		t.Error("round must not change 재부팅됨")
	}
}

func TestPersistenceKeepsHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.pm_state.json")
	f := newFake()
	f.refused["a"] = true
	f.pmDone["b"] = true
	f.pingDown["gone"] = true
	cfg := testCfg()
	col, _ := NewCollector(cfg, []string{"a", "b", "gone"}, path, f.deps())
	col.Round(context.Background())
	f.advance(2 * time.Hour)
	col.Round(context.Background())
	if status(col, "a") != StatusDelayed {
		t.Fatal("a delayed")
	}

	// 재시작: gone 은 호스트파일에서 빠지고 new 가 추가된다
	f2 := newFake()
	f2.now = f.now.Add(time.Minute)
	f2.refused["a"] = true
	col2, err := NewCollector(cfg, []string{"a", "b", "new"}, path, f2.deps())
	if err != nil {
		t.Fatal(err)
	}
	s := col2.Snapshot()
	if s.Total != 3 {
		t.Fatalf("total=%d", s.Total)
	}
	if status(col2, "a") != StatusDelayed || status(col2, "b") != StatusDone || status(col2, "new") != StatusNone {
		t.Errorf("restored: a=%v b=%v new=%v", status(col2, "a"), status(col2, "b"), status(col2, "new"))
	}
	var a Record
	for _, r := range s.Records {
		if r.Name == "a" {
			a = r
		}
	}
	if !a.FirstInstallingAt.Equal(t0) {
		t.Errorf("firstInstallingAt lost: %v", a.FirstInstallingAt)
	}
	col2.Round(context.Background())
	if status(col2, "a") != StatusDelayed { // 처음 본 시각이 유지돼 바로 지연
		t.Errorf("a after restart: %v", status(col2, "a"))
	}
	b, _ := os.ReadFile(path)
	if contains(string(b), `"gone"`) {
		t.Error("removed host kept in state")
	}
	if !contains(string(b), `"지연"`) {
		t.Error("state should store Korean labels")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestCheckFieldsPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	f := newFake()
	f.pmDone["a"] = true
	col, _ := NewCollector(testCfg(), []string{"a"}, path, f.deps())
	col.Round(context.Background())
	col.mu.Lock()
	r := col.recs["a"]
	r.Check = Check{LDAP: "OK", DNSNTP: "FAIL", Autofs: "OK", HT: "ON", Note: "n"}
	col.recs["a"] = r
	col.mu.Unlock()
	if err := col.save(); err != nil {
		t.Fatal(err)
	}
	col2, _ := NewCollector(testCfg(), []string{"a"}, path, f.deps())
	if got := col2.Snapshot().Records[0].Check; got != (Check{LDAP: "OK", DNSNTP: "FAIL", Autofs: "OK", HT: "ON", Note: "n"}) {
		t.Errorf("check lost: %+v", got)
	}
}

func TestSaveOnlyWhenChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	f := newFake()
	f.refused["a"] = true
	col, _ := NewCollector(testCfg(), []string{"a", "b"}, path, f.deps())
	f.pingDown["b"] = true
	col.Round(context.Background())
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	f.advance(time.Minute) // 시각만 지나고 상태는 그대로
	col.Round(context.Background())
	st, _ := os.Stat(path)
	if !st.ModTime().Equal(old) {
		t.Error("state rewritten though nothing changed")
	}
	// 재시작 직후에도 같은 내용이면 쓰지 않는다
	col2, _ := NewCollector(testCfg(), []string{"a", "b"}, path, f.deps())
	col2.Round(context.Background())
	st, _ = os.Stat(path)
	if !st.ModTime().Equal(old) {
		t.Error("state rewritten after restart with same content")
	}
	// 바뀌면 쓰고, 임시 파일은 남기지 않는다
	delete(f.pingDown, "b")
	f.pmDone["b"] = true
	col2.Round(context.Background())
	st, _ = os.Stat(path)
	if st.ModTime().Equal(old) {
		t.Error("state not written after change")
	}
	ents, _ := os.ReadDir(filepath.Dir(path))
	if len(ents) != 1 {
		t.Errorf("leftover files: %d", len(ents))
	}
}

func TestSnapshotSortAndVendors(t *testing.T) {
	f := newFake()
	hosts := []string{"s2", "c9", "s1", "c1", "zz", "p1"}
	f.pingDown["s2"], f.pingDown["c9"], f.pingDown["zz"] = true, true, true
	f.refused["s1"] = true
	f.pmDone["c1"] = true
	f.refused["p1"] = true
	f.diskBad["p1"] = true // 22 거부라 디스크 확인 안 함 -> 설치중
	col, _ := NewCollector(testCfg(), hosts, filepath.Join(t.TempDir(), "s.json"), f.deps())
	if col.Snapshot().Counts[StatusNone] != 6 {
		t.Error("before first round all 확인전")
	}
	col.Round(context.Background())
	var got []string
	for _, r := range col.Snapshot().Records {
		got = append(got, r.Name)
	}
	want := []string{"p1", "s1", "c1", "c9", "s2", "zz"} // 설치중(p1,s1) 설치완료(c1) ping X(c9,s2,zz)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order=%v, want %v", got, want)
		}
	}
	vs := col.Snapshot().Vendors
	order := testCfg().VendorOrder()
	if len(vs) != len(order) {
		t.Fatalf("vendors=%d", len(vs))
	}
	for i, v := range vs {
		if v.Vendor != order[i] {
			t.Errorf("vendor order %d: %s", i, v.Vendor)
		}
	}
	for _, v := range vs {
		switch v.Vendor {
		case "D":
			if len(v.Hosts[StatusPingX]) != 1 || v.Hosts[StatusPingX][0] != "c9" {
				t.Errorf("D ping X: %v", v.Hosts)
			}
		case "sds":
			if h := v.Hosts[StatusPingX]; len(h) != 1 || h[0] != "s2" {
				t.Errorf("sds ping X: %v", v.Hosts)
			}
		case "기타":
			if h := v.Hosts[StatusPingX]; len(h) != 1 || h[0] != "zz" {
				t.Errorf("기타 ping X: %v", v.Hosts)
			}
		}
	}
}

func TestRunNeverOverlaps(t *testing.T) {
	f := newFake()
	var inflight, maxIn, calls int32
	d := f.deps()
	d.Ping = func(hosts []string, _ time.Duration, _ int) (map[string]bool, error) {
		n := atomic.AddInt32(&inflight, 1)
		for {
			m := atomic.LoadInt32(&maxIn)
			if n <= m || atomic.CompareAndSwapInt32(&maxIn, m, n) {
				break
			}
		}
		atomic.AddInt32(&calls, 1)
		time.Sleep(30 * time.Millisecond) // interval(5ms)보다 오래 걸림
		atomic.AddInt32(&inflight, -1)
		m := map[string]bool{}
		for _, h := range hosts {
			m[h] = true
		}
		return m, nil
	}
	f.refused["a"] = true
	col, _ := NewCollector(testCfg(), []string{"a"}, filepath.Join(t.TempDir(), "s.json"), d)
	col.interval = 5 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	// 다른 고루틴에서 Round 를 동시에 불러도 겹치지 않아야 한다.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); col.Run(ctx) }()
	go func() {
		defer wg.Done()
		for i := 0; i < 3; i++ {
			col.Round(context.Background())
		}
	}()
	wg.Wait()
	if maxIn != 1 {
		t.Errorf("rounds overlapped: max in flight %d", maxIn)
	}
	if calls < 3 {
		t.Errorf("too few rounds: %d", calls)
	}
	if s := col.Snapshot(); s.Counts[StatusInstalling] != 1 || s.Round < 3 {
		t.Errorf("snapshot: %+v", s)
	}
}

func TestRunWaitsIntervalFromStart(t *testing.T) {
	f := newFake()
	var starts []time.Time
	var mu sync.Mutex
	d := f.deps()
	d.Ping = func(hosts []string, _ time.Duration, _ int) (map[string]bool, error) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		return map[string]bool{}, nil
	}
	col, _ := NewCollector(testCfg(), []string{"a"}, filepath.Join(t.TempDir(), "s.json"), d)
	col.interval = 80 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	col.Run(ctx)
	mu.Lock()
	defer mu.Unlock()
	if len(starts) < 2 {
		t.Fatalf("rounds=%d", len(starts))
	}
	if gap := starts[1].Sub(starts[0]); gap < 75*time.Millisecond || gap > 110*time.Millisecond {
		t.Errorf("start-to-start gap %v, want ~80ms", gap)
	}
}

func TestRoundPingErrorKeepsState(t *testing.T) {
	f := newFake()
	f.pmDone["a"] = true
	d := f.deps()
	col, _ := NewCollector(testCfg(), []string{"a"}, filepath.Join(t.TempDir(), "s.json"), d)
	col.Round(context.Background())
	d2 := d
	d2.Ping = func([]string, time.Duration, int) (map[string]bool, error) { return nil, os.ErrPermission }
	col.deps = d2
	col.recs["a"] = Record{Name: "a", Vendor: "기타", Status: StatusInstalling}
	if err := col.Round(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if status(col, "a") != StatusInstalling || col.Snapshot().LastErr == "" {
		t.Error("state must be kept and error recorded")
	}
}

func TestNewCollectorBadState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	os.WriteFile(path, []byte("{broken"), 0o600)
	if _, err := NewCollector(testCfg(), []string{"a"}, path, newFake().deps()); err == nil {
		t.Error("corrupt state must be an error")
	}
}
