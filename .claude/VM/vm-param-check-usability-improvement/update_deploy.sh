#!/bin/bash
# update_deploy.sh — vm-param-check-usability-improvement/vm-param-check를 원격 배포 경로에
# 최신 상태로 갱신하는 스크립트.
#
# 이 스크립트는 "내 test 환경"이 아니라 실제로 도구를 써야 하는 다른 서버에서 실행하는 것을
# 전제로 한다. 그 서버에서 이 저장소를 매번 통째로 새로 clone/checkout할 필요 없이,
# public GitHub 저장소에서 이 도구가 있는 하위 디렉토리만 받아와 배포 경로에 반영하고
# 재빌드까지 마친다.
#
# 기본 배포 경로: /root/vm-param-check-usability-improvement/vm-param-check
# (원본 저장소 안 경로 .claude/VM/vm-param-check-usability-improvement/vm-param-check 와는
#  다르다 — 배포 경로는 .claude/VM 접두어 없이 도구만 평평하게 놓인 구조다)
#
# 사용법:
#   bash update_deploy.sh                              # 기본 경로에 배포
#   bash update_deploy.sh /다른/배포/경로/vm-param-check   # 배포 경로 직접 지정
#
# 동작:
#   1) GitHub public 저장소를 임시 디렉토리로 clone (인증 불필요)
#   2) 배포 경로가 이미 있으면 타임스탬프 백업 디렉토리로 이동(보존) — 덮어써서 사라지는 일 없음
#   3) 저장소 안의 vm-param-check 소스 전체를 배포 경로로 복사
#   4) 배포 경로에서 go build -mod=vendor로 재빌드 (오프라인 가능 — vendor/ 포함)
#   5) 임시 clone 디렉토리 정리
#
# 실패 시 그 자리에서 즉시 중단한다(set -e) — 배포 경로가 절반만 갱신된 채로 남는 것을
# 막기 위해 3)번(복사)은 임시 디렉토리에 전부 준비한 뒤 한 번에 옮기는 방식을 쓴다.

set -euo pipefail

REPO_URL="https://github.com/qazx2675/myrepo.git"
REPO_SUBPATH=".claude/VM/vm-param-check-usability-improvement/vm-param-check"
DEFAULT_DEPLOY_PATH="/root/vm-param-check-usability-improvement/vm-param-check"

DEPLOY_PATH="${1:-$DEFAULT_DEPLOY_PATH}"
TMP_CLONE_DIR="$(mktemp -d /tmp/vm-param-check-update.XXXXXX)"

cleanup() {
    rm -rf "$TMP_CLONE_DIR"
}
trap cleanup EXIT

echo "[1/4] 저장소 clone 중... ($REPO_URL)"
git clone --depth 1 -q "$REPO_URL" "$TMP_CLONE_DIR/repo"

SRC_DIR="$TMP_CLONE_DIR/repo/$REPO_SUBPATH"
if [ ! -d "$SRC_DIR" ]; then
    echo "[오류] 저장소 안에서 $REPO_SUBPATH 를 찾을 수 없습니다 — 저장소 구조가 바뀌었을 수 있습니다." >&2
    exit 1
fi
if [ ! -f "$SRC_DIR/main.go" ] || [ ! -f "$SRC_DIR/go.mod" ]; then
    echo "[오류] $SRC_DIR 안에 main.go 또는 go.mod가 없습니다 — 배포를 중단합니다." >&2
    exit 1
fi

echo "[2/4] 배포 경로 준비 중... ($DEPLOY_PATH)"
mkdir -p "$(dirname "$DEPLOY_PATH")"
if [ -e "$DEPLOY_PATH" ]; then
    BACKUP_PATH="${DEPLOY_PATH}.bak.$(date +%Y%m%d%H%M%S)"
    echo "  기존 배포본을 백업합니다: $BACKUP_PATH"
    mv "$DEPLOY_PATH" "$BACKUP_PATH"
fi

echo "[3/4] 소스 복사 중..."
cp -r "$SRC_DIR" "$DEPLOY_PATH"

echo "[4/4] 재빌드 중... (go build -mod=vendor, 오프라인 가능)"
(
    cd "$DEPLOY_PATH"
    go build -mod=vendor -o vm-param-check .
)

echo
echo "완료. 배포 경로: $DEPLOY_PATH"
echo "  실행: cd $DEPLOY_PATH && ./vm-param-check -demo"
if [ -n "${BACKUP_PATH:-}" ]; then
    echo "  이전 배포본은 다음 경로에 그대로 남아 있습니다: $BACKUP_PATH (문제 없으면 직접 삭제하세요)"
fi
