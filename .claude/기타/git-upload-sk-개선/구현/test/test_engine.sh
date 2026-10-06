#!/bin/bash
# test_engine.sh - templates/update_engine.sh 시나리오 테스트
# 사용: bash test_engine.sh      (마지막 줄: PASS=n FAIL=m)
# 픽스처는 전부 mktemp 로 만든다. 업데이트 스크립트는 생성물과 같은 형태(lib + 엔진 + MANIFEST/SPEC 블록 + main,
# 맨 위 set -u)로 조립해 `bash <스크립트>` 로 실행한다 → 블록 추출 경로도 실제와 같다.

HERE=$(cd "$(dirname "$0")" && pwd)
LIB="$HERE/../templates/lib_common.sh"
ENG="$HERE/../templates/update_engine.sh"

T=$(mktemp -d "${TMPDIR:-/tmp}/gus_etest.XXXXXX") || exit 2
trap 'rm -rf "$T"' EXIT

PASS=0; FAIL=0
pass() { PASS=$((PASS + 1)); }
fail() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$1"; [ -n "${2-}" ] && printf '      %s\n' "$2"; return 0; }
eq()    { if [ "$2" = "$3" ]; then pass; else fail "$1" "기대=[$2] 실제=[$3]"; fi; }
isok()  { local n=$1; shift; if "$@" > /dev/null 2>&1; then pass; else fail "$n" "명령 실패: $*"; fi; }
has()   { if grep -qF -- "$3" "$2"; then pass; else fail "$1" "'$3' 가 출력에 없음"; fi; }
hasnt() { if grep -qF -- "$3" "$2"; then fail "$1" "'$3' 가 출력에 있으면 안 됨"; else pass; fi; }
same()  { if cmp -s "$2" "$3"; then pass; else fail "$1" "$(diff "$2" "$3" | head -8 | tr '\n' '|')"; fi; }
nocr()  { if grep -q $'\r' "$2" 2> /dev/null; then fail "$1" "CR 이 남아 있음: $2"; else pass; fi; }
nobak() { if ls "$2".bak.* > /dev/null 2>&1; then fail "$1" "백업이 생기면 안 됨"; else pass; fi; }

# shellcheck source=/dev/null
. "$LIB"
# shellcheck source=/dev/null
. "$ENG"

OUT=$T/out
UNBOUND=0
run() {   # run <스크립트> <인자...>   (stdin 은 호출 측) → RC, $OUT
    bash "$@" > "$OUT" 2>&1
    RC=$?
    grep -q 'unbound variable' "$OUT" && { UNBOUND=1; sed -n '/unbound variable/p' "$OUT" | head -3; }
    return 0
}
show_out() { sed 's/^/      | /' "$OUT" | head -40; }

mkupd() {   # mkupd <manifest> <spec> <출력 스크립트>
    {
        echo '#!/bin/bash'
        echo 'set -u'
        cat "$LIB"
        cat "$ENG"
        engine_emit_block MANIFEST "$1"
        engine_emit_block SPEC "$2"
        echo 'engine_main "$@"; exit $?'
    } > "$3"
}

# ---------------------------------------------------------------- 픽스처
P=$T/proj
mkproj() {
    rm -rf "$P"; mkdir -p "$P/setup"
    cat > "$P/app.sh" <<'EOS'
#!/bin/bash
# app.sh - 점검 스크립트 (OO 현장 수정본)
# 담당자: OO
vendor="OO"
log_dir="/OO/log/app"   # 로그 경로 OO
checker="/opt/cc/config_check.sh"
gossh_pw='S3cr3t!x'
# OO팀 수정: 타임아웃 조정
timeout=45   # 현장 조정값
extra_opt=""

run_check() {
    echo "check $vendor"
    "$checker" -q
}

main() {
    run_check
    echo "점검부탁드립니다 - OO"
    echo "done"
}
main "$@"
EOS
    cp "$P/app.sh" "$T/app.orig"
    cat > "$P/setup/vars.manifest" <<'EOS'
# 현장에 있던 이전 manifest (v1.0.0)
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|none|1.0.0|-
gossh_pw|sh:app.sh|secret|common|-|n|gossh 비밀번호|-|none|1.0.0|-
extra_opt|sh:app.sh|text|common|-|n|기존 빈 변수|x|none|1.0.0|-
legacy_var|sh:app.sh|text|common|-|n|없어진 변수|x|none|1.0.0|-
EOS
}

cat > "$T/m1" <<'EOS'
#@meta project=demo
#@meta version=1.1.0
#@guard 현장
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|none|1.0.0|-
gossh_pw|sh:app.sh|secret|common|-|n|gossh 비밀번호|-|none|1.0.0|-
auto_done_host|sh:app.sh|host|common|-|n|원격 호스트|os8mgmt|host|1.1.0|-
extra_opt|sh:app.sh|text|common|-|n|기존 빈 변수|x|none|1.0.0|-
EOS

cat > "$T/s1" <<'EOS'
@file app.sh
#candidates ^extra_opt= | ^timeout=
@change c1 insert_after
@anchor ^extra_opt=
@text
auto_done_host=""   # 완료기록 원격 호스트 (v1.1)
@endtext

@change c2 insert_before
@anchor ^main "\$@"$
@text
report() {
    echo "report $log_dir"
}

@endtext

@change c3 replace_line
@anchor ^    echo "done"$
@text
    echo "done v1.1"
@endtext

@change c4 replace_range
@anchor ^run_check\(\) \{$
@anchor_end ^\}$
@text
run_check() {
    echo "check $vendor"
    "$checker" -q -auto
}
@endtext

@change c5 replace_line
@anchor ^checker=
@text
checker=""   # 체크 스크립트 경로 (v1.1)
@endtext
EOS

cat > "$T/expected" <<'EOS'
#!/bin/bash
# app.sh - 점검 스크립트 (OO 현장 수정본)
# 담당자: OO
vendor="OO"
log_dir="/OO/log/app"   # 로그 경로 OO
checker="/opt/cc/config_check.sh"   # 체크 스크립트 경로 (v1.1)
gossh_pw='S3cr3t!x'
# OO팀 수정: 타임아웃 조정
timeout=45   # 현장 조정값
extra_opt=""
auto_done_host="os8mgmt"   # 완료기록 원격 호스트 (v1.1)

run_check() {
    echo "check $vendor"
    "$checker" -q -auto
}

main() {
    run_check
    echo "점검부탁드립니다 - OO"
    echo "done v1.1"
}
report() {
    echo "report $log_dir"
}

main "$@"
EOS

jbak() { awk -F'\t' 'NR == 1 { print $3 }' "$P/.update_journal" 2> /dev/null; }

# 단일 변경 명세로 스크립트 만들기: spec1 <id> <op> <anchor> <text...>  (anchor_end 는 AEND 변수)
spec1() {
    local id=$1 op=$2 anc=$3; shift 3
    {
        echo "@file app.sh"
        echo "@change $id $op"
        echo "@anchor $anc"
        [ -n "${AEND:-}" ] && echo "@anchor_end $AEND"
        echo "@text"
        printf '%s\n' "$@"
        echo "@endtext"
    } > "$T/s_one"
    mkupd "$T/m1" "$T/s_one" "$P/setup/u_one.sh"
}

# ================================================================ 0. 정적 검사
isok "bash -n engine" bash -n "$ENG"
nocr "engine 에 CR 없음" "$ENG"
nocr "test 에 CR 없음" "$HERE/test_engine.sh"
n=$(grep -v '^[[:space:]]*#' "$ENG" | grep -cE '\b(python[0-9]*|perl)\b|local -n|declare -g|\$\{[A-Za-z_]+\^\^|\[\[ -v |mapfile -d|read -N|;;&|\|&')
eq "금지 기능(python/perl/4.2+) 없음" 0 "$n"

# ================================================================ 1. 블록 삽입·추출
mkproj
UPD=$P/setup/update_v1.1.0.sh
mkupd "$T/m1" "$T/s1" "$UPD"
isok "생성 스크립트 bash -n" bash -n "$UPD"
engine_extract_block "$UPD" SPEC > "$T/x.spec" 2> /dev/null
same "SPEC 블록 추출 = 원문" "$T/s1" "$T/x.spec"
engine_extract_block "$UPD" MANIFEST > "$T/x.man" 2> /dev/null
same "MANIFEST 블록 추출 = 원문" "$T/m1" "$T/x.man"
head -n -3 "$UPD" > "$T/trunc.sh"
engine_extract_block "$T/trunc.sh" SPEC > /dev/null 2>&1; eq "잘린 스크립트: SPEC 추출 실패" 1 $?
printf 'x\n#__SPEC_END__\n' > "$T/bad.blk"
engine_emit_block SPEC "$T/bad.blk" > /dev/null 2>&1; eq "마커 줄을 품은 내용은 emit 거부" 1 $?

# ================================================================ 2. --dry
run "$UPD" --dir "$P" --dry < /dev/null
eq "dry rc" 0 "$RC"
same "dry: 파일 불변" "$T/app.orig" "$P/app.sh"
nobak "dry: 백업 없음" "$P/app.sh"
if [ -e "$P/.update_journal" ]; then fail "dry: 저널 없음"; else pass; fi
has "dry: diff 표시" "$OUT" '+auto_done_host=""'
has "dry: 새 변수 예고" "$OUT" '질문할 새 변수: auto_done_host'
has "dry: 검증 요약" "$OUT" '주석·코멘트 문구 변경 0건'
hasnt "dry: secret 미노출" "$OUT" 'S3cr3t'
[ "$RC" = 0 ] || show_out

# ================================================================ 3. 현장 사본 적용 (기본 --dir = 스크립트 상위)
printf 'os8mgmt\n' > "$T/in"
run "$UPD" --yes < "$T/in"
eq "apply rc" 0 "$RC"
same "apply: 결과 = 기대(현장 수정 보존 + 의도한 변경만)" "$T/expected" "$P/app.sh"
has "apply: 5건 적용" "$OUT" '변경 적용 5건'
has "apply: 요약 0건" "$OUT" '주석·코멘트 문구 변경 0건'
has "apply: 기존 값 유지 (선언 줄이 바뀌어도)" "$OUT" '기존 값 유지: checker'
has "apply: 새 변수만 질문" "$OUT" '[i] auto_done_host  ('
hasnt "apply: 기존 값 있는 변수 질문 안 함" "$OUT" '[i] checker  ('
hasnt "apply: 이전부터 빈 변수 질문 안 함" "$OUT" '[i] extra_opt  ('
has "apply: 이전부터 빈 변수 안내" "$OUT" '이전부터 비어 있던 변수 1개'
has "apply: manifest 에서 빠진 변수 경고" "$OUT" '빠진 변수: legacy_var'
has "apply: Disclaimer" "$OUT" 'Disclaimer'
has "apply: --undo 안내" "$OUT" '--undo'
hasnt "apply: secret 미노출" "$OUT" 'S3cr3t'
b=$(jbak)
if [ -n "$b" ] && [ -f "$b" ]; then same "apply: 저널 백업 = 원본" "$T/app.orig" "$b"; else fail "apply: 저널에 백업 기록"; fi
[ "$RC" = 0 ] || show_out
nb1=$(ls "$P"/app.sh.bak.* 2> /dev/null | wc -l)

# ================================================================ 4. 멱등 (두 번째 실행)
run "$UPD" --dir "$P" --yes < /dev/null
eq "2회차 rc" 0 "$RC"
same "2회차: 파일 불변" "$T/expected" "$P/app.sh"
eq "2회차: 이미 적용됨 5건" 5 "$(grep -c '이미 적용됨 — 건너뜀' "$OUT")"
has "2회차: 변경 없음" "$OUT" '변경 없음'
eq "2회차: 새 백업 없음" "$nb1" "$(ls "$P"/app.sh.bak.* 2> /dev/null | wc -l)"

# ================================================================ 5. --undo
run "$UPD" --dir "$P" --undo < /dev/null
eq "undo rc" 0 "$RC"
same "undo: 원본 복원" "$T/app.orig" "$P/app.sh"
run "$UPD" --dir "$P" --undo < /dev/null
eq "undo 2회 rc" 0 "$RC"
has "undo 2회: 이미 되돌림" "$OUT" '이미 되돌렸습니다'
same "undo 2회: 그대로" "$T/app.orig" "$P/app.sh"

# ================================================================ 6. CRLF 입력
mkproj
sed 's/$/\r/' "$T/app.orig" > "$P/app.sh"; cp "$P/app.sh" "$T/app.crlf"
mkupd "$T/m1" "$T/s1" "$UPD"
run "$UPD" --dir "$P" --yes < "$T/in"
eq "CRLF rc" 0 "$RC"
same "CRLF: 결과 = 기대(LF)" "$T/expected" "$P/app.sh"
nocr "CRLF: 결과에 CR 없음" "$P/app.sh"
b=$(jbak); [ -n "$b" ] && same "CRLF: 백업은 원본 바이트 그대로" "$T/app.crlf" "$b"
run "$UPD" --dir "$P" --undo < /dev/null
same "CRLF: undo 로 CRLF 원본 바이트 복원" "$T/app.crlf" "$P/app.sh"

# ================================================================ 7. 앵커 모호/없음 → 중단, 파일 바이트 불변
mkproj
spec1 amb insert_after '^(gossh_pw|vendor)=' 'x=1'
run "$P/setup/u_one.sh" --dir "$P" --yes < /dev/null
eq "모호 앵커 rc" 1 "$RC"
same "모호 앵커: 파일 불변" "$T/app.orig" "$P/app.sh"
has "모호 앵커: id·개수" "$OUT" '[amb] 앵커 일치 2곳'
has "모호 앵커: 줄 번호" "$OUT" ': 4 7'
hasnt "모호 앵커: secret 미노출" "$OUT" 'S3cr3t'
nobak "모호 앵커: 백업 없음" "$P/app.sh"

spec1 miss replace_line '^no_such_line=' 'no_such_line=1'
run "$P/setup/u_one.sh" --dir "$P" --yes < /dev/null
eq "없는 앵커 rc" 1 "$RC"
same "없는 앵커: 파일 불변" "$T/app.orig" "$P/app.sh"
has "없는 앵커: 0곳" "$OUT" '[miss] 앵커 일치 0곳'

# ================================================================ 8. 마스킹 가드 → 중단
AEND='^timeout=' spec1 rmc replace_range '^gossh_pw=' "gossh_pw=''" 'timeout=60'
run "$P/setup/u_one.sh" --dir "$P" --yes < /dev/null
eq "주석 줄 삭제 rc" 1 "$RC"
same "주석 줄 삭제: 파일 불변" "$T/app.orig" "$P/app.sh"
has "주석 줄 삭제: 위반 표시" "$OUT" '[rmc] 마스킹 위반(주석 줄 또는 가드 키워드): 원본 8번째 줄'
hasnt "주석 줄 삭제: secret 미노출" "$OUT" 'S3cr3t'

spec1 grd replace_line '^timeout=' 'timeout=60   # 기본값'
run "$P/setup/u_one.sh" --dir "$P" --yes < /dev/null
eq "manifest #@guard 키워드 rc" 1 "$RC"
same "manifest #@guard: 파일 불변" "$T/app.orig" "$P/app.sh"

spec1 kw replace_line '점검부탁' '    echo "확인 요청"'
run "$P/setup/u_one.sh" --dir "$P" --yes < /dev/null
eq "기본 가드(점검부탁) 코드 줄 rc" 1 "$RC"
same "기본 가드: 파일 불변" "$T/app.orig" "$P/app.sh"

spec1 tc replace_line '^log_dir=' 'log_dir="/var/log/app"   # 로그 경로'
run "$P/setup/u_one.sh" --dir "$P" --yes < /dev/null
eq "줄 끝 코멘트 변경 rc" 1 "$RC"
has "줄 끝 코멘트 변경: 이유" "$OUT" '줄 끝 코멘트가 바뀜'
same "줄 끝 코멘트 변경: 파일 불변" "$T/app.orig" "$P/app.sh"

spec1 tc2 replace_line '^log_dir=' 'log_dir="/var/log/app2"   # 로그 경로 OO'
run "$P/setup/u_one.sh" --dir "$P" --yes < /dev/null
eq "코멘트 유지한 코드 변경은 허용 rc" 0 "$RC"
has "코멘트 유지한 코드 변경: 적용" "$P/app.sh" 'log_dir="/var/log/app2"   # 로그 경로 OO'
mkproj

# ================================================================ 9. 문법을 깨는 명세 → 복구(불변)
spec1 syn insert_after '^extra_opt=' 'if [ -n "$x" ]; then'
run "$P/setup/u_one.sh" --dir "$P" --yes < /dev/null
eq "문법 오류 rc" 1 "$RC"
has "문법 오류: bash -n 표시" "$OUT" '문법 검사(bash -n) 실패'
same "문법 오류: 파일 불변" "$T/app.orig" "$P/app.sh"
nobak "문법 오류: 백업 없음" "$P/app.sh"

# ================================================================ 10. renamed_from 값 이전 + secret 마스킹
cat > "$T/m2" <<'EOS'
#@meta version=1.2.0
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|none|1.0.0|-
gossh_pass|sh:app.sh|secret|common|-|n|gossh 비밀번호|-|none|1.2.0|gossh_pw
extra_opt|sh:app.sh|text|common|-|n|기존 빈 변수|x|none|1.0.0|-
EOS
printf '@file app.sh\n@change ren replace_line\n@anchor ^gossh_pw=\n@text\ngossh_pass=%s\n@endtext\n' "''" > "$T/s2"
mkupd "$T/m2" "$T/s2" "$P/setup/u2.sh"
run "$P/setup/u2.sh" --dir "$P" --dry < /dev/null
eq "rename dry rc" 0 "$RC"
hasnt "rename dry: secret 미노출" "$OUT" 'S3cr3t'
has "rename dry: 마스킹 표시" "$OUT" 'gossh_pass=****'
run "$P/setup/u2.sh" --dir "$P" --yes < /dev/null
eq "rename rc" 0 "$RC"
has "rename: 값 이전" "$P/app.sh" 'gossh_pass="S3cr3t!x"'
hasnt "rename: 이전 선언 제거" "$P/app.sh" 'gossh_pw='
has "rename: 이전 메시지(마스킹)" "$OUT" '값 이전: gossh_pw -> gossh_pass (****)'
hasnt "rename: 이전된 변수는 질문 안 함" "$OUT" '[i] gossh_pass  ('
hasnt "rename: 이름 바뀐 변수는 빠진 변수 경고 안 함" "$OUT" '빠진 변수: gossh_pw'
hasnt "rename: secret 미노출" "$OUT" 'S3cr3t'
[ "$RC" = 0 ] || show_out

# ================================================================ 11. 새 변수 입력 실패(EOF) → 전체 원상복구
mkproj
mkupd "$T/m1" "$T/s1" "$UPD"
run "$UPD" --dir "$P" --yes < /dev/null
eq "가이드 EOF rc" 1 "$RC"
same "가이드 EOF: 원상복구" "$T/app.orig" "$P/app.sh"
has "가이드 EOF: 복구 표시" "$OUT" '원상복구'

# ================================================================ 12. 확인 질문에 n → 불변
nb1=$(ls "$P"/app.sh.bak.* 2> /dev/null | wc -l)
run "$UPD" --dir "$P" < <(printf 'n\n')
eq "확인 n rc" 1 "$RC"
same "확인 n: 파일 불변" "$T/app.orig" "$P/app.sh"
eq "확인 n: 새 백업 없음" "$nb1" "$(ls "$P"/app.sh.bak.* 2> /dev/null | wc -l)"

# ================================================================ 13. 대상 파일 인자 (다른 위치의 현장 사본)
mkdir -p "$T/field"; cp "$T/app.orig" "$T/field/app_copy.sh"
run "$UPD" "$T/field/app_copy.sh" --dir "$P" --yes < "$T/in"
eq "대상 파일 인자 rc" 0 "$RC"
same "대상 파일 인자: 사본에 적용" "$T/expected" "$T/field/app_copy.sh"
same "대상 파일 인자: 루트 파일은 그대로" "$T/app.orig" "$P/app.sh"
[ "$RC" = 0 ] || show_out

# ================================================================ 14. 명세 오류 → rc 2
printf '@file app.sh\n@change bad replace_range\n@anchor ^vendor=\n@text\nx\n@endtext\n' > "$T/s3"
mkupd "$T/m1" "$T/s3" "$P/setup/u3.sh"
run "$P/setup/u3.sh" --dir "$P" --yes < /dev/null
eq "anchor_end 없는 replace_range rc" 2 "$RC"
printf '@file app.sh\n@bogus\n' > "$T/s4"
mkupd "$T/m1" "$T/s4" "$P/setup/u4.sh"
run "$P/setup/u4.sh" --dir "$P" --yes < /dev/null
eq "알 수 없는 지시자 rc" 2 "$RC"
run "$UPD" --dir "$T/nonexist" < /dev/null
eq "없는 --dir rc" 2 "$RC"
same "명세 오류: 파일 불변" "$T/app.orig" "$P/app.sh"

# ================================================================ 15. set -u
eq "set -u: unbound variable 없음" 0 "$UNBOUND"

# ================================================================ 16. 택일(xor) 변수 + 재실행: 비워 둔 구성원을 다시 묻지 않는다
cat > "$T/mx" <<'EOS'
#@meta project=demo
#@meta version=1.1.0
#@xor g|어느 쪽을 쓰나요?|xa:A 쪽|xb:B 쪽
xa|sh:app.sh|text|common|xor:g|n|A 값|a|none|1.1.0|-
xb|sh:app.sh|text|common|xor:g|n|B 값|b|none|1.1.0|-
EOS
cat > "$T/sx" <<'EOS'
@file app.sh
@change x1 insert_after
@anchor ^extra_opt=
@text
xa=""
xb=""
@endtext
EOS
mkproj
mkupd "$T/mx" "$T/sx" "$UPD"
run "$UPD" --dir "$P" --yes < <(printf '2\nbval\n')
eq "xor 재실행: 첫 실행 rc" 0 "$RC"
eq "xor 재실행: 선택한 xb 값 적용" 'xb="bval"' "$(grep '^xb=' "$P/app.sh")"
eq "xor 재실행: 선택 안 한 xa 는 빈 값" 'xa=""' "$(grep '^xa=' "$P/app.sh")"
cp "$P/app.sh" "$T/x.after1"; cp "$P/.update_journal" "$T/x.journal1"
nbx=$(ls "$P"/app.sh.bak.* | wc -l)
run "$UPD" --dir "$P" --yes < /dev/null
eq "xor 재실행: 2회차 rc 0 (비워 둔 xa 를 다시 묻지 않음)" 0 "$RC"
has "xor 재실행: 2회차 변경 없음" "$OUT" '변경 없음'
hasnt "xor 재실행: 2회차에 값 질문 없음" "$OUT" '값 입력'
same "xor 재실행: 2회차 파일 불변" "$T/x.after1" "$P/app.sh"
eq "xor 재실행: 새 백업 없음" "$nbx" "$(ls "$P"/app.sh.bak.* | wc -l)"
same "xor 재실행: 저널 그대로(--undo 정보 보존)" "$T/x.journal1" "$P/.update_journal"
run "$UPD" --dir "$P" --undo
same "xor 재실행: --undo 는 업데이트 전 원본으로 복원" "$T/app.orig" "$P/app.sh"
# 둘 다 비워 둔 경우(0=사용 안 함)는 아직 미설정이므로 재실행 때 다시 묻는다 (기존 동작 유지)
mkproj
mkupd "$T/mx" "$T/sx" "$UPD"
run "$UPD" --dir "$P" --yes < <(printf '0\n')
eq "xor 둘 다 빈 값: 첫 실행 rc" 0 "$RC"
run "$UPD" --dir "$P" --yes < <(printf '0\n')
has "xor 둘 다 빈 값: 재실행에서 택일 질문을 다시 함" "$OUT" '어느 쪽을 쓰나요?'
# 다른 구성원이 값을 이미 갖고 있으면(현장이 직접 채움) 처음부터 질문하지 않는다
cat > "$T/sx2" <<'EOS'
@file app.sh
@change x1 insert_after
@anchor ^extra_opt=
@text
xa=""
@endtext
EOS
mkproj
sed -i 's|^extra_opt=""$|extra_opt=""\nxb="mine"|' "$P/app.sh"
mkupd "$T/mx" "$T/sx2" "$UPD"
run "$UPD" --dir "$P" --yes < /dev/null
eq "xor: 현장이 이미 xb 를 채운 사본 rc" 0 "$RC"
hasnt "xor: 이미 채워진 구성원이 있으면 질문 없음" "$OUT" '값 입력'

echo "PASS=$PASS FAIL=$FAIL"
