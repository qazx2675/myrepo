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
#   bash bios.sh <명령> [옵션...]                 # 기능별 명령 (account, passwd, trs, dump, export, ucsm, check, allcheck, encrypt, update)
#
# 하는 일: 스크립트 위치로 이동 → OS 에 맞는 바이너리 선택(없으면 빌드 시도) → 필요한 파일 확인 →
#          bin/biostool bios 실행. 재부팅은 하지 않으며, 설정(TUI 에서만)은 BMC 의 Pending 까지만 합니다.
#          점검 끝에 1순위 계정이 없어 2순위(rf.conf)로 점검한 호스트가 있으면 계정 생성을 묻습니다 (기본 Y).
set -euo pipefail
caller_pwd="$PWD" # 스크립트 위치로 이동하기 전 호출한 쪽 작업 폴더 (-auto 의 targets 경로·check.res_<user> 위치 기준)
script_dir="$(cd "$(dirname "$0")" && pwd)"

# 기능별 명령: 첫 인자가 명령 이름이면 그 기능의 래퍼를 그대로 실행한다 (나머지 인자는 그대로 전달).
# 래퍼가 각자 호출한 폴더 기준으로 경로를 해석하도록 이 스크립트 폴더로 이동하기 전에 넘긴다.
case "${1:-}" in
  account) shift; exec bash "$script_dir/account_create.sh" "$@" ;;
  trs) shift; exec bash "$script_dir/trs.sh" "$@" ;;
  trs-cisco | trscisco) shift; exec bash "$script_dir/trs_cisco.sh" "$@" ;;
  dump) shift; exec bash "$script_dir/xml.sh" "$@" ;;
  export) shift; exec bash "$script_dir/bios_export.sh" "$@" ;;
  ucsm) shift; exec bash "$script_dir/ucsm_check.sh" "$@" ;;
  check) shift; exec bash "$script_dir/bios_check.sh" "$@" ;;
  allcheck) shift; exec bash "$script_dir/all_bios_check.sh" "$@" ;;
  encrypt) shift; exec bash "$script_dir/encrypt.sh" "$@" ;;
  update) shift; exec bash "$script_dir/update.sh" "$@" ;;
esac

cd "$script_dir"
. ./lib.sh

# passwd: 래퍼 없이 바이너리를 바로 쓴다 (bios.conf·user.txt 는 이 폴더 기준, account_create.sh 와 같음).
if [ "${1:-}" = "passwd" ]; then
  shift
  select_bin || exit 2
  echo "※ 이 도구는 참고용(보조 도구)입니다. 비밀번호를 바꾼 뒤에는 랜덤한 서버 몇 대를 직접 골라 실제로 로그인되는지 확인하십시오."
  exec "$BIN" account change "$@"
fi

usage() {
  cat <<'EOF'
사용법:
  bash bios.sh [--profile <이름>] [biostool bios 옵션...]

  인자 없이 터미널에서 실행         TUI (대상·프로파일을 화면에서 입력, 끝에서 설정 여부를 묻는다)
  -profile <이름>, --profile <이름>  profiles/<이름>.tsv 를 바로 사용
  -user <파일>                      점검 대상 목록 (기본 user.txt)
  -user-xml <파일>                  Cisco(XML API, 읽기 전용) 대상 목록 — 점검만 (전체비교·설정 제외)
                                   Cisco 장비는 XML 선택: hostname 이 c 로 시작하면 자동, IP 는 -user-xml 파일에
                                   (접두어는 bios.conf xml_host_prefix, -user-xml 만 주면 user.txt 는 읽지 않음)
  -plain                           TUI 없이 색 없는 평문으로 출력 (설정 질문 없음)
  -ref <hostname>                  전체비교 비교서버 (쉼표·공백으로 여러 개)
  -ref-json <파일>                  전체비교 기준 속성 JSON (파일 이름만 주면 bios.conf refs_dir 에서 찾음)
  -diff <파일>                      전체비교 기준 목록 diff.txt
  -ignore <파일>                    전체비교에서 제외할 속성 목록 (기본 ignore_attrs.txt)
  -from-dump <디렉터리>              접속 대신 xml.sh 로 저장한 덤프를 읽음
  -conf <파일>, -hosts <파일>         설정 파일 / 이름 해석용 hosts
  (파일·폴더 옵션의 상대경로는 호출한 폴더에 있으면 그 기준, 없으면 이 스크립트 폴더 기준)

비대화형 (auto_setup 이 호출):
  bash bios.sh -auto-check <user> <targets.txt> [-profile <이름>]
  bash bios.sh -auto       <user> <targets.txt> [-profile <이름>] [-user-xml <파일>]
    - 묻지 않고 점검만 합니다 (설정 없음). 프로파일은 -profile 또는 bios.conf 의 auto_profile.
    - targets.txt 와 -user-xml 파일은 호출한 폴더 기준 경로이고, check.res_<user> (-auto 는 _postapply 도) 는 호출한 폴더에 만들어집니다.
    - 점검을 끝냈으면 FAIL 이 있어도 종료코드 0. conf 오류·대상 없음·프로파일 없음 같은 치명적 오류만 0 이 아닙니다.

점검 끝 계정 생성 질문 (TUI·-plain):
  1순위 계정(bios.conf user)이 거부되어 2순위(/etc/rf_tool/rf.conf) 계정으로 점검한 호스트가 있으면
  "계정을 만들까요? [Y/n]" 을 묻습니다 (빈 입력 = Y, 터미널 입력일 때만). bios.conf 에 user2 가 있으면
  같은 호스트에 두 번째 계정(user2_role 권한)도 함께 만듭니다. -auto / -auto-check 는 묻지 않습니다.

기능별 명령 (bash bios.sh <명령> [옵션...], 옵션은 각 명령의 -h 참고):
  account   [-dry-run] [-ucsm] ...     1순위(+ user2) 계정 생성        = bash account_create.sh
  passwd    [-dry-run] [-ucsm] ...     1순위 계정 비밀번호 변경         = bin/biostool account change
  trs       [-targets IP,...] ...      BMC 접속 장애(403 등) 진단        = bash trs.sh
  trs-cisco [-targets IP,...] ...      Cisco UNSUPPORTED·XML 실패 원인 진단 = bash trs_cisco.sh
  dump      [대상...] [--compact]      사전조사 덤프 (GET 전용)          = bash xml.sh
  export    <비교서버> [대상...]       BIOS 설정값 JSON 추출·비교        = bash bios_export.sh
  ucsm      [-user ucsm.txt] ...       Cisco UCSM(XML API) BIOS 점검     = bash ucsm_check.sh
  check     -retry-from <폴더> | -dry-run ...  옛 점검 (재시도·dry-run)  = bash bios_check.sh
  allcheck  [-ref 서버 | -diff diff.txt] ...   옛 전체비교              = bash all_bios_check.sh
  encrypt   [2|rf [벤더]]              비밀번호 암호화 (2: 두 번째 계정 → pass2.enc, rf: 2순위 벤더 계정 → rf.enc) = bash encrypt.sh
  update    [ZIP 경로]                 새 버전으로 설치 폴더 갱신         = bash update.sh

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

# 파일·폴더 옵션의 상대경로는 호출한 폴더에 있으면 그 기준으로 (lib.sh fix_path_args)
fix_path_args "$caller_pwd"

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
  # -user-xml 도 targets.txt 처럼 호출한 폴더 기준 절대경로로 바꾼다 (-user-xml X / --user-xml X / -user-xml=X)
  for ((i = 0; i < n; i++)); do
    case "${args[$i]}" in
      -user-xml | --user-xml)
        if [ $((i + 1)) -lt "$n" ]; then
          case "${args[$((i + 1))]}" in
            /*) ;;
            *) args[$((i + 1))]="$caller_pwd/${args[$((i + 1))]}" ;;
          esac
        fi
        ;;
      -user-xml=* | --user-xml=*)
        v="${args[$i]#*=}"
        case "$v" in
          /*) ;;
          *) args[$i]="${args[$i]%%=*}=$caller_pwd/$v" ;;
        esac
        ;;
    esac
  done
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
userxml="$(opt_val user-xml "")"
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
  # 터미널이고 -plain 이 아니면 TUI 가 대상을 직접 물으므로 -user 를 주지 않은 user.txt 는 필요 없다.
  # -user-xml 만 주면 biostool 이 user.txt 를 읽지 않으므로 그때도 필요 없다.
  if { { has_flag plain || [ ! -t 1 ]; } && ! has_flag user-xml; } || has_flag user; then
    if [ ! -f "$userfile" ]; then
      echo "[없음] $userfile  -> cp user.txt.example $userfile  후 점검할 대상을 한 줄에 하나씩 입력" >&2
      missing=1
    fi
  fi
fi
if [ -n "$userxml" ] && [ ! -f "$userxml" ]; then
  echo "[없음] $userxml  -> -user-xml 의 Cisco(XML) 대상 목록 (한 줄에 하나)" >&2
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

exec "$BIN" bios ${args[@]+"${args[@]}"}
