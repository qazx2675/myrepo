package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var tNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

// tuiSnap 은 골든·키 테스트용 표본이다: 설치중 14(지연 포함 4), 디스크 3, 설치완료 12, ping X 6, 재부팅됨 2, 완료후꺼짐 3.
func tuiSnap() Snapshot {
	cfg := defaultConfig()
	var recs []Record
	add := func(prefix string, vendor string, n int, st Status, reason string, startMin int) {
		for i := 0; i < n; i++ {
			r := Record{Name: fmt.Sprintf("%s%03d", prefix, i+1), Vendor: vendor, Status: st, Reason: reason}
			if startMin > 0 {
				r.FirstInstallingAt = tNow.Add(-time.Duration(startMin+i*7) * time.Minute)
			}
			recs = append(recs, r)
		}
	}
	add("c", "D", 6, StatusInstalling, "OS 설치중", 5)
	add("h", "D", 4, StatusInstalling, "22번 포트 거부", 20)
	add("s", "sds", 4, StatusDelayed, "OS 설치중", 61)
	add("p", "L", 3, StatusDisk, "디스크 미인식", 30)
	add("c1", "D", 8, StatusDone, "", 0)
	add("l", "j", 4, StatusDone, "", 0)
	add("s1", "sds", 6, StatusPingX, "ping 응답 없음", 0)
	add("d", "t", 2, StatusRebooted, "완료 후 꺼졌다가 다시 켜짐", 0)
	add("m", "W", 3, StatusOff, "완료 후 ping 응답 없음", 0)
	s := buildSnapshot(cfg, recs, 3, true, tNow.Add(-95*time.Second), "")
	return *s
}

func newTestView(w, h int) *ViewState {
	return &ViewState{W: w, H: h, Now: tNow, Warning: "설정 파일 pm_monitor.conf 없음 - 기본값 사용"}
}

func press(v *ViewState, snap Snapshot, keys ...string) action {
	var act action
	for _, k := range keys {
		for _, ev := range parseKeys([]byte(k)) {
			if a := v.handleKey(ev, snap); a != actNone {
				act = a
			}
		}
	}
	return act
}

func TestTUIGolden(t *testing.T) {
	snap := tuiSnap()
	type tc struct {
		name string
		prep func(v *ViewState, snap Snapshot)
	}
	cases := []tc{
		{"default", func(v *ViewState, s Snapshot) {}},
		{"filter", func(v *ViewState, s Snapshot) { press(v, s, "1") }},
		{"search", func(v *ViewState, s Snapshot) { press(v, s, "/", "s1", "\r") }},
		{"searching", func(v *ViewState, s Snapshot) { press(v, s, "/", "s0") }},
		{"c_busy", func(v *ViewState, s Snapshot) { press(v, s, "c") }},
		{"c_panel", func(v *ViewState, s Snapshot) { press(v, s, "c"); v.checkDone(nil) }},
		{"r_panel", func(v *ViewState, s Snapshot) { press(v, s, "r") }},
		{"d_panel", func(v *ViewState, s Snapshot) { press(v, s, "d"); v.setReport('d', "DHCP 조회중...", true) }},
		{"d_result", func(v *ViewState, s Snapshot) {
			press(v, s, "d")
			v.setReport('d', "host=s1001 mac=aa:bb:cc:00:00:01 ip=10.0.0.1 lease=active\nhost=s1002 mac=aa:bb:cc:00:00:02 ip=10.0.0.2 lease=expired\n(없는 서버: s1003)", false)
		}},
		{"l_result", func(v *ViewState, s Snapshot) {
			press(v, s, "l")
			v.setReport('l', ldapTable(withChecks(s)), false)
		}},
		{"scroll_end", func(v *ViewState, s Snapshot) { press(v, s, "\x1b[F") }},
		{"c_panel_end", func(v *ViewState, s Snapshot) { press(v, s, "c"); v.checkDone(nil); press(v, s, "\x1b[F") }},
	}
	for _, w := range []int{60, 80, 120} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s_%d", c.name, w), func(t *testing.T) {
				v := newTestView(w, 24)
				c.prep(v, snap)
				got := render(*v, snap, w, 24)
				if n := strings.Count(got, "\n") + 1; n != 24 {
					t.Errorf("줄 수 %d, 24 여야 함", n)
				}
				for i, l := range strings.Split(got, "\n") {
					if sw := strWidth(l); sw > w {
						t.Errorf("%d번째 줄 폭 %d > %d: %q", i+1, sw, w, l)
					}
				}
				golden(t, fmt.Sprintf("tui_%s_%d", c.name, w), got+"\n")
			})
		}
	}
}

func TestTUIHeaderContent(t *testing.T) {
	snap := tuiSnap()
	v := newTestView(80, 24)
	got := render(*v, snap, 80, 24)
	for _, want := range []string{"전체 40대", "1 설치중 10", "2 지연 4", "3 디스크미인식 3", "4 설치완료 12", "5 ping X 6",
		"6 재부팅됨 2", "7 완료후꺼짐 3", "갱신 11:58:25", "회차 3", "수집중", "경고: 설정 파일", "q 종료"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q 없음\n%s", want, got)
		}
	}
	snap.Running = false
	snap.LastErr = "gossh -pm: 실패"
	if got := render(*v, snap, 80, 24); !strings.Contains(got, "대기중") || !strings.Contains(got, "오류: gossh -pm: 실패") {
		t.Errorf("대기중/오류 줄 없음\n%s", got)
	}
}

func TestTUIElapsedColumn(t *testing.T) {
	snap := tuiSnap()
	v := newTestView(80, 40)
	got := render(*v, snap, 80, 40)
	// c001: 설치중, 5분 전부터 -> 5m. 지연 s001: 61분 -> 1h01m. 설치완료는 경과 비움.
	if !strings.Contains(got, "c001") || !strings.Contains(got, "5m") || !strings.Contains(got, "1h01m") {
		t.Errorf("경과 표시가 이상함\n%s", got)
	}
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "c1001") && (strings.Contains(l, "m ") || strings.Contains(l, "h0")) && strings.Contains(l, "설치완료") {
			t.Errorf("완료 서버에 경과가 있음: %q", l)
		}
	}
}

func TestParseKeys(t *testing.T) {
	cases := []struct {
		in   string
		want []Key
	}{
		{"\x1b[A\x1b[B", []Key{{Kind: kUp}, {Kind: kDown}}},
		{"\x1bOA", []Key{{Kind: kUp}}},
		{"\x1b[5~\x1b[6~", []Key{{Kind: kPgUp}, {Kind: kPgDn}}},
		{"\x1b[H\x1b[F", []Key{{Kind: kHome}, {Kind: kEnd}}},
		{"\x1b[1~\x1b[4~\x1b[7~\x1b[8~", []Key{{Kind: kHome}, {Kind: kEnd}, {Kind: kHome}, {Kind: kEnd}}},
		{"\x1b", []Key{{Kind: kEsc}}},
		{"\r\n", []Key{{Kind: kEnter}}},
		{"\r", []Key{{Kind: kEnter}}},
		{"a\x7f", []Key{{Kind: kRune, R: 'a'}, {Kind: kBackspace}}},
		{"\x03", []Key{{Kind: kRune, R: 3}}},
		{"한", []Key{{Kind: kRune, R: '한'}}},
		{"\x1b[", nil},
	}
	for _, c := range cases {
		got := parseKeys([]byte(c.in))
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%q -> %v, want %v", c.in, got, c.want)
		}
	}
}

func TestKeySequences(t *testing.T) {
	snap := tuiSnap()
	v := newTestView(80, 24)

	// 필터
	press(v, snap, "3")
	if v.Filter != StatusDisk || v.Scroll != 0 {
		t.Fatalf("3 -> 필터 %v", v.Filter)
	}
	v.prepare(snap, 80)
	if len(v.rows) != 3 {
		t.Fatalf("디스크미인식 필터 행 수 %d", len(v.rows))
	}
	press(v, snap, "0")
	v.prepare(snap, 80)
	if v.Filter != 0 || len(v.rows) != snap.Total {
		t.Fatalf("0 -> 해제 안 됨: %v %d", v.Filter, len(v.rows))
	}

	// 검색: 입력 중 q 는 글자, Enter 적용, Esc 취소, 대소문자 무시
	if act := press(v, snap, "/", "q", "S", "1"); act != actNone || !v.Searching || v.SearchBuf != "qS1" {
		t.Fatalf("검색 입력: act=%v searching=%v buf=%q", act, v.Searching, v.SearchBuf)
	}
	press(v, snap, "\x7f", "\x7f", "\x7f", "s1", "\r")
	if v.Searching || v.Search != "s1" {
		t.Fatalf("검색 적용: %v %q", v.Searching, v.Search)
	}
	v.prepare(snap, 80)
	if len(v.rows) != 6 { // s1001~s1006
		t.Fatalf("검색 결과 %d", len(v.rows))
	}
	press(v, snap, "/", "zz", "\x1b")
	if v.Searching || v.Search != "s1" {
		t.Fatalf("Esc 취소 후 검색어가 바뀜: %q", v.Search)
	}
	press(v, snap, "/", "\x7f\x7f", "\r") // 검색어 지우고 적용
	if v.Search != "" {
		t.Fatalf("검색 해제 안 됨: %q", v.Search)
	}
	// 한글 backspace
	press(v, snap, "/", "한글", "\x7f")
	if v.SearchBuf != "한" {
		t.Fatalf("한글 backspace: %q", v.SearchBuf)
	}
	press(v, snap, "\x1b")

	// 스크롤 (24줄 화면: 표 행 수 = 24 - 헤더 - 푸터 - 2)
	press(v, snap, "\x1b[B", "\x1b[B")
	if v.Scroll != 2 {
		t.Fatalf("Down x2 -> %d", v.Scroll)
	}
	press(v, snap, "\x1b[A", "\x1b[A", "\x1b[A")
	if v.Scroll != 0 {
		t.Fatalf("위로 넘어감 -> %d", v.Scroll)
	}
	press(v, snap, "\x1b[F")
	end := v.Scroll
	if end <= 0 {
		t.Fatalf("End -> %d", end)
	}
	press(v, snap, "\x1b[B", "\x1b[6~")
	if v.Scroll != end {
		t.Fatalf("끝에서 더 내려감: %d != %d", v.Scroll, end)
	}
	press(v, snap, "\x1b[5~")
	if v.Scroll >= end || v.Scroll < 0 {
		t.Fatalf("PgUp -> %d", v.Scroll)
	}
	press(v, snap, "\x1b[H")
	if v.Scroll != 0 {
		t.Fatalf("Home -> %d", v.Scroll)
	}

	// 종료
	if press(newTestView(80, 24), snap, "q") != actQuit {
		t.Fatal("q 로 종료 안 됨")
	}
	if press(newTestView(80, 24), snap, "\x03") != actQuit {
		t.Fatal("Ctrl+C 로 종료 안 됨")
	}
	if newTestView(80, 24).handleKey(Key{Kind: kEOF}, snap) != actQuit {
		t.Fatal("EOF 로 종료 안 됨")
	}
	v = newTestView(80, 24)
	press(v, snap, "c")
	if press(v, snap, "q") != actQuit {
		t.Fatal("패널 열린 상태에서 q 종료 안 됨")
	}
}

func TestPanelToggle(t *testing.T) {
	snap := tuiSnap()
	v := newTestView(80, 24)

	if act := press(v, snap, "c"); act != actCheck || v.Panel != 'c' || !v.Busy {
		t.Fatalf("c 열기: act=%v panel=%c busy=%v", act, v.Panel, v.Busy)
	}
	if got := render(*v, snap, 80, 24); !strings.Contains(got, "완료 서버 ping 확인중...") {
		t.Fatalf("확인중 표시 없음\n%s", got)
	}
	// 확인 중에 닫고 다시 열어도 확인을 또 시작하지 않는다
	if act := press(v, snap, "c"); act != actNone || v.Panel != 0 {
		t.Fatalf("c 닫기: act=%v panel=%c", act, v.Panel)
	}
	if act := press(v, snap, "c"); act != actNone || v.Panel != 'c' || !v.Busy {
		t.Fatalf("확인 중 다시 열기: act=%v", act)
	}
	v.checkDone(nil)
	got := render(*v, snap, 80, 24)
	if v.Busy || strings.Contains(got, "확인중") || !strings.Contains(got, "40대 중 14대 설치완료") {
		t.Fatalf("확인 끝난 뒤 설치현황이 안 나옴\n%s", got)
	}
	// 확인이 끝난 뒤 다시 열면 새 확인을 시작한다
	press(v, snap, "c")
	if act := press(v, snap, "c"); act != actCheck {
		t.Fatalf("다시 열 때 확인 시작 안 함: %v", act)
	}
	v.checkDone(fmt.Errorf("raw socket 실패"))
	if got := render(*v, snap, 80, 24); !strings.Contains(got, "ping 확인 실패: raw socket 실패") {
		t.Fatalf("실패 이유가 안 보임\n%s", got)
	}

	// 다른 패널 키로 전환, 같은 키로 닫기
	press(v, snap, "r")
	if v.Panel != 'r' {
		t.Fatalf("r 전환: %c", v.Panel)
	}
	if got := render(*v, snap, 80, 24); !strings.Contains(got, "안녕하세요 sds 담당자님") {
		t.Fatalf("점검요청 코멘트 없음\n%s", got)
	}
	press(v, snap, "r")
	if v.Panel != 0 {
		t.Fatal("r 로 닫기 실패")
	}
	// d, l 은 열 때 조회 시작을 요청하고 같은 키로 닫는다
	for _, k := range []string{"d", "l"} {
		if act := press(v, snap, k); act != actReport || v.reportReq != rune(k[0]) || v.Panel != rune(k[0]) {
			t.Fatalf("%s 열기: act=%v req=%c panel=%c", k, act, v.reportReq, v.Panel)
		}
		v.reportReq = 0
		if act := press(v, snap, k); act != actNone || v.Panel != 0 {
			t.Fatalf("%s 닫기: act=%v panel=%c", k, act, v.Panel)
		}
	}
	// 숫자·/ 는 패널을 닫고 동작, Esc 는 닫기
	press(v, snap, "r", "2")
	if v.Panel != 0 || v.Filter != StatusDelayed {
		t.Fatalf("패널 중 필터: panel=%c filter=%v", v.Panel, v.Filter)
	}
	press(v, snap, "r", "/")
	if v.Panel != 0 || !v.Searching {
		t.Fatalf("패널 중 검색")
	}
	press(v, snap, "\x1b", "r", "\x1b")
	if v.Panel != 0 {
		t.Fatal("Esc 로 패널 닫기 실패")
	}
}

func TestPanelScrollAndProvider(t *testing.T) {
	snap := tuiSnap()
	v := newTestView(60, 12)
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("줄 %02d", i))
	}
	v.PanelText = func(kind rune, s Snapshot) string {
		if kind == 'd' {
			return strings.Join(lines, "\n")
		}
		return defaultPanelText(kind, s)
	}
	press(v, snap, "d", "\x1b[B")
	if v.PanelScroll != 1 {
		t.Fatalf("패널 Down -> %d", v.PanelScroll)
	}
	press(v, snap, "\x1b[F")
	got := render(*v, snap, 60, 12)
	if !strings.Contains(got, "줄 49") || strings.Contains(got, "줄 00") || !strings.Contains(got, "[d] DHCP") {
		t.Fatalf("패널 끝 스크롤\n%s", got)
	}
	if v.Scroll != 0 {
		t.Fatal("패널 스크롤이 표 스크롤을 건드림")
	}
	press(v, snap, "\x1b[H")
	if v.PanelScroll != 0 {
		t.Fatal("Home")
	}
}

func TestWrapText(t *testing.T) {
	long := strings.Repeat("가", 40) // 폭 80
	for _, l := range wrapText("a "+long+" b\n\n"+strings.Repeat("host1 ", 30), 30) {
		if strWidth(l) > 31 {
			t.Errorf("폭 초과 %d: %q", strWidth(l), l)
		}
	}
}

func TestRenderCleansControlChars(t *testing.T) {
	snap := tuiSnap()
	snap.Records = append([]Record{{Name: "ev\x1b[31mil", Vendor: "D", Status: StatusInstalling, Reason: "x\x1b]0;t\a"}}, snap.Records...)
	got := render(*newTestView(80, 24), snap, 80, 24)
	if strings.ContainsRune(got, 0x1b) {
		t.Fatal("제어문자가 그대로 나감")
	}
}

// ---- 성능 ----

func bigSnap(n int) Snapshot {
	cfg := defaultConfig()
	recs := make([]Record, n)
	for i := range recs {
		st := Status(1 + i%7)
		recs[i] = Record{Name: fmt.Sprintf("node%05d", i), Vendor: "D", Status: st, Reason: "OS 설치중"}
		if st == StatusInstalling || st == StatusDelayed {
			recs[i].FirstInstallingAt = tNow.Add(-time.Duration(i) * time.Second)
		}
	}
	return *buildSnapshot(cfg, recs, 1, false, tNow, "")
}

func TestRenderUnder50ms(t *testing.T) {
	snap := bigSnap(5000)
	v := newTestView(80, 40)
	v.prepare(snap, 80)
	press(v, snap, "\x1b[B") // 한 번 데워 둔다
	steps := map[string]func(){
		"스크롤 키+렌더": func() { press(v, snap, "\x1b[6~"); render(*v, snap, 80, 40) },
		"필터 키+렌더":  func() { press(v, snap, "3"); v.prepare(snap, 80); render(*v, snap, 80, 40) },
		"검색 적용+렌더": func() { press(v, snap, "/", "node04", "\r"); v.prepare(snap, 80); render(*v, snap, 80, 40) },
		"c 패널+렌더":  func() { press(v, snap, "c"); v.checkDone(nil); v.prepare(snap, 80); render(*v, snap, 80, 40) },
	}
	for _, name := range []string{"스크롤 키+렌더", "필터 키+렌더", "검색 적용+렌더", "c 패널+렌더"} {
		start := time.Now()
		steps[name]()
		if d := time.Since(start); d > 50*time.Millisecond {
			t.Errorf("%s: %v (50ms 초과)", name, d)
		}
	}
}

func BenchmarkRender5000(b *testing.B) {
	snap := bigSnap(5000)
	v := newTestView(80, 40)
	v.prepare(snap, 80)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		press(v, snap, "\x1b[B")
		_ = render(*v, snap, 80, 40)
	}
}

// ---- 루프 ----

type fakeSrc struct {
	mu      sync.Mutex
	snap    *Snapshot
	updates chan struct{}
	checks  int32
	release chan struct{} // CheckCompleted 가 이 채널을 기다린다(nil 이면 바로 끝)
	boom    bool
}

func newFakeSrc() *fakeSrc {
	s := tuiSnap()
	return &fakeSrc{snap: &s, updates: make(chan struct{}, 1)}
}

func (f *fakeSrc) Snapshot() *Snapshot {
	if f.boom {
		panic("snapshot 패닉")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}
func (f *fakeSrc) Updates() <-chan struct{} { return f.updates }
func (f *fakeSrc) Run(ctx context.Context)  { <-ctx.Done() }
func (f *fakeSrc) CheckCompleted(ctx context.Context) error {
	atomic.AddInt32(&f.checks, 1)
	if f.release != nil {
		<-f.release
	}
	f.mu.Lock()
	s := *f.snap
	s.Counts = map[Status]int{}
	for k, v := range f.snap.Counts {
		s.Counts[k] = v
	}
	f.snap = &s
	f.mu.Unlock()
	return nil
}

type syncBuf struct {
	mu     sync.Mutex
	b      bytes.Buffer
	writes int
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	return s.b.Write(p)
}
func (s *syncBuf) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }
func (s *syncBuf) Writes() int    { s.mu.Lock(); defer s.mu.Unlock(); return s.writes }

type loopEnv struct {
	env      tuiEnv
	out      *syncBuf
	in       *io.PipeWriter
	tick     chan time.Time
	resize   chan os.Signal
	sig      chan os.Signal
	restored *int32
}

func newLoopEnv() *loopEnv {
	pr, pw := io.Pipe()
	out := &syncBuf{}
	tick := make(chan time.Time)
	resize := make(chan os.Signal, 1)
	sig := make(chan os.Signal, 1)
	var restored int32
	le := &loopEnv{out: out, in: pw, tick: tick, resize: resize, sig: sig, restored: &restored}
	le.env = tuiEnv{
		In:      pr,
		Out:     out,
		Size:    func() (int, int) { return 80, 24 },
		MakeRaw: func() (func(), error) { return func() { atomic.AddInt32(&restored, 1) }, nil },
		Tick:    func(d time.Duration) (<-chan time.Time, func()) { return tick, func() {} },
		Now:     func() time.Time { return tNow },
		Signals: sig,
		Resize:  resize,
	}
	return le
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("시간 초과: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func runLoopAsync(src source, le *loopEnv) <-chan int {
	res := make(chan int, 1)
	go func() { res <- runTUILoop(src, le.env, "", nil) }()
	return res
}

func wait(t *testing.T, res <-chan int) int {
	t.Helper()
	select {
	case code := <-res:
		return code
	case <-time.After(3 * time.Second):
		t.Fatal("루프가 끝나지 않음")
		return -1
	}
}

func TestLoopCheckFlowAndRestore(t *testing.T) {
	src := newFakeSrc()
	src.release = make(chan struct{})
	le := newLoopEnv()
	res := runLoopAsync(src, le)
	defer le.in.Close()

	waitFor(t, "첫 화면", func() bool { return strings.Contains(le.out.String(), "install_mn") })
	le.in.Write([]byte("c"))
	waitFor(t, "확인중 표시", func() bool { return strings.Contains(le.out.String(), "완료 서버 ping 확인중...") })
	// 확인 중에도 키가 즉시 먹는다: 닫았다가 다시 연다
	n := le.out.Writes()
	le.in.Write([]byte("c"))
	waitFor(t, "닫기 반영", func() bool { return le.out.Writes() > n })
	le.in.Write([]byte("c"))
	waitFor(t, "다시 열기 반영", func() bool { return le.out.Writes() > n+1 })
	if c := atomic.LoadInt32(&src.checks); c != 1 {
		t.Fatalf("확인이 %d 번 시작됨", c)
	}
	close(src.release)
	waitFor(t, "확인 끝난 설치현황", func() bool { return strings.Contains(le.out.String(), "40대 중 14대 설치완료") })
	le.in.Write([]byte("q"))
	if code := wait(t, res); code != 0 {
		t.Fatalf("종료 코드 %d", code)
	}
	s := le.out.String()
	if !strings.HasPrefix(s, enterScreen) || !strings.HasSuffix(s, leaveScreen) {
		t.Fatalf("화면 전환 문자열이 앞뒤에 없음: %q ... %q", s[:20], s[len(s)-20:])
	}
	if atomic.LoadInt32(le.restored) != 1 {
		t.Fatalf("터미널 복구 %d 번", atomic.LoadInt32(le.restored))
	}
}

func TestLoopSignalAndEOF(t *testing.T) {
	for _, how := range []string{"signal", "eof"} {
		src := newFakeSrc()
		le := newLoopEnv()
		res := runLoopAsync(src, le)
		waitFor(t, "첫 화면", func() bool { return strings.Contains(le.out.String(), "install_mn") })
		if how == "signal" {
			le.sig <- syscallTerm{}
		} else {
			le.in.Close()
		}
		if code := wait(t, res); code != 0 {
			t.Fatalf("%s: 종료 코드 %d", how, code)
		}
		if atomic.LoadInt32(le.restored) != 1 || !strings.HasSuffix(le.out.String(), leaveScreen) {
			t.Fatalf("%s: 터미널 복구 안 됨", how)
		}
		le.in.Close()
	}
}

type syscallTerm struct{}

func (syscallTerm) String() string { return "terminated" }
func (syscallTerm) Signal()        {}

func TestLoopPanicRestores(t *testing.T) {
	src := newFakeSrc()
	src.boom = true
	le := newLoopEnv()
	defer le.in.Close()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("패닉이 전파되어야 함")
			}
		}()
		runTUILoop(src, le.env, "", nil)
	}()
	if atomic.LoadInt32(le.restored) != 1 || !strings.HasSuffix(le.out.String(), leaveScreen) {
		t.Fatal("패닉 뒤 터미널 복구 안 됨")
	}
}

func TestLoopSkipsIdenticalFrame(t *testing.T) {
	src := newFakeSrc()
	le := newLoopEnv()
	res := runLoopAsync(src, le)
	defer le.in.Close()
	waitFor(t, "첫 화면", func() bool { return le.out.Writes() >= 2 }) // enterScreen + 첫 프레임
	n := le.out.Writes()
	le.tick <- tNow // 받았다는 것은 앞 그리기가 끝났다는 뜻
	le.tick <- tNow
	le.tick <- tNow
	if got := le.out.Writes(); got != n {
		t.Fatalf("바뀐 것이 없는데 다시 씀: %d -> %d", n, got)
	}
	le.resize <- syscallTerm{} // 강제 다시 그림
	waitFor(t, "resize 다시 그림", func() bool { return le.out.Writes() > n })
	if !strings.Contains(le.out.String(), "\x1b[2J") {
		t.Fatal("resize 때 화면을 지우지 않음")
	}
	le.in.Write([]byte("q"))
	wait(t, res)
}

// withChecks 는 완료 서버 일부에 점검 결과를 채운 스냅샷 복사본이다(l 패널 골든용).
func withChecks(s Snapshot) *Snapshot {
	recs := append([]Record(nil), s.Records...)
	n := 0
	for i := range recs {
		if !ldapDone(recs[i].Status) {
			continue
		}
		switch n % 4 {
		case 0:
			recs[i].Check = Check{LDAP: "OK", DNSNTP: "OK", Autofs: "OK", HT: "ON"}
		case 1:
			recs[i].Check = Check{LDAP: "OK", DNSNTP: "OK", Autofs: "FAIL", HT: "OFF"}
		case 2:
			recs[i].Check = Check{LDAP: "FAIL", DNSNTP: "FAIL", Autofs: "FAIL", HT: "-", Note: "인프라 불일치 (dns+ntp=zxcv ldap=zxcv appl=qwer)"}
		}
		n++
	}
	out := s
	out.Records = recs
	return &out
}

// reportSrc 는 reportSource 를 구현한 가짜 source 다.
type reportSrc struct {
	*fakeSrc
	release chan struct{}
	calls   int32
}

func (r *reportSrc) Report(kind rune) string {
	atomic.AddInt32(&r.calls, 1)
	<-r.release
	return "결과 " + string(kind)
}
func (r *reportSrc) ReportBusy(kind rune) string { return "조회중 " + string(kind) }

func TestLoopReportPanels(t *testing.T) {
	src := &reportSrc{fakeSrc: newFakeSrc(), release: make(chan struct{})}
	le := newLoopEnv()
	res := runLoopAsync(src, le)
	defer le.in.Close()
	waitFor(t, "첫 화면", func() bool { return strings.Contains(le.out.String(), "install_mn") })
	le.in.Write([]byte("d"))
	waitFor(t, "조회중 표시", func() bool { return strings.Contains(le.out.String(), "조회중 d") })
	// 조회 중에도 키가 즉시 먹고, 진행 중인 패널은 다시 열어도 조회를 또 시작하지 않는다
	n := le.out.Writes()
	le.in.Write([]byte("d"))
	waitFor(t, "닫기 반영", func() bool { return le.out.Writes() > n })
	le.in.Write([]byte("d"))
	waitFor(t, "다시 열기 반영", func() bool { return le.out.Writes() > n+1 })
	if c := atomic.LoadInt32(&src.calls); c != 1 {
		t.Fatalf("조회가 %d 번 시작됨", c)
	}
	// l 은 별도 패널이라 d 가 진행 중이어도 시작된다
	le.in.Write([]byte("l"))
	waitFor(t, "l 조회중", func() bool { return strings.Contains(le.out.String(), "조회중 l") })
	close(src.release)
	waitFor(t, "l 결과", func() bool { return strings.Contains(le.out.String(), "결과 l") })
	le.in.Write([]byte("q"))
	if code := wait(t, res); code != 0 {
		t.Fatalf("종료 코드 %d", code)
	}
	if c := atomic.LoadInt32(&src.calls); c != 2 {
		t.Fatalf("조회 %d 번", c)
	}
}
