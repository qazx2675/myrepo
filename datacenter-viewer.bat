@echo off
chcp 65001 > nul
setlocal
cd /d "%~dp0"
title 데이터센터 3D 뷰어

echo ===============================================================================
echo [데이터센터 3D 렌더러 및 입고 시뮬레이션 시스템]
echo ===============================================================================
echo.

rem 1) 네이티브 Go 실행 파일 실행 시도
if exist "datacenter-viewer.exe" (
    echo [1/2] 뷰어 실행 파일 확인 중...
    start "" "datacenter-viewer.exe" 2>nul
    if not errorlevel 1 goto :done
)

rem 2) Windows 스마트 앱 컨트롤 차단 시: 설치된 Go 엔진을 통한 안전 실행
set "GO_EXE=C:\Program Files\Go\bin\go.exe"
where go >nul 2>nul && set "GO_EXE=go"

if exist "%GO_EXE%" (
    echo [스마트 앱 컨트롤 우회 모드] Go 런처를 통해 3D 뷰어를 안전하게 시작합니다...
    "%GO_EXE%" run ./cmd/datacenter-viewer
    goto :done
)

rem 3) Node.js 기반 폴백 실행
where node >nul 2>nul
if not errorlevel 1 (
    echo [폴백 모드] Node.js를 통해 3D 뷰어를 시작합니다...
    start /b "" node "server.js" > nul 2>&1
    timeout /t 1 > nul
    start "" "http://localhost:5500/index.html"
    goto :done
)

echo [오류] 실행 가능한 런처를 찾을 수 없습니다.
pause

:done
endlocal
