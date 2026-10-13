// work_tui_test.go - v0.8.0 gossh 작업 화면(work_tui.go): s 진입(-user·세션 user)·새 작업 흐름(붙여넣기→재사용→명령→실행→기록)·
// 항목 메뉴(새 명령·재실행·실패만 재실행 파생·결과 보기·메모·호스트 목록)·검색·골든(60/80/120)·60칸 줄바꿈.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type wkTUI struct {
	t  *testing.T
	st *tuiState
	rg *rig
}

func newWorkTUI(t *testing.T) *wkTUI {
	t.Helper()
	workTestEnv(t)
	saveSessionState(t)
	env, rg := newRig(strings.NewReader(""))
	st := &tuiState{src: &fakeSrc{snap: twoGroupSnap(), local: true}, env: env, pending: map[string]time.Time{}, restore: func() {}}
	st.sel.DateFilter = true
	st.refresh()
	st.draw()
	return &wkTUI{t: t, st: st, rg: rg}
}

func (h *wkTUI) keys(s string) {
	for _, ev := range parseKeys([]byte(s)) {
		h.st.handle(ev)
		h.st.pollOneoff()
		h.st.pollWork()
		h.st.draw()
	}
}

func (h *wkTUI) frame() string { return stripANSI(lastFrame(h.rg)) }

func (h *wkTUI) open(user string) {
	h.t.Helper()
	forceUser = user
	h.keys("s")
	if h.st.wk == nil {
		h.t.Fatal("s 로 gossh 작업 화면이 열려야 함")
	}
	h.st.wk.loc = time.UTC
	h.keys("r") // 다시 그리기 (목록 화면에서 r 은 무시)
}

func TestWorkOpenWithForceUserAndSessionUser(t *testing.T) {
	h := newWorkTUI(t)
	h.open("alice")
	f := h.frame()
	for _, want := range []string{"[user: alice] gossh 작업", "기록이 없습니다", "os6_mgmt/os6_gossh 미설정", "n 새 작업", "/ 호스트 검색", "Enter 선택", "q 닫기"} {
		if !strings.Contains(f, want) {
			t.Errorf("화면에 %q 없음:\n%s", want, f)
		}
	}
	h.keys("q")
	if h.st.wk != nil || strings.Contains(h.frame(), "gossh 작업 [") {
		t.Fatal("q 로 닫혀야 함")
	}
	// -user 없이: 처음 한 번 세션 user 를 고르고, 다음부터는 바로
	forceUser = ""
	h.keys("s")
	if h.st.oo == nil || h.st.oo.onUser == nil || h.st.wk != nil {
		t.Fatal("세션 user 선택 단계가 먼저 열려야 함")
	}
	h.keys("bob\r")
	if h.st.wk == nil || h.st.wk.user != "bob" || sessionUser != "bob" {
		t.Fatalf("bob 으로 열려야 함: wk=%v session=%q", h.st.wk, sessionUser)
	}
	h.keys("\x1b")
	h.keys("s")
	if h.st.oo != nil || h.st.wk == nil || h.st.wk.user != "bob" {
		t.Fatal("두 번째 s 는 묻지 않음")
	}
	if !strings.Contains(h.frame(), "[user: bob]") {
		t.Error("헤더 user")
	}
	// 원격 클라이언트(os6_mgmt)면 머리 안내가 다름
	h.keys("q")
	h.st.src = &fakeSrc{snap: twoGroupSnap(), local: false}
	os8_mgmt = "os8mgmt"
	var calls [][]string
	workRemoteRun = relayInProcess(t, &calls)
	h.keys("s")
	if h.st.wk == nil || !h.st.wk.client || !strings.Contains(h.frame(), "os6_mgmt 에서 실행 — 대수와 상관없이 이 서버의 gossh 로 바로 실행, 기록은 os8_mgmt(os8mgmt)") {
		t.Fatalf("원격 클라이언트 안내:\n%s", h.frame())
	}
	if len(calls) != 1 || calls[0][1] != "load" {
		t.Errorf("원격 클라이언트는 os8 기록을 읽음: %v", calls)
	}
}

func TestWorkNewRunReuseDeriveMemoSearch(t *testing.T) {
	h := newWorkTUI(t)
	os6_mgmt, os6_gossh = "os6mgmt", "/opt/gossh/gossh"
	var calls [][]string
	workScript = fakeGosshRun(t, &calls, map[string]bool{"svr003": true, "svr004": true}, true)
	h.open("alice")

	// 새 작업: 붙여넣기(공백·쉼표·줄바꿈, 중복) → 빈 줄로 끝
	h.keys("n")
	h.keys("svr001 svr002,svr003\rsvr002\rsvr004\r")
	if f := h.frame(); !strings.Contains(f, "입력 4대") || !strings.Contains(f, "svr004") {
		t.Fatalf("붙여넣기 화면:\n%s", f)
	}
	h.keys("\r")
	w := h.st.wk
	if w.step != wkCmd || !w.draft.NewList || !workNameRe.MatchString(w.draft.File) {
		t.Fatalf("명령 입력 단계: step=%d draft=%+v", w.step, w.draft)
	}
	file := w.draft.File
	if f := h.frame(); !strings.Contains(f, "파일: "+workPath(file)+" (새 목록) · svr001 등 4대") || !strings.Contains(f, "접속불가 호스트는 os6_mgmt 경유로 자동 재실행") {
		t.Fatalf("명령 화면:\n%s", f)
	}
	h.keys("cat /proc/uptime\r")
	if len(calls) != 2 || calls[0][0] != "gossh" || calls[0][2] != filepath.Join(workTmpDir, file) || calls[0][3] != "cat /proc/uptime" || calls[1][0] != "ssh" {
		t.Fatalf("실행: %v", calls)
	}
	if b, _ := os.ReadFile(filepath.Join(workTmpDir, file)); string(b) != "svr001\nsvr002\nsvr003\nsvr004\n" {
		t.Errorf("/tmp 파일: %q", b)
	}
	if h.st.restore == nil || h.rg.raws != 1 {
		t.Errorf("TUI 를 떠났다가 raw 로 돌아와야 함: raws=%d", h.rg.raws)
	}
	f := h.frame()
	if !strings.Contains(f, file+" : svr001 등 4대") || !strings.Contains(f, "cat /proc/uptime") || !strings.Contains(f, "성공 2 접속불가 2(os6 재실행 → 성공 2)") {
		t.Fatalf("목록:\n%s", f)
	}
	if !strings.Contains(f, file+": 성공 2 접속불가 2(os6 재실행 → 성공 2)") {
		t.Errorf("안내 줄:\n%s", f)
	}

	// 같은 목록(순서 달라도) → 재사용? y
	h.keys("n")
	h.keys("svr004 svr003 svr002 svr001\r\r")
	if w.step != wkReuse || !strings.Contains(h.frame(), file+" 와 같은 목록(4대) — 재사용? [y/n]") {
		t.Fatalf("재사용 질문:\n%s", h.frame())
	}
	h.keys("y")
	if w.step != wkCmd || w.draft.File != file || w.draft.NewList {
		t.Fatalf("재사용: %+v", w.draft)
	}
	calls = nil
	h.keys("true\r")
	if len(calls) != 2 || len(w.flat) != 2 || w.flat[0].Cmd != "true" || w.flat[0].File != file {
		t.Fatalf("같은 파일로 두 번째 실행: %d %d", len(calls), len(w.flat))
	}
	// n 이면 새 이름
	h.keys("n")
	h.keys("svr001 svr002 svr003 svr004\r\rn")
	if w.step != wkCmd || w.draft.File == file || !w.draft.NewList {
		t.Fatalf("새 목록: %+v", w.draft)
	}
	h.keys("\x1b")
	if w.step != wkList {
		t.Fatal("Esc 로 목록")
	}

	// 항목 메뉴 3: 실패·접속불가만 새 파일로 (os6 재실행으로 모두 성공했으면 없음)
	h.keys("\r")
	if w.step != wkMenu || !strings.Contains(h.frame(), "1 같은 파일로 새 명령  2 재실행  3 실패·접속불가만 재실행") {
		t.Fatalf("메뉴:\n%s", h.frame())
	}
	h.keys("3")
	if w.msg != "실패·접속불가 호스트가 없습니다" {
		t.Errorf("모두 성공: %q", w.msg)
	}
	// os6 도 실패하게 → 3 은 svr003·svr004 만 담은 새 파일(파생)
	workScript = func(argv []string, logPath string) (int, error) {
		calls = append(calls, argv)
		out := gosshOutput([]string{"svr001: ok", "svr002: ERROR: 종료코드 2", "svr003: ok", "svr004: ok"}, false, true)
		if argv[0] != "ssh" {
			os.Remove(argv[2] + "_res_off") // gossh 는 시작 때 이전 결과 파일을 지운다
		}
		if argv[0] == "ssh" {
			out = "ssh: connect to host os6mgmt port 22: Connection refused\r\n"
		}
		os.WriteFile(logPath, []byte(out), 0600)
		return 1, nil
	}
	h.keys("\r2y") // 재실행 (확인)
	if w.flat[0].Res.Ok != 3 || strings.Join(w.flat[0].Res.Fail, ",") != "svr002" {
		t.Fatalf("재실행 결과: %+v", w.flat[0].Res)
	}
	h.keys("\r3")
	if w.step != wkAsk || !strings.Contains(w.ask, "실패·접속불가 1대만 새 파일 "+workListDir()+"/") {
		t.Fatalf("3 확인: %q", w.ask)
	}
	h.keys("y")
	derived := w.flat[0]
	if derived.Parent != file || derived.File == file || derived.N != 1 {
		t.Fatalf("파생 기록: %+v", derived)
	}
	if b, _ := os.ReadFile(filepath.Join(workTmpDir, derived.File)); string(b) != "svr002\n" {
		t.Errorf("파생 파일 내용: %q", b)
	}
	if !strings.Contains(h.frame(), derived.File+" : svr002 1대  ("+file+" 의 실패·접속불가)") {
		t.Errorf("파생 표시:\n%s", h.frame())
	}

	// 5 메모 (파일 단위)
	h.keys("\r5LDAP 재적용 대상\r")
	if !strings.Contains(h.frame(), "(메모: LDAP 재적용 대상)") {
		t.Errorf("메모:\n%s", h.frame())
	}
	// 6 호스트 목록 (기준 실행의 결과와 함께)
	h.keys("\r6")
	if f := h.frame(); !strings.Contains(f, "호스트 목록 "+derived.File+" (1대)") || !strings.Contains(f, "svr002  실패") {
		t.Errorf("호스트 목록:\n%s", f)
	}
	h.keys("\x1b")
	// 4 결과 보기: less -R 에 압축을 푼 0600 파일
	var viewed string
	workLess = func(p string) error {
		b, _ := os.ReadFile(p)
		fi, _ := os.Stat(p)
		viewed = fmt.Sprintf("%v|%s", fi.Mode().Perm(), b)
		return nil
	}
	h.keys("\r4")
	if !strings.HasPrefix(viewed, "-rw-------|") || !strings.Contains(viewed, "svr002: ERROR: 종료코드 2") {
		t.Errorf("결과 보기: %.200q", viewed)
	}
	// 만료돼 지워졌으면 안내
	dir, _ := workUserDir("alice")
	os.RemoveAll(filepath.Join(dir, "out"))
	h.keys("\r4")
	if !strings.Contains(w.msg, "보관 기간(7일)이 지나 삭제") {
		t.Errorf("만료 안내: %q", w.msg)
	}

	// / 검색: svr002 → 이 호스트가 들어 있던 실행 전부, 시간순
	h.keys("/svr002\r")
	if w.step != wkSearch || len(w.found) != len(w.db.Runs) || w.found[0].At > w.found[len(w.found)-1].At {
		t.Fatalf("검색: step=%d %d/%d", w.step, len(w.found), len(w.db.Runs))
	}
	if f := h.frame(); !strings.Contains(f, "검색: svr002 (") || !strings.Contains(f, "→ svr002 실패") {
		t.Errorf("검색 화면:\n%s", f)
	}
	h.keys("\x1b/svr003\r")
	if len(w.found) != len(w.db.Runs)-1 { // 파생 파일에는 없음
		t.Errorf("svr003 검색: %d", len(w.found))
	}
	h.keys("\x1bq")
	if h.st.wk != nil {
		t.Fatal("닫기")
	}
	// 기록 저장 위치·권한
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0700 {
		t.Errorf("user 디렉터리 0700: %v", err)
	}
}

// 위험 명령: gossh 위험 작업 옵션을 붙이기 전에 한 번 묻는다 / os6 미설정 대량은 실행 전 확인
func TestWorkDangerAndBigAsk(t *testing.T) {
	h := newWorkTUI(t)
	var calls [][]string
	workScript = fakeGosshRun(t, &calls, nil, true)
	h.open("alice")
	h.keys("n")
	h.keys("a1 a2\r\r")
	h.keys("reboot\r")
	if h.st.wk.step != wkAsk || !strings.Contains(h.frame(), "위험 명령(reboot·halt 등)") {
		t.Fatalf("위험 명령 확인:\n%s", h.frame())
	}
	h.keys("n")
	if len(calls) != 0 || h.st.wk.msg != "취소했습니다" {
		t.Fatal("n 이면 실행 안 함")
	}
	h.keys("n")
	h.keys("a1 a2\r\r") // 실행하지 않았으므로 기록 없음 → 바로 새 목록
	h.keys("init 6\ry")
	if len(calls) != 1 || calls[0][3] != workDangerFlag || calls[0][4] != "init 6" {
		t.Fatalf("확인 후 위험 작업 옵션: %v", calls)
	}
	// os6 미설정 + 기준 초과 → 실행 전 확인
	work_os6_over = "2"
	calls = nil
	h.keys("n")
	h.keys("b1 b2 b3\r\r")
	h.keys("uptime\r")
	if h.st.wk.step != wkAsk || !strings.Contains(h.st.wk.ask, "3대는 기준 2대를 넘지만 os6_mgmt 가 설정되지 않아") {
		t.Fatalf("대량 확인: %q", h.st.wk.ask)
	}
	h.keys("y")
	if len(calls) != 1 || calls[0][0] != "gossh" {
		t.Fatalf("확인 후 os8 실행: %v", calls)
	}
}

// ---- 골든 ----

func goldenWorkState() *workState {
	w := &workState{user: "alice", loc: time.UTC, now: func() time.Time { return time.Unix(tsNow, 0) }}
	h20 := make([]string, 20)
	for i := range h20 {
		h20[i] = fmt.Sprintf("svr%03d", i+1)
	}
	h1200 := make([]string, 1200)
	for i := range h1200 {
		h1200[i] = fmt.Sprintf("spice%04d", i+101)
	}
	at := func(hh, mm int) int64 { return time.Date(2026, 10, 12, hh, mm, 0, 0, time.UTC).Unix() }
	db := &workDB{Memo: map[string]string{"qkz": "LDAP 재적용 대상"},
		Lists: map[string][]string{"qkz": h20, "fbw": h1200, "xyz": {"svr003", "svr007"}},
		Runs: []*workRec{
			{ID: "20261012-140200-0001", File: "qkz", At: at(14, 2), Side: "os8", Cmd: "cat /proc/uptime", N: 20,
				Res: &workRes{Ok: 18, Unr: []string{"svr003", "svr007"}}, O6: &workRes{Ok: 2}},
			{ID: "20261012-141000-0002", File: "qkz", At: at(14, 10), Side: "os8", Cmd: "systemctl is-active sssd", N: 20, Res: &workRes{Ok: 20}},
			{ID: "20261012-153000-0003", File: "fbw", At: at(15, 30), Side: "os6 전량", Cmd: "rpm -q kernel", N: 1200,
				Res: &workRes{Ok: 1198, Unr: []string{"spice0101", "spice0102"}}},
			{ID: "20261012-160000-0004", File: "xyz", Parent: "qkz", At: at(16, 0), Side: "os8", N: 2,
				Cmd: "grep -E '^(passwd|group):' /etc/nsswitch.conf | awk '{print $2, $3}'", Res: &workRes{Ok: 1, Fail: []string{"svr007"}}},
		}}
	w.setDB(db)
	w.notes = []workNote{{"", "실행: 1,000대 이하 os8 gossh (접속불가는 os6_mgmt 경유 재실행) · 초과는 전량 os6_mgmt(os6mgmt) 경유"},
		{stGray, "요약은 영구 보관 · 전체 출력은 7일(user 당 200MB) · /tmp 목록 파일은 7일 뒤 정리"}}
	return w
}

func TestWorkRenderGolden(t *testing.T) {
	saved, savedG := os6_mgmt, os6_gossh
	os6_mgmt, os6_gossh = "os6mgmt", "/opt/gossh/gossh"
	defer func() { os6_mgmt, os6_gossh = saved, savedG }()
	for _, color := range []bool{false, true} {
		mode := "plain"
		if color {
			mode = "color"
		}
		for _, wd := range []int{60, 80, 120} {
			var sb strings.Builder
			add := func(name string, s string) { sb.WriteString("== " + name + " ==\n" + s + "\n") }
			w := goldenWorkState()
			w.cur = 1
			add("list", renderWork(w, wd, 24, color))
			w.cur = 0
			w.sel, w.back, w.step = w.flat[0], wkList, wkMenu
			add("menu", renderWork(w, wd, 24, color))
			w.query, w.found, w.scur, w.step, w.back = "svr007", w.db.search("svr007"), 1, wkSearch, wkSearch
			w.sc = flowScroll{}
			add("search", renderWork(w, wd, 24, color))
			w.sel, w.step, w.sc = w.db.Runs[0], wkHosts, flowScroll{}
			add("hosts", renderWork(w, wd, 12, color))
			w.step, w.draft = wkCmd, workDraft{File: "xqa", NewList: true, Hosts: w.db.Lists["fbw"]}
			w.in.set("rpm -q kernel")
			add("cmd", renderWork(w, wd, 14, color))
			w.step, w.pasted, w.paste = wkPaste, []string{"svr001", "svr002", "svr003"}, pasteBuf{}
			w.paste.set("svr004")
			w.msg = ""
			add("paste", renderWork(w, wd, 14, color))
			got := goldenText(sb.String())
			name := "tui_work_" + mode + "_" + strconv.Itoa(wd) + ".golden"
			path := filepath.Join("testdata", name)
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got), 0644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("골든 없음 %s (go test -run WorkRenderGolden -update): %v", name, err)
			}
			if string(want) != got {
				t.Errorf("%s 불일치\n--- got ---\n%s", name, got)
			}
			for _, l := range strings.Split(sb.String(), "\n") { // 모든 줄이 폭 안 (줄바꿈 규칙, … 로 자르지 않음)
				if !strings.HasPrefix(l, "== ") && strWidth(stripANSI(l)) > wd {
					t.Errorf("%d칸 넘는 줄: %q", wd, l)
				}
			}
			if !color && strings.Contains(sb.String(), "\x1b[") {
				t.Error("NO_COLOR(plain) 에 색 코드")
			}
		}
	}
}
