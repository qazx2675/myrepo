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
	"strings"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/xuri/excelize/v2"
)

// TargetSheetInfo represents information about a sheet in the target Excel file
type TargetSheetInfo struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	H2Summary   string `json:"h2_summary"`
	ShapeCount  int    `json:"shape_count"`
	HasH2       bool   `json:"has_h2"`
	H2MergeArea string `json:"h2_merge_area"`
}

// TemplatePlaceholders stores references to the placeholder cells in the template
type TemplatePlaceholders struct {
	MajorRow    int
	MajorCol    int
	MajorRange  string
	MinorRow    int
	MinorCol    int
	MinorRange  string
	DetailRow   int
	DetailCol   int
	DetailRange string
	MethodRow   int
	MethodCol   int
	MethodRange string
}

// SOPGenerateRequest encapsulates all parameters required to generate an SOP Excel
type SOPGenerateRequest struct {
	TargetPath     string   `json:"target_path"`
	TemplatePath   string   `json:"template_path"`
	MajorCategory  string   `json:"major_category"`
	MinorCategory  string   `json:"minor_category"`
	DetailText     string   `json:"detail_text"`
	SelectedSheets []string `json:"selected_sheets"`
	DstFilename    string   `json:"dst_filename"`
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

// CreateDefaultTemplate generates a standard SOP template Excel file
// matching the user specification:
// Row 2: B2="작업자", C2:D2
// Row 3: B3="확인자", C3:D3
// Row 8 (Header): B8="대분류", C8="중분류", G8="상세내용", H8="작업방법"
// Below Row 8 (Data merged cells):
// B9:B15="대분류", C9:F15="중분류", G9:G15="상세내용", H9:K15="작업방법"
func CreateDefaultTemplate(destPath string) error {
	_ = os.MkdirAll(filepath.Dir(destPath), 0755)

	f := excelize.NewFile()
	defer f.Close()

	sheet := "SOP_양식"
	defaultSheet := f.GetSheetName(0)
	f.SetSheetName(defaultSheet, sheet)

	// Column widths
	_ = f.SetColWidth(sheet, "A", "A", 4)
	_ = f.SetColWidth(sheet, "B", "B", 15)
	_ = f.SetColWidth(sheet, "C", "C", 16)
	_ = f.SetColWidth(sheet, "D", "F", 12)
	_ = f.SetColWidth(sheet, "G", "G", 32)
	_ = f.SetColWidth(sheet, "H", "K", 22)

	// Styles
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "#1E293B", Family: "맑은 고딕"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#E2E8F0"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#94A3B8", Style: 1},
			{Type: "top", Color: "#94A3B8", Style: 1},
			{Type: "bottom", Color: "#94A3B8", Style: 1},
			{Type: "right", Color: "#94A3B8", Style: 1},
		},
	})

	metaHeaderStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 10, Color: "#334155", Family: "맑은 고딕"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#F1F5F9"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#CBD5E1", Style: 1},
			{Type: "top", Color: "#CBD5E1", Style: 1},
			{Type: "bottom", Color: "#CBD5E1", Style: 1},
			{Type: "right", Color: "#CBD5E1", Style: 1},
		},
	})

	metaValueStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#0F172A", Family: "맑은 고딕"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#CBD5E1", Style: 1},
			{Type: "top", Color: "#CBD5E1", Style: 1},
			{Type: "bottom", Color: "#CBD5E1", Style: 1},
			{Type: "right", Color: "#CBD5E1", Style: 1},
		},
	})

	centerPlaceholderStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 11, Color: "#64748B", Family: "맑은 고딕"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#CBD5E1", Style: 1},
			{Type: "top", Color: "#CBD5E1", Style: 1},
			{Type: "bottom", Color: "#CBD5E1", Style: 1},
			{Type: "right", Color: "#CBD5E1", Style: 1},
		},
	})

	topPlaceholderStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#64748B", Family: "맑은 고딕"},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "top", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#CBD5E1", Style: 1},
			{Type: "top", Color: "#CBD5E1", Style: 1},
			{Type: "bottom", Color: "#CBD5E1", Style: 1},
			{Type: "right", Color: "#CBD5E1", Style: 1},
		},
	})

	// Row 2: 작업자 (Worker)
	_ = f.SetRowHeight(sheet, 2, 24)
	_ = f.SetCellValue(sheet, "B2", "작업자")
	_ = f.SetCellStyle(sheet, "B2", "B2", metaHeaderStyle)
	_ = f.MergeCell(sheet, "C2", "D2")
	_ = f.SetCellStyle(sheet, "C2", "D2", metaValueStyle)

	// Row 3: 확인자 (Checker)
	_ = f.SetRowHeight(sheet, 3, 24)
	_ = f.SetCellValue(sheet, "B3", "확인자")
	_ = f.SetCellStyle(sheet, "B3", "B3", metaHeaderStyle)
	_ = f.MergeCell(sheet, "C3", "D3")
	_ = f.SetCellStyle(sheet, "C3", "D3", metaValueStyle)

	// Row 8: Table Header
	_ = f.SetRowHeight(sheet, 8, 30)
	_ = f.SetCellValue(sheet, "B8", "대분류")
	_ = f.SetCellStyle(sheet, "B8", "B8", headerStyle)

	_ = f.MergeCell(sheet, "C8", "F8")
	_ = f.SetCellValue(sheet, "C8", "중분류")
	_ = f.SetCellStyle(sheet, "C8", "F8", headerStyle)

	_ = f.SetCellValue(sheet, "G8", "상세내용")
	_ = f.SetCellStyle(sheet, "G8", "G8", headerStyle)

	_ = f.MergeCell(sheet, "H8", "K8")
	_ = f.SetCellValue(sheet, "H8", "작업방법")
	_ = f.SetCellStyle(sheet, "H8", "K8", headerStyle)

	// Rows 9..15: Data Placeholder Merged Cells
	for r := 9; r <= 15; r++ {
		_ = f.SetRowHeight(sheet, r, 20)
	}

	// B9:B15 -> 대분류
	_ = f.MergeCell(sheet, "B9", "B15")
	_ = f.SetCellValue(sheet, "B9", "대분류")
	_ = f.SetCellStyle(sheet, "B9", "B15", centerPlaceholderStyle)

	// C9:F15 -> 중분류
	_ = f.MergeCell(sheet, "C9", "F15")
	_ = f.SetCellValue(sheet, "C9", "중분류")
	_ = f.SetCellStyle(sheet, "C9", "F15", centerPlaceholderStyle)

	// G9:G15 -> 상세내용
	_ = f.MergeCell(sheet, "G9", "G15")
	_ = f.SetCellValue(sheet, "G9", "상세내용")
	_ = f.SetCellStyle(sheet, "G9", "G15", topPlaceholderStyle)

	// H9:K15 -> 작업방법
	_ = f.MergeCell(sheet, "H9", "K15")
	_ = f.SetCellValue(sheet, "H9", "작업방법")
	_ = f.SetCellStyle(sheet, "H9", "K15", topPlaceholderStyle)

	return f.SaveAs(destPath)
}

// CreateSampleTarget creates a multi-sheet target.xlsx file
// containing H2 merged cells with text and embedded images/shapes for testing
func CreateSampleTarget(destPath string) error {
	_ = os.MkdirAll(filepath.Dir(destPath), 0755)

	f := excelize.NewFile()
	defer f.Close()

	// Sheet 1: 1_OS설치
	s1 := "1_OS설치"
	defaultSheet := f.GetSheetName(0)
	f.SetSheetName(defaultSheet, s1)

	// Sheet 2: 2_네트워크구성
	s2 := "2_네트워크구성"
	_, _ = f.NewSheet(s2)

	// Sheet 3: 3_보안점검
	s3 := "3_보안점검"
	_, _ = f.NewSheet(s3)

	sheets := []struct {
		name    string
		title   string
		content string
		color   color.RGBA
	}{
		{
			name:  s1,
			title: "1. OS 설치 및 기본 환경 구성",
			content: "1. ISO 이미지 마운트 및 UEFI 부팅\n" +
				"2. 파티션 구성:\n" +
				"   - /boot: 1024MB (xfs)\n" +
				"   - swap: 4096MB\n" +
				"   - /: 나머지 전체 용량 할당\n" +
				"3. 관리자(root) 패스워드 설정 및 설치 시작",
			color: color.RGBA{R: 59, G: 130, B: 246, A: 255}, // Blue
		},
		{
			name:  s2,
			title: "2. 네트워크 및 본딩(Bonding) 구성",
			content: "1. 물리 NIC 확인 (ens192, ens224)\n" +
				"2. LACP(Mode 4) 본딩 bond0 생성\n" +
				"   - IP: 192.168.10.50/24\n" +
				"   - Gateway: 192.168.10.1\n" +
				"   - DNS: 168.126.63.1, 8.8.8.8\n" +
				"3. 게이트웨이 핑 테스트 및 외부 통신 확인",
			color: color.RGBA{R: 16, G: 185, B: 129, A: 255}, // Green
		},
		{
			name:  s3,
			title: "3. 시스템 보안 및 방화벽 설정",
			content: "1. SSH 원격 접속 보안 설정:\n" +
				"   - /etc/ssh/sshd_config -> PermitRootLogin no\n" +
				"2. firewalld 방화벽 활성화 및 포트 제어:\n" +
				"   - 허용 포트: 22(SSH), 443(HTTPS)\n" +
				"3. SELinux enforcing 상태 확인 완료",
			color: color.RGBA{R: 245, G: 158, B: 11, A: 255}, // Orange
		},
	}

	h2Style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 11, Color: "#1E293B", Family: "맑은 고딕"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#F8FAFC"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "top", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#94A3B8", Style: 1},
			{Type: "top", Color: "#94A3B8", Style: 1},
			{Type: "bottom", Color: "#94A3B8", Style: 1},
			{Type: "right", Color: "#94A3B8", Style: 1},
		},
	})

	for _, item := range sheets {
		// Set column widths
		_ = f.SetColWidth(item.name, "A", "G", 10)
		_ = f.SetColWidth(item.name, "H", "K", 24)

		for r := 2; r <= 8; r++ {
			_ = f.SetRowHeight(item.name, r, 22)
		}

		// Merge H2:K8
		_ = f.MergeCell(item.name, "H2", "K8")
		_ = f.SetCellValue(item.name, "H2", item.content)
		_ = f.SetCellStyle(item.name, "H2", "K8", h2Style)

		// Create a sample badge image and insert into H2 area
		imgData := generateSampleBadge(item.title, item.color)
		if len(imgData) > 0 {
			_ = f.AddPictureFromBytes(item.name, "J3", &excelize.Picture{
				Extension: ".png",
				File:      imgData,
				Format: &excelize.GraphicOptions{
					ScaleX:  0.8,
					ScaleY:  0.8,
					OffsetX: 10,
					OffsetY: 10,
				},
			})
		}
	}

	return f.SaveAs(destPath)
}

func generateSampleBadge(label string, col color.RGBA) []byte {
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

// ScanTargetSheets reads sheet names, H2 text, and shape count from targetPath using Excel COM
func ScanTargetSheets(targetPath string) ([]TargetSheetInfo, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		oleErr, ok := err.(*ole.OleError)
		// S_FALSE or RPC_E_CHANGED_MODE can be safely ignored
		if !ok || (oleErr.Code() != 0x00000001 && oleErr.Code() != 0x80010106) {
			return nil, fmt.Errorf("OLE 초기화 실패: %w", err)
		}
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("Excel.Application")
	if err != nil {
		return nil, fmt.Errorf("Excel Application 실행 실패: %w", err)
	}
	excelApp := unknown.MustQueryInterface(ole.IID_IDispatch)
	defer excelApp.Release()

	oleutil.PutProperty(excelApp, "Visible", false)
	oleutil.PutProperty(excelApp, "DisplayAlerts", false)
	defer oleutil.CallMethod(excelApp, "Quit")

	workbooksVar, err := oleutil.GetProperty(excelApp, "Workbooks")
	if err != nil {
		return nil, fmt.Errorf("Workbooks 객체 획득 실패: %w", err)
	}
	workbooks := workbooksVar.ToIDispatch()
	defer workbooks.Release()

	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		absTargetPath = targetPath
	}

	// Open target file ReadOnly
	wbVar, err := oleutil.CallMethod(workbooks, "Open", absTargetPath, 0, true)
	if err != nil {
		return nil, fmt.Errorf("Target 엑셀 열기 실패 (%s): %w", targetPath, err)
	}
	wb := wbVar.ToIDispatch()
	defer func() {
		oleutil.CallMethod(wb, "Close", false)
		wb.Release()
	}()

	sheetsVar, err := oleutil.GetProperty(wb, "Worksheets")
	if err != nil {
		return nil, fmt.Errorf("Worksheets 획득 실패: %w", err)
	}
	sheets := sheetsVar.ToIDispatch()
	defer sheets.Release()

	countVar, _ := oleutil.GetProperty(sheets, "Count")
	sheetCount := 1
	if countVar != nil && countVar.Value() != nil {
		sheetCount = int(countVar.Value().(int32))
	}

	var results []TargetSheetInfo
	for i := 1; i <= sheetCount; i++ {
		wsVar, err := oleutil.GetProperty(sheets, "Item", i)
		if err != nil || wsVar == nil {
			continue
		}
		ws := wsVar.ToIDispatch()

		nameVar, _ := oleutil.GetProperty(ws, "Name")
		sheetName := fmt.Sprintf("Sheet%d", i)
		if nameVar != nil && nameVar.Value() != nil {
			sheetName = nameVar.ToString()
		}

		info := TargetSheetInfo{
			Index: i,
			Name:  sheetName,
		}

		// Inspect H2 cell and its merge area
		h2Var, err := oleutil.GetProperty(ws, "Range", "H2")
		if err == nil && h2Var != nil {
			h2 := h2Var.ToIDispatch()
			mergeAreaVar, _ := oleutil.GetProperty(h2, "MergeArea")
			var targetArea *ole.IDispatch
			if mergeAreaVar != nil && mergeAreaVar.ToIDispatch() != nil {
				targetArea = mergeAreaVar.ToIDispatch()
			} else {
				targetArea = h2
			}

			// Get address
			addrVar, _ := oleutil.GetProperty(targetArea, "Address")
			if addrVar != nil && addrVar.Value() != nil {
				info.H2MergeArea = addrVar.ToString()
			}

			// Get text from H2 (top-left cell of merge area)
			textVar, _ := oleutil.GetProperty(h2, "Text")
			var fullText string
			if textVar != nil && textVar.Value() != nil {
				fullText = textVar.ToString()
			}
			if fullText == "" {
				valVar, _ := oleutil.GetProperty(h2, "Value2")
				if valVar != nil && valVar.Value() != nil {
					fullText = fmt.Sprintf("%v", valVar.Value())
				}
			}

			fullText = strings.TrimSpace(fullText)
			info.HasH2 = (fullText != "")
			if len([]rune(fullText)) > 60 {
				info.H2Summary = string([]rune(fullText)[:60]) + "..."
			} else {
				info.H2Summary = fullText
			}

			// Count shapes overlapping H2
			mLeft := toFloat64(oleutil.MustGetProperty(targetArea, "Left"))
			mTop := toFloat64(oleutil.MustGetProperty(targetArea, "Top"))
			mWidth := toFloat64(oleutil.MustGetProperty(targetArea, "Width"))
			mHeight := toFloat64(oleutil.MustGetProperty(targetArea, "Height"))

			shapesVar, _ := oleutil.GetProperty(ws, "Shapes")
			if shapesVar != nil {
				shapes := shapesVar.ToIDispatch()
				sCountVar, _ := oleutil.GetProperty(shapes, "Count")
				sCount := 0
				if sCountVar != nil && sCountVar.Value() != nil {
					sCount = int(sCountVar.Value().(int32))
				}

				for sIdx := 1; sIdx <= sCount; sIdx++ {
					shapeVar, _ := oleutil.CallMethod(shapes, "Item", sIdx)
					if shapeVar == nil {
						shapeVar, _ = oleutil.GetProperty(shapes, "Item", sIdx)
					}
					if shapeVar == nil {
						continue
					}
					shape := shapeVar.ToIDispatch()

					sLeft := toFloat64(oleutil.MustGetProperty(shape, "Left"))
					sTop := toFloat64(oleutil.MustGetProperty(shape, "Top"))
					sWidth := toFloat64(oleutil.MustGetProperty(shape, "Width"))
					sHeight := toFloat64(oleutil.MustGetProperty(shape, "Height"))

					if isShapeOverlapping(sLeft, sTop, sWidth, sHeight, mLeft, mTop, mWidth, mHeight) {
						info.ShapeCount++
					}
					shape.Release()
				}
				shapes.Release()
			}

			if targetArea != h2 {
				targetArea.Release()
			}
			h2.Release()
		}

		ws.Release()
		results = append(results, info)
	}

	return results, nil
}

// FindTemplatePlaceholders scans the template worksheet and finds the bottom-most
// occurrence of '대분류', '중분류', '상세내용', '작업방법' to preserve the header
// and identify the data placeholder merged cells
func FindTemplatePlaceholders(ws *ole.IDispatch) (*TemplatePlaceholders, error) {
	usedRangeVar, err := oleutil.GetProperty(ws, "UsedRange")
	if err != nil {
		return nil, fmt.Errorf("UsedRange 접근 실패: %w", err)
	}
	usedRange := usedRangeVar.ToIDispatch()
	defer usedRange.Release()

	rowsVar, _ := oleutil.GetProperty(usedRange, "Rows")
	rowsObj := rowsVar.ToIDispatch()
	totalRows := int(toFloat64(oleutil.MustGetProperty(rowsObj, "Count")))
	startRow := int(toFloat64(oleutil.MustGetProperty(rowsObj, "Row")))
	rowsObj.Release()

	colsVar, _ := oleutil.GetProperty(usedRange, "Columns")
	colsObj := colsVar.ToIDispatch()
	totalCols := int(toFloat64(oleutil.MustGetProperty(colsObj, "Count")))
	startCol := int(toFloat64(oleutil.MustGetProperty(colsObj, "Column")))
	colsObj.Release()

	tp := &TemplatePlaceholders{}

	// We scan row by row (up to 100 rows is typical for template header+data)
	endRow := startRow + totalRows - 1
	endCol := startCol + totalCols - 1

	for r := startRow; r <= endRow; r++ {
		for c := startCol; c <= endCol; c++ {
			cellVar, err := oleutil.GetProperty(ws, "Cells", r, c)
			if err != nil || cellVar == nil {
				continue
			}
			cell := cellVar.ToIDispatch()

			textVar, _ := oleutil.GetProperty(cell, "Text")
			var txt string
			if textVar != nil {
				txt = strings.TrimSpace(textVar.ToString())
			}
			if txt == "" {
				valVar, _ := oleutil.GetProperty(cell, "Value2")
				if valVar != nil && valVar.Value() != nil {
					txt = strings.TrimSpace(fmt.Sprintf("%v", valVar.Value()))
				}
			}

			if txt == "" {
				cell.Release()
				continue
			}

			// Get MergeArea address
			mergeAreaVar, _ := oleutil.GetProperty(cell, "MergeArea")
			var targetArea *ole.IDispatch
			if mergeAreaVar != nil && mergeAreaVar.ToIDispatch() != nil {
				targetArea = mergeAreaVar.ToIDispatch()
			} else {
				targetArea = cell
			}

			addrVar, _ := oleutil.GetProperty(targetArea, "Address")
			addr := ""
			if addrVar != nil && addrVar.Value() != nil {
				addr = addrVar.ToString()
			}

			// We pick the bottom-most (highest r) for each placeholder
			switch txt {
			case "대분류":
				if r >= tp.MajorRow {
					tp.MajorRow = r
					tp.MajorCol = c
					tp.MajorRange = addr
				}
			case "중분류":
				if r >= tp.MinorRow {
					tp.MinorRow = r
					tp.MinorCol = c
					tp.MinorRange = addr
				}
			case "상세내용":
				if r >= tp.DetailRow {
					tp.DetailRow = r
					tp.DetailCol = c
					tp.DetailRange = addr
				}
			case "작업방법":
				if r >= tp.MethodRow {
					tp.MethodRow = r
					tp.MethodCol = c
					tp.MethodRange = addr
				}
			}

			if targetArea != cell {
				targetArea.Release()
			}
			cell.Release()
		}
	}

	// Fallback to standard cells B8/B9, C8/C9, G8/G9, H8/H9 if not discovered
	if tp.MajorRange == "" {
		tp.MajorRange = "$B$9:$B$15"
		tp.MajorRow = 9
		tp.MajorCol = 2
	}
	if tp.MinorRange == "" {
		tp.MinorRange = "$C$9:$F$15"
		tp.MinorRow = 9
		tp.MinorCol = 3
	}
	if tp.DetailRange == "" {
		tp.DetailRange = "$G$9:$G$15"
		tp.DetailRow = 9
		tp.DetailCol = 7
	}
	if tp.MethodRange == "" {
		tp.MethodRange = "$H$9:$K$15"
		tp.MethodRow = 9
		tp.MethodCol = 8
	}

	return tp, nil
}

// GenerateSOPExcel builds a complete SOP Excel document using Excel COM
// - Creates a brand new workbook from template (leaving template untouched)
// - Leaves top header intact and updates the bottom placeholder merged cells
// - Combines checked target sheets' H2 merged text and shapes in specified order into single 작업방법 cell
// - Stacks shapes vertically without overlap
// - Adjusts row heights to fit content
// - Saves as new file in program folder with user-specified name
func GenerateSOPExcel(req SOPGenerateRequest) (*SOPGenerateResult, error) {
	startTime := time.Now()

	appDir := GetAppDir()

	// Normalize destination filename
	dstFilename := strings.TrimSpace(req.DstFilename)
	if dstFilename == "" {
		dstFilename = "SOP_완료.xlsx"
	}
	if !strings.HasSuffix(strings.ToLower(dstFilename), ".xlsx") {
		dstFilename += ".xlsx"
	}
	destFullPath := filepath.Join(appDir, dstFilename)

	// Validate target and template paths
	targetPath := req.TargetPath
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(appDir, targetPath)
	}
	if _, err := os.Stat(targetPath); err != nil {
		return nil, fmt.Errorf("Target 엑셀 파일을 찾을 수 없습니다 (%s): %w", targetPath, err)
	}

	templatePath := req.TemplatePath
	if !filepath.IsAbs(templatePath) {
		templatePath = filepath.Join(appDir, templatePath)
	}
	if _, err := os.Stat(templatePath); err != nil {
		// If template file does not exist, attempt to auto-create default template
		if strings.Contains(templatePath, "default_template.xlsx") {
			_ = CreateDefaultTemplate(templatePath)
		} else {
			return nil, fmt.Errorf("양식(Template) 엑셀 파일을 찾을 수 없습니다 (%s): %w", templatePath, err)
		}
	}

	if len(req.SelectedSheets) == 0 {
		return nil, fmt.Errorf("선택된 작업(시트)이 없습니다. 최소 1개 이상의 시트를 선택하세요.")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		oleErr, ok := err.(*ole.OleError)
		if !ok || (oleErr.Code() != 0x00000001 && oleErr.Code() != 0x80010106) {
			return nil, fmt.Errorf("OLE 초기화 실패: %w", err)
		}
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("Excel.Application")
	if err != nil {
		return nil, fmt.Errorf("Excel Application 실행 실패: %w", err)
	}
	excelApp := unknown.MustQueryInterface(ole.IID_IDispatch)
	defer excelApp.Release()

	oleutil.PutProperty(excelApp, "Visible", false)
	oleutil.PutProperty(excelApp, "DisplayAlerts", false)
	defer oleutil.CallMethod(excelApp, "Quit")

	workbooksVar, err := oleutil.GetProperty(excelApp, "Workbooks")
	if err != nil {
		return nil, fmt.Errorf("Workbooks 객체 획득 실패: %w", err)
	}
	workbooks := workbooksVar.ToIDispatch()
	defer workbooks.Release()

	// 1. Open template in ReadOnly mode
	tmplWbVar, err := oleutil.CallMethod(workbooks, "Open", templatePath, 0, true)
	if err != nil {
		return nil, fmt.Errorf("양식 파일 열기 실패 (%s): %w", templatePath, err)
	}
	tmplWb := tmplWbVar.ToIDispatch()
	defer func() {
		oleutil.CallMethod(tmplWb, "Close", false)
		tmplWb.Release()
	}()

	tmplSheetsVar, err := oleutil.GetProperty(tmplWb, "Worksheets")
	if err != nil {
		return nil, fmt.Errorf("양식 Worksheets 획득 실패: %w", err)
	}
	tmplSheets := tmplSheetsVar.ToIDispatch()
	defer tmplSheets.Release()

	tmplWsVar, err := oleutil.GetProperty(tmplSheets, "Item", 1)
	if err != nil {
		return nil, fmt.Errorf("양식 기본 시트 획득 실패: %w", err)
	}
	tmplWs := tmplWsVar.ToIDispatch()
	defer tmplWs.Release()

	// 2. Clone template sheet to create a new, separate workbook (leaving template 100% untouched)
	oleutil.CallMethod(tmplWs, "Copy")
	time.Sleep(50 * time.Millisecond)

	newWbVar, err := oleutil.GetProperty(excelApp, "ActiveWorkbook")
	if err != nil || newWbVar == nil {
		return nil, fmt.Errorf("신규 통합문서 생성 실패: %v", err)
	}
	dstWb := newWbVar.ToIDispatch()
	defer func() {
		oleutil.CallMethod(dstWb, "Close", false)
		dstWb.Release()
	}()

	dstWsVar, err := oleutil.GetProperty(dstWb, "ActiveSheet")
	if err != nil || dstWsVar == nil {
		return nil, fmt.Errorf("신규 시트 획득 실패: %v", err)
	}
	dstWs := dstWsVar.ToIDispatch()
	defer dstWs.Release()

	// 3. Find placeholders in the new sheet (preserves top header, targets bottom placeholder cells)
	placeholders, err := FindTemplatePlaceholders(dstWs)
	if err != nil {
		return nil, fmt.Errorf("양식 플레이스홀더 분석 실패: %w", err)
	}

	// 4. Update 대분류 (Major Category)
	if placeholders.MajorRange != "" {
		majorRangeVar, _ := oleutil.GetProperty(dstWs, "Range", placeholders.MajorRange)
		if majorRangeVar != nil {
			mr := majorRangeVar.ToIDispatch()
			oleutil.PutProperty(mr, "Value2", req.MajorCategory)
			oleutil.PutProperty(mr, "VerticalAlignment", -4108)   // xlCenter
			oleutil.PutProperty(mr, "HorizontalAlignment", -4108) // xlCenter
			mr.Release()
		}
	}

	// 5. Update 중분류 (Minor Category)
	if placeholders.MinorRange != "" {
		minorRangeVar, _ := oleutil.GetProperty(dstWs, "Range", placeholders.MinorRange)
		if minorRangeVar != nil {
			mr := minorRangeVar.ToIDispatch()
			oleutil.PutProperty(mr, "Value2", req.MinorCategory)
			oleutil.PutProperty(mr, "VerticalAlignment", -4108)   // xlCenter
			oleutil.PutProperty(mr, "HorizontalAlignment", -4108) // xlCenter
			mr.Release()
		}
	}

	// 6. Update 상세내용 (Detail Description)
	if placeholders.DetailRange != "" {
		detailRangeVar, _ := oleutil.GetProperty(dstWs, "Range", placeholders.DetailRange)
		if detailRangeVar != nil {
			dr := detailRangeVar.ToIDispatch()
			oleutil.PutProperty(dr, "Value2", req.DetailText)
			oleutil.PutProperty(dr, "WrapText", true)
			oleutil.PutProperty(dr, "VerticalAlignment", -4160) // xlTop
			dr.Release()
		}
	}

	// 7. Open target workbook in ReadOnly mode
	srcWbVar, err := oleutil.CallMethod(workbooks, "Open", targetPath, 0, true)
	if err != nil {
		return nil, fmt.Errorf("Target 파일 열기 실패 (%s): %w", targetPath, err)
	}
	srcWb := srcWbVar.ToIDispatch()
	defer func() {
		oleutil.CallMethod(srcWb, "Close", false)
		srcWb.Release()
	}()

	// 8. Access Destination 작업방법 (Work Method) merged area
	methodRangeVar, err := oleutil.GetProperty(dstWs, "Range", placeholders.MethodRange)
	if err != nil || methodRangeVar == nil {
		return nil, fmt.Errorf("작업방법 대상 영역 접근 실패 (%s): %v", placeholders.MethodRange, err)
	}
	dstMethodArea := methodRangeVar.ToIDispatch()
	defer dstMethodArea.Release()

	dstMethodLeft := toFloat64(oleutil.MustGetProperty(dstMethodArea, "Left"))
	dstMethodTop := toFloat64(oleutil.MustGetProperty(dstMethodArea, "Top"))
	dstMethodWidth := toFloat64(oleutil.MustGetProperty(dstMethodArea, "Width"))
	dstMethodHeight := toFloat64(oleutil.MustGetProperty(dstMethodArea, "Height"))

	// 9. Combine checked sheets in order: 1, 2, 3...
	var combinedTextParts []string
	cumulativeOffsetY := float64(0)
	handledShapes := make(map[string]bool)

	for idx, sheetName := range req.SelectedSheets {
		orderNum := idx + 1
		sheetClean := strings.TrimSpace(sheetName)
		if sheetClean == "" {
			continue
		}

		wsSrcVar, err := oleutil.GetProperty(srcWb, "Worksheets", sheetClean)
		if err != nil || wsSrcVar == nil {
			continue
		}
		wsSrc := wsSrcVar.ToIDispatch()

		// Read H2 cell and its merge area
		h2Var, err := oleutil.GetProperty(wsSrc, "Range", "H2")
		if err != nil || h2Var == nil {
			wsSrc.Release()
			continue
		}
		srcH2 := h2Var.ToIDispatch()

		mergeAreaVar, _ := oleutil.GetProperty(srcH2, "MergeArea")
		var srcArea *ole.IDispatch
		if mergeAreaVar != nil && mergeAreaVar.ToIDispatch() != nil {
			srcArea = mergeAreaVar.ToIDispatch()
		} else {
			srcArea = srcH2
		}

		// Read text from srcH2 (top-left cell of merge area)
		textVar, _ := oleutil.GetProperty(srcH2, "Text")
		var h2Text string
		if textVar != nil && textVar.Value() != nil {
			h2Text = textVar.ToString()
		}
		if h2Text == "" {
			valVar, _ := oleutil.GetProperty(srcH2, "Value2")
			if valVar != nil && valVar.Value() != nil {
				h2Text = fmt.Sprintf("%v", valVar.Value())
			}
		}

		// Format sequential order block text
		headerTitle := fmt.Sprintf("%d. [%s]", orderNum, sheetClean)
		blockFullText := headerTitle
		if strings.TrimSpace(h2Text) != "" {
			blockFullText += "\n" + strings.TrimSpace(h2Text)
		}
		combinedTextParts = append(combinedTextParts, blockFullText)

		// Geometric bounds of source H2 area
		srcLeft := toFloat64(oleutil.MustGetProperty(srcArea, "Left"))
		srcTop := toFloat64(oleutil.MustGetProperty(srcArea, "Top"))
		srcWidth := toFloat64(oleutil.MustGetProperty(srcArea, "Width"))
		srcHeight := toFloat64(oleutil.MustGetProperty(srcArea, "Height"))

		// Calculate text occupied height estimate
		lines := strings.Split(blockFullText, "\n")
		textEstimatedHeight := float64(len(lines)) * 18.0

		maxShapeBottomRelative := float64(0)
		copiedShapeCount := 0

		// Find shapes anchored to or overlapping source H2 area
		shapesVar, _ := oleutil.GetProperty(wsSrc, "Shapes")
		if shapesVar != nil {
			shapes := shapesVar.ToIDispatch()
			sCountVar, _ := oleutil.GetProperty(shapes, "Count")
			sCount := 0
			if sCountVar != nil && sCountVar.Value() != nil {
				sCount = int(sCountVar.Value().(int32))
			}

			for sIdx := 1; sIdx <= sCount; sIdx++ {
				shapeVar, err := oleutil.CallMethod(shapes, "Item", sIdx)
				if err != nil || shapeVar == nil {
					shapeVar, _ = oleutil.GetProperty(shapes, "Item", sIdx)
				}
				if shapeVar == nil {
					continue
				}
				shape := shapeVar.ToIDispatch()

				nameVar, _ := oleutil.GetProperty(shape, "Name")
				shapeName := fmt.Sprintf("%s_Shape_%d", sheetClean, sIdx)
				if nameVar != nil && nameVar.Value() != nil {
					shapeName = fmt.Sprintf("%s_%s", sheetClean, nameVar.ToString())
				}

				if handledShapes[shapeName] {
					shape.Release()
					continue
				}

				sLeft := toFloat64(oleutil.MustGetProperty(shape, "Left"))
				sTop := toFloat64(oleutil.MustGetProperty(shape, "Top"))
				sWidth := toFloat64(oleutil.MustGetProperty(shape, "Width"))
				sHeight := toFloat64(oleutil.MustGetProperty(shape, "Height"))

				if isShapeOverlapping(sLeft, sTop, sWidth, sHeight, srcLeft, srcTop, srcWidth, srcHeight) {
					// Relative offsets from top-left of H2
					offsetL := sLeft - srcLeft
					offsetT := sTop - srcTop
					if offsetT < 0 {
						offsetT = 0
					}
					if offsetL < 0 {
						offsetL = 0
					}

					// Copy shape to destination sheet with vertical stacking
					oleutil.CallMethod(wsSrc, "Activate")
					oleutil.CallMethod(shape, "Copy")
					time.Sleep(30 * time.Millisecond)

					oleutil.CallMethod(dstWs, "Activate")
					oleutil.CallMethod(dstWs, "Paste")

					// Retrieve pasted shape from destination shapes collection
					dstShapesVar, _ := oleutil.GetProperty(dstWs, "Shapes")
					if dstShapesVar != nil {
						dstShapes := dstShapesVar.ToIDispatch()
						newCountVar, _ := oleutil.GetProperty(dstShapes, "Count")
						if newCountVar != nil && newCountVar.Value() != nil {
							newCount := int(newCountVar.Value().(int32))
							pastedVar, _ := oleutil.CallMethod(dstShapes, "Item", newCount)
							if pastedVar == nil {
								pastedVar, _ = oleutil.GetProperty(dstShapes, "Item", newCount)
							}
							if pastedVar != nil {
								pastedShape := pastedVar.ToIDispatch()

								// Calculate stacked target coordinates
								targetTop := dstMethodTop + cumulativeOffsetY + offsetT + 22.0 // Add title header clearance
								targetLeft := dstMethodLeft + offsetL

								// Ensure shape stays within horizontal bounds if too wide
								if targetLeft+sWidth > dstMethodLeft+dstMethodWidth && dstMethodWidth > 50 {
									targetLeft = dstMethodLeft + 10
								}

								oleutil.PutProperty(pastedShape, "Left", targetLeft)
								oleutil.PutProperty(pastedShape, "Top", targetTop)
								oleutil.PutProperty(pastedShape, "Width", sWidth)
								oleutil.PutProperty(pastedShape, "Height", sHeight)
								pastedShape.Release()
							}
						}
						dstShapes.Release()
					}

					handledShapes[shapeName] = true
					copiedShapeCount++

					bottomRelative := offsetT + sHeight + 22.0
					if bottomRelative > maxShapeBottomRelative {
						maxShapeBottomRelative = bottomRelative
					}
				}

				shape.Release()
			}
			shapes.Release()
		}

		if srcArea != srcH2 {
			srcArea.Release()
		}
		srcH2.Release()
		wsSrc.Release()

		// Calculate block height for vertical stacking
		blockHeight := math.Max(srcHeight, math.Max(textEstimatedHeight, maxShapeBottomRelative))
		if blockHeight < 60 {
			blockHeight = 60
		}
		// Add spacer between sequential sections
		cumulativeOffsetY += blockHeight + 20.0
	}

	// 10. Write combined text into single destination 작업방법 merged cell
	finalCombinedText := strings.Join(combinedTextParts, "\n\n")
	oleutil.PutProperty(dstMethodArea, "Value2", finalCombinedText)
	oleutil.PutProperty(dstMethodArea, "WrapText", true)
	oleutil.PutProperty(dstMethodArea, "VerticalAlignment", -4160) // xlTop

	// 11. Adjust destination row heights if cumulative content exceeds default merged area height
	if cumulativeOffsetY > dstMethodHeight {
		// Expand row heights across the merged rows proportionally
		rowsVar, _ := oleutil.GetProperty(dstMethodArea, "Rows")
		if rowsVar != nil {
			rowsObj := rowsVar.ToIDispatch()
			rowCount := int(toFloat64(oleutil.MustGetProperty(rowsObj, "Count")))
			startRow := int(toFloat64(oleutil.MustGetProperty(rowsObj, "Row")))
			rowsObj.Release()

			if rowCount > 0 {
				extraNeeded := cumulativeOffsetY - dstMethodHeight + 30.0
				addPerRow := extraNeeded / float64(rowCount)
				for r := startRow; r < startRow+rowCount; r++ {
					rowVar, _ := oleutil.GetProperty(dstWs, "Rows", r)
					if rowVar != nil {
						rowObj := rowVar.ToIDispatch()
						curHeight := toFloat64(oleutil.MustGetProperty(rowObj, "RowHeight"))
						oleutil.PutProperty(rowObj, "RowHeight", curHeight+addPerRow)
						rowObj.Release()
					}
				}
			}
		}
	}

	// 12. Save newly created workbook as destination file (in program folder)
	_ = os.Remove(destFullPath) // Remove if previously exists
	oleutil.CallMethod(dstWb, "SaveAs", destFullPath, 51)
	time.Sleep(100 * time.Millisecond)

	stat, err := os.Stat(destFullPath)
	if err != nil {
		return nil, fmt.Errorf("결과 엑셀 파일 저장 확인 실패 (%s): %w", destFullPath, err)
	}

	elapsed := time.Since(startTime).Round(time.Millisecond).String()

	return &SOPGenerateResult{
		Success:        true,
		DstPath:        destFullPath,
		DstFilename:    dstFilename,
		FileSize:       stat.Size(),
		SelectedSheets: req.SelectedSheets,
		ExecutionTime:  elapsed,
		Message:        fmt.Sprintf("SOP 엑셀 생성이 완료되었습니다 (%s, %d개 작업 조합)", dstFilename, len(req.SelectedSheets)),
	}, nil
}

func isShapeOverlapping(sLeft, sTop, sWidth, sHeight, mLeft, mTop, mWidth, mHeight float64) bool {
	if sWidth <= 0 || sHeight <= 0 || mWidth <= 0 || mHeight <= 0 {
		return false
	}
	// Center point inside bounding box
	centerX := sLeft + sWidth/2.0
	centerY := sTop + sHeight/2.0
	if centerX >= mLeft && centerX <= mLeft+mWidth && centerY >= mTop && centerY <= mTop+mHeight {
		return true
	}

	// Area overlap check
	x1 := math.Max(sLeft, mLeft)
	x2 := math.Min(sLeft+sWidth, mLeft+mWidth)
	y1 := math.Max(sTop, mTop)
	y2 := math.Min(sTop+sHeight, mTop+mHeight)
	if x2 > x1 && y2 > y1 {
		overlapArea := (x2 - x1) * (y2 - y1)
		shapeArea := sWidth * sHeight
		if overlapArea >= 0.15*shapeArea || overlapArea >= 0.15*(mWidth*mHeight) {
			return true
		}
	}
	return false
}

func toFloat64(v *ole.VARIANT) float64 {
	if v == nil || v.Value() == nil {
		return 0
	}
	switch val := v.Value().(type) {
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
	default:
		return 0
	}
}
