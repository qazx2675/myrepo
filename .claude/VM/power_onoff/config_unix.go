//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

// checkOwnerRoot 는 파일이 root(uid 0) 소유인지 확인한다.
func checkOwnerRoot(fi os.FileInfo, path string) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("설정 파일 소유자를 확인할 수 없습니다: %s", path)
	}
	if st.Uid != 0 {
		return fmt.Errorf("설정 파일이 root 소유가 아닙니다 (uid=%d): %s", st.Uid, path)
	}
	return nil
}
