# lib/results.sh — 결과 리다이렉션 (§6)
#
# 입력: RES_IP, RES_LDAP  (각각 tsv: "<vm이름>\t<OK|FAIL>")
# 출력: RESULT_DIR 아래 5종
#   on_off_${user}.txt          IP·LDAP 둘 다 성공 (별도 전원 on/off 작업용)
#   ip_ok_${user}.txt           IP 성공
#   ldap_ok_${user}.txt         LDAP 성공
#   on_off_${user}.failed.txt   하나라도 실패한 VM 이름만 (재시도 입력용)
#   failed_${user}.txt          "<vm이름> <에러코드>"  (C2=IP실패 / D3=LDAP실패 / CD=둘다)
#
# 기존 파일은 .bak.<stamp> 로 백업 후 새로 씁니다.
#
# change.sh 가 source 합니다. 필요 변수: RESULT_DIR, RUN_USER, RES_IP, RES_LDAP

_backup_then() {
  local f="$1"
  [ -f "$f" ] && cp -p "$f" "$f.bak.$(date +%Y%m%d%H%M%S)"
  : >"$f"
}

_status_of() { # _status_of <tsv> <vm>  → OK|FAIL|"" (없으면 빈값)
  awk -F'\t' -v v="$2" '$1==v{print $2; found=1} END{if(!found) print ""}' "$1"
}

write_results() {
  mkdir -p "$RESULT_DIR"
  local onoff="$RESULT_DIR/on_off_${RUN_USER}.txt"
  local ipok="$RESULT_DIR/ip_ok_${RUN_USER}.txt"
  local ldok="$RESULT_DIR/ldap_ok_${RUN_USER}.txt"
  local onoff_f="$RESULT_DIR/on_off_${RUN_USER}.failed.txt"
  local failed="$RESULT_DIR/failed_${RUN_USER}.txt"
  local f
  for f in "$onoff" "$ipok" "$ldok" "$onoff_f" "$failed"; do _backup_then "$f"; done

  # 대상 VM 전체 = RES_IP 와 RES_LDAP 에 나온 이름의 합집합
  local vms
  vms="$( { awk -F'\t' '{print $1}' "$RES_IP" 2>/dev/null; \
            awk -F'\t' '{print $1}' "$RES_LDAP" 2>/dev/null; } | sort -u )"

  local v si sl
  while IFS= read -r v; do
    [ -n "$v" ] || continue
    si="$(_status_of "$RES_IP" "$v")"
    sl="$(_status_of "$RES_LDAP" "$v")"
    [ "$si" = OK ] && echo "$v" >>"$ipok"
    [ "$sl" = OK ] && echo "$v" >>"$ldok"
    if [ "$si" = OK ] && [ "$sl" = OK ]; then
      echo "$v" >>"$onoff"
    else
      echo "$v" >>"$onoff_f"
      # 둘 다 실패면 CD, IP 만 C2, LDAP 만 D3
      local code
      if [ "$si" != OK ] && [ "$sl" != OK ]; then code="CD"
      elif [ "$si" != OK ]; then code="C2"
      else code="D3"; fi
      echo "$v $code" >>"$failed"
    fi
  done <<<"$vms"

  log RESULT "on_off=$(grep -c . "$onoff" 2>/dev/null || echo 0)  실패=$(grep -c . "$onoff_f" 2>/dev/null || echo 0)  → $RESULT_DIR"
}

# retry 대상: failed_${user}.txt 에서 특정 단계 실패 VM 이름만
# retry_vms <ip|ldap>
retry_vms() {
  local kind="$1" failed="$RESULT_DIR/failed_${RUN_USER}.txt"
  [ -f "$failed" ] || die F3 "재시도할 실패 목록이 없습니다: $failed"
  case "$kind" in
    ip)   awk '$2=="C2"||$2=="CD"{print $1}' "$failed" ;;
    ldap) awk '$2=="D3"||$2=="CD"{print $1}' "$failed" ;;
    *)    die F2 "retry_vms: 알 수 없는 종류 $kind" ;;
  esac
}
