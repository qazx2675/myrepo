// os6run.go - os6_mgmt 직접 실행(TUI g/c/t, 데몬 없이 TUI 프로세스 안에서 1회): 공유 경로 <os6 auto_setup 디렉터리>/os6_run/ 의
// 잠금(active/)·run 기록(runs/)·완료기록(done/)·스냅샷 캐시(cache/), os8 데몬 쪽 수거(잠금 호스트 제외·완료기록 인정), 스냅샷 비고
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 디렉터리 구성 (<base>/os6_run/, autofs 공유 — os8_mgmt·os6_mgmt 에서 같은 경로, root 소유 0755·파일 0644):
//
//	active/<id>.lock : 실행 중 잠금 (JSON os6Lock). os6 TUI 가 os6Beat 마다 mtime 을 갱신, 끝나면 삭제
//	runs/<id>.json   : run 기록 (JSON os6RunRec, 시작 때 state=running → 끝나면 done/error)
//	runs/<id>.txt    : 결과 본문 (os6_mgmt 로컬 codes/<code>.txt 사본 — os8 code 번호와 겹치지 않게 따로 둔다)
//	done/<host>      : 완료기록 "epoch user sha256 os6_direct" (done.go 와 같은 형식·인정 규칙, 설정체크+설정수정·정상 종료 때만)
//	cache/snapshot.json : os6 TUI 가 마지막으로 받은 os8 스냅샷 (os8_mgmt 접속 불가 때 대신 보여 줌)
//
// 모든 파일은 같은 디렉터리의 임시 파일 → rename 으로 원자 기록한다 (NFS 에서도 rename 은 원자적).
const (
	os6RunName = "os6_run"
	os6DoneSrc = "os6_direct" // 완료기록 출처
	os6BusyMsg = "실행 중입니다 — 끝날 때까지 기다리세요"
)

var (
	// os6Beat: os6 TUI 가 잠금 mtime 을 갱신하는 주기
	os6Beat = 30 * time.Second
	// os6Stale: 데몬 — 잠금 mtime 이 (데몬 시계로) 이 시간 동안 바뀌지 않으면 응답 없는 잠금으로 보고 무시.
	// 서버 간 시계 차와 상관없도록 mtime 의 "값"이 아니라 "바뀌었는지"를 데몬 시계로 잰다 (NFS 속성 캐시 60초 여유 포함).
	os6Stale = 3 * time.Minute
	// os6StaleAbs: 상태를 기억하지 않는 쪽(스냅샷 비고·다른 os6 TUI) — 지금 − mtime 이 이보다 크면 응답 없는 잠금 (시계 차 여유 포함)
	os6StaleAbs = 15 * time.Minute
)

// newOS6Runner / newOS6Checker: os6 직접 실행이 쓰는 Runner·Checker (테스트에서 가짜로 교체).
// Runner 는 데몬과 같은 realRunner(hang_min 감시·code 파일·run 임시 디렉터리 정리 포함)를 이 서버의 체크스크립트로 쓴다.
var (
	newOS6Runner  = func(osCheck string) Runner { return realRunner{osCheck: osCheck, detach: true} }
	newOS6Checker = func() Checker { return NewRealChecker() }
)

// os6Direct: 이 프로세스가 os6_mgmt(원격 클라이언트, os8_mgmt 설정)에서 도는지 — TUI g/c/t 를 직접 실행한다
func os6Direct() bool { return os8_mgmt != "" }

// os6RunDir: <base>/os6_run. base 는
//   - os6_mgmt(원격 클라이언트): conf os6_autosetup, 비면 실행 파일(심볼릭 링크를 따라간 실제 위치)의 디렉터리
//   - os8_mgmt(데몬): conf os6_autosetup (autofs 공유라 os6_mgmt 와 같은 경로), 비면 "" = 사용 안 함
//
// os6_autosetup 에 실행 파일 경로를 적었으면 그 디렉터리를 쓴다 (probeRemote 와 같은 규칙).
func os6RunDir() string {
	p := os6_autosetup
	if p == "" {
		if !os6Direct() {
			return ""
		}
		if p = engineExeDir(); p == "" {
			return ""
		}
	}
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		p = filepath.Dir(p)
	}
	return filepath.Join(p, os6RunName)
}

// os6Lock: 실행 중 잠금 (active/<id>.lock)
type os6Lock struct {
	ID      string   `json:"id"`
	Server  string   `json:"server"` // 실행한 서버 (os.Hostname)
	PID     int      `json:"pid"`    // 실행한 TUI 프로세스
	Login   string   `json:"login"`  // 실행한 계정 (SUDO_USER → USER)
	Job     string   `json:"job"`
	User    string   `json:"user"` // job user (체크스크립트 인자)
	Yml     string   `json:"yml"`
	Mode    string   `json:"mode,omitempty"` // ModeCheck = 설정체크만
	Hosts   []string `json:"hosts"`
	Started int64    `json:"started"` // 시작 epoch (os6_mgmt 시계, 표시용)
}

// os6RunRec: run 기록 (runs/<id>.json)
type os6RunRec struct {
	os6Lock
	State     string   `json:"state"` // running | done | error
	Ended     int64    `json:"ended,omitempty"`
	Code      string   `json:"code,omitempty"` // os6_mgmt 로컬 code (os8 code 와 별개)
	Abnormal  bool     `json:"abnormal,omitempty"`
	Processed []string `json:"processed,omitempty"` // 체크 결과가 나온 호스트
	Failed    []string `json:"failed,omitempty"`    // 그중 FAIL 이 있는 호스트
	Error     string   `json:"error,omitempty"`
	Result    string   `json:"result,omitempty"` // 결과 본문 (os6_run 기준 상대 경로 runs/<id>.txt)
}

// os6WriteAtomic: 같은 디렉터리의 임시 파일 → rename (0644)
func os6WriteAtomic(p string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp")
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(f.Name(), 0644)
	}
	if werr == nil {
		werr = os.Rename(f.Name(), p)
	}
	if werr != nil {
		os.Remove(f.Name())
	}
	return werr
}

func os6WriteJSON(p string, v interface{}) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	return os6WriteAtomic(p, append(b, '\n'))
}

// readOS6Locks: active/*.lock (깨진 파일은 건너뜀). mtime 도 함께.
func readOS6Locks(dir string) (map[string]os6Lock, map[string]time.Time, error) {
	ents, err := os.ReadDir(filepath.Join(dir, "active"))
	if err != nil {
		return nil, nil, err
	}
	locks, mt := map[string]os6Lock{}, map[string]time.Time{}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".lock") {
			continue
		}
		p := filepath.Join(dir, "active", name)
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var lk os6Lock
		if json.Unmarshal(b, &lk) != nil || len(lk.Hosts) == 0 {
			continue
		}
		locks[name], mt[name] = lk, fi.ModTime()
	}
	return locks, mt, nil
}

// os6LockDead: 상태 없이 판단하는 응답 없는 잠금 — 같은 서버이고 그 pid 가 없거나, mtime 이 os6StaleAbs 보다 오래됨
func os6LockDead(lk os6Lock, mtime, now time.Time) bool {
	if h, _ := os.Hostname(); h != "" && lk.Server == h && lk.PID > 0 {
		if err := syscall.Kill(lk.PID, 0); err == syscall.ESRCH {
			return true
		}
	}
	return now.Sub(mtime) > os6StaleAbs
}

// os6HeldHosts: (os6 TUI) 다른 os6 직접 실행이 잡고 있는 호스트 → 잠금 id. 같은 서버의 죽은 잠금은 지운다.
func os6HeldHosts(dir string, now time.Time) map[string]string {
	out := map[string]string{}
	locks, mt, err := readOS6Locks(dir)
	if err != nil {
		return out
	}
	for name, lk := range locks {
		if os6LockDead(lk, mt[name], now) {
			if h, _ := os.Hostname(); lk.Server == h {
				os.Remove(filepath.Join(dir, "active", name))
			}
			continue
		}
		for _, n := range lk.Hosts {
			out[n] = lk.ID
		}
	}
	return out
}

// newOS6Lock: 이 프로세스의 잠금 내용 (id = <epoch>_<서버 짧은 이름>_<pid>)
func newOS6Lock(j *Job, yml, mode string, hosts []string, now time.Time) os6Lock {
	srv, _ := os.Hostname()
	short := shortHost(srv)
	if !userNameRe.MatchString(short) {
		short = "os6"
	}
	login := os.Getenv("SUDO_USER")
	if login == "" {
		login = os.Getenv("USER")
	}
	return os6Lock{ID: fmt.Sprintf("%d_%s_%d", now.Unix(), short, os.Getpid()), Server: srv, PID: os.Getpid(), Login: login,
		Job: j.ID, User: j.User, Yml: yml, Mode: mode, Hosts: append([]string(nil), hosts...), Started: now.Unix()}
}

// os6Acquire: 디렉터리 준비 + 잠금·run 기록(state=running) 작성. 공유 경로에 못 쓰면 실행하지 않도록 오류 (잠금 없이 돌면 os8 과 겹칠 수 있음).
func os6Acquire(dir string, lk os6Lock) (string, error) {
	if dir == "" {
		return "", errors.New("os6_run 경로를 정하지 못했습니다 (conf os6_autosetup 확인)")
	}
	for _, d := range []string{"active", "runs", "done"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			return "", fmt.Errorf("%s 에 쓸 수 없습니다: %v (공유 경로·root 권한 확인)", dir, err)
		}
	}
	lp := filepath.Join(dir, "active", lk.ID+".lock")
	if err := os6WriteJSON(lp, lk); err != nil {
		return "", fmt.Errorf("잠금 기록 실패(%s): %v", lp, err)
	}
	if err := os6WriteJSON(filepath.Join(dir, "runs", lk.ID+".json"), os6RunRec{os6Lock: lk, State: "running"}); err != nil {
		os.Remove(lp)
		return "", fmt.Errorf("run 기록 실패(%s): %v", dir, err)
	}
	return lp, nil
}

// os6Execute: 잠금을 잡은 채 Runner 1회 실행 (hasOS6=false — 이 서버가 os6_mgmt). 잠금은 os6Beat 마다 mtime 갱신.
// 끝나면 결과 본문·완료기록·run 기록을 남기고 잠금을 지운다. 반환: 최종 run 기록과 결과 본문.
func os6Execute(dir string, r Runner, j *Job, lk os6Lock, lockPath string, clock func() time.Time) (os6RunRec, string) {
	stop, beatDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(beatDone)
		t := time.NewTicker(os6Beat)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				n := time.Now()
				_ = os.Chtimes(lockPath, n, n)
			}
		}
	}()
	logf("os6 직접 실행 시작: %s job %s 그룹 %s %d대 [%s]", lk.ID, j.ID, lk.Yml, len(lk.Hosts), modeLabel(lk.Mode))
	res, err := r.Run(j, lk.Hosts, false)
	close(stop)
	<-beatDone

	rec := os6RunRec{os6Lock: lk, State: "done", Ended: clock().Unix(), Code: res.Code, Abnormal: res.Abnormal,
		Processed: res.Processed, Failed: res.Failed, Result: filepath.Join("runs", lk.ID+".txt")}
	var text string
	if err != nil {
		rec.State, rec.Error = "error", err.Error()
		text = "[X] 실행 실패: " + err.Error() + "\n"
	} else if b, rerr := readCode(res.Code); rerr == nil {
		text = string(b)
	} else {
		text = fmt.Sprintf("[X] 결과 파일(code %s)을 읽지 못했습니다: %v\n", res.Code, rerr)
	}
	if werr := os6WriteAtomic(filepath.Join(dir, rec.Result), []byte(text)); werr != nil {
		logf("[X] os6 직접 실행 결과 기록 실패(%s): %v", dir, werr)
	}
	if err == nil && !res.Abnormal && lk.Mode != ModeCheck { // 설정체크만·비정상 종료는 완료 처리하지 않는다 (수동 run 과 같음)
		if n, werr := os6WriteDone(dir, j, res, rec.Ended); werr != nil {
			logf("[X] os6 직접 실행 완료기록 실패(%s): %v", dir, werr)
		} else if n > 0 {
			logf("os6 직접 실행 완료기록: %d대 → %s", n, filepath.Join(dir, "done"))
		}
	}
	if werr := os6WriteJSON(filepath.Join(dir, "runs", lk.ID+".json"), rec); werr != nil {
		logf("[X] os6 직접 실행 run 기록 실패(%s): %v", dir, werr)
	}
	os.Remove(lockPath)
	logf("os6 직접 실행 끝: %s code %s 처리 %d대 상태 %s", lk.ID, res.Code, len(res.Processed), rec.State)
	return rec, text
}

// os6WriteDone: 처리된 호스트마다 done/<host> 완료기록 (config_check 의 auto_done_dir 과 같은 형식 — sha256 = postapply 의 그 호스트 줄)
func os6WriteDone(dir string, j *Job, res RunResult, epoch int64) (int, error) {
	post, _ := os.ReadFile(filepath.Join(runsDir(), res.Code, "check.res_"+j.User+"_postapply"))
	n := 0
	for _, h := range res.Processed {
		if !validHostName(h) {
			continue
		}
		rec := doneRecord{Epoch: epoch, User: j.User, Sha: postHostSha(string(post), h), Source: os6DoneSrc}
		if err := writeDoneRecordIn(filepath.Join(dir, "done"), h, rec); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// postHostSha: post 에서 "host:" 로 시작하는 줄들(각 줄 + 줄바꿈)의 sha256 (config_check auto_record_done 의 awk|sha256sum 과 같음)
func postHostSha(post, host string) string {
	var b strings.Builder
	for _, l := range strings.Split(post, "\n") {
		if strings.HasPrefix(l, host+":") {
			b.WriteString(l + "\n")
		}
	}
	s := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(s[:])
}

// ---- os8 데몬 쪽 ----

// os6Seen: 데몬이 본 잠금 하나 (mtime 이 마지막으로 바뀐 것을 본 데몬 시각으로 응답 여부를 판단)
type os6Seen struct {
	mtime time.Time
	at    time.Time
	stale bool
	lk    os6Lock
}

// os6Scan: 매 주기 os6_run/active 를 읽어 d.os6Hold(호스트 → 잠금 id) 를 만든다. 응답 없는 잠금(os6Stale 동안 갱신 없음)은 제외.
// 잠금이 생기거나 없어지거나 응답 없음으로 바뀔 때만 로그, 읽기 오류(권한 등)는 같은 내용 1회.
func (d *Daemon) os6Scan(now time.Time) {
	d.os6Hold = map[string]string{}
	dir := os6RunDir()
	if dir == "" {
		return
	}
	if d.os6Seen == nil {
		d.os6Seen = map[string]*os6Seen{}
		if _, err := os.Stat(dir); err != nil {
			logf("os6 직접 실행 기록 경로: %s (아직 없음 — os6_mgmt 에서 실행한 적이 없거나 공유 경로가 아님)", dir)
		} else {
			logf("os6 직접 실행 기록 경로: %s", dir)
		}
	}
	locks, mt, err := readOS6Locks(dir)
	if err != nil && !os.IsNotExist(err) {
		if msg := err.Error(); msg != d.os6Err {
			d.os6Err = msg
			logf("[X] os6 직접 실행 잠금 읽기 실패: %v (os8 run 과 겹칠 수 있음)", err)
		}
		return // 읽지 못한 동안은 직전 판단을 버린다 (잠금 없음으로 간주)
	}
	d.os6Err = ""
	for name, s := range d.os6Seen {
		if _, ok := locks[name]; !ok {
			logf("os6 직접 실행 잠금 해제: %s (%d대)", s.lk.ID, len(s.lk.Hosts))
			delete(d.os6Seen, name)
		}
	}
	for name, lk := range locks {
		s := d.os6Seen[name]
		if s == nil {
			s = &os6Seen{mtime: mt[name], at: now, lk: lk}
			d.os6Seen[name] = s
			logf("os6 직접 실행 감지: %s job %s 그룹 %s %d대 (%s@%s) → os8 run 에서 제외", lk.ID, lk.Job, lk.Yml, len(lk.Hosts), lk.Login, lk.Server)
		} else if !mt[name].Equal(s.mtime) {
			s.mtime, s.at = mt[name], now
			if s.stale {
				s.stale = false
				logf("os6 직접 실행 잠금 다시 갱신됨: %s → os8 run 에서 제외", lk.ID)
			}
		}
		if now.Sub(s.at) > os6Stale {
			if !s.stale {
				s.stale = true
				logf("[!] os6 직접 실행 잠금 응답 없음(%s 동안 갱신 없음) → 무시: %s (%d대)", os6Stale, lk.ID, len(lk.Hosts))
			}
			continue
		}
		for _, n := range lk.Hosts {
			d.os6Hold[n] = lk.ID
		}
	}
}

// ---- 스냅샷 비고 (os8 의 snapshot·TUI) ----

// os6SnapInfo: 실행 중 호스트(잠금, 응답 없으면 stale)와 호스트별 마지막 끝난 run
type os6SnapInfo struct {
	active map[string]bool // 호스트 → 응답 없는 잠금이면 false
	last   map[string]*os6RunRec
}

func readOS6SnapInfo(now time.Time) *os6SnapInfo {
	dir := os6RunDir()
	if dir == "" {
		return nil
	}
	info := &os6SnapInfo{active: map[string]bool{}, last: map[string]*os6RunRec{}}
	if locks, mt, err := readOS6Locks(dir); err == nil {
		for name, lk := range locks {
			alive := now.Sub(mt[name]) <= os6StaleAbs
			for _, n := range lk.Hosts {
				info.active[n] = info.active[n] || alive
			}
		}
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "runs"))
	since := now.Add(-closedKeepDur()).Unix()
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if ep, _, ok := strings.Cut(e.Name(), "_"); ok { // id = <시작 epoch>_… — 오래된 기록은 읽지 않는다 (runs/ 는 지우지 않아 계속 쌓임)
			if n, err := strconv.ParseInt(ep, 10, 64); err == nil && n < since {
				continue
			}
		}
		b, err := os.ReadFile(filepath.Join(dir, "runs", e.Name()))
		if err != nil {
			continue
		}
		rec := &os6RunRec{}
		if json.Unmarshal(b, rec) != nil || rec.State == "running" || rec.Started < since {
			continue
		}
		for _, n := range rec.Hosts {
			if p := info.last[n]; p == nil || rec.Started > p.Started {
				info.last[n] = rec
			}
		}
	}
	if len(info.active) == 0 && len(info.last) == 0 {
		return nil
	}
	return info
}

// note: 호스트 비고에 붙일 os6 직접 실행 상태 (job 전달 이후 run 만)
func (o *os6SnapInfo) note(host string, submitted int64) string {
	if alive, ok := o.active[host]; ok {
		if alive {
			return "os6_mgmt 직접 실행 중"
		}
		return "os6_mgmt 직접 실행 잠금(응답 없음)"
	}
	r := o.last[host]
	if r == nil || r.Started < submitted {
		return ""
	}
	what := "os6 직접"
	if r.Mode == ModeCheck {
		what += "(체크만)"
	}
	what += " " + time.Unix(r.Started, 0).Format("01-02 15:04") + " "
	in := func(l []string) bool {
		for _, n := range l {
			if n == host {
				return true
			}
		}
		return false
	}
	switch {
	case r.State == "error":
		return what + "실행 실패"
	case r.Abnormal:
		return what + "비정상 종료"
	case in(r.Failed):
		return what + "FAIL"
	case in(r.Processed):
		return what + "OK"
	}
	return what + "접속불가"
}

// annotateOS6: 스냅샷 job 의 호스트 비고에 os6 직접 실행 상태를 붙인다
func annotateOS6(sj *SnapJob, o *os6SnapInfo) {
	if o == nil {
		return
	}
	for gi := range sj.Groups {
		for hi := range sj.Groups[gi].Hosts {
			h := &sj.Groups[gi].Hosts[hi]
			if n := o.note(h.Host, sj.Submitted); n != "" {
				if h.Note != "" {
					n = h.Note + ", " + n
				}
				h.Note = n
			}
		}
	}
}

// ---- 스냅샷 캐시 (os6 TUI) ----

// snapCachedErr: os8_mgmt 스냅샷을 못 받아 os6 에 저장된 스냅샷을 대신 돌려줌 (TUI 는 스냅샷을 쓰고 안내 줄에 이 문구)
type snapCachedErr struct {
	err error
	at  time.Time
}

func (e *snapCachedErr) Error() string {
	return fmt.Sprintf("os8_mgmt 접속 불가 — 저장된 스냅샷(%s, %d분 전) 표시 중 (단계·진행률은 그 시각 기준, g·c·t 는 os6_mgmt 에서 직접 실행): %v",
		e.at.Format(snapTimeLayout), int(time.Since(e.at)/time.Minute), e.err)
}

func snapCachePath() string {
	if d := os6RunDir(); d != "" {
		return filepath.Join(d, "cache", "snapshot.json")
	}
	return ""
}

// saveSnapCache: 받은 스냅샷 저장 (실패는 무시 — 캐시일 뿐)
func saveSnapCache(s Snapshot) {
	p := snapCachePath()
	if p == "" || os.MkdirAll(filepath.Dir(p), 0755) != nil {
		return
	}
	if b, err := snapshotJSON(s); err == nil {
		_ = os6WriteAtomic(p, b)
	}
}

// cachedSnapshot: 원격 스냅샷 실패(err) 시 저장된 스냅샷 — 있으면 (스냅샷, *snapCachedErr), 없으면 (빈 스냅샷, 안내를 붙인 err)
func cachedSnapshot(err error) (Snapshot, error) {
	p := snapCachePath()
	if p == "" {
		return Snapshot{}, err
	}
	b, rerr := os.ReadFile(p)
	var s Snapshot
	if rerr == nil {
		rerr = json.Unmarshal(b, &s)
	}
	if rerr != nil {
		return Snapshot{}, fmt.Errorf("%v (저장된 스냅샷 없음: %s)", err, p)
	}
	return s, &snapCachedErr{err: err, at: time.Unix(s.Now, 0)}
}

// ---- os6 직접 실행 대상 ----

// os6DirectScript: os6_mgmt 의 체크스크립트 — os6_os_check_sh(“-” 제외) 우선, 없으면 os_check_sh
func os6DirectScript() string {
	if os6_os_check_sh != "" && os6_os_check_sh != "-" {
		return os6_os_check_sh
	}
	return os_check_sh
}

// os6EnsureGossh: PATH 에 gossh 가 없고 os6_gossh 가 있으면 그 디렉터리를 PATH 앞에 (체크스크립트·준비확인이 gossh 를 PATH 로 찾는다)
func os6EnsureGossh() {
	if _, err := exec.LookPath("gossh"); err == nil || os6_gossh == "" {
		return
	}
	os.Setenv("PATH", filepath.Dir(os6_gossh)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// os8BusyHosts: os8 스냅샷에서 os8 데몬이 지금 실행 중인 호스트 (체크중·2차체크 단계, 실행 중 수동 run 의 그룹 호스트)
func os8BusyHosts(s Snapshot) map[string]bool {
	out := map[string]bool{}
	for _, j := range s.Jobs {
		running := map[string]bool{}
		for _, m := range j.Manual {
			if m.State == "running" {
				running[m.Yml] = true
			}
		}
		for _, g := range j.Groups {
			for _, h := range g.Hosts {
				if h.Stage == StageChecking || h.Stage == StageSecond || running[g.Yml] {
					out[h.Host] = true
				}
			}
		}
	}
	return out
}
