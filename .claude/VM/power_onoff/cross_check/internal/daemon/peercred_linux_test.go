//go:build linux

package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// SO_PEERCRED 로 읽은 uid 가 이 프로세스의 uid 와 같은지(같은 프로세스가 양쪽 끝).
func TestOSPeerUID(t *testing.T) {
	dir, err := os.MkdirTemp("", "ccp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ln, err := net.Listen("unix", filepath.Join(dir, "p.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := net.Dial("unix", filepath.Join(dir, "p.sock"))
		if err == nil {
			defer c.Close()
			buf := make([]byte, 1)
			c.Read(buf)
		}
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	uid, ok := osPeerUID(c)
	if !ok || uid != os.Getuid() {
		t.Fatalf("osPeerUID = %d, %v (기대 %d)", uid, ok, os.Getuid())
	}
	if _, ok := osPeerUID(nil); ok {
		t.Fatal("UnixConn 이 아니면 false")
	}
}

// 기본 PeerUID 로 띄운 데몬이 작업자 uid 를 감사 기록에 남기는지.
func TestDefaultPeerUIDRecorded(t *testing.T) {
	h := newHarness(t, nil)
	c := h.client()
	rh := h.run(c, "alice", "bob", []string{"vm01ev01"})
	if j := h.job(rh.ID); j.WorkerUID != os.Getuid() {
		t.Fatalf("WorkerUID = %d, 기대 %d", j.WorkerUID, os.Getuid())
	}
}
