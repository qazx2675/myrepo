// Package color 는 관리 노드(관리서버)에서 실행하는 ip-change-engine 의 출력에
// 최소한의 ANSI 색상을 입혀 가독성을 높입니다.
//
// 이모지는 쓰지 않고 색상 코드만 사용합니다. 표준 출력이 터미널이 아니거나(파이프로
// 리다이렉트되는 경우 등) NO_COLOR 환경변수가 설정돼 있으면 자동으로 색을 끕니다.
// 대상 노드로 주입되는 apply_body.sh 출력에는 적용하지 않습니다 — gossh 의
// "<host>: <내용>" 파싱과 요구된 "hostname ip gateway" 출력 형식을 건드리지 않기 위함입니다.
package color

import "os"

var enabled = isTerminal(os.Stdout) && os.Getenv("NO_COLOR") == ""

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	cyan   = "\x1b[36m"
)

func wrap(code, s string) string {
	if !enabled {
		return s
	}
	return code + s + reset
}

// Green 은 성공(OK) 항목에 씁니다.
func Green(s string) string { return wrap(green, s) }

// Yellow 는 스킵/경고 항목에 씁니다.
func Yellow(s string) string { return wrap(yellow, s) }

// Cyan 은 정보 안내/구간 제목에 씁니다.
func Cyan(s string) string { return wrap(cyan, s) }

// BoldRed 는 실패(FAIL)/오류처럼 눈에 띄어야 하는 항목에 씁니다.
func BoldRed(s string) string { return wrap(bold+red, s) }

// BoldCyan 은 제목/구간 헤더에 씁니다.
func BoldCyan(s string) string { return wrap(bold+cyan, s) }
