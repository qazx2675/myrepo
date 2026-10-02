// icmp.go - raw 소켓 ICMP echo 송수신(외부 ping 프로세스 없음) + Route 별 분기 Pinger (local / os6 세션)
package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

const (
	icmpEchoRequest = 8
	icmpEchoReply   = 0
	icmpPayloadLen  = 8 // round(4) + 예비(4)
)

// icmpChecksum: RFC 1071 인터넷 체크섬
func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// buildEchoRequest: ICMP echo request 패킷 (type=8, code=0, 체크섬 채움)
func buildEchoRequest(id, seq uint16, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	b[0] = icmpEchoRequest
	binary.BigEndian.PutUint16(b[4:], id)
	binary.BigEndian.PutUint16(b[6:], seq)
	copy(b[8:], payload)
	binary.BigEndian.PutUint16(b[2:], icmpChecksum(b))
	return b
}

// parseEchoReply: echo reply 파싱. IPv4 헤더가 붙어 있으면 제거. 체크섬이 틀리거나 reply 가 아니면 ok=false
func parseEchoReply(b []byte) (id, seq uint16, payload []byte, ok bool) {
	if len(b) >= 20 && b[0]>>4 == 4 && b[9] == 1 { // IPv4 + ICMP
		hl := int(b[0]&0x0f) * 4
		if hl < 20 || len(b) < hl {
			return 0, 0, nil, false
		}
		b = b[hl:]
	}
	if len(b) < 8 || b[0] != icmpEchoReply || b[1] != 0 {
		return 0, 0, nil, false
	}
	if icmpChecksum(b) != 0 {
		return 0, 0, nil, false
	}
	return binary.BigEndian.Uint16(b[4:]), binary.BigEndian.Uint16(b[6:]), b[8:], true
}

// icmpPinger: raw 소켓 1개로 한 번에 전 대상에 1패킷씩 송신 후 응답 수집
type icmpPinger struct {
	Wait     time.Duration // 마지막 송신 후 응답 대기 상한
	Batch    int           // 이 개수만큼 송신할 때마다 BatchGap 휴식
	BatchGap time.Duration
	id       uint16

	mu      sync.Mutex
	conn    net.PacketConn
	round   uint32
	lastErr string
}

func newIcmpPinger() *icmpPinger {
	return &icmpPinger{Wait: 2 * time.Second, Batch: 100, BatchGap: 10 * time.Millisecond, id: uint16(os.Getpid() & 0xffff)}
}

// open: raw 소켓을 연다 (이미 열려 있으면 그대로)
func (p *icmpPinger) open() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != nil {
		return nil
	}
	c, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("ICMP raw 소켓을 열 수 없습니다 (root 권한 필요): %v", err)
	}
	p.conn = c
	return nil
}

// Ping: Pinger 계약. 소켓 오류면 전부 Known=false (상태 변화 시에만 로그)
func (p *icmpPinger) Ping(targets []PingTarget) map[string]PingResult {
	res, err := p.ping(targets)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if msg != p.lastErr {
		p.lastErr = msg
		if msg != "" {
			logf("[X] icmp: %s", msg)
		}
	}
	return res
}

// ping: IP 가 없거나 잘못된 대상은 Known=false, 나머지는 응답 여부로 Up 결정
func (p *icmpPinger) ping(targets []PingTarget) (map[string]PingResult, error) {
	out := make(map[string]PingResult, len(targets))
	addrs := make([]net.IP, len(targets))
	n := 0
	for i, t := range targets {
		ip := net.ParseIP(t.IP).To4()
		if ip == nil {
			out[t.Host] = PingResult{}
			continue
		}
		addrs[i] = ip
		n++
	}
	if n == 0 {
		return out, nil
	}
	if err := p.open(); err != nil {
		for _, t := range targets {
			out[t.Host] = PingResult{}
		}
		return out, err
	}
	p.mu.Lock()
	conn := p.conn
	p.round++
	round := p.round
	p.mu.Unlock()

	payload := make([]byte, icmpPayloadLen)
	binary.BigEndian.PutUint32(payload, round)

	got := make([]bool, len(targets))
	var gmu sync.Mutex
	remain := n
	done := make(chan struct{})
	// 수신 고루틴 1개: 송신 중에도 소켓 버퍼가 넘치지 않도록 먼저 시작
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			m, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			rid, seq, pl, ok := parseEchoReply(buf[:m])
			if !ok || rid != p.id || len(pl) < 4 || binary.BigEndian.Uint32(pl) != round {
				continue
			}
			idx := int(seq)
			fip, _ := from.(*net.IPAddr)
			for ; idx < len(targets); idx += 65536 { // 65536 대 초과 시 seq 중복 → 출발지 IP 로 구분
				if addrs[idx] != nil && fip != nil && fip.IP.Equal(addrs[idx]) {
					break
				}
			}
			if idx >= len(targets) {
				continue
			}
			gmu.Lock()
			if !got[idx] {
				got[idx] = true
				remain--
			}
			finished := remain == 0
			gmu.Unlock()
			if finished {
				return
			}
		}
	}()

	sent := 0
	for i := range targets {
		if addrs[i] == nil {
			continue
		}
		pkt := buildEchoRequest(p.id, uint16(i&0xffff), payload)
		_, _ = conn.WriteTo(pkt, &net.IPAddr{IP: addrs[i]}) // 송신 실패 = 무응답으로 취급
		sent++
		if p.Batch > 0 && sent%p.Batch == 0 {
			time.Sleep(p.BatchGap)
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(p.Wait))
	<-done
	_ = conn.SetReadDeadline(time.Time{})

	gmu.Lock()
	for i, t := range targets {
		if addrs[i] != nil {
			out[t.Host] = PingResult{Up: got[i], Known: true}
		}
	}
	gmu.Unlock()
	return out, nil
}

// realPinger: Route=="os6" 는 os6 세션의 마지막 상태, 그 외(local)는 직접 ICMP
type realPinger struct {
	local *icmpPinger
	os6   *os6Sessions
}

// NewRealPinger: 데몬용 실제 Pinger (단계 D 가 newDefaultDaemon 에서 사용)
func NewRealPinger() Pinger {
	return &realPinger{local: newIcmpPinger(), os6: newOS6Sessions()}
}

func (r *realPinger) Ping(targets []PingTarget) map[string]PingResult {
	var loc, o6 []PingTarget
	for _, t := range targets {
		if t.Route == "os6" {
			o6 = append(o6, t)
		} else {
			loc = append(loc, t)
		}
	}
	out := make(map[string]PingResult, len(targets))
	if len(loc) > 0 {
		for h, v := range r.local.Ping(loc) {
			out[h] = v
		}
	}
	// os6 대상이 없어도 호출: 집합이 비면 세션을 종료한다
	for h, v := range r.os6.Ping(o6) {
		out[h] = v
	}
	return out
}
