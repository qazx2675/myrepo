package fixer

import (
	"strings"
	"testing"

	"vm-param-check/model"
)

// vmInfo는 테스트용 최소 VMInfo를 만든다.
func vmInfo(name string, poweredOn bool, extra map[string]string) model.VMInfo {
	if extra == nil {
		extra = map[string]string{}
	}
	return model.VMInfo{Name: name, PoweredOn: poweredOn, ExtraConfig: extra}
}

func finding(vm, key, expected, actual, result string) model.Finding {
	return model.Finding{VM: vm, Key: key, Expected: expected, Actual: actual, Result: result}
}

// extraValue는 fix.ExtraConfig에서 key의 값을 찾는다.
func extraValue(t *testing.T, fix VMFix, key string) (string, bool) {
	t.Helper()
	for _, ov := range fix.ExtraConfig {
		opt := ov.GetOptionValue()
		if opt.Key == key {
			s, _ := opt.Value.(string)
			return s, true
		}
	}
	return "", false
}

// FAIL/설정없음 항목만 교정 대상이 되고 OK는 손대지 않는지 확인한다.
func TestBuildPlanOnlyFixesBroken(t *testing.T) {
	findings := []model.Finding{
		finding("hostev01", "sched.mem.prealloc", "TRUE", "", "설정없음"),
		finding("hostev01", "sched.swap.vmxSwapEnabled", "FALSE", "FALSE", "OK"),
		finding("hostev01", "sched.vcpu0.affinity", "0,1", "3", "FAIL"),
	}
	vms := []model.VMInfo{vmInfo("hostev01", false, nil)}

	plan, err := BuildPlan(findings, vms)
	if err != nil {
		t.Fatalf("BuildPlan 실패: %v", err)
	}
	if len(plan.Fixes) != 1 {
		t.Fatalf("교정 대상 VM 1대를 기대했으나 %d대", len(plan.Fixes))
	}

	fix := plan.Fixes[0]
	if _, ok := extraValue(t, fix, "sched.swap.vmxSwapEnabled"); ok {
		t.Error("OK 항목(sched.swap.vmxSwapEnabled)이 교정 대상에 들어갔다")
	}
	if v, ok := extraValue(t, fix, "sched.mem.prealloc"); !ok || v != "TRUE" {
		t.Errorf("설정없음 항목이 기대값으로 교정되지 않음: %q, 존재=%v", v, ok)
	}
	if v, ok := extraValue(t, fix, "sched.vcpu0.affinity"); !ok || v != "0,1" {
		t.Errorf("affinity FAIL이 기대값으로 교정되지 않음: %q, 존재=%v", v, ok)
	}
	// 메모리 고정 계열을 건드렸으므로 sched.mem.pin이 함께 기록되어야 한다.
	if v, ok := extraValue(t, fix, "sched.mem.pin"); !ok || v != "TRUE" {
		t.Errorf("sched.mem.pin이 함께 기록되지 않음: %q, 존재=%v", v, ok)
	}
}

// affinity만 FAIL이면 메모리 고정 블록을 건드리지 않으므로 sched.mem.pin도 넣지 않는다.
func TestBuildPlanNoMemPinForAffinityOnly(t *testing.T) {
	findings := []model.Finding{
		finding("hostev01", "sched.vcpu0.affinity", "0,1", "3", "FAIL"),
	}
	plan, err := BuildPlan(findings, []model.VMInfo{vmInfo("hostev01", false, nil)})
	if err != nil {
		t.Fatalf("BuildPlan 실패: %v", err)
	}
	if _, ok := extraValue(t, plan.Fixes[0], "sched.mem.pin"); ok {
		t.Error("affinity만 교정하는데 sched.mem.pin이 들어갔다")
	}
}

// CPU 토폴로지 UI 필드가 하드웨어 필드로 설정되는지, 그리고 둘 중 하나만 FAIL이어도
// vCPU/소켓당 코어를 함께 맞추는지 확인한다(vSphere 나누어떨어짐 제약 때문).
func TestBuildPlanTopologyFields(t *testing.T) {
	findings := []model.Finding{
		finding("hostev01", "config.hardware.numCPU", "8", "8", "OK"),
		finding("hostev01", KeyCoresPerSock, "4", "1", "FAIL"),
		finding("hostev01", KeyNumaCoresNode, "4", "", "설정없음"),
	}
	plan, err := BuildPlan(findings, []model.VMInfo{vmInfo("hostev01", false, nil)})
	if err != nil {
		t.Fatalf("BuildPlan 실패: %v", err)
	}

	fix := plan.Fixes[0]
	if fix.NumCoresPerSocket == nil || *fix.NumCoresPerSocket != 4 {
		t.Errorf("NumCoresPerSocket이 기대값 4로 설정되지 않음: %v", fix.NumCoresPerSocket)
	}
	if fix.NumCPUs == nil || *fix.NumCPUs != 8 {
		t.Errorf("코어수만 FAIL이어도 NumCPUs를 함께 맞춰야 함: %v", fix.NumCPUs)
	}
	if fix.CoresPerNumaNode == nil || *fix.CoresPerNumaNode != 4 {
		t.Errorf("CoresPerNumaNode가 기대값 4로 설정되지 않음: %v", fix.CoresPerNumaNode)
	}
}

// vCPU가 소켓당 코어 수로 나누어떨어지지 않으면 vSphere가 거부하므로 계획 단계에서 막는다.
func TestBuildPlanRejectsIndivisibleTopology(t *testing.T) {
	findings := []model.Finding{
		finding("hostev01", "config.hardware.numCPU", "6", "8", "FAIL"),
		finding("hostev01", KeyCoresPerSock, "4", "1", "FAIL"),
	}
	_, err := BuildPlan(findings, []model.VMInfo{vmInfo("hostev01", false, nil)})
	if err == nil {
		t.Fatal("나누어떨어지지 않는 조합인데 에러가 나지 않았다")
	}
	if !strings.Contains(err.Error(), "나누어떨어지지") {
		t.Errorf("예상과 다른 에러: %v", err)
	}
}

// 메모리/디스크/Shares/호스트 전원정책은 자동교정 대상이 아니라 수동조치로 분류되어야 한다.
func TestBuildPlanManualItems(t *testing.T) {
	findings := []model.Finding{
		finding("hostev01", "config.hardware.memoryMB (GB 환산)", "64", "32", "FAIL"),
		finding("hostev01", "cpuAllocation.shares (CPU Shares Ratio)", "8000", "1000", "FAIL"),
		finding("hostev01", "host power policy", "High Performance", "Balanced", "FAIL"),
	}
	plan, err := BuildPlan(findings, []model.VMInfo{vmInfo("hostev01", false, nil)})
	if err != nil {
		t.Fatalf("BuildPlan 실패: %v", err)
	}
	if len(plan.Fixes) != 0 {
		t.Errorf("수동조치 항목만 있는데 교정 대상이 생겼다: %d대", len(plan.Fixes))
	}
	if len(plan.Manual) != 3 {
		t.Errorf("수동조치 3건을 기대했으나 %d건", len(plan.Manual))
	}
}

// affinity 키는 vcpu 번호 순으로 기록되어야 한다(sched.vcpu2가 sched.vcpu10보다 앞).
func TestAffinityKeyOrder(t *testing.T) {
	broken := map[string]bool{
		"sched.vcpu10.affinity": true,
		"sched.vcpu2.affinity":  true,
		"sched.vcpu1.affinity":  true,
	}
	got := sortedAffinityKeys(broken)
	want := []string{"sched.vcpu1.affinity", "sched.vcpu2.affinity", "sched.vcpu10.affinity"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("정렬 결과가 다름: %v", got)
		}
	}
}

// 전원이 켜진 VM이 하나라도 있으면 게이트에서 막아야 한다.
func TestGatePowerOn(t *testing.T) {
	vms := []model.VMInfo{
		vmInfo("hostev01", false, nil),
		vmInfo("hostev02", true, nil),
	}
	err := CheckGates([]string{"hostev01", "hostev02"}, vms)
	if err == nil || !strings.Contains(err.Error(), "전원 OFF 검증 실패") {
		t.Fatalf("전원 ON VM을 막지 못했다: %v", err)
	}
}

// 같은 그룹 안에서 스펙이 다르면 동질성 게이트가 막아야 한다.
func TestGateHomogeneity(t *testing.T) {
	a := vmInfo("aaev01", false, nil)
	a.NumCPU = 8
	b := vmInfo("bbev01", false, nil)
	b.NumCPU = 16

	err := CheckGates([]string{"aaev01", "bbev01"}, []model.VMInfo{a, b})
	if err == nil || !strings.Contains(err.Error(), "동질성 검증 실패") {
		t.Fatalf("스펙 불일치를 막지 못했다: %v", err)
	}
}

// ev01/ev02 대수가 다르면 막아야 한다.
func TestGateGroupCount(t *testing.T) {
	vms := []model.VMInfo{
		vmInfo("aaev01", false, nil),
		vmInfo("bbev01", false, nil),
		vmInfo("aaev02", false, nil),
	}
	err := CheckGates([]string{"aaev01", "bbev01", "aaev02"}, vms)
	if err == nil || !strings.Contains(err.Error(), "대수가 다릅니다") {
		t.Fatalf("그룹 대수 불일치를 막지 못했다: %v", err)
	}
}

// 스펙이 같고 전부 꺼져 있으면 통과해야 한다.
func TestGatePass(t *testing.T) {
	vms := []model.VMInfo{vmInfo("aaev01", false, nil), vmInfo("bbev01", false, nil)}
	if err := CheckGates([]string{"aaev01", "bbev01"}, vms); err != nil {
		t.Fatalf("정상 조건인데 게이트가 막았다: %v", err)
	}
}
