package checker

import (
	"testing"

	"vm-param-check/model"
)

// -shares-ev01=normal 일 때는 ratio 숫자가 아니라 Level이 normal인지로 판정해야 한다.
// CPU/메모리 둘 다 normal이어야 통과다.
func TestCheckSharesNormal(t *testing.T) {
	tests := []struct {
		name     string
		cpuLevel string
		memLevel string
		wantCPU  string
		wantMem  string
	}{
		{"둘 다 normal", "normal", "normal", "OK", "OK"},
		{"CPU만 custom", "custom", "normal", "FAIL", "OK"},
		{"메모리만 high", "normal", "high", "OK", "FAIL"},
		{"둘 다 custom", "custom", "custom", "FAIL", "FAIL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := model.VMInfo{
				Name:              "host01ev01",
				CPUSharesLevel:    tt.cpuLevel,
				MemorySharesLevel: tt.memLevel,
				// ratio 값은 normal 판정에서 무시돼야 한다 — 일부러 엉뚱한 값을 넣어둔다.
				CPUShares:    12345,
				MemoryShares: 67890,
			}
			got := checkShares(vm, SharesExpect{EV01: []SharesItem{{Normal: true}}}, "ev01", false)
			if len(got) != 2 {
				t.Fatalf("Finding %d개, 기대 2개(CPU/메모리): %+v", len(got), got)
			}
			if got[0].Result != tt.wantCPU {
				t.Errorf("CPU Result = %q, 기대 %q (actual=%q)", got[0].Result, tt.wantCPU, got[0].Actual)
			}
			if got[1].Result != tt.wantMem {
				t.Errorf("메모리 Result = %q, 기대 %q (actual=%q)", got[1].Result, tt.wantMem, got[1].Actual)
			}
			if got[0].Expected != "normal" || got[1].Expected != "normal" {
				t.Errorf("Expected 가 normal이 아닙니다: cpu=%q mem=%q", got[0].Expected, got[1].Expected)
			}
		})
	}
}

// 디스크는 허용값을 여러 개 줄 수 있다(환산 차이로 1024/1026처럼 갈리는 경우).
func TestCheckHardwareDiskAllowsMultiple(t *testing.T) {
	diskFinding := func(actualGB float64, expect []int) model.Finding {
		vm := model.VMInfo{Name: "host01ev01", DiskGB: actualGB}
		got := CheckHardware(vm,
			CPUExpect{}, MemExpect{}, DiskExpect{Base: expect},
			SharesExpect{EV01: []SharesItem{{Normal: true}}}, "ev01", false, false)
		for _, f := range got {
			if f.Key == "disk total capacity (GB 환산, 반올림)" {
				return f
			}
		}
		t.Fatalf("디스크 Finding이 없습니다: %+v", got)
		return model.Finding{}
	}

	tests := []struct {
		name     string
		actual   float64
		expect   []int
		want     string
		wantExpS string
	}{
		{"목록 첫번째와 일치", 1024, []int{1024, 1026}, "OK", "1024 또는 1026"},
		{"목록 두번째와 일치", 1026, []int{1024, 1026}, "OK", "1024 또는 1026"},
		{"목록에 없음", 1025, []int{1024, 1026}, "FAIL", "1024 또는 1026"},
		// 값 1개면 표시도 기존과 똑같이 숫자만 나와야 한다(CSV 회귀 방지).
		{"단일값 일치", 500, []int{500}, "OK", "500"},
		{"단일값 불일치", 400, []int{500}, "FAIL", "500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := diskFinding(tt.actual, tt.expect)
			if f.Result != tt.want {
				t.Errorf("Result = %q, 기대 %q", f.Result, tt.want)
			}
			if f.Expected != tt.wantExpS {
				t.Errorf("Expected 표시 = %q, 기대 %q", f.Expected, tt.wantExpS)
			}
		})
	}
}

// normal을 안 주면 기존 동작(ratio 숫자 비교) 그대로여야 한다 — 회귀 방지.
func TestCheckSharesRatioUnchanged(t *testing.T) {
	vm := model.VMInfo{
		Name:              "host01ev01",
		CPUSharesLevel:    "custom",
		MemorySharesLevel: "custom",
		CPUShares:         4000,
		MemoryShares:      4000,
	}
	got := checkShares(vm, SharesExpect{EV01: RatioShares(4000)}, "ev01", false)
	if len(got) != 2 {
		t.Fatalf("Finding %d개, 기대 2개: %+v", len(got), got)
	}
	if got[0].Result != "OK" || got[1].Result != "OK" {
		t.Errorf("ratio 일치인데 OK가 아닙니다: cpu=%q mem=%q", got[0].Result, got[1].Result)
	}

	// Level이 custom이 아니면 ratio 모드에서는 여전히 FAIL이어야 한다.
	vm.CPUSharesLevel = "normal"
	got = checkShares(vm, SharesExpect{EV01: RatioShares(4000)}, "ev01", false)
	if got[0].Result != "FAIL" {
		t.Errorf("ratio 모드인데 level=normal이 FAIL이 아닙니다: %q", got[0].Result)
	}
}

// -shares-ev01=4000,normal 처럼 쉼표로 여러 개를 섞으면 둘 중 하나만 맞아도 OK여야 한다.
func TestCheckSharesMixedList(t *testing.T) {
	items := []SharesItem{{Ratio: 4000}, {Normal: true}}

	// ratio로 일치
	vm := model.VMInfo{Name: "host01ev01", CPUSharesLevel: "custom", CPUShares: 4000, MemorySharesLevel: "custom", MemoryShares: 4000}
	got := checkShares(vm, SharesExpect{EV01: items}, "ev01", false)
	if got[0].Result != "OK" || got[1].Result != "OK" {
		t.Errorf("ratio 매칭인데 OK가 아닙니다: cpu=%q mem=%q", got[0].Result, got[1].Result)
	}

	// normal로 일치 (ratio는 목록에 없는 값이어도 normal이면 OK)
	vm = model.VMInfo{Name: "host01ev01", CPUSharesLevel: "normal", CPUShares: 99999, MemorySharesLevel: "normal", MemoryShares: 99999}
	got = checkShares(vm, SharesExpect{EV01: items}, "ev01", false)
	if got[0].Result != "OK" || got[1].Result != "OK" {
		t.Errorf("normal 매칭인데 OK가 아닙니다: cpu=%q mem=%q", got[0].Result, got[1].Result)
	}
	if got[0].Actual != "level=normal" {
		t.Errorf("Actual 표시가 예상과 다릅니다: %q", got[0].Actual)
	}

	// 둘 다 아니면 FAIL
	vm = model.VMInfo{Name: "host01ev01", CPUSharesLevel: "custom", CPUShares: 1234, MemorySharesLevel: "custom", MemoryShares: 1234}
	got = checkShares(vm, SharesExpect{EV01: items}, "ev01", false)
	if got[0].Result != "FAIL" || got[1].Result != "FAIL" {
		t.Errorf("둘 다 불일치인데 FAIL이 아닙니다: cpu=%q mem=%q", got[0].Result, got[1].Result)
	}
	if got[0].Expected != "4000 또는 normal" {
		t.Errorf("Expected 표시가 예상과 다릅니다: %q", got[0].Expected)
	}
}
