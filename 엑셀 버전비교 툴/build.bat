@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags "-s -w -H windowsgui" -o "엑셀버전비교.exe" .
if errorlevel 1 (
  echo 빌드 실패
  exit /b 1
)
echo 빌드 완료: 엑셀버전비교.exe
