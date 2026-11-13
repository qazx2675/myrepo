// Package launcher opens the cross-checker web UI as a chrome-less desktop
// window using the Edge/Chrome "--app" mode (same approach as datacenter-viewer).
package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Window size tuned for the 3-tab cross-checker panel.
const (
	WindowWidth  = 640
	WindowHeight = 920
)

// candidatePaths returns well-known Edge/Chrome install locations (Edge first,
// because it is preinstalled on every Windows 10/11 PC in the closed network).
func candidatePaths() []string {
	if runtime.GOOS != "windows" {
		return []string{
			"/usr/bin/microsoft-edge", "/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		}
	}
	var paths []string
	pf86 := os.Getenv("ProgramFiles(x86)")
	pf := os.Getenv("ProgramFiles")
	local := os.Getenv("LocalAppData")
	for _, base := range []string{pf86, pf, local} {
		if base != "" {
			paths = append(paths, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	for _, base := range []string{pf, pf86, local} {
		if base != "" {
			paths = append(paths, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	return paths
}

// FindBrowser returns the first installed Edge/Chrome executable, or "".
func FindBrowser() string {
	if custom := os.Getenv("CROSS_CHECKER_BROWSER"); custom != "" {
		if st, err := os.Stat(custom); err == nil && !st.IsDir() {
			return custom
		}
	}
	for _, p := range candidatePaths() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if lp, err := exec.LookPath("msedge.exe"); err == nil {
		return lp
	}
	if lp, err := exec.LookPath("chrome.exe"); err == nil {
		return lp
	}
	return ""
}

// ProfileDir is the isolated browser profile used by the app window so it never
// mixes with (or is blocked by) the user's normal browser session.
func ProfileDir() string {
	if custom := os.Getenv("CROSS_CHECKER_PROFILE_DIR"); custom != "" {
		return custom
	}
	return filepath.Join(os.TempDir(), "host-cross-checker-profile")
}

// Args builds the command-line arguments for app-mode launch.
func Args(appURL, profileDir string) []string {
	args := []string{
		"--app=" + appURL,
		"--user-data-dir=" + profileDir,
		fmt.Sprintf("--window-size=%d,%d", WindowWidth, WindowHeight),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-default-apps",
		"--disable-sync",
		"--disable-background-mode",
		"--disable-features=RendererCodeIntegrity,Translate,msEdgeSidebarV2,BlockInsecurePrivateNetworkRequests",
		"--disable-renderer-backgrounding",
		"--disable-background-timer-throttling",
		"--proxy-server=direct://",
		"--proxy-bypass-list=<-loopback>",
	}
	if dbgPort := os.Getenv("CROSS_CHECKER_DEBUG_PORT"); dbgPort != "" {
		args = append(args, "--remote-debugging-port="+dbgPort)
	}
	return args
}

// AWXProfileDir is the isolated browser profile used when opening the AWX
// web console directly so TLS/PNA flags apply even if the main browser is running.
func AWXProfileDir() string {
	if custom := os.Getenv("CROSS_CHECKER_AWX_PROFILE_DIR"); custom != "" {
		return custom
	}
	return filepath.Join(os.TempDir(), "host-cross-checker-awx-profile")
}

// BuildAWXArgs builds Edge/Chrome CLI flags for opening AWX without blocking cert prompts.
func BuildAWXArgs(targetURL, profileDir string, spkiHashes []string, insecureTLS bool) []string {
	args := []string{
		"--new-window",
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate,msEdgeSidebarV2,BlockInsecurePrivateNetworkRequests",
		"--proxy-server=direct://",
		"--proxy-bypass-list=<-loopback>",
	}
	if insecureTLS {
		args = append(args, "--allow-insecure-localhost")
		if len(spkiHashes) > 0 {
			args = append(args, "--ignore-certificate-errors-spki-list="+strings.Join(spkiHashes, ","))
		} else {
			args = append(args, "--ignore-certificate-errors")
		}
	}
	if dbgPort := os.Getenv("CROSS_CHECKER_AWX_DEBUG_PORT"); dbgPort != "" {
		args = append(args, "--remote-debugging-port="+dbgPort)
	}
	args = append(args, targetURL)
	return args
}

// OpenAWX launches the AWX console URL in a dedicated Edge/Chrome window with
// certificate bypass and private-network bridge allowances enabled.
func OpenAWX(targetURL string, spkiHashes []string, insecureTLS bool) (string, string, error) {
	browser := FindBrowser()
	if browser == "" {
		return "default", "os-default", openDefault(targetURL)
	}
	profile := AWXProfileDir()
	_ = os.MkdirAll(profile, 0o755)
	bypassMethod := "none"
	if insecureTLS {
		if len(spkiHashes) > 0 {
			bypassMethod = "spki"
		} else {
			bypassMethod = "ignore-cert-errors"
		}
	}
	cmd := exec.Command(browser, BuildAWXArgs(targetURL, profile, spkiHashes, insecureTLS)...)
	if err := cmd.Start(); err != nil {
		return browser, "os-default", openDefault(targetURL)
	}
	go func() { _ = cmd.Wait() }()
	return browser, bypassMethod, nil
}

// Open launches appURL in an Edge/Chrome app window.
func Open(appURL string) (string, error) {
	browser := FindBrowser()
	if browser == "" {
		return "default", openDefault(appURL)
	}
	profile := ProfileDir()
	_ = os.MkdirAll(profile, 0o755)
	cmd := exec.Command(browser, Args(appURL, profile)...)
	if err := cmd.Start(); err != nil {
		return browser, openDefault(appURL)
	}
	// Reaping child process without terminating the Go server.
	// Edge/Chrome Startup Boost delegates window creation to a background broker
	// causing the spawned child to exit in ~100ms. Lifecycle is strictly managed
	// via HTTP heartbeats (/api/ping and /api/bye) in the web UI.
	go func() { _ = cmd.Wait() }()
	return browser, nil
}

func openDefault(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}
