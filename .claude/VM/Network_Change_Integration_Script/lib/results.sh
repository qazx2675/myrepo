# lib/results.sh — 결과 리다이렉션 (§6)
#
# 입력: RES_IP, RES_LDAP  (각각 tsv: "<vm이름>\t<OK|FAIL>", LDAP 은 NOCHANGE 도 있음)
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

write_results() {
  mkdir -p "$RESULT_DIR"
  local onoff="$RESULT_DIR/on_off_${RUN_USER}.txt"
  local ipok="$RESULT_DIR/ip_ok_${RUN_USER}.txt"
  local ldok="$RESULT_DIR/ldap_ok_${RUN_USER}.txt"
  local onoff_f="$RESULT_DIR/on_off_${RUN_USER}.failed.txt"
  local failed="$RESULT_DIR/failed_${RUN_USER}.txt"
  local f
  for f in "$onoff" "$ipok" "$ldok" "$onoff_f" "$failed"; do _backup_then "$f"; done

  # 대상 VM 전체 = RES_IP 와 RES_LDAP 에 나온 이름의 합집합(정렬).
  # VM 마다 awk 를 두 번씩 띄우던 것을 awk 한 번으로(대상이 많을 때 수 초 → 즉시).
  # LDAP 의 NOCHANGE(이미 원하는 설정)는 성공으로 치되, 이번에 바꾼 게 없으므로
  # ldap_ok(롤백 대상)에는 넣지 않습니다.
  { awk -F'\t' '{print $1}' "$RES_IP" 2>/dev/null
    awk -F'\t' '{print $1}' "$RES_LDAP" 2>/dev/null; } | sort -u |
  awk -v ipf="$RES_IP" -v ldf="$RES_LDAP" \
      -v onoff="$onoff" -v ipok="$ipok" -v ldok="$ldok" -v onoff_f="$onoff_f" -v failed="$failed" '
    function load(f, arr,   l, a) {
      while ((getline l < f) > 0) { split(l, a, "\t"); arr[a[1]] = a[2] }
      close(f)
    }
    BEGIN { load(ipf, si); load(ldf, sl) }
    NF == 0 { next }
    {
      v = $1; i = si[v]; d = sl[v]
      iok = (i == "OK"); dok = (d == "OK" || d == "NOCHANGE")
      if (iok) print v >> ipok
      if (d == "OK") print v >> ldok
      if (iok && dok) { print v >> onoff; next }
      print v >> onoff_f
      # 둘 다 실패면 CD, IP 만 C2, LDAP 만 D3
      print v " " ((!iok && !dok) ? "CD" : (!iok ? "C2" : "D3")) >> failed
    }'

  local n_on n_f
  n_on="$(grep -c . "$onoff" 2>/dev/null)"; n_f="$(grep -c . "$onoff_f" 2>/dev/null)"
  log RESULT "on_off=${n_on:-0}  실패=${n_f:-0}  → $RESULT_DIR"
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
