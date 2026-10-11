// changerun.go - m 메뉴 실행: 엔진(ip-change-engine / ldap-config-engine)·gossh 외부 프로세스 실행, os8 → os6_mgmt 경유 폴백,
// 엔진 출력 파싱(호스트별 결과), 취소(프로세스 그룹 SIGTERM → 3초 뒤 SIGKILL), 임시 디렉터리(0700) 정리, 기록·로그 호출.
//
// 엔진 출력 파싱 규칙 (엔진 소스 기준, 색 코드는 제거 후 파싱, 엔진에는 NO_COLOR=1 도 준다):
//
//	ip-change-engine (표준출력, 결과는 "------" 구분선 뒤, 호스트명은 줄 맨 앞 %-20s):
//	  "<host> <기존IP> -> <새IP>   (GW <gw>)"            → OK
//	  "<host> 복원 <ifcfg> -> IPADDR <ip>   (STAMP <s>)"   → OK (-rollback)
//	  "<host> FAIL: <사유>" (+ 4칸 들여쓴 상세 줄)          → FAIL
//	  "<host> 알 수 없는 응답: <줄>"                         → FAIL
//	  "<host> UNREACHABLE (gossh 응답 없음)"                → 응답 없음 (os6 재시도 대상)
//	  종료코드 0 전부 OK / 2 일부 실패·gossh 출력 없음 / 1 인자·설정 오류(표준에러 "오류: …")
//	ldap-config-engine (표준출력 "  %-24s <상태>", FAIL/NORESULT 뒤 6칸 들여쓴 원격 출력 — DIFF| 줄은 버림):
//	  상태 OK|NOCHANGE|FAIL|NORESULT|NOBACKUP, "UNREACHABLE (gossh 응답 없음)" → 응답 없음
//	  표준에러 "경고: 다음 호스트는 자산현황(…)에 없고 default_site 도 없어 건너뜁니다: a, b" → 건너뜀(자산현황 없음)
//	  -rollback 의 "오류: gossh 출력이 비어 있습니다"(종료 1) → 전부 응답 없음, 그 밖의 종료 1 → 전부 FAIL(엔진 오류)
//	  -dry-run 의 OK 는 "CHANGED"(바뀔 예정)로 바꿔 보여 준다 (엔진은 OK 호스트의 바뀔 파일 목록을 출력하지 않음)
//	출력에 결과 줄이 없는 대상 호스트 = 응답 없음 → os6_mgmt 경유 재시도 (os6_mgmt/os6_gossh 가 비면 재시도 없음)
package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 호스트 결과 상태 (기록 파일 값)
const (
	rsOK        = "OK"
	rsChanged   = "CHANGED" // LDAP dry-run: 바뀔 예정
	rsNoChange  = "NOCHANGE"
	rsFail      = "FAIL"
	rsUnreach   = "UNREACHABLE"
	rsNoResult  = "NORESULT"
	rsNoBackup  = "NOBACKUP"
	rsSkipIP    = "SKIP_IP_FAIL"  // IP+LDAP: IP 실패로 LDAP 건너뜀
	rsSkipAsset = "SKIP_NO_ASSET" // 자산현황에 없어 엔진이 건너뜀
	rsCancel    = "CANCELLED"     // 취소로 결과 미확인
	rsNoPending = "NOPENDING"     // auto.appl 원복: 대기 파일 없음
)

// ldapNoRestart: LDAP 엔진 호출에는 항상 붙는다 (원복이 서비스를 재시작하지 않도록 — 테스트로 고정)
const ldapNoRestart = "-no-restart"

// applScriptPath: auto.appl 원복은 노드의 이 스크립트만 실행한다 (ldap_setting apply_body.sh 7번 블록이 만들고, 재시작하지 않음)
const applScriptPath = "/root/auto_appl_restore.sh"

// ---- 외부 명령 실행기 (테스트에서 교체) ----

type changeCmd struct {
	Name string
	Args []string
	Env  []string // os.Environ() 뒤에 덧붙임
}

type changeOut struct {
	Stdout, Stderr string
	RC             int
	StartErr       error // 시작 자체 실패 (실행 파일 없음 등)
	Killed         bool  // 취소로 종료시킴
}

// changeExecFn: c 를 실행. cancel 이 닫히면 프로세스 그룹을 끝낸다. onLine 은 출력 줄마다(표준출력·표준에러, 동시 호출 없음).
type changeExecFn func(c changeCmd, cancel <-chan struct{}, onLine func(string)) changeOut

var changeExec changeExecFn = realChangeExec

func realChangeExec(c changeCmd, cancel <-chan struct{}, onLine func(string)) changeOut {
	cmd := exec.Command(c.Name, c.Args...)
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.SysProcAttr = oneoffSysProcAttr() // 별도 프로세스 그룹 (취소 시 그룹 전체 종료, TUI 의 tty 신호와 분리)
	op, err := cmd.StdoutPipe()
	if err != nil {
		return changeOut{RC: -1, StartErr: err}
	}
	ep, err := cmd.StderrPipe()
	if err != nil {
		return changeOut{RC: -1, StartErr: err}
	}
	if err := cmd.Start(); err != nil {
		return changeOut{RC: -1, StartErr: err}
	}
	var so, se bytes.Buffer
	var mu sync.Mutex
	var wg sync.WaitGroup
	scan := func(r io.Reader, b *bytes.Buffer) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			mu.Lock()
			b.WriteString(sc.Text() + "\n")
			if onLine != nil {
				onLine(sc.Text())
			}
			mu.Unlock()
		}
		_, _ = io.Copy(io.Discard, r) // 너무 긴 줄 뒤 나머지는 버리고 파이프만 비운다
	}
	wg.Add(2)
	go scan(op, &so)
	go scan(ep, &se)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		_ = cmd.Wait()
		close(done)
	}()
	killed := false
	select {
	case <-done:
	case <-cancel:
		killed = true
		pid := cmd.Process.Pid
		killGroup(pid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(oneoffKillGap):
			killGroup(pid, syscall.SIGKILL)
			<-done
		}
	}
	rc := -1
	if cmd.ProcessState != nil {
		rc = cmd.ProcessState.ExitCode()
	}
	return changeOut{Stdout: so.String(), Stderr: se.String(), RC: rc, Killed: killed}
}

// ---- 작업(비동기) ----

type changePlan struct {
	kind         changeKind
	dry          bool // LDAP dry-run (미리보기) — 기록 없음
	rbIP, rbLDAP bool
	infra        string
	user         string
	entries      []ipEntry
	hosts        []string
	forceOS6     map[string]bool // 처음부터 os6 경유 (os6 설정이 없으면 무시)
}

type changeJob struct {
	mu        sync.Mutex
	phase     string
	tail      []string // 마스킹된 출력 꼬리 (화면용)
	res       []*chgHost
	cancelled bool
	cancelCh  chan struct{}
	once      sync.Once
	done      chan struct{}
	recPath   string // done 이후 읽기
	recErr    string
}

const changeTailMax = 200

func (j *changeJob) cancel() {
	j.once.Do(func() {
		j.mu.Lock()
		j.cancelled = true
		j.mu.Unlock()
		close(j.cancelCh)
	})
}

func (j *changeJob) finished() bool {
	select {
	case <-j.done:
		return true
	default:
		return false
	}
}

func (j *changeJob) wasCancelled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cancelled
}

func (j *changeJob) results() []*chgHost {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.res
}

// progress: 화면용 (현재 단계 문구, 출력 꼬리 사본)
func (j *changeJob) progress() (string, []string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.phase, append([]string(nil), j.tail...)
}

func (j *changeJob) setPhase(p string) {
	j.mu.Lock()
	j.phase = p
	j.mu.Unlock()
}

func (j *changeJob) addLine(l string) {
	j.mu.Lock()
	j.tail = append(j.tail, l)
	if len(j.tail) > changeTailMax {
		j.tail = j.tail[len(j.tail)-changeTailMax:]
	}
	j.mu.Unlock()
}

// startChangeJob: 백그라운드 실행. 실제 변경이면 시작 기록을 먼저 쓰고(실패하면 아무것도 실행하지 않음), 끝나면 기록·로그·이전 기록 갱신.
func startChangeJob(env *changeEnv, mask *secretMask, p changePlan) *changeJob {
	j := &changeJob{cancelCh: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(j.done)
		r := &chgRunner{env: env, mask: mask, job: j, plan: p, res: map[string]*chgHost{}}
		r.initHosts()
		var rec *changeRecord
		if !p.dry {
			rec = &changeRecord{User: p.user, Kind: recKind(p), Infra: p.infra, Start: env.Now().Format(time.RFC3339), Hosts: r.list()}
			path, err := createChangeRecord(env.StateDir, rec, env.Now())
			if err != nil {
				j.recErr = err.Error()
				r.failAll("기록 파일을 쓸 수 없어 실행하지 않음")
				j.mu.Lock()
				j.res = r.list()
				j.mu.Unlock()
				return
			}
			j.recPath = path
		}
		dir, err := os.MkdirTemp("", "as_change_")
		if err == nil {
			err = os.Chmod(dir, 0700)
		}
		if err != nil {
			r.failAll("임시 디렉터리 생성 실패")
		} else {
			r.dir = dir
			r.run()
			os.RemoveAll(dir)
		}
		res := r.list()
		j.mu.Lock()
		j.res = res
		j.mu.Unlock()
		if rec == nil {
			return
		}
		rec.End, rec.Cancelled, rec.Hosts = env.Now().Format(time.RFC3339), j.wasCancelled(), res
		if err := saveChangeRecord(j.recPath, rec); err != nil && j.recErr == "" {
			j.recErr = err.Error()
		}
		env.Log("%s", changeLogSummary(rec))
		if err := markAfterRun(env.StateDir, p, res); err != nil && j.recErr == "" {
			j.recErr = "이전 기록 갱신 실패: " + err.Error()
		}
	}()
	return j
}

// recKind: 기록 파일 이름·kind 값
func recKind(p changePlan) string {
	switch p.kind {
	case kindIP:
		return "ip"
	case kindLDAP:
		return "ldap"
	case kindBoth:
		return "both"
	case kindApplRestore:
		return "appl"
	case kindRollback:
		switch {
		case p.rbIP && p.rbLDAP:
			return "rollback-both"
		case p.rbIP:
			return "rollback-ip"
		}
		return "rollback-ldap"
	}
	return "change"
}

// ---- 실행기 ----

type chgRunner struct {
	env     *changeEnv
	mask    *secretMask
	job     *changeJob
	plan    changePlan
	dir     string
	wrapper string
	res     map[string]*chgHost
}

// engHost: 엔진 호출 1회의 호스트 결과
type engHost struct{ st, detail, old, new, gw string }

func (r *chgRunner) cancelled() bool {
	select {
	case <-r.job.cancelCh:
		return true
	default:
		return false
	}
}

func (r *chgRunner) initHosts() {
	ent := map[string]ipEntry{}
	for _, e := range r.plan.entries {
		ent[e.Host] = e
	}
	var sites map[string]string
	if r.plan.kind == kindLDAP || r.plan.kind == kindBoth {
		sites = assetSites(filepath.Join(r.env.ConfDir, "assets.txt"))
	}
	for _, h := range r.plan.hosts {
		x := &chgHost{Host: h}
		if e, ok := ent[h]; ok && !r.plan.dry {
			x.NewIP, x.GW = e.IP, e.GW
		}
		if sites != nil {
			x.Site = siteOf(sites, h)
		}
		r.res[h] = x
	}
}

func (r *chgRunner) list() []*chgHost {
	out := make([]*chgHost, 0, len(r.plan.hosts))
	for _, h := range r.plan.hosts {
		c := *r.res[h]
		out = append(out, &c)
	}
	return out
}

func (r *chgRunner) failAll(why string) {
	for _, h := range r.plan.hosts {
		x := r.res[h]
		switch {
		case r.plan.dry || r.plan.kind == kindLDAP:
			x.LDAPResult = rsFail
		case r.plan.kind == kindApplRestore:
			x.ApplResult = rsFail
		case r.plan.kind == kindRollback && !r.plan.rbIP:
			x.LDAPResult = rsFail
		default:
			x.IPResult = rsFail
		}
		x.Detail = why
	}
}

func (r *chgRunner) run() {
	p, hosts := r.plan, r.plan.hosts
	switch {
	case p.dry:
		r.phaseLDAP(hosts, "dry", p.forceOS6)
	case p.kind == kindIP:
		r.phaseIP(hosts, false)
	case p.kind == kindLDAP:
		r.phaseLDAP(hosts, "apply", p.forceOS6)
	case p.kind == kindBoth:
		r.phaseIP(hosts, false)
		if r.cancelled() {
			return
		}
		var ok []string
		force := map[string]bool{}
		for _, h := range hosts {
			x := r.res[h]
			if x.IPResult == rsOK {
				ok = append(ok, h)
				if x.Route == "os6" {
					force[h] = true
				}
				continue
			}
			x.LDAPResult = rsSkipIP
		}
		if len(ok) > 0 { // IP 가 전부 실패하면 LDAP 은 실행하지 않는다
			r.phaseLDAP(ok, "apply", force)
		}
	case p.kind == kindRollback:
		force := map[string]bool{}
		for h := range p.forceOS6 {
			force[h] = true
		}
		if p.rbIP {
			r.phaseIP(hosts, true)
			for _, h := range hosts {
				if x := r.res[h]; x.Route == "os6" && x.IPResult != rsUnreach {
					force[h] = true
				}
			}
		}
		if p.rbLDAP && !r.cancelled() {
			r.phaseLDAP(hosts, "rollback", force)
		}
	case p.kind == kindApplRestore:
		r.phaseAppl(hosts)
	}
}

func (r *chgRunner) phaseIP(hosts []string, rollback bool) {
	label := "IP 변경"
	if rollback {
		label = "IP 원복"
	}
	ent := map[string]ipEntry{}
	for _, e := range r.plan.entries {
		ent[e.Host] = e
	}
	call := func(hs []string, g string) map[string]engHost {
		var b strings.Builder
		for _, h := range hs {
			if rollback {
				b.WriteString(h + "\n")
			} else {
				b.WriteString(h + " " + ent[h].IP + "\n")
			}
		}
		f, err := r.writeTemp("targets", b.String())
		if err != nil {
			return failMap(hs, "임시 파일 쓰기 실패")
		}
		return parseIPOut(r.exec(r.env.IPEngine, ipEngineArgs(r.env.ConfDir, f, g, rollback)), hs, rollback)
	}
	out, via := r.routed(label, hosts, r.plan.forceOS6, call)
	for _, h := range hosts {
		x, e := r.res[h], out[h]
		x.IPResult, x.Route = e.st, via[h]
		if e.old != "" {
			x.OldIP = e.old
		}
		if e.new != "" {
			x.NewIP = e.new
		}
		if e.gw != "" {
			x.GW = e.gw
		}
		x.addDetail("IP", e.detail)
	}
}

// phaseLDAP: mode = dry | apply | rollback
func (r *chgRunner) phaseLDAP(hosts []string, mode string, force map[string]bool) {
	label := map[string]string{"dry": "LDAP 미리보기(dry-run)", "apply": "LDAP 변경", "rollback": "LDAP 원복"}[mode]
	call := func(hs []string, g string) map[string]engHost {
		f, err := r.writeTemp("hosts", hostList(hs))
		if err != nil {
			return failMap(hs, "임시 파일 쓰기 실패")
		}
		return parseLDAPOut(r.exec(r.env.LDAPEngine, ldapEngineArgs(mode, r.env.ConfDir, r.plan.infra, f, g)), hs)
	}
	out, via := r.routed(label, hosts, force, call)
	for _, h := range hosts {
		x, e := r.res[h], out[h]
		st := e.st
		if mode == "dry" && st == rsOK {
			st = rsChanged
		}
		x.LDAPResult, x.Route = st, via[h]
		if mode == "apply" && st == rsOK {
			x.ApplPending = true // auto.appl 은 /etc/auto.appl.bak 에 대기 → auto.appl 원복으로 반영
		}
		x.addDetail("LDAP", e.detail)
	}
}

func (r *chgRunner) phaseAppl(hosts []string) {
	call := func(hs []string, g string) map[string]engHost {
		f, err := r.writeTemp("hosts", hostList(hs))
		if err != nil {
			return failMap(hs, "임시 파일 쓰기 실패")
		}
		return parseApplOut(r.exec(g, []string{"-pm", "-script", "-w", f, buildRemoteCmd(applRemoteScript)}), hs)
	}
	out, via := r.routed("auto.appl 원복", hosts, r.plan.forceOS6, call)
	for _, h := range hosts {
		x, e := r.res[h], out[h]
		x.ApplResult, x.Route = e.st, via[h]
		x.addDetail("auto.appl", e.detail)
	}
}

// routed: os8 에서 먼저 실행 → 응답 없는 호스트만 os6_mgmt 경유로 재시도. force 호스트는 처음부터 os6 경유.
// os6_mgmt/os6_gossh 가 비어 있으면 os6 경유 없이 os8 결과 그대로 (응답 없음). 반환: 결과, 경로(os8|os6).
func (r *chgRunner) routed(label string, hosts []string, force map[string]bool, call func([]string, string) map[string]engHost) (map[string]engHost, map[string]string) {
	res, via := map[string]engHost{}, map[string]string{}
	os6ok := r.env.OS6Mgmt != "" && r.env.OS6Gossh != ""
	var first, second []string
	retried := map[string]bool{}
	for _, h := range hosts {
		if os6ok && force[h] {
			second = append(second, h)
		} else {
			first = append(first, h)
		}
	}
	settle := func(hs []string, out map[string]engHost, route, miss string) {
		killed := r.cancelled()
		for _, h := range hs {
			e, ok := out[h]
			switch {
			case killed && (!ok || e.st == rsUnreach):
				e = engHost{st: rsCancel, detail: "취소로 결과 미확인"}
			case !ok || e.st == "":
				e = engHost{st: rsUnreach, detail: miss}
			case e.st == rsUnreach && e.detail == "":
				e.detail = miss
			}
			res[h], via[h] = e, route
		}
	}
	if len(first) > 0 {
		r.job.setPhase(label + " — os8 에서 실행 중 (" + strconv.Itoa(len(first)) + "대)")
		settle(first, call(first, r.env.Gossh), "os8", "응답 없음")
		for _, h := range first {
			if res[h].st == rsUnreach && os6ok {
				second = append(second, h)
				retried[h] = true
			}
		}
	}
	if len(second) == 0 || r.cancelled() {
		return res, via
	}
	w, err := r.os6Wrapper()
	if err != nil {
		for _, h := range second {
			res[h], via[h] = engHost{st: rsFail, detail: "os6 경유 준비 실패"}, "os6"
		}
		return res, via
	}
	r.job.setPhase(label + " — os6_mgmt 경유 실행 중 (" + strconv.Itoa(len(second)) + "대)")
	out := call(second, w)
	miss := "os6 경유 응답 없음"
	for _, h := range second {
		if retried[h] {
			if e, ok := out[h]; !ok || e.st == rsUnreach {
				out[h] = engHost{st: rsUnreach, detail: "os8·os6 경유 모두 응답 없음"}
			}
		}
	}
	settle(second, out, "os6", miss)
	return res, via
}

// exec: 외부 명령 실행 — 출력은 들어오는 즉시 마스킹(화면 꼬리에는 DIFF 줄 제외), 임시 파일은 작업 디렉터리로 (TMPDIR)
func (r *chgRunner) exec(name string, args []string) changeOut {
	o := r.env.Exec(changeCmd{Name: name, Args: args, Env: []string{"TMPDIR=" + r.dir, "NO_COLOR=1"}}, r.job.cancelCh,
		func(l string) {
			l = r.mask.apply(stripANSI(strings.TrimRight(l, "\r")))
			if !strings.Contains(l, "DIFF|") && strings.TrimSpace(l) != "" {
				r.job.addLine(l)
			}
		})
	o.Stdout, o.Stderr = r.mask.apply(o.Stdout), r.mask.apply(o.Stderr)
	return o
}

func (r *chgRunner) writeTemp(prefix, content string) (string, error) {
	f, err := os.CreateTemp(r.dir, prefix+"_*.txt")
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// os6Wrapper: 작업 디렉터리에 os6 경유 gossh 래퍼(0700)를 한 번 만든다
func (r *chgRunner) os6Wrapper() (string, error) {
	if r.wrapper != "" {
		return r.wrapper, nil
	}
	if r.dir == "" {
		return "", errors.New("작업 디렉터리 없음")
	}
	p := filepath.Join(r.dir, "gossh_os6.sh")
	if err := os.WriteFile(p, []byte(chgOS6WrapperScript(r.env.OS6Mgmt, r.env.OS6Gossh)), 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(p, 0700); err != nil {
		return "", err
	}
	r.wrapper = p
	return p, nil
}

// chgOS6WrapperScript: 엔진의 -gossh 로 넘기는 래퍼. 받은 gossh 인자를 printf %q 로 하나씩 이스케이프해
// "ssh <os6_mgmt> <os6_gossh> 인자…" 로 전달한다. -w <호스트 파일> 은 로컬 파일이므로 표준입력으로 보내
// os6_mgmt 의 임시 파일(mktemp)로 바꿔 넣고 끝나면 지운다 (기존 auto_setup 의 os6 gossh 호출과 같은 방식).
// 비밀번호·bindpw 는 들어가지 않는다 (gossh 인증은 기존과 같이 키 기반, -p 없음).
// runner.go 의 os6WrapperScript(데몬용, bin/gossh 고정 경로) 와 같은 방식이지만, TUI 에서 도는 별도 프로세스 그룹이라
// ssh 가 /dev/tty 로 비밀번호를 물으면 멈추므로 BatchMode=yes 를 주고, os6_gossh 경로도 원격 셸용으로 따옴표 처리한다.
func chgOS6WrapperScript(mgmt, gossh string) string {
	return "#!/bin/bash\n" +
		"# auto_setup 변경 메뉴: os6_mgmt 경유 gossh 래퍼 (실행 중에만 있는 임시 파일)\n" +
		"M=" + shq(mgmt) + "\n" +
		"G=" + shq(shq(gossh)) + "\n" +
		`w=""; a=""; nx=0
for x in "$@"; do
  if [ "$nx" = 1 ]; then w=$x; a="$a \"\$f\""; nx=0; continue; fi
  [ "$x" = "-w" ] && nx=1
  a="$a $(printf '%q' "$x")"
done
if [ -z "$w" ] || [ ! -r "$w" ]; then echo "as_change: -w 호스트 목록 파일 없음" >&2; exit 97; fi
exec ssh -o BatchMode=yes "$M" "f=\$(mktemp) || exit 97; cat > \"\$f\"; $G$a; rc=\$?; rm -f \"\$f\" \"\$f\"_*; exit \$rc" < "$w"
`
}

// ---- 엔진 인자 (순수) ----

func ipEngineArgs(confDir, targets, gossh string, rollback bool) []string {
	a := []string{"-config", filepath.Join(confDir, "ip_change.conf"), "-targets", targets, "-gossh", gossh}
	if rollback {
		a = append(a, "-rollback") // 가장 최근 백업 (엔진 기본)
	}
	return a
}

// ldapEngineArgs: mode = dry | apply | rollback. -no-restart 는 모든 호출에 붙는다 (원복은 엔진 기본이 재시작이므로 필수).
// -print-script·-p·-monitor 는 쓰지 않는다. 원복은 -infra 가 필요 없다.
func ldapEngineArgs(mode, confDir, infra, hostFile, gossh string) []string {
	a := []string{"-config", filepath.Join(confDir, "ldap_config.conf"), "-assets", filepath.Join(confDir, "assets.txt"),
		"-host-file", hostFile, "-gossh", gossh, ldapNoRestart}
	switch mode {
	case "dry":
		a = append(a, "-infra", infra, "-dry-run")
	case "apply":
		a = append(a, "-infra", infra)
	case "rollback":
		a = append(a, "-rollback")
	}
	return a
}

// applRemoteScript: auto.appl 원복 — 스크립트만 실행하고 결과를 기계용 줄로 (서비스 재시작 없음). buildRemoteCmd 로 감싸 보낸다
// (따옴표 없는 명령줄이라 대상 로그인 셸이 csh 여도 같음).
const applRemoteScript = `if [ ! -f ` + applScriptPath + ` ]; then echo "APPL|NOSCRIPT"; exit 0; fi
out=$(bash ` + applScriptPath + ` 2>&1 </dev/null); rc=$?
printf '%s\n' "$out" | sed 's/^/APPL|MSG|/'
echo "APPL|RC|$rc"
`

// ---- 파싱 (순수) ----

var (
	ipOKRe     = regexp.MustCompile(`^(\S+)\s+(\S*) -> (\S+)\s+\(GW ([^)]*)\)\s*$`)
	ipRbRe     = regexp.MustCompile(`^(\S+)\s+복원 (\S+) -> IPADDR (\S*)\s+\(STAMP ([^)]*)\)\s*$`)
	ipFailRe   = regexp.MustCompile(`^(\S+)\s+FAIL: (.*)$`)
	ipUnkRe    = regexp.MustCompile(`^(\S+)\s+알 수 없는 응답: (.*)$`)
	unreachRe  = regexp.MustCompile(`^\s*(\S+)\s+UNREACHABLE\b`)
	ldapLineRe = regexp.MustCompile(`^  (\S+)\s+(OK|NOCHANGE|FAIL|NORESULT|NOBACKUP)\s*$`)
	ldapSkipRe = regexp.MustCompile(`건너뜁니다: (.+)$`)
)

const detailMax = 3 // 호스트별 상세 줄 최대 개수

func failMap(hosts []string, why string) map[string]engHost {
	m := map[string]engHost{}
	for _, h := range hosts {
		m[h] = engHost{st: rsFail, detail: why}
	}
	return m
}

func hostSet(hosts []string) map[string]bool {
	m := map[string]bool{}
	for _, h := range hosts {
		m[h] = true
	}
	return m
}

func joinDetail(d, l string) string {
	l = strings.TrimSpace(l)
	if l == "" || strings.Count(d, " / ")+1 > detailMax {
		return d
	}
	if d == "" {
		return l
	}
	return d + " / " + l
}

// engineErr: 표준에러 첫 줄 ("오류: " 제거)
func engineErr(o changeOut) string {
	if o.StartErr != nil {
		return "엔진 실행 실패: " + o.StartErr.Error()
	}
	for _, l := range splitLines(o.Stderr) {
		if l = strings.TrimSpace(l); l != "" {
			return "엔진 오류: " + clip(strings.TrimPrefix(l, "오류: "))
		}
	}
	return "엔진 오류 (종료코드 " + strconv.Itoa(o.RC) + ")"
}

func emptyGossh(o changeOut) bool {
	return strings.Contains(o.Stdout+o.Stderr, "gossh 출력이 비어 있습니다")
}

// parseIPOut: ip-change-engine 출력 → 호스트별 결과 (없는 호스트 = 응답 없음으로 routed 가 채움)
func parseIPOut(o changeOut, hosts []string, rollback bool) map[string]engHost {
	if o.StartErr != nil {
		return failMap(hosts, engineErr(o))
	}
	set, out, last := hostSet(hosts), map[string]engHost{}, ""
	for _, l := range splitLines(o.Stdout) {
		if strings.HasPrefix(l, " ") {
			if last != "" && strings.HasPrefix(l, "    ") {
				e := out[last]
				e.detail = joinDetail(e.detail, l)
				out[last] = e
			}
			continue
		}
		last = ""
		if m := unreachRe.FindStringSubmatch(l); m != nil && set[m[1]] {
			out[m[1]] = engHost{st: rsUnreach}
			continue
		}
		if m := ipFailRe.FindStringSubmatch(l); m != nil && set[m[1]] {
			out[m[1]], last = engHost{st: rsFail, detail: strings.TrimSpace(m[2])}, m[1]
			continue
		}
		if m := ipUnkRe.FindStringSubmatch(l); m != nil && set[m[1]] {
			out[m[1]], last = engHost{st: rsFail, detail: "알 수 없는 응답: " + clip(m[2])}, m[1]
			continue
		}
		if rollback {
			if m := ipRbRe.FindStringSubmatch(l); m != nil && set[m[1]] {
				out[m[1]] = engHost{st: rsOK, new: m[3], detail: "복원 " + m[2] + " (STAMP " + m[4] + ")"}
			}
			continue
		}
		if m := ipOKRe.FindStringSubmatch(l); m != nil && set[m[1]] {
			out[m[1]] = engHost{st: rsOK, old: m[2], new: m[3], gw: m[4]}
		}
	}
	if len(out) == 0 && o.RC == 1 && !emptyGossh(o) && !o.Killed {
		return failMap(hosts, engineErr(o))
	}
	return out
}

// parseLDAPOut: ldap-config-engine 출력 → 호스트별 결과 (DIFF 줄·RESULT 줄은 상세에 넣지 않음)
func parseLDAPOut(o changeOut, hosts []string) map[string]engHost {
	if o.StartErr != nil {
		return failMap(hosts, engineErr(o))
	}
	set, out, last := hostSet(hosts), map[string]engHost{}, ""
	for _, l := range splitLines(o.Stdout) {
		if m := ldapLineRe.FindStringSubmatch(l); m != nil && set[m[1]] {
			out[m[1]], last = engHost{st: m[2]}, ""
			if m[2] == rsFail || m[2] == rsNoResult {
				last = m[1]
			}
			continue
		}
		if m := unreachRe.FindStringSubmatch(l); m != nil && set[m[1]] {
			out[m[1]], last = engHost{st: rsUnreach}, ""
			continue
		}
		if !strings.HasPrefix(l, "      ") {
			last = ""
			continue
		}
		if last == "" {
			continue
		}
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "DIFF|") || strings.HasPrefix(t, "RESULT|") {
			continue
		}
		e := out[last]
		if e.st == rsFail && !strings.HasPrefix(t, "APPLY|FAIL|") {
			continue
		}
		e.detail = joinDetail(e.detail, strings.TrimPrefix(t, "APPLY|FAIL|"))
		out[last] = e
	}
	for _, l := range splitLines(o.Stderr) {
		if m := ldapSkipRe.FindStringSubmatch(l); m != nil {
			for _, h := range strings.Split(m[1], ",") {
				if h = strings.TrimSpace(h); set[h] {
					out[h] = engHost{st: rsSkipAsset, detail: "자산현황(assets.txt)에 없음"}
				}
			}
		}
	}
	if len(out) == 0 && o.RC == 1 && !emptyGossh(o) && !o.Killed {
		return failMap(hosts, engineErr(o))
	}
	return out
}

// parseApplOut: gossh "host: APPL|…" 줄 → 호스트별 결과
func parseApplOut(o changeOut, hosts []string) map[string]engHost {
	if o.StartErr != nil {
		return failMap(hosts, "gossh 실행 실패: "+o.StartErr.Error())
	}
	set, out := hostSet(hosts), map[string]engHost{}
	for h, lines := range splitGossh(stripANSI(o.Stdout)) {
		if !set[h] {
			continue
		}
		rc, noScript, msg := "", false, ""
		for _, l := range lines {
			switch {
			case strings.HasPrefix(l, "APPL|NOSCRIPT"):
				noScript = true
			case strings.HasPrefix(l, "APPL|RC|"):
				rc = strings.TrimSpace(l[len("APPL|RC|"):])
			case strings.HasPrefix(l, "APPL|MSG|"):
				msg = joinDetail(msg, l[len("APPL|MSG|"):])
			}
		}
		switch {
		case noScript:
			out[h] = engHost{st: rsFail, detail: "원복 스크립트 없음 (" + applScriptPath + ")"}
		case rc == "0":
			out[h] = engHost{st: rsOK, detail: msg}
		case rc != "" && strings.Contains(msg, "원복할 파일이 없습니다"):
			out[h] = engHost{st: rsNoPending, detail: "대기 중인 /etc/auto.appl.bak 없음"}
		case rc != "":
			out[h] = engHost{st: rsFail, detail: joinDetail("종료코드 "+rc, msg)}
		default:
			out[h] = engHost{st: rsFail, detail: "알 수 없는 응답"}
		}
	}
	return out
}
