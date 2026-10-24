@echo off
chcp 65001 > nul
setlocal
cd /d "%~dp0"
title 데이터센터 3D 뷰어 - 초기 상태 리셋

echo ===============================================================================
echo [데이터센터 3D 렌더러 - 초기 랙 구성으로 리셋]
echo ===============================================================================
echo.
echo 현재 data/ 폴더의 기존 15,000대 데이터를 data\backup_15000\ 폴더에 안전하게 백업합니다.
echo.

if not exist "data\backup_15000" mkdir "data\backup_15000"
if exist "data\datacenter_layout.json" copy /y "data\datacenter_layout.json" "data\backup_15000\" > nul
if exist "data\allocated_hosts.csv" copy /y "data\allocated_hosts.csv" "data\backup_15000\" > nul

echo [1/2] 32대 기본 랙 레이아웃(X001~X003, X029)으로 초기화 중...
copy /y "sample_data\datacenter_layout_initial.json" "data\datacenter_layout.json" > nul

echo [2/2] 서버 할당 내역 초기화(빈 랙 상태) 완료...
(
    echo 센터이름,모델코드,랙라벨,hostname,몇U,수동지정U
) > "data\allocated_hosts.csv"

echo.
echo ===============================================================================
echo [완료] 초기 랙 구성(초반 상태: 32대 빈 랙)으로 성공적으로 리셋되었습니다!
echo.
echo 이제 'datacenter-viewer.bat' 또는 바로가기를 실행하신 후,
echo 화면 우측 상단의 [📄 CSV 자동 할당] 버튼을 눌러 준비하신 CSV를 직접 넣어보세요!
echo ===============================================================================
echo.
pause
endlocal
