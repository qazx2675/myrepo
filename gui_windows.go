//go:build windows

// [용도] 업무에 필요한 서버 설정값 비교를 위해 사용되는 툴로, 업무 시 텍스트 기록을 위한 목적으로 가져오는 데만 사용됩니다.
package main

// 윈도우 창 앱용 시스템 호출: DPAPI, 단일 인스턴스, 창 앞으로 가져오기, 셸 열기, 콘솔 붙이기, 브라우저 실행.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modCrypt32       = syscall.NewLazyDLL("crypt32.dll")
	procProtect      = modCrypt32.NewProc("CryptProtectData")
	procUnprotect    = modCrypt32.NewProc("CryptUnprotectData")
	procLocalFree    = kernel32.NewProc("LocalFree")
	procCreateMutex  = kernel32.NewProc("CreateMutexW")
	procAttachCon    = kernel32.NewProc("AttachConsole")
	procGetConsoleWn = kernel32.NewProc("GetConsoleWindow")
	modUser32        = syscall.NewLazyDLL("user32.dll")
	procEnumWindows  = modUser32.NewProc("EnumWindows")
	procGetWinText   = modUser32.NewProc("GetWindowTextW")
	procIsVisible    = modUser32.NewProc("IsWindowVisible")
	procShowWindow   = modUser32.NewProc("ShowWindow")
	procSetFore      = modUser32.NewProc("SetForegroundWindow")
	modShell32       = syscall.NewLazyDLL("shell32.dll")
	procShellExecute = modShell32.NewProc("ShellExecuteW")
)

const (
	createNoWindow   = 0x08000000
	cryptUIForbidden = 0x1
	errAlreadyExists = 183
	swRestore        = 9
	swShowNormal     = 1
	attachParent     = ^uintptr(0) // ATTACH_PARENT_PROCESS (-1)
)

type dataBlob struct {
	cb uint32
	pb *byte
}

func blobBytes(b dataBlob) []byte {
	if b.pb == nil || b.cb == 0 {
		return nil
	}
	out := make([]byte, b.cb)
	copy(out, unsafe.Slice(b.pb, b.cb))
	return out
}

// dpapiEncrypt 는 현재 사용자 범위 DPAPI 로 암호화해 base64 로 돌려준다.
func dpapiEncrypt(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("빈 비밀번호")
	}
	raw := []byte(plain)
	in := dataBlob{cb: uint32(len(raw)), pb: &raw[0]}
	var out dataBlob
	r, _, e := procProtect.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, cryptUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return "", fmt.Errorf("CryptProtectData 실패: %v", e)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pb)))
	return base64.StdEncoding.EncodeToString(blobBytes(out)), nil
}

// dpapiDecrypt 는 dpapiEncrypt 의 역이다. 다른 사용자/PC 에서 만든 값은 실패한다.
func dpapiDecrypt(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil || len(raw) == 0 {
		return "", errors.New("password_enc 형식 오류")
	}
	in := dataBlob{cb: uint32(len(raw)), pb: &raw[0]}
	var out dataBlob
	r, _, e := procUnprotect.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, cryptUIForbidden, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return "", fmt.Errorf("CryptUnprotectData 실패: %v", e)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pb)))
	return string(blobBytes(out)), nil
}

// acquireSingleInstance 는 이름 있는 뮤텍스로 단일 실행을 보장한다. 이미 있으면 already=true.
func acquireSingleInstance(name string) (release func(), already bool, err error) {
	p, _ := syscall.UTF16PtrFromString(name)
	h, _, e := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(p)))
	if h == 0 {
		return func() {}, false, fmt.Errorf("CreateMutex 실패: %v", e)
	}
	if errno, ok := e.(syscall.Errno); ok && errno == errAlreadyExists {
		_ = syscall.CloseHandle(syscall.Handle(h))
		return func() {}, true, nil
	}
	return func() { _ = syscall.CloseHandle(syscall.Handle(h)) }, false, nil
}

// focusWindowByTitle 은 제목에 title 이 들어간 보이는 창을 앞으로 가져온다.
func focusWindowByTitle(title string) bool {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if v, _, _ := procIsVisible.Call(hwnd); v == 0 {
			return 1
		}
		buf := make([]uint16, 512)
		n, _, _ := procGetWinText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n > 0 && strings.Contains(syscall.UTF16ToString(buf[:n]), title) {
			found = hwnd
			return 0
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	if found == 0 {
		return false
	}
	_, _, _ = procShowWindow.Call(found, swRestore)
	r, _, _ := procSetFore.Call(found)
	return r != 0
}

func shellExecute(verb, file, args string) error {
	v, _ := syscall.UTF16PtrFromString(verb)
	f, _ := syscall.UTF16PtrFromString(file)
	var a *uint16
	if args != "" {
		a, _ = syscall.UTF16PtrFromString(args)
	}
	r, _, _ := procShellExecute.Call(0, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(a)), 0, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("ShellExecute 실패 (코드 %d)", r)
	}
	return nil
}

// openPath 는 파일을 연결 프로그램으로 연다 (자식 콘솔 없음).
func openPath(p string) error { return shellExecute("open", p, "") }

// openFolderSelect 는 탐색기에서 파일을 선택한 상태로 연다. 파일이 없으면 폴더를 연다.
func openFolderSelect(p string) error {
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return shellExecute("open", "explorer.exe", `/select,"`+p+`"`)
	}
	if _, err := os.Stat(p); err != nil {
		p = filepath.Dir(p)
	}
	return shellExecute("open", p, "")
}

// attachParentConsole 은 부모 콘솔(cmd, PowerShell)에 붙는다. 붙으면 표준 출력을 그 콘솔로 바꾼다.
func attachParentConsole() bool {
	if r, _, _ := procAttachCon.Call(attachParent); r == 0 {
		return false
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	return true
}

// hasConsoleWindow 는 이 프로세스에 콘솔 창이 있는지 본다 (-H windowsgui 확인용).
func hasConsoleWindow() bool {
	h, _, _ := procGetConsoleWn.Call()
	return h != 0
}

func browserCandidates() []string {
	var out []string
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LocalAppData")} {
		if base != "" {
			out = append(out, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
		if base != "" {
			out = append(out, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	return out
}

// startHidden 은 자식 프로세스를 콘솔 창 없이 시작한다.
// 브라우저는 창이 보여야 하므로 HideWindow 를 쓰지 않고 CREATE_NO_WINDOW 만 쓴다.
func startHidden(cmd *exec.Cmd, hide bool) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: hide, CreationFlags: createNoWindow}
	return cmd.Start()
}
