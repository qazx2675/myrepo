#!/usr/bin/env bash
# lpage_from_spec.sh — SPEC_DIR 의 스펙을 읽어 lpage_setting 옵션을 만들어 넘기는 단독 스크립트.
# vm_setup.sh -del_lpage 로 실행했을 때의 lpage 단계와 똑같이 동작한다:
#   HugePage 키(sched.mem.lpage.enable1GPage / sched.mem.pin / sched.mem.prealloc /
#   sched.mem.prealloc.pinnedMainMem / sched.swap.vmxSwapEnabled)는 삭제하고,
#   CPU 토폴로지(소켓당 코어 수, NUMA)와 numa.vcpu.maxPerVirtualNode 는 스펙대로 적용한다.
#   (cpuid.coresPerSocket 은 지우지 않는다.) 다른 설정(VM 생성/affinity/전원 등)은 건드리지 않는다.
#
# 사용법:
#   bash lpage_from_spec.sh -spec <스펙 폴더명> -w <BM 목록 파일> -vc <vCenter IP> [-id <계정>] [-s <SPEC_DIR>] [-c <동시처리>] [-n]
#     -spec  SPEC_DIR 아래 스펙 폴더 이름 (CAE 번호는 무시하고 매칭 — vm_setup.sh 와 같은 규칙)
#     -w     BM(ESXi) 호스트명 목록, 한 줄에 하나. VM 이름은 <BM 이름의 . 앞부분>ev01, ev02 ...
#     -n     실행하지 않고 만들어진 명령만 출력
#   비밀번호: VC_PASSWORD 환경변수 → ../secret(passwd_update.sh 로 등록) → 직접 입력 순.
set -uo pipefail
unset VC_PASS VCENTER_PASS
HERE="$(cd "$(dirname "$0")" && pwd)"
CHECK_DIR="$HERE/../vm-param-check-usability-improvement/vm-param-check"
CHECK_BIN="$CHECK_DIR/vm-param-check"
LP_BIN="$HERE/lpage_setting-source/lpage_setting"
VC_ID="${VC_ID:-lscsystems@vsphere.local}"
spec=""; wl=""; VC_IP=""; SPEC_DIR=""; conc=""; dry=0
die() { echo "[오류] $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    -spec) shift; spec="${1:-}" ;;   -spec=*) spec="${1#-spec=}" ;;
    -w) shift; wl="${1:-}" ;;        -w=*) wl="${1#-w=}" ;;
    -vc) shift; VC_IP="${1:-}" ;;    -vc=*) VC_IP="${1#-vc=}" ;;
    -id) shift; VC_ID="${1:-}" ;;    -id=*) VC_ID="${1#-id=}" ;;
    -s) shift; SPEC_DIR="${1:-}" ;;  -s=*) SPEC_DIR="${1#-s=}" ;;
    -c) shift; conc="${1:-}" ;;      -c=*) conc="${1#-c=}" ;;
    -n) dry=1 ;;
    *) die "알 수 없는 옵션: $1 (사용법은 파일 맨 위 주석 참고)" ;;
  esac
  shift
done
[ -n "$spec" ] && [ -n "$wl" ] && [ -n "$VC_IP" ] || die "-spec, -w, -vc 는 필수입니다."
[ -f "$wl" ] || die "BM 목록 파일이 없습니다: $wl"
[ -x "$CHECK_BIN" ] || die "vm-param-check 바이너리가 없습니다: $CHECK_BIN (스펙 해석에 씁니다)"
[ -x "$LP_BIN" ] || die "lpage_setting 바이너리가 없습니다: $LP_BIN"
if [ -z "$SPEC_DIR" ]; then
  if [ -d "$CHECK_DIR/SPEC_DIR" ]; then SPEC_DIR="$CHECK_DIR/SPEC_DIR"; else SPEC_DIR="$HERE/../SPEC_DIR"; fi
fi
[ -d "$SPEC_DIR" ] || die "SPEC_DIR 를 찾을 수 없습니다: $SPEC_DIR"
SPEC_DIR="$(cd "$SPEC_DIR" && pwd)"

# ---- 스펙 읽기 (vm_setup.sh 와 같은 -specExport) ----
out="$("$CHECK_BIN" -specRoot="$SPEC_DIR" -specExport="$spec" 2>&1)" || die "스펙을 읽지 못했습니다: $(printf '%s' "$out" | tail -2)"
get() { printf '%s\n' "$out" | sed -n "s/^$1=//p" | head -1; }
is_uint() { [[ "$1" =~ ^[0-9]+$ ]] && [ "$1" -gt 0 ]; }
g="$(get groups)"; is_uint "$g" || die "스펙에서 ev 개수를 읽지 못했습니다"

# ---- lpage_setting 옵션 만들기: cores=총 vCPU, sockets=cpu/소켓당코어, numa=cpu/NUMA노드당vCPU ----
LP_ARGS=()
for n in $(seq 1 "$g"); do
  nn="$(printf '%02d' "$n")"
  cpu="$(get "cpu-ev$nn")"; cps="$(get "cores-ev$nn")"; numa="$(get "numa-ev$nn")"
  is_uint "$cpu" || die "cpu-ev$nn 값이 양의 정수가 아닙니다: '$cpu'"
  [ -n "$cps" ] || continue
  is_uint "$cps" || die "cores-ev$nn 값이 양의 정수가 아닙니다: '$cps'"
  [ $((cpu % cps)) -eq 0 ] || die "ev$nn: cpu($cpu)가 cores($cps)로 나누어떨어지지 않습니다"
  LP_ARGS+=("-ev${nn}Cores=$cpu" "-ev${nn}Sockets=$((cpu / cps))")
  if [ -n "$numa" ]; then
    is_uint "$numa" || die "numa-ev$nn 값이 양의 정수가 아닙니다: '$numa'"
    [ $((cpu % numa)) -eq 0 ] || die "ev$nn: cpu($cpu)가 numa($numa)로 나누어떨어지지 않습니다"
    LP_ARGS+=("-ev${nn}Numa=$((cpu / numa))")
  fi
done
[ "${#LP_ARGS[@]}" -gt 0 ] || die "스펙에 cores 가 없어 적용할 토폴로지가 없습니다"

# ---- 대상: BM 이름의 . 앞부분 (lpage_setting 은 이 문자열 + ev01.. 을 VM 이름으로 쓴다) ----
work="$(mktemp -d)"; trap 'rm -rf "$work"' EXIT
sed -e 's/\r$//' -e 's/#.*$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' "$wl" | awk 'NF {split($1,a,"."); print a[1]}' > "$work/vmbase.txt"
[ -s "$work/vmbase.txt" ] || die "BM 목록이 비어 있습니다: $wl"

CONC_ARG=(); [ -n "$conc" ] && CONC_ARG=("-concurrency=$conc")
cmd=("$LP_BIN" -vcTargetIP="$VC_IP" -id="$VC_ID" -worklistFile="$work/vmbase.txt" "${LP_ARGS[@]}" -del_lpage ${CONC_ARG[@]+"${CONC_ARG[@]}"})
echo "스펙 $(basename "$(get specdir)") — BM $(wc -l < "$work/vmbase.txt")대 × VM ${g}대"
printf '$ %s\n' "${cmd[*]}"
[ "$dry" = 1 ] && exit 0

if [ -z "${VC_PASSWORD:-}" ] && [ -f "$HERE/../secret_lib.sh" ]; then
  . "$HERE/../secret_lib.sh"
  VC_PASSWORD="$(secret_get vcenter "$VC_ID" 2>/dev/null)" || VC_PASSWORD=""
fi
[ -n "${VC_PASSWORD:-}" ] || { read -r -s -p "$VC_ID 비밀번호: " VC_PASSWORD; echo; }
export VC_PASSWORD
"${cmd[@]}"
