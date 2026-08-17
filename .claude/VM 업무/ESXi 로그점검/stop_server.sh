#!/bin/bash
echo "[INFO] ESXi 로그 점검 웹 서버를 종료합니다..."
pkill -f "esxi-log-check -server"
echo "[INFO] 서버가 종료되었습니다."
