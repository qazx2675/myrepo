@echo off
chcp 65001 > nul
title 데이터센터 3D 렌더러 및 입고 시뮬레이션 시스템

echo ===============================================================================
echo [데이터센터 3D 렌더러 및 입고 시뮬레이션 시스템 실행기]
echo.
echo * 별도의 Node.js 또는 빌드 도구 설치 없이 즉시 실행됩니다.
echo * 기본 웹 브라우저(Edge, Chrome 등)로 3D 뷰어가 열립니다.
echo ===============================================================================
echo.

set SCRIPT_DIR=%~dp0
set INDEX_PATH=%SCRIPT_DIR%index.html

echo 웹 서버 실행 확인 중...
start /b "" node "%SCRIPT_DIR%server.js" > nul 2>&1

timeout /t 1 > nul
start "" "http://localhost:5500/index.html"

echo 브라우저에서 실행되었습니다.
echo 창을 닫으려면 아무 키나 누르세요.
pause > nul
