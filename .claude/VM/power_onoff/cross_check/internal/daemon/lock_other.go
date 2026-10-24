//go:build !linux && !darwin

package daemon

import (
	"errors"
	"runtime"
)

// AcquireLock 은 flock 이 없는 OS 에서는 지원하지 않는다.
func AcquireLock(path string) (release func(), err error) {
	return nil, errors.New("AcquireLock: unsupported OS (" + runtime.GOOS + ")")
}
