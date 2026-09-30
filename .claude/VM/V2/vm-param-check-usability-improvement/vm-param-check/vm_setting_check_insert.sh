#!/usr/bin/env bash
# vm_setting_check_insert.sh — 폴더명 기반 스펙 자동매칭(-specRoot)으로 체크를 실행하는
# 보조 스크립트. README.md "빠른 시작" 4~7번을 한 번에 실행하는 셸이다.
#
# user: -u <user> 로 지정. 없으면 그냥 실행 시 번호 메뉴에서 고른다(vm_setup.sh 와 같은 양식: 0) 직접 선택).
# user 값에 따라 대상 목록 파일과 출력 CSV 이름이 정해진다:
#   -f "${user}.txt"  →  같은 폴더에 "<user>.txt"(한 줄에 VM hostname 하나씩)가 있어야 함
#   -out "result_${user}.csv"
# vm_setup.sh 로 만든 직후라면(VMsetup/run_<user>/check_specs.txt 가 있으면) 그때 쓴 스펙 폴더로 물어보지 않고 체크한다.
set -euo pipefail
cd "$(dirname "$0")"

# 이전 실행에서 잘못된 값으로 export 해 둔 비밀번호가 셸에 남아 있으면 암호 파일을 건너뛰고
# 그 값으로 로그인 실패가 나는 사고가 반복돼서, 실행할 때마다 지우고 시작한다.
unset VC_PASSWORD VC_PASS VCENTER_PASS

# ===== 환경 설정 (vm_setup.sh 와 동일한 -id / 암호 파일 방식) =====
# 계정: 기본 lscsystems@vsphere.local, 다른 계정은 -id <계정> (또는 환경변수 VC_ID)
# 비밀번호: 항상 V2 폴더 secret/ 의 암호 파일(../../passwd_update.sh 로 등록) → 직접 입력 순.
VC_ID="${VC_ID:-lscsystems@vsphere.local}"
user=""; want_fix=0; spec_arg=""
while [ $# -gt 0 ]; do
  case "$1" in
    -id) shift; VC_ID="${1:-}" ;;
    -id=*) VC_ID="${1#-id=}" ;;
    -u) shift; user="${1:-}" ;;
    -u=*) user="${1#-u=}" ;;
    -fix) want_fix=1 ;;
    -specFolder) shift; spec_arg="${1:-}" ;;
    -specFolder=*) spec_arg="${1#-specFolder=}" ;;
    *) echo "알 수 없는 옵션: $1 (사용법: $0 [-u <user>] [-id <계정>] [-specFolder <스펙 폴더명>] [-fix])" >&2; exit 2 ;;
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

# ===== vm_setup.sh 로 만든 직후: 그때 쓴 스펙 폴더를 물어보지 않고 그대로 쓴다 =====
# vm_setup.sh 가 run_<user>/check_specs.txt 에 "스펙 폴더<TAB>대상 VM 목록" 을 남긴다. 있으면 스펙마다
# -specFolder + -yes 로 체크한다(스펙 폴더·대상 목록·vCenter 모두 그 실행 기준이라 추가 입력이 없다).
# 설정 변경(-fix)은 하지 않고 체크만 한다. 고치려면 이 스크립트에 -fix 를 붙인다(변경 직전 확인은 vm-param-check 가 한다).
RUN_SPECS="../../VMsetup/run_${user}/check_specs.txt"
if [ -z "$spec_arg" ] && [ -s "$RUN_SPECS" ]; then
  export VC_USER="$VC_ID" VC_PASS="$VC_PASSWORD"
  auto_fix=(); [ "$want_fix" = 1 ] && auto_fix=(-fix)
  echo "=== vm_setup.sh 실행(run_${user})의 스펙 폴더로 자동 체크합니다 — 추가 입력 없음 ==="
  n=0; bad=0
  while IFS=$'\t' read -r sf tf; do
    [ -n "$sf" ] && [ -f "$tf" ] || continue
    n=$((n + 1))
    echo "--- 스펙 ${n}: ${sf} (대상 ${tf}) ---"
    ./vm-param-check -vcenterList="../../VMsetup/run_${user}/vcenter_check.txt" -f="$tf" -specRoot="$SPEC_ROOT" \
      -specFolder="$sf" -yes -out="result_${user}_${n}.csv" ${auto_fix[@]+"${auto_fix[@]}"} || bad=1
  done < "$RUN_SPECS"
  [ "$n" -gt 0 ] || { echo "run_${user}/check_specs.txt 에 쓸 수 있는 스펙이 없습니다." >&2; exit 1; }
  exit "$bad"
fi

target_file="${user}.txt"
out_file="result_${user}.csv"

if [ ! -f "$target_file" ]; then
  echo "대상 목록 파일이 없습니다: $target_file (한 줄에 VM hostname 하나씩 적어두세요)" >&2
  exit 1
fi

# vm-param-check 바이너리는 VC_USER/VC_PASS 환경변수를 읽는다 — 위에서 받은 VC_ID/VC_PASSWORD 를 그대로 넘긴다.
export VC_USER="$VC_ID" VC_PASS="$VC_PASSWORD"

# 스펙 폴더명을 한 번만 물어 모든 대상 VM 에 적용한다. VM 이 CAE 규칙에 안 맞는 vCenter 폴더(예: Task)에 있고
# 포트그룹명으로도 스펙을 못 정하면 vm-param-check 가 VM 마다 폴더명을 물어 무인으로는 진행이 안 되기 때문이다.
# 비워 두면 예전처럼 vCenter 폴더/포트그룹으로 자동매칭하고 확인 질문(y/N)을 받는다.
# -specFolder <이름> 을 주면 질문 없이 그 스펙을 쓴다(run_<user> 기록이 있어도 이 옵션이 우선).
spec_args=()
spec_folder="$spec_arg"
[ -n "$spec_folder" ] || read -r -p "스펙 폴더명 (모든 VM 에 이 스펙 적용, Enter = 자동매칭): " spec_folder
if [ -n "$spec_folder" ]; then
  spec_args=(-specFolder="$spec_folder" -yes)
fi

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
  ${spec_args[@]+"${spec_args[@]}"} \
  "${fix_args[@]}"
