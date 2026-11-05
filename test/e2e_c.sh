#!/bin/bash
# 시나리오 C (TTY): 대화형 화면 갱신, 재실행 y/N, stress_run.sh 메뉴. 사용: bash test/e2e_c.sh
cd "$(dirname "$0")/.." || exit 2
T=$PWD/test
cleanup_load() { pkill -f '[w]hile :; do :; done'; ssh -o BatchMode=yes root@192.168.0.60 "pkill -f '[w]hile :; do :; done'" 2>/dev/null; }
wait_idle() { for i in $(seq 1 60); do a=$(awk '{print $1}' /proc/loadavg); b=$(ssh -o BatchMode=yes root@192.168.0.60 "awk '{print \$1}' /proc/loadavg"); awk -v a=$a -v b=$b 'BEGIN{exit !(a<1.0 && b<1.0)}' && return; sleep 5; done; }
start_fake() { (cd $T/fake_awx && go build -o /root/work/fake_awx_bin .); /root/work/fake_awx_bin -addr 127.0.0.1:18080 "$@" > /root/work/fake_awx.log 2>&1 & FAKE=$!; sleep 1; }
stop_fake() { kill $FAKE 2>/dev/null; wait $FAKE 2>/dev/null; cleanup_load; }
cp $T/tester_setting.conf conf/tester_setting.conf
cleanup_load; wait_idle
echo "######## C1: TTY, skiponce testserver0002 -> 'y' 재실행"
start_fake -skiponce testserver0002
# 입력: 감시 끝날 때까지 기다렸다가 y
( sleep 100; echo y ) | script -qfc "./stress-test -user tester -hosts $T/hosts_c.txt -steps b" /root/work/c1.typescript > /dev/null
echo "[exit=$?]"
stop_fake; wait_idle
echo "######## C2: TTY, 미실행 + 'n'"
echo testserver0002 > /root/work/st-skip.txt
start_fake -skipfile /root/work/st-skip.txt
( sleep 100; echo n ) | script -qfc "./stress-test -user tester -hosts $T/hosts_c.txt -steps b" /root/work/c2.typescript > /dev/null
echo "[exit=$?]"
stop_fake; : > /root/work/st-skip.txt
