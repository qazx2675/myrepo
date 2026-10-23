package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var vmNameRe = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)ev([0-9]{2})$`)

// ValidateUser 는 user 가 설정의 users 에 있는지 확인한다.
func ValidateUser(cfg *Config, user string) error {
	for _, u := range cfg.Users {
		if u == user {
			return nil
		}
	}
	return fmt.Errorf("허용되지 않은 user 입니다: %q (허용: %s)", user, strings.Join(cfg.Users, ", "))
}

// TargetFilePath 는 대상 파일의 유일한 경로 dir/on_off_<user>.txt 를 돌려준다.
func TargetFilePath(dir, user string) string {
	return filepath.Join(dir, "on_off_"+user+".txt")
}

// ParseVMName 은 VM 이름을 BM 이름과 ev 번호(01~99)로 나눈다. 마지막 ev+두 자리가 ev 번호다.
func ParseVMName(name string) (bm string, ev int, ok bool) {
	m := vmNameRe.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 {
		return "", 0, false
	}
	return m[1], n, true
}

// LoadTargets 는 on_off_<user>.txt 만 읽어 VM 이름 목록(파일 순서)을 돌려준다.
func LoadTargets(dir, user string) ([]string, error) {
	path := TargetFilePath(dir, user)
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("대상 파일을 읽을 수 없습니다: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("대상 파일이 심볼릭 링크입니다: %s", path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("대상 파일이 일반 파일이 아닙니다: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("대상 파일을 열 수 없습니다: %v", err)
	}
	defer f.Close()

	var names, problems []string
	seen := map[string]int{}
	sc := bufio.NewScanner(f)
	ln := 0
	for sc.Scan() {
		ln++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, _, ok := ParseVMName(line); !ok {
			problems = append(problems, fmt.Sprintf("%d번째 줄: VM 이름 규칙 위반 %q (끝이 ev+두 자리, 01~99)", ln, line))
			continue
		}
		if prev, dup := seen[line]; dup {
			problems = append(problems, fmt.Sprintf("%d번째 줄: 중복된 VM %q (처음 %d번째 줄)", ln, line, prev))
			continue
		}
		seen[line] = ln
		names = append(names, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("대상 파일 읽기 실패: %v", err)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("대상 파일 %s 오류:\n  %s", path, strings.Join(problems, "\n  "))
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("대상 파일에 VM 이 없습니다: %s", path)
	}
	return names, nil
}

// SortByBMEv 는 BM 이름순, 같은 BM 은 ev 오름차순으로 정렬한 복사본을 돌려준다.
func SortByBMEv(names []string) []string {
	out := append([]string(nil), names...)
	sort.SliceStable(out, func(i, j int) bool {
		bi, ei, _ := ParseVMName(out[i])
		bj, ej, _ := ParseVMName(out[j])
		if bi != bj {
			return bi < bj
		}
		return ei < ej
	})
	return out
}
