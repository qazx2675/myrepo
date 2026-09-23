#!/usr/bin/env bash
# install.sh — 192.168.0.58(록키)에 saccae 검증 환경을 만든다. root 로 실행, 여러 번 실행해도 된다.
#   1) /etc/hosts 에 *.saccae.com 이름 등록 (hosts.saccae 블록)
#   2) V2 브랜치를 $DEST(/home/saccae) 에 받아 오프라인 빌드 (이미 있으면 git pull)
#   3) 검증용 SPEC_DIR 스펙 3개 + BM 목록(saccae/vm06/vm08/vm20) + vcenter.txt + 사용법.txt 복사 (같은 이름 파일은 덮어씀)
#   4) 가상 vCenter(vcsim) 빌드 후 (재)시작 — 호스트 hostname0000~0099.saccae.com
set -euo pipefail
SRC="$(cd "$(dirname "$0")" && pwd)"; VERIFY="$(dirname "$SRC")"
DEST="${DEST:-/home/saccae}"; BRANCH="${BRANCH:-V2}"; REPO="${REPO:-https://github.com/qazx2675/myrepo.git}"
VENDOR="${VCSIM_VENDOR:-$VERIFY/../../공통/govendor/govmomi-0.55.1-vcsim}"
[ "$(id -u)" = 0 ] || { echo "root 로 실행하세요."; exit 1; }

echo "== 1) /etc/hosts"
sed -i '/^# >>> saccae 검증 환경/,/^# <<< saccae 검증 환경/d' /etc/hosts
cat "$SRC/hosts.saccae" >> /etc/hosts
getent hosts vcsim.saccae.com vcenter.saccae.com

echo "== 2) V2 코드 ($DEST, $BRANCH 브랜치)"
if [ -d "$DEST/.git" ]; then git -C "$DEST" pull --ff-only
else git clone -b "$BRANCH" --single-branch "$REPO" "$DEST"; fi
(cd "$DEST" && ./setup.sh)

echo "== 3) 검증 데이터"
cp -r "$SRC/SPEC_DIR/." "$DEST/SPEC_DIR/"
cp "$SRC"/VMsetup/*.txt "$DEST/VMsetup/"
cp "$SRC/vcenter.txt" "$SRC/vcenter_vcsim.txt" "$DEST/"
cp "$SRC/사용법.txt" "$DEST/사용법.txt"
ls "$DEST/SPEC_DIR"

echo "== 4) 가상 vCenter (vcsim)"
mkdir -p "$DEST/testenv/bin"
[ -d "$VENDOR" ] || { echo "vcsim 의존성 폴더가 없습니다: $VENDOR (VCSIM_VENDOR 로 지정 가능)"; exit 1; }
(cd "$VERIFY" && ln -sfn "$VENDOR" vendor && go build -mod=vendor -o "$DEST/testenv/bin/vcsimenv" ./cmd/vcsimenv)
cp "$SRC/vcsim.sh" "$DEST/testenv/vcsim.sh"; chmod +x "$DEST/testenv/vcsim.sh"
"$DEST/testenv/vcsim.sh" restart

echo; echo "완료 — 사용법: $DEST/사용법.txt"
