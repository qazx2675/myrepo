#!/usr/bin/env bash
# folder_setup.sh — SPEC_DIR(이 스크립트와 같은 위치에 생성되는 로컬 폴더) 아래에
# 새 스펙 디렉터리를 대화형으로 만드는 보조 스크립트.
#
# 폴더명 규칙(CAE 규칙) 검사와 중복 스펙 거부는 vm-param-check -initFolder가 그대로
# 해준다 — 이 스크립트가 그 로직을 다시 구현하지 않고, 만들어진 빈 틀에 값만 채운다.
set -euo pipefail
cd "$(dirname "$0")"

SPEC_DIR="./SPEC_DIR"

if [ ! -x ./vm-param-check ]; then
  echo "vm-param-check 바이너리가 없어 먼저 빌드합니다..."
  bash setup.sh
fi

mkdir -p "$SPEC_DIR"

read -r -p "생성할 vCenter 폴더 이름을 입력하세요 (예: RND-CAE001-LICE48c-TCAD): " folder_name
if [ -z "$folder_name" ]; then
  echo "폴더 이름은 필수입니다." >&2
  exit 1
fi

# 이름 규칙 위반이나 중복(차수만 달라도)은 여기서 걸린다 — 실패하면 그대로 종료.
./vm-param-check -specRoot="$SPEC_DIR" -initFolder="$folder_name"

spec_file="$SPEC_DIR/$folder_name/${folder_name}_spec.txt"

ask_required() {
  local prompt="$1"
  local value=""
  while [ -z "$value" ]; do
    read -r -p "$prompt (필수): " value
    [ -z "$value" ] && echo "  값이 필요합니다."
  done
  printf '%s' "$value"
}

ask_optional() {
  local prompt="$1"
  local value
  read -r -p "$prompt (선택, 필요 없으면 그냥 Enter): " value
  printf '%s' "$value"
}

echo
echo "=== 필수 값 (ev01 및 미분류 VM 기준) ==="
ht=$(ask_required "ht (on/off)")
cores=$(ask_required "cores (소켓당 코어 수)")
numa=$(ask_required "numa (NUMA 노드당 최대 vCPU 수)")
cpu=$(ask_required "cpu (vCPU 수)")
mem=$(ask_required "mem (메모리 GB)")
disk=$(ask_required "disk (디스크 총량 GB)")
shares_ev01=$(ask_required "shares-ev01 (CPU/메모리 Shares ratio)")

echo
echo "=== ev02 값 (선택 — 전부 비워두면 ev02 관련 체크는 스킵됨) ==="
cores_ev02=$(ask_optional "cores-ev02")
numa_ev02=$(ask_optional "numa-ev02")
cpu_ev02=$(ask_optional "cpu-ev02")
mem_ev02=$(ask_optional "mem-ev02")
disk_ev02=$(ask_optional "disk-ev02")
shares_ev02=$(ask_optional "shares-ev02")

echo
echo "=== ev03 값 (선택 — 전부 비워두면 ev03 관련 체크는 스킵됨) ==="
cores_ev03=$(ask_optional "cores-ev03")
numa_ev03=$(ask_optional "numa-ev03")
cpu_ev03=$(ask_optional "cpu-ev03")
mem_ev03=$(ask_optional "mem-ev03")
disk_ev03=$(ask_optional "disk-ev03")
shares_ev03=$(ask_optional "shares-ev03")

{
  echo "# $folder_name 스펙 정의 파일 (folder_setup.sh로 생성)"
  echo "ht=$ht"
  echo "cores=$cores"
  echo "numa=$numa"
  echo "cpu=$cpu"
  echo "mem=$mem"
  echo "disk=$disk"
  echo "shares-ev01=$shares_ev01"

  [ -n "$cores_ev02" ] && echo "cores-ev02=$cores_ev02"
  [ -n "$numa_ev02" ] && echo "numa-ev02=$numa_ev02"
  [ -n "$cpu_ev02" ] && echo "cpu-ev02=$cpu_ev02"
  [ -n "$mem_ev02" ] && echo "mem-ev02=$mem_ev02"
  [ -n "$disk_ev02" ] && echo "disk-ev02=$disk_ev02"
  [ -n "$shares_ev02" ] && echo "shares-ev02=$shares_ev02"

  [ -n "$cores_ev03" ] && echo "cores-ev03=$cores_ev03"
  [ -n "$numa_ev03" ] && echo "numa-ev03=$numa_ev03"
  [ -n "$cpu_ev03" ] && echo "cpu-ev03=$cpu_ev03"
  [ -n "$mem_ev03" ] && echo "mem-ev03=$mem_ev03"
  [ -n "$disk_ev03" ] && echo "disk-ev03=$disk_ev03"
  [ -n "$shares_ev03" ] && echo "shares-ev03=$shares_ev03"
} > "$spec_file"

echo
echo "생성 완료: $spec_file"
echo "--- 내용 ---"
cat "$spec_file"
