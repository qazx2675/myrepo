@echo off
chcp 65001 > nul
title 업무프로세스 ntfy 챗봇
cd /d "%~dp0"

where py >nul 2>nul
if %errorlevel% equ 0 (
    py -u bot\bot.py
) else if exist "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" (
    "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" -u bot\bot.py
) else if exist "%LOCALAPPDATA%\Programs\Python\Python312\python.exe" (
    "%LOCALAPPDATA%\Programs\Python\Python312\python.exe" -u bot\bot.py
) else (
    python -u bot\bot.py
)
pause
