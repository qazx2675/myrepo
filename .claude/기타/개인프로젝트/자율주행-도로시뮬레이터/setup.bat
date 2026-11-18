@echo off
REM setup.bat - Windows 폐쇄망(오프라인) 설치. wheels\ 안의 패키지만 사용, 인터넷 접속 시도 안 함.
cd /d "%~dp0"
set PY=py -3.11
%PY% -c "import sys" 2>nul || set PY=python
%PY% -m venv .venv || (echo 오류: Python 3.11 을 먼저 설치하십시오. & exit /b 2)
.venv\Scripts\pip install --no-index --find-links wheels -r requirements.txt || exit /b 1
.venv\Scripts\python main.py --headless --seconds 60
echo.
echo 완료. 다음으로 진행하십시오:
echo   1) .venv\Scripts\python main.py                 (창 띄워서 운전)
echo   2) .venv\Scripts\python tests\test_sim.py       (기능 테스트)
