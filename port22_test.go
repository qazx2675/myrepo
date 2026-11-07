package main

import (
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func withPort(t *testing.T, p int) {
	old := sshPort
	sshPort = p
	t.Cleanup(func() { sshPort = old })
}

func TestPortOpen(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	withPort(t, l.Addr().(*net.TCPAddr).Port)
	if r := CheckPort22([]string{"127.0.0.1"}, 10, time.Second); r["127.0.0.1"] != PortOpen {
		t.Fatalf("got %v", r)
	}
}

func TestPortRefused(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	withPort(t, p)
	if r := CheckPort22([]string{"127.0.0.1"}, 10, time.Second); r["127.0.0.1"] != PortRefused {
		t.Fatalf("got %v", r)
	}
}

func TestPortTimeout(t *testing.T) {
	withPort(t, 22)
	if r := CheckPort22([]string{"192.0.2.1"}, 10, 300*time.Millisecond); r["192.0.2.1"] != PortTimeout {
		t.Fatalf("got %v", r)
	}
}

func TestPortParallelLimit(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	var cur, max int32
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			n := atomic.AddInt32(&cur, 1)
			for {
				m := atomic.LoadInt32(&max)
				if n <= m || atomic.CompareAndSwapInt32(&max, m, n) {
					break
				}
			}
			go func() {
				time.Sleep(50 * time.Millisecond)
				c.Close()
				atomic.AddInt32(&cur, -1)
			}()
		}
	}()
	withPort(t, l.Addr().(*net.TCPAddr).Port)
	var hosts []string
	for i := 1; i <= 30; i++ {
		hosts = append(hosts, fmt.Sprintf("127.0.0.%d", i))
	}
	r := CheckPort22(hosts, 5, time.Second)
	if len(r) != 30 {
		t.Fatalf("len %d", len(r))
	}
	if m := atomic.LoadInt32(&max); m > 5 || m < 1 {
		t.Fatalf("동시 최대 %d (한도 5)", m)
	}
}
