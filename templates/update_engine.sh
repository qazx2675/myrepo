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
