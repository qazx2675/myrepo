// Package webui 는 프론트엔드 정적 자산을 바이너리에 내장한다.
// dist/ 는 build-windows.bat(또는 build.sh)가 프로젝트 루트의 index.html, css/, js/, vendor/ 를 복사해 채운다.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS 는 dist 를 루트로 하는 파일시스템.
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
