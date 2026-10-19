#!/usr/bin/env bash
# demo_env.sh — bios.sh(TUI·전체비교·수정) 를 직접 눌러 볼 수 있는 모의 환경 (실제 BMC 접속 없음, Go 불필요).
#
# 127.0.0.1 의 포트 19201~19207 에 벤더·모델이 다른 Redfish mock BMC 7대(mockbmc)와,
# 19301·19302 에 Cisco UCSM XML API mock 2대(mockucsm)를 백그라운드로 띄우고,
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
MOCKU="$ROOT/bin/mockucsm$SUF"

# 포트 이름 트리 (벤더·모델 섞음: HPE Gen10/Gen11, Dell, Lenovo, Cisco, Supermicro)
MOCKS="19201:hpe-dl360gen11 19202:hpe-dl360gen11 19203:hpe-dl360gen10 19204:dell-r660 19205:lenovo-sr650v3 19206:cisco-c220m7 19207:cisco-c220m7"

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
  chmod +x "$BIOS" "$MOCK" "$MOCKU" 2>/dev/null || true
  for b in "$BIOS" "$MOCK" "$MOCKU"; do
    if [ ! -x "$b" ]; then
      echo "오류: $b 이(가) 없거나 실행할 수 없습니다." >&2
      echo "  (빌드: go build -o bin/mockucsm$SUF ./cmd/mockucsm — mockucsm 바이너리가 필요합니다)" >&2
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
    # 19207 은 IP 줄이 아니라 c301(XML 실패 → Redfish 폴백 시연)로만 쓴다
    [ "$port" = "19207" ] || echo "127.0.0.1:$port" >>"$D/user.txt"
  done

  # Cisco UCSM XML mock: 19301 = 블레이드 1대(c101), 19302 = 도메인 1개에 블레이드 3대(서비스 프로파일 c201·c202·other)
  # 서버 정의 JSON 은 사용자/비밀번호 포함 — mock 전용 값이고 권한 600 으로 만든다.
  umask 077
  cat >"$D/ucsm_single.json" <<EOF
{"user":"$USER_ID","password":"$USER_PW","servers":[
 {"dn":"sys/chassis-1/blade-1","class":"computeBlade","model":"UCSX-210C-M7","sp_name":"c101",
  "bios":{"biosVfCPUPerformance":{"vpCPUPerformance":"hpc"},
          "biosVfIntelHyperThreadingTech":{"vpIntelHyperThreadingTech":"disabled"}}}
]}
EOF
  cat >"$D/ucsm_domain.json" <<EOF
{"user":"$USER_ID","password":"$USER_PW","servers":[
 {"dn":"sys/chassis-1/blade-1","class":"computeBlade","model":"UCSX-210C-M7","sp_name":"c201",
  "bios":{"biosVfCPUPerformance":{"vpCPUPerformance":"hpc"},
          "biosVfIntelHyperThreadingTech":{"vpIntelHyperThreadingTech":"enabled"}}},
 {"dn":"sys/chassis-1/blade-2","class":"computeBlade","model":"UCSX-210C-M7","sp_name":"c202",
  "bios":{"biosVfCPUPerformance":{"vpCPUPerformance":"platform-default"},
          "biosVfIntelHyperThreadingTech":{"vpIntelHyperThreadingTech":"enabled"}}},
 {"dn":"sys/chassis-1/blade-3","class":"computeBlade","model":"UCSX-210C-M7","sp_name":"other",
  "bios":{"biosVfCPUPerformance":{"vpCPUPerformance":"hpc"},
          "biosVfIntelHyperThreadingTech":{"vpIntelHyperThreadingTech":"disabled"}}}
]}
EOF
  umask 022
  for m in 19301:ucsm_single 19302:ucsm_domain; do
    port="${m%%:*}"
    nohup "$MOCKU" -listen "127.0.0.1:$port" -data "$D/${m#*:}.json" \
      >"$D/logs/ucsm_$port.log" 2>&1 &
    echo $! >>"$D/pids"
  done

  # 관리망 이름 해석용 hosts: c101-m 등은 모두 127.0.0.1 (포트는 user 파일 줄의 :포트 로 구분)
  echo "127.0.0.1 c101-m c201-m c202-m c301-m" >"$D/hosts"
  # user.txt = 혼합 목록: IP 줄(Redfish 7대 중 6대) + Cisco 이름 줄(c 로 시작 → XML 먼저, 실패하면 Redfish 폴백). user_ip_only.txt = IP 줄만
  cp "$D/user.txt" "$D/user_ip_only.txt"
  {
    echo "c101:19301"
    echo "c201:19302"
    echo "c202:19302"
    echo "c301:19207"
  } >>"$D/user.txt"
  # -user-xml 시험용: IP 형태 XML 대상 (이름 접두사 규칙과 무관하게 XML 로 접속)
  echo "127.0.0.1:19301" >"$D/user_xml.txt"

  i=0
  while [ $i -lt 50 ]; do
    n="$(grep -l "서빙합니다" "$D"/logs/mock_*.log "$D"/logs/ucsm_*.log 2>/dev/null | wc -l)"
    [ "$n" -ge 9 ] && break
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
xml_host_prefix=c
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
  # Cisco UCSM/CIMC XML 구역: attribute = <biosVf 클래스>.<속성>. hyper_threading 은 verified=Y 라 c101 에서 FAIL 이 난다
  cat >>"$D/profiles/DEMO.tsv" <<'EOF'
# [xml]
CISCO	UCS X210c M7	system_profile	biosVfCPUPerformance.vpCPUPerformance	hpc	Y
CISCO	UCS X210c M7	hyper_threading	biosVfIntelHyperThreadingTech.vpIntelHyperThreadingTech	enabled	Y
EOF
  chmod 600 "$D/bios.conf"

  # 전체비교 refs JSON: 19201(HPE Gen11) 을 기준 파일로 추출 (실패해도 계속)
  echo "127.0.0.1:19202" >"$D/ref_target.txt"
  "$BIOS" allcheck -conf "$D/bios.conf" -user "$D/ref_target.txt" -ref 127.0.0.1:19201 -export-attrs \
    -hosts "$D/hosts" >"$D/logs/export.log" 2>&1 || true
  for f in "$D"/results/*_all/attrs/127.0.0.1_19201.json; do
    [ -f "$f" ] && cp "$f" "$D/refs/hpe-dl360gen11-ref.json"
  done

  cat <<EOF

모의 환경 준비 완료: $D
  Redfish mock BMC 7대 (127.0.0.1:19201~19207) + Cisco UCSM XML mock 2대 (19301·19302)
  19201·19202 HPE DL360 Gen11 / 19203 HPE DL360 Gen10 / 19204 Dell R660 / 19205 Lenovo SR650 V3 / 19206·19207 Cisco C220 M7 (Redfish)

시연 호스트 (이름은 $D/hosts 에서 127.0.0.1 로 풀리고 포트는 user.txt 줄의 :포트 로 구분):
  c101:19301  XML  UCSM mock, 블레이드 1대 (X210c M7). hyper_threading=disabled 라 DEMO 프로파일에서 FAIL 1건
  c201:19302  XML  블레이드 3대 도메인에서 서비스 프로파일 이름 c201 인 블레이드만 선택 (모두 OK)
  c202:19302  XML  같은 도메인에서 c202 블레이드만 선택 (system_profile=platform-default → FAIL)
  c301:19207  XML 실패 → Redfish 폴백 (19207 은 Redfish mock 이라 /nuova 가 없음)
  127.0.0.1:19201~19206  이름 접두사 c 가 아니므로 Redfish 로 점검 (user.txt 앞쪽 6줄)
  127.0.0.1:19301  user_xml.txt — IP 형태 XML 대상 (-user-xml 로 지정해야 XML)
  XML 쪽 계정은 mockbmc 와 같은 시험용 계정, 서버 정의는 $D/ucsm_single.json·ucsm_domain.json

따라 해 보기 (저장소 폴더에서):
  # 1) TUI 전체 흐름: 대상 입력 → 프로파일(DEMO 추천) → 결과 → Tab(전체 설정 비교) → 기준 선택 → e(수정 단계), q 두 번 종료
  bash bios.sh -conf $D/bios.conf -hosts $D/hosts
  # 2) 대상 파일 지정 + 프로파일 지정 (Redfish 6대만)
  bash bios.sh -conf $D/bios.conf -hosts $D/hosts -user $D/user_ip_only.txt -profile DEMO
  # 3) 평문 출력 + 비교서버 지정
  bash bios.sh -conf $D/bios.conf -hosts $D/hosts -user $D/user_ip_only.txt -profile DEMO -plain -ref 127.0.0.1:19201
  # 4) Redfish + Cisco XML 혼합 실행 (user.txt = IP 6줄 + c101·c201·c202·c301)
  bash bios.sh -conf $D/bios.conf -hosts $D/hosts -profile DEMO -user $D/user.txt
  # 5) IP 형태 XML 대상 지정 (-user-xml)
  bash bios.sh -conf $D/bios.conf -hosts $D/hosts -profile DEMO -user $D/user.txt -user-xml $D/user_xml.txt
  # 6) auto_setup 비대화형 (설정 안 함, 현재 폴더에 check.res_tester)
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
    sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
    exit 1
    ;;
esac
