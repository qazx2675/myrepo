// runner.go - os_check 실행(-auto), os6 gossh 래퍼 생성, code 파일(codes/<code>.txt) 작성
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	reportStart = "############### 결과 리포트 ###############"
	reportEnd   = "###########################################"
)

var (
	// 요약 블록 머리줄: LDAP / SPLUNK / 커널 / <infra> infra 커널
	excerptHeadRe = regexp.MustCompile(`^===== (LDAP 정보|SPLUNK 정보|커널 버전|.+ infra 커널 버전)`)
	// 블록 닫는 줄: "=" 만으로 이루어진 줄
	excerptEndRe = regexp.MustCompile(`^=+$`)
	// 로그에 색이 섞여 들어오는 경우 대비
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

type realRunner struct{}

func NewRealRunner() Runner { return realRunner{} }

func (realRunner) Run(j *Job, hosts []string, hasOS6 bool) (RunResult, error) {
	if os_check_sh == "" {
		return RunResult{}, errors.New("os_check_sh 가 비어 있습니다")
	}
	if hasOS6 && os6_mgmt == "" {
		return RunResult{}, errors.New("os6_mgmt 가 비어 있습니다 (os6 호스트 포함 run)")
	}
	if err := ensureDirs(); err != nil {
		return RunResult{}, err
	}
	code, err := newCode()
	if err != nil {
		return RunResult{}, err
	}
	dir := filepath.Join(runsDir(), code)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return RunResult{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(strings.Join(hosts, "\n")+"\n"), 0644); err != nil {
		return RunResult{}, err
	}
	// os_check 가 현재 디렉터리의 dhcp.sh 를 쓰므로 원본 위치로 링크 (없으면 생략)
	if src := firstExisting(dhcpCandidates(os_check_sh)); src != "" {
		_ = os.Symlink(src, filepath.Join(dir, "dhcp.sh"))
	}

	env := os.Environ()
	if hasOS6 {
		if err := writeOS6Wrapper(); err != nil {
			return RunResult{}, err
		}
		env = append(env, "PATH="+binDir()+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	logFile := filepath.Join(dir, "os_check.log")
	lf, err := os.Create(logFile)
	if err != nil {
		return RunResult{}, err
	}
	cmd := exec.Command("bash", os_check_sh, "-auto", j.User, "targets.txt")
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = lf
	cmd.Stderr = lf
	logf("os_check 실행: code=%s user=%s %d대 os6=%v", code, j.User, len(hosts), hasOS6)
	if err := cmd.Run(); err != nil {
		logf("[X] os_check 종료 오류: code=%s: %v", code, err)
	}
	lf.Close()

	if err := postRunHook(dir, j, hosts); err != nil {
		logf("[X] postRunHook: %v", err)
	}

	logText, _ := os.ReadFile(logFile)
	post, postErr := os.ReadFile(filepath.Join(dir, "check.res_"+j.User+"_postapply"))
	text, abnormal := buildCodeText(string(logText), string(post), postErr == nil, hosts, logFile)
	if err := os.WriteFile(codePath(code), []byte(text), 0644); err != nil {
		return RunResult{Code: code}, err
	}
	processed := parsePostapplyHosts(string(post), hosts)
	if abnormal {
		logf("[!] os_check 비정상 종료(결과 리포트 없음): code=%s", code)
	}
	return RunResult{Processed: processed, Abnormal: abnormal, Code: code, Failed: parsePostapplyFailed(string(post), hosts)}, nil
}

// postRunHook: run 전/후 단계 확장 지점(GPU 드라이버 등). 현재는 아무 것도 하지 않는다.
func postRunHook(dir string, j *Job, hosts []string) error { return nil }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ---- os6 gossh 래퍼 ----

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func os6WrapperScript() string {
	return `#!/bin/bash
# gossh - os6_mgmt 경유 gossh 래퍼 (auto_setup 이 생성, 직접 수정 금지)
mgmt=` + shq(os6_mgmt) + `
gossh=` + shq(os6_gossh) + `
q=""
wfile=""
while [ $# -gt 0 ]; do
	if [ "$1" = "-w" ] && [ $# -ge 2 ] && [ -z "$wfile" ]; then
		wfile=$2
		q="$q"' -w "$f"'
		shift 2
		continue
	fi
	q="$q $(printf '%q' "$1")"
	shift
done
[ -n "$wfile" ] || wfile=/dev/null
exec ssh "$mgmt" "f=\$(mktemp); cat > \$f; $gossh$q; rc=\$?; rm -f \$f \${f}_*; exit \$rc" < "$wfile"
`
}

func writeOS6Wrapper() error {
	if err := os.MkdirAll(binDir(), 0755); err != nil {
		return err
	}
	p := filepath.Join(binDir(), "gossh")
	if err := os.WriteFile(p, []byte(os6WrapperScript()), 0755); err != nil {
		return err
	}
	return os.Chmod(p, 0755)
}

// ---- code 파일 ----

// extractReport: 결과 리포트 블록(시작줄 ~ 마지막 닫는줄). 완전하면 ok=true.
// 시작줄만 있으면 로그 끝까지 발췌하고 ok=false, 시작줄이 없으면 ("", false).
func extractReport(lines []string) (string, bool) {
	start := -1
	for i, l := range lines {
		if l == reportStart {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := -1
	for i := len(lines) - 1; i > start; i-- {
		if lines[i] == reportEnd {
			end = i
			break
		}
	}
	if end < 0 {
		return strings.TrimRight(strings.Join(lines[start:], "\n"), "\n"), false
	}
	return strings.Join(lines[start:end+1], "\n"), true
}

// extractSummaries: LDAP/SPLUNK/커널 블록(머리줄 ~ 닫는 "=====" 줄) 을 나타난 순서대로
func extractSummaries(lines []string) []string {
	var blocks []string
	for i := 0; i < len(lines); i++ {
		if !excerptHeadRe.MatchString(lines[i]) {
			continue
		}
		j := i + 1
		for j < len(lines) && !excerptEndRe.MatchString(lines[j]) {
			if excerptHeadRe.MatchString(lines[j]) { // 닫는줄 없이 다음 블록 시작
				break
			}
			j++
		}
		if j < len(lines) && excerptEndRe.MatchString(lines[j]) {
			blocks = append(blocks, strings.Join(lines[i:j+1], "\n"))
			i = j
		} else {
			blocks = append(blocks, strings.TrimRight(strings.Join(lines[i:j], "\n"), "\n"))
			i = j - 1
		}
	}
	return blocks
}

// lineHost: "host: ..." / "host : ..." 줄의 호스트명. 형식이 아니면 "".
func lineHost(line string) string {
	i := strings.Index(line, ":")
	if i <= 0 {
		return ""
	}
	h := strings.TrimSpace(line[:i])
	if h == "" || strings.ContainsAny(h, " \t") {
		return ""
	}
	return h
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(ansiRe.ReplaceAllString(s, ""), "\r", "")
	return strings.Split(s, "\n")
}

// parsePostapplyHosts: postapply 결과에 나온 호스트 (등장 순서, 요청 호스트로 한정)
func parsePostapplyHosts(post string, hosts []string) []string {
	want := map[string]bool{}
	for _, h := range hosts {
		want[h] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, l := range splitLines(post) {
		h := lineHost(l)
		if h != "" && want[h] && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// postapplyFailLines: postapply 의 FAIL 포함 줄과 FAIL 이 있는 호스트 집합
func postapplyFailLines(post string) ([]string, map[string]bool) {
	failHosts := map[string]bool{}
	var failLines []string
	for _, l := range splitLines(post) {
		if !strings.Contains(strings.ToUpper(l), "FAIL") {
			continue
		}
		failLines = append(failLines, l)
		if h := lineHost(l); h != "" {
			failHosts[h] = true
		}
	}
	return failLines, failHosts
}

// parsePostapplyFailed: postapply 에 FAIL 줄이 있는 호스트 (등장 순서, 요청 호스트로 한정)
func parsePostapplyFailed(post string, hosts []string) []string {
	_, failHosts := postapplyFailLines(post)
	var out []string
	for _, h := range parsePostapplyHosts(post, hosts) {
		if failHosts[h] {
			out = append(out, h)
		}
	}
	return out
}

// settingCheckText: "설정체크 (설정 수정 후 재점검)" 섹션 (FAIL 줄 / NO FAIL 한 줄)
func settingCheckText(post string, havePost bool, hosts []string) string {
	var sb strings.Builder
	sb.WriteString("설정체크 (설정 수정 후 재점검)")
	if !havePost {
		sb.WriteString("\n(재점검 결과 없음)")
		return sb.String()
	}
	failLines, failHosts := postapplyFailLines(post)
	var noFail []string
	for _, h := range parsePostapplyHosts(post, hosts) {
		if !failHosts[h] {
			noFail = append(noFail, h)
		}
	}
	for _, l := range failLines {
		sb.WriteString("\n" + l)
	}
	if len(noFail) > 0 {
		fmt.Fprintf(&sb, "\nNO FAIL : %s (%d대)", strings.Join(noFail, " "), len(noFail))
	}
	return sb.String()
}

// buildCodeText: codes/<code>.txt 본문과 비정상 여부(결과 리포트 블록이 없거나 불완전)
func buildCodeText(logText, post string, havePost bool, hosts []string, logFile string) (string, bool) {
	var parts []string

	report, ok := extractReport(splitLines(logText))
	if report != "" {
		parts = append(parts, report)
	}

	parts = append(parts, settingCheckText(post, havePost, hosts))

	parts = append(parts, extractSummaries(splitLines(logText))...)
	parts = append(parts, "원본 : "+logFile)
	return strings.Join(parts, "\n\n") + "\n", !ok
}
