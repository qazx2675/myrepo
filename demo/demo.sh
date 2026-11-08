#!/bin/bash
# demo.sh - install_mn 데모 환경을 한 번에 만들고 실행한다 (랩·연습용, 실제 서버에 접속하지 않음)
#
# 사용법:  sudo bash demo/demo.sh        (root 필요: raw ICMP, /etc/hosts, lo 주소)
#          bash demo/demo.sh -y          (안내 화면에서 Enter 기다리지 않음)
#          bash demo/demo.sh -n 8000     (호스트 대수. 기본 27 = 기존 데모 그대로, 27 초과면 4자리 번호 이름으로 자동 생성)
#          bash demo/demo.sh -name 이름  (기본 pm_demo. 작업 폴더 /tmp/<이름>, /etc/hosts 표시, OFF 주소가 이름별로 달라
#                                          데모 두 개를 동시에 돌려도 서로 건드리지 않는다)
#   같은 이름의 데모가 이미 돌고 있으면(/tmp/<이름>/demo.pid 의 프로세스가 살아 있거나 그 폴더에서 도는 프로세스가 있으면)
#   아무것도 지우지 않고 거절한다.
#
# 하는 일
#   1. /tmp/pm_demo 에 가짜 gossh·ssh, 설정 파일, 호스트 파일(26대)을 만든다.
#   2. /etc/hosts 에 데모 호스트 이름을 잠깐 넣는다(127.77.x = ping O, 192.0.2.x = ping X).
#   3. 시나리오를 백그라운드로 돌려 시간에 따라 상태를 바꾼다(설치완료·지연·완료후꺼짐·재부팅됨).
#   4. install_mn 을 실행한다. 종료(q)하면 /etc/hosts·lo 주소·임시 폴더를 모두 원래대로 되돌린다.
set -u

HERE="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
TOP="$(cd "$HERE/.." && pwd)"
NAME=pm_demo
N=27
YES=0
while [ $# -gt 0 ]; do
  case "$1" in
    -y) YES=1 ;;
    -n) shift; N="${1:-}" ;;
    -name) shift; NAME="${1:-}" ;;
    *) echo "알 수 없는 옵션: $1  (사용: [-y] [-n 대수] [-name 이름])" >&2; exit 2 ;;
  esac
  shift
done
case "$N" in ''|*[!0-9]*) echo "-n 은 숫자여야 합니다 (27 이상)." >&2; exit 2 ;; esac
[ "$N" -ge 27 ] || { echo "-n 은 27 이상이어야 합니다 (기본 27)." >&2; exit 2; }
case "$NAME" in ''|*[!A-Za-z0-9_]*) echo "-name 은 영문·숫자·_ 만 쓸 수 있습니다." >&2; exit 2 ;; esac
WORK=/tmp/$NAME
MARK_BEGIN="# $NAME BEGIN"
MARK_END="# $NAME END"
OFF_HOST=s005          # 완료 후 꺼졌다가 다시 켜지는 서버
OFF_IP=172.31.77.5
if [ "$NAME" != pm_demo ]; then
  # 이름마다 다른 OFF 주소(172.31.78.x). 같은 이름은 항상 같은 주소
  OFF_IP="172.31.78.$(( $(printf %s "$NAME" | cksum | cut -d' ' -f1) % 250 + 1 ))"
fi
INTERVAL=10
[ "$N" -gt 1000 ] && INTERVAL=30     # 8000대는 ICMP 한 바퀴가 약 16초(ping_pps 500)라 30초 간격
[ "$N" -gt 27 ] && OFF_HOST=s0005

if [ "$(id -u)" != "0" ]; then
  echo "root 로 실행하세요 (raw ICMP, /etc/hosts, lo 주소 변경이 필요합니다)." >&2
  exit 1
fi

# ---- 같은 이름의 데모가 이미 돌고 있으면 거절 (여기서 끝내므로 아래 정리 trap 은 아직 없다) ----
RUNNING=""
if [ -f "$WORK/demo.pid" ]; then
  op=$(cat "$WORK/demo.pid" 2>/dev/null)
  [ -n "$op" ] && [ "$op" != "$$" ] && kill -0 "$op" 2>/dev/null && RUNNING="demo.sh pid $op"
fi
if [ -z "$RUNNING" ] && [ -d "$WORK" ]; then
  for pd in /proc/[0-9]*; do   # 예전 버전 데모(pid 파일 없음)도 그 폴더에서 도는 프로세스로 알아본다
    [ "${pd#/proc/}" = "$$" ] && continue
    [ "$(readlink "$pd/cwd" 2>/dev/null)" = "$WORK" ] && { RUNNING="프로세스 ${pd#/proc/}"; break; }
  done
fi
if [ -n "$RUNNING" ]; then
  echo "이름 '$NAME' 데모가 이미 실행 중입니다($RUNNING). 지우지 않고 종료합니다." >&2
  echo "다른 데모를 함께 돌리려면 -name 으로 다른 이름을 주세요 (예: -name ${NAME}2)." >&2
  exit 1
fi

BIN="$TOP/install_mn_os8"
if [ ! -x "$BIN" ]; then
  [ -f "$BIN" ] && chmod +x "$BIN"
fi
if [ ! -x "$BIN" ]; then
  echo "install_mn_os8 가 없어 build.sh 로 빌드합니다." >&2
  bash "$TOP/build.sh" -n || exit 1
fi

# ---- 정리 (어떻게 끝나든 실행) ----
SCEN_PID=""
cleanup() {
  [ -n "$SCEN_PID" ] && kill "$SCEN_PID" 2>/dev/null
  sed -i "/^$MARK_BEGIN\$/,/^$MARK_END\$/d" /etc/hosts
  ip addr del "$OFF_IP/32" dev lo 2>/dev/null
  rm -rf "$WORK"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

cleanup   # 지난번 데모가 비정상 종료돼 남긴 것 정리
mkdir -p "$WORK/bin"
echo $$ > "$WORK/demo.pid"

# ---- 호스트 ----
SUMMARY="설치중 13대, 디스크미인식 2대, 설치완료 9대, ping X 3대"
if [ "$N" -gt 27 ]; then
# N>27: 한 번의 awk 로 hosts.txt, hosts.block, state, demo.env 를 만든다(호스트마다 프로세스를 띄우지 않음)
awk -v n="$N" -v off_ip="$OFF_IP" -v W="$WORK" '
BEGIN {
  srand(7)
  split("s s s s h h h c c c p p p l l d d m m x", pf, " ")
  split("192.0.2 198.51.100 203.0.113", tn, " ")
  for (i = 1; i <= n; i++) {
    p = pf[int(rand() * 20) + 1]
    if (i == 5) p = "s"
    c[p]++
    nm = sprintf("%s%04d", p, c[p])
    r = rand() * 100
    st = (r < 55) ? "installing" : (r < 88) ? "done" : (r < 90) ? "disk" : (r < 93) ? "pingx" : "done"
    if (i == 5) st = "done"
    print nm > (W "/hosts.txt")
    cnt[st]++
    if (st == "pingx") {
      k = ++px
      if (k % 50 != 0) print tn[int((k - 1) / 254) % 3 + 1] "." ((k - 1) % 254 + 1) " " nm > (W "/hosts.block")
      continue
    }
    if (i == 5) ip = off_ip
    else ip = "127.78." int((i - 1) / 250) "." ((i - 1) % 250 + 1)
    print ip " " nm > (W "/hosts.block")
    print nm " " st > (W "/state")
    if (st == "done") { d++; if (d % 500 == 7) lf = lf " " nm; if (d % 700 == 11) ht = ht " " nm; if (d % 600 == 13) dn = dn " " nm; if (d % 900 == 17) ap = ap " " nm }
  }
  printf "export STUB_LDAP_FAIL=\"%s\" STUB_HT_OFF=\"%s\" STUB_DNS_DIFF=\"%s\" STUB_APPL_DIFF=\"%s\"\n", lf, ht, dn, ap > (W "/demo.env")
  printf "설치중 %d대, 디스크미인식 %d대, 설치완료 %d대, ping X %d대", cnt["installing"], cnt["disk"], cnt["done"], cnt["pingx"] > (W "/summary.txt")
}'
SUMMARY="$(cat "$WORK/summary.txt")"
{
  echo "$MARK_BEGIN"
  cat "$WORK/hosts.block"
  echo "$MARK_END"
} >> /etc/hosts
ip addr add "$OFF_IP/32" dev lo
cp "$WORK/state" "$WORK/state.orig"
else

#  벤더: s=sds, h·c=D, p=L, l=j, d=t, m=W, 그 외=기타
ALL="s001 s002 s003 s004 s005 s006 h001 h002 h003 h004 h005 h006 c001 c002 c003 p001 p002 p003 p004 l001 l002 l003 d001 d002 m001 x001"
DOWN="h006 l003 m002x"       # ping X (m002x 는 없는 이름: 이름 해석 실패도 ping X)
ALL="$ALL m002x"
printf '%s\n' $ALL > "$WORK/hosts.txt"

{
  echo "$MARK_BEGIN"
  n=1
  for h in $ALL; do
    case " $DOWN " in *" $h "*) [ "$h" != m002x ] && echo "192.0.2.$n $h" ;;
      *) if [ "$h" = "$OFF_HOST" ]; then echo "$OFF_IP $h"; else echo "127.77.0.$n $h"; fi ;;
    esac
    n=$((n + 1))
  done
  echo "$MARK_END"
} >> /etc/hosts
ip addr add "$OFF_IP/32" dev lo

# ---- 가짜 gossh 상태 (done | installing | disk) ----
cat > "$WORK/state" <<'EOF'
s001 done
s002 done
s003 installing
s004 disk
s005 done
s006 installing
h001 done
h002 installing
h003 installing
h004 disk
h005 installing
c001 done
c002 installing
c003 installing
p001 done
p002 done
p003 installing
p004 installing
l001 installing
l002 done
d001 done
d002 installing
m001 installing
x001 installing
EOF
fi

cp "$TOP/test/stub_gossh.sh" "$WORK/bin/gossh"
cat > "$WORK/bin/ssh" <<'EOF'
#!/bin/bash
# 가짜 ssh: DHCP 서버의 a.sh 대신 등록 여부 예시를 출력한다(데모용 형식)
echo "[데모] DHCP 서버 a.sh 출력 예시 (실제 형식은 현장 a.sh 를 따름)"
printf '%-10s %-19s %s\n' HOSTNAME MAC DHCP
while read -r h; do
  [ -z "$h" ] && continue
  case "$h" in
    h*) printf '%-10s %-19s %s\n' "$h" "-" "미등록" ;;
    *)  printf '%-10s %-19s %s\n' "$h" "52:54:00:12:34:$(printf %02x ${#h})" "등록" ;;
  esac
done
EOF
chmod +x "$WORK/bin/gossh" "$WORK/bin/ssh"

cat > "$WORK/pm_monitor.conf" <<EOF
# 데모 설정 (demo.sh 가 만든 것)
gossh=$WORK/bin/gossh
interval_sec=$INTERVAL
delay_min=1
dhcp_server=dhcp-demo
ldap_check_sh=/user/demo/ldap_check.sh
ping_timeout_ms=1000
EOF

export STUB_GOSSH_STATE="$WORK/state"
export STUB_LDAP_ONELINE=1          # 현재 ldap_check.sh 처럼 한 줄 요약
if [ "$N" -gt 27 ]; then
  . "$WORK/demo.env"                # LDAP FAIL·H/T OFF·DNS 다름·autofs 다름 예시(완료 서버 중 몇 대)
else
  export STUB_LDAP_FAIL="p002"      # LDAP FAIL 예시
  export STUB_HT_OFF="d001"         # H/T OFF 예시
fi
export PATH="$WORK/bin:$PATH"       # d 키의 ssh 를 가짜 ssh 로

# ---- 시나리오 (백그라운드) ----
(
  log() { echo "$(date +%T) $*" >> "$WORK/scenario.log"; }
  set_state() { sed -i "s/^$1 .*/$1 $2/" "$WORK/state"; }
  log "시작: $SUMMARY"
  sleep 30
  if [ "$N" -gt 27 ]; then
    awk '$2 == "installing" { if (++k % 10 == 0) $2 = "done" } { print }' "$WORK/state" > "$WORK/state.new" && mv "$WORK/state.new" "$WORK/state"
    log "설치중 서버 10대 중 1대가 설치완료"
  else
    for h in s003 h002 c002; do set_state "$h" done; done
    log "s003 h002 c002 설치완료"
  fi
  sleep 60
  ip addr del "$OFF_IP/32" dev lo 2>/dev/null
  log "$OFF_HOST 꺼짐 -> c 를 누르면 완료후꺼짐"
  sleep 60
  ip addr add "$OFF_IP/32" dev lo 2>/dev/null
  log "$OFF_HOST 다시 켜짐 -> c 를 누르면 재부팅됨"
) &
SCEN_PID=$!

cat <<EOF

 ===== install_mn 데모 =====
 호스트 ${N}대(가짜). 실제 서버에는 접속하지 않습니다. ${INTERVAL}초마다 갱신합니다.
 구성: $SUMMARY

 시간표 (시작 기준)
   0초    설치중·디스크미인식·설치완료·ping X 가 보입니다
   30초   설치중 서버 일부가 설치완료로 바뀝니다 (27대: s003 h002 c002)
   60초   남은 설치중 서버가 '지연'으로 바뀝니다 (delay_min=1)
   90초   $OFF_HOST 가 꺼집니다   -> c 를 누르면 '완료후꺼짐'
   150초  $OFF_HOST 가 다시 켜집니다 -> c 를 두 번(닫고 열기) 누르면 '재부팅됨'

 키
   c  설치현황 코멘트      r  지연 서버 점검요청 코멘트
   d  DHCP 조회(ping X)    l  LDAP·H/T 점검(완료 서버, FAIL·OFF·DNS·autofs 다름 예시 포함. 27대: p002 FAIL·d001 OFF)
   /  hostname 검색        1~7 상태 필터, 0 해제
   ↑↓ PgUp PgDn 스크롤     같은 키를 다시 누르면 닫힘, q 종료

 종료하면 /etc/hosts, lo 주소, $WORK 를 원래대로 되돌립니다.
EOF
if [ "$YES" = "0" ]; then
  printf '\n Enter 를 누르면 시작합니다...'
  read -r _
fi

cd "$WORK" && "$BIN" hosts.txt
rc=$?
echo "데모 종료 (종료 코드 $rc). 정리했습니다."
exit $rc
