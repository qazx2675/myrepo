//go:build linux

package daemon

import (
	"net"
	"syscall"
)

// osPeerUID 는 Unix 소켓 상대 프로세스의 uid 를 SO_PEERCRED 로 읽는다(감사 기록용).
func osPeerUID(c net.Conn) (int, bool) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return -1, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return -1, false
	}
	var cred *syscall.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil || cerr != nil || cred == nil {
		return -1, false
	}
	return int(cred.Uid), true
}
