#!/usr/bin/env bash
# mac_ip_resolve.sh — mac_info 목록(mac_all.txt)의 IP 칸 "<VM이름>_DNS_AND_TOOLS_NOT_FOUND" 를 실제 IP 로 바꾼다.
#
# VM 이름을 이 서버의 DNS·/etc/hosts 로 조회한다(IPv4 만). 이름 그대로 안 나오면 "<VM이름>.<도메인>" 으로 한 번 더 찾는다.
# 못 찾은 줄은 그대로 두고 개수만 알려 준다. 이미 IP 가 들어 있는 줄은 건드리지 않는다.
#
# 사용법: mac_ip_resolve.sh <파일> [도메인]     (도메인 기본값: 환경변수 MAC_DOMAIN, 없으면 saccae.com)
#   파일을 그 자리에서 바꾸고, 원본은 <파일>.bak 로 남긴다.
# vm_setup.sh 의 resolve_mac_ips 함수와 같은 동작이다(vm_setup.sh 는 이 파일 없이도 혼자 동작한다).
set -uo pipefail
[ $# -ge 1 ] && [ -f "$1" ] || { echo "사용법: $0 <mac_all.txt> [도메인]" >&2; exit 2; }
FILE="$1"; DOM="${2-${MAC_DOMAIN-saccae.com}}"

resolve_mac_ips() {
  local f="$1" dom="${2-}" tmp line vm ip n=0 miss=0
  tmp="$(mktemp)"
  while IFS= read -r line || [ -n "$line" ]; do
    vm="$(awk '{print $4}' <<< "$line")"
    if [ -n "$vm" ] && [ "$(awk '{print $5}' <<< "$line")" = "${vm}_DNS_AND_TOOLS_NOT_FOUND" ]; then
      ip="$(getent ahostsv4 "$vm" 2>/dev/null | awk 'NR==1{print $1}')"
      [ -z "$ip" ] && [ -n "$dom" ] && ip="$(getent ahostsv4 "$vm.${dom#.}" 2>/dev/null | awk 'NR==1{print $1}')"
      if [ -n "$ip" ]; then line="$(awk -v ip="$ip" '{$5=ip}1' <<< "$line")"; n=$((n + 1)); else miss=$((miss + 1)); fi
    fi
    printf '%s\n' "$line" >> "$tmp"
  done < "$f"
  cat "$tmp" > "$f"; rm -f "$tmp"
  RESOLVED=$n; UNRESOLVED=$miss
}

cp "$FILE" "$FILE.bak"
resolve_mac_ips "$FILE" "$DOM"
echo "[완료] IP 변환 ${RESOLVED}줄 / 못 찾음 ${UNRESOLVED}줄 (원본 백업: $FILE.bak)"
