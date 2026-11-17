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
rem -H windowsgui: GUI subsystem, no cmd window. --cli output: awx_fetch.log (UTF-8) and parent console.
go build -trimpath -ldflags "-s -w -H windowsgui" -o dist\awx_fetch.exe . || goto :err
echo built: dist\awx_fetch.exe
exit /b 0
:err
echo build failed
exit /b 1
