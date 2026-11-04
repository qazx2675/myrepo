#!/bin/bash
# fake_config_check.sh - new_setup 시험용 가짜 config_check.sh
# 사용: bash fake_config_check.sh -auto-check <user> <목록파일>   (이 문자열 '-auto-check' 가 파일 안에
#       있어야 new_setup 이 비대화형 지원 스크립트로 본다. 시험에서 이 문자열을 지운 복사본을 '미지원' 으로 쓴다)
#   FAKE_CC_SCENARIO=<파일>  각 줄 "host<TAB>pass|fail|none" (없는 호스트는 pass)
#   FAKE_CC_LOG=<파일>       (선택) argv·작업 폴더·TMPDIR·목록·gossh 위치를 이어 붙여 기록
# 결과: 현재 폴더의 check.res_<user> 에 pass -> "host: OK ldap ok",
#       fail -> "host: OK x" + "host: FAIL selinux enforcing", none -> 줄 없음
set -u

if [ "${1:-}" != "-auto-check" ] || [ $# -lt 3 ]; then
	echo "사용: $0 -auto-check <user> <목록파일>" >&2
	exit 1
fi
user=$2
list=$3
[ -f "$list" ] || { echo "목록 파일 없음: $list" >&2; exit 1; }

# 진짜처럼 화면에 잡음 출력
echo "config_check (fake) 시작"
echo "  user=$user list=$list"

hosts=()
while IFS= read -r line || [ -n "$line" ]; do
	h=$(printf '%s' "$line" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
	case $h in '' | '#'*) continue ;; esac
	hosts+=("$h")
done < "$list"

if [ -n "${FAKE_CC_LOG:-}" ]; then
	{
		echo "=== CC argv=$*"
		echo "PWD=$(pwd -P)"
		echo "TMPDIR=${TMPDIR:-}"
		echo "GOSSH=$(command -v gossh 2> /dev/null || echo NONE)"
		echo "LIST=${hosts[*]-}"
	} >> "$FAKE_CC_LOG"
fi

out=check.res_$user
: > "$out"
for h in ${hosts[@]+"${hosts[@]}"}; do
	r=""
	if [ -n "${FAKE_CC_SCENARIO:-}" ] && [ -f "$FAKE_CC_SCENARIO" ]; then
		r=$(awk -F'\t' -v h="$h" '$1==h {print $2; exit}' "$FAKE_CC_SCENARIO")
	fi
	case ${r:-pass} in
		pass) echo "$h: OK ldap ok" >> "$out" ;;
		fail) { echo "$h: OK x"; echo "$h: FAIL selinux enforcing"; } >> "$out" ;;
		none) ;;
	esac
	echo "checked $h"
done
echo "config_check (fake) 끝"
exit 0
