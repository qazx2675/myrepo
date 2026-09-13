//go:build !windows

// copy-widget은 Windows 전용 오버레이 위젯이다. 다른 OS에서는 빌드 확인용
// 스텁만 제공한다 (CI가 리눅스에서도 go build ./... 를 돌릴 수 있도록).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "copy-widget은 Windows 전용 프로그램입니다.")
	os.Exit(1)
}
