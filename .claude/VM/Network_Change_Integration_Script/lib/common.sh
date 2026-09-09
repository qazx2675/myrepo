# lib/common.sh — 공통 유틸: 설정 로드, 로그, 디버그 레벨, 에러코드
#
# 이 파일은 change.sh 가 source 합니다. 단독 실행하지 않습니다.

# ── 디버그 레벨 (change.sh 가 -d1/-d2/-d3 파싱해서 채움) ──────────────────────
: "${DEBUG_LEVEL:=0}"

# ── 로그 파일 (change.sh 가 지정) ────────────────────────────────────────────
: "${LOG_FILE:=/dev/null}"

# log LEVEL MESSAGE...
#   화면과 로그 파일에 함께 남깁니다. 로그에는 항상, 화면에는 레벨에 따라.
log() {
  local lvl="$1"; shift
  local ts; ts="$(date '+%Y-%m-%d %H:%M:%S')"
  printf '%s [%s] %s\n' "$ts" "$lvl" "$*" >>"$LOG_FILE" 2>/dev/null || true
  printf '[%s] %s\n' "$lvl" "$*"
}

# dlog N MESSAGE — 디버그 레벨 N 이상일 때만 화면 출력 (로그에는 항상)
dlog() {
  local need="$1"; shift
  local ts; ts="$(date '+%Y-%m-%d %H:%M:%S')"
  printf '%s [d%s] %s\n' "$ts" "$need" "$*" >>"$LOG_FILE" 2>/dev/null || true
  [ "$DEBUG_LEVEL" -ge "$need" ] && printf '[d%s] %s\n' "$need" "$*"
  return 0
}

# stage_begin CODE_PREFIX "제목"
stage_begin() {
  STAGE_PREFIX="$1"
  STAGE_TITLE="$2"
  STAGE_T0="$(date +%s)"
  log "$STAGE_PREFIX" "── $STAGE_TITLE 시작 ──"
}

# stage_end [ok|fail]
stage_end() {
  local result="${1:-ok}"
  local dt=$(( $(date +%s) - STAGE_T0 ))
  dlog 1 "$STAGE_TITLE 소요 ${dt}s"
  if [ "$result" = ok ]; then
    log "$STAGE_PREFIX" "OK  ($STAGE_TITLE, ${dt}s)"
  else
    log "$STAGE_PREFIX" "FAIL  ($STAGE_TITLE, ${dt}s)"
  fi
}

# die CODE MESSAGE...
#   에러코드(예: A2, G1)와 사람이 읽을 메시지를 화면·로그에 남기고 종료.
#   화면 복사가 안 되는 환경을 전제로, 첫 줄에 코드만 크게 찍습니다.
die() {
  local code="$1"; shift
  {
    echo
    echo "  ┌─────────────────────────────"
    echo "  │  오류 코드:  $code"
    echo "  │  $*"
    echo "  │  → 원인·대처는 README 의 '에러코드 대응표' 에서 [$code] 를 보십시오."
    echo "  └─────────────────────────────"
    echo
  } | tee -a "$LOG_FILE" >&2
  exit 1
}

# warn_box MESSAGE... — 크게 눈에 띄는 경고 (기본값 적용 등)
warn_box() {
  {
    echo
    echo "  ***  경고  ***"
    echo "  $*"
    echo
  } | tee -a "$LOG_FILE"
}

# ── integration.conf 파싱 ───────────────────────────────────────────────────
# conf_get KEY [DEFAULT]
declare -A CONF
load_conf() {
  local path="$1"
  [ -f "$path" ] || die G3 "integration.conf 가 없습니다: $path (integration.conf.sample 을 복사해 채우십시오)"
  local line k v
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line%%$'\r'}"
    case "$line" in ''|'#'*) continue ;; esac
    [[ "$line" == *'='* ]] || die G3 "integration.conf $line — '=' 가 없는 줄"
    k="${line%%=*}"; v="${line#*=}"
    # 앞뒤 공백 제거
    k="$(printf '%s' "$k" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    v="$(printf '%s' "$v" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    CONF["$k"]="$v"
  done <"$path"
}

conf_get() {
  local k="$1" def="${2:-}"
  if [ -n "${CONF[$k]+x}" ] && [ -n "${CONF[$k]}" ]; then
    printf '%s' "${CONF[$k]}"
  else
    printf '%s' "$def"
  fi
}

# ── user선택 (사용자 커스텀 영역 — 의도적으로 비워 둠) ──────────────────────
# 통합 스크립트에서 딱 한 곳, 여기서만 계정을 고릅니다. 결과로 RUN_USER 를
# 설정하십시오. 각 하위 프로젝트의 빈 선택 함수는 호출하지 않습니다.
user선택() {
  :
}
