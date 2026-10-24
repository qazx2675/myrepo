#!/usr/bin/env bash
# cc_admin_pw.sh — 무작위 영문 대소문자+숫자 비밀번호를 stdout 에 한 줄로 낸다 (cross_check 관리자 admin 용).
#   bash cc_admin_pw.sh        # 16자
#   bash cc_admin_pw.sh 24     # 길이 지정 (8~128)
# /dev/urandom 만 쓴다 (폐쇄망·OS6 에서도 동작, 외부 명령 dd/tr/head 만 필요).
set -u
LEN="${1:-16}"
case "$LEN" in ''|*[!0-9]*) echo "[오류] 길이는 숫자여야 합니다: $LEN" >&2; exit 2 ;; esac
if [ "$LEN" -lt 8 ] || [ "$LEN" -gt 128 ]; then
  echo "[오류] 길이는 8~128 이어야 합니다: $LEN" >&2; exit 2
fi
[ -r /dev/urandom ] || { echo "[오류] /dev/urandom 을 읽을 수 없습니다" >&2; exit 1; }

PW=""
i=0
# 256바이트마다 평균 62자 정도 나오므로 보통 한 번에 끝난다. pipefail/SIGPIPE 를 피하려고 dd 로 끊어 읽는다.
while [ "${#PW}" -lt "$LEN" ] && [ "$i" -lt 50 ]; do
  PW="$PW$(dd if=/dev/urandom bs=256 count=1 2>/dev/null | LC_ALL=C tr -dc 'A-Za-z0-9')"
  i=$((i + 1))
done
if [ "${#PW}" -lt "$LEN" ]; then
  echo "[오류] 무작위 값을 충분히 얻지 못했습니다" >&2; exit 1
fi
printf '%s\n' "${PW:0:$LEN}"
