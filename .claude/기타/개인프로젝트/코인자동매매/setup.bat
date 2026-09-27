@echo off
chcp 65001 >nul
cd /d "%~dp0"

py -m venv .venv
.venv\Scripts\python -m pip install --upgrade pip
.venv\Scripts\python -m pip install -r requirements.txt

if not exist .env (
    copy .env.example .env >nul
    echo.
    echo [생성] .env  -  빗썸 "조회 전용" API 키를 채우세요.
)

echo.
echo 준비 완료.  run_m1.bat          (공개 조회만)
echo             run_m1.bat --private (.env 키로 잔고 + 권한 검증)
pause
