import os

BASE_DIR = r"C:\Users\qazx2\.gemini\antigravity\scratch\excel-workload-counter"
BAS_PATH = os.path.join(BASE_DIR, "modWorkloadCounter.bas")

VBA_CODE = r'''Attribute VB_Name = "modWorkloadCounter"
Option Explicit

' ============================================================================
' Excel 2021 서버운영 업무량 동적 카운트 모듈 (32비트 / 64비트 공용)
' ============================================================================
' 사용법:
'   =CountWorkload($H$15, $J15, K$14)
'   =CountWorkload($H$15, $J15, K$14, [연도], [시트명])
'
' 매개변수:
'   PathTemplate : 경로 템플릿 셀 (예: H15, "\\공유폴더\서버운영 업무량(**년_*월)_NEW_[이름].xlsx")
'   PersonName   : 이름 셀 (예: J15, "김철수")
'   MonthValue   : 월 셀 (예: K14, "10월" 또는 숫자 9, 10)
'   YearValue    : (선택) 2자리 또는 4자리 연도. 생략 시 현재 연도 뒤 2자리(예: 26) 사용
'   SheetName    : (선택) 대상 시트 이름. 생략 시 "업무량" 사용
' ============================================================================

Private mPow2(0 To 24) As Long
Private mPow2Ready As Boolean

Private Sub EnsurePow2()
    If mPow2Ready Then Exit Sub
    Dim i As Long
    mPow2(0) = 1&
    For i = 1 To 24
        mPow2(i) = mPow2(i - 1) * 2&
    Next i
    mPow2Ready = True
End Sub

Public Function CountWorkload( _
    ByVal PathTemplate As Variant, _
    ByVal PersonName As Variant, _
    ByVal MonthValue As Variant, _
    Optional ByVal YearValue As Variant, _
    Optional ByVal SheetName As Variant _
) As Variant
    On Error GoTo ErrHandler

    Dim vPath As Variant, vPerson As Variant, vMonth As Variant
    Dim vYear As Variant, vSheet As Variant
    Dim tmpl As String, pName As String, mToken As String
    Dim yr2 As String, targetSheet As String, targetPath As String
    Dim sTmp As String
    Dim openCount As Long

    vPath = GetScalarValue(PathTemplate)
    vPerson = GetScalarValue(PersonName)
    vMonth = GetScalarValue(MonthValue)

    If IsError(vPath) Or IsEmpty(vPath) Or IsNull(vPath) Then
        CountWorkload = ""
        Exit Function
    End If
    If IsError(vPerson) Or IsEmpty(vPerson) Or IsNull(vPerson) Then
        CountWorkload = ""
        Exit Function
    End If
    If IsError(vMonth) Or IsEmpty(vMonth) Or IsNull(vMonth) Then
        CountWorkload = ""
        Exit Function
    End If

    tmpl = CleanTrim(CStr(vPath))
    pName = CleanTrim(CStr(vPerson))
    If Len(tmpl) = 0 Or Len(pName) = 0 Then
        CountWorkload = ""
        Exit Function
    End If

    mToken = FormatMonthToken(vMonth)
    If Len(mToken) = 0 Then
        CountWorkload = ""
        Exit Function
    End If

    If IsMissing(YearValue) Then
        vYear = Empty
    Else
        vYear = GetScalarValue(YearValue)
    End If
    yr2 = FormatYear2(vYear)

    targetSheet = ChrW(&HC5C5) & ChrW(&HBB34) & ChrW(&HB7C9) ' "업무량"
    If Not IsMissing(SheetName) Then
        vSheet = GetScalarValue(SheetName)
        If Not IsError(vSheet) And Not IsEmpty(vSheet) And Not IsNull(vSheet) Then
            sTmp = CleanTrim(CStr(vSheet))
            If Len(sTmp) > 0 Then targetSheet = sTmp
        End If
    End If

    targetPath = BuildTargetPath(tmpl, yr2, mToken, pName)
    If Len(targetPath) = 0 Then
        CountWorkload = 0
        Exit Function
    End If

    ' 1. 현재 Excel에 이미 열려 있는 파일이면 메모리에서 직접 카운트
    If TryCountOpenWorkbook(targetPath, targetSheet, openCount) Then
        CountWorkload = openCount
        Exit Function
    End If

    ' 2. 닫혀 있는 .xlsx 파일이면 ZIP+XML 직접 파싱으로 안전하게 카운트
    CountWorkload = CountClosedXlsxColumnA(targetPath, targetSheet)
    Exit Function

ErrHandler:
    CountWorkload = 0
End Function

Public Sub RefreshWorkloadCounts()
    Application.CalculateFullRebuild
End Sub

' ============================================================================
' 인자 정규화 및 경로 생성 도우미
' ============================================================================

Private Function GetScalarValue(ByVal v As Variant) As Variant
    If TypeName(v) = "Range" Then
        GetScalarValue = v.Cells(1, 1).Value
    Else
        GetScalarValue = v
    End If
End Function

Private Function CleanTrim(ByVal s As String) As String
    Dim startIdx As Long, endIdx As Long
    Dim ch As Integer
    startIdx = 1
    endIdx = Len(s)
    Do While startIdx <= endIdx
        ch = AscW(Mid$(s, startIdx, 1))
        If ch <> 32 And ch <> 9 And ch <> 10 And ch <> 13 Then Exit Do
        startIdx = startIdx + 1
    Loop
    Do While endIdx >= startIdx
        ch = AscW(Mid$(s, endIdx, 1))
        If ch <> 32 And ch <> 9 And ch <> 10 And ch <> 13 Then Exit Do
        endIdx = endIdx - 1
    Loop
    If startIdx > endIdx Then
        CleanTrim = ""
    Else
        CleanTrim = Mid$(s, startIdx, endIdx - startIdx + 1)
    End If
End Function

Private Function HasVisibleText(ByVal s As String) As Boolean
    Dim i As Long, ch As Integer
    For i = 1 To Len(s)
        ch = AscW(Mid$(s, i, 1))
        If ch <> 32 And ch <> 9 And ch <> 10 And ch <> 13 Then
            HasVisibleText = True
            Exit Function
        End If
    Next i
    HasVisibleText = False
End Function

Private Function FormatMonthToken(ByVal vMonth As Variant) As String
    Dim sMonthChar As String
    Dim s As String
    Dim i As Long, ch As Integer
    Dim curDigits As String, lastValid As Long, nVal As Long
    Dim foundValid As Boolean

    sMonthChar = ChrW(&HC6D4) ' "월"

    If IsDate(vMonth) Then
        FormatMonthToken = CStr(Month(CDate(vMonth))) & sMonthChar
        Exit Function
    End If

    s = CleanTrim(CStr(vMonth))
    If Len(s) = 0 Then
        FormatMonthToken = ""
        Exit Function
    End If

    ' 문자열 내 숫자 그룹 중 마지막(1~12)을 추출해 앞 0 없이 "*월" 생성
    curDigits = ""
    foundValid = False
    For i = 1 To Len(s)
        ch = AscW(Mid$(s, i, 1))
        If ch >= 48 And ch <= 57 Then
            curDigits = curDigits & ChrW(ch)
        ElseIf Len(curDigits) > 0 Then
            If Len(curDigits) <= 4 Then
                nVal = CLng(curDigits)
                If nVal >= 1 And nVal <= 12 Then
                    lastValid = nVal
                    foundValid = True
                End If
            End If
            curDigits = ""
        End If
    Next i
    If Len(curDigits) > 0 And Len(curDigits) <= 4 Then
        nVal = CLng(curDigits)
        If nVal >= 1 And nVal <= 12 Then
            lastValid = nVal
            foundValid = True
        End If
    End If

    If foundValid Then
        FormatMonthToken = CStr(lastValid) & sMonthChar
    Else
        FormatMonthToken = s
    End If
End Function

Private Function FormatYear2(ByVal vYear As Variant) As String
    Dim defYr As String
    Dim s As String, i As Long, ch As Integer, digits As String

    defYr = Right$("0" & CStr(Year(Date) Mod 100), 2)

    If IsError(vYear) Or IsEmpty(vYear) Or IsNull(vYear) Then
        FormatYear2 = defYr
        Exit Function
    End If
    If IsDate(vYear) Then
        FormatYear2 = Right$("0" & CStr(Year(CDate(vYear)) Mod 100), 2)
        Exit Function
    End If

    s = CleanTrim(CStr(vYear))
    If Len(s) = 0 Then
        FormatYear2 = defYr
        Exit Function
    End If

    digits = ""
    For i = 1 To Len(s)
        ch = AscW(Mid$(s, i, 1))
        If ch >= 48 And ch <= 57 Then digits = digits & ChrW(ch)
    Next i

    If Len(digits) >= 2 Then
        FormatYear2 = Right$(digits, 2)
    ElseIf Len(digits) = 1 Then
        FormatYear2 = "0" & digits
    Else
        FormatYear2 = defYr
    End If
End Function

Private Function BuildTargetPath( _
    ByVal tmpl As String, _
    ByVal yr2 As String, _
    ByVal mToken As String, _
    ByVal pName As String _
) As String
    Dim sYearChar As String, sNameToken As String, sMonthWild As String
    Dim res As String

    sYearChar = ChrW(&HB144) ' "년"
    sMonthWild = "*" & ChrW(&HC6D4) ' "*월"
    sNameToken = ChrW(&H5B) & ChrW(&HC774) & ChrW(&HB984) & ChrW(&H5D) ' "[이름]"

    res = Replace(tmpl, "**" & sYearChar, yr2 & sYearChar, 1, -1, vbTextCompare)
    res = Replace(res, sMonthWild, mToken, 1, -1, vbTextCompare)
    res = Replace(res, sNameToken, pName, 1, -1, vbTextCompare)
    BuildTargetPath = res
End Function

' ============================================================================
' 열린 워크북 직접 카운트
' ============================================================================

Private Function TryCountOpenWorkbook( _
    ByVal targetPath As String, _
    ByVal targetSheet As String, _
    ByRef outCount As Long _
) As Boolean
    Dim wb As Workbook, ws As Worksheet
    Dim normTarget As String, normWb As String
    Dim lastRow As Long, r As Long, cnt As Long
    Dim vals As Variant

    normTarget = Replace(targetPath, "/", "\")
    For Each wb In Application.Workbooks
        normWb = Replace(wb.FullName, "/", "\")
        If StrComp(normWb, normTarget, vbTextCompare) = 0 Then
            On Error Resume Next
            Set ws = wb.Worksheets(targetSheet)
            On Error GoTo 0

            If ws Is Nothing Then
                outCount = 0
                TryCountOpenWorkbook = True
                Exit Function
            End If

            If Not IsEmpty(ws.Cells(ws.Rows.Count, 1).Value2) Then
                lastRow = ws.Rows.Count
            Else
                lastRow = ws.Cells(ws.Rows.Count, 1).End(-4162).Row ' xlUp
            End If

            If lastRow < 3 Then
                outCount = 0
                TryCountOpenWorkbook = True
                Exit Function
            End If

            vals = ws.Range(ws.Cells(3, 1), ws.Cells(lastRow, 1)).Value2
            cnt = 0
            If IsArray(vals) Then
                For r = LBound(vals, 1) To UBound(vals, 1)
                    If IsCountableScalar(vals(r, 1)) Then cnt = cnt + 1
                Next r
            Else
                If IsCountableScalar(vals) Then cnt = 1
            End If

            outCount = cnt
            TryCountOpenWorkbook = True
            Exit Function
        End If
    Next wb

    TryCountOpenWorkbook = False
End Function

Private Function IsCountableScalar(ByVal v As Variant) As Boolean
    If IsEmpty(v) Or IsNull(v) Then
        IsCountableScalar = False
    ElseIf IsError(v) Then
        IsCountableScalar = True
    ElseIf VarType(v) = vbString Then
        IsCountableScalar = HasVisibleText(CStr(v))
    Else
        IsCountableScalar = True
    End If
End Function

' ============================================================================
' 닫힌 .xlsx 파일 읽기 (ZIP + OOXML 직접 파싱)
' ============================================================================

Private Function CountClosedXlsxColumnA(ByVal filePath As String, ByVal targetSheet As String) As Long
    On Error GoTo ErrHandler

    Dim fso As Object
    Set fso = CreateObject("Scripting.FileSystemObject")
    If Not fso.FileExists(filePath) Then
        CountClosedXlsxColumnA = 0
        Exit Function
    End If

    Dim zipBytes() As Byte
    If Not ReadFileBytesSafe(fso, filePath, zipBytes) Then
        CountClosedXlsxColumnA = 0
        Exit Function
    End If

    Dim entryNames() As String
    Dim entryMethod() As Long
    Dim entryCompSize() As Long
    Dim entryUncompSize() As Long
    Dim entryLocalOff() As Long
    Dim entryCount As Long

    If Not ParseZipCentralDirectory(zipBytes, entryNames, entryMethod, entryCompSize, entryUncompSize, entryLocalOff, entryCount) Then
        CountClosedXlsxColumnA = 0
        Exit Function
    End If

    Dim wbXml As String, relsXml As String, sheetXml As String
    If Not ExtractZipText(zipBytes, entryNames, entryMethod, entryCompSize, entryUncompSize, entryLocalOff, entryCount, "xl/workbook.xml", wbXml) Then
        CountClosedXlsxColumnA = 0
        Exit Function
    End If

    Dim rId As String
    rId = FindSheetRelId(wbXml, targetSheet)
    If Len(rId) = 0 Then
        CountClosedXlsxColumnA = 0
        Exit Function
    End If

    If Not ExtractZipText(zipBytes, entryNames, entryMethod, entryCompSize, entryUncompSize, entryLocalOff, entryCount, "xl/_rels/workbook.xml.rels", relsXml) Then
        CountClosedXlsxColumnA = 0
        Exit Function
    End If

    Dim sheetZipPath As String
    sheetZipPath = FindRelTarget(relsXml, rId)
    If Len(sheetZipPath) = 0 Then
        CountClosedXlsxColumnA = 0
        Exit Function
    End If

    If Not ExtractZipText(zipBytes, entryNames, entryMethod, entryCompSize, entryUncompSize, entryLocalOff, entryCount, sheetZipPath, sheetXml) Then
        CountClosedXlsxColumnA = 0
        Exit Function
    End If

    CountClosedXlsxColumnA = CountColumnAFromSheetXml(sheetXml, zipBytes, entryNames, entryMethod, entryCompSize, entryUncompSize, entryLocalOff, entryCount)
    Exit Function

ErrHandler:
    CountClosedXlsxColumnA = 0
End Function

Private Function ReadFileBytesSafe(ByVal fso As Object, ByVal filePath As String, ByRef outBytes() As Byte) As Boolean
    Dim stm As Object
    Dim tmpDir As String, tmpPath As String

    On Error GoTo TryTempCopy

    Set stm = CreateObject("ADODB.Stream")
    stm.Type = 1 ' adTypeBinary
    stm.Open
    stm.LoadFromFile filePath
    If stm.Size <= 0 Then
        stm.Close
        ReadFileBytesSafe = False
        Exit Function
    End If
    outBytes = stm.Read
    stm.Close
    ReadFileBytesSafe = True
    Exit Function

TryTempCopy:
    On Error GoTo ErrFail
    If Not stm Is Nothing Then
        On Error Resume Next
        If stm.State <> 0 Then stm.Close
        On Error GoTo ErrFail
    End If

    tmpDir = fso.GetSpecialFolder(2).Path ' TemporaryFolder
    tmpPath = tmpDir & "\wl_" & Replace(CStr(Timer), ".", "") & "_" & CStr(Int(Rnd() * 100000)) & ".xlsx"
    fso.CopyFile filePath, tmpPath, True

    Set stm = CreateObject("ADODB.Stream")
    stm.Type = 1
    stm.Open
    stm.LoadFromFile tmpPath
    If stm.Size > 0 Then
        outBytes = stm.Read
        ReadFileBytesSafe = True
    Else
        ReadFileBytesSafe = False
    End If
    stm.Close

    On Error Resume Next
    fso.DeleteFile tmpPath, True
    Exit Function

ErrFail:
    On Error Resume Next
    If Len(tmpPath) > 0 Then fso.DeleteFile tmpPath, True
    ReadFileBytesSafe = False
End Function

' ============================================================================
' XML 파서 (workbook.xml, rels, sharedStrings.xml, worksheet.xml)
' ============================================================================

Private Function CreateXmlDoc(ByVal xmlText As String) As Object
    Dim doc As Object
    If Len(xmlText) = 0 Then Exit Function
    On Error Resume Next
    Set doc = CreateObject("MSXML2.DOMDocument.6.0")
    If doc Is Nothing Then Set doc = CreateObject("MSXML2.DOMDocument")
    On Error GoTo 0
    If doc Is Nothing Then Exit Function

    doc.async = False
    doc.validateOnParse = False
    doc.resolveExternals = False
    doc.preserveWhiteSpace = False
    If doc.LoadXML(xmlText) Then
        Set CreateXmlDoc = doc
    End If
End Function

Private Function GetAttrText(ByVal node As Object, ByVal baseName As String) As String
    Dim attr As Object
    For Each attr In node.Attributes
        If StrComp(attr.BaseName, baseName, vbTextCompare) = 0 Then
            GetAttrText = CStr(attr.Text)
            Exit Function
        End If
    Next attr
    GetAttrText = ""
End Function

Private Function GetRelIdAttr(ByVal node As Object) As String
    Dim attr As Object
    For Each attr In node.Attributes
        If StrComp(attr.NodeName, "r:id", vbTextCompare) = 0 Then
            GetRelIdAttr = CStr(attr.Text)
            Exit Function
        End If
    Next attr
    GetRelIdAttr = GetAttrText(node, "id")
End Function

Private Function FindSheetRelId(ByVal wbXml As String, ByVal targetSheet As String) As String
    Dim doc As Object
    Set doc = CreateXmlDoc(wbXml)
    If doc Is Nothing Or doc.DocumentElement Is Nothing Then Exit Function
    FindSheetRelId = FindSheetRelIdRecursive(doc.DocumentElement, targetSheet)
End Function

Private Function FindSheetRelIdRecursive(ByVal parentNode As Object, ByVal targetSheet As String) As String
    Dim child As Object, res As String
    For Each child In parentNode.ChildNodes
        If child.NodeType = 1 Then
            If child.BaseName = "sheet" Then
                If StrComp(GetAttrText(child, "name"), targetSheet, vbBinaryCompare) = 0 Then
                    FindSheetRelIdRecursive = GetRelIdAttr(child)
                    Exit Function
                End If
            Else
                res = FindSheetRelIdRecursive(child, targetSheet)
                If Len(res) > 0 Then
                    FindSheetRelIdRecursive = res
                    Exit Function
                End If
            End If
        End If
    Next child
End Function

Private Function FindRelTarget(ByVal relsXml As String, ByVal rId As String) As String
    Dim doc As Object
    Dim child As Object, rawTarget As String
    Set doc = CreateXmlDoc(relsXml)
    If doc Is Nothing Or doc.DocumentElement Is Nothing Then Exit Function

    For Each child In doc.DocumentElement.ChildNodes
        If child.NodeType = 1 And child.BaseName = "Relationship" Then
            If StrComp(GetAttrText(child, "Id"), rId, vbBinaryCompare) = 0 Then
                rawTarget = Replace(GetAttrText(child, "Target"), "\", "/")
                Do While Left$(rawTarget, 2) = "./"
                    rawTarget = Mid$(rawTarget, 3)
                Loop
                If Left$(rawTarget, 1) = "/" Then
                    rawTarget = Mid$(rawTarget, 2)
                ElseIf LCase$(Left$(rawTarget, 3)) <> "xl/" Then
                    rawTarget = "xl/" & rawTarget
                End If
                FindRelTarget = rawTarget
                Exit Function
            End If
        End If
    Next child
End Function

Private Function LoadSharedStrings( _
    ByRef zipBytes() As Byte, _
    ByRef entryNames() As String, _
    ByRef entryMethod() As Long, _
    ByRef entryCompSize() As Long, _
    ByRef entryUncompSize() As Long, _
    ByRef entryLocalOff() As Long, _
    ByVal entryCount As Long, _
    ByRef outArr() As String _
) As Long
    Dim ssXml As String
    Dim doc As Object
    Dim totalCount As Long
    Dim idx As Long, siNode As Object

    If Not ExtractZipText(zipBytes, entryNames, entryMethod, entryCompSize, entryUncompSize, entryLocalOff, entryCount, "xl/sharedStrings.xml", ssXml) Then
        LoadSharedStrings = 0
        Exit Function
    End If

    Set doc = CreateXmlDoc(ssXml)
    If doc Is Nothing Or doc.DocumentElement Is Nothing Then
        LoadSharedStrings = 0
        Exit Function
    End If

    totalCount = doc.DocumentElement.ChildNodes.Length
    If totalCount <= 0 Then
        LoadSharedStrings = 0
        Exit Function
    End If

    ReDim outArr(0 To totalCount - 1)
    idx = 0
    For Each siNode In doc.DocumentElement.ChildNodes
        If siNode.NodeType = 1 And siNode.BaseName = "si" Then
            outArr(idx) = ExtractRichText(siNode)
            idx = idx + 1
        End If
    Next siNode

    If idx = 0 Then
        LoadSharedStrings = 0
    Else
        If idx < totalCount Then ReDim Preserve outArr(0 To idx - 1)
        LoadSharedStrings = idx
    End If
End Function

Private Function ExtractRichText(ByVal containerNode As Object) As String
    Dim child As Object, subChild As Object
    Dim acc As String
    acc = ""
    For Each child In containerNode.ChildNodes
        If child.NodeType = 1 Then
            If child.BaseName = "t" Then
                acc = acc & CStr(child.Text)
            ElseIf child.BaseName = "r" Then
                For Each subChild In child.ChildNodes
                    If subChild.NodeType = 1 And subChild.BaseName = "t" Then
                        acc = acc & CStr(subChild.Text)
                    End If
                Next subChild
            End If
        End If
    Next child
    ExtractRichText = acc
End Function

Private Function IsColumnARef(ByVal cellRef As String) As Boolean
    Dim n As Long, i As Long, ch As Integer
    n = Len(cellRef)
    If n < 2 Then Exit Function
    If UCase$(Left$(cellRef, 1)) <> "A" Then Exit Function
    For i = 2 To n
        ch = AscW(Mid$(cellRef, i, 1))
        If ch < 48 Or ch > 57 Then Exit Function
    Next i
    IsColumnARef = True
End Function

Private Function CountColumnAFromSheetXml( _
    ByVal sheetXml As String, _
    ByRef zipBytes() As Byte, _
    ByRef entryNames() As String, _
    ByRef entryMethod() As Long, _
    ByRef entryCompSize() As Long, _
    ByRef entryUncompSize() As Long, _
    ByRef entryLocalOff() As Long, _
    ByVal entryCount As Long _
) As Long
    Dim doc As Object
    Dim sheetDataNode As Object, child As Object
    Dim ssLoaded As Boolean, ssArr() As String, ssCount As Long
    Dim rowNode As Object, cNode As Object, cSub As Object
    Dim curRow As Long, rAttr As String
    Dim curCol As Long, cRef As String, isColA As Boolean
    Dim cellType As String, hasV As Boolean, vText As String
    Dim isNode As Object, cnt As Long, ssIdx As Long

    Set doc = CreateXmlDoc(sheetXml)
    If doc Is Nothing Or doc.DocumentElement Is Nothing Then Exit Function

    For Each child In doc.DocumentElement.ChildNodes
        If child.NodeType = 1 And child.BaseName = "sheetData" Then
            Set sheetDataNode = child
            Exit For
        End If
    Next child
    If sheetDataNode Is Nothing Then Exit Function

    ssLoaded = False
    ssCount = 0
    curRow = 0
    cnt = 0

    For Each rowNode In sheetDataNode.ChildNodes
        If rowNode.NodeType = 1 And rowNode.BaseName = "row" Then
            rAttr = GetAttrText(rowNode, "r")
            If Len(rAttr) > 0 Then
                curRow = CLng(rAttr)
            Else
                curRow = curRow + 1
            End If

            If curRow >= 3 Then
                curCol = 0
                For Each cNode In rowNode.ChildNodes
                    If cNode.NodeType = 1 And cNode.BaseName = "c" Then
                        curCol = curCol + 1
                        cRef = GetAttrText(cNode, "r")
                        If Len(cRef) > 0 Then
                            isColA = IsColumnARef(cRef)
                        Else
                            isColA = (curCol = 1)
                        End If

                        If isColA Then
                            cellType = GetAttrText(cNode, "t")
                            hasV = False
                            vText = ""
                            Set isNode = Nothing

                            For Each cSub In cNode.ChildNodes
                                If cSub.NodeType = 1 Then
                                    If cSub.BaseName = "v" Then
                                        hasV = True
                                        vText = CStr(cSub.Text)
                                    ElseIf cSub.BaseName = "is" Then
                                        Set isNode = cSub
                                    End If
                                End If
                            Next cSub

                            If cellType = "s" Then
                                If hasV And Len(vText) > 0 Then
                                    If Not ssLoaded Then
                                        ssCount = LoadSharedStrings(zipBytes, entryNames, entryMethod, entryCompSize, entryUncompSize, entryLocalOff, entryCount, ssArr)
                                        ssLoaded = True
                                    End If
                                    ssIdx = CLng(vText)
                                    If ssIdx >= 0 And ssIdx < ssCount Then
                                        If HasVisibleText(ssArr(ssIdx)) Then cnt = cnt + 1
                                    End If
                                End If
                            ElseIf cellType = "inlineStr" Then
                                If Not isNode Is Nothing Then
                                    If HasVisibleText(ExtractRichText(isNode)) Then cnt = cnt + 1
                                End If
                            Else
                                If hasV Then
                                    If HasVisibleText(vText) Then cnt = cnt + 1
                                End If
                            End If
                            Exit For
                        End If
                    End If
                Next cNode
            End If
        End If
    Next rowNode

    CountColumnAFromSheetXml = cnt
End Function

' ============================================================================
' ZIP 파서 및 순수 VBA RFC 1951 Deflate 압축 해제
' ============================================================================

Private Function ReadUInt16(ByRef b() As Byte, ByVal off As Long) As Long
    ReadUInt16 = CLng(b(off)) + CLng(b(off + 1)) * 256&
End Function

Private Function ReadUInt32(ByRef b() As Byte, ByVal off As Long) As Double
    ReadUInt32 = CDbl(b(off)) + CDbl(b(off + 1)) * 256# + CDbl(b(off + 2)) * 65536# + CDbl(b(off + 3)) * 16777216#
End Function

Private Function Utf8BytesToString(ByRef b() As Byte, Optional ByVal byteCount As Long = -1) As String
    On Error GoTo ErrHandler
    Dim lb As Long, ub As Long, actualLen As Long
    Dim stm As Object, trimmed() As Byte, i As Long

    lb = LBound(b)
    ub = UBound(b)
    If ub < lb Then Exit Function

    actualLen = ub - lb + 1
    If byteCount = 0 Then Exit Function
    If byteCount > 0 And byteCount < actualLen Then
        ReDim trimmed(0 To byteCount - 1)
        For i = 0 To byteCount - 1
            trimmed(i) = b(lb + i)
        Next i
    Else
        trimmed = b
    End If

    Set stm = CreateObject("ADODB.Stream")
    stm.Type = 1 ' adTypeBinary
    stm.Open
    stm.Write trimmed
    stm.Position = 0
    stm.Type = 2 ' adTypeText
    stm.Charset = "utf-8"
    Utf8BytesToString = stm.ReadText
    stm.Close
    Exit Function

ErrHandler:
    On Error Resume Next
    If Not stm Is Nothing Then
        If stm.State <> 0 Then stm.Close
    End If
    Utf8BytesToString = ""
End Function

Private Function SliceToString(ByRef b() As Byte, ByVal startOff As Long, ByVal byteLen As Long) As String
    If byteLen <= 0 Then Exit Function
    Dim subArr() As Byte
    Dim i As Long
    ReDim subArr(0 To byteLen - 1)
    For i = 0 To byteLen - 1
        subArr(i) = b(startOff + i)
    Next i
    SliceToString = Utf8BytesToString(subArr, byteLen)
End Function

Private Function ParseZipCentralDirectory( _
    ByRef b() As Byte, _
    ByRef entryNames() As String, _
    ByRef entryMethod() As Long, _
    ByRef entryCompSize() As Long, _
    ByRef entryUncompSize() As Long, _
    ByRef entryLocalOff() As Long, _
    ByRef entryCount As Long _
) As Boolean
    Dim totalLen As Long, minSearch As Long, i As Long, eocd As Long, commentLen As Long
    Dim cdCount As Long, cdOffset As Long
    Dim pos As Long, nameLen As Long, extraLen As Long, cmtLen As Long

    totalLen = UBound(b) - LBound(b) + 1
    If totalLen < 22 Then Exit Function

    minSearch = totalLen - 65558
    If minSearch < 0 Then minSearch = 0

    eocd = -1
    For i = totalLen - 22 To minSearch Step -1
        If b(i) = &H50 And b(i + 1) = &H4B And b(i + 2) = &H5 And b(i + 3) = &H6 Then
            commentLen = ReadUInt16(b, i + 20)
            If i + 22 + commentLen = totalLen Then
                eocd = i
                Exit For
            End If
        End If
    Next i
    If eocd < 0 Then Exit Function

    cdCount = ReadUInt16(b, eocd + 10)
    cdOffset = CLng(ReadUInt32(b, eocd + 16))
    If cdCount <= 0 Or cdOffset < 0 Or cdOffset >= totalLen Then Exit Function

    ReDim entryNames(0 To cdCount - 1)
    ReDim entryMethod(0 To cdCount - 1)
    ReDim entryCompSize(0 To cdCount - 1)
    ReDim entryUncompSize(0 To cdCount - 1)
    ReDim entryLocalOff(0 To cdCount - 1)

    pos = cdOffset
    For i = 0 To cdCount - 1
        If pos + 46 > totalLen Then Exit Function
        If b(pos) <> &H50 Or b(pos + 1) <> &H4B Or b(pos + 2) <> &H1 Or b(pos + 3) <> &H2 Then Exit Function

        entryMethod(i) = ReadUInt16(b, pos + 10)
        entryCompSize(i) = CLng(ReadUInt32(b, pos + 20))
        entryUncompSize(i) = CLng(ReadUInt32(b, pos + 24))
        nameLen = ReadUInt16(b, pos + 28)
        extraLen = ReadUInt16(b, pos + 30)
        cmtLen = ReadUInt16(b, pos + 32)
        entryLocalOff(i) = CLng(ReadUInt32(b, pos + 42))

        entryNames(i) = Replace(SliceToString(b, pos + 46, nameLen), "\", "/")
        pos = pos + 46 + nameLen + extraLen + cmtLen
    Next i

    entryCount = cdCount
    ParseZipCentralDirectory = True
End Function

Private Function ExtractZipText( _
    ByRef b() As Byte, _
    ByRef entryNames() As String, _
    ByRef entryMethod() As Long, _
    ByRef entryCompSize() As Long, _
    ByRef entryUncompSize() As Long, _
    ByRef entryLocalOff() As Long, _
    ByVal entryCount As Long, _
    ByVal targetName As String, _
    ByRef outText As String _
) As Boolean
    Dim idx As Long, foundIdx As Long
    Dim lhOff As Long, lhNameLen As Long, lhExtraLen As Long, dataStart As Long
    Dim compMethod As Long, compSize As Long, uncompSize As Long
    Dim outBytes() As Byte, outLen As Long, k As Long

    foundIdx = -1
    For idx = 0 To entryCount - 1
        If StrComp(entryNames(idx), targetName, vbTextCompare) = 0 Then
            foundIdx = idx
            Exit For
        End If
    Next idx
    If foundIdx < 0 Then Exit Function

    lhOff = entryLocalOff(foundIdx)
    If lhOff + 30 > UBound(b) + 1 Then Exit Function
    If b(lhOff) <> &H50 Or b(lhOff + 1) <> &H4B Or b(lhOff + 2) <> &H3 Or b(lhOff + 3) <> &H4 Then Exit Function

    lhNameLen = ReadUInt16(b, lhOff + 26)
    lhExtraLen = ReadUInt16(b, lhOff + 28)
    dataStart = lhOff + 30 + lhNameLen + lhExtraLen

    compMethod = entryMethod(foundIdx)
    compSize = entryCompSize(foundIdx)
    uncompSize = entryUncompSize(foundIdx)

    If compSize = 0 And uncompSize = 0 Then
        outText = ""
        ExtractZipText = True
        Exit Function
    End If
    If dataStart + compSize > UBound(b) + 1 Then Exit Function

    If compMethod = 0 Then
        ReDim outBytes(0 To compSize - 1)
        For k = 0 To compSize - 1
            outBytes(k) = b(dataStart + k)
        Next k
        outText = Utf8BytesToString(outBytes, compSize)
        ExtractZipText = True
    ElseIf compMethod = 8 Then
        If InflateBytes(b, dataStart, compSize, uncompSize, outBytes, outLen) Then
            outText = Utf8BytesToString(outBytes, outLen)
            ExtractZipText = True
        End If
    End If
End Function

Private Function ReadBits( _
    ByRef inBuf() As Byte, _
    ByRef inPos As Long, _
    ByVal endPos As Long, _
    ByRef bitBuf As Long, _
    ByRef bitCnt As Long, _
    ByVal need As Long _
) As Long
    If need = 0 Then
        ReadBits = 0
        Exit Function
    End If
    Do While bitCnt < need
        If inPos >= endPos Then
            Err.Raise vbObjectError + 1001, , "Deflate EOF"
        End If
        bitBuf = bitBuf + CLng(inBuf(inPos)) * mPow2(bitCnt)
        inPos = inPos + 1
        bitCnt = bitCnt + 8
    Loop
    ReadBits = bitBuf And (mPow2(need) - 1&)
    bitBuf = bitBuf \ mPow2(need)
    bitCnt = bitCnt - need
End Function

Private Function ConstructHuffman( _
    ByRef lengths() As Long, _
    ByVal startIdx As Long, _
    ByVal numCodes As Long, _
    ByRef count() As Long, _
    ByRef symbol() As Long _
) As Boolean
    Const MAXBITS As Long = 15
    Dim offs(1 To MAXBITS) As Long
    Dim i As Long, leftVal As Long, sym As Long, ln As Long

    ReDim count(0 To MAXBITS)
    ReDim symbol(0 To numCodes - 1)

    For sym = 0 To numCodes - 1
        count(lengths(startIdx + sym)) = count(lengths(startIdx + sym)) + 1
    Next sym

    If count(0) = numCodes Then
        ConstructHuffman = True
        Exit Function
    End If

    leftVal = 1
    For i = 1 To MAXBITS
        leftVal = leftVal * 2 - count(i)
        If leftVal < 0 Then Exit Function
    Next i

    offs(1) = 0
    For i = 1 To MAXBITS - 1
        offs(i + 1) = offs(i) + count(i)
    Next i

    For sym = 0 To numCodes - 1
        ln = lengths(startIdx + sym)
        If ln <> 0 Then
            symbol(offs(ln)) = sym
            offs(ln) = offs(ln) + 1
        End If
    Next sym

    ConstructHuffman = True
End Function

Private Function DecodeHuffman( _
    ByRef inBuf() As Byte, _
    ByRef inPos As Long, _
    ByVal endPos As Long, _
    ByRef bitBuf As Long, _
    ByRef bitCnt As Long, _
    ByRef count() As Long, _
    ByRef symbol() As Long _
) As Long
    Const MAXBITS As Long = 15
    Dim ln As Long, code As Long, first As Long, index As Long, cnt As Long

    code = 0
    first = 0
    index = 0
    For ln = 1 To MAXBITS
        code = code Or ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 1)
        cnt = count(ln)
        If code - cnt < first Then
            DecodeHuffman = symbol(index + (code - first))
            Exit Function
        End If
        index = index + cnt
        first = (first + cnt) * 2
        code = code * 2
    Next ln

    Err.Raise vbObjectError + 1002, , "Invalid Huffman code"
End Function

Private Function InflateBytes( _
    ByRef inBuf() As Byte, _
    ByVal startOff As Long, _
    ByVal compSize As Long, _
    ByVal expectedSize As Long, _
    ByRef outBuf() As Byte, _
    ByRef outLen As Long _
) As Boolean
    On Error GoTo ErrHandler
    EnsurePow2

    Dim lenBase(0 To 28) As Long, lenExt(0 To 28) As Long
    Dim distBase(0 To 29) As Long, distExt(0 To 29) As Long
    Dim clOrder(0 To 18) As Long
    InitDeflateTables lenBase, lenExt, distBase, distExt, clOrder

    Dim outCap As Long, outPos As Long
    outCap = expectedSize
    If outCap < 1024 Then outCap = 1024
    ReDim outBuf(0 To outCap - 1)
    outPos = 0

    Dim inPos As Long, endPos As Long, bitBuf As Long, bitCnt As Long
    inPos = startOff
    endPos = startOff + compSize
    bitBuf = 0
    bitCnt = 0

    Dim bFinal As Long, bType As Long
    Dim litCnt() As Long, litSym() As Long
    Dim distCnt() As Long, distSym() As Long
    Dim fixBuilt As Boolean
    Dim fixLitCnt() As Long, fixLitSym() As Long
    Dim fixDistCnt() As Long, fixDistSym() As Long
    Dim fLengths(0 To 319) As Long
    Dim sLen As Long, nLenVal As Long, j As Long, k As Long
    Dim nlen As Long, ndist As Long, ncode As Long
    Dim clLens(0 To 18) As Long, clCnt() As Long, clSym() As Long
    Dim allLens() As Long, idx As Long, symVal As Long, repLen As Long, repCount As Long
    Dim sym As Long, cpLen As Long, cpDist As Long, dSym As Long, cIdx As Long

    fixBuilt = False

    Do
        bFinal = ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 1)
        bType = ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 2)

        If bType = 0 Then
            ' Stored (비압축) 블록
            bitBuf = 0
            bitCnt = 0
            If inPos + 4 > endPos Then GoTo ErrHandler
            sLen = ReadUInt16(inBuf, inPos)
            nLenVal = ReadUInt16(inBuf, inPos + 2)
            inPos = inPos + 4
            If (sLen Xor &HFFFF&) <> nLenVal Then GoTo ErrHandler
            If inPos + sLen > endPos Then GoTo ErrHandler
            If outPos + sLen > outCap Then
                Do While outPos + sLen > outCap
                    outCap = outCap * 2
                Loop
                ReDim Preserve outBuf(0 To outCap - 1)
            End If
            For j = 0 To sLen - 1
                outBuf(outPos + j) = inBuf(inPos + j)
            Next j
            inPos = inPos + sLen
            outPos = outPos + sLen
        Else
            If bType = 1 Then
                ' Fixed Huffman 블록
                If Not fixBuilt Then
                    For k = 0 To 143: fLengths(k) = 8: Next k
                    For k = 144 To 255: fLengths(k) = 9: Next k
                    For k = 256 To 279: fLengths(k) = 7: Next k
                    For k = 280 To 287: fLengths(k) = 8: Next k
                    For k = 288 To 319: fLengths(k) = 5: Next k
                    If Not ConstructHuffman(fLengths, 0, 288, fixLitCnt, fixLitSym) Then GoTo ErrHandler
                    If Not ConstructHuffman(fLengths, 288, 32, fixDistCnt, fixDistSym) Then GoTo ErrHandler
                    fixBuilt = True
                End If
                litCnt = fixLitCnt: litSym = fixLitSym
                distCnt = fixDistCnt: distSym = fixDistSym
            ElseIf bType = 2 Then
                ' Dynamic Huffman 블록
                nlen = ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 5) + 257
                ndist = ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 5) + 1
                ncode = ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 4) + 4
                If nlen > 286 Or ndist > 30 Then GoTo ErrHandler

                For k = 0 To ncode - 1
                    clLens(clOrder(k)) = ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 3)
                Next k
                For k = ncode To 18
                    clLens(clOrder(k)) = 0
                Next k

                If Not ConstructHuffman(clLens, 0, 19, clCnt, clSym) Then GoTo ErrHandler

                ReDim allLens(0 To nlen + ndist - 1)
                idx = 0
                Do While idx < nlen + ndist
                    symVal = DecodeHuffman(inBuf, inPos, endPos, bitBuf, bitCnt, clCnt, clSym)
                    If symVal < 16 Then
                        allLens(idx) = symVal
                        idx = idx + 1
                    Else
                        repLen = 0
                        If symVal = 16 Then
                            If idx = 0 Then GoTo ErrHandler
                            repLen = allLens(idx - 1)
                            repCount = 3 + ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 2)
                        ElseIf symVal = 17 Then
                            repCount = 3 + ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 3)
                        Else
                            repCount = 11 + ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, 7)
                        End If
                        If idx + repCount > nlen + ndist Then GoTo ErrHandler
                        For k = 1 To repCount
                            allLens(idx) = repLen
                            idx = idx + 1
                        Next k
                    End If
                Loop

                If Not ConstructHuffman(allLens, 0, nlen, litCnt, litSym) Then GoTo ErrHandler
                If Not ConstructHuffman(allLens, nlen, ndist, distCnt, distSym) Then GoTo ErrHandler
            Else
                GoTo ErrHandler
            End If

            ' 압축 데이터 심볼 디코딩
            Do
                sym = DecodeHuffman(inBuf, inPos, endPos, bitBuf, bitCnt, litCnt, litSym)
                If sym < 256 Then
                    If outPos >= outCap Then
                        outCap = outCap * 2
                        ReDim Preserve outBuf(0 To outCap - 1)
                    End If
                    outBuf(outPos) = CByte(sym)
                    outPos = outPos + 1
                ElseIf sym = 256 Then
                    Exit Do
                Else
                    sym = sym - 257
                    If sym > 28 Then GoTo ErrHandler
                    cpLen = lenBase(sym) + ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, lenExt(sym))

                    dSym = DecodeHuffman(inBuf, inPos, endPos, bitBuf, bitCnt, distCnt, distSym)
                    If dSym > 29 Then GoTo ErrHandler
                    cpDist = distBase(dSym) + ReadBits(inBuf, inPos, endPos, bitBuf, bitCnt, distExt(dSym))
                    If cpDist > outPos Then GoTo ErrHandler

                    If outPos + cpLen > outCap Then
                        Do While outPos + cpLen > outCap
                            outCap = outCap * 2
                        Loop
                        ReDim Preserve outBuf(0 To outCap - 1)
                    End If
                    For cIdx = 0 To cpLen - 1
                        outBuf(outPos) = outBuf(outPos - cpDist)
                        outPos = outPos + 1
                    Next cIdx
                End If
            Loop
        End If
    Loop While bFinal = 0

    outLen = outPos
    If outPos = 0 Then
        ReDim outBuf(0 To 0)
    ElseIf outPos - 1 <> UBound(outBuf) Then
        ReDim Preserve outBuf(0 To outPos - 1)
    End If
    InflateBytes = True
    Exit Function

ErrHandler:
    outLen = 0
    InflateBytes = False
End Function

Private Sub InitDeflateTables( _
    ByRef lenBase() As Long, _
    ByRef lenExt() As Long, _
    ByRef distBase() As Long, _
    ByRef distExt() As Long, _
    ByRef clOrder() As Long _
)
    Dim vLenBase As Variant, vLenExt As Variant
    Dim vDistBase As Variant, vDistExt As Variant
    Dim vClOrder As Variant, i As Long

    vLenBase = Array(3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131, 163, 195, 227, 258)
    vLenExt = Array(0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0)
    vDistBase = Array(1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537, 2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577)
    vDistExt = Array(0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13)
    vClOrder = Array(16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15)

    For i = 0 To 28
        lenBase(i) = CLng(vLenBase(i))
        lenExt(i) = CLng(vLenExt(i))
    Next i
    For i = 0 To 29
        distBase(i) = CLng(vDistBase(i))
        distExt(i) = CLng(vDistExt(i))
    Next i
    For i = 0 To 18
        clOrder(i) = CLng(vClOrder(i))
    Next i
End Sub
'''

os.makedirs(BASE_DIR, exist_ok=True)
with open(BAS_PATH, "w", encoding="cp949", newline="\r\n") as f:
    f.write(VBA_CODE.lstrip("\n"))

print(f"Wrote {BAS_PATH} ({os.path.getsize(BAS_PATH)} bytes)")
