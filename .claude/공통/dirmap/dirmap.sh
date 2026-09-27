#!/bin/bash
# 자주 사용하는 폴더 구조를 tree 형태로 출력하는 스크립트

# ===== 설정 (이곳에서 변수 수정) =====
TARGET_PATH="/home/saccae"           # 탐색할 기본 경로
MAX_DEPTH="2"                        # 탐색 깊이 (1 이상)
EXCLUDE_PATTERN="\.git|\.cache"      # 제외할 폴더명 (정규식, egrep -E 형식)
                                     # 예: "\.git|\.cache"      (폴더명에 .git 또는 .cache 포함)
                                     #     "^test_"             (test_로 시작)
                                     #     "source$"            (source로 끝남)
                                     #     ""                   (제외 없음)
# ===================================

# 사용법 (아래는 선택사항 - 명령행 인자로도 덮어쓸 수 있음):
#   ./dirmap.sh                         (위 변수값으로 실행)
#   ./dirmap.sh <경로> <깊이> [제외패턴]  (인자로 덮어쓰기)

set -euo pipefail

# 명령행 인자로 덮어쓰기 (선택사항)
if [[ $# -gt 0 ]]; then
    TARGET_PATH="${1//$'\r'/}"
fi
if [[ $# -gt 1 ]]; then
    MAX_DEPTH="${2//$'\r'/}"
fi
if [[ $# -gt 2 ]]; then
    EXCLUDE_PATTERN="${3//$'\r'/}"
fi

BASE_INPUT="$TARGET_PATH"

if [[ ! -d "$BASE_INPUT" ]]; then
    echo "에러: '$BASE_INPUT' 는 존재하지 않는 디렉토리입니다." >&2
    exit 1
fi

if ! [[ "$MAX_DEPTH" =~ ^[0-9]+$ ]] || [[ "$MAX_DEPTH" -lt 1 ]]; then
    echo "에러: 깊이는 1 이상의 정수여야 합니다." >&2
    exit 1
fi

BASE_PATH="$(realpath "$BASE_INPUT")"

# 폴더 설명 등록 (키: 절대경로, 값: 설명)
declare -A FOLDER_DESC=(
    ["/home/saccae/VMsetup"]="VM 자동 구성 스크립트 모음"
)

get_description() {
    echo "${FOLDER_DESC[$1]:-}"
}

is_excluded() {
    local path="$1"
    local name="$(basename "$path")"
    [[ -z "$EXCLUDE_PATTERN" ]] && return 1
    egrep -q "$EXCLUDE_PATTERN" <<< "$name"
}

print_tree() {
    local dir="$1"
    local depth="$2"
    local prefix="$3"

    if (( depth > MAX_DEPTH )); then
        return
    fi

    local entries=()
    while IFS= read -r d; do
        [[ -n "$d" ]] && entries+=("$d")
    done < <(find "$dir" -mindepth 1 -maxdepth 1 -type d | sort)

    local filtered=()
    for e in "${entries[@]}"; do
        is_excluded "$e" || filtered+=("$e")
    done

    local count=${#filtered[@]}
    local i=0
    for e in "${filtered[@]}"; do
        i=$((i+1))
        local name connector child_prefix desc
        name="$(basename "$e")"

        if (( i == count )); then
            connector="└──"
            child_prefix="${prefix}    "
        else
            connector="├──"
            child_prefix="${prefix}│   "
        fi

        echo "${prefix}${connector} ${name}"

        desc="$(get_description "$e")"
        [[ -n "$desc" ]] && echo "${child_prefix}└ : ${desc}"

        print_tree "$e" $((depth + 1)) "$child_prefix"
    done
}

echo "$BASE_PATH"
print_tree "$BASE_PATH" 1 ""
