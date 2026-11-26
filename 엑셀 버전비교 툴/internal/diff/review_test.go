package diff

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"excel-diff/internal/reader"
)

func sheetOf(rows [][]string) []reader.Row {
	var out []reader.Row
	for i, r := range rows {
		row := reader.Row{Num: i + 1}
		for c, v := range r {
			row.Cells = append(row.Cells, reader.Cell{Col: c + 1, Key: v, Display: v})
		}
		out = append(out, row)
	}
	return out
}

func alignRows(old, new [][]string, key int) *aligner {
	var cols []ColPair
	for c := 1; c <= len(old[0]); c++ {
		cols = append(cols, ColPair{Old: c, New: c})
	}
	a := newAligner(sheetOf(old), sheetOf(new), cols, interner{})
	if key > 0 {
		a.matchKeys(key, key, "S", "S")
	}
	a.align(func(int) {})
	return a
}

// 한쪽에서만 유일하고 상대 쪽에서 중복인 키의 행은 자동 정렬로 짝지어야 한다.
func TestKeyDuplicatedOnOtherSide(t *testing.T) {
	old := [][]string{{"id", "name", "v"}, {"A", "apple", "1"}, {"B", "banana", "2"}}
	new := [][]string{{"id", "name", "v"}, {"A", "apple", "1"}, {"A", "apple copy", "3"}, {"B", "banana", "2"}}
	for _, c := range []struct {
		name     string
		old, new [][]string
		mate     []int
	}{
		{"최신 중복", old, new, []int{0, 1, 3}},
		{"이전 중복", new, old, []int{0, 1, -1, 2}},
	} {
		if got := alignRows(c.old, c.new, 1).old.mate; !equalInts(got, c.mate) {
			t.Error(c.name, got, c.mate)
		}
	}
}

// 상대 파일에서 중복이라 키로 비교하지 않은 행도 경고에 나와야 한다.
func TestKeyWarningIncludesOtherSideDuplicate(t *testing.T) {
	old := sheetOf([][]string{{"id"}, {"A"}, {"B"}})
	new := sheetOf([][]string{{"id"}, {"A"}, {"A"}, {"B"}})
	w := newAligner(old, new, []ColPair{{Old: 1, New: 1}}, interner{}).matchKeys(1, 1, "S", "S")
	if len(w) != 2 || !strings.Contains(w[0].Message, "[이전 파일]") || !strings.Contains(w[0].Message, "1개 행") ||
		!strings.Contains(w[0].Message, "(행: 2)") || !strings.Contains(w[1].Message, "(행: 2, 3)") {
		t.Error(w)
	}
}

func equalInts(x, y []int) bool {
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// repeated 는 모든 값이 여러 번 나오는(식별 열·드문 값 없는) 시트와,
// 한 열이 전부 바뀌고 맨 위에 한 행이 삽입된 최신 시트를 만든다.
func repeated(rows, cols int) (old, new [][]string) {
	row := func(i int, last string) []string {
		r := make([]string, cols)
		for c, x := 0, i; c < cols-1; c++ { // 앞 열들은 i 의 자릿수(밑 7)
			if c < 6 {
				r[c], x = strconv.Itoa(x%7), x/7
			} else {
				r[c] = strconv.Itoa((i + c) % 5)
			}
		}
		r[cols-1] = last
		return r
	}
	new = append(new, row(rows+3, "new"))
	for i := range rows {
		old = append(old, row(i, "old"))
		new = append(new, row(i, "new"))
	}
	return old, new
}

// band 는 처음부터 끝까지 이어지는 경로를 항상 남겨야 한다.
func TestBandConnected(t *testing.T) {
	for _, s := range [][2]int{{1, 1}, {1, 100000}, {100000, 1}, {300, 301}, {301, 300}, {30000, 60000}, {60000, 30000}, {7, 1000000}, {200000, 200001}} {
		k, l := s[0], s[1]
		lo, hi := band(k, l)
		ok := lo(0) == 0 && hi(k-1) == l-1
		for i := 0; i < k; i++ {
			ok = ok && lo(i) <= hi(i) && (i == 0 || lo(i) >= lo(i-1) && hi(i) >= hi(i-1) && lo(i) <= hi(i-1)+1)
		}
		if !ok {
			t.Error("끊긴 band", k, l)
		}
	}
}

func TestInsertIntoLargeChangedGap(t *testing.T) {
	for _, size := range [][2]int{{300, 5}, {30000, 30}} {
		old, new := repeated(size[0], size[1])
		start := time.Now()
		a := alignRows(old, new, 0)
		t.Logf("%d행 %d열: %v", size[0], size[1], time.Since(start))
		wrong := 0
		for i, m := range a.old.mate {
			if m != i+1 {
				wrong++
			}
		}
		if wrong > 0 {
			t.Errorf("%d행: 잘못 짝지은 행 %d개", size[0], wrong)
		}
	}
}
