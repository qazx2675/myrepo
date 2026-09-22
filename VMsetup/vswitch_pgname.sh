#!/usr/bin/env bash
# vswitch_pgname.sh — vswitch_${user}.txt 의 포트그룹 컬럼을 "<폴더명>-cae-a-b-c-0" 형식으로 변환한다.
#
# 각 줄이 이미 "BM  <폴더명>-cae-a-b-c-d  VLAN" 형식(vm_setup.sh 가 알아보는 형식)이면 그대로 둔다.
# 그게 아니면 2번째 컬럼을 IP(a.b.c.d, 항상 /24 가정)로 보고 폴더명을 입력받아
# "<폴더명>-cae-<a>-<b>-<c>-0" 으로 바꾼다(마지막 옥텟은 /24 이므로 항상 0).
# IP도 아니면 판단할 수 없으므로 그대로 두고 경고만 낸다.
#
# 인라인 주석이 있는 줄을 변환하면 그 주석은 사라진다(변환된 줄에는 주석을 다시 붙이지 않는다).
#
# 사용법: vswitch_pgname.sh <vswitch_user.txt>
#   파일을 그 자리에서 바꾸고, 원본은 <파일>.bak 로 남긴다.
set -euo pipefail

PG_RE='^(.+)-[cC][aA][eE]-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3}-[0-9]{1,3}$'
IP_RE='^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$'

[ $# -eq 1 ] || { echo "사용법: $0 <vswitch_user.txt>" >&2; exit 2; }
FILE="$1"
[ -f "$FILE" ] || { echo "파일이 없습니다: $FILE" >&2; exit 1; }

is_octet() { [[ "$1" =~ ^[0-9]{1,3}$ ]] && [ "$1" -le 255 ]; }

TMP="$(mktemp)"
lineno=0
changed=0
# 파일을 fd 3 으로 읽는다 — stdin(fd 0)을 그대로 두면 대화형 폴더명 입력(read -p)과
# 입력 파일 읽기가 서로 stdin 을 두고 충돌한다(둘 다 fd 0 을 읽으면 파일 내용이 폴더명으로 잘못 들어간다).
exec 3< "$FILE"
while IFS= read -r raw <&3 || [ -n "$raw" ]; do
  lineno=$((lineno + 1))

  trimmed="$(printf '%s' "$raw" | sed -e 's/#.*$//' -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
  if [ -z "$trimmed" ]; then printf '%s\n' "$raw" >> "$TMP"; continue; fi

  bm="$(awk '{print $1}' <<< "$trimmed")"
  pg="$(awk '{print $2}' <<< "$trimmed")"
  vlan="$(awk '{print $3}' <<< "$trimmed")"
  if [ -z "$bm" ] || [ -z "$pg" ] || [ -z "$vlan" ]; then
    echo "[경고] ${lineno}번째 줄 형식을 알 수 없어 그대로 둡니다: $raw" >&2
    printf '%s\n' "$raw" >> "$TMP"
    continue
  fi

  if [[ "$pg" =~ $PG_RE ]]; then
    printf '%s\n' "$raw" >> "$TMP"
    continue
  fi

  if [[ "$pg" =~ $IP_RE ]]; then
    o1="${BASH_REMATCH[1]}"; o2="${BASH_REMATCH[2]}"; o3="${BASH_REMATCH[3]}"
    bad=0
    for o in "$o1" "$o2" "$o3"; do is_octet "$o" || bad=1; done
    if [ "$bad" -eq 1 ]; then
      echo "[경고] ${lineno}번째 줄 IP가 올바르지 않아 그대로 둡니다: $pg" >&2
      printf '%s\n' "$raw" >> "$TMP"
      continue
    fi
    read -r -p "${lineno}번째 줄 ($bm $pg $vlan) — 폴더명을 입력하세요: " folder
    if [ -z "$folder" ]; then
      echo "[오류] 폴더명이 비어 있어 ${lineno}번째 줄을 그대로 둡니다: $raw" >&2
      printf '%s\n' "$raw" >> "$TMP"
      continue
    fi
    newpg="${folder}-cae-${o1}-${o2}-${o3}-0"
    printf '%s  %s  %s\n' "$bm" "$newpg" "$vlan" >> "$TMP"
    changed=$((changed + 1))
    echo "[변환] ${lineno}번째 줄: $pg -> $newpg" >&2
  else
    echo "[경고] ${lineno}번째 줄 포트그룹명이 CAE 형식도 IP도 아니어서 그대로 둡니다: $pg" >&2
    printf '%s\n' "$raw" >> "$TMP"
  fi
done
exec 3<&-

cp "$FILE" "${FILE}.bak"
mv "$TMP" "$FILE"
echo "[완료] ${changed}줄 변환함 (원본 백업: ${FILE}.bak)"
