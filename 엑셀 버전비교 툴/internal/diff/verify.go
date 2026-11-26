package diff

import (
	"errors"
	"fmt"

	"excel-diff/internal/reader"
)

// Verify 는 비교 대상 시트의 모든 비어 있지 않은 셀이 정확히 한 번 분류되었는지,
// "동일"한 짝의 Key 가 실제로 같은지를 원본 Workbook 으로부터 다시 확인한다.
func Verify(old, new *reader.Workbook, r *Result) error {
	var oldNames, newNames []string
	oldNames = append(oldNames, r.ExcludedOld...)
	newNames = append(newNames, r.ExcludedNew...)
	var total Summary
	for i := range r.Sheets {
		sd := &r.Sheets[i]
		if sd.Old == nil && sd.New == nil {
			return errors.New("누락 검증 실패: 빈 시트 결과가 있습니다")
		}
		if sd.Old != nil {
			oldNames = append(oldNames, sd.Old.Name)
		}
		if sd.New != nil {
			newNames = append(newNames, sd.New.Name)
		}
		if err := verifySheet(old, new, sd); err != nil {
			return fmt.Errorf("누락 검증 실패: 시트 '%s': %w", sd.Name(), err)
		}
		total.add(sd.Summary)
	}
	if err := verifyNames(old, oldNames); err != nil {
		return fmt.Errorf("누락 검증 실패: 이전 파일: %w", err)
	}
	if err := verifyNames(new, newNames); err != nil {
		return fmt.Errorf("누락 검증 실패: 최신 파일: %w", err)
	}
	if total != r.Summary {
		return errors.New("누락 검증 실패: 전체 건수가 시트별 합과 다릅니다")
	}
	return nil
}

func verifyNames(wb *reader.Workbook, names []string) error {
	left := map[string]bool{}
	for _, s := range wb.Sheets {
		left[s.Name] = true
	}
	for _, n := range names {
		if !left[n] {
			return fmt.Errorf("시트 '%s' 이(가) 없거나 두 번 나옵니다", n)
		}
		delete(left, n)
	}
	if len(left) > 0 {
		return fmt.Errorf("결과에 없는 시트가 %d개 있습니다", len(left))
	}
	return nil
}

func verifySheet(oldWB, newWB *reader.Workbook, sd *SheetDiff) error {
	// 결과가 가리키는 시트가 아니라 원본 Workbook 의 시트를 기준으로 삼는다.
	source := func(wb *reader.Workbook, s *reader.Sheet) ([]reader.Row, error) {
		if s == nil {
			return nil, nil
		}
		for i := range wb.Sheets {
			if &wb.Sheets[i] == s {
				return wb.Sheets[i].Rows, nil
			}
		}
		return nil, errors.New("원본에 없는 시트를 가리킵니다")
	}
	oldRows, err := source(oldWB, sd.Old)
	if err != nil {
		return err
	}
	newRows, err := source(newWB, sd.New)
	if err != nil {
		return err
	}

	oldCol, newCol := map[int]int{}, map[int]int{} // 열 번호 -> Cols 첨자
	for i, c := range sd.Cols {
		for _, e := range []struct {
			num int
			at  map[int]int
		}{{c.Old, oldCol}, {c.New, newCol}} {
			if e.num > 0 {
				if _, dup := e.at[e.num]; dup {
					return fmt.Errorf("%d번 열이 두 번 짝지어졌습니다", e.num)
				}
				e.at[e.num] = i
			}
		}
	}

	rowSet := func(rows []reader.Row) map[*reader.Row]bool {
		m := make(map[*reader.Row]bool, len(rows))
		for i := range rows {
			m[&rows[i]] = true
		}
		return m
	}
	oldLeft, newLeft := rowSet(oldRows), rowSet(newRows)
	var got Summary
	next := 0 // sd.Changes 의 다음 항목
	for r, p := range sd.Rows {
		oldCells, newCells := map[int]*reader.Cell{}, map[int]*reader.Cell{} // Cols 첨자 -> 셀
		for _, e := range []struct {
			row   *reader.Row
			left  map[*reader.Row]bool
			col   map[int]int
			cells map[int]*reader.Cell
		}{{p.Old, oldLeft, oldCol, oldCells}, {p.New, newLeft, newCol, newCells}} {
			if e.row == nil {
				continue
			}
			if !e.left[e.row] {
				return fmt.Errorf("%d행이 원본에 없거나 두 번 나옵니다", e.row.Num)
			}
			delete(e.left, e.row)
			for i := range e.row.Cells {
				c := &e.row.Cells[i]
				k, ok := e.col[c.Col]
				if !ok || e.cells[k] != nil {
					return fmt.Errorf("%d행 %d열 셀을 분류할 열이 없거나 겹칩니다", e.row.Num, c.Col)
				}
				e.cells[k] = c
			}
		}
		for ; next < len(sd.Changes) && sd.Changes[next].Row == r; next++ {
			ch := sd.Changes[next]
			o, n := oldCells[ch.Col], newCells[ch.Col]
			if ch.Old != o || ch.New != n || o == nil && n == nil {
				return fmt.Errorf("변경 %d번이 원본 셀과 다르거나 중복입니다", next)
			}
			unknown := o != nil && o.Unknown || n != nil && n.Unknown
			var ok bool
			switch ch.Kind {
			case CellModified:
				ok = !unknown && o != nil && n != nil && o.Key != n.Key
				got.ModifiedCells++
			case CellAdded:
				ok = !unknown && o == nil
				got.AddedCells++
			case CellDeleted:
				ok = !unknown && n == nil
				got.DeletedCells++
			case CellUnknown:
				ok = unknown
				got.UnknownCells++
			}
			if !ok {
				return fmt.Errorf("변경 %d번의 종류가 원본과 맞지 않습니다", next)
			}
			delete(oldCells, ch.Col)
			delete(newCells, ch.Col)
		}
		// 남은 셀은 모두 "동일"한 짝이어야 한다.
		for k, o := range oldCells {
			n := newCells[k]
			if n == nil || o.Unknown || n.Unknown || o.Key != n.Key {
				return fmt.Errorf("%d행 %d열의 변경이 누락되었습니다", p.Old.Num, o.Col)
			}
			got.SameCells++
			delete(newCells, k)
		}
		for _, n := range newCells {
			return fmt.Errorf("최신 %d행 %d열의 변경이 누락되었습니다", p.New.Num, n.Col)
		}
	}
	if next != len(sd.Changes) {
		return errors.New("행에 속하지 않거나 순서가 어긋난 변경이 있습니다")
	}
	if len(oldLeft) > 0 || len(newLeft) > 0 {
		return fmt.Errorf("결과에 없는 행이 있습니다(이전 %d, 최신 %d)", len(oldLeft), len(newLeft))
	}
	s := sd.Summary
	if got.SameCells != s.SameCells || got.ModifiedCells != s.ModifiedCells || got.AddedCells != s.AddedCells ||
		got.DeletedCells != s.DeletedCells || got.UnknownCells != s.UnknownCells {
		return errors.New("셀 건수가 요약과 다릅니다")
	}
	return nil
}
