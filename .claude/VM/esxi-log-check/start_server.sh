#!/bin/bash
cd "$(dirname "$0")"

# 로컬 테스트 모드를 위한 가짜 로그 파일 생성 (없을 경우를 대비)
mkdir -p /var/log/ipmi
touch /var/log/vmkernel.log /var/log/vobd.log /var/log/ipmi/sel

nohup ./esxi-log-check -server :12345 -input vmkernel.log=/var/log/vmkernel.log -input vobd.log=/var/log/vobd.log -input ipmi_sel=/var/log/ipmi/sel > server.log 2>&1 &
echo "[INFO] ESXi 로그 점검 웹 서버가 백그라운드에서 시작되었습니다 (PID: $!)."
echo "[INFO] 접속 주소: http://192.168.0.58:12345"
echo "[INFO] 로컬 테스트 모드로 실행되었습니다 (gossh 미사용)."
echo "[INFO] 웹 서버 로그는 server.log에서 확인할 수 있습니다."
