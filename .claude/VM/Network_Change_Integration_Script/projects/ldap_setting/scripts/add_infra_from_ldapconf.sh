#!/bin/bash
###############################################################################
# add_infra_from_ldapconf.sh — 기존 ldap.conf 에서 URI/BINDDN/BINDPW 를 읽어
# conf/ldap_config.conf 에 새 infra.<이름>.* 블록을 이어붙입니다.
#
# ldap.conf 에는 dns/ntp/site/storage/mountpoint 정보가 없으므로, 그 부분은
# TODO 로 표시만 해 두고 실행 후 반드시 사용자가 직접 채워야 합니다. 즉 이
# 스크립트는 "설정값 전체 자동 생성"이 아니라 "URI/BINDDN/BINDPW 타이핑을
# 줄여주는 보조 도구"입니다.
#
# 사용법
#   ./add_infra_from_ldapconf.sh <인프라이름> <ldap.conf 경로> [conf 경로]
#
#   예) ./add_infra_from_ldapconf.sh newsite /etc/openldap/ldap.conf
#       ./add_infra_from_ldapconf.sh newsite /etc/openldap/ldap.conf ../conf/ldap_config.conf
#
# 동작
#   1. ldap.conf 에서 URI(최대 3개) / BINDDN / BINDPW 를 읽습니다.
#   2. conf 파일(기본 ../conf/ldap_config.conf)에 이미 같은 인프라 이름이
#      있으면 중복 등록 사고를 막기 위해 거부합니다.
#   3. infra.<이름>.dns / .ntp / site.* 는 ldap.conf 에서 알 수 없으므로
#      TODO 주석과 함께 자리만 만들어 둡니다.
###############################################################################

set -u
cd "$(dirname "$0")" || exit 2

INFRA="${1:-}"
LDAP_CONF="${2:-}"
CONF="${3:-../conf/ldap_config.conf}"

if [ -z "$INFRA" ] || [ -z "$LDAP_CONF" ]; then
    echo "사용법: $0 <인프라이름> <ldap.conf 경로> [conf 경로]"
    echo "  예)   $0 newsite /etc/openldap/ldap.conf"
    exit 2
fi

# 인프라 이름은 conf 의 key 조각(infra.<이름>.xxx)으로 그대로 쓰이므로
# '.' 이 들어가면 키 구조가 깨집니다.
case "$INFRA" in
    *.*)
        echo "오류: 인프라 이름에 '.' 을 쓸 수 없습니다: $INFRA"
        exit 2
        ;;
esac

if [ ! -f "$LDAP_CONF" ]; then
    echo "오류: ldap.conf 를 찾을 수 없습니다: $LDAP_CONF"
    exit 2
fi

if [ ! -f "$CONF" ]; then
    echo "오류: 설정 파일이 없습니다: $CONF"
    echo "      ldap_config.conf.sample 을 복사해 먼저 만들어 두십시오."
    exit 2
fi

if grep -q "^infra\.${INFRA}\." "$CONF"; then
    echo "오류: '$INFRA' 인프라가 $CONF 에 이미 있습니다. 중복 등록을 막기 위해 중단합니다."
    echo "      다른 이름을 쓰거나, 기존 블록을 직접 지우고 다시 실행하십시오."
    exit 1
fi

###############################################################################
# ldap.conf 파싱
###############################################################################

URI_LINE="$(awk 'tolower($1) == "uri" { $1=""; sub(/^[ \t]+/,""); print; exit }' "$LDAP_CONF")"
BINDDN="$(awk 'tolower($1) == "binddn" { $1=""; sub(/^[ \t]+/,""); print; exit }' "$LDAP_CONF")"
BINDPW="$(awk 'tolower($1) == "bindpw" { $1=""; sub(/^[ \t]+/,""); print; exit }' "$LDAP_CONF")"

if [ -z "$URI_LINE" ]; then
    echo "오류: $LDAP_CONF 에서 URI 를 찾지 못했습니다."
    exit 1
fi

# URI 는 공백으로 여러 개 나열될 수 있습니다. 최대 3개까지만 씁니다
# (ldap_config.conf 스키마가 uri1~uri3 까지만 지원).
set -- $URI_LINE
URI1="${1:-}"
URI2="${2:-NONE}"
URI3="${3:-NONE}"

if [ -z "$URI1" ]; then
    echo "오류: URI 값을 하나도 읽지 못했습니다."
    exit 1
fi
if [ "$#" -gt 3 ]; then
    echo "경고: URI 가 4개 이상 있어 앞의 3개만 씁니다: $URI_LINE"
fi

if [ -z "$BINDDN" ] || [ -z "$BINDPW" ]; then
    echo "경고: ldap.conf 에 BINDDN 또는 BINDPW 가 없습니다 — TODO 로 남겨둡니다."
    echo "      (원래 값이 있던 노드의 ldap.conf 를 넣으면 자동으로 채워집니다.)"
fi
: "${BINDDN:=TODO_BINDDN}"
: "${BINDPW:=TODO_BINDPW}"

###############################################################################
# conf 에 이어붙이기
###############################################################################

{
    echo ""
    echo "###############################################################################"
    echo "# INFRA : $INFRA  (add_infra_from_ldapconf.sh 로 $LDAP_CONF 에서 자동 생성, $(date +%Y-%m-%d))"
    echo "#"
    echo "# dns/ntp 와 site.*(storage/mountpoint/wappl_mount) 는 ldap.conf 에 없는 값이라"
    echo "# 아래 TODO 를 반드시 실제 값으로 채우십시오. 채우기 전에는 검증(config.Load)이"
    echo "# 실패합니다(dns/ntp/storage/mountpoint 필수)."
    echo "###############################################################################"
    echo ""
    echo "infra.${INFRA}.dns    = TODO_DNS1, TODO_DNS2"
    echo "infra.${INFRA}.ntp    = TODO_NTP1, TODO_NTP2"
    echo ""
    echo "infra.${INFRA}.uri1   = ${URI1}"
    echo "infra.${INFRA}.uri2   = ${URI2}"
    echo "infra.${INFRA}.uri3   = ${URI3}"
    echo ""
    echo "infra.${INFRA}.binddn = ${BINDDN}"
    echo "infra.${INFRA}.bindpw = ${BINDPW}"
    echo ""
    echo "# site 는 최소 1개 필요합니다. storage 는 이 인프라 안에서 고유해야 합니다."
    echo "infra.${INFRA}.site.TODO_SITE.uri_order   = uri1,uri2,uri3"
    echo "infra.${INFRA}.site.TODO_SITE.storage     = TODO_STORAGE"
    echo "infra.${INFRA}.site.TODO_SITE.mountpoint  = TODO_MOUNTPOINT"
    echo "# wappl_mount 는 선택 항목입니다 — 이 사이트에 /wappl 이 없으면 아예 지우십시오."
    echo "infra.${INFRA}.site.TODO_SITE.wappl_mount = TODO_WAPPL_MOUNTPOINT"
} >> "$CONF"

echo "완료: $CONF 에 infra.${INFRA}.* 블록을 추가했습니다."
echo "  URI    : uri1=${URI1} uri2=${URI2} uri3=${URI3}"
echo "  BINDDN : ${BINDDN}"
[ "$BINDDN" = "TODO_BINDDN" ] && echo "           ↑ ldap.conf 에 없어 TODO 로 남겼습니다. 채워 넣으십시오."
echo
echo "다음을 반드시 확인/수정하십시오 (TODO_ 로 검색):"
echo "  grep -n 'TODO_' $CONF"
echo "  - dns / ntp: 이 인프라를 판별할 실제 DNS·NTP 주소"
echo "  - site 이름(TODO_SITE)과 storage/mountpoint: 실제 사이트 이름과 값으로 교체"
echo "  - wappl_mount: 이 사이트에 /wappl 이 없으면 그 줄 자체를 지우십시오"
