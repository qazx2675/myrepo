// log.go - auto_setup.log 기록 (상태 변화·실행·오류만, 10MB 넘으면 .1 로 1개 보관)
package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const logMax = 10 * 1024 * 1024

var logMu sync.Mutex

func logf(format string, a ...interface{}) {
	logMu.Lock()
	defer logMu.Unlock()
	p := logPath()
	if st, err := os.Stat(p); err == nil && st.Size() >= logMax {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, a...))
	f.Close()
}
