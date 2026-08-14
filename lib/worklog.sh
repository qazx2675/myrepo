# worklog.sh - 사용자별 작업 로그 자동 기록 라이브러리 (source 전용)
#
# 사용법:
#   source /path/to/worklog.sh
#   worklog_start "$USER_SELECTED"      # 기존 스크립트 앞부분(user 선택 직후)에서 호출
#   ... 기존 작업 로직 ...
#   worklog_finish_prompt "$USER_SELECTED"   # (선택) 스크립트 끝에서 미완료건 완료여부 재확인
#
# 독립 명령:
#   bin/worklog-finish.sh   -> 아무 때나 미완료 건을 골라서 완료 처리
#   bin/worklog-status.sh   -> 현재 미완료 건 조회
#
# 설계 원칙:
#   - START는 스크립트 실행 시 자동/즉시 기록 (사람이 잊을 수 없음)
#   - FINISH(완료)는 반드시 사람이 명시적으로 처리해야만 기록됨 (자동 종료 감지 안 함 - 신뢰 불가)
#   - 미완료 건은 open/ 디렉토리에 파일로 존재 = 단일 진실 소스(source of truth)
#   - 미완료 건 여러 개 동시 존재 가능, 순서 강제 없음, 스킵 가능, 다음 실행 때 다시 물어봄(누락 방지)
#   - 동시 실행 안전성: user별/incident별로 파일명이 분리되고, all.log는 한 줄 단위 append로
#     원자성을 확보(락 불필요) - 여러 사용자가 같은 스크립트를 동시에 실행해도 안전

# ── 경로 설정 (필요시 이 부분만 환경에 맞게 수정) ──────────────────────────
WORKLOG_ROOT="${WORKLOG_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
WORKLOG_DIR="${WORKLOG_DIR:-$WORKLOG_ROOT/logs}"
WORKLOG_OPEN_DIR="$WORKLOG_DIR/open"
WORKLOG_USERS_DIR="$WORKLOG_DIR/users"
WORKLOG_ALL_LOG="$WORKLOG_DIR/all.log"
WORKLOG_ARTIFACTS_DIR="$WORKLOG_DIR/artifacts"
WORKLOG_SESSIONS_DIR="$WORKLOG_DIR/sessions"

# 완료 시점에 자동 수집할 산출물 파일 패턴 (필요시 수정/추가)
# {user} 는 실행 시 실제 사용자명으로 치환됨
WORKLOG_ARTIFACT_PATTERNS=(
    "vswitch_{user}.txt"
    "spec_list_{user}"
)
# 위 파일들이 존재할 것으로 예상되는 디렉토리들(여러 곳 검색 가능하도록 배열로)
WORKLOG_ARTIFACT_SEARCH_DIRS=(
    "."
    "$HOME"
)

# 인시던트 번호 형식 검증 정규식 (REQ + 숫자, 자릿수 미확정이라 유연하게 잡음)
# 정확한 자릿수가 확정되면 여기만 수정: 예) ^REQ[0-9]{7}$
WORKLOG_INCIDENT_REGEX='^REQ[0-9]+$'

mkdir -p "$WORKLOG_OPEN_DIR" "$WORKLOG_USERS_DIR" "$WORKLOG_ARTIFACTS_DIR" "$WORKLOG_SESSIONS_DIR"

# ── 내부 유틸 ──────────────────────────────────────────────────────────────

_worklog_now() {
    date '+%Y-%m-%d %H:%M:%S'
}

_worklog_now_compact() {
    date '+%Y%m%d_%H%M%S'
}

# all.log와 개별 user 로그에 한 줄씩 원자적으로 append
# 인자: user, incident, event(START|END), extra(선택)
_worklog_append() {
    local user="$1" incident="$2" event="$3" extra="${4:-}"
    local line="$(_worklog_now)|${user}|${incident}|${event}|${extra}"
    # 한 줄을 한 번의 echo로 append -> OS 레벨에서 원자적 (O_APPEND), 락 불필요
    echo "$line" >> "$WORKLOG_ALL_LOG"
    echo "$line" >> "$WORKLOG_USERS_DIR/${user}.txt"
}

# 인시던트 번호 입력받고 형식 검증될 때까지 재입력 요구
_worklog_read_incident() {
    local incident=""
    while true; do
        read -rp "인시던트 번호 입력 (예: REQ1234567): " incident
        if [[ -z "$incident" ]]; then
            echo "  [!] 인시던트 번호는 비워둘 수 없습니다." >&2
            continue
        fi
        if [[ ! "$incident" =~ $WORKLOG_INCIDENT_REGEX ]]; then
            echo "  [!] 형식이 올바르지 않습니다. (기대 형식: REQ로 시작 + 숫자)" >&2
            continue
        fi
        break
    done
    echo "$incident"
}

# 특정 user의 미완료(open) 건 파일 목록을 시간순으로 출력
_worklog_list_open() {
    local user="$1"
    find "$WORKLOG_OPEN_DIR" -maxdepth 1 -type f -name "${user}_*.open" 2>/dev/null | sort
}

# open 파일에서 필드 읽기 (key=value 형식)
_worklog_open_field() {
    local file="$1" key="$2"
    grep "^${key}=" "$file" 2>/dev/null | head -1 | cut -d'=' -f2-
}

# 완료 시점에 산출물 파일들을 찾아서 artifacts 디렉토리로 복사
# 인자: user, incident, dest_dir
_worklog_collect_artifacts() {
    local user="$1" incident="$2" dest_dir="$3"
    mkdir -p "$dest_dir"
    local pattern found_path found_any=0
    for pattern in "${WORKLOG_ARTIFACT_PATTERNS[@]}"; do
        local fname="${pattern/\{user\}/$user}"
        found_path=""
        local d
        for d in "${WORKLOG_ARTIFACT_SEARCH_DIRS[@]}"; do
            if [[ -f "$d/$fname" ]]; then
                found_path="$d/$fname"
                break
            fi
        done
        if [[ -n "$found_path" ]]; then
            cp -p "$found_path" "$dest_dir/" 2>/dev/null && found_any=1
            echo "  [수집] $fname -> $dest_dir/"
        else
            echo "  [없음] $fname (검색 경로에서 못 찾음, 스킵)" >> "$dest_dir/_missing_artifacts.log"
        fi
    done
    return 0
}

# ── 공개 함수 ──────────────────────────────────────────────────────────────

# worklog_start <user>
# 기존 스크립트에서 user 선택 직후 호출. 다음을 수행:
#   1) 그 user의 기존 미완료건이 있으면 안내 + 완료 처리 여부 선택(스킵 가능, 강제 아님)
#   2) 새 인시던트 번호 입력받아 검증
#   3) START 기록 + open 파일 생성 + 터미널 세션 로깅 시작
# 반환: WORKLOG_CURRENT_INCIDENT, WORKLOG_CURRENT_OPEN_FILE 전역변수에 설정됨
worklog_start() {
    local user="$1"
    if [[ -z "$user" ]]; then
        echo "[worklog] 오류: worklog_start에 user를 전달해야 합니다." >&2
        return 1
    fi

    echo "======================================"
    echo " 작업 로그 시작 - user: $user"
    echo "======================================"

    # 1) 미완료 건 확인 (있어도 강제 아님, 스킵 가능)
    worklog_check_open "$user"

    # 2) 새 작업의 인시던트 번호
    local incident
    incident="$(_worklog_read_incident)"

    # 3) START 기록
    local ts_compact
    ts_compact="$(_worklog_now_compact)"
    local open_file="$WORKLOG_OPEN_DIR/${user}_${incident}_${ts_compact}.open"

    {
        echo "user=${user}"
        echo "incident=${incident}"
        echo "start_time=$(_worklog_now)"
        echo "open_file=${open_file}"
    } > "$open_file"

    _worklog_append "$user" "$incident" "START" ""

    export WORKLOG_CURRENT_USER="$user"
    export WORKLOG_CURRENT_INCIDENT="$incident"
    export WORKLOG_CURRENT_OPEN_FILE="$open_file"

    echo "[worklog] START 기록됨: user=$user incident=$incident"
    echo "[worklog] 이 작업을 완료하면 나중에 아래 중 하나로 완료 처리하세요:"
    echo "          - bin/worklog-finish.sh 실행"
    echo "          - 다음 스크립트 실행 시 자동으로 물어봄"
    echo "======================================"
}

# worklog_check_open <user>
# 해당 user의 미완료 건들을 목록으로 보여주고, 완료 처리할지/스킵할지 선택받음.
# 여러 건이어도 하나만 선택해서 처리(순서 강제 없음), 0(스킵) 선택 시 아무것도 안 함.
# 새 작업 시작을 막지 않음 - 독립적으로 동작.
worklog_check_open() {
    local user="$1"
    local -a open_files
    mapfile -t open_files < <(_worklog_list_open "$user")

    if [[ ${#open_files[@]} -eq 0 ]]; then
        return 0
    fi

    echo "--------------------------------------"
    echo "[worklog] '$user'의 미완료 작업이 ${#open_files[@]}건 있습니다:"
    local i=1
    local -a incidents
    for f in "${open_files[@]}"; do
        local inc start_t
        inc="$(_worklog_open_field "$f" "incident")"
        start_t="$(_worklog_open_field "$f" "start_time")"
        echo "  $i) $inc  (시작: $start_t)"
        incidents[$i]="$f"
        ((i++))
    done
    echo "  0) 지금은 넘어가기 (나중에 처리, 계속 미완료로 유지됨)"
    echo "--------------------------------------"

    local choice
    read -rp "완료 처리할 번호 선택 [0-$((i-1))]: " choice

    if [[ "$choice" == "0" || -z "$choice" ]]; then
        echo "[worklog] 스킵함. 미완료 건은 계속 남아있습니다."
        return 0
    fi

    if [[ -z "${incidents[$choice]:-}" ]]; then
        echo "[worklog] 잘못된 선택입니다. 스킵합니다."
        return 0
    fi

    _worklog_do_finish "${incidents[$choice]}"
}

# worklog_finish_prompt <user>
# (선택) 기존 스크립트 맨 끝에서 호출하면, 방금 시작한 건을 바로 완료 처리할지 물어봄.
# 호출 안 해도 무방 - 완료 처리는 언제든 bin/worklog-finish.sh로 가능.
worklog_finish_prompt() {
    local user="${1:-$WORKLOG_CURRENT_USER}"
    local open_file="${WORKLOG_CURRENT_OPEN_FILE:-}"

    if [[ -z "$open_file" || ! -f "$open_file" ]]; then
        return 0
    fi

    echo "--------------------------------------"
    read -rp "[worklog] 방금 작업(${WORKLOG_CURRENT_INCIDENT})을 지금 완료 처리하시겠습니까? [y/N]: " ans
    if [[ "$ans" =~ ^[Yy]$ ]]; then
        _worklog_do_finish "$open_file"
    else
        echo "[worklog] 완료 처리하지 않음. 나중에 bin/worklog-finish.sh 로 처리하세요."
    fi
}

# 내부: 실제 완료 처리 로직 (open_file 하나를 받아 종료 기록 + artifacts 수집)
_worklog_do_finish() {
    local open_file="$1"
    if [[ ! -f "$open_file" ]]; then
        echo "[worklog] 오류: open 파일을 찾을 수 없습니다: $open_file" >&2
        return 1
    fi

    local user incident start_time
    user="$(_worklog_open_field "$open_file" "user")"
    incident="$(_worklog_open_field "$open_file" "incident")"
    start_time="$(_worklog_open_field "$open_file" "start_time")"

    # 완료 시각: 기본값 지금, 수동 입력도 허용("사실 아까 끝났다" 케이스)
    local end_time
    read -rp "완료 시각 입력 [Enter=지금 / YYYY-MM-DD HH:MM:SS 직접입력]: " end_time_input
    if [[ -z "$end_time_input" ]]; then
        end_time="$(_worklog_now)"
    else
        end_time="$end_time_input"
    fi

    local elapsed=""
    if command -v date >/dev/null 2>&1; then
        local s e
        s=$(date -d "$start_time" +%s 2>/dev/null)
        e=$(date -d "$end_time" +%s 2>/dev/null)
        if [[ -n "$s" && -n "$e" && "$e" -ge "$s" ]]; then
            local diff=$((e - s))
            elapsed="elapsed=$((diff/3600))h$(((diff%3600)/60))m"
        fi
    fi

    _worklog_append "$user" "$incident" "END" "end_time=${end_time};${elapsed}"

    # 산출물 수집
    local dest_dir="$WORKLOG_ARTIFACTS_DIR/${user}_${incident}_$(_worklog_now_compact)"
    echo "[worklog] 산출물 수집 중..."
    _worklog_collect_artifacts "$user" "$incident" "$dest_dir"

    rm -f "$open_file"
    echo "[worklog] 완료 처리됨: user=$user incident=$incident ($elapsed)"
}

# worklog_wrap_session <user> <incident> <실행할 명령...>
# (선택) 터미널 전체 실행 로그(명령+출력)를 script(1)으로 녹화하고 싶을 때 사용.
# 예: worklog_wrap_session "$user" "$WORKLOG_CURRENT_INCIDENT" ./기존작업.sh
worklog_wrap_session() {
    local user="$1" incident="$2"
    shift 2
    local session_log="$WORKLOG_SESSIONS_DIR/${user}_${incident}_$(_worklog_now_compact).log"

    if ! command -v script >/dev/null 2>&1; then
        echo "[worklog] 경고: script 명령을 찾을 수 없어 세션 녹화 없이 실행합니다." >&2
        "$@"
        return $?
    fi

    echo "[worklog] 터미널 세션 녹화 시작 -> $session_log"
    # -q: quiet, -c: 실행할 명령, -e: 종료코드 그대로 반환 (util-linux script 기준)
    script -q -e -c "$(printf '%q ' "$@")" "$session_log"
}

# worklog_status [user]
# 미완료 건 전체(또는 특정 user) 조회용
worklog_status() {
    local user="${1:-}"
    local pattern="*.open"
    [[ -n "$user" ]] && pattern="${user}_*.open"

    local -a files
    mapfile -t files < <(find "$WORKLOG_OPEN_DIR" -maxdepth 1 -type f -name "$pattern" 2>/dev/null | sort)

    if [[ ${#files[@]} -eq 0 ]]; then
        echo "미완료 작업이 없습니다."
        return 0
    fi

    printf "%-12s %-16s %-20s\n" "USER" "INCIDENT" "START_TIME"
    printf "%-12s %-16s %-20s\n" "----" "--------" "----------"
    for f in "${files[@]}"; do
        local u i s
        u="$(_worklog_open_field "$f" "user")"
        i="$(_worklog_open_field "$f" "incident")"
        s="$(_worklog_open_field "$f" "start_time")"
        printf "%-12s %-16s %-20s\n" "$u" "$i" "$s"
    done
}
