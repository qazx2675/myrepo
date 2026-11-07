// icmp.go - raw 소켓 ICMP echo (외부 ping 프로세스 없음). auto_setup 의 icmp.go 설계를 따른다.
// 소켓 1개, 송신 속도 제한(pps), 수신 고루틴 1개. echo id + seq(대상 번호) + 출발지 IP 로 응답을 매핑한다.
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
	icmpPayloadLen  = 8
	resolveParallel = 50 // 호스트 이름 -> IP 변환 동시 수
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
	if len(b) >= 20 && b[0]>>4 == 4 && b[9] == 1 {
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

// resolveHosts: 호스트 이름을 IPv4 로 바꾼다(동시 수 제한). 변환 실패는 nil.
func resolveHosts(hosts []string) []net.IP {
	out := make([]net.IP, len(hosts))
	sem := make(chan struct{}, resolveParallel)
	var wg sync.WaitGroup
	for i, h := range hosts {
		if ip := net.ParseIP(h).To4(); ip != nil {
			out[i] = ip
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, h string) {
			defer wg.Done()
			defer func() { <-sem }()
			ips, err := net.LookupIP(h)
			if err != nil {
				return
			}
			for _, ip := range ips {
				if v4 := ip.To4(); v4 != nil {
					out[i] = v4
					return
				}
			}
		}(i, h)
	}
	wg.Wait()
	return out
}

// PingAll: hosts 전부에 echo 1개씩 보내고 응답한 호스트를 true 로 돌려준다.
// 송신은 초당 pps 개로 균등 분산, 마지막 송신 후 timeout 동안 응답을 기다린다.
// 이름 변환 실패 호스트는 false. raw 소켓을 못 열면(root 아님) error.
func PingAll(hosts []string, timeout time.Duration, pps int) (map[string]bool, error) {
	res := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		res[h] = false
	}
	if len(hosts) == 0 {
		return res, nil
	}
	if pps <= 0 {
		pps = 500
	}
	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return res, fmt.Errorf("ICMP raw 소켓을 열 수 없습니다 (root 권한 필요): %v", err)
	}
	defer conn.Close()

	ips := resolveHosts(hosts)
	// 같은 IP 는 한 번만 보낸다: targets[idx] = IP, owners[idx] = 그 IP 를 가진 호스트들
	var targets []net.IP
	var owners [][]string
	idxOf := map[string]int{}
	for i, ip := range ips {
		if ip == nil {
			continue
		}
		k := ip.String()
		idx, ok := idxOf[k]
		if !ok {
			idx = len(targets)
			idxOf[k] = idx
			targets = append(targets, ip)
			owners = append(owners, nil)
		}
		owners[idx] = append(owners[idx], hosts[i])
	}
	if len(targets) == 0 {
		return res, nil
	}

	id := uint16(os.Getpid() & 0xffff)
	got := make([]bool, len(targets))
	var gmu sync.Mutex
	remain := len(targets)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		for {
			m, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			rid, seq, _, ok := parseEchoReply(buf[:m])
			fip, _ := from.(*net.IPAddr)
			if !ok || rid != id || fip == nil {
				continue
			}
			idx := int(seq)
			for ; idx < len(targets); idx += 65536 { // 65536 대 초과 시 seq 중복 -> 출발지 IP 로 구분
				if targets[idx].Equal(fip.IP) {
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

	payload := make([]byte, icmpPayloadLen)
	interval := time.Second / time.Duration(pps)
	start := time.Now()
	for i, ip := range targets {
		if d := time.Until(start.Add(time.Duration(i) * interval)); d > 0 {
			time.Sleep(d)
		}
		_, _ = conn.WriteTo(buildEchoRequest(id, uint16(i&0xffff), payload), &net.IPAddr{IP: ip}) // 송신 실패 = 무응답
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	<-done

	gmu.Lock()
	for i, ok := range got {
		if ok {
			for _, h := range owners[i] {
				res[h] = true
			}
		}
	}
	gmu.Unlock()
	return res, nil
}
