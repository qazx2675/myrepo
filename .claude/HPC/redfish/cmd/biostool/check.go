package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// check.go 는 `biostool check` 의 점검 본체입니다 (읽기 전용, ModeReadOnly).
//
// 호스트마다 Login → detectSystem → Bios Attributes(현재값) + SettingsObject Attributes(Pending)
// 를 읽어 프로파일의 항목별로 판정하고, 결과를 results/<일시>/ 에 TSV 로 남깁니다.
// 같은 판정 로직(evaluate)을 온라인(Client)과 -from-dump(덤프 파일 읽기)가 getFunc 만 바꿔 공유합니다.
// 설정(Pending 쓰기)은 하지 않습니다 — Y 이후의 설정과 -dry-run 은 set.go 입니다.

// 판정 상태 (항목 단위). 호스트 단위 오류는 redfish.go 의 StatusXxx 와 StatusNoHostsEntry 를 그대로 씁니다.
const (
	StatusOK             = "OK"              // 현재값 = 기대값
	StatusPendingOK      = "PENDING_OK"      // 현재값 ≠ 기대값이지만 Pending 이 기대값 (재부팅 대기)
	StatusFail           = "FAIL"            // 불일치 (verified=Y)
	StatusUnverified     = "UNVERIFIED"      // 불일치지만 verified≠Y → 설정 불가
	StatusPendingExists  = "PENDING_EXISTS"  // FAIL 이지만 남의 Pending 이 있어 설정 제외
	StatusMappingMissing = "MAPPING_MISSING" // 프로파일 행이 없거나 후보 속성이 BMC 에 없음
	StatusNoDump         = "NO_DUMP"         // -from-dump 에서 대상에 맞는 덤프 폴더가 없음
	StatusPendingNoJob   = "PENDING_NO_JOB"  // Dell: Pending 은 기대값이나 예약된 BIOS 설정 Job 이 없음 → 재부팅해도 반영 안 될 수 있음
)

// checkClientGap 은 점검 때 같은 BMC 로 가는 연속 요청 간격입니다 (0 이면 Client 기본 100ms).
// 테스트가 -1 로 바꿔 쓰며 운영에서는 바꾸지 않습니다.
var checkClientGap time.Duration

// itemResult 는 호스트 1대의 표준 항목 1개 판정입니다.
type itemResult struct {
	Std      string
	Attr     string // BMC 의 실제 속성 이름 (매핑 실패면 프로파일 첫 후보, 행이 없으면 "-")
	Expected string // 프로파일 value 원문 (A|B 포함)
	Current  string
	Pending  string // 이 속성의 Pending 값 (없으면 "-")
	Result   string
	Reason   string // 사람이 읽는 사유 (리포트용, TSV 에는 없음)
	Verified bool
	// JobUnknown 은 Dell PENDING_OK 인데 예약된 BIOS 설정 Job 이 있는지 확인하지 못했음을 뜻합니다 (리포트 경고용).
	JobUnknown bool
}

// hostCheck 는 호스트 1대의 점검 결과입니다. Status 가 비어 있으면 점검이 끝난 것이고(Items 참고),
// 아니면 호스트 단위 오류 상태입니다.
type hostCheck struct {
	Target  Target
	Status  string
	HTTP    int
	Detail  string // 오류 요약 (비밀번호·토큰 없음)
	Info    *SystemInfo
	Items   []itemResult
	Cands   []stdMatch // MAPPING_MISSING 호스트의 이름 후보 (표준 4항목)
	Foreign int        // 우리 항목이 아닌 속성 중 Pending 이 현재값과 다른 개수
	Jobs    int        // Dell Jobs 컬렉션 멤버 수 (정보용, 모르면 -1)
	Dump    string     // MAPPING_MISSING 덤프 저장 위치(결과 폴더 기준) 또는 사유
	Stats   CallStats
	Notes   []string
}

func (h *hostCheck) setErr(err error) {
	h.Status, h.HTTP = errStatus(err)
	h.Detail = errBrief(err)
	var rf *RFError
	if errors.As(err, &rf) && rf.Detail != "" {
		h.Detail = rf.Detail
	}
}

// statePrecedence 는 호스트 대표 상태를 고르는 순서입니다 (앞이 더 나쁨).
var statePrecedence = []string{
	StatusFail, StatusPendingNoJob, StatusPendingExists, StatusUnverified, StatusMappingMissing, StatusPendingOK, StatusOK,
}

// state 는 호스트 대표 상태입니다: 호스트 오류면 그 상태, 아니면 항목 중 가장 나쁜 것.
func (h *hostCheck) state() string {
	if h.Status != "" {
		return h.Status
	}
	for _, st := range statePrecedence {
		if h.hasStatus(st) {
			return st
		}
	}
	return StatusOK
}

// allGood 는 모든 항목이 OK/PENDING_OK 인지 (ok.txt 대상).
func (h *hostCheck) allGood() bool {
	s := h.state()
	return s == StatusOK || s == StatusPendingOK
}

// settable 은 Y 응답 시 설정 대상이 되는 항목인지 봅니다: FAIL, 또는 verified=Y 이고 남의 Pending 이 없는
// PENDING_NO_JOB (같은 값을 다시 PATCH 하고 Dell BIOS 설정 Job 을 만든다 — 기존 PATCH+Job 경로 그대로).
func (h *hostCheck) settable(it itemResult) bool {
	return it.Result == StatusFail || (it.Result == StatusPendingNoJob && it.Verified && h.Foreign == 0)
}

func (h *hostCheck) hasStatus(st string) bool {
	for _, it := range h.Items {
		if it.Result == st {
			return true
		}
	}
	return false
}

// isRetryStatus 는 retry.txt 에 넣을 호스트 상태입니다. AUTH_FAIL 은 넣지 않고(계정부터 고쳐야 함),
// 차단기가 건너뛴 SKIPPED_AUTH_STOP 은 넣습니다.
func isRetryStatus(s string) bool {
	return s == StatusUnreachable || s == StatusTimeout || s == StatusBMCError || s == StatusSkippedAuthStop
}

// ---- 호스트별 호출 도우미 ----

// memoGet 은 호스트 1대 안에서 같은 경로를 두 번 받지 않게 합니다 (성공·실패 모두 기억). 한 고루틴 전용.
type memoGet struct {
	get getFunc
	m   map[string]memoRes
}

type memoRes struct {
	b   []byte
	err error
}

func newMemoGet(get getFunc) *memoGet { return &memoGet{get: get, m: map[string]memoRes{}} }

func (m *memoGet) fetch(p string) ([]byte, error) {
	_, key := pathKey(p)
	if r, ok := m.m[key]; ok {
		return r.b, r.err
	}
	b, err := m.get(p)
	m.m[key] = memoRes{b, err}
	return b, err
}

// regCache 는 속성 레지스트리를 `벤더|모델|BIOS버전` 키로 프로세스 안에서 한 번만 내려받게 합니다.
// 레지스트리는 크므로(수 MB) 같은 기종 호스트 수천 대가 각각 받지 않도록 하며,
// 이름 후보를 보여 줄 때(MAPPING_MISSING)에만 씁니다.
type regCache struct {
	mu sync.Mutex
	m  map[string]*regEntry
}

type regEntry struct {
	once sync.Once
	reg  *attrRegistry
}

func (c *regCache) get(key string, load func() *attrRegistry) *attrRegistry {
	c.mu.Lock()
	e := c.m[key]
	if e == nil {
		e = &regEntry{}
		c.m[key] = e
	}
	c.mu.Unlock()
	e.once.Do(func() { e.reg = load() })
	return e.reg
}

// mmDumper 는 MAPPING_MISSING 호스트의 덤프를 모델당 1대만 저장합니다.
type mmDumper struct {
	dir  string // results/<런>/mapping_missing
	mu   sync.Mutex
	seen map[string]bool
	n    int
}

func (m *mmDumper) claim(info *SystemInfo) bool {
	key := strings.ToLower(info.Vendor) + "|" + normalizeModel(info.Model)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen[key] {
		return false
	}
	m.seen[key] = true
	m.n++
	return true
}

// record 는 덤프 결과를 index.tsv 와 summary.txt(사람이 읽고 전달할 요약)에 덧붙입니다.
func (m *mmDumper) record(r *dumpResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := appendIndex(m.dir, []*dumpResult{r}); err != nil {
		return err
	}
	var b strings.Builder
	writeDumpSummary(&b, []*dumpResult{r}, dumpOpts{Out: m.dir})
	f, err := os.OpenFile(filepath.Join(m.dir, "summary.txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ---- 실행 ----

type checkOpts struct {
	Profile      *Profile
	FromDump     string
	ResultDir    string
	RetryFromDir string    // 단계 7: -retry-from 값 (이전 결과 폴더 또는 그 안의 retry.txt). 비면 일반 점검
	Now          time.Time // 결과 폴더 이름·리포트 시각 (0 이면 지금)
}

// checkRun 은 점검 1회의 전체 결과입니다.
type checkRun struct {
	Profile      string
	Now          time.Time
	Dir          string // results/<일시>
	FromDump     string
	Hosts        []*hostCheck // 입력 순서
	MMModels     int          // MAPPING_MISSING 덤프를 저장한 모델 수
	AuthStop     int          // conf auth_fail_stop (0 이면 차단기 비활성)
	prof         *Profile     // 점검에 쓴 프로파일 (set 이 항목을 다시 검증할 때 씀)
	RetryFromDir string       // 단계 7: 이전 결과 폴더 (재시도 모드일 때만)
	prevResult   *tsvTable    // 단계 7: 이전 결과 (병합 바탕, 읽기만 한다)
	Retry        *retryMerge  // 단계 7: 병합 결과 (병합했을 때만)
}

type checkEnv struct {
	conf     *Config
	password string
	prof     *Profile
	fromDump map[string]string // 소문자 폴더 이름 → 호스트 덤프 폴더
	offline  bool
	regs     *regCache
	mm       *mmDumper
}

// newRunDir 은 results/<YYYYMMDD_HHMMSS> 를 만듭니다 (같은 초에 또 실행하면 _2, _3 ...).
func newRunDir(base string, now time.Time) (string, error) {
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	name := now.Format("20060102_150405")
	for i := 1; i <= 100; i++ {
		d := filepath.Join(base, name)
		if i > 1 {
			d = fmt.Sprintf("%s_%d", d, i)
		}
		err := os.Mkdir(d, 0o700)
		if err == nil {
			return d, nil
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	return "", fmt.Errorf("결과 폴더를 만들 수 없습니다: %s", filepath.Join(base, name))
}

// doCheck 는 대상 전체를 점검하고 결과 파일을 씁니다. 파일 쓰기에 실패해도 점검 결과(run)는 돌려줍니다.
func doCheck(rc *runContext, o checkOpts) (*checkRun, error) {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	// 재시도 모드: 이전 결과를 먼저 읽어 검증한다 (접속·폴더 생성 전에 실패하면 아무것도 남기지 않는다).
	var prevDir string
	var prev *tsvTable
	if o.RetryFromDir != "" {
		var perr error
		prevDir = retryPrevDir(o.RetryFromDir)
		if prev, perr = loadPrevCheck(prevDir, o.Profile.Name); perr != nil {
			return nil, perr
		}
	}
	dir, err := newRunDir(o.ResultDir, now)
	if err != nil {
		return nil, fmt.Errorf("결과 폴더: %w", err)
	}
	env := &checkEnv{
		conf: rc.Conf, password: rc.Password, prof: o.Profile,
		regs: &regCache{m: map[string]*regEntry{}},
	}
	run := &checkRun{Profile: o.Profile.Name, Now: now, Dir: dir, FromDump: o.FromDump, prof: o.Profile, RetryFromDir: prevDir, prevResult: prev}
	if o.FromDump != "" {
		env.offline = true
		if env.fromDump, err = indexDumpHosts(o.FromDump); err != nil {
			return nil, err
		}
	} else {
		env.mm = &mmDumper{dir: filepath.Join(dir, "mapping_missing"), seen: map[string]bool{}}
	}

	run.Hosts = make([]*hostCheck, len(rc.Targets))
	authStop := rc.Conf.AuthFailStop
	if env.offline {
		authStop = 0 // 덤프 읽기는 BMC 에 로그인하지 않는다
	}
	run.AuthStop = authStop
	sweepHosts(len(rc.Targets), rc.Conf.Concurrency, authStop,
		func(i int) hostOutcome {
			h := env.checkHost(rc.Targets[i])
			run.Hosts[i] = h
			return outcomeOf(h.Status, h.Stats, h.Target.Err == "")
		},
		func(i int) {
			h := newHostCheck(rc.Targets[i])
			h.Status = StatusSkippedAuthStop
			if h.Target.Err != "" {
				h.Status = h.Target.Err // 이름 해석 실패는 원래 상태를 유지한다
			}
			run.Hosts[i] = h
		})
	if env.mm != nil {
		run.MMModels = env.mm.n
	}
	return run, writeResultFiles(run)
}

func (e *checkEnv) checkHost(t Target) *hostCheck {
	if e.offline {
		return e.checkOffline(t)
	}
	return e.checkOnline(t)
}

func newHostCheck(t Target) *hostCheck { return &hostCheck{Target: t, Jobs: -1} }

func (e *checkEnv) checkOnline(t Target) *hostCheck {
	h := newHostCheck(t)
	if t.Err != "" {
		h.Status = t.Err
		return h
	}
	c := NewClient(ClientOpts{
		BaseURL: t.BaseURL(), User: e.conf.User, Pass: e.password, Insecure: e.conf.Insecure,
		Timeout: e.conf.Timeout, Retries: e.conf.Retries, Mode: ModeReadOnly, Gap: checkClientGap,
	})
	closed := false
	defer func() {
		if !closed {
			if err := c.Close(); err != nil {
				h.Notes = append(h.Notes, "세션 삭제: "+errBrief(err))
			}
		}
		h.Stats = c.Stats()
	}()
	if err := c.Login(); err != nil {
		h.setErr(err)
		return h
	}
	e.evaluate(h, newMemoGet(c.GetRaw))

	// 매핑 없는 모델은 같은 세션으로 덤프를 모델당 1대만 저장한다 (사람이 요약을 보고 프로파일을 채운다).
	if h.Status == "" && h.hasStatus(StatusMappingMissing) && h.Info != nil && e.mm.claim(h.Info) {
		r := dumpWith(c, t, dumpOpts{Out: e.mm.dir})
		closed = true
		for _, n := range r.Notes {
			h.Notes = append(h.Notes, "덤프: "+n)
		}
		if r.RelDir != "" {
			h.Dump = filepath.ToSlash(filepath.Join("mapping_missing", r.RelDir))
		} else {
			h.Dump = "저장 실패(" + r.Status + ")"
		}
		if err := e.mm.record(r); err != nil {
			h.Notes = append(h.Notes, "덤프 색인 기록 실패: "+err.Error())
		}
	}
	return h
}

func (e *checkEnv) checkOffline(t Target) *hostCheck {
	h := newHostCheck(t)
	dir, ok := e.fromDump[strings.ToLower(dirPart(t.Hostname))]
	if !ok {
		h.Status = StatusNoDump
		if t.Err != "" {
			h.Status = t.Err
		}
		return h
	}
	get, err := newDumpFileGet(dir)
	if err != nil {
		h.Status, h.Detail = StatusHTTPError, "덤프 폴더를 읽을 수 없습니다: "+err.Error()
		return h
	}
	e.evaluate(h, newMemoGet(get))
	return h
}

// evaluate 는 호스트 1대의 데이터를 읽어 항목별로 판정합니다 (온라인·오프라인 공용).
func (e *checkEnv) evaluate(h *hostCheck, m *memoGet) {
	info, err := detectSystemWith(m.fetch)
	h.Info = info
	if err != nil {
		h.setErr(err)
		return
	}
	if info.BiosPath == "" {
		h.Status, h.Detail = StatusUnsupported, "System 에 Bios 리소스가 없습니다"
		return
	}
	bb, err := m.fetch(info.BiosPath)
	if err != nil {
		h.setErr(err)
		return
	}
	attrs, err := parseAttrs(bb)
	if err != nil {
		h.setErr(err)
		return
	}
	if len(attrs) == 0 {
		h.Status, h.Detail = StatusUnsupported, "Bios Attributes 가 비어 있습니다"
		return
	}
	pend := map[string]string{}
	if info.SettingsPath != "" {
		sb, err := m.fetch(info.SettingsPath)
		if err == nil {
			if p, perr := parseAttrs(sb); perr == nil {
				pend = p
			}
		} else if st, _ := errStatus(err); st == StatusUnreachable || st == StatusTimeout || st == StatusBMCError || st == StatusAuthFail {
			// BMC 가 일시적으로 불안정하면 Pending 을 모른 채 판정하지 않고 재시도 대상으로 둔다.
			// 404 같은 영구 오류(Settings 미지원)는 Pending 없음으로 본다.
			h.setErr(err)
			return
		}
	}
	e.judge(h, m, info, attrs, pend)
}

// lookupAttr 은 프로파일의 속성 이름을 BMC 의 실제 이름으로 찾습니다 (정확히 일치 우선, 없으면 대소문자 무시).
func lookupAttr(attrs map[string]string, name string) (string, bool) {
	if _, ok := attrs[name]; ok {
		return name, true
	}
	best := ""
	for k := range attrs {
		if strings.EqualFold(k, name) && (best == "" || k < best) {
			best = k
		}
	}
	return best, best != ""
}

// matchAny 는 v 가 허용값 중 하나와 같은지 봅니다. 둘 다 10진 정수면 정수로 비교합니다
// (정수 속성에서 프로파일 "05" 와 BMC 의 5 가 매번 FAIL 로 보이지 않게; set.go 가 보내는 정규형과 같은 규칙).
func matchAny(allowed []string, v string) bool {
	v = strings.TrimSpace(v)
	vn, verr := strconv.ParseInt(v, 10, 64)
	for _, a := range allowed {
		if a == v {
			return true
		}
		if an, aerr := strconv.ParseInt(a, 10, 64); verr == nil && aerr == nil && an == vn {
			return true
		}
	}
	return false
}

// judge 는 표준 항목별로 상태를 정합니다.
//
// 불일치(현재값 ∉ 기대값)일 때: Pending 이 기대값이면 PENDING_OK, 아니면
// verified=N 이면 UNVERIFIED, 남의 Pending 이 있으면 PENDING_EXISTS, 그 외 FAIL.
// "남의 Pending" 은 (1) 우리 항목이 아닌 속성 중 Pending 값이 현재값과 다른 것이 있거나
// (2) 우리 속성의 Pending 값이 기대값도 현재값도 아닌 경우입니다. Pending 값이 현재값과 같으면 변경이 아니므로 무시합니다.
// Dell 의 PENDING_OK 는 예약된 BIOS 설정 Job 이 없다고 확인되면 PENDING_NO_JOB 으로 바꿉니다 (Job 목록을 GET 만 함).
func (e *checkEnv) judge(h *hostCheck, m *memoGet, info *SystemInfo, attrs, pend map[string]string) {
	type pick struct {
		rows []profRow
		row  *profRow
		name string
	}
	picks := make([]pick, len(e.prof.Items))
	ours := map[string]bool{}
	for i, std := range e.prof.Items {
		rows := e.prof.rowsFor(info.Vendor, info.Model, std)
		picks[i].rows = rows
		for j := range rows {
			if name, ok := lookupAttr(attrs, rows[j].Attr); ok {
				picks[i].row, picks[i].name = &rows[j], name
				ours[name] = true
				break
			}
		}
	}
	for k, v := range pend {
		if ours[k] {
			continue
		}
		if cur, ok := attrs[k]; !ok || cur != v {
			h.Foreign++
		}
	}

	who := strings.TrimSpace(info.Vendor + " " + info.Model)
	for i, std := range e.prof.Items {
		p := picks[i]
		it := itemResult{Std: std, Attr: "-", Expected: "-", Current: "-", Pending: "-"}
		switch {
		case len(p.rows) == 0:
			it.Result = StatusMappingMissing
			it.Reason = "프로파일에 " + who + " 행이 없음"
		case p.row == nil:
			it.Result = StatusMappingMissing
			it.Attr, it.Expected = p.rows[0].Attr, p.rows[0].Value
			names := make([]string, len(p.rows))
			for j, r := range p.rows {
				names[j] = r.Attr
			}
			it.Reason = "프로파일 후보 속성(" + strings.Join(names, ", ") + ")이 BMC 에 없음"
		default:
			row := p.row
			it.Attr, it.Expected, it.Verified = p.name, row.Value, row.Verified
			it.Current = attrs[p.name]
			pv, hasPend := pend[p.name]
			if hasPend {
				it.Pending = pv
			}
			ownConflict := hasPend && !matchAny(row.Allowed, pv) && pv != it.Current
			switch {
			case matchAny(row.Allowed, it.Current):
				it.Result = StatusOK
			case hasPend && matchAny(row.Allowed, pv):
				it.Result = StatusPendingOK
				it.Reason = "재부팅 대기 중 (Pending=기대값)"
			case !row.Verified:
				it.Result = StatusUnverified
				it.Reason = "프로파일 행이 verified=N (설정하지 않음)"
			case ownConflict:
				it.Result = StatusPendingExists
				it.Reason = "이 속성에 기대값도 현재값도 아닌 Pending 값(" + pv + ")이 있음"
			case h.Foreign > 0:
				it.Result = StatusPendingExists
				it.Reason = fmt.Sprintf("다른 속성 Pending %d건이 이미 있음", h.Foreign)
			default:
				it.Result = StatusFail
			}
		}
		h.Items = append(h.Items, it)
	}

	// Dell: Pending 이 기대값이어도 예약된 BIOS 설정 Job 이 없으면 재부팅해도 반영되지 않을 수 있다 (GET 만 함).
	if info.Vendor == "Dell" && info.ManagerPath != "" && h.hasStatus(StatusPendingOK) {
		found, known := dellBiosJob(m.fetch, strings.TrimRight(info.ManagerPath, "/")+"/Jobs")
		for i := range h.Items {
			it := &h.Items[i]
			if it.Result != StatusPendingOK {
				continue
			}
			switch {
			case found:
				it.Reason += ", BIOS 설정 Job 예약 확인"
			case !known:
				it.JobUnknown = true
				it.Reason += ", BIOS 설정 Job 확인 불가 — iDRAC Job Queue 확인 필요"
			default:
				it.Result = StatusPendingNoJob
				it.Reason = "Pending 은 기대값이나 예약된 BIOS 설정 Job 이 없음 — 재부팅해도 반영되지 않을 수 있음"
			}
		}
	}

	if h.hasStatus(StatusMappingMissing) {
		var reg *attrRegistry
		if info.RegistryPath != "" {
			key := strings.ToLower(info.Vendor + "|" + info.Model + "|" + info.BiosVersion)
			reg = e.regs.get(key, func() *attrRegistry {
				b, err := m.fetch(info.RegistryPath)
				if err != nil {
					return nil
				}
				r, err := parseRegistry(b)
				if err != nil {
					return nil
				}
				return r
			})
		}
		h.Cands = findStdCandidates(attrs, reg)
	}
	if h.hasStatus(StatusPendingExists) && info.Vendor == "Dell" && info.ManagerPath != "" {
		jp := strings.TrimRight(info.ManagerPath, "/") + "/Jobs"
		if b, err := m.fetch(jp); err == nil {
			h.Jobs = membersCount(b)
		}
	}
}

// maxJobScan 은 Dell Job 목록에서 BIOS 설정 Job 을 찾으려고 GET 할 최대 멤버 수입니다 (목록 뒤쪽, 최근 것부터).
const maxJobScan = 20

// pendingJobStates 는 "아직 실행 전(다음 재부팅 때 실행)" 으로 보는 Job 상태입니다 (소문자).
var pendingJobStates = map[string]bool{"scheduled": true, "scheduling": true, "new": true, "pending": true}

// dellBiosJob 은 Dell Jobs 컬렉션에 예약된 BIOS 설정 Job(JobType=BIOSConfiguration, 실행 전 상태)이 있는지 봅니다 (GET 만).
// known 이 false 면 조회 실패나 멤버가 너무 많아(maxJobScan 초과) 있는지 없는지 모르는 것입니다.
func dellBiosJob(get getFunc, jobsPath string) (found, known bool) {
	b, err := get(jobsPath)
	if err != nil {
		return false, false
	}
	ms, err := membersOf(b)
	if err != nil {
		return false, false
	}
	known = len(ms) <= maxJobScan
	for i, n := len(ms)-1, 0; i >= 0 && n < maxJobScan; i, n = i-1, n+1 {
		ok, err := isBiosJob(get, jobsPath, ms[i])
		if ok {
			return true, true
		}
		if err != nil {
			known = false
		}
	}
	return false, known
}

// isBiosJob 은 Job 1개를 GET 해 실행 전 BIOS 설정 Job 인지 봅니다. id 는 jobsPath 바로 아래(Jobs/<id>)만 읽습니다.
func isBiosJob(get getFunc, jobsPath, id string) (bool, error) {
	jc, _, err1 := normalizePath(jobsPath)
	ic, _, err2 := normalizePath(id)
	if err1 != nil || err2 != nil {
		return false, nil
	}
	rest := strings.TrimPrefix(strings.ToLower(ic), strings.ToLower(jc)+"/")
	if rest == strings.ToLower(ic) || rest == "" || strings.Contains(rest, "/") {
		return false, nil // Jobs 바로 아래가 아니면 읽지 않는다
	}
	b, err := get(id)
	if err != nil {
		return false, err
	}
	var j struct{ JobType, JobState string }
	if json.Unmarshal(b, &j) != nil {
		return false, badJSON("Job")
	}
	return strings.EqualFold(j.JobType, "BIOSConfiguration") && pendingJobStates[strings.ToLower(j.JobState)], nil
}

// ---- -from-dump ----

// indexDumpHosts 는 dir 아래에서 redfish/v1.json 을 가진 호스트 폴더를 모두 찾아
// 소문자 폴더 이름 → 폴더 경로 표를 만듭니다. 같은 이름이 여러 곳이면 v1.json 이 가장 최근인 것을 씁니다.
func indexDumpHosts(dir string) (map[string]string, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("-from-dump 폴더 %s: %w", dir, err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("-from-dump 는 폴더여야 합니다: %s", dir)
	}
	type cand struct {
		dir string
		mod time.Time
	}
	best := map[string]cand{}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || !strings.EqualFold(d.Name(), "redfish") {
			return nil
		}
		fi, serr := os.Stat(filepath.Join(p, "v1.json"))
		if serr != nil || fi.IsDir() {
			return nil
		}
		host := filepath.Dir(p)
		key := strings.ToLower(filepath.Base(host))
		if c, ok := best[key]; !ok || fi.ModTime().After(c.mod) || (fi.ModTime().Equal(c.mod) && host > c.dir) {
			best[key] = cand{dir: host, mod: fi.ModTime()}
		}
		return filepath.SkipDir
	})
	if err != nil {
		return nil, fmt.Errorf("-from-dump 폴더 %s: %w", dir, err)
	}
	out := make(map[string]string, len(best))
	for k, c := range best {
		out[k] = c.dir
	}
	return out, nil
}

// newDumpFileGet 은 호스트 덤프 폴더를 읽는 getFunc 입니다 (dump 의 저장 규칙과 같은 경로 변환, 대소문자 무시).
// 온라인과 같은 허용목록(checkAllowed)을 거치므로 LogServices 같은 경로는 읽지 않습니다.
func newDumpFileGet(hostDir string) (getFunc, error) {
	idx := map[string]string{}
	err := filepath.WalkDir(hostDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(hostDir, p)
		if rerr != nil {
			return rerr
		}
		idx[strings.ToLower(filepath.ToSlash(rel))] = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return func(p string) ([]byte, error) {
		if _, err := checkAllowed("GET", p, ModeReadOnly, ""); err != nil {
			return nil, err
		}
		clean, _, err := normalizePath(p)
		if err != nil {
			return nil, err
		}
		fp, ok := idx[strings.ToLower(resourceFile(clean))]
		if !ok {
			return nil, &RFError{Status: StatusHTTPError, HTTP: 404, Detail: "덤프에 없는 리소스"}
		}
		b, err := os.ReadFile(fp)
		if err != nil {
			return nil, &RFError{Status: StatusHTTPError, Detail: "덤프 파일을 읽을 수 없습니다"}
		}
		if !json.Valid(b) {
			return nil, badJSON("덤프")
		}
		return b, nil
	}, nil
}

// ---- 결과 파일 ----

var resultHeader = []string{"hostname", "ip", "profile", "vendor", "model", "bios_version", "std_name", "attribute", "expected", "current", "pending", "result"}

// rows 는 호스트 1대의 result.tsv 행입니다: 항목마다 1행, 호스트 단위 오류는 std_name "-" 1행.
func (r *checkRun) rows(h *hostCheck) [][]string {
	vendor, model, bios := "-", "-", "-"
	if h.Info != nil {
		vendor, model, bios = tsvField(h.Info.Vendor), tsvField(h.Info.Model), tsvField(h.Info.BiosVersion)
	}
	base := []string{tsvField(h.Target.Hostname), tsvField(h.Target.IP), tsvField(r.Profile), vendor, model, bios}
	row := func(tail ...string) []string {
		out := append([]string(nil), base...)
		for _, s := range tail {
			out = append(out, tsvField(s))
		}
		return out
	}
	if h.Status != "" {
		return [][]string{row("-", "-", "-", "-", "-", h.Status)}
	}
	out := make([][]string, 0, len(h.Items))
	for _, it := range h.Items {
		out = append(out, row(it.Std, it.Attr, it.Expected, it.Current, it.Pending, it.Result))
	}
	return out
}

func writeTSV(path string, header []string, rows [][]string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, strings.Join(header, "\t"))
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeLines(path string, lines []string) error {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// writeResultFiles 는 result.tsv, ok.txt, fail.tsv, retry.txt, run_info.txt 를 씁니다 (모두 0600).
// 재시도 모드(-retry-from)면 이어서 merged_* 병합 파일도 씁니다 (retry.go).
func writeResultFiles(r *checkRun) error {
	var all, fail [][]string
	var ok, retry []string
	for _, h := range r.Hosts {
		rows := r.rows(h)
		all = append(all, rows...)
		if h.Status != "" {
			if isRetryStatus(h.Status) {
				retry = append(retry, h.Target.Input)
			}
			continue
		}
		if h.allGood() {
			ok = append(ok, h.Target.Hostname)
		}
		for i, it := range h.Items {
			switch it.Result {
			case StatusFail, StatusUnverified, StatusPendingExists, StatusMappingMissing, StatusPendingNoJob:
				fail = append(fail, rows[i])
			}
		}
	}
	if err := writeTSV(filepath.Join(r.Dir, "result.tsv"), resultHeader, all); err != nil {
		return err
	}
	if err := writeLines(filepath.Join(r.Dir, "ok.txt"), ok); err != nil {
		return err
	}
	if err := writeTSV(filepath.Join(r.Dir, "fail.tsv"), resultHeader, fail); err != nil {
		return err
	}
	if err := writeLines(filepath.Join(r.Dir, "retry.txt"), retry); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "run_info.txt"), []byte(r.runInfo()), 0o600); err != nil {
		return err
	}
	if r.prevResult != nil {
		return r.writeMergedResults(all)
	}
	return nil
}

// tally 는 상태별 호스트 수(대표 상태 기준)와 항목 수입니다.
func (r *checkRun) tally() (hosts, items map[string]int) {
	hosts, items = map[string]int{}, map[string]int{}
	for _, h := range r.Hosts {
		hosts[h.state()]++
		for _, it := range h.Items {
			items[it.Result]++
		}
	}
	return hosts, items
}

func sortedKeys(m map[string]int) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// runInfo 는 run_info.txt 본문입니다 (비밀번호·토큰·계정 없음).
func (r *checkRun) runInfo() string {
	hosts, items := r.tally()
	var b strings.Builder
	fmt.Fprintf(&b, "도구: biostool %s (check, 읽기 전용)\n", toolVersion)
	fmt.Fprintf(&b, "프로파일: %s\n", r.Profile)
	fmt.Fprintf(&b, "시각: %s\n", r.Now.Format("2006-01-02 15:04:05"))
	if r.FromDump != "" {
		fmt.Fprintf(&b, "모드: 덤프 읽기 (%s), 접속 안 함\n", r.FromDump)
	} else {
		b.WriteString("모드: 실제 접속\n")
	}
	fmt.Fprintf(&b, "대상 수: %d\n", len(r.Hosts))
	if r.AuthStop > 0 {
		fmt.Fprintf(&b, "AUTH_FAIL 차단기: auth_fail_stop=%d\n", r.AuthStop)
	}
	if w := authStopWarning(hosts[StatusAuthFail], hosts[StatusSkippedAuthStop], authStopRerunCheck); w != "" {
		b.WriteString("경고: " + w + "\n")
	}
	b.WriteString("호스트 기준 집계 (대표 상태):\n")
	for _, k := range sortedKeys(hosts) {
		fmt.Fprintf(&b, "  %s: %d\n", k, hosts[k])
	}
	b.WriteString("항목 기준 집계:\n")
	for _, k := range sortedKeys(items) {
		fmt.Fprintf(&b, "  %s: %d\n", k, items[k])
	}
	var sess, gets int
	for _, h := range r.Hosts {
		sess += h.Stats.Sessions
		gets += h.Stats.Gets
	}
	fmt.Fprintf(&b, "BMC 호출: 세션 생성 %d, GET %d\n", sess, gets)
	fmt.Fprintf(&b, "MAPPING_MISSING 덤프 저장 모델 수: %d\n", r.MMModels)
	return b.String()
}
