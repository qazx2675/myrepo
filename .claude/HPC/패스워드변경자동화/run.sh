#!/usr/bin/env bash
# run.sh — pwreset 빌드 + 실행 래퍼.
#
# 사용법:
#   ./run.sh                                             # 기본 파일명으로 실행
#   ./run.sh -list list.txt -oldpw old_password.txt \
#            -newpw newpass.enc -key key.bin -out result.csv
#   ./run.sh -encrypt -in plain.txt -out newpass.enc -key key.bin
#
# 인자를 그대로 pwreset 바이너리에 넘깁니다. 실행 전에 go build 로
# 바이너리를 (재)빌드하고, 서브커맨드에 필요한 입력 파일이 있는지 확인합니다.
set -euo pipefail
cd "$(dirname "$0")"

usage() {
  cat <<'EOF'
사용법:
  ./run.sh [-list <파일>] [-oldpw <파일>] [-newpw <파일>] [-key <파일>] [-out <파일>]
  ./run.sh -encrypt -in <평문파일> [-out <파일>] [-key <파일>]

기본값 (인자 생략 시):
  -list  list.txt
  -oldpw old_password.txt
  -newpw newpass.enc
  -key   key.bin
  -out   result.csv

먼저 실행할 것:
  cp list.txt.example         list.txt
  cp old_password.txt.example old_password.txt
  ./run.sh -encrypt -in <새_비밀번호_평문파일> -out newpass.enc -key key.bin
  chmod +x run.sh   # 권한 오류 시

이 스크립트를 처음 받았다면 실행 권한이 없을 수 있습니다: chmod +x run.sh
EOF
}

if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. Go 툴체인을 먼저 설치하십시오." >&2
  exit 2
fi

for a in "$@"; do
  if [ "$a" = "-h" ] || [ "$a" = "--help" ]; then
    usage
    exit 0
  fi
done

echo "빌드: pwreset ..."
go build -o bin/pwreset ./cmd/pwreset
echo "-> bin/pwreset"

# -encrypt 모드인지 확인 (인자 검증 분기용).
is_encrypt=0
for a in "$@"; do
  if [ "$a" = "-encrypt" ]; then
    is_encrypt=1
  fi
done

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

if [ "$is_encrypt" = "1" ]; then
  in_path="$(get_opt -in "$@")"
  if [ -z "${in_path:-}" ]; then
    echo "오류: -encrypt 에는 -in <평문파일> 이 필요합니다." >&2
    usage
    exit 1
  fi
  if [ ! -f "$in_path" ]; then
    echo "오류: 평문 파일이 없습니다: $in_path" >&2
    exit 1
  fi
else
  list_path="$(get_opt -list "$@")"
  list_path="${list_path:-list.txt}"
  oldpw_path="$(get_opt -oldpw "$@")"
  oldpw_path="${oldpw_path:-old_password.txt}"
  newpw_path="$(get_opt -newpw "$@")"
  newpw_path="${newpw_path:-newpass.enc}"
  key_path="$(get_opt -key "$@")"
  key_path="${key_path:-key.bin}"

  missing=0
  for f in "$list_path" "$oldpw_path" "$newpw_path" "$key_path"; do
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
