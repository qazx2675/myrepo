###############################################################################
# 설정 적용 본체. (Go 엔진이 위 헤더에 이 실행의 STAMP, 경로, HOST_MAP 을 붙입니다)
#
# 동작
#   1. 이 노드의 hostname 으로 HOST_MAP 에서 자신의 "변경될 IP" 를 찾음
#   2. 이 노드에 실제로 할당된 IPv4 주소 목록을 구함 (hosts/DNS 에 의존하지 않음 —
#      /etc/hosts 에 IPv4 항목이 없거나 IPv6 만 등록된 노드가 실제로 있었음)
#   3. NETWORK_SCRIPTS_DIR(또는 RHEL 9+ 이면 RHEL9_PATH) 안에서, IPADDR 값이
#      2번 목록에 있는 ifcfg-* 파일을 찾음(= 지금 서비스 중인 IP)
#   4. 그 파일을 .bak.<STAMP> 로 백업한 뒤 IPADDR/GATEWAY 두 줄만 갱신
#      (파일을 통째로 덮어쓰지 않음). 네트워크 서비스는 재시작하지 않음
#   5. 갱신 결과를 다시 읽어 검증
#
# 출력 형식 (한 줄, gossh 가 "<host>: " 를 앞에 붙임. "|" 구분 기계용 포맷 —
# 화면에 보이는 "hostname 기존IP -> 변경IP" 표시는 ip-change-engine(main.go) 이 만듭니다)
#   성공: RESULT|OK|<hostname>|<기존IP>|<변경IP>|<게이트웨이>
#   실패: RESULT|FAIL|<hostname>|<사유>
###############################################################################

STAMP="$(date +%Y%m%d%H%M%S)"
NODE_HOST="$(hostname -s 2>/dev/null || hostname)"
NODE_FQDN="$(hostname 2>/dev/null || echo "$NODE_HOST")"

# ROOT 는 테스트 fixture 용입니다(운영에서는 항상 비어 있음). OS 버전 판정에만
# 씁니다 — NETWORK_SCRIPTS_DIR/RHEL9_PATH 는 conf 로 이미 임의 경로 지정이 가능합니다.
ROOT="${ROOT:-}"

fail_out()
{
    echo "RESULT|FAIL|$NODE_HOST|$1"
    exit 1
}

#------------------------------------------------------------------------------
# 1. HOST_MAP 에서 이 노드의 변경될 IP 찾기 (hostname -s, hostname 둘 다 매칭 시도)
#------------------------------------------------------------------------------

NEW_IP=""
while read -r m_host m_ip; do
    [ -z "$m_host" ] && continue
    if [ "$m_host" = "$NODE_HOST" ] || [ "$m_host" = "$NODE_FQDN" ]; then
        NEW_IP="$m_ip"
        break
    fi
done <<'HOSTMAPEOF'
__HOST_MAP__
HOSTMAPEOF

if [ -z "$NEW_IP" ]; then
    fail_out "대상 목록에 이 hostname($NODE_HOST)이 없습니다"
fi

GATEWAY="$(echo "$NEW_IP" | awk -F. '{print $1"."$2"."$3".1"}')"

#------------------------------------------------------------------------------
# 2. 이 노드에 실제 할당된 IPv4 주소 목록 조회
#
#    getent hosts "$(hostname)" 로 알아내는 방식은 쓰지 않습니다 — /etc/hosts·DNS
#    설정에 좌우되어, 실제 랩에서 그 항목에 IPv4 가 아예 없고 IPv6 만 등록된
#    노드가 있었습니다(호스트 해석과 실제 인터페이스 주소가 다를 수 있음).
#    대신 인터페이스에 직접 물어봅니다.
#------------------------------------------------------------------------------

LOCAL_IPS="$(hostname -I 2>/dev/null | tr ' ' '\n' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$')"
if [ -z "$LOCAL_IPS" ]; then
    # hostname -I 를 지원하지 않는 구형 환경 대비
    LOCAL_IPS="$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1)"
fi
if [ -z "$LOCAL_IPS" ]; then
    fail_out "이 노드의 IPv4 주소를 확인할 수 없습니다 (hostname -I / ip addr 모두 실패)"
fi

#------------------------------------------------------------------------------
# 3. OS 버전에 따라 검색할 경로 결정
#------------------------------------------------------------------------------

detect_os_major()
{
    local v=""
    if [ -f "$ROOT/etc/redhat-release" ]; then
        v="$(sed -n 's/.*release \([0-9][0-9]*\).*/\1/p' "$ROOT/etc/redhat-release" | head -n1)"
    fi
    if [ -z "$v" ] && [ -f "$ROOT/etc/os-release" ]; then
        v="$(sed -n 's/^VERSION_ID="\{0,1\}\([0-9][0-9]*\).*/\1/p' "$ROOT/etc/os-release" | head -n1)"
    fi
    echo "$v"
}

SEARCH_DIR="$NETWORK_SCRIPTS_DIR"
OS_MAJOR="$(detect_os_major)"
if [ -n "$RHEL9_PATH" ] && [ -n "$OS_MAJOR" ] && [ "$OS_MAJOR" -ge 9 ] 2>/dev/null; then
    SEARCH_DIR="$RHEL9_PATH"
fi

if [ ! -d "$SEARCH_DIR" ]; then
    fail_out "설정 경로가 없습니다: $SEARCH_DIR"
fi

#------------------------------------------------------------------------------
# 4. LOCAL_IPS 중 하나가 등록된 ifcfg 파일 찾기 (= 지금 서비스 중인 IP)
#------------------------------------------------------------------------------

TARGET_FILE=""
CUR_IP=""
for f in "$SEARCH_DIR"/ifcfg-*; do
    [ -f "$f" ] || continue
    line="$(grep -i '^[[:space:]]*IPADDR[[:space:]]*=' "$f" | tail -n1)"
    [ -z "$line" ] && continue
    val="$(echo "$line" | cut -d= -f2- | tr -d '"'"'"' \t')"
    for ip in $LOCAL_IPS; do
        if [ "$val" = "$ip" ]; then
            if [ -n "$TARGET_FILE" ]; then
                fail_out "이 노드의 IPv4($val)가 등록된 ifcfg 파일이 둘 이상입니다: $TARGET_FILE, $f"
            fi
            TARGET_FILE="$f"
            CUR_IP="$val"
        fi
    done
done

if [ -z "$TARGET_FILE" ]; then
    fail_out "$SEARCH_DIR 에서 이 노드의 IPv4($(echo "$LOCAL_IPS" | tr '\n' ' '))와 일치하는 ifcfg 파일을 찾지 못했습니다"
fi

#------------------------------------------------------------------------------
# 5. 백업
#------------------------------------------------------------------------------

cp -p "$TARGET_FILE" "$TARGET_FILE.bak.$STAMP" || fail_out "백업 실패: $TARGET_FILE"

#------------------------------------------------------------------------------
# 6. IPADDR / GATEWAY 두 줄만 갱신 (해당 키로 시작하는 줄만 교체, 없으면 추가)
#------------------------------------------------------------------------------

set_ifcfg_key()
{
    local file="$1" key="$2" val="$3"
    local tmp; tmp="$(mktemp)"

    AWK_K="$key" AWK_V="$val" awk '
        BEGIN { k = ENVIRON["AWK_K"]; v = ENVIRON["AWK_V"]; done = 0 }
        {
            line = $0
            sub(/^[ \t]+/, "", line)
            first = line
            sub(/=.*/, "", first)
            if (tolower(first) == tolower(k)) {
                if (!done) { print k "=" v; done = 1 }
                next
            }
            print
        }
        END { if (!done) print k "=" v }
    ' "$file" > "$tmp"

    if [ ! -s "$tmp" ]; then
        rm -f "$tmp"
        fail_out "$key 갱신 결과가 비어 있어 반영하지 않았습니다: $file"
    fi

    cat "$tmp" > "$file" || fail_out "쓰기 실패: $file"
    rm -f "$tmp"
}

set_ifcfg_key "$TARGET_FILE" IPADDR "$NEW_IP"
set_ifcfg_key "$TARGET_FILE" GATEWAY "$GATEWAY"

#------------------------------------------------------------------------------
# 7. 검증 (재시작 없음)
#------------------------------------------------------------------------------

check_ip="$(grep -i '^[[:space:]]*IPADDR[[:space:]]*=' "$TARGET_FILE" | tail -n1 | cut -d= -f2- | tr -d '"'"'"' \t')"
check_gw="$(grep -i '^[[:space:]]*GATEWAY[[:space:]]*=' "$TARGET_FILE" | tail -n1 | cut -d= -f2- | tr -d '"'"'"' \t')"

if [ "$check_ip" != "$NEW_IP" ] || [ "$check_gw" != "$GATEWAY" ]; then
    fail_out "$TARGET_FILE 갱신 검증 실패 (IPADDR=$check_ip GATEWAY=$check_gw)"
fi

echo "RESULT|OK|$NODE_HOST|$CUR_IP|$NEW_IP|$GATEWAY"
exit 0
