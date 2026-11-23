// config.go - 명령행 인자 파싱
package main

import (
	"fmt"
	"strconv"
)

// Config 는 실행 인자다. bash 래퍼(gpu_install.sh)가 값을 채워 넘긴다.
type Config struct {
	AutofsPath string // GPU 설치파일(.run)이 있는 디렉토리
	ListFile   string // 대상 목록 파일
	Parallel   int    // 동시 처리 대수
	MountUser  string // 마운트 판정용 계정 (/user/<MountUser>)
	Gossh      string // gossh 경로 (비우면 PATH 에서 찾음)
	LogDir     string // 호스트별 로그 디렉토리 (실행할 때 logs/<날짜시각> 로 정함)
}

const (
	defaultList     = "./list"
	defaultParallel = 10
)

// ParseArgs 는 `[autofs경로] [list파일] [-p N] [-mount-user 계정] [-gossh 경로]` 를 읽는다.
// 위치 인자는 순서대로 autofs경로, list파일이고 옵션은 어느 위치에나 올 수 있다.
func ParseArgs(args []string) (*Config, error) {
	cfg := &Config{ListFile: defaultList, Parallel: defaultParallel}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-p", "-mount-user", "-gossh":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%s 옵션에 값이 필요합니다", a)
			}
			i++
			v := args[i]
			switch a {
			case "-p":
				n, err := strconv.Atoi(v)
				if err != nil || n < 1 {
					return nil, fmt.Errorf("-p 는 1 이상의 정수여야 합니다: %q", v)
				}
				cfg.Parallel = n
			case "-mount-user":
				cfg.MountUser = v
			case "-gossh":
				cfg.Gossh = v
			}
		default:
			if len(a) > 1 && a[0] == '-' {
				return nil, fmt.Errorf("알 수 없는 옵션: %s", a)
			}
			pos = append(pos, a)
		}
	}
	if len(pos) > 2 {
		return nil, fmt.Errorf("위치 인자가 너무 많습니다: %v", pos)
	}
	if len(pos) >= 1 && pos[0] != "" {
		cfg.AutofsPath = pos[0]
	}
	if len(pos) == 2 && pos[1] != "" {
		cfg.ListFile = pos[1]
	}
	if cfg.AutofsPath == "" {
		return nil, fmt.Errorf("autofs 경로가 필요합니다 (인자 또는 gpu_install.sh 의 AUTOFS_PATH)")
	}
	return cfg, nil
}
