package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func needRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("raw ICMP 는 root 필요")
	}
}

func TestChecksumBuildParse(t *testing.T) {
	if got := icmpChecksum([]byte{0x00, 0x01, 0xf2, 0x03, 0xf4, 0xf5, 0xf6, 0xf7}); got != 0x220d {
		t.Fatalf("checksum = %#x", got)
	}
	pkt := buildEchoRequest(0x1234, 7, []byte{1, 2, 3, 4})
	if pkt[0] != 8 || icmpChecksum(pkt) != 0 {
		t.Fatal("bad request")
	}
	if _, _, _, ok := parseEchoReply(pkt); ok {
		t.Fatal("request parsed as reply")
	}
	rep := append([]byte(nil), pkt...)
	rep[0] = 0
	rep[2], rep[3] = 0, 0
	c := icmpChecksum(rep)
	rep[2], rep[3] = byte(c>>8), byte(c)
	id, seq, _, ok := parseEchoReply(rep)
	if !ok || id != 0x1234 || seq != 7 {
		t.Fatal("reply parse")
	}
}

func TestPingLoopbackAndDown(t *testing.T) {
	needRoot(t)
	res, err := PingAll([]string{"127.0.0.1", "127.0.0.2", "192.0.2.1", "no-such-host.invalid"}, 800*time.Millisecond, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !res["127.0.0.1"] || !res["127.0.0.2"] {
		t.Fatalf("loopback no reply: %v", res)
	}
	if res["192.0.2.1"] || res["no-such-host.invalid"] {
		t.Fatalf("should be down: %v", res)
	}
}

func TestPingMappingMany(t *testing.T) {
	needRoot(t)
	var hosts []string
	for i := 1; i <= 200; i++ {
		hosts = append(hosts, fmt.Sprintf("127.0.%d.%d", i/250, i%250+1))
	}
	hosts = append(hosts, "127.0.0.1") // 중복 IP
	res, err := PingAll(hosts, time.Second, 2000)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hosts {
		if !res[h] {
			t.Fatalf("%s no reply", h)
		}
	}
}

func TestPingRateLimit(t *testing.T) {
	needRoot(t)
	var hosts []string
	for i := 1; i <= 50; i++ {
		hosts = append(hosts, fmt.Sprintf("127.0.0.%d", i))
	}
	st := time.Now()
	if _, err := PingAll(hosts, 100*time.Millisecond, 100); err != nil {
		t.Fatal(err)
	}
	// 응답이 전부 오면 수신 고루틴이 일찍 끝나지만 송신은 49*10ms 걸린다
	if d := time.Since(st); d < 450*time.Millisecond {
		t.Fatalf("pps 제한 미준수: %v", d)
	}
}
