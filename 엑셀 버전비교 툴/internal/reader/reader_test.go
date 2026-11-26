package reader

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func save(t testing.TB, f *excelize.File) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "a.xlsx")
	if err := f.SaveAs(p); err != nil {
		t.Fatal(err)
	}
	return p
}

var itoa = strconv.Itoa

func must(t testing.TB, errs ...error) {
	t.Helper()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// patch 는 zip 항목 하나를 바꾼다. excelize 로 못 만드는 XML(저장된 수식값 등)용.
func patch(t *testing.T, p, entry, content string) {
	t.Helper()
	zr, err := zip.OpenReader(p)
	must(t, err)
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	found := false
	for _, f := range zr.File {
		w, err := zw.Create(f.Name)
		must(t, err)
		if f.Name == entry {
			found = true
			_, err = io.WriteString(w, content)
			must(t, err)
			continue
		}
		r, err := f.Open()
		must(t, err)
		_, err = io.Copy(w, r)
		must(t, err, r.Close())
	}
	must(t, zw.Close(), zr.Close(), os.WriteFile(p, out.Bytes(), 0o600))
	if !found {
		t.Fatal("no entry", entry)
	}
}

func cells(s Sheet) map[string]Cell {
	m := map[string]Cell{}
	for _, r := range s.Rows {
		for _, c := range r.Cells {
			n, _ := excelize.CoordinatesToCellName(c.Col, r.Num)
			m[n] = c
		}
	}
	return m
}

func check(t *testing.T, got map[string]Cell, ref, key, display string) {
	t.Helper()
	c, ok := got[ref]
	if !ok {
		t.Errorf("%s: missing", ref)
	} else if c.Key != key || c.Display != display {
		t.Errorf("%s: got %q/%q want %q/%q", ref, c.Key, c.Display, key, display)
	}
}

func TestValues(t *testing.T) {
	f := excelize.NewFile()
	style := func(id int, custom string) int {
		s := &excelize.Style{NumFmt: id}
		if custom != "" {
			s.CustomNumFmt = &custom
		}
		n, err := f.NewStyle(s)
		must(t, err)
		return n
	}
	set := func(ref string, v any, st int) {
		must(t, f.SetCellValue("Sheet1", ref, v))
		if st != 0 {
			must(t, f.SetCellStyle("Sheet1", ref, ref, st))
		}
	}
	comma := style(3, "")
	set("A1", 1000, 0)
	set("B1", 1000, comma)
	set("C1", "1000", 0)
	set("D1", "007", 0)
	set("E1", 7, 0)
	set("F1", "1.0", 0)
	set("A2", 1.001, style(2, ""))
	set("B2", 1.002, style(2, ""))
	set("C2", 0.30000000000000004, 0)
	set("D2", 45292, style(0, "yyyy-mm-dd"))
	set("E2", 45292, style(0, "yyyy/m/d"))
	set("F2", true, 0)
	set("G2", 45292, style(14, ""))
	set("A3", "  Ab\nc ", 0)
	set("B3", "", 0)
	must(t, f.SetCellStyle("Sheet1", "C3", "C3", comma)) // 서식만 있는 빈 셀
	set("D3", "병합", 0)
	must(t, f.MergeCell("Sheet1", "D3", "E4"))
	set("A6", "x", 0)
	must(t, f.SetRowVisible("Sheet1", 6, false),
		f.SetColVisible("Sheet1", "B", false),
		f.SetColWidth("Sheet1", "C", "C", 25))
	_, err := f.NewSheet("빈시트")
	must(t, err)
	_, err = f.NewSheet("숨김")
	must(t, err, f.SetSheetVisible("숨김", false), f.SetCellValue("숨김", "B2", "h"))
	must(t, f.AddChartSheet("차트", &excelize.Chart{Type: excelize.Col,
		Series: []excelize.ChartSeries{{Values: "Sheet1!$A$1:$B$1"}}}))
	p := save(t, f)

	before, err := os.ReadFile(p)
	must(t, err)
	info, err := os.Stat(p)
	must(t, err)
	last := -1
	wb, err := Read(p, func(done, total int) {
		if done < last || done > total {
			t.Errorf("progress %d/%d after %d", done, total, last)
		}
		last = done
	})
	must(t, err)
	if last != progressTotal {
		t.Error("progress end", last)
	}
	after, err := os.ReadFile(p)
	must(t, err)
	info2, err := os.Stat(p)
	must(t, err)
	if !bytes.Equal(before, after) || !info.ModTime().Equal(info2.ModTime()) {
		t.Error("source modified")
	}

	if len(wb.Sheets) != 3 || wb.Sheets[1].Name != "빈시트" || len(wb.Sheets[1].Rows) != 0 || !wb.Sheets[2].Hidden || wb.Sheets[0].Hidden {
		t.Fatalf("sheets: %+v", wb.Sheets)
	}
	if len(wb.Warnings) != 1 || wb.Warnings[0].Sheet != "차트" {
		t.Error("warnings", wb.Warnings)
	}
	s := wb.Sheets[0]
	got := cells(s)
	check(t, got, "A1", "1000", "1000")
	check(t, got, "B1", "1000", "1,000")
	check(t, got, "C1", "1000", "1000")
	check(t, got, "D1", "007", "007")
	check(t, got, "E1", "7", "7")
	check(t, got, "F1", "1.0", "1.0")
	check(t, got, "A2", "1.001", "1.00")
	check(t, got, "B2", "1.002", "1.00")
	check(t, got, "C2", "0.3", "0.3")
	check(t, got, "D2", "45292", "2024-01-01")
	check(t, got, "E2", "45292", "2024/1/1")
	check(t, got, "F2", "TRUE", "TRUE")
	check(t, got, "G2", "45292", "2024-01-01")
	check(t, got, "A3", "  Ab\nc ", "  Ab\nc ")
	check(t, got, "D3", "병합", "병합")
	check(t, got, "A6", "x", "x")
	if len(got) != 16 {
		t.Error("cell count", len(got), got)
	}
	if r := s.Rows[len(s.Rows)-1]; r.Num != 6 || !r.Hidden || s.Rows[0].Hidden {
		t.Error("rows", s.Rows)
	}
	if !s.Cols[2].Hidden || s.Cols[3].Hidden || s.Cols[3].Width != 25 {
		t.Error("cols", s.Cols)
	}
}

func TestFormulasAndRawTypes(t *testing.T) {
	f := excelize.NewFile()
	st, err := f.NewStyle(&excelize.Style{NumFmt: 3})
	must(t, err, f.SetCellValue("Sheet1", "A1", "seed"), f.SetCellStyle("Sheet1", "Z9", "Z9", st))
	p := save(t, f)
	s := itoa(st)
	patch(t, p, "xl/worksheets/sheet1.xml", `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1"><v>600</v></c><c r="B1" s="`+s+`"><f>A1*2</f><v>1234</v></c><c r="C1" s="`+s+`"><f>A1*2</f></c><c r="D1"><f>A1+1</f><v></v></c>
<c r="E1" t="str"><f>""</f><v></v></c><c r="F1"><f>NOSUCHFUNC(A1)</f></c><c r="G1" t="e"><f>1/0</f><v>#DIV/0!</v></c>
<c r="H1" t="str"><f>"00"&amp;"7"</f><v>007</v></c><c r="I1"><f>"00"&amp;"7"</f></c><c r="J1"><f>""</f></c>
<c r="K1" t="inlineStr"><is><r><t>in</t></r><r><t>line_x000D_</t></r><rPh sb="0" eb="1"><t>NO</t></rPh></is></c>
<c r="L1" t="d"><v>2024-01-01</v></c><c r="M1" t="e"><v>#N/A</v></c><c r="N1"><v>1E3</v></c></row>
<row r="3"><c r="A3" t="str"><f>""</f><v></v></c></row>
</sheetData></worksheet>`)
	wb, err := Read(p, nil)
	must(t, err)
	sh := wb.Sheets[0]
	got := cells(sh)
	check(t, got, "A1", "600", "600")
	check(t, got, "B1", "1234", "1,234")
	check(t, got, "C1", "1200", "1,200")
	check(t, got, "D1", "601", "601")
	check(t, got, "F1", unknownKey, unknownDisplay)
	check(t, got, "G1", "#DIV/0!", "#DIV/0!")
	check(t, got, "H1", "007", "007")
	check(t, got, "I1", "007", "007")
	check(t, got, "K1", "inline\r", "inline\r")
	check(t, got, "M1", "#N/A", "#N/A")
	check(t, got, "N1", "1000", "1000")
	if got["L1"].Key != "45292" {
		t.Error("L1", got["L1"])
	}
	for _, ref := range []string{"E1", "J1", "A3"} {
		if c, ok := got[ref]; ok {
			t.Error("should be empty", ref, c)
		}
	}
	if !got["F1"].Unknown || got["C1"].Unknown || len(sh.Rows) != 1 {
		t.Error("flags/rows", sh.Rows)
	}
	if len(wb.Warnings) != 2 || wb.Warnings[0].Cell != "F1" || !strings.Contains(wb.Warnings[1].Message, "자체 계산한 수식 4개") {
		t.Error("warnings", wb.Warnings)
	}
}

func TestErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		must(t, os.WriteFile(p, b, 0o600))
		return p
	}
	f := excelize.NewFile()
	enc := filepath.Join(dir, "enc.xlsx")
	must(t, f.SaveAs(enc, excelize.Options{Password: "pw"}))
	good, err := os.ReadFile(save(t, excelize.NewFile()))
	must(t, err)
	badXML := save(t, excelize.NewFile())
	patch(t, badXML, "xl/worksheets/sheet1.xml", "<worksheet><sheetData><row r=\"1\"><c")
	for p, want := range map[string]string{
		write("a.xls", oleMagic):              ".xls",
		write("a.csv", []byte("1,2")):         "CSV",
		write("a.txt", []byte("1")):           "지원하지",
		enc:                                   "암호",
		write("bad.xlsx", []byte("garbage!")): "손상",
		write("cut.xlsx", good[:len(good)/2]): "손상",
		write("empty.xlsx", nil):              "손상",
		badXML:                                "손상",
		filepath.Join(dir, "none.xlsx"):       "읽을 수 없습니다",
	} {
		if _, err := Read(p, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v (want %q)", filepath.Base(p), err, want)
		}
	}
}

// 대용량 측정: READER_BIG=1 go test -run Big -v ./internal/reader
func TestBig(t *testing.T) {
	if os.Getenv("READER_BIG") == "" {
		t.Skip("READER_BIG 미설정")
	}
	f := excelize.NewFile()
	date, err := f.NewStyle(&excelize.Style{NumFmt: 14})
	must(t, err)
	comma, err := f.NewStyle(&excelize.Style{NumFmt: 4})
	must(t, err)
	sw, err := f.NewStreamWriter("Sheet1")
	must(t, err)
	row := make([]any, 40)
	for r := 1; r <= 30000; r++ {
		for c := range row {
			switch c % 4 {
			case 0:
				row[c] = "서버-" + itoa(r%500)
			case 1:
				row[c] = excelize.Cell{StyleID: comma, Value: float64(r*c) + 0.25}
			case 2:
				row[c] = excelize.Cell{StyleID: date, Value: 45000 + r%900}
			default:
				row[c] = r + c
			}
		}
		ref, _ := excelize.CoordinatesToCellName(1, r)
		must(t, sw.SetRow(ref, row))
	}
	must(t, sw.Flush())
	p := save(t, f)
	f = nil
	runtime.GC()

	stop, peak := make(chan struct{}), make(chan uint64)
	go func() {
		var m runtime.MemStats
		var max uint64
		for {
			runtime.ReadMemStats(&m)
			if v := m.Sys - m.HeapReleased; v > max {
				max = v
			}
			select {
			case <-stop:
				peak <- max
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	start := time.Now()
	wb, err := Read(p, nil)
	elapsed := time.Since(start)
	close(stop)
	mem := <-peak
	must(t, err)
	n := 0
	for _, r := range wb.Sheets[0].Rows {
		n += len(r.Cells)
	}
	st, _ := os.Stat(p)
	t.Logf("cells=%d file=%.1fMB time=%v peak=%.0fMB sample=%+v", n, float64(st.Size())/1e6, elapsed, float64(mem)/1e6, wb.Sheets[0].Rows[1].Cells[:4])
	if n != 40*30000 {
		t.Fatal("cell count")
	}
}
