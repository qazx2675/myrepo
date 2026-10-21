#!/usr/bin/env bash
# run.sh — pwreset 실행 래퍼. 바이너리(bin/pwreset)는 저장소에 이미 빌드되어
# 커밋되어 있으므로 여기서는 빌드하지 않습니다.
#
# 사용법:
#   ./run.sh -pass <평문파일>                # 새 비밀번호 암호화 (key.bin 없으면 생성, newpass.enc 저장)
#   ./run.sh -list <파일> [-out <결과csv>]   # 비밀번호 변경 시작
#
# 인자를 그대로 bin/pwreset 바이너리에 넘깁니다.
set -euo pipefail
cd "$(dirname "$0")"

usage() {
  cat <<'EOF'
사용법:
  ./run.sh -pass <평문파일>                        # 새 비밀번호 암호화 (key.bin 없으면 생성, newpass.enc 저장)
  ./run.sh -list <파일> [-out <결과csv>] [-i <키>]  # 비밀번호 변경 시작 (키 인증 우선)

고정 경로 (플래그 없음):
  old_password.txt  과거 비밀번호 후보 목록 (최신이 맨 위, 강제변경의 Current password 응답에 사용)
  newpass.enc        암호화된 새 비밀번호 (-pass 로 생성됨)
  key.bin             AES-256 키 (-pass 실행 시 없으면 자동 생성)

인증: SSH 키를 먼저 쓰고(없으면 ~/.ssh 표준 키 자동 탐색), 후보 비밀번호는
폴백입니다. 대상이 PasswordAuthentication 을 꺼둔 서버라면 키 인증이 필요합니다.

먼저 실행할 것:
  cp list.txt.example         list.txt
  cp old_password.txt.example old_password.txt
  ./run.sh -pass <새_비밀번호_평문파일>
  chmod +x run.sh   # 권한 오류 시
EOF
}

for a in "$@"; do
  if [ "$a" = "-h" ] || [ "$a" = "--help" ]; then
    usage
    exit 0
  fi
done

if [ ! -x ./bin/pwreset ]; then
  echo "오류: bin/pwreset 바이너리가 없습니다(저장소에 커밋된 바이너리가 있는 체크아웃을 쓰십시오)." >&2
  exit 2
fi

# 값이 붙는 옵션에서 파일 경로를 뽑아내는 헬퍼.
get_opt() {
  local name="$1"
  shift
  local prev=""
  for a in "$@"; do
    if [ "$prev" = "$name" ]; then
      echo "$a"
      return 0
    fi
    prev="$a"
  done
}

pass_path="$(get_opt -pass "$@")"

if [ -n "${pass_path:-}" ]; then
  if [ ! -f "$pass_path" ]; then
    echo "오류: 평문 파일이 없습니다: $pass_path" >&2
    exit 1
  fi
else
  list_path="$(get_opt -list "$@")"
  list_path="${list_path:-list.txt}"

  missing=0
  for f in "$list_path" old_password.txt newpass.enc key.bin; do
    if [ ! -f "$f" ]; then
      echo "오류: 필요한 파일이 없습니다: $f" >&2
      missing=1
    fi
  done
  if [ "$missing" = "1" ]; then
    usage
    exit 1
  fi
fi

exec ./bin/pwreset "$@"
