// notify.go - wall 알림 (root 로 PATH 의 wall 실행, 메시지는 stdin, 실패 시 로그만)
package main

import (
	"os"
	"os/exec"
	"strings"
)

type wallNotifier struct{}

// wallLocale: wall 에 넘길 UTF-8 로케일. cron 으로 뜬 데몬은 LANG 이 비어(C 로케일) wall 이 한글을
// \354\225\210 처럼 8진수로 바꿔 출력하므로, 이미 UTF-8 이면 그대로 쓰고 아니면 ko_KR.UTF-8 로 강제한다.
func wallLocale(env []string) []string {
	utf8 := func(k string) bool {
		for _, e := range env {
			if strings.HasPrefix(e, k+"=") {
				v := strings.ToUpper(e[len(k)+1:])
				return strings.Contains(v, "UTF-8") || strings.Contains(v, "UTF8")
			}
		}
		return false
	}
	out := make([]string, 0, len(env)+2)
	for _, e := range env {
		if strings.HasPrefix(e, "LC_ALL=") || strings.HasPrefix(e, "LANG=") || strings.HasPrefix(e, "LC_CTYPE=") {
			continue
		}
		out = append(out, e)
	}
	loc := "ko_KR.UTF-8"
	if utf8("LC_ALL") {
		loc = envValue(env, "LC_ALL")
	} else if utf8("LANG") {
		loc = envValue(env, "LANG")
	}
	return append(out, "LANG="+loc, "LC_ALL="+loc)
}

func envValue(env []string, k string) string {
	for _, e := range env {
		if strings.HasPrefix(e, k+"=") {
			return e[len(k)+1:]
		}
	}
	return ""
}

// Wall: 배너("Broadcast message from …" 와 빈 줄)를 빼고(-n, root 만 가능) 한 번에 내보낸다.
// -n 을 쓸 수 없으면(비root 등) 배너가 있는 일반 wall 로 다시 시도한다.
func (wallNotifier) Wall(msg string) {
	env := wallLocale(os.Environ())
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command("wall", args...)
		cmd.Env = env
		cmd.Stdin = strings.NewReader(msg)
		return cmd.CombinedOutput()
	}
	if _, err := run("-n"); err == nil {
		return
	}
	if out, err := run(); err != nil {
		logf("[X] wall 실패: %v %s", err, strings.TrimSpace(string(out)))
	}
}
