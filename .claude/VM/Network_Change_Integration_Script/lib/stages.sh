# lib/stages.sh — IP 변경(C) / LDAP(D) 단계 실행
#
# 공통 처리:
#   - OS 6 분기 (§8.1): os6_hostgroup 에 속한 호스트는 별도 gossh 로 나눠 호출
#   - 2패스 타임아웃 (§8.2): 1차(짧게) → 타임아웃/실패 호스트 중 ping 되는 것만
#     2차(길게) 재실행
#   - 엔진 stdout 을 파싱해 호스트별 OK/FAIL 을 <work>/res_*.tsv 로 남김
#
# ⚠️ 엔진 출력 파싱 계약: 아래 awk 패턴은 ip-change-engine / ldap-config-engine
#    의 화면 출력 형식에 의존합니다. 엔진 출력이 바뀌면 CI 의 "출력 계약 확인"
#    단계가 먼저 깨지도록 해 두었습니다 (.github/workflows).
#
# change.sh 가 source 합니다.

# ── 호스트 그룹 판정 ────────────────────────────────────────────────────────
# _in_os6 <hostname>  → os6_hostgroup 파일에 있으면 0
_OS6_LOADED=0
declare -A _OS6_SET
_load_os6() {
  [ "$_OS6_LOADED" -eq 1 ] && return
  _OS6_LOADED=1
  local f; f="$(conf_get os6_hostgroup)"
  [ -n "$f" ] && [ -f "$f" ] || return
  local h
  while IFS= read -r h || [ -n "$h" ]; do
    h="${h%%$'\r'}"; h="${h//[[:space:]]/}"
    case "$h" in ''|'#'*) continue ;; esac
    _OS6_SET["$h"]=1
  done <"$f"
  dlog 1 "OS6 호스트그룹 ${#_OS6_SET[@]}건 로드"
}
_in_os6() { _load_os6; [ -n "${_OS6_SET[$1]+x}" ]; }

# _ping_ok <host> — 1회 ping (2초)
_ping_ok() { ping -c1 -W2 "$1" >/dev/null 2>&1; }

# ── ip-change-engine 한 번 실행 ─────────────────────────────────────────────
# _run_ip <targets:"vm ip"> <timeout> <gossh> <bin> <result.tsv(append)>
_run_ip() {
  local targets="$1" timeout="$2" gossh="$3" bin="$4" out="$5"
  local raw; raw="$(mktemp)"
  local rc=0
  NO_COLOR=1 "$bin" \
    -config "$(conf_get ipchange_conf ./conf/ip_change.conf)" \
    -targets "$targets" \
    -gossh "$gossh" -u "$SSH_USER" -p "$GOSSH_PW" -P "$SSH_PORT" \
    -c "$(conf_get concurrency 8)" -t "$timeout" \
    >"$raw" 2>&1 || rc=$?
  cat "$raw" >>"$LOG_FILE" 2>/dev/null || true
  [ "$DEBUG_LEVEL" -ge 3 ] && { echo "--- ip-change-engine raw ---"; cat "$raw"; echo "---"; }
  if [ "$rc" -eq 1 ]; then
    cat "$raw" >&2
    die C1 "ip-change-engine 실행 실패 (exit 1). 로그: $LOG_FILE"
  fi
  awk '
    /-> .*\(GW / { print $1 "\tOK";   next }
    / FAIL: /    { print $1 "\tFAIL"; next }
    /UNREACHABLE/{ print $1 "\tFAIL"; next }
    /알 수 없는 응답/ { print $1 "\tFAIL"; next }
  ' "$raw" >>"$out"
  rm -f "$raw"
}

# ── ldap-config-engine 한 번 실행 ──────────────────────────────────────────
# _run_ldap <hostfile:"host"> <timeout> <gossh> <bin> <result.tsv(append)>
_run_ldap() {
  local hostfile="$1" timeout="$2" gossh="$3" bin="$4" out="$5"
  local raw; raw="$(mktemp)"
  local rc=0
  local ds; ds="$(conf_get default_site)"
  local dry=""; [ "${DRY_RUN:-0}" -eq 1 ] && dry="-dry-run"
  NO_COLOR=1 "$bin" \
    -config "$(conf_get ldap_conf)" \
    -assets "$(conf_get ldap_assets)" \
    -infra "$INFRA" \
    -host-file "$hostfile" \
    ${ds:+-default-site "$ds"} $dry \
    -gossh "$gossh" -u "$SSH_USER" -p "$GOSSH_PW" -P "$SSH_PORT" \
    -c "$(conf_get concurrency 8)" -t "$timeout" \
    >"$raw" 2>&1 || rc=$?
  cat "$raw" >>"$LOG_FILE" 2>/dev/null || true
  [ "$DEBUG_LEVEL" -ge 3 ] && { echo "--- ldap-config-engine raw ---"; cat "$raw"; echo "---"; }
  if [ "$rc" -eq 1 ]; then
    cat "$raw" >&2
    # 자산현황·기본값 모두로 site 판정이 안 된 경우도 여기로 옵니다.
    grep -q "default_site\|사이트\|자산현황" "$raw" && die D1 "LDAP site 판정 실패. 로그: $LOG_FILE"
    die D2 "ldap-config-engine 실행 실패 (exit 1). 로그: $LOG_FILE"
  fi
  awk '
    /^ +[^ ]+ +OK([[:space:]]|$)/        { print $1 "\tOK";   next }
    /^ +[^ ]+ +(FAIL|NORESULT|UNREACHABLE)([[:space:]]|$)/ { print $1 "\tFAIL"; next }
  ' "$raw" >>"$out"
  rm -f "$raw"
}

# ── 대상 목록에서 host 컬럼만 뽑기 ─────────────────────────────────────────
# _hosts_of <file>  → 첫 컬럼만 (공백/주석 제외) stdout
_hosts_of() { awk 'NF && $1 !~ /^#/ { print $1 }' "$1"; }

# ── 2패스 + OS6 분기 공통 실행기 ───────────────────────────────────────────
# _stage_run <kind:ip|ldap> <targets_file> <result.tsv>
#   targets_file: ip 는 "vm ip", ldap 은 "host" (host 컬럼만 봄)
_stage_run() {
  local kind="$1" targets="$2" result="$3"
  : >"$result"

  local gossh_std;  gossh_std="$(conf_get gossh gossh)"
  local gossh_os6;  gossh_os6="$(conf_get os6_gossh "$gossh_std")"
  local bin_std bin_os6 runner
  case "$kind" in
    ip)   bin_std="$(conf_get ipchange_bin ./bin/ip-change-engine)"; runner=_run_ip ;;
    ldap) bin_std="$(conf_get ldap_bin ./bin/ldap-config-engine)";   runner=_run_ldap ;;
  esac
  local os6dir; os6dir="$(conf_get os6_bin_dir)"
  bin_os6="$bin_std"
  [ -n "$os6dir" ] && bin_os6="$os6dir/$(basename "$bin_std")"

  local t1 t2
  t1="$(conf_get timeout_pass1 60)"
  t2="$(conf_get timeout_pass2 600)"

  # 대상을 일반/OS6 로 분할
  local all_std all_os6 line h
  all_std="$(mktemp)"; all_os6="$(mktemp)"
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line%%$'\r'}"
    case "$line" in ''|'#'*) continue ;; esac
    h="${line%% *}"
    if _in_os6 "$h"; then echo "$line" >>"$all_os6"; else echo "$line" >>"$all_std"; fi
  done <"$targets"

  local grp bin gossh tf
  for grp in std os6; do
    if [ "$grp" = std ]; then tf="$all_std"; bin="$bin_std"; gossh="$gossh_std"
    else tf="$all_os6"; bin="$bin_os6"; gossh="$gossh_os6"; fi
    [ -s "$tf" ] || continue
    [ -x "$bin" ] || die G2 "엔진 바이너리가 없습니다: $bin (setup.sh 를 실행하세요)"
    command -v "$gossh" >/dev/null 2>&1 || [ -x "$gossh" ] || die G1 "gossh 를 찾을 수 없습니다: $gossh"

    log "${kind^^}" "[$grp] 1차 실행 ($(grep -c . "$tf")대, timeout ${t1}s)"
    "$runner" "$tf" "$t1" "$gossh" "$bin" "$result"

    # 2차: 1차에서 FAIL 이고 ping 되는 호스트만
    local retry; retry="$(mktemp)"
    local fh
    while IFS= read -r fh; do
      # targets_file 에서 그 host 로 시작하는 원본 줄을 되살림
      grep -E "^${fh}([[:space:]]|$)" "$tf" | head -1
    done < <(awk -F'\t' '$2=="FAIL"{print $1}' "$result" | sort -u) >"$retry.cand"
    : >"$retry"
    while IFS= read -r line || [ -n "$line" ]; do
      [ -n "$line" ] || continue
      h="${line%% *}"
      if _ping_ok "$h"; then echo "$line" >>"$retry"
      else log "${kind^^}" "[$grp] $h — ping 실패, 2차 재시도 안 함 (즉시 실패 확정)"; fi
    done <"$retry.cand"

    if [ -s "$retry" ]; then
      log "${kind^^}" "[$grp] 2차 재실행 ($(grep -c . "$retry")대, timeout ${t2}s)"
      local r2; r2="$(mktemp)"
      "$runner" "$retry" "$t2" "$gossh" "$bin" "$r2"
      # 2차 결과가 있는 호스트는 1차 결과를 덮어씀
      local rh rs
      while IFS=$'\t' read -r rh rs; do
        grep -v -P "^${rh}\t" "$result" >"$result.tmp" || true
        mv "$result.tmp" "$result"
        printf '%s\t%s\n' "$rh" "$rs" >>"$result"
      done <"$r2"
      rm -f "$r2"
    fi
    rm -f "$retry" "$retry.cand"
  done

  # targets 에 있는데 결과에 안 나온 호스트는 FAIL 로 확정
  local seen
  for h in $(_hosts_of "$targets"); do
    grep -qP "^${h}\t" "$result" || printf '%s\tFAIL\n' "$h" >>"$result"
  done

  rm -f "$all_std" "$all_os6"
  sort -u -o "$result" "$result"
}

# ── C 단계: IP 변경 ───────────────────────────────────────────────────────
# stage_ip <targets:"vm ip">  → RES_IP (tsv: host<TAB>OK|FAIL) 를 채움
stage_ip() {
  stage_begin C "IP 변경 (ip_change)"
  if [ "${DRY_RUN:-0}" -eq 1 ]; then
    # ip-change-engine 은 dry-run 을 지원하지 않습니다. 대상만 보여주고 넘어갑니다.
    log C "dry-run: $(grep -cve '^[[:space:]]*$' -e '^[[:space:]]*#' "$1")대에 IP 변경 예정 (엔진 미실행)"
    stage_end ok
    return 0
  fi
  render_confs
  RES_IP="$WORK/res_ip_${RUN_USER}.tsv"
  _stage_run ip "$1" "$RES_IP"
  local ok fail
  ok=$(awk -F'\t' '$2=="OK"' "$RES_IP" | wc -l)
  fail=$(awk -F'\t' '$2=="FAIL"' "$RES_IP" | wc -l)
  log C "IP 변경 결과 — OK ${ok} / FAIL ${fail}"
  [ "$fail" -eq 0 ] && stage_end ok || stage_end fail
}

# ── LDAP 대상 인프라 선택 (§11.4 보강) ─────────────────────────────────────
# integration.conf 에 기본값을 두지 않습니다(원본 ldap_setting 규칙 5와 동일한
# 이유 — 이전 작업의 인프라가 그대로 남아 조용히 다른 인프라에 적용되는 사고를
# 막기 위함). --infra 로 명시하거나, 대화형으로 매번 고릅니다.
# 결과: 전역 변수 INFRA
_ldap_infra_names() {  # <ldap_conf 경로> → 인프라 이름 목록(줄단위, 중복없음)
  local f="$1"
  [ -f "$f" ] || return 1
  grep -oE '^infra\.[^.[:space:]]+\.' "$f" | sed -E 's/^infra\.([^.]+)\.$/\1/' | sort -u
}

select_ldap_infra() {
  if [ -n "${INFRA_ARG:-}" ]; then
    INFRA="$INFRA_ARG"
    return
  fi

  local conf; conf="$(conf_get ldap_conf)"
  local names; names="$(_ldap_infra_names "$conf")"
  [ -n "$names" ] || die D2 "ldap_conf 에서 infra 목록을 찾을 수 없습니다: $conf"

  if [ -t 0 ]; then
    echo "대상 LDAP 인프라를 선택하십시오:"
    local n
    select n in $names; do
      [ -n "$n" ] && { INFRA="$n"; break; }
      echo "다시 선택하십시오."
    done
  else
    die D2 "LDAP 대상 인프라가 지정되지 않았습니다. --infra <이름> 으로 지정하십시오. (사용 가능: $(printf '%s' "$names" | tr '\n' ' '))"
  fi
}

# ── D 단계: LDAP ─────────────────────────────────────────────────────────
# stage_ldap <hosts_file:"host">  → RES_LDAP 채움
stage_ldap() {
  stage_begin D "LDAP 설정 (ldap_setting)"
  select_ldap_infra
  log D "대상 인프라: $INFRA"
  if [ "${DRY_RUN:-0}" -ne 1 ]; then
    ask_yes "LDAP 설정을 인프라 '$INFRA' 에 적용합니다. 계속하시겠습니까?" || die B1 "사용자가 중단했습니다."
  fi
  RES_LDAP="$WORK/res_ldap_${RUN_USER}.tsv"
  _stage_run ldap "$1" "$RES_LDAP"
  local ok fail
  ok=$(awk -F'\t' '$2=="OK"' "$RES_LDAP" | wc -l)
  fail=$(awk -F'\t' '$2=="FAIL"' "$RES_LDAP" | wc -l)
  log D "LDAP 결과 — OK ${ok} / FAIL ${fail}"
  [ "$fail" -eq 0 ] && stage_end ok || stage_end fail
}
