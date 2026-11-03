// pgkeys_test.go - PageUp/PageDown 키 파싱·스크롤
package main

import "testing"

func TestParseKeysPageUpDown(t *testing.T) {
	got := parseKeys([]byte("\x1b[5~\x1b[6~\x1b[A\x1b[3~x"))
	want := []keyEv{{k: kPgUp}, {k: kPgDn}, {k: kUp}, {k: kRune, r: 'x'}}
	if len(got) != len(want) {
		t.Fatalf("parseKeys: %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] %+v != %+v", i, got[i], want[i])
		}
	}
}

func TestChangeResultPageKeys(t *testing.T) {
	c := &changeState{scroll: 15, maxScroll: 30}
	c.resultKey(keyEv{k: kPgDn})
	if c.scroll != 25 {
		t.Fatalf("PgDn: %d", c.scroll)
	}
	c.resultKey(keyEv{k: kPgDn})
	if c.scroll != 30 {
		t.Fatalf("PgDn 끝: %d", c.scroll)
	}
	c.resultKey(keyEv{k: kPgUp})
	if c.scroll != 20 {
		t.Fatalf("PgUp: %d", c.scroll)
	}
}
