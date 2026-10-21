#!/usr/bin/env bash
# cisco/trs.sh — Cisco 장비(UCSX-210C-M7 등) 조사 장애 원인 진단 (읽기 전용).
#   bios.sh 에서 attr 이 비고 비고가 "UNSUPPORTED [XML 실패 → Redfish]" 로 나올 때 쓴다.
#
# 사용법:
#   bash cisco/trs.sh                           # user.txt 의 Cisco BMC 주소를 진단
#   bash cisco/trs.sh -targets 10.1.2.3         # 대상을 직접 지정 (쉼표·공백 구분, ip:포트 가능)
#   bash cisco/trs.sh -targets c101-m -profile VM    # hosts 의 이름도 가능, 프로파일 # [xml] 클래스까지 확인
#   bash cisco/trs.sh -path /redfish/v1/Systems/1/Bios   # 추가로 GET 해 볼 Redfish 경로 (쉼표 구분)
#   bash cisco/trs.sh -conf my.conf -user list.txt
#
# 하는 일: 스크립트 위치(또는 상위 폴더의 lib.sh 위치)로 이동 → OS 에 맞는 바이너리 선택 → 필요한 파일 확인
#   → bin/biostool trs-cisco 실행.
#   Redfish: TCP/TLS → ServiceRoot → 인증(Basic 1회 + 세션 1회) → System 의 Bios 링크·Oem·Chassis·Manager
#            → 레지스트리·펌웨어 목록 → 하위 리소스에서 BIOS 속성 찾기 → bios.sh 방식 재현
#   XML    : GET / → 쿠키 없는 확인 요청 1회(XML API 가 있는지만) → 있을 때만 aaaLogin 1회 + 서버 목록 조회
#   그리고 두 경로의 원인을 종합해 출력하고 results/<일시>_trs_cisco/ 에 호스트별로 저장합니다.
# 설정 변경·재부팅은 하지 않습니다. 비밀번호·토큰은 출력·저장하지 않습니다.
set -euo pipefail
caller_pwd="$PWD"
here="$(cd "$(dirname "$0")" && pwd)"
me="$(basename "$0")"
# 이 스크립트는 저장소 루트(lib.sh 가 있는 곳) 또는 그 아래 cisco/ 폴더 어디에 있어도 된다.
root="$here"
[ -f "$root/lib.sh" ] || { root="$here/.."; me="cisco/$me"; }
cd "$root"
. ./lib.sh

usage() {
  cat <<EOF2
사용법:
  bash $me [biostool trs-cisco 옵션...]

  -targets <대상>   진단할 Cisco BMC (IP/hostname, 쉼표·공백 구분, ip:포트 가능). 지정하면 user.txt 를 읽지 않음
  -user <파일>      대상 목록 (기본 user.txt, 한 줄에 하나 — bios.sh 에서 UNSUPPORTED 가 나온 장비의 관리망 주소)
  -profile <이름>   XML 로 확인할 # [xml] 구역 프로파일 (기본 VM, 없어도 진행)
  -path <경로>      추가로 GET 해 볼 Redfish 경로 (쉼표 구분)
  -conf <파일>, -hosts <파일>, -rf-conf <파일>

처음 한 번 준비할 것 (bios.sh 와 같음):
  cp bios.conf.example bios.conf      # user= 에 BMC 계정 ID 입력
  bash encrypt.sh                     # BMC 비밀번호 입력 -> pass.enc + key.bin 생성
  cp user.txt.example  user.txt       # 진단할 Cisco BMC 주소를 한 줄에 하나씩

계정 잠금 방지: Redfish 인증은 Basic 1회 + 세션 1회(401 이면 중단), XML 로그인은 1회이며 XML API 가 없는 주소에는
비밀번호를 보내지 않습니다. Redfish 에서 401 이 나온 계정으로는 XML 로그인도 하지 않습니다.
결과: 화면 출력 + results/<일시>_trs_cisco/<호스트>.txt  (이 내용을 그대로 붙여 주시면 원인 분석·도구 수정에 쓸 수 있습니다)
EOF2
}

args=()
while [ $# -gt 0 ]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    *)
      args+=("$1")
      shift
      ;;
  esac
done

fix_path_args "$caller_pwd"
select_bin || exit 2

opt_val() { # <옵션이름> <기본값> : args 에서 -name X / --name X / -name=X 를 찾는다.
  local name="$1" def="$2" i n=${#args[@]}
  for ((i = 0; i < n; i++)); do
    case "${args[$i]}" in
      "-$name" | "--$name")
        if [ $((i + 1)) -lt "$n" ]; then
          echo "${args[$((i + 1))]}"
          return
        fi
        ;;
      "-$name="* | "--$name="*)
        echo "${args[$i]#*=}"
        return
        ;;
    esac
  done
  echo "$def"
}

conf="$(opt_val conf bios.conf)"
userfile="$(opt_val user user.txt)"
targets="$(opt_val targets "")"
passfile="$(conf_val "$conf" pass_file pass.enc)"
keyfile="$(conf_val "$conf" key_file key.bin)"

missing=0
if [ ! -f "$conf" ]; then
  echo "[없음] $conf  -> cp bios.conf.example $conf  후 user= 에 BMC 계정 ID 입력" >&2
  missing=1
fi
if [ -z "$targets" ] && [ ! -f "$userfile" ]; then
  echo "[없음] $userfile  -> cp user.txt.example $userfile  후 진단할 Cisco BMC 주소를 한 줄에 하나씩 입력 (또는 -targets <IP>)" >&2
  missing=1
fi
if [ ! -f "$passfile" ] || [ ! -f "$keyfile" ]; then
  echo "[없음] $passfile / $keyfile  -> bash encrypt.sh  (BMC 비밀번호를 암호화해 생성)" >&2
  missing=1
fi
if [ "$missing" = "1" ]; then
  echo "위 파일을 만든 뒤 다시 실행하십시오." >&2
  exit 1
fi

exec "$BIN" trs-cisco ${args[@]+"${args[@]}"}
