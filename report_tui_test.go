package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 리포트 키 흐름·화면 시험. reportIO 에는 가짜를 넣는다.

type fakeRep struct {
	rules    []vendorRule
	eta      int
	warns    []string
	confErr  error
	base     *repBase
	days     int
	baseErr  error
	files    map[string]string
	saved    []string // "a|b"
	saveErr  error
	readPath string
	confArg  string
}

func (f *fakeRep) io() reportIO {
	return reportIO{
		conf: func(p string) ([]vendorRule, int, []string, error) {
			f.confArg = p
			return f.rules, f.eta, f.warns, f.confErr
		},
		baseline: func(cur []repHost, rules []vendorRule, today time.Time) (*repBase, int, error) {
			return f.base, f.days, f.baseErr
		},
		readFile: func(p string) (string, error) {
			f.readPath = p
			s, ok := f.files[p]
			if !ok {
				return "", errors.New("no such file")
			}
			return s, nil
		},
		save: func(today time.Time, snap repSnapshot, a, b string) (string, error) {
			if f.saveErr != nil {
				return "", f.saveErr
			}
			f.saved = append(f.saved, a+"|"+b)
			return "/state/report_20261105.txt", nil
		},
	}
}

func repActions(s *sent, f *fakeRep) tuiActions {
	a := s.actions()
	a.report = f.io()
	return a
}

func newRepFake() *fakeRep {
	return &fakeRep{
		rules: []vendorRule{{Name: "HPE", Patterns: []string{"ev0*"}}},
		eta:   2,
		base:  &repBase{Label: "저장본 2026-11-04", Vendors: map[string]*repBaseVendor{"hpe": {OSDone: intp(1), LoadDone: intp(0), Unreach: intp(1), Names: []string{"ev04-긴이름서버"}, HasNames: true}}},
		days:  1,
	}
}

func typeKeys(v *viewState, st *runState, a tuiActions, s string) {
	for _, r := range s {
		press(v, st, true, a, string(r))
	}
}

func TestReportPickKeys(t *testing.T) {
	for _, k := range []string{"p", "ㅔ", "P", "ㅖ"} {
		var s sent
		f := newRepFake()
		v := viewState{}
		press(&v, sampleState(), true, repActions(&s, f), k)
		if v.Rep != repPick {
			t.Errorf("%q: Rep=%q", k, v.Rep)
		}
	}
	// st 가 nil 이면 아무것도 안 함
	var s sent
	v := viewState{}
	press(&v, nil, true, repActions(&s, newRepFake()), "p")
	if v.Rep != "" || v.Flash != "" {
		t.Errorf("st nil: %+v", v)
	}
	// 그 밖의 키는 취소
	v = viewState{}
	st := sampleState()
	press(&v, st, true, repActions(&s, newRepFake()), "p", "x")
	if v.Rep != "" || v.Flash != "취소했습니다" {
		t.Errorf("취소: %+v", v)
	}
	// 호스트가 없으면 오류 화면, 파일 안 씀
	f := newRepFake()
	empty := sampleState()
	empty.Hosts = nil
	v = viewState{}
	press(&v, empty, true, repActions(&s, f), "p")
	if v.Rep != repErr || !strings.Contains(strings.Join(v.RepLines, ""), "호스트가 없어") || len(f.saved) != 0 {
		t.Errorf("호스트 없음: %+v", v)
	}
}

func TestReportMethod1(t *testing.T) {
	var s sent
	f := newRepFake()
	st := sampleState()
	st.ConfPath = "/x/new_setup.conf"
	v := viewState{}
	a := repActions(&s, f)
	press(&v, st, true, a, "p", "1")
	if f.confArg != "/x/new_setup.conf" {
		t.Errorf("ConfPath 가 conf 로 안 넘어감: %q", f.confArg)
	}
	if v.Rep != repShow || len(f.saved) != 1 || v.RepPath != "/state/report_20261105.txt" || v.RepLabel != "저장본 2026-11-04" {
		t.Fatalf("방식 1: %+v saved=%v", v, f.saved)
	}
	if !strings.Contains(strings.Join(v.RepLines, "\n"), "HPE:") {
		t.Errorf("리포트 글: %q", v.RepLines)
	}
	// 어제가 아니면 n일 전
	f.days = 3
	v = viewState{}
	press(&v, st, true, a, "p", "1")
	if v.RepLabel != "저장본 2026-11-04(3일 전)" {
		t.Errorf("label=%q", v.RepLabel)
	}
	// 기준 없음 -> err 문구 그대로
	f.baseErr = errNoSnapshot
	v = viewState{}
	press(&v, st, true, a, "p", "1")
	if v.Rep != repErr || v.RepLines[0] != errNoSnapshot.Error() {
		t.Errorf("err: %+v", v)
	}
	// 아무 키나 누르면 닫힘
	press(&v, st, true, a, "z")
	if v.Rep != "" {
		t.Errorf("err 닫힘: %q", v.Rep)
	}
	// 목록 다름
	f.baseErr = errors.New("호스트 목록이 다릅니다. 저장본 2026-11-04 3대, 현재 4대. 저장본에만 있는 호스트 0대, 현재에만 있는 호스트 1대. 2(코멘트 입력)를 쓰십시오.")
	press(&v, st, true, a, "p", "1")
	if v.Rep != repErr || !strings.Contains(v.RepLines[0], "호스트 목록이 다릅니다") {
		t.Errorf("목록 다름: %+v", v)
	}
}

func TestReportNotesAndWarns(t *testing.T) {
	var s sent
	f := newRepFake()
	f.rules = nil
	f.warns = []string{"설정 REPORT_ETA_DAYS 값 \"x\" 을(를) 읽을 수 없어 기본값 2 를 씁니다(0 이상의 정수)"}
	f.confErr = errors.New("읽기 실패")
	v := viewState{}
	press(&v, sampleState(), true, repActions(&s, f), "p")
	j := strings.Join(v.RepWarns, "\n")
	if !strings.Contains(j, "REPORT_ETA_DAYS") || !strings.Contains(j, "읽기 실패") || !strings.Contains(j, repNoVendor) {
		t.Errorf("warns=%q", j)
	}
}

func TestReportMethod2Input(t *testing.T) {
	var s sent
	f := newRepFake()
	st := sampleState()
	v := viewState{}
	a := repActions(&s, f)
	press(&v, st, true, a, "p", "2")
	if v.Rep != repInput {
		t.Fatalf("Rep=%q", v.Rep)
	}
	// 공백·대문자·한글이 그대로 들어가고, q·p 가 TUI 를 끝내거나 열지 않는다
	if press(&v, st, true, a, "q", " ", "A", "ㅂ", "p", "1") {
		t.Fatal("입력 중 q 가 TUI 를 끝냄")
	}
	if string(v.RepInput) != "q Aㅂp1" {
		t.Errorf("입력=%q", string(v.RepInput))
	}
	press(&v, st, true, a, "enter", "tab", "backspace", "backspace")
	if string(v.RepInput) != "q Aㅂp1" {
		t.Errorf("enter/tab/backspace: %q", string(v.RepInput))
	}
	press(&v, st, true, a, "up", "pgdn", "ctrl-l")
	if string(v.RepInput) != "q Aㅂp1" {
		t.Errorf("이름 있는 키가 글에 섞임: %q", string(v.RepInput))
	}
	// Esc 로 취소
	press(&v, st, true, a, "esc")
	if v.Rep != "" || v.Flash != "취소했습니다" {
		t.Errorf("esc: %+v", v)
	}
	// Ctrl-C 는 끝냄
	press(&v, st, true, a, "p", "2")
	if !press(&v, st, true, a, "ctrl-c") {
		t.Errorf("ctrl-c 가 TUI 를 못 끝냄")
	}
}

const yesterday = `HPE:
    OS설치 3대 중 2대 완료, 1대 OS 설치 중 (전일대비 +0대)
    부하테스트 2대 중 1대 완료, 전일대비 +0대, 11/07 완료예정
    접속불가서버 1대 (전일대비 +0대)
        접속불가
          - ev03
`

func TestReportMethod2Flow(t *testing.T) {
	var s sent
	f := newRepFake()
	st := sampleState()
	v := viewState{}
	a := repActions(&s, f)
	press(&v, st, true, a, "p", "2")
	for _, l := range strings.Split(yesterday, "\n") {
		typeKeys(&v, st, a, l)
		press(&v, st, true, a, "enter")
	}
	press(&v, st, true, a, "ctrl-d")
	if v.Rep != repConfirm {
		t.Fatalf("Rep=%q", v.Rep)
	}
	j := strings.Join(v.RepLines, "\n")
	if !strings.Contains(j, "HPE  OS완료 2  부하완료 1  접속불가 1 (ev03)") || !strings.Contains(j, "기타  전일 정보 없음") {
		t.Errorf("확인 화면:\n%s", j)
	}
	// y -> show
	press(&v, st, true, a, "y")
	if v.Rep != repShow || len(f.saved) != 1 || v.RepLabel != "코멘트" {
		t.Fatalf("y: %+v", v)
	}
	// 다른 키로는 닫힘(만들지 않음)
	f.saved = nil
	v = viewState{}
	press(&v, st, true, a, "p", "2", "x", "ctrl-d", "n")
	if v.Rep != "" || len(f.saved) != 0 || v.Flash != "취소했습니다" {
		t.Errorf("확인 취소: %+v saved=%v", v, f.saved)
	}
	// 빈 코멘트: 확인 화면은 모든 벤더 전일 정보 없음, y 로 만든다
	v = viewState{}
	press(&v, st, true, a, "p", "2", "ctrl-d")
	if v.Rep != repConfirm || !strings.Contains(strings.Join(v.RepLines, "\n"), "HPE  전일 정보 없음") {
		t.Errorf("빈 코멘트: %+v", v)
	}
	press(&v, st, true, a, "y")
	if v.Rep != repShow {
		t.Errorf("빈 코멘트 y: %q", v.Rep)
	}
	// 설정에 없는 벤더 알림과 현재 목록에 없는 이름
	v = viewState{}
	press(&v, st, true, a, "p", "2")
	typeKeys(&v, st, a, "LENOVO:")
	press(&v, st, true, a, "enter")
	typeKeys(&v, st, a, "HPE:")
	press(&v, st, true, a, "enter")
	typeKeys(&v, st, a, "접속불가서버 1대")
	press(&v, st, true, a, "enter")
	typeKeys(&v, st, a, "- gone-9")
	press(&v, st, true, a, "ctrl-d")
	j = strings.Join(v.RepLines, "\n")
	if !strings.Contains(j, "알림: 설정에 없는 벤더: LENOVO") || !strings.Contains(j, "알림: 현재 목록에 없는 이름 1개") {
		t.Errorf("알림:\n%s", j)
	}
}

func TestReportAtFile(t *testing.T) {
	var s sent
	f := newRepFake()
	f.files = map[string]string{"/tmp/y.txt": yesterday}
	st := sampleState()
	v := viewState{}
	a := repActions(&s, f)
	press(&v, st, true, a, "p", "2")
	typeKeys(&v, st, a, "@/tmp/y.txt")
	press(&v, st, true, a, "ctrl-d")
	if f.readPath != "/tmp/y.txt" || v.Rep != repConfirm || !strings.Contains(strings.Join(v.RepLines, "\n"), "OS완료 2") {
		t.Errorf("@경로: path=%q %+v", f.readPath, v)
	}
	// 없는 파일
	v = viewState{}
	press(&v, st, true, a, "p", "2")
	typeKeys(&v, st, a, "@/nope")
	press(&v, st, true, a, "ctrl-d")
	if v.Rep != repErr || !strings.Contains(v.RepLines[0], "코멘트 파일을 읽을 수 없습니다: ") {
		t.Errorf("없는 파일: %+v", v)
	}
	// 너무 긴 코멘트
	v = viewState{}
	press(&v, st, true, a, "p", "2")
	v.RepInput = []rune(strings.Repeat("a\n", 2001))
	press(&v, st, true, a, "ctrl-d")
	if v.Rep != repErr || !strings.Contains(v.RepLines[0], "너무 깁니다") {
		t.Errorf("긴 코멘트: %+v", v)
	}
}

func TestReportSaveFailureKeepsReport(t *testing.T) {
	var s sent
	f := newRepFake()
	f.saveErr = errors.New("디스크 가득")
	st := sampleState()
	v := viewState{}
	press(&v, st, true, repActions(&s, f), "p", "1")
	if v.Rep != repShow || !strings.HasPrefix(v.RepPath, "저장 실패: 디스크 가득") || len(v.RepLines) == 0 {
		t.Fatalf("%+v", v)
	}
	frame := renderRun(st, viewState{NoColor: true, Rep: v.Rep, RepLines: v.RepLines, RepPath: v.RepPath, RepLabel: v.RepLabel, RepDate: t0}, 120, 20, t0, true)
	if !strings.Contains(frame, "저장 실패: 디스크 가득") || !strings.Contains(frame, "HPE:") {
		t.Errorf("화면:\n%s", frame)
	}
}

func TestReportTwoVersions(t *testing.T) {
	var s sent
	f := newRepFake()
	// 현재 접속불가 0대 + 기준에는 있음: 두 본
	st := sampleState()
	st.Hosts[3].Status = stDone
	v := viewState{}
	press(&v, st, true, repActions(&s, f), "p", "1")
	j := strings.Join(v.RepLines, "\n")
	if !strings.Contains(j, repTitleA) || !strings.Contains(j, repTitleB) || len(f.saved) != 1 || !strings.Contains(f.saved[0], "|") || strings.HasSuffix(f.saved[0], "|") {
		t.Fatalf("두 본:\n%s\nsaved=%q", j, f.saved)
	}
	if strings.Contains(f.saved[0], "[본") {
		t.Errorf("제목이 파일 글에 들어감: %q", f.saved[0])
	}
	if strings.Index(j, repTitleA) > strings.Index(j, repTitleB) {
		t.Errorf("본 A 가 본 B 앞에 와야 함")
	}
}

func TestReportScroll(t *testing.T) {
	var s sent
	f := newRepFake()
	st := sampleState()
	v := viewState{}
	a := repActions(&s, f)
	press(&v, st, true, a, "p", "1")
	v.RepLines = nil
	for i := 0; i < 50; i++ {
		v.RepLines = append(v.RepLines, "줄")
	}
	// press 는 h=20: 본문 칸 = 20-3-경고 수
	cp := repBodyCap(&v, 20)
	press(&v, st, true, a, "down")
	if v.RepScroll != 1 {
		t.Errorf("down: %d", v.RepScroll)
	}
	press(&v, st, true, a, "up", "up")
	if v.RepScroll != 0 {
		t.Errorf("up 아래로 안 내려감: %d", v.RepScroll)
	}
	press(&v, st, true, a, "pgdn")
	if v.RepScroll != cp {
		t.Errorf("pgdn: %d != %d", v.RepScroll, cp)
	}
	press(&v, st, true, a, " ")
	if want := 50 - cp; v.RepScroll != want { // 공백은 pgdn(normKey). 2*cp 는 끝을 넘으므로 끝에 멈춘다
		t.Errorf("space: %d != %d", v.RepScroll, want)
	}
	press(&v, st, true, a, "end")
	if v.RepScroll != 50-cp {
		t.Errorf("end: %d", v.RepScroll)
	}
	press(&v, st, true, a, "home")
	if v.RepScroll != 0 || v.Rep != repShow {
		t.Errorf("home: %d %q", v.RepScroll, v.Rep)
	}
	press(&v, st, true, a, "q") // 그 밖의 키는 닫힘(TUI 는 안 끝남)
	if v.Rep != "" {
		t.Errorf("닫힘: %q", v.Rep)
	}
}

// 입력 질문이 떠 있어도 리포트가 열린 동안 숫자는 칸으로 가지 않는다. 닫으면 칸 입력으로 돌아온다.
func TestReportDigitsNotToFields(t *testing.T) {
	var s sent
	f := newRepFake()
	f.baseErr = errNoSnapshot
	st := inputState()
	v := viewState{}
	a := repActions(&s, f)
	press(&v, st, true, a, "2") // 칸에 '2'
	before := map[string]string{}
	for k, x := range v.InVals {
		before[k] = x
	}
	idx := v.InIdx
	press(&v, st, true, a, "p", "1") // 1 은 리포트의 방식 1(기준 없음 → 오류 화면)
	if v.Rep != repErr {
		t.Fatalf("Rep=%q", v.Rep)
	}
	press(&v, st, true, a, "z") // 닫음
	for k, x := range before {
		if v.InVals[k] != x {
			t.Errorf("칸 %s 값이 바뀜: %q -> %q", k, x, v.InVals[k])
		}
	}
	if v.InIdx != idx || v.Rep != "" {
		t.Errorf("칸 위치 %d != %d, Rep=%q", v.InIdx, idx, v.Rep)
	}
	press(&v, st, true, a, "3") // 닫은 뒤에는 칸 입력으로 돌아온다
	if v.InVals[st.Question.Inputs[v.InIdx].Key] == "" && v.InIdx == idx {
		t.Errorf("닫은 뒤 칸 입력이 안 돌아옴: %+v", v.InVals)
	}
}

func TestParseKeysCtrlD(t *testing.T) {
	if got := strings.Join(parseKeys([]byte{0x04}), ","); got != "ctrl-d" {
		t.Errorf("ctrl-d=%q", got)
	}
	if got := strings.Join(parseKeys([]byte("a\x04b")), ","); got != "a,ctrl-d,b" {
		t.Errorf("%q", got)
	}
}

func TestKeyHintsAndHelpHaveReport(t *testing.T) {
	found := false
	for _, k := range keyHints(sampleState(), viewState{}, true) {
		if k == "p 리포트" {
			found = true
		}
	}
	if !found {
		t.Errorf("keyHints 에 p 리포트 없음")
	}
	if !strings.Contains(strings.Join(helpLines(300, true), "\n"), "p  벤더별 리포트") {
		t.Errorf("helpLines 에 p 없음")
	}
}

func TestRenderReportScreens(t *testing.T) {
	st := sampleState()
	st.Updated = t0
	body := []string{"HPE:", "    OS설치 3대 중 2대 완료", "    접속불가서버 1대 (전일대비 +0대)"}
	cases := []struct {
		name string
		v    viewState
		want []string
	}{
		{"pick", viewState{Rep: repPick, RepWarns: []string{repNoVendor}}, []string{
			"리포트 — 전일대비 기준을 고르십시오",
			"  1  전일 저장본(상태 폴더의 report_YYYYMMDD.json)",
			"  2  코멘트 입력(전날 리포트 글 붙여넣기 또는 @파일)",
			"  다른 키  취소", "! " + repNoVendor}},
		{"input", viewState{Rep: repInput, RepInput: []rune("HPE:\n  OS설치 3대")}, []string{
			"전날 리포트 글을 붙여넣으십시오 — Ctrl-D 완료, Esc 취소, 파일은 첫 줄에 @경로", "HPE:", "  OS설치 3대_", "2줄 입력됨"}},
		{"input-empty", viewState{Rep: repInput}, []string{"_", "0줄 입력됨"}},
		{"confirm", viewState{Rep: repConfirm, RepLines: []string{"코멘트에서 읽은 내용 (y 계속 / 다른 키 취소)", "  HPE  OS완료 2", "알림: 설정에 없는 벤더: LENOVO"}}, []string{
			"코멘트에서 읽은 내용 (y 계속 / 다른 키 취소)", "  HPE  OS완료 2", "알림: 설정에 없는 벤더: LENOVO"}},
		{"show", viewState{Rep: repShow, RepLines: body, RepLabel: "저장본 2026-11-04", RepDate: t0, RepPath: "/s/report_20261105.txt"}, []string{
			"리포트 2026-11-05 — 기준: 저장본 2026-11-04 — 판정 시각 11/05 09:00:00 — 저장: /s/report_20261105.txt",
			"HPE:", "    OS설치 3대 중 2대 완료", "    접속불가서버 1대 (전일대비 +0대)"}},
		{"err", viewState{Rep: repErr, RepLines: []string{"전일 저장본이 없습니다. 2(코멘트 입력)를 쓰십시오."}}, []string{
			"리포트를 만들 수 없습니다", "  전일 저장본이 없습니다. 2(코멘트 입력)를 쓰십시오.", "아무 키나 누르면 닫힘"}},
	}
	for _, c := range cases {
		c.v.NoColor = true
		for _, size := range [][2]int{{100, 20}, {50, 8}, {30, 3}, {100, 1}} {
			checkFrame(t, c.name, renderRun(st, c.v, size[0], size[1], t0, true), size[0], size[1])
		}
		frame := renderRun(st, c.v, 120, 20, t0, true)
		lines := checkFrame(t, c.name, frame, 120, 20)
		for _, w := range c.want {
			found := false
			for _, l := range lines {
				if strings.TrimRight(l, " ") == w {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: 줄 %q 없음\n%s", c.name, w, frame)
			}
		}
		if strings.Contains(frame, "new_setup -run |") {
			t.Errorf("%s: 리포트 화면에 기본 화면이 섞임", c.name)
		}
	}
	// show 의 마지막 줄은 스크롤 안내 줄이다
	f := renderRun(st, viewState{NoColor: true, Rep: repShow, RepLines: body, RepDate: t0}, 120, 12, t0, true)
	if ls := strings.Split(f, "\n"); !strings.Contains(ls[len(ls)-1], "스크롤") || !strings.Contains(ls[len(ls)-1], "1-3 / 3줄") {
		t.Errorf("show 마지막 줄: %q", ls[len(ls)-1])
	}
}

func TestReportShowScrollWindow(t *testing.T) {
	st := sampleState()
	var body []string
	for i := 0; i < 30; i++ {
		body = append(body, "줄"+string(rune('A'+i%26)))
	}
	v := viewState{NoColor: true, Rep: repShow, RepLines: body, RepDate: t0, RepScroll: 10}
	frame := renderRun(st, v, 80, 10, t0, true)
	lines := checkFrame(t, "scroll", frame, 80, 10)
	if lines[2] != body[10] || !strings.Contains(lines[9], "11-17 / 30줄") {
		t.Errorf("스크롤 창:\n%s", frame)
	}
}

func TestSaveReportFiles(t *testing.T) {
	dir := t.TempDir()
	today := time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local)
	snap := makeSnapshot([]repHost{{Host: "a", Vendor: "기타", Status: stDone}}, today)
	p, err := saveReportFiles(dir, today, snap, "A글\n", "B글\n")
	if err != nil || filepath.Base(p) != "report_20261112.txt" {
		t.Fatalf("%v %q", err, p)
	}
	for name, want := range map[string]string{"report_20261112.txt": "A글\n", "report_20261112_B.txt": "B글\n"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(b) != want {
			t.Errorf("%s: %v %q", name, err, b)
		}
	}
	if _, err := readSnapshot(filepath.Join(dir, "report_20261112.json")); err != nil {
		t.Errorf("스냅샷: %v", err)
	}
	// 본이 하나가 되면 옛 _B 파일을 지운다
	if _, err := saveReportFiles(dir, today, snap, "A글\n", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "report_20261112_B.txt")); !os.IsNotExist(err) {
		t.Errorf("_B 가 안 지워짐: %v", err)
	}
	// 쓰기 실패
	if _, err := saveReportFiles(filepath.Join(dir, "없는폴더"), today, snap, "A", ""); err == nil {
		t.Errorf("없는 폴더에 쓰기가 성공함")
	}
}

func TestNewReportIOConf(t *testing.T) {
	dir := t.TempDir()
	rio := newReportIO(dir)
	// 빈 경로: 규칙 없음, 오류 없음
	r, eta, w, err := rio.conf("")
	if r != nil || eta != defaultReportETADays || w != nil || err != nil {
		t.Errorf("빈 경로: %v %d %v %v", r, eta, w, err)
	}
	// 없는 파일: 오류
	if _, _, _, err := rio.conf(filepath.Join(dir, "none.conf")); err == nil {
		t.Errorf("없는 설정 파일이 오류가 아님")
	}
	cp := filepath.Join(dir, "x.conf")
	if err := os.WriteFile(cp, []byte("VENDOR_HPE=hpe*,dl3*\nVENDOR_DELL=dell*\nREPORT_ETA_DAYS=x\nSTRESS_IGNORE_OFFLINE=zz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, eta, w, err = rio.conf(cp)
	if err != nil || len(r) != 2 || r[0].Name != "HPE" || eta != defaultReportETADays || len(w) != 1 || !strings.Contains(w[0], "REPORT_ETA_DAYS") {
		t.Errorf("conf: %v %d %v %v", r, eta, w, err)
	}
	// 저장본이 없으면 오류
	if _, _, err := rio.baseline([]repHost{{Host: "a"}}, r, time.Now()); err == nil {
		t.Errorf("저장본이 없는데 오류가 아님")
	}
	if b, err := rio.readFile(cp); err != nil || !strings.Contains(b, "VENDOR_HPE") {
		t.Errorf("readFile: %v", err)
	}
}
