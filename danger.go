package main

// 위험 명령 판정(isDangerousCommand)과 -dnlgjawkrdjqghkrdls 의 "대상 호스트 재입력 대조" 로직.
// main.go 의 위험 확인 흐름에서 호출한다. 입출력이 없는 순수 함수 위주라 danger_test.go 로 표 기반 테스트를 한다.

import (
	"bufio"
	"regexp"
	"strings"
	"unicode"
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
func parseConfirmHosts(lines []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range lines {
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
