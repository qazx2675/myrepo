#!/usr/bin/env bash
# encrypt.sh — BMC 비밀번호를 AES-256-GCM 으로 암호화해 pass.enc + key.bin 을 만든다.
#
# 사용법: bash encrypt.sh      # 1순위 계정(bios.conf user) 비밀번호 -> pass.enc
#         bash encrypt.sh 2    # 두 번째 계정(bios.conf user2) 비밀번호 -> pass2.enc (계정 생성에서만 씀)
#   비밀번호를 화면에 표시하지 않고 두 번 입력받아, 같을 때만 저장합니다.
#   비밀번호는 명령행 인자로 전달하지 않고 파이프(표준입력)로만 넘기므로 ps 에 보이지 않습니다.
#   저장 위치는 bios.conf 의 pass_file(2 이면 pass2_file)/key_file (없으면 pass.enc / pass2.enc / key.bin).
#   key.bin 이 이미 있으면 그 키를 그대로 씁니다 (pass.enc 와 pass2.enc 는 같은 key.bin 으로 풉니다).
set -euo pipefail
cd "$(dirname "$0")"
. ./lib.sh

which=1
case "${1:-}" in
  "") ;;
  2 | -2 | user2) which=2 ;;
  -h | --help)
    sed -n '2,9p' "$0"
    exit 0
    ;;
  *)
    echo "오류: 알 수 없는 인자 $1 (bash encrypt.sh 또는 bash encrypt.sh 2)" >&2
    exit 1
    ;;
esac

select_bin || exit 2

if [ "$which" = "2" ]; then
  out="$(conf_val bios.conf pass2_file pass2.enc)"
  label="두 번째 계정(user2=$(conf_val bios.conf user2 ''))"
else
  out="$(conf_val bios.conf pass_file pass.enc)"
  label="BMC"
fi
key="$(conf_val bios.conf key_file key.bin)"

printf '%s 비밀번호: ' "$label" >&2
IFS= read -rs pw || true
echo >&2
printf '비밀번호 확인: ' >&2
IFS= read -rs pw2 || true
echo >&2

if [ -z "$pw" ]; then
  echo "오류: 비밀번호가 비어 있습니다." >&2
  exit 1
fi
if [ "$pw" != "$pw2" ]; then
  echo "오류: 두 번 입력한 비밀번호가 다릅니다." >&2
  exit 1
fi

# printf 는 bash 내장이라 ps 에 노출되지 않는다.
printf '%s' "$pw" | "$BIN" encrypt -key "$key" -out "$out"
unset pw pw2

for f in "$out" "$key"; do
  perm="$(stat -c '%a' "$f")"
  if [ "$perm" != "600" ]; then
    chmod 600 "$f"
    echo "권한 보정: $f ($perm -> 600)" >&2
  fi
done
echo "권한 확인: $out, $key = 600"
echo "주의: $key 와 $out 은 짝입니다. 둘 다 git 에 올리지 마십시오 (.gitignore 에 제외됨)."
