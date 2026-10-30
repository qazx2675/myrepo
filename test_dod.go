//go:build ignore

// SOP v2.0 One-Shot E2E 검증 스크립트. 실행: go run -mod=vendor test_dod.go (Windows + Excel 필요)
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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

const (
	exeName     = "sop-generator.exe"
	resultName  = "DoD_결과.xlsx"
	testWorker  = "테스트작업자"
	testVerif   = "테스트검증자"
	testObject  = "테스트대상서버"
	baseSheet   = "3_보안점검"
	secondSheet = "1_OS설치"
)

var sectionNames = []string{"사전작업", "본작업", "사후작업"}
var stepOf = map[string]string{"사전작업": "1", "본작업": "2", "사후작업": "3"}

// 헤더 키 (공백 제거 후 소문자)
const (
	hStep   = "step"
	hTask   = "작업내용"
	hPre    = "작업자에의한사전작업내용"
	hTarget = "작업대상"
	hVerify = "검증방법"
	hWorker = "작업자"
	hVerif  = "검증자"
	hResult = "작업결과"
)

var allHeaders = []string{hStep, hTask, hPre, hTarget, hVerify, hWorker, hVerif, hResult}
var stackHeaders = []string{hTask, hPre, hVerify}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// ---------------------------------------------------------------------------
// excelize 공용 스캐너 (고정 좌표 없음)
// ---------------------------------------------------------------------------

type rect struct{ c1, r1, c2, r2 int }

func (r rect) contains(col, row int) bool {
	return col >= r.c1 && col <= r.c2 && row >= r.r1 && row <= r.r2
}

func cellName(col, row int) string {
	n, _ := excelize.CoordinatesToCellName(col, row)
	return n
}

func (r rect) String() string {
	return cellName(r.c1, r.r1) + ":" + cellName(r.c2, r.r2)
}

// strip: 모든 공백 제거
func strip(s string) string {
	return strings.Join(strings.Fields(s), "")
}

type section struct {
	Name      string
	TitleRow  int
	TitleCol  int
	HeaderRow int
	DataRow   int
	Cols      map[string]int  // 헤더키 -> 열(1-based)
	Blocks    map[string]rect // 헤더키 -> 데이터 블록(병합 영역 또는 단일 셀)
}

type sheetScan struct {
	Name     string
	Sections map[string]*section
	Merges   []rect
}

func parseMerges(f *excelize.File, sheet string) []rect {
	var out []rect
	ms, err := f.GetMergeCells(sheet)
	if err != nil {
		return out
	}
	for _, m := range ms {
		c1, r1, e1 := excelize.CellNameToCoordinates(m.GetStartAxis())
		c2, r2, e2 := excelize.CellNameToCoordinates(m.GetEndAxis())
		if e1 == nil && e2 == nil {
			out = append(out, rect{c1, r1, c2, r2})
		}
	}
	return out
}

func blockAt(merges []rect, col, row int) rect {
	for _, m := range merges {
		if m.c1 == col && m.r1 == row {
			return m
		}
	}
	for _, m := range merges {
		if m.contains(col, row) {
			return m
		}
	}
	return rect{col, row, col, row}
}

func scanSheet(f *excelize.File, sheet string) (*sheetScan, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, err
	}
	ss := &sheetScan{Name: sheet, Sections: map[string]*section{}, Merges: parseMerges(f, sheet)}
	// 섹션 제목: 공백 제거 후 정확히 일치하는 첫 셀
	for ri, row := range rows {
		for ci, v := range row {
			s := strip(v)
			for _, nm := range sectionNames {
				if s == nm {
					if _, ok := ss.Sections[nm]; !ok {
						ss.Sections[nm] = &section{Name: nm, TitleRow: ri + 1, TitleCol: ci + 1}
					}
				}
			}
		}
	}
	for _, sec := range ss.Sections {
		// 제목 아래(최대 6행)에서 Step + 작업내용 이 있는 헤더 행 탐색
		for ri := sec.TitleRow; ri < sec.TitleRow+6 && ri < len(rows); ri++ {
			cols := map[string]int{}
			for ci, v := range rows[ri] {
				k := strings.ToLower(strip(v))
				for _, h := range allHeaders {
					if k == h {
						if _, dup := cols[h]; !dup {
							cols[h] = ci + 1
						}
					}
				}
			}
			if cols[hStep] > 0 && cols[hTask] > 0 {
				sec.HeaderRow = ri + 1
				sec.DataRow = ri + 2
				sec.Cols = cols
				sec.Blocks = map[string]rect{}
				for h, c := range cols {
					sec.Blocks[h] = blockAt(ss.Merges, c, sec.DataRow)
				}
				break
			}
		}
	}
	return ss, nil
}

func (ss *sheetScan) complete() (bool, string) {
	for _, nm := range sectionNames {
		s := ss.Sections[nm]
		if s == nil || s.HeaderRow == 0 {
			return false, fmt.Sprintf("%s: 섹션/헤더 미발견(%s)", ss.Name, nm)
		}
		for _, h := range allHeaders {
			if s.Cols[h] == 0 {
				return false, fmt.Sprintf("%s/%s: 헤더 '%s' 미발견", ss.Name, nm, h)
			}
		}
	}
	return true, ""
}

func cellText(f *excelize.File, sheet string, col, row int) string {
	v, _ := f.GetCellValue(sheet, cellName(col, row))
	return v
}

func fontSizeOf(f *excelize.File, sheet, cell string) float64 {
	id, err := f.GetCellStyle(sheet, cell)
	if err != nil {
		return 0
	}
	st, err := f.GetStyle(id)
	if err != nil || st == nil || st.Font == nil {
		return 0
	}
	return st.Font.Size
}

// 리치텍스트 런 중 크기가 지정된 것이 있으면 모두 want 인지 검사 (없으면 true)
func richSizesOK(f *excelize.File, sheet, cell string, want float64) bool {
	runs, err := f.GetCellRichText(sheet, cell)
	if err != nil {
		return true
	}
	for _, r := range runs {
		if r.Font != nil && r.Font.Size != 0 && r.Font.Size != want {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// COM 헬퍼
// ---------------------------------------------------------------------------

func num(v *ole.VARIANT) float64 {
	if v == nil {
		return 0
	}
	switch x := v.Value().(type) {
	case int8:
		return float64(x)
	case int16:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case uint8:
		return float64(x)
	case uint16:
		return float64(x)
	case uint32:
		return float64(x)
	case float32:
		return float64(x)
	case float64:
		return x
	}
	return 0
}

type shapeInfo struct {
	Row, Col                 int
	Left, Top, Width, Height float64
}

func comWorksheet(wb *ole.IDispatch, name string) (*ole.IDispatch, error) {
	v, err := oleutil.GetProperty(wb, "Worksheets")
	if err != nil {
		return nil, err
	}
	sheets := v.ToIDispatch()
	defer sheets.Release()
	w, err := oleutil.GetProperty(sheets, "Item", name)
	if err != nil {
		return nil, err
	}
	return w.ToIDispatch(), nil
}

func comShapes(ws *ole.IDispatch) ([]shapeInfo, error) {
	sv, err := oleutil.GetProperty(ws, "Shapes")
	if err != nil {
		return nil, err
	}
	shapes := sv.ToIDispatch()
	defer shapes.Release()
	cv, err := oleutil.GetProperty(shapes, "Count")
	if err != nil {
		return nil, err
	}
	n := int(num(cv))
	var out []shapeInfo
	for i := 1; i <= n; i++ {
		iv, err := oleutil.CallMethod(shapes, "Item", i)
		if err != nil {
			return nil, err
		}
		sh := iv.ToIDispatch()
		var si shapeInfo
		if tl, err := oleutil.GetProperty(sh, "TopLeftCell"); err == nil {
			d := tl.ToIDispatch()
			if r, err := oleutil.GetProperty(d, "Row"); err == nil {
				si.Row = int(num(r))
			}
			if c, err := oleutil.GetProperty(d, "Column"); err == nil {
				si.Col = int(num(c))
			}
			d.Release()
		}
		for _, p := range []struct {
			n string
			d *float64
		}{{"Left", &si.Left}, {"Top", &si.Top}, {"Width", &si.Width}, {"Height", &si.Height}} {
			if v, err := oleutil.GetProperty(sh, p.n); err == nil {
				*p.d = num(v)
			}
		}
		sh.Release()
		out = append(out, si)
	}
	return out, nil
}

// 범위 r 의 (Left, Top, Width, Height) — 병합 영역은 MergeArea 로 보정
func comRangeGeom(ws *ole.IDispatch, r rect) (l, t, w, h float64, err error) {
	rv, err := oleutil.GetProperty(ws, "Range", cellName(r.c1, r.r1))
	if err != nil {
		return
	}
	rng := rv.ToIDispatch()
	defer rng.Release()
	mv, err := oleutil.GetProperty(rng, "MergeArea")
	if err != nil {
		return
	}
	ma := mv.ToIDispatch()
	defer ma.Release()
	vals := map[string]float64{}
	for _, p := range []string{"Left", "Top", "Width", "Height"} {
		v, e := oleutil.GetProperty(ma, p)
		if e != nil {
			err = e
			return
		}
		vals[p] = num(v)
	}
	return vals["Left"], vals["Top"], vals["Width"], vals["Height"], nil
}

func comRowHeightSum(ws *ole.IDispatch, r rect) float64 {
	sum := 0.0
	for row := r.r1; row <= r.r2; row++ {
		rv, err := oleutil.GetProperty(ws, "Range", cellName(1, row))
		if err != nil {
			continue
		}
		rng := rv.ToIDispatch()
		if hv, err := oleutil.GetProperty(rng, "RowHeight"); err == nil {
			sum += num(hv)
		}
		rng.Release()
	}
	return sum
}

type comCheck struct {
	ID     string
	Cat    string
	Desc   string
	Passed bool
	Detail string
}

// COM 검증 일괄 수행. 반환: 검사 결과 목록, 원본 시트 이미지 수 합계(선택 시트 기준), 시트별 전체 도형 수
func runCOMChecks(resultPath, targetPath string, selected []string, srcScans map[string]*sheetScan, resScan *sheetScan, resSheet string) (checks []comCheck, srcShapesAll int, err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// S_FALSE(*ole.OleError code 1)는 CoUninitialize 필요, RPC_E_CHANGED_MODE는 불필요
	needUninit := true
	if e := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); e != nil {
		oe, ok := e.(*ole.OleError)
		switch {
		case ok && oe.Code() == 0x00000001:
		case ok && oe.Code() == 0x80010106:
			needUninit = false
		default:
			return nil, 0, fmt.Errorf("CoInitializeEx: %v", e)
		}
	}
	if needUninit {
		defer ole.CoUninitialize()
	}

	unknown, e := oleutil.CreateObject("Excel.Application")
	if e != nil {
		return nil, 0, fmt.Errorf("CreateObject: %v", e)
	}
	app := unknown.MustQueryInterface(ole.IID_IDispatch)
	defer app.Release()
	oleutil.PutProperty(app, "Visible", false)
	oleutil.PutProperty(app, "DisplayAlerts", false)
	defer oleutil.CallMethod(app, "Quit")

	wbsV, e := oleutil.GetProperty(app, "Workbooks")
	if e != nil {
		return nil, 0, e
	}
	wbs := wbsV.ToIDispatch()
	defer wbs.Release()

	openRO := func(p string) (*ole.IDispatch, error) {
		abs, _ := filepath.Abs(p)
		v, e := oleutil.CallMethod(wbs, "Open", abs, 0, true)
		if e != nil {
			return nil, e
		}
		return v.ToIDispatch(), nil
	}

	// --- 원본: 선택 시트의 3개 적재 열 데이터 블록 안의 도형 수 ---
	tgtWB, e := openRO(targetPath)
	if e != nil {
		return nil, 0, fmt.Errorf("target Open: %v", e)
	}
	srcInBlocks := 0
	var srcDetail []string
	for _, sn := range selected {
		ws, e := comWorksheet(tgtWB, sn)
		if e != nil {
			return nil, 0, fmt.Errorf("target sheet %s: %v", sn, e)
		}
		shapes, e := comShapes(ws)
		ws.Release()
		if e != nil {
			return nil, 0, e
		}
		srcShapesAll += len(shapes)
		cnt := 0
		for _, secName := range sectionNames {
			sec := srcScans[sn].Sections[secName]
			if sec == nil {
				continue
			}
			for _, h := range stackHeaders {
				b := sec.Blocks[h]
				for _, s := range shapes {
					if b.contains(s.Col, s.Row) {
						cnt++
					}
				}
			}
		}
		srcInBlocks += cnt
		srcDetail = append(srcDetail, fmt.Sprintf("%s=%d", sn, cnt))
	}
	oleutil.CallMethod(tgtWB, "Close", false)
	tgtWB.Release()

	// --- 결과 ---
	resWB, e := openRO(resultPath)
	if e != nil {
		return nil, 0, fmt.Errorf("result Open: %v", e)
	}
	defer func() {
		oleutil.CallMethod(resWB, "Close", false)
		resWB.Release()
	}()
	ws, e := comWorksheet(resWB, resSheet)
	if e != nil {
		return nil, 0, fmt.Errorf("result sheet: %v", e)
	}
	defer ws.Release()
	shapes, e := comShapes(ws)
	if e != nil {
		return nil, 0, e
	}

	checks = append(checks, comCheck{"DoD-5.4", "이미지/도형",
		"결과 시트 도형 수 == 선택 시트 3개 적재열 데이터 블록 내 도형 수 합",
		len(shapes) == srcInBlocks && srcInBlocks > 0,
		fmt.Sprintf("결과=%d, 원본 블록 합=%d (%s)", len(shapes), srcInBlocks, strings.Join(srcDetail, ", "))})

	const eps = 1.0
	horizOK, overlapOK, belowOK, heightOK := true, true, true, true
	var dH, dO, dB, dG []string
	for _, secName := range sectionNames {
		sec := resScan.Sections[secName]
		if sec == nil {
			continue
		}
		secMaxBottom, secTop := 0.0, 0.0
		var secRect rect
		for _, h := range stackHeaders {
			b := sec.Blocks[h]
			secRect = b
			ml, mt, mw, _, ge := comRangeGeom(ws, b)
			if ge != nil {
				horizOK = false
				dH = append(dH, fmt.Sprintf("%s/%s geom err %v", secName, h, ge))
				continue
			}
			secTop = mt
			var in []shapeInfo
			for _, s := range shapes {
				if b.contains(s.Col, s.Row) {
					in = append(in, s)
				}
			}
			sort.Slice(in, func(i, j int) bool { return in[i].Top < in[j].Top })
			for i, s := range in {
				if s.Left < ml-eps || s.Left+s.Width > ml+mw+eps {
					horizOK = false
					dH = append(dH, fmt.Sprintf("%s/%s #%d L=%.1f W=%.1f (merge L=%.1f W=%.1f)", secName, h, i+1, s.Left, s.Width, ml, mw))
				}
				if s.Top <= mt {
					belowOK = false
					dB = append(dB, fmt.Sprintf("%s/%s #%d Top=%.1f <= merge.Top=%.1f", secName, h, i+1, s.Top, mt))
				}
				if i > 0 {
					p := in[i-1]
					if s.Top < p.Top+p.Height-0.5 {
						overlapOK = false
						dO = append(dO, fmt.Sprintf("%s/%s #%d/#%d 겹침", secName, h, i, i+1))
					}
				}
				if bt := s.Top + s.Height; bt > secMaxBottom {
					secMaxBottom = bt
				}
			}
		}
		if secMaxBottom > 0 {
			need := secMaxBottom - secTop
			have := comRowHeightSum(ws, secRect)
			if have+eps < need {
				heightOK = false
				dG = append(dG, fmt.Sprintf("%s 행높이합 %.1f < 필요 %.1f", secName, have, need))
			}
			dG = append(dG, fmt.Sprintf("%s: 행높이합 %.1f / 필요 %.1f", secName, have, need))
		}
	}
	checks = append(checks,
		comCheck{"DoD-5.5", "이미지/도형", "모든 도형이 자기 병합 셀 영역 내(가로)에 위치", horizOK, strings.Join(dH, "; ")},
		comCheck{"DoD-5.6", "이미지/도형", "열별 도형 간 세로 비겹침", overlapOK, strings.Join(dO, "; ")},
		comCheck{"DoD-5.7", "이미지/도형", "도형이 열 헤더 텍스트 시작점(병합 Top) 아래에 위치", belowOK, strings.Join(dB, "; ")},
		comCheck{"DoD-5.8", "행높이", "데이터 블록 행 높이 합 >= 최하단 도형 bottom - 병합 top", heightOK, strings.Join(dG, "; ")})
	return checks, srcShapesAll, nil
}

// ---------------------------------------------------------------------------

func main() {
	fmt.Println("================================================================================")
	fmt.Println("  SOP 자동 생성 프로그램 완성 조건(DoD v2.0) One-Shot E2E 검증 시작")
	fmt.Println("================================================================================")

	var results []TestResult
	record := func(id, cat, desc string, passed bool, detail string) {
		results = append(results, TestResult{id, cat, desc, passed, detail})
		status := "PASS"
		if !passed {
			status = "FAIL"
		}
		fmt.Printf("[%s] %-8s | %-12s | %s\n", status, id, cat, desc)
		if detail != "" {
			fmt.Printf("       -> %s\n", detail)
		}
	}

	appDir, _ := os.Getwd()
	exePath := filepath.Join(appDir, exeName)
	targetPath := filepath.Join(appDir, "target.xlsx")
	resultPath := filepath.Join(appDir, resultName)
	cfgPath := filepath.Join(appDir, "config.json")

	// config.json 백업 (종료 시 복원)
	origCfg, cfgErr := os.ReadFile(cfgPath)
	restoreCfg := func() {
		if cfgErr == nil {
			_ = os.WriteFile(cfgPath, origCfg, 0644)
		} else {
			_ = os.Remove(cfgPath)
		}
	}
	finish := func() {
		restoreCfg()
		fmt.Println("\n================================================================================")
		fmt.Println("                       DoD v2.0 최종 검증 결과 요약")
		fmt.Println("================================================================================")
		pass := 0
		for _, r := range results {
			st := "[ PASS ]"
			if r.Passed {
				pass++
			} else {
				st = "[ FAIL ]"
			}
			fmt.Printf("%s %-8s | %-12s | %s\n", st, r.ID, r.Category, r.Desc)
		}
		fmt.Printf("\n총 검증 항목: %d개 | 통과: %d개 | 실패: %d개\n", len(results), pass, len(results)-pass)
		if pass == len(results) {
			fmt.Println("DoD v2.0 모든 항목 One-Shot 통과. 제작완료판정 기준 충족.")
			fmt.Printf("결과 파일(육안 확인용): %s\n", resultPath)
			return
		}
		fmt.Println("일부 검증 항목이 통과하지 못했습니다. 확인 후 조치하십시오.")
		os.Exit(1)
	}

	// ---- 1. 빌드 ----
	fmt.Println("\n[검증 1] 단일 실행 파일 빌드...")
	_ = os.Remove(exePath)
	bc := exec.Command("go", "build", "-mod=vendor", "-ldflags=-H windowsgui -s -w", "-o", exeName, ".")
	bc.Dir = appDir
	bout, berr := bc.CombinedOutput()
	st, serr := os.Stat(exePath)
	buildOK := berr == nil && serr == nil && st.Size() > 0
	if buildOK {
		record("DoD-1.1", "빌드", "sop-generator.exe 단일 실행 파일 빌드 (-H windowsgui -s -w)", true,
			fmt.Sprintf("크기 %.2f MB", float64(st.Size())/(1024*1024)))
	} else {
		record("DoD-1.1", "빌드", "sop-generator.exe 단일 실행 파일 빌드 (-H windowsgui -s -w)", false,
			fmt.Sprintf("err=%v out=%s", berr, string(bout)))
		finish()
		return
	}

	// ---- 2. 샘플 생성 ----
	fmt.Println("\n[검증 2] 샘플 target 생성 및 전제조건 확인...")
	_ = os.Remove(targetPath)
	cs := exec.Command(exePath, "-cli", "-create-sample")
	cs.Dir = appDir
	csOut, csErr := cs.CombinedOutput()
	if st, e := os.Stat(targetPath); csErr != nil || e != nil || st.Size() == 0 {
		record("DoD-0.1", "원샷", "샘플 target.xlsx 생성(-cli -create-sample)", false, fmt.Sprintf("err=%v out=%s", csErr, string(csOut)))
		finish()
		return
	}

	selected := []string{baseSheet, secondSheet}
	srcScans := map[string]*sheetScan{}
	var tf *excelize.File
	{
		var e error
		tf, e = excelize.OpenFile(targetPath)
		if e != nil {
			record("DoD-0.1", "원샷", "샘플 target.xlsx 열기", false, e.Error())
			finish()
			return
		}
	}
	list := tf.GetSheetList()
	preOK := len(list) >= 2
	var preDetail []string
	preDetail = append(preDetail, fmt.Sprintf("시트 %d개 %v", len(list), list))
	posSig := map[string]bool{}
	font14 := false
	picCount := 0
	for _, sn := range list {
		ss, e := scanSheet(tf, sn)
		if e != nil {
			preOK = false
			preDetail = append(preDetail, sn+": "+e.Error())
			continue
		}
		srcScans[sn] = ss
		if ok, why := ss.complete(); !ok {
			preOK = false
			preDetail = append(preDetail, why)
			continue
		}
		var sig []string
		for _, nm := range sectionNames {
			sec := ss.Sections[nm]
			sig = append(sig, fmt.Sprint(sec.TitleRow))
			for _, h := range stackHeaders {
				b := sec.Blocks[h]
				if fontSizeOf(tf, sn, cellName(b.c1, b.r1)) == 14 {
					font14 = true
				}
				if pics, e := tf.GetPictures(sn, cellName(b.c1, b.r1)); e == nil {
					picCount += len(pics)
				}
			}
		}
		posSig[strings.Join(sig, ",")] = true
	}
	for _, sn := range selected {
		if srcScans[sn] == nil {
			preOK = false
			preDetail = append(preDetail, "필수 시트 없음: "+sn)
		}
	}
	if len(posSig) < 2 {
		preOK = false
	}
	if !font14 {
		preOK = false
	}
	preDetail = append(preDetail, fmt.Sprintf("섹션 행위치 서로 다른 시트 패턴 %d종, 14pt 데이터셀=%v, 데이터셀 GetPictures=%d", len(posSig), font14, picCount))
	record("DoD-0.1", "원샷", "샘플 전제조건(>=2시트, 3섹션, 시트별 상이 행위치, 14pt)", preOK, strings.Join(preDetail, " | "))
	tf.Close()

	// ---- 3. 생성 실행 (단 1회) ----
	fmt.Println("\n[검증 3] 결과 엑셀 생성 (단일 실행)...")
	targetHashBefore, _ := fileHash(targetPath)
	_ = os.Remove(resultPath)
	gen := exec.Command(exePath, "-cli", "-target=target.xlsx",
		"-sheets="+strings.Join(selected, ","),
		"-out="+resultName,
		"-worker="+testWorker, "-verifier="+testVerif, "-object="+testObject)
	gen.Dir = appDir
	genOut, genErr := gen.CombinedOutput()
	rst, rerr := os.Stat(resultPath)
	created := genErr == nil && rerr == nil && rst.Size() > 0
	detail := ""
	if rerr == nil {
		detail = fmt.Sprintf("%s (%d bytes)", resultPath, rst.Size())
	} else {
		detail = fmt.Sprintf("exit err=%v out=%s", genErr, string(genOut))
	}
	record("DoD-1.2", "파일생성", "지정한 이름으로 exe 위치에 결과 엑셀 생성 (종료코드 0)", created, detail)
	targetHashAfter, _ := fileHash(targetPath)
	record("DoD-1.3", "폐쇄망제약", "원본 target.xlsx 불변 (덮어쓰기 없음)", targetHashBefore != "" && targetHashBefore == targetHashAfter,
		fmt.Sprintf("sha256 동일: %v", targetHashBefore == targetHashAfter))

	// ---- 4. 정적 검증 ----
	fmt.Println("\n[검증 4] 레거시 제거 / 설정 / UI 정적 검증...")
	var srcFiles []string
	for _, p := range []string{"config.go", "handlers.go", "excel_com.go", "main.go"} {
		srcFiles = append(srcFiles, filepath.Join(appDir, p))
	}
	_ = filepath.Walk(filepath.Join(appDir, "web"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			srcFiles = append(srcFiles, p)
		}
		return nil
	})
	banned := []string{"대분류", "중분류", "상세내용", "MajorCategor", "MinorCategor", "default_template", "/api/templates", "/api/config/category"}
	var hits []string
	for _, p := range srcFiles {
		b, e := os.ReadFile(p)
		if e != nil {
			hits = append(hits, filepath.Base(p)+": 읽기 실패")
			continue
		}
		for _, w := range banned {
			if strings.Contains(string(b), w) {
				hits = append(hits, fmt.Sprintf("%s ∋ %q", filepath.Base(p), w))
			}
		}
	}
	record("DoD-2.1", "레거시제거", "대/중분류·상세내용·기본양식 관련 코드/문자열 완전 삭제 (config/handlers/excel_com/main/web)", len(hits) == 0,
		fmt.Sprintf("검사 파일 %d개, 잔존 %d건 %s", len(srcFiles), len(hits), strings.Join(hits, "; ")))
	_, tdErr := os.Stat(filepath.Join(appDir, "templates"))
	record("DoD-2.2", "레거시제거", "templates/ 폴더 부재", os.IsNotExist(tdErr), "")

	cfgBytes, e := os.ReadFile(cfgPath)
	cfgOK := false
	cfgDetail := ""
	if e == nil {
		var m map[string]interface{}
		if je := json.Unmarshal(cfgBytes, &m); je == nil {
			_, hv := m["verifier_name"]
			_, ho := m["target_object"]
			cfgOK = m["worker_name"] == testWorker && hv && ho
			cfgDetail = fmt.Sprintf("worker_name=%v, verifier_name키=%v, target_object키=%v", m["worker_name"], hv, ho)
		} else {
			cfgDetail = je.Error()
		}
	} else {
		cfgDetail = e.Error()
	}
	record("DoD-3.1", "작업자관리", "config.json 에 worker_name 영구 저장 + verifier_name/target_object 키 존재", cfgOK, cfgDetail)

	html, _ := os.ReadFile(filepath.Join(appDir, "web", "index.html"))
	hs := string(html)
	record("DoD-3.2", "입력UI", `web/index.html 에 id="targetObject", id="verifierName" 존재`,
		strings.Contains(hs, `id="targetObject"`) && strings.Contains(hs, `id="verifierName"`),
		fmt.Sprintf("targetObject=%v verifierName=%v", strings.Contains(hs, `id="targetObject"`), strings.Contains(hs, `id="verifierName"`)))

	// ---- 5. 결과 엑셀 구조 검증 (excelize) ----
	fmt.Println("\n[검증 5] 결과 엑셀 구조/내용 검증...")
	var resScan *sheetScan
	resSheet := ""
	var rf *excelize.File
	if created {
		var oe error
		rf, oe = excelize.OpenFile(resultPath)
		if oe != nil {
			record("DoD-4.0", "섹션탐색", "결과 파일 열기", false, oe.Error())
		} else {
			defer rf.Close()
			resSheet = rf.GetSheetName(0)
			resScan, _ = scanSheet(rf, resSheet)
		}
	}
	if resScan == nil {
		record("DoD-4.1", "섹션탐색", "결과 시트에서 3개 섹션/8개 헤더 동적 탐색", false, "결과 파일 없음/열기 실패")
		finish()
		return
	}
	ok, why := resScan.complete()
	record("DoD-4.1", "섹션탐색", "결과 시트에서 3개 섹션/8개 헤더 동적 탐색(고정좌표 없음)", ok, why)
	if !ok {
		finish()
		return
	}
	base := srcScans[baseSheet]

	// Step
	var stepD []string
	stepOK := true
	for _, nm := range sectionNames {
		sec := resScan.Sections[nm]
		v := strip(cellText(rf, resSheet, sec.Blocks[hStep].c1, sec.Blocks[hStep].r1))
		if v != stepOf[nm] {
			stepOK = false
		}
		stepD = append(stepD, fmt.Sprintf("%s=%q", nm, v))
	}
	record("DoD-4.2", "Step", "Step 값 사전작업=1, 본작업=2, 사후작업=3", stepOK, strings.Join(stepD, ", "))

	// 작업대상/작업자/검증자
	inOK := true
	var inD []string
	for _, nm := range sectionNames {
		sec := resScan.Sections[nm]
		for _, p := range []struct{ h, want string }{{hTarget, testObject}, {hWorker, testWorker}, {hVerif, testVerif}} {
			b := sec.Blocks[p.h]
			v := strings.TrimSpace(cellText(rf, resSheet, b.c1, b.r1))
			if v != p.want {
				inOK = false
				inD = append(inD, fmt.Sprintf("%s/%s=%q", nm, p.h, v))
			}
		}
	}
	record("DoD-3.3", "엑셀기입", "작업대상/작업자/검증자가 3개 섹션 모두에 정확히 기입", inOK, strings.Join(inD, "; "))

	// 순서 적재
	findHdr := func(s string, n int, name string) int {
		a := strings.Index(s, fmt.Sprintf("%d.[%s]", n, name))
		b := strings.Index(s, fmt.Sprintf("%d.%s", n, name))
		if a >= 0 && (b < 0 || a < b) {
			return a
		}
		return b
	}
	ordOK, contOK := true, true
	var ordD, contD []string
	sf, _ := excelize.OpenFile(targetPath)
	for _, nm := range sectionNames {
		for _, h := range stackHeaders {
			rb := resScan.Sections[nm].Blocks[h]
			rv := strip(cellText(rf, resSheet, rb.c1, rb.r1))
			i1 := findHdr(rv, 1, baseSheet)
			i2 := findHdr(rv, 2, secondSheet)
			if i1 < 0 || i2 < 0 || i1 >= i2 {
				ordOK = false
				ordD = append(ordD, fmt.Sprintf("%s/%s 헤더 idx(1.%s)=%d idx(2.%s)=%d", nm, h, baseSheet, i1, secondSheet, i2))
			}
			p1, p2 := -1, -1
			for k, sn := range selected {
				sb := srcScans[sn].Sections[nm].Blocks[h]
				st := strip(cellText(sf, sn, sb.c1, sb.r1))
				if st == "" {
					contD = append(contD, fmt.Sprintf("%s/%s/%s 원본 빈 텍스트(건너뜀)", sn, nm, h))
					continue
				}
				idx := strings.Index(rv, st)
				if idx < 0 {
					contOK = false
					contD = append(contD, fmt.Sprintf("%s/%s/%s 원본 텍스트 누락", sn, nm, h))
				}
				if k == 0 {
					p1 = idx
				} else {
					p2 = idx
				}
			}
			if p1 >= 0 && p2 >= 0 && p1 >= p2 {
				contOK = false
				contD = append(contD, fmt.Sprintf("%s/%s 본문 순서 불일치", nm, h))
			}
		}
	}
	sf.Close()
	record("DoD-5.1", "시트순서", "각 섹션 3개 열에 '1. [3_보안점검]' -> '2. [1_OS설치]' 헤더 순서 적재", ordOK, strings.Join(ordD, "; "))
	record("DoD-5.2", "시트순서", "각 섹션 3개 열에 원본 시트 텍스트가 선택 순서대로 포함", contOK, strings.Join(contD, "; "))

	// 병합 구조 / 열 너비 / 글꼴
	mergeOK := true
	var mergeD []string
	for _, nm := range sectionNames {
		for _, h := range allHeaders {
			bb := base.Sections[nm].Blocks[h]
			rb := resScan.Sections[nm].Blocks[h]
			baseMerged := bb.c1 != bb.c2 || bb.r1 != bb.r2
			if baseMerged && (rb.c1 != bb.c1 || rb.c2 != bb.c2 || (rb.c1 == rb.c2 && rb.r1 == rb.r2)) {
				mergeOK = false
				mergeD = append(mergeD, fmt.Sprintf("%s/%s 원본 %s -> 결과 %s", nm, h, bb, rb))
			}
		}
	}
	record("DoD-1.4", "양식복제", "데이터 블록 병합 구조(열 범위) 유지", mergeOK, strings.Join(mergeD, "; "))

	widthOK := true
	var widthD []string
	bf, _ := excelize.OpenFile(targetPath)
	maxCol := 0
	for _, sec := range base.Sections {
		for _, c := range sec.Cols {
			if c > maxCol {
				maxCol = c
			}
		}
	}
	maxCol += 4
	for c := 1; c <= maxCol; c++ {
		cn, _ := excelize.ColumnNumberToName(c)
		w1, _ := bf.GetColWidth(baseSheet, cn)
		w2, _ := rf.GetColWidth(resSheet, cn)
		if w1-w2 > 0.01 || w2-w1 > 0.01 {
			widthOK = false
			widthD = append(widthD, fmt.Sprintf("%s: %.2f vs %.2f", cn, w1, w2))
		}
	}
	record("DoD-1.5", "양식복제", "열 너비가 기준 시트(3_보안점검)와 동일", widthOK, fmt.Sprintf("1~%d열 비교 %s", maxCol, strings.Join(widthD, "; ")))

	fontOK, titleOK := true, true
	var fontD, titleD []string
	for _, nm := range sectionNames {
		for _, h := range stackHeaders {
			rb := resScan.Sections[nm].Blocks[h]
			cn := cellName(rb.c1, rb.r1)
			if sz := fontSizeOf(rf, resSheet, cn); sz != 14 || !richSizesOK(rf, resSheet, cn, 14) {
				fontOK = false
				fontD = append(fontD, fmt.Sprintf("%s/%s=%.1f", nm, h, sz))
			}
		}
		rs, bs := resScan.Sections[nm], base.Sections[nm]
		rt := fontSizeOf(rf, resSheet, cellName(rs.TitleCol, rs.TitleRow))
		bt := fontSizeOf(bf, baseSheet, cellName(bs.TitleCol, bs.TitleRow))
		if rt != bt {
			titleOK = false
		}
		titleD = append(titleD, fmt.Sprintf("%s 결과=%.1f 원본=%.1f", nm, rt, bt))
	}
	bf.Close()
	record("DoD-1.6", "양식복제", "데이터 셀 글꼴 크기 14pt 보존", fontOK, strings.Join(fontD, "; "))
	record("DoD-1.7", "양식복제", "섹션 제목 글꼴 크기가 원본과 동일", titleOK, strings.Join(titleD, "; "))

	// ---- 6. COM ----
	fmt.Println("\n[검증 6] Excel COM 이미지/도형/행높이 검증...")
	checks, _, cerr := runCOMChecks(resultPath, targetPath, selected, srcScans, resScan, resSheet)
	if cerr != nil {
		record("DoD-5.4", "이미지/도형", "Excel COM 검증 수행", false, cerr.Error())
	}
	for _, c := range checks {
		record(c.ID, c.Cat, c.Desc, c.Passed, c.Detail)
	}

	allPass := true
	for _, r := range results {
		if !r.Passed {
			allPass = false
		}
	}
	record("DoD-0", "원샷", "단일 실행으로 전 항목 일괄 통과 (재시도 없음)", allPass, "")
	finish()
}
