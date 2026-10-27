package main

// tui_flow.go recheckHosts 시험: 설정 적용(가짜) 뒤 설정을 보낸 호스트만 묻지 않고 다시 점검해 결과를 합치고,
// 다시 e 로 수정 단계에 들어갈 수 있다. Ctrl+X 로 못 한 호스트는 처음 결과를 두고 다시 설정하지 않는다.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"biostool/internal/mockbmc"
)

// 설정(가짜 적용: b 에 Pending 을 넣고 b 만 보냈다고 알림) → 재점검(b 만 다시 접속) → PENDING_OK 합침 → 다시 e 가능.
func TestBiosFlowRecheckAfterSetMergesSentHosts(t *testing.T) {
	a, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	b, ub := startAllSrv(t, "hpe-dl360gen11", func(s *mockbmc.Server) {
		setAttrs(t, s, map[string]interface{}{"ProcHyperthreading": "Disabled"})
	})
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", ua, ub), "VM"
	var cmps []*compareResult
	swapFlowPlan(t, func(cfg *Config, u *unifiedRun, cmp *compareResult) *setPlan {
		cmps = append(cmps, cmp)
		return &setPlan{Items: []setItem{{Host: ub, Attr: "ProcHyperthreading", From: "Disabled", To: "Enabled", Source: SetSrcProfile, hostIdx: 1}}}
	}, func(*runContext, *unifiedRun, *setPlan, bool, func(string, string), <-chan struct{}) (*setOutcome, error) {
		setPending(t, b, hpeSettings, map[string]interface{}{"ProcHyperthreading": "Enabled"})
		return &setOutcome{Lines: []string{ub + ": 설정 확인 OK"}, OK: 1, Tried: []int{1}}, nil
	})
	script := []flowStep{
		{"profile-result", "\t"}, // 전체 설정 비교를 먼저 해 둔다 (재점검 뒤 비워지는지)
		{"ref", "1"},
		{"ref-host", ua + "\x04"},
		{"compare-result", "e"},
		{"edit-menu", "1"},
		{"ask-set", "y"},
		{"set-result", "q"},
		{"profile-result", "e"}, // 7: 재점검을 마쳤으니 다시 수정 단계로 들어갈 수 있다
		{"edit-menu", "1"},
		{"ask-set", "n"},
		{"profile-result", "qq"},
	}
	var h0, h1 *hostCheck
	var s1 *biosSnap
	fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", script, nil, func(i int, f *biosFlow) {
		switch i {
		case 0:
			h0, h1, s1 = f.u.Check.Hosts[0], f.u.Check.Hosts[1], f.u.Snaps[1]
		case 7:
			if f.u.Check.Hosts[0] != h0 || f.u.Check.Hosts[1] == h1 || f.u.Snaps[1] == s1 {
				t.Error("설정을 보낸 b 만 새 결과로 바뀌어야 함")
			}
			if st := itemOf(t, f.u.Check.Hosts[1], "hyper_threading").Result; st != StatusPendingOK {
				t.Errorf("재점검 결과 = %s, 기대 %s", st, StatusPendingOK)
			}
			if f.ui.Tab() != 0 || f.cmp != nil || len(f.unchecked) != 0 {
				t.Errorf("재점검 뒤: tab=%d cmp=%v unchecked=%v, 기대 프로파일 화면·전체비교 비움", f.ui.Tab(), f.cmp, f.unchecked)
			}
			if !strings.Contains(f.setNote, "재점검 1대 완료") {
				t.Errorf("setNote = %q", f.setNote)
			}
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if len(cmps) != 2 || cmps[0] == nil || cmps[1] != nil {
		t.Errorf("설정 계획에 넘긴 전체비교 = %v, 기대 [있음 없음] (두 번째 e 가 막히지 않아야 함)", cmps)
	}
	// 세션: 설정을 보내지 않은 a 는 처음 1회뿐, b 는 처음 + 재점검
	assertOneSession(t, a)
	assertClean(t, a)
	if ss := b.Sessions(); ss.Created != 2 || ss.Deleted != 2 {
		t.Errorf("b 세션 = %+v, 기대 생성 2 / 삭제 2", ss)
	}
	// 화면: 재점검 진행 표시 · 합친 프로파일 결과
	sc := fr.screen()
	for _, w := range []string{"재점검 (설정을 보낸 호스트)", "CSV 저장 (재점검)", "BIOS 점검 결과 — 프로파일 VM"} {
		if !strings.Contains(sc, w) {
			t.Errorf("화면에 %q 없음", w)
		}
	}
	// 파일: 처음 점검 CSV 는 그대로, 합친 결과 CSV 는 recheck 폴더에
	dir := fr.f.u.Check.Dir
	if _, err := os.Stat(filepath.Join(dir, "profile_long.csv")); err != nil {
		t.Errorf("처음 점검 CSV 가 있어야 함: %v", err)
	}
	merged := 0
	for _, p := range fr.f.csvs {
		if strings.Contains(filepath.ToSlash(p), "/recheck/") && strings.Contains(filepath.Base(p), "profile_merged_") {
			merged++
			if _, err := os.Stat(p); err != nil {
				t.Errorf("합친 결과 CSV 없음: %v", err)
			}
		}
	}
	if merged == 0 {
		t.Errorf("합친 결과 CSV 가 요약 목록에 없음: %v", fr.f.csvs)
	}
	// 요약: 재점검 결과가 건수에 반영 (b 가 difference → PENDING_OK 라 OK)
	out := fr.out.String()
	for _, w := range []string{"대상 2대 점검 — OK 2 · difference 0", "설정: 설정하지 않았습니다"} {
		if !strings.Contains(out, w) {
			t.Errorf("요약에 %q 없음:\n%s", w, out)
		}
	}
}

// 재점검 중 Ctrl+X: 시작하지 않은 호스트(c)는 처음 결과를 그대로 두고, 재점검하지 못한 호스트가 남으면 다시 설정하지 않는다.
func TestBiosFlowRecheckCancelKeepsOldResult(t *testing.T) {
	_, ua := startAllSrv(t, "hpe-dl360gen11", nil)
	slow := startMockTree(t, "hpe-dl360gen11", func(o *mockbmc.Options) {
		o.Fail = map[string]mockbmc.Fault{"POST /redfish/v1/SessionService/Sessions": {Delay: 400 * time.Millisecond}}
	})
	ub := strings.TrimPrefix(slow.URL(), "https://")
	c, uc := startAllSrv(t, "hpe-dl360gen11", nil)
	e, o := flowEnv(t, "")
	o.user, o.profile = e.writeFile("targets.txt", ua, ub, uc), "VM"
	builds := 0
	swapFlowPlan(t, func(*Config, *unifiedRun, *compareResult) *setPlan {
		builds++
		return &setPlan{Items: []setItem{{Host: ub, Attr: "ProcHyperthreading", From: "Disabled", To: "Enabled", Source: SetSrcProfile, hostIdx: 1}}}
	}, func(*runContext, *unifiedRun, *setPlan, bool, func(string, string), <-chan struct{}) (*setOutcome, error) {
		return &setOutcome{Lines: []string{"적용 결과 줄"}, Tried: []int{1, 2}}, nil
	})
	script := []flowStep{
		{"profile-result", "e"},
		{"edit-menu", "1"},
		{"ask-set", "y"},
		{"set-result", "q\x18"}, // 설정 결과를 닫자마자 재점검 진행 화면에서 Ctrl+X (첫 호스트 b 는 로그인 400ms 지연)
		{"profile-result", "e"}, // c 를 재점검하지 못했으므로 막힘
		{"profile-result", "qq"},
	}
	var h1, h2 *hostCheck
	fr, err := runFlowTC(t, biosTUIArgs{O: o, UserSet: true}, "", script, nil, func(i int, f *biosFlow) {
		switch i {
		case 0:
			h1, h2 = f.u.Check.Hosts[1], f.u.Check.Hosts[2]
		case 4:
			// 첫 호스트 b 는 Ctrl+X 가 처리되기 전에 시작했을 수도, 아닐 수도 있다 (시작했으면 끝까지 재점검해 합침).
			// 어느 쪽이든 c 는 접속하지 않고 처음 결과 그대로, 합치지 못한 호스트는 unchecked 에 남는다.
			want := "map[1:true 2:true]"
			if f.u.Check.Hosts[1] != h1 {
				want = "map[2:true]"
			}
			if f.u.Check.Hosts[2] != h2 || fmt.Sprint(f.unchecked) != want {
				t.Errorf("c 는 처음 결과 그대로, 합치지 못한 호스트만 unchecked: unchecked=%v 기대 %s", f.unchecked, want)
			}
		}
	})
	if err != nil {
		t.Fatalf("흐름 오류: %v", err)
	}
	if builds != 1 {
		t.Errorf("설정 계획 %d회, 기대 1 (재점검하지 못한 호스트가 있으면 다시 설정하지 않음)", builds)
	}
	if ss := c.Sessions(); ss.Created != 1 {
		t.Errorf("c 세션 = %+v, 취소했으니 처음 1회뿐이어야 함", ss)
	}
	if !strings.Contains(fr.screen(), "재점검하지 못했습니다") {
		t.Error("다시 설정할 수 없다는 안내가 그려져야 함")
	}
	if out := fr.out.String(); !strings.Contains(out, "대는 재점검하지 못해 처음 결과 그대로)") {
		t.Errorf("요약:\n%s", out)
	}
}
