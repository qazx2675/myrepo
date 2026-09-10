# lib/conf.sh — 중앙 integration.conf 를 각 프로젝트 conf 로 렌더링 (§2.2)
#
# 지금은 ip_change.conf 만 렌더링합니다.
#   - ldap_setting: infra×site 스키마가 커서 운영자가 직접 관리합니다. 통합은
#     -config 로 그 파일을 가리키고 -default-site 플래그만 넘깁니다.
#   - vm-network-migration: 설정을 전부 플래그로 받으므로 렌더링할 파일이 없습니다.
#
# change.sh 가 source 합니다. 필요 변수: CONF_DIR

render_confs() {
  local dst="$CONF_DIR/ip_change.conf"
  {
    echo "# 자동 생성됨 — integration.conf 에서 렌더링. 직접 고치지 마십시오."
    printf 'network_scripts_dir=%s\n' \
      "$(conf_get ipchange.network_scripts_dir /etc/sysconfig/network-scripts)"
    local r9; r9="$(conf_get ipchange.rhel9_path)"
    if [ -n "$r9" ]; then printf 'rhel9_path=%s\n' "$r9"; fi
  } >"$dst" || die G2 "conf 렌더링 실패: $dst"
  dlog 1 "렌더링 완료: $dst"
}
