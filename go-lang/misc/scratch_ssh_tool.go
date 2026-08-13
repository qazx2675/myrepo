// 오늘테스트해볼거 - gossh 초기 스캐폴딩 스크래치 (SSH 병렬 실행 도구 초기 버전)
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// (초기 스크래치 버전 - 이후 gossh.go 로 발전)
func main() {
	_ = bufio.NewScanner
	_ = flag.String
	_ = log.Fatal
	_ = filepath.Join
	_ = strings.TrimSpace
	_ = sync.WaitGroup{}
	_ = time.Second
	_ = ssh.Password
	fmt.Println("scratch")
}
