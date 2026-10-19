#!/usr/bin/env bash
# lib.sh — bios_check.sh / xml.sh / encrypt.sh 가 source 하는 공용 함수.
# 직접 실행하지 않습니다. 호출하는 쪽이 먼저 스크립트 디렉터리로 cd 해야 합니다.
# (RHEL6 의 bash 4.1 에서도 동작하도록 4.4 이상 전용 문법은 쓰지 않습니다.)

# select_bin: 실행할 바이너리를 BIN 에 설정한다.
#   커널 메이저 버전 < 3 (RHEL6) → bin/biostool_os6, 그 외 → bin/biostool
#   바이너리가 없으면 go 가 있을 때 빌드 스크립트로 빌드를 시도하고, 없으면 안내 후 실패(return 1).
select_bin() {
  local kmajor build
  kmajor="$(uname -r | cut -d. -f1)"
  case "$kmajor" in
    '' | *[!0-9]*) kmajor=99 ;;
  esac
  if [ "$kmajor" -lt 3 ]; then
    BIN="bin/biostool_os6"
    build="build_os6.sh"
  else
    BIN="bin/biostool"
    build="build.sh"
  fi

  if [ -f "$BIN" ] && [ ! -x "$BIN" ]; then
    chmod +x "$BIN" 2>/dev/null || true
  fi
  if [ ! -x "$BIN" ]; then
    if command -v go >/dev/null 2>&1; then
      echo "바이너리 $BIN 이(가) 없어 go 로 빌드합니다 (bash $build) ..." >&2
      bash "./$build" >&2 || return 1
    else
      echo "오류: 바이너리 $BIN 이(가) 없고 go 도 없습니다." >&2
      echo "      저장소의 bin/ 디렉터리째 내려받았는지 확인하십시오 (빌드 완료 바이너리가 커밋돼 있습니다)." >&2
      return 1
    fi
  fi
  [ -x "$BIN" ]
}

# fix_path_args <호출한 폴더>: 전역 배열 args 의 파일·폴더 옵션(-conf -user -user-xml -hosts -ref-json -diff -ignore
#   -from-dump -retry-from -rf-conf, `-옵션 값` / `-옵션=값`) 상대경로를, 호출한 폴더에 그 파일·폴더가 있으면 호출한 폴더 기준
#   절대경로로 바꾼다 (래퍼는 스크립트 폴더로 cd 하므로, 다른 폴더에서 `bash /경로/bios.sh -user list.txt` 로 불러도 찾게).
#   호출한 폴더에 없으면 그대로 둔다 (예전처럼 스크립트 폴더 기준, -ref-json 은 파일 이름만 주면 refs_dir 에서 찾음).
fix_path_args() {
  local cpwd="$1" i n v
  [ "$cpwd" = "$PWD" ] && return 0
  n=${#args[@]}
  for ((i = 0; i < n; i++)); do
    case "${args[$i]}" in
      -conf | --conf | -user | --user | -user-xml | --user-xml | -hosts | --hosts | -ref-json | --ref-json | \
        -diff | --diff | -ignore | --ignore | -from-dump | --from-dump | -retry-from | --retry-from | -rf-conf | --rf-conf)
        if [ $((i + 1)) -lt "$n" ]; then
          v="${args[$((i + 1))]}"
          case "$v" in
            "" | /* | -*) ;;
            *) if [ -e "$cpwd/$v" ]; then args[$((i + 1))]="$cpwd/$v"; fi ;;
          esac
        fi
        ;;
      -conf=* | --conf=* | -user=* | --user=* | -user-xml=* | --user-xml=* | -hosts=* | --hosts=* | -ref-json=* | --ref-json=* | \
        -diff=* | --diff=* | -ignore=* | --ignore=* | -from-dump=* | --from-dump=* | -retry-from=* | --retry-from=* | -rf-conf=* | --rf-conf=*)
        v="${args[$i]#*=}"
        case "$v" in
          "" | /*) ;;
          *) if [ -e "$cpwd/$v" ]; then args[$i]="${args[$i]%%=*}=$cpwd/$v"; fi ;;
        esac
        ;;
    esac
  done
  return 0
}

# conf_val <conf파일> <키> <기본값>: bios.conf 에서 key=value 의 값을 읽는다 (없으면 기본값).
# Go 쪽과 같이 마지막으로 나온 줄이 우선이고, 빈 값이면 기본값을 쓴다.
conf_val() {
  local v=""
  if [ -f "$1" ]; then
    v="$(sed -n "s/^[[:space:]]*$2[[:space:]]*=[[:space:]]*//p" "$1" | tail -n 1 | tr -d '\r' | sed 's/[[:space:]]*$//')"
  fi
  if [ -n "$v" ]; then
    echo "$v"
  else
    echo "$3"
  fi
}
