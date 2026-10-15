#!/usr/bin/env bash
# bios.sh — BIOS 점검·전체비교 통합 실행 래퍼 (진입점 하나).
#
# 사용법:
#   bash bios.sh                                  # (터미널) TUI: 대상 입력 → 프로파일 선택 → 점검 → 전체비교 → 수정
#   bash bios.sh -profile VM -user user.txt       # TUI 에서 프로파일·대상을 미리 지정
#   bash bios.sh -plain -profile VM               # TUI 없이 색 없는 평문 출력 (설정 질문 없음)
#   bash bios.sh -plain -profile VM -ref <비교서버> # 평문 + 비교서버와 BIOS 전체 값 비교
#   bash bios.sh -auto-check <user> targets.txt   # 비대화형 점검 → 호출한 폴더에 check.res_<user>
#   bash bios.sh -auto       <user> targets.txt   # 위와 같음 + check.res_<user>_postapply (auto_setup 용)
#
# 하는 일: 스크립트 위치로 이동 → OS 에 맞는 바이너리 선택(없으면 빌드 시도) → 필요한 파일 확인 →
#          bin/biostool bios 실행. 재부팅은 하지 않으며, 설정(TUI 에서만)은 BMC 의 Pending 까지만 합니다.
set -euo pipefail
caller_pwd="$PWD" # 스크립트 위치로 이동하기 전 호출한 쪽 작업 폴더 (-auto 의 targets 경로·check.res_<user> 위치 기준)
cd "$(dirname "$0")"
. ./lib.sh

usage() {
  cat <<'EOF'
사용법:
  bash bios.sh [--profile <이름>] [biostool bios 옵션...]

  인자 없이 터미널에서 실행         TUI (대상·프로파일을 화면에서 입력, 끝에서 설정 여부를 묻는다)
  -profile <이름>, --profile <이름>  profiles/<이름>.tsv 를 바로 사용
  -user <파일>                      점검 대상 목록 (기본 user.txt)
  -plain                           TUI 없이 색 없는 평문으로 출력 (설정 질문 없음)
  -ref <hostname>                  전체비교 비교서버 (쉼표로 여러 개)
  -ref-json <파일>                  전체비교 기준 속성 JSON (파일 이름만 주면 bios.conf refs_dir 에서 찾음)
  -diff <파일>                      전체비교 기준 목록 diff.txt
  -ignore <파일>                    전체비교에서 제외할 속성 목록 (기본 ignore_attrs.txt)
  -from-dump <디렉터리>              접속 대신 xml.sh 로 저장한 덤프를 읽음
  -conf <파일>, -hosts <파일>         설정 파일 / 이름 해석용 hosts

비대화형 (auto_setup 이 호출):
  bash bios.sh -auto-check <user> <targets.txt> [-profile <이름>]
  bash bios.sh -auto       <user> <targets.txt> [-profile <이름>]
    - 묻지 않고 점검만 합니다 (설정 없음). 프로파일은 -profile 또는 bios.conf 의 auto_profile.
    - targets.txt 는 호출한 폴더 기준 경로이고, check.res_<user> (-auto 는 _postapply 도) 는 호출한 폴더에 만들어집니다.
    - 점검을 끝냈으면 FAIL 이 있어도 종료코드 0. conf 오류·대상 없음·프로파일 없음 같은 치명적 오류만 0 이 아닙니다.

옛 이름: bios_check.sh (점검), all_bios_check.sh (전체비교) 는 그대로 쓸 수 있습니다 (옛 옵션은 옛 방식으로 동작).

처음 한 번 준비할 것:
  cp bios.conf.example bios.conf      # user= 에 BMC 계정 ID 입력
  cp user.txt.example  user.txt       # 점검할 대상 (hostname / hostname-m / IP) 한 줄씩 (TUI 에서는 화면에서 입력해도 됨)
  bash encrypt.sh                     # BMC 비밀번호 입력 -> pass.enc + key.bin 생성
EOF
}

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
      args+=(-profile "$2")
      shift 2
      ;;
    --profile=*)
      args+=(-profile "${1#--profile=}")
      shift
      ;;
    *)
      args+=("$1")
      shift
      ;;
  esac
done

# 비대화형 -auto / -auto-check <user> <targets>: targets 는 호출한 폴더 기준 절대경로로 바꾸고,
# check.res_<user> 가 호출한 폴더에 만들어지도록 -res-dir 로 넘긴다 (이 스크립트는 위에서 cd 했으므로).
auto=0
n=${#args[@]}
for ((i = 0; i < n; i++)); do
  case "${args[$i]}" in
    -auto | --auto | -auto-check | --auto-check)
      auto=1
      if [ $((i + 2)) -ge "$n" ] || [ "${args[$((i + 1))]#-}" != "${args[$((i + 1))]}" ] || [ "${args[$((i + 2))]#-}" != "${args[$((i + 2))]}" ]; then
        echo "오류: ${args[$i]} 는 바로 뒤에 <user> <targets.txt> 가 필요합니다.  예) bash bios.sh ${args[$i]} alice targets.txt" >&2
        exit 1
      fi
      case "${args[$((i + 2))]}" in
        /*) ;;
        *) args[$((i + 2))]="$caller_pwd/${args[$((i + 2))]}" ;;
      esac
      autotargets="${args[$((i + 2))]}"
      break
      ;;
  esac
done
if [ "$auto" = "1" ]; then
  args=(-res-dir "$caller_pwd" ${args[@]+"${args[@]}"})
fi

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
has_flag() { # <옵션이름> : args 에 -name / --name 이 있는지
  local a
  for a in ${args[@]+"${args[@]}"}; do
    case "$a" in "-$1" | "--$1" | "-$1="* | "--$1="*) return 0 ;; esac
  done
  return 1
}

conf="$(opt_val conf bios.conf)"
userfile="$(opt_val user user.txt)"
fromdump="$(opt_val from-dump "")"
passfile="$(conf_val "$conf" pass_file pass.enc)"
keyfile="$(conf_val "$conf" key_file key.bin)"

missing=0
if [ ! -f "$conf" ]; then
  echo "[없음] $conf  -> cp bios.conf.example $conf  후 user= 에 BMC 계정 ID 입력" >&2
  missing=1
fi
if [ "$auto" = "1" ]; then
  # 비대화형은 대상이 targets.txt 라 user.txt 가 필요 없다
  if [ ! -f "$autotargets" ]; then
    echo "[없음] $autotargets  -> -auto / -auto-check 의 targets.txt (호출한 폴더 기준 경로)" >&2
    missing=1
  fi
else
  # 터미널이고 -plain 이 아니면 TUI 가 대상을 직접 물으므로 -user 를 주지 않은 user.txt 는 필요 없다
  if { has_flag plain || [ ! -t 1 ]; } || has_flag user; then
    if [ ! -f "$userfile" ]; then
      echo "[없음] $userfile  -> cp user.txt.example $userfile  후 점검할 대상을 한 줄에 하나씩 입력" >&2
      missing=1
    fi
  fi
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

exec "$BIN" bios ${args[@]+"${args[@]}"}
