package report

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"excel-diff/internal/describe"
	"excel-diff/internal/diff"
	"excel-diff/internal/reader"
)

// makeFile 은 시트별 격자("" 는 빈 셀)로 xlsx 를 만든다. edit 로 추가 설정을 한다.
func makeFile(t testing.TB, path string, names []string, grids [][][]any, edit func(*excelize.File)) {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for i, name := range names {
		if i == 0 {
			must(t, f.SetSheetName("Sheet1", name))
		} else {
			_, err := f.NewSheet(name)
			must(t, err)
		}
		for r, row := range grids[i] {
			for c, v := range row {
				if v == "" {
					continue
				}
				cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
				must(t, f.SetCellValue(name, cell, v))
			}
		}
	}
	if edit != nil {
		edit(f)
	}
	must(t, f.SaveAs(path))
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func run(t testing.TB, o Options) (*Output, *excelize.File) {
	t.Helper()
	out, err := Run(o)
	must(t, err)
	f, err := excelize.OpenFile(out.Path)
	must(t, err)
	t.Cleanup(func() { f.Close() })
	return out, f
}

func value(t testing.TB, f *excelize.File, sheet, cell string) string {
	t.Helper()
	v, err := f.GetCellValue(sheet, cell)
	must(t, err)
	return v
}

func color(t testing.TB, f *excelize.File, sheet, cell string) string {
	t.Helper()
	id, err := f.GetCellStyle(sheet, cell)
	must(t, err)
	s, err := f.GetStyle(id)
	must(t, err)
	if len(s.Fill.Color) == 0 {
		return ""
	}
	return s.Fill.Color[0]
}

func want(t testing.TB, f *excelize.File, sheet, cell, v string, bg fill) {
	t.Helper()
	if got := value(t, f, sheet, cell); got != v {
		t.Errorf("%s!%s = %q, want %q", sheet, cell, got, v)
	}
	if got := color(t, f, sheet, cell); got != fillColors[bg] {
		t.Errorf("%s!%s 색 = %q, want %q", sheet, cell, got, fillColors[bg])
	}
}

func allText(t testing.TB, f *excelize.File, sheet string) string {
	t.Helper()
	rows, err := f.GetRows(sheet)
	must(t, err)
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strings.Join(r, "|") + "\n")
	}
	return b.String()
}

var linkExp = regexp.MustCompile(`^'((?:[^']|'')+)'!([A-Z]+[0-9]+)$`)

// checkLinks 는 모든 바로가기가 실제 시트를 가리키는지 확인하고 개수를 돌려준다.
func checkLinks(t testing.TB, f *excelize.File, log string) (links int) {
	t.Helper()
	rows, err := f.GetRows(log)
	must(t, err)
	exists := map[string]bool{}
	for _, n := range f.GetSheetList() {
		exists[n] = true
	}
	for i := 2; i <= len(rows); i++ {
		cell, _ := excelize.CoordinatesToCellName(9, i)
		ok, target, err := f.GetCellHyperLink(log, cell)
		must(t, err)
		if !ok {
			continue
		}
		links++
		m := linkExp.FindStringSubmatch(target)
		if m == nil || !exists[strings.ReplaceAll(m[1], "''", "'")] {
			t.Fatalf("%d행 링크 대상 오류: %q", i, target)
		}
	}
	return links
}

// linkOf 는 설명에 contains 가 있는 첫 변경내역 행의 바로가기 대상이다.
func linkOf(t testing.TB, f *excelize.File, contains string) (sheet, cell string) {
	t.Helper()
	rows, err := f.GetRows(logName)
	must(t, err)
	for i, r := range rows {
		if len(r) > 7 && strings.Contains(r[7], contains) {
			at, _ := excelize.CoordinatesToCellName(9, i+1)
			_, target, err := f.GetCellHyperLink(logName, at)
			must(t, err)
			m := linkExp.FindStringSubmatch(target)
			if m == nil {
				t.Fatalf("%q 의 링크 없음: %q", contains, target)
			}
			return strings.ReplaceAll(m[1], "''", "'"), m[2]
		}
	}
	t.Fatalf("항목 없음: %q\n%s", contains, allText(t, f, logName))
	return "", ""
}

// specialFiles 는 특수문자·긴 시트명·숨김·추가/삭제 시트가 있는 입력 두 개를 만든다.
func specialFiles(t testing.TB, dir string) (old, new string) {
	long := "가나다라마바사아자차카타파하가나다라마바사아자차카타파하12" // 30자
	quote := "O'Brien & <x>"
	head := []any{"ID", "이름", "수량", "비고"}
	oldMain := [][]any{head,
		{"a1", "=SUM(A1)", 10, "x"},
		{"a2", "둘", 20, "지울 행"},
		{"a3", "셋", 30, "z"},
		{"a4", "넷", 40, "w"},
		{"a5", "다섯", 50, "v"},
		{"a6", "여섯", 60, "u"},
		{"a7", "_x005F_x0041_ <a&b>", 1.5, "ctl"},
	}
	newMain := [][]any{head,
		{"a1", "=SUM(A1)", 11, "x"},
		{"a3", "셋", 30, "z"},
		{"a5", "다섯", 50, "v"},
		{"a6", "여섯", 60, "u"},
		{"a4", "넷", 40, "w"},
		{"a7", "_x005F_x0041_ <a&b>", 1.5, "CTL"},
		{"a8", " 앞뒤 \n줄바꿈 ", "007", strings.Repeat("긴", 32767)},
	}
	small := [][]any{{"k", "v"}, {"1", "a"}}
	changed := [][]any{{"k", "v"}, {"1", "b"}}
	hide := func(f *excelize.File) {
		must(t, f.SetColWidth("요약", "B", "B", 30))
		must(t, f.SetRowVisible("요약", 2, false))
		must(t, f.SetColVisible("요약", "D", false))
	}
	old, new = filepath.Join(dir, "이전.xlsx"), filepath.Join(dir, "최신.xlsx")
	makeFile(t, old, []string{"요약", "변경내역", long + "A", long + "B", quote, "삭제됨"},
		[][][]any{oldMain, small, small, small, small, small}, hide)
	makeFile(t, new, []string{"요약", "변경내역", long + "A", long + "B", quote, "추가됨"},
		[][][]any{newMain, changed, changed, changed, changed, small}, hide)
	return old, new
}

func TestSpecial(t *testing.T) {
	dir := t.TempDir()
	old, new := specialFiles(t, dir)
	before, err := os.ReadFile(old)
	must(t, err)
	var percents []int
	var stages []string
	out, f := run(t, Options{OldPath: old, NewPath: new, OutDir: filepath.Join(dir, "results"),
		Progress: func(p int, s string) {
			percents = append(percents, p)
			if len(stages) == 0 || stages[len(stages)-1] != s {
				stages = append(stages, s)
			}
		}})
	if after, _ := os.ReadFile(old); string(after) != string(before) {
		t.Error("입력 파일이 바뀜")
	}
	for i := 1; i < len(percents); i++ {
		if percents[i] < percents[i-1] {
			t.Fatalf("진행률 감소: %v", percents)
		}
	}
	if percents[len(percents)-1] != 100 || !strings.Contains(strings.Join(stages, ","), "결과 파일 저장 중,완료") {
		t.Errorf("진행: %v %v", percents, stages)
	}
	if !regexp.MustCompile(`^비교결과_\d{8}_\d{6}\.xlsx$`).MatchString(filepath.Base(out.Path)) {
		t.Error(out.Path)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "results", "*.tmp")); len(left) > 0 {
		t.Error("임시 파일이 남음", left)
	}

	if keep := os.Getenv("EXCELDIFF_KEEP"); keep != "" {
		must(t, os.MkdirAll(keep, 0o755))
		data, err := os.ReadFile(out.Path)
		must(t, err)
		must(t, os.WriteFile(filepath.Join(keep, "special.xlsx"), data, 0o644))
	}

	list := f.GetSheetList()
	t.Log(strings.Join(list, " / "))
	// 요약, 변경내역, 짝 5개×3, 추가·삭제 시트×2
	if len(list) != 2+5*3+2*2 || list[0] != "요약" || list[1] != "변경내역" {
		t.Fatal("시트 목록", len(list), list)
	}
	seen := map[string]bool{}
	for _, n := range list {
		if seen[fold(n)] || length(n) > 31 {
			t.Error("시트명", n)
		}
		seen[fold(n)] = true
	}
	for _, n := range []string{"요약_비교", "요약_Before", "요약_After", "O'Brien & <x>_비교", "삭제됨_Before", "추가됨_After"} {
		if !seen[fold(n)] {
			t.Error("시트 없음", n)
		}
	}
	if seen["삭제됨_after"] || seen["추가됨_before"] {
		t.Error("없는 쪽 시트가 생김")
	}

	// 비교 시트: A 이전 행 | B~E | F 구분 | G 최신 행 | H~K
	c := "요약_비교"
	want(t, f, c, "A4", "2 (숨김)", fillModified)
	want(t, f, c, "B4", "a1", fillNone)
	want(t, f, c, "C4", "=SUM(A1)", fillNone)
	want(t, f, c, "D4", "10", fillModified)
	want(t, f, c, "J4", "11", fillModified)
	want(t, f, c, "D2", "C", fillHeader)
	want(t, f, c, "E2", "D (숨김)", fillHeader)
	if formula, _ := f.GetCellFormula(c, "C4"); formula != "" {
		t.Error("수식이 됨", formula)
	}
	text := allText(t, f, c)
	for _, s := range []string{"_x0041_ <a&b>", " 앞뒤 \n줄바꿈 ", "007"} {
		if !strings.Contains(text, s) {
			t.Errorf("비교 시트에 %q 없음", s)
		}
	}
	// 원래 위치
	want(t, f, "요약_Before", "D3", "지울 행", fillDeleted)
	want(t, f, "요약_Before", "A5", "a4", fillMoved)
	want(t, f, "요약_After", "C2", "11", fillModified)
	want(t, f, "요약_After", "A6", "a4", fillMoved)
	want(t, f, "요약_After", "A8", "a8", fillAdded)
	if typ, _ := f.GetCellType("요약_After", "C2"); typ != excelize.CellTypeUnset && typ != excelize.CellTypeNumber {
		t.Error("숫자형이 아님", typ)
	}
	if typ, _ := f.GetCellType("요약_After", "C8"); typ != excelize.CellTypeInlineString {
		t.Error("007 은 문자열", typ)
	}
	if v := value(t, f, "요약_After", "D8"); v != strings.Repeat("긴", 32767) {
		t.Error("긴 값", length(v))
	}
	for _, sheet := range []string{"요약_After", c} {
		if v, _ := f.GetColVisible(sheet, "D"); !v {
			t.Error("숨김 열이 숨겨짐", sheet)
		}
		if v, _ := f.GetRowVisible(sheet, 2); !v {
			t.Error("숨김 행이 숨겨짐", sheet)
		}
		if p, err := f.GetPanes(sheet); err != nil || !p.Freeze {
			t.Error("틀 고정 없음", sheet, err)
		}
	}
	if w, _ := f.GetColWidth("요약_After", "B"); w != 30 {
		t.Error("열 너비", w)
	}

	// 변경내역과 링크
	if n := checkLinks(t, f, logName); n != out.Items {
		t.Errorf("링크 %d, 항목 %d", n, out.Items)
	}
	rows, err := f.GetRows(logName)
	must(t, err)
	if strings.Join(rows[0], "|") != strings.Join(logHeader, "|") || len(rows) != out.Items+1 {
		t.Error("변경내역", len(rows), rows[0])
	}
	if s, cell := linkOf(t, f, "'10' → '11'"); s != c || value(t, f, s, cell) != "11" {
		t.Error("수정 링크", s, cell)
	}
	if s, cell := linkOf(t, f, "3행 삭제"); s != c || !strings.HasPrefix(cell, "B") || value(t, f, s, cell) != "a2" {
		t.Error("삭제 링크", s, cell)
	} else {
		row := cell[1:]
		want(t, f, c, "A"+row, "3", fillDeleted)
		want(t, f, c, "E"+row, "지울 행", fillDeleted)
		want(t, f, c, "G"+row, "", fillNone)
		want(t, f, c, "H"+row, "", fillNone)
	}
	if s, cell := linkOf(t, f, "이동"); s != c || !strings.HasPrefix(cell, "G") || color(t, f, s, cell) != fillColors[fillMoved] {
		t.Error("이동 링크", s, cell)
	}
	if s, cell := linkOf(t, f, "시트 '삭제됨' 삭제"); s != "삭제됨_비교" || cell != "A1" {
		t.Error("시트 링크", s, cell)
	}
	filter := false
	for _, d := range f.GetDefinedName() {
		filter = filter || d.Name == "_xlnm._FilterDatabase"
	}
	if !filter {
		t.Error("자동 필터 없음")
	}

	summary := allText(t, f, summaryName)
	for _, s := range []string{"이전.xlsx", "최신.xlsx", "시트별 건수", "삭제됨", "색 범례", "비교하지 않음", "Excel 한계"} {
		if !strings.Contains(summary, s) {
			t.Errorf("요약에 %q 없음", s)
		}
	}
}

// edgeSource 는 reader 를 거치지 않고 제어문자·실제값 병기·확인 불가를 시험할 비교 결과를 만든다.
func edgeSource(t testing.TB) Source {
	book := func(cells ...reader.Cell) *reader.Workbook {
		for i := range cells {
			cells[i].Col = i + 1
		}
		return &reader.Workbook{Sheets: []reader.Sheet{{Name: "a:b/c", Cols: map[int]reader.Col{},
			Rows: []reader.Row{{Num: 1, Cells: cells}}}},
			Warnings: []reader.Warning{{Sheet: "a:b/c", Cell: "A1", Message: "=경고\x01"}}}
	}
	same := reader.Cell{Key: "same", Display: "same"}
	old := book(same, reader.Cell{Key: "1.001", Display: "1.00"}, reader.Cell{Key: "\x00unknown", Display: "값 확인 불가", Unknown: true},
		reader.Cell{Key: "x", Display: "x"}, reader.Cell{Key: "5", Display: ""})
	new := book(same, reader.Cell{Key: "1.002", Display: "1.00"}, reader.Cell{Key: "3", Display: "3"},
		reader.Cell{Key: "\x01\xef\xbf\xbe", Display: strings.Repeat("\x01", 10000)}, reader.Cell{Key: "6", Display: ""})
	res, err := diff.Compare(old, new, diff.DefaultPairs(old, new), nil)
	must(t, err)
	return Source{OldName: "a", NewName: "b", Created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local),
		Result: res, Items: describe.Describe(res, nil)}
}

func TestEdgeAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := edgeSource(t)
	first := filepath.Join(dir, "비교결과_20260102_030405.xlsx")
	must(t, os.WriteFile(first, []byte("keep"), 0o644))
	path, err := Write(src, dir, nil)
	must(t, err)
	path3, err := Write(src, dir, nil)
	must(t, err)
	if filepath.Base(path) != "비교결과_20260102_030405_2.xlsx" || filepath.Base(path3) != "비교결과_20260102_030405_3.xlsx" {
		t.Error(path, path3)
	}
	if b, _ := os.ReadFile(first); string(b) != "keep" {
		t.Error("기존 파일을 덮어씀")
	}
	if keep := os.Getenv("EXCELDIFF_KEEP"); keep != "" {
		data, err := os.ReadFile(path)
		must(t, err)
		must(t, os.MkdirAll(keep, 0o755))
		must(t, os.WriteFile(filepath.Join(keep, "edge.xlsx"), data, 0o644))
	}
	f, err := excelize.OpenFile(path)
	must(t, err)
	defer f.Close()
	s := "a_b_c_비교"
	// A 이전 행 | B~F | G 구분 | H 최신 행 | I~M
	want(t, f, s, "C3", "1.00 (실제값 1.001)", fillModified)
	want(t, f, s, "J3", "1.00 (실제값 1.002)", fillModified)
	want(t, f, s, "D3", "값 확인 불가", fillUnknown)
	want(t, f, s, "K3", "3", fillUnknown)
	want(t, f, s, "F3", "(실제값 5)", fillModified)
	if v := value(t, f, s, "L3"); !strings.HasPrefix(v, "⟨U+0001⟩") || !strings.HasSuffix(v, cutMark) || length(v) > describe.Limit {
		t.Error("축약", length(v))
	}
	// 변경내역은 자르지 않고 나눈다
	rows, err := f.GetRows(logName)
	must(t, err)
	all := ""
	for _, r := range rows[1:] {
		if len(r) > 6 {
			all += r[6]
		}
	}
	if n := strings.Count(all, "⟨U+0001⟩"); n < 10000 {
		t.Error("변경내역 값이 잘림", n)
	}
	if v := allText(t, f, summaryName); !strings.Contains(v, "=경고⟨U+0001⟩") || !strings.Contains(v, "a_b_c_After D1") && !strings.Contains(v, "a_b_c_비교 L3") || !strings.Contains(v, "여러 행으로") {
		t.Error(v)
	}
}

func TestOutputDir(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "file")
	must(t, os.WriteFile(blocked, nil, 0o644))
	if _, err := Write(edgeSource(t), filepath.Join(blocked, "results"), nil); !errors.Is(err, ErrOutputDir) {
		t.Fatal(err)
	}
	old, new := specialFiles(t, dir)
	out, _ := run(t, Options{OldPath: old, NewPath: new, OutDir: blocked, FallbackDir: filepath.Join(dir, "docs")})
	if filepath.Dir(out.Path) != filepath.Join(dir, "docs") {
		t.Error(out.Path)
	}
}

func TestLimits(t *testing.T) {
	defer func(r, c, l int) { maxRows, maxCols, maxLinks = r, c, l }(maxRows, maxCols, maxLinks)
	dir := t.TempDir()
	var old, new [][]any
	for r := range 12 {
		var a, b []any
		for c := range 5 {
			v := strings.Repeat("v", c+1) + string(rune('A'+r))
			a, b = append(a, v), append(b, v)
		}
		b[4] = "n" + string(rune('A'+r))
		if r%2 == 1 {
			b[1] = "m" + string(rune('a'+r))
		}
		old, new = append(old, a), append(new, b)
	}
	new = append(new, []any{"추가1", "2", "3", "4", "5"})
	o, n := filepath.Join(dir, "o.xlsx"), filepath.Join(dir, "n.xlsx")
	makeFile(t, o, []string{"S"}, [][][]any{old}, nil)
	makeFile(t, n, []string{"S"}, [][][]any{new}, nil)
	// 원본 읽기 후 한도를 낮춘다. 비교 시트: 데이터 10행, 열 짝 3개
	maxRows, maxCols, maxLinks = 6, 9, 3
	out, f := run(t, Options{OldPath: o, NewPath: n, OutDir: dir})
	list := strings.Join(f.GetSheetList(), ",")
	t.Log(list, out.Items)
	// 13행 / 4행 = 4조각, 열 5 / 3 = 2조각
	for _, s := range []string{"S_비교", "S_비교8", "변경내역2", "요약2"} {
		if !strings.Contains(list, s) {
			t.Error("분할 시트 없음", s)
		}
	}
	total, links, wantLinks, summary := 0, 0, 0, ""
	for _, s := range f.GetSheetList() {
		if strings.HasPrefix(s, logName) {
			rows, err := f.GetRows(s)
			must(t, err)
			if len(rows) > maxRows {
				t.Error("행 한도 초과", s, len(rows))
			}
			total += len(rows) - 1
			wantLinks += min(len(rows)-1, maxLinks)
			links += checkLinks(t, f, s)
		}
		if strings.HasPrefix(s, summaryName) {
			summary += allText(t, f, s)
		}
	}
	if total != out.Items || links != wantLinks {
		t.Error(total, links, out.Items)
	}
	// 열 조각 2(D,E), 행 조각 4(13행): 마지막 추가 행. A | B C | D | E | F G
	want(t, f, "S_비교8", "E2", "최신 행", fillHeader)
	want(t, f, "S_비교8", "E3", "13", fillAdded)
	want(t, f, "S_비교8", "G3", "5", fillAdded)
	for _, s := range []string{"행 한도를 넘어", "열 한도를 넘어", "하이퍼링크 한도", "변경내역이 시트당"} {
		if !strings.Contains(summary, s) {
			t.Errorf("요약에 %q 없음\n%s", s, summary)
		}
	}
}

func TestNamer(t *testing.T) {
	n := newNamer(summaryName, logName)
	long := strings.Repeat("😀", 20)
	for _, c := range []struct{ base, suffix, want string }{
		{"abc", "_비교", "abc_비교"},
		{"ABC", "_비교", "ABC~2_비교"},
		{"'a[1]*?", "_After", "_a_1____After"},
		{long, "_Before", strings.Repeat("😀", 12) + "_Before"},
		{long + "x", "_Before", strings.Repeat("😀", 11) + "~2_Before"},
		{"요약", "", "요약~2"},
		{"변경내역", "2", "변경내역2"},
	} {
		if got := n.name(c.base, c.suffix); got != c.want || length(got) > 31 {
			t.Errorf("%q+%q = %q, want %q", c.base, c.suffix, got, c.want)
		}
	}
}

func TestNumber(t *testing.T) {
	for s, ok := range map[string]bool{"0": true, "-12.5": true, "1234567": true, "007": false, "1.50": false, "1,000": false,
		"-0": false, "NaN": false, "-Inf": false, "123456789012": false, "123456789": false, "1e3": false, "": false, " 1": false} {
		if _, got := number(s, defaultWidth); got != ok {
			t.Error(s, got)
		}
	}
}
