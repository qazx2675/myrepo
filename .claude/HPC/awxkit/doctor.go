package main

import (
	"fmt"
	"os"
	"runtime"

	"awxkit/awx"
)

// runDoctor는 설정 파일 로딩부터 AWX 연결·권한·파라미터 설정까지를 순서대로 점검하고
// 문제가 있으면 즉시 화면에 알려준다.
func runDoctor(confPath string) {
	fmt.Printf("[i] 설정 파일: %s\n", confPath)
	warnIfWorldReadable(confPath)

	cfg, client, err := loadConfigAndClient(confPath)
	if err != nil {
		fmt.Printf("[X] 설정 오류: %v\n", err)
		os.Exit(1)
	}

	ping, err := client.Ping()
	if err != nil {
		fmt.Printf("[X] AWX 서버 연결 실패: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[✔] AWX 연결 성공 (버전: %s)\n", ping.Version)

	templates, err := client.ListJobTemplates()
	if err != nil {
		fmt.Printf("[X] 템플릿 목록 조회 실패: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[✔] 조회 가능한 템플릿 %d개\n", len(templates))

	checkConfiguredTemplate(client, "S1 (NodeInfo)", cfg.S1Template)
	checkConfiguredTemplate(client, "S3 (DHCP)", cfg.S3Template)
	checkConfiguredTemplate(client, "S4 (PXE)", cfg.S4Template)

	fmt.Println("\n점검 완료.")
}

// checkConfiguredTemplate은 conf에 지정된 템플릿이 실제로 존재하고,
// extra_vars 전달과 실행 권한이 정상인지 확인한다.
func checkConfiguredTemplate(client *awx.Client, label, idOrName string) {
	if idOrName == "" {
		fmt.Printf("[-] %s: conf에 템플릿이 지정되지 않아 건너뜁니다.\n", label)
		return
	}

	t, err := client.ResolveTemplate(idOrName)
	if err != nil {
		fmt.Printf("[X] %s: 템플릿(%s)을 찾을 수 없습니다: %v\n", label, idOrName, err)
		return
	}

	if !t.AskVariablesOnLaunch {
		fmt.Printf("[!] %s: 템플릿(%s)의 'ask_variables_on_launch'가 꺼져 있습니다. extra_vars를 보내도 조용히 무시됩니다.\n", label, t.Name)
	}
	if !t.SummaryFields.UserCapabilities.Start {
		fmt.Printf("[X] %s: 템플릿(%s)을 실행할 권한이 없습니다.\n", label, t.Name)
		return
	}
	fmt.Printf("[✔] %s: 템플릿(%s, ID=%d) 실행 가능\n", label, t.Name, t.ID)
}

// warnIfWorldReadable은 conf 파일에 평문 비밀번호가 들어있으므로 권한이 과도하게
// 열려 있지 않은지 경고한다. Windows에서는 유닉스 권한 비트 개념이 없어 건너뛴다.
func warnIfWorldReadable(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	mode := info.Mode().Perm()
	if mode&0o077 != 0 {
		fmt.Printf("[!] %s 파일 권한이 %o 입니다. 비밀번호가 평문으로 들어있으니 'chmod 600 %s'를 권장합니다.\n", path, mode, path)
	}
}
