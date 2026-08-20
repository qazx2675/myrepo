// Package fixer는 vm-param-check가 낸 FAIL/설정없음 항목을 실제 vCenter 설정 변경으로 옮긴다.
//
// 핵심 원칙: 기대값을 다시 계산하거나 재해석하지 않고, Finding에 이미 들어있는 "기대값"을
// 그대로 기록한다. 기존 외부 도구(main_lpage.go)는 -ev01Numa를 "NUMA 노드 개수"로 해석해서
// numa.vcpu.maxPerVirtualNode에 소켓당 코어 수를 써넣는 등 체크 쪽과 값의 의미가 달랐고,
// 그래서 교정 직후 재검증이 FAIL로 뜨는 문제가 있었다. 여기서는 체크가 기대한 값을 그대로
// 써넣으므로 체크 -> 교정 -> 재검증이 항상 같은 값을 보게 된다.
package fixer

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/vmware/govmomi/vim25/types"

	"vm-param-check/model"
)

// CPU 토폴로지 UI(설정 편집 > VM 옵션 > CPU 토폴로지)가 읽고 쓰는 항목들.
// ExtraConfig가 아니라 ConfigSpec의 하드웨어 필드라서 별도로 취급해야 한다 —
// ExtraConfig(cpuid.coresPerSocket)만 넣으면 UI는 "Assigned at power on"으로 남는다.
const (
	KeyNumCPU        = "config.hardware.numCPU"
	KeyCoresPerSock  = "hardware.numCoresPerSocket (CPU 토폴로지 UI)"
	KeyNumaCoresNode = "config.numaInfo.coresPerNumaNode (CPU 토폴로지 UI)"
)

// advancedOrder는 ExtraConfig(고급 설정)로 교정하는 키들 — 출력/기록 순서를 고정해서
// dry-run 로그가 실행할 때마다 달라지지 않게 한다. 앞의 4개는 checker/fixed.go의 고정
// 기대값이고, 다음 2개는 토폴로지 기대값(-cores/-numa)이 반영되는 항목, 마지막은
// numa.vcpu.preferHT(-preferHT/SPEC_DIR로 값이 주어졌을 때만 체크 대상이 됨) 이다.
var advancedOrder = []string{
	"sched.mem.lpage.enable1GPage",
	"sched.mem.prealloc",
	"sched.mem.prealloc.pinnedMainMem",
	"sched.swap.vmxSwapEnabled",
	"cpuid.coresPerSocket",
	"numa.vcpu.maxPerVirtualNode",
	"numa.vcpu.preferHT",
}

// memPinKey는 vCenter가 Advanced Config로 되돌려주지 않아 체크 대상에서는 빠져 있지만
// (checker/fixed.go 주석 참고), 기존 설정 스크립트(main_lpage.go)가 항상 함께 넣던 키다.
// 메모리 고정(pin/prealloc) 블록을 손댈 때만 같이 기록해서 실제 VMX 상태를 기존 도구와
// 동일하게 맞춘다. 체크에는 여전히 넣지 않는다(넣으면 영구 FAIL이 됨).
const memPinKey = "sched.mem.pin"

// memPinTriggers는 memPinKey를 함께 기록할 조건 — 메모리 고정 계열 키를 교정할 때만.
var memPinTriggers = map[string]bool{
	"sched.mem.lpage.enable1GPage":     true,
	"sched.mem.prealloc":               true,
	"sched.mem.prealloc.pinnedMainMem": true,
	"sched.swap.vmxSwapEnabled":        true,
}

var affinityKeyRe = regexp.MustCompile(`^sched\.vcpu(\d+)\.affinity$`)

// Change는 dry-run 출력용 변경 1건 — 실제 적용 값과 1:1로 대응한다.
type Change struct {
	Where string // "고급설정" | "CPU 토폴로지"
	Key   string
	From  string // 현재값 (설정없음이면 빈 문자열)
	To    string
}

// VMFix는 VM 1대에 적용할 변경 전체다. VM당 Reconfigure를 한 번만 호출하려고
// ExtraConfig와 하드웨어 필드를 한 구조체에 모아둔다.
type VMFix struct {
	VM      string
	Group   string
	Vcenter string // 이 VM을 조회한 vCenter 주소 — 여러 vCenter를 동시에 다룰 때 Apply가 어느 클라이언트를 쓸지 판단하는 데 씀
	Ref     types.ManagedObjectReference

	ExtraConfig []types.BaseOptionValue

	NumCPUs           *int32
	NumCoresPerSocket *int32
	CoresPerNumaNode  *int32

	Changes []Change
}

// Plan은 이번 실행에서 바꿀 것(Fixes)과 사람이 직접 해야 할 것(Manual)을 나눠 담는다.
type Plan struct {
	Fixes    []VMFix
	Manual   []model.Finding
	Warnings []string
}

// TargetVMs는 게이트 검증 대상 VM 이름 목록이다(실제로 Reconfigure가 들어가는 VM만).
func (p *Plan) TargetVMs() []string {
	var out []string
	for _, f := range p.Fixes {
		out = append(out, f.VM)
	}
	return out
}

// fixable은 이 항목Key를 이 도구가 자동으로 고칠 수 있는지 판정한다.
// 메모리/디스크/Shares/네트워크/호스트 전원정책은 여기 포함되지 않는다 — 전부 수동조치 대상.
func fixable(key string) bool {
	switch key {
	case KeyNumCPU, KeyCoresPerSock, KeyNumaCoresNode:
		return true
	}
	for _, k := range advancedOrder {
		if k == key {
			return true
		}
	}
	return affinityKeyRe.MatchString(key)
}

// BuildPlan은 체크 결과(findings)와 조회한 VM 정보(vms)로부터 교정 계획을 만든다.
// findings는 vm-param-check가 낸 것 그대로를 넘긴다 — OK 항목도 필요하다(기대값을 읽어야 해서).
func BuildPlan(findings []model.Finding, vms []model.VMInfo) (*Plan, error) {
	infoByName := map[string]model.VMInfo{}
	for _, vm := range vms {
		infoByName[vm.Name] = vm
	}

	// VM 등장 순서를 유지해서 dry-run 출력과 적용 로그가 실행할 때마다 같은 순서로 나오게 한다.
	var order []string
	expected := map[string]map[string]string{} // VM -> 항목Key -> 기대값
	actual := map[string]map[string]string{}   // VM -> 항목Key -> 실제값 (dry-run 표시용)
	broken := map[string]map[string]bool{}     // VM -> 항목Key -> FAIL/설정없음 여부

	plan := &Plan{}

	for _, f := range findings {
		if _, seen := expected[f.VM]; !seen {
			expected[f.VM] = map[string]string{}
			actual[f.VM] = map[string]string{}
			broken[f.VM] = map[string]bool{}
			order = append(order, f.VM)
		}
		expected[f.VM][f.Key] = f.Expected
		actual[f.VM][f.Key] = f.Actual

		if f.Result != "FAIL" && f.Result != "설정없음" {
			continue
		}
		if fixable(f.Key) {
			broken[f.VM][f.Key] = true
		} else {
			plan.Manual = append(plan.Manual, f)
		}
	}

	for _, vmName := range order {
		b := broken[vmName]
		if len(b) == 0 {
			continue
		}
		info, ok := infoByName[vmName]
		if !ok {
			return nil, fmt.Errorf("VM %q의 vCenter 조회 정보가 없습니다 — 체크 결과와 조회 결과가 어긋났습니다", vmName)
		}

		fix := VMFix{VM: vmName, Group: GroupOf(vmName), Vcenter: info.VCenter, Ref: info.Ref}
		exp, act := expected[vmName], actual[vmName]

		// 1) 고급 설정(ExtraConfig) — 고정값 4종 + 토폴로지 2종
		pinNeeded := false
		for _, key := range advancedOrder {
			if !b[key] {
				continue
			}
			fix.addExtra("고급설정", key, act[key], exp[key])
			if memPinTriggers[key] {
				pinNeeded = true
			}
		}
		if pinNeeded {
			// 체크 대상이 아니라 From/To 비교가 무의미하므로 현재값은 비워둔다.
			fix.addExtra("고급설정", memPinKey, "", "TRUE")
		}

		// 2) affinity — vcpu 번호 순으로 정렬해서 기록(sched.vcpu2 < sched.vcpu10)
		for _, key := range sortedAffinityKeys(b) {
			fix.addExtra("고급설정", key, act[key], exp[key])
		}

		// 3) CPU 토폴로지 UI 필드.
		//    vSphere는 vCPU 수가 소켓당 코어 수로 나누어떨어져야 Reconfigure를 받아주므로,
		//    둘 중 하나만 틀렸더라도 두 값을 기대값 조합으로 함께 맞춘다.
		if b[KeyNumCPU] || b[KeyCoresPerSock] {
			cpu, err := intExpect(vmName, exp, KeyNumCPU)
			if err != nil {
				return nil, err
			}
			cores, err := intExpect(vmName, exp, KeyCoresPerSock)
			if err != nil {
				return nil, err
			}
			if cores <= 0 || cpu%cores != 0 {
				return nil, fmt.Errorf("%s: vCPU 기대값(%d)이 소켓당 코어 수 기대값(%d)으로 나누어떨어지지 않아 교정할 수 없습니다 — -cpu/-cores 값을 확인하세요", vmName, cpu, cores)
			}
			fix.setCPU(int32(cpu), act[KeyNumCPU])
			fix.setCoresPerSocket(int32(cores), act[KeyCoresPerSock])
		}

		if b[KeyNumaCoresNode] {
			numa, err := intExpect(vmName, exp, KeyNumaCoresNode)
			if err != nil {
				return nil, err
			}
			if numa <= 0 {
				return nil, fmt.Errorf("%s: NUMA 노드당 코어 수 기대값이 %d 입니다 — -numa 값을 확인하세요", vmName, numa)
			}
			fix.setCoresPerNumaNode(int32(numa), act[KeyNumaCoresNode])

			// 나누어떨어지지 않으면 NUMA 노드 크기가 불균등해진다. vSphere가 거부하는 조합은
			// 아니라서 중단하지는 않고, 사용자가 의도한 값인지 확인할 수 있게 경고만 남긴다.
			if cpu, err := intExpect(vmName, exp, KeyNumCPU); err == nil && cpu%numa != 0 {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("%s: vCPU(%d)가 NUMA 노드당 코어 수(%d)로 나누어떨어지지 않습니다 — NUMA 노드 크기가 불균등해집니다", vmName, cpu, numa))
			}
		}

		plan.Fixes = append(plan.Fixes, fix)
	}

	return plan, nil
}

func (f *VMFix) addExtra(where, key, from, to string) {
	f.ExtraConfig = append(f.ExtraConfig, &types.OptionValue{Key: key, Value: to})
	f.Changes = append(f.Changes, Change{Where: where, Key: key, From: from, To: to})
}

func (f *VMFix) setCPU(v int32, from string) {
	f.NumCPUs = &v
	f.Changes = append(f.Changes, Change{Where: "CPU 토폴로지", Key: "vCPU 수(NumCPUs)", From: from, To: strconv.Itoa(int(v))})
}

func (f *VMFix) setCoresPerSocket(v int32, from string) {
	f.NumCoresPerSocket = &v
	f.Changes = append(f.Changes, Change{Where: "CPU 토폴로지", Key: "소켓당 코어 수(NumCoresPerSocket)", From: from, To: strconv.Itoa(int(v))})
}

func (f *VMFix) setCoresPerNumaNode(v int32, from string) {
	f.CoresPerNumaNode = &v
	f.Changes = append(f.Changes, Change{Where: "CPU 토폴로지", Key: "NUMA 노드당 코어 수(CoresPerNumaNode)", From: from, To: strconv.Itoa(int(v))})
}

// sortedAffinityKeys는 broken 중 affinity 키만 vcpu 번호 오름차순으로 돌려준다.
// 문자열 정렬로는 sched.vcpu10이 sched.vcpu2보다 앞에 와서 로그가 읽기 어려워진다.
func sortedAffinityKeys(broken map[string]bool) []string {
	var keys []string
	for k := range broken {
		if affinityKeyRe.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return affinityIndex(keys[i]) < affinityIndex(keys[j]) })
	return keys
}

func affinityIndex(key string) int {
	m := affinityKeyRe.FindStringSubmatch(key)
	n, _ := strconv.Atoi(m[1])
	return n
}

// intExpect는 해당 VM의 항목Key 기대값을 정수로 읽는다. 기대값이 아예 없다는 건 체크가
// 그 항목을 건너뛰었다는 뜻이라(그룹 옵션 미지정 등) 교정 근거가 없으므로 에러로 알린다.
func intExpect(vmName string, exp map[string]string, key string) (int, error) {
	raw, ok := exp[key]
	if !ok {
		return 0, fmt.Errorf("%s: %q 기대값이 체크 결과에 없어 교정할 수 없습니다 — 해당 그룹 기대값 옵션을 주고 다시 실행하세요", vmName, key)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %q 기대값(%q)이 정수가 아닙니다", vmName, key, raw)
	}
	return n, nil
}

// GroupOf는 VM 이름으로 ev01/ev02/ev03 그룹을 판정한다 (vm-param-check의 classifyGroup과 동일 규칙).
func GroupOf(vmName string) string {
	switch {
	case containsFold(vmName, "ev01"):
		return "ev01"
	case containsFold(vmName, "ev02"):
		return "ev02"
	case containsFold(vmName, "ev03"):
		return "ev03"
	default:
		return "기타"
	}
}
