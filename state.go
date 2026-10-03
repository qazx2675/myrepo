// state.go - Job/Host 상태(2차: stage·그룹·LDAP·2차·완료출처), jobs/*.json 원자 저장·복원, queue(.job) 파싱(그룹 줄), codes 관리
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
	Processed string `json:"processed"` // 처리된 run 의 code (완료기록 인정이면 "external"/"manual"), 비면 미처리
	Miss      int    `json:"miss"`
	Fails     int    `json:"fails"` // os_check 비정상 종료 연속 횟수

	// 2차 (§14-2·3·4) — 모두 omitempty, 구버전 JSON 과 양방향 호환
	Stage   string     `json:"stage,omitempty"`    // Stage 값 (model.go), 데몬이 상태 변화 때만 갱신
	StageAt int64      `json:"stage_at,omitempty"` // 현재 단계 진입 절대시각(epoch)
	DownAt  int64      `json:"down_at,omitempty"`  // seen_down 된 시각 (READY 해제 재부팅 시 갱신) — 정체 판정 기준
	BootAt  int64      `json:"boot_at,omitempty"`  // 준비확인 uptime 으로 계산한 마지막 부팅 시각(= 확인시각 − uptime)
	Ldap    *LdapState `json:"ldap,omitempty"`     // LDAP 백업/비교 상태 (nil = 미수집)
	Second  string     `json:"second,omitempty"`   // 2차 체크: "" | running | ok | fail
	DoneSrc string     `json:"done_src,omitempty"` // 완료 출처: run | external | manual
}

// LdapState: 호스트별 LDAP 백업·비교 상태. bindpw 값 자체는 어디에도 저장·출력하지 않는다.
type LdapState struct {
	Backup  string `json:"backup"`           // "none" | "ok"
	Bindpw  string `json:"bindpw,omitempty"` // "" (미비교) | "same" | "diff" | "na"
	Applied bool   `json:"applied"`          // diff 일 때 백업본 적용 성공
	Reason  string `json:"reason,omitempty"` // 사유 (백업 없음·적용 실패 등, 비밀값 금지)
}

const (
	LdapBackupOK   = "ok"
	LdapBackupNone = "none"
	BindpwSame     = "same"
	BindpwDiff     = "diff"
	BindpwNA       = "na"

	SecondRunning = "running"
	SecondOK      = "ok"
	SecondFail    = "fail"

	DoneSrcRun      = "run"
	DoneSrcExternal = "external"
	DoneSrcManual   = "manual"
)

type Run struct {
	Code   string   `json:"code"`
	Hosts  []string `json:"hosts"`
	At     int64    `json:"at"`
	Manual bool     `json:"manual,omitempty"` // 수동 run (requests manual-run)
	Yml    string   `json:"yml,omitempty"`    // 수동 run 의 그룹
}

// Group: (all).yaml 을 구성하는 yml 그룹 1개 (.job 의 그룹 줄)
type Group struct {
	Infra  string   `json:"infra"`
	OS     string   `json:"os"`
	Boot   string   `json:"boot"`
	Splunk string   `json:"splunk"`
	Hosts  []string `json:"hosts"`
}

// 그룹 줄이 없는 구버전 .job 의 단일 그룹 이름 / 어느 그룹에도 없는 호스트를 모으는 그룹 이름
const (
	allGroup = "(all)"
	etcGroup = "(기타)"
)

type Job struct {
	ID             string            `json:"id"`
	User           string            `json:"user"`
	Submitted      int64             `json:"submitted"`
	FirstReady     int64             `json:"first_ready"`
	LateFirstReady int64             `json:"late_first_ready"`
	FirstRunDone   bool              `json:"first_run_done"`
	Hosts          map[string]*Host  `json:"hosts"`
	Runs           []Run             `json:"runs"`
	Groups         map[string]*Group `json:"groups,omitempty"` // 키 = yml 파일명
	AllYml         string            `json:"all_yml,omitempty"`
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
	for _, d := range []string{queueDir(), jobsDir(), doneDir(), codesDir(), runsDir(), binDir(), doneRecDir(), requestsDir()} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

// ---- jobs ----

// saveJob: tmp → rename 원자 저장. 내용이 디스크와 같으면 쓰지 않고 false.
func saveJob(j *Job) (bool, error) {
	return writeJobFile(filepath.Join(jobsDir(), j.ID+".json"), j)
}

// loadJobFile: job JSON 1개 읽기 (jobs/done/ 의 종료된 job 에도 사용)
func loadJobFile(p string) (*Job, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	j := &Job{}
	if err := json.Unmarshal(b, j); err != nil {
		return nil, err
	}
	if j.ID == "" {
		return nil, errors.New("id 없음")
	}
	if j.Hosts == nil {
		j.Hosts = map[string]*Host{}
	}
	return j, nil
}

// writeJobFile: 경로 p 에 tmp → rename 원자 저장. 내용이 같으면 쓰지 않고 false.
func writeJobFile(p string, j *Job) (bool, error) {
	b, err := json.MarshalIndent(j, "", " ")
	if err != nil {
		return false, err
	}
	b = append(b, '\n')
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

// jobSpec: .job 파일 해석 결과
type jobSpec struct {
	User   string
	Time   int64
	Hosts  []string          // 호스트 줄 순서 + 그룹 줄에만 있는 호스트(뒤에), 중복 제거
	Groups map[string]*Group // 그룹 줄이 없으면 {"(all)": 전체 호스트}
	AllYml string
}

// parseJobFile: user=, time=, 나머지 줄은 호스트(첫 필드). 중복 호스트는 제거. (1차 시그니처 유지)
func parseJobFile(b []byte) (user string, t int64, hosts []string, err error) {
	s, err := parseJobSpec(b)
	if err != nil {
		return "", 0, nil, err
	}
	return s.User, s.Time, s.Hosts, nil
}

// parseJobSpec: 1차 줄(user=/time=/호스트) + 2차 그룹 줄
//
//	yml=<파일명> infra=<> os=<> boot=<> splunk=<> hosts=<h1,h2,…>   (값에 공백 없음, 빈 값 허용)
//	all=<파일명>
//
// 같은 yml 줄이 여러 번이면 호스트를 합친다. 그룹 줄이 하나도 없으면(구버전 01) 단일 그룹 "(all)".
func parseJobSpec(b []byte) (jobSpec, error) {
	var s jobSpec
	seen := map[string]bool{}
	add := func(h string) {
		if h != "" && !seen[h] {
			seen[h] = true
			s.Hosts = append(s.Hosts, h)
		}
	}
	var extra []string // 그룹 줄에만 나온 호스트 (호스트 줄 뒤에 붙임)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "user="):
			s.User = strings.TrimSpace(line[len("user="):])
			continue
		case strings.HasPrefix(line, "time="):
			s.Time, _ = strconv.ParseInt(strings.TrimSpace(line[len("time="):]), 10, 64)
			continue
		case strings.HasPrefix(line, "all="):
			s.AllYml = strings.TrimSpace(line[len("all="):])
			continue
		case strings.HasPrefix(line, "yml="):
			name, g := parseGroupLine(line)
			if name == "" {
				continue
			}
			if s.Groups == nil {
				s.Groups = map[string]*Group{}
			}
			if old := s.Groups[name]; old != nil {
				g.Hosts = append(old.Hosts, g.Hosts...)
			}
			g.Hosts = uniq(g.Hosts)
			s.Groups[name] = g
			extra = append(extra, g.Hosts...)
			continue
		}
		add(strings.Fields(line)[0])
	}
	for _, h := range extra {
		add(h)
	}
	if !userRe.MatchString(s.User) {
		return jobSpec{}, errors.New("user 값이 없거나 올바르지 않음")
	}
	if len(s.Hosts) == 0 {
		return jobSpec{}, errors.New("호스트가 없음")
	}
	if s.Groups == nil {
		s.Groups = map[string]*Group{allGroup: {Hosts: append([]string(nil), s.Hosts...)}}
	}
	return s, nil
}

// parseGroupLine: "yml=a.yml infra=x os=y boot=z splunk=w hosts=h1,h2" → ("a.yml", Group)
func parseGroupLine(line string) (string, *Group) {
	g := &Group{}
	name := ""
	for _, f := range strings.Fields(line) {
		i := strings.IndexByte(f, '=')
		if i < 0 {
			continue
		}
		k, v := f[:i], f[i+1:]
		switch k {
		case "yml":
			name = v
		case "infra":
			g.Infra = v
		case "os":
			g.OS = v
		case "boot":
			g.Boot = v
		case "splunk":
			g.Splunk = v
		case "hosts":
			for _, h := range strings.Split(v, ",") {
				if h = strings.TrimSpace(h); h != "" {
					g.Hosts = append(g.Hosts, h)
				}
			}
		}
	}
	return name, g
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
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
