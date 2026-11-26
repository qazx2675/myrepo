package report

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"excel-diff/internal/describe"
)

var (
	logHeader = []string{"번호", "시트", "구분", "위치(이전)", "위치(최신)", "이전값", "최신값", "한글 설명", "바로가기"}
	logWidths = []float64{8, 20, 10, 15, 15, 35, 35, 80, 12}
)

// logRow 는 변경내역의 행 하나이다.
type logRow struct {
	item              *describe.Item
	old, new, explain string
}

// logRows 는 항목을 행으로 바꾼다. XML 표기 때문에 셀 한도를 넘게 된 값은 잘라내지 않고 여러 행으로 나눈다.
func (w *writer) logRows() []logRow {
	rows := make([]logRow, 0, len(w.src.Items))
	split := 0
	for i := range w.src.Items {
		it := &w.src.Items[i]
		old, new, explain := chunks(xmlSafe(it.OldValue)), chunks(xmlSafe(it.NewValue)), chunks(xmlSafe(it.Text))
		n := max(len(old), len(new), len(explain))
		if n > 1 {
			split++
		}
		at := func(s []string, i int) string {
			if i < len(s) {
				return s[i]
			}
			return ""
		}
		for k := range n {
			rows = append(rows, logRow{it, at(old, k), at(new, k), at(explain, k)})
		}
	}
	if split > 0 {
		w.notes = append(w.notes, fmt.Sprintf("셀 글자 수 한도를 넘는 변경내역 %d건은 같은 번호의 여러 행으로 나눴습니다", split))
	}
	return rows
}

func kindFill(k describe.Kind) fill {
	switch k {
	case describe.KindModified:
		return fillModified
	case describe.KindAdded:
		return fillAdded
	case describe.KindDeleted:
		return fillDeleted
	case describe.KindMoved:
		return fillMoved
	case describe.KindUnknown:
		return fillUnknown
	}
	return fillHeader
}

// target 은 항목의 바로가기 위치(비교 시트의 행·셀)이다. 비교에서 제외한 시트면 "" 이다.
// 최신 쪽 행이 있으면 최신 쪽, 없으면 이전 쪽을 가리키고, 행이 없는 열 항목은 열 머리를 가리킨다.
func (w *writer) target(it *describe.Item) (sheet, cell string, err error) {
	if it.SheetIndex < 0 {
		return "", "", nil
	}
	p := w.sheets[it.SheetIndex]
	side, ref := 1, it.New
	if it.New.Row == 0 && (it.Old.Row > 0 || it.New.Col == 0) {
		side, ref = 0, it.Old
	}
	if ref == (describe.Ref{}) { // 시트 항목
		return p.compare[0], "A1", nil
	}
	r, k := -1, -1
	if i, ok := p.rows[side][ref.Row]; ok {
		r = i
	}
	if i, ok := p.cols[side][ref.Col]; ok {
		k = i
	}
	colPart, rowPart := max(k, 0)/p.perCols, max(r, 0)/p.perRows
	n := min(p.perCols, len(p.sd.Cols)-colPart*p.perCols)
	col := [2]int{1, n + 3}[side]
	if k >= 0 {
		col += 1 + k - colPart*p.perCols
	}
	row := headerRows
	if r >= 0 {
		row += 1 + r - rowPart*p.perRows
	}
	cell, err = excelize.CoordinatesToCellName(col, row)
	return p.compare[colPart*p.rowParts+rowPart], cell, err
}

func (w *writer) writeLog(rows []logRow) error {
	per := maxRows - 1
	unlinked := 0
	for part, name := range w.logs {
		chunk := rows[min(part*per, len(rows)):min((part+1)*per, len(rows))]
		sw, err := w.stream(name, 1, 0, logWidths)
		if err != nil {
			return err
		}
		values := make([]any, len(logHeader))
		for i, h := range logHeader {
			values[i] = w.text(name, i+1, 1, h, styleKey{fill: fillHeader, bold: true})
		}
		if err := setRow(sw, 1, values); err != nil {
			return err
		}
		links := 0
		for i, row := range chunk {
			it, num := row.item, i+2
			plain := styleKey{wrap: true}
			text := func(col int, s string, k styleKey) { values[col-1] = w.text(name, col, num, s, k) }
			clear(values)
			values[0] = excelize.Cell{StyleID: w.style(plain), Value: it.No}
			text(2, it.Sheet, plain)
			text(3, it.Kind.String(), styleKey{fill: kindFill(it.Kind), wrap: true})
			text(4, it.OldPos, plain)
			text(5, it.NewPos, plain)
			text(6, row.old, plain)
			text(7, row.new, plain)
			text(8, row.explain, plain)
			sheet, cell, err := w.target(it)
			if err != nil {
				return err
			}
			switch {
			case sheet == "":
			case links < maxLinks:
				links++
				at, err := excelize.CoordinatesToCellName(len(logHeader), num)
				if err != nil {
					return err
				}
				location := "'" + strings.ReplaceAll(sheet, "'", "''") + "'!" + cell
				if err := w.f.SetCellHyperLink(name, at, location, "Location"); err != nil {
					return err
				}
				text(9, "바로가기", styleKey{link: true, wrap: true})
			default:
				unlinked++
				text(9, sheet+" "+cell, plain)
			}
			if err := setRow(sw, num, values); err != nil {
				return err
			}
			w.step()
		}
		end, err := excelize.CoordinatesToCellName(len(logHeader), len(chunk)+1)
		if err != nil {
			return err
		}
		if err := w.f.AutoFilter(name, "A1:"+end, nil); err != nil {
			return err
		}
		if err := sw.Flush(); err != nil {
			return err
		}
	}
	if unlinked > 0 {
		w.notes = append(w.notes, fmt.Sprintf("시트당 하이퍼링크 한도(%d개)를 넘은 변경내역 %d행은 바로가기 대신 위치를 글자로 적었습니다", maxLinks, unlinked))
	}
	return nil
}
