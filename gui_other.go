//go:build !windows

package main

// 창 앱은 윈도우 전용이다. 리눅스 빌드가 깨지지 않도록 같은 이름의 함수를 둔다.

import (
	"errors"
	"os/exec"
)

var errWindowsOnly = errors.New("윈도우 전용 기능입니다")

func dpapiEncrypt(string) (string, error)                { return "", errWindowsOnly }
func dpapiDecrypt(string) (string, error)                { return "", errWindowsOnly }
func acquireSingleInstance(string) (func(), bool, error) { return func() {}, false, nil }
func focusWindowByTitle(string) bool                     { return false }
func openPath(string) error                              { return errWindowsOnly }
func openFolderSelect(string) error                      { return errWindowsOnly }
func attachParentConsole() bool                          { return false }
func hasConsoleWindow() bool                             { return true }
func browserCandidates() []string {
	return []string{"/usr/bin/microsoft-edge", "/usr/bin/google-chrome", "/usr/bin/chromium"}
}
func startHidden(cmd *exec.Cmd, hide bool) error { return cmd.Start() }
