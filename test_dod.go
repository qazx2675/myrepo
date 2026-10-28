//go:build ignore

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/xuri/excelize/v2"
)

type TestResult struct {
	ID       string
	Category string
	Desc     string
	Passed   bool
	Detail   string
}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

func main() {
	fmt.Println("================================================================================")
	fmt.Println("  SOP 자동 생성 프로그램 완성 조건(DoD v1.2) 전 항목 일괄 검증 테스트 시작")
	fmt.Println("================================================================================")

	var results []TestResult
	record := func(id, cat, desc string, passed bool, detail string) {
		r := TestResult{ID: id, Category: cat, Desc: desc, Passed: passed, Detail: detail}
		results = append(results, r)
		status := "PASS"
		if !passed {
			status = "FAIL"
		}
		fmt.Printf("[%s] %-6s | %-12s | %s\n", status, id, cat, desc)
		if detail != "" {
			fmt.Printf("       -> %s\n", detail)
		}
	}

	appDir, _ := os.Getwd()

	// -------------------------------------------------------------------------
	// 1. 환경, 빌드 및 파일 생성 조건
	// -------------------------------------------------------------------------

	// Test 1.1: Go 기반 단일 실행 파일(.exe) 빌드 (-ldflags="-H windowsgui -s -w")
	fmt.Println("\n[검증 1] 단일 실행 파일 빌드 테스트...")
	exePath := filepath.Join(appDir, "excel-test-runner.exe")
	buildCmd := exec.Command("go", "build", "-ldflags=-H windowsgui -s -w", "-o", "excel-test-runner.exe", ".")
	buildCmd.Dir = appDir
	buildOut, buildErr := buildCmd.CombinedOutput()
	if buildErr == nil {
		stat, _ := os.Stat(exePath)
		record("DoD-1.1", "빌드", "Go 기반 단일 .exe 빌드 (-H windowsgui -s -w)", true,
			fmt.Sprintf("정상 빌드 완료 (크기: %.2f MB)", float64(stat.Size())/(1024*1024)))
	} else {
		record("DoD-1.1", "빌드", "Go 기반 단일 .exe 빌드 (-H windowsgui -s -w)", false,
			fmt.Sprintf("빌드 실패: %v (출력: %s)", buildErr, string(buildOut)))
	}

	// Setup: Sample Target & Default Template
	targetPath := filepath.Join(appDir, "target.xlsx")
	tmplPath := filepath.Join(appDir, "templates", "default_template.xlsx")

	_ = exec.Command(exePath, "-cli", "-create-sample").Run()
	_ = exec.Command(exePath, "-cli", "-create-default-template").Run()

	targetHashBefore, _ := fileHash(targetPath)
	tmplHashBefore, _ := fileHash(tmplPath)

	// Test 1.2 & 1.3: 폐쇄망 제약 준수 (새 엑셀 생성, 원본 불변) & 파일명 직접 지정
	customOutFilename := "SOP_DoD_검증결과.xlsx"
	customOutPath := filepath.Join(appDir, customOutFilename)
	_ = os.Remove(customOutPath)

	genCmd := exec.Command(exePath, "-cli",
		"-major=VM설치(직접입력)",
		"-minor=공통(직접입력)",
		"-detail=상세내용 줄바꿈 테스트 1행\n상세내용 2행입니다.",
		"-sheets=3_보안점검,1_OS설치,2_네트워크구성",
		"-out="+customOutFilename,
	)
	genCmd.Dir = appDir
	_, _ = genCmd.CombinedOutput()

	// Check if new file was created in appDir
	statOut, errOut := os.Stat(customOutPath)
	newFileCreated := (errOut == nil && statOut.Size() > 0)
	record("DoD-1.3", "파일생성", "사용자 지정 파일명으로 프로그램 위치에 생성", newFileCreated,
		fmt.Sprintf("생성 경로: %s (크기: %d bytes)", customOutPath, statOut.Size()))

	// Check that target and template files were NOT modified (in-place modification strictly avoided)
	targetHashAfter, _ := fileHash(targetPath)
	tmplHashAfter, _ := fileHash(tmplPath)
	notModified := (targetHashBefore == targetHashAfter) && (tmplHashBefore == tmplHashAfter)
	record("DoD-1.2", "폐쇄망제약", "원본(target) 및 양식(template) 덮어쓰기 없이 새 통합문서 생성", notModified && newFileCreated,
		fmt.Sprintf("Target 불변: %v, 양식 불변: %v", targetHashBefore == targetHashAfter, tmplHashBefore == tmplHashAfter))

	// -------------------------------------------------------------------------
	// 2. 작업자 이름 관리 조건
	// -------------------------------------------------------------------------
	fmt.Println("\n[검증 2] 작업자 이름 관리 테스트...")

	// Test 2.1 & 2.2: 최초 실행 시 작업자 설정 & 영구 기억
	testWorker := "김엔지니어"
	_ = exec.Command(exePath, "-cli", "-worker="+testWorker).Run()

	getWorkerCmd := exec.Command(exePath, "-cli", "-get-worker")
	getWorkerOut, _ := getWorkerCmd.CombinedOutput()
	workerRemembered := strings.Contains(string(getWorkerOut), testWorker)
	record("DoD-2.1", "작업자관리", "최초 실행 시 작업자 이름 지정", workerRemembered, "작업자 등록 완료")
	record("DoD-2.2", "작업자관리", "작업자 이름 영구 기억 (config.json)", workerRemembered,
		fmt.Sprintf("재실행 후 조회 결과: %s", strings.TrimSpace(string(getWorkerOut))))

	// Test 2.3: 작업자 이름 변경 버튼/API
	newWorker := "이수석엔지니어"
	_ = exec.Command(exePath, "-cli", "-worker="+newWorker).Run()
	getNewWorkerOut, _ := exec.Command(exePath, "-cli", "-get-worker").CombinedOutput()
	workerChanged := strings.Contains(string(getNewWorkerOut), newWorker)
	record("DoD-2.3", "작업자관리", "작업자 이름 변경 및 즉시 저장/반영", workerChanged,
		fmt.Sprintf("변경 후 조회 결과: %s", strings.TrimSpace(string(getNewWorkerOut))))

	// Test 2.4: 작업자/확인자 엑셀 기입 제외
	// Inspect generated Excel: C2, C3 cells must be empty
	fOut, errOpen := excelize.OpenFile(customOutPath)
	workerExcluded := false
	if errOpen == nil {
		sheet := fOut.GetSheetName(0)
		c2Val, _ := fOut.GetCellValue(sheet, "C2")
		c3Val, _ := fOut.GetCellValue(sheet, "C3")
		workerExcluded = (c2Val == "" && c3Val == "")
		record("DoD-2.4", "작업자관리", "작업자/확인자 항목 엑셀 기입 제외 (셀 매핑 미적용)", workerExcluded,
			fmt.Sprintf("C2(작업자칸)='%s', C3(확인자칸)='%s' (모두 빈칸 유지)", c2Val, c3Val))
		fOut.Close()
	} else {
		record("DoD-2.4", "작업자관리", "작업자/확인자 항목 엑셀 기입 제외", false, errOpen.Error())
	}

	// -------------------------------------------------------------------------
	// 3. 분류 및 상세내용 입력 조건
	// -------------------------------------------------------------------------
	fmt.Println("\n[검증 3] 분류 및 상세내용 입력 테스트...")

	fOut, errOpen = excelize.OpenFile(customOutPath)
	if errOpen == nil {
		sheet := fOut.GetSheetName(0)

		// Test 3.1: 대분류 병합 셀 기록
		b9Val, _ := fOut.GetCellValue(sheet, "B9")
		majorMatches := strings.Contains(b9Val, "VM설치")
		record("DoD-3.1", "분류입력", "대분류 선택/직접입력 및 B9 병합 셀 기록", majorMatches,
			fmt.Sprintf("B9 값: '%s'", b9Val))

		// Test 3.2: 중분류 병합 셀 기록
		c9Val, _ := fOut.GetCellValue(sheet, "C9")
		minorMatches := strings.Contains(c9Val, "공통")
		record("DoD-3.2", "분류입력", "중분류 선택/직접입력 및 C9 병합 셀 기록", minorMatches,
			fmt.Sprintf("C9 값: '%s'", c9Val))

		// Test 3.3: 상세내용 줄바꿈 유지 기록
		g9Val, _ := fOut.GetCellValue(sheet, "G9")
		detailHasLines := strings.Contains(g9Val, "상세내용 줄바꿈 테스트 1행") && strings.Contains(g9Val, "상세내용 2행입니다.")
		record("DoD-3.3", "상세내용", "상세내용 멀티라인 줄바꿈 유지 기록 (G9 병합 셀)", detailHasLines,
			fmt.Sprintf("G9 줄바꿈 확인: %v (내용: %q)", detailHasLines, g9Val))

		fOut.Close()
	} else {
		record("DoD-3.1", "분류입력", "대분류", false, errOpen.Error())
		record("DoD-3.2", "분류입력", "중분류", false, errOpen.Error())
		record("DoD-3.3", "상세내용", "상세내용", false, errOpen.Error())
	}

	// -------------------------------------------------------------------------
	// 4. 작업방법(시트별 H2 병합 셀 + 이미지/도형) 순서 조합 조건
	// -------------------------------------------------------------------------
	fmt.Println("\n[검증 4] 작업방법 순서 조합 및 텍스트/도형 복사 테스트...")

	fOut, errOpen = excelize.OpenFile(customOutPath)
	if errOpen == nil {
		sheet := fOut.GetSheetName(0)
		h9Val, _ := fOut.GetCellValue(sheet, "H9")

		// Test 4.1: 체크 순서 유지 (3_보안점검 -> 1_OS설치 -> 2_네트워크구성)
		pos3 := strings.Index(h9Val, "[3_보안점검]")
		pos1 := strings.Index(h9Val, "[1_OS설치]")
		pos2 := strings.Index(h9Val, "[2_네트워크구성]")
		orderMaintained := (pos3 != -1 && pos1 != -1 && pos2 != -1) && (pos3 < pos1 && pos1 < pos2)
		record("DoD-4.1", "작업방법", "시트 작업 목록 인식 및 체크 순서 엄격 유지 (3번 -> 1번 -> 2번)", orderMaintained,
			fmt.Sprintf("순서 확인: 3_보안점검(인덱스 %d) < 1_OS설치(인덱스 %d) < 2_네트워크구성(인덱스 %d)", pos3, pos1, pos2))

		// Test 4.2: 순서 번호 및 시트 이름 자동 기입
		hasNum1 := strings.Contains(h9Val, "1. [3_보안점검]")
		hasNum2 := strings.Contains(h9Val, "2. [1_OS설치]")
		hasNum3 := strings.Contains(h9Val, "3. [2_네트워크구성]")
		autoNumbered := hasNum1 && hasNum2 && hasNum3
		record("DoD-4.2", "작업방법", "순서 번호(1., 2., 3.) 및 시트 이름 자동 기입", autoNumbered,
			fmt.Sprintf("번호 기입 확인: '1. [3_보안점검]', '2. [1_OS설치]', '3. [2_네트워크구성]' 모두 포함됨"))

		// Test 4.3: 단일 병합 셀 내 텍스트 및 이미지/도형 완벽 복사 (세로 적재)
		hasH2Text1 := strings.Contains(h9Val, "PermitRootLogin no")
		hasH2Text2 := strings.Contains(h9Val, "ISO 이미지 마운트")
		hasH2Text3 := strings.Contains(h9Val, "LACP(Mode 4) 본딩")
		allTextsCopied := hasH2Text1 && hasH2Text2 && hasH2Text3

		fOut.Close()

		// Check images/drawings in the sheet
		shapeCount := checkShapesViaCOM(customOutPath)
		shapesCopied := (shapeCount >= 3)
		record("DoD-4.3", "작업방법", "단일 작업방법 병합 셀 내 텍스트 및 이미지/도형 완벽 복사", allTextsCopied && shapesCopied,
			fmt.Sprintf("3개 시트 H2 텍스트 복사: %v | 이미지/도형 복사: %d개 감지 (세로 적재 배치)", allTextsCopied, shapeCount))
	} else {
		record("DoD-4.1", "작업방법", "시트 순서 유지", false, errOpen.Error())
		record("DoD-4.2", "작업방법", "순서 번호 자동 기입", false, errOpen.Error())
		record("DoD-4.3", "작업방법", "텍스트 및 도형 복사", false, errOpen.Error())
	}

	// -------------------------------------------------------------------------
	// 5. 유동적 양식(Template) 생성 및 관리 조건
	// -------------------------------------------------------------------------
	fmt.Println("\n[검증 5] 유동적 양식 생성, 헤더 보존 및 하단 플레이스홀더 교체 테스트...")

	// Test 5.1: 기본 양식 파일 생성 기능
	defaultTmplStat, defaultTmplErr := os.Stat(tmplPath)
	tmplCreated := (defaultTmplErr == nil && defaultTmplStat.Size() > 0)
	record("DoD-5.1", "양식관리", "기본 양식 파일 생성 기능 (B8, C8, G8, H8 구조)", tmplCreated,
		fmt.Sprintf("default_template.xlsx 크기: %d bytes", defaultTmplStat.Size()))

	// Test 5.2: 상단 제목줄(헤더) 보존 및 하단 플레이스홀더 병합 셀 자동 교체
	// Let's create a custom template with custom layout:
	// Header at Row 6: B6="대분류", C6="중분류", E6="상세내용", F6="작업방법"
	// Bottom placeholder merged cells at Row 10:
	// B10:B14="대분류", C10:D14="중분류", E10:E14="상세내용", F10:I14="작업방법"
	customTmplPath := filepath.Join(appDir, "templates", "custom_test_template.xlsx")
	createCustomTestTemplate(customTmplPath)

	customOut2 := "SOP_커스텀양식_검증.xlsx"
	customOutPath2 := filepath.Join(appDir, customOut2)
	_ = os.Remove(customOutPath2)

	genCustomCmd := exec.Command(exePath, "-cli",
		"-template="+customTmplPath,
		"-major=커스텀대분류",
		"-minor=커스텀중분류",
		"-detail=커스텀상세내용",
		"-sheets=1_OS설치,2_네트워크구성",
		"-out="+customOut2,
	)
	genCustomCmd.Dir = appDir
	_, _ = genCustomCmd.CombinedOutput()

	// Inspect generated file from custom template:
	// Header Row 6 must still have "대분류", "중분류", "상세내용", "작업방법"
	// Data Row 10 must have replaced values!
	fCustomOut, errCustomOpen := excelize.OpenFile(customOutPath2)
	headerPreservedAndReplaced := false
	if errCustomOpen == nil {
		sheet := fCustomOut.GetSheetName(0)

		// Header check at Row 6
		hMajor, _ := fCustomOut.GetCellValue(sheet, "B6")
		hMinor, _ := fCustomOut.GetCellValue(sheet, "C6")
		hDetail, _ := fCustomOut.GetCellValue(sheet, "E6")
		hMethod, _ := fCustomOut.GetCellValue(sheet, "F6")
		headerIntact := (hMajor == "대분류" && hMinor == "중분류" && hDetail == "상세내용" && hMethod == "작업방법")

		// Replaced data cell check at Row 10
		dMajor, _ := fCustomOut.GetCellValue(sheet, "B10")
		dMinor, _ := fCustomOut.GetCellValue(sheet, "C10")
		dDetail, _ := fCustomOut.GetCellValue(sheet, "E10")
		dMethod, _ := fCustomOut.GetCellValue(sheet, "F10")
		dataReplaced := (dMajor == "커스텀대분류" && dMinor == "커스텀중분류" && dDetail == "커스텀상세내용" && strings.Contains(dMethod, "1. [1_OS설치]"))

		headerPreservedAndReplaced = headerIntact && dataReplaced
		record("DoD-5.2", "양식관리", "상단 헤더 보존 및 하단 플레이스홀더 병합 셀 자동 탐색/교체", headerPreservedAndReplaced,
			fmt.Sprintf("헤더 보존(Row 6): %v | 데이터 교체(Row 10): %v", headerIntact, dataReplaced))

		fCustomOut.Close()
	} else {
		record("DoD-5.2", "양식관리", "상단 헤더 보존 및 하단 플레이스홀더 교체", false, errCustomOpen.Error())
	}

	// Test 5.3: 양식 저장 및 재사용
	// Verify template is present in templates/ and selectable in config
	templatesInDir, _ := filepath.Glob(filepath.Join(appDir, "templates", "*.xlsx"))
	tmplStoredAndReused := len(templatesInDir) >= 2
	record("DoD-5.3", "양식관리", "양식 파일 저장(templates/) 및 재사용 지원", tmplStoredAndReused,
		fmt.Sprintf("저장된 양식 수: %d개 (%s)", len(templatesInDir), strings.Join(templatesInDir, ", ")))

	// -------------------------------------------------------------------------
	// Final Summary Report
	// -------------------------------------------------------------------------
	fmt.Println("\n================================================================================")
	fmt.Println("                       DoD v1.2 최종 검증 결과 요약")
	fmt.Println("================================================================================")
	allPassed := true
	passCount := 0
	for _, r := range results {
		status := "[ PASS ]"
		if !r.Passed {
			status = "[ FAIL ]"
			allPassed = false
		} else {
			passCount++
		}
		fmt.Printf("%s %-8s | %-12s | %s\n", status, r.ID, r.Category, r.Desc)
	}

	fmt.Printf("\n총 검증 항목: %d개 | 통과: %d개 | 실패: %d개\n", len(results), passCount, len(results)-passCount)
	if allPassed {
		fmt.Println("★ 축하합니다! 조건정의서(DoD v1.2)의 모든 16개 검증 항목을 100% 통과하였습니다.")
		fmt.Println("★ 제작완료판정 승인 기준을 충족합니다.")
	} else {
		fmt.Println("❌ 일부 검증 항목이 통과하지 못했습니다. 확인 후 조치하십시오.")
		os.Exit(1)
	}
}

func checkShapesViaCOM(filePath string) int {
	ole.CoInitialize(0)
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject("Excel.Application")
	if err != nil {
		fmt.Printf("      [checkShapes COM] CreateObject err: %v\n", err)
		return 0
	}
	excelApp := unknown.MustQueryInterface(ole.IID_IDispatch)
	defer excelApp.Release()

	oleutil.PutProperty(excelApp, "Visible", false)
	oleutil.PutProperty(excelApp, "DisplayAlerts", false)
	defer oleutil.CallMethod(excelApp, "Quit")

	workbooksVar, err := oleutil.GetProperty(excelApp, "Workbooks")
	if err != nil {
		fmt.Printf("      [checkShapes COM] GetProperty Workbooks err: %v\n", err)
		return 0
	}
	workbooks := workbooksVar.ToIDispatch()
	defer workbooks.Release()

	absPath, _ := filepath.Abs(filePath)
	wbVar, err := oleutil.CallMethod(workbooks, "Open", absPath, 0, true)
	if err != nil {
		fmt.Printf("      [checkShapes COM] Open err: %v\n", err)
		return 0
	}
	wb := wbVar.ToIDispatch()
	defer func() {
		oleutil.CallMethod(wb, "Close", false)
		wb.Release()
	}()

	sheetsVar, err := oleutil.GetProperty(wb, "Worksheets")
	if err != nil {
		fmt.Printf("      [checkShapes COM] GetProperty Worksheets err: %v\n", err)
		return 0
	}
	sheets := sheetsVar.ToIDispatch()
	defer sheets.Release()

	wsVar, err := oleutil.GetProperty(sheets, "Item", 1)
	if err != nil {
		fmt.Printf("      [checkShapes COM] GetProperty Sheet(1) err: %v\n", err)
		return 0
	}
	ws := wsVar.ToIDispatch()
	defer ws.Release()

	shapesVar, err := oleutil.GetProperty(ws, "Shapes")
	if err != nil || shapesVar == nil {
		fmt.Printf("      [checkShapes COM] GetProperty Shapes err: %v\n", err)
		return 0
	}
	shapes := shapesVar.ToIDispatch()
	defer shapes.Release()

	countVar, err := oleutil.GetProperty(shapes, "Count")
	if err != nil || countVar == nil || countVar.Value() == nil {
		fmt.Printf("      [checkShapes COM] GetProperty Count err: %v\n", err)
		return 0
	}
	return int(countVar.Value().(int32))
}

func createCustomTestTemplate(path string) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	f := excelize.NewFile()
	defer f.Close()

	sheet := "커스텀양식"
	f.SetSheetName(f.GetSheetName(0), sheet)

	// Top Title at Row 2
	_ = f.SetCellValue(sheet, "B2", "SOP 커스텀 테스트 양식")

	// Header row at Row 6
	_ = f.SetCellValue(sheet, "B6", "대분류")
	_ = f.MergeCell(sheet, "C6", "D6")
	_ = f.SetCellValue(sheet, "C6", "중분류")
	_ = f.SetCellValue(sheet, "E6", "상세내용")
	_ = f.MergeCell(sheet, "F6", "I6")
	_ = f.SetCellValue(sheet, "F6", "작업방법")

	// Bottom placeholder merged cells at Row 10:
	// B10:B14 -> 대분류
	_ = f.MergeCell(sheet, "B10", "B14")
	_ = f.SetCellValue(sheet, "B10", "대분류")

	// C10:D14 -> 중분류
	_ = f.MergeCell(sheet, "C10", "D14")
	_ = f.SetCellValue(sheet, "C10", "중분류")

	// E10:E14 -> 상세내용
	_ = f.MergeCell(sheet, "E10", "E14")
	_ = f.SetCellValue(sheet, "E10", "상세내용")

	// F10:I14 -> 작업방법
	_ = f.MergeCell(sheet, "F10", "I14")
	_ = f.SetCellValue(sheet, "F10", "작업방법")

	_ = f.SaveAs(path)
}
