package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

const usageText = `사용법: install_mn [-c <설정파일>] <호스트파일>
  호스트파일   한 줄에 hostname 하나 (빈 줄과 # 줄은 무시)
  -c           설정 파일 지정 (기본: 실행한 디렉터리의 pm_monitor.conf)
`

func usage() {
	fmt.Fprint(os.Stderr, usageText)
	os.Exit(2)
}

// parseArgs 는 인자에서 설정 파일 경로와 호스트파일 경로를 뽑는다.
func parseArgs(args []string) (conf, hostfile string, ok bool) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-c":
			if i+1 >= len(args) {
				return "", "", false
			}
			conf = args[i+1]
			i++
		case strings.HasPrefix(a, "-") && a != "-":
			return "", "", false
		default:
			if hostfile != "" {
				return "", "", false
			}
			hostfile = a
		}
	}
	return conf, hostfile, hostfile != ""
}

// readHosts 는 호스트파일을 읽는다. 공백 제거, 빈 줄·# 줄 제외, 중복은 처음 것만 남긴다.
func readHosts(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	seen := map[string]bool{}
	var hosts []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		h := strings.TrimSpace(sc.Text())
		if h == "" || strings.HasPrefix(h, "#") || seen[h] {
			continue
		}
		seen[h] = true
		hosts = append(hosts, h)
	}
	return hosts, sc.Err()
}

func main() {
	confPath, hostfile, ok := parseArgs(os.Args[1:])
	if !ok {
		usage()
	}
	if st, err := os.Stat(hostfile); err != nil || st.IsDir() {
		fmt.Fprintf(os.Stderr, "호스트파일을 읽을 수 없음: %s\n", hostfile)
		usage()
	}
	hosts, err := readHosts(hostfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "호스트파일 읽기 실패: %v\n", err)
		os.Exit(1)
	}
	cfg, err := LoadConfig(confPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "설정 오류: %v\n", err)
		os.Exit(1)
	}
	// 8000대에서 회차 중 쓰레기(GC 전 메모리)가 쌓여 RSS 가 50MB 를 넘지 않게 GC 를 더 자주 돌린다(계획서 7.5)
	debug.SetGCPercent(25)
	os.Exit(run(cfg, hostfile, hosts))
}

// run 은 root 인지 확인하고 수집기를 만들어 TUI 로 넘긴다. 종료 코드를 돌려준다.
func run(cfg *Config, hostfile string, hosts []string) int {
	if os.Geteuid() != 0 { // raw ICMP 는 root 만 쓸 수 있다
		fmt.Fprintln(os.Stderr, "install_mn 은 raw ICMP(ping)를 쓰므로 root 로 실행하세요. 예: sudo install_mn <호스트파일>")
		return 1
	}
	col, err := NewCollector(cfg, hosts, stateFileName(hostfile), defaultDeps())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return runTUI(col, cfg)
}
