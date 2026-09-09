###############################################################################
# 설정 적용 본체. (Go 엔진이 위 헤더에 이 실행의 STAMP, 경로, HOST_MAP 을 붙입니다)
#
# 동작
#   1. 이 노드의 hostname 으로 HOST_MAP 에서 자신의 "변경될 IP" 를 찾음
#   2. getent hosts 로 "현재 서비스 IP" 를 구함
#   3. NETWORK_SCRIPTS_DIR(또는 RHEL 9+ 이면 RHEL9_PATH) 안에서
#      IPADDR=<현재 서비스 IP> 인 ifcfg-* 파일을 찾음
#   4. 그 파일을 .bak.<STAMP> 로 백업한 뒤 IPADDR/GATEWAY 두 줄만 갱신
#      (파일을 통째로 덮어쓰지 않음). 네트워크 서비스는 재시작하지 않음
#   5. 갱신 결과를 다시 읽어 검증
#
# 출력 형식 (한 줄, gossh 가 "<host>: " 를 앞에 붙임)
#   성공: <hostname> <새IP> <게이트웨이>
#   실패: <hostname> FAIL <사유>
###############################################################################

STAMP="$(date +%Y%m%d%H%M%S)"
NODE_HOST="$(hostname -s 2>/dev/null || hostname)"
NODE_FQDN="$(hostname 2>/dev/null || echo "$NODE_HOST")"

# ROOT 는 테스트 fixture 용입니다(운영에서는 항상 비어 있음). OS 버전 판정에만
# 씁니다 — NETWORK_SCRIPTS_DIR/RHEL9_PATH 는 conf 로 이미 임의 경로 지정이 가능합니다.
ROOT="${ROOT:-}"

fail_out()
{
    echo "$NODE_HOST FAIL $1"
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
# 2. 현재 서비스 IP 조회
#------------------------------------------------------------------------------

CUR_IP="$(getent hosts "$NODE_FQDN" 2>/dev/null | awk '{print $1; exit}')"
if [ -z "$CUR_IP" ]; then
    CUR_IP="$(getent hosts "$NODE_HOST" 2>/dev/null | awk '{print $1; exit}')"
fi
if [ -z "$CUR_IP" ]; then
    fail_out "getent hosts 로 현재 서비스 IP 를 확인할 수 없습니다"
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
# 4. 현재 서비스 IP 가 등록된 ifcfg 파일 찾기
#------------------------------------------------------------------------------

TARGET_FILE=""
for f in "$SEARCH_DIR"/ifcfg-*; do
    [ -f "$f" ] || continue
    line="$(grep -i '^[[:space:]]*IPADDR[[:space:]]*=' "$f" | tail -n1)"
    [ -z "$line" ] && continue
    val="$(echo "$line" | cut -d= -f2- | tr -d '"'"'"' \t')"
    if [ "$val" = "$CUR_IP" ]; then
        if [ -n "$TARGET_FILE" ]; then
            fail_out "IPADDR=$CUR_IP 가 등록된 파일이 둘 이상입니다: $TARGET_FILE, $f"
        fi
        TARGET_FILE="$f"
    fi
done

if [ -z "$TARGET_FILE" ]; then
    fail_out "$SEARCH_DIR 에서 IPADDR=$CUR_IP 인 ifcfg 파일을 찾지 못했습니다"
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

echo "$NODE_HOST $NEW_IP $GATEWAY"
exit 0
