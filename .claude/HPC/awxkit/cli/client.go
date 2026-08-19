// Package cli는 각 단계별 바이너리(cmd/doctor, cmd/nodeinfo 등)가 공유하는
// conf 로딩·AWX 클라이언트 생성·Job 폴링·선택지 처리 등의 로직을 담는다.
package cli

import (
	"fmt"
	"os"
	"time"

	"awxkit/awx"
	"awxkit/config"
)

// LoadConfigAndClient는 conf 파일을 읽고 그 값으로 AWX 클라이언트를 생성한다.
func LoadConfigAndClient(confPath string) (*config.Config, *awx.Client, error) {
	cfg, err := config.Load(confPath)
	if err != nil {
		return nil, nil, err
	}
	if cfg.AWXURL == "" || cfg.Username == "" || cfg.Password == "" {
		return nil, nil, fmt.Errorf("awx_url / username / password 중 비어있는 값이 있습니다")
	}
	client := awx.NewClient(cfg.AWXURL, cfg.Username, cfg.Password, cfg.InsecureTLS, 10*time.Second)
	return cfg, client, nil
}

// AppendHistory는 실행 이력을 cfg.HistoryFile에 한 줄 추가한다.
func AppendHistory(cfg *config.Config, line string) {
	if cfg.HistoryFile == "" {
		return
	}
	f, err := os.OpenFile(cfg.HistoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Printf("[!] 이력 파일 기록 실패 (%s): %v\n", cfg.HistoryFile, err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), line)
}

// SettingChangeWarning은 설정을 바꾸는 시나리오(DHCP 등) 실행 후 항상 보여주는 검증 권고 문구다.
const SettingChangeWarning = "[!] 설정 변경 작업입니다. 완료 후 랜덤하게 서버 몇 대를 직접 확인해 실제로 변경되었는지 검증하세요."
