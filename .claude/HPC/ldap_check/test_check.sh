#!/usr/bin/env bash
###############################################################################
# test_check.sh — ldap_check.sh 단독 회귀 테스트
#
# 실제 장비도, 설정 엔진(ldap_setting)도 필요하지 않습니다.
# 손으로 만든 fixture /etc 트리를 ROOT 로 넘겨 판별·검사 결과를 확인합니다.
#
# 기준값은 ldap_config.conf.sample 의 zxcv 인프라입니다.
###############################################################################

set -u
cd "$(dirname "$0")" || exit 2

CHECK="./ldap_check.sh"
CONF="$(mktemp)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK" "$CONF"' EXIT

cp ldap_config.conf.sample "$CONF"

PASS=0
FAIL=0
ok() { PASS=$((PASS+1)); echo "  PASS  $1"; }
ng() { FAIL=$((FAIL+1)); echo "  FAIL  $1"; [ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/        /'; }

check() { LDAP_CONFIG="$CONF" ROOT="$1" bash "$CHECK" 2>&1; }

###############################################################################
# zxcv / a1 에 완전히 부합하는 정상 fixture
###############################################################################

URIS='ldap://10.10.1.20/ ldap://10.10.1.21/ ldap://10.10.1.22/'
DN='uid=svcaccount,ou=user,ou=system,dc=example,dc=com'
PW='CHANGE_ME_zxcv'

make_good()
{
    local d="$1"
    rm -rf "$d"; mkdir -p "$d/etc/openldap" "$d/etc/sssd"

    echo "Rocky Linux release 8.10 (Green Obsidian)" > "$d/etc/redhat-release"

    printf '# ldap.conf\nBASE dc=example,dc=com\nURI %s\nBINDDN %s\nBINDPW %s\n' \
        "$URIS" "$DN" "$PW" > "$d/etc/openldap/ldap.conf"

    printf '[ autofs ]\ntimeout = 300\nldap_uri = "%s"\n' "$URIS" > "$d/etc/autofs.conf"

    printf '<?xml version="1.0" ?>\n<autofs_ldap_sasl_conf\n    usetls="no"\n    tlsrequired="no"\n    authrequired="simple"\n    user="%s"\n    secret="%s"\n/>\n' \
        "$DN" "$PW" > "$d/etc/autofs_ldap_auth.conf"

    printf '[sssd]\ndomains = example.com\n\n[domain/example.com]\nid_provider = ldap\nldap_uri = %s\nldap_default_bind_dn = %s\nldap_default_authtok = %s\n' \
        "$URIS" "$DN" "$PW" > "$d/etc/sssd/sssd.conf"

    printf 'search example.com\nnameserver 10.10.1.10\nnameserver 10.10.1.11\n' > "$d/etc/resolv.conf"
    printf '# chrony\nserver 10.20.1.10 iburst\nserver 10.20.1.11 iburst\ndriftfile /var/lib/chrony/drift\n' > "$d/etc/chrony.conf"
    printf '/appl\t-ro,hard,tcp,vers=3\tasdf1:/appl1\n/wappl\t-rw,hard,tcp,vers=3\tasdf1:/wappl1\n' > "$d/etc/auto.appl"
}

###############################################################################
echo "[1] 정상 노드는 8개 항목이 모두 OK 여야 한다"
###############################################################################

make_good "$WORK/good"
out="$(check "$WORK/good")"; rc=$?
nok="$(printf '%s\n' "$out" | grep -c '^OK')"
if [ "$rc" = "0" ] && [ "$nok" = "8" ] && ! printf '%s\n' "$out" | grep -q '^FAIL'; then
    ok "정상 노드 → OK 8건, exit 0"
else
    ng "정상 노드 판정 실패 (rc=$rc, OK=$nok)" "$out"
fi

if printf '%s\n' "$out" | grep -q 'auto.appl a1' && printf '%s\n' "$out" | grep -q '/zxcv'; then
    ok "infra=zxcv, site=a1 로 판별"
else
    ng "판별 결과가 다름" "$out"
fi

###############################################################################
echo "[2] DNS 만 다른 인프라 값이면 교차 검증에서 걸려야 한다"
###############################################################################

cp -a "$WORK/good" "$WORK/dns"
printf 'nameserver 10.10.2.10\nnameserver 10.10.2.11\n' > "$WORK/dns/etc/resolv.conf"
out="$(check "$WORK/dns")"; rc=$?
if [ "$rc" = "1" ] && printf '%s\n' "$out" | grep -q 'infra-mismatch'; then
    ok "DNS 불일치 → infra-mismatch"
else
    ng "DNS 불일치를 잡지 못함 (rc=$rc)" "$out"
fi

###############################################################################
echo "[3] auto.appl 만 다른 인프라 값이면 걸려야 한다"
###############################################################################

cp -a "$WORK/good" "$WORK/appl"
printf '/appl\t-rw,soft,intr\tqwer3:/appl3\n' > "$WORK/appl/etc/auto.appl"
out="$(check "$WORK/appl")"; rc=$?
if [ "$rc" = "1" ] && printf '%s\n' "$out" | grep -q 'appl=qwer'; then
    ok "auto.appl 혼재 → infra-mismatch (ldap=zxcv appl=qwer)"
else
    ng "auto.appl 혼재를 잡지 못함 (rc=$rc)" "$out"
fi

###############################################################################
echo "[4] bindpw 만 틀리면 LDAP 축 판별이 실패해야 한다"
###############################################################################

cp -a "$WORK/good" "$WORK/pw"
sed -i 's|^BINDPW .*|BINDPW WRONG|' "$WORK/pw/etc/openldap/ldap.conf"
out="$(check "$WORK/pw")"; rc=$?
if [ "$rc" = "1" ] && printf '%s\n' "$out" | grep -q 'ldap=?'; then
    ok "bindpw 불일치 → LDAP 축 판별 실패"
else
    ng "bindpw 불일치를 잡지 못함 (rc=$rc)" "$out"
fi

###############################################################################
echo "[5] URI 순서만 틀리면 ldap.conf 한 건만 FAIL 이어야 한다"
###############################################################################

cp -a "$WORK/good" "$WORK/ord"
sed -i 's|^URI .*|URI ldap://10.10.1.21/ ldap://10.10.1.20/ ldap://10.10.1.22/|' \
    "$WORK/ord/etc/openldap/ldap.conf"
out="$(check "$WORK/ord")"; rc=$?
nfail="$(printf '%s\n' "$out" | grep -c '^FAIL')"
if [ "$rc" = "1" ] && [ "$nfail" = "1" ] &&
   printf '%s\n' "$out" | grep '^FAIL' | grep -q 'ldap.conf'; then
    ok "URI 순서 오류를 ldap.conf 에서만 감지"
else
    ng "URI 순서 검사 오류 (rc=$rc, FAIL=$nfail)" "$out"
fi

###############################################################################
echo "[6] chrony 에 pool 줄이 남아 있으면 NTP 집합이 달라져 걸려야 한다"
###############################################################################

cp -a "$WORK/good" "$WORK/pool"
printf '# chrony\npool 2.pool.ntp.org iburst\nserver 10.20.1.10 iburst\nserver 10.20.1.11 iburst\n' \
    > "$WORK/pool/etc/chrony.conf"
out="$(check "$WORK/pool")"; rc=$?
if [ "$rc" = "1" ]; then
    ok "잔존 pool 줄 감지"
else
    ng "잔존 pool 줄을 잡지 못함 (rc=$rc)" "$out"
fi

###############################################################################
echo "[7] uri3=NONE 인프라(qwer)에서 a1/a4 를 storage 로 구분해야 한다"
###############################################################################

QURIS='ldap://10.10.2.20/ ldap://10.10.2.21/'
QPW='CHANGE_ME_qwer'
for S in a1:qwer1:/appl1 a4:qwer4:/appl4; do
    site="${S%%:*}"; rest="${S#*:}"; store="${rest%%:*}"; mnt="${rest#*:}"
    d="$WORK/q_$site"
    make_good "$d"
    printf '# ldap.conf\nURI %s\nBINDDN %s\nBINDPW %s\n' "$QURIS" "$DN" "$QPW" > "$d/etc/openldap/ldap.conf"
    printf '[ autofs ]\nldap_uri = "%s"\n' "$QURIS" > "$d/etc/autofs.conf"
    printf '<?xml version="1.0" ?>\n<autofs_ldap_sasl_conf user="%s" secret="%s" />\n' "$DN" "$QPW" \
        > "$d/etc/autofs_ldap_auth.conf"
    printf '[sssd]\n\n[domain/example.com]\nldap_uri = %s\nldap_default_bind_dn = %s\nldap_default_authtok = %s\n' \
        "$QURIS" "$DN" "$QPW" > "$d/etc/sssd/sssd.conf"
    printf 'nameserver 10.10.2.10\nnameserver 10.10.2.11\n' > "$d/etc/resolv.conf"
    printf 'server 10.20.2.10 iburst\nserver 10.20.2.11 iburst\n' > "$d/etc/chrony.conf"
    printf '/appl\t-rw,soft,intr\t%s:%s\n' "$store" "$mnt" > "$d/etc/auto.appl"

    out="$(check "$d")"; rc=$?
    if [ "$rc" = "0" ] && printf '%s\n' "$out" | grep -q "auto.appl $site" &&
       printf '%s\n' "$out" | grep -q '/qwer'; then
        ok "qwer/$site (URI 순서가 a1·a4 동일) 판별"
    else
        ng "qwer/$site 판별 실패 (rc=$rc)" "$out"
    fi
done

###############################################################################
echo "[8] 설정 파일이 없으면 exit 2 여야 한다"
###############################################################################

out="$(LDAP_CONFIG=/nonexistent/x.conf bash "$CHECK" 2>&1)"; rc=$?
if [ "$rc" = "2" ]; then
    ok "설정 파일 부재 시 exit 2"
else
    ng "설정 파일 부재 처리 오류 (rc=$rc)" "$out"
fi

###############################################################################
echo
echo "=============================="
echo " PASS: $PASS   FAIL: $FAIL"
echo "=============================="
###############################################################################

[ "$FAIL" -gt 0 ] && exit 1
exit 0
