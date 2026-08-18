// Package config는 vcenter.txt / affinity 파일 / target(단일 대상) 파일을 읽는다.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadLines는 '#' 주석과 빈 줄을 무시하고 트림된 라인 목록을 반환한다.
// vcenter.txt, targets 파일 등 "한 줄에 하나" 포맷 전부에 재사용한다.
func LoadLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, scanner.Err()
}

// LoadAffinityFile은 "sched.vcpu0.affinity = 0" 형식의 파일을 읽어
// key->value(기대값) 맵으로 변환한다. ev02/ev03 그룹의 기대값 소스로 쓰인다.
func LoadAffinityFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := map[string]string{}
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("%s:%d 형식 오류 (key = value 형태가 아님): %q", path, lineNo, line)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		result[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
