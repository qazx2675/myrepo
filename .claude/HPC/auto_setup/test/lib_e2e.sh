# lib_e2e.sh - run_e2e.sh / manual_check.sh 공용 도우미 (source 전용): 스크래치 빌드·가짜 gossh/ssh/wall·01/os_check 복사본 실행
#
# 호출 전에 ROOT(auto_setup 프로젝트 루트), S(스크래치 디렉터리) 를 정해 둘 것.
# 원본 01 / os_check 는 읽기만 하고, 복사본에만 테스트 값(빈 변수·경로)을 채운다.
# shellcheck shell=bash disable=SC2034,SC2154

E2E_USER=e2euser
E2E_MGMT=os6.mgmt

e2e_paths() {
	SRC01="$ROOT/../awx_script/01.AWX_nodeinfo_V2.sh"
	SRCOC="$ROOT/../OS 환경설정 체크/os_check_final_annotated.sh"
	BIN="$S/bin"        # 데몬/os_check 단계 PATH 스텁 (gossh ssh wall)
	BIN01="$S/bin01"    # 01 단계 PATH 스텁 (ssh scp gossh)
	OS6="$S/os6"        # os6_autosetup 디렉터리 + os6_gossh(가짜 원격 gossh)
	OC="$S/oc"          # os_check 복사본 위치
	W01="$S/w01"        # 01 복사본 작업 디렉터리
	export E2E_MGMT
}

# e2e_scen 이름 : 시나리오/로그 디렉터리를 새로 만들고 E2E_SCEN 으로 export
e2e_scen() {
	E2E_SCEN="$S/scen_$1"
	rm -rf "$E2E_SCEN"
	mkdir -p "$E2E_SCEN"
	export E2E_SCEN
}

# e2e_make_stubs : 스텁·os_check 복사본 생성
e2e_make_stubs() {
	e2e_paths
	mkdir -p "$BIN" "$BIN01" "$OS6" "$OC" "$S/tmp"

	# ---- 가짜 gossh (로컬/os6 공용). 시나리오: $E2E_SCEN/{refused,pingx,fail}_hosts ----
	cat > "$BIN/gossh" <<'STUB'
#!/bin/bash
# 가짜 gossh (E2E 용): 인자를 [ ] 로 감싸 gossh.log 에 기록, 명령별로 고정 응답
SCEN=${E2E_SCEN:?E2E_SCEN 미설정}
tag=local; [[ $0 == */os6/* ]] && tag=os6
{ printf '%s' "$tag"; printf ' [%s]' "$@"; echo; } >> "$SCEN/gossh.log"
hf=""; cmd=""
while [[ $# -gt 0 ]]; do
	case $1 in
		-pm|-script) shift ;;
		-w) hf=$2; shift 2 ;;
		*) [[ -z $cmd ]] && cmd=$1; shift ;;
	esac
done
[[ -f $hf ]] || exit 1
mapfile -t hs < <(grep . "$hf")
echo "$tag hosts=${hs[*]} cmd=$cmd" >> "$SCEN/gossh.hosts"
has() { grep -qxF "$1" "$SCEN/$2" 2>/dev/null; }
# 2차 체크(os6_mgmt 에서 실행되는 os_check)는 E2E_SECOND=1 → fail2_hosts / refused2_hosts 를 본다
ff=fail_hosts; rf=refused_hosts
if [[ -n $E2E_SECOND ]]; then ff=fail2_hosts; rf=refused2_hosts; fi
# LDAP 백업/비교/복원 흉내 (E2E_LDAP_ROOT 가 있을 때만): 원격 스크립트를 풀어 R= 를 호스트별 가짜 루트로 바꿔 로컬 실행
ldap_emul() {
	local host=$1 c=$2 b outer inner script root kind
	root="$E2E_LDAP_ROOT/$host"
	[[ -d $root ]] || { echo "$host: ERROR no root"; return 0; }
	b=${c#echo }; b=${b%% |*}
	outer=$(printf '%s' "$b" | tr -d . | base64 -d 2>/dev/null)
	inner=$(sed -n 's/.*echo \([A-Za-z0-9+\/=]*\) | base64 -d > .*/\1/p' <<< "$outer")
	script=$(printf '%s' "$inner" | base64 -d 2>/dev/null)
	grep -q '^R=' <<< "$script" || return 0
	case $script in
		*"==PUT"*) kind=restore ;;
		*"==FILE"*) kind=collect ;;
		*) kind=probe ;;
	esac
	echo "ldap-$kind $host" >> "$SCEN/order.log"
	printf '%s\n' "$script" | sed "s#^R=.*#R='$root'#" > "$SCEN/.ldap_$$.sh"
	bash "$SCEN/.ldap_$$.sh" < /dev/null 2> /dev/null | sed "s/^/$host: /"
	rm -f "$SCEN/.ldap_$$.sh"
}
case $cmd in "bash "*run.sh) echo "run hosts=${hs[*]}" >> "$SCEN/order.log" ;; esac
for h in "${hs[@]}"; do
	case $cmd in
		"cat /proc/uptime")
			if has "$h" bootold_hosts; then echo "$h: 999999.00 1.00"; else echo "$h: 0.50 0.40"; fi ;;
		hostname)
			if has "$h" "$rf"; then echo "$h: ERROR Connection refused"
			elif has "$h" pingx_hosts; then echo "$h: ERROR ping 불가"
			else echo "$h: $h"; fi ;;
		"uname -r") echo "$h: 5.14.0-1" ;;
		"bash -c "*AUTO_DONE_OK*) bash -c "$cmd" 2> /dev/null | sed "s/^/$h: /" ;;
		"echo "*"base64 -d | bash") [[ -n $E2E_LDAP_ROOT ]] && ldap_emul "$h" "$cmd" ;;
		"bash "*run.sh)
			if has "$h" "$ff"; then echo "$h: FAIL selinux enforcing"; else echo "$h: OK"; fi ;;
		"bash "*info_check.sh) echo "$h: INFO ldap infra1 siteA"; echo "$h: INFO SDS Splunk typeA" ;;
		"bash "*.sh) echo "$h: applied" ;;
	esac
done
exit 0
STUB
	cp "$BIN/gossh" "$OS6/gossh"

	# ---- 가짜 ssh: os6_mgmt 로 가는 호출만 흉내 (probe 는 전부 up, 나머지는 로컬 bash -c) ----
	cat > "$BIN/ssh" <<'STUB'
#!/bin/bash
SCEN=${E2E_SCEN:?E2E_SCEN 미설정}
{ printf 'ssh'; printf ' [%s]' "$@"; echo; } >> "$SCEN/ssh.log"
while [[ $1 == -* ]]; do
	case $1 in -o) shift 2 ;; *) shift ;; esac
done
host=$1; shift
[[ $host == "$E2E_MGMT" ]] || { echo "stub ssh: 알 수 없는 호스트 $host" >&2; exit 255; }
cmd="$*"
if [[ $cmd == *" probe "* ]]; then
	while IFS= read -r l && [[ -n $l ]]; do
		echo "probe-list: $l" >> "$SCEN/probe.log"
		# shellcheck disable=SC2086
		set -- $l
		echo "$1 up"
	done
	cat > /dev/null   # stdin EOF(데몬이 세션을 닫음) 까지 유지
	exit 0
fi
[[ $cmd == *" -auto "* ]] && export E2E_SECOND=1
exec bash -c "$cmd"
STUB

	# ---- 가짜 wall ----
	cat > "$BIN/wall" <<'STUB'
#!/bin/bash
SCEN=${E2E_SCEN:?E2E_SCEN 미설정}
{ echo "=====WALL====="; cat; } >> "$SCEN/wall.log"
STUB

	# ---- 01 단계 스텁 ----
	cat > "$BIN01/ssh" <<'STUB'
#!/bin/bash
echo "ssh $*" >> "$STUBLOG/calls.log"
shift
cmd="$*"
if [[ $cmd =~ custom_inventory\.sh[[:space:]]+([^[:space:]]+) ]]; then
	f="$REMOTE_DIR/$(basename "${BASH_REMATCH[1]}")"
	[[ -f $f ]] || { echo "stub ssh: 입력 파일 없음 $f" >&2; exit 1; }
	n=$(wc -l < "$f")
	infra=$(awk 'NR==1 {print $3}' "$f")
	{ echo "# generated from $(basename "$f")"; cat "$f"; } > "$SVR_DIR/${infra}_inventory-$(date +%s)_${n}ea.yml"
fi
exit 0
STUB
	cat > "$BIN01/scp" <<'STUB'
#!/bin/bash
echo "scp $*" >> "$STUBLOG/calls.log"
args=("$@")
last=$((${#args[@]} - 1))
mkdir -p "$REMOTE_DIR"
for ((i = 0; i < last; i++)); do cp "${args[i]}" "$REMOTE_DIR/" || exit 1; done
exit 0
STUB
	cat > "$BIN01/gossh" <<'STUB'
#!/bin/bash
hf=""; cmd=""
while [[ $# -gt 0 ]]; do
	case $1 in -w) hf=$2; shift 2 ;; -pm|-script) shift ;; *) cmd=$1; shift ;; esac
done
if [[ $cmd == *AUTO_SETUP_OK* ]]; then   # 01 의 os8_mgmt 원샷 전달: 로컬 bash 로 실행(AUTO_SETUP_DIR 상속) + "host: " 접두
	h=$(head -1 "$hf")
	echo "gossh-remote $h" >> "$STUBLOG/calls.log"
	bash -c "$cmd" 2> /dev/null | sed "s/^/$h: /"
	exit 0
fi
while read -r h; do [[ -n $h ]] && printf '%s: ldap.lab\n' "$h"; done < "$hf"
exit 0
STUB
	chmod +x "$BIN"/* "$BIN01"/* "$OS6/gossh"

	e2e_make_oc
}

# e2e_make_oc : os_check 복사본 (경로 placeholder 4곳만 치환) + dhcp.sh 스텁
e2e_make_oc() {
	local n
	cp "$SRCOC" "$OC/os_check_final_annotated.sh"
	sed -i \
		-e "s#^RUN_SH_DIR=.*#RUN_SH_DIR=\"$S/check\"#" \
		-e "s#^SETTING_DIR=.*#SETTING_DIR=\"$S/setting\"#" \
		-e "s#^RCLOCAL_SH=.*#RCLOCAL_SH=\"$S/setting/rclocal.sh\"#" \
		-e "s#^INFO_CHECK_SH=.*#INFO_CHECK_SH=\"$S/check/info_check.sh\"#" \
		"$OC/os_check_final_annotated.sh"
	n=$(diff "$SRCOC" "$OC/os_check_final_annotated.sh" | grep -c '^>')
	if [[ $n -ne 4 ]]; then
		echo "[HARNESS ERROR] os_check 복사본 치환 줄 수 이상: $n (기대 4)" >&2
		return 2
	fi
	cat > "$OC/dhcp.sh" <<'STUB'
#!/bin/bash
echo "dhcp stub: $(paste -sd' ' "$1")"
STUB
	chmod +x "$OC/dhcp.sh"
}

# e2e_make_oc_var 대상디렉터리 auto_done_dir값 auto_done_host값 : 완료기록 변수만 채운 os_check 복사본 (+dhcp.sh 스텁)
# 원본과의 차이는 경로 4줄 + 변수 줄 2개뿐이어야 한다.
e2e_make_oc_var() {
	local d=$1 dd=$2 dh=$3 n want=0
	[[ -n $dd ]] && want=$((want + 1))
	[[ -n $dh ]] && want=$((want + 1))
	mkdir -p "$d"
	cp "$OC/os_check_final_annotated.sh" "$d/os_check_final_annotated.sh"
	cp "$OC/dhcp.sh" "$d/dhcp.sh"
	sed -i \
		-e "s#^auto_done_dir=\"\"#auto_done_dir=\"$dd\"#" \
		-e "s#^auto_done_host=\"\"#auto_done_host=\"$dh\"#" \
		"$d/os_check_final_annotated.sh"
	n=$(diff "$OC/os_check_final_annotated.sh" "$d/os_check_final_annotated.sh" | grep -c '^>')
	if [[ $n -ne $want ]]; then
		echo "[HARNESS ERROR] os_check 완료기록 복사본 치환 줄 수 이상: $n (기대 $want)" >&2
		return 2
	fi
}

# e2e_build : 소스(*_test.go 제외)를 스크래치에 복사해 빈 변수 4개를 -ldflags -X 로만 주입해 빌드
e2e_build() {
	e2e_paths
	rm -rf "$S/src"
	mkdir -p "$S/src" "$OS6"
	cp "$ROOT"/*.go "$ROOT/go.mod" "$S/src/"
	rm -f "$S"/src/*_test.go
	(cd "$S/src" && GOFLAGS=-buildvcs=false go build \
		-ldflags "-X main.os_check_sh=$OC/os_check_final_annotated.sh -X main.os6_mgmt=$E2E_MGMT -X main.os6_gossh=$OS6/gossh -X main.os6_autosetup=$OS6" \
		-o "$S/auto_setup" .) || return 1
	cp "$S/auto_setup" "$OS6/auto_setup"
}

# e2e_run_01 호스트명... : 01 복사본(스텁 02) 실행 → RC01, $S/out01.txt. AUTO_SETUP_DIR 은 호출자가 export.
e2e_run_01() {
	local W="$W01" SVR="$S/svr_dir" RD="$S/remote" h i=0 bad
	rm -rf "$W" "$SVR" "$RD" "$S/root_user"
	mkdir -p "$W/awxkit" "$W/.stublog" "$SVR" "$RD" "$S/root_user/$E2E_USER/myrepo" "$S/tmp"
	cp "$SRC01" "$W/01.AWX_nodeinfo_V2.sh"
	printf '#!/bin/bash\nexit 0\n' > "$W/02.source_dhcp_pxe.sh"
	printf '#!/bin/bash\nexit 0\n' > "$S/root_user/$E2E_USER/myrepo/.git_upload.sh"
	chmod +x "$W/02.source_dhcp_pxe.sh" "$S/root_user/$E2E_USER/myrepo/.git_upload.sh"
	sed -i \
		-e "s#^repohost=\"\"#repohost=\"repo.lab\"#" \
		-e "s#^svr_dir=\"\"#svr_dir=\"$SVR\"#" \
		-e "s#^lacp_comment=\"\"#lacp_comment=\"LACP-COMMENT\"#" \
		-e "s#^inventory_delete_host=\"\"#inventory_delete_host=\"delhost.lab\"#" \
		${E2E_01_HOST:+-e "s#^auto_setup_host=\"\"#auto_setup_host=\"$E2E_01_HOST\"#"} \
		-e "s#/root/user/#$S/root_user/#g" \
		"$W/01.AWX_nodeinfo_V2.sh"
	sed -i -e '/^user(){/,/^}/c\
user(){\
\tuser='"$E2E_USER"'\
}' "$W/01.AWX_nodeinfo_V2.sh"
	bad=$(diff "$SRC01" "$W/01.AWX_nodeinfo_V2.sh" | grep '^>' \
		| grep -vE '^> (repohost|svr_dir|lacp_comment|inventory_delete_host|auto_setup_host)=|^> '$'\t''user='"$E2E_USER"'$|root_user')
	if [[ -n $bad ]]; then echo "[HARNESS ERROR] 01 복사본 sed 가 예상 외 줄을 변경: $bad" >&2; return 2; fi
	: > "$W/$E2E_USER.txt"
	for h in "$@"; do
		i=$((i + 1))
		printf 'Dell R750 INFRA-A %s 10.9.0.%d aa:bb:cc:dd:ee:%02x eth0 sda sda5 1200 RHEL8 UEFI\n' "$h" "$i" "$i" >> "$W/$E2E_USER.txt"
	done
	(
		cd "$W" || exit 1
		printf 'N\nY\nsu\n\n' \
			| env PATH="$BIN01:$PATH" STUBLOG="$W/.stublog" SVR_DIR="$SVR" REMOTE_DIR="$RD" TMPDIR="$S/tmp" NO_COLOR=1 \
				timeout 120 bash ./01.AWX_nodeinfo_V2.sh 2>&1 | cat > "$S/out01.txt"
		echo "${PIPESTATUS[1]}" > "$S/rc01"
	)
	sleep 1   # tee(프로세스 치환) 마무리 대기
	RC01=$(cat "$S/rc01")
}

# e2e_run_oc 실행디렉터리 호스트목록파일 : os_check 복사본 -auto 실행 → RCOC, <디렉터리>/out.log
# PATH 앞에 $BIN(가짜 gossh), stdin 은 /dev/null (프롬프트가 있으면 즉시 EOF 로 드러남)
e2e_run_oc() {
	local d=$1 list=$2
	mkdir -p "$d"
	cp "$list" "$d/targets.txt"
	ln -sf "$OC/dhcp.sh" "$d/dhcp.sh"
	(
		cd "$d" || exit 1
		env PATH="$BIN:$PATH" E2E_SCEN="$E2E_SCEN" NO_COLOR=1 \
			timeout 120 bash "${OC_SCRIPT:-$OC/os_check_final_annotated.sh}" -auto "$E2E_USER" targets.txt < /dev/null > out.log 2>&1
		echo $? > rc
	)
	RCOC=$(cat "$d/rc")
}

# ---------------------------------------------------------------------------------------------
# 2차 시나리오(run_e2e2.sh / manual_check.sh 11~17) 공용

# e2e_build_variants : e2e_build 이후. 스크래치 src 로 2차 체크용(os6_os_check_sh)·원격 클라이언트용(os8_mgmt) 바이너리를 -ldflags -X 로만 만든다.
#   $S/auto_setup_2nd    : 기본 빌드 + os6_os_check_sh (os6_mgmt 위 os_check 흉내 = $OS6 의 복사본)
#   $S/auto_setup_no6    : os6_mgmt 도 비움 (os6 경유 호스트 없는 시나리오, 2차 체크 비활성)
#   $S/auto_setup_autofs : os6_mgmt 만 채우고 os6_os_check_sh 는 비움(os_check_sh 로 폴백, autofs 동일 경로) + awx_dir=$S/awx
#   $S/auto_setup_client : 원격 클라이언트 모드 (os8_mgmt=os8.mgmt, os8_autosetup=서버 쪽 로컬 바이너리 $S/auto_setup)
e2e_build_variants() {
	e2e_paths
	cp "$OC/os_check_final_annotated.sh" "$OS6/os_check_final_annotated.sh"
	cp "$OC/dhcp.sh" "$OS6/dhcp.sh"
	(cd "$S/src" && GOFLAGS=-buildvcs=false go build \
		-ldflags "-X main.os_check_sh=$OC/os_check_final_annotated.sh -X main.os6_mgmt=$E2E_MGMT -X main.os6_gossh=$OS6/gossh -X main.os6_autosetup=$OS6 -X main.os6_os_check_sh=$OS6/os_check_final_annotated.sh" \
		-o "$S/auto_setup_2nd" .) || return 1
	(cd "$S/src" && GOFLAGS=-buildvcs=false go build \
		-ldflags "-X main.os_check_sh=$OC/os_check_final_annotated.sh -X main.os6_gossh=$OS6/gossh -X main.os6_autosetup=$OS6" \
		-o "$S/auto_setup_no6" .) || return 1
	mkdir -p "$S/awx/awxkit" && cp "$OC/dhcp.sh" "$S/awx/awxkit/dhcp.sh" || return 1
	(cd "$S/src" && GOFLAGS=-buildvcs=false go build \
		-ldflags "-X main.os_check_sh=$OC/os_check_final_annotated.sh -X main.os6_mgmt=$E2E_MGMT -X main.os6_gossh=$OS6/gossh -X main.os6_autosetup=$OS6 -X main.awx_dir=$S/awx" \
		-o "$S/auto_setup_autofs" .) || return 1
	(cd "$S/src" && GOFLAGS=-buildvcs=false go build \
		-ldflags "-X main.os8_mgmt=os8.mgmt -X main.os8_autosetup=$S/auto_setup" \
		-o "$S/auto_setup_client" .) || return 1
}

# e2e_make_rgossh : 원격 클라이언트 모드용 가짜 gossh ($S/binr/gossh). -w 파일의 호스트마다 명령을 로컬 bash -c 로 실행하고
# gossh 처럼 줄마다 공백을 다듬고 빈 줄을 버린 뒤 "host: " 를 붙인다. 로컬 서버(os8_mgmt) 역할은 같은 머신이 한다.
e2e_make_rgossh() {
	mkdir -p "$S/binr"
	cat > "$S/binr/gossh" <<'STUB'
#!/bin/bash
hf=""; cmd=""
while [[ $# -gt 0 ]]; do
	case $1 in -pm|-script) shift ;; -w) hf=$2; shift 2 ;; *) [[ -z $cmd ]] && cmd=$1; shift ;; esac
done
[[ -f $hf ]] || exit 1
[[ -n $RGOSSH_LOG ]] && echo "$(head -1 "$hf") ${cmd:0:48}" >> "$RGOSSH_LOG"
while read -r h; do
	[[ -n $h ]] || continue
	bash -c "$cmd" 2>&1 < /dev/null | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' | grep -v '^$' | sed "s/^/$h: /"
done < "$hf"
exit 0
STUB
	chmod +x "$S/binr/gossh"
}

# _hj ip route seen_down ready_at processed miss fails stage stage_at down_at extra : job JSON 의 호스트 객체
_hj() {
	printf '{"ip":"%s","route":"%s","seen_down":%s,"ready_at":%s,"processed":"%s","miss":%s,"fails":%s,"stage":"%s","stage_at":%s,"down_at":%s%s}' "$@"
}

# e2e_sample_closed 디렉터리 : 종료된 job (jobs/done) — ok.yml 은 전 호스트 완료, bad.yml 은 실패 호스트 1대
E2E_CLOSED_JOB=20261002-100000-carol
e2e_sample_closed() {
	local d=$1 now N j
	now=$(date +%s); N=$now; j=$E2E_CLOSED_JOB
	mkdir -p "$d/jobs/done" "$d/codes" "$d/queue" "$d/runs" "$d/bin" "$d/done" "$d/requests"
	cat > "$d/jobs/done/$j.json" <<JSON
{"id":"$j","user":"carol","submitted":$((N - 9000)),"first_ready":$((N - 8000)),"late_first_ready":0,"first_run_done":true,
"hosts":{
"dk1":$(_hj 10.1.0.1 local true $((N - 8000)) 4821 0 0 "done" $((N - 7000)) $((N - 8600)) ',"done_src":"run"'),
"dk2":$(_hj 10.1.0.2 local true $((N - 8000)) 4821 0 0 "done" $((N - 7000)) $((N - 8600)) ',"done_src":"run"'),
"dk3":$(_hj 10.1.0.3 local true $((N - 8000)) "" 0 3 failed $((N - 7000)) $((N - 8600)) '')},
"runs":[{"code":"4821","hosts":["dk1","dk2"],"at":$((N - 7000))}],
"groups":{"ok.yml":{"infra":"I3","os":"RHEL8","boot":"UEFI","splunk":"typeA","hosts":["dk1","dk2"]},
"bad.yml":{"infra":"I4","os":"RHEL7","boot":"BIOS","splunk":"-","hosts":["dk3"]}},"all_yml":"all.yml"}
JSON
}

# e2e_sample_state 디렉터리 NOW : 모든 단계가 보이는 가짜 상태 (진행 중 job 2 + 종료 job 1 + code 4821)
#   alice: gpu.yml(완료·설치중·정체) / cpu.yml(READY·실패)   bob: 그룹 줄 없는 구버전 job (대기·부팅확인)
e2e_sample_state() {
	local d=$1 N=$2 j1=20261003-090000-alice j2=20261003-100000-bob
	e2e_sample_closed "$d"
	touch -d "@$((N - 100))" "$d/jobs/done/$E2E_CLOSED_JOB.json"
	cat > "$d/jobs/$j1.json" <<JSON
{"id":"$j1","user":"alice","submitted":$((N - 7200)),"first_ready":$((N - 60)),"late_first_ready":0,"first_run_done":true,
"hosts":{
"g1":$(_hj 10.0.0.1 local true $((N - 3000)) 4821 0 0 "done" $((N - 2000)) $((N - 6000)) ',"done_src":"run"'),
"g2":$(_hj 10.0.0.2 local true 0 "" 2 0 installing $((N - 900)) $((N - 1800)) ''),
"g3":$(_hj 10.0.0.3 local true 0 "" 2 0 deploying $((N - 4000)) $((N - 4000)) ''),
"c1":$(_hj 10.0.0.4 os6 true $((N - 60)) "" 0 0 ready $((N - 60)) $((N - 3000)) ',"boot_at":'$((N - 100))',"ldap":{"backup":"ok","bindpw":"diff","applied":true}'),
"c2":$(_hj 10.0.0.5 local true $((N - 500)) "" 0 3 failed $((N - 400)) $((N - 3000)) ',"second":"fail"')},
"runs":[{"code":"4821","hosts":["g1"],"at":$((N - 2000))}],
"groups":{"gpu.yml":{"infra":"I1","os":"RHEL8","boot":"UEFI","splunk":"typeA","hosts":["g1","g2","g3"]},
"cpu.yml":{"infra":"I2","os":"RHEL7","boot":"BIOS","splunk":"-","hosts":["c1","c2"]}},"all_yml":"all.yml"}
JSON
	cat > "$d/jobs/$j2.json" <<JSON
{"id":"$j2","user":"bob","submitted":$((N - 300)),"first_ready":0,"late_first_ready":0,"first_run_done":false,
"hosts":{
"b1":$(_hj 10.2.0.1 local false 0 "" 0 0 queued $((N - 250)) 0 ''),
"b2":$(_hj 10.2.0.2 local true 0 "" 0 0 booting $((N - 30)) $((N - 200)) ',"ldap":{"backup":"none","reason":"전달 시점 ping X"}')},
"runs":[]}
JSON
	printf '샘플 결과 코멘트\n############### 결과 리포트 ###############\n4821 sample\n###########################################\n원본 : /tmp/none\n' > "$d/codes/4821.txt"
}
