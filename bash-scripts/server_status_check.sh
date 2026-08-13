#!/bin/bash
# server_status_check.sh - ping/pdsh/nc 조합 4단계 서버 상태 분류 + 벤더별 코멘트 생성
# 상태: 1) 정상(ping+ssh 응답) 2) ping만 응답(ssh 불가) 3) 완전 무응답 4) 포트만 열림(nc)

usage() {
    echo "사용법: $0 -f <host_list_file>"
    exit 1
}

while getopts "f:h" opt; do
    case "$opt" in
        f) hostfile="$OPTARG" ;;
        h) usage ;;
        *) usage ;;
    esac
done

[ -z "$hostfile" ] && usage
[ ! -f "$hostfile" ] && { echo "파일을 찾을 수 없습니다: $hostfile"; exit 1; }

normal_list=()
ping_only_list=()
dead_list=()
port_only_list=()

check_vendor_comment() {
    local host="$1"
    if [[ "$host" == *"esx"* ]]; then
        echo "ESXi 호스트 - vCenter 연결 상태 별도 확인 필요"
    elif [[ "$host" == *"dell"* || "$host" == *"idrac"* ]]; then
        echo "Dell iDRAC 대상 - Redfish API로 재확인 권장"
    elif [[ "$host" == *"hpe"* || "$host" == *"ilo"* ]]; then
        echo "HPE iLO 대상 - Redfish API로 재확인 권장"
    else
        echo "일반 리눅스 호스트"
    fi
}

while IFS= read -r host || [ -n "$host" ]; do
    [ -z "$host" ] && continue

    if ping -c 1 -W 1 "$host" &>/dev/null; then
        if pdsh -w "$host" "echo ok" &>/dev/null; then
            normal_list+=("$host")
        else
            ping_only_list+=("$host")
        fi
    else
        if nc -z -w 1 "$host" 22 &>/dev/null; then
            port_only_list+=("$host")
        else
            dead_list+=("$host")
        fi
    fi
done < "$hostfile"

echo "===== [정상] ping+ssh 응답 (${#normal_list[@]}대) ====="
for h in "${normal_list[@]}"; do echo "  $h"; done

echo "===== [주의] ping만 응답, ssh 불가 (${#ping_only_list[@]}대) ====="
for h in "${ping_only_list[@]}"; do
    echo "  $h : $(check_vendor_comment "$h")"
done

echo "===== [확인필요] 포트(22)만 열림, ping 무응답 (${#port_only_list[@]}대) ====="
for h in "${port_only_list[@]}"; do
    echo "  $h : $(check_vendor_comment "$h")"
done

echo "===== [장애의심] 완전 무응답 (${#dead_list[@]}대) ====="
for h in "${dead_list[@]}"; do
    echo "  $h : $(check_vendor_comment "$h")"
done
