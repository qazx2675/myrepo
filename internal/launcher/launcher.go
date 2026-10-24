// Package launcher 는 URL 을 주소창 없는 앱 창(Edge/Chrome --app)으로 열고, 실패 시 기본 브라우저로 폴백한다.
package launcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func candidates() []string {
	pf := os.Getenv("ProgramFiles")
	pf86 := os.Getenv("ProgramFiles(x86)")
	local := os.Getenv("LocalAppData")
	return []string{
		filepath.Join(pf86, `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(pf, `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(pf, `Google\Chrome\Application\chrome.exe`),
		filepath.Join(pf86, `Google\Chrome\Application\chrome.exe`),
		filepath.Join(local, `Google\Chrome\Application\chrome.exe`),
	}
}

// Open 은 앱 모드 창을 열고 프로세스를 반환한다. 앱 모드가 불가하면 기본 브라우저를 열고 nil 을 반환한다.
func Open(url string) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" {
		for _, exe := range candidates() {
			if _, err := os.Stat(exe); err == nil {
				profile := filepath.Join(os.TempDir(), "datacenter-viewer-app-profile")
				cmd := exec.Command(
					exe,
					"--app="+url,
					"--user-data-dir="+profile,
					"--no-first-run",
					"--no-default-browser-check",
					"--disable-background-mode",
					"--disable-features=RendererCodeIntegrity",
					"--window-size=1680,980",
				)
				if err := cmd.Start(); err == nil {
					return cmd, nil
				}
			}
		}
		return nil, exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	return nil, exec.Command("xdg-open", url).Start()
}
