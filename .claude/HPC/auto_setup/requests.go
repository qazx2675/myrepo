// requests.go - 요청 채널: requests/<epoch>_<kind>.req 원자 기록(CLI·TUI·원격) + 데몬 5초 주기 수거(manual-run / cancel)
package main

import (
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
	ReqManualRun = "manual-run" // payload: jobid=<id> yml=<그룹>  → 그룹 호스트 전체가 완료일 때만 수락, 데몬 run 큐에 수동 run
	ReqCancel    = "cancel"     // payload: jobid=<id>            → auto_setup cancel 과 동일
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
	j, _ := d.findJob(id)
	if j == nil {
		return "job 없음"
	}
	hosts, ok := groupHosts(j, yml)
	if !ok || len(hosts) == 0 {
		return "그룹 없음"
	}
	var notDone []string
	for _, h := range hosts {
		if j.Hosts[h].Processed == "" {
			notDone = append(notDone, h)
		}
	}
	if len(notDone) > 0 {
		return fmt.Sprintf("완료 전 호스트 %d대 (%s)", len(notDone), strings.Join(notDone, " "))
	}
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
	hasOS6 := false
	ready := map[string]bool{}
	for _, n := range hosts {
		ready[n] = true
		if j.Hosts[n].Route == "os6" {
			hasOS6 = true
		}
	}
	c := &inflight{jobID: j.ID, user: j.User, hosts: hosts, ready: ready, at: now.Unix(),
		manual: true, reqPath: p, yml: pl["yml"], closed: closed}
	if d.runErr == "" {
		logf("수동 run 시작: job %s 그룹 %s %d대 (%s)", j.ID, pl["yml"], len(hosts), name)
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
		out[pl["jobid"]] = append(out[pl["jobid"]], SnapManual{File: name, Yml: pl["yml"], State: st, Requested: ep})
	}
	return out
}
