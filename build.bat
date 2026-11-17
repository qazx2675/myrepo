@echo off
rem Windows build: dist\awx_fetch.exe (downloader). Same Go source as Linux.
setlocal
cd /d "%~dp0"
set CGO_ENABLED=0
if not exist dist mkdir dist
go version || goto :err
go vet ./... || goto :err
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags "-s -w" -o dist\awx_fetch.exe . || goto :err
echo built: dist\awx_fetch.exe
exit /b 0
:err
echo build failed
exit /b 1
