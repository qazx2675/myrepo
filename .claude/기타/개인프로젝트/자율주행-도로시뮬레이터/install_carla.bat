@echo off
REM install_carla.bat - CARLA 0.9.16 자동 설치 (기본 D:\CARLA_0.9.16). 옵션은 install_carla.ps1 참고.
chcp 65001 >nul
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install_carla.ps1" %*
