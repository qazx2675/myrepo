package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// targets 는 대상 목록(list.txt)과 과거 비밀번호 후보(old_password.txt)를 읽습니다.
//
// list.txt: 줄바꿈으로 IP 만 나열 (계정은 root 고정).
// old_password.txt: 후보 비밀번호를 최신(맨 위)부터 한 줄에 하나씩.

// loadTargets 는 대상 IP 목록을 읽어들입니다.
//
// 빈 줄과 "#" 로 시작하는 줄은 건너뜁니다. 유효한 IPv4 만 받습니다.
func loadTargets(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if net.ParseIP(line) == nil || !strings.Contains(line, ".") {
			return nil, fmt.Errorf("%s:%d: 유효한 IPv4 가 아닙니다: %q", path, lineNo, line)
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: 대상이 하나도 없습니다", path)
	}
	return out, nil
}

// loadPasswords 는 과거 비밀번호 후보 목록을 파일 순서(맨 위=최신) 그대로 읽습니다.
//
// 비밀번호 원문을 훼손하지 않도록 좌우 공백을 다듬지 않으며, 윈도우 개행(\r)만
// 제거하고 완전히 빈 줄만 건너뜁니다. 이 파일은 읽기 전용으로만 다룹니다.
func loadPasswords(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: 후보 비밀번호가 하나도 없습니다", path)
	}
	return out, nil
}
