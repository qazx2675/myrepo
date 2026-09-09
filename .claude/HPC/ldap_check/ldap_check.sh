#!/bin/bash
###############################################################################
# ldap_check.sh — 노드 로컬 LDAP/DNS/NTP/autofs 설정 정합성 검사
#
# 배치 위치
#   보통 /user/asdf/ 에 두고 gossh 로 전 노드에서 로컬 실행합니다.
#   같은 디렉터리의 ldap_config.conf 를 읽습니다.
#
# 판별 방식 (3중 교차 검증)
#   infra_dnsntp : resolv.conf 의 nameserver + chrony/ntp.conf 의 server 집합
#   infra_ldap   : ldap.conf 의 URI 집합 + BINDDN + BINDPW
#   infra_appl   : auto.appl 의 storage 이름
#
#   셋이 모두 같은 인프라를 가리킬 때만 정상입니다.
#   하나라도 다르면 그 노드는 인프라가 섞인 것이므로 전부 FAIL 입니다.
#
#   site 는 auto.appl 의 storage 로 결정합니다.
#   (URI 순서는 사이트마다 겹칠 수 있어 판별 키로 쓸 수 없습니다. 판별이 아니라
#    '결정된 사이트의 기대 순서와 맞는지' 검증에만 씁니다.)
#
# 출력   한 줄 요약만 찍습니다.
#          정상: INFO<TAB>LDAP<TAB><infra><TAB><site>
#          실패: FAIL<TAB>LDAP<TAB>UNDEFINED  (원인은 이 스크립트 안에서만 판정, 밖으로는 안 찍음)
# 종료코드  0 = 전부 OK,  1 = 설정 불일치,  2 = 판별 불가 또는 파일 없음
#
# jq 를 쓰지 않습니다. 설정 파일이 평문 key=value 이기 때문입니다.
###############################################################################

CONFIG_FILE="${LDAP_CONFIG:-$(dirname "$0")/ldap_config.conf}"
ROOT="${ROOT:-}"

if [ ! -f "$CONFIG_FILE" ]; then
    echo "FAIL	LDAP	UNDEFINED"
    exit 2
fi

###############################################################################
# 설정 파일 읽기
###############################################################################

# conf_get <키> : 평문 key=value 에서 값 하나를 꺼냅니다. 키의 '.' 은 그대로 매칭.
conf_get()
{
    local key esc
    key="$1"
    esc="$(printf '%s' "$key" | sed 's/[.[\*^$]/\\&/g')"
    sed -n "s/^[[:space:]]*${esc}[[:space:]]*=[[:space:]]*\(.*\)\$/\1/p" "$CONFIG_FILE" \
        | head -n1 | sed 's/[[:space:]]*$//'
}

# conf_list <키> : 쉼표로 구분된 값을 한 줄에 하나씩.
conf_list()
{
    conf_get "$1" | tr ',' '\n' | sed 's/^[[:space:]]*//; s/[[:space:]]*$//' | sed '/^$/d'
}

# 정의된 인프라 이름들
INFRAS="$(sed -n 's/^[[:space:]]*infra\.\([^.]*\)\..*$/\1/p' "$CONFIG_FILE" | sort -u)"

if [ -z "$INFRAS" ]; then
    echo "FAIL	LDAP	UNDEFINED"
    exit 2
fi

# 인프라의 사이트 이름들
site_names()
{
    sed -n "s/^[[:space:]]*infra\.$1\.site\.\([^.]*\)\..*\$/\1/p" "$CONFIG_FILE" | sort -u
}

S4_ENABLED="$(conf_get s4.enabled)"
S4_PREFIX="$(conf_get s4.prefix)"
S4_SERVICES="$(conf_get s4.services)"

###############################################################################
# 공통 : 순서 무관 집합 비교 (기준값 = 실제값)
###############################################################################

same_set()
{
    local a b
    a="$(printf '%s\n' "$1" | sed '/^$/d' | sort -u)"
    b="$(printf '%s\n' "$2" | sed '/^$/d' | sort -u)"
    [ -n "$a" ] && [ "$a" = "$b" ]
}

###############################################################################
# 이 노드의 실제 설정값 읽기
###############################################################################

NODE_HOST="$(hostname -s 2>/dev/null || hostname)"

OS_MAJOR="$(sed -n 's/.*release \([0-9][0-9]*\).*/\1/p' "$ROOT/etc/redhat-release" 2>/dev/null | head -n1)"
if [ -z "$OS_MAJOR" ]; then
    OS_MAJOR="$(sed -n 's/^VERSION_ID="\{0,1\}\([0-9][0-9]*\).*/\1/p' "$ROOT/etc/os-release" 2>/dev/null | head -n1)"
fi
if [ -z "$OS_MAJOR" ]; then
    echo "FAIL	LDAP	UNDEFINED"
    exit 2
fi

# s4 예외 판정 (설정 파일의 규칙을 그대로 따릅니다)
IS_S4=0
if [ "$S4_ENABLED" = "true" ] && [ -n "$S4_PREFIX" ]; then
    case "$NODE_HOST" in
        "$S4_PREFIX"*) IS_S4=1 ;;
    esac
fi

s4_has()
{
    case ",$S4_SERVICES," in
        *",$1,"*) return 0 ;;
    esac
    return 1
}

if [ "$OS_MAJOR" -le 7 ]; then
    AUTH_FILE="/etc/nslcd.conf";     AUTH_KIND=nslcd
elif [ "$IS_S4" = "1" ] && s4_has nslcd; then
    AUTH_FILE="/etc/nslcd.conf";     AUTH_KIND=nslcd
else
    AUTH_FILE="/etc/sssd/sssd.conf"; AUTH_KIND=sssd
fi

if [ "$OS_MAJOR" -le 7 ]; then
    TIME_FILE="/etc/ntp.conf";    TIME_KIND=ntp
elif [ "$IS_S4" = "1" ] && s4_has ntp; then
    TIME_FILE="/etc/ntp.conf";    TIME_KIND=ntp
else
    TIME_FILE="/etc/chrony.conf"; TIME_KIND=chrony
fi

# --- DNS : resolv.conf 의 nameserver ---
ACTUAL_DNS="$(awk '$1 == "nameserver" { print $2 }' "$ROOT/etc/resolv.conf" 2>/dev/null)"

# --- NTP : server / pool 의 대상 주소 ---
ACTUAL_NTP="$(awk '$1 == "server" || $1 == "pool" { print $2 }' "$ROOT$TIME_FILE" 2>/dev/null)"

# --- LDAP : ldap.conf 의 URI 순서 / BINDDN / BINDPW ---
ACTUAL_URI_LINE="$(awk 'tolower($1) == "uri" { $1=""; sub(/^[ \t]+/,""); print; exit }' \
                    "$ROOT/etc/openldap/ldap.conf" 2>/dev/null)"
ACTUAL_URIS="$(printf '%s\n' "$ACTUAL_URI_LINE" | tr ' \t' '\n\n' | sed '/^$/d')"
ACTUAL_BINDDN="$(awk 'tolower($1) == "binddn" { $1=""; sub(/^[ \t]+/,""); print; exit }' \
                    "$ROOT/etc/openldap/ldap.conf" 2>/dev/null)"
ACTUAL_BINDPW="$(awk 'tolower($1) == "bindpw" { $1=""; sub(/^[ \t]+/,""); print; exit }' \
                    "$ROOT/etc/openldap/ldap.conf" 2>/dev/null)"

# --- auto.appl : /appl 로 시작하는 줄의 storage ---
#     "store:/appl1" 과 "store/appl1" 두 형식 모두 처리합니다.
ACTUAL_STORAGE="$(awk '$1 ~ /^\/appl/ { t=$3; sub(/[:/].*$/,"",t); if (t != "") print t }' \
                    "$ROOT/etc/auto.appl" 2>/dev/null | sort -u)"
ACTUAL_MOUNT="$(awk '$1 ~ /^\/appl/ { t=$3; sub(/^[^:/]*[:]/,"",t); if (t != "") print t; exit }' \
                    "$ROOT/etc/auto.appl" 2>/dev/null)"

# --- auto.appl : /wappl 로 시작하는 줄의 mountpoint (있을 때만 검사, 선택 항목) ---
ACTUAL_WAPPL_MOUNT="$(awk '$1 ~ /^\/wappl/ { t=$3; sub(/^[^:/]*[:]/,"",t); if (t != "") print t; exit }' \
                    "$ROOT/etc/auto.appl" 2>/dev/null)"

###############################################################################
# 1차 : DNS + NTP 로 인프라 판별
###############################################################################

INFRA_DNSNTP=""
for i in $INFRAS; do
    if same_set "$(conf_list "infra.$i.dns")" "$ACTUAL_DNS" &&
       same_set "$(conf_list "infra.$i.ntp")" "$ACTUAL_NTP"; then
        INFRA_DNSNTP="$i"
        break
    fi
done

###############################################################################
# 2차 : LDAP (URI 집합 + BINDDN + BINDPW) 로 인프라 판별
###############################################################################

INFRA_LDAP=""
for i in $INFRAS; do
    expect_uris=""
    for k in uri1 uri2 uri3; do
        v="$(conf_get "infra.$i.$k")"
        [ -z "$v" ] && continue
        [ "$v" = "NONE" ] && continue
        expect_uris="$expect_uris$v
"
    done
    if same_set "$expect_uris" "$ACTUAL_URIS" &&
       [ "$(conf_get "infra.$i.binddn")" = "$ACTUAL_BINDDN" ] &&
       [ "$(conf_get "infra.$i.bindpw")" = "$ACTUAL_BINDPW" ]; then
        INFRA_LDAP="$i"
        break
    fi
done

###############################################################################
# 3차 : auto.appl storage 로 인프라 + 사이트 판별
###############################################################################

INFRA_APPL=""
SITE=""
# storage 가 여러 개 잡히면 auto.appl 이 오염된 것이므로 판별하지 않습니다.
if [ "$(printf '%s\n' "$ACTUAL_STORAGE" | sed '/^$/d' | wc -l)" = "1" ]; then
    for i in $INFRAS; do
        for s in $(site_names "$i"); do
            if [ "$(conf_get "infra.$i.site.$s.storage")" = "$ACTUAL_STORAGE" ]; then
                INFRA_APPL="$i"
                SITE="$s"
                break
            fi
        done
        [ -n "$INFRA_APPL" ] && break
    done
fi

###############################################################################
# 교차 검증
###############################################################################

if [ -z "$INFRA_DNSNTP" ] || [ -z "$INFRA_LDAP" ] || [ -z "$INFRA_APPL" ] ||
   [ "$INFRA_DNSNTP" != "$INFRA_LDAP" ] || [ "$INFRA_DNSNTP" != "$INFRA_APPL" ]; then

    echo "FAIL	LDAP	UNDEFINED"
    exit 1
fi

INFRA="$INFRA_DNSNTP"
RC=0
# 파일별 결과는 더 이상 화면에 찍지 않고, RC 만 조용히 추적합니다.
# 사람이 원인을 봐야 할 때는 이 스크립트를 노드에서 직접 실행해 디버깅하십시오.
report()
{
    [ "$1" = "0" ] || RC=1
}

###############################################################################
# 파일별 최종 검사
###############################################################################

EXPECT_BINDDN="$(conf_get "infra.$INFRA.binddn")"
EXPECT_BINDPW="$(conf_get "infra.$INFRA.bindpw")"

# 결정된 사이트의 기대 URI 순서 (NONE 은 건너뜀)
EXPECT_URI_LINE=""
for k in $(conf_list "infra.$INFRA.site.$SITE.uri_order"); do
    v="$(conf_get "infra.$INFRA.$k")"
    [ -z "$v" ] && continue
    [ "$v" = "NONE" ] && continue
    if [ -z "$EXPECT_URI_LINE" ]; then
        EXPECT_URI_LINE="$v"
    else
        EXPECT_URI_LINE="$EXPECT_URI_LINE $v"
    fi
done

# 1. resolv.conf — 집합 비교 (순서 무관)
same_set "$(conf_list "infra.$INFRA.dns")" "$ACTUAL_DNS"
report $? resolv.conf

# 2. chrony.conf / ntp.conf — 집합 비교 (순서 무관)
same_set "$(conf_list "infra.$INFRA.ntp")" "$ACTUAL_NTP"
report $? "$(basename "$TIME_FILE")" "$TIME_KIND"

# 3. ldap.conf — URI 는 '순서까지' 엄격 비교, binddn/bindpw 는 완전 일치
if [ "$ACTUAL_URI_LINE" = "$EXPECT_URI_LINE" ] &&
   [ "$ACTUAL_BINDDN" = "$EXPECT_BINDDN" ] &&
   [ "$ACTUAL_BINDPW" = "$EXPECT_BINDPW" ]; then
    report 0 ldap.conf "$SITE"
else
    report 1 ldap.conf "기대순서=[$EXPECT_URI_LINE] 실제=[$ACTUAL_URI_LINE]"
fi

# 4. autofs.conf — ldap_uri 가 ldap.conf 와 같은 순서인지
AUTOFS_URI="$(sed -n 's/^[[:space:]]*ldap_uri[[:space:]]*=[[:space:]]*//p' \
                "$ROOT/etc/autofs.conf" 2>/dev/null | head -n1 | sed 's/^"//; s/"[[:space:]]*$//')"
[ "$AUTOFS_URI" = "$EXPECT_URI_LINE" ]
report $? autofs.conf

# 5. autofs_ldap_auth.conf — user / secret
AUTH_USER="$(sed -n 's/.*[[:space:]]user="\([^"]*\)".*/\1/p' \
                "$ROOT/etc/autofs_ldap_auth.conf" 2>/dev/null | head -n1)"
AUTH_SECRET="$(sed -n 's/.*[[:space:]]secret="\([^"]*\)".*/\1/p' \
                "$ROOT/etc/autofs_ldap_auth.conf" 2>/dev/null | head -n1)"
[ "$AUTH_USER" = "$EXPECT_BINDDN" ] && [ "$AUTH_SECRET" = "$EXPECT_BINDPW" ]
report $? autofs_ldap_auth.conf

# 6. nslcd.conf 또는 sssd.conf
if [ "$AUTH_KIND" = "nslcd" ]; then
    N_URI="$(awk 'tolower($1)=="uri" { $1=""; sub(/^[ \t]+/,""); print; exit }' "$ROOT$AUTH_FILE" 2>/dev/null)"
    N_DN="$(awk  'tolower($1)=="binddn" { $1=""; sub(/^[ \t]+/,""); print; exit }' "$ROOT$AUTH_FILE" 2>/dev/null)"
    N_PW="$(awk  'tolower($1)=="bindpw" { $1=""; sub(/^[ \t]+/,""); print; exit }' "$ROOT$AUTH_FILE" 2>/dev/null)"
    [ "$N_URI" = "$EXPECT_URI_LINE" ] && [ "$N_DN" = "$EXPECT_BINDDN" ] && [ "$N_PW" = "$EXPECT_BINDPW" ]
    report $? nslcd.conf
else
    ini_get()
    {
        awk -v want="$1" '
            /^[ \t]*\[/ { inSec = (index($0, "[domain/") == 1) ? 1 : 0; next }
            inSec {
                line = $0; sub(/^[ \t]+/, "", line)
                if (line ~ /^[#;]/) next
                split(line, a, "=")
                k = a[1]; gsub(/[ \t]+$/, "", k)
                if (k == want) {
                    sub(/^[^=]*=[ \t]*/, "", line)
                    print line; exit
                }
            }
        ' "$ROOT$AUTH_FILE" 2>/dev/null
    }
    S_URI="$(ini_get ldap_uri)"
    S_DN="$(ini_get ldap_default_bind_dn)"
    S_PW="$(ini_get ldap_default_authtok)"
    [ "$S_URI" = "$EXPECT_URI_LINE" ] && [ "$S_DN" = "$EXPECT_BINDDN" ] && [ "$S_PW" = "$EXPECT_BINDPW" ]
    report $? sssd.conf
fi

# 7. auto.appl — storage 와 mountpoint
EXPECT_STORAGE="$(conf_get "infra.$INFRA.site.$SITE.storage")"
EXPECT_MOUNT="$(conf_get "infra.$INFRA.site.$SITE.mountpoint")"
[ "$ACTUAL_STORAGE" = "$EXPECT_STORAGE" ] && [ "$ACTUAL_MOUNT" = "$EXPECT_MOUNT" ]
report $? auto.appl "$SITE"

# 8. auto.appl(/wappl) — 이 site 에 wappl_mount 가 설정된 경우에만 검사(선택 항목)
EXPECT_WAPPL_MOUNT="$(conf_get "infra.$INFRA.site.$SITE.wappl_mount")"
if [ -n "$EXPECT_WAPPL_MOUNT" ]; then
    [ "$ACTUAL_WAPPL_MOUNT" = "$EXPECT_WAPPL_MOUNT" ]
    report $? auto.appl-wappl "$SITE"
fi

if [ "$RC" = "0" ]; then
    echo "INFO	LDAP	$INFRA	$SITE"
else
    echo "FAIL	LDAP	UNDEFINED"
fi
exit $RC
