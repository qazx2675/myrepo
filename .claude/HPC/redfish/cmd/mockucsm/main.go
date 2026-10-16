// mockucsm 은 Cisco UCSM/CIMC XML API(POST /nuova) 흉내 서버입니다 (시연·검증용, 실제 장비 아님).
// aaaLogin / configResolveClass / aaaLogout 만 처리합니다. 포트만 바꿔 여러 개를 띄울 수 있습니다.
//
// 사용법:
//
//	mockucsm -listen 127.0.0.1:19301 -data testdata/mockucsm/single.json
//
// 종료(Ctrl-C / SIGTERM) 시 받은 요청 수를 한 줄 출력합니다.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"biostool/internal/mockucsm"
)

func main() {
	data := flag.String("data", "", "서버 정의 JSON 파일 (user, password, servers[])")
	listen := flag.String("listen", "127.0.0.1:19301", "수신 주소 (host:포트)")
	verbose := flag.Bool("v", false, "요청마다 한 줄 출력")
	flag.Parse()

	if *data == "" {
		fmt.Fprintln(os.Stderr, "오류: -data 가 필요합니다 (예: -data demo.json)")
		os.Exit(1)
	}
	srv, err := mockucsm.Load(*data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		os.Exit(1)
	}
	if *verbose {
		srv.Logf = func(f string, a ...interface{}) { fmt.Printf(f+"\n", a...) }
	}
	addr, err := srv.ListenTLS(*listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "mockucsm: %s 을(를) https://%s/nuova 에서 서빙합니다 (자체서명 인증서, Ctrl-C 로 종료)\n", *data, addr)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	srv.Close()
	fmt.Printf("MOCKUCSM_SUMMARY calls=%d\n", len(srv.Calls()))
}
