#!/usr/bin/env bash
# demo_env.sh — bios.sh(TUI·전체비교·수정) 를 직접 눌러 볼 수 있는 모의 환경 (실제 BMC 접속 없음, Go 불필요).
#
# 127.0.0.1 의 포트 19201~19206 에 벤더·모델이 다른 mock BMC 6대를 백그라운드로 띄우고,
# /tmp/bios_demo (또는 BIOS_DEMO_DIR) 에 시험용 bios.conf·pass.enc·user.txt·profiles·refs 를 만든다.
#
# 사용법:
#   bash testcase/demo_env.sh start     # mock 기동 + 시험 파일 생성, 따라 할 명령 출력
#   bash testcase/demo_env.sh status    # mock 살아 있는지
#   bash testcase/demo_env.sh stop      # mock 종료 + 시험 폴더 삭제
#
# 시험용 계정·비밀번호는 mock 전용 값입니다 (실제 계정 아님). bash 4.1(RHEL6) 호환.
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
D="${BIOS_DEMO_DIR:-/tmp/bios_demo}"
USER_ID="demoadmin"
USER_PW="Demo-Test-1"

kmajor="$(uname -r | cut -d. -f1)"
case "$kmajor" in '' | *[!0-9]*) kmajor=99 ;; esac
SUF=""
if [ "$kmajor" -lt 3 ]; then SUF="_os6"; fi
BIOS="$ROOT/bin/biostool$SUF"
MOCK="$ROOT/bin/mockbmc$SUF"

# 포트 이름 트리 (벤더·모델 섞음: HPE Gen10/Gen11, Dell, Lenovo, Cisco, Supermicro)
MOCKS="19201:hpe-dl360gen11 19202:hpe-dl360gen11 19203:hpe-dl360gen10 19204:dell-r660 19205:lenovo-sr650v3 19206:cisco-c220m7"

stop_all() {
  if [ -f "$D/pids" ]; then
    for p in $(cat "$D/pids"); do kill "$p" 2>/dev/null; done
  fi
  rm -rf "$D"
  echo "모의 환경을 종료하고 $D 를 지웠습니다."
}

status_all() {
  if [ ! -f "$D/pids" ]; then
    echo "모의 환경이 없습니다 (bash testcase/demo_env.sh start)."
    return 1
  fi
  for p in $(cat "$D/pids"); do
    if kill -0 "$p" 2>/dev/null; then echo "  pid $p 실행 중"; else echo "  pid $p 종료됨"; fi
  done
}

start_all() {
  chmod +x "$BIOS" "$MOCK" 2>/dev/null || true
  for b in "$BIOS" "$MOCK"; do
    if [ ! -x "$b" ]; then
      echo "오류: $b 이(가) 없거나 실행할 수 없습니다." >&2
      exit 2
    fi
  done
  if [ -f "$D/pids" ]; then
    echo "이미 실행 중입니다. 다시 만들려면 먼저: bash testcase/demo_env.sh stop" >&2
    exit 1
  fi
  mkdir -p "$D/profiles" "$D/results" "$D/refs" "$D/logs"
  : >"$D/pids"
  : >"$D/hosts"

  open_flag=""
  if "$MOCK" -h 2>&1 | grep -q -- "-open-serviceroot"; then open_flag="-open-serviceroot"; fi

  : >"$D/user.txt"
  for m in $MOCKS; do
    port="${m%%:*}"
    tree="${m#*:}"
    MOCKBMC_PASS="$USER_PW" nohup "$MOCK" -dir "$ROOT/testdata/$tree" -listen "127.0.0.1:$port" -user "$USER_ID" $open_flag \
      >"$D/logs/mock_$port.log" 2>&1 &
    echo $! >>"$D/pids"
    echo "127.0.0.1:$port" >>"$D/user.txt"
  done
  i=0
  while [ $i -lt 50 ]; do
    n="$(grep -l "서빙합니다" "$D"/logs/mock_*.log 2>/dev/null | wc -l)"
    [ "$n" -ge 6 ] && break
    sleep 0.1
    i=$((i + 1))
  done

  cat >"$D/bios.conf" <<EOF
user=$USER_ID
pass_file=$D/pass.enc
key_file=$D/key.bin
concurrency=10
timeout=5
insecure=true
retries=0
auth_fail_stop=0
result_dir=$D/results
dump_dir=$D/dumps
profile_dir=$D/profiles
refs_dir=$D/refs
# 시험용: 전체비교 difference 도 수정 대상에 넣는다 (운영 기본값은 off)
allcheck_set=on
exclude_attrs=SerialNumber|ServerName|*Uuid*
auto_profile=VM_research
EOF
  printf '%s' "$USER_PW" | "$BIOS" encrypt -key "$D/key.bin" -out "$D/pass.enc" >/dev/null || {
    echo "오류: 비밀번호 암호화 실패" >&2
    exit 2
  }
  cp "$ROOT"/profiles/*.tsv "$D/profiles/" 2>/dev/null
  # 일부러 FAIL 이 나도록: mock HPE Gen11 의 하이퍼스레딩과 반대 값을 verified=Y 로
  printf 'vendor\tmodel\tstd_name\tattribute\tvalue\tverified\n' >"$D/profiles/DEMO.tsv"
  printf 'HPE\tDL360 Gen11\thyper_threading\tProcHyperthreading\tDisabled\tY\n' >>"$D/profiles/DEMO.tsv"
  printf 'HPE\t*\tllc_prefetch\tLlcPrefetch\tEnabled\tN\n' >>"$D/profiles/DEMO.tsv"
  printf 'DELL\tR660\thyper_threading\tLogicalProc\tEnabled\tY\n' >>"$D/profiles/DEMO.tsv"
  chmod 600 "$D/bios.conf"

  # 전체비교 refs JSON: 19201(HPE Gen11) 을 기준 파일로 추출 (실패해도 계속)
  echo "127.0.0.1:19202" >"$D/ref_target.txt"
  "$BIOS" allcheck -conf "$D/bios.conf" -user "$D/ref_target.txt" -ref 127.0.0.1:19201 -export-attrs \
    -hosts "$D/hosts" >"$D/logs/export.log" 2>&1 || true
  for f in "$D"/results/*_all/attrs/127.0.0.1_19201.json; do
    [ -f "$f" ] && cp "$f" "$D/refs/hpe-dl360gen11-ref.json"
  done

  cat <<EOF

모의 환경 준비 완료: $D  (mock BMC 6대 127.0.0.1:19201~19206)
  19201·19202 HPE DL360 Gen11 / 19203 HPE DL360 Gen10 / 19204 Dell R660 / 19205 Lenovo SR650 V3 / 19206 Cisco C220 M7

따라 해 보기 (저장소 폴더에서):
  # 1) TUI 전체 흐름: 대상 입력 → 프로파일(DEMO 추천) → 결과 → 전체비교 y → 기준 선택 → 수정 질문
  bash bios.sh -conf $D/bios.conf -hosts $D/hosts
  # 2) 대상 파일 지정 + 프로파일 지정
  bash bios.sh -conf $D/bios.conf -hosts $D/hosts -user $D/user.txt -profile DEMO
  # 3) 평문 출력 + 비교서버 지정
  bash bios.sh -conf $D/bios.conf -hosts $D/hosts -user $D/user.txt -profile DEMO -plain -ref 127.0.0.1:19201
  # 4) auto_setup 비대화형 (설정 안 함, 현재 폴더에 check.res_tester)
  cd /tmp && bash $ROOT/bios.sh -auto-check tester $D/user.txt -conf $D/bios.conf -hosts $D/hosts; cat /tmp/check.res_tester; cd -
  # 상태 / 종료
  bash testcase/demo_env.sh status
  bash testcase/demo_env.sh stop
EOF
}

case "${1:-}" in
  start) start_all ;;
  stop) stop_all ;;
  status) status_all ;;
  *)
    sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
    exit 1
    ;;
esac
