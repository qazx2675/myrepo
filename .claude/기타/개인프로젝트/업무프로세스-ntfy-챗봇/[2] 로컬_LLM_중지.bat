@echo off
title 로컬 LLM 중지
chcp 65001 >nul
cd /d "D:\업무프로세스"

echo ============================================================
echo   [로컬 LLM 중지] Ollama 및 GPU VRAM 메모리 해제 중...
echo ============================================================
where py >nul 2>nul
if %errorlevel% equ 0 (
    py scheduler\manage_service.py stop-llm
) else if exist "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" (
    "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" scheduler\manage_service.py stop-llm
) else (
    python scheduler\manage_service.py stop-llm
)

echo.
echo [완료] 로컬 LLM 및 GPU 메모리가 정상 해제되었습니다.
timeout /t 3 >nul
