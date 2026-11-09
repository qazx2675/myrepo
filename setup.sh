#!/bin/bash
# setup.sh - new_setup 최초 설정 도우미 (대화형, bash 4.1 호환)
#   new_setup.conf 를 처음 만들거나 값을 바꿀 때 쓴다. 질문에 답하면 설정 파일을 만들어 준다.
#
# 사용법:  bash setup.sh [-c <설정파일>] [--yes] [--help]
#   -c <설정파일>  쓸 설정 파일. 생략하면 setup.sh 옆의 new_setup.conf (new_setup 이 자동으로 찾는 위치)
#   --yes          확인 질문을 생략(시험·자동화용): 저장 확인=예, 경고 후 다시 입력=아니오(입력값 유지),
#                  stress-test 설정 이어하기=아니오
# 종료 코드: 0 완료/변경 없음, 1 사용자 중단, 2 인자·파일 오류
#
# ※ 관리서버는 여러 사용자가 같이 쓰므로 /etc 등 공용 경로에는 아무것도 쓰지 않는다.
#    설정 파일은 항상 임시 파일에 만든 뒤 mv 로 바꾸고, 이전 파일은 <설정파일>.bak 로 남긴다.

# 심볼릭 링크를 따라 실제 폴더를 찾는다
SELF=$0
while [ -h "$SELF" ]; do
	LINK=$(readlink "$SELF")
	case $LINK in
		/*) SELF=$LINK ;;
		*) SELF=$(dirname "$SELF")/$LINK ;;
	esac
done
SDIR=$(cd "$(dirname "$SELF")" && pwd -P)
SAMPLE=$SDIR/new_setup.conf.sample
CONF=$SDIR/new_setup.conf
YES=0

usage() {
	cat <<'EOF'
new_setup 최초 설정 도우미

사용법:  bash setup.sh [-c <설정파일>] [--yes] [--help]
  -c <설정파일>  쓸 설정 파일 (기본: setup.sh 옆의 new_setup.conf)
  --yes          확인 질문 생략(저장 확인=예, 경고 후 다시 입력=아니오, stress-test 설정 이어하기=아니오)
  --help         이 도움말

설정 파일이 없으면 new_setup.conf.sample 을 복사해서 시작하고(주석 유지),
있으면 현재 값을 기본값으로 보여 줍니다.
  Enter = 현재 값 유지,  - = 값 비우기

종료 코드: 0 완료/변경 없음, 1 사용자 중단, 2 인자·파일 오류
EOF
}

die2() {
	echo "setup.sh: $*" >&2
	exit 2
}

while [ $# -gt 0 ]; do
	case $1 in
		-c)
			[ $# -ge 2 ] || die2 "-c 뒤에 설정 파일 경로가 필요합니다"
			CONF=$2
			shift 2
			;;
		--yes | -y)
			YES=1
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			echo "setup.sh: 알 수 없는 인자: $1" >&2
			usage >&2
			exit 2
			;;
	esac
done

[ -d "$CONF" ] && die2 "설정 파일 경로가 폴더입니다: $CONF"
CONFDIR=$(dirname "$CONF")
[ -d "$CONFDIR" ] || die2 "설정 파일을 둘 폴더가 없습니다: $CONFDIR"

# 시작점: 있으면 현재 설정, 없으면 샘플
if [ -f "$CONF" ]; then
	BASE=$CONF
	FRESH=0
	[ -r "$CONF" ] || die2 "설정 파일을 읽을 수 없습니다: $CONF"
else
	[ -r "$SAMPLE" ] || die2 "샘플 설정 파일이 없습니다: $SAMPLE"
	BASE=$SAMPLE
	FRESH=1
fi

TMP=
TMP2=
cleanup() {
	[ -n "$TMP" ] && rm -f "$TMP"
	[ -n "$TMP2" ] && rm -f "$TMP2"
}
trap cleanup EXIT
trap 'echo; echo "중단했습니다. 설정 파일은 바뀌지 않았습니다."; exit 1' INT TERM

# ---------------------------------------------------------------------------
# 설정 파일 읽기/쓰기 (new_setup 의 parseConf 와 같은 규칙: '#' 주석, 첫 '=' 뒤 전체, 앞뒤 공백 제거)
# ---------------------------------------------------------------------------

# get_val <파일> <키>: 마지막으로 나온(유효한) 줄의 값. 없으면 빈 값
get_val() {
	awk -v k="$2" '
	{
		line = $0
		sub(/^[ \t]+/, "", line)
		if (line ~ /^#/) next
		i = index(line, "=")
		if (i < 1) next
		key = substr(line, 1, i - 1)
		gsub(/[ \t]+$/, "", key)
		if (key == k) {
			v = substr(line, i + 1)
			sub(/^[ \t]+/, "", v)
			sub(/[ \t\r]+$/, "", v)
			val = v
		}
	}
	END { print val }' "$1"
}

# set_val <입력> <출력> <키> <값>: 유효한 첫 KEY= 줄을 바꾸고(중복 줄은 제거),
# 없으면 '#KEY=' 샘플 줄을 풀어 값을 넣고, 그것도 없으면 끝에 덧붙인다. 나머지 줄·주석은 그대로.
set_val() {
	NS_K=$3 NS_V=$4 awk '
	BEGIN { k = ENVIRON["NS_K"]; v = ENVIRON["NS_V"] }
	function keyof(line,   t, i) {
		t = line
		sub(/^[ \t]+/, "", t)
		if (t ~ /^#/) return ""
		i = index(t, "=")
		if (i < 1) return ""
		t = substr(t, 1, i - 1)
		gsub(/[ \t]+$/, "", t)
		return t
	}
	function ckeyof(line,   t, i) {
		if (line !~ /^#[A-Z_]+[ \t]*=/) return ""
		t = substr(line, 2)
		i = index(t, "=")
		t = substr(t, 1, i - 1)
		gsub(/[ \t]+$/, "", t)
		return t
	}
	NR == FNR { if (keyof($0) == k) active = 1; next }
	{
		if (keyof($0) == k) {
			if (!done) { print k "=" v; done = 1 }
			next
		}
		if (!active && !done && ckeyof($0) == k) { print k "=" v; done = 1; next }
		print
	}
	END { if (!done) print k "=" v }' "$1" "$1" >"$2"
}

# ---------------------------------------------------------------------------
# 값 검증 (경고만 한다. 경고 문구를 한 줄씩 출력, 문제 없으면 아무것도 안 낸다)
# ---------------------------------------------------------------------------

unquote() {
	local v=$1
	case $v in
		\"*\")
			v=${v#\"}
			v=${v%\"}
			;;
		\'*\')
			v=${v#\'}
			v=${v%\'}
			;;
	esac
	printf '%s' "$v"
}

# 앞의 ~ 를 홈 폴더로 바꾼다(new_setup 도 같이 처리한다)
expand_path() {
	local v
	v=$(unquote "$1")
	case $v in
		"~") printf '%s' "$HOME" ;;
		\~/*) printf '%s' "$HOME/${v#\~/}" ;;
		*) printf '%s' "$v" ;;
	esac
}

v_gossh() {
	local p
	p=$(expand_path "$1")
	if [ -z "$p" ]; then
		command -v gossh >/dev/null 2>&1 || echo "비어 있는데 PATH 에서 gossh 를 찾을 수 없습니다. 경로를 지정하세요."
		return 0
	fi
	if [ -d "$p" ]; then
		p=$p/gossh
	fi
	if [ ! -e "$p" ]; then
		echo "gossh 를 찾을 수 없습니다: $p"
	elif [ ! -f "$p" ]; then
		echo "일반 파일이 아닙니다: $p"
	elif [ ! -x "$p" ]; then
		echo "실행 권한이 없습니다: $p"
	fi
	return 0
}

v_oscheck() {
	local p
	[ -z "$1" ] && return 0
	p=$(expand_path "$1")
	if [ ! -f "$p" ]; then
		echo "config_check.sh 파일이 없습니다: $p"
		return 0
	fi
	if [ ! -r "$p" ]; then
		echo "읽을 수 없는 파일입니다: $p"
		return 0
	fi
	grep -q -e '-auto-check' "$p" || echo "'-auto-check' 문자열이 없습니다(구버전이면 설정체크가 '-' 로 나옵니다): $p"
	grep -Eq -e '-auto([^-[:alnum:]_]|$)' "$p" || echo "'-auto' 처리 구문을 찾지 못했습니다(-run 의 설정수정에 필요): $p"
	return 0
}

v_exclude() {
	case $1 in
		"" | -) return 0 ;;
	esac
	case $1 in
		\|* | *\| | *\|\|*) echo "'|' 앞뒤나 연속된 '||' 에 빈 항목이 있습니다(빈 문자열은 모든 hostname 에 일치할 수 있음)." ;;
	esac
	return 0
}

v_stress() {
	local p
	[ -z "$1" ] && return 0
	p=$(expand_path "$1")
	if [ ! -e "$p" ]; then
		echo "stress-test 파일이 없습니다: $p"
	elif [ ! -f "$p" ]; then
		echo "일반 파일이 아닙니다(바이너리 파일 경로여야 합니다): $p"
	elif [ ! -x "$p" ]; then
		echo "실행 권한이 없습니다: $p"
	fi
	return 0
}

v_stressconf() {
	local p
	[ -z "$1" ] && return 0
	p=$(expand_path "$1")
	[ -f "$p" ] || echo "설정 파일이 없습니다: $p"
	return 0
}

v_route() {
	local p
	[ -z "$1" ] && return 0
	p=$(expand_path "$1")
	if [ ! -d "$p" ]; then
		echo "폴더가 없습니다: $p"
		return 0
	fi
	[ -f "$p/info_mn.sh" ] || echo "폴더에 info_mn.sh 가 없습니다: $p"
	[ -f "$p/info.sh" ] || echo "폴더에 info.sh 가 없습니다: $p"
	return 0
}

# CPU 로그는 대상 장비 안의 경로라 이 서버에서 존재를 확인할 수 없다. 형식만 본다.
v_cpulog() {
	local p dir
	[ -z "$1" ] && return 0
	p=$(unquote "$1")
	case $p in
		/* | \~/*) ;;
		*) echo "절대 경로가 아닙니다(대상 장비에서의 경로여야 합니다): $p" ;;
	esac
	dir=${p%/*}
	case $dir in
		*[\*\?]* | *[[:space:]]*) echo "폴더 부분에는 와일드카드(* ?)·공백을 쓸 수 없습니다(파일 이름에만 가능): $dir" ;;
	esac
	return 0
}

v_none() {
	return 0
}

v_cpuok() {
	[ -z "$1" ] && echo "비워 두면 그 벤더의 CPU 테스트 검사가 꺼집니다."
	return 0
}

# 시간: Go duration(47m, 30s, 1h30m) 또는 숫자만(분), 0 초과
v_duration() {
	local v
	[ -z "$1" ] && return 0
	v=$(unquote "$1")
	if printf '%s' "$v" | grep -Eq '^([0-9]+(\.[0-9]*)?|\.[0-9]+)$'; then
		awk -v n="$v" 'BEGIN { exit !(n + 0 > 0) }' || echo "0 보다 커야 합니다: $v"
		return 0
	fi
	if printf '%s' "$v" | grep -Eq '^([0-9]+(\.[0-9]*)?(ns|us|µs|ms|s|m|h))+$'; then
		printf '%s' "$v" | grep -Eq '^(0+(\.0*)?(ns|us|µs|ms|s|m|h))+$' && echo "0 보다 커야 합니다: $v"
		return 0
	fi
	echo "시간으로 읽을 수 없습니다(예: 47m, 30s, 1h30m, 숫자만 쓰면 분): $v"
	return 0
}

# ---------------------------------------------------------------------------
# 입출력 도우미
# ---------------------------------------------------------------------------

EOFSEEN=0
ANS=
# readline <프롬프트>: 한 줄 읽어 ANS 에 넣는다(앞뒤 공백 제거). 입력이 끝났으면 빈 값(=Enter)
readline() {
	printf '%s' "$1"
	if [ "$EOFSEEN" -eq 1 ] || ! IFS= read -r ANS; then
		EOFSEEN=1
		ANS=
		echo
	fi
	ANS=${ANS%$'\r'}
	ANS=${ANS#"${ANS%%[![:space:]]*}"}
	ANS=${ANS%"${ANS##*[![:space:]]}"}
}

# yesno <질문> <기본 Y|N>: 예면 0
yesno() {
	local def=$2
	while :; do
		readline "$1"
		case $ANS in
			"") [ "$def" = Y ] && return 0 || return 1 ;;
			[Yy] | [Yy][Ee][Ss]) return 0 ;;
			[Nn] | [Nn][Oo]) return 1 ;;
		esac
		echo "  y 또는 n 으로 답하세요."
		[ "$EOFSEEN" -eq 1 ] && { [ "$def" = Y ] && return 0 || return 1; }
	done
}

declare -A OLD NEW
KEYS=()

# ask <키> <검증함수> <설명> [기본값 안내]
ask() {
	local key=$1 vfn=$2 desc=$3 defhint=${4:-} shown warn prev
	while :; do
		if [ -n "${NEW[$key]}" ]; then
			shown=${NEW[$key]}
		else
			shown="(비어 있음${defhint:+, 기본 $defhint})"
		fi
		echo
		echo "[$key] $desc"
		readline "  현재: $shown   (Enter=유지, -=비움)
  > "
		prev=${NEW[$key]}
		case $ANS in
			"")
				warn=$($vfn "${NEW[$key]}")
				[ -n "$warn" ] && printf '  경고: %s\n' "$warn"
				return 0
				;;
			-) NEW[$key]= ;;
			*) NEW[$key]=$ANS ;;
		esac
		warn=$($vfn "${NEW[$key]}")
		[ -z "$warn" ] && return 0
		printf '  경고: %s\n' "$warn"
		if [ "$YES" -eq 1 ] || [ "$EOFSEEN" -eq 1 ]; then
			echo "  경고가 있지만 입력한 값을 그대로 씁니다."
			return 0
		fi
		if yesno "  다시 입력할까요? (Y/n; n=입력한 값 그대로 사용) " Y; then
			NEW[$key]=$prev
		else
			return 0
		fi
	done
}

disp() {
	if [ -n "$1" ]; then printf '%s' "$1"; else printf '(비어 있음)'; fi
}

# ---------------------------------------------------------------------------
# 시작
# ---------------------------------------------------------------------------

ALLKEYS="GOSSH OS_CHECK_SH OS_CHECK_EXCLUDE STRESS_TEST STRESS_TEST_CONF USER_ROUTE
CPU_INTEL_LOG CPU_INTEL_OK CPU_INTEL_LINE CPU_AMD_LOG CPU_AMD_OK CPU_AMD_LINE
CPU_WAIT_START CPU_WAIT_INTERVAL CPU_WAIT_MAX GOSSH_TIMEOUT CHECK_TIMEOUT STRESS_TIMEOUT"
for k in $ALLKEYS; do
	KEYS+=("$k")
	OLD[$k]=$(get_val "$BASE" "$k")
	NEW[$k]=${OLD[$k]}
done
# 배포 브랜치(setup-check)에는 stress-test/ 가 함께 들어 있다 — 비어 있으면 그 경로를 기본값으로
if [ -z "${NEW[STRESS_TEST]}" ] && [ -f "$SDIR/stress-test/stress-test" ]; then
	NEW[STRESS_TEST]="$SDIR/stress-test/stress-test"
fi

echo "=== new_setup 설정 도우미 ==="
if [ "$FRESH" -eq 1 ]; then
	echo "설정 파일이 없어 샘플(new_setup.conf.sample)에서 시작합니다."
else
	echo "현재 설정 파일의 값을 기본값으로 보여 줍니다."
fi
echo "설정 파일: $CONF"
echo "각 질문에서 Enter=현재 값 유지, -=값 비우기. 중간에 Ctrl+C 로 끝내면 아무것도 저장되지 않습니다."

echo
echo "--- 1. 기본 설정 (장비 점검 / 설정체크) ---"
ask GOSSH v_gossh "gossh 실행 파일 경로(또는 gossh 가 든 폴더). 비우면 PATH 에서 찾습니다."
ask OS_CHECK_SH v_oscheck "관리서버에 있는 config_check.sh 경로. 비우면 설정체크 열이 '-' 입니다. -auto-check 지원 버전이어야 합니다."
ask OS_CHECK_EXCLUDE v_exclude "설정체크에서 제외할 hostname 문자열('|' 로 구분, 예: asdf|qwer). 포함되면 FAIL 이어도 'O' 로 봅니다."

echo
echo "--- 2. -run (설정수정·CPU·stress-ng 자동 진행) 설정 ---"
ask STRESS_TEST v_stress "stress-test 바이너리 경로. 비우면 부하 단계에서 오류 안내가 나옵니다."
ask STRESS_TEST_CONF v_stressconf "stress-test 의 -conf 로 넘길 설정 파일. 비우면 stress-test 기본 탐색."
ask USER_ROUTE v_route "info_mn.sh / info.sh 가 있는 폴더(사용자 선택 메뉴). 비우면 -user 또는 \$USER 를 씁니다."

echo
if yesno "고급 설정(CPU 판정 문구, 대기 시간, 시간 제한)을 바꿀까요? (y/N) " N; then
	echo
	echo "--- 3. CPU 테스트 판정 (로그 경로는 대상 장비 안의 경로) ---"
	echo "로그에 성공 문구가 있으면 CPU테스트 'O'. 파일 이름에 * ? 를 쓰면 가장 최근 파일을 씁니다."
	ask CPU_INTEL_LOG v_cpulog "Intel CPU 진단 로그 경로."
	ask CPU_INTEL_OK v_cpuok "Intel 성공 문구(고정 문자열, 대소문자 무시)."
	ask CPU_INTEL_LINE v_none "(선택) 지정하면 이 문자열이 든 줄만 검사하고 그 줄이 모두 성공 문구를 포함해야 'O'. 비우면 로그 어디에든 문구가 있으면 'O'."
	ask CPU_AMD_LOG v_cpulog "AMD CPU 진단 결과 파일 경로."
	ask CPU_AMD_OK v_cpuok "AMD 성공 문구(예: PASS)."
	ask CPU_AMD_LINE v_none "AMD 에서 검사할 줄 표식(예: Result). 그 줄이 1개 이상이고 모두 성공 문구를 포함하면 'O'."

	echo
	echo "--- 4. -run 의 CPU 테스트 대기 시간 (47m, 30s, 1h30m 또는 숫자만=분) ---"
	ask CPU_WAIT_START v_duration "CPU 테스트 launch 후 완료 확인을 시작할 때까지의 대기." 47m
	ask CPU_WAIT_INTERVAL v_duration "완료 확인·꺼짐 감시 간격." 2m
	ask CPU_WAIT_MAX v_duration "확인 시작 후 최대 대기." 30m

	echo
	echo "--- 5. 외부 명령 시간 제한 (넘으면 중단하고 '시간초과' 처리) ---"
	ask GOSSH_TIMEOUT v_duration "gossh 한 번의 시간 제한." 10m
	ask CHECK_TIMEOUT v_duration "config_check.sh 한 번의 시간 제한." 40m
	ask STRESS_TIMEOUT v_duration "stress-test 한 번의 시간 제한." 60m
fi

# ---------------------------------------------------------------------------
# 변경 요약 → 확인 → 저장
# ---------------------------------------------------------------------------

CHANGED=()
for k in "${KEYS[@]}"; do
	[ "${OLD[$k]}" != "${NEW[$k]}" ] && CHANGED+=("$k")
done

echo
echo "=== 변경 요약 ==="
if [ "${#CHANGED[@]}" -eq 0 ]; then
	echo "변경 없음."
else
	for k in "${CHANGED[@]}"; do
		printf '  %s: %s → %s\n' "$k" "$(disp "${OLD[$k]}")" "$(disp "${NEW[$k]}")"
	done
fi

WRITE=0
if [ "${#CHANGED[@]}" -gt 0 ] || [ "$FRESH" -eq 1 ]; then
	WRITE=1
fi

if [ "$WRITE" -eq 1 ]; then
	if [ "$FRESH" -eq 1 ]; then
		echo "새 설정 파일을 만듭니다: $CONF"
	else
		echo "저장하면 이전 파일은 $CONF.bak 로 남습니다."
	fi
	if [ "$YES" -ne 1 ]; then
		if ! yesno "이대로 저장할까요? (Y/n) " Y; then
			echo "저장하지 않고 종료합니다."
			exit 1
		fi
	fi

	TMP=$(mktemp "$CONF.XXXXXX") || die2 "임시 파일을 만들 수 없습니다(폴더 쓰기 권한 확인): $CONFDIR"
	TMP2=$TMP.2
	cp "$BASE" "$TMP" || die2 "임시 파일에 복사하지 못했습니다"
	for k in "${CHANGED[@]}"; do
		set_val "$TMP" "$TMP2" "$k" "${NEW[$k]}" || die2 "설정 값을 쓰지 못했습니다: $k"
		mv -f "$TMP2" "$TMP" || die2 "임시 파일을 갱신하지 못했습니다"
	done
	if [ "$FRESH" -eq 0 ]; then
		cp -p "$CONF" "$CONF.bak" || die2 "백업을 만들지 못했습니다: $CONF.bak"
		chmod --reference="$CONF" "$TMP" 2>/dev/null || true
	else
		chmod "$(printf '%o' $((0666 & ~$(umask))))" "$TMP" 2>/dev/null || true
	fi
	mv -f "$TMP" "$CONF" || die2 "설정 파일을 저장하지 못했습니다: $CONF"
	TMP=
	echo "저장했습니다: $CONF"
	[ "$FRESH" -eq 0 ] && echo "이전 설정: $CONF.bak"
fi

# ---------------------------------------------------------------------------
# stress-test 자체 설정 이어하기
# ---------------------------------------------------------------------------

ST=$(expand_path "${NEW[STRESS_TEST]}")
# STRESS_REMOTE(B서버 원격 실행)면 STRESS_TEST 는 B 의 경로이므로 이 서버에서 확인·설정하지 않는다
REMOTE=""
[ -f "$CONF" ] && REMOTE=$(get_val "$CONF" STRESS_REMOTE)
if [ -n "$REMOTE" ]; then
	echo
	echo "STRESS_REMOTE=$REMOTE → stress-test 는 B서버에서 실행합니다. stress-test 설정은 B서버의 그 도구 폴더에서 하세요: $(dirname "${NEW[STRESS_TEST]}")"
elif [ -z "$ST" ]; then
	echo
	echo "STRESS_TEST 가 비어 있어 stress-test 설정은 건너뜁니다(-run 의 부하 단계에서 필요하면 나중에 지정)."
else
	STSETUP=$(dirname "$ST")/setup.sh
	echo
	if [ -f "$STSETUP" ]; then
		if [ "$YES" -eq 1 ]; then
			echo "stress-test 설정은 --yes 에서는 건너뜁니다: bash $STSETUP --user <사용자>"
		elif yesno "stress-test 설정도 이어서 할까요? (Y/n) " Y; then
			DEFUSER=${USER:-$(id -un 2>/dev/null)}
			readline "  사용자 이름 [기본: $DEFUSER]: "
			U=${ANS:-$DEFUSER}
			# STRESS_TEST_CONF 를 지정했으면 그 파일을, 아니면 stress-test 기본(<폴더>/conf/<user>_setting.conf)을 채운다
			STCONF=$(expand_path "${NEW[STRESS_TEST_CONF]}")
			STARGS=(--user "$U")
			[ -n "$STCONF" ] && STARGS+=(--conf "$STCONF")
			if ! bash "$STSETUP" "${STARGS[@]}"; then
				echo "경고: stress-test 설정이 정상 종료하지 않았습니다. 그 폴더에서 다시 실행해 보세요: $STSETUP"
			fi
		fi
	else
		echo "stress-test 설정은 그 도구 폴더에서 따로 하세요(setup.sh 가 없습니다): $(dirname "$ST")"
	fi
fi

echo
echo "완료. 다음부터는 new_setup.sh <호스트파일> 로 실행하면 이 설정을 자동으로 읽습니다."
exit 0
