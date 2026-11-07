#!/bin/bash
# load_run.sh - 랩(.58)에서 5천 대 부하 시험을 분리된 프로세스로 시작한다 (계획서 7.5)
#   사용법: bash test/load_run.sh [초=600]      (pm_monitor 소스 폴더에서, install_mn_os8 이 빌드돼 있어야 함)
#   /root/pm_load 에 호스트(127.1.x.y 5천 개, 모두 ping 응답·로컬 sshd 22번 열림)·설정·스텁을 만들고
#   pty_drive.py(가상 터미널 120x40 + 키 입력)와 load_check.sh 를 백그라운드로 띄운다.
#   끝나면 /root/pm_load/load_report.txt, drive.log 를 보면 된다. 정리: rm -rf /root/pm_load
set -eu
DUR=${1:-600}
SRC=$(cd "$(dirname "$0")/.." && pwd)
D=/root/pm_load
rm -rf "$D"
mkdir -p "$D"
cp "$SRC/install_mn_os8" "$D/install_mn"
cp "$SRC"/test/stub_gossh.sh "$SRC"/test/load_check.sh "$SRC"/test/pty_drive.py "$D"/
chmod +x "$D"/*
cd "$D"

# 5천 대: 127.1.0.1 ~ 127.1.19.250. 상태 분포: 설치중 60%, 완료 30%, 디스크 3%, 접속불가(gossh 실패) 7%
awk 'BEGIN { for (a = 0; a < 20; a++) for (b = 1; b <= 250; b++) print "127.1." a "." b }' >hosts.txt
awk '{ m = (NR - 1) % 100; s = (m < 60) ? "installing" : (m < 90) ? "done" : (m < 93) ? "disk" : "down"; print $1, s }' hosts.txt >stub_state.txt
echo '#!/bin/bash' >ldap_check.sh # 이름만 필요(스텁이 명령에 ldap_check 가 있으면 응답)
cat >pm_monitor.conf <<EOF
gossh=$D/stub_gossh.sh
interval_sec=60
ping_pps=500
tcp_parallel=100
ping_timeout_ms=1500
ldap_check_sh=$D/ldap_check.sh
EOF
echo "호스트 $(wc -l <hosts.txt)대, 상태: $(awk '{c[$2]++} END {for (k in c) printf "%s=%d ", k, c[k]}' stub_state.txt)"

export STUB_GOSSH_STATE=$D/stub_state.txt STUB_GOSSH_NICE=$D/nice.log STUB_GOSSH_LOCK=$D/lock STUB_GOSSH_SLEEP=2 STUB_GOSSH_LOG=$D/gossh_calls.log
setsid nohup python3 pty_drive.py "$DUR" drive.log pm.pid load -- ./install_mn -c pm_monitor.conf hosts.txt >drive.out 2>&1 </dev/null &
for _ in $(seq 1 50); do [ -s pm.pid ] && break; sleep 0.1; done
PID=$(cat pm.pid)
setsid nohup env PING_PPS=500 TCP_PARALLEL=100 bash load_check.sh "$PID" "$DUR" "$D/load_report.txt" >check.out 2>&1 </dev/null &
echo "시작: install_mn pid=$PID, ${DUR}초. 결과: $D/load_report.txt, $D/drive.log"
