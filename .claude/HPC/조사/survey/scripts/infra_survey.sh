#!/usr/bin/env bash
#
# 인프라망 판별 스크립트 (샘플)
#
#   입력: $1 = hostname
#   출력: stdout 첫 줄 = 인프라망 이름 (예: "업무망")
#
# ┌─────────────────────────────────────────────────────────────────┐
# │ 이 파일은 샘플입니다. 사내 인프라망 판별 규칙에 맞게 교체하십시오.  │
# │ conf.toml 의 [scripts].infra_net 이 이 파일 경로를 가리킵니다.     │
# └─────────────────────────────────────────────────────────────────┘
#
set -euo pipefail

host="${1:?hostname 인자가 필요합니다}"

# --- 예시 구현: hostname 으로 SSH 접속해 주 IP 대역으로 판별 ---
ip="$(ssh -o BatchMode=yes -o ConnectTimeout=5 "$host" \
      "ip -4 -o addr show scope global | awk '{print \$4}' | head -n1" 2>/dev/null || true)"

case "$ip" in
  10.10.*)  echo "업무망" ;;
  10.20.*)  echo "DB망" ;;
  172.16.*) echo "DMZ망" ;;
  *)        echo "미확인" ;;
esac
