#!/bin/bash
# 01.AWX_nodeinfo_V2.sh / 02.source_dhcp_pxe.sh 실제 실행 검증 하네스 (계획서 §9)
#
# 규칙
#  - 모든 어서션은 `bash ./01.AWX_nodeinfo_V2.sh` / `bash ./02.source_dhcp_pxe.sh` 를 "실제로 실행한 결과물"
#    (stdout, LOG, 생성 파일, 스텁 호출 기록 calls.log)에 대해서만 수행한다.
#  - 스크립트 로직(awk 등)을 이 파일에 복제하지 않는다. (기대값은 사람이 적은 상수/독립 오라클)
#  - 원본 01/02 는 읽기만 한다. 케이스마다 mktemp -d 스크래치에 복사하고, "복사본에만" sed 로 테스트 값을 채운다.
#    테스트용 임시 값은 이 test/ 안에만 존재한다.
#  - 실행 위치: 랩 Rocky(.58) 권장.  bash /root/awx_test_tmp/test/run_tests.sh
#
# 환경변수: KEEP_SCRATCH=1 (스크래치 보존)

SRC=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
F01=01.AWX_nodeinfo_V2.sh
F02=02.source_dhcp_pxe.sh
TU=testuser
ORIG_PATH=$PATH

SCRATCHES=()
FPID=""
RUN01_COUNT=0
RUN02_COUNT=0

cleanup_h() {
	{ exec 9>&-; } 2>/dev/null
	[[ -n $FPID ]] && kill -KILL "$FPID" 2>/dev/null
	if [[ -n $KEEP_SCRATCH ]]; then
		echo "KEEP_SCRATCH: ${SCRATCHES[*]}"
	elif (( ${#SCRATCHES[@]} )); then
		rm -rf "${SCRATCHES[@]}"
	fi
}
trap cleanup_h EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# ===================== 결과 수집 =====================
ROWS=(); PASS_N=0; FAIL_N=0; SEP=$'\037'
CID=""; CTITLE=""; CFAILS=(); CEV=()

short() {
	local s=${1//$'\n'/ / }
	(( ${#s} > 110 )) && s="${s:0:110}…"
	printf '%s' "$s"
}
case_begin() { CID=$1; CTITLE=$2; CFAILS=(); CEV=(); }
case_end() {   # [info]  → info 이면 PASS/FAIL 집계에서 제외(정보성 프로브)
	local st ev="" i f
	if (( ${#CFAILS[@]} )); then st=FAIL; else st=PASS; fi
	[[ $1 == info ]] && st="INFO-$st"
	for i in 0 1 2; do [[ -n ${CEV[i]} ]] && ev+="${ev:+ ; }${CEV[i]}"; done
	printf '%-10s %-8s %s\n' "$st" "$CID" "$CTITLE"
	for f in "${CFAILS[@]}"; do printf '           ! %s\n' "$f"; done
	ROWS+=("$st$SEP$CID$SEP$CTITLE$SEP$ev")
	if [[ $1 != info ]]; then
		if [[ $st == PASS ]]; then PASS_N=$((PASS_N+1)); else FAIL_N=$((FAIL_N+1)); fi
	fi
}
t_eq()  { if [[ $2 == "$3" ]]; then CEV+=("$1: $(short "$2")"); else CFAILS+=("$1 | 기대=[$(short "$3")] 실제=[$(short "$2")]"); fi; }
t_has() { if grep -Eq -- "$3" "$2" 2>/dev/null; then CEV+=("$1: $(short "$(grep -E -m1 -- "$3" "$2")")"); else CFAILS+=("$1 | 패턴 없음 /$3/ in $(basename "$2")"); fi; }
t_no()  { if grep -Eq -- "$3" "$2" 2>/dev/null; then CFAILS+=("$1 | 있으면 안 되는 패턴 /$3/: $(short "$(grep -E -m1 -- "$3" "$2")")"); fi; }
t_rc()  { # desc actual expected|nz
	if [[ $3 == nz ]]; then
		if [[ $2 -ne 0 ]]; then CEV+=("$1: rc=$2"); else CFAILS+=("$1 | 종료코드 0 (비0 기대)"); fi
	else t_eq "$1" "$2" "$3"; fi
}
t_notmp() { local n; n=$(find "$S/tmp" -mindepth 1 2>/dev/null | wc -l); t_eq "mktemp 잔여(\$TMPDIR) 0건" "$n" 0; }

# ===================== 스크래치 · 스텁 =====================
# setup_case [변수명=값 ...]
#   키: repohost svr_dir ai_server_list day_print lacp_comment inventory_delete_host  (복사본 최상단 빈 변수에 채울 값)
#       raw=1 → 복사만(sed 없음), user=0 → user() 본문 미치환
setup_case() {
	local kv k v
	cfg_repohost=repo.lab
	cfg_svr_dir=""             # 아래에서 $W/svr_dir 로 설정
	cfg_ai_server_list=""
	cfg_day_print=""
	cfg_lacp_comment=LACP-COMMENT-SENTINEL
	cfg_inventory_delete_host=delhost.lab
	cfg_infra_alias=""
	cfg_os6_host=""; cfg_os6_user=""; cfg_os6_dir=""; cfg_os6_gossh=""
	cfg_raw=0; cfg_user=1
	S=$(TMPDIR=/tmp mktemp -d); SCRATCHES+=("$S")
	W=$S/work
	mkdir -p "$W/awxkit" "$W/.stublog" "$W/svr_dir" "$S/tmp" "$S/bin" "$S/remote/Inventory" "$S/root_user/$TU/myrepo"
	cfg_svr_dir=$W/svr_dir
	mkdir -p "$S/os6share"
	for kv in "$@"; do
		k=${kv%%=*}; v=${kv#*=}
		printf -v "cfg_$k" '%s' "$v"
	done
	cfg_os6_dir=${cfg_os6_dir//@S@/$S}; cfg_os6_gossh=${cfg_os6_gossh//@S@/$S}
	STUBLOG=$W/.stublog; SVR_DIR=$W/svr_dir; REMOTE_DIR=$S/remote/Inventory
	CALLS=$STUBLOG/calls.log; OUT=$S/out.txt
	: > "$CALLS"
	export STUBLOG SVR_DIR REMOTE_DIR
	unset AWX_COLOR NO_COLOR STUB_SLEEP FAIL_CALL FAIL_ONCE_FILE NODEINFO_FAIL NODEINFO_MODE NODEINFO_DB GOSSH_SCENARIO OS6_SCENARIO OS6_LSBLK_SCENARIO LSBLK_SCENARIO OS6_SSH_FAIL AWX_AUTO AWX_AUTO_NODEINFO AWX_AUTO_OS AWX_USER AWX_VERIFY_FILE AWX_DNS_CHECKED AWX_TTY AWX_RESOLV_CONF STUB_DNSDB
	export GOSSH_SCENARIO=same
	export AUTO_SETUP_DIR="$S/as"   # 01 [14-1] auto_setup 전달 위치(스크래치)
	export PATH="$S/bin:$ORIG_PATH"
	export AWX_RESOLV_CONF="$S/no_resolv.conf"   # DNS 점검: 기본은 resolv.conf 없음 → "점검 생략" (기존 케이스가 실제 DNS 를 건드리지 않게). DNS 케이스는 dns_setup 이 바꾼다

	cp "$SRC/$F01" "$SRC/$F02" "$W/"
	echo "POWERLIMIT_SENTINEL" > "$W/.power_limit_setting.txt"

	if [[ $cfg_raw != 1 ]]; then
		sed -i \
			-e "s#^repohost=\"\"#repohost=\"$cfg_repohost\"#" \
			-e "s#^svr_dir=\"\"#svr_dir=\"$cfg_svr_dir\"#" \
			-e "s#^ai_server_list=\"\"#ai_server_list=\"$cfg_ai_server_list\"#" \
			-e "s#^day_print=\"\"#day_print=\"$cfg_day_print\"#" \
			-e "s#^lacp_comment=\"\"#lacp_comment=\"$cfg_lacp_comment\"#" \
			-e "s#^inventory_delete_host=\"\"#inventory_delete_host=\"$cfg_inventory_delete_host\"#" \
			-e "s#^infra_alias=\"\"#infra_alias=\"$cfg_infra_alias\"#" \
			-e "s#^os6_host=\"\"#os6_host=\"$cfg_os6_host\"#" \
			-e "s#^os6_user=\"\"#os6_user=\"$cfg_os6_user\"#" \
			-e "s#^os6_dir=\"\"#os6_dir=\"$cfg_os6_dir\"#" \
			-e "s#^os6_gossh=\"\"#os6_gossh=\"$cfg_os6_gossh\"#" \
			-e "s#/root/user/#$S/root_user/#g" \
			"$W/$F01"
		if [[ $cfg_user == 1 ]]; then
			# 복사본의 user(){...} 본문(현장 코드)을 테스트 user 설정으로 통째로 대체
			sed -i -e '/^user(){/,/^}/c\
user(){\
\tuser=testuser\
}' "$W/$F01"
		else
			# user=0: 실제 user() 를 쓰되 메뉴 스크립트(info_mn.sh/info.sh)만 스텁 경로로 향하게 한다
			mkdir -p "$S/umenu"
			printf '%s\n' '#!/bin/bash' 'echo "STUB_USER_MENU"' 'echo usermenu >> "$STUBLOG/calls.log"' > "$S/umenu/info_mn.sh"
			printf '%s\n' '#!/bin/bash' 'echo testuser' > "$S/umenu/info.sh"
			sed -i -e "s#^user_route=\"\"#user_route=\"$S/umenu\"#" "$W/$F01"
		fi
		# 무결성: 복사본과 원본의 차이는 최상단 변수/user 본문/git 경로 줄뿐이어야 한다
		local bad
		bad=$(diff "$SRC/$F01" "$W/$F01" | grep '^>' | grep -vE '^> (repohost|svr_dir|ai_server_list|day_print|lacp_comment|inventory_delete_host|infra_alias|os6_host|os6_user|os6_dir|os6_gossh)=|^> '$'\t''user=testuser$|^> user_route=|root_user')
		if [[ -n $bad ]]; then echo "[HARNESS ERROR] 복사본 sed 가 예상 외 줄을 변경: $bad"; exit 2; fi
		if [[ $cfg_user == 1 ]] && ! grep -q $'^\tuser=testuser$' "$W/$F01"; then
			echo "[HARNESS ERROR] user() 본문 치환 실패"; exit 2; fi
	fi

	# ---- 스텁: awxkit/{nodeinfo,invsync,dhcp,pxe}.sh ----
	cat > "$W/awxkit/nodeinfo.sh" <<'STUB'
#!/bin/bash
echo "nodeinfo $*" >> "$STUBLOG/calls.log"
user=""; hosts=""
while [[ $# -gt 0 ]]; do
	case $1 in
		-user) user=$2; shift 2 ;;
		-hosts) hosts=$2; shift 2 ;;
		*) shift ;;
	esac
done
[[ -n $NODEINFO_FAIL ]] && { echo "stub nodeinfo: 강제 실패" >&2; exit 1; }
outdir="$(dirname "$0")/output"; mkdir -p "$outdir"
out="$outdir/${user}_nodeinfo.yaml"
if [[ $NODEINFO_MODE == msg ]]; then echo "PLAY [nodeinfo] *****" > "$out"; else : > "$out"; fi
while read -r h _; do
	[[ -n $h ]] || continue
	line=$(awk -v h="$h" '$4 == h' "$NODEINFO_DB")
	[[ -n $line ]] || { echo "stub nodeinfo: DB 에 없는 호스트 $h" >&2; continue; }
	if [[ $NODEINFO_MODE == msg ]]; then
		printf 'ok: [%s] => {\n    "msg": "%s"\n}\n' "$h" "$line" >> "$out"
	else
		printf '%s\n' "$line" >> "$out"
	fi
done < "$hosts"
exit 0
STUB
	local n
	for n in invsync dhcp pxe; do
		cat > "$W/awxkit/$n.sh" <<'STUB'
#!/bin/bash
name=$(basename "$0" .sh)
line="$name $*"
echo "$line" >> "$STUBLOG/calls.log"
echo "start $name" >> "$STUBLOG/events.log"
[[ -n $STUB_SLEEP && $name != invsync ]] && sleep "$STUB_SLEEP"
echo "end $name" >> "$STUBLOG/events.log"
if [[ -n $FAIL_CALL && $line =~ $FAIL_CALL ]]; then
	if [[ -z $FAIL_ONCE_FILE ]]; then echo "stub $name: 강제 실패" >&2; exit 1
	elif [[ -f $FAIL_ONCE_FILE ]]; then rm -f "$FAIL_ONCE_FILE"; echo "stub $name: 1회 강제 실패" >&2; exit 1
	fi
fi
exit 0
STUB
	done

	# ---- 스텁: ssh / scp / gossh (PATH 앞) ----
	cat > "$S/bin/ssh" <<'STUB'
#!/bin/bash
echo "ssh $*" >> "$STUBLOG/calls.log"
host=$1; shift
cmd="$*"
if [[ $cmd =~ custom_inventory\.sh[[:space:]]+([^[:space:]]+) ]]; then
	f="$REMOTE_DIR/$(basename "${BASH_REMATCH[1]}")"
	[[ -f $f ]] || { echo "stub ssh: 입력 파일 없음 $f" >&2; exit 1; }
	n=$(wc -l < "$f")
	infra=$(awk 'NR==1 {print $3}' "$f")
	{ echo "# generated on $host from $(basename "$f")"; cat "$f"; } > "$SVR_DIR/${infra}_inventory-$(date +%s)_${n}ea.yml"
fi
# os6 재조사: 스크립트가 공유 디렉터리에 만든 실행 파일(.os6_run.*)을 "os6_mgmt 에서" 실행하는 흉내 (OS6_SCENARIO / OS6_LSBLK_SCENARIO 로 gossh 스텁 제어)
if [[ $cmd =~ bash[[:space:]]+([^[:space:]]*\.os6_run\.[^[:space:]]+) ]]; then
	echo "os6-ssh $*" >> "$STUBLOG/calls.log"
	[[ -n $OS6_SSH_FAIL ]] && { echo "stub ssh: os6 접속 실패" >&2; exit 255; }
	GOSSH_SCENARIO=${OS6_SCENARIO:-same} LSBLK_SCENARIO=${OS6_LSBLK_SCENARIO:-std} bash "${BASH_REMATCH[1]}"
	exit $?
fi
exit 0
STUB
	cat > "$S/bin/scp" <<'STUB'
#!/bin/bash
echo "scp $*" >> "$STUBLOG/calls.log"
args=("$@")
last=$((${#args[@]} - 1))
mkdir -p "$REMOTE_DIR"
for ((i = 0; i < last; i++)); do cp "${args[i]}" "$REMOTE_DIR/" || exit 1; done
exit 0
STUB
	cat > "$S/bin/gossh" <<'STUB'
#!/bin/bash
cmd=${@: -1}
tag=""; [[ $* == *.os6_hosts.* ]] && tag="os6-"
if [[ $cmd == *lsblk* ]]; then echo "${tag}gossh-lsblk $*" >> "$STUBLOG/calls.log"; else echo "${tag}gossh $*" >> "$STUBLOG/calls.log"; fi
hf=""
while [[ $# -gt 0 ]]; do
	case $1 in
		-w) hf=$2; shift 2 ;;
		*) shift ;;
	esac
done
mapfile -t hs < <(grep . "$hf")
n=${#hs[@]}
if [[ $cmd == *lsblk* ]]; then
	# lsblk -nl -o NAME,TYPE,SIZE,MOUNTPOINT 흉내 (LSBLK_SCENARIO: std | bad | sdb | nvme)
	row() { printf '%s: %s\n' "$h" "$*"; }
	std() {   # disk part-prefix boot-mount
		row "$1 disk 500G"; row "${1}${2}1 part 500M ${3:-/boot/efi}"; row "${1}${2}2 part 30G /"
		row "${1}${2}3 part 20G /var"; row "${1}${2}4 part 8G [SWAP]"; row "${1}${2}5 part 441.5G /tmp"
	}
	for ((i = 0; i < n; i++)); do
		h=${hs[i]}
		if [[ ${LSBLK_SCENARIO:-std} == down ]] && (( i == n - 1 )); then echo "$h: 접속불가 (Timeout)" >&2; continue; fi
		case ${LSBLK_SCENARIO:-std}.$i in
			bad.1) row "sda disk 500G"; row "sda1 part 500M /boot"; row "sda2 part 100G"
			       row "rhel-root lvm 30G /"; row "rhel-var lvm 20G /var" ;;
			bad.2) row "sda disk 500G"; row "sda1 part 500M /boot"; row "sda2 part 30G /"; row "sda3 part 25G /var"
			       row "sda4 part 8G [SWAP]"; row "sda5 part 100G /home" ;;
			sdb.*) std sdb "" /boot ;;
			nvme.*) std nvme0n1 p /boot/efi ;;
			*) std sda "" /boot ;;
		esac
	done
	exit 0
fi
cp "$hf" "$STUBLOG/gossh_hosts.txt"
for ((i = 0; i < n; i++)); do
	h=${hs[i]}
	v=$'INFO\tLDAP\tINFRA1\tSITE1'; mode="fault-tolerance (active-backup)"
	case $GOSSH_SCENARIO in
		diff)   (( i > 0 )) && v=$'INFO\tLDAP\tINFRA9\tSITE9' ;;
		lacp)   (( i < 2 )) && mode="IEEE 802.3ad Dynamic link aggregation" ;;
		noresp) if (( i == n - 1 )); then echo "$h: 접속불가 (Timeout)" >&2; continue; fi ;;
	esac
	printf '%s: %s\n' "$h" "$v"
	printf '%s: Bonding Mode: %s\n' "$h" "$mode"
done
exit 0
STUB
	cat > "$S/root_user/$TU/myrepo/.git_upload.sh" <<'STUB'
#!/bin/bash
echo "git_upload $* cwd=$PWD" >> "$STUBLOG/calls.log"
exit 0
STUB
	chmod +x "$W"/awxkit/*.sh "$S"/bin/* "$S/root_user/$TU/myrepo/.git_upload.sh"
}

# 입력 데이터 시딩
seed_nodeinfo() {   # 데이터함수 → nodeinfo DB + 호스트명만 있는 ${TU}.txt
	"$1" > "$S/db.txt"
	export NODEINFO_DB=$S/db.txt
	awk '{print $4}' "$S/db.txt" > "$W/$TU.txt"
}
seed_raw() { "$1" > "$W/$TU.txt"; }   # 12필드 원본을 ${TU}.txt 에 직접

# ===================== 실행기 =====================
run01() {   # "입력(printf %b)"  → RC / $OUT / $CALLS
	RUN01_COUNT=$((RUN01_COUNT+1))
	( cd "$W" && printf '%b' "$1" | TMPDIR="$S/tmp" timeout "${RUN_TIMEOUT:-180}" bash "./$F01" 2>&1 | cat > "$S/out.txt"; echo "${PIPESTATUS[1]}" > "$S/rc" )
	RC=$(cat "$S/rc")
}
run02() {   # "입력" 인자...   (02 직접 실행)
	local input=$1; shift
	RUN02_COUNT=$((RUN02_COUNT+1))
	( cd "$W" && printf '%b' "$input" | TMPDIR="$S/tmp" timeout "${RUN_TIMEOUT:-60}" bash "./$F02" "$@" 2>&1 | cat > "$S/out.txt"; echo "${PIPESTATUS[1]}" > "$S/rc" )
	RC=$(cat "$S/rc")
}

# FIFO 로 stdin 공급(프롬프트에서 대기시키는 용도). job control 로 SIGINT 무시 상태를 피한다.
fifo_start() {
	RUN01_COUNT=$((RUN01_COUNT+1))
	rm -f "$S/in"; mkfifo "$S/in"
	set -m
	{ ( cd "$W" && exec env TMPDIR="$S/tmp" bash "./$F01" < "$S/in" > "$S/out.txt" 2>&1 ) & } 2>/dev/null
	FPID=$!
	set +m
	exec 9> "$S/in"
}
fifo_wait() {   # 정규식 초
	local t
	for ((t = 0; t < $2 * 5; t++)); do
		grep -Eq -- "$1" "$S/out.txt" 2>/dev/null && return 0
		sleep 0.2
	done
	return 1
}
fifo_alive() { kill -0 "$FPID" 2>/dev/null; }
fifo_finish() {   # 최대 초 대기 → 살아 있으면 KILL. FRC=종료코드, FSTUCK=1(안 죽음)
	local t; FSTUCK=0
	{ exec 9>&-; } 2>/dev/null
	for ((t = 0; t < $1 * 5; t++)); do fifo_alive || break; sleep 0.2; done
	if fifo_alive; then FSTUCK=1; kill -KILL "$FPID" 2>/dev/null; fi
	wait "$FPID" 2>/dev/null; FRC=$?
	FPID=""
	sleep 1   # tee(프로세스 치환) 마무리 대기
}

# ===================== 데이터셋 =====================
mkln() {   # infra host nic disk part cap os boot   (vendor/model 고정, IP·MAC 자동)
	IPC=$((IPC+1))
	printf 'Dell R750 %s %s 10.9.0.%d aa:bb:cc:dd:ee:%02x %s %s %s %s %s %s\n' "$1" "$2" "$IPC" "$IPC" "$3" "$4" "$5" "$6" "$7" "$8"
}
D1() {   # MAC 보정 · sed 치환 샘플(7줄)
	cat <<'EOF'
Dell R750 INFRA-A spice01 10.1.0.1 aa:bb:cc:dd:ee:01 eth0 sda sda5 1.1T RHEL8 UEFI
Dell R750 INFRA-A dspr05 10.1.0.2 aa:bb:cc:dd:ee:FF eth0 sda sda5 1.1T RHEL8 UEFI
Dell R750 INFRA-A pspr02 10.1.0.3 aa:bb:cc:dd:ee:0b eth0 sda sda5 1.1T RHEL8 UEFI
Dell R750 INFRA-A spice01ev02 10.1.0.4 aa:bb:cc:dd:ee:01 eth0 sda sda5 1.1T RHEL8 UEFI
test1234 R750 INFRA-B node01 10.1.0.5 aa:bb:cc:dd:ee:01 eth1 sdb sdb1 7T RHEL9 레거시
Dell R750 INFRA-A pice03 10.1.0.6 aa:bb:cc:dd:ee:09 eth0 sda sda5 1.1T RHEL8 UEFI
Dell R750 INFRA-A spice08 10.1.0.7 aa:bb:cc:dd:ee:08 eth0 sda sda5 1.1T RHEL8 UEFI
EOF
}
D1_EXPECT() {
	cat <<'EOF'
Dell R750 INFRA-A spice01 10.1.0.1 aa:bb:cc:dd:ee:00 eth0 sda sda5 1200 RHEL8 UEFI
Dell R750 INFRA-A dspr05 10.1.0.2 aa:bb:cc:dd:ee:FE eth0 sda sda5 1200 RHEL8 UEFI
Dell R750 INFRA-A pspr02 10.1.0.3 aa:bb:cc:dd:ee:0a eth0 sda sda5 1200 RHEL8 UEFI
Dell R750 INFRA-A spice01ev02 10.1.0.4 aa:bb:cc:dd:ee:01 eth0 sda sda5 1200 RHEL8 UEFI
offchip R750 INFRA-B node01 10.1.0.5 aa:bb:cc:dd:ee:01 eth1 sdb sdb1 7600 RHEL9 레거시
Dell R750 INFRA-A pice03 10.1.0.6 aa:bb:cc:dd:ee:08 eth0 sda sda5 1200 RHEL8 UEFI
Dell R750 INFRA-A spice08 10.1.0.7 aa:bb:cc:dd:ee:08 eth0 sda sda5 1200 RHEL8 UEFI
EOF
}
DN() {   # DN n : host01..hostNN
	local i
	for ((i = 1; i <= $1; i++)); do
		printf 'Dell R750 INFRA-A host%02d 10.2.0.%d aa:bb:cc:dd:ee:%02x eth0 sda sda5 1.1T RHEL8 UEFI\n' "$i" "$i" "$i"
	done
}
D3() {   # 10개 그룹 · 14대
	IPC=0
	mkln INFRA-A hostA01 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln INFRA-A hostA02 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln INFRA-A nodeev03 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln INFRA-A srv04 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln INFRA-B hostA05 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln INFRA-A hostA06 eth1 sda sda5 1.1T RHEL8 UEFI
	mkln INFRA-A hostA07 eth0 nvme0n1 nvme0n1p1 1.1T RHEL8 UEFI
	mkln INFRA-A hostA08 eth0 sda sda5 7T RHEL8 UEFI
	mkln INFRA-A hostA09 eth0 sda sda5 1.1T RHEL9 UEFI
	mkln INFRA-A hostA10 eth0 sda sda5 1.1T RHEL8 레거시
	mkln INFRA-A hostA11 eth0 sda sda5 1.1T RHEL8 BIOS
	mkln INFRA-A hostA12 eth0 sda sda4 1.1T RHEL8 UEFI
	mkln INFRA-A srv13 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln INFRA-A srvev14 eth0 sda sda5 1.1T RHEL8 UEFI
}
D5() {   # dhcp_pool 용: MAC 보정이 반영되는 spice01 포함
	cat <<'EOF'
Dell R750 INFRA-A spice01 10.5.0.1 aa:bb:cc:dd:ee:01 eth0 sda sda5 1.1T RHEL8 UEFI
Dell R750 INFRA-A host02 10.5.0.2 aa:bb:cc:dd:ee:02 eth0 sda sda5 1.1T RHEL8 UEFI
EOF
}
D6() {   # 단일 그룹 3대
	IPC=0
	mkln INFRA-A host01 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln INFRA-A host02 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln INFRA-A host03 eth0 sda sda5 1.1T RHEL8 UEFI
}
D7() {   # 3개 그룹(IA/IB/IC) · 4대
	IPC=0
	mkln IA hostA1 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln IA hostA2 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln IB evB1 eth0 sda sda5 1.1T RHEL9 BIOS
	mkln IC srvC1 eth0 sda sda5 1.1T RHEL8 레거시
}

yml_list() { ls "$W/svr_dir" 2>/dev/null | grep '\.yml$' | LC_ALL=C sort; }
oracle_cols() {   # n : 20개씩 세로 → 가로 (독립 오라클)
	local n=$1 r c i line rows
	rows=$(( n < 20 ? n : 20 ))
	for ((r = 1; r <= rows; r++)); do
		line=""
		for ((c = 0; c * 20 + r <= n; c++)); do i=$((c * 20 + r)); line+="${line:+ }$(printf 'host%02d' "$i")"; done
		echo "$line"
	done
	echo "총 ${n}대"
}

# 02 호출 체인 검증: calls.log 의 invsync→dhcp→pxe 가 yml 마다 3개 1조로 이어지고 값이 EXP_* 와 일치하는지
declare -A EXP_INFRA EXP_OS EXP_BOOT EXP_SPL
verify_chain() {   # desc [skip-yml-prefix...]  (실패 호출로 건너뛴 단계는 호출자가 별도 검증)
	local desc=$1 types line p y n=0
	types=$(grep -E '^(invsync|dhcp|pxe) ' "$CALLS" | awk '{print $1}' | paste -sd' ')
	types=${types//pxe dhcp/dhcp pxe}   # dhcp/pxe 는 동시 실행이라 기록 순서 무관
	local expect_seq=""
	for p in "${!EXP_INFRA[@]}"; do expect_seq+="${expect_seq:+ }invsync dhcp pxe"; done
	(( ${#EXP_INFRA[@]} > 1 )) && expect_seq+=" invsync"   # 그룹 2개 이상이면 마지막에 전체 yml invsync
	t_eq "$desc: 호출 순서 invsync→dhcp→pxe 반복 (+ 전체 invsync)" "$types" "$expect_seq"
	while read -r line; do
		[[ $line =~ ^invsync\ -user\ $TU\ -file\ (([A-Za-z0-9]+)_inventory-[0-9]+_([0-9]+)ea\.yml)$ ]] || { CFAILS+=("$desc: invsync 형식 오류 [$line]"); continue; }
		y=${BASH_REMATCH[1]}; p=${BASH_REMATCH[2]}; n=${BASH_REMATCH[3]}
		[[ -n ${EXP_OS[$p]} ]] || { CFAILS+=("$desc: 예상 밖 yml $y"); continue; }
		t_eq "$desc: dhcp($p)" "$(grep -E "^dhcp .*" "$CALLS" | grep -F -- "-infra ${EXP_INFRA[$p]}" | head -1)" "dhcp -user $TU -infra ${EXP_INFRA[$p]}"
		t_eq "$desc: pxe($p)" "$(grep -E '^pxe ' "$CALLS" | grep -F -- "-infra ${EXP_INFRA[$p]} " | head -1)" "pxe -user $TU -infra ${EXP_INFRA[$p]} -os ${EXP_OS[$p]} -boot ${EXP_BOOT[$p]} -splunk ${EXP_SPL[$p]}"
	done < <(grep '^invsync ' "$CALLS")
}

# ===================== shellcheck 단계 =====================
step_shellcheck() {
	case_begin "SC" "shellcheck -S warning 01/02 (원본)"
	if command -v shellcheck >/dev/null 2>&1; then
		local out rc
		out=$(cd "$SRC" && shellcheck -S warning "$F01" "$F02" 2>&1); rc=$?
		if [[ $rc -eq 0 ]]; then CEV+=("shellcheck $(shellcheck --version | sed -n 's/^version: //p') 경고 0건")
		else CFAILS+=("shellcheck rc=$rc"); printf '%s\n' "$out" | sed 's/^/           | /'; fi
		case_end
	else
		printf '%-10s %-8s %s\n' "SKIP" "SC" "shellcheck 미설치"
		ROWS+=("SKIP${SEP}SC${SEP}shellcheck 미설치${SEP}")
	fi
}

# ===================== 케이스 1: 12필드 변환 + MAC 보정 =====================
case1() {
	local mode
	for mode in msg plain; do
		case_begin "1-$mode" "nodeinfo(${mode}) → ${TU}.txt 12필드 변환 + MAC 보정"
		setup_case
		seed_nodeinfo D1; export NODEINFO_MODE=$mode
		run01 'Y\nN\n'
		t_rc "01 종료코드(작업진행 N)" "$RC" 0
		t_has "nodeinfo -hosts 절대경로 전달" "$CALLS" "^nodeinfo -user $TU -hosts $W/$TU\.txt$"
		t_eq "${TU}.txt 전체(12필드·치환·MAC) 일치" "$(cat "$W/$TU.txt")" "$(D1_EXPECT)"
		t_has "spice01 :01→:00" "$W/$TU.txt" ' spice01 10\.1\.0\.1 aa:bb:cc:dd:ee:00 '
		t_has "dspr05 :FF→:FE" "$W/$TU.txt" ' dspr05 10\.1\.0\.2 aa:bb:cc:dd:ee:FE '
		t_has "pspr02 :0b→:0a" "$W/$TU.txt" ' pspr02 10\.1\.0\.3 aa:bb:cc:dd:ee:0a '
		t_has "spice01ev02 불변" "$W/$TU.txt" ' spice01ev02 10\.1\.0\.4 aa:bb:cc:dd:ee:01 '
		t_has "node01 불변(MAC)·sed 3종(7T→7600, test1234→offchip)" "$W/$TU.txt" '^offchip .* node01 .*:01 eth1 sdb sdb1 7600 '
		t_no "msg/잡음 줄 제거" "$W/$TU.txt" 'msg|PLAY|ok:|[{}]'
		t_eq "필드 수 12 아닌 줄 수" "$(awk 'NF != 12' "$W/$TU.txt" | wc -l)" 0
		t_eq "작업진행 N → ssh/scp 미호출" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
		t_has "awxkit/output 산출물 보존" "$W/awxkit/output/${TU}_nodeinfo.yaml" 'INFRA-A'
		t_notmp
		case_end
	done
	case_begin "1-raw" "awx N: 기존 12필드 ${TU}.txt → sed/MAC 보정 후 12필드 유지"
	setup_case
	seed_raw D1
	run01 'N\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "${TU}.txt 전체 일치" "$(cat "$W/$TU.txt")" "$(D1_EXPECT)"
	t_eq "awx N → nodeinfo 미호출" "$(grep -c '^nodeinfo ' "$CALLS")" 0
	t_has "등록 대상 하단 총 대수" "$OUT" '^총 7대$'
	t_notmp
	case_end
}

# ===================== 케이스 2: 다단 출력 =====================
case2() {
	local n blk
	for n in 20 21 45; do
		case_begin "2-$n" "등록 대상 ${n}대 다단 출력 (01 stdout)"
		setup_case
		DN "$n" > "$W/$TU.txt"
		run01 'N\nN\n'
		blk=$(awk '/\[5\] show_targets/ {f=1; next} f {print} /^총 / {exit}' "$OUT")
		t_rc "01 종료코드" "$RC" 0
		t_eq "행·열 전체 일치" "$blk" "$(oracle_cols "$n")"
		CEV+=("1행: $(sed -n 1p <<< "$blk")")
		[[ $n -eq 45 ]] && t_eq "45대 1행" "$(sed -n 1p <<< "$blk")" "host01 host21 host41"
		t_eq "마지막 줄" "$(tail -1 <<< "$blk")" "총 ${n}대"
		t_notmp
		case_end
	done
}

# ===================== 케이스 3/4: 그룹 분할 · yml 수집 · git · ls =====================
case34() {
	local f sets exp_sets sizes pxe_exp
	setup_case
	seed_raw D3
	cp "$W/$TU.txt" "$S/orig_input.txt"
	run01 'N\nY\n\nls\nnofile.yml\nsu\n1\nY\n'

	case_begin "3" "그룹 수=고유 조합 수(10), ev/s/그 외 분리, 레거시→legacy (D3: 14대)"
	t_rc "01 종료코드" "$RC" 0
	t_eq "scp 가 받은 파일 수(그룹 10 + all 1)" "$(ls "$REMOTE_DIR" | grep -c "^${TU}_.*\.yaml$")" 11
	sets=$(for f in "$REMOTE_DIR/${TU}"_[0-9]*.yaml; do awk '{print $4}' "$f" | LC_ALL=C sort | paste -sd,; done | LC_ALL=C sort)
	exp_sets=$(printf '%s\n' hostA01,hostA02,hostA12 nodeev03,srvev14 srv04,srv13 hostA05 hostA06 hostA07 hostA08 hostA09 hostA10 hostA11 | LC_ALL=C sort)
	t_eq "그룹 파일 구성(호스트 집합 10개)" "$sets" "$exp_sets"
	t_eq "all 파일 = 가공된 ${TU}.txt" "$(cat "$REMOTE_DIR/${TU}_all.yaml")" "$(cat "$W/$TU.txt")"
	t_eq "scp 호출 1회, 목적지" "$(grep '^scp ' "$CALLS" | awk '{print $NF}')" "repo.lab:/root/Inventory/"
	t_eq "scp 인자의 입력 파일 수 11" "$(grep '^scp ' "$CALLS" | grep -o "${TU}_[^ ]*\.yaml" | wc -l)" 11
	t_eq "custom_inventory.sh ssh 호출 11회" "$(grep -c '^ssh repo.lab bash /root/Inventory/custom_inventory.sh /root/Inventory/' "$CALLS")" 11
	t_eq "마지막 custom_inventory 호출은 all" "$(grep 'custom_inventory.sh' "$CALLS" | tail -1 | awk '{print $NF}')" "/root/Inventory/${TU}_all.yaml"
	pxe_exp=$(printf '%s\n' \
		"-infra infra-a -os 2026-ECAD_TCAD -boot BIOS -splunk On-premise" \
		"-infra infra-a -os 2026-ECAD_TCAD -boot legacy -splunk On-premise" \
		"-infra infra-a -os 2026-ECAD_TCAD -boot UEFI -splunk Cloud" \
		"-infra infra-a -os 2026-ECAD_TCAD -boot UEFI -splunk no" \
		"-infra infra-a -os 2026-ECAD_TCAD -boot UEFI -splunk On-premise" \
		"-infra infra-a -os 2026-ECAD_TCAD -boot UEFI -splunk On-premise" \
		"-infra infra-a -os 2026-ECAD_TCAD -boot UEFI -splunk On-premise" \
		"-infra infra-a -os 2026-ECAD_TCAD -boot UEFI -splunk On-premise" \
		"-infra infra-a -os 2026-ECAD_TCAD -boot UEFI -splunk On-premise" \
		"-infra infra-b -os 2026-ECAD_TCAD -boot UEFI -splunk On-premise" | LC_ALL=C sort)
	t_eq "pxe 옵션 10건(ev→no, s→Cloud, 레거시→legacy)" "$(grep '^pxe ' "$CALLS" | sed "s/^pxe -user $TU //" | LC_ALL=C sort)" "$pxe_exp"
	t_no "pxe 에 한글 레거시 원문 미전달" "$CALLS" '^pxe .*레거시'
	t_eq "02 invsync = 그룹 10 + 전체 1" "$(grep -c '^invsync ' "$CALLS")" 11
	t_eq "all yml(14ea) invsync 는 마지막 1회뿐" "$(grep '^invsync ' "$CALLS" | grep -c '_14ea\.yml')|$(grep '^invsync ' "$CALLS" | tail -1 | grep -c '_14ea\.yml')" "1|1"
	t_eq "dhcp/pxe 는 그룹 yml 만 (10회씩)" "$(grep -c '^dhcp ' "$CALLS")|$(grep -c '^pxe ' "$CALLS")" "10|10"
	t_notmp
	case_end

	case_begin "4" "yml 수집=그룹+1(11), git 스텁 호출 수=yml 수, 메뉴 ls '파일 [boot]'"
	t_eq "svr_dir yml 수" "$(yml_list | wc -l)" 11
	sizes=$(yml_list | sed 's/.*_\([0-9]*\)ea\.yml$/\1/' | sort -n | paste -sd' ')
	t_eq "yml 대수(_Nea) 목록" "$sizes" "1 1 1 1 1 1 1 2 2 3 14"
	t_eq "git 스텁 호출 수 == yml 수" "$(grep -c '^git_upload ' "$CALLS")" "$(yml_list | wc -l)"
	t_eq "git 호출 인자 yml 집합 == svr_dir" "$(grep '^git_upload ' "$CALLS" | awk '{print $3}' | LC_ALL=C sort)" "$(yml_list)"
	t_eq "git 은 myrepo 안에서 실행" "$(grep '^git_upload ' "$CALLS" | grep -vc "cwd=$S/root_user/$TU/myrepo$")" 0
	t_eq "myrepo 로 복사된 yml == svr_dir" "$(ls "$S/root_user/$TU/myrepo" | grep '\.yml$' | LC_ALL=C sort)" "$(yml_list)"
	local ls_lines
	ls_lines=$(grep -E '^[^ ]+\.yml \[[^]]*\]$' "$OUT")
	t_eq "ls 출력 줄 수" "$(grep -c . <<< "$ls_lines")" 11
	t_eq "ls 출력 파일명 집합 == svr_dir" "$(awk '{print $1}' <<< "$ls_lines" | LC_ALL=C sort)" "$(yml_list)"
	t_eq "ls [UEFI] 8 / [legacy] 1 / [BIOS] 1 / [all] 1" "$(for b in UEFI legacy BIOS all; do printf '%s=%s ' "$b" "$(grep -c "\[$b\]\$" <<< "$ls_lines")"; done)" "UEFI=8 legacy=1 BIOS=1 all=1 "
	tail -1 <<< "$ls_lines" > "$S/ls_last.txt"
	t_has "ls 마지막(전체) 줄이 [all]" "$S/ls_last.txt" '_14ea\.yml \[all\]$'
	t_has "메뉴 없는 파일명 안내" "$OUT" '^\[!\] nofile\.yml 없음$'
	ls "$W/tmp" > "$S/tmp_ls.txt"
	t_has "git 이후 cd 복귀(상대경로 tmp/all_${TU} 생성)" "$S/tmp_ls.txt" "^all_${TU}\$"
	case_end
}

# ===================== 케이스 5: dhcp_pool =====================
case5() {
	local exp
	case_begin "5" "dhcp_pool 줄 형식 · 2회 실행 누적"
	setup_case
	seed_raw D5
	exp=$(printf '%s\n' \
		"tmp tmp SEC spice01 10.5.0.1 aa:bb:cc:dd:ee:00 eth0 sda sda5 960 7.9" \
		"tmp tmp SEC host02 10.5.0.2 aa:bb:cc:dd:ee:02 eth0 sda sda5 960 7.9")
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "1회차 종료코드" "$RC" 0
	t_eq "1회차 dhcp_pool 내용(MAC 보정 반영)" "$(cat "$W/dhcp_pool/dhcp_pool_delete_info.txt")" "$exp"
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "2회차 종료코드" "$RC" 0
	t_eq "2회차 누적(4줄 = 기존+동일 2줄)" "$(cat "$W/dhcp_pool/dhcp_pool_delete_info.txt")" "$exp"$'\n'"$exp"
	t_notmp
	case_end
}

# ===================== 케이스 6: LDAP / LACP / 응답없음 / AI =====================
case6() {
	# 6-same : LDAP 동일 + AI 일치(host02)
	case_begin "6-same" "LDAP 동일 한 줄 요약 + ai_server_list 일치(host02) → power limit 안내"
	setup_case ai_server_list='host02|zz99'
	seed_raw D6; export GOSSH_SCENARIO=same
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "LDAP 동일 요약" "$OUT" '^모든 호스트의 LDAP이 INFO LDAP INFRA1 SITE1으로 동일함$'
	t_has "AI power limit 안내" "$OUT" '^AI GPU서버는 power limit설정이 필요합니다'
	t_has ".power_limit_setting.txt 내용 출력" "$OUT" '^POWERLIMIT_SENTINEL$'
	t_no "LACP 문구 없음" "$OUT" 'LACP-COMMENT-SENTINEL'
	t_no "응답 없음 없음" "$OUT" '^응답 없음'
	t_eq "gossh 인자" "$(grep '^gossh ' "$CALLS" | sed 's#-w [^ ]* #-w HOSTFILE #')" "gossh -script -w HOSTFILE cat /etc/openldap/ldap.conf |grep -v '#' |grep -i uri |awk -F= '{print \$2}' |awk -F',' '{print \$1}';cat /proc/net/bonding/bond0 |grep -i mod"
	t_eq "gossh -w 호스트파일 내용" "$(cat "$STUBLOG/gossh_hosts.txt")" "$(printf 'host01\nhost02\nhost03')"
	t_has "tmp/all_${TU} 보존" "$W/tmp/all_${TU}" '^host01: INFO'
	t_has "02 작업 리스트(| 가로)" "$OUT" '^host01\|host02\|host03$'
	t_notmp
	case_end

	# 6-diff : LDAP 상이 + AI 비일치
	case_begin "6-diff" "LDAP 상이 → 값별 호스트 나열, ai_server_list 비일치 → 안내 없음"
	setup_case ai_server_list='zzz01|zzz02'
	seed_raw D6; export GOSSH_SCENARIO=diff
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "값1: host01" "$OUT" '^INFO LDAP INFRA1 SITE1 : host01$'
	t_has "값2: host02 host03" "$OUT" '^INFO LDAP INFRA9 SITE9 : host02 host03$'
	t_no "동일 요약 없음" "$OUT" '동일함'
	t_no "AI 안내 없음" "$OUT" 'AI GPU서버|POWERLIMIT_SENTINEL'
	t_notmp
	case_end

	# 6-lacp : LACP 혼재 + ai_server_list 비어 있음
	case_begin "6-lacp" "LACP 혼재 → 호스트 나열 + lacp_comment, ai_server_list 빈 값도 오류 없이 진행"
	setup_case ai_server_list=''
	seed_raw D6; export GOSSH_SCENARIO=lacp
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "LACP 호스트 줄 + 코멘트 줄" "$(grep -A1 -x 'host01 host02' "$OUT" | paste -sd'/')" "host01 host02/LACP-COMMENT-SENTINEL"
	t_has "LDAP 동일 요약 병행" "$OUT" '^모든 호스트의 LDAP이 .*동일함$'
	t_no "AI 안내 없음" "$OUT" 'AI GPU서버'
	t_no "[X] 오류 없음" "$OUT" '^\[X\]'
	t_notmp
	case_end

	# 6-noresp : 응답 없음 + AI 접두어만 일치(정확 일치 아님)
	case_begin "6-noresp" "응답 없음 호스트 보고, ai_server_list 접두어(host0) 는 정확 일치 아님 → 안내 없음"
	setup_case ai_server_list='host0'
	seed_raw D6; export GOSSH_SCENARIO=noresp
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "응답 없음 : host03" "$OUT" '^응답 없음 : host03$'
	t_no "AI 안내 없음(접두어 불일치)" "$OUT" 'AI GPU서버'
	t_no "LACP 문구 없음" "$OUT" 'LACP-COMMENT-SENTINEL'
	t_notmp
	case_end
}

# ===================== 케이스 7: 02 =====================
case7() {
	local cnt
	# 7a: 전체 성공
	case_begin "7a" "02: 그룹 yml 수만큼 invsync→dhcp→pxe, 올바른 -infra/-os/-boot/-splunk"
	setup_case
	seed_raw D7
	EXP_INFRA=([IA]=ia [IB]=ib [IC]=ic); EXP_OS=([IA]=2026-ECAD_TCAD [IB]=2026-ECAD_TCAD [IC]=2026-ECAD_TCAD)
	EXP_BOOT=([IA]=UEFI [IB]=BIOS [IC]=legacy); EXP_SPL=([IA]=On-premise [IB]=no [IC]=Cloud)
	run01 'N\nY\nls\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	verify_chain "7a"
	t_eq "invsync 3회 + 전체 1회" "$(grep -c '^invsync ' "$CALLS")" 4
	t_eq "전체 yml invsync 는 마지막, dhcp/pxe 는 3회뿐" "$(grep '^invsync ' "$CALLS" | tail -1 | grep -c '_4ea\.yml')|$(grep -c '^dhcp ' "$CALLS")|$(grep -c '^pxe ' "$CALLS")" "1|3|3"
	t_has "최종 수량 HPC(On-premise)" "$OUT" '^HPC ia 2026-ECAD_TCAD : 2대$'
	t_has "최종 수량 SDS(Cloud)" "$OUT" '^SDS ic 2026-ECAD_TCAD : 1대$'
	t_has "최종 수량 no(ev)" "$OUT" '^no ib 2026-ECAD_TCAD : 1대$'
	t_has "최종 수량 합계" "$OUT" '^합계 : 4대$'
	t_eq "invsync -file 의 _Nea (IA=2,IB=1,IC=1, 전체 IA=4)" "$(grep '^invsync ' "$CALLS" | sed 's/.*-file \(I.\)_inventory-[0-9]*_\([0-9]*\)ea.yml/\1=\2/' | paste -sd' ')" "IA=2 IB=1 IC=1 IA=4"
	t_has "확인표 헤더" "$OUT" '^번호 \| yml \| infra \| os \| boot \| splunk \| 호스트 수$'
	t_has "확인표 IC 행(infra 소문자화)" "$OUT" '^3 \| IC_inventory-[0-9]+_1ea\.yml \| ic \| 2026-ECAD_TCAD \| legacy \| Cloud \| 1$'
	t_has "요약" "$OUT" '요약 : 전체 3 / 성공 3 / 실패 0'
	t_notmp
	case_end

	# 7b: 확인표 N → 수동 값 (Enter=유지)
	case_begin "7b" "02: 확인표 N → 수동 값 반영(Enter=유지)"
	setup_case
	seed_raw D7
	EXP_INFRA=([IA]=ia [IB]=ibx [IC]=ic); EXP_OS=([IA]=RHEL7 [IB]=2026-ECAD_TCAD [IC]=2026-ECAD_TCAD)
	EXP_BOOT=([IA]=UEFI [IB]=UEFI [IC]=legacy); EXP_SPL=([IA]=On-premise [IB]=Cloud [IC]=Cloud)
	run01 'N\nY\nls\nsu\n1\nN\n''\nRHEL7\n\n\n''IBX\n\nUEFI\nCloud\n''\n\n\n\n'
	t_rc "01 종료코드" "$RC" 0
	verify_chain "7b"
	t_has "옵션 확정 출력" "$OUT" '^옵션 확정$'
	t_has "확정표 IA 행(수동 os)" "$OUT" '^1 \| IA_inventory-[0-9]+_2ea\.yml \| ia \| RHEL7 \| UEFI \| On-premise \| 2$'
	t_has "확정표 IB 행(수동 infra/boot/splunk)" "$OUT" '^2 \| IB_inventory-[0-9]+_1ea\.yml \| ibx \| 2026-ECAD_TCAD \| UEFI \| Cloud \| 1$'
	t_notmp
	case_end

	# 7c: 중간 실패 → 요약 + 01 재시도 N → exit 1
	case_begin "7c" "02 중간 실패(IB dhcp): 요약 + 나머지 yml 진행, 01 재시도 N → exit 1"
	setup_case
	seed_raw D7
	export FAIL_CALL="^dhcp -user $TU -infra ib$"
	run01 'N\nY\nls\nsu\n1\nY\nN\n'
	t_rc "01 종료코드 1" "$RC" 1
	t_has "요약 줄" "$OUT" '요약 : 전체 3 / 성공 2 / 실패 1'
	t_has "실패 항목" "$OUT" '^실패 \(dhcp\) : IB_inventory-[0-9]+_1ea\.yml$'
	t_eq "IB pxe 는 dhcp 실패와 무관하게 동시 실행됨" "$(grep -c '^pxe .*-infra ib ' "$CALLS")" 1
	t_has "IC 는 계속 진행(pxe)" "$CALLS" '^pxe -user testuser -infra ic '
	t_eq "invsync 3회(재시도 없음, 전체 yml invsync 생략)" "$(grep -c '^invsync ' "$CALLS")" 3
	t_has "전체 yml 갱신 생략 경고" "$OUT" '전체 yml\(.*\) 인벤토리 소스 갱신은 건너뜁니다'
	t_eq "02 실패 → auto_setup queue 파일 없음" "$(find "$S/as/queue" -name '*.job' 2>/dev/null | wc -l)" 0
	t_no "02 실패 → 전달 로그 없음" "$OUT" 'auto_setup 전달'
	t_notmp
	case_end

	# 7f: 재시도 Y → 성공
	case_begin "7f" "02 1회 실패 후 01 재시도 Y → 전체 성공, exit 0"
	setup_case
	seed_raw D7
	export FAIL_CALL="^dhcp -user $TU -infra ib$" FAIL_ONCE_FILE=$S/failonce
	: > "$S/failonce"
	run01 'N\nY\nls\nsu\n1\nY\nY\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "작업 리스트 2회 출력" "$(grep -c '^작업 리스트$' "$OUT")" 2
	t_eq "invsync 7회(1차 3 + 재시도 3 + 전체 1)" "$(grep -c '^invsync ' "$CALLS")" 7
	t_has "최종 요약 성공" "$OUT" '요약 : 전체 3 / 성공 3 / 실패 0'
	local qf; qf=$(find "$S/as/queue" -name '*.job' 2>/dev/null)
	t_eq "auto_setup queue 파일 1개(재시도해도 성공 시 1회만)" "$(grep -c . <<< "$qf")" 1
	basename "$qf" > "$S/qname"; sed -n 1p "$qf" > "$S/q1"; sed -n 2p "$qf" > "$S/q2"
	t_has "queue 파일명 <epoch>_<user>_<pid>.job" "$S/qname" "^[0-9]+_${TU}_[0-9]+\\.job$"
	t_eq "queue 파일 1행 user=" "$(cat "$S/q1")" "user=$TU"
	t_has "queue 파일 2행 time=<epoch>" "$S/q2" '^time=[0-9]+$'
	t_eq "queue 파일 호스트명 줄(정렬)" "$(sed -n '3,$p' "$qf" | grep -v '=' | LC_ALL=C sort | paste -sd' ')" "evB1 hostA1 hostA2 srvC1"
	t_eq "queue .tmp 잔여 없음" "$(find "$S/as/queue" -name '*.tmp' | wc -l)" 0
	t_has "전달 로그 1줄(4대)" "$OUT" 'auto_setup 전달 : 4대 \('
	# --- 2차: 그룹 줄 (호스트 줄 뒤에 추가, 기존 줄·순서 불변) ---
	t_eq "그룹 줄 수(yml= 3 + all= 1)" "$(grep -c '^yml=' "$qf")|$(grep -c '^all=' "$qf")" "3|1"
	t_eq "그룹 줄은 호스트 줄 뒤(1~2행 user/time, 3~6행 호스트, 7행~ 그룹)" "$(sed -n '3,6p' "$qf" | grep -c '=')|$(sed -n '7,$p' "$qf" | grep -vc '=')" "0|0"
	t_has "그룹 IA 줄(infra/os/boot/splunk/hosts)" "$qf" '^yml=IA_inventory-[0-9]+_2ea\.yml infra=IA os=RHEL8 boot=UEFI splunk=On-premise hosts=hostA1,hostA2$'
	t_has "그룹 IB 줄" "$qf" '^yml=IB_inventory-[0-9]+_1ea\.yml infra=IB os=RHEL9 boot=BIOS splunk=no hosts=evB1$'
	t_has "그룹 IC 줄(레거시→legacy)" "$qf" '^yml=IC_inventory-[0-9]+_1ea\.yml infra=IC os=RHEL8 boot=legacy splunk=Cloud hosts=srvC1$'
	t_has "all= 전체 yml(4대)" "$qf" '^all=IA_inventory-[0-9]+_4ea\.yml$'
	t_eq "그룹 hosts= 합집합 == 호스트 줄" "$(grep '^yml=' "$qf" | sed 's/.* hosts=//' | tr ',' '\n' | LC_ALL=C sort | paste -sd' ')" "$(sed -n '3,6p' "$qf" | LC_ALL=C sort | paste -sd' ')"
	t_notmp
	case_end

	# 7m: queue 디렉터리 생성 불가 → 경고만, 진행·종료코드 영향 없음
	case_begin "7m" "auto_setup queue 생성 불가 → 경고 1줄만, 01 은 정상 완료(exit 0)"
	setup_case
	seed_raw D7
	: > "$S/as"   # 일반 파일이라 $S/as/queue 생성 불가
	run01 'N\nY\nls\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "전달 실패 경고" "$OUT" '^\[!\] auto_setup 전달 실패 \(.*/as/queue 생성 불가\)$'
	t_has "02 성공 후 등록 후 확인 단계 진행" "$OUT" '\[15\] verify_hosts'
	t_has "완료 출력" "$OUT" '완료'
	t_notmp
	case_end

	# 7n/7o: auto_setup_host 채움(복사본 sed) + 스텁 gossh → 원격 queue 전송 (로컬 queue 없음, 실패 시 경고만)
	remote_stub() {   # 원본 스텁 gossh 는 유지하고 AUTO_SETUP_OK 명령만 가로채 "원격"($S/rq)에서 실제로 실행
		mv "$S/bin/gossh" "$S/bin/gossh.orig"
		cat > "$S/bin/gossh" <<'STUB'
#!/bin/bash
cmd=${@: -1}
if [[ $cmd == *AUTO_SETUP_OK* ]]; then
	hf=""; a=("$@")
	for ((i = 0; i < ${#a[@]}; i++)); do [[ ${a[i]} == -w ]] && hf=${a[i+1]}; done
	echo "gossh-autosetup host=$(grep . "$hf") $*" >> "$STUBLOG/calls.log"
	[[ -n $RSTUB_FAIL ]] && { echo "ERROR $(grep . "$hf"): connect timeout" >&2; exit 1; }
	out=$(AUTO_SETUP_DIR="$RSTUB_DIR" bash -c "$cmd") && printf '%s: %s\n' "$(grep . "$hf")" "$out"
	exit 0
fi
exec "$(dirname "$0")/gossh.orig" "$@"
STUB
		chmod +x "$S/bin/gossh"
		export RSTUB_DIR=$S/remote_as
	}
	case_begin "7n" "auto_setup_host 채움: gossh 로 원격 queue 에 원자 전송(로컬 queue 없음), 내용은 로컬 전달과 동일, 로그 1줄"
	setup_case
	seed_raw D7
	run01 'N\nY\nls\nsu\n1\nY\n'     # 로컬 전달(기준)
	local lq; lq=$(find "$S/as/queue" -name '*.job' 2>/dev/null)
	setup_case
	seed_raw D7
	sed -i 's#^auto_setup_host=""#auto_setup_host="os8.lab"#' "$W/$F01"
	t_eq "복사본 주입 1건" "$(grep -c "^auto_setup_host=\"os8.lab\"" "$W/$F01")" 1
	remote_stub
	run01 'N\nY\nls\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "로컬 queue 없음" "$(find "$S/as" -name '*.job' 2>/dev/null | wc -l)" 0
	local rf; rf=$(find "$S/remote_as/queue" -name '*.job' 2>/dev/null)
	t_eq "원격 queue 파일 1개" "$(grep -c . <<< "$rf")" 1
	basename "$rf" > "$S/qname"
	t_has "원격 파일명 <epoch>_<user>_<pid>.job" "$S/qname" "^[0-9]+_${TU}_[0-9]+\.job$"
	t_eq "원격 .tmp 잔여 없음" "$(find "$S/remote_as/queue" -name '.*' -type f | wc -l)" 0
	t_eq "원격 내용 == 로컬 전달 내용(time=·yml 파일명 epoch 제외)" "$(grep -v '^time=' "$rf" | sed 's/inventory-[0-9]*/inventory-N/g')" "$(grep -v '^time=' "$lq" | sed 's/inventory-[0-9]*/inventory-N/g')"
	t_has "원격 time=<epoch>" "$rf" '^time=[0-9]+$'
	t_eq "gossh 전송 호출 1회, 대상 os8.lab" "$(grep -c '^gossh-autosetup host=os8.lab ' "$CALLS")" 1
	t_eq "비밀번호 변수 비면 -p 없음" "$(grep -c "^gossh-autosetup host=os8.lab -script " "$CALLS")" 1
	t_has "전달 로그(4대 → 호스트)" "$OUT" 'auto_setup 전달 : 4대 → os8\.lab$'
	t_no "경고 없음" "$OUT" 'auto_setup 전달 실패'
	t_notmp
	case_end

	case_begin "7o" "auto_setup_host 채움 + gossh 전송 실패 → 경고 1줄만, 01 은 정상 완료(exit 0), 로컬 queue 없음"
	setup_case
	seed_raw D7
	sed -i 's#^auto_setup_host=""#auto_setup_host="os8.lab"#' "$W/$F01"
	t_eq "복사본 주입 1건" "$(grep -c "^auto_setup_host=\"os8.lab\"" "$W/$F01")" 1
	remote_stub
	export RSTUB_FAIL=1
	run01 'N\nY\nls\nsu\n1\nY\n'
	unset RSTUB_FAIL
	t_rc "01 종료코드" "$RC" 0
	t_eq "전달 실패 경고 1줄" "$(grep -c '^\[!\] auto_setup 전달 실패 (os8\.lab)$' "$OUT")" 1
	t_no "성공 로그 없음" "$OUT" 'auto_setup 전달 :'
	t_eq "로컬·원격 queue 모두 없음" "$(find "$S/as" "$S/remote_as" -name '*.job' 2>/dev/null | wc -l)" 0
	t_has "이후 등록 후 확인 단계 진행" "$OUT" '\[15\] verify_hosts'
	t_notmp
	case_end


	case_begin "7p" "auto_setup_gossh_pw 채움 → gossh 전송에 -p <비밀번호> 전달(비우면 -p 없음), 전송 성공"
	setup_case
	seed_raw D7
	sed -i -e 's#^auto_setup_host=""#auto_setup_host="os8.lab"#' -e 's#^auto_setup_gossh_pw=""#auto_setup_gossh_pw="pw 1!x"#' "$W/$F01"
	t_eq "복사본 주입 1건" "$(grep -c '^auto_setup_gossh_pw="pw 1!x"' "$W/$F01")" 1
	remote_stub
	run01 'N\nY\nls\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "gossh 전송 호출에 -p 'pw 1!x' (공백·특수문자 포함 한 인자)" "$(grep -c '^gossh-autosetup host=os8.lab -p pw 1!x -script ' "$CALLS")" 1
	t_has "전달 로그" "$OUT" 'auto_setup 전달 : 4대 → os8\.lab$'
	t_no "경고 없음" "$OUT" 'auto_setup 전달 실패'
	t_eq "비밀번호가 로그·화면에 출력되지 않음" "$(grep -c 'pw 1!x' "$OUT")" 0
	case_end
	# 7d: 02 직접 실행 + 실패
	case_begin "7d" "02 직접 실행: 첫 yml invsync 실패 → 다음 yml 진행 + 요약 + exit 1"
	setup_case
	export FAIL_CALL='^invsync .*-file X_inv-1_1ea\.yml$'
	run02 '1\nY\n' "$TU" X_inv-1_1ea.yml=I1,O1,B1,S1 Y_inv-2_1ea.yml=I2,O2,B2,S2
	t_rc "02 종료코드 1" "$RC" 1
	t_has "요약 줄" "$S/out.txt" '요약 : 전체 2 / 성공 1 / 실패 1'
	t_has "실패 invsync 항목" "$S/out.txt" '^실패 \(invsync\) : X_inv-1_1ea\.yml$'
	t_has "성공 항목" "$S/out.txt" '^성공 : Y_inv-2_1ea\.yml$'
	t_eq "X 의 dhcp/pxe 건너뜀" "$(grep -cE '^(dhcp|pxe) .*-infra i1' "$CALLS")" 0
	t_eq "Y 의 pxe 호출" "$(grep '^pxe ' "$CALLS")" "pxe -user $TU -infra i2 -os 2026-ECAD_TCAD -boot B2 -splunk S2"
	case_end

	# 7h: dhcp/pxe 동시 실행 + 둘 다 끝나야 다음 yml
	case_begin "7h" "02: dhcp·pxe 동시 실행, 둘 다 끝난 뒤에 다음 yml 진행"
	setup_case
	export STUB_SLEEP=1
	run02 '1\nY\n' "$TU" A_inv-1_1ea.yml=I1,O1,B1,S1 B_inv-2_1ea.yml=I2,O2,B2,S2 C_inv-3_1ea.yml=I3,O3,B3,S3
	t_rc "02 종료코드" "$RC" 0
	# invsync=I, dhcp/pxe 시작=S, 종료=E.  동시 실행이면 yml 마다 "I S S E E" (순차였다면 "I S E S E")
	t_eq "이벤트 순서(yml 3개)" "$(awk '$1=="start"&&$2=="invsync"{printf "I "} $1=="start"&&$2!="invsync"{printf "S "} $1=="end"&&$2!="invsync"{printf "E "}' "$STUBLOG/events.log" | sed 's/ $//')" "I S S E E I S S E E I S S E E"
	t_has "요약" "$S/out.txt" '요약 : 전체 3 / 성공 3 / 실패 0'
	t_eq "dhcp/pxe 둘 다 실패 시 한 항목으로 요약" "$(unset STUB_SLEEP; : > "$CALLS"; export FAIL_CALL='^(dhcp|pxe) '; run02 '1\nY\n' "$TU" Z_inv-1_1ea.yml=I1,O1,B1,S1 >/dev/null 2>&1; grep -c '^실패 (dhcp, pxe) : Z_inv-1_1ea.yml$' "$S/out.txt")" 1
	case_end

	# 7i: OS 버전 선택 — 2번(yml 별 선택)
	case_begin "7i" "02: OS 버전 2번 선택 → yml 별 번호가 pxe -os 에 반영, 잘못된 번호는 재질문"
	setup_case
	run02 '2\n3\n9\n4\n5\nY\n' "$TU" A_inv-1_1ea.yml=I1,O1,B1,S1 B_inv-2_1ea.yml=I2,O2,B2,S2 C_inv-3_1ea.yml=I3,O3,B3,S3
	t_rc "02 종료코드" "$RC" 0
	t_has "선택지 목록 출력" "$S/out.txt" '^  4\) 2026-OPC_MDP$'
	t_has "잘못된 번호(9) 재질문" "$S/out.txt" '^\[!\] 1~5 중에서 선택하세요$'
	t_eq "pxe -os (A=3 B=4 C=5)" "$(grep '^pxe ' "$CALLS" | sed 's/.*-os \([^ ]*\) .*/\1/' | paste -sd' ')" "2026 2026-OPC_MDP 2026-ECAD_TCAD"
	t_has "확인표에 선택 반영" "$S/out.txt" '^2 \| .*B_inv-2_1ea\.yml.* \| i2 \| 2026-OPC_MDP \| B2 \| S2 \| 1$'
	t_has "OS 질문이 옵션 확인표보다 먼저" "$S/out.txt" 'OS 버전 선택'
	t_eq "os 선택 질문이 확인표 헤더보다 앞" "$(grep -n 'OS 버전 선택\|^번호 | yml' "$S/out.txt" | head -2 | cut -d: -f2 | cut -c1-8 | paste -sd'|')" "OS 버전 선택|번호 | yml"
	case_end

	# 7j: 전체 yml(--all) invsync — 그룹 2개 이상일 때만, 모두 성공했을 때만
	case_begin "7j" "02: --all 전체 yml 은 그룹 2개 이상·전부 성공 시 마지막 invsync 만 수행(1개면 생략, 실패 시 exit 1)"
	setup_case
	run02 '1\nY\n' "$TU" A_inv-1_2ea.yml=I1,O1,B1,On-premise B_inv-2_1ea.yml=I2,O2,B2,Cloud --all=ALL_inv-9_3ea.yml
	t_rc "그룹 2개: 종료코드" "$RC" 0
	t_eq "그룹 2개: invsync 순서(A,B,전체)" "$(grep '^invsync ' "$CALLS" | sed 's/.*-file //' | paste -sd' ')" "A_inv-1_2ea.yml B_inv-2_1ea.yml ALL_inv-9_3ea.yml"
	t_eq "그룹 2개: 전체 yml 은 dhcp/pxe 없음" "$(grep -c '^dhcp ' "$CALLS")|$(grep -c '^pxe ' "$CALLS")" "2|2"
	t_has "그룹 2개: 요약에 전체 invsync" "$S/out.txt" '^성공 \(전체 invsync\) : ALL_inv-9_3ea\.yml$'
	t_has "최종 수량 HPC" "$S/out.txt" '^HPC i1 2026-ECAD_TCAD : 2대$'
	t_has "최종 수량 SDS" "$S/out.txt" '^SDS i2 2026-ECAD_TCAD : 1대$'
	t_has "최종 수량 합계" "$S/out.txt" '^합계 : 3대$'
	: > "$CALLS"
	run02 '1\nY\n' "$TU" A_inv-1_2ea.yml=I1,O1,B1,On-premise --all=ALL_inv-9_2ea.yml
	t_rc "그룹 1개: 종료코드" "$RC" 0
	t_eq "그룹 1개: 전체 invsync 생략(invsync 1회)" "$(grep -c '^invsync ' "$CALLS")" 1
	: > "$CALLS"
	export FAIL_CALL='^invsync .*-file ALL_inv'
	run02 '1\nY\n' "$TU" A_inv-1_2ea.yml=I1,O1,B1,On-premise B_inv-2_1ea.yml=I2,O2,B2,Cloud --all=ALL_inv-9_3ea.yml
	t_rc "전체 invsync 실패: 종료코드 1" "$RC" 1
	t_has "전체 invsync 실패 항목" "$S/out.txt" '^실패 \(전체 invsync\) : ALL_inv-9_3ea\.yml$'
	case_end

	# 7k: dhcp/pxe 별 infra 치환 (02 상단 변수)
	case_begin "7k" "02: dhcp_infra_alias / pxe_infra_alias — dhcp 와 pxe 에 각각 다른 infra 이름 전달"
	setup_case
	sed -i -e 's#^dhcp_infra_alias=""#dhcp_infra_alias="i1:dhcpA"#' -e 's#^pxe_infra_alias=""#pxe_infra_alias="i1:pxeA,I2:pxeB"#' "$W/$F02"
	run02 '1\nY\n' "$TU" A_inv-1_1ea.yml=I1,O1,B1,S1 B_inv-2_1ea.yml=I2,O2,B2,S2 C_inv-3_1ea.yml=I3,O3,B3,S3
	t_rc "02 종료코드" "$RC" 0
	t_eq "dhcp -infra (i1→dhcpA, 나머지 그대로)" "$(grep '^dhcp ' "$CALLS" | sed 's/.*-infra //' | LC_ALL=C sort | paste -sd' ')" "dhcpA i2 i3"
	t_eq "pxe -infra (i1→pxeA, I2→pxeB 대소문자 무시, i3 그대로)" "$(grep '^pxe ' "$CALLS" | sed 's/.*-infra \([^ ]*\) .*/\1/' | LC_ALL=C sort | paste -sd' ')" "i3 pxeA pxeB"
	t_has "치환 안내(dhcp)" "$S/out.txt" '^\[infra 치환\] dhcp -infra i1 -> dhcpA$'
	t_has "치환 안내(pxe)" "$S/out.txt" '^\[infra 치환\] pxe -infra i2 -> pxeB$'
	case_end

	# 7e: 인자 없이 02 단독(대화형)
	case_begin "7e" "02 단독 실행(대화형) 호환: 인자 없음 / user 만 있음"
	setup_case
	: > "$W/a.yml"; : > "$W/b.yml"
	run02 "$TU\nfoo.yml\n"
	t_rc "인자 없음: 종료코드" "$RC" 0
	t_has "인자 없음: 현재 디렉터리 yml 목록 표시" "$S/out.txt" '^a\.yml$'
	t_eq "인자 없음: invsync/dhcp/pxe 호출" "$(grep -E '^(invsync|dhcp|pxe) ' "$CALLS" | paste -sd'|')" "invsync -user $TU -file foo.yml|dhcp -user $TU|pxe -user $TU"
	: > "$CALLS"
	run02 'bar.yml\n' "$TU"
	t_rc "user 만 인자: 종료코드" "$RC" 0
	t_eq "user 만 인자: 호출" "$(grep -E '^(invsync|dhcp|pxe) ' "$CALLS" | paste -sd'|')" "invsync -user $TU -file bar.yml|dhcp -user $TU|pxe -user $TU"
	case_end

	case_begin "7g" "02 인자 형식 오류(= 없음) → [X] + exit 1, awxkit 미호출"
	setup_case
	run02 'Y\n' "$TU" badarg
	t_rc "종료코드 1" "$RC" 1
	t_has "오류 메시지" "$S/out.txt" '^\[X\] 인자 형식 오류'
	t_eq "awxkit 미호출" "$(wc -l < "$CALLS")" 0
	case_end
}

# ===================== 케이스 8: 입력 검증 · 가드 =====================
case8() {
	local pre post v name
	case_begin "8a" "호스트명만 있는 ${TU}.txt (nodeinfo 미사용) → 12필드 오류 종료"
	setup_case
	D6 | awk '{print $4}' > "$W/$TU.txt"
	run01 'N\nY\n'
	t_rc "종료코드≠0" "$RC" nz
	t_has "12필드 오류 메시지" "$OUT" '^\[X\] 12필드 아님: 1: host01$'
	t_eq "ssh/scp 미호출(delhost 포함)" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	t_notmp
	case_end

	case_begin "8b" "nodeinfo 실패 → ${TU}.txt 불변, 종료코드≠0"
	setup_case
	D6 | awk '{print $4}' > "$W/$TU.txt"
	pre=$(cksum < "$W/$TU.txt")
	export NODEINFO_FAIL=1 NODEINFO_DB=$S/none
	run01 'Y\nY\n'
	post=$(cksum < "$W/$TU.txt")
	t_rc "종료코드≠0" "$RC" nz
	t_eq "${TU}.txt 체크섬 불변" "$post" "$pre"
	t_has "실패 안내" "$OUT" '^\[X\] nodeinfo 실행 실패'
	t_eq "ssh/scp 미호출" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	t_notmp
	case_end

	case_begin "8c" "작업진행 N → delhost 미호출 / Y 입력 전엔 미호출, Y 이후에만 delhost"
	setup_case
	seed_raw D6
	run01 'N\nN\n'
	t_rc "N: 종료코드" "$RC" 0
	t_has "N: 작업 취소 로그" "$OUT" '작업 취소'
	t_eq "N: ssh/scp 호출 0" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	# FIFO: 프롬프트에서 대기 → Y 전에는 ssh 0, Y 후 delhost
	setup_case
	seed_raw D6
	fifo_start
	printf 'N\n' >&9
	fifo_wait '총 3대' 15
	sleep 0.5
	t_eq "Y 입력 전 ssh/scp 호출 0" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	printf 'Y\nsu\n1\nY\n' >&9
	fifo_finish 90
	t_eq "프로세스 정상 종료" "$FRC/$FSTUCK" "0/0"
	t_eq "Y 이후 첫 ssh = delhost" "$(grep -m1 '^ssh ' "$CALLS")" "ssh delhost.lab bash /root/server/delhost_$TU"
	t_eq "delhost 호출 1회" "$(grep -c 'delhost_' "$CALLS")" 1
	t_eq "delhost 이후에 scp/custom_inventory (호출 순서)" "$(grep -nE '^(ssh|scp) ' "$CALLS" | sed -n '1p;2p' | sed 's/^[0-9]*://' | awk '{print $1}' | paste -sd' ')" "ssh scp"
	t_notmp
	case_end

	case_begin "8d" "원본(최상단 빈 변수/빈 user) 그대로 실행 → [X] 로 종료, delhost 미호출"
	setup_case raw=1
	seed_raw D6
	run01 'N\nY\n'
	t_rc "원본 그대로: 종료코드≠0" "$RC" nz
	t_has "user 비어 있음 오류" "$OUT" '^\[X\] user 값이 없습니다'
	t_eq "ssh/scp 미호출" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	setup_case repohost= svr_dir= ai_server_list= lacp_comment= inventory_delete_host=
	seed_raw D6
	run01 'N\nY\n'
	t_rc "user 만 채움 + 나머지 빈 변수: 종료코드≠0" "$RC" nz
	t_has "빈 변수 오류" "$OUT" '^\[X\] inventory_delete_host 가 비어 있습니다$'
	t_eq "ssh/scp 미호출(delhost 없음)" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	t_notmp
	case_end

	case_begin "8e" "변수 하나씩만 비움 → 해당 변수 [X] 오류, delhost 미호출"
	for v in inventory_delete_host repohost svr_dir lacp_comment; do
		setup_case "$v="
		seed_raw D6
		run01 'N\nY\n'
		t_rc "$v 빈 값: 종료코드≠0" "$RC" nz
		t_has "$v 오류 메시지" "$OUT" "^\[X\] $v 가 비어 있습니다\$"
		t_eq "$v 빈 값: ssh/scp 미호출" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	done
	t_notmp
	case_end
}

# ===================== 케이스 9: 잔여 파일 =====================
case9() {
	local f left allowed_ok
	case_begin "9a" "정상 완료 후 find -newer marker == §1 결과물, mktemp 잔여 0"
	setup_case
	seed_nodeinfo D6; export NODEINFO_MODE=msg
	touch "$S/marker"; sleep 1
	run01 'Y\nY\nls\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	left=$(cd "$W" && find . -newer "$S/marker" -type f | LC_ALL=C sort)
	allowed_ok=""
	while IFS= read -r f; do
		case $f in
			"./$TU.txt"|./svr_dir/*.yml|"./LOG/$TU.log"|./dhcp_pool/dhcp_pool_delete_info.txt|"./tmp/all_$TU"|"./awxkit/output/${TU}_nodeinfo.yaml"|./.stublog/*) ;;
			*) allowed_ok+="$f " ;;
		esac
	done <<< "$left"
	t_eq "§1 목록 밖 파일 없음" "$allowed_ok" ""
	for f in "./$TU.txt" "./LOG/$TU.log" ./dhcp_pool/dhcp_pool_delete_info.txt "./tmp/all_$TU" "./awxkit/output/${TU}_nodeinfo.yaml"; do
		t_eq "결과물 존재 $f" "$(grep -cxF "$f" <<< "$left")" 1
	done
	t_eq "svr_dir yml 2개(그룹+all)" "$(grep -c '^\./svr_dir/.*\.yml$' <<< "$left")" 2
	t_has "LOG 에 로그 기록" "$W/LOG/$TU.log" '\[14\] 02\.source_dhcp_pxe\.sh'
	t_eq "로컬 분할 입력 파일(${TU}_*.yaml) 잔여 0" "$(find "$W" -name "${TU}_*.yaml" -not -path '*/awxkit/*' | wc -l)" 0
	t_notmp
	case_end

	# FIFO 시그널 테스트
	sig_case() {   # id title 대기정규식 입력 시그널 기대rc
		case_begin "$1" "$2"
		setup_case
		seed_raw D6
		fifo_start
		[[ -n $4 ]] && printf '%b' "$4" >&9
		fifo_wait "$3" 30 || CFAILS+=("프롬프트 대기 지점 도달 실패: /$3/")
		sleep 0.7
		t_eq "신호 전 mktemp 잔여 ≥1 (테스트 유효성)" "$([[ $(find "$S/tmp" -mindepth 1 | wc -l) -ge 1 ]] && echo yes || echo no)" yes
		kill -"$5" "$FPID" 2>/dev/null
		fifo_finish 10
		t_eq "프로세스 종료(멈춤 없음)" "$FSTUCK" 0
		t_eq "종료코드(trap)" "$FRC" "$6"
		t_notmp
		t_has "LOG 파일 존재" "$W/LOG/$TU.log" '시작 user=testuser'
		case_end
	}
	sig_case "9b" "FIFO: awx 사용여부 프롬프트 대기 중 INT → \$TMPDIR 잔여 0" '\[2\] download_txt' '' INT 130
	sig_case "9c" "FIFO: 작업진행 프롬프트 대기 중 INT → \$TMPDIR 잔여 0" '총 3대' 'N\n' INT 130
	sig_case "9d" "FIFO: 메뉴 프롬프트 대기 중 INT → \$TMPDIR 잔여 0" '동일함' 'N\nY\n' INT 130
	sig_case "9e" "FIFO: 작업진행 프롬프트 대기 중 TERM → \$TMPDIR 잔여 0" '총 3대' 'N\n' TERM 143
}

# ===================== EOF 입력 처리 =====================
eof_cases() {
	case_begin "10a" "메뉴에서 stdin EOF → 무한 루프 없이 오류 종료(rc 1)"
	setup_case
	seed_raw D6
	RUN_TIMEOUT=8 run01 'N\nY\n\n'
	t_eq "8초 내 오류 종료(124=타임아웃 → 무한 루프)" "$RC" 1
	t_notmp
	case_end
	case_begin "10b" "02 확인표(Y|N) 에서 stdin EOF → 무한 루프 없이 오류 종료(rc 1)"
	setup_case
	RUN_TIMEOUT=8 run02 '' "$TU" a_inv-1_1ea.yml=I,O,B,S
	t_eq "8초 내 오류 종료(124=타임아웃 → 무한 루프)" "$RC" 1
	case_end
}

# ===================== 색상 =====================
color_cases() {
	local E=$'\033'
	case_begin "11a" "AWX_COLOR=1: LDAP 값 초록·총 대수 굵게·02 요약 초록, LOG 에는 색 코드 없음"
	setup_case
	seed_raw D6; export GOSSH_SCENARIO=same AWX_COLOR=1
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "LDAP 값만 초록" "$OUT" "^모든 호스트의 LDAP이 ${E}\[32mINFO LDAP INFRA1 SITE1${E}\[0m으로 동일함$"
	t_has "총 대수 굵게" "$OUT" "^${E}\[1m총 [0-9]+대${E}\[0m$"
	t_has "02 성공 줄 초록(02 에 색 전달)" "$OUT" "${E}\[32m성공 : "
	t_eq "LOG 에 색 코드 없음" "$(grep -c "$E" "$W/LOG/$TU.log")" 0
	t_has "LOG 에 평문 기록" "$W/LOG/$TU.log" '^모든 호스트의 LDAP이 INFO LDAP INFRA1 SITE1으로 동일함$'
	t_notmp
	case_end

	case_begin "11b" "기본(비터미널): 색 코드 없음, NO_COLOR 면 AWX_COLOR=1 이어도 터미널 판정만 끔"
	setup_case
	seed_raw D6; export GOSSH_SCENARIO=same
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "출력에 색 코드 없음" "$(grep -c "$E" "$OUT")" 0
	case_end
}

# ===================== infra 치환 · 등록 후 대상 확인 =====================
D12() {   # 미등록 infra(adjfg) 2대 + 정상 1대
	IPC=0
	mkln adjfg hostZ1 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln adjfg hostZ2 eth0 sda sda5 1.1T RHEL8 UEFI
	mkln IA hostA1 eth0 sda sda5 1.1T RHEL8 UEFI
}
feature_cases() {
	case_begin "12a" "infra_alias: adjfg → INFRA-X 치환이 ${TU}.txt·yml·pxe 에 반영, 치환 건수 출력"
	setup_case infra_alias='adjfg:INFRA-X zzz:INFRA-Y'
	seed_raw D12
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "${TU}.txt 의 infra 필드" "$(awk '{print $3}' "$W/$TU.txt" | paste -sd' ')" "INFRA-X INFRA-X IA"
	t_has "치환 건수 메시지" "$OUT" '^\[infra 치환\] adjfg -> INFRA-X : 2건$'
	t_eq "pxe -infra (치환 후 소문자)" "$(grep '^pxe ' "$CALLS" | sed 's/.*-infra \([^ ]*\) .*/\1/' | LC_ALL=C sort | paste -sd' ')" "ia infra-x"
	t_no "미등록 이름 adjfg 가 어디에도 전달되지 않음" "$CALLS" 'adjfg'
	case_end

	case_begin "12b" "infra_alias 비어 있으면(기본) 치환 없음"
	setup_case
	seed_raw D12
	run01 'N\nN\n'
	t_eq "${TU}.txt 의 infra 필드 그대로" "$(awk '{print $3}' "$W/$TU.txt" | paste -sd' ')" "adjfg adjfg IA"
	t_no "치환 메시지 없음" "$OUT" 'infra 치환'
	case_end

	local tail_in
	case_begin "13a" "등록 후 확인: 붙여넣은 서버가 모두 존재 → '모두 존재함'"
	setup_case
	seed_raw D6
	run01 'N\nY\nsu\n1\nY\nhost01 host02,host03\n\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "모두 존재함" "$OUT" '붙여넣은 대상 서버 3대가 모두 존재함'
	case_end

	case_begin "13b" "등록 후 확인: 등록 대상에 없는 서버가 있으면 오류(exit 1), 붙여넣지 않은 대상은 경고"
	setup_case
	seed_raw D6
	run01 'N\nY\nsu\n1\nY\nhost01\nhost09|host01\n\n'
	t_rc "01 종료코드 1" "$RC" 1
	t_has "없는 서버 보고" "$OUT" '^\[X\] 등록 대상에 없는 서버 \(1대\) : host09$'
	t_has "붙여넣지 않은 대상 경고" "$OUT" '^\[!\] 붙여넣지 않은 등록 대상 \(2대\) : host02 host03$'
	t_no "'모두 존재함' 출력 없음" "$OUT" '모두 존재함'
	case_end

	case_begin "13c" "등록 후 확인: 바로 빈 줄 또는 입력 종료(EOF)면 건너뜀"
	setup_case
	seed_raw D6
	run01 'N\nY\nsu\n1\nY\n\n'
	t_rc "빈 줄: 종료코드" "$RC" 0
	t_has "빈 줄: 확인 생략" "$OUT" '대상 확인 생략'
	setup_case
	seed_raw D6
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "EOF: 종료코드" "$RC" 0
	t_has "EOF: 확인 생략" "$OUT" '대상 확인 생략'
	t_notmp
	case_end
}

# ===================== day_print · OS 번호 변환 · 파티션 표준 =====================
ext_cases() {
	case_begin "14a" "day_print 에 user 가 있으면 작업 대상을 '날짜 tmp tmp infra hostname ...' 로 출력"
	setup_case day_print="other,${TU}"
	seed_raw D6
	run01 'N\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "날짜 형식 줄 수(3)" "$(grep -cE '^[0-9]{4}-[0-9]{2}-[0-9]{2} tmp tmp [^ ]+ host0[123] ' "$OUT")" 3
	t_eq "날짜는 오늘" "$(grep -E ' tmp tmp [^ ]+ host01 ' "$OUT" | cut -d' ' -f1)" "$(date +%F)"
	t_has "첫 줄 전체(vendor model 대신 tmp tmp)" "$OUT" '^[0-9-]{10} tmp tmp INFRA-A host01 10\.9\.0\.1 aa:bb:cc:dd:ee:01 eth0 sda sda5 1200 RHEL8 UEFI$'
	t_has "총 대수" "$OUT" '^총 3대$'
	case_end

	case_begin "14b" "day_print 에 user 가 없으면 기존 출력(호스트명만)"
	setup_case day_print="other x"
	seed_raw D6
	run01 'N\nN\n'
	t_has "호스트명만 출력" "$OUT" '^host01 host02 host03$|^host01$'
	t_eq "날짜 형식 줄 없음" "$(grep -cE '^[0-9]{4}-[0-9]{2}-[0-9]{2} tmp tmp' "$OUT")" 0
	case_end

	case_begin "7l" "02: 숫자 OS 값(2025 등)은 conf s4_osver_choices 의 순번으로 pxe -os 에 전달"
	setup_case
	mkdir -p "$W/awxkit/conf"
	printf 's4_osver_choices = 2024, 2025, 2026, 2026-OPC_MDP, 2026-ECAD_TCAD   # 주석\n' > "$W/awxkit/conf/${TU}_setting.conf"
	run02 '2\n2\n3\n4\nY\n' "$TU" A_inv-1_1ea.yml=I1,O1,B1,S1 B_inv-2_1ea.yml=I2,O2,B2,S2 C_inv-3_1ea.yml=I3,O3,B3,S3
	t_rc "02 종료코드" "$RC" 0
	t_eq "pxe -os (2025→2, 2026→3, 2026-OPC_MDP 그대로)" "$(grep '^pxe ' "$CALLS" | sed 's/.*-os \([^ ]*\) .*/\1/' | paste -sd' ')" "2 3 2026-OPC_MDP"
	t_has "번호 변환 안내" "$S/out.txt" '^\[os 번호 변환\] pxe -os 2025 -> 2 '
	t_has "확인표에는 원래 OS 값" "$S/out.txt" '^1 \| .*A_inv-1_1ea\.yml.* \| i1 \| 2025 \| B1 \| S1 \| 1$'
	case_end

	local sc
	for sc in std nvme bad sdb; do
		case_begin "15-$sc" "02: 파티션 표준 확인 (LSBLK_SCENARIO=$sc)"
		setup_case
		seed_raw D6
		export LSBLK_SCENARIO=$sc
		run02 '1\nY\n' "$TU" A_inv-1_1ea.yml=I1,O1,B1,S1
		t_rc "02 종료코드(정보 출력만이라 0)" "$RC" 0
		t_has "단계 제목" "$S/out.txt" '파티션 표준 확인'
		t_eq "lsblk 는 gossh 로 1회 호출" "$(grep -c '^gossh-lsblk ' "$CALLS")" 1
		case $sc in
			std|nvme) t_has "모두 표준" "$S/out.txt" '^모든 호스트\(3대\)가 표준 파티션입니다$' ;;
			bad)
				t_has "다른 파티션 있음(2대)" "$S/out.txt" '^표준과 다른 파티션이 있습니다 \(2대 / 전체 3대\)$'
				t_has "host02 LVM" "$S/out.txt" '^host02 : LVM/논리 볼륨 rhel-root; LVM/논리 볼륨 rhel-var; / 가 물리 파티션이 아님$'
				t_has "host03 var/home/tmp" "$S/out.txt" '^host03 : /var 25G\(20G 아님\); 표준 외 /home\(100G\); /tmp 없음$'
				t_no "host01 은 표준" "$S/out.txt" '^host01 :' ;;
			sdb) t_has "OS 디스크 sdb" "$S/out.txt" '^host01 : OS 설치 디스크가 sda/nvme0n1 이 아님\(sdb\)$' ;;
		esac
		unset LSBLK_SCENARIO
		case_end
	done
}

# ===================== 케이스 16/17: os6_mgmt 재조사 =====================
os6_cases() {
	local os6args=('os6_host=os6.lab' 'os6_user=root' 'os6_dir=@S@/os6share' 'os6_gossh=@S@/bin/gossh')

	case_begin "16a" "01: os8 접속불가 host03 → os6 재조사 성공 → 응답없음 없음, 호스트 목록은 host03 만 전달, 공유 dir 잔여 0"
	setup_case ai_server_list='' "${os6args[@]}"
	seed_raw D6; export GOSSH_SCENARIO=noresp OS6_SCENARIO=same
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "os6 ssh 1회" "$(grep -c '^os6-ssh ' "$CALLS")" 1
	t_eq "os6 로 넘긴 호스트 = host03 만" "$(cat "$STUBLOG/gossh_hosts.txt")" "host03"
	t_has "재조사 안내" "$OUT" 'os8_mgmt 접속 불가 1대 → os6_mgmt\(os6\.lab\) 재조사'
	t_has "반영 안내" "$OUT" 'os6_mgmt 재조사 결과 반영 : 1대'
	t_no "응답 없음 줄 없음" "$OUT" '(응답 없음|모두 접속 불가) :'
	t_has "LDAP 전체 동일(os6 값 합침)" "$OUT" '^모든 호스트의 LDAP이 .*동일함$'
	t_eq "공유 dir 임시파일 잔여 0" "$(find "$S/os6share" -mindepth 1 | wc -l)" 0
	t_notmp
	case_end

	case_begin "16b" "01: os6 에서도 접속불가 → 'os8/os6 모두 접속 불가' 목록만 경고, 진행은 계속(rc 0)"
	setup_case "${os6args[@]}"
	seed_raw D6; export GOSSH_SCENARIO=noresp OS6_SCENARIO=noresp
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "모두 접속 불가 : host03" "$OUT" '^os8/os6 모두 접속 불가 : host03$'
	t_no "반영 안내 없음" "$OUT" 'os6_mgmt 재조사 결과 반영'
	t_eq "공유 dir 임시파일 잔여 0" "$(find "$S/os6share" -mindepth 1 | wc -l)" 0
	case_end

	case_begin "16c" "01: os6 변수가 비어 있으면 재조사 생략(기존 '응답 없음' 그대로, os6 호출 0)"
	setup_case
	seed_raw D6; export GOSSH_SCENARIO=noresp
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "응답 없음 : host03" "$OUT" '^응답 없음 : host03$'
	t_eq "os6 ssh 0회" "$(grep -c '^os6-ssh ' "$CALLS")" 0
	case_end

	case_begin "16d" "01: os6 ssh 실패 → 경고 후 os8 결과만으로 진행(rc 0)"
	setup_case "${os6args[@]}"
	seed_raw D6; export GOSSH_SCENARIO=noresp OS6_SSH_FAIL=1
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "재조사 실행 실패 경고" "$OUT" '^\[!\] os6_mgmt 재조사 실행 실패'
	t_has "모두 접속 불가 : host03" "$OUT" '^os8/os6 모두 접속 불가 : host03$'
	t_eq "공유 dir 임시파일 잔여 0" "$(find "$S/os6share" -mindepth 1 | wc -l)" 0
	case_end

	case_begin "16e" "01: os6 결과로 교체된 호스트가 LACP 판정에 반영된다"
	setup_case "${os6args[@]}"
	seed_raw D6; export GOSSH_SCENARIO=noresp OS6_SCENARIO=lacp
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "LACP 호스트 줄 + 코멘트 줄" "$(grep -B1 'LACP-COMMENT-SENTINEL' "$OUT" | paste -sd'/')" "host03/LACP-COMMENT-SENTINEL"
	case_end

	case_begin "17a" "02: 파티션 확인에서 host03 접속불가 → os6 재조사로 표준 판정 (os6 호출 1회)"
	setup_case "${os6args[@]}"
	seed_raw D6
	export os6_host=$cfg_os6_host os6_user=$cfg_os6_user os6_dir=$cfg_os6_dir os6_gossh=$cfg_os6_gossh
	export LSBLK_SCENARIO=down OS6_LSBLK_SCENARIO=std
	run02 '1\nY\n' "$TU" A_inv-1_1ea.yml=I1,O1,B1,S1
	t_rc "02 종료코드" "$RC" 0
	t_has "재조사 안내" "$S/out.txt" '^os8_mgmt 접속 불가 1대 → os6_mgmt\(os6\.lab\) 재조사$'
	t_has "모두 표준(3대)" "$S/out.txt" '^모든 호스트\(3대\)가 표준 파티션입니다$'
	t_eq "os6 lsblk 호출 1회" "$(grep -c '^os6-gossh-lsblk ' "$CALLS")" 1
	t_eq "공유 dir 임시파일 잔여 0" "$(find "$S/os6share" -mindepth 1 | wc -l)" 0
	unset os6_host os6_user os6_dir os6_gossh LSBLK_SCENARIO OS6_LSBLK_SCENARIO
	case_end

	case_begin "17b" "02: os6 에서도 접속불가 → 'os8/os6 모두 접속 불가(확인 불가)' 경고, rc 0"
	setup_case "${os6args[@]}"
	seed_raw D6
	export os6_host=$cfg_os6_host os6_user=$cfg_os6_user os6_dir=$cfg_os6_dir os6_gossh=$cfg_os6_gossh
	export LSBLK_SCENARIO=down OS6_LSBLK_SCENARIO=down
	run02 '1\nY\n' "$TU" A_inv-1_1ea.yml=I1,O1,B1,S1
	t_rc "02 종료코드" "$RC" 0
	t_has "모두 접속 불가 경고" "$S/out.txt" '^\[!\] os8/os6 모두 접속 불가\(확인 불가\) : host03$'
	unset os6_host os6_user os6_dir os6_gossh LSBLK_SCENARIO OS6_LSBLK_SCENARIO
	case_end

	case_begin "17c" "02: os6 변수가 비어 있으면 재조사 생략(기존 'lsblk 응답 없음' 그대로)"
	setup_case
	seed_raw D6
	export LSBLK_SCENARIO=down
	run02 '1\nY\n' "$TU" A_inv-1_1ea.yml=I1,O1,B1,S1
	t_rc "02 종료코드" "$RC" 0
	t_has "lsblk 응답 없음" "$S/out.txt" '^\[!\] lsblk 응답 없음\(확인 불가\) : host03$'
	t_eq "os6 호출 0회" "$(grep -c '^os6-' "$CALLS")" 0
	unset LSBLK_SCENARIO
	case_end
}

# ===================== 케이스 18/19: auto 모드(AWX_AUTO) · 다른 경로 실행 =====================
auto_cases() {
	local E=$'\033'
	case_begin "18a" "AWX_AUTO=1 (nodeinfo n, OS 2025): 질문 5개가 '(auto)' 로 자동 답변되고 입력 없이 끝까지 진행"
	setup_case
	seed_raw D6
	export AWX_AUTO=1 AWX_AUTO_NODEINFO=n AWX_AUTO_OS=2025
	run01 ''
	t_rc "01 종료코드" "$RC" 0
	t_has "nodeinfo 사용여부 자동" "$OUT" '^ awx nodeinfo 사용여부 Y\|N : n \(auto\)$'
	t_has "작업진행여부 자동" "$OUT" '^작업진행여부 \(Y\|N\) : Y \(auto\)$'
	t_has "인벤토리 소스 자동 su" "$OUT" '^AWX 인벤토리 소스 : su exit : 종료 ls : yaml 파일출력 : su \(auto\)$'
	t_has "OS 버전 선택 자동" "$OUT" '^OS 버전 선택 : 2025 \(auto\)$'
	t_has "옵션 확인 자동" "$OUT" '^옵션이 맞습니까 \(Y\|N\) : Y \(auto\)$'
	t_eq "nodeinfo 미호출(n)" "$(grep -c '^nodeinfo ' "$CALLS")" 0
	t_has "pxe -os 2025" "$CALLS" '^pxe .* -os 2025 '
	t_no "pxe 에 다른 OS 없음" "$CALLS" '^pxe .* -os (2024|2026)'
	t_no "경고 없음" "$OUT" 'AWX_AUTO'
	t_notmp
	case_end

	case_begin "18b" "AWX_AUTO=1 (nodeinfo y, OS default): nodeinfo 호출 + default_os(2026-ECAD_TCAD) 적용"
	setup_case
	seed_nodeinfo D6; export NODEINFO_MODE=msg
	export AWX_AUTO=1 AWX_AUTO_NODEINFO=y AWX_AUTO_OS=default
	run01 ''
	t_rc "01 종료코드" "$RC" 0
	t_has "nodeinfo 사용여부 자동 y" "$OUT" '^ awx nodeinfo 사용여부 Y\|N : y \(auto\)$'
	t_has "nodeinfo -hosts 호출" "$CALLS" "^nodeinfo -user $TU -hosts $W/$TU\.txt$"
	t_has "OS 버전 선택 자동(default → 값)" "$OUT" '^OS 버전 선택 : 2026-ECAD_TCAD \(auto\)$'
	t_has "pxe -os 2026-ECAD_TCAD" "$CALLS" '^pxe .* -os 2026-ECAD_TCAD '
	case_end

	case_begin "18c" "AWX_AUTO=1 + AWX_AUTO_OS 가 선택지에 없음: 노란 경고 후 OS 버전 선택·옵션 확인은 직접 입력"
	setup_case
	seed_raw D6
	export AWX_AUTO=1 AWX_AUTO_NODEINFO=n AWX_AUTO_OS=2099 AWX_COLOR=1
	run01 '1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "노란 경고" "$OUT" "^${E}\[33m\[!\] AWX_AUTO_OS 값\(2099\)이 선택지\(2024 2025 2026 2026-OPC_MDP 2026-ECAD_TCAD default\)에 없어 auto 를 끄고 직접 입력합니다${E}\[0m$"
	t_has "01 의 질문은 그대로 auto" "$OUT" '작업진행여부 \(Y\|N\) : .*Y \(auto\)'
	t_no "OS 버전 선택 auto 아님" "$OUT" 'OS 버전 선택.*\(auto\)'
	t_no "옵션 확인 auto 아님" "$OUT" '옵션이 맞습니까.*\(auto\)'
	t_has "직접 입력한 기본값 1 → default_os" "$CALLS" '^pxe .* -os 2026-ECAD_TCAD '
	case_end

	case_begin "18d" "AWX_AUTO=1 + AWX_AUTO_NODEINFO 가 y|n 이 아님: 경고 후 nodeinfo 질문만 직접 입력"
	setup_case
	seed_raw D6
	export AWX_AUTO=1 AWX_AUTO_NODEINFO=x AWX_AUTO_OS=2024
	run01 'N\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "경고" "$OUT" '^\[!\] AWX_AUTO_NODEINFO 가 y\|n 이 아니어서 직접 입력합니다$'
	t_no "nodeinfo 질문 auto 아님" "$OUT" 'nodeinfo 사용여부.*\(auto\)'
	t_has "나머지는 auto (OS 2024)" "$OUT" '^OS 버전 선택 : 2024 \(auto\)$'
	case_end

	case_begin "18e" "AWX_AUTO 없이 AWX_AUTO_OS 만 있으면 기존과 동일(auto 에코 없음, 입력 그대로 사용)"
	setup_case
	seed_raw D6
	export AWX_AUTO_OS=2025
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_no "(auto) 에코 없음" "$OUT" '\(auto\)|\(auto_setup\)'
	t_eq "nodeinfo 미호출(입력 N)" "$(grep -c '^nodeinfo ' "$CALLS")" 0
	t_has "입력 1 → default_os (AWX_AUTO_OS=2025 무시)" "$CALLS" '^pxe .* -os 2026-ECAD_TCAD '
	case_end

	case_begin "18f" "AWX_AUTO 없이 AWX_AUTO_NODEINFO=n: nodeinfo 질문만 자동 답변 '(auto_setup)', 나머지 질문은 직접 입력"
	setup_case
	seed_raw D6
	export AWX_AUTO_NODEINFO=n AWX_AUTO_OS=2025
	run01 'Y\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "nodeinfo 자동 답변 표시" "$OUT" '^ awx nodeinfo 사용여부 Y\|N : n \(auto_setup\)$'
	t_eq "nodeinfo 미호출" "$(grep -c '^nodeinfo ' "$CALLS")" 0
	t_no "다른 질문은 auto 아님" "$OUT" '\(auto\)$'
	t_has "입력 1 → default_os (AWX_AUTO_OS 무시)" "$CALLS" '^pxe .* -os 2026-ECAD_TCAD '
	t_no "경고 없음" "$OUT" 'AWX_AUTO_NODEINFO'
	case_end

	case_begin "18g" "AWX_AUTO 없이 AWX_AUTO_NODEINFO=y: nodeinfo 가 호출되고 나머지 질문은 직접 입력"
	setup_case
	seed_nodeinfo D6; export NODEINFO_MODE=msg
	export AWX_AUTO_NODEINFO=y
	run01 'Y\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "nodeinfo 자동 답변 표시" "$OUT" '^ awx nodeinfo 사용여부 Y\|N : y \(auto_setup\)$'
	t_has "nodeinfo -hosts 호출" "$CALLS" "^nodeinfo -user $TU -hosts $W/$TU\.txt$"
	t_has "02 까지 완료" "$CALLS" '^pxe '
	case_end

	case_begin "18h" "AWX_AUTO 없이 AWX_AUTO_NODEINFO 가 y|n 이 아니면 자동 답변 없이 직접 입력(경고 없음)"
	setup_case
	seed_raw D6
	export AWX_AUTO_NODEINFO=x
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_no "자동 답변 표시 없음" "$OUT" '\(auto\)|\(auto_setup\)'
	t_no "경고 없음" "$OUT" 'AWX_AUTO_NODEINFO'
	t_eq "nodeinfo 미호출(입력 N)" "$(grep -c '^nodeinfo ' "$CALLS")" 0
	case_end

	case_begin "19a" "다른 디렉터리에서 실행(bash <절대경로>/01...): 스크립트 디렉터리 기준으로 동작"
	setup_case
	seed_raw D6
	RUN01_COUNT=$((RUN01_COUNT+1))
	( cd "$S" && printf '%b' 'N\nY\nsu\n1\nY\n' | TMPDIR="$S/tmp" timeout 180 bash "$W/$F01" 2>&1 | cat > "$S/out.txt"; echo "${PIPESTATUS[1]}" > "$S/rc" )
	RC=$(cat "$S/rc")
	t_rc "01 종료코드" "$RC" 0
	t_has "02 호출까지 완료" "$CALLS" '^pxe '
	t_has "LOG 는 스크립트 디렉터리에" "$W/LOG/$TU.log" '시작 user='
	t_eq "실행 디렉터리($S)에 LOG/ 없음" "$([[ -e $S/LOG ]] && echo yes || echo no)" no
	t_eq "실행 디렉터리에 dhcp_pool/ 없음" "$([[ -e $S/dhcp_pool ]] && echo yes || echo no)" no
	t_has "dhcp_pool 은 스크립트 디렉터리에" "$W/dhcp_pool/dhcp_pool_delete_info.txt" 'host01'
	t_notmp
	case_end

	case_begin "19b" "다른 디렉터리 + 상대경로 + 심볼릭 링크로 실행해도 동일"
	setup_case
	seed_raw D6
	ln -s "$W/$F01" "$S/link01.sh"
	RUN01_COUNT=$((RUN01_COUNT+1))
	( cd "$S/tmp" && printf '%b' 'N\nY\nsu\n1\nY\n' | TMPDIR="$S/tmp" timeout 180 bash ../link01.sh 2>&1 | cat > "$S/out.txt"; echo "${PIPESTATUS[1]}" > "$S/rc" )
	RC=$(cat "$S/rc")
	t_rc "01 종료코드" "$RC" 0
	t_has "02 호출까지 완료" "$CALLS" '^pxe '
	t_has "LOG 는 원본 스크립트 디렉터리에" "$W/LOG/$TU.log" '시작 user='
	case_end

	case_begin "19c" "다른 디렉터리 + AWX_AUTO=1 (TUI 에서의 실행 형태): 입력 없이 끝까지"
	setup_case
	seed_raw D6
	export AWX_AUTO=1 AWX_AUTO_NODEINFO=n AWX_AUTO_OS=2026
	RUN01_COUNT=$((RUN01_COUNT+1))
	( cd "$S" && printf '' | TMPDIR="$S/tmp" timeout 180 bash "$W/$F01" 2>&1 | cat > "$S/out.txt"; echo "${PIPESTATUS[1]}" > "$S/rc" )
	RC=$(cat "$S/rc")
	t_rc "01 종료코드" "$RC" 0
	t_has "pxe -os 2026" "$CALLS" '^pxe .* -os 2026 '
	t_has "완료" "$OUT" '완료'
	case_end

	case_begin "20a" "AWX_USER 유효: user 메뉴 생략(스텁 메뉴 미호출), 그 user 로 진행 + 'user : 값 (auto_setup)'"
	setup_case user=0
	seed_raw D6
	export AWX_USER=testuser
	run01 'N\nY\nsu\n1\nY\n\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "user 에코" "$OUT" '^user : testuser \(auto_setup\)$'
	t_no "user 메뉴 미출력" "$OUT" 'STUB_USER_MENU|Input Number'
	t_eq "user 메뉴 호출 0회" "$(grep -c '^usermenu' "$CALLS")" 0
	t_has "시작 user=testuser" "$W/LOG/$TU.log" '시작 user=testuser'
	t_has "02 호출까지 완료" "$CALLS" '^pxe '
	case_end

	case_begin "20b" "AWX_USER 잘못됨(공백/특수문자): 노란 경고 후 기존 user 메뉴로 진행"
	setup_case user=0
	seed_raw D6
	export AWX_USER='bad user;x' AWX_COLOR=1
	run01 '1\nN\nY\nsu\n1\nY\n\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "노란 경고" "$OUT" "^${E}\[33m\[!\] AWX_USER 가 올바르지 않아 메뉴로 진행합니다${E}\[0m$"
	t_has "user 메뉴 출력" "$OUT" 'STUB_USER_MENU'
	t_eq "user 메뉴 호출 1회" "$(grep -c '^usermenu' "$CALLS")" 1
	t_no "auto_setup 에코 없음" "$OUT" 'user : .*\(auto_setup\)'
	t_has "메뉴 user 사용" "$OUT" '시작 user=testuser'
	unset AWX_COLOR
	case_end

	case_begin "20c" "AWX_USER 없음(user=0): 기존과 동일하게 메뉴 사용, 경고·에코 없음"
	setup_case user=0
	seed_raw D6
	run01 '1\nN\nY\nsu\n1\nY\n\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "user 메뉴 출력" "$OUT" 'STUB_USER_MENU'
	t_no "경고·에코 없음" "$OUT" 'AWX_USER|\(auto_setup\)'
	case_end

	case_begin "20d" "AWX_VERIFY_FILE: 목록 전부 등록 대상에 있음 → stdin 없이 '모두 존재함'"
	setup_case
	seed_raw D6
	printf 'host01 host02,host03\n' > "$S/verify.txt"
	export AWX_VERIFY_FILE=$S/verify.txt
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "프롬프트 줄 유지" "$OUT" '등록 후 확인.*작업 대상 서버 목록을 붙여넣으세요'
	t_has "auto_setup 입력 에코" "$OUT" '^붙여넣을 목록: auto_setup 에서 입력한 3대$'
	t_has "모두 존재함" "$OUT" '붙여넣은 대상 서버 3대가 모두 존재함'
	t_no "생략 아님" "$OUT" '대상 확인 생략'
	case_end

	case_begin "20e" "AWX_VERIFY_FILE: 등록 대상에 없는 서버 포함 → exit 1 + [X], 빠진 대상은 경고"
	setup_case
	seed_raw D6
	printf 'host01\nhost09|host01\n' > "$S/verify.txt"
	export AWX_VERIFY_FILE=$S/verify.txt
	run01 'N\nY\nsu\n1\nY\n'
	t_rc "01 종료코드 1" "$RC" 1
	t_has "없는 서버 보고" "$OUT" '^\[X\] 등록 대상에 없는 서버 \(1대\) : host09$'
	t_has "붙여넣지 않은 대상 경고" "$OUT" '^\[!\] 붙여넣지 않은 등록 대상 \(2대\) : host02 host03$'
	t_no "'모두 존재함' 출력 없음" "$OUT" '모두 존재함'
	case_end

	case_begin "20f" "AWX_VERIFY_FILE 가 없는 파일/빈 파일: 기존대로 stdin 에서 읽음"
	setup_case
	seed_raw D6
	export AWX_VERIFY_FILE=$S/nonexistent.txt
	run01 'N\nY\nsu\n1\nY\nhost01 host02 host03\n\n'
	t_rc "없는 파일: 종료코드" "$RC" 0
	t_no "없는 파일: auto_setup 에코 없음" "$OUT" 'auto_setup 에서 입력한'
	t_has "없는 파일: stdin 목록 사용" "$OUT" '붙여넣은 대상 서버 3대가 모두 존재함'
	setup_case
	seed_raw D6
	: > "$S/empty.txt"
	export AWX_VERIFY_FILE=$S/empty.txt
	run01 'N\nY\nsu\n1\nY\nhost01\n\n'
	t_rc "빈 파일: 종료코드" "$RC" 0
	t_no "빈 파일: auto_setup 에코 없음" "$OUT" 'auto_setup 에서 입력한'
	t_has "빈 파일: stdin 목록 사용" "$OUT" '붙여넣은 대상 서버 1대가 모두 존재함'
	case_end

	case_begin "20g" "AWX_AUTO=1 + AWX_USER + AWX_VERIFY_FILE: stdin 전혀 없이(< /dev/null) 끝까지 완료"
	setup_case user=0
	seed_raw D6
	printf 'host01 host02 host03\n' > "$S/verify.txt"
	export AWX_AUTO=1 AWX_AUTO_NODEINFO=n AWX_AUTO_OS=2025 AWX_USER=testuser AWX_VERIFY_FILE=$S/verify.txt
	RUN01_COUNT=$((RUN01_COUNT+1))
	( cd "$W" && TMPDIR="$S/tmp" timeout 180 bash "./$F01" < /dev/null 2>&1 | cat > "$S/out.txt"; echo "${PIPESTATUS[0]}" > "$S/rc" )
	RC=$(cat "$S/rc")
	t_rc "01 종료코드" "$RC" 0
	t_has "user 에코" "$OUT" '^user : testuser \(auto_setup\)$'
	t_has "pxe -os 2025" "$CALLS" '^pxe .* -os 2025 '
	t_has "auto_setup 입력 에코" "$OUT" '^붙여넣을 목록: auto_setup 에서 입력한 3대$'
	t_has "모두 존재함" "$OUT" '붙여넣은 대상 서버 3대가 모두 존재함'
	t_has "완료" "$OUT" '완료'
	t_eq "user 메뉴 호출 0회" "$(grep -c '^usermenu' "$CALLS")" 0
	case_end
	unset AWX_AUTO AWX_AUTO_NODEINFO AWX_AUTO_OS AWX_COLOR AWX_USER AWX_VERIFY_FILE
}

# ===================== 케이스 21~23: DNS 점검 ([4-1] dns_check) =====================
# 스텁 dig/nslookup 이 $STUB_DNSDB("서버 이름 타입 값")를 읽어 응답한다. 값: IP[,IP] | NX | NODATA | SERVFAIL | FAIL(응답 없음). 없는 이름은 NX.
# resolv.conf 는 스크래치의 가짜(AWX_RESOLV_CONF): 서버 10.1.0.1~3, search dns.test.  기존 케이스는 resolv.conf 가 없어 "점검 생략"이 나온다.
FARM_NONE=""; FARM_NSL=""; DNSKEEP=""
mk_farm() {   # 변수명 제외할_명령... : 시스템 명령은 그대로 심볼릭 링크하되 제외한 이름(dig/nslookup)만 없는 PATH 용 디렉터리
	local var=$1 d dirs=() i f; shift
	f=$(TMPDIR=/tmp mktemp -d); SCRATCHES+=("$f")
	IFS=: read -ra dirs <<< "$ORIG_PATH"
	for ((i = ${#dirs[@]} - 1; i >= 0; i--)); do
		d=${dirs[i]}; [[ -d $d ]] || continue
		find "$d" -maxdepth 1 \( -type f -o -type l \) -print0 2>/dev/null | xargs -0 -r ln -sft "$f"
	done
	for d in "$@"; do rm -f "$f/$d"; done
	printf -v "$var" '%s' "$f"
}

dns_setup() {   # [both|dig|nslookup|none] : 스텁 도구와 가짜 resolv.conf 준비. none 이면 dig·nslookup 이 모두 없는 PATH
	local tools=${1:-both}
	STUB_DNSDB=$S/dnsdb; : > "$STUB_DNSDB"; export STUB_DNSDB
	DCALLS=$STUBLOG/dns_calls.log; : > "$DCALLS"
	# 스텁이 공유하는 조회 프로그램: 정확히 일치하는 줄 > 이름 * 줄 > NX
	cat > "$STUBLOG/dnsq.awk" <<'AWK'
$1 == s && $3 == t { if ($2 == n) ex = $4; else if ($2 == "*") wi = $4 }
END { print (ex != "" ? ex : (wi != "" ? wi : "NX")) }
AWK
	printf '# test resolv.conf\n; comment\nnameserver 10.1.0.1\nnameserver 10.1.0.2\nnameserver 10.1.0.3\nsearch dns.test\n' > "$S/resolv.conf"
	export AWX_RESOLV_CONF=$S/resolv.conf
	if [[ $tools == both || $tools == dig ]]; then
		cat > "$S/bin/dig" <<'STUB'
#!/bin/bash
srv=""; type=A; name=""
while [[ $# -gt 0 ]]; do
	case $1 in
		@*) srv=${1#@} ;;
		-t) type=$2; shift ;;
		-p) shift ;;
		-r|+*) ;;
		*) name=$1 ;;
	esac
	shift
done
name=${name%.}; name=${name,,}
echo "dig $srv $name $type" >> "$STUBLOG/dns_calls.log"
val=$(awk -v s="$srv" -v n="$name" -v t="$type" -f "$STUBLOG/dnsq.awk" "$STUB_DNSDB")
hdr() { printf ';; Got answer:\n;; ->>HEADER<<- opcode: QUERY, status: %s, id: 1\n;; flags: qr rd ra; QUERY: 1, ANSWER: %d, AUTHORITY: 0, ADDITIONAL: 1\n\n' "$1" "$2"; }
case $val in
	FAIL) echo ";; connection timed out; no servers could be reached"; exit 9 ;;
	NX) hdr NXDOMAIN 0 ;;
	NODATA) hdr NOERROR 0 ;;
	SERVFAIL) hdr SERVFAIL 0 ;;
	*)
		IFS=, read -ra v <<< "$val"
		hdr NOERROR "${#v[@]}"; echo ";; ANSWER SECTION:"
		for x in "${v[@]}"; do
			if [[ $type == PTR ]]; then printf '%s.\t60\tIN\tPTR\t%s.\n' "$name" "$x"; else printf '%s.\t60\tIN\tA\t%s\n' "$name" "$x"; fi
		done ;;
esac
exit 0
STUB
	fi
	if [[ $tools == both || $tools == nslookup ]]; then
		cat > "$S/bin/nslookup" <<'STUB'
#!/bin/bash
type=A; pos=()
for a in "$@"; do
	case $a in -type=*) type=${a#-type=} ;; -*) ;; *) pos+=("$a") ;; esac
done
name=${pos[0]%.}; name=${name,,}; srv=${pos[1]}
echo "nslookup $srv $name $type" >> "$STUBLOG/dns_calls.log"
val=$(awk -v s="$srv" -v n="$name" -v t="$type" -f "$STUBLOG/dnsq.awk" "$STUB_DNSDB")
hdr() { printf 'Server:\t\t%s\nAddress:\t%s#53\n\n' "$srv" "$srv"; }
case $val in
	FAIL) echo ";; connection timed out; no servers could be reached"; echo; exit 1 ;;
	NX) hdr; echo "** server can't find $name: NXDOMAIN" >&2; echo; exit 1 ;;
	SERVFAIL) hdr; echo "** server can't find $name: SERVFAIL" >&2; echo; exit 1 ;;
	NODATA) hdr; echo "Non-authoritative answer:"; echo; exit 0 ;;
	*)
		hdr; echo "Non-authoritative answer:"
		IFS=, read -ra v <<< "$val"
		if [[ $type == PTR ]]; then
			for x in "${v[@]}"; do printf '%s\tname = %s.\n' "$name" "$x"; done
		else
			printf 'Name:\t%s\n' "$name"
			for x in "${v[@]}"; do echo "Address: $x"; done
		fi
		echo; exit 0 ;;
esac
STUB
	fi
	chmod +x "$S"/bin/*
	if [[ $tools == both || $tools == dig ]]; then export PATH="$S/bin:$ORIG_PATH"
	else
		[[ -n $FARM_NONE ]] || mk_farm FARM_NONE dig nslookup
		export PATH="$S/bin:$FARM_NONE"
	fi
}
dns_rec() {   # 이름 값 [서버번호...] : A 레코드(이름에 '.' 이 없으면 .dns.test 를 붙임). 서버 생략 시 1 2 3
	local n=$1 v=$2 s; shift 2
	[[ $n == *.* || $n == '*' ]] || n=$n.dns.test
	n=${n,,}
	(( $# )) || set -- 1 2 3
	for s in "$@"; do printf '10.1.0.%s %s A %s\n' "$s" "$n" "$v" >> "$STUB_DNSDB"; done
}
dns_raw() {   # 이름 값 : 이름 그대로(접미사 없이) 모든 서버에 A
	local s
	for s in 1 2 3; do printf '10.1.0.%s %s A %s\n' "$s" "$1" "$2" >> "$STUB_DNSDB"; done
}
dns_ptr() {   # IPv4 이름 : 모든 서버에 PTR
	local a b c d s; IFS=. read -r a b c d <<< "$1"
	for s in 1 2 3; do printf '10.1.0.%s %s.%s.%s.%s.in-addr.arpa PTR %s\n' "$s" "$d" "$c" "$b" "$a" "$2" >> "$STUB_DNSDB"; done
}
D6_FIXED() { D6 | sed 's/ 1\.1T / 1200 /'; }   # D6 을 01 의 [2] sed(1.1T → 1200)까지 거친 모양 (기대값)
dns_q() { grep -cE "$1" "$DCALLS"; }

# 불일치·일치 섞인 4대. 공백·탭이 일정하지 않은 줄을 일부러 넣어 "5번째 필드 외 바이트 동일"을 확인한다
DA() {
	printf 'Dell R750 INFRA-A dnsA1 10.2.0.1 aa:bb:cc:dd:ee:01 eth0 sda sda5 1200 RHEL8 UEFI\n'
	printf 'Dell  R750\tINFRA-A dnsA2 10.2.0.2 aa:bb:cc:dd:ee:02 eth0 sda sda5 1200 RHEL8 UEFI\n'
	printf 'Dell R750 INFRA-A  dnsA3  10.2.0.3  aa:bb:cc:dd:ee:03 eth0 sda sda5 1200 RHEL8 UEFI\n'
	printf 'Dell R750 INFRA-A dnsA4 10.2.0.4 aa:bb:cc:dd:ee:04 eth0 sda sda5 1200 RHEL8\tUEFI\n'
}
DA_REC() { dns_rec dnsA1 10.2.0.101; dns_rec dnsA2 10.2.0.2; dns_rec dnsA3 10.2.0.103; dns_rec dnsA4 10.2.0.104; }
DA_ALL() {   # a: 불일치 3대(1·3·4번 줄)의 5번째 필드만 DNS 값으로
	printf 'Dell R750 INFRA-A dnsA1 10.2.0.101 aa:bb:cc:dd:ee:01 eth0 sda sda5 1200 RHEL8 UEFI\n'
	printf 'Dell  R750\tINFRA-A dnsA2 10.2.0.2 aa:bb:cc:dd:ee:02 eth0 sda sda5 1200 RHEL8 UEFI\n'
	printf 'Dell R750 INFRA-A  dnsA3  10.2.0.103  aa:bb:cc:dd:ee:03 eth0 sda sda5 1200 RHEL8 UEFI\n'
	printf 'Dell R750 INFRA-A dnsA4 10.2.0.104 aa:bb:cc:dd:ee:04 eth0 sda sda5 1200 RHEL8\tUEFI\n'
}
DA_S() {     # s + y n y: 1번 줄·4번 줄만 교체
	printf 'Dell R750 INFRA-A dnsA1 10.2.0.101 aa:bb:cc:dd:ee:01 eth0 sda sda5 1200 RHEL8 UEFI\n'
	printf 'Dell  R750\tINFRA-A dnsA2 10.2.0.2 aa:bb:cc:dd:ee:02 eth0 sda sda5 1200 RHEL8 UEFI\n'
	printf 'Dell R750 INFRA-A  dnsA3  10.2.0.3  aa:bb:cc:dd:ee:03 eth0 sda sda5 1200 RHEL8 UEFI\n'
	printf 'Dell R750 INFRA-A dnsA4 10.2.0.104 aa:bb:cc:dd:ee:04 eth0 sda sda5 1200 RHEL8\tUEFI\n'
}

# 전체 유형 13대: 일치 2 · 불일치 5(중복 경고 목록/DNS/PTR 포함) · 판단 불가 3 · 미등록 2 · 조회 실패 1
FL_REG=(10.5.0.1 10.5.0.2 10.5.0.3 10.5.0.4 10.5.0.5 10.5.0.6 10.5.0.7 10.5.0.8 10.5.0.9 10.5.0.1 10.5.0.30 10.5.0.50 10.5.0.99)
FL_ALL=(10.5.0.1 10.5.0.22 10.5.0.3 10.5.0.4 10.5.0.5 10.5.0.6 10.5.0.7 10.5.0.8 10.5.0.9 10.5.0.100 10.5.0.31 10.5.0.51 10.5.0.50)   # 'a' 이후 5번째 필드(불일치 5대만 DNS 값)
FL() {   # 입력 줄: 번호가 곧 fl01..fl13
	local i
	for ((i = 0; i < 13; i++)); do printf 'Dell R750 INFRA-A fl%02d %s aa:bb:cc:dd:ee:%02x eth0 sda sda5 1200 RHEL8 UEFI\n' $((i + 1)) "${FL_REG[i]}" $((i + 1)); done
}
FL_EXPECT_ALL() {
	local i
	for ((i = 0; i < 13; i++)); do printf 'Dell R750 INFRA-A fl%02d %s aa:bb:cc:dd:ee:%02x eth0 sda sda5 1200 RHEL8 UEFI\n' $((i + 1)) "${FL_ALL[i]}" $((i + 1)); done
}
FL_REC() {
	dns_rec fl01 10.5.0.1
	dns_rec fl02 10.5.0.22
	dns_rec fl03 10.5.0.3 1 2; dns_rec fl03 10.5.0.4 3
	dns_rec fl04 10.5.0.4,10.5.0.40
	dns_rec fl05 10.5.0.5 1 3; dns_rec fl05 NX 2
	dns_rec fl06 NX
	dns_rec fl07 NODATA
	dns_rec fl08 SERVFAIL
	dns_rec fl09 10.5.0.9 1 2; dns_rec fl09 FAIL 3
	dns_rec fl10 10.5.0.100
	dns_rec fl11 10.5.0.31; dns_ptr 10.5.0.30 other30.dns.test
	dns_rec fl12 10.5.0.51
	dns_rec fl13 10.5.0.50
}
fl_table() { sed -n '/^hostname /,/^\[DNS\] /p' "$OUT" | sed '$d'; }   # OUT 의 표 부분(머리글 줄 ~ 요약 줄 직전)

dns_cases() {
	local E=$'\033' i n n4 n41 n5
	unset AWX_AUTO AWX_AUTO_NODEINFO AWX_AUTO_OS
	DNSKEEP=$(TMPDIR=/tmp mktemp -d); SCRATCHES+=("$DNSKEEP")   # 케이스 사이에 표를 비교하려고 남겨 두는 곳

	case_begin "21a" "일치 3대: 표·질문 없이 '[DNS] 일치 3대' 한 줄, ${TU}.txt 불변, 단계 로그 [4]→[4-1]→[5] 순서, 서버 3대 모두에 질의"
	setup_case; dns_setup
	seed_raw D6
	dns_rec host01 10.9.0.1; dns_rec host02 10.9.0.2; dns_rec host03 10.9.0.3
	run01 'N\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "일치 요약" "$OUT" '^\[DNS\] 일치 3대 \(미등록 0, 판단 불가 0, 조회 실패 0\)$'
	t_no "표 없음" "$OUT" '^hostname +등록IP'
	t_no "점검 생략 아님" "$OUT" '점검 생략'
	t_eq "${TU}.txt 불변" "$(cat "$W/$TU.txt")" "$(D6_FIXED)"
	n4=$(grep -n '\[4\] fix_mac' "$OUT" | cut -d: -f1); n41=$(grep -n '\[4-1\] dns_check' "$OUT" | cut -d: -f1); n5=$(grep -n '\[5\] show_targets' "$OUT" | cut -d: -f1)
	t_eq "로그 순서 [4] < [4-1] < [5]" "$(( n4 < n41 && n41 < n5 ? 1 : 0 ))" 1
	t_eq "서버 3대 모두 질의(host01.dns.test)" "$(grep -E '^dig [^ ]+ host01\.dns\.test A$' "$DCALLS" | awk '{print $2}' | sort | paste -sd' ')" "10.1.0.1 10.1.0.2 10.1.0.3"
	t_eq "첫 후보는 search 도메인 붙인 이름(짧은 이름 ndots 1 미만)" "$(grep -E '^dig 10\.1\.0\.1 host01' "$DCALLS" | paste -sd';')" "dig 10.1.0.1 host01.dns.test A"
	t_eq "작업진행 N → ssh/scp 미호출" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	t_notmp
	case_end

	case_begin "21b" "불일치 → a: 모두 DNS 로 교체. 5번째 필드만 바뀌고 나머지(공백·탭 포함)는 바이트 동일, LOG 에 [DNS] 줄"
	setup_case; dns_setup
	seed_raw DA; DA_REC
	run01 'N\na\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "표 머리글" "$OUT" '^hostname +등록IP +DNS IP +비고$'
	t_has "표: dnsA1" "$OUT" '^dnsA1 +10\.2\.0\.1 +10\.2\.0\.101$'
	t_has "표: dnsA3" "$OUT" '^dnsA3 +10\.2\.0\.3 +10\.2\.0\.103$'
	t_has "표: dnsA4" "$OUT" '^dnsA4 +10\.2\.0\.4 +10\.2\.0\.104$'
	t_no "표에 일치 호스트 없음" "$OUT" '^dnsA2 '
	t_has "불일치 요약" "$OUT" '^\[DNS\] 불일치 3대 \(일치 1대, 미등록 0, 판단 불가 0, 조회 실패 0\)$'
	t_has "교체 로그 dnsA1" "$OUT" '^\[DNS\] dnsA1 10\.2\.0\.1 -> 10\.2\.0\.101$'
	t_has "교체 로그 dnsA3" "$OUT" '^\[DNS\] dnsA3 10\.2\.0\.3 -> 10\.2\.0\.103$'
	t_has "교체 로그 dnsA4" "$OUT" '^\[DNS\] dnsA4 10\.2\.0\.4 -> 10\.2\.0\.104$'
	t_has "LOG 에 교체 기록" "$W/LOG/$TU.log" '^\[DNS\] dnsA1 10\.2\.0\.1 -> 10\.2\.0\.101$'
	t_has "교체 대수" "$OUT" '^\[DNS\] 3대 교체$'
	DA_ALL > "$S/expect.txt"
	t_eq "${TU}.txt 바이트 비교(5번째 필드만 변경)" "$(cmp "$S/expect.txt" "$W/$TU.txt" && echo same)" same
	t_notmp
	case_end

	case_begin "21c" "불일치 → n: 그대로 진행, 파일 불변, '교체 없음'"
	setup_case; dns_setup
	seed_raw DA; DA_REC
	run01 'N\nn\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "교체 없음" "$OUT" '^\[DNS\] 교체 없음 — 등록값 그대로 진행$'
	t_no "교체 로그 없음" "$OUT" '^\[DNS\] dnsA. '
	DA > "$S/expect.txt"
	t_eq "${TU}.txt 바이트 동일" "$(cmp "$S/expect.txt" "$W/$TU.txt" && echo same)" same
	t_has "작업진행 단계까지 진행([5])" "$OUT" '\[5\] show_targets'
	case_end

	case_begin "21d" "불일치 → s: 호스트마다 y/n (y n y → 1번·4번 줄만 교체)"
	setup_case; dns_setup
	seed_raw DA; DA_REC
	run01 'N\ns\ny\nn\ny\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "교체 로그 dnsA1" "$OUT" '^\[DNS\] dnsA1 10\.2\.0\.1 -> 10\.2\.0\.101$'
	t_no "dnsA3 는 n → 교체 로그 없음" "$OUT" '^\[DNS\] dnsA3 '
	t_has "교체 로그 dnsA4" "$OUT" '^\[DNS\] dnsA4 10\.2\.0\.4 -> 10\.2\.0\.104$'
	t_has "교체 대수" "$OUT" '^\[DNS\] 2대 교체$'
	DA_S > "$S/expect.txt"
	t_eq "${TU}.txt 바이트 비교" "$(cmp "$S/expect.txt" "$W/$TU.txt" && echo same)" same
	case_end

	case_begin "21e" "불일치 → q: 중단(종료코드 0, 작업 취소), 파일 불변, 이후 단계([5]·ssh) 없음"
	setup_case; dns_setup
	seed_raw DA; DA_REC
	run01 'N\nq\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "작업 취소 로그" "$OUT" '작업 취소 \(DNS 점검에서 중단\)'
	t_no "[5] 이후 진행 안 함" "$OUT" '\[5\] show_targets'
	DA > "$S/expect.txt"
	t_eq "${TU}.txt 바이트 동일" "$(cmp "$S/expect.txt" "$W/$TU.txt" && echo same)" same
	t_eq "ssh/scp 미호출" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	t_notmp
	case_end

	case_begin "21f" "메뉴 잘못된 입력은 다시 묻고, 입력이 끝나면 오류 종료(1)"
	setup_case; dns_setup
	seed_raw DA; DA_REC
	run01 'N\nx\nn\nN\n'
	t_rc "잘못된 입력 후 n: 종료코드" "$RC" 0
	t_has "재입력 안내" "$OUT" '^\[!\] a, n, s, q 중 하나를 입력하세요$'
	t_has "n 처리" "$OUT" '^\[DNS\] 교체 없음'
	setup_case; dns_setup
	seed_raw DA; DA_REC
	run01 'N\n'
	t_rc "메뉴에서 입력 끝: 종료코드 1" "$RC" 1
	t_has "입력 끝 오류" "$OUT" '^\[X\] 입력이 끝났습니다$'
	case_end

	case_begin "21g" "전체 유형 13대(dig): 표·비고·중복 경고 3종, 교체는 불일치만(a)"
	setup_case; dns_setup
	seed_raw FL; FL_REC
	run01 'N\na\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "요약" "$OUT" '^\[DNS\] 불일치 5대 \(일치 2대, 미등록 2, 판단 불가 3, 조회 실패 1\)$'
	t_has "불일치: fl02(경고 없음)" "$OUT" '^fl02 +10\.5\.0\.2 +10\.5\.0\.22$'
	t_has "판단 불가: 서버 간 다름(fl03)" "$OUT" '^fl03 +10\.5\.0\.3 +10\.5\.0\.3,10\.5\.0\.4 +⚠ 판단 불가: 서버 간 답이 다름$'
	t_has "판단 불가: A 여러 개(fl04)" "$OUT" '^fl04 +10\.5\.0\.4 +10\.5\.0\.4,10\.5\.0\.40 +⚠ 판단 불가: A 레코드 여러 개$'
	t_has "판단 불가: A 와 NX 혼재(fl05)" "$OUT" '^fl05 +10\.5\.0\.5 +10\.5\.0\.5 +⚠ 판단 불가: 서버 간 답이 다름$'
	t_has "미등록 NXDOMAIN(fl06)" "$OUT" '^fl06 +10\.5\.0\.6 +- +미등록$'
	t_has "미등록 NODATA(fl07)" "$OUT" '^fl07 +10\.5\.0\.7 +- +미등록$'
	t_has "조회 실패(fl08)" "$OUT" '^fl08 +10\.5\.0\.8 +- +⚠ 응답한 서버 없음$'
	t_no "응답한 서버끼리 같으면 일치(fl09: 서버 1대 응답 없음)" "$OUT" '^fl09 '
	t_no "일치(fl01)는 표에 없음" "$OUT" '^fl01 '
	t_has "중복 경고 (목록)+(DNS) fl10" "$OUT" '^fl10 +10\.5\.0\.1 +10\.5\.0\.100 +⚠ 등록IP 10\.5\.0\.1 은 fl01 의 IP \(목록\)  ⚠ 등록IP 10\.5\.0\.1 은 fl01 의 IP \(DNS\)$'
	t_has "중복 경고 (PTR) fl11" "$OUT" '^fl11 +10\.5\.0\.30 +10\.5\.0\.31 +⚠ 등록IP 10\.5\.0\.30 은 other30 의 IP \(PTR\)$'
	t_has "중복 경고 (DNS) fl12" "$OUT" '^fl12 +10\.5\.0\.50 +10\.5\.0\.51 +⚠ 등록IP 10\.5\.0\.50 은 fl13 의 IP \(DNS\)$'
	t_has "불일치 fl13(경고 없음)" "$OUT" '^fl13 +10\.5\.0\.99 +10\.5\.0\.50$'
	t_eq "PTR 은 불일치 등록 IP 5개 × 서버 3 = 15회만" "$(dns_q ' PTR$')" 15
	t_eq "미등록 fl06: 서버1 에 search 이름 → 짧은 이름 순서" "$(grep -E '^dig 10\.1\.0\.1 fl06' "$DCALLS" | paste -sd';')" "dig 10.1.0.1 fl06.dns.test A;dig 10.1.0.1 fl06 A"
	t_eq "교체는 불일치 5대만(판단 불가·미등록·실패 제외)" "$(grep -c '^\[DNS\] fl.. ' "$OUT")" 5
	FL_EXPECT_ALL > "$S/expect.txt"
	t_eq "${TU}.txt 바이트 비교" "$(cmp "$S/expect.txt" "$W/$TU.txt" && echo same)" same
	fl_table > "$DNSKEEP/tbl_dig.txt"
	t_notmp
	case_end

	case_begin "21h" "같은 전체 유형 13대를 nslookup 만 있는 환경에서(폴백): 표·요약이 dig 결과와 동일"
	setup_case; dns_setup nslookup
	seed_raw FL; FL_REC
	run01 'N\nn\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "요약" "$OUT" '^\[DNS\] 불일치 5대 \(일치 2대, 미등록 2, 판단 불가 3, 조회 실패 1\)$'
	fl_table > "$DNSKEEP/tbl_nsl.txt"
	t_eq "표 전체가 dig 실행 결과와 동일" "$(cmp "$DNSKEEP/tbl_dig.txt" "$DNSKEEP/tbl_nsl.txt" && echo same)" same
	t_eq "nslookup 스텁만 호출(dig 호출 0)" "$(grep -c '^dig ' "$DCALLS")" 0
	t_eq "nslookup 호출 있음" "$(( $(grep -c '^nslookup ' "$DCALLS") > 0 ? 1 : 0 ))" 1
	t_eq "PTR 도 같은 횟수" "$(dns_q ' PTR$')" 15
	case_end

	case_begin "21i" "판단 불가만 있고 불일치 없음: 표에 경고, 질문 없이 진행, 파일 불변"
	setup_case; dns_setup
	seed_raw D6
	dns_rec host01 10.9.0.1 1 2; dns_rec host01 10.9.0.9 3
	dns_rec host02 10.9.0.2,10.9.0.22
	dns_rec host03 10.9.0.3
	run01 'N\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "서버 간 다름" "$OUT" '^host01 +10\.9\.0\.1 +10\.9\.0\.1,10\.9\.0\.9 +⚠ 판단 불가: 서버 간 답이 다름$'
	t_has "A 여러 개" "$OUT" '^host02 +10\.9\.0\.2 +10\.9\.0\.2,10\.9\.0\.22 +⚠ 판단 불가: A 레코드 여러 개$'
	t_has "요약(불일치 없음)" "$OUT" '^\[DNS\] 일치 1대 \(미등록 0, 판단 불가 2, 조회 실패 0\)$'
	t_no "질문·교체 없음" "$OUT" '^\[DNS\] (교체|[0-9]+대 교체|응답)'
	t_eq "${TU}.txt 불변" "$(cat "$W/$TU.txt")" "$(D6_FIXED)"
	t_has "작업진행 단계까지 진행([5])" "$OUT" '\[5\] show_targets'
	case_end

	case_begin "21j" "불일치 + 판단 불가 섞임, a: 불일치만 교체"
	setup_case; dns_setup
	seed_raw D6
	dns_rec host01 10.9.0.11
	dns_rec host02 10.9.0.2,10.9.0.22
	dns_rec host03 10.9.0.3
	run01 'N\na\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_eq "교체 1건" "$(grep -c '^\[DNS\] host.. ' "$OUT")" 1
	t_has "host01 교체" "$OUT" '^\[DNS\] host01 10\.9\.0\.1 -> 10\.9\.0\.11$'
	t_eq "host02(판단 불가) 5번째 필드 불변" "$(awk '$4 == "host02" {print $5}' "$W/$TU.txt")" 10.9.0.2
	t_eq "host01 5번째 필드 교체" "$(awk '$4 == "host01" {print $5}' "$W/$TU.txt")" 10.9.0.11
	case_end

	case_begin "21k" "A 레코드를 한 번도 못 받음(전부 시간 초과/전부 NX): '점검 생략' 한 줄 후 기존대로 진행"
	setup_case; dns_setup
	seed_raw D6
	dns_rec '*' FAIL
	run01 'N\nN\n'
	t_rc "전부 시간 초과: 종료코드" "$RC" 0
	t_has "점검 생략 문구" "$OUT" '^DNS 조회 불가 — 점검 생략$'
	t_eq "점검 생략 한 줄만" "$(grep -c '점검 생략' "$OUT")" 1
	t_no "표·요약 없음" "$OUT" '^hostname |^\[DNS\]'
	t_eq "${TU}.txt 불변" "$(cat "$W/$TU.txt")" "$(D6_FIXED)"
	t_has "기존대로 [5] 진행" "$OUT" '\[5\] show_targets'
	setup_case; dns_setup
	seed_raw D6
	run01 'N\nN\n'
	t_rc "전부 NXDOMAIN(A 없음): 종료코드" "$RC" 0
	t_has "점검 생략 문구" "$OUT" '^DNS 조회 불가 — 점검 생략$'
	setup_case
	seed_raw D6
	run01 'N\nN\n'
	t_rc "resolv.conf 없음: 종료코드" "$RC" 0
	t_has "점검 생략 문구" "$OUT" '^DNS 조회 불가 — 점검 생략$'
	t_notmp
	case_end

	case_begin "21l" "dig·nslookup 둘 다 없음: 'DNS 도구 없음 — 점검 생략' 후 진행"
	setup_case; dns_setup none
	seed_raw D6
	run01 'N\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "도구 없음 문구" "$OUT" '^DNS 도구 없음 — 점검 생략$'
	t_no "그 외 DNS 출력 없음" "$OUT" '^\[DNS\]|^hostname '
	t_has "기존대로 [5] 진행" "$OUT" '\[5\] show_targets'
	case_end

	case_begin "21m" "응답 없는 서버는 건너뜀: 한 서버가 계속 시간 초과여도 3번 실패한 뒤로는 그 서버에 묻지 않고(100대 중 일부만 질의), 나머지 2대 답으로 일치 판정"
	setup_case; dns_setup
	DN 100 > "$W/$TU.txt"
	for i in $(seq 1 100); do dns_rec "$(printf 'host%02d' "$i")" "10.2.0.$i" 1 2; done
	dns_rec '*' FAIL 3
	run01 'N\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "100대 모두 일치" "$OUT" '^\[DNS\] 일치 100대 \(미등록 0, 판단 불가 0, 조회 실패 0\)$'
	n=$(dns_q '^dig 10\.1\.0\.3 ')
	t_eq "응답 없는 서버(10.1.0.3)에는 3번 이상 40번 이하만 질의(100대 전부 묻지 않음)" "$(( n >= 3 && n <= 40 ? 1 : 0 ))" 1
	CEV+=("10.1.0.3 질의 ${n}회")
	t_eq "정상 서버(10.1.0.1)에는 100대 모두 질의" "$(dns_q '^dig 10\.1\.0\.1 host')" 100
	case_end

	case_begin "21n" "resolv.conf 해석: 주석·중복·4번째 nameserver 무시, search/ndots(점 2개 이상은 그대로 먼저), 포트 표기"
	setup_case; dns_setup
	printf '# c\n; c\nnameserver 10.1.0.1\nnameserver 10.1.0.1\nnameserver 10.1.0.2:5353\nnameserver 10.1.0.3\nnameserver 10.1.0.4\nsearch a.test b.test\noptions ndots:2 timeout:1\n' > "$S/resolv.conf"
	cat > "$W/$TU.txt" <<'EOF'
Dell R750 INFRA-A svr1 10.7.0.1 aa:bb:cc:dd:ee:01 eth0 sda sda5 1200 RHEL8 UEFI
Dell R750 INFRA-A x.y.z 10.7.0.2 aa:bb:cc:dd:ee:02 eth0 sda sda5 1200 RHEL8 UEFI
Dell R750 INFRA-A abs. 10.7.0.3 aa:bb:cc:dd:ee:03 eth0 sda sda5 1200 RHEL8 UEFI
EOF
	dns_rec svr1.b.test 10.7.0.1; dns_rec x.y.z 10.7.0.2; dns_raw abs 10.7.0.3
	run01 'N\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "3대 일치" "$OUT" '^\[DNS\] 일치 3대 '
	t_eq "서버는 3개(10.1.0.4 제외·중복 제거, 포트 표기 허용)" "$(grep -E '^dig ' "$DCALLS" | awk '{print $2}' | sort -u | paste -sd' ')" "10.1.0.1 10.1.0.2 10.1.0.3"
	t_eq "svr1: ndots 2 > 점 0개 → a.test, b.test 순서(A 나오면 멈춤)" "$(grep -E '^dig 10\.1\.0\.1 svr1' "$DCALLS" | awk '{print $3}' | paste -sd' ')" "svr1.a.test svr1.b.test"
	t_eq "x.y.z: 점 2개 >= ndots → 이름 그대로 먼저" "$(grep -E '^dig 10\.1\.0\.1 x\.y\.z' "$DCALLS" | awk '{print $3}' | paste -sd' ')" "x.y.z"
	t_eq "끝이 '.' 이면 그 이름만" "$(grep -E '^dig 10\.1\.0\.1 abs' "$DCALLS" | awk '{print $3}' | paste -sd' ')" "abs"
	case_end

	case_begin "21o" "search/domain 이 resolv.conf 에 없으면 호스트명의 도메인을 씀 (hostname 스텁 lab01.dom.test)"
	setup_case; dns_setup
	printf 'nameserver 10.1.0.1\n' > "$S/resolv.conf"
	printf '#!/bin/bash\necho lab01.dom.test\n' > "$S/bin/hostname"; chmod +x "$S/bin/hostname"
	seed_raw D6
	dns_rec host01.dom.test 10.9.0.1 1; dns_rec host02.dom.test 10.9.0.2 1; dns_rec host03.dom.test 10.9.0.3 1
	run01 'N\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "3대 일치" "$OUT" '^\[DNS\] 일치 3대 '
	t_eq "질의 이름" "$(grep -E '^dig 10\.1\.0\.1 host01' "$DCALLS" | awk '{print $3}' | paste -sd' ')" "host01.dom.test"
	case_end

	case_begin "21p" "AWX_COLOR=1: 경고 노랑(⚠)·조회 실패 빨강, LOG 에는 색 코드 없음 / 색 없음이면 표에 색 코드 없음"
	setup_case; dns_setup
	seed_raw FL; FL_REC
	export AWX_COLOR=1
	run01 'N\nn\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "판단 불가 노랑" "$OUT" "${E}\[33m⚠ 판단 불가: 서버 간 답이 다름${E}\[0m"
	t_has "조회 실패 빨강" "$OUT" "${E}\[31m⚠ 응답한 서버 없음${E}\[0m"
	t_has "중복 경고 노랑" "$OUT" "${E}\[33m⚠ 등록IP 10\.5\.0\.30 은 other30 의 IP \(PTR\)${E}\[0m"
	t_eq "LOG 에 색 코드 없음" "$(grep -c "$E" "$W/LOG/$TU.log")" 0
	unset AWX_COLOR
	setup_case; dns_setup
	seed_raw FL; FL_REC
	run01 'N\nn\nN\n'
	t_eq "색 없음: 출력에 색 코드 없음" "$(grep -c "$E" "$OUT")" 0
	case_end

	case_begin "21q" "AWX_DNS_CHECKED=1 (AWX_AUTO 없이): 표만 출력하고 묻지 않고 진행, 파일 불변"
	setup_case; dns_setup
	seed_raw DA; DA_REC
	export AWX_DNS_CHECKED=1
	run01 'N\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "표" "$OUT" '^dnsA1 +10\.2\.0\.1 +10\.2\.0\.101$'
	t_has "이미 점검 안내" "$OUT" '^\[DNS\] auto_setup 에서 이미 점검·결정함 \(AWX_DNS_CHECKED=1\) — 등록값 그대로 진행$'
	t_no "메뉴 입력 소비 없음(교체 로그 없음)" "$OUT" '^\[DNS\] dnsA. |교체'
	DA > "$S/expect.txt"
	t_eq "${TU}.txt 바이트 동일" "$(cmp "$S/expect.txt" "$W/$TU.txt" && echo same)" same
	t_has "[5] 진행" "$OUT" '\[5\] show_targets'
	case_end

	# ---- 22: AWX_AUTO=1 (stdin 없음, 질문은 터미널로) ----
	# 터미널 대신 AWX_TTY(테스트 훅)에 파일을 준다. 기본값 /dev/tty 경로는 22g 에서 script 로 실제 pty 를 붙여 확인한다.
	auto_dns_setup() {   # 불일치 1대(host02) + 나머지 일치
		setup_case; dns_setup
		seed_raw D6
		dns_rec host01 10.9.0.1; dns_rec host02 10.9.0.22; dns_rec host03 10.9.0.3
		export AWX_AUTO=1 AWX_AUTO_NODEINFO=n AWX_AUTO_OS=2025
	}
	run01_nostdin() {
		RUN01_COUNT=$((RUN01_COUNT+1))
		( cd "$W" && TMPDIR="$S/tmp" timeout 180 bash "./$F01" < /dev/null 2>&1 | cat > "$S/out.txt"; echo "${PIPESTATUS[0]}" > "$S/rc" )
		RC=$(cat "$S/rc")
	}

	case_begin "22a" "auto + 불일치 + 터미널 없음(AWX_TTY 를 열 수 없음): 표 출력 후 중단(종료코드 1), 이후 단계 없음"
	auto_dns_setup
	export AWX_TTY=$S/no_such_tty
	run01_nostdin
	t_rc "01 종료코드" "$RC" 1
	t_has "표" "$OUT" '^host02 +10\.9\.0\.2 +10\.9\.0\.22$'
	t_has "질문 문구" "$OUT" '등록값 그대로 계속 진행할까요\? \[y/N\] '
	t_has "중단 사유" "$OUT" "^\[X\] DNS 불일치가 있으나 터미널\($S/no_such_tty\)에서 답을 받을 수 없어 중단합니다$"
	t_no "작업진행 단계 없음" "$OUT" '작업진행여부'
	t_eq "ssh/scp 미호출" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	t_eq "${TU}.txt 불변" "$(cat "$W/$TU.txt")" "$(D6_FIXED)"
	t_notmp
	case_end

	case_begin "22b" "auto + 불일치 + 터미널에서 y: 등록값 그대로 끝까지 진행(교체 없음)"
	auto_dns_setup
	printf 'y\n' > "$S/tty_in"; export AWX_TTY=$S/tty_in
	run01_nostdin
	t_rc "01 종료코드" "$RC" 0
	t_has "응답 기록" "$OUT" '\[DNS\] 응답: y$'
	t_has "LOG 에도 응답 기록" "$W/LOG/$TU.log" '\[DNS\] 응답: y$'
	t_has "작업진행여부 auto" "$OUT" '작업진행여부 \(Y\|N\) : .*Y \(auto\)'
	t_has "02 까지 완료" "$CALLS" '^pxe '
	t_eq "${TU}.txt 불변(등록값 그대로)" "$(cat "$W/$TU.txt")" "$(D6_FIXED)"
	t_has "완료" "$OUT" '완료'
	case_end

	case_begin "22c" "auto + 불일치 + 터미널에서 N / 빈 입력 / EOF: 중단(종료코드 1)"
	auto_dns_setup
	printf 'N\n' > "$S/tty_in"; export AWX_TTY=$S/tty_in
	run01_nostdin
	t_rc "N: 종료코드" "$RC" 1
	t_has "N: 중단 문구" "$OUT" '^\[X\] DNS 불일치 — 중단$'
	t_no "N: 작업진행 단계 없음" "$OUT" '작업진행여부'
	t_eq "N: ssh/scp 미호출" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
	auto_dns_setup
	printf '\n' > "$S/tty_in"; export AWX_TTY=$S/tty_in
	run01_nostdin
	t_rc "빈 줄: 종료코드" "$RC" 1
	t_has "빈 줄: 중단 문구" "$OUT" '^\[X\] DNS 불일치 — 중단$'
	auto_dns_setup
	: > "$S/tty_in"; export AWX_TTY=$S/tty_in
	run01_nostdin
	t_rc "EOF(빈 파일): 종료코드" "$RC" 1
	t_has "EOF: 답을 받을 수 없어 중단" "$OUT" '터미널\(.*\)에서 답을 받을 수 없어 중단합니다'
	t_notmp
	case_end

	case_begin "22d" "auto + 불일치 없음: 터미널이 없어도 묻지 않고 진행 (AWX_TTY 를 열 수 없는 경로)"
	setup_case; dns_setup
	seed_raw D6
	dns_rec host01 10.9.0.1; dns_rec host02 10.9.0.2; dns_rec host03 10.9.0.3
	export AWX_AUTO=1 AWX_AUTO_NODEINFO=n AWX_AUTO_OS=2025 AWX_TTY=$S/no_such_tty
	run01_nostdin
	t_rc "01 종료코드" "$RC" 0
	t_has "일치 요약" "$OUT" '^\[DNS\] 일치 3대 '
	t_no "질문 없음" "$OUT" '계속 진행할까요'
	t_has "02 까지 완료" "$CALLS" '^pxe '
	case_end

	case_begin "22e" "auto + AWX_DNS_CHECKED=1 + 터미널 없음: 표만 출력하고 묻지 않고 진행, 등록값 그대로"
	auto_dns_setup
	export AWX_TTY=$S/no_such_tty AWX_DNS_CHECKED=1
	run01_nostdin
	t_rc "01 종료코드" "$RC" 0
	t_has "표" "$OUT" '^host02 +10\.9\.0\.2 +10\.9\.0\.22$'
	t_has "이미 점검 안내" "$OUT" '이미 점검·결정함 \(AWX_DNS_CHECKED=1\)'
	t_no "질문 없음" "$OUT" '계속 진행할까요'
	t_has "02 까지 완료" "$CALLS" '^pxe '
	t_eq "${TU}.txt 불변" "$(cat "$W/$TU.txt")" "$(D6_FIXED)"
	case_end

	case_begin "22f" "auto 아닌 일반 실행에서 AWX_TTY 는 무시(질문은 stdin 메뉴), 메뉴는 그대로 a/n/s/q"
	setup_case; dns_setup
	seed_raw D6
	dns_rec host01 10.9.0.1; dns_rec host02 10.9.0.22; dns_rec host03 10.9.0.3
	export AWX_TTY=$S/no_such_tty
	run01 'N\na\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "교체" "$OUT" '^\[DNS\] host02 10\.9\.0\.2 -> 10\.9\.0\.22$'
	case_end
	unset AWX_TTY AWX_DNS_CHECKED AWX_AUTO AWX_AUTO_NODEINFO AWX_AUTO_OS

	# 22g: 실제 pty 로 기본 경로(/dev/tty) 확인 — script 가 없으면 건너뜀
	if command -v script >/dev/null 2>&1; then
		for ans in y N; do
			case_begin "22g-$ans" "auto + 실제 /dev/tty(script 로 pty 연결) 에서 '$ans': $( [[ $ans == y ]] && echo '진행' || echo '중단' )"
			auto_dns_setup
			RUN01_COUNT=$((RUN01_COUNT+1))
			( cd "$W" && { sleep 1; printf '%s\n' "$ans"; sleep 4; } | NO_COLOR=1 TMPDIR="$S/tmp" timeout 120 script -qefc "bash ./$F01" /dev/null 2>&1 | tr -d '\r' > "$S/out.txt"; echo "${PIPESTATUS[1]}" > "$S/rc" )
			RC=$(cat "$S/rc")
			t_has "질문 문구" "$OUT" '등록값 그대로 계속 진행할까요\? \[y/N\] '
			if [[ $ans == y ]]; then
				t_rc "종료코드" "$RC" 0
				t_has "응답 기록" "$OUT" '\[DNS\] 응답: y$'
				t_has "02 까지 완료" "$CALLS" '^pxe '
			else
				t_rc "종료코드" "$RC" 1
				t_has "중단" "$OUT" '^\[X\] DNS 불일치 — 중단$'
				t_eq "ssh/scp 미호출" "$(grep -cE '^(ssh|scp) ' "$CALLS")" 0
			fi
			case_end
		done
		unset AWX_AUTO AWX_AUTO_NODEINFO AWX_AUTO_OS
	else
		printf '%-10s %-8s %s\n' "SKIP" "22g" "script 명령 없음 — /dev/tty 실제 경로 확인 생략"
		ROWS+=("SKIP${SEP}22g${SEP}script 없음${SEP}")
	fi
}

# ===================== 케이스 23: 실제 dig / nslookup + 가짜 DNS 서버(UDP) =====================
# 스텁이 아니라 시스템의 진짜 dig·nslookup 출력을 01 이 해석하는지 확인한다 (python3 + dig + nslookup 이 있을 때만)
real_dns_cases() {
	local i port p0 pids=() zone
	if ! command -v python3 >/dev/null 2>&1 || ! command -v dig >/dev/null 2>&1 || ! command -v nslookup >/dev/null 2>&1; then
		printf '%-10s %-8s %s\n' "SKIP" "23" "python3/dig/nslookup 없음 — 실제 도구 확인 생략"
		ROWS+=("SKIP${SEP}23${SEP}python3/dig/nslookup 없음${SEP}")
		return 0
	fi
	[[ -n $FARM_NSL ]] || mk_farm FARM_NSL dig
	setup_case
	p0=$(( 40000 + RANDOM % 20000 ))
	zone=$S/fz; mkdir -p "$zone"
	# 서버 3대: 포트 p0..p0+2.  fl 와 같은 내용을 서버별 존 파일로
	{
		echo "fl01.dns.test A 10.5.0.1"; echo "fl02.dns.test A 10.5.0.22"
		echo "fl03.dns.test A 10.5.0.3"; echo "fl04.dns.test A 10.5.0.4,10.5.0.40"
		echo "fl05.dns.test A 10.5.0.5"; echo "fl06.dns.test A NX"; echo "fl07.dns.test A NODATA"; echo "fl08.dns.test A SERVFAIL"
		echo "fl09.dns.test A 10.5.0.9"; echo "fl10.dns.test A 10.5.0.100"; echo "fl11.dns.test A 10.5.0.31"
		echo "fl12.dns.test A 10.5.0.51"; echo "fl13.dns.test A 10.5.0.50"
		echo "30.0.5.10.in-addr.arpa PTR other30.dns.test"
	} > "$zone/z1"
	cp "$zone/z1" "$zone/z2"; cp "$zone/z1" "$zone/z3"
	sed -i 's/^fl03.dns.test A 10.5.0.3$/fl03.dns.test A 10.5.0.4/' "$zone/z3"      # 서버 3 만 다른 답
	sed -i 's/^fl05.dns.test A 10.5.0.5$/fl05.dns.test A NX/' "$zone/z2"             # 서버 2 만 NXDOMAIN
	sed -i 's/^fl09.dns.test A 10.5.0.9$/fl09.dns.test A DROP/' "$zone/z3"           # 서버 3 은 응답 안 함(시간 초과)
	for i in 1 2 3; do
		python3 "$SRC/test/fake_dns.py" $((p0 + i - 1)) "$zone/z$i" "$zone/pid$i" > /dev/null 2>&1 < /dev/null &
		pids+=($!)
	done
	for ((i = 0; i < 30; i++)); do [[ -s $zone/pid1 && -s $zone/pid2 && -s $zone/pid3 ]] && break; sleep 0.2; done
	sleep 0.5
	printf 'nameserver 127.0.0.1:%d\nnameserver 127.0.0.1:%d\nnameserver 127.0.0.1:%d\nsearch dns.test\n' "$p0" $((p0 + 1)) $((p0 + 2)) > "$S/resolv.conf"
	export AWX_RESOLV_CONF=$S/resolv.conf
	STUB_DNSDB=$S/dnsdb; : > "$STUB_DNSDB"; export STUB_DNSDB

	case_begin "23a" "진짜 dig + 가짜 DNS 서버 3대: 13대 판정·표·중복 경고(PTR 포함)가 스텁 결과와 같음"
	seed_raw FL
	run01 'N\nn\nN\n'
	t_rc "01 종료코드" "$RC" 0
	t_has "요약" "$OUT" '^\[DNS\] 불일치 5대 \(일치 2대, 미등록 2, 판단 불가 3, 조회 실패 1\)$'
	fl_table > "$DNSKEEP/tbl_real_dig.txt"
	t_eq "표 전체가 스텁 dig 실행 결과와 동일" "$(cmp "$DNSKEEP/tbl_dig.txt" "$DNSKEEP/tbl_real_dig.txt" && echo same)" same
	t_no "진짜 도구인데 점검 생략 아님" "$OUT" '점검 생략'
	case_end

	case_begin "23b" "진짜 nslookup(dig 없는 PATH) + 같은 가짜 DNS 서버: 표가 진짜 dig 결과와 동일"
	seed_raw FL
	local savepath=$PATH
	export PATH="$S/bin:$FARM_NSL"; run01 'N\nn\nN\n'; export PATH=$savepath
	t_rc "01 종료코드" "$RC" 0
	t_has "요약" "$OUT" '^\[DNS\] 불일치 5대 \(일치 2대, 미등록 2, 판단 불가 3, 조회 실패 1\)$'
	fl_table > "$DNSKEEP/tbl_real_nsl.txt"
	t_eq "표 전체가 진짜 dig 결과와 동일" "$(cmp "$DNSKEEP/tbl_real_dig.txt" "$DNSKEEP/tbl_real_nsl.txt" && echo same)" same
	case_end

	kill "${pids[@]}" 2>/dev/null
}

# ===================== 실행 =====================
echo "===== 01/02 실제 실행 기반 테스트 ($(date '+%F %T')) ====="
echo "SRC=$SRC  bash=${BASH_VERSION}  host=$(hostname)"
echo
step_shellcheck
case1
case2
case34
case5
case6
case7
case8
case9
eof_cases
color_cases
feature_cases
ext_cases
os6_cases
auto_cases
dns_cases
real_dns_cases

echo
echo "===== 케이스별 결과 (근거: 실제 실행 출력) ====="
for r in "${ROWS[@]}"; do
	IFS="$SEP" read -r st id title ev <<< "$r"
	printf '%-10s %-8s %s\n' "$st" "$id" "$title"
	[[ -n $ev ]] && printf '           근거: %s\n' "$ev"
done
echo
echo "PASS=$PASS_N FAIL=$FAIL_N (정보성 프로브/SKIP 제외)"
echo "01 실행 횟수(bash ./$F01): $RUN01_COUNT"
echo "02 직접 실행 횟수(bash ./$F02): $RUN02_COUNT  (01 안에서 호출된 02 는 별도)"
[[ $FAIL_N -eq 0 ]] && exit 0 || exit 1
