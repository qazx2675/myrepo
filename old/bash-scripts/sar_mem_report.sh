#!/bin/bash
# sar_mem_report.sh - sar -r 기반 메모리 사용률 리포트 (임계치 초과 구간 색상 강조)
# 사용법: bash sar_mem_report.sh [sar파일 or 미지정시 오늘자 sar 사용]

THRESHOLD=81.0
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

sarfile="$1"

if [ -n "$sarfile" ] && [ -f "$sarfile" ]; then
    sar_cmd="cat $sarfile"
else
    sar_cmd="sar -r"
fi

echo "=================================================="
echo " 메모리 사용률 리포트 (임계치: ${THRESHOLD}%)"
echo "=================================================="
printf "%-10s %-10s %-10s\n" "시간" "사용률(%)" "상태"
echo "--------------------------------------------------"

over_count=0
total_count=0

eval "$sar_cmd" | grep -E '^[0-9]{2}:[0-9]{2}:[0-9]{2}' | while read -r line; do
    time=$(echo "$line" | awk '{print $1" "$2}')
    memused=$(echo "$line" | awk '{print $5}')

    [[ ! "$memused" =~ ^[0-9]+([.][0-9]+)?$ ]] && continue

    total_count=$((total_count + 1))
    is_over=$(awk -v a="$memused" -v b="$THRESHOLD" 'BEGIN{print (a>b)?1:0}')

    if [ "$is_over" -eq 1 ]; then
        printf "${RED}%-10s %-10s %-10s${NC}\n" "$time" "$memused" "위험(초과)"
        over_count=$((over_count + 1))
    else
        is_warn=$(awk -v a="$memused" -v b="$THRESHOLD" 'BEGIN{print (a>b-5)?1:0}')
        if [ "$is_warn" -eq 1 ]; then
            printf "${YELLOW}%-10s %-10s %-10s${NC}\n" "$time" "$memused" "주의"
        else
            printf "${GREEN}%-10s %-10s %-10s${NC}\n" "$time" "$memused" "정상"
        fi
    fi
done

echo "--------------------------------------------------"
echo "리포트 종료. 임계치(${THRESHOLD}%) 초과 구간은 빨간색으로 표시됩니다."
