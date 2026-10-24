#!/usr/bin/env bash
# playground_worker.sh — 실제 작업 스크립트(vm_power_onoff 등)가 cross_check 를 쓰는 방식을 흉내 내는 "작업자 시뮬레이터".
#
#   bash playground_worker.sh <작업자> <확인자> [대상...]
#
#   1) <작업자> 의 대상 파일(on_off_<작업자>.txt)에서 대상을 읽는다 (대상을 직접 적으면 그것을 쓴다 — 대상 이탈 시험용)
#   2) cross_check --run 으로 확인자 승인을 기다린다  (stdout 의 JOB/TOKEN 은 out=$(...) || exit 1; eval "$out" 로 안전하게 받는다)
#   3) 승인되면 Enter 를 기다린다 — 일부러 오래 기다려 승인 유효시간 초과를 시험해 볼 수 있다
#   4) cross_check --start 가 통과하면 "작업 수행(시뮬레이션)", 거부되면 거부 사유를 보여 주고 끝낸다
#
# 놀이터 데몬(playground.sh start)이 떠 있어야 한다. 환경변수 CROSS_CHECK_* 가 없으면 이 폴더의 playground.sh env 를 자동으로 적용한다.
# 실제로 VM 전원을 건드리지 않는다(화면 출력만).
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"

usage() {
  cat >&2 <<EOF
사용법: bash playground_worker.sh <작업자> <확인자> [대상...]
  예) bash playground_worker.sh alice bob                  # alice 의 대상 파일 전체를 올림
      bash playground_worker.sh alice bob svr1ev01         # 일부 대상만
      bash playground_worker.sh alice bob svr9ev99         # 대상 파일 밖의 대상 → 거부되는지 시험
EOF
}

case "${1:-}" in -h|--help) usage; exit 0 ;; esac
[ $# -ge 2 ] || { usage; exit 2; }
USER_NAME="$1"
CHECKER="$2"
shift 2

# 놀이터 환경 적용 (이미 eval "$(bash playground.sh env)" 를 했으면 그대로 둔다)
if [ -z "${CROSS_CHECK_SOCKET:-}" ] || ! command -v cross_check >/dev/null 2>&1; then
  eval "$(bash "$SCRIPT_DIR/playground.sh" env)"
fi
command -v cross_check >/dev/null 2>&1 || { echo "[오류] cross_check 를 찾지 못했습니다 — 먼저 bash $SCRIPT_DIR/playground.sh start" >&2; exit 1; }
if [ ! -S "$CROSS_CHECK_SOCKET" ]; then
  echo "[오류] 놀이터 데몬이 떠 있지 않습니다 — 먼저:  bash $SCRIPT_DIR/playground.sh start" >&2
  exit 1
fi

# 1) 대상 읽기
if [ $# -gt 0 ]; then
  TARGETS="$(printf '%s\n' "$@")"
  SRC="명령행"
else
  TARGET_DIR="$(sed -n 's/^target_dir=//p' "$CROSS_CHECK_CONF" | head -1 | sed 's/#.*//' | tr -d ' \t\r')"
  TFILE="$TARGET_DIR/on_off_${USER_NAME}.txt"
  if [ ! -f "$TFILE" ]; then
    echo "[오류] 대상 파일이 없습니다: $TFILE  (놀이터 계정: alice, bob, carol)" >&2
    exit 1
  fi
  TARGETS="$(sed 's/#.*//' "$TFILE" | tr -s ' \t\r' '\n\n\n' | grep -v '^$' || true)"
  SRC="$TFILE"
fi
if [ -z "$TARGETS" ]; then
  echo "[오류] 작업 대상이 비어 있습니다" >&2
  exit 1
fi
TARGET_LIST="$(printf '%s' "$TARGETS" | tr '\n' ' ')"

CONF_TTL="$(sed -n 's/^approval_ttl_sec=\([0-9][0-9]*\).*/\1/p' "$CROSS_CHECK_CONF" 2>/dev/null | head -1)"
echo "[작업자] 사용자=$USER_NAME 확인자=$CHECKER"
echo "[작업자] 대상(${SRC}): $TARGET_LIST"
echo "[작업자] conf 의 승인 유효시간: ${CONF_TTL:-?}초 (데몬이 실제 적용 중인 값은 승인 때 아래에 표시됩니다)"

# Ctrl+C: 대기 중이면 cross_check 가 끊기며 데몬이 "작업자 스크립트 종료" 로 기록한다
trap 'echo; echo "[작업자] Ctrl+C — 작업을 중단합니다 (대기 중이었다면 데몬이 \"작업자 스크립트 종료\" 로 기록)"; exit 130' INT

# 2) 확인자 승인 대기 — 실제 스크립트와 같은 안전한 받기 패턴
#    (eval "$(cross_check --run ...)" 로 바로 받으면 --run 이 실패해도 종료 코드가 0 이 되므로 변수에 한 번 받는다)
out=$(printf '%s\n' "$TARGETS" | cross_check --run --user "$USER_NAME" --checker "$CHECKER") || {
  rc=$?
  echo "[작업자] 승인을 받지 못했습니다 (cross_check --run 종료 코드 $rc) — 본 작업을 하지 않습니다." >&2
  exit 1
}
eval "$out"
export CROSS_CHECK_TOKEN
if [ -z "${CROSS_CHECK_JOB:-}" ] || [ -z "${CROSS_CHECK_TOKEN:-}" ]; then
  echo "[작업자] 승인 응답에 JOB/TOKEN 이 없습니다 — 중단" >&2
  exit 1
fi
echo "[작업자] 승인됨 — 작업 ID $CROSS_CHECK_JOB"

# 3) 사람이 개입하는 자리(실제로는 안전장치 단계 등). 여기서 승인 유효시간을 넘기면 --start 가 거부된다.
echo "[작업자] Enter 를 누르면 본 작업을 시작합니다(--start). 승인 유효시간 안에 누르세요 — 일부러 기다려 만료를 시험해도 됩니다."
IFS= read -r _ || echo "[작업자] (입력이 없어 바로 진행합니다)"

# 4) 본 작업 직전 신호
if cross_check --start --job "$CROSS_CHECK_JOB"; then
  unset CROSS_CHECK_TOKEN
  echo
  echo "[작업자] 작업 수행(시뮬레이션) — 아래 대상에 전원 작업을 했다고 가정합니다:"
  for t in $TARGET_LIST; do
    echo "    - $t (전원 작업 시뮬레이션)"
    sleep 0.3
  done
  echo "[작업자] 완료 (작업 $CROSS_CHECK_JOB)"
  exit 0
else
  rc=$?
  unset CROSS_CHECK_TOKEN
  echo
  echo "[작업자] 거부됨 — cross_check --start 종료 코드 $rc. 본 작업을 수행하지 않습니다." >&2
  exit 1
fi
