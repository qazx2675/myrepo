#!/usr/bin/env bash
# worklog-status.sh - 현재 미완료 작업 목록 조회 (전체 또는 특정 user)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/worklog.sh
source "$SCRIPT_DIR/../lib/worklog.sh"

# 사용법: worklog-status.sh [user]
worklog_status "${1:-}"
