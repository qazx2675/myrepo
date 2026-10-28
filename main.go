package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed web/*
var embeddedWebFS embed.FS

// attachParentConsole attaches to parent CMD or PowerShell console if available
func attachParentConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	attachConsole := kernel32.NewProc("AttachConsole")
	// ATTACH_PARENT_PROCESS = 0xFFFFFFFF
	r, _, _ := attachConsole.Call(^uintptr(0))
	if r != 0 {
		// Reconnect Go stdout and stderr to the newly attached console
		stdoutHandle, err1 := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
		if err1 == nil && stdoutHandle != syscall.InvalidHandle {
			os.Stdout = os.NewFile(uintptr(stdoutHandle), "/dev/stdout")
		}
		stderrHandle, err2 := syscall.GetStdHandle(syscall.STD_ERROR_HANDLE)
		if err2 == nil && stderrHandle != syscall.InvalidHandle {
			os.Stderr = os.NewFile(uintptr(stderrHandle), "/dev/stderr")
		}
	}
}

// findBrowserExecutable searches for Edge or Chrome
func findBrowserExecutable() (string, string) {
	edgePaths := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, p := range edgePaths {
		if _, err := os.Stat(p); err == nil {
			return p, "edge"
		}
	}

	chromePaths := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	}
	for _, p := range chromePaths {
		if _, err := os.Stat(p); err == nil {
			return p, "chrome"
		}
	}

	return "", ""
}

// launchAppWindow opens the GUI in a standalone application window
func launchAppWindow(url string) (*exec.Cmd, error) {
	browserPath, _ := findBrowserExecutable()
	if browserPath != "" {
		tempProfile := filepath.Join(os.TempDir(), "sop_runner_edge_profile")
		cmd := exec.Command(
			browserPath,
			fmt.Sprintf("--app=%s", url),
			"--window-size=980,880",
			fmt.Sprintf("--user-data-dir=%s", tempProfile),
			"--no-first-run",
			"--no-default-browser-check",
		)
		if err := cmd.Start(); err == nil {
			return cmd, nil
		}
	}

	// Fallback to default browser
	cmd := exec.Command("cmd", "/c", "start", "", url)
	return cmd, cmd.Start()
}

func main() {
	attachParentConsole()

	// Flags
	cliMode := flag.Bool("cli", false, "명령줄(CLI)에서 즉시 실행")
	createSampleFlag := flag.Bool("create-sample", false, "테스트용 원본 파일(target.xlsx) 자동 생성")
	createDefaultTmplFlag := flag.Bool("create-default-template", false, "기본 양식 파일(default_template.xlsx) 자동 생성")

	targetFlag := flag.String("target", "target.xlsx", "Target 엑셀 파일명")
	templateFlag := flag.String("template", "", "양식(Template) 엑셀 파일명")
	majorFlag := flag.String("major", "", "대분류")
	minorFlag := flag.String("minor", "", "중분류")
	detailFlag := flag.String("detail", "", "상세내용")
	sheetsFlag := flag.String("sheets", "", "선택할 시트 목록 (순서대로 쉼표 구분: 예 '3_보안점검,1_OS설치')")
	outFlag := flag.String("out", "SOP_완료.xlsx", "생성할 결과 엑셀 파일명")
	workerFlag := flag.String("worker", "", "작업자 이름 설정")
	getWorkerFlag := flag.Bool("get-worker", false, "현재 등록된 작업자 이름 확인")

	flag.Parse()

	cfgMgr, err := NewConfigManager()
	if err != nil {
		fmt.Printf("[오류] 설정 초기화 실패: %v\n", err)
		os.Exit(1)
	}

	appDir := GetAppDir()

	// Ensure default template is created in templates folder
	defaultTmplPath := filepath.Join(appDir, "templates", "default_template.xlsx")
	if _, err := os.Stat(defaultTmplPath); err != nil {
		_ = CreateDefaultTemplate(defaultTmplPath)
	}

	// CLI Mode Execution
	if *cliMode {
		runCLI(cfgMgr, appDir, *createSampleFlag, *createDefaultTmplFlag, *workerFlag, *getWorkerFlag,
			*targetFlag, *templateFlag, *majorFlag, *minorFlag, *detailFlag, *sheetsFlag, *outFlag)
		return
	}

	// GUI Mode: Launch Web Server & Browser
	serverState := NewServerState(cfgMgr)

	mux := http.NewServeMux()

	// Static files from embedded FS
	webSubFS, err := fs.Sub(embeddedWebFS, "web")
	if err != nil {
		fmt.Printf("[오류] 내장 웹 리소스 로드 실패: %v\n", err)
		os.Exit(1)
	}
	mux.Handle("/", http.FileServer(http.FS(webSubFS)))

	// API routes
	mux.HandleFunc("/api/config", serverState.handleGetConfig)
	mux.HandleFunc("/api/config/worker", serverState.handleSetWorker)
	mux.HandleFunc("/api/config/category", serverState.handleAddCategory)
	mux.HandleFunc("/api/target/sheets", serverState.handleScanTargetSheets)
	mux.HandleFunc("/api/templates/create-default", serverState.handleCreateDefaultTemplate)
	mux.HandleFunc("/api/templates/upload", serverState.handleUploadTemplate)
	mux.HandleFunc("/api/templates/select", serverState.handleSelectTemplate)
	mux.HandleFunc("/api/generate", serverState.handleGenerateSOP)
	mux.HandleFunc("/api/create-sample-target", serverState.handleCreateSampleTarget)
	mux.HandleFunc("/api/open-file", serverState.handleOpenFile)
	mux.HandleFunc("/api/exit", serverState.handleExit)
	mux.HandleFunc("/api/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		serverState.recordHeartbeat()
		respondJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("[오류] 포트 바인딩 실패: %v\n", err)
		os.Exit(1)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	appURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	server := &http.Server{Handler: mux}

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[오류] 서버 실행 오류: %v\n", err)
		}
	}()

	fmt.Printf("[SOP Generator] 서버 시작됨: %s\n", appURL)
	_, _ = launchAppWindow(appURL)

	// Heartbeat watcher: Auto-shutdown if UI is closed and no heartbeat for 45s
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			diff := time.Now().Unix() - serverState.lastHeartbeat.Load()
			if diff > 45 {
				serverState.requestExit()
				return
			}
		}
	}()

	<-serverState.exitChan
	fmt.Println("[SOP Generator] 프로그램 정상 종료 중...")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func runCLI(cfgMgr *ConfigManager, appDir string, createSample, createDefaultTmpl bool,
	worker string, getWorker bool,
	target, template, major, minor, detail, sheets, out string) {

	if getWorker {
		fmt.Printf("작업자: %s\n", cfgMgr.GetConfig().WorkerName)
		return
	}

	if worker != "" {
		if err := cfgMgr.SetWorkerName(worker); err != nil {
			fmt.Printf("[오류] 작업자 이름 설정 실패: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("[성공] 작업자 이름 저장 완료: %s\n", worker)
	}

	if createSample {
		targetPath := filepath.Join(appDir, "target.xlsx")
		if err := CreateSampleTarget(targetPath); err != nil {
			fmt.Printf("[오류] 샘플 Target 생성 실패: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("[성공] 테스트용 target.xlsx 생성 완료: %s\n", targetPath)
	}

	if createDefaultTmpl {
		tmplPath := filepath.Join(appDir, "templates", "default_template.xlsx")
		if err := CreateDefaultTemplate(tmplPath); err != nil {
			fmt.Printf("[오류] 기본 양식 생성 실패: %v\n", err)
			os.Exit(1)
		}
		_ = cfgMgr.AddTemplate("default_template", "기본 표준 양식 (B8,C8,G8,H8)", tmplPath, true)
		fmt.Printf("[성공] 기본 양식(default_template.xlsx) 생성 완료: %s\n", tmplPath)
	}

	if sheets == "" {
		if !createSample && !createDefaultTmpl && worker == "" && !getWorker {
			fmt.Println("SOP 자동 생성 프로그램 CLI 모드")
			fmt.Println("사용법 예시:")
			fmt.Println("  excel-test-runner.exe -cli -create-sample")
			fmt.Println("  excel-test-runner.exe -cli -worker=\"홍길동\"")
			fmt.Println("  excel-test-runner.exe -cli -major=\"VM설치\" -minor=\"서버\" -detail=\"점검내용\" -sheets=\"3_보안점검,1_OS설치\" -out=\"SOP_결과.xlsx\"")
		}
		return
	}

	// Run SOP Generation
	sheetList := parseCommaList(sheets)
	if len(sheetList) == 0 {
		fmt.Println("[오류] 선택된 시트가 없습니다.")
		os.Exit(1)
	}

	tmplPath := template
	if tmplPath == "" {
		cfg := cfgMgr.GetConfig()
		for _, t := range cfg.Templates {
			if t.ID == cfg.SelectedTemplate {
				tmplPath = t.FilePath
				break
			}
		}
		if tmplPath == "" {
			tmplPath = filepath.Join(appDir, "templates", "default_template.xlsx")
		}
	}

	targetPath := target
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(appDir, targetPath)
	}

	if major == "" {
		major = "VM설치"
	}
	if minor == "" {
		minor = "공통"
	}

	req := SOPGenerateRequest{
		TargetPath:     targetPath,
		TemplatePath:   tmplPath,
		MajorCategory:  major,
		MinorCategory:  minor,
		DetailText:     detail,
		SelectedSheets: sheetList,
		DstFilename:    out,
	}

	fmt.Printf("[실행] SOP 엑셀 생성 중...\n")
	fmt.Printf(" - 원본 파일: %s\n", targetPath)
	fmt.Printf(" - 양식 파일: %s\n", tmplPath)
	fmt.Printf(" - 대분류: %s | 중분류: %s\n", major, minor)
	fmt.Printf(" - 시트 순서: %s\n", strings.Join(sheetList, " -> "))
	fmt.Printf(" - 출력 파일명: %s\n", out)

	res, err := GenerateSOPExcel(req)
	if err != nil {
		fmt.Printf("[실행 실패] %v\n", err)
		os.Exit(1)
	}

	fmt.Println("==================================================")
	fmt.Printf("[성공] %s\n", res.Message)
	fmt.Printf(" - 생성 파일 경로: %s\n", res.DstPath)
	fmt.Printf(" - 파일 크기: %d 바이트\n", res.FileSize)
	fmt.Printf(" - 소요 시간: %s\n", res.ExecutionTime)
	fmt.Println("==================================================")
}

func parseCommaList(input string) []string {
	parts := strings.Split(input, ",")
	var result []string
	for _, p := range parts {
		clean := strings.TrimSpace(p)
		if clean != "" {
			result = append(result, clean)
		}
	}
	return result
}
