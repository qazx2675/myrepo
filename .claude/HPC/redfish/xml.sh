#!/usr/bin/env bash
# xml.sh — 사전조사(덤프) 실행 래퍼. 이름은 요청에 따라 유지하며 산출물은 JSON 입니다.
#
# 사용법:
#   bash xml.sh <관리망 주소 또는 hostname-m> [...]    # 대상을 직접 지정
#   bash xml.sh 192.0.2.10 host0002-m 192.0.2.11:8443  # ip:포트 도 가능
#   bash xml.sh                                        # 대상 없으면 user.txt 의 전체 대상
#   bash xml.sh --compact 192.0.2.10                   # 호스트당 1줄 요약
#
# 인자를 모아 bin/biostool dump -targets <대상,대상,...> 로 전달합니다 (GET 전용, 호스트당 로그인 1회).
# 접속 계정/비밀번호는 bios.conf, pass.enc, key.bin 을 사용합니다 (준비는 bios_check.sh -h 참고).
# 폐쇄망이라 파일을 반출할 수 없으므로 터미널에 나오는 짧은 요약을 사람이 읽어 전달합니다.
set -euo pipefail
cd "$(dirname "$0")"
. ./lib.sh

usage() {
  cat <<'EOF'
사용법: bash xml.sh [--compact] [<관리망 주소 또는 hostname-m> ...]
  예) bash xml.sh 192.0.2.10 host0002-m
      bash xml.sh               (대상 생략 시 user.txt 사용)
  Redfish 응답을 GET 으로만 조회해 dumps/ 에 JSON 으로 저장하고 짧은 요약을 출력합니다.
  요약에는 비밀번호·토큰·시리얼이 없습니다. 요약을 복사해 전달하십시오.
EOF
}

compact=""
targets=""
for a in "$@"; do
  case "$a" in
    -h | --help)
      usage
      exit 0
      ;;
    -compact | --compact)
      compact="-compact"
      ;;
    -*)
      echo "오류: 알 수 없는 옵션: $a" >&2
      usage >&2
      exit 1
      ;;
    *)
      targets="${targets:+$targets,}$a"
      ;;
  esac
done

select_bin || exit 2

missing=0
for f in bios.conf "$(conf_val bios.conf pass_file pass.enc)" "$(conf_val bios.conf key_file key.bin)"; do
  if [ ! -f "$f" ]; then
    echo "[없음] $f  (bios.conf: cp bios.conf.example bios.conf / pass.enc,key.bin: bash encrypt.sh)" >&2
    missing=1
  fi
done
if [ -z "$targets" ] && [ ! -f user.txt ]; then
  echo "[없음] user.txt  (대상을 인자로 주거나: cp user.txt.example user.txt)" >&2
  missing=1
fi
if [ "$missing" = "1" ]; then
  exit 1
fi

out="$(conf_val bios.conf dump_dir dumps)"

# 실패 호스트가 있어도 안내는 반드시 보여 주기 위해 exec 하지 않고 종료코드를 보관한다.
rc=0
if [ -n "$targets" ]; then
  "$BIN" dump -targets "$targets" $compact || rc=$?
else
  "$BIN" dump $compact || rc=$?
fi

echo
echo "저장 위치: $out/<vendor>/<model>/<biosver>/<host>/redfish/v1/...  (목록: $out/index.tsv)"
echo "위 요약을 복사해 전달해 주십시오 (파일은 반출하지 않습니다)."
exit "$rc"
