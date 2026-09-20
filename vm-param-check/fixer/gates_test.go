package fixer

import (
	"strings"
	"testing"

	"vm-param-check/model"
)

// 코어/소켓과 NUMA는 -fix가 기대값으로 직접 고치는 항목이라, FAIL인 VM끼리 값이 달라도
// 동질성 게이트가 막으면 안 된다.
func TestGateIgnoresCoresPerSocketAndNuma(t *testing.T) {
	a := vmInfo("aaev01", false, map[string]string{"numa.vcpu.maxPerVirtualNode": "4"})
	a.NumCoresPerSocket = 4
	b := vmInfo("bbev01", false, map[string]string{"numa.vcpu.maxPerVirtualNode": "16"})
	b.NumCoresPerSocket = 16

	if err := CheckGates([]string{"aaev01", "bbev01"}, []model.VMInfo{a, b}); err != nil {
		t.Fatalf("코어/소켓·NUMA만 다른데 게이트가 막았다: %v", err)
	}
	if d := diffSpec(a, b); d != "" {
		t.Errorf("diffSpec이 코어/소켓·NUMA 차이를 잡았다: %q", d)
	}
}

// 고칠 수 없는(수동조치) 항목과 vCPU/HT는 여전히 비교해서 막아야 한다.
func TestDiffSpecStillDetects(t *testing.T) {
	base := vmInfo("aaev01", false, nil)
	base.NumCPU = 8
	base.MemoryMB = 32768
	base.DiskGB = 200
	base.CPUSharesLevel, base.CPUShares = "custom", 2000

	tests := []struct {
		name   string
		mutate func(*model.VMInfo)
		want   string
	}{
		{"vCPU", func(v *model.VMInfo) { v.NumCPU = 16 }, "vCPU"},
		{"메모리", func(v *model.VMInfo) { v.MemoryMB = 65536 }, "메모리"},
		{"디스크", func(v *model.VMInfo) { v.DiskGB = 400 }, "디스크"},
		{"Shares", func(v *model.VMInfo) { v.CPUShares = 1000 }, "Shares"},
		{"HT", func(v *model.VMInfo) { v.ExtraConfig = map[string]string{"sched.vcpu0.affinity": "0,1"} }, "HT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := base
			other.Name = "bbev01"
			tt.mutate(&other)
			if d := diffSpec(base, other); !strings.Contains(d, tt.want) {
				t.Errorf("diffSpec = %q, %q 차이를 잡아야 한다", d, tt.want)
			}
		})
	}
}

// PASS인 VM은 교정 대상(targets)에서 빠지지만, 그룹 대수는 PASS 포함 전부로 세야 한다.
// ev01: PASS 1 + FAIL 1 / ev02: FAIL 2 -> 실제 구성은 2:2라서 통과해야 한다.
// (예전에는 targets만 세서 ev01=1대, ev02=2대로 잘못 막혔다.)
func TestGateGroupCountIncludesPass(t *testing.T) {
	vms := []model.VMInfo{
		vmInfo("aaev01", false, nil), // PASS라서 교정 대상 아님
		vmInfo("bbev01", false, nil), // FAIL
		vmInfo("aaev02", false, nil), // FAIL
		vmInfo("bbev02", false, nil), // FAIL
	}
	targets := []string{"bbev01", "aaev02", "bbev02"}

	if err := CheckGates(targets, vms); err != nil {
		t.Fatalf("PASS 포함 2:2 구성인데 게이트가 막았다: %v", err)
	}
}

// 조회한 전체 기준으로도 대수가 다르면 여전히 막아야 한다(PASS 여부와 무관).
func TestGateGroupCountMismatchWithPass(t *testing.T) {
	vms := []model.VMInfo{
		vmInfo("aaev01", false, nil), // PASS
		vmInfo("bbev01", false, nil),
		vmInfo("aaev02", false, nil),
		vmInfo("bbev02", false, nil),
		vmInfo("ccev02", false, nil),
	}
	// 교정 대상만 보면 ev01=1, ev02=3 이지만 전체는 ev01=2, ev02=3 — 어느 쪽이든 불일치.
	err := CheckGates([]string{"bbev01", "aaev02", "bbev02", "ccev02"}, vms)
	if err == nil || !strings.Contains(err.Error(), "ev01=2대, ev02=3대") {
		t.Fatalf("전체 기준 대수 불일치(2:3)를 막지 못했거나 표기가 다르다: %v", err)
	}
}

// 교정 대상이 한 그룹뿐이어도, 조회한 전체에 다른 그룹이 있으면 대수는 전체 기준으로 비교한다.
func TestGateGroupCountUsesAllEvenIfTargetsOneGroup(t *testing.T) {
	vms := []model.VMInfo{
		vmInfo("aaev01", false, nil),
		vmInfo("bbev01", false, nil),
		vmInfo("aaev02", false, nil), // ev02는 1대뿐 — 2:1 불일치
	}
	err := CheckGates([]string{"aaev01", "bbev01"}, vms) // 대상은 ev01뿐
	if err == nil || !strings.Contains(err.Error(), "대수가 다릅니다") {
		t.Fatalf("대상이 한 그룹이어도 전체 구성 불일치는 막아야 한다: %v", err)
	}
}

// 그룹이 하나뿐이면(ev01만 있음) 대수 비교 자체가 없다 — 기존 동작 유지.
func TestGateGroupCountSingleGroupSkipped(t *testing.T) {
	vms := []model.VMInfo{vmInfo("aaev01", false, nil), vmInfo("bbev01", false, nil), vmInfo("ccev01", false, nil)}
	if err := CheckGates([]string{"aaev01", "bbev01"}, vms); err != nil {
		t.Fatalf("ev01 한 그룹뿐인데 게이트가 막았다: %v", err)
	}
}

// 에러 문구에는 실제로 비교하는 값만 나와야 한다(코어/소켓·NUMA가 원인처럼 보이면 안 된다).
func TestDescribeSpecOnlyComparedFields(t *testing.T) {
	v := vmInfo("aaev01", false, map[string]string{"numa.vcpu.maxPerVirtualNode": "4"})
	v.NumCPU, v.NumCoresPerSocket = 8, 4
	got := describeSpec(v)
	for _, banned := range []string{"코어/소켓", "NUMA"} {
		if strings.Contains(got, banned) {
			t.Errorf("describeSpec에 비교하지 않는 값(%s)이 들어 있다: %s", banned, got)
		}
	}
}
