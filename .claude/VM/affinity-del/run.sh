#!/usr/bin/env bash
# run.sh — list.txt 의 VM 에서 sched.vcpuN.affinity 삭제.
# 사용: ./run.sh -vc 192.168.0.50 [-dry-run] [-id ID] [-list list.txt]
# 비밀번호는 VC_PW 환경변수, 없으면 프롬프트로 입력.
set -euo pipefail
cd "$(dirname "$0")"

if [ ! -x ./affinity_del ]; then
  echo "빌드 중 (vendor 사용, 오프라인)..."
  go build -mod=vendor -o affinity_del main.go
fi

if [ -z "${VC_PW:-}" ]; then
  read -r -s -p "vCenter 비밀번호: " VC_PW
  echo
  export VC_PW
fi

exec ./affinity_del "$@"
