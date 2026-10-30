package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/xuri/excelize/v2"
)

// SectionColumns holds the 1-based column numbers of the 8 section headers
type SectionColumns struct {
	StepCol          int // "Step"
	TaskNameCol      int // "작업내용"
	PreTaskDetailCol int // "작업자에 의한 사전 작업 내용"
	TargetObjCol     int // "작업대상"
	VerifyMethodCol  int // "검증방법"
	WorkerCol        int // "작업자"
	VerifierCol      int // "검증자"
	ResultCol        int // "작업결과"
}

// SectionInfo describes one discovered section (title, header and data merged block)
type SectionInfo struct {
	Name         string // "사전작업" | "본작업" | "사후작업"
	StepNumber   string // "1" | "2" | "3"
	TitleRow     int
	HeaderRow    int
	DataStartRow int // HeaderRow+1
	DataEndRow   int
	Cols         SectionColumns
}

// TargetSheetInfo is the per-sheet summary returned by POST /api/target/load
type TargetSheetInfo struct {
	Index          int      `json:"index"` // 1-based 시트 순서
	Name           string   `json:"name"`
	Sections       []string `json:"sections"` // 찾은 섹션 이름들 (순서: 사전/본/사후)
	HasAllSections bool     `json:"has_all_sections"`
	FontName       string   `json:"font_name"` // 사전작업 작업내용 데이터 셀 기준
	FontSize       float64  `json:"font_size"`
	ImageCount     int      `json:"image_count"` // 시트 전체 Shapes.Count
}

// SOPGenerateRequest encapsulates all parameters required to generate an SOP Excel
type SOPGenerateRequest struct {
	TargetPath     string   `json:"target_path"`     // 상대경로면 GetAppDir() 기준
	SelectedSheets []string `json:"selected_sheets"` // 체크한 순서
	WorkerName     string   `json:"worker_name"`
	VerifierName   string   `json:"verifier_name"`
	TargetObject   string   `json:"target_object"`
	OutputFileName string   `json:"output_file_name"` // 빈값이면 "SOP_결과본.xlsx"
}

// SOPGenerateResult contains the outcome of the SOP generation
type SOPGenerateResult struct {
	Success        bool     `json:"success"`
	DstPath        string   `json:"dst_path"`
	DstFilename    string   `json:"dst_filename"`
	FileSize       int64    `json:"file_size"`
	SelectedSheets []string `json:"selected_sheets"`
	ExecutionTime  string   `json:"execution_time"`
	Message        string   `json:"message"`
}

// Section names in canonical order with their fixed Step numbers
var sectionDefs = []struct{ Name, Step string }{
	{"사전작업", "1"},
	{"본작업", "2"},
	{"사후작업", "3"},
}

// Columns whose contents are stacked from every selected sheet
var stackColumns = []func(SectionColumns) int{
	func(c SectionColumns) int { return c.TaskNameCol },
	func(c SectionColumns) int { return c.PreTaskDetailCol },
	func(c SectionColumns) int { return c.VerifyMethodCol },
}

const (
	defaultOutputName = "SOP_결과본.xlsx"
	resultSheetName   = "SOP"
	blockHeaderFormat = "%d. [%s]" // 적재 블록 헤더: "1. [시트이름]"

	xlTop             = -4160
	xlOpenXMLWorkbook = 51
	msoComment        = 4

	maxRowHeight     = 409.0 // Excel 최대 행 높이(pt)
	maxRichTextChars = 5000  // 글자별 글꼴 조회 상한(초과 시 단일 글꼴)
	maxScanRows      = 2000  // UsedRange 스캔 상한
	maxScanCols      = 100
	headerLookdown   = 5   // 타이틀 아래 헤더 탐색 행 수
	blockPadding     = 8.0 // 적재 블록 사이 여백(pt)
	cellInnerPad     = 4.0 // 셀 안쪽 여백(pt)
)

// ---------------------------------------------------------------------------
// Sample target (excelize, no COM)
// ---------------------------------------------------------------------------

// CreateSampleTarget creates a v2 sample target.xlsx: 3 sheets, each with
// 사전작업/본작업/사후작업 sections at different row positions, 14pt data cells
// and PNG images inside some data merged cells
func CreateSampleTarget(destPath string) error {
	_ = os.MkdirAll(filepath.Dir(destPath), 0755)

	f := excelize.NewFile()
	defer f.Close()

	type sampleSheet struct {
		name     string
		topRow   int     // 사전작업 타이틀 행
		gap      int     // 섹션 사이 빈 행 수
		spacer   [3]bool // 타이틀과 헤더 사이 빈 병합 행 여부
		dataRows [3]int  // 데이터 병합 블록 행 수
		texts    [3][3]string
		color    color.RGBA
		// 이미지를 넣을 (섹션 인덱스, 적재열 인덱스) 목록
		images [][2]int
	}

	sheets := []sampleSheet{
		{
			name: "1_OS설치", topRow: 2, gap: 3,
			spacer: [3]bool{false, true, true}, dataRows: [3]int{4, 6, 6},
			texts: [3][3]string{
				{"OS 설치 사전 준비\n- ISO 이미지 무결성 확인", "설치 대상 서버 전원/콘솔 확인\n- iDRAC 접속 계정 확인", "sha256sum 결과 비교"},
				{"OS 설치 진행\n- UEFI 부팅 후 파티션 구성\n- /boot 1GB, swap 4GB", "설치 화면 캡처 첨부", "설치 완료 후 재부팅 확인"},
				{"OS 기본 설정\n- 호스트명/시간대 설정", "설정 파일 백업\n- /etc/hosts 백업", "hostnamectl 결과 확인"},
			},
			color:  color.RGBA{R: 59, G: 130, B: 246, A: 255},
			images: [][2]int{{1, 1}},
		},
		{
			name: "2_네트워크설정", topRow: 4, gap: 2,
			spacer: [3]bool{true, false, true}, dataRows: [3]int{4, 5, 6},
			texts: [3][3]string{
				{"네트워크 사전 점검\n- 물리 NIC 링크 상태 확인", "스위치 포트 VLAN 확인 요청", "ethtool 링크 Up 확인"},
				{"본딩 구성\n- bond0 (LACP mode 4)\n- IP 192.168.10.50/24", "구성도 첨부", "게이트웨이 ping 응답 확인"},
				{"네트워크 설정 저장\n- nmcli 연결 프로필 저장", "기존 설정 백업 파일 보관", "재부팅 후 IP 유지 확인"},
			},
			color:  color.RGBA{R: 16, G: 185, B: 129, A: 255},
			images: [][2]int{{1, 1}},
		},
		{
			name: "3_보안점검", topRow: 6, gap: 4,
			spacer: [3]bool{true, true, false}, dataRows: [3]int{5, 6, 5},
			texts: [3][3]string{
				{"보안 점검 사전 준비\n- 점검 스크립트 배포", "점검 일정 공지\n- 담당자 승인 확인", "스크립트 실행 권한 확인"},
				{"보안 설정 적용\n- PermitRootLogin no\n- firewalld 22/443 허용", "적용 전 설정 화면 첨부", "sshd -T 결과 확인"},
				{"보안 점검 결과 정리\n- SELinux enforcing 확인", "점검 결과 보고서 작성", "점검 결과 화면 확인"},
			},
			color:  color.RGBA{R: 245, G: 158, B: 11, A: 255},
			images: [][2]int{{1, 1}, {2, 2}},
		},
	}

	border := func(c string) []excelize.Border {
		return []excelize.Border{
			{Type: "left", Color: c, Style: 1},
			{Type: "top", Color: c, Style: 1},
			{Type: "bottom", Color: c, Style: 1},
			{Type: "right", Color: c, Style: 1},
		}
	}
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Family: "맑은 고딕"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#DCE6F1"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    border("#000000"),
	})
	spacerStyle, _ := f.NewStyle(&excelize.Style{
		Font:   &excelize.Font{Size: 11, Family: "맑은 고딕"},
		Border: border("#000000"),
	})
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Family: "맑은 고딕"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#F2F2F2"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    border("#000000"),
	})
	dataStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 14, Family: "맑은 고딕"},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "top", WrapText: true},
		Border:    border("#000000"),
	})
	dataCenterStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 14, Family: "맑은 고딕"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    border("#000000"),
	})

	headers := []string{"Step", "작업내용", "작업자에 의한 사전 작업 내용", "작업대상", "검증방법", "작업자", "검증자", "작업결과"}
	widths := []float64{8, 30, 28, 14, 30, 10, 10, 12}
	stackIdx := []int{1, 2, 4} // headers 내 작업내용/사전 작업 내용/검증방법 위치

	for i, sh := range sheets {
		if i == 0 {
			f.SetSheetName(f.GetSheetName(0), sh.name)
		} else if _, err := f.NewSheet(sh.name); err != nil {
			return fmt.Errorf("샘플 시트 생성 실패 (%s): %w", sh.name, err)
		}
		_ = f.SetColWidth(sh.name, "A", "A", 3)
		for c, w := range widths {
			col, _ := excelize.ColumnNumberToName(c + 2)
			_ = f.SetColWidth(sh.name, col, col, w)
		}

		row := sh.topRow
		for s, def := range sectionDefs {
			// 섹션 타이틀 (B:I 병합)
			_ = f.MergeCell(sh.name, cell(2, row), cell(9, row))
			_ = f.SetCellValue(sh.name, cell(2, row), def.Name)
			_ = f.SetCellStyle(sh.name, cell(2, row), cell(9, row), titleStyle)
			_ = f.SetRowHeight(sh.name, row, 22)
			row++
			if sh.spacer[s] {
				_ = f.MergeCell(sh.name, cell(2, row), cell(9, row))
				_ = f.SetCellStyle(sh.name, cell(2, row), cell(9, row), spacerStyle)
				row++
			}

			// 8개 컬럼 헤더
			for c, h := range headers {
				_ = f.SetCellValue(sh.name, cell(c+2, row), h)
			}
			_ = f.SetCellStyle(sh.name, cell(2, row), cell(9, row), headerStyle)
			_ = f.SetRowHeight(sh.name, row, 33)
			row++

			// 데이터 병합 블록
			start, end := row, row+sh.dataRows[s]-1
			for r := start; r <= end; r++ {
				_ = f.SetRowHeight(sh.name, r, 24)
			}
			values := []string{def.Step, sh.texts[s][0], sh.texts[s][1], "샘플대상", sh.texts[s][2], "샘플작업자", "샘플검증자", ""}
			for c, v := range values {
				col := c + 2
				_ = f.MergeCell(sh.name, cell(col, start), cell(col, end))
				style := dataStyle
				if c == 0 || c == 3 || c == 5 || c == 6 || c == 7 {
					style = dataCenterStyle
				}
				_ = f.SetCellStyle(sh.name, cell(col, start), cell(col, end), style)
				if v != "" {
					_ = f.SetCellValue(sh.name, cell(col, start), v)
				}
			}

			// 데이터 병합 셀 안 이미지 (본문 텍스트 아래쪽)
			for _, im := range sh.images {
				if im[0] != s {
					continue
				}
				if err := f.AddPictureFromBytes(sh.name, cell(stackIdx[im[1]]+2, start+3), &excelize.Picture{
					Extension: ".png",
					File:      generateSampleBadge(sh.color),
					Format:    &excelize.GraphicOptions{ScaleX: 0.6, ScaleY: 0.6, OffsetX: 6, OffsetY: 4},
				}); err != nil {
					return fmt.Errorf("샘플 이미지 삽입 실패 (%s): %w", sh.name, err)
				}
			}
			row = end + 1 + sh.gap
		}
	}

	return f.SaveAs(destPath)
}

func cell(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

// generateSampleBadge renders a solid colored PNG badge with a dark border
func generateSampleBadge(col color.RGBA) []byte {
	width := 240
	height := 60
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill background
	draw.Draw(img, img.Bounds(), &image.Uniform{C: col}, image.Point{}, draw.Src)

	// Draw dark border
	borderColor := color.RGBA{R: 30, G: 41, B: 59, A: 255}
	for x := 0; x < width; x++ {
		img.Set(x, 0, borderColor)
		img.Set(x, height-1, borderColor)
	}
	for y := 0; y < height; y++ {
		img.Set(0, y, borderColor)
		img.Set(width-1, y, borderColor)
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// COM helpers
// ---------------------------------------------------------------------------

// withExcel starts a hidden Excel instance on a locked OS thread, runs fn and quits Excel
func withExcel(fn func(app, workbooks *ole.IDispatch) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// go-ole returns S_FALSE as *ole.OleError(code 1); S_OK/S_FALSE must be balanced
	// with CoUninitialize, RPC_E_CHANGED_MODE must not be
	needUninit := true
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		oleErr, ok := err.(*ole.OleError)
		switch {
		case ok && oleErr.Code() == 0x00000001: // S_FALSE
		case ok && oleErr.Code() == 0x80010106: // RPC_E_CHANGED_MODE
			needUninit = false
		default:
			return fmt.Errorf("OLE 초기화 실패: %w", err)
		}
	}
	if needUninit {
		defer ole.CoUninitialize()
	}

	unknown, err := oleutil.CreateObject("Excel.Application")
	if err != nil {
		return fmt.Errorf("Excel Application 실행 실패: %w", err)
	}
	app, err := unknown.QueryInterface(ole.IID_IDispatch)
	unknown.Release()
	if err != nil {
		return fmt.Errorf("Excel Application 인터페이스 획득 실패: %w", err)
	}
	defer app.Release()

	oleutil.PutProperty(app, "Visible", false)
	oleutil.PutProperty(app, "DisplayAlerts", false)
	defer oleutil.CallMethod(app, "Quit")

	workbooks, err := getDisp(app, "Workbooks")
	if err != nil {
		return fmt.Errorf("Workbooks 객체 획득 실패: %w", err)
	}
	defer workbooks.Release()

	return fn(app, workbooks)
}

func getDisp(obj *ole.IDispatch, name string, args ...interface{}) (*ole.IDispatch, error) {
	v, err := oleutil.GetProperty(obj, name, args...)
	if err != nil {
		return nil, err
	}
	d := v.ToIDispatch()
	if d == nil {
		return nil, fmt.Errorf("%s 객체가 없습니다", name)
	}
	return d, nil
}

func callDisp(obj *ole.IDispatch, name string, args ...interface{}) (*ole.IDispatch, error) {
	v, err := oleutil.CallMethod(obj, name, args...)
	if err != nil {
		return nil, err
	}
	d := v.ToIDispatch()
	if d == nil {
		return nil, fmt.Errorf("%s 결과 객체가 없습니다", name)
	}
	return d, nil
}

// getVal reads a scalar property (nil on error / VT_NULL)
func getVal(obj *ole.IDispatch, name string, args ...interface{}) interface{} {
	v, err := oleutil.GetProperty(obj, name, args...)
	if err != nil || v == nil {
		return nil
	}
	defer v.Clear()
	return v.Value()
}

func getNum(obj *ole.IDispatch, name string, args ...interface{}) float64 {
	return asFloat(getVal(obj, name, args...))
}

// getSubNum reads obj.sub.name (e.g. range.Rows.Count)
func getSubNum(obj *ole.IDispatch, sub, name string) float64 {
	d, err := getDisp(obj, sub)
	if err != nil {
		return 0
	}
	defer d.Release()
	return getNum(d, name)
}

func asFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case int16:
		return float64(val)
	case int8:
		return float64(val)
	case uint8:
		return float64(val)
	case uint16:
		return float64(val)
	case uint32:
		return float64(val)
	default:
		return 0
	}
}

func asText(v interface{}) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "TRUE"
		}
		return "FALSE"
	default:
		if n := asFloat(val); n != 0 {
			return strconv.FormatFloat(n, 'f', -1, 64)
		}
		return fmt.Sprintf("%v", val)
	}
}

// normalizeKey removes every whitespace character for header/title comparison
func normalizeKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func openReadOnly(workbooks *ole.IDispatch, path string) (*ole.IDispatch, error) {
	wb, err := callDisp(workbooks, "Open", path, 0, true)
	if err != nil {
		return nil, fmt.Errorf("Target 엑셀 열기 실패 (%s): %w", path, err)
	}
	return wb, nil
}

func closeWorkbook(wb *ole.IDispatch) {
	oleutil.CallMethod(wb, "Close", false)
	wb.Release()
}

func resolveTargetPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("Target 엑셀 파일 경로가 비어 있습니다")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(GetAppDir(), p)
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("Target 엑셀 파일을 찾을 수 없습니다 (%s): %w", p, err)
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Section discovery
// ---------------------------------------------------------------------------

// sheetGrid holds the text of the worksheet UsedRange (1-based sheet coordinates)
type sheetGrid struct {
	row0, col0 int
	cells      [][]string
}

func (g *sheetGrid) text(r, c int) string {
	r -= g.row0
	c -= g.col0
	if r < 0 || r >= len(g.cells) || c < 0 || c >= len(g.cells[r]) {
		return ""
	}
	return g.cells[r][c]
}

// readUsedGrid reads all cell texts of UsedRange. Each row is read with one
// WorksheetFunction.TextJoin call when available, otherwise cell by cell.
func readUsedGrid(ws *ole.IDispatch) (*sheetGrid, error) {
	used, err := getDisp(ws, "UsedRange")
	if err != nil {
		return nil, fmt.Errorf("UsedRange 접근 실패: %w", err)
	}
	g := &sheetGrid{row0: int(getNum(used, "Row")), col0: int(getNum(used, "Column"))}
	nRows := int(getSubNum(used, "Rows", "Count"))
	nCols := int(getSubNum(used, "Columns", "Count"))
	used.Release()
	if g.row0 < 1 || g.col0 < 1 {
		return nil, fmt.Errorf("UsedRange 위치 확인 실패")
	}
	nRows = min(nRows, maxScanRows)
	nCols = min(nCols, maxScanCols)

	var wf *ole.IDispatch
	if app, err := getDisp(ws, "Application"); err == nil {
		wf, _ = getDisp(app, "WorksheetFunction")
		app.Release()
	}
	if wf != nil {
		defer wf.Release()
	}

	const sep = "\x1f"
	for i := 0; i < nRows; i++ {
		r := g.row0 + i
		var row []string
		if wf != nil && nCols > 1 {
			if rng, err := getDisp(ws, "Range", cell(g.col0, r)+":"+cell(g.col0+nCols-1, r)); err == nil {
				if v, err := oleutil.CallMethod(wf, "TextJoin", sep, false, rng); err == nil {
					if s, ok := v.Value().(string); ok {
						if parts := strings.Split(s, sep); len(parts) == nCols {
							row = parts
						}
					}
					v.Clear()
				}
				rng.Release()
			}
		}
		if row == nil {
			row = make([]string, nCols)
			for j := 0; j < nCols; j++ {
				row[j] = cellText(ws, r, g.col0+j)
			}
		}
		g.cells = append(g.cells, row)
	}
	return g, nil
}

func cellText(ws *ole.IDispatch, r, c int) string {
	cl, err := getDisp(ws, "Cells", r, c)
	if err != nil {
		return ""
	}
	defer cl.Release()
	return asText(getVal(cl, "Value2"))
}

// matchHeaderRow maps the 8 headers of row r; ok when Step and 작업내용 exist
func matchHeaderRow(g *sheetGrid, r int) (SectionColumns, bool) {
	var cols SectionColumns
	set := func(dst *int, c int) {
		if *dst == 0 {
			*dst = c
		}
	}
	i := r - g.row0
	if i < 0 || i >= len(g.cells) {
		return cols, false
	}
	for j, txt := range g.cells[i] {
		c := g.col0 + j
		key := normalizeKey(txt)
		switch {
		case strings.EqualFold(key, "step"):
			set(&cols.StepCol, c)
		case key == "작업내용":
			set(&cols.TaskNameCol, c)
		case key == "작업자에의한사전작업내용":
			set(&cols.PreTaskDetailCol, c)
		case key == "작업대상":
			set(&cols.TargetObjCol, c)
		case key == "검증방법":
			set(&cols.VerifyMethodCol, c)
		case key == "작업자":
			set(&cols.WorkerCol, c)
		case key == "검증자":
			set(&cols.VerifierCol, c)
		case key == "작업결과":
			set(&cols.ResultCol, c)
		}
	}
	return cols, cols.StepCol > 0 && cols.TaskNameCol > 0
}

// FindSections discovers 사전작업/본작업/사후작업 sections without fixed coordinates.
// Titles without a valid header row (TitleRow+1..+5) are skipped.
func FindSections(ws *ole.IDispatch) ([]SectionInfo, error) {
	g, err := readUsedGrid(ws)
	if err != nil {
		return nil, err
	}
	if len(g.cells) == 0 {
		return []SectionInfo{}, nil
	}

	found := make(map[string]SectionInfo)
	for i, row := range g.cells {
		r := g.row0 + i
		for _, txt := range row {
			key := normalizeKey(txt)
			for _, def := range sectionDefs {
				if key != def.Name {
					continue
				}
				if _, dup := found[def.Name]; dup {
					continue
				}
				for hr := r + 1; hr <= r+headerLookdown; hr++ {
					cols, ok := matchHeaderRow(g, hr)
					if !ok {
						continue
					}
					sec := SectionInfo{
						Name:         def.Name,
						StepNumber:   def.Step,
						TitleRow:     r,
						HeaderRow:    hr,
						DataStartRow: hr + 1,
						Cols:         cols,
					}
					sec.DataEndRow = sec.DataStartRow
					for _, col := range []int{cols.StepCol, cols.TaskNameCol, cols.PreTaskDetailCol, cols.TargetObjCol, cols.VerifyMethodCol, cols.WorkerCol, cols.VerifierCol, cols.ResultCol} {
						if col == 0 {
							continue
						}
						box, err := readMergeBox(ws, sec.DataStartRow, col)
						if err != nil {
							return nil, fmt.Errorf("%s 데이터 병합 영역 확인 실패: %w", def.Name, err)
						}
						sec.DataEndRow = max(sec.DataEndRow, box.Row2)
					}
					found[def.Name] = sec
					break
				}
			}
		}
	}

	result := make([]SectionInfo, 0, len(found))
	for _, def := range sectionDefs {
		if sec, ok := found[def.Name]; ok {
			result = append(result, sec)
		}
	}
	return result, nil
}

// mergeBox is the cell/point geometry of a merge area (or single cell)
type mergeBox struct {
	Row1, Col1, Row2, Col2   int
	Left, Top, Width, Height float64
}

func (b mergeBox) contains(r, c int) bool {
	return r >= b.Row1 && r <= b.Row2 && c >= b.Col1 && c <= b.Col2
}

func readMergeBox(ws *ole.IDispatch, r, c int) (mergeBox, error) {
	cl, err := getDisp(ws, "Cells", r, c)
	if err != nil {
		return mergeBox{}, err
	}
	defer cl.Release()
	area, err := getDisp(cl, "MergeArea")
	if err != nil {
		return mergeBox{}, err
	}
	defer area.Release()

	b := mergeBox{
		Row1:   int(getNum(area, "Row")),
		Col1:   int(getNum(area, "Column")),
		Left:   getNum(area, "Left"),
		Top:    getNum(area, "Top"),
		Width:  getNum(area, "Width"),
		Height: getNum(area, "Height"),
	}
	b.Row2 = b.Row1 + max(1, int(getSubNum(area, "Rows", "Count"))) - 1
	b.Col2 = b.Col1 + max(1, int(getSubNum(area, "Columns", "Count"))) - 1
	return b, nil
}

// shapeInfo is a worksheet shape with its TopLeftCell position
type shapeInfo struct {
	Index                    int
	Row, Col                 int
	Left, Top, Width, Height float64
}

func listShapes(ws *ole.IDispatch) ([]shapeInfo, error) {
	shapes, err := getDisp(ws, "Shapes")
	if err != nil {
		return nil, fmt.Errorf("Shapes 접근 실패: %w", err)
	}
	defer shapes.Release()

	n := int(getNum(shapes, "Count"))
	var list []shapeInfo
	for i := 1; i <= n; i++ {
		sh, err := callDisp(shapes, "Item", i)
		if err != nil {
			continue
		}
		if int(getNum(sh, "Type")) != msoComment {
			if tl, err := getDisp(sh, "TopLeftCell"); err == nil {
				list = append(list, shapeInfo{
					Index:  i,
					Row:    int(getNum(tl, "Row")),
					Col:    int(getNum(tl, "Column")),
					Left:   getNum(sh, "Left"),
					Top:    getNum(sh, "Top"),
					Width:  getNum(sh, "Width"),
					Height: getNum(sh, "Height"),
				})
				tl.Release()
			}
		}
		sh.Release()
	}
	return list, nil
}

// ---------------------------------------------------------------------------
// Target scan
// ---------------------------------------------------------------------------

// ScanTargetSheets lists every sheet of the target workbook (opened ReadOnly)
// with its discovered sections, data font and shape count
func ScanTargetSheets(targetPath string) ([]TargetSheetInfo, error) {
	path, err := resolveTargetPath(targetPath)
	if err != nil {
		return nil, err
	}

	results := []TargetSheetInfo{}
	err = withExcel(func(app, workbooks *ole.IDispatch) error {
		wb, err := openReadOnly(workbooks, path)
		if err != nil {
			return err
		}
		defer closeWorkbook(wb)

		sheets, err := getDisp(wb, "Worksheets")
		if err != nil {
			return fmt.Errorf("Worksheets 획득 실패: %w", err)
		}
		defer sheets.Release()

		count := int(getNum(sheets, "Count"))
		for i := 1; i <= count; i++ {
			ws, err := getDisp(sheets, "Item", i)
			if err != nil {
				continue
			}
			info := TargetSheetInfo{Index: i, Name: asText(getVal(ws, "Name")), Sections: []string{}}

			secs, err := FindSections(ws)
			if err != nil {
				ws.Release()
				return fmt.Errorf("시트 '%s' 섹션 탐색 실패: %w", info.Name, err)
			}
			for _, s := range secs {
				info.Sections = append(info.Sections, s.Name)
			}
			info.HasAllSections = len(secs) == len(sectionDefs)
			if len(secs) > 0 {
				if cl, err := getDisp(ws, "Cells", secs[0].DataStartRow, secs[0].Cols.TaskNameCol); err == nil {
					fs := readCellFont(cl)
					info.FontName, info.FontSize = fs.Name, fs.Size
					cl.Release()
				}
			}
			if shapes, err := getDisp(ws, "Shapes"); err == nil {
				info.ImageCount = int(getNum(shapes, "Count"))
				shapes.Release()
			}
			ws.Release()
			results = append(results, info)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Font handling
// ---------------------------------------------------------------------------

type fontSpec struct {
	Name     string
	Size     float64
	Bold     bool
	Color    float64
	hasBold  bool
	hasColor bool
}

func (f *fontSpec) fillFrom(font *ole.IDispatch) {
	if f.Name == "" {
		f.Name, _ = getVal(font, "Name").(string)
	}
	if f.Size <= 0 {
		f.Size = getNum(font, "Size")
	}
	if !f.hasBold {
		f.Bold, f.hasBold = getVal(font, "Bold").(bool)
	}
	if !f.hasColor {
		if v := getVal(font, "Color"); v != nil {
			f.Color, f.hasColor = asFloat(v), true
		}
	}
}

func (f *fontSpec) complete() bool {
	return f.Name != "" && f.Size > 0 && f.hasBold && f.hasColor
}

// readCellFont reads the cell font; mixed (rich text) properties fall back to the first character
func readCellFont(cl *ole.IDispatch) fontSpec {
	var fs fontSpec
	if font, err := getDisp(cl, "Font"); err == nil {
		fs.fillFrom(font)
		font.Release()
	}
	if !fs.complete() {
		if ch, err := getDisp(cl, "Characters", 1, 1); err == nil {
			if font, err := getDisp(ch, "Font"); err == nil {
				fs.fillFrom(font)
				font.Release()
			}
			ch.Release()
		}
	}
	return fs
}

// readFontRuns returns the per-character font runs of body (the cell text after
// CRLF→LF normalisation and right-trim), 1-based UTF-16 positions relative to body.
// Returns nil when the cell font is uniform or the text is too long to walk.
func readFontRuns(cl *ole.IDispatch, raw, body string) []fontRun {
	font, err := getDisp(cl, "Font")
	if err != nil {
		return nil
	}
	_, nameOK := getVal(font, "Name").(string)
	_, boldOK := getVal(font, "Bold").(bool)
	uniform := nameOK && boldOK && getVal(font, "Size") != nil && getVal(font, "Color") != nil
	font.Release()
	bodyLen := utf16Len(body)
	if uniform || bodyLen == 0 || utf16Len(raw) > maxRichTextChars {
		return nil
	}

	var runs []fontRun
	src, dst := 1, 1 // raw / body 기준 UTF-16 위치
	rs := []rune(raw)
	for i := 0; i < len(rs) && dst <= bodyLen; i++ {
		n := utf16.RuneLen(rs[i])
		if n < 1 {
			n = 1
		}
		if rs[i] == '\r' && i+1 < len(rs) && rs[i+1] == '\n' { // CRLF → LF: CR은 제거됨
			src += n
			continue
		}
		var fs fontSpec
		if ch, err := getDisp(cl, "Characters", src, 1); err == nil {
			if f, err := getDisp(ch, "Font"); err == nil {
				fs.fillFrom(f)
				f.Release()
			}
			ch.Release()
		}
		if k := len(runs) - 1; k >= 0 && runs[k].font == fs && runs[k].start+runs[k].length == dst {
			runs[k].length += n
		} else {
			runs = append(runs, fontRun{start: dst, length: n, font: fs, bold: fs.Bold})
		}
		src += n
		dst += n
	}
	if k := len(runs) - 1; k >= 0 && runs[k].start+runs[k].length > bodyLen+1 {
		runs[k].length = bodyLen + 1 - runs[k].start
	}
	return runs
}

// applyFont formats Characters(start, length) of the cell (1-based, UTF-16 units)
func applyFont(cl *ole.IDispatch, start, length int, fs fontSpec, bold bool) {
	if length <= 0 {
		return
	}
	ch, err := getDisp(cl, "Characters", start, length)
	if err != nil {
		return
	}
	defer ch.Release()
	font, err := getDisp(ch, "Font")
	if err != nil {
		return
	}
	defer font.Release()
	if fs.Name != "" {
		oleutil.PutProperty(font, "Name", fs.Name)
	}
	if fs.Size > 0 {
		oleutil.PutProperty(font, "Size", fs.Size)
	}
	oleutil.PutProperty(font, "Bold", bold)
	if fs.hasColor {
		oleutil.PutProperty(font, "Color", fs.Color)
	}
}

func fontSizeOr(fs fontSpec) float64 {
	if fs.Size > 0 {
		return fs.Size
	}
	return 11
}

// lineHeight estimates the height of one text line in points
func lineHeight(fs fontSpec) float64 {
	return fontSizeOr(fs) * 1.35
}

// textHeight estimates the wrapped height of s rendered with fs in a cell of the given width
func textHeight(s string, fs fontSpec, width float64) float64 {
	size := fontSizeOr(fs)
	usable := math.Max(width-2*cellInnerPad, size)
	lines := 0
	for _, line := range strings.Split(s, "\n") {
		w := 0.0
		for _, r := range line {
			if r >= 0x1100 { // 한글/CJK 등 전각 문자
				w += size * 0.95
			} else {
				w += size * 0.55
			}
		}
		lines += max(1, int(math.Ceil(w/usable)))
	}
	return float64(lines) * lineHeight(fs)
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// ---------------------------------------------------------------------------
// SOP generation
// ---------------------------------------------------------------------------

// srcSheet caches what is read from one selected target sheet
type srcSheet struct {
	name     string
	ws       *ole.IDispatch
	sections map[string]SectionInfo
	shapes   []shapeInfo
}

// stackBlock is one "{idx}. [sheet]" block inside a destination merged cell
type stackBlock struct {
	header string
	body   string
	font   fontSpec
	runs   []fontRun // 원본 서식 있는 텍스트의 글꼴 구간(body 기준), 없으면 font 단일 적용
	src    *srcSheet
	srcBox mergeBox
	shapes []shapeInfo
}

type fontRun struct {
	start, length int
	font          fontSpec
	bold          bool
}

type shapePlan struct {
	src           *srcSheet
	index         int
	relX, relY    float64 // 결과 병합 셀 Left/Top 기준
	width, height float64
}

// columnPlan is the computed content of one destination stacking merged cell
type columnPlan struct {
	row, col int
	text     string
	runs     []fontRun
	shapes   []shapePlan
	height   float64 // 필요한 총 높이(pt)
}

// buildColumnPlan stacks blocks vertically: text gets blank-line padding so each
// block header starts below the previous block's text and images
func buildColumnPlan(dst mergeBox, base fontSpec, blocks []stackBlock) columnPlan {
	plan := columnPlan{row: dst.Row1, col: dst.Col1}
	var sb strings.Builder
	pos := 0         // 지금까지 기록한 UTF-16 길이
	textY := 0.0     // 추정 텍스트 끝 Y
	nextStart := 0.0 // 다음 블록이 시작해야 하는 Y
	padH := lineHeight(base)
	maxShapeW := dst.Width - 2*cellInnerPad

	for i, b := range blocks {
		blockStart := 0.0
		if i > 0 {
			k := max(1, int(math.Ceil((nextStart-textY)/padH)))
			sb.WriteString(strings.Repeat("\n", k+1))
			pos += k + 1
			blockStart = textY + float64(k)*padH
		}

		hl := utf16Len(b.header)
		plan.runs = append(plan.runs, fontRun{start: pos + 1, length: hl, font: base, bold: true})
		sb.WriteString(b.header)
		pos += hl
		headerH := textHeight(b.header, base, dst.Width)

		bodyH := 0.0
		if b.body != "" {
			sb.WriteString("\n")
			pos++
			bl := utf16Len(b.body)
			if len(b.runs) > 0 {
				for _, r := range b.runs {
					r.start += pos
					plan.runs = append(plan.runs, r)
				}
			} else {
				plan.runs = append(plan.runs, fontRun{start: pos + 1, length: bl, font: b.font, bold: b.font.Bold})
			}
			sb.WriteString(b.body)
			pos += bl
			bodyH = textHeight(b.body, b.font, dst.Width)
		}
		textY = blockStart + headerH + bodyH

		shapeBottom := 0.0
		for _, s := range b.shapes {
			w, h := s.Width, s.Height
			if w > maxShapeW && maxShapeW > 0 { // 열보다 넓으면 비율 유지 축소
				h *= maxShapeW / w
				w = maxShapeW
			}
			relX := math.Max(0, s.Left-b.srcBox.Left)
			if relX+w > dst.Width-cellInnerPad {
				relX = math.Max(0, dst.Width-cellInnerPad-w)
			}
			relY := math.Max(0, s.Top-b.srcBox.Top)
			plan.shapes = append(plan.shapes, shapePlan{
				src: b.src, index: s.Index,
				relX: relX, relY: blockStart + headerH + relY,
				width: w, height: h,
			})
			shapeBottom = math.Max(shapeBottom, relY+h)
		}
		nextStart = blockStart + headerH + math.Max(bodyH, shapeBottom) + blockPadding
	}

	plan.text = sb.String()
	plan.height = math.Max(textY, nextStart)
	return plan
}

func normalizeOutputName(name string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	}
	if name == "" || name == "." || name == "/" {
		return defaultOutputName
	}
	if !strings.EqualFold(filepath.Ext(name), ".xlsx") {
		name += ".xlsx"
	}
	return name
}

// GenerateSOPExcel builds the SOP workbook:
//   - opens target ReadOnly and copies the first selected sheet into a new workbook
//   - clears the data merged cells (format preserved) and their shapes
//   - writes Step 1/2/3, 작업대상/작업자/검증자
//   - stacks 작업내용/작업자에 의한 사전 작업 내용/검증방법 text (with fonts) and shapes
//     of every selected sheet in order into one merged cell per section
//   - grows row heights to fit and saves to the program folder
func GenerateSOPExcel(req SOPGenerateRequest) (*SOPGenerateResult, error) {
	startTime := time.Now()

	var selected []string
	for _, s := range req.SelectedSheets {
		if s = strings.TrimSpace(s); s != "" {
			selected = append(selected, s)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("선택된 작업(시트)이 없습니다. 최소 1개 이상의 시트를 선택하세요.")
	}

	targetPath, err := resolveTargetPath(req.TargetPath)
	if err != nil {
		return nil, err
	}

	dstFilename := normalizeOutputName(req.OutputFileName)
	destFullPath := filepath.Join(GetAppDir(), dstFilename)
	if absT, e1 := filepath.Abs(targetPath); e1 == nil {
		if absD, e2 := filepath.Abs(destFullPath); e2 == nil && strings.EqualFold(absT, absD) {
			return nil, fmt.Errorf("결과 파일 이름이 Target 파일과 같습니다. 다른 이름을 지정하세요 (%s)", dstFilename)
		}
	}
	if _, err := os.Stat(destFullPath); err == nil {
		if err := os.Remove(destFullPath); err != nil {
			return nil, fmt.Errorf("기존 결과 파일을 삭제할 수 없습니다. 파일이 열려 있는지 확인하세요 (%s): %w", destFullPath, err)
		}
	}

	var warnings []string
	err = withExcel(func(app, workbooks *ole.IDispatch) error {
		return generateWithExcel(app, workbooks, req, selected, targetPath, destFullPath, &warnings)
	})
	if err != nil {
		return nil, err
	}

	stat, err := os.Stat(destFullPath)
	if err != nil {
		return nil, fmt.Errorf("결과 엑셀 파일 저장 확인 실패 (%s): %w", destFullPath, err)
	}

	return &SOPGenerateResult{
		Success:        true,
		DstPath:        destFullPath,
		DstFilename:    dstFilename,
		FileSize:       stat.Size(),
		SelectedSheets: selected,
		ExecutionTime:  time.Since(startTime).Round(time.Millisecond).String(),
		Message:        strings.Join(append([]string{fmt.Sprintf("SOP 엑셀 생성이 완료되었습니다 (%s, %d개 작업 조합)", dstFilename, len(selected))}, warnings...), "\n"),
	}, nil
}

func generateWithExcel(app, workbooks *ole.IDispatch, req SOPGenerateRequest, selected []string, targetPath, destFullPath string, warnings *[]string) error {
	// 1. Target ReadOnly 열기 (저장하지 않고 닫음)
	srcWb, err := openReadOnly(workbooks, targetPath)
	if err != nil {
		return err
	}
	defer closeWorkbook(srcWb)

	srcSheets, err := getDisp(srcWb, "Worksheets")
	if err != nil {
		return fmt.Errorf("Target Worksheets 획득 실패: %w", err)
	}
	defer srcSheets.Release()

	// 2. 선택 시트별 섹션/도형 분석
	srcs := make([]*srcSheet, 0, len(selected))
	for _, name := range selected {
		ws, err := getDisp(srcSheets, "Item", name)
		if err != nil {
			return fmt.Errorf("선택한 시트 '%s'를 Target 파일에서 찾을 수 없습니다", name)
		}
		defer ws.Release()

		secs, err := FindSections(ws)
		if err != nil {
			return fmt.Errorf("시트 '%s' 섹션 탐색 실패: %w", name, err)
		}
		shapes, err := listShapes(ws)
		if err != nil {
			return fmt.Errorf("시트 '%s' 도형 조회 실패: %w", name, err)
		}
		s := &srcSheet{name: name, ws: ws, sections: map[string]SectionInfo{}, shapes: shapes}
		for _, sec := range secs {
			s.sections[sec.Name] = sec
		}
		srcs = append(srcs, s)
	}
	if len(srcs[0].sections) == 0 {
		return fmt.Errorf("기준 시트 '%s'에서 사전작업/본작업/사후작업 섹션을 찾지 못했습니다", srcs[0].name)
	}

	// 3. 기준 시트(첫 선택 시트)를 단독 복사하여 새 통합문서 생성
	// Worksheet.Copy 를 인수 없이 호출하면 원본 서식·글꼴·열 너비를 100% 유지한 채
	// 해당 시트 1개만을 포함하는 새 통합문서가 ActiveWorkbook으로 생성됨
	if _, err := oleutil.CallMethod(srcs[0].ws, "Copy"); err != nil {
		return fmt.Errorf("기준 시트 '%s' 복사 실패: %w", srcs[0].name, err)
	}
	newWbVar, err := oleutil.GetProperty(app, "ActiveWorkbook")
	if err != nil || newWbVar == nil {
		return fmt.Errorf("새 통합문서 획득 실패: %v", err)
	}
	newWb := newWbVar.ToIDispatch()
	defer closeWorkbook(newWb)

	newSheets, err := getDisp(newWb, "Worksheets")
	if err != nil {
		return fmt.Errorf("새 통합문서 Worksheets 획득 실패: %w", err)
	}
	defer newSheets.Release()

	dstWs, err := getDisp(newSheets, "Item", 1)
	if err != nil {
		return fmt.Errorf("결과 시트 획득 실패: %w", err)
	}
	defer dstWs.Release()
	oleutil.PutProperty(dstWs, "Name", resultSheetName)

	dstSecs, err := FindSections(dstWs)
	if err != nil {
		return fmt.Errorf("결과 시트 섹션 탐색 실패: %w", err)
	}

	// 4. 데이터 병합 셀 내용 초기화 (서식/병합/테두리/글꼴 유지) + 데이터 영역 도형 삭제
	var dataAreas []mergeBox
	for _, sec := range dstSecs {
		c := sec.Cols
		area := mergeBox{Row1: sec.DataStartRow, Row2: sec.DataEndRow, Col1: math.MaxInt32}
		for _, col := range []int{c.StepCol, c.TaskNameCol, c.PreTaskDetailCol, c.TargetObjCol, c.VerifyMethodCol, c.WorkerCol, c.VerifierCol, c.ResultCol} {
			if col == 0 {
				continue
			}
			box, err := readMergeBox(dstWs, sec.DataStartRow, col)
			if err != nil {
				return fmt.Errorf("%s 데이터 셀 확인 실패: %w", sec.Name, err)
			}
			area.Col1 = min(area.Col1, box.Col1)
			area.Col2 = max(area.Col2, box.Col2)
		}
		dataAreas = append(dataAreas, area)

		for _, col := range []int{c.TaskNameCol, c.PreTaskDetailCol, c.VerifyMethodCol, c.TargetObjCol, c.WorkerCol, c.VerifierCol} {
			if col == 0 {
				continue
			}
			if cl, err := getDisp(dstWs, "Cells", sec.DataStartRow, col); err == nil {
				if ma, err := getDisp(cl, "MergeArea"); err == nil {
					oleutil.CallMethod(ma, "ClearContents")
					ma.Release()
				}
				cl.Release()
			}
		}
	}
	if err := deleteShapesIn(dstWs, dataAreas); err != nil {
		return err
	}

	// 5. 적재 계획 계산 (행 높이 변경 전에 모든 위치를 상대 좌표로 계산)
	type sectionPlan struct {
		sec     SectionInfo
		columns []columnPlan
	}
	var plans []sectionPlan
	for _, sec := range dstSecs {
		sp := sectionPlan{sec: sec}
		for _, colOf := range stackColumns {
			dstCol := colOf(sec.Cols)
			if dstCol == 0 {
				continue
			}
			dstBox, err := readMergeBox(dstWs, sec.DataStartRow, dstCol)
			if err != nil {
				return fmt.Errorf("%s 결과 병합 셀 확인 실패: %w", sec.Name, err)
			}
			var base fontSpec
			if cl, err := getDisp(dstWs, "Cells", dstBox.Row1, dstBox.Col1); err == nil {
				base = readCellFont(cl)
				cl.Release()
			}

			var blocks []stackBlock
			for idx, s := range srcs {
				ssec, ok := s.sections[sec.Name]
				if !ok {
					continue
				}
				srcCol := colOf(ssec.Cols)
				if srcCol == 0 {
					continue
				}
				srcBox, err := readMergeBox(s.ws, ssec.DataStartRow, srcCol)
				if err != nil {
					return fmt.Errorf("시트 '%s' %s 병합 셀 확인 실패: %w", s.name, sec.Name, err)
				}
				b := stackBlock{header: fmt.Sprintf(blockHeaderFormat, idx+1, s.name), src: s, srcBox: srcBox}
				if cl, err := getDisp(s.ws, "Cells", srcBox.Row1, srcBox.Col1); err == nil {
					src := asText(getVal(cl, "Value2"))
					raw := strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
					b.body = strings.TrimRight(raw, " \t\n")
					b.font = readCellFont(cl)
					b.runs = readFontRuns(cl, src, b.body)
					cl.Release()
				}
				for _, sh := range s.shapes {
					if srcBox.contains(sh.Row, sh.Col) {
						b.shapes = append(b.shapes, sh)
					}
				}
				blocks = append(blocks, b)
			}
			if len(blocks) > 0 {
				sp.columns = append(sp.columns, buildColumnPlan(dstBox, base, blocks))
			}
		}
		plans = append(plans, sp)
	}

	// 6. 행 높이 확장 (도형 배치 전에 확정해야 위치가 밀리지 않음)
	for _, sp := range plans {
		need := 0.0
		for _, cp := range sp.columns {
			need = math.Max(need, cp.height)
		}
		if need > 0 {
			if !fitRowHeights(dstWs, sp.sec, need*1.15+blockPadding) { // 줄 높이 추정 오차 여유
				*warnings = append(*warnings, fmt.Sprintf("경고: [%s] 내용이 행 높이 최대치(409pt)를 초과하여 일부가 잘릴 수 있습니다", sp.sec.Name))
			}
		}
	}

	// 7. 값 기입 + 텍스트/서식 적재 + 도형 복사
	for _, sp := range plans {
		sec := sp.sec
		writeCell(dstWs, sec.DataStartRow, sec.Cols.StepCol, sec.StepNumber)
		writeCell(dstWs, sec.DataStartRow, sec.Cols.TargetObjCol, literalText(req.TargetObject)) // ' 접두: 수식/숫자 변환 없이 텍스트로 저장
		writeCell(dstWs, sec.DataStartRow, sec.Cols.WorkerCol, literalText(req.WorkerName))
		writeCell(dstWs, sec.DataStartRow, sec.Cols.VerifierCol, literalText(req.VerifierName))

		for _, cp := range sp.columns {
			if err := writeColumnPlan(dstWs, cp); err != nil {
				return fmt.Errorf("%s 적재 실패: %w", sec.Name, err)
			}
		}
	}
	oleutil.PutProperty(app, "CutCopyMode", false)

	// 8. 프로그램 폴더에 저장
	oleutil.CallMethod(dstWs, "Activate")
	if a1, err := getDisp(dstWs, "Range", "A1"); err == nil {
		oleutil.CallMethod(a1, "Select")
		a1.Release()
	}
	if _, err := oleutil.CallMethod(newWb, "SaveAs", destFullPath, xlOpenXMLWorkbook); err != nil {
		return fmt.Errorf("결과 엑셀 저장 실패 (%s): %w", destFullPath, err)
	}
	return nil
}

// deleteShapesIn removes shapes whose top-left cell lies inside any of the areas
func deleteShapesIn(ws *ole.IDispatch, areas []mergeBox) error {
	list, err := listShapes(ws)
	if err != nil {
		return err
	}
	shapes, err := getDisp(ws, "Shapes")
	if err != nil {
		return fmt.Errorf("Shapes 접근 실패: %w", err)
	}
	defer shapes.Release()

	for i := len(list) - 1; i >= 0; i-- { // 뒤에서부터 삭제해야 인덱스 유지
		s := list[i]
		for _, a := range areas {
			if a.contains(s.Row, s.Col) {
				if sh, err := callDisp(shapes, "Item", s.Index); err == nil {
					oleutil.CallMethod(sh, "Delete")
					sh.Release()
				}
				break
			}
		}
	}
	return nil
}

// fitRowHeights spreads the needed height evenly over the data rows (max 409pt each).
// Returns false when need exceeds 409pt*rows (content may be clipped).
func fitRowHeights(ws *ole.IDispatch, sec SectionInfo, need float64) bool {
	n := sec.DataEndRow - sec.DataStartRow + 1
	if n <= 0 {
		return true
	}
	per := math.Min(maxRowHeight, math.Ceil(need/float64(n)))
	for r := sec.DataStartRow; r <= sec.DataEndRow; r++ {
		row, err := getDisp(ws, "Rows", r)
		if err != nil {
			continue
		}
		cur := getNum(row, "RowHeight")
		oleutil.PutProperty(row, "RowHeight", math.Max(cur, per))
		row.Release()
	}
	return need <= maxRowHeight*float64(n)
}

func writeCell(ws *ole.IDispatch, r, c int, value string) {
	if c == 0 {
		return
	}
	if cl, err := getDisp(ws, "Cells", r, c); err == nil {
		oleutil.PutProperty(cl, "Value2", value)
		cl.Release()
	}
}

// writeColumnPlan writes the stacked text with per-run fonts and pastes the shapes
func writeColumnPlan(ws *ole.IDispatch, cp columnPlan) error {
	box, err := readMergeBox(ws, cp.row, cp.col)
	if err != nil {
		return err
	}
	cl, err := getDisp(ws, "Cells", cp.row, cp.col)
	if err != nil {
		return err
	}
	defer cl.Release()

	if _, err := oleutil.PutProperty(cl, "Value2", cp.text); err != nil {
		return fmt.Errorf("텍스트 기입 실패: %w", err)
	}
	for _, run := range cp.runs {
		applyFont(cl, run.start, run.length, run.font, run.bold)
	}
	if ma, err := getDisp(cl, "MergeArea"); err == nil {
		oleutil.PutProperty(ma, "WrapText", true)
		oleutil.PutProperty(ma, "VerticalAlignment", xlTop)
		ma.Release()
	}

	for _, sp := range cp.shapes {
		if err := copyShape(sp.src.ws, sp.index, ws, box.Left+sp.relX, box.Top+sp.relY, sp.width, sp.height); err != nil {
			return fmt.Errorf("시트 '%s' 도형 복사 실패: %w", sp.src.name, err)
		}
	}
	return nil
}

// copyShape copies a shape across workbooks via the clipboard and positions it
func copyShape(srcWs *ole.IDispatch, index int, dstWs *ole.IDispatch, left, top, width, height float64) error {
	srcShapes, err := getDisp(srcWs, "Shapes")
	if err != nil {
		return err
	}
	defer srcShapes.Release()
	shape, err := callDisp(srcShapes, "Item", index)
	if err != nil {
		return err
	}
	defer shape.Release()
	dstShapes, err := getDisp(dstWs, "Shapes")
	if err != nil {
		return err
	}
	defer dstShapes.Release()

	before := int(getNum(dstShapes, "Count"))
	after := before
	for attempt := 0; attempt < 3 && after <= before; attempt++ {
		oleutil.CallMethod(srcWs, "Activate")
		if _, err := oleutil.CallMethod(shape, "Copy"); err == nil {
			time.Sleep(30 * time.Millisecond)
			oleutil.CallMethod(dstWs, "Activate")
			oleutil.CallMethod(dstWs, "Paste")
		}
		after = int(getNum(dstShapes, "Count"))
		if after <= before {
			time.Sleep(150 * time.Millisecond)
		}
	}
	if after <= before {
		return fmt.Errorf("클립보드 붙여넣기 실패")
	}

	pasted, err := callDisp(dstShapes, "Item", after)
	if err != nil {
		return err
	}
	defer pasted.Release()
	oleutil.PutProperty(pasted, "LockAspectRatio", 0) // msoFalse: 계산된 크기 그대로 적용
	oleutil.PutProperty(pasted, "Width", width)
	oleutil.PutProperty(pasted, "Height", height)
	oleutil.PutProperty(pasted, "Left", left)
	oleutil.PutProperty(pasted, "Top", top)
	return nil
}

// literalText prefixes ' so Excel stores the value as text (empty stays empty)
func literalText(v string) string {
	if v == "" {
		return ""
	}
	return "'" + v
}
