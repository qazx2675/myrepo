#!/bin/bash
# lib_e2e.sh - run_e2e.sh / manual_check.sh 공용 도우미 (source 전용): 스크래치 빌드·가짜 gossh/ssh/wall·01/os_check 복사본 실행
#
# 호출 전에 ROOT(auto_setup 프로젝트 루트), S(스크래치 디렉터리) 를 정해 둘 것.
# 원본 01 / os_check 는 읽기만 하고, 복사본에만 테스트 값(빈 변수·경로)을 채운다.
# shellcheck disable=SC2034,SC2154

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
for h in "${hs[@]}"; do
	case $cmd in
		"cat /proc/uptime") echo "$h: 0.50 0.40" ;;
		hostname)
			if has "$h" refused_hosts; then echo "$h: ERROR Connection refused"
			elif has "$h" pingx_hosts; then echo "$h: ERROR ping 불가"
			else echo "$h: $h"; fi ;;
		"uname -r") echo "$h: 5.14.0-1" ;;
		"bash "*run.sh)
			if has "$h" fail_hosts; then echo "$h: FAIL selinux enforcing"; else echo "$h: OK"; fi ;;
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
hf=""
while [[ $# -gt 0 ]]; do
	case $1 in -w) hf=$2; shift 2 ;; *) shift ;; esac
done
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
		-e "s#/root/user/#$S/root_user/#g" \
		"$W/01.AWX_nodeinfo_V2.sh"
	sed -i -e '/^user(){/,/^}/c\
user(){\
\tuser='"$E2E_USER"'\
}' "$W/01.AWX_nodeinfo_V2.sh"
	bad=$(diff "$SRC01" "$W/01.AWX_nodeinfo_V2.sh" | grep '^>' \
		| grep -vE '^> (repohost|svr_dir|lacp_comment|inventory_delete_host)=|^> '$'\t''user='"$E2E_USER"'$|root_user')
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
			timeout 120 bash "$OC/os_check_final_annotated.sh" -auto "$E2E_USER" targets.txt < /dev/null > out.log 2>&1
		echo $? > rc
	)
	RCOC=$(cat "$d/rc")
}
