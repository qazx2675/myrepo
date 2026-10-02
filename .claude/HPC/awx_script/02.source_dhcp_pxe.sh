#!/bin/bash
# AWX 인벤토리 소스 등록 / DHCP / PXE - yml 별 자동 반복 (인자: <yml>=<infra>,<os>,<boot>,<splunk> ...)
user=$1
[[ $# -gt 0 ]] && shift
if [[ -z $user ]]; then
	read -r -p ".txt를 제외한 user 입력 : " user
fi

awxdir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/awxkit"

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
		echo "[X] 인자 형식 오류(<yml>=<infra>,<os>,<boot>,<splunk>): $spec"
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
	echo "번호 | yml | infra | os | boot | splunk | 호스트 수"
	for ((i=0; i<total; i++)); do
		cnt="-"
		[[ ${ymls[i]} =~ _([0-9]+)ea(\.|$) ]] && cnt=${BASH_REMATCH[1]}
		echo "$((i+1)) | ${ymls[i]} | ${infras[i]} | ${oss[i]} | ${boots[i]} | ${splunks[i]} | $cnt"
	done
}

print_table
while true; do
	read -r -p "옵션이 맞습니까 (Y|N) : " ok || { echo "[X] 입력이 끝났습니다"; exit 1; }
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
	echo "옵션 확정"
	print_table
fi

# yml 별 순차 실행: invsync 후 dhcp 와 pxe 는 동시 실행하고, 둘 다 끝나야 다음 yml 로 진행
# (한 단계 실패 시 해당 yml 의 나머지는 건너뛰고 다음 yml 진행)
results=(); fail_cnt=0
outdir=$(mktemp -d)
trap 'rm -rf "$outdir"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
for ((i=0; i<total; i++)); do
	yml=${ymls[i]}; infra=${infras[i]}; os=${oss[i]}; boot=${boots[i]}; splunk=${splunks[i]}
	echo "===== [$((i+1))/$total] $yml ====="
	if ! bash "$awxdir/invsync.sh" -user "${user}" -file "$yml"; then
		results+=("실패 (invsync) : $yml"); ((fail_cnt++)); continue
	fi
	# 동시 실행이라 프롬프트를 받을 수 없으므로 stdin 을 닫고, 출력은 끝난 뒤 순서대로 보여 줌
	bash "$awxdir/dhcp.sh" -user "${user}" -infra "$infra" < /dev/null > "$outdir/dhcp.out" 2>&1 &
	dhcp_pid=$!
	bash "$awxdir/pxe.sh" -user "${user}" -infra "$infra" -os "$os" -boot "$boot" -splunk "$splunk" < /dev/null > "$outdir/pxe.out" 2>&1 &
	pxe_pid=$!
	dhcp_rc=0; pxe_rc=0
	wait "$dhcp_pid" || dhcp_rc=$?
	wait "$pxe_pid" || pxe_rc=$?
	echo "--- dhcp ---"; cat "$outdir/dhcp.out"
	echo "--- pxe ---"; cat "$outdir/pxe.out"
	if [[ $dhcp_rc -ne 0 && $pxe_rc -ne 0 ]]; then
		results+=("실패 (dhcp, pxe) : $yml"); ((fail_cnt++)); continue
	elif [[ $dhcp_rc -ne 0 ]]; then
		results+=("실패 (dhcp) : $yml"); ((fail_cnt++)); continue
	elif [[ $pxe_rc -ne 0 ]]; then
		results+=("실패 (pxe) : $yml"); ((fail_cnt++)); continue
	fi
	results+=("성공 : $yml")
done

echo "===== 요약 : 전체 $total / 성공 $((total - fail_cnt)) / 실패 $fail_cnt ====="
printf '%s\n' "${results[@]}"
[[ $fail_cnt -eq 0 ]] || exit 1
exit 0
