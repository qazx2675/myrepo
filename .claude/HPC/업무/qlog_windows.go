//go:build windows

// hpcbot — 로그 파일 잠금 (윈도우: 개발·테스트용, 잠금 없음)
package main

// lockFile 은 윈도우에서는 아무것도 하지 않는다 (운영 대상은 RHEL8).
func lockFile(path string) (func(), error) { return func() {}, nil }
