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
	"slices"
	"sort"
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

	maxRowHeight       = 409.0 // Excel 최대 행 높이(pt)
	measureChunkHeight = 250.0 // 한 번에 재는 글 높이 상한(추정치, 행 높이 제한보다 여유 있게)
	maxRichTextChars   = 5000  // 글자별 글꼴 조회 상한(초과 시 단일 글꼴)
	maxScanRows        = 2000  // UsedRange 스캔 상한
	maxScanCols        = 100
	headerLookdown     = 10  // 타이틀 아래 헤더 탐색 행 수 (다음 섹션 제목 전까지)
	blockPadding       = 8.0 // 적재 블록 사이 여백(pt)
	cellInnerPad       = 4.0 // 셀 안쪽 여백(pt)
	maxAnchorLines     = 300 // 도형 기준 줄 계산 상한(초과 시 도형은 본문 뒤)
	anchorTolerance    = 2.0 // 도형 위쪽과 줄 끝 비교 허용 오차(pt)
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
		// 병합 셀 밖으로 40% 삐져나온 이미지 (섹션 인덱스, 적재열 인덱스, 방향)
		overhangs []sampleOverhang
	}

	sheets := []sampleSheet{
		{
			name: "1_OS설치", topRow: 2, gap: 3,
			spacer: [3]bool{false, true, true}, dataRows: [3]int{4, 6, 6},
			texts: [3][3]string{
				{"OS 설치 사전 준비\n- ISO 이미지 무결성 확인", "설치 대상 서버 전원/콘솔 확인\n- iDRAC 접속 계정 확인", "sha256sum 결과 비교"},
				{"OS 설치 진행\n- UEFI 부팅 후 파티션 구성\n- /boot 1GB, swap 4GB\n- 3단계: RAID 1 구성 확인\n- 4단계: 커널 파라미터 적용\n- 5단계: NTP 동기화", "설치 화면 캡처 첨부", "설치 완료 후 재부팅 확인"},
				{"OS 기본 설정\n- 호스트명/시간대 설정", "설정 파일 백업\n- /etc/hosts 백업", "hostnamectl 결과 확인"},
			},
			color:     color.RGBA{R: 59, G: 130, B: 246, A: 255},
			images:    [][2]int{{1, 1}, {1, 0}},
			overhangs: []sampleOverhang{{0, 2, "right"}, {2, 0, "down"}},
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
				{"보안 점검 결과 정리\n- SELinux enforcing 확인", "점검 결과 보고서 작성", "점검 결과 화면 확인\n- 추가: auditd 규칙 재로딩\n- 추가: 결과 스크린샷 보관"},
			},
			color:     color.RGBA{R: 245, G: 158, B: 11, A: 255},
			images:    [][2]int{{1, 1}, {2, 2}},
			overhangs: []sampleOverhang{{0, 0, "left"}, {2, 1, "up"}},
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

			// 병합 셀 경계 밖으로 40% 삐져나온 이미지 (이웃 열·행에 앵커를 두고 오프셋으로 맞춤)
			for _, ov := range sh.overhangs {
				if ov.sec != s {
					continue
				}
				col := stackIdx[ov.stack] + 2
				anchorCol, anchorRow, offX, offY := col, start+1, 6.0, 4.0
				switch ov.dir {
				case "right":
					offX = colPx(widths[col-2]) - 0.6*sampleImgW
				case "left":
					anchorCol, offX = col-1, colPx(widths[col-3])-0.4*sampleImgW
				case "up":
					anchorRow, offY = start-1, 33*4.0/3-0.4*sampleImgH
				case "down":
					anchorRow, offY = end, 24*4.0/3-0.6*sampleImgH
				}
				if err := f.AddPictureFromBytes(sh.name, cell(anchorCol, anchorRow), &excelize.Picture{
					Extension: ".png",
					File:      generateSampleBadge(sh.color),
					Format:    &excelize.GraphicOptions{ScaleX: 0.6, ScaleY: 0.6, OffsetX: int(offX), OffsetY: int(offY)},
				}); err != nil {
					return fmt.Errorf("샘플 이미지 삽입 실패 (%s): %w", sh.name, err)
				}
			}
			row = end + 1 + sh.gap
		}
	}

	return f.SaveAs(destPath)
}

type sampleOverhang struct {
	sec, stack int
	dir        string // right / left / up / down
}

// 샘플 배지 이미지 크기(px, 240x60 의 0.6배)
const sampleImgW, sampleImgH = 144.0, 36.0

// colPx converts a column width (characters) to pixels the way excelize does (기본 글꼴 최대 숫자 폭 7px)
func colPx(w float64) float64 {
	return math.Ceil(w*7 + 5.5)
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
	oleutil.PutProperty(app, "ScreenUpdating", false) // 숨김 상태여도 화면 갱신을 끄면 서식·높이 변경이 빨라진다
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
		return nil, fmt.Errorf("SOP 파일 열기 실패 (%s): %w", path, err)
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
		return "", fmt.Errorf("SOP 파일 경로가 비어 있습니다")
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(GetAppDir(), p)
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("SOP 파일을 찾을 수 없습니다 (%s): %w", p, err)
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

// sectionTitleKey strips numbering/bullets and a trailing note from a whitespace-free
// title, e.g. "3.사후작업", "■사후작업", "사후작업(복구)" → "사후작업"
func sectionTitleKey(key string) string {
	key = strings.TrimLeftFunc(key, func(r rune) bool {
		return unicode.IsDigit(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) || (r >= 'Ⅰ' && r <= 'ⅿ')
	})
	if i := strings.IndexAny(key, "([{<（［【"); i > 0 {
		key = key[:i]
	}
	return strings.TrimRightFunc(key, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) })
}

// rowHasSectionTitle reports whether row r contains any section title
func rowHasSectionTitle(g *sheetGrid, r int) bool {
	i := r - g.row0
	if i < 0 || i >= len(g.cells) {
		return false
	}
	for _, txt := range g.cells[i] {
		k := sectionTitleKey(normalizeKey(txt))
		for _, def := range sectionDefs {
			if k == def.Name {
				return true
			}
		}
	}
	return false
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
				if sectionTitleKey(key) != def.Name {
					continue
				}
				if _, dup := found[def.Name]; dup {
					continue
				}
				for hr := r + 1; hr <= r+headerLookdown; hr++ {
					if hr > r+1 && rowHasSectionTitle(g, hr) {
						break // 다음 섹션 제목을 만나면 이 제목에는 헤더가 없는 것
					}
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

// centerIn reports whether the shape's center lies inside the box, so a shape that
// sticks partly out of its merged cell still belongs to the cell holding most of it
func (s shapeInfo) centerIn(b mergeBox) bool {
	cx, cy := s.Left+s.Width/2, s.Top+s.Height/2
	return cx >= b.Left && cx < b.Left+b.Width && cy >= b.Top && cy < b.Top+b.Height
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

	// raw 위치(1-based UTF-16) → body 위치: CRLF 의 CR 은 body 에서 빠진다
	units := utf16.Encode([]rune(raw))
	dstOf := make([]int, len(units)+2)
	d := 1
	for p := 1; p <= len(units); p++ {
		dstOf[p] = d
		if !(units[p-1] == '\r' && p < len(units) && units[p] == '\n') {
			d++
		}
	}
	dstOf[len(units)+1] = d

	// 줄 단위로 글꼴을 묻고, 한 줄 안에서 글꼴이 섞여 있을 때만 반씩 나눠 묻는다
	// (글자마다 묻던 방식은 긴 셀에서 수천 번의 COM 호출로 수 분이 걸렸다)
	var runs []fontRun
	var walk func(start, length int)
	walk = func(start, length int) {
		var fs fontSpec
		if ch, err := getDisp(cl, "Characters", start, length); err == nil {
			if f, err := getDisp(ch, "Font"); err == nil {
				fs.fillFrom(f)
				f.Release()
			}
			ch.Release()
		}
		if !fs.complete() && length > 1 {
			half := length / 2
			walk(start, half)
			walk(start+half, length-half)
			return
		}
		s, e := dstOf[start], min(dstOf[start+length], bodyLen+1)
		if s >= e {
			return
		}
		if k := len(runs) - 1; k >= 0 && runs[k].font == fs && runs[k].start+runs[k].length == s {
			runs[k].length += e - s
		} else {
			runs = append(runs, fontRun{start: s, length: e - s, font: fs, bold: fs.Bold})
		}
	}
	// 줄바꿈 글자는 앞뒤 어느 줄의 글꼴을 따를지 정해져 있지 않아 줄 글자와 따로 묻는다
	lineStart := 1
	for p := 1; p <= len(units); p++ {
		if units[p-1] == '\r' || units[p-1] == '\n' {
			if p > lineStart {
				walk(lineStart, p-lineStart)
			}
			walk(p, 1)
			lineStart = p + 1
		}
	}
	if lineStart <= len(units) {
		walk(lineStart, len(units)-lineStart+1)
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

// textMeasurer measures wrapped text height with Excel itself (row AutoFit on a
// scratch sheet) instead of estimating it from character counts
type textMeasurer struct {
	ws, cl *ole.IDispatch
	width  float64            // 현재 측정 열 너비(pt)
	minH   float64            // AutoFit 최소 높이(시트 기본 행 높이), 이 이하로 나온 값은 실제보다 크다
	cache  map[string]float64 // 같은 글·글꼴·너비는 다시 재지 않는다
}

func newTextMeasurer(wb *ole.IDispatch) *textMeasurer {
	sheets, err := getDisp(wb, "Worksheets")
	if err != nil {
		return nil
	}
	defer sheets.Release()
	ws, err := callDisp(sheets, "Add")
	if err != nil {
		return nil
	}
	cl, err := getDisp(ws, "Cells", 1, 1)
	if err != nil {
		oleutil.CallMethod(ws, "Delete")
		ws.Release()
		return nil
	}
	oleutil.PutProperty(cl, "WrapText", true)
	oleutil.PutProperty(cl, "VerticalAlignment", xlTop)
	return &textMeasurer{ws: ws, cl: cl, minH: getNum(ws, "StandardHeight"), cache: map[string]float64{}}
}

// close deletes the scratch sheet; safe to call more than once
func (m *textMeasurer) close() {
	if m == nil || m.ws == nil {
		return
	}
	m.cl.Release()
	oleutil.CallMethod(m.ws, "Delete")
	m.ws.Release()
	m.ws, m.cl = nil, nil
}

func (m *textMeasurer) setWidth(width float64) {
	if m.width == width {
		return
	}
	col, err := getDisp(m.ws, "Columns", 1)
	if err != nil {
		return
	}
	defer col.Release()
	// 열 너비(문자 단위)와 pt 는 선형 관계이므로 두 점으로 환산
	oleutil.PutProperty(col, "ColumnWidth", 10)
	w10 := getNum(col, "Width")
	oleutil.PutProperty(col, "ColumnWidth", 20)
	w20 := getNum(col, "Width")
	if w20 > w10 {
		oleutil.PutProperty(col, "ColumnWidth", math.Max(0, math.Min(255, 10+(width-w10)*10/(w20-w10))))
	}
	m.width = width
}

// height returns the wrapped height of s in a cell of the given width. The text is
// measured in chunks of lines (estimated below measureChunkHeight) and summed, so
// a chunk never hits the row height limit and text that only grows at the end
// reuses the cached measurements of its earlier chunks
func (m *textMeasurer) height(s string, fs fontSpec, runs []fontRun, bold bool, width float64) float64 {
	lines := strings.Split(s, "\n")
	var cuts []int // 각 조각의 시작 줄
	est, hasText := 0.0, false
	for i, ln := range lines {
		le := textHeight(ln, fs, width)
		if i > 0 && hasText && est+le > measureChunkHeight {
			cuts = append(cuts, i)
			est, hasText = 0, false
		}
		est += le
		hasText = hasText || strings.TrimSpace(ln) != ""
	}
	if len(cuts) > 0 && !hasText { // 빈 줄만 남은 마지막 조각은 앞 조각에 붙인다
		cuts = cuts[:len(cuts)-1]
	}
	if len(cuts) == 0 {
		return m.split(s, fs, runs, bold, width)
	}
	pad := math.Max(0, m.measure("가", fs, nil, bold, width)-m.lineHeight(fs)) // 측정마다 붙는 셀 여백
	total, from, start := 0.0, 1, 0
	for _, end := range append(cuts, len(lines)) {
		part := strings.Join(lines[start:end], "\n")
		to := from + utf16Len(part) - 1
		total += m.split(part, fs, sliceRuns(runs, from, to), bold, width)
		from, start = to+2, end // 조각 사이 줄바꿈 건너뜀
	}
	return total - pad*float64(len(cuts))
}

// split measures s at once, or in halves when it reaches the row height limit (409pt)
func (m *textMeasurer) split(s string, fs fontSpec, runs []fontRun, bold bool, width float64) float64 {
	h := m.measure(s, fs, runs, bold, width)
	if h < maxRowHeight {
		return h
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return math.Max(h, textHeight(s, fs, width))
	}
	mid := len(lines) / 2
	first := strings.Join(lines[:mid], "\n")
	cut := utf16Len(first) + 1 // 두 번째 조각 시작(1-based)
	second := strings.Join(lines[mid:], "\n")
	pad := math.Max(0, m.measure("가", fs, nil, bold, width)-m.lineHeight(fs)) // 측정마다 붙는 셀 여백
	return m.split(first, fs, sliceRuns(runs, 1, cut-1), bold, width) +
		m.split(second, fs, sliceRuns(runs, cut+1, utf16Len(s)), bold, width) - pad
}

// measure is one AutoFit measurement (capped at the row height limit). AutoFit
// never goes below the sheet's standard row height, so short text is measured
// below a few reference lines whose pitch is then subtracted.
func (m *textMeasurer) measure(s string, fs fontSpec, runs []fontRun, bold bool, width float64) float64 {
	if m == nil || m.ws == nil {
		return textHeight(s, fs, width)
	}
	h := m.raw(s, fs, runs, bold, width)
	if h > m.minH+0.01 || s == "" {
		return h
	}
	k := m.refLines(fs)
	prefix := strings.Repeat("가\n", k)
	shifted := make([]fontRun, len(runs))
	for i, r := range runs {
		r.start += utf16Len(prefix)
		shifted[i] = r
	}
	return m.raw(prefix+s, fs, shifted, bold, width) - float64(k)*m.pitch(fs, bold, width)
}

// raw is a cached AutoFit measurement as Excel reports it
func (m *textMeasurer) raw(s string, fs fontSpec, runs []fontRun, bold bool, width float64) float64 {
	runs = mergeRuns(runs)
	key := fmt.Sprintf("%v|%v|%v|%v|%s", width, fs, bold, runs, s)
	if h, ok := m.cache[key]; ok {
		return h
	}
	h := m.autoFit(s, fs, runs, bold, width)
	m.cache[key] = h
	return h
}

// refLines is the number of reference lines that is surely taller than minH
func (m *textMeasurer) refLines(fs fontSpec) int {
	return max(2, int(math.Ceil(m.minH*1.5/lineHeight(fs))))
}

// pitch is the height one more line of fs adds, from two multi-line measurements
// (a single line is clamped to minH, which made the pitch and margins wrong)
func (m *textMeasurer) pitch(fs fontSpec, bold bool, width float64) float64 {
	k := m.refLines(fs)
	a := m.raw(strings.Repeat("가\n", k-1)+"가", fs, nil, bold, width)
	b := m.raw(strings.Repeat("가\n", 2*k-1)+"가", fs, nil, bold, width)
	if p := (b - a) / float64(k); p > 0 && a > m.minH+0.01 && b < maxRowHeight {
		return p
	}
	return lineHeight(fs)
}

func (m *textMeasurer) autoFit(s string, fs fontSpec, runs []fontRun, bold bool, width float64) float64 {
	est := textHeight(s, fs, width)
	m.setWidth(width)
	if _, err := oleutil.PutProperty(m.cl, "Value2", literalText(s)); err != nil { // ' 접두: "- ..." 가 수식으로 해석되지 않도록
		return est
	}
	applyFont(m.cl, 1, utf16Len(s), fs, bold)
	for _, r := range runs {
		applyFont(m.cl, r.start, r.length, r.font, r.bold)
	}
	row, err := getDisp(m.ws, "Rows", 1)
	if err != nil {
		return est
	}
	defer row.Release()
	oleutil.CallMethod(row, "AutoFit")
	h := getNum(row, "RowHeight")
	if h <= 0 {
		return est
	}
	if h >= maxRowHeight {
		return math.Max(h, est)
	}
	return h
}

// lineHeight returns the line pitch of fs (height added by one more line)
func (m *textMeasurer) lineHeight(fs fontSpec) float64 {
	if m == nil || m.ws == nil {
		return lineHeight(fs)
	}
	return m.pitch(fs, fs.Bold, math.Max(m.width, 100))
}

// sliceRuns returns the runs inside [from, to] (1-based UTF-16), rebased to start at 1
func sliceRuns(runs []fontRun, from, to int) []fontRun {
	var out []fontRun
	for _, r := range runs {
		s, e := max(r.start, from), min(r.start+r.length-1, to)
		if s > e {
			continue
		}
		r.start, r.length = s-from+1, e-s+1
		out = append(out, r)
	}
	return out
}

// mergeRuns sorts runs by position and joins adjacent runs with the same font, so
// each font change costs one Characters call instead of one per line
func mergeRuns(runs []fontRun) []fontRun {
	if len(runs) < 2 {
		return runs
	}
	sorted := slices.Clone(runs)
	slices.SortStableFunc(sorted, func(a, b fontRun) int { return a.start - b.start })
	out := sorted[:1]
	for _, r := range sorted[1:] {
		last := &out[len(out)-1]
		if last.start+last.length == r.start && last.font == r.font && last.bold == r.bold {
			last.length += r.length
			continue
		}
		out = append(out, r)
	}
	return out
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
// block header starts below the previous block's text and images. Positions are
// measured on the accumulated cell text itself, so they do not drift block by block.
func buildColumnPlan(dst mergeBox, base fontSpec, blocks []stackBlock, m *textMeasurer) columnPlan {
	plan := columnPlan{row: dst.Row1, col: dst.Col1}
	var sb strings.Builder
	pos := 0         // 지금까지 기록한 UTF-16 길이
	nextStart := 0.0 // 다음 블록이 시작해야 하는 Y
	padH := m.lineHeight(base)
	oneLine := m.measure("가", base, nil, false, dst.Width)
	maxShapeW := dst.Width - 2*cellInnerPad

	// startAfter is the Y where a line starts after the current text plus nl line breaks
	startAfter := func(nl int) float64 {
		if pos == 0 && nl == 0 {
			return 0
		}
		return m.height(sb.String()+strings.Repeat("\n", nl)+"가", base, plan.runs, false, dst.Width) - oneLine
	}

	for i, b := range blocks {
		if i > 0 {
			nl := max(2, int(math.Ceil((nextStart-startAfter(1))/padH))+1)
			for tries := 0; tries < 5; tries++ { // 모자란 높이만큼 줄을 한 번에 보충
				d := nextStart - startAfter(nl)
				if d <= 0 {
					break
				}
				nl += max(1, int(math.Ceil(d/padH)))
			}
			sb.WriteString(strings.Repeat("\n", nl))
			pos += nl
		}

		hl := utf16Len(b.header)
		plan.runs = append(plan.runs, fontRun{start: pos + 1, length: hl, font: base, bold: true})
		sb.WriteString(b.header)
		pos += hl

		lb := layoutBody(b, dst.Width, maxShapeW, m)
		bodyStart := startAfter(1)
		if lb.body != "" {
			sb.WriteString("\n")
			pos++
			bl := utf16Len(lb.body)
			if len(lb.runs) > 0 {
				for _, r := range lb.runs {
					r.start += pos
					plan.runs = append(plan.runs, r)
				}
			} else {
				plan.runs = append(plan.runs, fontRun{start: pos + 1, length: bl, font: b.font, bold: b.font.Bold})
			}
			sb.WriteString(lb.body)
			pos += bl
		}
		textY := m.height(sb.String(), base, plan.runs, false, dst.Width)

		// 도형은 원본에서 놓여 있던 줄 위치에 배치 (layoutBody 가 그 자리에 빈 줄을 확보)
		shapeBottom := 0.0
		for _, sp := range lb.shapes {
			sp.relY += bodyStart
			shapeBottom = math.Max(shapeBottom, sp.relY+sp.height)
			plan.shapes = append(plan.shapes, sp)
		}
		nextStart = math.Max(textY, shapeBottom) + blockPadding
	}

	plan.text = sb.String()
	plan.height = nextStart
	return plan
}

// bodyLayout is one block's body text with blank lines inserted for its shapes
type bodyLayout struct {
	body   string
	runs   []fontRun   // body 기준(1-based) 글꼴 구간
	shapes []shapePlan // relY 는 본문 시작 기준
}

// layoutBody keeps each shape at the text line it sat on in the source: the shape
// group is placed after the lines that end above it, and blank lines are added so
// the following text starts below the shapes (existing blank lines are reused).
// Heights are measured on the final text with its fonts so text is never hidden.
func layoutBody(b stackBlock, dstWidth, maxShapeW float64, m *textMeasurer) bodyLayout {
	out := bodyLayout{body: b.body, runs: b.runs}
	if len(b.shapes) == 0 {
		return out
	}
	var lines []string
	if b.body != "" {
		lines = strings.Split(b.body, "\n")
	}
	n := len(lines)
	bold := b.font.Bold
	if len(b.runs) == 0 && b.body != "" { // 단일 글꼴 본문도 구간으로 다뤄 빈 줄 삽입 후에도 글꼴 유지
		b.runs = []fontRun{{start: 1, length: utf16Len(b.body), font: b.font, bold: bold}}
	}

	// 원본 셀에서 앞쪽 k줄이 차지하는 높이 (k=0 → 0), 필요한 k 만 측정
	srcMemo := map[int]float64{0: 0}
	srcH := func(k int) float64 {
		if h, ok := srcMemo[k]; ok {
			return h
		}
		h := math.Inf(1) // 너무 긴 본문은 도형을 모두 맨 앞 기준으로 둔다
		if n <= maxAnchorLines {
			t := strings.Join(lines[:k], "\n")
			h = m.height(t, b.font, sliceRuns(b.runs, 1, utf16Len(t)), bold, b.srcBox.Width)
		}
		srcMemo[k] = h
		return h
	}

	// 도형마다 기준 줄 수(anchor): 도형 위쪽보다 위에서 끝나는 줄 수
	type item struct {
		s      shapeInfo
		anchor int
		relTop float64
	}
	items := make([]item, 0, len(b.shapes))
	for _, s := range b.shapes {
		relTop := math.Max(0, s.Top-b.srcBox.Top)
		// 줄이 늘수록 높이가 커지므로 이분 탐색
		k := sort.Search(n, func(i int) bool { return srcH(i+1) > relTop+anchorTolerance })
		items = append(items, item{s: s, anchor: k, relTop: relTop})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].anchor < items[j].anchor })

	lineOff := make([]int, n+1) // 원본 body 에서 각 줄의 시작 UTF-16 위치
	for i := 0; i < n; i++ {
		lineOff[i+1] = lineOff[i] + utf16Len(lines[i]) + 1
	}
	added := make([]int, n) // 각 줄 앞에 추가한 빈 줄 수
	lh := m.lineHeight(b.font)

	// build returns the text and runs of the first k lines with the blank lines added so far;
	// added blank lines get the block font so their height is exactly lh
	build := func(k int) (string, []fontRun) {
		var sb strings.Builder
		var runs []fontRun
		shiftAt := func(off int) int { // 원본 위치 off 앞에 추가된 줄바꿈 수
			c := 0
			for i := 0; i < n && lineOff[i] <= off; i++ {
				c += added[i]
			}
			return c
		}
		pos := 0
		for i := 0; i < k; i++ {
			if i > 0 {
				sb.WriteString("\n")
				pos++
			}
			if added[i] > 0 {
				runs = append(runs, fontRun{start: pos + 1, length: added[i], font: b.font, bold: bold})
				sb.WriteString(strings.Repeat("\n", added[i]))
				pos += added[i]
			}
			sb.WriteString(lines[i])
			pos += utf16Len(lines[i])
		}
		end := 0
		if k > 0 {
			end = lineOff[k] - 1
		}
		for _, r := range b.runs {
			s0, e0 := r.start-1, min(r.start-1+r.length, end)
			if s0 >= e0 {
				continue
			}
			// 줄 시작 앞에 끼운 빈 줄은 위 runs 가 담당하므로 원본 구간은 잘라서 옮긴다
			for i := 0; i < n; i++ {
				ls, le := lineOff[i], lineOff[i]+utf16Len(lines[i])+1 // 줄 + 뒤 줄바꿈
				ps, pe := max(s0, ls), min(e0, le)
				if ps >= pe {
					continue
				}
				d := shiftAt(ls)
				runs = append(runs, fontRun{start: ps + d + 1, length: pe - ps, font: r.font, bold: r.bold})
			}
		}
		return sb.String(), runs
	}
	textTo := func(k int) float64 { // 결과 셀에서 앞쪽 k줄이 끝나는 높이
		if k == 0 {
			return 0
		}
		t, rs := build(k)
		return m.height(t, b.font, rs, bold, dstWidth)
	}
	oneLine := m.measure("가", b.font, nil, bold, dstWidth)
	startOf := func(idx int) float64 { // 결과 셀에서 줄 idx 가 시작하는 높이
		t, rs := build(idx)
		var sb strings.Builder
		sb.WriteString(t)
		if idx > 0 {
			sb.WriteString("\n")
		}
		at := utf16Len(sb.String()) + 1
		sb.WriteString(strings.Repeat("\n", added[idx]))
		sb.WriteString("가")
		rs = append(rs, fontRun{start: at, length: added[idx] + 1, font: b.font, bold: bold})
		return m.height(sb.String(), b.font, rs, bold, dstWidth) - oneLine
	}

	for i := 0; i < len(items); {
		k := items[i].anchor
		j := i
		for j < len(items) && items[j].anchor == k {
			j++
		}
		group := items[i:j]
		i = j

		dstY := textTo(k)
		groupTop := math.Inf(1)
		for _, it := range group {
			groupTop = math.Min(groupTop, it.relTop)
		}
		gap := 0.0 // 원본의 줄-도형 간격 유지
		if !math.IsInf(srcH(k), 0) {
			gap = math.Max(0, groupTop-srcH(k))
		}

		bottom := 0.0
		for _, it := range group {
			s := it.s
			w, h := s.Width, s.Height
			if w > maxShapeW && maxShapeW > 0 { // 열보다 넓으면 비율 유지 축소
				h *= maxShapeW / w
				w = maxShapeW
			}
			relX := math.Max(0, s.Left-b.srcBox.Left)
			if relX+w > dstWidth-cellInnerPad {
				relX = math.Max(0, dstWidth-cellInnerPad-w)
			}
			relY := dstY + gap + (it.relTop - groupTop) // 묶음 내부 배치 유지
			out.shapes = append(out.shapes, shapePlan{
				src: b.src, index: s.Index,
				relX: relX, relY: relY,
				width: w, height: h,
			})
			bottom = math.Max(bottom, relY+h)
		}

		// 뒤따르는 첫 글자 줄이 도형 아래에서 시작하도록 빈 줄 보충 (+1줄 여유)
		next := k
		for next < n && strings.TrimSpace(lines[next]) == "" {
			next++
		}
		if next >= n {
			continue // 뒤에 글이 없으면 다음 블록 간격으로 처리됨
		}
		need := int(math.Ceil((bottom+blockPadding-dstY)/lh)) + 1
		if extra := need - (next - k); extra > 0 {
			added[next] += extra
		}
		// 실측 확인: 다음 글이 도형 하단 아래에서 시작할 때까지 보충
		for tries := 0; tries < 5; tries++ {
			d := bottom + blockPadding - startOf(next)
			if d <= 0 {
				break
			}
			added[next] += max(1, int(math.Ceil(d/lh)))
		}
	}

	out.body, out.runs = build(n)
	return out
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
			return nil, fmt.Errorf("결과 파일 이름이 SOP 파일과 같습니다. 다른 이름을 지정하세요 (%s)", dstFilename)
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
		return fmt.Errorf("SOP 파일 Worksheets 획득 실패: %w", err)
	}
	defer srcSheets.Release()

	// 2. 선택 시트별 섹션/도형 분석
	srcs := make([]*srcSheet, 0, len(selected))
	for _, name := range selected {
		ws, err := getDisp(srcSheets, "Item", name)
		if err != nil {
			return fmt.Errorf("선택한 시트 '%s'를 SOP 파일에서 찾을 수 없습니다", name)
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
		if rg, err := getDisp(dstWs, "Range", cell(area.Col1, area.Row1)+":"+cell(area.Col2, area.Row2)); err == nil {
			area.Left, area.Top = getNum(rg, "Left"), getNum(rg, "Top")
			area.Width, area.Height = getNum(rg, "Width"), getNum(rg, "Height")
			rg.Release()
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
	measurer := newTextMeasurer(newWb) // 임시 측정 시트, 저장 전에 삭제
	defer measurer.close()
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
					msg := fmt.Sprintf("경고: 시트 '%s'에서 [%s] 섹션(제목 + Step/작업내용 헤더)을 찾지 못해 해당 내용이 제외되었습니다", s.name, sec.Name)
					if !slices.Contains(*warnings, msg) {
						*warnings = append(*warnings, msg)
					}
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
					if sh.centerIn(srcBox) {
						b.shapes = append(b.shapes, sh)
					}
				}
				blocks = append(blocks, b)
			}
			if len(blocks) > 0 {
				sp.columns = append(sp.columns, buildColumnPlan(dstBox, base, blocks, measurer))
			}
		}
		plans = append(plans, sp)
	}
	measurer.close()

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

// deleteShapesIn removes shapes whose center lies inside any of the areas
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
			if s.centerIn(a) {
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
	for _, run := range mergeRuns(cp.runs) {
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
