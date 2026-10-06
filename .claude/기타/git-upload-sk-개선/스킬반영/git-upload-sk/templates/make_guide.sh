#!/bin/bash
# make_guide.sh - setup_guide.sh 에 lib_common.sh 를 인라인해 자체포함 단일 가이드 스크립트를 생성한다 (개발자용)
#
# 사용:
#   bash make_guide.sh --out <setup_guide.sh 생성물> [--guide <setup_guide.sh 틀>] [--lib <lib_common.sh>]
#   --out    생성할 자체포함 가이드 (프로젝트의 setup/ 에 둔다)
#   --guide  틀 (기본: 이 스크립트와 같은 디렉터리의 setup_guide.sh)
#   --lib    인라인할 공용 함수 (기본: 이 스크립트와 같은 디렉터리의 lib_common.sh)
#   종료코드: 0 생성 완료, 2 인자·파일 오류, 3 생성물 검사 실패(파일을 만들지 않음)
#
# 동작:
#   1) 틀에서 줄 끝에 마커 #__LIB_INCLUDE__ 가 있는 줄(정확히 1개; 설명 주석 속 언급은 세지 않음)을 lib_common.sh 내용으로 치환한다.
#      (lib 의 첫 줄 #! 는 뺀다. 인라인 위치는 최상위라서 lib 의 declare -A 가 전역이 된다.)
#   2) 머리에 "붙여넣기 잘림 검사"(줄 수 + 끝줄 #__END_OF_GUIDE__)를 붙인다 — update_v<ver>.sh 와 같은 방식.
#   3) 생성물 검사: bash -n, CR 없음, 마커 1회, lib 함수 존재. 하나라도 실패하면 파일을 만들지 않는다.
#   생성물은 lib_common.sh 없이 단독으로 동작한다 (setup_guide.sh 틀을 lib 와 함께 둔 것과 동작이 같다).
#
# 제약: bash 4.1 + sed/awk/grep/coreutils.

HERE=$(cd "$(dirname "$0")" && pwd)
GUIDE=$HERE/setup_guide.sh
LIB=$HERE/lib_common.sh
OUT=

err() { printf '[X] %s\n' "$*" >&2; }
ok()  { printf '[O] %s\n' "$*"; }
info() { printf '[i] %s\n' "$*"; }
die2() { err "$*"; exit 2; }
die3() { err "$*"; exit 3; }

while [ $# -gt 0 ]; do
    case "$1" in
        --out|--guide|--lib)
            [ $# -ge 2 ] || die2 "$1 값이 없습니다"
            case "$1" in --out) OUT=$2 ;; --guide) GUIDE=$2 ;; --lib) LIB=$2 ;; esac
            shift ;;
        -h|--help)
            sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) die2 "알 수 없는 인자: $1" ;;
    esac
    shift
done
[ -n "$OUT" ] || die2 "--out 은 필수입니다"
[ -f "$GUIDE" ] || die2 "틀(setup_guide.sh) 없음: $GUIDE"
[ -f "$LIB" ] || die2 "lib_common.sh 없음: $LIB"
OUTDIR=$(dirname "$OUT")
[ -d "$OUTDIR" ] || die2 "--out 디렉터리 없음: $OUTDIR"

TD=$(mktemp -d "${TMPDIR:-/tmp}/gus_mkg.XXXXXX") || die2 "임시 디렉터리 생성 실패"
trap 'rm -rf "$TD"' EXIT
chmod 700 "$TD"

MARK='#__LIB_INCLUDE__'          # 마커는 "줄 끝"에 있는 것만 센다 (틀의 설명 주석에도 이 문자열이 나온다)
MARK_RE='[[:space:]]#__LIB_INCLUDE__[[:space:]]*$'
tr -d '\r' < "$GUIDE" | LC_ALL=C awk 1 > "$TD/guide"
tr -d '\r' < "$LIB" | LC_ALL=C awk 1 > "$TD/lib"

n=$(grep -c -- "$MARK_RE" "$TD/guide")
[ "$n" = 1 ] || die2 "틀에 $MARK 마커 줄이 정확히 1개여야 합니다 (현재 ${n}개): $GUIDE"
grep -q -- "$MARK_RE" "$TD/lib" && die2 "lib 안에 $MARK 마커가 있습니다: $LIB"
grep -qx '#__END_OF_GUIDE__' "$TD/guide" && die2 "틀에 #__END_OF_GUIDE__ 줄이 있습니다: $GUIDE"
ML=$(grep -n -- "$MARK_RE" "$TD/guide" | sed 's/:.*//')

# 본문: 마커 앞 / lib(첫 줄 #! 제외) / 마커 뒤 / 끝줄
{
    head -n $((ML - 1)) "$TD/guide" | sed '1{/^#!/d;}'
    printf '# ===== lib_common.sh (인라인) =====\n'
    sed '1{/^#!/d;}' "$TD/lib"
    printf '# ===== lib_common.sh 끝 =====\n'
    tail -n +$((ML + 1)) "$TD/guide"
    printf '#__END_OF_GUIDE__\n'
} > "$TD/body"

build_header() {   # build_header <총 줄 수(6자리)>
    cat <<EOS
#!/bin/bash
# setup_guide.sh - 설정 가이드 (make_guide.sh 로 자동 생성, $(date '+%Y-%m-%d %H:%M')) — 직접 고치지 말고 틀을 고친 뒤 다시 생성한다.
# 이 파일은 자체포함 단일 스크립트다 (lib_common.sh 내용을 인라인). vars.manifest 는 이 스크립트와 같은 디렉터리에서 읽는다.
# 붙여넣기 확인: 마지막 줄이 #__END_OF_GUIDE__ 이고 전체 줄 수가 아래 값(${1})이어야 한다 — 아니면 실행하지 않는다.
GUS_EXPECT_LINES=${1}
_gus_selfcheck() {
    local n l
    if [ ! -f "\$0" ]; then
        printf '[X] 파일로 저장한 뒤 bash <파일> 로 실행하십시오 (표준입력·프로세스 치환으로는 실행할 수 없음)\n' >&2; return 1
    fi
    n=\$(awk 'END { print NR }' "\$0")
    l=\$(tail -n 1 "\$0" | tr -d '\r')
    if [ "\$l" != '#__END_OF_GUIDE__' ] || [ "\$n" != "\$((10#\$GUS_EXPECT_LINES))" ]; then
        printf '[X] 스크립트가 잘렸거나 변형되었습니다 (줄 수 %s, 기대 %s, 마지막 줄 [%s]) — 다시 복사해 주십시오.\n' "\$n" "\$((10#\$GUS_EXPECT_LINES))" "\${l:0:30}" >&2
        return 1
    fi
    return 0
}
_gus_selfcheck || exit 2
unset -f _gus_selfcheck
EOS
}

build_header 000000 > "$TD/hdr"
HL=$(wc -l < "$TD/hdr"); HL=$((HL + 0))
BL=$(wc -l < "$TD/body"); BL=$((BL + 0))
TOTAL=$((HL + BL))
{ build_header "$(printf '%06d' "$TOTAL")"; cat "$TD/body"; } > "$TD/final"

# 검사
bash -n "$TD/final" 2> "$TD/syn" || { sed 's/^/    /' "$TD/syn" | head -n 5 >&2; die3 "생성물이 bash -n 에 실패했습니다"; }
grep -q $'\r' "$TD/final" && die3 "생성물에 CR 이 있습니다"
[ "$(tail -n 1 "$TD/final")" = '#__END_OF_GUIDE__' ] || die3 "끝줄 마커 오류"
[ "$(awk 'END { print NR }' "$TD/final")" = "$TOTAL" ] || die3 "줄 수 계산 불일치"
[ "$(grep -c -- "$MARK_RE" "$TD/final")" = 0 ] || die3 "치환되지 않은 $MARK 가 남았습니다"
for f in ui_init lf_normalize backup_file restore_latest manifest_load target_get target_set check_value ask_var run_guide summary_table confirm; do
    grep -q "^${f}()" "$TD/final" || die3 "lib 함수 ${f}() 가 생성물에 없습니다"
done
[ "$(grep -c '^LIB_COMMON_VERSION=' "$TD/final")" = 1 ] || die3 "lib 가 정확히 1번 포함되지 않았습니다"

cat "$TD/final" > "$OUT" || die2 "출력 실패: $OUT"
chmod 755 "$OUT" 2> /dev/null
SZ=$(wc -c < "$OUT"); SZ=$((SZ + 0))
ok "생성 완료: $OUT"
info "크기 ${SZ} 바이트, ${TOTAL}줄 (끝줄 #__END_OF_GUIDE__ 로 붙여넣기 잘림을 스스로 검사)"
exit 0
