// Package reader 는 xlsx/xlsm 을 읽기 전용으로 열어 비교용 값과 표시 문자열을 추출한다.
package reader

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Workbook 은 읽어 들인 통합 문서이다.
type Workbook struct {
	Sheets   []Sheet // 워크시트만, 통합 문서 순서
	Warnings []Warning
}

// Sheet 는 워크시트 하나이다.
type Sheet struct {
	Name   string
	Hidden bool
	Rows   []Row       // 비어 있지 않은 셀이 있는 행만, 행 번호 오름차순
	Cols   map[int]Col // 열 번호(1부터) -> 파일에 설정된 열 정보
}

// Row 는 원본 위치의 행이다.
type Row struct {
	Num    int    // 원본 행 번호(1부터)
	Hidden bool   // 숨김(필터로 가려진 행 포함)
	Cells  []Cell // 열 번호 오름차순
}

// Cell 은 비어 있지 않은 셀이다.
type Cell struct {
	Col     int    // 원본 열 번호(1부터)
	Key     string // 비교용 정규화 값. 이 값이 같으면 동일한 셀이다
	Display string // 표시형식을 적용한 문자열
	Unknown bool   // 수식 값 확인 불가. Key 는 unknownKey, Display 는 "값 확인 불가"
}

// Col 은 열 정보이다.
type Col struct {
	Width  float64 // 0 이면 지정 없음
	Hidden bool
}

// Warning 은 사용자에게 알려야 하는 사항이다. Cell 은 없을 수 있다.
type Warning struct {
	Sheet, Cell, Message string
}

func (w Warning) String() string {
	s := w.Message
	if w.Sheet != "" {
		pos := "시트 '" + w.Sheet + "'"
		if w.Cell != "" {
			pos += " " + w.Cell
		}
		s = pos + ": " + s
	}
	return s
}

const (
	unknownKey     = "\x00unknown" // XML 에 올 수 없는 문자라 실제 값과 겹치지 않는다
	unknownDisplay = "값 확인 불가"

	progressTotal = 1000
	progressRead  = 900 // 나머지는 수식 계산 구간
)

var oleMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// formulaRef 는 저장된 계산값이 없는 수식 셀의 위치이다.
type formulaRef struct {
	sheet, row, cell int // wb.Sheets[sheet].Rows[row].Cells[cell]
	styled           bool
}

// Read 는 path 의 xlsx/xlsm 파일을 읽는다. 원본은 수정하지 않는다.
// progress 는 nil 이어도 되며, done 은 0 부터 total 까지 증가한다.
func Read(path string, progress func(done, total int)) (*Workbook, error) {
	name := filepath.Base(path)
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".xlsx", ".xlsm":
	case ".xls":
		return nil, fmt.Errorf("%s: 구형 .xls 형식은 지원하지 않습니다. Excel에서 .xlsx로 저장한 뒤 다시 시도하세요", name)
	case ".csv":
		return nil, fmt.Errorf("%s: CSV 파일은 지원하지 않습니다. Excel에서 .xlsx로 저장한 뒤 다시 시도하세요", name)
	default:
		return nil, fmt.Errorf("%s: 지원하지 않는 형식입니다(.xlsx, .xlsm만 가능)", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: 파일을 읽을 수 없습니다: %w", name, err)
	}
	if bytes.HasPrefix(data, oleMagic) {
		return nil, fmt.Errorf("%s: 암호가 걸린 파일이거나 구형 .xls 파일입니다. 암호를 해제하고 .xlsx로 저장한 뒤 다시 시도하세요", name)
	}
	wb, err := read(data, progress)
	if err != nil {
		return nil, fmt.Errorf("%s: 파일이 손상되었거나 올바른 Excel 파일이 아닙니다: %w", name, err)
	}
	return wb, nil
}

func read(data []byte, progress func(done, total int)) (wb *Workbook, err error) {
	// 손상된 입력에서 라이브러리가 panic 해도 오류로 돌려준다.
	defer func() {
		if r := recover(); r != nil {
			wb, err = nil, fmt.Errorf("%v", r)
		}
	}()
	report := func(done int) {
		if progress != nil {
			progress(done, progressTotal)
		}
	}
	report(0)

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	pkg := newPackage(zr)
	info, err := pkg.workbook()
	if err != nil {
		return nil, err
	}
	// 기본 날짜 형식(내장 14, 22)은 한글 Excel 처럼 연-월-일로 표시한다.
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{ShortDatePattern: "yyyy-mm-dd"})
	if err != nil {
		return nil, err
	}
	defer f.Close()

	wb = &Workbook{}
	if info.date1904 {
		wb.Warnings = append(wb.Warnings, Warning{Message: "1904 날짜 체계 파일입니다. 1900 날짜 체계 파일과 비교하면 날짜가 변경으로 표시됩니다"})
	}
	var strs []string
	if info.sharedStrings != "" {
		if strs, err = pkg.sharedStrings(info.sharedStrings); err != nil {
			return nil, err
		}
	}

	// 진행률은 시트 XML 크기에 비례. 시트마다 두 번(값, 표시 문자열) 읽는다.
	var totalSize, doneSize int64
	for _, s := range info.sheets {
		if s.worksheet {
			totalSize += 2 * pkg.size(s.path)
		}
	}
	at := func(n int64) {
		if totalSize > 0 {
			report(int(min(n, totalSize) * progressRead / totalSize))
		}
	}

	var parsers []sheetParser // wb.Sheets 와 같은 순서
	pending := 0
	for _, s := range info.sheets {
		if !s.worksheet {
			wb.Warnings = append(wb.Warnings, Warning{Sheet: s.name, Message: "차트 시트 등 셀이 없는 시트는 비교하지 않습니다"})
			continue
		}
		size := pkg.size(s.path)
		p := sheetParser{
			sheet:    Sheet{Name: s.name, Hidden: s.hidden, Cols: map[int]Col{}},
			index:    len(wb.Sheets),
			strs:     strs,
			date1904: info.date1904,
			progress: func(offset int64) { at(doneSize + offset) },
		}
		if err := p.parse(pkg, s.path); err != nil {
			return nil, fmt.Errorf("시트 '%s': %w", s.name, err)
		}
		doneSize += size
		if err := fillDisplay(f, &p.sheet, func(row, last int) { at(doneSize + size*int64(row)/int64(last)) }); err != nil {
			return nil, fmt.Errorf("시트 '%s': %w", s.name, err)
		}
		doneSize += size
		wb.Sheets = append(wb.Sheets, p.sheet)
		parsers = append(parsers, p)
		pending += len(p.pending)
	}

	// excelize 는 계산할 때 시트 전체를 메모리에 올리므로 모든 시트를 읽은 뒤에 계산한다.
	if pending > 0 {
		if err := calculateAll(f, wb, parsers, func(done, total int) {
			report(progressRead + (progressTotal-progressRead)*done/total)
		}); err != nil {
			return nil, err
		}
	}
	dropEmpty(wb)
	report(progressTotal)
	return wb, nil
}

// fillDisplay 는 excelize 로 서식 적용값을 읽어 Display 를 채운다.
// 수식 문자열 결과(Display 가 이미 있음)와 저장값 없는 수식은 건너뛴다.
func fillDisplay(f *excelize.File, s *Sheet, progress func(row, last int)) (err error) {
	if len(s.Rows) == 0 {
		return nil
	}
	rows, err := f.Rows(s.Name)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	last := s.Rows[len(s.Rows)-1].Num
	next := 0
	for num := 1; next < len(s.Rows) && rows.Next(); num++ {
		values, err := rows.Columns()
		if err != nil {
			return err
		}
		if s.Rows[next].Num != num {
			continue
		}
		cells := s.Rows[next].Cells
		for i := range cells {
			c := &cells[i]
			if !c.Unknown && c.Display == "" && c.Col <= len(values) {
				c.Display = values[c.Col-1]
				if c.Display == c.Key {
					c.Display = c.Key // 같은 문자열은 메모리를 공유
				}
			}
		}
		next++
		if next%1000 == 0 {
			progress(num, last)
		}
	}
	if next != len(s.Rows) {
		return fmt.Errorf("%d행의 표시값을 읽지 못했습니다", s.Rows[next].Num)
	}
	return nil
}

func calcValue(f *excelize.File, sheet, axis string, styled bool) (raw, shown string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	if raw, err = f.CalcCellValue(sheet, axis, excelize.Options{RawCellValue: true}); err != nil {
		return
	}
	if !styled {
		return raw, calcKey(raw), nil
	}
	shown, err = f.CalcCellValue(sheet, axis)
	return
}

// dropEmpty 는 계산 결과가 빈 문자열인 셀과, 그래서 비게 된 행을 제거한다.
// pending 의 인덱스를 다 쓴 뒤에 호출해야 한다.
func dropEmpty(wb *Workbook) {
	for i := range wb.Sheets {
		rows := wb.Sheets[i].Rows[:0]
		for _, r := range wb.Sheets[i].Rows {
			cells := r.Cells[:0]
			for _, c := range r.Cells {
				if c.Key != "" {
					cells = append(cells, c)
				}
			}
			if r.Cells = cells; len(cells) > 0 {
				rows = append(rows, r)
			}
		}
		wb.Sheets[i].Rows = rows
	}
}
