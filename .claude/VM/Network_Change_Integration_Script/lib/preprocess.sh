# lib/preprocess.sh — vswitch_${user}.txt 검증·표준화 (§4)
#
# 출력은 vm-network-migration 의 LoadWorklist 가 요구하는 정확히 3열
#   <BM호스트.도메인>  <포트그룹명>  <VLAN>
#
# change.sh 가 source 합니다. 필요 변수:
#   PP_DOMAIN  (bm_domain)
#   PP_TAG     (preprocess_tag, Case 1/2 가운데 문자열)
#   PP_FOLDER  (선택 — Case 2 폴더명. 비어 있고 대화형이면 물어봄)

_is_ip() { [[ "$1" =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]]; }
_is_num() { [[ "$1" =~ ^[0-9]+$ ]]; }

_mkpg() {
  # _mkpg <folder> <ip>  →  folder-TAG-a-b-c-0  (앞 3옥텟, 4옥텟은 항상 0)
  local folder="$1" ip="$2" a b c d
  IFS=. read -r a b c d <<<"$ip"
  printf '%s-%s-%s-%s-%s-0' "$folder" "$PP_TAG" "$a" "$b" "$c"
}

_ask_folder() {
  if [ -n "$PP_FOLDER" ]; then printf '%s' "$PP_FOLDER"; return; fi
  if [ ! -t 0 ]; then
    die A2 "3열(BM IP VLAN) 줄은 폴더명이 필요합니다. -folder 로 지정하거나 4열 형식을 쓰십시오."
  fi
  local f
  read -r -p "  3열 줄들에 쓸 폴더명: " f
  [ -n "$f" ] || die A2 "폴더명이 비어 있습니다."
  PP_FOLDER="$f"
  printf '%s' "$f"
}

# preprocess_vswitch <입력파일> <출력파일>
preprocess_vswitch() {
  local in="$1" out="$2"
  [ -f "$in" ] || die A1 "vswitch 파일을 열 수 없습니다: $in"

  : >"$out"
  local ln=0 line host fqdn n folder ip vlan pg
  local -a F
  while IFS= read -r line || [ -n "$line" ]; do
    ln=$((ln + 1))
    line="${line%%$'\r'}"
    case "$line" in ''|'#'*) continue ;; esac
    # shellcheck disable=SC2206
    F=($line)
    n=${#F[@]}
    host="${F[0]}"

    # 이미 도메인이 붙어 있으면(FQDN) 변환하지 않고 3열 검증만 (멱등).
    # IP 주소는 점이 있어도 FQDN 이 아니므로 아래 케이스 판정으로 내려갑니다.
    if [[ "$host" == *.* ]] && ! _is_ip "$host"; then
      [ "$n" -eq 3 ]   || die A2 "$in:$ln 도메인 포함 줄은 3열이어야 합니다 (열 $n): $line"
      _is_num "${F[2]}" || die A3 "$in:$ln VLAN 이 숫자가 아닙니다: $line"
      printf '%s %s %s\n' "${F[0]}" "${F[1]}" "${F[2]}" >>"$out"
      continue
    fi

    # IP 는 도메인을 붙이지 않고, short name 은 도메인을 붙입니다.
    if _is_ip "$host"; then fqdn="$host"; else fqdn="${host}.${PP_DOMAIN}"; fi
    case "$n" in
      4)  # BM 폴더 IP VLAN
        folder="${F[1]}"; ip="${F[2]}"; vlan="${F[3]}"
        _is_ip "$ip"    || die A3 "$in:$ln 3번째 필드가 IP 형식이 아닙니다: $line"
        _is_num "$vlan" || die A3 "$in:$ln VLAN 이 숫자가 아닙니다: $line"
        pg="$(_mkpg "$folder" "$ip")"
        printf '%s %s %s\n' "$fqdn" "$pg" "$vlan" >>"$out"
        ;;
      3)
        if _is_ip "${F[1]}"; then
          # BM IP VLAN  → 폴더명 추가 입력
          ip="${F[1]}"; vlan="${F[2]}"
          _is_num "$vlan" || die A3 "$in:$ln VLAN 이 숫자가 아닙니다: $line"
          folder="$(_ask_folder)"
          pg="$(_mkpg "$folder" "$ip")"
          printf '%s %s %s\n' "$fqdn" "$pg" "$vlan" >>"$out"
        else
          # Case 3: 이미 BM PG VLAN — 도메인만 붙입니다
          _is_num "${F[2]}" || die A3 "$in:$ln VLAN 이 숫자가 아닙니다: $line"
          printf '%s %s %s\n' "$fqdn" "${F[1]}" "${F[2]}" >>"$out"
        fi
        ;;
      *)
        die A2 "$in:$ln 열 개수가 $n 입니다 — 3 또는 4 여야 합니다: $line"
        ;;
    esac
  done <"$in"

  [ -s "$out" ] || die A2 "$in 에 유효한 항목이 없습니다."
  log A "표준화 완료: $(grep -cve '^[[:space:]]*$' "$out")건 → $out"
}
