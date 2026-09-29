//go:build windows

package conf

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// CheckNotMappedDrive 는 p 가 네트워크 드라이브(Z:\ 등)로 시작하면 오류를 반환한다.
func CheckNotMappedDrive(p string) error {
	if len(p) < 2 || p[1] != ':' {
		return nil
	}
	c := p[0]
	if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
		return nil
	}
	root, err := windows.UTF16PtrFromString(p[:2] + `\`)
	if err != nil {
		return nil
	}
	if windows.GetDriveType(root) == windows.DRIVE_REMOTE {
		return fmt.Errorf("%q 은(는) 네트워크 드라이브입니다. 실행 계정에 따라 보이지 않을 수 있으므로 UNC 경로(\\\\서버\\공유\\...)를 사용하세요", p)
	}
	return nil
}
