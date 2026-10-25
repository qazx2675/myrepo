package tui

// screen_test.go - 전체 화면 TUI 시험(auto_setup tui_test.go 방식): 가짜 tty(newRig) + 키 각본(runKeys) + 마지막 프레임(lastFrame),
// 복원 확인(assertRestored), 키 decoder, 골든 화면(80·100칸, 한글 폭), 폭 넘침·비밀값 미노출 속성.

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cross_check/internal/daemon"
	"cross_check/internal/store"
)

var updateGolden = flag.Bool("update", false, "testdata/screen_*.golden 갱신")

// ---- 가짜 tty ----

type rig struct {
	out      bytes.Buffer
	raws     int
	restored int
	ticks    []time.Duration
}

// newRig 는 in 을 키 입력으로 쓰는 가짜 환경(100x30, tick 은 울리지 않음, 시계는 t0+30초).
func newRig(in io.Reader) (Env, *rig) {
	r := &rig{}
	never := make(chan time.Time)
	return Env{
		In:   in,
		Out:  &r.out,
		Size: func() (int, int) { return 100, 30 },
		MakeRaw: func() (func(), error) {
			r.raws++
			return func() { r.restored++ }, nil
		},
		Tick: func(d time.Duration) (<-chan time.Time, func()) {
			r.ticks = append(r.ticks, d)
			return never, func() {}
		},
		Now: func() time.Time { return t0.Add(30 * time.Second) },
		Loc: time.Local,
	}, r
}

// lastFrame 은 마지막으로 그린 화면(색 코드 제거).
func lastFrame(r *rig) string {
	o := r.out.String()
	i := strings.LastIndex(o, "\x1b[H")
	if i < 0 {
		return ""
	}
	return stripANSI(o[i:])
}

// frames 는 그린 화면 전부(색 코드 제거).
func frames(r *rig) []string {
	parts := strings.Split(r.out.String(), "\x1b[H")
	var out []string
	for _, p := range parts[1:] {
		out = append(out, stripANSI(p))
	}
	return out
}

func assertRestored(t *testing.T, r *rig) {
	t.Helper()
	if r.raws != 1 || r.restored != 1 {
		t.Errorf("raw=%d restored=%d (1/1 이어야 함)", r.raws, r.restored)
	}
	o := r.out.String()
	if !strings.HasPrefix(o, enterScreen) || !strings.HasSuffix(o, leaveScreen) {
		t.Errorf("대체 화면 진입/복원 시퀀스 없음: %q … %q", head(o, 30), tail(o, 30))
	}
}

func head(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func tail(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[len(s)-n:]
}

// runUserKeys 는 사용자 화면을 키 각본으로 끝까지 돌린다.
func runUserKeys(t *testing.T, f *fakeClient, user, keys string) (*rig, int) {
	t.Helper()
	env, r := newRig(strings.NewReader(keys))
	code, ok := RunUserFull(env, f, user)
	if !ok {
		t.Fatal("raw 실패로 처리됨")
	}
	return r, code
}

func runAdminKeys(t *testing.T, f *fakeClient, keys string) (*rig, int) {
	t.Helper()
	env, r := newRig(strings.NewReader(keys))
	code, ok := RunAdminFull(env, f)
	if !ok {
		t.Fatal("raw 실패로 처리됨")
	}
	return r, code
}

const (
	kUpS    = "\x1b[A"
	kDownS  = "\x1b[B"
	kRightS = "\x1b[C"
	kLeftS  = "\x1b[D"
	kEscS   = "\x1b"
	ctrlD   = "\x04"
	pStart  = "\x1b[200~"
	pEnd    = "\x1b[201~"
)

// aliceJobFake 는 놀이터와 같은 상황: alice → bob 대기 작업 하나(대상 svr1ev01 svr1ev02 svr2ev01).
func aliceJobFake() *fakeClient {
	f := newFake()
	f.pw["alice"] = "alice123"
	f.pw["bob"] = "bob12345"
	f.pw["admin"] = "admin123"
	j := job("tnfa0001", "alice", "bob", daemon.StatusWaiting, "", "svr1ev01", "svr1ev02", "svr2ev01")
	j.Caller = "playground_worker.sh"
	f.jobs = []daemon.JobView{j}
	return f
}

// ---- 키 decoder ----

func TestKeyDecoder(t *testing.T) {
	type e = keyEv
	cases := []struct {
		in   string
		want []keyEv
	}{
		{"\x1b[A\x1b[B\x1b[C\x1b[D", []e{{k: kUp}, {k: kDown}, {k: kRight}, {k: kLeft}}},
		{"\x1b[Z\t", []e{{k: kBackTab}, {k: kRune, r: '\t'}}},
		{"\x1bOA\x1bOB\x1bOC\x1bOD", []e{{k: kUp}, {k: kDown}, {k: kRight}, {k: kLeft}}},
		{"\x1b", []e{{k: kEsc}}},
		{"\x1bq", []e{{k: kEsc}, {k: kRune, r: 'q'}}},
		{"\r", []e{{k: kEnter}}},
		{"\r\n", []e{{k: kEnter}}},
		{"\n", []e{{k: kEnter}}},
		{"\x04", []e{{k: kRune, r: runeCtrlD}}},
		{"\x03\x1a\x18\x15\t", []e{{k: kRune, r: runeCtrlC}, {k: kRune, r: runeCtrlZ}, {k: kRune, r: runeCtrlX}, {k: kRune, r: runeCtrlU}, {k: kRune, r: '\t'}}},
		{"\x7f\x08", []e{{k: kRune, r: runeBackspace}, {k: kRune, r: runeBackspace}}},
		{"\x1b[1;5A", []e{{k: kUp}}},
		{"\x1b[5~\x1b[6~", []e{{k: kPgUp}, {k: kPgDn}}},
		{"\x1b[H\x1b[F\x1b[1~\x1b[4~", []e{{k: kHome}, {k: kEnd}, {k: kHome}, {k: kEnd}}},
		{"\x1b[3~", nil},
		{"가나", []e{{k: kRune, r: '가'}, {k: kRune, r: '나'}}},
		{"전체 정리", []e{{k: kRune, r: '전'}, {k: kRune, r: '체'}, {k: kRune, r: ' '}, {k: kRune, r: '정'}, {k: kRune, r: '리'}}},
		{"\x1b[Bx", []e{{k: kDown}, {k: kRune, r: 'x'}}},
		{"\x1b[200~a\n\x1b[201~b", []e{{k: kRune, r: 'a', paste: true}, {k: kEnter, paste: true}, {k: kRune, r: 'b'}}},
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

// 읽기 경계에서 잘린 시퀀스·UTF-8 조각은 다음 읽기와 이어 붙인다
func TestKeyDecoderPartialSequences(t *testing.T) {
	var d keyDecoder
	if evs := d.feed([]byte("a\x1b[")); len(evs) != 1 || evs[0].r != 'a' {
		t.Fatalf("앞부분: %v", evs)
	}
	if evs := d.feed([]byte("A")); len(evs) != 1 || evs[0].k != kUp {
		t.Fatalf("잘린 CSI 이어 붙이기: %v", evs)
	}
	ga := []byte("가")
	if evs := d.feed(ga[:2]); len(evs) != 0 {
		t.Fatalf("UTF-8 조각은 보류: %v", evs)
	}
	if evs := d.feed(ga[2:]); len(evs) != 1 || evs[0].r != '가' {
		t.Fatalf("UTF-8 이어 붙이기: %v", evs)
	}
	// 끝의 단독 ESC 는 보류 → 다음이 '[' 면 시퀀스, 조용하면(flush) Esc
	if evs := d.feed([]byte("\x1b")); len(evs) != 0 || !d.hasLoneEsc() {
		t.Fatalf("단독 ESC 보류: %v", evs)
	}
	if evs := d.feed([]byte("[B")); len(evs) != 1 || evs[0].k != kDown {
		t.Fatalf("ESC + [B: %v", evs)
	}
	d.feed([]byte("\x1b"))
	if evs := d.flush(); len(evs) != 1 || evs[0].k != kEsc {
		t.Fatalf("flush → Esc: %v", evs)
	}
	// 붙여넣기 끝 표시가 잘려도 구간이 바르게 닫힌다
	evs := d.feed([]byte("\x1b[200~x\x1b[20"))
	evs = append(evs, d.feed([]byte("1~y"))...)
	if len(evs) != 2 || !evs[0].paste || evs[1].paste || evs[1].r != 'y' {
		t.Fatalf("붙여넣기 구간: %v", evs)
	}
	// 너무 긴 미완성 시퀀스는 버린다
	d.feed([]byte("\x1b[" + strings.Repeat("1", 40)))
	if len(d.pending) != 0 {
		t.Fatalf("긴 미완성 시퀀스 보류: %d", len(d.pending))
	}
}

// ---- 사용자 화면: 확인자 승인 흐름 ----

func TestFullApproveFlowPasteThenPassword(t *testing.T) {
	f := aliceJobFake()
	// → 확인자 구역, Enter(승인 화면), 붙여넣기(탭·공백·줄바꿈 섞어서), Ctrl+D, 틀린 패스워드, 맞는 패스워드, Enter(목록), q
	keys := "\r" + "svr2ev01\tsvr1ev01\n svr1ev02 \n" + ctrlD + "wrongpw\r" + "bob12345\r" + "\r" + "q"
	r, code := runUserKeys(t, f, "bob", keys)
	assertRestored(t, r)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if f.jobs[0].Status != daemon.StatusApproved {
		t.Fatalf("승인되어야 함: %+v", f.jobs[0])
	}
	if len(f.pasted) != 2 || !strings.Contains(f.pasted[1], "svr2ev01\tsvr1ev01") {
		t.Fatalf("붙여넣은 글 그대로 전달: %q", f.pasted)
	}
	all := strings.Join(frames(r), "\n=====\n")
	// (1/3 붙여넣기 화면은 쌓인 키를 모두 처리한 뒤 한 번만 그리므로 각본 입력에서는 2/3 부터 보인다 — 1/3 은 TestPasteStepLiveCount)
	for _, want := range []string{"2/3 패스워드", "붙여넣은 이름 3개를 보냅니다", "[X] 인증 실패", "3/3 결과", "[승인 완료] 작업 tnfa0001", "[필수 정책]"} {
		if !strings.Contains(all, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
	for _, secret := range []string{"wrongpw", "bob12345"} {
		if strings.Contains(r.out.String(), secret) {
			t.Fatalf("패스워드 %q 가 화면에 나옴", secret)
		}
	}
	// 붙여넣는 동안 판정(일치/불일치)은 보이지 않는다
	for _, fr := range frames(r) {
		if strings.Contains(fr, "1/3") && (strings.Contains(fr, "일치") || strings.Contains(fr, "빠짐")) {
			t.Fatalf("붙여넣기 단계에 판정이 보임:\n%s", fr)
		}
	}
}

func TestFullApproveMismatchAborts(t *testing.T) {
	f := aliceJobFake()
	keys := "\r" + "svr1ev01 svr1ev02\n\r" + "bob12345\r"
	r, _ := runUserKeys(t, f, "bob", keys+"\rq")
	fr := strings.Join(frames(r), "\n")
	if !strings.Contains(fr, "붙여넣은 대상 불일치(빠짐 1·남음 0·중복 0)") || !strings.Contains(fr, "작업이 중단되었습니다") {
		t.Fatalf("불일치 결과 화면:\n%s", lastFrame(r))
	}
	if strings.Contains(fr, "svr2ev01") {
		t.Fatal("불일치 결과에 빠진 이름이 보이면 안 됨")
	}
	if f.jobs[0].Status != daemon.StatusAborted {
		t.Fatalf("불일치 → 중단: %+v", f.jobs[0])
	}
}

// 1/3 붙여넣기: 인식한 이름 수만 바로 보이고(판정 없음), 빈 붙여넣기는 다음 단계로 가지 않는다
func TestPasteStepLiveCount(t *testing.T) {
	u := newUserScreen(aliceJobFake(), "bob", true, nil, time.Local)
	u.refresh()
	u.key(keyEv{k: kEnter})
	feed := func(s string) {
		for _, ev := range parseKeys([]byte(s)) {
			u.key(ev)
		}
	}
	feed("\x04")
	if fr := stripANSI(u.render(100, 30, false)); !strings.Contains(fr, "1/3 대상 붙여넣기") || !strings.Contains(fr, "붙여넣은 대상이 없습니다") || !strings.Contains(fr, "인식한 이름: 0개") {
		t.Fatalf("빈 붙여넣기:\n%s", fr)
	}
	feed("svr1ev01\tsvr1ev02 svr2ev01\nsvr9")
	fr := stripANSI(u.render(100, 30, false))
	if !strings.Contains(fr, "인식한 이름: 4개") || strings.Contains(fr, "일치") || strings.Contains(fr, "빠짐") {
		t.Fatalf("이름 수만(판정 없음):\n%s", fr)
	}
	feed("\x7f\x7f\x7f\x7f") // svr9 지움
	if fr := stripANSI(u.render(100, 30, false)); !strings.Contains(fr, "인식한 이름: 3개") {
		t.Fatalf("Backspace:\n%s", fr)
	}
	feed("\x7f") // 빈 줄에서 Backspace → 앞 줄을 다시 편집(이름 수는 그대로)
	if len(u.ap.box.lines) != 0 || !strings.Contains(string(u.ap.box.cur), "svr2ev01") {
		t.Fatalf("앞 줄 다시 편집: %+v", u.ap.box)
	}
	if u.ap.step != apPaste || !u.batching() {
		t.Fatal("아직 1/3, 붙여넣기 단계는 키를 모아 처리")
	}
}

func TestFullApproveEscAndEmptyPaste(t *testing.T) {
	f := aliceJobFake()
	// 빈 상태로 Ctrl+D(안내만) → Esc → 목록(승인 안 보냄)
	r, _ := runUserKeys(t, f, "bob", "\r"+ctrlD+kEscS+"q")
	if !strings.Contains(strings.Join(frames(r), "\n"), "승인하지 않고 목록으로 돌아왔습니다") {
		t.Fatalf("Esc → 목록:\n%s", lastFrame(r))
	}
	if f.hasCall("approve") || f.jobs[0].Status != daemon.StatusWaiting {
		t.Fatal("Esc 로 돌아가면 승인하지 않아야 함")
	}
	// 2/3 에서 Esc → 1/3 으로(붙여넣은 내용 유지, 패스워드는 지움)
	u := newUserScreen(aliceJobFake(), "bob", true, nil, time.Local)
	u.refresh()
	for _, ev := range parseKeys([]byte("\rsvr1ev01\n" + ctrlD + "abc" + kEscS)) {
		u.key(ev)
	}
	if fr := stripANSI(u.render(100, 30, false)); u.ap.step != apPaste || !strings.Contains(fr, "1/3") || !strings.Contains(fr, "인식한 이름: 1개") || len(u.ap.pw.rs) != 0 {
		t.Fatalf("2/3 에서 Esc → 1/3(내용 유지):\n%s", fr)
	}
}

// 붙여넣기 구간(bracketed paste) 안의 빈 줄은 입력을 끝내지 않는다. 구간 밖의 빈 줄 Enter 가 끝.
func TestFullBracketedPasteBlankLines(t *testing.T) {
	f := aliceJobFake()
	keys := "\r" + pStart + "svr1ev01\n\n\nsvr1ev02\r\n\nsvr2ev01\n" + pEnd + "\r" + "bob12345\r" + "\rq"
	r, _ := runUserKeys(t, f, "bob", keys)
	if f.jobs[0].Status != daemon.StatusApproved {
		t.Fatalf("붙여넣은 빈 줄로 끝나지 않고 3개 모두 받아 승인: %+v\n%s", f.jobs[0], lastFrame(r))
	}
	// 목록에서 붙여넣은 글은 단축키가 아니다(q 를 붙여넣어도 끝나지 않음)
	f2 := aliceJobFake()
	r2, code := runUserKeys(t, f2, "bob", pStart+"qc"+pEnd+"q")
	if code != ExitOK || f2.hasCall("cancel") || strings.Count(r2.out.String(), "\x1b[H") < 2 {
		t.Fatalf("붙여넣은 q·c 는 무시되어야 함 (code=%d)", code)
	}
}

func TestFullCheckerCannotSeeTargetsWorkerCan(t *testing.T) {
	f := aliceJobFake()
	r, _ := runUserKeys(t, f, "bob", "") // 확인자: 목록만(Enter 는 승인 화면)
	for _, fr := range frames(r) {
		if strings.Contains(fr, "svr1ev01") {
			t.Fatalf("확인자 화면에 대상:\n%s", fr)
		}
	}
	r, _ = runUserKeys(t, f, "alice", "\t\r") // Tab → 내 작업(작업자) 구역, 상세
	fr := lastFrame(r)
	if !strings.Contains(fr, "svr1ev01") || !strings.Contains(fr, "대상 VM") || !strings.Contains(fr, "3개") {
		t.Fatalf("작업자 상세에는 대상:\n%s", fr)
	}
	if !strings.Contains(fr, "playground_worker.sh") {
		t.Fatalf("상세에 호출 스크립트:\n%s", fr)
	}
}

func TestFullCancelNeedsPassword(t *testing.T) {
	f := aliceJobFake()
	r, _ := runUserKeys(t, f, "bob", "c"+"\r"+"nope00\r"+"bob12345\r"+"q")
	all := strings.Join(frames(r), "\n")
	for _, want := range []string{"취소(중단)", "패스워드를 입력하세요", "[X] 인증 실패", "[취소 완료] 작업 tnfa0001"} {
		if !strings.Contains(all, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
	if f.jobs[0].Status != daemon.StatusAborted {
		t.Fatalf("취소 → 중단: %+v", f.jobs[0])
	}
	// 작업자는 c 로 취소할 수 없다
	f2 := aliceJobFake()
	r2, _ := runUserKeys(t, f2, "alice", "\tcq")
	if !strings.Contains(strings.Join(frames(r2), "\n"), "c 취소는 내가 확인자인 대기 작업에서만") || f2.hasCall("cancel") {
		t.Fatal("작업자 c 는 안내만")
	}
}

func TestFullListPanesAllViewAndCounters(t *testing.T) {
	f := aliceJobFake()
	appr := t0.Add(10 * time.Second)
	j2 := job("tnfa0002", "bob", "carol", daemon.StatusApproved, "", "svr3ev01")
	j2.ApprovedAt, j2.Phase = &appr, daemon.PhaseWorking
	f.jobs = append(f.jobs, j2)
	r, _ := runUserKeys(t, f, "bob", "")
	fr := lastFrame(r)
	for _, want := range []string{"cross_check 상태 리포트", "(user: bob)", "갱신 09:00:30", "● 데몬 실행 (pid 4128)",
		"전체 1  대기 1  승인 0  작업중 0  작업완료 0  작업중단 0  중단 0", "[내가 확인자인 작업 1]", "내 작업 (작업자) 1", "승인 대기 1건", "playground_worker.sh",
		"↑↓ 이동  ←→/Tab 구역 전환  Enter 상세/승인  a 전체 보기", "q 종료"} {
		if !strings.Contains(fr, want) {
			t.Errorf("목록에 %q 없음:\n%s", want, fr)
		}
	}
	if strings.Index(fr, "내가 확인자인 작업 1") > strings.Index(fr, "내 작업 (작업자) 1") {
		t.Errorf("확인자 구역이 왼쪽(먼저)이어야 함:\n%s", fr)
	}
	// ← → Tab Shift+Tab 모두 구역 전환(2구역이라 서로 반대편으로), Tab 두 번이면 처음으로
	for name, key := range map[string]string{"→": kRightS, "←": "\x1b[D", "Tab": "\t", "Shift+Tab": "\x1b[Z"} {
		r, _ = runUserKeys(t, f, "bob", key)
		if fr := lastFrame(r); !strings.Contains(fr, "[내 작업 (작업자) 1]") || !strings.Contains(fr, "승인/작업중") || strings.Contains(fr, "playground_worker.sh") {
			t.Fatalf("%s → 내 작업 구역:\n%s", name, fr)
		}
	}
	r, _ = runUserKeys(t, f, "bob", "\t\t")
	if fr := lastFrame(r); !strings.Contains(fr, "[내가 확인자인 작업 1]") || !strings.Contains(fr, "playground_worker.sh") {
		t.Fatalf("Tab 두 번 → 처음(확인자) 구역:\n%s", fr)
	}
	r, _ = runUserKeys(t, f, "bob", "a\t")
	if fr := lastFrame(r); !strings.Contains(fr, "[내가 확인자인 작업 1]") || strings.Contains(fr, "[전체 보기") {
		t.Fatalf("전체 보기에서 Tab → 구역 보기:\n%s", fr)
	}
	r, _ = runUserKeys(t, f, "bob", "a")
	if fr := lastFrame(r); !strings.Contains(fr, "[전체 보기 2]") || !strings.Contains(fr, "전체 2  대기 1  승인 0  작업중 1") {
		t.Fatalf("a 전체 보기:\n%s", fr)
	}
}

func TestFullCtrlCDoesNotQuitAndQuits(t *testing.T) {
	f := aliceJobFake()
	r, code := runUserKeys(t, f, "bob", "\x03\x1a")
	if !strings.Contains(lastFrame(r), "종료는 q 를 누르세요") || code != ExitOK {
		t.Fatalf("Ctrl+C/Z 는 안내만:\n%s", lastFrame(r))
	}
	assertRestored(t, r)
}

func TestFullRegisterFirstRun(t *testing.T) {
	f := aliceJobFake()
	delete(f.pw, "bob")
	r, code := runUserKeys(t, f, "bob", "abcdef1\rabcdef2\r"+"new_pw_1\rnew_pw_1\r"+"q")
	if code != ExitOK || f.pw["bob"] != "new_pw_1" {
		t.Fatalf("등록: code=%d pw=%q", code, f.pw["bob"])
	}
	all := strings.Join(frames(r), "\n")
	for _, want := range []string{"첫 실행 — 패스워드 등록", "두 입력이 서로 다릅니다", "[등록 완료] bob"} {
		if !strings.Contains(all, want) {
			t.Errorf("화면에 %q 없음", want)
		}
	}
	if strings.Contains(r.out.String(), "new_pw_1") || strings.Contains(r.out.String(), "abcdef") {
		t.Fatal("등록 패스워드가 화면에 나옴")
	}
	if !strings.Contains(all, "새 패스워드      > ********_") {
		t.Fatalf("마스킹 표시(*) 없음:\n%s", all)
	}
	// 3번 실패하면 종료 1, 이유는 대체 화면을 떠난 뒤에도 남는다
	f2 := aliceJobFake()
	delete(f2.pw, "bob")
	r2, code2 := runUserKeys(t, f2, "bob", "aaaaaa1\raaaaaa2\r"+"x\rx\r"+"aaaaaa3\raaaaaa4\r")
	if code2 != ExitFail || !strings.Contains(tail(r2.out.String(), 400), "다시 입력하세요") && !strings.Contains(r2.out.String(), "(2/3)") {
		t.Fatalf("등록 3번 실패: code=%d", code2)
	}
}

func TestFullPasswordChange(t *testing.T) {
	f := aliceJobFake()
	r, _ := runUserKeys(t, f, "bob", "p"+"bob12345\rnew_pw_9\rnew_pw_9\r"+"q")
	if f.pw["bob"] != "new_pw_9" || !strings.Contains(strings.Join(frames(r), "\n"), "[변경 완료] bob") {
		t.Fatalf("패스워드 변경: %q", f.pw["bob"])
	}
}

// ---- 루프: 복원·시그널·창 크기·raw 실패·갱신 ----

func TestFullRestoreOnSignal(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	env, r := newRig(pr)
	sig := make(chan os.Signal, 1)
	sig <- os.Interrupt
	env.Signals = sig
	code, ok := RunUserFull(env, aliceJobFake(), "bob")
	if !ok || code != ExitInterrupt {
		t.Fatalf("시그널 → 130: %d %v", code, ok)
	}
	assertRestored(t, r)
}

type panicClient struct{ *fakeClient }

func (p *panicClient) List(string) ([]daemon.JobView, error) { panic("boom") }

func TestFullRestoreOnPanic(t *testing.T) {
	env, r := newRig(strings.NewReader("q"))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("패닉이 전파되어야 함")
			}
		}()
		RunUserFull(env, &panicClient{aliceJobFake()}, "bob")
	}()
	if r.raws != 1 || r.restored != 1 || !strings.HasSuffix(r.out.String(), leaveScreen) {
		t.Fatalf("패닉 후 복원: raw=%d restored=%d", r.raws, r.restored)
	}
}

func TestFullRawFailureFallsBack(t *testing.T) {
	env, r := newRig(strings.NewReader("q"))
	env.MakeRaw = func() (func(), error) { return nil, errors.New("no tty") }
	if _, ok := RunUserFull(env, aliceJobFake(), "bob"); ok {
		t.Fatal("raw 실패면 ok=false(줄 입력 화면으로)")
	}
	if r.out.Len() != 0 {
		t.Fatalf("raw 실패면 아무것도 쓰지 않아야 함: %q", r.out.String())
	}
}

func TestFullResizeRedrawsImmediately(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	env, r := newRig(pr)
	n := 0
	redrawn := make(chan struct{})
	env.Size = func() (int, int) {
		n++
		if n == 1 {
			return 80, 24
		}
		if n == 2 {
			close(redrawn)
		}
		return 100, 24
	}
	rs := make(chan os.Signal, 1)
	rs <- os.Interrupt // 값은 상관없음(실제로는 SIGWINCH)
	env.Resize = rs
	go func() {
		<-redrawn
		pw.Write([]byte("q"))
	}()
	RunUserFull(env, aliceJobFake(), "bob")
	o := r.out.String()
	i := strings.Index(o, "\x1b[2J")
	if i < 0 || !strings.Contains(o[:i], strings.Repeat("-", 80)+"\x1b[K") || !strings.Contains(o[i:], strings.Repeat("-", 100)+"\x1b[K") {
		t.Fatalf("창 크기 변경 → 지우고 새 폭(80→100)으로 다시 그림")
	}
}

func TestFullAutoRefreshTickAndDaemonDown(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	env, r := newRig(pr)
	tick := make(chan time.Time)
	env.Tick = func(d time.Duration) (<-chan time.Time, func()) {
		r.ticks = append(r.ticks, d)
		return tick, func() {}
	}
	f := aliceJobFake()
	done := make(chan int)
	go func() {
		code, _ := RunUserFull(env, f, "bob")
		done <- code
	}()
	tick <- time.Now() // 2초 tick 한 번: 다시 읽음
	f.mu.Lock()
	f.connErr = errors.New("connection refused")
	f.mu.Unlock()
	tick <- time.Now() // 데몬 연결 실패: 마지막 목록은 두고 머리말·안내 줄에 표시
	pw.Write([]byte("q"))
	<-done
	if len(r.ticks) != 1 || r.ticks[0] != 2*time.Second {
		t.Fatalf("자동 갱신 주기 2초: %v", r.ticks)
	}
	fr := lastFrame(r)
	if !strings.Contains(fr, "○ 데몬 중지") || !strings.Contains(fr, "상태 갱신 실패") || !strings.Contains(fr, "갱신 09:00:30") {
		t.Fatalf("데몬 중지 표시:\n%s", fr)
	}
	if f.listCalls < 3 {
		t.Fatalf("tick 마다 List: %d", f.listCalls)
	}
}

// ---- 관리자 화면 ----

func TestFullAdminFlows(t *testing.T) {
	f := aliceJobFake()
	appr := t0.Add(10 * time.Second)
	w := job("tnfa0002", "alice", "bob", daemon.StatusApproved, "", "svr1ev02")
	w.ApprovedAt, w.StartedAt, w.Phase = &appr, &appr, daemon.PhaseWorking
	f.jobs = append(f.jobs, w)
	keys := "wrong1\r" + "admin123\r" + // 로그인(1번 실패)
		"s1" + "점검\r" + // 맨 위(최근) = tnfa0002 작업중 → 1 작업완료, 사유
		kDownS + "s2\r" + // tnfa0001 대기 → 2 중단
		"d" + "n" + // 삭제 취소
		"x" + "아니오\r" + // 정리 취소
		"p" + "alice\r" + "new_pw_7\rnew_pw_7\r" + // 패스워드 재설정
		"x" + "전체 정리\r" + // 전체 정리
		"q"
	r, code := runAdminKeys(t, f, keys)
	assertRestored(t, r)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	for _, c := range []string{"admin_set_phase tnfa0002 작업완료 점검", "admin_set tnfa0001 중단 ", "admin_set_password alice", "admin_clear"} {
		if !f.hasCall(c) {
			t.Errorf("호출 없음 %q: %v", c, f.calls)
		}
	}
	if f.hasCall("admin_delete") {
		t.Error("n 이면 삭제하지 않아야 함")
	}
	all := strings.Join(frames(r), "\n")
	for _, want := range []string{"관리자 로그인", "[X] 인증 실패 (1/3)", "cross_check 관리자 상태 리포트", "(admin)",
		"승인 유효시간 2분 (approval_ttl_sec=120)", "작업 제한 24시간 (work_timeout_sec=86400)",
		"상태 변경 ▶ tnfa0002", "1 승인/작업완료", "[변경 완료] 작업 tnfa0002 → 승인/작업완료",
		"삭제하지 않았습니다", "확인 문구가 달라 정리하지 않았습니다", "[재설정 완료] alice", "[정리 완료] 2건 삭제",
		"↑↓ 이동  Enter 상세  s 상태 변경  d 삭제  x 전체 정리", "q 종료"} {
		if !strings.Contains(all, want) {
			t.Errorf("관리자 화면에 %q 없음", want)
		}
	}
	if strings.Contains(r.out.String(), "admin123") || strings.Contains(r.out.String(), "new_pw_7") {
		t.Fatal("패스워드가 화면에 나옴")
	}
}

func TestFullAdminLoginFailsThreeTimesAndMissing(t *testing.T) {
	f := aliceJobFake()
	r, code := runAdminKeys(t, f, "a1\ra2\ra3\r")
	if code != ExitFail || !strings.Contains(r.out.String(), "관리자 인증을 3번 실패해 종료합니다") {
		t.Fatalf("3번 실패 → 1: %d", code)
	}
	delete(f.pw, "admin")
	r, code = runAdminKeys(t, f, "")
	if code != ExitFail || !strings.Contains(r.out.String(), "관리자 화면을 열 수 없습니다") || r.raws != 0 {
		t.Fatalf("admin 항목 없음 → 화면 없이 안내·1: %d raws=%d", code, r.raws)
	}
}

func TestAdminChoicesMatchDaemonRules(t *testing.T) {
	labels := func(st store.Status, ph store.Phase) string {
		var out []string
		for _, c := range adminChoices(store.Job{Status: st, Phase: ph}) {
			out = append(out, c.label)
		}
		return strings.Join(out, ",")
	}
	cases := map[string]string{
		labels(daemon.StatusWaiting, ""):                   "승인,중단",
		labels(daemon.StatusApproved, ""):                  "승인/작업중단,중단",
		labels(daemon.StatusApproved, daemon.PhaseWorking): "승인/작업완료,승인/작업중단,중단",
		labels(daemon.StatusApproved, daemon.PhaseDone):    "중단",
		labels(daemon.StatusAborted, ""):                   "",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("adminChoices = %q, want %q", got, want)
		}
	}
}

// ---- 골든 화면 (80·100칸, 한글 폭) ----

// goldenFake 는 상태가 고루 섞인 목록(한글 caller 포함).
func goldenFake() *fakeClient {
	f := aliceJobFake()
	at := func(s int) *time.Time { t := t0.Add(time.Duration(s) * time.Second); return &t }
	mk := func(id, u, c string, st store.Status, ph store.Phase, reason, caller string, targets ...string) daemon.JobView {
		j := job(id, u, c, st, reason, targets...)
		j.Phase, j.Caller = ph, caller
		if st == daemon.StatusApproved {
			j.ApprovedAt = at(5)
		}
		if ph != "" {
			j.StartedAt = at(8)
		}
		if ph == daemon.PhaseDone || ph == daemon.PhaseAborted {
			j.FinishedAt = at(20)
		}
		return j
	}
	f.jobs = append([]daemon.JobView{
		mk("tnf90001", "bob", "alice", daemon.StatusApproved, daemon.PhaseDone, "", "vm_power_onoff", "svr3ev01"),
		mk("tnf90002", "bob", "carol", daemon.StatusApproved, daemon.PhaseAborted, daemon.ReasonWorkTimeout, "전원작업_스크립트.sh", "svr3ev02"),
		mk("tnf90003", "bob", "alice", daemon.StatusAborted, "", daemon.MismatchReason(1, 0, 0, false), "playground_worker.sh", "svr3ev01"),
		mk("tnf90004", "bob", "carol", daemon.StatusApproved, daemon.PhaseWorking, "", "", "svr3ev01", "svr3ev02"),
		mk("tnf90005", "bob", "alice", daemon.StatusApproved, "", "", "아주긴호출스크립트이름_가나다라마바사아자차카타파하.sh", "svr3ev02"),
	}, f.jobs...)
	return f
}

// goldenScreens 는 이름 → 화면(폭·색) 렌더 함수.
func goldenScreens(t *testing.T) map[string]func(w int, color bool) string {
	now := func() time.Time { return t0.Add(30 * time.Second) }
	user := func(prep func(u *userScreen)) func(w int, color bool) string {
		return func(w int, color bool) string {
			u := newUserScreen(goldenFake(), "bob", true, now, time.Local)
			u.refresh()
			if prep != nil {
				prep(u)
			}
			return u.render(w, 24, color)
		}
	}
	return map[string]func(int, bool) string{
		"user_mine": user(func(u *userScreen) {
			u.key(keyEv{k: kRight}) // 기본은 확인자 구역 → 내 작업
		}),
		"user_checker": user(nil), // 기본 구역
		"user_detail_worker": user(func(u *userScreen) {
			u.key(keyEv{k: kRune, r: '\t'}) // Tab → 내 작업
			u.key(keyEv{k: kDown})
			u.key(keyEv{k: kEnter})
		}),
		"user_paste": user(func(u *userScreen) {
			u.key(keyEv{k: kEnter})
			for _, ev := range parseKeys([]byte("svr2ev01\tsvr1ev01\nsvr1ev02 가나다\nsvr9")) {
				u.key(ev)
			}
		}),
		"user_password": user(func(u *userScreen) {
			u.key(keyEv{k: kEnter})
			for _, ev := range parseKeys([]byte("svr2ev01 svr1ev01 svr1ev02\n\x04bob1")) {
				u.key(ev)
			}
		}),
		"user_result": user(func(u *userScreen) {
			u.key(keyEv{k: kEnter})
			for _, ev := range parseKeys([]byte("svr2ev01 svr1ev01 svr1ev02\n\x04bob12345\r")) {
				u.key(ev)
			}
		}),
		"admin_list": func(w int, color bool) string {
			a := newAdminScreen(goldenFake(), now, time.Local)
			a.pw = "admin123"
			a.mode = amList
			a.refresh()
			return a.render(w, 24, color)
		},
	}
}

func TestScreenGolden(t *testing.T) {
	for name, fn := range goldenScreens(t) {
		for _, color := range []bool{false, true} {
			for _, w := range []int{80, 100} {
				mode := "plain"
				if color {
					mode = "color"
				}
				file := filepath.Join("testdata", "screen_"+name+"_"+mode+"_"+strconv.Itoa(w)+".golden")
				got := fn(w, color) + "\n"
				if *updateGolden {
					os.MkdirAll("testdata", 0o755)
					if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(file)
				if err != nil {
					t.Fatalf("골든 없음 %s (go test -run Golden -update): %v", file, err)
				}
				if string(want) != got {
					t.Errorf("%s 불일치\n--- got ---\n%s", file, got)
				}
			}
		}
	}
}

// 어떤 폭에서도 줄이 폭을 넘지 않고, 줄 수가 높이 이하이며, 색 줄은 리셋으로 끝나고, 패스워드·제어문자가 없다
func TestScreenWidthHeightAndSecrets(t *testing.T) {
	for name, fn := range goldenScreens(t) {
		for _, w := range []int{40, 60, 80, 100, 120, 200} {
			for _, color := range []bool{false, true} {
				s := fn(w, color)
				lines := strings.Split(s, "\n")
				if len(lines) > 24 {
					t.Errorf("%s w=%d: 줄 수 %d > 24", name, w, len(lines))
				}
				for _, l := range lines {
					if sw := strWidth(stripANSI(l)); sw > w {
						t.Errorf("%s w=%d: 폭 %d 초과: %q", name, w, sw, stripANSI(l))
					}
					if strings.ContainsAny(stripANSI(l), "\x1b\x00\r\t") {
						t.Errorf("%s w=%d: 제어문자: %q", name, w, l)
					}
				}
				if strings.Contains(s, "bob12345") || strings.Contains(s, "bob1") && !strings.Contains(name, "paste") {
					t.Errorf("%s: 패스워드가 화면에", name)
				}
			}
		}
	}
}

// 확인자 쪽 화면(목록·붙여넣기·패스워드·결과)에는 데몬이 준 대상 이름이 없다(붙여넣기 칸의 내 입력은 예외)
func TestScreenCheckerNeverShowsTargets(t *testing.T) {
	sc := goldenScreens(t)
	for _, name := range []string{"user_checker", "user_password", "user_result"} {
		s := sc[name](100, false)
		for _, tg := range []string{"svr1ev01", "svr1ev02", "svr2ev01"} {
			if strings.Contains(s, tg) {
				t.Errorf("%s 에 대상 %s:\n%s", name, tg, s)
			}
		}
	}
}
