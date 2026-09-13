// copy-send는 ETX(폐쇄망) 환경에서 {user}_copy.txt 의 내용을 읽어
// AWX Job Template의 description 필드에 그대로 등록한다.
// Windows 쪽 copy-widget이 이 값을 조회해 클립보드로 가져간다.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"copy-bridge/internal/awx"
	"copy-bridge/internal/config"
)

func main() {
	confPath := flag.String("conf", "", "설정 파일 경로 (기본: conf/copy_setting.conf 자동 탐색)")
	filePath := flag.String("file", "", "전송할 텍스트 파일 경로 (기본: -user 로 결정되는 {user}_copy.txt)")
	user := flag.String("user", "", "사용자 식별자. -file 을 생략하면 이 값으로 {user}_copy.txt 를 찾는다")
	force := flag.Bool("force", false, "AWX에 아직 수신되지 않은 이전 데이터가 남아 있어도 덮어쓴다")
	flag.Parse()

	if err := run(*confPath, *filePath, *user, *force); err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		os.Exit(1)
	}
}

func run(confPath, filePath, user string, force bool) error {
	path, err := config.ResolvePath(confPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	if filePath == "" {
		if user == "" {
			user = os.Getenv("USER")
		}
		if user == "" {
			return fmt.Errorf("-file 또는 -user 를 지정하십시오 (USER 환경변수도 비어 있습니다)")
		}
		filePath = user + "_copy.txt"
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("텍스트 파일을 읽을 수 없습니다 (%s): %w", filePath, err)
	}
	text := string(raw)

	lineCount := strings.Count(text, "\n") + 1
	if cfg.MaxLines > 0 && lineCount > cfg.MaxLines {
		return fmt.Errorf("줄 수(%d)가 max_lines(%d)를 초과합니다. 파일을 나눠서 전송하십시오", lineCount, cfg.MaxLines)
	}

	client := awx.NewClient(cfg.AWXURL, cfg.Username, cfg.Password, cfg.InsecureTLS, 30*time.Second)
	tpl, err := client.ResolveTemplate(cfg.Template)
	if err != nil {
		return err
	}

	if !force && strings.TrimSpace(tpl.Description) != "" {
		return fmt.Errorf("AWX에 아직 수신되지 않은 이전 데이터가 남아 있습니다. " +
			"Windows에서 먼저 불러오기를 하거나, 정말 덮어쓰려면 -force 를 사용하십시오")
	}

	if err := client.SetDescription(tpl.ID, text); err != nil {
		return err
	}

	fmt.Printf("전송 완료: %s (%d줄, %d바이트) -> template=%q(id=%d)\n",
		filePath, lineCount, len(raw), tpl.Name, tpl.ID)
	fmt.Println("Windows에서 오버레이 위젯의 불러오기를 눌러 확인하십시오.")
	return nil
}
