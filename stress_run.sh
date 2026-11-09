#!/bin/bash
# 부하테스트 자동화 래퍼 (stress-test)
# 사용법: bash stress_run.sh user.txt [-retry N] [-er 파일] [-conf 파일] [-ignore-offline]
#   user 선택(USER_ROUTE) → 단계 선택(a=cpu, b=stress) → ./stress-test 실행

# ==== 사용자 설정 ====
GOSSH=""        # gossh 경로 (폴더 또는 파일). 비우면 설정 파일 gossh 키 → PATH 순
USER_ROUTE=""   # user 선택 스크립트 폴더 (info_mn.sh, info.sh). 비우면 STRESS_USER → $USER
# =====================

export GOSSH
sd=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

if [[ -z $1 || ! -f $1 ]]; then
	echo "사용법: bash $(basename "$0") <입력파일> [-retry N] [-er 파일] [-conf 파일]" >&2
	[[ -n $1 ]] && echo "오류: 입력 파일이 없습니다: $1" >&2
	exit 2
fi

# ==== user 선택 (AWX 01 스크립트와 같은 방식) ====
user=""
if [[ -n $USER_ROUTE ]]; then
	bash "$USER_ROUTE/info_mn.sh"
	read -p "Input Number: " user_choice
	user=$(bash "$USER_ROUTE/info.sh" "$user_choice")
	[[ -z $user ]] && { echo "오류: user 값이 없습니다 (USER_ROUTE 스크립트 확인)" >&2; exit 2; }
fi

# ==== 단계 선택 ====
while true; do
	echo "a) cpu"
	echo "b) stress"
	read -p '어떤 부하테스트를 실행할지 고르세요: ' steps || [[ -n $steps ]] || { echo "오류: 입력이 없습니다 (비대화형은 stress-test -batch 를 직접 실행)" >&2; exit 2; }
	steps=${steps// /}
	# a/b 만, 같은 글자 중복 없이 (a, b, ab, ba)
	if [[ $steps =~ ^[ab]+$ && ${#steps} -le 2 && $steps != aa && $steps != bb ]]; then
		break
	fi
	echo "a, b, ab, ba 중에서 입력하세요."
done

# ==== 바이너리 없으면 빌드 ====
if [[ ! -x $sd/stress-test ]]; then
	echo "[i] stress-test 바이너리가 없어 빌드합니다."
	bash "$sd/build.sh" || { echo "오류: 빌드 실패" >&2; exit 2; }
fi

args=()
[[ -n $user ]] && args+=(-user "$user")
"$sd/stress-test" "${args[@]}" -hosts "$1" -steps "$steps" "${@:2}"
