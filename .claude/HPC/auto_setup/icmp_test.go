// icmp_test.go - icmp.go 단위 테스트: 체크섬/패킷 빌드/파싱(순수 함수) + 가짜 PacketConn 으로 Ping 동작
package main

import (
	"encoding/binary"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

func TestIcmpChecksumRFC1071(t *testing.T) {
	b := []byte{0x00, 0x01, 0xf2, 0x03, 0xf4, 0xf5, 0xf6, 0xf7}
	if got := icmpChecksum(b); got != 0x220d {
		t.Fatalf("checksum = %#x, want 0x220d", got)
	}
	// 홀수 길이
	if got := icmpChecksum([]byte{0xff}); got != 0x00ff {
		t.Fatalf("odd checksum = %#x", got)
	}
}

func TestBuildParseEcho(t *testing.T) {
	pkt := buildEchoRequest(0x1234, 7, []byte{1, 2, 3, 4})
	if pkt[0] != 8 || icmpChecksum(pkt) != 0 {
		t.Fatalf("bad request packet: %v", pkt)
	}
	// request 는 reply 로 파싱되지 않는다
	if _, _, _, ok := parseEchoReply(pkt); ok {
		t.Fatal("request parsed as reply")
	}
	rep := toReply(pkt)
	id, seq, pl, ok := parseEchoReply(rep)
	if !ok || id != 0x1234 || seq != 7 || len(pl) != 4 || pl[3] != 4 {
		t.Fatalf("parse reply: %v %v %v %v", id, seq, pl, ok)
	}
}

func TestParseEchoReplyIPHeaderAndCorrupt(t *testing.T) {
	rep := toReply(buildEchoRequest(1, 2, []byte{9, 9, 9, 9}))
	ip := make([]byte, 20)
	ip[0] = 0x45
	ip[9] = 1
	if _, seq, _, ok := parseEchoReply(append(ip, rep...)); !ok || seq != 2 {
		t.Fatal("IPv4 헤더가 붙은 reply 파싱 실패")
	}
	bad := append([]byte(nil), rep...)
	bad[8] ^= 0xff
	if _, _, _, ok := parseEchoReply(bad); ok {
		t.Fatal("체크섬 오류를 거부해야 함")
	}
	if _, _, _, ok := parseEchoReply([]byte{0, 0, 0}); ok {
		t.Fatal("짧은 패킷을 거부해야 함")
	}
}

func toReply(req []byte) []byte {
	r := append([]byte(nil), req...)
	r[0] = icmpEchoReply
	r[2], r[3] = 0, 0
	binary.BigEndian.PutUint16(r[2:], icmpChecksum(r))
	return r
}

// fakeConn: 지정한 IP 에만 echo reply 를 돌려주는 PacketConn
type fakeConn struct {
	alive    map[string]bool
	replies  chan fakeReply
	mu       sync.Mutex
	deadline time.Time
	closed   chan struct{}
}

type fakeReply struct {
	b    []byte
	from net.IP
}

func newFakeConn(alive ...string) *fakeConn {
	f := &fakeConn{alive: map[string]bool{}, replies: make(chan fakeReply, 1024), closed: make(chan struct{})}
	for _, a := range alive {
		f.alive[a] = true
	}
	return f
}

func (f *fakeConn) WriteTo(b []byte, a net.Addr) (int, error) {
	ip := a.(*net.IPAddr).IP
	if f.alive[ip.String()] {
		f.replies <- fakeReply{toReply(b), ip}
	}
	return len(b), nil
}

func (f *fakeConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		select {
		case r := <-f.replies:
			return copy(b, r.b), &net.IPAddr{IP: r.from}, nil
		case <-f.closed:
			return 0, nil, errors.New("closed")
		case <-time.After(5 * time.Millisecond): // 데드라인 폴링 (실제 소켓처럼 대기 중에도 적용)
			f.mu.Lock()
			d := f.deadline
			f.mu.Unlock()
			if !d.IsZero() && !time.Now().Before(d) {
				return 0, nil, errors.New("i/o timeout")
			}
		}
	}
}

func (f *fakeConn) SetReadDeadline(t time.Time) error {
	f.mu.Lock()
	f.deadline = t
	f.mu.Unlock()
	return nil
}
func (f *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error { return nil }
func (f *fakeConn) Close() error                       { close(f.closed); return nil }
func (f *fakeConn) LocalAddr() net.Addr                { return &net.IPAddr{} }

func TestIcmpPingFakeConn(t *testing.T) {
	setupDir(t)
	p := newIcmpPinger()
	p.Wait = 200 * time.Millisecond
	p.Batch = 1
	p.BatchGap = time.Millisecond
	p.conn = newFakeConn("10.0.0.1", "10.0.0.3")
	targets := []PingTarget{
		{Host: "a", IP: "10.0.0.1", Route: "local"},
		{Host: "b", IP: "10.0.0.2", Route: "local"},
		{Host: "c", IP: "10.0.0.3", Route: "local"},
		{Host: "noip", Route: "local"},
	}
	res := p.Ping(targets)
	want := map[string]PingResult{
		"a": {Up: true, Known: true}, "b": {Up: false, Known: true}, "c": {Up: true, Known: true}, "noip": {},
	}
	for h, w := range want {
		if res[h] != w {
			t.Errorf("%s = %+v, want %+v", h, res[h], w)
		}
	}
	// 두 번째 주기: round 가 달라도 정상 동작
	if res2 := p.Ping(targets[:1]); res2["a"] != (PingResult{Up: true, Known: true}) {
		t.Errorf("round2 a = %+v", res2["a"])
	}
}

func TestIcmpPingNoSocket(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 에서는 소켓이 열린다")
	}
	setupDir(t)
	res := newIcmpPinger().Ping([]PingTarget{{Host: "a", IP: "10.0.0.1"}})
	if res["a"].Known {
		t.Fatal("소켓 권한이 없으면 Known=false 여야 함")
	}
}
