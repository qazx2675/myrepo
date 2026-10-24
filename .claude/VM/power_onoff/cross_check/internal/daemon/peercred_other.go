//go:build !linux

package daemon

import "net"

// osPeerUID 는 Linux 가 아니면 상대 uid 를 알 수 없다(-1, false). 감사 기록에 -1 로 남는다.
func osPeerUID(net.Conn) (int, bool) { return -1, false }
