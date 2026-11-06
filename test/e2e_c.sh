#!/bin/bash
# 시나리오 C (TTY): 대화형 종료 시각 입력, 화면 갱신, 재실행 y/N, 인벤토리 사용 중 r. 사용: bash test/e2e_c.sh
cd "$(dirname "$0")/.." || exit 2
T=$PWD/test
cleanup_load() { pkill -f '[w]hile :; do :; done'; ssh -o BatchMode=yes root@192.168.0.60 "pkill -f '[w]hile :; do :; done'" 2>/dev/null; }
wait_idle() { for i in $(seq 1 60); do a=$(awk '{print $1}' /proc/loadavg); b=$(ssh -o BatchMode=yes root@192.168.0.60 "awk '{print \$1}' /proc/loadavg"); awk -v a=$a -v b=$b 'BEGIN{exit !(a<1.0 && b<1.0)}' && return; sleep 5; done; }
start_fake() { (cd $T/fake_awx && go build -o /root/work/fake_awx_bin .); /root/work/fake_awx_bin -addr 127.0.0.1:18080 "$@" > /root/work/fake_awx.log 2>&1 & FAKE=$!; sleep 1; }
stop_fake() { kill $FAKE 2>/dev/null; wait $FAKE 2>/dev/null; cleanup_load; }
# 종료 시각 입력: 잘못된 월(13) 한 번 → 다시 묻는지 확인, 그다음 내년 1월 1일 0시 0분
Y=$(( $(date +%Y) + 1 ))
end_input() { printf '%s\n' "$Y" 13 01 01 00 00; }
cp $T/tester_setting.conf conf/tester_setting.conf
cleanup_load; wait_idle
echo "######## C1: TTY, 종료 시각 입력 + skiponce testserver0002 -> 'y' 재실행"
start_fake -skiponce testserver0002
# 입력: 종료 시각 → 감시 끝날 때까지 기다렸다가 y
( sleep 2; end_input; sleep 100; echo y ) | script -qfc "./stress-test -user tester -hosts $T/hosts_c.txt -steps b" /root/work/c1.typescript > /dev/null
echo "[exit=$?]"
grep -aoE 'end_(year|month|day|hour|min): |월은 1-12|종료 시각: [^ ]+' /root/work/c1.typescript | sort | uniq -c
stop_fake; wait_idle
echo "######## C2: TTY, 미실행 + 'n'"
echo testserver0002 > /root/work/st-skip.txt
start_fake -skipfile /root/work/st-skip.txt
( sleep 2; end_input; sleep 100; echo n ) | script -qfc "./stress-test -user tester -hosts $T/hosts_c.txt -steps b" /root/work/c2.typescript > /dev/null
echo "[exit=$?]"
stop_fake; : > /root/work/st-skip.txt; wait_idle
echo "######## C3: TTY, cpu 템플릿 인벤토리 사용 중 → 'r=다시 확인' 질문 → 해제 후 r"
echo kim > /root/work/st-busy.txt
start_fake -busyfile /root/work/st-busy.txt
( sleep 10; : > /root/work/st-busy.txt; echo r; sleep 100; echo n ) | script -qfc "./stress-test -user tester -hosts $T/hosts_c.txt -steps a" /root/work/c3.typescript > /dev/null
echo "[exit=$?]"
grep -aoE '인벤토리가 kim 입니다[^—]*|r=다시 확인, x=이 묶음 중단: ' /root/work/c3.typescript | sort | uniq -c
grep -oE 'PATCH template=[^ ]+ inventory=[^ ]+' /root/work/fake_awx.log
stop_fake; : > /root/work/st-busy.txt
