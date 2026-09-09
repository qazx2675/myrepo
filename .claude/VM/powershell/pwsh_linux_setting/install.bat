@echo off
chcp 65001 > nul
title PowerShell Linux 환경 원클릭 설치기
echo ========================================================
echo   PowerShell Linux-like Environment Installer (폐쇄망용)
echo ========================================================
echo.

set SCRIPT_DIR=%~dp0
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%install.ps1"

echo.
pause
