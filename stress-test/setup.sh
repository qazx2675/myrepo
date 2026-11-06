#!/bin/bash
# stress-test 최초 설정 (대화형)
# conf/${user}_setting.conf 를 conf/sample_setting.conf 기준으로 만들거나, 기존 값을 기본값으로 보여 주며 수정한다.
# 사용법: bash setup.sh [--user <사용자>] [--conf <설정파일>]
#   --user 생략 시 STRESS_USER → $USER, --conf 생략 시 <이 스크립트 폴더>/conf/${user}_setting.conf
# 종료코드: 0 저장 완료 / 1 사용자 중단 / 2 오류
# setup_check 의 setup.sh 에서도 이 규약으로 호출한다. (bash 4.1 호환)

sd=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
sample="$sd/conf/sample_setting.conf"

die() { echo "오류: $*" >&2; exit 2; }
abort() { stty echo 2>/dev/null; echo; echo "중단했습니다. 설정 파일은 바뀌지 않았습니다." >&2; exit 1; }
trap abort INT

user=""; conf=""
while [[ $# -gt 0 ]]; do
	case $1 in
		--user) [[ -n $2 ]] || die "--user 값이 없습니다"; user=$2; shift 2 ;;
		--conf) [[ -n $2 ]] || die "--conf 값이 없습니다"; conf=$2; shift 2 ;;
		-h|--help) sed -n '2,7p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		*) die "알 수 없는 인자: $1 (사용법: bash setup.sh [--user <사용자>] [--conf <설정파일>])" ;;
	esac
done
[[ -n $user ]] || user=${STRESS_USER:-$USER}
[[ $user =~ ^[A-Za-z0-9._-]+$ ]] || die "사용자를 정할 수 없거나 형식이 올바르지 않습니다: '$user' (--user 로 지정)"
[[ -n $conf ]] || conf="$sd/conf/${user}_setting.conf"
[[ -t 0 ]] || die "대화형 전용입니다 (터미널에서 실행하세요)"
[[ -f $sample ]] || die "샘플 설정이 없습니다: $sample"

trim() { local s=$1; s=${s#"${s%%[![:space:]]*}"}; s=${s%"${s##*[![:space:]]}"}; printf '%s' "$s"; }

# conf 한 줄 → key / value / comment (Go 파서와 같게: '#' 이후 주석, 첫 '=' 기준)
split_line() {
	local line=$1 body
	L_KEY=""; L_VAL=""; L_CMT=""
	body=${line%%#*}
	[[ $line == *#* ]] && L_CMT="#${line#*#}"
	[[ $body == *=* ]] || return 1
	L_KEY=$(trim "${body%%=*}")
	L_VAL=$(trim "${body#*=}")
	[[ -n $L_KEY ]]
}

# 샘플: 키 순서·기본값·설명
declare -a keys=()
declare -A def=() hint=() cur=()
while IFS= read -r line || [[ -n $line ]]; do
	line=${line%$'\r'}
	[[ $(trim "$line") == \#* ]] && continue
	split_line "$line" || continue
	keys+=("$L_KEY"); def[$L_KEY]=$L_VAL; hint[$L_KEY]=$(trim "${L_CMT#\#}")
done < "$sample"

# 기존 설정
declare -a extra=()
if [[ -f $conf ]]; then
	while IFS= read -r line || [[ -n $line ]]; do
		line=${line%$'\r'}
		[[ $(trim "$line") == \#* ]] && continue
		split_line "$line" || continue
		[[ -n ${def[$L_KEY]+x} ]] || extra+=("$L_KEY")
		cur[$L_KEY]=$L_VAL
	done < "$conf"
	echo "[i] 기존 설정을 기본값으로 보여 줍니다: $conf"
	# cpu_intel_*/cpu_amd_* 는 없어짐(cpu_template 하나) → Intel 값(Intel 템플릿이 비면 AMD)을 cpu_* 기본값으로 옮기고 저장할 때 뺀다
	old_cpu=""; keep=()
	for k in "${extra[@]}"; do
		case $k in cpu_intel_*|cpu_amd_*) old_cpu+=" $k" ;; *) keep+=("$k") ;; esac
	done
	extra=("${keep[@]}")
	if [[ -n $old_cpu ]]; then
		echo "[!] 없어진 키:$old_cpu — cpu 템플릿은 cpu_template 하나로 바뀌었습니다 (Intel/AMD 는 템플릿 안에서 구분)"
		if [[ -z ${cur[cpu_template]+x} ]]; then
			g=intel; [[ -z ${cur[cpu_intel_template]} ]] && g=amd
			for f in template hostname_key extra_vars; do cur[cpu_$f]=${cur[cpu_${g}_$f]}; done
			echo "    cpu_${g}_* 값을 cpu_template/cpu_hostname_key/cpu_extra_vars 기본값으로 옮겼습니다 (Intel 템플릿이 비어 있으면 AMD). 저장하면 옛 키는 빠집니다"
		fi
	fi
	# 기존 설정에 없는 (새로 생긴) 키는 샘플 값을 기본값으로
	for k in "${keys[@]}"; do [[ -n ${cur[$k]+x} ]] || cur[$k]=${def[$k]}; done
else
	echo "[i] 새 설정 파일을 만듭니다: $conf (값은 샘플 기준)"
	for k in "${keys[@]}"; do cur[$k]=${def[$k]}; done
fi
echo "    Enter = 현재값 유지, '-' = 값 비우기"
echo

check_val() {   # key value → 오류 메시지(문제 없으면 빈 값)
	local k=$1 v=$2
	[[ $v == *#* ]] && { echo "'#' 은 값에 쓸 수 없습니다"; return; }
	[[ -z $v ]] && return
	case $k in
		monitor_sec|monitor_interval|poll_interval) [[ $v =~ ^[1-9][0-9]*$ ]] || echo "1 이상의 정수여야 합니다" ;;
		load_threshold) [[ $v =~ ^[0-9]*\.?[0-9]+$ && ! $v =~ ^0*\.?0*$ ]] || echo "0 보다 큰 숫자여야 합니다" ;;
		insecure_tls) [[ $v =~ ^(true|false|yes|no|1|0|on|off)$ ]] || echo "true 또는 false 로 입력하세요" ;;
		stress_end_keys) [[ $v =~ ^[[:space:]]*[^,[:space:]]+[[:space:]]*(,[[:space:]]*[^,[:space:]]+[[:space:]]*){4}$ ]] || echo "쉼표로 구분한 5개(연,월,일,시,분 순 survey 변수명)여야 합니다. 예: end_year,end_month,end_day,end_hour,end_min" ;;
	esac
}

section=""
for k in "${keys[@]}"; do
	case $k in
		awx_url) s="AWX 접속" ;; gossh) s="gossh" ;; repohost) s="인벤토리 등록 (AWX 01 방식)" ;;
		inventory_source) s="인벤토리 소스" ;; cpu_template) s="템플릿 (cpu: 하나, stress: stress-ng) — survey key 는 stress-test -inspect 로 확인" ;;
		default_inventory) s="템플릿 인벤토리 (실행 때 username 인벤토리로 바꿨다가 되돌림)" ;;
		monitor_sec) s="감시" ;; output_dir) s="공통" ;; *) s="" ;;
	esac
	[[ -n $s && $s != "$section" ]] && { section=$s; echo "── $s ──"; }
	[[ -n ${hint[$k]} ]] && echo "  # ${hint[$k]}"
	while true; do
		if [[ $k == password ]]; then
			shown="(비어 있음)"; [[ -n ${cur[$k]} ]] && shown="(설정됨)"
			read -r -s -p "  $k [$shown]: " v || abort
			echo
		else
			read -r -p "  $k [${cur[$k]}]: " v || abort
		fi
		v=$(trim "$v")
		[[ -z $v ]] && v=${cur[$k]}
		[[ $v == - ]] && v=""
		msg=$(check_val "$k" "$v")
		[[ -z $msg ]] && break
		echo "  → $msg"
	done
	cur[$k]=$v
done

# 필수값 안내 (stress-test 는 비어 있으면 종료코드 2)
miss=""
for k in awx_url username password repohost repo_dir svr_dir; do [[ -n ${cur[$k]} ]] || miss+=" $k"; done
[[ -n $miss ]] && echo "[!] 비어 있는 필수 값:$miss (stress-test 실행 시 오류가 납니다)"
echo "[i] 템플릿 survey 변수명(*_hostname_key, stress_end_keys)은 stress-test -inspect 로 key 확인"

echo
echo "── 저장할 값 ──"
for k in "${keys[@]}"; do
	v=${cur[$k]}; [[ $k == password && -n $v ]] && v="********"
	printf '  %-24s = %s\n' "$k" "$v"
done
read -r -p "저장할까요? (Y/n): " yn || abort
[[ $yn =~ ^[Nn] ]] && { echo "저장하지 않았습니다."; exit 1; }

# 샘플 줄 구조(주석·순서)를 그대로 두고 값만 바꿔 쓴다
tmp=$(mktemp "${conf}.XXXXXX" 2>/dev/null) || { mkdir -p "$(dirname "$conf")" && tmp=$(mktemp "${conf}.XXXXXX"); } || die "설정 파일을 쓸 수 없습니다: $conf"
{
	while IFS= read -r line || [[ -n $line ]]; do
		line=${line%$'\r'}
		if [[ $(trim "$line") != \#* ]] && split_line "$line"; then
			out="$L_KEY = ${cur[$L_KEY]}"
			[[ -n $L_CMT ]] && out=$(printf '%-30s %s' "$out" "$L_CMT")
			printf '%s\n' "$out"
		else
			printf '%s\n' "$line"
		fi
	done < "$sample"
	if [[ ${#extra[@]} -gt 0 ]]; then
		echo
		echo "# ── 기존 설정에서 옮긴 키 (샘플에 없음) ──"
		for k in "${extra[@]}"; do echo "$k = ${cur[$k]}"; done
	fi
} > "$tmp" || { rm -f "$tmp"; die "설정 파일을 쓸 수 없습니다: $conf"; }
if [[ -f $conf ]]; then
	bak="$conf.bak.$(date +%Y%m%d%H%M%S)"
	cp -p "$conf" "$bak" && echo "[i] 기존 파일 백업: $bak"
fi
chmod 600 "$tmp"
mv -f "$tmp" "$conf" || { rm -f "$tmp"; die "설정 파일을 쓸 수 없습니다: $conf"; }
echo "[✔] 저장했습니다: $conf (권한 600)"
echo "[i] 템플릿 key·인벤토리 확인: stress-test -inspect -conf $conf   (survey 변수명, stress_end_keys 점검)"

# ==== 선택 점검 (실패해도 저장은 유지, 경고만) ====
read -r -p "접속 점검(AWX·템플릿·repohost·폴더)을 할까요? (y/N): " yn || exit 0
[[ $yn =~ ^[Yy] ]] || exit 0

curl_awx() {   # 경로 → HTTP 본문 (마지막 줄에 상태코드). 비밀번호는 명령줄에 노출하지 않는다
	local u=${cur[username]//\\/\\\\} p=${cur[password]//\\/\\\\} k=""
	u=${u//\"/\\\"}; p=${p//\"/\\\"}
	[[ ${cur[insecure_tls]} =~ ^(true|yes|1|on)$ ]] && k="-k"
	curl -s $k -m 10 -K <(printf 'user = "%s:%s"\n' "$u" "$p") -w '\n%{http_code}' "${cur[awx_url]%/}$1"
}
urlenc() { printf '%s' "$1" | od -An -tx1 | tr -d ' \n' | sed 's/../%&/g'; }
if ! command -v curl >/dev/null; then
	echo "[!] curl 이 없어 AWX 점검을 건너뜁니다"
elif [[ -n ${cur[awx_url]} ]]; then
	code=$(curl_awx /api/v2/me/ | tail -n1)
	case $code in
		200) echo "[✔] AWX 접속·로그인 성공"
			for t in cpu_template stress_template; do
				name=${cur[$t]}; [[ -n $name ]] || continue
				if [[ $name =~ ^[0-9]+$ ]]; then path="/api/v2/job_templates/$name/"
				else path="/api/v2/job_templates/?name=$(urlenc "$name")"; fi
				res=$(curl_awx "$path"); c=$(tail -n1 <<<"$res")
				if [[ $c == 200 && ! $res =~ \"count\":[[:space:]]*0[,}] ]]; then echo "[✔] $t: $name 조회됨"
				else echo "[!] $t: $name 을 찾지 못했습니다 (HTTP $c)"; fi
			done
			# 템플릿 실행 때 바꿀 인벤토리(username)와 되돌릴 인벤토리(default_inventory)
			for t in username default_inventory; do
				name=${cur[$t]}; [[ -n $name ]] || continue
				res=$(curl_awx "/api/v2/inventories/?name=$(urlenc "$name")"); c=$(tail -n1 <<<"$res")
				if [[ $c == 200 && ! $res =~ \"count\":[[:space:]]*0[,}] ]]; then echo "[✔] 인벤토리 $name ($t) 조회됨"
				else echo "[!] 인벤토리 $name ($t) 을 찾지 못했습니다 (HTTP $c) — 없으면 stress-test 가 종료코드 2"; fi
			done ;;
		401|403) echo "[!] AWX 로그인 실패 (HTTP $code): username/password 확인" ;;
		*) echo "[!] AWX 접속 실패 (HTTP ${code:-없음}): awx_url/insecure_tls 확인" ;;
	esac
fi
if [[ -n ${cur[repohost]} ]]; then
	if ssh -o BatchMode=yes -o ConnectTimeout=5 "${cur[repohost]}" "test -f '${cur[repo_dir]}/custom_inventory.sh'" </dev/null 2>/dev/null; then
		echo "[✔] repohost 접속, ${cur[repo_dir]}/custom_inventory.sh 있음"
	else
		echo "[!] repohost(${cur[repohost]}) 에 비밀번호 없이 접속되지 않거나 ${cur[repo_dir]}/custom_inventory.sh 가 없습니다"
	fi
fi
if [[ -n ${cur[svr_dir]} ]]; then
	if [[ -d ${cur[svr_dir]} ]]; then echo "[✔] svr_dir 있음: ${cur[svr_dir]}"
	else echo "[!] svr_dir 폴더가 없습니다: ${cur[svr_dir]}"; fi
fi
if [[ -n ${cur[git_dir]} ]]; then
	g=${cur[git_dir]//'${user}'/$user}
	if [[ -f $g/.git_upload.sh ]]; then echo "[✔] git_dir: $g/.git_upload.sh 있음"
	else echo "[!] git_dir 에 .git_upload.sh 가 없습니다: $g"; fi
fi
exit 0
