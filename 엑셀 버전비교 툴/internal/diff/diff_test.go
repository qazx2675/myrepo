package diff

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"excel-diff/internal/reader"
)

// sheet 는 격자로 시트를 만든다. "" 는 빈 셀, "?" 는 확인 불가 셀이다.
func sheet(name string, grid [][]string) reader.Sheet {
	s := reader.Sheet{Name: name, Cols: map[int]reader.Col{}}
	for r, line := range grid {
		row := reader.Row{Num: r + 1}
		for c, v := range line {
			switch v {
			case "":
				continue
			case "?":
				row.Cells = append(row.Cells, reader.Cell{Col: c + 1, Key: "\x00unknown", Display: "값 확인 불가", Unknown: true})
			default:
				row.Cells = append(row.Cells, reader.Cell{Col: c + 1, Key: v, Display: v})
			}
		}
		if len(row.Cells) > 0 {
			s.Rows = append(s.Rows, row)
		}
	}
	return s
}

func book(sheets ...reader.Sheet) *reader.Workbook { return &reader.Workbook{Sheets: sheets} }

// lines 는 "a,b,c" 형태의 행들을 격자로 만든다.
func lines(rows ...string) (g [][]string) {
	for _, r := range rows {
		g = append(g, strings.Split(r, ","))
	}
	return
}

func run(t testing.TB, old, new [][]string, key ...int) SheetDiff {
	t.Helper()
	a, b := book(sheet("S", old)), book(sheet("S", new))
	p := DefaultPairs(a, b)
	if len(key) == 2 {
		p[0].OldKeyCol, p[0].NewKeyCol = key[0], key[1]
	}
	res, err := Compare(a, b, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res.Sheets[0]
}

// describe 는 상태가 같지 않은 행·열과 셀 변경을 짧게 적는다.
func describe(sd SheetDiff) string {
	status := []string{"=", "~", "+", "-", ">"}
	var out []string
	for _, c := range sd.Cols {
		if c.Status != StatusSame {
			out = append(out, fmt.Sprintf("C%s%d:%d", status[c.Status], c.Old, c.New))
		}
	}
	num := func(r *reader.Row) int {
		if r == nil {
			return 0
		}
		return r.Num
	}
	for _, r := range sd.Rows {
		if r.Status != StatusSame {
			out = append(out, fmt.Sprintf("R%s%d:%d", status[r.Status], num(r.Old), num(r.New)))
		}
	}
	kind := []string{"", "m", "a", "d", "u"}
	for _, ch := range sd.Changes {
		r, c := sd.Rows[ch.Row], sd.Cols[ch.Col]
		if r.Old == nil || r.New == nil || c.Old == 0 || c.New == 0 {
			continue // 행·열 상태로 알 수 있다
		}
		out = append(out, fmt.Sprintf("%s%d.%d", kind[ch.Kind], r.New.Num, c.New))
	}
	return strings.Join(out, " ")
}

func TestScenarios(t *testing.T) {
	base := lines("id,name,qty", "1,apple,10", "2,banana,20", "3,cherry,30", "4,date,40", "5,elder,50")
	cases := []struct {
		name     string
		old, new [][]string
		want     string
	}{
		{"same", base, base, ""},
		{"modify", base, lines("id,name,qty", "1,apple,10", "2,banana,21", "3,cherry,", "4,date,40", "5,elder,50"),
			"R~3:3 R~4:4 m3.3 d4.3"},
		{"row insert", base, lines("id,name,qty", "9,new,90", "1,apple,10", "2,banana,20", "3,cherry,30", "4,date,40", "5,elder,50"),
			"R+0:2"},
		{"row delete", base, lines("id,name,qty", "2,banana,20", "3,cherry,30", "4,date,40", "5,elder,50"), "R-2:0"},
		{"blank row insert", base, lines("id,name,qty", "1,apple,10", ",,", "2,banana,20", "3,cherry,30", "4,date,40", "5,elder,50"), ""},
		{"row move", base, lines("id,name,qty", "2,banana,20", "3,cherry,30", "4,date,40", "5,elder,50", "1,apple,10"), "R>2:6"},
		{"row move and modify", base, lines("id,name,qty", "2,banana,20", "3,cherry,30", "4,date,40", "5,elder,50", "1,apple,11"),
			"R>2:6 m6.3"},
		{"column insert", base, lines("id,new,name,qty", "1,x,apple,10", "2,y,banana,20", "3,z,cherry,30", "4,w,date,40", "5,v,elder,50"),
			"C+0:2"},
		{"empty column insert", base, lines("id,,name,qty", "1,,apple,10", "2,,banana,20", "3,,cherry,30", "4,,date,40", "5,,elder,50"), ""},
		{"column delete", base, lines("id,qty", "1,10", "2,20", "3,30", "4,40", "5,50"), "C-2:0"},
		{"column move", base, lines("qty,id,name", "10,1,apple", "20,2,banana", "30,3,cherry", "40,4,date", "50,5,elder"), "C>3:1"},
		{"header only", base, lines("id,title,qty", "1,apple,10", "2,banana,20", "3,cherry,30", "4,date,40", "5,elder,50"),
			"R~1:1 m1.2"},
		{"column cleared", base, lines("id,,qty", "1,,10", "2,,20", "3,,30", "4,,40", "5,,50"),
			"R~1:1 R~2:2 R~3:3 R~4:4 R~5:5 R~6:6 d1.2 d2.2 d3.2 d4.2 d5.2 d6.2"},
		{"column all changed", base, lines("id,label,qty", "1,A,10", "2,B,20", "3,C,30", "4,D,40", "5,E,50"),
			"R~1:1 R~2:2 R~3:3 R~4:4 R~5:5 R~6:6 m1.2 m2.2 m3.2 m4.2 m5.2 m6.2"},
		{"all cells changed", lines("a,b", "c,d", "e,f"), lines("g,h", "i,j", "k,l"),
			"R~1:1 R~2:2 R~3:3 m1.1 m1.2 m2.1 m2.2 m3.1 m3.2"},
		{"unknown both sides", lines("h,v", "1,?"), lines("h,v", "1,?"), "R~2:2 u2.2"},
		{"empty column filled", lines("id,,qty", "1,,10", "2,,20"), lines("id,memo,qty", "1,x,10", "2,y,20"),
			"R~1:1 R~2:2 R~3:3 a1.2 a2.2 a3.2"},
		{"duplicate rows", lines("h", "x", "x", "x", "y"), lines("h", "x", "x", "y"), "R-4:0"},
	}
	for _, c := range cases {
		if got := describe(run(t, c.old, c.new)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMostlyChangedLowCardinality(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 4))
	grid := func(n int) (g [][]string) {
		for range n {
			var l []string
			for range 40 {
				l = append(l, []string{"Y", "N"}[rng.IntN(2)])
			}
			g = append(g, l)
		}
		return
	}
	s := run(t, grid(2000), grid(2000)).Summary
	if s.RowsAdded > 100 || s.RowsDeleted > 100 || s.RowsMovedModified > 0 || s.ColsAdded+s.ColsDeleted+s.ColsMoved > 0 {
		t.Fatalf("summary %+v", s)
	}
}

func TestColumnsSurviveRowChurn(t *testing.T) {
	old := [][]string{{"a", "b", "c"}}
	for i := range 200 {
		old = append(old, []string{fmt.Sprint("a", i), fmt.Sprint("b", i), fmt.Sprint("c", i)})
	}
	new := [][]string{{"c", "a", "extra", "b"}}
	for i := 299; i >= 50; i-- { // 삭제 + 추가 + 역순 정렬
		new = append(new, []string{fmt.Sprint("c", i), fmt.Sprint("a", i), fmt.Sprint("e", i), fmt.Sprint("b", i)})
	}
	sd := run(t, old, new)
	want := []ColPair{{Old: 3, New: 1, Status: StatusMoved}, {Old: 1, New: 2}, {New: 3, Status: StatusAdded}, {Old: 2, New: 4}}
	if !slices.Equal(sd.Cols, want) {
		t.Fatalf("cols %+v", sd.Cols)
	}
	s := sd.Summary
	if s.RowsDeleted+s.RowsModified != 50 || s.RowsMoved != 149 || s.RowsMovedModified != 0 {
		t.Fatalf("summary %+v", s)
	}
}

func TestLayoutOrder(t *testing.T) {
	sd := run(t, lines("h,i", "a,1", "b,2", "c,3", "d,4"), lines("h,i", "d,4", "a,1", "n,9", "c,3"))
	var got []string
	for _, r := range sd.Rows {
		o, n := 0, 0
		if r.Old != nil {
			o = r.Old.Num
		}
		if r.New != nil {
			n = r.New.Num
		}
		got = append(got, fmt.Sprintf("%d:%d", o, n))
	}
	// d 는 이동하여 최신 위치에, b 와 n 은 같은 구간이므로 위치 짝(수정)
	if want := "1:1 5:2 2:3 3:4 4:5"; strings.Join(got, " ") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestKeyColumn(t *testing.T) {
	old := lines("id,v", "1,a", "2,b", "3,c", ",x", "7,d", "7,e")
	new := lines("id,v", "1,a", "4,b", "3,z", ",x", "7,d", "7,e")
	a, b := book(sheet("S", old)), book(sheet("S", new))
	res, err := Compare(a, b, []SheetPair{{Old: "S", New: "S", OldKeyCol: 1, NewKeyCol: 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := describe(res.Sheets[0]), "R-3:0 R+0:3 R~4:4 m4.2"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if len(res.Warnings) != 2 {
		t.Errorf("warnings %v", res.Warnings)
	}
	if _, err := Compare(a, b, []SheetPair{{Old: "S", New: "S", OldKeyCol: 1}}, nil); err == nil {
		t.Error("한쪽 키 열만 지정하면 오류여야 한다")
	}
}

func TestSheets(t *testing.T) {
	hidden := sheet("keep", lines("a"))
	hidden.Hidden = true
	a := book(sheet("keep", lines("a")), sheet("gone", lines("g,h")), sheet("before", lines("r")))
	b := book(hidden, sheet("fresh", lines("f,?")), sheet("after", lines("r")))
	a.Warnings = []reader.Warning{{Message: "w"}}

	pairs := DefaultPairs(a, b)
	want := []SheetPair{{Old: "keep", New: "keep"}, {New: "fresh"}, {New: "after"}, {Old: "gone"}, {Old: "before"}}
	if !slices.Equal(pairs, want) {
		t.Fatalf("pairs %+v", pairs)
	}
	last := -1
	res, err := Compare(a, b, []SheetPair{{Old: "keep", New: "keep"}, {Old: "before", New: "after"}, {New: "fresh"}, {Old: "gone"}},
		func(done, total int) {
			if done <= last || done > total {
				t.Errorf("progress %d after %d", done, last)
			}
			last = done
		})
	if err != nil {
		t.Fatal(err)
	}
	s := res.Summary
	if s != (Summary{SheetsAdded: 1, SheetsDeleted: 1, SheetsRenamed: 1, RowsAdded: 1, RowsDeleted: 1,
		ColsAdded: 2, ColsDeleted: 2, SameCells: 2, AddedCells: 1, DeletedCells: 2, UnknownCells: 1}) {
		t.Errorf("summary %+v", s)
	}
	if last != progressTotal || res.Warnings[0].Message != "[이전 파일] w" || !res.Sheets[0].New.Hidden {
		t.Error("progress/warning/hidden")
	}
	res, err = Compare(a, b, pairs[:1], nil)
	if err != nil || len(res.ExcludedOld) != 2 || len(res.ExcludedNew) != 2 {
		t.Errorf("excluded %v %+v", err, res)
	}
	if _, err := Compare(a, b, []SheetPair{{Old: "keep"}, {Old: "keep"}}, nil); err == nil {
		t.Error("같은 시트를 두 번 지정하면 오류여야 한다")
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	a := book(sheet("S", lines("h,i", "a,1", "b,2")))
	b := book(sheet("S", lines("h,i", "a,9", "b,2")))
	fresh := func() *Result {
		r, err := Compare(a, b, DefaultPairs(a, b), nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for name, spoil := range map[string]func(*Result){
		"변경 누락":  func(r *Result) { r.Sheets[0].Changes = nil },
		"변경 중복":  func(r *Result) { r.Sheets[0].Changes = append(r.Sheets[0].Changes, r.Sheets[0].Changes[0]) },
		"행 누락":   func(r *Result) { r.Sheets[0].Rows = r.Sheets[0].Rows[:2] },
		"행 중복":   func(r *Result) { r.Sheets[0].Rows = append(r.Sheets[0].Rows, r.Sheets[0].Rows[2]) },
		"짝 뒤바뀜":  func(r *Result) { x := r.Sheets[0].Rows; x[0].New, x[2].New = x[2].New, x[0].New },
		"열 누락":   func(r *Result) { r.Sheets[0].Cols = r.Sheets[0].Cols[:1] },
		"시트 누락":  func(r *Result) { r.Sheets, r.Summary = nil, Summary{} },
		"건수 불일치": func(r *Result) { r.Summary.SameCells++ },
	} {
		r := fresh()
		spoil(r)
		if Verify(a, b, r) == nil {
			t.Errorf("%s 를 잡지 못함", name)
		}
	}
}

// TestPlanted 는 고유 ID 데이터에 심은 변경이 정확히 보고되는지 확인한다.
func TestPlanted(t *testing.T) {
	for seed := range uint64(600) {
		rng := rand.New(rand.NewPCG(seed, 1))
		useKey := seed%2 == 0
		nCols, nRows := 6+rng.IntN(15), 20+rng.IntN(300)
		serial := 0
		fresh := func() string { serial++; return fmt.Sprint("v", serial) }

		type record struct {
			id    string
			cells map[string]string // 열 머리글 -> 값
		}
		var oldHeads []string
		for c := range nCols {
			oldHeads = append(oldHeads, fmt.Sprint("h", c))
		}
		makeRow := func(heads []string) record {
			r := record{id: fresh(), cells: map[string]string{}}
			r.cells[oldHeads[0]] = r.id
			for _, h := range heads {
				if h != oldHeads[0] && (rng.IntN(10) > 0 || h == oldHeads[1] || h == oldHeads[2]) {
					r.cells[h] = fresh()
				}
			}
			return r
		}
		var oldRows []record
		for range nRows {
			oldRows = append(oldRows, makeRow(oldHeads))
		}

		// 열: 0~2 는 남기고 삭제·추가·이동을 하나씩
		newHeads := slices.Clone(oldHeads)
		want := map[string]bool{}
		var deletedCol, addedCol, movedCol string
		if rng.IntN(2) == 0 {
			i := 3 + rng.IntN(len(newHeads)-3)
			deletedCol = newHeads[i]
			newHeads = slices.Delete(newHeads, i, i+1)
		}
		if rng.IntN(2) == 0 {
			i := rng.IntN(len(newHeads) + 1)
			addedCol = "added"
			newHeads = slices.Insert(newHeads, i, addedCol)
			if deletedCol != "" {
				// 삭제와 추가가 같은 자리면 "수정"으로 보는 것이 맞으므로 머리글·값이 달라도 짝지어질 수 있다.
				if i == slices.Index(oldHeads, deletedCol) {
					newHeads = slices.Delete(newHeads, i, i+1)
					addedCol = ""
				}
			}
		}
		if rng.IntN(2) == 0 && deletedCol == "" && addedCol == "" {
			i := 3 + rng.IntN(len(newHeads)-3)
			h := newHeads[i]
			newHeads = slices.Delete(newHeads, i, i+1)
			j := rng.IntN(len(newHeads) + 1)
			newHeads = slices.Insert(newHeads, j, h)
			if j != i {
				movedCol = h
			}
		}

		// 행: 5행 구간마다 가운데에서 한 가지 작업
		var newRows []record
		var carried []record // 이동할 행
		moves := 0
		for start := 0; start < len(oldRows); start += 5 {
			seg := oldRows[start:min(start+5, len(oldRows))]
			op := rng.IntN(7)
			for i, r := range seg {
				n := record{r.id, map[string]string{}}
				for _, h := range newHeads {
					if v, ok := r.cells[h]; ok {
						n.cells[h] = v
					}
				}
				if addedCol != "" {
					n.cells[addedCol] = fresh()
				}
				change := func() {
					h := newHeads[rng.IntN(len(newHeads))]
					if h == oldHeads[0] || h == addedCol {
						h = oldHeads[1]
					}
					_, had := n.cells[h]
					switch {
					case had && rng.IntN(4) == 0 && h != oldHeads[1] && h != oldHeads[2]:
						delete(n.cells, h)
					default:
						n.cells[h] = fresh()
					}
				}
				mid := i == 2
				switch {
				case mid && op == 0: // 삭제
					continue
				case mid && op == 1: // 추가
					newRows = append(newRows, n, makeRow(newHeads))
					continue
				case mid && op == 2: // 수정
					change()
				case mid && op == 3 && moves < 5: // 뒤로 이동(가끔 값 수정)
					if rng.IntN(2) == 0 {
						change()
					}
					carried = append(carried, n)
					moves++
					continue
				case mid && op == 4 && len(carried) > 0:
					newRows = append(newRows, n, carried[0])
					carried = carried[1:]
					continue
				}
				newRows = append(newRows, n)
			}
		}
		newRows = append(newRows, carried...)

		grid := func(heads []string, rows []record) [][]string {
			g := [][]string{heads}
			for _, r := range rows {
				line := make([]string, len(heads))
				for c, h := range heads {
					line[c] = r.cells[h]
				}
				g = append(g, line)
			}
			return g
		}
		og, ng := grid(oldHeads, oldRows), grid(newHeads, newRows)

		// 기대값
		byID := map[string]record{}
		for _, r := range oldRows {
			byID[r.id] = r
		}
		inNew := map[string]bool{}
		var order []weighted
		for _, r := range newRows {
			inNew[r.id] = true
			o, ok := byID[r.id]
			if !ok {
				want["row+ "+r.id] = true
				continue
			}
			order = append(order, weighted{slices.IndexFunc(oldRows, func(x record) bool { return x.id == r.id }), len(order), 1})
			for _, h := range newHeads {
				if h == addedCol || h == deletedCol {
					continue
				}
				if a, b := o.cells[h], r.cells[h]; a != b {
					want[fmt.Sprintf("cell %s %s %q %q", r.id, h, a, b)] = true
				}
			}
		}
		for _, r := range oldRows {
			if !inNew[r.id] {
				want["row- "+r.id] = true
			}
		}
		for name, h := range map[string]string{"col+ ": addedCol, "col- ": deletedCol, "col> ": movedCol} {
			if h != "" {
				want[name+h] = true
			}
		}
		wantMoved := len(order) - len(chain(order))

		var key []int
		if useKey {
			key = []int{1, slices.Index(newHeads, oldHeads[0]) + 1}
		}
		sd := run(t, og, ng, key...)

		got := map[string]bool{}
		text := func(c *reader.Cell) string {
			if c == nil {
				return ""
			}
			return c.Display
		}
		id := func(r *reader.Row, g [][]string, col int) string { return g[r.Num-1][col] }
		newID := slices.Index(newHeads, oldHeads[0])
		for _, r := range sd.Rows {
			switch {
			case r.Old == nil:
				got["row+ "+id(r.New, ng, newID)] = true
			case r.New == nil:
				got["row- "+id(r.Old, og, 0)] = true
			case id(r.Old, og, 0) != id(r.New, ng, newID):
				got["mispaired "+id(r.Old, og, 0)+" "+id(r.New, ng, newID)] = true
			}
		}
		var movedHeads []string
		for _, c := range sd.Cols {
			switch c.Status {
			case StatusAdded:
				got["col+ "+ng[0][c.New-1]] = true
			case StatusDeleted:
				got["col- "+og[0][c.Old-1]] = true
			case StatusMoved:
				movedHeads = append(movedHeads, og[0][c.Old-1])
			}
		}
		// 이동 열은 어느 쪽을 이동으로 보든 최소 개수이면 된다.
		if movedCol != "" {
			p, q := slices.Index(oldHeads, movedCol), slices.Index(newHeads, movedCol)
			if len(movedHeads) == 1 && (movedHeads[0] == movedCol || abs(p-q) == 1) {
				got["col> "+movedCol] = true
			}
		} else if len(movedHeads) > 0 {
			got[fmt.Sprint("col> ", movedHeads)] = true
		}
		for _, ch := range sd.Changes {
			r, c := sd.Rows[ch.Row], sd.Cols[ch.Col]
			if r.Old != nil && r.New != nil && c.Old > 0 && c.New > 0 {
				got[fmt.Sprintf("cell %s %s %q %q", id(r.New, ng, newID), ng[0][c.New-1], text(ch.Old), text(ch.New))] = true
			}
		}
		bad := sd.Summary.RowsMoved != wantMoved
		var diffs []string
		for k := range want {
			if !got[k] {
				diffs = append(diffs, "누락 "+k)
			}
		}
		for k := range got {
			if !want[k] {
				diffs = append(diffs, "오검출 "+k)
			}
		}
		if bad || len(diffs) > 0 {
			t.Fatalf("seed %d key=%v cols(+%q -%q >%q) moved %d want %d: %v", seed, useKey, addedCol, deletedCol, movedCol,
				sd.Summary.RowsMoved, wantMoved, diffs[:min(len(diffs), 12)])
		}
	}
}

// TestChaos 는 중복·빈 칸·확인 불가가 섞인 임의 변형에서 자체 검증이 통과하는지 본다.
func TestChaos(t *testing.T) {
	for seed := range uint64(3000) {
		rng := rand.New(rand.NewPCG(seed, 2))
		domain := 1 + rng.IntN(30)
		value := func() string {
			switch rng.IntN(12) {
			case 0, 1:
				return ""
			case 2:
				if rng.IntN(5) == 0 {
					return "?"
				}
			}
			return fmt.Sprint(rng.IntN(domain))
		}
		line := func(n int) []string {
			l := make([]string, n)
			for i := range l {
				l[i] = value()
			}
			return l
		}
		cols := 1 + rng.IntN(8)
		var old [][]string
		for range rng.IntN(60) {
			old = append(old, line(cols))
		}
		var new [][]string
		for _, l := range old {
			new = append(new, slices.Clone(l))
		}
		width := cols
		for range rng.IntN(30) {
			switch op := rng.IntN(8); {
			case op == 0 && len(new) > 0:
				i := rng.IntN(len(new))
				new = slices.Delete(new, i, i+1)
			case op == 1:
				new = slices.Insert(new, rng.IntN(len(new)+1), line(width))
			case op == 2 && len(new) > 0:
				i := rng.IntN(len(new))
				l := new[i]
				new = slices.Delete(new, i, i+1)
				new = slices.Insert(new, rng.IntN(len(new)+1), l)
			case op == 3 && len(new) > 0 && width > 0:
				new[rng.IntN(len(new))][rng.IntN(width)] = value()
			case op == 4:
				at, empty := rng.IntN(width+1), rng.IntN(2) == 0
				for i := range new {
					v := ""
					if !empty {
						v = value()
					}
					new[i] = slices.Insert(new[i], at, v)
				}
				width++
			case op == 5 && width > 0:
				at := rng.IntN(width)
				for i := range new {
					new[i] = slices.Delete(new[i], at, at+1)
				}
				width--
			case op == 6 && width > 1:
				a, b := rng.IntN(width), rng.IntN(width)
				for i := range new {
					new[i][a], new[i][b] = new[i][b], new[i][a]
				}
			case op == 7:
				rng.Shuffle(len(new), func(i, j int) { new[i], new[j] = new[j], new[i] })
			}
		}
		var key []int
		if seed%3 == 0 {
			key = []int{1 + rng.IntN(cols), 1 + rng.IntN(max(width, 1))}
		}
		run(t, old, new, key...) // Compare 가 Verify 를 실행한다
		if key != nil {
			key[1] = key[0]
		}
		if sd := run(t, old, old, key...); len(sd.Changes) > 0 {
			for _, ch := range sd.Changes {
				if ch.Kind != CellUnknown {
					t.Fatalf("seed %d: 같은 파일에서 변경 %+v", seed, ch)
				}
			}
		}
		same := run(t, old, old, key...).Summary
		if same.RowsAdded+same.RowsDeleted+same.RowsMoved+same.ColsAdded+same.ColsDeleted+same.ColsMoved != 0 {
			t.Fatalf("seed %d: 같은 파일 %+v", seed, same)
		}
	}
}

// TestIdentityColumns 는 키 열을 지정하지 않아도 값이 고유한 열(자동 식별 열)로 행을 바르게 짝짓는지 본다.
func TestIdentityColumns(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	serial := 0
	row := func() []string {
		serial++
		l := []string{"", fmt.Sprint("ID", serial), fmt.Sprint("n", rng.IntN(1_000_000))}
		for range 5 {
			l = append(l, fmt.Sprint(rng.IntN(5)))
		}
		return l
	}
	table := func(n int) [][]string {
		g := [][]string{{"No", "ID", "name", "a", "b", "c", "d", "e"}}
		for range n {
			g = append(g, row())
		}
		return g
	}
	clone := func(g [][]string) (out [][]string) {
		for _, l := range g {
			out = append(out, slices.Clone(l))
		}
		return
	}
	number := func(g [][]string) [][]string { // 순번 열: 행을 넣고 빼면 다시 매겨진다
		for i := 1; i < len(g); i++ {
			g[i][0] = fmt.Sprint(i)
		}
		return g
	}
	insert := func(g [][]string, at, n int) [][]string {
		for range n {
			g = slices.Insert(g, at, row())
		}
		return g
	}
	check := func(name string, old, new [][]string, added, deleted, modified int) {
		t.Helper()
		sd := run(t, old, new)
		mispaired := 0
		for _, r := range sd.Rows {
			if r.Old != nil && r.New != nil && old[r.Old.Num-1][1] != new[r.New.Num-1][1] {
				mispaired++
			}
		}
		s := sd.Summary
		if mispaired != 0 || s.RowsAdded != added || s.RowsDeleted != deleted || s.RowsMoved != 0 ||
			modified >= 0 && s.RowsModified != modified {
			t.Errorf("%s: 짝 어긋남 %d, 추가 %d/%d, 삭제 %d/%d, 수정 %d/%d, 이동 %d",
				name, mispaired, s.RowsAdded, added, s.RowsDeleted, deleted, s.RowsModified, modified, s.RowsMoved)
		}
	}

	// 같은 자리에서 행 삭제 + 무관한 행 추가: 수정이 아니라 삭제·추가
	old := number(table(60))
	new := clone(old)
	new[30] = row()
	new[30][0] = old[30][0]
	check("같은 자리 교체", old, new, 1, 1, 0)

	// 삭제 행과 멀리 추가된 행이 한 식별 열(name)에서만 우연히 같은 값: 짝짓지 않는다
	new = clone(old)
	new = slices.Insert(new, 50, row())
	new[50][2] = old[10][2]
	new = number(slices.Delete(new, 10, 11))
	check("우연히 같은 값", old, new, 1, 1, -1)

	// ID 오타만 고친 행은 수정
	new = clone(old)
	new[30][1] += "-오타"
	sd := run(t, old, new)
	if s := sd.Summary; s.RowsModified != 1 || s.RowsAdded+s.RowsDeleted+s.RowsMoved != 0 {
		t.Errorf("ID 오타: %+v", s)
	}

	// ID 외 모든 값 변경 + 행 삽입·삭제 + 순번 열 다시 매김(큰 구간): 순번이 아니라 ID 로 짝짓는다
	old = number(table(300))
	new = clone(old)
	new = slices.Delete(new, 100, 105)
	new = insert(new, 200, 3)
	new = insert(new, 1, 7)
	for _, l := range new[1:] {
		l[2] = "x" + l[2]
		for c := 3; c < len(l); c++ {
			l[c] = fmt.Sprint(5 + rng.IntN(5))
		}
	}
	check("ID 외 전부 변경 + 순번", old, number(new), 10, 5, 295)

	// 내용은 거의 같고 순번만 다시 매겨진 경우
	new = clone(old)
	new = slices.Delete(new, 150, 152)
	new = insert(new, 1, 3)
	for i := range 10 {
		new[20+i*20][3+i%5] = "변경"
	}
	check("순번 다시 매김", old, number(new), 3, 2, -1)

	// 식별 열이 없으면(ID 까지 모두 변경) 위치 순서대로 수정
	new = clone(old)
	for _, l := range new[1:] {
		for c := range l {
			l[c] = "Z" + l[c]
		}
	}
	if s := run(t, old, new).Summary; s.RowsAdded+s.RowsDeleted+s.RowsMoved != 0 || s.RowsModified != 300 {
		t.Errorf("식별 열 없음: %+v", s)
	}
}
