#!/usr/bin/env bash
# run.sh — VM 네트워크 마이그레이션 전체 워크플로우 제어 스크립트.
#
#   [백업] -> [Step 2 포트그룹 생성] -> [Step 1 연결 해제] -> [Step 3 신규 연결] -> [Step 4 검증]
#
# 각 단계의 종료 코드를 확인해서, 실패하면 그 단계에서 실패한 VM 만 골라
# 자동으로 롤백합니다. 포트그룹 생성을 연결 해제보다 먼저 돌리는 이유는
# 해제~연결 사이의 네트워크 단절 구간을 최대한 짧게 만들기 위해서입니다.
set -uo pipefail

cd "$(dirname "$0")"
BIN="$(pwd)/bin"

USER_TOKEN=""
CONCURRENCY=8
NIC_INDEX=0
TARGET_VSWITCH="vSwitch0"
DRY_RUN=""
ASSUME_YES=0
ROLLBACK_ONLY=0
RESUME=0
FORCE_BACKUP=0

usage() {
  cat <<'USAGE'
사용법: ./run.sh [옵션] [사용자토큰]

옵션:
  -u, --user <토큰>        작업 대상 사용자 토큰 (미지정 시 대화형 선택)
  -c, --concurrency <N>    동시에 처리할 VM 수 (기본 8)
      --nic-index <N>      대상 가상 NIC 순번 (기본 0 = 네트워크 어댑터 1)
      --vswitch <이름>     포트그룹을 만들 표준 가상 스위치 (기본 vSwitch0)
      --dry-run            실제 변경 없이 무엇이 바뀔지만 출력
  -y, --yes                실행 전 확인 프롬프트를 건너뜁니다
      --rollback           마이그레이션 없이 롤백만 수행합니다
      --resume             기존 상태 파일을 그대로 두고 Step 0(백업)을 건너뜁니다
      --force-backup       기존 상태 파일을 현재 상태로 덮어씁니다 (주의: 이전 원본 기록이 사라집니다)
  -h, --help               이 도움말

환경변수 (필수):
  VC_USER       vCenter 로그인 계정
  VC_PASSWORD   vCenter 비밀번호
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    -u|--user)        USER_TOKEN="$2"; shift 2 ;;
    -c|--concurrency) CONCURRENCY="$2"; shift 2 ;;
    --nic-index)      NIC_INDEX="$2"; shift 2 ;;
    --vswitch)        TARGET_VSWITCH="$2"; shift 2 ;;
    --dry-run)        DRY_RUN="-dry-run"; shift ;;
    -y|--yes)         ASSUME_YES=1; shift ;;
    --rollback)       ROLLBACK_ONLY=1; shift ;;
    --resume)         RESUME=1; shift ;;
    --force-backup)   FORCE_BACKUP=1; shift ;;
    -h|--help)        usage; exit 0 ;;
    -*)               echo "알 수 없는 옵션: $1" >&2; usage; exit 2 ;;
    *)                USER_TOKEN="$1"; shift ;;
  esac
done

# -----------------------------------------------------------------------------
# select_user — 작업 대상 사용자 환경을 고릅니다. (사용자 커스텀 영역)
#
# 기본 구현은 현재 디렉터리의 vswitch_*.txt 에서 토큰을 뽑아 목록으로 보여줍니다.
# 사이트마다 사용자 목록을 얻는 방법이 다르면 이 함수만 고치면 됩니다.
# -----------------------------------------------------------------------------
select_user() {
  local candidates=()
  local f token
  for f in vswitch_*.txt; do
    [ -e "$f" ] || continue
    token="${f#vswitch_}"
    token="${token%.txt}"
    candidates+=("$token")
  done

  if [ ${#candidates[@]} -eq 0 ]; then
    echo "오류: vswitch_<사용자>.txt 파일이 없습니다. -u 로 직접 지정하세요." >&2
    exit 2
  fi
  if [ ${#candidates[@]} -eq 1 ]; then
    USER_TOKEN="${candidates[0]}"
    echo "[INFO] 대상 사용자: $USER_TOKEN"
    return
  fi

  echo "작업 대상 사용자를 선택하세요:"
  local i=1
  for token in "${candidates[@]}"; do
    echo "  $i) $token"
    i=$((i + 1))
  done
  local choice
  read -r -p "번호: " choice
  if ! [[ "$choice" =~ ^[0-9]+$ ]] || [ "$choice" -lt 1 ] || [ "$choice" -gt ${#candidates[@]} ]; then
    echo "오류: 잘못된 선택입니다." >&2
    exit 2
  fi
  USER_TOKEN="${candidates[$((choice - 1))]}"
}

# -----------------------------------------------------------------------------
# tag_reset — 작업 완료 후 태그 재설정 여부를 묻습니다. (사용자 커스텀 영역)
# -----------------------------------------------------------------------------
tag_reset() {
  [ "$ASSUME_YES" -eq 1 ] && return 0
  local choice
  read -r -p "네트워크 태그를 재설정하시겠습니까? (y/n): " choice
  if [ "$choice" == "y" ]; then
    # 향후 태그 재설정 로직 추가 가능
    :
  fi
}

# -----------------------------------------------------------------------------
# 사전 점검 — 실행 전에 빠뜨린 것이 있으면 vCenter 를 건드리기 전에 멈춥니다.
# -----------------------------------------------------------------------------
preflight() {
  local missing=0
  local b file

  if [ ! -d "$BIN" ]; then
    echo "오류: bin/ 이 없습니다. 먼저 ./setup.sh 로 빌드하세요." >&2
    exit 2
  fi
  for b in backup pgcreate disconnect connect verify rollback; do
    if [ ! -x "$BIN/nm-$b" ]; then
      echo "오류: $BIN/nm-$b 가 없습니다. ./setup.sh 를 다시 실행하세요." >&2
      missing=1
    fi
  done
  [ "$missing" -eq 1 ] && exit 2

  if [ -z "${VC_USER:-}" ] || [ -z "${VC_PASSWORD:-}" ]; then
    echo "오류: 환경변수 VC_USER / VC_PASSWORD 를 설정하세요." >&2
    echo "      예: export VC_USER='administrator@vsphere.local'" >&2
    echo "          read -rsp '비밀번호: ' VC_PASSWORD; export VC_PASSWORD; echo" >&2
    exit 2
  fi

  for file in vcenter.txt "${USER_TOKEN}.txt" "vswitch_${USER_TOKEN}.txt"; do
    if [ ! -f "$file" ]; then
      echo "오류: 입력 파일 $file 이 없습니다." >&2
      missing=1
    fi
  done
  [ "$missing" -eq 1 ] && exit 2
}

# common_args — 모든 바이너리가 같은 플래그 이름을 쓰므로 인자를 한 곳에서 만듭니다.
#
# dry-run 일 때는 상태 파일을 별도 경로로 돌립니다. 백업 자체는 vCenter 를 읽기만
# 하므로 dry-run 에서도 상태 파일을 만들어야 뒤 단계가 무엇을 할지 계산할 수 있는데,
# 그렇다고 진짜 state_{user}.json 을 건드리면 실제 작업의 원본 기록이 오염됩니다.
common_args() {
  printf '%s' "-user=$USER_TOKEN -concurrency=$CONCURRENCY -nic-index=$NIC_INDEX $DRY_RUN -state-file=$STATE_FILE"
}

FAILED_FILE=""
ROLLED_BACK=0

# rollback_failed — 직전 단계에서 실패한 VM 만 골라 작업 전 상태로 되돌립니다.
rollback_failed() {
  local step_name="$1"
  local only_arg=""

  # dry-run 은 vCenter 를 바꾸지 않았으므로 되돌릴 것이 없습니다.
  # 여기서 롤백을 부르면 "치명적" 같은 잘못된 경고만 내게 됩니다.
  if [ -n "$DRY_RUN" ]; then
    echo >&2
    echo "[중단] dry-run 중 '$step_name' 에서 오류가 났습니다." >&2
    echo "       실제로 변경한 것은 없으므로 롤백하지 않습니다." >&2
    exit 1
  fi

  echo
  echo "=============================================================="
  echo "[롤백] '$step_name' 실패 -> 작업 전 상태로 되돌립니다."
  echo "=============================================================="

  if [ -s "$FAILED_FILE" ]; then
    only_arg="-only-file=$FAILED_FILE"
    echo "[INFO] 실패한 VM $(grep -cve '^[[:space:]]*$' "$FAILED_FILE")대만 선택적으로 되돌립니다."
  else
    # 실패 목록이 비어 있으면(설정 오류 등으로 단계가 통째로 멈춘 경우)
    # 어디까지 반영됐는지 알 수 없으므로 대상 전체를 되돌립니다.
    echo "[INFO] 실패 목록이 비어 있어 대상 전체를 되돌립니다."
  fi

  # -prune: 되돌린 VM 을 상태 파일에서 빼서 이후 단계의 대상에서 제외합니다.
  # common_args / only_arg 는 여러 인자로 쪼개져야 하므로 의도적으로 따옴표를 뺍니다.
  # shellcheck disable=SC2046,SC2086
  if ! "$BIN/nm-rollback" $(common_args) $only_arg -prune; then
    echo >&2
    echo "[치명적] 자동 롤백도 실패했습니다. rollback_failed_${USER_TOKEN}.txt 를 확인하고" >&2
    echo "         해당 VM 은 vCenter 에서 수동으로 원복하십시오." >&2
    exit 3
  fi

  ROLLED_BACK=1
  echo "[INFO] 실패한 VM 은 작업 전 상태로 되돌렸습니다."
}

# remaining_count — 상태 파일에 남아 있는 대상 VM 수.
# 상태 파일은 이 도구가 직접 쓴 JSON 이라 vm_name 줄 수를 세면 됩니다.
remaining_count() {
  [ -f "$STATE_FILE" ] || { echo 0; return; }
  grep -c '"vm_name"' "$STATE_FILE" 2>/dev/null || echo 0
}

# step — 단계 하나를 실행합니다.
#
# 일부 VM 만 실패하면, 실패한 VM 만 원래대로 되돌린 뒤 나머지 VM 으로 계속
# 진행합니다. 여기서 통째로 멈추면 이미 연결이 끊긴(Step 1 성공) VM 들이
# 신규 포트그룹에 붙지 못한 채 네트워크가 죽은 상태로 남기 때문입니다.
step() {
  local title="$1"; shift
  local remain
  echo
  echo "=============================================================="
  echo "  $title"
  echo "=============================================================="
  if ! "$@"; then
    rollback_failed "$title"
    remain=$(remaining_count)
    if [ "$remain" -eq 0 ]; then
      echo >&2
      echo "[중단] 모든 대상 VM 이 원래 상태로 되돌아갔습니다. 진행할 대상이 없습니다." >&2
      exit 1
    fi
    echo "[INFO] 남은 ${remain}대로 다음 단계를 계속 진행합니다."
  fi
}

# -----------------------------------------------------------------------------
# 본체
# -----------------------------------------------------------------------------
[ -z "$USER_TOKEN" ] && select_user
FAILED_FILE="failed_${USER_TOKEN}.txt"
STATE_FILE="state_${USER_TOKEN}.json"
# dry-run 은 실제 상태 파일을 절대 건드리지 않습니다.
[ -n "$DRY_RUN" ] && STATE_FILE="state_${USER_TOKEN}.dryrun.json"
preflight

VM_COUNT=$(grep -cve '^[[:space:]]*$' -e '^[[:space:]]*#' "${USER_TOKEN}.txt")
MODE_LABEL="실제 변경"
[ -n "$DRY_RUN" ] && MODE_LABEL="dry-run (변경 없음)"

cat <<BANNER

--------------------------------------------------------------------
 VM 네트워크 마이그레이션
--------------------------------------------------------------------
 대상 사용자   : $USER_TOKEN
 VM 목록       : ${USER_TOKEN}.txt (${VM_COUNT}대)
 네트워크 설정 : vswitch_${USER_TOKEN}.txt
 상태 파일     : state_${USER_TOKEN}.json
 대상 vSwitch  : $TARGET_VSWITCH
 동시 실행 수  : $CONCURRENCY
 모드          : $MODE_LABEL
--------------------------------------------------------------------

주의사항 (Disclaimer)
 본 스크립트 및 툴은 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을
 권장합니다. 이 도구는 VM 의 네트워크 설정을 실제로 변경(write)하므로,
 작업 완료 후 반드시 랜덤한 서버 몇 대를 직접 확인해서 의도한 포트그룹으로
 실제 변경되었는지 눈으로 검증하십시오. Step 4 검증을 통과했다는 것이 곧
 서비스 정상을 뜻하지는 않습니다(게스트 내부 IP/라우팅은 확인하지 않습니다).

BANNER

if [ "$ROLLBACK_ONLY" -eq 1 ]; then
  echo "[모드] 마이그레이션 없이 롤백만 수행합니다."
  if [ "$ASSUME_YES" -eq 0 ]; then
    read -r -p "정말 롤백하시겠습니까? (y/n): " confirm
    [ "$confirm" == "y" ] || { echo "취소했습니다."; exit 0; }
  fi
  # shellcheck disable=SC2046,SC2086
  "$BIN/nm-rollback" $(common_args)
  exit $?
fi

if [ "$ASSUME_YES" -eq 0 ] && [ -z "$DRY_RUN" ]; then
  read -r -p "위 내용으로 진행하시겠습니까? (y/n): " confirm
  [ "$confirm" == "y" ] || { echo "취소했습니다."; exit 0; }
fi

# -----------------------------------------------------------------------------
# Step 0 사전 상태 백업
#
# 상태 파일이 이미 있으면 그냥 덮어쓰면 안 됩니다. 이전 실행이 중간까지 갔다면
# "지금 상태"는 이미 변경된 상태라서, 그걸 원본으로 덮어쓰는 순간 진짜 원본이
# 사라지고 롤백이 무의미해집니다. 그래서 사용자가 어떻게 할지 정하게 합니다.
#
# dry-run 은 별도 상태 파일을 쓰므로 이 확인이 필요 없습니다(항상 새로 만듭니다).
# -----------------------------------------------------------------------------
if [ -n "$DRY_RUN" ]; then
  rm -f "$STATE_FILE"
  RESUME=0
  FORCE_BACKUP=0
  # 어디서 빠져나가든 dry-run 임시 상태 파일은 남기지 않습니다.
  trap 'rm -f "$STATE_FILE"' EXIT
fi

if [ -z "$DRY_RUN" ] && [ "$RESUME" -eq 0 ] && [ "$FORCE_BACKUP" -eq 0 ] && [ -f "$STATE_FILE" ]; then
  echo
  echo "[확인 필요] 상태 파일 $STATE_FILE 이 이미 있습니다."
  echo "  이전 작업의 원본 기록입니다. 덮어쓰면 원본으로 되돌릴 수 없게 됩니다."
  echo
  echo "   1) 이어서 진행  — 기존 백업을 그대로 쓰고 Step 0 을 건너뜁니다 (--resume)"
  echo "   2) 새로 백업    — 현재 상태를 원본으로 덮어씁니다 (--force-backup)"
  echo "   3) 취소"
  echo
  if [ "$ASSUME_YES" -eq 1 ]; then
    echo "오류: -y 로 실행 중이라 자동 선택하지 않습니다." >&2
    echo "      --resume 또는 --force-backup 을 명시하거나, 롤백 후 상태 파일을 정리하세요." >&2
    exit 2
  fi
  read -r -p "선택 (1/2/3): " state_choice
  case "$state_choice" in
    1) RESUME=1 ;;
    2) FORCE_BACKUP=1 ;;
    *) echo "취소했습니다."; exit 0 ;;
  esac
fi

if [ "$RESUME" -eq 1 ]; then
  echo
  echo "=============================================================="
  echo "  [Step 0] 사전 상태 백업 — 건너뜀 (--resume, 기존 $STATE_FILE 사용)"
  echo "=============================================================="
else
  # 백업 단계는 실패해도 되돌릴 것이 없으므로(아직 아무것도 바꾸지 않았음)
  # 롤백을 부르지 않고 그대로 중단합니다.
  echo
  echo "=============================================================="
  echo "  [Step 0] 사전 상태 백업"
  echo "=============================================================="
  FORCE_ARG=""
  [ "$FORCE_BACKUP" -eq 1 ] && FORCE_ARG="-force"
  # common_args 는 여러 인자로 쪼개져야 하므로 의도적으로 따옴표를 뺍니다.
  # shellcheck disable=SC2046,SC2086
  if ! "$BIN/nm-backup" $(common_args) $FORCE_ARG; then
    echo >&2
    echo "[중단] 사전 백업 실패. 아무것도 변경하지 않았습니다." >&2
    echo "       롤백이 불가능한 상태로 작업을 시작하지 않기 위한 의도적인 중단입니다." >&2
    exit 1
  fi
fi

# shellcheck disable=SC2046,SC2086
step "[Step 2] 신규 포트그룹 생성" \
  "$BIN/nm-pgcreate" $(common_args) "-target-vswitch=$TARGET_VSWITCH"

# shellcheck disable=SC2046,SC2086
step "[Step 1] 기존 포트그룹 연결 해제" \
  "$BIN/nm-disconnect" $(common_args)

# shellcheck disable=SC2046,SC2086
step "[Step 3] 신규 포트그룹 연결" \
  "$BIN/nm-connect" $(common_args)

# shellcheck disable=SC2046,SC2086
step "[Step 4] 연결성 검증" \
  "$BIN/nm-verify" $(common_args)

if [ -n "$DRY_RUN" ]; then
  rm -f "$STATE_FILE"
  echo
  echo "=============================================================="
  echo "  dry-run 완료 — 실제로 변경된 것은 없습니다"
  echo "=============================================================="
  exit 0
fi

echo
echo "=============================================================="
if [ "$ROLLED_BACK" -eq 1 ]; then
  echo "  작업 완료 (일부 VM 은 원래 상태로 되돌림)"
else
  echo "  전체 작업 완료"
fi
echo "=============================================================="
echo

if [ "$ROLLED_BACK" -eq 1 ]; then
  echo "[주의] 중간에 실패해서 되돌린 VM 이 있습니다."
  echo "       rollback_failed_${USER_TOKEN}.txt / failed_${USER_TOKEN}.txt 를 확인하고,"
  echo "       되돌린 VM 은 원인을 파악한 뒤 다시 작업하십시오."
  echo
fi

echo "다시 강조합니다: 지금 랜덤으로 서버 몇 대를 골라 vCenter 에서 직접"
echo "포트그룹이 의도대로 바뀌었는지, 게스트 통신이 되는지 확인하십시오."
echo

tag_reset

[ "$ROLLED_BACK" -eq 1 ] && exit 1
exit 0
