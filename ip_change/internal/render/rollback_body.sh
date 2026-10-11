###############################################################################
# 롤백 본체. (Go 엔진이 위 헤더에 NETWORK_SCRIPTS_DIR / RHEL9_PATH / ROLLBACK_TO 를 붙입니다)
#
# 동작
#   1. OS 버전으로 검색 경로 결정 (apply_body.sh 와 동일한 규칙)
#   2. 그 경로의 ifcfg-* 중 "<ifcfg>.bak.<STAMP>" 백업이 있는 파일을 찾음
#        - ROLLBACK_TO 가 지정되면 그 STAMP 백업만
#        - 아니면 STAMP 가 가장 큰(가장 최근) 백업
#   3. 최신 STAMP 를 가진 ifcfg 가 둘 이상이면 실패 (모호한 복원 방지 — -rollback-to 로 지정)
#   4. 백업 내용을 원래 ifcfg 로 되돌리고 IPADDR 로 검증. 네트워크 재시작 없음.
#      백업 파일(.bak.*)은 지우지 않고 남깁니다.
#
# 출력 형식 (한 줄, gossh 가 "<host>: " 를 앞에 붙임)
#   성공: RESULT|OK|<hostname>|<ifcfg>|<백업STAMP>|<복원된IPADDR>
#   실패: RESULT|FAIL|<hostname>|<사유>
###############################################################################

NODE_HOST="$(hostname -s 2>/dev/null || hostname)"

# ROOT 는 테스트 fixture 용입니다(운영에서는 항상 비어 있음). OS 버전 판정에만 씁니다.
ROOT="${ROOT:-}"

fail_out()
{
    echo "RESULT|FAIL|$NODE_HOST|$1"
    exit 1
}

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
# 백업 후보 수집  (STAMP<TAB>ifcfg<TAB>bak)
#------------------------------------------------------------------------------

CAND="$(mktemp)" || fail_out "임시파일 생성 실패"
trap 'rm -f "$CAND"' EXIT

for f in "$SEARCH_DIR"/ifcfg-*; do
    [ -f "$f" ] || continue
    case "$f" in *.bak.*) continue ;; esac
    for b in "$f".bak.*; do
        [ -f "$b" ] || continue
        stamp="${b##*.bak.}"
        case "$stamp" in ''|*[!0-9]*) continue ;; esac
        if [ -n "$ROLLBACK_TO" ] && [ "$stamp" != "$ROLLBACK_TO" ]; then
            continue
        fi
        printf '%s\t%s\t%s\n' "$stamp" "$f" "$b" >>"$CAND"
    done
done

if [ ! -s "$CAND" ]; then
    if [ -n "$ROLLBACK_TO" ]; then
        fail_out "STAMP=$ROLLBACK_TO 백업을 찾지 못했습니다: $SEARCH_DIR"
    fi
    fail_out "복원할 백업(.bak.*)이 없습니다: $SEARCH_DIR"
fi

TOP_STAMP="$(cut -f1 "$CAND" | sort -rn | head -n1)"
N="$(awk -F'\t' -v s="$TOP_STAMP" '$1==s' "$CAND" | wc -l)"
if [ "$N" -ne 1 ]; then
    fail_out "가장 최근 백업 STAMP($TOP_STAMP)를 가진 ifcfg 가 여러 개입니다 — -rollback-to 로 지정하십시오"
fi

LINE="$(awk -F'\t' -v s="$TOP_STAMP" '$1==s' "$CAND")"
IFCFG="$(printf '%s\n' "$LINE" | cut -f2)"
BAK="$(printf '%s\n' "$LINE" | cut -f3)"

#------------------------------------------------------------------------------
# 복원 + 검증
#------------------------------------------------------------------------------

cat "$BAK" > "$IFCFG" || fail_out "복원 쓰기 실패: $IFCFG"

now_ip="$(grep -i '^[[:space:]]*IPADDR[[:space:]]*=' "$IFCFG" | tail -n1 | cut -d= -f2- | tr -d '"'"'"' \t')"
bak_ip="$(grep -i '^[[:space:]]*IPADDR[[:space:]]*=' "$BAK"   | tail -n1 | cut -d= -f2- | tr -d '"'"'"' \t')"

if [ "$now_ip" != "$bak_ip" ]; then
    fail_out "복원 검증 실패 ($IFCFG 의 IPADDR=$now_ip, 기대=$bak_ip)"
fi

echo "RESULT|OK|$NODE_HOST|$IFCFG|$TOP_STAMP|$now_ip"
exit 0
