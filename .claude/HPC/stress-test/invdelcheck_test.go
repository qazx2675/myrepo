package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

const hostsKey = "GET /api/v2/inventories/7/hosts/"

// 삭제 확인 시험 준비: 삭제 스크립트는 성공하는 가짜 ssh, 시계는 가짜, 재조회 간격은 10초.
func newDeleteCheckApp(t *testing.T) (*app, *fakeAWX, *strings.Builder) {
	a, f, _ := newTestApp(t)
	a.user = "hong"
	a.cfg.InventoryDeleteHost = "delhost01"
	a.cfg.PollInterval = 10
	oldUnit := pollUnit
	pollUnit = time.Second
	t.Cleanup(func() { pollUnit = oldUnit })
	fakeClock(t)
	fakeDeleteSSH(t, "exit 0")
	var out strings.Builder
	a.out = &out
	return a, f, &out
}

func historyText(a *app) string {
	b, _ := os.ReadFile(a.cfg.HistoryFile)
	return string(b)
}

func TestDeleteCheckBeforeEmptyContinues(t *testing.T) {
	a, f, out := newDeleteCheckApp(t)
	a.batchMode = true
	f.hostSeq = [][]string{{}}
	if err := a.inventoryDelete(); err != nil {
		t.Fatal(err)
	}
	if f.calls[hostsKey] != 1 {
		t.Errorf("삭제 전이 비었으면 다시 읽지 않아야 합니다: 조회 %d회", f.calls[hostsKey])
	}
	if !strings.Contains(out.String(), "[삭제확인] before=0 after=0") || strings.Contains(historyText(a), "status=fail") {
		t.Errorf("out=%q hist=%q", out.String(), historyText(a))
	}
}

func TestDeleteCheckEmptiedOK(t *testing.T) {
	a, f, out := newDeleteCheckApp(t)
	a.batchMode = true
	f.hostSeq = [][]string{{"n1", "n2", "n3"}, {}}
	if err := a.inventoryDelete(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[삭제확인] before=3 after=0\n") {
		t.Errorf("out=%q", out.String())
	}
	if h := historyText(a); !strings.Contains(h, "action=inventory_delete_check status=ok before=3 after=0") {
		t.Errorf("hist=%q", h)
	}
}

// 대화형(-batch 아님)에서는 [삭제확인] 줄을 내지 않는다.
func TestDeleteCheckNoMarkerWithoutBatch(t *testing.T) {
	a, f, out := newDeleteCheckApp(t)
	f.hostSeq = [][]string{{"n1"}, {}}
	if err := a.inventoryDelete(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), deleteCheckMark) {
		t.Errorf("대화형에서 마커 줄을 냈습니다: %q", out.String())
	}
}

// 삭제 전과 같은 목록이 30초 동안 계속되면 중지한다. 재조회 횟수를 세고, 등록(yml/동기화)은 일어나지 않는다.
func TestDeleteCheckIdenticalStops(t *testing.T) {
	a, f, out := newDeleteCheckApp(t)
	a.batchMode = true
	f.hostSeq = [][]string{{"n1", "N2"}}
	err := a.inventoryDelete()
	if err == nil || !strings.Contains(err.Error(), "삭제되지 않았") || !strings.Contains(err.Error(), "N2") || strings.Contains(err.Error(), "확인 불가") {
		t.Fatalf("err = %v", err)
	}
	// 삭제 전 1회 + 삭제 뒤 0·10·20·30초 4회
	if f.calls[hostsKey] != 5 {
		t.Errorf("호스트 조회 %d회, want 5", f.calls[hostsKey])
	}
	if f.calls["POST /api/v2/inventory_sources/11/update/"] != 0 || len(f.launches) != 0 {
		t.Error("중지했는데 등록/launch 가 일어났습니다")
	}
	if strings.Contains(out.String(), deleteCheckMark) {
		t.Errorf("실패인데 [삭제확인] 줄을 냈습니다: %q", out.String())
	}
	if h := historyText(a); !strings.Contains(h, "action=inventory_delete_check status=fail before=2 after=2") {
		t.Errorf("hist=%q", h)
	}
}

// 늦게 반영돼 재조회 중에 비면 통과한다.
func TestDeleteCheckLateEmptied(t *testing.T) {
	a, f, _ := newDeleteCheckApp(t)
	f.hostSeq = [][]string{{"n1"}, {"n1"}, {"n1"}, {}}
	if err := a.inventoryDelete(); err != nil {
		t.Fatal(err)
	}
	if f.calls[hostsKey] != 4 {
		t.Errorf("조회 %d회, want 4", f.calls[hostsKey])
	}
}

func TestDeleteCheckPartialWarns(t *testing.T) {
	a, f, out := newDeleteCheckApp(t)
	f.hostSeq = [][]string{{"n1", "n2", "n3"}, {"n3"}}
	if err := a.inventoryDelete(); err != nil {
		t.Fatalf("일부만 남으면 계속 진행해야 합니다: %v", err)
	}
	if !strings.Contains(out.String(), markWarn) || !strings.Contains(out.String(), "n3") {
		t.Errorf("경고가 없습니다: %q", out.String())
	}
	if f.calls[hostsKey] != 2 {
		t.Errorf("조회 %d회, want 2", f.calls[hostsKey])
	}
	if h := historyText(a); !strings.Contains(h, "status=warn before=3 after=1") {
		t.Errorf("hist=%q", h)
	}
}

func TestDeleteCheckUnverifiedBefore(t *testing.T) {
	a, f, _ := newDeleteCheckApp(t)
	f.hostFail = map[int]bool{1: true}
	rec := fakeDeleteSSH(t, "exit 0")
	err := a.inventoryDelete()
	if err == nil || !strings.Contains(err.Error(), "확인 불가") || strings.Contains(err.Error(), "삭제되지 않았") {
		t.Fatalf("err = %v", err)
	}
	if _, e := os.Stat(rec); e == nil {
		t.Error("삭제 전 조회가 실패했는데 삭제 스크립트를 실행했습니다")
	}
	if h := historyText(a); !strings.Contains(h, "status=unverified") {
		t.Errorf("hist=%q", h)
	}
}

func TestDeleteCheckUnverifiedAfter(t *testing.T) {
	a, f, _ := newDeleteCheckApp(t)
	f.hostSeq = [][]string{{"n1"}}
	f.hostFail = map[int]bool{2: true}
	err := a.inventoryDelete()
	if err == nil || !strings.Contains(err.Error(), "확인 불가") || strings.Contains(err.Error(), "삭제되지 않았") {
		t.Fatalf("err = %v", err)
	}
	if h := historyText(a); !strings.Contains(h, "status=unverified") || strings.Contains(h, "status=fail") {
		t.Errorf("hist=%q", h)
	}
}

// 제외 호스트는 양쪽에서 빠지고 대소문자는 무시한다.
func TestDeleteCheckExcludeAndCase(t *testing.T) {
	a, f, out := newDeleteCheckApp(t)
	a.batchMode = true
	a.cfg.InventoryMatchExclude = []string{"MGMT01"}
	// 삭제 뒤에는 제외 호스트만 남음 → 비었다고 본다
	f.hostSeq = [][]string{{"mgmt01", "N1", "n2"}, {"Mgmt01"}}
	if err := a.inventoryDelete(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[삭제확인] before=2 after=0") {
		t.Errorf("out=%q", out.String())
	}
	// 대소문자만 다른 같은 목록 → 삭제 안 됨
	a2, f2, _ := newDeleteCheckApp(t)
	f2.hostSeq = [][]string{{"n1", "n2"}, {"N2", "N1"}}
	if err := a2.inventoryDelete(); err == nil || !strings.Contains(err.Error(), "삭제되지 않았") {
		t.Errorf("대소문자만 다른데 같은 목록으로 보지 않았습니다: %v", err)
	}
	// 제외 호스트만 있던 인벤토리 → 지울 것 없음, 재조회 없이 통과
	a3, f3, _ := newDeleteCheckApp(t)
	a3.cfg.InventoryMatchExclude = []string{"mgmt01"}
	f3.hostSeq = [][]string{{"MGMT01"}}
	if err := a3.inventoryDelete(); err != nil || f3.calls[hostsKey] != 1 {
		t.Errorf("err=%v 조회 %d회", err, f3.calls[hostsKey])
	}
}

func TestDeleteCheckEmptyHostNoReads(t *testing.T) {
	a, f, out := newDeleteCheckApp(t)
	a.cfg.InventoryDeleteHost = ""
	a.batchMode = true
	if err := a.inventoryDelete(); err != nil {
		t.Fatal(err)
	}
	if f.calls[hostsKey] != 0 || out.Len() != 0 {
		t.Errorf("조회 %d회, out=%q", f.calls[hostsKey], out.String())
	}
}

// 오탐 방지: 삭제 확인은 삭제와 첫 등록 사이에서만 한다. 등록 뒤 인벤토리가 삭제 전 목록과 같아도 register 는 통과하고,
// 삭제 확인 이력을 더 남기지 않는다.
func TestRegisterAfterDeleteCheckNoFalsePositive(t *testing.T) {
	a, f, _ := newDeleteCheckApp(t)
	f.hostSeq = [][]string{{"n1", "n2"}, {}}
	if err := a.inventoryDelete(); err != nil {
		t.Fatal(err)
	}
	checks := strings.Count(historyText(a), "action=inventory_delete_check")
	if checks != 1 {
		t.Fatalf("삭제 확인 이력 %d건, want 1", checks)
	}
	// 등록 단계: 같은 호스트가 다시 들어와 삭제 전 목록과 같아진다
	writeScript(t, sshBin, `f=$(basename "${4##* }"); n=$(wc -l < "$FAKE_REPO/$f"); touch "$FAKE_SVR/${f%.yaml}_${n}ea.yml"`+"\n")
	f.hostSeq = nil
	f.hosts = []string{"n1", "n2"}
	before := f.calls[hostsKey]
	ts := twoTargets()
	if _, err := a.register("x.yaml", ts); err != nil {
		t.Fatalf("등록 뒤 목록이 삭제 전과 같다고 register 가 실패했습니다: %v", err)
	}
	if got := strings.Count(historyText(a), "action=inventory_delete_check"); got != checks {
		t.Errorf("register 가 삭제 확인 이력을 남겼습니다: %d건", got)
	}
	if f.calls[hostsKey] == before {
		t.Error("register 가 호스트를 읽지 않았습니다(시험 전제 확인)")
	}
}
