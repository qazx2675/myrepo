#!/usr/bin/env bash
# cron_wrapper.sh — crontab 등록용 진입점.
#
# cron 의 day-of-month 필드는 매월 1일 기준으로 리셋되어 "*/85" 로는 정확히 85일 주기를
# 표현할 수 없다. 그래서 이 스크립트는 매일 실행되도록 등록하고(예: 0 3 * * *),
# 내부적으로 마지막 "성공적으로 전체 완료한" 실행 이후 85일이 지났는지 직접 확인해서
# 지났을 때만 실제 갱신을 수행한다. 일부 vCenter만 실패한 경우에는 기준일을 갱신하지 않아
# 다음날 다시 시도한다(전체 성공 전까지 재시도).
#
# 사용 전 준비:
#   1) ./admin_password.secret 파일에 ADMIN_PASSWORD 값만 한 줄로 저장하고 chmod 600 (git 제외 대상)
#   2) crontab -e 에 아래처럼 등록 (매일 새벽 3시 체크):
#        0 3 * * * /경로/vc_password_update/cron_wrapper.sh >> /경로/vc_password_update/cron.log 2>&1
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE"

INTERVAL_DAYS=85
STAMP_FILE="$HERE/.last_success"
SECRET_FILE="$HERE/admin_password.secret"
DIR="${VC_SECRET_DIR:-$HERE/secret}"
VC_LIST="${VC_LIST_FILE:-$HERE/vcenter.txt}"

ts() { date '+%F %T'; }

if [ -s "$STAMP_FILE" ]; then
  last=$(cat "$STAMP_FILE")
  now=$(date +%s)
  elapsed_days=$(( (now - last) / 86400 ))
  if [ "$elapsed_days" -lt "$INTERVAL_DAYS" ]; then
    echo "[$(ts)] 스킵: 마지막 성공 후 ${elapsed_days}일 경과 (${INTERVAL_DAYS}일 되어야 실행)"
    exit 0
  fi
fi

if [ ! -x ./vc_password_update ]; then
  echo "[$(ts)] 바이너리가 없어 먼저 빌드합니다"
  ./setup.sh
fi

if [ ! -r "$SECRET_FILE" ]; then
  echo "[$(ts)] [오류] $SECRET_FILE 이 없습니다 (admin 비밀번호를 한 줄로 저장, chmod 600)" >&2
  exit 1
fi
ADMIN_PASSWORD="$(head -n1 "$SECRET_FILE")"
export ADMIN_PASSWORD

echo "[$(ts)] 실행: -dir $DIR -vc $VC_LIST"
if ./vc_password_update -dir "$DIR" -vc "$VC_LIST"; then
  date +%s > "$STAMP_FILE"
  echo "[$(ts)] 전체 성공 — 다음 실행 기준일 갱신"
else
  echo "[$(ts)] 일부 또는 전체 실패 — 기준일 갱신 안 함(내일 다시 시도)"
  exit 1
fi
