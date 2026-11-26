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
