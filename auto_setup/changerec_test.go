package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func readChgRecord(t *testing.T, p string) *changeRecord {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("기록 읽기: %v", err)
	}
	rec := &changeRecord{}
	if err := json.Unmarshal(b, rec); err != nil {
		t.Fatalf("기록 해석: %v", err)
	}
	return rec
}

func TestChangeRecordSchemaAndPerms(t *testing.T) {
	r := newChgRig(t)
	c := chgRun(t, r, kindBoth, "user1", "1", "h1 10.1.1.5\nh2 10.1.1.6\n")
	p := c.recordPath()
	dir := filepath.Join(r.env.StateDir, "changes", "user1")
	if filepath.Dir(p) != dir {
		t.Fatalf("기록 위치: %s", p)
	}
	if !changeRecRe.MatchString(filepath.Base(p)) || !strings.HasSuffix(p, "_both.json") || !strings.HasPrefix(filepath.Base(p), "20261012093") {
		t.Errorf("파일 이름: %s", filepath.Base(p))
	}
	if runtime.GOOS != "windows" {
		for _, d := range []string{filepath.Join(r.env.StateDir, "changes"), dir} {
			if fi, err := os.Stat(d); err != nil || fi.Mode().Perm() != 0700 {
				t.Errorf("%s 퍼미션: %v %v", d, fi.Mode(), err)
			}
		}
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0600 {
			t.Errorf("파일 퍼미션: %v", fi.Mode())
		}
	}
	var raw map[string]interface{}
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"user", "kind", "infra", "start", "end", "cancelled", "hosts"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("키 %s 없음", k)
		}
	}
	h0 := raw["hosts"].([]interface{})[0].(map[string]interface{})
	for _, k := range []string{"host", "ip_result", "ldap_result", "old_ip", "new_ip", "gw", "site", "route", "appl_pending", "ip_rolled_back", "ldap_rolled_back"} {
		if _, ok := h0[k]; !ok {
			t.Errorf("hosts[] 키 %s 없음: %v", k, h0)
		}
	}
	rec := readChgRecord(t, p)
	if rec.User != "user1" || rec.Kind != "both" || rec.Infra != "alpha" || rec.Cancelled || rec.End == "" {
		t.Errorf("기록: %+v", rec)
	}
	if h := rec.Hosts[0]; h.Host != "h1" || h.IPResult != rsOK || h.LDAPResult != rsOK || h.NewIP != "10.1.1.5" || h.Route != "os8" || !h.ApplPending || h.Site != "a1" {
		t.Errorf("호스트: %+v", h)
	}
	// 로그 한 줄: 종류·user·대수·OK/FAIL 만 (호스트 이름·IP 없음)
	if len(r.logs) != 1 || r.logs[0] != "변경 메뉴 both: user user1 대상 2대 OK 2 FAIL 0" {
		t.Errorf("로그: %q", r.logs)
	}
	for _, s := range []string{"h1", "10.1.1"} {
		if strings.Contains(r.logs[0], s) {
			t.Errorf("로그에 %q", s)
		}
	}
	// 같은 초·종류 → _2
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	p1, err1 := createChangeRecord(r.env.StateDir, &changeRecord{User: "u2", Kind: "ip"}, at)
	p2, err2 := createChangeRecord(r.env.StateDir, &changeRecord{User: "u2", Kind: "ip"}, at)
	if err1 != nil || err2 != nil || filepath.Base(p1) != "20260102030405_ip.json" || filepath.Base(p2) != "20260102030405_ip_2.json" {
		t.Errorf("충돌 이름: %s %s %v %v", p1, p2, err1, err2)
	}
	if _, err := createChangeRecord(r.env.StateDir, &changeRecord{User: "..", Kind: "ip"}, at); err == nil {
		t.Error("user .. 거부")
	}
}

func TestChangeRecordWriteFailAborts(t *testing.T) {
	r := newChgRig(t)
	os.MkdirAll(r.env.StateDir, 0700)
	os.WriteFile(filepath.Join(r.env.StateDir, "changes"), []byte("x"), 0600) // 디렉터리 자리에 파일
	c := chgRun(t, r, kindIP, "user1", "", "h1 10.1.1.5\n")
	if len(r.w.calls) != 0 {
		t.Fatalf("기록을 못 쓰면 실행하지 않음: %d", len(r.w.calls))
	}
	if c.recErr == "" || c.results[0].IPResult != rsFail {
		t.Errorf("결과: %+v %q", c.results[0], c.recErr)
	}
	if !strings.Contains(stripANSI(renderChange(c, 100, 0, false)), "기록 문제") {
		t.Error("화면에 기록 문제 안내")
	}
}

func listRecs(t *testing.T, dir string) []string {
	ents, _ := os.ReadDir(dir)
	var n []string
	for _, e := range ents {
		n = append(n, e.Name())
	}
	sort.Strings(n)
	return n
}

func TestChangeRollbackAutofillAndMark(t *testing.T) {
	r := newChgRig(t)
	r.w.ipFail["h3"] = "x"
	r.w.os8Down["h4"] = true
	chgRun(t, r, kindIP, "user1", "", "h1 10.1.1.5\nh3 10.1.1.7\nh4 10.1.1.8\n") // h1 OK, h3 FAIL, h4 OK(os6)
	chgRun(t, r, kindLDAP, "user1", "1", "h2 h5\n")                              // h2,h5 LDAP OK
	chgRun(t, r, kindIP, "other", "", "h9 10.1.1.9\n")                           // 다른 user
	r.w.os8Down = map[string]bool{}
	r.w.calls = nil

	// IP+LDAP 원복: 자동 채움 = IP OK(h1,h4) ∪ LDAP OK(h2,h5), 다른 user·실패 호스트 제외
	c := newChangeState(r.env, kindRollback)
	chgKeys(c, "3user1\n")
	if c.step != csInput || !c.prefilled {
		t.Fatalf("자동 채움: step=%d msg=%q", c.step, c.msg)
	}
	got := strings.Split(string(c.in), "\n")
	sort.Strings(got)
	if strings.Join(got, ",") != "h1,h2,h4,h5" {
		t.Errorf("자동 채움 대상: %v", got)
	}
	if !c.recOS6["h4"] {
		t.Error("기록의 os6 경로를 이어받아야 함")
	}
	if !strings.Contains(stripANSI(renderChange(c, 100, 0, false)), "기록에서 채운 대상 4대") {
		t.Error("입력 화면에 자동 채움 표시")
	}
	chgKeys(c, "\x04")
	plain := stripANSI(renderChange(c, 100, 0, false))
	if !strings.Contains(plain, "가장 최근 백업으로 되돌림") || !strings.Contains(plain, "yes 를 입력하면 4대") {
		t.Errorf("원복 미리보기:\n%s", plain)
	}
	chgKeys(c, "yes\n")
	chgWait(t, c)
	ipRb, ldRb := r.w.callsOf("ipRb"), r.w.callsOf("ldapRb")
	if len(ipRb) != 2 || len(ldRb) != 2 {
		t.Fatalf("원복 호출 (os8 + h4 os6): ip %d ldap %d", len(ipRb), len(ldRb))
	}
	for _, cl := range ldRb {
		if !hasArg(cl.args, "-no-restart") || !hasArg(cl.args, "-rollback") || hasArg(cl.args, "-infra") {
			t.Errorf("LDAP 원복 인자: %v", cl.args)
		}
	}
	if ipRb[1].via != "os6" || strings.Join(ipRb[1].hosts, ",") != "h4" || strings.Contains(strings.Join(ipRb[0].hosts, ","), "h4") {
		t.Errorf("h4 는 처음부터 os6: %+v", ipRb)
	}
	if !strings.HasSuffix(c.recordPath(), "_rollback-both.json") {
		t.Errorf("원복 기록 이름: %s", c.recordPath())
	}
	// 이전 기록 갱신 (원자적: 임시 파일 남지 않음)
	dir := filepath.Join(r.env.StateDir, "changes", "user1")
	for _, n := range listRecs(t, dir) {
		if !changeRecRe.MatchString(n) {
			t.Errorf("임시 파일이 남음: %s", n)
		}
	}
	for _, rf := range loadChangeRecords(r.env.StateDir, "user1") {
		for _, h := range rf.rec.Hosts {
			switch {
			case rf.rec.Kind == "ip" && h.IPResult == rsOK && !h.IPRolledBack:
				t.Errorf("IP 원복 표시 안 됨: %+v", h)
			case rf.rec.Kind == "ip" && h.IPResult == rsFail && h.IPRolledBack:
				t.Errorf("실패 호스트는 원복 표시 안 함: %+v", h)
			case rf.rec.Kind == "ldap" && (!h.LDAPRolledBack || h.ApplPending):
				t.Errorf("LDAP 원복 → ldap_rolled_back, appl_pending=false: %+v", h)
			}
		}
	}
	// 다시 원복 메뉴를 열면 채울 대상 없음
	c = newChangeState(r.env, kindRollback)
	chgKeys(c, "3user1\n")
	if c.prefilled || !strings.Contains(c.msg, "기록에 대상 호스트가 없습니다") {
		t.Errorf("원복 후 자동 채움: %q %q", string(c.in), c.msg)
	}
	// 다른 user 기록은 그대로
	if hs, _ := changeAutofill(r.env.StateDir, "other", "ip"); strings.Join(hs, ",") != "h9" {
		t.Errorf("other: %v", hs)
	}
}

func TestChangeAutofillReplaceAndEdit(t *testing.T) {
	r := newChgRig(t)
	chgRun(t, r, kindIP, "user1", "", "h1 10.1.1.5\nh2 10.1.1.6\n")
	c := newChangeState(r.env, kindRollback)
	chgKeys(c, "1user1\n")
	if string(c.in) != "h1\nh2" {
		t.Fatalf("채움: %q", string(c.in))
	}
	chgKeys(c, "\x7f") // Backspace: 목록 편집
	if c.prefilled || string(c.in) != "h1\nh" {
		t.Errorf("편집: %q", string(c.in))
	}
	c = newChangeState(r.env, kindRollback)
	chgKeys(c, "1user1\nzz9\n") // 새 입력: 목록 대체
	if string(c.in) != "zz9\n" {
		t.Errorf("대체: %q", string(c.in))
	}
}

func TestChangeApplRestore(t *testing.T) {
	r := newChgRig(t)
	r.w.ldap["h3"] = "NOCHANGE"
	chgRun(t, r, kindLDAP, "user1", "1", "h1 h2 h3 h4\n") // h1,h2,h4 OK → 대기, h3 NOCHANGE → 대기 아님
	r.w.appl["h2"], r.w.appl["h4"] = "nofile", "noscript"
	r.w.calls = nil
	c := newChangeState(r.env, kindApplRestore)
	chgKeys(c, "user1\n")
	if strings.Join(strings.Split(string(c.in), "\n"), ",") != "h1,h2,h4" {
		t.Fatalf("auto.appl 대기 자동 채움: %q", string(c.in))
	}
	chgKeys(c, "\x04")
	if !strings.Contains(stripANSI(renderChange(c, 120, 0, false)), applScriptPath+" 만 실행") {
		t.Error("미리보기 안내")
	}
	chgKeys(c, "yes\n")
	chgWait(t, c)
	calls := r.w.callsOf("appl")
	if len(calls) != 1 || calls[0].name != "gossh" || !hasArg(calls[0].args, "-pm") || argAfter(calls[0].args, "-w") == "" {
		t.Fatalf("gossh 직접 호출: %+v", calls)
	}
	cmd := calls[0].args[len(calls[0].args)-1]
	if !strings.HasPrefix(cmd, "echo ") || strings.ContainsAny(cmd, `'"`) {
		t.Errorf("따옴표 없는 원격 명령이어야 함: %.60s", cmd)
	}
	res := chgRes(c)
	if res["h1"].ApplResult != rsOK || res["h2"].ApplResult != rsNoPending || res["h4"].ApplResult != rsFail {
		t.Errorf("결과: %+v %+v %+v", res["h1"], res["h2"], res["h4"])
	}
	hs, _ := changeAutofill(r.env.StateDir, "user1", "appl")
	if strings.Join(hs, ",") != "h4" {
		t.Errorf("성공·대기없음은 appl_pending=false, 실패는 남음: %v", hs)
	}
}

func TestChangeResultScreenNotes(t *testing.T) {
	r := newChgRig(t)
	c := chgRun(t, r, kindBoth, "user1", "1", "h1 10.1.1.5\n")
	plain := stripANSI(renderChange(c, 120, 0, false))
	for _, w := range []string{"h1", "IP OK 10.0.0.9 → 10.1.1.5 (GW 10.1.1.1)", "LDAP OK", "IP: OK 1", "LDAP: OK 1",
		"네트워크 재시작/재부팅 후 반영", "auto.appl 대기 1대 — m 메뉴의 auto.appl 원복으로 반영", "무작위로 몇 대를 직접 접속해 확인하십시오", "기록: "} {
		if !strings.Contains(plain, w) {
			t.Errorf("결과 화면에 %q 없음:\n%s", w, plain)
		}
	}
	col := renderChange(c, 120, 0, true)
	if !strings.Contains(col, "\x1b[32mOK\x1b[0m") {
		t.Error("OK 는 초록")
	}
	if !chgKeys(c, "q") {
		t.Error("결과에서 q 로 닫힘")
	}
}
