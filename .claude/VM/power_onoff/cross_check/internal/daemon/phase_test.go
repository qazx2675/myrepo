package daemon

// 0.3.0 기능 시험: 붙여넣기 대조 승인, 확인자에게 대상 숨김, 본 작업 단계(작업중/작업완료/작업중단),
// 작업 제한 시간(work_timeout_sec), 호출 스크립트(caller) 기록, ping 의 pid.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cross_check/internal/store"
)

// ---- 붙여넣기 대조 ----

func TestApprovePastedMatchAnyOrderAndSeparators(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01", "vm01ev02", "vm02ev01"})
	nextEvent(t, rh, StatusWaiting, "")
	// 순서 다르게, 탭·공백·CRLF·빈 줄 섞어서
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], "vm02ev01\r\n\r\n vm01ev01\tvm01ev02  \n"); err != nil {
		t.Fatalf("일치하는 붙여넣기 승인: %v", err)
	}
	nextEvent(t, rh, StatusApproved, "bob 가 승인")
	if j := h.job(rh.ID); j.Status != StatusApproved || j.Phase != PhaseNone {
		t.Fatalf("승인 후 = %+v", j)
	}
}

func TestApprovePastedMismatchAbortsWithCountsOnly(t *testing.T) {
	cases := []struct {
		name, pasted, reason string
	}{
		{"빠짐", "vm01ev01", MismatchReason(1, 0, 0, false)},
		{"남음", "vm01ev01 vm01ev02 vm03ev01", MismatchReason(0, 1, 0, false)},
		{"중복", "vm01ev01 vm01ev02 vm01ev01", MismatchReason(0, 0, 1, false)},
		{"대소문자", "VM01EV01 vm01ev02", MismatchReason(1, 1, 0, false)},
		{"비어 있음", " \n\t ", MismatchReason(2, 0, 0, true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			c := h.client()
			rh := h.run(c, "alice", "bob", []string{"vm01ev01", "vm01ev02"})
			nextEvent(t, rh, StatusWaiting, "")
			err := c.Approve(rh.ID, "bob", testPasswords["bob"], tc.pasted)
			wantDaemonErr(t, err, tc.reason)
			if !IsMismatchReason(err.Error()) {
				t.Fatalf("IsMismatchReason(%q) = false", err)
			}
			ev := nextEvent(t, rh, StatusAborted, tc.reason)
			if ev.By != "bob" {
				t.Errorf("불일치 중단 by = %q", ev.By)
			}
			expectClosed(t, rh)
			if j := h.job(rh.ID); j.Status != StatusAborted || j.Reason != tc.reason {
				t.Fatalf("불일치 후 = %+v", j)
			}
			// 응답·이벤트·로그·저장된 사유 어디에도 대상 이름이 없어야 한다(개수만)
			for _, s := range []string{err.Error(), ev.Reason, h.logs.String(), h.job(rh.ID).Reason} {
				if strings.Contains(s, "vm01ev02") || strings.Contains(strings.ToLower(s), "vm03ev01") {
					t.Fatalf("이름이 새어 나감: %q", s)
				}
			}
			// 다시 승인할 수 없다
			wantDaemonErr(t, c.Approve(rh.ID, "bob", testPasswords["bob"], h.paste(rh.ID)), "대기 상태가 아니")
		})
	}
}

// 패스워드를 먼저 확인한다: 틀린 패스워드면 붙여넣기가 틀려도 작업은 그대로(대기)
func TestApproveWrongPasswordBeforeCompare(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	nextEvent(t, rh, StatusWaiting, "")
	wantExact(t, c.Approve(rh.ID, "bob", "wrong_pw", "nothing_like_it"), MsgAuthFailed)
	wantExact(t, c.Approve(rh.ID, "bob", "wrong_pw", "vm01ev01"), MsgAuthFailed)
	if j := h.job(rh.ID); j.Status != StatusWaiting {
		t.Fatalf("틀린 패스워드가 상태를 바꿈: %+v", j)
	}
	if h.srv.thr.failures("bob") != 2 {
		t.Fatalf("실패 횟수(지연 규칙) = %d", h.srv.thr.failures("bob"))
	}
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], "vm01ev01"); err != nil {
		t.Fatal(err)
	}
}

// ---- 확인자에게 대상 숨김 ----

func TestCheckerNeverSeesTargets(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01", "vm03ev01"})
	for _, raw := range []string{
		`{"op":"list","user":"bob"}`,
		`{"op":"get","user":"bob","id":"` + rh.ID + `"}`,
		`{"op":"get","user":"bob","id":"` + rh.ID + `","password":` + jsonStr(testPasswords["bob"]) + `}`,
	} {
		b, _ := json.Marshal(rawCall(t, h.sock, raw))
		if strings.Contains(string(b), "vm01ev01") || strings.Contains(string(b), "vm03ev01") || strings.Contains(string(b), "target") {
			t.Fatalf("확인자 응답에 대상 정보: %s → %s", raw, b)
		}
	}
	b, _ := json.Marshal(rawCall(t, h.sock, `{"op":"list","user":"alice"}`))
	if !strings.Contains(string(b), "vm03ev01") || !strings.Contains(string(b), `"target_count":2`) {
		t.Fatalf("작업자 응답에는 대상이 있어야 함: %s", b)
	}
	all, err := c.AdminList(testPasswords["admin"])
	if err != nil || len(all) != 1 || len(all[0].Targets) != 2 {
		t.Fatalf("관리자는 대상을 봄: %+v %v", all, err)
	}
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ---- 본 작업 단계 ----

// approveStart 는 등록 → 승인(→ 시작) 까지 한 작업.
func approveStart(t *testing.T, h *harness, c *Client, target string, start bool) *RunHandle {
	t.Helper()
	rh := h.run(c, "alice", "bob", []string{target})
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], target); err != nil {
		t.Fatal(err)
	}
	if start {
		if err := c.Start(rh.ID, rh.Token); err != nil {
			t.Fatal(err)
		}
	}
	return rh
}

func TestFinishTransitions(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()

	// 작업중 → done → 승인/작업완료
	d := approveStart(t, h, c, "vm01ev01", true)
	if j := h.job(d.ID); j.Phase != PhaseWorking {
		t.Fatalf("start 후 작업중: %+v", j)
	}
	if err := c.Finish(d.ID, d.Token, ResultDone); err != nil {
		t.Fatal(err)
	}
	if j := h.job(d.ID); j.Status != StatusApproved || j.Phase != PhaseDone || j.FinishedAt == nil {
		t.Fatalf("작업완료: %+v", j)
	}

	// 작업중 → aborted → 승인/작업중단(작업자가 작업을 중단)
	a := approveStart(t, h, c, "vm01ev02", true)
	if err := c.Finish(a.ID, a.Token, ResultAborted); err != nil {
		t.Fatal(err)
	}
	if j := h.job(a.ID); j.Phase != PhaseAborted || j.Reason != ReasonWorkerAborted {
		t.Fatalf("작업중단: %+v", j)
	}

	// 시작 전(승인) → done 은 거부, aborted 는 승인/작업중단(본 작업 전에 중단)
	b := approveStart(t, h, c, "vm02ev01", false)
	wantDaemonErr(t, c.Finish(b.ID, b.Token, ResultDone), "시작하지 않아")
	if err := c.Finish(b.ID, b.Token, ResultAborted); err != nil {
		t.Fatal(err)
	}
	if j := h.job(b.ID); j.Phase != PhaseAborted || j.Reason != ReasonWorkerAbortedBeforeStart || j.StartedAt != nil {
		t.Fatalf("시작 전 작업중단: %+v", j)
	}
	// 끝난 작업은 시작도 결과 변경도 안 된다(만료 스위퍼 대상도 아님)
	wantDaemonErr(t, c.Start(b.ID, b.Token), "이미 끝난 작업")
	wantDaemonErr(t, c.Finish(d.ID, d.Token, ResultAborted), "이미 끝난 작업")
	h.srv.mu.Lock()
	_, pend := h.srv.pending[b.ID]
	nWorking := len(h.srv.working)
	h.srv.mu.Unlock()
	if pend || nWorking != 0 {
		t.Fatalf("끝난 작업이 추적에 남음: pending=%v working=%d", pend, nWorking)
	}

	// 대기 작업에는 결과를 기록할 수 없다
	w := h.run(c, "alice", "bob", []string{"vm03ev01"})
	wantDaemonErr(t, c.Finish(w.ID, w.Token, ResultAborted), "승인 상태가 아니")
	wantDaemonErr(t, c.Finish(w.ID, "", ResultAborted), MsgAuthFailed)

	// list 에 phase 가 보인다
	v, _ := c.Get(d.ID, "alice", "")
	if v.Phase != PhaseDone || v.FinishedAt == nil || StatusLabel(v.Status, v.Phase) != "승인/작업완료" {
		t.Fatalf("Get phase: %+v", v)
	}
}

// 시작한 작업은 승인 유효시간으로 만료되지 않고, 작업 제한 시간을 넘기면 승인/작업중단(작업자 응답 없음)
func TestWorkTimeoutAndTTLDoesNotExpireStarted(t *testing.T) {
	clk := newFakeClock()
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.Now = clk.Now
		cfg.WorkTimeout = 10 * time.Minute
	})
	c := h.client()
	started := approveStart(t, h, c, "vm01ev01", true)
	idle := approveStart(t, h, c, "vm01ev02", false)

	clk.Add(5 * time.Minute) // 승인 유효시간(2분)은 지났고 작업 제한(10분)은 아직
	h.srv.sweep()
	if j := h.job(started.ID); j.Status != StatusApproved || j.Phase != PhaseWorking {
		t.Fatalf("시작한 작업은 승인 유효시간으로 만료되지 않음: %+v", j)
	}
	if j := h.job(idle.ID); j.Status != StatusAborted || !IsExpiredReason(j.Reason) {
		t.Fatalf("시작 안 한 작업은 만료: %+v", j)
	}
	clk.Add(5*time.Minute + time.Second) // 시작 후 10분 1초
	h.srv.sweep()
	j := h.job(started.ID)
	if j.Status != StatusApproved || j.Phase != PhaseAborted || j.Reason != ReasonWorkTimeout || j.FinishedAt == nil {
		t.Fatalf("작업 제한 초과 → 승인/작업중단(작업자 응답 없음): %+v", j)
	}
	// 늦게 온 finish 는 거부(이미 끝남)
	wantDaemonErr(t, c.Finish(started.ID, started.Token, ResultDone), "이미 끝난 작업")
}

func TestAdminSetPhases(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	apw := testPasswords["admin"]

	w := approveStart(t, h, c, "vm01ev01", true) // 작업중
	wantDaemonErr(t, c.AdminSetPhase(apw, w.ID, PhaseWorking, ""), "--start 로만")
	if err := c.AdminSetPhase(apw, w.ID, PhaseDone, "수동 확인"); err != nil {
		t.Fatal(err)
	}
	if j := h.job(w.ID); j.Phase != PhaseDone || j.Reason != ReasonAdminSet+": 수동 확인" {
		t.Fatalf("관리자 작업완료: %+v", j)
	}
	wantDaemonErr(t, c.AdminSetPhase(apw, w.ID, PhaseAborted, ""), "이미 끝난 작업")
	wantDaemonErr(t, c.Finish(w.ID, w.Token, ResultDone), "이미 끝난 작업") // token 폐기

	n := approveStart(t, h, c, "vm01ev02", false) // 시작 전
	wantDaemonErr(t, c.AdminSetPhase(apw, n.ID, PhaseDone, ""), "작업중인 작업만")
	if err := c.AdminSetPhase(apw, n.ID, PhaseAborted, ""); err != nil {
		t.Fatal(err)
	}
	if j := h.job(n.ID); j.Phase != PhaseAborted {
		t.Fatalf("관리자 작업중단(시작 전): %+v", j)
	}
	wantDaemonErr(t, c.Start(n.ID, n.Token), "이미 끝난 작업")

	x := approveStart(t, h, c, "vm02ev01", true)
	if err := c.AdminSet(apw, x.ID, StatusAborted, "취소"); err != nil { // 작업중 → 중단(예전 규칙 그대로)
		t.Fatal(err)
	}
	h.srv.mu.Lock()
	_, still := h.srv.working[x.ID]
	h.srv.mu.Unlock()
	if still {
		t.Fatal("중단된 작업이 작업 제한 추적에 남음")
	}
	wantDaemonErr(t, c.Finish(x.ID, x.Token, ResultDone), "승인 상태가 아니")

	wt := h.run(c, "alice", "bob", []string{"vm03ev01"}) // 대기
	wantDaemonErr(t, c.AdminSetPhase(apw, wt.ID, PhaseAborted, ""), "승인 상태가 아니")
	wantDaemonErr(t, c.AdminSetPhase(apw, wt.ID, "이상", ""), "승인 상태가 아니")
}

// 데몬 재시작: 작업중이던 작업은 승인/작업중단(데몬 재시작), 끝난 작업은 그대로
func TestRestartMarksWorkingAborted(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	w := approveStart(t, h, c, "vm01ev01", true)
	d := approveStart(t, h, c, "vm01ev02", true)
	if err := c.Finish(d.ID, d.Token, ResultDone); err != nil {
		t.Fatal(err)
	}
	h.srv.Close()
	st, err := store.Open(h.storePath)
	if err != nil {
		t.Fatal(err)
	}
	st.AbortOpen(ReasonRestart)
	if j, _ := st.Get(w.ID); j.Status != StatusApproved || j.Phase != PhaseAborted || j.Reason != ReasonRestart {
		t.Fatalf("작업중 → 승인/작업중단: %+v", j)
	}
	if j, _ := st.Get(d.ID); j.Phase != PhaseDone || j.Reason != "" {
		t.Fatalf("작업완료 유지: %+v", j)
	}
}

// ---- caller ----

func TestCallerRecordedAndSanitized(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh, err := c.RunCaller("alice", "bob", []string{"vm01ev01"}, "  vm_power_onoff.sh\x1b[31m\n")
	if err != nil {
		t.Fatal(err)
	}
	defer rh.Close()
	if j := h.job(rh.ID); j.Caller != "vm_power_onoff.sh[31m" {
		t.Fatalf("caller 정리(제어문자 제거): %q", j.Caller)
	}
	v, _ := c.Get(rh.ID, "bob", "")
	if v.Caller != "vm_power_onoff.sh[31m" {
		t.Fatalf("확인자에게도 caller 는 보임: %+v", v)
	}
	long := strings.Repeat("가", 100)
	rh2, err := c.RunCaller("alice", "bob", []string{"vm01ev02"}, long)
	if err != nil {
		t.Fatal(err)
	}
	defer rh2.Close()
	if got := []rune(h.job(rh2.ID).Caller); len(got) != MaxCallerLen {
		t.Fatalf("caller 64자 제한: %d", len(got))
	}
	if SanitizeCaller("a\xffb") != "a?b" || SanitizeCaller("\x00\x07") != "" {
		t.Fatalf("SanitizeCaller: %q %q", SanitizeCaller("a\xffb"), SanitizeCaller("\x00\x07"))
	}
}

// ---- ping ----

func TestPingPIDAndWorkTimeout(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.WorkTimeout = 2 * time.Hour })
	c := h.client()
	info, err := c.Info()
	if err != nil || info.PID != os.Getpid() || info.ApprovalTTL != DefaultApprovalTTL || info.WorkTimeout != 2*time.Hour {
		t.Fatalf("Info = %+v, %v", info, err)
	}
}

// ---- 설정 ----

func TestLoadConfWorkTimeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cross_check.conf")
	base := "target_dir=/etc/x/targets\nusers=alice,bob\n"
	os.WriteFile(p, []byte(base), 0o644)
	if c, err := LoadConf(p); err != nil || c.WorkTimeout != DefaultWorkTimeout {
		t.Fatalf("기본 work_timeout_sec: %+v %v", c, err)
	}
	for _, v := range []string{"600", "86400", "604800"} {
		os.WriteFile(p, []byte(base+"work_timeout_sec="+v+"\n"), 0o644)
		if _, err := LoadConf(p); err != nil {
			t.Errorf("work_timeout_sec=%s: %v", v, err)
		}
	}
	for _, v := range []string{"599", "604801", "0", "-1", "abc", ""} {
		os.WriteFile(p, []byte(base+"work_timeout_sec="+v+"\n"), 0o644)
		_, err := LoadConf(p)
		if err == nil || !strings.Contains(err.Error(), "work_timeout_sec 는 600~604800") {
			t.Errorf("work_timeout_sec=%q 는 범위 오류여야 함: %v", v, err)
		}
	}
	t.Setenv(EnvMinWorkTimeoutSec, "2")
	os.WriteFile(p, []byte(base+"work_timeout_sec=2\n"), 0o644)
	if c, err := LoadConf(p); err != nil || c.WorkTimeout != 2*time.Second {
		t.Fatalf("시험용 최소값: %+v %v", c, err)
	}
}

func TestReloadWorkTimeoutOnSIGHUP(t *testing.T) {
	h := newHarness(t, nil)
	conf := filepath.Join(h.dir, "cross_check.conf")
	os.WriteFile(conf, []byte("target_dir="+h.targetDir+"\nusers="+strings.Join(testUsers, ",")+"\nwork_timeout_sec=3600\n"), 0o644)
	if _, _, err := h.srv.ReloadApprovalTTL(conf); err != nil {
		t.Fatal(err)
	}
	if h.srv.WorkTimeout() != time.Hour {
		t.Fatalf("SIGHUP 으로 work_timeout_sec 반영: %v", h.srv.WorkTimeout())
	}
	if !strings.Contains(h.logs.String(), "작업 제한 시간 24시간(86400초) → 1시간(3600초)") {
		t.Fatalf("로그: %s", h.logs.String())
	}
}
