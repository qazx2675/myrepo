#!/bin/bash
# AWX 인벤토리 소스 등록 / DHCP / PXE - yml 별 자동 반복 (인자: <yml>=<infra>,<os>,<boot>,<splunk> ...)
# dhcp / pxe 가 서로 다른 infra 이름을 쓰는 경우의 치환: "표시infra:넘길값" (공백/쉼표 구분, 표시infra 는 대소문자 무시). 비우면 치환 없음
dhcp_infra_alias=""          # 예: "infra1:asdf"   → dhcp -infra asdf
pxe_infra_alias=""           # 예: "infra1:asdfl"  → pxe  -infra asdfl
# 치환 목록에서 infra 에 해당하는 값을 찾는다 (없으면 infra 그대로): map_infra "<목록>" "<infra>"
map_infra() {
	local p k v=${2,,}
	for p in ${1//,/ }; do
		k=${p%%:*}
		if [[ ${k,,} == "$v" && $p == *:* ]]; then echo "${p#*:}"; return; fi
	done
	echo "$2"
}

# pxe -os 값 변환: awxkit 은 숫자만으로 된 값(2024 등)을 "선택지 번호"로 해석하므로,
# conf 의 s4_osver_choices 에서 해당 값의 순번을 찾아 번호로 넘긴다 (숫자가 아니거나 conf/선택지가 없으면 그대로)
os_arg() {
	local v=$1 conf="" cand line item idx=0
	[[ $v =~ ^[0-9]+$ ]] || { echo "$v"; return; }
	for cand in "$awxdir/conf/${user}_setting.conf" "$HOME/.awxkit/${user}_setting.conf"; do
		[[ -f $cand ]] && { conf=$cand; break; }
	done
	[[ -n $conf ]] || { echo "$v"; return; }
	line=$(awk -F= '$1 ~ /^[[:space:]]*s4_osver_choices[[:space:]]*$/ { sub(/^[^=]*=/, ""); sub(/#.*/, ""); print; exit }' "$conf")
	for item in ${line//,/ }; do
		idx=$((idx + 1))
		if [[ $item == "$v" ]]; then echo "$idx"; return; fi
	done
	echo "$v"
}

# 색상: 터미널이거나 01 이 AWX_COLOR=1 로 넘겼을 때만 사용 (NO_COLOR 가 있으면 끔)
if [[ ( -t 1 && -z $NO_COLOR ) || $AWX_COLOR == 1 ]]; then
	ESC=$'\033'
	RED=$ESC'[31m'; GREEN=$ESC'[32m'; YELLOW=$ESC'[33m'; CYAN=$ESC'[36m'; BOLD=$ESC'[1m'; RST=$ESC'[0m'
else
	RED=""; GREEN=""; YELLOW=""; CYAN=""; BOLD=""; RST=""
fi
err() { echo "${RED}$*${RST}"; }
warn() { echo "${YELLOW}$*${RST}"; }

user=$1
[[ $# -gt 0 ]] && shift
if [[ -z $user ]]; then
	read -r -p ".txt를 제외한 user 입력 : " user
fi

awxdir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/awxkit"

# --all=<yml> : 전체 호스트 yml. 그룹 yml 이 2개 이상이고 모두 성공했을 때 마지막에 invsync 만 수행
all_yml=""; specs=()
for a in "$@"; do
	if [[ $a == --all=* ]]; then all_yml=${a#--all=}; else specs+=("$a"); fi
done
set -- "${specs[@]}"

# 인자가 없으면 기존 방식(대화형 1개)으로 단독 실행
if [[ $# -eq 0 ]]; then
	ls -1 *.yml 2>/dev/null
	read -r -p "인벤토리 등록할 source yaml 파일 등록 : " yaml
	bash "$awxdir/invsync.sh" -user "${user}" -file "$yaml" || exit 1
	bash "$awxdir/dhcp.sh" -user "${user}" || exit 1
	bash "$awxdir/pxe.sh" -user "${user}" || exit 1
	exit 0
fi

# 인자 파싱: <yml>=<infra>,<os>,<boot>,<splunk>
ymls=(); infras=(); oss=(); boots=(); splunks=()
for spec in "$@"; do
	if [[ $spec != *=* ]]; then
		err "[X] 인자 형식 오류(<yml>=<infra>,<os>,<boot>,<splunk>): $spec"
		exit 1
	fi
	ymls+=("${spec%%=*}")
	IFS=, read -r f_infra f_os f_boot f_splunk <<< "${spec#*=}"
	infras+=("${f_infra,,}"); oss+=("$f_os"); boots+=("$f_boot"); splunks+=("$f_splunk")
done
total=${#ymls[@]}

# 옵션 확인표 (호스트 수는 yml 이름의 <N>ea 에서 추출)
print_table(){
	local i cnt
	echo "${BOLD}번호 | yml | infra | os | boot | splunk | 호스트 수${RST}"
	for ((i=0; i<total; i++)); do
		cnt="-"
		[[ ${ymls[i]} =~ _([0-9]+)ea(\.|$) ]] && cnt=${BASH_REMATCH[1]}
		echo "$((i+1)) | ${CYAN}${ymls[i]}${RST} | ${infras[i]} | ${oss[i]} | ${boots[i]} | ${splunks[i]} | $cnt"
	done
}

# OS 버전 선택: 01 이 넘긴 os 값은 쓰지 않고 아래 선택지로 정한다 (옵션 확인표 출력 전에 질문)
os_choices=(2024 2025 2026 2026-OPC_MDP 2026-ECAD_TCAD)
default_os="2026-ECAD_TCAD"
while true; do
	echo "${BOLD}OS 버전 선택${RST}"
	echo "  ${CYAN}1${RST}) 기본값 (${default_os}) — 모든 yml 동일"
	echo "  ${CYAN}2${RST}) yml 별로 직접 선택"
	read -r -p "${YELLOW}번호 : ${RST}" os_mode || { err "[X] 입력이 끝났습니다"; exit 1; }
	[[ $os_mode == [12] ]] && break
done
if [[ $os_mode == 1 ]]; then
	for ((i=0; i<total; i++)); do oss[i]=$default_os; done
else
	for ((j=0; j<${#os_choices[@]}; j++)); do echo "  ${CYAN}$((j+1))${RST}) ${os_choices[j]}"; done
	for ((i=0; i<total; i++)); do
		while true; do
			read -r -p "${YELLOW}${ymls[i]}에 사용할 버전 : ${RST}" n || { err "[X] 입력이 끝났습니다"; exit 1; }
			[[ $n =~ ^[1-5]$ ]] && break
			warn "[!] 1~${#os_choices[@]} 중에서 선택하세요"
		done
		oss[i]=${os_choices[n-1]}
	done
fi

print_table
while true; do
	read -r -p "${YELLOW}옵션이 맞습니까 (Y|N) : ${RST}" ok || { err "[X] 입력이 끝났습니다"; exit 1; }
	[[ $ok == [YyNn] ]] && break
done

# N 이면 yml 별로 값 재입력 (Enter = 현재값 유지) 후 확정
if [[ $ok == [Nn] ]]; then
	for ((i=0; i<total; i++)); do
		echo "[$((i+1))/$total] ${ymls[i]}"
		read -r -p "  infra [${infras[i]}] : " v; [[ -n $v ]] && infras[i]=${v,,}
		read -r -p "  os [${oss[i]}] : " v; [[ -n $v ]] && oss[i]=$v
		read -r -p "  boot [${boots[i]}] : " v; [[ -n $v ]] && boots[i]=$v
		read -r -p "  splunk [${splunks[i]}] : " v; [[ -n $v ]] && splunks[i]=$v
	done
	echo "${GREEN}옵션 확정${RST}"
	print_table
fi

# yml 별 순차 실행: invsync 후 dhcp 와 pxe 는 동시 실행하고, 둘 다 끝나야 다음 yml 로 진행
# (한 단계 실패 시 해당 yml 의 나머지는 건너뛰고 다음 yml 진행)
results=(); fail_cnt=0; ok=()
outdir=$(mktemp -d)
trap 'rm -rf "$outdir"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
for ((i=0; i<total; i++)); do
	yml=${ymls[i]}; infra=${infras[i]}; os=${oss[i]}; boot=${boots[i]}; splunk=${splunks[i]}
	echo "${BOLD}${CYAN}===== [$((i+1))/$total] $yml =====${RST}"
	if ! bash "$awxdir/invsync.sh" -user "${user}" -file "$yml"; then
		results+=("실패 (invsync) : $yml"); ((fail_cnt++)); continue
	fi
	dhcp_infra=$(map_infra "$dhcp_infra_alias" "$infra"); pxe_infra=$(map_infra "$pxe_infra_alias" "$infra")
	[[ $dhcp_infra == "$infra" ]] || echo "${YELLOW}[infra 치환] dhcp -infra $infra -> $dhcp_infra${RST}"
	[[ $pxe_infra == "$infra" ]] || echo "${YELLOW}[infra 치환] pxe -infra $infra -> $pxe_infra${RST}"
	pxe_os=$(os_arg "$os")
	[[ $pxe_os == "$os" ]] || echo "${YELLOW}[os 번호 변환] pxe -os $os -> $pxe_os (conf s4_osver_choices 순번)${RST}"
	# 동시 실행이라 프롬프트를 받을 수 없으므로 stdin 을 닫고, 출력은 끝난 뒤 순서대로 보여 줌
	bash "$awxdir/dhcp.sh" -user "${user}" -infra "$dhcp_infra" < /dev/null > "$outdir/dhcp.out" 2>&1 &
	dhcp_pid=$!
	bash "$awxdir/pxe.sh" -user "${user}" -infra "$pxe_infra" -os "$pxe_os" -boot "$boot" -splunk "$splunk" < /dev/null > "$outdir/pxe.out" 2>&1 &
	pxe_pid=$!
	dhcp_rc=0; pxe_rc=0
	wait "$dhcp_pid" || dhcp_rc=$?
	wait "$pxe_pid" || pxe_rc=$?
	echo "${CYAN}--- dhcp ---${RST}"; cat "$outdir/dhcp.out"
	echo "${CYAN}--- pxe ---${RST}"; cat "$outdir/pxe.out"
	if [[ $dhcp_rc -ne 0 && $pxe_rc -ne 0 ]]; then
		results+=("실패 (dhcp, pxe) : $yml"); ((fail_cnt++)); continue
	elif [[ $dhcp_rc -ne 0 ]]; then
		results+=("실패 (dhcp) : $yml"); ((fail_cnt++)); continue
	elif [[ $pxe_rc -ne 0 ]]; then
		results+=("실패 (pxe) : $yml"); ((fail_cnt++)); continue
	fi
	results+=("성공 : $yml"); ok[i]=1
done

# 그룹 yml 이 여러 개면, 모두 등록된 뒤 전체 yml 을 AWX 인벤토리 소스(1단계, invsync)까지만 적용해
# 인벤토리 호스트를 전체 대상으로 갱신한다 (그룹 yml 이 1개면 생략)
all_fail=0
if [[ -n $all_yml && $total -gt 1 ]]; then
	if [[ $fail_cnt -eq 0 ]]; then
		echo "${BOLD}${CYAN}===== 전체 호스트 인벤토리 소스 갱신 (invsync) : $all_yml =====${RST}"
		if bash "$awxdir/invsync.sh" -user "${user}" -file "$all_yml"; then
			results+=("성공 (전체 invsync) : $all_yml")
		else
			results+=("실패 (전체 invsync) : $all_yml"); all_fail=1
		fi
	else
		warn "[!] 실패한 yml 이 있어 전체 yml($all_yml) 인벤토리 소스 갱신은 건너뜁니다"
	fi
fi

echo "${BOLD}===== 요약 : 전체 $total / 성공 $((total - fail_cnt)) / 실패 $fail_cnt =====${RST}"
for r in "${results[@]}"; do
	if [[ $r == 성공* ]]; then echo "${GREEN}${r}${RST}"; else echo "${RED}${r}${RST}"; fi
done

# 최종 수량: On-premise=HPC, Cloud=SDS 로 표시하고 "구분 infra OS버전" 별로 합산 (성공한 yml 기준, 대수는 yml 이름의 <N>ea)
declare -A qty
sum=0
for ((i=0; i<total; i++)); do
	[[ -n ${ok[i]} ]] || continue
	[[ ${ymls[i]} =~ _([0-9]+)ea(\.|$) ]] || continue
	cnt=${BASH_REMATCH[1]}
	case ${splunks[i]} in
		On-premise) kind=HPC ;;
		Cloud) kind=SDS ;;
		*) kind=${splunks[i]} ;;
	esac
	key="$kind ${infras[i]} ${oss[i]}"
	qty[$key]=$(( ${qty[$key]:-0} + cnt )); sum=$((sum + cnt))
done
if [[ ${#qty[@]} -gt 0 ]]; then
	echo "${BOLD}===== 최종 수량 =====${RST}"
	while IFS= read -r key; do echo "$key : ${qty[$key]}대"; done < <(printf '%s\n' "${!qty[@]}" | LC_ALL=C sort)
	echo "합계 : ${sum}대"
fi


# 파티션 표준 확인: ${user}.txt 의 호스트에서 lsblk 를 읽어 표준 여부를 판정한다 (정보 출력만, 종료코드에 영향 없음)
#  표준 = 물리 파티션(LVM 아님), OS 디스크는 sda 또는 nvme0n1, /boot 또는 /boot/efi 500~512M, / 30G, /var 20G, swap 존재, /tmp(나머지)
#  이외의 마운트·파티션이 있으면 표준과 다른 파티션으로 보고한다 (용량은 lsblk 표시값 기준)
check_partitions() {
	echo "${BOLD}===== 파티션 표준 확인 =====${RST}"
	if [[ ! -f ${user}.txt ]]; then warn "[!] ${user}.txt 가 없어 파티션 확인을 건너뜁니다"; return 0; fi
	if ! command -v gossh >/dev/null 2>&1; then warn "[!] gossh 가 없어 파티션 확인을 건너뜁니다"; return 0; fi
	awk '{print $4}' "${user}.txt" | grep . | LC_ALL=C sort -u > "$outdir/hosts"
	gossh -script -w "$outdir/hosts" "lsblk -nl -o NAME,TYPE,SIZE,MOUNTPOINT" < /dev/null > "$outdir/lsblk"
	awk -v red="$RED" -v grn="$GREEN" -v rst="$RST" '
	function mib(s,   n, u) { n = s + 0; u = substr(s, length(s), 1); if (u == "K") return n / 1024; if (u == "M") return n; if (u == "G") return n * 1024; if (u == "T") return n * 1048576; return n / 1048576 }
	function diskof(nm) { if (nm ~ /^nvme/) sub(/p[0-9]+$/, "", nm); else sub(/[0-9]+$/, "", nm); return nm }
	{
		i = index($0, ": "); if (i < 2) next
		h = substr($0, 1, i - 1); n = split(substr($0, i + 2), f, /[ \t]+/)
		if (n < 3) next
		if (!(h in seen)) { seen[h] = 1; ord[++nh] = h }
		k = ++cnt[h]; nm[h, k] = f[1]; ty[h, k] = f[2]; sz[h, k] = f[3]; mp[h, k] = (n >= 4 ? f[4] : "")
	}
	END {
		for (x = 1; x <= nh; x++) {
			h = ord[x]; p = ""; rootdisk = ""; boot = 0; rootok = 0; varok = 0; swap = 0; tmp = 0
			for (k = 1; k <= cnt[h]; k++) {
				if (ty[h, k] !~ /^(disk|part|rom|loop)$/) p = p "; LVM/논리 볼륨 " nm[h, k]
				if (mp[h, k] == "/" && ty[h, k] == "part") rootdisk = diskof(nm[h, k])
			}
			if (rootdisk == "") p = p "; / 가 물리 파티션이 아님"
			else if (rootdisk !~ /^(sda|nvme0n1)$/) p = p "; OS 설치 디스크가 sda/nvme0n1 이 아님(" rootdisk ")"
			if (rootdisk != "") for (k = 1; k <= cnt[h]; k++) {
				if (ty[h, k] != "part" || diskof(nm[h, k]) != rootdisk) continue
				m = mp[h, k]; s = sz[h, k]
				if (m == "") { if (mib(s) > 2) p = p "; 마운트 없는 파티션 " nm[h, k] "(" s ")" }
				else if (m == "/boot" || m == "/boot/efi") { boot = 1; if (mib(s) < 500 || mib(s) > 512) p = p "; " m " " s "(500~512M 아님)" }
				else if (m == "/") { rootok = 1; if (s != "30G") p = p "; / " s "(30G 아님)" }
				else if (m == "/var") { varok = 1; if (s != "20G") p = p "; /var " s "(20G 아님)" }
				else if (m == "[SWAP]") swap = 1
				else if (m == "/tmp") tmp = 1
				else p = p "; 표준 외 " m "(" s ")"
			}
			if (rootdisk != "") {
				if (!boot) p = p "; /boot 또는 /boot/efi 없음"
				if (!varok) p = p "; /var 없음"
				if (!swap) p = p "; swap 없음"
				if (!tmp) p = p "; /tmp 없음"
			}
			if (p == "") std++; else { bad++; badline[bad] = h " : " substr(p, 3) }
		}
		if (nh == 0) { print "[!] lsblk 결과가 없어 확인하지 못했습니다"; exit }
		if (bad == 0) print grn "모든 호스트(" nh "대)가 표준 파티션입니다" rst
		else {
			print red "표준과 다른 파티션이 있습니다 (" bad "대 / 전체 " nh "대)" rst
			for (y = 1; y <= bad; y++) print red badline[y] rst
		}
	}' "$outdir/lsblk"
	local noresp
	noresp=$(LC_ALL=C comm -23 "$outdir/hosts" <(sed -n 's/^\([^ :]*\): .*/\1/p' "$outdir/lsblk" | LC_ALL=C sort -u))
	[[ -z $noresp ]] || warn "[!] lsblk 응답 없음(확인 불가) : $(printf '%s\n' "$noresp" | paste -sd' ' -)"
}
check_partitions

[[ $fail_cnt -eq 0 && $all_fail -eq 0 ]] || exit 1
exit 0
