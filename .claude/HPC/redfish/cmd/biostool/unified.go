package main

// unified.go — 수집 통합 엔진 (계획서_v0.2_TUI.md "흐름" 3~5, 단계 2): 호스트당 1회 접속으로 프로파일 판정과
// 전체비교를 함께 얻습니다. 쓰기(PATCH/POST)는 전혀 없습니다 (check 와 같은 ModeReadOnly 경로).
//
// 엔진 API (단계 6 TUI · 단계 7 설정 · 단계 8 진입점이 씀):
//
//	runUnified(rc *runContext, o unifiedOpts) (*unifiedRun, error)
//	    대상마다 세션 1회: check 와 똑같이 읽고(ServiceRoot·Systems·System·Bios·Settings·Manager …, GET 추가 없음)
//	    프로파일 판정(hostCheck)과 함께 Bios 현재 Attributes 전체를 스냅샷(biosSnap, Manager 모델·버전 포함)으로 남긴다.
//	    기존 결과 파일(result.tsv, ok.txt, fail.tsv, retry.txt, run_info.txt)은 check 와 똑같이 쓴다.
//	    반환값이 nil 이 아니면 결과를 쓸 수 있고, 이때 error 는 결과 파일 기록 경고입니다.
//	(*unifiedRun).ProfileLines() []ResultLine          프로파일 결과 줄 (대상 입력 순서)
//	(*unifiedRun).HostOrder() []string                 대상 hostname 입력 순서 (writeCheckRes 의 hostsOrder)
//	(*unifiedRun).Compare(ref compareRef, st progStage) (*compareResult, error)
//	    남겨 둔 스냅샷으로 전체비교 (대상에는 다시 접속하지 않음). 기준은 셋 중 정확히 하나:
//	      compareRef{Hosts: []string{"host"}}  비교서버 hostname — 대상 목록에 있으면 그 스냅샷을 쓰고, 없으면 그 서버만 1회 조회
//	      compareRef{JSONPath: "refs/x.json"}  bios_export / allcheck -export-attrs 로 저장한 속성 JSON
//	      compareRef{DiffFile: "diff.txt"}     모델별 기준 목록 (allcheck 와 같은 선택 규칙)
//	    같은 모델(벤더|normalizeModel)끼리만 비교, BIOS 버전이 달라도 비교하고 BIOS_VER_DIFF 특이사항(allHost.Notes).
//	    제외 = ignore_attrs.txt(없으면 내장 기본 목록) ∪ bios.conf exclude_attrs (unifiedRun.Ignore).
//	(*unifiedRun).CompareCancel(ref, st, cancel)       Compare + 취소 (cancel 이 닫히면 남은 비교서버는 조회 안 함 → 대상 REF_CANCELED)
//	(*allHost).sameAttrNames() bool                    단계 7 설정 관문: 같은 모델 기준과 비교했고 속성 이름 목록이 같음
//	(*unifiedRun).WriteOutputs(prefix string, lines []ResultLine) ([]string, error)   결과 폴더에 CSV 4종
//	listRefJSON(dir string) ([]refFile, error)         refs/ 의 *.json 목록 (기준 고르기 화면용)
//
// 진행 표시는 progStage(진행 표시·단계 번호·이름)로 넘깁니다. P 가 nil 이면 표시하지 않습니다.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// progStage 는 엔진이 진행 표시에서 시작할 단계입니다 (단계 번호·이름은 부르는 쪽이 정함). P 가 nil 이면 아무것도 안 함.
type progStage struct {
	P    *progress
	Idx  int
	Name string
}

func (s progStage) start(total int) {
	if s.P != nil {
		s.P.Stage(s.Idx, s.Name, total)
	}
}

func (s progStage) add(tag string) {
	if s.P != nil {
		s.P.Add(tag)
	}
}

// unifiedOpts 는 runUnified 의 입력입니다.
type unifiedOpts struct {
	Profile   *Profile
	FromDump  string // 비어 있지 않으면 접속 대신 덤프 읽기 (check -from-dump 와 같음, 비교서버도 덤프에서 읽음)
	ResultDir string
	HostsPath string     // 비교서버·diff.txt 이름 해석에 쓸 hosts 파일 (-hosts)
	Ignore    *ignoreSet // ignore_attrs.txt (loadIgnore 결과). nil 이면 내장 기본 목록. bios.conf exclude_attrs 는 여기서 합친다
	Now       time.Time  // 결과 폴더 이름·리포트 시각 (0 이면 지금)
	Collect   progStage  // 접속·조회 단계 (호스트마다 Add: OK / difference / N/A / 오류)
	// Cancel 이 닫히면 아직 시작하지 않은 호스트는 접속하지 않고 CANCELED (TUI Ctrl+X). nil 이면 취소 없음
	Cancel <-chan struct{}
}

// unifiedRun 은 수집 1회의 결과입니다.
type unifiedRun struct {
	Check  *checkRun   // 프로파일 판정 (결과 폴더 Check.Dir)
	Snaps  []*biosSnap // rc.Targets 순서. 읽기에 성공한 호스트는 Attrs·MgmtModel·MgmtVersion 이 채워져 있음
	Ignore *ignoreSet  // 전체비교 제외 패턴 (ignore_attrs.txt 또는 내장 기본 ∪ exclude_attrs)

	rc        *runContext
	hostsPath string
	fromDump  string
}

// runUnified 는 대상 전체를 호스트당 세션 1회로 읽어 프로파일을 판정하고 스냅샷을 남깁니다.
func runUnified(rc *runContext, o unifiedOpts) (*unifiedRun, error) {
	ign := o.Ignore
	if ign == nil {
		ign = newIgnoreSet(defaultIgnore, "내장 기본 목록")
	}
	u := &unifiedRun{
		Snaps: make([]*biosSnap, len(rc.Targets)), Ignore: mergeExclude(ign, rc.Conf.ExcludeAttrs),
		rc: rc, hostsPath: o.HostsPath, fromDump: o.FromDump,
	}
	o.Collect.start(len(rc.Targets))
	run, err := doCheck(rc, checkOpts{
		Profile: o.Profile, FromDump: o.FromDump, ResultDir: o.ResultDir, Now: o.Now, KeepAttrs: true, Cancel: o.Cancel,
		OnHost: func(i int, h *hostCheck) {
			u.Snaps[i] = snapFromCheck(h)
			o.Collect.add(hostProgTag(h))
		},
	})
	if run == nil {
		return nil, err
	}
	u.Check = run
	return u, err
}

// snapFromCheck 는 프로파일 점검 결과에서 전체비교용 스냅샷을 만듭니다 (속성 지도는 hostCheck 에서 넘겨받아 한 벌만 남김).
func snapFromCheck(h *hostCheck) *biosSnap {
	s := &biosSnap{
		Target: h.Target, Status: h.Status, HTTP: h.HTTP, Detail: h.Detail, Stats: h.Stats,
		Attrs: h.attrs, MgmtModel: h.MgmtModel, MgmtVersion: h.MgmtVersion,
	}
	h.attrs = nil
	if h.Info != nil {
		s.Manufacturer, s.Vendor, s.Model, s.BiosVersion = h.Info.Manufacturer, h.Info.Vendor, h.Info.Model, h.Info.BiosVersion
		s.Key = strings.ToLower(s.Vendor) + "|" + normalizeModel(s.Model)
	}
	if s.Status == "" && len(s.Attrs) == 0 {
		s.Status, s.Detail = StatusUnsupported, "Bios Attributes 를 읽지 못했습니다"
	}
	return s
}

// hostProgTag 는 진행 표시 카운터용 호스트 대표 태그입니다: 호스트 오류 → 오류, difference 가 있으면 difference,
// N/A 만 있으면 N/A, 그 외 OK.
func hostProgTag(h *hostCheck) string {
	if h.Status != "" {
		return progTagErr
	}
	tag := TagOK
	for _, it := range h.Items {
		switch tagFor(it.Result) {
		case TagDiff:
			return TagDiff
		case TagNA:
			tag = TagNA
		}
	}
	return tag
}

// ProfileLines 는 프로파일 결과 줄입니다 (대상 입력 순서·항목 순서).
func (u *unifiedRun) ProfileLines() []ResultLine { return profileLines(u.Check.Hosts) }

// HostOrder 는 대상 hostname 입력 순서입니다.
func (u *unifiedRun) HostOrder() []string {
	out := make([]string, len(u.rc.Targets))
	for i, t := range u.rc.Targets {
		out[i] = t.Hostname
	}
	return out
}

// WriteOutputs 는 결과 폴더(Check.Dir)에 <prefix>summary/wide_hosts/wide_attrs/long CSV 를 쓰고 경로를 돌려줍니다.
func (u *unifiedRun) WriteOutputs(prefix string, lines []ResultLine) ([]string, error) {
	return writeResultCSVs(u.Check.Dir, prefix, lines)
}

// ---- 전체비교 ----

// compareRef 는 전체비교 기준입니다. 셋 중 정확히 하나만 채웁니다.
type compareRef struct {
	Hosts    []string // 비교서버 hostname (여러 대면 모델별로 맞는 것을 고름)
	JSONPath string   // 기준 속성 JSON 파일
	DiffFile string   // diff.txt (모델별 기준 hostname 목록)
}

func (r compareRef) count() int {
	n := 0
	for _, set := range []bool{len(r.Hosts) > 0, r.JSONPath != "", r.DiffFile != ""} {
		if set {
			n++
		}
	}
	return n
}

// compareResult 는 전체비교 1회의 결과입니다.
type compareResult struct {
	Ref    compareRef
	Label  string     // 사람이 읽는 기준 설명 (예: "비교서버 host1", "refs JSON x.json", "diff.txt")
	Ignore *ignoreSet // 적용한 제외 패턴
	Refs   []*allRef  // 기준 (읽기 실패 포함)
	// Hosts 는 대상마다 1개(Snaps 순서)입니다. Status SAME/DIFF 면 비교함(Ref·Diffs·Notes), 아니면 NO_REFERENCE·
	// REFERENCE·REF_UNREACHABLE·REF_CANCELED·접속 오류 상태. 설정 관문은 (*allHost).sameAttrNames().
	Hosts []*allHost
	Lines []ResultLine // compareLines (대상 순서, 대상 안에서는 속성 이름순)
	// RefCanceled 는 취소(Ctrl+X)로 조회하지 않은 비교서버 수입니다. 그 기준이 필요한 대상은 REF_CANCELED (N/A, 설정 제외).
	RefCanceled int
}

// sameAttrNames 는 단계 7 설정 관문입니다: 같은 모델의 기준과 비교했고(SAME/DIFF) 제외 속성을 뺀 속성 이름 목록이
// 기준과 같다(ONLY_REF·ONLY_HOST 0건). BIOS 버전은 달라도 됩니다.
func (h *allHost) sameAttrNames() bool {
	return h.compared() && h.NOnlyRef == 0 && h.NOnlyHost == 0
}

// Compare 는 남겨 둔 스냅샷을 ref 기준으로 비교합니다. 대상에는 다시 접속하지 않고, 대상 목록에 없는 비교서버·
// diff.txt 기준만 1회씩 읽습니다 (st 단계에서 진행 표시). 같은 unifiedRun 으로 여러 번 불러도 됩니다.
func (u *unifiedRun) Compare(ref compareRef, st progStage) (*compareResult, error) {
	return u.CompareCancel(ref, st, nil)
}

// CompareCancel 은 Compare 에 취소를 더한 것입니다 (TUI Ctrl+X). cancel 이 닫히면 아직 시작하지 않은 비교서버는 접속하지
// 않고(CANCELED), 그 기준이 필요한 대상은 REF_CANCELED 로 남긴 채 나머지는 그대로 비교합니다. nil 이면 취소 없음.
func (u *unifiedRun) CompareCancel(ref compareRef, st progStage, cancel <-chan struct{}) (*compareResult, error) {
	refs, label, err := u.compareRefs(ref, st, cancel)
	if err != nil {
		return nil, err
	}
	env := &allEnv{ign: u.Ignore, refByID: map[string]*allRef{}, byKey: map[string][]*allRef{}}
	for _, r := range refs {
		env.refByID[r.ID] = r
	}
	env.indexRefs(refs)
	res := &compareResult{Ref: ref, Label: label, Ignore: u.Ignore, Refs: refs}
	for _, r := range refs {
		if r.Snap != nil && r.Snap.Status == StatusCanceled {
			res.RefCanceled++
		}
	}
	for _, s := range u.Snaps {
		h := env.finish(&allHost{Target: s.Target, Snap: s})
		if h.Status == StatusNoReference && ref.DiffFile == "" {
			h.Detail = "기준(" + label + ")과 모델이 다름 (대상 모델 키 " + s.Key + ")"
		}
		res.Hosts = append(res.Hosts, h)
		res.Lines = append(res.Lines, compareLines(h, u.Ignore)...)
	}
	return res, nil
}

// compareRefs 는 기준을 준비합니다: JSON 은 파일에서, 비교서버·diff.txt 는 대상 스냅샷을 재사용하거나 새로 읽습니다.
func (u *unifiedRun) compareRefs(ref compareRef, st progStage, cancel <-chan struct{}) ([]*allRef, string, error) {
	if ref.count() != 1 {
		return nil, "", fmt.Errorf("전체비교 기준은 비교서버(-ref)·기준 JSON(-ref-json)·diff.txt(-diff) 중 하나만 지정하십시오")
	}
	if ref.JSONPath != "" {
		r, err := refFromJSON(ref.JSONPath)
		if err != nil {
			return nil, "", err
		}
		st.start(0)
		return []*allRef{r}, "refs JSON " + filepath.Base(ref.JSONPath), nil
	}
	var entries []refEntry
	var label string
	if len(ref.Hosts) > 0 {
		entries = parseRefList(strings.NewReplacer(",", "\n", ";", "\n").Replace(strings.Join(ref.Hosts, "\n")))
		if len(entries) == 0 {
			return nil, "", fmt.Errorf("비교서버 hostname 이 없습니다")
		}
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Input
		}
		label = "비교서버 " + strings.Join(names, ", ")
	} else {
		var err error
		if entries, err = loadRefList(ref.DiffFile); err != nil {
			return nil, "", err
		}
		label = ref.DiffFile
	}
	refs, err := newAllRefs(entries, u.hostsPath)
	if err != nil {
		return nil, "", err
	}
	// 대상 목록에 있는 기준은 이미 읽은 스냅샷을 쓴다 (같은 BMC 에 다시 로그인하지 않음).
	byID := map[string]*biosSnap{}
	for _, s := range u.Snaps {
		if id := targetID(s.Target); byID[id] == nil {
			byID[id] = s
		}
	}
	var todo []*allRef
	for _, r := range refs {
		if s := byID[r.ID]; s != nil {
			r.Snap = s
			continue
		}
		todo = append(todo, r)
	}
	st.start(len(todo))
	if len(todo) == 0 {
		return refs, label, nil
	}
	env := &allEnv{conf: u.rc.Conf, password: u.rc.Password, fallback: u.rc.Fallback, offline: u.fromDump != "", cancel: cancel}
	authStop := u.rc.Conf.AuthFailStop
	if env.offline {
		authStop = 0 // 덤프 읽기는 BMC 에 로그인하지 않는다
		if env.dumps, err = indexDumpHosts(u.fromDump); err != nil {
			return nil, "", err
		}
	}
	ts := make([]Target, len(todo))
	for i, r := range todo {
		ts[i] = r.Target
	}
	env.sweepSnaps(ts, authStop, func(i int, s *biosSnap) {
		todo[i].Snap = s
		switch {
		case s.ok():
			st.add(TagOK)
		case s.Status == StatusCanceled:
			st.add(TagNA)
		default:
			st.add(progTagErr)
		}
	})
	return refs, label, nil
}

// ---- 기준 속성 JSON (refs/) ----

// refFile 은 refs 폴더의 기준 JSON 1개입니다 (기준 고르기 화면용).
type refFile struct {
	Name        string // 파일 이름 (예: dl360g11.json)
	Path        string
	Host        string
	Vendor      string
	Model       string
	BiosVersion string
	Attrs       int    // 속성 수
	Err         string // 읽기·형식 오류 (비어 있지 않으면 기준으로 쓸 수 없음)
}

// readRefJSON 은 allexport.go(exportAttrs) 형식의 JSON 을 읽습니다. 속성 값은 parseAttrs 와 같은 규칙으로 문자열로 바꿉니다
// (손으로 고친 파일에 숫자·불리언이 있어도 BMC 응답과 같은 표기로 비교되게).
func readRefJSON(path string) (*attrExport, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("기준 JSON %s: %w", path, err)
	}
	var doc struct {
		Host        string                     `json:"host"`
		IP          string                     `json:"ip"`
		Vendor      string                     `json:"vendor"`
		Model       string                     `json:"model"`
		BiosVersion string                     `json:"bios_version"`
		Attributes  map[string]json.RawMessage `json:"attributes"`
	}
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), &doc); err != nil {
		return nil, fmt.Errorf("기준 JSON %s: JSON 형식이 아닙니다 (%v)", path, err)
	}
	if len(doc.Attributes) == 0 {
		return nil, fmt.Errorf("기준 JSON %s: attributes 가 없습니다 (bios_export 또는 allcheck -export-attrs 로 만든 파일인지 확인)", path)
	}
	if strings.TrimSpace(doc.Model) == "" {
		return nil, fmt.Errorf("기준 JSON %s: model 이 없습니다 (같은 모델끼리만 비교하므로 필요)", path)
	}
	e := &attrExport{Host: strings.TrimSpace(doc.Host), IP: strings.TrimSpace(doc.IP), Vendor: strings.TrimSpace(doc.Vendor),
		Model: strings.TrimSpace(doc.Model), BiosVersion: strings.TrimSpace(doc.BiosVersion), Attributes: make(map[string]string, len(doc.Attributes))}
	for k, raw := range doc.Attributes {
		e.Attributes[k] = rawToString(raw)
	}
	return e, nil
}

// refFromJSON 은 기준 JSON 을 비교 기준(allRef)으로 만듭니다. ID 는 "json|경로" 라 대상과 같은 호스트로 보지 않습니다.
func refFromJSON(path string) (*allRef, error) {
	e, err := readRefJSON(path)
	if err != nil {
		return nil, err
	}
	host := e.Host
	if host == "" {
		host = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	t := Target{Input: path, Hostname: host, IP: e.IP}
	s := &biosSnap{Target: t, Manufacturer: e.Vendor, Vendor: normalizeVendor(e.Vendor), Model: e.Model, BiosVersion: e.BiosVersion, Attrs: e.Attributes}
	s.Key = strings.ToLower(s.Vendor) + "|" + normalizeModel(s.Model)
	return &allRef{Entry: refEntry{Input: path}, Target: t, ID: "json|" + path, Snap: s}, nil
}

// listRefJSON 은 dir 의 *.json 을 이름순으로 돌려줍니다. 폴더가 없으면 빈 목록이고, 읽을 수 없는 파일은 Err 를 채워 넣습니다.
func listRefJSON(dir string) ([]refFile, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("기준 JSON 폴더 %s: %w", dir, err)
	}
	var out []refFile
	for _, de := range ents {
		if de.IsDir() || !strings.EqualFold(filepath.Ext(de.Name()), ".json") {
			continue
		}
		p := filepath.Join(dir, de.Name())
		f := refFile{Name: de.Name(), Path: p}
		if e, err := readRefJSON(p); err != nil {
			f.Err = err.Error()
		} else {
			f.Host, f.Vendor, f.Model, f.BiosVersion, f.Attrs = e.Host, normalizeVendor(e.Vendor), e.Model, e.BiosVersion, len(e.Attributes)
		}
		out = append(out, f)
	}
	return out, nil
}
