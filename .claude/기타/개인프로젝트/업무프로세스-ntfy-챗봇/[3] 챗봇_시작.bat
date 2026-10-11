@echo off
title 업무프로세스 ntfy 챗봇
chcp 65001 >nul
cd /d "D:\업무프로세스"

echo ============================================================
echo   [1단계] 로컬 LLM (Ollama) 상태 확인 및 자동 가동
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
echo ============================================================
echo   [2단계] 챗봇 시작 및 워밍업 (휴대폰 실시간 대기)
echo ============================================================
where py >nul 2>nul
if %errorlevel% equ 0 (
    py -u bot\bot.py
) else if exist "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" (
    "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" -u bot\bot.py
) else (
    python -u bot\bot.py
)
pause
