#!/usr/bin/env bash
# all_bios_check.sh — 모델별 BIOS 전체 Attribute 비교 실행 래퍼 (읽기 전용, 설정은 하지 않음).
#
# 사용법:
#   bash all_bios_check.sh                          # diff.txt(기준) 와 user.txt(대상) 를 비교
#   bash all_bios_check.sh -diff-max 10             # 나머지 인자는 그대로 biostool allcheck 에 전달
#   bash all_bios_check.sh -from-dump dumps         # 접속 없이 xml.sh 로 저장한 덤프끼리 비교
#   bash all_bios_check.sh -conf my.conf -user list.txt -diff ref.txt -ignore my_ignore.txt
#
# 하는 일: 스크립트 위치로 이동 → OS 에 맞는 바이너리 선택(없으면 빌드 시도) → 필요한 파일 확인 →
#          bin/biostool allcheck 실행. 기준·대상 모두 BMC 에서 모델을 자동 조회해 같은 모델끼리 비교합니다.
set -euo pipefail
cd "$(dirname "$0")"
. ./lib.sh

usage() {
  cat <<'EOF2'
사용법:
  bash all_bios_check.sh [biostool allcheck 옵션...]

  -diff <파일>         기준(정상 설정값) 호스트 목록 (기본 diff.txt)
  -user <파일>         조사 대상 목록 (기본 user.txt)
  -ignore <파일>       비교에서 제외할 속성 목록 (기본 ignore_attrs.txt, 없으면 내장 기본 목록)
  -from-dump <디렉터리> 접속 대신 저장된 덤프끼리 비교 (diff.txt 와 user.txt 의 호스트 모두 덤프가 있어야 함:
                       bash xml.sh <기준 호스트> <대상 호스트> ...)
  -retry-from <폴더>   이전 결과 폴더에서 재시도 대상(retry.txt)을 읽어 재비교 (-user 와 함께 쓸 수 없음)
  -diff-max <N>        호스트당 터미널에 보일 차이 속성 수 (기본 30, 넘으면 all_diff.tsv 안내)
  -list-max <N>        차이 상세·특이사항·오류에 나열할 호스트 수 (기본 20)
  -conf <파일>, -hosts <파일>

처음 한 번 준비할 것:
  cp bios.conf.example bios.conf      # user= 에 BMC 계정 ID 입력
  bash encrypt.sh                     # BMC 비밀번호 입력 -> pass.enc + key.bin 생성
  cp diff.txt.example  diff.txt       # 정상 설정값을 가진 대표(기준) hostname 한 줄씩 (모델은 적지 않아도 됨)
  cp user.txt.example  user.txt       # 조사할 대상 hostname 한 줄씩

결과: results/<일시>_all/ (all_diff.tsv 차이 전체, summary.tsv 호스트별 요약, retry.txt, run_info.txt)
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

select_bin || exit 2

# 인자로 -conf/-user/-diff 를 바꿨으면 그 값을, 아니면 기본 파일명을 점검 대상으로 삼는다.
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
difffile="$(opt_val diff diff.txt)"
fromdump="$(opt_val from-dump "")"
retryfrom="$(opt_val retry-from "")"
passfile="$(conf_val "$conf" pass_file pass.enc)"
keyfile="$(conf_val "$conf" key_file key.bin)"

missing=0
if [ ! -f "$conf" ]; then
  echo "[없음] $conf  -> cp bios.conf.example $conf  후 user= 에 BMC 계정 ID 입력" >&2
  missing=1
fi
if [ ! -f "$difffile" ]; then
  echo "[없음] $difffile  -> cp diff.txt.example $difffile  후 정상 설정값을 가진 대표(기준) hostname 을 한 줄에 하나씩 입력 (모델은 BMC 에서 자동 조회)" >&2
  missing=1
fi
if [ -z "$retryfrom" ] && [ ! -f "$userfile" ]; then   # -retry-from 이면 대상은 이전 결과의 retry.txt 라 user.txt 가 필요 없다
  echo "[없음] $userfile  -> cp user.txt.example $userfile  후 조사할 대상 hostname 을 한 줄에 하나씩 입력" >&2
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

exec "$BIN" allcheck ${args[@]+"${args[@]}"}
