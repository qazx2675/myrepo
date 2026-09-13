//go:build windows

// copy-widget은 Windows 개인 PC에서 띄우는 항상 위(always-on-top) 소형 위젯이다.
// 외부 GUI 라이브러리 없이 표준 라이브러리 syscall만으로 Win32 API를 직접 호출한다
// (폐쇄망 오프라인 빌드 원칙 + go.mod에 외부 의존성이 생기지 않도록).
//
// 동작: 창을 왼쪽 클릭하면 AWX Job Template의 description을 조회해
// (1) 클립보드에 복사하고 (2) 내용을 메시지박스로 보여준 뒤 (3) AWX 값을 비운다.
// 창을 드래그해서 원하는 위치로 옮길 수 있고, 오른쪽 클릭하면 종료된다.
package main

import (
	"flag"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"copy-bridge/internal/awx"
	"copy-bridge/internal/config"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procDrawTextW        = user32.NewProc("DrawTextW")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procMessageBoxW      = user32.NewProc("MessageBoxW")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procGetCursorPos     = user32.NewProc("GetCursorPos")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")

	procSelectObject   = gdi32.NewProc("SelectObject")
	procGetStockObject = gdi32.NewProc("GetStockObject")
	procSetBkMode      = gdi32.NewProc("SetBkMode")
)

const (
	wsPopup  = 0x80000000
	wsBorder = 0x00800000

	wsExTopMost    = 0x00000008
	wsExToolWindow = 0x00000080

	swShow = 5

	wmDestroy     = 0x0002
	wmPaint       = 0x000F
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmMouseMove   = 0x0200

	mkLButton = 0x0001

	swpNoSize   = 0x0001
	swpNoZOrder = 0x0004

	dtCenter     = 0x00000001
	dtVCenter    = 0x00000004
	dtSingleLine = 0x00000020

	transparentBk = 1

	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	mbOK       = 0x00000000
	mbIconInfo = 0x00000040

	idcArrow       = 32512
	whiteBrush     = 0
	defaultGuiFont = 17

	cwUseDefault = 0x80000000

	windowWidth  = 110
	windowHeight = 90
)

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

type msgT struct {
	hwnd    syscall.Handle
	message uint32
	_       uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type paintStruct struct {
	hdc         syscall.Handle
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

var (
	confPathFlag string

	dragging        bool
	moved           bool
	dragStartCursor point
	dragWindowStart point
)

func main() {
	flag.StringVar(&confPathFlag, "conf", "", "설정 파일 경로 (기본: conf/copy_setting.conf 자동 탐색)")
	flag.Parse()

	className, _ := syscall.UTF16PtrFromString("CopyBridgeWidget")
	titleName, _ := syscall.UTF16PtrFromString("복사 브릿지")

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))
	bg, _, _ := procGetStockObject.Call(whiteBrush)

	wndProcPtr := syscall.NewCallback(wndProc)

	wc := wndClassEx{
		style:         0,
		lpfnWndProc:   wndProcPtr,
		hInstance:     syscall.Handle(hInstance),
		hCursor:       syscall.Handle(cursor),
		hbrBackground: syscall.Handle(bg),
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))

	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fmt.Println("창 클래스 등록 실패:", err)
		return
	}

	exStyle := uintptr(wsExTopMost | wsExToolWindow)
	style := uintptr(wsPopup | wsBorder)

	hwnd, _, err := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titleName)),
		style,
		uintptr(cwUseDefault), uintptr(cwUseDefault),
		windowWidth, windowHeight,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		fmt.Println("창 생성 실패:", err)
		return
	}

	// CreateWindowExW의 WS_EX_TOPMOST만으로는 다른 최상위 창에 가려질 수 있어
	// 생성 직후 한 번 더 최상단으로 고정한다.
	const hwndTopmost = ^uintptr(0) // -1
	procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoSize|swpNoZOrder|0x0001)

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	var m msgT
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmPaint:
		onPaint(hwnd)
		return 0

	case wmLButtonDown:
		dragging = true
		moved = false
		getCursorPos(&dragStartCursor)
		dragWindowStart = getWindowOrigin(hwnd)
		return 0

	case wmMouseMove:
		if dragging && wParam&mkLButton != 0 {
			var cur point
			getCursorPos(&cur)
			dx := cur.x - dragStartCursor.x
			dy := cur.y - dragStartCursor.y
			if abs32(dx) > 3 || abs32(dy) > 3 {
				moved = true
			}
			newX := dragWindowStart.x + dx
			newY := dragWindowStart.y + dy
			procSetWindowPos.Call(uintptr(hwnd), 0, uintptr(int32(newX)), uintptr(int32(newY)), 0, 0, swpNoSize|swpNoZOrder)
		}
		return 0

	case wmLButtonUp:
		wasDragging := dragging
		wasMoved := moved
		dragging = false
		moved = false
		if wasDragging && !wasMoved {
			handleFetch(hwnd)
		}
		return 0

	case wmRButtonUp:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0

	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func getCursorPos(p *point) {
	procGetCursorPos.Call(uintptr(unsafe.Pointer(p)))
}

func getWindowOrigin(hwnd syscall.Handle) point {
	var r rect
	procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return point{x: r.left, y: r.top}
}

func onPaint(hwnd syscall.Handle) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	var cr rect
	procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&cr)))

	procSetBkMode.Call(hdc, transparentBk)

	font, _, _ := procGetStockObject.Call(defaultGuiFont)
	procSelectObject.Call(hdc, font)

	label, _ := syscall.UTF16PtrFromString("📋 불러오기")
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(label)), ^uintptr(0),
		uintptr(unsafe.Pointer(&cr)), dtCenter|dtVCenter|dtSingleLine)
}

// handleFetch는 AWX에서 description을 조회해 클립보드로 복사하고
// 확인 메시지를 띄운 뒤 AWX 쪽 값을 비운다(수신 완료 cleanup).
func handleFetch(hwnd syscall.Handle) {
	confPath, err := config.ResolvePath(confPathFlag)
	if err != nil {
		showMessage(hwnd, "설정 오류", err.Error())
		return
	}
	cfg, err := config.Load(confPath)
	if err != nil {
		showMessage(hwnd, "설정 오류", err.Error())
		return
	}

	client := awx.NewClient(cfg.AWXURL, cfg.Username, cfg.Password, cfg.InsecureTLS, 15*time.Second)
	tpl, err := client.ResolveTemplate(cfg.Template)
	if err != nil {
		showMessage(hwnd, "AWX 오류", err.Error())
		return
	}

	if strings.TrimSpace(tpl.Description) == "" {
		showMessage(hwnd, "불러오기", "새로 등록된 데이터가 없습니다.")
		return
	}
	text := tpl.Description

	if err := setClipboardText(text); err != nil {
		showMessage(hwnd, "클립보드 오류", err.Error())
		return
	}

	if err := client.ClearDescription(tpl.ID); err != nil {
		showMessage(hwnd, "경고",
			"클립보드 복사는 완료됐지만 AWX 초기화(cleanup)에 실패했습니다: "+err.Error())
	}

	preview := text
	if len(preview) > 2000 {
		preview = preview[:2000] + "\n...(이하 생략, 클립보드에는 전체가 복사됨)"
	}
	showMessage(hwnd, "불러옴 (클립보드에 복사됨)", preview)
}

func showMessage(hwnd syscall.Handle, title, text string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	procMessageBoxW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbOK|mbIconInfo)
}

// setClipboardText는 텍스트를 시스템 클립보드(CF_UNICODETEXT)에 그대로 복사한다.
func setClipboardText(text string) error {
	r1, _, err := procOpenClipboard.Call(0)
	if r1 == 0 {
		return fmt.Errorf("OpenClipboard 실패: %v", err)
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	u16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := len(u16) * 2

	h, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(size))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc 실패")
	}
	// go vet(unsafeptr)이 uintptr->Pointer 변환을 안전하다고 인식하려면
	// syscall.Syscall 호출 결과를 그 자리에서 바로 변환해야 하므로,
	// LazyProc.Call 대신 syscall.Syscall을 직접 사용한다.
	p, _, _ := syscall.Syscall(procGlobalLock.Addr(), 1, h, 0, 0)
	ptr := unsafe.Pointer(p)
	if ptr == nil {
		return fmt.Errorf("GlobalLock 실패")
	}
	dst := unsafe.Slice((*uint16)(ptr), len(u16))
	copy(dst, u16)
	procGlobalUnlock.Call(h)

	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		return fmt.Errorf("SetClipboardData 실패: %v", err)
	}
	return nil
}
