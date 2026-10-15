#!/usr/bin/env bash
# all_bios_check.sh — 모델별 BIOS 전체 Attribute 비교 실행 래퍼 (읽기 전용, 설정은 하지 않음).
#
# 사용법:
#   bash all_bios_check.sh                          # (터미널) bios.sh 의 TUI 로 넘어감 (옛 1.체크/2.전체비교 메뉴를 대신함)
#   bash all_bios_check.sh -ref <비교서버>          # 비교서버 1대와 user.txt 의 BIOS 전체 값을 비교
#   bash all_bios_check.sh -user u.txt              # (터미널 아님/인자 있음) diff.txt(기준) 와 대상을 비교
#   bash all_bios_check.sh -diff-max 10             # 나머지 인자는 그대로 biostool allcheck 에 전달
#   bash all_bios_check.sh -from-dump dumps         # 접속 없이 xml.sh 로 저장한 덤프끼리 비교
#   bash all_bios_check.sh -conf my.conf -user list.txt -diff ref.txt -ignore my_ignore.txt
#
# 하는 일: 스크립트 위치로 이동 → OS 에 맞는 바이너리 선택(없으면 빌드 시도) → 필요한 파일 확인 →
#          bin/biostool allcheck 실행. 기준·대상 모두 BMC 에서 모델을 자동 조회해 같은 모델끼리 비교합니다.
set -euo pipefail
caller_pwd="$PWD"
cd "$(dirname "$0")"
script_dir="$PWD"
. ./lib.sh

usage() {
  cat <<'EOF2'
사용법:
  bash all_bios_check.sh [biostool allcheck 옵션...]

  -ref <hostname>      비교서버 1대를 직접 지정 (diff.txt 불필요, 리포트가 '비교서버' 표기, 모두 같으면 '모두 동일')
  -diff <파일>         기준(정상 설정값) 호스트 목록 (기본 diff.txt)
  -user <파일>         조사 대상 목록 (기본 user.txt)
  -ignore <파일>       비교에서 제외할 속성 목록 (기본 ignore_attrs.txt, 없으면 내장 기본 목록)
  -from-dump <디렉터리> 접속 대신 저장된 덤프끼리 비교 (diff.txt 와 user.txt 의 호스트 모두 덤프가 있어야 함:
                       bash xml.sh <기준 호스트> <대상 호스트> ...)
  -retry-from <폴더>   이전 결과 폴더에서 재시도 대상(retry.txt)을 읽어 재비교 (-user 와 함께 쓸 수 없음)
  -diff-max <N>        호스트당 터미널에 보일 차이 속성 수 (기본 30, 넘으면 all_diff.tsv 안내)
  -list-max <N>        차이 상세·특이사항·오류에 나열할 호스트 수 (기본 20)
  -conf <파일>, -hosts <파일>

새 진입점은 `bash bios.sh` 입니다. 인자 없이 터미널에서 실행하면 bios.sh 의 TUI 로 넘어갑니다
  (-conf/-hosts/-ignore 만 함께 줘도 같음. -diff-max, -list-max, -export-attrs 를 주면 옛 대화형 메뉴:
   대상 입력 → 1. 체크(bios_check.sh) / 2. 전체비교 → 비교서버 입력).
-user, -diff, -ref, -from-dump, -retry-from 등을 주면 옛 방식(biostool allcheck, 비대화형 리포트)으로 동작합니다.

처음 한 번 준비할 것:
  cp bios.conf.example bios.conf      # user= 에 BMC 계정 ID 입력
  bash encrypt.sh                     # BMC 비밀번호 입력 -> pass.enc + key.bin 생성
  cp diff.txt.example  diff.txt       # 정상 설정값을 가진 대표(기준) hostname 한 줄씩 (모델은 적지 않아도 됨)
  cp user.txt.example  user.txt       # 조사할 대상 hostname 한 줄씩

결과: results/<일시>_all/ (report.txt 화면 리포트 전체, all_diff.tsv 차이 전체, summary.tsv 호스트별 요약, retry.txt, run_info.txt)
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

# ---- 대화형 모드: 터미널에서 대상(-user)·기준(-diff/-ref)·덤프·재시도 인자 없이 실행했을 때만 ----
# (있으면 아래 기존 동작 그대로. -conf/-hosts/-diff-max 같은 옵션은 함께 써도 됨)
# 대상 입력 → 1. 체크(bios_check.sh) / 2. 전체비교(비교서버 1대와 BIOS 전체 값 비교)
tmp_user=""
cleanup_tmp() { [ -n "$tmp_user" ] && rm -f "$tmp_user"; return 0; }
trap cleanup_tmp EXIT
if [ -t 0 ] && [ -z "$(opt_val user "")$(opt_val diff "")$(opt_val ref "")$(opt_val from-dump "")$(opt_val retry-from "")" ]; then
  # 새 진입점 bios.sh 의 TUI 가 이 1.체크/2.전체비교 메뉴를 대신한다 (allcheck 전용 옵션을 줬거나 출력이 터미널이 아니면 아래 옛 메뉴).
  if [ -t 1 ] && ! printf '%s\n' ${args[@]+"${args[@]}"} | grep -qE '^--?(diff-max|list-max|export-attrs)(=|$)'; then
    cd "$caller_pwd"
    exec bash "$script_dir/bios.sh" ${args[@]+"${args[@]}"}
  fi
  printf '대상(user.txt 파일 경로 또는 hostname, 쉼표·공백 구분) [user.txt] : '
  read -r in_user || in_user=""
  in_user="${in_user:-user.txt}"
  if [ -f "$in_user" ]; then
    iuser="$in_user"
  else
    tmp_user="$(mktemp)"
    chmod 600 "$tmp_user"
    printf '%s\n' "$in_user" | tr ', ' '\n\n' | sed '/^$/d' >"$tmp_user"
    if [ ! -s "$tmp_user" ]; then
      echo "오류: 대상이 비어 있습니다." >&2
      exit 1
    fi
    iuser="$tmp_user"
  fi
  echo
  echo "1. 체크      (프로파일 표준값과 비교, 다르면 설정 여부를 묻는다)"
  echo "2. 전체비교  (비교서버 1대의 BIOS 전체 값과 대상을 비교, 다른 값만 리포트)"
  printf '선택 [1/2] : '
  read -r choice || choice=""
  case "$choice" in
    1)
      chk_args=(-user "$iuser")
      [ -n "$(opt_val conf "")" ] && chk_args+=(-conf "$(opt_val conf "")")
      [ -n "$(opt_val hosts "")" ] && chk_args+=(-hosts "$(opt_val hosts "")")
      bash ./bios_check.sh "${chk_args[@]}"
      exit $?
      ;;
    2)
      printf '비교서버(hostname 1대) : '
      read -r in_ref || in_ref=""
      in_ref="$(printf '%s' "$in_ref" | tr -d '[:space:]')"
      if [ -z "$in_ref" ]; then
        echo "오류: 비교서버가 비어 있습니다." >&2
        exit 1
      fi
      args+=(-ref "$in_ref" -user "$iuser")
      ;;
    *)
      echo "오류: 1 또는 2 를 입력하십시오." >&2
      exit 1
      ;;
  esac
fi

conf="$(opt_val conf bios.conf)"
userfile="$(opt_val user user.txt)"
difffile="$(opt_val diff diff.txt)"
reftarget="$(opt_val ref "")"
fromdump="$(opt_val from-dump "")"
retryfrom="$(opt_val retry-from "")"
passfile="$(conf_val "$conf" pass_file pass.enc)"
keyfile="$(conf_val "$conf" key_file key.bin)"

missing=0
if [ ! -f "$conf" ]; then
  echo "[없음] $conf  -> cp bios.conf.example $conf  후 user= 에 BMC 계정 ID 입력" >&2
  missing=1
fi
if [ -z "$reftarget" ] && [ ! -f "$difffile" ]; then   # -ref 로 비교서버를 직접 줬으면 diff.txt 가 필요 없다
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

"$BIN" allcheck ${args[@]+"${args[@]}"}
