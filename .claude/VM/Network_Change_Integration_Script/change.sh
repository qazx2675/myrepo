#!/usr/bin/env bash
###############################################################################
# change.sh — 통합 VM 망변경 스크립트 (전처리 → IP 변경 → LDAP → 포트그룹)
#
#   ./change.sh [옵션] [계정]        전체 흐름
#   ./change.sh port [계정|인시던트]  포트그룹(2차)만
#   ./change.sh rollback <인시던트>   인시던트 역순 원복 (§7)
#   ./change.sh --retry ip|ldap [계정] 해당 단계 실패분만 재시도
#   ./change.sh --debug-inventory     vCenter 인벤토리 덤프 (§9)
#
# 옵션:
#   -u, --user <계정>   작업 계정 (미지정 시 lib/common.sh 의 user선택() 호출)
#   -y, --yes           확인 프롬프트 건너뜀
#   --dry-run           실제 변경 없이 예정만
#   -d1 / -d2 / -d3     디버그 레벨 (단계시간 / 산출물보존 / raw+스크립트)
#   --only  C|D|E       해당 단계만
#   --from  C|D|E       해당 단계부터 끝까지
#   --folder <이름>     전처리 3열(BM IP VLAN) 줄에 쓸 폴더명
#   --tag <문자열>      전처리 가운데 문자열 (기본 integration.conf 의 preprocess_tag)
#
# 환경변수:
#   GOSSH_PW      대상 노드 SSH 비밀번호 (IP/LDAP 단계)
#   VC_PASSWORD   vCenter 비밀번호 (포트그룹 단계)
#   INTEGRATION_CONF  설정 파일 경로 (기본 ./integration.conf)
###############################################################################
set -uo pipefail
cd "$(dirname "$0")"

. lib/common.sh
. lib/conf.sh
. lib/preprocess.sh
. lib/stages.sh
. lib/results.sh
. lib/incident.sh

CONF_FILE="${INTEGRATION_CONF:-./integration.conf}"
WORK="./work"; LOG_DIR="./logs"; CONF_DIR="./conf"

usage() { sed -n '2,33p' "$0" | sed 's/^# \{0,1\}//'; }

# ── 인자 파싱 ──────────────────────────────────────────────────────────────
SUBCMD=""; RETRY=""; RUN_USER=""; DRY_RUN=0; DEBUG_LEVEL=0
DEBUG_INVENTORY=0; PP_FOLDER=""; PP_TAG_OVERRIDE=""; ONLY=""; FROM=""
INCIDENT_ARG=""; ASSUME_YES=0

while [ $# -gt 0 ]; do
  case "$1" in
    port)              SUBCMD=port; shift ;;
    rollback)          SUBCMD=rollback; INCIDENT_ARG="${2:-}"; shift; [ $# -gt 0 ] && shift ;;
    --retry)           RETRY="${2:-}"; shift 2 ;;
    --dry-run)         DRY_RUN=1; shift ;;
    --debug-inventory) DEBUG_INVENTORY=1; shift ;;
    -d1)               DEBUG_LEVEL=1; shift ;;
    -d2)               DEBUG_LEVEL=2; shift ;;
    -d3)               DEBUG_LEVEL=3; shift ;;
    --only)            ONLY="${2:-}"; shift 2 ;;
    --from)            FROM="${2:-}"; shift 2 ;;
    --folder)          PP_FOLDER="${2:-}"; shift 2 ;;
    --tag)             PP_TAG_OVERRIDE="${2:-}"; shift 2 ;;
    -y|--yes)          ASSUME_YES=1; shift ;;
    -u|--user)         RUN_USER="${2:-}"; shift 2 ;;
    -h|--help)         usage; exit 0 ;;
    -*)                echo "알 수 없는 옵션: $1" >&2; usage; exit 2 ;;
    *)                 if [ "$SUBCMD" = rollback ] && [ -z "$INCIDENT_ARG" ]; then
                         INCIDENT_ARG="$1"
                       elif [ "$SUBCMD" = port ] && [ -z "$INCIDENT_ARG" ] && [ -z "$RUN_USER" ]; then
                         INCIDENT_ARG="$1"
                       else
                         RUN_USER="$1"
                       fi
                       shift ;;
  esac
done

# ── 설정 로드 ─────────────────────────────────────────────────────────────
mkdir -p "$WORK" "$LOG_DIR" "$CONF_DIR"
LOG_FILE="$LOG_DIR/change_$(date +%Y%m%d_%H%M%S).log"
: >"$LOG_FILE"
[ "$DEBUG_LEVEL" -ge 3 ] && set -x

load_conf "$CONF_FILE"
SSH_USER="$(conf_get ssh_user root)"
SSH_PORT="$(conf_get ssh_port 22)"
GOSSH_PW="${GOSSH_PW:-}"
RESULT_DIR="$(conf_get result_dir ./results)"
PP_DOMAIN="$(conf_get bm_domain seccae.com)"
PP_TAG="${PP_TAG_OVERRIDE:-$(conf_get preprocess_tag cae)}"
mkdir -p "$RESULT_DIR"

log MAIN "change.sh 시작  (로그: $LOG_FILE)"

# ── 단계 판정 ─────────────────────────────────────────────────────────────
_stage_num() { case "$1" in C|c) echo 1 ;; D|d) echo 2 ;; E|e) echo 3 ;; *) echo 0 ;; esac; }
stage_enabled() {
  local s; s="$(_stage_num "$1")"
  if [ -n "$ONLY" ]; then [ "$s" -eq "$(_stage_num "$ONLY")" ]; return; fi
  if [ -n "$FROM" ]; then [ "$s" -ge "$(_stage_num "$FROM")" ]; return; fi
  return 0
}

ask_yes() {
  [ "$ASSUME_YES" -eq 1 ] && return 0
  [ -t 0 ] || return 1
  local a; read -r -p "$1 (y/N): " a
  [ "$a" = y ] || [ "$a" = Y ]
}

confirm_targets() { # §13 — IP 단계는 적용 전 대상표를 반드시 출력
  local label="$1" file="$2"
  echo; echo "── $label 대상 ($(grep -cve '^[[:space:]]*$' -e '^[[:space:]]*#' "$file")건) ──"
  grep -vE '^[[:space:]]*(#|$)' "$file" | nl -ba | sed 's/^/  /'
  echo
  [ "$DRY_RUN" -eq 1 ] && { echo "(dry-run — 실제 변경 없음)"; return 0; }
  ask_yes "위 대상에 $label 를 적용합니다. 계속하시겠습니까?" || die B1 "사용자가 중단했습니다."
}

# ── 포트그룹 단계 (vm-network-migration 에 위임) ──────────────────────────
run_portgroup() {
  stage_begin E "포트그룹 (vm-network-migration)"
  local nm_dir; nm_dir="$(conf_get nm_dir ../vm-network-migration)"
  [ -x "$nm_dir/run.sh" ] || die G2 "nm/run.sh 를 찾을 수 없습니다: $nm_dir (setup.sh 실행)"

  local vsw="$WORK/vswitch_${RUN_USER}.std.txt"
  [ -s "$vsw" ] || { [ -n "${INC_DIR:-}" ] && vsw="$INC_DIR/vswitch_${RUN_USER}.std.txt"; }
  [ -s "$vsw" ] || die B2 "표준화된 vswitch 파일이 없습니다. 전처리를 먼저 실행하세요."

  # 포트그룹 대상 = IP 변경 성공 VM (§5.2 — IP 실패 VM 이 새 VLAN 으로 옮겨져
  # '옛 IP + 새 VLAN' 으로 고립되는 것을 막음). ip_ok 가 없으면(단계 격리 등)
  # {user}.txt 전체.
  local pg_targets="$WORK/pg_targets_${RUN_USER}.txt"
  if [ -s "$RESULT_DIR/ip_ok_${RUN_USER}.txt" ]; then
    cp "$RESULT_DIR/ip_ok_${RUN_USER}.txt" "$pg_targets"
  elif [ -s "${RUN_USER}.txt" ]; then
    _hosts_of "${RUN_USER}.txt" >"$pg_targets"
  elif [ -n "${INC_DIR:-}" ] && [ -s "$INC_DIR/${RUN_USER}.txt" ]; then
    _hosts_of "$INC_DIR/${RUN_USER}.txt" >"$pg_targets"
  fi
  [ -s "$pg_targets" ] || die E9 "포트그룹 대상이 없습니다."

  cp -p "$pg_targets" "$nm_dir/${RUN_USER}.txt"
  cp -p "$vsw" "$nm_dir/vswitch_${RUN_USER}.txt"
  cp -p "$(conf_get vcenter_file ./vcenter.txt)" "$nm_dir/vcenter.txt" \
    || die G3 "vcenter.txt 가 없습니다: $(conf_get vcenter_file ./vcenter.txt)"

  local dry=""; [ "$DRY_RUN" -eq 1 ] && dry="--dry-run"
  # shellcheck disable=SC2086
  ( cd "$nm_dir" && VC_PASSWORD="${VC_PASSWORD:-}" ./run.sh -y -u "$RUN_USER" \
      --id "$(conf_get vc_id lscsystems@vsphere.local)" \
      -c "$(conf_get concurrency 8)" --vswitch "$(conf_get nm_vswitch vSwitch0)" $dry ) \
    2>&1 | tee -a "$LOG_FILE"
  local rc=${PIPESTATUS[0]}

  [ -f "$nm_dir/state_${RUN_USER}.json" ] && cp -p "$nm_dir/state_${RUN_USER}.json" "$WORK/"
  if [ "$DEBUG_LEVEL" -lt 2 ]; then
    rm -f "$nm_dir/${RUN_USER}.txt" "$nm_dir/vswitch_${RUN_USER}.txt" "$nm_dir/vcenter.txt"
  fi

  if [ "$rc" -eq 0 ]; then stage_end ok
  else stage_end fail; die E9 "포트그룹 단계 실패 (nm exit $rc). 로그: $LOG_FILE"; fi
}

# ═══════════════════════════════════════════════════════════════════════════
# 서브커맨드: --debug-inventory
# ═══════════════════════════════════════════════════════════════════════════
if [ "$DEBUG_INVENTORY" -eq 1 ]; then
  nm_dir="$(conf_get nm_dir ../vm-network-migration)"
  [ -x "$nm_dir/run.sh" ] || die G2 "nm/run.sh 없음: $nm_dir"
  cp -p "$(conf_get vcenter_file ./vcenter.txt)" "$nm_dir/vcenter.txt" \
    || die G3 "vcenter.txt 없음"
  ( cd "$nm_dir" && VC_PASSWORD="${VC_PASSWORD:-}" ./run.sh --debug-inventory \
      --id "$(conf_get vc_id lscsystems@vsphere.local)" -c "$(conf_get concurrency 8)" )
  rc=$?
  rm -f "$nm_dir/vcenter.txt"
  exit "$rc"
fi

# ═══════════════════════════════════════════════════════════════════════════
# 서브커맨드: rollback
# ═══════════════════════════════════════════════════════════════════════════
if [ "$SUBCMD" = rollback ]; then
  if [ -z "$INCIDENT_ARG" ]; then
    echo "사용법: ./change.sh rollback <인시던트이름>" >&2
    echo "저장된 인시던트:" >&2
    incident_list >&2
    exit 2
  fi
  rollback_incident "$INCIDENT_ARG"
  exit 0
fi

# ── 작업 계정 결정 ────────────────────────────────────────────────────────
if [ -z "$RUN_USER" ] && [ -z "$INCIDENT_ARG" ]; then
  user선택 || true
fi
if [ "$SUBCMD" = port ] && [ -n "$INCIDENT_ARG" ]; then
  incident_load "$INCIDENT_ARG"
fi
[ -n "$RUN_USER" ] || die B1 "작업 계정을 정하지 못했습니다. -u <계정> 으로 지정하거나 lib/common.sh 의 user선택() 를 채우십시오."

VMFILE="${RUN_USER}.txt"
VSWITCH_IN="vswitch_${RUN_USER}.txt"
VSWITCH_STD="$WORK/vswitch_${RUN_USER}.std.txt"
HOSTS_FILE="$WORK/hosts_${RUN_USER}.txt"
RES_IP=/dev/null; RES_LDAP=/dev/null

# ═══════════════════════════════════════════════════════════════════════════
# 서브커맨드: --retry ip|ldap
# ═══════════════════════════════════════════════════════════════════════════
if [ -n "$RETRY" ]; then
  case "$RETRY" in ip|ldap) ;; *) die F2 "--retry 는 ip 또는 ldap 이어야 합니다." ;; esac
  [ -f "$VMFILE" ] || die B1 "$VMFILE 가 없습니다."
  retry_vms "$RETRY" | sort -u >"$WORK/retry_${RETRY}_${RUN_USER}.txt"
  if [ ! -s "$WORK/retry_${RETRY}_${RUN_USER}.txt" ]; then
    log RETRY "재시도할 $RETRY 실패 대상이 없습니다."
    exit 0
  fi
  # 이전 결과 tsv 이어쓰기 (없으면 새로)
  RES_IP="$WORK/res_ip_${RUN_USER}.tsv"
  RES_LDAP="$WORK/res_ldap_${RUN_USER}.tsv"
  if [ "$RETRY" = ip ]; then
    awk 'NR==FNR{w[$1]=1;next} ($1 in w)' \
      "$WORK/retry_ip_${RUN_USER}.txt" "$VMFILE" >"$WORK/retry_ip_targets_${RUN_USER}.txt"
    confirm_targets "IP 변경(재시도)" "$WORK/retry_ip_targets_${RUN_USER}.txt"
    stage_ip "$WORK/retry_ip_targets_${RUN_USER}.txt"
  else
    stage_ldap "$WORK/retry_ldap_${RUN_USER}.txt"
  fi
  write_results
  log RETRY "재시도 완료. 결과: $RESULT_DIR"
  exit 0
fi

# ═══════════════════════════════════════════════════════════════════════════
# 서브커맨드: port (2차만)
# ═══════════════════════════════════════════════════════════════════════════
if [ "$SUBCMD" = port ]; then
  run_portgroup
  log MAIN "포트그룹 단계 완료."
  exit 0
fi

# ═══════════════════════════════════════════════════════════════════════════
# 전체 흐름
# ═══════════════════════════════════════════════════════════════════════════

# 재개 확인 (인시던트가 남아 있으면)
if [ -z "$ONLY" ] && [ -z "$FROM" ] && [ -n "$(incident_list 2>/dev/null)" ]; then
  echo "이전에 중단된 작업이 있습니다:"
  incident_list
  if [ "$ASSUME_YES" -eq 0 ] && [ -t 0 ]; then
    read -r -p "재개할 인시던트 이름 (엔터 = 새로 시작): " _r
    if [ -n "$_r" ]; then
      incident_load "$_r"
      run_portgroup
      exit 0
    fi
  fi
fi

# 1. 전처리 (§4) — 포트그룹 단계가 활성일 때 필요
if stage_enabled E; then
  [ -f "$VSWITCH_IN" ] || die B2 "$VSWITCH_IN 가 없습니다."
  preprocess_vswitch "$VSWITCH_IN" "$VSWITCH_STD"
fi
[ -f "$VMFILE" ] || die B1 "$VMFILE 가 없습니다."
_hosts_of "$VMFILE" >"$HOSTS_FILE"

# 2. C: IP 변경
if stage_enabled C; then
  [ -n "$GOSSH_PW" ] || die G4 "환경변수 GOSSH_PW (대상 노드 SSH 비밀번호) 를 설정하세요."
  confirm_targets "IP 변경" "$VMFILE"
  stage_ip "$VMFILE"
fi

# 3. D: LDAP
if stage_enabled D; then
  [ -n "$GOSSH_PW" ] || die G4 "환경변수 GOSSH_PW 를 설정하세요."
  LDAP_STAMP_HINT="$(date +%Y%m%d%H%M%S)"
  stage_ldap "$HOSTS_FILE"
fi

# 4. 결과 리다이렉션 (§6) — dry-run 은 결과 파일을 만들지 않습니다
if [ "$DRY_RUN" -eq 0 ] && { stage_enabled C || stage_enabled D; }; then
  write_results
fi

# 5. 포트그룹 질의
if stage_enabled E; then
  if [ "$DRY_RUN" -eq 1 ] || ask_yes "포트그룹(2차) 작업을 지금 진행하시겠습니까?"; then
    run_portgroup
  else
    _inc=""
    [ -t 0 ] && read -r -p "인시던트 이름 (엔터 = 자동): " _inc
    [ -n "$_inc" ] || _inc="incident_$(date +%Y%m%d_%H%M%S)"
    incident_save "$_inc" "portgroup"
    log MAIN "포트그룹은 나중에 진행:  ./change.sh port $_inc"
  fi
fi

log MAIN "완료. 로그: $LOG_FILE"
