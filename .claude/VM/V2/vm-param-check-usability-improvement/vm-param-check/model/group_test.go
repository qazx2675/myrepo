package model

import "testing"

func TestClassifyGroup(t *testing.T) {
	cases := map[string]string{
		"host01ev01":  "ev01",
		"HOST01EV03":  "ev03",
		"host01ev04":  "ev04",
		"host01ev10":  "ev10",
		"host01":      "",
		"host01ev11":  "ev11",
		"host01ev99":  "ev99",
		"host01ev00":  "", // ev00은 없는 번호
		"bm-ev09.lab": "ev09",
	}
	for name, want := range cases {
		if got := ClassifyGroup(name); got != want {
			t.Errorf("ClassifyGroup(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestGroupNames(t *testing.T) {
	names := GroupNames()
	if len(names) != MaxGroup || names[0] != "ev01" || names[MaxGroup-1] != "ev99" {
		t.Errorf("GroupNames() = %v", names)
	}
}
