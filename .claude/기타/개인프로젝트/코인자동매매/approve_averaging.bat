@echo off
chcp 65001 >nul
cd /d "%~dp0"
if "%~1"=="" (echo 사용법: approve_averaging.bat KRW-XXX & pause & exit /b 1)
.venv\Scripts\python -W ignore -m src.averaging %1
pause
