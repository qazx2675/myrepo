#!/bin/bash
# make_update_package.sh — 폐쇄망으로 가져갈 "업데이트 패키지"를 만든다.
# 인터넷이 되는 빌드 서버(Go 설치됨)에서, 이 저장소를 받은 상태로 실행한다.
#
# 결과: dist/vm-param-check-update-YYYYMMDD.tar.gz
#   update.sh        폐쇄망 서버에서 실행하는 업데이트 스크립트
#   payload/         바뀐 파일만 (지금은 vm-param-check 실행파일)
#   SHA256SUMS       전송 중 깨졌는지 확인용
#   VERSION.txt      어느 소스로 언제 만든 빌드인지
#
# 폐쇄망 서버에서는:
#   tar xzf vm-param-check-update-YYYYMMDD.tar.gz
#   cd vm-param-check-update-YYYYMMDD
#   bash update.sh "/내가/사용중인/디렉토리"
#
# 실행파일은 정적 빌드(CGO_ENABLED=0, linux/amd64)라서 서버의 glibc 버전이 달라도 돌아간다.
# 다른 CPU(arm64 등)면 GOARCH 환경변수로 바꿔서 실행한다: GOARCH=arm64 bash make_update_package.sh
#
# 빌드는 vm-param-check/setup.sh 를 그대로 쓴다(vendor 처리는 setup.sh 가 알아서 함).
# 파일을 더 바꿔야 하면 payload/ 에 같은 상대경로로 넣으면 update.sh 가 다른 것만 반영한다.

set -euo pipefail
cd "$(dirname "$0")"

die() {
    echo "[오류] $*" >&2
    exit 1
}

command -v go >/dev/null 2>&1 || die "go 가 없습니다. 빌드 서버에서 실행하세요."
[ -f vm-param-check/setup.sh ] || die "vm-param-check/setup.sh 가 없습니다. 프로젝트 폴더 안에서 실행하세요."
[ -f update.sh ] || die "update.sh 가 없습니다."

TARGET_ARCH="${GOARCH:-amd64}"
NAME="vm-param-check-update-$(date +%Y%m%d)"
STAGE="dist/$NAME"
rm -rf "$STAGE"
mkdir -p "$STAGE/payload"

echo "[1/3] 빌드 중... (linux/$TARGET_ARCH, 정적, 오프라인)"
CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_ARCH" GOPROXY=off bash vm-param-check/setup.sh
cp vm-param-check/vm-param-check "$STAGE/payload/vm-param-check"
chmod 755 "$STAGE/payload/vm-param-check"
cp update.sh "$STAGE/update.sh"

echo "[2/3] 버전 정보와 체크섬 만드는 중..."
SRC_COMMIT="${SOURCE_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo 알수없음)}"
{
    echo "빌드 시각 : $(date '+%Y-%m-%d %H:%M:%S')"
    echo "소스 커밋 : $SRC_COMMIT"
    echo "대상      : linux/$TARGET_ARCH (정적 빌드)"
} > "$STAGE/VERSION.txt"
(cd "$STAGE" && sha256sum update.sh payload/vm-param-check VERSION.txt > SHA256SUMS)

echo "[3/3] 압축 중..."
tar -C dist -czf "dist/$NAME.tar.gz" "$NAME"

echo
echo "완료: $(pwd)/dist/$NAME.tar.gz ($(du -h "dist/$NAME.tar.gz" | cut -f1))"
echo "  이 파일을 USB/nfs 로 폐쇄망에 가져가서:"
echo "    tar xzf $NAME.tar.gz && cd $NAME && bash update.sh \"/내가/사용중인/디렉토리\""
