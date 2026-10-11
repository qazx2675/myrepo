@echo off
title 업무프로세스 ntfy 챗봇 - 수동 테스트
chcp 65001 >nul
cd /d "%~dp0"

set "SCRIPT=%~dp0수동테스트.py"
if not exist "%SCRIPT%" set "SCRIPT=%~dp0bot\수동테스트.py"

set "PYCMD=python"
where py >nul 2>nul
if %errorlevel% equ 0 (
    set "PYCMD=py"
) else if exist "%LOCALAPPDATA%\Programs\Python\Launcher\py.exe" (
    set "PYCMD=%LOCALAPPDATA%\Programs\Python\Launcher\py.exe"
)

REM 인자가 있으면 그대로 전달 (예: [6] 수동테스트.bat send 사용자1 long)
if not "%~1"=="" (
    "%PYCMD%" "%SCRIPT%" %*
    pause
    exit /b
)

REM 인자가 없으면 절차를 먼저 보여 주고 메뉴를 띄운다
"%PYCMD%" "%SCRIPT%" show

:menu
echo.
echo   1. 절차 다시 보기
echo   2. 케이스 전송 (실제로 ntfy 로 전송)
echo   0. 종료
set "SEL="
set /p SEL=선택: 
if "%SEL%"=="1" (
    "%PYCMD%" "%SCRIPT%" show
    goto menu
)
if "%SEL%"=="2" (
    "%PYCMD%" "%SCRIPT%" list
    set "UNAME="
    set "UCASE="
    set /p UNAME=사용자 이름 ^(users.json 의 이름^): 
    set /p UCASE=케이스 이름: 
    "%PYCMD%" "%SCRIPT%" send "%UNAME%" "%UCASE%"
    goto menu
)
if "%SEL%"=="0" exit /b
goto menu
