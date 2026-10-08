// model.go - 호스트 단계(Stage) 모델·한글 라벨·정체 판정, 상태 스냅샷(Snapshot) 구조체와 BuildSnapshot (CLI·TUI·원격 공용)
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Stage: 호스트 진행 단계. jobs/*.json 의 host.stage 와 snapshot 의 stage 값(문자열 그대로).
type Stage string

const (
	StageQueued     Stage = "queued"     // 대기: 전달됨, 아직 ping X 를 못 봄
	StageDeploying  Stage = "deploying"  // 배포중: ping X (seen_down) — dd / 재부팅
	StageInstalling Stage = "installing" // 설치중: anaconda 확인됨 (이후 재부팅 ping X 동안에도 유지)
	StageBooting    Stage = "booting"    // 부팅확인: seen_down 후 ping O, 아직 READY 아님
	StageReady      Stage = "ready"      // READY: 새 OS 부팅 확인, run 대기
	StageLdap       Stage = "ldap"       // LDAP확인: READY 직후 LDAP 비교·적용 중
	StageChecking   Stage = "checking"   // 체크중: os_check run 진행 중
	StageSecond     Stage = "second"     // 2차체크: os6_mgmt 경유 2차 체크 진행 중
	StageDone       Stage = "done"       // 완료: processed (run / 완료기록 / 수동)
	StageStuck      Stage = "stuck"      // 정체: seen_down 후 installStuck 을 넘겨도 READY 안 됨
	StageFailed     Stage = "failed"     // 실패: os_check 비정상 종료 연속 maxFails 회
)

// StageOrder: 화면·집계 표시 순서
var StageOrder = []Stage{StageQueued, StageDeploying, StageInstalling, StageBooting, StageReady,
	StageLdap, StageChecking, StageSecond, StageDone, StageStuck, StageFailed}

var stageLabels = map[Stage]string{
	StageQueued:     "대기",
	StageDeploying:  "배포중",
	StageInstalling: "설치중",
	StageBooting:    "부팅확인",
	StageReady:      "READY",
	StageLdap:       "LDAP확인",
	StageChecking:   "체크중",
	StageSecond:     "2차체크",
	StageDone:       "완료",
	StageStuck:      "정체",
	StageFailed:     "실패",
}

// Label: 한글 라벨 (알 수 없는 값은 그대로)
func (s Stage) Label() string {
	if l, ok := stageLabels[s]; ok {
		return l
	}
	return string(s)
}

func stageLabel(s Stage) string { return s.Label() }

// installPhase: 정체 판정 대상 단계 (seen_down 후 READY 전)
func (s Stage) installPhase() bool {
	return s == StageDeploying || s == StageInstalling || s == StageBooting || s == StageStuck
}

// transient: 데몬 프로세스 안에서만 의미가 있는 단계 (재기동 시 baseStage 로 되돌림)
func (s Stage) transient() bool {
	return s == StageLdap || s == StageChecking || s == StageSecond
}

// baseStage: 저장된 stage 가 없을 때(1차 JSON 등) 필드만으로 계산하는 단계
func baseStage(h *Host) Stage {
	switch {
	case h.Processed != "":
		return StageDone
	case h.Fails >= maxFails:
		return StageFailed
	case h.ReadyAt > 0:
		return StageReady
	case h.SeenDown:
		return StageDeploying
	}
	return StageQueued
}

// effectiveStage: 저장된 stage + 필드 + 현재 시각 → 실제 단계 (데몬·스냅샷 공용, 데몬이 없어도 동작).
// 정체: 설치 단계(배포중/설치중/부팅확인) 이고 READY 아님 && now − down_at > installStuck (경계값은 아직 설치중).
func effectiveStage(h *Host, now int64) Stage {
	if h.Processed != "" {
		return StageDone
	}
	if h.Fails >= maxFails {
		return StageFailed
	}
	s := Stage(h.Stage)
	if _, ok := stageLabels[s]; !ok || s == StageDone || s == StageFailed {
		s = baseStage(h)
	}
	if s.installPhase() {
		if h.ReadyAt == 0 && h.DownAt > 0 && now-h.DownAt > secs(installStuck) {
			return StageStuck
		}
		if s == StageStuck { // installStuck 이 늘어난 경우 등
			return StageDeploying
		}
	}
	return s
}

// ---- 그룹 ----

// GroupView: 화면용 그룹 (호스트는 job 에 현재 남아 있는 것만, 정렬)
type GroupView struct {
	Yml   string
	Info  Group
	Hosts []string
}

// jobGroups: yml 이름순 그룹 목록. 그룹 정보가 없으면(1차 JSON) 단일 "(all)", 어느 그룹에도 없는 호스트는 "(기타)".
func jobGroups(j *Job) []GroupView {
	in := map[string]bool{}
	var out []GroupView
	names := make([]string, 0, len(j.Groups))
	for n := range j.Groups {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		g := j.Groups[n]
		if g == nil {
			continue
		}
		v := GroupView{Yml: n, Info: Group{Infra: g.Infra, OS: g.OS, Boot: g.Boot, Splunk: g.Splunk}}
		for _, h := range uniq(g.Hosts) {
			if _, ok := j.Hosts[h]; ok {
				v.Hosts = append(v.Hosts, h)
				in[h] = true
			}
		}
		sort.Strings(v.Hosts)
		out = append(out, v)
	}
	var rest []string
	for h := range j.Hosts {
		if !in[h] {
			rest = append(rest, h)
		}
	}
	if len(rest) > 0 {
		sort.Strings(rest)
		name := etcGroup
		if len(j.Groups) == 0 {
			name = allGroup
		}
		out = append(out, GroupView{Yml: name, Hosts: rest})
	}
	return out
}

// groupHosts: 그룹 yml 의 현재 호스트 목록 (없으면 ok=false)
func groupHosts(j *Job, yml string) ([]string, bool) {
	for _, g := range jobGroups(j) {
		if g.Yml == yml {
			return g.Hosts, true
		}
	}
	return nil, false
}

// ---- 스냅샷 ----

// closedKeep: 이 시간 안에 종료된(jobs/done) job 도 스냅샷에 closed=true 로 포함 (완료 후 수동 run 용)
var closedKeep = 24 * time.Hour

const snapTimeLayout = "2006-01-02 15:04:05"

// noPingNote: ping 정보 없음이 이 시간 이상 이어지면 비고에 표시
var noPingNote = time.Minute

// StageCounts: 단계별 대수 (Total = 호스트 수)
type StageCounts struct {
	Total      int `json:"total"`
	Queued     int `json:"queued"`
	Deploying  int `json:"deploying"`
	Installing int `json:"installing"`
	Booting    int `json:"booting"`
	Ready      int `json:"ready"`
	Ldap       int `json:"ldap"`
	Checking   int `json:"checking"`
	Second     int `json:"second"`
	Done       int `json:"done"`
	Stuck      int `json:"stuck"`
	Failed     int `json:"failed"`
}

func (c *StageCounts) field(s Stage) *int {
	switch s {
	case StageQueued:
		return &c.Queued
	case StageDeploying:
		return &c.Deploying
	case StageInstalling:
		return &c.Installing
	case StageBooting:
		return &c.Booting
	case StageReady:
		return &c.Ready
	case StageLdap:
		return &c.Ldap
	case StageChecking:
		return &c.Checking
	case StageSecond:
		return &c.Second
	case StageDone:
		return &c.Done
	case StageStuck:
		return &c.Stuck
	case StageFailed:
		return &c.Failed
	}
	return nil
}

// Add: 호스트 1대 추가
func (c *StageCounts) Add(s Stage) {
	c.Total++
	if p := c.field(s); p != nil {
		*p++
	}
}

// Get: 단계별 대수
func (c StageCounts) Get(s Stage) int {
	if p := c.field(s); p != nil {
		return *p
	}
	return 0
}

func (c *StageCounts) merge(o StageCounts) {
	c.Total += o.Total
	for _, s := range StageOrder {
		*c.field(s) += o.Get(s)
	}
}

// Snapshot: `auto_setup snapshot` 의 JSON. 같은 디렉터리 내용 + 같은 now → 같은 바이트(정렬·시각 형식 고정).
type Snapshot struct {
	Version int         `json:"version"`  // 1
	Now     int64       `json:"now"`      // 스냅샷 시각 epoch
	NowText string      `json:"now_text"` // "2006-01-02 15:04:05" (로컬 시각)
	Daemon  SnapDaemon  `json:"daemon"`
	Totals  StageCounts `json:"totals"` // 진행 중(closed=false) job 합계
	Jobs    []SnapJob   `json:"jobs"`   // 진행 중 job(전달시각·id 순) → 종료된 job(같은 순)
}

type SnapDaemon struct {
	Running bool  `json:"running"`
	PID     int   `json:"pid"`     // pid 파일 값 (없으면 0)
	Started int64 `json:"started"` // 데몬 기동 epoch (pid 파일, 없으면 0)
}

type SnapJob struct {
	ID        string       `json:"id"`
	User      string       `json:"user"`
	Submitted int64        `json:"submitted"`
	AllYml    string       `json:"all_yml"`
	Closed    bool         `json:"closed"` // jobs/done 의 종료된 job
	Counts    StageCounts  `json:"counts"`
	Groups    []SnapGroup  `json:"groups"`
	Runs      []Run        `json:"runs"`
	Manual    []SnapManual `json:"manual"`            // 대기·진행 중 수동 run 요청
	Recheck   *Recheck     `json:"recheck,omitempty"` // 마지막 g(재확인) 진행·결과
}

type SnapGroup struct {
	Yml        string      `json:"yml"`
	Infra      string      `json:"infra"`
	OS         string      `json:"os"`
	Boot       string      `json:"boot"`
	Splunk     string      `json:"splunk"`
	Counts     StageCounts `json:"counts"`
	MaxElapsed int64       `json:"max_elapsed"` // 완료·실패 제외 호스트의 최장 elapsed (초)
	Complete   bool        `json:"complete"`    // 모든 호스트 완료 → 수동 run 가능
	Hosts      []SnapHost  `json:"hosts"`
}

type SnapHost struct {
	Host    string     `json:"host"`
	Stage   Stage      `json:"stage"`
	Label   string     `json:"label"`
	StageAt int64      `json:"stage_at"`
	Elapsed int64      `json:"elapsed"` // now − stage_at (초), stage_at 이 없으면 0
	DownAt  int64      `json:"down_at"` // 설치 경과 표시용 (now − down_at)
	Route   string     `json:"route"`
	Done    string     `json:"done"` // processed 값 (run code / external / manual)
	Note    string     `json:"note"` // 비고 (os6경유, code, 출처, LDAP, 2차, 실패 횟수 …)
	Ldap    *LdapState `json:"ldap,omitempty"`
	Second  string     `json:"second"`
	DoneSrc string     `json:"done_src"`
	Yml     string     `json:"-"` // 전체 보기(allView)에서만 채움: 이 호스트가 속한 그룹 yml
}

type SnapManual struct {
	File      string `json:"file"`           // requests/active/ 의 파일명
	Yml       string `json:"yml"`            // 그룹
	State     string `json:"state"`          // queued | running
	Requested int64  `json:"requested"`      // 요청 epoch (파일명)
	Mode      string `json:"mode,omitempty"` // "check" = 설정체크만, 비면 설정체크 + 설정수정
}

// BuildSnapshot: dir(=AUTO_SETUP_DIR) 의 jobs/*.json(+최근 종료 jobs/done) 과 requests/active 를 읽어 스냅샷 생성.
// 데몬이 없어도 동작하며 아무 파일도 쓰지 않는다.
func BuildSnapshot(dir string, now time.Time) Snapshot {
	t := now.Unix()
	s := Snapshot{Version: 1, Now: t, NowText: now.Format(snapTimeLayout), Jobs: []SnapJob{}}
	if pid, started, err := readPidIn(dir); err == nil {
		s.Daemon = SnapDaemon{Running: processAlive(pid), PID: pid, Started: started}
	}
	manual := readActiveRequests(dir)
	for _, j := range readJobsIn(filepath.Join(dir, "jobs"), time.Time{}) {
		sj := buildSnapJob(j, t, false, manual[j.ID])
		sj.Recheck = readRecheck(dir, j.ID)
		s.Totals.merge(sj.Counts)
		s.Jobs = append(s.Jobs, sj)
	}
	for _, j := range readJobsIn(filepath.Join(dir, "jobs", "done"), now.Add(-closedKeep)) {
		sj := buildSnapJob(j, t, true, manual[j.ID])
		sj.Recheck = readRecheck(dir, j.ID)
		s.Jobs = append(s.Jobs, sj)
	}
	return s
}

// snapshotJSON: 스냅샷 출력 바이트 (들여쓰기 1칸 + 끝 줄바꿈)
func snapshotJSON(s Snapshot) ([]byte, error) {
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// readJobsIn: d/*.json (since 가 0 이 아니면 수정시각 since 이후만), 전달시각·id 순. 깨진 파일은 건너뜀(로그 없음).
func readJobsIn(d string, since time.Time) []*Job {
	ents, err := os.ReadDir(d)
	if err != nil {
		return nil
	}
	var jobs []*Job
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if !since.IsZero() {
			if fi, err := e.Info(); err != nil || fi.ModTime().Before(since) {
				continue
			}
		}
		if j, err := loadJobFile(filepath.Join(d, e.Name())); err == nil {
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(a, b int) bool {
		if jobs[a].Submitted != jobs[b].Submitted {
			return jobs[a].Submitted < jobs[b].Submitted
		}
		return jobs[a].ID < jobs[b].ID
	})
	return jobs
}

func buildSnapJob(j *Job, now int64, closed bool, manual []SnapManual) SnapJob {
	sj := SnapJob{ID: j.ID, User: j.User, Submitted: j.Submitted, AllYml: j.AllYml, Closed: closed,
		Groups: []SnapGroup{}, Runs: j.Runs, Manual: manual}
	if sj.Runs == nil {
		sj.Runs = []Run{}
	}
	if sj.Manual == nil {
		sj.Manual = []SnapManual{}
	}
	hosts := map[string]SnapHost{}
	names := make([]string, 0, len(j.Hosts))
	for n := range j.Hosts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		sh := snapHost(n, j.Hosts[n], now)
		hosts[n] = sh
		sj.Counts.Add(sh.Stage)
	}
	for _, g := range jobGroups(j) {
		sg := SnapGroup{Yml: g.Yml, Infra: g.Info.Infra, OS: g.Info.OS, Boot: g.Info.Boot, Splunk: g.Info.Splunk,
			Hosts: []SnapHost{}, Complete: len(g.Hosts) > 0}
		for _, n := range g.Hosts {
			sh := hosts[n]
			sg.Hosts = append(sg.Hosts, sh)
			sg.Counts.Add(sh.Stage)
			if sh.Stage != StageDone {
				sg.Complete = false
				if sh.Stage != StageFailed && sh.Elapsed > sg.MaxElapsed {
					sg.MaxElapsed = sh.Elapsed
				}
			}
		}
		sj.Groups = append(sj.Groups, sg)
	}
	return sj
}

func snapHost(name string, h *Host, now int64) SnapHost {
	st := effectiveStage(h, now)
	sh := SnapHost{Host: name, Stage: st, Label: st.Label(), StageAt: h.StageAt, DownAt: h.DownAt,
		Route: h.Route, Done: h.Processed, Note: hostNote(h), Second: h.Second, DoneSrc: h.DoneSrc}
	if h.StageAt > 0 && now > h.StageAt {
		sh.Elapsed = now - h.StageAt
	}
	// ping 정보가 noPingNote 이상 없음 → 단계가 멈춰 보이는 이유 표시 (os6 경로면 probe 세션 문제)
	if h.NoPing > 0 && h.Processed == "" && now-h.NoPing >= secs(noPingNote) {
		n := "ping 정보없음"
		if h.Route == "os6" {
			n += "(os6 probe 확인)"
		}
		if sh.Note != "" {
			n = sh.Note + ", " + n
		}
		sh.Note = n
	}
	if h.Ldap != nil {
		l := *h.Ldap
		sh.Ldap = &l
	}
	return sh
}

// hostNote: 비고 (고정 순서, ", " 구분). bindpw·binddn 값은 다루지 않는다.
func hostNote(h *Host) string {
	var p []string
	if h.IP == "" && h.Processed == "" {
		p = append(p, "이름 미해결")
	}
	if h.Route == "os6" {
		p = append(p, "os6경유")
	}
	switch h.DoneSrc {
	case DoneSrcExternal:
		p = append(p, "타경로 완료기록")
	case DoneSrcManual:
		p = append(p, "수동 완료")
	default:
		if validCode(h.Processed) {
			p = append(p, "code "+h.Processed)
		}
	}
	if h.Ldap != nil {
		switch {
		case h.Ldap.Backup != LdapBackupOK:
			p = append(p, "LDAP 백업없음")
		case h.Ldap.Bindpw == BindpwSame:
			p = append(p, "LDAP binddn 동일")
		case h.Ldap.Bindpw == BindpwDiff && h.Ldap.Applied:
			p = append(p, "LDAP 복원")
		case h.Ldap.Bindpw == BindpwDiff && strings.HasPrefix(h.Ldap.Reason, ldapManualPrefix):
			p = append(p, "LDAP 수동 복원 필요")
		case h.Ldap.Bindpw == BindpwDiff:
			p = append(p, "LDAP 적용실패")
		case h.Ldap.Bindpw == BindpwNA:
			p = append(p, "LDAP binddn 확인불가")
		}
	}
	switch h.Second {
	case SecondRunning:
		p = append(p, "2차 진행")
	case SecondOK:
		p = append(p, "2차 OK")
	case SecondFail:
		p = append(p, "2차 FAIL")
	}
	if h.Fails > 0 && h.Processed == "" {
		p = append(p, "실패 "+strconv.Itoa(h.Fails)+"회")
	}
	return strings.Join(p, ", ")
}

// Recheck: g(재확인) 진행 기록 — 단계별 줄, 호스트별 결과(Result: os8 | os6 | fail)
type Recheck struct {
	Job   string        `json:"job"`
	Yml   string        `json:"yml"` // 대상 그룹, "*" = job 전체
	At    int64         `json:"at"`  // 시작 epoch
	Done  bool          `json:"done"`
	Lines []string      `json:"lines"`
	Hosts []RecheckHost `json:"hosts,omitempty"`
}

type RecheckHost struct {
	Host   string `json:"host"`
	Result string `json:"result"`
	Detail string `json:"detail,omitempty"`
}
