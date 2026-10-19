#!/bin/bash
# run_setup_scripts.sh - conf/setup_guide.sh · conf/update_v*.sh 스텁 검증 (실제 경로·서버 없이 임시 사본에서)
#   사용: bash test/run_setup_scripts.sh [update 스크립트 (기본: conf/ 의 가장 최신 update_v*.sh)]
#   실제 setup.sh(빌드·설치·cron)는 실행하지 않는다 — 임시 사본의 setup.sh 를 스텁으로 바꿔 이어서 실행만 확인.
set -u
cd "$(dirname "$0")/.."
ROOT=$(pwd)
UPD=${1:-conf/update_v0.4.0.sh}        # 4~6절: v0.4.0 업데이트 (기존 케이스 유지)
UPD6=${2:-conf/update_v0.6.0.sh}       # 7절: v0.6.0 업데이트 (awx_profile_1~9 주석·빈 변수 추가)
UPD8=${3:-conf/update_v0.8.0.sh}       # 8절: v0.8.0 업데이트 (newhost_template_1~9·work_os6_over 추가)
UPD7=${4:-conf/update_v0.7.0.sh}       # 8절 순차 적용: 0.6.0 → 0.7.0 → 0.8.0
export NO_COLOR=1
PASS=0 FAIL=0
ok()   { PASS=$((PASS + 1)); echo "  [O] $*"; }
ng()   { FAIL=$((FAIL + 1)); echo "  [X] $*"; }
chk()  { if eval "$2"; then ok "$1"; else ng "$1"; fi; }
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

newproj() {   # newproj <dir>: conf/ + 스텁 setup.sh 만 있는 사본
	mkdir -p "$1/conf"
	cp conf/setup_guide.sh conf/vars.manifest conf/auto_setup.conf.example "$1/conf/"
	printf '#!/bin/bash\necho SETUP_STUB_RAN\n' > "$1/setup.sh"
}
val() { grep "^$2=" "$1/conf/auto_setup.conf" | head -1 | cut -d= -f2-; }

echo "== 1. 신규: os8 역할 가이드 (빈 conf → 값 입력 → setup.sh 이어서 실행; 질문 순서: 독립 변수 → 의존 변수) =="
P=$TMP/p1; newproj "$P"
mkdir -p "$TMP/awx" "$TMP/share"; : > "$TMP/check.sh"
cp "$P/conf/auto_setup.conf.example" "$P/conf/auto_setup.conf"
# 빈 입력 23개 = enable_*/engine 4 + newhost_template 9 + work_os6_over 1 (v0.8.0) + awx_profile 9
printf '%s\n' os6mgmt01 "$TMP/check.sh" "$TMP/awx" "$TMP/share" '' '' '' '' '' '' '' '' '' '' '' '' '' '' '' '' '' '' '' '' '' '' '' /user/gossh /user/as - \
	| bash "$P/conf/setup_guide.sh" --role os8 --yes > "$TMP/o1" 2>&1
rc=$?
chk "종료코드 0" "[ $rc -eq 0 ]"
chk "os6_mgmt 기록" "[ \"\$(val $P os6_mgmt)\" = os6mgmt01 ]"
chk "os_check_sh 기록" "[ \"\$(val $P os_check_sh)\" = '$TMP/check.sh' ]"
chk "os6_os_check_sh '-' 기록" "[ \"\$(val $P os6_os_check_sh)\" = - ]"
chk "os8_mgmt 는 묻지 않고 빈 값" "[ -z \"\$(val $P os8_mgmt)\" ]"
chk "주석 줄 수 불변" "[ \$(grep -c '^#' $P/conf/auto_setup.conf) -eq \$(grep -c '^#' $P/conf/auto_setup.conf.example) ]"
chk "setup.sh 이어서 실행" "grep -q SETUP_STUB_RAN $TMP/o1"
chk "Disclaimer 출력" "grep -q '랜덤 서버' $TMP/o1"

echo "== 2. 신규: os6 역할 (os8_mgmt 만, setup.sh 건너뜀) =="
P=$TMP/p2; newproj "$P"
cp "$P/conf/auto_setup.conf.example" "$P/conf/auto_setup.conf"
printf '%s\n' os8mgmt01 '' | bash "$P/conf/setup_guide.sh" --role os6 --no-setup --yes > "$TMP/o2" 2>&1
rc=$?
chk "종료코드 0" "[ $rc -eq 0 ]"
chk "os8_mgmt 기록" "[ \"\$(val $P os8_mgmt)\" = os8mgmt01 ]"
chk "os8 역할 변수는 빈 값" "[ -z \"\$(val $P os6_mgmt)\" ] && [ -z \"\$(val $P os_check_sh)\" ]"
chk "setup.sh 실행 안 함" "! grep -q SETUP_STUB_RAN $TMP/o2"

echo "== 3. 신규: 잘못된 경로 → 건너뛰기(s) 후 나머지 진행 =="
P=$TMP/p3; newproj "$P"
cp "$P/conf/auto_setup.conf.example" "$P/conf/auto_setup.conf"
printf '%s\n' '' /no/such/check.sh s '' '' \
	| bash "$P/conf/setup_guide.sh" --role os8 --no-setup --yes > "$TMP/o3" 2>&1
chk "없는 파일 이유 표시" "grep -q '/no/such/check.sh' $TMP/o3"
chk "건너뛴 변수는 빈 값 유지" "[ -z \"\$(val $P os_check_sh)\" ]"

echo "== 4. 업데이트: 현장 사본(값 채움·주석 수정·CRLF) → 새 줄만 삽입, 값·주석 보존 =="
[ -n "$UPD" ] && [ -f "$UPD" ] || { ng "업데이트 스크립트 없음"; echo "PASS=$PASS FAIL=$FAIL"; exit 1; }
P=$TMP/p4; newproj "$P"; cp "$UPD" "$P/conf/"
U=$P/conf/$(basename "$UPD")
F=$P/conf/auto_setup.conf
# 0.3.0 예시(삽입 줄 없음)에 현장 값·주석 수정
grep -v '^# (v0.4.0)' conf/auto_setup.conf.example \
	| sed -e 's/^os6_mgmt=$/os6_mgmt=OO-mgmt/' -e 's|^os_check_sh=$|os_check_sh=/OO/check.sh|' \
	      -e 's/^# awx 스크립트 경로.*/# awx 경로 (현장 메모: 담당자 OO)/' \
	| sed 's/$/\r/' > "$F"
printf 'os6_gossh=/OO/gossh\r\n' >> "$F"   # 현장에서 아래쪽에 직접 덧붙인 줄
before_vals=$(tr -d '\r' < "$F" | grep -v '^#' | grep '=')
bash "$U" --dir "$P" --yes < /dev/null > "$TMP/o4" 2>&1
rc=$?
chk "종료코드 0" "[ $rc -eq 0 ]"
chk "새 주석 줄 삽입(os6_mgmt 바로 위)" "grep -B1 '^os6_mgmt=' $F | grep -q '^# (v0.4.0)'"
chk "값 줄 전부 보존" "[ \"\$(grep -v '^#' $F | grep '=')\" = \"\$before_vals\" ]"
chk "현장 주석 보존" "grep -q '현장 메모: 담당자 OO' $F"
chk "CR 제거(LF)" "! grep -q \$'\\r' $F"
chk "백업 생성" "ls $F.bak.* >/dev/null 2>&1"
bash "$U" --dir "$P" --yes < /dev/null > "$TMP/o4b" 2>&1
chk "재실행은 변경 없음(멱등)" "[ \$(grep -c '^# (v0.4.0)' $F) -eq 1 ]"

echo "== 5. --undo: 업데이트 전(LF 정규화 후) 바이트 동일 복원 =="
P=$TMP/p5; newproj "$P"; cp "$UPD" "$P/conf/"
U=$P/conf/$(basename "$UPD"); F=$P/conf/auto_setup.conf
grep -v '^# (v0.4.0)' conf/auto_setup.conf.example | sed 's/^os6_mgmt=$/os6_mgmt=OO/' > "$F"
cp "$F" "$TMP/orig5"
bash "$U" --dir "$P" --yes < /dev/null > /dev/null 2>&1
bash "$U" --dir "$P" --undo --yes < /dev/null > "$TMP/o5" 2>&1
chk "undo 바이트 동일" "cmp -s $F $TMP/orig5"

echo "== 6. 앵커 없음(os6_mgmt 줄 삭제된 사본) → 중단, 파일 불변 =="
P=$TMP/p6; newproj "$P"; cp "$UPD" "$P/conf/"
U=$P/conf/$(basename "$UPD"); F=$P/conf/auto_setup.conf
grep -v -e '^# (v0.4.0)' -e '^os6_mgmt=' conf/auto_setup.conf.example > "$F"
cp "$F" "$TMP/orig6"
bash "$U" --dir "$P" --yes < /dev/null > "$TMP/o6" 2>&1
rc=$?
chk "종료코드 0 아님" "[ $rc -ne 0 ]"
chk "파일 불변" "cmp -s $F $TMP/orig6"
chk "앵커 위치 안내" "grep -q 'os6_mgmt' $TMP/o6"

echo "== 7. v0.6.0 업데이트: v0.4.0 시기 conf(현장 값·CRLF) → awx_profile 블록만 삽입, 값 보존 =="
[ -f "$UPD6" ] || { ng "v0.6.0 업데이트 스크립트 없음: $UPD6"; echo "PASS=$PASS FAIL=$FAIL"; exit 1; }
P=$TMP/p7; newproj "$P"; cp "$UPD6" "$P/conf/"
U=$P/conf/$(basename "$UPD6"); F=$P/conf/auto_setup.conf
sed '/^# (v0.6.0)/,/^awx_profile_9=$/d' conf/auto_setup.conf.example \
	| sed -e 's/^os6_mgmt=$/os6_mgmt=OO-mgmt/' -e 's|^awx_dir=$|awx_dir=/OO/awx|' \
	      -e 's/^# 비어 있지 않으면 원격.*/# 원격 클라이언트 (현장 메모: 담당자 OO)/' \
	| sed 's/$/\r/' > "$F"
before_vals=$(tr -d '\r' < "$F" | grep -v '^#' | grep '=')
printf '\n\n\n\n\n\n\n\n\n' | bash "$U" --dir "$P" --yes > "$TMP/o7" 2>&1   # 새 변수 awx_profile_1~9 는 빈 값(Enter)
rc=$?
chk "종료코드 0" "[ $rc -eq 0 ]"
chk "awx_profile_1~9 빈 변수 9개 삽입" "[ \$(grep -c '^awx_profile_[1-9]=\$' $F) -eq 9 ]"
chk "설명 주석(v0.6.0) 삽입" "grep -q '^# (v0.6.0) AWX 실행 프로파일' $F"
chk "기존 값 줄 전부 보존" "[ \"\$(grep -v '^#' $F | grep '=' | grep -v '^awx_profile_')\" = \"\$before_vals\" ]"
chk "현장 주석 보존" "grep -q '현장 메모: 담당자 OO' $F"
printf '\n\n\n\n\n\n\n\n\n' | bash "$U" --dir "$P" --yes > "$TMP/o7b" 2>&1
chk "재실행은 변경 없음(멱등)" "[ \$(grep -c '^awx_profile_1=' $F) -eq 1 ]"

echo "== 8. v0.8.0 업데이트: v0.7.0 시기 conf(현장 값·CRLF) → newhost_template_1~9·work_os6_over 빈 변수만 추가, 값 보존 =="
[ -f "$UPD8" ] || { ng "v0.8.0 업데이트 스크립트 없음: $UPD8"; echo "PASS=$PASS FAIL=$FAIL"; exit 1; }
V8DEL='/^# (v0.8.0) 신규/,/^work_os6_over=$/d'      # 예시에서 v0.8.0 블록만 지움 = v0.7.0 시기 conf
TEN='\n\n\n\n\n\n\n\n\n\n'                            # 새 변수 10개에 줄바꿈 10번 (Enter = 빈 값 유지) — printf "$TEN" 으로 씀
P=$TMP/p8; newproj "$P"; cp "$UPD8" "$P/conf/"
U=$P/conf/$(basename "$UPD8"); F=$P/conf/auto_setup.conf
sed "$V8DEL" conf/auto_setup.conf.example \
	| sed -e 's/^os6_mgmt=$/os6_mgmt=OO-mgmt/' -e 's|^ldap_engine=$|ldap_engine=/OO/ldap|' \
	      -e 's/^# 비어 있지 않으면 원격.*/# 원격 클라이언트 (현장 메모: 담당자 OO)/' \
	| sed 's/$/\r/' > "$F"
cp "$F" "$TMP/orig8"
before_vals=$(tr -d '\r' < "$F" | grep -v '^#' | grep '=')
bash "$U" --dir "$P" --dry --yes < /dev/null > "$TMP/o8d" 2>&1
rc=$?
chk "--dry 종료코드 0" "[ $rc -eq 0 ]"
chk "--dry 는 파일 무변경(CRLF 그대로)" "cmp -s $F $TMP/orig8"
chk "--dry 는 백업·저널을 만들지 않음" "! ls $F.bak.* >/dev/null 2>&1 && [ ! -e $P/conf/.update_journal ]"
printf "$TEN" | bash "$U" --dir "$P" --yes > "$TMP/o8" 2>&1
rc=$?
chk "종료코드 0" "[ $rc -eq 0 ]"
chk "newhost_template_1~9 빈 변수 9개 삽입" "[ \$(grep -c '^newhost_template_[1-9]=\$' $F) -eq 9 ]"
chk "work_os6_over 빈 변수 1개 삽입" "[ \$(grep -c '^work_os6_over=\$' $F) -eq 1 ]"
chk "설명 주석(v0.8.0) 삽입" "grep -q '^# (v0.8.0) 신규 설치 양식 템플릿' $F && grep -q '^# (v0.8.0) gossh 작업' $F"
chk "ldap_engine(v0.7.0 마지막 변수) 바로 다음에 삽입" "grep -A2 '^ldap_engine=' $F | grep -q '^# (v0.8.0) 신규'"
chk "기존 값 줄 전부 보존" "[ \"\$(grep -v '^#' $F | grep '=' | grep -v -e '^newhost_template_' -e '^work_os6_over=')\" = \"\$before_vals\" ]"
chk "현장 주석 보존" "grep -q '현장 메모: 담당자 OO' $F"
chk "CR 제거(LF)" "! grep -q \$'\\r' $F"
chk "백업 생성" "ls $F.bak.* >/dev/null 2>&1"
cp "$F" "$TMP/after8"
printf "$TEN" | bash "$U" --dir "$P" --yes > "$TMP/o8b" 2>&1
chk "재실행은 변경 없음(멱등)" "cmp -s $F $TMP/after8 && [ \$(grep -c '^work_os6_over=' $F) -eq 1 ] && [ \$(grep -c '^newhost_template_1=' $F) -eq 1 ]"
chk "업데이트 결과의 키 집합 = 예시의 키 집합" "[ \"\$(grep -o '^[a-z_0-9]*=' $F | sort)\" = \"\$(grep -o '^[a-z_0-9]*=' conf/auto_setup.conf.example | sort)\" ]"

echo "== 8b. --undo: v0.8.0 업데이트 전(LF) 바이트 동일 복원 =="
P=$TMP/p8u; newproj "$P"; cp "$UPD8" "$P/conf/"
U=$P/conf/$(basename "$UPD8"); F=$P/conf/auto_setup.conf
sed "$V8DEL" conf/auto_setup.conf.example | sed 's/^os6_mgmt=$/os6_mgmt=OO/' > "$F"
cp "$F" "$TMP/orig8u"
printf "$TEN" | bash "$U" --dir "$P" --yes > /dev/null 2>&1
chk "업데이트가 적용됨" "grep -q '^work_os6_over=' $F"
bash "$U" --dir "$P" --undo --yes < /dev/null > "$TMP/o8u" 2>&1
chk "undo 바이트 동일" "cmp -s $F $TMP/orig8u"

echo "== 8c. 순차 적용: v0.4.0 시기 conf → update_v0.6.0 → v0.7.0 → v0.8.0 (각 단계 새 변수만 추가, 값 보존) =="
[ -f "$UPD7" ] && [ -f "$UPD6" ] || { ng "순차 적용에 필요한 업데이트 스크립트 없음 ($UPD6 $UPD7)"; echo "PASS=$PASS FAIL=$FAIL"; exit 1; }
P=$TMP/p8s; newproj "$P"; cp "$UPD6" "$UPD7" "$UPD8" "$P/conf/"
F=$P/conf/auto_setup.conf
sed -e '/^# (v0.6.0)/,/^awx_profile_9=$/d' -e '/^# (v0.7.0)/,/^ldap_engine=$/d' -e "$V8DEL" conf/auto_setup.conf.example \
	| sed -e 's/^os6_mgmt=$/os6_mgmt=OO-mgmt/' -e 's|^awx_dir=$|awx_dir=/OO/awx|' \
	      -e 's/^# 비어 있지 않으면 원격.*/# 원격 클라이언트 (현장 메모: 담당자 OO)/' > "$F"
chk "시작 사본에는 v0.6.0 이후 변수가 없음" "! grep -q -e '^awx_profile_' -e '^enable_ip_change=' -e '^newhost_template_' -e '^work_os6_over=' $F"
before_vals=$(grep -v '^#' "$F" | grep '=')
printf '\n\n\n\n\n\n\n\n\n' | bash "$P/conf/$(basename "$UPD6")" --dir "$P" --yes > "$TMP/o8s6" 2>&1; rc6=$?
printf '\n\n\n\n' | bash "$P/conf/$(basename "$UPD7")" --dir "$P" --yes > "$TMP/o8s7" 2>&1; rc7=$?
printf "$TEN" | bash "$P/conf/$(basename "$UPD8")" --dir "$P" --yes > "$TMP/o8s8" 2>&1; rc8=$?
chk "세 단계 모두 종료코드 0" "[ $rc6 -eq 0 ] && [ $rc7 -eq 0 ] && [ $rc8 -eq 0 ]"
chk "최종 키 집합 = 예시의 키 집합" "[ \"\$(grep -o '^[a-z_0-9]*=' $F | sort)\" = \"\$(grep -o '^[a-z_0-9]*=' conf/auto_setup.conf.example | sort)\" ]"
chk "각 키는 한 번씩만" "[ -z \"\$(grep -o '^[a-z_0-9]*=' $F | sort | uniq -d)\" ]"
chk "기존 값 줄 전부 보존" "[ \"\$(grep -v '^#' $F | grep '=' | grep -v -e '^awx_profile_' -e '^enable_' -e '^ip_engine=' -e '^ldap_engine=' -e '^newhost_template_' -e '^work_os6_over=')\" = \"\$before_vals\" ]"
chk "현장 주석 보존" "grep -q '현장 메모: 담당자 OO' $F"
chk "v0.8.0 블록이 v0.7.0 블록(ldap_engine) 뒤에 위치" "awk '/^ldap_engine=/{a=NR} /^newhost_template_1=/{b=NR} END{exit !(a && b && b>a)}' $F"

echo
echo "PASS=$PASS FAIL=$FAIL"
[ $FAIL -eq 0 ]
