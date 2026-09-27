#!/usr/bin/env bash
# run.sh — vc_password_update 실행 편의 스크립트.
# 바이너리가 없으면 setup.sh 로 자동 빌드하고, 인자를 그대로 vc_password_update 에 전달한다.
# ADMIN_PASSWORD 가 비어 있고 터미널이 연결되어 있으면 물어본다(크론에서는 미리 export 해둘 것 — cron_wrapper.sh 참고).
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE"

if [ ! -x ./vc_password_update ]; then
  echo "[INFO] 바이너리가 없어 먼저 빌드합니다 (setup.sh)"
  ./setup.sh
fi

if [ -z "${ADMIN_PASSWORD:-}" ]; then
  if [ -t 0 ]; then
    printf 'admin 계정(%s) 비밀번호: ' "${ADMIN_ID:-Administrator@vsphere.local}" >&2
    IFS= read -r -s ADMIN_PASSWORD || exit 1
    echo >&2
    export ADMIN_PASSWORD
  else
    echo "[오류] 환경변수 ADMIN_PASSWORD 가 필요합니다 (비대화형 실행: crontab 은 cron_wrapper.sh 사용)." >&2
    exit 1
  fi
fi

exec ./vc_password_update "$@"
