#!/bin/bash
# update_v0.6.1.sh - awx_script v0.6.1 업데이트 스크립트 (make_update.sh 로 자동 생성, 2026-10-09, 0.6.0 변경 포함 누적)
# 대상 파일(@file): 01.AWX_nodeinfo_V2.sh, 02.source_dhcp_pxe.sh
# 사용: bash update_v0.6.1.sh [<대상 파일>...] [--dir <루트>] [--undo] [--dry] [--yes] [--role <역할>]
#   기본 동작: 현장 사본의 마스킹(벤더 문자열·경로·주석·코멘트)을 건드리지 않고 앵커 기반으로 변경만 적용한다.
# 이 파일은 자체포함 단일 스크립트다 (lib_common.sh + update_engine.sh + manifest + 변경 명세).
# 붙여넣기 확인: 마지막 줄이 #__END_OF_UPDATE__ 이고 전체 줄 수가 아래 값(001882)이어야 한다 — 아니면 실행하지 않는다.
set -u
GUS_EXPECT_LINES=001882
_gus_selfcheck() {
    local n l
    if [ ! -f "$0" ]; then
        printf '[X] 파일로 저장한 뒤 bash <파일> 로 실행하십시오 (표준입력·프로세스 치환으로는 실행할 수 없음)\n' >&2; return 1
    fi
    n=$(awk 'END { print NR }' "$0")
    l=$(tail -n 1 "$0" | tr -d '\r')
    if [ "$l" != '#__END_OF_UPDATE__' ] || [ "$n" != "$((10#$GUS_EXPECT_LINES))" ]; then
        printf '[X] 스크립트가 잘렸거나 변형되었습니다 (줄 수 %s, 기대 %s, 마지막 줄 [%s]) — 다시 복사해 주십시오. 대상 파일은 변경하지 않았습니다.\n' "$n" "$((10#$GUS_EXPECT_LINES))" "${l:0:30}" >&2
        return 1
    fi
    return 0
}
_gus_selfcheck || exit 2
unset -f _gus_selfcheck
#!/bin/bash
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
#!/bin/bash
# update_engine.sh - 앵커 기반 변경 엔진 (update_v<ver>.sh 생성물에 lib_common.sh 바로 다음에 인라인)
#
# 의존: lib_common.sh 가 먼저 source/인라인되어 있어야 한다
#       (ok/warn/err/info, backup_file, restore_latest, manifest_load, guard_hit, run_guide, GUIDE_ONLY,
#        _awk_target, _target_parse, _trim_into, confirm).
# 제약: bash 4.1 + sed/awk/grep/diff/coreutils. 빈 배열을 "${a[@]}" 로 펼치지 않는다 (set -u + bash<4.4).
#
# ---------------------------------------------------------------- 입력
# engine_main [<대상 파일>...] [--dir <루트>] [--undo] [--dry] [--yes] [--role <역할>]
#   manifest : ENGINE_MANIFEST(문자열) > ENGINE_MANIFEST_FILE(파일) > ENGINE_SELF 의 MANIFEST 블록
#   spec     : ENGINE_SPEC(문자열)     > ENGINE_SPEC_FILE(파일)     > ENGINE_SELF 의 SPEC 블록
#   ENGINE_SELF          블록을 꺼낼 스크립트 파일 (기본 "$0" = 생성된 update_v<ver>.sh 자신)
#   ENGINE_VERSION       이 업데이트의 버전 (기본: manifest 의 #@meta version) — since 비교에 사용
#   ENGINE_JOURNAL       --undo 저널 경로 (기본: 첫 @file 대상 파일의 디렉터리/.update_journal)
#   ENGINE_OLD_MANIFEST  현장에 있던 이전 manifest (기본: <루트>/vars.manifest, <루트>/setup/, <루트>/conf/,
#                        스크립트 디렉터리 순으로 탐색) — "manifest 에서 빠진 변수" 경고에만 쓴다
#   --dir 기본값: 스크립트 디렉터리의 상위(..) — setup_guide.sh 와 같다.
#   <대상 파일>: @file 경로 대신 쓸 현장 사본. basename 이 같은 @file 에 대응 (@file 이 1개면 이름 무관).
#   종료코드: 0 정상(변경 없음·--dry 포함), 1 중단/실패(대상 파일 불변 또는 원상복구됨), 2 인자·명세·파일 오류
#
# ---------------------------------------------------------------- 생성 스크립트 안의 블록 형식
# engine_emit_block <NAME> <파일> 이 아래처럼 출력하고, engine_extract_block <스크립트> <NAME> 이 sed 로 꺼낸다.
#   : <<'#__MANIFEST_END__'
#   #__MANIFEST_BEGIN__
#   ...원문 그대로...
#   #__MANIFEST_END__
# (SPEC 도 같은 형식). 따옴표 붙은 here-doc 이라 실행되지 않는다. 각 마커 줄은 정확히 1개여야 한다(잘림 감지).
# 스크립트 순서: 헤더 → lib_common.sh → 이 엔진 → MANIFEST 블록 → SPEC 블록 → `engine_main "$@"; exit $?`
#
# ---------------------------------------------------------------- 변경 명세 (SPEC)
#   @file <상대경로|절대경로>      (상대경로는 --dir 기준)
#   @change <id> <op>              op: insert_after | insert_before | replace_line | replace_range
#   @anchor <ERE>                  grep -E 정규식. 코드 줄(빈 줄·# 주석 줄 제외, 1번째 줄 #! 는 코드로 침)과
#                                  정확히 1곳 일치해야 한다. 앞뒤 공백은 잘린다 (공백이 필요하면 [ ] 사용).
#   @anchor_end <ERE>              replace_range 만: 앵커 이후 첫 번째로 일치하는 코드 줄이 범위 끝
#   @text                          이 줄부터 @endtext 앞까지 원문 그대로(주석 줄 포함). 비어 있으면 안 된다.
#   @endtext
#   @text 밖의 빈 줄과 # 로 시작하는 줄(#candidates 등)은 무시. 줄 삭제는 replace_range 의 @text 에
#   남길 앵커 줄을 그대로 넣어 표현한다.
#
# ---------------------------------------------------------------- 동작 요약
#   1) 대상 파일을 CR 제거·끝 줄바꿈 보정한 작업본으로 복사, 모든 변경·변수 처리는 작업본에서 한다.
#   2) 변경마다: 앵커 유일성 → 멱등 검사(@text 전체가 앵커 자리에 이미 있으면 "이미 적용됨";
#      manifest 변수 선언 줄은 값과 무관하게 비교) → replace_* 로 사라지는 원본 줄 검사
#      (주석 줄·가드 키워드·바뀌는 줄 끝 코멘트면 중단, 단 @text 에 같은 줄이 그대로 있으면 허용).
#   3) 변수: 기존 값 유지(업데이트가 선언 줄을 바꿔도 이전 값 복원) → renamed_from 값 이전 →
#      새로 추가된 변수(원본에 선언이 없던 것, 또는 값이 비고 since == 이 버전)만 run_guide 로 질문.
#   4) 원본↔작업본 diff(변수 값 차이 제외)로 사라진 원본 줄 전체를 다시 검사, sh 대상은 bash -n.
#   5) 확인 → 백업(.bak.<ts>) + 저널 → 쓰기(cat >) → 가이드 → 최종 diff 재검증. 실패하면 백업으로 복구.

# ---------------------------------------------------------------- 전역
_E_TD=
_E_WRITTEN=0
_E_DONE=0
_E_VERSION=
_E_JOURNAL=
_E_SECNAMES=
_E_SECVALS=
_E_WHY=
_E_RMCNT=0
_E_BADCNT=0
_E_NAPPLIED=0
_E_NSKIP=0
_E_NEWVARS=
S_N=0; S_FILE=(); S_ID=(); S_OP=(); S_ANC=(); S_AEND=(); S_TXT=(); S_HAST=(); S_FK=()
F_N=0; F_REL=(); F_PATH=(); F_ORIG=(); F_W=(); F_WN=(); F_SPEC=(); F_BAK=(); F_SH=(); F_CR=(); F_OV=()
V_F=(); V_OLD=(); V_OLDF=(); V_RNOLD=(); V_RNF=()

# 줄 끝 코멘트 추출 (따옴표·역슬래시 고려, 공백/;/(/&/| 뒤의 # 부터). 바이트 단위(LC_ALL=C)로 실행.
_E_TC_AWK='
function tc(s,   i, n, c, q, p) {
    n = length(s); q = ""
    for (i = 1; i <= n; i++) {
        c = substr(s, i, 1)
        if (q == "\047") { if (c == q) q = ""; continue }
        if (c == "\\") { i++; continue }
        if (q == "\"") { if (c == "\"") q = ""; continue }
        if (c == "\047" || c == "\"") { q = c; continue }
        if (c == "#") {
            p = (i == 1) ? " " : substr(s, i - 1, 1)
            if (p == " " || p == "\t" || p == ";" || p == "(" || p == "&" || p == "|") {
                c = substr(s, i); sub(/[ \t]+$/, "", c); return c
            }
        }
    }
    return ""
}
{ sub(/\r$/, ""); print tc($0) }'

# 멱등 비교용 정규화: 끝 공백 제거, manifest 변수 선언 줄은 값만 버린다 ("변수명=" + 값 뒤 나머지).
# ENVIRON[GUS_VN] = "sh:이름 conf:이름 ..."
_E_NORM_AWK='
function skipval(r,   c, i, n, ch) {
    c = substr(r, 1, 1); n = length(r)
    if (c == "\"") {
        for (i = 2; i <= n; i++) { ch = substr(r, i, 1); if (ch == "\\") { i++; continue } if (ch == "\"") return substr(r, i + 1) }
        return ""
    }
    if (c == "\047") { i = index(substr(r, 2), "\047"); return i ? substr(r, i + 2) : "" }
    if (match(r, /[ \t;]/)) return substr(r, RSTART)
    return ""
}
function norm(s,   i, re) {
    sub(/\r$/, "", s); sub(/[ \t]+$/, "", s)
    for (i = 1; i <= nv; i++) {
        if (vt[i] == "sh") re = "^[ \t]*(export[ \t]+)?" vn[i] "="
        else re = "^[ \t]*" vn[i] "[ \t]*="
        if (match(s, re)) return "\001" vn[i] "=" (vt[i] == "sh" ? skipval(substr(s, RLENGTH + 1)) : "")
    }
    return s
}
BEGIN {
    nv = split(ENVIRON["GUS_VN"], va, " ")
    for (i = 1; i <= nv; i++) { vt[i] = va[i]; sub(/:.*/, "", vt[i]); vn[i] = va[i]; sub(/^[^:]*:/, "", vn[i]) }
}'

# 화면 출력 마스킹: secret 변수 선언 줄의 값, 그리고 알려진 secret 값 문자열을 **** 로.
_E_MASK_AWK='
BEGIN { nn = split(ENVIRON["GUS_SN"], sn, " "); nv = split(ENVIRON["GUS_SV"], sv, "\n") }
{
    line = $0
    for (i = 1; i <= nn; i++)
        if (match(line, "(^|[^A-Za-z0-9_])" sn[i] "[ \t]*=")) { line = substr(line, 1, RSTART + RLENGTH - 1) "****"; break }
    for (i = 1; i <= nv; i++) {
        v = sv[i]; if (v == "") continue
        out = ""
        while ((p = index(line, v)) > 0) { out = out substr(line, 1, p - 1) "****"; line = substr(line, p + length(v)) }
        line = out line
    }
    print line
}'

# ---------------------------------------------------------------- 블록 삽입/추출
engine_emit_block() {   # engine_emit_block <NAME> <파일> : 생성기용, here-doc 으로 감싼 블록을 stdout 에
    local n=$1 f=$2
    [ -f "$f" ] || { err "블록으로 넣을 파일 없음: $f" >&2; return 2; }
    if tr -d '\r' < "$f" | grep -aqE "^#__${n}_(BEGIN|END)__\$"; then
        err "블록 내용에 마커 줄이 들어 있음: $f" >&2; return 1
    fi
    printf ": <<'#__%s_END__'\n#__%s_BEGIN__\n" "$n" "$n"
    tr -d '\r' < "$f" | LC_ALL=C awk 1
    printf '#__%s_END__\n' "$n"
}

engine_extract_block() {   # engine_extract_block <스크립트> <NAME> : 마커 사이 원문을 stdout 에 (rc 1 = 마커 오류)
    local f=$1 n=$2 b e
    [ -f "$f" ] || { err "블록을 읽을 파일 없음: $f" >&2; return 2; }
    b=$(tr -d '\r' < "$f" | grep -anx -- "#__${n}_BEGIN__" | sed 's/:.*//' | tr '\n' ' ')
    e=$(tr -d '\r' < "$f" | grep -anx -- "#__${n}_END__" | sed 's/:.*//' | tr '\n' ' ')
    b=${b% }; e=${e% }
    case "$b$e" in *' '*) err "${n} 블록 마커가 여러 개입니다: $f" >&2; return 1 ;; esac
    if [ -z "$b" ] || [ -z "$e" ] || [ "$e" -le "$b" ]; then
        err "${n} 블록 마커(#__${n}_BEGIN__/#__${n}_END__)를 찾지 못함 — 붙여넣기 중 잘렸는지 확인: $f" >&2
        return 1
    fi
    tr -d '\r' < "$f" | sed -n "/^#__${n}_BEGIN__\$/,/^#__${n}_END__\$/p" | sed '1d;$d'
}

# ---------------------------------------------------------------- 정리·중단
_eng_cleanup() {
    [ -n "$_E_TD" ] && [ -d "$_E_TD" ] && rm -rf "$_E_TD"
    _E_TD=
    lib_cleanup
}

_eng_restore_all() {
    local k
    for ((k = 0; k < F_N; k++)); do
        [ -n "${F_BAK[$k]}" ] || continue
        if cat "${F_BAK[$k]}" > "${F_PATH[$k]}"; then
            warn "원상복구: ${F_PATH[$k]}  <-  ${F_BAK[$k]}"
        else
            err "원상복구 실패: ${F_PATH[$k]} (백업: ${F_BAK[$k]})"
        fi
    done
}

_eng_interrupt() {
    trap - INT TERM
    printf '\n'
    if [ "$_E_WRITTEN" = 1 ] && [ "$_E_DONE" != 1 ]; then
        err "중단됨 — 이번 실행의 변경을 원상복구합니다"
        _eng_restore_all
    else
        err "중단됨 — 대상 파일은 변경되지 않았습니다"
    fi
    _eng_cleanup
    exit 1
}

# ---------------------------------------------------------------- 도구
_mask_out() { GUS_SN=$_E_SECNAMES GUS_SV=$_E_SECVALS LC_ALL=C awk "$_E_MASK_AWK"; }

_add_secval() { [ -n "$1" ] && _E_SECVALS="$_E_SECVALS$1"$'\n'; return 0; }

_tc() { printf '%s\n' "$1" | LC_ALL=C awk "$_E_TC_AWK"; }

_abs_path() {   # 디렉터리가 있으면 절대경로, 없으면 그대로
    local p=$1 d b
    case "$p" in /*) ;; *) p="$PWD/$p" ;; esac
    d=${p%/*}; b=${p##*/}
    [ -n "$d" ] || d=/
    if d=$(cd "$d" 2> /dev/null && pwd); then printf '%s/%s\n' "${d%/}" "$b"; else printf '%s\n' "$p"; fi
}

_rel_norm() { local r=$1; while [ "${r#./}" != "$r" ]; do r=${r#./}; done; printf '%s' "$r"; }

_vtype() { printf '%s' "${M_TARGET[$1]%%:*}"; }

_vn_for() {   # 파일 k 를 대상으로 하는 manifest 변수 "sh:이름 conf:이름 ..."
    local k=$1 i out=
    for ((i = 0; i < M_COUNT; i++)); do
        [ "${V_F[$i]-}" = "$k" ] && out="$out ${M_TARGET[$i]%%:*}:${M_NAME[$i]}"
    done
    printf '%s' "${out# }"
}

_anchor_lines() {   # _anchor_lines <ERE> <파일> : 일치하는 코드 줄 번호
    grep -anE -- "$1" "$2" | LC_ALL=C awk '{
        p = index($0, ":"); n = substr($0, 1, p - 1); t = substr($0, p + 1)
        if (t ~ /^[ \t]*$/) next
        if (t ~ /^[ \t]*#/ && !(n == 1 && t ~ /^#!/)) next
        print n }'
}

_blk_find() {   # _blk_find <text> <work> <vn> : @text 블록(정규화)이 나타나는 시작 줄 번호들
    GUS_VN=$3 LC_ALL=C awk "$_E_NORM_AWK"'
        NR == FNR { t[++n] = norm($0); next }
        { w[++m] = norm($0) }
        END {
            for (s = 1; s + n - 1 <= m; s++) {
                ok = 1
                for (j = 1; j <= n; j++) if (w[s + j - 1] != t[j]) { ok = 0; break }
                if (ok) print s
            }
        }' "$1" "$2"
}

_splice() {   # _splice <파일> <시작줄> <지울 줄 수> <넣을 파일>
    local f=$1 s=$2 r=$3 ins=$4 t="$_E_TD/splice.tmp"
    { head -n $((s - 1)) "$f"; cat "$ins"; tail -n +$((s + r)) "$f"; } > "$t" && cat "$t" > "$f"
}

_orig_no() {   # 작업본 줄 → 원본 줄 번호 (새로 넣은 줄은 +)
    sed -n "${2}p" "$1"
}

_val_norm() {   # _val_norm <파일> <vn> : manifest 변수 값만 비운 사본을 stdout 에 (diff 비교용)
    local f=$1 x t1="$_E_TD/vn.1" t2="$_E_TD/vn.2"
    LC_ALL=C tr -d '\r' < "$f" | LC_ALL=C awk 1 > "$t1"
    for x in $2; do
        _awk_target set "${x%%:*}" "${x#*:}" "$t1" "" > "$t2"
        cat "$t2" > "$t1"
    done
    cat "$t1"
}

_line_ok() {   # _line_ok <사라지는 줄> <추가된 줄 파일|""> <추가 줄 코멘트 파일|""> : 허용이면 rc 0, 아니면 _E_WHY
    local L=$1 c
    if [ -n "$2" ] && grep -aqxF -- "$L" "$2"; then return 0; fi
    if guard_hit "$L"; then _E_WHY="주석 줄 또는 가드 키워드"; return 1; fi
    c=$(_tc "$L")
    if [ -n "$c" ] && { [ -z "$3" ] || ! grep -aqxF -- "$c" "$3"; }; then
        _E_WHY="줄 끝 코멘트가 바뀜"; return 1
    fi
    return 0
}

_diff_check() {   # _diff_check <원본> <결과> <vn> <표시명> : 사라진 원본 줄 전부 검사 → _E_RMCNT _E_BADCNT
    local o=$1 nw=$2 vn=$3 label=$4 line ln txt
    local no="$_E_TD/dc.o" nn="$_E_TD/dc.n" add="$_E_TD/dc.add" addtc="$_E_TD/dc.addtc"
    _E_RMCNT=0; _E_BADCNT=0
    _val_norm "$o" "$vn" > "$no"
    _val_norm "$nw" "$vn" > "$nn"
    diff --old-line-format='' --new-line-format='%L' --unchanged-line-format='' "$no" "$nn" > "$add"
    LC_ALL=C awk "$_E_TC_AWK" "$add" > "$addtc"
    while IFS= read -r line || [ -n "$line" ]; do
        ln=${line%%|*}; txt=${line#*|}
        _E_RMCNT=$((_E_RMCNT + 1))
        _line_ok "$txt" "$add" "$addtc" && continue
        _E_BADCNT=$((_E_BADCNT + 1))
        err "마스킹 위반($_E_WHY): $label 원본 ${ln}번째 줄"
        printf '    %s\n' "$txt" | _mask_out
    done < <(diff --old-line-format='%dn|%L' --new-line-format='' --unchanged-line-format='' "$no" "$nn")
    [ "$_E_BADCNT" -eq 0 ]
}

_is_sh() {   # _is_sh <경로> <작업본>
    case "$1" in *.sh|*.bash) return 0 ;; esac
    head -n 1 "$2" | grep -aqE '^#!.*(/|env[[:space:]]+)(ba|k|z|da)?sh([[:space:]]|$)'
}

# ---------------------------------------------------------------- 명세 파서
_spec_parse() {   # stdin
    local line t n=0 cur= intext=0 k=-1 id op x rest bad=0 c j
    S_N=0; S_FILE=(); S_ID=(); S_OP=(); S_ANC=(); S_AEND=(); S_TXT=(); S_HAST=(); S_FK=()
    while IFS= read -r line || [ -n "$line" ]; do
        n=$((n + 1))
        line=${line%$'\r'}
        if [ $intext = 1 ]; then
            _trim_into t "$line"
            if [ "$t" = "@endtext" ]; then intext=0; continue; fi
            printf '%s\n' "$line" >> "${S_TXT[$k]}"
            continue
        fi
        _trim_into t "$line"
        case "$t" in
            ''|'#'*) continue ;;
            '@file '*)
                rest=${t#@file }; _trim_into cur "$rest"
                [ -n "$cur" ] || { err "명세 ${n}줄: @file 경로가 비어 있음"; bad=1; } ;;
            '@change '*)
                rest=${t#@change }
                id=; op=; x=
                read -r id op x <<< "$rest"
                if [ -z "$cur" ]; then err "명세 ${n}줄: @change 앞에 @file 이 없음"; bad=1; continue; fi
                if [ -z "$op" ] || [ -n "$x" ]; then err "명세 ${n}줄: 형식은 @change <id> <op>"; bad=1; continue; fi
                k=$S_N
                S_FILE[$k]=$cur; S_ID[$k]=$id; S_OP[$k]=$op; S_ANC[$k]=; S_AEND[$k]=
                S_TXT[$k]="$_E_TD/txt.$k"; S_HAST[$k]=0; S_FK[$k]=-1
                : > "${S_TXT[$k]}"
                S_N=$((k + 1)) ;;
            '@anchor '*|'@anchor_end '*)
                if [ $k -lt 0 ]; then err "명세 ${n}줄: @change 앞에 ${t%% *}"; bad=1; continue; fi
                rest=${t#* }; _trim_into rest "$rest"
                if [ "${t%% *}" = @anchor ]; then S_ANC[$k]=$rest; else S_AEND[$k]=$rest; fi ;;
            '@text')
                if [ $k -lt 0 ]; then err "명세 ${n}줄: @change 앞에 @text"; bad=1; continue; fi
                if [ "${S_HAST[$k]}" = 1 ]; then err "명세 ${n}줄: [${S_ID[$k]}] @text 가 두 번"; bad=1; fi
                S_HAST[$k]=1; : > "${S_TXT[$k]}"; intext=1 ;;
            *) err "명세 ${n}줄: 알 수 없는 줄: $t"; bad=1 ;;
        esac
    done
    [ $intext = 1 ] && { err "명세: @endtext 가 없음 (잘림?)"; bad=1; }
    [ $S_N -gt 0 ] || { err "명세에 @change 가 없음"; bad=1; }
    for ((c = 0; c < S_N; c++)); do
        id=${S_ID[$c]}
        if ! [[ $id =~ ^[A-Za-z0-9_.:-]+$ ]]; then err "명세: change id 형식 오류: $id"; bad=1; fi
        for ((j = 0; j < c; j++)); do [ "${S_ID[$j]}" = "$id" ] && { err "명세: change id 중복: $id"; bad=1; }; done
        case "${S_OP[$c]}" in
            insert_after|insert_before|replace_line|replace_range) ;;
            *) err "명세 [$id]: 알 수 없는 op: ${S_OP[$c]}"; bad=1 ;;
        esac
        [ -n "${S_ANC[$c]}" ] || { err "명세 [$id]: @anchor 없음"; bad=1; }
        if [ "${S_OP[$c]}" = replace_range ]; then
            [ -n "${S_AEND[$c]}" ] || { err "명세 [$id]: replace_range 는 @anchor_end 필요"; bad=1; }
        elif [ -n "${S_AEND[$c]}" ]; then
            err "명세 [$id]: @anchor_end 는 replace_range 에만 쓴다"; bad=1
        fi
        for x in "${S_ANC[$c]}" "${S_AEND[$c]}"; do
            [ -n "$x" ] || continue
            printf '' | grep -Eq -- "$x" 2> /dev/null
            [ $? -eq 2 ] && { err "명세 [$id]: 정규식 오류: $x"; bad=1; }
        done
        if [ "${S_HAST[$c]}" != 1 ] || [ ! -s "${S_TXT[$c]}" ]; then
            err "명세 [$id]: @text 가 없거나 비어 있음"; bad=1
        fi
    done
    [ $bad -eq 0 ]
}

# ---------------------------------------------------------------- 입력 읽기
_eng_load_inputs() {
    local self=${ENGINE_SELF:-$0} mtxt stxt
    if [ -n "${ENGINE_MANIFEST+x}" ]; then
        mtxt=$ENGINE_MANIFEST
    elif [ -n "${ENGINE_MANIFEST_FILE:-}" ]; then
        [ -f "$ENGINE_MANIFEST_FILE" ] || { err "manifest 파일 없음: $ENGINE_MANIFEST_FILE"; return 2; }
        mtxt=$(cat "$ENGINE_MANIFEST_FILE")
    else
        mtxt=$(engine_extract_block "$self" MANIFEST 2>&1) || { printf '%s\n' "$mtxt"; return 2; }
    fi
    if [ -n "${ENGINE_SPEC+x}" ]; then
        stxt=$ENGINE_SPEC
    elif [ -n "${ENGINE_SPEC_FILE:-}" ]; then
        [ -f "$ENGINE_SPEC_FILE" ] || { err "명세 파일 없음: $ENGINE_SPEC_FILE"; return 2; }
        stxt=$(cat "$ENGINE_SPEC_FILE")
    else
        stxt=$(engine_extract_block "$self" SPEC 2>&1) || { printf '%s\n' "$stxt"; return 2; }
    fi
    manifest_load - <<< "$mtxt" || { err "manifest 오류 — 중단"; return 2; }
    _spec_parse <<< "$stxt" || { err "변경 명세 오류 — 중단"; return 2; }
    _E_VERSION=${ENGINE_VERSION:-${META[version]-}}
    local i
    _E_SECNAMES=
    for ((i = 0; i < M_COUNT; i++)); do
        [ "${M_KIND[$i]}" = secret ] || continue
        _E_SECNAMES="$_E_SECNAMES ${M_NAME[$i]}"
        [ "${M_RENAMED[$i]}" != - ] && _E_SECNAMES="$_E_SECNAMES ${M_RENAMED[$i]}"
    done
    _E_SECNAMES=${_E_SECNAMES# }
    return 0
}

# ---------------------------------------------------------------- 파일 결정
_file_add() {   # _file_add <rel> <path> <spec 0|1> -> _FK
    local k=$F_N
    F_REL[$k]=$1; F_PATH[$k]=$2; F_SPEC[$k]=$3
    F_ORIG[$k]=; F_W[$k]=; F_WN[$k]=; F_BAK[$k]=; F_SH[$k]=0; F_CR[$k]=0; F_OV[$k]=0
    F_N=$((k + 1)); _FK=$k
}

_eng_resolve_files() {   # _eng_resolve_files <pos 개수> <pos...>   (_ENG_DRY 사용)
    local np=$1 c k j p rel path m hit i t tf
    shift
    local -a U=() OV=() pos=()
    for ((j = 0; j < np; j++)); do pos[$j]=$1; shift; done
    F_N=0; F_REL=(); F_PATH=(); F_ORIG=(); F_W=(); F_WN=(); F_SPEC=(); F_BAK=(); F_SH=(); F_CR=(); F_OV=()
    for ((c = 0; c < S_N; c++)); do
        rel=$(_rel_norm "${S_FILE[$c]}"); hit=0
        for ((j = 0; j < ${#U[@]}; j++)); do [ "${U[$j]}" = "$rel" ] && hit=1; done
        [ $hit = 1 ] || { U[${#U[@]}]=$rel; OV[${#OV[@]}]=; }
    done
    for ((j = 0; j < np; j++)); do
        p=${pos[$j]}; m=-1; hit=0
        for ((k = 0; k < ${#U[@]}; k++)); do
            if [ "${U[$k]##*/}" = "${p##*/}" ]; then hit=$((hit + 1)); m=$k; fi
        done
        if [ $hit -eq 0 ] && [ $np -eq 1 ] && [ ${#U[@]} -eq 1 ]; then m=0; hit=1; fi
        if [ $hit -ne 1 ] || [ -n "${OV[$m]}" ]; then
            err "대상 파일을 명세의 @file 에 대응시키지 못함: $p (명세 파일: ${U[*]})"; return 2
        fi
        OV[$m]=$p
    done
    for ((j = 0; j < ${#U[@]}; j++)); do
        rel=${U[$j]}
        if [ -n "${OV[$j]}" ]; then path=${OV[$j]}
        else case "$rel" in /*) path=$rel ;; *) path="$PROJECT_DIR/$rel" ;; esac; fi
        path=$(_abs_path "$path")
        if [ ! -f "$path" ]; then err "대상 파일 없음: $path  (--dir 또는 대상 파일 인자를 확인)"; return 2; fi
        if [ ! -r "$path" ] || { [ "${_ENG_DRY:-0}" != 1 ] && [ ! -w "$path" ]; }; then
            err "대상 파일 읽기/쓰기 권한 없음: $path"; return 2
        fi
        _file_add "$rel" "$path" 1
        [ -n "${OV[$j]}" ] && F_OV[$_FK]=1
    done
    for ((c = 0; c < S_N; c++)); do
        rel=$(_rel_norm "${S_FILE[$c]}")
        for ((k = 0; k < F_N; k++)); do [ "${F_REL[$k]}" = "$rel" ] && S_FK[$c]=$k; done
    done
    # manifest 변수 → 파일
    V_F=()
    for ((i = 0; i < M_COUNT; i++)); do
        V_F[$i]=-1
        t=${M_TARGET[$i]%%:*}
        rel=$(_rel_norm "${M_TARGET[$i]#*:}")
        for ((k = 0; k < F_N; k++)); do [ "${F_REL[$k]}" = "$rel" ] && V_F[$i]=$k; done
        if [ "${V_F[$i]}" = -1 ]; then
            _target_parse "$i"; tf=$(_abs_path "$_TF")
            for ((k = 0; k < F_N; k++)); do [ "${F_PATH[$k]}" = "$tf" ] && V_F[$i]=$k; done
            if [ "${V_F[$i]}" = -1 ] && [ -f "$tf" ]; then
                _file_add "$rel" "$tf" 0; V_F[$i]=$_FK
            fi
        fi
        k=${V_F[$i]}
        if [ "$k" != -1 ] && [ "${F_OV[$k]}" = 1 ]; then M_TARGET[$i]="$t:${F_PATH[$k]}"; fi
    done
    _E_JOURNAL=${ENGINE_JOURNAL:-${F_PATH[0]%/*}/.update_journal}
    return 0
}

# ---------------------------------------------------------------- 작업본 준비·변수 스냅샷
_eng_prepare() {
    local k i o w x v
    for ((k = 0; k < F_N; k++)); do
        o="$_E_TD/orig.$k"; w="$_E_TD/work.$k"
        LC_ALL=C tr -d '\r' < "${F_PATH[$k]}" | LC_ALL=C awk 1 > "$o"
        grep -q $'\r' "${F_PATH[$k]}" && F_CR[$k]=1
        cp "$o" "$w"
        LC_ALL=C awk '{ print NR }' "$o" > "$_E_TD/wn.$k"
        F_ORIG[$k]=$o; F_W[$k]=$w; F_WN[$k]="$_E_TD/wn.$k"
        _is_sh "${F_PATH[$k]}" "$o" && F_SH[$k]=1
        [ "${F_CR[$k]}" = 1 ] && info "CR 제거(LF 로 정규화) 예정: ${F_PATH[$k]}"
    done
    for ((i = 0; i < M_COUNT; i++)); do
        V_OLD[$i]=; V_OLDF[$i]=0; V_RNOLD[$i]=; V_RNF[$i]=0
        k=${V_F[$i]}
        [ "$k" = -1 ] && continue
        if v=$(_awk_target get "$(_vtype "$i")" "${M_NAME[$i]}" "${F_ORIG[$k]}"); then
            V_OLD[$i]=$v; V_OLDF[$i]=1
            [ "${M_KIND[$i]}" = secret ] && _add_secval "$v"
        fi
        x=${M_RENAMED[$i]}
        if [ "$x" != - ] && v=$(_awk_target get "$(_vtype "$i")" "$x" "${F_ORIG[$k]}"); then
            V_RNOLD[$i]=$v; V_RNF[$i]=1
            [ "${M_KIND[$i]}" = secret ] && _add_secval "$v"
        fi
    done
}

# ---------------------------------------------------------------- 변경 1건 적용 (작업본)
_anchor_report() {   # _anchor_report <id> <표시명> <종류> <ERE> <작업본> <wn> <줄 목록>
    local id=$1 label=$2 what=$3 re=$4 w=$5 wn=$6 lines=$7 l cnt=0 nums=
    for l in $lines; do cnt=$((cnt + 1)); nums="$nums $(_orig_no "$wn" "$l")"; done
    err "[$id] ${what} 일치 ${cnt}곳 — 정확히 1곳이어야 합니다 ($label)"
    printf '    %s: %s\n' "$what" "$re"
    if [ $cnt -eq 0 ]; then
        printf '    일치 줄 없음 — 현장 사본에서 이 줄이 수정·삭제되었을 수 있습니다\n'
    else
        printf '    일치 줄 번호(원본 기준, +는 이번에 넣은 줄):%s\n' "$nums"
        for l in $lines; do
            printf '    %s: %s\n' "$(_orig_no "$wn" "$l")" "$(sed -n "${l}p" "$w")"
        done | head -n 10 | _mask_out
    fi
}

_apply_change() {   # _apply_change <c> : rc 0 적용/건너뜀, 1 중단
    local c=$1 k w wn txt n id op label lines cnt=0 a e=0 l starts s hit=0 vn st rm ln L
    local plus="$_E_TD/plus" ttc="$_E_TD/ttc"
    k=${S_FK[$c]}; w=${F_W[$k]}; wn=${F_WN[$k]}; txt=${S_TXT[$c]}
    id=${S_ID[$c]}; op=${S_OP[$c]}; label=${F_PATH[$k]}
    n=$(wc -l < "$txt"); n=$((n + 0))
    vn=$(_vn_for "$k")
    lines=$(_anchor_lines "${S_ANC[$c]}" "$w")
    for l in $lines; do cnt=$((cnt + 1)); a=$l; done
    starts=$(_blk_find "$txt" "$w" "$vn")
    # 멱등 검사
    if [ $cnt -eq 1 ]; then
        for s in $starts; do
            case "$op" in
                insert_after)  [ "$s" -eq $((a + 1)) ] && hit=1 ;;
                insert_before) [ "$s" -eq $((a - n)) ] && hit=1 ;;
                *) [ "$s" -le "$a" ] && [ "$a" -le $((s + n - 1)) ] && hit=1 ;;
            esac
        done
    elif [ $cnt -eq 0 ]; then
        case "$op" in replace_*)
            s=0; for l in $starts; do s=$((s + 1)); done
            [ $s -eq 1 ] && hit=1 ;;
        esac
    fi
    if [ $hit = 1 ]; then
        info "[$id] 이미 적용됨 — 건너뜀 ($op, $label)"
        _E_NSKIP=$((_E_NSKIP + 1))
        return 0
    fi
    if [ $cnt -ne 1 ]; then
        _anchor_report "$id" "$label" 앵커 "${S_ANC[$c]}" "$w" "$wn" "$lines"
        return 1
    fi
    ln=$(_orig_no "$wn" "$a")
    sed 's/.*/+/' "$txt" > "$plus"
    case "$op" in
        insert_after)  st=$((a + 1)); rm=0 ;;
        insert_before) st=$a; rm=0 ;;
        replace_line)  st=$a; rm=1 ;;
        replace_range)
            e=$(_anchor_lines "${S_AEND[$c]}" "$w" | awk -v a="$a" '$1 > a { print; exit }')
            if [ -z "$e" ]; then
                err "[$id] 끝 앵커가 앵커(원본 ${ln}번째 줄) 이후에 없음 ($label)"
                printf '    끝 앵커: %s\n' "${S_AEND[$c]}"
                return 1
            fi
            st=$a; rm=$((e - a + 1)) ;;
    esac
    # replace_* 로 사라지는 원본 줄 검사 (마스킹 가드)
    if [ $rm -gt 0 ]; then
        LC_ALL=C awk "$_E_TC_AWK" "$txt" > "$ttc"
        for ((l = st; l < st + rm; l++)); do
            s=$(_orig_no "$wn" "$l")
            [ "$s" = + ] && continue
            L=$(sed -n "${l}p" "$w")
            if ! _line_ok "$L" "$txt" "$ttc"; then
                err "[$id] 마스킹 위반($_E_WHY): 원본 ${s}번째 줄이 $op 로 사라집니다 — 적용하지 않고 중단 ($label)"
                printf '    %s\n' "$L" | _mask_out
                return 1
            fi
        done
    fi
    _splice "$w" "$st" "$rm" "$txt" && _splice "$wn" "$st" "$rm" "$plus" || { err "[$id] 작업본 편집 실패"; return 1; }
    ok "[$id] $op 반영(작업본) — 원본 ${ln}번째 줄 기준 (+${n}줄, -${rm}줄) ($label)"
    _E_NAPPLIED=$((_E_NAPPLIED + 1))
    return 0
}

# ---------------------------------------------------------------- 변수 처리 (작업본)
_wget() {   # _wget <idx> [이름] : 작업본에서 값 읽기 (rc 2 = 선언 없음)
    local i=$1 nm=${2:-${M_NAME[$1]}}
    _awk_target get "$(_vtype "$i")" "$nm" "${F_W[${V_F[$i]}]}"
}

_wset() {   # _wset <idx> <값>
    local i=$1 v=$2 w t="$_E_TD/wset" got
    w=${F_W[${V_F[$i]}]}
    _awk_target set "$(_vtype "$i")" "${M_NAME[$i]}" "$w" "$v" > "$t" || return 1
    got=$(_awk_target get "$(_vtype "$i")" "${M_NAME[$i]}" "$t") || return 1
    [ "$got" = "$v" ] || return 1
    cat "$t" > "$w"
}

_xor_peer_set() {   # _xor_peer_set <idx> : 같은 택일(xor) 그룹의 다른 변수에 이미 값이 있으면 rc 0 (이 변수는 비워 두는 것이 정상)
    local i=$1 j v g=${M_GROUP[$1]}
    case "$g" in xor:*) ;; *) return 1 ;; esac
    for ((j = 0; j < M_COUNT; j++)); do
        [ "$j" != "$i" ] && [ "${M_GROUP[$j]}" = "$g" ] && [ "${V_F[$j]}" != -1 ] || continue
        v=$(_wget "$j") && [ -n "$v" ] && return 0
    done
    return 1
}

_eng_vars() {   # rc 1 = 중단
    local i k nm kind now rc r rv old_empty=0
    _E_NEWVARS=
    for ((i = 0; i < M_COUNT; i++)); do
        nm=${M_NAME[$i]}; kind=${M_KIND[$i]}; k=${V_F[$i]}
        if [ "$k" = -1 ]; then
            warn "변수 $nm: 대상 파일 없음 (${M_TARGET[$i]}) — 변수 처리 건너뜀"; continue
        fi
        now=$(_wget "$i"); rc=$?
        # ① 기존 값 유지
        if [ "${V_OLDF[$i]}" = 1 ] && [ -n "${V_OLD[$i]}" ]; then
            if [ $rc -ne 0 ]; then
                err "변수 $nm: 업데이트 후 선언 줄이 사라져 기존 값을 보존할 수 없음 — 중단"; return 1
            fi
            if [ "$now" != "${V_OLD[$i]}" ]; then
                _wset "$i" "${V_OLD[$i]}" || { err "변수 $nm: 기존 값 복원 실패 — 중단"; return 1; }
                info "기존 값 유지: $nm = $(_mask "$kind" "${V_OLD[$i]}")" | _mask_out
                now=${V_OLD[$i]}
            fi
        fi
        # ② renamed_from 값 이전
        r=${M_RENAMED[$i]}
        if [ "$r" != - ] && [ "${V_RNF[$i]}" = 1 ] && [ -n "${V_RNOLD[$i]}" ]; then
            rv=${V_RNOLD[$i]}
            if [ $rc -ne 0 ]; then
                warn "변수 $nm: 선언이 없어 이전 이름 $r 의 값을 옮기지 못함"
            elif [ -z "$now" ]; then
                _wset "$i" "$rv" || { err "변수 $nm: $r 값 이전 실패 — 중단"; return 1; }
                ok "값 이전: $r -> $nm ($(_mask "$kind" "$rv"))" | _mask_out
                now=$rv
            elif [ "$now" != "$rv" ]; then
                info "변수 $nm 에 이미 값이 있어 $r 의 값은 옮기지 않음"
            fi
        fi
        # ③ 새 변수만 질문
        if [ $rc -ne 0 ]; then
            warn "변수 $nm: 대상에 선언이 없음 (${M_TARGET[$i]}) — 질문하지 않음"; continue
        fi
        [ -n "$now" ] && continue
        # 택일 그룹의 다른 구성원이 이미 채워져 있으면 이 변수는 비워 두는 것이 정상 — 재실행 때 다시 묻지 않는다
        _xor_peer_set "$i" && continue
        if [ "${V_OLDF[$i]}" = 0 ] || { [ -n "$_E_VERSION" ] && [ "${M_SINCE[$i]}" = "$_E_VERSION" ]; }; then
            _E_NEWVARS="$_E_NEWVARS $nm"
        else
            old_empty=$((old_empty + 1))
        fi
    done
    _E_NEWVARS=${_E_NEWVARS# }
    [ $old_empty -gt 0 ] && info "이전부터 비어 있던 변수 ${old_empty}개는 묻지 않습니다 (setup_guide.sh 로 입력)"
    return 0
}

_eng_removed_vars() {   # 이전 manifest 에만 있는 변수 경고 (소스의 선언·값은 그대로 둔다)
    local f= c nm i x known
    local self=${ENGINE_SELF:-$0}
    if [ -n "${ENGINE_OLD_MANIFEST:-}" ]; then f=$ENGINE_OLD_MANIFEST
    else
        for c in "$PROJECT_DIR/vars.manifest" "$PROJECT_DIR/setup/vars.manifest" "$PROJECT_DIR/conf/vars.manifest" "${self%/*}/vars.manifest"; do
            [ -f "$c" ] && { f=$c; break; }
        done
    fi
    [ -n "$f" ] && [ -f "$f" ] || return 0
    while IFS= read -r nm; do
        known=0
        for ((i = 0; i < M_COUNT; i++)); do
            [ "${M_NAME[$i]}" = "$nm" ] && known=1
            [ "${M_RENAMED[$i]}" = "$nm" ] && known=1
        done
        [ $known = 1 ] || warn "manifest 에서 빠진 변수: $nm — 소스의 선언·값은 그대로 유지합니다 (필요 없으면 직접 정리)"
    done < <(tr -d '\r' < "$f" | awk -F'|' '!/^[ \t]*#/ && NF == 11 { x = $1; gsub(/[ \t]/, "", x); if (x != "") print x }')
}

# ---------------------------------------------------------------- 검사 (작업본)
_eng_check_work() {   # rc 1 = 중단
    local k bad=0 se="$_E_TD/syn.err"
    for ((k = 0; k < F_N; k++)); do
        _diff_check "${F_ORIG[$k]}" "${F_W[$k]}" "$(_vn_for "$k")" "${F_PATH[$k]}" || bad=1
        if [ "${F_SH[$k]}" = 1 ] && ! cmp -s "${F_ORIG[$k]}" "${F_W[$k]}"; then
            if ! bash -n "${F_W[$k]}" 2> "$se"; then
                err "문법 검사(bash -n) 실패 — 적용하지 않고 중단: ${F_PATH[$k]}"
                sed "s|${F_W[$k]}|(작업본)|g" "$se" | head -n 5 | _mask_out | sed 's/^/    /'
                bash -n "${F_ORIG[$k]}" 2> /dev/null || warn "원본도 이미 문법 오류 상태입니다: ${F_PATH[$k]}"
                bad=1
            fi
        fi
    done
    [ $bad -eq 0 ]
}

# ---------------------------------------------------------------- 백업·저널·쓰기
_eng_backup() {   # _eng_backup <k> : 이번 실행에서 처음일 때만 백업 + 저널 기록
    local k=$1 b
    [ -n "${F_BAK[$k]}" ] && return 0
    b=$(backup_file "${F_PATH[$k]}") || return 1
    F_BAK[$k]=$b
    if [ "$_E_JRESET" = 1 ]; then
        _E_JRESET=0
        rm -f "$_E_JOURNAL.undone"
        : > "$_E_JOURNAL" || { err "저널을 쓸 수 없음: $_E_JOURNAL"; return 1; }
    fi
    printf '%s\t%s\t%s\n' "${_E_VERSION:--}" "${F_PATH[$k]}" "$b" >> "$_E_JOURNAL"
}

_eng_final_verify() {   # 백업 ↔ 최종 파일 재검증. rc 1 = 위반
    local k bad=0 tot=0
    _E_FINAL_RM=0
    for ((k = 0; k < F_N; k++)); do
        [ -n "${F_BAK[$k]}" ] || continue
        if ! _diff_check "${F_BAK[$k]}" "${F_PATH[$k]}" "$(_vn_for "$k")" "${F_PATH[$k]}"; then bad=1; fi
        tot=$((tot + _E_RMCNT))
        if [ "${F_SH[$k]}" = 1 ] && ! bash -n "${F_PATH[$k]}" 2> /dev/null; then
            err "최종 문법 검사(bash -n) 실패: ${F_PATH[$k]}"; bad=1
        fi
    done
    _E_FINAL_RM=$tot
    [ $bad -eq 0 ]
}

# ---------------------------------------------------------------- --undo
_engine_undo() {
    local ver f b n=0 bad=0 sb
    if [ ! -f "$_E_JOURNAL" ]; then
        if [ -f "$_E_JOURNAL.undone" ]; then
            info "마지막 업데이트는 이미 되돌렸습니다 ($_E_JOURNAL.undone) — 변경 없음"
            return 0
        fi
        warn "저널 없음($_E_JOURNAL) — 명세 대상 파일을 각각 가장 최근 백업으로 복원합니다"
        for ((n = 0; n < F_N; n++)); do restore_latest "${F_PATH[$n]}" || bad=1; done
        [ $bad -eq 0 ] || return 1
        return 0
    fi
    while IFS=$'\t' read -r ver f b; do
        [ -n "$f" ] || continue
        [ -f "$b" ] || { err "백업 없음: $b  ($f)"; bad=1; }
    done < "$_E_JOURNAL"
    [ $bad -eq 0 ] || { err "되돌리기 중단 — 파일은 변경되지 않았습니다"; return 1; }
    while IFS=$'\t' read -r ver f b; do
        [ -n "$f" ] || continue
        if [ -f "$f" ]; then
            sb=$(backup_file "$f") && info "현재 상태 백업: $sb"
            cat "$b" > "$f" || { err "복원 실패: $f"; bad=1; continue; }
        else
            cp -p "$b" "$f" || { err "복원 실패: $f"; bad=1; continue; }
        fi
        ok "복원 완료: $f  <-  $b  (v$ver 적용 전)"
        n=$((n + 1))
    done < "$_E_JOURNAL"
    [ $bad -eq 0 ] || return 1
    mv -f "$_E_JOURNAL" "$_E_JOURNAL.undone"
    ok "되돌리기 완료: ${n}개 파일"
    return 0
}

# ---------------------------------------------------------------- main
_eng_usage() {
    cat <<'EOS'
사용: bash update_v<버전>.sh [<대상 파일>...] [--dir <루트>] [--undo] [--dry] [--yes] [--role <역할>]
  <대상 파일>   명세의 @file 대신 쓸 현장 사본 (basename 으로 대응)
  --dir         @file·manifest 상대경로 기준 (기본: 스크립트 디렉터리의 상위)
  --undo        마지막 실행 전 상태로 복원 (저널의 백업 사용)
  --dry         바뀔 내용만 출력하고 아무 것도 바꾸지 않음
  --yes         확인 질문에 자동 y (변수 값 질문은 그대로)
  --role        새 변수 질문 시 역할 필터 (os8 / os6 / common ...)
EOS
}

_engine_run() {
    local undo=0 dry=0 role= dir= k c rc nchg=0 np=0 self
    local -a pos=()
    _E_TD=$(mktemp -d "${TMPDIR:-/tmp}/gus_eng.XXXXXX") || { err "임시 디렉터리 생성 실패"; return 2; }
    chmod 700 "$_E_TD"
    trap '_eng_cleanup' EXIT
    trap '_eng_interrupt' INT TERM
    _E_WRITTEN=0; _E_DONE=0; _E_NAPPLIED=0; _E_NSKIP=0; _E_SECVALS=; _E_JRESET=1
    while [ $# -gt 0 ]; do
        case "$1" in
            --undo) undo=1 ;;
            --dry|--dry-run) dry=1 ;;
            --yes|-y) ASSUME_YES=1 ;;
            --dir) [ $# -ge 2 ] || { err "--dir 값이 없습니다"; return 2; }; dir=$2; shift ;;
            --dir=*) dir=${1#--dir=} ;;
            --role) [ $# -ge 2 ] || { err "--role 값이 없습니다"; return 2; }; role=$2; shift ;;
            --role=*) role=${1#--role=} ;;
            -h|--help) _eng_usage; return 0 ;;
            --) shift; while [ $# -gt 0 ]; do pos[$np]=$1; np=$((np + 1)); shift; done; break ;;
            -*) err "알 수 없는 옵션: $1"; _eng_usage; return 2 ;;
            *) pos[$np]=$1; np=$((np + 1)) ;;
        esac
        shift
    done
    if [ $undo = 1 ] && [ $dry = 1 ]; then err "--undo 와 --dry 는 함께 쓸 수 없습니다"; return 2; fi
    self=${ENGINE_SELF:-$0}
    if [ -z "$dir" ]; then
        dir=$(cd "$(dirname "$self")" 2> /dev/null && pwd) && dir="$dir/.." || dir=.
    fi
    PROJECT_DIR=$(cd "$dir" 2> /dev/null && pwd) || { err "--dir 디렉터리 없음: $dir"; return 2; }
    _ENG_DRY=$dry
    _eng_load_inputs || return 2
    printf '%s== 업데이트 v%s%s ==%s\n' "$C_BOLD" "${_E_VERSION:-?}" "${META[project]:+ (${META[project]})}" "$C_RST"
    info "프로젝트 루트: $PROJECT_DIR"
    if [ $np -gt 0 ]; then _eng_resolve_files "$np" "${pos[@]}" || return 2
    else _eng_resolve_files 0 || return 2; fi
    if [ $undo = 1 ]; then _engine_undo; return $?; fi
    [ $dry = 1 ] && info "--dry: 바뀔 내용만 보여 주고 파일은 바꾸지 않습니다"

    # 1) 작업본에서 변경 적용
    _eng_prepare
    for ((c = 0; c < S_N; c++)); do
        if ! _apply_change "$c"; then
            err "중단 — 대상 파일은 변경되지 않았습니다"; return 1
        fi
    done
    # 2) 변수 처리 + 3) 전체 검사
    _eng_vars || { err "중단 — 대상 파일은 변경되지 않았습니다"; return 1; }
    _eng_removed_vars
    if ! _eng_check_work; then
        err "중단 — 대상 파일은 변경되지 않았습니다"; return 1
    fi
    for ((k = 0; k < F_N; k++)); do
        cmp -s "${F_PATH[$k]}" "${F_W[$k]}" || nchg=$((nchg + 1))
    done

    if [ $dry = 1 ]; then
        printf '\n%s== 바뀔 내용 (--dry) ==%s\n' "$C_BOLD" "$C_RST"
        for ((k = 0; k < F_N; k++)); do
            cmp -s "${F_ORIG[$k]}" "${F_W[$k]}" && continue
            diff -u --label "현재: ${F_PATH[$k]}" --label "적용 후" "${F_ORIG[$k]}" "${F_W[$k]}" | _mask_out
        done
        [ -n "$_E_NEWVARS" ] && info "적용 후 질문할 새 변수: $_E_NEWVARS"
        ok "검사 통과: 변경 ${_E_NAPPLIED}건 적용 가능, 이미 적용됨 ${_E_NSKIP}건, 주석·코멘트 문구 변경 0건"
        info "--dry: 아무 파일도 바꾸지 않았습니다"
        return 0
    fi

    if [ $nchg -eq 0 ] && [ -z "$_E_NEWVARS" ]; then
        ok "변경 없음 — 모든 변경이 이미 적용되어 있습니다 (적용 0건, 이미 적용됨 ${_E_NSKIP}건)"
        return 0
    fi
    if [ $nchg -gt 0 ]; then
        printf '\n%s== 적용 대상 ==%s\n' "$C_BOLD" "$C_RST"
        for ((k = 0; k < F_N; k++)); do
            cmp -s "${F_PATH[$k]}" "${F_W[$k]}" || printf '    %s\n' "${F_PATH[$k]}"
        done
        if ! confirm "위 파일에 변경 ${_E_NAPPLIED}건을 적용하시겠습니까?"; then
            warn "취소했습니다 — 파일은 변경되지 않았습니다"; return 1
        fi
    fi
    # 4) 백업 → 쓰기
    for ((k = 0; k < F_N; k++)); do
        cmp -s "${F_PATH[$k]}" "${F_W[$k]}" && continue
        _eng_backup "$k" || { err "백업 실패 — 중단"; _eng_restore_all; return 1; }
    done
    for ((k = 0; k < F_N; k++)); do
        cmp -s "${F_PATH[$k]}" "${F_W[$k]}" && continue
        _E_WRITTEN=1
        cat "${F_W[$k]}" > "${F_PATH[$k]}" || { err "쓰기 실패: ${F_PATH[$k]} — 원상복구"; _eng_restore_all; return 1; }
    done
    # 5) 새 변수만 가이드
    if [ -n "$_E_NEWVARS" ]; then
        local i nm
        for ((i = 0; i < M_COUNT; i++)); do
            nm=${M_NAME[$i]}
            case " $_E_NEWVARS " in *" $nm "*) ;; *) continue ;; esac
            _eng_backup "${V_F[$i]}" || { err "백업 실패 — 원상복구"; _eng_restore_all; return 1; }
        done
        printf '\n%s== 새 변수 입력 ==%s\n' "$C_BOLD" "$C_RST"
        info "이번 업데이트로 추가된 변수만 묻습니다: $_E_NEWVARS"
        GUIDE_ONLY=$_E_NEWVARS
        _E_WRITTEN=1
        run_guide "$role"; rc=$?
        GUIDE_ONLY=
        if [ $rc -ne 0 ]; then
            err "새 변수 입력이 끝나지 않아 이번 업데이트 전체를 원상복구합니다"
            _eng_restore_all; return 1
        fi
    fi
    # 6) 최종 검증
    if ! _eng_final_verify; then
        err "최종 검증 실패 — 원상복구합니다"; _eng_restore_all; return 1
    fi
    _E_DONE=1
    printf '\n%s== 검증 요약 ==%s\n' "$C_BOLD" "$C_RST"
    ok "변경 적용 ${_E_NAPPLIED}건, 이미 적용됨(건너뜀) ${_E_NSKIP}건"
    ok "삭제·변경된 원본 코드 줄 ${_E_FINAL_RM}줄 (변수 값 변경 제외, 모두 가드 검사 통과)"
    ok "주석·코멘트 문구 변경 0건"
    for ((k = 0; k < F_N; k++)); do
        [ -n "${F_BAK[$k]}" ] || continue
        [ "${F_SH[$k]}" = 1 ] && ok "문법 검사(bash -n) 통과: ${F_PATH[$k]}"
        info "백업: ${F_BAK[$k]}"
    done
    info "되돌리려면: bash ${self##*/} --undo  (저널: $_E_JOURNAL)"
    warn "Disclaimer: 업데이트·설정 변경 후 랜덤 서버 몇 대에서 실제 변경을 확인하십시오."
    return 0
}

engine_main() {   # engine_main "$@" — 위 "입력" 참고
    local rc
    _engine_run "$@"; rc=$?
    _eng_cleanup
    trap - EXIT INT TERM
    return $rc
}
: <<'#__MANIFEST_END__'
#__MANIFEST_BEGIN__
# awx_script vars.manifest — 01.AWX_nodeinfo_V2.sh 최상단 빈 변수 14개 + user() 안의 user_route + 02.source_dhcp_pxe.sh 의 빈 변수 2개 (git-upload-sk 가이드·업데이트 공용)
#
# 형식: name|target|kind|role|group|required|desc|example|check|since|renamed_from  (| 구분 11필드)
#   - target 은 프로젝트 루트(setup/ 의 상위 = awx_script) 기준 상대경로. 값은 소스의 name="..." 줄에 들어간다.
#   - 설명 안 줄바꿈은 \n, 값에 세로선(파이프) 문자를 쓸 수 없다 → 세로선으로 구분하는 변수는 설명에 말로 풀어 쓴다.
#   - 역할: os8 = os8_mgmt (auto_setup 데몬이 도는 서버, 접속 불가 호스트를 os6_mgmt 에서 재조사하는 쪽),
#           os6 = os6_mgmt (auto_setup 데몬 서버로 gossh 전달이 필요한 쪽). common 은 어느 역할에서나 질문.
#   - 다른 서버 위 경로·원격 호스트는 이 서버에서 확인할 수 없어 check=none/host. awx_script 는 setup.sh 가 없으므로 #@meta setup 을 두지 않는다.
#   - 이 업데이트(0.6.1, 0.6.0 변경 포함 누적)에서는 새 변수가 없다. 이미 있던 변수(since < 0.6.0)는 업데이트가 묻지 않고 현장 값을 그대로 둔다.
#
#@meta project=awx_script
#@meta version=0.6.1
#
#
# ---- 01.AWX_nodeinfo_V2.sh ----
repohost|sh:01.AWX_nodeinfo_V2.sh|host|common|-|y|인벤토리 yml 을 만들어 주는 서버 (ssh/scp 대상). 입력 파일을 scp 로 이 서버의 /root/Inventory/ 에 올리고 ssh 로 custom_inventory.sh 를 실행한다.\n비어 있으면 작업진행 단계에서 "repohost 가 비어 있습니다" 로 중단한다.|repo01|host|0.1.0|-
svr_dir|sh:01.AWX_nodeinfo_V2.sh|path|common|-|y|custom_inventory.sh 가 만든 yml 이 이 서버에서 보이는 공유 경로 (autofs/NFS 등). 새로 생긴 yml 1개를 찾아 AWX 인벤토리 소스로 쓴다.\n존재하는 디렉터리여야 하며, 비어 있거나 없으면 작업진행 단계에서 중단한다.|/path/svr_dir|dir|0.1.0|-
ai_server_list|sh:01.AWX_nodeinfo_V2.sh|text|common|-|n|AI GPU 서버의 호스트명 (완전 일치, 정규식 아님). 여러 대는 세로선(파이프) 문자로 이어 쓴다.\n등록 대상에 이 서버가 있으면 "power limit 설정 필요" 안내와 .power_limit_setting.txt 내용을 출력한다. 비워 두면 이 안내를 하지 않는다.|gpuhost01|none|0.1.0|-
day_print|sh:01.AWX_nodeinfo_V2.sh|text|common|-|n|이 목록에 user 가 들어 있으면 작업 대상을 "날짜 tmp tmp infra hostname ..." 형식으로 출력한다. 여러 user 는 공백·쉼표·세로선 중 하나로 구분한다.\n비워 두면 기존 세로 다단 출력만 사용한다.|user1|none|0.4.0|-
lacp_comment|sh:01.AWX_nodeinfo_V2.sh|text|common|-|y|LACP(802.3ad) 본딩 호스트가 발견되었을 때 호스트 목록 아래에 노란색으로 함께 출력하는 안내 문구 (담당자에게 확인할 내용 등).\n비어 있으면 작업진행 단계에서 중단한다.|LACP 호스트입니다. 스위치 설정을 확인하세요|none|0.1.0|-
inventory_delete_host|sh:01.AWX_nodeinfo_V2.sh|host|common|-|y|기존 인벤토리 호스트를 지우는 서버. 작업진행 Y 이후 ssh 로 접속해 "bash /root/server/delhost_<user>" 를 실행한다 (user@host 형식 가능).\n비어 있으면 원격 삭제가 시작되기 전에 미리 검사해 중단한다.|root@awxmgmt01|host|0.1.0|-
infra_alias|sh:01.AWX_nodeinfo_V2.sh|text|common|-|n|등록되지 않은 infra 이름을 등록된 이름으로 바꾸는 치환 목록. "원래이름:바꿀이름" 형식을 공백 또는 쉼표로 나열한다 (3번째 필드에 적용).\n비워 두면 치환하지 않는다.|adjfg:infra1 foo:infra2|none|0.4.0|-
auto_setup_host|sh:01.AWX_nodeinfo_V2.sh|host|os6|-|n|auto_setup 데몬이 도는 os8_mgmt 호스트명. os6_mgmt 등 다른 서버에서 01 을 실행할 때만 채운다.\n채우면 02 성공 후 호스트 목록을 gossh 로 그 서버의 ${AUTO_SETUP_DIR:-/tmp/auto_setup}/queue/ 에 전송하고(로컬 queue 는 만들지 않음), 비우면 이 서버의 로컬 queue 에 직접 기록한다 (os8_mgmt 에서 실행하는 경우).|os8mgmt01|host|0.5.0|-
auto_setup_gossh_pw|sh:01.AWX_nodeinfo_V2.sh|secret|os6|dep:auto_setup_host|n|auto_setup_host 로 gossh 전송 시 키 인증이 안 될 때 쓰는 SSH 비밀번호 (gossh -p 로 전달). 비우면 -p 없이 호출한다.\n입력은 화면에 표시되지 않고 소스에 평문으로 기록되며 파일 권한이 600 으로 바뀐다. 이 파일은 저장소에 커밋하지 않는다.|(비밀번호 - 입력 시 화면에 표시되지 않음)|none|0.5.1|-
os6_host|sh:01.AWX_nodeinfo_V2.sh|host|os8|-|n|os6_mgmt 호스트명. os8_mgmt 에서 접속 불가인 호스트를 os6_mgmt 의 gossh 로 다시 조사한다 (01 의 LDAP/LACP 점검, 02 의 파티션 확인).\nos6_host·os6_user·os6_dir·os6_gossh 네 변수가 모두 채워져야 동작하며 하나라도 비면 재조사를 생략한다. 01 이 네 값을 02 로 export 한다.|os6mgmt01|host|0.5.1|-
os6_user|sh:01.AWX_nodeinfo_V2.sh|text|os8|dep:os6_host|n|os6_mgmt 에 ssh 로 접속할 계정 (키 인증).\n네 변수(os6_host·os6_user·os6_dir·os6_gossh)가 모두 채워져야 재조사가 동작한다.|root|none|0.5.1|-
os6_dir|sh:01.AWX_nodeinfo_V2.sh|path|os8|dep:os6_host|n|os8_mgmt 와 os6_mgmt 가 같은 절대경로로 공유(autofs)하는 디렉터리. 재조사할 호스트 목록과 실행 파일을 임시로 만들었다가 종료 시 지운다.\n이 서버에 존재하는 디렉터리여야 하며, 네 변수가 모두 채워져야 재조사가 동작한다.|/path/os6_share|dir|0.5.1|-
os6_gossh|sh:01.AWX_nodeinfo_V2.sh|path|os8|dep:os6_host|n|os6_mgmt 위 gossh 실행 파일의 전체 경로 (os6_mgmt 기준 경로, 이 서버에 없어도 된다).\n네 변수가 모두 채워져야 재조사가 동작한다.|/path/gossh|none|0.5.1|-
user_route|sh:01.AWX_nodeinfo_V2.sh|path|common|-|y|user() 함수가 쓰는 info_mn.sh(user 선택 메뉴)와 info.sh(번호 → user 이름)가 있는 디렉터리.\n01 은 시작할 때 "bash <user_route>/info_mn.sh" 로 메뉴를 보여 주고 선택한 번호를 "bash <user_route>/info.sh 번호" 에 넘겨 user 를 정한다. 비면 user 값이 없다며 중단한다.|/path/info|dir|0.2.1|-
# ---- 02.source_dhcp_pxe.sh ----
dhcp_infra_alias|sh:02.source_dhcp_pxe.sh|text|common|-|n|dhcp 에 넘기는 infra 이름이 yml 의 infra 와 다를 때의 치환 목록. "표시infra:넘길값" 형식을 공백 또는 쉼표로 나열한다 (표시infra 는 대소문자 무시).\n비워 두면 치환 없이 infra 를 그대로 dhcp -infra 로 넘긴다.|infra1:asdf|none|0.3.0|-
pxe_infra_alias|sh:02.source_dhcp_pxe.sh|text|common|-|n|pxe 에 넘기는 infra 이름이 yml 의 infra 와 다를 때의 치환 목록. "표시infra:넘길값" 형식을 공백 또는 쉼표로 나열한다 (표시infra 는 대소문자 무시).\n비워 두면 치환 없이 infra 를 그대로 pxe -infra 로 넘긴다.|infra1:asdfl|none|0.3.0|-
#__MANIFEST_END__
: <<'#__SPEC_END__'
#__SPEC_BEGIN__
# make_update.sh 자동 생성 (2026-10-08 11:23) — 앵커는 사람이 검토할 것. #candidates 는 대체 후보(엔진은 무시)
@file 01.AWX_nodeinfo_V2.sh

#candidates 1) insert_after 줄 17: ^[[:space:]]*svr_idr[[:space:]]*=
@change c1 insert_after
@anchor ^[[:space:]]*svr_idr[[:space:]]*=
@text
cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")" || exit 1   # 어느 경로에서 실행해도 awxkit/·LOG/·02 등 상대경로가 이 스크립트 디렉터리 기준이 되도록 이동
@endtext

#candidates 1) insert_after 줄 67: ^[[:space:]]*user_route[[:space:]]*=
#candidates 2) insert_before 줄 68: ^[[:space:]]*bash[[:space:]]+\$user_route/info_mn\.sh([[:space:]]+#.*)?[[:space:]]*$
@change c2 insert_after
@anchor ^[[:space:]]*user_route[[:space:]]*=
@text
if [[ -n $AWX_USER && $AWX_USER =~ ^[A-Za-z0-9._-]+$ ]]; then
	# auto_setup 에서 넘긴 user (메뉴 생략)
	user=$AWX_USER
	echo "user : ${user} (auto_setup)"
	return 0
fi
[[ -n $AWX_USER ]] && warn "[!] AWX_USER 가 올바르지 않아 메뉴로 진행합니다"
@endtext

#candidates 1) replace_line 줄 88: ^[[:space:]]*read[[:space:]]+-r[[:space:]]+-p[[:space:]]+"[[:space:]]+awx[[:space:]]+nodeinfo[[:space:]]+사용여부[[:space:]]+Y\|N[[:space:]]+:[[:space:]]+"[[:space:]]+awx_yn([[:space:]]+#.*)?[[:space:]]*$
@change c3 replace_line
@anchor ^[[:space:]]*read[[:space:]]+-r[[:space:]]+-p[[:space:]]+"[[:space:]]+awx[[:space:]]+nodeinfo[[:space:]]+사용여부[[:space:]]+Y\|N[[:space:]]+:[[:space:]]+"[[:space:]]+awx_yn([[:space:]]+#.*)?[[:space:]]*$
@text
if [[ $AWX_AUTO == 1 && $AWX_AUTO_NODEINFO == [YyNn] ]]; then
		# auto 모드: 환경변수 AWX_AUTO_NODEINFO(y|n) 로 자동 답변
		awx_yn=$AWX_AUTO_NODEINFO
		echo " awx nodeinfo 사용여부 Y|N : ${awx_yn} (auto)"
	else
		[[ $AWX_AUTO == 1 ]] && warn "[!] AWX_AUTO_NODEINFO 가 y|n 이 아니어서 직접 입력합니다"
		read -r -p " awx nodeinfo 사용여부 Y|N : " awx_yn
	fi
@endtext

#candidates 1) replace_line 줄 362: ^[[:space:]]*read[[:space:]]+-r[[:space:]]+-p[[:space:]]+"\$\{YELLOW\}작업진행여부[[:space:]]+\(Y\|N\)[[:space:]]+:[[:space:]]+\$\{RST\}"[[:space:]]+go([[:space:]]+#.*)?[[:space:]]*$
@change c4 replace_line
@anchor ^[[:space:]]*read[[:space:]]+-r[[:space:]]+-p[[:space:]]+"\$\{YELLOW\}작업진행여부[[:space:]]+\(Y\|N\)[[:space:]]+:[[:space:]]+\$\{RST\}"[[:space:]]+go([[:space:]]+#.*)?[[:space:]]*$
@text
if [[ $AWX_AUTO == 1 ]]; then
	go=Y
	echo "${YELLOW}작업진행여부 (Y|N) : ${RST}${go} (auto)"
else
	read -r -p "${YELLOW}작업진행여부 (Y|N) : ${RST}" go
fi
@endtext

#candidates 1) replace_line 줄 390: ^[[:space:]]*read[[:space:]]+-r[[:space:]]+-p[[:space:]]+"AWX[[:space:]]+인벤토리[[:space:]]+소스[[:space:]]+:[[:space:]]+\$\{GREEN\}su\$\{RST\}[[:space:]]+\$\{RED\}exit\$\{RST\}[[:space:]]+:[[:space:]]+종료[[:space:]]+\$\{CYAN\}ls\$\{RST\}[[:space:]]+:[[:space:]]+yaml[[:space:]]+파일출력[[:space:]]+:[[:space:]]+"[[:space:]]+sel[[:space:]]+\|\|[[:space:]]+\{[[:space:]]+err[[:space:]]+"\[X\][[:space:]]+입력이[[:space:]]+끝났습니다";[[:space:]]+exit[[:space:]]+1;[[:space:]]+\}([[:space:]]+#.*)?[[:space:]]*$
@change c5 replace_line
@anchor ^[[:space:]]*read[[:space:]]+-r[[:space:]]+-p[[:space:]]+"AWX[[:space:]]+인벤토리[[:space:]]+소스[[:space:]]+:[[:space:]]+\$\{GREEN\}su\$\{RST\}[[:space:]]+\$\{RED\}exit\$\{RST\}[[:space:]]+:[[:space:]]+종료[[:space:]]+\$\{CYAN\}ls\$\{RST\}[[:space:]]+:[[:space:]]+yaml[[:space:]]+파일출력[[:space:]]+:[[:space:]]+"[[:space:]]+sel[[:space:]]+\|\|[[:space:]]+\{[[:space:]]+err[[:space:]]+"\[X\][[:space:]]+입력이[[:space:]]+끝났습니다";[[:space:]]+exit[[:space:]]+1;[[:space:]]+\}([[:space:]]+#.*)?[[:space:]]*$
@text
	menu_prompt="AWX 인벤토리 소스 : ${GREEN}su${RST} ${RED}exit${RST} : 종료 ${CYAN}ls${RST} : yaml 파일출력 : "
	if [[ $AWX_AUTO == 1 ]]; then
		sel=su
		echo "${menu_prompt}${sel} (auto)"
	else
		read -r -p "$menu_prompt" sel || { err "[X] 입력이 끝났습니다"; exit 1; }
	fi
@endtext

#candidates 1) insert_after 줄 453: ^[[:space:]]*echo[[:space:]]+"\$\{BOLD\}등록[[:space:]]+후[[:space:]]+확인\$\{RST\}[[:space:]]+:[[:space:]]+작업[[:space:]]+대상[[:space:]]+서버[[:space:]]+목록을[[:space:]]+붙여넣으세요[[:space:]]+\(공백/쉼표/\|[[:space:]]+구분[[:space:]]+가능,[[:space:]]+입력을[[:space:]]+마치려면[[:space:]]+빈[[:space:]]+줄[[:space:]]+/[[:space:]]+건너뛰려면[[:space:]]+바로[[:space:]]+빈[[:space:]]+줄\)"([[:space:]]+#.*)?[[:space:]]*$
#candidates 2) insert_before 줄 454: ^[[:space:]]*while[[:space:]]+IFS=[[:space:]]+read[[:space:]]+-r[[:space:]]+line;[[:space:]]+do([[:space:]]+#.*)?[[:space:]]*$
@change c6 insert_after
@anchor ^[[:space:]]*echo[[:space:]]+"\$\{BOLD\}등록[[:space:]]+후[[:space:]]+확인\$\{RST\}[[:space:]]+:[[:space:]]+작업[[:space:]]+대상[[:space:]]+서버[[:space:]]+목록을[[:space:]]+붙여넣으세요[[:space:]]+\(공백/쉼표/\|[[:space:]]+구분[[:space:]]+가능,[[:space:]]+입력을[[:space:]]+마치려면[[:space:]]+빈[[:space:]]+줄[[:space:]]+/[[:space:]]+건너뛰려면[[:space:]]+바로[[:space:]]+빈[[:space:]]+줄\)"([[:space:]]+#.*)?[[:space:]]*$
@text
	if [[ -n $AWX_VERIFY_FILE && -f $AWX_VERIFY_FILE && -r $AWX_VERIFY_FILE && -s $AWX_VERIFY_FILE ]]; then
		# auto_setup 에서 넘긴 대상 서버 목록 (stdin 입력 생략)
		pasted=$(cat "$AWX_VERIFY_FILE")$'\n'
		echo "붙여넣을 목록: auto_setup 에서 입력한 $(printf '%s' "$pasted" | tr ',|' '  ' | tr -s '[:space:]' '\n' | grep -c .)대"
	else
@endtext

#candidates 1) insert_before 줄 458: ^[[:space:]]*if[[:space:]]+\[\[[[:space:]]+-z[[:space:]]+\$pasted[[:space:]]+\]\];[[:space:]]+then[[:space:]]+log[[:space:]]+"대상[[:space:]]+확인[[:space:]]+생략";[[:space:]]+return[[:space:]]+0;[[:space:]]+fi([[:space:]]+#.*)?[[:space:]]*$
@change c7 insert_before
@anchor ^[[:space:]]*if[[:space:]]+\[\[[[:space:]]+-z[[:space:]]+\$pasted[[:space:]]+\]\];[[:space:]]+then[[:space:]]+log[[:space:]]+"대상[[:space:]]+확인[[:space:]]+생략";[[:space:]]+return[[:space:]]+0;[[:space:]]+fi([[:space:]]+#.*)?[[:space:]]*$
@text
	fi
@endtext

@file 02.source_dhcp_pxe.sh

#candidates 1) insert_after 줄 96: ^[[:space:]]*default_os[[:space:]]*=
@change d1 insert_after
@anchor ^[[:space:]]*default_os[[:space:]]*=
@text
# auto 모드(AWX_AUTO=1): AWX_AUTO_OS(선택지 중 하나 또는 default) 를 모든 yml 에 적용하고 옵션 확인도 Y 로 답한다. 선택지에 없으면 auto 를 끄고 직접 입력
auto_on=0
if [[ $AWX_AUTO == 1 ]]; then
	auto_os=$AWX_AUTO_OS
	[[ $auto_os == default ]] && auto_os=$default_os
	for c in "${os_choices[@]}"; do [[ $c == "$auto_os" ]] && auto_on=1; done
	if [[ $auto_on == 1 ]]; then default_os=$auto_os
	else warn "[!] AWX_AUTO_OS 값(${AWX_AUTO_OS})이 선택지(${os_choices[*]} default)에 없어 auto 를 끄고 직접 입력합니다"
	fi
fi
@endtext

#candidates 1) insert_before 줄 98: ^[[:space:]]*echo[[:space:]]+"\$\{BOLD\}OS[[:space:]]+버전[[:space:]]+선택\$\{RST\}"([[:space:]]+#.*)?[[:space:]]*$
@change d2 insert_before
@anchor ^[[:space:]]*echo[[:space:]]+"\$\{BOLD\}OS[[:space:]]+버전[[:space:]]+선택\$\{RST\}"([[:space:]]+#.*)?[[:space:]]*$
@text
	if [[ $auto_on == 1 ]]; then os_mode=1; echo "${BOLD}OS 버전 선택${RST} : ${default_os} (auto)"; break; fi
@endtext

#candidates 1) insert_before 줄 120: ^[[:space:]]*read[[:space:]]+-r[[:space:]]+-p[[:space:]]+"\$\{YELLOW\}옵션이[[:space:]]+맞습니까[[:space:]]+\(Y\|N\)[[:space:]]+:[[:space:]]+\$\{RST\}"[[:space:]]+ok[[:space:]]+\|\|[[:space:]]+\{[[:space:]]+err[[:space:]]+"\[X\][[:space:]]+입력이[[:space:]]+끝났습니다";[[:space:]]+exit[[:space:]]+1;[[:space:]]+\}([[:space:]]+#.*)?[[:space:]]*$
@change d3 insert_before
@anchor ^[[:space:]]*read[[:space:]]+-r[[:space:]]+-p[[:space:]]+"\$\{YELLOW\}옵션이[[:space:]]+맞습니까[[:space:]]+\(Y\|N\)[[:space:]]+:[[:space:]]+\$\{RST\}"[[:space:]]+ok[[:space:]]+\|\|[[:space:]]+\{[[:space:]]+err[[:space:]]+"\[X\][[:space:]]+입력이[[:space:]]+끝났습니다";[[:space:]]+exit[[:space:]]+1;[[:space:]]+\}([[:space:]]+#.*)?[[:space:]]*$
@text
	if [[ $auto_on == 1 ]]; then ok=Y; echo "${YELLOW}옵션이 맞습니까 (Y|N) : ${RST}${ok} (auto)"; break; fi
@endtext
#__SPEC_END__
ENGINE_VERSION=${ENGINE_VERSION:-0.6.1}
engine_main "$@"; exit $?
#__END_OF_UPDATE__
