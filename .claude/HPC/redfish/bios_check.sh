#!/usr/bin/env bash
# bios_check.sh — BIOS 표준값 점검 실행 래퍼.
#
# 사용법:
#   bash bios_check.sh                         # profiles/*.tsv 를 번호 메뉴로 선택
#   bash bios_check.sh --profile VM            # 메뉴 생략
#   bash bios_check.sh --profile VM -no-prompt # 나머지 인자는 그대로 biostool check 에 전달
#   bash bios_check.sh -conf my.conf -user list.txt
#
# 하는 일: 스크립트 위치로 이동 → OS 에 맞는 바이너리 선택(없으면 빌드 시도) →
#          필요한 파일 확인 → 프로파일 선택 → bin/biostool check -profile <선택> 실행.
# 재부팅은 하지 않으며, 설정은 BMC 의 Pending 까지만 합니다.
set -euo pipefail
cd "$(dirname "$0")"
. ./lib.sh

usage() {
  cat <<'EOF'
사용법:
  bash bios_check.sh [--profile <이름>] [biostool check 옵션...]

  --profile <이름>   profiles/<이름>.tsv 를 바로 사용 (생략하면 번호 메뉴)
  그 외 인자           그대로 biostool check 에 전달 (-from-dump <디렉터리>, -retry-from <폴더>, -dry-run,
                      -no-prompt, -stdin-ok, -list-max <N>, -fail-max <N>, -conf <파일>, -user <파일>, -hosts <파일>)

설정(Y/N) 입력:
  Y 는 터미널에서 직접 입력할 때만 받습니다. 표준입력이 파이프·파일이면(`yes |`, `< 답.txt` 등) 묻지 않고 N 으로 처리합니다.
  -stdin-ok          자동화용 예외: 비대화형 입력의 Y 로도 실제 설정합니다 (실행 시 경고 한 줄 출력). 필요할 때만 쓰십시오.

처음 한 번 준비할 것:
  cp bios.conf.example bios.conf      # user= 에 BMC 계정 ID 입력
  cp user.txt.example  user.txt       # 점검할 대상 (hostname / hostname-m / IP) 한 줄씩
  bash encrypt.sh                     # BMC 비밀번호 입력 -> pass.enc + key.bin 생성

재시도 (단계 7):
  bash bios_check.sh --profile VM -retry-from results/<일시>/        # 이전 retry.txt 에서 읽기
  bash bios_check.sh --profile VM -retry-from results/<일시>/retry.txt # 파일 경로도 가능
EOF
}

profile=""
args=()
while [ $# -gt 0 ]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --profile)
      if [ $# -lt 2 ]; then
        echo "오류: --profile 에는 프로파일 이름이 필요합니다." >&2
        exit 1
      fi
      profile="$2"
      shift 2
      ;;
    --profile=*)
      profile="${1#--profile=}"
      shift
      ;;
    *)
      args+=("$1")
      shift
      ;;
  esac
done

select_bin || exit 2

# 인자로 -conf/-user 를 바꿨으면 그 값을, 아니면 기본 파일명을 점검 대상으로 삼는다.
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
fromdump="$(opt_val from-dump "")"
retryfrom="$(opt_val retry-from "")"
passfile="$(conf_val "$conf" pass_file pass.enc)"
keyfile="$(conf_val "$conf" key_file key.bin)"
pdir="$(conf_val "$conf" profile_dir profiles)"

missing=0
if [ ! -f "$conf" ]; then
  echo "[없음] $conf  -> cp bios.conf.example $conf  후 user= 에 BMC 계정 ID 입력" >&2
  missing=1
fi
if [ -z "$retryfrom" ] && [ ! -f "$userfile" ]; then   # -retry-from 이면 대상은 이전 결과의 retry.txt 라 user.txt 가 필요 없다
  echo "[없음] $userfile  -> cp user.txt.example $userfile  후 점검할 대상을 한 줄에 하나씩 입력" >&2
  missing=1
fi
if [ -z "$fromdump" ]; then
  if [ ! -f "$passfile" ] || [ ! -f "$keyfile" ]; then
    echo "[없음] $passfile / $keyfile  -> bash encrypt.sh  (BMC 비밀번호를 암호화해 생성)" >&2
    missing=1
  fi
fi
if [ "$missing" = "1" ]; then
  echo "위 파일을 만든 뒤 다시 실행하십시오." >&2
  exit 1
fi

if [ -z "$profile" ]; then
  shopt -s nullglob
  files=("$pdir"/*.tsv)
  shopt -u nullglob
  if [ ${#files[@]} -eq 0 ]; then
    echo "오류: 프로파일이 없습니다 ($pdir/*.tsv)." >&2
    exit 1
  fi
  echo "프로파일을 선택하십시오:"
  i=1
  for f in "${files[@]}"; do
    n="$(basename "$f" .tsv)"
    echo "  $i) $n"
    i=$((i + 1))
  done
  tries=0
  while :; do
    printf '번호 (1-%d): ' "${#files[@]}"
    if ! read -r sel; then
      echo >&2
      echo "오류: 입력이 없어 종료합니다." >&2
      exit 1
    fi
    case "$sel" in
      '' | *[!0-9]*) ;;
      *)
        if [ "$sel" -ge 1 ] && [ "$sel" -le "${#files[@]}" ]; then
          profile="$(basename "${files[$((sel - 1))]}" .tsv)"
          break
        fi
        ;;
    esac
    tries=$((tries + 1))
    if [ "$tries" -ge 3 ]; then
      echo "오류: 올바른 번호가 아닙니다." >&2
      exit 1
    fi
    echo "올바른 번호가 아닙니다." >&2
  done
else
  case "$profile" in
    */* | *\\*)
      echo "오류: 프로파일 이름에는 경로를 쓸 수 없습니다: $profile" >&2
      exit 1
      ;;
  esac
  if [ ! -f "$pdir/$profile.tsv" ]; then
    echo "오류: 프로파일 $pdir/$profile.tsv 이(가) 없습니다. 사용 가능:" >&2
    for f in "$pdir"/*.tsv; do
      [ -e "$f" ] && echo "  $(basename "$f" .tsv)" >&2
    done
    exit 1
  fi
fi

echo "프로파일: $profile"
exec "$BIN" check -profile "$profile" ${args[@]+"${args[@]}"}
