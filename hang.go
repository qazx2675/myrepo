// hang.go - run 의 gossh 멈춤 감시 (conf hang_min): 오래 끝나지 않는 gossh 호출에 INT 두 번(걸린 호스트만 취소하고 계속), 그래도 남으면 TERM
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// gossh 는 1초 안에 Ctrl+C 를 두 번 받으면 아직 진행 중인 호스트를 취소하고(<목록>_res_cancel) 나머지 결과로 끝난다.
// 체크스크립트(config_check)는 gossh 구간에서 INT 를 무시하지만 gossh 는 직접 받는다 → gossh 프로세스에만 보낸다.
const hangGap = 300 * time.Millisecond

// hangPoll: 감시 주기 (테스트는 줄인다)
var hangPoll = 10 * time.Second

// hangTermAfter: INT×2 뒤에도 이 시간 동안 끝나지 않으면 TERM (취소 기능이 없는 gossh·반응 없음 대비, 사용자가 pkill 하던 것과 같음)
func hangTermAfter(limit time.Duration) time.Duration { return limit + time.Minute }

type procInfo struct {
	pid, ppid int
	start     string // /proc/<pid>/stat 의 starttime (pid 재사용 구분)
	comm      string
	argv      []string
}

// readProcs: root(/proc) 의 프로세스 목록. 읽는 중 사라진 프로세스는 건너뛴다.
func readProcs(root string) map[int]*procInfo {
	out := map[int]*procInfo{}
	ents, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "stat"))
		if err != nil {
			continue
		}
		// "pid (comm) state ppid ... starttime(22번째)" — comm 에 공백·괄호가 있을 수 있어 마지막 ')' 기준
		s := string(b)
		i, k := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
		if i < 0 || k < i {
			continue
		}
		f := strings.Fields(s[k+1:])
		if len(f) < 20 {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		p := &procInfo{pid: pid, ppid: ppid, start: f[19], comm: s[i+1 : k]}
		if c, err := os.ReadFile(filepath.Join(root, e.Name(), "cmdline")); err == nil {
			p.argv = strings.Split(strings.TrimRight(string(c), "\x00"), "\x00")
		}
		out[pid] = p
	}
	return out
}

// descendants: root 의 자손 pid (root 제외), 작은 pid 순
func descendants(procs map[int]*procInfo, root int) []int {
	kids := map[int][]int{}
	for pid, p := range procs {
		kids[p.ppid] = append(kids[p.ppid], pid)
	}
	var out []int
	q := []int{root}
	seen := map[int]bool{root: true}
	for len(q) > 0 {
		x := q[0]
		q = q[1:]
		for _, c := range kids[x] {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
				q = append(q, c)
			}
		}
	}
	sort.Ints(out)
	return out
}

// hangWatch: run 1개(체크스크립트 프로세스 root 의 자손)의 gossh 감시. 다른 run·사용자가 띄운 gossh 는 자손이 아니라 건드리지 않는다.
//   - 실제 gossh (comm=gossh, os6 래퍼가 아님): limit 넘으면 INT×2, 그 뒤 term 넘게 남으면 TERM
//   - os6 래퍼(bash <data>/bin/gossh, 맨 위 프로세스만 — 같은 명령줄의 서브셸 제외): limit 넘으면 USR1
//     (래퍼가 os6_mgmt 의 gossh 에 INT×2), 그 뒤 term 넘게 남으면 USR2 (os6_mgmt 의 gossh 에 TERM).
//     래퍼가 이 서버에서 같이 돌린 실제 gossh(os8 호스트분)는 위 규칙대로 따로 처리된다.
//
// 경과 시간은 감시가 처음 본 시각부터 센다 (감시 주기만큼 늦을 수 있음).
type hangWatch struct {
	code     string
	root     int
	limit    time.Duration
	term     time.Duration
	wrapper  string // os6 래퍼 경로 (<data>/bin/gossh)
	procRoot string
	now      func() time.Time
	kill     func(pid int, sig syscall.Signal) error
	seen     map[string]*hangProc // 키 = pid:starttime
}

type hangProc struct {
	first, intAt time.Time
	termed       bool
}

func newHangWatch(code string, root int, limit time.Duration) *hangWatch {
	return &hangWatch{code: code, root: root, limit: limit, term: hangTermAfter(limit),
		wrapper: filepath.Join(binDir(), "gossh"), procRoot: "/proc", now: time.Now, kill: syscall.Kill,
		seen: map[string]*hangProc{}}
}

func (w *hangWatch) isWrapper(p *procInfo) bool {
	return p != nil && len(p.argv) >= 2 && p.argv[1] == w.wrapper
}

// scan: 한 번 확인하고 필요한 시그널을 보낸다
func (w *hangWatch) scan() {
	now := w.now()
	procs := readProcs(w.procRoot)
	alive := map[string]bool{}
	for _, pid := range descendants(procs, w.root) {
		p := procs[pid]
		wrap := w.isWrapper(p)
		if wrap && w.isWrapper(procs[p.ppid]) { // 래퍼의 서브셸 (fork 라 명령줄이 같음)
			continue
		}
		if !wrap && p.comm != "gossh" {
			continue
		}
		key := fmt.Sprintf("%d:%s", pid, p.start)
		alive[key] = true
		s := w.seen[key]
		if s == nil {
			s = &hangProc{first: now}
			w.seen[key] = s
		}
		switch {
		case s.intAt.IsZero() && now.Sub(s.first) >= w.limit:
			s.intAt = now
			mins := int(now.Sub(s.first) / time.Minute)
			if wrap {
				logf("[!] gossh hang 감지(os6_mgmt 경유): code=%s pid=%d %d분 경과 → os6_mgmt 의 걸린 호스트 취소(INT×2)", w.code, pid, mins)
				_ = w.kill(pid, syscall.SIGUSR1)
				continue
			}
			logf("[!] gossh hang 감지: code=%s pid=%d %d분 경과 → 걸린 호스트 취소(INT×2)", w.code, pid, mins)
			_ = w.kill(pid, syscall.SIGINT)
			time.Sleep(hangGap)
			_ = w.kill(pid, syscall.SIGINT)
		case !s.intAt.IsZero() && !s.termed && now.Sub(s.intAt) >= w.term:
			s.termed = true
			mins := int(now.Sub(s.intAt) / time.Minute)
			if wrap {
				logf("[!] gossh 가 취소 후에도 %d분 남음(os6_mgmt 경유): code=%s pid=%d → os6_mgmt 의 gossh 종료(TERM)", mins, w.code, pid)
				_ = w.kill(pid, syscall.SIGUSR2)
				continue
			}
			logf("[!] gossh 가 취소 후에도 %d분 남음: code=%s pid=%d → 종료(TERM)", mins, w.code, pid)
			_ = w.kill(pid, syscall.SIGTERM)
		}
	}
	for k := range w.seen {
		if !alive[k] {
			delete(w.seen, k)
		}
	}
}

// run: stop 이 닫힐 때까지 hangPoll 마다 scan
func (w *hangWatch) run(stop <-chan struct{}, poll time.Duration) {
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			w.scan()
		}
	}
}

// watchGossh: 체크스크립트 프로세스(pid) 아래 gossh 감시 시작. 돌려준 함수가 감시를 끝내고 고루틴 종료까지 기다린다.
// hang_min=0 이면 아무것도 하지 않는다.
func watchGossh(code string, pid int) func() {
	lim := hangLimit()
	if lim <= 0 {
		return func() {}
	}
	w := newHangWatch(code, pid, lim)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		w.run(stop, hangPoll)
	}()
	return func() {
		close(stop)
		<-done
	}
}

// ---- 원격(os6_mgmt) gossh: 환경변수 표지로 찾기 ----
// 원격 gossh 의 목록 경로는 원격 mktemp 로 정해져 이쪽에서 모른다 → 원격 명령이 gossh 를 띄우기 전에 export 한 표지(이름=값)로
// 이 run 이 띄운 gossh 만 찾는다 (다른 run·사용자의 gossh 에는 표지가 없다). 아래 셸 조각은 작은따옴표·`!`·줄바꿈이 없어
// remoteBash 와 래퍼의 bash -c '…' 어디에 넣어도 된다 (csh/tcsh 로그인 셸 대비). kv 는 [A-Za-z0-9_.=] 만.

// gosshPidsSh: 표지 kv 를 가진 프로세스 중 comm 이 name 인 것의 pid 를 pl 에 모은다
func gosshPidsSh(kv, name string) string {
	return `pl=; for e in /proc/[0-9]*/environ; do grep -qzxF -- ` + kv + ` "$e" 2>/dev/null || continue; p=${e#/proc/}; p=${p%/environ}; ` +
		`[ "$(ps -o comm= -p "$p" 2>/dev/null)" = ` + dqs(name) + ` ] && pl="$pl $p"; done; `
}

// gosshCheckPidsSh: 주어진 pid 중 아직 표지 kv 를 가진 것만 pl 에 (목록 확인과 시그널 사이 pid 재사용 대비)
func gosshCheckPidsSh(kv string, pids []int) string {
	s := make([]string, len(pids))
	for i, p := range pids {
		s[i] = strconv.Itoa(p)
	}
	return `pl=; for p in ` + strings.Join(s, " ") + `; do grep -qzxF -- ` + kv + ` /proc/$p/environ 2>/dev/null && pl="$pl $p"; done; `
}

// gosshSigSh: pl 의 프로세스에 INT×2(0.3초 간격) 또는 TERM. 보낸 pid 를 ==SIG== 줄로 출력.
func gosshSigSh(term bool) string {
	if term {
		return `[ -z "$pl" ] || kill -TERM $pl 2>/dev/null; echo ==SIG==$pl`
	}
	return `[ -z "$pl" ] || { kill -INT $pl; sleep 0.3; kill -INT $pl; } 2>/dev/null; echo ==SIG==$pl`
}

// dqs: bash 큰따옴표 문자열 (", \, $, ` 이스케이프)
func dqs(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`")
	return `"` + r.Replace(s) + `"`
}

// commName: 실행 파일 경로의 comm (basename 앞 15바이트, 커널 TASK_COMM_LEN)
func commName(p string) string {
	b := filepath.Base(p)
	if len(b) > 15 {
		b = b[:15]
	}
	return b
}

// parseEtime: ps etime "[[dd-]hh:]mm:ss" → 초 (형식이 아니면 -1)
func parseEtime(s string) int64 {
	s = strings.TrimSpace(s)
	var days int64
	if i := strings.IndexByte(s, '-'); i >= 0 {
		d, err := strconv.ParseInt(s[:i], 10, 64)
		if err != nil {
			return -1
		}
		days, s = d, s[i+1:]
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return -1
	}
	var t int64
	for _, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n < 0 {
			return -1
		}
		t = t*60 + n
	}
	return days*86400 + t
}
