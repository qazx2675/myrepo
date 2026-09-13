@echo off
REM build.bat - builds the Windows widget (copy-widget.exe).
REM Requires the Go toolchain: https://go.dev/dl/ (windows/amd64 installer).
setlocal
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 goto nogo

REM go.mod requires a lower version than this installed toolchain, so no
REM auto-download is needed; pin to local anyway in case that ever changes.
set GOTOOLCHAIN=local

if not exist dist mkdir dist

echo Building copy-widget.exe ...
go build -o dist\copy-widget.exe .\cmd\copy-widget
if errorlevel 1 goto buildfail

echo Done: dist\copy-widget.exe
echo.
echo Next steps:
echo   1. Copy conf\copy_setting.conf.sample to conf\copy_setting.conf and fill in real values
echo      (must match the conf\copy_setting.conf used on the ETX side)
echo   2. Run dist\copy-widget.exe
goto end

:nogo
echo ERROR: 'go' command not found. Install Go from https://go.dev/dl/ first.
exit /b 2

:buildfail
echo Build failed.
exit /b 1

:end
endlocal
