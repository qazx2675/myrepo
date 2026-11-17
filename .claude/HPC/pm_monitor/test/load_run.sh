#!/bin/bash
# load_run.sh - 랩(.58)에서 N대(기본 5천, v0.2.0 검증은 8천) 부하 시험을 분리된 프로세스로 시작한다 (계획서 7.5)
#   사용법: bash test/load_run.sh [초=600] [호스트수=5000] [yum 비율(%)=0]      (pm_monitor 소스 폴더에서, install_mn_os8 이 빌드돼 있어야 함)
#   yum 비율을 주면(예: 5) 그 비율의 서버가 호스트파일 두 번째 칸 yum(v0.4.0)이 된다: 3분의 1은 시작 300초 뒤 새 커널로 재부팅(yum_update/접속가능),
#   3분의 1은 같은 커널로 재부팅(재부팅됨), 나머지는 그대로(접속가능). gossh 스텁이 NS_YUM 조회에 답한다(test/stub_gossh.sh 머리말). 접속불가 7%는 yum 서버에도 같다.
#   작업 폴더는 환경변수 LOAD_DIR(기본 /root/pm_load). 다른 세션의 폴더와 겹치지 않게 바꿀 수 있다.
#   작업 폴더에 호스트(127.1.x.y N개, 모두 ping 응답·로컬 sshd 22번 열림)·설정·스텁을 만들고
#   pty_drive.py(가상 터미널 120x40 + 키 입력)로 화면을 붙이고, 수집 데몬 프로세스를 load_check.sh 로 잰다(v0.3.0 부터 수집은 데몬).
#   v0.5.0: 호스트 전부가 든 oklist.txt 를 같이 넘기고, 끝에 load_check.sh 가 gossh 호출 수·조사 대수·hosts.txt.res 행 수를 보고서에 덧붙인다.
#   끝나면 /root/pm_load/load_report.txt, drive.log 를 보면 된다. 정리: rm -rf /root/pm_load
set -eu
DUR=${1:-600}
NH=${2:-5000}
YUMPCT=${3:-0}
SRC=$(cd "$(dirname "$0")/.." && pwd)
D=${LOAD_DIR:-/root/pm_load}
rm -rf "$D"
mkdir -p "$D"
cp "$SRC/install_mn_os8" "$D/install_mn"
cp "$SRC"/test/stub_gossh.sh "$SRC"/test/load_check.sh "$SRC"/test/pty_drive.py "$D"/
chmod +x "$D"/*
cd "$D"

# N대: 127.1.0.1 ~ (250개씩 묶음, 5천=127.1.19.250, 8천=127.1.31.250). 상태 분포: 설치중 60%, 완료 30%, 디스크 3%, 접속불가(gossh 실패) 7%
awk -v n="$NH" 'BEGIN { for (i = 0; i < n; i++) print "127.1." int(i / 250) "." (i % 250 + 1) }' >hosts.txt
awk '{ m = (NR - 1) % 100; s = (m < 60) ? "installing" : (m < 90) ? "done" : (m < 93) ? "disk" : "down"; print $1, s }' hosts.txt >stub_state.txt
if [ "$YUMPCT" -gt 0 ]; then
	# yum 서버: 100/비율 번째마다 한 대(5% = 20번째마다). 기준 커널·부팅 시각 칸 + (3분의 2는) 시작 300초 뒤 전환 칸을 스텁 상태에 쓴다
	awk -v every=$((100 / YUMPCT)) -v t0="$(date +%s)" '{
		h = $1; s = $2
		if (NR % every == 7) {
			k++
			print h " yum" >"hosts.new"
			if (s == "down") { print h, s >"state.new"; next }
			line = h " done 4.18.0-513.el8.x86_64 2026-11-17 09:00:00"
			if (k % 3 == 0) line = line " 4.18.0-553.el8.x86_64 2026-11-17 14:20:00 " (t0 + 300)
			else if (k % 3 == 1) line = line " 4.18.0-513.el8.x86_64 2026-11-17 14:21:00 " (t0 + 300)
			print line >"state.new"
		} else { print h >"hosts.new"; print h, s >"state.new" }
	}' stub_state.txt
	mv hosts.new hosts.txt
	mv state.new stub_state.txt
	echo "yum 서버 $(grep -c ' yum$' hosts.txt)대(전체 $(wc -l <hosts.txt)대 중)"
fi
# v0.5.0: oklist(스텁 기본 조사값과 같은 값, 호스트 전부) -> 자동 조사·설정동일여부 O 판정·res 쓰기(8000줄)까지 부하에 들어간다
awk '{ printf "%s\t4.18.0-553.el8.x86_64\tnas01:/export/appl\t10.10.1.10 10.10.1.11\tldap01.demo ldap02.demo\tuid=pmuser,ou=user,ou=ldap_user,dc=cae,dc=com\tON\n", $1 }' hosts.txt >oklist_body.txt
{ printf 'hostname\tOS커널\tappl\tdns\tldap_URI\tbinddn\th/t\n'; cat oklist_body.txt; } >oklist.txt
rm -f oklist_body.txt
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
# 화면(pty_drive)은 데몬에 붙었다 q 로 떨어지고, 수집은 데몬이 한다 -> 부하는 데몬 프로세스 기준으로 잰다. 끝나면 --stop
setsid nohup bash -c "python3 pty_drive.py $DUR drive.log pm.pid load -- ./install_mn -c pm_monitor.conf hosts.txt oklist.txt; ./install_mn --stop hosts.txt" >drive.out 2>&1 </dev/null &
PID=""
for _ in $(seq 1 100); do
	PID=$(./install_mn --status hosts.txt 2>/dev/null | sed -n 's/.*pid \([0-9]*\) .*/\1/p')
	[ -n "$PID" ] && break
	sleep 0.2
done
[ -n "$PID" ] || { echo "수집 데몬이 시작되지 않음" >&2; exit 1; }
setsid nohup env PING_PPS=500 TCP_PARALLEL=100 LOAD_DIR="$D" bash load_check.sh "$PID" "$DUR" "$D/load_report.txt" >check.out 2>&1 </dev/null &
echo "시작: 수집 데몬 pid=$PID, ${DUR}초. 결과: $D/load_report.txt, $D/drive.log (끝나면 데몬은 자동으로 --stop)"
