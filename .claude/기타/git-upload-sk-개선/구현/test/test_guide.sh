#!/bin/bash
# test_guide.sh - templates/setup_guide.sh 시나리오 테스트
# 사용: bash test_guide.sh      (마지막 줄: PASS=n FAIL=m)
# 픽스처는 전부 mktemp 로 만든다. 입력은 stdin 파이프(비 tty).

HERE=$(cd "$(dirname "$0")" && pwd)
TPL="$HERE/../templates"

T=$(mktemp -d "${TMPDIR:-/tmp}/gus_guide.XXXXXX") || exit 2
trap 'rm -rf "$T"' EXIT

PASS=0; FAIL=0
pass() { PASS=$((PASS + 1)); }
fail() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$1"; [ -n "${2-}" ] && printf '      %s\n' "$2"; return 0; }
eq()    { if [ "$2" = "$3" ]; then pass; else fail "$1" "기대=[$2] 실제=[$3]"; fi; }
has()   { if grep -qF -- "$3" "$2"; then pass; else fail "$1" "'$3' 가 $2 에 없음"; fi; }
hasnt() { if grep -qF -- "$3" "$2"; then fail "$1" "'$3' 가 $2 에 있으면 안 됨"; else pass; fi; }
same()  { if cmp -s "$2" "$3"; then pass; else fail "$1" "파일이 달라짐: $3"; fi; }
nocr()  { if grep -q $'\r' "$2" 2> /dev/null; then fail "$1" "CR 이 남아 있음: $2"; else pass; fi; }
noesc() { if grep -q $'\033' "$2"; then fail "$1" "이스케이프 코드가 있음: $2"; else pass; fi; }

P=$T/proj          # 프로젝트 루트
OUT=$T/out.txt
RC=

# ---------------------------------------------------------------- 픽스처
mkfix() {   # mkfix full|small
    local kind=${1:-full}
    rm -rf "$P" "$T/real"
    mkdir -p "$P/setup" "$P/conf" "$T/real/dir"
    printf '#!/bin/bash\necho checker\n' > "$T/real/check.sh"
    cp "$TPL/setup_guide.sh" "$TPL/lib_common.sh" "$P/setup/"
    cat > "$P/app.sh" << 'EOS'
#!/bin/bash
# 담당자: 홍길동 (마스킹 보존)
auto_done_dir=""   # 로컬 done 경로
auto_done_host=""
checker=""
export gossh_pw=''
logdir=""
label=""
echo "$auto_done_dir"
# 감사합니다
EOS
    cat > "$P/conf/app.conf" << 'EOS'
# conf
extra=
other=1
EOS
    cat > "$P/setup.sh" << 'EOS'
#!/bin/bash
d=$(dirname "$0")
echo "ran in $(pwd)" > "$d/setup.ran"
[ -e "$d/setup.fail" ] && exit 3
echo "fake setup done"
exit 0
EOS
    if [ "$kind" = full ]; then
        cat > "$P/setup/vars.manifest" << 'EOS'
# 테스트 manifest (빈 변수 7개)
#@meta project=demo
#@meta version=1.0.0
#@meta setup=setup.sh
#@xor done|이 스크립트는 어느 서버에서 실행?|auto_done_dir:os8_mgmt 에서 실행(로컬 기록)|auto_done_host:os6_mgmt 에서 실행(원격 기록)
auto_done_dir|sh:app.sh|path|os8|xor:done|n|로컬 done 디렉터리\n두번째 줄|/tmp/auto_setup/done|dir|1.0.0|-
auto_done_host|sh:app.sh|host|os6,os8|xor:done|n|원격 호스트|os8mgmt|probe|1.0.0|-
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|file|1.0.0|-
gossh_pw|sh:app.sh|secret|common|dep:auto_done_host|n|gossh 비밀번호|-|none|1.0.0|-
extra|conf:conf/app.conf|text|common|-|n|추가 값|x|none|1.0.0|old_extra
logdir|sh:app.sh|path|os6|-|n|로그 디렉터리|/var/log/app|dir|1.0.0|-
label|sh:app.sh|text|common|-|n|표시 이름|서버A|none|1.0.0|-
EOS
    else
        # small: 빈 변수 2개(label, extra) + 값이 있는 checker, setup 지정 없음
        sed -i "s|^checker=\"\"|checker=\"$T/real/check.sh\"|" "$P/app.sh"
        cat > "$P/setup/vars.manifest" << 'EOS'
#@meta project=small
label|sh:app.sh|text|common|-|n|표시 이름|서버A|none|1.0.0|-
extra|conf:conf/app.conf|text|common|-|n|추가 값|x|none|1.0.0|-
checker|sh:app.sh|path|common|-|y|체크 스크립트 경로|/path/config_check.sh|file|1.0.0|-
EOS
    fi
    cp "$P/app.sh" "$T/app.sh.orig"
    cp "$P/conf/app.conf" "$T/app.conf.orig"
}

# runw <stdin 텍스트> <옵션...>  : setup/ 안에서 실행, 출력은 $OUT (stdout+stderr), 종료코드는 $RC
runw() {
    local inp=$1; shift
    printf '%s' "$inp" | (cd "$P/setup" && bash setup_guide.sh "$@") > "$OUT" 2>&1
    RC=$?
}

val_of() {   # val_of <파일> <변수> : sh 의 name="..." / name='...' 값 (단순 케이스)
    sed -n "s/^[[:space:]]*\(export[[:space:]]\+\)\?$2=[\"']\(.*\)[\"'].*/\2/p" "$1" | head -n 1
}

SECRET='S3cr3t!pw'
NL=$'\n'

# ================================================================ 1. 신규 프로젝트 전체 흐름 + setup.sh 연결
mkfix full
runw "1${NL}2${NL}os8mgmt${NL}$T/real/check.sh${NL}ex1${NL}my label${NL}${SECRET}${NL}y${NL}y${NL}"
eq "full: 종료코드 0" 0 "$RC"
eq "full: auto_done_host 적용" os8mgmt "$(val_of "$P/app.sh" auto_done_host)"
eq "full: checker 적용" "$T/real/check.sh" "$(val_of "$P/app.sh" checker)"
eq "full: label 적용" "my label" "$(val_of "$P/app.sh" label)"
eq "full: secret 평문 기록" "$SECRET" "$(val_of "$P/app.sh" gossh_pw)"
has "full: conf 적용" "$P/conf/app.conf" "extra=ex1"
eq "full: 미선택 xor 변수 불변" "" "$(val_of "$P/app.sh" auto_done_dir)"
eq "full: logdir(os6 전용) 불변" "" "$(val_of "$P/app.sh" logdir)"
eq "full: 변경된 원본 줄은 4줄뿐" 4 "$(diff "$T/app.sh.orig" "$P/app.sh" | grep -c '^<')"
has "full: 담당자 주석 보존" "$P/app.sh" "# 담당자: 홍길동 (마스킹 보존)"
has "full: 감사합니다 주석 보존" "$P/app.sh" "# 감사합니다"
eq "full: secret 파일 모드 600" 600 "$(stat -c %a "$P/app.sh")"
hasnt "full: 출력에 secret 없음" "$OUT" "$SECRET"
hasnt "full: 출력에 secret 부분 문자열 없음" "$OUT" "3cr3t"
has "full: 마스킹 표시" "$OUT" "****"
has "full: 평문 경고" "$OUT" "저장소에 커밋하지 마십시오"
has "full: 배너 프로젝트명" "$OUT" "demo 설정 가이드"
has "full: Disclaimer 시작" "$OUT" "Disclaimer"
has "full: 역할 질문" "$OUT" "이 서버의 역할을 선택하세요"
has "full: setup 질문" "$OUT" "setup.sh 를 이어서 실행하시겠습니까? (y/n)"
has "full: setup 실행됨" "$P/setup.ran" "ran in $P"
has "full: setup 출력" "$OUT" "fake setup done"
has "full: 랜덤 서버 확인 문구" "$OUT" "설정 변경 후 랜덤 서버 몇 대에서 실제 변경 확인"
has "full: 최종 요약" "$OUT" "== 최종 요약 =="
has "full: 최종 요약 값 변경 5개" "$OUT" "값 변경: 5개"
has "full: 백업 안내" "$OUT" "백업:"
nocr "full: app.sh LF" "$P/app.sh"
noesc "full: 비 tty 에서 색 코드 없음" "$OUT"
if ls "$P"/app.sh.bak.* > /dev/null 2>&1; then pass; else fail "full: app.sh 백업 생성"; fi
if ls "$P"/conf/app.conf.bak.* > /dev/null 2>&1; then pass; else fail "full: app.conf 백업 생성"; fi
if grep -q "$SECRET" "$P"/app.sh.bak.* 2> /dev/null; then fail "full: 백업에 secret 없어야 함(원본은 빈 값)"; else pass; fi

# ================================================================ 2. 빈 변수 <=5: n 답변
mkfix small
runw "n${NL}"
eq "small-n: 종료코드 0" 0 "$RC"
has "small-n: 확인 질문" "$OUT" "가이드를 실행하시겠습니까? (y/n)"
has "small-n: label 목록" "$OUT" "label"
has "small-n: extra 목록" "$OUT" "extra"
has "small-n: app.sh 위치 안내" "$OUT" "$P/app.sh"
has "small-n: app.conf 위치 안내" "$OUT" "$P/conf/app.conf"
hasnt "small-n: 값 있는 변수 checker 는 목록 제외" "$OUT" "checker"
hasnt "small-n: 역할 질문 없음" "$OUT" "역할을 선택"
same "small-n: app.sh 불변" "$T/app.sh.orig" "$P/app.sh"
same "small-n: app.conf 불변" "$T/app.conf.orig" "$P/conf/app.conf"
if ls "$P"/app.sh.bak.* > /dev/null 2>&1; then fail "small-n: 백업 만들면 안 됨"; else pass; fi

# 3. 빈 변수 <=5: y 답변 (역할 변수 없음 → 역할 질문 생략, setup 지정 없음 → 조용히 건너뜀)
mkfix small
runw "y${NL}L1${NL}E1${NL}${NL}y${NL}"
eq "small-y: 종료코드 0" 0 "$RC"
eq "small-y: label 적용" L1 "$(val_of "$P/app.sh" label)"
has "small-y: extra 적용" "$P/conf/app.conf" "extra=E1"
eq "small-y: checker 유지" "$T/real/check.sh" "$(val_of "$P/app.sh" checker)"
hasnt "small-y: 역할 질문 생략" "$OUT" "역할을 선택"
hasnt "small-y: setup 질문 없음" "$OUT" "이어서 실행"
has "small-y: 역할 자동 안내" "$OUT" "공통 변수만 진행"
has "small-y: 랜덤 서버 확인 문구" "$OUT" "설정 변경 후 랜덤 서버 몇 대에서 실제 변경 확인"
if [ -e "$P/setup.ran" ]; then fail "small-y: setup 실행되면 안 됨"; else pass; fi

# ================================================================ 4. xor: 1번 선택 (auto_done_dir), host 쪽 dep 변수는 질문 안 함
mkfix full
runw "1${NL}1${NL}$T/real/dir${NL}$T/real/check.sh${NL}${NL}${NL}y${NL}" --no-setup
eq "xor-1: 종료코드 0" 0 "$RC"
eq "xor-1: auto_done_dir 적용" "$T/real/dir" "$(val_of "$P/app.sh" auto_done_dir)"
eq "xor-1: auto_done_host 비어 있음" "" "$(val_of "$P/app.sh" auto_done_host)"
has "xor-1: 택일 질문 문구" "$OUT" "이 스크립트는 어느 서버에서 실행?"
has "xor-1: 선택지 설명" "$OUT" "os6_mgmt 에서 실행(원격 기록)"
has "xor-1: 의존 변수 질문 생략 안내" "$OUT" "gossh_pw 는 auto_done_host 가 비어 있어 질문하지 않았습니다"
hasnt "xor-1: gossh_pw 질문 안 함" "$OUT" "gossh 비밀번호"
# xor 0 (사용 안 함)
mkfix full
runw "1${NL}0${NL}$T/real/check.sh${NL}${NL}${NL}y${NL}" --no-setup
eq "xor-0: 종료코드 0" 0 "$RC"
eq "xor-0: 둘 다 비어 있음" "" "$(val_of "$P/app.sh" auto_done_dir)$(val_of "$P/app.sh" auto_done_host)"
has "xor-0: 사용 안 함 안내" "$OUT" "선택: 사용 안 함"

# ================================================================ 6. 잘못된 경로 → 다시 입력
mkfix full
runw "1${NL}0${NL}/nonexistent/check.sh${NL}$T/real/check.sh${NL}${NL}${NL}y${NL}" --no-setup
eq "invalid-retry: 종료코드 0" 0 "$RC"
has "invalid-retry: 이상 메시지" "$OUT" "파일 없음: /nonexistent/check.sh"
eq "invalid-retry: 재입력 값 적용" "$T/real/check.sh" "$(val_of "$P/app.sh" checker)"

# 7. 잘못된 경로 → 건너뛰기
mkfix full
runw "1${NL}0${NL}/nonexistent/check.sh${NL}s${NL}${NL}${NL}y${NL}" --no-setup
eq "invalid-skip: 종료코드 0" 0 "$RC"
has "invalid-skip: 건너뜀 안내" "$OUT" "checker 건너뜀"
has "invalid-skip: 점검 표에 건너뜀" "$OUT" "건너뜀(이상: 필수 값이 비어 있음)"
has "invalid-skip: 최종 요약 건너뜀 경고" "$OUT" "건너뛴 변수는 직접 채워야 합니다: checker"
eq "invalid-skip: checker 불변" "" "$(val_of "$P/app.sh" checker)"

# ================================================================ 8. 변경 요약 n → 변경 없음
mkfix full
runw "1${NL}2${NL}os8mgmt${NL}$T/real/check.sh${NL}ex1${NL}lab${NL}${SECRET}${NL}n${NL}"
eq "summary-n: 종료코드 1" 1 "$RC"
has "summary-n: 취소 안내" "$OUT" "취소했습니다"
same "summary-n: app.sh 불변" "$T/app.sh.orig" "$P/app.sh"
same "summary-n: app.conf 불변" "$T/app.conf.orig" "$P/conf/app.conf"
hasnt "summary-n: secret 출력 없음" "$OUT" "$SECRET"
if [ -e "$P/setup.ran" ]; then fail "summary-n: setup 실행되면 안 됨"; else pass; fi

# ================================================================ 9. 역할 필터링
mkfix full
runw "os6host${NL}$T/real/check.sh${NL}${NL}$T/real/dir${NL}${NL}${NL}" --role os6 --yes --no-setup
eq "role-os6: 종료코드 0" 0 "$RC"
has "role-os6: logdir 질문" "$OUT" "로그 디렉터리"
hasnt "role-os6: os8 전용 auto_done_dir 질문 안 함" "$OUT" "로컬 done 디렉터리"
hasnt "role-os6: 역할 질문 생략" "$OUT" "역할을 선택"
eq "role-os6: auto_done_host 적용" os6host "$(val_of "$P/app.sh" auto_done_host)"
eq "role-os6: logdir 적용" "$T/real/dir" "$(val_of "$P/app.sh" logdir)"
mkfix full
runw "0${NL}$T/real/check.sh${NL}${NL}${NL}y${NL}" --role os8 --no-setup
hasnt "role-os8: logdir 질문 안 함" "$OUT" "로그 디렉터리"
mkfix full
runw "$T/real/check.sh${NL}${NL}${NL}" --role common --yes --no-setup
eq "role-common: 종료코드 0" 0 "$RC"
hasnt "role-common: 택일 변수 질문 안 함" "$OUT" "원격 호스트"
hasnt "role-common: logdir 질문 안 함" "$OUT" "로그 디렉터리"
has "role-common: checker 질문" "$OUT" "체크 스크립트 경로"

# ================================================================ 10. --no-setup
mkfix full
runw "1${NL}0${NL}$T/real/check.sh${NL}${NL}${NL}y${NL}" --no-setup
eq "no-setup: 종료코드 0" 0 "$RC"
hasnt "no-setup: setup 질문 없음" "$OUT" "이어서 실행"
has "no-setup: 요약에 건너뜀 표시" "$OUT" "건너뜀 (--no-setup)"
if [ -e "$P/setup.ran" ]; then fail "no-setup: setup 실행되면 안 됨"; else pass; fi

# 10b. setup 에 n 답변 / setup 실패 / setup 파일 없음
mkfix full
runw "1${NL}0${NL}$T/real/check.sh${NL}${NL}${NL}y${NL}n${NL}"
eq "setup-n: 종료코드 0" 0 "$RC"
has "setup-n: 실행 안 함 표시" "$OUT" "실행 안 함"
if [ -e "$P/setup.ran" ]; then fail "setup-n: setup 실행되면 안 됨"; else pass; fi
mkfix full
: > "$P/setup.fail"
runw "1${NL}0${NL}$T/real/check.sh${NL}${NL}${NL}y${NL}y${NL}"
eq "setup-fail: 종료코드 1" 1 "$RC"
has "setup-fail: 실패 안내" "$OUT" "종료코드 3"
eq "setup-fail: 설정 변경은 유지" "$T/real/check.sh" "$(val_of "$P/app.sh" checker)"
mkfix full
rm -f "$P/setup.sh"
runw "1${NL}0${NL}$T/real/check.sh${NL}${NL}${NL}y${NL}"
eq "setup-missing: 종료코드 0" 0 "$RC"
hasnt "setup-missing: 조용히 건너뜀" "$OUT" "이어서 실행"

# ================================================================ 11. CRLF 대상 정규화
mkfix full
sed -i 's/$/\r/' "$P/app.sh" "$P/conf/app.conf"
runw "1${NL}0${NL}$T/real/check.sh${NL}ex2${NL}lab${NL}y${NL}" --no-setup
eq "crlf: 종료코드 0" 0 "$RC"
nocr "crlf: app.sh LF" "$P/app.sh"
nocr "crlf: app.conf LF" "$P/conf/app.conf"
has "crlf: CR 제거 안내" "$OUT" "CR 제거"
eq "crlf: checker 적용" "$T/real/check.sh" "$(val_of "$P/app.sh" checker)"
has "crlf: 담당자 주석 보존" "$P/app.sh" "# 담당자: 홍길동 (마스킹 보존)"

# ================================================================ 12. NO_COLOR / 비 tty 에 이스케이프 없음
mkfix full
printf '1\n0\n%s\n\n\ny\n' "$T/real/check.sh" | (cd "$P/setup" && NO_COLOR=1 bash setup_guide.sh --no-setup) > "$OUT" 2>&1
noesc "color: NO_COLOR 에 이스케이프 없음" "$OUT"
has "color: 접두 [O] 유지" "$OUT" "[O]"
has "color: 접두 [i] 유지" "$OUT" "[i]"
# tty 에서는 색이 켜지는지 (script 명령이 있을 때만; 없거나 동작하지 않으면 SKIP)
if command -v script > /dev/null 2>&1; then
    mkfix full
    printf '1\n0\n%s\n\n\ny\n' "$T/real/check.sh" > "$T/tty.in"
    script -qec "cd '$P/setup' && bash setup_guide.sh --no-setup" /dev/null < "$T/tty.in" > "$OUT" 2>&1
    if grep -q $'\033\\[3[0-9]m' "$OUT"; then
        pass
        mkfix full
        script -qec "cd '$P/setup' && NO_COLOR=1 bash setup_guide.sh --no-setup" /dev/null < "$T/tty.in" > "$OUT" 2>&1
        noesc "color: tty 라도 NO_COLOR 면 색 없음" "$OUT"
    else
        printf 'SKIP: script 로 tty 색 확인 불가\n'
    fi
fi

# ================================================================ 13. 인자 오류 → 종료코드 2
mkfix full
runw "" --bogus
eq "args: 알 수 없는 옵션 2" 2 "$RC"
has "args: 사용법 출력" "$OUT" "사용법: bash setup_guide.sh"
runw "" --role foo
eq "args: 잘못된 role 2" 2 "$RC"
runw "" --role
eq "args: role 값 없음 2" 2 "$RC"
runw "" --dir
eq "args: dir 값 없음 2" 2 "$RC"
runw "" --dir /nonexistent/dir/xyz
eq "args: 없는 dir 2" 2 "$RC"
runw "" --dir "$T/real"
eq "args: 대상 파일 없는 dir 2" 2 "$RC"
has "args: 대상 파일 없음 안내" "$OUT" "대상 파일 없음"
runw "" --help
eq "args: --help 0" 0 "$RC"
has "args: --help 사용법" "$OUT" "--no-setup"
mkdir -p "$T/nomf" && cp "$TPL/setup_guide.sh" "$TPL/lib_common.sh" "$T/nomf/"
printf '' | (cd "$T/nomf" && bash setup_guide.sh) > "$OUT" 2>&1
eq "args: manifest 없음 2" 2 "$?"
mkdir -p "$T/nolib" && cp "$TPL/setup_guide.sh" "$T/nolib/"
printf '' | (cd "$T/nolib" && bash setup_guide.sh) > "$OUT" 2>&1
eq "args: lib 없음 2" 2 "$?"
mkfix full
printf 'broken|line\n' > "$P/setup/vars.manifest"
runw "" --role os8
eq "args: manifest 형식 오류 2" 2 "$RC"

# ================================================================ 14. --dir 로 다른 프로젝트 루트 지정
mkfix full
rm -rf "$T/proj2"; mkdir -p "$T/proj2/conf"
cp "$P/app.sh" "$T/proj2/app.sh"; cp "$P/conf/app.conf" "$T/proj2/conf/app.conf"
runw "1${NL}0${NL}$T/real/check.sh${NL}${NL}${NL}y${NL}" --dir "$T/proj2" --no-setup
eq "dir: 종료코드 0" 0 "$RC"
eq "dir: 지정한 루트의 파일이 변경됨" "$T/real/check.sh" "$(val_of "$T/proj2/app.sh" checker)"
same "dir: 기본 루트의 파일은 불변" "$T/app.sh.orig" "$P/app.sh"

# ================================================================ 15. 입력 종료(EOF)
mkfix full
runw ""
eq "eof: 역할 질문에서 EOF → 1" 1 "$RC"
same "eof: app.sh 불변" "$T/app.sh.orig" "$P/app.sh"
mkfix full
runw "1${NL}0${NL}" --no-setup
eq "eof: 변수 질문 중 EOF → 1" 1 "$RC"
same "eof(2): app.sh 불변" "$T/app.sh.orig" "$P/app.sh"

# ================================================================ 16. --yes: 확인 질문 자동 y
mkfix full
runw "1${NL}0${NL}$T/real/check.sh${NL}${NL}${NL}" --yes
eq "yes: 종료코드 0" 0 "$RC"
has "yes: 자동 y 표시" "$OUT" "y (자동)"
eq "yes: 적용됨" "$T/real/check.sh" "$(val_of "$P/app.sh" checker)"
if [ -e "$P/setup.ran" ]; then pass; else fail "yes: setup 자동 실행"; fi

printf 'PASS=%d FAIL=%d\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
