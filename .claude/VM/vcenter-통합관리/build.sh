#!/usr/bin/env bash
# ./cmd/* 를 windows/amd64, linux/amd64 로 빌드하고, 공유폴더에 그대로 복사할 dist/vc-portal/ 을 만든다.
# (vendor 사용, 오프라인 가능)
set -euo pipefail
cd "$(dirname "$0")"

built=0
for d in ./cmd/*/; do
  [ -d "$d" ] || continue
  name=$(basename "$d")
  mkdir -p bin/windows bin/linux
  # 런처(vcportal)는 링크 클릭 시 콘솔 창이 뜨지 않도록 GUI 서브시스템으로 빌드한다.
  ldflags=""
  [ "$name" = "vcportal" ] && ldflags="-H windowsgui"
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -mod=vendor -trimpath -ldflags "$ldflags" -o "bin/windows/${name}.exe" "./cmd/${name}"
  echo "built bin/windows/${name}.exe"
  GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -mod=vendor -trimpath -o "bin/linux/${name}" "./cmd/${name}"
  echo "built bin/linux/${name}"
  built=$((built+1))
done
echo "완료: ${built}개 명령 빌드"

# ---- 배포 패키지: dist/vc-portal/ ----
out=dist/vc-portal
rm -rf "$out"
mkdir -p "$out/data" "$out/config" "$out/launcher" "$out/collector"
cp web/index.html "$out/"
cp -r web/assets "$out/assets"
cp bin/windows/vcportal.exe "$out/launcher/"
cp bin/windows/vcportal-collector.exe "$out/collector/"

# .ps1 / conf 예제는 Windows PowerShell 5.1·메모장이 한글을 읽도록 UTF-8 BOM + CRLF 로 맞춘다.
crlf_bom() {
  { printf '\xef\xbb\xbf'; sed '$a\' "$1" | sed -e '1s/^\xef\xbb\xbf//' -e 's/\r$//' -e 's/$/\r/'; } > "$2"
}
crlf_bom scripts/install-launcher.ps1 "$out/launcher/install-launcher.ps1"
crlf_bom scripts/run-collector.ps1    "$out/collector/run-collector.ps1"
crlf_bom config/vcportal.conf.example "$out/config/vcportal.conf.example"

echo "배포 패키지: $out  (이 폴더를 공유폴더의 vc-portal 로 그대로 복사)"
