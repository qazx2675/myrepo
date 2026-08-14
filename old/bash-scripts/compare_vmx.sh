#!/bin/bash
# compare_vmx.sh - 두 VMX 파일 간 key=value 차이 비교 (awk 기반)
# 사용법: bash compare_vmx.sh <vmx1> <vmx2>

vmx1="$1"
vmx2="$2"

if [ -z "$vmx1" ] || [ -z "$vmx2" ]; then
    echo "사용법: $0 <vmx1> <vmx2>"
    exit 1
fi
[ ! -f "$vmx1" ] && { echo "파일 없음: $vmx1"; exit 1; }
[ ! -f "$vmx2" ] && { echo "파일 없음: $vmx2"; exit 1; }

norm() {
    awk -F'=' '
    /^[a-zA-Z0-9_.:]+[[:space:]]*=/ {
        key=$1; gsub(/^[ \t]+|[ \t]+$/, "", key);
        val=$0; sub(/^[^=]*=/, "", val); gsub(/^[ \t"]+|[ \t"]+$/, "", val);
        print key "=" val
    }' "$1" | sort
}

diff_result=$(diff <(norm "$vmx1") <(norm "$vmx2"))

if [ -z "$diff_result" ]; then
    echo "두 VMX 파일에 차이가 없습니다."
else
    echo "===== VMX 차이 (< $vmx1 / > $vmx2) ====="
    echo "$diff_result"
fi
