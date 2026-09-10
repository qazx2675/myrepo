#!/usr/bin/env bash
# setup.sh — 통합 스크립트 준비 (폐쇄망 오프라인 빌드)
#
# 세 프로젝트를 각자의 위치에서 빌드하고, 나온 바이너리를 bin/ 으로 모읍니다.
# 세 프로젝트는 같은 저장소(myrepo) 안에 있으므로, 저장소를 통째로 내려받았다면
# 인터넷 없이 빌드됩니다.
set -euo pipefail
cd "$(dirname "$0")"
ROOT="$(pwd)"

command -v go >/dev/null 2>&1 || { echo "오류: go 가 없습니다. Go 툴체인을 설치하세요." >&2; exit 2; }

IP_DIR="../../HPC/ip_change"
LDAP_DIR="../../HPC/ldap_setting"
NM_DIR="../vm-network-migration"

for d in "$IP_DIR" "$LDAP_DIR" "$NM_DIR"; do
  [ -d "$d" ] || { echo "오류: $d 가 없습니다. myrepo 를 통째로 내려받았는지 확인하세요." >&2; exit 2; }
done

mkdir -p bin

echo ">> ip_change 빌드"
( cd "$IP_DIR" && ./setup.sh >/dev/null && cp -f bin/ip-change-engine "$ROOT/bin/" )
echo "   -> bin/ip-change-engine"

echo ">> ldap_setting 빌드"
( cd "$LDAP_DIR" && ./setup.sh >/dev/null && cp -f bin/ldap-config-engine "$ROOT/bin/" )
echo "   -> bin/ldap-config-engine"

echo ">> vm-network-migration 빌드"
( cd "$NM_DIR" && ./setup.sh >/dev/null )
echo "   (nm-* 바이너리는 $NM_DIR/bin 에 그대로 두고 run.sh 가 씁니다)"

echo
echo "빌드 완료. 다음으로 진행하십시오:"
echo "  1) integration.conf.sample → integration.conf 복사 후 값 채우기"
echo "     (특히 ldap_conf, ldap_assets, vc_id — ldap_infra 는 실행 시 --infra 로 지정)"
echo "  2) conf/ip_change.conf 는 실행 시 자동 렌더링됩니다 (직접 안 만들어도 됨)"
echo "  3) lib/common.sh 의 user선택() 함수 채우기 (또는 항상 -u 로 지정)"
echo "  4) <계정>.txt (내용: 'VM이름 변경될IP'), vswitch_<계정>.txt 준비"
echo "  5) vcenter.txt 준비 (포트그룹 단계용, 한 줄에 vCenter 주소 하나)"
echo "  6) export GOSSH_PW=...  ;  export VC_PASSWORD=..."
echo "  7) ./change.sh <계정>"
