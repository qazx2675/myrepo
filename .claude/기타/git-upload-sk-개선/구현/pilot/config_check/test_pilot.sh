#!/bin/bash
# test_pilot.sh - config_check 파일럿 시나리오 테스트 (랩에서 실행; gossh 는 스텁, 실서버 접속 없음)
# 사용: bash test_pilot.sh      (마지막 줄: PASS=n FAIL=m)
#
#   0. 정적: NEW 는 OLD 에 대한 삽입 전용 변경인가, setup/ 에는 3개 파일만 있는가
#   E. 동작 동일성: NEW -auto = 브랜치 버전(-auto) / NEW 수동 모드 = OLD (바이트 단위)  + 브랜치의 run_auto_test.sh 18개
#   A. 신규 프로젝트: 빈 변수 사본 + 자체포함 가이드 (택일·잘못된 입력 재입력·건너뜀·역할·변수 5개 이하)
#   B. 업데이트: 현장 사본(벤더 문자열·경로·주석·user= 줄 수정)에 update_v<ver>.sh 적용
#   C. 이상 상황: 앵커 줄이 바뀐 사본, CRLF, 잘린 붙여넣기, 재실행(멱등)
#   D. 비교: 예전 diff 패치(apply_auto_mode.sh) 방식은 같은 사본에서 실패
#
# 실제 현장 값(실경로·실서버)은 검증할 수 없다 — 전부 스텁·임시 디렉터리 기반이다.

ROOT=$(cd "$(dirname "$0")" && pwd)
SETUP="$ROOT/setup"
OLD="$ROOT/old/config_check.sh"
NEW="$ROOT/new/config_check.sh"
REF="$ROOT/ref"
VER=1.1.0
UPD="$SETUP/update_v${VER}.sh"
GUIDE="$SETUP/setup_guide.sh"

T=$(mktemp -d "${TMPDIR:-/tmp}/gus_pilot.XXXXXX") || exit 2
trap 'rm -rf "$T"' EXIT

PASS=0; FAIL=0
pass() { PASS=$((PASS + 1)); }
fail() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$1"; [ -n "${2-}" ] && printf '      %s\n' "$2"; return 0; }
eq()    { if [ "$2" = "$3" ]; then pass; else fail "$1" "기대=[$2] 실제=[$3]"; fi; }
isok()  { local n=$1; shift; if "$@" > /dev/null 2>&1; then pass; else fail "$n" "명령 실패: $*"; fi; }
has()   { if grep -qF -- "$3" "$2"; then pass; else fail "$1" "'$3' 가 $2 에 없음"; fi; }
hasnt() { if grep -qF -- "$3" "$2"; then fail "$1" "'$3' 가 $2 에 있으면 안 됨"; else pass; fi; }
same()  { if cmp -s "$2" "$3"; then pass; else fail "$1" "파일이 달라짐: $(diff "$2" "$3" | head -6 | tr '\n' '|')"; fi; }
nocr()  { if grep -q $'\r' "$2" 2> /dev/null; then fail "$1" "CR 이 남아 있음: $2"; else pass; fi; }
nobak() { if ls "$2".bak.* > /dev/null 2>&1; then fail "$1" "백업이 생기면 안 됨: $2"; else pass; fi; }
absent() { if [ -e "$2" ]; then fail "$1" "파일이 있으면 안 됨: $2"; else pass; fi; }
nrm()   { diff "$1" "$2" | grep -c '^<'; }          # 원본에서 사라진/바뀐 줄 수

# ---------------------------------------------------------------- 환경: gossh 스텁 (run_auto_test.sh 와 같은 방식)
W=$T/w
mkdir -p "$W/bin" "$W/run" "$W/done_local" "$W/asdir" "$W/asdf"
cat > "$W/bin/gossh" <<'STUB'
#!/bin/bash
args=("$@"); f=""; cmd=""
i=0
while [ $i -lt ${#args[@]} ]; do
    case "${args[$i]}" in -w) f="${args[$((i+1))]}"; i=$((i+2)); continue ;; -pm|-script) ;; *) cmd="${args[$i]}" ;; esac
    i=$((i+1))
done
echo "$cmd" >> "$GOSSH_LOG"
case "$cmd" in
    "bash -c '"*) bash -c "$cmd" | sed 's/^/done-host: /'; exit 0 ;;
    *run.sh*)
        while read -r h; do
            case "$h" in
                down*) echo "$h" >> "${f}_res_off" ;;
                fail*) echo "$h: FAIL usb0 interface check"; echo "$h: INFO LDAP infraA std 1" ;;
                err*)  echo "$h: ERROR connect" ;;
                *)     echo "$h: OK"; echo "$h: INFO LDAP infraA std 1" ;;
            esac
        done < "$f" ;;
    *) while read -r h; do echo "$h: ok"; done < "$f" ;;
esac
STUB
chmod +x "$W/bin/gossh"
export PATH="$W/bin:$PATH" GOSSH_LOG="$W/gossh.log" AUTO_SETUP_DIR="$W/asdir" NO_COLOR=1
: > "$GOSSH_LOG"
printf 'echo "1) u1"\n' > "$W/info_mn.sh"
printf 'echo u1\n' > "$W/info.sh"
# 현장 user= 줄이 부르는 스크립트: 실행되면 흔적을 남긴다 (-auto 에서는 실행되면 안 됨)
printf 'touch "%s/a_sh_ran"\necho u1\n' "$W" > "$W/asdf/a.sh"
printf 'pa01 pa02\nfail01 down01\nerr01\n' > "$W/targets.txt"
printf 'ab01 ab02\n' > "$W/t2.txt"

# ---------------------------------------------------------------- 사본 만들기
# mk_stub <원본> <출력> [done_dir] [done_host] : user 선택 스텁·완료기록 변수 주입 (run_auto_test.sh 의 make_copy 와 같음)
mk_stub() {
    sed -e "s|^user_info_mn=.*|user_info_mn=\"$W/info_mn.sh\"|" \
        -e "s|^user_info_output=.*|user_info_output=\"$W/info.sh\"|" \
        -e "s|^auto_done_dir=\"\"|auto_done_dir=\"${3-}\"|" \
        -e "s|^auto_done_host=\"\"|auto_done_host=\"${4-}\"|" \
        "$1" > "$2"
}

# mk_field <출력> : 현장 사본 = OLD + 손으로 고친 흔적들 (벤더 문자열·경로·주석·코멘트 문구·user= 줄·이미 채운 변수)
mk_field() {
    sed -e 's|^home="/user/siy/MGMT"|home="/opt/company/MGMT"|' \
        -e 's|^setting_home=.*|setting_home="/opt/company/SA/SETTING"|' \
        -e 's|^check_script=.*|check_script="/opt/company/SA/os/run.sh"|' \
        -e 's|^os6_host="".*|os6_host="os6mgmt01"      # 회사 OS6 관리서버 (OO 수정)|' \
        -e 's|^uptime_enable_user="".*|uptime_enable_user="u1"    # uptime 확인 대상 user (회사 값)|' \
        -e 's|^dhcp_server="".*|dhcp_server="root@10.1.1.5"   # 회사 DHCP 서버|' \
        -e 's|^# config_check.sh - .*|# config_check.sh - 회사 현장 수정본 (OO팀, 2026-10)|' \
        -e 's|^# 파일의 줄 수$|# 파일의 줄 수 (OO 수정)|' \
        -e 's|^########################## 1. user 선택 ##########################|########################## 1. user 선택 (회사: 메뉴 방식 변경) ####|' \
        -e 's|return "D"|return "DELL"|' \
        -e 's|for v in D S L J T W|for v in DELL S L J T W|' \
        -e 's|if \[ "\$v" = D \]|if [ "$v" = DELL ]|' \
        -e 's|안녕하세요 D 담당자님|안녕하세요 DELL 담당자님|' \
        -e 's|점검필요하신지 확인 부탁드립니다\.|점검필요하신지 확인 부탁드립니다 (OO).|' \
        -e 's|^# DHCP 정보 (접속불가 코멘트 아래)|# DHCP 정보 (OO 현장: 접속불가 코멘트 아래)|' \
        -e 's|^user=\$(bash "\$user_info_output".*|user=`bash '"$W"'/asdf/a.sh $user_choice`|' \
        -e "s|^user_info_mn=.*|user_info_mn=\"$W/info_mn.sh\"|" \
        "$OLD" > "$1"
}
# 현장 수정 흔적 목록 (업데이트 뒤에도 그대로 있어야 하는 줄)
FIELD_MARKS=(
    'home="/opt/company/MGMT"'
    'setting_home="/opt/company/SA/SETTING"'
    'check_script="/opt/company/SA/os/run.sh"'
    'os6_host="os6mgmt01"      # 회사 OS6 관리서버 (OO 수정)'
    'uptime_enable_user="u1"    # uptime 확인 대상 user (회사 값)'
    'dhcp_server="root@10.1.1.5"   # 회사 DHCP 서버'
    '# config_check.sh - 회사 현장 수정본 (OO팀, 2026-10)'
    '# 파일의 줄 수 (OO 수정)'
    '########################## 1. user 선택 (회사: 메뉴 방식 변경) ####'
    '    if (h ~ /^(c|h|sh|s2h|s3h|s4h)/) return "DELL"'
    'for v in DELL S L J T W; do'
    '    if [ "$v" = DELL ]; then'
    '            echo "안녕하세요 DELL 담당자님"'
    '                echo "점검필요하신지 확인 부탁드립니다 (OO)."'
    '            echo "점검필요하신지 확인 부탁드립니다 (OO)."'
    '# DHCP 정보 (OO 현장: 접속불가 코멘트 아래)'
)
FIELD_USERLINE="user=\`bash $W/asdf/a.sh \$user_choice\`"

# mk_proj <프로젝트 디렉터리> <config_check.sh 사본> : setup/ 에는 배포 파일만 복사
mk_proj() {
    rm -rf "$1"; mkdir -p "$1/setup"
    cp "$2" "$1/config_check.sh"
    cp "$SETUP/vars.manifest" "$SETUP/setup_guide.sh" "$UPD" "$1/setup/"
}

# 출력 정규화: 소요 시간 제거
normo() { sed -E 's/\(소요 [0-9]+초\)/(소요 N초)/' "$1"; }
# 완료기록 정규화: epoch 제거
normdone() { local d=$1 f; for f in "$d"/*; do [ -f "$f" ] || continue; printf '%s: %s\n' "$(basename "$f")" "$(sed -E 's/^[0-9]+ //' "$f")"; done; }

# run_auto <스크립트> <출력파일> [목록파일] : $W/run 에서 -auto 실행 (stdin 없음)
run_auto() {
    rm -rf "$W/run"; mkdir -p "$W/run"
    : > "$GOSSH_LOG"
    (cd "$W/run" && bash "$1" -auto u1 "${3:-$W/targets.txt}" < /dev/null > "$2" 2>&1)
    return $?
}
# run_manual <스크립트> <출력파일> <stdin 텍스트>
run_manual() {
    rm -rf "$W/run"; mkdir -p "$W/run"
    printf 'ab01 ab02\nfail01\n' > "$W/run/u1.txt"
    : > "$GOSSH_LOG"
    (cd "$W/run" && printf '%s' "$3" | bash "$1" > "$2" 2>&1)
    return $?
}

NL=$'\n'

# ================================================================ 0. 정적 검사
isok "bash -n new/config_check.sh" bash -n "$NEW"
isok "bash -n setup/setup_guide.sh" bash -n "$GUIDE"
isok "bash -n setup/update_v${VER}.sh" bash -n "$UPD"
for f in "$NEW" "$OLD" "$GUIDE" "$UPD" "$SETUP/vars.manifest" "$ROOT/vars.manifest" "$ROOT/test_pilot.sh" "$ROOT/build.sh"; do nocr "CR 없음: ${f#$ROOT/}" "$f"; done
eq "setup/ 에는 3개 파일뿐" "setup_guide.sh update_v${VER}.sh vars.manifest" "$(ls "$SETUP" | tr '\n' ' ' | sed 's/ $//')"
same "setup/vars.manifest = 최상위 vars.manifest" "$ROOT/vars.manifest" "$SETUP/vars.manifest"
eq "NEW 는 OLD 에 대한 삽입 전용 변경 (사라진·바뀐 줄 0)" 0 "$(nrm "$OLD" "$NEW")"
eq "NEW 에서 추가된 줄 수 = 줄 수 차이" "$(( $(wc -l < "$NEW") - $(wc -l < "$OLD") ))" "$(diff "$OLD" "$NEW" | grep -c "^>")"
eq "생성물의 변경 명세는 전부 insert_* (replace 0건)" 0 "$(sed -n '/^#__SPEC_BEGIN__$/,/^#__SPEC_END__$/p' "$UPD" | grep -c '^@change .* replace_')"
n=$(grep -v '^[[:space:]]*#' "$NEW" | grep -cE '\b(python[0-9]*|perl)\b|local -n|declare -g|\$\{[A-Za-z_]+\^\^|\[\[ -v |mapfile -d|read -N|;;&|\|&')
eq "NEW: 금지 기능(python/perl/4.2+) 없음" 0 "$n"
if [ -x "$UPD" ] && [ -x "$GUIDE" ]; then pass; else fail "setup 스크립트 실행 권한"; fi

# ================================================================ E. 동작 동일성
# E1. 브랜치의 run_auto_test.sh (18개) 를 NEW 로 실행
mkdir -p "$T/rt/test"; cp "$NEW" "$T/rt/config_check.sh"; cp "$REF/run_auto_test.sh" "$T/rt/test/"
bash "$T/rt/test/run_auto_test.sh" > "$T/rt.out" 2>&1
eq "E1 브랜치 run_auto_test.sh(NEW): 18 PASS / 0 FAIL" "=== PASS 18 / FAIL 0 ===" "$(tail -n 1 "$T/rt.out")"
# E2. -auto 출력·산출물이 브랜치 버전과 같다 (완료기록 설정 3가지)
for cfg in none dir host; do
    case $cfg in none) dd=""; dh="" ;; dir) dd="$W/done_local"; dh="" ;; host) dd=""; dh="os8host" ;; esac
    mk_stub "$ROOT/ref/config_check.branch.sh" "$T/e_br.sh" "$dd" "$dh"
    mk_stub "$NEW" "$T/e_new.sh" "$dd" "$dh"
    for who in br new; do
        rm -rf "$W/done_local"/* "$W/asdir"/*
        run_auto "$T/e_$who.sh" "$T/e_$who.out"; eval "rc_$who=$?"
        normo "$T/e_$who.out" > "$T/e_$who.norm"
        { normdone "$W/done_local"; normdone "$W/asdir/done"; } > "$T/e_$who.done"
        cp "$W/run/check.res_u1_postapply" "$T/e_$who.post" 2> /dev/null || : > "$T/e_$who.post"
        ls "$W/run" | LC_ALL=C sort > "$T/e_$who.ls"
        cp "$GOSSH_LOG" "$T/e_$who.glog"
    done
    eq "E2[$cfg] -auto 종료코드 같음" "$rc_br" "$rc_new"
    same "E2[$cfg] -auto 화면 출력이 브랜치 버전과 바이트 단위로 같음" "$T/e_br.norm" "$T/e_new.norm"
    same "E2[$cfg] 완료기록 파일이 같음" "$T/e_br.done" "$T/e_new.done"
    same "E2[$cfg] postapply 파일이 같음" "$T/e_br.post" "$T/e_new.post"
    same "E2[$cfg] 생성 파일 목록이 같음" "$T/e_br.ls" "$T/e_new.ls"
    same "E2[$cfg] gossh 호출 명령이 같음" "$T/e_br.glog" "$T/e_new.glog"
done
has "E2 (참고) -auto 출력에 결과 리포트 마커" "$T/e_new.out" "############### 결과 리포트 ###############"
# E3. 수동 모드 = OLD (여러 입력 경로)
mk_stub "$OLD" "$T/m_old.sh"; mk_stub "$NEW" "$T/m_new.sh"
for inp in "1${NL}y${NL}n${NL}" "1${NL}y${NL}y${NL}" "1${NL}y${NL}set${NL}" "1${NL}n${NL}" "1${NL}maybe${NL}y${NL}zz${NL}n${NL}" "1${NL}y${NL}"; do
    for who in old new; do
        run_manual "$T/m_$who.sh" "$T/m_$who.out" "$inp"; eval "mrc_$who=$?"
        normo "$T/m_$who.out" > "$T/m_$who.norm"
        ls "$W/run" | LC_ALL=C sort > "$T/m_$who.ls"
        cp "$GOSSH_LOG" "$T/m_$who.glog"
    done
    lbl=$(printf '%s' "$inp" | tr '\n' ',')
    eq "E3[$lbl] 수동 모드 종료코드 = OLD" "$mrc_old" "$mrc_new"
    same "E3[$lbl] 수동 모드 화면 출력 = OLD (바이트 단위)" "$T/m_old.norm" "$T/m_new.norm"
    same "E3[$lbl] 수동 모드 생성 파일 목록 = OLD" "$T/m_old.ls" "$T/m_new.ls"
    same "E3[$lbl] 수동 모드 gossh 호출 = OLD" "$T/m_old.glog" "$T/m_new.glog"
done
run_manual "$T/m_new.sh" "$T/m_full.out" "1${NL}y${NL}n${NL}"; has "E3 (참고) 수동 모드가 끝까지 진행됨(마무리 출력)" "$T/m_full.out" "대 OS 설치 완료하였습니다."
# 수동 모드에서도 NEW 에 인자를 줘도 OLD 와 같이 무시 (예: 잘못된 첫 인자)
(cd "$W/run" && printf '1\ny\nn\n' | bash "$T/m_new.sh" foo bar > "$T/m_arg.out" 2>&1); a1=$?
(cd "$W/run" && printf '1\ny\nn\n' | bash "$T/m_old.sh" foo bar > "$T/m_argo.out" 2>&1); a2=$?
eq "E3 수동 모드 임의 인자 처리 = OLD" "$a2" "$a1"
same "E3 수동 모드 임의 인자 출력 = OLD" <(normo "$T/m_argo.out") <(normo "$T/m_arg.out")

# ================================================================ A. 신규 프로젝트: 빈 변수 사본 + 자체포함 가이드
DONE=$T/done_dir; mkdir -p "$DONE"
val() { sed -n "s/^$2=\"\([^\"]*\)\".*/\1/p" "$1" | head -n 1; }   # 설정 구역의 name="값" 줄 값
PA=$T/projA
runa() {   # runa <stdin> <가이드 옵션...> : 가이드는 setup/ 안에서 실행
    printf '%s' "$1" | (cd "$PA/setup" && bash setup_guide.sh "${@:2}") > "$T/a.out" 2>&1
    ARC=$?
}
# 가이드 단독 배포 확인: setup/ 에는 lib_common.sh 가 없다 (자체포함)
mk_proj "$PA" "$NEW"
absent "A 자체포함: setup/ 에 lib_common.sh 없음" "$PA/setup/lib_common.sh"
cp "$NEW" "$T/a.orig"

# A1. 역할 os8, 택일에서 auto_done_dir 선택. 잘못된 호스트·없는 디렉터리 → 재입력
runa "1${NL}1${NL}os6mgmt01${NL}user1|user2${NL}gpu01|gpu02${NL}bad host!${NL}root@10.0.0.9${NL}/nonexistent/x${NL}${DONE}${NL}/opt/ai/check.sh${NL}y${NL}"
eq "A1 종료코드 0" 0 "$ARC"
CC=$PA/config_check.sh
eq "A1 os6_host 적용" os6mgmt01 "$(val "$CC" os6_host)"
eq "A1 uptime_enable_user 적용(세로선 포함)" 'user1|user2' "$(val "$CC" uptime_enable_user)"
eq "A1 ai_server_list 적용" 'gpu01|gpu02' "$(val "$CC" ai_server_list)"
eq "A1 ai_server_script 적용(의존: ai_server_list 가 채워져서 질문됨)" /opt/ai/check.sh "$(val "$CC" ai_server_script)"
eq "A1 dhcp_server: 잘못된 값 뒤 재입력한 값" root@10.0.0.9 "$(val "$CC" dhcp_server)"
eq "A1 auto_done_dir(택일 선택) 적용" "$DONE" "$(val "$CC" auto_done_dir)"
eq "A1 auto_done_host 는 택일에서 빠져 빈 값" "" "$(val "$CC" auto_done_host)"
eq "A1 변경된 원본 줄은 6줄뿐" 6 "$(nrm "$T/a.orig" "$CC")"
eq "A1 추가된 줄도 6줄뿐" 6 "$(diff "$T/a.orig" "$CC" | grep -c '^>')"
isok "A1 결과 bash -n" bash -n "$CC"
nocr "A1 결과 LF" "$CC"
has "A1 소스 주석 보존(담당자 아닌 일반 주석)" "$CC" "# gossh 는 Ctrl+C 를 직접 처리한다"
has "A1 역할 질문" "$T/a.out" "이 서버의 역할을 선택하세요"
has "A1 택일 상황 질문" "$T/a.out" "이 스크립트는 어느 서버에서 실행됩니까?"
has "A1 택일 선택지 1" "$T/a.out" "1) auto_done_dir"
has "A1 택일 선택지 2" "$T/a.out" "2) auto_done_host"
has "A1 택일 선택지 0(사용 안 함)" "$T/a.out" "0) 사용 안 함"
has "A1 호스트 형식 오류 이유 표시" "$T/a.out" "이상: 호스트명 형식 오류"
has "A1 디렉터리 없음 이유 표시" "$T/a.out" "이상: 디렉터리 없음: /nonexistent/x"
has "A1 설명(요구사항 2-1): 기록 형식" "$T/a.out" 'epoch user sha256 source'
has "A1 설명(요구사항 2-1): 데몬 경로" "$T/a.out" '${AUTO_SETUP_DIR:-/tmp/auto_setup}/done/<호스트>'
has "A1 설명(요구사항 2-1): 누가 기록" "$T/a.out" '스크립트가 자기 서버의 로컬 디렉터리에 직접 기록한다'
has "A1 설명(요구사항 2-1): done 경로 일치" "$T/a.out" '데몬이 읽는 done/ 디렉터리와 같은 경로'
has "A1 설명(요구사항 2-1): os6 에서 실행하면 데몬이 못 봄" "$T/a.out" 'os6 로컬에 기록되어 데몬이 못 본다'
has "A1 설명(요구사항 2-1): autofs 공유 시 host 만" "$T/a.out" 'auto_done_host 만 채운다'
has "A1 설명(요구사항 2-1): 에러 없는 호스트만" "$T/a.out" 'ERROR·접속불가 호스트는 기록하지 않음'
has "A1 설명: 둘 다 비면 아무 동작 없음" "$T/a.out" '둘 다 비면 아무 동작도 하지 않는다'
has "A1 예시 표시" "$T/a.out" "예: /tmp/auto_setup/done"
has "A1 최종 요약 변경 6개" "$T/a.out" "값 변경: 6개"
hasnt "A1 setup.sh 이어서 실행 질문 없음(config_check 는 setup.sh 없음)" "$T/a.out" "이어서 실행하시겠습니까"
has "A1 Disclaimer" "$T/a.out" "랜덤 서버 몇 대에서 실제 변경 확인"
if ls "$PA"/config_check.sh.bak.* > /dev/null 2>&1; then pass; else fail "A1 백업 생성"; fi

# A2. 역할 os6(이름 입력): 택일은 auto_done_host 하나뿐이라 질문 없이 바로 변수. Enter=유지, 잘못된 값 → 건너뜀(s)
mk_proj "$PA" "$NEW"
runa "os6${NL}${NL}${NL}${NL}bad!${NL}s${NL}os8mgmt01${NL}y${NL}"
eq "A2 종료코드 0" 0 "$ARC"
eq "A2 auto_done_host 적용" os8mgmt01 "$(val "$CC" auto_done_host)"
eq "A2 os6 역할이라 auto_done_dir 는 질문·변경 없음" "" "$(val "$CC" auto_done_dir)"
eq "A2 건너뛴 dhcp_server 는 빈 값" "" "$(val "$CC" dhcp_server)"
eq "A2 Enter 유지: os6_host 빈 값 그대로" "" "$(val "$CC" os6_host)"
eq "A2 변경된 원본 줄 1줄" 1 "$(nrm "$T/a.orig" "$CC")"
has "A2 건너뜀 표시" "$T/a.out" "dhcp_server 건너뜀"
has "A2 건너뛴 변수 안내" "$T/a.out" "건너뛴 변수는 직접 채워야 합니다: dhcp_server"
has "A2 의존 변수 질문 안 함 안내" "$T/a.out" "ai_server_script 는 ai_server_list 가 비어 있어 질문하지 않았습니다"
hasnt "A2 택일 질문 없음(구성원 1개)" "$T/a.out" "어느 서버에서 실행됩니까"
hasnt "A2 auto_done_dir 질문 안 함" "$T/a.out" "auto_done_dir  ("

# A3. 택일에서 0(사용 안 함) + 나머지 Enter → 변경할 값이 없음, 파일 불변·백업 없음
mk_proj "$PA" "$NEW"
runa "0${NL}${NL}${NL}${NL}${NL}" --role os8
eq "A3 종료코드 0" 0 "$ARC"
same "A3 파일 불변" "$T/a.orig" "$CC"
has "A3 안내" "$T/a.out" "변경할 값이 없습니다"
nobak "A3 백업 없음" "$CC"
# A4. --role common: 공통 변수만 (auto_done_* 는 역할 변수라 질문 없음)
mk_proj "$PA" "$NEW"
runa "${NL}${NL}${NL}${NL}" --role common
eq "A4 종료코드 0" 0 "$ARC"
hasnt "A4 common 역할에는 auto_done_* 질문 없음" "$T/a.out" "auto_done"
hasnt "A4 택일 질문 없음" "$T/a.out" "택일"
same "A4 파일 불변" "$T/a.orig" "$CC"

# A5. 변수 5개 이하(4개) 변형 manifest: 실행 여부를 먼저 묻는다
mk_proj "$PA" "$NEW"
{ grep -E '^#@(meta|guard|xor)' "$ROOT/vars.manifest"; grep -E '^(os6_host|dhcp_server|auto_done_dir|auto_done_host)\|' "$ROOT/vars.manifest"; } > "$PA/setup/vars.manifest"
eq "A5 변형 manifest 변수 4개" 4 "$(grep -c '^[a-z0-9_]*|sh:' "$PA/setup/vars.manifest")"
runa "n${NL}"
eq "A5 n: 종료코드 0" 0 "$ARC"
has "A5 n: 확인 질문" "$T/a.out" "가이드를 실행하시겠습니까? (y/n)"
has "A5 n: 직접 채울 변수 안내" "$T/a.out" "직접 채울 변수와 위치"
has "A5 n: 위치 안내(config_check.sh)" "$T/a.out" "$PA/config_check.sh"
has "A5 n: auto_done_dir 안내" "$T/a.out" "auto_done_dir"
hasnt "A5 n: 역할 질문 없음" "$T/a.out" "역할을 선택"
same "A5 n: 파일 불변" "$T/a.orig" "$CC"
nobak "A5 n: 백업 없음" "$CC"
runa "y${NL}1${NL}2${NL}h1${NL}root@d1${NL}os8mgmt01${NL}y${NL}"
eq "A5 y: 종료코드 0" 0 "$ARC"
eq "A5 y: auto_done_host(택일 선택) 적용" os8mgmt01 "$(val "$CC" auto_done_host)"
eq "A5 y: os6_host 적용" h1 "$(val "$CC" os6_host)"
eq "A5 y: dhcp_server 적용" root@d1 "$(val "$CC" dhcp_server)"
eq "A5 y: auto_done_dir 빈 값(택일)" "" "$(val "$CC" auto_done_dir)"
has "A5 y: 설명(요구사항 2-1): gossh 로 대신 기록" "$T/a.out" 'gossh 로 이 서버(데몬이 있는 서버)에 접속해 대신 기록한다'
has "A5 y: 설명(요구사항 2-1): 다른 서버에서 실행" "$T/a.out" '다른 서버(os6_mgmt 등)에서 실행되어 데몬 서버로 기록을 보내야 할 때'
has "A5 y: 설명(요구사항 2-1): autofs 공유" "$T/a.out" 'autofs 로 os8_mgmt·os6_mgmt 가 한 스크립트를 공유하는 경우에도 이 변수만 채운다'
has "A5 y: 설명: gossh -p 필요" "$T/a.out" 'auto_setup_gossh_pw (gossh -p)'

# ================================================================ B. 업데이트: 현장 사본(OLD + 손으로 고친 흔적)에 update_v<ver>.sh
mk_field "$T/field.orig"
PB=$T/projB
isok "B0 현장 사본 bash -n" bash -n "$T/field.orig"
mk=0; for m in "${FIELD_MARKS[@]}"; do grep -qxF -- "$m" "$T/field.orig" || { fail "B0 현장 수정 흔적이 사본에 만들어져 있어야 함" "$m"; mk=1; }; done
[ $mk = 0 ] && pass
has "B0 user= 줄이 현장 방식(백틱+절대경로)" "$T/field.orig" "$FIELD_USERLINE"
# 현장 사본이 수동 모드에서 실제로 동작 (user= 줄이 실행됨)
rm -f "$W/a_sh_ran"
run_manual "$T/field.orig" "$T/b0.out" "1${NL}y${NL}n${NL}"; eq "B0 현장 사본 수동 모드 정상" 0 "$?"
if [ -e "$W/a_sh_ran" ]; then pass; else fail "B0 수동 모드에서 현장 user= 줄이 실행되어야 함"; fi

updrun() {   # updrun <프로젝트> <stdin> <옵션...> : setup/ 안에서 update 실행 → $T/u.out, UR
    printf '%s' "$2" | (cd "$1/setup" && bash "update_v${VER}.sh" "${@:3}") > "$T/u.out" 2>&1
    UR=$?
}
# B1. --dry : 아무것도 바꾸지 않는다
mk_proj "$PB" "$T/field.orig"
updrun "$PB" "" --dry --yes
eq "B1 --dry 종료코드 0" 0 "$UR"
same "B1 --dry: 파일 불변" "$T/field.orig" "$PB/config_check.sh"
nobak "B1 --dry: 백업 없음" "$PB/config_check.sh"
has "B1 --dry: 바뀔 내용 표시" "$T/u.out" "바뀔 내용 (--dry)"
has "B1 --dry: 새 변수 예고" "$T/u.out" "적용 후 질문할 새 변수: auto_done_dir auto_done_host"
has "B1 --dry: 주석·코멘트 변경 0건" "$T/u.out" "주석·코멘트 문구 변경 0건"

# B2. 실제 적용: 새 변수(auto_done_*)만 질문 — 택일에서 2(auto_done_host) 선택
updrun "$PB" "2${NL}os8mgmt01${NL}" --yes
CB=$PB/config_check.sh
eq "B2 종료코드 0" 0 "$UR"
isok "B2 결과 bash -n" bash -n "$CB"
nocr "B2 결과 LF" "$CB"
eq "B2 원본 줄이 하나도 사라지거나 바뀌지 않음 (현장 수정 전부 바이트 보존)" 0 "$(nrm "$T/field.orig" "$CB")"
mk=0; for m in "${FIELD_MARKS[@]}"; do grep -qxF -- "$m" "$CB" || { fail "B2 현장 수정 흔적이 보존되어야 함" "$m"; mk=1; }; done
[ $mk = 0 ] && pass
has "B2 현장 user= 줄(백틱·절대경로) 그대로" "$CB" "$FIELD_USERLINE"
eq "B2 auto_done_host 값 적용" os8mgmt01 "$(val "$CB" auto_done_host)"
eq "B2 auto_done_dir 는 택일에서 빠져 빈 값" "" "$(val "$CB" auto_done_dir)"
eq "B2 기존 값 유지: os6_host" os6mgmt01 "$(val "$CB" os6_host)"
eq "B2 기존 값 유지: dhcp_server" root@10.1.1.5 "$(val "$CB" dhcp_server)"
eq "B2 기존 값 유지: uptime_enable_user" u1 "$(val "$CB" uptime_enable_user)"
eq "B2 추가된 줄 = 삽입분(8건) + 값 적용 줄" "$(( $(wc -l < "$NEW") - $(wc -l < "$OLD") ))" "$(diff "$T/field.orig" "$CB" | grep -c '^>')"
has "B2 검증 요약: 주석·코멘트 문구 변경 0건" "$T/u.out" "주석·코멘트 문구 변경 0건"
has "B2 검증 요약: 변경 적용 8건" "$T/u.out" "변경 적용 8건"
has "B2 검증 요약: 삭제·변경된 원본 코드 줄 0줄" "$T/u.out" "삭제·변경된 원본 코드 줄 0줄"
has "B2 문법 검사 통과 표시" "$T/u.out" "문법 검사(bash -n) 통과"
has "B2 새 변수만 묻는다는 안내" "$T/u.out" "이번 업데이트로 추가된 변수만 묻습니다: auto_done_dir auto_done_host"
eq "B2 값 질문은 1번뿐 (auto_done_host)" 1 "$(grep -c '값 입력 (Enter=현재값 유지)' "$T/u.out")"
hasnt "B2 기존 변수 os6_host 는 질문하지 않음" "$T/u.out" "os6_host  ("
hasnt "B2 기존 변수 dhcp_server 는 질문하지 않음" "$T/u.out" "dhcp_server  ("
has "B2 택일 상황 질문" "$T/u.out" "이 스크립트는 어느 서버에서 실행됩니까?"
has "B2 --undo 안내" "$T/u.out" "--undo"
has "B2 Disclaimer" "$T/u.out" "랜덤 서버 몇 대에서 실제 변경을 확인하십시오"
if [ -f "$PB/.update_journal" ]; then pass; else fail "B2 저널 생성"; fi
bk=0; for b in "$PB"/config_check.sh.bak.*; do cmp -s "$b" "$T/field.orig" && bk=1; done
eq "B2 백업 중 하나는 현장 원본과 바이트 동일" 1 "$bk"
cp "$CB" "$T/b2.result"

# B3. 업데이트된 현장 사본에서 -auto : user= 줄(현장 방식)이 실행되지 않고, 브랜치 버전과 같은 동작
rm -f "$W/a_sh_ran"; rm -rf "$W/asdir"/*
run_auto "$CB" "$T/b3.out"; eq "B3 -auto 종료코드 0" 0 "$?"
if [ -e "$W/a_sh_ran" ]; then fail "B3 -auto 에서 현장 user= 줄이 실행되면 안 됨"; else pass; fi
has "B3 -auto 질문 자동 응답(작업)" "$T/b3.out" "작업을 진행하시겠습니까? (y/n): y (-auto)"
has "B3 -auto 질문 자동 응답(환경설정=set)" "$T/b3.out" "환경설정을 수정하시겠습니까? (y/n/set): set (-auto)"
has "B3 결과 리포트 시작 마커" "$T/b3.out" "############### 결과 리포트 ###############"
eq "B3 결과 리포트 끝 마커(줄 단독, 그 뒤에 완료기록 INFO)" 1 "$(grep -cx "###########################################" "$T/b3.out")"
if [ -s "$W/run/check.res_u1_postapply" ] && grep -q '^pa01:' "$W/run/check.res_u1_postapply"; then pass; else fail "B3 postapply 생성"; fi
if [ -f "$W/asdir/done/pa01" ] && [ -f "$W/asdir/done/pa02" ] && [ -f "$W/asdir/done/fail01" ] && [ ! -e "$W/asdir/done/err01" ] && [ ! -e "$W/asdir/done/down01" ]; then pass; else fail "B3 auto_done_host 로 원격 완료기록(정상 3대만)"; fi
has "B3 완료기록 INFO" "$T/b3.out" "auto_setup 완료기록 : 3대 → os8mgmt01"
has "B3 현장이 채운 uptime_enable_user 값이 그대로 동작(uptime 실행)" "$GOSSH_LOG" "uptime"
has "B3 현장 경로(check_script)가 gossh 명령에 반영" "$GOSSH_LOG" "bash /opt/company/SA/os/run.sh pd"
# B3b. 같은 입력으로 브랜치 버전(-auto)을 돌린 출력과 NEW 업데이트본의 출력 비교용 — 현장 수정 외 동일해야 하므로 NEW 단독 비교는 E2 에서 수행

# B4. 수동 모드: 업데이트 전·후 현장 사본의 동작이 같다 (같은 입력 → 같은 출력·산출물·gossh 호출)
for who in before after; do
    if [ $who = before ]; then src=$T/field.orig; else src=$CB; fi
    rm -f "$W/a_sh_ran"
    run_manual "$src" "$T/b4_$who.out" "1${NL}y${NL}set${NL}"; eval "b4rc_$who=$?"
    normo "$T/b4_$who.out" > "$T/b4_$who.norm"
    ls "$W/run" | LC_ALL=C sort > "$T/b4_$who.ls"
    cp "$GOSSH_LOG" "$T/b4_$who.glog"
    if [ -e "$W/a_sh_ran" ]; then eval "b4ran_$who=1"; else eval "b4ran_$who=0"; fi
done
eq "B4 수동 모드 종료코드 같음" "$b4rc_before" "$b4rc_after"
same "B4 수동 모드 화면 출력 같음(업데이트 전/후)" "$T/b4_before.norm" "$T/b4_after.norm"
same "B4 수동 모드 생성 파일 목록 같음" "$T/b4_before.ls" "$T/b4_after.ls"
same "B4 수동 모드 gossh 호출 같음" "$T/b4_before.glog" "$T/b4_after.glog"
eq "B4 수동 모드에서는 현장 user= 줄이 실행됨(전/후 모두)" "1 1" "$b4ran_before $b4ran_after"
hasnt "B4 수동 모드에서는 완료기록·마커 없음" "$T/b4_after.out" "결과 리포트"

# B5. --undo : 업데이트 전 상태로 바이트 단위 복원
updrun "$PB" "" --undo
eq "B5 --undo 종료코드 0" 0 "$UR"
same "B5 --undo: 현장 원본으로 바이트 동일 복원" "$T/field.orig" "$CB"
has "B5 되돌리기 완료 표시" "$T/u.out" "되돌리기 완료"
updrun "$PB" "" --undo
eq "B5 --undo 두 번째: 이미 되돌림 안내, 종료코드 0" 0 "$UR"
same "B5 --undo 두 번째에도 원본 그대로" "$T/field.orig" "$CB"

# B6. 택일에서 auto_done_dir 선택 (존재하는 디렉터리) → 로컬 완료기록
mk_proj "$PB" "$T/field.orig"
updrun "$PB" "1${NL}${DONE}${NL}" --yes
eq "B6 종료코드 0" 0 "$UR"
eq "B6 auto_done_dir 적용" "$DONE" "$(val "$CB" auto_done_dir)"
eq "B6 auto_done_host 는 빈 값" "" "$(val "$CB" auto_done_host)"
eq "B6 원본 줄 보존" 0 "$(nrm "$T/field.orig" "$CB")"
rm -rf "$DONE"/*; rm -rf "$W/asdir"/*
run_auto "$CB" "$T/b6.out"; eq "B6 -auto 종료코드 0" 0 "$?"
if [ -f "$DONE/pa01" ] && [ -f "$DONE/pa02" ] && [ -f "$DONE/fail01" ] && [ ! -e "$DONE/err01" ] && [ ! -e "$DONE/down01" ]; then pass; else fail "B6 auto_done_dir 로 로컬 완료기록(정상 3대만)"; fi
if ls "$W/asdir"/done/* > /dev/null 2>&1; then fail "B6 auto_done_dir 이면 원격(gossh) 기록은 하지 않음"; else pass; fi
has "B6 완료기록 INFO(로컬)" "$T/b6.out" "auto_setup 완료기록 : 3대 → $DONE"
eq "B6 기록 형식 'epoch user sha256 config_check'" 1 "$(grep -Ec '^[0-9]+ u1 [0-9a-f]{64} config_check$' "$DONE/pa01")"

# B7. 택일에서 0(사용 안 함): 둘 다 빈 값 → 완료기록 없음(출력·파일 없음)
mk_proj "$PB" "$T/field.orig"
updrun "$PB" "0${NL}" --yes
eq "B7 종료코드 0" 0 "$UR"
eq "B7 auto_done_* 둘 다 빈 값" ",," "$(val "$CB" auto_done_dir),$(val "$CB" auto_done_host),"
eq "B7 원본 줄 보존" 0 "$(nrm "$T/field.orig" "$CB")"
rm -rf "$W/asdir"/*
run_auto "$CB" "$T/b7.out"; eq "B7 -auto 종료코드 0" 0 "$?"
hasnt "B7 완료기록 출력 없음" "$T/b7.out" "완료기록"
if ls "$W/asdir"/done/* > /dev/null 2>&1; then fail "B7 완료기록 파일이 생기면 안 됨"; else pass; fi

# B8. user= 줄만 다른 사본 (사용자가 실제로 겪은 형태)
sed -e "s|^user=\$(bash \"\$user_info_output\".*|$FIELD_USERLINE|" "$OLD" > "$T/field.userline"
has "B8 사본: user= 줄 변경" "$T/field.userline" "$FIELD_USERLINE"
mk_proj "$PB" "$T/field.userline"
updrun "$PB" "2${NL}os8mgmt01${NL}" --yes
eq "B8 user= 줄만 다른 사본에 업데이트 성공" 0 "$UR"
eq "B8 원본 줄 보존" 0 "$(nrm "$T/field.userline" "$CB")"
has "B8 user= 줄 보존" "$CB" "$FIELD_USERLINE"
isok "B8 bash -n" bash -n "$CB"
# B8b. 같은 사본(원본 OLD, 현장 수정 없음)
mk_proj "$PB" "$OLD"
updrun "$PB" "2${NL}os8mgmt01${NL}" --yes
eq "B8b 현장 수정 없는 OLD 사본에도 성공" 0 "$UR"
sed 's|^auto_done_host=""|auto_done_host="os8mgmt01"|' "$NEW" > "$T/b8_expected.sh"
same "B8b OLD 에 업데이트 = NEW 에 auto_done_host 만 채운 것과 바이트 동일" "$T/b8_expected.sh" "$CB"

# ================================================================ C. 이상 상황
PC=$T/projC
aborted() {   # aborted <라벨> <사본 파일(적용 전 바이트)> : 업데이트가 중단되고 파일이 바이트 동일, 백업·저널 없음
    local lbl=$1 orig=$2
    if [ "$UR" -ne 0 ]; then pass; else fail "$lbl 종료코드가 0 이 아니어야 함" "rc=$UR"; fi
    same "$lbl 대상 파일 바이트 동일" "$orig" "$PC/config_check.sh"
    nobak "$lbl 백업 없음" "$PC/config_check.sh"
    absent "$lbl 저널 없음" "$PC/.update_journal"
    has "$lbl 중단 안내" "$T/u.out" "중단 — 대상 파일은 변경되지 않았습니다"
}
# C1. 앵커 줄을 현장에서 고친 사본 → 중단 + 어느 앵커가 왜 안 맞는지 표시, 파일 불변
anc_case() {   # anc_case <변경 id> <sed 식> <설명>
    local id=$1 expr=$2 why=$3
    sed -e "$expr" "$T/field.orig" > "$T/c1.$id"
    if cmp -s "$T/c1.$id" "$T/field.orig"; then fail "C1[$id] 사본 수정이 적용되지 않음(테스트 오류)" "$why"; return; fi
    mk_proj "$PC" "$T/c1.$id"
    updrun "$PC" "2${NL}os8mgmt01${NL}" --yes
    aborted "C1[$id:$why]" "$T/c1.$id"
    has "C1[$id] 어느 변경의 앵커인지 표시" "$T/u.out" "[$id] 앵커 일치 0곳"
    has "C1[$id] 일치 줄 없음 안내" "$T/u.out" "일치 줄 없음 — 현장 사본에서 이 줄이 수정·삭제되었을 수 있습니다"
    has "C1[$id] 찾던 앵커 정규식 표시" "$T/u.out" "앵커: "
}
anc_case c2 's|^bash "\$user_info_mn"|bash /asdf/menu.sh|' "user 선택 첫 줄(bash \$user_info_mn)을 현장에서 고침"
anc_case c3 's|대상 목록 파일이 없습니다|대상 목록 없음|' "목록 파일 확인 줄의 메시지를 고침"
anc_case c5 's/awk .NF && !seen\[\$0\]++. > /awk NF > /' "호스트 목록 읽는 줄을 고침"
anc_case c6 's|do_check "\$TMP/ok.txt" recheck|do_check "$TMP/ok.txt" recheck2|' "재체크 호출 줄을 고침"
# 앵커 줄이 2곳이면 (exit 0 이 둘)
sed -e 's|^exit 0$|exit 0\nexit 0|' "$T/field.orig" > "$T/c1.dup"
mk_proj "$PC" "$T/c1.dup"; updrun "$PC" "" --yes
aborted "C1[dup] exit 0 이 2곳" "$T/c1.dup"
has "C1[dup] 일치 2곳 표시" "$T/u.out" "[c8] 앵커 일치 2곳"
has "C1[dup] 일치 줄 번호 표시" "$T/u.out" "일치 줄 번호(원본 기준"
# 앵커 줄의 코멘트·공백·값만 바뀐 경우는 허용 (현장 수정 허용 범위)
sed -e 's|^exit 0$|exit 0   # 현장 메모|' -e 's|^HOSTS="\$TMP/hosts"|HOSTS="$TMP/hosts"   # 현장 메모|' -e 's|^dhcp_server="root@10.1.1.5"|dhcp_server="root@10.9.9.9"|' -e 's|^esac$|esac   # 현장 메모|' "$T/field.orig" > "$T/c1.ok"
mk_proj "$PC" "$T/c1.ok"; updrun "$PC" "2${NL}os8mgmt01${NL}" --yes
eq "C1[허용] 앵커 줄의 코멘트·값 수정은 허용 → 성공" 0 "$UR"
eq "C1[허용] 원본 줄 보존" 0 "$(nrm "$T/c1.ok" "$PC/config_check.sh")"
eq "C1[허용] 현장 값 보존(dhcp_server)" root@10.9.9.9 "$(val "$PC/config_check.sh" dhcp_server)"

# C2. 이미 -auto 가 들어간 사본(브랜치 버전/예전 패치 적용본): 중복 적용으로 꼬이지 않고 중단
sed -e "s|^user=\$(bash \"\$user_info_output\".*|$FIELD_USERLINE|" "$REF/config_check.branch.sh" > "$T/c2.branch"
mk_proj "$PC" "$T/c2.branch"
updrun "$PC" "" --yes
aborted "C2 이미 -auto 적용된 사본" "$T/c2.branch"
has "C2 어느 변경이 안 맞는지 표시" "$T/u.out" "앵커 일치 0곳"

# C3. CRLF 현장 사본: 결과는 LF, 현장 수정 보존, --undo 는 CRLF 원본을 바이트 단위로 복원
sed 's/$/\r/' "$T/field.orig" > "$T/field.crlf"
mk_proj "$PC" "$T/field.crlf"
updrun "$PC" "2${NL}os8mgmt01${NL}" --yes
eq "C3 CRLF 사본 업데이트 성공" 0 "$UR"
nocr "C3 결과는 LF" "$PC/config_check.sh"
eq "C3 원본(CR 제거 기준) 줄이 하나도 사라지거나 바뀌지 않음" 0 "$(nrm <(tr -d '\r' < "$T/field.crlf") "$PC/config_check.sh")"
has "C3 CR 제거 안내" "$T/u.out" "CR 제거"
isok "C3 결과 bash -n" bash -n "$PC/config_check.sh"
mk=0; for m in "${FIELD_MARKS[@]}"; do grep -qxF -- "$m" "$PC/config_check.sh" || mk=1; done
eq "C3 현장 수정 흔적 보존" 0 "$mk"
updrun "$PC" "" --undo
eq "C3 --undo 종료코드 0" 0 "$UR"
same "C3 --undo: CRLF 원본을 바이트 단위로 복원" "$T/field.crlf" "$PC/config_check.sh"
# vim 으로 \r 을 지운 사본(LF)은 B 에서 이미 검증 — 여기서는 CR 이 일부 줄에만 섞인 사본
awk 'NR % 7 == 0 { printf "%s\r\n", $0; next } { print }' "$T/field.orig" > "$T/field.mixed"
mk_proj "$PC" "$T/field.mixed"
updrun "$PC" "2${NL}os8mgmt01${NL}" --yes
eq "C3 CR 이 일부만 섞인 사본도 성공" 0 "$UR"
nocr "C3 혼합 사본 결과 LF" "$PC/config_check.sh"
eq "C3 혼합 사본 원본 줄 보존" 0 "$(nrm <(tr -d '\r' < "$T/field.mixed") "$PC/config_check.sh")"

# C4. 잘린 붙여넣기
# C4a. update 스크립트가 잘림 → 실행 거부, 대상 불변
mk_proj "$PC" "$T/field.orig"
head -n 1500 "$UPD" > "$PC/setup/update_v${VER}.sh"
updrun "$PC" "" --yes
eq "C4a 잘린 update 스크립트는 실행 거부 → 2" 2 "$UR"
has "C4a 잘림 안내" "$T/u.out" "잘렸거나 변형"
same "C4a 대상 파일 불변" "$T/field.orig" "$PC/config_check.sh"
nobak "C4a 백업 없음" "$PC/config_check.sh"
# 끝줄 마커만 지워진 경우
mk_proj "$PC" "$T/field.orig"
sed '$d' "$UPD" > "$PC/setup/update_v${VER}.sh"
updrun "$PC" "" --yes
eq "C4a' 끝줄 마커가 없는 스크립트 → 2" 2 "$UR"
same "C4a' 대상 파일 불변" "$T/field.orig" "$PC/config_check.sh"
# C4b. 현장 사본이 잘려 붙여넣어짐 (앞 250줄만) → 뒤쪽 앵커를 못 찾고 중단
head -n 250 "$T/field.orig" > "$T/field.trunc"
mk_proj "$PC" "$T/field.trunc"
updrun "$PC" "" --yes
aborted "C4b 잘린 현장 사본" "$T/field.trunc"
has "C4b 못 찾은 앵커 표시" "$T/u.out" "[c6] 앵커 일치 0곳"
# C4c. 가이드가 잘림
head -n 600 "$GUIDE" > "$PC/setup/setup_guide.sh"
printf '' | (cd "$PC/setup" && bash setup_guide.sh) > "$T/g.out" 2>&1; eq "C4c 잘린 가이드는 실행 거부 → 2" 2 "$?"
has "C4c 잘림 안내" "$T/g.out" "잘렸거나 변형"

# C5. 재실행(멱등)
mk_proj "$PC" "$T/field.orig"
updrun "$PC" "2${NL}os8mgmt01${NL}" --yes
eq "C5 첫 실행 성공" 0 "$UR"
cp "$PC/config_check.sh" "$T/c5.after1"; cp "$PC/.update_journal" "$T/c5.journal1"
nb1=$(ls "$PC"/config_check.sh.bak.* | wc -l)
updrun "$PC" "" --yes
eq "C5 재실행(stdin 없음, --yes) 종료코드 0" 0 "$UR"
same "C5 재실행해도 파일 바이트 동일" "$T/c5.after1" "$PC/config_check.sh"
has "C5 재실행: 변경 없음 안내" "$T/u.out" "변경 없음"
eq "C5 재실행: 새 백업이 생기지 않음" "$nb1" "$(ls "$PC"/config_check.sh.bak.* | wc -l)"
same "C5 재실행: 저널(되돌리기 정보)이 그대로" "$T/c5.journal1" "$PC/.update_journal"
updrun "$PC" "" --undo
same "C5 재실행 뒤에도 --undo 는 현장 원본으로 복원" "$T/field.orig" "$PC/config_check.sh"
# 이미 적용된 NEW 자체(빈 새 변수)에 적용: 8건 모두 '이미 적용됨', 새 변수만 다시 묻는다
mk_proj "$PC" "$NEW"
updrun "$PC" "0${NL}" --yes
has "C5 NEW 에 적용: 이미 적용됨 표시" "$T/u.out" "이미 적용됨"
same "C5 NEW 에 적용해도(택일 0) 바이트 동일" "$NEW" "$PC/config_check.sh"

# ================================================================ D. 비교: 예전 diff 패치(apply_auto_mode.sh) 방식
DD=$T/dpatch; mkdir -p "$DD"
cp "$REF/apply_auto_mode.sh" "$REF/auto_mode.patch" "$DD/"
patch_try() {   # patch_try <사본> -> DRC, $T/d.out, 대상은 $DD/t.sh
    cp "$1" "$DD/t.sh"
    (cd "$DD" && bash apply_auto_mode.sh t.sh) > "$T/d.out" 2>&1; DRC=$?
}
# D1. 대조군: 현장 수정이 없는 OLD 사본에는 패치가 들어간다
patch_try "$OLD"
eq "D1 대조군: 수정 없는 OLD 사본에는 diff 패치가 적용됨" 0 "$DRC"
has "D1 적용 완료" "$T/d.out" "적용 완료"
# D2. user= 줄만 다른 사본 → 패치 실패
patch_try "$T/field.userline"
if [ "$DRC" -ne 0 ]; then pass; else fail "D2 user= 줄이 다른 사본에 diff 패치는 실패해야 함"; fi
has "D2 패치 적용 실패 메시지" "$T/d.out" "패치 적용 실패"
same "D2 대상은 변경되지 않음" "$T/field.userline" "$DD/t.sh"
# D3. 전체 현장 사본 → 패치 실패
patch_try "$T/field.orig"
if [ "$DRC" -ne 0 ]; then pass; else fail "D3 현장 사본에 diff 패치는 실패해야 함"; fi
same "D3 대상은 변경되지 않음" "$T/field.orig" "$DD/t.sh"
# D4. 같은 두 사본에 새 업데이트(update_v${VER}.sh)는 성공 (B2·B8 에서 검증한 것의 재확인)
mk_proj "$PC" "$T/field.userline"; updrun "$PC" "2${NL}os8mgmt01${NL}" --yes
eq "D4 같은 user= 사본에 새 업데이트는 성공" 0 "$UR"
mk_proj "$PC" "$T/field.orig"; updrun "$PC" "2${NL}os8mgmt01${NL}" --yes
eq "D4 같은 현장 사본에 새 업데이트는 성공" 0 "$UR"
eq "D4 현장 수정 보존(원본 줄 0줄 변경)" 0 "$(nrm "$T/field.orig" "$PC/config_check.sh")"

printf 'PASS=%d FAIL=%d
' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
