//go:build !windows

package tui

import (
	"os"
	"os/exec"
	"strings"
)

// IsTerminal 은 f 가 터미널(문자 장치)인지 알려준다.
func IsTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// EchoOff 는 stdin 이 터미널이면 stty -echo 로 입력 에코를 끄고, 원래 설정으로 되돌리는 함수를 돌려준다.
// 터미널이 아니거나(파이프·테스트) stty 가 실패하면 nil 을 돌려준다(그냥 한 줄 읽으면 된다).
// vm_power_onoff main.go 의 hiddenPrompt 와 같은 방식이다.
func EchoOff() (restore func()) {
	if !IsTerminal(os.Stdin) {
		return nil
	}
	saved, err := sttyOut("-g")
	if err != nil || saved == "" {
		return nil
	}
	if sttyRun("-echo") != nil {
		return nil
	}
	return func() { _ = sttyRun(saved) }
}

func sttyRun(args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func sttyOut(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err
}
