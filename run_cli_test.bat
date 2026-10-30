@echo off
chcp 65001 > nul
echo ========================================================
echo   SOP Generator v2 - CLI 즉시 테스트 실행
echo   - 샘플 target.xlsx 생성 (시트: 1_OS설치, 2_네트워크설정, 3_보안점검)
echo   - 시트 "3_보안점검,1_OS설치" 순서로 SOP_CLI_결과.xlsx 생성
echo ========================================================
"%~dp0sop-generator.exe" -cli -create-sample
"%~dp0sop-generator.exe" -cli -target=target.xlsx -sheets="3_보안점검,1_OS설치" -worker=홍길동 -verifier=김검증 -object=서버01 -out=SOP_CLI_결과.xlsx
echo.
pause
