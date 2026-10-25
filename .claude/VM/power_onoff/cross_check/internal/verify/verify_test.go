package verify

import (
	"reflect"
	"testing"
)

func TestParseSeparators(t *testing.T) {
	cases := map[string][]string{
		"":                                 nil,
		"   \n\t\r\n ":                     nil,
		"a":                                {"a"},
		"a\nb\nc":                          {"a", "b", "c"},
		"a\r\nb\r\n":                       {"a", "b"},
		"a\tb  c":                          {"a", "b", "c"},
		" svr1ev01\tsvr1ev02 \n\nsvr2ev01": {"svr1ev01", "svr1ev02", "svr2ev01"},
		"x x":                              {"x", "x"}, // 중복은 Parse 가 지우지 않는다(Compare 가 판단)
	}
	for in, want := range cases {
		got := Parse(in)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Parse(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCompare(t *testing.T) {
	want := []string{"svr1ev01", "svr1ev02", "svr2ev01"}
	cases := []struct {
		name string
		got  []string
		ok   bool
		r    Result
	}{
		{"같음(순서 다름)", []string{"svr2ev01", "svr1ev01", "svr1ev02"}, true, Result{}},
		{"빠짐", []string{"svr1ev01", "svr1ev02"}, false, Result{Missing: []string{"svr2ev01"}}},
		{"남음", []string{"svr1ev01", "svr1ev02", "svr2ev01", "svr9ev99"}, false, Result{Extra: []string{"svr9ev99"}}},
		{"대소문자 구분", []string{"SVR1EV01", "svr1ev02", "svr2ev01"}, false,
			Result{Missing: []string{"svr1ev01"}, Extra: []string{"SVR1EV01"}}},
		{"중복", []string{"svr1ev01", "svr1ev02", "svr2ev01", "svr1ev01"}, false, Result{Duplicate: []string{"svr1ev01"}}},
		{"비어 있음", nil, false, Result{Missing: []string{"svr1ev01", "svr1ev02", "svr2ev01"}, Empty: true}},
	}
	for _, c := range cases {
		r := Compare(want, c.got)
		if r.OK() != c.ok {
			t.Errorf("%s: OK()=%v want %v (%+v)", c.name, r.OK(), c.ok, r)
		}
		if !reflect.DeepEqual(norm(r), norm(c.r)) {
			t.Errorf("%s: %+v want %+v", c.name, r, c.r)
		}
	}
}

func TestCompareEdge(t *testing.T) {
	if Compare(nil, nil).OK() {
		t.Fatal("둘 다 비면 일치가 아님")
	}
	if Compare(nil, []string{"a"}).OK() {
		t.Fatal("기대 목록이 비면 일치가 아님")
	}
	// 기대 목록 쪽 중복도 불일치
	r := Compare([]string{"a", "a"}, []string{"a"})
	if r.OK() || !reflect.DeepEqual(r.Duplicate, []string{"a"}) {
		t.Fatalf("기대 목록 중복: %+v", r)
	}
	if !Compare(Parse("b a"), Parse("a\nb\n")).OK() {
		t.Fatal("Parse 결과끼리 같은 집합이면 일치")
	}
}

func norm(r Result) Result {
	if len(r.Missing) == 0 {
		r.Missing = nil
	}
	if len(r.Extra) == 0 {
		r.Extra = nil
	}
	if len(r.Duplicate) == 0 {
		r.Duplicate = nil
	}
	return r
}
