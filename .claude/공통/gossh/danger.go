package main

// 위험 명령 판정(isDangerousCommand)과 -dnlgjawkrdjqghkrdls 의 "대상 호스트 재입력 대조" 로직.
// main.go 의 위험 확인 흐름에서 호출한다. 입출력이 없는 순수 함수 위주라 danger_test.go 로 표 기반 테스트를 한다.

import (
	"bufio"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ★ 기존 위험어 7개는 예전 동작 그대로 "소문자 부분일치"로 유지한다(호스트명·인자에 섞여도 거부하는 보수적 규칙).
var legacyDangerWords = []string{"reboot", "poweroff", "shutdown", "halt", "init 0", "init 6", "ddc"}

// ★ 새 위험어는 "독립된 명령"일 때만 감지한다. 단어 경계만 보면 `cat parted.log` 의 parted 같은 인자도 걸리므로
// "명령 위치"에 올 때만 명령으로 본다.
//   명령 위치 = 문자열/줄 시작, 또는 ; | & ( { ` ' " $( 바로 뒤(공백 허용).
//              그 앞에 sudo/xargs/exec/nohup/time/env/nice/command 래퍼(와 그 옵션), VAR=값 접두가 있어도 된다.
//              경로가 붙은 형태(/usr/sbin/mkfs.xfs)도 명령으로 본다.
//   끝 경계   = 문자열 끝, 또는 공백 ; | & ( ) ` ' "
// 따라서 `departed01`, base64 데이터 속 글자, `cat parted.log`, `echo mkfs` 는 위험으로 보지 않는다.
// (알려진 한계: `sudo -u root wipefs` 처럼 래퍼 옵션이 값을 받는 형태, `ssh host mkfs` 처럼 다른 명령의 인자로
//  넘기는 형태는 놓칠 수 있다. 오탐을 줄이려는 의도적 선택.)
const (
	dgCmdStart = "(?:^|[;|&(`'\"{\\n]|\\$\\()\\s*" +
		"(?:(?:\\w+=\\S*\\s+)|(?:(?:sudo|xargs|exec|nohup|time|env|nice|command)\\s+(?:-\\S+\\s+)*))*" +
		"(?:[^\\s;|&()`'\"]*/)?"
	dgEnd = "(?:$|[\\s;|&()`'\"])"
	// 같은 명령 안(구분자 ; | & 줄바꿈 전)에서 인자를 건너뛰는 조각
	dgArgs = "(?:[^;|&\\n]*\\s)?"
)

var dangerPatterns = []*regexp.Regexp{
	// A 즉시 재시작·정지
	regexp.MustCompile(dgCmdStart + `kexec` + dgEnd),
	regexp.MustCompile(dgCmdStart + `systemctl\s+(?:-\S+\s+)*(?:kexec|rescue|emergency)` + dgEnd),
	regexp.MustCompile(dgCmdStart + `systemctl\s+(?:-\S+\s+)*isolate\s+(?:rescue|emergency)(?:\.target)?` + dgEnd),
	regexp.MustCompile(dgCmdStart + `(?:tel)?init\s+1` + dgEnd),
	regexp.MustCompile(dgCmdStart + `ipmitool\s+` + dgArgs + `power\s+(?:off|reset|cycle)` + dgEnd),
	regexp.MustCompile(`(?:^|[\s;|&()` + "`" + `'"])(?:\S*/)?sysrq-trigger` + dgEnd),
	// B 디스크·데이터 파괴
	regexp.MustCompile(dgCmdStart + `mkfs(?:\.[a-z0-9]+)?` + dgEnd),
	regexp.MustCompile(dgCmdStart + `dd\s+` + dgArgs + `of=/dev/`),
	regexp.MustCompile(dgCmdStart + `(?:wipefs|fdisk|parted|sgdisk|shred|lvremove|vgremove|pvremove)` + dgEnd),
	// rm -rf / : 루트 자체(/ 또는 /*)일 때만. /tmp/x 같은 하위 경로는 제외. 재귀 옵션(-r/-R/--recursive)이 있어야 한다.
	regexp.MustCompile(dgCmdStart + `rm\s+(?:-\S+\s+)*?(?:-[a-z]*r[a-z]*|--recursive)\s+(?:-\S+\s+)*/\*?` + dgEnd),
	// C 접속 끊김
	regexp.MustCompile(dgCmdStart + `ifdown` + dgEnd),
	regexp.MustCompile(dgCmdStart + `nmcli\s+` + dgArgs + `down` + dgEnd),
	regexp.MustCompile(dgCmdStart + `ip\s+(?:-\S+\s+)*link\s+set\s+` + dgArgs + `down` + dgEnd),
	regexp.MustCompile(dgCmdStart + `systemctl\s+(?:-\S+\s+)*stop\s+(?:\S+\s+)*?(?:sshd|network|networkmanager)(?:\.service)?` + dgEnd),
}

// isDangerousCommand 는 명령어가 위험 작업(종료·재부팅·디스크 파괴·접속 끊김 등)인지 판정한다.
func isDangerousCommand(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, w := range legacyDangerWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	for _, re := range dangerPatterns {
		if re.MatchString(lower) {
			return true
		}
	}
	return false
}

// parseConfirmHosts 는 재입력된 줄들을 호스트 목록으로 바꾼다.
// 구분자는 공백·탭·쉼표·| 이고, 각 토큰은 -w 와 같은 expandHostLine 으로 범위식을 펼친다. 중복은 제거한다.
// -w 파일처럼 # 으로 시작하는 줄은 주석으로 보고 건너뛴다(호스트 파일 내용을 그대로 붙여 넣어도 일치하게).
func parseConfirmHosts(lines []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, tok := range strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == '|' || unicode.IsSpace(r)
		}) {
			for _, h := range expandHostLine(tok) {
				if !seen[h] {
					seen[h] = true
					out = append(out, h)
				}
			}
		}
	}
	return out
}

// diffHostSets 는 순서·중복을 무시하고 두 호스트 집합의 차이를 돌려준다. 둘 다 비어 있으면 완전일치다.
func diffHostSets(target, input []string) (onlyInput, onlyTarget []string) {
	t := map[string]bool{}
	for _, h := range target {
		t[h] = true
	}
	in := map[string]bool{}
	for _, h := range input {
		in[h] = true
		if !t[h] {
			onlyInput = append(onlyInput, h)
		}
	}
	for _, h := range target {
		if !in[h] {
			onlyTarget = append(onlyTarget, h)
		}
	}
	return
}

// readConfirmLines 는 빈 줄(공백만 있는 줄) 또는 EOF 까지 줄 단위로 읽는다.
func readConfirmLines(r *bufio.Reader) []string {
	var lines []string
	for {
		s, err := r.ReadString('\n')
		if strings.TrimSpace(s) == "" {
			break // 빈 줄 또는 EOF
		}
		lines = append(lines, s)
		if err != nil {
			break // 마지막 줄에 개행이 없던 EOF
		}
	}
	return lines
}

/// confirmLineEditor 는 터미널에서 줄 버퍼링 없이 한 바이트씩 읽은 재입력을 줄로 조립한다(readConfirmTTY 가 씀).
// 터미널의 줄 단위 입력은 한 줄 4095자에서 잘리고, CRLF 로 붙여 넣으면 줄마다 빈 줄이 생겨 첫 줄에서 끝났다.
// 그래서 CR·LF·CRLF 를 모두 줄바꿈 하나로 보고, 에코·백스페이스·Ctrl+U 는 여기서 직접 처리한다.
// 브래킷 붙여넣기(ESC[200~ ... ESC[201~) 안의 빈 줄은 입력 끝으로 보지 않는다.
type confirmLineEditor struct {
	lines   []string
	cur     []byte
	lastCR  bool   // 바로 앞 바이트가 CR 이면 뒤따르는 LF 는 같은 줄바꿈이다
	esc     int    // 0 보통, 1 ESC 직후, 2 ESC [ 또는 ESC O 시퀀스 안(방향키 등은 버린다)
	escArg  []byte // 시퀀스 인자(붙여넣기 시작 200~ / 끝 201~ 판별용)
	inPaste bool   // 브래킷 붙여넣기 안
}

// feed 가 돌려주는 사건
const (
	confirmNone  = iota
	confirmBlank // 빈 줄(공백만 있는 줄 포함)에서 Enter (붙여넣기 밖)
	confirmEOF   // 빈 줄에서 Ctrl+D
	confirmIntr  // Ctrl+C 또는 Ctrl+\
)

// feed 는 한 바이트를 처리하고 화면에 찍을 에코와 사건을 돌려준다.
func (e *confirmLineEditor) feed(b byte) (echo string, ev int) {
	cr := e.lastCR
	e.lastCR = false
	switch e.esc {
	case 1:
		if b == '[' || b == 'O' {
			e.esc = 2
			e.escArg = e.escArg[:0]
			return "", confirmNone
		}
		e.esc = 0 // ESC 단독(또는 Alt+키): ESC 만 버리고 이 바이트는 보통대로 처리
	case 2:
		if b >= 0x40 && b <= 0x7e {
			e.esc = 0
			if b == '~' {
				switch string(e.escArg) {
				case "200":
					e.inPaste = true
				case "201":
					e.inPaste = false
				}
			}
		} else if len(e.escArg) < 16 {
			e.escArg = append(e.escArg, b)
		}
		return "", confirmNone
	}
	switch {
	case b == '\r' || b == '\n':
		if b == '\n' && cr {
			return "", confirmNone // CRLF 의 LF
		}
		e.lastCR = b == '\r'
		line := string(e.cur)
		e.cur = e.cur[:0]
		if strings.TrimSpace(line) != "" {
			e.lines = append(e.lines, line)
			return "\n", confirmNone
		}
		if e.inPaste {
			return "\n", confirmNone // 붙여 넣은 목록 안의 빈 줄
		}
		return "\n", confirmBlank
	case b == 0x03 || b == 0x1c: // Ctrl+C, Ctrl+\ (재입력 동안 ISIG 를 꺼서 바이트로 들어온다)
		return "", confirmIntr
	case b == 0x7f || b == 0x08: // 백스페이스
		if len(e.cur) == 0 {
			return "", confirmNone
		}
		_, n := utf8.DecodeLastRune(e.cur)
		e.cur = e.cur[:len(e.cur)-n]
		return "\b \b", confirmNone
	case b == 0x15: // Ctrl+U: 현재 줄 지우기
		echo = strings.Repeat("\b \b", utf8.RuneCount(e.cur))
		e.cur = e.cur[:0]
		return echo, confirmNone
	case b == 0x04: // Ctrl+D: 빈 줄에서만 입력 끝
		if len(e.cur) == 0 {
			return "", confirmEOF
		}
		return "", confirmNone
	case b == 0x1b:
		e.esc = 1
		return "", confirmNone
	case b == '\t' || b >= 0x20:
		e.cur = append(e.cur, b)
		return string([]byte{b}), confirmNone // UTF-8 바이트를 그대로 찍는다(string(b) 는 룬 변환이라 한글이 깨짐)
	}
	return "", confirmNone // 그 밖의 제어문자(Ctrl+Z 등)는 무시
}

// finish 는 Enter 없이 끝난 마지막 줄까지 포함한 입력 줄을 돌려준다.
func (e *confirmLineEditor) finish() []string {
	if strings.TrimSpace(string(e.cur)) != "" {
		e.lines = append(e.lines, string(e.cur))
		e.cur = e.cur[:0]
	}
	return e.lines
}
