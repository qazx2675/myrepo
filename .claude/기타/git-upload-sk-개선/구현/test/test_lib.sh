#!/bin/bash
# test_lib.sh - templates/lib_common.sh 단위·시나리오 테스트
# 사용: bash test_lib.sh      (마지막 줄: PASS=n FAIL=m)
# 픽스처는 전부 mktemp 로 만든다. 입력은 stdin 파이프(비 tty).

HERE=$(cd "$(dirname "$0")" && pwd)
LIB="$HERE/../templates/lib_common.sh"

T=$(mktemp -d "${TMPDIR:-/tmp}/gus_test.XXXXXX") || exit 2
trap 'rm -rf "$T"' EXIT

PASS=0; FAIL=0
pass() { PASS=$((PASS + 1)); }
fail() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$1"; [ -n "${2-}" ] && printf '      %s\n' "$2"; return 0; }
eq()    { if [ "$2" = "$3" ]; then pass; else fail "$1" "기대=[$2] 실제=[$3]"; fi; }
isok()  { local n=$1; shift; if "$@" > /dev/null 2>&1; then pass; else fail "$n" "명령 실패: $*"; fi; }
isno()  { local n=$1; shift; if "$@" > /dev/null 2>&1; then fail "$n" "성공하면 안 됨: $*"; else pass; fi; }
has()   { if grep -qF -- "$3" "$2"; then pass; else fail "$1" "'$3' 가 $2 에 없음"; fi; }
hasnt() { if grep -qF -- "$3" "$2"; then fail "$1" "'$3' 가 $2 에 있으면 안 됨"; else pass; fi; }
rc_is() { eq "$1" "$2" "$3"; }
nocr()  { if grep -q $'\r' "$2" 2> /dev/null; then fail "$1" "CR 이 남아 있음: $2"; else pass; fi; }

# shellcheck source=/dev/null
. "$LIB"

# ---------------------------------------------------------------- 픽스처
mkfix() {
    rm -rf "$T/proj"
    mkdir -p "$T/proj/conf"
    PROJECT_DIR=$T/proj
    cat > "$T/proj/app.sh" <<'EOS'
#!/bin/bash
# 담당자: 홍길동 (마스킹 보존)
auto_done_dir=""   # 로컬 done 경로
auto_done_host=""
checker=""
export gossh_pw=''
echo "$auto_done_dir"
# 감사합니다
EOS
    cat > "$T/proj/conf/app.conf" <<'EOS'
# conf
extra=
other=1
EOS
    cat > "$T/proj/vars.manifest" <<'EOS'
# 테스트 manifest
#@meta project=demo
#@meta version=1.0.0
#@meta setup=setup.sh
#@guard 점검필수
#@xor done|이 스크립트는 어느 서버에서 실행?|auto_done_dir:os8_mgmt 에서 실행(로컬 기록)|auto_done_host:os6_mgmt 에서 실행(원격 기록)
auto_done_dir|sh:app.sh|path|os8|xor:done|n|로컬 done 디렉터리\n두번째 줄|/tmp/auto_setup/done|dir|1.0.0|-
auto_done_host|sh:app.sh|host|os6,os8|xor:done|n|원격 호스트|os8mgmt|probe|1.0.0|-
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|file|1.0.0|-
gossh_pw|sh:app.sh|secret|common|dep:auto_done_host|n|gossh 비밀번호|-|none|1.0.0|-
extra|conf:conf/app.conf|text|common|-|n|추가 값|x|none|1.0.0|old_extra
EOS
    cp "$T/proj/app.sh" "$T/app.sh.orig"
    cp "$T/proj/conf/app.conf" "$T/app.conf.orig"
    : > "$T/real.sh"
    mkdir -p "$T/realdir"
}
idx() { _idx_of "$1" && echo "$_IDX"; }

# ================================================================ 0. 정적 검사
isok "bash -n lib" bash -n "$LIB"
nocr "lib 에 CR 없음" "$LIB"
n=$(grep -v '^[[:space:]]*#' "$LIB" | grep -cE '\b(python[0-9]*|perl)\b|local -n|declare -g|\$\{[A-Za-z_]+\^\^|\[\[ -v |mapfile -d|read -N|;;&|\|&')
eq "금지 기능(python/perl/4.2+) 없음" 0 "$n"
nocr "test 에 CR 없음" "$HERE/test_lib.sh"

# ================================================================ 1. ui_init / 색
ui_init
eq "비 tty: 색 끔" "" "$C_OK$C_ERR$C_RST"
out=$(ok "정상"; warn "경고"; err "오류"; info "안내")
eq "접두 표시" $'[O] 정상\n[!] 경고\n[X] 오류\n[i] 안내' "$out"
if command -v script > /dev/null 2>&1; then
    script -qec "bash -c '. \"$LIB\"; ok hi'" /dev/null > "$T/col.out" 2> /dev/null
    if grep -q $'\033\[32m' "$T/col.out"; then pass; else fail "tty 에서 초록 색 사용" "$(od -c "$T/col.out" | head -3)"; fi
    NO_COLOR=1 script -qec "bash -c '. \"$LIB\"; ok hi'" /dev/null > "$T/col2.out" 2> /dev/null
    if grep -q $'\033' "$T/col2.out"; then fail "NO_COLOR 면 색 끔"; else pass; fi
else
    echo "SKIP: script 명령 없음 (tty 색 테스트)"
fi

# ================================================================ 2. lf_normalize / backup / restore
printf 'a\r\nb\r\n' > "$T/crlf.txt"
lf_normalize "$T/crlf.txt" > "$T/o" 2>&1
nocr "lf_normalize CR 제거" "$T/crlf.txt"
eq "lf_normalize 내용" $'a\nb' "$(cat "$T/crlf.txt")"
has "lf_normalize info 1줄" "$T/o" "CR 제거"
lf_normalize "$T/crlf.txt" > "$T/o" 2>&1
eq "lf_normalize 두 번째는 조용" "0" "$(wc -c < "$T/o")"
isno "lf_normalize 없는 파일" lf_normalize "$T/nonexist"

printf 'v1\n' > "$T/b.txt"; chmod 640 "$T/b.txt"
b1=$(backup_file "$T/b.txt")
printf 'v2\n' > "$T/b.txt"
b2=$(backup_file "$T/b.txt")
printf 'v3\n' > "$T/b.txt"
isok "backup 파일 생성" test -f "$b1"
if [ "$b1" != "$b2" ]; then pass; else fail "같은 초 백업 경로가 달라야 함" "$b1"; fi
eq "backup 경로 패턴" "1" "$(printf '%s' "$b1" | grep -cE '\.bak\.[0-9]{14}')"
eq "backup 권한 보존" "640" "$(stat -c %a "$b1")"
restore_latest "$T/b.txt" > /dev/null
eq "restore_latest 는 가장 최근 백업" "v2" "$(cat "$T/b.txt")"
isno "백업 없는 파일 restore" restore_latest "$T/none.txt"
isno "없는 파일 backup" backup_file "$T/none.txt"

# ================================================================ 3. manifest_load
mkfix
manifest_load "$T/proj/vars.manifest"; rc=$?
rc_is "manifest_load rc" 0 $rc
eq "변수 5개" 5 "$M_COUNT"
eq "M_NAME[2]" checker "${M_NAME[2]}"
eq "M_TARGET[4]" "conf:conf/app.conf" "${M_TARGET[4]}"
eq "M_GROUP[0]" "xor:done" "${M_GROUP[0]}"
eq "M_RENAMED[4]" old_extra "${M_RENAMED[4]}"
eq "M_ROLE[1] 공백 제거" "os6,os8" "${M_ROLE[1]}"
eq "M_INDEX" 3 "${M_INDEX[gossh_pw]}"
eq "M_DESC 는 \\n 원문 유지" '로컬 done 디렉터리\n두번째 줄' "${M_DESC[0]}"
eq "META setup" setup.sh "${META[setup]}"
eq "META project" demo "${META[project]}"
eq "META version" 1.0.0 "${META[version]}"
eq "GUARDS" 1 "${#GUARDS[@]}"
eq "GUARDS 값" 점검필수 "${GUARDS[0]}"
eq "XOR_Q" 1 "${#XOR_Q[@]}"
case "${XOR_Q[0]}" in done\|이\ 스크립트는*) pass ;; *) fail "XOR_Q 원문" "${XOR_Q[0]}" ;; esac
manifest_load - < "$T/proj/vars.manifest"; eq "stdin 블록 로드" 5 "$M_COUNT"
sed 's/$/\r/' "$T/proj/vars.manifest" > "$T/crlf.manifest"
manifest_load "$T/crlf.manifest"; eq "CRLF manifest 도 처리" 5 "$M_COUNT"
eq "CRLF manifest 마지막 필드" old_extra "${M_RENAMED[4]}"
manifest_load "$T/none.manifest" > /dev/null 2>&1; rc_is "manifest 파일 없음 rc" 2 $?
printf 'a|sh:x|path|common|-|y|d|e|none|1\n' > "$T/bad1.m"
manifest_load "$T/bad1.m" > "$T/o" 2>&1; rc_is "필드 수 오류 rc" 1 $?
has "필드 수 오류 메시지" "$T/o" "필드 수 오류"
printf '1a|sh:x|path|common|-|y|d|e|none|1|-\n' > "$T/bad2.m"
manifest_load "$T/bad2.m" > "$T/o" 2>&1; rc_is "변수명 오류 rc" 1 $?
printf 'a|sh:x|path|common|-|y|d|e|none|1|-\na|sh:x|path|common|-|y|d|e|none|1|-\n' > "$T/bad3.m"
manifest_load "$T/bad3.m" > "$T/o" 2>&1; rc_is "중복 변수명 rc" 1 $?
has "중복 메시지" "$T/o" "중복"
printf 'a|sh:x|zzz|common|-|y|d|e|none|1|-\n' > "$T/bad4.m"
manifest_load "$T/bad4.m" > "$T/o" 2>&1; rc_is "kind 오류 rc" 1 $?
printf 'a|x|path|common|-|y|d|e|none|1|-\n' > "$T/bad5.m"
manifest_load "$T/bad5.m" > "$T/o" 2>&1; rc_is "target 오류 rc" 1 $?
printf '#@guard (\n' > "$T/bad6.m"
manifest_load "$T/bad6.m" > "$T/o" 2>&1; rc_is "guard 정규식 오류 rc" 1 $?
printf '#@xor bad id\n' > "$T/bad7.m"
manifest_load "$T/bad7.m" > "$T/o" 2>&1; rc_is "xor 형식 오류 rc" 1 $?
manifest_load "$T/proj/vars.manifest"

# ---- guard_hit
isok "guard: 주석 줄" guard_hit '   # anything'
isok "guard: 담당자" guard_hit 'x=1 # 담당자 홍길동'
isok "guard: 감사합니다" guard_hit 'echo 감사합니다'
isok "guard: 점검부탁" guard_hit 'echo 점검부탁'
isok "guard: manifest 추가 가드" guard_hit 'echo 점검필수 항목'
isno "guard: 일반 코드" guard_hit 'x=1'

# ================================================================ 4. target_get / target_set (sh)
mkfix
manifest_load "$T/proj/vars.manifest"
eq "get: 빈 값" "" "$(target_get auto_done_dir)"
eq "get: 단일따옴표(빈)" "" "$(target_get gossh_pw)"
isno "get: 없는 변수" target_get nope
rc_is "get: 대상 파일 없음 rc" 2 "$(PROJECT_DIR=$T/zz target_get checker > /dev/null 2>&1; echo $?)"

# 특수문자 값 왕복 (sh: 실제 source 해서 값이 같은지) + conf 왕복
cat > "$T/m2.manifest" <<'EOS'
tv|sh:v.sh|text|common|-|n|d|e|none|1|-
cv|conf:v.conf|text|common|-|n|d|e|none|1|-
EOS
manifest_load "$T/m2.manifest"
vals=('a&b/c\d"e$f g' '$(echo PWNED)' '`id`' '%s %d \n lit' '한글 값 & 공백' "it's" '#notcomment' 'x;y' '\\' '"' '&' '\&\\&' '${HOME}' '!hist' 'a|b' 'tab	in')
i=0
for v in "${vals[@]}"; do
    i=$((i + 1))
    printf '#!/bin/bash\n# 주석 보존 담당자\ntv="old" # 꼬리 주석 유지\nafter=1\n' > "$T/proj/v.sh"
    printf '# c\ncv=old\nafter=1\n' > "$T/proj/v.conf"
    target_set tv "$v" > "$T/o" 2>&1; rc=$?
    rc_is "sh set rc #$i" 0 $rc
    got=$(bash -c '. "$1" > /dev/null; printf "%s" "$tv"' _ "$T/proj/v.sh")
    eq "sh 실제 source 값 #$i" "$v" "$got"
    eq "sh target_get 왕복 #$i" "$v" "$(target_get tv)"
    eq "sh 꼬리 주석 보존 #$i" "1" "$(grep -c '^tv=.* # 꼬리 주석 유지$' "$T/proj/v.sh")"
    eq "sh 다른 줄 불변 #$i" "$(printf '#!/bin/bash\n# 주석 보존 담당자\nafter=1')" "$(grep -v '^tv=' "$T/proj/v.sh")"
    target_set cv "$v" > "$T/o" 2>&1; rc=$?
    rc_is "conf set rc #$i" 0 $rc
    eq "conf target_get 왕복 #$i" "$v" "$(target_get cv)"
    eq "conf 줄 형식 #$i" "cv=$v" "$(grep '^cv=' "$T/proj/v.conf")"
    eq "conf 다른 줄 불변 #$i" "$(printf '# c\nafter=1')" "$(grep -v '^cv=' "$T/proj/v.conf")"
done
nocr "sh 결과 LF" "$T/proj/v.sh"
nocr "conf 결과 LF" "$T/proj/v.conf"
printf '#!/bin/bash\ntv="old"\n' > "$T/proj/v.sh"
target_set tv "" > /dev/null; eq "빈 값 sh" 'tv=""' "$(grep '^tv=' "$T/proj/v.sh")"
printf '#!/bin/bash\ntv=old   # c\n' > "$T/proj/v.sh"
target_set tv "new val" > /dev/null; eq "비인용 선언 -> 인용 + 꼬리 유지" 'tv="new val"   # c' "$(grep '^tv=' "$T/proj/v.sh")"
printf "#!/bin/bash\ntv='sq val' # c\n" > "$T/proj/v.sh"
eq "단일따옴표 읽기" "sq val" "$(target_get tv)"
printf '#!/bin/bash\nexport tv="x"\n' > "$T/proj/v.sh"
target_set tv "y" > /dev/null; eq "export 접두 유지" 'export tv="y"' "$(grep 'tv=' "$T/proj/v.sh")"
printf '#!/bin/bash\n  tv="x"\ntv="second"\n' > "$T/proj/v.sh"
target_set tv "y" > /dev/null; eq "첫 선언만 치환(들여쓰기 유지)" $'  tv="y"\ntv="second"' "$(grep 'tv=' "$T/proj/v.sh")"
# 선언 없음 / 주석 처리된 선언 → rc 2, 파일 불변
printf '#!/bin/bash\n# tv="x"\nother=1\n' > "$T/proj/v.sh"; cp "$T/proj/v.sh" "$T/v.cmp"
target_set tv "y" > /dev/null 2>&1; rc_is "선언 없음 rc 2" 2 $?
isok "선언 없음 시 파일 불변" cmp "$T/proj/v.sh" "$T/v.cmp"
printf '# cv=1\n' > "$T/proj/v.conf"; cp "$T/proj/v.conf" "$T/v.cmp"
target_set cv "y" > /dev/null 2>&1; rc_is "conf 주석 선언은 선언 아님 rc 2" 2 $?
isok "conf 불변" cmp "$T/proj/v.conf" "$T/v.cmp"
target_set cv $'a\nb' > /dev/null 2>&1; rc_is "줄바꿈 값 rc 3" 3 $?
target_set nope x > /dev/null 2>&1; rc_is "미등록 변수 rc 1" 1 $?
PROJECT_DIR=$T/zz target_set tv x > /dev/null 2>&1; rc_is "대상 파일 없음 rc 2" 2 $?
# CRLF 대상 → LF 출력, 권한/inode 유지
printf '#!/bin/bash\r\ntv="old"\r\nafter=1\r\n' > "$T/proj/v.sh"; chmod 750 "$T/proj/v.sh"
ino1=$(stat -c %i "$T/proj/v.sh")
eq "CRLF 선언 읽기" "old" "$(target_get tv)"
target_set tv "new" > /dev/null
nocr "CRLF 대상 쓰기 후 LF" "$T/proj/v.sh"
eq "권한 유지" 750 "$(stat -c %a "$T/proj/v.sh")"
eq "inode 유지" "$ino1" "$(stat -c %i "$T/proj/v.sh")"
# conf 앞공백 값은 읽기-검증에서 거부, 원본 불변
printf 'cv=old\n' > "$T/proj/v.conf"
target_set cv " lead" > /dev/null 2>&1; rc_is "conf 앞공백 값 rc 5" 5 $?
eq "conf 불변(rc5)" "cv=old" "$(cat "$T/proj/v.conf")"
# conf 선언 앞뒤 공백 형식 유지
printf '  cv = old  \n' > "$T/proj/v.conf"
eq "conf 공백 선언 읽기(끝 공백 제거)" old "$(target_get cv)"
target_set cv new > /dev/null; eq "conf 접두 형식 유지" '  cv = new' "$(cat "$T/proj/v.conf")"
# secret: chmod 600 + 경고 1회
mkfix; manifest_load "$T/proj/vars.manifest"
chmod 644 "$T/proj/app.sh"
_SECRET_WARNED=0
target_set gossh_pw 'S3cr3t&"x' > "$T/o" 2>&1
eq "secret 대상 chmod 600" 600 "$(stat -c %a "$T/proj/app.sh")"
has "secret 커밋 금지 경고" "$T/o" "커밋하지 마십시오"
hasnt "secret 값 비출력" "$T/o" 'S3cr3t'
target_set gossh_pw 'Other2' > "$T/o" 2>&1
eq "경고는 1회" 0 "$(grep -c '커밋' "$T/o")"

# ================================================================ 5. check_value
mkfix; manifest_load "$T/proj/vars.manifest"
iD=$(idx auto_done_dir); iH=$(idx auto_done_host); iC=$(idx checker); iP=$(idx gossh_pw); iX=$(idx extra)
eq "file 존재" OK "$(check_value $iC "$T/real.sh")"
case "$(check_value $iC "$T/nofile")" in "파일 없음: "*) pass ;; *) fail "file 없음 이유" ;; esac
isno "file 없음 rc" check_value $iC "$T/nofile"
case "$(check_value $iC "$T/realdir")" in "파일 없음: "*) pass ;; *) fail "디렉터리는 file 아님" ;; esac
eq "dir 존재" OK "$(check_value $iD "$T/realdir")"
case "$(check_value $iD "$T/real.sh")" in "디렉터리 없음: "*) pass ;; *) fail "dir 이유" ;; esac
eq "필수 빈 값" "필수 값이 비어 있음" "$(check_value $iC "")"
eq "선택 빈 값 OK" OK "$(check_value $iD "")"
eq "줄바꿈 값" "값에 줄바꿈이 들어 있음" "$(check_value $iX $'a\nb')"
for h in host1 10.0.0.1 root@10.0.0.1:22 my-host.example.com a_b.c 255.255.255.255 h:65535; do
    eq "host 정상 $h" OK "$(check_value $iH "$h")"
done
for h in 10.0.0.256 'host name' 1.2.3 h:99999 h:0 -bad 'a@' '@h' 'h:' 123 'h..x' '1.2.3.4.5'; do
    r=$(check_value $iH "$h") && fail "host 이상이어야 함: $h" || pass
    case "$r" in OK) fail "host 이상 이유: $h" ;; esac
done
probe_hook() { [ "$2" = "dead" ] && { echo "응답 없음"; return 1; }; return 0; }
eq "probe 훅 정상" OK "$(check_value $iH alive)"
eq "probe 훅 실패" "연결 점검 실패: 응답 없음" "$(check_value $iH dead)"
eq "PROBE=0 이면 훅 생략" OK "$(PROBE=0 check_value $iH dead)"
unset -f probe_hook
eq "훅 없으면 형식만" OK "$(check_value $iH dead)"
eq "secret 은 형식검사 안 함, 이유에 값 없음" OK "$(check_value $iP 'S3cr3t')"
iPc=$iP
eq "check=none" OK "$(check_value $iX 'anything')"

# ================================================================ 6. ask_var
mkfix; manifest_load "$T/proj/vars.manifest"; guide_reset
printf '%s\n' "$T/real.sh" > "$T/in"
ask_var $iC < "$T/in" > "$T/o" 2>&1; rc_is "ask_var 새 값 rc" 0 $?
eq "ask_var NEWVAL" "$T/real.sh" "${NEWVAL[$iC]}"
has "ask_var 설명" "$T/o" "체크 스크립트 경로"
has "ask_var 예시" "$T/o" "예: /path/config_check.sh"
has "ask_var 현재값 표시" "$T/o" "현재값: (비어 있음)"
guide_reset
ask_var $iD < /dev/null > "$T/o" 2>&1
has "ask_var 설명의 \\n 은 줄바꿈" "$T/o" "    두번째 줄"
hasnt "ask_var 설명에 \\n 원문 안 남음" "$T/o" '\n'
guide_reset
has "ask_var 프롬프트는 stderr 로 출력" "$T/o" "Enter=현재값 유지"
guide_reset
printf '/nonexistent\n%s\n' "$T/real.sh" > "$T/in"
ask_var $iC < "$T/in" > "$T/o" 2>&1; rc_is "ask_var 재입력 rc" 0 $?
has "ask_var 이상 이유" "$T/o" "파일 없음: /nonexistent"
eq "ask_var 재입력 후 값" "$T/real.sh" "${NEWVAL[$iC]}"
guide_reset
printf '/nonexistent\ns\n' > "$T/in"
ask_var $iC < "$T/in" > "$T/o" 2>&1; rc_is "ask_var 건너뛰기 rc" 3 $?
eq "ask_var SKIPPED" 1 "${SKIPPED[$iC]}"
eq "ask_var 건너뛰면 NEWVAL 없음" "" "${NEWVAL[$iC]+x}"
guide_reset
printf '/nonexistent\n\n' > "$T/in"
ask_var $iC < "$T/in" > "$T/o" 2>&1; rc_is "ask_var 재입력에서 Enter=건너뛰기" 3 $?
guide_reset
ask_var $iC < /dev/null > "$T/o" 2>&1; rc_is "ask_var EOF rc 1" 1 $?
# Enter 유지
target_set checker "$T/real.sh" > /dev/null
guide_reset
printf '\n' > "$T/in"
ask_var $iC < "$T/in" > "$T/o" 2>&1; rc_is "ask_var Enter 유지 rc" 0 $?
eq "ask_var Enter 유지 시 NEWVAL 없음" "" "${NEWVAL[$iC]+x}"
has "ask_var 현재값 유지 안내" "$T/o" "현재값 유지"
# 공백 trim, CR 제거
guide_reset
printf '  %s  \r\n' "$T/real.sh" > "$T/in"
printf '%s\n' "$T/real.sh" > /dev/null
target_set checker "" > /dev/null
ask_var $iC < "$T/in" > "$T/o" 2>&1
eq "ask_var 입력 trim/CR 제거" "$T/real.sh" "${NEWVAL[$iC]}"
# secret
guide_reset
printf 'S3cr3t!x\n' > "$T/in"
ask_var $iP < "$T/in" > "$T/o" 2>&1; rc_is "ask_var secret rc" 0 $?
eq "ask_var secret 값 저장(메모리)" 'S3cr3t!x' "${NEWVAL[$iP]}"
hasnt "ask_var secret 값 비출력" "$T/o" 'S3cr3t'
has "ask_var secret 마스킹" "$T/o" '****'
# 파일에서 가져온 현재 secret 도 마스킹
target_set gossh_pw 'OldPw99' > /dev/null 2>&1
guide_reset
printf '\n' > "$T/in"
ask_var $iP < "$T/in" > "$T/o" 2>&1
hasnt "현재 secret 비출력" "$T/o" 'OldPw99'
has "현재 secret 마스킹" "$T/o" '현재값: ****'
nocr "ask_var 출력 LF" "$T/o"

# ================================================================ 7. confirm
printf 'y\n' | confirm "진행?" > "$T/o" 2>&1; rc_is "confirm y" 0 $?
printf 'N\n' | confirm "진행?" > "$T/o" 2>&1; rc_is "confirm n" 1 $?
printf 'x\n\nyes\n' | confirm "진행?" > "$T/o" 2>&1; rc_is "confirm 잘못된 입력 후 y" 0 $?
has "confirm 재질문 경고" "$T/o" "y 또는 n"
printf 'y\r\n' | confirm "진행?" > "$T/o" 2>&1; rc_is "confirm CR 입력" 0 $?
confirm "진행?" < /dev/null > "$T/o" 2>&1; rc_is "confirm EOF 는 n" 1 $?
has "confirm (y/n) 자동 부착" "$T/o" "진행? (y/n)"
printf 'y\n' | confirm "가이드를 실행하시겠습니까? (y/n)" > "$T/o" 2>&1
eq "confirm (y/n) 중복 없음" 0 "$(grep -c '(y/n) (y/n)' "$T/o")"
ASSUME_YES=1 confirm "진행?" < /dev/null > "$T/o" 2>&1; rc_is "ASSUME_YES" 0 $?
unset ASSUME_YES

# ================================================================ 8. summary_table
mkfix; manifest_load "$T/proj/vars.manifest"; guide_reset
NEWVAL[$iP]='S3cr3t!x'
NEWVAL[$iC]="$T/nofile"
summary_table > "$T/o" 2>&1; rc_is "summary_table 이상 있으면 rc 1" 1 $?
has "summary 이상 표시" "$T/o" "이상(파일 없음: $T/nofile)"
has "summary 정상 표시" "$T/o" "정상"
has "summary secret 마스킹" "$T/o" "****"
hasnt "summary secret 비출력" "$T/o" 'S3cr3t'
eq "SUMMARY_BAD" "$iC" "${SUMMARY_BAD[*]}"
SKIPPED[$iC]=1
summary_table > "$T/o" 2>&1; rc_is "건너뛴 변수는 bad 아님 rc 0" 0 $?
has "summary 건너뜀 표시" "$T/o" "건너뜀(이상:"
nocr "summary LF" "$T/o"
guide_reset

# ================================================================ 9. run_guide 시나리오
# A: 전체 흐름 (os8, 택일=2번(host), 의존 질문, 변경 요약, 적용)
mkfix; manifest_load "$T/proj/vars.manifest"
printf '%s\n' 2 os8mgmt "$T/real.sh" "hello & world" 'S3cr3t!x' y > "$T/in"
run_guide os8 < "$T/in" > "$T/o" 2>&1; rc_is "A rc" 0 $?
has "A 택일 질문" "$T/o" "이 스크립트는 어느 서버에서 실행?"
has "A 택일 선택지 설명" "$T/o" "os6_mgmt 에서 실행(원격 기록)"
hasnt "A 택일 제외 변수 질문 안 함" "$T/o" "[i] auto_done_dir"
has "A 점검 표" "$T/o" "== 점검 결과 =="
has "A 변경 요약" "$T/o" "== 변경 요약 =="
hasnt "A secret 비출력" "$T/o" 'S3cr3t'
a_vals=$(bash -c '. "$1" > /dev/null; printf "%s|%s|%s|%s" "$auto_done_dir" "$auto_done_host" "$checker" "$gossh_pw"' _ "$T/proj/app.sh")
eq "A app.sh 값" "|os8mgmt|$T/real.sh|S3cr3t!x" "$a_vals"
eq "A conf 값" 'extra=hello & world' "$(grep '^extra=' "$T/proj/conf/app.conf")"
eq "A conf 다른 줄 불변" 'other=1' "$(grep '^other=' "$T/proj/conf/app.conf")"
eq "A app.sh 변경 줄 수 3" 3 "$(diff "$T/app.sh.orig" "$T/proj/app.sh" | grep -c '^>')"
eq "A 주석 줄 불변" "$(grep '^#' "$T/app.sh.orig")" "$(grep '^#' "$T/proj/app.sh")"
eq "A 꼬리 주석 불변" 'auto_done_dir=""   # 로컬 done 경로' "$(grep '^auto_done_dir=' "$T/proj/app.sh")"
eq "A secret 파일 chmod 600" 600 "$(stat -c %a "$T/proj/app.sh")"
isok "A app.sh 백업 존재" bash -c 'ls "$1"/app.sh.bak.* ' _ "$T/proj"
isok "A conf 백업 존재" bash -c 'ls "$1"/conf/app.conf.bak.* ' _ "$T/proj"
isok "A bash -n" bash -n "$T/proj/app.sh"
nocr "A app.sh LF" "$T/proj/app.sh"
nocr "A 출력 LF" "$T/o"
has "A --undo 안내" "$T/o" "--undo"
restore_latest "$T/proj/app.sh" > /dev/null
isok "A --undo 로 원복" cmp "$T/proj/app.sh" "$T/app.sh.orig"

# B: 요약에서 n → 파일 불변, 백업도 안 만듦
mkfix; manifest_load "$T/proj/vars.manifest"
printf '%s\n' 2 os8mgmt "$T/real.sh" "" "" n > "$T/in"
run_guide os8 < "$T/in" > "$T/o" 2>&1; rc_is "B 취소 rc 1" 1 $?
has "B 취소 안내" "$T/o" "취소했습니다"
isok "B app.sh 불변" cmp "$T/proj/app.sh" "$T/app.sh.orig"
isok "B conf 불변" cmp "$T/proj/conf/app.conf" "$T/app.conf.orig"
eq "B 백업 안 만듦" 0 "$(ls "$T"/proj/app.sh.bak.* 2> /dev/null | wc -l)"

# C: 적용 중 실패 → 전체 원상복구 (선언 없는 conf 변수)
mkfix
cat > "$T/proj/vars.manifest" <<'EOS'
checker|sh:app.sh|path|common|-|y|체크 스크립트|/p|file|1.0.0|-
ghost|conf:conf/app.conf|text|common|-|n|선언 없는 변수|x|none|1.0.0|-
EOS
manifest_load "$T/proj/vars.manifest"
printf '%s\n' "$T/real.sh" "v" y > "$T/in"
sed -i '/^extra=/d' "$T/proj/conf/app.conf"; cp "$T/proj/conf/app.conf" "$T/app.conf.orig"
run_guide common < "$T/in" > "$T/o" 2>&1; rc_is "C 실패 rc 1" 1 $?
has "C 선언 없음 안내" "$T/o" "선언 줄을 찾지 못함: ghost"
has "C 원상복구 안내" "$T/o" "원상복구"
isok "C app.sh 원상복구" cmp "$T/proj/app.sh" "$T/app.sh.orig"
isok "C conf 불변" cmp "$T/proj/conf/app.conf" "$T/app.conf.orig"

# D: 택일 0(사용 안 함) → 의존 변수 질문 안 함, 필수 변수 건너뛰기
mkfix; manifest_load "$T/proj/vars.manifest" 2> /dev/null
cp "$T/app.sh.orig" "$T/proj/app.sh"
cat > "$T/proj/vars.manifest" <<'EOS'
#@xor done|어느 서버?|auto_done_dir:os8|auto_done_host:os6
auto_done_dir|sh:app.sh|path|os8|xor:done|n|로컬|/x|dir|1.0.0|-
auto_done_host|sh:app.sh|host|os6,os8|xor:done|n|원격|h|probe|1.0.0|-
checker|sh:app.sh|path|common|-|y|체크 스크립트|/p|file|1.0.0|-
gossh_pw|sh:app.sh|secret|common|dep:auto_done_host|n|비번|-|none|1.0.0|-
extra|conf:conf/app.conf|text|common|-|n|추가|x|none|1.0.0|-
EOS
manifest_load "$T/proj/vars.manifest"
printf '%s\n' 0 /nope s newextra y > "$T/in"
run_guide os8 < "$T/in" > "$T/o" 2>&1; rc_is "D rc" 0 $?
has "D 사용 안 함 선택" "$T/o" "선택: 사용 안 함"
has "D 의존 변수 질문 생략" "$T/o" "gossh_pw 는 auto_done_host 가 비어 있어 질문하지 않았습니다"
has "D 건너뜀 표시" "$T/o" "[!] checker"
has "D 건너뜀 이유" "$T/o" "건너뜀(이상: 필수 값이 비어 있음)"
eq "D conf 적용" 'extra=newextra' "$(grep '^extra=' "$T/proj/conf/app.conf")"
isok "D checker 그대로" bash -c '. "$1" > /dev/null; [ -z "$checker" ]' _ "$T/proj/app.sh"
isok "D app.sh 불변" cmp "$T/proj/app.sh" "$T/app.sh.orig"

# E: 역할 필터 — os6 에는 auto_done_dir 질문 없음, 택일 구성원 1개면 질문 없이 바로
mkfix; manifest_load "$T/proj/vars.manifest"
cat > "$T/proj/vars.manifest" <<'EOS'
#@xor done|어느 서버?|auto_done_dir:os8|auto_done_host:os6
auto_done_dir|sh:app.sh|path|os8|xor:done|n|로컬|/x|dir|1.0.0|-
auto_done_host|sh:app.sh|host|os6,os8|xor:done|n|원격|h|probe|1.0.0|-
checker|sh:app.sh|path|common|-|y|체크 스크립트|/p|file|1.0.0|-
EOS
manifest_load "$T/proj/vars.manifest"
printf '%s\n' h6 "$T/real.sh" y > "$T/in"
run_guide os6 < "$T/in" > "$T/o" 2>&1; rc_is "E rc" 0 $?
hasnt "E os8 전용 변수 질문 안 함" "$T/o" "auto_done_dir"
hasnt "E 택일 질문 없음" "$T/o" "어느 서버?"
eq "E host 적용" 'auto_done_host="h6"' "$(grep '^auto_done_host=' "$T/proj/app.sh")"
# role=common 이면 common 변수만
manifest_load "$T/proj/vars.manifest"; mkfix; manifest_load "$T/proj/vars.manifest"
guide_reset
printf '%s\n' "$T/real.sh" "" y > "$T/in"
cat > "$T/proj/vars.manifest" <<'EOS'
a1|sh:app.sh|path|os8|-|n|a|/x|none|1.0.0|-
checker|sh:app.sh|path|common|-|y|체크|/p|file|1.0.0|-
EOS
manifest_load "$T/proj/vars.manifest"
printf '%s\n' "$T/real.sh" y > "$T/in"
run_guide common < "$T/in" > "$T/o" 2>&1; rc_is "E2 rc" 0 $?
hasnt "E2 common 은 os8 전용 변수 제외" "$T/o" "a1"

# F: GUIDE_ONLY 부분집합
mkfix; manifest_load "$T/proj/vars.manifest"
printf '%s\n' v1 y > "$T/in"
GUIDE_ONLY="extra" run_guide os8 < "$T/in" > "$T/o" 2>&1; rc_is "F rc" 0 $?
hasnt "F 부분집합 외 변수 질문 안 함" "$T/o" "checker"
eq "F 적용" 'extra=v1' "$(grep '^extra=' "$T/proj/conf/app.conf")"

# G: 입력 중 EOF → rc 1, 파일 불변
mkfix; manifest_load "$T/proj/vars.manifest"
printf '%s\n' 2 os8mgmt > "$T/in"
run_guide os8 < "$T/in" > "$T/o" 2>&1; rc_is "G EOF rc 1" 1 $?
isok "G app.sh 불변" cmp "$T/proj/app.sh" "$T/app.sh.orig"
isok "G conf 불변" cmp "$T/proj/conf/app.conf" "$T/app.conf.orig"

# H: 변경 없음(모두 Enter) → 적용 단계 생략
mkfix; manifest_load "$T/proj/vars.manifest"
cat > "$T/proj/vars.manifest" <<'EOS'
extra|conf:conf/app.conf|text|common|-|n|추가|x|none|1.0.0|-
EOS
manifest_load "$T/proj/vars.manifest"
printf '\n' > "$T/in"
run_guide common < "$T/in" > "$T/o" 2>&1; rc_is "H rc" 0 $?
has "H 변경 없음 안내" "$T/o" "변경할 값이 없습니다"
eq "H 백업 없음" 0 "$(ls "$T"/proj/conf/app.conf.bak.* 2> /dev/null | wc -l)"

# I: CRLF 대상 파일도 처리, 결과 LF, 백업은 원본(CRLF) 보존
mkfix
sed 's/$/\r/' "$T/app.sh.orig" > "$T/proj/app.sh"
cp "$T/proj/app.sh" "$T/app.sh.crlf"
cat > "$T/proj/vars.manifest" <<'EOS'
checker|sh:app.sh|path|common|-|y|체크 스크립트|/p|file|1.0.0|-
EOS
manifest_load "$T/proj/vars.manifest"
printf '%s\n' "$T/real.sh" y > "$T/in"
run_guide common < "$T/in" > "$T/o" 2>&1; rc_is "I rc" 0 $?
nocr "I 결과 LF" "$T/proj/app.sh"
isok "I 백업은 원본(CRLF) 그대로" cmp "$(ls "$T"/proj/app.sh.bak.* | head -1)" "$T/app.sh.crlf"
eq "I 값 적용" "$T/real.sh" "$(bash -c '. "$1" > /dev/null; printf %s "$checker"' _ "$T/proj/app.sh")"

# ================================================================ 10. set -u 호환 스모크
mkfix
bash -c '
set -u
. "$1"
PROJECT_DIR=$2
manifest_load "$2/vars.manifest" || exit 11
target_get checker > /dev/null || exit 12
target_set checker "$3" > /dev/null || exit 13
check_value 2 "$3" > /dev/null || exit 14
summary_table > /dev/null
exit 0' _ "$LIB" "$T/proj" "$T/real.sh" > "$T/o" 2>&1
rc_is "set -u 에서 오류 없음" 0 $?
has "set -u 스모크 값 반영" "$T/proj/app.sh" "checker=\"$T/real.sh\""
printf '%s\n' 2 os8mgmt "$T/real.sh" "z" 'pw1' y > "$T/in"
mkfix
bash -c '
set -u
. "$1"
PROJECT_DIR=$2
manifest_load "$2/vars.manifest" || exit 11
run_guide os8' _ "$LIB" "$T/proj" < "$T/in" > "$T/o" 2>&1
rc_is "set -u 에서 run_guide" 0 $?
hasnt "set -u 에서 unbound 오류 없음" "$T/o" "unbound"

# ================================================================ 결과
printf 'PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
