package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

const usageText = `사용법: install_mn [-c <설정파일>] <호스트파일> [oklist.txt]
  호스트파일   한 줄에 hostname 하나 (빈 줄과 # 줄은 무시)
  oklist.txt   (선택) 서버별 기준 설정표. 탭 구분 7칸: hostname OS커널 appl dns ldap_URI binddn h/t
               l 화면에서 다른 칸을 빨간색으로, <호스트파일>.res 의 설정동일여부에 O/X 로 보여 줍니다
  -c           설정 파일 지정 (기본: 실행한 디렉터리의 pm_monitor.conf)
  수집은 백그라운드에서 돌고 화면은 붙었다 떼었다 합니다. q 로 화면만 나가면 수집은 계속되고,
  같은 호스트파일로 다시 실행하면 이어서 붙습니다. 수집은 시작 12시간 뒤 스스로 끝납니다.
       install_mn --status [<호스트파일>]   실행 중인 수집 보기(호스트파일을 빼면 전부)
       install_mn --stop <호스트파일>       수집 끝내기(화면에서는 Q 후 y)
       install_mn -f [-c <설정파일>] <호스트파일> [oklist.txt]   백그라운드 없이 화면과 함께 실행·종료(옛 방식)
`

func usage() {
	fmt.Fprint(os.Stderr, usageText)
	os.Exit(2)
}

// 실행 방식
const (
	modeAttach = ""       // 수집 데몬을 확인(없으면 시작)하고 화면을 붙인다
	modeFG     = "fg"     // -f: 데몬 없이 이 프로세스가 수집하고 화면을 닫으면 끝난다
	modeDaemon = "daemon" // --daemon: 내부용(화면이 자기 자신을 이 모드로 다시 실행한다)
	modeStatus = "status" // --status
	modeStop   = "stop"   // --stop
)

type options struct {
	conf, hostfile, mode string
	oklist               string // 두 번째 위치 인자(선택, 계획서 4.9). --status·--stop 에는 쓸 수 없다
}

// parseArgs 는 인자에서 실행 방식, 설정 파일 경로, 호스트파일 경로를 뽑는다.
func parseArgs(args []string) (o options, ok bool) {
	setMode := func(m string) bool {
		if o.mode != modeAttach {
			return false
		}
		o.mode = m
		return true
	}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-c":
			if i+1 >= len(args) {
				return options{}, false
			}
			o.conf = args[i+1]
			i++
		case a == "-f":
			if !setMode(modeFG) {
				return options{}, false
			}
		case a == "--daemon":
			if !setMode(modeDaemon) {
				return options{}, false
			}
		case a == "--status":
			if !setMode(modeStatus) {
				return options{}, false
			}
		case a == "--stop":
			if !setMode(modeStop) {
				return options{}, false
			}
		case strings.HasPrefix(a, "-") && a != "-":
			return options{}, false
		default:
			switch {
			case o.hostfile == "":
				o.hostfile = a
			case o.oklist == "":
				o.oklist = a
			default:
				return options{}, false
			}
		}
	}
	if o.oklist != "" && (o.mode == modeStatus || o.mode == modeStop) {
		return options{}, false
	}
	if o.mode == modeStatus {
		return o, o.conf == ""
	}
	return o, o.hostfile != ""
}

// yumWord 는 호스트파일 두 번째 칸에서 yum update 모드를 뜻하는 글자다(대소문자 무시, 계획서 4.8).
const yumWord = "yum"

// readHostSpecs 는 호스트파일을 읽는다(계획서 4.8). 한 줄은 공백·탭으로 나눈 칸이고,
// 첫 칸이 hostname, 두 번째 칸이 yum(대소문자 무시)이면 yum update 모드, 그 밖의 값이나 칸 없음은 OS 설치다.
// 세 번째 칸부터는 무시한다. 빈 줄·# 줄은 건너뛰고, 같은 hostname 은 처음 줄만 쓴다.
// yum 은 yum 모드인 hostname 의 집합이다(없으면 빈 맵).
func readHostSpecs(path string) (hosts []string, yum map[string]bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	seen := map[string]bool{}
	yum = map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) == 0 || strings.HasPrefix(fs[0], "#") || seen[fs[0]] {
			continue
		}
		h := fs[0]
		seen[h] = true
		hosts = append(hosts, h)
		if len(fs) >= 2 && strings.EqualFold(fs[1], yumWord) {
			yum[h] = true
		}
	}
	return hosts, yum, sc.Err()
}

// readHosts 는 호스트 이름만 필요할 때 쓴다(모드는 버린다).
func readHosts(path string) ([]string, error) {
	hosts, _, err := readHostSpecs(path)
	return hosts, err
}

func main() {
	opt, ok := parseArgs(os.Args[1:])
	if !ok {
		usage()
	}
	if os.Geteuid() != 0 { // raw ICMP 는 root 만 쓸 수 있다. 수집 소켓도 root 전용이다
		fmt.Fprintln(os.Stderr, "install_mn 은 raw ICMP(ping)를 쓰므로 root 로 실행하세요. 예: sudo install_mn <호스트파일>")
		os.Exit(1)
	}
	switch opt.mode {
	case modeStatus:
		os.Exit(cmdStatus(opt.hostfile))
	case modeStop:
		os.Exit(cmdStop(opt.hostfile))
	}
	if st, err := os.Stat(opt.hostfile); err != nil || st.IsDir() {
		fmt.Fprintf(os.Stderr, "호스트파일을 읽을 수 없음: %s\n", opt.hostfile)
		usage()
	}
	hosts, yum, err := readHostSpecs(opt.hostfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "호스트파일 읽기 실패: %v\n", err)
		os.Exit(1)
	}
	var okl *Oklist
	if opt.oklist != "" {
		if okl, err = loadOklist(opt.oklist); err != nil {
			fmt.Fprintf(os.Stderr, "oklist 파일을 읽을 수 없음: %s (%v)\n", opt.oklist, err)
			os.Exit(1)
		}
	}
	cfg, err := LoadConfig(opt.conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "설정 오류: %v\n", err)
		os.Exit(1)
	}
	// 8000대에서 회차 중 쓰레기(GC 전 메모리)가 쌓여 RSS 가 50MB 를 넘지 않게 GC 를 더 자주 돌린다(계획서 7.5)
	debug.SetGCPercent(25)
	switch opt.mode {
	case modeDaemon:
		os.Exit(runDaemon(cfg, opt.hostfile, opt.oklist, hosts, WithYum(yum), WithOklist(okl)))
	case modeFG:
		os.Exit(runForeground(cfg, opt.hostfile, hosts, WithYum(yum), WithOklist(okl)))
	}
	os.Exit(runAttach(opt, cfg))
}

// runForeground 는 데몬 없이 수집기를 만들어 TUI 로 넘긴다. 화면을 닫으면 수집도 끝난다. 종료 코드를 돌려준다.
func runForeground(cfg *Config, hostfile string, hosts []string, opts ...CollectorOpt) int {
	opts = append(opts, WithResFile(resFileName(hostfile), nil)) // 실패는 화면 오류 줄에 보인다
	col, err := NewCollector(cfg, hosts, stateFileName(hostfile), defaultDeps(), opts...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return runTUI(col, cfg)
}

// runAttach 는 수집 데몬이 돌고 있게 한 뒤(없으면 시작) 화면을 붙인다.
func runAttach(opt options, cfg *Config) int {
	if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
		fmt.Fprintln(os.Stderr, "install_mn 은 터미널(tty)에서 실행해야 합니다.")
		return 1
	}
	sock, notice, err := ensureDaemon(opt.conf, opt.hostfile, opt.oklist, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rs, err := attachRemote(sock)
	if err != nil {
		fmt.Fprintln(os.Stderr, "수집에 붙지 못함:", err)
		return 1
	}
	return runTUIRemote(rs, cfg, notice, opt.hostfile)
}
