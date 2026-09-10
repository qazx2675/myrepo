# lib/incident.sh — 인시던트 저장/재개 (§5.3) 와 롤백 (§7)
#
# 통합 스크립트는 자체 백업을 만들지 않습니다. 세 프로젝트가 남긴 백업의
# 위치·시점을 incidents/<name>/ 에 인덱싱만 하고, 롤백은 각 프로젝트의
# 기존 롤백 경로를 역순으로 호출합니다.
#
# change.sh 가 source 합니다.

INC_DIR_BASE="incidents"

# incident_save <name> <stage_reached>
#   stage_reached: "ip_ldap"(포트그룹 남음) 등
incident_save() {
  local name="$1" reached="$2"
  local d="$INC_DIR_BASE/$name"
  mkdir -p "$d" || die F1 "인시던트 디렉터리를 만들 수 없습니다: $d"

  {
    echo "name=$name"
    echo "user=$RUN_USER"
    echo "created=$(date '+%Y-%m-%d %H:%M:%S')"
    echo "stage_reached=$reached"
    echo "ldap_stamp_hint=${LDAP_STAMP_HINT:-}"
    echo "ldap_infra=${INFRA:-}"
  } >"$d/meta"

  # 결과 목록 사본 (롤백 대상 산정용)
  local f
  for f in on_off ip_ok ldap_ok on_off.failed failed; do
    [ -f "$RESULT_DIR/${f}_${RUN_USER}.txt" ] && cp -p "$RESULT_DIR/${f}_${RUN_USER}.txt" "$d/" 2>/dev/null || true
  done
  cp -p "$RESULT_DIR/on_off_${RUN_USER}.failed.txt" "$d/" 2>/dev/null || true

  # 표준화된 vswitch (포트그룹 재개용)
  [ -f "$WORK/vswitch_${RUN_USER}.std.txt" ] && cp -p "$WORK/vswitch_${RUN_USER}.std.txt" "$d/"
  [ -f "${RUN_USER}.txt" ] && cp -p "${RUN_USER}.txt" "$d/"

  # 백업 인덱스 (§7.1) — 어디에 무엇이 있는지만 기록
  cat >"$d/backup_index.txt" <<EOF
# 인시던트 $name — 백업 위치 인덱스 (통합 스크립트는 새 백업을 만들지 않음)

[ip_change]
대상            : ip_ok_${RUN_USER}.txt 의 VM 들
원격 백업       : 각 노드의 <ifcfg>.bak.<STAMP>  (apply_body.sh 가 생성)
자동 롤백       : 없음 (ip_change 에 rollback 서브커맨드 없음)
수동 롤백       : 각 노드에서 <ifcfg>.bak.<STAMP> 를 원래 이름으로 복원

[ldap_setting]
대상            : ldap_ok_${RUN_USER}.txt 의 호스트들
원격 백업       : 각 노드의 <설정파일>.bak.<STAMP>
자동 롤백       : ldap-config-engine -rollback  (가장 최근 시점)
시점 지정       : ldap-config-engine -rollback-to <STAMP>   (-list-backups 로 확인)
시점 힌트       : ${LDAP_STAMP_HINT:-(미기록)}

[portgroup / vm-network-migration]
상태 파일       : state_${RUN_USER}.json  (이 디렉터리에 사본)
자동 롤백       : nm/run.sh --rollback  (state 파일 기준 역순 원복)
EOF

  # 포트그룹까지 갔다면 상태 파일도 보관
  [ -f "$(conf_get nm_dir ./projects/vm-network-migration)/state_${RUN_USER}.json" ] && \
    cp -p "$(conf_get nm_dir ./projects/vm-network-migration)/state_${RUN_USER}.json" "$d/" 2>/dev/null || true

  log F "인시던트 저장: $d"
}

incident_list() {
  local d
  [ -d "$INC_DIR_BASE" ] || return
  for d in "$INC_DIR_BASE"/*/; do
    [ -f "$d/meta" ] || continue
    local n u c s
    n=$(sed -n 's/^name=//p' "$d/meta")
    u=$(sed -n 's/^user=//p' "$d/meta")
    c=$(sed -n 's/^created=//p' "$d/meta")
    s=$(sed -n 's/^stage_reached=//p' "$d/meta")
    printf '  %-24s user=%-10s %s  (남은단계: %s)\n' "$n" "$u" "$c" "$s"
  done
}

# incident_load <name>  — RUN_USER 등을 인시던트에서 복원
incident_load() {
  local name="$1" d="$INC_DIR_BASE/$1"
  [ -f "$d/meta" ] || die F3 "인시던트를 찾을 수 없습니다: $name"
  RUN_USER=$(sed -n 's/^user=//p' "$d/meta")
  INC_STAGE_REACHED=$(sed -n 's/^stage_reached=//p' "$d/meta")
  LDAP_STAMP_HINT=$(sed -n 's/^ldap_stamp_hint=//p' "$d/meta")
  INFRA=$(sed -n 's/^ldap_infra=//p' "$d/meta")
  INC_DIR="$d"
  [ -n "$RUN_USER" ] || die F2 "인시던트 meta 에 user 가 없습니다: $d/meta"
  log F "인시던트 로드: $name (user=$RUN_USER, 남은단계=$INC_STAGE_REACHED)"
}

# rollback_incident <name>  — 역순(포트그룹 → LDAP → IP)
rollback_incident() {
  incident_load "$1"
  local d="$INC_DIR"
  local nm_dir; nm_dir="$(conf_get nm_dir ./projects/vm-network-migration)"

  log ROLLBACK "== 인시던트 $1 롤백 시작 (역순) =="

  # 1) 포트그룹
  if [ -f "$d/state_${RUN_USER}.json" ]; then
    log ROLLBACK "[1/3] 포트그룹 원복 (nm/run.sh --rollback)"
    cp -p "$d/state_${RUN_USER}.json" "$nm_dir/state_${RUN_USER}.json"
    cp -p "$d/vswitch_${RUN_USER}.std.txt" "$nm_dir/vswitch_${RUN_USER}.txt" 2>/dev/null || true
    cp -p "$(conf_get vcenter_file ./vcenter.txt)" "$nm_dir/vcenter.txt" 2>/dev/null || true
    ( cd "$nm_dir" && VC_PASSWORD="${VC_PASSWORD:-}" ./run.sh --rollback -y \
        -u "$RUN_USER" --id "$(conf_get vc_id lscsystems@vsphere.local)" ) \
      || log ROLLBACK "포트그룹 롤백에서 오류 — nm 로그 확인"
    rm -f "$nm_dir/vswitch_${RUN_USER}.txt" "$nm_dir/vcenter.txt"
  else
    log ROLLBACK "[1/3] 포트그룹 단계 기록 없음 — 건너뜀"
  fi

  # 2) LDAP
  if [ -s "$d/ldap_ok_${RUN_USER}.txt" ]; then
    if [ -z "${INFRA:-}" ]; then
      log ROLLBACK "[2/3] 인시던트에 저장된 LDAP 인프라가 없습니다 — 자동 롤백을 건너뜁니다."
      warn_box "이 인시던트는 --infra 도입 이전에 저장된 것입니다. 아래 명령으로 수동 실행하십시오:" \
        "  $(conf_get ldap_bin ./bin/ldap-config-engine) -config $(conf_get ldap_conf) -assets $(conf_get ldap_assets) -infra <원래 인프라> -host-file $d/ldap_ok_${RUN_USER}.txt -rollback ${LDAP_STAMP_HINT:+-rollback-to $LDAP_STAMP_HINT} -gossh $(conf_get gossh gossh) -u $SSH_USER -P $SSH_PORT"
    else
      log ROLLBACK "[2/3] LDAP 원복 (ldap-config-engine -rollback, 인프라=$INFRA)"
      local rbto=""
      [ -n "$LDAP_STAMP_HINT" ] && rbto="-rollback-to $LDAP_STAMP_HINT"
      # shellcheck disable=SC2086
      NO_COLOR=1 "$(conf_get ldap_bin ./bin/ldap-config-engine)" \
        -config "$(conf_get ldap_conf)" -assets "$(conf_get ldap_assets)" \
        -infra "$INFRA" -host-file "$d/ldap_ok_${RUN_USER}.txt" \
        -rollback $rbto \
        -gossh "$(conf_get gossh gossh)" -u "$SSH_USER" -p "$GOSSH_PW" -P "$SSH_PORT" \
        | tee -a "$LOG_FILE" \
        || log ROLLBACK "LDAP 롤백에서 오류 — 로그 확인"
    fi
  else
    log ROLLBACK "[2/3] LDAP 성공 기록 없음 — 건너뜀"
  fi

  # 3) IP — 자동 롤백 없음
  if [ -s "$d/ip_ok_${RUN_USER}.txt" ]; then
    warn_box "IP 변경은 자동 롤백이 없습니다. 아래 VM 들은 각 노드에서 <ifcfg>.bak.<STAMP> 를 수동 복원하십시오:"
    sed 's/^/    /' "$d/ip_ok_${RUN_USER}.txt" | tee -a "$LOG_FILE"
  else
    log ROLLBACK "[3/3] IP 변경 성공 기록 없음 — 건너뜀"
  fi

  log ROLLBACK "== 인시던트 $1 롤백 종료 =="
}
