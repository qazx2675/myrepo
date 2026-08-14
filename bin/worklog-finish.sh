#!/usr/bin/env bash
# worklog-finish.sh - 미완료 작업 중 하나를 선택해 완료 처리하는 독립 명령

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/worklog.sh
source "$SCRIPT_DIR/../lib/worklog.sh"

# ── user 선택 (기존 프로젝트의 select 목록과 동일하게 맞춰서 수정) ─────────
USERS=(kim lee park choi)  # 실제 사용자 목록으로 교체하세요

echo "사용자를 선택하세요:"
select sel_user in "${USERS[@]}"; do
    if [[ -n "$sel_user" ]]; then
        break
    fi
    echo "잘못된 선택입니다."
done

worklog_check_open "$sel_user"
