// second.go - 2차(이중) 체크: os6_mgmt 에서 os_check -auto 를 한 번 더 실행하고 결과를 code 파일에 병합
package main

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() { newSecond = func() Second { return NewRealSecond() } }

const (
	secondRCMarker  = "==RC=="
	secondDirMarker = "==DIR=="
	secondMaxFile   = 64 << 20 // 회수 파일 1개 상한
)

var secondDirRe = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

type realSecond struct{}

func NewRealSecond() Second { return realSecond{} }

// secondSSH: os6_mgmt 로의 ssh 호출 (1차 probe/래퍼와 같은 BatchMode, 접속 제한시간 추가).
// 원격 명령은 remoteBash 로 감싸 bash 가 해석한다 (os6_mgmt 로그인 셸이 csh 여도 동작).
func secondSSH(stdin, remote string) (string, error) {
	return runExternal(stdin, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=30", os6_mgmt, remoteBash(remote))
}

// secondRunScript: 원격 임시 디렉터리 생성 → 목록 수신(stdin) → os_check -auto 실행 (sh 호환 문법).
// env(이름=값, [A-Za-z0-9_.=] 만)는 함께 export 한다 — gossh 멈춤 감시의 표지 (secondHangWatch)
func secondRunScript(user string, env ...string) string {
	oc := os6OSCheckPath()
	ex := ""
	for _, kv := range env {
		ex += " " + kv
	}
	// TMPDIR=$d: 체크스크립트(config_check)의 작업 디렉터리도 이 안에 — 회수 뒤 rm -rf 로 함께 지워진다
	return remoteTmpSh("d", true, true) + "export TMPDIR=\"$d\"" + ex + "; echo " + secondDirMarker + "$d; cat > \"$d/targets.txt\" && cd \"$d\" && " +
		dhcpLinkSh(dhcpCandidates(oc)) + " && " +
		"bash " + shq(oc) + " -auto " + shq(user) + " targets.txt > os_check.log 2>&1; echo " + secondRCMarker + "$?"
}

// secondFetchScript: 결과 파일 tar 회수 후 원격 임시 디렉터리 삭제 (회수 실패여도 삭제)
func secondFetchScript(dir, user string) string {
	return "cd " + shq(dir) + " && tar cf - os_check.log $(ls check.res_" + shq(user) + "* 2>/dev/null); rc=$?; cd /; rm -rf " + shq(dir) + "; exit $rc"
}

func (realSecond) Run(j *Job, first RunResult, targets []string) (SecondResult, error) {
	if os6_mgmt == "" || os6OSCheckPath() == "" || len(targets) == 0 {
		return SecondResult{}, nil
	}
	if !validCode(first.Code) {
		return SecondResult{}, errors.New("2차 체크: 1차 code 가 없습니다")
	}
	dir := filepath.Join(runsDir(), first.Code, "second")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return SecondResult{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "targets.txt"), []byte(hostList(targets)), 0644); err != nil {
		return SecondResult{}, err
	}
	logf("2차 체크 실행: code=%s user=%s %d대", first.Code, j.User, len(targets))

	// os6_mgmt 의 체크스크립트가 띄우는 gossh 는 표지로 찾는다 (hang_min 을 넘기면 걸린 호스트 취소)
	kv := fmt.Sprintf("AUTO_SETUP_2ND=c%s.%d.%d", first.Code, os.Getpid(), time.Now().UnixNano())
	stopWatch := func() {}
	if lim := hangLimit(); lim > 0 {
		stopWatch = secondHangWatch(first.Code, kv, lim, hangTermAfter(lim), secondHangPoll)
	}
	out, err := secondSSH(hostList(targets), secondRunScript(j.User, kv))
	stopWatch()
	rdir := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, secondDirMarker) {
			rdir = strings.TrimSpace(strings.TrimPrefix(l, secondDirMarker))
		}
	}
	if rdir == "" || !secondDirRe.MatchString(rdir) {
		if err == nil {
			err = errors.New("원격 작업 디렉터리 생성 실패")
		}
		return SecondResult{}, fmt.Errorf("2차 체크 실행 실패: %v", err)
	}
	rc := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, secondRCMarker) {
			rc = strings.TrimSpace(strings.TrimPrefix(l, secondRCMarker))
		}
	}
	if rc != "0" {
		logf("[!] 2차 os_check 종료코드 %q: code=%s", rc, first.Code)
	}

	tarOut, ferr := secondSSH("", secondFetchScript(rdir, j.User))
	if err := extractSecondTar(tarOut, dir); err != nil {
		if ferr != nil {
			err = fmt.Errorf("%v (%v)", err, ferr)
		}
		return SecondResult{}, fmt.Errorf("2차 체크 결과 회수 실패: %v", err)
	}

	logPath := filepath.Join(dir, "os_check.log")
	logText, _ := os.ReadFile(logPath)
	post, postErr := os.ReadFile(filepath.Join(dir, "check.res_"+j.User+"_postapply"))
	_, ok := extractReport(splitLines(string(logText)))
	res := SecondResult{}
	if !ok {
		logf("[!] 2차 os_check 비정상 종료(결과 리포트 없음): code=%s", first.Code)
	} else if postErr == nil {
		res.Processed = parsePostapplyHosts(string(post), targets)
		res.Fail = parsePostapplyFailed(string(post), targets)
	}
	if err := appendSecondSection(first.Code, res, first, targets, string(logText), string(post), postErr == nil, logPath); err != nil {
		logf("[X] 2차 결과 code 병합 실패: code=%s: %v", first.Code, err)
	}
	return res, nil
}

// secondHangPoll: 2차 체크 gossh 멈춤 확인 주기 (ssh 1회씩이라 1차보다 길게, 테스트는 줄인다)
var secondHangPoll = 30 * time.Second

// secondHangWatch: 2차 체크(os6_mgmt 의 체크스크립트 1회) 동안 표지 kv 를 가진 os6_mgmt 의 gossh 를 감시한다.
// 시작 후 limit 이 지나야 걸릴 수 있으므로 그때부터 poll 마다 ssh 로 목록(pid, 경과 시간 etime)을 받고,
// limit 을 넘긴 gossh 에 INT×2(한 번만), 그 뒤 term 을 넘겨 남아 있으면 TERM 을 보낸다 (1차 hangWatch 와 같은 규칙).
// 돌려준 함수가 감시를 끝내고 고루틴 종료까지 기다린다 (진행 중인 ssh 는 checkTimeout 안에 끝난다).
func secondHangWatch(code, kv string, limit, term, poll time.Duration) func() {
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-stop:
			return
		case <-time.After(limit):
		}
		intAt := map[int]time.Time{}
		termed := map[int]bool{}
		list := gosshPidsSh(kv, "gossh") + `for p in $pl; do echo "==G== $p $(ps -o etime= -p "$p" 2>/dev/null)"; done`
		for {
			out, _ := secondSSHWatch(list)
			now := time.Now()
			var toInt, toTerm []int
			for _, l := range strings.Split(out, "\n") {
				f := strings.Fields(l)
				if len(f) != 3 || f[0] != "==G==" {
					continue
				}
				pid, err := strconv.Atoi(f[1])
				age := parseEtime(f[2])
				if err != nil || age < 0 {
					continue
				}
				switch at, ok := intAt[pid]; {
				case !ok && time.Duration(age)*time.Second >= limit:
					intAt[pid] = now
					toInt = append(toInt, pid)
					logf("[!] 2차 체크 gossh hang 감지: code=%s os6_mgmt pid=%d %d분 경과 → 걸린 호스트 취소(INT×2)", code, pid, age/60)
				case ok && !termed[pid] && now.Sub(at) >= term:
					termed[pid] = true
					toTerm = append(toTerm, pid)
					logf("[!] 2차 체크 gossh 가 취소 후에도 %d분 남음: code=%s os6_mgmt pid=%d → 종료(TERM)", int(now.Sub(at)/time.Minute), code, pid)
				}
			}
			if len(toInt) > 0 {
				secondSSHWatch(gosshCheckPidsSh(kv, toInt) + gosshSigSh(false))
			}
			if len(toTerm) > 0 {
				secondSSHWatch(gosshCheckPidsSh(kv, toTerm) + gosshSigSh(true))
			}
			select {
			case <-stop:
				return
			case <-time.After(poll):
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

// secondSSHWatch: 감시용 ssh (시간 제한 있음 — 감시가 2차 체크 종료를 붙잡지 않게)
func secondSSHWatch(remote string) (string, error) {
	return runExternalDiag("", "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", os6_mgmt, remoteBash(remote))
}

// extractSecondTar: 회수한 tar 에서 os_check.log / check.res_* 일반 파일만 dir 에 푼다 (경로 성분 무시)
func extractSecondTar(data, dir string) error {
	tr := tar.NewReader(strings.NewReader(data))
	n := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := path.Base(h.Name)
		if h.Typeflag != tar.TypeReg || (name != "os_check.log" && !strings.HasPrefix(name, "check.res_")) {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, secondMaxFile))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0644); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return errors.New("회수된 파일 없음")
	}
	return nil
}

// buildSecondSection: code 파일에 이어 붙일 "### 2차 체크 (os6_mgmt)" 섹션 (순수 함수)
func buildSecondSection(res SecondResult, first RunResult, targets []string, logText, post string, havePost bool, logPath string) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("### 2차 체크 (os6_mgmt)\n대상 : %s (%d대)", strings.Join(targets, " "), len(targets)))

	report, ok := extractReport(splitLines(logText))
	if report != "" {
		parts = append(parts, report)
	}
	if !ok {
		parts = append(parts, "[!] 2차 os_check 비정상 종료 - 2차 호스트 미처리")
	}
	parts = append(parts, settingCheckText(post, havePost, targets))

	secondFail := map[string]bool{}
	for _, h := range res.Fail {
		secondFail[h] = true
	}
	okSet := map[string]bool{}
	firstFail := map[string]bool{}
	for _, h := range first.Failed {
		firstFail[h] = true
	}
	for _, h := range first.Processed { // 1차 OK
		if !firstFail[h] {
			okSet[h] = true
		}
	}
	for _, h := range res.Processed { // 2차 OK
		if !secondFail[h] {
			okSet[h] = true
		}
	}
	failSet := map[string]bool{}
	for _, h := range first.Failed {
		failSet[h] = true
	}
	for _, h := range targets {
		failSet[h] = true
	}
	var fails, oks []string
	for h := range failSet {
		if !okSet[h] {
			fails = append(fails, h)
		}
	}
	for h := range okSet {
		oks = append(oks, h)
	}
	sort.Strings(fails)
	sort.Strings(oks)
	final := "최종 판정 (1차 OK 또는 2차 OK)"
	if len(fails) > 0 {
		final += fmt.Sprintf("\nFAIL : %s (%d대)", strings.Join(fails, " "), len(fails))
	}
	if len(oks) > 0 {
		final += fmt.Sprintf("\nNO FAIL : %s (%d대)", strings.Join(oks, " "), len(oks))
	}
	parts = append(parts, final)
	parts = append(parts, "원본 : "+logPath)
	return strings.Join(parts, "\n\n") + "\n"
}

// appendSecondSection: codes/<code>.txt 끝에 2차 섹션을 이어 붙인다.
func appendSecondSection(code string, res SecondResult, first RunResult, targets []string, logText, post string, havePost bool, logPath string) error {
	cur, err := readCode(code)
	if err != nil {
		return err
	}
	text := string(cur)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += "\n" + buildSecondSection(res, first, targets, logText, post, havePost, logPath)
	return os.WriteFile(codePath(code), []byte(text), 0644)
}
