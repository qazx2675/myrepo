#!/usr/bin/env bash
# vm_setting_check_insert.sh — 폴더명 기반 스펙 자동매칭(-specRoot)으로 체크를 실행하는
# 보조 스크립트. README.md "빠른 시작" 4~7번을 한 번에 실행하는 셸이다.
#
# user 값에 따라 대상 목록 파일과 출력 CSV 이름이 정해진다:
#   -f "${user}.txt"  →  같은 폴더에 "<user>.txt"(한 줄에 VM hostname 하나씩)가 있어야 함
#   -out "result_${user}.csv"
set -euo pipefail
cd "$(dirname "$0")"

# ===== 환경 설정 (직접 채우세요) =====
VC_USER='administrator@vsphere.local'
VC_PASS='...'
VCENTER_LIST='vcenter.txt'
SPEC_ROOT='./SPEC_DIR'

# ===== user 변수 지정 (직접 채우세요) =====
# 이 값으로 "<user>.txt"(대상 VM hostname 목록)를 읽고 "result_<user>.csv"를 만든다.
set_user() {
  user=""   # 예: user="kdh"
}

set_user

if [ -z "${user:-}" ]; then
  echo "set_user() 함수 안의 user 변수를 채워주세요 (예: user=\"kdh\")." >&2
  exit 1
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

export VC_USER VC_PASS

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
