#!/usr/bin/env bash
# bios_export.sh — 비교서버와 대상서버의 BIOS 설정값(JSON)을 뽑아 눈으로 비교하는 래퍼 (읽기 전용, 설정은 하지 않음).
#
# 사용법:
#   bash bios_export.sh <비교서버> <대상1> [대상2 ...]     # hostname / IP / IP:포트
#   bash bios_export.sh <비교서버>                         # 대상은 user.txt
#   bash bios_export.sh <비교서버> <대상> -conf my.conf    # 나머지 옵션은 biostool allcheck 에 그대로 전달
#
# 하는 일: 스크립트 위치로 이동 → OS 에 맞는 바이너리 선택 → 필요한 파일 확인 →
#          bin/biostool allcheck -ref <비교서버> -export-attrs 실행(GET 4번/호스트, 로그인 1회).
#          호스트마다 results/<일시>_all/attrs/<호스트>.json 이 만들어지고, 같은 폴더의 비교 리포트도 함께 나옵니다.
set -euo pipefail
cd "$(dirname "$0")"
. ./lib.sh

usage() {
  cat <<'EOF2'
사용법:
  bash bios_export.sh <비교서버> [<대상> ...] [biostool allcheck 옵션...]

  <비교서버>   기준이 되는 서버 1대 (hostname / hostname-m / IP / IP:포트)
  <대상> ...   비교할 서버들 (생략하면 user.txt)

결과: results/<일시>_all/
  attrs/<호스트>.json   호스트별 BIOS 속성 전체 (속성 이름 순서로 정렬, 비밀번호·토큰 없음)
  report.txt            차이만 모은 리포트 / all_diff.tsv 차이 전체

눈으로 비교하는 법 (실행이 끝나면 같은 명령이 안내됩니다):
  diff -u attrs/<비교서버>.json attrs/<대상>.json | less
  diff -y -W 200 --suppress-common-lines attrs/<비교서버>.json attrs/<대상>.json | less
  vimdiff attrs/<비교서버>.json attrs/<대상>.json

처음 한 번 준비할 것은 bios_check.sh 와 같습니다 (bios.conf, bash encrypt.sh).
EOF2
}

pos=()
opts=()
while [ $# -gt 0 ]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    -conf | -hosts | -ignore | -user | -diff-max | -list-max)
      if [ $# -lt 2 ]; then
        echo "오류: $1 에는 값이 필요합니다." >&2
        exit 1
      fi
      opts+=("$1" "$2")
      shift 2
      ;;
    -*)
      opts+=("$1")
      shift
      ;;
    *)
      pos+=("$1")
      shift
      ;;
  esac
done

if [ ${#pos[@]} -lt 1 ]; then
  echo "오류: 비교서버를 지정하십시오.  예) bash bios_export.sh <비교서버> <대상1> <대상2>" >&2
  usage >&2
  exit 1
fi
ref="${pos[0]}"

select_bin || exit 2

opt_val() { # <옵션이름> <기본값>
  local name="$1" def="$2" i n=${#opts[@]}
  for ((i = 0; i + 1 < n; i++)); do
    if [ "${opts[$i]}" = "-$name" ]; then
      echo "${opts[$((i + 1))]}"
      return
    fi
  done
  echo "$def"
}

conf="$(opt_val conf bios.conf)"
passfile="$(conf_val "$conf" pass_file pass.enc)"
keyfile="$(conf_val "$conf" key_file key.bin)"
resdir="$(conf_val "$conf" result_dir results)"

missing=0
if [ ! -f "$conf" ]; then
  echo "[없음] $conf  -> cp bios.conf.example $conf  후 user= 에 BMC 계정 ID 입력" >&2
  missing=1
fi
if [ ! -f "$passfile" ] || [ ! -f "$keyfile" ]; then
  echo "[없음] $passfile / $keyfile  -> bash encrypt.sh  (BMC 비밀번호를 암호화해 생성)" >&2
  missing=1
fi

# 대상: 위치 인자(비교서버 뒤) → 임시 목록 파일, 없으면 -user 또는 user.txt
tmp_user=""
cleanup_tmp() { [ -n "$tmp_user" ] && rm -f "$tmp_user"; return 0; }
trap cleanup_tmp EXIT
if [ ${#pos[@]} -gt 1 ]; then
  tmp_user="$(mktemp)"
  chmod 600 "$tmp_user"
  printf '%s\n' "${pos[@]:1}" >"$tmp_user"
  opts+=(-user "$tmp_user")
else
  userfile="$(opt_val user user.txt)"
  if [ ! -f "$userfile" ]; then
    echo "[없음] $userfile  -> 대상을 인자로 주거나(bash bios_export.sh <비교서버> <대상>) user.txt 에 한 줄씩 입력" >&2
    missing=1
  fi
fi
if [ "$missing" = "1" ]; then
  echo "위 항목을 준비한 뒤 다시 실행하십시오." >&2
  exit 1
fi

before="$(ls -d "$resdir"/*_all 2>/dev/null | tail -n 1 || true)"
"$BIN" allcheck -ref "$ref" -export-attrs ${opts[@]+"${opts[@]}"}
after="$(ls -d "$resdir"/*_all 2>/dev/null | tail -n 1 || true)"

if [ -n "$after" ] && [ "$after" != "$before" ] && [ -d "$after/attrs" ]; then
  echo
  echo "== 눈으로 비교하기 =="
  echo "속성 JSON 폴더: $after/attrs/"
  ls "$after/attrs" | sed 's/^/  /'
  echo
  echo "  cd $after/attrs"
  echo "  diff -u <비교서버>.json <대상>.json | less"
  echo "  diff -y -W 200 --suppress-common-lines <비교서버>.json <대상>.json | less"
  echo "  vimdiff <비교서버>.json <대상>.json"
  echo "  (비교서버 파일 이름은 호스트 이름의 ':' 등을 '_' 로 바꾼 것입니다)"
fi
