package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

var docNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

const fakeGosshHelp = `Usage of gossh:
  -P string
    	SSH 포트 (default "22")
  -pm
    	/user/svrauto 마운트 상태 추가 점검 및 OS설치중 감지
  -script
    	작업 요약 출력 숨김
  -w string
    	호스트 목록 파일
`

type docTest struct {
	t      *testing.T
	cwd    string
	alive  bool
	pids   map[int]bool
	d      doctorDeps
	gossh  func(args []string) (stdout, all string)
	calls  [][]string
	checkV map[string]string
}

// newDocTest 는 임시 작업 경로와 가짜 의존성을 만든다.
func newDocTest(t *testing.T) *docTest {
	t.Helper()
	x := &docTest{t: t, cwd: t.TempDir(), pids: map[int]bool{}}
	x.d = doctorDeps{
		now:        func() time.Time { return docNow },
		cwd:        x.cwd,
		loadConf:   func(string) (config, string, error) { return parseConf(""), "", nil },
		exePath:    func() (string, error) { return filepath.Join(x.cwd, "bin", "new_setup"), nil },
		lookPath:   func(n string) (string, error) { return "", fmt.Errorf("not found: %s", n) },
		alive:      func(string) bool { return x.alive },
		pidRunning: func(p int) bool { return x.pids[p] },
		gosshHelp:  func(string) (string, error) { return fakeGosshHelp, nil },
		gossh: func(_ string, args []string, _ time.Duration) (string, string, error) {
			x.calls = append(x.calls, args)
			if x.gossh == nil {
				return "", "", nil
			}
			so, all := x.gossh(args)
			return so, all, nil
		},
		checkRun: func(_ config, _, _ string, hosts []string) (map[string]string, error) {
			res := map[string]string{}
			for _, h := range hosts {
				res[h] = x.checkV[h]
			}
			return res, nil
		},
	}
	return x
}

func (x *docTest) write(name, text string, mode os.FileMode) string {
	x.t.Helper()
	p := filepath.Join(x.cwd, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		x.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), mode); err != nil {
		x.t.Fatal(err)
	}
	return p
}

// state 는 hosts / hosts_run 을 만들고 상태를 저장한다. mod 로 상태를 고친다.
func (x *docTest) state(mod func(s *runState)) (hostFile, dir string) {
	x.t.Helper()
	hostFile = x.write("hosts", "ev01\nev02\n", 0o644)
	dir = filepath.Join(x.cwd, "hosts_run")
	st := &runState{
		Version: stateVersion, HostArg: hostFile, HostFile: hostFile, WorkDir: x.cwd, User: "tester",
		Created: docNow.Add(-time.Hour), Stage: stageFix, Busy: true, Detail: "설정수정 중", WorkerPID: 4242,
		Updated: docNow.Add(-5 * time.Second), Heartbeat: docNow.Add(-2 * time.Second),
		Hosts: []hostRow{
			{Host: "ev01", Status: stDone, Check: "O", CPU: "O", Stress: "O", HT: "ON", OS: "4.18"},
			{Host: "ev02", Status: stDone, Check: "X", CPU: "NOTRUN", Stress: "NOTRUN", HT: "ON", OS: "4.18"},
		},
	}
	if mod != nil {
		mod(st)
	}
	if err := saveState(dir, st); err != nil {
		x.t.Fatal(err)
	}
	return hostFile, dir
}

func (x *docTest) run(hostFile string) (*docReport, string) {
	x.t.Helper()
	r := &docReport{}
	docRun(r, doctorArgs{hostFile: hostFile}, x.d, &docEnvInfo{conf: parseConf("")})
	return r, r.b.String()
}

func mustContain(t *testing.T, text string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(text, s) {
			t.Errorf("보고서에 %q 가 없습니다:\n%s", s, text)
		}
	}
}

func mustNotContain(t *testing.T, text string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(text, s) {
			t.Errorf("보고서에 %q 가 있으면 안 됩니다:\n%s", s, text)
		}
	}
}

func levels(r *docReport) string {
	var l []string
	for _, p := range r.Problems {
		l = append(l, p.Level)
	}
	return strings.Join(l, "")
}

func TestDoctorNoStateDir(t *testing.T) {
	x := newDocTest(t)
	hf := x.write("hosts", "ev01\n", 0o644)
	r, text := x.run(hf)
	mustContain(t, text, "상태 폴더가 없습니다", "실행한 적이 없다는 뜻")
	if len(r.Problems) != 0 {
		t.Errorf("문제 없어야 함: %v", r.Problems)
	}
}

func TestDoctorDeadWorkerMidStage(t *testing.T) {
	x := newDocTest(t)
	hf, dir := x.state(nil)
	x.alive = false
	x.pids[4242] = true // lock 은 없는데 같은 pid 프로세스는 있는 경우도 함께 알린다
	os.WriteFile(filepath.Join(dir, workerLog), []byte("2026-11-05 11:00:00 워커 시작 pid=4242\n2026-11-05 11:00:05 설정수정 시작 1대: ev02\n"), 0o644)
	r, text := x.run(hf)
	mustContain(t, text, "[X] 워커가 죽어 있습니다: 설정수정 단계 진행 중이었음", "외부 명령", "같은 명령으로 다시 실행하세요: new_setup -run "+hf,
		"이어서 진행할까요?", "lock 은 안 잡혀 있는데 pid 4242", "설정수정 시작 1대: ev02")
	if !strings.Contains(levels(r), lvBad) {
		t.Errorf("[X] 문제가 있어야 함: %v", r.Problems)
	}
}

func TestDoctorPendingQuestion(t *testing.T) {
	x := newDocTest(t)
	hf, dir := x.state(func(s *runState) {
		s.Busy = false
		s.QSeq = 3
		s.Question = &question{ID: "q3", Stage: stageFix, Text: "설정체크 O 아닌 호스트 1대", Asked: docNow.Add(-20 * time.Minute),
			Choices: []choice{{"c", "계속"}, {"r", "재시도"}, {"s", "중지"}}}
	})
	x.alive = true
	// 이전 질문(q2)에 대한 답이 남아 있음
	if err := writeAnswer(dir, "q2", "c"); err != nil {
		t.Fatal(err)
	}
	r, text := x.run(hf)
	mustContain(t, text, "질문 q3 에 대한 답을 기다리는 중", "20분 0초 전", "설정체크 O 아닌 호스트 1대", "TUI 에서 답하세요: new_setup -run "+hf,
		"c 계속 / r 재시도 / s 중지", "answer.json 의 질문 id(q2)가 현재 질문(q3)과 달라", "[O] 워커 정상")
	if levels(r) != lvWarn+lvWarn {
		t.Errorf("주의 2건이어야 함(질문 대기, 답 불일치): %v", r.Problems)
	}
	// 읽기 전용: answer.json 은 그대로 남아 있어야 한다
	if _, err := os.Stat(filepath.Join(dir, answerFile)); err != nil {
		t.Errorf("answer.json 이 지워졌습니다: %v", err)
	}
}

func TestDoctorQuestionWithDeadWorker(t *testing.T) {
	x := newDocTest(t)
	hf, dir := x.state(func(s *runState) {
		s.Busy = false
		s.Question = &question{ID: "q1", Stage: stageJudge, Text: "t", Asked: docNow, Choices: []choice{{"c", "계속"}}}
	})
	x.alive = false
	writeAnswer(dir, "q1", "c")
	_, text := x.run(hf)
	mustContain(t, text, "워커가 죽어 있습니다", "먼저 같은 명령으로 다시 실행", "답(c)이 기록돼 있지만 처리할 워커가 없습니다")
}

func TestDoctorStaleHeartbeat(t *testing.T) {
	x := newDocTest(t)
	hf, _ := x.state(func(s *runState) { s.Heartbeat = docNow.Add(-90 * time.Second) })
	x.alive = true
	x.pids[4242] = true
	r, text := x.run(hf)
	mustContain(t, text, "[X] 워커는 살아 있으나(lock 잡힘, pid 4242) heartbeat 가 1분 30초 동안 갱신되지 않았습니다", "kill -9 4242", "ps -ef | grep")
	if !strings.Contains(levels(r), lvBad) {
		t.Errorf("[X] 기대: %v", r.Problems)
	}
	// 15초 이내면 정상
	x2 := newDocTest(t)
	hf2, _ := x2.state(func(s *runState) { s.Heartbeat = docNow.Add(-10 * time.Second) })
	x2.alive = true
	r2, text2 := x2.run(hf2)
	mustContain(t, text2, "[O] 워커 정상: 살아 있고 heartbeat 10초 전")
	if len(r2.Problems) != 0 {
		t.Errorf("문제 없어야 함: %v", r2.Problems)
	}
}

func TestDoctorStopReq(t *testing.T) {
	// 워커 살아 있음: 곧 중지
	x := newDocTest(t)
	hf, dir := x.state(nil)
	x.alive = true
	os.WriteFile(filepath.Join(dir, stopReqFile), []byte("x"), 0o644)
	_, text := x.run(hf)
	mustContain(t, text, "중지 요청(stop.req", "워커가 곧 중지합니다")
	// 워커 죽음: 재기동해도 즉시 중지 → 지우라고 안내(지우지는 않음)
	x2 := newDocTest(t)
	hf2, dir2 := x2.state(nil)
	x2.alive = false
	os.WriteFile(filepath.Join(dir2, stopReqFile), []byte("x"), 0o644)
	_, text2 := x2.run(hf2)
	mustContain(t, text2, "워커가 없습니다", "rm "+filepath.Join(dir2, stopReqFile), "재기동 즉시 중지")
	if _, err := os.Stat(filepath.Join(dir2, stopReqFile)); err != nil {
		t.Errorf("stop.req 를 지우면 안 됩니다: %v", err)
	}
}

func TestDoctorFinished(t *testing.T) {
	x := newDocTest(t)
	hf, dir := x.state(func(s *runState) {
		s.Stage = stageFinish
		s.Busy = false
		s.Detail = "완료"
		s.CheckOff = true
		for i := range s.Hosts {
			s.Hosts[i].Check, s.Hosts[i].CPU, s.Hosts[i].Stress = "-", "O", "O"
		}
	})
	x.alive = false
	os.WriteFile(filepath.Join(dir, resultFile), []byte("x"), 0o644)
	r, text := x.run(hf)
	mustContain(t, text, "작업이 끝났습니다(완료)", "result.txt")
	mustNotContain(t, text, "워커가 죽어")
	if len(r.Problems) != 0 {
		t.Errorf("끝난 작업은 문제 없어야 함: %v", r.Problems)
	}
}

func TestDoctorFinishedWithNotOK(t *testing.T) {
	x := newDocTest(t)
	hf, _ := x.state(func(s *runState) {
		s.Stage = stageFinish
		s.Busy = false
		s.Detail = "완료(세 열이 모두 O 가 아닌 호스트 1대)"
	})
	_, text := x.run(hf)
	mustContain(t, text, "ev02", "설정체크 X", "CPU NOTRUN", "세 열이 모두 O 가 아닌 호스트가 1대")
}

func TestDoctorCPUTimeline(t *testing.T) {
	// 마감이 지났는데 감시가 안 끝남(워커 살아 있음)
	x := newDocTest(t)
	hf, _ := x.state(func(s *runState) {
		s.Stage = stageLoad
		s.CPUList = []string{"ev01"}
		s.CPULaunch = docNow.Add(-90 * time.Minute)
		s.CheckStart = docNow.Add(-43 * time.Minute)
		s.Deadline = docNow.Add(-13 * time.Minute)
		s.NextCheck = docNow.Add(-13 * time.Minute)
		s.Hosts[0].Rebooted, s.Hosts[1].NoResp = true, true
	})
	x.alive = true
	r, text := x.run(hf)
	mustContain(t, text, "CPU 시간표: launch", "마감", "CPU 완료 확인 마감(", "지났는데 감시가 끝나지 않았습니다",
		"CPU 테스트 중 재부팅된 호스트 1대", "CPU 테스트 중 응답 없는 호스트 1대", prgRebooted, prgNoResp)
	if len(r.Problems) < 3 {
		t.Errorf("문제 3건 이상 기대: %v", r.Problems)
	}
	// 정상 진행: 다음 확인이 미래
	x2 := newDocTest(t)
	hf2, _ := x2.state(func(s *runState) {
		s.Stage = stageLoad
		s.CPUList = []string{"ev01"}
		s.CPULaunch = docNow.Add(-10 * time.Minute)
		s.CheckStart = docNow.Add(37 * time.Minute)
		s.Deadline = docNow.Add(67 * time.Minute)
		s.NextCheck = docNow.Add(2 * time.Minute)
	})
	x2.alive = true
	_, text2 := x2.run(hf2)
	mustContain(t, text2, "[O] CPU 감시 진행 중 — 다음 확인 2분 0초 후")
}

func TestDoctorLaunchNotSeen(t *testing.T) {
	x := newDocTest(t)
	hf, dir := x.state(func(s *runState) {
		s.Stage = stageLoad
		s.Detail = "CPU 등록·launch 중 (1대)"
		s.CPUList = []string{"ev01"}
		s.Updated = docNow.Add(-30 * time.Minute)
	})
	x.alive = true
	os.WriteFile(filepath.Join(dir, workerLog), []byte("2026-11-05 11:30:00 부하 단계 시작: CPU 1대 [ev01], stress-ng 0대 []\n+ (cd /opt/st && /opt/st/stress-test -user tester)\n등록 중...\n"), 0o644)
	_, text := x.run(hf)
	mustContain(t, text, "launch 시각 아직 없음", "CPU 등록·launch 가 30분 0초 동안 끝나지 않았습니다", "worker.log 에 stress-test 의 [감시] 줄이 없습니다")
}

func TestDoctorLastErrorAndLogRules(t *testing.T) {
	x := newDocTest(t)
	hf, dir := x.state(func(s *runState) { s.LastError = "CPU stress-test 시간초과" })
	x.alive = true
	os.WriteFile(filepath.Join(dir, workerLog), []byte("a\n2026-11-05 11:00:00 판정 실패: gossh 실행 실패\nb\n"), 0o644)
	_, text := x.run(hf)
	mustContain(t, text, "[X] last_error: CPU stress-test 시간초과", "GOSSH_TIMEOUT", "판정(gossh) 실패가 있었습니다", "판정 실패: gossh 실행 실패")
}

func TestDoctorBrokenStateJSON(t *testing.T) {
	x := newDocTest(t)
	hf, dir := x.state(nil)
	os.WriteFile(filepath.Join(dir, stateFile), []byte("{broken"), 0o644)
	r, text := x.run(hf)
	mustContain(t, text, "state.json 을 읽을 수 없습니다", "-new")
	if len(r.Problems) == 0 {
		t.Error("문제 기대")
	}
}

func TestDoctorStateMismatchesAndHostFile(t *testing.T) {
	x := newDocTest(t)
	hf, _ := x.state(func(s *runState) { s.WorkDir = "/other/place" })
	x.write("hosts", "ev01\nev03\n", 0o644)
	x.alive = true
	_, text := x.run(hf)
	mustContain(t, text, "작업을 시작한 경로(/other/place)와 다릅니다", "파일에만 있음 ev03", "작업에만 있음 ev02")
}

// 상태 폴더의 모든 파일은 진단 전후로 같아야 한다(읽기 전용).
func TestDoctorReadOnly(t *testing.T) {
	x := newDocTest(t)
	hf, dir := x.state(func(s *runState) {
		s.Question = &question{ID: "q1", Stage: 2, Text: "t", Asked: docNow, Choices: []choice{{"c", "계속"}}}
	})
	writeAnswer(dir, "q1", "c")
	os.WriteFile(filepath.Join(dir, stopReqFile), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, workerLog), []byte("l\n"), 0o644)
	snap := func() string {
		var l []string
		filepath.Walk(x.cwd, func(p string, fi os.FileInfo, err error) error {
			if err == nil && !fi.IsDir() {
				b, _ := os.ReadFile(p)
				l = append(l, fmt.Sprintf("%s %d %d %s", p, fi.Size(), fi.ModTime().UnixNano(), b))
			}
			return nil
		})
		sort.Strings(l)
		return strings.Join(l, "\n")
	}
	before := snap()
	x.alive = true
	x.run(hf)
	r := &docReport{}
	docEnv(r, doctorArgs{hostFile: hf}, x.d)
	if after := snap(); before != after {
		t.Errorf("진단이 파일을 바꿨습니다:\n전: %s\n후: %s", before, after)
	}
}

// ---------------------------------------------------------------------------
// 환경 점검
// ---------------------------------------------------------------------------

func TestDoctorEnvAllGood(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("실행 권한 검사는 리눅스에서")
	}
	x := newDocTest(t)
	gossh := x.write("tools/gossh", "#!/bin/sh\n", 0o755)
	script := x.write("tools/config_check.sh", "case $1 in\n -auto-check) ;;\n -auto) ;;\nesac\n", 0o644)
	st := x.write("st/stress-test", "#!/bin/sh\n", 0o755)
	x.write("st/conf/tester_setting.conf", "history_file = /var/log/st_hist.log # 이력\n", 0o644)
	route := filepath.Join(x.cwd, "route")
	x.write("route/info_mn.sh", "", 0o755)
	x.write("route/info.sh", "", 0o755)
	conf := "GOSSH=" + gossh + "\nOS_CHECK_SH=" + script + "\nSTRESS_TEST=" + st + "\nUSER_ROUTE=" + route + "\nERROR_PATTERN=error x\n"
	x.d.loadConf = func(string) (config, string, error) { return parseConf(conf), "/etc/new_setup.conf", nil }
	x.d.lookPath = func(n string) (string, error) { return "/bin/" + n, nil }
	r := &docReport{}
	env := docEnv(r, doctorArgs{user: "tester"}, x.d)
	text := r.b.String()
	if len(r.Problems) != 0 {
		t.Fatalf("문제 없어야 함: %v\n%s", r.Problems, text)
	}
	mustContain(t, text, "[O] 설정 파일: /etc/new_setup.conf", "[O] gossh(설정 GOSSH)", "-w -pm -script 지원", "-auto-check 지원", "-auto 지원",
		"[O] STRESS_TEST", "[O] stress-test conf:", "user=tester", "[O] USER_ROUTE", "[O] 현재 경로 쓰기 가능", "[O] 설정 문구에 gossh 위험어 없음")
	if env.history != "/var/log/st_hist.log" {
		t.Errorf("history_file: %q", env.history)
	}
	if env.gossh != gossh {
		t.Errorf("gossh: %q", env.gossh)
	}
	out, code := r.finish()
	if code != 0 || !strings.Contains(out, "문제가 발견되지 않았습니다") {
		t.Errorf("종료코드 0 기대: %d", code)
	}
}

func TestDoctorEnvProblems(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("실행 권한 검사는 리눅스에서")
	}
	x := newDocTest(t)
	gossh := x.write("tools/gossh", "#!/bin/sh\n", 0o644) // 실행 권한 없음
	script := x.write("tools/config_check.sh", "echo old\n", 0o644)
	st := x.write("st/stress-test", "x", 0o755) // conf 없음
	conf := "GOSSH=" + gossh + "\nOS_CHECK_SH=" + script + "\nSTRESS_TEST=" + st + "\nUSER_ROUTE=" + x.cwd + "/nope\nCPU_WAIT_MAX=abc\n" +
		"DISK_PATTERN=reboot now\nERROR_PATTERN=bad!\nCPU_AMD_LOG=\n"
	x.d.loadConf = func(string) (config, string, error) { return parseConf(conf), "", nil }
	r := &docReport{}
	docEnv(r, doctorArgs{user: "tester"}, x.d)
	text := r.b.String()
	mustContain(t, text, "설정 파일 없음 → 기본값", "설정 경고: 설정 CPU_WAIT_MAX", "실행 권한이 없습니다", "-auto-check 를 지원하지 않습니다(구버전)",
		"-auto 인자를 찾지 못했습니다", "bash 를 찾을 수 없습니다", "stress-test conf(tester_setting.conf)를 찾지 못했습니다", "USER_ROUTE 스크립트가 없습니다",
		"gossh 위험어가 있습니다", "'!' 가 있습니다", "CPU_AMD_LOG 가 비어 있어")
	out, code := r.finish()
	if code != 1 || !strings.Contains(out, "== 요약 ==") || !strings.Contains(out, "-> 해결:") {
		t.Errorf("종료코드 1 과 요약 기대: %d\n%s", code, out)
	}
}

func TestDoctorEnvGosshFallbackAndHelp(t *testing.T) {
	x := newDocTest(t)
	// GOSSH 비움 + PATH 에도 없음
	r := &docReport{}
	if g := docGossh(r, parseConf(""), x.d); g != "" {
		t.Errorf("gossh 없음 기대: %q", g)
	}
	mustContain(t, r.b.String(), "gossh 를 찾을 수 없습니다")
	// PATH 에서 찾음 + 도움말에 필요한 옵션 없음
	p := x.write("path/gossh", "x", 0o755)
	x.d.lookPath = func(string) (string, error) { return p, nil }
	x.d.gosshHelp = func(string) (string, error) { return "Usage of other:\n  -x\n", nil }
	r2 := &docReport{}
	docGossh(r2, parseConf(""), x.d)
	mustContain(t, r2.b.String(), "[!] gossh(PATH) 도움말에 필요한 옵션이 없습니다: -w -pm -script")
	// 실행 실패
	x.d.gosshHelp = func(string) (string, error) { return "", fmt.Errorf("exec format error") }
	r3 := &docReport{}
	if g := docGossh(r3, parseConf(""), x.d); g != "" {
		t.Errorf("실행 실패면 빈 값: %q", g)
	}
	mustContain(t, r3.b.String(), "gossh -h", "exec format error")
}

func TestDoctorEnvConfError(t *testing.T) {
	x := newDocTest(t)
	x.d.loadConf = func(string) (config, string, error) {
		return config{}, "", fmt.Errorf("설정 파일을 읽을 수 없습니다: x")
	}
	r := &docReport{}
	docEnv(r, doctorArgs{conf: "x"}, x.d)
	mustContain(t, r.b.String(), "[X] 설정 파일 오류", "기본값으로 계속")
}

func TestDoctorEnvUnwritableCwd(t *testing.T) {
	x := newDocTest(t)
	x.d.cwd = filepath.Join(x.cwd, "no", "such")
	r := &docReport{}
	docEnv(r, doctorArgs{}, x.d)
	mustContain(t, r.b.String(), "현재 경로에 쓸 수 없습니다")
}

func TestStressHistoryPath(t *testing.T) {
	if p := stressHistoryPath("a=b\n", "/opt/st"); p != "/opt/st/stress-test_history.log" {
		t.Errorf("기본값: %q", p)
	}
	if p := stressHistoryPath("history_file = logs/h.log\n", "/opt/st"); p != "/opt/st/logs/h.log" {
		t.Errorf("상대: %q", p)
	}
	if p := stressHistoryPath("# history_file = x\nhistory_file=\"/v/h.log\" # c\n", "/opt/st"); p != "/v/h.log" {
		t.Errorf("절대·따옴표: %q", p)
	}
}

func TestDoctorHistory(t *testing.T) {
	x := newDocTest(t)
	hf, _ := x.state(nil)
	hist := x.write("hist.log", strings.Join([]string{
		"2026-11-05T10:00:00Z user=other action=start",
		"2026-11-05T11:30:00Z user=tester action=start hosts=h steps=a count=2",
		"2026-11-05T11:40:00Z user=tester action=launch stage=a status=fail error=\"x\"",
		"2026-11-05T10:41:00Z user=testerX action=start",
	}, "\n")+"\n", 0o644)
	r := &docReport{}
	docRun(r, doctorArgs{hostFile: hf}, x.d, &docEnvInfo{conf: parseConf(""), history: hist})
	text := r.b.String()
	mustContain(t, text, "user=tester 마지막 10줄", "action=start hosts=h", "launch 실패 기록이 1건")
	mustNotContain(t, text, "user=other", "user=testerX")
}

func TestMaskSecrets(t *testing.T) {
	in := "PASSWORD=hunter2 api_key: abc123 --password s3cret token='a b' ok=1"
	out := maskSecrets(in)
	mustNotContain(t, out, "hunter2", "abc123", "s3cret", "a b")
	mustContain(t, out, "PASSWORD=****", "ok=1")
}

func TestValidateDoctorArgs(t *testing.T) {
	cases := []struct {
		a  doctorArgs
		n  int
		ok bool
	}{
		{doctorArgs{}, 0, true},
		{doctorArgs{hostFile: "h"}, 1, true},
		{doctorArgs{host: "10.0.0.1"}, 0, true},
		{doctorArgs{host: "ev01", check: true}, 0, true},
		{doctorArgs{host: "ev01", hostFile: "h"}, 1, false},
		{doctorArgs{host: "ev[01-02]"}, 0, false},
		{doctorArgs{host: "a b"}, 0, false},
		{doctorArgs{host: "a;rm"}, 0, false},
		{doctorArgs{check: true}, 0, false},
		{doctorArgs{hostFile: "a"}, 2, false},
	}
	for _, c := range cases {
		if err := validateDoctorArgs(c.a, c.n); (err == nil) != c.ok {
			t.Errorf("%+v n=%d: err=%v", c.a, c.n, err)
		}
	}
	if !hasDoctorFlag([]string{"-doctor", "x"}) || hasDoctorFlag([]string{"-run", "x"}) {
		t.Error("hasDoctorFlag")
	}
}

// ---------------------------------------------------------------------------
// 호스트 단건 점검(가짜 gossh 출력 주입)
// ---------------------------------------------------------------------------

func hostTest(t *testing.T, check bool) (*docTest, *docReport, *docEnvInfo) {
	t.Helper()
	x := newDocTest(t)
	t.Setenv("TMPDIR", t.TempDir()) // 단건 점검의 임시 폴더가 여기에 생기고 끝나면 비어야 한다
	script := x.write("config_check.sh", "# -auto-check\n", 0o644)
	conf := parseConf("OS_CHECK_SH=" + script + "\n")
	return x, &docReport{}, &docEnvInfo{conf: conf, gossh: "/fake/gossh"}
}

const fakePass1 = "ev01: vendor_id\t: GenuineIntel\nev01: NS_CPU_INTEL_NOLOG\nev01: Thread(s) per core:    2\nev01: NS_KERNEL 4.18.0-553.el8.x86_64\n"

func TestDoctorHostOK(t *testing.T) {
	x, r, env := hostTest(t, false)
	x.gossh = func(args []string) (string, string) {
		cmd := args[len(args)-1]
		switch {
		case strings.Contains(cmd, "vendor_id"):
			return fakePass1, "ev01: ...\n" + fakePass1
		case strings.Contains(cmd, "btime"):
			o := "ev01: btime 1793000000\nev01:  11:59:00 up 1 day,  3 users,  load average: 0.10\nev01: NS_MISSING /var/cpudiag/result.log\nev01: NS_MISSING /var/cpudiag/results.json\n"
			return o, o
		}
		return "ev01: NS_DONE\n", "ev01: NS_DONE\n"
	}
	docHost(r, doctorArgs{host: "ev01"}, x.d, env)
	text := r.b.String()
	mustContain(t, text, "== 3. 호스트 단건 점검: ev01 ==", "[1차] 정보수집", "-pm -script", "[2차] anaconda", "grep btime /proc/stat; ",
		"부팅 시각(btime 1793000000)", "uptime:", "없음: /var/cpudiag/result.log",
		"[O] 상태 = 완료", "CPU테스트 = NOTRUN", "벤더 GenuineIntel → Intel 로그 /var/cpudiag/result.log", "NS_CPU_INTEL_NOLOG(로그 파일 없음) 있음",
		"stress-ng = NOTRUN", "H/T = ON", "OS = 4.18.0-553.el8.x86_64",
		"bash "+filepath.Join(x.cwd, "config_check.sh")+" -auto-check new_setup targets.txt", "-check 를 주세요",
		header, "ev01|완료|-|NOTRUN|NOTRUN|ON|4.18.0-553.el8.x86_64|")
	if len(r.Problems) != 0 {
		t.Errorf("문제 없어야 함: %v", r.Problems)
	}
	if len(x.calls) != 3 {
		t.Errorf("gossh 3회 기대: %d", len(x.calls))
	}
	for _, c := range x.calls {
		if c[0] != "-w" || !strings.HasPrefix(c[1], os.TempDir()) {
			t.Errorf("임시 호스트 파일을 써야 함: %v", c)
		}
	}
	if ents, _ := os.ReadDir(os.TempDir()); len(ents) != 0 {
		t.Errorf("임시 폴더가 남았습니다: %v", ents)
	}
}

func TestDoctorHostCheckFlag(t *testing.T) {
	x, r, env := hostTest(t, true)
	x.gossh = func(args []string) (string, string) { return fakePass1, fakePass1 }
	x.checkV = map[string]string{"ev01": "X"}
	docHost(r, doctorArgs{host: "ev01", check: true}, x.d, env)
	text := r.b.String()
	mustContain(t, text, "설정체크 = X", "ev01|완료|X|")
	if !strings.Contains(levels(r), lvWarn) {
		t.Errorf("설정체크 X 는 주의: %v", r.Problems)
	}
}

func TestDoctorHostUnreachable(t *testing.T) {
	x, r, env := hostTest(t, false)
	x.gossh = func(args []string) (string, string) {
		os.WriteFile(args[1]+"_res_off", []byte("10.255.255.1\n"), 0o644)
		return "", "10.255.255.1: connect timeout\n"
	}
	docHost(r, doctorArgs{host: "10.255.255.1"}, x.d, env)
	text := r.b.String()
	mustContain(t, text, "_res_off = 접속 불가", "[X] 접속불가: gossh 가 이 호스트에 접속하지 못했습니다", "ping 10.255.255.1", "10.255.255.1|접속불가|-|-|-|-|-|")
	mustNotContain(t, text, "[2차]")
	if len(x.calls) != 1 {
		t.Errorf("접속불가면 gossh 1회만: %d", len(x.calls))
	}
}

func TestDoctorHostInstalling(t *testing.T) {
	x, r, env := hostTest(t, false)
	x.gossh = func(args []string) (string, string) {
		cmd := args[len(args)-1]
		if strings.Contains(cmd, "vendor_id") {
			os.WriteFile(args[1]+"_os_install", []byte("ev01\n"), 0o644)
			return "", ""
		}
		if strings.Contains(cmd, "btime") {
			return "", ""
		}
		return "ev01: NS_ERR\nev01: NS_DONE\n", "ev01: NS_ERR\nev01: NS_DONE\n"
	}
	docHost(r, doctorArgs{host: "ev01"}, x.d, env)
	text := r.b.String()
	mustContain(t, text, "상태 = 설치정지", "2차 표식: NS_ERR, NS_DONE", "ev01|설치정지|-|-|-|-|-|", "_os_install = OS설치중 후보")
}

func TestDoctorHostCshError(t *testing.T) {
	x, r, env := hostTest(t, false)
	x.gossh = func(args []string) (string, string) { return fakePass1, "ev01: Unmatched '.\n" + fakePass1 }
	docHost(r, doctorArgs{host: "ev01"}, x.d, env)
	mustContain(t, r.b.String(), "[X] 원격 셸 오류로 보이는 출력", "csh/tcsh")
}

func TestDoctorHostNoGossh(t *testing.T) {
	x := newDocTest(t)
	r := &docReport{}
	docHost(r, doctorArgs{host: "ev01"}, x.d, &docEnvInfo{conf: parseConf("")})
	mustContain(t, r.b.String(), "gossh 를 쓸 수 없어")
	if len(x.calls) != 0 {
		t.Error("gossh 호출 금지")
	}
}

func TestDoctorHostDangerousConfig(t *testing.T) {
	x := newDocTest(t)
	r := &docReport{}
	docHost(r, doctorArgs{host: "ev01"}, x.d, &docEnvInfo{conf: parseConf("DISK_PATTERN=halt\n"), gossh: "/g"})
	mustContain(t, r.b.String(), "위험어가 있어 보낼 수 없습니다")
	if len(x.calls) != 0 {
		t.Error("gossh 호출 금지")
	}
}

func TestExplainCPU(t *testing.T) {
	conf := parseConf("")
	s := explainCPU(&hostInfo{vendor: "AuthenticAMD", cpuAMD: true}, conf)
	mustContain(t, s, "CPU테스트 = O", "AuthenticAMD", "/var/cpudiag/results.json", "\"Result\" 가 든 줄이 1개 이상", "NS_CPU_AMD 있음")
	s = explainCPU(&hostInfo{}, conf)
	mustContain(t, s, "CPU테스트 = X", "벤더를 알 수 없음")
}

func TestDocExtraCmdSafe(t *testing.T) {
	c := docExtraCmd(parseConf("CPU_AMD_LOG=/var/cpudiag/res*.json\n"))
	if isDangerousCommand(c) {
		t.Errorf("위험어 포함: %s", c)
	}
	for _, bad := range []string{"{", "}", "!", "=$", " > ", "2>"} {
		if strings.Contains(c, bad) {
			t.Errorf("csh 에서 위험한 문자 %q: %s", bad, c)
		}
	}
	mustContain(t, c, "grep btime /proc/stat", "ls -l", "NS_MISSING", "ls -t")
	if !strings.HasSuffix(c, "uptime") {
		t.Errorf("작은따옴표로 끝나면 csh 에서 깨짐: %s", c)
	}
	if !strings.HasSuffix(c, "uptime") {
		t.Errorf("따옴표로 끝나면 csh 에서 깨짐: %s", c)
	}
}

func TestRunDoctorSummaryAndUserFromState(t *testing.T) {
	x := newDocTest(t)
	hf, _ := x.state(nil)
	x.alive = false
	out, code := runDoctor(doctorArgs{hostFile: hf}, x.d)
	if code != 1 {
		t.Errorf("종료코드 1 기대: %d", code)
	}
	mustContain(t, out, "new_setup 점검 보고서", "범례:", "== 1. 환경 점검 ==", "== 2. -run 작업 진단 ==", "== 요약 ==", "워커가 죽어 있습니다")
	// 환경만: 문제 없음이면 0 (gossh 가 PATH 에 없으면 문제이므로 PATH 에 있다고 가정)
	if runtime.GOOS != "windows" {
		g := x.write("g/gossh", "x", 0o755)
		x.d.lookPath = func(string) (string, error) { return g, nil }
		out2, code2 := runDoctor(doctorArgs{}, x.d)
		if code2 != 0 {
			t.Errorf("환경만 점검은 0 기대: %d\n%s", code2, out2)
		}
	}
}
