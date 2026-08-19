package main

import (
	"fmt"
	"time"

	"awxkit/awx"
	"awxkit/config"
)

// loadConfigAndClient는 conf 파일을 읽고 그 값으로 AWX 클라이언트를 생성한다.
// doctor/ls/survey 등 AWX에 접속하는 모든 명령이 공용으로 사용한다.
func loadConfigAndClient(confPath string) (*config.Config, *awx.Client, error) {
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
