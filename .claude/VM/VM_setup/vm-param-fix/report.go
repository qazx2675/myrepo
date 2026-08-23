// report.go: vm-param-check가 낸 상세 CSV(result.csv)를 읽어서 FAIL/설정없음 항목에
// 태그(affinity/lpage/power/manual)를 붙이고, 원래 체크에 쓰인 전역 기대값(cpu/cores/numa 등)을
// CSV 자체에서 복원한다 — 사용자가 원래 vm-param-check를 어떤 플래그로 돌렸는지 다시 몰라도 되게.
package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Row는 vm-param-check 상세 CSV 한 줄(model.Finding과 동일한 컬럼).
type Row struct {
	VM       string
	Source   string
	Key      string
	Expected string
	Actual   string
	Result   string
	Note     string
}

var affinityKeyRe = regexp.MustCompile(`^sched\.vcpu\d+\.affinity$`)

var lpageKeys = map[string]bool{
	"sched.mem.lpage.enable1GPage":               true,
	"sched.mem.prealloc":                         true,
	"sched.mem.prealloc.pinnedMainMem":            true,
	"sched.swap.vmxSwapEnabled":                  true,
	"cpuid.coresPerSocket":                       true,
	"hardware.numCoresPerSocket (CPU 토폴로지 UI)":    true,
	"numa.vcpu.maxPerVirtualNode":                true,
	"config.numaInfo.coresPerNumaNode (CPU 토폴로지 UI)": true,
}

// Tag는 이 FAIL 항목을 어느 외부 교정 도구가 담당하는지를 나타낸다.
type Tag string

const (
	TagAffinity Tag = "affinity"
	TagLpage    Tag = "lpage"
	TagPower    Tag = "power"
	TagManual   Tag = "manual" // 자동교정 대상 아님 (vCPU/메모리/디스크/Shares/네트워크 등)
)

func classifyTag(r Row) Tag {
	switch {
	case affinityKeyRe.MatchString(r.Key):
		return TagAffinity
	case r.Key == "host power policy":
		return TagPower
	case lpageKeys[r.Key]:
		return TagLpage
	default:
		return TagManual
	}
}

func loadCSV(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rd := csv.NewReader(f)
	records, err := rd.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("빈 CSV 파일")
	}
	header := records[0]
	idx := map[string]int{}
	for i, h := range header {
		// vm-param-check가 Excel 호환용으로 붙이는 UTF-8 BOM(0xEF,0xBB,0xBF)이 첫 헤더 셀
		// 앞에 남아있을 수 있어 제거한다.
		idx[strings.TrimSpace(strings.TrimPrefix(h, "\xEF\xBB\xBF"))] = i
	}
	need := []string{"VM명", "소스", "항목Key", "기대값", "실제값", "결과", "비고"}
	for _, n := range need {
		if _, ok := idx[n]; !ok {
			return nil, fmt.Errorf("CSV 헤더에 %q 컬럼이 없습니다 (vm-param-check가 낸 상세 CSV가 맞는지 확인하세요)", n)
		}
	}

	var rows []Row
	for _, rec := range records[1:] {
		rows = append(rows, Row{
			VM:       rec[idx["VM명"]],
			Source:   rec[idx["소스"]],
			Key:      rec[idx["항목Key"]],
			Expected: rec[idx["기대값"]],
			Actual:   rec[idx["실제값"]],
			Result:   rec[idx["결과"]],
			Note:     rec[idx["비고"]],
		})
	}
	return rows, nil
}

// GlobalExpect는 CSV 전체에서 복원한, vm-param-check 실행 시 쓰였던 기대값이다.
// vm-param-check가 이제 ev02/ev03에 별도 기대값(-cores-ev02 등)을 받을 수 있어서,
// cpu/cores/numa/mem/disk는 더 이상 CSV 전체에 걸쳐 하나일 필요가 없다 — 대신 그룹별로만
// 일관돼야 한다(ev01 안에서는 다 같아야 하고, ev02 안에서도 다 같아야 하는 식).
// Base는 ev01+미분류(vm-param-check에서 항상 -cores 등으로 필수 체크됨) 기준값이고,
// EV02/EV03는 옵션(-cores-ev02 등)이 아예 없어서 CSV에 해당 항목이 없으면 nil.
type GlobalExpect struct {
	CPU, Cores, Numa, MemGB, DiskGB                       int
	CPUEV02, CoresEV02, NumaEV02, MemGBEV02, DiskGBEV02   *int
	CPUEV03, CoresEV03, NumaEV03, MemGBEV03, DiskGBEV03   *int
	SharesEV01                      int
	SharesEV02                      *int
	HTOn                            bool
	HasAffinityIssue, HasLpageIssue, HasPowerIssue bool
	HasEV03LpageIssue bool // ev03는 lpage_setting이 -ev03Cores/-ev03Numa를 지원하지 않아 별도 관리
}

// hwGroupOf는 VM 이름으로 하드웨어 기대값이 어느 묶음("base"=ev01+미분류 | "ev02" | "ev03")에
// 속하는지 정한다 — vm-param-check의 group 판정(ev01/미분류는 같은 -cores 등을 씀)과 맞춤.
func hwGroupOf(vmName string) string {
	switch groupOf(vmName) {
	case "ev02":
		return "ev02"
	case "ev03":
		return "ev03"
	default: // "ev01" 또는 "기타" — vm-param-check에서 동일한 -cores 등을 적용받음
		return "base"
	}
}

// extractGroupValue는 key에 해당하는 행 중 bucket("base"|"ev02"|"ev03")에 속한 것만 모아
// 정수 기대값 하나로 합친다. bucket 안에서 값이 서로 다르면(=서로 다른 조건으로 체크한 결과가
// 섞인 것) 에러. bucket에 해당 행이 아예 없으면 found=false(옵션이 안 주어졌던 것).
func extractGroupValue(rows []Row, key, bucket string) (value int, found bool, err error) {
	var expected string
	for _, r := range rows {
		if r.Key != key || hwGroupOf(r.VM) != bucket {
			continue
		}
		if !found {
			expected = r.Expected
			found = true
			continue
		}
		if r.Expected != expected {
			return 0, false, fmt.Errorf("CSV 안에서 %q(%s 그룹)의 기대값이 VM마다 다릅니다(%q vs %q) — 서로 다른 조건으로 체크한 결과를 섞어 쓴 것으로 보입니다", key, bucket, expected, r.Expected)
		}
	}
	if !found {
		return 0, false, nil
	}
	n, _ := strconv.Atoi(expected)
	return n, true, nil
}

// extractGlobalExpect는 CSV 전체(FAIL 여부 무관)를 훑어서 위 값들을 뽑는다.
func extractGlobalExpect(rows []Row) (GlobalExpect, error) {
	var g GlobalExpect

	requiredKeys := []string{"config.hardware.numCPU", "cpuid.coresPerSocket", "numa.vcpu.maxPerVirtualNode",
		"config.hardware.memoryMB (GB 환산)", "disk total capacity (GB 환산, 반올림)"}
	for _, key := range requiredKeys {
		if _, found, err := extractGroupValue(rows, key, "base"); err != nil {
			return g, err
		} else if !found {
			return g, fmt.Errorf("CSV에 %q 항목이 전혀 없습니다 — vm-param-check의 상세(detail) CSV가 맞는지, 요약(summary) CSV를 잘못 넣은 건 아닌지 확인하세요", key)
		}
	}
	// ev02/ev03는 옵션이라 CSV에 아예 없을 수 있음 — 있으면 그룹 내 일관성만 에러로 잡는다.
	for _, key := range []string{"config.hardware.numCPU", "cpuid.coresPerSocket", "numa.vcpu.maxPerVirtualNode",
		"config.hardware.memoryMB (GB 환산)", "disk total capacity (GB 환산, 반올림)"} {
		for _, bucket := range []string{"ev02", "ev03"} {
			if _, _, err := extractGroupValue(rows, key, bucket); err != nil {
				return g, err
			}
		}
	}

	if v, found, _ := extractGroupValue(rows, "config.hardware.numCPU", "base"); found {
		g.CPU = v
	}
	if v, found, _ := extractGroupValue(rows, "cpuid.coresPerSocket", "base"); found {
		g.Cores = v
	}
	if v, found, _ := extractGroupValue(rows, "numa.vcpu.maxPerVirtualNode", "base"); found {
		g.Numa = v
	}
	if v, found, _ := extractGroupValue(rows, "config.hardware.memoryMB (GB 환산)", "base"); found {
		g.MemGB = v
	}
	if v, found, _ := extractGroupValue(rows, "disk total capacity (GB 환산, 반올림)", "base"); found {
		g.DiskGB = v
	}

	assignEV := func(key, bucket string, dst **int) {
		if v, found, _ := extractGroupValue(rows, key, bucket); found {
			vv := v
			*dst = &vv
		}
	}
	assignEV("config.hardware.numCPU", "ev02", &g.CPUEV02)
	assignEV("cpuid.coresPerSocket", "ev02", &g.CoresEV02)
	assignEV("numa.vcpu.maxPerVirtualNode", "ev02", &g.NumaEV02)
	assignEV("config.hardware.memoryMB (GB 환산)", "ev02", &g.MemGBEV02)
	assignEV("disk total capacity (GB 환산, 반올림)", "ev02", &g.DiskGBEV02)
	assignEV("config.hardware.numCPU", "ev03", &g.CPUEV03)
	assignEV("cpuid.coresPerSocket", "ev03", &g.CoresEV03)
	assignEV("numa.vcpu.maxPerVirtualNode", "ev03", &g.NumaEV03)
	assignEV("config.hardware.memoryMB (GB 환산)", "ev03", &g.MemGBEV03)
	assignEV("disk total capacity (GB 환산, 반올림)", "ev03", &g.DiskGBEV03)

	for _, r := range rows {
		if lpageKeys[r.Key] && (r.Result == "FAIL" || r.Result == "설정없음") && groupOf(r.VM) == "ev03" {
			g.HasEV03LpageIssue = true
		}
	}

	for _, r := range rows {
		if r.Key == "cpuAllocation.shares (CPU Shares Ratio)" {
			v, _ := strconv.Atoi(r.Expected)
			switch r.Source {
			case "ev01":
				g.SharesEV01 = v
			case "ev02":
				vv := v
				g.SharesEV02 = &vv
			}
		}
		if affinityKeyRe.MatchString(r.Key) {
			g.HasAffinityIssue = g.HasAffinityIssue || r.Result == "FAIL" || r.Result == "설정없음"
			if strings.Contains(r.Expected, ",") {
				g.HTOn = true
			}
		}
		if lpageKeys[r.Key] && (r.Result == "FAIL" || r.Result == "설정없음") {
			g.HasLpageIssue = true
		}
		if r.Key == "host power policy" && (r.Result == "FAIL" || r.Result == "설정없음") {
			g.HasPowerIssue = true
		}
	}

	hasEV01Row := false
	hasEV01Shares := false
	for _, r := range rows {
		if strings.Contains(r.VM, "ev01") {
			hasEV01Row = true
		}
		if r.Key == "cpuAllocation.shares (CPU Shares Ratio)" && r.Source == "ev01" {
			hasEV01Shares = true
		}
	}
	if hasEV01Row && !hasEV01Shares {
		return g, fmt.Errorf("CSV에 ev01 VM은 있는데 ev01 Shares 항목(cpuAllocation.shares)이 없습니다 — 상세 CSV가 맞는지 확인하세요")
	}

	return g, nil
}

// baseHostname은 VM 이름에서 ev01/ev02/ev03 접미사를 제거해 "BM 기준 호스트명"을 복원한다.
// affinity_setting/lpage_setting은 이 base hostname을 worklist로 받아서 자기가 직접
// ev01/ev02 접미사를 붙이는 방식이라, 우리가 반대로 붙은 접미사를 떼어내야 한다.
func baseHostname(vmName string) string {
	for _, suf := range []string{"ev01", "ev02", "ev03"} {
		if strings.Contains(vmName, suf) {
			return strings.Replace(vmName, suf, "", 1)
		}
	}
	return vmName
}

// hostFromNote는 "host power policy" Finding의 비고 컬럼("host=esxi01" 형식)에서
// 실제 ESXi 물리 호스트 이름을 뽑는다.
func hostFromNote(note string) (string, bool) {
	const prefix = "host="
	if strings.HasPrefix(note, prefix) {
		return strings.TrimPrefix(note, prefix), true
	}
	return "", false
}
