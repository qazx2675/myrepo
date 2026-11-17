// fakeawx 는 E2E 테스트용 가짜 AWX 서버다. 실제 AWX 대신 쓴다.
// 사용: fakeawx -listen 127.0.0.1:30099 -user u -pass p
package main

import (
	"flag"
	"log"
	"net/http"

	"awx_copy/internal/fakeawx"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:30099", "listen address")
	user := flag.String("user", "fake", "basic auth user")
	pass := flag.String("pass", "fake", "basic auth password")
	maxBody := flag.Int64("max-body", 0, "PATCH body limit (0 = none)")
	flag.Parse()
	s := fakeawx.New(*user, *pass)
	s.MaxBody = *maxBody
	log.Printf("fakeawx listening on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, s))
}
