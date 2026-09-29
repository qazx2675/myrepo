//go:build !windows

// 런처는 Windows 전용이다. 이 파일은 리눅스(랩)에서 go vet/빌드가 되도록 두는 최소 구현이다.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

func showMessage(title, text string, isErr bool) {
	fmt.Fprintf(os.Stderr, "[%s] %s\n", title, text)
}

func acquireSingleton(name string) bool { return true }

func findBrowser(kind string) (string, error) {
	names := []string{"microsoft-edge", "microsoft-edge-stable"}
	if kind == "chrome" {
		names = []string{"google-chrome", "google-chrome-stable", "chromium"}
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s 브라우저를 찾을 수 없습니다", kind)
}

func startDetached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
