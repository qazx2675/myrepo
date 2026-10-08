// done.go - 완료기록(<AUTO_SETUP_DIR>/done/<host>) 인정 규칙·수거, 수동 완료(markDone)
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 완료기록 한 줄: "epoch user sha256 source"
//
//	epoch : 기록 시각(os_check 가 정상 종료한 시각)
//	sha256: 그 run 의 check.res_<user>_postapply 중 해당 호스트 줄의 해시 (수동은 "-")
//	source: 기록한 곳 (os8/os6/호스트명 등, 수동 완료는 "manual")
type doneRecord struct {
	Epoch  int64
	User   string
	Sha    string
	Source string
}

// doneFutureSlack: 완료기록 시각이 지금보다 이만큼 넘게 미래면 위조로 보고 거부 (서버 간 시계 차 허용)
var doneFutureSlack = 5 * time.Minute

func doneRecDir() string     { return filepath.Join(dataDir(), "done") }
func doneAppliedDir() string { return filepath.Join(dataDir(), "done", "applied") }

func parseDoneRecord(b []byte) (doneRecord, error) {
	line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
	f := strings.Fields(line)
	if len(f) != 4 {
		return doneRecord{}, errors.New("형식 오류(epoch user sha256 source)")
	}
	e, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil || e <= 0 {
		return doneRecord{}, errors.New("epoch 형식 오류")
	}
	return doneRecord{Epoch: e, User: f[1], Sha: f[2], Source: f[3]}, nil
}

// judgeDoneRecord: 완료기록 인정 규칙 (순수 함수)
//
//	인정: record.epoch >= max(job.submitted, 마지막 부팅)
//	      마지막 부팅 = host.boot_at(준비확인 uptime 기준) — 그 뒤에 seen_down(재부팅)을 봤으면 down_at
//	보류(pending): 재설치 증거(seen_down 또는 전달 이후 부팅)가 아직 없음 → 조용히 대기
//	수동(source=manual): 부팅 시각 검증 없이 인정, 단 전달 이전 기록은 거부(다음 job 에 새지 않도록)
func judgeDoneRecord(rec doneRecord, j *Job, h *Host) (ok, pending bool, reason string) {
	if rec.Epoch < j.Submitted {
		return false, false, "전달 이전 기록"
	}
	if rec.Source == DoneSrcManual {
		return true, false, ""
	}
	if !h.SeenDown && h.BootAt < j.Submitted {
		return false, true, ""
	}
	boot := h.BootAt
	if h.DownAt > boot {
		boot = h.DownAt
	}
	if rec.Epoch < boot {
		return false, false, "마지막 부팅 이전 기록"
	}
	return true, false, ""
}

// acceptDoneRecords: done/<host> 를 읽어 진행 중 job 의 미처리 호스트에 대해 판정.
// 인정 → processed(done_src=external|manual), 기록은 done/applied/ 로 이동. 거부 → 그대로 두고 같은 내용은 로그 1회.
// 진행 중 run 대상 호스트는 run 결과 반영 후 판단(자기 run 의 기록을 external 로 오인하지 않도록).
func (d *Daemon) acceptDoneRecords(now time.Time) {
	ents, err := os.ReadDir(doneRecDir())
	if err != nil || len(ents) == 0 {
		return
	}
	owner := map[string]*Job{}
	for _, j := range d.jobs {
		for name := range j.Hosts {
			owner[name] = j
		}
	}
	inRun := map[string]bool{}
	if d.cur != nil {
		for _, n := range d.cur.hosts {
			inRun[n] = true
		}
	}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		j := owner[name]
		if j == nil || inRun[name] && d.cur.jobID == j.ID {
			continue
		}
		h := j.Hosts[name]
		if h.Processed != "" {
			continue
		}
		p := filepath.Join(doneRecDir(), name)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		key := name + "\x00" + string(b)
		rec, err := parseDoneRecord(b)
		if err != nil {
			d.rejectDoneOnce(key, name, j.ID, err.Error())
			continue
		}
		if rec.Source != DoneSrcManual && rec.Epoch > now.Unix()+secs(doneFutureSlack) { // 위조 시각(미래) — 부팅 이후 규칙 우회 방지 (수동은 원래 시각 검증 없음)
			d.rejectDoneOnce(key, name, j.ID, fmt.Sprintf("미래 시각 기록: %s", time.Unix(rec.Epoch, 0).Format(snapTimeLayout)))
			continue
		}
		ok, pending, reason := judgeDoneRecord(rec, j, h)
		if pending {
			continue
		}
		if !ok {
			d.rejectDoneOnce(key, name, j.ID, fmt.Sprintf("%s: 기록 %s", reason, time.Unix(rec.Epoch, 0).Format(snapTimeLayout)))
			continue
		}
		src := DoneSrcExternal
		if rec.Source == DoneSrcManual {
			src = DoneSrcManual
			logf("[!] 수동 완료 처리(부팅 시각 검증 없음): %s (job %s)", name, j.ID)
		} else {
			logf("완료기록 인정: %s (job %s, 출처 %s) → run 대상 제외", name, j.ID, rec.Source)
		}
		h.Processed, h.DoneSrc, h.Fails = src, src, 0
		d.setStage(j, name, StageDone, now.Unix())
		d.dirty[j.ID] = true
		delete(d.doneLogged, key)
		if err := os.MkdirAll(doneAppliedDir(), 0755); err == nil {
			if err := os.Rename(p, filepath.Join(doneAppliedDir(), name)); err != nil {
				logf("[X] 완료기록 이동 실패(%s): %v", name, err)
			}
		}
	}
}

func (d *Daemon) rejectDoneOnce(key, host, jobID, reason string) {
	if d.doneLogged[key] {
		return
	}
	d.doneLogged[key] = true
	logf("[!] 완료기록 거부: %s (job %s, %s)", host, jobID, reason)
}

// markDone: CLI `auto_setup done <host...>` — 출처 source(보통 "manual")로 완료기록을 원자 기록.
// 데몬이 다음 주기(5초)에 수거한다(데몬이 없으면 기동 후). 시각 검증이 없으므로 경고 로그를 남긴다.
func markDone(hosts []string, source string) error {
	if len(hosts) == 0 {
		return errors.New("호스트가 없습니다")
	}
	if source == "" || strings.ContainsAny(source, " \t\n/") {
		return fmt.Errorf("잘못된 출처: %q", source)
	}
	for _, h := range hosts {
		if !validHostName(h) {
			return fmt.Errorf("잘못된 호스트명: %q", h)
		}
	}
	user := os.Getenv("SUDO_USER")
	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" || strings.ContainsAny(user, " \t\n") {
		user = "-"
	}
	now := time.Now().Unix()
	for _, h := range hosts {
		if err := writeDoneRecord(h, doneRecord{Epoch: now, User: user, Sha: "-", Source: source}); err != nil {
			return err
		}
	}
	logf("[!] 수동 완료 요청(%s, 시각 검증 없음): %s", source, strings.Join(hosts, " "))
	return nil
}

func validHostName(h string) bool {
	return h != "" && h != "." && h != ".." && h != "applied" && !strings.HasPrefix(h, ".") &&
		!strings.ContainsAny(h, "/\\ \t\n")
}

// writeDoneRecord: done/<host> 에 tmp → rename 원자 기록
func writeDoneRecord(host string, r doneRecord) error {
	if err := os.MkdirAll(doneRecDir(), 0755); err != nil {
		return err
	}
	p := filepath.Join(doneRecDir(), host)
	tmp := filepath.Join(doneRecDir(), "."+host+".tmp")
	line := fmt.Sprintf("%d %s %s %s\n", r.Epoch, r.User, r.Sha, r.Source)
	if err := os.WriteFile(tmp, []byte(line), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
