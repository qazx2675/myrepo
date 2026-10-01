package config

import (
	"reflect"
	"testing"
)

func TestExpandTag(t *testing.T) {
	cases := []struct {
		raw    string
		groups int
		want   []string
		bad    bool
	}{
		{"영업팀", 3, []string{"영업팀", "영업팀", "영업팀"}, false},
		{"ev01:DB,ev02-ev03:WAS", 3, []string{"DB", "WAS", "WAS"}, false},
		{"VM,ev04:TEST", 4, []string{"VM", "VM", "VM", "TEST"}, false},
		{"ev02:X", 3, []string{"", "X", ""}, false},
		{"ev01-ev99:A", 99, nil, false},
		{"ev05:X", 3, nil, true},
		{"ev03-ev02:X", 3, nil, true},
		{"ev01:", 3, nil, true},
		{"a,,b", 3, nil, true},
		{"x1:A", 3, nil, true},
	}
	for _, c := range cases {
		got, err := ExpandTag(c.raw, c.groups)
		if c.bad {
			if err == nil {
				t.Errorf("%q 는 오류여야 함", c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.raw, err)
			continue
		}
		if c.want != nil && !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q = %v, want %v", c.raw, got, c.want)
		}
		if len(got) != c.groups {
			t.Errorf("%q 길이 %d", c.raw, len(got))
		}
	}
}
