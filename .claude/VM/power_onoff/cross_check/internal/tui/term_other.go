//go:build windows

package tui

import "os"

// IsTerminal 은 f 가 문자 장치인지 알려준다(Windows 개발기에서는 참고용).
func IsTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// EchoOff 는 Windows 에서는 아무것도 하지 않는다(stty 없음, 대상 환경은 RHEL).
func EchoOff() (restore func()) { return nil }
