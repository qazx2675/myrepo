// state.go - Job/Host 상태, jobs/*.json 원자 저장·복원, queue(.job) 파싱, codes 관리
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Host struct {
	IP        string `json:"ip"`
	Route     string `json:"route"` // "" (미판별) | "local" | "os6"
	SeenDown  bool   `json:"seen_down"`
	ReadyAt   int64  `json:"ready_at"`
	Processed string `json:"processed"` // 처리된 run 의 code, 비면 미처리
	Miss      int    `json:"miss"`
	Fails     int    `json:"fails"` // os_check 비정상 종료 연속 횟수
}

type Run struct {
	Code  string   `json:"code"`
	Hosts []string `json:"hosts"`
	At    int64    `json:"at"`
}

type Job struct {
	ID             string           `json:"id"`
	User           string           `json:"user"`
	Submitted      int64            `json:"submitted"`
	FirstReady     int64            `json:"first_ready"`
	LateFirstReady int64            `json:"late_first_ready"`
	FirstRunDone   bool             `json:"first_run_done"`
	Hosts          map[string]*Host `json:"hosts"`
	Runs           []Run            `json:"runs"`
}

// ---- 경로 ----

func dataDir() string {
	if d := os.Getenv("AUTO_SETUP_DIR"); d != "" {
		return d
	}
	return "/tmp/auto_setup"
}

func queueDir() string { return filepath.Join(dataDir(), "queue") }
func jobsDir() string  { return filepath.Join(dataDir(), "jobs") }
func doneDir() string  { return filepath.Join(dataDir(), "jobs", "done") }
func codesDir() string { return filepath.Join(dataDir(), "codes") }
func runsDir() string  { return filepath.Join(dataDir(), "runs") }
func binDir() string   { return filepath.Join(dataDir(), "bin") }
func logPath() string  { return filepath.Join(dataDir(), "auto_setup.log") }
func lockPath() string { return filepath.Join(dataDir(), "auto_setup.lock") }

func ensureDirs() error {
	for _, d := range []string{queueDir(), jobsDir(), doneDir(), codesDir(), runsDir(), binDir()} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

// ---- jobs ----

// saveJob: tmp → rename 원자 저장. 내용이 디스크와 같으면 쓰지 않고 false.
func saveJob(j *Job) (bool, error) {
	b, err := json.MarshalIndent(j, "", " ")
	if err != nil {
		return false, err
	}
	b = append(b, '\n')
	p := filepath.Join(jobsDir(), j.ID+".json")
	if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, b) {
		return false, nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// loadJobs: jobs/*.json 전부 복원 (전달시각 순). 깨진 파일은 로그만 남기고 건너뜀.
func loadJobs() ([]*Job, error) {
	ents, err := os.ReadDir(jobsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var jobs []*Job
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(jobsDir(), e.Name()))
		if err != nil {
			continue
		}
		j := &Job{}
		if err := json.Unmarshal(b, j); err != nil || j.ID == "" {
			logf("[X] job 파일 해석 실패: %s", e.Name())
			continue
		}
		if j.Hosts == nil {
			j.Hosts = map[string]*Host{}
		}
		jobs = append(jobs, j)
	}
	sort.Slice(jobs, func(a, b int) bool {
		if jobs[a].Submitted != jobs[b].Submitted {
			return jobs[a].Submitted < jobs[b].Submitted
		}
		return jobs[a].ID < jobs[b].ID
	})
	return jobs, nil
}

func validID(id string) bool {
	return id != "" && !strings.ContainsAny(id, "/\\") && id != "." && id != ".." && id != "done"
}

// archiveJob: jobs/<id>.json → jobs/done/
func archiveJob(id string) error {
	if !validID(id) {
		return fmt.Errorf("잘못된 job id: %s", id)
	}
	src := filepath.Join(jobsDir(), id+".json")
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("job 없음: %s", id)
	}
	if err := os.MkdirAll(doneDir(), 0755); err != nil {
		return err
	}
	return os.Rename(src, filepath.Join(doneDir(), id+".json"))
}

func cancelJob(id string) error { return archiveJob(id) }

func newJobID(user string, t int64) string {
	base := time.Unix(t, 0).Format("20060102-150405") + "-" + user
	id := base
	for n := 2; ; n++ {
		_, e1 := os.Stat(filepath.Join(jobsDir(), id+".json"))
		_, e2 := os.Stat(filepath.Join(doneDir(), id+".json"))
		if e1 != nil && e2 != nil {
			return id
		}
		id = base + "-" + strconv.Itoa(n)
	}
}

// hostState: 상태 표시용 분류
func hostState(h *Host) string {
	switch {
	case h.Processed != "":
		return "처리완료"
	case h.Fails >= 3:
		return "실패"
	case h.ReadyAt > 0:
		return "READY"
	case h.SeenDown:
		return "설치중"
	}
	return "대기"
}

func formatJobStatus(j *Job) string {
	cnt := map[string]int{}
	var remain []string
	for name, h := range j.Hosts {
		s := hostState(h)
		cnt[s]++
		if s != "처리완료" && s != "실패" {
			remain = append(remain, name)
		}
	}
	sort.Strings(remain)
	var sb strings.Builder
	fmt.Fprintf(&sb, "job %s  user=%s  전달=%s  (%d대, run %d회)\n", j.ID, j.User,
		time.Unix(j.Submitted, 0).Format("2006-01-02 15:04:05"), len(j.Hosts), len(j.Runs))
	fmt.Fprintf(&sb, "  대기 %d / 설치중 %d / READY %d / 처리완료 %d / 실패 %d\n",
		cnt["대기"], cnt["설치중"], cnt["READY"], cnt["처리완료"], cnt["실패"])
	if len(remain) > 0 {
		fmt.Fprintf(&sb, "  남은 호스트: %s\n", strings.Join(remain, " "))
	}
	return sb.String()
}

// ---- queue (.job) ----

var jobFileRe = regexp.MustCompile(`^(\d+)_(.+)_(\d+)\.job$`)
var userRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// parseJobFile: user=, time=, 나머지 줄은 호스트(첫 필드). 중복 호스트는 제거.
func parseJobFile(b []byte) (user string, t int64, hosts []string, err error) {
	seen := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "user=") {
			user = strings.TrimSpace(line[len("user="):])
			continue
		}
		if strings.HasPrefix(line, "time=") {
			t, _ = strconv.ParseInt(strings.TrimSpace(line[len("time="):]), 10, 64)
			continue
		}
		h := strings.Fields(line)[0]
		if !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	if !userRe.MatchString(user) {
		return "", 0, nil, errors.New("user 값이 없거나 올바르지 않음")
	}
	if len(hosts) == 0 {
		return "", 0, nil, errors.New("호스트가 없음")
	}
	return user, t, hosts, nil
}

// ---- codes ----

func validCode(code string) bool {
	if len(code) != 4 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func codePath(code string) string { return filepath.Join(codesDir(), code+".txt") }

func readCode(code string) ([]byte, error) {
	if !validCode(code) {
		return nil, errors.New("code 형식 오류")
	}
	return os.ReadFile(codePath(code))
}

// newCode: codes/ 에 없는 4자리 code (0000~9999)
func newCode() (string, error) {
	for i := 0; i < 10000; i++ {
		c := fmt.Sprintf("%04d", rand.Intn(10000))
		_, errRun := os.Stat(filepath.Join(runsDir(), c)) // 중단된 run 폴더(옛 check.res_*) 재사용 방지
		if _, err := os.Stat(codePath(c)); os.IsNotExist(err) && os.IsNotExist(errRun) {
			return c, nil
		}
	}
	return "", errors.New("사용 가능한 code 없음")
}
