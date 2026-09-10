#!/usr/bin/env bash
# setup.sh — 통합 스크립트 준비 (폐쇄망 오프라인 빌드)
#
# 세 프로젝트(ip_change / ldap_setting / vm-network-migration)의 소스를
# ./projects/ 아래에 자체 보관합니다(다른 부서로 이 폴더만 단독 이관되므로
# myrepo 의 다른 위치를 상대경로로 참조하지 않음). 각자의 위치에서 빌드하고,
# 나온 바이너리를 bin/ 으로 모읍니다. 이 폴더 하나만 있으면 인터넷 없이
# 빌드됩니다.
set -euo pipefail
cd "$(dirname "$0")"
ROOT="$(pwd)"

command -v go >/dev/null 2>&1 || { echo "오류: go 가 없습니다. Go 툴체인을 설치하세요." >&2; exit 2; }

IP_DIR="./projects/ip_change"
LDAP_DIR="./projects/ldap_setting"
NM_DIR="./projects/vm-network-migration"

for d in "$IP_DIR" "$LDAP_DIR" "$NM_DIR"; do
  [ -d "$d" ] || { echo "오류: $d 가 없습니다. 이 폴더(Network_Change_Integration_Script) 를 통째로 내려받았는지 확인하세요." >&2; exit 2; }
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
echo "  1) integration.conf.sample → integration.conf 복사 후 값 채우기 (특히 vc_id)"
echo "     ldap_infra 는 conf 에 두지 않고 실행 시 --infra 로 지정합니다."
echo "  2) conf/ldap_config.conf.sample → conf/ldap_config.conf,"
echo "     conf/assets.txt.sample → conf/assets.txt 로 복사 후 값 채우기"
echo "     (이 폴더가 다른 곳으로 이관돼도 독립적으로 동작하도록 자체 보관합니다."
echo "      원본 ldap_setting 설정이 바뀌면 이 사본도 함께 갱신하십시오.)"
echo "  3) conf/ip_change.conf 는 실행 시 자동 렌더링됩니다 (직접 안 만들어도 됨)"
echo "  4) lib/common.sh 의 user선택() 함수 채우기 (또는 항상 -u 로 지정)"
echo "  5) <계정>.txt (내용: 'VM이름 변경될IP'), vswitch_<계정>.txt 준비"
echo "  6) vcenter.txt 준비 (포트그룹 단계용, 한 줄에 vCenter 주소 하나)"
echo "  7) export GOSSH_PW=...  ;  export VC_PASSWORD=..."
echo "  8) ./change.sh <계정>"
echo
echo "참고: OS6(RHEL/CentOS 6) 대상이 있다면 bin_os6/ 에 Go 1.20 으로 미리"
echo "      빌드해 둔 ip-change-engine/ldap-config-engine 이 있습니다"
echo "      (integration.conf 의 os6_hostgroup 채우면 사용, README 3.4 참고)."
