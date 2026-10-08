// requests.go - 요청 채널: requests/<epoch>_<kind>.req 원자 기록(CLI·TUI·원격) + 데몬 5초 주기 수거(manual-run / cancel)
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 요청 종류 (payload 는 key=value 줄)
const (
	ReqManualRun = "manual-run" // payload: jobid=<id> yml=<그룹>  → 완료 여부와 상관없이 수락, 그룹 호스트 전체를 데몬 run 큐에서 수동 run (접속불가는 wall 에 표시)
	ReqCancel    = "cancel"     // payload: jobid=<id>            → auto_setup cancel 과 동일
	ReqRefresh   = "refresh"    // payload: 없음                  → 다음 step 에서 ping·준비확인 즉시 수행 (TUI r 키)
	ReqRecheck   = "recheck"    // payload: jobid=<id> yml=<그룹|*> → 완료 제외 호스트를 os8 → os6_mgmt 순으로 직접 재확인 (TUI g 키)
)

// 디렉터리: requests/ (새 요청) → active/ (수락된 manual-run, 끝나면 삭제) | rejected/ (거부, 끝에 reason= 줄)
func requestsDir() string    { return filepath.Join(dataDir(), "requests") }
func reqActiveDir() string   { return filepath.Join(dataDir(), "requests", "active") }
func reqRejectedDir() string { return filepath.Join(dataDir(), "requests", "rejected") }

// 파일명: <epoch>_<kind>.req, 같은 초에 같은 kind 가 있으면 <epoch>_<kind>_<n>.req
var reqFileRe = regexp.MustCompile(`^(\d+)_([a-z][a-z0-9-]*)(?:_\d+)?\.req$`)
var reqKindRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var reqKeyRe = regexp.MustCompile(`^[a-z_]+$`)

// writeRequest: 요청 파일을 원자적으로 만든다(임시파일 → link, 기존 파일은 덮어쓰지 않음). 만든 파일 경로 반환.
// 결과 확인: 거부되면 requests/rejected/<같은 이름> 이 생긴다(마지막 줄 reason=…). 수락된 manual-run 은 active/ 에 있다가 run 이 끝나면 삭제.
func writeRequest(kind string, payload map[string]string) (string, error) {
	if !reqKindRe.MatchString(kind) {
		return "", fmt.Errorf("잘못된 요청 종류: %q", kind)
	}
	keys := make([]string, 0, len(payload))
	for k, v := range payload {
		if !reqKeyRe.MatchString(k) || strings.ContainsAny(v, "\r\n") {
			return "", fmt.Errorf("잘못된 payload: %q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k + "=" + payload[k] + "\n")
	}
	if err := os.MkdirAll(requestsDir(), 0755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(requestsDir(), ".tmp_")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(sb.String()); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	_ = os.Chmod(tmp, 0644)
	ep := time.Now().Unix()
	for n := 1; n < 1000; n++ {
		name := fmt.Sprintf("%d_%s.req", ep, kind)
		if n > 1 {
			name = fmt.Sprintf("%d_%s_%d.req", ep, kind, n)
		}
		p := filepath.Join(requestsDir(), name)
		err := os.Link(tmp, p)
		if err == nil {
			return p, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", errors.New("요청 파일 이름을 만들 수 없음")
}

// parseRequest: key=value 줄 → map (reason/state 등 데몬이 붙인 줄 포함)
func parseRequest(b []byte) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.IndexByte(line, '='); i > 0 {
			m[line[:i]] = line[i+1:]
		}
	}
	return m
}

// listReqFiles: d 의 요청 파일 이름 (이름순 = 시각순)
func listReqFiles(d string) []string {
	ents, err := os.ReadDir(d)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && reqFileRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// collectRequests: requests/*.req 수거 (데몬 step 마다). 처리한 요청은 requests/ 에서 사라진다.
func (d *Daemon) collectRequests(now time.Time) {
	for _, name := range listReqFiles(requestsDir()) {
		p := filepath.Join(requestsDir(), name)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		kind := reqFileRe.FindStringSubmatch(name)[2]
		pl := parseRequest(b)
		switch kind {
		case ReqManualRun:
			if reason := d.acceptManualRun(pl); reason != "" {
				rejectRequest(p, name, b, reason)
				continue
			}
			if err := os.MkdirAll(reqActiveDir(), 0755); err != nil {
				logf("[X] 요청 처리 실패(%s): %v", name, err)
				continue
			}
			if err := os.Rename(p, filepath.Join(reqActiveDir(), name)); err != nil {
				logf("[X] 요청 처리 실패(%s): %v", name, err)
				continue
			}
			logf("요청 수락: %s (job %s, 그룹 %s) → 수동 run 대기", name, pl["jobid"], pl["yml"])
		case ReqCancel:
			id := pl["jobid"]
			if err := cancelJob(id); err != nil {
				rejectRequest(p, name, b, err.Error())
				continue
			}
			d.removeJob(id)
			os.Remove(p)
			logf("요청 수락: %s → cancel %s", name, id)
		case ReqRefresh:
			d.lastPing, d.lastCheck = time.Time{}, time.Time{} // 이번 step 에서 바로 ping·준비확인
			// 정체 호스트는 ping 정보가 없거나 경로가 틀려 갱신이 멈춘 경우가 있어, 이번 확인에 강제로 포함해 재시도한다
			nStuck := 0
			for _, j := range d.jobs {
				for name, h := range j.Hosts {
					if active(h) && effectiveStage(h, now.Unix()) == StageStuck {
						d.forceCheck[name] = true
						nStuck++
					}
				}
			}
			if nStuck > 0 {
				logf("수동 재시도(r): 정체 호스트 %d대를 즉시 재확인", nStuck)
			}
			os.Remove(p)
		case ReqRecheck:
			if reason := d.recheckHosts(pl["jobid"], pl["yml"], now); reason != "" {
				rejectRequest(p, name, b, reason)
				continue
			}
			os.Remove(p)
		default:
			rejectRequest(p, name, b, "알 수 없는 요청 종류")
		}
	}
}

// findJob: 진행 중(메모리) → 종료(jobs/done) 순으로 찾는다. closed=true 면 jobs/done 의 job.
func (d *Daemon) findJob(id string) (j *Job, closed bool) {
	if j := d.jobs[id]; j != nil {
		return j, false
	}
	if !validID(id) {
		return nil, false
	}
	if j, err := loadJobFile(filepath.Join(doneDir(), id+".json")); err == nil {
		return j, true
	}
	return nil, false
}

// acceptManualRun: 수락이면 "", 거부면 사유
func (d *Daemon) acceptManualRun(pl map[string]string) string {
	id, yml := pl["jobid"], pl["yml"]
	if id == "" || yml == "" {
		return "jobid/yml 없음"
	}
	if m := pl["mode"]; m != "" && m != ModeCheck {
		return "알 수 없는 mode (check 만 허용)"
	}
	j, _ := d.findJob(id)
	if j == nil {
		return "job 없음"
	}
	hosts, ok := groupHosts(j, yml)
	if !ok || len(hosts) == 0 {
		return "그룹 없음"
	}
	// 완료 여부와 상관없이 그룹 전체를 시도한다 (접속불가 호스트는 수동 run 결과 wall 에 따로 알림)
	for _, name := range listReqFiles(reqActiveDir()) {
		b, _ := os.ReadFile(filepath.Join(reqActiveDir(), name))
		if q := parseRequest(b); q["jobid"] == id && q["yml"] == yml {
			return "같은 그룹 수동 run 이 이미 대기 중"
		}
	}
	return ""
}

// rejectRequest: requests/rejected/<name> 에 원문 + reason= 줄로 이동, 로그 1회(파일이 옮겨지므로 반복 없음)
func rejectRequest(p, name string, body []byte, reason string) {
	logf("[!] 요청 거부: %s (%s)", name, reason)
	if err := os.MkdirAll(reqRejectedDir(), 0755); err == nil {
		out := strings.TrimRight(string(body), "\n")
		if out != "" {
			out += "\n"
		}
		out += "reason=" + strings.ReplaceAll(reason, "\n", " ") + "\n"
		dst := filepath.Join(reqRejectedDir(), name)
		tmp := dst + ".tmp"
		if os.WriteFile(tmp, []byte(out), 0644) == nil && os.Rename(tmp, dst) == nil {
			os.Remove(p)
			return
		}
		os.Remove(tmp)
	}
	os.Remove(p) // 기록 실패해도 반복 처리하지 않음
}

// scheduleManual: run 이 없을 때 active/ 의 가장 오래된 수동 run 을 시작 (자동 run 이 우선)
func (d *Daemon) scheduleManual(now time.Time) {
	if d.cur != nil || now.Before(d.runRetry) {
		return
	}
	names := listReqFiles(reqActiveDir())
	if len(names) == 0 {
		return
	}
	name := names[0]
	p := filepath.Join(reqActiveDir(), name)
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	pl := parseRequest(b)
	j, closed := d.findJob(pl["jobid"])
	var hosts []string
	ok := false
	if j != nil {
		hosts, ok = groupHosts(j, pl["yml"])
	}
	if !ok || len(hosts) == 0 {
		rejectRequest(p, name, b, "job/그룹 없음 (수락 후 사라짐)")
		return
	}
	if pl["state"] != "running" {
		_ = os.WriteFile(p+".tmp", append(b, []byte("state=running\n")...), 0644)
		_ = os.Rename(p+".tmp", p)
	}
	d.refreshManualRoutes(j, hosts, closed)
	hasOS6 := false
	ready := map[string]bool{}
	for _, n := range hosts {
		ready[n] = true
		if j.Hosts[n].Route == "os6" {
			hasOS6 = true
		}
	}
	c := &inflight{jobID: j.ID, user: j.User, hosts: hosts, ready: ready, at: now.Unix(),
		manual: true, reqPath: p, yml: pl["yml"], closed: closed, mode: pl["mode"]}
	if d.runErr == "" {
		logf("수동 run 시작: job %s 그룹 %s %d대 (%s) [%s]", j.ID, pl["yml"], len(hosts), name, modeLabel(pl["mode"]))
	}
	d.launch(c, j, hasOS6)
}

// readActiveRequests: 스냅샷용 — dir/requests/active 의 수동 run (jobid 별)
func readActiveRequests(dir string) map[string][]SnapManual {
	out := map[string][]SnapManual{}
	ad := filepath.Join(dir, "requests", "active")
	for _, name := range listReqFiles(ad) {
		b, err := os.ReadFile(filepath.Join(ad, name))
		if err != nil {
			continue
		}
		pl := parseRequest(b)
		st := "queued"
		if pl["state"] == "running" {
			st = "running"
		}
		ep, _ := strconv.ParseInt(reqFileRe.FindStringSubmatch(name)[1], 10, 64)
		out[pl["jobid"]] = append(out[pl["jobid"]], SnapManual{File: name, Yml: pl["yml"], State: st, Requested: ep, Mode: pl["mode"]})
	}
	return out
}

// refreshManualRoutes: 수동 run 직전 경로 재판별. os8 에서 직접 응답하는 호스트는 route=local 로(설치 중 os6 로 붙은 뒤 그대로 남은
// 호스트도 포함 — os6 래퍼를 거치면 체크 출력이 비는 경우가 있다), os8 에서 응답하지 않고 os6_mgmt 경유로만 응답하는 호스트는
// route=os6 로 바꿔 run 이 올바른 gossh 를 쓰게 한다 (정체 등으로 경로가 틀어진 호스트도 일반 호스트처럼 체크되도록).
// os6_mgmt·os6_gossh 가 없으면 아무것도 하지 않는다. 종료된 job(closed)은 job 파일을 고치지 않고 이번 run 에만 반영한다.
func (d *Daemon) refreshManualRoutes(j *Job, hosts []string, closed bool) {
	if os6_mgmt == "" || os6_gossh == "" {
		return
	}
	var cand []string
	for _, n := range hosts {
		if h := j.Hosts[n]; h != nil {
			cand = append(cand, n)
		}
	}
	if len(cand) == 0 {
		return
	}
	res, err := d.Checker.Check("local", cand)
	if err != nil {
		logf("[X] 수동 run 경로 확인 실패(local): %v", err)
		return
	}
	var miss []string
	for _, n := range cand {
		if cr, ok := res[n]; ok && cr.Responded {
			if h := j.Hosts[n]; h.Route != "local" {
				old := h.Route
				h.Route = "local"
				d.markRoute(j, closed)
				if old == "os6" {
					logf("경로 전환: %s os6 → local (수동 run 전 확인: os8 직접 응답, job %s)", n, j.ID)
				}
			}
			continue
		}
		miss = append(miss, n)
	}
	if len(miss) == 0 {
		return
	}
	res, err = d.Checker.Check("os6", miss)
	if err != nil {
		logf("[X] 수동 run 경로 확인 실패(os6): %v", err)
		return
	}
	for _, n := range miss {
		if cr, ok := res[n]; ok && cr.Responded {
			if j.Hosts[n].Route != "os6" {
				j.Hosts[n].Route = "os6"
				d.markRoute(j, closed)
				logf("경로 전환: %s → os6 (수동 run 전 확인: os8 무응답, os6_mgmt 응답, job %s)", n, j.ID)
			}
		}
	}
}

// recheckHosts: g 키 — 진행 중 job 의 완료되지 않은 호스트(정체·실패 포함, 단계 무관)를 지금 직접 확인한다.
// 1) os8_mgmt 에서 준비확인 → 응답하면 route=local, 2) 무응답이면 os6_mgmt 경유 → 응답하면 route=os6,
// 3) 둘 다 무응답이면 접속불가로 간주(상태 그대로, 로그에 목록). 응답한 호스트는 준비확인 결과를 바로 반영하고 이미 READY 인 호스트는 경로만 갱신.
// yml 이 "" 또는 "*" 이면 job 전체, 아니면 그 그룹만. 진행 과정·실패 지점·최종 결과는 recheck/<jobid>.json 에 단계마다 기록되어
// 화면(g 진행 보기)에서 실시간으로 보인다. 수락이면 "", 거부면 사유.
func (d *Daemon) recheckHosts(id, yml string, now time.Time) string {
	j, closed := d.findJob(id)
	if j == nil {
		writeRecheck(&Recheck{Job: id, Yml: yml, At: now.Unix(), Done: true,
			Lines: []string{fmt.Sprintf("[%s] job 을 찾을 수 없습니다 (취소되었거나 오래전에 종료됨)", now.Format("15:04:05"))}})
		return "job 없음"
	}
	var names []string
	if yml == "" || yml == allView {
		for n := range j.Hosts {
			names = append(names, n)
		}
	} else {
		var ok bool
		if names, ok = groupHosts(j, yml); !ok {
			return "그룹 없음"
		}
	}
	var cand []string
	for _, n := range names {
		if h := j.Hosts[n]; h != nil && h.Processed == "" {
			cand = append(cand, n)
		}
	}
	sort.Strings(cand)
	t := now.Unix()
	rc := &Recheck{Job: j.ID, Yml: yml, At: t}
	if rc.Yml == "" {
		rc.Yml = allView
	}
	step := func(format string, a ...interface{}) {
		rc.Lines = append(rc.Lines, fmt.Sprintf("[%s] ", now.Format("15:04:05"))+fmt.Sprintf(format, a...))
		writeRecheck(rc)
	}
	if len(cand) == 0 {
		step("재확인할 호스트가 없습니다 (모두 완료)")
		rc.Done = true
		writeRecheck(rc)
		return ""
	}
	step("재확인 시작: %d대 (완료 제외, 단계 무관)", len(cand))
	detail := map[string]string{}
	var miss []string

	step("1/3 os8_mgmt 에서 직접 확인 중 (%d대)…", len(cand))
	res, err := d.Checker.Check("local", cand)
	if err != nil {
		step("    os8_mgmt 확인 호출 실패: %v → 전부 무응답으로 처리하고 다음 단계로", err)
		for _, n := range cand {
			detail[n] = "os8 확인 호출 실패"
		}
		miss = cand
	} else {
		nOK := 0
		for _, n := range cand {
			cr, ok := res[n]
			if !ok || !cr.Responded {
				detail[n] = "os8 무응답"
				miss = append(miss, n)
				continue
			}
			nOK++
			rc.Hosts = append(rc.Hosts, RecheckHost{Host: n, Result: "os8", Detail: d.recheckApply(j, n, "local", cr, t, closed)})
		}
		step("    os8_mgmt 응답 %d대 / 무응답 %d대", nOK, len(miss))
	}

	nOS6 := 0
	switch {
	case len(miss) == 0:
		step("2/3 os6_mgmt 경유 확인: 필요 없음 (전부 os8 에서 응답)")
	case os6_mgmt == "" || os6_gossh == "":
		step("2/3 os6_mgmt 경유 확인: 건너뜀 (os6_mgmt/os6_gossh 미설정) — 무응답 %d대는 접속불가", len(miss))
	default:
		step("2/3 os6_mgmt 경유로 확인 중 (%d대)…", len(miss))
		var left []string
		if res, err = d.Checker.Check("os6", miss); err != nil {
			step("    os6_mgmt 확인 호출 실패: %v", err)
			for _, n := range miss {
				detail[n] += " → os6 확인 호출 실패"
			}
			left = miss
		} else {
			for _, n := range miss {
				if cr, ok := res[n]; ok && cr.Responded {
					nOS6++
					rc.Hosts = append(rc.Hosts, RecheckHost{Host: n, Result: "os6", Detail: d.recheckApply(j, n, "os6", cr, t, closed)})
				} else {
					detail[n] += " → os6 무응답"
					left = append(left, n)
				}
			}
			step("    os6_mgmt 응답 %d대 / 무응답 %d대", nOS6, len(left))
		}
		miss = left
	}
	for _, n := range miss {
		rc.Hosts = append(rc.Hosts, RecheckHost{Host: n, Result: "fail", Detail: strings.TrimPrefix(strings.TrimSpace(detail[n]), "→ ")})
	}
	sort.Slice(rc.Hosts, func(a, b int) bool { return rc.Hosts[a].Host < rc.Hosts[b].Host })
	d.lastPing, d.lastCheck = time.Time{}, time.Time{} // ping 도 바로 갱신
	step("3/3 최종 결과: 응답 %d대 (os8 %d, os6 경유 %d), 접속불가 %d대", len(cand)-len(miss), len(cand)-len(miss)-nOS6, nOS6, len(miss))
	if closed { // 종료된 job: 바뀐 경로만 jobs/done 파일에 반영
		if _, err := writeJobFile(filepath.Join(doneDir(), j.ID+".json"), j); err != nil {
			logf("[X] job 저장 실패(%s): %v", j.ID, err)
		}
	}
	rc.Done = true
	writeRecheck(rc)
	logf("재확인(g): job %s %d대 중 응답 %d대 (os6 경유 %d대), 접속불가 %d대", j.ID, len(cand), len(cand)-len(miss), nOS6, len(miss))
	if len(miss) > 0 {
		logf("[!] 재확인(g) 접속불가·미응답 %d대 (job %s): %s", len(miss), j.ID, strings.Join(miss, " "))
	}
	return ""
}

// recheckApply: g 재확인에서 응답한 호스트 1대 반영 — 경로 갱신 + (아직 READY 전이면) 준비확인 결과 반영.
// 반환: 화면에 보일 설명 (경로 변경, 현재 단계)
func (d *Daemon) recheckApply(j *Job, name, route string, cr CheckResult, t int64, closed bool) string {
	h := j.Hosts[name]
	var notes []string
	if h.Route != route {
		old := h.Route
		h.Route = route
		d.markRoute(j, closed)
		if old != "" {
			logf("경로 전환: %s %s → %s (재확인 g, job %s)", name, old, route, j.ID)
			notes = append(notes, "경로 "+old+" → "+route)
		}
	}
	if active(h) && h.ReadyAt == 0 && !closed { // 종료된 job 은 경로만 갱신하고 단계는 건드리지 않음
		d.applyCheck(j, name, cr, t)
	}
	notes = append(notes, "단계 "+stageLabels[effectiveStage(h, t)])
	return strings.Join(notes, ", ")
}

// ---- 재확인(g) 진행 기록: recheck/<jobid>.json (단계마다 갱신 → 화면이 실시간으로 읽음) ----

func recheckDir() string { return filepath.Join(dataDir(), "recheck") }

func writeRecheck(rc *Recheck) {
	if !validID(rc.Job) {
		return
	}
	if err := os.MkdirAll(recheckDir(), 0755); err != nil {
		return
	}
	b, err := json.MarshalIndent(rc, "", " ")
	if err != nil {
		return
	}
	p := filepath.Join(recheckDir(), rc.Job+".json")
	if os.WriteFile(p+".tmp", append(b, '\n'), 0644) == nil {
		_ = os.Rename(p+".tmp", p)
	}
}

// readRecheck: dir/recheck/<jobid>.json (없거나 깨졌으면 nil)
func readRecheck(dir, jobID string) *Recheck {
	b, err := os.ReadFile(filepath.Join(dir, "recheck", jobID+".json"))
	if err != nil {
		return nil
	}
	rc := &Recheck{}
	if json.Unmarshal(b, rc) != nil {
		return nil
	}
	return rc
}

func (d *Daemon) markRoute(j *Job, closed bool) {
	if !closed {
		d.dirty[j.ID] = true
	}
}
