#!/bin/bash
# case_split.sh - "key" = "value" 패턴 추출 및 완벽한 중복 격리
# 엑셀 표(case1~caseN) 붙여넣기 결과에서 1번VM/2번VM 설정값을 각각 파일로 분리 저장
# 사용법: bash case_split.sh -i raw.txt   (기본값: a.txt(1번VM), b.txt(2번VM))

usage() {
    echo "사용법: $0 -i <입력파일> [-1 <1번VM 출력파일명>] [-2 <2번VM 출력파일명>]"
    exit 1
}

infile=""
out1="a.txt"
out2="b.txt"

while getopts "i:1:2:h" opt; do
    case "$opt" in
        i) infile="$OPTARG" ;;
        1) out1="$OPTARG" ;;
        2) out2="$OPTARG" ;;
        h) usage ;;
        *) usage ;;
    esac
done

[ -z "$infile" ] && usage
[ ! -f "$infile" ] && { echo "입력 파일을 찾을 수 없습니다: $infile"; exit 1; }

# "key" = "value" 패턴 (소켓 이름 등 불필요한 텍스트 무시)
pattern='"[^"]+"[[:space:]]*=[[:space:]]*"[^"]+"'

# 핵심 함수: '=' 기준으로 앞부분(키)만 자른 뒤, 남아있는 양옆의 공백/탭을 완벽히 제거
get_key() {
    echo "$1" | awk -F'=' '{print $1}' | sed -E 's/^[[:space:]]+|[[:space:]]+$//g'
}

declare -A a_keys
declare -A b_keys
declare -a a_list
declare -a b_list

while IFS= read -r line || [ -n "$line" ]; do
    matches=()
    rest="$line"

    while [[ "$rest" =~ ($pattern) ]]; do
        matches+=("${BASH_REMATCH[1]}")
        rest="${rest#*"${BASH_REMATCH[1]}"}"
    done

    count=${#matches[@]}

    if [ "$count" -eq 1 ]; then
        raw="${matches[0]}"
        key=$(get_key "$raw")
        if [ -z "${a_keys[$key]}" ]; then
            a_list+=("$raw")
            a_keys["$key"]=1
        fi
    elif [ "$count" -ge 2 ]; then
        raw1="${matches[0]}"
        key1=$(get_key "$raw1")
        if [ -z "${a_keys[$key1]}" ]; then
            a_list+=("$raw1")
            a_keys["$key1"]=1
        fi

        raw2="${matches[1]}"
        key2=$(get_key "$raw2")
        if [ -z "${b_keys[$key2]}" ]; then
            b_list+=("$raw2")
            b_keys["$key2"]=1
        fi
    fi
done < "$infile"

> "$out1"
for raw in "${a_list[@]}"; do
    key=$(get_key "$raw")
    if [ -z "${b_keys[$key]}" ]; then
        clean_raw=$(echo "$raw" | sed -E 's/[[:space:]]*=[[:space:]]*/ = /')
        echo "$clean_raw" >> "$out1"
    fi
done

> "$out2"
for raw in "${b_list[@]}"; do
    clean_raw=$(echo "$raw" | sed -E 's/[[:space:]]*=[[:space:]]*/ = /')
    echo "$clean_raw" >> "$out2"
done

echo "생성 완료: $out1, $out2"
