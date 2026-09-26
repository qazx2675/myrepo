#!/usr/bin/env bash
# vm_setup.sh — SPEC_DIR 스펙으로 VM을 만들고 설정하는 실행 편의 스크립트.
#
# 흐름:
#   1) user 선택(-u 가 없으면 이 폴더의 <user>.txt 목록에서 번호로) → ${user}.txt(BM 목록) + ${HERE}/vswitch_${user}.txt 읽기
#   2) BM별 스펙 자동 할당(포트그룹 이름의 <폴더명>-cae-a-b-c-d 에서 폴더명 추출, CAE 번호는 무시하고 매칭)
#      → 못 정한 BM 만 SPEC_DIR 목록에서 선택, 목록에 없으면 vim으로 새 스펙 입력(+ev별 affinity)
#   3) VM(evNN)별 포트그룹(네트워크 어댑터 1) 자동 할당 → VM 표 확인(y/n) → 수동 선택 → 목록에 없으면 vim
#   4) CAE 번호 변경 질문(숫자변경기능) → vCenter 선택 (-v 가 없으면 vcenter.txt 번호, Enter = 이 user 의 이전 실행 vCenter)
#   5) vswitch_setting(BM에 포트그룹 생성, 호스트 병렬) → vm_create → affinity_setting → lpage_setting (스펙별로, 도구 안에서 병렬)
#   6) vm-param-check 로 만든 VM 이 스펙과 같은지 체크
#
# 실제 vCenter를 변경하는 단계(5) 직전에 요약을 보여주고 한 번 더 확인받는다. -n 이면 여기서 멈춘다.
# 도구가 실패하면(종료코드 0 이 아니면) 그 자리에서 멈춘다. 이미 있는 포트그룹/VM 은 실패가 아니다.
# 스펙 해석은 vm-param-check -specExport 가 맡는다(체크와 같은 파서 — 파서를 두 벌 만들지 않음).
# 비밀번호: 항상 V2/secret 의 암호 파일(passwd_update.sh 로 등록) → 직접 입력 순으로 받는다.
# (예전엔 환경변수 VC_PASSWORD 를 먼저 봤는데, 이전 실행에서 잘못된 값으로 export 해 둔 게
#  셸에 남아 있으면 암호 파일을 건너뛰고 그 값으로 로그인 실패가 나는 사고가 반복돼서,
#  실행할 때마다 지우고 시작한다.)
set -o pipefail

unset VC_PASSWORD VC_PASS VCENTER_PASS

HERE="$(cd "$(dirname "$0")" && pwd)"
USER_TAG=""; VC_IP="${VC_IP:-}"; VC_ID="${VC_ID:-lscsystems@vsphere.local}"
SPEC_DIR=""; CONC=""; TARGET_VSWITCH=""; DRY_RUN=0
EDITOR_CMD="${VM_SETUP_EDITOR:-vim}"
CHECK_DIR="$HERE/../vm-param-check-usability-improvement/vm-param-check"
CHECK_BIN="$CHECK_DIR/vm-param-check"
# mac_info(MAC 수집) — 생성한 VM 의 MAC 으로 만든 Provisioning List 를 이 경로에 <user>.txt 로 복사한다.
# 비워 두면 복사하지 않고 실행 폴더(run_<user>/)에만 남긴다.
awx_route="${awx_route:-}"
# mac_info 출력 줄에 들어가는 값 (VM VM <arg1=인프라> <VM> <IP> <MAC> eth0 sda sda5 <디스크(자동)> <argStr=OS버전> uefi).
# 환경변수로 없으면 시작할 때 물어본다("설치 정보" 단계). sda5 다음 정수(디스크)는 고정값이 아니라
# ev 스펙의 disk 값에서 자동으로 계산한다(고정 용량 480/600/960/1200/1900/7600 중 가장 가까운 값).
MAC_ARG1="${MAC_ARG1:-}"; MAC_ARGSTR="${MAC_ARGSTR:-}"

usage() {
  cat <<EOF
사용법: $0 [-u <user>] [-v <vCenter>] [옵션]

  -u <user>     작업 이름. ${HERE}/<user>.txt (BM 목록), ${HERE}/vswitch_<user>.txt 를 읽는다
                (없으면 번호 메뉴에서 고른다. 0) 직접 선택 = user 이름을 직접 입력)
  -v <vCenter>  vCenter 주소 (환경변수 VC_IP 도 가능). 없으면 vcenter.txt 목록에서 번호로 고른다
                (V2 폴더의 vcenter.txt, 없으면 vm-param-check 폴더의 것. Enter = 이 user 의 이전 실행 vCenter)
  -id <계정>    vCenter 계정 (기본: lscsystems@vsphere.local, -i 도 같음)
                비밀번호: ../secret 의 암호 파일(../passwd_update.sh 로 등록) → 직접 입력 (실행할 때마다
                셸의 VC_PASSWORD/VC_PASS 는 지우고 시작하므로 남아있는 값을 쓰지 않는다)
  -s <dir>      SPEC_DIR 경로 (기본: vm-param-check 폴더에 SPEC_DIR 이 있으면 그것, 없으면 ${HERE}/../SPEC_DIR)
  -w <vswitch>  포트그룹을 만들 가상 스위치 (기본: vswitch_setting 기본값 vSwitch0)
  -c <n>        vswitch/affinity/lpage 동시 처리 수 (기본: 각 도구 기본값)
  -n            확인만: 스펙·포트그룹 할당까지 정하고 실행 계획을 보여준 뒤 vCenter는 변경하지 않고 종료
  -h            도움말

환경변수 VM_SETUP_EDITOR 로 vim 대신 다른 편집기를 쓸 수 있다.
색상: 터미널이면 자동으로 켜진다. NO_COLOR=1 또는 VMSETUP_COLOR=never 로 끄고, VMSETUP_COLOR=always 로 강제.
환경변수 MAC_ARGSTR(OS 버전)/MAC_ARG1(인프라) 이 있으면 시작할 때 묻지 않고 그 값을 쓴다(비대화식 실행용).
EOF
}

# ---------- 색상 (터미널로 출력할 때만. 파일로 돌리거나 NO_COLOR 면 끈다) ----------
case "${VMSETUP_COLOR:-auto}" in
  always) USE_COLOR=1 ;;
  never)  USE_COLOR=0 ;;
  *)      USE_COLOR=0; [ -t 2 ] && [ -z "${NO_COLOR:-}" ] && USE_COLOR=1 ;;
esac
if [ "$USE_COLOR" -eq 1 ]; then
  C_RED=$'\033[1;31m'; C_GRN=$'\033[32m'; C_YEL=$'\033[33m'; C_CYN=$'\033[1;36m'; C_BLD=$'\033[1m'; C_DIM=$'\033[2m'; C_RST=$'\033[0m'
else
  C_RED=""; C_GRN=""; C_YEL=""; C_CYN=""; C_BLD=""; C_DIM=""; C_RST=""
fi

say()  { printf '%s\n' "$*"; }
info() { printf '%s[INFO]%s %s\n' "$C_GRN" "$C_RST" "$*"; }
warn() { printf '%s[경고]%s %s\n' "$C_YEL" "$C_RST" "$*" >&2; }
die()  { printf '%s[오류] %s%s\n' "$C_RED" "$*" "$C_RST" >&2; exit 1; }
hdr()  { printf '\n%s=== %s ===%s\n' "$C_CYN" "$*" "$C_RST" >&2; }

# prompt <변수명> <안내문> — 표준입력에서 한 줄 읽는다(EOF면 중단: 무인 실행으로 엉뚱한 기본값이 선택되지 않게).
prompt() {
  local __name="$1"
  printf '%s%s%s' "$C_BLD" "$2" "$C_RST" >&2
  IFS= read -r "$__name" || die "입력이 끝났습니다(stdin EOF) — 대화형으로 실행하세요."
}
ask_yn() {
  local __a
  while :; do
    prompt __a "$1 (y/n): "
    case "${__a,,}" in y|yes) return 0 ;; n|no) return 1 ;; esac
  done
}

# -id <계정> / -id=<계정> 은 getopts 가 못 읽으므로 -i 로 바꿔 넘긴다
ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in -id) ARGS+=(-i) ;; -id=*) ARGS+=(-i "${1#-id=}") ;; *) ARGS+=("$1") ;; esac
  shift
done
set -- "${ARGS[@]}"
while getopts "u:v:i:s:w:c:nh" opt; do
  case "$opt" in
    u) USER_TAG="$OPTARG" ;;
    v) VC_IP="$OPTARG" ;;
    i) VC_ID="$OPTARG" ;;
    s) SPEC_DIR="$OPTARG" ;;
    w) TARGET_VSWITCH="$OPTARG" ;;
    c) CONC="$OPTARG" ;;
    n) DRY_RUN=1 ;;
    h) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done
# SPEC_DIR: 기존 vm-param-check 폴더에 SPEC_DIR 을 복원해 두었으면 그것을 먼저 쓴다
# (vm-param-check 의 vm_setting_check_insert.sh 와 같은 순서 — 두 도구가 같은 스펙을 본다)
if [ -z "$SPEC_DIR" ]; then
  if [ -d "$CHECK_DIR/SPEC_DIR" ]; then SPEC_DIR="$CHECK_DIR/SPEC_DIR"; else SPEC_DIR="$HERE/../SPEC_DIR"; fi
fi
[ -d "$SPEC_DIR" ] || die "SPEC_DIR 를 찾을 수 없습니다: $SPEC_DIR"
SPEC_DIR="$(cd "$SPEC_DIR" && pwd)"

read_list() { sed -e 's/\r$//' -e '1s/^\xef\xbb\xbf//' -e 's/#.*$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' "$1" | awk 'NF'; }

# select_user — -u 가 없을 때 번호 메뉴(자주 쓰는 user 또는 직접 입력)를 보여주고 USER_TAG 를 채운다.
select_user() {
  local -a users=(lsh ljh dhk); local i ans
  while :; do
    hdr "user 선택 ($HERE)"
    printf '  0) 직접 선택\n' >&2
    for i in "${!users[@]}"; do printf '  %d) %s\n' "$((i + 1))" "${users[$i]}" >&2; done
    prompt ans "번호: "
    if [ "$ans" = "0" ]; then
      prompt USER_TAG "user 이름 입력: "
      [ -n "$USER_TAG" ] && return 0
      continue
    fi
    [[ "$ans" =~ ^[0-9]+$ ]] || continue
    [ "$ans" -ge 1 ] && [ "$ans" -le "${#users[@]}" ] && { USER_TAG="${users[$((ans - 1))]}"; return 0; }
  done
}
[ -n "$USER_TAG" ] || select_user
[[ "$USER_TAG" =~ ^[A-Za-z0-9._-]+$ ]] || die "user 에는 영문/숫자/._- 만 쓸 수 있습니다: $USER_TAG"

BM_FILE="$HERE/${USER_TAG}.txt"
VSW_FILE="$HERE/vswitch_${USER_TAG}.txt"
[ -f "$BM_FILE" ]  || die "BM 목록 파일이 없습니다: $BM_FILE (한 줄에 BM 하나)"
[ -f "$VSW_FILE" ] || die "포트그룹 파일이 없습니다: $VSW_FILE (BM 포트그룹 VLAN)"

# ---------- 설치 정보 (OS 버전 / 인프라) — 시작 전에 받아 MAC 목록(mac_info)에 넣는다 ----------
# 환경변수로 이미 있으면(비대화식 실행) 묻지 않는다. read -p 로 직접 받는다(EOF면 중단).
if [ -z "$MAC_ARGSTR" ]; then
  hdr "설치 정보"
  while :; do
    read -r -p "OS 버전 (예: 8.10): " MAC_ARGSTR || die "입력이 끝났습니다(stdin EOF) — 대화형으로 실행하세요."
    [[ "$MAC_ARGSTR" =~ ^[0-9]+(\.[0-9]+)*$ ]] && break
    warn "OS 버전 형식이 올바르지 않습니다 (숫자와 점만, 예: 8.10)."
    MAC_ARGSTR=""
  done
fi
if [ -z "$MAC_ARG1" ]; then
  while :; do
    read -r -p "인프라 입력 : " MAC_ARG1 || die "입력이 끝났습니다(stdin EOF) — 대화형으로 실행하세요."
    [[ -n "$MAC_ARG1" && "$MAC_ARG1" != *[[:space:]]* ]] && break
    warn "인프라 값은 비어 있지 않아야 하고 공백을 포함할 수 없습니다."
    MAC_ARG1=""
  done
fi

# ---------- 필요한 실행파일 (없으면 vendor 로 오프라인 빌드) ----------
# V2 의 setup.sh 로 만든다 — OS6 이면 빌드 대신 bin_os6/ 의 실행파일을 제자리에 복사한다.
ensure_bin() {
  local bin="$1" name="$2"
  [ -x "$bin" ] && return 0
  info "$(basename "$bin") 실행파일이 없어 준비합니다 (setup.sh $name)..."
  bash "$HERE/../setup.sh" "$name" >/dev/null || die "준비 실패: $name — bash $HERE/../setup.sh $name 로 확인하세요"
}
ensure_bin "$CHECK_BIN" vm-param-check
for t in vm_create vswitch_setting affinity_setting lpage_setting nic_assign mac_info power_setting; do
  ensure_bin "$HERE/${t}-source/$t" "$t"
done

# ---------- 입력 읽기 ----------
mapfile -t BMS < <(read_list "$BM_FILE" | awk '{print $1}')
[ "${#BMS[@]}" -gt 0 ] || die "$BM_FILE 에 BM 이 없습니다."

declare -A BM_PGS=()      # BM -> "pg1 pg2 ..."
declare -A PG_VLAN=()     # "BM|pg" -> vlan
while read -r bm pg vlan _; do
  [ -n "${vlan:-}" ] || { warn "vswitch 파일 형식 오류(줄 무시): $bm $pg"; continue; }
  BM_PGS[$bm]+="${BM_PGS[$bm]:+ }$pg"
  PG_VLAN["$bm|$pg"]="$vlan"
done < <(read_list "$VSW_FILE")
for bm in "${BMS[@]}"; do
  [ -n "${BM_PGS[$bm]:-}" ] || warn "$bm 는 vswitch 파일에 포트그룹이 없습니다 — 스펙은 수동 선택, 어댑터 없이 생성될 수 있습니다."
done

# ---------- vCenter: vcenter.txt 에서 번호로 선택 + user 별 이전 실행 기억 ----------
# vcenter.txt 는 V2 폴더(/home/vcenter.txt)를 먼저 보고, 없으면 vm-param-check 폴더의 것을 쓴다(한 줄에 하나, # 주석).
VC_LIST_FILE=""
for f in "$HERE/../vcenter.txt" "$CHECK_DIR/vcenter.txt"; do
  [ -f "$f" ] && { VC_LIST_FILE="$(cd "$(dirname "$f")" && pwd)/vcenter.txt"; break; }
done
LAST_VC_FILE="$HERE/run_${USER_TAG}/last_vcenter"   # "<vCenter> <날짜 시각>" 한 줄
LAST_VC=""; LAST_VC_AT=""
[ -f "$LAST_VC_FILE" ] && read -r LAST_VC LAST_VC_AT < "$LAST_VC_FILE"
[ -n "$LAST_VC" ] && info "$USER_TAG 이전 실행 vCenter: $LAST_VC ($LAST_VC_AT)"

# select_vcenter — VC_IP 를 채운다. Enter = 이전 실행 vCenter.
select_vcenter() {
  local -a vcs=(); local i ans mark
  [ -n "$VC_LIST_FILE" ] && mapfile -t vcs < <(read_list "$VC_LIST_FILE" | awk '{print $1}')
  while :; do
    if [ "${#vcs[@]}" -gt 0 ]; then
      hdr "vCenter 선택 ($VC_LIST_FILE)"
      for i in "${!vcs[@]}"; do
        mark=""; [ "${vcs[$i]}" = "$LAST_VC" ] && mark="   ${C_GRN}<- 이전 실행${C_RST}"
        printf '  %d) %s%s\n' "$((i + 1))" "${vcs[$i]}" "$mark" >&2
      done
      printf '  0) 목록에 없음 — 직접 입력\n' >&2
      prompt ans "번호${LAST_VC:+ (Enter = 이전 실행: $LAST_VC)}: "
    else
      prompt ans "vCenter 접속 IP${LAST_VC:+ (Enter = 이전 실행: $LAST_VC)}: "
      [ -n "$ans" ] && { VC_IP="$ans"; return 0; }
    fi
    if [ -z "$ans" ]; then [ -n "$LAST_VC" ] && { VC_IP="$LAST_VC"; return 0; }; continue; fi
    [[ "$ans" =~ ^[0-9]+$ ]] || continue
    if [ "$ans" -eq 0 ]; then
      prompt ans "vCenter 접속 IP: "; [ -n "$ans" ] && { VC_IP="$ans"; return 0; }; continue
    fi
    [ "$ans" -le "${#vcs[@]}" ] && { VC_IP="${vcs[$((ans - 1))]}"; return 0; }
  done
}

# ---------- 스펙 조회 (vm-param-check -specExport) ----------
declare -A EXPORT_TEXT=()   # 스펙 폴더 절대경로 -> 내보낸 값 전체
LOOKUP_DIR=""; LOOKUP_ERR=""

# spec_lookup <폴더명> — 성공하면 LOOKUP_DIR 에 스펙 폴더 절대경로. (명령 치환을 쓰면 EXPORT_TEXT 가 사라져서 전역변수로 돌려준다)
spec_lookup() {
  local out err_file rc
  LOOKUP_DIR=""; LOOKUP_ERR=""
  err_file="$(mktemp)"
  out="$("$CHECK_BIN" -specRoot="$SPEC_DIR" -specExport="$1" 2>"$err_file")"; rc=$?
  LOOKUP_ERR="$(sed 's/^[0-9/: ]\{19,\} //' "$err_file")"; rm -f "$err_file"
  [ "$rc" -eq 0 ] || return 1
  LOOKUP_DIR="$(printf '%s\n' "$out" | sed -n 's/^specdir=//p' | head -1)"
  [ -n "$LOOKUP_DIR" ] || return 1
  EXPORT_TEXT[$LOOKUP_DIR]="$out"
}
exp_get() { printf '%s\n' "${EXPORT_TEXT[$1]:-}" | sed -n "s/^$2=//p" | head -1; }

folder_of_pg() { printf '%s' "$1" | sed -nE 's/^(.+)-[cC][aA][eE]-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3}$/\1/p'; }
is_uint() { [[ "$1" =~ ^[0-9]+$ ]] && [ "$1" -gt 0 ]; }
vm_name() { printf '%sev%02d' "${1%%.*}" "$2"; }

# build_flags <스펙 폴더> [noaff] — 스펙 값을 각 도구 옵션으로 바꿔 CREATE_ARGS/AFF_ARGS/LP_ARGS 에 담는다. 잘못된 값이면 FLAG_ERR 를 채우고 실패.
# affinity 는 ev 마다 파일이 있어야 한다(자동 계산은 삭제). 여러 ev 가 같은 파일을 가리켜도 된다.
# noaff: vim 으로 새 스펙을 만드는 중(affinity 는 저장 뒤에 고름)이라 affinity 가 없어도 통과시킨다.
CREATE_ARGS=(); AFF_ARGS=(); LP_ARGS=(); FLAG_ERR=""
build_flags() {
  local d="$1" noaff="${2:-}" g n nn cpu mem disk sh aff cps numa v
  CREATE_ARGS=(); AFF_ARGS=(); LP_ARGS=(); FLAG_ERR=""
  g="$(exp_get "$d" groups)"; is_uint "$g" || { FLAG_ERR="스펙에서 ev 개수를 읽지 못했습니다"; return 1; }
  CREATE_ARGS+=("-vmCount=$g"); AFF_ARGS+=("-vm_cnt=$g")
  for n in $(seq 1 "$g"); do
    nn="$(printf '%02d' "$n")"
    cpu="$(exp_get "$d" "cpu-ev$nn")"; mem="$(exp_get "$d" "mem-ev$nn")"
    disk="$(exp_get "$d" "disk-ev$nn")"; disk="${disk%%,*}"      # 여러 허용값(1024,1026)이면 첫 값으로 만든다
    sh="$(exp_get "$d" "shares-ev$nn")"; sh="${sh%%,*}"
    for v in "cpu-ev$nn:$cpu" "mem-ev$nn:$mem" "disk-ev$nn:$disk"; do
      is_uint "${v#*:}" || { FLAG_ERR="${v%%:*} 값이 양의 정수가 아닙니다: '${v#*:}'"; return 1; }
    done
    case "${sh,,}" in normal|nomal) sh="nomal" ;; *) is_uint "$sh" || { FLAG_ERR="shares-ev$nn 값은 숫자 또는 normal 이어야 합니다: '$sh'"; return 1; } ;; esac
    CREATE_ARGS+=("-ev${nn}Cpu=$cpu" "-ev${nn}Mem=$mem" "-ev${nn}Disk=$disk" "-ev${nn}Share=$sh")

    aff="$(exp_get "$d" "affinity-ev$nn")"
    if [ -n "$aff" ]; then
      [ -f "$aff" ] || { FLAG_ERR="affinity-ev$nn 파일이 없습니다: $aff"; return 1; }
      AFF_ARGS+=("-affinityFile$nn=$aff")
    elif [ -z "$noaff" ]; then
      FLAG_ERR="affinity-ev$nn 이 스펙에 없습니다 (자동 계산은 삭제됨 — 스펙에 affinity-ev$nn=<파일> 을 적어주세요. 여러 ev 가 같은 파일을 써도 됩니다)"; return 1
    fi

    # 스펙의 cores = 소켓당 코어 수, numa = NUMA 노드당 vCPU 수. lpage 는 총 코어/소켓 수/NUMA 노드 수를 받는다.
    cps="$(exp_get "$d" "cores-ev$nn")"; numa="$(exp_get "$d" "numa-ev$nn")"
    if [ -n "$cps" ]; then
      is_uint "$cps" || { FLAG_ERR="cores-ev$nn 값이 양의 정수가 아닙니다: '$cps'"; return 1; }
      [ $((cpu % cps)) -eq 0 ] || { FLAG_ERR="ev$nn: cpu($cpu)가 cores($cps)로 나누어떨어지지 않습니다"; return 1; }
      LP_ARGS+=("-ev${nn}Cores=$cpu" "-ev${nn}Sockets=$((cpu / cps))")
      if [ -n "$numa" ]; then
        is_uint "$numa" || { FLAG_ERR="numa-ev$nn 값이 양의 정수가 아닙니다: '$numa'"; return 1; }
        [ $((cpu % numa)) -eq 0 ] || { FLAG_ERR="ev$nn: cpu($cpu)가 numa($numa)로 나누어떨어지지 않습니다"; return 1; }
        LP_ARGS+=("-ev${nn}Numa=$((cpu / numa))")
      fi
    fi
  done
}

# ---------- vim 편집 (템플릿의 모든 옵션은 키="" 상태 + 설명은 주석) ----------
SPEC_MARK='# [스펙 수동 입력]'
AFF_MARK='# [affinity 수동 입력]'
NIC_MARK='# [네트워크 어댑터 수동 지정]'
declare -A PARSED=()

# spec_key_list <n> — 그룹 n(1~99)의 스펙 키 이름들. ev01 은 접미사 없는 키(cpu/mem/...)를 쓴다.
spec_key_list() {
  local n="$1" nn; nn="$(printf '%02d' "$1")"
  if [ "$n" -eq 1 ]; then echo "cpu mem disk shares-ev01 cores numa"
  else echo "cpu-ev$nn mem-ev$nn disk-ev$nn shares-ev$nn cores-ev$nn numa-ev$nn"; fi
}

write_spec_template() {
  local f="$1" n keys k
  {
    echo "$SPEC_MARK"
    echo "# 값을 큰따옴표 안에 적는다. 비워 둔 항목은 '지정 안 함'이다."
    echo "# ev01 은 필수이고, ev02~ev99 는 값을 하나라도 적으면 그 ev 를 만든다."
    echo "#   - 아래에는 ev10 까지만 틀이 있다. ev11~ev99 는 같은 규칙으로 줄을 직접 추가한다 (예: cpu-ev11=\"4\")"
    echo "#   - 값을 적은 ev 는 cpu/mem/disk/shares 를 모두 적어야 한다"
    echo "#   - ev 번호는 ev01 부터 빠짐없이 이어져야 한다 (ev02 를 비우고 ev03 을 적으면 오류)"
    echo "# affinity 파일은 여기서 적지 않는다 — 저장한 뒤 ev 별로 (직전 ev 와 같은 파일 / 기존 파일 / vim 입력) 중에서 고른다."
    echo
    echo 'folder=""        # [필수] 새로 만들 스펙 폴더 이름. 예: TST-CAE001-SAMP48c-QRST (SPEC_DIR 아래 이 이름으로 저장됨)'
    echo 'ht=""            # [필수] 하이퍼스레딩 on 또는 off (vm-param-check 가 이 스펙으로 체크할 때 사용)'
    for n in $(seq 1 10); do   # 틀은 ev10 까지만 (ev11~ev99 는 직접 추가)
      echo
      if [ "$n" -eq 1 ]; then echo "# --- ev01 (필수) ---"; else echo "# --- ev$(printf '%02d' "$n") (선택) ---"; fi
      for k in $(spec_key_list "$n"); do
        case "$k" in
          cpu*)    printf '%-16s # vCPU 수\n' "$k=\"\"" ;;
          mem*)    printf '%-16s # 메모리 GB\n' "$k=\"\"" ;;
          disk*)   printf '%-16s # 디스크 GB (여러 허용값은 쉼표, 만들 때는 첫 값 사용. 예: 1024,1026)\n' "$k=\"\"" ;;
          shares*) printf '%-16s # CPU/메모리 Shares: ratio 숫자(예: 4000) 또는 normal\n' "$k=\"\"" ;;
          cores*)  printf '%-16s # 소켓당 코어 수 (비우면 lpage 토폴로지 설정 생략)\n' "$k=\"\"" ;;
          numa*)   printf '%-16s # NUMA 노드당 vCPU 수 (비우면 NUMA 설정 생략)\n' "$k=\"\"" ;;
        esac
      done
    done
  } > "$f"
}

# parse_kv_file <파일> — `키="값"`(또는 키=값) 줄을 PARSED 에 담는다. '#' 주석 줄과 값 뒤 주석은 무시.
parse_kv_file() {
  local line
  local re_q='^[[:space:]]*([A-Za-z0-9._-]+)[[:space:]]*=[[:space:]]*"([^"]*)"'
  local re_p='^[[:space:]]*([A-Za-z0-9._-]+)[[:space:]]*=[[:space:]]*([^[:space:]#"]+)'
  PARSED=()
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line%$'\r'}"
    if [[ "$line" =~ $re_q ]]; then PARSED["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"
    elif [[ "$line" =~ $re_p ]]; then PARSED["${BASH_REMATCH[1]}"]="${BASH_REMATCH[2]}"; fi
  done < "$1"
}

# put_error <파일> <메시지> — 이전 오류 안내를 지우고 맨 위에 주석으로 오류를 붙인다.
put_error() {
  local f="$1" msg="$2" tmp; tmp="$(mktemp)"
  { printf '%s\n' "$msg" | sed 's/^/# [오류] /'; grep -v '^# \[오류\]' "$f"; } > "$tmp"; mv "$tmp" "$f"
}
edit_file() { "$EDITOR_CMD" "$1"; }

NEW_SPEC_DIR=""
# create_spec_via_vim — vim 으로 새 스펙을 입력받아 SPEC_DIR/<folder>/ 에 저장한다. 성공하면 NEW_SPEC_DIR 를 채운다.
create_spec_via_vim() {
  local tmp err folder ht n k dir spec_file created g nn cpu
  NEW_SPEC_DIR=""
  tmp="$(mktemp)"; write_spec_template "$tmp"
  while :; do
    edit_file "$tmp"
    parse_kv_file "$tmp"
    err=""; created=0; dir=""
    folder="${PARSED[folder]:-}"; ht="${PARSED[ht]:-}"
    if [ -z "$folder" ]; then err="folder 가 비어 있습니다 (새로 만들 스펙 폴더 이름을 적어주세요)"
    elif [[ "$folder" =~ [/[:space:]] ]]; then err="folder 에는 공백이나 / 를 쓸 수 없습니다: $folder"
    else case "${ht,,}" in on|off) ;; *) err="ht 는 on 또는 off 여야 합니다: '$ht'" ;; esac; fi

    if [ -z "$err" ]; then
      # 폴더명 규칙(CAE 4레코드)과 같은 스펙 중복은 vm-param-check -initFolder 가 검사한다.
      if err="$("$CHECK_BIN" -specRoot="$SPEC_DIR" -initFolder="$folder" 2>&1 >/dev/null)"; then
        err=""; created=1; dir="$SPEC_DIR/$folder"; spec_file="$dir/${folder}_spec.txt"
        {
          echo "# $folder 스펙 정의 파일 (vm_setup.sh 의 vim 입력으로 생성 — $(date +%F))"
          echo "ht=${ht,,}"
          for n in $(seq 1 99); do for k in $(spec_key_list "$n"); do
            [ -n "${PARSED[$k]:-}" ] && echo "$k=${PARSED[$k]}"
          done; done
        } > "$spec_file"
        if ! spec_lookup "$folder"; then err="$LOOKUP_ERR"
        elif ! build_flags "$LOOKUP_DIR" noaff; then err="$FLAG_ERR"; fi
        [ -n "$err" ] && rm -rf "$dir"
      else
        err="$(printf '%s' "$err" | sed 's/^[0-9/: ]\{19,\} //')"
      fi
    fi

    if [ -z "$err" ]; then break; fi
    printf '\n[오류] %s\n' "$err" >&2
    ask_yn "다시 편집할까요? (n 이면 취소)" || { rm -f "$tmp"; return 1; }
    put_error "$tmp" "$err"
  done
  rm -f "$tmp"

  # ev 별 affinity: 직전 ev 와 같은 파일 / 기존 파일 / vim 직접 입력 (자동 계산은 삭제 — ev 마다 반드시 고른다)
  g="$(exp_get "$LOOKUP_DIR" groups)"; dir="$LOOKUP_DIR"; spec_file="$dir/$(basename "$dir")_spec.txt"
  AFF_REL=""
  for n in $(seq 1 "$g"); do
    nn="$(printf '%02d' "$n")"; cpu="$(exp_get "$dir" "cpu-ev$nn")"
    pick_affinity "$dir" "$nn" "$cpu"
    echo "affinity-ev$nn=$AFF_REL" >> "$spec_file"
  done
  spec_lookup "$(basename "$dir")" || { warn "저장한 스펙을 다시 읽지 못했습니다: $LOOKUP_ERR"; return 1; }
  NEW_SPEC_DIR="$LOOKUP_DIR"
  say "[INFO] 새 스펙 저장 완료: $NEW_SPEC_DIR"
}

# pick_affinity <스펙폴더> <NN> <vCPU 수> — ev 의 affinity 파일을 정해 AFF_REL(스펙 폴더 기준 파일 이름)에 담는다.
# 들어올 때 AFF_REL 은 직전 ev 의 파일이다(1번 "같은 파일 사용"). 자동 계산은 삭제했으므로 반드시 하나를 고른다.
pick_affinity() {
  local dir="$1" nn="$2" cpu="$3" ans i f tmp err k line val out_tmp out="$1/affinity_ev$2.txt" prev="$AFF_REL"
  local -a found
  while :; do
    printf '\n[ev%s] affinity 지정 방법 (vCPU %s개)\n' "$nn" "$cpu" >&2
    [ -n "$prev" ] && printf '  1) 직전 ev 와 같은 파일 사용: %s\n' "$prev" >&2
    printf '  2) SPEC_DIR 의 기존 affinity 파일 선택\n  3) vim 으로 직접 입력\n' >&2
    if [ -n "$prev" ]; then prompt ans "선택 [1]: "; ans="${ans:-1}"; else prompt ans "선택 (2/3): "; fi
    case "$ans" in
      1) [ -n "$prev" ] || continue; AFF_REL="$prev"; return 0 ;;
      2)
        mapfile -t found < <(find "$SPEC_DIR" -type f -name 'affinity*.txt' | sort)
        if [ "${#found[@]}" -eq 0 ]; then warn "SPEC_DIR 에 affinity*.txt 가 없습니다."; continue; fi
        for i in "${!found[@]}"; do printf '  %d) %s\n' "$((i + 1))" "${found[$i]#$SPEC_DIR/}" >&2; done
        prompt ans "번호 (0=뒤로): "
        [[ "$ans" =~ ^[0-9]+$ ]] && [ "$ans" -ge 1 ] && [ "$ans" -le "${#found[@]}" ] || continue
        f="${found[$((ans - 1))]}"
        # 이 스펙 폴더 안의 파일이면 그대로 가리키고(여러 ev 가 같은 파일), 다른 폴더 것이면 복사해서 폴더를 자기완결로 둔다
        if [ "$(dirname "$f")" = "$dir" ]; then AFF_REL="$(basename "$f")"; return 0; fi
        cp "$f" "$out" && { AFF_REL="affinity_ev$nn.txt"; return 0; } ;;
      3)
        tmp="$(mktemp)"
        {
          echo "$AFF_MARK"
          echo "# [ev$nn] vCPU 마다 물리 CPU 번호를 큰따옴표 안에 적는다. 예) \"0,1\" (HT on 이면 코어 2개 묶음), \"2\""
          echo "# 모든 줄을 채워야 저장된다 (vCPU $cpu개)."
          for k in $(seq 0 $((cpu - 1))); do printf 'sched.vcpu%s.affinity=""\n' "$k"; done
        } > "$tmp"
        while :; do
          edit_file "$tmp"; parse_kv_file "$tmp"; err=""; out_tmp=""
          for k in $(seq 0 $((cpu - 1))); do
            val="${PARSED[sched.vcpu$k.affinity]:-}"
            if [ -z "$val" ]; then err="sched.vcpu$k.affinity 값이 비어 있습니다"; break; fi
            [[ "$val" =~ ^[0-9]+(,[0-9]+)*$ ]] || { err="sched.vcpu$k.affinity 값 형식 오류(숫자와 쉼표만): '$val'"; break; }
            out_tmp+="sched.vcpu$k.affinity=$val"$'\n'
          done
          if [ -z "$err" ]; then printf '%s' "$out_tmp" > "$out"; rm -f "$tmp"; AFF_REL="affinity_ev$nn.txt"; return 0; fi
          printf '\n[오류] %s\n' "$err" >&2
          ask_yn "다시 편집할까요? (n 이면 방법 선택으로 돌아감)" || break
          put_error "$tmp" "$err"
        done
        rm -f "$tmp" ;;
    esac
  done
}

# ---------- 1) BM별 스펙 결정 ----------
declare -A SPEC_OF=()   # BM -> 스펙 폴더 절대경로
list_specs() {
  local d n
  for d in "$SPEC_DIR"/*/; do
    d="${d%/}"; n="$(basename "$d")"
    [ -f "$d/${n}_spec.txt" ] && echo "$d"
  done
}

auto_assign_specs() {
  local bm pg f d
  for bm in "${BMS[@]}"; do
    local -a cands=()
    for pg in ${BM_PGS[$bm]:-}; do
      f="$(folder_of_pg "$pg")"; [ -n "$f" ] || continue
      spec_lookup "$f" || continue
      for d in "${cands[@]}"; do [ "$d" = "$LOOKUP_DIR" ] && continue 2; done
      cands+=("$LOOKUP_DIR")
    done
    [ "${#cands[@]}" -eq 1 ] || continue
    # VM 생성에 못 쓰는 스펙(예: affinity 파일 없음)은 자동 할당하지 않고 이유를 알린다.
    # affinity 만 없는 스펙(기존 vm-param-check SPEC_DIR 을 그대로 가져온 경우 등)은 지금 골라 스펙에 추가할 수 있다.
    if build_flags "${cands[0]}" || offer_missing_affinity "${cands[0]}"; then SPEC_OF[$bm]="${cands[0]}"
    else warn "$bm: 자동 매칭된 스펙 $(basename "${cands[0]}") 을(를) 쓸 수 없습니다 — $FLAG_ERR"; fi
  done
}

# offer_missing_affinity <스펙 폴더> — affinity-evNN 이 빠진 스펙이면 ev 별 affinity 를 골라 스펙 파일에 추가한다.
# 스펙마다 한 번만 묻는다. 성공하면 LOOKUP_DIR 이 그 스펙(다시 읽은 것).
declare -A AFF_OFFERED=()
offer_missing_affinity() {
  local dir="$1" g n nn cpu cur spec_file
  [[ "$FLAG_ERR" == *"affinity-ev"*"이 스펙에 없습니다"* ]] || return 1
  [ -z "${AFF_OFFERED[$dir]:-}" ] || return 1
  AFF_OFFERED[$dir]=1
  warn "스펙 $(basename "$dir") 에 affinity 파일이 없는 ev 가 있습니다 (예전 vm-param-check 스펙은 ev01 affinity 를 자동 계산했지만 지금은 ev 마다 파일이 필요)."
  ask_yn "지금 ev 별 affinity 를 골라 이 스펙에 추가할까요?" || return 1
  spec_file="$dir/$(basename "$dir")_spec.txt"
  g="$(exp_get "$dir" groups)"; AFF_REL=""
  for n in $(seq 1 "$g"); do
    nn="$(printf '%02d' "$n")"; cur="$(exp_get "$dir" "affinity-ev$nn")"
    if [ -n "$cur" ]; then AFF_REL="${cur#$dir/}"; continue; fi   # 이미 있는 ev 는 그대로(다음 ev 의 "같은 파일" 후보)
    cpu="$(exp_get "$dir" "cpu-ev$nn")"
    pick_affinity "$dir" "$nn" "$cpu"
    printf 'affinity-ev%s=%s\n' "$nn" "$AFF_REL" >> "$spec_file"
  done
  info "$spec_file 에 affinity 를 추가했습니다."
  spec_lookup "$(basename "$dir")" && build_flags "$LOOKUP_DIR"
}

# pick_spec <BM> — 목록에서 고르거나 vim 으로 새로 만든다. SPEC_OF[BM] 를 채운다(유효한 선택이 나올 때까지 반복).
LAST_SPEC=""
pick_spec() {
  local bm="$1" ans i
  local -a specs
  while :; do
    mapfile -t specs < <(list_specs)
    printf '\n[%s] 적용할 스펙을 고르세요\n  0) 목록에 없음 — vim 으로 새 스펙 입력\n' "$bm" >&2
    for i in "${!specs[@]}"; do printf '  %d) %s\n' "$((i + 1))" "$(basename "${specs[$i]}")" >&2; done
    prompt ans "번호${LAST_SPEC:+ (Enter = 이전 선택: $(basename "$LAST_SPEC"))}: "
    if [ -z "$ans" ] && [ -n "$LAST_SPEC" ]; then SPEC_OF[$bm]="$LAST_SPEC"; return 0; fi
    [[ "$ans" =~ ^[0-9]+$ ]] || continue
    if [ "$ans" -eq 0 ]; then
      create_spec_via_vim || { warn "새 스펙을 만들지 못했습니다."; continue; }
      SPEC_OF[$bm]="$NEW_SPEC_DIR"; LAST_SPEC="$NEW_SPEC_DIR"; return 0
    fi
    [ "$ans" -le "${#specs[@]}" ] || continue
    if spec_lookup "$(basename "${specs[$((ans - 1))]}")" && { build_flags "$LOOKUP_DIR" || offer_missing_affinity "$LOOKUP_DIR"; }; then
      SPEC_OF[$bm]="$LOOKUP_DIR"; LAST_SPEC="$LOOKUP_DIR"; return 0
    fi
    warn "이 스펙은 VM 생성에 쓸 수 없습니다: ${LOOKUP_ERR:-$FLAG_ERR}"
  done
}

info "user $USER_TAG — BM ${#BMS[@]}대, SPEC_DIR=$SPEC_DIR"
auto_assign_specs
# BM→스펙 확인 표는 따로 묻지 않는다 — VM 표에 스펙 열이 있어 거기서 함께 확인한다.
# 자동으로 정하지 못한 BM 만 직접 고른다.
spec_hdr=0
for bm in "${BMS[@]}"; do
  [ -n "${SPEC_OF[$bm]:-}" ] && continue
  if [ "$spec_hdr" -eq 0 ]; then info "스펙을 자동으로 정하지 못한 BM 은 직접 선택합니다." >&2; spec_hdr=1; fi
  pick_spec "$bm"
done
# 선택한 스펙이 VM 생성에 쓸 수 있는지 마지막으로 확인
for bm in "${BMS[@]}"; do
  spec_lookup "$(basename "${SPEC_OF[$bm]}")" || die "스펙을 읽지 못했습니다: ${SPEC_OF[$bm]} — $LOOKUP_ERR"
  build_flags "$LOOKUP_DIR" || die "$(basename "${SPEC_OF[$bm]}"): $FLAG_ERR"
done

# ---------- 2) VM별 포트그룹(네트워크 어댑터 1) ----------
declare -A NIC_OF=(); declare -A VM_BM=(); VMS=()
for bm in "${BMS[@]}"; do
  g="$(exp_get "${SPEC_OF[$bm]}" groups)"
  for n in $(seq 1 "$g"); do vm="$(vm_name "$bm" "$n")"; VMS+=("$vm"); VM_BM[$vm]="$bm"; done
done

auto_assign_nics() {
  local vm bm pg f cnt pick
  for vm in "${VMS[@]}"; do
    bm="${VM_BM[$vm]}"; cnt=0; pick=""
    for pg in ${BM_PGS[$bm]:-}; do cnt=$((cnt + 1)); pick="$pg"; done
    if [ "$cnt" -eq 1 ]; then NIC_OF[$vm]="$pick"; continue; fi
    [ "$cnt" -gt 1 ] || continue
    # 포트그룹이 여러 개면, 이 BM 의 스펙과 같은 스펙으로 이어지는 포트그룹이 하나뿐일 때만 자동 선택
    local matched=0; pick=""
    for pg in ${BM_PGS[$bm]}; do
      f="$(folder_of_pg "$pg")"; [ -n "$f" ] || continue
      spec_lookup "$f" && [ "$LOOKUP_DIR" = "${SPEC_OF[$bm]}" ] && { matched=$((matched + 1)); pick="$pg"; }
    done
    [ "$matched" -eq 1 ] && NIC_OF[$vm]="$pick"
  done
}

print_nic_table() {
  local vm pg
  hdr "VM → 스펙 / 포트그룹 (네트워크 어댑터 1)"
  printf '%s%-26s %-24s %-28s %s%s\n' "$C_BLD" "VM" "BM" "스펙" "포트그룹" "$C_RST" >&2
  for vm in "${VMS[@]}"; do
    pg="${NIC_OF[$vm]:-${C_YEL}(어댑터 없음)${C_RST}}"
    printf '%-26s %-24s %-28s %s\n' "$vm" "${VM_BM[$vm]}" "$(basename "${SPEC_OF[${VM_BM[$vm]}]}")" "$pg" >&2
  done
}

write_nic_template() {
  {
    echo "$NIC_MARK"
    echo "# 목록에 없는 조합을 직접 지정한다. 값은 큰따옴표 안에 적는다."
    echo "#   hostname  : VM 이름 (예: $(vm_name "${BMS[0]}" 1)). 이 실행에서 만드는 VM 이름과 같아야 한다"
    echo "#   portgroup : 네트워크 어댑터 1 에 연결할 표준 vSwitch 포트그룹 이름 (해당 BM 에 있어야 한다)"
    echo "# 여러 VM 을 지정하려면 아래 두 줄을 복사해서 이어 적는다. 비워 둔 블록은 무시된다."
    echo "# (연결됨 / 전원을 켤 때 연결 은 자동으로 체크된다. 포트그룹을 새로 만드는 것은 지원하지 않는다.)"
    echo 'hostname=""'
    echo 'portgroup=""'
  } > "$1"
}

# nic_vim — hostname/portgroup 블록을 읽어 NIC_OF 에 반영한다.
nic_vim() {
  local tmp line host pg vm bm ok err re_q re_p
  re_q='^[[:space:]]*(hostname|portgroup)[[:space:]]*=[[:space:]]*"([^"]*)"'
  re_p='^[[:space:]]*(hostname|portgroup)[[:space:]]*=[[:space:]]*([^[:space:]#"]+)'
  tmp="$(mktemp)"; write_nic_template "$tmp"
  while :; do
    edit_file "$tmp"; err=""; host=""; pg=""
    local -a pairs=()
    while IFS= read -r line || [ -n "$line" ]; do
      line="${line%$'\r'}"; local key="" val=""
      if [[ "$line" =~ $re_q ]]; then key="${BASH_REMATCH[1]}"; val="${BASH_REMATCH[2]}"
      elif [[ "$line" =~ $re_p ]]; then key="${BASH_REMATCH[1]}"; val="${BASH_REMATCH[2]}"; fi
      case "$key" in
        hostname)  [ -n "$host$pg" ] && pairs+=("$host|$pg"); host="$val"; pg="" ;;
        portgroup) pg="$val"; pairs+=("$host|$pg"); host=""; pg="" ;;
      esac
    done < "$tmp"
    [ -n "$host" ] && pairs+=("$host|")
    local -a apply_vm=() apply_pg=()
    for p in "${pairs[@]}"; do
      host="${p%%|*}"; pg="${p#*|}"
      [ -z "$host" ] && [ -z "$pg" ] && continue            # 비워 둔 블록
      if [ -z "$host" ] || [ -z "$pg" ]; then err="hostname 과 portgroup 을 둘 다 적어주세요 (hostname='$host', portgroup='$pg')"; break; fi
      if [ -z "${VM_BM[$host]:-}" ]; then err="이 실행에서 만드는 VM 이 아닙니다: $host"; break; fi
      bm="${VM_BM[$host]}"
      if [ -z "${PG_VLAN["$bm|$pg"]:-}" ]; then
        warn "$host: 포트그룹 '$pg' 는 $bm 의 vswitch 파일에 없습니다 — 호스트에 이미 만들어져 있어야 VM 생성이 성공합니다."
        ask_yn "그래도 사용할까요?" || { err="사용하지 않기로 했습니다: $host → $pg"; break; }
      fi
      apply_vm+=("$host"); apply_pg+=("$pg")
    done
    if [ -z "$err" ]; then
      for i in "${!apply_vm[@]}"; do NIC_OF[${apply_vm[$i]}]="${apply_pg[$i]}"; done
      rm -f "$tmp"; return 0
    fi
    printf '\n[오류] %s\n' "$err" >&2
    ask_yn "다시 편집할까요? (n 이면 취소)" || { rm -f "$tmp"; return 1; }
    put_error "$tmp" "$err"
  done
}

# pick_nic <VM> — 이 BM 의 포트그룹 목록에서 고르거나(Enter=현재값 유지) vim 으로 지정한다.
pick_nic() {
  local vm="$1" bm ans i cur hint
  local -a pgs
  bm="${VM_BM[$vm]}"; read -ra pgs <<< "${BM_PGS[$bm]:-}"
  while :; do
    cur="${NIC_OF[$vm]:-}"
    printf '\n[%s] (BM %s) 네트워크 어댑터 1 의 포트그룹\n  0) 목록에 없음 — vim 으로 지정\n' "$vm" "$bm" >&2
    for i in "${!pgs[@]}"; do printf '  %d) %s (VLAN %s)\n' "$((i + 1))" "${pgs[$i]}" "${PG_VLAN["$bm|${pgs[$i]}"]}" >&2; done
    printf '  a<번호>) 이 BM(%s)의 VM 전체에 그 포트그룹 적용 (예: a1)\n' "$bm" >&2
    if [ -n "$cur" ]; then hint=" (Enter = 현재값 유지: $cur)"; else hint=" (Enter = 어댑터 없이 생성)"; fi
    prompt ans "번호$hint: "
    [ -z "$ans" ] && return 0
    if [[ "$ans" =~ ^[aA]([0-9]+)$ ]]; then
      local pn="${BASH_REMATCH[1]}" v
      [ "$pn" -ge 1 ] && [ "$pn" -le "${#pgs[@]}" ] || continue
      for v in "${VMS[@]}"; do [ "${VM_BM[$v]}" = "$bm" ] && NIC_OF[$v]="${pgs[$((pn - 1))]}"; done
      return 0
    fi
    [[ "$ans" =~ ^[0-9]+$ ]] || continue
    if [ "$ans" -eq 0 ]; then nic_vim && return 0; continue; fi
    [ "$ans" -le "${#pgs[@]}" ] || continue
    NIC_OF[$vm]="${pgs[$((ans - 1))]}"; return 0
  done
}

auto_assign_nics
nic_hdr=0
for vm in "${VMS[@]}"; do
  [ -z "${NIC_OF[$vm]:-}" ] || continue                     # 자동으로 정해졌다
  [ -n "${BM_PGS[${VM_BM[$vm]}]:-}" ] || continue           # BM 에 포트그룹이 없으면 고를 게 없다
  if [ "$nic_hdr" -eq 0 ]; then print_nic_table; info "포트그룹을 자동으로 정하지 못한 VM 은 직접 선택합니다." >&2; nic_hdr=1; fi
  pick_nic "$vm"
done
while :; do
  print_nic_table
  ask_yn "위 스펙·포트그룹 할당이 맞습니까?" && break
  for vm in "${VMS[@]}"; do pick_nic "$vm"; done
done
for vm in "${VMS[@]}"; do
  [ -n "${NIC_OF[$vm]:-}" ] || warn "$vm 는 네트워크 어댑터 없이 만들어집니다."
done

# ask_cae_number — 이번 실행에서 만드는 포트그룹 이름(<폴더명>-cae-a-b-c-d)의 CAE/LSI 번호를 바꿀지 한 번 묻는다.
# 스펙은 CAE 번호를 빼고 매칭하므로(SAC-CAE001 로 등록된 스펙을 SAC-CAE100 으로 실행해도 같은 스펙) 스펙은 그대로다.
# 바꾸면 BM 에 만들 포트그룹과 VM 어댑터 1 의 포트그룹 이름이 함께 바뀐다.
ask_cae_number() {
  local -a folders=(); local -A seen=(); local bm pg f vm ans new newf re='^([^-]+-)(CAE|LSI|cae|lsi)([0-9]+)(-.+)$'
  for bm in "${BMS[@]}"; do
    for pg in ${BM_PGS[$bm]:-}; do
      f="$(folder_of_pg "$pg")"
      [ -n "$f" ] && [[ "$f" =~ $re ]] && [ -z "${seen[$f]:-}" ] && { seen[$f]=1; folders+=("$f"); }
    done
  done
  [ "${#folders[@]}" -gt 0 ] || return 0
  hdr "CAE 번호 (이번에 만드는 포트그룹 이름: ${folders[*]})"
  prompt ans "CAE 번호를 바꾸시겠습니까? (y/N): "
  case "${ans,,}" in y|yes) ;; *) return 0 ;; esac
  for f in "${folders[@]}"; do
    [[ "$f" =~ $re ]] || continue
    while :; do
      prompt ans "$f 의 새 번호 (지금 ${BASH_REMATCH[3]}, Enter = 그대로): "
      [ -z "$ans" ] && break
      [[ "$ans" =~ ^[0-9]+$ ]] && break
      warn "숫자만 입력하세요."
    done
    [ -n "$ans" ] || continue
    [[ "$f" =~ $re ]]; newf="${BASH_REMATCH[1]}${BASH_REMATCH[2]}${ans}${BASH_REMATCH[4]}"
    [ "$newf" = "$f" ] && continue
    for bm in "${BMS[@]}"; do                                   # BM 에 만들 포트그룹
      new=""
      for pg in ${BM_PGS[$bm]:-}; do
        if [ "${pg#"$f"-}" != "$pg" ]; then PG_VLAN["$bm|$newf-${pg#"$f"-}"]="${PG_VLAN["$bm|$pg"]}"; unset "PG_VLAN[$bm|$pg]"; pg="$newf-${pg#"$f"-}"; fi
        new+="${new:+ }$pg"
      done
      BM_PGS[$bm]="$new"
    done
    for vm in "${VMS[@]}"; do                                   # VM 어댑터 1
      pg="${NIC_OF[$vm]:-}"; [ -n "$pg" ] && [ "${pg#"$f"-}" != "$pg" ] && NIC_OF[$vm]="$newf-${pg#"$f"-}"
    done
    info "$f → $newf (포트그룹 이름에 반영)"
  done
}
# 숫자변경기능 — CAE 번호 변경 질문. 이 기능이 필요 없으면 아래 한 줄(ask_cae_number)을 주석처리하면 바로 꺼진다.
ask_cae_number

# ---------- 3) 실행 계획 ----------
RUN_DIR="$HERE/run_${USER_TAG}"; mkdir -p "$RUN_DIR"
declare -A SPEC_BMS=(); SPEC_ORDER=()
for bm in "${BMS[@]}"; do
  d="${SPEC_OF[$bm]}"
  [ -n "${SPEC_BMS[$d]:-}" ] || SPEC_ORDER+=("$d")
  SPEC_BMS[$d]+="$bm"$'\n'
done
: > "$RUN_DIR/hostgroup.txt"
for vm in "${VMS[@]}"; do [ -n "${NIC_OF[$vm]:-}" ] && echo "$vm ${NIC_OF[$vm]}" >> "$RUN_DIR/hostgroup.txt"; done
# 이 실행의 BM 만 골라 vswitch 입력 파일을 만든다(도구가 실행 폴더 기준 상대경로로 읽는다)
: > "$RUN_DIR/vswitch.txt"
for bm in "${BMS[@]}"; do
  for pg in ${BM_PGS[$bm]:-}; do echo "$bm $pg ${PG_VLAN["$bm|$pg"]}" >> "$RUN_DIR/vswitch.txt"; done
done

# vCenter 는 실행 계획에 보이도록 계획 출력 전에 고른다(-n 은 vCenter 에 접속하지 않으므로 묻지 않음)
[ "$DRY_RUN" -eq 1 ] || [ -n "$VC_IP" ] || select_vcenter

# ev_ranges "01 02 03 05" -> "ev01~ev03,ev05"
ev_ranges() {
  local out="" s="" p="" n
  for n in $1; do
    n=$((10#$n))
    if [ -n "$p" ] && [ "$n" -eq $((p + 1)) ]; then p=$n; continue; fi
    [ -n "$s" ] && out+="${out:+,}$(printf 'ev%02d' "$s")$([ "$p" -ne "$s" ] && printf '~ev%02d' "$p")"
    s=$n; p=$n
  done
  [ -n "$s" ] && out+="${out:+,}$(printf 'ev%02d' "$s")$([ "$p" -ne "$s" ] && printf '~ev%02d' "$p")"
  printf '%s' "$out"
}

# print_affinity — AFF_ARGS 의 affinity 파일을 내용별로 묶어 한 번씩만 보여준다
# (파일 이름이 달라도 내용이 같으면 하나로, ev 가 20개라도 같은 파일이면 한 번)
print_affinity() {
  local a nn f key i
  local -A evs_of=() names_of=() file_of=(); local -a order=()
  for a in "${AFF_ARGS[@]}"; do
    [[ "$a" =~ ^-affinityFile([0-9]+)=(.+)$ ]] || continue
    nn="${BASH_REMATCH[1]}"; f="${BASH_REMATCH[2]}"
    key="$(sed -e 's/\r$//' -e 's/#.*$//' -e 's/[[:space:]]//g' "$f" | awk 'NF' | cksum)"
    if [ -z "${evs_of[$key]:-}" ]; then order+=("$key"); file_of[$key]="$f"; fi
    evs_of[$key]+=" $nn"
    [[ " ${names_of[$key]:-} " == *" $(basename "$f") "* ]] || names_of[$key]+="${names_of[$key]:+ }$(basename "$f")"
  done
  say "   affinity 설정값 (내용이 같은 파일은 한 번만):"
  for key in "${order[@]}"; do
    printf '     %s[%s]%s %s\n' "$C_BLD" "$(ev_ranges "${evs_of[$key]}")" "$C_RST" "${names_of[$key]// /, }"
    sed -e 's/\r$//' -e 's/#.*$//' "${file_of[$key]}" | awk 'NF' | sed 's/^/        /'
  done
}

# print_ev_kv <머리말> <-evNNKey=Val ...> — vm_create/lpage_setting 처럼 ev별로 여러 키=값을 갖는
# 인자 배열을, 값이 같은 ev 는 ev_ranges 로 묶어서 "ev01: Key=Val Key=Val" 식으로 보여준다
# (affinity 설정값을 파일 내용으로 묶어 보여주는 것과 같은 목적 — ev 가 많을 때 한눈에 비교하려고).
print_ev_kv() {
  local label="$1"; shift
  local a nn key
  local -A kv_of=() evs_of=(); local -a nns=() order=()
  for a in "$@"; do
    [[ "$a" =~ ^-ev([0-9]+)([A-Za-z]+)=(.*)$ ]] || continue
    nn="${BASH_REMATCH[1]}"
    [ -n "${kv_of[$nn]:-}" ] || nns+=("$nn")
    kv_of[$nn]+="${kv_of[$nn]:+ }${BASH_REMATCH[2]}=${BASH_REMATCH[3]}"
  done
  IFS=$'\n' nns=($(printf '%s\n' "${nns[@]}" | sort -n)); unset IFS
  for nn in "${nns[@]}"; do
    key="${kv_of[$nn]}"
    [ -n "${evs_of[$key]:-}" ] || order+=("$key")
    evs_of[$key]+=" $nn"
  done
  say "   $label"
  for key in "${order[@]}"; do
    printf '     %s: %s\n' "$(ev_ranges "${evs_of[$key]}")" "$key"
  done
}

# nearest_disk_cap <스펙 Disk 값> — 고정 용량(480/600/960/1200/1900/7600) 중 가장 가까운 값.
# 거리가 같으면 큰 값(예: 540 은 480 과 600 이 둘 다 60 차이 → 600).
nearest_disk_cap() {
  local disk="$1"; local -a caps=(480 600 960 1200 1900 7600); local best="" bestdiff="" c diff
  for c in "${caps[@]}"; do
    diff=$((disk > c ? disk - c : c - disk))
    if [ -z "$best" ] || [ "$diff" -lt "$bestdiff" ] || { [ "$diff" -eq "$bestdiff" ] && [ "$c" -gt "$best" ]; }; then
      best="$c"; bestdiff="$diff"
    fi
  done
  printf '%s' "$best"
}

# build_mac_disk_args <CREATE_ARGS...> — -evNNDisk=<스펙값> 들을 nearest_disk_cap 으로 바꿔
# MAC_DISK_ARGS(print_ev_kv 로 보여줄 용도)와 MAC_DISK_OF[nn]=cap(mac_apply_disk 가 쓸 맵)을 채운다.
MAC_DISK_ARGS=(); declare -A MAC_DISK_OF=()
build_mac_disk_args() {
  MAC_DISK_ARGS=(); MAC_DISK_OF=()
  local a nn disk cap
  for a in "$@"; do
    [[ "$a" =~ ^-ev([0-9]+)Disk=([0-9]+)$ ]] || continue
    nn="${BASH_REMATCH[1]}"; disk="${BASH_REMATCH[2]}"
    cap="$(nearest_disk_cap "$disk")"
    MAC_DISK_OF[$nn]="$cap"
    MAC_DISK_ARGS+=("-ev${nn}Disk=$cap")
  done
}

# mac_apply_disk — 방금 만든 mac_info 출력(Provisioning_List_<vCenter>.txt, 실행 폴더 기준 상대경로)을
# 읽어 VM 이름 끝의 evNN 으로 MAC_DISK_OF[nn] 값을 찾아 10번째 칸(디스크)을 바꾼 뒤 MAC_ALL 에 이어붙인다.
mac_apply_disk() {
  local raw="Provisioning_List_${VC_IP//./_}.txt" line vmname nn cap
  [ -f "$raw" ] || { warn "MAC 목록 파일을 찾지 못했습니다: $RUN_DIR/$raw"; return; }
  while IFS= read -r line || [ -n "$line" ]; do
    vmname="$(awk '{print $4}' <<< "$line")"
    if [[ "$vmname" =~ ev([0-9]+)$ ]]; then
      nn="${BASH_REMATCH[1]}"; cap="${MAC_DISK_OF[$nn]:-}"
      [ -n "$cap" ] && line="$(awk -v cap="$cap" '{$10=cap}1' <<< "$line")"
    fi
    printf '%s\n' "$line" >> "$MAC_ALL"
  done < "$raw"
  rm -f "$raw"
}

hdr "실행 계획 (실행 폴더: $RUN_DIR)" 2>&1
say "vCenter        : ${VC_IP:-(미지정)} / 계정 $VC_ID"
say "설치 정보       : OS 버전 $MAC_ARGSTR / 인프라 $MAC_ARG1"
say "포트그룹 생성   : $(wc -l < "$RUN_DIR/vswitch.txt")건 (vswitch_setting${TARGET_VSWITCH:+, 스위치 $TARGET_VSWITCH})"
k=0
for d in "${SPEC_ORDER[@]}"; do
  k=$((k + 1)); printf '%s' "${SPEC_BMS[$d]}" > "$RUN_DIR/worklist_$k.txt"
  # affinity_setting/lpage_setting 은 worklist 문자열을 그대로 VM 이름 접두어로 쓴다(vm_create 는 BM 이름의 . 앞부분만 씀).
  # BM 이 esxi-node-001.domain 형태면 VM 이름은 esxi-node-001ev01 이므로 이 두 도구에는 짧은 이름 목록을 따로 넘긴다.
  awk -F. '{print $1}' "$RUN_DIR/worklist_$k.txt" > "$RUN_DIR/vmbase_$k.txt"
  spec_lookup "$(basename "$d")" && build_flags "$LOOKUP_DIR" || die "${LOOKUP_ERR:-$FLAG_ERR}"
  say "${C_BLD}스펙 $k        : $(basename "$d") — BM $(grep -c . "$RUN_DIR/worklist_$k.txt")대 × VM ${CREATE_ARGS[0]#-vmCount=}대${C_RST}"
  print_ev_kv "vm_create" "${CREATE_ARGS[@]}"
  build_mac_disk_args "${CREATE_ARGS[@]}"
  print_ev_kv "MAC 디스크(sda5, 자동매칭)" "${MAC_DISK_ARGS[@]}"
  say "   power_setting    BM $(grep -c . "$RUN_DIR/worklist_$k.txt")대 → 고성능 전원 정책(이미 고성능이면 스킵)"
  say "   affinity_setting ${AFF_ARGS[*]}"
  [ "${#LP_ARGS[@]}" -gt 0 ] && print_ev_kv "lpage_setting" "${LP_ARGS[@]}" || say "   lpage_setting: 스펙에 cores 가 없어 건너뜀"
  # vm-param-check 는 ev01 의 cores/numa 가 필수라, 없으면 생성 뒤 스펙 체크(6단계)를 할 수 없다
  [ -n "$(exp_get "$LOOKUP_DIR" cores-ev01)" ] && [ -n "$(exp_get "$LOOKUP_DIR" numa-ev01)" ] \
    || warn "스펙 $(basename "$d") 에 ev01 cores/numa 가 없어 생성 뒤 vm-param-check 스펙 체크는 건너뜁니다 (VM 생성은 진행)."
  print_affinity
done
say "어댑터 매핑     : $RUN_DIR/hostgroup.txt ($(wc -l < "$RUN_DIR/hostgroup.txt")건)"
say "MAC 수집       : mac_info -arg1=$MAC_ARG1 -argStr=$MAC_ARGSTR (디스크는 위 표대로 자동매칭) → ${awx_route:-(awx_route 비어 있음 — 복사 안 함)}${awx_route:+/${USER_TAG}.txt}"

if [ "$DRY_RUN" -eq 1 ]; then say; info "-n 지정 — vCenter 를 변경하지 않고 여기서 종료합니다."; exit 0; fi

# 비밀번호: 환경변수 → 암호 파일(../secret, passwd_update.sh 로 등록) → 직접 입력
if [ -z "${VC_PASSWORD:-}" ] && [ -f "$HERE/../secret_lib.sh" ]; then
  . "$HERE/../secret_lib.sh"
  if VC_PASSWORD="$(secret_get vcenter "$VC_ID")" && [ -n "$VC_PASSWORD" ]; then
    info "$VC_ID 비밀번호: 암호 파일에서 읽었습니다 ($(secret_file vcenter "$VC_ID"))"
  else
    VC_PASSWORD=""
  fi
fi
if [ -z "${VC_PASSWORD:-}" ]; then
  printf '%s 비밀번호: ' "$VC_ID" >&2; IFS= read -r -s VC_PASSWORD || die "입력이 끝났습니다."; echo >&2
fi
export VC_PASSWORD
printf '\n'
ask_yn "실제 vCenter($VC_IP)에 포트그룹/VM 을 생성·변경합니다. 진행할까요?" || { say "취소했습니다. vCenter 는 변경하지 않았습니다."; exit 0; }
printf '%s %s\n' "$VC_IP" "$(date '+%F %T')" > "$LAST_VC_FILE"   # 다음 실행에서 이전 실행 vCenter 로 보여준다

# ---------- 4) 실행 ----------
run() {
  local rc
  say; say "${C_DIM}\$ $*${C_RST}"
  "$@"; rc=$?
  [ "$rc" -eq 0 ] || die "실패: $(basename "$1") (종료코드 $rc) — 원인을 고친 뒤 다시 실행하면 이미 만든 포트그룹/VM 은 건너뜁니다."
}
cd "$RUN_DIR" || die "실행 폴더로 이동하지 못했습니다: $RUN_DIR"
CONC_ARG=(); [ -n "$CONC" ] && CONC_ARG=("-concurrency=$CONC")
VSW_ARG=(); [ -n "$TARGET_VSWITCH" ] && VSW_ARG=("-targetVSwitch=$TARGET_VSWITCH")
MAC_ALL="mac_all.txt"; : > "$MAC_ALL"

if [ -s vswitch.txt ]; then
  run "$HERE/vswitch_setting-source/vswitch_setting" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile=vswitch.txt "${VSW_ARG[@]}" "${CONC_ARG[@]}"
fi
k=0
for d in "${SPEC_ORDER[@]}"; do
  k=$((k + 1))
  spec_lookup "$(basename "$d")" && build_flags "$LOOKUP_DIR" || die "$FLAG_ERR"
  hdr "스펙 $k/${#SPEC_ORDER[@]}: $(basename "$d")" 2>&1
  run "$HERE/vm_create-source/vm_create" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile="worklist_$k.txt" -mapFile=hostgroup.txt "${CREATE_ARGS[@]}"
  build_mac_disk_args "${CREATE_ARGS[@]}"
  run "$HERE/mac_info-source/mac_info" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile="worklist_$k.txt" -arg1="$MAC_ARG1" -argInt=0 -argStr="$MAC_ARGSTR"
  mac_apply_disk
  run "$HERE/power_setting-source/power_setting" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile="worklist_$k.txt" "${CONC_ARG[@]}"
  run "$HERE/affinity_setting-source/affinity_setting" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile="vmbase_$k.txt" "${AFF_ARGS[@]}" "${CONC_ARG[@]}"
  if [ "${#LP_ARGS[@]}" -gt 0 ]; then
    run "$HERE/lpage_setting-source/lpage_setting" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile="vmbase_$k.txt" "${LP_ARGS[@]}" "${CONC_ARG[@]}"
  fi
done

# ---------- 5) 스펙 체크 (vm-param-check) ----------
# 방금 만든 VM 만, 이번에 쓴 스펙(-specFolder)으로 체크한다. 결과가 달라도 VM 은 이미 만들어졌으므로 중단하지 않는다.
hdr "스펙 체크 — vm-param-check (실행한 스펙과 같은지)" 2>&1
printf '%s\n' "$VC_IP" > vcenter_check.txt
CHECK_FAIL=0; k=0
for d in "${SPEC_ORDER[@]}"; do
  k=$((k + 1))
  spec_lookup "$(basename "$d")" || die "$LOOKUP_ERR"
  if [ -z "$(exp_get "$LOOKUP_DIR" cores-ev01)" ] || [ -z "$(exp_get "$LOOKUP_DIR" numa-ev01)" ]; then
    printf '  %s[건너뜀]%s 스펙 %s %s — ev01 cores/numa 가 없어 vm-param-check 로 체크할 수 없습니다\n' "$C_YEL" "$C_RST" "$k" "$(basename "$d")"
    continue
  fi
  g="$(exp_get "$LOOKUP_DIR" groups)"
  : > "check_targets_$k.txt"
  while read -r bm; do
    [ -n "$bm" ] || continue
    for n in $(seq 1 "$g"); do vm_name "$bm" "$n" >> "check_targets_$k.txt"; echo >> "check_targets_$k.txt"; done
  done < "worklist_$k.txt"
  VC_USER="$VC_ID" VC_PASS="$VC_PASSWORD" "$CHECK_BIN" -noColor -vcenterList=vcenter_check.txt -f="check_targets_$k.txt" \
    -specRoot="$SPEC_DIR" -specFolder="$(basename "$d")" -yes -onlyFail -out="check_$k.csv" > "check_$k.log" 2>&1 < /dev/null
  rc=$?
  total="$(sed -n 's/^총 \([0-9]*\)대 중 PASS \([0-9]*\)대, FAIL \([0-9]*\)대.*/\1 \2 \3/p' "check_$k.log" | tail -1)"
  if [ "$rc" -ne 0 ] || [ -z "$total" ]; then
    CHECK_FAIL=1; warn "스펙 $k $(basename "$d"): 체크를 끝내지 못했습니다 (종료코드 $rc) — $RUN_DIR/check_$k.log"
    tail -3 "check_$k.log" | sed 's/^/     /' >&2; continue
  fi
  set -- $total
  if [ "$3" -eq 0 ]; then
    printf '  %s[일치]%s 스펙 %s %s — VM %s대 모두 PASS\n' "$C_GRN" "$C_RST" "$k" "$(basename "$d")" "$1"
  else
    CHECK_FAIL=1
    printf '  %s[차이]%s 스펙 %s %s — VM %s대 중 PASS %s / FAIL %s\n' "$C_YEL" "$C_RST" "$k" "$(basename "$d")" "$1" "$2" "$3"
    grep -h '\[FAIL\]\|\[설정없음\]' "check_$k.log" | sed 's/: 기대값.*//' | sort | uniq -c | sort -rn | head -8 | sed 's/^/      /'
  fi
done

# ---------- 7) MAC 목록 합본 → awx_route 로 <user>.txt 복사 ----------
# mac_info 는 스펙마다(4단계에서) 실행했고, mac_apply_disk 가 디스크를 바꿔 MAC_ALL 에 이어붙였다.
if [ -s "$MAC_ALL" ]; then
  hdr "MAC 수집 결과" 2>&1
  if [ -z "$awx_route" ]; then
    info "awx_route 가 비어 있어 복사하지 않습니다: $RUN_DIR/$MAC_ALL"
  elif mkdir -p "$awx_route" && cp "$MAC_ALL" "$awx_route/${USER_TAG}.txt"; then
    info "MAC 목록 복사: $RUN_DIR/$MAC_ALL → $awx_route/${USER_TAG}.txt"
  else
    die "MAC 목록 복사 실패: $RUN_DIR/$MAC_ALL → $awx_route/${USER_TAG}.txt"
  fi
fi

printf '\n%s[완료]%s VM 생성·설정을 마쳤습니다.\n' "$C_GRN$C_BLD" "$C_RST"
if [ "$CHECK_FAIL" -eq 1 ]; then
  cat <<EOF
  - 스펙과 다른 항목이 있습니다. 자세한 내용: $RUN_DIR/check_<번호>.log, check_<번호>.csv
    교정: cd $CHECK_DIR && VC_USER=$VC_ID ./vm-param-check -vcenterList=$RUN_DIR/vcenter_check.txt \\
          -f=$RUN_DIR/check_targets_<번호>.txt -specRoot=$SPEC_DIR -specFolder=<스펙 폴더> -yes -fix
    (lpage_setting 은 numa.vcpu.maxPerVirtualNode 를 기존 동작대로 쓰고, 스펙의 preferHT 는 vm_setup 이 설정하지 않으므로 -fix 로 맞춘다.)
EOF
fi
cat <<EOF
  - 만든 VM 의 포트그룹만 바꾸려면 nic_assign: $HERE/nic_assign-source/nic_assign -vcTargetIP=$VC_IP -mapFile=<VM 이름 포트그룹 목록>
EOF
