// version.go - 드라이버 버전 ↔ .run 파일 매핑, 버전 질문
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	runPrefix = "NVIDIA-Linux-x86_64-"
	runSuffix = ".run"
)

// RunFile 은 버전에 해당하는 .run 파일의 전체 경로다.
func RunFile(dir, ver string) string {
	return filepath.Join(dir, runPrefix+ver+runSuffix)
}

// ListVersions 는 디렉토리의 NVIDIA-Linux-x86_64-<버전>.run 파일에서 버전을 모아 정렬해 돌려준다.
func ListVersions(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var vs []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, runPrefix) || !strings.HasSuffix(n, runSuffix) {
			continue
		}
		v := strings.TrimSuffix(strings.TrimPrefix(n, runPrefix), runSuffix)
		if versionRe.MatchString(v) {
			vs = append(vs, v)
		}
	}
	sort.Strings(vs)
	return vs, nil
}

// CheckRuns 는 버전이 정해진 줄의 .run 파일이 모두 있는지 본다. 없는 것은 한꺼번에 오류로 낸다.
func CheckRuns(dir string, es []Entry) error {
	var miss []string
	done := map[string]bool{}
	for _, e := range es {
		if e.Version == "" || done[e.Version] {
			continue
		}
		done[e.Version] = true
		if st, err := os.Stat(RunFile(dir, e.Version)); err != nil || st.IsDir() {
			miss = append(miss, RunFile(dir, e.Version))
		}
	}
	if len(miss) > 0 {
		return fmt.Errorf(".run 파일이 없습니다:\n  %s", strings.Join(miss, "\n  "))
	}
	return nil
}

// AskVersion 은 버전 목록을 번호로 보여 주고 하나를 고르게 한다. 잘못 입력하면 다시 묻고
// 입력이 끝나면(EOF) 오류를 돌려준다.
func AskVersion(in *bufio.Reader, out io.Writer, vs []string) (string, error) {
	fmt.Fprintln(out, "버전이 없는 호스트에 적용할 드라이버를 고르세요:")
	for i, v := range vs {
		fmt.Fprintf(out, "  %d) %s\n", i+1, v)
	}
	for {
		fmt.Fprintf(out, "번호 (1-%d): ", len(vs))
		s, err := in.ReadString('\n')
		s = strings.TrimSpace(s)
		if s != "" {
			if n, e := strconv.Atoi(s); e == nil && n >= 1 && n <= len(vs) {
				return vs[n-1], nil
			}
			fmt.Fprintln(out, "잘못된 입력입니다.")
		}
		if err != nil {
			return "", fmt.Errorf("버전을 선택하지 못했습니다: %w", err)
		}
	}
}

// ResolveVersions 는 버전 없는 줄이 있으면 한 번만 물어 전부에 적용하고, 마지막에 .run 존재를 검사한다.
// es 를 직접 고쳐 채운다.
func ResolveVersions(dir string, es []Entry, in *bufio.Reader, out io.Writer) error {
	if err := CheckRuns(dir, es); err != nil {
		return err
	}
	var blank []int
	for i, e := range es {
		if e.Version == "" {
			blank = append(blank, i)
		}
	}
	if len(blank) == 0 {
		return nil
	}
	vs, err := ListVersions(dir)
	if err != nil {
		return err
	}
	if len(vs) == 0 {
		return fmt.Errorf("%s 에 %s<버전>%s 파일이 없습니다", dir, runPrefix, runSuffix)
	}
	v, err := AskVersion(in, out, vs)
	if err != nil {
		return err
	}
	for _, i := range blank {
		es[i].Version = v
	}
	return nil
}
