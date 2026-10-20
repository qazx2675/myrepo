#!/usr/bin/env bash
# trs.sh — BMC 접속 장애(403 등) 트러블슈팅 실행 래퍼 (읽기 전용 진단).
#
# 사용법:
#   bash trs.sh                          # user.txt 의 BMC 관리망 IP 를 진단
#   bash trs.sh -targets 10.1.2.3        # 대상을 직접 지정 (쉼표·공백 구분, ip:포트 가능)
#   bash trs.sh -path /redfish/v1/Systems/1/Bios/Settings   # 추가로 GET 해 볼 경로 (쉼표 구분)
#   bash trs.sh -conf my.conf -user list.txt
#
# 하는 일: 스크립트 위치로 이동 → OS 에 맞는 바이너리 선택 → 필요한 파일 확인 → bin/biostool trs 실행.
#   TCP/TLS → 비인증 ServiceRoot → 인증(Basic 1회 + 세션 1회) → 인증 후 읽기(Systems/Bios/Managers/계정 상태)
#   → 403 경로의 헤더·인증 변형 비교 → 원인 추정을 출력하고 results/<일시>_trs/ 에 호스트별로 저장합니다.
# 설정 변경·재부팅은 하지 않습니다. 비밀번호·토큰은 출력·저장하지 않습니다.
set -euo pipefail
caller_pwd="$PWD"
cd "$(dirname "$0")"
. ./lib.sh

usage() {
  cat <<'EOF2'
사용법:
  bash trs.sh [biostool trs 옵션...]

  -targets <대상>   진단할 BMC (IP/hostname, 쉼표·공백 구분, ip:포트 가능). 지정하면 user.txt 를 읽지 않음
  -user <파일>      대상 목록 (기본 user.txt, 한 줄에 하나 — 관리망 IP)
  -path <경로>      추가로 GET 해 볼 Redfish 경로 (쉼표 구분)
  -conf <파일>, -hosts <파일>, -rf-conf <파일>

처음 한 번 준비할 것 (bios.sh 와 같음):
  cp bios.conf.example bios.conf      # user= 에 BMC 계정 ID 입력
  bash encrypt.sh                     # BMC 비밀번호 입력 -> pass.enc + key.bin 생성
  cp user.txt.example  user.txt       # 진단할 BMC 관리망 IP 를 한 줄에 하나씩

계정 잠금 방지: 인증 시도는 호스트당 Basic 1회 + 세션 생성 1회(+ 2순위 계정 1회)이고, 401 이면 바로 중단합니다.
결과: 화면 출력 + results/<일시>_trs/<호스트>.txt  (이 내용을 그대로 붙여 주시면 원인 분석·도구 수정에 쓸 수 있습니다)
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
  echo "[없음] $userfile  -> cp user.txt.example $userfile  후 진단할 BMC 관리망 IP 를 한 줄에 하나씩 입력 (또는 -targets <IP>)" >&2
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

exec "$BIN" trs ${args[@]+"${args[@]}"}
