// probe.go - 'probe' 하위명령(os6_mgmt 쪽 ping 감시) + 데몬 쪽 os6 장기 ssh 세션 관리(os6Sessions)
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

const probeUsage = "사용법: auto_setup probe [-i 10s]   (stdin: 줄마다 'host' 또는 'host ip', 빈 줄 또는 EOF 로 목록 끝)\n"

// lookupHost: 테스트에서 교체 가능한 이름 해석
var lookupHost = net.LookupHost

// runProbe: stdin 호스트 목록을 icmp 로 감시. 첫 결과는 전체, 이후엔 변화만 "host up|down" 출력
func runProbe(args []string) int {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	interval := fs.Duration("i", pingInterval, "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || *interval <= 0 {
		fmt.Fprint(os.Stderr, probeUsage)
		return 1
	}
	p := newIcmpPinger()
	if err := p.open(); err != nil {
		fmt.Fprintln(os.Stderr, "[X]", err)
		return 1
	}
	return probeLoop(os.Stdin, os.Stdout, *interval, p)
}

// readProbeList: 빈 줄 또는 EOF 까지 목록을 읽는다. eof=true 면 stdin 이 이미 닫힌 것.
// 이후 stdin 은 EOF 까지 버리고 closed 를 닫는다(원격 ssh 가 끊기면 EOF → 종료).
func readProbeList(in io.Reader) (lines []string, eof bool, closed <-chan struct{}) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for {
		if !sc.Scan() {
			c := make(chan struct{})
			close(c)
			return lines, true, c
		}
		l := strings.TrimSpace(sc.Text())
		if l == "" {
			break
		}
		lines = append(lines, l)
	}
	c := make(chan struct{})
	go func() {
		for sc.Scan() {
		}
		close(c)
	}()
	return lines, false, c
}

// parseProbeLines: 'host' 또는 'host ip' → PingTarget (이름이면 IPv4 해석, 실패 시 건너뜀)
func parseProbeLines(lines []string) []PingTarget {
	var ts []PingTarget
	seen := map[string]bool{}
	var recs [][]string
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) > 2 { // 한 줄에 3개 이상이면 공백 구분 호스트 목록으로 취급
			for _, h := range f {
				recs = append(recs, []string{h})
			}
		} else if len(f) > 0 {
			recs = append(recs, f)
		}
	}
	for _, f := range recs {
		if seen[f[0]] {
			continue
		}
		t := PingTarget{Host: f[0], Route: "local"}
		if len(f) == 2 {
			t.IP = f[1]
		} else if ip := net.ParseIP(f[0]); ip != nil {
			t.IP = f[0]
		} else if addrs, err := lookupHost(f[0]); err == nil {
			for _, a := range addrs {
				if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
					t.IP = a
					break
				}
			}
		}
		if net.ParseIP(t.IP).To4() == nil {
			fmt.Fprintf(os.Stderr, "probe: %s 주소 해석 실패, 건너뜀\n", f[0])
			continue
		}
		seen[f[0]] = true
		ts = append(ts, t)
	}
	return ts
}

// probeLoop: 목록이 EOF 로 끝났으면 첫 결과만 내고 종료, 빈 줄로 끝났으면 stdin EOF 까지 주기 반복
func probeLoop(in io.Reader, out io.Writer, interval time.Duration, p Pinger) int {
	lines, eof, closed := readProbeList(in)
	targets := parseProbeLines(lines)
	w := bufio.NewWriter(out)
	last := map[string]bool{}
	first := true
	for {
		start := time.Now()
		res := p.Ping(targets)
		for _, t := range targets {
			r, ok := res[t.Host]
			if !ok || !r.Known {
				continue
			}
			if prev, had := last[t.Host]; first || !had || prev != r.Up {
				st := "down"
				if r.Up {
					st = "up"
				}
				fmt.Fprintf(w, "%s %s\n", t.Host, st)
				last[t.Host] = r.Up
			}
		}
		first = false
		if w.Flush() != nil || eof {
			return 0
		}
		select {
		case <-closed:
			return 0
		case <-time.After(interval - time.Since(start)):
		}
	}
}

// ---- 데몬 쪽: os6_mgmt 장기 ssh 세션 ----

// sshCommand: 테스트에서 가짜 명령으로 교체
var sshCommand = exec.Command

// shQuote: POSIX sh 작은따옴표 quoting
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// blockReader: stop 이 닫힐 때까지 Read 를 막다가 EOF (stdin 을 열어 둔 채 유지)
type blockReader struct{ stop <-chan struct{} }

func (b blockReader) Read(p []byte) (int, error) {
	<-b.stop
	return 0, io.EOF
}

type os6Sessions struct {
	mgmt     string
	dir      string
	interval time.Duration

	mu    sync.Mutex
	key   string // 현재 세션의 목록 키
	gen   int
	alive bool
	cmd   *exec.Cmd
	stop  chan struct{}
	state map[string]bool // host → up
	quiet bool            // 출력 없이 끝난 세션 뒤(접속 불가 등): 10초마다 재시작 로그 생략
}

func newOS6Sessions() *os6Sessions {
	return &os6Sessions{mgmt: os6_mgmt, dir: os6_autosetup, interval: pingInterval, state: map[string]bool{}}
}

// Ping: 목록이 바뀌었거나 세션이 죽었으면 재시작하고, 마지막으로 알려진 상태를 반환한다.
// 세션이 죽은 동안/아직 응답이 없는 호스트는 Known=false (down 으로 보지 않음).
func (s *os6Sessions) Ping(targets []PingTarget) map[string]PingResult {
	var ls []string
	for _, t := range targets {
		if t.Host == "" || strings.ContainsAny(t.Host+t.IP, " \t\r\n") {
			continue
		}
		l := t.Host
		if t.IP != "" {
			l += " " + t.IP
		}
		ls = append(ls, l)
	}
	sort.Strings(ls)
	key := strings.Join(ls, "\n")

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mgmt == "" || len(ls) == 0 {
		s.killLocked()
		s.key = ""
	} else if !s.alive || key != s.key {
		s.killLocked()
		s.startLocked(key)
	}
	out := make(map[string]PingResult, len(targets))
	for _, t := range targets {
		if up, ok := s.state[t.Host]; ok && s.alive {
			out[t.Host] = PingResult{Up: up, Known: true}
		} else {
			out[t.Host] = PingResult{}
		}
	}
	return out
}

// Close: 세션 종료
func (s *os6Sessions) Close() {
	s.mu.Lock()
	s.killLocked()
	s.mu.Unlock()
}

func (s *os6Sessions) killLocked() {
	if s.cmd == nil {
		return
	}
	close(s.stop)
	_ = s.cmd.Process.Kill()
	s.cmd = nil
	s.alive = false
	s.gen++
	s.state = map[string]bool{}
}

func (s *os6Sessions) startLocked(key string) {
	remote := shQuote(strings.TrimRight(s.dir, "/")+"/auto_setup") + " probe -i " + shQuote(s.interval.String())
	stop := make(chan struct{})
	cmd := sshCommand("ssh", "-o", "BatchMode=yes", "-o", "ServerAliveInterval=30", s.mgmt, remote)
	cmd.Stdin = io.MultiReader(strings.NewReader(key+"\n\n"), blockReader{stop})
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	s.key = key
	if err != nil {
		if !s.quiet {
			logf("[X] os6 probe 세션 시작 실패: %v", err)
		}
		s.quiet = true
		return
	}
	s.gen++
	gen := s.gen
	s.cmd, s.stop, s.alive = cmd, stop, true
	if !s.quiet {
		logf("os6 probe 세션 시작 (%d대)", strings.Count(key, "\n")+1)
	}
	go s.reader(gen, cmd, out, stop)
}

// reader: 세션 stdout 의 "host up|down" 을 상태에 반영, 끝나면 세션 사망 처리
func (s *os6Sessions) reader(gen int, cmd *exec.Cmd, out io.Reader, stop chan struct{}) {
	sc := bufio.NewScanner(out)
	got := false
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 || (f[1] != "up" && f[1] != "down") {
			continue
		}
		got = true
		s.mu.Lock()
		if s.gen == gen {
			s.state[f[0]] = f[1] == "up"
			s.quiet = false
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	if s.gen == gen {
		s.alive = false
		s.state = map[string]bool{}
		s.cmd = nil
		s.gen++
		close(stop)
		if !s.quiet {
			logf("os6 probe 세션 종료 (다음 주기에 재시작)")
		}
		if !got {
			s.quiet = true
		}
	}
	s.mu.Unlock()
	_ = cmd.Wait()
}
