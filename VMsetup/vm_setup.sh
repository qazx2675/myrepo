#!/usr/bin/env bash
# vm_setup.sh — SPEC_DIR 스펙으로 VM을 만들고 설정하는 실행 편의 스크립트.
#
# 흐름:
#   1) ${user}.txt(BM 목록) + SPEC_DIR/vswitch_${user}.txt(BM 포트그룹 VLAN) 읽기
#   2) BM별 스펙 자동 할당(포트그룹 이름의 <폴더명>-cae-a-b-c-d 에서 폴더명 추출) → 표 확인(y/n)
#      → n 이거나 미할당이면 SPEC_DIR 목록에서 선택, 목록에 없으면 vim으로 새 스펙 입력(+ev별 affinity)
#   3) VM(evNN)별 포트그룹(네트워크 어댑터 1) 자동 할당 → 표 확인(y/n) → 수동 선택 → 목록에 없으면 vim
#   4) vCenter 선택 (-v 가 없으면 vcenter.txt 목록에서 번호로, Enter = 이 user 의 이전 실행 vCenter)
#   5) vswitch_setting(BM에 포트그룹 생성, 호스트 병렬) → vm_create → affinity_setting → lpage_setting (스펙별로, 도구 안에서 병렬)
#
# 실제 vCenter를 변경하는 단계(5) 직전에 요약을 보여주고 한 번 더 확인받는다. -n 이면 여기서 멈춘다.
# 도구가 실패하면(종료코드 0 이 아니면) 그 자리에서 멈춘다. 이미 있는 포트그룹/VM 은 실패가 아니다.
# 스펙 해석은 vm-param-check -specExport 가 맡는다(체크와 같은 파서 — 파서를 두 벌 만들지 않음).
set -o pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
USER_TAG=""; VC_IP="${VC_IP:-}"; VC_ID="${VC_ID:-administrator@vsphere.local}"
SPEC_DIR=""; CONC=""; TARGET_VSWITCH=""; DRY_RUN=0
EDITOR_CMD="${VM_SETUP_EDITOR:-vim}"

usage() {
  cat <<EOF
사용법: $0 -u <user> -v <vCenter IP> [옵션]

  -u <user>     작업 이름. ${HERE}/<user>.txt (BM 목록), SPEC_DIR/vswitch_<user>.txt 를 읽는다 (필수)
  -v <ip>       vCenter 접속 IP (환경변수 VC_IP 도 가능). 없으면 vcenter.txt 목록에서 번호로 고른다
                (V2 폴더의 vcenter.txt, 없으면 vm-param-check 폴더의 것. Enter = 이 user 의 이전 실행 vCenter)
  -i <id>       vCenter 계정 (기본: ${VC_ID}). 비밀번호는 환경변수 VC_PASSWORD, 없으면 물어본다
  -s <dir>      SPEC_DIR 경로 (기본: ${HERE}/../SPEC_DIR)
  -w <vswitch>  포트그룹을 만들 가상 스위치 (기본: vswitch_setting 기본값 vSwitch0)
  -c <n>        vswitch/affinity/lpage 동시 처리 수 (기본: 각 도구 기본값)
  -n            확인만: 스펙·포트그룹 할당까지 정하고 실행 계획을 보여준 뒤 vCenter는 변경하지 않고 종료
  -h            도움말

환경변수 VM_SETUP_EDITOR 로 vim 대신 다른 편집기를 쓸 수 있다.
EOF
}

say()  { printf '%s\n' "$*"; }
warn() { printf '[경고] %s\n' "$*" >&2; }
die()  { printf '[오류] %s\n' "$*" >&2; exit 1; }

# prompt <변수명> <안내문> — 표준입력에서 한 줄 읽는다(EOF면 중단: 무인 실행으로 엉뚱한 기본값이 선택되지 않게).
prompt() {
  local __name="$1"
  printf '%s' "$2" >&2
  IFS= read -r "$__name" || die "입력이 끝났습니다(stdin EOF) — 대화형으로 실행하세요."
}
ask_yn() {
  local __a
  while :; do
    prompt __a "$1 (y/n): "
    case "${__a,,}" in y|yes) return 0 ;; n|no) return 1 ;; esac
  done
}

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
[ -n "$USER_TAG" ] || { usage >&2; die "-u <user> 가 필요합니다."; }
[[ "$USER_TAG" =~ ^[A-Za-z0-9._-]+$ ]] || die "user 에는 영문/숫자/._- 만 쓸 수 있습니다: $USER_TAG"
[ -n "$SPEC_DIR" ] || SPEC_DIR="$HERE/../SPEC_DIR"
[ -d "$SPEC_DIR" ] || die "SPEC_DIR 를 찾을 수 없습니다: $SPEC_DIR"
SPEC_DIR="$(cd "$SPEC_DIR" && pwd)"
CHECK_DIR="$HERE/../vm-param-check-usability-improvement/vm-param-check"
CHECK_BIN="$CHECK_DIR/vm-param-check"

BM_FILE="$HERE/${USER_TAG}.txt"
VSW_FILE="$SPEC_DIR/vswitch_${USER_TAG}.txt"
[ -f "$BM_FILE" ]  || die "BM 목록 파일이 없습니다: $BM_FILE (한 줄에 BM 하나)"
[ -f "$VSW_FILE" ] || die "포트그룹 파일이 없습니다: $VSW_FILE (BM 포트그룹 VLAN)"

# ---------- 필요한 실행파일 (없으면 vendor 로 오프라인 빌드) ----------
ensure_bin() {
  local bin="$1" dir="$2"
  [ -x "$bin" ] && return 0
  say "[INFO] $(basename "$bin") 실행파일이 없어 빌드합니다..."
  (cd "$dir" && bash setup.sh >/dev/null) || die "빌드 실패: $dir"
}
ensure_bin "$CHECK_BIN" "$CHECK_DIR"
for t in vm_create vswitch_setting affinity_setting lpage_setting nic_assign; do
  ensure_bin "$HERE/${t}-source/$t" "$HERE/${t}-source"
done

# ---------- 입력 읽기 ----------
read_list() { sed -e 's/\r$//' -e '1s/^\xef\xbb\xbf//' -e 's/#.*$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' "$1" | awk 'NF'; }

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
[ -n "$LAST_VC" ] && say "[INFO] $USER_TAG 이전 실행 vCenter: $LAST_VC ($LAST_VC_AT)"

# select_vcenter — VC_IP 를 채운다. Enter = 이전 실행 vCenter.
select_vcenter() {
  local -a vcs=(); local i ans mark
  [ -n "$VC_LIST_FILE" ] && mapfile -t vcs < <(read_list "$VC_LIST_FILE" | awk '{print $1}')
  while :; do
    if [ "${#vcs[@]}" -gt 0 ]; then
      printf '\n=== vCenter 선택 (%s) ===\n' "$VC_LIST_FILE" >&2
      for i in "${!vcs[@]}"; do
        mark=""; [ "${vcs[$i]}" = "$LAST_VC" ] && mark="   <- 이전 실행"
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

# spec_key_list <n> — 그룹 n(1~10)의 스펙 키 이름들. ev01 은 접미사 없는 키(cpu/mem/...)를 쓴다.
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
    echo "# ev01 은 필수이고, ev02~ev10 은 값을 하나라도 적으면 그 ev 를 만든다."
    echo "#   - 값을 적은 ev 는 cpu/mem/disk/shares 를 모두 적어야 한다"
    echo "#   - ev 번호는 ev01 부터 빠짐없이 이어져야 한다 (ev02 를 비우고 ev03 을 적으면 오류)"
    echo "# affinity 파일은 여기서 적지 않는다 — 저장한 뒤 ev 별로 (직전 ev 와 같은 파일 / 기존 파일 / vim 입력) 중에서 고른다."
    echo
    echo 'folder=""        # [필수] 새로 만들 스펙 폴더 이름. 예: TST-CAE001-SAMP48c-QRST (SPEC_DIR 아래 이 이름으로 저장됨)'
    echo 'ht=""            # [필수] 하이퍼스레딩 on 또는 off (vm-param-check 가 이 스펙으로 체크할 때 사용)'
    for n in $(seq 1 10); do
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
          for n in $(seq 1 10); do for k in $(spec_key_list "$n"); do
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
    # VM 생성에 못 쓰는 스펙(예: affinity 파일 없음)은 자동 할당하지 않고 이유를 알린다
    if build_flags "${cands[0]}"; then SPEC_OF[$bm]="${cands[0]}"
    else warn "$bm: 자동 매칭된 스펙 $(basename "${cands[0]}") 을(를) 쓸 수 없습니다 — $FLAG_ERR"; fi
  done
}

print_spec_table() {
  local bm
  printf '\n=== BM → 스펙 ===\n' >&2
  printf '%-32s %-36s %s\n' "BM" "포트그룹" "스펙 폴더" >&2
  for bm in "${BMS[@]}"; do
    printf '%-32s %-36s %s\n' "$bm" "${BM_PGS[$bm]:-(없음)}" "$( [ -n "${SPEC_OF[$bm]:-}" ] && basename "${SPEC_OF[$bm]}" || echo '(미할당)')" >&2
  done
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
    if spec_lookup "$(basename "${specs[$((ans - 1))]}")" && build_flags "$LOOKUP_DIR"; then
      SPEC_OF[$bm]="$LOOKUP_DIR"; LAST_SPEC="$LOOKUP_DIR"; return 0
    fi
    warn "이 스펙은 VM 생성에 쓸 수 없습니다: ${LOOKUP_ERR:-$FLAG_ERR}"
  done
}

say "[INFO] BM ${#BMS[@]}대, SPEC_DIR=$SPEC_DIR"
auto_assign_specs
while :; do
  print_spec_table
  unassigned=0
  for bm in "${BMS[@]}"; do [ -n "${SPEC_OF[$bm]:-}" ] || unassigned=1; done
  if [ "$unassigned" -eq 1 ]; then
    say "[INFO] 스펙을 자동으로 정하지 못한 BM 은 직접 선택합니다." >&2
    for bm in "${BMS[@]}"; do [ -n "${SPEC_OF[$bm]:-}" ] || pick_spec "$bm"; done
    continue
  fi
  ask_yn "위 스펙 할당이 맞습니까?" && break
  LAST_SPEC=""
  for bm in "${BMS[@]}"; do pick_spec "$bm"; done
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
  local vm
  printf '\n=== VM → 포트그룹 (네트워크 어댑터 1) ===\n' >&2
  printf '%-34s %-30s %s\n' "VM" "BM" "포트그룹" >&2
  for vm in "${VMS[@]}"; do
    printf '%-34s %-30s %s\n' "$vm" "${VM_BM[$vm]}" "${NIC_OF[$vm]:-(어댑터 없음)}" >&2
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
print_nic_table
nic_hdr=0
for vm in "${VMS[@]}"; do
  [ -z "${NIC_OF[$vm]:-}" ] || continue                     # 자동으로 정해졌다
  [ -n "${BM_PGS[${VM_BM[$vm]}]:-}" ] || continue           # BM 에 포트그룹이 없으면 고를 게 없다
  if [ "$nic_hdr" -eq 0 ]; then say "[INFO] 포트그룹을 자동으로 정하지 못한 VM 은 직접 선택합니다." >&2; nic_hdr=1; fi
  pick_nic "$vm"
done
while :; do
  print_nic_table
  ask_yn "위 포트그룹 할당이 맞습니까?" && break
  for vm in "${VMS[@]}"; do pick_nic "$vm"; done
done
for vm in "${VMS[@]}"; do
  [ -n "${NIC_OF[$vm]:-}" ] || warn "$vm 는 네트워크 어댑터 없이 만들어집니다."
done

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

printf '\n=== 실행 계획 (실행 폴더: %s) ===\n' "$RUN_DIR"
say "vCenter        : ${VC_IP:-(미지정)} / 계정 $VC_ID"
say "포트그룹 생성   : $(wc -l < "$RUN_DIR/vswitch.txt")건 (vswitch_setting${TARGET_VSWITCH:+, 스위치 $TARGET_VSWITCH})"
k=0
for d in "${SPEC_ORDER[@]}"; do
  k=$((k + 1)); printf '%s' "${SPEC_BMS[$d]}" > "$RUN_DIR/worklist_$k.txt"
  # affinity_setting/lpage_setting 은 worklist 문자열을 그대로 VM 이름 접두어로 쓴다(vm_create 는 BM 이름의 . 앞부분만 씀).
  # BM 이 esxi-node-001.domain 형태면 VM 이름은 esxi-node-001ev01 이므로 이 두 도구에는 짧은 이름 목록을 따로 넘긴다.
  awk -F. '{print $1}' "$RUN_DIR/worklist_$k.txt" > "$RUN_DIR/vmbase_$k.txt"
  spec_lookup "$(basename "$d")" && build_flags "$LOOKUP_DIR" || die "${LOOKUP_ERR:-$FLAG_ERR}"
  say "스펙 $k        : $(basename "$d") — BM $(grep -c . "$RUN_DIR/worklist_$k.txt")대 × VM ${CREATE_ARGS[0]#-vmCount=}대"
  say "   vm_create ${CREATE_ARGS[*]}"
  say "   affinity_setting ${AFF_ARGS[*]}"
  [ "${#LP_ARGS[@]}" -gt 0 ] && say "   lpage_setting ${LP_ARGS[*]}" || say "   lpage_setting: 스펙에 cores 가 없어 건너뜀"
done
say "어댑터 매핑     : $RUN_DIR/hostgroup.txt ($(wc -l < "$RUN_DIR/hostgroup.txt")건)"

if [ "$DRY_RUN" -eq 1 ]; then say; say "[INFO] -n 지정 — vCenter 를 변경하지 않고 여기서 종료합니다."; exit 0; fi

if [ -z "${VC_PASSWORD:-}" ]; then
  printf '%s 비밀번호: ' "$VC_ID" >&2; IFS= read -r -s VC_PASSWORD || die "입력이 끝났습니다."; echo >&2
fi
export VC_PASSWORD
printf '\n'
ask_yn "실제 vCenter($VC_IP)에 포트그룹/VM 을 생성·변경합니다. 진행할까요?" || { say "취소했습니다. vCenter 는 변경하지 않았습니다."; exit 0; }
printf '%s %s\n' "$VC_IP" "$(date '+%F %T')" > "$LAST_VC_FILE"   # 다음 실행에서 이전 실행 vCenter 로 보여준다

# ---------- 4) 실행 ----------
run() { say; say "\$ $*"; "$@" || die "실패: $1 (종료코드 $?) — 원인을 고친 뒤 다시 실행하면 이미 만든 포트그룹/VM 은 건너뜁니다."; }
cd "$RUN_DIR" || die "실행 폴더로 이동하지 못했습니다: $RUN_DIR"
CONC_ARG=(); [ -n "$CONC" ] && CONC_ARG=("-concurrency=$CONC")
VSW_ARG=(); [ -n "$TARGET_VSWITCH" ] && VSW_ARG=("-targetVSwitch=$TARGET_VSWITCH")

if [ -s vswitch.txt ]; then
  run "$HERE/vswitch_setting-source/vswitch_setting" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile=vswitch.txt "${VSW_ARG[@]}" "${CONC_ARG[@]}"
fi
k=0
for d in "${SPEC_ORDER[@]}"; do
  k=$((k + 1))
  spec_lookup "$(basename "$d")" && build_flags "$LOOKUP_DIR" || die "$FLAG_ERR"
  say; say "===== 스펙 $k/${#SPEC_ORDER[@]}: $(basename "$d") ====="
  run "$HERE/vm_create-source/vm_create" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile="worklist_$k.txt" -mapFile=hostgroup.txt "${CREATE_ARGS[@]}"
  run "$HERE/affinity_setting-source/affinity_setting" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile="vmbase_$k.txt" "${AFF_ARGS[@]}" "${CONC_ARG[@]}"
  if [ "${#LP_ARGS[@]}" -gt 0 ]; then
    run "$HERE/lpage_setting-source/lpage_setting" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile="vmbase_$k.txt" "${LP_ARGS[@]}" "${CONC_ARG[@]}"
  fi
done

cat <<EOF

[완료] VM 생성·설정을 마쳤습니다. 설정 변경 후에는 랜덤한 서버 몇 대를 골라 vCenter 에서 실제로 반영됐는지 확인하세요.
  - 스펙대로 맞는지 체크: cd $CHECK_DIR && ./vm-param-check -specRoot=$SPEC_DIR -f=<VM 이름 목록> ...
    (lpage_setting 은 NUMA 노드당 코어 수를 스펙 numa 값으로 맞추지만 numa.vcpu.maxPerVirtualNode 는 기존 동작대로 쓰므로,
     vm-param-check 결과에서 FAIL 이 나오면 -fix 로 교정하세요.)
  - 만든 VM 의 포트그룹만 바꾸려면 nic_assign: $HERE/nic_assign-source/nic_assign -vcTargetIP=$VC_IP -mapFile=<VM 이름 포트그룹 목록>
EOF
