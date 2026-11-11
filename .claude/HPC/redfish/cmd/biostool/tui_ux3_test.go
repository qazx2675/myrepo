package main

// tui_ux3_test.go — 사용감 개선 3단계: 벤더가 하나일 때 ←→ 바로 이동, 결과 교체 시 위치 유지, 키 안내 그룹·색,
// 맨 위 단계 줄, 구분선·[안내]/[!]·[질문]/[입력] 표시.

import (
	"fmt"
	"strings"
	"testing"
)

// 벤더가 하나뿐이고 그룹이 둘 이상이면 Enter 없이 ←→ 가 두 번째 줄을 움직이고, Enter 는 아무 일도 없으며,
// q 는 맨 위처럼 두 번 눌러야 끝난다 (벤더 줄로 돌아가기 단계가 없음). 벤더가 둘 이상이면 기존 동작 (TestTUITabSwitching).
func TestTUISingleVendorArrowsWithoutEnter(t *testing.T) {
	one := func() []VendorGroups { return tuiSampleVGs()[:1] } // HPE: iLO 4 / iLO 5
	u, r, _ := browseKeys(t, "\x1b[C", one())
	f := tuiLastFrame(r)
	if u.vi != 0 || u.gi != 1 || u.inGroup || !u.subActive() || !strings.Contains(f, ">> 관리망  [iLO 4] >[iLO 5]") ||
		!strings.Contains(f, "   벤더  >[HPE]") || !strings.Contains(f, "AttrC5") || !strings.Contains(f, "←→ 관리망") ||
		strings.Contains(f, "Enter 관리망 고르기") || strings.Contains(f, "Enter: 아래") || strings.Contains(f, "q/Esc 벤더") ||
		!strings.Contains(f, "호스트 1대 · ←→ 관리망 이동") {
		t.Errorf("Enter 없이 →: vi=%d gi=%d inGroup=%v\n%s", u.vi, u.gi, u.inGroup, f)
	}
	// Enter 는 아무 일도 하지 않는다 (안내 줄도 없음), ← 는 첫 그룹에서 멈춤
	u, r, _ = browseKeys(t, "\x1b[C\r\x1b[D\x1b[D\r", one())
	if f := tuiLastFrame(r); u.gi != 0 || u.inGroup || strings.Contains(f, "[안내]") || strings.Contains(f, "[!]") {
		t.Errorf("Enter 는 아무 일 없음: gi=%d inGroup=%v\n%s", u.gi, u.inGroup, f)
	}
	// q 한 번은 종료 확인만, 두 번째 q 가 끝 — 그룹 위치는 그대로
	u, r, act := browseKeys(t, "\x1b[Cq", one())
	if f := tuiLastFrame(r); act != TUIQuit || !u.Quit() || u.gi != 1 || !strings.Contains(f, tuiQuitConfirm) {
		t.Errorf("q 한 번은 종료 확인: act=%v quit=%v gi=%d\n%s", act, u.Quit(), u.gi, f)
	}
	u, _, act = browseKeys(t, "\x1b[Cqq", one())
	if act != TUIQuit || u.Quit() || u.gi != 1 {
		t.Errorf("q q 로 끝: act=%v quit=%v gi=%d", act, u.Quit(), u.gi)
	}
	// Esc 는 맨 위처럼 종료 확인 취소만
	_, r, _ = browseKeys(t, "q\x1b", one())
	if f := tuiLastFrame(r); strings.Contains(f, tuiQuitConfirm) {
		t.Errorf("Esc 는 종료 확인 취소:\n%s", f)
	}
	// 그룹이 하나뿐인 벤더는 기존대로 벤더 줄이 움직이는 줄
	u, r, _ = browseKeys(t, "\x1b[C", tuiSampleVGs()[1:])
	if f := tuiLastFrame(r); u.subActive() || !strings.Contains(f, ">> 벤더  >[DELL]") {
		t.Errorf("그룹 1개 벤더:\n%s", f)
	}
}

// SetResults 는 앞 결과가 있으면 보던 벤더·그룹을 이름으로, 필터를 그대로 유지하고 (없으면 처음으로), 스크롤은 같은 그룹일 때만 유지한다.
func TestTUISetResultsKeepsPositionByName(t *testing.T) {
	u := &TUI{env: tuiEnv{}}
	u.SetResults("a", tuiSampleVGs())
	u.vi, u.gi, u.inGroup, u.filter = 0, 1, true, tuiFilterDiff
	u.SetResults("b", tuiSampleVGs())
	if u.vi != 0 || u.gi != 1 || !u.inGroup || u.filter != tuiFilterDiff {
		t.Errorf("같은 이름은 그대로: vi=%d gi=%d inGroup=%v filter=%c", u.vi, u.gi, u.inGroup, u.filter)
	}
	// 순서가 바뀌어도 이름으로 찾는다
	vgs := tuiSampleVGs()
	vgs[0], vgs[1] = vgs[1], vgs[0] // DELL, HPE
	u.SetResults("c", vgs)
	if u.vi != 1 || u.gi != 1 || u.filter != tuiFilterDiff {
		t.Errorf("순서가 바뀐 같은 벤더·그룹: vi=%d gi=%d", u.vi, u.gi)
	}
	// 벤더는 있고 그룹 이름이 없으면 그 벤더의 첫 그룹 (그룹 줄 단계는 풀림)
	vgs = tuiSampleVGs()
	vgs[0].Groups = vgs[0].Groups[:1]
	u.SetResults("d", vgs)
	if u.vi != 0 || u.gi != 0 || u.inGroup || u.filter != tuiFilterDiff {
		t.Errorf("없는 그룹: vi=%d gi=%d inGroup=%v", u.vi, u.gi, u.inGroup)
	}
	// 벤더가 없으면 처음으로 (필터는 유지)
	u.vi, u.gi = 0, 0
	u.SetResults("e", tuiSampleVGs()[1:])
	if u.vi != 0 || u.gi != 0 || u.filter != tuiFilterDiff {
		t.Errorf("없는 벤더: vi=%d gi=%d filter=%c", u.vi, u.gi, u.filter)
	}
	// 앞이 비어 있었으면 필터도 처음(전체)으로
	u.SetResults("f", nil)
	u.filter = tuiFilterNA
	u.SetResults("g", tuiSampleVGs())
	if u.filter != tuiFilterAll || u.vi != 0 || u.gi != 0 {
		t.Errorf("앞이 빈 결과: filter=%c", u.filter)
	}
	// 스크롤: 같은 벤더·그룹이면 유지, 다르면 0
	u.SetResults("h", tuiBigVGs(2, 60))
	u.frame(100, 24)
	u.top = 7
	u.SetResults("i", tuiBigVGs(2, 60))
	if u.top != 7 {
		t.Errorf("같은 그룹이면 스크롤 유지: top=%d", u.top)
	}
	other := tuiBigVGs(2, 60)
	other[0].Groups[0].Name = "iLO 7"
	u.SetResults("j", other)
	if u.top != 0 {
		t.Errorf("다른 그룹이면 스크롤 처음: top=%d", u.top)
	}
	// 보기 탭에 저장된 상태는 SetResults 와 상관없다
	u2 := &TUI{env: tuiEnv{}}
	u2.SetTabs([]string{"A", "B"})
	u2.SetResults("A", tuiSampleVGs())
	u2.vi, u2.gi, u2.filter = 0, 1, tuiFilterOK
	u2.SwitchTab(1)
	u2.SetResults("B", tuiSampleVGs()[1:])
	u2.SwitchTab(0)
	if u2.vi != 0 || u2.gi != 1 || u2.filter != tuiFilterOK || u2.title != "A" {
		t.Errorf("저장된 탭 상태: vi=%d gi=%d filter=%c title=%q", u2.vi, u2.gi, u2.filter, u2.title)
	}
}

// 키 안내: "[이동] [보기] [작업]" 그룹 이름 항목, 키(청록 굵게)·설명(회색) 나눔, 그룹 이름이 줄 끝에 혼자 남지 않음.
func TestTUIHintGroupsAndRendering(t *testing.T) {
	u := &TUI{env: tuiEnv{}}
	u.SetTabs([]string{"프로파일 점검", "전체 설정 비교"})
	u.SetResults("x", tuiSampleVGs())
	u.SetRefKey(true)
	items := u.hintItems()
	var labels []string
	for _, it := range items {
		if tuiIsHintLabel(it) {
			labels = append(labels, it)
		}
	}
	if fmt.Sprint(labels) != "[[이동] [보기] [작업]]" || items[0] != "[이동]" {
		t.Errorf("결과 보기 그룹 순서: %v", items)
	}
	if j := strings.Join(items, "|"); !strings.Contains(j, "PgUp/PgDn 페이지") || strings.Contains(j, "한 화면") {
		t.Errorf("PgUp/PgDn 페이지: %v", items)
	}
	plain := tuiWrapItems(false, 200, items)
	if len(plain) != 1 || !strings.HasPrefix(plain[0], " [이동] Tab 화면 전환  ←→ 벤더") || !strings.Contains(plain[0], " | [보기] a 전체  o OK만") ||
		!strings.Contains(plain[0], " | [작업] r 기준 다시 선택  e 수정 메뉴  ? 도움말  q 두 번 종료") {
		t.Errorf("한 줄 안내:\n%q", plain)
	}
	// 색: 키는 굵은 청록, 설명은 회색, 그룹 이름은 흐리게. 글자는 색 없는 쪽과 같다 (구분선 문자만 다름)
	col := tuiWrapItems(true, 200, items)
	if !strings.Contains(col[0], "\x1b[1;36mTab\x1b[0m\x1b[90m 화면 전환\x1b[0m") || !strings.Contains(col[0], "\x1b[2m[이동]\x1b[0m") {
		t.Errorf("색 안내: %q", col[0])
	}
	if strings.ReplaceAll(tuiStripANSI(col[0]), "│", "|") != plain[0] {
		t.Errorf("색 있는 글자 = 없는 글자:\n%q\n%q", tuiStripANSI(col[0]), plain[0])
	}
	// 어떤 폭에서도 그룹 이름이 줄 끝에 혼자 남지 않고 폭을 넘지 않음, 모든 안내가 한 번씩 나옴
	for w := 30; w <= 130; w++ {
		ls := tuiWrapItems(false, w, items)
		all := strings.Join(ls, "\n")
		for _, l := range ls {
			if tuiStrWidth(l) > w {
				t.Fatalf("w=%d 폭 넘음 %d: %q", w, tuiStrWidth(l), l)
			}
			tr := strings.TrimRight(l, " |")
			for _, lb := range labels {
				if strings.HasSuffix(tr, lb) {
					t.Fatalf("w=%d 그룹 이름이 줄 끝에: %q", w, l)
				}
			}
		}
		for _, it := range items {
			if !tuiIsHintLabel(it) && !strings.Contains(all, it) {
				t.Fatalf("w=%d 안내 빠짐 %q:\n%s", w, it, all)
			}
		}
	}
	// 글 보기·질문·조회 중
	u.SetText("미리보기", []string{"x"})
	if h := strings.Join(u.hintItems(), "|"); strings.Contains(h, "[보기]") || !strings.Contains(h, "[작업]|? 도움말|Enter/q 계속") {
		t.Errorf("글 보기 안내: %s", h)
	}
	u.prompt = &tuiPrompt{kind: tpYesNo, title: "q"}
	if h := u.hintItems(); h[0] != "[입력]" || strings.Contains(strings.Join(h, "|"), "[작업]") {
		t.Errorf("질문 안내: %v", h)
	}
	u.prompt = nil
	u.SetResults("x", tuiSampleVGs())
	u.job = &TUIJob{status: "s"}
	if h := strings.Join(u.hintItems(), "|"); !strings.Contains(h, "[진행]|Ctrl+X 작업 취소|? 도움말") || strings.Contains(h, "[작업]") {
		t.Errorf("조회 중 안내: %s", h)
	}
}

// 맨 위 단계 줄: stage 0 이면 없음, 있으면 첫 줄이고 본문 높이를 한 줄 가져가며, 한글 폭에서도 화면 폭을 넘지 않는다.
func TestTUIStageLine(t *testing.T) {
	u := &TUI{env: tuiEnv{}}
	u.SetResults("BIOS 점검 결과", tuiSampleVGs())
	f0 := u.frame(80, 24)
	body0 := u.bodyH
	if !strings.HasPrefix(f0[0], " BIOS 점검 결과") || strings.Contains(strings.Join(f0, "\n"), "› ") {
		t.Errorf("stage 0 은 단계 줄 없음:\n%s", strings.Join(f0, "\n"))
	}
	u.SetStage(4)
	f := u.frame(80, 24)
	if f[0] != " 대상 › 프로파일 › 조회 › [결과] › 수정" || !strings.HasPrefix(f[1], " BIOS 점검 결과") || len(f) != 24 || u.bodyH != body0-1 {
		t.Errorf("stage 4 (색 없음): bodyH %d→%d len=%d\n%s", body0, u.bodyH, len(f), strings.Join(f, "\n"))
	}
	for st, want := range map[int]string{1: " [대상] › 프로파일 › 조회 › 결과 › 수정", 5: " 대상 › 프로파일 › 조회 › 결과 › [수정]"} {
		if got := tuiStageLine(false, 80, st); got != want {
			t.Errorf("stage %d: %q", st, got)
		}
	}
	// 색: 지금 단계는 굵게 반전, 나머지는 회색 (괄호 없음)
	u.env.Color = true
	c := u.frame(80, 24)[0]
	if !strings.Contains(c, "\x1b[1;7m결과\x1b[0m") || !strings.Contains(c, "\x1b[90m대상\x1b[0m") || tuiStripANSI(c) != " 대상 › 프로파일 › 조회 › 결과 › 수정" {
		t.Errorf("stage 4 (색): %q", c)
	}
	// 좁은 화면(한글 2칸)에서도 폭 안
	for w := 12; w <= 40; w++ {
		if got := tuiStrWidth(tuiStripANSI(tuiStageLine(true, w, 3))); got > w {
			t.Fatalf("w=%d 단계 줄 폭 %d", w, got)
		}
	}
	for _, l := range u.frame(40, 24) {
		if tuiStrWidth(tuiStripANSI(l)) > 40 {
			t.Errorf("폭 넘음: %q", l)
		}
	}
	// 0 으로 되돌리면 줄이 사라진다
	u.env.Color = false
	u.SetStage(0)
	if f := u.frame(80, 24); strings.Contains(f[0], "›") || u.bodyH != body0 {
		t.Errorf("stage 0 으로: %q bodyH=%d", f[0], u.bodyH)
	}
	// 도움말 화면에는 그리지 않는다
	u.SetStage(4)
	u.help = true
	if f := u.frame(80, 24); !strings.Contains(f[0], "도움말") {
		t.Errorf("도움말 첫 줄: %q", f[0])
	}
}

// 본문과 꼬리 사이 구분선, 안내 줄 [안내]/[!] 표시 (u.msg 는 그대로), 질문·입력 [질문]/[입력] 표시.
func TestTUISeparatorMarkersAndPrompts(t *testing.T) {
	u := &TUI{env: tuiEnv{}}
	u.SetResults("x", tuiSampleVGs())
	idx := func(f []string, pfx string) int {
		for i := len(f) - 1; i >= 0; i-- {
			if strings.HasPrefix(f[i], pfx) {
				return i
			}
		}
		return -1
	}
	f := u.frame(80, 24)
	rule, hint := idx(f, "-----"), idx(f, " [이동]")
	if rule < 1 || f[rule] != strings.Repeat("-", 80) || hint != rule+1 || strings.Contains(f[rule-1], "-----") {
		t.Fatalf("구분선(색 없음 '-') 은 안내 바로 위:\n%s", strings.Join(f, "\n"))
	}
	if f[len(f)-1] != " " {
		t.Errorf("안내 줄 없음: %q", f[len(f)-1])
	}
	u.env.Color = true
	if cf := u.frame(80, 24); !strings.Contains(cf[rule], strings.Repeat("─", 80)) {
		t.Errorf("색 있으면 ─ 선: %q", cf[rule])
	}
	u.env.Color = false
	u.msg = "종료하려면 q 를 한 번 더 누르세요"
	if f = u.frame(80, 24); f[len(f)-1] != " [안내] 종료하려면 q 를 한 번 더 누르세요" || u.msg != "종료하려면 q 를 한 번 더 누르세요" {
		t.Errorf("[안내]: %q msg=%q", f[len(f)-1], u.msg)
	}
	for _, m := range []string{"전체비교 실패: x", "결과 파일 기록 중 오류", "파일을 찾을 수 없음", "설정할 항목이 없습니다"} {
		u.msg = m
		if f = u.frame(80, 24); f[len(f)-1] != " [!] "+m || u.msg != m {
			t.Errorf("[!] %q: %q", m, f[len(f)-1])
		}
	}
	u.env.Color = true
	if l := u.frame(80, 24); !strings.Contains(l[len(l)-1], "\x1b[1;31m[!] \x1b[0m\x1b[1;33m설정할 항목이 없습니다\x1b[0m") {
		t.Errorf("[!] 색: %q", l[len(l)-1])
	}
	u.env.Color, u.msg = false, "저장했습니다"
	if f = u.frame(80, 24); f[len(f)-1] != " [안내] 저장했습니다" {
		t.Errorf("[안내] 일반: %q", f[len(f)-1])
	}
	u.msg = ""
	// 질문·입력·고르기 제목 표시
	u.prompt = &tuiPrompt{kind: tpYesNo, title: "진행할까요?"}
	if g := strings.Join(u.frame(80, 24), "\n"); !strings.Contains(g, " [질문] 진행할까요? [y/n] ") || !strings.Contains(g, " [입력] y 예") {
		t.Errorf("Ask:\n%s", g)
	}
	u.prompt = &tuiPrompt{kind: tpLine, title: "이름", buf: []rune("abc")}
	if g := strings.Join(u.frame(80, 24), "\n"); !strings.Contains(g, " [입력] 이름: abc_") {
		t.Errorf("InputLine:\n%s", g)
	}
	u.prompt = &tuiPrompt{kind: tpLines, title: "대상 붙여넣기", lines: []string{"a"}}
	if g := strings.Join(u.frame(80, 24), "\n"); !strings.Contains(g, " [입력] 대상 붙여넣기  (1줄 입력됨)") {
		t.Errorf("InputLines:\n%s", g)
	}
	u.prompt = &tuiPrompt{kind: tpChoose, title: "프로파일 선택", items: []string{"a", "b"}}
	if g := strings.Join(u.frame(80, 24), "\n"); !strings.Contains(g, " [질문] 프로파일 선택") || !strings.Contains(g, " > ") {
		t.Errorf("Choose:\n%s", g)
	}
	// 어떤 프레임도 높이를 넘지 않는다 (구분선·단계 줄 포함, 작은 화면)
	u.SetStage(3)
	for h := 6; h <= 30; h++ {
		if n := len(u.frame(60, h)); n > h {
			t.Fatalf("h=%d 프레임 %d줄", h, n)
		}
	}
}

// 대표 화면 (go test -run TestTUIShowExample -v 로 레이아웃을 눈으로 본다).
func TestTUIShowExample(t *testing.T) {
	u := &TUI{env: tuiEnv{}}
	u.SetTabs([]string{"프로파일 점검", "전체 설정 비교"})
	u.SetResults("BIOS 점검 결과 — 프로파일 VM", tuiSampleVGs())
	u.SetStage(4)
	u.SetMessage("설정할 항목이 없습니다")
	t.Log("\n" + strings.Join(u.frame(100, 24), "\n"))
}
