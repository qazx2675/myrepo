###############################################################################
# 공통 라이브러리 — apply_body.sh 와 rollback_body.sh 양쪽에서 공유합니다.
#
# 환경변수
#   ROOT   기록 대상 루트. 기본 "" (= 실제 /etc). 테스트 시 /tmp/fixture 등을 넣음
#   DRYRUN 1 이면 아무것도 바꾸지 않고 무엇이 바뀔지만 보고
###############################################################################

ROOT="${ROOT:-}"
DRYRUN="${DRYRUN:-0}"

CHANGED=""
FAILED=0

log()  { echo "APPLY|$1|$2"; }
fail() { echo "APPLY|FAIL|$1"; FAILED=1; }

changed_has()
{
    case " $CHANGED " in
        *" $1 "*) return 0 ;;
    esac
    return 1
}

mark_changed()
{
    changed_has "$1" || CHANGED="$CHANGED $1"
}

NODE_HOST="$(hostname -s 2>/dev/null || hostname)"

###############################################################################
# autofs 서비스 리로드/재시작
#
# /etc/auto.master 또는 /etc/auto.home 이 실제로 변경(또는 복원)되었을 때만 호출합니다.
# - DRYRUN=1 이면 WOULD-RESTART 로그만 남김
# - ROOT 가 지정된 테스트 모드에서는 실제 시스템 서비스를 절대 건드리지 않음
# - systemctl 이 있으면 active 상태일 때 reload(실패 시 restart), 비활성이지만 enabled 면 restart
# - 구형(RHEL6 등) 환경이면 service autofs reload(실패 시 restart) 폴백
###############################################################################

reload_or_restart_autofs()
{
    local svc="autofs"

    if ! changed_has /etc/auto.master && ! changed_has /etc/auto.home; then
        return 0
    fi

    if [ "$DRYRUN" = "1" ]; then
        log WOULD-RESTART "$svc"
        return 0
    fi

    if [ -n "$ROOT" ]; then
        log SKIP-RESTART "$svc (ROOT=$ROOT 테스트 모드)"
        return 0
    fi

    if command -v systemctl >/dev/null 2>&1; then
        if systemctl is-active "$svc" >/dev/null 2>&1; then
            if systemctl reload "$svc" >/dev/null 2>&1 || systemctl restart "$svc" >/dev/null 2>&1; then
                log RESTARTED "$svc"
            else
                fail "서비스 리로드/재시작 실패: $svc"
            fi
            return 0
        fi
        if systemctl is-enabled "$svc" >/dev/null 2>&1; then
            if systemctl restart "$svc" >/dev/null 2>&1; then
                log RESTARTED "$svc"
            else
                fail "서비스 재시작 실패: $svc"
            fi
            return 0
        fi
        log SKIP-RESTART "$svc (비활성 상태)"
        return 0
    fi

    if command -v service >/dev/null 2>&1; then
        if service "$svc" status >/dev/null 2>&1; then
            if service "$svc" reload >/dev/null 2>&1 || service "$svc" restart >/dev/null 2>&1; then
                log RESTARTED "$svc"
            else
                fail "서비스 리로드/재시작 실패: $svc"
            fi
            return 0
        fi
    fi

    log SKIP-RESTART "$svc (미설치 또는 비활성)"
    return 0
}
