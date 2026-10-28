@echo off
chcp 65001 > nul
echo ========================================================
echo   Excel Sheet Cloner - CLI 즉시 테스트 실행
echo   - 원본: target.xlsx (시트: test1)
echo   - 대상 셀: A3 (병합+이미지), A6 (2x2병합+이미지)
echo   - 신규: test2.xlsx (시트: test_sheet2)
echo ========================================================
"%~dp0excel-test-runner.exe" -cli -target=target.xlsx -src-sheet=test1 -dst-file=test2.xlsx -dst-sheet=test_sheet2 -cells="A3,A6"
echo.
pause
