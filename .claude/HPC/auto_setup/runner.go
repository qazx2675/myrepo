// runner.go - os_check 실행(-auto), os6 gossh 래퍼 생성, code 파일(codes/<code>.txt) 작성
package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// 실행 모드: 자동 흐름(배포 → 완료)과 수동 c 는 설정체크 + 설정수정, 수동 t 는 설정체크만 (환경설정 수정 안 함)
const (
	ModeCheck      = "check"
	modeApplyLabel = "설정체크 + 설정수정"
	modeCheckLabel = "설정체크만 (설정 수정 안 함)"
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

// checkModeUnsupported: 설정체크만(t, -auto-check) 을 이 체크스크립트가 지원하지 않으면 사유, 아니면 "".
// -auto 만 아는 스크립트(config_check.sh 등)에 -auto-check 를 주면 사용자 선택 메뉴에서 멈춘다.
// 스크립트를 읽을 수 없으면 판단하지 않는다 (막지 않음).
func checkModeUnsupported() string {
	b, err := os.ReadFile(os_check_sh)
	if err != nil || strings.Contains(string(b), "-auto-check") {
		return ""
	}
	return "os_check_sh 가 -auto-check 를 지원하지 않음 (설정체크만 t 불가 — c 로 실행하거나 -auto-check 를 지원하는 체크스크립트로 교체)"
}

func (realRunner) Run(j *Job, hosts []string, hasOS6 bool) (RunResult, error) {
	if os_check_sh == "" {
		return RunResult{}, errors.New("os_check_sh 가 비어 있습니다")
	}
	if j.RunMode == ModeCheck {
		if m := checkModeUnsupported(); m != "" {
			return RunResult{}, errors.New(m)
		}
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
		// 래퍼가 os8 호스트는 이 서버의 gossh 로, os6 호스트만 os6_mgmt 경유로 나눠 실행하도록 이번 run 의 os6 경로 호스트 목록을 넘긴다
		var o6 []string
		for _, h := range hosts {
			if jh := j.Hosts[h]; jh != nil && jh.Route == "os6" {
				o6 = append(o6, h)
			}
		}
		o6File := filepath.Join(dir, "os6_hosts.txt")
		if err := os.WriteFile(o6File, []byte(strings.Join(o6, "\n")+"\n"), 0644); err != nil {
			return RunResult{}, err
		}
		env = append(env, "PATH="+binDir()+string(os.PathListSeparator)+os.Getenv("PATH"), "AUTO_SETUP_OS6_HOSTS="+o6File)
	}

	logFile := filepath.Join(dir, "os_check.log")
	lf, err := os.Create(logFile)
	if err != nil {
		return RunResult{}, err
	}
	flag, resFile := "-auto", "check.res_"+j.User+"_postapply"
	if j.RunMode == ModeCheck { // 설정체크만: 환경설정 수정·재점검 없음 → 점검 결과 파일을 본다
		flag, resFile = "-auto-check", "check.res_"+j.User
	}
	cmd := exec.Command("bash", os_check_sh, flag, j.User, "targets.txt")
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = lf
	cmd.Stderr = lf
	logf("os_check 실행: code=%s user=%s %d대 os6=%v 모드=%s", code, j.User, len(hosts), hasOS6, modeLabel(j.RunMode))
	if err := cmd.Run(); err != nil {
		logf("[X] os_check 종료 오류: code=%s: %v", code, err)
	}
	lf.Close()

	if err := postRunHook(dir, j, hosts); err != nil {
		logf("[X] postRunHook: %v", err)
	}

	logText, _ := os.ReadFile(logFile)
	post, postErr := os.ReadFile(filepath.Join(dir, resFile))
	text, abnormal := buildCodeTextMode(j.RunMode, string(logText), string(post), postErr == nil, hosts, logFile)
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

// remoteBash: ssh 로 보내는 원격 명령을 `bash -c '<inner>'` 한 줄로 감싼다 (inner 의 작은따옴표는 shq 로 이스케이프).
// 원격 로그인 셸이 csh/tcsh 여도 작은따옴표 안은 글자 그대로 bash 에 넘어가므로 $(...)·${f}·$? 를 bash 가 해석한다.
// inner 에 줄바꿈이 있으면 안 된다 (csh 는 작은따옴표 안의 줄바꿈을 받지 않음).
// 아래 bash 래퍼 두 개(os6WrapperScript, chgOS6WrapperScript)는 같은 감싸기를 bash 안에서 한다 (${c//\'/\'\\\'\'}).
//
// tcsh 는 작은따옴표 안의 `!` 도 history 치환해 "Event not found" 로 깨진다. inner 에 `!` 가 있을 때만
// bash -c "`echo <base64(두 글자마다 .)> | tr -d . | base64 -d`" 로 보낸다 — `$`·`!`·작은따옴표·백슬래시가 없어
// sh·bash·csh·tcsh 로그인 셸 모두에서 같은 inner 가 bash 에 전달된다 (원격 stdin 은 inner 가 그대로 쓴다).
// '.' 은 gossh 를 거치는 경로(client.go remoteRun)에서 base64 가 위험어(reboot·halt·ddc 등)나 /user/ 를 우연히 만들지 않게 한다.
// `!` 가 없으면 기존 형태(bash -c '…') 그대로. 래퍼 두 개도 `!` 가 있으면 같은 형태를 bash 로 만든다.
func remoteBash(inner string) string {
	if !strings.Contains(inner, "!") {
		return "bash -c " + shq(inner)
	}
	return `bash -c "` + "`echo " + dotOut(base64.StdEncoding.EncodeToString([]byte(inner))) + " | tr -d . | base64 -d`" + `"`
}

// os6WrapperScript: 원격 명령은 bash -c '…' 로 감싸 보낸다 (remoteBash 와 같은 형태, os6_mgmt 로그인 셸이 csh 여도 동작).
// os6_gossh 경로는 원격 bash 용으로 한 번 더 인용한다(shq(shq(…))) — 공백·' 가 있어도 깨지지 않게.
//
// os6 호스트도 os8 호스트와 똑같이 처리되도록 (v0.8.7):
//   - gossh 가 -w 목록 옆에 남기는 결과 파일(<목록>_res_off·_res_refsed·_os_install·_nosvrauto·_res_cancel 등)을 os6_mgmt 에서
//     가져와 원래 목록 옆에 만든다 (체크스크립트가 접속불가·설치중·nosvrauto 를 이 파일로 판정한다). 전에는 os6_mgmt 에서 지워져
//     os6 호스트는 접속불가여도 OFF 가 아닌 NONE, 설치중이어도 INST 가 아니었다.
//   - AUTO_SETUP_OS6_HOSTS(이번 run 의 os6 경로 호스트 목록 파일)가 있고 이 서버의 gossh(realGossh)를 찾았으면 -w 목록을 경로별로 나눠
//     os8 호스트는 이 서버의 gossh 로, os6 호스트만 os6_mgmt 경유로 실행하고 출력·결과 파일을 합친다
//     (전에는 run 에 os6 호스트가 하나라도 있으면 os8 호스트까지 전부 os6_mgmt 경유 — os6_mgmt 에서 안 닿는 os8 호스트는 실패).
func os6WrapperScript(realGossh string) string {
	return `#!/bin/bash
# gossh - os6_mgmt 경유 gossh 래퍼 (auto_setup 이 생성, 직접 수정 금지)
mgmt=` + shq(os6_mgmt) + `
gossh=` + shq(shq(os6_gossh)) + `
real=` + shq(realGossh) + `
mark='==AUTO_SETUP_GOSSH_RES=='
args=("$@")
wi=-1
for ((i = 0; i < ${#args[@]}; i++)); do
	if [ "${args[i]}" = "-w" ] && [ $((i + 1)) -lt ${#args[@]} ]; then
		wi=$((i + 1))
		break
	fi
done

# remote <목록> <결과 접두> : 목록을 stdin 으로 넘겨 os6_mgmt 에서 gossh 실행. 출력은 그대로, 결과 파일은 <결과 접두>_* 로 가져온다
remote() {
	local list=$1 dest=$2 q="" i c bq e res rc suf b
	for ((i = 0; i < ${#args[@]}; i++)); do
		if [ $i -eq $wi ]; then
			q="$q"' "$f"'
			continue
		fi
		q="$q $(printf '%q' "${args[i]}")"
	done
	c="f=\$(mktemp); cat > \$f; $gossh$q; "'rc=$?; echo '"$mark"'; for x in "$f"_*; do [ -f "$x" ] && echo "${x#$f} $(base64 -w0 < "$x")"; done; rm -f "$f" "$f"_*; exit $rc'
	res=$(mktemp) || return 1
	if [[ $c == *'!'* ]]; then
		# ! 가 있으면 tcsh 로그인 셸이 작은따옴표 안에서도 history 치환하므로 base64 로 보낸다 (runner.go remoteBash 와 같은 형태)
		bq=$'\x60'
		e=$(printf %s "$c" | base64 -w0 | sed 's/../&./g')
		ssh "$mgmt" "bash -c \"${bq}echo $e | tr -d . | base64 -d${bq}\"" < "$list" | awk -v m="$mark" -v o="$res" "$filt"
		rc=${PIPESTATUS[0]}
	else
		c=${c//\'/\'\\\'\'}
		ssh "$mgmt" "bash -c '$c'" < "$list" | awk -v m="$mark" -v o="$res" "$filt"
		rc=${PIPESTATUS[0]}
	fi
	if [ $wi -ge 0 ]; then
		while read -r suf b; do
			case "$suf" in _*) printf '%s' "$b" | base64 -d > "${dest}${suf}" 2>/dev/null ;; esac
		done < "$res"
	fi
	rm -f "$res"
	return $rc
}
# 표지줄 전은 그대로 출력, 표지줄 뒤(결과 파일)는 o 로. 표지줄이 출력 마지막 줄에 붙어 오면 앞부분만 출력한다
filt='r { print > o; next } { i = index($0, m); if (i && substr($0, i) == m) { if (i > 1) printf "%s", substr($0, 1, i - 1); r = 1; next } print; fflush() }'

if [ $wi -lt 0 ]; then # -w 없음: 전부 os6_mgmt 경유 (stdin 없음)
	remote /dev/null /dev/null
	exit $?
fi
wfile=${args[wi]}
o6=${AUTO_SETUP_OS6_HOSTS:-}
if [ -z "$o6" ] || [ ! -f "$o6" ] || [ -z "$real" ] || [ ! -x "$real" ]; then # 경로 정보·이 서버 gossh 가 없으면 전부 os6_mgmt 경유
	remote "$wfile" "$wfile"
	exit $?
fi
L=$(mktemp) && R=$(mktemp) || exit 1
awk -v L="$L" -v R="$R" 'NR == FNR { s[$1] = 1; next } { h = $1 } h == "" { next } (h in s) { print > R; next } { print > L }' "$o6" "$wfile"
if [ ! -s "$R" ]; then # os6 호스트 없음: 이 서버의 gossh 그대로
	rm -f "$L" "$R"
	exec "$real" "$@"
fi
if [ ! -s "$L" ]; then # 전부 os6 호스트
	rm -f "$L" "$R"
	remote "$wfile" "$wfile"
	exit $?
fi
# 섞여 있음: os8 호스트는 이 서버에서, os6 호스트는 os6_mgmt 경유로 동시에 실행하고 출력·결과 파일을 합친다
lo=$(mktemp) && le=$(mktemp) || exit 1
largs=("${args[@]}")
largs[wi]=$L
"$real" "${largs[@]}" > "$lo" 2> "$le" &
lp=$!
remote "$R" "$R"
rrc=$?
wait $lp
lrc=$?
cat "$lo"
cat "$le" >&2
declare -A seen
for p in "$L"_* "$R"_*; do
	[ -f "$p" ] || continue
	case "$p" in "$L"_*) suf=${p#"$L"} ;; *) suf=${p#"$R"} ;; esac
	[ -n "${seen[$suf]:-}" ] && continue
	seen[$suf]=1
	cat "$L$suf" "$R$suf" 2>/dev/null > "$wfile$suf"
done
rm -f "$L" "$R" "$L"_* "$R"_* "$lo" "$le"
[ $lrc -ne 0 ] && exit $lrc
exit $rrc
`
}

// localGosshPath: os8 쪽 실제 gossh 경로 (bin/gossh 래퍼 자신은 제외). 없으면 "".
func localGosshPath() string {
	p, err := exec.LookPath("gossh")
	if err != nil {
		return ""
	}
	if a, err := filepath.Abs(p); err == nil && a == filepath.Join(binDir(), "gossh") {
		return ""
	}
	return p
}

func writeOS6Wrapper() error {
	if err := os.MkdirAll(binDir(), 0755); err != nil {
		return err
	}
	p := filepath.Join(binDir(), "gossh")
	if err := os.WriteFile(p, []byte(os6WrapperScript(localGosshPath())), 0755); err != nil {
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
	// gossh 가 접속하지 못한 호스트는 결과에 "host: ERROR: SSH 접속 실패 …" 줄만 남는다 — 체크 결과가 아니므로 처리로 치지 않는다
	// (config_check.sh 의 완료기록 규칙과 같게 ERROR 줄이 있는 호스트는 제외). 전에는 이 줄만으로 완료 처리될 수 있었다.
	bad := map[string]bool{}
	for _, l := range splitLines(post) {
		if h := lineHost(l); h != "" {
			if b := strings.TrimSpace(l[strings.Index(l, ":")+1:]); strings.HasPrefix(b, "ERROR") || strings.HasPrefix(b, "SSH 접속 실패") {
				bad[h] = true
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, l := range splitLines(post) {
		h := lineHost(l)
		if h != "" && want[h] && !seen[h] && !bad[h] {
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
	return settingCheckTitled("설정체크 (설정 수정 후 재점검)", post, havePost, hosts)
}

func settingCheckTitled(title, post string, havePost bool, hosts []string) string {
	var sb strings.Builder
	sb.WriteString(title)
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

// modeLabel: 실행 모드 표시 문구
func modeLabel(mode string) string {
	if mode == ModeCheck {
		return modeCheckLabel
	}
	return modeApplyLabel
}

// buildCodeText: 설정체크 + 설정수정 run 의 code 본문 (자동 흐름·수동 c)
func buildCodeText(logText, post string, havePost bool, hosts []string, logFile string) (string, bool) {
	return buildCodeTextMode("", logText, post, havePost, hosts, logFile)
}

// buildCodeTextMode: codes/<code>.txt 본문과 비정상 여부(결과 리포트 블록이 없거나 불완전). 첫 줄에 실행 모드를 밝힌다.
// mode=ModeCheck(설정체크만)이면 post 는 점검 결과 파일(check.res_<user>)이고 설정 수정·재점검 섹션 대신 "설정체크 (설정 수정 안 함)" 을 쓴다.
func buildCodeTextMode(mode, logText, post string, havePost bool, hosts []string, logFile string) (string, bool) {
	var parts []string
	parts = append(parts, "작업 : "+modeLabel(mode))

	report, ok := extractReport(splitLines(logText))
	if report != "" {
		parts = append(parts, report)
	}

	if mode == ModeCheck {
		parts = append(parts, settingCheckTitled("설정체크 (설정 수정 안 함)", post, havePost, hosts))
	} else {
		parts = append(parts, settingCheckText(post, havePost, hosts))
	}

	parts = append(parts, extractSummaries(splitLines(logText))...)
	if !ok {
		parts = append(parts, noReportDiag(logText))
	}
	parts = append(parts, "원본 : "+logFile)
	return strings.Join(parts, "\n\n") + "\n", !ok
}

// noReportDiag: 결과 리포트가 없을 때(대상이 모두 접속불가이거나 체크스크립트가 중간에 멈춤) 결과 창에 보여 줄 진단.
// 로그 파일 경로를 열어 보지 않아도 원인을 알 수 있게 체크스크립트 출력의 마지막 줄을 함께 싣는다.
func noReportDiag(logText string) string {
	var tail []string
	for _, l := range splitLines(logText) {
		l = strings.TrimRight(ansiRe.ReplaceAllString(l, ""), " \t\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		if r := []rune(l); len(r) > 160 {
			l = string(r[:160]) + "…"
		}
		tail = append(tail, l)
	}
	if len(tail) > 12 {
		tail = tail[len(tail)-12:]
	}
	var sb strings.Builder
	sb.WriteString("[!] 결과 리포트가 없습니다 — 대상 서버가 모두 접속불가이거나 체크스크립트가 끝까지 실행되지 않았습니다.\n")
	sb.WriteString("    user 선택 화면에서 멈췄다면 체크스크립트가 -auto / -auto-check 인자를 인식하지 못한 것입니다 (os_check_sh 버전 확인).")
	if len(tail) == 0 {
		sb.WriteString("\n    체크스크립트 출력이 없습니다.")
		return sb.String()
	}
	sb.WriteString("\n체크스크립트 출력 (마지막 " + fmt.Sprint(len(tail)) + "줄):")
	for _, l := range tail {
		sb.WriteString("\n  | " + l)
	}
	return sb.String()
}
