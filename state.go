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

// Status 는 서버 상태다. 숫자는 TUI 필터 키(1~9)와 같다(계획서 3.1 표 순서, 8·9 는 yum 모드 전용 4.8).
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
	StatusAlive                    // 8 접속가능 (yum 모드: 기준 커널을 기록했고 아직 변화 없음)
	StatusYumDone                  // 9 yum_update/접속가능 (yum 모드: 커널이 바뀐 완료)
	statusCount
)

var statusLabels = [statusCount]string{"확인전", "설치중", "지연", "디스크미인식", "설치완료", "ping X", "재부팅됨", "완료후꺼짐", "접속가능", "yum_update/접속가능"}

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
// OS 서버 기준이다. Record 가 있으면 yum 서버를 구분하는 Record.Completed 를 쓴다.
func (s Status) IsCompleted() bool {
	return s == StatusDone || s == StatusRebooted || s == StatusOff
}

// CountsAsDone 은 "N대 설치완료" 수에 들어가는 상태인지 알려준다(재부팅됨 포함).
func (s Status) CountsAsDone() bool { return s == StatusDone || s == StatusRebooted }

// Check 는 서버 설정 조사 결과다(계획서 4.9). state.json 에 같이 저장한다.
// v0.4.0 이하의 필드(ldap·dns_ntp·autofs)는 읽을 때 버려지고, Ver 가 낮아 다시 조사된다.
type Check struct {
	Ver       int    `json:"ver,omitempty"`        // 형식 버전. ldapCmdVer 보다 작으면 다시 조사한다
	Kernel    string `json:"kernel,omitempty"`     // uname -r
	Appl      string `json:"appl,omitempty"`       // /etc/auto.appl 의 /appl 마운트 위치
	DNS       string `json:"dns,omitempty"`        // resolv.conf nameserver(파일 순서, 공백 구분)
	LdapURI   string `json:"ldap_uri,omitempty"`   // ldap.conf URI 의 호스트 이름(파일 순서, 공백 구분)
	BindDN    string `json:"binddn,omitempty"`     // ldap.conf binddn 값
	HT        string `json:"ht,omitempty"`         // ON / OFF / ""(모름)
	LdapCheck string `json:"ldap_check,omitempty"` // ldap_check 출력(사람용, 비교에 쓰지 않음)
	Note      string `json:"note,omitempty"`       // 최근 조사 실패 이유 등
}

// current 는 현재 형식으로 채워진 결과인지 알려준다(v0.1.x 로 저장된 값은 false).
func (c Check) current() bool { return c.Ver >= ldapCmdVer }

// ModeYum 은 Record.Mode 에서 yum update 모드를 뜻한다(빈 문자열은 OS 설치, 계획서 4.8).
const ModeYum = "yum"

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
	// InstallDone 은 설치완료(OS)·yum_update/접속가능(yum)을 한 번이라도 본 서버다(계획서 4.9). 한 번 켜지면
	// ping X·완료후꺼짐·재부팅됨이 되어도 꺼지지 않고, 모드(OS<->yum)가 바뀌어 기록을 새로 시작할 때만 꺼진다.
	InstallDone bool `json:"install_done,omitempty"`

	// yum update 모드 전용(계획서 4.8). 모두 비어 있으면 json 에 나오지 않아 OS 서버의 state.json 은 v0.3.0 과 같다.
	Mode      string    `json:"mode,omitempty"`        // ""=OS 설치, "yum"=yum update
	YumKernel string    `json:"yum_kernel,omitempty"`  // 기준 커널(uname -r), 처음 한 번만 기록
	YumBoot   string    `json:"yum_boot,omitempty"`    // 기준 부팅 시각(uptime -s 그대로)
	YumSince  time.Time `json:"yum_since,omitempty"`   // 기준을 기록한 시각(zero 면 json 에서 뺀다, MarshalJSON 참고)
	YumDoneAt time.Time `json:"yum_done_at,omitempty"` // yum_update/접속가능으로 처음 본 시각(zero 면 뺀다)
	YumNow    string    `json:"yum_now,omitempty"`     // 가장 최근에 본 커널

	// LastSeen 은 마지막으로 확인한 시각이다. 매 회차 바뀌어 저장을 일으키므로 메모리에만 둔다.
	LastSeen time.Time `json:"-"`
}

// IsYum 은 yum update 모드 서버인지 알려준다.
func (r Record) IsYum() bool { return r.Mode == ModeYum }

// Completed 는 평소 회차에서 ping·ssh 하지 않는 완료 계열인지 알려준다.
// OS 서버는 Status.IsCompleted 와 같고, yum 서버는 yum_update/접속가능·완료후꺼짐만 완료다
// (yum 서버의 재부팅됨은 커널이 아직 같으므로 매 회차 계속 확인한다, 계획서 4.8).
func (r Record) Completed() bool {
	if r.IsYum() {
		return r.Status == StatusYumDone || r.Status == StatusOff
	}
	return r.Status.IsCompleted()
}

// MarshalJSON 은 zero 인 yum 시각 필드를 뺀다. encoding/json 의 omitempty 는 time.Time 구조체를 비었다고 보지 않아
// 그대로 두면 OS 서버에도 "0001-01-01T00:00:00Z" 가 찍혀 v0.3.0 의 state.json·소켓 출력과 달라진다.
func (r Record) MarshalJSON() ([]byte, error) {
	type plain Record // 메서드가 없는 같은 모양(MarshalJSON 재귀 방지)
	aux := struct {
		plain
		YumSince  *time.Time `json:"yum_since,omitempty"` // plain 의 같은 이름 필드를 가린다(더 얕은 필드가 이긴다)
		YumDoneAt *time.Time `json:"yum_done_at,omitempty"`
	}{plain: plain(r)}
	if !r.YumSince.IsZero() {
		aux.YumSince = &r.YumSince
	}
	if !r.YumDoneAt.IsZero() {
		aux.YumDoneAt = &r.YumDoneAt
	}
	return json.Marshal(aux)
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

// yumRebootTolerance 는 부팅 시각(uptime -s)이 이만큼 넘게 달라야 재부팅으로 보는 기준이다(1초 오차 허용).
const yumRebootTolerance = 60 * time.Second

// bootTimeDiffers 는 두 `uptime -s` 값이 60초 넘게 다른지 알려준다. 어느 쪽이든 읽을 수 없으면 다르지 않다고 본다.
func bootTimeDiffers(base, cur string) bool {
	a, err1 := time.Parse(yumBootLayout, base)
	b, err2 := time.Parse(yumBootLayout, cur)
	if err1 != nil || err2 != nil {
		return false
	}
	d := b.Sub(a)
	if d < 0 {
		d = -d
	}
	return d > yumRebootTolerance
}

// applyYum 은 yum update 모드 서버의 순수 전이 함수다(계획서 4.8). 완료(yum_update/접속가능·완료후꺼짐)는 바꾸지 않는다.
// obs 는 커널 조회 결과이며 ping X·22번 불가로 조회하지 않았으면 nil 이다.
//
//	ping X, 기준 있음            -> 재부팅됨 (재부팅 중, RebootedAt 은 처음 한 번)
//	ping X, 기준 없음            -> ping X
//	ping O, ssh 불가, 기준 없음  -> 확인전 (이유 표시)
//	ping O, ssh 불가, 기준 있음  -> 상태 유지 (이유만 갱신)
//	ssh O, 기준 없음             -> 접속가능 (기준 커널·부팅 시각·YumSince 기록)
//	ssh O, 커널 다름             -> yum_update/접속가능 (YumDoneAt)
//	ssh O, 커널 같음, 재부팅 흔적 -> 재부팅됨 (이전 재부팅됨이거나 부팅 시각이 60초 넘게 다름)
//	ssh O, 커널 같음, 흔적 없음  -> 접속가능
func applyYum(prev Record, ping bool, port PortState, obs *YumObs, now time.Time) Record {
	r := prev
	r.LastSeen = now
	if prev.Completed() {
		return r
	}
	baseline := prev.YumKernel != ""
	if !ping {
		if baseline {
			r.Status = StatusRebooted
			r.Reason = "재부팅 중(ping 응답 없음)"
			if r.RebootedAt.IsZero() {
				r.RebootedAt = now
			}
		} else {
			r.Status = StatusPingX
			r.Reason = "ping 응답 없음"
		}
		return r
	}
	var why string
	switch {
	case port == PortRefused:
		why = "22번 포트 거부"
	case port != PortOpen:
		why = "22번 포트 응답 없음"
	case obs == nil:
		why = "ssh 확인 실패: 응답 없음"
	case obs.Err != "" || obs.Kernel == "":
		e := obs.Err
		if e == "" {
			e = "응답 없음"
		}
		why = "ssh 확인 실패: " + e
	}
	if why != "" {
		if !baseline {
			r.Status = StatusNone
		}
		r.Reason = why
		return r
	}
	if !baseline {
		r.YumKernel, r.YumBoot, r.YumSince, r.YumNow = obs.Kernel, obs.Boot, now, obs.Kernel
		r.Status = StatusAlive
		r.Reason = "기준 커널 " + obs.Kernel
		return r
	}
	r.YumNow = obs.Kernel
	if obs.Kernel != prev.YumKernel {
		r.Status = StatusYumDone
		if r.YumDoneAt.IsZero() {
			r.YumDoneAt = now
		}
		r.Reason = "커널 " + prev.YumKernel + " → " + obs.Kernel
		return r
	}
	if prev.Status == StatusRebooted || bootTimeDiffers(prev.YumBoot, obs.Boot) {
		r.Status = StatusRebooted
		if r.RebootedAt.IsZero() {
			r.RebootedAt = now
		}
		r.Reason = "재부팅됨, 커널 동일 " + obs.Kernel
		return r
	}
	r.Status = StatusAlive
	r.Reason = "기준 커널 " + obs.Kernel
	return r
}

// yumDelayed 는 yum 지연(계획서 4.8)인지 알려준다: 기준을 기록한 뒤 delay 이상 지났는데 아직 완료가 아님.
// 상태는 바꾸지 않는다(표시·코멘트에서만 쓴다).
func yumDelayed(r Record, now time.Time, delay time.Duration) bool {
	return r.IsYum() && !r.YumSince.IsZero() && !r.Completed() && now.Sub(r.YumSince) >= delay
}

// ApplyCompletedPing 은 `c` 키에서 완료 서버에 ping 한 결과를 반영한다.
// 설치완료·재부팅됨 + ping X -> 완료후꺼짐, 완료후꺼짐 + ping O -> 재부팅됨.
// yum 서버: yum_update/접속가능 + ping X -> 완료후꺼짐, 완료후꺼짐 + ping O -> yum_update/접속가능.
func ApplyCompletedPing(rec Record, pingOK bool, now time.Time) Record {
	r := rec
	if rec.IsYum() {
		switch {
		case rec.Status == StatusYumDone && !pingOK:
			r.Status = StatusOff
			r.OffAt = now
			r.Reason = "완료 후 ping 응답 없음"
		case rec.Status == StatusOff && pingOK:
			r.Status = StatusYumDone
			r.RebootedAt = now
			r.Reason = "완료 후 꺼졌다가 다시 켜짐"
		}
		return r
	}
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

// YumVendorGroup 은 벤더별 yum 지연 서버 목록(hostname 순)이다(계획서 4.8 의 r 코멘트용).
type YumVendorGroup struct {
	Vendor     string
	Waiting    []string // 지연 A: 재부팅 대기(상태 접속가능)
	SameKernel []string // 지연 B: 재부팅됨, 커널 동일(재부팅 뒤 ssh 로 커널이 같음을 확인)
	Rebooting  []string // 지연 C: 재부팅됨이지만 재부팅 뒤 아직 접속되지 않음(이유가 `재부팅 중`)
}

// yumKernelSame 은 yum 서버가 재부팅은 되었으나 커널이 그대로임을 ssh 로 확인한 기록인지 알려준다.
// 상태 이름은 재부팅됨 하나지만, 재부팅 중(ping X)인 서버와 구분하려고 applyYum 이 남기는 Reason 으로 판정한다.
func yumKernelSame(r Record) bool {
	return r.Status == StatusRebooted && strings.HasPrefix(r.Reason, "재부팅됨, 커널 동일")
}

// YumSummary 는 yum update 모드 서버의 집계다(계획서 4.8). 수집 회차마다 스냅샷을 만들 때 한 번 계산한다.
// 이 필드들은 스냅샷과 함께 소켓으로 화면에 가므로 모두 내보내는 필드다. yum 서버가 없으면 전부 zero 다.
type YumSummary struct {
	Total      int // yum 서버 수
	Done       int // yum_update/접속가능(커널 변경 완료)
	Alive      int // 접속가능
	Rebooted   int // 재부팅됨(커널은 아직 같음) 전체: 재부팅 중인 서버와 커널 동일 확인 서버
	SameKernel int // 그 중 재부팅 뒤 접속해 커널이 같음을 확인한 서버(yumKernelSame)
	PingX      int // 기준 커널을 기록하기 전 ping X
	None       int // 확인전
	Off        int // 완료후꺼짐
	Delayed    int // yum 지연(접속가능·재부팅됨 중 기준 시각부터 DelayMin 이 지난 서버)
	DelayMin   int // 지연 기준(분), cfg.YumDelayMin

	PingXHosts      []string // c 의 `yum 접속불가` 목록(hostname 순)
	WaitingHosts    []string // c 의 `yum 재부팅 대기` 목록: 접속가능 서버
	RebootingHosts  []string // c 의 `yum 재부팅 중` 목록: 재부팅됨이지만 아직 접속되지 않는 서버
	SameKernelHosts []string // c 의 `yum 재부팅됨 커널 동일` 목록: 재부팅 뒤 커널이 같음을 확인한 서버

	Vendors []YumVendorGroup // 지연 서버가 있는 벤더만, cfg.VendorOrder() 순서
}

// Snapshot 은 TUI 에 넘기는 불변 스냅샷이다. 만든 뒤에는 고치지 않는다.
type Snapshot struct {
	Records []Record       // 상태 순서(1~9, 확인전은 끝) -> hostname 순
	Counts  map[Status]int // 모든 서버(OS·yum)를 상태별로 센다. 화면 머리줄·필터가 쓴다
	Total   int            // 모든 서버 수(OS·yum)
	Vendors []VendorGroup  // cfg.VendorOrder() 순서. OS 서버만 담는다(yum 서버는 Yum 에 따로)
	Round   int            // 시작한 회차 번호
	Running bool           // 회차 진행 중
	Updated time.Time      // 마지막으로 회차가 끝난 시각(없으면 zero)
	LastErr string
	Yum     YumSummary // yum 서버 집계(계획서 4.8)

	// 계획서 4.9: HostOrder 는 Records 를 호스트파일 순서로 가리키는 인덱스(l 표·res 파일 줄 순서),
	// Cmp 는 Records 와 같은 순서의 oklist 비교 비트(cmpOn, 다른 칸 비트; oklist 가 없으면 nil), Cfg 는 그 집계다.
	HostOrder []int
	Cmp       []uint8
	Cfg       CfgSummary
}

// Done 은 설치완료 수(재부팅됨 포함)다. OS 서버만 센다(yum 서버의 재부팅됨은 완료가 아니다).
func (s *Snapshot) Done() int {
	return s.Counts[StatusDone] + s.Counts[StatusRebooted] - s.Yum.Rebooted
}

// osCounts 는 OS 서버만의 상태별 개수다. yum 서버가 없으면 Counts 를 그대로 돌려준다(복사하지 않음, 고치지 말 것).
func (s *Snapshot) osCounts() map[Status]int {
	if s.Yum.Total == 0 {
		return s.Counts
	}
	c := make(map[Status]int, len(s.Counts))
	for k, v := range s.Counts {
		c[k] = v
	}
	c[StatusRebooted] -= s.Yum.Rebooted
	c[StatusPingX] -= s.Yum.PingX
	c[StatusOff] -= s.Yum.Off
	c[StatusNone] -= s.Yum.None
	return c
}

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
	// Yum 은 yum 서버의 커널·부팅 시각 조회다. nil 이면 YumCheck 를 쓴다(yum 서버가 없는 테스트의 Deps 는 비워 둬도 된다).
	Yum func(cfg *Config, hosts []string) (map[string]YumObs, error)
	// Cfg 는 설정 조사(계획서 4.9)다: 응답한 서버의 Check 와 응답하지 않은 서버의 이유.
	// nil 이면 회차의 자동 첫 조사를 하지 않고, `l` 재조사는 LdapQuery 를 쓴다.
	Cfg func(cfg *Config, hosts []string) (map[string]Check, map[string]string, error)
}

func defaultDeps() Deps {
	return Deps{Ping: PingAll, Port: CheckPort22, Pm: PmCheck, Disk: DiskCheck, Now: time.Now, Yum: YumCheck, Cfg: LdapQuery}
}

// stateVersion 은 state.json 형식 번호다. 2(v0.5.0): install_done 추가. 1 로 저장된 파일은 읽을 때 install_done 을 상태로 정한다.
const stateVersion = 2

// stickDone 은 지금 상태가 완료면 InstallDone 을 켠다(끄지는 않는다).
func stickDone(r Record) Record {
	if doneNow(r) {
		r.InstallDone = true
	}
	return r
}

// migrateDone 은 install_done 이 없던 state.json(v0.4.0 이하)의 서버에 InstallDone 을 정한다(계획서 4.9).
// OS 서버: 설치완료·완료후꺼짐·재부팅됨. yum 서버: yum_update/접속가능이거나 YumDoneAt 이 있음(재부팅됨만으로는 아님).
func migrateDone(r Record) Record {
	if r.IsYum() {
		r.InstallDone = r.Status == StatusYumDone || !r.YumDoneAt.IsZero()
	} else {
		r.InstallDone = r.Status == StatusDone || r.Status == StatusOff || r.Status == StatusRebooted
	}
	return r
}

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

	yum map[string]bool // yum update 모드 서버의 hostname(호스트파일 두 번째 칸이 yum). nil 이면 모두 OS 설치

	ok *Oklist // 기준 설정표(계획서 4.9). nil 이면 비교하지 않는다

	// res 파일(계획서 4.9). resPath 가 비어 있으면 쓰지 않는다. resErr·resLogged 는 mu 로 보호한다.
	resPath   string
	resLog    func(string) // 쓰기 실패를 한 번 남길 곳(데몬 로그). nil 이면 남기지 않는다
	resMu     sync.Mutex   // res 쓰기끼리 겹치지 않게 막는다(resLast 보호)
	resLast   []byte
	resLastD  []byte // 완료 목록 파일(<호스트파일>.done.txt)의 지난 내용
	resLastL  []byte // l 표 파일(<호스트파일>.ldap.txt)의 지난 내용
	resErr    string
	resLogged bool
}

// CollectorOpt 는 NewCollector 의 선택 설정이다.
type CollectorOpt func(*Collector)

// WithYum 은 yum update 모드인 hostname 집합을 정한다(readHostSpecs 의 두 번째 반환값).
func WithYum(set map[string]bool) CollectorOpt {
	return func(c *Collector) { c.yum = set }
}

// WithOklist 는 기준 설정표를 정한다(nil 이면 비교 없음).
func WithOklist(ok *Oklist) CollectorOpt {
	return func(c *Collector) { c.ok = ok }
}

// WithResFile 은 res 파일 경로와 쓰기 실패를 남길 곳(nil 가능)을 정한다.
func WithResFile(path string, log func(string)) CollectorOpt {
	return func(c *Collector) { c.resPath, c.resLog = path, log }
}

// NewCollector 는 state 파일을 읽어(이력 유지) 수집기를 만든다. 호스트파일에서 빠진 서버는 버린다.
// 서버의 모드(OS 설치·yum)가 state 에 저장된 모드와 달라졌으면 그 서버의 기록은 새로 시작한다(계획서 4.8).
func NewCollector(cfg *Config, hosts []string, statePath string, deps Deps, opts ...CollectorOpt) (*Collector, error) {
	c := &Collector{
		cfg: cfg, hosts: hosts, statePath: statePath, deps: deps,
		interval: time.Duration(cfg.IntervalSec) * time.Second,
		recs:     make(map[string]Record, len(hosts)),
		notify:   make(chan struct{}, 1),
	}
	for _, o := range opts {
		o(c)
	}
	old := map[string]Record{}
	if b, err := os.ReadFile(statePath); err == nil {
		var sf stateFile
		if err := json.Unmarshal(b, &sf); err != nil {
			return nil, fmt.Errorf("%s 읽기 실패: %v", statePath, err)
		}
		for _, r := range sf.Hosts {
			if sf.Version < 2 { // install_done 이 없던 형식
				r = migrateDone(r)
			}
			old[r.Name] = r
		}
		c.lastSaved = b
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("%s 읽기 실패: %v", statePath, err)
	}
	for _, h := range hosts {
		mode := ""
		if c.yum[h] {
			mode = ModeYum
		}
		r, ok := old[h]
		if !ok || r.Mode != mode { // 새 서버이거나 모드가 바뀜 -> 기록 초기화
			r = Record{Name: h, Mode: mode}
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
	if c.resErr != "" { // res 파일 쓰기 실패는 수집을 멈추지 않고 오류 줄에만 보인다
		if lastErr != "" {
			lastErr += "; "
		}
		lastErr += c.resErr
	}
	c.mu.Unlock()
	var now time.Time // yum 지연 판정 기준 시각(Now 가 없는 테스트의 Deps 도 허용)
	if c.deps.Now != nil {
		now = c.deps.Now()
	}
	s := buildSnapshot(c.cfg, recs, round, running, updated, lastErr, withYumNow(now), withOklist(c.ok))
	c.snap.Store(s)
	c.writeRes(s)
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// snapOpt 는 buildSnapshot 의 선택 설정이다(기존 호출은 그대로 컴파일된다).
type snapOpt func(*snapParams)

type snapParams struct {
	now time.Time
	ok  *Oklist
}

// withYumNow 는 yum 지연(계획서 4.8)을 판정할 기준 시각을 정한다. 주지 않으면 지연을 세지 않는다.
func withYumNow(now time.Time) snapOpt { return func(p *snapParams) { p.now = now } }

// withOklist 는 비교할 기준 설정표를 정한다(계획서 4.9). nil 이면 비교하지 않는다.
func withOklist(ok *Oklist) snapOpt { return func(p *snapParams) { p.ok = ok } }

// buildSnapshot 은 recs(호스트파일 순서)로 스냅샷을 만든다. recs 는 고치지 않는다.
func buildSnapshot(cfg *Config, in []Record, round int, running bool, updated time.Time, lastErr string, opts ...snapOpt) *Snapshot {
	var sp snapParams
	for _, o := range opts {
		o(&sp)
	}
	// 상태 순서 -> hostname 순으로 늘어놓고, 원래(호스트파일) 순서는 HostOrder 로 남긴다
	idx := make([]int, len(in))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool {
		a, b := &in[idx[i]], &in[idx[j]]
		if ra, rb := statusRank(a.Status), statusRank(b.Status); ra != rb {
			return ra < rb
		}
		return a.Name < b.Name
	})
	recs := make([]Record, len(in))
	hostOrd := make([]int, len(in))
	for k, i := range idx {
		recs[k] = in[i]
		hostOrd[i] = k
	}
	s := &Snapshot{Records: recs, Counts: map[Status]int{}, Total: len(recs),
		Round: round, Running: running, Updated: updated, LastErr: lastErr, HostOrder: hostOrd}
	if sp.ok != nil {
		s.Cfg.HasOK, s.Cfg.OK, s.Cfg.Warn = true, sp.ok.Count, sp.ok.Warn
		s.Cmp = make([]uint8, len(recs))
	}
	for k := range recs {
		r := &recs[k]
		if !r.Check.current() {
			continue
		}
		s.Cfg.Checked++
		if sp.ok == nil {
			continue
		}
		if row, ok := sp.ok.Rows[r.Name]; ok {
			m := cfgDiff(r.Check, row)
			s.Cmp[k] = cmpOn | m
			if m != 0 {
				s.Cfg.Diff++
			}
		}
	}
	groups := map[string]*VendorGroup{}
	order := cfg.VendorOrder()
	for _, v := range order {
		groups[v] = &VendorGroup{Vendor: v, Hosts: map[Status][]string{}}
	}
	yumDelay := time.Duration(cfg.YumDelayMin) * time.Minute
	// yv 는 지연 yum 서버가 있는 벤더다(처음 필요할 때 만든다).
	var yv map[string]*YumVendorGroup
	for _, r := range recs { // 이미 상태·이름 순이라 목록도 이름 순이 된다
		s.Counts[r.Status]++
		if r.IsYum() { // yum 서버는 OS 벤더 블록에 넣지 않고 Yum 에 따로 모은다
			y := &s.Yum
			y.Total++
			switch r.Status {
			case StatusYumDone:
				y.Done++
			case StatusAlive:
				y.Alive++
				y.WaitingHosts = append(y.WaitingHosts, r.Name)
			case StatusRebooted:
				y.Rebooted++
				if yumKernelSame(r) {
					y.SameKernel++
					y.SameKernelHosts = append(y.SameKernelHosts, r.Name)
				} else {
					y.RebootingHosts = append(y.RebootingHosts, r.Name)
				}
			case StatusPingX:
				y.PingX++
				y.PingXHosts = append(y.PingXHosts, r.Name)
			case StatusNone:
				y.None++
			case StatusOff:
				y.Off++
			}
			if yumDelayed(r, sp.now, yumDelay) && (r.Status == StatusAlive || r.Status == StatusRebooted) {
				y.Delayed++
				if yv == nil {
					yv = map[string]*YumVendorGroup{}
				}
				g := yv[r.Vendor]
				if g == nil {
					g = &YumVendorGroup{Vendor: r.Vendor}
					yv[r.Vendor] = g
				}
				switch {
				case r.Status == StatusAlive:
					g.Waiting = append(g.Waiting, r.Name)
				case yumKernelSame(r):
					g.SameKernel = append(g.SameKernel, r.Name)
				default:
					g.Rebooting = append(g.Rebooting, r.Name)
				}
			}
			continue
		}
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
	if s.Yum.Total > 0 {
		s.Yum.DelayMin = cfg.YumDelayMin
		// 지연 yum 서버를 벤더 설정 순서로 늘어놓는다. 설정에 없는 벤더(드묾)는 이름 순으로 뒤에 붙인다.
		for _, v := range order {
			if g := yv[v]; g != nil {
				s.Yum.Vendors = append(s.Yum.Vendors, *g)
				delete(yv, v)
			}
		}
		if len(yv) > 0 {
			rest := make([]string, 0, len(yv))
			for v := range yv {
				rest = append(rest, v)
			}
			sort.Strings(rest)
			for _, v := range rest {
				s.Yum.Vendors = append(s.Yum.Vendors, *yv[v])
			}
		}
	}
	return s
}

func (c *Collector) pendingHosts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, h := range c.hosts {
		if !c.recs[h].Completed() {
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
	// yum 서버(계획서 4.8)는 ping·22번 확인을 OS 서버와 같이 하고, gossh -pm·디스크 확인 대신 커널 조회를 한 번 한다.
	isYum := make(map[string]bool)
	c.mu.Lock()
	for _, h := range pending {
		if c.recs[h].IsYum() {
			isYum[h] = true
		}
	}
	c.mu.Unlock()
	yumObs := map[string]*YumObs{}
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
		// 2. 22번 포트 (ping O 인 서버, OS·yum 같이)
		var open, yumOpen []string
		if len(pingOK) > 0 {
			ports := c.deps.Port(pingOK, c.cfg.TCPParallel, timeout)
			for _, h := range pingOK {
				obs[h].Port = ports[h]
				if ports[h] != PortOpen {
					continue
				}
				if isYum[h] {
					yumOpen = append(yumOpen, h)
				} else {
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
		// 4-1. yum 커널·부팅 시각 조회: 22번이 열린 yum 서버에 gossh 한 번. 실패하면 그 서버들은 ssh 불가로 본다(상태 유지).
		if len(yumOpen) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			yf := c.deps.Yum
			if yf == nil {
				yf = YumCheck
			}
			res, err := yf(c.cfg, yumOpen)
			if err != nil {
				errs = append(errs, "yum 커널 조회: "+err.Error())
			}
			for _, h := range yumOpen {
				o, ok := res[h]
				if err != nil || !ok {
					o = YumObs{Err: "응답 없음"}
					if err != nil {
						o.Err = "gossh 실패"
					}
				}
				yumObs[h] = &o
			}
		}
	}
	// 5. 상태 갱신·저장
	now := c.deps.Now()
	delay := time.Duration(c.cfg.DelayMin) * time.Minute
	c.mu.Lock()
	for _, h := range pending {
		if isYum[h] {
			o := obs[h]
			c.recs[h] = stickDone(applyYum(c.recs[h], o.Ping, o.Port, yumObs[h], now))
			continue
		}
		c.recs[h] = stickDone(apply(c.recs[h], *obs[h], now, delay))
	}
	c.mu.Unlock()
	if err := c.save(); err != nil {
		errs = append(errs, "state 저장: "+err.Error())
	}
	// 6. 설정 자동 첫 조사(계획서 4.9): 상태 갱신 뒤, 아직 결과가 없는 대상 서버 전부(대수 제한 없음, 동시 실행 제한은 gossh).
	// 실패한 서버는 결과가 비어 있으니 다음 회차에 다시 대상이 되고, 성공한 서버는 자동으로 다시 조사하지 않는다.
	if c.deps.Cfg != nil && ctx.Err() == nil {
		if t := c.autoTargets(); len(t) > 0 {
			res, fails, err := c.deps.Cfg(c.cfg, t)
			if err != nil {
				errs = append(errs, "설정 조사: "+err.Error())
			} else {
				c.applyChecks(res, fails)
				if err := c.save(); err != nil {
					errs = append(errs, "state 저장: "+err.Error())
				}
			}
		}
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
		if c.recs[h].Completed() {
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
		c.recs[h] = stickDone(ApplyCompletedPing(c.recs[h], res[h], now))
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
	return writeFileAtomicMode(path, data, 0)
}

// writeFileAtomicMode 는 같은 폴더의 임시 파일에 쓰고 이름을 바꾼다. mode 가 0 이 아니면 그 권한으로 바꾼다.
func writeFileAtomicMode(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pm_state_*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if mode != 0 {
		if err := tmp.Chmod(mode); err != nil {
			tmp.Close()
			os.Remove(name)
			return err
		}
	}
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
