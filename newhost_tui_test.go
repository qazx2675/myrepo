// newhost_tui_test.go - 신규 설치 양식 흐름(w→2 / CLI newhost): 붙여넣기·고치기, 지정 언어(덮어쓰기·열 수정·원자성·+ 템플릿),
// 이력 추천, DNS 연결(가짜 결과 a/n/s/q·비활성), 미리보기·저장, AWX 연결(nodeinfo N 강제·AWX_DNS_CHECKED), 세션 user·-user,
// 원격 클라이언트 안내, Ctrl+X 취소·Esc 이전 단계, oneShot 루프·대량 붙여넣기, 화면 틀 스크롤, 골든(폭 60/80/120).
package main

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type nhH struct {
	t      *testing.T
	st     *tuiState
	rg     *rig
	awx    string
	state  string
	envs   [][]string // awxExec 호출마다 env
	verify []string   // awxExec 시점의 AWX_VERIFY_FILE 내용
}

// nhSetup: awx_dir(01 스크립트 있음)·AUTO_SETUP_DIR 임시, conf 템플릿 3개, 프로파일 없음, DNS 는 기본 비활성(가짜), awxExec 가짜.
func nhSetup(t *testing.T, user string) *nhH {
	t.Helper()
	saveSessionState(t)
	setProfiles(t)
	setNewhostTemplates(t, "A|sec bond sda 500 8.10 uefi", "mec|mec eth0 sda 500 9.6 uefi", "mec eth0 nvme 400 9.6 uefi")
	oldOS8, oldDNS, oldExec := os8_mgmt, newhostDNS, awxExec
	t.Cleanup(func() { os8_mgmt, newhostDNS, awxExec = oldOS8, oldDNS, oldExec })
	os8_mgmt = ""
	h := &nhH{t: t, awx: t.TempDir(), state: t.TempDir()}
	os.WriteFile(filepath.Join(h.awx, awxScript), []byte("#!/bin/bash\n"), 0755)
	awx_dir = h.awx
	t.Setenv("AUTO_SETUP_DIR", h.state)
	newhostDNS = func(es []DNSEntry) DNSReport { return DNSReport{Reason: DNSDisabledReason, Detail: "가짜"} }
	awxExec = func(dir string, env []string) (int, error) {
		h.envs = append(h.envs, env)
		if v := envMap(env)["AWX_VERIFY_FILE"]; v != "" {
			b, _ := os.ReadFile(v)
			h.verify = append(h.verify, string(b))
		}
		return 0, nil
	}
	forceUser = user
	env, rg := newRig(strings.NewReader(""))
	h.rg = rg
	h.st = &tuiState{src: &fakeSrc{snap: twoGroupSnap(), local: true}, env: env, pending: map[string]time.Time{}, restore: func() {}}
	h.st.refresh()
	h.st.draw()
	return h
}

func (h *nhH) keys(s string) {
	h.t.Helper()
	for _, ev := range parseKeys([]byte(s)) {
		h.st.handle(ev)
		h.st.pollNewhost()
		h.st.draw()
	}
}

func (h *nhH) frame() string { return stripANSI(lastFrame(h.rg)) }

func (h *nhH) n() *newhostState {
	h.t.Helper()
	if h.st.nh == nil {
		h.t.Fatalf("신규 설치 양식이 열려 있지 않음 (msg=%q)", h.st.sel.Msg)
	}
	return h.st.nh
}

func (h *nhH) waitStep(step int) {
	h.t.Helper()
	waitFor(h.t, "신규 설치 양식 단계 "+strconv.Itoa(step), func() bool {
		h.st.pollNewhost()
		h.st.draw()
		return h.st.nh != nil && h.st.nh.step == step
	})
}

// nhFakeDNS: 호스트별 상태를 정해 둔 가짜 점검 (dnsIP 가 있으면 불일치, st 에 있으면 그 상태, 나머지는 일치)
func nhFakeDNS(dnsIP map[string]string, st map[string]DNSStatus, warn map[string][]string) func([]DNSEntry) DNSReport {
	return func(es []DNSEntry) DNSReport {
		rep := DNSReport{Enabled: true, Servers: []string{"10.9.9.9"}}
		for _, e := range es {
			r := DNSHostResult{Host: e.Host, IP: e.IP, Status: DNSMatch, DNSIP: e.IP}
			if ip, ok := dnsIP[e.Host]; ok {
				r.Status, r.DNSIP, r.Warnings = DNSMismatch, ip, warn[e.Host]
			} else if s, ok := st[e.Host]; ok {
				r.Status, r.DNSIP, r.Note = s, "", s.Label()+" 사유"
			}
			rep.Hosts = append(rep.Hosts, r)
		}
		return rep
	}
}

const nhPaste3 = "svr001 10.0.0.1 AA-BB-CC-DD-EE-01\rsvr002,10.0.0.2,aabb.ccdd.ee02\r10.0.0.3\taabbccddee03\tspice-a01\r"

func readLog(t *testing.T, state string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(state, "auto_setup.log"))
	return string(b)
}

// 전체 흐름: 붙여넣기 → 지정·열 수정 → DNS 불일치 a → 미리보기 → y 저장(백업·권한 유지) → AWX y (프로파일 없음)
func TestNewhostFlowFull(t *testing.T) {
	h := nhSetup(t, "alice")
	os.WriteFile(filepath.Join(h.awx, "alice.txt"), []byte("old content\n"), 0600)
	newhostDNS = nhFakeDNS(map[string]string{"svr001": "10.0.0.11"}, map[string]DNSStatus{"spice-a01": DNSUnregistered},
		map[string][]string{"svr001": {"등록IP 10.0.0.1 은 svr009 의 IP (DNS)"}})
	h.keys("w2")
	n := h.n()
	if n.user != "alice" || n.step != nhPaste || !strings.Contains(h.frame(), "신규 설치 양식 [user: alice] - 1/5 붙여넣기") {
		t.Fatalf("시작 화면:\n%s", h.frame())
	}
	h.keys(nhPaste3)
	if len(n.rows) != 3 || n.rows[2].Host != "spice-a01" || n.rows[2].MAC != "aa:bb:cc:dd:ee:03" || n.rows[1].MAC != "aa:bb:cc:dd:ee:02" {
		t.Fatalf("붙여넣기: %+v", n.rows)
	}
	h.keys("\r") // 빈 줄 = 끝
	if n.step != nhAssign {
		t.Fatalf("빈 줄 뒤 템플릿 지정이어야 함: step=%d msg=%q", n.step, n.msg)
	}
	f := h.frame()
	for _, s := range []string{"1 [A]    tmp tmp sec <hostname> <ip> <mac> bond sda  sda5      500 8.10 uefi", "(sec-bond-sda-500-8.10-uefi)",
		"3        tmp tmp mec <hostname> <ip> <mac> eth0 nvme nvme0n1p8 400 9.6  uefi", "값 없음 3", "지정 > _"} {
		if !strings.Contains(f, s) {
			t.Errorf("지정 화면에 %q 없음:\n%s", s, f)
		}
	}
	h.keys("svr001-002=1  spice*=MEC\r")
	if n.msg != "3행 지정했습니다" || n.eff(n.rows[2]).Label != "2 [mec]" {
		t.Fatalf("지정: msg=%q %+v", n.msg, n.eff(n.rows[2]))
	}
	h.keys("svr002 os=9.6\r")
	if v := n.eff(n.rows[1]); v.Label != "1 [A]*" || v.T.OS != "9.6" || v.Src != nhSrcEdit {
		t.Fatalf("열 수정: %+v", v)
	}
	h.keys("\r")
	h.waitStep(nhDNSAsk)
	f = h.frame()
	for _, s := range []string{"a 모두 DNS 로 / n 그대로 / s 하나씩 / q 중단 :", "svr001", "10.0.0.11", "⚠ 등록IP 10.0.0.1 은 svr009 의 IP (DNS)", "spice-a01", "미등록 사유"} {
		if !strings.Contains(f, s) {
			t.Errorf("DNS 화면에 %q 없음:\n%s", s, f)
		}
	}
	h.keys("a")
	if n.step != nhPreview || n.rows[0].IP != "10.0.0.11" || n.msg != "DNS IP 로 1대 바꿨습니다" {
		t.Fatalf("a 뒤: step=%d ip=%s msg=%q", n.step, n.rows[0].IP, n.msg)
	}
	f = h.frame()
	for _, s := range []string{"DNS 점검: 일치 1 · 불일치 1 (DNS 로 바꿈 1) · 판단 불가 0 · 미등록 1", "■ sec-bond-sda-500-8.10-uefi = 1 [A] — 1대",
		"(DNS 로 바꿈 10.0.0.1 → 10.0.0.11)", "■ mec-eth0-sda-500-9.6-uefi = 2 [mec] — 1대", "DNS 경고 (바꾸지 않음):", "y 를 누르면"} {
		if !strings.Contains(f, s) {
			t.Errorf("미리보기에 %q 없음:\n%s", s, f)
		}
	}
	h.keys("y")
	if n.step != nhSaved {
		t.Fatalf("저장 실패: %q", n.msg)
	}
	want := "tmp tmp sec svr001 10.0.0.11 aa:bb:cc:dd:ee:01 bond sda sda5 500 8.10 uefi\n" +
		"tmp tmp sec svr002 10.0.0.2 aa:bb:cc:dd:ee:02 bond sda sda5 500 9.6 uefi\n" +
		"tmp tmp mec spice-a01 10.0.0.3 aa:bb:cc:dd:ee:03 eth0 sda sda5 500 9.6 uefi\n"
	p := filepath.Join(h.awx, "alice.txt")
	if b, _ := os.ReadFile(p); string(b) != want {
		t.Errorf("alice.txt:\n%s", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0600 {
		t.Errorf("기존 권한 유지: %v", fi.Mode().Perm())
	}
	if b, _ := os.ReadFile(n.saved.Backup); string(b) != "old content\n" {
		t.Errorf("백업 %s: %q", n.saved.Backup, b)
	}
	if b, _ := os.ReadFile(n.saved.History); string(b) != want {
		t.Errorf("이력 %s: %q", n.saved.History, b)
	}
	lg := readLog(t, h.state)
	if !strings.Contains(lg, "newhost user=alice [DNS] svr001 10.0.0.1 -> 10.0.0.11") || !strings.Contains(lg, "newhost user=alice 3대 저장 "+p) {
		t.Errorf("auto_setup.log:\n%s", lg)
	}
	f = h.frame()
	if !strings.Contains(f, "AWX 실행할까요? [y/n]") || !strings.Contains(f, "tmp tmp mec spice-a01 10.0.0.3") || !strings.Contains(f, "저장 완료: "+p) {
		t.Errorf("저장 화면:\n%s", f)
	}
	h.keys("y")
	if h.st.nh != nil || !h.st.nhSaved || len(h.envs) != 1 {
		t.Fatalf("AWX 로 이어져야 함: nh=%v envs=%d msg=%q", h.st.nh != nil, len(h.envs), h.st.sel.Msg)
	}
	m := envMap(h.envs[0])
	if _, ok := m["AWX_AUTO_NODEINFO"]; m["AWX_USER"] != "alice" || ok || m["AWX_DNS_CHECKED"] != "1" || m["AWX_AUTO"] == "1" {
		t.Errorf("AWX env (프로파일 없음 — 자동 N 이 없어야 함): %v", m)
	}
	if h.verify[0] != "svr001\nsvr002\nspice-a01\n" {
		t.Errorf("대상 확인 파일: %q", h.verify[0])
	}
	if _, err := os.Stat(m["AWX_VERIFY_FILE"]); !os.IsNotExist(err) {
		t.Errorf("대상 확인 파일이 지워지지 않음")
	}
	if !strings.Contains(h.rg.out.String(), "프로파일 없이 일반 실행합니다") || h.st.sel.Msg != "AWX 종료 (rc=0)" {
		t.Errorf("안내 줄·종료 메시지: %q", h.st.sel.Msg)
	}
}

// 프로파일이 있으면 기존 w 처럼 고르고, nodeinfo 는 프로파일 값(y)과 무관하게 n. DNS 비활성이면 AWX_DNS_CHECKED 없음.
func TestNewhostAwxProfiles(t *testing.T) {
	for _, path := range []string{"pick", "plain", "esc"} {
		h := nhSetup(t, "alice")
		setProfiles(t, "y|2026|기본")
		h.keys("w2" + "svr001 10.0.0.1 aa:bb:cc:dd:ee:01\r.\r" + "*=A\r\r")
		h.waitStep(nhPreview)
		if !strings.Contains(h.frame(), DNSDisabledReason) {
			t.Errorf("DNS 비활성 사유가 미리보기에 없음:\n%s", h.frame())
		}
		h.keys("yy")
		if h.st.awx == nil || h.st.awx.nh == nil || !strings.Contains(h.frame(), "신규 설치 양식: user alice · 대상 1대 · y 로 프로파일을 고르면 nodeinfo 는 N 으로 실행") {
			t.Fatalf("%s: 프로파일 질문 화면이어야 함:\n%s", path, h.frame())
		}
		switch path {
		case "pick":
			h.keys("y1")
			m := envMap(h.envs[0])
			if m["AWX_AUTO"] != "1" || m["AWX_AUTO_NODEINFO"] != "n" || m["AWX_AUTO_OS"] != "2026" || m["AWX_USER"] != "alice" {
				t.Errorf("pick env: %v", m)
			}
			if _, ok := m["AWX_DNS_CHECKED"]; ok {
				t.Errorf("DNS 비활성인데 AWX_DNS_CHECKED")
			}
			if !strings.Contains(h.rg.out.String(), "nodeinfo 는 N 으로 실행합니다 (프로파일 1 의 nodeinfo=y 와 무관)") {
				t.Errorf("프로파일 안내 줄 없음")
			}
		case "plain":
			h.keys("n")
			if m := envMap(h.envs[0]); m["AWX_AUTO"] == "1" {
				t.Errorf("일반 실행 env: %v", m)
			} else if v, ok := m["AWX_AUTO_NODEINFO"]; ok {
				t.Errorf("프로파일을 고르지 않았는데 자동 N 이 적용됨: AWX_AUTO_NODEINFO=%s", v)
			}
			if h.st.oo != nil {
				t.Errorf("user·목록 입력 단계가 열리면 안 됨")
			}
		case "esc":
			h.keys("\x1b")
			if len(h.envs) != 0 || h.st.awx != nil || !strings.Contains(h.st.sel.Msg, nhSavedOnlyMsg) {
				t.Errorf("Esc: envs=%d msg=%q", len(h.envs), h.st.sel.Msg)
			}
		}
	}
}

// DNS s 하나씩(y/n), n 그대로, q 중단, 조회 중 Esc
func TestNewhostDNSChoices(t *testing.T) {
	start := func(t *testing.T) *nhH {
		h := nhSetup(t, "alice")
		newhostDNS = nhFakeDNS(map[string]string{"svr001": "10.0.0.11", "svr002": "10.0.0.12"}, nil, nil)
		h.keys("w2" + "svr001 10.0.0.1 aa:bb:cc:dd:ee:01\rsvr002 10.0.0.2 aa:bb:cc:dd:ee:02\r\r*=A\r\r")
		h.waitStep(nhDNSAsk)
		return h
	}
	h := start(t)
	h.keys("s")
	if !strings.Contains(h.frame(), "(1/2) svr001") || !strings.Contains(h.frame(), "DNS IP 로 바꿀까요? [y/n]") {
		t.Fatalf("하나씩:\n%s", h.frame())
	}
	h.keys("\x1b") // 처음 선택으로
	if h.n().step != nhDNSAsk {
		t.Fatalf("Esc → a/n/s/q")
	}
	h.keys("syn")
	n := h.n()
	if n.step != nhPreview || n.rows[0].IP != "10.0.0.11" || n.rows[1].IP != "10.0.0.2" || len(n.dnsChanged) != 1 {
		t.Errorf("s y n: %+v", n.rows)
	}
	h.keys("y")
	h.keys("n")
	if !h.st.nhSaved || !strings.Contains(h.st.sel.Msg, nhSavedOnlyMsg) || len(h.envs) != 0 {
		t.Errorf("저장 후 n: msg=%q", h.st.sel.Msg)
	}

	h = start(t)
	h.keys("n")
	if n := h.n(); n.step != nhPreview || n.rows[0].IP != "10.0.0.1" || n.msg != "등록값 그대로 진행합니다" {
		t.Errorf("n: %+v %q", n.rows, n.msg)
	}

	h = start(t)
	h.keys("q")
	if h.st.nh != nil || h.st.sel.Msg != nhStopMsg {
		t.Errorf("q: msg=%q", h.st.sel.Msg)
	}
	if _, err := os.Stat(filepath.Join(h.awx, "alice.txt")); !os.IsNotExist(err) {
		t.Errorf("q 인데 파일이 생김")
	}

	// 조회 중 Esc → 지정 단계로 (늦게 온 결과는 버림)
	h = nhSetup(t, "alice")
	release, finished := make(chan struct{}), make(chan struct{})
	newhostDNS = func(es []DNSEntry) DNSReport {
		defer close(finished) // 백그라운드 goroutine 이 newhostDNS 를 읽은 뒤 끝났음을 테스트가 알 수 있게 (-race)
		<-release
		return nhFakeDNS(map[string]string{"svr001": "10.0.0.11"}, nil, nil)(es)
	}
	h.keys("w2svr001 10.0.0.1 aa:bb:cc:dd:ee:01\r\r*=A\r\r")
	if h.n().step != nhDNS || !strings.Contains(h.frame(), "DNS 조회 중… 1대") || h.st.fastTick == nil {
		t.Fatalf("조회 중 화면·fast tick:\n%s", h.frame())
	}
	h.keys("\x1b")
	close(release)
	<-finished
	time.Sleep(50 * time.Millisecond)
	h.keys("x") // 아무 키 (지정 입력에 x)
	if n := h.n(); n.step != nhAssign || n.dnsDone || h.st.fastTick != nil {
		t.Errorf("조회 중 Esc: step=%d done=%v tick=%v", n.step, n.dnsDone, h.st.fastTick != nil)
	}
	// 미리보기 Esc → 지정, 다시 Enter 면 DNS 를 다시 점검
	newhostDNS = nhFakeDNS(nil, nil, nil)
	h.keys("\x7f\r")
	h.waitStep(nhPreview)
	h.keys("\x1b")
	if h.n().step != nhAssign {
		t.Errorf("미리보기 Esc → 지정")
	}
}

// 붙여넣기 오류·중복은 빨강 + 줄 번호, 고칠 때까지 진행 불가 — d(삭제)·e(다시 입력)·빈 줄 Backspace(앞 줄)
func TestNewhostPasteFix(t *testing.T) {
	h := nhSetup(t, "alice")
	h.keys("w2")
	h.keys("svr001 10.0.0.1 aa:bb:cc:dd:ee:01\rsvr002 10.0.0.2\rSVR001 10.0.0.3 aa:bb:cc:dd:ee:03\r\r")
	n := h.n()
	if n.step != nhPaste || !n.msgErr || !strings.Contains(n.msg, "오류 3줄 (행 1,2,3)") {
		t.Fatalf("오류가 있으면 진행 불가: step=%d msg=%q", n.step, n.msg)
	}
	f := h.frame()
	if !strings.Contains(f, "✗ 값이 2개 (hostname ip mac [템플릿] 필요)") || !strings.Contains(f, "✗ hostname 중복 (행 1,3)") || !strings.Contains(f, "오류 3줄 (빨강)") {
		t.Errorf("오류 표시:\n%s", f)
	}
	h.keys("d 2\r")
	if len(n.rows) != 2 || n.msg != "행 2 삭제 (1줄)" {
		t.Fatalf("d 2: %d %q", len(n.rows), n.msg)
	}
	h.keys("e 2\r")
	if n.editRow != 2 || n.paste.text() != "SVR001 10.0.0.3 aa:bb:cc:dd:ee:03" || !strings.Contains(h.frame(), "e 2 > SVR001") {
		t.Fatalf("e 2: %d %q", n.editRow, n.paste.text())
	}
	h.keys(strings.Repeat("\x7f", 33) + "svr003 10.0.0.3 aa:bb:cc:dd:ee:03\r")
	if n.editRow != 0 || len(n.rows) != 2 || n.rows[1].Host != "svr003" || n.rows[0].Dup != "" {
		t.Fatalf("e 로 고침: %+v", n.rows)
	}
	h.keys("\x7f") // 빈 줄에서 Backspace → 마지막 줄을 꺼내 고침
	if len(n.rows) != 1 || n.paste.text() != "svr003 10.0.0.3 aa:bb:cc:dd:ee:03" {
		t.Fatalf("Backspace: %d %q", len(n.rows), n.paste.text())
	}
	h.keys("\x04") // Ctrl+D = 현재 줄 확정 + 끝
	if n.step != nhAssign || len(n.rows) != 2 {
		t.Fatalf("Ctrl+D: step=%d rows=%d msg=%q", n.step, len(n.rows), n.msg)
	}
	h.keys("svr001=1\r\x1b") // Esc = 이전 단계 (지정은 hostname 으로 유지)
	if n.step != nhPaste || len(n.rows) != 2 {
		t.Fatalf("Esc → 붙여넣기")
	}
	h.keys(".\r")
	if n.step != nhAssign || n.eff(n.rows[0]).Label != "1 [A]" {
		t.Errorf(". 로 끝 + 지정 유지: step=%d %+v", n.step, n.eff(n.rows[0]))
	}
	for _, c := range []string{"d 9", "e 1,2"} {
		h.keys("\x1b" + c + "\r")
		if !n.msgErr {
			t.Errorf("%q: 오류여야 함 (msg=%q)", c, n.msg)
		}
		h.keys("\r")
	}
	// 빈 목록에서 끝 → 안내만
	h2 := nhSetup(t, "bob")
	h2.keys("w2\r")
	if n := h2.n(); n.step != nhPaste || !strings.Contains(n.msg, "1줄 이상") {
		t.Errorf("빈 목록: %q", n.msg)
	}
}

// 지정 언어: 덮어쓰기, 행 번호, 열 수정(disk → part), 하나라도 틀리면 아무것도 바꾸지 않음, 값 없는 행은 진행 불가, + 템플릿
func TestNewhostAssignLanguage(t *testing.T) {
	h := nhSetup(t, "alice")
	var lines []string
	for i := 1; i <= 25; i++ {
		lines = append(lines, "svr"+strings.Repeat("0", 3-len(strconv.Itoa(i)))+strconv.Itoa(i)+" 10.0.1."+strconv.Itoa(i)+" aa:bb:cc:dd:01:"+strings.Repeat("0", 2-len(strconv.Itoa(i)))+strconv.Itoa(i))
	}
	h.keys("w2" + strings.Join(lines, "\r") + "\r\r")
	n := h.n()
	if n.step != nhAssign || len(n.rows) != 25 {
		t.Fatalf("25대: step=%d rows=%d msg=%q", n.step, len(n.rows), n.msg)
	}
	h.keys("\r") // 값 없는 행 → 진행 불가
	if n.step != nhAssign || !strings.Contains(n.msg, "값 없는 행 25개 (행 1,2,3,4,5,6,7,8,9,10,…)") {
		t.Fatalf("값 없음: %q", n.msg)
	}
	h.keys("*=A svr021-023=mec 24-25=3 svr005=2\r")
	lbl := func(i int) string { return n.eff(n.rows[i-1]).Label }
	if lbl(1) != "1 [A]" || lbl(5) != "2 [mec]" || lbl(21) != "2 [mec]" || lbl(23) != "2 [mec]" || lbl(24) != "3" || lbl(20) != "1 [A]" {
		t.Errorf("덮어쓰기: %s %s %s %s %s", lbl(1), lbl(5), lbl(21), lbl(24), lbl(20))
	}
	h.keys("svr001-003 disk=nvme boot=legacy\r")
	if v := n.eff(n.rows[1]); v.T.Disk != "nvme" || v.T.Part != "nvme0n1p8" || v.T.Boot != "legacy" || v.Label != "1 [A]*" {
		t.Errorf("열 수정: %+v", v)
	}
	before := n.eff(n.rows[9]).T
	h.keys("svr010=3 svr011 disk=hdd\r") // 두 번째가 틀림 → svr010 도 그대로
	if n.eff(n.rows[9]).T != before || !n.msgErr || string(n.in) != "svr010=3 svr011 disk=hdd" {
		t.Errorf("원자성: %+v msg=%q in=%q", n.eff(n.rows[9]), n.msg, string(n.in))
	}
	n.in = nil
	for _, bad := range []string{"svr001=zz", "os=9.6", "zzz=1", "svr001", "svr001 part=sda5"} {
		h.keys(bad + "\r")
		if !n.msgErr {
			t.Errorf("%q: 오류여야 함", bad)
		}
		n.in = nil
	}
	h.keys("+ gpu bond nvme 900 8.10 legacy G\r")
	if !strings.Contains(n.msg, "템플릿 4 [G] 추가") || len(n.tpls) != 4 {
		t.Fatalf("+ 템플릿: %q", n.msg)
	}
	if b, err := os.ReadFile(filepath.Join(h.state, "newhost", "alice", "templates")); err != nil || string(b) != "G|gpu bond nvme 900 8.10 legacy\n" {
		t.Errorf("templates 파일: %q %v", b, err)
	}
	h.keys("svr025=g\r")
	if lbl(25) != "4 [G]" {
		t.Errorf("새 템플릿으로 지정: %s", lbl(25))
	}
	h.keys("+ x y sda 1 2 uefi a\r")
	if !n.msgErr || !strings.Contains(n.msg, "별칭 a 이 1 번과 겹침") || string(n.in) != "+ x y sda 1 2 uefi a" {
		t.Errorf("겹치는 별칭: %q", n.msg)
	}
	n.sc = flowScroll{} // 오류 행으로 스크롤됐을 수 있으므로 맨 위로
	h.st.draw()
	if !strings.Contains(h.frame(), "4 [G]    tmp tmp gpu") || !strings.Contains(h.frame(), "(gpu-bond-nvme-900-8.10-legacy) user") {
		t.Errorf("목록에 새 템플릿:\n%s", h.frame())
	}
	// 다시 열면 user 템플릿이 그대로 보임
	h.keys("\x18y")
	h.keys("w2")
	if len(h.n().tpls) != 4 {
		t.Errorf("다시 열었을 때 user 템플릿")
	}
}

// 지정 없는 행은 이력 추천(노랑 추천), 추천만으로도 진행 가능, 미리보기에 (추천 ← 근거)
func TestNewhostRecommendFlow(t *testing.T) {
	h := nhSetup(t, "alice")
	writeHist(t, filepath.Join(h.state, "newhost", "bob", "history", "1.txt"), time.Unix(1700000000, 0),
		"tmp tmp gpu svr0100 1.1.2.1 aa:bb:cc:dd:ef:01 bond nvme nvme0n1p8 900 8.10 uefi")
	h.keys("w2svr0001 10.0.0.1 aa:bb:cc:dd:ee:01\rnode01 10.0.0.2 aa:bb:cc:dd:ee:02\r\r")
	n := h.n()
	if v := n.eff(n.rows[0]); v.Src != nhSrcRec || v.From != "svr0100" || v.T.Size != "900" {
		t.Fatalf("추천: %+v", v)
	}
	f := h.frame()
	if !strings.Contains(f, "추천    gpu bond nvme 900 8.10 uefi  ← svr0100") || !strings.Contains(f, "(값 없음)") || !strings.Contains(f, "추천 1") {
		t.Errorf("추천 표시:\n%s", f)
	}
	h.keys("node01=mec\r\r")
	h.waitStep(nhPreview)
	if !strings.Contains(h.frame(), "(추천 ← svr0100)") {
		t.Errorf("미리보기 추천 표시:\n%s", h.frame())
	}
	// 명시 지정이 생기면 추천은 사라지고, 열 수정은 "수정"
	h.keys("\x1bsvr0001 os=9.6\r")
	if v := n.eff(n.rows[0]); v.Src != nhSrcEdit || v.Label != "수정" || v.T.Size != "900" || v.T.OS != "9.6" {
		t.Errorf("추천 행 열 수정: %+v", v)
	}
}

// Ctrl+X → y 취소(저장 안 함) / 다른 키 계속, Ctrl+C·Ctrl+Z 무시, 첫 단계 Esc
func TestNewhostCancel(t *testing.T) {
	h := nhSetup(t, "alice")
	h.keys("w2svr001 10.0.0.1 aa:bb:cc:dd:ee:01\r")
	h.keys("\x03\x1a")
	if h.st.nh == nil || !strings.Contains(h.n().msg, "취소는 Ctrl+X") {
		t.Fatalf("Ctrl+C·Ctrl+Z 무시")
	}
	h.keys("\x18")
	if !strings.Contains(h.frame(), "신규 설치 양식을 취소할까요? (저장하지 않음) [y/n]") {
		t.Fatalf("취소 질문:\n%s", h.frame())
	}
	h.keys("n")
	if h.st.nh == nil || len(h.n().rows) != 1 {
		t.Fatalf("n 이면 계속")
	}
	h.keys("\x1b") // 입력이 있으면 버릴지 묻는다
	if !strings.Contains(h.frame(), "입력한 1줄을 버리고 닫을까요? [y/n]") {
		t.Fatalf("Esc 질문:\n%s", h.frame())
	}
	h.keys("y")
	if h.st.nh != nil || h.st.sel.Msg != nhCancelMsg {
		t.Errorf("버리고 닫기: %q", h.st.sel.Msg)
	}
	h.keys("w2\x1b") // 비었으면 바로 닫힘
	if h.st.nh != nil {
		t.Errorf("빈 입력 Esc")
	}
	h.keys("w2svr001 10.0.0.1 aa:bb:cc:dd:ee:01\r\r*=A\r\r")
	h.waitStep(nhPreview)
	h.keys("\x18y")
	if h.st.nh != nil || h.st.sel.Msg != nhCancelMsg || h.st.nhSaved {
		t.Errorf("미리보기 취소")
	}
	if _, err := os.Stat(filepath.Join(h.awx, "alice.txt")); !os.IsNotExist(err) {
		t.Errorf("취소했는데 파일이 생김")
	}
}

// 시작 조건: 원격 클라이언트(os6_mgmt)는 안내만, awx_dir 없음은 안내만, -user 없으면 세션 user 를 한 번 고름
func TestNewhostStartConditions(t *testing.T) {
	h := nhSetup(t, "alice")
	os8_mgmt = "os8host"
	h.keys("w2")
	if h.st.nh != nil || h.st.sel.Msg != newhostRemoteMsg || !strings.Contains(h.frame(), newhostRemoteMsg) {
		t.Errorf("원격(os8_mgmt 설정): %q", h.st.sel.Msg)
	}
	os8_mgmt = ""
	h.st.src = &fakeSrc{snap: twoGroupSnap(), local: false}
	h.keys("w2")
	if h.st.nh != nil || h.st.sel.Msg != newhostRemoteMsg {
		t.Errorf("원격(로컬 아님): %q", h.st.sel.Msg)
	}
	h.st.src = &fakeSrc{snap: twoGroupSnap(), local: true}
	awx_dir = filepath.Join(h.awx, "없음")
	h.keys("w2")
	if h.st.nh != nil || h.st.sel.Msg != newhostNoDirMsg {
		t.Errorf("awx_dir 없음: %q", h.st.sel.Msg)
	}

	// 세션 user: 처음 한 번 고르고 기억
	h = nhSetup(t, "")
	useUserMenu(t)
	h.keys("w2")
	if h.st.oo == nil || h.st.nh != nil || !strings.Contains(h.frame(), "이 세션에서 사용할 user 선택") {
		t.Fatalf("세션 user 선택 화면:\n%s", h.frame())
	}
	h.keys("1\r")
	if n := h.n(); n.user != "alice" || sessionUser != "alice" || h.st.oo != nil || !strings.Contains(h.frame(), "[user: alice]") {
		t.Fatalf("세션 user: %q %q", n.user, sessionUser)
	}
	h.keys("\x1bw2")
	if h.st.oo != nil || h.n().user != "alice" {
		t.Errorf("두 번째는 묻지 않음")
	}
	h.keys("\x1b")
	sessionUser = ""
	h.keys("w2\x1b") // 선택 화면에서 취소 → 흐름 안 열림
	if h.st.oo != nil || h.st.nh != nil {
		t.Errorf("user 선택 취소")
	}
}

// CLI: 인자 검사, 원격·비 tty 거부 (테스트의 stdin 은 tty 가 아님)
func TestNewhostCLI(t *testing.T) {
	saveSessionState(t)
	old := os8_mgmt
	defer func() { os8_mgmt = old }()
	if rc := run([]string{"newhost", "extra"}); rc != 1 {
		t.Errorf("인자 초과 rc=%d", rc)
	}
	if rc := run([]string{"newhost", "-user", ".."}); rc != 1 {
		t.Errorf("잘못된 -user rc=%d", rc)
	}
	if rc := run([]string{"newhost", "-user", "carol"}); rc != 1 || forceUser != "carol" {
		t.Errorf("비 tty rc=%d forceUser=%q", rc, forceUser)
	}
	os8_mgmt = "os8host"
	if rc := cmdNewhost(); rc != 1 {
		t.Errorf("원격 rc=%d", rc)
	}
	if !strings.Contains(usageText, "newhost [-user <이름>]") {
		t.Errorf("usage 에 newhost 없음")
	}
}

// slowReader: 조각마다 잠깐 쉬며 돌려준다 (비동기 DNS 결과가 다음 키보다 먼저 오게)
type slowReader struct {
	parts []string
	gap   time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	if len(s.parts) == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.gap)
	n := copy(p, s.parts[0])
	s.parts = s.parts[1:]
	return n, nil
}

// oneShot 루프 (CLI newhost 와 같은 경로): 흐름이 닫히면(AWX 실행 뒤) 루프가 끝나고 저장 여부가 남는다
func TestNewhostOneShotLoop(t *testing.T) {
	h := nhSetup(t, "alice")
	newhostDNS = nhFakeDNS(map[string]string{"svr001": "10.0.0.11"}, nil, nil)
	env, rg := newRig(&slowReader{gap: 30 * time.Millisecond, parts: []string{
		"svr001 10.0.0.1 aa:bb:cc:dd:ee:01\r\r", "*=A\r", "\r", "a", "y", "y", "q"}})
	env.Tick = func(time.Duration) (<-chan time.Time, func()) { // 실제처럼 tick 이 와야 DNS 결과를 꺼낸다
		tk := time.NewTicker(10 * time.Millisecond)
		return tk.C, tk.Stop
	}
	var st *tuiState
	rc := runTUILoopWith(&fakeSrc{snap: twoGroupSnap(), local: true}, env, func(s *tuiState) {
		st = s
		s.oneShot = true
		s.openNewhost("alice")
	})
	if rc != 0 || st == nil || !st.nhSaved || len(h.envs) != 1 || st.sel.Msg != "AWX 종료 (rc=0)" {
		t.Fatalf("oneShot: rc=%d saved=%v envs=%d msg=%q\n%s", rc, st != nil && st.nhSaved, len(h.envs), st.sel.Msg, stripANSI(lastFrame(rg)))
	}
	if m := envMap(h.envs[0]); m["AWX_DNS_CHECKED"] != "1" || m["AWX_USER"] != "alice" {
		t.Errorf("env: %v", m)
	}
	if rg.raws != 2 || rg.restored != 2 { // 처음 + AWX 뒤 재진입
		t.Errorf("raw %d / restored %d", rg.raws, rg.restored)
	}
	if !strings.Contains(rg.out.String(), "신규 설치 양식 [user: alice] - 1/5 붙여넣기") {
		t.Errorf("첫 화면이 신규 설치 양식이어야 함")
	}
}

// 대량 붙여넣기: 1500줄을 한꺼번에 넣어도 빠르게 처리 (붙여넣기 단계는 쌓인 키를 모아 처리한 뒤 한 번 그림)
func TestNewhostBigPaste(t *testing.T) {
	nhSetup(t, "alice")
	var sb strings.Builder
	for i := 0; i < 1500; i++ {
		sb.WriteString("node" + strconv.Itoa(i) + "\t10." + strconv.Itoa(i/250) + "." + strconv.Itoa(i%250) + ".1, 02:00:00:00:" +
			strings.Repeat("0", 2-len(strconv.FormatInt(int64(i/256), 16))) + strconv.FormatInt(int64(i/256), 16) + ":" +
			strings.Repeat("0", 2-len(strconv.FormatInt(int64(i%256), 16))) + strconv.FormatInt(int64(i%256), 16) + "\n")
	}
	sb.WriteString("\n\x18y")
	env, rg := newRig(strings.NewReader(sb.String()))
	var n *newhostState
	t0 := time.Now()
	rc := runTUILoopWith(&fakeSrc{snap: twoGroupSnap(), local: true}, env, func(s *tuiState) {
		s.oneShot = true
		s.openNewhost("alice")
		n = s.nh
	})
	el := time.Since(t0)
	if rc != 0 || len(n.rows) != 1500 || len(n.badRows()) != 0 || n.step != nhAssign {
		t.Fatalf("rc=%d rows=%d bad=%d step=%d msg=%q", rc, len(n.rows), len(n.badRows()), n.step, n.msg)
	}
	frames := strings.Count(rg.out.String(), "\x1b[H")
	t.Logf("1500줄 붙여넣기: %v, 그린 횟수 %d", el, frames)
	if el > 15*time.Second {
		t.Errorf("너무 느림: %v", el)
	}
}

// 화면 틀: 본문이 넘치면 위·아래 표시, ↑↓ PgUp/PgDn, 끝까지 내리면 따라가기
func TestRenderFlowScroll(t *testing.T) {
	var body []string
	for i := 1; i <= 50; i++ {
		body = append(body, " 줄 "+strconv.Itoa(i))
	}
	f := flowFrame{Title: "제목", Head: []string{" 머리"}, Body: body, Foot: []string{" 입력 > _"}, Msg: "안내", Hint: " Esc 닫기"}
	sc := &flowScroll{}
	out := strings.Split(stripANSI(renderFlow(f, sc, 60, 20, true)), "\n")
	if len(out) != 20 || out[3] != " 줄 1" || !strings.Contains(out[15], "(아래 38줄 — ↓ PgDn)") || out[16] != " 입력 > _" {
		t.Fatalf("처음:\n%s", strings.Join(out, "\n"))
	}
	if sc.Max != 37 || sc.Page != 11 {
		t.Errorf("Max=%d Page=%d", sc.Max, sc.Page)
	}
	sc.key(keyEv{k: kPgDn})
	out = strings.Split(stripANSI(renderFlow(f, sc, 60, 20, false)), "\n")
	if !strings.Contains(out[3], "(위 12줄 — ↑ PgUp)") || out[4] != " 줄 13" {
		t.Errorf("PgDn:\n%s", strings.Join(out, "\n"))
	}
	for i := 0; i < 5; i++ {
		sc.key(keyEv{k: kPgDn})
	}
	if !sc.Follow || sc.Top != 37 {
		t.Errorf("끝까지 → 따라가기: %+v", sc)
	}
	sc.key(keyEv{k: kUp})
	if sc.Follow || sc.Top != 36 {
		t.Errorf("↑: %+v", sc)
	}
	if sc.key(keyEv{k: kRune, r: 'a'}) {
		t.Errorf("다른 키는 처리 안 함")
	}
	if out := renderFlow(f, &flowScroll{}, 60, 0, false); strings.Count(out, "\n") != 50+6 {
		t.Errorf("높이 제한 없음: %d줄", strings.Count(out, "\n")+1)
	}
}

// ---- 골든 (폭 60/80/120 × plain/color): 붙여넣기(오류)·지정(추천·값 없음)·DNS 선택·미리보기·저장 ----

func newhostGoldenState(t *testing.T) *newhostState {
	setNewhostTemplates(t, "A|sec bond sda 500 8.10 uefi", "mec|mec eth0 sda 500 9.6 uefi", "mec eth0 nvme 400 9.6 uefi")
	n := newNewhostState("alice", t.TempDir(), "/awx", func() time.Time { return time.Unix(1760245506, 0) })
	for _, l := range []string{"svr001 10.0.0.1 aa:bb:cc:dd:ee:01", "svr002 10.0.0.2 aa:bb:cc:dd:ee:02", "spice-a01 10.0.0.3 aabb.ccdd.ee03",
		"gpu0001 10.0.0.4 aa-bb-cc-dd-ee-04", "svr002 10.0.0.5"} {
		n.rows = append(n.rows, nhParseLine(l))
	}
	nhCheckDups(n.rows)
	return n
}

func TestNewhostGolden(t *testing.T) {
	t.Setenv("AUTO_SETUP_DIR", t.TempDir()) // applyDNS 의 auto_setup.log
	for _, w := range []int{60, 80, 120} {
		for _, color := range []bool{false, true} {
			mode := "plain"
			if color {
				mode = "color"
			}
			var sb strings.Builder
			add := func(name, out string) { sb.WriteString("== " + name + " ==\n" + out + "\n") }
			n := newhostGoldenState(t)
			add("paste", renderNewhost(n, time.Time{}, w, 24, color))
			n.rows = n.rows[:4]
			nhCheckDups(n.rows)
			n.step, n.sc = nhAssign, flowScroll{}
			n.hist = &nhHistIndex{byHost: map[string]nhHist{}, byPfx: map[string]nhHist{}}
			rt, _ := parseNewhostTpl("gpu bond nvme 900 8.10 uefi")
			n.hist.byPfx["gpu"] = nhHist{Host: "gpu0100", T: rt}
			n.applyAssign("svr001-002=1 spice*=mec svr002 os=9.6")
			n.recompute()
			n.in = []rune("svr0")
			n.setMsg("3행 지정했습니다")
			add("assign", renderNewhost(n, time.Time{}, w, 40, color))
			n.rep = nhFakeDNS(map[string]string{"svr001": "10.0.0.11"}, map[string]DNSStatus{"spice-a01": DNSUnregistered, "gpu0001": DNSAmbiguous},
				map[string][]string{"svr001": {"등록IP 10.0.0.1 은 svr009 의 IP (DNS)"}})(n.entries())
			n.dnsDone, n.step, n.sc = true, nhDNSAsk, flowScroll{}
			n.setMsg("")
			add("dnsask", renderNewhost(n, time.Time{}, w, 24, color))
			n.step = nhPreview
			n.applyDNS([]string{"svr001"})
			add("preview", renderNewhost(n, time.Time{}, w, 30, color))
			n.step, n.sc = nhSaved, flowScroll{}
			n.saved = nhSaveResult{Path: "/awx/alice.txt", Backup: "/tmp/auto_setup/newhost/alice/backup/20261012-140506.txt",
				History: "/tmp/auto_setup/newhost/alice/history/20261012-140506.txt"}
			n.savedLines = n.lines()
			n.setMsg("")
			add("saved", renderNewhost(n, time.Time{}, w, 24, color))
			n.ask = nhAskCancel
			n.step = nhPreview
			add("cancel-ask", renderNewhost(n, time.Time{}, w, 24, color))
			got := goldenText(sb.String())
			name := "tui_newhost_" + mode + "_" + strconv.Itoa(w) + ".golden"
			path := filepath.Join("testdata", name)
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got), 0644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("골든 없음 %s (go test -run NewhostGolden -update): %v", name, err)
			}
			if string(want) != got {
				t.Errorf("%s 불일치\n--- got ---\n%s", name, got)
			}
			if !color && w == 60 { // 60칸: 모든 줄이 폭 안 (줄바꿈 규칙)
				for _, l := range strings.Split(got, "\n") {
					if strings.HasPrefix(l, "== ") {
						continue
					}
					if strWidth(l) > 60 {
						t.Errorf("60칸 넘는 줄: %q", l)
					}
				}
			}
		}
	}
}

// 붙여넣기 네 번째 값(템플릿): 처음부터 목록이 보이고, 끝내면 바뀔 양식 예시 + y/n, y 면 지정되어 템플릿 지정 단계로, n 이면 붙여넣기로 돌아감.
// 템플릿 없는 줄은 종전처럼 다음 단계에서 지정, 없는 템플릿은 빨강 오류.
func TestNewhostPasteTemplateColumn(t *testing.T) {
	h := nhSetup(t, "alice")
	h.keys("w2")
	if f := h.frame(); !strings.Contains(f, "1 [A]") || !strings.Contains(f, "hostname ip mac [템플릿]") {
		t.Fatalf("붙여넣기 단계에 템플릿 목록·안내가 없음:\n%s", f)
	}
	h.keys("svr001 10.0.0.1 aa:bb:cc:dd:ee:01 A\rsvr002 10.0.0.2 aa:bb:cc:dd:ee:02 mec\rsvr003 10.0.0.3 aa:bb:cc:dd:ee:03\rsvr004 10.0.0.4 aa:bb:cc:dd:ee:04 zzz\r\r")
	n := h.n()
	if n.step != nhPaste || !strings.Contains(n.rows[3].Err, `템플릿 "zzz" 없음`) {
		t.Fatalf("없는 템플릿은 오류여야 함: step=%d %+v", n.step, n.rows[3])
	}
	h.keys("d 4\r\r")
	if n.step != nhConfirm {
		t.Fatalf("템플릿 있는 줄이 있으면 확인 단계: step=%d msg=%q", n.step, n.msg)
	}
	f := h.frame()
	for _, s := range []string{"1/5 양식 확인", "■ 1 [A] (sec-bond-sda-500-8.10-uefi) — 1대", "■ 2 [mec] (mec-eth0-sda-500-9.6-uefi) — 1대",
		"템플릿 없는 줄 1대", "입력   : svr001 10.0.0.1 aa:bb:cc:dd:ee:01 A",
		"저장될 줄: tmp tmp sec svr001 10.0.0.1 aa:bb:cc:dd:ee:01 bond sda sda5 500 8.10 uefi", "이렇게 2대의 템플릿을 지정할까요? [y/n]"} {
		if !strings.Contains(f, s) {
			t.Errorf("확인 화면에 %q 없음:\n%s", s, f)
		}
	}
	if len(n.assign) != 0 {
		t.Errorf("y 전에는 지정되면 안 됨: %v", n.assign)
	}
	h.keys("n")
	if n.step != nhPaste || len(n.rows) != 3 || len(n.assign) != 0 {
		t.Fatalf("n → 붙여넣기로 (줄 유지): step=%d rows=%d", n.step, len(n.rows))
	}
	h.keys("\ry")
	if n.step != nhAssign || n.eff(n.rows[0]).Label != "1 [A]" || n.eff(n.rows[1]).Label != "2 [mec]" || n.eff(n.rows[2]).Src != nhSrcNone {
		t.Fatalf("y → 지정 후 템플릿 지정 단계: step=%d %+v %+v %+v", n.step, n.eff(n.rows[0]), n.eff(n.rows[1]), n.eff(n.rows[2]))
	}
	if !strings.Contains(n.msg, "2행에 템플릿을 지정했습니다") {
		t.Errorf("msg=%q", n.msg)
	}
	h.keys("svr003=3\r\r")
	h.waitStep(nhPreview)
	h.keys("y")
	want := "tmp tmp sec svr001 10.0.0.1 aa:bb:cc:dd:ee:01 bond sda sda5 500 8.10 uefi\n" +
		"tmp tmp mec svr002 10.0.0.2 aa:bb:cc:dd:ee:02 eth0 sda sda5 500 9.6 uefi\n" +
		"tmp tmp mec svr003 10.0.0.3 aa:bb:cc:dd:ee:03 eth0 nvme nvme0n1p8 400 9.6 uefi\n"
	if b, _ := os.ReadFile(filepath.Join(h.awx, "alice.txt")); string(b) != want {
		t.Errorf("alice.txt:\n%s", b)
	}
}

// 템플릿 값이 하나도 없으면 확인 단계 없이 종전처럼 바로 템플릿 지정 단계
func TestNewhostPasteNoTemplateColumn(t *testing.T) {
	h := nhSetup(t, "alice")
	h.keys("w2")
	h.keys("svr001 10.0.0.1 aa:bb:cc:dd:ee:01\r\r")
	if h.n().step != nhAssign {
		t.Fatalf("step=%d", h.n().step)
	}
}
