package reader

import (
	"archive/zip"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// 저장값 없는 수식이 계산할 수 없는 셀에 의존하면 Unknown 이어야 한다.
func TestFormulaDependencies(t *testing.T) {
	f := excelize.NewFile()
	_, err := f.NewSheet("Other Sheet")
	must(t, err,
		f.SetDefinedName(&excelize.DefinedName{Name: "bad_name", RefersTo: "Sheet1!$A$1"}),
		f.SetDefinedName(&excelize.DefinedName{Name: "good_name", RefersTo: "'Other Sheet'!$A$1"}),
		f.SetCellValue("Other Sheet", "A1", 7),
		f.SetCellValue("Sheet1", "A2", 2))
	unknown := map[string]string{
		"F1": "[1]Other!A1", "G1": "F1+10",
		"A1": "NOSUCHFUNC(1)", "H1": `IF(A1>0,"yes","no")`,
		"I1": "ISERROR(NOSUCHFUNC(1))",
		"D1": "E1+1", "E1": "D1+1", "J1": "D1*2",
		"K1": "SUM(F1:G1)", "L1": "'Other Sheet'!B1+1",
		"M1": "bad_name+1", "N1": `INDIRECT("A2")`, "O1": "SUM(OFFSET(A2,0,0))+1",
		"P1": "SUM(1:1)", "Q1": "Q1+1", "R1": "S1+1", // S1 은 저장값이 있어도 다시 계산된다
		"T1": "SUM(A:A)", "U1": "NoSuchSheet!A1+1",
	}
	known := map[string][2]string{ // 수식, 결과
		"A3": {"A2+1", "3"}, "B3": {"SUM(A2:A3)", "5"}, "C3": {"good_name*2", "14"},
		"E3": {"'Other Sheet'!A1+B3", "12"},
	}
	for ref, formula := range unknown {
		must(t, f.SetCellFormula("Sheet1", ref, formula))
	}
	for ref, v := range known {
		must(t, f.SetCellFormula("Sheet1", ref, v[0]))
	}
	// S1 은 저장값이 있는 외부 참조 수식이 되도록 XML 을 고친다.
	must(t, f.SetCellFormula("Other Sheet", "B1", "Sheet1!F1"), f.SetCellFormula("Sheet1", "S1", "1"))
	p := save(t, f)
	zr, err := zip.OpenReader(p)
	must(t, err)
	r, err := zr.Open("xl/worksheets/sheet1.xml")
	must(t, err)
	data, err := io.ReadAll(r)
	must(t, err, r.Close(), zr.Close())
	if !strings.Contains(string(data), "<f>1</f>") {
		t.Fatalf("S1 XML: %s", data)
	}
	patch(t, p, "xl/worksheets/sheet1.xml", strings.Replace(string(data), "<f>1</f>", "<f>[1]Other!A1</f><v>3</v>", 1))

	wb, err := Read(p, nil)
	must(t, err)
	got := cells(wb.Sheets[0])
	for ref, formula := range unknown {
		if !got[ref].Unknown {
			t.Errorf("%s=%s: %q, Unknown 이어야 한다", ref, formula, got[ref].Display)
		}
	}
	for ref, v := range known {
		check(t, got, ref, v[1], v[1])
	}
	summary := 0
	for _, w := range wb.Warnings {
		if strings.Contains(w.Message, "자체 계산한 수식") {
			summary++
			if w.Sheet != "Sheet1" || w.Cell != "" || !strings.Contains(w.Message, "4개") {
				t.Error(w)
			}
		}
	}
	if summary != 1 {
		t.Error("요약 경고", summary, wb.Warnings)
	}
}

// readFormulas 는 f 를 저장해 읽고 시트별 셀을 돌려준다.
func readFormulas(t *testing.T, f *excelize.File) map[string]Cell {
	t.Helper()
	wb, err := Read(save(t, f), nil)
	must(t, err)
	return cells(wb.Sheets[0])
}

func wantFormulas(t *testing.T, got map[string]Cell, known map[string]string, unknown ...string) {
	t.Helper()
	for ref, v := range known {
		check(t, got, ref, v, v)
	}
	for _, ref := range unknown {
		if !got[ref].Unknown {
			t.Errorf("%s: %q, Unknown 이어야 한다", ref, got[ref].Display)
		}
	}
}

// 공유 수식 복사 셀의 시트 지정 참조는 이동할 것이 없는(전부 절대) 경우만 계산한다.
func TestFormulaSharedSheetRef(t *testing.T) {
	f := excelize.NewFile()
	for _, name := range []string{"Master", "Data", "Q1-Data"} {
		_, err := f.NewSheet(name)
		must(t, err)
	}
	for r := 1; r <= 2; r++ {
		must(t, f.SetCellValue("Sheet1", "A"+itoa(r), r),
			f.SetCellValue("Master", "A"+itoa(r), r), f.SetCellValue("Master", "B"+itoa(r), r*10),
			f.SetCellValue("Data", "A"+itoa(r), r*100))
	}
	shared := func(col, formula string) {
		kind, ref := excelize.STCellFormulaTypeShared, col+"1:"+col+"2"
		must(t, f.SetCellFormula("Sheet1", col+"1", formula, excelize.FormulaOpts{Type: &kind, Ref: &ref}))
	}
	shared("B", "VLOOKUP(A1,Master!$A:$B,2,0)")
	shared("C", "A1+Master!$B$1+SUM(Master!$1:$1)")
	shared("D", "Master!B1+1")
	shared("E", "Master!$B1+1")
	shared("F", "SUM(Master!$A$1:B1)")
	shared("G", "'Q1-Data'!$A$1+1") // 복사본은 따옴표가 사라져 Q1 - Data!$A$1 로 읽힌다
	got := readFormulas(t, f)
	wantFormulas(t, got, map[string]string{"B1": "10", "B2": "20", "C1": "22", "C2": "23", "D1": "11"},
		"D2", "E2", "F2", "G2")
}

// ROW/COLUMN/ROWS/COLUMNS 인수의 자기 참조는 순환이 아니다. 다른 셀을 거쳐 되돌아오면
// excelize 가 틀린 값을 낼 수 있으므로 Unknown 이다.
func TestFormulaPositionFuncs(t *testing.T) {
	f := excelize.NewFile()
	for ref, formula := range map[string]string{
		"A2": "ROW(A2)-1", "B2": "A2+1", // 자기 참조
		"A3": "ROWS(A$1:A3)", "B3": "A3+1",
		"C3": "COLUMN(C3)+COLUMNS($A3:C3)", "D3": "C3+1",
		"A5": "ROW(B5)", "B5": "ROW(C5)", "C5": "B5+1", "D5": "C5+1", // ROW 로만 되돌아오는 참조
		"A6": "ROWS(A$6:B6)", "B6": "A6+1",
		"A7": "ROW(B7)", "B7": "C7+1", "C7": "1+1", "D7": "A7+B7",
		"A10": "ROW(A10)+A10", "A11": "SUM(ROW(A11),A11)", "A12": "ROW((A12+1))",
	} {
		must(t, f.SetCellFormula("Sheet1", ref, formula))
	}
	wantFormulas(t, readFormulas(t, f), map[string]string{
		"A2": "1", "B2": "2", "A3": "3", "B3": "4", "C3": "6", "D3": "7",
		"A7": "7", "B7": "3", "D7": "10",
	}, "B5", "C5", "D5", "A6", "B6", "A10", "A11", "A12")
}

// 3차원 참조는 시트 이름이 열 이름처럼 보여도 계산하지 않는다.
func TestFormula3D(t *testing.T) {
	f := excelize.NewFile()
	must(t, f.SetSheetName("Sheet1", "Jan"))
	_, err := f.NewSheet("Mar")
	must(t, err, f.SetCellFormula("Jan", "B2", "[1]Other!A1"), f.SetCellValue("Mar", "B2", 5),
		f.SetCellFormula("Mar", "A1", "SUM(Jan:Mar!B2)"), // 열 B..JAN 으로 읽으면 A 열은 범위 밖이다
		f.SetCellFormula("Mar", "A2", "SUM('Jan:Mar'!B2)"),
		f.SetCellFormula("Mar", "A3", "SUM(Mar!B2:Mar!B3)"))
	wb, err := Read(save(t, f), nil)
	must(t, err)
	wantFormulas(t, cells(wb.Sheets[1]), map[string]string{"A3": "5"}, "A1", "A2")
}

// 상한을 넘는 병합·배열 영역은 어느 위치를 참조해도 기준 셀에 의존한다.
func TestFormulaBigArea(t *testing.T) {
	f := excelize.NewFile()
	kind, ref := excelize.STCellFormulaTypeArray, "C1:C20000"
	must(t, f.SetCellFormula("Sheet1", "A1", "[1]Other!A1"), f.MergeCell("Sheet1", "A1", "A20000"),
		f.SetCellFormula("Sheet1", "B1", "1+1"), f.MergeCell("Sheet1", "B1", "B20000"),
		f.SetCellFormula("Sheet1", "C1", "[1]Other!A1", excelize.FormulaOpts{Type: &kind, Ref: &ref}),
		f.SetCellFormula("Sheet1", "D1", "A15000+1"),
		f.SetCellFormula("Sheet1", "D2", "B15000+1"),
		f.SetCellFormula("Sheet1", "D3", "C15000+1"),
		f.SetCellFormula("Sheet1", "D4", "SUM(A19999:A20000)"),
		f.SetCellFormula("Sheet1", "D5", "A20001+1"))
	wantFormulas(t, readFormulas(t, f), map[string]string{"D2": "3", "D5": "1"}, "D1", "D3", "D4")
}

// 긴 사슬, 넓은 범위 참조에서도 재귀나 O(n^2) 폭발이 없어야 한다.
func TestFormulaBig(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	const n = 30000
	f := excelize.NewFile()
	sw, err := f.NewStreamWriter("Sheet1")
	must(t, err)
	for r := 1; r <= n; r++ {
		next := "1"
		if r < n {
			next = "A" + itoa(r+1) + "+1" // 아래를 참조하는 긴 사슬
		}
		must(t, sw.SetRow("A"+itoa(r), []any{
			excelize.Cell{Formula: next},
			excelize.Cell{Formula: "A" + itoa(r) + "*2"},
			excelize.Cell{Formula: "COUNT(A" + itoa(r) + ":B" + itoa(r) + ")"},
		}))
	}
	must(t, sw.Flush())
	p := save(t, f)
	start := time.Now()
	wb, err := Read(p, nil)
	must(t, err)
	t.Logf("수식 %d개: %v", 3*n, time.Since(start))
	got := cells(wb.Sheets[0])
	check(t, got, "A1", itoa(n), itoa(n))
	check(t, got, "B1", itoa(2*n), itoa(2*n))
	check(t, got, "C"+itoa(n), "2", "2")
}
