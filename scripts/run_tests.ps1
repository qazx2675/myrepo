$ErrorActionPreference = "Stop"

$baseDir = "C:\Users\qazx2\.gemini\antigravity\scratch\excel-workload-counter"
$dataDir = Join-Path $baseDir "test_data"
$xlsmPath = Join-Path $baseDir "서버운영_업무량_집계템플릿.xlsm"

$script:passed = 0
$script:failed = 0

function Assert-Equal {
    param(
        [string]$Id,
        [string]$Desc,
        $Expected,
        $Actual
    )
    $ok = $false
    if ($Expected -is [int] -or $Expected -is [long] -or $Expected -is [double]) {
        if ($Actual -is [int] -or $Actual -is [long] -or $Actual -is [double] -or $Actual -is [decimal] -or $Actual -is [float]) {
            if ([double]$Actual -eq [double]$Expected) {
                $ok = $true
            }
        }
    } else {
        if ($null -eq $Actual) { $Actual = "" }
        if (($Actual -is [string]) -and ([string]$Actual -eq [string]$Expected)) {
            $ok = $true
        }
    }

    if ($ok) {
        $script:passed++
        Write-Output ("[PASS] {0}: {1} (Expected={2}, Actual={3})" -f $Id, $Desc, $Expected, $Actual)
    } else {
        $script:failed++
        Write-Output ("[FAIL] {0}: {1} (Expected={2}, Actual={3})" -f $Id, $Desc, $Expected, $Actual)
    }
}

$excel = $null
$tmpDecPath = Join-Path $dataDir "서버운영 업무량(26년_12월)_NEW_김철수.xlsx"
$tmpReopenXlsm = Join-Path $baseDir "_tmp_reopen_test.xlsm"

try {
    if (Test-Path $tmpDecPath) { Remove-Item $tmpDecPath -Force }
    if (Test-Path $tmpReopenXlsm) { Remove-Item $tmpReopenXlsm -Force }

    $excel = New-Object -ComObject Excel.Application
    $excel.Visible = $false
    $excel.DisplayAlerts = $false

    $wb = $excel.Workbooks.Open($xlsmPath)
    $ws = $wb.Worksheets.Item("집계")
    $excel.CalculateFullRebuild()

    # TC-01: 김철수 10월 (2번째 시트 '업무량', A3~A7 = 5건)
    Assert-Equal "TC-01" "김철수 10월 정상 카운트 (2번째 시트 '업무량', A3 포함 5건)" 5 $ws.Range("K15").Value2

    # TC-02: 김철수 11월 (A3~A5 = 3건)
    Assert-Equal "TC-02" "김철수 11월 정상 카운트 (3건)" 3 $ws.Range("L15").Value2

    # TC-04 & TC-05 & TC-06 & TC-08 & TC-09 (드래그된 K15:L17 검증)
    Assert-Equal "TC-04a" "H15:I15 병합셀 & 혼합참조 드래그 확인 (이영희 10월: 빈행/공백/수식'' 제외 4건)" 4 $ws.Range("K16").Value2
    Assert-Equal "TC-04b" "A3 비어있고 A4만 있는 경우 (박민수 10월: 1건)" 1 $ws.Range("K17").Value2
    Assert-Equal "TC-08" "대상 파일 미존재 시 0 반환 (이영희 11월)" 0 $ws.Range("L16").Value2
    Assert-Equal "TC-09" "파일은 있으나 '업무량' 시트 미존재 시 0 반환 (박민수 11월)" 0 $ws.Range("L17").Value2

    # TC-13: SUM 집계 호환
    Assert-Equal "TC-13a" "합계 수식 SUM(K15:K17) = 10" 10 $ws.Range("K18").Value2
    Assert-Equal "TC-13b" "합계 수식 SUM(L15:L17) = 3" 3 $ws.Range("L18").Value2

    # 보조 테스트 시트 추가 (메모리 내 테스트 후 저장하지 않고 닫음)
    $wsT = $wb.Worksheets.Add()
    $wsT.Name = "_UnitTest"
    $tmpl = Join-Path $dataDir "서버운영 업무량(**년_*월)_NEW_[이름].xlsx"
    $wsT.Range("A1").Value2 = $tmpl

    # TC-03: 한 자리 월 정규화 ("9월", "09월", 숫자 9, 날짜 2026-09-01)
    $wsT.Range("B1").Value2 = "9월"
    $wsT.Range("B2").Value2 = "09월"
    $wsT.Range("B3").Value2 = 9
    $wsT.Range("B4").Value2 = "2026-09-01"
    $wsT.Range("B4").NumberFormat = "yyyy-mm-dd"
    $wsT.Range("C1").Formula = '=CountWorkload($A$1,"김철수",B1)'
    $wsT.Range("C2").Formula = '=CountWorkload($A$1,"김철수",B2)'
    $wsT.Range("C3").Formula = '=CountWorkload($A$1,"김철수",B3)'
    $wsT.Range("C4").Formula = '=CountWorkload($A$1,"김철수",B4)'

    # TC-07: OOXML 특수 유형 (inlineStr, rich-text sharedStrings, t="str", space sharedString, 0, AA3 무시)
    $wsT.Range("C5").Formula = '=CountWorkload($A$1,"특수유형","10월")'

    # TC-10: 입력 공백 처리 + 빈 셀 포함 SUM 호환성
    $wsT.Range("D1").Value2 = ""
    $wsT.Range("C6").Formula = '=CountWorkload($A$1,D1,"10월")'
    $wsT.Range("C7").Formula = '=CountWorkload($A$1,"김철수",D1)'
    $wsT.Range("C8").Formula = '=SUM(C5:C7)'

    # TC-11: 연도 선택 인자 (25, 2025, "25년"), 고정 연도 템플릿(26년_*월), 시트명 인자("개요")
    $wsT.Range("A2").Value2 = (Join-Path $dataDir "서버운영 업무량(26년_*월)_NEW_[이름].xlsx")
    $wsT.Range("C9").Formula = '=CountWorkload($A$1,"김철수","10월",25)'
    $wsT.Range("C10").Formula = '=CountWorkload($A$1,"김철수","10월",2025)'
    $wsT.Range("C11").Formula = '=CountWorkload($A$2,"김철수","10월")'
    $wsT.Range("C12").Formula = '=CountWorkload($A$1,"김철수","10월",,"개요")'

    # TC-14: 병합범위 전체($H$15:$I$15) 인자 전달 호환
    $wsT.Range("C13").Formula = '=CountWorkload(집계!$H$15:$I$15,"김철수","10월")'

    # TC-15: A1:A2 헤더만 있고 A3 이하 빈 시트 -> 0건
    $wsT.Range("C15").Formula = '=CountWorkload($A$1,"빈데이터","10월")'

    # TC-16: 공백 포함 한글 하위 폴더('공유 폴더') -> 3건
    $wsT.Range("A3").Value2 = (Join-Path (Join-Path $dataDir "공유 폴더") "서버운영 업무량(**년_*월)_NEW_[이름].xlsx")
    $wsT.Range("C16").Formula = '=CountWorkload($A$3,"홍길동","10월")'

    # TC-18: 읽기 전용(Read-Only) 속성 파일 -> 2건
    $wsT.Range("C17").Formula = '=CountWorkload($A$1,"읽기전용","10월")'

    $excel.CalculateFullRebuild()

    Assert-Equal "TC-03a" "한 자리 월 '9월' -> 26년_9월 (2건)" 2 $wsT.Range("C1").Value2
    Assert-Equal "TC-03b" "한 자리 월 '09월' -> 26년_9월 (2건)" 2 $wsT.Range("C2").Value2
    Assert-Equal "TC-03c" "한 자리 월 숫자 9 -> 26년_9월 (2건)" 2 $wsT.Range("C3").Value2
    Assert-Equal "TC-03d" "날짜 셀 2026-09-01 -> 26년_9월 (2건)" 2 $wsT.Range("C4").Value2
    Assert-Equal "TC-07" "OOXML 특수유형(inlineStr/richText/t=str/0/공백/AA3제외) -> 4건" 4 $wsT.Range("C5").Value2
    Assert-Equal "TC-10a" "이름 공백 시 '' 반환" "" $wsT.Range("C6").Value2
    Assert-Equal "TC-10b" "월 공백 시 '' 반환" "" $wsT.Range("C7").Value2
    Assert-Equal "TC-10c" "빈 문자열 결과 포함 SUM() 오류 없음 (4)" 4 $wsT.Range("C8").Value2
    Assert-Equal "TC-11a" "선택 연도 인자 25 -> 25년 파일 (2건)" 2 $wsT.Range("C9").Value2
    Assert-Equal "TC-11b" "선택 연도 인자 2025 -> 25년 파일 (2건)" 2 $wsT.Range("C10").Value2
    Assert-Equal "TC-11c" "템플릿에 26년 고정 시 정상 동작 (5건)" 5 $wsT.Range("C11").Value2
    Assert-Equal "TC-11d" "선택 시트명 '개요' 지정 시 A3:A4 카운트 (2건)" 2 $wsT.Range("C12").Value2
    Assert-Equal "TC-14" '병합 범위 전체($H$15:$I$15) 전달 호환 (5건)' 5 $wsT.Range("C13").Value2
    Assert-Equal "TC-15" "A1:A2 헤더만 있고 A3 이하가 빈 시트 -> 0건" 0 $wsT.Range("C15").Value2
    Assert-Equal "TC-16" "공백 포함 한글 하위 폴더('공유 폴더') 경로 처리 (3건)" 3 $wsT.Range("C16").Value2
    Assert-Equal "TC-17" "시트 새로고침 버튼 매크로 연결 확인" "RefreshWorkloadCounts" ([string]($ws.Buttons().Item(1).OnAction -replace "^.*!", ""))
    Assert-Equal "TC-18" "읽기 전용(Read-Only) 속성 파일 카운트 (2건)" 2 $wsT.Range("C17").Value2

    # TC-12: 열린 워크북 메모리 조회 + 닫힌 파일 수정 후 RefreshWorkloadCounts 및 Workbook_Open 검증
    $wbDec = $excel.Workbooks.Add()
    $wsDec = $wbDec.Worksheets.Item(1)
    $wsDec.Name = "업무량"
    $wsDec.Range("A1").Value2 = "제목"
    $wsDec.Range("A2").Value2 = "날짜"
    $wsDec.Range("A3").Value2 = "2026-12-01"
    $wsDec.Range("A4").Value2 = "2026-12-02"
    $wbDec.SaveAs($tmpDecPath, 51) # .xlsx
    $wbDec.Close($false)
    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($wbDec) | Out-Null

    $wsT.Range("C14").Formula = '=CountWorkload($A$1,"김철수","12월")'
    $excel.CalculateFullRebuild()
    Assert-Equal "TC-12a" "닫힌 신규 12월 파일 카운트 (2건)" 2 $wsT.Range("C14").Value2

    # 12월 파일을 Excel로 열고 저장 전 메모리상 1행 추가 -> RefreshWorkloadCounts로 3건 확인
    $wbDecOpen = $excel.Workbooks.Open($tmpDecPath)
    $wsDecOpen = $wbDecOpen.Worksheets.Item("업무량")
    $wsDecOpen.Range("A5").Value2 = "2026-12-03"
    $wb.Activate()
    $excel.Run(("'{0}'!RefreshWorkloadCounts" -f $wb.Name))
    Assert-Equal "TC-12b" "대상 파일이 열려 있고 미저장 수정 상태에서도 메모리에서 카운트 (3건)" 3 $wsT.Range("C14").Value2

    # 저장 후 닫고, 템플릿 복사본 재오픈 시 Workbook_Open 자동 갱신 검증
    $wbDecOpen.Save()
    $wbDecOpen.Close($false)
    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($wbDecOpen) | Out-Null

    $wb.Close($false)
    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($wb) | Out-Null

    # 템플릿 사본 저장 후, 대상 파일에 1건 더 추가 -> 사본 재오픈 시 Workbook_Open 자동 갱신으로 4건 확인
    Copy-Item $xlsmPath $tmpReopenXlsm -Force
    $wbRe = $excel.Workbooks.Open($tmpReopenXlsm)
    $wsRe = $wbRe.Worksheets.Item("집계")
    $wsRe.Range("K14").Value2 = "12월"
    $wbRe.Save()
    $wbRe.Close($false)
    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($wbRe) | Out-Null

    $wbDec2 = $excel.Workbooks.Open($tmpDecPath)
    $wbDec2.Worksheets.Item("업무량").Range("A6").Value2 = "2026-12-04"
    $wbDec2.Save()
    $wbDec2.Close($false)
    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($wbDec2) | Out-Null

    $wbRe2 = $excel.Workbooks.Open($tmpReopenXlsm)
    $wsRe2 = $wbRe2.Worksheets.Item("집계")
    Assert-Equal "TC-12c" "외부 파일 수정 후 .xlsm 재오픈 시 Workbook_Open 자동 재계산 (4건)" 4 $wsRe2.Range("K15").Value2
    $wbRe2.Close($false)
    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($wbRe2) | Out-Null
}
finally {
    if ($excel) {
        $excel.Quit()
        [System.Runtime.Interopservices.Marshal]::ReleaseComObject($excel) | Out-Null
    }
    if (Test-Path $tmpDecPath) { Remove-Item $tmpDecPath -Force }
    if (Test-Path $tmpReopenXlsm) { Remove-Item $tmpReopenXlsm -Force }
    [GC]::Collect()
    [GC]::WaitForPendingFinalizers()
}

Write-Output ("SUMMARY: Passed={0}, Failed={1}" -f $script:passed, $script:failed)
if ($script:failed -gt 0) {
    exit 1
}
exit 0
