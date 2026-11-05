#!/bin/bash
# stress-test 랩 e2e (.58 에서 실행). 사용: bash test/e2e.sh [A|B|D|E|all]
# 전제: /root/work/stress-test 에 바이너리, 이 test/ 폴더가 그 아래에 있음.
cd "$(dirname "$0")/.." || exit 2
ROOT=$PWD
T=$ROOT/test
mkdir -p /root/work/st-repo /root/work/st-svr /root/work/st-git conf output
cp $T/fake_repo/custom_inventory.sh /root/work/st-repo/
cp $T/fake_repo/.git_upload.sh /root/work/st-git/
cp $T/tester_setting.conf conf/tester_setting.conf
SKIP=/root/work/st-skip.txt

cleanup_load() { pkill -f '[w]hile :; do :; done'; ssh -o BatchMode=yes root@192.168.0.60 "pkill -f '[w]hile :; do :; done'" 2>/dev/null; }
wait_idle() { # 두 호스트 1분 로드가 1 미만이 될 때까지 대기
  for i in $(seq 1 60); do
    a=$(awk '{print $1}' /proc/loadavg); b=$(ssh -o BatchMode=yes root@192.168.0.60 "awk '{print \$1}' /proc/loadavg")
    awk -v a=$a -v b=$b 'BEGIN{exit !(a<1.0 && b<1.0)}' && return; sleep 5
  done
}
start_fake() { # 인자: fake_awx 플래그
  (cd $T/fake_awx && go build -o /root/work/fake_awx_bin . ) || exit 2
  /root/work/fake_awx_bin -addr 127.0.0.1:18080 "$@" > /root/work/fake_awx.log 2>&1 &
  FAKE=$!; sleep 1
}
stop_fake() { kill $FAKE 2>/dev/null; wait $FAKE 2>/dev/null; cleanup_load; }
st() { echo "\$ stress-test $*"; ./stress-test -user tester "$@"; echo "[exit=$?]"; }

case ${1:-all} in A|all)
  echo "######## A"; cleanup_load; wait_idle; : > $SKIP; start_fake -skipfile $SKIP
  st -hosts $T/hosts_mixed.txt -steps ba -er $T/error_list.txt -batch; cat /proc/loadavg
  stop_fake;;
esac
case ${1:-all} in B|all)
  echo "######## B1 skip testserver0002 (retry 1, still skipped)"; wait_idle; echo testserver0002 > $SKIP; start_fake -skipfile $SKIP
  st -hosts $T/hosts_mixed.txt -steps b -er $T/error_list.txt -batch -retry 1
  stop_fake; wait_idle
  echo "######## B2 skiponce -> retry succeeds"; start_fake -skiponce testserver0002
  st -hosts $T/hosts_mixed.txt -steps b -batch -retry 1
  stop_fake; wait_idle
  echo "######## B3 retry 0 exit 1"; echo testserver0002 > $SKIP; start_fake -skipfile $SKIP
  st -hosts $T/hosts_mixed.txt -steps b -batch -retry 0
  stop_fake; : > $SKIP;;
esac
case ${1:-all} in D|all)
  echo "######## D failing template stress-ng"; wait_idle; start_fake -failtmpl stress-ng
  st -hosts $T/hosts_mixed.txt -steps ba -batch
  stop_fake;;
esac
case ${1:-all} in E|all)
  echo "######## E"; 
  st -hosts $T/hosts_mixed.txt -steps c -batch
  st -hosts $T/hosts_mixed.txt -steps aa -batch
  st -hosts $T/hosts_mixed.txt -steps b -batch -conf /nonexistent.conf
  st -user nobody -hosts $T/hosts_mixed.txt -steps b -batch
  sed '/^repohost/d' conf/tester_setting.conf > /tmp/st_bad.conf
  st -hosts $T/hosts_mixed.txt -steps b -batch -conf /tmp/st_bad.conf
  echo "bad line here extra" > /tmp/st_badhosts.txt; echo "a b c" >> /tmp/st_badhosts.txt
  st -hosts /tmp/st_badhosts.txt -steps b -batch
  st -hosts /nonexistent -steps b -batch;;
esac
cleanup_load
