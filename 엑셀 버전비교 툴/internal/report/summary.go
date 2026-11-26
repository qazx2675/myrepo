package report

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"excel-diff/internal/describe"
	"excel-diff/internal/diff"
	"excel-diff/internal/reader"
)

// scope 는 비교 범위와 비교하지 않는 항목이다(계획서 3장).
var scope = []string{
	"비교 대상은 셀에 보이는 계산된 값입니다. 숫자 1000과 텍스트 \"1000\"은 같고, 유효숫자 15자리 밖의 오차는 무시합니다.",
	"날짜는 내부 값으로 비교합니다(날짜 표시형식만 바뀌면 동일).",
	"앞뒤 공백, 대소문자, 줄바꿈 차이는 변경입니다.",
	"병합 셀은 값이 있는 첫 셀만 비교합니다.",
	"숨김 행·열·시트(필터로 가려진 행 포함)도 비교합니다.",
	"저장된 계산값이 없고 계산할 수 없는 수식은 \"값 확인 불가\"로 표시합니다.",
	"비교하지 않음: 수식 내용, 색·글꼴·테두리·병합 모양, 메모, 그림·차트",
	"결과에는 값과 열 너비만 옮기며 원본 서식은 복제하지 않습니다.",
}

var legend = []struct {
	k    styleKey
	name string
	text string
}{
	{styleKey{fill: fillModified}, "노랑", "수정"},
	{styleKey{fill: fillAdded}, "초록", "추가"},
	{styleKey{fill: fillDeleted}, "빨강", "삭제"},
	{styleKey{fill: fillMoved}, "파랑", "이동(이동한 행의 값이 바뀐 셀은 노랑 등 변경 색)"},
	{styleKey{fill: fillUnknown}, "주황", "값 확인 불가"},
	{styleKey{hidden: true}, "회색 기울임", "원본에서 숨김인 행·열(결과에서는 숨기지 않음)"},
}

// writeSummary 는 요약 시트를 쓴다. 행 한도를 넘으면 "요약2" 시트를 뒤에 더 만든다.
func (w *writer) writeSummary(names namer) error {
	res := w.src.Result
	var rows [][]any
	name, part := summaryName, 1
	widths := []float64{24, 24, 24, 12, 12, 12, 12, 14, 12, 12, 12, 12, 12, 12, 14, 40}
	bold, header := styleKey{bold: true}, styleKey{fill: fillHeader, bold: true}

	// add 는 한 행을 붙인다. 값은 문자열 또는 int 이다.
	add := func(k styleKey, values ...any) {
		num := len(rows) + 1
		for i, v := range values {
			if s, ok := v.(string); ok {
				values[i] = w.text(name, i+1, num, s, k)
			}
		}
		rows = append(rows, values)
	}
	section := func(title string) {
		add(styleKey{})
		add(bold, title)
	}

	add(bold, "엑셀 버전비교 결과")
	add(styleKey{}, "이전 파일", w.src.OldName)
	add(styleKey{}, "최신 파일", w.src.NewName)
	add(styleKey{}, "생성 시각", w.src.Created.Format("2006-01-02 15:04:05"))
	add(styleKey{}, "변경내역 항목 수", len(w.src.Items))
	if len(w.src.Items) == 0 {
		add(bold, "변경 사항이 없습니다.")
	}

	section("시트별 건수")
	add(header, "이전 시트", "최신 시트", "상태", "추가 행", "삭제 행", "수정 행", "이동 행", "이동 중 값 변경",
		"추가 열", "삭제 열", "이동 열", "수정 셀", "추가 셀", "삭제 셀", "확인 불가 셀", "결과 시트")
	counts := func(s diff.Summary) []any {
		return []any{s.RowsAdded, s.RowsDeleted, s.RowsModified, s.RowsMoved, s.RowsMovedModified,
			s.ColsAdded, s.ColsDeleted, s.ColsMoved, s.ModifiedCells, s.AddedCells, s.DeletedCells, s.UnknownCells}
	}
	for _, p := range w.sheets {
		sd := p.sd
		sheetName := func(s *reader.Sheet) string {
			switch {
			case s == nil:
				return "(없음)"
			case s.Hidden:
				return s.Name + " (숨김)"
			}
			return s.Name
		}
		state := "비교"
		switch {
		case sd.Old == nil:
			state = "시트 추가"
		case sd.New == nil:
			state = "시트 삭제"
		case sd.Renamed():
			state = "시트 이름 변경"
		}
		values := append([]any{sheetName(sd.Old), sheetName(sd.New), state}, counts(sd.Summary)...)
		add(styleKey{}, append(values, strings.Join(p.names(), ", "))...)
	}
	s := res.Summary
	add(bold, append([]any{"합계", "",
		fmt.Sprintf("추가 %d · 삭제 %d · 이름 변경 %d", s.SheetsAdded, s.SheetsDeleted, s.SheetsRenamed)}, counts(s)...)...)

	section("시트·열 변화")
	none := true
	for _, it := range w.src.Items {
		if it.Kind == describe.KindStructure && it.SheetIndex >= 0 {
			add(styleKey{}, it.Sheet, it.Text)
			none = false
		}
	}
	if none {
		add(styleKey{}, "없음")
	}

	section("비교에서 제외한 시트")
	for _, n := range res.ExcludedOld {
		add(styleKey{}, "이전 파일", n)
	}
	for _, n := range res.ExcludedNew {
		add(styleKey{}, "최신 파일", n)
	}
	if len(res.ExcludedOld)+len(res.ExcludedNew) == 0 {
		add(styleKey{}, "없음")
	}

	section(fmt.Sprintf("경고 (%d건)", len(res.Warnings)))
	if len(res.Warnings) == 0 {
		add(styleKey{}, "없음")
	} else {
		add(header, "시트", "셀", "내용")
		for _, warn := range res.Warnings {
			add(styleKey{}, warn.Sheet, warn.Cell, warn.Message)
		}
	}

	section("색 범례")
	for _, l := range legend {
		num := len(rows) + 1
		rows = append(rows, []any{w.text(name, 1, num, l.name, l.k), w.text(name, 2, num, l.text, styleKey{})})
	}

	section("비교 범위")
	for _, line := range scope {
		add(styleKey{}, line)
	}

	section("Excel 한계로 분할·축약한 내용")
	notes := w.notes
	if len(rows)+len(notes)+2 > maxRows {
		notes = append(notes, "요약이 행 한도를 넘어 '요약2' 이후 시트에 이어집니다")
	}
	if w.cut > 0 {
		notes = append(notes, fmt.Sprintf("셀 글자 수 한도(%d자)를 넘어 끝에 '%s'을 붙이고 줄인 셀 %d개(처음 %d개 위치: %s)",
			describe.Limit, cutMark, w.cut, len(w.cutAt), strings.Join(w.cutAt, ", ")))
	}
	if len(notes) == 0 {
		notes = []string{"없음"}
	}
	for _, n := range notes {
		add(styleKey{}, n)
	}

	for start := 0; start < len(rows); start += maxRows {
		if start > 0 {
			part++
			name = names.name(summaryName, fmt.Sprint(part))
			if _, err := w.f.NewSheet(name); err != nil {
				return err
			}
		}
		sw, err := w.stream(name, 0, 0, widths)
		if err != nil {
			return err
		}
		for i, values := range rows[start:min(start+maxRows, len(rows))] {
			for j, v := range values {
				if n, ok := v.(int); ok {
					values[j] = excelize.Cell{Value: n}
				}
			}
			if err := setRow(sw, i+1, values); err != nil {
				return err
			}
		}
		if err := sw.Flush(); err != nil {
			return err
		}
	}
	return nil
}
