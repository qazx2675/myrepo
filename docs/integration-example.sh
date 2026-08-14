# integration-example.sh - 기존 작업 스크립트에 worklog를 붙이는 최소 예시
#
# 실제로는 이 파일을 복사하지 말고, 기존 스크립트 맨 위에 아래 2줄만 추가하고
# user 선택 로직 바로 다음 줄에 worklog_start 한 줄만 넣으면 됩니다.

# 1) 라이브러리 불러오기 (기존 스크립트 최상단 근처)
source "/path/to/worklog-tracker/lib/worklog.sh"

# ── 여기서부터는 기존 스크립트에 원래 있던 내용 ──────────────────────────

echo "사용자를 선택하세요:"
USERS=(kim lee park choi)
select SELECTED_USER in "${USERS[@]}"; do
    [[ -n "$SELECTED_USER" ]] && break
done

# 2) user 선택 직후, 딱 한 줄만 추가
worklog_start "$SELECTED_USER"

# ── 이 아래로 기존 작업 로직이 그대로 이어짐 (전혀 수정 불필요) ───────────
echo "여기서부터 원래 하던 vswitch 설정, spec 조회 등 기존 작업 실행..."
# ./do_vswitch_setup.sh "$SELECTED_USER"
# ./do_spec_list.sh "$SELECTED_USER"

# 3) (선택) 스크립트 맨 끝에서 바로 완료 처리할지 물어보고 싶다면 한 줄 추가
#    안 넣어도 무방 - 나중에 bin/worklog-finish.sh 로 언제든 처리 가능
worklog_finish_prompt "$SELECTED_USER"

# ── 참고: 터미널 전체 로그(명령+출력)까지 녹화하고 싶다면 ──────────────
# 기존 작업을 직접 실행하는 대신 아래처럼 감싸서 실행:
#
#   worklog_wrap_session "$SELECTED_USER" "$WORKLOG_CURRENT_INCIDENT" \
#       ./기존_작업_스크립트.sh "$SELECTED_USER"
#
# 이렇게 하면 logs/sessions/ 에 그 실행 구간의 전체 터미널 로그가 남습니다.
