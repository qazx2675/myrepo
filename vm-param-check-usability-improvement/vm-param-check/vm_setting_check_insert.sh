#!/usr/bin/env bash
# vm_setting_check_insert.sh — 폴더명 기반 스펙 자동매칭(-specRoot)으로 체크를 실행하는
# 보조 스크립트. README.md "빠른 시작" 4~7번을 한 번에 실행하는 셸이다.
#
# user: -u <user> 로 지정. 없으면 그냥 실행 시 번호 메뉴에서 고른다(vm_setup.sh 와 같은 양식: 0) 직접 선택).
# user 값에 따라 대상 목록 파일과 출력 CSV 이름이 정해진다:
#   -f "${user}.txt"  →  같은 폴더에 "<user>.txt"(한 줄에 VM hostname 하나씩)가 있어야 함
#   -out "result_${user}.csv"
set -euo pipefail
cd "$(dirname "$0")"

# 이전 실행에서 잘못된 값으로 export 해 둔 비밀번호가 셸에 남아 있으면 암호 파일을 건너뛰고
# 그 값으로 로그인 실패가 나는 사고가 반복돼서, 실행할 때마다 지우고 시작한다.
unset VC_PASSWORD VC_PASS VCENTER_PASS

# ===== 환경 설정 (vm_setup.sh 와 동일한 -id / 암호 파일 방식) =====
# 계정: 기본 lscsystems@vsphere.local, 다른 계정은 -id <계정> (또는 환경변수 VC_ID)
# 비밀번호: 항상 V2 폴더 secret/ 의 암호 파일(../../passwd_update.sh 로 등록) → 직접 입력 순.
VC_ID="${VC_ID:-lscsystems@vsphere.local}"
user=""
while [ $# -gt 0 ]; do
  case "$1" in
    -id) shift; VC_ID="${1:-}" ;;
    -id=*) VC_ID="${1#-id=}" ;;
    -u) shift; user="${1:-}" ;;
    -u=*) user="${1#-u=}" ;;
    *) echo "알 수 없는 옵션: $1 (사용법: $0 [-u <user>] [-id <계정>])" >&2; exit 2 ;;
  esac
  shift
done
if [ -z "${VC_PASSWORD:-}" ] && [ -f ../../secret_lib.sh ]; then
  . ../../secret_lib.sh
  if VC_PASSWORD="$(secret_get vcenter "$VC_ID")" && [ -n "$VC_PASSWORD" ]; then
    echo "$VC_ID 비밀번호: 암호 파일에서 읽었습니다 ($(secret_file vcenter "$VC_ID"))"
  else
    VC_PASSWORD=""
  fi
fi
if [ -z "${VC_PASSWORD:-}" ]; then
  read -r -s -p "$VC_ID 비밀번호: " VC_PASSWORD; echo
fi
VCENTER_LIST='vcenter.txt'
SPEC_ROOT='./SPEC_DIR'
# V2 배치(/home/SPEC_DIR, /home/vm-param-check-usability-improvement/vm-param-check): 이 폴더 안에 SPEC_DIR 이
# 없고 나란히 있는 공유 SPEC_DIR 이 있으면 그쪽을 쓴다 (VMsetup/vm_setup.sh 와 같은 스펙 폴더를 공유).
[ -d "$SPEC_ROOT" ] || { [ -d ../../SPEC_DIR ] && SPEC_ROOT='../../SPEC_DIR'; }

# ===== user 선택 =====
# -u <user> 로 바로 지정. 없으면 번호 메뉴에서 고른다(vm_setup.sh 와 같은 양식: 0) 직접 선택).
# 이 값으로 "<user>.txt"(대상 VM hostname 목록)를 읽고 "result_<user>.csv"를 만든다.
if [ -z "$user" ]; then
  users=(lsh ljh dhk)
  while :; do
    echo >&2
    echo "=== user 선택 ($(pwd)) ===" >&2
    echo "  0) 직접 선택" >&2
    for i in "${!users[@]}"; do printf '  %d) %s\n' "$((i + 1))" "${users[$i]}" >&2; done
    read -r -p "번호: " ans
    if [ "$ans" = "0" ]; then
      read -r -p "user 이름 입력: " user
      [ -n "$user" ] && break
      continue
    fi
    [[ "$ans" =~ ^[0-9]+$ ]] || continue
    [ "$ans" -ge 1 ] && [ "$ans" -le "${#users[@]}" ] && { user="${users[$((ans - 1))]}"; break; }
  done
fi

if [ ! -x ./vm-param-check ]; then
  echo "vm-param-check 바이너리가 없어 먼저 빌드합니다..."
  bash setup.sh
fi

target_file="${user}.txt"
out_file="result_${user}.csv"

if [ ! -f "$target_file" ]; then
  echo "대상 목록 파일이 없습니다: $target_file (한 줄에 VM hostname 하나씩 적어두세요)" >&2
  exit 1
fi

# vm-param-check 바이너리는 VC_USER/VC_PASS 환경변수를 읽는다 — 위에서 받은 VC_ID/VC_PASSWORD 를 그대로 넘긴다.
export VC_USER="$VC_ID" VC_PASS="$VC_PASSWORD"

fix_args=()
read -r -p "체크 후 실제로 설정을 변경(-fix)하시겠습니까? (y/N): " do_fix
if [[ "$do_fix" == "y" || "$do_fix" == "Y" ]]; then
  echo "-fix를 포함해서 실행합니다. (실제 변경 여부는 vm-param-check 자체에서 다시 한 번 확인받습니다 — 이중 확인)"
  fix_args=(-fix)
else
  echo "체크만 수행합니다 (-fix 없음)."
fi

echo
echo "=== 체크 실행: 대상=${target_file}, 출력=${out_file} ==="
./vm-param-check \
  -vcenterList="$VCENTER_LIST" \
  -f="$target_file" \
  -specRoot="$SPEC_ROOT" \
  -out="$out_file" \
  "${fix_args[@]}"
