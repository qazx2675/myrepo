@echo off
title 로컬 LLM 시작
chcp 65001 >nul
cd /d "D:\업무프로세스"

echo ============================================================
echo   [로컬 LLM 시작] 상태 확인 및 Ollama 서비스 가동 중...
echo ============================================================
where py >nul 2>nul
if %errorlevel% equ 0 (
    py scheduler\manage_service.py start-llm
) else if exist "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" (
    "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" scheduler\manage_service.py start-llm
) else (
    python scheduler\manage_service.py start-llm
)

echo.
echo [완료] 로컬 LLM 준비 완료 (qwen3:8b 사용 가능)
timeout /t 3 >nul
