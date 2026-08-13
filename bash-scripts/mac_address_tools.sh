#!/bin/bash
# mac_address_tools.sh - MAC 주소 홀/짝 판별 및 마지막 옥텟 증감 처리 모음 (awk 기반 one-liner 모음)

# 1) MAC 주소 마지막 옥텟이 짝수/홀수인지 판별
check_mac_parity() {
    local mac="$1"
    local last_octet=$(echo "$mac" | awk -F: '{print $NF}')
    local dec=$((16#$last_octet))
    if [ $((dec % 2)) -eq 0 ]; then
        echo "짝수"
    else
        echo "홀수"
    fi
}

# 2) MAC 주소 마지막 옥텟 +1 (짝수 -> 홀수 페어 생성용)
increment_mac() {
    local mac="$1"
    echo "$mac" | awk -F: '{ printf "%s:%s:%s:%s:%s:%02x\n", $1,$2,$3,$4,$5, (strtonum("0x"$6)+1) }'
}

# 3) MAC 리스트 파일에서 짝수만 추출
filter_even_macs() {
    local file="$1"
    awk -F: '{ if (strtonum("0x"$NF) % 2 == 0) print }' "$file"
}

# 사용 예:
# check_mac_parity "00:50:56:aa:bb:0c"
# increment_mac "00:50:56:aa:bb:0c"
# filter_even_macs mac_list.txt
