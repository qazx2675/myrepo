@echo off
cd /d "%~dp0"
echo Starting AWX Host Cross-Checker Desktop App...
start "" "host-cross-checker.exe" -share="%~dp0"
