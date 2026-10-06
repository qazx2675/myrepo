#!/bin/bash
# setup_guide.sh - 설정 가이드 (make_guide.sh 로 자동 생성, 2026-10-06 09:44) — 직접 고치지 말고 틀을 고친 뒤 다시 생성한다.
# 이 파일은 자체포함 단일 스크립트다 (lib_common.sh 내용을 인라인). vars.manifest 는 이 스크립트와 같은 디렉터리에서 읽는다.
# 붙여넣기 확인: 마지막 줄이 #__END_OF_GUIDE__ 이고 전체 줄 수가 아래 값(001072)이어야 한다 — 아니면 실행하지 않는다.
GUS_EXPECT_LINES=001072
_gus_selfcheck() {
    local n l
    if [ ! -f "$0" ]; then
        printf '[X] 파일로 저장한 뒤 bash <파일> 로 실행하십시오 (표준입력·프로세스 치환으로는 실행할 수 없음)\n' >&2; return 1
    fi
    n=$(awk 'END { print NR }' "$0")
    l=$(tail -n 1 "$0" | tr -d '\r')
    if [ "$l" != '#__END_OF_GUIDE__' ] || [ "$n" != "$((10#$GUS_EXPECT_LINES))" ]; then
        printf '[X] 스크립트가 잘렸거나 변형되었습니다 (줄 수 %s, 기대 %s, 마지막 줄 [%s]) — 다시 복사해 주십시오.\n' "$n" "$((10#$GUS_EXPECT_LINES))" "${l:0:30}" >&2
        return 1
    fi
    return 0
}
_gus_selfcheck || exit 2
unset -f _gus_selfcheck
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
# ===== lib_common.sh (인라인) =====
# lib_common.sh - git-upload-sk 공용 함수 (가이드·패치 스크립트가 source 하거나 그대로 인라인)
#
# 제약: bash 4.1 호환, sed/awk/grep/coreutils 만 사용 (python/perl 없음).
# 최상위(함수 밖)에서 source/인라인해야 한다 (declare -A 가 전역이 되도록).
#
# 호출 측이 정하는 전역:
#   PROJECT_DIR   manifest target 의 상대경로 기준 디렉터리 (기본 ".")
#   ASSUME_YES=1  confirm 에 자동 y (테스트·--yes 용)
#   PROBE=0       probe 점검 훅 호출 안 함 (기본 1)
#   GUIDE_ONLY    run_guide 가 질문할 변수명 부분집합 (공백 구분, 업데이트용; 비면 전체)
#   probe_hook    (선택) 호출 측이 정의하는 함수: probe_hook <idx> <value>, 실패 시 rc!=0 + stdout 이유
#
# target_set 반환코드: 0 정상, 1 변수/형식 오류, 2 선언 줄 없음(또는 대상 파일 없음),
#                      3 값에 줄바꿈, 4 쓰기 후 문법 오류(원본 불변), 5 읽기-검증 불일치(원본 불변)

LIB_COMMON_VERSION=1

declare -A META
declare -A M_INDEX
M_COUNT=0
M_NAME=(); M_TARGET=(); M_KIND=(); M_ROLE=(); M_GROUP=()
M_REQUIRED=(); M_DESC=(); M_EXAMPLE=(); M_CHECK=(); M_SINCE=(); M_RENAMED=()
GUARDS=()
XOR_Q=()
DEFAULT_GUARDS=('^[[:space:]]*#' '담당자' '감사합니다' '점검부탁')

NEWVAL=()        # idx -> 새로 입력된 값 (현재값과 다를 때만)
SKIPPED=()       # idx -> 1 (사용자가 건너뜀)
GUIDE_SET=()     # 이번 가이드에서 질문 대상이 된 idx (manifest 순서)
GUIDE_CAND=()    # 역할·부분집합 필터를 통과한 idx
XOR_OFF=()       # idx -> 1 (택일에서 선택되지 않음)
SUMMARY_BAD=()   # summary_table 이 찾은 이상 idx (건너뜀 제외)
_AP_FILES=(); _AP_BAKS=()
_LIB_TMPS=()
_TMP=
_IDX=
_TT=
_TF=
_SECRET_WARNED=0

# ---------------------------------------------------------------- 임시 파일
_tmp_new() {
    _TMP=$(mktemp "${TMPDIR:-/tmp}/gus.XXXXXX") || return 1
    _LIB_TMPS[${#_LIB_TMPS[@]}]=$_TMP
}

lib_cleanup() {
    local i
    for ((i = 0; i < ${#_LIB_TMPS[@]}; i++)); do rm -f "${_LIB_TMPS[$i]}"; done
    _LIB_TMPS=()
}

# ---------------------------------------------------------------- UI
ui_init() {
    C_OK=; C_WARN=; C_ERR=; C_INFO=; C_BOLD=; C_DIM=; C_RST=
    if [ -t 1 ] && [ -z "${NO_COLOR+x}" ]; then
        C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_ERR=$'\033[31m'; C_INFO=$'\033[36m'
        C_BOLD=$'\033[1m'; C_DIM=$'\033[90m'; C_RST=$'\033[0m'
    fi
}

ok()   { printf '%s[O] %s%s\n' "$C_OK" "$*" "$C_RST"; }
warn() { printf '%s[!] %s%s\n' "$C_WARN" "$*" "$C_RST"; }
err()  { printf '%s[X] %s%s\n' "$C_ERR" "$*" "$C_RST"; }
info() { printf '%s[i] %s%s\n' "$C_INFO" "$*" "$C_RST"; }

# ---------------------------------------------------------------- 문자열 도구
_trim_into() {   # _trim_into <변수명> <값>
    local __v=$2
    __v=${__v#"${__v%%[![:space:]]*}"}
    __v=${__v%"${__v##*[![:space:]]}"}
    printf -v "$1" '%s' "$__v"
}

_mask() {   # _mask <kind> <값> : 화면 표시용 (secret 은 ****)
    if [ -z "$2" ]; then printf '(비어 있음)'
    elif [ "$1" = secret ]; then printf '****'
    else printf '%s' "$2"; fi
}

_desc_out() {   # 설명 안 \n 을 줄바꿈으로, 들여쓰기 4칸
    local d=${1//\\n/$'\n'}
    printf '%s\n' "$d" | sed 's/^/    /'
}

# ---------------------------------------------------------------- 파일 도구
lf_normalize() {
    local f=$1 t
    [ -f "$f" ] || return 1
    if grep -q $'\r' "$f"; then
        _tmp_new || return 1
        t=$_TMP
        tr -d '\r' < "$f" > "$t" && cat "$t" > "$f"
        rm -f "$t"
        info "CR 제거(LF 로 정규화): $f"
    fi
    return 0
}

backup_file() {
    local f=$1 ts dst n=0
    if [ ! -f "$f" ]; then err "백업할 파일 없음: $f" >&2; return 1; fi
    ts=$(date +%Y%m%d%H%M%S)
    dst="$f.bak.$ts"
    while [ -e "$dst" ]; do n=$((n + 1)); dst="$f.bak.$ts.$n"; done
    if ! cp -p "$f" "$dst"; then err "백업 실패: $f" >&2; return 1; fi
    printf '%s\n' "$dst"
}

restore_latest() {
    local f=$1 last
    last=$(ls -1d "$f".bak.* 2>/dev/null | LC_ALL=C sort | tail -n 1)
    if [ -z "$last" ]; then err "복원할 백업 없음: $f"; return 1; fi
    if [ -e "$f" ]; then cat "$last" > "$f"; else cp -p "$last" "$f"; fi
    # shellcheck disable=SC2181
    if [ $? -ne 0 ]; then err "복원 실패: $last -> $f"; return 1; fi
    ok "복원 완료: $f  <-  $last"
    return 0
}

# ---------------------------------------------------------------- manifest
_manifest_reset() {
    M_COUNT=0
    M_NAME=(); M_TARGET=(); M_KIND=(); M_ROLE=(); M_GROUP=()
    M_REQUIRED=(); M_DESC=(); M_EXAMPLE=(); M_CHECK=(); M_SINCE=(); M_RENAMED=()
    M_INDEX=(); META=(); GUARDS=(); XOR_Q=()
}

_manifest_parse() {   # stdin 에서 읽는다
    local line t n=0 bad=0 pipes rest k v id i
    local f_name f_target f_kind f_role f_group f_req f_desc f_ex f_chk f_since f_ren
    while IFS= read -r line || [ -n "$line" ]; do
        n=$((n + 1))
        line=${line%$'\r'}
        _trim_into t "$line"
        [ -z "$t" ] && continue
        case "$t" in
            "#@guard "*)
                rest=${t#"#@guard "}
                printf '' | grep -Eq -- "$rest" 2>/dev/null
                if [ $? -eq 2 ]; then
                    err "manifest ${n}줄: #@guard 정규식 오류"; bad=1
                else
                    GUARDS[${#GUARDS[@]}]=$rest
                fi
                continue ;;
            "#@xor "*)
                rest=${t#"#@xor "}
                id=${rest%%|*}
                if [ "$id" = "$rest" ] || ! [[ $id =~ ^[A-Za-z0-9_]+$ ]]; then
                    err "manifest ${n}줄: #@xor 형식 오류 (#@xor <id>|<질문>|<변수>:<설명>|...)"; bad=1
                else
                    XOR_Q[${#XOR_Q[@]}]=$rest
                fi
                continue ;;
            "#@meta "*)
                rest=${t#"#@meta "}
                k=${rest%%=*}; v=${rest#*=}
                _trim_into k "$k"; _trim_into v "$v"
                if [ "$k" = "$rest" ] || [ -z "$k" ]; then
                    err "manifest ${n}줄: #@meta 형식 오류 (#@meta key=value)"; bad=1
                else
                    META[$k]=$v
                fi
                continue ;;
            "#@"*) warn "manifest ${n}줄: 알 수 없는 지시자 무시: ${t%% *}"; continue ;;
            "#"*) continue ;;
        esac
        pipes=${line//[^|]/}
        if [ ${#pipes} -ne 10 ]; then
            err "manifest ${n}줄: 필드 수 오류 (구분자 | 가 ${#pipes}개, 10개 필요 — 값에 | 금지)"; bad=1
            continue
        fi
        IFS='|' read -r f_name f_target f_kind f_role f_group f_req f_desc f_ex f_chk f_since f_ren <<< "$line"
        _trim_into f_name "$f_name"; _trim_into f_target "$f_target"; _trim_into f_kind "$f_kind"
        _trim_into f_role "$f_role"; _trim_into f_group "$f_group"; _trim_into f_req "$f_req"
        _trim_into f_desc "$f_desc"; _trim_into f_ex "$f_ex"; _trim_into f_chk "$f_chk"
        _trim_into f_since "$f_since"; _trim_into f_ren "$f_ren"
        f_role=${f_role// /}
        [ -z "$f_ren" ] && f_ren=-
        if ! [[ $f_name =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
            err "manifest ${n}줄: 변수명 형식 오류: $f_name"; bad=1; continue
        fi
        if [ -n "${M_INDEX[$f_name]+x}" ]; then
            err "manifest ${n}줄: 변수명 중복: $f_name"; bad=1; continue
        fi
        case "$f_target" in sh:?*|conf:?*) ;; *) err "manifest ${n}줄($f_name): target 은 sh:<경로> 또는 conf:<경로>"; bad=1; continue ;; esac
        case "$f_kind" in path|host|secret|text) ;; *) err "manifest ${n}줄($f_name): kind 오류: $f_kind"; bad=1; continue ;; esac
        [ -n "$f_role" ] || { err "manifest ${n}줄($f_name): role 이 비어 있음"; bad=1; continue; }
        case "$f_group" in
            -) ;;
            xor:?*) [[ ${f_group#xor:} =~ ^[A-Za-z0-9_]+$ ]] || { err "manifest ${n}줄($f_name): xor id 오류"; bad=1; continue; } ;;
            dep:?*) [[ ${f_group#dep:} =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || { err "manifest ${n}줄($f_name): dep 변수명 오류"; bad=1; continue; } ;;
            *) err "manifest ${n}줄($f_name): group 은 - / xor:<id> / dep:<변수>"; bad=1; continue ;;
        esac
        case "$f_req" in y|n) ;; *) err "manifest ${n}줄($f_name): required 는 y 또는 n"; bad=1; continue ;; esac
        case "$f_chk" in file|dir|host|probe|none) ;; *) err "manifest ${n}줄($f_name): check 오류: $f_chk"; bad=1; continue ;; esac
        i=$M_COUNT
        M_NAME[$i]=$f_name; M_TARGET[$i]=$f_target; M_KIND[$i]=$f_kind; M_ROLE[$i]=$f_role
        M_GROUP[$i]=$f_group; M_REQUIRED[$i]=$f_req; M_DESC[$i]=$f_desc; M_EXAMPLE[$i]=$f_ex
        M_CHECK[$i]=$f_chk; M_SINCE[$i]=$f_since; M_RENAMED[$i]=$f_ren
        M_INDEX[$f_name]=$i
        M_COUNT=$((i + 1))
    done
    [ $bad -eq 0 ]
}

manifest_load() {   # manifest_load <파일|->   ('-' 또는 인자 없음 = stdin 블록)
    local src=${1:--} rc=0
    _manifest_reset
    if [ "$src" = - ]; then
        _manifest_parse || rc=1
    elif [ -f "$src" ]; then
        _manifest_parse < "$src" || rc=1
    else
        err "manifest 파일 없음: $src" >&2
        return 2
    fi
    return $rc
}

_idx_of() {   # _idx_of <변수명> -> _IDX
    if [ -n "${M_INDEX[$1]+x}" ]; then _IDX=${M_INDEX[$1]}; return 0; fi
    return 1
}

_role_match() {   # _role_match <idx> <role> : common 이거나 role 목록에 포함
    local r=",${M_ROLE[$1]},"
    case "$r" in *,common,*) return 0 ;; esac
    case "$r" in *,"$2",*) return 0 ;; esac
    return 1
}

# ---------------------------------------------------------------- target 읽기/쓰기
# awk 프로그램: mode=get|set, type=sh|conf, name=변수명, ENVIRON[GUS_VAL]=새 값 (-v 를 쓰면 백슬래시가 해석되므로 ENVIRON)
_GUS_AWK='
function shq(s,   i, c, o, n) {
    o = ""; n = length(s)
    for (i = 1; i <= n; i++) {
        c = substr(s, i, 1)
        if (c == "\\" || c == "\"" || c == "$" || c == "`") o = o "\\"
        o = o c
    }
    return o
}
BEGIN {
    newv = ENVIRON["GUS_VAL"]; found = 0; sq = "\047"
    if (type == "sh") pat = "^[ \t]*(export[ \t]+)?" name "="
    else pat = "^[ \t]*" name "[ \t]*=[ \t]*"
}
{
    line = $0; sub(/\r$/, "", line)
    if (!found && match(line, pat)) {
        pre = substr(line, 1, RLENGTH); rest = substr(line, RLENGTH + 1)
        ok = 1; val = ""; trail = ""
        if (type == "conf") {
            val = rest; sub(/[ \t]+$/, "", val)
        } else {
            c = substr(rest, 1, 1)
            if (c == "\"") {
                ok = 0; n = length(rest)
                for (i = 2; i <= n; i++) {
                    ch = substr(rest, i, 1)
                    if (ch == "\\") {
                        nx = substr(rest, i + 1, 1)
                        if (nx == "\\" || nx == "\"" || nx == "$" || nx == "`") { val = val nx; i++ }
                        else val = val ch
                    } else if (ch == "\"") { ok = 1; trail = substr(rest, i + 1); break }
                    else val = val ch
                }
            } else if (c == sq) {
                t = substr(rest, 2); k = index(t, sq)
                if (k == 0) ok = 0
                else { val = substr(t, 1, k - 1); trail = substr(t, k + 1) }
            } else {
                if (match(rest, /[ \t;]/)) { val = substr(rest, 1, RSTART - 1); trail = substr(rest, RSTART) }
                else val = rest
            }
        }
        if (ok) {
            found = 1
            if (mode == "get") { print val; exit 0 }
            if (type == "conf") line = pre newv
            else line = pre "\"" shq(newv) "\"" trail
        }
    }
    if (mode == "set") print line
}
END { if (!found) exit 2 }
'

_target_parse() {   # _target_parse <idx> -> _TT(sh|conf) _TF(파일 경로)
    local t=${M_TARGET[$1]}
    case "$t" in
        sh:*)   _TT=sh;   _TF=${t#sh:} ;;
        conf:*) _TT=conf; _TF=${t#conf:} ;;
        *) return 1 ;;
    esac
    case "$_TF" in /*) ;; *) _TF="${PROJECT_DIR:-.}/$_TF" ;; esac
    return 0
}

_awk_target() {   # _awk_target <mode> <type> <name> <file> [value]
    GUS_VAL=${5-} awk -v mode="$1" -v type="$2" -v name="$3" "$_GUS_AWK" "$4"
}

target_get() {
    local name=$1 v
    _idx_of "$name" || return 1
    _target_parse "$_IDX" || return 1
    [ -f "$_TF" ] || return 2
    v=$(_awk_target get "$_TT" "$name" "$_TF") || return 2
    printf '%s\n' "$v"
}

target_set() {
    local name=$1 val=${2-} idx tt tf tmp got rc kind
    _idx_of "$name" || { err "target_set: manifest 에 없는 변수: $name"; return 1; }
    idx=$_IDX
    _target_parse "$idx" || { err "target_set: target 형식 오류: $name"; return 1; }
    tt=$_TT; tf=$_TF; kind=${M_KIND[$idx]}
    case "$val" in *$'\n'*|*$'\r'*) err "target_set: 값에 줄바꿈을 넣을 수 없음: $name"; return 3 ;; esac
    if [ ! -f "$tf" ]; then err "대상 파일 없음: $tf"; return 2; fi
    _tmp_new || { err "임시 파일 생성 실패"; return 1; }
    tmp=$_TMP
    _awk_target set "$tt" "$name" "$tf" "$val" > "$tmp"
    rc=$?
    if [ $rc -ne 0 ]; then
        rm -f "$tmp"
        err "선언 줄을 찾지 못함: $name  ($tf)"
        return 2
    fi
    got=$(_awk_target get "$tt" "$name" "$tmp")
    if [ "$got" != "$val" ]; then
        rm -f "$tmp"
        err "쓰기 검증 불일치(원본 불변): $name"
        return 5
    fi
    if [ "$tt" = sh ] && bash -n "$tf" 2>/dev/null && ! bash -n "$tmp" 2>/dev/null; then
        rm -f "$tmp"
        err "쓰기 후 문법 오류가 생겨 취소(원본 불변): $name"
        return 4
    fi
    if ! cat "$tmp" > "$tf"; then
        rm -f "$tmp"
        err "대상 파일 쓰기 실패: $tf"
        return 1
    fi
    rm -f "$tmp"
    if [ "$kind" = secret ] && [ -n "$val" ]; then
        chmod 600 "$tf"
        if [ "$_SECRET_WARNED" != 1 ]; then
            _SECRET_WARNED=1
            warn "비밀번호가 소스에 평문으로 기록됩니다 (chmod 600 적용). 이 파일을 저장소에 커밋하지 마십시오."
        fi
    fi
    return 0
}

# ---------------------------------------------------------------- 검증
_host_reason() {   # 형식이 맞으면 빈 문자열, 아니면 이유
    local v=$1 h user port re o
    local -a oct
    h=$v
    case "$h" in
        *@*)
            user=${h%%@*}; h=${h#*@}
            re='^[A-Za-z0-9_][A-Za-z0-9_.-]*$'
            [[ $user =~ $re ]] || { printf '사용자명 형식 오류'; return 1; } ;;
    esac
    case "$h" in
        *:*)
            port=${h##*:}; h=${h%:*}
            re='^[0-9]+$'
            if ! [[ $port =~ $re ]] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
                printf '포트 형식 오류(1-65535)'; return 1
            fi ;;
    esac
    [ -n "$h" ] || { printf '호스트가 비어 있음'; return 1; }
    re='^[0-9.]+$'
    if [[ $h =~ $re ]]; then
        re='^[0-9]{1,3}(\.[0-9]{1,3}){3}$'
        [[ $h =~ $re ]] || { printf 'IP 주소 형식 오류'; return 1; }
        IFS=. read -r -a oct <<< "$h"
        for o in "${oct[@]}"; do
            [ "$((10#$o))" -le 255 ] || { printf 'IP 주소 범위 오류(0-255)'; return 1; }
        done
        return 0
    fi
    re='^[A-Za-z0-9]([A-Za-z0-9_.-]*[A-Za-z0-9])?$'
    [[ $h =~ $re ]] || { printf '호스트명 형식 오류'; return 1; }
    case "$h" in *..*) printf '호스트명 형식 오류'; return 1 ;; esac
    return 0
}

check_value() {   # check_value <idx> <value> -> stdout: OK 또는 이유 (OK 면 rc 0)
    local idx=$1 val=${2-} kind chk req r pr
    kind=${M_KIND[$idx]}; chk=${M_CHECK[$idx]}; req=${M_REQUIRED[$idx]}
    case "$val" in *$'\n'*|*$'\r'*) echo "값에 줄바꿈이 들어 있음"; return 1 ;; esac
    if [ -z "$val" ]; then
        if [ "$req" = y ]; then echo "필수 값이 비어 있음"; return 1; fi
        echo OK; return 0
    fi
    if [ "$kind" = secret ]; then echo OK; return 0; fi   # secret 은 값을 이유 문구에 쓰지 않도록 형식 검사 안 함
    case "$chk" in
        file)
            if [ ! -f "$val" ]; then echo "파일 없음: $val"; return 1; fi
            if [ ! -r "$val" ]; then echo "읽기 권한 없음: $val"; return 1; fi ;;
        dir)
            if [ ! -d "$val" ]; then echo "디렉터리 없음: $val"; return 1; fi ;;
        host|probe)
            r=$(_host_reason "$val") || { echo "$r"; return 1; }
            if [ "$chk" = probe ] && [ "${PROBE:-1}" = 1 ] && declare -F probe_hook > /dev/null 2>&1; then
                pr=$(probe_hook "$idx" "$val") || { echo "연결 점검 실패${pr:+: $pr}"; return 1; }
            fi ;;
        none|*) ;;
    esac
    echo OK
    return 0
}

guard_hit() {   # guard_hit <줄> : 마스킹 가드(주석·키워드) 에 걸리면 rc 0
    local line=$1 g i
    for g in "${DEFAULT_GUARDS[@]}"; do
        printf '%s\n' "$line" | grep -Eq -- "$g" && return 0
    done
    for ((i = 0; i < ${#GUARDS[@]}; i++)); do
        printf '%s\n' "$line" | grep -Eq -- "${GUARDS[$i]}" && return 0
    done
    return 1
}

# ---------------------------------------------------------------- 입력
_read_line() {   # _read_line <변수명> <프롬프트> [secret(1)] — tty 아니면 프롬프트를 stderr 로
    local __n=$1 __p=$2 __s=${3:-0} __v= __rc=0
    if [ -t 0 ]; then
        if [ "$__s" = 1 ]; then
            IFS= read -r -s -p "$__p" __v || __rc=$?
            printf '\n' >&2
        else
            IFS= read -r -p "$__p" __v || __rc=$?
        fi
    else
        printf '%s' "$__p" >&2
        IFS= read -r __v || __rc=$?
        printf '\n' >&2
    fi
    if [ $__rc -ne 0 ] && [ -z "$__v" ]; then return 1; fi
    __v=${__v%$'\r'}
    printf -v "$__n" '%s' "$__v"
    return 0
}

confirm() {   # confirm <질문> : y -> 0, n -> 1 (잘못된 입력은 다시 묻는다)
    local q=$1 ans
    case "$q" in *"(y/n)"*) ;; *) q="$q (y/n)" ;; esac
    if [ "${ASSUME_YES:-0}" = 1 ]; then
        printf '%s: y (자동)\n' "$q"
        return 0
    fi
    while :; do
        if ! _read_line ans "$q: " 0; then err "입력이 끝나 n 으로 처리"; return 1; fi
        case "$ans" in
            y|Y|yes|YES|Yes) return 0 ;;
            n|N|no|NO|No) return 1 ;;
            *) warn "y 또는 n 으로 답해 주세요" ;;
        esac
    done
}

_effective() {   # 대기 중인 새 값이 있으면 그것, 없으면 target 의 현재값
    local i=$1 v
    if [ -n "${NEWVAL[$i]+x}" ]; then
        printf '%s' "${NEWVAL[$i]}"
    else
        v=$(target_get "${M_NAME[$i]}") || v=
        printf '%s' "$v"
    fi
}

ask_var() {   # ask_var <idx>  rc: 0 입력/유지, 3 건너뜀, 1 입력 종료(EOF)
    local idx=$1 name kind req g cur val v2 reason sec=0 meta
    name=${M_NAME[$idx]}; kind=${M_KIND[$idx]}
    [ "$kind" = secret ] && sec=1
    if [ "${M_REQUIRED[$idx]}" = y ]; then req=필수; else req=선택; fi
    g=${M_GROUP[$idx]}
    meta="$kind, $req"
    case "$g" in xor:*) meta="$meta, 택일:${g#xor:}" ;; dep:*) meta="$meta, 의존:${g#dep:}" ;; esac
    if ! target_get "$name" > /dev/null; then
        warn "대상에서 선언 줄을 찾지 못함: ${M_TARGET[$idx]} ($name) — 적용 단계에서 오류가 납니다"
    fi
    cur=$(_effective "$idx")
    printf '\n%s[i]%s %s%s%s  (%s)\n' "$C_INFO" "$C_RST" "$C_BOLD" "$name" "$C_RST" "$meta"
    _desc_out "${M_DESC[$idx]}"
    if [ -n "${M_EXAMPLE[$idx]}" ]; then printf '%s    예: %s%s\n' "$C_DIM" "${M_EXAMPLE[$idx]}" "$C_RST"; fi
    if [ $sec = 1 ]; then printf '    (입력은 화면에 표시되지 않으며 소스에 평문으로 기록됩니다)\n'; fi
    printf '    현재값: %s\n' "$(_mask "$kind" "$cur")"
    _read_line val "값 입력 (Enter=현재값 유지): " $sec || { err "입력이 끝났습니다 (EOF)"; return 1; }
    [ $sec = 1 ] || _trim_into val "$val"
    [ -n "$val" ] || val=$cur
    while :; do
        reason=$(check_value "$idx" "$val") && break
        err "이상: $reason"
        _read_line v2 "다시 입력 (s 또는 Enter=건너뛰기): " $sec || { err "입력이 끝났습니다 (EOF)"; return 1; }
        [ $sec = 1 ] || _trim_into v2 "$v2"
        if [ -z "$v2" ] || [ "$v2" = s ] || [ "$v2" = S ]; then
            SKIPPED[$idx]=1
            warn "$name 건너뜀"
            return 3
        fi
        val=$v2
    done
    unset 'SKIPPED[$idx]'
    if [ "$val" != "$cur" ]; then
        NEWVAL[$idx]=$val
        ok "$name = $(_mask "$kind" "$val")"
    else
        unset 'NEWVAL[$idx]'
        info "$name 현재값 유지"
    fi
    return 0
}

# ---------------------------------------------------------------- 요약 표
summary_table() {   # 변수별 정상/이상(이유) 표. 이상이 있으면 rc 1, SUMMARY_BAD 에 idx
    local i k cur res mark word col n
    local -a set=()
    SUMMARY_BAD=()
    if [ ${#GUIDE_SET[@]} -gt 0 ]; then
        for ((k = 0; k < ${#GUIDE_SET[@]}; k++)); do set[${#set[@]}]=${GUIDE_SET[$k]}; done
    else
        for ((i = 0; i < M_COUNT; i++)); do set[${#set[@]}]=$i; done
    fi
    printf '\n%s== 점검 결과 ==%s\n' "$C_BOLD" "$C_RST"
    printf '    %-24s %s\n' "변수" "값  /  상태"
    for ((k = 0; k < ${#set[@]}; k++)); do
        i=${set[$k]}
        cur=$(_effective "$i")
        res=$(check_value "$i" "$cur")
        if [ "$res" = OK ]; then
            mark='[O]'; col=$C_OK; word=정상
        elif [ -n "${SKIPPED[$i]+x}" ]; then
            mark='[!]'; col=$C_WARN; word="건너뜀(이상: $res)"
        else
            mark='[X]'; col=$C_ERR; word="이상($res)"
            SUMMARY_BAD[${#SUMMARY_BAD[@]}]=$i
        fi
        printf '%s%s%s %-24s %s  %s%s\n' "$col" "$mark" "$C_RST" "${M_NAME[$i]}" "$(_mask "${M_KIND[$i]}" "$cur")" "$col$word" "$C_RST"
    done
    n=${#SUMMARY_BAD[@]}
    [ "$n" -eq 0 ]
}

# ---------------------------------------------------------------- 가이드
guide_reset() {
    NEWVAL=(); SKIPPED=(); GUIDE_SET=(); GUIDE_CAND=(); XOR_OFF=(); SUMMARY_BAD=()
}

_only_match() {
    [ -z "${GUIDE_ONLY:-}" ] && return 0
    case " $GUIDE_ONLY " in *" $1 "*) return 0 ;; esac
    return 1
}

_xor_choose() {   # _xor_choose <id> : GUIDE_CAND 중 xor:<id> 구성원 중 하나를 고르게 한다
    local id=$1 k i p q= e v d def=0 n=0 ans cur sel
    local -a mem=() parts=() od_n=() od_d=()
    for ((k = 0; k < ${#GUIDE_CAND[@]}; k++)); do
        i=${GUIDE_CAND[$k]}
        [ "${M_GROUP[$i]}" = "xor:$id" ] && mem[${#mem[@]}]=$i
    done
    [ ${#mem[@]} -gt 1 ] || return 0
    for ((k = 0; k < ${#XOR_Q[@]}; k++)); do
        e=${XOR_Q[$k]}
        IFS='|' read -r -a parts <<< "$e"
        if [ "${parts[0]}" = "$id" ]; then
            q=${parts[1]}
            for ((p = 2; p < ${#parts[@]}; p++)); do
                v=${parts[$p]%%:*}; d=${parts[$p]#*:}
                od_n[${#od_n[@]}]=$v; od_d[${#od_d[@]}]=$d
            done
            break
        fi
    done
    [ -n "$q" ] || q="다음 중 하나만 채웁니다 ($id)"
    printf '\n%s[i]%s %s%s%s\n' "$C_INFO" "$C_RST" "$C_BOLD" "$q" "$C_RST"
    for ((k = 0; k < ${#mem[@]}; k++)); do
        i=${mem[$k]}; d=
        for ((p = 0; p < ${#od_n[@]}; p++)); do
            [ "${od_n[$p]}" = "${M_NAME[$i]}" ] && d=${od_d[$p]}
        done
        [ -n "$d" ] || d=${M_DESC[$i]}
        d=${d//\\n/ }
        printf '    %d) %s%s%s — %s\n' $((k + 1)) "$C_BOLD" "${M_NAME[$i]}" "$C_RST" "$d"
        cur=$(_effective "$i")
        if [ -n "$cur" ] && [ $def -eq 0 ]; then def=$((k + 1)); fi
    done
    printf '    0) 사용 안 함 (모두 비워 둠)\n'
    while :; do
        _read_line ans "선택 (번호, Enter=기본 $def): " 0 || { err "입력이 끝났습니다 (EOF)"; return 1; }
        [ -n "$ans" ] || ans=$def
        if [[ $ans =~ ^[0-9]+$ ]] && [ "$ans" -le ${#mem[@]} ]; then break; fi
        warn "0 ~ ${#mem[@]} 사이의 번호를 입력해 주세요"
    done
    sel=$ans
    for ((k = 0; k < ${#mem[@]}; k++)); do
        i=${mem[$k]}
        if [ $((k + 1)) -ne "$sel" ]; then
            XOR_OFF[$i]=1
            cur=$(_effective "$i")
            [ -n "$cur" ] && warn "${M_NAME[$i]} 에 이미 값이 있으나 택일에서 제외되어 질문하지 않습니다 (값은 그대로 둠)"
        fi
    done
    [ "$sel" -gt 0 ] && info "선택: ${M_NAME[${mem[$((sel - 1))]}]}" || info "선택: 사용 안 함"
    return 0
}

_rollback() {
    local k
    for ((k = 0; k < ${#_AP_FILES[@]}; k++)); do
        cat "${_AP_BAKS[$k]}" > "${_AP_FILES[$k]}" && warn "원상복구: ${_AP_FILES[$k]}"
    done
}

_verify_file() {   # _verify_file <파일> <백업> <변경 변수 idx 목록(공백)>
    local f=$1 bak=$2 list=$3 t1 line ln txt i nm decl rm_cnt=0 add_cnt exp=0 bad=0
    for i in $list; do exp=$((exp + 1)); done
    _tmp_new || return 1
    t1=$_TMP
    awk '{ sub(/\r$/, ""); print }' "$bak" > "$t1"
    while IFS= read -r line; do
        ln=${line%%|*}; txt=${line#*|}
        rm_cnt=$((rm_cnt + 1))
        decl=0
        for i in $list; do
            nm=${M_NAME[$i]}
            _target_parse "$i"
            if [ "$_TT" = sh ]; then
                printf '%s\n' "$txt" | grep -Eq "^[[:space:]]*(export[[:space:]]+)?${nm}=" && decl=1
            else
                printf '%s\n' "$txt" | grep -Eq "^[[:space:]]*${nm}[[:space:]]*=" && decl=1
            fi
        done
        if [ $decl -eq 0 ]; then
            bad=1
            if guard_hit "$txt"; then
                err "마스킹 위반: 주석·가드 줄이 바뀜 (원본 ${ln}번째 줄) — $f"
            else
                err "의도하지 않은 줄 변경 (원본 ${ln}번째 줄) — $f"
            fi
        fi
    done < <(diff --old-line-format='%dn|%L' --new-line-format='' --unchanged-line-format='' "$t1" "$f")
    add_cnt=$(diff --old-line-format='' --new-line-format='x
' --unchanged-line-format='' "$t1" "$f" | grep -c x)
    rm -f "$t1"
    if [ $rm_cnt -ne $exp ] || [ "$add_cnt" -ne $exp ]; then
        bad=1
        err "변경 줄 수 불일치: 기대 ${exp}, 삭제 ${rm_cnt}, 추가 ${add_cnt} — $f"
    fi
    [ $bad -eq 0 ]
}

_apply_changes() {   # GUIDE_SET 중 NEWVAL 이 있는 변수를 적용. 실패하면 전부 원상복구하고 rc 1
    local i k f bak seen=$'\n' nfile=0 napplied=0 list rc ok_sh
    local -a files=() oks=()
    _AP_FILES=(); _AP_BAKS=()
    for ((k = 0; k < ${#GUIDE_SET[@]}; k++)); do
        i=${GUIDE_SET[$k]}
        [ -n "${NEWVAL[$i]+x}" ] || continue
        _target_parse "$i" || { err "target 형식 오류: ${M_NAME[$i]}"; return 1; }
        case "$seen" in *$'\n'"$_TF"$'\n'*) continue ;; esac
        seen="$seen$_TF"$'\n'
        files[${#files[@]}]=$_TF
    done
    for ((nfile = 0; nfile < ${#files[@]}; nfile++)); do
        f=${files[$nfile]}
        bak=$(backup_file "$f") || { _rollback; return 1; }
        _AP_FILES[${#_AP_FILES[@]}]=$f; _AP_BAKS[${#_AP_BAKS[@]}]=$bak
        if bash -n "$bak" 2> /dev/null; then oks[$nfile]=1; else oks[$nfile]=0; fi
        lf_normalize "$f" || { _rollback; return 1; }
    done
    for ((k = 0; k < ${#GUIDE_SET[@]}; k++)); do
        i=${GUIDE_SET[$k]}
        [ -n "${NEWVAL[$i]+x}" ] || continue
        target_set "${M_NAME[$i]}" "${NEWVAL[$i]}" || { rc=$?; err "적용 실패: ${M_NAME[$i]} (코드 $rc) — 원상복구합니다"; _rollback; return 1; }
        napplied=$((napplied + 1))
    done
    for ((nfile = 0; nfile < ${#files[@]}; nfile++)); do
        f=${files[$nfile]}; bak=${_AP_BAKS[$nfile]}
        list=
        for ((k = 0; k < ${#GUIDE_SET[@]}; k++)); do
            i=${GUIDE_SET[$k]}
            [ -n "${NEWVAL[$i]+x}" ] || continue
            _target_parse "$i"
            [ "$_TF" = "$f" ] && list="$list $i"
        done
        if ! _verify_file "$f" "$bak" "$list"; then
            err "검증 실패 — 원상복구합니다"; _rollback; return 1
        fi
        i=${list# }; _target_parse "${i%% *}"
        ok_sh=${oks[$nfile]}
        if [ "$_TT" = sh ] && [ "$ok_sh" = 1 ] && ! bash -n "$f" 2> /dev/null; then
            err "문법 검사(bash -n) 실패: $f — 원상복구합니다"; _rollback; return 1
        fi
    done
    ok "${napplied}개 변수 적용 완료"
    for ((nfile = 0; nfile < ${#files[@]}; nfile++)); do
        info "백업: ${_AP_BAKS[$nfile]}"
    done
    info "되돌리려면 --undo (가장 최근 백업으로 복원)"
    return 0
}

run_guide() {   # run_guide <role>   rc: 0 정상(적용 또는 변경 없음), 1 중단/실패(복구됨), 2 manifest 없음
    local role=${1-} i k j id pass progress dn n s old
    local xor_seen=" "
    local -a dep=() depdone=() act=()
    guide_reset
    if [ "$M_COUNT" -le 0 ]; then err "변수 목록(manifest)이 비어 있습니다"; return 2; fi
    for ((i = 0; i < M_COUNT; i++)); do
        if [ -n "$role" ] && ! _role_match "$i" "$role"; then continue; fi
        _only_match "${M_NAME[$i]}" || continue
        GUIDE_CAND[${#GUIDE_CAND[@]}]=$i
    done
    if [ ${#GUIDE_CAND[@]} -eq 0 ]; then info "질문할 변수가 없습니다 (역할: ${role:-전체})"; return 0; fi
    # 1) 택일 그룹은 상황 질문으로 하나만
    for ((k = 0; k < ${#GUIDE_CAND[@]}; k++)); do
        i=${GUIDE_CAND[$k]}
        case "${M_GROUP[$i]}" in
            xor:*)
                id=${M_GROUP[$i]#xor:}
                case "$xor_seen" in *" $id "*) ;; *)
                    xor_seen="$xor_seen$id "
                    _xor_choose "$id" || return 1 ;;
                esac ;;
        esac
    done
    # 2) 의존 없는 변수 질문 (의존 변수는 나중에)
    for ((k = 0; k < ${#GUIDE_CAND[@]}; k++)); do
        i=${GUIDE_CAND[$k]}
        [ -n "${XOR_OFF[$i]+x}" ] && continue
        case "${M_GROUP[$i]}" in
            dep:*) dep[${#dep[@]}]=$i; continue ;;
        esac
        GUIDE_SET[${#GUIDE_SET[@]}]=$i
        ask_var "$i"; s=$?
        [ $s -eq 1 ] && return 1
    done
    # 3) 의존 변수: 의존 대상이 채워진 경우에만 질문 (더 이상 진전이 없을 때까지)
    progress=1
    while [ $progress -eq 1 ] && [ ${#dep[@]} -gt 0 ]; do
        progress=0
        for ((k = 0; k < ${#dep[@]}; k++)); do
            i=${dep[$k]}
            [ -n "${depdone[$k]+x}" ] && continue
            dn=${M_GROUP[$i]#dep:}
            _idx_of "$dn" || continue
            [ -n "$(_effective "$_IDX")" ] || continue
            depdone[$k]=1; progress=1
            GUIDE_SET[${#GUIDE_SET[@]}]=$i
            ask_var "$i"; s=$?
            [ $s -eq 1 ] && return 1
        done
    done
    for ((k = 0; k < ${#dep[@]}; k++)); do
        [ -n "${depdone[$k]+x}" ] && continue
        info "${M_NAME[${dep[$k]}]} 는 ${M_GROUP[${dep[$k]}]#dep:} 가 비어 있어 질문하지 않았습니다"
    done
    if [ ${#GUIDE_SET[@]} -eq 0 ]; then info "질문 대상이 없습니다"; return 0; fi
    GUIDE_SET=($(printf '%s\n' "${GUIDE_SET[@]}" | sort -n))
    # 4) 점검 표 → 이상 변수만 재입력/건너뛰기
    if ! summary_table; then
        for ((k = 0; k < ${#SUMMARY_BAD[@]}; k++)); do
            ask_var "${SUMMARY_BAD[$k]}"; s=$?
            [ $s -eq 1 ] && return 1
        done
        summary_table
    fi
    # 5) 변경 요약 y/n → 적용
    n=0
    printf '\n%s== 변경 요약 ==%s\n' "$C_BOLD" "$C_RST"
    for ((k = 0; k < ${#GUIDE_SET[@]}; k++)); do
        i=${GUIDE_SET[$k]}
        [ -n "${NEWVAL[$i]+x}" ] || continue
        n=$((n + 1))
        old=$(target_get "${M_NAME[$i]}") || old=
        printf '    %s%s%s: %s -> %s   (%s)\n' "$C_BOLD" "${M_NAME[$i]}" "$C_RST" \
            "$(_mask "${M_KIND[$i]}" "$old")" "$(_mask "${M_KIND[$i]}" "${NEWVAL[$i]}")" "${M_TARGET[$i]}"
    done
    if [ $n -eq 0 ]; then info "변경할 값이 없습니다 (모두 현재값 유지)"; return 0; fi
    if ! confirm "위 ${n}개 변경을 적용하시겠습니까?"; then
        warn "취소했습니다 — 파일은 변경되지 않았습니다"
        return 1
    fi
    _apply_changes
}

# ---------------------------------------------------------------- 초기화
ui_init
if [ -z "$(trap -p EXIT)" ]; then trap lib_cleanup EXIT; fi
# ===== lib_common.sh 끝 =====

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
#__END_OF_GUIDE__
