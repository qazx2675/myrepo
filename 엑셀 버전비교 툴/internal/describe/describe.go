package describe

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/xuri/excelize/v2"

	"excel-diff/internal/diff"
	"excel-diff/internal/reader"
)

// Limit 은 Excel 셀 하나의 글자 수 한도(UTF-16 단위)이다.
// Item 의 문자열 필드는 이 길이를 넘지 않는다.
const Limit = 32767

// Kind 는 변경내역의 "구분"이다.
type Kind uint8

const (
	KindModified  Kind = iota + 1 // 수정
	KindAdded                     // 행 추가
	KindDeleted                   // 행 삭제
	KindMoved                     // 행 이동(값 변경 동반 포함)
	KindUnknown                   // 항목의 모든 셀 변경이 확인 불가
	KindStructure                 // 시트·열
)

// String 은 "구분" 열에 쓰는 이름이다.
func (k Kind) String() string {
	return [...]string{"", "수정", "추가", "삭제", "이동", "확인 불가", "시트·열"}[k]
}

// Ref 는 바로가기 대상인 원본 행·열 번호(1부터)이다. 없으면 0.
type Ref struct{ Row, Col int }

// Item 은 변경내역 한 줄이다.
type Item struct {
	No         int    // 1부터
	SheetIndex int    // diff.Result.Sheets 의 첨자. 비교에서 제외한 시트는 -1
	Sheet      string // 최신 시트명(삭제·이전 파일 제외 시트는 이전 시트명)
	Kind       Kind
	OldPos     string // "B3", "A3~A10", "15행", "20~35행", "C열"
	NewPos     string
	OldValue   string
	NewValue   string
	Text       string // 한글 설명
	Old, New   Ref    // 범위·여러 셀이면 첫 행·첫 열

	// 이 항목이 담은 diff.SheetDiff 의 Changes·Rows·Cols 첨자(검증용).
	// 나뉜 항목은 첫 조각만 가지며 이어지는 조각은 cont 이다.
	changes, rows, cols []int
	cont                bool
}

// Describe 는 비교 결과를 변경내역 항목으로 바꾼다. pairs 는 diff.Compare 에 준 것으로
// 키 열(행 이름표)을 알기 위해서만 쓰며 nil 이어도 된다.
//
// 순서는 시트별로 시트 항목, 열 항목, 행 항목(좌우 배치 순서)이고 제외한 시트가 마지막이다.
func Describe(r *diff.Result, pairs []diff.SheetPair) []Item {
	b := &builder{}
	for i := range r.Sheets {
		var p diff.SheetPair
		if i < len(pairs) {
			p = pairs[i]
		}
		b.sheet(i, &r.Sheets[i], p)
	}
	for _, side := range []struct {
		label string
		names []string
		old   bool
	}{{"이전", r.ExcludedOld, true}, {"최신", r.ExcludedNew, false}} {
		for _, name := range side.names {
			it := Item{SheetIndex: -1, Sheet: name, Kind: KindStructure,
				Text: fmt.Sprintf("%s 파일의 시트 '%s': 비교에서 제외(내용 비교 안 함)", side.label, visible(name, false))}
			if side.old {
				it.OldPos = "시트 전체"
			} else {
				it.NewPos = "시트 전체"
			}
			b.add(it)
		}
	}
	return b.items
}

type builder struct{ items []Item }

// add 는 번호를 매겨 추가한다. 문자열이 Limit 을 넘으면 여러 항목으로 나눈다(잘라내지 않음).
func (b *builder) add(it Item) {
	if fits(it.Text) && fits(it.OldValue) && fits(it.NewValue) {
		it.No = len(b.items) + 1
		b.items = append(b.items, it)
		return
	}
	const mark = 32 // " (12/34)" 자리
	text, old, new := split(it.Text, Limit-mark), split(it.OldValue, Limit), split(it.NewValue, Limit)
	n := max(len(text), len(old), len(new))
	at := func(s []string, i int) string {
		if i < len(s) {
			return s[i]
		}
		return ""
	}
	for i := range n {
		part := it
		part.OldValue, part.NewValue = at(old, i), at(new, i)
		t := at(text, i)
		if t == "" {
			t = "(이어짐)"
		}
		part.Text = fmt.Sprintf("%s (%d/%d)", t, i+1, n)
		if i > 0 {
			part.changes, part.rows, part.cols, part.cont = nil, nil, nil, true
		}
		part.No = len(b.items) + 1
		b.items = append(b.items, part)
	}
}

// side 는 한쪽 시트의 머리글과 행 이름표 정보이다.
type side struct {
	header   *reader.Row // 머리글 행, 없으면 nil
	labelCol int         // 행 이름표 열
}

func newSide(s *reader.Sheet, keyCol int) side {
	if s == nil || len(s.Rows) == 0 {
		return side{}
	}
	// 머리글은 처음 10개 행 중 칸이 충분히 채워진 첫 행(한 칸짜리 제목 행은 건너뜀).
	top := s.Rows[:min(10, len(s.Rows))]
	widest := 0
	for i := range top {
		widest = max(widest, len(top[i].Cells))
	}
	out := side{labelCol: keyCol}
	for i := range top {
		if len(top[i].Cells) >= max(min(2, widest), (widest+1)/2) {
			out.header = &top[i]
			break
		}
	}
	if keyCol <= 0 {
		for i := range s.Rows {
			if c := s.Rows[i].Cells[0].Col; out.labelCol <= 0 || c < out.labelCol {
				out.labelCol = c
			}
		}
	}
	return out
}

func cellAt(row *reader.Row, col int) *reader.Cell {
	if row == nil {
		return nil
	}
	i, ok := slices.BinarySearchFunc(row.Cells, col, func(c reader.Cell, n int) int { return cmp.Compare(c.Col, n) })
	if !ok {
		return nil
	}
	return &row.Cells[i]
}

// name 은 열의 머리글이다. row 가 머리글 행이거나 그 위면 붙이지 않는다.
func (s side) name(row *reader.Row, col int) string {
	if s.header == nil || row != nil && row.Num <= s.header.Num {
		return ""
	}
	c := cellAt(s.header, col)
	if c == nil || c.Unknown {
		return ""
	}
	return short(c.Display, 30)
}

// label 은 " (web01)" 형태의 행 이름표이다.
func (s side) label(row *reader.Row) string {
	if row == nil || s.header != nil && row.Num <= s.header.Num {
		return ""
	}
	c := cellAt(row, s.labelCol)
	if c == nil || c.Unknown || c.Display == "" {
		return ""
	}
	return " (" + short(c.Display, 30) + ")"
}

func colName(n int) string {
	s, err := excelize.ColumnNumberToName(n)
	if err != nil { // 시트에서 읽은 열 번호는 항상 범위 안이다
		return fmt.Sprint(n)
	}
	return s
}

func address(col, row int) string {
	if col == 0 {
		return ""
	}
	return colName(col) + fmt.Sprint(row)
}

func hidden(h bool) string {
	if h {
		return "(숨김)"
	}
	return ""
}

// sheetBuilder 는 시트 하나를 처리한다.
type sheetBuilder struct {
	*builder
	sd       *diff.SheetDiff
	index    int
	suffix   string
	old, new side
	run      run
}

func (b *builder) sheet(index int, sd *diff.SheetDiff, p diff.SheetPair) {
	s := &sheetBuilder{builder: b, sd: sd, index: index,
		old: newSide(sd.Old, p.OldKeyCol), new: newSide(sd.New, p.NewKeyCol)}
	if sd.Old != nil && sd.Old.Hidden || sd.New != nil && sd.New.Hidden {
		s.suffix = " [시트(숨김)]"
	}
	s.sheetItem()

	// 셀 변경을 행 항목과 열 항목으로 나눈다. 추가·삭제 행의 셀은 모두 행 항목이고,
	// 짝지은 행에서 추가·삭제 열의 셀은 열 항목이다.
	rowChanges := make([][]int, len(sd.Rows))
	colChanges := make([][]int, len(sd.Cols))
	for i, ch := range sd.Changes {
		row, col := sd.Rows[ch.Row], sd.Cols[ch.Col]
		if row.Old != nil && row.New != nil && (col.Old == 0 || col.New == 0) {
			colChanges[ch.Col] = append(colChanges[ch.Col], i)
		} else {
			rowChanges[ch.Row] = append(rowChanges[ch.Row], i)
		}
	}
	if sd.Old != nil && sd.New != nil { // 추가·삭제 시트의 열은 시트 항목이 대신한다
		for k, c := range sd.Cols {
			if c.Status != diff.StatusSame {
				s.column(k, colChanges[k])
			}
		}
	}
	for r := range sd.Rows {
		s.row(r, rowChanges[r])
	}
	s.flush()
}

func (s *sheetBuilder) emit(it Item) {
	it.SheetIndex, it.Sheet = s.index, s.sd.Name()
	it.Text += s.suffix
	s.add(it)
}

func (s *sheetBuilder) sheetItem() {
	sd := s.sd
	q := func(sh *reader.Sheet) string { return "'" + visible(sh.Name, false) + "'" }
	it := Item{Kind: KindStructure}
	switch {
	case sd.Old == nil:
		it.NewPos, it.NewValue = "시트 전체", sd.New.Name
		it.Text = fmt.Sprintf("시트 %s 추가 (%d개 행)", q(sd.New), len(sd.Rows))
	case sd.New == nil:
		it.OldPos, it.OldValue = "시트 전체", sd.Old.Name
		it.Text = fmt.Sprintf("시트 %s 삭제 (%d개 행)", q(sd.Old), len(sd.Rows))
	case sd.Renamed():
		it.OldPos, it.NewPos = "시트 이름", "시트 이름"
		it.OldValue, it.NewValue = sd.Old.Name, sd.New.Name
		it.Text = fmt.Sprintf("시트 이름 변경: %s → %s", q(sd.Old), q(sd.New))
	default:
		return
	}
	s.emit(it)
}

// column 은 추가·삭제·이동 열의 항목을 만든다. 값 목록이 Limit 을 넘으면 셀 단위로 항목을 나눈다.
func (s *sheetBuilder) column(k int, changes []int) {
	c := s.sd.Cols[k]
	named := func(sd side, col int, h bool) string {
		out := colName(col) + "열"
		if n := sd.name(nil, col); n != "" {
			out += "(" + n + ")"
		}
		return out + hidden(h)
	}
	base := Item{Kind: KindStructure, Old: Ref{Col: c.Old}, New: Ref{Col: c.New}}
	if c.Old > 0 {
		base.OldPos = colName(c.Old) + "열"
	}
	if c.New > 0 {
		base.NewPos = colName(c.New) + "열"
	}
	var head string
	switch c.Status {
	case diff.StatusMoved: // 짝지은 열의 셀 변경은 행 항목에 있다
		base.cols = []int{k}
		base.Text = named(s.old, c.Old, c.OldHidden) + " → " + named(s.new, c.New, c.NewHidden) + " 열 이동"
		s.emit(base)
		return
	case diff.StatusAdded:
		head = named(s.new, c.New, c.NewHidden) + " 열 추가"
	default:
		head = named(s.old, c.Old, c.OldHidden) + " 열 삭제"
	}

	var value strings.Builder
	var part []int
	size, unknown, parts := 0, 0, 0
	flush := func(last bool) {
		it := base
		it.changes = part
		if parts == 0 {
			it.cols = []int{k}
		}
		it.Text = head
		if len(part) > 0 { // 0 이면 값이 모두 추가·삭제 행 항목에 있다
			it.Text += fmt.Sprintf(" (%d개 값)", len(part))
		}
		if unknown > 0 {
			it.Text += fmt.Sprintf(", 값 확인 불가 %d개 포함", unknown)
		}
		if parts > 0 || !last {
			it.Text += fmt.Sprintf(" [나눔 %d]", parts+1)
		}
		if len(part) > 0 {
			row := s.sd.Rows[s.sd.Changes[part[0]].Row]
			if c.Status == diff.StatusAdded {
				it.New.Row = row.New.Num
			} else {
				it.Old.Row = row.Old.Num
			}
		}
		if c.Status == diff.StatusAdded {
			it.NewValue = value.String()
		} else {
			it.OldValue = value.String()
		}
		s.emit(it)
		parts++
		value.Reset()
		part, size, unknown = nil, 0, 0
	}
	for _, i := range changes {
		ch := s.sd.Changes[i]
		row := s.sd.Rows[ch.Row]
		var line string
		var cell *reader.Cell
		if c.Status == diff.StatusAdded {
			cell = ch.New
			line = address(c.New, row.New.Num) + "=" + listed(cell)
		} else {
			cell = ch.Old
			line = address(c.Old, row.Old.Num) + "=" + listed(cell)
		}
		n := length(line)
		if len(part) > 0 && size+1+n > Limit {
			flush(false)
		}
		if len(part) > 0 {
			value.WriteByte('\n')
			size++
		}
		value.WriteString(line)
		size += n
		part = append(part, i)
		if cell.Unknown {
			unknown++
		}
	}
	flush(true)
}

// run 은 다음 행과 묶일 수 있어 아직 내보내지 않은 행 묶음이다.
type run struct {
	kind    Kind // 0 이면 비어 있음. KindModified 는 한 셀 변경 범위(확인 불가 포함)
	rows    []int
	changes []int

	value         strings.Builder // 추가·삭제 행의 값 목록
	size, unknown int
}

func (s *sheetBuilder) row(r int, changes []int) {
	p := s.sd.Rows[r]
	switch {
	case p.Old == nil:
		s.whole(KindAdded, r, p.New, changes)
	case p.New == nil:
		s.whole(KindDeleted, r, p.Old, changes)
	case p.Status == diff.StatusMoved && len(changes) == 0:
		if !s.joins(KindMoved, p) {
			s.flush()
			s.run.kind = KindMoved
		}
		s.run.rows = append(s.run.rows, r)
	case p.Status == diff.StatusMoved || len(changes) > 1:
		s.flush()
		s.rowItem(r, changes)
	case len(changes) == 1:
		if !s.joins(KindModified, p) || !s.sameChange(s.run.changes[0], changes[0]) {
			s.flush()
			s.run.kind = KindModified
		}
		s.run.rows = append(s.run.rows, r)
		s.run.changes = append(s.run.changes, changes[0])
	}
}

// joins 는 행이 현재 묶음의 바로 다음 행(양쪽 번호 연속, 숨김 상태 같음)인지 알려준다.
func (s *sheetBuilder) joins(kind Kind, p diff.RowPair) bool {
	if s.run.kind != kind {
		return false
	}
	last := s.sd.Rows[s.run.rows[len(s.run.rows)-1]]
	next := func(a, b *reader.Row) bool {
		if a == nil || b == nil {
			return a == b
		}
		return b.Num == a.Num+1 && a.Hidden == b.Hidden
	}
	return next(last.Old, p.Old) && next(last.New, p.New)
}

func (s *sheetBuilder) sameChange(i, j int) bool {
	a, b := s.sd.Changes[i], s.sd.Changes[j]
	same := func(x, y *reader.Cell) bool {
		if x == nil || y == nil {
			return x == y
		}
		return x.Key == y.Key && x.Display == y.Display
	}
	return a.Col == b.Col && a.Kind == b.Kind && same(a.Old, b.Old) && same(a.New, b.New)
}

// whole 은 추가·삭제 행을 묶음에 넣는다. 값 목록이 Limit 을 넘기 전에 행 단위로 나눈다.
func (s *sheetBuilder) whole(kind Kind, r int, row *reader.Row, changes []int) {
	var line strings.Builder
	fmt.Fprintf(&line, "%d행: ", row.Num)
	unknown := 0
	for n, i := range changes {
		ch := s.sd.Changes[i]
		cell, col := ch.New, s.sd.Cols[ch.Col].New
		if kind == KindDeleted {
			cell, col = ch.Old, s.sd.Cols[ch.Col].Old
		}
		if n > 0 {
			line.WriteString(", ")
		}
		line.WriteString(colName(col))
		line.WriteByte('=')
		line.WriteString(listed(cell))
		if cell.Unknown {
			unknown++
		}
	}
	size := length(line.String())
	if !s.joins(kind, s.sd.Rows[r]) || s.run.size+1+size > Limit {
		s.flush()
		s.run.kind = kind
	}
	if len(s.run.rows) > 0 {
		s.run.value.WriteByte('\n')
		s.run.size++
	}
	s.run.value.WriteString(line.String())
	s.run.size += size
	s.run.unknown += unknown
	s.run.rows = append(s.run.rows, r)
	s.run.changes = append(s.run.changes, changes...)
}

// span 은 "5" 또는 "5~9" 이다.
func span(a, b int) string {
	if a == b {
		return fmt.Sprint(a)
	}
	return fmt.Sprintf("%d~%d", a, b)
}

// flush 는 쌓인 행 묶음을 항목으로 내보낸다.
func (s *sheetBuilder) flush() {
	u := &s.run
	if u.kind == 0 {
		return
	}
	defer func() { s.run = run{} }()
	if u.kind == KindModified {
		if len(u.rows) == 1 {
			s.rowItem(u.rows[0], u.changes)
		} else {
			s.rangeItem(u.rows, u.changes)
		}
		return
	}

	first, last := s.sd.Rows[u.rows[0]], s.sd.Rows[u.rows[len(u.rows)-1]]
	it := Item{Kind: u.kind, rows: u.rows, changes: u.changes}
	var oldText, newText string
	if first.Old != nil {
		it.OldPos, it.Old.Row = span(first.Old.Num, last.Old.Num)+"행", first.Old.Num
		oldText = it.OldPos + hidden(first.Old.Hidden)
	}
	if first.New != nil {
		it.NewPos, it.New.Row = span(first.New.Num, last.New.Num)+"행", first.New.Num
		newText = it.NewPos + hidden(first.New.Hidden)
	}
	if len(u.changes) > 0 { // 추가·삭제 행의 첫 값이 있는 열
		c := s.sd.Cols[s.sd.Changes[u.changes[0]].Col]
		if first.Old != nil {
			it.Old.Col = c.Old
		}
		if first.New != nil {
			it.New.Col = c.New
		}
	}
	switch u.kind {
	case KindAdded:
		it.NewValue = u.value.String()
		it.Text = newText + " 추가"
	case KindDeleted:
		it.OldValue = u.value.String()
		it.Text = oldText + " 삭제"
	case KindMoved:
		it.Text = oldText + " → " + newText + " 이동"
	}
	switch {
	case len(u.rows) > 1:
		it.Text += fmt.Sprintf(" (%d개 행)", len(u.rows))
	case first.New != nil:
		it.Text += s.new.label(first.New)
	default:
		it.Text += s.old.label(first.Old)
	}
	if u.unknown > 0 {
		it.Text += fmt.Sprintf(", 값 확인 불가 %d개 포함", u.unknown)
	}
	s.emit(it)
}

// rowItem 은 짝지은 행 하나의 셀 변경(이동 포함)을 항목 하나로 만든다.
func (s *sheetBuilder) rowItem(r int, changes []int) {
	p := s.sd.Rows[r]
	it := Item{Kind: KindModified, rows: []int{r}, changes: changes,
		Old: Ref{Row: p.Old.Num}, New: Ref{Row: p.New.Num},
		OldPos: fmt.Sprintf("%d행", p.Old.Num), NewPos: fmt.Sprintf("%d행", p.New.Num)}

	var head string
	oldMark, newMark := hidden(p.Old.Hidden), hidden(p.New.Hidden)
	switch {
	case p.Status == diff.StatusMoved:
		it.Kind = KindMoved
		head = fmt.Sprintf("%d행%s → %d행%s 이동", p.Old.Num, oldMark, p.New.Num, newMark)
	case p.Old.Num != p.New.Num:
		head = fmt.Sprintf("%d행%s → %d행%s", p.Old.Num, oldMark, p.New.Num, newMark)
	default:
		head = fmt.Sprintf("%d행%s", p.New.Num, hidden(p.Old.Hidden || p.New.Hidden))
	}
	head += s.new.label(p.New)

	var texts, olds, news []string
	unknown := 0
	for _, i := range changes {
		ch := s.sd.Changes[i]
		c := s.sd.Cols[ch.Col] // 여기의 열은 양쪽에 있다
		letter := colName(c.New) + "열"
		if c.Old != c.New {
			letter = colName(c.Old) + "열→" + letter
		}
		name := s.new.name(p.New, c.New)
		if name == "" {
			name = s.old.name(p.Old, c.Old)
		}
		if name != "" {
			letter += "(" + name + ")"
		}
		before, after, n := values(ch)
		texts = append(texts, letter+hidden(c.OldHidden || c.NewHidden)+" "+phrase(ch, n))
		if len(changes) == 1 {
			olds, news = append(olds, before), append(news, after)
			it.OldPos, it.NewPos = address(c.Old, p.Old.Num), address(c.New, p.New.Num)
		} else {
			olds = append(olds, colName(c.Old)+": "+before)
			news = append(news, colName(c.New)+": "+after)
		}
		if it.Old.Col == 0 {
			it.Old.Col, it.New.Col = c.Old, c.New
		}
		if ch.Kind == diff.CellUnknown {
			unknown++
		}
	}
	if len(changes) > 0 {
		head += ": " + strings.Join(texts, ", ")
		if it.Kind == KindModified && unknown == len(changes) {
			it.Kind = KindUnknown
		}
	}
	it.Text, it.OldValue, it.NewValue = head, strings.Join(olds, "\n"), strings.Join(news, "\n")
	s.emit(it)
}

// rangeItem 은 같은 열의 같은 변화가 연속한 행을 묶는다.
func (s *sheetBuilder) rangeItem(rows, changes []int) {
	first, last := s.sd.Rows[rows[0]], s.sd.Rows[rows[len(rows)-1]]
	ch := s.sd.Changes[changes[0]]
	c := s.sd.Cols[ch.Col]
	cells := func(col, a, b int) string { return address(col, a) + "~" + address(col, b) }
	it := Item{Kind: KindModified, rows: rows, changes: changes,
		OldPos: cells(c.Old, first.Old.Num, last.Old.Num), NewPos: cells(c.New, first.New.Num, last.New.Num),
		Old: Ref{first.Old.Num, c.Old}, New: Ref{first.New.Num, c.New}}
	if ch.Kind == diff.CellUnknown {
		it.Kind = KindUnknown
	}
	oldHidden, newHidden := first.Old.Hidden || c.OldHidden, first.New.Hidden || c.NewHidden
	var where string
	if it.OldPos == it.NewPos {
		where = it.NewPos + hidden(oldHidden || newHidden)
	} else {
		where = "이전 " + it.OldPos + hidden(oldHidden) + " → 최신 " + it.NewPos + hidden(newHidden)
	}
	before, after, n := values(ch)
	it.OldValue, it.NewValue = before, after
	it.Text = fmt.Sprintf("%s (%d개 셀): %s", where, len(rows), phrase(ch, n))
	s.emit(it)
}
