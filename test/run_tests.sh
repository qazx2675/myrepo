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
	cfg_raw=0; cfg_user=1
	S=$(TMPDIR=/tmp mktemp -d); SCRATCHES+=("$S")
	W=$S/work
	mkdir -p "$W/awxkit" "$W/.stublog" "$W/svr_dir" "$S/tmp" "$S/bin" "$S/remote/Inventory" "$S/root_user/$TU/myrepo"
	cfg_svr_dir=$W/svr_dir
	for kv in "$@"; do
		k=${kv%%=*}; v=${kv#*=}
		printf -v "cfg_$k" '%s' "$v"
	done
	STUBLOG=$W/.stublog; SVR_DIR=$W/svr_dir; REMOTE_DIR=$S/remote/Inventory
	CALLS=$STUBLOG/calls.log; OUT=$S/out.txt
	: > "$CALLS"
	export STUBLOG SVR_DIR REMOTE_DIR
	unset AWX_COLOR NO_COLOR STUB_SLEEP FAIL_CALL FAIL_ONCE_FILE NODEINFO_FAIL NODEINFO_MODE NODEINFO_DB GOSSH_SCENARIO
	export GOSSH_SCENARIO=same
	export PATH="$S/bin:$ORIG_PATH"

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
			-e "s#/root/user/#$S/root_user/#g" \
			"$W/$F01"
		if [[ $cfg_user == 1 ]]; then
			# 복사본의 user(){...} 본문(현장 코드)을 테스트 user 설정으로 통째로 대체
			sed -i -e '/^user(){/,/^}/c\
user(){\
\tuser=testuser\
}' "$W/$F01"
		fi
		# 무결성: 복사본과 원본의 차이는 최상단 변수/user 본문/git 경로 줄뿐이어야 한다
		local bad
		bad=$(diff "$SRC/$F01" "$W/$F01" | grep '^>' | grep -vE '^> (repohost|svr_dir|ai_server_list|day_print|lacp_comment|inventory_delete_host|infra_alias)=|^> '$'\t''user=testuser$|root_user')
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
if [[ $cmd == *lsblk* ]]; then echo "gossh-lsblk $*" >> "$STUBLOG/calls.log"; else echo "gossh $*" >> "$STUBLOG/calls.log"; fi
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
		noresp) if (( i == n - 1 )); then echo "ERROR $h: connect timeout" >&2; continue; fi ;;
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
	t_notmp
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
