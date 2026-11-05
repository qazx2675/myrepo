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
	// 등록·launch 가 끝나 감시에 들어가면 launched 를 (한 번) 부른다. 종료코드와 오류(errTimeout 등)를 돌려준다.
	StressTest(steps, listFile string, hosts []string, launched func()) (int, error)
	// Probe 는 hosts 의 btime·CPU·stress-ng 값을 한 번 확인한다. 응답 없는 호스트는 맵에 없다.
	Probe(hosts []string) (map[string]probeInfo, error)
	Now() time.Time
	Sleep(d time.Duration)
}

// stressMonitorMark 는 stress-test -batch 가 감시에 들어가면 stdout 에 찍는 줄의 머리말.
// ★ stress-test 는 한 단계의 모든 묶음(Intel → AMD)을 차례로 등록·launch·poll 한 뒤에 감시(monitorLoad)를 시작하고,
// 감시는 회차마다 "[감시] <단계> 경과 …" 한 줄을 낸다(monitor.go summaryLine). 그래서 이 줄이 처음 보이면
// 그 단계의 등록·launch 가 모두 끝난 것이다. 묶음이 모두 실패하면 감시 없이 끝나므로 프로세스 종료도 함께 본다.
const stressMonitorMark = "[감시]"

// lineWatch 는 받은 출력을 w 로 넘기면서 줄 머리말이 mark 인 줄이 처음 보이면 fn 을 부른다.
type lineWatch struct {
	w    io.Writer
	mark []byte
	fn   func()
	buf  []byte
	once sync.Once
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
		if bytes.HasPrefix(bytes.TrimSpace(l.buf[:i]), l.mark) && l.fn != nil {
			l.once.Do(l.fn)
		}
		l.buf = l.buf[i+1:]
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
func (e *realExec) StressTest(steps, listFile string, hosts []string, launched func()) (int, error) {
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
	if e.conf.StressTestConf != "" {
		c, _ := filepath.Abs(e.conf.StressTestConf)
		args = append(args, "-conf", c)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = filepath.Dir(bin)
	cmd.Stdout = &lineWatch{w: e.log, mark: []byte(stressMonitorMark), fn: launched}
	cmd.Stderr = e.log
	fmt.Fprintf(e.log, "+ (cd %s && %s %s)\n", cmd.Dir, bin, strings.Join(args, " "))
	_, err = runCmd(cmd, e.conf.StressTimeout)
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
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
