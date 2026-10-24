// datacenter-viewer: 서버 입고 마운트 3D 뷰어 단일 실행 파일.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"datacenter-viewer/internal/launcher"
	"datacenter-viewer/internal/server"
	"datacenter-viewer/internal/storage"
	"datacenter-viewer/internal/webui"
)

func waitForServerReady(healthURL string, timeout time.Duration) {
	client := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func main() {
	port := flag.Int("port", 0, "리슨 포트 (0=자동)")
	dataDir := flag.String("data-dir", "", "데이터 폴더 (기본: 실행 파일 옆 ./data)")
	headless := flag.Bool("headless", false, "창을 열지 않고 서버만 실행")
	flag.Parse()

	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exeDir := filepath.Dir(exe)

	dir := *dataDir
	if dir == "" {
		if cfg := storage.LoadConfigPath(exeDir); cfg != "" {
			dir = cfg
		} else {
			dir = filepath.Join(exeDir, "data")
		}
	}
	st, err := storage.New(dir, exeDir)
	if err != nil {
		log.Printf("[경고] 지정된 데이터 폴더(%s) 접근 실패: %v, 기본 ./data 폴더로 시도합니다.", dir, err)
		dir = filepath.Join(exeDir, "data")
		st, err = storage.New(dir, exeDir)
		if err != nil {
			log.Fatalf("데이터 폴더 생성 실패: %v", err)
		}
	}
	static, err := webui.FS()
	if err != nil {
		log.Fatal(err)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatalf("포트 열기 실패: %v", err)
	}
	baseURL := "http://" + ln.Addr().String()
	url := baseURL + "/index.html"
	log.Printf("서버 시작: %s (데이터: %s)", url, st.Dir())

	handler, tracker := server.New(static, st)
	go func() {
		if err := http.Serve(ln, handler); err != nil {
			log.Fatal(err)
		}
	}()

	// 1) 로컬 HTTP 서버가 100% 응답 가능한 상태가 될 때까지 먼저 확인한 뒤 브라우저 창을 연다!
	waitForServerReady(baseURL+"/api/health", 5*time.Second)

	if *headless {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
		return
	}

	// 2) 앱 창 실행
	_, err = launcher.Open(url)
	if err != nil {
		log.Printf("창 열기 실패, 브라우저로 직접 접속하세요: %s (%v)", url, err)
	}

	// 3) Edge/Chrome 이 백그라운드 브로커 프로세스로 창 생성을 위임하고 자식 PID 가 0.1초 만에 종료되더라도
	//    절대 서버를 끄지 않고, 실제 브라우저 페이지의 하트비트(/api/ping, /api/bye)를 기준으로 서버 수명을 유지한다!
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	startTime := time.Now()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sig:
			return
		case <-ticker.C:
			if tracker.IsConnected() {
				if tracker.ShouldExit() {
					return
				}
			} else if time.Since(startTime) > 60*time.Second {
				// 60초 동안 아무 브라우저도 접속하지 않은 경우에만 자동 종료
				return
			}
		}
	}
}
