@echo off
REM run_carla.bat - CARLA 서버가 꺼져 있으면 켜고, 시뮬레이터(carla_sim\run.py)를 실행. 인자는 run.py 로 전달.
REM   run_carla.bat --level long --cars 0
chcp 65001 >nul
cd /d "%~dp0"
if "%CARLA_ROOT%"=="" set CARLA_ROOT=D:\CARLA_0.9.16
if not exist "%CARLA_ROOT%\CarlaUE4.exe" (echo 오류: %CARLA_ROOT%\CarlaUE4.exe 가 없습니다. install_carla.bat 을 먼저 실행하십시오. & exit /b 1)
tasklist /fi "imagename eq CarlaUE4-Win64-Shipping.exe" | find /i "CarlaUE4" >nul
if errorlevel 1 (
  echo CARLA 서버 시작 중... 처음엔 1~2분 걸립니다.
  REM -RenderOffScreen: 서버 창 없이 뒤에서만 렌더링 (게임 창 하나만 보임). 끄기: stop_carla.bat
  start "" "%CARLA_ROOT%\CarlaUE4.exe" -quality-level=Epic -RenderOffScreen
  timeout /t 40 /nobreak >nul
)
.venv\Scripts\python carla_sim\run.py %*
