#!/bin/bash
# make_update.sh - 이전→신규 소스 diff 로 update_v<ver>.sh (자체포함 단일 스크립트) 를 생성한다 (개발자용)
#
# 사용:
#   bash make_update.sh --old <이전 소스> --new <새 소스> --manifest <vars.manifest> --version <v> \
#        [--file <상대경로>] [--old-manifest <이전 manifest>] [--guard <ERE> ...] --out <update_v<ver>.sh> [--yes]
#   --file          생성 스크립트의 @file 에 쓸 상대경로 (기본: --new 의 basename). 절대경로·.. 는 불가.
#   --old-manifest  있으면 새 변수/제거된 변수/이름 바뀐 변수를 요약한다.
#   --guard         마스킹 가드 키워드(ERE) 추가 (여러 번 가능). 생성 스크립트의 manifest 사본에 #@guard 로 덧붙는다.
#   --yes           "생성하시겠습니까?" 확인 질문에 자동 y
#   종료코드: 0 생성 완료, 1 사용자가 생성을 취소, 2 인자·파일 오류, 3 안전하게 변환할 수 없음(스크립트를 만들지 않음)
#
# 동작:
#   1) 두 소스를 임시 사본에서 LF 로 정규화하고 diff(이전→신규)의 hunk 마다 명세 1건을 만든다.
#        추가 hunk → insert_after / insert_before : 삽입 지점 바로 앞(없으면 뒤) 코드 줄을 앵커로 쓴다.
#                    (코드 줄 = 빈 줄·# 주석 줄이 아닌 줄). 그 줄이 이전 소스에서 유일하지 않으면 건너뛰고,
#                    같은 줄이 겹치는 경우 결과가 같은 범위 안에서 삽입 지점을 옮겨 본다.
#        바뀐/지워진 hunk → replace_line / replace_range : 시작·끝이 코드 줄이 되도록 범위를 넓히고,
#                    넓힌 줄은 @text 에 그대로 다시 넣는다 (현장에서 그 줄을 고쳤으면 되돌려지므로 요약에 표시).
#   2) 앵커는 ERE 메타문자를 이스케이프한 정규식이다. 대입문(이름=값)은 값이 현장에서 바뀌므로 `^이름=` 만 쓰고,
#      그 외 줄은 전체 줄(줄 끝 코멘트 허용)을 쓴다. update_engine.sh 의 _anchor_lines 로 유일성을 직접 검증한다.
#   3) 사람 검토용 요약표(변경 id·op·앵커·삽입/삭제 줄 수·경고)를 출력한다.
#   4) lib_common.sh + update_engine.sh + manifest + 명세 + main 을 한 파일로 합치고 끝줄에 #__END_OF_UPDATE__ 를 둔다.
#      머리의 자체 검사(줄 수·끝줄)가 붙여넣기 잘림을 감지한다.
#   5) 자체 검증: 생성한 스크립트를 이전 소스 사본에 실제로 적용해(변수 없는 manifest 로) 결과가 새 소스와
#      바이트 단위로 같은지, 다시 실행하면 변경이 없는지 확인한다. 어긋나면 종료코드 3 으로 파일을 만들지 않는다.
#      (마스킹 위반 경고가 있는 변경은 엔진이 중단하는 것이 정상이므로 이 검증을 건너뛴다.)
#
# 제약: bash 4.1 + sed/awk/grep/diff/coreutils. 빈 배열을 "${a[@]}" 로 펼치지 않는다.

HERE=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=/dev/null
. "$HERE/lib_common.sh" || exit 2
# shellcheck source=/dev/null
. "$HERE/update_engine.sh" || exit 2

TD=
_mk_cleanup() { [ -n "$TD" ] && [ -d "$TD" ] && rm -rf "$TD"; TD=; lib_cleanup; }
trap '_mk_cleanup' EXIT
trap '_mk_cleanup; exit 130' INT TERM

die2() { err "$*" >&2; exit 2; }
die3() { err "$*" >&2; exit 3; }

usage() {
    cat <<'EOS'
사용: bash make_update.sh --old <이전 소스> --new <새 소스> --manifest <vars.manifest> --version <v> \
          [--file <상대경로>] [--old-manifest <이전 manifest>] [--guard <ERE> ...] --out <update_v<ver>.sh> [--yes]
EOS
}

# ---------------------------------------------------------------- 인자
OLD_SRC=; NEW_SRC=; MANIFEST=; VERSION=; REL=; OLD_MAN=; OUT=; YES=0
XG=()
while [ $# -gt 0 ]; do
    case "$1" in
        --old|--new|--manifest|--version|--file|--old-manifest|--guard|--out)
            [ $# -ge 2 ] || die2 "$1 값이 없습니다"
            case "$1" in
                --old) OLD_SRC=$2 ;;
                --new) NEW_SRC=$2 ;;
                --manifest) MANIFEST=$2 ;;
                --version) VERSION=$2 ;;
                --file) REL=$2 ;;
                --old-manifest) OLD_MAN=$2 ;;
                --guard) XG[${#XG[@]}]=$2 ;;
                --out) OUT=$2 ;;
            esac
            shift ;;
        --yes|-y) YES=1 ;;
        -h|--help) usage; exit 0 ;;
        *) usage >&2; die2 "알 수 없는 인자: $1" ;;
    esac
    shift
done
[ -n "$OLD_SRC" ] && [ -n "$NEW_SRC" ] && [ -n "$MANIFEST" ] && [ -n "$VERSION" ] && [ -n "$OUT" ] \
    || { usage >&2; die2 "--old --new --manifest --version --out 은 필수입니다"; }
[ -f "$OLD_SRC" ] || die2 "이전 소스 없음: $OLD_SRC"
[ -f "$NEW_SRC" ] || die2 "새 소스 없음: $NEW_SRC"
[ -f "$MANIFEST" ] || die2 "manifest 없음: $MANIFEST"
[ -z "$OLD_MAN" ] || [ -f "$OLD_MAN" ] || die2 "이전 manifest 없음: $OLD_MAN"
[[ $VERSION =~ ^[0-9A-Za-z._-]+$ ]] || die2 "--version 형식 오류(영문·숫자·._- 만): $VERSION"
[ -n "$REL" ] || REL=${NEW_SRC##*/}
REL=$(_rel_norm "$REL")
case "$REL" in
    /*|../*|*/../*|*/..|..) die2 "--file 은 상대경로여야 하고 .. 를 쓸 수 없습니다: $REL" ;;
esac
[[ $REL == *[[:space:]]* ]] && die2 "--file 에 공백을 쓸 수 없습니다: $REL"
OUTDIR=$(dirname "$OUT")
[ -d "$OUTDIR" ] || die2 "--out 디렉터리 없음: $OUTDIR"
OUT_ABS=$(_abs_path "$OUT")
for x in "$OLD_SRC" "$NEW_SRC" "$MANIFEST"; do
    [ "$(_abs_path "$x")" = "$OUT_ABS" ] && die2 "--out 이 입력 파일과 같습니다: $OUT"
done
[ "$YES" = 1 ] && ASSUME_YES=1
for ((i = 0; i < ${#XG[@]}; i++)); do
    printf '' | grep -Eq -- "${XG[$i]}" 2> /dev/null
    [ $? -eq 2 ] && die2 "--guard 정규식 오류: ${XG[$i]}"
done

TD=$(mktemp -d "${TMPDIR:-/tmp}/gus_mk.XXXXXX") || die2 "임시 디렉터리 생성 실패"
chmod 700 "$TD"

# ---------------------------------------------------------------- manifest
OLD_NAMES=()
HAVE_OLDMAN=0
if [ -n "$OLD_MAN" ]; then
    manifest_load "$OLD_MAN" > /dev/null || die2 "이전 manifest 오류: $OLD_MAN"
    for ((i = 0; i < M_COUNT; i++)); do OLD_NAMES[$i]=${M_NAME[$i]}; done
    HAVE_OLDMAN=1
fi
manifest_load "$MANIFEST" || die2 "manifest 오류: $MANIFEST"
for ((i = 0; i < ${#XG[@]}; i++)); do GUARDS[${#GUARDS[@]}]=${XG[$i]}; done
PROJECT=${META[project]-}
META_VER=${META[version]-}

SECNAMES=
for ((i = 0; i < M_COUNT; i++)); do
    [ "${M_KIND[$i]}" = secret ] && SECNAMES="$SECNAMES ${M_NAME[$i]}"
done

# 화면 표시용: secret 변수 대입 줄은 값을 가린다
show_line() {
    local t=$1 n
    t=${t#"${t%%[![:space:]]*}"}
    for n in $SECNAMES; do
        if [[ $t =~ ^((export|local|readonly)[[:space:]]+)?"$n"[[:space:]]*= ]]; then
            printf '%s=****' "$n"; return 0
        fi
    done
    printf '%s' "$t"
}

# ---------------------------------------------------------------- 소스 정규화·읽기
norm_lf() { LC_ALL=C tr -d '\r' < "$1" | LC_ALL=C awk 1 > "$2"; }
OLDF=$TD/old; NEWF=$TD/new
norm_lf "$OLD_SRC" "$OLDF"
norm_lf "$NEW_SRC" "$NEWF"
grep -q $'\r' "$OLD_SRC" && info "이전 소스에 CR 이 있어 LF 로 정규화했습니다 (임시 사본)"
grep -q $'\r' "$NEW_SRC" && info "새 소스에 CR 이 있어 LF 로 정규화했습니다 (임시 사본)"

NOLD=0; OL=(); ISC=()
while IFS= read -r l || [ -n "$l" ]; do
    NOLD=$((NOLD + 1)); OL[$NOLD]=$l
    if [[ $l =~ ^[[:space:]]*$ ]]; then ISC[$NOLD]=0
    elif [[ $l =~ ^[[:space:]]*# ]] && ! { [ "$NOLD" = 1 ] && [[ $l == '#!'* ]]; }; then ISC[$NOLD]=0
    else ISC[$NOLD]=1; fi
done < "$OLDF"
[ "$NOLD" -gt 0 ] || die2 "이전 소스가 비어 있습니다"

LC_ALL=C diff "$OLDF" "$NEWF" > "$TD/diff"
rc=$?
[ $rc -le 1 ] || die2 "diff 실패"
[ $rc -eq 1 ] || die2 "이전·신규 소스(LF 정규화 후)가 같습니다 — 변경 사항이 없습니다"

# ---------------------------------------------------------------- hunk 파싱
H_N=0; H_S=(); H_E=(); H_TY=(); H_TC=(); CH=(); TER=()
HDR_RE='^([0-9]+)(,([0-9]+))?([acd])([0-9]+)(,([0-9]+))?$'
cur=-1
while IFS= read -r l; do
    if [[ $l =~ $HDR_RE ]]; then
        os=${BASH_REMATCH[1]}; oe=${BASH_REMATCH[3]}; ty=${BASH_REMATCH[4]}
        [ -n "$oe" ] || oe=$os
        cur=$H_N; H_N=$((H_N + 1))
        if [ "$ty" = a ]; then H_S[$cur]=$((os + 1)); H_E[$cur]=$os
        else H_S[$cur]=$os; H_E[$cur]=$oe; fi
        H_TY[$cur]=$ty; H_TC[$cur]=0
        : > "$TD/h.$cur"
        for ((j = H_S[cur]; j <= H_E[cur]; j++)); do CH[$j]=$cur; done
        continue
    fi
    case "$l" in
        '> '*) printf '%s\n' "${l#> }" >> "$TD/h.$cur"; H_TC[$cur]=$((H_TC[cur] + 1)) ;;
        '>')   printf '\n' >> "$TD/h.$cur"; H_TC[$cur]=$((H_TC[cur] + 1)) ;;
    esac
done < "$TD/diff"
[ "$H_N" -gt 0 ] || die2 "diff 결과를 해석하지 못했습니다"

# ---------------------------------------------------------------- 앵커 도구
ASSIGN_RE='^((export|local|readonly)[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*='
FORMS=()
anchor_forms() {   # anchor_forms <줄 번호> -> FORMS: 대입문 접두형(있으면), 전체 줄형
    local t=${OL[$1]} c code kw name re esc
    FORMS=()
    t=${t#"${t%%[![:space:]]*}"}; t=${t%"${t##*[![:space:]]}"}
    if [ "$1" = 1 ] && [[ $t == '#!'* ]]; then
        code=$t
    else
        c=$(_tc "$t")
        if [ -n "$c" ]; then
            code=${t%"$c"}; code=${code%"${code##*[![:space:]]}"}
        else
            code=$t
        fi
    fi
    [ -n "$code" ] || return 0
    if [[ $code =~ $ASSIGN_RE ]]; then
        kw=${BASH_REMATCH[2]}; name=${BASH_REMATCH[3]}
        re='^[[:space:]]*'
        [ -n "$kw" ] && re="$re$kw[[:space:]]+"
        FORMS[0]="$re$name[[:space:]]*="
    fi
    esc=$(printf '%s' "$code" | LC_ALL=C sed -e 's/[][\.*^$(){}?+|]/\\&/g' -e 's/[[:space:]][[:space:]]*/[[:space:]]+/g')
    FORMS[${#FORMS[@]}]="^[[:space:]]*${esc}([[:space:]]+#.*)?[[:space:]]*\$"
}

anchor_unique_at() {   # <ERE> <줄>: 이전 소스에서 그 줄만 일치하면 rc 0
    local r
    r=$(_anchor_lines "$1" "$OLDF" | tr '\n' ' ')
    [ "$r" = "$2 " ]
}

conflicts() {   # conflicts <ERE> <hunk> <own 포함 1|0>: 다른 hunk 의 삽입 텍스트 코드 줄과 일치하면 rc 0
    local f=$1 h=$2 own=$3 j r
    for ((j = 0; j < H_N; j++)); do
        [ "$j" = "$h" ] && [ "$own" != 1 ] && continue
        r=$(_anchor_lines "$f" "$TD/h.$j")
        [ -n "$r" ] && return 0
    done
    return 1
}

A_ERE=; A_ALT=()
try_anchor() {   # try_anchor <줄> <hunk> <own> -> A_ERE(선택), A_ALT(다른 형태 후보)
    local ln=$1 h=$2 own=$3 f k
    A_ERE=; A_ALT=()
    anchor_forms "$ln"
    for ((k = 0; k < ${#FORMS[@]}; k++)); do
        f=${FORMS[$k]}
        anchor_unique_at "$f" "$ln" || continue
        conflicts "$f" "$h" "$own" && continue
        if [ -z "$A_ERE" ]; then A_ERE=$f; else A_ALT[${#A_ALT[@]}]=$f; fi
    done
    [ -n "$A_ERE" ]
}

end_ok() {   # end_ok <ERE> <앵커 줄> <끝 줄>: 앵커 이후 첫 일치 코드 줄이 끝 줄이면 rc 0
    local r
    r=$(_anchor_lines "$1" "$OLDF" | awk -v a="$2" '$1 > a { print; exit }')
    [ "$r" = "$3" ]
}

claimed() {   # claimed <줄> <hunk>: 다른 hunk 의 변경·영역이면 rc 0
    local i=$1 h=$2
    [ -n "${CH[$i]-}" ] && [ "${CH[$i]}" != "$h" ] && return 0
    [ -n "${TER[$i]-}" ] && [ "${TER[$i]}" != "$h" ] && return 0
    return 1
}

shorten() { local s; s=$(show_line "$1"); if [ ${#s} -gt 38 ]; then printf '%s...' "${s:0:35}"; else printf '%s' "$s"; fi; }

# ---------------------------------------------------------------- hunk -> 변경 1건
C_ID=(); C_OP=(); C_ANC=(); C_AEND=(); C_TXT=(); C_LN=(); C_ELN=(); C_ADD=(); C_DEL=(); C_NOTE=(); C_CAND=(); C_GW=()
NGW=0; NNOTE=0

# 후보 목록 문자열에 한 줄 추가
cand_add() { CAND_BUF="$CAND_BUF#candidates $1"$'\n'; }

process_add() {   # 추가 hunk: 삽입 지점 N 뒤
    local h=$1 N=$((H_S[h] - 1)) P k cnt viable=0 first f sh dir n2
    local -a OP_P=() OP_F=() OP_SH=() DP=() DF=() UP=() UF=()
    local -a CD_OP=() CD_LN=() CD_ERE=() CD_F=() CD_SH=()
    local note= skipped= fn t
    CAND_BUF=
    # 삽입 위치를 결과가 같은 범위에서 옮길 수 있는 옵션 (텍스트를 회전)
    cp "$TD/h.$h" "$TD/o.$h.0"
    fn="$TD/o.$h.0"; P=$N; k=0
    while [ $((P + 1)) -le "$NOLD" ] && [ -z "${CH[$((P + 1))]-}" ] && [ $k -lt 100 ]; do
        IFS= read -r first < "$fn"
        [ "$first" = "${OL[$((P + 1))]}" ] || break
        k=$((k + 1)); t="$TD/o.$h.d$k"
        { tail -n +2 "$fn"; head -n 1 "$fn"; } > "$t"
        fn=$t; P=$((P + 1))
        DP[${#DP[@]}]=$P; DF[${#DF[@]}]=$t
    done
    fn="$TD/o.$h.0"; P=$N; k=0
    while [ "$P" -ge 1 ] && [ -z "${CH[$P]-}" ] && [ $k -lt 100 ]; do
        f=$(tail -n 1 "$fn"; echo x); f=${f%x}; f=${f%$'\n'}
        [ "$f" = "${OL[$P]}" ] || break
        k=$((k + 1)); t="$TD/o.$h.u$k"
        { tail -n 1 "$fn"; head -n -1 "$fn"; } > "$t"
        fn=$t; P=$((P - 1))
        UP[${#UP[@]}]=$P; UF[${#UF[@]}]=$t
    done
    OP_P[0]=$N; OP_F[0]="$TD/o.$h.0"; OP_SH[0]=0
    cnt=${#DP[@]}; [ ${#UP[@]} -gt $cnt ] && cnt=${#UP[@]}
    for ((k = 0; k < cnt; k++)); do
        if [ $k -lt ${#DP[@]} ]; then
            n2=${#OP_P[@]}; OP_P[$n2]=${DP[$k]}; OP_F[$n2]=${DF[$k]}; OP_SH[$n2]=$((k + 1))
        fi
        if [ $k -lt ${#UP[@]} ]; then
            n2=${#OP_P[@]}; OP_P[$n2]=${UP[$k]}; OP_F[$n2]=${UF[$k]}; OP_SH[$n2]=$((-(k + 1)))
        fi
    done
    for ((k = 0; k < ${#OP_P[@]} && viable < 3; k++)); do
        P=${OP_P[$k]}; sh=${OP_SH[$k]}
        # 앞 줄(P) 뒤에 삽입
        if [ "$P" -ge 1 ] && [ "${ISC[$P]}" = 1 ] && [ -z "${CH[$P]-}" ]; then
            if try_anchor "$P" "$h" 1; then
                n2=${#CD_OP[@]}; CD_OP[$n2]=insert_after; CD_LN[$n2]=$P; CD_ERE[$n2]=$A_ERE; CD_F[$n2]=${OP_F[$k]}; CD_SH[$n2]=$sh
                viable=$((viable + 1))
            elif [ "$sh" = 0 ]; then
                skipped="$skipped 줄 $P"
            fi
        fi
        # 뒤 줄(P+1) 앞에 삽입
        if [ $((P + 1)) -le "$NOLD" ] && [ "${ISC[$((P + 1))]}" = 1 ] && [ -z "${CH[$((P + 1))]-}" ] && [ $viable -lt 3 ]; then
            if try_anchor "$((P + 1))" "$h" 1; then
                n2=${#CD_OP[@]}; CD_OP[$n2]=insert_before; CD_LN[$n2]=$((P + 1)); CD_ERE[$n2]=$A_ERE; CD_F[$n2]=${OP_F[$k]}; CD_SH[$n2]=$sh
                viable=$((viable + 1))
            elif [ "$sh" = 0 ]; then
                skipped="$skipped 줄 $((P + 1))"
            fi
        fi
    done
    if [ ${#CD_OP[@]} -eq 0 ]; then
        err "[c$((h + 1))] 원본 ${N}번째 줄 뒤 삽입(${H_TC[$h]}줄)을 안전하게 변환할 수 없음 — 유일한 코드 줄 앵커가 없습니다" >&2
        printf '    삽입 지점 앞뒤 코드 줄이 이전 소스에서 유일하지 않거나, 주석·빈 줄 사이이거나, 다른 변경과 겹칩니다.\n' >&2
        printf '    해결: 새 소스의 그 위치 옆에 유일한 식별 줄(예: 마커 코드 줄)을 추가하거나, 명세를 직접 작성하십시오.\n' >&2
        [ "$N" -ge 1 ] && printf '    앞 줄 %s: %s\n' "$N" "$(show_line "${OL[$N]}")" >&2
        [ "$((N + 1))" -le "$NOLD" ] && printf '    뒤 줄 %s: %s\n' "$((N + 1))" "$(show_line "${OL[$((N + 1))]}")" >&2
        exit 3
    fi
    for ((k = 0; k < ${#CD_OP[@]}; k++)); do
        cand_add "$((k + 1))) ${CD_OP[$k]} 줄 ${CD_LN[$k]}: ${CD_ERE[$k]}"
    done
    C_OP[$h]=${CD_OP[0]}; C_ANC[$h]=${CD_ERE[0]}; C_AEND[$h]=; C_LN[$h]=${CD_LN[0]}; C_ELN[$h]=
    C_TXT[$h]=${CD_F[0]}; C_ADD[$h]=${H_TC[$h]}; C_DEL[$h]=0; C_CAND[$h]=$CAND_BUF; C_GW[$h]=
    [ -n "$skipped" ] && note="가까운 코드 줄(${skipped# }) 앵커 부적합(비유일 등) 건너뜀"
    if [ "${CD_SH[0]}" != 0 ]; then
        [ -n "$note" ] && note="$note; "
        note="${note}삽입 지점을 동일 결과 범위에서 ${CD_SH[0]}줄 이동"
    fi
    C_NOTE[$h]=$note
}

find_start() {   # find_start <h> <from> -> AS, ST_ERE, ST_ALT  (rc 1 = 실패)
    local h=$1 from=$2 i lim=0
    AS=; ST_ERE=; ST_ALT=()
    for ((i = from; i >= 1 && lim <= 12; i--, lim++)); do
        claimed "$i" "$h" && return 1
        [ "${ISC[$i]}" = 1 ] || continue
        if try_anchor "$i" "$h" 0; then AS=$i; ST_ERE=$A_ERE; ST_ALT=("${A_ALT[@]+"${A_ALT[@]}"}"); return 0; fi
    done
    return 1
}

find_end() {   # find_end <h> <from> <as> -> AE, EN_ERE  (AE==AS 이면 replace_line)
    local h=$1 from=$2 as=$3 i lim=0 k f
    AE=; EN_ERE=
    for ((i = from; i <= NOLD && lim <= 12; i++, lim++)); do
        claimed "$i" "$h" && return 1
        [ "${ISC[$i]}" = 1 ] || continue
        if [ "$i" = "$as" ]; then AE=$i; return 0; fi
        [ "$i" -gt "$as" ] || continue
        anchor_forms "$i"
        for ((k = 0; k < ${#FORMS[@]}; k++)); do
            f=${FORMS[$k]}
            end_ok "$f" "$as" "$i" || continue
            conflicts "$f" "$h" 0 && continue
            AE=$i; EN_ERE=$f; return 0
        done
    done
    return 1
}

process_rep() {   # 바뀐/지워진 hunk: 원본 S..E 를 T 로
    local h=$1 S=${H_S[h]} E=${H_E[h]} i st2 ok2=0 op tx="$TD/t.$h" ttc="$TD/ttc.$h" w why gw=
    local ret_a ret_b note= k
    CAND_BUF=
    if find_start "$h" "$S" && find_end "$h" "$E" "$AS"; then ok2=1; fi
    # 지우기만 하는데 유지할 줄이 없으면 한 줄 이상 넓힌다
    if [ $ok2 = 1 ] && [ "${H_TC[$h]}" = 0 ] && [ "$AS" = "$S" ] && [ "$AE" = "$E" ]; then
        ok2=0
        if find_end "$h" "$((E + 1))" "$AS"; then ok2=1
        elif find_start "$h" "$((S - 1))" && find_end "$h" "$E" "$AS"; then ok2=1; fi
    fi
    if [ $ok2 != 1 ]; then
        err "[c$((h + 1))] 원본 ${S}~${E}번째 줄 변경(${H_TC[$h]}줄로 대체)을 안전하게 변환할 수 없음" >&2
        printf '    범위 앞뒤의 코드 줄 중 유일하게 지정할 수 있는 앵커를 찾지 못했거나 다른 변경과 겹칩니다.\n' >&2
        printf '    해결: 명세를 직접 작성하거나 변경 단위를 나눠 소스를 수정하십시오.\n' >&2
        for ((i = S; i <= E && i < S + 4; i++)); do printf '    원본 %s: %s\n' "$i" "$(show_line "${OL[$i]}")" >&2; done
        exit 3
    fi
    {
        for ((i = AS; i < S; i++)); do printf '%s\n' "${OL[$i]}"; done
        cat "$TD/h.$h"
        for ((i = E + 1; i <= AE; i++)); do printf '%s\n' "${OL[$i]}"; done
    } > "$tx"
    [ -s "$tx" ] || die3 "[c$((h + 1))] 대체할 텍스트가 비어 있어 변환할 수 없음"
    for ((i = AS; i <= AE; i++)); do TER[$i]=$h; done
    if [ "$AS" = "$AE" ]; then op=replace_line; else op=replace_range; fi
    # 사라지는 원본 줄 가드 검사 (엔진과 같은 함수)
    LC_ALL=C awk "$_E_TC_AWK" "$tx" > "$ttc"
    for ((i = AS; i <= AE; i++)); do
        _line_ok "${OL[$i]}" "$tx" "$ttc" && continue
        gw="$gw원본 ${i}번째 줄 마스킹 위반 예정(${_E_WHY}): $(shorten "${OL[$i]}")"$'\n'
        NGW=$((NGW + 1))
    done
    C_OP[$h]=$op; C_ANC[$h]=$ST_ERE; C_AEND[$h]=$EN_ERE; C_LN[$h]=$AS; C_ELN[$h]=$AE
    C_TXT[$h]=$tx; C_ADD[$h]=${H_TC[$h]}; C_DEL[$h]=$((E - S + 1)); C_GW[$h]=$gw
    cand_add "1) $op 줄 $AS: $ST_ERE"
    for ((k = 0; k < ${#ST_ALT[@]} && k < 2; k++)); do cand_add "$((k + 2))) $op 줄 $AS (다른 형태): ${ST_ALT[$k]}"; done
    C_CAND[$h]=$CAND_BUF
    ret_a=$((S - AS)); ret_b=$((AE - E))
    if [ $ret_a -gt 0 ] || [ $ret_b -gt 0 ]; then
        note="범위 확장: 앞 ${ret_a}줄·뒤 ${ret_b}줄을 그대로 다시 넣음(현장에서 그 줄을 고쳤으면 되돌려짐)"
    fi
    C_NOTE[$h]=$note
}

for ((h = 0; h < H_N; h++)); do
    C_ID[$h]="c$((h + 1))"
    if [ "${H_TY[$h]}" = a ]; then process_add "$h"; else process_rep "$h"; fi
    # @text 안에 @endtext 줄이 있으면 명세로 표현할 수 없다
    if grep -qE '^[[:space:]]*@endtext[[:space:]]*$' "${C_TXT[$h]}"; then
        die3 "[${C_ID[$h]}] 삽입·치환 텍스트에 '@endtext' 줄이 있어 명세로 표현할 수 없음"
    fi
done

# ---------------------------------------------------------------- 요약 (사람 검토용)
printf '\n%s== 생성 요약 (사람 검토용) ==%s\n' "$C_BOLD" "$C_RST"
info "프로젝트: ${PROJECT:-(manifest #@meta project 없음)}  버전: $VERSION  대상 파일(@file): $REL"
info "이전: $OLD_SRC (${NOLD}줄)  ->  신규: $NEW_SRC  /  hunk ${H_N}개"
printf '%-4s %-14s %-40s %5s %5s\n' ID OP '앵커(원본 줄: 내용)' '+삽입' '-삭제'
for ((h = 0; h < H_N; h++)); do
    a="${C_LN[$h]}: $(shorten "${OL[${C_LN[$h]}]}")"
    [ -n "${C_ELN[$h]}" ] && [ "${C_ELN[$h]}" != "${C_LN[$h]}" ] && a="$a  ~  ${C_ELN[$h]}"
    printf '%-4s %-14s %-40s %5s %5s\n' "${C_ID[$h]}" "${C_OP[$h]}" "$a" "+${C_ADD[$h]}" "-${C_DEL[$h]}"
done
NWARN=0
for ((h = 0; h < H_N; h++)); do
    if [ -n "${C_NOTE[$h]}" ]; then warn "${C_ID[$h]}: ${C_NOTE[$h]}"; NWARN=$((NWARN + 1)); fi
    if [ -n "${C_GW[$h]}" ]; then
        while IFS= read -r l; do
            [ -n "$l" ] && warn "${C_ID[$h]}: $l"
        done <<< "${C_GW[$h]}"
    fi
done
if [ $NGW -gt 0 ]; then
    warn "마스킹 위반 예정 ${NGW}건: 이 상태의 스크립트는 현장 사본에 적용할 때 엔진이 중단합니다 (주석·가드 키워드·줄 끝 코멘트 변경)."
    warn "  → 소스에서 해당 주석 변경을 빼거나, 변경이 의도된 것이면 현장 적용 시 직접 수정하도록 안내하십시오."
fi

# 변수 요약
NEWV=; REMV=; RENV=
for ((i = 0; i < M_COUNT; i++)); do
    nm=${M_NAME[$i]}
    if [ "$HAVE_OLDMAN" = 1 ]; then
        had=0
        for ((j = 0; j < ${#OLD_NAMES[@]}; j++)); do
            [ "${OLD_NAMES[$j]}" = "$nm" ] && had=1
            [ "${M_RENAMED[$i]}" != - ] && [ "${OLD_NAMES[$j]}" = "${M_RENAMED[$i]}" ] && had=2
        done
        [ $had = 0 ] && NEWV="$NEWV $nm"
        [ $had = 2 ] && RENV="$RENV ${M_RENAMED[$i]}->$nm"
    elif [ -n "${M_SINCE[$i]}" ] && [ "${M_SINCE[$i]}" = "$VERSION" ]; then
        NEWV="$NEWV $nm"
    fi
done
if [ "$HAVE_OLDMAN" = 1 ]; then
    for ((j = 0; j < ${#OLD_NAMES[@]}; j++)); do
        nm=${OLD_NAMES[$j]}; kept=0
        for ((i = 0; i < M_COUNT; i++)); do
            [ "${M_NAME[$i]}" = "$nm" ] && kept=1
            [ "${M_RENAMED[$i]}" = "$nm" ] && kept=1
        done
        [ $kept = 0 ] && REMV="$REMV $nm"
    done
fi
info "새 변수(실행 시 질문):${NEWV:- (없음)}"
[ -n "$RENV" ] && info "이름 바뀐 변수(값 자동 이전):$RENV"
[ -n "$REMV" ] && warn "manifest 에서 제거된 변수(현장 소스에는 유지·경고):$REMV"
[ -n "$META_VER" ] && [ "$META_VER" != "$VERSION" ] && warn "manifest #@meta version=$META_VER 이 --version $VERSION 과 다릅니다 — 스크립트는 $VERSION 을 쓰도록 고정합니다"
[ -z "$PROJECT" ] && warn "manifest 에 #@meta project 가 없습니다 (헤더에 표시되지 않음)"
tg=0
for ((i = 0; i < M_COUNT; i++)); do
    if [ "$(_rel_norm "${M_TARGET[$i]#*:}")" = "$REL" ]; then
        tg=$((tg + 1))
        if ! _awk_target get "${M_TARGET[$i]%%:*}" "${M_NAME[$i]}" "$NEWF" > /dev/null 2>&1; then
            warn "변수 ${M_NAME[$i]}: 새 소스에 선언 줄이 없음 — 업데이트 후 값 처리·질문이 되지 않습니다"
            NWARN=$((NWARN + 1))
        fi
    fi
done
[ $M_COUNT -gt 0 ] && [ $tg -eq 0 ] && warn "manifest 의 변수 중 --file($REL) 을 대상으로 하는 것이 없습니다 (target 경로 확인)"
if [ ${#GUARDS[@]} -gt 0 ]; then
    info "마스킹 가드(manifest + --guard):"
    for ((i = 0; i < ${#GUARDS[@]}; i++)); do printf '    %s\n' "${GUARDS[$i]}"; done
fi

if ! confirm "이 명세로 업데이트 스크립트를 생성하시겠습니까?"; then
    warn "취소했습니다 — 스크립트를 만들지 않았습니다"
    exit 1
fi

# ---------------------------------------------------------------- 명세·manifest 사본
SPEC=$TD/spec.txt
{
    printf '# make_update.sh 자동 생성 (%s) — 앵커는 사람이 검토할 것. #candidates 는 대체 후보(엔진은 무시)\n' "$(date '+%Y-%m-%d %H:%M')"
    printf '@file %s\n' "$REL"
    for ((h = 0; h < H_N; h++)); do
        printf '\n'
        printf '%s' "${C_CAND[$h]}"
        printf '@change %s %s\n' "${C_ID[$h]}" "${C_OP[$h]}"
        printf '@anchor %s\n' "${C_ANC[$h]}"
        [ -n "${C_AEND[$h]}" ] && printf '@anchor_end %s\n' "${C_AEND[$h]}"
        printf '@text\n'
        cat "${C_TXT[$h]}"
        printf '@endtext\n'
    done
} > "$SPEC"

MANF=$TD/manifest.txt
{
    tr -d '\r' < "$MANIFEST" | awk 1
    for ((i = 0; i < ${#XG[@]}; i++)); do printf '#@guard %s\n' "${XG[$i]}"; done
} > "$MANF"

# ---------------------------------------------------------------- 조립
build_header() {   # build_header <총 줄 수(6자리)>
    cat <<EOS
#!/bin/bash
# update_v${VERSION}.sh - ${PROJECT:-project} v${VERSION} 업데이트 스크립트 (make_update.sh 로 자동 생성, ${GEN_DATE})
# 대상 파일(@file): ${REL}
# 사용: bash update_v${VERSION}.sh [<대상 파일>...] [--dir <루트>] [--undo] [--dry] [--yes] [--role <역할>]
#   기본 동작: 현장 사본의 마스킹(벤더 문자열·경로·주석·코멘트)을 건드리지 않고 앵커 기반으로 변경만 적용한다.
# 이 파일은 자체포함 단일 스크립트다 (lib_common.sh + update_engine.sh + manifest + 변경 명세).
# 붙여넣기 확인: 마지막 줄이 #__END_OF_UPDATE__ 이고 전체 줄 수가 아래 값(${1})이어야 한다 — 아니면 실행하지 않는다.
set -u
GUS_EXPECT_LINES=${1}
_gus_selfcheck() {
    local n l
    if [ ! -f "\$0" ]; then
        printf '[X] 파일로 저장한 뒤 bash <파일> 로 실행하십시오 (표준입력·프로세스 치환으로는 실행할 수 없음)\n' >&2; return 1
    fi
    n=\$(awk 'END { print NR }' "\$0")
    l=\$(tail -n 1 "\$0" | tr -d '\r')
    if [ "\$l" != '#__END_OF_UPDATE__' ] || [ "\$n" != "\$((10#\$GUS_EXPECT_LINES))" ]; then
        printf '[X] 스크립트가 잘렸거나 변형되었습니다 (줄 수 %s, 기대 %s, 마지막 줄 [%s]) — 다시 복사해 주십시오. 대상 파일은 변경하지 않았습니다.\n' "\$n" "\$((10#\$GUS_EXPECT_LINES))" "\${l:0:30}" >&2
        return 1
    fi
    return 0
}
_gus_selfcheck || exit 2
unset -f _gus_selfcheck
EOS
}

GEN_DATE=$(date '+%Y-%m-%d %H:%M')
BODY=$TD/body.sh
{
    tr -d '\r' < "$HERE/lib_common.sh" | awk 1
    tr -d '\r' < "$HERE/update_engine.sh" | awk 1
    engine_emit_block MANIFEST "$MANF" || exit 2
    engine_emit_block SPEC "$SPEC" || exit 2
    printf 'ENGINE_VERSION=${ENGINE_VERSION:-%s}\n' "$VERSION"
    printf 'engine_main "$@"; exit $?\n'
    printf '#__END_OF_UPDATE__\n'
} > "$BODY" || die3 "스크립트 조립 실패"
build_header 000000 > "$TD/hdr.sh"
HL=$(wc -l < "$TD/hdr.sh"); HL=$((HL + 0))
BL=$(wc -l < "$BODY"); BL=$((BL + 0))
TOTAL=$((HL + BL))
CNT=$(printf '%06d' "$TOTAL")
FINAL=$TD/final.sh
{ build_header "$CNT"; cat "$BODY"; } > "$FINAL"

# 구조 검사
[ "$(tail -n 1 "$FINAL")" = '#__END_OF_UPDATE__' ] || die3 "끝줄 마커 오류"
[ "$(grep -cx '#__END_OF_UPDATE__' "$FINAL")" = 1 ] || die3 "#__END_OF_UPDATE__ 마커 줄이 여러 개입니다 (소스 내용에 있는지 확인)"
grep -q $'\r' "$FINAL" && die3 "생성물에 CR 이 있습니다"
[ "$(awk 'END { print NR }' "$FINAL")" = "$TOTAL" ] || die3 "줄 수 계산 불일치"
bash -n "$FINAL" 2> "$TD/syn.err" || { sed 's/^/    /' "$TD/syn.err" | head -n 5 >&2; die3 "생성된 스크립트가 bash -n 에 실패했습니다"; }
engine_extract_block "$FINAL" SPEC > "$TD/x.spec" 2> /dev/null && cmp -s "$TD/x.spec" "$SPEC" || die3 "명세 블록 왕복 검사 실패"
engine_extract_block "$FINAL" MANIFEST > "$TD/x.man" 2> /dev/null && cmp -s "$TD/x.man" "$MANF" || die3 "manifest 블록 왕복 검사 실패"

# ---------------------------------------------------------------- 자체 검증 (실제 적용 시뮬레이션)
simulate() {   # 이전 소스 사본에 적용 -> 새 소스와 바이트 동일 + 재실행 무변경
    local sd=$TD/sim stub="$TD/stub.manifest" log="$TD/sim.log" i
    rm -rf "$sd"; mkdir -p "$sd/$(dirname "$REL")"
    cp "$OLDF" "$sd/$REL"
    {
        [ -n "$PROJECT" ] && printf '#@meta project=%s\n' "$PROJECT"
        printf '#@meta version=%s\n' "$VERSION"
        for ((i = 0; i < ${#GUARDS[@]}; i++)); do printf '#@guard %s\n' "${GUARDS[$i]}"; done
    } > "$stub"
    ENGINE_MANIFEST=$(cat "$stub") ENGINE_JOURNAL="$sd/.journal" NO_COLOR=1 \
        bash "$FINAL" --dir "$sd" --yes < /dev/null > "$log" 2>&1
    SIM_RC=$?
    if [ $SIM_RC -ne 0 ]; then SIM_WHY="적용 실패(rc=$SIM_RC)"; return 1; fi
    if ! cmp -s "$sd/$REL" "$NEWF"; then
        SIM_WHY="적용 결과가 새 소스와 다릅니다: $(diff "$NEWF" "$sd/$REL" | head -n 6 | tr '\n' '|')"
        return 1
    fi
    ENGINE_MANIFEST=$(cat "$stub") ENGINE_JOURNAL="$sd/.journal" NO_COLOR=1 \
        bash "$FINAL" --dir "$sd" --yes < /dev/null > "$TD/sim2.log" 2>&1
    SIM_RC=$?
    if [ $SIM_RC -ne 0 ] || ! cmp -s "$sd/$REL" "$NEWF" || ! grep -q '변경 없음' "$TD/sim2.log"; then
        SIM_WHY="재실행이 멱등이 아닙니다(rc=$SIM_RC)"; log="$TD/sim2.log"
        return 1
    fi
    return 0
}

if [ $NGW -eq 0 ]; then
    if simulate; then
        ok "자체 검증 통과: 이전 소스에 적용하면 새 소스와 바이트 단위로 같고, 재실행은 변경 없음(멱등)"
    else
        err "자체 검증 실패 — 스크립트를 만들지 않습니다: $SIM_WHY" >&2
        sed 's/^/    | /' "$TD/sim.log" | head -n 25 >&2
        exit 3
    fi
else
    warn "마스킹 위반 예정 경고가 있어 자체 검증(실제 적용 시뮬레이션)을 건너뜁니다 — 엔진이 중단하는 것이 정상 동작입니다"
fi

# ---------------------------------------------------------------- 출력
cat "$FINAL" > "$OUT" || die2 "출력 실패: $OUT"
chmod 755 "$OUT" 2> /dev/null
SZ=$(wc -c < "$OUT"); SZ=$((SZ + 0))
SHA=
if command -v sha256sum > /dev/null 2>&1; then SHA=$(sha256sum "$OUT" | awk '{ print $1 }')
elif command -v shasum > /dev/null 2>&1; then SHA=$(shasum -a 256 "$OUT" | awk '{ print $1 }'); fi
printf '\n'
ok "생성 완료: $OUT"
info "크기 ${SZ} 바이트, ${TOTAL}줄, 변경 ${H_N}건"
[ -n "$SHA" ] && info "sha256: $SHA"
info "붙여넣기 잘림 확인: 마지막 줄이 #__END_OF_UPDATE__ 이고 줄 수가 ${TOTAL}이어야 합니다 (스크립트도 실행 전에 스스로 검사)"
info "현장 사용: bash $(basename "$OUT") [--dry]  /  되돌리기: bash $(basename "$OUT") --undo"
[ $NWARN -gt 0 ] || [ $NGW -gt 0 ] && warn "요약표의 경고(앵커 이동·범위 확장·마스킹 위반 예정)를 검토한 뒤 배포하십시오"
exit 0
