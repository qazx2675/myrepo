package diff

import (
	"errors"
	"fmt"
	"slices"

	"excel-diff/internal/reader"
)

// SheetPair 는 비교할 시트 짝이다. 한쪽 이름이 "" 이면 시트 추가/삭제이다.
type SheetPair struct {
	Old, New             string
	OldKeyCol, NewKeyCol int // 키 열 번호(1부터). 둘 다 0 이면 지정 없음
}

// Status 는 행·열의 상태이다. 열에는 StatusModified 를 쓰지 않는다.
type Status uint8

const (
	StatusSame Status = iota
	StatusModified
	StatusAdded
	StatusDeleted
	StatusMoved // 짝지은 행·열 사이의 상대 순서가 바뀜
)

// CellKind 는 셀 변경의 종류이다.
type CellKind uint8

const (
	CellModified CellKind = iota + 1
	CellAdded             // 빈 → 값
	CellDeleted           // 값 → 빈
	CellUnknown           // 한쪽이라도 값 확인 불가
)

// ColPair 는 열 짝이다. 번호는 1부터이며 0 은 그쪽에 열이 없음을 뜻한다.
type ColPair struct {
	Old, New             int
	Status               Status // Same, Added, Deleted, Moved
	OldHidden, NewHidden bool
}

// RowPair 는 행 짝이다. 원본 행 번호·숨김·셀은 Old/New 에서 읽는다.
type RowPair struct {
	Old, New *reader.Row // 그쪽에 행이 없으면 nil
	Status   Status
	// Changed 는 짝지은 열(양쪽에 있는 열)에 셀 변경이 있다는 뜻이다. 이동 행도 true 일 수 있다.
	Changed bool
}

// CellChange 는 동일하지 않은 셀 하나이다.
type CellChange struct {
	Row, Col int // SheetDiff.Rows, SheetDiff.Cols 의 첨자
	Kind     CellKind
	Old, New *reader.Cell // 그쪽에 값이 없으면 nil
}

// SheetDiff 는 시트 짝 하나의 비교 결과이다.
//
// Rows 와 Cols 는 좌우 배치 순서이다. 상대 순서가 유지된 짝(Same/Modified)을 기준선으로 하고,
// 기준선 사이에는 삭제(이전 순서)를 먼저, 이어서 추가·이동(최신 순서)을 둔다.
// 이동은 최신 위치에 한 번만 나온다. 비어 있지 않은 셀이 없는 행·열은 나오지 않는다
// (다만 같은 자리에서 모두 지워지거나 새로 채워진 열은 양쪽 번호를 가진다).
type SheetDiff struct {
	Old, New *reader.Sheet // 시트 추가면 Old == nil, 삭제면 New == nil
	Cols     []ColPair
	Rows     []RowPair
	Changes  []CellChange // (Row, Col) 오름차순
	Summary  Summary
}

// Renamed 는 이름이 다른 시트를 짝지었는지 알려준다.
func (s *SheetDiff) Renamed() bool { return s.Old != nil && s.New != nil && s.Old.Name != s.New.Name }

// Name 은 최신 시트명이며, 삭제된 시트는 이전 시트명이다.
func (s *SheetDiff) Name() string {
	if s.New != nil {
		return s.New.Name
	}
	return s.Old.Name
}

// Summary 는 건수이다.
type Summary struct {
	SheetsAdded, SheetsDeleted, SheetsRenamed int
	RowsAdded, RowsDeleted                    int
	RowsModified                              int // 이동하지 않고 수정된 행
	RowsMoved, RowsMovedModified              int // RowsMovedModified 는 RowsMoved 중 값도 바뀐 행
	ColsAdded, ColsDeleted, ColsMoved         int
	SameCells                                 int // 동일한 셀 짝(이동한 행·열 포함)
	ModifiedCells, AddedCells, DeletedCells   int
	UnknownCells                              int
}

func (s *Summary) add(o Summary) {
	s.SheetsAdded += o.SheetsAdded
	s.SheetsDeleted += o.SheetsDeleted
	s.SheetsRenamed += o.SheetsRenamed
	s.RowsAdded += o.RowsAdded
	s.RowsDeleted += o.RowsDeleted
	s.RowsModified += o.RowsModified
	s.RowsMoved += o.RowsMoved
	s.RowsMovedModified += o.RowsMovedModified
	s.ColsAdded += o.ColsAdded
	s.ColsDeleted += o.ColsDeleted
	s.ColsMoved += o.ColsMoved
	s.SameCells += o.SameCells
	s.ModifiedCells += o.ModifiedCells
	s.AddedCells += o.AddedCells
	s.DeletedCells += o.DeletedCells
	s.UnknownCells += o.UnknownCells
}

// Result 는 비교 결과이다. 입력 Workbook 을 참조하므로 결과를 쓰는 동안 고치면 안 된다.
type Result struct {
	Sheets                   []SheetDiff // 시트 짝 목록 순서
	ExcludedOld, ExcludedNew []string    // 짝 목록에 없어 비교하지 않은 시트
	Summary                  Summary
	Warnings                 []reader.Warning
}

// DefaultPairs 는 같은 이름끼리 짝짓고 남는 시트는 추가/삭제로 둔다.
// 순서는 최신 파일의 시트 순서이며 삭제된 시트가 뒤에 온다.
func DefaultPairs(old, new *reader.Workbook) []SheetPair {
	left := map[string]bool{}
	for _, s := range old.Sheets {
		left[s.Name] = true
	}
	var pairs []SheetPair
	for _, s := range new.Sheets {
		p := SheetPair{New: s.Name}
		if left[s.Name] {
			p.Old = s.Name
			delete(left, s.Name)
		}
		pairs = append(pairs, p)
	}
	for _, s := range old.Sheets {
		if left[s.Name] {
			pairs = append(pairs, SheetPair{Old: s.Name})
		}
	}
	return pairs
}

const progressTotal = 1000

// Compare 는 시트 짝 목록대로 비교하고 Verify 를 통과한 결과만 돌려준다.
// progress 는 nil 이어도 되며, done 은 0 부터 total 까지 증가한다.
func Compare(old, new *reader.Workbook, pairs []SheetPair, progress func(done, total int)) (*Result, error) {
	type job struct {
		old, new *reader.Sheet
		pair     SheetPair
		weight   int
	}
	res := &Result{}
	oldByName, newByName := sheetMap(old), sheetMap(new)
	var jobs []job
	total := 1
	for _, p := range pairs {
		j := job{pair: p, weight: 1}
		for _, side := range []struct {
			label, name string
			from        map[string]*reader.Sheet
			to          **reader.Sheet
		}{{"이전", p.Old, oldByName, &j.old}, {"최신", p.New, newByName, &j.new}} {
			if side.name == "" {
				continue
			}
			s, ok := side.from[side.name]
			if !ok {
				return nil, fmt.Errorf("%s 파일의 시트 '%s' 이(가) 없거나 두 번 지정되었습니다", side.label, side.name)
			}
			delete(side.from, side.name)
			*side.to = s
			for i := range s.Rows {
				j.weight += len(s.Rows[i].Cells)
			}
		}
		if j.old == nil && j.new == nil {
			return nil, errors.New("양쪽이 모두 빈 시트 짝이 있습니다")
		}
		if (p.OldKeyCol > 0) != (p.NewKeyCol > 0) || p.OldKeyCol < 0 || p.NewKeyCol < 0 {
			return nil, fmt.Errorf("시트 '%s%s': 키 열은 이전과 최신을 모두 지정해야 합니다", p.Old, p.New)
		}
		total += j.weight
		jobs = append(jobs, j)
	}
	for _, s := range old.Sheets {
		if oldByName[s.Name] != nil {
			res.ExcludedOld = append(res.ExcludedOld, s.Name)
		}
	}
	for _, s := range new.Sheets {
		if newByName[s.Name] != nil {
			res.ExcludedNew = append(res.ExcludedNew, s.Name)
		}
	}
	for _, side := range []struct {
		label string
		wb    *reader.Workbook
	}{{"[이전 파일] ", old}, {"[최신 파일] ", new}} {
		for _, w := range side.wb.Warnings {
			w.Message = side.label + w.Message
			res.Warnings = append(res.Warnings, w)
		}
	}

	done, last := 0, -1
	report := func(units int) {
		if v := int(int64(units) * progressTotal / int64(total)); progress != nil && v > last {
			last = v
			progress(v, progressTotal)
		}
	}
	report(0)
	for _, j := range jobs {
		step := func(percent int) { report(done + int(int64(j.weight)*int64(percent)/100)) }
		sd, warnings := compareSheet(j.old, j.new, j.pair, step)
		res.Sheets = append(res.Sheets, sd)
		res.Summary.add(sd.Summary)
		res.Warnings = append(res.Warnings, warnings...)
		done += j.weight
		report(done)
	}
	if err := Verify(old, new, res); err != nil {
		return nil, err
	}
	report(total)
	return res, nil
}

func sheetMap(wb *reader.Workbook) map[string]*reader.Sheet {
	m := make(map[string]*reader.Sheet, len(wb.Sheets))
	for i := range wb.Sheets {
		m[wb.Sheets[i].Name] = &wb.Sheets[i]
	}
	return m
}

func compareSheet(old, new *reader.Sheet, p SheetPair, step func(percent int)) (SheetDiff, []reader.Warning) {
	sd := SheetDiff{Old: old, New: new}
	var oldRows, newRows []reader.Row
	var oldInfo, newInfo map[int]reader.Col
	if old != nil {
		oldRows, oldInfo = old.Rows, old.Cols
	}
	if new != nil {
		newRows, newInfo = new.Rows, new.Cols
	}

	values := interner{}
	cols := alignCols(columns(oldRows, values), columns(newRows, values), p.OldKeyCol, p.NewKeyCol)
	for i := range cols {
		c := &cols[i]
		c.OldHidden = c.Old > 0 && oldInfo[c.Old].Hidden
		c.NewHidden = c.New > 0 && newInfo[c.New].Hidden
	}
	sd.Cols = cols
	step(20)

	var warnings []reader.Warning
	rows := newAligner(oldRows, newRows, cols, values)
	if p.OldKeyCol > 0 && old != nil && new != nil {
		warnings = rows.matchKeys(p.OldKeyCol, p.NewKeyCol, old.Name, new.Name)
	}
	rows.align(func(percent int) { step(20 + percent*65/100) })
	sd.Rows = rows.result()
	step(85)

	classify(&sd)
	switch {
	case old == nil:
		sd.Summary.SheetsAdded = 1
	case new == nil:
		sd.Summary.SheetsDeleted = 1
	case sd.Renamed():
		sd.Summary.SheetsRenamed = 1
	}
	return sd, warnings
}

// classify 는 짝지은 행·열을 따라 셀을 분류하고 행 상태와 건수를 채운다.
func classify(sd *SheetDiff) {
	oldAt, newAt := map[int]int{}, map[int]int{} // 열 번호 -> Cols 첨자
	for i, c := range sd.Cols {
		if c.Old > 0 {
			oldAt[c.Old] = i
		}
		if c.New > 0 {
			newAt[c.New] = i
		}
		switch c.Status {
		case StatusAdded:
			sd.Summary.ColsAdded++
		case StatusDeleted:
			sd.Summary.ColsDeleted++
		case StatusMoved:
			sd.Summary.ColsMoved++
		}
	}
	oldCells := make([]*reader.Cell, len(sd.Cols))
	newCells := make([]*reader.Cell, len(sd.Cols))
	var used []int
	place := func(row *reader.Row, at map[int]int, dst []*reader.Cell) {
		if row == nil {
			return
		}
		for i := range row.Cells {
			k := at[row.Cells[i].Col]
			if oldCells[k] == nil && newCells[k] == nil {
				used = append(used, k)
			}
			dst[k] = &row.Cells[i]
		}
	}
	s := &sd.Summary
	for r := range sd.Rows {
		pair := &sd.Rows[r]
		used = used[:0]
		place(pair.Old, oldAt, oldCells)
		place(pair.New, newAt, newCells)
		slices.Sort(used)
		for _, k := range used {
			o, n := oldCells[k], newCells[k]
			oldCells[k], newCells[k] = nil, nil
			var kind CellKind
			switch {
			case o != nil && o.Unknown || n != nil && n.Unknown:
				kind = CellUnknown
				s.UnknownCells++
			case o == nil:
				kind = CellAdded
				s.AddedCells++
			case n == nil:
				kind = CellDeleted
				s.DeletedCells++
			case o.Key != n.Key:
				kind = CellModified
				s.ModifiedCells++
			default:
				s.SameCells++
				continue
			}
			sd.Changes = append(sd.Changes, CellChange{Row: r, Col: k, Kind: kind, Old: o, New: n})
			if c := sd.Cols[k]; c.Old > 0 && c.New > 0 && pair.Old != nil && pair.New != nil {
				pair.Changed = true
			}
		}
		switch pair.Status {
		case StatusAdded:
			s.RowsAdded++
		case StatusDeleted:
			s.RowsDeleted++
		case StatusMoved:
			s.RowsMoved++
			if pair.Changed {
				s.RowsMovedModified++
			}
		default:
			if pair.Changed {
				pair.Status = StatusModified
				s.RowsModified++
			}
		}
	}
}
