#!/usr/bin/env bash
# ucsm_check.sh — Cisco UCSM(XML API) 관리 블레이드의 BIOS 표준값 점검 래퍼 (읽기 전용, 설정은 하지 않음).
#
# 사용법:
#   bash ucsm_check.sh                                # user.txt 의 UCSM 주소를 점검
#   bash ucsm_check.sh -user ucsm.txt -list-max 50    # 나머지 인자는 그대로 biostool ucsm 에 전달
#
# 하는 일: 스크립트 위치로 이동 → OS 에 맞는 바이너리 선택(없으면 빌드 시도) → 필요한 파일 확인 →
#          bin/biostool ucsm 실행. 대상 파일의 각 줄은 블레이드 BMC 가 아니라 UCSM(Fabric Interconnect VIP) 주소입니다.
# UCSM 에 보내는 호출은 aaaLogin / configResolveClass(읽기) / aaaLogout 뿐입니다.
set -euo pipefail
cd "$(dirname "$0")"
. ./lib.sh

usage() {
  cat <<'EOF'
사용법:
  bash ucsm_check.sh [biostool ucsm 옵션...]

  -user <파일>         점검할 UCSM(Fabric Interconnect VIP) 주소 목록 (기본 user.txt, 한 줄에 IP 또는 이름)
  -profile <이름>      기대값 프로파일 (기본 VM, profile_dir/<이름>.tsv 의 # [xml] 구역)
  -list-max <N>        FAIL·UNVERIFIED 서버, MAPPING_MISSING·UNSUPPORTED 묶음을 나열할 최대 수 (기본 20)
  -conf <파일>, -hosts <파일>

UCSM 에 1순위 계정이 없어 AUTH_FAIL 이면: bash account_create.sh -ucsm  (rf.conf 의 cisco_user/cisco_pass 로 만듭니다)

처음 한 번 준비할 것:
  cp bios.conf.example bios.conf      # user= 에 UCSM 계정 ID 입력 (읽기 전용 계정 권장)
  bash encrypt.sh                     # UCSM 비밀번호 입력 -> pass.enc + key.bin 생성
  cp user.txt.example  user.txt       # 점검할 UCSM 주소를 한 줄씩 (블레이드 BMC 주소가 아님)

결과: results/<일시>/ucsm_result.tsv (Redfish 점검의 result.tsv 와 별도)
EOF
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

select_bin || exit 2

# 인자로 -conf/-user/-profile 을 바꿨으면 그 값을, 아니면 기본 파일명을 점검 대상으로 삼는다.
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
profdir="$(conf_val "$conf" profile_dir profiles)"
profile="$(opt_val profile VM)"
passfile="$(conf_val "$conf" pass_file pass.enc)"
keyfile="$(conf_val "$conf" key_file key.bin)"

missing=0
if [ ! -f "$conf" ]; then
  echo "[없음] $conf  -> cp bios.conf.example $conf  후 user= 에 UCSM 계정 ID 입력" >&2
  missing=1
fi
if [ ! -f "$userfile" ]; then
  echo "[없음] $userfile  -> cp user.txt.example $userfile  후 점검할 UCSM 주소를 한 줄에 하나씩 입력" >&2
  missing=1
fi
if [ ! -f "$passfile" ] || [ ! -f "$keyfile" ]; then
  echo "[없음] $passfile / $keyfile  -> bash encrypt.sh  (UCSM 비밀번호를 암호화해 생성)" >&2
  missing=1
fi
if [ ! -f "$profdir/$profile.tsv" ]; then
  echo "[없음] $profdir/$profile.tsv  -> -profile <이름> 으로 # [xml] 구역이 있는 프로파일 지정" >&2
  missing=1
fi
if [ "$missing" = "1" ]; then
  echo "위 파일을 만든 뒤 다시 실행하십시오." >&2
  exit 1
fi

exec "$BIN" ucsm ${args[@]+"${args[@]}"}
