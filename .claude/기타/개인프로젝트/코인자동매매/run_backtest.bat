@echo off
chcp 65001 >nul
cd /d "%~dp0"
if not exist .venv\Scripts\python.exe (echo setup.bat 을 먼저 실행하세요 & pause & exit /b 1)
.venv\Scripts\python -m src.run_backtest %*
pause
