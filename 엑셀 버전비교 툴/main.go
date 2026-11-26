// 엑셀 버전비교 툴. 실행하면 127.0.0.1 에 서버를 띄우고 앱 창을 연다.
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"syscall"
	"unsafe"

	"excel-diff/internal/web"
)

const title = "엑셀 버전비교"

var (
	shell32        = syscall.NewLazyDLL("shell32.dll")
	shellExecute   = shell32.NewProc("ShellExecuteW")
	knownFolder    = shell32.NewProc("SHGetKnownFolderPath")
	coTaskMemFree  = syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree")
	messageBoxProc = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	dir := filepath.Dir(exe)
	logPath := openLog(dir)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(fmt.Errorf("포트를 열 수 없습니다: %w", err))
	}
	// 문서 폴더를 찾지 못하면 대체 폴더 없이 동작한다.
	docs, err := documentsDir()
	if err != nil {
		log.Printf("문서 폴더를 찾지 못했습니다: %v", err)
	}
	app, err := web.New(web.Config{
		Host:        listener.Addr().String(),
		OutDir:      filepath.Join(dir, "results"),
		FallbackDir: docs,
		Open:        shellOpen,
	})
	if err != nil {
		fatal(fmt.Errorf("시작할 수 없습니다: %w", err))
	}
	server := &http.Server{Handler: app.Handler()}
	go func() {
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			app.Close()
			fatal(fmt.Errorf("서버 오류: %w", err))
		}
	}()
	log.Printf("시작: %s (로그 %s)", listener.Addr(), logPath)

	if err := openWindow(app.URL()); err != nil {
		app.Close()
		fatal(fmt.Errorf("화면(브라우저)을 열 수 없습니다: %w", err))
	}
	app.Wait()

	// 이 시점에는 진행 중인 작업이 없다.
	if err := server.Close(); err != nil {
		log.Printf("서버 종료: %v", err)
	}
	if err := app.Close(); err != nil {
		log.Printf("임시 파일 삭제 실패: %v", err)
	}
	log.Print("종료")
}

// openLog 는 exe 옆(쓸 수 없으면 임시 폴더)의 로그 파일에 로그와 비정상 종료 내용을 남긴다.
func openLog(dir string) string {
	for _, d := range []string{dir, os.TempDir()} {
		path := filepath.Join(d, "excel-diff.log")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
		if err != nil {
			continue
		}
		log.SetOutput(f)
		// 메모리 부족처럼 복구할 수 없는 오류도 파일에 남긴다.
		if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
			log.Printf("충돌 로그 설정 실패: %v", err)
		}
		return path
	}
	log.SetOutput(io.Discard)
	return "(없음)"
}

// fatal 은 콘솔이 없는 빌드에서도 보이도록 메시지 상자를 띄우고 끝낸다.
func fatal(err error) {
	log.Printf("오류: %v", err)
	text, _ := syscall.UTF16PtrFromString(err.Error()) // NUL 이 없으면 실패하지 않는다
	caption, _ := syscall.UTF16PtrFromString(title)
	const iconError = 0x10
	messageBoxProc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(caption)), iconError)
	os.Exit(1)
}

// openWindow 는 Edge(없으면 Chrome)의 앱 창, 둘 다 없으면 기본 브라우저를 연다.
func openWindow(url string) error {
	for _, p := range []struct{ env, path string }{
		{"ProgramFiles(x86)", `Microsoft\Edge\Application\msedge.exe`},
		{"ProgramFiles", `Microsoft\Edge\Application\msedge.exe`},
		{"ProgramFiles", `Google\Chrome\Application\chrome.exe`},
		{"ProgramFiles(x86)", `Google\Chrome\Application\chrome.exe`},
		{"LOCALAPPDATA", `Google\Chrome\Application\chrome.exe`},
	} {
		base := os.Getenv(p.env)
		if base == "" {
			continue
		}
		browser := filepath.Join(base, p.path)
		if _, err := os.Stat(browser); err != nil {
			continue
		}
		cmd := exec.Command(browser,
			"--app="+url,
			"--window-size=1100,900",
			"--user-data-dir="+filepath.Join(os.TempDir(), "excel-diff-browser-profile"),
			"--no-first-run",
			"--no-default-browser-check",
		)
		if err := cmd.Start(); err != nil {
			log.Printf("브라우저 실행 실패(%s): %v", browser, err)
			continue
		}
		return nil
	}
	return shellOpen(url)
}

// shellOpen 은 파일·폴더·주소를 연결된 프로그램으로 연다.
func shellOpen(target string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	const showNormal = 1
	// 32 이하의 반환값은 실패이다.
	if r, _, _ := shellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, showNormal); r <= 32 {
		return fmt.Errorf("연결된 프로그램으로 열 수 없습니다(코드 %d)", r)
	}
	return nil
}

// documentsDir 은 내 문서 폴더(OneDrive 로 옮긴 경우 포함)를 돌려준다.
func documentsDir() (string, error) {
	// FOLDERID_Documents
	id := syscall.GUID{Data1: 0xFDD39AD0, Data2: 0x238F, Data3: 0x46AF, Data4: [8]byte{0xAD, 0xB4, 0x6C, 0x85, 0x48, 0x03, 0x69, 0xC7}}
	var p *[32768]uint16
	r, _, _ := knownFolder.Call(uintptr(unsafe.Pointer(&id)), 0, 0, uintptr(unsafe.Pointer(&p)))
	defer coTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	if r != 0 {
		return "", fmt.Errorf("SHGetKnownFolderPath 0x%x", r)
	}
	return syscall.UTF16ToString(p[:]), nil
}
