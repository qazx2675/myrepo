#!/usr/bin/env bash
#
# 자산 조사 실행 래퍼
#
#   사용법: ./run_survey.sh
#
# 하는 일:
#   1) survey 바이너리 존재/실행권한 확인 (없으면 빌드 명령 안내)
#   2) conf/conf.toml 존재 확인
#   3) 이 스크립트가 있는 폴더에서 survey 실행
#      -> 결과 result_YYYYMMDD_HHMM.tsv 가 이 폴더에 생성됨
#
# 설정은 conf/conf.toml 하나만 사용한다. 옵션으로 지정하지 않는다.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="${SCRIPT_DIR}/survey"
CONF="${SCRIPT_DIR}/conf/conf.toml"

if [[ ! -x "$BIN" ]]; then
  echo "[error] 바이너리가 없습니다: $BIN" >&2
  echo "        빌드: (cd \"$SCRIPT_DIR\" && go build -o survey ./cmd/survey)" >&2
  exit 1
fi

if [[ ! -f "$CONF" ]]; then
  echo "[error] 설정 파일이 없습니다: $CONF" >&2
  exit 1
fi

cd "$SCRIPT_DIR"
exec "$BIN"
