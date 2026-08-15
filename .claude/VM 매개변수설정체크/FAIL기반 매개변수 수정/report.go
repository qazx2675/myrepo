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

// GlobalExpect는 CSV 전체에서 복원한, vm-param-check 실행 시 쓰였던 전역 기대값이다.
// (cpu/cores/numa/mem/disk는 계획서 3-4상 모든 VM 공통이라 어느 행에서 뽑아도 동일해야 한다.)
type GlobalExpect struct {
	CPU, Cores, Numa, MemGB, DiskGB int
	SharesEV01                      int
	SharesEV02                      *int
	HTOn                            bool
	HasAffinityIssue, HasLpageIssue, HasPowerIssue bool
}

// extractGlobalExpect는 CSV 전체(FAIL 여부 무관)를 훑어서 위 값들을 뽑고, 같은 Key가
// 서로 다른 기대값을 갖고 있으면(=vm-param-check를 서로 다른 옵션으로 여러 번 돌린 CSV를
// 섞어 쓴 경우) 즉시 에러로 중단한다 — "동일한 설정의 그룹" 전제 자체가 깨진 것이므로.
func extractGlobalExpect(rows []Row) (GlobalExpect, error) {
	var g GlobalExpect
	seen := map[string]string{}
	take := func(key string) (string, bool) {
		for _, r := range rows {
			if r.Key == key {
				if prev, ok := seen[key]; ok && prev != r.Expected {
					return "", false
				}
				seen[key] = r.Expected
				return r.Expected, true
			}
		}
		return "", false
	}
	assertConsistent := func(key string) error {
		var expected string
		found := false
		for _, r := range rows {
			if r.Key != key {
				continue
			}
			if !found {
				expected = r.Expected
				found = true
				continue
			}
			if r.Expected != expected {
				return fmt.Errorf("CSV 안에서 %q의 기대값이 VM마다 다릅니다(%q vs %q) — 서로 다른 조건으로 체크한 결과를 섞어 쓴 것으로 보입니다", key, expected, r.Expected)
			}
		}
		return nil
	}

	for _, key := range []string{"config.hardware.numCPU", "cpuid.coresPerSocket", "numa.vcpu.maxPerVirtualNode",
		"config.hardware.memoryMB (GB 환산)", "disk total capacity (GB 환산, 반올림)"} {
		if err := assertConsistent(key); err != nil {
			return g, err
		}
	}

	if v, ok := take("config.hardware.numCPU"); ok {
		g.CPU, _ = strconv.Atoi(v)
	}
	if v, ok := take("cpuid.coresPerSocket"); ok {
		g.Cores, _ = strconv.Atoi(v)
	}
	if v, ok := take("numa.vcpu.maxPerVirtualNode"); ok {
		g.Numa, _ = strconv.Atoi(v)
	}
	if v, ok := take("config.hardware.memoryMB (GB 환산)"); ok {
		g.MemGB, _ = strconv.Atoi(v)
	}
	if v, ok := take("disk total capacity (GB 환산, 반올림)"); ok {
		g.DiskGB, _ = strconv.Atoi(v)
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
