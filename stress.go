package main

// -run 워커가 쓰는 외부 명령 실행기: 판정(judge)·설정수정(config_check -auto)·stress-test 호출·CPU 감시용 확인(gossh).
// 워커 상태기계는 executor 인터페이스만 보고 동작하므로 시험에서는 가짜 실행기·가짜 시각을 넣는다.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// probeInfo 는 CPU 감시 확인 한 번의 호스트별 결과(응답한 호스트만 맵에 들어간다).
type probeInfo struct {
	Btime  int64  // /proc/stat btime(부팅 시각, 초)
	CPU    string // CPU테스트 열 값(O / X / NOTRUN)
	Stress string // stress-ng 열 값(O / NOTRUN)
}

// executor 는 워커가 쓰는 외부 동작과 시각 함수.
type executor interface {
	// Judge 는 호스트파일 전체를 판정한다(main 의 judge 와 같음).
	Judge() (*judgeResult, error)
	// ConfigFix 는 hosts 에 config_check.sh -auto <user> 를 돌린다(체크 + 수정).
	ConfigFix(hosts []string) error
	// StressTest 는 stress-test -steps <steps> 를 listFile(hosts 한 줄씩)로 실행하고 끝날 때까지 기다린다.
	// end("연,월,일,시,분")는 steps 에 b 가 있을 때 -end 로 넘긴다. 등록·launch 가 끝나 감시에 들어가면 launched 를 (한 번) 부른다.
	// stress-test 가 "[인벤토리사용중] <메시지>" 줄을 내고 stdin 을 기다리면 busy(메시지)를 불러 "r"(다시 확인) 또는 "x"(이 묶음 중단)를 받아 stdin 에 넣는다
	// (busy 가 답을 기다리는 동안 시간 제한은 멈춘다). 종료코드와 오류(errTimeout 등)를 돌려준다.
	// stress-test 가 "[등록] yml=<yml> hosts=<쉼표>" 줄을 내면(등록·동기화 직후) registered(yml, hosts)를 부른다(hosts 를 못 읽었으면 nil). launched 보다 먼저 불린다.
	StressTest(steps, listFile string, hosts []string, end string, launched func(), registered func(yml string, hosts []string), busy func(msg string) string) (int, error)
	// YamlState 는 stress-test -yaml-state 로 인벤토리 소스의 현재 yml 과 인벤토리 호스트 이름을 읽는다(읽기 전용, -auto 의 launch 직전 확인).
	YamlState() (yamlInfo, error)
	// Probe 는 hosts 의 btime·CPU·stress-ng 값을 한 번 확인한다. 응답 없는 호스트는 맵에 없다.
	Probe(hosts []string) (map[string]probeInfo, error)
	// Loads 는 hosts 의 1분 부하(/proc/loadavg 첫 값)를 한 번 확인한다. 응답 없는 호스트는 맵에 없다.
	Loads(hosts []string) (map[string]float64, error)
	Now() time.Time
	Sleep(d time.Duration)
}

// stressMonitorMark 는 stress-test -batch 가 감시에 들어가면 stdout 에 찍는 줄의 머리말.
// ★ stress-test 는 한 단계의 모든 묶음(Intel → AMD)을 차례로 등록·launch·poll 한 뒤에 감시(monitorLoad)를 시작하고,
// 감시는 회차마다 "[감시] <단계> 경과 …" 한 줄을 낸다(monitor.go summaryLine). 그래서 이 줄이 처음 보이면
// 그 단계의 등록·launch 가 모두 끝난 것이다. 묶음이 모두 실패하면 감시 없이 끝나므로 프로세스 종료도 함께 본다.
const stressMonitorMark = "[감시]"

// stressBusyMark 는 stress-test -batch 가 템플릿 인벤토리가 DEFAULT 가 아니어서 멈출 때 stdout 에 찍는 줄의 머리말.
// 이 줄 뒤에 stdin 에서 한 줄(r = 다시 확인, 그 밖·EOF = 이 묶음 중단)을 읽는다.
const stressBusyMark = "[인벤토리사용중] "

// stressRegisteredMark 는 stress-test 가 yml 을 등록(invsync)한 직후 찍는 줄의 머리말: "[등록] yml=<yml> hosts=<쉼표>"(hosts 는 못 읽으면 없음).
// -auto 가 launch 직전에 다른 사람이 인벤토리를 바꿨는지 보려고 우리가 등록한 값으로 기억한다.
const stressRegisteredMark = "[등록]"

// yamlStateMark 는 stress-test -yaml-state 가 줄마다 붙이는 머리말("[yaml상태] key=value").
const yamlStateMark = "[yaml상태]"

// yamlInfo 는 인벤토리 소스의 yml 과 인벤토리 호스트 이름. Hosts 가 비어 있으면 '모름'으로 본다(등록 줄에서 못 읽은 경우).
type yamlInfo struct {
	Yml   string
	Hosts []string
}

// kvFields 는 "k=v k2=v2" 줄을 맵으로 나눈다.
func kvFields(s string) map[string]string {
	m := map[string]string{}
	for _, f := range strings.Fields(s) {
		if k, v, ok := strings.Cut(f, "="); ok {
			m[k] = v
		}
	}
	return m
}

// splitHosts 는 쉼표로 이은 호스트 이름을 나눈다(빈 값은 nil).
func splitHosts(s string) []string {
	var out []string
	for _, h := range strings.Split(s, ",") {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// parseRegistered 는 "[등록]" 줄에서 머리말을 뗀 나머지에서 yml·hosts 를 읽는다. yml 이 없으면 ok=false.
func parseRegistered(rest string) (yml string, hosts []string, ok bool) {
	m := kvFields(rest)
	if m["yml"] == "" {
		return "", nil, false
	}
	return m["yml"], splitHosts(m["hosts"]), true
}

// parseYamlStateOutput 은 stress-test -yaml-state 출력에서 "[yaml상태]" 줄의 yml·hosts 를 읽는다. yml 이 없으면 오류.
func parseYamlStateOutput(out string) (yamlInfo, error) {
	var st yamlInfo
	found := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, yamlStateMark) {
			continue
		}
		m := kvFields(strings.TrimPrefix(line, yamlStateMark))
		if v, ok := m["yml"]; ok {
			st.Yml, found = v, true
		}
		if v, ok := m["hosts"]; ok {
			st.Hosts = splitHosts(v)
		}
	}
	if !found {
		return yamlInfo{}, errors.New("stress-test -yaml-state 출력에서 yml 값을 찾지 못했습니다(stress-test 가 옛 버전일 수 있습니다)")
	}
	return st, nil
}

// lineWatch 는 받은 출력을 w 로 넘기면서 줄 머리말이 mark 인 줄이 처음 보이면 fn 을 부르고,
// 머리말이 busyMark 인 줄마다 (머리말을 뗀 메시지로) busy 를, 머리말이 regMark 인 줄마다 (머리말을 뗀 나머지로) reg 를 부른다
// (busy 가 돌아올 때까지 출력 읽기는 멈춘다).
type lineWatch struct {
	w        io.Writer
	mark     []byte
	fn       func()
	busyMark []byte
	busy     func(msg string)
	regMark  []byte
	reg      func(rest string)
	buf      []byte
	once     sync.Once
}

func (l *lineWatch) Write(p []byte) (int, error) {
	if l.w != nil {
		l.w.Write(p)
	}
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimSpace(l.buf[:i])
		l.buf = l.buf[i+1:]
		if len(l.mark) > 0 && bytes.HasPrefix(line, l.mark) && l.fn != nil {
			l.once.Do(l.fn)
		}
		if len(l.busyMark) > 0 && bytes.HasPrefix(line, bytes.TrimSpace(l.busyMark)) && l.busy != nil {
			l.busy(string(bytes.TrimSpace(line[len(bytes.TrimSpace(l.busyMark)):])))
		}
		if len(l.regMark) > 0 && bytes.HasPrefix(line, l.regMark) && l.reg != nil {
			l.reg(string(bytes.TrimSpace(line[len(l.regMark):])))
		}
	}
	return len(p), nil
}

// realExec 는 실제 명령을 돌리는 executor.
type realExec struct {
	conf    config
	gossh   string    // gossh 실제 경로
	hostArg string    // gossh -w 로 넘기는 호스트파일 인자
	user    string    // config_check -auto / stress-test -user
	dir     string    // 상태 폴더(lists/ 를 쓴다)
	log     io.Writer // 외부 명령 출력(worker.log)
}

func (e *realExec) Now() time.Time { return time.Now() }

// Sleep 은 d 만큼 쉬되 중지 요청(cancelCh)이 오면 바로 돌아온다.
func (e *realExec) Sleep(d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-cancelCh:
	}
}

func (e *realExec) Judge() (*judgeResult, error) {
	return judge(e.conf, e.gossh, e.hostArg, e.log)
}

func (e *realExec) ConfigFix(hosts []string) error {
	if e.conf.OSCheckSh == "" {
		return errors.New("설정 파일의 OS_CHECK_SH 가 비어 있어 설정수정을 할 수 없습니다")
	}
	_, err := runConfigCheck(e.conf, expandPath(e.conf.OSCheckSh), e.gossh, hosts, ccFix, e.user, e.log)
	return err
}

// StressTest 는 stress-test 를 바이너리 폴더에서 실행한다(그 폴더의 ./conf, ./output 이 단독 실행과 같게 쓰이도록).
func (e *realExec) StressTest(steps, listFile string, hosts []string, end string, launched func(), registered func(yml string, hosts []string), busy func(msg string) string) (int, error) {
	if e.conf.StressTest == "" {
		return 2, errors.New("설정 파일의 STRESS_TEST(stress-test 경로)가 비어 있습니다")
	}
	bin, err := filepath.Abs(e.conf.StressTest)
	if err != nil {
		return 2, err
	}
	list, err := filepath.Abs(listFile)
	if err != nil {
		return 2, err
	}
	args := []string{"-user", e.user, "-hosts", list, "-steps", steps, "-batch"}
	if strings.Contains(steps, "b") && end != "" {
		args = append(args, "-end", end)
	}
	if e.conf.StressTestConf != "" {
		c, _ := filepath.Abs(e.conf.StressTestConf)
		args = append(args, "-conf", c)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = filepath.Dir(bin)
	// stress-test 는 gossh 를 conf gossh → 환경변수 GOSSH → PATH 순으로 찾는다. 단독 실행은 stress_run.sh 가 GOSSH 를
	// 넘기지만 여기서는 바이너리를 직접 부르므로, GOSSH 가 비어 있으면 new_setup 이 쓰는 gossh 경로를 넘긴다.
	if os.Getenv("GOSSH") == "" && e.gossh != "" {
		cmd.Env = append(os.Environ(), "GOSSH="+e.gossh)
	}
	// stdin 은 인벤토리 사용 중 질문의 답(r / x)을 넣는 통로. 프로세스가 끝나면 exec 이 닫는다(읽던 쪽은 EOF = 중단).
	in, err := cmd.StdinPipe()
	if err != nil {
		return 2, err
	}
	hold := make(chan bool, 64) // 질문에 답하는 동안 시간 제한 일시정지(true) / 재개(false)
	cmd.Stdout = &lineWatch{w: e.log, mark: []byte(stressMonitorMark), fn: launched, regMark: []byte(stressRegisteredMark), reg: func(rest string) {
		if registered != nil {
			if yml, hs, ok := parseRegistered(rest); ok {
				registered(yml, hs)
			}
		}
	}, busyMark: []byte(stressBusyMark), busy: func(msg string) {
		select {
		case hold <- true:
		default:
		}
		ans := "x"
		if busy != nil {
			ans = busy(msg)
		}
		if ans != "r" {
			ans = "x"
		}
		io.WriteString(in, ans+"\n")
		select {
		case hold <- false:
		default:
		}
	}}
	cmd.Stderr = e.log
	fmt.Fprintf(e.log, "+ (cd %s && %s %s)\n", cmd.Dir, bin, strings.Join(args, " "))
	_, err = runCmdHold(cmd, e.conf.StressTimeout, cancelCh, hold)
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}

// yamlStateTimeout 은 stress-test -yaml-state(AWX 조회 몇 번) 한 번의 시간 제한.
const yamlStateTimeout = 2 * time.Minute

// YamlState 는 stress-test -yaml-state 를 바이너리 폴더에서 실행해 인벤토리 소스의 현재 yml·호스트를 읽는다(읽기 전용).
func (e *realExec) YamlState() (yamlInfo, error) {
	if e.conf.StressTest == "" {
		return yamlInfo{}, errors.New("설정 파일의 STRESS_TEST(stress-test 경로)가 비어 있습니다")
	}
	bin, err := filepath.Abs(e.conf.StressTest)
	if err != nil {
		return yamlInfo{}, err
	}
	args := []string{"-user", e.user, "-yaml-state"}
	if e.conf.StressTestConf != "" {
		c, _ := filepath.Abs(e.conf.StressTestConf)
		args = append(args, "-conf", c)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = filepath.Dir(bin)
	fmt.Fprintf(e.log, "+ (cd %s && %s %s)\n", cmd.Dir, bin, strings.Join(args, " "))
	out, err := runCmd(cmd, yamlStateTimeout)
	io.WriteString(e.log, out)
	if err != nil {
		msg := strings.TrimSpace(out)
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		if msg != "" {
			return yamlInfo{}, fmt.Errorf("%w: %s", err, msg)
		}
		return yamlInfo{}, err
	}
	return parseYamlStateOutput(out)
}

// probeCmd 는 CPU 감시 확인 명령: 부팅 시각 + 1차 정보수집 명령(CPU·stress-ng 값). csh 에서도 안전한 조각만 쓴다.
func probeCmd(c config) string {
	return "grep btime /proc/stat; " + buildInfoCmd(c)
}

// parseProbeOutput 은 확인 출력에서 btime 을 낸 호스트만 응답으로 보고 값을 채운다.
func parseProbeOutput(out string) map[string]probeInfo {
	bt := map[string]int64{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		i := strings.Index(line, ": ")
		if i <= 0 {
			continue
		}
		f := strings.Fields(line[i+2:])
		if len(f) == 2 && f[0] == "btime" {
			if n, err := strconv.ParseInt(f[1], 10, 64); err == nil {
				bt[line[:i]] = n
			}
		}
	}
	info := parseInfoOutput(out)
	res := map[string]probeInfo{}
	for h, b := range bt {
		ih := info[h]
		res[h] = probeInfo{Btime: b, CPU: ih.cpuResult(), Stress: ih.stressResult()}
	}
	return res
}

func (e *realExec) Probe(hosts []string) (map[string]probeInfo, error) {
	cmdText := probeCmd(e.conf)
	if isDangerousCommand(cmdText) {
		return nil, fmt.Errorf("확인 명령에 gossh 위험어가 들어 있습니다: %s", cmdText)
	}
	list := filepath.Join(e.dir, listsDir, "probe.txt")
	if err := writeList(list, hosts); err != nil {
		return nil, err
	}
	out, err := runGossh(e.gossh, []string{"-w", list, "-pm", "-script", cmdText}, e.log, e.conf.GosshTimeout)
	for _, s := range resSuffixes {
		os.Remove(list + s)
	}
	if err != nil {
		return nil, err
	}
	return parseProbeOutput(out), nil
}

// writeList 는 호스트를 한 줄에 하나씩 쓴다(stress-test·config_check·gossh 입력).
func writeList(path string, hosts []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(hosts, "\n")+"\n"), 0o644)
}

// loadCmd 는 부하 확인 명령. csh 에서도 안전하게 중괄호·느낌표·따옴표 없이 쓴다.
const loadCmd = "cat /proc/loadavg"

// parseLoadOutput 은 "호스트: 0.52 0.30 0.20 1/234 5678" 꼴 줄에서 호스트별 1분 부하를 읽는다(읽을 수 없는 줄은 무시).
func parseLoadOutput(out string) map[string]float64 {
	res := map[string]float64{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		i := strings.Index(line, ": ")
		if i <= 0 {
			continue
		}
		f := strings.Fields(line[i+2:])
		if len(f) < 4 || !strings.Contains(f[3], "/") { // /proc/loadavg 는 "1분 5분 15분 실행/전체 마지막pid"
			continue
		}
		if v, err := strconv.ParseFloat(f[0], 64); err == nil && v >= 0 {
			res[line[:i]] = v
		}
	}
	return res
}

func (e *realExec) Loads(hosts []string) (map[string]float64, error) {
	list := filepath.Join(e.dir, listsDir, "load.txt")
	if err := writeList(list, hosts); err != nil {
		return nil, err
	}
	out, err := runGossh(e.gossh, []string{"-w", list, "-pm", "-script", loadCmd}, e.log, e.conf.GosshTimeout)
	for _, s := range resSuffixes {
		os.Remove(list + s)
	}
	if err != nil {
		return nil, err
	}
	return parseLoadOutput(out), nil
}
