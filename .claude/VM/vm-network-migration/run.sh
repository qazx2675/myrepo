#!/usr/bin/env bash
# run.sh — vm-network-migration 실행 편의 래퍼.
# 설정 파일/자격증명 확인 -> (필요 시) 빌드 -> 실행 순서로 진행합니다.
# 이 스크립트에 넘긴 인자는 그대로 Go 바이너리에 전달됩니다.
#
#   사전 준비)
#     export VC_USER='administrator@vsphere.local'
#     read -rs -p 'vCenter 비밀번호: ' VC_PASS; export VC_PASS; echo
#
#   사용 예)
#     ./run.sh -to-portgroup=PG-NEW-100 -dry-run
#     ./run.sh -to-portgroup=PG-NEW-100 -from-portgroup=PG-OLD-010 -concurrency=8
#     ./run.sh -rollback=rollback_20260902_143000.csv
set -euo pipefail
cd "$(dirname "$0")"

BIN="./vm-network-migration"
VCENTER_FILE="vcenter.txt"
VM_FILE="vmlist.txt"

echo "========================================================="
echo "  VM 네트워크 포트그룹 이관 도구 (vm-network-migration)"
echo "========================================================="

# 1) 자격증명 확인 (파일이 아니라 환경 변수로 받습니다)
if [[ -z "${VC_USER:-}${VCENTER_USER:-}" || -z "${VC_PASS:-}${VCENTER_PASS:-}" ]]; then
    echo "오류: vCenter 계정 환경 변수가 없습니다." >&2
    echo "      아래처럼 설정한 뒤 다시 실행하세요(비밀번호는 화면에 표시되지 않습니다):" >&2
    echo "        export VC_USER='administrator@vsphere.local'" >&2
    echo "        read -rs -p 'vCenter 비밀번호: ' VC_PASS; export VC_PASS; echo" >&2
    exit 1
fi

# 2) 목록 파일 확인 (인자로 다른 경로를 준 경우는 Go 쪽에서 다시 검사합니다)
if [[ "$*" != *"-vcenter-file"* && ! -f "$VCENTER_FILE" ]]; then
    echo "오류: vCenter 목록 파일이 없습니다. ($VCENTER_FILE)" >&2
    echo "      cp vcenter.txt.example vcenter.txt 후 주소를 한 줄에 하나씩 적으세요." >&2
    exit 1
fi
if [[ "$*" != *"-rollback"* && "$*" != *"-vm-file"* && ! -f "$VM_FILE" ]]; then
    echo "오류: 대상 VM 목록 파일이 없습니다. ($VM_FILE)" >&2
    echo "      vmlist.txt.example 을 참고해서 만드세요." >&2
    exit 1
fi

# 3) 빌드 (바이너리가 없거나 소스가 더 최신이면 다시 빌드)
NEED_BUILD=0
if [[ ! -x "$BIN" ]]; then
    NEED_BUILD=1
else
    for f in ./*.go; do
        [[ "$f" -nt "$BIN" ]] && NEED_BUILD=1 && break
    done
fi

if [[ "$NEED_BUILD" -eq 1 ]]; then
    if command -v go >/dev/null 2>&1; then
        echo "소스가 변경되었습니다. 오프라인 빌드를 진행합니다..."
        bash setup.sh
    elif [[ -x "$BIN" ]]; then
        echo "경고: go 가 없어 기존 바이너리를 그대로 사용합니다."
    else
        echo "오류: go 도 없고 빌드된 바이너리도 없습니다. Go 를 설치하세요." >&2
        exit 1
    fi
fi

# 4) 실행
echo "---------------------------------------------------------"
set +e
"$BIN" "$@"
RC=$?
set -e

# 5) 마무리 안내
echo "---------------------------------------------------------"
if [[ $RC -ne 0 ]]; then
    echo "일부 VM 처리에 실패했습니다. 위 결과와 report_*.csv 를 확인하세요. (exit=$RC)"
else
    echo "모든 대상 처리가 정상 종료되었습니다."
fi
echo
echo "[반드시 확인] 이 도구의 검증은 vCenter 가 보고하는 값에 기반합니다."
echo "             변경된 VM 중 무작위로 몇 대를 직접 골라, vCenter UI 또는 게스트 접속으로"
echo "             실제 포트그룹과 통신 상태가 의도대로 바뀌었는지 눈으로 확인하세요."
exit $RC
