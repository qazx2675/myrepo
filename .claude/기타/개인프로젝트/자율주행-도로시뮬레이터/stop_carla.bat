@echo off
REM stop_carla.bat - 화면 없이 돌고 있는 CARLA 서버를 끈다 (GPU 메모리 반환).
chcp 65001 >nul
taskkill /f /im CarlaUE4-Win64-Shipping.exe >nul 2>&1 && echo CARLA 서버를 껐습니다. || echo 실행 중인 CARLA 서버가 없습니다.
taskkill /f /im CarlaUE4.exe >nul 2>&1
