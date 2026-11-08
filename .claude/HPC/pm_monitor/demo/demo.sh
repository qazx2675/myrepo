#!/bin/bash
# demo.sh - install_mn 데모 환경을 한 번에 만들고 실행한다 (랩·연습용, 실제 서버에 접속하지 않음)
#
# 사용법:  sudo bash demo/demo.sh        (root 필요: raw ICMP, /etc/hosts, lo 주소)
#          bash demo/demo.sh -y          (안내 화면에서 Enter 기다리지 않음)
#
# 하는 일
#   1. /tmp/pm_demo 에 가짜 gossh·ssh, 설정 파일, 호스트 파일(26대)을 만든다.
#   2. /etc/hosts 에 데모 호스트 이름을 잠깐 넣는다(127.77.x = ping O, 192.0.2.x = ping X).
#   3. 시나리오를 백그라운드로 돌려 시간에 따라 상태를 바꾼다(설치완료·지연·완료후꺼짐·재부팅됨).
#   4. install_mn 을 실행한다. 종료(q)하면 /etc/hosts·lo 주소·임시 폴더를 모두 원래대로 되돌린다.
set -u

HERE="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
TOP="$(cd "$HERE/.." && pwd)"
WORK=/tmp/pm_demo
MARK_BEGIN="# pm_demo BEGIN"
MARK_END="# pm_demo END"
OFF_HOST=s005          # 완료 후 꺼졌다가 다시 켜지는 서버
OFF_IP=172.31.77.5
YES=0
[ "${1:-}" = "-y" ] && YES=1

if [ "$(id -u)" != "0" ]; then
  echo "root 로 실행하세요 (raw ICMP, /etc/hosts, lo 주소 변경이 필요합니다)." >&2
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

# ---- 호스트 ----
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
interval_sec=10
delay_min=1
dhcp_server=dhcp-demo
ldap_check_sh=/user/demo/ldap_check.sh
ping_timeout_ms=1000
EOF

export STUB_GOSSH_STATE="$WORK/state"
export STUB_LDAP_ONELINE=1          # 현재 ldap_check.sh 처럼 한 줄 요약
export STUB_LDAP_FAIL="p002"        # LDAP FAIL 예시
export STUB_HT_OFF="d001"           # H/T OFF 예시
export PATH="$WORK/bin:$PATH"       # d 키의 ssh 를 가짜 ssh 로

# ---- 시나리오 (백그라운드) ----
(
  log() { echo "$(date +%T) $*" >> "$WORK/scenario.log"; }
  set_state() { sed -i "s/^$1 .*/$1 $2/" "$WORK/state"; }
  log "시작: 설치중 13대, 디스크미인식 2대, 설치완료 9대, ping X 3대"
  sleep 30
  for h in s003 h002 c002; do set_state "$h" done; done
  log "s003 h002 c002 설치완료"
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
 호스트 27대(가짜). 실제 서버에는 접속하지 않습니다. 10초마다 갱신합니다.

 시간표 (시작 기준)
   0초    설치중·디스크미인식·설치완료·ping X 가 보입니다
   30초   s003 h002 c002 가 설치완료로 바뀝니다
   60초   남은 설치중 서버가 '지연'으로 바뀝니다 (delay_min=1)
   90초   $OFF_HOST 가 꺼집니다   -> c 를 누르면 '완료후꺼짐'
   150초  $OFF_HOST 가 다시 켜집니다 -> c 를 두 번(닫고 열기) 누르면 '재부팅됨'

 키
   c  설치현황 코멘트      r  지연 서버 점검요청 코멘트
   d  DHCP 조회(ping X)    l  LDAP·H/T 점검(완료 서버, p002 FAIL·d001 OFF 예시)
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
