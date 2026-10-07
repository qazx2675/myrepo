#!/usr/bin/env bash
# encrypt.sh — BMC 비밀번호를 AES-256-GCM 으로 암호화해 pass.enc + key.bin 을 만든다.
#
# 사용법: bash encrypt.sh
#   비밀번호를 화면에 표시하지 않고 두 번 입력받아, 같을 때만 저장합니다.
#   비밀번호는 명령행 인자로 전달하지 않고 파이프(표준입력)로만 넘기므로 ps 에 보이지 않습니다.
#   저장 위치는 bios.conf 의 pass_file/key_file (없으면 pass.enc / key.bin).
set -euo pipefail
cd "$(dirname "$0")"
. ./lib.sh

select_bin || exit 2

out="$(conf_val bios.conf pass_file pass.enc)"
key="$(conf_val bios.conf key_file key.bin)"

printf 'BMC 비밀번호: ' >&2
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
