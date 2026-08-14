#!/bin/bash
# disk_and_text_oneliners.sh - lsblk 용량 합산, join 다중파일 병합, tmp 파일 정리 등 자주 쓰는 one-liner 모음

# 1) lsblk로 전체 디스크 용량 합산 (GB)
sum_disk_capacity() {
    lsblk -b -d -n -o SIZE | awk '{sum+=$1} END {printf "%.2f GB\n", sum/1024/1024/1024}'
}

# 2) 여러 파일을 공통 키 기준으로 병합 (join, 정렬 필요)
join_multi_files() {
    local f1="$1"; local f2="$2"; local f3="$3"
    join <(sort "$f1") <(sort "$f2") | join - <(sort "$f3")
}

# 3) root 소유가 아닌 /tmp 최상위 파일 목록
find_non_root_tmp_files() {
    find /tmp -maxdepth 1 ! -user root
}

# 4) 특정 포트가 열릴 때까지 대기 (worklist 기반 배치 처리용)
wait_for_port() {
    local host="$1"; local port="$2"; local timeout="${3:-60}"
    local waited=0
    while ! nc -z -w 1 "$host" "$port" &>/dev/null; do
        sleep 2
        waited=$((waited+2))
        [ "$waited" -ge "$timeout" ] && { echo "타임아웃: $host:$port"; return 1; }
    done
    echo "$host:$port 오픈 확인"
}
