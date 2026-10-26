#!/usr/bin/env bash
# account_create.sh — 1순위 계정 생성(biostool account create) 실행 래퍼.
#
# 사용법:
#   bash account_create.sh                 # 점검 → 대상 목록 → Y/n(기본 Y) → 생성·검증·롤백
#   bash account_create.sh -dry-run        # 보낼 계획만 출력 (BMC 에 쓰지 않음)
#   bash account_create.sh -ucsm           # 대상이 Cisco UCSM(XML API) 일 때
#   bash account_create.sh -user list.txt -j 50
#
# 하는 일: 스크립트 위치로 이동 → OS 에 맞는 바이너리 선택 → 필요한 파일 확인 →
#          bin/biostool account create 실행 (나머지 인자는 그대로 전달).
# 1순위 계정(bios.conf user + pass.enc)이 BMC 에 없으면 rf.conf 의 벤더 admin(2순위) 계정으로
# 로그인해 같은 id/pw 의 관리자 계정을 만듭니다. 재부팅·BIOS 변경은 하지 않습니다.
# 비밀번호 변경(change)은 바이너리를 직접 쓰십시오: bin/biostool account change (사용법.txt 참고).
set -euo pipefail
cd "$(dirname "$0")"
. ./lib.sh

usage() {
  cat <<'EOF'
사용법:
  bash account_create.sh [biostool account create 옵션...]

  -dry-run         전송 없이 만들 계획만 출력
  -no-prompt       묻지 않고 대상 목록만 출력
  -stdin-ok        터미널이 아닌 입력의 y/n 도 읽음 (자동화용)
  -ucsm            대상이 Cisco UCSM (XML API, 로컬 사용자)
  -j <N>           동시 처리 수 (기본 100, 최대 300)
  -list-max <N>    상태별로 나열할 호스트 수 (기본 20)
  -conf <파일>     설정 파일 (기본 bios.conf)
  -user <파일>     대상 목록 (기본 user.txt)
  -hosts <파일>    이름 해석용 hosts (기본 /etc/hosts)
  -rf-conf <파일>  2순위(admin) 계정 파일 (기본 환경변수 RF_TOOL_CONF, 없으면 /etc/rf_tool/rf.conf)

처음 한 번 준비할 것:
  cp bios.conf.example bios.conf      # user= 에 만들 1순위 계정 ID 입력
  cp user.txt.example  user.txt       # 대상 (hostname / hostname-m / IP) 한 줄씩
  bash encrypt.sh                     # 만들 계정의 비밀번호 입력 -> pass.enc + key.bin 생성
  /etc/rf_tool/rf.conf                # <vendor>_user (chmod 600) — 계정을 만들 admin 계정명
  bash encrypt.sh rf [벤더]           # 그 admin 비밀번호 입력 -> rf.enc (벤더별, 같은 key.bin)
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

# 인자로 -conf/-user/-rf-conf 를 바꿨으면 그 값을, 아니면 기본값을 점검 대상으로 삼는다.
opt_val() { # <옵션이름> <기본값>
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
passfile="$(conf_val "$conf" pass_file pass.enc)"
keyfile="$(conf_val "$conf" key_file key.bin)"
rfconf="$(opt_val rf-conf "${RF_TOOL_CONF:-/etc/rf_tool/rf.conf}")"

missing=0
if [ ! -f "$conf" ]; then
  echo "[없음] $conf  -> cp bios.conf.example $conf  후 user= 에 만들 계정 ID 입력" >&2
  missing=1
fi
if [ ! -f "$userfile" ]; then
  echo "[없음] $userfile  -> cp user.txt.example $userfile  후 대상을 한 줄에 하나씩 입력" >&2
  missing=1
fi
if [ ! -f "$passfile" ] || [ ! -f "$keyfile" ]; then
  echo "[없음] $passfile / $keyfile  -> bash encrypt.sh  (만들 계정의 비밀번호를 암호화해 생성)" >&2
  missing=1
fi
if [ ! -f "$rfconf" ]; then
  echo "[없음] $rfconf  -> 계정을 만들 벤더 admin 계정명(<vendor>_user)을 적고 chmod 600, 비밀번호는 bash encrypt.sh rf [벤더] (rf.enc)" >&2
  missing=1
fi
if [ "$missing" = "1" ]; then
  echo "위 파일을 만든 뒤 다시 실행하십시오." >&2
  exit 1
fi

echo "※ 이 도구는 참고용(보조 도구)입니다. 계정을 만든 뒤에는 랜덤한 서버 몇 대를 직접 골라 실제로 로그인되는지 확인하십시오."
exec "$BIN" account create ${args[@]+"${args[@]}"}
