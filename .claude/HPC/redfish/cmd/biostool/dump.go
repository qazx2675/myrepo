package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// dump.go 는 사전조사 서브커맨드 `biostool dump` 입니다 (xml.sh 가 실행).
//
// Redfish 트리를 GET 전용(ModeReadOnly)으로 좁게 순회해 JSON 으로 저장하고, 사람이 눈으로 읽어
// 전달할 수 있는 짧은 요약을 터미널에 출력합니다 (폐쇄망이라 파일을 반출할 수 없음).
// 순회 범위: ServiceRoot, Systems 와 각 System, 각 System 의 Bios·Settings(또는 Pending),
// Registries 컬렉션과 Bios 가 가리키는 레지스트리 파일(+Location Uri 의 JSON),
// Managers 와 각 Manager (+ Jobs/Tasks 컬렉션 목록만). SessionService·Chassis 는 보지 않고,
// LogServices 등 허용목록 밖 경로는 따라가지 않으며 skipped 로 셉니다.

const toolVersion = "0.1.0"

// maxDumpResources 는 호스트 1대에서 요청할 수 있는 서로 다른 GET 경로의 상한입니다 (과다 호출 방지).
// 초과하면 순회를 멈추고 경고(PARTIAL)로 표시합니다. 테스트가 낮춰 쓰므로 var 입니다.
var maxDumpResources = 120

// dumpClientGap 은 같은 BMC 에 대한 연속 요청 간격입니다 (0 이면 Client 기본 100ms).
// 테스트가 -1 로 바꿔 쓰며 운영에서는 바꾸지 않습니다.
var dumpClientGap time.Duration

var errDumpLimit = errors.New("호스트당 리소스 상한에 도달했습니다")

// 터미널 요약에서 표준 항목 하나에 보여 주는 후보 수 상한.
const maxShownCands = 6

type dumpOpts struct {
	Out     string // 덤프 저장 루트
	Compact bool   // 호스트당 1줄 요약
}

// dumpResult 는 호스트 1대의 덤프 결과입니다.
type dumpResult struct {
	Target    Target
	Status    string // OK / PARTIAL / SAVE_FAIL / 실패 상태(AUTH_FAIL, UNREACHABLE, ...)
	HTTP      int
	Info      *SystemInfo
	Stats     CallStats
	Skipped   int
	Resources int      // 저장한 JSON 파일 수
	Notes     []string // 일부 실패·상한 경고 (식별정보 없음)
	RelDir    string   // 저장 루트 기준 상대 경로 (저장하지 않았으면 빈 문자열)
	Sum       *hostSummary
}

func (r *dumpResult) failed() bool {
	return r.Status != "OK" && r.Status != "PARTIAL" && r.Status != "SAVE_FAIL"
}

// hostSummary 는 터미널 요약에 쓰는 값입니다.
type hostSummary struct {
	AttrCount     int
	BiosOK        bool
	RegistryOK    bool
	SettingsKnown bool
	SettingsDiff  int
	JobsKnown     bool
	Jobs          int
	Std           []stdMatch
}

// ---- 1회 덤프 세션 (호스트당 1개) ----

type dumpRes struct {
	path string // 정규화된 요청 경로 (대소문자 유지)
	body []byte
	err  error
}

// dumpSession 은 호스트 1대의 GET 을 대신 보내며 응답을 보관합니다.
// 같은 경로(대소문자·끝 슬래시 무시)는 한 번만 요청하고, 서로 다른 경로 수가 상한을 넘으면 거부합니다.
// 허용목록에 걸리는 경로는 요청하지 않고 skipped 로 셉니다. 한 고루틴에서만 씁니다.
type dumpSession struct {
	c       *Client
	max     int
	res     map[string]*dumpRes
	order   []string // 성공한 응답의 키 (요청 순서)
	skipped map[string]bool
	notes   []string
	limited bool
	calls   int
}

func newDumpSession(c *Client) *dumpSession {
	return &dumpSession{c: c, max: maxDumpResources, res: map[string]*dumpRes{}, skipped: map[string]bool{}}
}

func pathKey(p string) (clean, key string) {
	clean, _, err := normalizePath(p)
	if err != nil {
		return "", strings.ToLower(p)
	}
	return clean, strings.ToLower(clean)
}

// get 은 getFunc 입니다 (detectSystemWith 에 넘김).
func (d *dumpSession) get(p string) ([]byte, error) {
	clean, key := pathKey(p)
	if r, ok := d.res[key]; ok {
		return r.body, r.err
	}
	r := &dumpRes{path: clean}
	d.res[key] = r
	if _, err := checkAllowed("GET", p, ModeReadOnly, ""); err != nil {
		d.skipped[key] = true
		r.err = err
		return nil, err
	}
	if d.calls >= d.max {
		d.limited = true
		r.err = errDumpLimit
		return nil, r.err
	}
	d.calls++
	b, err := d.c.GetRaw(p)
	if err == nil && !json.Valid(b) {
		err = badJSON("BMC")
	}
	r.err = err
	if err != nil {
		return nil, err
	}
	r.body = b
	d.order = append(d.order, key)
	return b, nil
}

// fetch 는 get 에 실패 기록(notes)을 더한 것입니다. 허용목록 거부는 실패가 아니라 skipped 입니다.
func (d *dumpSession) fetch(p, what string) ([]byte, error) {
	b, err := d.get(p)
	if err != nil && !errors.Is(err, ErrNotAllowed) {
		d.note("%s: %s", what, errBrief(err))
	}
	return b, err
}

func (d *dumpSession) note(format string, args ...interface{}) {
	s := fmt.Sprintf(format, args...)
	for _, n := range d.notes {
		if n == s {
			return
		}
	}
	d.notes = append(d.notes, s)
}

// cached 는 이미 받은 응답을 돌려줍니다 (없거나 실패했으면 nil).
func (d *dumpSession) cached(p string) []byte {
	if p == "" {
		return nil
	}
	_, key := pathKey(p)
	if r, ok := d.res[key]; ok && r.err == nil {
		return r.body
	}
	return nil
}

// skipLink 는 문서 안의 링크가 허용목록에 걸리면(LogServices 등) 따라가지 않고 skipped 로 셉니다.
func (d *dumpSession) skipLink(l string) {
	if _, err := checkAllowed("GET", l, ModeReadOnly, ""); err != nil {
		_, key := pathKey(l)
		d.skipped[key] = true
	}
}

// scanLinks 는 문서 안의 모든 @odata.id 값을 모읍니다 (Actions 아래는 제외, 순서는 키 이름순).
func scanLinks(b []byte) []string {
	var v interface{}
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	var out []string
	var walk func(x interface{})
	walk = func(x interface{}) {
		switch t := x.(type) {
		case map[string]interface{}:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if k == "Actions" {
					continue
				}
				if s, ok := t[k].(string); ok {
					if k == "@odata.id" {
						out = append(out, s)
					}
					continue
				}
				walk(t[k])
			}
		case []interface{}:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

func lastSeg(p string) string {
	return strings.ToLower(path.Base(strings.TrimRight(p, "/")))
}

// membersCount 는 컬렉션의 멤버 수입니다 (Members 배열, 비어 있으면 Members@odata.count).
func membersCount(b []byte) int {
	var col struct {
		Members []json.RawMessage
		Count   int `json:"Members@odata.count"`
	}
	if json.Unmarshal(b, &col) != nil {
		return 0
	}
	if len(col.Members) == 0 && col.Count > 0 {
		return col.Count
	}
	return len(col.Members)
}

// ---- 순회 ----

// walkSystemFiles 는 System 1개의 Settings 와 레지스트리 JSON 을 받습니다 (Bios 는 detect 가 이미 받음).
func (d *dumpSession) walkSystemFiles(info *SystemInfo) {
	if info.SettingsPath != "" {
		d.fetch(info.SettingsPath, "Settings")
	}
	if info.RegistryPath != "" {
		d.fetch(info.RegistryPath, "레지스트리")
	}
}

// walkManagers 는 Managers 컬렉션과 각 Manager, 그리고 거기서 찾은 Jobs/Tasks 컬렉션(목록만)을 받습니다.
// Jobs 멤버 수 합계를 돌려줍니다 (Jobs 컬렉션을 하나도 못 찾으면 known=false).
func (d *dumpSession) walkManagers(info *SystemInfo) (jobs int, known bool) {
	var mpaths []string
	if b, err := d.fetch(info.ManagersPath, "Managers 컬렉션"); err == nil {
		mpaths, _ = membersOf(b)
	}
	if info.ManagerPath != "" {
		have := false
		for _, m := range mpaths {
			if strings.EqualFold(strings.TrimRight(m, "/"), strings.TrimRight(info.ManagerPath, "/")) {
				have = true
			}
		}
		if !have {
			mpaths = append(mpaths, info.ManagerPath)
		}
	}
	seen := map[string]bool{}
	count := func(b []byte, p string) {
		if _, key := pathKey(p); !seen[key] {
			seen[key] = true
			jobs += membersCount(b)
			known = true
		}
	}
	for _, mp := range mpaths {
		mb, err := d.fetch(mp, "Manager")
		if err != nil {
			continue
		}
		found := false
		for _, l := range scanLinks(mb) {
			d.skipLink(l)
			switch lastSeg(l) {
			case "jobs":
				found = true
				if b, err := d.fetch(l, "Jobs 컬렉션"); err == nil {
					count(b, l)
				}
			case "tasks":
				found = true
				d.fetch(l, "Tasks 컬렉션")
			}
		}
		if !found && info.Vendor == "Dell" {
			// Dell 은 Manager 문서에 링크가 없어도 <Manager>/Jobs 가 있는 펌웨어가 있다 (없으면 조용히 넘어감).
			jp := strings.TrimRight(mp, "/") + "/Jobs"
			if b, err := d.get(jp); err == nil {
				count(b, jp)
			}
		}
	}
	return jobs, known
}

// ---- 호스트 1대 ----

func dumpHost(rc *runContext, t Target, opts dumpOpts) *dumpResult {
	if t.Err != "" {
		return &dumpResult{Target: t, Status: t.Err}
	}
	c := NewClient(ClientOpts{
		BaseURL: t.BaseURL(), User: rc.Conf.User, Pass: rc.Password, Insecure: rc.Conf.Insecure,
		Timeout: rc.Conf.Timeout, Retries: rc.Conf.Retries, Mode: ModeReadOnly, Gap: dumpClientGap,
	})
	return dumpWith(c, t, opts)
}

// dumpWith 는 이미 만든 읽기 전용 Client 로 호스트 1대를 덤프하고 c 를 닫습니다.
// Login 은 한 번만 일어나므로 이미 로그인한 Client 를 넘겨도 세션이 늘지 않습니다
// (check 가 MAPPING_MISSING 호스트를 같은 세션으로 덤프할 때 씁니다).
func dumpWith(c *Client, t Target, opts dumpOpts) *dumpResult {
	r := &dumpResult{Target: t}
	ds := newDumpSession(c)
	finish := func() {
		if err := c.Close(); err != nil {
			ds.note("세션 삭제: %s", errBrief(err))
		}
		r.Stats = c.Stats()
		r.Skipped = len(ds.skipped)
	}
	fail := func(err error) *dumpResult {
		finish()
		r.Status, r.HTTP = errStatus(err)
		return r
	}

	if err := c.Login(); err != nil {
		return fail(err)
	}
	info, derr := detectSystemWith(ds.get)
	r.Info = info
	if info == nil || info.SystemPath == "" || ds.cached(info.SystemPath) == nil {
		return fail(derr)
	}
	if derr != nil {
		ds.note("시스템 판별: %s", errBrief(derr))
	}

	ds.walkSystemFiles(info)
	for _, sp := range info.Systems {
		if strings.EqualFold(sp, info.SystemPath) {
			continue
		}
		other := &SystemInfo{
			Vendor: info.Vendor, SystemsPath: info.SystemsPath,
			ManagersPath: info.ManagersPath, RegistriesPath: info.RegistriesPath,
		}
		if err := other.fillSystem(ds.get, sp); err != nil {
			ds.note("추가 System: %s", errBrief(err))
		}
		ds.walkSystemFiles(other)
		for _, n := range other.Notes {
			ds.note("추가 System: %s", n)
		}
	}
	ds.fetch(info.RegistriesPath, "Registries 컬렉션")
	jobs, jobsKnown := ds.walkManagers(info)
	for _, sp := range info.Systems {
		if b := ds.cached(sp); b != nil {
			for _, l := range scanLinks(b) {
				ds.skipLink(l)
			}
		}
	}
	for _, n := range info.Notes {
		ds.note("%s", n)
	}
	if ds.limited {
		ds.note("리소스 상한(%d) 도달로 순회를 중단했습니다", ds.max)
	}
	finish()

	r.Sum = ds.buildSummary(info, jobs, jobsKnown)
	r.Notes = ds.notes
	r.Status = "OK"
	if len(ds.notes) > 0 {
		r.Status = "PARTIAL"
	}
	if err := ds.save(opts.Out, r); err != nil {
		r.Status = "SAVE_FAIL"
		r.Notes = append(r.Notes, "저장 실패: "+err.Error())
	}
	return r
}

// buildSummary 는 받아 둔 Bios·Settings·레지스트리에서 터미널 요약 값을 만듭니다.
func (d *dumpSession) buildSummary(info *SystemInfo, jobs int, jobsKnown bool) *hostSummary {
	s := &hostSummary{Jobs: jobs, JobsKnown: jobsKnown}
	var reg *attrRegistry
	if b := d.cached(info.RegistryPath); b != nil {
		if rg, err := parseRegistry(b); err == nil {
			reg, s.RegistryOK = rg, true
		}
	}
	bb := d.cached(info.BiosPath)
	if bb == nil {
		return s
	}
	bios, err := parseAttrs(bb)
	if err != nil {
		return s
	}
	s.BiosOK = true
	s.AttrCount = len(bios)
	s.Std = findStdCandidates(bios, reg)
	if b := d.cached(info.SettingsPath); b != nil {
		if pend, err := parseAttrs(b); err == nil {
			s.SettingsKnown = true
			for k, v := range pend {
				if cur, ok := bios[k]; !ok || cur != v {
					s.SettingsDiff++
				}
			}
		}
	}
	return s
}

// ---- 저장 ----

// dumpMeta 는 호스트 폴더 루트의 dump_meta.json 입니다. 비밀번호·토큰은 담지 않습니다.
type dumpMeta struct {
	Time        string `json:"time"`
	ToolVersion string `json:"tool_version"`
	Host        string `json:"host"`
	Vendor      string `json:"vendor"`
	Model       string `json:"model"`
	BiosVersion string `json:"bios_version"`
	Auth        string `json:"auth"` // session | basic
	Status      string `json:"status"`
	Calls       struct {
		Get      int `json:"get"`
		Post     int `json:"post"`
		Patch    int `json:"patch"`
		Delete   int `json:"delete"`
		Sessions int `json:"sessions"`
	} `json:"calls"`
	Resources int      `json:"resources"` // 저장한 JSON 파일 수
	Skipped   int      `json:"skipped"`   // 허용목록 밖이라 따라가지 않은 링크 수 (LogServices 등)
	Failed    int      `json:"failed"`    // 일부 실패 건수
	Limit     int      `json:"limit"`
	Notes     []string `json:"notes,omitempty"`
}

// safeSeg 는 경로 구성 요소 1개를 파일 시스템 안전하게 만듭니다 (영숫자 . _ - + ~ @ = , 만 유지).
func safeSeg(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("._-+~@=,", r):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if strings.Trim(out, ".") == "" { // "", ".", ".." 등
		return "_"
	}
	return out
}

// dirPart 는 vendor/model/biosver/host 폴더 이름용입니다: safeSeg 에 더해 ".." 를 없애고
// 연속·양끝 "_" 를 정리하며 비면 unknown 입니다 (예: "PowerEdge R660" → "PowerEdge_R660").
func dirPart(s string) string {
	out := safeSeg(strings.TrimSpace(s))
	for strings.Contains(out, "..") {
		out = strings.ReplaceAll(out, "..", "_")
	}
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	out = strings.Trim(out, "_.")
	if out == "" {
		return "unknown"
	}
	return out
}

// resourceFile 은 요청 경로의 저장 파일 위치입니다 (mock 트리와 같은 규칙: 경로 + ".json",
// 루트는 redfish/v1.json). 예: /redfish/v1/Systems/1/Bios → redfish/v1/Systems/1/Bios.json
func resourceFile(clean string) string {
	segs := strings.Split(strings.Trim(clean, "/"), "/")
	for i := range segs {
		segs[i] = safeSeg(segs[i])
	}
	return strings.Join(segs, "/") + ".json"
}

// hostRelDir 는 저장 루트 기준 호스트 폴더입니다: <vendor>/<model>/<biosver>/<host>
func hostRelDir(info *SystemInfo, host string) string {
	return strings.Join([]string{dirPart(info.Vendor), dirPart(info.Model), dirPart(info.BiosVersion), dirPart(host)}, "/")
}

// save 는 받은 JSON 과 dump_meta.json 을 저장합니다. 같은 호스트 폴더는 지우고 새로 씁니다 (덮어쓰기).
// 파일 0600, 디렉터리 0700.
func (d *dumpSession) save(outDir string, r *dumpResult) error {
	rel := hostRelDir(r.Info, r.Target.Hostname)
	hostDir := filepath.Join(outDir, filepath.FromSlash(rel))
	if err := os.RemoveAll(hostDir); err != nil {
		return err
	}
	if err := os.MkdirAll(hostDir, 0o700); err != nil {
		return err
	}
	for _, key := range d.order {
		rs := d.res[key]
		fp := filepath.Join(hostDir, filepath.FromSlash(resourceFile(rs.path)))
		if err := os.MkdirAll(filepath.Dir(fp), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(fp, rs.body, 0o600); err != nil {
			return err
		}
	}
	r.Resources = len(d.order)
	r.RelDir = rel

	m := dumpMeta{
		Time: time.Now().Format(time.RFC3339), ToolVersion: toolVersion, Host: r.Target.Hostname,
		Vendor: r.Info.Vendor, Model: r.Info.Model, BiosVersion: r.Info.BiosVersion,
		Auth: r.Stats.AuthMode, Status: r.Status, Resources: r.Resources, Skipped: r.Skipped,
		Failed: len(r.Notes), Limit: d.max, Notes: r.Notes,
	}
	m.Calls.Get, m.Calls.Post, m.Calls.Patch, m.Calls.Delete = r.Stats.Gets, r.Stats.Posts, r.Stats.Patches, r.Stats.Deletes
	m.Calls.Sessions = r.Stats.Sessions
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(hostDir, "dump_meta.json"), append(data, '\n'), 0o600)
}

// ---- index.tsv ----

func tsvField(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	return s
}

// appendIndex 는 <out>/index.tsv 에 호스트당 한 줄을 덧붙입니다 (새 파일이면 머리글 포함, 0600).
// 열: host ip vendor model biosver 경로 상태. 실패한 호스트도 상태와 함께 기록합니다.
func appendIndex(outDir string, rs []*dumpResult) error {
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(outDir, "index.tsv"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	if st, err := f.Stat(); err == nil && st.Size() == 0 {
		fmt.Fprintln(w, "host\tip\tvendor\tmodel\tbiosver\tpath\tstatus")
	}
	for _, r := range rs {
		var vendor, model, bios string
		if r.Info != nil {
			vendor, model, bios = r.Info.Vendor, r.Info.Model, r.Info.BiosVersion
		}
		status := r.Status
		if r.HTTP != 0 {
			status = fmt.Sprintf("%s(HTTP %d)", r.Status, r.HTTP)
		}
		fmt.Fprintln(w, strings.Join([]string{
			tsvField(r.Target.Hostname), tsvField(r.Target.IP), tsvField(vendor), tsvField(model),
			tsvField(bios), tsvField(r.RelDir), tsvField(status),
		}, "\t"))
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ---- 실행 ----

// runDump 는 대상 전체를 Concurrency 개 워커로 덤프하고 index.tsv 를 기록합니다 (결과는 입력 순서).
func runDump(rc *runContext, opts dumpOpts) ([]*dumpResult, error) {
	rs := make([]*dumpResult, len(rc.Targets))
	sweepHosts(len(rc.Targets), rc.Conf.Concurrency, rc.Conf.AuthFailStop,
		func(i int) hostOutcome {
			r := dumpHost(rc, rc.Targets[i], opts)
			rs[i] = r
			return outcomeOf(r.Status, r.Stats, r.Target.Err == "")
		},
		func(i int) {
			t := rc.Targets[i]
			r := &dumpResult{Target: t, Status: StatusSkippedAuthStop}
			if t.Err != "" {
				r.Status = t.Err // 이름 해석 실패는 원래 상태를 유지한다
			}
			rs[i] = r
		})
	return rs, appendIndex(opts.Out, rs)
}

func cmdDump(args []string) error {
	fs, o := newFlagSet("dump")
	targets := fs.String("targets", "", "대상(쉼표 또는 공백 구분, ip:포트 가능). 지정하면 -user 파일 대신 사용")
	out := fs.String("out", "", "덤프 저장 디렉터리 (기본: conf 의 dump_dir)")
	compact := fs.Bool("compact", false, "호스트당 1줄로 요약")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if o.fromDump != "" {
		return fmt.Errorf("-from-dump 는 dump 에 해당하지 않습니다 (dump 는 BMC 에 접속해 저장하는 명령입니다)")
	}
	if o.retryFrom != "" {
		return fmt.Errorf("-retry-from 은 check, allcheck 에서만 쓸 수 있습니다")
	}
	rc, err := prepare(o, *targets, "")
	if err != nil {
		return err
	}
	opts := dumpOpts{Out: *out, Compact: *compact}
	if opts.Out == "" {
		opts.Out = rc.Conf.DumpDir
	}
	rs, ierr := runDump(rc, opts)
	writeDumpSummary(os.Stdout, rs, opts)
	if ierr != nil {
		return fmt.Errorf("index.tsv 기록 실패: %w", ierr)
	}
	bad := 0
	for _, r := range rs {
		if r.failed() || r.Status == "SAVE_FAIL" {
			bad++
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d개 호스트에서 덤프하지 못했습니다 (요약의 상태 코드 참고)", bad)
	}
	return nil
}

// ---- 터미널 요약 ----

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// hostLabel 은 요약 헤더의 "host ip" 부분입니다 (IP 로 입력한 대상은 한 번만).
func hostLabel(t Target) string {
	if t.IP == "" || strings.Contains(t.Hostname, t.IP) {
		return t.Hostname
	}
	return t.Hostname + " " + t.IP
}

func candText(c attrCand, registryOK bool) string {
	s := c.Name + " = " + clip(c.Value, 60)
	switch a := c.Reg; {
	case a == nil && registryOK:
		s += " [레지스트리에 없음]"
	case a == nil:
	case len(a.Values) > 0:
		s += " [허용값: " + strings.Join(a.Values, " | ") + "]"
	case a.Lower != "" || a.Upper != "":
		s += fmt.Sprintf(" [%s %s..%s]", a.Type, a.Lower, a.Upper)
	case a.Type != "":
		s += " [" + a.Type + "]"
	}
	if c.Reg != nil && c.Reg.ReadOnly {
		s += " [ReadOnly]"
	}
	return s
}

func pendingText(s *hostSummary) string {
	jobs := "-"
	if s.JobsKnown {
		jobs = fmt.Sprint(s.Jobs)
	}
	diff := "-"
	if s.SettingsKnown {
		diff = fmt.Sprint(s.SettingsDiff)
	}
	verdict := "없음"
	switch {
	case s.SettingsDiff > 0 || s.Jobs > 0:
		verdict = "있음"
	case !s.SettingsKnown && !s.JobsKnown:
		verdict = "확인불가"
	}
	return fmt.Sprintf("pending=%s (settings_diff=%s jobs=%s)", verdict, diff, jobs)
}

// writeDumpSummary 는 사람이 옮겨 적어 전달하는 요약을 출력합니다.
// 호스트 헤더 1줄(host ip vendor model biosver auth=...)에만 식별 이름이 나오고, 시리얼·토큰·비밀번호는 없습니다.
func writeDumpSummary(w io.Writer, rs []*dumpResult, opts dumpOpts) {
	ok, partial, bad := 0, 0, 0
	authFails, skipped := 0, 0
	for _, r := range rs {
		switch r.Status {
		case StatusAuthFail:
			authFails++
		case StatusSkippedAuthStop:
			skipped++
		}
		switch {
		case r.failed() || r.Status == "SAVE_FAIL":
			bad++
		case r.Status == "PARTIAL":
			partial++
		default:
			ok++
		}
		if r.Sum == nil || r.Info == nil {
			line := "!! " + hostLabel(r.Target) + " " + r.Status
			if r.HTTP != 0 {
				line += fmt.Sprintf(" (HTTP %d)", r.HTTP)
			}
			fmt.Fprintln(w, line)
			continue
		}
		writeHostSummary(w, r, opts.Compact)
	}
	fmt.Fprintf(w, "-- 합계 %d대: OK %d / PARTIAL %d / 실패 %d | 저장 위치: %s (index.tsv 포함)\n", len(rs), ok, partial, bad, opts.Out)
	if warn := authStopWarning(authFails, skipped, authStopRerunDump); warn != "" {
		fmt.Fprintln(w, "!! "+warn)
	}
}

func writeHostSummary(w io.Writer, r *dumpResult, compact bool) {
	info, s := r.Info, r.Sum
	head := fmt.Sprintf("== %s %s %s %s auth=%s", hostLabel(r.Target), info.Vendor, info.Model, info.BiosVersion, r.Stats.AuthMode)
	if r.Status != "OK" {
		head += " " + r.Status
	}
	settings := "없음"
	if info.SettingsPath != "" {
		settings = info.SettingsPath + " (" + info.SettingsFrom + ")"
	}

	if compact {
		var items []string
		for _, m := range s.Std {
			var cs []string
			for _, c := range m.Cands {
				cs = append(cs, c.Name+":"+clip(c.Value, 30))
			}
			if len(cs) == 0 {
				cs = []string{"-"}
			}
			items = append(items, m.Std+"="+strings.Join(cs, ","))
		}
		fmt.Fprintf(w, "%s | settings=%s attrs=%d %s | %s\n", head, settings, s.AttrCount, pendingText(s), strings.Join(items, " "))
		return
	}

	fmt.Fprintln(w, head)
	extra := ""
	if len(info.Systems) > 1 {
		extra = fmt.Sprintf("  systems=%d(첫 번째 사용)", len(info.Systems))
	}
	fmt.Fprintf(w, "  settings=%s  bios_attrs=%d%s\n", settings, s.AttrCount, extra)
	registry := info.RegistryID
	if registry == "" || !s.RegistryOK {
		registry = "없음"
	}
	fmt.Fprintf(w, "  %s  registry=%s\n", pendingText(s), registry)
	for _, m := range s.Std {
		label := fmt.Sprintf("  %-16s ", m.Std)
		if len(m.Cands) == 0 {
			fmt.Fprintln(w, label+"후보 없음")
			continue
		}
		cands := m.Cands
		more := 0
		if len(cands) > maxShownCands {
			more = len(cands) - maxShownCands
			cands = cands[:maxShownCands]
		}
		pad := strings.Repeat(" ", len(label))
		for i, c := range cands {
			prefix := label
			if i > 0 {
				prefix = pad
			}
			fmt.Fprintln(w, prefix+candText(c, s.RegistryOK))
		}
		if more > 0 {
			fmt.Fprintf(w, "%s... 외 %d개\n", pad, more)
		}
	}
	if len(r.Notes) > 0 {
		shown := r.Notes
		if len(shown) > 3 {
			shown = shown[:3]
		}
		fmt.Fprintf(w, "  note(%d): %s\n", len(r.Notes), strings.Join(shown, "; "))
	}
}
