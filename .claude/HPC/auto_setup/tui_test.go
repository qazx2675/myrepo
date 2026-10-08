// tui_test.go - TUI 루프 테스트: 키 파싱, 화면1→2→수동 실행 요청 시퀀스, 종료 시 raw 복원(가짜 tty), 갱신 tick, plain
package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// ---- 가짜 원본·가짜 tty ----

type reqCall struct {
	kind    string
	payload map[string]string
}

type fakeSrc struct {
	snap     Snapshot
	err      error
	reqErr   error
	reqs     []reqCall
	local    bool
	calls    int
	panicOn  int // N 번째 Snapshot 호출에서 panic (0 = 안 함)
	nextSnap func(n int) Snapshot
	codes    map[string]string // Code(n) 응답
}

func (f *fakeSrc) Snapshot() (Snapshot, error) {
	f.calls++
	if f.panicOn > 0 && f.calls == f.panicOn {
		panic("boom")
	}
	if f.nextSnap != nil {
		return f.nextSnap(f.calls), f.err
	}
	return f.snap, f.err
}

func (f *fakeSrc) Request(kind string, payload map[string]string) error {
	cp := map[string]string{}
	for k, v := range payload {
		cp[k] = v
	}
	f.reqs = append(f.reqs, reqCall{kind, cp})
	return f.reqErr
}

func (f *fakeSrc) Code(n string) (string, error) {
	if t, ok := f.codes[n]; ok {
		return t, nil
	}
	return "", errors.New("미사용")
}
func (f *fakeSrc) Local() bool { return f.local }

type rig struct {
	out      bytes.Buffer
	raws     int
	restored int
	ticks    []time.Duration
}

// newRig: in 을 키 입력으로 쓰는 가짜 환경. tick 은 절대 울리지 않는다.
func newRig(in io.Reader) (tuiEnv, *rig) {
	r := &rig{}
	never := make(chan time.Time)
	return tuiEnv{
		In:   in,
		Out:  &r.out,
		Size: func() (int, int) { return 100, 24 },
		MakeRaw: func() (func(), error) {
			r.raws++
			return func() { r.restored++ }, nil
		},
		Tick: func(d time.Duration) (<-chan time.Time, func()) {
			r.ticks = append(r.ticks, d)
			return never, func() {}
		},
		Now:   func() time.Time { return time.Unix(tsNow, 0) },
		Color: false,
	}, r
}

// twoGroupSnap: a.yml(진행 중) / b.yml(전부 완료)
func twoGroupSnap() Snapshot {
	return tsSnap(true, tsJob("J1", "user1", false,
		tsGroup("a.yml", "ib", "rhel8", "pxe", "y", tsHost("a1", StageDone, 10, ""), tsHost("a2", StageInstalling, 500, "")),
		tsGroup("b.yml", "ib", "rhel8", "pxe", "y", tsHost("b1", StageDone, 10, ""), tsHost("b2", StageDone, 20, ""))))
}

func runKeys(t *testing.T, src *fakeSrc, keys string) *rig {
	t.Helper()
	env, r := newRig(strings.NewReader(keys))
	if code := runTUILoop(src, env); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	return r
}

func assertRestored(t *testing.T, r *rig) {
	t.Helper()
	if r.raws != 1 || r.restored != 1 {
		t.Errorf("raw=%d restored=%d (1/1 이어야 함)", r.raws, r.restored)
	}
	o := r.out.String()
	if !strings.HasPrefix(o, enterScreen) || !strings.HasSuffix(o, leaveScreen) {
		t.Errorf("대체 화면 진입/복원 시퀀스 없음")
	}
}

// lastFrame: 마지막으로 그려진 화면
func lastFrame(r *rig) string {
	o := r.out.String()
	i := strings.LastIndex(o, "\x1b[H")
	if i < 0 {
		return ""
	}
	return o[i:]
}

// ---- 키 파싱 ----

func TestParseKeys(t *testing.T) {
	type e = keyEv
	cases := []struct {
		in   string
		want []keyEv
	}{
		{"\x1b[A\x1b[B\x1b[C\x1b[D", []e{{k: kUp}, {k: kDown}, {k: kRight}, {k: kLeft}}},
		{"\x1bOA\x1bOB\x1bOC\x1bOD", []e{{k: kUp}, {k: kDown}, {k: kRight}, {k: kLeft}}},
		{"\x1b", []e{{k: kEsc}}},
		{"\x1bq", []e{{k: kEsc}, {k: kRune, r: 'q'}}},
		{"\r", []e{{k: kEnter}}},
		{"\r\n", []e{{k: kEnter}}},
		{"\n", []e{{k: kEnter}}},
		{"jk?", []e{{k: kRune, r: 'j'}, {k: kRune, r: 'k'}, {k: kRune, r: '?'}}},
		{"\x03", []e{{k: kRune, r: runeCtrlC}}},
		{"\x1a", []e{{k: kRune, r: runeCtrlZ}}},
		{"\x1b[1;5A", []e{{k: kUp}}},
		{"\x1b[3~", nil},
		{"\x1b[", nil},
		{"가", []e{{k: kRune, r: '가'}}},
		{"\x1b[Bx", []e{{k: kDown}, {k: kRune, r: 'x'}}},
	}
	for _, c := range cases {
		got := parseKeys([]byte(c.in))
		if len(got) != len(c.want) {
			t.Errorf("parseKeys(%q)=%v want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseKeys(%q)[%d]=%v want %v", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// ---- 키 시퀀스 ----

func TestKeySequenceManualRun(t *testing.T) {
	src := &fakeSrc{snap: twoGroupSnap(), local: true}
	// 화면1 → ↓(b.yml) → Enter → f → c → y
	r := runKeys(t, src, "\x1b[B\rfcy")
	assertRestored(t, r)
	if len(src.reqs) != 1 {
		t.Fatalf("요청 %d 건: %+v", len(src.reqs), src.reqs)
	}
	got := src.reqs[0]
	if got.kind != "manual-run" || len(got.payload) != 2 || got.payload["jobid"] != "J1" || got.payload["yml"] != "b.yml" {
		t.Errorf("요청 인자: %+v", got)
	}
	if !strings.Contains(r.out.String(), "수동 실행 [설정체크 + 설정수정] - b.yml") {
		t.Errorf("요청됨 안내 없음")
	}
	if !strings.Contains(r.out.String(), "[y/n]") {
		t.Errorf("확인 질문 없음")
	}
}

func TestKeySequenceIncompleteGroupAllowed(t *testing.T) {
	src := &fakeSrc{snap: twoGroupSnap(), local: true}
	r := runKeys(t, src, "\rcy") // a.yml (미완료): c → 확인(미완료 포함 안내) → y → 요청
	if len(src.reqs) != 1 || src.reqs[0].kind != ReqManualRun || src.reqs[0].payload["yml"] != "a.yml" {
		t.Fatalf("미완료 그룹도 요청되어야 함: %+v", src.reqs)
	}
	if strings.Contains(r.out.String(), "수동 실행 불가") || !strings.Contains(r.out.String(), "미완료") || !strings.Contains(r.out.String(), "[y/n]") {
		t.Errorf("확인 질문에 미완료 포함 안내가 있어야 함")
	}
	assertRestored(t, r)
}

func TestKeySequenceCancelAndRepeat(t *testing.T) {
	src := &fakeSrc{snap: twoGroupSnap(), local: true}
	runKeys(t, src, "\x1b[B\rcn")
	if len(src.reqs) != 0 {
		t.Fatalf("n 인데 요청됨")
	}
	runKeys(t, src, "\x1b[B\r\x1bcy") // Esc 는 화면 1 로 복귀 → c, y 는 화면 1 에서 무시
	if len(src.reqs) != 0 {
		t.Fatalf("화면 1 에서 요청됨")
	}
	src2 := &fakeSrc{snap: twoGroupSnap(), local: true}
	r := runKeys(t, src2, "\x1b[B\rcyqc") // 두 번째 c 는 재요청 차단
	if len(src2.reqs) != 1 || !strings.Contains(r.out.String(), "방금 요청했습니다") {
		t.Fatalf("중복 요청 차단: %d 건", len(src2.reqs))
	}
}

func TestKeyManualAlreadyActiveAndRequestError(t *testing.T) {
	s := twoGroupSnap()
	s.Jobs[0].Manual = []SnapManual{{Yml: "b.yml", State: "queued", Requested: tsNow - 5}}
	src := &fakeSrc{snap: s, local: true}
	r := runKeys(t, src, "\x1b[B\rc")
	if len(src.reqs) != 0 || !strings.Contains(r.out.String(), "이미 이 그룹의 수동 run") {
		t.Errorf("활성 수동 run 이 있는데 c 허용")
	}
	src = &fakeSrc{snap: twoGroupSnap(), local: true, reqErr: errors.New("전송 실패")}
	r = runKeys(t, src, "\x1b[B\rcy")
	if len(src.reqs) != 1 || !strings.Contains(r.out.String(), "[X] 요청 실패: 전송 실패") {
		t.Errorf("요청 오류 표시")
	}
}

func TestKeyNavigation(t *testing.T) {
	two := tsSnap(true,
		tsJob("J1", "u1", false, tsGroup("a.yml", "", "", "", "", tsHost("a1", StageQueued, 0, "")), tsGroup("b.yml", "", "", "", "", tsHost("b1", StageQueued, 0, ""))),
		tsJob("J2", "u2", false, tsGroup("c.yml", "", "", "", "", tsHost("c1", StageQueued, 0, ""))))
	src := &fakeSrc{snap: two, local: true}
	r := runKeys(t, src, "\x1b[C\r") // → 로 J2 첫 그룹, Enter
	if !strings.Contains(lastFrame(r), "job J2 / c.yml") {
		t.Errorf("→ 후 Enter 로 J2 상세가 아님")
	}
	src = &fakeSrc{snap: two, local: true}
	r = runKeys(t, src, "jj\r") // 아래로 2칸 → c.yml, Enter
	if !strings.Contains(lastFrame(r), "job J2 / c.yml") {
		t.Errorf("j j Enter")
	}
	r = runKeys(t, &fakeSrc{snap: two, local: true}, "jj\r\x1b[D") // ← 로 복귀
	if !strings.Contains(lastFrame(r), "> ") || strings.Contains(lastFrame(r), "호스트  ") {
		t.Errorf("← 로 화면 1 복귀:\n%s", lastFrame(r))
	}
	refs := flatGroups(two)
	for _, c := range []struct {
		row  int
		next bool
		want int
	}{{0, true, 2}, {1, true, 2}, {2, true, 2}, {2, false, 0}, {1, false, 0}, {0, false, 0}} {
		if got := jumpJob(refs, c.row, c.next); got != c.want {
			t.Errorf("jumpJob(row=%d next=%v)=%d want %d", c.row, c.next, got, c.want)
		}
	}
	r = runKeys(t, &fakeSrc{snap: two, local: true}, "?x") // ? 후 아무 키로 닫힘
	if !strings.Contains(r.out.String(), "도움말") || strings.Contains(lastFrame(r), "도움말 -") {
		t.Errorf("도움말 표시/닫힘")
	}
}

func TestKeyFilterAndHostCursor(t *testing.T) {
	src := &fakeSrc{snap: sampleStuck(), local: true}
	r := runKeys(t, src, "\rjjf")
	f := lastFrame(r)
	if !strings.Contains(f, "[필터: 정체·실패만]") || strings.Contains(f, "web001") || !strings.Contains(f, "> web002") {
		t.Errorf("필터 후 마지막 화면:\n%s", f)
	}
}

// ---- 복원 보장 ----

func TestRestoreOnQuitKeys(t *testing.T) {
	for _, in := range []string{"q", "\x1b", "\x03q", "\x1aq", ""} { // 화면 1 q / Esc / Ctrl-C 후 q / Ctrl-Z 후 q / 입력 EOF
		src := &fakeSrc{snap: twoGroupSnap(), local: true}
		assertRestored(t, runKeys(t, src, in))
	}
}

// Ctrl+C·Ctrl+Z 는 종료하지 않고 안내만 한다 (종료는 q)
func TestCtrlCZDoNotQuit(t *testing.T) {
	for _, k := range []string{"\x03", "\x1a"} {
		env, r := newRig(strings.NewReader(k + "q"))
		src := &fakeSrc{snap: twoGroupSnap(), local: true}
		if code := runTUILoop(src, env); code != 0 {
			t.Fatalf("exit=%d", code)
		}
		if !strings.Contains(r.out.String(), "종료는 q 를 누르세요") {
			t.Errorf("%q: 안내 문구가 그려져야 함", k)
		}
	}
}

func TestRestoreOnSignal(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	env, r := newRig(pr)
	sig := make(chan os.Signal, 1)
	sig <- os.Interrupt
	env.Signals = sig
	if code := runTUILoop(&fakeSrc{snap: twoGroupSnap(), local: true}, env); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	assertRestored(t, r)
}

func TestRestoreOnPanic(t *testing.T) {
	for _, n := range []int{1, 2} { // 첫 갱신 중 / 두 번째(r 키) 갱신 중 패닉
		src := &fakeSrc{snap: twoGroupSnap(), local: true, panicOn: n}
		env, r := newRig(strings.NewReader("r"))
		func() {
			defer func() {
				if recover() == nil {
					t.Error("패닉이 전파되어야 함")
				}
			}()
			runTUILoop(src, env)
		}()
		if r.raws != 1 || r.restored != 1 || !strings.HasSuffix(r.out.String(), leaveScreen) {
			t.Errorf("패닉 후 복원: raw=%d restored=%d", r.raws, r.restored)
		}
	}
}

func TestRawFailureFallsBackToPlain(t *testing.T) {
	src := &fakeSrc{snap: twoGroupSnap(), local: true}
	env, r := newRig(strings.NewReader(""))
	env.MakeRaw = func() (func(), error) { return nil, errors.New("no tty") }
	if code := runTUILoop(src, env); code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if strings.Contains(r.out.String(), "\x1b") || !strings.Contains(r.out.String(), "a.yml") {
		t.Errorf("raw 실패 시 plain 출력이어야 함: %q", r.out.String())
	}
}

// ---- 갱신 ----

func TestAutoRefreshTickAndInterval(t *testing.T) {
	for _, local := range []bool{true, false} {
		pr, pw := io.Pipe()
		env, r := newRig(pr)
		tick := make(chan time.Time) // 비버퍼: 보내는 쪽이 수신 확인까지 대기
		env.Tick = func(d time.Duration) (<-chan time.Time, func()) {
			r.ticks = append(r.ticks, d)
			return tick, func() {}
		}
		src := &fakeSrc{local: local}
		src.nextSnap = func(n int) Snapshot {
			if n == 1 {
				return twoGroupSnap()
			}
			return sampleNoJob()
		}
		done := make(chan int)
		go func() { done <- runTUILoop(src, env) }()
		tick <- time.Time{}
		pw.Write([]byte("q"))
		if code := <-done; code != 0 {
			t.Fatalf("exit=%d", code)
		}
		pw.Close()
		want := 2 * time.Second
		if !local {
			want = 10 * time.Second
		}
		if len(r.ticks) != 1 || r.ticks[0] != want {
			t.Errorf("local=%v 갱신 주기 %v want %v", local, r.ticks, want)
		}
		if src.calls != 2 || !strings.Contains(r.out.String(), "진행 중인 작업 없음") {
			t.Errorf("tick 갱신: calls=%d", src.calls)
		}
		assertRestored(t, r)
	}
}

type errAfter struct {
	*fakeSrc
	after int
	calls int
}

func (e *errAfter) Snapshot() (Snapshot, error) {
	e.calls++
	if e.calls > e.after {
		return Snapshot{}, errors.New("연결 끊김")
	}
	return e.fakeSrc.Snapshot()
}

func TestRefreshErrorKeepsLastSnapshot(t *testing.T) {
	wrapped := &errAfter{fakeSrc: &fakeSrc{snap: twoGroupSnap(), local: true}, after: 1}
	env, r := newRig(strings.NewReader("r"))
	if code := runTUILoop(wrapped, env); code != 0 {
		t.Fatal("exit")
	}
	f := lastFrame(r)
	if !strings.Contains(f, "[X] 상태 갱신 실패: 연결 끊김") || !strings.Contains(f, "a.yml") {
		t.Errorf("오류 표시 + 마지막 스냅샷 유지:\n%s", f)
	}
}

func TestGroupDisappearsReturnsToOverview(t *testing.T) {
	src := &fakeSrc{local: true}
	src.nextSnap = func(n int) Snapshot {
		if n == 1 {
			return twoGroupSnap()
		}
		return sampleNoJob()
	}
	r := runKeys(t, src, "\rr") // 상세 진입 → 갱신 시 그룹 소멸
	if !strings.Contains(lastFrame(r), "진행 중인 작업 없음") {
		t.Errorf("그룹이 사라지면 화면 1 로 복귀해야 함:\n%s", lastFrame(r))
	}
}

// ---- plain ----

func TestPrintPlainNoEscape(t *testing.T) {
	src := &fakeSrc{snap: sampleStuck(), local: true}
	var b bytes.Buffer
	if code := printPlain(src, &b); code != 0 {
		t.Fatal("exit")
	}
	out := b.String()
	if strings.Contains(out, "\x1b") || !strings.Contains(out, "web.yml") || !strings.Contains(out, "[정체·실패] job J1 / web.yml") ||
		!strings.Contains(out, "web002") || strings.Contains(out, "SECRET123") {
		t.Errorf("plain 출력:\n%s", out)
	}
	if code := printPlain(&fakeSrc{err: errors.New("x"), local: true}, &b); code != 1 {
		t.Errorf("오류 시 exit=%d", code)
	}
}

func TestRunTUIPlainFlag(t *testing.T) {
	if code := runTUI(&fakeSrc{snap: sampleNormal(), local: true}, true); code != 0 {
		t.Errorf("runTUI plain exit=%d", code)
	}
}
