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

// -specRoot 로 VM마다 스펙이 다르면 동질성은 같은 스펙의 같은 그룹끼리만 본다.
func TestGateBySpec(t *testing.T) {
	a := vmInfo("aaev01", false, nil)
	a.NumCPU = 8
	b := vmInfo("bbev01", false, nil)
	b.NumCPU = 2
	c := vmInfo("ccev01", false, nil)
	c.NumCPU = 4
	vms := []model.VMInfo{a, b, c}
	targets := []string{"aaev01", "bbev01"}

	if err := CheckGates(targets, vms); err == nil {
		t.Fatal("스펙 구분이 없으면 vCPU 가 다른 ev01 은 막아야 한다")
	}
	if err := CheckGatesBySpec(targets, vms, map[string]string{"aaev01": "A_spec.txt", "bbev01": "B_spec.txt"}); err != nil {
		t.Fatalf("스펙이 다른 ev01 끼리는 비교하지 않아야 한다: %v", err)
	}
	err := CheckGatesBySpec([]string{"aaev01", "ccev01"}, vms, map[string]string{"aaev01": "A_spec.txt", "ccev01": "A_spec.txt"})
	if err == nil || !strings.Contains(err.Error(), "A_spec.txt ev01") {
		t.Fatalf("같은 스펙 안에서 vCPU 가 다르면 막아야 한다: %v", err)
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

// ev01/ev02 짝(VM 대수)이 안 맞아도 게이트는 막지 않는다 — 경고 문구만 나온다.
// ev01: PASS 1 + FAIL 1 / ev02: FAIL 3 (전체 2:3)
func TestGateDoesNotBlockOnGroupCountMismatch(t *testing.T) {
	vms := []model.VMInfo{
		vmInfo("aaev01", false, nil), // PASS
		vmInfo("bbev01", false, nil),
		vmInfo("aaev02", false, nil),
		vmInfo("bbev02", false, nil),
		vmInfo("ccev02", false, nil),
	}
	targets := []string{"bbev01", "aaev02", "bbev02", "ccev02"}

	if err := CheckGates(targets, vms); err != nil {
		t.Fatalf("대수가 달라도 게이트는 막으면 안 된다(경고만): %v", err)
	}
	w := GroupCountWarning(vms)
	if !strings.Contains(w, "ev01=2대, ev02=3대") {
		t.Errorf("경고에 전체 기준 대수(2:3)가 없다: %q", w)
	}
	if !strings.Contains(w, "교정은 그대로 진행") {
		t.Errorf("경고에 계속 진행한다는 안내가 없다: %q", w)
	}
}

// PASS인 VM은 교정 대상(targets)에서 빠지지만, 대수는 PASS 포함 전부로 센다.
// ev01: PASS 1 + FAIL 1 / ev02: FAIL 2 -> 실제 구성은 2:2라서 경고도 없어야 한다.
func TestGroupCountWarningIncludesPass(t *testing.T) {
	vms := []model.VMInfo{
		vmInfo("aaev01", false, nil), // PASS라서 교정 대상 아님
		vmInfo("bbev01", false, nil), // FAIL
		vmInfo("aaev02", false, nil), // FAIL
		vmInfo("bbev02", false, nil), // FAIL
	}
	if w := GroupCountWarning(vms); w != "" {
		t.Errorf("PASS 포함 2:2 구성인데 경고가 나왔다: %q", w)
	}
	if err := CheckGates([]string{"bbev01", "aaev02", "bbev02"}, vms); err != nil {
		t.Fatalf("게이트가 막았다: %v", err)
	}
}

// 교정 대상이 한 그룹뿐이어도 대수는 조회한 전체 기준으로 비교한다.
func TestGroupCountWarningUsesAllVMs(t *testing.T) {
	vms := []model.VMInfo{
		vmInfo("aaev01", false, nil),
		vmInfo("bbev01", false, nil),
		vmInfo("aaev02", false, nil), // ev02는 1대뿐 — 2:1
	}
	if w := GroupCountWarning(vms); !strings.Contains(w, "ev01=2대, ev02=1대") {
		t.Errorf("전체 기준 2:1 경고가 없다: %q", w)
	}
	if err := CheckGates([]string{"aaev01", "bbev01"}, vms); err != nil { // 대상은 ev01뿐
		t.Fatalf("게이트가 막았다: %v", err)
	}
}

// 비교할 그룹이 하나뿐이거나 접미사 없는 VM("기타")만 섞여 있으면 경고 자체가 없다.
func TestGroupCountWarningSkipped(t *testing.T) {
	onlyEV01 := []model.VMInfo{vmInfo("aaev01", false, nil), vmInfo("bbev01", false, nil), vmInfo("ccev01", false, nil)}
	if w := GroupCountWarning(onlyEV01); w != "" {
		t.Errorf("ev01 한 그룹뿐인데 경고가 나왔다: %q", w)
	}
	withOther := []model.VMInfo{vmInfo("aaev01", false, nil), vmInfo("plainhost", false, nil), vmInfo("plainhost2", false, nil)}
	if w := GroupCountWarning(withOther); w != "" {
		t.Errorf("기타 그룹은 대수 비교 대상이 아닌데 경고가 나왔다: %q", w)
	}
	if w := GroupCountWarning(nil); w != "" {
		t.Errorf("VM이 없는데 경고가 나왔다: %q", w)
	}
}

// ev03까지 세 그룹이면 세 그룹의 대수가 모두 표기된다.
func TestGroupCountWarningThreeGroups(t *testing.T) {
	vms := []model.VMInfo{
		vmInfo("aaev01", false, nil), vmInfo("bbev01", false, nil),
		vmInfo("aaev02", false, nil), vmInfo("bbev02", false, nil),
		vmInfo("aaev03", false, nil),
	}
	if w := GroupCountWarning(vms); !strings.Contains(w, "ev01=2대, ev02=2대, ev03=1대") {
		t.Errorf("세 그룹 대수 표기가 다르다: %q", w)
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
