#!/usr/bin/env bash
#
# 인프라망 판별 스크립트 (샘플)
#
#   실행 방식: survey 도구가
#       gossh -w <hosts> -script "bash <conf 의 infra_net 값>"
#   로 **조사 대상 호스트에서** 원격 실행한다. hostname 인자는 없다(각 호스트가 자기 자신을 본다).
#
#   출력: 한 줄. survey 는 gossh 의 "hostname: " 접두를 떼고
#         conf 의 infra_regex 로 값을 추출한다.
#
# ┌─────────────────────────────────────────────────────────────────┐
# │ 이 파일은 샘플입니다. 사내 인프라망 판별 규칙에 맞게 교체하고,       │
# │ 조사 대상 호스트에 배포한 뒤 conf 의 infra_net 을 그 경로로 지정.    │
# └─────────────────────────────────────────────────────────────────┘
#
set -euo pipefail

# --- 예시: 기본 라우트 인터페이스의 IP 대역으로 판별 ---
ip="$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | head -n1)"

case "$ip" in
  10.10.*)  echo "INFO 업무망" ;;
  10.20.*)  echo "INFO DB망" ;;
  172.16.*) echo "INFO DMZ망" ;;
  *)        echo "INFO 미확인" ;;
esac
# conf 예시: infra_regex = 'INFO\s+(.+)'  ->  "업무망" 등이 인프라망 값이 됨
