// export.go: 스펙 파일을 VM 생성/설정 도구(VMsetup)가 쓸 수 있는 형태로 정리해서 내보낸다.
//
// VMsetup의 vm_setup.sh가 SPEC_DIR 스펙으로 vm_create/affinity/lpage 등을 호출할 때,
// 스펙 파서를 bash로 다시 만들지 않고 이 도구(-specExport)가 이미 검증된 파서로 읽은 값을
// "이름=값" 줄로 넘겨준다. ev01의 이름 없는 키(cpu/mem/disk/cores/numa)는 -ev01을 붙여
// ev01~ev99이 같은 모양이 되도록 정규화한다.
package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"vm-param-check/model"
)

// ev01만 접미사 없이 쓰는 키들. 나머지 그룹은 "<키>-evNN" 형태다.
var ev01BareKeys = map[string]bool{"cores": true, "numa": true, "cpu": true, "mem": true, "disk": true}

// createRequiredKinds는 VM을 만들려면 그룹마다 반드시 있어야 하는 값이다.
var createRequiredKinds = []string{"cpu", "mem", "disk", "shares"}

// ExportLine은 내보낼 값 1개다(Name은 정규화된 이름, 예: cpu-ev01).
type ExportLine struct {
	Name  string
	Value string
}

// ExportSpec은 매칭된 스펙을 VM 생성용으로 정규화해서 돌려준다.
//
// 규칙(계획서 D4/D5):
//   - ev01은 필수이고 cpu/mem/disk/shares가 전부 있어야 한다
//   - ev02~ev99은 값이 하나라도 있으면 "정의된 그룹"이고, 그러면 cpu/mem/disk/shares가 전부 있어야 한다
//   - 정의된 그룹은 ev01부터 연속이어야 한다(예: ev02 없이 ev03만 있으면 에러)
//
// 반환값의 첫 줄은 "groups=<정의된 그룹 수>", 둘째 줄은 "specdir=<매칭된 스펙 폴더 절대경로>"이고,
// affinity 상대경로는 스펙 폴더 기준 절대경로로 바꾼다.
func ExportSpec(m *SpecMatch) ([]ExportLine, error) {
	values := map[string]string{}
	var order []string
	for _, opt := range m.Options {
		name := opt.Name
		if ev01BareKeys[name] {
			name += "-ev01"
		}
		if strings.HasPrefix(name, "affinity-") && !filepath.IsAbs(opt.Value) {
			abs, err := filepath.Abs(filepath.Join(m.SpecDir, opt.Value))
			if err != nil {
				return nil, err
			}
			opt.Value = abs
		}
		if _, dup := values[name]; !dup {
			order = append(order, name)
		}
		values[name] = opt.Value
	}

	defined := map[string]bool{}
	for name := range values {
		if i := strings.LastIndex(name, "-ev"); i >= 0 {
			if g := model.ClassifyGroup(name[i+1:]); g == name[i+1:] {
				defined[g] = true
			}
		}
	}

	groups := 0
	for i, g := range model.GroupNames() {
		if !defined[g] {
			continue
		}
		if i != groups {
			return nil, fmt.Errorf("%s: %s 값이 있는데 %s 값이 없습니다 — ev 번호는 ev01부터 연속이어야 합니다",
				m.SpecFile, g, model.GroupName(groups+1))
		}
		var missing []string
		for _, kind := range createRequiredKinds {
			if values[kind+"-"+g] == "" {
				missing = append(missing, kind+"-"+g)
			}
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("%s: %s 을(를) 만들려면 %s 값이 필요합니다", m.SpecFile, g, strings.Join(missing, ", "))
		}
		groups++
	}
	if groups == 0 {
		return nil, fmt.Errorf("%s: ev01 값(cpu/mem/disk/shares-ev01)이 없습니다 — ev01은 필수입니다", m.SpecFile)
	}

	specDirAbs, err := filepath.Abs(m.SpecDir)
	if err != nil {
		return nil, err
	}
	out := []ExportLine{{Name: "groups", Value: fmt.Sprint(groups)}, {Name: "specdir", Value: specDirAbs}}
	for _, name := range order {
		out = append(out, ExportLine{Name: name, Value: values[name]})
	}
	return out, nil
}
