// Package verify 는 확인자가 붙여넣은 작업 대상 목록을 파싱하고, 작업자가 등록한 대상과 대조한다(계획서 3-2).
//
// 규칙:
//   - Parse: 줄바꿈(\n, \r)·탭·스페이스를 모두 구분자로 보고, 빈 조각은 버린다(순서는 입력 그대로, 중복도 그대로 남긴다).
//   - Compare: 집합 비교. 순서는 상관없고 대소문자는 구분한다. 어느 쪽이든 같은 이름이 두 번 이상 나오면 불일치(Duplicate),
//     붙여넣은 목록(또는 기대 목록)이 비어 있으면 절대 일치가 아니다(Empty).
//
// 결과에는 이름이 들어 있지만, 데몬은 확인자에게 이름을 돌려주지 않고 개수만 알린다(확인자는 대상을 볼 수 없다).
package verify

import (
	"sort"
	"strings"
)

// Parse 는 붙여넣은 글을 이름 목록으로 나눈다. 구분자는 \n \r \t 스페이스이고 빈 조각은 버린다.
func Parse(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
}

// Result 는 Compare 의 결과. 이름 목록은 정렬되어 있다.
type Result struct {
	Missing   []string // want 에는 있는데 붙여넣은 목록(got)에 없는 이름
	Extra     []string // got 에는 있는데 want 에 없는 이름
	Duplicate []string // want 또는 got 안에서 두 번 이상 나온 이름(한 번씩만)
	Empty     bool     // want 또는 got 이 비어 있음
}

// OK 는 완전히 일치하는지(빠짐·남음·중복 없음, 비어 있지 않음).
func (r Result) OK() bool {
	return !r.Empty && len(r.Missing) == 0 && len(r.Extra) == 0 && len(r.Duplicate) == 0
}

// Compare 는 want(작업자가 등록한 대상)와 got(확인자가 붙여넣은 대상)을 집합으로 비교한다.
func Compare(want, got []string) Result {
	var r Result
	if len(want) == 0 || len(got) == 0 {
		r.Empty = true
	}
	ws, wd := count(want)
	gs, gd := count(got)
	dup := map[string]bool{}
	for _, n := range wd {
		dup[n] = true
	}
	for _, n := range gd {
		dup[n] = true
	}
	for n := range ws {
		if !gs[n] {
			r.Missing = append(r.Missing, n)
		}
	}
	for n := range gs {
		if !ws[n] {
			r.Extra = append(r.Extra, n)
		}
	}
	for n := range dup {
		r.Duplicate = append(r.Duplicate, n)
	}
	sort.Strings(r.Missing)
	sort.Strings(r.Extra)
	sort.Strings(r.Duplicate)
	return r
}

// count 는 목록을 집합으로 만들고, 두 번 이상 나온 이름을 따로 돌려준다.
func count(list []string) (map[string]bool, []string) {
	set := make(map[string]bool, len(list))
	var dups []string
	seenDup := map[string]bool{}
	for _, n := range list {
		if set[n] {
			if !seenDup[n] {
				seenDup[n] = true
				dups = append(dups, n)
			}
			continue
		}
		set[n] = true
	}
	return set, dups
}
