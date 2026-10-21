#!/bin/bash
# AWX nodeinfo V2 - HPC 서버 등록 전처리 및 인벤토리/DHCP/PXE 등록
repohost=""
svr_dir=""                   # custom_inventory.sh 가 yml 을 쓰는 경로(로컬에서 보이는 공유 경로)
ai_server_list=""            # 예: "host01|host02"  (호스트명 정확 일치)
day_print=""                  # 이 변수에 user 가 들어 있으면(공백/쉼표/| 구분) 작업 대상을 "날짜 tmp tmp infra hostname ..." 형식으로 출력
lacp_comment=""
inventory_delete_host=""     # 원문은 함수 안 → 최상단으로 이동
infra_alias=""               # 등록되지 않은 infra 이름 치환 "adjfg:infra1 foo:infra2" (공백/쉼표 구분, 비워 두면 치환 없음)
auto_setup_host=""           # (auto_setup) os8_mgmt 호스트명(os6_mgmt 등 다른 서버에서 채움): 비어 있으면 로컬 queue, 있으면 gossh 로 그 서버 queue 에 전송
auto_setup_gossh_pw=""       # (auto_setup) os8_mgmt 에 gossh 키 인증이 안 될 때 쓰는 SSH 비밀번호(gossh -p). 비우면 -p 없이 호출. 커밋에는 항상 빈 값
os6_host=""                  # (os6 재조사) os6_mgmt 호스트명. os8_mgmt 에서 접속 불가인 호스트를 os6_mgmt 에서 다시 확인. 네 변수 중 하나라도 비면 재조사 생략
os6_user=""                  # (os6 재조사) os6_mgmt ssh 계정 (키 인증, 예: root)
os6_dir=""                   # (os6 재조사) os8/os6 가 같은 절대경로로 공유(auto mount)하는 디렉터리. 호스트 목록·실행 파일을 임시로 둠
os6_gossh=""                 # (os6 재조사) os6_mgmt 의 gossh 절대경로
export os6_host os6_user os6_dir os6_gossh   # 02 의 파티션 확인에서도 사용
svr_idr="$svr_dir"           # git 블록 원문($svr_idr 오타)을 그대로 쓰기 위한 별칭
cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")" || exit 1   # 어느 경로에서 실행해도 awxkit/·LOG/·02 등 상대경로가 이 스크립트 디렉터리 기준이 되도록 이동

# ==== [0] 공통: 시작 경로 · 임시물 정리 · 로그 ====
start_pwd=$(pwd)

tmp_paths=()
add_tmp() { tmp_paths+=("$@"); }
cleanup() {
	local p   # 임시물 + gossh 가 목록 옆에 만든 결과 파일(<목록>_res_off 등 — 접속불가 호스트명이 들어 있음)도 지운다
	[[ ${#tmp_paths[@]} -gt 0 ]] || return 0
	for p in "${tmp_paths[@]}"; do [[ -n $p ]] && rm -rf "$p" "$p"_*; done
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# 임시 파일은 /tmp 바로 아래가 아니라 auto_setup 데이터 디렉터리의 tmp 에 (auto_setup 에서 실행하면 TMPDIR 이 이미 그곳). 못 쓰면 /tmp
if [[ -z $TMPDIR ]]; then
	_t=${AUTO_SETUP_DIR:-/tmp/auto_setup}/tmp
	mkdir -p "$_t" 2>/dev/null; chmod 1777 "$_t" 2>/dev/null
	[[ -d $_t && -w $_t ]] && export TMPDIR=$_t
	unset _t
fi

# 색상: 터미널(또는 AWX_COLOR=1)일 때만 사용, NO_COLOR 가 있으면 끔. 로그 파일에는 색 코드를 넣지 않고, 02 에는 AWX_COLOR 로 전달
if [[ ( -t 1 && -z $NO_COLOR ) || $AWX_COLOR == 1 ]]; then
	AWX_COLOR=1
	ESC=$'\033'
	RED=$ESC'[31m'; GREEN=$ESC'[32m'; YELLOW=$ESC'[33m'; CYAN=$ESC'[36m'; BOLD=$ESC'[1m'; RST=$ESC'[0m'
else
	AWX_COLOR=0; ESC=""
	RED=""; GREEN=""; YELLOW=""; CYAN=""; BOLD=""; RST=""
fi
export AWX_COLOR
err() { echo "${RED}$*${RST}"; }
warn() { echo "${YELLOW}$*${RST}"; }

log() { echo "${CYAN}[$(date '+%F %T')]${RST} $*"; }

# 비어 있으면 안 되는 최상단 변수 검사: require_var 변수명...
require_var() {
	local n
	for n in "$@"; do
		[[ -n ${!n} ]] || { err "[X] $n 가 비어 있습니다"; exit 1; }
	done
}

# 단계 간 공유 변수
yaml=""                      # 생성된 yml 파일명 전체(공백 구분, 전체 파일 포함)
group_yml=""                 # 그룹(분할) yml 파일명만(공백 구분, 전체 파일 제외)
all_yml=""                   # 전체(_all) yml 파일명 (그룹 yml 이 2개 이상일 때 02 가 마지막에 invsync 만 수행)
declare -A yml_opt           # yml 파일명 → "infra os boot splunk"
declare -A yml_boot          # yml 파일명 → boot (ls 표시용)
declare -A yml_hosts         # yml 파일명 → 호스트 목록(쉼표 구분, .job 그룹 줄용)
hostfile=$(mktemp)           # 호스트명 목록(gossh -w / 작업 리스트 출력용)
splitdir=$(mktemp -d)        # 분할/전체 입력 파일 임시 디렉터리
add_tmp "$hostfile" "$splitdir"

# ==== [1] user ====
user(){
user_route=""
if [[ -n $AWX_USER && $AWX_USER =~ ^[A-Za-z0-9._-]+$ ]]; then
	# auto_setup 에서 넘긴 user (메뉴 생략)
	user=$AWX_USER
	echo "user : ${user} (auto_setup)"
	return 0
fi
[[ -n $AWX_USER ]] && warn "[!] AWX_USER 가 올바르지 않아 메뉴로 진행합니다"
bash $user_route/info_mn.sh
read -p "Input Number: " user_choice
user=`bash $user_route/info.sh $user_choice`
}

user
[[ -z $user ]] && { err "[X] user 값이 없습니다 (user 함수 확인)"; exit 1; }

mkdir -p LOG
if [[ $AWX_COLOR == 1 ]]; then
	exec > >(tee >(sed -u "s/${ESC}\[[0-9;]*m//g" >> "LOG/${user}.log")) 2>&1
else
	exec > >(tee -a "LOG/${user}.log") 2>&1
fi
log "시작 user=${user}"

# ==== 함수 정의 ====

# [2] ${user}.txt 준비
download_txt() {
if [[ $AWX_AUTO == 1 && $AWX_AUTO_NODEINFO == [YyNn] ]]; then
		# auto 모드: 환경변수 AWX_AUTO_NODEINFO(y|n) 로 자동 답변
		awx_yn=$AWX_AUTO_NODEINFO
		echo " awx nodeinfo 사용여부 Y|N : ${awx_yn} (auto)"
	elif [[ $AWX_AUTO != 1 && $AWX_AUTO_NODEINFO == [YyNn] ]]; then
		# auto_setup 양식 생성(프로파일 없는 AWX 실행): AWX_AUTO 없이 nodeinfo 질문만 자동 답변, 나머지 질문은 그대로 직접 입력
		awx_yn=$AWX_AUTO_NODEINFO
		echo " awx nodeinfo 사용여부 Y|N : ${awx_yn} (auto_setup)"
	else
		[[ $AWX_AUTO == 1 ]] && warn "[!] AWX_AUTO_NODEINFO 가 y|n 이 아니어서 직접 입력합니다"
		read -r -p " awx nodeinfo 사용여부 Y|N : " awx_yn
	fi
	if [[ $awx_yn == [Yy] ]]; then
		# nodeinfo 실패 시 ${user}.txt(입력 호스트 목록)를 덮어쓰지 않고 종료
		bash awxkit/nodeinfo.sh -user ${user} -hosts "${start_pwd}/${user}.txt" || { err "[X] nodeinfo 실행 실패 (${user}.txt 는 변경하지 않음)"; exit 1; }
		[[ -f awxkit/output/${user}_nodeinfo.yaml ]] || { err "[X] awxkit/output/${user}_nodeinfo.yaml 없음"; exit 1; }
		cat awxkit/output/${user}_nodeinfo.yaml > ${user}.txt
	fi

	[[ -f ${user}.txt ]] || { err "[X] ${user}.txt 없음"; exit 1; }
	sed -i 's/1.1T/1200/g' ${user}.txt
	sed -i 's/7T/7600/g' ${user}.txt
	sed -i 's/test1234/offchip/g' ${user}.txt
}

# [3] msg 파싱 / 12필드 검사
parse_msg() {
	if grep -q 'msg' "${user}.txt"; then
		sed -i -n 's/^.*"msg"[[:space:]]*:[[:space:]]*"\([^"]*\)".*$/\1/p' "${user}.txt"
	fi
	[[ -s ${user}.txt ]] || { err "[X] ${user}.txt 에 유효한 내용이 없습니다"; exit 1; }
	# 모든 줄이 12필드(vendor model infra hostname ip mac nic disk part 용량 os boot)여야 함
	awk 'NF != 12 { bad=1; print "[X] 12필드 아님: " NR ": " $0 } END { exit bad }' "${user}.txt" || {
		err "[X] ${user}.txt 는 12필드 형식(msg 값 또는 vendor model infra hostname ip mac nic disk part 용량 os boot)이어야 합니다"
		exit 1
	}
}

# [3-1] 등록되지 않은 infra 이름 치환 (infra_alias="from:to ..."), 3번째 필드
apply_infra_alias() {
	[[ -n $infra_alias ]] || return 0
	local content
	content=$(awk -v map="$infra_alias" '
	BEGIN { n = split(map, p, /[ ,]+/); for (i = 1; i <= n; i++) { k = index(p[i], ":"); if (k > 1) m[substr(p[i], 1, k-1)] = substr(p[i], k+1) } }
	($3 in m) { cnt[$3]++; $3 = m[$3] }
	{ print }
	END { for (k in cnt) print "[infra 치환] " k " -> " m[k] " : " cnt[k] "건" > "/dev/stderr" }' "${user}.txt") && printf '%s\n' "$content" > "${user}.txt"
}

# [4] MAC 짝수 보정
fix_mac() {
	local content
	content=$(awk '
	($4 ~ /^(spice|pice|dspr|pspr)/) && ($4 !~ /ev/) {
		c = substr($6, length($6), 1); i = index("13579bdfBDF", c)
		if (i) $6 = substr($6, 1, length($6)-1) substr("02468aceACE", i, 1)
	}
	{ print }' "${user}.txt") && printf '%s\n' "$content" > "${user}.txt"
}

# [4-1] DNS 점검: ${user}.txt 의 hostname(4번째)·IP(5번째)를 /etc/resolv.conf 의 DNS 서버(최대 3)에 직접 A 질의해 비교한다.
# 판정 규칙은 auto_setup 의 dnscheck.go 와 같다 (/etc/hosts·getent 는 쓰지 않고, 짧은 이름은 search/ndots 를 glibc 와 같은 순서로 붙여 조회).
#   일치 / 불일치(합의된 A 1개 != 등록 IP, 교체 대상) / 판단 불가(서버끼리 다름·A 와 NX 혼재·A 여러 개: 경고만, 교체 제외) /
#   미등록(응답한 서버가 모두 NXDOMAIN·NODATA) / 조회 실패(응답한 서버 없음). 1대라도 A 를 받아야 점검하고, 아니면 한 줄 안내 후 그대로 진행한다.
# 속도: dig(없으면 nslookup) 질의를 xargs -P 32 로 병렬 실행, 질의당 2초·재시도 1회.
#   응답을 한 번도 못 받고 3번 실패한 서버는 이후 질의를 건너뛴다 (응답 기록 ok.N, 실패 횟수 fail.N 줄 수를 DNS_DIR 에 둠).
# 테스트 훅 환경변수: AWX_RESOLV_CONF(기본 /etc/resolv.conf), AWX_TTY(auto 모드 질문을 읽는 곳, 기본 /dev/tty)
# DNS 서버 주소 해석: "1.2.3.4" | "fe80::1%eth0" | "1.2.3.4:5353" | "[::1]:5353" → "주소,포트"
dns_server_spec() {
	local a=$1 re4='^[0-9]{1,3}(\.[0-9]{1,3}){3}$' re4p='^([0-9]{1,3}(\.[0-9]{1,3}){3}):([0-9]+)$' re6p='^\[([0-9A-Fa-f:.]+(%[A-Za-z0-9._-]+)?)\]:([0-9]+)$' re6='^[0-9A-Fa-f:.]+(%[A-Za-z0-9._-]+)?$'
	if [[ $a =~ $re6p ]]; then echo "${BASH_REMATCH[1]},${BASH_REMATCH[3]}"
	elif [[ $a =~ $re4p ]]; then echo "${BASH_REMATCH[1]},${BASH_REMATCH[3]}"
	elif [[ $a =~ $re4 ]]; then echo "$a,53"
	elif [[ $a == *:* && $a =~ $re6 ]]; then echo "$a,53"
	fi
}

# dns_read_conf <resolv.conf> : DNS_SRVS("주소,포트" 공백 구분, 최대 3) · DNS_SEARCH · DNS_NDOTS 설정. search/domain 이 없으면 호스트명의 도메인(glibc 동작)
dns_read_conf() {
	local f=$1 line s n o spec h dup
	local -a w cur
	DNS_SRVS=""; DNS_SEARCH=""; DNS_NDOTS=1
	if [[ -r $f ]]; then
		while IFS= read -r line || [[ -n $line ]]; do
			line=${line#"${line%%[![:space:]]*}"}
			[[ -z $line || $line == [\#\;]* ]] && continue
			read -ra w <<< "$line"
			case ${w[0]} in
				nameserver)
					[[ -n ${w[1]} ]] || continue
					spec=$(dns_server_spec "${w[1]}")
					[[ -n $spec ]] || continue
					read -ra cur <<< "$DNS_SRVS"
					dup=0
					for s in "${cur[@]}"; do [[ $s == "$spec" ]] && dup=1; done
					(( dup == 0 && ${#cur[@]} < 3 )) && DNS_SRVS="${DNS_SRVS:+$DNS_SRVS }$spec" ;;
				domain) [[ -n ${w[1]} ]] && DNS_SEARCH=${w[1]} ;;
				search) [[ -n ${w[1]} ]] && DNS_SEARCH="${w[*]:1:6}" ;;
				options)
					for o in "${w[@]:1}"; do
						if [[ $o =~ ^ndots:([0-9]+)$ ]]; then
							n=$((10#${BASH_REMATCH[1]})); (( n > 15 )) && n=15; DNS_NDOTS=$n
						fi
					done ;;
			esac
		done < "$f"
	fi
	if [[ -z $DNS_SEARCH ]]; then
		h=$(hostname 2>/dev/null) || h=$HOSTNAME
		[[ $h == ?*.?* ]] && DNS_SEARCH=${h#*.}
	fi
}

# dns_cands <이름> : 질의할 이름 순서를 dns_cand 배열에 담는다 (glibc·nslookup 과 같음 —
# 끝이 '.' 이면 그 이름만, 점 개수 >= ndots 면 이름 그대로 먼저 → search 도메인, 아니면 search 도메인 먼저 → 그대로)
dns_cands() {
	local h=$1 s dots
	local -a ws=()
	dns_cand=()
	if [[ $h == *. ]]; then
		while [[ $h == *. ]]; do h=${h%.}; done
		[[ -n $h ]] && dns_cand=("$h")
		return 0
	fi
	for s in $DNS_SEARCH; do
		while [[ $s == .* ]]; do s=${s#.}; done
		while [[ $s == *. ]]; do s=${s%.}; done
		[[ -n $s ]] && ws+=("$h.$s")
	done
	dots=${h//[^.]/}
	if (( ${#dots} >= DNS_NDOTS )); then dns_cand=("$h" "${ws[@]}"); else dns_cand=("${ws[@]}" "$h"); fi
}

# dns_ask <서버번호> <이름> <A|PTR> : 질의 1회(재시도 포함). NOERROR/NXDOMAIN 이면 0 (Q_VALS: A 의 IPv4 또는 PTR 이름, 공백 구분, 없으면 빈 값),
# 응답이 없거나 SERVFAIL 등이면 1. xargs 가 띄우는 자식 bash 에서도 쓰므로 DNS_* 환경변수만 본다.
dns_ask() {
	local i=$1 name=$2 type=$3 spec addr port out rc line st="" alive=0 ret=1 v
	local -a f srv w
	local re4='^[0-9]+(\.[0-9]+){3}$'
	Q_VALS=""
	if [[ ! -e $DNS_DIR/ok.$i && -f $DNS_DIR/fail.$i ]]; then
		mapfile -t f < "$DNS_DIR/fail.$i"
		(( ${#f[@]} >= 3 )) && return 1
	fi
	read -ra srv <<< "$DNS_SRVS"
	spec=${srv[i]}; addr=${spec%,*}; port=${spec##*,}
	if [[ $DNS_TOOL == dig ]]; then
		out=$(dig -r +noall +comments +answer +time=2 +tries=2 -t "$type" -p "$port" "@$addr" "$name." 2>/dev/null); rc=$?
		if (( rc == 0 )); then
			while IFS= read -r line; do
				if [[ $line == *"status: "* ]]; then st=${line#*status: }; st=${st%%,*}; continue; fi
				[[ -z $line || $line == \;* ]] && continue
				read -ra w <<< "$line"
				[[ ${w[2]} == IN && ${w[3]} == "$type" ]] || continue
				v=${w[4]%.}
				if [[ $type == A ]]; then [[ $v =~ $re4 ]] || continue; fi
				Q_VALS="${Q_VALS:+$Q_VALS }$v"
			done <<< "$out"
			if [[ $st == NOERROR || $st == NXDOMAIN ]]; then alive=1; ret=0
			elif [[ -n $st ]]; then alive=1   # SERVFAIL·REFUSED 등: 서버는 살아 있으나 답으로 쓰지 않음
			fi
		fi
	else
		out=$(nslookup -type="$type" -timeout=2 -retry=1 -port="$port" "$name." "$addr" 2>&1); rc=$?
		case $out in
			*SERVFAIL*|*REFUSED*) alive=1 ;;
			*"timed out"*|*"no servers"*|*"refused"*|*"communications error"*) ;;
			*)
				while IFS= read -r line; do
					if [[ $type == A && $line == Address:* ]]; then
						v=${line#Address:}; v=${v//[[:space:]]/}
						[[ $v =~ $re4 ]] && Q_VALS="${Q_VALS:+$Q_VALS }$v"
					elif [[ $type == PTR && $line == *"name = "* ]]; then
						v=${line##*"name = "}; v=${v%.}
						Q_VALS="${Q_VALS:+$Q_VALS }$v"
					fi
				done <<< "$out"
				if [[ -n $Q_VALS || $out == *NXDOMAIN* || $out == *"No answer"* || $out == *"find "* || $rc -eq 0 ]]; then alive=1; ret=0; fi ;;
		esac
	fi
	if (( alive )); then
		[[ -e $DNS_DIR/ok.$i ]] || : > "$DNS_DIR/ok.$i"
	else
		echo x >> "$DNS_DIR/fail.$i"
	fi
	(( ret )) && Q_VALS=""
	return $ret
}

# dns_lookup_a <서버번호> : 후보 이름(dns_cand)을 한 서버에 차례로 물어 결과를 출력한다 (A:ip,ip = A 있음 / N = A 없음 / F = 응답 없음·오류)
dns_lookup_a() {
	local i=$1 c r=F re='^[A-Za-z0-9_][A-Za-z0-9_.-]*$'
	for c in "${dns_cand[@]}"; do
		[[ $c =~ $re ]] || { r=F; break; }
		if dns_ask "$i" "$c" A; then
			if [[ -n $Q_VALS ]]; then r="A:${Q_VALS// /,}"; break; fi
			r=N
		else
			r=F; break
		fi
	done
	echo "$r"
}

# dns_worker <고유 이름 번호> <소문자 이름> : 서버들에 동시에 A 질의 → $DNS_DIR/res 에 "번호|서버1|서버2|.." 한 줄 (응답 없는 서버 하나가 호스트당 4초를 잡아먹어도 겹치게)
dns_worker() {
	local idx=$1 name=$2 i r res=""
	local -a srv
	read -ra srv <<< "$DNS_SRVS"
	dns_cands "$name"
	for ((i = 0; i < ${#srv[@]}; i++)); do
		dns_lookup_a "$i" > "$DNS_DIR/t.$idx.$i" &
	done
	wait
	for ((i = 0; i < ${#srv[@]}; i++)); do
		read -r r < "$DNS_DIR/t.$idx.$i" || r=F
		rm -f "$DNS_DIR/t.$idx.$i"
		res+="|${r:-F}"
	done
	printf '%s%s\n' "$idx" "$res" >> "$DNS_DIR/res"
}

# dns_batch <번호 이름>... : 한 번의 bash 로 여러 호스트를 처리한다 (호스트마다 bash 를 새로 띄우는 비용을 줄임)
dns_batch() {
	while (( $# >= 2 )); do dns_worker "$1" "$2"; shift 2; done
}

# dns_ptr_worker <IPv4> : 모든 서버에 PTR 질의 → $DNS_DIR/ptr 에 "ip|이름 이름.." 한 줄
dns_ptr_worker() {
	local ip=$1 i names="" o1 o2 o3 o4 re='^[0-9]+(\.[0-9]+){3}$'
	local -a srv
	[[ $ip =~ $re ]] || return 0
	IFS=. read -r o1 o2 o3 o4 <<< "$ip"
	read -ra srv <<< "$DNS_SRVS"
	for ((i = 0; i < ${#srv[@]}; i++)); do
		dns_ask "$i" "$o4.$o3.$o2.$o1.in-addr.arpa" PTR && [[ -n $Q_VALS ]] && names="${names:+$names }$Q_VALS"
	done
	printf '%s|%s\n' "$ip" "$names" >> "$DNS_DIR/ptr"
}

# dns_check: 위 규칙으로 점검해 표를 출력하고, 불일치가 있으면 교체 여부를 묻는다.
#   대화형: a 모두 DNS 로 / n 그대로 / s 하나씩 / q 중단.  AWX_AUTO=1: 터미널(/dev/tty)로 "등록값 그대로 계속 진행할까요? [y/N]" (y 외·터미널 없음은 중단).
#   AWX_DNS_CHECKED=1(auto_setup 양식 생성에서 이미 점검·결정): 표만 출력하고 묻지 않고 진행.  교체는 ${user}.txt 5번째 필드만 바꾼다.
dns_check() {
	local d tool per n_ok n_mm n_un n_am n_na total ans rn h oldip newip tty
	if command -v dig >/dev/null 2>&1; then tool=dig
	elif command -v nslookup >/dev/null 2>&1; then tool=nslookup
	else warn "DNS 도구 없음 — 점검 생략"; return 0
	fi
	dns_read_conf "${AWX_RESOLV_CONF:-/etc/resolv.conf}"
	[[ -n $DNS_SRVS ]] || { warn "DNS 조회 불가 — 점검 생략"; return 0; }
	d=$(mktemp -d) || { warn "[!] DNS 점검 임시 디렉터리를 만들 수 없어 생략합니다"; return 0; }
	add_tmp "$d"
	export DNS_TOOL=$tool DNS_DIR=$d DNS_SRVS DNS_SEARCH DNS_NDOTS
	export -f dns_cands dns_ask dns_lookup_a dns_worker dns_batch dns_ptr_worker
	: > "$d/res"; : > "$d/ptr"; : > "$d/warn.tsv"

	# rows.tsv: "행번호 hostname 등록IP 고유이름번호",  uniq.txt: 고유 이름(소문자)을 "번호\n이름\n" 쌍으로 (xargs -0 입력)
	awk -v d="$d" '{
		k = tolower($4)
		if (!(k in u)) { u[k] = ++n; print n "\n" k > (d "/uniq.txt") }
		printf "%d\t%s\t%s\t%d\n", NR, $4, $5, u[k] > (d "/rows.tsv")
	}' "${user}.txt"
	# 호스트가 적을 땐 1대씩 병렬로(응답 없는 서버를 가려내는 3번 실패가 한꺼번에 쌓이게), 많으면 최대 10대씩 묶어 bash 시작 비용을 줄인다
	per=$(( ($(wc -l < "$d/uniq.txt") / 2 + 31) / 32 )); (( per > 10 )) && per=10; (( per < 1 )) && per=1
	tr '\n' '\0' < "$d/uniq.txt" | xargs -0 -n $((per * 2)) -P 32 bash -c 'dns_batch "$@"' _

	# 판정: status.tsv 한 줄당 "행번호 hostname 등록IP 상태 DNS_IP A목록 비고"  (상태 OK 일치/MM 불일치/AM 판단 불가/UN 미등록/NA 조회 실패)
	awk -F'\t' -v rf="$d/res" -v anyf="$d/anyA" '
	function sortuniq(str,   arr, cnt, x, y, tmp, outs, prev) {
		cnt = split(str, arr, ",")
		for (x = 2; x <= cnt; x++) { tmp = arr[x]; for (y = x - 1; y >= 1 && (arr[y] "") > (tmp ""); y--) arr[y + 1] = arr[y]; arr[y + 1] = tmp }
		outs = ""; prev = ""
		for (x = 1; x <= cnt; x++) if (arr[x] != "" && (arr[x] "") != prev) { outs = outs (outs == "" ? "" : ",") arr[x]; prev = arr[x] "" }
		return outs
	}
	FILENAME == rf { split($0, p, "|"); res[p[1]] = $0; next }
	{
		n = split(res[$4], p, "|")
		nA = 0; nNX = 0; all = ""; nk = 0; split("", keys)
		for (k = 2; k <= n; k++) {
			if (substr(p[k], 1, 2) == "A:") {
				nA++; ips = sortuniq(substr(p[k], 3)); all = all "," ips
				if (!(ips in keys)) { keys[ips] = 1; nk++ }
			} else if (p[k] == "N") nNX++
		}
		st = ""; dnsip = ""; ips = ""; note = ""
		if (nA + nNX == 0) { st = "NA"; note = "응답한 서버 없음" }
		else if (nA == 0) { st = "UN"; note = "미등록" }
		else {
			any = 1; ips = sortuniq(all)
			if (nNX > 0 || nk > 1) { st = "AM"; note = "판단 불가: 서버 간 답이 다름" }
			else if (split(ips, t, ",") != 1) { st = "AM"; note = "판단 불가: A 레코드 여러 개" }
			else { dnsip = ips; st = ((ips "") == ($3 "")) ? "OK" : "MM" }
		}
		printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", $1, $2, $3, st, dnsip, ips, note
	}
	END { if (any) print 1 > anyf }' "$d/res" "$d/rows.tsv" > "$d/status.tsv"

	if [[ ! -s $d/anyA ]]; then
		warn "DNS 조회 불가 — 점검 생략"
		export -nf dns_cands dns_ask dns_lookup_a dns_worker dns_batch dns_ptr_worker
		rm -rf "$d"
		return 0
	fi

	# 불일치 호스트의 등록 IP 만 PTR 조회 (중복 경고용)
	awk -F'\t' '$4 == "MM" && !s[$3]++ { print $3 }' "$d/status.tsv" | tr '\n' '\0' | xargs -0 -r -n 1 -P 32 bash -c 'dns_ptr_worker "$1"' _
	export -nf dns_cands dns_ask dns_lookup_a dns_worker dns_batch dns_ptr_worker

	# 중복 경고: 불일치 호스트의 등록 IP 가 (목록) 다른 호스트의 등록 IP, (DNS) 다른 호스트의 DNS 기준 IP, (PTR) 다른 hostname 의 역조회와 같은지. warn.tsv: "행번호 문구"
	awk -F'\t' -v pf="$d/ptr" -v wf="$d/warn.tsv" '
	function shortn(h,   i) { h = tolower(h); i = index(h, "."); if (i > 0) h = substr(h, 1, i - 1); return h }
	function add(i, other, tag,   key) {
		key = tag "/" shortn(other)
		if (key in seen) return
		seen[key] = 1
		print rn[i] "\t등록IP " ip[i] " 은 " other " 의 IP (" tag ")" > wf
	}
	FILENAME == pf { i = index($0, "|"); pt[substr($0, 1, i - 1)] = substr($0, i + 1); next }
	{ n++; rn[n] = $1; host[n] = $2; ip[n] = $3; st[n] = $4; dip[n] = $5; sh[n] = shortn($2) }
	END {
		for (i = 1; i <= n; i++) {
			if (st[i] != "MM" || ip[i] == "") continue
			split("", seen)
			for (j = 1; j <= n; j++) if (j != i && sh[j] != sh[i] && (ip[j] "") == (ip[i] "")) add(i, host[j], "목록")
			for (j = 1; j <= n; j++) if (j != i && sh[j] != sh[i] && (st[j] == "OK" || st[j] == "MM") && (dip[j] "") == (ip[i] "")) add(i, host[j], "DNS")
			m = split(pt[ip[i]], pn, " ")
			for (k = 1; k <= m; k++) pn[k] = shortn(pn[k])
			for (k = 2; k <= m; k++) { t = pn[k]; for (j = k - 1; j >= 1 && (pn[j] "") > (t ""); j--) pn[j + 1] = pn[j]; pn[j + 1] = t }
			for (k = 1; k <= m; k++) if (pn[k] != sh[i]) add(i, pn[k], "PTR")
		}
	}' "$d/ptr" "$d/status.tsv"

	read -r n_ok n_mm n_un n_am n_na total < <(awk -F'\t' '{ c[$4]++ } END { print c["OK"] + 0, c["MM"] + 0, c["UN"] + 0, c["AM"] + 0, c["NA"] + 0, NR }' "$d/status.tsv")

	# 표: 일치가 아닌 호스트만. 경고(⚠) 노랑, 조회 실패·불일치의 DNS IP 빨강
	if (( total > n_ok )); then
		awk -F'\t' -v wf="$d/warn.tsv" -v cy="$YELLOW" -v cr="$RED" -v rst="$RST" '
		function pad(s, w, dw) { if (dw == "") dw = length(s); while (dw < w) { s = s " "; dw++ } return s }
		function col(c, s) { return (c == "" || s == "") ? s : c s rst }
		FILENAME == wf { wl[$1] = wl[$1] (wl[$1] == "" ? "" : "\t") $2; next }
		$4 == "OK" { next }
		{
			n++; hh[n] = $2; rr[n] = $3; sa[n] = $4
			dv[n] = ($4 == "MM") ? $5 : ($6 != "" ? $6 : "-")
			nt[n] = $7; wv[n] = wl[$1]
			if (length(hh[n]) > w1) w1 = length(hh[n])
			if (length(rr[n]) > w2) w2 = length(rr[n])
			if (length(dv[n]) > w3) w3 = length(dv[n])
		}
		END {
			if (w1 < 8) w1 = 8
			if (w2 < 6) w2 = 6
			if (w3 < 6) w3 = 6
			print pad("hostname", w1) "  " pad("등록IP", w2, 6) "  " pad("DNS IP", w3) "  비고"
			for (i = 1; i <= n; i++) {
				note = ""
				if (nt[i] != "") {
					if (sa[i] == "NA") note = col(cr, "⚠ " nt[i])
					else if (sa[i] == "AM") note = col(cy, "⚠ " nt[i])
					else note = nt[i]
				}
				m = split(wv[i], ws, "\t")
				for (k = 1; k <= m; k++) note = note (note == "" ? "" : "  ") col(cy, "⚠ " ws[k])
				dcell = (note != "") ? pad(dv[i], w3) : dv[i]
				if (sa[i] == "MM") dcell = col(cr, dcell)
				line = pad(hh[i], w1) "  " pad(rr[i], w2) "  " dcell
				if (note != "") line = line "  " note
				print line
			}
		}' "$d/warn.tsv" "$d/status.tsv"
	fi

	if (( n_mm == 0 )); then
		echo "[DNS] 일치 ${n_ok}대 (미등록 ${n_un}, 판단 불가 ${n_am}, 조회 실패 ${n_na})"
		rm -rf "$d"
		return 0
	fi
	warn "[DNS] 불일치 ${n_mm}대 (일치 ${n_ok}대, 미등록 ${n_un}, 판단 불가 ${n_am}, 조회 실패 ${n_na})"
	: > "$d/apply.tsv"
	awk -F'\t' '$4 == "MM" { print $1 "\t" $2 "\t" $3 "\t" $5 }' "$d/status.tsv" > "$d/mm.tsv"

	if [[ $AWX_DNS_CHECKED == 1 ]]; then
		echo "[DNS] auto_setup 에서 이미 점검·결정함 (AWX_DNS_CHECKED=1) — 등록값 그대로 진행"
		rm -rf "$d"
		return 0
	elif [[ $AWX_AUTO == 1 ]]; then
		# auto 모드: stdin 이 없으므로 질문은 터미널로만 받는다. 물을 수 없으면 중단
		tty=${AWX_TTY:-/dev/tty}
		printf '%s' "${YELLOW}등록값 그대로 계속 진행할까요? [y/N] ${RST}"
		ans=""
		if ! { IFS= read -r ans < "$tty"; } 2>/dev/null; then
			echo
			err "[X] DNS 불일치가 있으나 터미널(${tty})에서 답을 받을 수 없어 중단합니다"
			exit 1
		fi
		echo "[DNS] 응답: ${ans:-(없음)}"
		[[ $ans == [Yy] || $ans == [Yy][Ee][Ss] ]] || { err "[X] DNS 불일치 — 중단"; exit 1; }
		rm -rf "$d"
		return 0
	fi

	while true; do
		read -r -p "${YELLOW}a 모두 DNS 로 / n 그대로 / s 하나씩 / q 중단 : ${RST}" ans || { err "[X] 입력이 끝났습니다"; exit 1; }
		case $ans in
			a|A) awk -F'\t' '{ print $1 "\t" $4 }' "$d/mm.tsv" > "$d/apply.tsv"; break ;;
			n|N) break ;;
			s|S)
				exec 3< "$d/mm.tsv"
				while IFS=$'\t' read -r rn h oldip newip <&3; do
					read -r -p "  ${h} ${oldip} -> ${newip} 로 바꿀까요? [y/N] " ans || { err "[X] 입력이 끝났습니다"; exit 1; }
					[[ $ans == [Yy] ]] && printf '%s\t%s\n' "$rn" "$newip" >> "$d/apply.tsv"
				done
				exec 3<&-
				break ;;
			q|Q) log "작업 취소 (DNS 점검에서 중단)"; exit 0 ;;
			*) warn "[!] a, n, s, q 중 하나를 입력하세요" ;;
		esac
	done

	if [[ -s $d/apply.tsv ]]; then
		# 5번째 필드만 바꾼다 (다른 필드·공백·줄 순서는 바이트 그대로). apply.tsv: "행번호 새IP"
		awk -v af="$d/apply.tsv" -v cf="$d/changed" '
		function repl5(line, newip,   s, pre, i) {
			s = line; pre = ""
			for (i = 1; i <= 4; i++) { match(s, /^[ \t]*[^ \t]+/); pre = pre substr(s, 1, RLENGTH); s = substr(s, RLENGTH + 1) }
			match(s, /^[ \t]*/); pre = pre substr(s, 1, RLENGTH); s = substr(s, RLENGTH + 1)
			match(s, /^[^ \t]+/)
			return pre newip substr(s, RLENGTH + 1)
		}
		FILENAME == af { to[$1] = $2; next }
		{
			if (FNR in to) {
				print "[DNS] " $4 " " $5 " -> " to[FNR] > cf
				print repl5($0, to[FNR])
			} else print
		}' "$d/apply.tsv" "${user}.txt" > "$d/new.txt" && cat "$d/new.txt" > "${user}.txt"
		[[ -f $d/changed ]] && cat "$d/changed"
		echo "[DNS] $(grep -c . "$d/changed")대 교체"
	else
		echo "[DNS] 교체 없음 — 등록값 그대로 진행"
	fi
	rm -rf "$d"
}

# [5] 등록 대상 20개씩 세로 다단 출력
show_targets() {
	# day_print 에 user 가 있으면 ${user}.txt 를 "날짜 tmp tmp infra hostname ip mac ..." 형식으로 출력
	if [[ -n $day_print && "|${day_print//[ ,]/|}|" == *"|${user}|"* ]]; then
		awk -v d="$(date +%F)" -v cbold="$BOLD" -v crst="$RST" '
		{ printf "%s tmp tmp", d; for (i = 3; i <= NF; i++) printf " %s", $i; printf "\n" }
		END { print cbold "총 " NR "대" crst }' "${user}.txt"
		return 0
	fi
	awk '{print $4}' "${user}.txt" | awk -v cbold="$BOLD" -v crst="$RST" '
	{ a[NR]=$0; if (length($0)>w) w=length($0) }
	END{
		rows = (NR<20 ? NR : 20); cols = int((NR+19)/20)
		for (r=1; r<=rows; r++) {
			line = ""
			for (c=0; c<cols; c++) { i = c*20 + r; if (i<=NR) line = line sprintf("%-" w "s ", a[i]) }
			sub(/ +$/, "", line); print line
		}
		print cbold "총 " NR "대" crst
	}'
}

# [7] 인벤토리 삭제 (작업진행 Y 이후)
inventory_delete() {
	require_var inventory_delete_host
	ssh $inventory_delete_host "bash /root/server/delhost_${user}" || { err "[X] inventory_delete 실패"; exit 1; }
}

# [8] 분할 파일 + 전체 파일 생성: $splitdir 안에만
split_files() {
	# groups: 번호|infra|nic|disk|용량|os|boot|splunk  (D 단계가 yml_opt/yml_boot 구성에 사용)
	awk -v d="$splitdir" -v u="$user" '
	function splunk(h) { if (h ~ /ev/) return "no"; if (h ~ /^s/) return "Cloud"; return "On-premise" }
	{
		b = ($12 == "레거시") ? "legacy" : $12
		key = $3 "|" $7 "|" $8 "|" $10 "|" $11 "|" b "|" splunk($4)
		if (!(key in n)) { n[key] = ++g; print n[key] "|" key > (d "/groups") }
		print > (d "/" u "_" n[key] ".yaml")
	}' "${user}.txt"
	cp "${user}.txt" "$splitdir/${user}_all.yaml"
}

# [9] dhcp_pool/dhcp_pool_delete_info.txt 기록
dhcp_info() {
	mkdir -p dhcp_pool
	awk '{print "tmp tmp SEC", $4, $5, $6, "eth0 sda sda5 960 7.9"}' "${user}.txt" >> dhcp_pool/dhcp_pool_delete_info.txt
}

# [10] scp → custom_inventory.sh → svr_dir 신규 yml 수집 → yaml/group_yml/yml_opt/yml_boot
gen_inventory() {
	require_var repohost svr_dir
	local -a files=()
	local -A g_infra g_os g_boot g_splunk
	local num infra os boot splunk f before after new lines n

	# groups: 번호|infra|nic|disk|용량|os|boot|splunk → 번호별 옵션 (파일은 번호순으로 이미 기록됨)
	while IFS='|' read -r num infra _ _ _ os boot splunk; do
		[[ -n $num ]] || continue
		g_infra[$num]=$infra; g_os[$num]=$os; g_boot[$num]=$boot; g_splunk[$num]=$splunk
		files+=("${user}_${num}.yaml")
	done < "$splitdir/groups"
	files+=("${user}_all.yaml")

	# 입력 파일 전체를 한 번에 repohost 로 복사
	scp "$splitdir"/*.yaml "$repohost:/root/Inventory/" || { err "[X] scp 실패"; exit 1; }

	for f in "${files[@]}"; do
		before=$(ls "$svr_dir"/*.yml 2>/dev/null | sed 's#.*/##' | LC_ALL=C sort)
		ssh "$repohost" "bash /root/Inventory/custom_inventory.sh /root/Inventory/$f" || { err "[X] custom_inventory.sh 실패: $f"; exit 1; }
		after=$(ls "$svr_dir"/*.yml 2>/dev/null | sed 's#.*/##' | LC_ALL=C sort)
		new=$(LC_ALL=C comm -13 <(printf '%s\n' "$before") <(printf '%s\n' "$after"))
		n=$(printf '%s\n' "$new" | grep -c .)
		if [[ $n -ne 1 ]]; then
			err "[X] $f: $svr_dir 에 새로 생긴 yml 이 ${n}개입니다 (1개여야 함)"
			printf '%s\n' "$new"
			exit 1
		fi

		lines=$(wc -l < "$splitdir/$f")
		if [[ $new =~ _([0-9]+)ea\.yml$ ]] && [[ ${BASH_REMATCH[1]} -ne $lines ]]; then
			warn "[!] 경고: $new 의 대수(${BASH_REMATCH[1]}ea)가 입력 줄 수(${lines})와 다릅니다"
		fi

		yaml="${yaml:+$yaml }$new"
		if [[ $f != "${user}_all.yaml" ]]; then
			num=${f#"${user}_"}; num=${num%.yaml}
			group_yml="${group_yml:+$group_yml }$new"
			yml_opt[$new]="${g_infra[$num]} ${g_os[$num]} ${g_boot[$num]} ${g_splunk[$num]}"
			yml_boot[$new]="${g_boot[$num]}"
			yml_hosts[$new]=$(awk 'NF { print $4 }' "$splitdir/$f" | paste -sd, -)
		else
			all_yml=$new
		fi
		log "yml 생성: $new ($f)"
		sleep 1
	done

	rm -rf "$splitdir"            # 로컬 분할 파일 삭제 (trap 에도 등록됨)
}

# [11] git 업로드 (원문 블록을 그대로 사용하고 마지막에 cd "$now_pwd")
# 아래 함수 본문의 블록은 원문 그대로 보존 (백틱·$svr_idr·미인용 변수 포함)
# shellcheck disable=SC2006,SC2086,SC2116,SC2154,SC2164
git_upload() {
	require_var svr_dir
	now_pwd=`pwd`
	for x in `echo $yaml`
	do
		cd /root/user/${user}/myrepo
		cp $svr_idr/$x /root/user/${user}/myrepo/$x
		bash .git_upload.sh ${user} $x
	done
	cd "$now_pwd"
}

# [12-0] os6_mgmt 재조사 공통 (02 의 파티션 확인과 같은 함수를 쓰므로 수정 시 02 도 함께 수정)
# 네 변수가 모두 채워져 있어야 동작. os8_mgmt 에서 접속 불가인 호스트를 os6_mgmt 의 gossh 로 다시 조사한다
os6_enabled() { [[ -n $os6_host && -n $os6_user && -n $os6_dir && -n $os6_gossh ]]; }
# os6_run <호스트목록파일> <gossh 명령> <stdout 저장파일> <stderr 저장파일> : 공유 디렉터리에 목록·실행 파일을 만들고 ssh 로 실행만 한다
os6_run() {
	local hf=$1 cmd=$2 so=$3 se=$4 rh rr
	[[ -d $os6_dir ]] || { warn "[!] os6_dir 경로가 없습니다: $os6_dir"; return 1; }
	rh=$(mktemp "$os6_dir/.os6_hosts.XXXXXX") || return 1
	rr=$(mktemp "$os6_dir/.os6_run.XXXXXX") || { rm -f "$rh"; return 1; }
	add_tmp "$rh" "$rr"
	cp "$hf" "$rh"
	printf '%q -script -w %q %q\n' "$os6_gossh" "$rh" "$cmd" > "$rr"
	chmod 644 "$rh" "$rr"
	ssh -o BatchMode=yes -o ConnectTimeout=10 "${os6_user}@${os6_host}" "bash $(printf '%q' "$rr")" < /dev/null > "$so" 2> "$se"
}
# os6_failed <호스트목록파일> <stdout파일> <stderr파일> : stderr 에 나온 호스트 중 stdout 에 한 줄도 없는 호스트(목록 안의 호스트만)
os6_failed() {
	LC_ALL=C comm -23 \
		<(sed $'s/\033\\[[0-9;]*m//g' "$3" | sed -n 's/^\([^ :]*\): .*/\1/p' | LC_ALL=C sort -u | LC_ALL=C comm -12 - <(LC_ALL=C sort -u "$1")) \
		<(sed -n 's/^\([^ :]*\): .*/\1/p' "$2" | LC_ALL=C sort -u)
}

# [12] AI 서버 안내 · LDAP/LACP 점검, hostfile 채움
check_servers() {
	require_var lacp_comment
	local out="tmp/all_${user}" lacp_hosts noresp cmd err_f retry rl o6 e6 n6 noresp_label="응답 없음"

	awk '{print $4}' "${user}.txt" > "$hostfile"

	# AI GPU 서버(호스트명 정확 일치) 안내
	if [[ -n $ai_server_list ]] && grep -Eqx "($ai_server_list)" "$hostfile"; then
		echo "${YELLOW}AI GPU서버는 power limit설정이 필요합니다. cat .power_limit_setting.txt를 참고하세요.${RST}"
		if [[ -f .power_limit_setting.txt ]]; then
			cat .power_limit_setting.txt
		else
			warn "[!] .power_limit_setting.txt 없음"
		fi
	fi

	# gossh -script 출력(stdout)은 "호스트명: 줄" 형식. 접속불가/ERROR 는 stderr 라 파일에 없음
	mkdir -p tmp
	cmd="cat /etc/openldap/ldap.conf |grep -v '#' |grep -i uri |awk -F= '{print \$2}' |awk -F',' '{print \$1}';cat /proc/net/bonding/bond0 |grep -i mod"
	err_f=$(mktemp); add_tmp "$err_f"
	gossh -script -w "$hostfile" "$cmd" > "$out" 2> "$err_f"
	cat "$err_f" >&2

	# os8_mgmt 에서 접속 불가(stderr 에 나온 호스트 중 응답 없음)인 호스트는 os6_mgmt 에서 다시 조사하고, 그 호스트의 결과를 os6 값으로 교체
	retry=$(os6_failed "$hostfile" "$out" "$err_f")
	if [[ -n $retry ]] && os6_enabled; then
		log "os8_mgmt 접속 불가 $(printf '%s\n' "$retry" | grep -c .)대 → os6_mgmt(${os6_host}) 재조사"
		rl=$(mktemp); o6=$(mktemp); e6=$(mktemp); add_tmp "$rl" "$o6" "$e6" "$out.new"
		printf '%s\n' "$retry" > "$rl"
		if os6_run "$rl" "$cmd" "$o6" "$e6"; then
			cat "$e6" >&2
			awk 'NR == FNR { h[$1] = 1; next } { i = index($0, ": "); if (i < 2 || !(substr($0, 1, i-1) in h)) print }' "$rl" "$out" > "$out.new" && cat "$o6" >> "$out.new" && mv "$out.new" "$out"
			n6=$(printf '%s\n' "$retry" | LC_ALL=C comm -12 - <(sed -n 's/^\([^ :]*\): .*/\1/p' "$o6" | LC_ALL=C sort -u) | grep -c .)
			(( n6 > 0 )) && echo "${GREEN}os6_mgmt 재조사 결과 반영 : ${n6}대${RST}"
		else
			warn "[!] os6_mgmt 재조사 실행 실패 (ssh/gossh 확인) — os8_mgmt 결과만 사용"
		fi
		noresp_label="os8/os6 모두 접속 불가"
	fi

	# LACP(802.3ad) 호스트 한 줄 나열
	lacp_hosts=$(grep -i 'Bonding Mode' "$out" | grep -i '802\.3ad' | cut -d: -f1 | LC_ALL=C sort -u)
	if [[ -n $lacp_hosts ]]; then
		echo "${YELLOW}$(printf '%s\n' "$lacp_hosts" | paste -sd' ' -)${RST}"
		echo "${YELLOW}${lacp_comment}${RST}"
	fi

	# LDAP: Bonding Mode 줄 제외, 호스트별 값(여러 줄이면 합침)의 고유값 비교 (ldap_check 출력 형식에 비의존)
	LC_ALL=C sort -u "$out" | awk -v cgrn="$GREEN" -v cyel="$YELLOW" -v crst="$RST" '
	tolower($0) ~ /bonding mode/ { next }
	{
		i = index($0, ": "); if (i < 2) next
		h = substr($0, 1, i-1); v = substr($0, i+2); gsub(/\t/, " ", v)
		if (!(h in hv)) ord[++nh] = h
		hv[h] = (h in hv) ? hv[h] " / " v : v
	}
	END {
		for (k = 1; k <= nh; k++) {
			v = hv[ord[k]]
			if (!(v in hs)) vord[++nv] = v
			hs[v] = (v in hs) ? hs[v] " " ord[k] : ord[k]
		}
		if (nv == 1) print "모든 호스트의 LDAP이 " cgrn vord[1] crst "으로 동일함"
		else for (k = 1; k <= nv; k++) print cyel vord[k] crst " : " hs[vord[k]]
	}'

	# 응답 없는 호스트: hostfile 과 결과의 호스트 목록 비교
	noresp=$(LC_ALL=C comm -23 <(LC_ALL=C sort -u "$hostfile" | grep .) \
		<(sed -n 's/^\([^ :]*\): .*/\1/p' "$out" | LC_ALL=C sort -u))
	if [[ -n $noresp ]]; then
		echo "${RED}${noresp_label} : $(printf '%s\n' "$noresp" | paste -sd' ' -)${RST}"
	fi
}

# ==== [2]~[5] 입력 준비 ====
log "[2] download_txt"
download_txt
log "[3] parse_msg"
parse_msg
log "[3-1] apply_infra_alias"
apply_infra_alias
log "[4] fix_mac"
fix_mac
log "[4-1] dns_check"
dns_check
log "[5] show_targets"
show_targets

# ==== [6] 작업진행여부 ====
if [[ $AWX_AUTO == 1 ]]; then
	go=Y
	echo "${YELLOW}작업진행여부 (Y|N) : ${RST}${go} (auto)"
else
	read -r -p "${YELLOW}작업진행여부 (Y|N) : ${RST}" go
fi
[[ $go == [Yy] ]] || { log "작업 취소"; exit 0; }
echo "${BOLD}작업진행..${RST}"
# 원격 삭제(inventory_delete) 이후에 빈 변수 오류가 나지 않도록 미리 검사
require_var inventory_delete_host repohost svr_dir lacp_comment
[[ -d $svr_dir ]] || { err "[X] svr_dir 경로가 없습니다: $svr_dir"; exit 1; }

# ==== [7] inventory_delete ====
log "[7] inventory_delete"
inventory_delete

# ==== [8]~[11] 분할 · dhcp 기록 · 인벤토리 생성 · git ====
log "[8] split_files"
split_files
log "[9] dhcp_info"
dhcp_info
log "[10] gen_inventory"
gen_inventory
log "[11] git_upload"
git_upload

# ==== [12] AI/LDAP/LACP 점검 ====
log "[12] check_servers"
check_servers

# ==== [13] 메뉴 ====
require_var svr_dir
while true; do
	menu_prompt="AWX 인벤토리 소스 : ${GREEN}su${RST} ${RED}exit${RST} : 종료 ${CYAN}ls${RST} : yaml 파일출력 : "
	if [[ $AWX_AUTO == 1 ]]; then
		sel=su
		echo "${menu_prompt}${sel} (auto)"
	else
		read -r -p "$menu_prompt" sel || { err "[X] 입력이 끝났습니다"; exit 1; }
	fi
	case $sel in
		su)   break ;;
		exit) exit 0 ;;
		ls)   for i in $yaml; do echo "${CYAN}${i}${RST} [${yml_boot[$i]:-all}]"; done ;;
		"")   ;;
		*)    if [[ -f $svr_dir/$sel ]]; then cat "$svr_dir/$sel"; else warn "[!] $sel 없음"; fi ;;
	esac
done

# ==== [14] 02 호출 ====
for ((;;)); do
	echo "${BOLD}작업 리스트${RST}"
	paste -sd'|' "$hostfile"
	args=(); for f in $group_yml; do args+=("$f=${yml_opt[$f]// /,}"); done   # yml=infra,os,boot,splunk
	[[ -n $all_yml ]] && args+=("--all=$all_yml")   # 그룹 yml 이 여러 개면 02 가 마지막에 전체 yml 로 invsync 만 수행
	log "[14] 02.source_dhcp_pxe.sh ${user}"
	bash 02.source_dhcp_pxe.sh ${user} "${args[@]}" && break
	read -r -p "${YELLOW}02 실패 — 재시도 (Y|N) : ${RST}" r
	[[ $r == [Yy] ]] || exit 1
done
# [14-1] auto_setup 전달: 02 성공 직후 호스트 목록(4번째 필드)을 auto_setup queue 에 남김 (실패해도 진행에 영향 없음)
# .job 그룹 줄(호스트 줄 뒤): yml=<파일명> infra= os= boot= splunk= hosts=<h1,h2,..> 그룹별 한 줄 + all=<전체 yml>. 그룹이 2개 미만이면 생략
as_group_lines() {
	local f i o b s
	(( $(wc -w <<< "$group_yml") >= 2 )) || return 0
	for f in $group_yml; do
		read -r i o b s <<< "${yml_opt[$f]}"
		echo "yml=$f infra=$i os=$o boot=$b splunk=$s hosts=${yml_hosts[$f]}"
	done
	[[ -n $all_yml ]] && echo "all=$all_yml"
	return 0
}
if [[ -n $auto_setup_host ]]; then
	# 원격 전달: 로컬 queue 는 만들지 않고 gossh 로 ${AUTO_SETUP_DIR:-/tmp/auto_setup}/queue/ 에 원자 기록(원격에서 tmp 로 쓰고 mv)
	as_job=$(mktemp); as_hl=$(mktemp); add_tmp "$as_job" "$as_hl"
	as_name="$(date +%s)_${user}_$$.job"
	{ echo "user=${user}"; echo "time=$(date +%s)"; cat "$hostfile"; as_group_lines; } > "$as_job"
	echo "$auto_setup_host" > "$as_hl"
	as_b64=$(base64 -w0 < "$as_job" 2>/dev/null | sed 's/../&./g')   # 두 글자마다 '.': base64 가 gossh 위험어(ddc/halt 등)를 우연히 만들지 않게
	as_out=""
	if command -v gossh >/dev/null 2>&1 && [[ -n $as_b64 ]]; then
		as_out=$(gossh ${auto_setup_gossh_pw:+-p "$auto_setup_gossh_pw"} -script -w "$as_hl" "bash -c 'd=\"\${AUTO_SETUP_DIR:-/tmp/auto_setup}/queue\"; mkdir -p \"\$d\" && echo ${as_b64} | tr -d . | base64 -d > \"\$d/.${as_name}.tmp\" && mv -f \"\$d/.${as_name}.tmp\" \"\$d/${as_name}\" && echo AUTO_SETUP_OK'" 2>/dev/null)
	fi
	if [[ $as_out == *AUTO_SETUP_OK* ]]; then
		log "auto_setup 전달 : $(grep -c . "$hostfile")대 → ${auto_setup_host}"
	else
		warn "[!] auto_setup 전달 실패 (${auto_setup_host})"
	fi
else
auto_setup_queue="${AUTO_SETUP_DIR:-/tmp/auto_setup}/queue"
if mkdir -p "$auto_setup_queue" 2>/dev/null; then
	q="$auto_setup_queue/$(date +%s)_${user}_$$.job"
	{ echo "user=${user}"; echo "time=$(date +%s)"; cat "$hostfile"; as_group_lines; } > "$q.tmp" 2>/dev/null && mv "$q.tmp" "$q" 2>/dev/null \
		&& log "auto_setup 전달 : $(grep -c . "$hostfile")대 ($q)" \
		|| warn "[!] auto_setup 전달 실패 ($q)"
else
	warn "[!] auto_setup 전달 실패 ($auto_setup_queue 생성 불가)"
fi
fi   # auto_setup_host
# [15] 등록 후 확인: 붙여넣은 서버가 모두 이번 등록 대상에 있는지 검사
verify_hosts() {
	local pasted="" line pf missing extra n
	echo "${BOLD}등록 후 확인${RST} : 작업 대상 서버 목록을 붙여넣으세요 (공백/쉼표/| 구분 가능, 입력을 마치려면 빈 줄 / 건너뛰려면 바로 빈 줄)"
	if [[ -n $AWX_VERIFY_FILE && -f $AWX_VERIFY_FILE && -r $AWX_VERIFY_FILE && -s $AWX_VERIFY_FILE ]]; then
		# auto_setup 에서 넘긴 대상 서버 목록 (stdin 입력 생략)
		pasted=$(cat "$AWX_VERIFY_FILE")$'\n'
		echo "붙여넣을 목록: auto_setup 에서 입력한 $(printf '%s' "$pasted" | tr ',|' '  ' | tr -s '[:space:]' '\n' | grep -c .)대"
	else
	while IFS= read -r line; do
		[[ -z ${line//[[:space:]]/} ]] && break
		pasted+="$line"$'\n'
	done
	fi
	if [[ -z $pasted ]]; then log "대상 확인 생략"; return 0; fi
	pf=$(mktemp); add_tmp "$pf"
	printf '%s' "$pasted" | tr ',|' '  ' | tr -s '[:space:]' '\n' | grep . | LC_ALL=C sort -u > "$pf"
	n=$(wc -l < "$pf")
	missing=$(LC_ALL=C comm -23 "$pf" <(LC_ALL=C sort -u "$hostfile"))
	extra=$(LC_ALL=C comm -13 "$pf" <(LC_ALL=C sort -u "$hostfile"))
	if [[ -n $extra ]]; then
		warn "[!] 붙여넣지 않은 등록 대상 ($(printf '%s\n' "$extra" | grep -c .)대) : $(printf '%s\n' "$extra" | paste -sd' ' -)"
	fi
	if [[ -n $missing ]]; then
		err "[X] 등록 대상에 없는 서버 ($(printf '%s\n' "$missing" | grep -c .)대) : $(printf '%s\n' "$missing" | paste -sd' ' -)"
		return 1
	fi
	echo "${GREEN}붙여넣은 대상 서버 ${n}대가 모두 존재함${RST}"
}
log "[15] verify_hosts"
verify_hosts || exit 1
log "${GREEN}완료${RST}"
