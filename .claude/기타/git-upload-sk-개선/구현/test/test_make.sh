#!/bin/bash
# test_make.sh - templates/make_update.sh (생성기) 시나리오 테스트
# 사용: bash test_make.sh      (마지막 줄: PASS=n FAIL=m)
# 픽스처(이전/신규 소스 쌍)로 생성 → 생성된 스크립트를 손으로 고친 "현장 사본"에 실제 적용(--yes)해서
# 현장 수정이 바이트 단위로 보존되고 의도한 변경만 반영되는지 확인한다.

HERE=$(cd "$(dirname "$0")" && pwd)
MK="$HERE/../templates/make_update.sh"

T=$(mktemp -d "${TMPDIR:-/tmp}/gus_mtest.XXXXXX") || exit 2
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
exists() { if [ -f "$2" ]; then pass; else fail "$1" "파일 없음: $2"; fi; }
absent() { if [ -e "$2" ]; then fail "$1" "파일이 있으면 안 됨: $2"; else pass; fi; }

GOUT=$T/gout     # 생성기 출력
OUT=$T/out       # 생성된 스크립트 실행 출력
UNBOUND=0
gen() {   # gen <출력 스크립트> [추가 인자...]   입력: OLDS NEWS MAN (환경 변수 역할), 항상 --yes
    local o=$1; shift
    bash -u "$MK" --old "$OLDS" --new "$NEWS" --manifest "$MAN" --version 1.1.0 --file app.sh --out "$o" --yes "$@" > "$GOUT" 2>&1
    GRC=$?
    grep -q 'unbound variable' "$GOUT" && { UNBOUND=1; grep 'unbound variable' "$GOUT" | head -3; }
    return 0
}
run() {   # run <스크립트> <인자...> → RC, $OUT  (stdin 은 호출 측)
    bash "$@" > "$OUT" 2>&1
    RC=$?
    grep -q 'unbound variable' "$OUT" && { UNBOUND=1; grep 'unbound variable' "$OUT" | head -3; }
    return 0
}
show_g() { sed 's/^/      | /' "$GOUT" | head -50; }
show_o() { sed 's/^/      | /' "$OUT" | head -40; }
specof() {   # specof <생성 스크립트> -> $T/spec (SPEC 블록 원문)
    sed -n '/^#__SPEC_BEGIN__$/,/^#__SPEC_END__$/p' "$1" | sed '1d;$d' > "$T/spec"
}

P=$T/proj
mkfield() { rm -rf "$P"; mkdir -p "$P"; cp "$1" "$P/app.sh"; }

# ---------------------------------------------------------------- 픽스처
mkdir -p "$T/f"
cat > "$T/f/base.sh" <<'EOS'
#!/bin/bash
# app.sh - 점검 스크립트
# 담당자:
vendor=""
log_dir="/var/log/app"   # 로그 경로
checker=""
gossh_pw=''
# 타임아웃(초)
timeout=30
extra_opt=""

run_check() {
    echo "check $vendor"
    "$checker" -q
}

main() {
    run_check
    echo "점검부탁드립니다"
    echo "done"
}
main "$@"
EOS

# 현장 수정 (마스킹된 벤더 문자열·경로·주석·코멘트·비밀번호). 이전/신규 소스에 똑같이 적용하면 "기대 결과"가 된다.
cat > "$T/f/fe.sed" <<'EOS'
s|^vendor=""|vendor="OO"|
s|^log_dir="/var/log/app"   # 로그 경로$|log_dir="/OO/log/app"   # 로그 경로 OO|
s|^checker=""|checker="/opt/cc/config_check.sh"|
s|^gossh_pw=''$|gossh_pw='S3cr3t!x'|
s|^# 담당자:$|# 담당자: OO|
s|^# 타임아웃(초)$|# OO팀 수정: 타임아웃 조정|
s|^timeout=30$|timeout=45   # 현장 조정값|
s|^    echo "점검부탁드립니다"$|    echo "점검부탁드립니다 - OO"|
s|^# app.sh - 점검 스크립트$|# app.sh - 점검 스크립트 (OO 현장 수정본)|
EOS
fe() { sed -f "$T/f/fe.sed" "$1"; }

cat > "$T/f/m0" <<'EOS'
#@meta project=demo
#@meta version=1.1.0
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|none|1.0.0|-
gossh_pw|sh:app.sh|secret|common|-|n|gossh 비밀번호|-|none|1.0.0|-
EOS

# ================================================================ 0. 정적 검사
isok "bash -n make_update.sh" bash -n "$MK"
nocr "make_update.sh 에 CR 없음" "$MK"
nocr "test_make.sh 에 CR 없음" "$HERE/test_make.sh"
n=$(grep -v '^[[:space:]]*#' "$MK" | grep -cE '\b(python[0-9]*|perl)\b|local -n|declare -g|\$\{[A-Za-z_]+\^\^|\[\[ -v |mapfile -d|read -N|;;&|\|&')
eq "금지 기능(python/perl/4.2+) 없음" 0 "$n"

# ================================================================ 1. 삽입만 (insert-only)
OLDS=$T/f/base.sh; MAN=$T/f/m0; NEWS=$T/f/a.new
sed 's|^extra_opt=""$|extra_opt=""\nauto_done_host=""   # 완료기록 호스트|' "$OLDS" > "$NEWS"
UPD=$T/u_a.sh
gen "$UPD"
eq "A 생성 rc" 0 "$GRC"; [ "$GRC" = 0 ] || show_g
exists "A 출력 파일" "$UPD"
isok "A bash -n" bash -n "$UPD"
nocr "A 생성물에 CR 없음" "$UPD"
specof "$UPD"
eq "A 변경 1건" 1 "$(grep -c '^@change ' "$T/spec")"
has "A insert_after" "$T/spec" '@change c1 insert_after'
has "A 대입문 앵커는 이름만" "$T/spec" '@anchor ^[[:space:]]*extra_opt[[:space:]]*='
has "A #candidates 병기" "$T/spec" '#candidates 1) insert_after 줄 10'
has "A 요약표 헤더" "$GOUT" 'OP'
has "A 요약표 c1" "$GOUT" 'insert_after'
has "A 자체 검증 통과" "$GOUT" '자체 검증 통과'
has "A sha256 출력" "$GOUT" 'sha256:'
mkfield <(fe "$OLDS")
fe "$NEWS" > "$T/a.expected"
run "$UPD" --dir "$P" --yes < /dev/null
eq "A 적용 rc" 0 "$RC"; [ "$RC" = 0 ] || show_o
same "A 현장 수정 보존 + 삽입 반영(바이트 동일)" "$T/a.expected" "$P/app.sh"
has "A 요약: 주석·코멘트 변경 0건" "$OUT" '주석·코멘트 문구 변경 0건'

# ================================================================ 2. 치환 (replace_line / replace_range)
NEWS=$T/f/b.new
sed -e 's|^    echo "done"$|    echo "done v1.1"|' \
    -e '/^    echo "check \$vendor"$/{N;s|.*|    echo "check start"\n    "$checker" -q -auto\n    echo ok|}' "$OLDS" > "$NEWS"
UPD=$T/u_b.sh
gen "$UPD"
eq "B 생성 rc" 0 "$GRC"; [ "$GRC" = 0 ] || show_g
specof "$UPD"
has "B replace_range" "$T/spec" 'replace_range'
has "B replace_line" "$T/spec" 'replace_line'
has "B anchor_end" "$T/spec" '@anchor_end '
mkfield <(fe "$OLDS")
fe "$NEWS" > "$T/b.expected"
run "$UPD" --dir "$P" --yes < /dev/null
eq "B 적용 rc" 0 "$RC"; [ "$RC" = 0 ] || show_o
same "B 치환 결과 + 현장 수정 보존" "$T/b.expected" "$P/app.sh"

# ================================================================ 3. 다중 hunk + 새 변수(manifest) + 현장 사본
NEWS=$T/f/c.new
cat > "$NEWS" <<'EOS'
#!/bin/bash
# app.sh - 점검 스크립트
# 담당자:
vendor=""
log_dir="/var/log/app"   # 로그 경로
checker=""   # 체크 스크립트 경로 (v1.1)
gossh_pw=''
# 타임아웃(초)
timeout=30
extra_opt=""
auto_done_host=""   # 완료기록 원격 호스트 (v1.1)

run_check() {
    echo "check $vendor"
    "$checker" -q -auto
}

main() {
    run_check
    echo "점검부탁드립니다"
    echo "done v1.1"
}
report() {
    echo "report $log_dir"
}

main "$@"
EOS
MAN=$T/f/m_c
cat > "$MAN" <<'EOS'
#@meta project=demo
#@meta version=1.1.0
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|none|1.0.0|-
gossh_pw|sh:app.sh|secret|common|-|n|gossh 비밀번호|-|none|1.0.0|-
auto_done_host|sh:app.sh|host|common|-|n|원격 호스트|os8mgmt|host|1.1.0|-
EOS
cat > "$T/f/m_c_old" <<'EOS'
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|none|1.0.0|-
gossh_pw|sh:app.sh|secret|common|-|n|gossh 비밀번호|-|none|1.0.0|-
legacy_var|sh:app.sh|text|common|-|n|없어진 변수|x|none|1.0.0|-
EOS
UPD=$T/u_c.sh
gen "$UPD" --old-manifest "$T/f/m_c_old"
eq "C 생성 rc" 0 "$GRC"; [ "$GRC" = 0 ] || show_g
has "C 새 변수 요약" "$GOUT" '새 변수(실행 시 질문): auto_done_host'
has "C 제거된 변수 요약" "$GOUT" 'manifest 에서 제거된 변수(현장 소스에는 유지·경고): legacy_var'
has "C 자체 검증 통과" "$GOUT" '자체 검증 통과'
specof "$UPD"
nc=$(grep -c '^@change ' "$T/spec")
if [ "$nc" -ge 4 ]; then pass; else fail "C 다중 hunk(>=4)" "변경 ${nc}건"; fi
engine_chk=$(sed -n '/^#__MANIFEST_BEGIN__$/,/^#__MANIFEST_END__$/p' "$UPD" | sed '1d;$d')
if [ "$engine_chk" = "$(cat "$MAN")" ]; then pass; else fail "C manifest 블록 = 원문"; fi
# 현장 사본(수정됨)에 적용
mkfield <(fe "$OLDS")
{ fe "$NEWS" | sed 's|^auto_done_host=""|auto_done_host="os8mgmt"|'; } > "$T/c.expected"
printf 'os8mgmt\n' > "$T/in"
run "$UPD" --dir "$P" --yes < "$T/in"
eq "C 적용 rc" 0 "$RC"; [ "$RC" = 0 ] || show_o
same "C 결과 = 현장 수정 보존 + 의도한 변경 + 새 변수 값" "$T/c.expected" "$P/app.sh"
has "C 새 변수만 질문" "$OUT" '[i] auto_done_host  ('
hasnt "C 기존 값 있는 변수 질문 안 함" "$OUT" '[i] checker  ('
hasnt "C secret 미노출" "$OUT" 'S3cr3t'
has "C 요약 0건" "$OUT" '주석·코멘트 문구 변경 0건'
# 멱등 재실행
cp "$P/app.sh" "$T/c.after"
nb=$(ls "$P"/app.sh.bak.* 2> /dev/null | wc -l)
run "$UPD" --dir "$P" --yes < /dev/null
eq "C 재실행 rc" 0 "$RC"
same "C 재실행: 파일 불변" "$T/c.after" "$P/app.sh"
has "C 재실행: 변경 없음" "$OUT" '변경 없음'
eq "C 재실행: 새 백업 없음" "$nb" "$(ls "$P"/app.sh.bak.* 2> /dev/null | wc -l)"
cp "$UPD" "$T/u_c.keep"
# --undo
run "$UPD" --dir "$P" --undo < /dev/null
eq "C undo rc" 0 "$RC"
same "C undo: 현장 원본 복원" <(fe "$OLDS") "$P/app.sh"

# ================================================================ 4. 잘림 감지 (붙여넣기)
UPD=$T/u_c.keep
lines=$(wc -l < "$UPD"); size=$(wc -c < "$UPD")
mkfield <(fe "$OLDS"); cp "$P/app.sh" "$T/trunc.field"
trunc_check() {   # trunc_check <이름> <잘린 파일>
    run "$2" --dir "$P" --yes < /dev/null
    eq "$1: rc 2" 2 "$RC"
    has "$1: 명확한 메시지" "$OUT" '잘렸거나 변형되었습니다'
    same "$1: 대상 파일 불변" "$T/trunc.field" "$P/app.sh"
    nobak "$1: 백업 없음" "$P/app.sh"
    if [ -e "$P/.update_journal" ]; then fail "$1: 저널 없음"; else pass; fi
}
head -c $((size / 2)) "$UPD" > "$T/t1.sh";                   trunc_check "절반(바이트)" "$T/t1.sh"
head -n $((lines / 2)) "$UPD" > "$T/t2.sh";                  trunc_check "절반(줄)" "$T/t2.sh"
head -n -1 "$UPD" > "$T/t3.sh";                              trunc_check "끝줄 마커만 없음" "$T/t3.sh"
head -n $((lines - 4)) "$UPD" > "$T/t4.sh";                  trunc_check "명세 블록 중간에서 잘림" "$T/t4.sh"
sed '300,310d' "$UPD" > "$T/t5.sh";                          trunc_check "중간 줄 누락" "$T/t5.sh"
{ cat "$UPD"; echo 'rm -rf /nonexistent_gus_test'; } > "$T/t6.sh"; trunc_check "끝에 줄이 덧붙음" "$T/t6.sh"
# 표준입력으로는 실행 불가 안내
run -s -- --dir "$P" --yes < "$UPD"
eq "stdin 실행 시도 rc 2" 2 "$RC"
has "stdin 실행 시도: 안내 메시지" "$OUT" "파일로 저장한 뒤"
same "stdin 실행 시도: 대상 파일 불변" "$T/trunc.field" "$P/app.sh"
# 온전한 파일은 통과
run "$UPD" --dir "$P" --dry < /dev/null
eq "온전한 파일 --dry rc" 0 "$RC"
same "--dry: 파일 불변" "$T/trunc.field" "$P/app.sh"

# ================================================================ 5. 생성물 형식 (끝 마커·줄 수·CR·sha)
eq "마지막 줄 = 끝 마커" '#__END_OF_UPDATE__' "$(tail -n 1 "$UPD")"
eq "끝 마커 줄은 1개" 1 "$(grep -cx '#__END_OF_UPDATE__' "$UPD")"
nocr "생성물에 CR 없음" "$UPD"
has "출력된 줄 수 = wc -l" "$GOUT" "${lines}줄"
if command -v sha256sum > /dev/null 2>&1; then
    sha=$(sha256sum "$UPD" | awk '{ print $1 }')
    has "출력된 sha256 = 실제" "$GOUT" "$sha"
fi
# lib_common.sh / update_engine.sh 가 그대로(verbatim) 들어 있는지
LIBF="$HERE/../templates/lib_common.sh"; ENGF="$HERE/../templates/update_engine.sh"
la=$(grep -n '^LIB_COMMON_VERSION=' "$UPD" | head -1 | cut -d: -f1)
if [ -n "$la" ]; then pass; else fail "lib 인라인 위치"; fi
n1=$(wc -l < "$LIBF")
la0=$(grep -n '^#!/bin/bash$' "$UPD" | sed -n 2p | cut -d: -f1)
sed -n "${la0},$((la0 + n1 - 1))p" "$UPD" > "$T/x.lib"
same "lib_common.sh 가 그대로 인라인" "$LIBF" "$T/x.lib"
ea=$((la0 + n1))
sed -n "${ea},$((ea + $(wc -l < "$ENGF") - 1))p" "$UPD" > "$T/x.eng"
same "update_engine.sh 가 그대로 인라인" "$ENGF" "$T/x.eng"
has "헤더에 프로젝트·버전" "$UPD" 'demo v1.1.0 업데이트 스크립트'

# ================================================================ 6. 비유일 앵커 처리
cat > "$T/f/d.old" <<'EOS'
#!/bin/bash
check() {
    if [ -f /a ]; then
        echo a
    fi
    if [ -f /b ]; then
        echo b
    fi
}
check
EOS
sed '8a\    echo "tail"' "$T/f/d.old" > "$T/f/d.new"
OLDS=$T/f/d.old; NEWS=$T/f/d.new; MAN=$T/f/m0
UPD=$T/u_d.sh
gen "$UPD"
eq "D 생성 rc" 0 "$GRC"; [ "$GRC" = 0 ] || show_g
specof "$UPD"
has "D 유일한 뒤 줄로 insert_before" "$T/spec" '@change c1 insert_before'
has "D 앵커는 }" "$T/spec" '@anchor ^[[:space:]]*\}'
has "D 비유일 후보 건너뜀 경고" "$GOUT" '앵커 부적합(비유일 등) 건너뜀'
has "D 자체 검증 통과" "$GOUT" '자체 검증 통과'
sed 's|^        echo a$|        echo a   # OO 수정|' "$OLDS" > "$T/f/d.field"
mkfield "$T/f/d.field"
sed 's|^        echo a$|        echo a   # OO 수정|' "$NEWS" > "$T/d.expected"
run "$UPD" --dir "$P" --yes < /dev/null
eq "D 적용 rc" 0 "$RC"; [ "$RC" = 0 ] || show_o
same "D 결과(현장 수정 보존)" "$T/d.expected" "$P/app.sh"

# 같은 줄 반복 + 인접 유일 줄 없음 → 종료코드 3, 파일 안 만듦
printf 'x\nx\n' > "$T/f/e.old"; printf 'x\nNEW\nx\n' > "$T/f/e.new"
OLDS=$T/f/e.old; NEWS=$T/f/e.new
UPD=$T/u_e.sh
gen "$UPD"
eq "E 앵커 없음 → rc 3" 3 "$GRC"
absent "E 스크립트를 만들지 않음" "$UPD"
has "E 안전하게 변환할 수 없음 설명" "$GOUT" '안전하게 변환할 수 없음'
has "E 해결 안내" "$GOUT" '해결:'

# 같은 줄 회전으로 유일한 앵커 찾기: 빈 줄 사이 삽입
cat > "$T/f/g.old" <<'EOS'
#!/bin/bash
foo() {
    echo 1
}

main() {
    foo
}
main
EOS
cat > "$T/f/g.new" <<'EOS'
#!/bin/bash
foo() {
    echo 1
}

bar() {
    echo 2
}

main() {
    foo
}
main
EOS
OLDS=$T/f/g.old; NEWS=$T/f/g.new
UPD=$T/u_g.sh
gen "$UPD"
eq "G 생성 rc" 0 "$GRC"; [ "$GRC" = 0 ] || show_g
specof "$UPD"
has "G main() 앞에 삽입" "$T/spec" '@change c1 insert_before'
has "G 자체 검증 통과" "$GOUT" '자체 검증 통과'

# ================================================================ 7. 마스킹 위반 경고 (주석·코멘트 변경)
OLDS=$T/f/base.sh; MAN=$T/f/m0
NEWS=$T/f/h1.new
sed 's|^# 타임아웃(초)$|# timeout (sec)|' "$OLDS" > "$NEWS"
UPD=$T/u_h1.sh
gen "$UPD"
eq "H1 주석 변경: 생성 rc(경고만)" 0 "$GRC"
has "H1 마스킹 위반 예정 경고" "$GOUT" '마스킹 위반 예정(주석 줄 또는 가드 키워드)'
has "H1 자체 검증 생략 안내" "$GOUT" '자체 검증(실제 적용 시뮬레이션)을 건너뜁니다'
mkfield <(fe "$OLDS")
cp "$P/app.sh" "$T/h1.field"
run "$UPD" --dir "$P" --yes < /dev/null
eq "H1 적용 시 엔진이 중단 rc 1" 1 "$RC"
has "H1 엔진 마스킹 위반 표시" "$OUT" '마스킹 위반'
same "H1 중단 시 현장 파일 불변" "$T/h1.field" "$P/app.sh"
nobak "H1 백업 없음" "$P/app.sh"

# 줄 끝 코멘트만 바뀌는 경우
NEWS=$T/f/h2.new
sed 's|^log_dir="/var/log/app"   # 로그 경로$|log_dir="/var/log/app"   # 로그 디렉터리|' "$OLDS" > "$NEWS"
gen "$T/u_h2.sh"
eq "H2 줄 끝 코멘트 변경: 생성 rc" 0 "$GRC"
has "H2 줄 끝 코멘트 경고" "$GOUT" '줄 끝 코멘트가 바뀜'

# --guard: 코드 줄에 가드 키워드
sed 's|^    echo "done"$|    echo "OOMARK done"|' "$T/f/base.sh" > "$T/f/h3.old"
sed 's|^    echo "OOMARK done"$|    echo "finish"|' "$T/f/h3.old" > "$T/f/h3.new"
OLDS=$T/f/h3.old; NEWS=$T/f/h3.new
gen "$T/u_h3.sh"
eq "H3 --guard 없이: 생성 rc" 0 "$GRC"
hasnt "H3 --guard 없이 경고 없음" "$GOUT" '마스킹 위반 예정'
has "H3 --guard 없이 자체 검증 통과" "$GOUT" '자체 검증 통과'
gen "$T/u_h3g.sh" --guard 'OOMARK'
eq "H3 --guard 있으면: 생성 rc" 0 "$GRC"
has "H3 --guard 경고" "$GOUT" '마스킹 위반 예정(주석 줄 또는 가드 키워드)'
has "H3 manifest 사본에 #@guard 덧붙임" "$T/u_h3g.sh" '#@guard OOMARK'
absent "H3 --guard 오류 정규식 → 생성 안 함" "$T/u_h3bad.sh"
gen "$T/u_h3bad.sh" --guard '(unclosed'
eq "H3 잘못된 --guard rc 2" 2 "$GRC"
absent "H3 잘못된 --guard: 파일 없음" "$T/u_h3bad.sh"

# ================================================================ 8. 앵커 안의 정규식 특수문자
cat > "$T/f/r.old" <<'EOS'
#!/bin/bash
x='a.b[c]*$|(d)\e/f&g?+{1}'
echo '[ ] * . $ ( ) | \ / & { } ? +' > /dev/null
x='zzz'
arr=(1 2)
echo "end"
EOS
cat > "$T/f/r.new" <<'EOS'
#!/bin/bash
x='a.b[c]*$|(d)\e/f&g?+{1}'
y1='after-x'
echo '[ ] * . $ ( ) | \ / & { } ? +' > /dev/null
y2='after-echo'
x='zzz'
arr=(1 2)
echo "end"
EOS
OLDS=$T/f/r.old; NEWS=$T/f/r.new; MAN=$T/f/m0
UPD=$T/u_r.sh
gen "$UPD"
eq "R 생성 rc" 0 "$GRC"; [ "$GRC" = 0 ] || show_g
specof "$UPD"
has "R 이스케이프된 앵커(대입문이 비유일 → 전체 줄)" "$T/spec" 'x='"'"'a\.b\[c\]\*\$\|\(d\)\\e/f&g\?\+\{1\}'"'"
has "R 특수문자 줄 앵커" "$T/spec" '\[[[:space:]]+\][[:space:]]+\*[[:space:]]+\.[[:space:]]+\$[[:space:]]+\('
has "R 자체 검증 통과" "$GOUT" '자체 검증 통과'
sed 's|^arr=(1 2)$|arr=(7 8 9)|' "$OLDS" > "$T/f/r.field"
sed 's|^arr=(1 2)$|arr=(7 8 9)|' "$NEWS" > "$T/r.expected"
mkfield "$T/f/r.field"
run "$UPD" --dir "$P" --yes < /dev/null
eq "R 적용 rc" 0 "$RC"; [ "$RC" = 0 ] || show_o
same "R 결과(특수문자 앵커로 정확한 위치에 삽입)" "$T/r.expected" "$P/app.sh"
# 앵커 줄의 값이 현장에서 달라져도 대입문 앵커는 이름만 보므로 적용된다
printf '#!/bin/bash\nname="a.b"\nz=1\n' > "$T/f/s.old"
printf '#!/bin/bash\nname="a.b"\nnew1=1\nz=1\n' > "$T/f/s.new"
OLDS=$T/f/s.old; NEWS=$T/f/s.new
gen "$T/u_s.sh"
eq "S 생성 rc" 0 "$GRC"
printf '#!/bin/bash\nname="현장 값 (OO) [x]"   # 현장 코멘트\nz=1\n' > "$T/f/s.field"
printf '#!/bin/bash\nname="현장 값 (OO) [x]"   # 현장 코멘트\nnew1=1\nz=1\n' > "$T/s.expected"
mkfield "$T/f/s.field"
run "$T/u_s.sh" --dir "$P" --yes < /dev/null
eq "S 적용 rc" 0 "$RC"; [ "$RC" = 0 ] || show_o
same "S 앵커 줄 값이 달라도 적용" "$T/s.expected" "$P/app.sh"

# ================================================================ 9. CRLF 이전/신규 소스
OLDS=$T/f/base.sh; MAN=$T/f/m_c; NEWS=$T/f/c.new
sed 's/$/\r/' "$OLDS" > "$T/f/crlf.old"
sed 's/$/\r/' "$NEWS" > "$T/f/crlf.new"
OLDS=$T/f/crlf.old; NEWS=$T/f/crlf.new
UPD=$T/u_crlf.sh
gen "$UPD"
eq "CRLF 생성 rc" 0 "$GRC"; [ "$GRC" = 0 ] || show_g
nocr "CRLF 원본에서 생성해도 생성물에 CR 없음" "$UPD"
has "CRLF 정규화 안내" "$GOUT" 'LF 로 정규화'
has "CRLF 자체 검증 통과" "$GOUT" '자체 검증 통과'
fe "$T/f/base.sh" | sed 's/$/\r/' > "$T/f/crlf.field"
mkfield "$T/f/crlf.field"
run "$UPD" --dir "$P" --yes < "$T/in"
eq "CRLF 적용 rc" 0 "$RC"; [ "$RC" = 0 ] || show_o
same "CRLF 현장 사본 → 결과는 LF, 내용은 기대와 동일" "$T/c.expected" "$P/app.sh"
nocr "CRLF 결과에 CR 없음" "$P/app.sh"
bk=$(ls "$P"/app.sh.bak.* 2> /dev/null | head -1)
if [ -n "$bk" ]; then same "CRLF 백업은 원본 바이트 그대로" "$T/f/crlf.field" "$bk"; else fail "CRLF 백업 존재"; fi

# ================================================================ 9b. 줄 삭제 / 파일 끝 추가 / 이름 바뀐 변수 요약 / 기본 --file
OLDS=$T/f/base.sh; MAN=$T/f/m0
NEWS=$T/f/j.new
sed '/^extra_opt=""$/d' "$OLDS" > "$NEWS"
UPD=$T/u_j.sh
gen "$UPD"
eq "J 줄 삭제 생성 rc" 0 "$GRC"; [ "$GRC" = 0 ] || show_g
has "J 자체 검증 통과" "$GOUT" '자체 검증 통과'
mkfield <(fe "$OLDS")
fe "$NEWS" > "$T/j.expected"
run "$UPD" --dir "$P" --yes < /dev/null
eq "J 삭제 적용 rc" 0 "$RC"; [ "$RC" = 0 ] || show_o
same "J 줄 삭제 결과 + 현장 수정 보존" "$T/j.expected" "$P/app.sh"

NEWS=$T/f/k.new
{ cat "$OLDS"; echo 'echo "appended at EOF"'; } > "$NEWS"
UPD=$T/u_k.sh
gen "$UPD"
eq "K 파일 끝 추가 생성 rc" 0 "$GRC"; [ "$GRC" = 0 ] || show_g
specof "$UPD"
has "K insert_after 마지막 줄" "$T/spec" '@change c1 insert_after'
mkfield <(fe "$OLDS")
fe "$NEWS" > "$T/k.expected"
run "$UPD" --dir "$P" --yes < /dev/null
eq "K 적용 rc" 0 "$RC"
same "K 파일 끝 추가 결과" "$T/k.expected" "$P/app.sh"

# 이름 바뀐 변수 요약 (--old-manifest)
cat > "$T/f/m_ren" <<'EOS'
#@meta project=demo
#@meta version=1.1.0
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|none|1.0.0|-
gossh_pass|sh:app.sh|secret|common|-|n|gossh 비밀번호|-|none|1.1.0|gossh_pw
EOS
NEWS=$T/f/a.new
bash -u "$MK" --old "$OLDS" --new "$NEWS" --manifest "$T/f/m_ren" --old-manifest "$T/f/m_c_old" --version 1.1.0 --file app.sh --out "$T/u_ren.sh" --yes > "$GOUT" 2>&1
eq "REN 생성 rc" 0 $?
has "REN 이름 바뀐 변수 요약" "$GOUT" '이름 바뀐 변수(값 자동 이전): gossh_pw->gossh_pass'
has "REN 소스에 선언 없음 경고" "$GOUT" '변수 gossh_pass: 새 소스에 선언 줄이 없음'
has "REN 제거된 변수는 legacy_var 만" "$GOUT" '제거된 변수(현장 소스에는 유지·경고): legacy_var'
hasnt "REN 이름 바뀐 gossh_pw 는 제거 취급 안 함" "$GOUT" '제거된 변수(현장 소스에는 유지·경고): legacy_var gossh_pw'

# --file 기본값 = --new 의 basename
mkdir -p "$T/f/nd"; cp "$T/f/a.new" "$T/f/nd/app.sh"
bash -u "$MK" --old "$OLDS" --new "$T/f/nd/app.sh" --manifest "$MAN" --version 1.1.0 --out "$T/u_nd.sh" --yes > "$GOUT" 2>&1
eq "기본 --file 생성 rc" 0 $?
specof "$T/u_nd.sh"
has "기본 --file = basename" "$T/spec" '@file app.sh'

# ================================================================ 10. 인자·종료코드
OLDS=$T/f/base.sh; MAN=$T/f/m0
bash "$MK" --old "$OLDS" --new "$OLDS" --manifest "$MAN" --version 1.1.0 --out "$T/u_same.sh" --yes > "$GOUT" 2>&1
eq "변경 없음 rc 2" 2 $?
has "변경 없음 메시지" "$GOUT" '변경 사항이 없습니다'
absent "변경 없음: 파일 없음" "$T/u_same.sh"
bash "$MK" --old "$OLDS" > "$GOUT" 2>&1
eq "필수 인자 누락 rc 2" 2 $?
NEWS=$T/f/a.new
bash "$MK" --old "$OLDS" --new "$NEWS" --manifest "$MAN" --version 1.1.0 --file /abs/app.sh --out "$T/u_abs.sh" --yes > "$GOUT" 2>&1
eq "--file 절대경로 rc 2" 2 $?
bash "$MK" --old "$OLDS" --new "$NEWS" --manifest "$T/nonexist" --version 1.1.0 --out "$T/u_nm.sh" --yes > "$GOUT" 2>&1
eq "manifest 없음 rc 2" 2 $?
bash "$MK" --old "$OLDS" --new "$NEWS" --manifest "$MAN" --version 'a b' --out "$T/u_v.sh" --yes > "$GOUT" 2>&1
eq "버전 형식 오류 rc 2" 2 $?
# 확인 질문에 n → 만들지 않음 (rc 1)
bash "$MK" --old "$OLDS" --new "$NEWS" --manifest "$MAN" --version 1.1.0 --file app.sh --out "$T/u_n.sh" < <(printf 'n\n') > "$GOUT" 2>&1
eq "확인 n rc 1" 1 $?
absent "확인 n: 파일 없음" "$T/u_n.sh"
bash "$MK" --old "$OLDS" --new "$NEWS" --manifest "$MAN" --version 1.1.0 --file app.sh --out "$T/u_y.sh" < <(printf 'y\n') > "$GOUT" 2>&1
eq "확인 y rc 0" 0 $?
exists "확인 y: 파일 생성" "$T/u_y.sh"

# ================================================================ 11. set -u
eq "set -u: unbound variable 없음" 0 "$UNBOUND"

echo "PASS=$PASS FAIL=$FAIL"
