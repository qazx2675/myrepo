package daemon

// 승인 대기 제한 시간(wait_timeout_sec) 시험: 설정 파싱·기본값, 대기 만료 → 중단(사유·이벤트), 승인된 작업은 영향 없음,
// 관리자 승인·취소와 충돌 없음, SIGHUP 재적재, ping·run 응답의 선택 필드.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cross_check/internal/store"
)

func TestReasonWaitTimeout(t *testing.T) {
	if got := ReasonWaitTimeout(10 * time.Minute); got != "승인 대기 시간 초과(10분)" {
		t.Fatalf("ReasonWaitTimeout(10분) = %q", got)
	}
	if got := ReasonWaitTimeout(90 * time.Second); got != "승인 대기 시간 초과(1분 30초)" {
		t.Fatalf("ReasonWaitTimeout(90초) = %q", got)
	}
	if !IsWaitTimeoutReason(ReasonWaitTimeout(3*time.Second)) || IsWaitTimeoutReason(ReasonExpired(time.Minute)) {
		t.Fatal("IsWaitTimeoutReason")
	}
}

func TestLoadConfWaitTimeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cross_check.conf")
	base := "target_dir=/etc/x/targets\nusers=alice,bob\n"
	os.WriteFile(p, []byte(base), 0o644)
	if c, err := LoadConf(p); err != nil || c.WaitTimeout != DefaultWaitTimeout || DefaultWaitTimeout != 10*time.Minute {
		t.Fatalf("기본 wait_timeout_sec(600): %+v %v", c, err)
	}
	for _, v := range []string{"30", "600", "86400"} {
		os.WriteFile(p, []byte(base+"wait_timeout_sec="+v+"\n"), 0o644)
		if _, err := LoadConf(p); err != nil {
			t.Errorf("wait_timeout_sec=%s: %v", v, err)
		}
	}
	for _, v := range []string{"29", "86401", "0", "-1", "abc", "", "10m"} {
		os.WriteFile(p, []byte(base+"wait_timeout_sec="+v+"\n"), 0o644)
		_, err := LoadConf(p)
		if err == nil || !strings.Contains(err.Error(), "wait_timeout_sec 는 30~86400") {
			t.Errorf("wait_timeout_sec=%q 는 범위 오류여야 함: %v", v, err)
		}
	}
	os.WriteFile(p, []byte(base+"wait_timeout_sec=60\nwait_timeout_sec=90\n"), 0o644)
	if _, err := LoadConf(p); err == nil || !strings.Contains(err.Error(), "중복") {
		t.Errorf("중복 키는 오류: %v", err)
	}
	// Apply 로 Config 에 옮겨진다
	os.WriteFile(p, []byte(base+"wait_timeout_sec=120\n"), 0o644)
	c, err := LoadConf(p)
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	c.Apply(&cfg)
	if cfg.WaitTimeout != 2*time.Minute {
		t.Fatalf("Apply: %v", cfg.WaitTimeout)
	}
	t.Setenv(EnvMinWaitTimeoutSec, "2")
	os.WriteFile(p, []byte(base+"wait_timeout_sec=2\n"), 0o644)
	if c, err := LoadConf(p); err != nil || c.WaitTimeout != 2*time.Second {
		t.Fatalf("시험용 최소값: %+v %v", c, err)
	}
	t.Setenv(EnvMinWaitTimeoutSec, "0") // 1 미만은 무시(원래 최소 30)
	if _, err := LoadConf(p); err == nil {
		t.Fatal("시험용 최소값이 무효면 2초는 거부")
	}
}

// 대기로 제한 시간을 넘기면 중단 + 사유 + 작업자 연결에 중단 이벤트(그 뒤 연결 닫힘). 승인된 작업은 영향 없음.
func TestWaitTimeoutAbortsWaitingOnly(t *testing.T) {
	clk := newFakeClock()
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.Now = clk.Now
		cfg.WaitTimeout = 5 * time.Minute
		cfg.ApprovalTTL = time.Hour // 승인된 작업이 승인 유효시간으로 만료되지 않게
	})
	c := h.client()
	waiting := h.run(c, "alice", "bob", []string{"vm01ev01"})
	if waiting.WaitSec != 300 || waiting.WaitExpiresAt == nil || !waiting.WaitExpiresAt.Equal(clk.Now().Add(5*time.Minute)) {
		t.Fatalf("run 응답의 wait_timeout_sec/wait_expires_at: %d %v", waiting.WaitSec, waiting.WaitExpiresAt)
	}
	nextEvent(t, waiting, StatusWaiting, "")
	approved := h.run(c, "alice", "bob", []string{"vm01ev02"})
	nextEvent(t, approved, StatusWaiting, "")
	if err := c.Approve(approved.ID, "bob", testPasswords["bob"], "vm01ev02"); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, approved, StatusApproved, "")

	clk.Add(5 * time.Minute) // 딱 제한 시간: 아직 중단 아님(초과해야 중단)
	h.srv.sweep()
	if j := h.job(waiting.ID); j.Status != StatusWaiting {
		t.Fatalf("제한 시간 전에는 대기 유지: %+v", j)
	}
	clk.Add(time.Second)
	h.srv.sweep()
	want := "승인 대기 시간 초과(5분)"
	if j := h.job(waiting.ID); j.Status != StatusAborted || j.Reason != want {
		t.Fatalf("대기 만료 → 중단(%s): %+v", want, j)
	}
	ev := nextEvent(t, waiting, StatusAborted, want)
	if ev.By != BySystem {
		t.Errorf("대기 만료 이벤트 by = %q", ev.By)
	}
	expectClosed(t, waiting)
	if j := h.job(approved.ID); j.Status != StatusApproved || j.Phase != PhaseNone {
		t.Fatalf("승인된 작업은 대기 제한의 영향 없음: %+v", j)
	}
	// 승인된 작업은 시작·완료까지 정상
	if err := c.Start(approved.ID, approved.Token); err != nil {
		t.Fatal(err)
	}
	clk.Add(time.Hour)
	h.srv.sweep()
	if j := h.job(approved.ID); j.Phase != PhaseWorking {
		t.Fatalf("작업중은 대기 제한의 영향 없음: %+v", j)
	}
	// 만료된 대기 작업은 승인·취소할 수 없다
	wantDaemonErr(t, c.Approve(waiting.ID, "bob", testPasswords["bob"], "vm01ev01"), "대기 상태가 아니")
	wantDaemonErr(t, c.Cancel(waiting.ID, "bob", testPasswords["bob"]), "대기 상태가 아니")
	if !strings.Contains(h.logs.String(), "작업 "+waiting.ID+" 중단: "+want) {
		t.Fatalf("로그: %s", h.logs.String())
	}
	h.srv.mu.Lock()
	n := len(h.srv.waiting)
	h.srv.mu.Unlock()
	if n != 0 {
		t.Fatalf("waiting 추적이 정리되지 않음: %d", n)
	}
}

// 관리자 승인·확인자 취소·작업자 연결 끊김으로 대기를 벗어난 작업은 대기 제한으로 다시 바뀌지 않는다.
func TestWaitTimeoutNoConflictWithOtherPaths(t *testing.T) {
	clk := newFakeClock()
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.Now = clk.Now
		cfg.WaitTimeout = time.Minute
		cfg.ApprovalTTL = time.Hour
	})
	c := h.client()
	apw := testPasswords["admin"]
	byAdmin := h.run(c, "alice", "bob", []string{"vm01ev01"})
	canceled := h.run(c, "alice", "bob", []string{"vm01ev02"})
	gone := h.run(c, "alice", "bob", []string{"vm02ev01"})
	for _, rh := range []*RunHandle{byAdmin, canceled, gone} {
		nextEvent(t, rh, StatusWaiting, "")
	}
	if err := c.AdminSet(apw, byAdmin.ID, StatusApproved, "점검"); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, byAdmin, StatusApproved, ReasonAdminSet)
	if err := c.Cancel(canceled.ID, "bob", testPasswords["bob"]); err != nil {
		t.Fatal(err)
	}
	gone.Close()
	waitFor(t, 3*time.Second, "연결 끊김 → 중단", func() bool { return h.job(gone.ID).Status == StatusAborted })

	clk.Add(2 * time.Minute)
	h.srv.sweep()
	if j := h.job(byAdmin.ID); j.Status != StatusApproved {
		t.Fatalf("관리자 승인 작업은 그대로: %+v", j)
	}
	if j := h.job(canceled.ID); j.Reason != ReasonCancel("bob") {
		t.Fatalf("취소 사유 유지: %+v", j)
	}
	if j := h.job(gone.ID); j.Reason != ReasonWorkerGone {
		t.Fatalf("연결 끊김 사유 유지: %+v", j)
	}
}

func TestPingWaitTimeout(t *testing.T) {
	h := newHarness(t, nil)
	info, err := h.client().Info()
	if err != nil || info.WaitTimeout != DefaultWaitTimeout {
		t.Fatalf("Info 기본 대기 제한 = %+v, %v", info, err)
	}
	h2 := newHarness(t, func(h *harness, cfg *Config) { cfg.WaitTimeout = 45 * time.Second })
	if info, err := h2.client().Info(); err != nil || info.WaitTimeout != 45*time.Second {
		t.Fatalf("Info 45초 = %+v, %v", info, err)
	}
}

// SIGHUP: wait_timeout_sec 를 다시 읽고, 이미 대기 중인 작업에도 새 값(등록 시각 + 새 값)이 적용된다.
// 잘못된 값이면 이전 값을 유지한다.
func TestReloadWaitTimeoutOnSIGHUP(t *testing.T) {
	clk := newFakeClock()
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.Now = clk.Now })
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	nextEvent(t, rh, StatusWaiting, "")

	conf := filepath.Join(h.dir, "cross_check.conf")
	head := "target_dir=" + h.targetDir + "\nusers=" + strings.Join(testUsers, ",") + "\n"
	os.WriteFile(conf, []byte(head+"wait_timeout_sec=60\n"), 0o644)
	if _, _, err := h.srv.ReloadApprovalTTL(conf); err != nil {
		t.Fatal(err)
	}
	if h.srv.WaitTimeout() != time.Minute {
		t.Fatalf("SIGHUP 으로 wait_timeout_sec 반영: %v", h.srv.WaitTimeout())
	}
	if !strings.Contains(h.logs.String(), "승인 대기 제한 시간 10분(600초) → 1분(60초)") {
		t.Fatalf("로그: %s", h.logs.String())
	}
	if info, err := c.Info(); err != nil || info.WaitTimeout != time.Minute {
		t.Fatalf("ping 에 새 값: %+v %v", info, err)
	}
	// 잘못된 값: 이전 값(1분) 유지
	os.WriteFile(conf, []byte(head+"wait_timeout_sec=5\n"), 0o644)
	if _, _, err := h.srv.ReloadApprovalTTL(conf); err == nil {
		t.Fatal("범위 밖 값은 오류")
	}
	if h.srv.WaitTimeout() != time.Minute || !strings.Contains(h.logs.String(), "승인 대기 제한 1분 유지") {
		t.Fatalf("이전 값 유지: %v / %s", h.srv.WaitTimeout(), h.logs.String())
	}
	clk.Add(time.Minute + time.Second) // 기존 대기 작업에도 새 값(1분) 적용
	h.srv.sweep()
	nextEvent(t, rh, StatusAborted, "승인 대기 시간 초과(1분)")
	expectClosed(t, rh)
}

// 데몬 재시작(kill -9 뒤 다시 뜸): 저장소에 남아 있던 대기 작업은 원래대로 "데몬 재시작" 으로 중단되고,
// 새 데몬의 대기 제한 추적에는 들어가지 않는다(사유가 "승인 대기 시간 초과" 로 덮이지 않음).
func TestWaitTimeoutRestartKeepsRestartReason(t *testing.T) {
	clk := newFakeClock()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "state", "jobs.json")
	os.MkdirAll(filepath.Dir(storePath), 0o700)
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	old, err := st.Add(store.Job{User: "alice", Checker: "bob", Targets: []string{"vm01ev01"}, Status: StatusWaiting,
		CreatedAt: clk.Now().Add(-time.Hour), UpdatedAt: clk.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.Now = clk.Now
		cfg.StorePath = storePath
	})
	h.srv.sweep()
	j := h.job(old.ID)
	if j.Status != StatusAborted || j.Reason != ReasonRestart {
		t.Fatalf("재시작 전 대기 작업은 데몬 재시작으로 중단: %+v", j)
	}
	h.srv.mu.Lock()
	n := len(h.srv.waiting)
	h.srv.mu.Unlock()
	if n != 0 {
		t.Fatalf("재시작 전 작업이 대기 제한 추적에 들어감: %d", n)
	}
}
