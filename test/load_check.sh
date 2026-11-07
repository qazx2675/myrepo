#!/bin/bash
# load_check.sh - install_mn 프로세스의 부하를 1초마다 /proc 에서 기록하고 계획서 7.5 기준으로 판정한다.
#
# 사용법: load_check.sh <pid> <초> [결과파일=test/load_report.txt]
#   CSV(1초마다 한 줄)는 <결과파일>.csv 에, 요약과 PASS/FAIL 은 <결과파일> 에 쓴다.
#   환경변수 PING_PPS(기본 500)·TCP_PARALLEL(기본 100) 은 ICMP·TCP 상한 판정용이다.
#
# 기록하는 값(1초마다)
#   cpu     /proc/<pid>/stat 의 utime+stime 증가분 / CLK_TCK / 실제 경과시간 * 100  (1코어 기준 %)
#   rss_kb  /proc/<pid>/status VmRSS         threads  Threads         fds  ls /proc/<pid>/fd | wc -l
#   kids    install_mn 의 직접 자식 프로세스 수(= 동시에 떠 있는 gossh/ssh. 자식은 한 번에 하나만 뜬다)
#   nice    직접 자식의 nice 값(ps -o ni)
#   in_echo/out_echo  /proc/net/snmp Icmp InEchos/OutEchos 초당 증가분. 127.x 로 보낸 echo request 는
#                     같은 호스트가 받으므로 InEchos 가 송신 속도 증거다(raw 소켓 송신은 OutEchos 에 안 잡힐 수 있음)
#   opens   /proc/net/snmp Tcp ActiveOpens 초당 증가분(TCP 연결 시도 속도)
#   tcp22   22번 포트로 나간 SYN-SENT+ESTABLISHED 연결 수(ss). 127.x 는 연결이 즉시 끝나 동시성 증거로는 약하다
#           (동시 연결 상한은 단위 테스트 TestPortParallelLimit 가 증명한다)
#   busy    1 이면 그 초에 일을 한 것: ICMP 송신, TCP 연결 시도(초당 10개 초과. ActiveOpens 는 호스트 전체
#           카운터라 다른 프로그램의 잡음을 거른다), 자식 프로세스 중 하나라도 있음
# SUMMARY_ONLY=1 이면 표본을 새로 뜨지 않고 기존 CSV 로 요약·판정만 다시 한다.
#
# 판정 정의
#   idle 초 = 그 초와 직전 3초 모두 busy=0 (회차가 끝난 뒤 스냅샷·저장 후처리 꼬리 3초는 뺀다)
#   idle CPU = idle 초들의 CPU 평균(1초 표본은 10ms 단위라 한 틱만 있어도 1% 로 보이므로 최대값 대신 평균으로 판정)
#   threads 는 처음 120초의 최대보다 마지막 120초의 최대가 크지 않으면 "늘지 않음"
set -u
if [ $# -lt 2 ]; then
	echo "사용법: $0 <pid> <초> [결과파일]" >&2
	exit 2
fi
PID=$1
SECS=$2
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=${3:-$HERE/load_report.txt}
CSV=$OUT.csv
PPS=${PING_PPS:-500}
PAR=${TCP_PARALLEL:-100}
TICK=$(getconf CLK_TCK)

snmp() { # snmp <줄접두어> <필드이름>
	awk -v p="$1:" -v f="$2" '$1==p { if (!h) { for (i=2;i<=NF;i++) n[$i]=i; h=1 } else { print $(n[f]); exit } }' /proc/net/snmp
}
cputicks() {
	local s
	s=$(cat /proc/$PID/stat 2>/dev/null) || return 1
	s=${s##*) }
	set -- $s
	echo $((${12} + ${13}))
}

if [ "${SUMMARY_ONLY:-0}" != 1 ]; then
echo "t,cpu_pct,rss_kb,threads,fds,kids,nice,in_echo,out_echo,opens,tcp22,busy" >"$CSV"
prev_t=$(date +%s.%N)
prev_c=$(cputicks) || { echo "pid $PID 없음" >&2; exit 1; }
prev_in=$(snmp Icmp InEchos)
prev_out=$(snmp Icmp OutEchos)
prev_op=$(snmp Tcp ActiveOpens)
start_t=$prev_t
end_s=$(( $(date +%s) + SECS ))
while [ "$(date +%s)" -lt "$end_s" ]; do
	sleep 1
	now_t=$(date +%s.%N)
	c=$(cputicks) || { echo "[!] pid $PID 가 사라짐" >&2; break; }
	rss=0
	thr=0
	while read -r k v _; do
		case $k in VmRSS:) rss=$v ;; Threads:) thr=$v ;; esac
	done </proc/$PID/status
	fds=$(ls /proc/$PID/fd 2>/dev/null | wc -l)
	kids=$(ps -o ni= --ppid "$PID" 2>/dev/null | tr -s ' \n' ' ')
	nk=$(echo $kids | wc -w)
	nice=${kids:--}
	nice=$(echo $nice | tr ' ' '/')
	cin=$(snmp Icmp InEchos)
	cout=$(snmp Icmp OutEchos)
	cop=$(snmp Tcp ActiveOpens)
	t22=$(ss -Htn state syn-sent state established '( dport = :22 )' 2>/dev/null | wc -l)
	awk -v t="$(awk -v a="$now_t" -v b="$start_t" 'BEGIN{print a-b}')" -v dt="$(awk -v a="$now_t" -v b="$prev_t" 'BEGIN{print a-b}')" \
		-v dc=$((c - prev_c)) -v tick="$TICK" -v rss="$rss" -v thr="$thr" -v fds="$fds" -v nk="$nk" -v nice="$nice" \
		-v din=$((cin - prev_in)) -v dout=$((cout - prev_out)) -v dop=$((cop - prev_op)) -v t22="$t22" \
		'BEGIN { cpu = dc / tick / dt * 100; busy = (din > 0 || dop > 10 || nk > 0) ? 1 : 0
		  printf "%.0f,%.1f,%d,%d,%d,%d,%s,%.0f,%.0f,%.0f,%d,%d\n", t, cpu, rss, thr, fds, nk, nice, din / dt, dout / dt, dop / dt, t22, busy }' >>"$CSV"
	prev_t=$now_t prev_c=$c prev_in=$cin prev_out=$cout prev_op=$cop
done
fi

# ---- 요약·판정 ----
awk -F, -v pps="$PPS" -v par="$PAR" -v outf="$OUT" -v pid="$PID" -v secs="$SECS" '
NR == 1 { next }
{
	n++; cpu[n] = $2; rss[n] = $3; thr[n] = $4; fd[n] = $5; kids[n] = $6; nice[n] = $7
	ein[n] = $8; eout[n] = $9; opn[n] = $10; t22[n] = $11; busy[n] = ($8 > 0 || $10 > 10 || $6 > 0) ? 1 : 0
	sum += $2; if ($2 > mx) mx = $2
	if ($3 > mrss) mrss = $3
	if ($5 > mfd) mfd = $5
	if ($6 > mk) mk = $6
	if ($8 > mein) mein = $8
	if ($9 > meout) meout = $9
	if ($11 > mt22) mt22 = $11
	if ($10 > mop) mop = $10
	if ($7 != "-") { m = split($7, a, "/"); for (j = 1; j <= m; j++) ns[a[j]] = 1 }
}
END {
	if (n == 0) { print "표본 없음" > outf; exit 1 }
	avg = sum / n
	for (i = 1; i <= n; i++) {
		quiet = 1
		for (j = i - 3; j <= i; j++) if (j >= 1 && busy[j]) quiet = 0
		if (i <= 3) quiet = 0
		if (quiet) { ni++; isum += cpu[i]; if (cpu[i] > imx) imx = cpu[i] }
		if (!busy[i]) { nb++; bsum += cpu[i] }
	}
	# ICMP 송신 속도의 5초 이동평균 최고(1초 표본은 스케줄링 지연 뒤 따라잡기로 잠깐 높게 보일 수 있다)
	for (i = 5; i <= n; i++) { t5 = 0; for (j = i - 4; j <= i; j++) t5 += ein[j]; t5 /= 5; if (t5 > m5) m5 = t5 }
	# p50
	for (i = 1; i <= n; i++) s[i] = cpu[i]
	for (i = 2; i <= n; i++) { v = s[i]; for (j = i - 1; j >= 1 && s[j] > v; j--) s[j+1] = s[j]; s[j+1] = v }
	p50 = s[int((n + 1) / 2)]
	w = (n < 120 ? int(n / 2) : 120)
	for (i = 1; i <= w; i++) if (thr[i] > thead) thead = thr[i]
	for (i = n - w + 1; i <= n; i++) if (thr[i] > ttail) ttail = thr[i]
	for (i = 1; i <= n; i++) { if (thr[i] > tmax) tmax = thr[i]; if (tmin == 0 || thr[i] < tmin) tmin = thr[i] }
	ns_list = ""; nok = 1
	for (k in ns) { ns_list = ns_list k " "; if (k != "10") nok = 0 }
	if (ns_list == "") { ns_list = "(표본에서 자식 없음)"; }
	iavg = (ni > 0 ? isum / ni : 0)
	banb = (nb > 0 ? bsum / nb : 0)
	printf "load_check: pid=%s 표본 %d초(요청 %d초)\n", pid, n, secs > outf
	printf "CPU(1코어 기준 %%): 평균 %.2f  최고 %.1f  p50 %.1f\n", avg, mx, p50 >> outf
	printf "CPU idle(회차 사이, 꼬리 3초 제외) 표본 %d초: 평균 %.2f  최고 %.1f   [참고] busy 아닌 모든 초 %d초 평균 %.2f\n", ni, iavg, imx, nb, banb >> outf
	printf "RSS 최대 %.1f MB (%d kB)\n", mrss / 1024, mrss >> outf
	printf "fd 최대 %d\n", mfd >> outf
	printf "threads: 처음 %d, 마지막 %d, 최소 %d, 최대 %d, 처음 %d초 최대 %d, 마지막 %d초 최대 %d\n", thr[1], thr[n], tmin, tmax, w, thead, w, ttail >> outf
	printf "동시 자식(gossh) 최대 %d, 본 nice 값: %s\n", mk, ns_list >> outf
	printf "ICMP echo 송신 속도(InEchos 증가분/초) 최고 %d  (OutEchos 최고 %d), 5초 평균 최고 %.0f, ping_pps 상한 %d\n", mein, meout, m5, pps >> outf
	printf "TCP: 연결 시도 속도 최고 %d/초, 22번 SYN-SENT+ESTAB 동시 최대 %d (tcp_parallel 상한 %d)\n", mop, mt22, par >> outf
	print "--- 판정 (계획서 7.5) ---" >> outf
	f = 0
	r = (avg < 5);          printf "%s CPU 10분 평균 < 5%%   : %.2f\n", (r ? "PASS" : "FAIL"), avg >> outf; f += !r
	r = (mx < 30);          printf "%s CPU 최고 < 30%%       : %.1f\n", (r ? "PASS" : "FAIL"), mx >> outf; f += !r
	r = (ni > 0 && iavg < 1); printf "%s CPU idle 평균 < 1%%   : %.2f (idle %d초)\n", (r ? "PASS" : "FAIL"), iavg, ni >> outf; f += !r
	r = (mrss < 50 * 1024); printf "%s RSS < 50MB           : %.1f MB\n", (r ? "PASS" : "FAIL"), mrss / 1024 >> outf; f += !r
	r = (mfd < 200);        printf "%s fd < 200             : %d\n", (r ? "PASS" : "FAIL"), mfd >> outf; f += !r
	r = (ttail <= thead);   printf "%s threads 늘지 않음    : 처음 구간 최대 %d, 마지막 구간 최대 %d\n", (r ? "PASS" : "FAIL"), thead, ttail >> outf; f += !r
	r = (mein <= pps + pps / 10); printf "%s ICMP 송신 <= ping_pps : %d/초 (상한 %d, 1초 표본 경계 오차 10%% 허용)\n", (r ? "PASS" : "FAIL"), mein, pps >> outf; f += !r
	r = (mt22 <= par);      printf "%s TCP 동시 <= tcp_parallel: %d (상한 %d)\n", (r ? "PASS" : "FAIL"), mt22, par >> outf; f += !r
	r = (mk <= 1);          printf "%s gossh 동시 <= 1       : %d\n", (r ? "PASS" : "FAIL"), mk >> outf; f += !r
	r = (nok);              printf "%s nice = 10            : %s\n", (r ? "PASS" : "FAIL"), ns_list >> outf; f += !r
	printf "총 %d 항목 FAIL\n", f >> outf
	exit (f > 0)
}' "$CSV"
rc=$?
cat "$OUT"
exit $rc
