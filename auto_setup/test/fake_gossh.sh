#!/bin/bash
# fake_gossh.sh - 변경 메뉴(m)·엔진 시험용 가짜 gossh. test/demo_change.sh, test/run_change_os.sh, Go 테스트(change_s6_test.go)가 쓴다.
#
# 진짜 gossh 와 같은 호출 규약을 흉내 낸다:  gossh [-pm] [-script] -w <호스트목록> [-u U] [-i K] [-P N] [-c N] [-t N] <원격 명령>
# 엔진(ip-change-engine, ldap-config-engine)과 auto_setup(auto.appl 원복)이 보내는 "base64 로 감싼 스크립트" 를 풀어,
# 호스트마다 fixture 루트(FAKE_ROOTS/<호스트>) 아래에서 **실제 스크립트를 로컬로 실행**하고 "<호스트>: <줄>" 로 출력한다.
#   - 호스트별 hostname -s / hostname / hostname -I 는 PATH 앞에 둔 shim 으로 돌려준다 (이 shim 은 스크립트 실행 때만 PATH 에 들어간다)
#   - systemctl · sss_cache · service · nmcli · ifup · ifdown 도 shim — 실제 서비스는 절대 건드리지 않고 호출만 기록한다
#   - 스크립트의 /etc 경로는 호스트 루트 아래로 옮겨 실행한다 (ROOT=<호스트 루트>, IP 스크립트의 network_scripts_dir 는 앞에 루트를 붙임).
#     fixture 밖을 가리키면 실행을 거부한다.
#   - 엔진 스크립트가 아닌 명령(데몬의 hostname/uptime 확인 등)은 같은 디렉터리의 gossh_e2e(있으면)로 넘긴다.
#
# 호스트별 상태: $FAKE_ROOTS/<호스트>/.fake/
#   hostname  이 노드의 hostname (FQDN 가능, 없으면 호스트 이름)    ips   hostname -I 가 돌려줄 값
#   os8_down  있으면 os8_mgmt 에서 접속 불가 (os6 경유로만 응답)      down  있으면 어디서도 접속 불가
#
# 환경변수 (모두 선택. 기본값은 이 파일의 부모 디렉터리 기준: <부모>/nodes, <부모>/fake)
#   FAKE_ROOTS         호스트별 fixture 루트들의 부모
#   FAKE_LOGDIR        기록 디렉터리: gossh.log(호출 1줄) node.log(노드 스크립트 출력) systemctl.log(shim 호출) shim/
#   FAKE_VIA           os8 | os6 (비면 이 파일 경로에 /os6/ 가 있으면 os6)
#   FAKE_UNGUARD=1     lib_common.sh 의 "ROOT 가 있으면 서비스 재시작 건너뜀" 보호를 끄고 shim systemctl 로 재시작을 기록하게 한다.
#                      -no-restart 가 정말 재시작을 막는지(또는 막지 않을 때 RESTARTED 가 나오는지) 보려는 시험 전용.
#   FAKE_FALLBACK      엔진 스크립트가 아닌 명령을 넘길 실행 파일 (기본 <이 디렉터리>/gossh_e2e)
set -u

SELF=$0
HERE=$(cd "$(dirname "$0")" && pwd -P)
BASE=$(cd "$HERE/.." && pwd -P)
FAKE_ROOTS=${FAKE_ROOTS:-$BASE/nodes}
FAKE_LOGDIR=${FAKE_LOGDIR:-$BASE/fake}
VIA=${FAKE_VIA:-}
if [[ -z $VIA ]]; then
	if [[ $SELF == */os6/* || $HERE == */os6 ]]; then VIA=os6; else VIA=os8; fi
fi
mkdir -p "$FAKE_LOGDIR" 2> /dev/null
GLOG=$FAKE_LOGDIR/gossh.log
NLOG=$FAKE_LOGDIR/node.log
SLOG=$FAKE_LOGDIR/systemctl.log
SHIM=$FAKE_LOGDIR/shim

ORIG=("$@")
hf=""
cmd=""
flags=()
while [[ $# -gt 0 ]]; do
	case $1 in
		-pm | -script) flags+=("$1"); shift ;;
		-w) hf=${2:-}; flags+=("-w" "$hf"); shift 2 ;;
		-p) flags+=("-p" "***"); shift 2 ;;
		-u | -i | -P | -c | -t) flags+=("$1" "${2:-}"); shift 2 ;;
		-*) flags+=("$1"); shift ;;
		*) cmd=$1; shift ;;
	esac
done
[[ -n $hf && -f $hf ]] || { echo "fake_gossh: -w 호스트 목록 파일 없음" >&2; exit 1; }

# ---- 원격 명령 풀기: echo <base64 (2글자마다 ' ' 또는 '.')> | tr -d <' '|.> | base64 -d | bash ----
inner="" sb64="" envpre="" script=""
if [[ $cmd == "echo "* && $cmd == *"| base64 -d | bash" ]]; then
	b=${cmd#echo }
	b=${b%% | tr -d *}
	inner=$(printf '%s' "$b" | tr -d ' .' | base64 -d 2> /dev/null)
	sb64=$(printf '%s\n' "$inner" | sed -n 's/.*echo \([A-Za-z0-9+/=]\{16,\}\) | base64 -d > .*/\1/p' | head -n1)
	envpre=$(printf '%s\n' "$inner" | sed -n 's/.*&& chmod [0-9]* [^ ]* && \(.*\)bash [^ ]*; rc=.*/\1/p' | head -n1)
	[[ -n $sb64 ]] && script=$(printf '%s' "$sb64" | base64 -d 2> /dev/null)
fi

kind=""
if [[ -n $script ]]; then
	case $script in
		*HOSTMAPEOF*) kind=ip ;;
		*"ROLLBACK_TO="*) kind=ipRb ;;
		*"APPL|NOSCRIPT"*) kind=appl ;;
		*"restart_for_changed"* | *"APPLY|"*)
			kind=ldap
			case " $envpre " in *" DRYRUN=1 "*) kind=ldapDry ;; esac
			[[ $script == *"restore_one()"* ]] && kind=ldapRb
			;;
	esac
fi
hosts=$(grep . "$hf" | paste -sd, -)
printf '%s via=%s kind=%s flags=[%s] hosts=%s\n' "$(date +%H:%M:%S)" "$VIA" "${kind:-other}" "${flags[*]}" "$hosts" >> "$GLOG"

if [[ -z $kind ]]; then
	FB=${FAKE_FALLBACK:-$HERE/gossh_e2e}
	if [[ -x $FB ]]; then exec "$FB" "${ORIG[@]}"; fi
	exit 0
fi

# ---- shim (이 실행 동안만 PATH 앞) ----
mkdir -p "$SHIM"
cat > "$SHIM/hostname" << 'SH'
#!/bin/sh
case "${1:-}" in
	-s | --short) echo "${FAKE_HOST%%.*}" ;;
	-I | --all-ip-addresses) echo "$FAKE_IPS" ;;
	*) echo "$FAKE_HOST" ;;
esac
SH
for n in systemctl sss_cache service nmcli ifup ifdown; do
	cat > "$SHIM/$n" << SH
#!/bin/sh
echo "\${FAKE_HOST%%.*} $n \$*" >> "\$FAKE_SYSTEMCTL_LOG"
exit 0
SH
done
chmod +x "$SHIM"/*
[[ $(PATH=$SHIM:$PATH command -v systemctl) == "$SHIM/systemctl" ]] || { echo "fake_gossh: shim 이 PATH 앞이 아님 — 중단" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

while IFS= read -r h; do
	h=${h//[[:space:]]/}
	[[ -n $h ]] || continue
	R=$FAKE_ROOTS/$h
	case $R in "$FAKE_ROOTS"/?*) ;; *) echo "$h: ERROR: 잘못된 호스트 이름" >&2; continue ;; esac
	if [[ ! -d $R ]]; then echo "$h: ERROR: 접속 실패 (fixture 없음)" >&2; continue; fi
	if [[ -e $R/.fake/down || ( $VIA == os8 && -e $R/.fake/os8_down ) ]]; then
		echo "$h: ERROR: 접속 실패 ($VIA)" >&2
		continue
	fi
	fhost=$h
	[[ -f $R/.fake/hostname ]] && fhost=$(head -n1 "$R/.fake/hostname")
	fips=""
	[[ -f $R/.fake/ips ]] && fips=$(head -n1 "$R/.fake/ips")

	s=$work/script.sh
	case $kind in
		ip | ipRb)
			sed -e "s#^NETWORK_SCRIPTS_DIR='/etc/#NETWORK_SCRIPTS_DIR='$R/etc/#" \
				-e "s#^RHEL9_PATH='/etc/#RHEL9_PATH='$R/etc/#" <<< "$script" > "$s"
			nsd=$(sed -n "s/^NETWORK_SCRIPTS_DIR='\(.*\)'\$/\1/p" "$s" | head -n1)
			r9=$(sed -n "s/^RHEL9_PATH='\(.*\)'\$/\1/p" "$s" | head -n1)
			if [[ $nsd != "$R"/* || ( -n $r9 && $r9 != "$R"/* ) ]]; then
				echo "$h: ERROR: fake_gossh: network_scripts_dir 이 fixture 밖이라 실행하지 않음 ($nsd $r9)" >&2
				continue
			fi
			;;
		appl)
			# /root/auto_appl_restore.sh 는 fixture 루트 아래 사본(/etc → 루트/etc 로 바꾼 것)을 실행
			if [[ -f $R/root/auto_appl_restore.sh ]]; then
				sed "s#/etc/#$R/etc/#g" "$R/root/auto_appl_restore.sh" > "$work/restore_run.sh"
			else
				: > "$work/restore_run.sh"
			fi
			sed -e "s#bash /root/auto_appl_restore\.sh#bash $work/restore_run.sh#" \
				-e "s#/root/auto_appl_restore\.sh#$R/root/auto_appl_restore.sh#g" <<< "$script" > "$s"
			;;
		*)
			if [[ ${FAKE_UNGUARD:-} == 1 ]]; then
				sed -e 's/\[ -n "\$ROOT" \]/[ -n "" ]/g' -e 's/\[ -z "\$ROOT" \]/[ -z "" ]/g' <<< "$script" > "$s"
			else
				printf '%s\n' "$script" > "$s"
			fi
			;;
	esac

	envs=()
	for tok in $envpre; do
		[[ $tok == ROOT=* ]] && continue   # 노드 루트는 이 가짜가 정한다
		[[ $tok == *=* ]] && envs+=("$tok")
	done
	(
		cd "$R" || exit 1
		env ${envs[@]+"${envs[@]}"} ROOT="$R" PATH="$SHIM:$PATH" FAKE_HOST="$fhost" FAKE_IPS="$fips" FAKE_SYSTEMCTL_LOG="$SLOG" \
			bash "$s" < /dev/null 2>&1
	) | sed -e 's/[[:space:]]*$//' | grep -v '^$' | sed "s/^/$h: /" | tee -a "$NLOG"
done < "$hf"
exit 0
