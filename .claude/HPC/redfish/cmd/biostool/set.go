package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// set.go 는 check 의 Y 응답 뒤 FAIL 항목을 BIOS Pending 으로 설정하는 경로(단계 6)와 -dry-run 출력입니다.
//
// 이 도구에서 BMC 에 쓰기를 하는 유일한 경로입니다. 재부팅·리셋·즉시 적용은 하지 않습니다.
// 안전장치 (겹겹이):
//  1. 입력 재검증: 점검이 FAIL 로 넘긴 항목을 프로파일과 다시 대조해 모델을 지정한(model≠*) verified=Y 인
//     같은 속성·기대값 행이 있는 것만 남긴다. BMC 모델명이 비면 쓰지 않는다.
//  2. 벤더 제한: Dell·HPE·Lenovo 만 쓴다 (Cisco·Supermicro·기타는 접속도 하지 않음, 계획서 2장).
//  3. 재조회: 로그인 후 Bios 현재값·Settings Pending·레지스트리를 다시 읽어, 점검 이후 바뀐 호스트는 쓰지 않는다
//     (ALREADY_OK / PENDING_EXISTS / CHANGED). Dell 은 Pending 이 기대값이어도 예약된 BIOS 설정 Job 이 없으면
//     (PENDING_NO_JOB) 같은 값을 다시 PATCH 하고 Job 을 만든다 — Job 존재를 확인하지 못하면 쓰지 않는다.
//  4. 레지스트리 사전검증: ReadOnly·허용값 밖이면 보내지 않는다. 시스템 프로파일이 바뀌면 나머지 항목은 미룬다.
//  5. 본문: 프로파일 속성만 {"Attributes":{...}} + (Bios 가 지원할 때만) ApplyTime=OnReset. 호스트당 PATCH 1회, 재시도 없음.
//     Dell 은 Job 이 자동 생성되지 않았을 때만 TargetSettingsURI 하나만 담은 Job 을 최대 1회 POST (재부팅 필드 없음).
//  6. redfish.go 의 허용목록(checkAllowed)·본문 검사(checkBody)가 전송 직전에 다시 막는다.
//
// -dry-run 은 같은 계획 함수(buildPlan)·같은 출력 함수(writePlan)를 쓰며, 읽기 전용 Client(ModeReadOnly)라 쓰기가 구조적으로 불가능합니다.

// 설정 결과 상태 (항목 단위). 쓰기 전 호스트 오류는 redfish.go 의 StatusXxx 를 그대로 씁니다.
const (
	StatusApplied               = "APPLIED"                  // Pending 반영을 재조회로 확인
	StatusApplyUnconfirmed      = "APPLY_UNCONFIRMED"        // PATCH 는 받아들여졌으나 반영 확인 불가 (Dell Job 실패 포함)
	StatusAlreadyOK             = "ALREADY_OK"               // 재조회해 보니 현재값 또는 Pending 이 이미 기대값 → 쓰지 않음
	StatusChanged               = "CHANGED"                  // 점검 이후 모델·경로·속성이 바뀜 → 쓰지 않음
	StatusSkippedReadOnly       = "SKIPPED_READONLY"         // 레지스트리에서 ReadOnly → 보내지 않음
	StatusInvalidValue          = "INVALID_VALUE"            // 허용값 밖·형식 불일치·안전 검사 거부 → 보내지 않음
	StatusDependsOnProfile      = "DEPENDS_ON_PROFILE"       // 시스템 프로파일 변경이 먼저 → 재부팅 후 재점검
	StatusSetNotSupportedVendor = "SET_NOT_SUPPORTED_VENDOR" // Dell·HPE·Lenovo 외 → 쓰지 않음
	StatusRejected              = "REJECTED"                 // BMC 가 400 등으로 거부
	StatusSetUnsupported        = "SET_UNSUPPORTED"          // 405/501/404, Settings·Job 경로를 만들 수 없음
	StatusSetError              = "SET_ERROR"                // 5xx·타임아웃·연결 끊김: 쓰기 결과 불명 (재시도하지 않음)
	StatusDuplicateTarget       = "DUPLICATE_TARGET"         // 같은 BMC 를 가리키는 앞의 대상에서 처리 → 두 번 쓰지 않음
)

// setVendors 는 쓰기를 허용하는 벤더입니다 (normalizeVendor 결과).
var setVendors = map[string]bool{"Dell": true, "HPE": true, "Lenovo": true}

// profileStd 는 시스템 프로파일 항목(Dell SysProfile, HPE WorkloadProfile 등)의 std_name 입니다.
const profileStd = "system_profile"

const applyNotice = `※ 재부팅은 하지 않았습니다. 설정은 BMC 의 Pending 까지이며, 다음 재부팅(OS 설치 등) 때 BIOS 에 반영됩니다.
※ 반영(재부팅) 후 bios_check.sh 로 재점검하십시오.
※ [Disclaimer] 설정 변경 후 랜덤한 서버 몇 대를 직접 확인해 실제 변경되었는지 확인하십시오.`

// setItem 은 설정 대상 항목 1개입니다.
type setItem struct {
	Std, Attr, Expected string
	Unverified          bool // dry-run 시험 출력용 UNVERIFIED 항목 (실제 설정 대상이 아님)
	Value               string
	Status              string // 비어 있으면 아직 보낼 후보
	Detail              string

	allowed []string        // 프로파일 행의 허용값
	raw     json.RawMessage // 보낼 값의 JSON (레지스트리 형식에 맞춤)
}

// set 은 상태가 아직 없을 때만 정합니다 (먼저 정해진 사유를 덮어쓰지 않음).
func (it *setItem) set(st, detail string) {
	if it.Status == "" {
		it.Status, it.Detail = st, detail
	}
}

// setPlan 은 호스트 1대에 보낼 요청입니다. Body·JobBody 는 checkBody 를 거친 정규형으로 실제 전송 바이트와 같습니다.
type setPlan struct {
	Items     []*setItem // 본문에 넣은 항목
	Deferred  []*setItem // 프로파일 종속으로 미룬 항목
	DeferWhy  string
	Path      string // PATCH 경로 (전송형)
	Body      []byte
	JobPath   string // Dell 만: Managers/<id>/Jobs
	JobBody   []byte
	ErrStatus string // 본문을 만들지 못했으면 상태와 사유 (보내지 않음)
	Err       string
}

func (p *setPlan) ready() bool { return p != nil && p.Err == "" && len(p.Items) > 0 }

// setHost 는 호스트 1대의 설정 진행 상황입니다.
type setHost struct {
	Target    Target
	Info      *SystemInfo // 점검 때 정보
	Items     []*setItem
	Plan      *setPlan // 실제 적용 계획 (dry-run 에서는 "Y 면 보낼 요청")
	Trial     *setPlan // dry-run: 미검증 항목까지 넣은 시험 계획
	Tried     bool     // PATCH 를 시도했는지 (apply_requests.txt 대상, 결과 줄에 실제 결과)
	Sent      bool     // PATCH 가 실제로 BMC 로 전송됐는지 (허용목록 등에서 막혔으면 false)
	Job       string   // Dell Job 처리 메모
	RegNote   string   // 레지스트리를 못 읽은 사유 (비면 레지스트리로 사전검증함)
	DupOf     string   // 같은 BMC(IP:포트)를 가리키는 앞의 대상 이름 (있으면 이 대상은 처리하지 않음)
	Notes     []string
	Stats     CallStats
	contacted bool
}

func (h *setHost) open() []*setItem {
	var out []*setItem
	for _, it := range h.Items {
		if it.Status == "" {
			out = append(out, it)
		}
	}
	return out
}

// finish 는 아직 상태가 없는 항목 전부에 상태를 정합니다.
func (h *setHost) finish(st, detail string) {
	for _, it := range h.Items {
		it.set(st, detail)
	}
}

func (h *setHost) finishErr(err error) {
	st, _ := errStatus(err)
	detail := errBrief(err)
	var rf *RFError
	if errors.As(err, &rf) && rf.Detail != "" {
		detail = rf.Detail
	}
	h.finish(st, detail)
}

// authStatus 는 AUTH_FAIL 차단기에 넘길 호스트 상태입니다 (항목 중 AUTH_FAIL 이 있으면 AUTH_FAIL).
func (h *setHost) authStatus() string {
	for _, it := range h.Items {
		if it.Status == StatusAuthFail {
			return StatusAuthFail
		}
	}
	return ""
}

// setEnv 는 설정(또는 dry-run) 1회의 공통 상태입니다.
type setEnv struct {
	conf     *Config
	password string
	prof     *Profile
	apply    bool              // false 면 dry-run (쓰기 없음, ModeReadOnly)
	fromDump map[string]string // dry-run -from-dump 일 때 호스트 덤프 폴더
	regs     *regCache
}

func newSetEnv(rc *runContext, run *checkRun, apply bool) (*setEnv, error) {
	if run.prof == nil {
		return nil, errors.New("점검에 쓴 프로파일 정보가 없어 설정할 수 없습니다")
	}
	e := &setEnv{conf: rc.Conf, password: rc.Password, prof: run.prof, apply: apply, regs: &regCache{m: map[string]*regEntry{}}}
	if run.FromDump != "" {
		if apply {
			return nil, errors.New("덤프 읽기 모드에서는 설정할 수 없습니다")
		}
		idx, err := indexDumpHosts(run.FromDump)
		if err != nil {
			return nil, err
		}
		e.fromDump = idx
	}
	return e, nil
}

// ---- 입력 ----

// bmcKey 는 같은 BMC 판정 키입니다: IP 는 net.ParseIP 로 정규화(IPv6 압축 표기·IPv4-mapped 를 같게, 소문자),
// 포트는 생략 시 443.
func bmcKey(t Target) string {
	ip := strings.ToLower(t.IP)
	if p := net.ParseIP(t.IP); p != nil {
		ip = p.String()
	}
	return ip + "|" + orDefault(t.Port, "443")
}

// markDuplicates 는 같은 BMC 를 가리키는 두 번째 이후 대상에 DupOf 를 표시합니다 (설정·dry-run 공용).
func markDuplicates(hosts []*setHost) {
	first := map[string]*setHost{}
	for _, h := range hosts {
		if h.Target.IP == "" {
			continue
		}
		k := bmcKey(h.Target)
		if f := first[k]; f != nil {
			h.DupOf = f.Target.Hostname
		} else {
			first[k] = h
		}
	}
}

// applyHosts 는 승인된 FAIL 항목을 호스트별로 묶습니다 (입력 순서 유지).
// user.txt 의 서로 다른 줄(hostname / hostname-m / IP)이 같은 BMC 로 해석되면 첫 대상만 처리하고
// 나머지는 DupOf 를 표시합니다 (같은 BMC 에 PATCH·Job 이 두 번, 동시에 나가는 것을 막음).
func applyHosts(fails []failItem) []*setHost {
	var out []*setHost
	idx := map[Target]*setHost{}
	for _, f := range fails {
		h := idx[f.Target]
		if h == nil {
			h = &setHost{Target: f.Target, Info: f.Info}
			idx[f.Target] = h
			out = append(out, h)
		}
		h.Items = append(h.Items, &setItem{Std: f.Std, Attr: f.Attr, Expected: f.Expected})
	}
	markDuplicates(out)
	return out
}

// dryRunHosts 는 dry-run 대상(설정 대상 항목 + UNVERIFIED)을 호스트별로 모읍니다.
// 중복 판정은 applyHosts 와 같아서, Y 응답 시 보낼 PATCH 건수와 dry-run 출력이 일치합니다.
func dryRunHosts(run *checkRun) []*setHost {
	var out []*setHost
	for _, h := range run.Hosts {
		if h.Status != "" {
			continue
		}
		var items []*setItem
		for _, it := range h.Items {
			if h.settable(it) || it.Result == StatusUnverified {
				items = append(items, &setItem{Std: it.Std, Attr: it.Attr, Expected: it.Expected, Unverified: it.Result == StatusUnverified})
			}
		}
		if len(items) > 0 {
			out = append(out, &setHost{Target: h.Target, Info: h.Info, Items: items})
		}
	}
	markDuplicates(out)
	return out
}

// ---- 실행 ----

// doApply 는 confirmApply 가 Y 를 받았을 때 부르는 실제 설정입니다 (report.go 의 applyFails).
func doApply(rc *runContext, run *checkRun, fails []failItem, w io.Writer, o reportOpts) error {
	e, err := newSetEnv(rc, run, true)
	if err != nil {
		return err
	}
	hosts := applyHosts(fails)
	fmt.Fprintf(w, "\n설정 시작: 호스트 %s대, 항목 %s건 (재조회 후 Pending 으로만 설정, 재부팅 없음)\n", commaInt(len(hosts)), commaInt(len(fails)))
	e.sweep(hosts)
	ferr := writeApplyFiles(run.Dir, hosts)
	writeApplyReport(w, run.Dir, hosts, o)
	if ferr != nil {
		return fmt.Errorf("설정 결과 파일 기록 실패: %w", ferr)
	}
	return nil
}

// doDryRun 은 쓰기 없이 호스트별로 보낼 PATCH 경로·본문(Job 포함 여부)을 출력합니다. Y/N 을 묻지 않습니다.
func doDryRun(rc *runContext, run *checkRun, w io.Writer) error {
	hosts := dryRunHosts(run)
	if len(hosts) == 0 {
		fmt.Fprintln(w, "\ndry-run: FAIL·UNVERIFIED 항목이 없어 보낼 요청이 없습니다 (BMC 에 쓰지 않음).")
		return nil
	}
	e, err := newSetEnv(rc, run, false)
	if err != nil {
		return err
	}
	e.sweep(hosts)
	writeDryRun(w, hosts, run.FromDump != "")
	return nil
}

// sweep 은 호스트를 sweepHosts(AUTH_FAIL 차단기·병렬)로 처리합니다.
func (e *setEnv) sweep(hosts []*setHost) {
	authStop := e.conf.AuthFailStop
	if e.fromDump != nil {
		authStop = 0 // 덤프 읽기는 로그인하지 않는다
	}
	sweepHosts(len(hosts), e.conf.Concurrency, authStop,
		func(i int) hostOutcome {
			h := hosts[i]
			e.runHost(h)
			return outcomeOf(h.authStatus(), h.Stats, h.contacted)
		},
		func(i int) {
			hosts[i].finish(StatusSkippedAuthStop, "AUTH_FAIL 차단기(auth_fail_stop)가 작동해 접속하지 않았습니다")
		})
}

// runHost 는 호스트 1대를 처리합니다: 접속 전 검사 → 로그인 → 재조회·계획 → (적용이면) 전송.
func (e *setEnv) runHost(h *setHost) {
	if h.DupOf != "" {
		h.finish(StatusDuplicateTarget, "같은 BMC 를 가리키는 앞의 대상("+h.DupOf+")에서 처리 — 두 번 쓰지 않음")
		return
	}
	for _, it := range h.Items {
		e.verifyItem(h, it)
	}
	switch {
	case h.Info == nil || !setVendors[h.Info.Vendor]:
		h.finish(StatusSetNotSupportedVendor, "쓰기 대상 벤더가 아님 — Dell·HPE·Lenovo 만 설정합니다 (계획서 2장)")
		return
	case h.Info.SettingsPath == "":
		h.finish(StatusSetUnsupported, "점검 때 Bios Settings 경로가 없었음")
		return
	case len(h.open()) == 0:
		return
	}
	if e.fromDump != nil {
		dir, ok := e.fromDump[strings.ToLower(dirPart(h.Target.Hostname))]
		if !ok {
			h.finish(StatusNoDump, "-from-dump 폴더에 이 호스트의 덤프가 없음")
			return
		}
		get, err := newDumpFileGet(dir)
		if err != nil {
			h.finish(StatusHTTPError, "덤프 폴더를 읽을 수 없습니다")
			return
		}
		e.planHost(h, newMemoGet(get))
		return
	}
	mode := ModeReadOnly // dry-run 은 쓰기가 구조적으로 불가능한 읽기 전용 Client 를 쓴다
	if e.apply {
		mode = ModeSet
	}
	c := NewClient(ClientOpts{
		BaseURL: h.Target.BaseURL(), User: e.conf.User, Pass: e.password, Insecure: e.conf.Insecure,
		Timeout: e.conf.Timeout, Retries: e.conf.Retries, Mode: mode, Gap: checkClientGap,
	})
	h.contacted = true
	defer func() {
		if err := c.Close(); err != nil {
			h.Notes = append(h.Notes, "세션 삭제: "+errBrief(err))
		}
		h.Stats = c.Stats()
	}()
	if err := c.Login(); err != nil {
		h.finishErr(err)
		return
	}
	if e.planHost(h, newMemoGet(c.GetRaw)) && e.apply {
		e.send(h, c)
	}
}

// verifyItem 은 항목을 프로파일과 다시 대조합니다 (점검 결과를 그대로 믿지 않는 방어 중첩).
// 같은 (벤더, 모델, 항목) 후보 행 중 속성 이름(대소문자 무시)·기대값이 같은 행이 있어야 하고,
// 실제 적용 항목은 그 행이 모델을 지정한(model≠*) verified=Y 행이어야 합니다 (parseProfile 도 * 행의 Y 를 거부).
// BMC 가 보고한 모델이 비어 있으면 쓰지 않습니다. 보낼 값은 허용값 A|B 의 첫 값입니다.
func (e *setEnv) verifyItem(h *setHost, it *setItem) {
	if h.Info != nil {
		if !it.Unverified && normalizeModel(h.Info.Model) == "" {
			it.set(StatusUnverified, "BMC 가 보고한 모델명이 비어 있어 설정하지 않음 (모델을 지정한 verified=Y 행만 쓴다)")
			return
		}
		for _, r := range e.prof.rowsFor(h.Info.Vendor, h.Info.Model, it.Std) {
			if !strings.EqualFold(r.Attr, it.Attr) || r.Value != it.Expected {
				continue
			}
			if (r.Verified && r.modelKey != "*") || it.Unverified {
				it.allowed, it.Value = r.Allowed, r.Allowed[0]
				return
			}
		}
	}
	it.set(StatusUnverified, "프로파일 재검증 실패: 모델을 지정한 verified=Y 행 중 같은 속성·기대값 행이 없음 (설정하지 않음)")
}

// bv 는 BMC 가 준 문자열을 사유에 넣을 때 쓰는 형태입니다 (제어문자 제거, 60자).
func bv(s string) string { return sanitizeDetail(clip(s, 60)) }

// planHost 는 재조회로 상태를 다시 판정하고 보낼 계획을 만듭니다. 보낼 것이 있으면 true.
// m 은 온라인(Client.GetRaw)이나 -from-dump(덤프 파일) 어느 쪽이든 GET 만 합니다.
func (e *setEnv) planHost(h *setHost, m *memoGet) bool {
	info, err := detectSystemWith(m.fetch)
	if err != nil {
		h.finishErr(err)
		return false
	}
	if why := changedWhy(h.Info, info); why != "" {
		h.finish(StatusChanged, why)
		return false
	}
	bb, err := m.fetch(info.BiosPath)
	if err != nil {
		h.finishErr(err)
		return false
	}
	var bios struct {
		Attributes map[string]json.RawMessage
		Settings   struct{ SupportedApplyTimes []string } `json:"@Redfish.Settings"`
	}
	if json.Unmarshal(bb, &bios) != nil || len(bios.Attributes) == 0 {
		h.finish(StatusHTTPError, "Bios 응답을 해석할 수 없거나 Attributes 가 비어 있음")
		return false
	}
	cur := make(map[string]string, len(bios.Attributes))
	for k, v := range bios.Attributes {
		cur[k] = rawToString(v)
	}
	sb, err := m.fetch(info.SettingsPath)
	if err != nil {
		if _, code := errStatus(err); code == http.StatusNotFound {
			h.finish(StatusSetUnsupported, "Settings 리소스를 읽을 수 없음 (HTTP 404) — Pending 을 확인할 수 없어 쓰지 않음")
		} else {
			h.finishErr(err)
		}
		return false
	}
	pend, err := parseAttrs(sb)
	if err != nil {
		h.finishErr(err)
		return false
	}

	// 우리 표준 속성(이 호스트에 매핑되는 프로파일 행)의 허용값. 그 값으로 대기 중인 Pending 은 남의 것이 아니다.
	ours := map[string][]string{}
	profAttr := ""
	for _, std := range e.prof.Items {
		for _, r := range e.prof.rowsFor(info.Vendor, info.Model, std) {
			if name, ok := lookupAttr(cur, r.Attr); ok {
				ours[name] = r.Allowed
				if std == profileStd {
					profAttr = name
				}
				break
			}
		}
	}
	for _, it := range h.Items {
		if it.allowed != nil {
			ours[it.Attr] = it.allowed
		}
	}

	var pendOurs []*setItem // Dell: Pending 이 이미 기대값인 항목 (BIOS 설정 Job 이 있어야 반영됨)
	for _, it := range h.open() {
		c, ok := cur[it.Attr]
		if !ok {
			it.set(StatusChanged, "속성 "+it.Attr+" 이(가) 지금 BMC 에 없음")
			continue
		}
		pv, hasP := pend[it.Attr]
		switch {
		case matchAny(it.allowed, c): // 정수는 정규형으로 비교 ("05" 와 5 는 같음) → 같은 값을 다시 보내지 않는다
			it.set(StatusAlreadyOK, "현재값이 이미 기대값 ("+bv(c)+")")
		case hasP && matchAny(it.allowed, pv) && info.Vendor == "Dell":
			pendOurs = append(pendOurs, it)
		case hasP && matchAny(it.allowed, pv):
			it.set(StatusAlreadyOK, "Pending 이 이미 기대값 ("+bv(pv)+", 재부팅 대기)")
		}
	}
	if len(pendOurs) > 0 {
		// Dell 은 Pending 만 있고 BIOS 설정 Job 이 없으면 재부팅해도 반영되지 않을 수 있다.
		// Job 이 없다고 확인되면 같은 값을 다시 PATCH 하고 Job 을 만든다 (기존 PATCH 1회 + Job 최대 1회 경로 그대로).
		// 확인하지 못하면 쓰지 않는다.
		found, known := false, false
		if info.ManagerPath != "" {
			found, known = dellBiosJob(m.fetch, strings.TrimRight(info.ManagerPath, "/")+"/Jobs")
		}
		for _, it := range pendOurs {
			pv := pend[it.Attr]
			switch {
			case found:
				it.set(StatusAlreadyOK, "Pending 이 이미 기대값 ("+bv(pv)+", 재부팅 대기, BIOS 설정 Job 예약 확인)")
			case !known:
				it.set(StatusAlreadyOK, "Pending 이 이미 기대값 ("+bv(pv)+")이나 BIOS 설정 Job 을 확인하지 못해 쓰지 않음 — iDRAC Job Queue 확인 필요")
			}
		}
		if known && !found {
			h.Notes = append(h.Notes, "Dell: 기대값 Pending 에 BIOS 설정 Job 이 없어 같은 값을 다시 보내고 Job 을 만듦")
		}
	}
	// 남의 Pending: 현재값과 다르고 우리 표준값도 아닌 Pending 이 하나라도 있으면 아무것도 쓰지 않는다.
	var foreign []string
	for k, v := range pend {
		if c, ok := cur[k]; ok && c == v {
			continue
		}
		if al, ok := ours[k]; ok && matchAny(al, v) {
			continue
		}
		foreign = append(foreign, k+"="+v)
	}
	if len(foreign) > 0 {
		sort.Strings(foreign)
		more := ""
		if len(foreign) > 3 {
			more = fmt.Sprintf(" 외 %d건", len(foreign)-3)
			foreign = foreign[:3]
		}
		h.finish(StatusPendingExists, "남의 Pending 이 이미 있음 ("+bv(strings.Join(foreign, ", "))+more+") — 건드리지 않음")
		return false
	}

	// 레지스트리 사전검증 (벤더|모델|BIOS버전 당 1회만 내려받는다)
	var reg *attrRegistry
	if info.RegistryPath == "" {
		h.RegNote = "레지스트리 위치를 알 수 없음"
	} else {
		why := ""
		load := func() *attrRegistry {
			b, err := m.fetch(info.RegistryPath)
			if err != nil {
				why = errBrief(err)
				return nil
			}
			if r, err := parseRegistry(b); err == nil && len(r.byName) > 0 {
				return r
			}
			why = "해석할 수 없거나 비어 있음"
			return nil
		}
		key := strings.ToLower(info.Vendor + "|" + info.Model + "|" + info.BiosVersion)
		reg = e.regs.get(key, load)
		if reg == nil {
			// 캐시된 실패(다른 호스트의 일시 오류일 수 있음)를 그대로 믿지 않고 이 호스트에서 1회 다시 받는다.
			// (이 호스트가 방금 실패한 경우는 memoGet 이 같은 결과를 돌려주므로 다시 보내지 않는다)
			reg = load()
		}
		if reg == nil {
			h.RegNote = "레지스트리를 읽지 못함 (" + why + ")"
		}
	}
	for _, it := range h.open() {
		raw, st, why := typedValue(it.Value, reg, it.Attr, bios.Attributes[it.Attr])
		if st != "" {
			it.set(st, why)
			continue
		}
		it.raw, it.Value = raw, rawToString(raw) // 재조회 비교도 보낸 값의 정규형으로 (예: 정수 "05" → 5)
	}

	onReset := false
	for _, t := range bios.Settings.SupportedApplyTimes {
		if t == "OnReset" {
			onReset = true
		}
	}
	pendProf := ""
	if profAttr != "" {
		if pv, ok := pend[profAttr]; ok && pv != cur[profAttr] {
			pendProf = pv // 이미 우리 표준값으로 프로파일 변경이 대기 중 (남의 값이면 위에서 PENDING_EXISTS)
		}
	}
	var verified, all []*setItem
	for _, it := range h.open() {
		all = append(all, it)
		if !it.Unverified {
			verified = append(verified, it)
		}
	}
	h.Plan = buildPlan(info, onReset, reg != nil, pendProf, verified)
	if p := h.Plan; p != nil {
		for _, it := range p.Deferred {
			it.set(StatusDependsOnProfile, p.DeferWhy)
		}
		if p.Err != "" {
			for _, it := range p.Items {
				it.set(p.ErrStatus, p.Err)
			}
		}
	}
	if !e.apply && len(all) > len(verified) {
		h.Trial = buildPlan(info, onReset, reg != nil, pendProf, all)
	}
	return h.Plan.ready()
}

// changedWhy 는 점검 때와 지금의 시스템이 다르면 사유를 돌려줍니다 (같으면 "").
func changedWhy(old, now *SystemInfo) string {
	switch {
	case vendorKey(old.Vendor) != vendorKey(now.Vendor) || normalizeModel(old.Model) != normalizeModel(now.Model):
		return "모델이 점검 때(" + bv(modelLabel(old)) + ")와 다름: 지금 " + bv(modelLabel(now))
	case now.BiosPath == "" || now.SettingsPath == "":
		return "지금은 Bios Settings 경로가 없음"
	case !strings.EqualFold(path.Clean(old.SettingsPath), path.Clean(now.SettingsPath)):
		return "Settings 경로가 점검 때와 다름"
	}
	return ""
}

// typedValue 는 보낼 값을 속성 형식에 맞는 JSON 으로 만듭니다.
// 레지스트리가 있으면 ReadOnly·허용값·형식을 검사하고, 없으면 현재값의 JSON 형식(문자열/숫자/불리언)을 따릅니다.
// 실패하면 (nil, 상태, 사유) 입니다.
func typedValue(val string, reg *attrRegistry, name string, cur json.RawMessage) (json.RawMessage, string, string) {
	str := func() json.RawMessage {
		b, _ := json.Marshal(val)
		return b
	}
	isInt := func() (json.RawMessage, bool) {
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return nil, false
		}
		return json.RawMessage(strconv.FormatInt(n, 10)), true
	}
	if reg != nil {
		ra := reg.lookup(name)
		switch {
		case ra == nil:
			return nil, StatusInvalidValue, "레지스트리에 없는 속성"
		case ra.ReadOnly:
			return nil, StatusSkippedReadOnly, "레지스트리에서 ReadOnly 인 속성"
		}
		switch ra.Type {
		case "Enumeration":
			if !inList(ra.Values, val) {
				return nil, StatusInvalidValue, "값 " + val + " 이(가) 허용값(" + bv(strings.Join(ra.Values, "|")) + ")에 없음"
			}
			return str(), "", ""
		case "Integer":
			raw, ok := isInt()
			if !ok {
				return nil, StatusInvalidValue, "Integer 속성인데 값 " + val + " 이(가) 정수가 아님"
			}
			n, _ := strconv.ParseFloat(string(raw), 64)
			if lo, err := strconv.ParseFloat(ra.Lower, 64); err == nil && n < lo {
				return nil, StatusInvalidValue, "값 " + val + " 이(가) 하한 " + ra.Lower + " 보다 작음"
			}
			if hi, err := strconv.ParseFloat(ra.Upper, 64); err == nil && n > hi {
				return nil, StatusInvalidValue, "값 " + val + " 이(가) 상한 " + ra.Upper + " 보다 큼"
			}
			return raw, "", ""
		case "Boolean":
			if val != "true" && val != "false" {
				return nil, StatusInvalidValue, "Boolean 속성인데 값이 true/false 가 아님"
			}
			return json.RawMessage(val), "", ""
		case "String":
			return str(), "", ""
		}
		return nil, StatusInvalidValue, "설정하지 않는 속성 형식(" + bv(ra.Type) + ")"
	}
	t := bytes.TrimSpace(cur)
	switch {
	case len(t) > 0 && t[0] == '"':
		return str(), "", ""
	case string(t) == "true" || string(t) == "false":
		if val == "true" || val == "false" {
			return json.RawMessage(val), "", ""
		}
	case len(t) > 0 && (t[0] == '-' || (t[0] >= '0' && t[0] <= '9')):
		if raw, ok := isInt(); ok {
			return raw, "", ""
		}
	}
	return nil, StatusInvalidValue, "레지스트리가 없고 현재값 형식으로도 보낼 값의 형식을 정할 수 없음"
}

// inList 는 정확히 같은 값이 있는지 봅니다 (Enumeration 값은 문자열 그대로 보내므로 정수 정규화를 하지 않음).
func inList(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// buildPlan 은 항목들로 보낼 요청을 만듭니다 (items 를 바꾸지 않는 순수 함수, dry-run 과 실제 적용이 공유).
//
// 시스템 프로파일 규칙: 프로파일이 바뀌는 중이면(이번에 보내거나 이미 Pending) 다른 항목이 프로파일에 종속되어
// ReadOnly 가 되거나 덮어써질 수 있으므로, 목표 프로파일이 Custom 이 아니면 프로파일 항목만 보내고 나머지는 미룬다.
// Custom 이어도 레지스트리를 못 읽었으면 보수적으로 똑같이 한다.
func buildPlan(info *SystemInfo, onReset, haveReg bool, pendProf string, items []*setItem) *setPlan {
	if len(items) == 0 {
		return nil
	}
	p := &setPlan{Items: items}
	var prof *setItem
	for _, it := range items {
		if it.Std == profileStd {
			prof = it
		}
	}
	target, src := pendProf, "Pending 으로 재부팅 대기 중인"
	if prof != nil {
		target, src = prof.Value, "이번에 설정할"
	}
	// 프로파일 항목 하나만 보내는 경우는 종속 문제가 없다. 프로파일이 이미 Pending 이면 다른 항목은 모두 미룬다.
	if target != "" && (prof == nil || len(items) > 1) {
		why := ""
		switch {
		case !strings.EqualFold(target, "Custom"):
			why = src + " 시스템 프로파일(" + bv(target) + ")이 먼저 반영되어야 함 (다른 항목이 프로파일에 종속될 수 있음) — 재부팅 후 재점검해 다시 설정"
		case !haveReg:
			why = "레지스트리를 읽지 못해 프로파일(" + bv(target) + ") 종속 여부를 확인할 수 없음 — 프로파일 항목만 먼저, 재부팅 후 재점검해 다시 설정"
		}
		if why != "" {
			p.Items, p.DeferWhy = nil, why
			for _, it := range items {
				if it == prof {
					p.Items = []*setItem{prof}
				} else {
					p.Deferred = append(p.Deferred, it)
				}
			}
		}
	}
	if len(p.Items) == 0 {
		return p
	}
	fail := func(st, why string) *setPlan {
		p.ErrStatus, p.Err = st, why
		return p
	}

	attrs := make(map[string]json.RawMessage, len(p.Items))
	for _, it := range p.Items {
		if _, dup := attrs[it.Attr]; dup {
			return fail(StatusInvalidValue, "같은 속성("+it.Attr+")이 두 항목에 있음 — 프로파일 확인 필요")
		}
		attrs[it.Attr] = it.raw
	}
	body := map[string]interface{}{"Attributes": attrs}
	if onReset {
		body["@Redfish.SettingsApplyTime"] = map[string]string{"ApplyTime": "OnReset"}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fail(StatusInvalidValue, "본문 JSON 변환 실패")
	}
	send, err := checkAllowed(http.MethodPatch, info.SettingsPath, ModeSet, "")
	if err != nil {
		return fail(StatusSetUnsupported, "Settings 경로가 허용목록(Bios/Settings·Bios/Pending) 밖이라 보내지 않음")
	}
	canon, err := checkBody(http.MethodPatch, send, raw)
	if err != nil {
		return fail(StatusInvalidValue, "안전 검사 거부 (보내지 않음): "+err.Error())
	}
	p.Path, p.Body = send, canon

	if info.Vendor == "Dell" {
		if info.ManagerPath == "" {
			return fail(StatusSetUnsupported, "Dell Manager 경로를 몰라 BIOS 설정 Job 을 만들 수 없어 쓰지 않음")
		}
		jsend, err := checkAllowed(http.MethodPost, strings.TrimRight(info.ManagerPath, "/")+"/Jobs", ModeSet, "")
		if err != nil {
			return fail(StatusSetUnsupported, "Dell Jobs 경로가 허용목록 밖이라 쓰지 않음")
		}
		target, _, err := normalizePath(info.SettingsPath)
		if err != nil {
			return fail(StatusSetUnsupported, "Dell Job 의 TargetSettingsURI 를 만들 수 없어 쓰지 않음")
		}
		// TargetSettingsURI 하나만 담는다. ScheduledStartTime·RebootJobType 등 재부팅을 부를 수 있는 키는 넣지 않는다.
		jraw, _ := json.Marshal(map[string]string{"TargetSettingsURI": target})
		jcanon, err := checkBody(http.MethodPost, jsend, jraw)
		if err != nil {
			return fail(StatusSetUnsupported, "Dell Job 본문 검사 거부 (보내지 않음): "+err.Error())
		}
		p.JobPath, p.JobBody = jsend, jcanon
	}
	return p
}

// send 는 계획대로 PATCH 1회(+Dell Job 최대 1회)를 보내고 Settings 를 다시 읽어 확인합니다. 재시도하지 않습니다.
func (e *setEnv) send(h *setHost, c *Client) {
	p := h.Plan
	var before map[string]bool // Dell: PATCH 전 Job 목록 (자동 생성 여부 판단용)
	if p.JobPath != "" {
		if b, err := c.GetRaw(p.JobPath); err == nil {
			before = memberSet(b)
		} else {
			h.Notes = append(h.Notes, "Dell Jobs 기준 조회 실패: "+errBrief(err))
		}
	}
	h.Tried = true
	sentBefore := c.Stats().Patches
	st, b, err := c.Patch(p.Path, json.RawMessage(p.Body))
	h.Sent = c.Stats().Patches > sentBefore // 허용목록·본문 검사·인증 실패 기억으로 막혔으면 실제로는 보내지 않은 것
	if err != nil {
		s, why := writeErrStatus(st, err)
		for _, it := range p.Items {
			it.set(s, why)
		}
		return
	}
	jobErr := ""
	if p.JobPath != "" {
		h.Job, jobErr = dellJob(c, p, st, b, before)
	}
	var pend map[string]string
	sb, gerr := c.GetRaw(p.Path)
	if gerr == nil {
		pend, gerr = parseAttrs(sb)
	}
	suffix := ""
	if h.Job != "" {
		suffix = "; " + h.Job
	}
	if h.RegNote != "" {
		suffix += "; " + h.RegNote + " → 레지스트리 사전검증 없이 보냄"
	}
	for _, it := range p.Items {
		pv, ok := pend[it.Attr]
		switch {
		case jobErr != "":
			it.set(StatusApplyUnconfirmed, fmt.Sprintf("PATCH %d 후 %s", st, jobErr)+suffix)
		case gerr != nil:
			it.set(StatusApplyUnconfirmed, fmt.Sprintf("PATCH %d 후 Settings 재조회 실패: %s", st, errBrief(gerr))+suffix)
		case ok && pv == it.Value:
			it.set(StatusApplied, fmt.Sprintf("PATCH %d, 재조회 Pending=%s 확인", st, bv(pv))+suffix)
		case ok:
			it.set(StatusApplyUnconfirmed, fmt.Sprintf("PATCH %d 후 재조회 Pending=%s (기대 %s)", st, bv(pv), it.Value)+suffix)
		default:
			it.set(StatusApplyUnconfirmed, fmt.Sprintf("PATCH %d 후 재조회한 Settings 에 이 속성의 Pending 이 없음", st)+suffix)
		}
	}
}

// maxJobProbe 는 PATCH 뒤 자동 생성 Job 후보(본문의 Job URI·Jobs 목록의 새 멤버)를 GET 해 볼 최대 개수입니다.
const maxJobProbe = 5

// reJobURI 는 응답 본문 안의 Managers/<id>/Jobs/<id> URI 입니다 (단순 "JID_" 문자열은 근거로 쓰지 않음).
var reJobURI = regexp.MustCompile(`(?i)"(/redfish/v1/Managers/[^"/]+/Jobs/[^"/]+)"`)

// dellJob 은 PATCH 로 BIOS 설정 Job 이 자동 생성되지 않은 것으로 보일 때만 Job 을 1회 만듭니다.
// 자동 생성으로 인정하는 경우 (엄격): PATCH 응답이 202, 또는 응답 본문의 Job URI·PATCH 전후 Jobs 목록의 새 멤버를
// GET 해 보니 JobType=BIOSConfiguration(대소문자 무시)이고 실행 전 상태(Scheduled/New 류)인 Job 이 있을 때.
// 본문에 "JID_" 문자열만 있거나 무관한 새 Job 이 생긴 것은 인정하지 않는다 (GET 은 읽기 전용, 원래 대소문자 ID 사용).
// (Location 헤더는 Client.Patch 가 돌려주지 않아 보지 않는다 — redfish.go 는 수정하지 않음)
// 돌려주는 errText 가 비어 있지 않으면 Job 생성 실패입니다.
func dellJob(c *Client, p *setPlan, st int, body []byte, before map[string]bool) (note, errText string) {
	if st == http.StatusAccepted {
		return "Dell: PATCH 응답 HTTP 202 → BIOS 설정 Job 이 자동 생성된 것으로 보고 Job POST 안 함", ""
	}
	var cands []string
	for _, m := range reJobURI.FindAllSubmatch(body, -1) {
		cands = append(cands, string(m[1]))
	}
	if before != nil {
		if b, err := c.GetRaw(p.JobPath); err == nil {
			if ms, err := membersOf(b); err == nil {
				for _, id := range ms {
					if !before[strings.ToLower(id)] {
						cands = append(cands, id)
					}
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, id := range cands {
		if seen[strings.ToLower(id)] || len(seen) >= maxJobProbe {
			continue
		}
		seen[strings.ToLower(id)] = true
		if ok, _ := isBiosJob(c.GetRaw, p.JobPath, id); ok {
			return "Dell: PATCH 후 예약된 BIOS 설정 Job(" + bv(path.Base(id)) + ") 확인 → Job POST 안 함", ""
		}
	}
	jst, hdr, _, err := c.PostJob(p.JobPath, json.RawMessage(p.JobBody))
	if err != nil {
		s, why := writeErrStatus(jst, err)
		return "", "BIOS 설정 Job 생성 실패 (" + s + ": " + why + ") — Pending 은 썼으나 Job 이 없으면 재부팅해도 반영되지 않을 수 있음"
	}
	loc := ""
	if hdr != nil && hdr.Get("Location") != "" {
		loc = " " + bv(path.Base(hdr.Get("Location")))
	}
	return fmt.Sprintf("Dell: BIOS 설정 Job 생성 (HTTP %d%s)", jst, loc), ""
}

func memberSet(b []byte) map[string]bool {
	ms, err := membersOf(b)
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(ms))
	for _, m := range ms {
		out[strings.ToLower(m)] = true
	}
	return out
}

// writeErrStatus 는 쓰기(PATCH·Job POST) 실패를 설정 결과 상태로 분류합니다. 어떤 경우에도 재시도하지 않습니다.
func writeErrStatus(code int, err error) (string, string) {
	st, _ := errStatus(err)
	switch {
	case errors.Is(err, ErrNotAllowed):
		return StatusSetUnsupported, "허용목록·본문 검사에서 막혀 보내지 않음"
	case code >= 200 && code < 300:
		// BMC 는 2xx 로 받아들였는데 응답 본문을 끝까지 받지 못함(연결 끊김·타임아웃·8MB 초과 등) → 반영됐을 수 있다.
		return StatusSetError, fmt.Sprintf("BMC 가 HTTP %d 로 받아들였으나 응답을 끝까지 받지 못함 (%s) — 반영됐을 수 있음. 재시도하지 않았습니다. bios_check.sh 로 재점검하십시오", code, errBrief(err))
	case st == StatusAuthFail || code == http.StatusUnauthorized || code == http.StatusForbidden:
		if code == 0 { // 앞선 요청의 인증 실패를 기억해 보내지 않은 경우
			return StatusAuthFail, "인증 실패 상태라 보내지 않음 — 계정 잠금 방지를 위해 재시도하지 않음"
		}
		return StatusAuthFail, fmt.Sprintf("인증 실패 (HTTP %d) — 계정 잠금 방지를 위해 재시도하지 않음", code)
	case code == http.StatusBadRequest:
		return StatusRejected, "BMC 가 거부 (HTTP 400): " + bmcMessage(err)
	case code == http.StatusNotFound || code == http.StatusMethodNotAllowed || code == http.StatusNotImplemented:
		return StatusSetUnsupported, fmt.Sprintf("BMC 가 이 요청을 지원하지 않음 (HTTP %d)", code)
	case code == 0 || code >= 500:
		return StatusSetError, "쓰기 결과를 알 수 없음 (" + errBrief(err) + ") — 재시도하지 않았습니다. bios_check.sh 로 재점검하십시오"
	}
	return StatusRejected, fmt.Sprintf("BMC 가 거부 (HTTP %d): %s", code, bmcMessage(err))
}

var reBMCMessage = regexp.MustCompile(`"[Mm]essage"\s*:\s*"((?:[^"\\]|\\.)*)"`)

// bmcMessage 는 오류 응답의 Message 들을 뽑습니다. RFError.Detail(비밀번호·토큰을 먼저 지우고 제어문자를 정리한 본문)만 쓰며,
// JSON 이스케이프(\uXXXX 등)는 풀지 않습니다: 풀면 일부만 이스케이프되어 마스킹을 피한 비밀번호가 평문으로 되살아날 수 있음.
func bmcMessage(err error) string {
	var rf *RFError
	if !errors.As(err, &rf) {
		return errBrief(err)
	}
	var msgs []string
	seen := map[string]bool{}
	for _, m := range reBMCMessage.FindAllStringSubmatch(rf.Detail, -1) {
		if s := sanitizeDetail(m[1]); s != "" && !seen[s] {
			seen[s] = true
			msgs = append(msgs, s)
		}
	}
	if len(msgs) == 0 {
		return rf.Detail
	}
	return clip(strings.Join(msgs, " / "), detailLimit)
}

// ---- 출력 ----

// writePlan 은 보낼 요청 1건(PATCH + Dell Job)을 출력합니다. dry-run 출력과 실제 적용 기록(apply_requests.txt)이
// 같은 함수를 쓰며, 본문은 실제로 전송되는 바이트 그대로입니다.
func writePlan(w io.Writer, p *setPlan) {
	fmt.Fprintf(w, "  PATCH %s\n", p.Path)
	fmt.Fprintf(w, "  본문  %s\n", p.Body)
	if p.JobPath != "" {
		fmt.Fprintf(w, "  Job   POST %s %s\n", p.JobPath, p.JobBody)
		fmt.Fprintln(w, "        (Dell: PATCH 후 자동 생성된 BIOS 설정 Job 이 보이지 않을 때만, 최대 1회. 재부팅 필드 없음)")
	} else {
		fmt.Fprintln(w, "  Job   없음")
	}
}

func hostHeader(h *setHost) string {
	label := h.Target.Hostname
	if h.Target.IP != "" && h.Target.IP != h.Target.Hostname {
		label += " (" + h.Target.IP + ")"
	}
	return label + "  " + sanitizeDetail(modelLabel(h.Info))
}

// writeDryRun 은 dry-run 결과를 출력합니다.
func writeDryRun(w io.Writer, hosts []*setHost, fromDump bool) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "== dry-run: 보낼 요청 (BMC 에 아무것도 쓰지 않음) ==")
	if fromDump {
		fmt.Fprintln(w, "(덤프 읽기 모드: 덤프 시점의 상태로 계산했습니다)")
	}
	var nReal, nItems, nTrial int
	for _, h := range hosts {
		fmt.Fprintf(w, "\n%s\n", hostHeader(h))
		if h.Plan.ready() {
			nReal++
			nItems += len(h.Plan.Items)
			fmt.Fprintln(w, "  [Y 응답 시 보낼 요청]")
			writePlan(w, h.Plan)
		}
		if h.Trial.ready() {
			nTrial++
			fmt.Fprintln(w, "  [미검증 — 실제 설정 불가, 시험 출력] verified=N 항목을 포함한 본문입니다. 사람이 확인하는 데만 쓰십시오")
			writePlan(w, h.Trial)
		}
		if h.RegNote != "" && (h.Plan.ready() || h.Trial.ready()) {
			fmt.Fprintf(w, "  참고: %s → 레지스트리 사전검증 없이 계산했습니다\n", h.RegNote)
		}
		for _, it := range h.Items {
			if it.Status != "" {
				fmt.Fprintf(w, "  제외  %-16s %s  %s (%s)\n", it.Std, it.Attr, it.Status, it.Detail)
			}
		}
		if t := h.Trial; t != nil {
			for _, it := range t.Deferred {
				if it.Unverified {
					fmt.Fprintf(w, "  제외  %-16s %s  %s (시험, %s)\n", it.Std, it.Attr, StatusDependsOnProfile, t.DeferWhy)
				}
			}
			if t.Err != "" {
				fmt.Fprintf(w, "  시험 본문 불가: %s (%s)\n", t.ErrStatus, t.Err)
			}
		}
	}
	fmt.Fprintf(w, "\ndry-run 요약: Y 응답 시 보낼 호스트 %s대(항목 %s건), 미검증 시험 출력 %s대.\n",
		commaInt(nReal), commaInt(nItems), commaInt(nTrial))
	fmt.Fprintln(w, "dry-run 이므로 BMC 에 아무것도 쓰지 않았고 설정 여부(Y/N)를 묻지 않습니다.")
}

var applyHeader = []string{"host", "ip", "vendor", "model", "std_name", "attribute", "expected", "status", "detail"}

// applyFailedStatus 는 쓰기가 실패했거나 결과를 모르거나 접속하지 못한 상태입니다 (apply_failed.txt 대상).
var applyFailedStatus = map[string]bool{
	StatusApplyUnconfirmed: true, StatusSetError: true, StatusRejected: true, StatusSetUnsupported: true,
	StatusAuthFail: true, StatusSkippedAuthStop: true, StatusUnreachable: true, StatusTimeout: true,
	StatusBMCError: true, StatusHTTPError: true, StatusUnsupported: true,
}

// writeApplyFiles 는 apply_result.tsv, applied.txt, (있으면) apply_failed.txt, apply_requests.txt 를 씁니다 (모두 0600).
func writeApplyFiles(dir string, hosts []*setHost) error {
	var rows [][]string
	var applied, failed []string
	var req bytes.Buffer
	for _, h := range hosts {
		vendor, model := "-", "-"
		if h.Info != nil {
			vendor, model = tsvField(h.Info.Vendor), tsvField(h.Info.Model)
		}
		isApplied, isFailed := false, false
		for _, it := range h.Items {
			rows = append(rows, []string{tsvField(h.Target.Hostname), tsvField(h.Target.IP), vendor, model,
				tsvField(it.Std), tsvField(it.Attr), tsvField(it.Expected), tsvField(it.Status), tsvField(it.Detail)})
			isApplied = isApplied || it.Status == StatusApplied
			isFailed = isFailed || applyFailedStatus[it.Status]
		}
		if isApplied {
			applied = append(applied, h.Target.Hostname)
		}
		if isFailed {
			failed = append(failed, h.Target.Input)
		}
		if h.Tried {
			fmt.Fprintf(&req, "%s  Sent=%t\n", hostHeader(h), h.Sent)
			writePlan(&req, h.Plan)
			for _, it := range h.Plan.Items {
				fmt.Fprintf(&req, "  결과  %s %s (%s)\n", it.Attr, it.Status, it.Detail)
			}
			req.WriteByte('\n')
		}
	}
	if err := writeTSV(filepath.Join(dir, "apply_result.tsv"), applyHeader, rows); err != nil {
		return err
	}
	if err := writeLines(filepath.Join(dir, "applied.txt"), applied); err != nil {
		return err
	}
	if len(failed) > 0 {
		if err := writeLines(filepath.Join(dir, "apply_failed.txt"), failed); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "apply_requests.txt"), req.Bytes(), 0o600)
}

// applyOrder 는 설정 결과 집계의 표시 순서입니다 (여기에 없는 상태는 뒤에 이름순).
var applyOrder = []string{
	StatusApplied, StatusApplyUnconfirmed, StatusSetError, StatusRejected, StatusSetUnsupported, StatusAuthFail,
	StatusSkippedAuthStop, StatusDependsOnProfile, StatusAlreadyOK, StatusPendingExists, StatusChanged,
	StatusSkippedReadOnly, StatusInvalidValue, StatusSetNotSupportedVendor, StatusUnverified, StatusDuplicateTarget,
}

// applyGuide 는 상태별 안내문입니다.
var applyGuide = map[string]string{
	StatusSetError:         "쓰기 결과를 알 수 없습니다 (비멱등 호출이라 재시도하지 않음). 해당 호스트를 bios_check.sh 로 재점검한 뒤 필요하면 다시 설정하십시오.",
	StatusApplyUnconfirmed: "BMC 는 요청을 받았으나 Pending 반영을 확인하지 못했습니다. 재점검하거나 BMC 화면에서 직접 확인하십시오. Dell 은 iDRAC Job Queue 에 BIOS 설정 Job 이 없으면 반영되지 않을 수 있음 — 확인 필요 (재점검에서 PENDING_NO_JOB 이면 다시 Y 로 Job 을 만들 수 있음).",
	StatusAuthFail:         "계정 잠금을 막으려고 재시도하지 않았습니다. 계정/비밀번호를 확인하십시오.",
	StatusSkippedAuthStop:  "AUTH_FAIL 차단기가 작동해 접속하지 않았습니다. 계정/비밀번호 확인 후 apply_failed.txt 로 다시 점검·설정하십시오.",
	StatusDependsOnProfile: "시스템 프로파일이 먼저 반영되어야 합니다. 재부팅 후 bios_check.sh 로 재점검해 다시 설정하십시오.",
	StatusPendingExists:    "점검 이후 남의 Pending 이 생겨 쓰지 않았습니다 (남의 Pending/Job 은 건드리지 않음).",
	StatusChanged:          "점검 이후 호스트가 바뀌어 쓰지 않았습니다. 다시 점검하십시오.",
}

// writeApplyReport 는 설정 결과를 터미널에 출력합니다.
func writeApplyReport(w io.Writer, dir string, hosts []*setHost, o reportOpts) {
	p := painter{o.Color}
	items := map[string]int{}
	hostsBy := map[string]int{}
	var applied []string
	total := 0
	for _, h := range hosts {
		seen := map[string]bool{}
		for _, it := range h.Items {
			total++
			items[it.Status]++
			if !seen[it.Status] {
				seen[it.Status] = true
				hostsBy[it.Status]++
			}
		}
		if seen[StatusApplied] {
			applied = append(applied, h.Target.Hostname)
		}
	}
	order := append([]string(nil), applyOrder...)
	known := map[string]bool{}
	for _, s := range applyOrder {
		known[s] = true
	}
	var rest []string
	for s := range items {
		if !known[s] {
			rest = append(rest, s)
		}
	}
	sort.Strings(rest)
	order = append(order, rest...)

	fmt.Fprintf(w, "\n== BIOS Pending 설정 결과 (호스트 %s대, 항목 %s건) ==\n", commaInt(len(hosts)), commaInt(total))
	for _, st := range order {
		if items[st] == 0 {
			continue
		}
		line := fmt.Sprintf("%-26s %6s건 (호스트 %s대)", st, commaInt(items[st]), commaInt(hostsBy[st]))
		switch {
		case st == StatusApplied:
			line = p.green(line)
		case applyFailedStatus[st]:
			line = p.red(line)
		default:
			line = p.yellow(line)
		}
		fmt.Fprintln(w, line)
	}
	if warn := authStopWarning(hostsBy[StatusAuthFail], hostsBy[StatusSkippedAuthStop], "apply_failed.txt 로 다시 점검"); warn != "" {
		fmt.Fprintln(w, p.boldRed("!! "+warn))
	}

	appliedPath := filepath.ToSlash(filepath.Join(dir, "applied.txt"))
	fmt.Fprintf(w, "\n%s %s대 → %s\n", p.green("[적용된 호스트 (Pending 설정 확인)]"), commaInt(len(applied)), appliedPath)
	if len(applied) > 0 {
		var lines []string
		if len(applied) <= o.ListMax {
			lines = wrapNames(applied, "  ", reportWidth)
		}
		if lines == nil || len(lines) >= okListMaxLines {
			fmt.Fprintf(w, "  호스트가 많아 이름은 생략합니다 → %s\n", appliedPath)
		} else {
			for _, l := range lines {
				fmt.Fprintln(w, l)
			}
		}
	}

	shown, cut := 0, 0
	header := false
	for _, st := range order {
		if st == StatusApplied || items[st] == 0 {
			continue
		}
		if !header {
			fmt.Fprintf(w, "\n%s\n", p.yellow("[적용 안 됨 · 사유별]"))
			header = true
		}
		fmt.Fprintf(w, "  %s %s건\n", st, commaInt(items[st]))
		if g := applyGuide[st]; g != "" {
			fmt.Fprintf(w, "    %s\n", g)
		}
		for _, h := range hosts {
			for _, it := range h.Items {
				if it.Status != st {
					continue
				}
				if shown >= o.FailMax {
					cut++
					continue
				}
				shown++
				fmt.Fprintf(w, "    %s  %-16s %s  (%s)\n", h.Target.Hostname, it.Std, it.Attr, it.Detail)
			}
		}
	}
	if cut > 0 {
		fmt.Fprintf(w, "    ... 외 %d건 → %s\n", cut, filepath.ToSlash(filepath.Join(dir, "apply_result.tsv")))
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s 설정 결과 파일: %s/  (apply_result.tsv 항목별 결과, applied.txt 적용 호스트, apply_failed.txt 실패·미확인 대상(있을 때), apply_requests.txt 보낸 요청)\n",
		p.cyan("*"), filepath.ToSlash(dir))
	fmt.Fprintln(w, p.boldRed(applyNotice))
}
