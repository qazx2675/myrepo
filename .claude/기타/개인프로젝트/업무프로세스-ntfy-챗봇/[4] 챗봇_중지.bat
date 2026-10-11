@echo off
title 챗봇 프로세스 중지
chcp 65001 >nul
cd /d "D:\업무프로세스"

echo ============================================================
echo   [챗봇 중지] 실행 중인 챗봇 프로세스 종료 중...
echo ============================================================
where py >nul 2>nul
if %errorlevel% equ 0 (
    py scheduler\manage_service.py stop-bot
) else if exist "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" (
    "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" scheduler\manage_service.py stop-bot
) else (
    python scheduler\manage_service.py stop-bot
)

echo.
echo [완료] 챗봇 프로세스가 정상 종료되었습니다.
timeout /t 3 >nul
