// del.go: 스펙 파일의 "검사·설정 제외" 옵션(all_del_affinity=y, ev02_del_lpage=y 등)을 읽는다.
//
// 어떤 ev 그룹은 affinity 나 lpage 를 일부러 쓰지 않는다. 그 그룹을 점검에서 빼려면
// 스펙에 아래 4가지 형태를 적는다(값은 y 만 허용, 범위·n 없음, 여러 ev 는 여러 줄).
//
//	all_del_affinity=y   all_del_lpage=y      -> 스펙의 모든 ev 그룹
//	evNN_del_affinity=y  evNN_del_lpage=y     -> 해당 ev 그룹만 (NN = 01~99)
package config

import (
	"fmt"
	"strings"

	"vm-param-check/model"
)

// 제외 대상 종류 접미사.
const (
	delAffinitySuffix = "_del_affinity"
	delLpageSuffix    = "_del_lpage"
)

// IsDelOption은 스펙 옵션 이름이 *_del_affinity / *_del_lpage 계열인지 알려준다.
// 접두어(all 또는 evNN)가 올바른지는 DelGroups 가 검증한다 — 오타도 "알 수 없는 플래그"가
// 아니라 정확한 오류 메시지로 알리기 위해, 여기서는 all/ev* 접두어만 느슨하게 본다.
func IsDelOption(name string) bool {
	for _, suffix := range []string{delAffinitySuffix, delLpageSuffix} {
		if prefix, ok := strings.CutSuffix(name, suffix); ok {
			return prefix == "all" || strings.HasPrefix(prefix, "ev")
		}
	}
	return false
}

// DelGroups는 옵션 목록에서 affinity / lpage 제외 그룹을 골라 돌려준다(키는 "ev01".."ev99").
// all_* 는 groups 개수만큼의 모든 그룹으로 펼친다. 값이 y(대소문자 무관)가 아니거나,
// evNN 의 NN 이 올바르지 않거나 groups 를 넘으면 에러다. 해당 옵션이 없으면 빈 맵을 돌려준다.
func DelGroups(opts []SpecOption, groups int) (aff, lp map[string]bool, err error) {
	aff = map[string]bool{}
	lp = map[string]bool{}
	for _, opt := range opts {
		if !IsDelOption(opt.Name) {
			continue
		}
		if !strings.EqualFold(opt.Value, "y") {
			return nil, nil, fmt.Errorf("%s 의 값은 y 만 쓸 수 있습니다 (받은 값: %q)", opt.Name, opt.Value)
		}
		target := aff
		prefix, ok := strings.CutSuffix(opt.Name, delAffinitySuffix)
		if !ok {
			target = lp
			prefix = strings.TrimSuffix(opt.Name, delLpageSuffix)
		}
		if prefix == "all" {
			for i := 1; i <= groups; i++ {
				target[model.GroupName(i)] = true
			}
			continue
		}
		if model.ClassifyGroup(prefix) != prefix {
			return nil, nil, fmt.Errorf("%s: ev 번호가 올바르지 않습니다 (ev01~ev99 형태여야 합니다)", opt.Name)
		}
		if indexOfGroup(prefix) >= groups {
			return nil, nil, fmt.Errorf("%s: %s 은(는) 이 스펙에 정의된 그룹(ev01~%s)에 없습니다", opt.Name, prefix, model.GroupName(groups))
		}
		target[prefix] = true
	}
	return aff, lp, nil
}

// indexOfGroup은 "evNN" 의 0부터 시작하는 순번을 돌려준다(올바른 이름일 때만 호출).
func indexOfGroup(g string) int {
	for i, name := range model.GroupNames() {
		if name == g {
			return i
		}
	}
	return -1
}

// SpecGroupCount는 스펙 옵션에 값이 정의된 ev 그룹 수(가장 큰 evNN 의 NN)를 돌려준다.
// ExportSpec 이 그룹 수를 정하는 방식과 같다 — "<종류>-evNN" 꼴 옵션 이름의 evNN 을 본다.
func SpecGroupCount(opts []SpecOption) int {
	n := 0
	for _, opt := range opts {
		if i := strings.LastIndex(opt.Name, "-ev"); i >= 0 {
			g := opt.Name[i+1:]
			if model.ClassifyGroup(g) == g {
				if idx := indexOfGroup(g) + 1; idx > n {
					n = idx
				}
			}
		}
	}
	return n
}
