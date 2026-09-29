//go:build !windows

package conf

// CheckNotMappedDrive 는 Windows 가 아니면 아무 것도 하지 않는다.
func CheckNotMappedDrive(p string) error { return nil }
