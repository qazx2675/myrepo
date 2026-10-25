package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatTTL(t *testing.T) {
	cases := map[int]string{
		-5: "0초", 0: "0초", 1: "1초", 10: "10초", 45: "45초", 59: "59초", 60: "1분", 90: "1분 30초", 119: "1분 59초",
		120: "2분", 121: "2분 1초", 600: "10분", 3599: "59분 59초", 3600: "1시간", 3660: "1시간 1분", 3601: "1시간 1초", 7200: "2시간",
		5400: "1시간 30분",
	}
	for sec, want := range cases {
		if got := FormatTTL(sec); got != want {
			t.Errorf("FormatTTL(%d) = %q, want %q", sec, got, want)
		}
	}
	if FormatTTLDuration(2*time.Minute) != "2분" || FormatTTLDuration(1500*time.Millisecond) != "2초" || FormatTTLDuration(0) != "0초" {
		t.Errorf("FormatTTLDuration: %q %q", FormatTTLDuration(2*time.Minute), FormatTTLDuration(1500*time.Millisecond))
	}
}

func TestReasonExpiredText(t *testing.T) {
	if got := ReasonExpired(DefaultApprovalTTL); got != "승인 후 2분 내 미진행" {
		t.Fatalf("기본 사유 문구가 바뀜: %q", got)
	}
	if got := ReasonExpired(90 * time.Second); got != "승인 후 1분 30초 내 미진행" {
		t.Fatalf("%q", got)
	}
	for _, d := range []time.Duration{10 * time.Second, 2 * time.Minute, time.Hour} {
		if !IsExpiredReason(ReasonExpired(d)) {
			t.Errorf("IsExpiredReason(%q) = false", ReasonExpired(d))
		}
	}
	if IsExpiredReason(ReasonRestart) || IsExpiredReason("승인 상태가 아니라 시작할 수 없음") || IsExpiredReason(MsgAuthFailed) {
		t.Error("다른 문구를 만료 사유로 오인")
	}
}

func TestLoadConfApprovalTTL(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cross_check.conf")
	base := "target_dir=/etc/vcetner_pwon_off/targets\nusers=alice,bob\n"

	// 키가 없으면 기본 120초
	os.WriteFile(p, []byte(base), 0o644)
	if c, err := LoadConf(p); err != nil || c.ApprovalTTL != 120*time.Second {
		t.Fatalf("기본값: %+v %v", c, err)
	}
	// 경계값 10, 3600 허용
	for _, v := range []string{"10", "11", "20", "90", "120", "3599", "3600"} {
		os.WriteFile(p, []byte(base+"approval_ttl_sec="+v+"\n"), 0o644)
		c, err := LoadConf(p)
		if err != nil {
			t.Errorf("approval_ttl_sec=%s: %v", v, err)
			continue
		}
		if got := int(c.ApprovalTTL / time.Second); got != atoiT(t, v) {
			t.Errorf("approval_ttl_sec=%s → %d초", v, got)
		}
	}
	// 범위 밖·숫자 아님은 한국어로 범위를 알려 주는 오류
	for _, v := range []string{"0", "1", "5", "9", "3601", "99999", "-120", "abc", "", "12.5", "2m", "1e3"} {
		os.WriteFile(p, []byte(base+"approval_ttl_sec="+v+"\n"), 0o644)
		_, err := LoadConf(p)
		if err == nil {
			t.Errorf("approval_ttl_sec=%q 는 오류여야 함", v)
			continue
		}
		for _, want := range []string{"approval_ttl_sec", "10~3600", "기본 120 = 2분"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("approval_ttl_sec=%q 오류 문구에 %q 가 없음: %v", v, want, err)
			}
		}
	}
	// 시험용 환경변수는 최소값만 낮춘다(상한·0 이하는 그대로 거부)
	t.Setenv(EnvMinTTLSec, "3")
	os.WriteFile(p, []byte(base+"approval_ttl_sec=3\n"), 0o644)
	if c, err := LoadConf(p); err != nil || c.ApprovalTTL != 3*time.Second {
		t.Fatalf("시험용 최소값 3초: %+v %v", c, err)
	}
	for _, v := range []string{"2", "0", "3601"} {
		os.WriteFile(p, []byte(base+"approval_ttl_sec="+v+"\n"), 0o644)
		if _, err := LoadConf(p); err == nil {
			t.Errorf("시험용 최소값 3 에서도 %s 는 거부", v)
		}
	}
	t.Setenv(EnvMinTTLSec, "0")
	os.WriteFile(p, []byte(base+"approval_ttl_sec=5\n"), 0o644)
	if _, err := LoadConf(p); err == nil {
		t.Error("잘못된 시험용 값(0)은 무시되고 최소 10초가 유지되어야 함")
	}
}

func atoiT(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// Config 로 준 값도 그대로 쓰이고(핑으로 확인), 0 이면 기본 120초다.
func TestPingReportsTTLFromConfig(t *testing.T) {
	h := newHarness(t, func(h *harness, cfg *Config) { cfg.ApprovalTTL = 45 * time.Second })
	c := h.client()
	d, err := c.ApprovalTTL()
	if err != nil || d != 45*time.Second {
		t.Fatalf("ApprovalTTL = %v, %v", d, err)
	}
	if h.srv.TTL() != 45*time.Second {
		t.Fatalf("Server.TTL = %v", h.srv.TTL())
	}
	if !strings.Contains(h.logs.String(), "승인 유효 45초(45초)") {
		t.Errorf("시작 로그에 유효시간이 없음:\n%s", h.logs.String())
	}

	h2 := newHarness(t, nil)
	if d, err := h2.client().ApprovalTTL(); err != nil || d != 120*time.Second {
		t.Fatalf("기본 ApprovalTTL = %v, %v", d, err)
	}
}

// run 응답·승인 이벤트·list/get 이 유효시간과 만료 시각을 알려 주고, 만료 사유 문구도 그 값으로 만들어진다.
func TestTTLReportedInRunEventsAndJobViews(t *testing.T) {
	clk := newFakeClock()
	h := newHarness(t, func(h *harness, cfg *Config) {
		cfg.Now = clk.Now
		cfg.ApprovalTTL = 90 * time.Second
	})
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	if rh.TTLSec != 90 {
		t.Fatalf("run 응답 ttl_sec = %d", rh.TTLSec)
	}
	nextEvent(t, rh, StatusWaiting, "")
	if v, err := c.Get(rh.ID, "alice", ""); err != nil || v.TTLSec != 90 || v.ExpiresAt != nil {
		t.Fatalf("대기 작업 view: %+v %v", v, err)
	}
	approvedAt := clk.Now()
	if err := c.Approve(rh.ID, "bob", testPasswords["bob"], h.paste(rh.ID)); err != nil {
		t.Fatal(err)
	}
	ev := nextEvent(t, rh, StatusApproved, "")
	if ev.TTLSec != 90 || ev.ExpiresAt == nil || !ev.ExpiresAt.Equal(approvedAt.Add(90*time.Second)) {
		t.Fatalf("승인 이벤트 ttl: %+v", ev)
	}
	v, err := c.Get(rh.ID, "bob", "")
	if err != nil || v.TTLSec != 90 || v.ExpiresAt == nil || !v.ExpiresAt.Equal(approvedAt.Add(90*time.Second)) {
		t.Fatalf("승인 작업 view: %+v %v", v, err)
	}
	jobs, err := c.List("alice")
	if err != nil || len(jobs) != 1 || jobs[0].TTLSec != 90 || jobs[0].ExpiresAt == nil {
		t.Fatalf("list view: %+v %v", jobs, err)
	}

	// 90초 정각까지 허용, 그 이후는 "승인 후 1분 30초 내 미진행"
	clk.Add(90*time.Second + time.Second)
	wantDaemonErr(t, c.Start(rh.ID, rh.Token), "승인 후 1분 30초 내 미진행")
	nextEvent(t, rh, StatusAborted, "승인 후 1분 30초 내 미진행")
	if j := h.job(rh.ID); j.Reason != "승인 후 1분 30초 내 미진행" {
		t.Fatalf("저장된 사유 = %q", j.Reason)
	}

	// 시작한 작업은 expires_at 이 사라진다
	r2 := h.run(c, "alice", "bob", []string{"vm01ev02"})
	c.Approve(r2.ID, "bob", testPasswords["bob"], h.paste(r2.ID))
	if err := c.Start(r2.ID, r2.Token); err != nil {
		t.Fatal(err)
	}
	if v, err := c.Get(r2.ID, "alice", ""); err != nil || v.ExpiresAt != nil || v.TTLSec != 90 {
		t.Fatalf("시작한 작업 view: %+v %v", v, err)
	}
}

// 구버전 클라이언트/데몬 호환: 새 필드는 모두 선택이라 빠져도 해석되고, 기존 JSON 에서는 새 필드가 없어도 된다.
func TestTTLFieldsAreOptionalOnTheWire(t *testing.T) {
	b, _ := encodeLine(Response{OK: true})
	if string(b) != "{\"ok\":true,\"error\":\"\"}\n" {
		t.Fatalf("ttl 이 0 이면 응답에 안 나와야 함(구버전 호환): %s", b)
	}
	b, _ = encodeLine(Response{OK: true, TTLSec: 120})
	if !strings.Contains(string(b), `"ttl_sec":120`) {
		t.Fatalf("ping 응답: %s", b)
	}
	b, _ = encodeLine(Event{Event: EventStatus, Status: StatusWaiting})
	if strings.Contains(string(b), "ttl_sec") || strings.Contains(string(b), "expires_at") {
		t.Fatalf("대기 이벤트에는 ttl 필드가 없어야 함: %s", b)
	}
}

// SIGHUP 처리 본체: 유효한 값은 즉시 반영(이미 승인된 작업에도), 잘못된 값·깨진 파일은 기존 값을 유지하고 로그를 남긴다.
func TestReloadApprovalTTL(t *testing.T) {
	clk := newFakeClock()
	h := newHarness(t, func(_ *harness, cfg *Config) {
		cfg.Now = clk.Now
		cfg.ApprovalTTL = 120 * time.Second
	})
	conf := filepath.Join(h.dir, "cross_check.conf")
	write := func(extra string) {
		body := "target_dir=" + h.targetDir + "\nusers=" + strings.Join(testUsers, ",") + "\n" + extra
		if err := os.WriteFile(conf, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := h.client()

	// 승인된 작업을 하나 만들어 둔다(120초 기준 승인)
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	c.Approve(rh.ID, "bob", testPasswords["bob"], h.paste(rh.ID))

	// 값을 줄이면 이미 승인된 작업에도 적용
	write("approval_ttl_sec=30\n")
	old, now, err := h.srv.ReloadApprovalTTL(conf)
	if err != nil || old != 120*time.Second || now != 30*time.Second {
		t.Fatalf("reload = %v %v %v", old, now, err)
	}
	if d, _ := c.ApprovalTTL(); d != 30*time.Second {
		t.Fatalf("ping ttl = %v", d)
	}
	if !strings.Contains(h.logs.String(), "2분(120초) → 30초(30초)") {
		t.Errorf("변경 로그 없음:\n%s", h.logs.String())
	}

	// 잘못된 값: 기존(30초) 유지
	for _, extra := range []string{"approval_ttl_sec=5\n", "approval_ttl_sec=99999\n", "approval_ttl_sec=abc\n", "approval_ttl_sec=60\napproval_ttl_sec=70\n", "unknown_key=1\n"} {
		write(extra)
		if _, now, err := h.srv.ReloadApprovalTTL(conf); err == nil || now != 30*time.Second {
			t.Errorf("%q: err=%v now=%v — 오류와 기존 값 유지여야 함", extra, err, now)
		}
		if h.srv.TTL() != 30*time.Second {
			t.Errorf("%q 뒤 TTL = %v", extra, h.srv.TTL())
		}
	}
	os.Remove(conf) // 파일이 없어져도 유지
	if _, _, err := h.srv.ReloadApprovalTTL(conf); err == nil || h.srv.TTL() != 30*time.Second {
		t.Fatalf("없는 파일: %v ttl=%v", err, h.srv.TTL())
	}
	if !strings.Contains(h.logs.String(), "30초 유지") {
		t.Errorf("실패 로그에 유지되는 값이 없음:\n%s", h.logs.String())
	}

	// 변경 후 31초가 지나면 이미 승인된 작업은 새 값(30초)으로 만료
	clk.Add(31 * time.Second)
	wantDaemonErr(t, c.Start(rh.ID, rh.Token), "승인 후 30초 내 미진행")

	// users 가 달라진 conf 는 TTL 만 반영하고 경고를 남긴다
	write("approval_ttl_sec=200\n")
	body, _ := os.ReadFile(conf)
	os.WriteFile(conf, []byte(strings.Replace(string(body), "users=alice,", "users=", 1)), 0o644)
	if _, now, err := h.srv.ReloadApprovalTTL(conf); err != nil || now != 200*time.Second {
		t.Fatalf("users 변경 conf: %v %v", now, err)
	}
	if !strings.Contains(h.logs.String(), "users·target_dir·auth 의 변경은 SIGHUP 으로 반영되지 않습니다") {
		t.Errorf("재시작 필요 경고가 없음:\n%s", h.logs.String())
	}
	if h.srv.cfg.AllowedUsers[0] != "alice" {
		t.Error("users 는 바뀌면 안 됨")
	}
}
