#!/bin/bash
# check_redfish.sh - Redfish API를 통한 BMC(iLO/iDRAC 등) HT(하이퍼스레딩) 상태 점검 스크립트
# curl + jq 사용, host 리스트를 입력받아 반복 조회

usage() {
    echo "사용법: $0 -f <host_list_file> -u <user> -p <password>"
    exit 1
}

while getopts "f:u:p:h" opt; do
    case "$opt" in
        f) hostfile="$OPTARG" ;;
        u) user="$OPTARG" ;;
        p) pass="$OPTARG" ;;
        h) usage ;;
        *) usage ;;
    esac
done

[ -z "$hostfile" ] || [ -z "$user" ] || [ -z "$pass" ] && usage
[ ! -f "$hostfile" ] && { echo "호스트 리스트 파일을 찾을 수 없습니다: $hostfile"; exit 1; }

echo "=================================================="
printf "%-20s %-15s\n" "BMC IP" "HT 상태"
echo "=================================================="

while IFS= read -r bmc_ip || [ -n "$bmc_ip" ]; do
    [ -z "$bmc_ip" ] && continue

    result=$(curl -s -k -u "${user}:${pass}" \
        "https://${bmc_ip}/redfish/v1/Systems/1/Bios/" 2>/dev/null)

    if [ -z "$result" ]; then
        printf "%-20s %-15s\n" "$bmc_ip" "연결실패"
        continue
    fi

    ht_status=$(echo "$result" | jq -r '.Attributes.ProcHyperthreading // .Attributes.LogicalProc // "N/A"')

    printf "%-20s %-15s\n" "$bmc_ip" "$ht_status"
done < "$hostfile"

echo "=================================================="
echo "점검 완료"
