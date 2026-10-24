@echo off
setlocal
rem Offline build: copy frontend into internal\webui\dist, run tests, build exe.
rem Usage: build-windows.bat [console]   (console = keep console window for debugging)
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
    if exist "C:\Program Files\Go\bin\go.exe" (
        set "PATH=C:\Program Files\Go\bin;%PATH%"
    ) else (
        echo [ERROR] Go is not installed. Install Go 1.21+ from https://go.dev/dl/
        exit /b 1
    )
)

if exist "frontend_vendor\three.min.js" (
    if not exist "vendor" mkdir "vendor"
    copy /y "frontend_vendor\*.js" "vendor\" >nul
)

set DIST=internal\webui\dist
if exist "%DIST%" rmdir /s /q "%DIST%"
mkdir "%DIST%"
mkdir "%DIST%\vendor"
copy /y index.html "%DIST%\" >nul
xcopy /e /i /y css "%DIST%\css" >nul
xcopy /e /i /y js "%DIST%\js" >nul
copy /y "vendor\*.js" "%DIST%\vendor\" >nul
if not exist "%DIST%\vendor\three.min.js" (
    echo [ERROR] vendor\three.min.js is missing!
    exit /b 1
)
if exist sample_data xcopy /e /i /y sample_data "%DIST%\sample_data" >nul

set CGO_ENABLED=0
set GOFLAGS=-mod=mod

set LDFLAGS=-s -w -H=windowsgui
if /i "%1"=="console" set LDFLAGS=-s -w

echo [TEST] Running tests and generating resource syso...
go test ./cmd/datacenter-viewer || exit /b 1
go test ./... || exit /b 1

echo [BUILD] Compiling datacenter-viewer.exe with icon...
go build -ldflags "%LDFLAGS%" -o datacenter-viewer.exe ./cmd/datacenter-viewer || exit /b 1

echo [DONE] datacenter-viewer.exe created successfully with embedded icon.
endlocal
