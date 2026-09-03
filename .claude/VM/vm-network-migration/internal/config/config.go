// Package config 는 이 도구가 쓰는 입력 파일들과 vCenter 자격증명을 읽습니다.
//
// 입력 파일은 모두 "한 줄에 한 항목, 빈 줄과 # 주석은 무시" 규약을 따릅니다.
// (vm-param-check / vswitch_setting-source 와 같은 규약)
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"vm-network-migration/internal/color"
)

// LoadLines 는 목록 파일을 읽어 의미 있는 줄만 돌려줍니다.
func LoadLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, sc.Err()
}

// dedup 은 순서를 유지하면서 중복을 제거하고, 제거한 개수를 함께 돌려줍니다.
func dedup(in []string) ([]string, int) {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	removed := 0
	for _, v := range in {
		if seen[v] {
			removed++
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, removed
}

// LoadVCenters 는 vcenter.txt 를 읽어 vCenter 주소 목록을 만듭니다.
// 한 줄에 IP 또는 FQDN 하나씩 적습니다. 계정 정보는 환경변수로 받습니다.
func LoadVCenters(path string) ([]string, error) {
	lines, err := LoadLines(path)
	if err != nil {
		return nil, err
	}
	out, removed := dedup(lines)
	if len(out) == 0 {
		return nil, fmt.Errorf("%s 에 유효한 vCenter 주소가 없습니다", path)
	}
	if removed > 0 {
		fmt.Printf("%s %s: 중복 주소 %d건 제거\n", color.Cyan("[INFO]"), path, removed)
	}
	return out, nil
}

// LoadVMList 는 {user}.txt 를 읽어 마이그레이션 대상 VM 이름 목록을 만듭니다.
func LoadVMList(path string) ([]string, error) {
	lines, err := LoadLines(path)
	if err != nil {
		return nil, err
	}
	out, removed := dedup(lines)
	if len(out) == 0 {
		return nil, fmt.Errorf("%s 에 대상 VM 이 없습니다", path)
	}
	if removed > 0 {
		fmt.Printf("%s %s: 중복 VM %d건 제거\n", color.Cyan("[INFO]"), path, removed)
	}
	return out, nil
}

// WorkEntry 는 vswitch_{user}.txt 한 줄입니다.
//
// 1번 컬럼은 VM 이름이 아니라 BM(ESXi 호스트) 이름입니다.
// 포트그룹은 호스트의 표준 vSwitch 위에 만들어지는 것이므로 생성 단위가 호스트이고,
// 어떤 VM 이 어느 포트그룹으로 갈지는 "그 VM 이 올라가 있는 호스트"로 결정됩니다.
type WorkEntry struct {
	BMHost string // ESXi 호스트 이름 (vCenter 인벤토리에 등록된 이름과 같아야 함)
	PGName string // 새로 만들 포트그룹 이름
	VlanID int32  // VLAN ID (0 이면 태깅 없음)
}

// LoadWorklist 는 vswitch_{user}.txt 를 읽습니다.
// 형식: <BM호스트명> <포트그룹명> <VLAN ID>  (공백 구분)
func LoadWorklist(path string) ([]WorkEntry, error) {
	lines, err := LoadLines(path)
	if err != nil {
		return nil, err
	}

	var out []WorkEntry
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) < 3 {
			return nil, fmt.Errorf("%s %d번째 항목 형식 오류(컬럼 3개 필요): %q", path, i+1, line)
		}
		vlan, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, fmt.Errorf("%s %d번째 항목 VLAN ID 가 숫자가 아닙니다: %q", path, i+1, f[2])
		}
		if vlan < 0 || vlan > 4094 {
			return nil, fmt.Errorf("%s %d번째 항목 VLAN ID 범위를 벗어났습니다(0~4094): %d", path, i+1, vlan)
		}
		out = append(out, WorkEntry{BMHost: f[0], PGName: f[1], VlanID: int32(vlan)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s 에 유효한 항목이 없습니다", path)
	}
	return out, nil
}

// TargetForHost 는 BM 호스트 하나에 대응하는 포트그룹 항목을 찾습니다.
//
// 한 호스트에 여러 줄이 적혀 있으면 "이 호스트의 VM 을 어느 포트그룹으로 옮길지"가
// 확정되지 않으므로 에러로 돌려줍니다. 포트그룹을 여러 개 만들어야 하는 경우라면
// 생성(Step 2)은 여러 줄 그대로 처리되지만, 이관 대상 VM 이 있는 호스트는
// 한 줄만 남겨야 합니다.
func TargetForHost(entries []WorkEntry, host string) (WorkEntry, error) {
	var found []WorkEntry
	for _, e := range entries {
		if strings.EqualFold(e.BMHost, host) {
			found = append(found, e)
		}
	}
	switch len(found) {
	case 0:
		return WorkEntry{}, fmt.Errorf("BM 호스트 %q 에 대한 항목이 worklist 에 없습니다", host)
	case 1:
		return found[0], nil
	default:
		names := make([]string, 0, len(found))
		for _, e := range found {
			names = append(names, e.PGName)
		}
		return WorkEntry{}, fmt.Errorf("BM 호스트 %q 에 항목이 %d개(%s) 있어 이관 대상 포트그룹을 특정할 수 없습니다",
			host, len(found), strings.Join(names, ", "))
	}
}

// Password 는 vCenter 비밀번호를 환경변수에서 읽습니다.
//
// 계정 ID 는 -id 플래그로 받고(기본 lscsystems@vsphere.local), 비밀번호만 환경변수로
// 받습니다. 비밀번호를 명령행에 적으면 셸 히스토리와 프로세스 목록(ps)에 남기 때문입니다.
// 목록의 모든 vCenter 에 같은 계정을 씁니다.
//
//	VC_PASSWORD              (기본)
//	VC_PASS / VCENTER_PASS   (대체)
func Password() (string, error) {
	pass := firstNonEmpty(os.Getenv("VC_PASSWORD"), os.Getenv("VC_PASS"), os.Getenv("VCENTER_PASS"))
	if pass == "" {
		return "", fmt.Errorf("vCenter 비밀번호가 없습니다. 환경변수 VC_PASSWORD 를 설정하세요")
	}
	return pass, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
