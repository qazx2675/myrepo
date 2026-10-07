package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

// allcheck.go 는 `biostool allcheck` (all_bios_check.sh) 의 본체입니다 (계획서 8장, 읽기 전용 ModeReadOnly).
//
// diff.txt 의 기준(정상) 호스트와 user.txt 의 대상 호스트를 BMC 에서 읽어 모델을 자동으로 알아내고,
// 같은 모델끼리 Bios.Attributes 전체를 비교합니다. 호스트마다 GET 은 ServiceRoot → Systems → System → Bios
// 4번뿐입니다 (속성 레지스트리·Settings·Managers 는 읽지 않으므로 detectSystem 을 쓰지 않고 가볍게 읽습니다).
//
// 순서: ① 기준 호스트를 먼저 모두 읽는다 ② 대상을 읽으면서 바로 같은 모델의 기준과 비교하고 속성 값은 버린다
// (수천 대의 Attributes 를 메모리에 쌓지 않기 위함). 두 단계가 AUTH_FAIL 차단기 예산을 나눠 쓴다.
// 온라인과 -from-dump(덤프 읽기)는 getFunc 만 다르고 같은 코드를 탄다.

// 호스트 단위 결과 상태 (오류 상태는 redfish.go·sweep.go·resolve.go·check.go 의 것을 그대로 쓴다).
const (
	StatusSame           = "SAME"            // 제외 속성을 뺀 모든 속성이 기준과 같다
	StatusDiff           = "DIFF"            // 차이 있음 (속성 단위 상태는 아래 attrStatus)
	StatusIsReference    = "REFERENCE"       // 기준 호스트 자신이라 같은 모델의 다른 기준이 없어 비교하지 않음
	StatusNoReference    = "NO_REFERENCE"    // 이 모델의 기준 호스트가 diff.txt 에 없음
	StatusRefUnreachable = "REF_UNREACHABLE" // 이 모델의 기준일 수 있는 호스트가 모두 접속 실패
)

// 속성 단위 상태 (all_diff.tsv 의 status. SAME 은 행으로 남기지 않고 summary 의 개수로만 센다).
const (
	attrDiff     = "DIFF"      // 값이 다름
	attrOnlyRef  = "ONLY_REF"  // 기준에만 있음
	attrOnlyHost = "ONLY_HOST" // 대상에만 있음
)

// 특이사항 코드 (summary.tsv 의 notes, 터미널 [특이사항]).
const (
	noteBiosVerDiff          = "BIOS_VER_DIFF"
	noteMultiRef             = "MULTI_REF"
	noteDiffTxtModelMismatch = "DIFF_TXT_MODEL_MISMATCH"
)

// absentMark 는 TSV·리포트에서 "그 호스트에는 이 속성이 없음" 을 나타냅니다 (빈 문자열 값은 "" 로 보여 구분).
const absentMark = "<없음>"

// ---- diff.txt (기준 목록) ----

// refEntry 는 diff.txt 한 줄입니다: hostname(또는 ip[:포트], 이름-m) + 선택적 모델 칸(첫 공백 뒤 나머지 전부).
// 모델 칸은 확인용일 뿐이며 분류는 항상 BMC 가 보고한 실제 모델로 합니다.
type refEntry struct {
	Input string
	Model string
}

// parseRefList 는 diff.txt 텍스트를 해석합니다. 빈 줄과 # 주석은 무시하고 같은 hostname 은 처음 것만 남깁니다.
func parseRefList(text string) []refEntry {
	text = strings.TrimPrefix(text, "\ufeff")
	var out []refEntry
	seen := map[string]bool{}
	for _, raw := range strings.Split(text, "\n") {
		line := raw
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line) // CRLF 의 \r 도 함께 제거
		if line == "" {
			continue
		}
		e := refEntry{Input: line}
		if i := strings.IndexFunc(line, unicode.IsSpace); i >= 0 {
			e.Input, e.Model = line[:i], strings.TrimSpace(line[i:])
		}
		if seen[e.Input] {
			continue
		}
		seen[e.Input] = true
		out = append(out, e)
	}
	return out
}

func loadRefList(path string) ([]refEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("기준(정상) 호스트 목록 %s 이(가) 없습니다. diff.txt.example 을 복사해 정상 설정값을 가진 대표 hostname 을 한 줄씩 적으십시오", path)
		}
		return nil, fmt.Errorf("기준 목록 %s: %w", path, err)
	}
	es := parseRefList(string(data))
	if len(es) == 0 {
		return nil, fmt.Errorf("기준 목록 %s 이(가) 비어 있습니다 (정상 설정값을 가진 대표 hostname 을 한 줄씩 적으십시오)", path)
	}
	return es, nil
}

// targetID 는 같은 BMC 를 가리키는 입력(이름·이름-m·IP 표기 차이)을 한 호스트로 보기 위한 키입니다.
func targetID(t Target) string {
	if t.IP != "" {
		return strings.ToLower(t.IP) + "|" + t.Port
	}
	return "name|" + strings.ToLower(t.Hostname)
}

// ---- ignore_attrs.txt (비교 제외 속성) ----

// defaultIgnore 는 ignore_attrs.txt 가 없을 때 쓰는 내장 목록입니다. 호스트마다 값이 달라야 정상인 것만 담았습니다
// (너무 넓은 패턴은 진짜 설정 차이를 가리므로 피함: 예) *Mac* 은 MachineCheck 류까지 걸려 쓰지 않음).
// 첫 실장비 결과를 보고 ignore_attrs.txt 로 보정하십시오.
var defaultIgnore = []string{
	"ServiceTag", "SystemServiceTag", "AssetTag", "ServerAssetTag", "SerialNumber", "ServerName",
	"SysMfrContactInfo", "UefiBootSeq", "BootSeq*", "*Uuid*", "*UUID*", "*MacAddr*",
	"SystemTime", "SystemDate", "Time", "Date", "*DateTime*", "RtcTime", "RtcDate",
}

// ignoreSet 은 비교 제외 패턴입니다 (소문자, * 만 와일드카드).
type ignoreSet struct {
	Pats   []string
	Source string // 사람이 읽는 출처 (리포트용)
}

func newIgnoreSet(pats []string, source string) *ignoreSet {
	s := &ignoreSet{Source: source}
	seen := map[string]bool{}
	for _, p := range pats {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && !seen[p] {
			seen[p] = true
			s.Pats = append(s.Pats, p)
		}
	}
	return s
}

// parseIgnore 는 ignore_attrs.txt 텍스트를 해석합니다 (한 줄 한 패턴, # 주석, 빈 줄 무시).
func parseIgnore(text, source string) *ignoreSet {
	text = strings.TrimPrefix(text, "\ufeff")
	var pats []string
	for _, raw := range strings.Split(text, "\n") {
		line := raw
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			pats = append(pats, line)
		}
	}
	return newIgnoreSet(pats, source)
}

// loadIgnore 는 ignore_attrs.txt 를 읽습니다. 파일이 없으면 내장 기본 목록을 쓰되,
// 사용자가 -ignore 로 경로를 직접 줬는데(explicit) 없으면 오류입니다.
func loadIgnore(path string, explicit bool) (*ignoreSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return newIgnoreSet(defaultIgnore, "내장 기본 목록 ("+path+" 없음)"), nil
		}
		return nil, fmt.Errorf("제외 속성 목록 %s: %w", path, err)
	}
	return parseIgnore(string(data), path), nil
}

func (s *ignoreSet) match(name string) bool {
	l := strings.ToLower(name)
	for _, p := range s.Pats {
		if globMatch(p, l) {
			return true
		}
	}
	return false
}

// globMatch 는 * (0글자 이상) 만 지원하는 와일드카드 일치입니다. 속성 이름에 [ ] ? 가 흔해서 path.Match 는 쓰지 않습니다.
func globMatch(pat, s string) bool {
	px, sx, star, mark := 0, 0, -1, 0
	for sx < len(s) {
		switch {
		case px < len(pat) && pat[px] == '*':
			star, mark = px, sx
			px++
		case px < len(pat) && pat[px] == s[sx]:
			px++
			sx++
		case star >= 0:
			px = star + 1
			mark++
			sx = mark
		default:
			return false
		}
	}
	for px < len(pat) && pat[px] == '*' {
		px++
	}
	return px == len(pat)
}

// ---- BMC 1대의 BIOS 스냅샷 ----

// biosSnap 은 호스트 1대에서 읽은 모델·BIOS 버전·Attributes 입니다. Status 가 비어 있으면 읽기에 성공한 것입니다.
type biosSnap struct {
	Target       Target
	Status       string
	HTTP         int
	Detail       string // 오류 요약 (비밀번호·토큰 없음)
	Manufacturer string
	Vendor       string // normalizeVendor(Manufacturer)
	Model        string
	BiosVersion  string
	Key          string // 모델 키: 소문자 벤더|normalizeModel(Model)
	Attrs        map[string]string
	Stats        CallStats
}

func (s *biosSnap) ok() bool { return s.Status == "" }

func (s *biosSnap) setErr(err error) {
	s.Status, s.HTTP = errStatus(err)
	s.Detail = errBrief(err)
	var rf *RFError
	if errors.As(err, &rf) && rf.Detail != "" {
		s.Detail = rf.Detail
	}
}

func (s *biosSnap) label() string { return strings.TrimSpace(s.Vendor + " " + s.Model) }

func skippedSnap(t Target) *biosSnap {
	s := &biosSnap{Target: t, Status: StatusSkippedAuthStop}
	if t.Err != "" {
		s.Status = t.Err // 이름 해석 실패는 원래 상태를 유지한다
	}
	return s
}

// read 는 GET 4번(ServiceRoot, Systems, System, Bios)으로 스냅샷을 채웁니다. 첫 System 만 봅니다 (detectSystem 과 같다).
func (s *biosSnap) read(get getFunc) error {
	b, err := get(rootPath)
	if err != nil {
		return err
	}
	var root struct {
		Vendor  string
		Systems odataLink
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return badJSON("ServiceRoot")
	}
	members, err := collectionMembers(get, orDefault(root.Systems.ID, defaultSystemsPath))
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return &RFError{Status: StatusUnsupported, Detail: "Systems 컬렉션에 멤버가 없습니다"}
	}
	sb, err := get(members[0])
	if err != nil {
		return err
	}
	var sys struct {
		Manufacturer string
		Model        string
		BiosVersion  string
		Bios         odataLink
	}
	if err := json.Unmarshal(sb, &sys); err != nil {
		return badJSON("System")
	}
	s.Manufacturer = strings.TrimSpace(sys.Manufacturer)
	s.Vendor = normalizeVendor(s.Manufacturer)
	if s.Manufacturer == "" {
		s.Vendor = normalizeVendor(root.Vendor)
	}
	s.Model = strings.TrimSpace(sys.Model)
	s.BiosVersion = strings.TrimSpace(sys.BiosVersion)
	s.Key = strings.ToLower(s.Vendor) + "|" + normalizeModel(s.Model)
	if sys.Bios.ID == "" {
		return &RFError{Status: StatusUnsupported, Detail: "System 에 Bios 리소스가 없습니다"}
	}
	bb, err := get(sys.Bios.ID)
	if err != nil {
		return err
	}
	if s.Attrs, err = parseAttrs(bb); err != nil {
		return err
	}
	if len(s.Attrs) == 0 {
		return &RFError{Status: StatusUnsupported, Detail: "Bios Attributes 가 비어 있습니다"}
	}
	return nil
}

// ---- 기준 호스트 / 대상 호스트 ----

// allRef 는 diff.txt 의 기준 호스트 1대입니다.
type allRef struct {
	Entry  refEntry
	Target Target
	ID     string // targetID
	Order  int    // diff.txt 순서
	Snap   *biosSnap
	Notes  []hostNote // DIFF_TXT_MODEL_MISMATCH
}

// newAllRefs 는 diff.txt 항목을 접속 대상으로 해석합니다 (user.txt 와 같은 이름 해석 규칙).
// 같은 BMC 를 가리키는 항목이 둘이면 먼저 나온 것만 남깁니다.
func newAllRefs(entries []refEntry, hostsPath string) ([]*allRef, error) {
	inputs := make([]string, len(entries))
	for i, e := range entries {
		inputs[i] = e.Input
	}
	ts, err := resolveTargets(inputs, hostsPath)
	if err != nil {
		return nil, err
	}
	var refs []*allRef
	seen := map[string]bool{}
	for i, t := range ts {
		id := targetID(t)
		if seen[id] {
			continue
		}
		seen[id] = true
		refs = append(refs, &allRef{Entry: entries[i], Target: t, ID: id, Order: len(refs)})
	}
	return refs, nil
}

// hostNote 는 특이사항 1건입니다 (Code 는 noteXxx, Detail 은 사람이 읽는 상세).
type hostNote struct{ Code, Detail string }

func (n hostNote) String() string {
	if n.Detail == "" {
		return n.Code
	}
	return n.Code + "(" + n.Detail + ")"
}

// attrDiffRow 는 속성 1개의 차이입니다 (SAME 은 만들지 않음).
type attrDiffRow struct {
	Attr, Ref, Host string
	Status          string // attrDiff / attrOnlyRef / attrOnlyHost
}

// allHost 는 user.txt 대상 1대의 결과입니다.
type allHost struct {
	Target Target
	Snap   *biosSnap
	Status string // StatusSame / StatusDiff / StatusIsReference / StatusNoReference / StatusRefUnreachable / 오류 상태
	HTTP   int
	Detail string
	Ref    *allRef // 비교한 기준 (SAME/DIFF 일 때)
	Diffs  []attrDiffRow

	Compared, Ignored          int // Compared = 제외 속성을 뺀 속성 이름의 합집합 크기
	NDiff, NOnlyRef, NOnlyHost int
	Notes                      []hostNote
}

func (h *allHost) compared() bool { return h.Status == StatusSame || h.Status == StatusDiff }

func (h *allHost) hasNote(code string) bool {
	for _, n := range h.Notes {
		if n.Code == code {
			return true
		}
	}
	return false
}

// allRun 은 all_bios_check 1회의 전체 결과입니다.
type allRun struct {
	Now          time.Time
	Dir          string // results/<일시>_all
	FromDump     string
	AuthStop     int
	Ignore       *ignoreSet
	Refs         []*allRef
	Hosts        []*allHost  // user.txt 순서
	RetryFromDir string      // 단계 7: 이전 결과 폴더 (재시도 모드일 때만)
	prevSummary  *tsvTable   // 단계 7: 이전 summary.tsv (병합 바탕, 읽기만 한다)
	prevAllDiff  *tsvTable   // 단계 7: 이전 all_diff.tsv
	Retry        *retryMerge // 단계 7: 병합 결과 (병합했을 때만)
}

type allEnv struct {
	conf     *Config
	password string
	ign      *ignoreSet
	offline  bool
	dumps    map[string]string // 소문자 폴더 이름 → 호스트 덤프 폴더
	refByID  map[string]*allRef
	byKey    map[string][]*allRef // 모델 키 → 접속에 성공한 기준 (diff.txt 순서)
	failed   []*allRef            // 읽지 못한 기준
}

// ---- 읽기 ----

// fetch 는 호스트 1대를 읽습니다 (온라인: 세션 1회 로그인·로그아웃, 오프라인: 덤프 폴더).
func (e *allEnv) fetch(t Target) *biosSnap {
	s := &biosSnap{Target: t}
	if t.Err != "" {
		s.Status = t.Err
		return s
	}
	if e.offline {
		dir, ok := e.dumps[strings.ToLower(dirPart(t.Hostname))]
		if !ok {
			s.Status = StatusNoDump
			return s
		}
		get, err := newDumpFileGet(dir)
		if err != nil {
			s.Status, s.Detail = StatusHTTPError, "덤프 폴더를 읽을 수 없습니다: "+err.Error()
			return s
		}
		if err := s.read(get); err != nil {
			s.setErr(err)
		}
		return s
	}
	c := NewClient(ClientOpts{
		BaseURL: t.BaseURL(), User: e.conf.User, Pass: e.password, Insecure: e.conf.Insecure,
		Timeout: e.conf.Timeout, Retries: e.conf.Retries, Mode: ModeReadOnly, Gap: checkClientGap,
	})
	defer func() {
		_ = c.Close()
		s.Stats = c.Stats()
	}()
	if err := c.Login(); err != nil {
		s.setErr(err)
		return s
	}
	if err := s.read(c.GetRaw); err != nil {
		s.setErr(err)
	}
	return s
}

// sweepSnaps 는 sweepHosts 로 대상을 읽고 결과 스냅샷마다 handle(i, 스냅샷) 을 부릅니다 (여러 고루틴에서 불리며
// i 번째 칸에만 써야 함). 차단기가 멈춘 뒤 도착한 호스트는 접속 없이 SKIPPED_AUTH_STOP 스냅샷으로 handle 에 갑니다.
// AUTH_FAIL 호스트 수를 돌려줍니다.
func (e *allEnv) sweepSnaps(ts []Target, authStop int, handle func(i int, s *biosSnap)) int {
	var fails int32
	sweepHosts(len(ts), e.conf.Concurrency, authStop,
		func(i int) hostOutcome {
			s := e.fetch(ts[i])
			handle(i, s)
			o := outcomeOf(s.Status, s.Stats, ts[i].Err == "")
			if o == outAuthFail {
				atomic.AddInt32(&fails, 1)
			}
			return o
		},
		func(i int) { handle(i, skippedSnap(ts[i])) })
	return int(fails)
}

// newAllRunDir 는 <base>/<YYYYMMDD_HHMMSS>_all 폴더를 만듭니다 (같은 초에 또 실행하면 _all_2, _all_3 ...).
func newAllRunDir(base string, now time.Time) (string, error) {
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	name := now.Format("20060102_150405") + "_all"
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

type allOpts struct {
	Refs         []*allRef
	Ignore       *ignoreSet
	FromDump     string
	ResultDir    string
	RetryFromDir string    // 단계 7: -retry-from 값 (이전 결과 폴더 또는 그 안의 retry.txt). 비면 일반 비교
	Now          time.Time // 0 이면 지금
}

// doAllCheck 는 기준·대상 전체를 읽어 비교하고 결과 파일을 씁니다. 파일 쓰기에 실패해도 결과(run)는 돌려줍니다.
func doAllCheck(rc *runContext, o allOpts) (*allRun, error) {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	env := &allEnv{
		conf: rc.Conf, password: rc.Password, ign: o.Ignore, offline: o.FromDump != "",
		refByID: map[string]*allRef{}, byKey: map[string][]*allRef{},
	}
	var err error
	// 재시도 모드: 이전 결과를 먼저 읽어 검증한다 (접속·폴더 생성 전에 실패하면 아무것도 남기지 않는다).
	var prevDir string
	var prevSum, prevDiff *tsvTable
	if o.RetryFromDir != "" {
		prevDir = retryPrevDir(o.RetryFromDir)
		if prevSum, prevDiff, err = loadPrevAll(prevDir); err != nil {
			return nil, err
		}
	}
	if env.offline {
		if env.dumps, err = indexDumpHosts(o.FromDump); err != nil {
			return nil, err
		}
	}
	dir, err := newAllRunDir(o.ResultDir, now)
	if err != nil {
		return nil, fmt.Errorf("결과 폴더: %w", err)
	}
	authStop := rc.Conf.AuthFailStop
	if env.offline {
		authStop = 0 // 덤프 읽기는 BMC 에 로그인하지 않는다
	}
	run := &allRun{Now: now, Dir: dir, FromDump: o.FromDump, AuthStop: authStop, Ignore: o.Ignore, Refs: o.Refs,
		RetryFromDir: prevDir, prevSummary: prevSum, prevAllDiff: prevDiff}

	// ① 기준 호스트
	refTs := make([]Target, len(o.Refs))
	for i, r := range o.Refs {
		refTs[i] = r.Target
		env.refByID[r.ID] = r
	}
	fails1 := env.sweepSnaps(refTs, authStop, func(i int, s *biosSnap) { o.Refs[i].Snap = s })
	for _, r := range o.Refs {
		if !r.Snap.ok() {
			env.failed = append(env.failed, r)
			continue
		}
		env.byKey[r.Snap.Key] = append(env.byKey[r.Snap.Key], r)
		if r.Entry.Model != "" && normalizeModel(r.Entry.Model) != normalizeModel(r.Snap.Model) {
			r.Notes = append(r.Notes, hostNote{noteDiffTxtModelMismatch,
				fmt.Sprintf("diff.txt 의 모델 %q ≠ BMC 가 보고한 %q (실제 모델로 분류)", r.Entry.Model, r.Snap.Model)})
		}
	}

	// ② 대상 호스트: 기준과 같은 호스트는 다시 읽지 않고, 나머지는 읽는 즉시 비교한다.
	hosts := make([]*allHost, len(rc.Targets))
	var todo []int
	for i, t := range rc.Targets {
		if r := env.refByID[targetID(t)]; r != nil {
			hosts[i] = env.finish(&allHost{Target: t, Snap: r.Snap})
			continue
		}
		todo = append(todo, i)
	}
	if len(todo) > 0 {
		ts := make([]Target, len(todo))
		for j, i := range todo {
			ts[j] = rc.Targets[i]
		}
		handle := func(j int, s *biosSnap) {
			hosts[todo[j]] = env.finish(&allHost{Target: ts[j], Snap: s})
			s.Attrs = nil // 비교가 끝났으니 속성 값은 버린다 (기준의 것만 남김)
		}
		stop2 := authStop
		if authStop > 0 {
			stop2 = authStop - fails1 // 기준 단계에서 이미 쓴 AUTH_FAIL 만큼 예산을 줄인다
		}
		if authStop > 0 && stop2 <= 0 {
			for j := range ts {
				handle(j, skippedSnap(ts[j]))
			}
		} else {
			env.sweepSnaps(ts, stop2, handle)
		}
	}
	run.Hosts = hosts
	return run, run.writeFiles()
}

// finish 는 읽은 스냅샷으로 호스트의 상태를 정합니다 (읽기 실패면 그 상태, 성공이면 비교).
func (e *allEnv) finish(h *allHost) *allHost {
	if !h.Snap.ok() {
		h.Status, h.HTTP, h.Detail = h.Snap.Status, h.Snap.HTTP, h.Snap.Detail
		return h
	}
	e.classify(h)
	return h
}

// classify 는 모델 키로 기준을 고르고 비교합니다. e 는 읽기 전용으로만 쓰므로 여러 고루틴에서 불러도 됩니다.
func (e *allEnv) classify(h *allHost) {
	s := h.Snap
	self := e.refByID[targetID(h.Target)]
	var cands []*allRef
	for _, r := range e.byKey[s.Key] {
		if r != self { // 자기 자신과는 비교하지 않는다
			cands = append(cands, r)
		}
	}
	if len(cands) == 0 {
		switch {
		case self != nil:
			h.Status, h.Detail = StatusIsReference, "기준 호스트 자신 (같은 모델의 다른 기준이 없어 비교하지 않음)"
		default:
			if names := e.failedFor(s); len(names) > 0 {
				h.Status = StatusRefUnreachable
				h.Detail = "기준 읽기 실패: " + strings.Join(names, ", ") + " (모델 미상이면 이 모델의 기준일 수 있음)"
			} else {
				h.Status, h.Detail = StatusNoReference, "모델 키 "+s.Key+" 의 기준이 diff.txt 에 없음"
			}
		}
		return
	}

	pick, matched := cands[0], false
	for _, r := range cands {
		if r.Snap.BiosVersion == s.BiosVersion {
			pick, matched = r, true
			break
		}
	}
	if len(cands) > 1 {
		why := "BIOS 버전 일치"
		if !matched {
			why = "BIOS 버전이 같은 기준이 없어 diff.txt 순서의 첫 기준"
		}
		h.Notes = append(h.Notes, hostNote{noteMultiRef, fmt.Sprintf("기준 %d개 중 %s 선택 (%s)", len(cands), pick.Target.Hostname, why)})
	}
	h.Ref = pick
	if s.BiosVersion != pick.Snap.BiosVersion {
		h.Notes = append(h.Notes, hostNote{noteBiosVerDiff, fmt.Sprintf("대상=%s 기준=%s", dash(s.BiosVersion), dash(pick.Snap.BiosVersion))})
	}
	compareAttrs(h, pick.Snap.Attrs, s.Attrs, e.ign)
	h.Status = StatusSame
	if len(h.Diffs) > 0 {
		h.Status = StatusDiff
	}
}

// failedFor 는 대상의 기준이었을 수 있는, 읽지 못한 기준 호스트의 "이름(상태)" 목록입니다.
// 읽지 못한 기준은 모델을 알 수 없으므로, diff.txt 에 모델 칸이 없으면 모든 모델의 후보로 보고
// 모델 칸이 있으면 그 모델과 같을 때만 후보로 봅니다.
func (e *allEnv) failedFor(s *biosSnap) []string {
	var out []string
	for _, r := range e.failed {
		if r.Entry.Model == "" || normalizeModel(r.Entry.Model) == normalizeModel(s.Model) {
			out = append(out, r.Target.Hostname+"("+r.Snap.Status+")")
		}
	}
	return out
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// attrRank 는 차이 표시 순서입니다 (값이 다른 것 먼저).
var attrRank = map[string]int{attrDiff: 0, attrOnlyRef: 1, attrOnlyHost: 2}

// compareAttrs 는 기준과 대상의 속성 전체를 비교해 h 의 Diffs 와 개수를 채웁니다.
// Compared = (기준∪대상) 속성 이름 중 제외 패턴에 걸리지 않은 것의 수, Ignored = 걸린 것의 수입니다.
func compareAttrs(h *allHost, ref, host map[string]string, ign *ignoreSet) {
	names := make([]string, 0, len(ref)+len(host))
	for n := range ref {
		names = append(names, n)
	}
	for n := range host {
		if _, ok := ref[n]; !ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if ign.match(n) {
			h.Ignored++
			continue
		}
		h.Compared++
		rv, rok := ref[n]
		hv, hok := host[n]
		switch {
		case rok && hok:
			if rv != hv {
				h.Diffs = append(h.Diffs, attrDiffRow{n, rv, hv, attrDiff})
				h.NDiff++
			}
		case rok:
			h.Diffs = append(h.Diffs, attrDiffRow{n, rv, "", attrOnlyRef})
			h.NOnlyRef++
		default:
			h.Diffs = append(h.Diffs, attrDiffRow{n, "", hv, attrOnlyHost})
			h.NOnlyHost++
		}
	}
	sort.SliceStable(h.Diffs, func(i, j int) bool { return attrRank[h.Diffs[i].Status] < attrRank[h.Diffs[j].Status] })
}

// cell 은 TSV·리포트에 쓸 값 표기입니다: 없음 → <없음>, 빈 문자열 → "".
func cell(v string, present bool) string {
	switch {
	case !present:
		return absentMark
	case v == "":
		return `""`
	}
	return v
}

func (d attrDiffRow) refCell() string  { return cell(d.Ref, d.Status != attrOnlyHost) }
func (d attrDiffRow) hostCell() string { return cell(d.Host, d.Status != attrOnlyRef) }

// ---- 결과 파일 ----

var (
	allDiffHeader = []string{"model", "ref_host", "host", "bios_version", "ref_bios_version", "attribute", "ref_value", "host_value", "status"}
	summaryHeader = []string{"host", "ip", "model", "ref_host", "bios_version", "ref_bios_version", "compared", "ignored", "diff", "only_ref", "only_host", "status", "notes"}
)

// writeTSVRows 는 머리글과 행을 스트리밍으로 씁니다 (all_diff.tsv 는 수십만 행일 수 있어 한꺼번에 모으지 않음). 0600.
func writeTSVRows(path string, header []string, each func(emit func(cols ...string))) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, strings.Join(header, "\t"))
	each(func(cols ...string) {
		for i, c := range cols {
			cols[i] = tsvField(c)
		}
		fmt.Fprintln(w, strings.Join(cols, "\t"))
	})
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (h *allHost) notesText() string {
	var parts []string
	for _, n := range h.Notes {
		parts = append(parts, n.String())
	}
	if !h.compared() && h.Detail != "" {
		parts = append(parts, h.Detail)
	}
	return strings.Join(parts, "; ")
}

func (r *allRun) writeFiles() error {
	err := writeTSVRows(filepath.Join(r.Dir, "all_diff.tsv"), allDiffHeader, func(emit func(...string)) {
		for _, h := range r.Hosts {
			for _, d := range h.Diffs {
				emit(h.Snap.label(), h.Ref.Target.Hostname, h.Target.Hostname, h.Snap.BiosVersion, h.Ref.Snap.BiosVersion,
					d.Attr, d.refCell(), d.hostCell(), d.Status)
			}
		}
	})
	if err != nil {
		return err
	}
	err = writeTSVRows(filepath.Join(r.Dir, "summary.tsv"), summaryHeader, func(emit func(...string)) {
		for _, h := range r.Hosts {
			model, refHost, ver, refVer := "-", "-", "-", "-"
			if h.Snap != nil && h.Snap.ok() {
				model, ver = h.Snap.label(), h.Snap.BiosVersion
			}
			n := [5]string{"-", "-", "-", "-", "-"}
			if h.compared() {
				refHost, refVer = h.Ref.Target.Hostname, h.Ref.Snap.BiosVersion
				n = [5]string{itoa(h.Compared), itoa(h.Ignored), itoa(h.NDiff), itoa(h.NOnlyRef), itoa(h.NOnlyHost)}
			}
			emit(h.Target.Hostname, h.Target.IP, model, refHost, ver, refVer, n[0], n[1], n[2], n[3], n[4], h.Status, h.notesText())
		}
	})
	if err != nil {
		return err
	}
	var retry []string
	for _, h := range r.Hosts {
		if isRetryStatus(h.Status) || h.Status == StatusRefUnreachable {
			retry = append(retry, h.Target.Input)
		}
	}
	if err := writeLines(filepath.Join(r.Dir, "retry.txt"), retry); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.Dir, "run_info.txt"), []byte(r.runInfo()), 0o600); err != nil {
		return err
	}
	if r.prevSummary != nil {
		return r.writeMergedResults()
	}
	return nil
}

func itoa(n int) string { return fmt.Sprint(n) }

// tally 는 호스트 상태별 수입니다.
func (r *allRun) tally() map[string]int {
	m := map[string]int{}
	for _, h := range r.Hosts {
		m[h.Status]++
	}
	return m
}

// authFails 는 AUTH_FAIL 호스트 수입니다 (기준 호스트 포함, 기준과 같은 대상은 한 번만).
func (r *allRun) authFails() int {
	seen := map[*biosSnap]bool{}
	n := 0
	add := func(s *biosSnap) {
		if s != nil && !seen[s] {
			seen[s] = true
			if s.Status == StatusAuthFail {
				n++
			}
		}
	}
	for _, ref := range r.Refs {
		add(ref.Snap)
	}
	for _, h := range r.Hosts {
		add(h.Snap)
	}
	return n
}

// callTotals 는 BMC 호출 합계입니다 (기준과 같은 대상은 한 번만 읽었으므로 한 번만 센다).
func (r *allRun) callTotals() (sessions, gets int) {
	seen := map[*biosSnap]bool{}
	add := func(s *biosSnap) {
		if s != nil && !seen[s] {
			seen[s] = true
			sessions += s.Stats.Sessions
			gets += s.Stats.Gets
		}
	}
	for _, ref := range r.Refs {
		add(ref.Snap)
	}
	for _, h := range r.Hosts {
		add(h.Snap)
	}
	return sessions, gets
}

// runInfo 는 run_info.txt 본문입니다 (비밀번호·토큰·계정 없음).
func (r *allRun) runInfo() string {
	t := r.tally()
	var b strings.Builder
	fmt.Fprintf(&b, "도구: biostool %s (allcheck, 읽기 전용)\n", toolVersion)
	fmt.Fprintf(&b, "시각: %s\n", r.Now.Format("2006-01-02 15:04:05"))
	if r.FromDump != "" {
		fmt.Fprintf(&b, "모드: 덤프 읽기 (%s), 접속 안 함\n", r.FromDump)
	} else {
		b.WriteString("모드: 실제 접속\n")
	}
	fmt.Fprintf(&b, "기준 호스트 수: %d, 대상 수: %d\n", len(r.Refs), len(r.Hosts))
	fmt.Fprintf(&b, "제외 속성: %s (패턴 %d개)\n", r.Ignore.Source, len(r.Ignore.Pats))
	b.WriteString("비교 정의: compared = (기준∪대상) 속성 이름 중 제외 패턴에 걸리지 않은 수, 값은 문자열로 비교\n")
	if r.AuthStop > 0 {
		fmt.Fprintf(&b, "AUTH_FAIL 차단기: auth_fail_stop=%d\n", r.AuthStop)
	}
	if w := authStopWarning(r.authFails(), t[StatusSkippedAuthStop], authStopRerunCheck); w != "" {
		b.WriteString("경고: " + w + "\n")
	}
	b.WriteString("기준 호스트:\n")
	for _, ref := range r.Refs {
		if ref.Snap.ok() {
			fmt.Fprintf(&b, "  %s\t%s\tBIOS %s\n", ref.Target.Hostname, ref.Snap.label(), dash(ref.Snap.BiosVersion))
		} else {
			fmt.Fprintf(&b, "  %s\t%s\n", ref.Target.Hostname, ref.Snap.Status)
		}
		for _, n := range ref.Notes {
			fmt.Fprintf(&b, "    %s\n", n)
		}
	}
	b.WriteString("호스트 기준 집계 (상태):\n")
	for _, k := range sortedKeys(t) {
		fmt.Fprintf(&b, "  %s: %d\n", k, t[k])
	}
	sess, gets := r.callTotals()
	fmt.Fprintf(&b, "BMC 호출: 세션 생성 %d, GET %d\n", sess, gets)
	return b.String()
}

// ---- 명령 ----

func cmdAllCheck(args []string) error {
	fs, o := newFlagSet("allcheck")
	diff := fs.String("diff", "diff.txt", "기준(정상 설정값) 호스트 목록: 한 줄에 hostname")
	ignore := fs.String("ignore", "ignore_attrs.txt", "비교에서 제외할 속성 목록 (없으면 내장 기본 목록)")
	listMax := fs.Int("list-max", 20, "차이 상세·특이사항·오류에 나열할 최대 호스트 수")
	diffMax := fs.Int("diff-max", 30, "호스트당 터미널에 보일 차이 속성 최대 수")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if o.profile != "" || o.dryRun {
		return fmt.Errorf("allcheck 는 읽기 전용 전체 비교라 -profile, -dry-run 을 쓰지 않습니다")
	}
	if *listMax < 0 || *diffMax < 0 {
		return fmt.Errorf("-list-max, -diff-max 는 0 이상이어야 합니다")
	}

	if proceed, err := retryPreflight(fs, o, os.Stdout); err != nil || !proceed {
		return err
	}

	explicitIgnore := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "ignore" {
			explicitIgnore = true
		}
	})
	entries, err := loadRefList(*diff)
	if err != nil {
		return err
	}
	ign, err := loadIgnore(*ignore, explicitIgnore)
	if err != nil {
		return err
	}
	rc, err := prepare(o, "", o.retryFrom)
	if err != nil {
		return err
	}
	refs, err := newAllRefs(entries, o.hosts)
	if err != nil {
		return err
	}
	printSummary(rc, o, "")
	fmt.Printf("기준(정상) 호스트: %s 에서 %d개, 제외 속성: %s\n", *diff, len(refs), ign.Source)

	run, err := doAllCheck(rc, allOpts{Refs: refs, Ignore: ign, FromDump: o.fromDump, ResultDir: rc.Conf.ResultDir, RetryFromDir: o.retryFrom})
	if run == nil {
		return err
	}
	fmt.Println()
	writeAllReport(os.Stdout, run, allReportOpts{ListMax: *listMax, DiffMax: *diffMax, Color: useColor()})
	if err != nil {
		fmt.Fprintln(os.Stderr, "경고: 결과 파일 기록 중 오류: "+err.Error())
	}
	return err
}
