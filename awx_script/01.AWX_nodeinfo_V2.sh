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
	[[ ${#tmp_paths[@]} -gt 0 ]] && rm -rf "${tmp_paths[@]}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

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
	while IFS= read -r line; do
		[[ -z ${line//[[:space:]]/} ]] && break
		pasted+="$line"$'\n'
	done
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
