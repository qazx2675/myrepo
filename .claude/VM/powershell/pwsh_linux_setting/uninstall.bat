@echo off
chcp 65001 > nul
title PowerShell Linux 환경 제거기
echo ========================================================
echo   PowerShell Linux-like Environment Uninstaller
echo ========================================================
echo.

set SCRIPT_DIR=%~dp0
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%SCRIPT_DIR%uninstall.ps1"

echo.
pause
