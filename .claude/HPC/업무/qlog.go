// hpcbot — 질문 code 발급·공용 로그 기록·회전
package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultLogPath = "/tmp/hpcbot/query.log"
	maxLogSize     = 10 << 20 // 10MB 를 넘으면 query.log.1 하나만 남기고 회전
)

// LogEntry 는 query.log 의 한 줄(JSON Lines)이다.
type LogEntry struct {
	Time      string `json:"time"`
	User      string `json:"user"`
	Host      string `json:"host"`
	Code      string `json:"code"`
	Input     string `json:"input"`
	Converted string `json:"converted,omitempty"`
	Mode      string `json:"mode"` // answer|candidates|nomatch|choice
	ID        string `json:"id,omitempty"`
	Section   string `json:"section,omitempty"`
	Title     string `json:"title,omitempty"`
	Top3      []Hit  `json:"top3,omitempty"`
	Answer    string `json:"answer,omitempty"`
	Jev       bool   `json:"jev"`
}

// QLog 는 code 발급과 로그 기록을 맡는다.
type QLog struct {
	path string
	used map[string]bool
}

var codeRe = regexp.MustCompile(`"code":"(\d{4})"`)

// NewQLog 는 $HPCBOT_LOG(없으면 /tmp/hpcbot/query.log)를 쓰고, 이미 있는 code 를 읽어 둔다.
func NewQLog() *QLog {
	p := os.Getenv("HPCBOT_LOG")
	if p == "" {
		p = defaultLogPath
	}
	q := &QLog{path: p, used: map[string]bool{}}
	for _, f := range []string{p, p + ".1"} {
		if b, err := os.ReadFile(f); err == nil {
			for _, m := range codeRe.FindAllSubmatch(b, -1) {
				q.used[string(m[1])] = true
			}
		}
	}
	return q
}

// NewCode 는 1000~9999 중 로그에 없는 4자리 code 를 발급한다.
func (q *QLog) NewCode() string {
	for range 100000 {
		c := fmt.Sprint(1000 + rand.IntN(9000))
		if !q.used[c] {
			q.used[c] = true
			return c
		}
	}
	// 9000개가 모두 소진된 극단적 경우: 중복을 허용한다
	return fmt.Sprint(1000 + rand.IntN(9000))
}

// Write 는 항목을 공용 로그에 한 줄로 추가한다 (flock + O_APPEND, 필요하면 회전).
func (q *QLog) Write(e LogEntry) error {
	dir := filepath.Dir(q.path)
	if _, err := os.Stat(dir); err != nil {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return err
		}
		_ = os.Chmod(dir, 0o777|os.ModeSticky)
	}
	if e.Time == "" {
		e.Time = time.Now().Format(time.RFC3339)
	}
	if e.User == "" {
		e.User = os.Getenv("USER")
		if e.User == "" {
			e.User = os.Getenv("USERNAME")
		}
	}
	if e.Host == "" {
		e.Host, _ = os.Hostname()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	unlock, err := lockFile(q.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	if st, err := os.Stat(q.path); err == nil && st.Size() > maxLogSize {
		if err := os.Rename(q.path, q.path+".1"); err != nil {
			return err
		}
		_ = os.Chmod(q.path+".1", 0o666)
	}
	_, existed := os.Stat(q.path)
	f, err := os.OpenFile(q.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		return err
	}
	defer f.Close()
	if existed != nil {
		_ = f.Chmod(0o666)
	}
	_, err = f.Write(line)
	return err
}

// splitLabel 은 "§6.1 GPU 드라이버 설치" 를 ("§6.1", "GPU 드라이버 설치") 로 나눈다.
func splitLabel(label string) (section, title string) {
	sec, title, ok := strings.Cut(label, " ")
	if !ok {
		return "", label
	}
	return sec, title
}
