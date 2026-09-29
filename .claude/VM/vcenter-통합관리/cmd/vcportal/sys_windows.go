//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// showMessage 는 GUI 서브시스템 exe 이므로 메시지 박스로 알린다.
func showMessage(title, text string, isErr bool) {
	flags := uint32(windows.MB_OK | windows.MB_SETFOREGROUND)
	if isErr {
		flags |= windows.MB_ICONERROR
	} else {
		flags |= windows.MB_ICONINFORMATION
	}
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString(title)
	windows.MessageBox(0, t, c, flags)
}

// findBrowser 는 설치된 Edge/Chrome 실행 파일을 찾는다.
func findBrowser(kind string) (string, error) {
	rel := `Microsoft\Edge\Application\msedge.exe`
	name := "Microsoft Edge"
	if kind == "chrome" {
		rel = `Google\Chrome\Application\chrome.exe`
		name = "Google Chrome"
	}
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		p := filepath.Join(base, rel)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s 이(가) 설치되어 있지 않습니다. conf [browser] type 을 확인하세요.", name)
}

// startDetached 는 런처가 끝나도 살아 있도록 브라우저를 분리된 프로세스로 띄운다.
// 런처가 "닫힐 때 자식도 종료" 작업 개체(Job) 안에서 실행된 경우에도 브라우저가 살아남도록
// Job 이탈(CREATE_BREAKAWAY_FROM_JOB)을 먼저 시도하고, 허용되지 않으면 이탈 없이 띄운다.
func startDetached(exe string, args []string) error {
	base := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	var err error
	for _, flags := range []uint32{base | windows.CREATE_BREAKAWAY_FROM_JOB, base} {
		cmd := exec.Command(exe, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
		if err = cmd.Start(); err == nil {
			return cmd.Process.Release()
		}
	}
	return err
}
