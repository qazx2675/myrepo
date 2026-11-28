//go:build !linux

package remote

import "errors"

// 이 엔진은 Linux 관리서버용입니다. 다른 OS 에서는 패스워드를 물을 수 없습니다.
func ReadPassword(prompt string) (string, error) {
	return "", errors.New("지원하지 않는 OS")
}
