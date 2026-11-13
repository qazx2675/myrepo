$ErrorActionPreference = "Stop"

$baseDir = "C:\Users\qazx2\.gemini\antigravity\scratch\excel-workload-counter"
$dataDir = Join-Path $baseDir "test_data"
$basPath = Join-Path $baseDir "modWorkloadCounter.bas"
$outXlsm = Join-Path $baseDir "서버운영_업무량_집계템플릿.xlsm"

if (Test-Path $outXlsm) { Remove-Item $outXlsm -Force }

$excel = $null
try {
    $excel = New-Object -ComObject Excel.Application
    $excel.Visible = $false
    $excel.DisplayAlerts = $false

    # 1. 이영희 10월 테스트 파일을 실제 Excel로 열어 수식 캐시 값 및 Excel ZIP 구조로 저장
    $leePath = Join-Path $dataDir "서버운영 업무량(26년_10월)_NEW_이영희.xlsx"
    $wbLee = $excel.Workbooks.Open($leePath)
    $excel.CalculateFullRebuild()
    $wbLee.Save()
    $wbLee.Close($false)
    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($wbLee) | Out-Null

    # 2. 집계 템플릿 .xlsm 생성
    $wb = $excel.Workbooks.Add()
    $ws = $wb.Worksheets.Item(1)
    $ws.Name = "집계"

    # VBA 모듈 임포트 및 ThisWorkbook(현재_통합_문서) Open 이벤트 등록
    $wb.VBProject.VBComponents.Import($basPath) | Out-Null
    $thisWbMod = $wb.VBProject.VBComponents.Item($wb.CodeName).CodeModule
    $openCode = @"
Private Sub Workbook_Open()
    On Error Resume Next
    RefreshWorkloadCounts
End Sub
"@
    $thisWbMod.AddFromString($openCode)

    # 타이틀 및 레이아웃 구성 (H14:L18)
    $ws.Range("H12:L12").Merge()
    $ws.Range("H12").Value2 = "서버운영 업무량 월별 카운트 현황"
    $ws.Range("H12").Font.Name = "맑은 고딕"
    $ws.Range("H12").Font.Size = 13
    $ws.Range("H12").Font.Bold = $true
    $ws.Range("H12").VerticalAlignment = -4108

    $ws.Range("H14:I14").Merge()
    $ws.Range("H14").Value2 = "대상 파일 경로 템플릿"
    $ws.Range("J14").Value2 = "이름"
    $ws.Range("K14").Value2 = "10월"
    $ws.Range("L14").Value2 = "11월"

    $ws.Range("H15:I15").Merge()
    $ws.Range("H15").Value2 = (Join-Path $dataDir "서버운영 업무량(**년_*월)_NEW_[이름].xlsx")
    $ws.Range("H15:I15").WrapText = $true

    $ws.Range("J15").Value2 = "김철수"
    $ws.Range("J16").Value2 = "이영희"
    $ws.Range("J17").Value2 = "박민수"

    # K15:L17 혼합참조 수식 적용 (=CountWorkload($H$15,$J15,K$14))
    $ws.Range("K15:L17").Formula = '=CountWorkload($H$15,$J15,K$14)'

    $ws.Range("J18").Value2 = "합계"
    $ws.Range("K18").Formula = "=SUM(K15:K17)"
    $ws.Range("L18").Formula = "=SUM(L15:L17)"

    $ws.Range("H20:L20").Merge()
    $ws.Range("H20").Value2 = "* 공유폴더 경로 적용 시 H15 셀의 경로를 수정한 뒤 [새로고침] 버튼 또는 Ctrl+Alt+F9를 누르세요."
    $ws.Range("H20").Font.Size = 9
    $ws.Range("H20").Font.Color = 0x555555

    # 서식 정리
    $ws.Cells.Font.Name = "맑은 고딕"
    $ws.Range("H12").Font.Size = 13
    $headerRng = $ws.Range("H14:L14")
    $headerRng.Font.Bold = $true
    $headerRng.HorizontalAlignment = -4108
    $headerRng.VerticalAlignment = -4108
    $headerRng.Interior.Color = 0xEFE8DC

    $sumRng = $ws.Range("J18:L18")
    $sumRng.Font.Bold = $true
    $sumRng.Interior.Color = 0xF5F5F5

    $tableRng = $ws.Range("H14:L18")
    $tableRng.Borders.LineStyle = 1
    $tableRng.Borders.Weight = 2
    $tableRng.VerticalAlignment = -4108

    $ws.Range("J15:J18").HorizontalAlignment = -4108
    $ws.Range("K15:L18").NumberFormat = "#,##0"
    $ws.Range("K15:L18").HorizontalAlignment = -4108

    $ws.Columns.Item("H").ColumnWidth = 28
    $ws.Columns.Item("I").ColumnWidth = 28
    $ws.Columns.Item("J").ColumnWidth = 14
    $ws.Columns.Item("K").ColumnWidth = 12
    $ws.Columns.Item("L").ColumnWidth = 12
    $ws.Rows.Item(14).RowHeight = 24
    $ws.Rows.Item(15).RowHeight = 36
    $ws.Rows.Item(16).RowHeight = 22
    $ws.Rows.Item(17).RowHeight = 22
    $ws.Rows.Item(18).RowHeight = 24

    # 새로고침 버튼 추가 (K13:L13 위치 위)
    $btnLeft = $ws.Range("K12").Left + 2
    $btnTop = $ws.Range("K12").Top + 2
    $btnWidth = $ws.Range("K12:L12").Width - 4
    $btnHeight = 22
    $btn = $ws.Buttons().Add($btnLeft, $btnTop, $btnWidth, $btnHeight)
    $btn.Caption = "새로고침 (재계산)"
    $btn.OnAction = "RefreshWorkloadCounts"

    $excel.CalculateFullRebuild()
    $wb.SaveAs($outXlsm, 52) # xlOpenXMLWorkbookMacroEnabled
    $wb.Close($false)
    [System.Runtime.Interopservices.Marshal]::ReleaseComObject($wb) | Out-Null

    Write-Output "Built template: $outXlsm"
}
finally {
    if ($excel) {
        $excel.Quit()
        [System.Runtime.Interopservices.Marshal]::ReleaseComObject($excel) | Out-Null
    }
    [GC]::Collect()
    [GC]::WaitForPendingFinalizers()
}
