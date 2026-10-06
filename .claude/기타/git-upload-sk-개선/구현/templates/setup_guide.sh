#!/bin/bash
# setup_guide.sh - 신규 프로젝트용 대화형 설정 가이드 (git-upload-sk)
#
# 사용:
#   bash setup_guide.sh [--role os8|os6|common] [--dir <프로젝트루트>] [--no-setup] [--yes] [--help]
#
# 위치 규칙 (이 스크립트는 프로젝트의 setup/ 또는 conf/ 디렉터리에 놓는다):
#   - vars.manifest 는 이 스크립트와 같은 디렉터리의 "vars.manifest" 를 읽는다.
#   - manifest 의 target(sh:/conf:) 상대경로와 #@meta setup= 은 프로젝트 루트 기준으로 푼다.
#     프로젝트 루트 기본값은 이 스크립트 디렉터리의 상위(..), --dir 로 바꾼다.
#   - lib_common.sh 는 같은 디렉터리에서 source 한다 (배포용 생성물은 그 줄 #__LIB_INCLUDE__ 를
#     lib_common.sh 내용으로 치환해 자체포함 스크립트로 만든다).
#
# 종료코드: 0 정상(변경 없음 포함), 1 사용자 중단/실패(변경은 원상복구됨; setup.sh 실패 시에는 설정 변경은 유지),
#           2 인자·파일 오류
#
# 제약: bash 4.1 호환, sed/awk/grep/coreutils 만 사용.

HERE=$(cd "$(dirname "$0")" && pwd)
if [ -f "$HERE/lib_common.sh" ]; then . "$HERE/lib_common.sh"; else echo "[X] lib_common.sh 를 찾을 수 없습니다: $HERE" >&2; exit 2; fi  #__LIB_INCLUDE__

ROLE=
DIR_ARG=
NO_SETUP=0
EMPTY_IDX=()
SETUP_STATE=

usage() {
    cat << 'EOS'
사용법: bash setup_guide.sh [옵션]

  --role os8|os6|common   이 서버의 역할 (생략하면 질문). common 은 공통 변수만.
  --dir <프로젝트루트>    manifest target 상대경로의 기준 (기본: 이 스크립트 디렉터리의 상위 ..)
  --no-setup              가이드 후 setup.sh 이어서 실행 질문을 건너뜀
  --yes                   확인(y/n) 질문에 자동으로 y (값 질문은 그대로 입력받음)
  -h, --help              이 도움말

vars.manifest 는 이 스크립트와 같은 디렉터리에서 읽습니다.
종료코드: 0 정상, 1 중단/실패, 2 인자·파일 오류
EOS
}

bad_usage() {
    err "$1" >&2
    usage >&2
    exit 2
}

on_int() {
    printf '\n' >&2
    err "사용자 중단 (Ctrl-C)" >&2
    exit 1
}

disclaimer() {
    warn "Disclaimer: 이 가이드는 대상 파일의 변수 값만 수정하며, 수정 전 .bak.<시각> 백업을 만들고 실패하면 자동 복구합니다."
    warn "그래도 운영 환경에 적용하기 전 변경 내용을 직접 확인하십시오. 비밀번호는 소스에 평문으로 기록됩니다(chmod 600, 저장소 커밋 금지)."
}

# ---------------------------------------------------------------- 인자
while [ $# -gt 0 ]; do
    case "$1" in
        --role)
            [ $# -ge 2 ] || bad_usage "--role 값이 없습니다"
            ROLE=$2; shift 2 ;;
        --role=*) ROLE=${1#--role=}; shift ;;
        --dir)
            [ $# -ge 2 ] || bad_usage "--dir 값이 없습니다"
            DIR_ARG=$2; shift 2 ;;
        --dir=*) DIR_ARG=${1#--dir=}; shift ;;
        --no-setup) NO_SETUP=1; shift ;;
        --yes|-y) ASSUME_YES=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) bad_usage "알 수 없는 옵션: $1" ;;
    esac
done
case "$ROLE" in ""|os8|os6|common) ;; *) bad_usage "--role 은 os8, os6, common 중 하나: $ROLE" ;; esac

if [ -n "$DIR_ARG" ]; then
    PROJECT_DIR=$DIR_ARG
else
    PROJECT_DIR="$HERE/.."
fi
if ! PROJECT_DIR=$(cd "$PROJECT_DIR" 2> /dev/null && pwd); then
    err "프로젝트 루트 디렉터리가 없습니다: ${DIR_ARG:-$HERE/..}" >&2
    exit 2
fi

trap on_int INT

# ---------------------------------------------------------------- manifest·대상 파일
manifest_load "$HERE/vars.manifest" || { err "vars.manifest 를 읽지 못했습니다: $HERE/vars.manifest" >&2; exit 2; }
if [ "$M_COUNT" -le 0 ]; then err "vars.manifest 에 변수가 없습니다" >&2; exit 2; fi

check_targets() {
    local i seen=$'\n' missing=0
    for ((i = 0; i < M_COUNT; i++)); do
        _target_parse "$i" || { err "target 형식 오류: ${M_NAME[$i]}" >&2; missing=1; continue; }
        case "$seen" in *$'\n'"$_TF"$'\n'*) continue ;; esac
        seen="$seen$_TF"$'\n'
        if [ ! -f "$_TF" ]; then
            err "대상 파일 없음: $_TF  (manifest target ${M_TARGET[$i]})" >&2
            missing=1
        fi
    done
    [ $missing -eq 0 ]
}
check_targets || { err "--dir 로 프로젝트 루트를 지정했는지 확인하세요 (현재: $PROJECT_DIR)" >&2; exit 2; }

# ---------------------------------------------------------------- 빈 변수
count_empty() {   # EMPTY_IDX 에 현재값이 빈 변수 idx 를 채운다
    local i v
    EMPTY_IDX=()
    for ((i = 0; i < M_COUNT; i++)); do
        v=$(target_get "${M_NAME[$i]}") || v=
        [ -z "$v" ] && EMPTY_IDX[${#EMPTY_IDX[@]}]=$i
    done
    return 0
}

list_empty() {   # 직접 채울 변수와 위치 (값은 출력하지 않음)
    local k i d
    if [ ${#EMPTY_IDX[@]} -eq 0 ]; then info "비어 있는 변수가 없습니다"; return 0; fi
    info "직접 채울 변수와 위치:"
    for ((k = 0; k < ${#EMPTY_IDX[@]}; k++)); do
        i=${EMPTY_IDX[$k]}
        _target_parse "$i"
        d=${M_DESC[$i]%%\\n*}
        printf '    %s%s%s  (%s, 역할: %s)\n' "$C_BOLD" "${M_NAME[$i]}" "$C_RST" "${M_KIND[$i]}" "${M_ROLE[$i]}"
        printf '        위치: %s  (%s)\n' "$_TF" "${_TT}"
        printf '        설명: %s\n' "$d"
        if [ -n "${M_EXAMPLE[$i]}" ] && [ "${M_KIND[$i]}" != secret ]; then
            printf '%s        예: %s%s\n' "$C_DIM" "${M_EXAMPLE[$i]}" "$C_RST"
        fi
    done
}

# ---------------------------------------------------------------- 역할
select_role() {   # ROLE 을 정한다 (이미 있으면 그대로)
    local i has8=0 has6=0 n k ans
    local -a opts=()
    [ -n "$ROLE" ] && return 0
    for ((i = 0; i < M_COUNT; i++)); do
        case ",${M_ROLE[$i]}," in *,os8,*) has8=1 ;; esac
        case ",${M_ROLE[$i]}," in *,os6,*) has6=1 ;; esac
    done
    if [ $has8 -eq 0 ] && [ $has6 -eq 0 ]; then
        ROLE=common
        info "역할별 변수가 없어 공통 변수만 진행합니다"
        return 0
    fi
    [ $has8 -eq 1 ] && opts[${#opts[@]}]=os8
    [ $has6 -eq 1 ] && opts[${#opts[@]}]=os6
    opts[${#opts[@]}]=common
    printf '\n%s[i]%s 이 서버의 역할을 선택하세요\n' "$C_INFO" "$C_RST"
    for ((k = 0; k < ${#opts[@]}; k++)); do
        if [ "${opts[$k]}" = common ]; then
            printf '    %d) common  (공통 변수만)\n' $((k + 1))
        else
            printf '    %d) %s\n' $((k + 1)) "${opts[$k]}"
        fi
    done
    n=${#opts[@]}
    while :; do
        _read_line ans "역할 (번호 또는 이름): " 0 || { err "입력이 끝났습니다 (EOF)" >&2; exit 1; }
        _trim_into ans "$ans"
        if [[ $ans =~ ^[0-9]+$ ]] && [ "$ans" -ge 1 ] && [ "$ans" -le "$n" ]; then
            ROLE=${opts[$((ans - 1))]}; break
        fi
        for ((k = 0; k < n; k++)); do
            [ "$ans" = "${opts[$k]}" ] && ROLE=$ans
        done
        [ -n "$ROLE" ] && break
        warn "1 ~ $n 사이의 번호나 역할 이름을 입력해 주세요"
    done
    info "선택한 역할: $ROLE"
}

# ---------------------------------------------------------------- setup.sh 이어서 실행
run_setup_chain() {   # 반환 0: 정상/건너뜀, 1: setup 실패
    local s=${META[setup]-} f rc
    SETUP_STATE=
    [ -z "$s" ] && return 0
    [ "$s" = - ] && return 0
    case "$s" in /*) f=$s ;; *) f="$PROJECT_DIR/$s" ;; esac
    [ -f "$f" ] || return 0
    if [ $NO_SETUP -eq 1 ]; then SETUP_STATE="건너뜀 (--no-setup): $s"; return 0; fi
    printf '\n'
    if ! confirm "$(basename "$s") 를 이어서 실행하시겠습니까? (y/n)"; then
        SETUP_STATE="실행 안 함: $s"
        return 0
    fi
    info "실행: $s  (작업 디렉터리: $PROJECT_DIR)"
    (cd "$PROJECT_DIR" && bash "$f")
    rc=$?
    if [ $rc -ne 0 ]; then
        SETUP_STATE="실패 (종료코드 $rc): $s"
        err "$s 가 실패했습니다 (종료코드 $rc). 설정 값 변경은 그대로 유지됩니다."
        return 1
    fi
    SETUP_STATE="완료: $s"
    ok "$s 실행 완료"
    return 0
}

final_summary() {
    local k i names=
    printf '\n%s== 최종 요약 ==%s\n' "$C_BOLD" "$C_RST"
    printf '    역할: %s\n' "$ROLE"
    printf '    질문한 변수: %d개, 값 변경: %d개, 건너뜀: %d개\n' "${#GUIDE_SET[@]}" "${#NEWVAL[@]}" "${#SKIPPED[@]}"
    for ((k = 0; k < ${#GUIDE_SET[@]}; k++)); do
        i=${GUIDE_SET[$k]}
        [ -n "${SKIPPED[$i]+x}" ] && names="$names ${M_NAME[$i]}"
    done
    [ -n "$names" ] && warn "건너뛴 변수는 직접 채워야 합니다:$names"
    [ -n "$SETUP_STATE" ] && printf '    setup: %s\n' "$SETUP_STATE"
    warn "설정 변경 후 랜덤 서버 몇 대에서 실제 변경 확인 바랍니다."
}

# ---------------------------------------------------------------- main
printf '\n%s== %s 설정 가이드 ==%s' "$C_BOLD" "${META[project]:-프로젝트}" "$C_RST"
[ -n "${META[version]-}" ] && printf '  (v%s)' "${META[version]}"
printf '\n'
info "프로젝트 루트: $PROJECT_DIR"
disclaimer

count_empty
if [ ${#EMPTY_IDX[@]} -le 5 ]; then
    printf '\n'
    info "비어 있는 변수가 ${#EMPTY_IDX[@]}개뿐입니다. 변수가 적으면 직접 파일을 수정하는 편이 빠를 수 있습니다."
    if ! confirm "가이드를 실행하시겠습니까? (y/n)"; then
        list_empty
        exit 0
    fi
fi

select_role

run_guide "$ROLE"
rc=$?
if [ $rc -eq 2 ]; then exit 2; fi
if [ $rc -ne 0 ]; then
    err "가이드가 중단되었습니다 — 파일은 변경되지 않았거나 원상복구되었습니다" >&2
    exit 1
fi

run_setup_chain
setup_rc=$?
final_summary
[ $setup_rc -eq 0 ] || exit 1
exit 0
