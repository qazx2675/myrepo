package report

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"excel-diff/internal/diff"
	"excel-diff/internal/reader"
)

const (
	defaultWidth = 9.140625 // Excel 기본 열 너비
	rowNumWidth  = 13       // "1048576 (숨김)"
	sepWidth     = 2
	headerRows   = 2 // 비교 시트의 머리 행 수
)

// sheetPlan 은 시트 짝 하나의 결과 시트 이름과 바로가기에 쓰는 위치 표이다.
type sheetPlan struct {
	sd            *diff.SheetDiff
	compare       []string // 열 조각 c, 행 조각 r 의 이름은 compare[c*rowParts+r]
	before, after string   // 그쪽 시트가 없으면 ""
	rowParts      int
	perRows       int // 비교 시트 하나의 데이터 행 수
	perCols       int // 비교 시트 하나의 열 짝 수

	// 원본 행·열 번호 -> sd.Rows, sd.Cols 첨자. [0] 은 이전, [1] 은 최신
	rows, cols [2]map[int]int
	changeAt   []int // 행 첨자 r 의 변경은 sd.Changes[changeAt[r]:changeAt[r+1]]
}

func (p *sheetPlan) names() []string {
	out := append([]string(nil), p.compare...)
	for _, n := range []string{p.before, p.after} {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func (w *writer) plan(sd *diff.SheetDiff, names namer) *sheetPlan {
	p := &sheetPlan{sd: sd, perRows: maxRows - headerRows, perCols: (maxCols - 3) / 2}
	p.rowParts = max(1, (len(sd.Rows)+p.perRows-1)/p.perRows)
	colParts := max(1, (len(sd.Cols)+p.perCols-1)/p.perCols)
	for i := range colParts * p.rowParts {
		suffix := "_비교"
		if i > 0 {
			suffix += fmt.Sprint(i + 1)
		}
		p.compare = append(p.compare, names.name(sd.Name(), suffix))
	}
	if sd.Old != nil {
		p.before = names.name(sd.Name(), "_Before")
	}
	if sd.New != nil {
		p.after = names.name(sd.Name(), "_After")
	}
	if p.rowParts > 1 {
		w.notes = append(w.notes, fmt.Sprintf("시트 '%s': 비교 시트가 행 한도를 넘어 %d행 단위로 나눴습니다", sd.Name(), p.perRows))
	}
	if colParts > 1 {
		w.notes = append(w.notes, fmt.Sprintf("시트 '%s': 좌우 배치가 열 한도를 넘어 비교 시트를 열 %d개 단위로 나눴습니다", sd.Name(), p.perCols))
	}

	for s := range 2 {
		p.rows[s], p.cols[s] = map[int]int{}, map[int]int{}
	}
	for k, c := range sd.Cols {
		if c.Old > 0 {
			p.cols[0][c.Old] = k
		}
		if c.New > 0 {
			p.cols[1][c.New] = k
		}
	}
	for r, pair := range sd.Rows {
		if pair.Old != nil {
			p.rows[0][pair.Old.Num] = r
		}
		if pair.New != nil {
			p.rows[1][pair.New.Num] = r
		}
	}
	p.changeAt = make([]int, len(sd.Rows)+1)
	for _, ch := range sd.Changes {
		p.changeAt[ch.Row+1]++
	}
	for r := range sd.Rows {
		p.changeAt[r+1] += p.changeAt[r]
	}
	return p
}

// width 는 원본 열 너비이다. 지정이 없거나 숨기려고 아주 좁힌 열은 기본 너비를 쓴다.
func width(s *reader.Sheet, col int) float64 {
	if s != nil && col > 0 {
		if v := s.Cols[col].Width; v >= 2 {
			return min(v, excelize.MaxColumnWidth)
		}
	}
	return defaultWidth
}

// cellFill 은 양쪽에 있는 행·열 위치의 배경색이다. side 는 0 이 이전, 1 이 최신이다.
func cellFill(pair diff.RowPair, col diff.ColPair, ch *diff.CellChange) fill {
	switch {
	case ch != nil && ch.Kind == diff.CellUnknown:
		return fillUnknown
	case pair.Status == diff.StatusAdded || col.Old == 0:
		return fillAdded
	case pair.Status == diff.StatusDeleted || col.New == 0:
		return fillDeleted
	case ch != nil:
		switch ch.Kind {
		case diff.CellAdded:
			return fillAdded
		case diff.CellDeleted:
			return fillDeleted
		}
		return fillModified
	case pair.Status == diff.StatusMoved:
		return fillMoved
	}
	return fillNone
}

func statusFill(s diff.Status) fill {
	return [...]fill{fillNone, fillModified, fillAdded, fillDeleted, fillMoved}[s]
}

// shown 은 셀에 쓸 문자열이다. 변경된 셀이 화면 표시만으로 구별되지 않으면 실제값을 보인다.
func shown(c *reader.Cell, ch *diff.CellChange) string {
	if ch == nil || c.Unknown {
		return c.Display
	}
	if c.Display == "" {
		return "(실제값 " + c.Key + ")"
	}
	if o, n := ch.Old, ch.New; o != nil && n != nil && !o.Unknown && !n.Unknown && o.Display == n.Display && o.Key != n.Key {
		return c.Display + " (실제값 " + c.Key + ")"
	}
	return c.Display
}

// value 는 원본 셀 하나를 결과 셀로 바꾼다. c 가 nil 이면 서식만 있는 빈 셀이다.
func (w *writer) value(sheet string, col, row int, c *reader.Cell, ch *diff.CellChange, width float64, k styleKey) any {
	if c == nil {
		return w.text(sheet, col, row, "", k)
	}
	s := shown(c, ch)
	if s == c.Key && !c.Unknown {
		if v, ok := number(s, width); ok {
			return excelize.Cell{StyleID: w.style(k), Value: v}
		}
	}
	return w.text(sheet, col, row, s, k)
}

// rowData 는 행 짝 하나의 셀과 변경을 열 첨자로 찾는 임시 표이다.
type rowData struct {
	cells   [2][]*reader.Cell
	changes []*diff.CellChange
	used    []int
}

func (p *sheetPlan) newRowData() *rowData {
	n := len(p.sd.Cols)
	return &rowData{cells: [2][]*reader.Cell{make([]*reader.Cell, n), make([]*reader.Cell, n)}, changes: make([]*diff.CellChange, n)}
}

func (d *rowData) load(p *sheetPlan, r int) {
	for _, k := range d.used {
		d.cells[0][k], d.cells[1][k], d.changes[k] = nil, nil, nil
	}
	d.used = d.used[:0]
	for s, row := range []*reader.Row{p.sd.Rows[r].Old, p.sd.Rows[r].New} {
		if row == nil {
			continue
		}
		for i := range row.Cells {
			k := p.cols[s][row.Cells[i].Col]
			d.cells[s][k] = &row.Cells[i]
			d.used = append(d.used, k)
		}
	}
	for i := p.changeAt[r]; i < p.changeAt[r+1]; i++ {
		k := p.sd.Changes[i].Col
		d.changes[k] = &p.sd.Changes[i]
		d.used = append(d.used, k)
	}
}

func (w *writer) writeSheets(p *sheetPlan) error {
	for i, name := range p.compare {
		c0 := i / p.rowParts * p.perCols
		r0 := i % p.rowParts * p.perRows
		if err := w.writeCompare(p, name, c0, min(c0+p.perCols, len(p.sd.Cols)), r0, min(r0+p.perRows, len(p.sd.Rows)),
			i < p.rowParts); err != nil { // 진행률은 첫 열 조각만 센다
			return err
		}
	}
	if p.before != "" {
		if err := w.writeSide(p, p.before, 0, p.sd.Old); err != nil {
			return err
		}
	}
	if p.after != "" {
		return w.writeSide(p, p.after, 1, p.sd.New)
	}
	return nil
}

// writeCompare 는 열 짝 [c0,c1), 행 짝 [r0,r1) 의 좌우 비교 시트를 쓴다.
// 열 배치: 이전 행 | 이전 열들 | 구분 | 최신 행 | 최신 열들
func (w *writer) writeCompare(p *sheetPlan, name string, c0, c1, r0, r1 int, count bool) error {
	sd := p.sd
	n := c1 - c0
	sep := n + 2
	start := [2]int{1, n + 3} // 각 쪽의 행 번호 열
	widths := make([]float64, 2*n+3)
	widths[start[0]-1], widths[sep-1], widths[start[1]-1] = rowNumWidth, sepWidth, rowNumWidth
	for i, c := range sd.Cols[c0:c1] {
		old, new := width(sd.Old, c.Old), width(sd.New, c.New)
		if c.Old == 0 { // 없는 쪽은 반대쪽 너비에 맞춘다
			old = new
		}
		if c.New == 0 {
			new = old
		}
		widths[start[0]+i], widths[start[1]+i] = old, new
	}
	sw, err := w.stream(name, headerRows, 1, widths)
	if err != nil {
		return err
	}
	if err := sw.SetColStyle(sep, sep, w.style(styleKey{fill: fillSeparator})); err != nil {
		return err
	}

	values := make([]any, len(widths))
	header := styleKey{fill: fillHeader, bold: true}
	for s, sheet := range []*reader.Sheet{sd.Old, sd.New} {
		title := [2]string{"Before (이전)", "After (최신)"}[s]
		switch {
		case sheet == nil:
			title += ": 시트 없음"
		case sheet.Hidden:
			title += ": " + sheet.Name + " (숨김 시트)"
		default:
			title += ": " + sheet.Name
		}
		at := start[s]
		if n > 0 {
			at++ // 좁은 행 번호 열(틀 고정)에 가리지 않게
		}
		values[at-1] = w.text(name, at, 1, title, header)
	}
	if err := setRow(sw, 1, values); err != nil {
		return err
	}
	clear(values)
	values[start[0]-1] = w.text(name, start[0], 2, "이전 행", header)
	values[start[1]-1] = w.text(name, start[1], 2, "최신 행", header)
	for i, c := range sd.Cols[c0:c1] {
		for s, side := range []struct {
			col    int
			hidden bool
		}{{c.Old, c.OldHidden}, {c.New, c.NewHidden}} {
			at := start[s] + 1 + i
			k := header
			label := ""
			if side.col > 0 {
				label, err = excelize.ColumnNumberToName(side.col)
				if err != nil {
					return err
				}
				var marks []string
				switch c.Status {
				case diff.StatusAdded:
					marks, k.fill = append(marks, "추가"), fillAdded
				case diff.StatusDeleted:
					marks, k.fill = append(marks, "삭제"), fillDeleted
				case diff.StatusMoved:
					marks, k.fill = append(marks, "이동"), fillMoved
				}
				if side.hidden {
					marks, k.hidden = append(marks, "숨김"), true
				}
				if len(marks) > 0 {
					label += " (" + strings.Join(marks, "·") + ")"
				}
			}
			values[at-1] = w.text(name, at, 2, label, k)
		}
	}
	if err := setRow(sw, 2, values); err != nil {
		return err
	}

	data := p.newRowData()
	for r := r0; r < r1; r++ {
		pair := sd.Rows[r]
		data.load(p, r)
		clear(values)
		num := r - r0 + headerRows + 1
		for s, row := range []*reader.Row{pair.Old, pair.New} {
			if row == nil {
				continue
			}
			at := start[s]
			k := styleKey{fill: statusFill(pair.Status), hidden: row.Hidden}
			if row.Hidden {
				values[at-1] = w.text(name, at, num, fmt.Sprintf("%d (숨김)", row.Num), k)
			} else {
				values[at-1] = excelize.Cell{StyleID: w.style(k), Value: row.Num}
			}
			for i, c := range sd.Cols[c0:c1] {
				col, colHidden := c.Old, c.OldHidden
				if s == 1 {
					col, colHidden = c.New, c.NewHidden
				}
				if col == 0 {
					continue
				}
				ch := data.changes[c0+i]
				at := start[s] + 1 + i
				values[at-1] = w.value(name, at, num, data.cells[s][c0+i], ch, widths[at-1],
					styleKey{fill: cellFill(pair, c, ch), hidden: row.Hidden || colHidden})
			}
		}
		if err := setRow(sw, num, values); err != nil {
			return err
		}
		if count {
			w.step()
		}
	}
	return sw.Flush()
}

// writeSide 는 원본 행·열 위치 그대로의 Before(side 0)·After(side 1) 시트를 쓴다.
func (w *writer) writeSide(p *sheetPlan, name string, side int, sheet *reader.Sheet) error {
	sd := p.sd
	last := 0
	for _, c := range sd.Cols {
		last = max(last, [2]int{c.Old, c.New}[side])
	}
	widths := make([]float64, last)
	for i := range widths {
		widths[i] = width(sheet, i+1)
	}
	sw, err := w.stream(name, 1, 0, widths)
	if err != nil {
		return err
	}
	data := p.newRowData()
	values := make([]any, last)
	for i := range sheet.Rows {
		row := &sheet.Rows[i]
		r := p.rows[side][row.Num]
		pair := sd.Rows[r]
		data.load(p, r)
		clear(values)
		for k, c := range sd.Cols {
			col, colHidden := c.Old, c.OldHidden
			if side == 1 {
				col, colHidden = c.New, c.NewHidden
			}
			if col == 0 {
				continue
			}
			ch := data.changes[k]
			values[col-1] = w.value(name, col, row.Num, data.cells[side][k], ch, widths[col-1],
				styleKey{fill: cellFill(pair, c, ch), hidden: row.Hidden || colHidden})
		}
		if err := setRow(sw, row.Num, values); err != nil {
			return err
		}
		w.step()
	}
	return sw.Flush()
}
