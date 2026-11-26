package describe

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"excel-diff/internal/diff"
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

func lines(rows ...string) (g [][]string) {
	for _, r := range rows {
		g = append(g, strings.Split(r, ","))
	}
	return
}

func compare(t testing.TB, a, b *reader.Workbook, pairs []diff.SheetPair) (*diff.Result, []Item) {
	t.Helper()
	if pairs == nil {
		pairs = diff.DefaultPairs(a, b)
	}
	res, err := diff.Compare(a, b, pairs, nil)
	if err != nil {
		t.Fatal(err)
	}
	items := Describe(res, pairs)
	check(t, res, items)
	return res, items
}

func texts(items []Item) (out []string) {
	for _, it := range items {
		out = append(out, it.Text)
	}
	return
}

func expect(t *testing.T, items []Item, want ...string) {
	t.Helper()
	if got := texts(items); !slices.Equal(got, want) {
		t.Errorf("설명\n got: %q\nwant: %q", got, want)
	}
}

func table(n int, edit func(r int, row []string)) [][]string {
	g := [][]string{{"서버", "IP", "상태", "비고", "담당자"}}
	for r := 2; r <= n; r++ {
		row := []string{fmt.Sprintf("web%02d", r), fmt.Sprintf("10.0.1.%d", r), "대기", fmt.Sprintf("memo%d", r), fmt.Sprintf("p%d", r)}
		if edit != nil {
			edit(r, row)
		}
		g = append(g, row)
	}
	return g
}

func diffTables(t *testing.T, old, new [][]string, key ...int) []Item {
	t.Helper()
	a, b := book(sheet("S", old)), book(sheet("S", new))
	pairs := diff.DefaultPairs(a, b)
	if len(key) == 2 {
		pairs[0].OldKeyCol, pairs[0].NewKeyCol = key[0], key[1]
	}
	_, items := compare(t, a, b, pairs)
	return items
}

func TestRange(t *testing.T) {
	old := table(20, nil)
	new := table(20, func(r int, row []string) {
		if r >= 3 && r <= 10 {
			row[0] = fmt.Sprintf("web%02d", r)
			row[2] = "완료"
		}
	})
	items := diffTables(t, old, new)
	expect(t, items, "C3~C10 (8개 셀): '대기' → '완료'")
	if it := items[0]; it.Kind != KindModified || it.Sheet != "S" || it.OldPos != "C3~C10" || it.NewPos != "C3~C10" ||
		it.OldValue != "대기" || it.NewValue != "완료" || it.Old != (Ref{3, 3}) || it.New != (Ref{3, 3}) {
		t.Errorf("%+v", it)
	}

	// 행이 밀린 범위
	shifted := slices.Insert(slices.Clone(new), 1, []string{"new1", "x1", "y1", "z1", "w1"}, []string{"new2", "x2", "y2", "z2", "w2"})
	expect(t, diffTables(t, old, shifted),
		"2~3행 추가 (2개 행)",
		"이전 C3~C10 → 최신 C5~C12 (8개 셀): '대기' → '완료'")

	// 계획서 예시(A열)
	column := func(v string) [][]string {
		g := [][]string{{"상태", "번호"}}
		for r := 2; r <= 12; r++ {
			s := "대기"
			if r >= 3 && r <= 10 {
				s = v
			}
			g = append(g, []string{s, fmt.Sprint(r)})
		}
		return g
	}
	expect(t, diffTables(t, column("대기"), column("완료")), "A3~A10 (8개 셀): '대기' → '완료'")
}

func TestRows(t *testing.T) {
	edit := func(ip, who string) func(int, []string) {
		return func(r int, row []string) {
			if r == 15 {
				row[0], row[1], row[4] = "web01", ip, who
			}
		}
	}
	old := table(20, edit("10.0.0.1", "김"))
	new := slices.Insert(table(20, edit("10.0.0.2", "이")), 5, []string{"db", "d", "e", "f", "g"})
	items := diffTables(t, old, new)
	expect(t, items,
		"6행 추가 (db)",
		"15행 → 16행 (web01): B열(IP) '10.0.0.1' → '10.0.0.2', E열(담당자) '김' → '이'")
	if it := items[1]; it.OldPos != "15행" || it.NewPos != "16행" || it.OldValue != "B: 10.0.0.1\nE: 김" || it.NewValue != "B: 10.0.0.2\nE: 이" ||
		it.Old != (Ref{15, 2}) || it.New != (Ref{16, 2}) {
		t.Errorf("%+v", it)
	}
	if it := items[0]; it.NewValue != "6행: A='db', B='d', C='e', D='f', E='g'" || it.OldValue != "" || it.Old != (Ref{}) || it.New != (Ref{6, 1}) {
		t.Errorf("%+v", it)
	}
	// 키 열이 이름표가 된다
	expect(t, diffTables(t, old, new, 4, 4)[1:],
		"15행 → 16행 (memo15): B열(IP) '10.0.0.1' → '10.0.0.2', E열(담당자) '김' → '이'")
}

func TestRangeBoundaries(t *testing.T) {
	old := table(12, nil)
	new := table(12, func(r int, row []string) {
		switch r {
		case 2, 3:
			row[2] = "완료"
		case 4: // 두 셀이 바뀐 행은 범위에 넣지 않음
			row[2], row[3] = "완료", "x"
		case 5, 7: // 연속이 아님
			row[2] = "완료"
		case 8: // 새 값이 다름
			row[2] = "보류"
		}
	})
	expect(t, diffTables(t, old, new),
		"C2~C3 (2개 셀): '대기' → '완료'",
		"4행 (web04): C열(상태) '대기' → '완료', D열(비고) 'memo4' → 'x'",
		"5행 (web05): C열(상태) '대기' → '완료'",
		"7행 (web07): C열(상태) '대기' → '완료'",
		"8행 (web08): C열(상태) '대기' → '보류'")
}

func TestAddDeleteMove(t *testing.T) {
	old := table(30, nil)
	deleted := slices.Delete(slices.Clone(old), 2, 5) // 3~5행 삭제
	expect(t, diffTables(t, old, deleted), "3~5행 삭제 (3개 행)")
	expect(t, diffTables(t, deleted, old), "3~5행 추가 (3개 행)")

	moved := slices.Clone(old)
	block := slices.Clone(moved[2:4])
	moved = slices.Insert(slices.Delete(moved, 2, 4), 20, block...)
	expect(t, diffTables(t, old, moved), "3~4행 → 21~22행 이동 (2개 행)")

	one := slices.Clone(old)
	row := slices.Clone(one[4])
	row[2] = "완료"
	one = slices.Insert(slices.Delete(one, 4, 5), 11, row)
	items := diffTables(t, old, one)
	expect(t, items, "5행 → 12행 이동 (web05): C열(상태) '대기' → '완료'")
	if items[0].Kind != KindMoved || items[0].OldPos != "C5" || items[0].NewPos != "C12" {
		t.Errorf("%+v", items[0])
	}
}

func TestColumns(t *testing.T) {
	old := lines("id,name,qty", "1,apple,10", "2,banana,20", "3,cherry,30", "4,date,40", "5,elder,50")
	added := lines("id,new,name,qty", "1,x,apple,10", "2,y,banana,20", "3,z,cherry,31", "4,?,date,40", "5,v,elder,50")
	items := diffTables(t, old, added)
	expect(t, items,
		"B열(new) 열 추가 (6개 값), 값 확인 불가 1개 포함",
		"4행 (3): C열→D열(qty) '30' → '31'")
	if v := items[0].NewValue; !strings.HasPrefix(v, "B1='new'\nB2='x'") || !strings.Contains(v, "B5=값 확인 불가") {
		t.Errorf("%q", v)
	}
	expect(t, diffTables(t, added, old)[:1], "B열(new) 열 삭제 (6개 값), 값 확인 불가 1개 포함")
	moved := lines("name,qty,id", "apple,10,1", "banana,20,2", "cherry,30,3", "date,40,4", "elder,50,5")
	expect(t, diffTables(t, old, moved), "A열(id) → C열(id) 열 이동")
}

func TestValueKinds(t *testing.T) {
	old := table(20, nil)
	new := table(20, func(r int, row []string) {
		switch r {
		case 2:
			row[2] = "대기 "
		case 4:
			row[3] = "MEMO4"
		case 6:
			row[3] = "memo\n6"
		case 8:
			row[2] = "?"
		case 10:
			row[2] = ""
		}
	})
	items := diffTables(t, old, new)
	expect(t, items,
		"2행 (web02): C열(상태) '대기' → '대기␣' [공백만 다름]",
		"4행 (web04): D열(비고) 'memo4' → 'MEMO4' [대소문자만 다름]",
		"6행 (web06): D열(비고) 'memo6' → 'memo↵6' [줄바꿈만 다름]",
		"8행 (web08): C열(상태) '대기' → 값 확인 불가",
		"10행 (web10): C열(상태) '대기' → (빈 값)")
	if items[3].Kind != KindUnknown || items[4].Kind != KindModified {
		t.Error(items[3].Kind, items[4].Kind)
	}
	const nbsp, zwsp = "\xc2\xa0", "\xe2\x80\x8b"
	for _, c := range []struct{ a, b, want string }{
		{"a b", "a\nb", "공백·줄바꿈만 다름"}, {"a\r\nb", "a\nb", "줄바꿈만 다름"}, {"Ab ", "aB", "대소문자·공백만 다름"},
		{"a" + nbsp + "b", "a b", "공백만 다름"}, {"a", "b", ""},
	} {
		if got, _ := difference(c.a, c.b); got != c.want {
			t.Errorf("%q %q: %q", c.a, c.b, got)
		}
	}
	if got := visible("a"+nbsp+zwsp+"\t", false); got != "a⟨U+00A0⟩⟨U+200B⟩⇥" {
		t.Errorf("%q", got)
	}
}

func TestDisplaySameAndHidden(t *testing.T) {
	a, b := book(sheet("S", table(8, nil))), book(sheet("S", table(8, nil)))
	set := func(w *reader.Workbook, r, c int, key, display string) {
		w.Sheets[0].Rows[r-1].Cells[c-1] = reader.Cell{Col: c, Key: key, Display: display}
	}
	set(a, 3, 3, "1.001", "1.00")
	set(b, 3, 3, "1.002", "1.00")
	b.Sheets[0].Rows[2].Hidden = true
	b.Sheets[0].Cols[3] = reader.Col{Hidden: true}
	b.Sheets[0].Hidden = true
	_, items := compare(t, a, b, nil)
	expect(t, items, "3행(숨김) (web03): C열(상태)(숨김) '1.00'(실제값 1.001) → '1.00'(실제값 1.002) [시트(숨김)]")
	if items[0].OldValue != "1.00 (실제값 1.001)" {
		t.Errorf("%q", items[0].OldValue)
	}
}

func TestSheets(t *testing.T) {
	g := lines("a,b", "1,2", "3,4")
	a := book(sheet("Old", g), sheet("Gone", g), sheet("X", g))
	b := book(sheet("New", g), sheet("Fresh", g), sheet("Y", g))
	_, items := compare(t, a, b, []diff.SheetPair{{Old: "Old", New: "New"}, {New: "Fresh"}, {Old: "Gone"}})
	expect(t, items,
		"시트 이름 변경: 'Old' → 'New'",
		"시트 'Fresh' 추가 (3개 행)",
		"1~3행 추가 (3개 행)",
		"시트 'Gone' 삭제 (3개 행)",
		"1~3행 삭제 (3개 행)",
		"이전 파일의 시트 'X': 비교에서 제외(내용 비교 안 함)",
		"최신 파일의 시트 'Y': 비교에서 제외(내용 비교 안 함)")
	if len(items) == 7 && (items[5].SheetIndex != -1 || items[2].SheetIndex != 1 ||
		items[2].NewValue != "1행: A='a', B='b'\n2행: A='1', B='2'\n3행: A='3', B='4'") {
		t.Errorf("%+v", items[2])
	}
}

func TestSame(t *testing.T) {
	if items := diffTables(t, table(10, nil), table(10, nil)); len(items) != 0 {
		t.Error(texts(items))
	}
}

func TestSplit(t *testing.T) {
	long := strings.Repeat("가", 32767)
	emoji := strings.Repeat("😀", 16000)
	old := table(10, nil)
	new := table(10, func(r int, row []string) {
		switch r {
		case 3: // 한 행에 긴 값 두 개: 항목 분할
			row[2], row[3] = long, emoji
		case 5: // 한 셀: 분할 불필요, 설명은 인용을 줄임
			row[2] = long
		}
	})
	for r := range 5 { // 긴 값의 추가 행 5개: 행 단위 분할
		new = append(new, []string{fmt.Sprint("n", r), strings.Repeat(fmt.Sprint(r), 20000)})
	}
	items := diffTables(t, old, new)
	if len(items) != 8 {
		t.Fatalf("%d개", len(items))
	}
	if items[0].NewValue+items[1].NewValue != "C: "+long+"\nD: "+emoji || !strings.HasSuffix(items[1].Text, "(2/2)") {
		t.Error("나눈 값이 원본과 다름")
	}
	if items[2].NewValue != long || length(items[2].Text) > 300 || !strings.Contains(items[2].Text, "(총 32767자)") {
		t.Error(items[2].Text)
	}
	for i, it := range items[3:] {
		if want := fmt.Sprintf("%d행", 11+i); it.NewPos != want || !strings.HasPrefix(it.Text, want+" 추가") {
			t.Error(it.NewPos, it.Text)
		}
	}

	// 값이 긴 추가 열은 셀 단위로 나뉜다
	wide := table(10, nil)
	for r := range wide {
		wide[r] = append(wide[r], strings.Repeat(fmt.Sprint(r), 20000))
	}
	if items := diffTables(t, table(10, nil), wide); len(items) != 10 {
		t.Error(len(items))
	}
}

// check 는 모든 변경 셀·행·열이 정확히 한 항목에 속하고 값이 그 항목에 남아 있는지 본다.
func check(t testing.TB, res *diff.Result, items []Item) {
	t.Helper()
	type key struct{ sheet, at int }
	changes, rows, cols := map[key]int{}, map[key]int{}, map[key]int{}
	for i, it := range items {
		if it.No != i+1 || !fits(it.Text) || !fits(it.OldValue) || !fits(it.NewValue) || it.Text == "" || it.Kind == 0 {
			t.Fatalf("잘못된 항목 %d: %d %d %d", i, length(it.Text), length(it.OldValue), length(it.NewValue))
		}
		for _, x := range []struct {
			at []int
			m  map[key]int
		}{{it.changes, changes}, {it.rows, rows}, {it.cols, cols}} {
			for _, k := range x.at {
				x.m[key{it.SheetIndex, k}]++
			}
		}
	}
	// 나뉜 항목은 이어 붙여 값을 확인한다.
	for i := 0; i < len(items); {
		it := items[i]
		oldValue, newValue := []string{it.OldValue}, []string{it.NewValue}
		j := i + 1
		for ; j < len(items) && items[j].cont; j++ {
			oldValue, newValue = append(oldValue, items[j].OldValue), append(newValue, items[j].NewValue)
		}
		o, n := strings.Join(oldValue, ""), strings.Join(newValue, "")
		for _, k := range it.changes {
			sd := res.Sheets[it.SheetIndex]
			ch := sd.Changes[k]
			if ch.Old != nil && !strings.Contains(o, ch.Old.Display) || ch.New != nil && !strings.Contains(n, ch.New.Display) {
				t.Fatalf("항목 %d: 값이 없음 %+v %+v", it.No, ch.Old, ch.New)
			}
			// 셀의 행이 항목의 행이거나, 열 항목이면 그 열이어야 한다.
			if len(it.rows) > 0 && !slices.Contains(it.rows, ch.Row) || len(it.cols) > 0 && it.cols[0] != ch.Col {
				t.Fatalf("항목 %d: 다른 행·열의 셀", it.No)
			}
		}
		i = j
	}
	total := 0
	for s, sd := range res.Sheets {
		paired := map[int]bool{} // 짝지은 열의 변경이 있는 행
		for k, ch := range sd.Changes {
			total++
			if changes[key{s, k}] != 1 {
				t.Fatalf("시트 %d 변경 %d: %d번", s, k, changes[key{s, k}])
			}
			if c := sd.Cols[ch.Col]; c.Old > 0 && c.New > 0 {
				paired[ch.Row] = true
			}
		}
		for r, p := range sd.Rows {
			want := 0
			if p.Old == nil || p.New == nil || p.Status == diff.StatusMoved || paired[r] {
				want = 1
			}
			if rows[key{s, r}] != want {
				t.Fatalf("시트 %d 행 첨자 %d: %d번, 기대 %d", s, r, rows[key{s, r}], want)
			}
		}
		for c, p := range sd.Cols {
			want := 0
			if sd.Old != nil && sd.New != nil && p.Status != diff.StatusSame {
				want = 1
			}
			if cols[key{s, c}] != want {
				t.Fatalf("시트 %d 열 첨자 %d: %d번, 기대 %d", s, c, cols[key{s, c}], want)
			}
		}
	}
	if len(changes) != total {
		t.Fatalf("변경 수 %d != %d", len(changes), total)
	}
}

func TestRandom(t *testing.T) {
	seen := map[Kind]int{}
	var seed uint64
	defer func() {
		if t.Failed() {
			t.Log("seed", seed)
		}
	}()
	for seed = range uint64(5000) {
		rng := rand.New(rand.NewPCG(seed, 11))
		value := func() string {
			return []string{"", "?", "a", "A", "a ", "b\nc", fmt.Sprint("v", rng.IntN(1000))}[rng.IntN(7)]
		}
		nr, nc := 1+rng.IntN(25), 1+rng.IntN(7)
		distinct := rng.IntN(2) == 0
		old := make([][]string, nr)
		for r := range old {
			old[r] = make([]string, nc)
			for c := range old[r] {
				if distinct && rng.IntN(5) > 0 {
					old[r][c] = fmt.Sprintf("%d-%d", r, c)
				} else {
					old[r][c] = value()
				}
			}
		}
		new := make([][]string, nr)
		for r := range new {
			new[r] = slices.Clone(old[r])
		}
		for range rng.IntN(8) {
			if len(new) == 0 {
				break
			}
			width := len(new[0])
			switch rng.IntN(8) {
			case 0: // 셀 수정
				new[rng.IntN(len(new))][rng.IntN(width)] = value()
			case 1: // 같은 열의 연속 변경
				c, v := rng.IntN(width), value()
				for r := rng.IntN(len(new)); r < len(new) && rng.IntN(6) > 0; r++ {
					new[r][c] = v
				}
			case 2: // 행 삽입
				row := make([]string, width)
				for c := range row {
					row[c] = value()
				}
				new = slices.Insert(new, rng.IntN(len(new)+1), row)
			case 3: // 행 삭제
				i := rng.IntN(len(new))
				new = slices.Delete(new, i, min(len(new), i+1+rng.IntN(3)))
			case 4: // 행 이동
				i := rng.IntN(len(new))
				block := slices.Clone(new[i : i+min(len(new)-i, 1+rng.IntN(3))])
				new = slices.Delete(new, i, i+len(block))
				new = slices.Insert(new, rng.IntN(len(new)+1), block...)
			case 5: // 열 삽입
				c := rng.IntN(width + 1)
				for r := range new {
					new[r] = slices.Insert(slices.Clone(new[r]), c, value())
				}
			case 6: // 열 삭제
				if width > 1 {
					c := rng.IntN(width)
					for r := range new {
						new[r] = slices.Delete(slices.Clone(new[r]), c, c+1)
					}
				}
			case 7: // 열 이동
				i, j := rng.IntN(width), rng.IntN(width)
				for r := range new {
					v := new[r][i]
					new[r] = slices.Insert(slices.Delete(slices.Clone(new[r]), i, i+1), j, v)
				}
			}
		}
		a, b := book(sheet("S", old), sheet("D", old)), book(sheet("S", new), sheet("N", new))
		_, items := compare(t, a, b, nil)
		for _, it := range items {
			seen[it.Kind]++
		}
	}
	for k := KindModified; k <= KindStructure; k++ {
		if seen[k] == 0 {
			t.Error("나오지 않은 구분", k)
		}
	}
}

// TestLarge 는 30열 × 30,000행의 모든 셀이 바뀐 경우 등의 시간을 잰다. go test -run Large -v
func TestLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("대용량 시험")
	}
	const rows, cols = 30000, 30
	// 앞의 same 개 열은 양쪽이 같다.
	grid := func(prefix string, same int) [][]string {
		g := make([][]string, rows)
		for r := range g {
			g[r] = make([]string, cols)
			for c := range g[r] {
				p := prefix
				if c < same {
					p = ""
				}
				g[r][c] = fmt.Sprintf("%s%d-%d", p, r, c)
			}
		}
		return g
	}
	column := func(v string) [][]string {
		g := grid("", cols)
		for r := range g {
			g[r][5] = v
		}
		return g
	}
	for _, c := range []struct {
		name     string
		old, new [][]string
	}{
		{"all changed", grid("o", 0), grid("n", 0)},
		{"all but first column changed", grid("o", 1), grid("n", 1)},
		{"one column range", column("a"), column("b")},
		{"rows deleted", grid("o", 0), grid("o", 0)[:1]},
	} {
		a, b := book(sheet("S", c.old)), book(sheet("S", c.new))
		res, err := diff.Compare(a, b, diff.DefaultPairs(a, b), nil)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		items := Describe(res, nil)
		elapsed := time.Since(start)
		check(t, res, items)
		t.Logf("%s: 변경 %d, 항목 %d, Describe %v", c.name, len(res.Sheets[0].Changes), len(items), elapsed.Round(time.Millisecond))
	}
}
