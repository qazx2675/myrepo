// state.go - 상태기계(분류·지연·완료후꺼짐·재부팅됨), 수집 회차(4.1), state.json 저장
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Status 는 서버 상태다. 숫자는 TUI 필터 키(1~7)와 같다(계획서 3.1 표 순서).
type Status int

const (
	StatusNone       Status = iota // 0 아직 확인 전(필터 대상 아님)
	StatusInstalling               // 1 설치중
	StatusDelayed                  // 2 지연
	StatusDisk                     // 3 디스크미인식
	StatusDone                     // 4 설치완료
	StatusPingX                    // 5 ping X (접속불가)
	StatusRebooted                 // 6 재부팅됨
	StatusOff                      // 7 완료후꺼짐
	statusCount
)

var statusLabels = [statusCount]string{"확인전", "설치중", "지연", "디스크미인식", "설치완료", "ping X", "재부팅됨", "완료후꺼짐"}

func (s Status) String() string {
	if s < 0 || s >= statusCount {
		return "?"
	}
	return statusLabels[s]
}

// MarshalText 와 UnmarshalText 는 json 에 한글 이름으로 저장한다.
func (s Status) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

func (s *Status) UnmarshalText(b []byte) error {
	for i, l := range statusLabels {
		if l == string(b) {
			*s = Status(i)
			return nil
		}
	}
	return fmt.Errorf("알 수 없는 상태: %q", b)
}

// IsCompleted 는 평소 회차에서 ping·ssh 하지 않는 완료 계열(설치완료·재부팅됨·완료후꺼짐)인지 알려준다.
func (s Status) IsCompleted() bool {
	return s == StatusDone || s == StatusRebooted || s == StatusOff
}

// CountsAsDone 은 "N대 설치완료" 수에 들어가는 상태인지 알려준다(재부팅됨 포함).
func (s Status) CountsAsDone() bool { return s == StatusDone || s == StatusRebooted }

// Check 는 LDAP·DNS·NTP·autofs·H/T 점검 결과다(7단계에서 채운다). state.json 에 같이 저장한다.
type Check struct {
	LDAP   string `json:"ldap,omitempty"`
	DNSNTP string `json:"dns_ntp,omitempty"`
	Autofs string `json:"autofs,omitempty"`
	HT     string `json:"ht,omitempty"`
	Note   string `json:"note,omitempty"`
}

// Record 는 서버 하나의 상태와 이력이다.
type Record struct {
	Name              string    `json:"name"`
	Vendor            string    `json:"vendor"`
	Status            Status    `json:"status"`
	FirstInstallingAt time.Time `json:"first_installing_at"` // 설치중으로 처음 본 시각
	DoneAt            time.Time `json:"done_at"`             // 설치완료로 처음 본 시각
	OffAt             time.Time `json:"off_at"`              // 완료후꺼짐 판정 시각
	RebootedAt        time.Time `json:"rebooted_at"`         // 재부팅됨 판정 시각
	Reason            string    `json:"reason"`              // 현재 상태의 이유(표시용)
	Check             Check     `json:"check"`
	// LastSeen 은 마지막으로 확인한 시각이다. 매 회차 바뀌어 저장을 일으키므로 메모리에만 둔다.
	LastSeen time.Time `json:"-"`
}

// PmResult 는 gossh -pm 확인 결과다.
type PmResult int

const (
	PmUnchecked  PmResult = iota // 확인하지 않음/gossh 실패(이전 상태 유지)
	PmDone                       // PM_OK
	PmInstalling                 // OS 설치중
	PmOther                      // 그 밖(ssh 접속 실패 등)
)

// Observation 은 한 회차에서 서버 하나에 대해 모은 관측값이다.
type Observation struct {
	Ping      bool
	Port      PortState // Ping 이 true 일 때만 의미 있음
	Pm        PmResult  // 22번이 열려 있을 때만 확인
	PmReason  string    // PmOther 의 이유
	Disk      bool      // 디스크미인식 표식이 나옴
	DiskError bool      // 디스크 확인 gossh 가 실패함(이전 디스크미인식 상태를 유지)
}

// apply 는 순수 전이 함수다. 완료 계열 서버는 평소 회차에서 바뀌지 않는다.
//
//	ping X                         -> ping X
//	22 거부 / pm 설치중            -> 설치중 (디스크 표식이 있으면 디스크미인식)
//	pm 설치완료                    -> 설치완료
//	pm 그 밖 / 22 무응답           -> 설치중 (완료를 확인하지 못했으므로, 이유를 남김)
//	설치중으로 처음 본 뒤 delay 이상 -> 지연 (디스크미인식이 우선)
func apply(prev Record, obs Observation, now time.Time, delay time.Duration) Record {
	r := prev
	r.LastSeen = now
	if prev.Status.IsCompleted() {
		return r
	}
	if !obs.Ping {
		r.Status = StatusPingX
		r.Reason = "ping 응답 없음"
		return r
	}
	switch {
	case obs.Port == PortRefused:
		r.Reason = "22번 포트 거부"
	case obs.Port == PortOpen && obs.Pm == PmDone:
		r.Status = StatusDone
		r.Reason = ""
		r.DoneAt = now
		return r
	case obs.Port == PortOpen && obs.Pm == PmInstalling:
		r.Reason = "OS 설치중"
	case obs.Port == PortOpen && obs.Pm == PmOther:
		r.Reason = "ssh 확인 실패: " + obs.PmReason
	case obs.Port == PortOpen: // gossh 실패 등으로 확인 못 함: 이전 상태 유지
		return r
	default: // 22번 응답 없음
		r.Reason = "22번 포트 응답 없음"
	}
	if r.FirstInstallingAt.IsZero() {
		r.FirstInstallingAt = now
	}
	switch {
	case obs.Disk || (obs.DiskError && prev.Status == StatusDisk):
		r.Status = StatusDisk
		r.Reason = "디스크 미인식"
	case now.Sub(r.FirstInstallingAt) >= delay:
		r.Status = StatusDelayed
	default:
		r.Status = StatusInstalling
	}
	return r
}

// ApplyCompletedPing 은 `c` 키에서 완료 서버에 ping 한 결과를 반영한다.
// 설치완료·재부팅됨 + ping X -> 완료후꺼짐, 완료후꺼짐 + ping O -> 재부팅됨.
func ApplyCompletedPing(rec Record, pingOK bool, now time.Time) Record {
	r := rec
	switch {
	case (rec.Status == StatusDone || rec.Status == StatusRebooted) && !pingOK:
		r.Status = StatusOff
		r.OffAt = now
		r.Reason = "완료 후 ping 응답 없음"
	case rec.Status == StatusOff && pingOK:
		r.Status = StatusRebooted
		r.RebootedAt = now
		r.Reason = "완료 후 꺼졌다가 다시 켜짐"
	}
	return r
}

// VendorGroup 은 벤더별 호스트 목록(상태별, hostname 순)이다.
type VendorGroup struct {
	Vendor string
	Hosts  map[Status][]string
}

// Snapshot 은 TUI 에 넘기는 불변 스냅샷이다. 만든 뒤에는 고치지 않는다.
type Snapshot struct {
	Records []Record // 상태 순서(1~7, 확인전은 끝) -> hostname 순
	Counts  map[Status]int
	Total   int
	Vendors []VendorGroup // cfg.VendorOrder() 순서
	Round   int           // 시작한 회차 번호
	Running bool          // 회차 진행 중
	Updated time.Time     // 마지막으로 회차가 끝난 시각(없으면 zero)
	LastErr string
}

// Done 은 설치완료 수(재부팅됨 포함)다.
func (s *Snapshot) Done() int { return s.Counts[StatusDone] + s.Counts[StatusRebooted] }

func statusRank(s Status) int {
	if s == StatusNone {
		return int(statusCount)
	}
	return int(s)
}

// Deps 는 수집에 쓰는 외부 동작이다(테스트에서 바꾼다).
type Deps struct {
	Ping func(hosts []string, timeout time.Duration, pps int) (map[string]bool, error)
	Port func(hosts []string, parallel int, timeout time.Duration) map[string]PortState
	Pm   func(cfg *Config, hosts []string) (done, installing []string, other map[string]string, err error)
	Disk func(cfg *Config, hosts []string) ([]string, error)
	Now  func() time.Time
}

func defaultDeps() Deps {
	return Deps{Ping: PingAll, Port: CheckPort22, Pm: PmCheck, Disk: DiskCheck, Now: time.Now}
}

// stateVersion 은 state.json 형식 번호다.
const stateVersion = 1

type stateFile struct {
	Version int      `json:"version"`
	Hosts   []Record `json:"hosts"`
}

// stateFileName 은 실행 디렉터리에 둘 state 파일 경로(<호스트파일 이름>.pm_state.json)다.
func stateFileName(hostfile string) string {
	return filepath.Base(hostfile) + ".pm_state.json"
}

// Collector 는 수집 회차를 돌리고 상태를 저장하며 스냅샷을 내보낸다.
type Collector struct {
	cfg       *Config
	hosts     []string
	statePath string
	deps      Deps
	interval  time.Duration

	runMu sync.Mutex // 회차와 완료 ping 이 겹치지 않게 막는다
	mu    sync.Mutex // recs, round, updated, lastErr 보호
	recs  map[string]Record

	saveMu    sync.Mutex // save 끼리 겹치지 않게 막는다(회차·l 점검이 동시에 저장할 수 있음)
	lastSaved []byte
	round     int
	updated   time.Time
	lastErr   string

	snap   atomic.Pointer[Snapshot]
	notify chan struct{}
}

// NewCollector 는 state 파일을 읽어(이력 유지) 수집기를 만든다. 호스트파일에서 빠진 서버는 버린다.
func NewCollector(cfg *Config, hosts []string, statePath string, deps Deps) (*Collector, error) {
	c := &Collector{
		cfg: cfg, hosts: hosts, statePath: statePath, deps: deps,
		interval: time.Duration(cfg.IntervalSec) * time.Second,
		recs:     make(map[string]Record, len(hosts)),
		notify:   make(chan struct{}, 1),
	}
	old := map[string]Record{}
	if b, err := os.ReadFile(statePath); err == nil {
		var sf stateFile
		if err := json.Unmarshal(b, &sf); err != nil {
			return nil, fmt.Errorf("%s 읽기 실패: %v", statePath, err)
		}
		for _, r := range sf.Hosts {
			old[r.Name] = r
		}
		c.lastSaved = b
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("%s 읽기 실패: %v", statePath, err)
	}
	for _, h := range hosts {
		r, ok := old[h]
		if !ok {
			r = Record{Name: h}
		}
		r.Vendor = cfg.Vendor(h)
		c.recs[h] = r
	}
	c.publish(false)
	return c, nil
}

// Snapshot 은 가장 최근 스냅샷이다.
func (c *Collector) Snapshot() *Snapshot { return c.snap.Load() }

// Updates 는 새 스냅샷이 나오면 신호가 오는 채널이다(용량 1, 밀리면 합친다).
func (c *Collector) Updates() <-chan struct{} { return c.notify }

// publish 는 현재 recs 로 스냅샷을 만들어 내보낸다.
func (c *Collector) publish(running bool) {
	c.mu.Lock()
	recs := make([]Record, 0, len(c.recs))
	for _, h := range c.hosts {
		recs = append(recs, c.recs[h])
	}
	round, updated, lastErr := c.round, c.updated, c.lastErr
	c.mu.Unlock()
	c.snap.Store(buildSnapshot(c.cfg, recs, round, running, updated, lastErr))
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

func buildSnapshot(cfg *Config, recs []Record, round int, running bool, updated time.Time, lastErr string) *Snapshot {
	sort.Slice(recs, func(i, j int) bool {
		a, b := recs[i], recs[j]
		if ra, rb := statusRank(a.Status), statusRank(b.Status); ra != rb {
			return ra < rb
		}
		return a.Name < b.Name
	})
	s := &Snapshot{Records: recs, Counts: map[Status]int{}, Total: len(recs),
		Round: round, Running: running, Updated: updated, LastErr: lastErr}
	groups := map[string]*VendorGroup{}
	order := cfg.VendorOrder()
	for _, v := range order {
		groups[v] = &VendorGroup{Vendor: v, Hosts: map[Status][]string{}}
	}
	for _, r := range recs { // 이미 상태·이름 순이라 목록도 이름 순이 된다
		s.Counts[r.Status]++
		g := groups[r.Vendor]
		if g == nil {
			g = &VendorGroup{Vendor: r.Vendor, Hosts: map[Status][]string{}}
			groups[r.Vendor] = g
			order = append(order, r.Vendor)
		}
		g.Hosts[r.Status] = append(g.Hosts[r.Status], r.Name)
	}
	for _, v := range order {
		s.Vendors = append(s.Vendors, *groups[v])
	}
	return s
}

func (c *Collector) pendingHosts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, h := range c.hosts {
		if !c.recs[h].Status.IsCompleted() {
			out = append(out, h)
		}
	}
	return out
}

// Round 는 4.1 의 한 회차를 돌린다. ping 이 실패하면(raw 소켓 등) 상태를 바꾸지 않고 error 를 돌려준다.
func (c *Collector) Round(ctx context.Context) error {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.mu.Lock()
	c.round++
	c.mu.Unlock()
	c.publish(true)
	err := c.round1(ctx)
	c.mu.Lock()
	if err != nil {
		c.lastErr = err.Error()
	} else {
		c.lastErr = ""
		c.updated = c.deps.Now()
	}
	c.mu.Unlock()
	c.publish(false)
	return err
}

func (c *Collector) round1(ctx context.Context) error {
	pending := c.pendingHosts()
	obs := make(map[string]*Observation, len(pending))
	var errs []string
	if len(pending) > 0 {
		timeout := time.Duration(c.cfg.PingTimeoutMs) * time.Millisecond
		// 1. ping (완료 계열 제외)
		pingRes, err := c.deps.Ping(pending, timeout, c.cfg.PingPPS)
		if err != nil {
			return err
		}
		var pingOK []string
		for _, h := range pending {
			o := &Observation{Ping: pingRes[h]}
			obs[h] = o
			if o.Ping {
				pingOK = append(pingOK, h)
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// 2. 22번 포트 (ping O 인 서버)
		var open []string
		if len(pingOK) > 0 {
			ports := c.deps.Port(pingOK, c.cfg.TCPParallel, timeout)
			for _, h := range pingOK {
				obs[h].Port = ports[h]
				if ports[h] == PortOpen {
					open = append(open, h)
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// 3. gossh -pm (22번이 열린 서버만)
		var inst []string
		if len(open) > 0 {
			done, installing, other, err := c.deps.Pm(c.cfg, open)
			if err != nil {
				errs = append(errs, "gossh -pm: "+err.Error())
			} else {
				for _, h := range done {
					obs[h].Pm = PmDone
				}
				for _, h := range installing {
					obs[h].Pm = PmInstalling
				}
				for h, why := range other {
					if o := obs[h]; o != nil {
						o.Pm, o.PmReason = PmOther, why
					}
				}
				inst = installing
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// 4. 디스크 확인: gossh -pm 이 설치중으로 판정한(ssh 가 되는) 서버만.
		// 22번이 거부된 서버는 ssh 가 안 되므로 대상이 아니다.
		if len(inst) > 0 {
			fails, err := c.deps.Disk(c.cfg, inst)
			if err != nil {
				errs = append(errs, "디스크 확인: "+err.Error())
				for _, h := range inst {
					obs[h].DiskError = true
				}
			} else {
				for _, h := range fails {
					if o := obs[h]; o != nil {
						o.Disk = true
					}
				}
			}
		}
	}
	// 5. 상태 갱신·저장
	now := c.deps.Now()
	delay := time.Duration(c.cfg.DelayMin) * time.Minute
	c.mu.Lock()
	for _, h := range pending {
		c.recs[h] = apply(c.recs[h], *obs[h], now, delay)
	}
	c.mu.Unlock()
	if err := c.save(); err != nil {
		errs = append(errs, "state 저장: "+err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// CheckCompleted 는 `c` 키용이다. 완료 계열 서버에 ping 해서 완료후꺼짐·재부팅됨을 판정한다.
func (c *Collector) CheckCompleted(ctx context.Context) error {
	c.runMu.Lock()
	defer c.runMu.Unlock()
	c.mu.Lock()
	var targets []string
	for _, h := range c.hosts {
		if c.recs[h].Status.IsCompleted() {
			targets = append(targets, h)
		}
	}
	c.mu.Unlock()
	if len(targets) == 0 {
		return nil
	}
	res, err := c.deps.Ping(targets, time.Duration(c.cfg.PingTimeoutMs)*time.Millisecond, c.cfg.PingPPS)
	if err != nil {
		return err
	}
	now := c.deps.Now()
	c.mu.Lock()
	for _, h := range targets {
		c.recs[h] = ApplyCompletedPing(c.recs[h], res[h], now)
	}
	c.mu.Unlock()
	serr := c.save()
	c.publish(false)
	return serr
}

// save 는 내용이 바뀐 때만 임시 파일에 쓰고 이름을 바꿔 저장한다.
func (c *Collector) save() error {
	c.saveMu.Lock()
	defer c.saveMu.Unlock()
	c.mu.Lock()
	sf := stateFile{Version: stateVersion}
	for _, h := range c.hosts {
		sf.Hosts = append(sf.Hosts, c.recs[h])
	}
	c.mu.Unlock()
	sort.Slice(sf.Hosts, func(i, j int) bool { return sf.Hosts[i].Name < sf.Hosts[j].Name })
	b, err := json.MarshalIndent(sf, "", " ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if bytes.Equal(b, c.lastSaved) {
		return nil
	}
	if err := writeFileAtomic(c.statePath, b); err != nil {
		return err
	}
	c.lastSaved = b
	return nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pm_state_*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Run 은 ctx 가 끝날 때까지 회차를 반복한다. 회차는 겹치지 않는다.
// 다음 회차는 앞 회차 시작 후 interval 뒤에 시작하고, 앞 회차가 더 오래 걸렸으면 끝나자마자 시작한다.
// 회차 사이에는 타이머만 걸어 두고 쉰다.
func (c *Collector) Run(ctx context.Context) {
	for {
		start := time.Now()
		_ = c.Round(ctx) // 오류는 스냅샷의 LastErr 로 보인다
		wait := c.interval - time.Since(start)
		if wait < 0 {
			wait = 0
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
