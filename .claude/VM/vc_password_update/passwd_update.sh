#!/usr/bin/env bash
# passwd_update.sh — vCenter / ESXi 비밀번호를 암호화해서 저장·갱신한다 (비밀번호가 바뀔 때마다 실행).
# 저장한 뒤에는 VMsetup/vm_setup.sh, vm-param-check 의 vm_setting_check_insert.sh 가
# 환경변수(VC_PASSWORD 등) 없이 이 파일을 풀어서 쓴다.
#
# 사용법:
#   ./passwd_update.sh                    vCenter 비밀번호 (계정 lscsystems@vsphere.local)
#   ./passwd_update.sh -id <계정>         다른 vCenter 계정
#   ./passwd_update.sh -esxi              ESXi 비밀번호 (계정 root)
#   ./passwd_update.sh -esxi -id <계정>   다른 ESXi 계정
#   ./passwd_update.sh -list              저장된 항목과 풀리는지 확인 (비밀번호는 보여주지 않음)
#
# 다른 서버로 옮길 때: 이 폴더의 secret/ (key 포함) 를 그대로 복사한다. OS6·OS8 모두 같은 파일로 풀린다.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=secret_lib.sh
. "$HERE/secret_lib.sh"

kind=vcenter; id=""; list=0
while [ $# -gt 0 ]; do
  case "$1" in
    -esxi) kind=esxi ;;
    -vcenter) kind=vcenter ;;
    -id) shift; id="${1:-}" ;;
    -id=*) id="${1#-id=}" ;;
    -list) list=1 ;;
    -h|--help) sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "알 수 없는 옵션: $1 (도움말: $0 -h)" >&2; exit 2 ;;
  esac
  shift
done
command -v openssl >/dev/null 2>&1 || { echo "[오류] openssl 이 없습니다." >&2; exit 1; }

if [ "$list" -eq 1 ]; then
  echo "암호 폴더: $SECRET_DIR"
  found=0
  for f in "$SECRET_DIR"/vcenter_*.enc "$SECRET_DIR"/esxi_*.enc; do
    [ -f "$f" ] || continue
    found=1; b="$(basename "$f" .enc)"; k="${b%%_*}"; a="${b#*_}"
    if secret_get "$k" "$a" >/dev/null && [ -n "$(secret_get "$k" "$a")" ]; then st="풀림"; else st="못 품(키가 다름?)"; fi
    printf '  %-8s %-36s %s  (%s)\n' "$k" "$a" "$st" "$(date -r "$f" '+%F %T' 2>/dev/null)"
  done
  [ "$found" -eq 1 ] || echo "  (저장된 비밀번호 없음 — $0 로 등록하세요)"
  exit 0
fi

[ -n "$id" ] || { [ "$kind" = esxi ] && id=root || id=lscsystems@vsphere.local; }
printf '%s 계정 %s 의 새 비밀번호: ' "$kind" "$id" >&2; IFS= read -r -s p1 || exit 1; echo >&2
printf '한 번 더: ' >&2; IFS= read -r -s p2 || exit 1; echo >&2
[ -n "$p1" ] || { echo "[오류] 비밀번호가 비어 있습니다." >&2; exit 1; }
[ "$p1" = "$p2" ] || { echo "[오류] 두 번 입력한 비밀번호가 다릅니다." >&2; exit 1; }

secret_put "$kind" "$id" "$p1" || { echo "[오류] 저장하지 못했습니다: $(secret_file "$kind" "$id")" >&2; exit 1; }
[ "$(secret_get "$kind" "$id")" = "$p1" ] || { echo "[오류] 저장은 했지만 다시 풀리지 않습니다." >&2; exit 1; }
echo "저장 완료: $(secret_file "$kind" "$id") (키: $SECRET_DIR/key)"
