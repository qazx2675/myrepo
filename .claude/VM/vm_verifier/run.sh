#!/usr/bin/env bash
# run.sh — vm-verifier 실행 편의 스크립트. 빌드 + VC_USER/VC_PASS 입력 + 옵션 입력을 대화형으로 처리한다.
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -x ./vm-verifier ]; then
    echo "[INFO] 바이너리가 없어 먼저 빌드합니다..."
    ./setup.sh
fi

if [ -z "${VC_USER:-}" ]; then
    read -p "VC_USER: " VC_USER
fi
if [ -z "${VC_PASS:-}" ]; then
    read -s -p "VC_PASS: " VC_PASS
    echo
fi
export VC_USER VC_PASS

read -p "vCenter 목록 파일 [vcenter.txt]: " VCENTER_LIST
VCENTER_LIST=${VCENTER_LIST:-vcenter.txt}
read -p "검증 대상 BM 접두어 목록 파일: " TARGETS
read -p "DHCP 파일 루트 경로 [/user/caedhcp]: " DHCP_ROOT
DHCP_ROOT=${DHCP_ROOT:-/user/caedhcp}

./vm-verifier -vcenterList "$VCENTER_LIST" -f "$TARGETS" -dhcp-root "$DHCP_ROOT"
