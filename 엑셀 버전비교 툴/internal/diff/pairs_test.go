package diff

import (
	"slices"
	"testing"

	"excel-diff/internal/reader"
)

// 이름이 대소문자만 다른 시트는 짝짓되, 정확히 같은 이름이 우선이다.
func TestDefaultPairsIgnoreCase(t *testing.T) {
	book := func(names ...string) *reader.Workbook {
		w := &reader.Workbook{}
		for _, n := range names {
			w.Sheets = append(w.Sheets, reader.Sheet{Name: n})
		}
		return w
	}
	for _, c := range []struct {
		old, new *reader.Workbook
		want     []SheetPair
	}{
		{book("Data", "Gone"), book("DATA", "Add"), []SheetPair{{Old: "Data", New: "DATA"}, {New: "Add"}, {Old: "Gone"}}},
		{book("data", "Data"), book("DATA", "Data"), []SheetPair{{Old: "data", New: "DATA"}, {Old: "Data", New: "Data"}}},
	} {
		if got := DefaultPairs(c.old, c.new); !slices.Equal(got, c.want) {
			t.Error(got, c.want)
		}
	}
}
