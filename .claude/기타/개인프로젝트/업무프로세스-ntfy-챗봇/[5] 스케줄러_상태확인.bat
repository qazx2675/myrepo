@echo off
title 챗봇 및 스케줄러 상태 점검
chcp 65001 >nul
cd /d "D:\업무프로세스"

echo ============================================================
echo   [챗봇 및 스케줄러 현재 상태 점검]
echo ============================================================
where py >nul 2>nul
if %errorlevel% equ 0 (
    py scheduler\manage_service.py status
) else (
    "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" scheduler\manage_service.py status
)

echo.
echo ============================================================
echo   [Windows 작업 스케줄러 등록 내역]
echo ============================================================
schtasks /query /tn "NtfyChatbot_WorkdayStart" /fo LIST | findstr /i "TaskName Status Next"
schtasks /query /tn "NtfyChatbot_WorkdayStop" /fo LIST | findstr /i "TaskName Status Next"
echo.
pause
