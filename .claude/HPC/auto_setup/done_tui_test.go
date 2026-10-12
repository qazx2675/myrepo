// done_tui_test.go - d 수동 완료(v0.7.1): 인라인 프롬프트·확인·취소·allView·완료 호스트·종료된 job, 배타 처리,
// 원격 클라이언트 중계, 안내 줄 폭, 도움말, 골든. 로컬 실행은 AUTO_SETUP_DIR(chgTUISetup 이 임시 디렉터리로 지정)에 완료기록을 쓴다.
package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doneSnap: a.yml(a1 완료, a2·a3 설치중·배포중) / b.yml(전부 완료) / c.yml(c1·c2 대기)
func doneSnap() Snapshot {
	return tsSnap(true, tsJob("J1", "user1", false,
		tsGroup("a.yml", "ib", "rhel8", "pxe", "y", tsHost("a1", StageDone, 10, ""), tsHost("a2", StageInstalling, 500, ""), tsHost("a3", StageDeploying, 300, "")),
		tsGroup("b.yml", "ib", "rhel8", "pxe", "y", tsHost("b1", StageDone, 10, ""), tsHost("b2", StageDone, 20, "")),
		tsGroup("c.yml", "ib", "rhel8", "pxe", "y", tsHost("c1", StageQueued, 0, ""), tsHost("c2", StageQueued, 0, ""))))
}

// doneTUI: 변경 메뉴는 켜 두고(m 과의 배타 확인) doneSnap 으로 시작하는 TUI
func doneTUI(t *testing.T) *chgTUI {
	h := chgTUISetup(t, true, true, false)
	h.st.src.(*fakeSrc).snap = doneSnap()
	h.st.refresh()
	h.keys("r")
	return h
}

// enter: 화면 1 에서 yml 그룹의 화면 2 로, host 에 커서 (host "" = 첫 호스트)
func (h *chgTUI) enter(yml, host string) {
	h.t.Helper()
	h.st.sel = selection{DateFilter: true, Detail: true, JobID: "J1", Yml: yml}
	if host != "" {
		_, g := findGroup(h.st.snap, "J1", yml)
		for i, x := range visibleHosts(g, false) {
			if x.Host == host {
				h.st.sel.Host = i
			}
		}
	}
	h.keys("r") // 다시 그리기
	h.st.sel.Msg = ""
}

// squash: 줄바꿈·공백을 모두 빼서 줄바꿈된 문장을 비교한다
func squash(s string) string { return strings.Join(strings.Fields(s), "") }

func (h *chgTUI) doneFile(host string) string {
	b, err := os.ReadFile(filepath.Join(os.Getenv("AUTO_SETUP_DIR"), "done", host))
	if err != nil {
		return ""
	}
	return string(b)
}

const (
	donePromptHost = "수동 완료 ▶ 1 이 호스트(a2)  2 그룹 미완료 2대   (Esc 취소)"
	doneWarn       = "완료로 처리합니다 — 부팅 시각 검증 없이 인정되고 체크스크립트는 실행되지 않습니다. 계속? (y/n)"
)

// 호스트 / 그룹 선택 → 확인 → 완료기록 (출처 manual) + 메시지
func TestDoneKeyHostAndGroup(t *testing.T) {
	h := doneTUI(t)
	h.enter("a.yml", "a2")
	h.keys("d")
	f := h.frame()
	if h.st.done == nil || !strings.Contains(f, donePromptHost) || !strings.Contains(f, "h 호스트 작업") {
		t.Fatalf("프롬프트:\n%s", f)
	}
	lines := strings.Split(f, "\n")
	if i := len(lines) - 1; !strings.Contains(lines[i], "수동 완료 ▶") {
		t.Errorf("프롬프트는 안내 줄 바로 아래(맨 아래)에:\n%s", f)
	}
	h.keys("1")
	if f := h.frame(); !strings.Contains(squash(f), squash("호스트 a2 을(를) "+doneWarn)) {
		t.Fatalf("확인 줄:\n%s", f)
	}
	h.keys("y")
	if h.st.done != nil || !strings.Contains(h.frame(), "완료 처리 요청: 1대 (데몬이 5초 안에 반영)") {
		t.Fatalf("요청 메시지:\n%s", h.frame())
	}
	if r := h.doneFile("a2"); !strings.HasSuffix(strings.TrimSpace(r), " - manual") || h.doneFile("a3") != "" || h.doneFile("a1") != "" {
		t.Fatalf("완료기록: a2=%q a3=%q a1=%q", r, h.doneFile("a3"), h.doneFile("a1"))
	}
	// 그룹: 미완료 전체 (완료 a1 제외)
	h.enter("a.yml", "a3")
	h.keys("d2")
	if f := h.frame(); !strings.Contains(squash(f), squash("그룹 a.yml 의 미완료 2대 을(를) "+doneWarn)) {
		t.Fatalf("그룹 확인 줄:\n%s", f)
	}
	h.keys("y")
	if h.doneFile("a3") == "" || h.doneFile("a1") != "" {
		t.Fatalf("그룹 완료기록: a3=%q a1=%q", h.doneFile("a3"), h.doneFile("a1"))
	}
}

// 취소: Esc / q / 확인에서 y 가 아닌 키 / 선택 중 다른 키는 무시
func TestDoneKeyCancelAndIgnore(t *testing.T) {
	h := doneTUI(t)
	h.enter("a.yml", "a2")
	h.keys("d\x1b")
	if h.st.done != nil || !strings.Contains(h.frame(), "취소했습니다") {
		t.Fatalf("Esc 취소:\n%s", h.frame())
	}
	h.keys("dq")
	if h.st.done != nil || !strings.Contains(h.frame(), "취소했습니다") {
		t.Fatalf("q 취소(TUI 종료 아님):\n%s", h.frame())
	}
	h.keys("d")
	for _, k := range []string{"z", "3", "\r", "\x1b[B", "?", "r", "\x03"} { // 선택 중에는 1/2/Esc/q 외 무시
		h.keys(k)
		if h.st.done == nil || h.st.done.confirm || h.st.help {
			t.Fatalf("%q 는 무시여야 함", k)
		}
	}
	h.keys("1n")
	if h.st.done != nil || h.doneFile("a2") != "" || !strings.Contains(h.frame(), "취소했습니다") {
		t.Fatalf("n 은 취소:\n%s", h.frame())
	}
	h.keys("d1\r") // Enter 도 취소
	if h.st.done != nil || h.doneFile("a2") != "" {
		t.Fatal("y 가 아니면 진행하지 않음")
	}
	if h.handleKeyQuits("dq") { // 프롬프트 중 q 는 프롬프트만 닫는다
		t.Fatal("프롬프트 q 가 TUI 를 종료함")
	}
}

// 전체 그룹 보기(allView): 1 만. 완료 호스트·모두 완료인 그룹·종료된 job 은 안내만
func TestDoneKeyAllViewAndNoTargets(t *testing.T) {
	h := doneTUI(t)
	h.enter("a.yml", "a2")
	h.keys("a") // 전체 그룹 보기
	_, g := findGroup(h.st.snap, "J1", allView)
	for i, x := range visibleHosts(g, false) {
		if x.Host == "c1" {
			h.st.sel.Host = i
		}
	}
	h.keys("d")
	f := h.frame()
	if h.st.done == nil || !strings.Contains(f, "수동 완료 ▶ 1 이 호스트(c1)") || strings.Contains(f, "2 그룹 미완료") {
		t.Fatalf("allView 는 1 만:\n%s", f)
	}
	h.keys("2") // 없는 선택지는 무시
	if h.st.done == nil || h.st.done.confirm {
		t.Fatal("allView 에서 2 는 무시")
	}
	h.keys("\x1b")
	// 완료 호스트: 호스트 선택지 없이 그룹만
	h.enter("a.yml", "a1")
	h.keys("d")
	if f := h.frame(); h.st.done == nil || strings.Contains(f, "1 이 호스트") || !strings.Contains(f, "수동 완료 ▶ 2 그룹 미완료 2대") {
		t.Fatalf("완료 호스트면 그룹만:\n%s", f)
	}
	h.keys("\x1b")
	// 전부 완료인 그룹: 안내만
	h.enter("b.yml", "b1")
	h.keys("d")
	if f := h.frame(); h.st.done != nil || !strings.Contains(f, "이 호스트(b1)와 그룹 모두 이미 완료입니다") {
		t.Fatalf("모두 완료 안내:\n%s", f)
	}
	// 종료된 job
	s := doneSnap()
	s.Jobs[0].Closed = true
	h.st.src.(*fakeSrc).snap = s
	h.st.refresh()
	h.enter("a.yml", "a2")
	h.keys("d")
	if f := h.frame(); h.st.done != nil || !strings.Contains(f, "종료된 작업은 완료 처리할 수 없습니다") {
		t.Fatalf("종료된 job 안내:\n%s", f)
	}
}

// 변경 메뉴(m)·x·w 와 동시에 열리지 않는다 — 어느 쪽이 먼저 열려 있든 상대는 무시
func TestDoneKeyExclusiveWithMenuAndOthers(t *testing.T) {
	h := doneTUI(t)
	h.enter("a.yml", "a2")
	h.keys("d")
	h.keys("mMxXwWhHsS")
	if h.st.chgMenu != nil || h.st.menu != nil || h.st.oo != nil || h.st.awx != nil || h.st.done == nil || h.st.done.confirm {
		t.Fatalf("프롬프트 중 m/x/w 는 무시여야 함:\n%s", h.frame())
	}
	h.keys("\x1b")
	h.keys("m")
	if h.st.chgMenu == nil {
		t.Fatal("m 메뉴")
	}
	h.keys("d")
	if h.st.done != nil || h.st.chgMenu == nil {
		t.Fatal("메뉴가 열려 있는 동안 d 는 무시여야 함")
	}
	if strings.Contains(h.frame(), "수동 완료 ▶") {
		t.Fatal("두 인라인 줄이 동시에 보이면 안 됨")
	}
}

// 원격 클라이언트(os6_mgmt): 로컬 완료기록을 쓰지 않고 os8_mgmt 의 `auto_setup done` 으로 중계
func TestDoneKeyRemoteRelay(t *testing.T) {
	h := doneTUI(t)
	os8_mgmt = "os8.test"
	h.st.src = &fakeSrc{snap: doneSnap(), local: false}
	h.st.refresh()
	var calls [][]string
	rc, stderr := "0", ""
	oldRun := runExternal
	runExternal = func(stdin, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		return "os8.test: " + markRC + rc + "\nos8.test: " + markO + "\nos8.test: " + markE + "\nos8.test: " +
			base64.StdEncoding.EncodeToString([]byte(stderr)) + "\n", nil
	}
	defer func() { runExternal = oldRun }()
	lastCmd := func() string { return calls[len(calls)-1][len(calls[len(calls)-1])-1] }

	h.enter("a.yml", "a2")
	h.keys("d1y")
	if len(calls) != 1 || calls[0][0] != "gossh" || lastCmd() != remoteCommand([]string{"done", "a2"}) {
		t.Fatalf("원격 호스트 중계 인자: %v", calls)
	}
	if !strings.Contains(h.frame(), "완료 처리 요청: 1대 (데몬이 5초 안에 반영)") {
		t.Fatalf("원격 성공 메시지:\n%s", h.frame())
	}
	if h.doneFile("a2") != "" {
		t.Fatal("원격 클라이언트는 로컬에 완료기록을 쓰면 안 됨")
	}
	h.keys("d2y") // 그룹은 --job/--yml 로 (큰 그룹도 인자 길이 걱정 없음)
	if len(calls) != 2 || lastCmd() != remoteCommand([]string{"done", "--job", "J1", "--yml", "a.yml"}) {
		t.Fatalf("원격 그룹 중계 인자: %v", calls)
	}
	if !strings.Contains(h.frame(), "완료 처리 요청: 2대") {
		t.Fatalf("원격 그룹 메시지:\n%s", h.frame())
	}
	rc, stderr = "1", "job 이 없습니다\n"
	h.keys("d1y")
	if f := h.frame(); !strings.Contains(f, "[X] 완료 처리 실패") || !strings.Contains(f, "job 이 없습니다") {
		t.Fatalf("원격 실패 사유:\n%s", f)
	}
}

// 안내 줄(화면 2): d 는 h 호스트 작업 메뉴 안으로 들어갔다(메뉴 항목 색은 분홍 그대로), 폭 40~200 에서 넘치지 않고 프롬프트·확인 줄도 잘리지 않는다
func TestDoneHintAndPromptWidths(t *testing.T) {
	if !strings.Contains(strings.Join(menuLines(hostMenu(false), 120, true), "\n"), "\x1b[1;38;5;175md\x1b[0m") {
		t.Error("d 수동 완료 색(분홍)")
	}
	for _, c := range []string{"1;36", stRed, "1;32", "1;33", "1;35", "1;34", "1;38;5;208"} {
		if hintStyles["d"] == c {
			t.Errorf("d 색이 기존 색 %s 와 겹침", c)
		}
	}
	if hintStyles["d"] != doneHintStyle {
		t.Error("doneHintStyle 과 hintStyles[d] 가 같아야 함")
	}
	s := doneSnap()
	st := &tuiState{snap: s}
	st.sel = selection{Detail: true, JobID: "J1", Yml: "a.yml", Host: 1}
	for _, w := range []int{40, 50, 60, 80, 100, 120, 160, 200} {
		for _, color := range []bool{false, true} {
			for _, yml := range []string{"a.yml", allView} {
				st.sel.Yml = yml
				for _, stage := range []struct {
					name string
					d    *doneState
				}{{"선택", &doneState{host: "a2"}}, {"확인-호스트", &doneState{host: "a2", kind: 1, confirm: true}}, {"확인-그룹", &doneState{host: "a2", kind: 2, confirm: true}}} {
					if yml == allView && stage.d.kind == 2 {
						continue
					}
					st.done = stage.d
					sel := st.sel
					sel.Inline = st.doneLines(w, color)
					if len(sel.Inline) == 0 {
						t.Fatalf("w=%d %s: 프롬프트 줄 없음", w, stage.name)
					}
					out := renderDetail(s, sel, w, 24, color)
					p := stripANSI(out)
					if !strings.Contains(p, "h 호스트 작업") {
						t.Errorf("w=%d: 안내 줄에 h 호스트 작업 없음", w)
					}
					for _, l := range strings.Split(p, "\n") {
						if strWidth(l) > w {
							t.Errorf("w=%d %s 폭 초과 %d: %q", w, stage.name, strWidth(l), l)
						}
					}
					for _, l := range sel.Inline { // 프롬프트·확인 줄은 … 로 자르지 않는다
						if strings.Contains(l, "…") {
							t.Errorf("w=%d %s 잘림: %q", w, stage.name, stripANSI(l))
						}
					}
					if n := len(strings.Split(out, "\n")); n > 24 {
						t.Errorf("w=%d %s: 높이 24 이하, 실제 %d", w, stage.name, n)
					}
					if strings.HasSuffix(stage.name, "호스트") && color && !strings.Contains(out, "\x1b[1;31m완료로") {
						t.Errorf("w=%d: 경고부는 빨강", w)
					}
				}
			}
		}
	}
}

// 도움말에 d 설명, 화면 1 안내 줄에는 d 가 없다
func TestDoneHelpAndOverviewUnaffected(t *testing.T) {
	h := stripANSI(renderHelp(120, 80, false))
	for _, s := range []string{"   d  ", "수동 완료", "부팅 시각 검증 없이 완료로 인정", "체크스크립트는 실행 안 함"} {
		if !strings.Contains(h, s) && !strings.Contains(squash(h), squash(s)) {
			t.Errorf("도움말에 %q 없음", s)
		}
	}
	if strings.Contains(renderOverview(doneSnap(), selection{Change: true}, 200, 24, false), "수동 완료") {
		t.Error("화면 1 안내 줄에는 d 수동 완료가 없어야 함")
	}
	if strings.Contains(renderPlain(doneSnap(), 120), "수동 완료") {
		t.Error("--plain 은 영향 없음")
	}
}

func TestDonePromptGolden(t *testing.T) {
	s := doneSnap()
	for _, color := range []bool{false, true} {
		mode := "plain"
		if color {
			mode = "color"
		}
		var sb strings.Builder
		for _, c := range []struct {
			name string
			yml  string
			d    *doneState
		}{{"select", "a.yml", &doneState{host: "a2"}}, {"confirm-host", "a.yml", &doneState{host: "a2", kind: 1, confirm: true}},
			{"confirm-group", "a.yml", &doneState{host: "a2", kind: 2, confirm: true}}, {"select-allview", allView, &doneState{host: "c1"}}} {
			st := &tuiState{snap: s, done: c.d, sel: selection{Detail: true, JobID: "J1", Yml: c.yml, Host: 1}}
			sel := st.sel
			sel.Inline = st.doneLines(60, color)
			sb.WriteString("== " + c.name + " ==\n" + renderDetail(s, sel, 60, 24, color) + "\n")
		}
		got := goldenText(sb.String())
		name := "tui_donemenu_" + mode + "_60.golden"
		path := filepath.Join("testdata", name)
		if *updateGolden {
			if err := os.WriteFile(path, []byte(got), 0644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("골든 없음 %s (go test -run DonePromptGolden -update): %v", name, err)
		}
		if string(want) != got {
			t.Errorf("%s 불일치\n--- got ---\n%s", name, got)
		}
	}
}
