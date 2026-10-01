// tags.go: 스펙 파일의 tag-* 줄(VM 사용자 지정 특성 값)을 ev 별 값으로 펼친다.
//
// 형식(태그당 한 줄):
//
//	tag-DEPT_NAME=영업팀                    값만 쓰면 모든 ev 에 적용
//	tag-PURPOSE=ev01:DB,ev02-ev03:WAS       evNN:값 또는 evNN-evMM:값 을 콤마로 나열
//	tag-VM_TYPE=가상서버,ev04:테스트          앞에서 정한 값을 뒤 항목이 덮어쓴다
//
// 이 값은 체크(vm-param-check)에는 쓰이지 않는다 — vm_setup.sh 의 태그 설정 단계(tag_setting)만 읽는다.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// TagNames는 설정할 수 있는 사용자 지정 특성(tag_setting 이 쓰는 이름)이다.
var TagNames = map[string]bool{"DEPT_NAME": true, "PURPOSE": true, "VM_TYPE": true}

// IsTagOption은 스펙 옵션 이름이 tag-* 인지 알려준다.
func IsTagOption(name string) bool { return strings.HasPrefix(name, "tag-") }

// ExpandTag는 raw 를 ev01..ev<groups> 순서의 값 목록으로 펼친다(값이 없는 ev 는 빈 문자열).
func ExpandTag(raw string, groups int) ([]string, error) {
	out := make([]string, groups)
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return nil, fmt.Errorf("빈 항목이 있습니다 (콤마가 겹치거나 끝에 있음)")
		}
		sel, val, hasSel := strings.Cut(item, ":")
		if !hasSel {
			sel, val = "", item
		}
		val = strings.TrimSpace(val)
		if val == "" || strings.Contains(val, ":") {
			return nil, fmt.Errorf("항목 %q 의 값이 비었거나 ':' 가 두 번 이상 들어 있습니다", item)
		}
		from, to := 1, groups
		if hasSel {
			a, b, isRange := strings.Cut(strings.TrimSpace(sel), "-")
			var err error
			if from, err = parseEv(a); err != nil {
				return nil, fmt.Errorf("항목 %q: %v", item, err)
			}
			to = from
			if isRange {
				if to, err = parseEv(b); err != nil {
					return nil, fmt.Errorf("항목 %q: %v", item, err)
				}
			}
			if from > to {
				return nil, fmt.Errorf("항목 %q: 범위가 거꾸로입니다", item)
			}
			if to > groups {
				return nil, fmt.Errorf("항목 %q: ev%02d 는 이 스펙의 ev 개수(%d)를 넘습니다", item, to, groups)
			}
		}
		for i := from; i <= to; i++ {
			out[i-1] = val
		}
	}
	return out, nil
}

func parseEv(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if !strings.HasPrefix(s, "ev") {
		return 0, fmt.Errorf("%q 는 evNN 형식이 아닙니다", s)
	}
	n, err := strconv.Atoi(s[2:])
	if err != nil || n < 1 || n > 99 {
		return 0, fmt.Errorf("%q 는 ev01~ev99 가 아닙니다", s)
	}
	return n, nil
}
