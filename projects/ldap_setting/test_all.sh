#!/usr/bin/env bash
###############################################################################
# test_all.sh — 실제 장비 없이 돌리는 회귀 테스트
#
# apply 스크립트를 fixture 디렉터리(/tmp/...)에 대고 실행한 뒤,
# 같은 fixture 를 체크 스크립트로 검사해 기대한 결과가 나오는지 확인합니다.
# gossh 도 네트워크도 필요하지 않습니다.
###############################################################################

set -u
cd "$(dirname "$0")" || exit 2

ENGINE="./bin/ldap-config-engine"
CHECK="${CHECK:-../ldap_check/ldap_check.sh}"
CONF="$(mktemp)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK" "$CONF"' EXIT

cp conf/ldap_config.conf.sample "$CONF"

PASS=0
FAIL=0

ok()   { PASS=$((PASS+1)); echo "  PASS  $1"; }
ng()   { FAIL=$((FAIL+1)); echo "  FAIL  $1"; [ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/        /'; }

if [ ! -x "$ENGINE" ]; then
    echo "오류: $ENGINE 이 없습니다. ./setup.sh 를 먼저 실행하십시오."
    exit 2
fi

# 적용(ldap_setting)과 검증(ldap_check)은 짝이라, 왕복 테스트에 두 쪽이 다 필요합니다.
if [ ! -f "$CHECK" ]; then
    echo "오류: 검증 스크립트를 찾을 수 없습니다: $CHECK"
    echo "      같은 저장소의 ../ldap_check/ 를 함께 내려받았는지 확인하십시오."
    echo "      다른 위치에 있으면 CHECK=<경로> 로 지정하십시오."
    exit 2
fi

###############################################################################
# fixture 만들기
#   $1 = 경로, $2 = OS 메이저(7|8)
###############################################################################

make_fixture()
{
    local d="$1" os="$2"
    rm -rf "$d"; mkdir -p "$d/etc/openldap" "$d/etc/sssd"

    if [ "$os" = "7" ]; then
        echo "Red Hat Enterprise Linux Server release 7.9 (Maipo)" > "$d/etc/redhat-release"
        printf '# nslcd\nuid nslcd\ngid ldap\nuri ldap://old/\nbase dc=e,dc=c\n' > "$d/etc/nslcd.conf"
        printf 'driftfile /var/lib/ntp/drift\nserver 0.rhel.pool.ntp.org iburst\nrestrict default nomodify\n' > "$d/etc/ntp.conf"
    else
        echo "Rocky Linux release 8.10 (Green Obsidian)" > "$d/etc/redhat-release"
        printf '[sssd]\nservices = nss, pam\ndomains = example.com\n\n[domain/example.com]\nid_provider = ldap\nldap_uri = ldap://old/\ncache_credentials = True\n' > "$d/etc/sssd/sssd.conf"
        printf '# chrony\npool 2.pool.ntp.org iburst\ndriftfile /var/lib/chrony/drift\n' > "$d/etc/chrony.conf"
        # s4 예외로 nslcd/ntp 가 선택될 수도 있으므로 양쪽 다 준비해 둡니다.
        printf '# nslcd\nuri ldap://old/\n' > "$d/etc/nslcd.conf"
        printf 'server old iburst\n' > "$d/etc/ntp.conf"
    fi

    printf '# ldap.conf\nBASE dc=example,dc=com\nURI ldap://old/\nTLS_CACERTDIR /etc/openldap/certs\n' > "$d/etc/openldap/ldap.conf"
    printf '[ autofs ]\ntimeout = 300\nbrowse_mode = no\nldap_uri="ldap://old/"\nmap_object_class = "automountMap"\n' > "$d/etc/autofs.conf"
    printf 'search example.com\nnameserver 8.8.8.8\noptions timeout:2\n' > "$d/etc/resolv.conf"
    printf '/appl\t-rw,soft,intr\toldstore:/applX\n' > "$d/etc/auto.appl"
}

# hostname 을 위조해서 s4 예외를 테스트하기 위한 가짜 실행파일
FAKEBIN="$WORK/fakebin"
mkdir -p "$FAKEBIN"
printf '#!/bin/sh\necho s4node01\n' > "$FAKEBIN/hostname"
chmod +x "$FAKEBIN/hostname"

apply()   { ROOT="$1" bash "$2" >/dev/null 2>&1; }
check()   { LDAP_CONFIG="$CONF" ROOT="$1" bash "$CHECK" 2>&1; }

###############################################################################
echo "[1] RHEL 8 : 사이트 a1~a4 적용 후 검증이 전부 OK 인가"
###############################################################################

for S in a1 a2 a3 a4; do
    "$ENGINE" -config "$CONF" -infra zxcv -site "$S" -print-script > "$WORK/ap_$S.sh" 2>/dev/null
    make_fixture "$WORK/f_$S" 8
    apply "$WORK/f_$S" "$WORK/ap_$S.sh"
    out="$(check "$WORK/f_$S")"; rc=$?
    if [ "$rc" = "0" ] && [ "$out" = "$(printf 'INFO\tLDAP\tzxcv\t%s' "$S")" ]; then
        ok "zxcv/$S 라운드트립"
    else
        ng "zxcv/$S 라운드트립 (rc=$rc)" "$out"
    fi
done

###############################################################################
echo "[2] 멱등성 : 두 번째 실행은 NOCHANGE 여야 한다"
###############################################################################

out="$(ROOT="$WORK/f_a1" bash "$WORK/ap_a1.sh" 2>&1)"
if printf '%s' "$out" | grep -q "NOCHANGE"; then
    ok "재실행 시 NOCHANGE"
else
    ng "재실행이 멱등하지 않음" "$out"
fi

###############################################################################
echo "[3] RHEL 7 : nslcd + ntp 를 쓰고 sssd/chrony 는 건드리지 않아야 한다"
###############################################################################

make_fixture "$WORK/f7" 7
apply "$WORK/f7" "$WORK/ap_a1.sh"
if grep -q "^binddn uid=svcaccount" "$WORK/f7/etc/nslcd.conf" 2>/dev/null &&
   grep -q "^server 10.20.1.10 iburst" "$WORK/f7/etc/ntp.conf" 2>/dev/null &&
   [ ! -f "$WORK/f7/etc/sssd/sssd.conf" ]; then
    ok "RHEL7 → nslcd + ntp"
else
    ng "RHEL7 분기 오류"
fi

out="$(check "$WORK/f7")"
if [ "$out" = "$(printf 'INFO\tLDAP\tzxcv\ta1')" ]; then
    ok "RHEL7 검증 OK"
else
    ng "RHEL7 검증 실패" "$out"
fi

###############################################################################
echo "[4] s4 예외 : RHEL 8 이어도 nslcd + ntp 를 강제해야 한다"
###############################################################################

make_fixture "$WORK/fs4" 8
PATH="$FAKEBIN:$PATH" apply "$WORK/fs4" "$WORK/ap_a1.sh"
if grep -q "^binddn uid=svcaccount" "$WORK/fs4/etc/nslcd.conf" 2>/dev/null &&
   grep -q "^server 10.20.1.10 iburst" "$WORK/fs4/etc/ntp.conf" 2>/dev/null &&
   ! grep -q "ldap_default_authtok" "$WORK/fs4/etc/sssd/sssd.conf" 2>/dev/null &&
   ! grep -q "10.20.1.10" "$WORK/fs4/etc/chrony.conf" 2>/dev/null; then
    ok "s4 → nslcd + ntp 강제, sssd/chrony 미변경"
else
    ng "s4 예외 처리 오류"
fi

###############################################################################
echo "[5] 인프라 혼재 : auto.appl 만 다른 인프라 값이면 전부 FAIL 이어야 한다"
###############################################################################

cp -a "$WORK/f_a1" "$WORK/fmix"
printf '/appl\t-rw,soft,intr\tqwer3:/appl3\n' > "$WORK/fmix/etc/auto.appl"
out="$(check "$WORK/fmix")"; rc=$?
if [ "$rc" = "1" ] && [ "$out" = "$(printf 'FAIL\tLDAP\tUNDEFINED')" ]; then
    ok "인프라 혼재 감지"
else
    ng "인프라 혼재를 잡지 못함 (rc=$rc)" "$out"
fi

###############################################################################
echo "[6] URI 순서 오류 : ldap.conf 만 FAIL 이고 나머지는 OK 여야 한다"
###############################################################################

cp -a "$WORK/f_a1" "$WORK/ford"
sed -i 's|^URI .*|URI ldap://10.10.1.21/ ldap://10.10.1.20/ ldap://10.10.1.22/|' "$WORK/ford/etc/openldap/ldap.conf"
out="$(check "$WORK/ford")"; rc=$?
if [ "$rc" = "1" ] && [ "$out" = "$(printf 'FAIL\tLDAP\tUNDEFINED')" ]; then
    ok "URI 순서 오류 감지"
else
    ng "URI 순서 검사 오류 (rc=$rc)" "$out"
fi

###############################################################################
echo "[7] uri3=NONE : URI 순서가 같아지는 a1/a4 를 storage 로 구분해야 한다"
###############################################################################

for S in a1 a4; do
    "$ENGINE" -config "$CONF" -infra qwer -site "$S" -print-script > "$WORK/q_$S.sh" 2>/dev/null
    make_fixture "$WORK/fq_$S" 8
    apply "$WORK/fq_$S" "$WORK/q_$S.sh"
    out="$(check "$WORK/fq_$S")"; rc=$?
    if [ "$rc" = "0" ] && [ "$out" = "$(printf 'INFO\tLDAP\tqwer\t%s' "$S")" ]; then
        ok "qwer/$S (uri3=NONE) 판별"
    else
        ng "qwer/$S 판별 실패 (rc=$rc)" "$out"
    fi
done

# 두 사이트의 URI 줄이 실제로 같은지 확인 — 같아야 이 테스트가 의미가 있습니다.
u1="$(awk 'tolower($1)=="uri"{$1="";sub(/^ +/,"");print}' "$WORK/fq_a1/etc/openldap/ldap.conf")"
u4="$(awk 'tolower($1)=="uri"{$1="";sub(/^ +/,"");print}' "$WORK/fq_a4/etc/openldap/ldap.conf")"
if [ "$u1" = "$u4" ] && [ -n "$u1" ]; then
    ok "a1/a4 의 URI 줄이 동일함을 확인 (storage 로만 구분 가능한 상황)"
else
    ng "테스트 전제가 깨짐: a1=[$u1] a4=[$u4]"
fi

###############################################################################
echo "[8] 설정 파일이 없으면 검증이 exit 2 로 끝나야 한다"
###############################################################################

out="$(LDAP_CONFIG=/nonexistent/x.conf bash "$CHECK" 2>&1)"; rc=$?
if [ "$rc" = "2" ]; then
    ok "설정 파일 부재 시 exit 2"
else
    ng "설정 파일 부재 처리 오류 (rc=$rc)" "$out"
fi

###############################################################################
echo "[9] 롤백 : 적용 전 상태로 정확히 되돌아가야 한다"
###############################################################################

"$ENGINE" -rollback      -print-script > "$WORK/rb_latest.sh" 2>/dev/null
"$ENGINE" -list-backups  -print-script > "$WORK/rb_list.sh"   2>/dev/null

make_fixture "$WORK/rb" 8
cp -a "$WORK/rb" "$WORK/rb_orig"

apply "$WORK/rb" "$WORK/ap_a1.sh"
out="$(ROOT="$WORK/rb" bash "$WORK/rb_latest.sh" 2>&1)"; rc=$?

# 백업 파일과, 적용이 새로 만들어 되돌릴 수 없는 파일은 비교에서 뺍니다.
if [ "$rc" = "0" ] &&
   diff -r --exclude='*.bak.*' --exclude='autofs_ldap_auth.conf' --exclude='auto.appl_back' \
        "$WORK/rb_orig" "$WORK/rb" >/dev/null 2>&1; then
    ok "롤백 후 원본과 완전히 일치"
else
    ng "롤백 결과가 원본과 다름 (rc=$rc)" \
       "$(diff -r --exclude='*.bak.*' --exclude='autofs_ldap_auth.conf' --exclude='auto.appl_back' "$WORK/rb_orig" "$WORK/rb" 2>&1 | head -20)"
fi

# 여러 번 고치는 파일(ldap.conf 는 URI/BINDDN/BINDPW 3회)이 제대로 돌아왔는지 콕 집어 확인.
# 백업을 매 수정마다 덮어쓰면 여기서 '부분 적용 상태' 가 남습니다.
if diff -q "$WORK/rb_orig/etc/openldap/ldap.conf" "$WORK/rb/etc/openldap/ldap.conf" >/dev/null 2>&1; then
    ok "여러 번 수정된 ldap.conf 도 원본으로 복원"
else
    ng "ldap.conf 가 부분 적용 상태로 복원됨" \
       "$(diff -u "$WORK/rb_orig/etc/openldap/ldap.conf" "$WORK/rb/etc/openldap/ldap.conf" 2>&1 | head -15)"
fi

###############################################################################
echo "[10] 롤백 재실행은 NOCHANGE 여야 한다"
###############################################################################

out="$(ROOT="$WORK/rb" bash "$WORK/rb_latest.sh" 2>&1)"
if printf '%s\n' "$out" | grep -q 'NOCHANGE'; then
    ok "롤백 멱등"
else
    ng "롤백이 멱등하지 않음" "$out"
fi

###############################################################################
echo "[11] 롤백 DRY-RUN 은 아무것도 바꾸지 않아야 한다"
###############################################################################

make_fixture "$WORK/rbd" 8
apply "$WORK/rbd" "$WORK/ap_a1.sh"
cp -a "$WORK/rbd" "$WORK/rbd_before"
out="$(ROOT="$WORK/rbd" DRYRUN=1 bash "$WORK/rb_latest.sh" 2>&1)"
if printf '%s\n' "$out" | grep -q 'WOULD-RESTORE' &&
   diff -r "$WORK/rbd_before" "$WORK/rbd" >/dev/null 2>&1; then
    ok "DRY-RUN 은 보고만 하고 파일을 건드리지 않음"
else
    ng "DRY-RUN 이 파일을 바꿨거나 보고하지 않음" "$out"
fi

###############################################################################
echo "[12] -list-backups 는 시점과 파일 목록을 보여주고 아무것도 바꾸지 않아야 한다"
###############################################################################

cp -a "$WORK/rbd" "$WORK/rbl_before"
out="$(ROOT="$WORK/rbd" bash "$WORK/rb_list.sh" 2>&1)"
stamp="$(printf '%s\n' "$out" | sed -n 's/^BACKUP|\([0-9]\{14\}\)|.*/\1/p' | head -n1)"
if [ -n "$stamp" ] && printf '%s\n' "$out" | grep -q 'LISTED' &&
   diff -r "$WORK/rbl_before" "$WORK/rbd" >/dev/null 2>&1; then
    ok "백업 시점 조회 ($stamp), 파일 변경 없음"
else
    ng "-list-backups 오류" "$out"
fi

###############################################################################
echo "[13] 두 번 적용한 뒤 '첫 적용 이전' 시점으로 정확히 되돌아가야 한다"
###############################################################################

make_fixture "$WORK/rb2" 8
cp -a "$WORK/rb2" "$WORK/rb2_orig"

apply "$WORK/rb2" "$WORK/ap_a1.sh"
first_stamp="$(ROOT="$WORK/rb2" bash "$WORK/rb_list.sh" 2>&1 \
    | sed -n 's/^BACKUP|\([0-9]\{14\}\)|.*/\1/p' | head -n1)"

sleep 1                       # 타임스탬프가 겹치지 않도록
apply "$WORK/rb2" "$WORK/ap_a3.sh"

nstamps="$(ROOT="$WORK/rb2" bash "$WORK/rb_list.sh" 2>&1 | grep -c '^BACKUP|')"
if [ "$nstamps" -ge 2 ]; then
    ok "적용 2회 → 백업 시점 2개 이상 ($nstamps)"
else
    ng "백업 시점이 쌓이지 않음 ($nstamps)"
fi

"$ENGINE" -rollback-to "$first_stamp" -print-script > "$WORK/rb_first.sh" 2>/dev/null
out="$(ROOT="$WORK/rb2" bash "$WORK/rb_first.sh" 2>&1)"; rc=$?
if [ "$rc" = "0" ] &&
   diff -r --exclude='*.bak.*' --exclude='autofs_ldap_auth.conf' --exclude='auto.appl_back' \
        "$WORK/rb2_orig" "$WORK/rb2" >/dev/null 2>&1; then
    ok "-rollback-to $first_stamp 로 최초 상태 복원"
else
    ng "지정 시점 복원 실패 (rc=$rc)" \
       "$(diff -r --exclude='*.bak.*' --exclude='autofs_ldap_auth.conf' --exclude='auto.appl_back' "$WORK/rb2_orig" "$WORK/rb2" 2>&1 | head -20)"
fi

###############################################################################
echo "[14] 백업이 하나도 없으면 NOBACKUP 을 보고해야 한다"
###############################################################################

make_fixture "$WORK/rbn" 8
out="$(ROOT="$WORK/rbn" bash "$WORK/rb_latest.sh" 2>&1)"; rc=$?
if [ "$rc" = "0" ] && printf '%s\n' "$out" | grep -q 'NOBACKUP'; then
    ok "백업 없음 → NOBACKUP, exit 0"
else
    ng "NOBACKUP 처리 오류 (rc=$rc)" "$out"
fi

###############################################################################
echo
echo "=============================="
echo " PASS: $PASS   FAIL: $FAIL"
echo "=============================="
###############################################################################

[ "$FAIL" -gt 0 ] && exit 1
exit 0
