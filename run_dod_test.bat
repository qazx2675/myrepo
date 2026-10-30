@echo off
chcp 65001 > nul
echo ================================================================================
echo   SOP 자동 생성 프로그램 DoD v2.0 원클릭 One-Shot 자동 검증 테스트 실행기
echo ================================================================================
cd /d "%~dp0"
go run -mod=vendor test_dod.go
if %errorlevel% equ 0 (
    echo.
    echo [최종 완료] DoD v2.0 전체 검증 항목 PASS!
) else (
    echo.
    echo [오류 발생] 검증 실패 항목을 확인하세요.
)
pause
