#!/usr/bin/env bash
# test.sh — biostool TUI 를 mock BMC 로 바로 시험해 보는 원클릭 데모 (실제 BMC 에는 접속하지 않음).
#
# 사용법:
#   bash test.sh           mock BMC 7대를 띄우고 TUI(biostool bios)를 바로 실행
#   bash test.sh --check   TUI 없이 같은 환경에서 점검+전체비교(-plain)를 돌려 OK/FAIL 출력
#   bash test.sh --clean   .testenv/ 와 .testbin/ 삭제
#
# 하는 일: 새로 빌드(.testbin/, bin/ 은 건드리지 않음) -> mock BMC 7대(127.0.0.1:18441~18447) 기동
#          -> 시험용 작업 폴더 .testenv/ 생성 (실제 user.txt·diff.txt·results 는 건드리지 않음) -> 실행 -> 종료 시 mock 정리.
set -euo pipefail
cd "$(dirname "$0")"
root="$PWD"
bindir="$root/.testbin"
env_dir="$root/.testenv"
base_port=18441

case "${1:-}" in
  -h | --help)
    sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
    ;;
  --clean)
    rm -rf "$bindir" "$env_dir"
    echo "삭제했습니다: .testbin/ .testenv/"
    exit 0
    ;;
  "" | --check) ;;
  *)
    echo "오류: 알 수 없는 인자 $1  (bash test.sh [--check|--clean])" >&2
    exit 1
    ;;
esac
mode="${1:-tui}"

if ! command -v go >/dev/null 2>&1; then
  for g in /usr/local/go/bin /usr/lib/golang/bin /opt/go/bin; do
    [ -x "$g/go" ] && PATH="$g:$PATH" && break
  done
fi
if ! command -v go >/dev/null 2>&1; then
  echo "오류: go 를 찾을 수 없습니다. 이 시험은 Go 가 설치된 리눅스 호스트에서 실행하십시오 (예: export PATH=/usr/local/go/bin:\$PATH)." >&2
  exit 2
fi

# 1) 새로 빌드 (bin/biostool 은 덮어쓰지 않음)
mkdir -p "$bindir"
echo "[1/4] 빌드: $(go version | cut -d' ' -f3)"
export GOFLAGS="${GOFLAGS:--mod=mod}" GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
CGO_ENABLED=0 go build -trimpath -o "$bindir/biostool" ./cmd/biostool
CGO_ENABLED=0 go build -trimpath -o "$bindir/mockbmc" ./cmd/mockbmc

# 2) mock BMC 기동 (종료 시 정리)
pids=()
cleanup() {
  local p
  for p in ${pids[@]+"${pids[@]}"}; do kill "$p" 2>/dev/null || true; done
  for p in ${pids[@]+"${pids[@]}"}; do wait "$p" 2>/dev/null || true; done
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

# 포트  트리  태그 (태그 '-' = 없음)
mocks=(
  "0 dell-r660 gold"
  "1 dell-r660 gold"
  "2 hpe-dl360gen11 gold"
  "3 hpe-dl360gen11 silver"
  "4 hpe-dl360gen10 silver"
  "5 lenovo-sr650v3 -"
  "6 dell-r660 -"
)
pass="mock-demo-pass" # mock 전용 값 (실제 BMC 비밀번호 아님)
echo "[2/4] mock BMC ${#mocks[@]}대 기동"
mkdir -p "$env_dir"
: >"$env_dir/user.txt"
for m in "${mocks[@]}"; do
  set -- $m
  port=$((base_port + $1))
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    echo "오류: 127.0.0.1:$port 가 이미 사용 중입니다 (이전 mock 이 남아 있으면 pkill mockbmc)." >&2
    exit 3
  fi
  MOCKBMC_PASS="$pass" "$bindir/mockbmc" -dir "testdata/$2" -listen "127.0.0.1:$port" -user admin >"$env_dir/mock_$port.log" 2>&1 &
  pids+=("$!")
  if [ "$3" = "-" ]; then
    echo "127.0.0.1:$port" >>"$env_dir/user.txt"
  else
    printf '127.0.0.1:%s %s\n' "$port" "$3" >>"$env_dir/user.txt"
  fi
done
for m in "${mocks[@]}"; do
  set -- $m
  port=$((base_port + $1))
  ok=0
  for _ in $(seq 1 50); do
    if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then ok=1; break; fi
    sleep 0.1
  done
  if [ "$ok" != 1 ]; then
    echo "오류: mock($2, 포트 $port) 가 뜨지 않았습니다. 로그:" >&2
    cat "$env_dir/mock_$port.log" >&2
    exit 3
  fi
done

# 3) 시험용 작업 폴더 (.testenv) — 실제 설정·결과와 분리
echo "[3/4] 시험 작업 폴더 .testenv/ 준비"
rm -rf "$env_dir/results" "$env_dir/refs" "$env_dir/dumps" "$env_dir/profiles"
mkdir -p "$env_dir/results" "$env_dir/refs" "$env_dir/dumps" "$env_dir/profiles"
cp testdata/profiles-test/VM.tsv "$env_dir/profiles/VM.tsv"
cat >"$env_dir/bios.conf" <<CONF
# test.sh 가 만든 시험용 설정 (mock 전용)
user=admin
pass_file=pass.enc
key_file=key.bin
concurrency=10
timeout=10
insecure=true
retries=1
result_dir=results
dump_dir=dumps
profile_dir=profiles
refs_dir=refs
CONF
# 전체비교 기준: 태그 gold/silver 와 태그 없음 각각 한 대 (같은 모델끼리만 비교됨)
cat >"$env_dir/diff.txt" <<DIFF
127.0.0.1:$((base_port + 0)) gold
127.0.0.1:$((base_port + 3)) silver
127.0.0.1:$((base_port + 6))
DIFF
printf 'ServiceTag\nAssetTag\nSerialNumber\nServerName\n' >"$env_dir/ignore_attrs.txt"
echo "# 시험용: hosts 비움 (대상은 모두 IP:포트)" >"$env_dir/hosts"
rm -f "$env_dir/key.bin" "$env_dir/pass.enc"
printf '%s' "$pass" | (cd "$env_dir" && "$bindir/biostool" encrypt -key key.bin -out pass.enc >/dev/null)

cd "$env_dir"
common=(-conf bios.conf -hosts hosts -user user.txt -ignore ignore_attrs.txt -diff diff.txt)

if [ "$mode" = "--check" ]; then
  echo "[4/4] 비대화형 점검+전체비교 (-plain)"
  rc=0
  out="$("$bindir/biostool" bios -plain -profile VM "${common[@]}" 2>&1 </dev/null)" || rc=$?
  printf '%s\n' "$out" | tail -n 40
  if [ "$rc" -ne 0 ]; then
    echo "FAIL: biostool 종료코드 $rc"
    exit 1
  fi
  if ! printf '%s' "$out" | grep -q '127\.0\.0\.1:1844'; then
    echo "FAIL: 출력에 mock 호스트가 없음"
    exit 1
  fi
  if printf '%s' "$out" | grep -Eq 'AUTH_FAIL|UNREACHABLE|TIMEOUT'; then
    echo "FAIL: 접속 오류 (AUTH_FAIL/UNREACHABLE/TIMEOUT) 가 있음"
    exit 1
  fi
  echo "OK: mock ${#mocks[@]}대 점검·전체비교 완료 (결과: .testenv/results/)"
  exit 0
fi

cat <<'GUIDE'
[4/4] TUI 시작 — 시험 대상 7대 (127.0.0.1:18441~18447, 모두 mock)
   18441 Dell R660      태그 gold      18444 HPE DL360 Gen11  태그 silver
   18442 Dell R660      태그 gold      18445 HPE DL360 Gen10  태그 silver
   18443 HPE DL360 Gen11 태그 gold      18446 Lenovo SR650 V3  (태그 없음)
                                        18447 Dell R660        (태그 없음)
 시험해 볼 것
  - 대상 붙여넣기: 엑셀 식 "IP<Tab>태그" 여러 줄 붙여넣기 (예: 127.0.0.1:18441 <Tab> gold), 끝낼 때 빈 줄에서 Enter
  - Tab: 화면 전환 / ←→: 태그 이동 / r: 기준(비교서버) 다시 고르기
  - x: 모델이 달라도 비교 (gold 태그의 Dell 기준 vs HPE 대상 = 모델 불일치 비교)
  - e: 수정(설정) 메뉴 — mock 에만 쓰이며 실제 장비는 건드리지 않음
  - 기준: diff.txt = 18441(gold), 18444(silver), 18447(태그 없음) / 프로파일 VM
  - 종료: q (mock 은 자동 정리, .testenv 는 남음: bash test.sh --clean 으로 삭제)
GUIDE
read -r -p "엔터를 누르면 시작합니다 ... " _ || true
# exec 를 쓰면 EXIT trap(mock 정리)이 돌지 않으므로 foreground 로 실행하고 종료 후 정리한다.
set +e
"$bindir/biostool" bios -profile VM "${common[@]}"
rc=$?
exit "$rc"
