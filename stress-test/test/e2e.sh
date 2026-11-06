#!/bin/bash
# stress-test 랩 e2e (.58 에서 실행). 사용: bash test/e2e.sh [A|B|D|E|F|G|all]
# 전제: /root/work/stress-test 에 바이너리, 이 test/ 폴더가 그 아래에 있음.
cd "$(dirname "$0")/.." || exit 2
ROOT=$PWD
T=$ROOT/test
mkdir -p /root/work/st-repo /root/work/st-svr /root/work/st-git conf output
cp $T/fake_repo/custom_inventory.sh /root/work/st-repo/
cp $T/fake_repo/.git_upload.sh /root/work/st-git/
cp $T/tester_setting.conf conf/tester_setting.conf
SKIP=/root/work/st-skip.txt
BUSY=/root/work/st-busy.txt
FLOG=/root/work/fake_awx.log
END=$(date -d '+1 day' +%Y,%m,%d,%H,%M)   # stress-ng 종료 시각 (-end)

cleanup_load() { pkill -f '[w]hile :; do :; done'; ssh -o BatchMode=yes root@192.168.0.60 "pkill -f '[w]hile :; do :; done'" 2>/dev/null; }
wait_idle() { # 두 호스트 1분 로드가 1 미만이 될 때까지 대기
  for i in $(seq 1 60); do
    a=$(awk '{print $1}' /proc/loadavg); b=$(ssh -o BatchMode=yes root@192.168.0.60 "awk '{print \$1}' /proc/loadavg")
    awk -v a=$a -v b=$b 'BEGIN{exit !(a<1.0 && b<1.0)}' && return; sleep 5
  done
}
start_fake() { # 인자: fake_awx 플래그
  (cd $T/fake_awx && go build -o /root/work/fake_awx_bin . ) || exit 2
  /root/work/fake_awx_bin -addr 127.0.0.1:18080 "$@" > $FLOG 2>&1 &
  FAKE=$!; sleep 1
}
stop_fake() { kill $FAKE 2>/dev/null; wait $FAKE 2>/dev/null; cleanup_load; }
st() { echo "\$ stress-test $*"; ./stress-test -user tester "$@"; echo "[exit=$?]"; }
# 가짜 AWX 로그에서 템플릿 인벤토리 변경·launch 순서를 보여 준다
show_switch() { grep -oE 'PATCH template=[^ ]+ inventory=[^ ]+|launch tmpl=[^ ]+ inventory=[0-9]+|extra_vars=.*' $FLOG; }

case ${1:-all} in A|all)
  echo "######## A"; cleanup_load; wait_idle; : > $SKIP; : > $BUSY; start_fake -skipfile $SKIP
  st -hosts $T/hosts_mixed.txt -steps ba -end $END -er $T/error_list.txt -batch; cat /proc/loadavg
  show_switch
  stop_fake;;
esac
case ${1:-all} in B|all)
  echo "######## B1 skip testserver0002 (retry 1, still skipped)"; wait_idle; echo testserver0002 > $SKIP; start_fake -skipfile $SKIP
  st -hosts $T/hosts_mixed.txt -steps b -end $END -er $T/error_list.txt -batch -retry 1
  stop_fake; wait_idle
  echo "######## B2 skiponce -> retry succeeds"; start_fake -skiponce testserver0002
  st -hosts $T/hosts_mixed.txt -steps b -end $END -batch -retry 1
  stop_fake; wait_idle
  echo "######## B3 retry 0 exit 1"; echo testserver0002 > $SKIP; start_fake -skipfile $SKIP
  st -hosts $T/hosts_mixed.txt -steps b -end $END -batch -retry 0
  stop_fake; : > $SKIP;;
esac
case ${1:-all} in D|all)
  echo "######## D failing template stress-ng"; wait_idle; start_fake -failtmpl stress-ng
  st -hosts $T/hosts_mixed.txt -steps ba -end $END -batch
  show_switch
  stop_fake;;
esac
case ${1:-all} in E|all)
  echo "######## E"; start_fake
  st -hosts $T/hosts_mixed.txt -steps c -batch
  st -hosts $T/hosts_mixed.txt -steps aa -batch
  st -hosts $T/hosts_mixed.txt -steps b -end $END -batch -conf /nonexistent.conf
  st -user nobody -hosts $T/hosts_mixed.txt -steps b -end $END -batch
  sed '/^repohost/d' conf/tester_setting.conf > /tmp/st_bad.conf
  st -hosts $T/hosts_mixed.txt -steps b -end $END -batch -conf /tmp/st_bad.conf
  echo "bad line here extra" > /tmp/st_badhosts.txt; echo "a b c" >> /tmp/st_badhosts.txt
  st -hosts /tmp/st_badhosts.txt -steps b -end $END -batch
  st -hosts /nonexistent -steps b -end $END -batch
  echo "--- -end 없음 / 과거 / 범위 밖"
  st -hosts $T/hosts_mixed.txt -steps b -batch
  st -hosts $T/hosts_mixed.txt -steps b -end 2020,1,1,0,0 -batch
  st -hosts $T/hosts_mixed.txt -steps b -end "$(date +%Y),13,1,0,0" -batch
  echo "--- 옛 cpu_intel_* 키"
  { cat conf/tester_setting.conf; echo "cpu_intel_template = cpu-intel"; } > /tmp/st_old.conf
  st -hosts $T/hosts_mixed.txt -steps a -batch -conf /tmp/st_old.conf
  echo "--- username 인벤토리 없음 / default_inventory 없음 / 템플릿 없음"
  sed 's/^username = admin/username = nobody/' conf/tester_setting.conf > /tmp/st_noinv.conf
  st -hosts $T/hosts_mixed.txt -steps a -batch -conf /tmp/st_noinv.conf
  sed 's/^default_inventory = DEFAULT/default_inventory = NODEF/' conf/tester_setting.conf > /tmp/st_nodef.conf
  st -hosts $T/hosts_mixed.txt -steps a -batch -conf /tmp/st_nodef.conf
  sed 's/^cpu_template = cpu-test/cpu_template = no-such/' conf/tester_setting.conf > /tmp/st_notmpl.conf
  st -hosts $T/hosts_mixed.txt -steps a -batch -conf /tmp/st_notmpl.conf
  stop_fake;;
esac
case ${1:-all} in F|all)
  echo "######## F1 템플릿 인벤토리 사용 중 → [인벤토리사용중] 줄 → stdin r → 실행"; wait_idle; echo kim > $BUSY; start_fake -busyfile $BUSY
  OUT=/root/work/f1.out; FIFO=/root/work/f1.fifo; rm -f $FIFO; mkfifo $FIFO; exec 3<>$FIFO
  ./stress-test -user tester -hosts $T/hosts_mixed.txt -steps a -batch <$FIFO >$OUT 2>&1 & pid=$!
  for i in $(seq 1 60); do grep -q '^\[인벤토리사용중\] ' $OUT && break; sleep 1; done
  grep '^\[인벤토리사용중\] ' $OUT
  : > $BUSY; echo r >&3
  wait $pid; echo "[exit=$?]"; exec 3>&-; rm -f $FIFO
  grep -c '등록 시작' $OUT | sed 's/^/등록 횟수: /'; tail -6 $OUT
  show_switch
  stop_fake; wait_idle
  echo "######## F2 사용 중 + stdin EOF → 묶음 중단(exit 1), PATCH 없음"; echo kim > $BUSY; start_fake -busyfile $BUSY
  st -hosts $T/hosts_mixed.txt -steps a -batch </dev/null
  echo "PATCH 횟수: $(grep -c 'PATCH template' $FLOG)"
  stop_fake; : > $BUSY
  echo "######## F3 인벤토리를 바꿔 둔 사이 SIGTERM → 되돌리고 종료(143)"; wait_idle; start_fake -launchdelay 5s
  OUT=/root/work/f3.out
  ./stress-test -user tester -hosts $T/hosts_mixed.txt -steps a -batch </dev/null >$OUT 2>&1 & pid=$!
  for i in $(seq 1 120); do grep -q 'PATCH template=cpu-test inventory=2' $FLOG && break; sleep 0.5; done
  kill -TERM $pid; wait $pid; echo "[exit=$?]"
  tail -3 $OUT
  show_switch
  stop_fake;;
esac
case ${1:-all} in G|all)
  echo "######## G -inspect (읽기 전용: 가짜 AWX 로그에 PATCH/launch 가 없어야 함)"; : > $BUSY; start_fake -busyfile $BUSY
  echo "--- G1 정상 conf → exit 0 (동기화 인벤토리≠username 은 참고 경고)"
  st -inspect
  echo "--- G2 템플릿 인자(이름·ID)"
  st -inspect stress-ng 1
  echo "--- G3 hostname_key 가 survey 에 없음 → exit 1"
  sed 's/^cpu_hostname_key = .*/cpu_hostname_key = no_such/' conf/tester_setting.conf > /tmp/st_nokey.conf
  st -inspect -conf /tmp/st_nokey.conf
  echo "--- G4 접속 불가 URL → exit 2"
  sed 's#^awx_url = .*#awx_url = http://127.0.0.1:1#' conf/tester_setting.conf > /tmp/st_badurl.conf
  st -inspect -conf /tmp/st_badurl.conf
  echo "--- G5 없는 템플릿 → exit 2"
  st -inspect no-such-template
  echo "--- G6 템플릿 인벤토리 사용 중(kim) → exit 1"
  echo kim > $BUSY
  st -inspect cpu-test
  : > $BUSY
  echo "--- G7 username(dh.kim) 이 이름에 들어간 인벤토리 I_dh.kim → 노란 체크, exit 0"
  sed "s/^username = .*/username = dh.kim/" conf/tester_setting.conf > /tmp/st_part.conf
  st -inspect -conf /tmp/st_part.conf
  echo "PATCH/launch 요청 횟수: $(grep -cE 'PATCH|launch tmpl' $FLOG)"
  stop_fake;;
esac
case ${1:-all} in H|all)
  echo "######## H -yaml-state (읽기 전용) + 등록 직후 [등록] 줄 (cpu 템플릿 job 은 실패시켜 부하를 걸지 않음)"; : > $BUSY; rm -f /root/work/st-svr/tester_*; start_fake -busyfile $BUSY -failtmpl cpu-test
  echo "--- H1 등록 전(처음 상태): yml 은 가짜 AWX 초기값, 호스트 0대"
  st -yaml-state
  echo "--- H2 -steps a 로 등록 → [등록] yml=<등록한 yml> hosts=<등록 직후 인벤토리 호스트>"
  ./stress-test -user tester -hosts $T/hosts_mixed.txt -steps a -batch | grep '^\[등록\]'
  echo "--- H3 -yaml-state 가 등록한 값을 그대로 보임 (yml 이름·호스트가 [등록] 줄과 같아야 함)"
  st -yaml-state
  echo "--- H4 다른 사용자가 호스트를 넣고 소스 yml 을 바꿈 → 달라진 값이 보임"
  curl -s -o /dev/null -X POST -d '{"name":"intruder"}' http://127.0.0.1:18080/api/v2/inventories/3/hosts/
  curl -s -o /dev/null -X PATCH -d '{"source_path":"os_install.yml"}' http://127.0.0.1:18080/api/v2/inventory_sources/7/
  st -yaml-state
  echo "--- H5 접속 불가 URL → exit 2 / 알 수 없는 추가 인자 → exit 2"
  sed 's#^awx_url = .*#awx_url = http://127.0.0.1:1#' conf/tester_setting.conf > /tmp/st_badurl2.conf
  st -yaml-state -conf /tmp/st_badurl2.conf
  st -yaml-state extra
  echo "템플릿 인벤토리 PATCH·launch 요청 횟수: $(grep -cE 'PATCH template|launch tmpl' $FLOG) (H2 의 cpu launch 1회 + 인벤토리 변경·복원 2회 = 3 이어야 함)"
  stop_fake;;
esac
cleanup_load
