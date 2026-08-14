#!/bin/bash
# fix_truncated.sh - 엑셀 붙여넣기에서 첫 줄 앞부분이 짤린 경우만 감지 및 수정
# 감지 방식: 2~6번째 줄의 공통 접두어(알파벳 부분)를 구하고,
# 첫 줄의 접두어가 그 공통 접두어의 "뒷부분(suffix)"과 일치하면 앞쪽 글자가 잘린 것으로 판단.

file="$1"

if [ -z "$file" ]; then
    echo "사용법: $0 <파일명>"
    exit 1
fi
if [ ! -f "$file" ]; then
    echo "파일을 찾을 수 없습니다: $file"
    exit 1
fi

get_prefix() {
    local line="$1"
    line="${line#$'\xef\xbb\xbf'}"
    line="${line#"${line%%[![:space:]]*}"}"
    if [[ "$line" =~ ^[A-Za-z]+ ]]; then
        echo "${BASH_REMATCH[0]}"
    fi
}

mapfile -t lines < "$file"
total=${#lines[@]}

if [ "$total" -lt 2 ]; then
    echo "비교할 줄이 부족합니다. 그대로 진행합니다."
    exit 0
fi

first_line="${lines[0]}"
first_prefix=$(get_prefix "$first_line")
sample_end=$(( total < 6 ? total - 1 : 5 ))

# Mac(Bash 3.2) 호환성을 위해 declare -A 대신 범용 파이프라인 사용
common_prefix=$(
    for ((i = 1; i <= sample_end; i++)); do
        get_prefix "${lines[$i]}"
    done | grep -v '^$' | sort | uniq -c | sort -nr | head -n 1 | awk '{print $2}'
)

is_truncated=0
missing=""

if [ -n "$common_prefix" ] && [ "$first_prefix" != "$common_prefix" ]; then
    if [[ "$common_prefix" == *"$first_prefix" ]] && [ ${#common_prefix} -gt ${#first_prefix} ]; then
        missing="${common_prefix:0:$(( ${#common_prefix} - ${#first_prefix} ))}"
        is_truncated=1
    fi
fi

if [ "$is_truncated" -eq 1 ]; then
    suggested="${missing}${first_line}"
    echo "파일의 첫 3줄:"; echo "---"; head -n 3 "$file" | cat -n; echo "---"
    echo "첫 번째 줄 앞부분이 짤린 것 같습니다!"
    echo " 다른 줄들의 공통 접두어: '${common_prefix}'"
    echo " 첫 줄의 접두어: '${first_prefix}' (앞에 '${missing}' 누락 의심)"
    echo " 현재 첫 줄: ${first_line}"
    echo " 수정 제안: ${suggested}"
    while true; do
        read -p "이 수정 내용을 사용하시겠습니까? (yes/no, 직접 입력하려면 텍스트 입력): " answer
        case "$answer" in
            yes|y)
                awk -v repl="$suggested" 'NR==1 {$0=repl} {print}' "$file" > "${file}.tmp" && mv "${file}.tmp" "$file"
                echo "첫 번째 줄이 '${suggested}'(으)로 수정되었습니다."
                break ;;
            no|n) echo "원본 그대로 진행합니다."; break ;;
            "") echo "입력이 없습니다. 다시 입력해주세요." ;;
            *)
                awk -v repl="$answer" 'NR==1 {$0=repl} {print}' "$file" > "${file}.tmp" && mv "${file}.tmp" "$file"
                echo "첫 번째 줄이 '${answer}'(으)로 수정되었습니다."
                break ;;
        esac
    done
else
    echo "첫 번째 줄이 정상입니다."
fi
