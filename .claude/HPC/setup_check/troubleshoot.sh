#!/bin/bash
# troubleshoot.sh - new_setup 읽기 전용 문제 진단(-doctor) 래퍼 (bash 4.1 / RHEL6 호환)
#   new_setup 바이너리를 new_setup.sh 와 같은 방식으로 골라 `-doctor` 로 실행한다. 설정·상태 파일·원격 호스트를 바꾸지 않는다.
#   troubleshoot.sh [-c <설정파일>] [-user <사용자>]                     환경 점검만
#   troubleshoot.sh [-c <설정파일>] <호스트파일>                          환경 점검 + 그 경로의 -run 작업 진단
#   troubleshoot.sh [-c <설정파일>] -host <호스트> [-check]              환경 점검 + 호스트 한 대 점검(-check: config_check -auto-check 도 실행)
#   보고서는 화면과 ./new_setup_doctor_<시각>.txt 에 저장된다. 종료코드: 0 문제 없음, 1 문제 있음, 2 인자 오류

# 심볼릭 링크를 따라 실제 폴더를 찾는다
SELF=$0
while [ -h "$SELF" ]; do
	LINK=$(readlink "$SELF")
	case $LINK in
		/*) SELF=$LINK ;;
		*) SELF=$(dirname "$SELF")/$LINK ;;
	esac
done
DIR=$(cd "$(dirname "$SELF")" && pwd -P)

BIN=$DIR/bin/new_setup
if [ -r /etc/redhat-release ] && grep -qE 'release 6(\.|[[:space:]]|$)' /etc/redhat-release; then
	BIN=$DIR/bin/new_setup_os6
fi

if [ ! -x "$BIN" ]; then
	echo "troubleshoot: 실행 파일이 없습니다: $BIN" >&2
	echo "  -> build.sh 로 빌드하거나, 저장소에서 bin/ 폴더를 다시 받으세요." >&2
	exit 2
fi

exec "$BIN" -doctor "$@"
