// probe_test.go - probe.go 단위 테스트: probeLoop 출력(전체→변화만), 인자 검사, quoting, os6Sessions(가짜 ssh 명령)
package main

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type scriptPinger struct {
	mu    sync.Mutex
	round int
	seq   []map[string]PingResult
}

func (s *scriptPinger) Ping(ts []PingTarget) map[string]PingResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.seq[len(s.seq)-1]
	if s.round < len(s.seq) {
		r = s.seq[s.round]
	}
	s.round++
	return r
}

func TestProbeLoopFullThenChangesThenExit(t *testing.T) {
	up, down := PingResult{Up: true, Known: true}, PingResult{Known: true}
	p := &scriptPinger{seq: []map[string]PingResult{
		{"a": up, "b": down},
		{"a": up, "b": down}, // 변화 없음 → 출력 없음
		{"a": down, "b": down, "c": {}},
	}}
	// 빈 줄로 목록을 끝내고 stdin 은 연 채로 둔다 → 3 라운드 후 stdin 닫아 종료
	pr, pw := io.Pipe()
	var out bytes.Buffer
	done := make(chan int)
	go func() { done <- probeLoop(pr, &out, 20*time.Millisecond, p) }()
	_, _ = pw.Write([]byte("a 10.0.0.1\nb 10.0.0.2\nc 10.0.0.3\n\n"))
	time.Sleep(200 * time.Millisecond)
	pw.Close()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("rc=%d", rc)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stdin EOF 후 종료하지 않음")
	}
	got := strings.Fields(out.String())
	want := []string{"a", "up", "b", "down", "a", "down"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("출력 %q, want %v", out.String(), want)
	}
}

func TestProbeLoopEOFListOneShot(t *testing.T) {
	p := &scriptPinger{seq: []map[string]PingResult{{"a": {Up: true, Known: true}}}}
	var out bytes.Buffer
	if rc := probeLoop(strings.NewReader("a 10.0.0.1\n"), &out, time.Hour, p); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if out.String() != "a up\n" {
		t.Fatalf("출력 %q", out.String())
	}
}

func TestParseProbeLines(t *testing.T) {
	old := lookupHost
	defer func() { lookupHost = old }()
	lookupHost = func(h string) ([]string, error) {
		if h == "named" {
			return []string{"fe80::1", "10.1.1.1"}, nil
		}
		return nil, errNoHost
	}
	ts := parseProbeLines([]string{"named", "x 10.0.0.9", "10.0.0.5", "missing", "x 10.0.0.9", "10.0.0.5 10.0.0.6 10.0.0.7"})
	got := []string{}
	for _, t := range ts {
		got = append(got, t.Host+"="+t.IP)
	}
	want := "named=10.1.1.1 x=10.0.0.9 10.0.0.5=10.0.0.5 10.0.0.6=10.0.0.6 10.0.0.7=10.0.0.7"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v", got)
	}
}

type noHostErr struct{}

func (noHostErr) Error() string { return "no such host" }

var errNoHost = noHostErr{}

func TestRunProbeBadArgs(t *testing.T) {
	for _, a := range [][]string{{"-x"}, {"extra"}, {"-i", "0s"}, {"-i", "abc"}} {
		if rc := runProbe(a); rc != 1 {
			t.Errorf("runProbe(%v) = %d, want 1", a, rc)
		}
	}
}

func TestShQuote(t *testing.T) {
	if got := shQuote("a b'c"); got != `'a b'\''c'` {
		t.Fatalf("got %s", got)
	}
	out, err := exec.Command("sh", "-c", "printf %s "+shQuote("x y';z$HOME")).Output()
	if err != nil || string(out) != "x y';z$HOME" {
		t.Fatalf("sh 왕복 실패: %q %v", out, err)
	}
}

// ---- os6Sessions ----

func withFakeSSH(t *testing.T, script string) *int32 {
	t.Helper()
	setupDir(t)
	var n int32
	old := sshCommand
	sshCommand = func(name string, arg ...string) *exec.Cmd {
		atomic.AddInt32(&n, 1)
		return exec.Command("sh", "-c", script, "fakessh")
	}
	t.Cleanup(func() { sshCommand = old })
	return &n
}

// 목록을 읽어 h2 는 down, 나머지는 up 을 내고 세션을 유지
const fakeProbeScript = `while read l; do [ -z "$l" ] && break; set -- $l; if [ "$1" = h2 ]; then echo "$1 down"; else echo "$1 up"; fi; done; exec sleep 30`

func newTestSessions() *os6Sessions {
	return &os6Sessions{mgmt: "mgmt.test", dir: "/opt/as/", interval: 10 * time.Second, state: map[string]bool{}}
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("시간 초과: %s", what)
}

func TestOS6SessionsParseAndRestartOnSetChange(t *testing.T) {
	n := withFakeSSH(t, fakeProbeScript)
	s := newTestSessions()
	defer s.Close()
	ts := []PingTarget{{Host: "h1", IP: "10.0.0.1", Route: "os6"}, {Host: "h2", IP: "10.0.0.2", Route: "os6"}}
	if r := s.Ping(ts); r["h1"].Known || r["h2"].Known {
		t.Fatal("세션 시작 직후엔 Known=false")
	}
	waitFor(t, "상태 수신", func() bool { return s.Ping(ts)["h2"].Known })
	r := s.Ping(ts)
	if r["h1"] != (PingResult{Up: true, Known: true}) || r["h2"] != (PingResult{Up: false, Known: true}) {
		t.Fatalf("파싱 결과 %+v", r)
	}
	if atomic.LoadInt32(n) != 1 {
		t.Fatalf("같은 목록인데 재시작됨: %d", atomic.LoadInt32(n))
	}
	// 목록 변경 → 재시작
	ts2 := append(ts, PingTarget{Host: "h3", IP: "10.0.0.3", Route: "os6"})
	s.Ping(ts2)
	if atomic.LoadInt32(n) != 2 {
		t.Fatalf("목록 변경 시 재시작 안 됨: %d", atomic.LoadInt32(n))
	}
	waitFor(t, "재시작 후 상태", func() bool { return s.Ping(ts2)["h3"].Known })
}

func TestOS6SessionsDeadIsUnknownAndRestarts(t *testing.T) {
	n := withFakeSSH(t, `read l; echo "h1 up"`) // 한 줄 출력 후 바로 종료
	s := newTestSessions()
	defer s.Close()
	ts := []PingTarget{{Host: "h1", IP: "10.0.0.1", Route: "os6"}}
	s.Ping(ts)
	waitFor(t, "세션 사망 감지", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return !s.alive
	})
	if r := s.Ping(ts); r["h1"].Known {
		t.Fatal("세션이 죽은 동안은 Known=false (down 으로 보면 안 됨)")
	}
	if atomic.LoadInt32(n) != 2 {
		t.Fatalf("죽은 세션이 재시작되지 않음: %d", atomic.LoadInt32(n))
	}
}

func TestOS6SessionsEmptyMgmtAndArgs(t *testing.T) {
	setupDir(t)
	s := newTestSessions()
	s.mgmt = ""
	if r := s.Ping([]PingTarget{{Host: "h1", IP: "10.0.0.1"}}); r["h1"].Known {
		t.Fatal("os6_mgmt 가 비면 Known=false")
	}
	var got []string
	old := sshCommand
	sshCommand = func(name string, arg ...string) *exec.Cmd {
		got = append([]string{name}, arg...)
		return exec.Command("sh", "-c", "exec sleep 30")
	}
	defer func() { sshCommand = old }()
	s.mgmt = "mgmt.test"
	s.Ping([]PingTarget{{Host: "h1", IP: "10.0.0.1"}})
	s.Close()
	want := "ssh -o BatchMode=yes -o ServerAliveInterval=30 mgmt.test '/opt/as/auto_setup' probe -i '10s'"
	if strings.Join(got, " ") != want {
		t.Fatalf("ssh 인자 %q", strings.Join(got, " "))
	}
}
