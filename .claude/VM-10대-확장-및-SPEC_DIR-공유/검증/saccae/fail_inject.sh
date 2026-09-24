#!/usr/bin/env bash
# fail_inject.sh — 실패 테스트용으로 가상 vCenter(vcsim.saccae.com)의 상태를 일부러 망가뜨린다 (govc 사용).
#   fail_inject.sh poweron <VM>         VM 전원을 켠다                      (vm-param-check -fix 의 전원 OFF 게이트)
#   fail_inject.sh mem <VM> <MB>        VM 메모리를 바꾼다                  (vm_setup 끝의 스펙 체크에서 [차이])
# 원래대로 돌리려면 vcsim.sh restart (가상 vCenter 전체 초기화).
set -euo pipefail
D="$(cd "$(dirname "$0")" && pwd)"
V2="$(dirname "$D")"
command -v govc >/dev/null 2>&1 || export PATH="$PATH:/root/go/bin"
. "$V2/secret_lib.sh"
export GOVC_URL=vcsim.saccae.com GOVC_INSECURE=1 GOVC_USERNAME="${VC_ID:-lscsystems@vsphere.local}"
GOVC_PASSWORD="${VC_PASSWORD:-$(secret_get vcenter "$GOVC_USERNAME")}" || { echo "비밀번호를 못 읽었습니다 — $V2/passwd_update.sh 로 등록하세요." >&2; exit 1; }
export GOVC_PASSWORD

case "${1:-}" in
  poweron)
    govc vm.power -on "${2:?VM 이름}" ;;
  mem)
    govc vm.change -vm "${2:?VM 이름}" -m "${3:?메모리 MB}"
    echo "$2 메모리를 $3 MB 로 바꿨습니다." ;;
  *) sed -n '2,5p' "$0" | sed 's/^# \{0,1\}//'; exit 2 ;;
esac
