// mockbmc 는 testdata 트리를 Redfish BMC 처럼 서빙하는 개발·검증용 서버입니다 (실제 BMC 아님).
// biostool 의 바이너리 e2e 검증(단계 8)에 쓰며, 포트만 바꿔 여러 개를 띄울 수 있습니다.
//
// 사용법:
//
//	MOCKBMC_PASS='...' mockbmc -dir testdata/dell-r660 -listen 127.0.0.1:8443 -user admin
//
// 종료(Ctrl-C / SIGTERM) 시 요청·세션·로그 경로 접근·쓰기 호출 집계를 한 줄 출력합니다.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"biostool/internal/mockbmc"
)

func main() {
	dir := flag.String("dir", "", "서빙할 트리 디렉터리 (예: testdata/dell-r660, redfish/v1.json 이 있는 곳)")
	listen := flag.String("listen", "127.0.0.1:8443", "수신 주소 (host:포트)")
	user := flag.String("user", "admin", "BMC 계정 ID")
	pass := flag.String("pass", "", "BMC 비밀번호 (프로세스 목록에 보이므로 환경변수 MOCKBMC_PASS 권장)")
	noSess := flag.Bool("no-sessions", false, "세션 서비스 없는 BMC 흉내 (Basic 인증 폴백 시험)")
	verbose := flag.Bool("v", false, "요청마다 한 줄 출력")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "오류: -dir 이 필요합니다 (예: -dir testdata/dell-r660)")
		os.Exit(1)
	}
	if *pass == "" {
		*pass = os.Getenv("MOCKBMC_PASS")
	}
	if *pass == "" {
		fmt.Fprintln(os.Stderr, "오류: 비밀번호가 필요합니다 (환경변수 MOCKBMC_PASS 또는 -pass)")
		os.Exit(1)
	}
	opts := mockbmc.Options{User: *user, Pass: *pass, NoSessions: *noSess}
	if *verbose {
		opts.Logf = func(f string, a ...interface{}) { fmt.Printf(f+"\n", a...) }
	}
	srv, err := mockbmc.New(*dir, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		os.Exit(1)
	}
	addr, err := srv.ListenTLS(*listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "오류:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "mockbmc: %s 을(를) https://%s 에서 서빙합니다 (계정 %s, 자체서명 인증서, Ctrl-C 로 종료)\n", *dir, addr, *user)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	srv.Close()

	ss := srv.Sessions()
	fmt.Printf("MOCKBMC_SUMMARY calls=%d sessions_created=%d sessions_deleted=%d login_failed=%d log_hits=%d writes=%d\n",
		len(srv.Calls()), ss.Created, ss.Deleted, ss.LoginFailed, srv.LogHits(), srv.Writes())
}
