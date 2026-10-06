#!/bin/bash
# test_make_guide.sh - templates/make_guide.sh (자체포함 가이드 생성기) 테스트
# 사용: bash test_make_guide.sh      (마지막 줄: PASS=n FAIL=m)
#
# 핵심 검증: 생성된 자체포함 가이드가 lib_common.sh 를 source 하는 틀과 "같게" 동작하는지 —
#   test_guide.sh 를 임시 트리로 복사하고 templates/setup_guide.sh 자리에 생성물을 놓은 뒤(lib_common.sh 는
#   실행하면 즉시 종료하는 독약으로 바꿔 둔다) 그대로 돌려 PASS/FAIL 이 원본과 같은지 비교한다.

HERE=$(cd "$(dirname "$0")" && pwd)
TPL="$HERE/../templates"
MG="$TPL/make_guide.sh"

T=$(mktemp -d "${TMPDIR:-/tmp}/gus_mkg_test.XXXXXX") || exit 2
trap 'rm -rf "$T"' EXIT

PASS=0; FAIL=0
pass() { PASS=$((PASS + 1)); }
fail() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$1"; [ -n "${2-}" ] && printf '      %s\n' "$2"; return 0; }
eq()    { if [ "$2" = "$3" ]; then pass; else fail "$1" "기대=[$2] 실제=[$3]"; fi; }
isok()  { local n=$1; shift; if "$@" > /dev/null 2>&1; then pass; else fail "$n" "명령 실패: $*"; fi; }
has()   { if grep -qF -- "$3" "$2"; then pass; else fail "$1" "'$3' 가 $2 에 없음"; fi; }
hasnt() { if grep -qF -- "$3" "$2"; then fail "$1" "'$3' 가 $2 에 있으면 안 됨"; else pass; fi; }
nocr()  { if grep -q $'\r' "$2" 2> /dev/null; then fail "$1" "CR 이 남아 있음: $2"; else pass; fi; }
absent() { if [ -e "$2" ]; then fail "$1" "파일이 있으면 안 됨: $2"; else pass; fi; }

# ================================================================ 0. 정적 검사
isok "bash -n make_guide.sh" bash -n "$MG"
nocr "make_guide.sh 에 CR 없음" "$MG"
nocr "test_make_guide.sh 에 CR 없음" "$HERE/test_make_guide.sh"
n=$(grep -v '^[[:space:]]*#' "$MG" | grep -cE '\b(python[0-9]*|perl)\b|local -n|declare -g|\$\{[A-Za-z_]+\^\^|\[\[ -v |mapfile -d|read -N|;;&|\|&')
eq "금지 기능(python/perl/4.2+) 없음" 0 "$n"

# ================================================================ 1. 생성
G=$T/setup_guide.gen.sh
bash "$MG" --out "$G" > "$T/g.out" 2>&1
eq "생성 rc" 0 "$?"
has "생성 완료 출력" "$T/g.out" "생성 완료"
isok "생성물 bash -n" bash -n "$G"
nocr "생성물에 CR 없음" "$G"
eq "마커 줄이 남지 않음(줄 끝 마커)" 0 "$(grep -c '[[:space:]]#__LIB_INCLUDE__[[:space:]]*$' "$G")"
eq "lib 는 정확히 1번 포함" 1 "$(grep -c '^LIB_COMMON_VERSION=' "$G")"
eq "lib 함수 run_guide 포함" 1 "$(grep -c '^run_guide()' "$G")"
eq "lib 의 #! 줄은 빠짐(shebang 은 맨 위 1개)" 1 "$(grep -c '^#!/bin/bash' "$G")"
eq "끝줄 마커" '#__END_OF_GUIDE__' "$(tail -n 1 "$G")"
n_exp=$(sed -n 's/^GUS_EXPECT_LINES=//p' "$G")
eq "머리의 기대 줄 수 = 실제 줄 수" "$((10#$n_exp))" "$(awk 'END { print NR }' "$G")"
has "틀의 본문 보존(가이드 main)" "$G" "== 최종 요약 =="
hasnt "source 줄 제거됨" "$G" '. "$HERE/lib_common.sh"'
if [ -x "$G" ]; then pass; else fail "생성물 실행 권한"; fi

# 같은 입력이면 같은 결과 (날짜 줄 제외)
G2=$T/setup_guide.gen2.sh
bash "$MG" --out "$G2" > /dev/null 2>&1
if [ "$(sed 2d "$G")" = "$(sed 2d "$G2")" ]; then pass; else fail "재생성 결과가 같아야 함(2번째 줄=날짜 제외)"; fi

# ================================================================ 2. 인자 오류·입력 검사 (생성 안 됨)
bash "$MG" > "$T/o" 2>&1; eq "--out 없음 → 2" 2 "$?"
bash "$MG" --out "$T/nodir/x.sh" > "$T/o" 2>&1; eq "--out 디렉터리 없음 → 2" 2 "$?"
bash "$MG" --out "$T/x.sh" --guide "$T/none.sh" > "$T/o" 2>&1; eq "틀 없음 → 2" 2 "$?"
bash "$MG" --out "$T/x.sh" --lib "$T/none.sh" > "$T/o" 2>&1; eq "lib 없음 → 2" 2 "$?"
bash "$MG" --bogus > "$T/o" 2>&1; eq "알 수 없는 인자 → 2" 2 "$?"
absent "오류 시 파일 안 만듦" "$T/x.sh"
# 마커 0개 / 2개
grep -v '#__LIB_INCLUDE__' "$TPL/setup_guide.sh" > "$T/g_nomark.sh"
bash "$MG" --out "$T/x.sh" --guide "$T/g_nomark.sh" > "$T/o" 2>&1; eq "마커 없음 → 2" 2 "$?"
has "마커 없음 안내" "$T/o" "정확히 1개"
{ cat "$TPL/setup_guide.sh"; echo '# x  #__LIB_INCLUDE__'; } > "$T/g_two.sh"
bash "$MG" --out "$T/x.sh" --guide "$T/g_two.sh" > "$T/o" 2>&1; eq "마커 2개 → 2" 2 "$?"
# lib 에서 필수 함수가 빠지면 생성 거부(3)
grep -v '^confirm()' "$TPL/lib_common.sh" > "$T/lib_nofn.sh"
bash "$MG" --out "$T/x.sh" --lib "$T/lib_nofn.sh" > "$T/o" 2>&1; eq "lib 함수 누락 → 3" 3 "$?"
absent "lib 누락 시 파일 안 만듦" "$T/x.sh"
# CRLF 틀/lib 도 처리 (결과는 LF)
sed 's/$/\r/' "$TPL/setup_guide.sh" > "$T/g_crlf.sh"; sed 's/$/\r/' "$TPL/lib_common.sh" > "$T/lib_crlf.sh"
bash "$MG" --out "$T/x_crlf.sh" --guide "$T/g_crlf.sh" --lib "$T/lib_crlf.sh" > "$T/o" 2>&1; eq "CRLF 입력 rc" 0 "$?"
nocr "CRLF 입력 → 생성물은 LF" "$T/x_crlf.sh"
isok "CRLF 입력 생성물 bash -n" bash -n "$T/x_crlf.sh"

# ================================================================ 3. 붙여넣기 잘림 감지
head -n 120 "$G" > "$T/trunc.sh"
mkdir -p "$T/tp/setup"; cp "$T/trunc.sh" "$T/tp/setup/setup_guide.sh"
printf 'x|y\n' > "$T/tp/setup/vars.manifest"
(cd "$T/tp/setup" && bash setup_guide.sh < /dev/null) > "$T/o" 2>&1; eq "잘린 가이드는 실행 거부 → 2" 2 "$?"
has "잘림 안내" "$T/o" "잘렸거나 변형"
sed 's/$/\r/' "$G" > "$T/tp/setup/setup_guide.sh"
printf '' | (cd "$T/tp/setup" && bash setup_guide.sh) > "$T/o" 2>&1
if grep -q '잘렸거나' "$T/o"; then fail "CRLF 로 붙여넣은 가이드를 잘림으로 오판"; else pass; fi
cat "$G" | (cd "$T/tp/setup" && bash /dev/stdin) > "$T/o" 2>&1; rc=$?
if [ $rc -ne 0 ]; then pass; else fail "stdin 으로 실행하면 거부해야 함" "rc=$rc"; fi
has "stdin 실행 안내" "$T/o" "파일로 저장한 뒤"

# ================================================================ 4. 동작 동일성: test_guide.sh 를 생성물로 재실행
# 원본(source 하는 틀) 결과
bash "$HERE/test_guide.sh" > "$T/orig.out" 2>&1
orig_last=$(tail -n 1 "$T/orig.out")
case "$orig_last" in PASS=*" FAIL=0") pass ;; *) fail "원본 test_guide.sh 가 먼저 통과해야 함" "$orig_last" ;; esac
# 임시 트리: test/test_guide.sh + templates/setup_guide.sh(생성물) + templates/lib_common.sh(독약)
mkdir -p "$T/tree/test" "$T/tree/templates"
cp "$HERE/test_guide.sh" "$T/tree/test/"
cp "$G" "$T/tree/templates/setup_guide.sh"
printf '#!/bin/bash\necho "POISON: lib_common.sh 를 source 하면 안 됨" >&2\nexit 77\n' > "$T/tree/templates/lib_common.sh"
bash "$T/tree/test/test_guide.sh" > "$T/gen.out" 2>&1
gen_last=$(tail -n 1 "$T/gen.out")
eq "생성물로 돌린 test_guide.sh 결과 = 원본 결과" "$orig_last" "$gen_last"
hasnt "독약 lib 가 실행된 흔적 없음" "$T/gen.out" "POISON"
if [ "$gen_last" != "${gen_last#PASS=}" ] && [ "${gen_last##* FAIL=}" = 0 ]; then pass; else fail "생성물 test_guide FAIL=0" "$gen_last"; fi
[ "${gen_last##* FAIL=}" = 0 ] || sed 's/^/      | /' "$T/gen.out" | head -30
# 출력 본문도 같은 화면인지: 가이드 직접 실행 비교 (같은 입력, 같은 픽스처)
mkdir -p "$T/cmp/a/setup" "$T/cmp/b/setup"
for d in a b; do
    printf '#!/bin/bash\nlabel=""\ncp=""\n' > "$T/cmp/$d/app.sh"
    printf '#@meta project=cmp\n#@meta version=1.0.0\nlabel|sh:app.sh|text|common|-|n|표시 이름|서버A|none|1.0.0|-\ncp|sh:app.sh|secret|common|-|n|비밀|-|none|1.0.0|-\n' > "$T/cmp/$d/setup/vars.manifest"
done
cp "$TPL/setup_guide.sh" "$TPL/lib_common.sh" "$T/cmp/a/setup/"
cp "$G" "$T/cmp/b/setup/setup_guide.sh"
for d in a b; do
    printf 'n\n' | (cd "$T/cmp/$d/setup" && NO_COLOR=1 bash setup_guide.sh) > "$T/cmp/$d.out1" 2>&1
    printf 'y\nval\nsec\ny\n' | (cd "$T/cmp/$d/setup" && NO_COLOR=1 bash setup_guide.sh --role common) > "$T/cmp/$d.out2" 2>&1
    sed -i "s|$T/cmp/$d|<P>|g; s|\.bak\.[0-9]*|.bak.<ts>|g" "$T/cmp/$d.out1" "$T/cmp/$d.out2"
done
if cmp -s "$T/cmp/a.out1" "$T/cmp/b.out1"; then pass; else fail "화면 출력 동일(빈 변수≤5, n)" "$(diff "$T/cmp/a.out1" "$T/cmp/b.out1" | head -4 | tr '\n' '|')"; fi
if cmp -s "$T/cmp/a.out2" "$T/cmp/b.out2"; then pass; else fail "화면 출력 동일(질문·적용)" "$(diff "$T/cmp/a.out2" "$T/cmp/b.out2" | head -4 | tr '\n' '|')"; fi
if cmp -s "$T/cmp/a/app.sh" "$T/cmp/b/app.sh"; then pass; else fail "적용 결과 동일"; fi
has "비교 실행에서 값이 적용됨" "$T/cmp/b/app.sh" 'label="val"'

printf 'PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
