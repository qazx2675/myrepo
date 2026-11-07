#!/bin/bash
# stub_gossh.sh - 가짜 gossh (실제 gossh -script 출력 형식을 그대로 흉내냄)
#
# 환경변수
#   STUB_GOSSH_STATE  "호스트 상태" 줄 파일. 상태: done | installing | disk | down (없는 호스트는 down)
#   STUB_GOSSH_NICE   (선택) 이 파일에 자기 nice 값을 덧붙임
#   STUB_GOSSH_LOCK   (선택) 이 폴더를 mkdir 로 잡고 실행(이미 있으면 $STUB_GOSSH_LOCK.overlap 파일 생성)
#   STUB_GOSSH_SLEEP  (선택) 동시 실행 검사용 지연 초
#
# 출력 (실제 gossh 와 같음)
#   -pm : done       -> stdout "host: PM_OK"
#         installing -> stderr "host: OS 설치중 (~/.profile anaconda 감지)" + <목록>_os_install 파일
#         disk       -> (installing 과 같음)
#         down       -> stderr "host: SSH 접속 실패: ..." + <목록>_res_off 파일
#   -pm 없음 + 명령에 NS_DISK 가 있으면(디스크 확인):
#         done       -> stdout "host: NS_NOLOG" "host: NS_DONE"
#         installing -> stdout "host: NS_DONE"
#         disk       -> stdout "host: NS_DISK" "host: NS_DONE"
#         down       -> stderr 접속 실패
#   명령에 ldap_check 가 있으면(LDAP 점검, 상태 done/installing/disk 모두 응답):
#         파일별 OK 줄 7개(README 형식) + "host: Thread(s) per core:    2"
#         STUB_LDAP_FAIL    공백 구분 호스트: autofs.conf 줄을 FAIL 로
#         STUB_LDAP_UNKNOWN 공백 구분 호스트: /unknown infra-mismatch 줄
#         STUB_LDAP_ONELINE 1 이면 현재 ldap_check.sh 처럼 한 줄 요약(INFO LDAP infra site)
#         STUB_HT_OFF       공백 구분 호스트: Thread(s) per core 를 1 로
#   STUB_GOSSH_LOG (선택) 이 파일에 호출마다 명령 줄(인자)을 한 줄 덧붙임
# 호스트 5천 대도 awk 한 번으로 처리한다(호스트별 프로세스 없음).
[ -n "$STUB_GOSSH_LOG" ] && echo "$*" >> "$STUB_GOSSH_LOG"
pm=0
list=""
cmd=""
while [ $# -gt 0 ]; do
  case "$1" in
    -pm) pm=1 ;;
    -w) shift; list="${1#^}" ;;
    -t|-c|-cf|-u|-p|-i|-P) shift ;;
    -*) ;;
    *) cmd="$cmd${cmd:+ }$1" ;;
  esac
  shift
done

[ -n "$STUB_GOSSH_NICE" ] && nice >> "$STUB_GOSSH_NICE"
if [ -n "$STUB_GOSSH_LOCK" ]; then
  mkdir "$STUB_GOSSH_LOCK" 2>/dev/null || : > "$STUB_GOSSH_LOCK.overlap"
  [ -n "$STUB_GOSSH_SLEEP" ] && sleep "$STUB_GOSSH_SLEEP"
  trap 'rmdir "$STUB_GOSSH_LOCK" 2>/dev/null' EXIT
fi

disk=0
case "$cmd" in *NS_DISK*) disk=1 ;; esac
ldap=0
case "$cmd" in *ldap_check*) ldap=1 ;; esac

awk -v pm="$pm" -v disk="$disk" -v ldap="$ldap" -v lfail=" $STUB_LDAP_FAIL " -v lunk=" $STUB_LDAP_UNKNOWN " -v lone="${STUB_LDAP_ONELINE:-0}" -v htoff=" $STUB_HT_OFF " -v inst="${list}_os_install" -v off="${list}_res_off" '
NR == FNR { st[$1] = $2; next }
{
  h = $1
  if (h == "") next
  s = (h in st) ? st[h] : "down"
  if (s == "down") {
    print h ": SSH 접속 실패: dial tcp " h ":22: i/o timeout" > "/dev/stderr"
    if (pm) print h > off
    next
  }
  if (ldap) {
    key = " " h " "
    if (index(lunk, key)) {
      print h ": FAIL\t/unknown\tinfra-mismatch\tdns+ntp=zxcv ldap=zxcv appl=qwer"
      print h ": FAIL\t/unknown\tresolv.conf"
    } else if (lone == "1") {
      if (index(lfail, key)) print h ": ERROR: FAIL\tLDAP\tUNDEFINED"
      else print h ": INFO\tLDAP\tzxcv\ta1"
    } else {
      print h ": OK\t/zxcv\tresolv.conf"
      print h ": OK\t/zxcv\tchrony.conf\tchrony"
      print h ": OK\t/zxcv\tldap.conf\ta1"
      print h ": " (index(lfail, key) ? "FAIL" : "OK") "\t/zxcv\tautofs.conf"
      print h ": OK\t/zxcv\tautofs_ldap_auth.conf"
      print h ": OK\t/zxcv\tsssd.conf"
      print h ": OK\t/zxcv\tauto.appl\ta1"
    }
    print h ": Thread(s) per core:    " (index(htoff, key) ? "1" : "2")
    next
  }
  if (pm) {
    if (s == "done") print h ": PM_OK"
    else { print h ": OS 설치중 (~/.profile anaconda 감지)" > "/dev/stderr"; print h > inst }
  } else if (disk) {
    if (s == "done") print h ": NS_NOLOG"
    if (s == "disk") print h ": NS_DISK"
    print h ": NS_DONE"
  } else {
    print h ": ok"
  }
}' "${STUB_GOSSH_STATE:-/dev/null}" "$list"
exit 0
