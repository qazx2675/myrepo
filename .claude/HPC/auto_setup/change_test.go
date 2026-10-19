package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- 메뉴 가시성 ----

func TestChangeMenuItemsVisibility(t *testing.T) {
	saveChangeState(t)
	keys := func() string {
		var s string
		for _, it := range changeMenuItems() {
			s += string(it.Key)
		}
		return s
	}
	cases := []struct {
		admin    bool
		ip, ldap string
		want     string
	}{
		{false, "", "", ""},
		{false, "y", "", "14"},
		{false, "", "yes", "245"},
		{false, "1", "true", "12345"},
		{false, "no", "0", ""},
		{true, "", "", "12345"},
	}
	for _, c := range cases {
		adminMode, enable_ip_change, enable_ldap_change = c.admin, c.ip, c.ldap
		if got := keys(); got != c.want {
			t.Errorf("admin=%v ip=%q ldap=%q: %q 기대 %q", c.admin, c.ip, c.ldap, got, c.want)
		}
	}
	adminMode = true
	for _, it := range changeMenuItems() {
		if want := map[rune]changeKind{'1': kindIP, '2': kindLDAP, '3': kindBoth, '4': kindRollback, '5': kindApplRestore}[it.Key]; it.Kind != want {
			t.Errorf("%c → %v", it.Key, it.Kind)
		}
	}
}

func TestChangeRollbackSubMenu(t *testing.T) {
	r := newChgRig(t)
	r.env.IPOn = false
	c := newChangeState(r.env, kindRollback)
	if c.step != csRbSel || len(c.rbOpts) != 1 || c.rbOpts[0].key != '2' {
		t.Fatalf("LDAP 만 켜짐: %+v", c.rbOpts)
	}
	chgKeys(c, "1") // 없는 항목 무시
	if c.step != csRbSel {
		t.Error("꺼진 항목 선택됨")
	}
	chgKeys(c, "2")
	if c.step != csUser || c.rbIP || !c.rbLDAP {
		t.Errorf("선택 후: step=%d ip=%v ldap=%v", c.step, c.rbIP, c.rbLDAP)
	}
	r.env.IPOn, r.env.LDAPOn = false, false
	if c := newChangeState(r.env, kindRollback); c.step != csFatal {
		t.Error("둘 다 꺼지면 시작 불가")
	}
}

func TestChangeMissingEngineFatal(t *testing.T) {
	r := newChgRig(t)
	r.env.LDAPEngine = ""
	c := newChangeState(r.env, kindBoth)
	if c.step != csFatal || !strings.Contains(c.fatal, ldapEngineName) {
		t.Fatalf("엔진 없음: step=%d %q", c.step, c.fatal)
	}
	if !strings.Contains(renderChange(c, 80, 0, false), ldapEngineName) {
		t.Error("화면에 안내 없음")
	}
	if !chgKeys(c, "\x1b") {
		t.Error("Esc 로 닫힘")
	}
	if c := newChangeState(r.env, kindIP); c.step != csUser {
		t.Error("IP 만이면 LDAP 엔진 없어도 시작")
	}
}

// ---- 입력 정규화 ----

func TestChangeParseIPInput(t *testing.T) {
	raw := "h1 10.1.1.5\r\nh2\t10.1.1.6\nh3,10.1.1.7\nh4=10.1.1.8\nh5->10.1.1.9\nh6 -> 10.1.2.10\n# 주석\n\n" +
		"h7 300.1.1.1\nh8 10.1.1\nh9 01.1.1.1\nh10 ::1\nonly\nh11 10.1.1.11 extra\nbad!host 10.1.1.12\n" +
		"h1 10.1.3.5\nh12 10.1.1.6\n"
	es, warns := parseIPInput(raw)
	got := map[string]string{}
	var order []string
	for _, e := range es {
		got[e.Host] = e.IP
		order = append(order, e.Host)
		if e.GW != ipGateway(e.IP) {
			t.Errorf("%s GW %s", e.Host, e.GW)
		}
	}
	if strings.Join(order, ",") != "h1,h2,h3,h4,h5,h6,h12" {
		t.Errorf("순서·대상: %v", order)
	}
	if got["h1"] != "10.1.3.5" || got["h5"] != "10.1.1.9" || got["h6"] != "10.1.2.10" {
		t.Errorf("값: %v", got)
	}
	all := strings.Join(warns, "\n")
	for _, w := range []string{"9번째 줄: IPv4 가 아님: 300.1.1.1", "IPv4 가 아님: 10.1.1", "IPv4 가 아님: 01.1.1.1", "IPv4 가 아님: ::1",
		"형식 오류 (hostname ip): only", "형식 오류 (hostname ip): h11", "hostname 이 올바르지 않음: bad!host",
		"h1 중복 — 마지막 값 10.1.3.5 사용", "새 IP 10.1.1.6 가 h2 와 h12 에 겹침"} {
		if !strings.Contains(all, w) {
			t.Errorf("경고 %q 없음:\n%s", w, all)
		}
	}
	if es[0].GW != "10.1.3.1" {
		t.Errorf("GW: %+v", es[0])
	}
}

func TestChangeParseHostInput(t *testing.T) {
	hs, warns := parseHostInput("h1 h2,h3\th1\nbad;x\n|h4|\n")
	if strings.Join(hs, ",") != "h1,h2,h3,h4" || len(warns) != 1 {
		t.Errorf("%v %v", hs, warns)
	}
}

// ---- 마스킹 ----

func TestChangeSecretMask(t *testing.T) {
	r := newChgRig(t)
	m := loadSecretMask(filepath.Join(r.env.ConfDir, "ldap_config.conf"))
	in := "x " + chgSecret + " y " + chgSecret2 + "\nBINDPW " + chgNodeSecret + "\nldap_default_authtok = abc123\n" +
		`secret="xyz789"` + "\nbindpw=qq1\npassword: pp2\nDIFF|-bindpw zz3\n"
	out := m.apply(in)
	for _, s := range []string{chgSecret, chgSecret2, chgNodeSecret, "abc123", "xyz789", "qq1", "pp2", "zz3"} {
		if strings.Contains(out, s) {
			t.Errorf("%q 가 남음:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "BINDPW ****") || !strings.Contains(out, "ldap_default_authtok = ****") {
		t.Errorf("키 패턴 마스킹:\n%s", out)
	}
	if (&secretMask{}).apply("10.1.1.5 OK") != "10.1.1.5 OK" {
		t.Error("일반 문자열은 그대로")
	}
	if m := loadSecretMask(filepath.Join(r.dir, "없음")); m.apply("bindpw a") != "bindpw ****" {
		t.Error("conf 없어도 키 패턴은 가림")
	}
}

// ---- user · infra · 입력 단계 ----

func TestChangeUserValidation(t *testing.T) {
	r := newChgRig(t)
	for _, bad := range []string{"..", ".", "a b", "../x", "x/y", "한글"} {
		c := newChangeState(r.env, kindIP)
		chgKeys(c, bad+"\n")
		if c.step != csUser || !strings.Contains(c.msg, "user 이름이 올바르지 않습니다") {
			t.Errorf("%q: step=%d msg=%q", bad, c.step, c.msg)
		}
	}
	if _, err := changeUserDir(r.dir, ".."); err == nil {
		t.Error("changeUserDir(..) 거부")
	}
	// user 메뉴: 번호 → info.sh 조회
	r.env.User = &oneoffState{menu: []string{"1) alice", "2) bob"}, route: "/r"}
	r.env.UserLookup = func(route, sel string) (string, error) {
		if route == "/r" && sel == "2" {
			return "bob\n", nil
		}
		return "", errors.New("x")
	}
	c := newChangeState(r.env, kindIP)
	chgKeys(c, "9\n")
	if c.step != csUser || !strings.Contains(c.msg, "조회 실패") {
		t.Errorf("잘못된 번호: %d %q", c.step, c.msg)
	}
	chgKeys(c, "2\n")
	if c.user != "bob" || c.step != csInput {
		t.Errorf("메뉴 선택: user=%q step=%d", c.user, c.step)
	}
	if !strings.Contains(renderChange(newChangeState(r.env, kindIP), 80, 0, false), "2) bob") {
		t.Error("user 메뉴 표시")
	}
}

func TestChangeInfraMenu(t *testing.T) {
	r := newChgRig(t)
	c := newChangeState(r.env, kindLDAP)
	chgKeys(c, "u1\n")
	if c.step != csInfra || strings.Join(c.infras, ",") != "alpha,beta" {
		t.Fatalf("infra 목록: %d %v", c.step, c.infras)
	}
	out := renderChange(c, 80, 0, false)
	if !strings.Contains(out, "1 alpha") || !strings.Contains(out, "2 beta") || strings.Contains(out, chgSecret) {
		t.Errorf("infra 화면:\n%s", out)
	}
	chgKeys(c, "3\n")
	if c.step != csInfra || c.msg == "" {
		t.Error("범위 밖 번호")
	}
	chgKeys(c, "2\n")
	if c.infra != "beta" || c.step != csInput {
		t.Errorf("선택: %q %d", c.infra, c.step)
	}
	// conf 에 infra 가 없으면 안내 후 Esc
	os.WriteFile(filepath.Join(r.env.ConfDir, "ldap_config.conf"), []byte("s4.enabled=false\n"), 0600)
	c = newChangeState(r.env, kindLDAP)
	chgKeys(c, "u1\n")
	if c.step != csInfra || !strings.Contains(c.msg, "infra.<이름>") {
		t.Errorf("infra 없음 안내: %q", c.msg)
	}
	if !chgKeys(c, "\x1b") {
		t.Error("Esc 로 닫힘")
	}
}

func TestChangeInputCancelAndEmpty(t *testing.T) {
	r := newChgRig(t)
	c := newChangeState(r.env, kindIP)
	chgKeys(c, "u1\n")
	chgKeys(c, "\x04")
	if c.step != csInput || !strings.Contains(c.msg, "유효한 대상이 없습니다") {
		t.Errorf("빈 입력: %q", c.msg)
	}
	chgKeys(c, "h1 10.1.1.5")
	if chgKeys(c, "q") {
		t.Error("입력이 있으면 q 는 글자")
	}
	if !chgKeys(c, "\x1b") {
		t.Error("Esc 는 취소")
	}
	c = newChangeState(r.env, kindIP)
	chgKeys(c, "u1\n")
	if !chgKeys(c, "q") {
		t.Error("빈 입력에서 q 는 취소")
	}
	if len(r.w.calls) != 0 {
		t.Error("실행 없음")
	}
}

// ---- 미리보기 · yes ----

func TestChangeYesOnly(t *testing.T) {
	for _, in := range []string{"no\n", "y\n", "YES\n", "yes \n", " yes\n", "\n", "\x1b"} {
		r := newChgRig(t)
		c := newChangeState(r.env, kindIP)
		chgKeys(c, "u1\nh1 10.1.1.5\x04")
		if c.step != csPreview {
			t.Fatalf("미리보기: %d", c.step)
		}
		if !chgKeys(c, in) {
			t.Errorf("%q: 닫혀야 함", in)
		}
		if c.CloseMsg == "" || len(r.w.calls) != 0 {
			t.Errorf("%q: 취소 안내 %q, 호출 %d", in, c.CloseMsg, len(r.w.calls))
		}
		if _, err := os.Stat(filepath.Join(r.env.StateDir, "changes")); !os.IsNotExist(err) {
			t.Errorf("%q: 기록이 생기면 안 됨", in)
		}
	}
}

func TestChangePreviewContent(t *testing.T) {
	r := newChgRig(t)
	r.env.Tracked["h2"] = "J1/infra_a.yml"
	c := newChangeState(r.env, kindIP)
	chgKeys(c, "u1\nh1 10.1.1.5\nh2 10.1.2.6\nzz 1.2.3\x04")
	out := renderChange(c, 100, 0, true)
	plain := stripANSI(out)
	for _, w := range []string{"h1", "→ 10.1.1.5 (GW 10.1.1.1)", "현재 IP 는 실행 때 노드에서 확인", "서비스 재시작 안 함",
		"재부팅/네트워크 재시작 후 반영", "1대가 진행 중 job 에 있음 — 변경 IP 반영 후 추적이 끊길 수 있음", "h2(J1/infra_a.yml)",
		"IPv4 가 아님: 1.2.3", "yes 를 입력하면 2대에 실제로 적용합니다"} {
		if !strings.Contains(plain, w) {
			t.Errorf("미리보기에 %q 없음:\n%s", w, plain)
		}
	}
	if !strings.Contains(out, "\x1b[1;31m 1대가 진행 중 job") {
		t.Errorf("job 경고는 빨강:\n%q", out)
	}
	// 스크롤: 높이가 작으면 ↓ 로 내려가고 끝에서 멈춤
	small := renderChange(c, 100, 12, false)
	if n := len(strings.Split(small, "\n")); n != 12 {
		t.Errorf("높이 12: %d 줄", n)
	}
	for i := 0; i < 50; i++ {
		c.handleKey(keyEv{k: kDown})
		renderChange(c, 100, 12, false)
	}
	if c.scroll != c.maxScroll || c.maxScroll == 0 {
		t.Errorf("스크롤 끝: %d / %d", c.scroll, c.maxScroll)
	}
}

func TestChangeLDAPDryRunPreview(t *testing.T) {
	r := newChgRig(t)
	r.w.ldap["h2"], r.w.ldap["h3"] = "NOCHANGE", "FAIL"
	r.w.os8Down["h4"] = true
	r.w.ldap["h5"] = "SKIP"
	c := newChangeState(r.env, kindLDAP)
	chgKeys(c, "u1\n1\nh1 h2 h3 h4 h5\x04")
	if c.step != csDry {
		t.Fatalf("dry-run 단계: %d", c.step)
	}
	chgWait(t, c)
	if c.step != csPreview {
		t.Fatalf("미리보기: %d %q", c.step, c.msg)
	}
	dry := r.w.callsOf("ldapDry")
	if len(dry) != 2 || dry[1].via != "os6" || strings.Join(dry[1].hosts, ",") != "h4" {
		t.Fatalf("dry-run 경유: %+v", dry)
	}
	plain := stripANSI(renderChange(c, 100, 0, false))
	for _, w := range []string{"CHANGED(바뀔 예정)", "NOCHANGE", "FAIL", "건너뜀(자산현황 없음)", "도메인 섹션이 없습니다",
		"LDAP dry-run: CHANGED 2 · NOCHANGE 1 · FAIL 1 · 건너뜀(자산현황 없음) 1", "auto.appl 은 /etc/auto.appl.bak 에 대기"} {
		if !strings.Contains(plain, w) {
			t.Errorf("%q 없음:\n%s", w, plain)
		}
	}
	// dry-run 에서 os6 로 응답한 h4 는 실제 적용 때 처음부터 os6
	chgKeys(c, "yes\n")
	chgWait(t, c)
	ap := r.w.callsOf("ldap")
	if len(ap) != 2 || strings.Contains(strings.Join(ap[0].hosts, ","), "h4") || ap[1].via != "os6" || strings.Join(ap[1].hosts, ",") != "h4" {
		t.Errorf("적용 경로: %+v", ap)
	}
}

func TestChangeDryRunCancelBackToInput(t *testing.T) {
	r := newChgRig(t)
	r.w.block = true
	c := newChangeState(r.env, kindLDAP)
	chgKeys(c, "u1\n1\nh1\x04")
	if c.step != csDry || !c.running() {
		t.Fatal("dry-run 중이어야 함")
	}
	chgKeys(c, "\x1bq")
	if c.step != csDry || !strings.Contains(c.msg, "Ctrl+X") {
		t.Errorf("실행 중 q/Esc 무시: %q", c.msg)
	}
	chgKeys(c, "\x18y")
	chgWait(t, c)
	if c.step != csInput || !strings.Contains(c.msg, "취소") {
		t.Errorf("dry-run 취소 → 입력으로: %d %q", c.step, c.msg)
	}
}

// ---- 취소 ----

func TestChangeCancelDuringRun(t *testing.T) {
	r := newChgRig(t)
	r.w.block = true
	c := newChangeState(r.env, kindIP)
	chgKeys(c, "u1\nh1 10.1.1.5\nh2 10.1.1.6\x04yes\n")
	if c.step != csRun {
		t.Fatalf("실행 중: %d", c.step)
	}
	waitFor(t, "첫 호출", func() bool { r.w.mu.Lock(); defer r.w.mu.Unlock(); return len(r.w.calls) == 1 })
	if chgKeys(c, "\x1bqQ") || !strings.Contains(c.msg, "Ctrl+X") {
		t.Errorf("실행 중 q/Esc 는 무시: %q", c.msg)
	}
	chgKeys(c, "\x18n")
	if c.cancelAsk || c.job.wasCancelled() {
		t.Error("n 이면 계속")
	}
	chgKeys(c, "\x18")
	if !c.cancelAsk || !strings.Contains(stripANSI(renderChange(c, 100, 0, false)), "[y/n]") {
		t.Error("취소 확인 질문")
	}
	chgKeys(c, "y")
	chgWait(t, c)
	if c.step != csResult || !c.cancelled {
		t.Fatalf("취소 결과: step=%d cancelled=%v", c.step, c.cancelled)
	}
	for _, h := range c.results {
		if h.IPResult != rsCancel {
			t.Errorf("%s: %+v", h.Host, h)
		}
	}
	if len(r.w.calls) != 1 {
		t.Errorf("취소 뒤 os6 재시도 없음: %d", len(r.w.calls))
	}
	plain := stripANSI(renderChange(c, 100, 0, false))
	if !strings.Contains(plain, "일부 호스트만 변경됐을 수 있음") || !strings.Contains(plain, "미확인(취소)") {
		t.Errorf("취소 안내:\n%s", plain)
	}
	rec := readChgRecord(t, c.recPath)
	if !rec.Cancelled || rec.End == "" || len(rec.Hosts) != 2 {
		t.Errorf("기록: %+v", rec)
	}
	if !strings.Contains(r.logs[0], "(취소됨)") {
		t.Errorf("로그: %v", r.logs)
	}
}

func TestChangeShutdownCancels(t *testing.T) {
	r := newChgRig(t)
	r.w.block = true
	c := newChangeState(r.env, kindIP)
	chgKeys(c, "u1\nh1 10.1.1.5\x04yes\n")
	waitFor(t, "호출", func() bool { r.w.mu.Lock(); defer r.w.mu.Unlock(); return len(r.w.calls) == 1 })
	c.shutdown()
	if c.running() || !c.cancelled || !readChgRecord(t, c.recPath).Cancelled {
		t.Error("종료 시 취소·기록")
	}
}

// ---- bindpw 는 어디에도 없다 ----

func TestChangeSecretNowhere(t *testing.T) {
	r := newChgRig(t)
	r.w.ldap["h2"] = "FAIL"
	r.w.ipFail["h3"] = "ifcfg 없음 bindpw=" + chgSecret
	r.w.os8Down["h4"] = true
	r.w.gate = make(chan struct{})
	var screens []string
	snap := func(c *changeState) {
		for _, w := range []int{60, 100} {
			screens = append(screens, renderChange(c, w, 0, true), renderChange(c, w, 20, false))
		}
	}
	c := newChangeState(r.env, kindBoth)
	snap(c)
	chgKeys(c, "u1\n")
	snap(c)
	chgKeys(c, "1\n")
	snap(c)
	chgKeys(c, "h1 10.1.1.5\nh2 10.1.1.6\nh3 10.1.1.7\nh4 10.1.1.8\x04")
	waitFor(t, "dry-run 출력", func() bool { _, tail := c.job.progress(); return len(tail) > 3 })
	snap(c)
	close(r.w.gate)
	chgWait(t, c)
	snap(c)
	r.w.gate = make(chan struct{})
	chgKeys(c, "yes\n")
	waitFor(t, "실행 출력", func() bool { _, tail := c.job.progress(); return len(tail) > 3 })
	snap(c)
	if _, tail := c.job.progress(); !strings.Contains(strings.Join(tail, "\n"), "****") {
		t.Errorf("실행 꼬리에 마스킹 흔적 없음: %v", tail)
	}
	close(r.w.gate)
	chgWait(t, c)
	snap(c)
	for i := 0; i < 30; i++ {
		c.handleKey(keyEv{k: kDown})
		snap(c)
	}
	var hay []string
	hay = append(hay, screens...)
	hay = append(hay, r.logs...)
	hay = append(hay, c.msg, c.CloseMsg)
	filepath.Walk(r.env.StateDir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			b, _ := os.ReadFile(p)
			hay = append(hay, p+"\n"+string(b))
		}
		return nil
	})
	for _, cl := range r.w.calls {
		hay = append(hay, cl.name, strings.Join(cl.args, " "), strings.Join(cl.env, " "))
		for p, b := range cl.files {
			hay = append(hay, p, b)
		}
	}
	if len(r.w.callsOf("ldapDry")) == 0 || len(r.w.callsOf("ldap")) == 0 || len(r.w.callsOf("ip")) < 2 {
		t.Fatalf("모든 경로가 실행되어야 함: %d calls", len(r.w.calls))
	}
	for _, s := range []string{chgSecret, chgSecret2, chgNodeSecret} {
		for _, h := range hay {
			if strings.Contains(h, s) {
				t.Errorf("비밀값 %q 노출:\n%s", s, h)
			}
		}
	}
}
