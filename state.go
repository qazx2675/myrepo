package main

// -run 상태 파일(./<호스트파일>_run/) 읽기·쓰기.
// 워커가 state.json 을 쓰고 TUI 가 읽는다. TUI 는 answer.json 에 답을 쓰고 워커가 읽은 뒤 지운다.
// 두 파일 모두 임시 파일 → rename 으로 원자적으로 기록한다.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 상태 폴더 안의 파일 이름
const (
	stateFile  = "state.json"
	answerFile = "answer.json"
	pidFile    = "worker.pid"
	lockFile   = "lock"
	workerLog  = "worker.log"
	listsDir   = "lists"
	resultFile = "result.txt"
)

// 단계 번호(state.json 의 stage)
const (
	stageJudge  = 1 // 판정
	stageFix    = 2 // 설정수정
	stageLoad   = 3 // 부하(CPU + stress-ng 동시)
	stageNG     = 4 // stress-ng(이번 CPU 대상)
	stageFinish = 5 // 완료
)

// stageNames 는 단계 번호 → 이름(인덱스 0 은 비움).
var stageNames = []string{"", "판정", "설정수정", "부하", "stress-ng", "완료"}

func stageName(n int) string {
	if n > 0 && n < len(stageNames) {
		return stageNames[n]
	}
	return "-"
}

// 질문 선택지
const (
	ansCont    = "c" // 계속(다음 단계)
	ansRetry   = "r" // 재시도(같은 단계 다시)
	ansExclude = "x" // O 아닌 호스트 제외하고 계속
	ansStop    = "s" // 작업 중지
)

// 진행 비고 문구
const (
	prgExcluded = "제외"
	prgRebooted = "실행도중 재부팅됨"
	prgNoResp   = "실행도중 응답없음"
	prgCPURun   = "CPU 실행중"
	prgNGRun    = "stress-ng 실행중"
)

// choice 는 질문 선택지 하나(키와 화면용 설명).
type choice struct {
	Key   string `json:"key"`   // c / r / x / s
	Label string `json:"label"` // 예: "계속(설정수정)"
}

// question 은 워커가 사용자 답을 기다리는 질문. 답은 answer.json 으로 받는다.
type question struct {
	ID      string    `json:"id"`      // 질문마다 새 값(q1, q2 …). 답의 id 가 같아야 받아들인다
	Stage   int       `json:"stage"`   // 질문을 낸 단계
	Text    string    `json:"text"`    // 결과 요약 + 묻는 말(여러 줄 가능)
	Choices []choice  `json:"choices"` // 고를 수 있는 것
	Asked   time.Time `json:"asked"`   // 질문 시각
}

// answer 는 answer.json 내용.
type answer struct {
	ID     string `json:"id"`     // question.id
	Choice string `json:"choice"` // c / r / x / s
}

// hostRow 는 호스트 한 대의 표 행. 앞 7개는 판정 결과(hostResult) 그대로.
type hostRow struct {
	Host   string `json:"host"`
	Status string `json:"status"` // 완료 / 디스크미인식 / 설치정지 / OS설치중 / 접속불가
	Check  string `json:"check"`  // 설정체크 O / X / 출력없음 / -
	CPU    string `json:"cpu"`    // O / X / NOTRUN / -
	Stress string `json:"stress"` // O / NOTRUN / -
	HT     string `json:"ht"`
	OS     string `json:"os"`

	Note     string `json:"note"`     // 장애 목록(error_list)의 사유
	Progress string `json:"progress"` // 진행 비고(아래 값들을 ", " 로 이은 화면용 문자열, 저장할 때 다시 만든다)

	Excluded  bool   `json:"excluded"`        // 제외(이후 단계에서 빠짐)
	CPUTarget bool   `json:"cpu_target"`      // 부하 단계에서 CPU 테스트를 돌린 적 있음(stress-ng 단계 대상 후보)
	Rebooted  bool   `json:"rebooted"`        // CPU 테스트 중 btime 이 바뀜
	NoResp    bool   `json:"no_resp"`         // CPU 테스트 감시 중 마지막 확인에 응답 없음
	Running   string `json:"running"`         // 진행 중 작업: "CPU 실행중" / "stress-ng 실행중" / ""
	Btime     int64  `json:"btime,omitempty"` // CPU launch 때 기록한 부팅 시각(/proc/stat btime)
}

// progressText 는 진행 비고 화면 문자열을 만든다.
func (r *hostRow) progressText() string {
	var p []string
	if r.Excluded {
		p = append(p, prgExcluded)
	}
	if r.Rebooted {
		p = append(p, prgRebooted)
	}
	if r.NoResp {
		p = append(p, prgNoResp)
	}
	if r.Running != "" {
		p = append(p, r.Running)
	}
	return strings.Join(p, ", ")
}

// noteText 는 결과표 비고 열(장애 목록 사유 + 진행 비고).
func (r *hostRow) noteText() string {
	var p []string
	for _, s := range []string{r.Note, r.progressText()} {
		if s != "" {
			p = append(p, s)
		}
	}
	return strings.Join(p, ", ")
}

// runState 는 state.json 내용. 워커만 쓰고 TUI 는 읽기만 한다.
type runState struct {
	Version int `json:"version"`

	// 작업 정보(-run 이 만들 때 정함)
	HostArg  string    `json:"host_arg"`  // 사용자가 준 호스트파일 인자 그대로('^' 접두 가능, gossh -w 로 넘김)
	HostFile string    `json:"host_file"` // 호스트파일 절대 경로('^' 뗀 것)
	WorkDir  string    `json:"work_dir"`  // -run 을 실행한 경로(워커는 여기로 이동해 판정한다)
	ConfPath string    `json:"conf_path"` // new_setup.conf 절대 경로(빈 값 = 기본값)
	ErrPath  string    `json:"er_path"`   // 장애 목록 절대 경로(빈 값 = 없음)
	User     string    `json:"user"`      // config_check -auto / stress-test -user 에 쓰는 사용자
	CheckOff bool      `json:"check_off"` // OS_CHECK_SH 가 비어 설정체크를 안 함(이때만 설정체크 '-' 가 통과)
	Created  time.Time `json:"created"`

	// 진행
	Stage   int       `json:"stage"`   // 1 판정 ~ 5 완료(중지면 멈춘 단계)
	Stopped bool      `json:"stopped"` // 사용자가 s(중지)를 고름 또는 워커가 더 진행할 수 없음
	Busy    bool      `json:"busy"`    // 외부 명령(판정·config_check·stress-test·감시) 진행 중
	Detail  string    `json:"detail"`  // 단계 진행 설명(한 줄)
	Hosts   []hostRow `json:"hosts"`   // 호스트파일 순서
	// 질문(없으면 null)
	Question *question `json:"question"`
	QSeq     int       `json:"q_seq"` // 질문 ID 일련번호

	// 부하 단계
	CPUList     []string  `json:"cpu_list"`      // 이번 부하 단계 CPU 대상
	StressList  []string  `json:"stress_list"`   // 이번 부하/stress-ng 단계 stress-ng 대상
	CPULaunch   time.Time `json:"cpu_launch"`    // CPU launch 시각(0 = 아직/해당 없음)
	NextCheck   time.Time `json:"next_check"`    // 다음 btime·응답(·CPU 완료) 확인 시각
	CheckStart  time.Time `json:"check_start"`   // CPU 완료 확인 시작 시각(launch + CPU_WAIT_START)
	Deadline    time.Time `json:"deadline"`      // CPU 완료 확인 마감(확인 시작 + CPU_WAIT_MAX)
	CPUWaitDone bool      `json:"cpu_wait_done"` // CPU 감시 끝남

	LastError string    `json:"last_error"` // 마지막 오류(없으면 빈 값)
	Updated   time.Time `json:"updated"`    // 마지막 기록 시각
	WorkerPID int       `json:"worker_pid"`
	Heartbeat time.Time `json:"heartbeat"` // 워커가 살아 있으면 몇 초마다 갱신(외부 명령 중에도)
}

const stateVersion = 1

// Finished 는 더 진행할 것이 없는지(완료 또는 중지).
func (s *runState) Finished() bool {
	return s.Stopped || s.Stage == stageFinish
}

// row 는 호스트 이름으로 행을 찾는다(없으면 nil).
func (s *runState) row(host string) *hostRow {
	for i := range s.Hosts {
		if s.Hosts[i].Host == host {
			return &s.Hosts[i]
		}
	}
	return nil
}

// stateDirFor 는 호스트파일 인자에 대한 상태 폴더(현재 경로의 <호스트파일 이름>_run, 절대 경로).
func stateDirFor(hostArg string) string {
	base := filepath.Base(strings.TrimPrefix(hostArg, "^")) + "_run"
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, base)
	}
	return base
}

// newRunState 는 새 작업 상태를 만든다(경로는 절대 경로로 바꿔 둔다). 저장은 하지 않는다.
func newRunState(hostArg, confPath, errPath, user string) (*runState, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	abs := func(p string) string {
		if p == "" {
			return ""
		}
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}
	return &runState{
		Version:  stateVersion,
		HostArg:  hostArg,
		HostFile: abs(strings.TrimPrefix(hostArg, "^")),
		WorkDir:  wd,
		ConfPath: abs(confPath),
		ErrPath:  abs(errPath),
		User:     user,
		Created:  time.Now(),
		Stage:    stageJudge,
	}, nil
}

// writeFileAtomic 은 같은 폴더의 임시 파일에 쓴 뒤 rename 한다.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// saveState 는 state.json 을 원자적으로 쓴다(진행 비고 문자열도 다시 만든다). 폴더가 없으면 만든다.
func saveState(dir string, s *runState) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i := range s.Hosts {
		s.Hosts[i].Progress = s.Hosts[i].progressText()
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, stateFile), append(b, '\n'))
}

// loadState 는 state.json 을 읽는다. 없으면 os.IsNotExist 로 구분되는 오류.
func loadState(dir string) (*runState, error) {
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil {
		return nil, err
	}
	var s runState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s 형식 오류: %v", stateFile, err)
	}
	return &s, nil
}

// writeAnswer 는 TUI 가 질문 id 에 대한 답을 answer.json 에 쓴다.
func writeAnswer(dir, id, ch string) error {
	b, _ := json.Marshal(answer{ID: id, Choice: ch})
	return writeFileAtomic(filepath.Join(dir, answerFile), append(b, '\n'))
}

// takeAnswer 는 answer.json 을 읽고 지운다. 없거나 깨졌으면 nil.
func takeAnswer(dir string) *answer {
	p := filepath.Join(dir, answerFile)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	os.Remove(p)
	var a answer
	if json.Unmarshal(b, &a) != nil {
		return nil
	}
	return &a
}

// writeResult 는 최종 결과표(header + 8열, 비고 = 장애 사유 + 진행 비고)를 result.txt 로 쓴다.
func writeResult(dir string, s *runState) error {
	return writeFileAtomic(filepath.Join(dir, resultFile), []byte(resultText(s)))
}

// stopReqFile 은 TUI 가 쓰는 중지 요청 파일. 워커가 보이면 실행 중인 외부 명령을 끝내고 중지한다.
const stopReqFile = "stop.req"

// writeStopReq 는 중지 요청 파일을 만든다.
func writeStopReq(dir string) error {
	return writeFileAtomic(filepath.Join(dir, stopReqFile), []byte(time.Now().Format(time.RFC3339)+"\n"))
}

// stopRequested 는 중지 요청 파일이 있는지.
func stopRequested(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, stopReqFile))
	return err == nil
}

// resultText 는 최종 결과표(header + 8열)를 문자열로 만든다.
func resultText(s *runState) string {
	var b strings.Builder
	b.WriteString(header + "\n")
	for i := range s.Hosts {
		r := &s.Hosts[i]
		hr := hostResult{Host: r.Host, Status: r.Status, Check: r.Check, CPU: r.CPU, Stress: r.Stress, HT: r.HT, OS: r.OS}
		b.WriteString(hr.row(strings.ReplaceAll(r.noteText(), sep, "/")) + "\n")
	}
	return b.String()
}

// runDir 는 상태에 기록된 작업 경로로 상태 폴더를 구한다(stateDirFor 와 같은 값).
func (s *runState) runDir() string {
	return filepath.Join(s.WorkDir, filepath.Base(strings.TrimPrefix(s.HostArg, "^"))+"_run")
}
