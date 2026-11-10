@echo off
cd /d "%~dp0"
echo ========================================================
echo   Host Cross-Checker Agent (18080)
echo   Listening on 127.0.0.1:18080
echo   Working Directory: %~dp0
echo ========================================================
cross-checker-agent.exe -port=18080 -share="%~dp0"
pause

