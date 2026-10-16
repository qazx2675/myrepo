package main

// tui_test.go — 결과 TUI 테스트 (가짜 tty: 정해진 키 바이트·고정 화면 크기). 키 입력이 끝나면(EOF) TUI 는 Quit 상태가 된다.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type tuiRig struct {
	out      bytes.Buffer
	raws     int
	restored int
	closed   int
	w, h     int
}

// newTUIRigReader 는 in 을 키 입력으로 쓰는 가짜 환경으로 TUI 를 엽니다 (100x24, 색 없음, tick 은 울리지 않음).
func newTUIRigReader(t *testing.T, in io.Reader, mod func(e *tuiEnv, r *tuiRig)) (*TUI, *tuiRig) {
	t.Helper()
	r := &tuiRig{w: 100, h: 24}
	never := make(chan time.Time)
	env := tuiEnv{
		In:   in,
		Out:  &r.out,
		Size: func() (int, int) { return r.w, r.h },
		MakeRaw: func() (func(), error) {
			r.raws++
			return func() { r.restored++ }, nil
		},
		Tick:  func(time.Duration) (<-chan time.Time, func()) { return never, func() {} },
		Close: func() { r.closed++ },
	}
	if mod != nil {
		mod(&env, r)
	}
	u, err := tuiOpen(env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(u.Close)
	return u, r
}

// newTUIRig 는 keys 를 처음 키를 기다릴 때(env.Ready) 넣고 입력을 끝냅니다 (사람이 화면을 보고 치듯이 —
// Ask 가 묻기 전에 쌓인 키를 버려도 시험 키는 남는다).
func newTUIRig(t *testing.T, keys string) (*TUI, *tuiRig) {
	t.Helper()
	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.Close() })
	var once sync.Once
	return newTUIRigReader(t, pr, func(e *tuiEnv, _ *tuiRig) {
		e.Ready = func() {
			once.Do(func() {
				if keys != "" {
					pw.Write([]byte(keys))
				}
				pw.Close()
			})
		}
	})
}

// tuiLastFrame 은 마지막으로 그린 화면 (ANSI 제거, 줄은 \n).
func tuiLastFrame(r *tuiRig) string {
	o := r.out.String()
	i := strings.LastIndex(o, "\x1b[H")
	if i < 0 {
		return ""
	}
	return tuiStripANSI(strings.ReplaceAll(o[i:], "\x1b[K\r\n", "\n"))
}

func tuiL(tag, vendor, host, mgmt, attr, exp, cur, note string) ResultLine {
	return ResultLine{Kind: KindProfile, Tag: tag, Host: host, IP: "10.0.0." + host[len(host)-1:], Vendor: vendor, Model: "DL380",
		BiosVersion: "U30", MgmtModel: mgmt, MgmtVersion: mgmt + " 2.99", Attr: attr, Expected: exp, Current: cur, Note: note}
}

// tuiSampleVGs: HPE iLO 4 / iLO 5 (속성 목록이 달라 그룹 2개), DELL iDRAC 9 (정상 1대 + 접속 실패 1대)
func tuiSampleVGs() []VendorGroups {
	return groupLines([]ResultLine{
		tuiL(TagOK, "HPE", "h1", "iLO 4", "AttrA", "On", "On", "OK"),
		tuiL(TagDiff, "HPE", "h1", "iLO 4", "AttrB", "On", "Off", "FAIL"),
		tuiL(TagOK, "HPE", "h2", "iLO 5", "AttrA", "On", "On", "OK"),
		tuiL(TagDiff, "HPE", "h2", "iLO 5", "AttrB5", "On", "Off", "FAIL"),
		tuiL(TagNA, "HPE", "h2", "iLO 5", "AttrC5", "On", "-", "MAPPING_MISSING"),
		tuiL(TagOK, "DELL", "d1", "iDRAC 9", "LogicalProc", "Enabled", "Enabled", "OK"),
		{Tag: TagNA, Host: "d2", IP: "10.0.1.2", Vendor: "DELL", Model: "-", MgmtModel: "iDRAC 9", Attr: "-", Expected: "-", Current: "-",
			Note: "UNREACHABLE", HostError: true},
	})
}

func browseKeys(t *testing.T, keys string, vgs []VendorGroups) (*TUI, *tuiRig, TUIAction) {
	t.Helper()
	u, r := newTUIRig(t, keys)
	u.SetResults("BIOS 점검 결과 — 프로파일 VM", vgs)
	return u, r, u.Browse()
}

// ---- 키 파서 ----

func TestTUIParseKeys(t *testing.T) {
	type k = tuiKey
	cases := []struct {
		in   string
		want []tuiKey
	}{
		{"\x1b[A\x1b[B\x1b[C\x1b[D", []k{{k: tkUp}, {k: tkDown}, {k: tkRight}, {k: tkLeft}}},
		{"\x1bOA\x1bOD", []k{{k: tkUp}, {k: tkLeft}}},
		{"\x1b[5~\x1b[6~", []k{{k: tkPgUp}, {k: tkPgDn}}},
		{"\x1b[H\x1b[F\x1b[1~\x1b[4~\x1bOH", []k{{k: tkHome}, {k: tkEnd}, {k: tkHome}, {k: tkEnd}, {k: tkHome}}},
		{"\t\x1b[Z", []k{{k: tkRune, r: tuiTab}, {k: tkBackTab}}},
		{"\x1b", []k{{k: tkEsc}}},
		{"\x1bq", []k{{k: tkEsc}, {k: tkRune, r: 'q'}}},
		{"\r\n\n", []k{{k: tkEnter}, {k: tkEnter}}},
		{"\x03\x1a\x04\x18", []k{{k: tkRune, r: tuiCtrlC}, {k: tkRune, r: tuiCtrlZ}, {k: tkRune, r: tuiCtrlD}, {k: tkRune, r: tuiCtrlX}}},
		{"\x7f\x08", []k{{k: tkRune, r: tuiBackspace}, {k: tkRune, r: tuiBackspace}}},
		{"\x1b[1;5A", []k{{k: tkUp}}},
		{"\x1b[3~", nil},
		{"\x1b[", nil},
		{"가j", []k{{k: tkRune, r: '가'}, {k: tkRune, r: 'j'}}},
	}
	for _, c := range cases {
		got := tuiParseKeys([]byte(c.in))
		if len(got) != len(c.want) {
			t.Errorf("tuiParseKeys(%q)=%v want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("tuiParseKeys(%q)[%d]=%v want %v", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// ---- 탭·그룹 ----

func TestTUITabSwitching(t *testing.T) {
	_, r, _ := browseKeys(t, "x", tuiSampleVGs())
	f := tuiLastFrame(r)
	if !strings.Contains(f, ">> 벤더  >[HPE] [DELL]") || !strings.Contains(f, "   관리망  >[iLO 4] [iLO 5]   호스트 1대") ||
		!strings.Contains(f, "AttrB ") || strings.Contains(f, "AttrB5") || !strings.Contains(f, "←→ 벤더") ||
		!strings.Contains(f, "Enter 관리망 고르기") || !strings.Contains(f, "q 두 번 종료") {
		t.Errorf("첫 화면은 HPE / iLO 4, 벤더 줄이 움직이는 줄:\n%s", f)
	}
	// Enter 로 벤더 안에 들어가면 ←→ 는 관리망 그룹 (끝에서 멈춤, 벤더는 그대로)
	u, r, act := browseKeys(t, "\r\x1b[C\x1b[C", tuiSampleVGs())
	f = tuiLastFrame(r)
	if !u.inGroup || u.vi != 0 || u.gi != 1 || act != TUIQuit || !strings.Contains(f, "   벤더  >[HPE] [DELL]") ||
		!strings.Contains(f, ">> 관리망  [iLO 4] >[iLO 5]") || !strings.Contains(f, "AttrC5") || strings.Contains(f, "h1 ") ||
		!strings.Contains(f, "←→ 관리망") || !strings.Contains(f, "q/Esc 벤더로") {
		t.Errorf("Enter → ←→ 관리망: inGroup=%v vi=%d gi=%d\n%s", u.inGroup, u.vi, u.gi, f)
	}
	if u, _, _ := browseKeys(t, "\r\x1b[C\x1b[D\x1b[D", tuiSampleVGs()); u.gi != 0 || u.vi != 0 {
		t.Errorf("← 는 첫 그룹에서 멈춤: vi=%d gi=%d", u.vi, u.gi)
	}
	// q 는 한 단계 뒤로(그룹 위치는 그대로), 맨 위(벤더 줄)는 q 두 번에 끝낸다. Esc 도 뒤로 가지만 맨 위에서는 끝내지 않음.
	for _, k := range []string{"q", "\x1b"} {
		u, _, act = browseKeys(t, "\r\x1b[C"+k+"\x1b[C"+k+"qq", tuiSampleVGs())
		if act != TUIQuit || u.Quit() || u.inGroup || u.vi != 1 || u.gi != 0 {
			t.Errorf("%q: 뒤로 → → DELL → 끝: act=%v quit=%v inGroup=%v vi=%d gi=%d", k, act, u.Quit(), u.inGroup, u.vi, u.gi)
		}
	}
	u, _, act = browseKeys(t, "\r\x1b[Cq", tuiSampleVGs())
	if act != TUIQuit || !u.Quit() || u.inGroup || u.gi != 1 {
		t.Errorf("그룹 줄의 q 는 끝내지 않음 (입력 끝으로 종료): quit=%v inGroup=%v gi=%d", u.Quit(), u.inGroup, u.gi)
	}
	// 관리망 그룹이 하나뿐이면 Enter 는 안내만
	u, r, _ = browseKeys(t, "\x1b[C\r", tuiSampleVGs())
	if f := tuiLastFrame(r); u.inGroup || !strings.Contains(f, "관리망 그룹이 하나뿐입니다") || !strings.Contains(f, "관리망  iDRAC 9   호스트 2대") {
		t.Errorf("그룹 1개 Enter: inGroup=%v\n%s", u.inGroup, f)
	}
	u, r, _ = browseKeys(t, "\x1b[C\x1b[Cq", tuiSampleVGs())
	f = tuiLastFrame(r)
	if u.vi != 1 || u.gi != 0 || !strings.Contains(f, "[HPE] >[DELL]") || !strings.Contains(f, "LogicalProc") {
		t.Errorf("→ 는 DELL 로(끝에서 멈춤, 그룹은 처음으로): vi=%d gi=%d\n%s", u.vi, u.gi, f)
	}
	if u, _, _ := browseKeys(t, "\x1b[C\x1b[D\x1b[Dq", tuiSampleVGs()); u.vi != 0 {
		t.Errorf("← 는 처음 벤더에서 멈춤: vi=%d", u.vi)
	}
	// 보기 탭이 없으면 Tab 은 아무 일도 하지 않음
	if u, _, act := browseKeys(t, "\tqq", tuiSampleVGs()); act != TUIQuit || u.Quit() || u.gi != 0 {
		t.Errorf("탭 없을 때 Tab: act=%v gi=%d", act, u.gi)
	}
}

// 보기 탭: Tab 은 어느 단계에서나 TUITab, 탭마다 벤더·그룹·단계·필터·스크롤을 따로 기억한다. r 은 SetRefKey 로 켠 탭에서만.
func TestTUIViewTabs(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	u, r := newTUIRigReader(t, pr, nil)
	u.SetTabs([]string{"프로파일 점검", "전체 설정 비교"})
	u.SetResults("프로파일 결과", tuiSampleVGs())
	browse := func(keys string) TUIAction {
		go pw.Write([]byte(keys))
		return u.Browse()
	}
	if act := browse("r\r\x1b[Cf\t"); act != TUITab || !u.inGroup || u.gi != 1 || u.filter != tuiFilterDiff {
		t.Fatalf("그룹 줄에서 Tab: act=%v inGroup=%v gi=%d filter=%c", act, u.inGroup, u.gi, u.filter)
	}
	f := tuiLastFrame(r)
	if !strings.Contains(f, "화면  >[프로파일 점검] [전체 설정 비교]   Tab 전환") || !strings.Contains(f, "Tab 화면 전환") || strings.Contains(f, "r 기준") {
		t.Errorf("보기 탭 줄·안내:\n%s", f)
	}
	u.SwitchTab(1)
	if u.Tab() != 1 || u.vgs != nil || u.inGroup || u.filter != tuiFilterAll || u.title != "전체 설정 비교" {
		t.Errorf("빈 탭으로: tab=%d inGroup=%v filter=%c title=%q", u.Tab(), u.inGroup, u.filter, u.title)
	}
	u.SetResults("전체비교 — 기준 x", groupLines([]ResultLine{tuiL(TagDiff, "DELL", "d9", "iDRAC 9", "SysProfile", "Perf", "Custom", "DIFF")}))
	u.SetRefKey(true)
	if act := browse("o\x1b[Z"); act != TUITab || u.filter != tuiFilterOK {
		t.Fatalf("Shift+Tab: act=%v filter=%c", act, u.filter)
	}
	if act := browse("r"); act != TUIRef {
		t.Errorf("r 은 SetRefKey 탭에서 TUIRef: %v", act)
	}
	if f := tuiLastFrame(r); !strings.Contains(f, "[프로파일 점검] >[전체 설정 비교]") || !strings.Contains(f, "전체비교 — 기준 x") || !strings.Contains(f, "r 기준 다시 선택") {
		t.Errorf("전체 설정 비교 화면:\n%s", f)
	}
	u.SwitchTab(0)
	if u.Tab() != 0 || !u.inGroup || u.gi != 1 || u.filter != tuiFilterDiff || u.refKey || u.title != "프로파일 결과" {
		t.Errorf("돌아오면 상태 유지: inGroup=%v gi=%d filter=%c refKey=%v", u.inGroup, u.gi, u.filter, u.refKey)
	}
	u.SwitchTab(1)
	if u.filter != tuiFilterOK || !u.refKey || u.title != "전체비교 — 기준 x" {
		t.Errorf("전체 설정 비교 상태 유지: filter=%c refKey=%v", u.filter, u.refKey)
	}
	// 글 보기에서는 Tab·r 을 받지 않는다
	u.SetText("수정 미리보기", []string{"x"})
	if act := browse("\trq"); act != TUIQuit || u.Quit() {
		t.Errorf("글 보기 Tab/r: act=%v", act)
	}
}

// 같은 벤더에서 속성 이름 목록이 같은 관리망은 한 탭으로 합쳐 보인다.
func TestTUIMgmtGroupMerge(t *testing.T) {
	vgs := groupLines([]ResultLine{
		tuiL(TagOK, "HPE", "h1", "iLO 4", "AttrA", "On", "On", "OK"),
		tuiL(TagOK, "HPE", "h2", "iLO 5", "AttrA", "On", "On", "OK"),
	})
	_, r, _ := browseKeys(t, "q", vgs)
	f := tuiLastFrame(r)
	if !strings.Contains(f, "관리망  iLO 4+iLO 5   호스트 2대") || !strings.Contains(f, "h1") || !strings.Contains(f, "h2") {
		t.Errorf("합친 그룹 표시:\n%s", f)
	}
}

// ---- 필터·건수 ----

func TestTUIFiltersAndCounts(t *testing.T) {
	vgs := tuiSampleVGs()
	body := func(f string) string { // 표 머리 줄 아래부터
		i := strings.Index(f, "태그")
		return f[i:]
	}
	_, r, _ := browseKeys(t, "\r\x1b[Cq", vgs)
	if f := tuiLastFrame(r); !strings.Contains(f, "[보기: 전체]  OK 1  difference 1  N/A 1") || !strings.Contains(f, "줄 1-3/3") {
		t.Errorf("전체 보기 건수:\n%s", f)
	}
	_, r, _ = browseKeys(t, "\r\x1b[Cfq", vgs) // 필터는 어느 단계에서나 (관리망 그룹 줄)
	if f := tuiLastFrame(r); !strings.Contains(f, "[보기: difference만]") || !strings.Contains(body(f), "[difference]") ||
		strings.Contains(body(f), "[OK]") || strings.Contains(body(f), "[N/A]") {
		t.Errorf("f:\n%s", f)
	}
	_, r, _ = browseKeys(t, "\r\x1b[Coq", vgs)
	if f := tuiLastFrame(r); !strings.Contains(f, "[보기: OK만]") || !strings.Contains(body(f), "[OK]") || strings.Contains(body(f), "[difference]") {
		t.Errorf("o:\n%s", f)
	}
	_, r, _ = browseKeys(t, "\r\x1b[Cnq", vgs)
	if f := tuiLastFrame(r); !strings.Contains(body(f), "[N/A]") || !strings.Contains(body(f), "(N/A)") || strings.Contains(body(f), "[OK]") {
		t.Errorf("n (값 없음은 (N/A)):\n%s", f)
	}
	_, r, _ = browseKeys(t, "\r\x1b[Cnaq", vgs)
	if f := tuiLastFrame(r); !strings.Contains(f, "[보기: 전체]") || !strings.Contains(body(f), "[OK]") {
		t.Errorf("a 로 전체 복귀:\n%s", f)
	}
	_, r, _ = browseKeys(t, "\x1b[Csq", vgs) // DELL 요약
	f := tuiLastFrame(r)
	if !strings.Contains(f, "[보기: 요약]") || !strings.Contains(f, "host  ip") || !strings.Contains(f, "difference  N/A  비고") ||
		!strings.Contains(f, "UNREACHABLE") || !strings.Contains(f, "iDRAC 9") {
		t.Errorf("s 요약:\n%s", f)
	}
	sums := tuiSummarize(vgs[1].Groups[0])
	if len(sums) != 2 || sums[0].OK != 1 || sums[0].Mgmt != "iDRAC 9 2.99" || sums[1].NA != 1 || sums[1].Err != "UNREACHABLE" || sums[1].Mgmt != "iDRAC 9" {
		t.Errorf("요약 집계: %+v", sums)
	}
}

// ---- 스크롤 ----

func TestTUIScrollBounds(t *testing.T) {
	var lines []ResultLine
	for i := 1; i <= 60; i++ {
		lines = append(lines, tuiL(TagOK, "HPE", "h1", "iLO 5", fmt.Sprintf("attr%02d", i), "1", "1", "OK"))
	}
	vgs := groupLines(lines)
	u, r, _ := browseKeys(t, "\x1b[6~q", vgs)
	if u.bodyH < 5 || u.top != u.bodyH {
		t.Fatalf("PgDn 한 번 = 본문 높이: top=%d bodyH=%d", u.top, u.bodyH)
	}
	u, r, _ = browseKeys(t, strings.Repeat("\x1b[6~", 10)+"q", vgs)
	f := tuiLastFrame(r)
	if u.top != 60-u.bodyH || !strings.Contains(f, "attr60") || strings.Contains(f, "attr01") || !strings.Contains(f, fmt.Sprintf("줄 %d-60/60", 61-u.bodyH)) {
		t.Errorf("PgDn 끝에서 멈춤: top=%d bodyH=%d\n%s", u.top, u.bodyH, f)
	}
	if u, _, _ := browseKeys(t, "G\x1b[5~q", vgs); u.top != 60-2*u.bodyH {
		t.Errorf("End 후 PgUp: top=%d", u.top)
	}
	if u, _, _ := browseKeys(t, "\x1b[Fgq", vgs); u.top != 0 {
		t.Errorf("Home(g): top=%d", u.top)
	}
	if u, _, _ := browseKeys(t, "kk\x1b[5~jjjkq", vgs); u.top != 2 {
		t.Errorf("맨 위에서 ↑ 는 0 에서 멈추고 j/k 는 한 줄: top=%d", u.top)
	}
	if u, _, _ := browseKeys(t, "\x1b[6~\r\x1b[Cq", tuiSampleVGs()); u.top != 0 {
		t.Errorf("줄이 화면보다 적으면 스크롤 없음: top=%d", u.top)
	}
}

// ---- Ctrl 키·종료·복원 ----

func TestTUICtrlCIgnored(t *testing.T) {
	for _, k := range []string{"\x03", "\x1a", "\x18"} {
		u, r, act := browseKeys(t, k+"qq", tuiSampleVGs())
		if act != TUIQuit || u.Quit() {
			t.Errorf("%q 뒤 q 두 번으로 끝나야 함: act=%v quit=%v", k, act, u.Quit())
		}
		want := "종료는 q 를 두 번 누르세요"
		if k == "\x18" { // 결과 화면의 Ctrl+X 는 끝내지 않고 안내만
			want = "Ctrl+X 는 진행 중 작업 취소입니다"
		}
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("%q: 안내 %q 없음", k, want)
		}
	}
	// 입력 중 Ctrl+C 는 글자로 들어가지 않고 취소 안내
	u, _ := newTUIRig(t, "a\x03b\x04")
	if got, ok := u.InputLines("대상"); !ok || len(got) != 1 || got[0] != "ab" {
		t.Errorf("입력 중 Ctrl+C: %q %v", got, ok)
	}
	u, _ = newTUIRig(t, "\x03")
	u.InputLine("x", "")
	if u.msg != "취소는 Ctrl+X 를 누르세요" {
		t.Errorf("입력 중 Ctrl+C 안내: %q", u.msg)
	}
}

func TestTUIQuitKeysAndRestore(t *testing.T) {
	for _, c := range []struct {
		keys string
		act  TUIAction
		quit bool
	}{
		{"qq", TUIQuit, false}, {"jjQQ", TUIQuit, false}, {"e", TUIEdit, false}, {"", TUIQuit, true},
		// 맨 위에서 q 한 번·Esc·N·Ctrl+X 는 끝내지 않는다 (입력 끝으로만 종료). q 뒤 Esc·다른 키면 종료 확인 취소
		{"q", TUIQuit, true}, {"\x1b", TUIQuit, true}, {"q\x1b", TUIQuit, true}, {"q\x1bq", TUIQuit, true},
		{"qjq", TUIQuit, true}, {"N", TUIQuit, true}, {"\x18", TUIQuit, true}, {"q\x1bqq", TUIQuit, false},
	} {
		u, r, act := browseKeys(t, c.keys, tuiSampleVGs())
		if act != c.act || u.Quit() != c.quit {
			t.Errorf("%q: act=%v quit=%v", c.keys, act, u.Quit())
		}
		u.Close()
		u.Close() // 두 번 불러도 됨
		o := r.out.String()
		if r.raws != 1 || r.restored != 1 || r.closed != 1 || !strings.HasPrefix(o, tuiEnterScreen) || !strings.HasSuffix(o, tuiLeaveScreen) {
			t.Errorf("%q: 복원 raw=%d restored=%d closed=%d", c.keys, r.raws, r.restored, r.closed)
		}
	}
}

func TestTUISignalQuits(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	sig := make(chan os.Signal, 1)
	sig <- os.Interrupt
	u, _ := newTUIRigReader(t, pr, func(e *tuiEnv, _ *tuiRig) { e.Signals = sig })
	u.SetResults("x", tuiSampleVGs())
	if act := u.Browse(); act != TUIQuit || !u.Quit() {
		t.Fatalf("시그널: act=%v quit=%v", act, u.Quit())
	}
	if a := u.Ask("다음?"); a != TUIAbort {
		t.Errorf("Quit 뒤 Ask=%v", a)
	}
	if _, ok := u.InputLines("x"); ok {
		t.Error("Quit 뒤 입력은 ok=false")
	}
}

func TestTUIOpenRawFailure(t *testing.T) {
	var out bytes.Buffer
	closed := 0
	_, err := tuiOpen(tuiEnv{In: strings.NewReader(""), Out: &out, Size: func() (int, int) { return 80, 24 },
		MakeRaw: func() (func(), error) { return nil, errors.New("no tty") }, Close: func() { closed++ }})
	if err == nil || out.Len() != 0 || closed != 1 {
		t.Errorf("raw 실패: err=%v out=%q closed=%d", err, out.String(), closed)
	}
}

func TestTUIResizeRedraws(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	rs := make(chan os.Signal, 1)
	rs <- os.Interrupt // 값은 상관없음 (실제로는 SIGWINCH)
	redrawn := make(chan struct{})
	u, r := newTUIRigReader(t, pr, func(e *tuiEnv, r *tuiRig) {
		e.Resize = rs
		n := 0 // 1 = tuiOpen, 2 = 첫 그리기, 3 = 크기 변경 뒤 다시 그리기
		e.Size = func() (int, int) {
			n++
			if n == 3 {
				close(redrawn)
			}
			if n >= 3 {
				return 60, 20
			}
			return 100, 24
		}
	})
	go func() {
		<-redrawn
		pw.Write([]byte("qq"))
	}()
	u.SetResults("x", tuiSampleVGs())
	u.Browse()
	if !strings.Contains(r.out.String(), "\x1b[2J\x1b[H") {
		t.Errorf("크기 변경 시 화면을 지우고 다시 그려야 함")
	}
	for _, l := range strings.Split(tuiLastFrame(r), "\n") {
		if tuiStrWidth(l) > 59 {
			t.Errorf("새 폭(60-1)을 넘는 줄: %q", l)
		}
	}
}

// ---- 질문·입력 ----

func TestTUIAsk(t *testing.T) {
	for _, c := range []struct {
		keys string
		want TUIAnswer
	}{{"y", TUIYes}, {"N", TUINo}, {"\x18", TUIAbort}, {"\x1b", TUIAbort}, {"", TUIAbort}, {"x\ry", TUIYes}} {
		u, r := newTUIRig(t, c.keys)
		u.SetResults("x", tuiSampleVGs())
		if got := u.Ask("전체비교 결과를 볼까요?"); got != c.want {
			t.Errorf("%q: %v want %v", c.keys, got, c.want)
		}
		if !strings.Contains(r.out.String(), "전체비교 결과를 볼까요? [y/n]") && c.keys != "" {
			t.Errorf("%q: 질문이 그려져야 함", c.keys)
		}
		if c.keys == "x\ry" && !strings.Contains(r.out.String(), "y 또는 n 을 누르세요") {
			t.Errorf("다른 키 안내 없음")
		}
		if u.prompt != nil {
			t.Error("질문 상자가 남아 있음")
		}
	}
	// 질문 중에도 Tab·방향키로 결과를 볼 수 있다 (글자 필터 키는 안 먹음)
	u, _ := newTUIRig(t, "\t\x1b[Cfy")
	u.SetResults("x", tuiSampleVGs())
	if a := u.Ask("수정할까요?"); a != TUIYes || u.vi != 1 || u.filter != tuiFilterAll {
		t.Errorf("질문 중 이동: a=%v vi=%d filter=%c", a, u.vi, u.filter)
	}
}

func TestTUIInputLinesPaste(t *testing.T) {
	u, _ := newTUIRig(t, "host1\nhost2\r\n  host3  \n\nhost4\x7f5\x04")
	got, ok := u.InputLines("대상 hostname 붙여넣기")
	if !ok || strings.Join(got, ",") != "host1,host2,host3,host5" {
		t.Errorf("붙여넣기: %q %v", got, ok)
	}
	u, _ = newTUIRig(t, "a\nb\x7f\x7fc\x04")
	if got, ok := u.InputLines("x"); !ok || strings.Join(got, ",") != "ac" {
		t.Errorf("빈 줄에서 Backspace 는 앞 줄로: %q", got)
	}
	u, _ = newTUIRig(t, "abc\ndef\x18")
	if got, ok := u.InputLines("x"); ok || got != nil {
		t.Errorf("Ctrl+X 취소: %q %v", got, ok)
	}
	u, r := newTUIRig(t, "h1\nh2\n")
	if _, ok := u.InputLines("대상"); ok || !u.Quit() {
		t.Error("Ctrl+D 없이 입력이 끝나면 ok=false")
	}
	if !strings.Contains(r.out.String(), "대상  (0줄 입력됨)") {
		t.Error("입력 상자가 그려져야 함")
	}
}

func TestTUIInputLine(t *testing.T) {
	u, r := newTUIRig(t, "abc\x7fd\r")
	if s, ok := u.InputLine("비교서버 hostname", ""); !ok || s != "abd" || !strings.Contains(r.out.String(), "비교서버 hostname: abd_") {
		t.Errorf("한 줄 입력: %q %v", s, ok)
	}
	u, _ = newTUIRig(t, " refs/a.json \r")
	if s, ok := u.InputLine("파일", ""); !ok || s != "refs/a.json" {
		t.Errorf("앞뒤 공백: %q", s)
	}
	u, _ = newTUIRig(t, "\x7fz\x18")
	if s, ok := u.InputLine("x", "init"); ok || s != "" {
		t.Errorf("Ctrl+X 취소: %q %v", s, ok)
	}
}

func TestTUIChoose(t *testing.T) {
	three := []string{"VM", "HPC", "DB"}
	for _, c := range []struct {
		keys  string
		items []string
		want  int
	}{
		{"\x1b[B\r", three, 1}, {"j\x1b[Bj\r", three, 2}, {"\x1b[A\r", three, 0}, {"3", three, 2},
		{"q", three, -1}, {"\x1b", three, -1}, {"\x18", three, -1}, {"9\r", three, 0},
		{"12", make([]string, 12), 11}, {"1\r", make([]string, 12), 0}, {"1\x7f2", make([]string, 12), 1},
	} {
		u, r := newTUIRig(t, c.keys)
		got, ok := u.Choose("프로파일 선택", c.items)
		if got != c.want || ok != (c.want >= 0) {
			t.Errorf("%q: %d %v want %d", c.keys, got, ok, c.want)
		}
		if c.keys == "9\r" && !strings.Contains(r.out.String(), "1~3 사이 번호를 누르세요") {
			t.Error("범위 밖 번호 안내")
		}
	}
	u, r := newTUIRig(t, "\x1b[B")
	u.Choose("프로파일 선택", three)
	if f := tuiLastFrame(r); !strings.Contains(f, "프로파일 선택") || !strings.Contains(f, " >  2) HPC") || !strings.Contains(f, "    1) VM") {
		t.Errorf("목록 표시:\n%s", f)
	}
	if i, ok := u.Choose("x", nil); i != -1 || ok {
		t.Error("빈 목록")
	}
}

// 화면 탭이 있으면 Choose·InputLine 에서 Tab 은 고르기를 접고 TakeTabOut 으로 알린다 (탭이 없으면 무시).
func TestTUIChooseInputTabOut(t *testing.T) {
	for _, k := range []string{"\t", "\x1b[Z"} {
		u, r := newTUIRig(t, k)
		u.SetTabs([]string{"프로파일 점검", "전체 설정 비교"})
		if i, ok := u.Choose("전체비교 기준 선택", []string{"a", "b"}); i != -1 || ok || !u.TakeTabOut() {
			t.Errorf("Choose %q: %d %v", k, i, ok)
		}
		if u.TakeTabOut() {
			t.Errorf("TakeTabOut 은 한 번 읽으면 지워져야 함")
		}
		if f := tuiLastFrame(r); !strings.Contains(f, "Tab 다른 화면") {
			t.Errorf("Choose 안내 줄:\n%s", f)
		}
		u2, _ := newTUIRig(t, "ab"+k)
		u2.SetTabs([]string{"프로파일 점검", "전체 설정 비교"})
		if s, ok := u2.InputLine("비교서버", ""); ok || s != "" || !u2.TakeTabOut() {
			t.Errorf("InputLine %q: %q %v", k, s, ok)
		}
	}
	// 탭이 없으면(대상·프로파일 고르기 단계) Tab 은 무시 — 목록은 그대로 Enter 로 고른다.
	u, _ := newTUIRig(t, "\t\r")
	if i, ok := u.Choose("프로파일 선택", []string{"VM", "HPC"}); i != 0 || !ok || u.TakeTabOut() {
		t.Errorf("탭 없음: %d %v", i, ok)
	}
}

// ---- 진행 ----

func TestTUIProgress(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	u, r := newTUIRigReader(t, pr, nil)
	u.SetResults("x", tuiSampleVGs())
	ready := make(chan struct{})
	go func() {
		<-ready
		pw.Write([]byte("q"))
		pw.Write([]byte("\r\x1b[C"))
		pw.Write([]byte("\x18"))
	}()
	err := u.Progress("시작", func(j *TUIJob) error {
		j.Set("[2/4 접속·조회 1/3 · OK 1]")
		close(ready)
		<-j.Canceled()
		return errors.New("취소됨")
	})
	o := tuiStripANSI(r.out.String())
	if err == nil || err.Error() != "취소됨" || u.Quit() || u.job != nil {
		t.Fatalf("Progress 결과: %v", err)
	}
	if !strings.Contains(o, "[진행] [2/4 접속·조회 1/3 · OK 1]") || !strings.Contains(o, "작업 중입니다 — 취소는 Ctrl+X") ||
		!strings.Contains(o, "취소를 요청했습니다") || !strings.Contains(o, "Ctrl+X 작업 취소") {
		t.Errorf("진행 화면:\n%s", tuiLastFrame(r))
	}
	if u.gi != 1 {
		t.Errorf("진행 중에도 Enter·→ 가 동작해야 함: gi=%d", u.gi)
	}
	// 바로 끝나는 작업
	if err := u.Progress("x", func(j *TUIJob) error { return nil }); err != nil || u.Quit() {
		t.Errorf("바로 끝나는 작업: %v", err)
	}
}

func TestTUIProgressSignalCancelsAndPanicPropagates(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	sig := make(chan os.Signal, 1)
	sig <- os.Interrupt
	u, _ := newTUIRigReader(t, pr, func(e *tuiEnv, _ *tuiRig) { e.Signals = sig })
	err := u.Progress("x", func(j *TUIJob) error {
		<-j.Canceled()
		return errors.New("중단")
	})
	if err == nil || !u.Quit() {
		t.Errorf("시그널이면 작업을 취소하고 끝: err=%v quit=%v", err, u.Quit())
	}
	u2, r2 := newTUIRigReader(t, pr, nil)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("작업 패닉이 호출한 쪽으로 전파되어야 함")
			}
		}()
		u2.Progress("x", func(j *TUIJob) error { panic("boom") })
	}()
	u2.Close()
	if r2.restored != 1 {
		t.Error("패닉 뒤 Close 로 복원")
	}
}

// 끝내는 중(첫 시그널로 작업 취소, 진행 중 호스트를 기다림)에 시그널을 또 받으면 기다리지 않고 터미널을 되돌린 뒤 끝낸다.
func TestTUIProgressSecondSignalExits(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	sig := make(chan os.Signal, 2)
	u, r := newTUIRigReader(t, pr, func(e *tuiEnv, _ *tuiRig) { e.Signals = sig })
	release := make(chan struct{})
	code := -1
	restoredAtExit := 0
	old := tuiExit
	defer func() { tuiExit = old }()
	tuiExit = func(c int) { code, restoredAtExit = c, r.restored; close(release) }
	err := u.Progress("x", func(j *TUIJob) error {
		sig <- os.Interrupt // 첫 시그널: 취소 요청
		<-j.Canceled()
		sig <- os.Interrupt // 진행 중 호스트가 아직 안 끝났는데 두 번째 시그널
		<-release           // 실제로는 os.Exit 로 여기까지 오지 않음
		return nil
	})
	if err != nil || code != tuiExitSignal || restoredAtExit != 1 || r.closed != 1 || !u.Quit() {
		t.Errorf("두 번째 시그널: err=%v code=%d restored=%d closed=%d", err, code, restoredAtExit, r.closed)
	}
}

// 맨 위(벤더 줄) q → 종료 확인 안내, q 한 번 더 → 끝, Esc → 계속. 관리망 그룹 줄·글 보기의 q 는 한 단계 뒤로.
func TestTUIHomeQuitConfirm(t *testing.T) {
	u, r, act := browseKeys(t, "q", tuiSampleVGs())
	if f := tuiLastFrame(r); act != TUIQuit || !u.Quit() || !strings.Contains(f, tuiQuitConfirm) {
		t.Errorf("q 한 번은 안내만: act=%v quit=%v\n%s", act, u.Quit(), f)
	}
	_, r, _ = browseKeys(t, "q\x1b", tuiSampleVGs())
	if f := tuiLastFrame(r); strings.Contains(f, tuiQuitConfirm) {
		t.Errorf("Esc 면 종료 확인 안내가 지워져야 함:\n%s", f)
	}
	u, _, act = browseKeys(t, "\r\x1b[Cqqq", tuiSampleVGs()) // 그룹 줄 q → 벤더 줄, q q → 끝
	if act != TUIQuit || u.Quit() || u.inGroup {
		t.Errorf("그룹 q → 벤더 줄 → q q 끝: act=%v quit=%v inGroup=%v", act, u.Quit(), u.inGroup)
	}
	for _, k := range []string{"q", "\x1b"} { // 글 보기(미리보기·설정 결과)의 q/Esc 는 바로 돌아가기
		u, _ := newTUIRig(t, k)
		u.SetText("수정 미리보기", []string{"x"})
		if act := u.Browse(); act != TUIQuit || u.Quit() {
			t.Errorf("글 보기 %q: act=%v quit=%v", k, act, u.Quit())
		}
	}
}

// Ask 는 묻기 전에 쌓인 키(미리 친 y)를 버리고, 질문을 띄운 뒤 친 키로 답한다. 쌓인 키 중 입력 끝이면 Quit.
func TestTUIAskDrainsTypeAhead(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	u, _ := newTUIRigReader(t, pr, func(e *tuiEnv, _ *tuiRig) {
		e.Ready = func() { pw.Write([]byte("n")) }
	})
	u.keys <- tuiKey{k: tkRune, r: 'y'}
	u.keys <- tuiKey{k: tkRune, r: 'y'}
	if a := u.Ask("수정할까요?"); a != TUINo {
		t.Errorf("미리 친 y 를 버리고 n: %v", a)
	}
	u.keys <- tuiKey{k: tkRune, r: 'y'}
	u.keys <- tuiKey{k: tkEOF}
	if a := u.Ask("진행할까요?"); a != TUIAbort || !u.Quit() {
		t.Errorf("쌓인 입력 끝: a=%v quit=%v", a, u.Quit())
	}
}

// ---- 내용 교체·글 보기·도움말 ----

func TestTUISetResultsAndText(t *testing.T) {
	u, r := newTUIRig(t, "\r\x1b[Cfe"+"qq"+"Gq")
	u.SetResults("BIOS 점검 결과 — 프로파일 VM", tuiSampleVGs())
	if act := u.Browse(); act != TUIEdit || !u.inGroup || u.gi != 1 {
		t.Fatalf("그룹 줄에서 e: act=%v inGroup=%v gi=%d", act, u.inGroup, u.gi)
	}
	u.SetResults("전체비교 — 기준 ref01", groupLines([]ResultLine{tuiL(TagDiff, "DELL", "d9", "iDRAC 9", "SysProfile", "Perf", "Custom", "FAIL")}))
	if u.gi != 0 || u.inGroup || u.filter != tuiFilterAll {
		t.Errorf("내용 교체 시 그룹·단계·필터 처음으로")
	}
	u.Browse()
	if f := tuiLastFrame(r); !strings.Contains(f, "전체비교 — 기준 ref01") || !strings.Contains(f, "SysProfile") || strings.Contains(f, "AttrA") {
		t.Errorf("내용 교체:\n%s", f)
	}
	var text []string
	for i := 1; i <= 40; i++ {
		text = append(text, fmt.Sprintf("미리보기 %02d", i))
	}
	u.SetText("수정 미리보기", text)
	if act := u.Browse(); act != TUIQuit {
		t.Error("글 보기 q")
	}
	f := tuiLastFrame(r)
	if !strings.Contains(f, "미리보기 40") || strings.Contains(f, "미리보기 01") || strings.Contains(f, "벤더") || strings.Contains(f, "←→") {
		t.Errorf("글 보기 G:\n%s", f)
	}
}

func TestTUIHelp(t *testing.T) {
	u, r, act := browseKeys(t, "?q", tuiSampleVGs())
	if !strings.Contains(r.out.String(), "도움말") || act != TUIQuit || !u.Quit() {
		t.Errorf("? 뒤 q 는 도움말만 닫음 (그 뒤 입력 끝으로 종료): act=%v quit=%v", act, u.Quit())
	}
	if f := tuiLastFrame(r); strings.Contains(f, "아무 키나 누르면 닫힙니다") {
		t.Errorf("도움말이 닫혀야 함:\n%s", f)
	}
	_, r, _ = browseKeys(t, "?", tuiSampleVGs())
	if f := tuiLastFrame(r); !strings.Contains(f, "Tab / Shift+Tab") || !strings.Contains(f, "아무 키나 누르면 닫힙니다") {
		t.Errorf("도움말 화면:\n%s", f)
	}
}

// ---- 칸 맞춤·폭 ----

func TestTUIKoreanAlignment(t *testing.T) {
	lines := []ResultLine{
		{Tag: TagDiff, Host: "서버01", IP: "10.0.0.1", Vendor: "HPE", Model: "DL380", BiosVersion: "U30", Attr: "부팅모드", Expected: "기대값가", Current: "사용 안 함", Note: "FAIL"},
		{Tag: TagOK, Host: "srv02", IP: "10.0.0.2", Vendor: "HPE", Model: "DL380 Gen10", BiosVersion: "U30", Attr: "BootMode", Expected: "EXPVAL", Current: "Enabled", Note: "OK"},
	}
	tbl := tuiLineTable(false, 0, lines)
	col := func(row, sub string) int { return tuiStrWidth(row[:strings.Index(row, sub)]) }
	if a, b, h := col(tbl[1], "기대값가"), col(tbl[2], "EXPVAL"), col(tbl[0], "기준값"); a != b || a != h {
		t.Errorf("기준값 칸 위치가 달라짐: %d %d %d\n%s", a, b, h, strings.Join(tbl, "\n"))
	}
	if a, b := col(tbl[1], "FAIL"), tuiStrWidth(tbl[2][:strings.LastIndex(tbl[2], "OK")]); a != b {
		t.Errorf("비고 칸 위치: %d %d\n%s", a, b, strings.Join(tbl, "\n"))
	}
	// 색이 있어도 글자 위치는 같고, 좁은 화면에서는 폭 안으로 줄인다
	ctbl := tuiLineTable(true, 0, lines)
	if tuiStripANSI(ctbl[1]) != tbl[1] || !strings.Contains(ctbl[1], "\x1b[32m기대값가") || !strings.Contains(ctbl[1], "\x1b[33m사용 안 함") {
		t.Errorf("색 줄: %q", ctbl[1])
	}
	for _, l := range tuiLineTable(true, 60, lines) {
		if w := tuiStrWidth(tuiStripANSI(l)); w > 60 {
			t.Errorf("폭 60 초과(%d): %q", w, tuiStripANSI(l))
		}
	}
	he := tuiLineTable(true, 0, []ResultLine{{Tag: TagNA, Host: "d2", Attr: "-", Note: "UNREACHABLE", HostError: true}})
	if !strings.Contains(he[1], "\x1b[31m[N/A]") || !strings.Contains(he[1], "\x1b[31mUNREACHABLE") ||
		!strings.Contains(he[1], "\x1b[32m(N/A)") || strings.Contains(he[1], "\x1b[31md2") {
		t.Errorf("호스트 오류 줄은 태그·현재값·비고만 빨강, 기준값은 초록: %q", he[1])
	}
	// TUI 화면 줄은 모두 폭(w-1) 안
	u, r := newTUIRigReader(t, strings.NewReader("\r\x1b[C"), func(e *tuiEnv, r *tuiRig) { e.Color = true; r.w = 50 })
	u.SetResults("BIOS 점검 결과 — 프로파일 아주긴이름의프로파일", tuiSampleVGs())
	u.Browse()
	for _, l := range strings.Split(tuiLastFrame(r), "\n") {
		if tuiStrWidth(l) > 49 {
			t.Errorf("화면 폭 초과: %q", l)
		}
	}
	if o := r.out.String(); !strings.Contains(o, "\x1b[7m[iLO 5]") || !strings.Contains(o, "\x1b[1;4m[HPE]") {
		t.Error("색이 있으면 움직이는 줄의 선택 탭은 반전, 다른 줄은 굵은 밑줄")
	}
}

// ---- 평문 ----

func TestTUIWritePlain(t *testing.T) {
	var b bytes.Buffer
	tuiWritePlain(&b, "BIOS 점검 결과 — 프로파일 VM", tuiSampleVGs(), false)
	o := b.String()
	for _, want := range []string{"BIOS 점검 결과 — 프로파일 VM", "전체: OK 3  difference 2  N/A 2", "== HPE / iLO 4 (호스트 1대) OK 1  difference 1  N/A 0 ==",
		"== HPE / iLO 5 (호스트 1대)", "== DELL / iDRAC 9 (호스트 2대)", "[요약]", "[결과]", "AttrC5", "UNREACHABLE", "(N/A)"} {
		if !strings.Contains(o, want) {
			t.Errorf("평문에 %q 없음:\n%s", want, o)
		}
	}
	if strings.Contains(o, "\x1b") {
		t.Error("색 없는 평문에 ESC")
	}
	b.Reset()
	tuiWritePlain(&b, "x", tuiSampleVGs(), true)
	if !strings.Contains(b.String(), "\x1b[32m") {
		t.Error("color=true 면 색")
	}
}
