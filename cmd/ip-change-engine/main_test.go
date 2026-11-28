package main

import (
	"reflect"
	"strings"
	"testing"

	"ip-change/internal/remote"
)

func TestReorderArgs(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"list", "-check"}, []string{"-check", "list"}},
		{[]string{"-u", "admin", "list", "-p"}, []string{"-u", "admin", "-p", "list"}},
		{[]string{"-p", "list"}, []string{"-p", "list"}},
		{[]string{"list", "-bond-mode=lacp", "-c", "5"}, []string{"-bond-mode=lacp", "-c", "5", "list"}},
		{[]string{"list"}, []string{"list"}},
	}
	for _, c := range cases {
		if got := reorderArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("reorderArgs(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseNodeOK(t *testing.T) {
	line := "RESULT|WARN|h1|1.1.1.1|2.2.2.2|2.2.2.1|eth0|static|RHEL 8|/etc/a/ifcfg-eth0|BACKUP=20260101000000; OLDGW=1.1.1.254; GWFIX=/etc/sysconfig/network,/etc/a/route-eth0"
	row := parseNode("h1", remote.Result{Host: "h1", Lines: []string{"PLAN|/etc/x|IPADDR=2.2.2.2", line}}, true)
	if row.Status != "WARN" || row.Dev != "eth0" || row.NewIP != "2.2.2.2" || len(row.Plan) != 1 {
		t.Fatalf("unexpected row: %+v", row)
	}
	items, hosts := gwCandidates([]nodeRow{row})
	if len(items) != 1 || hosts[0] != "h1" || items[0].Stamp != "20260101000000" || len(items[0].Files) != 2 || items[0].OldGW != "1.1.1.254" {
		t.Fatalf("unexpected gw items: %+v", items)
	}
}

func TestParseNodeFailShort(t *testing.T) {
	row := parseNode("h1", remote.Result{Lines: []string{"RESULT|FAIL|h1|ifcfg 파일을 찾지 못했습니다"}}, true)
	if row.Status != "FAIL" || row.Note != "ifcfg 파일을 찾지 못했습니다" {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestParseNodeNoResult(t *testing.T) {
	auth := remote.Result{Host: "h1", Errs: []string{"ERROR: SSH 접속 실패: ssh: unable to authenticate"}}
	if row := parseNode("h1", auth, true); row.Status != "FAIL" || row.Note != noteNeedPassword {
		t.Fatalf("unexpected row: %+v", row)
	}
	auth.PasswordTried = true
	if row := parseNode("h1", auth, true); row.Note != notePasswordFailed {
		t.Fatalf("unexpected row: %+v", row)
	}
	if row := parseNode("h1", remote.Result{}, false); row.Note != noteNoResponse {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestParseRollback(t *testing.T) {
	r := parseRollback("h", remote.Result{Lines: []string{"RESULT|OK|h|20260101000000|2|1|복원=/a,/b; 제거=/c"}}, true)
	if r.Status != "OK" || r.Stamp != "20260101000000" || r.Restored != "2" || r.Removed != "1" {
		t.Fatalf("unexpected: %+v", r)
	}
	r = parseRollback("h", remote.Result{Lines: []string{"RESULT|OK|h|/etc/ifcfg-eth0|20260101000000|1.1.1.1"}}, true)
	if r.Status != "OK" || r.Stamp != "20260101000000" || !strings.Contains(r.Note, "옛 방식") {
		t.Fatalf("unexpected legacy: %+v", r)
	}
	r = parseRollback("h", remote.Result{Lines: []string{"RESULT|FAIL|h|이미 롤백했습니다"}}, true)
	if r.Status != "FAIL" {
		t.Fatalf("unexpected fail: %+v", r)
	}
}

func TestTableAlignKorean(t *testing.T) {
	out := tableText([]string{"a", "결과", "z"}, [][]string{{"hostname", "OK", "x"}}, 1, false)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 || dispWidth(strings.SplitN(lines[0], " | ", 3)[0]) != dispWidth(strings.SplitN(lines[2], " | ", 3)[0]) {
		t.Fatalf("정렬이 맞지 않습니다:\n%s", out)
	}
}
