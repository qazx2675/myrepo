#!/usr/bin/env bash
# encrypt.sh — BMC 비밀번호를 AES-256-GCM 으로 암호화해 pass.enc + key.bin 을 만든다.
#
# 사용법: bash encrypt.sh            # 1순위 계정(bios.conf user) 비밀번호 -> pass.enc
#         bash encrypt.sh 2          # 두 번째 계정(bios.conf user2) 비밀번호 -> pass2.enc (계정 생성에서만 씀)
#         bash encrypt.sh rf dell    # 2순위 계정(rf.conf dell_user) 비밀번호 -> rf.enc 의 dell 항목만 갱신
#         bash encrypt.sh rf         # rf.conf 에 <vendor>_user 가 있는 벤더를 차례로 물어 rf.enc 에 저장
#   벤더: dell hpe lenovo supermicro cisco cray. rf.conf 는 RF_TOOL_CONF 또는 /etc/rf_tool/rf.conf.
#   pass.enc/pass2.enc: 비밀번호를 화면에 표시하지 않고 두 번 입력받아, 같을 때만 저장합니다.
#   rf: 글자마다 * 로 표시하고 붙여넣기는 거부하며 두 번 입력이 같을 때만 저장합니다 (터미널에서 직접 입력).
#       한 벤더만 갱신해도 rf.enc 의 다른 벤더 항목은 그대로 남습니다.
#   비밀번호는 명령행 인자로 받거나 넘기지 않으므로 ps·쉘 히스토리에 남지 않습니다.
#   저장 위치는 bios.conf 의 pass_file / pass2_file / rf_enc_file / key_file (없으면 pass.enc / pass2.enc / rf.enc / key.bin).
#   key.bin 이 이미 있으면 그 키를 그대로 씁니다 (pass.enc·pass2.enc·rf.enc 는 같은 key.bin 으로 풉니다).
set -euo pipefail
cd "$(dirname "$0")"
. ./lib.sh

rf_vendors="dell hpe lenovo supermicro cisco cray"
which=1
rf_vendor=""
case "${1:-}" in
  "") ;;
  2 | -2 | user2) which=2 ;;
  rf)
    which=rf
    rf_vendor="$(printf '%s' "${2:-}" | tr '[:upper:]' '[:lower:]')"
    if [ -n "$rf_vendor" ] && ! printf ' %s ' "$rf_vendors" | grep -q " $rf_vendor "; then
      echo "오류: 알 수 없는 벤더 ${2} (다음 중 하나: $rf_vendors)" >&2
      exit 1
    fi
    if [ $# -gt 2 ]; then
      echo "오류: 벤더는 하나만 줄 수 있습니다 (bash encrypt.sh rf <벤더>)" >&2
      exit 1
    fi
    ;;
  -h | --help)
    sed -n '2,14p' "$0"
    exit 0
    ;;
  *)
    echo "오류: 알 수 없는 인자 $1 (bash encrypt.sh, bash encrypt.sh 2, bash encrypt.sh rf [벤더])" >&2
    exit 1
    ;;
esac

select_bin || exit 2

# fix_perm 은 파일 권한을 600 으로 맞춘다.
fix_perm() {
  local f perm
  for f in "$@"; do
    perm="$(stat -c '%a' "$f")"
    if [ "$perm" != "600" ]; then
      chmod 600 "$f"
      echo "권한 보정: $f ($perm -> 600)" >&2
    fi
  done
}

if [ "$which" = "rf" ]; then
  out="$(conf_val bios.conf rf_enc_file rf.enc)"
  key="$(conf_val bios.conf key_file key.bin)"
  rfconf="${RF_TOOL_CONF:-/etc/rf_tool/rf.conf}"
  if [ ! -t 0 ]; then
    echo "오류: 2순위 계정 비밀번호는 터미널에서 직접 입력해야 합니다 (파이프·파일 입력 불가)" >&2
    exit 1
  fi
  # rf_user_of 는 rf.conf 의 <vendor>_user 값을 돌려준다 (대소문자 무시, 마지막 값, 없으면 빈 값).
  rf_user_of() {
    [ -f "$rfconf" ] || return 0
    sed -n "s/^[[:space:]]*$1_user[[:space:]]*=[[:space:]]*//Ip" "$rfconf" | tail -n 1 | tr -d '\r' | sed 's/[[:space:]]*$//'
  }
  if [ -n "$rf_vendor" ]; then
    list="$rf_vendor"
    if [ -z "$(rf_user_of "$rf_vendor")" ]; then
      echo "주의: $rfconf 에 ${rf_vendor}_user 가 없어 이 항목은 계정명을 적기 전까지 쓰이지 않습니다." >&2
    fi
  else
    if [ ! -f "$rfconf" ]; then
      echo "오류: $rfconf 이(가) 없습니다. 벤더를 직접 주십시오 (예: bash encrypt.sh rf dell)" >&2
      exit 1
    fi
    list=""
    for v in $rf_vendors; do
      [ -n "$(rf_user_of "$v")" ] && list="$list $v"
    done
    if [ -z "$list" ]; then
      echo "오류: $rfconf 에 계정명(<vendor>_user)이 있는 벤더가 없습니다." >&2
      exit 1
    fi
    echo "rf.conf 에 계정명이 있는 벤더:$list (차례로 비밀번호를 묻습니다)" >&2
  fi
  for v in $list; do
    echo "== $v (user=$(rf_user_of "$v")) ==" >&2
    "$BIN" encrypt -rf "$v" -key "$key" -out "$out"
  done
  fix_perm "$out" "$key"
  echo "권한 확인: $out, $key = 600"
  if [ -f "$rfconf" ] && grep -Eiq '^[[:space:]]*[a-z]+_pass[[:space:]]*=[[:space:]]*[^[:space:]]' "$rfconf"; then
    echo "주의: $rfconf 에 평문 <vendor>_pass 줄이 남아 있습니다. rf.enc 가 우선하지만 평문 줄은 지우십시오."
  fi
  echo "주의: $key 와 $out 은 짝입니다. 둘 다 git 에 올리지 마십시오 (.gitignore 에 제외됨)."
  exit 0
fi

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
