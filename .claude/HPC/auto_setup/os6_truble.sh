#!/bin/bash
# os6_truble.sh - os6_mgmt(os8_mgmt 가 닿지 못하는 서버로 가는 중계 서버) 경유가 안 될 때의 원인 점검 (읽기 전용)
#
# 사용법 (os8_mgmt 에서 root 로):
#   ./os6_truble.sh [conf 경로] [-t 대상호스트] [-p 병렬수] [-h]
#     conf 경로  : 생략하면 auto_setup 과 같은 순서로 찾는다
#                  $AUTO_SETUP_CONF/auto_setup.conf → <auto_setup 실행 파일 디렉터리>/conf/auto_setup.conf → /etc/auto_setup/auto_setup.conf
#     -t 호스트  : os6_mgmt 에서 gossh 한 번을 이 대상에게 실행해 본다 (예: -t sec01). 생략하면 gossh 실행 확인만
#     -p N       : 병렬 ssh 접속 시험 개수 (기본 8) — sshd MaxStartups / MaxSessions 한도 확인
#   NO_COLOR 가 설정돼 있거나 터미널이 아니면 색 없이 출력한다.
#
# 아무것도 바꾸지 않는다 (ssh 로 읽기 전용 명령만 실행, sshd 재시작 없음, 임시 파일은 시험이 끝나면 지움).
# 원격 명령은 os6_mgmt 로그인 셸이 csh/tcsh 여도 되도록 bash -c '...' 한 줄(작은따옴표·! 없음)로 보낸다.
#
# 본 스크립트는 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다.

PAR=8
TARGET=""
CONF=""
while [ $# -gt 0 ]; do
	case "$1" in
	-t) TARGET=$2; shift 2 ;;
	-p) PAR=$2; shift 2 ;;
	-h | --help) sed -n '2,15p' "$0"; exit 0 ;;
	*) CONF=$1; shift ;;
	esac
done
case "$PAR" in '' | *[!0-9]*) PAR=8 ;; esac
[ "$PAR" -lt 1 ] && PAR=1

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	GRN=$'\033[32m'; YEL=$'\033[33m'; RED=$'\033[31m'; RST=$'\033[0m'
else
	GRN=""; YEL=""; RED=""; RST=""
fi
NOK=0; NWARN=0; NFAIL=0
SUMMARY=()
ok() { NOK=$((NOK + 1)); printf '  %s[OK]%s   %s\n' "$GRN" "$RST" "$*"; }
warn() { NWARN=$((NWARN + 1)); printf '  %s[WARN]%s %s\n' "$YEL" "$RST" "$1"; SUMMARY+=("WARN|$CUR|$1|${2:-}"); [ -n "${2:-}" ] && printf '         → %s\n' "$2"; }
fail() { NFAIL=$((NFAIL + 1)); printf '  %s[FAIL]%s %s\n' "$RED" "$RST" "$1"; SUMMARY+=("FAIL|$CUR|$1|${2:-}"); [ -n "${2:-}" ] && printf '         → %s\n' "$2"; }
CUR=""
skip() { printf '  [SKIP] %s\n' "$*"; }
step() { CUR="$*"; printf '\n== %s\n' "$*"; }
cmdline() { printf '  $ %s\n' "$*"; }

# ---------------------------------------------------------------- conf
# auto_setup 의 conf.go 와 같은 규칙: key=value, # 주석, 앞뒤 공백·따옴표 제거
conf_get() { # conf_get <key>
	[ -n "$CONFFILE" ] || return 0
	awk -v k="$1" '
		/^[ \t]*#/ { next }
		{ i = index($0, "="); if (!i) next
		  key = substr($0, 1, i - 1); gsub(/^[ \t]+|[ \t]+$/, "", key); if (key != k) next
		  v = substr($0, i + 1); gsub(/^[ \t\r]+|[ \t\r]+$/, "", v)
		  n = length(v); if (n >= 2 && (substr(v, 1, 1) == "\"" || substr(v, 1, 1) == "\x27") && substr(v, n, 1) == substr(v, 1, 1)) v = substr(v, 2, n - 2)
		  print v; exit }' "$CONFFILE"
}

step "1. conf 값 (os6_mgmt / os6_gossh / os6_autosetup / os6_os_check_sh)"
CONFFILE=""
if [ -n "$CONF" ]; then
	[ -f "$CONF" ] && CONFFILE=$CONF
else
	SELF_DIR=$(cd "$(dirname "$0")" && pwd)
	AS_BIN=$(command -v auto_setup 2>/dev/null)
	for c in "${AUTO_SETUP_CONF:+$AUTO_SETUP_CONF/auto_setup.conf}" \
		"${AS_BIN:+$(dirname "$AS_BIN")/conf/auto_setup.conf}" "$SELF_DIR/conf/auto_setup.conf" /etc/auto_setup/auto_setup.conf; do
		[ -n "$c" ] && [ -f "$c" ] && { CONFFILE=$c; break; }
	done
fi
cmdline "conf 파일 찾기 (인자 > \$AUTO_SETUP_CONF > <실행파일 디렉터리>/conf > /etc/auto_setup)"
if [ -z "$CONFFILE" ]; then
	fail "auto_setup.conf 를 찾지 못함" "conf 경로를 인자로 주세요: $0 /경로/auto_setup.conf (conf 가 없고 빌드 때 값을 넣었다면 아래 값은 비어 보일 수 있음)"
else
	ok "conf: $CONFFILE"
fi
MGMT=$(conf_get os6_mgmt); GOSSH=$(conf_get os6_gossh); ASDIR=$(conf_get os6_autosetup); OCSH=$(conf_get os6_os_check_sh)
SSHTO=$(conf_get ssh_timeout)
case "$SSHTO" in '' | *[!0-9]*) SSHTO=30 ;; esac
[ "$SSHTO" -lt 5 ] && SSHTO=5
for kv in "os6_mgmt=$MGMT" "os6_gossh=$GOSSH" "os6_autosetup=$ASDIR" "os6_os_check_sh=$OCSH"; do
	k=${kv%%=*}; v=${kv#*=}
	if [ -n "$v" ]; then
		ok "$k = $v"
	else
		case "$k" in
		os6_mgmt | os6_gossh) fail "$k 가 비어 있음" "conf 의 $k 를 채우세요 (비면 os6 경유가 동작하지 않음)" ;;
		os6_autosetup) warn "os6_autosetup 이 비어 있음" "os6_mgmt 의 PATH 에서 auto_setup 을 찾는다 — probe(ping 감시)가 필요하면 경로(디렉터리 또는 실행 파일)를 적으세요" ;;
		*) printf "  [OK]   os6_os_check_sh 비어 있음 (os_check_sh 와 같은 경로로 간주, 2차 체크 전용)\n"; NOK=$((NOK + 1)) ;;
		esac
	fi
done
ok "ssh_timeout = ${SSHTO}초 (conf 값 '$(conf_get ssh_timeout)', 비면 30)"
if [ -z "$MGMT" ]; then
	printf '\nos6_mgmt 가 없어 원격 점검을 건너뜁니다.\n'
fi

# ---------------------------------------------------------------- 도우미
now_ms() { date +%s%N | cut -b1-13; }
# classify <출력>: ssh 출력에서 원인 한 줄 (auto_setup 의 sshReason 과 같은 규칙)
classify() {
	local o=$1
	case "$o" in
	*"REMOTE HOST IDENTIFICATION HAS CHANGED"*) echo "KEYCHANGED|os6_mgmt 호스트 키가 known_hosts 와 다름 (RHEL8 로 재설치하며 호스트 키가 바뀐 경우가 흔함)|ssh-keygen -R ${MGMT#*@} 로 옛 키를 지우고 사람이 한 번 ssh ${MGMT} 로 접속해 yes 로 새 키를 등록하세요 (root 의 ~/.ssh/known_hosts)" ;;
	*"Host key verification failed"*) echo "KEYUNKNOWN|os6_mgmt 호스트 키가 known_hosts 에 없음 (BatchMode 라 질문을 못 해 접속이 거부됨)|사람이 한 번 ssh ${MGMT} 로 접속해 yes 로 키를 등록하세요 (또는 ssh-keyscan -H ${MGMT#*@} >> ~/.ssh/known_hosts)" ;;
	*"Permission denied"*) echo "AUTH|인증 실패 (키 인증이 안 됨)|os8_mgmt root 의 공개키를 os6_mgmt 의 ~/.ssh/authorized_keys 에 넣고, RHEL8 sshd 의 PubkeyAcceptedKeyTypes(ssh-rsa 제한) 설정과 SELinux 컨텍스트(restorecon -R ~/.ssh)를 확인하세요" ;;
	*"Could not resolve hostname"*) echo "RESOLVE|이름 해석 실패|/etc/hosts 또는 DNS 에 os6_mgmt 이름을 넣거나 conf 를 IP 로 바꾸세요" ;;
	*"timed out during banner exchange"*) echo "BANNER|접속은 되나 sshd 배너 응답이 늦음|os6_mgmt sshd 가 바쁘거나(MaxStartups) 역방향 DNS 지연이 의심됩니다 — 아래 sshd 한도·UseDNS 항목과 ssh_timeout 값을 확인하세요" ;;
	*"Connection timed out"* | *"Operation timed out"*) echo "TIMEOUT|접속 시간 초과|방화벽/라우팅 확인, ssh_timeout 을 늘려 보세요 (conf)" ;;
	*"Connection refused"*) echo "REFUSED|접속 거부 (sshd 가 안 떠 있거나 포트가 다름)|os6_mgmt 에서 systemctl status sshd, ss -ltn | grep :22 를 확인하세요 (재시작은 하지 말고 먼저 원인 확인)" ;;
	*"No route to host"*) echo "NOROUTE|경로 없음|망/라우팅/방화벽 확인" ;;
	*"kex_exchange_identification"* | *"Connection closed by"* | *"Connection reset by"*) echo "KEXCLOSED|접속 직후 서버가 끊음 — sshd MaxStartups(동시 인증 대기) 한도 초과 가능성이 큼|os6_mgmt 의 /etc/ssh/sshd_config MaxStartups 를 늘리고(예: 30:30:100) sshd 를 reload 하거나, auto_setup 의 max_runs 를 줄이세요. 로그: journalctl -u sshd | grep -i drop" ;;
	*"exec request failed on channel"*) echo "EXECFAIL|원격 셸을 실행하지 못함 (보통 프로세스 수 한도 nproc 또는 로그인 셸 오류)|os6_mgmt 의 /etc/security/limits.d/*.conf nproc 한도와 계정의 로그인 셸(/etc/passwd)을 확인하세요" ;;
	*"Resource temporarily unavailable"* | *"Cannot fork"* | *"fork: retry"*) echo "FORK|원격 프로세스 수 한도(fork 실패)|os6_mgmt 의 /etc/security/limits.d/*.conf nproc 한도와 root 의 프로세스·스레드 수를 확인하세요 (아래 한도 항목)" ;;
	*"Is a directory"*) echo "ISDIR|실행 경로가 디렉터리|os6_autosetup/os6_gossh 가 파일이어야 하는 곳에 디렉터리를 가리키는지 확인" ;;
	*"Exec format error"*) echo "EXEC|실행 파일 형식 오류 (구조/OS 에 맞지 않는 바이너리)|os6_mgmt(RHEL8 x86_64)에서 실행되는 빌드를 쓰세요 (RHEL6 용 바이너리는 안 될 수 있음)" ;;
	*"No such file or directory"* | *"command not found"*) echo "NOFILE|실행 파일 없음|os6_autosetup/os6_gossh 경로를 확인하세요 (os6_mgmt 에서 ls -l 로 확인)" ;;
	esac
}
# 원격 실행: bash -c '<한 줄>' (csh 로그인 셸 안전 — $ 는 작은따옴표 안이라 로컬/원격 csh 가 건드리지 않음)
rsh() { # rsh <제한초> <한 줄 명령>
	local to=$1; shift
	timeout "$((to + SSHTO + 5))" ssh -o BatchMode=yes -o ConnectTimeout="$SSHTO" "$MGMT" "bash -c '$*'" 2>&1
}
show_out() { printf '%s\n' "$1" | head -${2:-6} | sed 's/^/      | /'; }

[ -z "$MGMT" ] && { SKIP_REMOTE=1; } || SKIP_REMOTE=""

# ---------------------------------------------------------------- 2. 이름·ping
step "2. os6_mgmt 이름 해석·ping"
if [ -n "$SKIP_REMOTE" ]; then
	skip "os6_mgmt 없음 — 건너뜀"
else
	H=$(ssh -G "$MGMT" 2>/dev/null | awk '$1=="hostname" { print $2; exit }'); [ -z "$H" ] && H=${MGMT#*@}
	cmdline "getent hosts $H"
	if g=$(getent hosts "$H"); then ok "해석됨: $g"; else fail "이름 해석 실패: $H" "/etc/hosts 나 DNS 에 등록하거나 conf 의 os6_mgmt 를 IP 로 바꾸세요"; fi
	cmdline "ping -c 2 -W 2 $H"
	if p=$(ping -c 2 -W 2 "$H" 2>&1); then ok "ping 응답: $(printf '%s\n' "$p" | tail -1)"; else warn "ping 무응답 (ICMP 차단일 수 있음 — 아래 ssh 결과로 판단)" "$(printf '%s' "$p" | tail -1)"; fi
fi

# ---------------------------------------------------------------- 3. ssh 접속
step "3. ssh 접속 (BatchMode, ConnectTimeout=${SSHTO}) 과 소요 시간"
SSH_OK=""
if [ -z "$SKIP_REMOTE" ]; then
	cmdline "ssh -o BatchMode=yes -o ConnectTimeout=$SSHTO $MGMT true"
	cmdline "ssh-keygen -F ${MGMT#*@}   (known_hosts 등록 여부, 읽기 전용)"
	if ssh-keygen -F "${H:-${MGMT#*@}}" > /dev/null 2>&1; then ok "known_hosts 에 등록돼 있음"; else warn "known_hosts 에 ${MGMT#*@} 가 없음 (또는 해시된 다른 이름으로만 등록)" "ssh config 별칭이면 별칭이 아니라 실제 호스트 이름으로 등록되며, 이름 대신 IP 로 등록했다면 conf 의 os6_mgmt 표기와 같게 등록하세요"; fi
	t0=$(now_ms)
	out=$(timeout $((SSHTO + 20)) ssh -o BatchMode=yes -o ConnectTimeout="$SSHTO" "$MGMT" true 2>&1); rc=$?
	t1=$(now_ms); el=$(((t1 - t0)))
	secs=$(awk -v m="$el" 'BEGIN { printf "%.1f", m / 1000 }')
	out=$(printf '%s\n' "$out" | grep -v -i 'post-quantum\|store now\|server may need\|openssh.com/pq')
	if [ $rc -eq 0 ]; then
		SSH_OK=1
		if [ "$el" -ge $((SSHTO * 500)) ]; then
			warn "ssh 접속 성공이지만 느림: ${secs}초 (ssh_timeout ${SSHTO}초의 절반 이상)" "아래 비교 시험 결과·sshd UseDNS/GSSAPI 항목을 보세요. 접속 지연이 길면 ssh_timeout 을 늘리세요"
		elif [ "$el" -ge 3000 ]; then
			warn "ssh 접속 성공, 다소 느림: ${secs}초" "정상이면 1초 안팎입니다. 비교 시험 결과를 보세요"
		else
			ok "ssh 접속 성공: ${secs}초"
		fi
		if [ "$el" -ge 3000 ]; then
			t0=$(now_ms); ssh -o BatchMode=yes -o ConnectTimeout="$SSHTO" -o GSSAPIAuthentication=no "$MGMT" true > /dev/null 2>&1; t1=$(now_ms)
			cmdline "ssh -o BatchMode=yes -o GSSAPIAuthentication=no $MGMT true   (비교)"
			printf '      | GSSAPIAuthentication=no 로: %s초\n' "$(awk -v m=$((t1 - t0)) 'BEGIN { printf "%.1f", m / 1000 }')"
			if [ $((t1 - t0)) -lt $((el / 2)) ]; then
				warn "GSSAPI 때문에 느림" "os6_mgmt 쪽이 아니라 os8_mgmt 의 /etc/ssh/ssh_config(또는 ~/.ssh/config)에 'GSSAPIAuthentication no' 를 두거나 os6_mgmt sshd_config 의 GSSAPIAuthentication no 를 검토하세요"
			fi
		fi
	else
		c=$(classify "$out")
		if [ -n "$c" ]; then
			IFS='|' read -r code why hint <<< "$c"
			fail "ssh 접속 실패 [$code] ${secs}초: $why" "$hint"
		elif [ $rc -eq 124 ]; then
			fail "ssh 가 응답하지 않아 ${SSHTO}초+20초 후 중단됨 (인증/로그인 단계가 멈춤)" "os6_mgmt 로그인 지연(UseDNS·GSSAPI·PAM·홈 디렉터리 마운트)을 확인하세요. 접속 후 첫 응답까지 오래 걸리면 ssh_timeout 과 별개로 준비확인(약 $((60 + 2 * (SSHTO > 10 ? SSHTO - 10 : 0)))초 상한)이 시간 초과됩니다"
		else
			fail "ssh 접속 실패 (rc=$rc, 알려진 원인과 일치하지 않음)" "ssh -vv $MGMT true 로 단계를 확인하세요"
		fi
		show_out "$out"
	fi
else
	skip "건너뜀"
fi

# ---------------------------------------------------------------- 4. 로그인 셸
step "4. os6_mgmt 로그인 셸과 bash -c 동작 (csh 안전 인용)"
if [ -n "$SSH_OK" ]; then
	cmdline "ssh $MGMT 'echo \$SHELL'"
	sh=$(timeout $((SSHTO + 20)) ssh -o BatchMode=yes -o ConnectTimeout="$SSHTO" "$MGMT" 'echo $SHELL' 2>/dev/null | tail -1)
	ok "로그인 셸: ${sh:-?}"
	case "$sh" in
	*csh) printf '      | csh 계열 — auto_setup 은 이를 전제로 bash -c 로 감싸 보냄\n' ;;
	esac
	cmdline "ssh $MGMT \"bash -c 'echo \\\$BASH_VERSION; echo \\\$((6*7))'\""
	o=$(rsh 20 'echo bash=$BASH_VERSION; echo calc=$((6*7)); echo uid=$(id -u)')
	if printf '%s' "$o" | grep -q 'calc=42' && printf '%s' "$o" | grep -q '^uid=0'; then ok "원격 bash -c 정상 ($(printf '%s' "$o" | grep '^bash='))"; else fail "원격 bash -c 가 기대대로 동작하지 않음" "root 로그인인지, bash 가 설치돼 있는지 확인"; show_out "$o"; fi
else
	skip "ssh 접속이 안 돼 건너뜀"
fi

# ---------------------------------------------------------------- 5. os6_autosetup
step "5. os6_autosetup (os6_mgmt 위 auto_setup — probe 세션이 실행됨)"
if [ -n "$SSH_OK" ]; then
	p=$ASDIR
	cmdline "ssh $MGMT bash -c 'p=${p:-auto_setup}; [ -d \$p ] && p=\$p/auto_setup; ls -l \$p; \$p -h | head -1'"
	rs="p=$p; [ -z \"\$p\" ] && p=auto_setup; if [ -d \"\$p\" ]; then echo kind=dir; p=\"\${p%/}/auto_setup\"; fi; echo path=\$p; q=\$p; case \$p in */*) ;; *) q=\$(command -v \$p) ;; esac; if [ -z \"\$q\" ]; then echo notfound=1; elif [ ! -e \"\$q\" ]; then echo missing=1; elif [ -d \"\$q\" ]; then echo isdir=1; elif [ ! -x \"\$q\" ]; then echo notexec=1; else ls -l \"\$q\" | cut -c1-120; file \"\$q\" 2>/dev/null | cut -c1-150; timeout 10 \"\$q\" -h 2>&1 | head -2; echo hrc=\${PIPESTATUS[0]}; fi; echo probes=\$(pgrep -fc [a]uto_setup.probe)"
	o=$(rsh 30 "$rs")
	if printf '%s' "$o" | grep -q 'notfound=1'; then
		fail "os6_mgmt 의 PATH 에서 auto_setup 을 못 찾음" "os6_autosetup 에 auto_setup 이 있는 디렉터리(또는 실행 파일 경로)를 적으세요"
	elif printf '%s' "$o" | grep -q 'missing=1'; then
		fail "os6_autosetup 경로에 파일이 없음 ($(printf '%s' "$o" | grep '^path='))" "os6_mgmt 에 auto_setup 을 복사하세요 (probe 가 실패해 os6 경로 호스트의 ping 정보가 없음)"
	elif printf '%s' "$o" | grep -q 'isdir=1'; then
		fail "실행 파일 자리에 디렉터리가 있음 ($(printf '%s' "$o" | grep '^path='))" "os6_autosetup 을 올바른 디렉터리/실행 파일로 고치세요"
	elif printf '%s' "$o" | grep -q 'notexec=1'; then
		fail "실행 권한이 없음 ($(printf '%s' "$o" | grep '^path='))" "os6_mgmt 에서 chmod +x 하세요"
	else
		if printf '%s' "$o" | grep -qi 'Exec format error\|cannot execute binary'; then
			fail "auto_setup 이 os6_mgmt 에서 실행되지 않음 (형식 오류)" "RHEL8 x86_64 에서 실행되는 빌드(os8 용)를 os6_autosetup 에 두세요"
		elif printf '%s' "$o" | grep -q '^hrc=0\|^hrc=1\|^hrc=2' && printf '%s' "$o" | grep -qi 'usage\|사용법\|auto_setup'; then
			ok "auto_setup 실행됨 ($(printf '%s' "$o" | grep '^path='))"
		else
			warn "auto_setup -h 결과를 확인하지 못함" "아래 출력을 보세요"
		fi
		show_out "$o" 8
	fi
	np=$(printf '%s' "$o" | sed -n 's/^probes=//p' | tail -1)
	case "$np" in
	'' ) warn "probe 세션 수를 읽지 못함" ;;
	0) printf '      | 지금 os6_mgmt 에서 도는 auto_setup probe 세션: 0개 (os6 경로 호스트가 없으면 정상)\n' ;;
	1) ok "os6_mgmt 에서 auto_setup probe 세션 1개 실행 중" ;;
	*) warn "os6_mgmt 에서 auto_setup probe 세션이 ${np}개" "데몬이 재시작되며 남은 옛 세션일 수 있음 — os6_mgmt 에서 pgrep -af 'auto_setup probe' 로 확인 (자동으로 끊기지 않은 세션은 사람이 정리)" ;;
	esac
else
	skip "ssh 접속이 안 돼 건너뜀"
fi

# ---------------------------------------------------------------- 6. gossh
step "6. os6_gossh 실행과 gossh 한 번 (os6_mgmt → 대상)"
if [ -n "$SSH_OK" ] && [ -n "$GOSSH" ]; then
	cmdline "ssh $MGMT bash -c 'test -x $GOSSH; $GOSSH -h | head -1'"
	o=$(rsh 20 "g=$GOSSH; if [ -d \"\$g\" ]; then echo isdir=1; elif [ ! -e \"\$g\" ]; then echo missing=1; elif [ ! -x \"\$g\" ]; then echo notexec=1; else timeout 10 \"\$g\" -h 2>&1 | head -1; fi")
	if printf '%s' "$o" | grep -q 'missing=1'; then fail "os6_gossh 파일이 없음: $GOSSH" "os6_mgmt 에 gossh 를 두세요 (RHEL8 이므로 os8 용 gossh 빌드 사용 — gossh_os6 는 저장소에서 제거됨)"
	elif printf '%s' "$o" | grep -q 'isdir=1'; then fail "os6_gossh 가 디렉터리: $GOSSH" "실행 파일 경로를 적으세요"
	elif printf '%s' "$o" | grep -q 'notexec=1'; then fail "os6_gossh 실행 권한 없음: $GOSSH" "chmod +x"
	elif printf '%s' "$o" | grep -qi 'Exec format error'; then fail "os6_gossh 형식 오류" "os6_mgmt 에서 실행되는 gossh 빌드를 두세요"
	else ok "os6_gossh 실행됨: $(printf '%s' "$o" | head -1 | cut -c1-70)"; fi
	if [ -n "$TARGET" ]; then
		cmdline "ssh $MGMT bash -c 'echo $TARGET > \$f; $GOSSH -pm -script -w \$f \"echo OS6TRUBLE_OK\"'"
		o=$(rsh 90 "f=\$(mktemp); echo $TARGET > \$f; timeout 60 $GOSSH -pm -script -w \$f \"echo OS6TRUBLE_OK\" 2>&1; rm -f \$f \${f}_*")
		if printf '%s' "$o" | grep -q 'OS6TRUBLE_OK'; then ok "gossh 로 $TARGET 실행 성공"; else fail "gossh 로 $TARGET 실행 실패" "대상이 os6_mgmt 에서도 안 닿는지(망/키) 확인"; show_out "$o"; fi
	fi
else
	[ -z "$GOSSH" ] && skip "os6_gossh 없음 — 건너뜀" || skip "ssh 접속이 안 돼 건너뜀"
fi

# ---------------------------------------------------------------- 7. sshd·프로세스 한도
step "7. os6_mgmt sshd 한도·프로세스 한도 (읽기 전용)"
if [ -n "$SSH_OK" ]; then
	cmdline "ssh $MGMT bash -c 'sshd -T | grep -Ei \"maxstartups|maxsessions|usedns|gssapiauthentication|logingracetime\"'"
	o=$(rsh 30 'sshd -T 2>/dev/null | grep -Ei "maxstartups|maxsessions|usedns|gssapiauthentication|logingracetime"')
	if [ -z "$o" ]; then
		warn "sshd -T 로 설정을 읽지 못함 (권한/경로)" "os6_mgmt 에서 직접 sshd -T | grep -Ei 'maxstartups|maxsessions' 실행"
	else
		show_out "$o" 8
		ms=$(printf '%s' "$o" | sed -n 's/^maxstartups //p' | head -1); mss=$(printf '%s' "$o" | sed -n 's/^maxsessions //p' | head -1)
		st=${ms%%:*}; case "$st" in '' | *[!0-9]*) st=10 ;; esac
		if [ "$st" -lt "$PAR" ]; then warn "MaxStartups 시작값 $st < 병렬 시험 수 $PAR" "동시 접속이 많으면 접속이 끊길 수 있음 — sshd_config 의 MaxStartups 를 늘리거나(reload 필요, 사람이 판단) auto_setup 의 max_runs 를 줄이세요"; else ok "MaxStartups $ms (시작값 $st)"; fi
		case "$mss" in '' | *[!0-9]*) ;; *) [ "$mss" -lt "$PAR" ] && warn "MaxSessions $mss < 병렬 수 $PAR (한 연결에서 세션을 여럿 여는 경우만 영향)" "필요하면 MaxSessions 조정" || ok "MaxSessions $mss" ;; esac
		printf '%s' "$o" | grep -qi '^usedns yes' && warn "UseDNS yes — 로그인 때 역방향 DNS 조회로 느려질 수 있음" "os6_mgmt 의 DNS 가 느리면 UseDNS no 를 검토 (사람이 판단·reload)"
		printf '%s' "$o" | grep -qi '^gssapiauthentication yes' && printf '      | GSSAPIAuthentication yes (KDC 가 없는 환경이면 로그인이 느려질 수 있음 — 위 3번 비교 시험 참고)\n'
	fi
	cmdline "ssh $MGMT bash -c 'ulimit -u; ulimit -n; ps -u root -L --no-headers | wc -l'"
	o=$(rsh 20 'echo nproc=$(ulimit -u); echo nofile=$(ulimit -n); echo threads=$(ps -u root -L --no-headers | wc -l); echo procs=$(ps -e --no-headers | wc -l)')
	show_out "$o" 5
	nproc=$(printf '%s' "$o" | sed -n 's/^nproc=//p'); thr=$(printf '%s' "$o" | sed -n 's/^threads=//p')
	case "$nproc" in '' | unlimited) ok "root 의 nproc 한도: ${nproc:-?}" ;;
	*[!0-9]*) ok "root 의 nproc 한도: $nproc" ;;
	*) if [ "${thr:-0}" -gt 0 ] && [ "$thr" -ge $((nproc * 8 / 10)) ]; then fail "root 스레드 수($thr)가 nproc 한도($nproc)의 80% 이상" "fork 실패(Resource temporarily unavailable)로 ssh 가 느려지거나 실패 — /etc/security/limits.d/*.conf 확인"; else ok "root nproc 한도 $nproc, 현재 스레드 $thr"; fi ;;
	esac
	cmdline "ssh $MGMT bash -c 'grep -Hv ^# /etc/security/limits.conf /etc/security/limits.d/*.conf | grep -Ei nproc'"
	o=$(rsh 20 'grep -Hv "^[[:space:]]*#" /etc/security/limits.conf /etc/security/limits.d/*.conf 2>/dev/null | grep -Ei "nproc|maxlogins"')
	if [ -n "$o" ]; then show_out "$o" 6; printf '%s' "$o" | grep -Eqi 'root' || printf '      | root 항목이 없음 — 일반 사용자용 nproc 제한은 root 에 적용되지 않음(* 와일드카드는 root 제외)\n'; else printf '      | nproc/maxlogins 항목 없음\n'; fi
else
	skip "ssh 접속이 안 돼 건너뜀"
fi

# ---------------------------------------------------------------- 8. 병렬 접속
step "8. 병렬 ssh 접속 시험 (동시에 ${PAR}개, ControlMaster 끔)"
if [ -n "$SSH_OK" ]; then
	cmdline "for i in 1..$PAR: ssh -o BatchMode=yes -o ConnectTimeout=$SSHTO -o ControlPath=none $MGMT true &"
	D=$(mktemp -d /tmp/os6_truble.XXXXXX) || D=""
	if [ -n "$D" ]; then
		t0=$(now_ms)
		for i in $(seq 1 "$PAR"); do
			( timeout $((SSHTO + 30)) ssh -o BatchMode=yes -o ConnectTimeout="$SSHTO" -o ControlPath=none "$MGMT" true > "$D/$i.out" 2>&1; echo $? > "$D/$i.rc" ) &
		done
		wait
		t1=$(now_ms)
		nf=0; allout=""
		for i in $(seq 1 "$PAR"); do
			r=$(cat "$D/$i.rc" 2>/dev/null)
			if [ "$r" != "0" ]; then nf=$((nf + 1)); allout="$allout$(grep -v -i 'post-quantum\|store now\|server may need\|openssh.com/pq' "$D/$i.out")"$'\n'; fi
		done
		rm -rf "$D"
		secs=$(awk -v m=$((t1 - t0)) 'BEGIN { printf "%.1f", m / 1000 }')
		if [ "$nf" -eq 0 ]; then
			ok "동시 ${PAR}개 모두 성공 (${secs}초)"
		else
			c=$(classify "$allout")
			if [ -n "$c" ]; then IFS='|' read -r code why hint <<< "$c"; else code=UNKNOWN; why="분류되지 않는 실패"; hint="ssh -vv 로 확인"; fi
			fail "동시 ${PAR}개 중 ${nf}개 실패 [$code] (${secs}초): $why" "$hint"
			show_out "$allout" 4
		fi
	else
		warn "임시 디렉터리를 만들지 못해 건너뜀"
	fi
else
	skip "ssh 접속이 안 돼 건너뜀"
fi

# ---------------------------------------------------------------- 9. os8_mgmt (이 서버)
step "9. 이 서버(os8_mgmt)의 auto_setup 데몬과 최근 로그"
DD=${AUTO_SETUP_DIR:-/tmp/auto_setup}
cmdline "pgrep -af 'auto_setup daemon'"
nd=$(pgrep -f 'auto_setup.* daemon' 2> /dev/null | wc -l)
case "$nd" in 0) warn "데몬이 실행 중이 아님" "auto_setup ensure 또는 auto_setup --restart" ;; 1) ok "데몬 1개 실행 중" ;; *) warn "데몬 프로세스 ${nd}개" "정상은 1개 — auto_setup --restart 로 정리" ;; esac
cmdline "grep -E 'os6|probe|경로 전환|준비확인 실패' $DD/auto_setup.log | tail -8"
if [ -f "$DD/auto_setup.log" ]; then
	l=$(grep -E 'os6|probe|경로 전환|준비확인 실패' "$DD/auto_setup.log" | tail -8)
	if [ -n "$l" ]; then
		printf '%s\n' "$l" | sed 's/^/      | /' | cut -c1-200
		printf '%s' "$l" | grep -q '준비확인 실패\|ssh 문제\|세션 종료' && warn "최근 로그에 os6 실패 기록이 있음" "위 줄의 [원인] 표시와 같은 항목을 이 점검의 위쪽 단계에서 확인하세요"
	else
		printf '      | os6 관련 로그 줄 없음\n'
	fi
else
	warn "로그 파일이 없음: $DD/auto_setup.log" "AUTO_SETUP_DIR 환경변수를 데몬과 같게 설정하고 다시 실행하세요"
fi

# ---------------------------------------------------------------- 요약
printf '\n================ 요약: OK %d / WARN %d / FAIL %d ================\n' "$NOK" "$NWARN" "$NFAIL"
if [ ${#SUMMARY[@]} -eq 0 ]; then
	printf '문제가 발견되지 않았습니다.\n'
else
	for s in "${SUMMARY[@]}"; do
		lv=${s%%|*}; r=${s#*|}; stp=${r%%|*}; r=${r#*|}; msg=${r%%|*}; h=${r#*|}
		if [ "$lv" = FAIL ]; then col=$RED; else col=$YEL; fi
		printf '%s[%s]%s %s\n' "$col" "$lv" "$RST" "$msg"
		printf '       단계: %s\n' "$stp"
		[ -n "$h" ] && printf '       조치: %s\n' "$h"
	done
fi
cat << 'EOF'

[주의사항] 본 스크립트는 100% 신뢰하기보다는 참고용(보조 도구)으로 사용하는 것을 권장합니다. 원인 판단과 설정 변경(sshd reload, known_hosts 정리 등)은 사람이 확인한 뒤 직접 하세요.
EOF
[ "$NFAIL" -gt 0 ] && exit 1
exit 0
