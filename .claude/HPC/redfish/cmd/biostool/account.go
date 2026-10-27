package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// account.go 는 BMC 계정 생성(create)·비밀번호 변경(change)입니다 (확정 사양: ACCOUNT_SPEC.md).
//
//   - create: 1순위 계정(bios.conf user + pass.enc)이 BMC 에 없을 때, rf.conf 의 벤더 admin(2순위) 계정으로
//     로그인해 같은 id/pw 의 관리자 계정을 만든다. 같은 id 가 이미 있으면 덮어쓰지 않고 건너뛴다.
//     만든 뒤 새 계정으로 1회 로그인해 검증하고, 실패한 서버만 이번에 만든 계정을 되돌린다(롤백).
//   - change: 1순위 계정의 비밀번호를 새 비밀번호로 바꾼다 (새 비밀번호는 터미널에서 직접 2번 입력).
//     바꾼 뒤 새 비밀번호로 로그인 검증, 실패한 서버만 이전 비밀번호로 되돌린다. pass.enc 는 자동 갱신하지 않는다.
//   - 두 번째 공용 계정(bios.conf user2 + pass2.enc, 권한 user2_role)이 있으면 create 가 같은 호스트에 그 계정도 만든다
//     (2순위 로그인 1회로 두 계정을 차례로 만들고 각각 검증·롤백). change 는 1순위 계정만 바꾼다.
//   - 비밀번호는 어떤 출력·결과 파일·오류 메시지에도 싣지 않는다. 쓰기는 account*.go 만 하며
//     redfish.go 의 ModeAccount 허용목록(계정 경로·고정 본문 형태)과 ucsmxml 계정 모드를 지난다.

// 계정 작업 결과 상태 (account_result.tsv 의 result 열). 호스트 오류는 AUTH_FAIL/UNREACHABLE/TIMEOUT 등 기존 상태를 쓴다.
const (
	acctCreated        = "CREATED"                 // 만들고 새 계정 로그인 검증까지 통과
	acctWouldCreate    = "WOULD_CREATE"            // dry-run: 만들 계획 (전송 안 함)
	acctNeedCreate     = "NEED_CREATE"             // 점검: 2순위 계정으로만 로그인됨 (생성 대상)
	acctPrimaryOK      = "PRIMARY_OK"              // 1순위 계정으로 이미 로그인됨 (만들 필요 없음)
	acctPresent        = "PRESENT"                 // 두 번째 계정(user2)이 BMC 계정 목록에 이미 있음 (만들 필요 없음)
	acctExists         = "SKIP_EXISTS"             // 같은 id 가 이미 있어 만들지 않음
	acctNoAdmin        = "NO_ADMIN"                // rf.conf 에 이 벤더의 admin 계정이 없거나 벤더를 식별 못 함
	acctNoSlot         = "NO_FREE_SLOT"            // 빈 슬롯 없음
	acctRejected       = "REJECTED"                // BMC 가 요청을 거부
	acctVerifyFailRB   = "VERIFY_FAIL_ROLLED_BACK" // 검증 로그인 실패 → 롤백 완료
	acctRollbackFailed = "ROLLBACK_FAILED"         // 검증 실패 + 롤백도 실패 (수동 조치 필요)
	acctVerifyUnknown  = "VERIFY_UNKNOWN"          // 통신 오류로 검증하지 못함 (롤백하지 않음, 수동 확인)
	acctChanged        = "CHANGED"                 // 비밀번호 변경 + 새 비밀번호 로그인 검증 통과
	acctWouldChange    = "WOULD_CHANGE"            // dry-run
	acctNoAccount      = "NO_ACCOUNT"              // 변경할 id 가 BMC 에 없음 (create 필요)
	acctSkippedWarmup  = "SKIPPED_WARMUP_FAIL"     // 웜업에서 실패가 많아 건너뜀
)

const (
	acctDefaultWorkers = 100
	acctMaxWorkers     = 300
	acctWarmup         = 10 // 처음 10대는 한 대씩 처리 (이상 징후 확인)
	acctWarmFailMin    = 3  // 웜업 표본이 이만큼 이상이고 절반 이상 실패면 중단
	acctMaxMembers     = 64 // 계정 컬렉션 멤버 조회 상한
)

// acctHost 는 호스트 1대의 계정 작업 결과입니다.
type acctHost struct {
	Target   Target
	Vendor   string
	Model    string
	Op       string // create / change
	Source   string // 점검에서 로그인에 성공한 계정 출처 (credPrimary / credFallback)
	Status   string // 결과 상태 또는 호스트 오류 상태
	Method   string // 사용한 방식 (예: "POST /redfish/v1/AccountService/Accounts", "PATCH slot 5")
	Rollback string // "-" | "완료" | "실패"
	Detail   string
	Warn     string // 실장비 미확인 방식 경고
	User     string // 작업한 계정 ID (account_result.tsv 의 account 열)
	Stats    CallStats

	spec          *acctSpec // create: 만들 계정 (nil 이면 1순위 계정)
	createdMember string    // POST 로 만든 멤버 경로 (롤백용)
	slotPath      string    // 슬롯 PATCH 로 만든 슬롯 경로 (롤백용)
}

func newAcctHost(t Target, op string) *acctHost { return &acctHost{Target: t, Op: op, Rollback: "-"} }

// acctSpec 은 만들 공용 계정 1개입니다: 1순위(bios.conf user + pass.enc, 관리자) 또는
// 두 번째(user2 + pass2.enc, 권한 user2_role). Pass 는 메모리에서만 쓴다.
type acctSpec struct {
	User, Pass string
	Role       string // roleAdministrator / roleOperator / roleReadOnly
	Second     bool   // 두 번째 계정 (user2)
}

// newCreateHosts 는 호스트 t 에 만들 계정마다 결과 칸을 만듭니다 (1순위 계정이 첫 칸).
func newCreateHosts(t Target, specs []acctSpec) []*acctHost {
	hs := make([]*acctHost, len(specs))
	for i := range specs {
		hs[i] = newAcctHost(t, "create")
		hs[i].spec, hs[i].User = &specs[i], specs[i].User
	}
	return hs
}

// name 은 요약·목록에 보일 이름입니다 (두 번째 계정 칸이면 "호스트(계정)").
func (h *acctHost) name() string {
	n := sanitizeDetail(h.Target.Hostname)
	if h.spec != nil && h.spec.Second {
		n += "(" + sanitizeDetail(h.spec.User) + ")"
	}
	return n
}

// setErr 는 호스트 오류(로그인·조회 실패)를 기록합니다.
func (h *acctHost) setErr(err error) {
	h.Status, _ = errStatus(err)
	h.Detail = errBrief(err)
	var rf *RFError
	if errors.As(err, &rf) && rf.Detail != "" {
		h.Detail = rf.Detail
	}
}

// acctEnv 는 계정 작업 실행 환경입니다. 비밀번호는 메모리에서만 쓴다.
type acctEnv struct {
	conf     *Config
	password string // 1순위 비밀번호 (create: 만들 계정의 비밀번호, change: 이전 비밀번호)
	newPass  string // change 의 새 비밀번호
	fb       *fallbackCreds
	dryRun   bool
	gap      time.Duration // 0 이면 Client 기본값 (테스트에서 -1)
	specs    []acctSpec    // create 로 만들 계정 (비면 1순위 계정 하나, newAcctEnv 가 채움)
	// admin 이 있으면 create 가 rf.conf 대신 이 계정으로 로그인합니다 (점검 끝: 1순위 계정 또는 운영자가 직접 입력한 관리자 계정).
	admin *fallbackAcct
}

// newAcctEnv 는 계정 작업 환경을 만듭니다. bios.conf 에 user2 가 있으면 pass2_file 을 key_file 로 복호화해
// 두 번째 계정으로 넣습니다 (없거나 복호화하지 못하면 오류 — 만들 계정의 비밀번호를 모르는 채로 진행하지 않음).
func newAcctEnv(rc *runContext, dryRun bool) (*acctEnv, error) {
	e := &acctEnv{conf: rc.Conf, password: rc.Password, fb: rc.Fallback, dryRun: dryRun}
	e.specs = []acctSpec{{User: rc.Conf.User, Pass: rc.Password, Role: roleAdministrator}}
	if rc.Conf.User2 != "" {
		p2, err := readPassword(rc.Conf.Pass2File, rc.Conf.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("두 번째 계정(user2=%s) 비밀번호를 읽지 못함 — bash encrypt.sh 2 로 %s 를 만드십시오: %w", rc.Conf.User2, rc.Conf.Pass2File, err)
		}
		e.specs = append(e.specs, acctSpec{User: rc.Conf.User2, Pass: p2, Role: rc.Conf.User2Role, Second: true})
	}
	return e, nil
}

// accounts 는 만들 계정 목록입니다 (specs 가 비면 1순위 계정 하나, 관리자).
func (e *acctEnv) accounts() []acctSpec {
	if len(e.specs) > 0 {
		return e.specs
	}
	return []acctSpec{{User: e.conf.User, Pass: e.password, Role: roleAdministrator}}
}

// specOf 는 결과 칸 h 가 만들 계정입니다.
func (e *acctEnv) specOf(h *acctHost) acctSpec {
	if h.spec != nil {
		return *h.spec
	}
	return e.accounts()[0]
}

func (e *acctEnv) opts(t Target, user, pass string, mode Mode, retries int) ClientOpts {
	return ClientOpts{
		BaseURL: t.BaseURL(), User: user, Pass: pass, Insecure: e.conf.Insecure,
		Timeout: e.conf.Timeout, Retries: retries, Mode: mode, Gap: e.gap,
	}
}

// ---- 계정 컬렉션 조회 (읽기) ----

// acctMember 는 BMC 계정 컬렉션의 멤버 1개입니다.
type acctMember struct {
	Path     string // 멤버의 @odata.id
	ID       string
	UserName string
	Enabled  bool
	RoleID   string
	Unknown  bool // 멤버를 읽지 못함 (빈 슬롯으로 취급하지 않음)
}

// isEmptySlot 은 쓸 수 있는 빈 슬롯인지 봅니다: 이름이 비어 있고 비활성이며 숫자 Id 이고
// Dell 은 Id 3 이상 (1=익명 IPMI, 2=root).
func (m acctMember) isEmptySlot(vendor string) bool {
	if m.Unknown || m.UserName != "" || m.Enabled {
		return false
	}
	n, err := strconv.Atoi(m.ID)
	if err != nil || n < 2 { // Id 1 은 익명/예약 슬롯인 BMC 가 있어 쓰지 않는다
		return false
	}
	if strings.EqualFold(vendor, "Dell") && n < 3 {
		return false
	}
	return true
}

// accountsCollection 은 AccountService 가 알려 주는 계정 컬렉션 경로입니다 (읽지 못하면 기본값).
func accountsCollection(c *Client) string {
	const def = "/redfish/v1/AccountService/Accounts"
	var svc struct{ Accounts odataLink }
	if err := c.GetJSON("/redfish/v1/AccountService", &svc); err != nil {
		return def
	}
	if p, _, err := normalizePath(svc.Accounts.ID); err == nil && reAcctColl.MatchString(strings.ToLower(p)) {
		return p
	}
	return def
}

// listAccounts 는 계정 컬렉션의 멤버를 읽습니다 (GET 뿐, 상한 acctMaxMembers).
func listAccounts(c *Client, coll string) ([]acctMember, error) {
	var col struct {
		Members []odataLink `json:"Members"`
	}
	if err := c.GetJSON(coll, &col); err != nil {
		return nil, err
	}
	var out []acctMember
	for i, ml := range col.Members {
		if i >= acctMaxMembers {
			break
		}
		p, _, err := normalizePath(ml.ID)
		if err != nil || !reAcctMember.MatchString(strings.ToLower(p)) {
			continue
		}
		var m struct {
			ID       string
			UserName string
			Enabled  *bool
			RoleID   string `json:"RoleId"`
		}
		am := acctMember{Path: p, ID: p[strings.LastIndex(p, "/")+1:]}
		if err := c.GetJSON(p, &m); err != nil {
			am.Unknown = true
			out = append(out, am)
			continue
		}
		if m.ID != "" {
			am.ID = m.ID
		}
		am.UserName, am.RoleID = m.UserName, m.RoleID
		am.Enabled = m.Enabled != nil && *m.Enabled
		out = append(out, am)
	}
	return out, nil
}

// findAccountExact 는 이름이 정확히 같은 멤버만 찾습니다 (change 용: 대소문자만 다른 남의 계정을 건드리지 않음).
func findAccountExact(ms []acctMember, name string) *acctMember {
	for i := range ms {
		if !ms[i].Unknown && ms[i].UserName != "" && ms[i].UserName == name {
			return &ms[i]
		}
	}
	return nil
}

func findAccount(ms []acctMember, name string) *acctMember {
	for i := range ms {
		if !ms[i].Unknown && ms[i].UserName != "" && strings.EqualFold(ms[i].UserName, name) {
			return &ms[i]
		}
	}
	return nil
}

// pickRole 은 BMC 의 Roles 컬렉션에서 권한 role 에 맞는 RoleId 를 고릅니다 (acctRoleIDs 후보 순서,
// 관리자면 Administrator → Admin → admin). 읽지 못하거나 맞는 것이 없으면 "" 이며 호출자가 후보를 순서대로 시도합니다.
func pickRole(c *Client, role string) string {
	var col struct {
		Members []odataLink `json:"Members"`
	}
	if err := c.GetJSON("/redfish/v1/AccountService/Roles", &col); err != nil {
		return ""
	}
	have := map[string]bool{}
	for _, m := range col.Members {
		if i := strings.LastIndex(strings.TrimRight(m.ID, "/"), "/"); i >= 0 {
			have[strings.TrimRight(m.ID, "/")[i+1:]] = true
		}
	}
	for _, r := range acctRoleIDs[role] {
		if have[r] {
			return r
		}
	}
	return ""
}

// lightInfo 는 벤더·모델만 가볍게 읽습니다 (ServiceRoot 와 Systems 첫 멤버, GET 2~3회).
func lightInfo(c *Client) (vendor, model string) {
	b, err := c.GetRaw(rootPath)
	if err != nil {
		return "", ""
	}
	vendor = vendorFromServiceRoot(b)
	var root struct{ Systems odataLink }
	_ = json.Unmarshal(b, &root)
	sys := orDefault(root.Systems.ID, defaultSystemsPath)
	var col struct {
		Members []odataLink `json:"Members"`
	}
	if c.GetJSON(sys, &col) == nil && len(col.Members) > 0 {
		var s struct{ Manufacturer, Model string }
		if c.GetJSON(col.Members[0].ID, &s) == nil {
			model = s.Model
			if v := fallbackVendorKey(s.Manufacturer); v != "" {
				vendor = v
			}
		}
	}
	return vendor, model
}

// ---- 관리자(2순위) 계정으로 계정 모드 로그인 ----

// openAdmin 은 대상 BMC 의 벤더를 비인증 ServiceRoot 로 식별하고 rf.conf 의 그 벤더 admin 계정으로
// ModeAccount Client 를 로그인해 돌려줍니다 (로그인 1회, 재시도 없음). 실패해도 Client 는 nil 이 아닙니다.
func (e *acctEnv) openAdmin(t Target) (*Client, string, error) {
	if e.admin != nil { // 지정한 계정: 벤더와 무관하게 그 계정으로 로그인 1회 (벤더는 로그인 뒤 lightInfo 로)
		c := NewClient(e.opts(t, e.admin.user, e.admin.pass, ModeAccount, 0))
		return c, "", c.Login()
	}
	probe := NewClient(e.opts(t, "", "", ModeReadOnly, 0))
	b, err := probe.ServiceRootNoAuth()
	_ = probe.Close()
	if err != nil {
		return NewClient(e.opts(t, "", "", ModeAccount, 0)), "", err
	}
	vendor := vendorFromServiceRoot(b)
	if vendor == "" {
		return NewClient(e.opts(t, "", "", ModeAccount, 0)), "", &RFError{Status: StatusAuthFail, Detail: "BMC 벤더를 식별하지 못해 admin 계정을 고르지 않음"}
	}
	user, pass, ok := e.fb.lookup(vendor)
	if !ok {
		return NewClient(e.opts(t, "", "", ModeAccount, 0)), vendor, &RFError{Status: StatusAuthFail, Detail: "rf.conf 에 " + vendorLabel(vendor) + " admin 계정 없음"}
	}
	c := NewClient(e.opts(t, user, pass, ModeAccount, 0))
	return c, vendor, c.Login()
}

// verifyLogin 은 계정 user/pass 로 1회 로그인해 봅니다 (읽기 전용 Client, 재시도 없음).
func (e *acctEnv) verifyLogin(t Target, user, pass string) error {
	c := NewClient(e.opts(t, user, pass, ModeReadOnly, 0))
	err := c.Login()
	_ = c.Close()
	return err
}

// ---- 점검 (create 의 1단계) ----

// probeCreate 는 1순위 계정 칸만 돌려줍니다 (probeCreateAll 참고).
func (e *acctEnv) probeCreate(t Target) *acctHost { return e.probeCreateAll(t)[0] }

// probeCreateAll 은 1순위 계정으로 로그인해 보고 (실패하면 2순위로) 어느 쪽으로 붙는지 기록합니다. 쓰기 없음.
// 두 번째 계정이 있으면 같은 세션에서 계정 목록을 읽어 그 이름이 있는지(PRESENT) 없는지(NEED_CREATE) 봅니다
// (두 번째 계정으로 로그인해 보지 않음 — 인증 실패 기록을 남기지 않기 위함). 목록을 못 읽으면 생성 단계에서 다시 확인합니다.
func (e *acctEnv) probeCreateAll(t Target) []*acctHost {
	hs := newCreateHosts(t, e.accounts())
	h := hs[0]
	if t.Err != "" {
		for _, x := range hs {
			x.Status = t.Err
		}
		return hs
	}
	c, li, err := loginWithFallback(e.opts(t, e.conf.User, e.password, ModeReadOnly, e.conf.Retries), e.fb)
	defer func() {
		_ = c.Close()
		h.Stats = li.stats(c)
	}()
	if err != nil {
		for _, x := range hs {
			x.setErr(err)
		}
		return hs
	}
	h.Source = li.Source
	h.Vendor, h.Model = lightInfo(c)
	h.Status = acctNeedCreate
	if li.Source == credPrimary {
		h.Status = acctPrimaryOK
	}
	if len(hs) > 1 {
		members, lerr := listAccounts(c, accountsCollection(c))
		for _, x := range hs[1:] {
			x.Source, x.Vendor, x.Model = li.Source, h.Vendor, h.Model
			switch {
			case lerr != nil:
				x.Status, x.Detail = acctNeedCreate, "계정 목록을 읽지 못함 — 생성 단계에서 다시 확인"
			case findAccount(members, x.User) != nil:
				x.Status = acctPresent
			default:
				x.Status = acctNeedCreate
			}
		}
	}
	return hs
}

// ---- 생성 (create 의 2단계) ----

// createOnHost 는 계정 칸 하나를 만듭니다 (createGroup 참고).
func (e *acctEnv) createOnHost(h *acctHost) { e.createGroup([]*acctHost{h}) }

// createGroup 은 같은 호스트(hs 는 모두 같은 Target)에 만들 계정들을 2순위 계정 로그인 1회로 차례로 만들고
// 각각 새 계정 로그인으로 검증·롤백합니다 (dry-run 이면 계획만). 호출 수는 첫 칸에 모읍니다.
func (e *acctEnv) createGroup(hs []*acctHost) {
	h0 := hs[0]
	t := h0.Target
	c, vendor, err := e.openAdmin(t)
	defer func() {
		_ = c.Close()
		st := c.Stats()
		h0.Stats.Gets += st.Gets
		h0.Stats.Posts += st.Posts
		h0.Stats.Patches += st.Patches
		h0.Stats.Deletes += st.Deletes
	}()
	if err != nil {
		for _, h := range hs {
			if e.admin == nil && (vendor == "" || strings.Contains(err.Error(), "admin 계정 없음")) {
				h.Status, h.Detail = acctNoAdmin, errDetail(err)
			} else {
				h.setErr(err)
			}
		}
		return
	}
	for _, s := range e.accounts() {
		c.AddSecret(s.Pass)
	}
	if h0.Vendor == "" || h0.Model == "" {
		v, m := lightInfo(c)
		if h0.Vendor == "" {
			h0.Vendor = v
		}
		if h0.Model == "" {
			h0.Model = m
		}
	}
	if vendor == "" {
		vendor = fallbackVendorKey(h0.Vendor)
	}
	coll := accountsCollection(c)
	for _, h := range hs {
		if h.Vendor == "" {
			h.Vendor = h0.Vendor
		}
		if h.Model == "" {
			h.Model = h0.Model
		}
		e.createOne(c, h, vendor, coll)
	}
}

// createOne 은 열린 admin 세션 c 로 계정 칸 h 의 계정을 만들고 검증·롤백합니다.
// 계정 목록은 매번 다시 읽는다 (앞 계정이 빈 슬롯을 썼을 수 있음).
func (e *acctEnv) createOne(c *Client, h *acctHost, vendor, coll string) {
	t := h.Target
	sp := e.specOf(h)
	h.User = sp.User
	members, err := listAccounts(c, coll)
	if err != nil {
		h.setErr(err)
		return
	}
	if m := findAccount(members, sp.User); m != nil {
		h.Status = acctExists
		h.Detail = fmt.Sprintf("id 가 이미 있음 (slot %s, enabled=%t, role=%s) — 덮어쓰지 않음. 비밀번호가 다르면 change 또는 BMC 에서 확인", m.ID, m.Enabled, orDefault(m.RoleID, "-"))
		return
	}
	var slots []acctMember
	for _, m := range members {
		if m.isEmptySlot(vendor) {
			slots = append(slots, m)
		}
	}
	role := pickRole(c, sp.Role)

	plan := e.planCreate(sp, vendor, coll, slots, role)
	h.Method, h.Warn = plan.method, plan.warn
	if e.dryRun {
		h.Status = acctWouldCreate
		h.Detail = plan.dryText
		return
	}
	created, errText := e.runCreate(c, h, plan)
	if !created {
		if h.Status == "" {
			h.Status = acctRejected
			if len(slots) == 0 && (strings.Contains(errText, "HTTP 405") || strings.Contains(errText, "HTTP 501")) {
				h.Status = acctNoSlot
				errText += " — 빈 슬롯도 없음"
			}
		}
		h.Detail = errText
		return
	}

	// 검증: 새 계정으로 1회 로그인
	verr := e.verifyLogin(t, sp.User, sp.Pass)
	if verr == nil {
		h.Status = acctCreated
		return
	}
	st, _ := errStatus(verr)
	if st != StatusAuthFail {
		h.Status = acctVerifyUnknown
		h.Detail = "계정은 만들었으나 검증 로그인을 통신 오류로 하지 못함 (" + errBrief(verr) + ") — 롤백하지 않음, 수동 확인"
		return
	}
	// 검증 로그인이 인증 거부 → 이번에 만든 계정을 되돌린다.
	if e.rollbackCreate(c, h, coll, vendor) {
		h.Status, h.Rollback = acctVerifyFailRB, "완료"
		h.Detail = "새 계정 로그인 검증 실패(" + errBrief(verr) + ") — 만든 계정을 되돌림"
	} else {
		h.Status, h.Rollback = acctRollbackFailed, "실패"
		h.Detail = "새 계정 로그인 검증 실패 + 롤백 실패 — BMC 에서 계정 " + sp.User + " 를 수동 확인/삭제하십시오"
	}
}

func errDetail(err error) string {
	var rf *RFError
	if errors.As(err, &rf) && rf.Detail != "" {
		return rf.Detail
	}
	return errBrief(err)
}

// createStep 은 계정 생성 시도 1회(POST 본문 또는 슬롯 PATCH)입니다.
type createStep struct {
	kind   string // "POST" | "PATCH"
	path   string // POST: 컬렉션, PATCH: 멤버
	body   map[string]interface{}
	slotID string
}

type createPlan struct {
	steps   []createStep
	method  string
	warn    string
	dryText string
}

// planCreate 는 벤더별 생성 순서를 정합니다.
//   - Dell: 빈 슬롯이 있으면 슬롯 PATCH (iDRAC8/9 verified), 없으면 POST (iDRAC10 verified).
//   - HPE: POST(RoleId) → 거부(400)면 POST(Oem 권한: iLO4). 슬롯 없음.
//   - 그 밖: POST → 405/501/404 면 빈 슬롯 PATCH (Lenovo Purley).
//
// 미확인 방식(실장비 검증 전)은 warn 에 사유를 적고 그대로 적용을 허용한다 (사양: 경고만).
// role 은 BMC Roles 에서 고른 RoleId (비면 sp.Role 의 후보를 순서대로, Cisco CIMC 는 소문자 이름 먼저).
func (e *acctEnv) planCreate(sp acctSpec, vendor, coll string, slots []acctMember, role string) *createPlan {
	roles := []string{role}
	if role == "" {
		ids := acctRoleIDs[sp.Role]
		roles = ids
		if strings.EqualFold(vendor, "Cisco") {
			roles = []string{ids[len(ids)-1], ids[0]} // admin / user / read-only 먼저
		}
	}
	postBody := func(r string) map[string]interface{} {
		b := map[string]interface{}{"UserName": sp.User, "Password": sp.Pass, "RoleId": r}
		if !strings.EqualFold(vendor, "HPE") {
			b["Enabled"] = true
		}
		if strings.EqualFold(vendor, "cray") {
			b["PasswordChangeRequired"] = false // AMI: 생략하면 true 로 처리되어 첫 로그인 전에 변경을 요구함
		}
		return b
	}
	p := &createPlan{}
	addPost := func() {
		for _, r := range roles {
			p.steps = append(p.steps, createStep{kind: "POST", path: coll, body: postBody(r)})
		}
		if strings.EqualFold(vendor, "HPE") {
			priv := hpeOemPrivileges(sp.Role)
			for _, ok := range []string{"Hpe", "Hp"} {
				p.steps = append(p.steps, createStep{kind: "POST", path: coll, body: map[string]interface{}{
					"UserName": sp.User, "Password": sp.Pass,
					"Oem": map[string]interface{}{ok: map[string]interface{}{"LoginName": sp.User, "Privileges": priv}},
				}})
			}
		}
	}
	addSlot := func() {
		if len(slots) == 0 {
			return
		}
		s := slots[0]
		for _, r := range roles {
			p.steps = append(p.steps, createStep{kind: "PATCH", path: s.Path, slotID: s.ID, body: map[string]interface{}{
				"UserName": sp.User, "Password": sp.Pass, "RoleId": r, "Enabled": true,
			}})
		}
	}
	switch {
	case strings.EqualFold(vendor, "Dell"):
		addSlot()
		addPost()
		if len(slots) > 0 {
			p.method = "PATCH 빈 슬롯 " + slots[0].ID + " (iDRAC8/9 방식)"
			p.warn = "Dell 빈 슬롯 PATCH 는 iDRAC8/9 에서 verified, iDRAC10 은 POST 만 verified — 거부되면 POST 로 다시 시도"
		} else {
			p.method = "POST " + coll + " (iDRAC10 방식)"
		}
	default:
		addPost()
		addSlot()
		p.method = "POST " + coll
		if len(slots) > 0 {
			p.method += " (거부 시 슬롯 PATCH)"
		}
		switch strings.ToLower(vendor) {
		case "cisco", "cray", "supermicro":
			p.warn = "실장비 미확인 방식 (id_json 문서 근거)"
		}
	}
	p.dryText = "생성 요청 본문(비밀번호 제외): " + maskedBody(p.steps)
	return p
}

// hpeOemPrivileges 는 RoleId 를 거부하는 HPE iLO4 에 보낼 Oem 권한입니다 (iLO 의 기본 역할 구성과 같게).
func hpeOemPrivileges(role string) map[string]bool {
	switch role {
	case roleReadOnly:
		return map[string]bool{"LoginPriv": true}
	case roleOperator:
		return map[string]bool{"LoginPriv": true, "RemoteConsolePriv": true, "VirtualMediaPriv": true, "VirtualPowerAndResetPriv": true}
	}
	return map[string]bool{"LoginPriv": true, "RemoteConsolePriv": true, "VirtualMediaPriv": true,
		"VirtualPowerAndResetPriv": true, "iLOConfigPriv": true, "UserConfigPriv": true}
}

// maskedBody 는 dry-run 에 보여 줄 첫 단계 본문입니다. Password 값은 ***** 로 가립니다.
func maskedBody(steps []createStep) string {
	if len(steps) == 0 {
		return "(보낼 요청 없음)"
	}
	cp := map[string]interface{}{}
	for k, v := range steps[0].body {
		cp[k] = v
	}
	if _, ok := cp["Password"]; ok {
		cp["Password"] = "*****"
	}
	b, _ := json.Marshal(cp)
	return steps[0].kind + " " + steps[0].path + " " + string(b)
}

// runCreate 는 단계를 순서대로 시도합니다. 400/405/501/404(요청 형태 거부) 만 다음 단계로 넘어가고
// 인증·권한(401/403)·그 밖의 오류는 즉시 멈춥니다 (같은 요청을 반복해 보내지 않음).
func (e *acctEnv) runCreate(c *Client, h *acctHost, p *createPlan) (bool, string) {
	var last string
	for i, s := range p.steps {
		var st int
		var err error
		member := ""
		switch s.kind {
		case "POST":
			st, member, _, err = c.AccountPost(s.path, s.body)
		case "PATCH":
			if aerr := c.AllowAccountTarget(s.path); aerr != nil {
				return false, "내부 오류: " + aerr.Error()
			}
			st, _, err = c.AccountPatch(s.path, s.body)
		}
		if err == nil {
			h.Method = s.kind + " " + s.path
			if s.kind == "POST" {
				h.createdMember = member
			}
			if s.kind == "PATCH" {
				h.slotPath = s.path
			}
			return true, ""
		}
		last = writeBrief(st, err)
		switch st {
		case http.StatusBadRequest, http.StatusMethodNotAllowed, http.StatusNotImplemented, http.StatusNotFound:
			if i+1 < len(p.steps) {
				continue
			}
		case http.StatusUnauthorized, http.StatusForbidden:
			h.Status = acctRejected
			return false, "권한 없음 또는 인증 거부 (" + last + ") — admin 계정에 사용자 관리 권한이 있는지 확인"
		}
		if st >= 200 && st < 300 {
			// 2xx 인데 응답을 끝까지 받지 못함: 만들어졌을 수 있다.
			h.Status = acctVerifyUnknown
			return false, "BMC 가 HTTP " + strconv.Itoa(st) + " 로 받았으나 응답을 끝까지 못 받음 — 만들어졌을 수 있음, 수동 확인"
		}
		return false, last
	}
	return false, last
}

// writeBrief 는 쓰기 실패의 한 줄 설명입니다 (BMC 메시지만, 비밀번호는 Client 가 이미 지움).
func writeBrief(code int, err error) string {
	if errors.Is(err, ErrNotAllowed) {
		return "허용목록·본문 검사에서 막혀 보내지 않음"
	}
	if code != 0 {
		return fmt.Sprintf("HTTP %d: %s", code, bmcMessage(err))
	}
	return errBrief(err)
}

// rollbackCreate 는 이번 실행이 만든 계정을 되돌립니다. POST 로 만들었으면 멤버 DELETE(없으면 슬롯 비우기),
// 슬롯 PATCH 로 만들었으면 비활성 → 이름 비우기. 하나라도 성공하지 못하면 false.
func (e *acctEnv) rollbackCreate(c *Client, h *acctHost, coll, vendor string) bool {
	target := h.createdMember
	if target == "" {
		target = h.slotPath
	}
	if target == "" { // Location 이 없었다: 다시 읽어서 우리 이름의 멤버를 찾는다 (이름이 같은 멤버는 우리가 만든 것뿐)
		if ms, err := listAccounts(c, coll); err == nil {
			if m := findAccount(ms, e.specOf(h).User); m != nil {
				target = m.Path
			}
		}
	}
	if target == "" {
		return false
	}
	if err := c.AllowAccountTarget(target); err != nil {
		return false
	}
	if h.slotPath == "" { // POST 로 만든 멤버: DELETE
		if st, _, err := c.AccountDelete(target); err == nil || st == http.StatusNotFound {
			return true
		}
	}
	// DELETE 가 안 되는 BMC(슬롯 방식)나 슬롯 PATCH 로 만든 경우: 비활성 → 이름 비우기
	if _, _, err := c.AccountPatch(target, map[string]interface{}{"Enabled": false, "RoleId": "None"}); err != nil {
		return false
	}
	_, _, err := c.AccountPatch(target, map[string]interface{}{"UserName": ""})
	return err == nil
}

// ---- 변경 (change) ----

// changeOnHost 는 1순위 계정의 비밀번호를 바꾸고 검증·롤백합니다 (dry-run 이면 대상 확인만).
func (e *acctEnv) changeOnHost(h *acctHost) {
	t := h.Target
	if t.Err != "" {
		h.Status = t.Err
		return
	}
	// 변경은 1순위 계정으로 로그인된 호스트에만 합니다 (admin 세션으로 남의 계정 비밀번호를 바꾸지 않음,
	// 1순위 로그인이 안 되면 이전 비밀번호를 알 수 없어 롤백도 할 수 없다).
	c := NewClient(e.opts(t, e.conf.User, e.password, ModeAccount, e.conf.Retries))
	defer func() {
		_ = c.Close()
		h.Stats = c.Stats()
	}()
	if err := c.Login(); err != nil {
		h.setErr(err)
		if h.Status == StatusAuthFail {
			h.Detail = "1순위 계정으로 로그인되지 않아 변경하지 않음 (계정이 없거나 비밀번호 불일치 — account create 또는 BMC 에서 확인)"
		}
		return
	}
	h.Source = credPrimary
	h.Vendor, h.Model = lightInfo(c)
	c.AddSecret(e.password)
	c.AddSecret(e.newPass)
	coll := accountsCollection(c)
	members, err := listAccounts(c, coll)
	if err != nil {
		h.setErr(err)
		return
	}
	own := findAccountExact(members, e.conf.User)
	if own == nil {
		h.Status, h.Detail = acctNoAccount, "BMC 에 id 가 없음 — account create 로 먼저 만드십시오"
		return
	}
	h.Method = "PATCH " + own.Path
	if e.dryRun {
		h.Status = acctWouldChange
		h.Detail = `요청 본문(비밀번호 제외): PATCH ` + own.Path + ` {"Password":"*****"}`
		return
	}
	if err := c.AllowAccountTarget(own.Path); err != nil {
		h.Status, h.Detail = acctRejected, "내부 오류: "+err.Error()
		return
	}
	st, _, perr := c.AccountPatch(own.Path, map[string]interface{}{"Password": e.newPass})
	if perr != nil {
		if st >= 200 && st < 300 {
			h.Status = acctVerifyUnknown
			h.Detail = "BMC 가 HTTP " + strconv.Itoa(st) + " 로 받았으나 응답을 끝까지 못 받음 — 변경됐을 수 있음, 수동 확인"
			return
		}
		h.Status, h.Detail = acctRejected, writeBrief(st, perr)
		if s2, _ := errStatus(perr); s2 == StatusAuthFail {
			h.Status = StatusAuthFail
		}
		return
	}
	verr := e.verifyLogin(t, e.conf.User, e.newPass)
	if verr == nil {
		h.Status = acctChanged
		return
	}
	vst, _ := errStatus(verr)
	if vst != StatusAuthFail {
		h.Status = acctVerifyUnknown
		h.Detail = "변경은 했으나 새 비밀번호 검증 로그인을 통신 오류로 하지 못함 (" + errBrief(verr) + ") — 롤백하지 않음, 수동 확인"
		return
	}
	// 새 비밀번호로 로그인이 거부됨 → 열려 있는 세션으로 이전 비밀번호를 되돌린다.
	if _, _, rerr := c.AccountPatch(own.Path, map[string]interface{}{"Password": e.password}); rerr == nil {
		if e.verifyLogin(t, e.conf.User, e.password) == nil {
			h.Status, h.Rollback = acctVerifyFailRB, "완료"
			h.Detail = "새 비밀번호 로그인 검증 실패 — 이전 비밀번호로 되돌림"
			return
		}
	}
	h.Status, h.Rollback = acctRollbackFailed, "실패"
	h.Detail = "새 비밀번호 로그인 검증 실패 + 되돌리기 실패 — BMC 에서 " + e.conf.User + " 계정 상태를 수동 확인하십시오"
}

// ---- 병렬 처리 ----

// isAcctFailure 는 웜업에서 "이상 징후"로 세는 상태입니다.
func isAcctFailure(st string) bool {
	switch st {
	case acctRejected, acctVerifyFailRB, acctRollbackFailed, StatusAuthFail:
		return true
	}
	return false
}

// runAcctPool 은 호스트 n 대를 처리합니다. 처음 warm 대는 한 대씩 처리하고, 실패가 절반 이상이면
// (표본 acctWarmFailMin 이상) 남은 호스트는 skip 합니다. 그 뒤는 workers 개 워커로 병렬 처리합니다.
// fn(i) 은 i 번째 호스트의 최종 상태를 돌려줍니다. 각 호출은 자기 칸에만 써야 합니다.
func runAcctPool(n, workers, warm int, fn func(i int) string, skip func(i int)) {
	next := 0
	if warm > n {
		warm = n
	}
	fails := 0
	for ; next < warm; next++ {
		if isAcctFailure(fn(next)) {
			fails++
		}
	}
	if warm >= acctWarmFailMin && fails*2 >= warm {
		for ; next < n; next++ {
			skip(next)
		}
		return
	}
	if workers < 1 {
		workers = 1
	}
	if workers > n-next {
		workers = n - next
	}
	if workers < 1 {
		return
	}
	idx := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				fn(i)
			}
		}()
	}
	for ; next < n; next++ {
		idx <- next
	}
	close(idx)
	wg.Wait()
}

// ---- 결과 출력 ----

// account 열은 맨 끝에 둔다 (result 가 7번째 열인 기존 awk 명령이 그대로 맞게).
var acctResultHeader = []string{"hostname", "ip", "vendor", "model", "op", "method", "result", "rollback", "detail", "warn", "account"}

func (h *acctHost) row() []string {
	f := func(s string) string {
		s = sanitizeDetail(s)
		if s == "" {
			return "-"
		}
		return s
	}
	return []string{h.Target.Hostname, f(h.Target.IP), f(h.Vendor), f(h.Model), h.Op, f(h.Method), f(h.Status), f(h.Rollback), f(h.Detail), f(h.Warn), f(h.User)}
}

// writeAcctResult 는 account_result.tsv 를 씁니다 (0600). 비밀번호·계정 해시는 어디에도 없다.
func writeAcctResult(dir string, hosts []*acctHost) (string, error) {
	var rows [][]string
	for _, h := range hosts {
		if h != nil {
			rows = append(rows, h.row())
		}
	}
	p := filepath.Join(dir, "account_result.tsv")
	return p, writeTSV(p, acctResultHeader, rows)
}

// acctSummaryOrder 는 요약에 보여 줄 상태 순서입니다.
var acctSummaryOrder = []string{
	acctCreated, acctChanged, acctWouldCreate, acctWouldChange, acctPrimaryOK, acctPresent, acctExists, acctNeedCreate,
	acctVerifyFailRB, acctRollbackFailed, acctVerifyUnknown, acctRejected, acctNoSlot, acctNoAdmin, acctNoAccount,
	acctSkippedWarmup,
}

// writeAcctReport 는 결과 요약을 출력합니다. listMax 는 상태별로 나열할 호스트 이름 수입니다.
func writeAcctReport(w io.Writer, hosts []*acctHost, listMax int, p painter) {
	by := map[string][]*acctHost{}
	var order []string
	for _, h := range hosts {
		if h == nil {
			continue
		}
		if _, ok := by[h.Status]; !ok {
			order = append(order, h.Status)
		}
		by[h.Status] = append(by[h.Status], h)
	}
	known := map[string]bool{}
	var states []string
	for _, s := range acctSummaryOrder {
		if len(by[s]) > 0 {
			states = append(states, s)
		}
		known[s] = true
	}
	var rest []string
	for _, s := range order {
		if !known[s] {
			rest = append(rest, s)
		}
	}
	sort.Strings(rest)
	states = append(states, rest...)
	for _, s := range states {
		hs := by[s]
		label := fmt.Sprintf("[%s] %d대", s, len(hs))
		switch s {
		case acctCreated, acctChanged:
			label = p.green(label)
		case acctRollbackFailed, acctVerifyUnknown:
			label = p.boldRed(label)
		case acctVerifyFailRB, acctRejected, acctNoSlot, acctNoAdmin, acctNoAccount, StatusAuthFail:
			label = p.red(label)
		case acctExists, acctSkippedWarmup:
			label = p.yellow(label)
		}
		fmt.Fprintln(w, label)
		names := make([]string, 0, len(hs))
		for _, h := range hs {
			names = append(names, h.name())
		}
		sort.Strings(names)
		shown := names
		if listMax >= 0 && len(shown) > listMax {
			shown = shown[:listMax]
		}
		for _, l := range wrapNames(shown, "  ", 100) {
			fmt.Fprintln(w, l)
		}
		if len(names) > len(shown) {
			fmt.Fprintf(w, "  ... 외 %d대 (account_result.tsv 참고)\n", len(names)-len(shown))
		}
		// 원인이 있는 상태는 첫 몇 건의 사유를 보여 준다.
		switch s {
		case acctRejected, acctVerifyFailRB, acctRollbackFailed, acctVerifyUnknown, acctNoSlot, acctNoAdmin, acctNoAccount, acctExists, StatusAuthFail:
			shownN := 0
			for _, h := range hs {
				if h.Detail == "" {
					continue
				}
				if shownN >= 3 {
					break
				}
				shownN++
				fmt.Fprintf(w, "    - %s: %s\n", h.name(), sanitizeDetail(h.Detail))
			}
		}
	}
	warned := map[string]bool{}
	for _, h := range hosts {
		if h != nil && h.Warn != "" && !warned[h.Warn] {
			warned[h.Warn] = true
			fmt.Fprintln(w, p.yellow("※ 경고: "+sanitizeDetail(h.Warn)))
		}
	}
}

// ---- Y/n 확인 ----

// askYesNo 는 한 줄을 읽어 y/n 을 판정합니다. defaultYes 면 빈 입력도 y 입니다. EOF 는 n 입니다.
func askYesNo(in io.Reader, w io.Writer, prompt string, defaultYes bool) bool {
	fmt.Fprint(w, prompt)
	line := readRawLine(in)
	if !strings.HasSuffix(line, "\n") {
		fmt.Fprintln(w)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	case "":
		return defaultYes && strings.HasSuffix(line, "\n")
	}
	return false
}

// readRawLine 은 한 줄만 바이트 단위로 읽습니다 (버퍼링하면 뒤이은 질문의 입력을 삼킬 수 있음, 256바이트까지). 줄 끝 \n 포함.
func readRawLine(in io.Reader) string {
	var sb strings.Builder
	one := make([]byte, 1)
	for sb.Len() < 256 {
		n, err := in.Read(one)
		if n == 1 {
			sb.WriteByte(one[0])
			if one[0] == '\n' {
				break
			}
		}
		if err != nil {
			break
		}
	}
	return sb.String()
}

// acctWorkers 는 -j 값을 정합니다 (0 이면 min(conf.Concurrency, 100), 상한 300).
func acctWorkers(flagVal, confConc int) int {
	w := flagVal
	if w <= 0 {
		w = confConc
		if w <= 0 || w > acctDefaultWorkers {
			w = acctDefaultWorkers
		}
	}
	if w > acctMaxWorkers {
		w = acctMaxWorkers
	}
	return w
}

// ---- 명령 ----

const accountUsage = `사용법:
  biostool account create [옵션]   1순위 계정이 없는 BMC 에 계정 생성 (점검 → 목록 → Y/n → 생성·검증·롤백)
                                   bios.conf 에 user2(+pass2.enc, user2_role)가 있으면 두 번째 계정도 없는 곳에 만든다
  biostool account change [옵션]   1순위 계정의 비밀번호 변경 (새 비밀번호는 터미널에서 직접 2번 입력)

옵션:
  -conf <파일>      설정 파일 (기본 bios.conf)        -user <파일>   대상 목록 (기본 user.txt)
  -hosts <파일>     이름 해석용 hosts (기본 /etc/hosts)  -rf-conf <파일> 2순위(admin) 계정 파일 (기본 /etc/rf_tool/rf.conf)
  -dry-run          전송 없이 계획만 출력              -no-prompt     묻지 않고 목록만 출력
  -stdin-ok         (create) 터미널이 아닌 입력의 y/n 허용
  -j <N>            동시 처리 수 (기본 100, 최대 300)    -list-max <N>  상태별로 나열할 호스트 수 (기본 20)
  -ucsm             대상이 Cisco UCSM (XML API, 로컬 사용자). 대상은 UCSM 주소
`

type accountFlags struct {
	co       *commonOpts
	noPrompt bool
	stdinOK  bool
	workers  int
	listMax  int
	ucsm     bool
}

func cmdAccount(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(accountUsage)
		return nil
	}
	op := args[0]
	if op != "create" && op != "change" {
		return fmt.Errorf("알 수 없는 account 동작 %q (create 또는 change)\n\n%s", op, accountUsage)
	}
	fs, co := newFlagSet("account " + op)
	af := &accountFlags{co: co}
	fs.BoolVar(&af.noPrompt, "no-prompt", false, "묻지 않고 목록만 출력")
	fs.BoolVar(&af.stdinOK, "stdin-ok", false, "(create) 터미널이 아닌 입력의 y/n 허용")
	fs.IntVar(&af.workers, "j", 0, "동시 처리 수 (기본 100, 최대 300)")
	fs.IntVar(&af.listMax, "list-max", 20, "상태별로 나열할 호스트 수")
	fs.BoolVar(&af.ucsm, "ucsm", false, "대상이 Cisco UCSM (XML API)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if af.listMax < 0 {
		return errors.New("-list-max 는 0 이상이어야 합니다")
	}
	rc, err := prepare(co, "", "")
	if err != nil {
		return err
	}
	if op == "create" && (rc.Fallback == nil || len(rc.Fallback.vendors()) == 0) {
		return errors.New("rf.conf 에 벤더 admin 계정이 없습니다. 계정 생성은 2순위(admin) 계정이 필요합니다 — /etc/rf_tool/rf.conf 를 확인하십시오 (-rf-conf)")
	}
	env := &acctEnv{conf: rc.Conf, password: rc.Password, fb: rc.Fallback, dryRun: co.dryRun}
	if op == "create" {
		if env, err = newAcctEnv(rc, co.dryRun); err != nil {
			return err
		}
	}
	workers := acctWorkers(af.workers, rc.Conf.Concurrency)
	p := painter{useColor()}
	dir, err := newRunDir(rc.Conf.ResultDir, time.Now())
	if err != nil {
		return fmt.Errorf("결과 폴더: %w", err)
	}
	ids := rc.Conf.User
	if op == "create" {
		ids = acctSpecsText(env.accounts())
	}
	fmt.Printf("설정: %s (id=%s, 동시 %d, dry-run=%t)\n", co.conf, ids, workers, co.dryRun)
	fmt.Printf("대상: %d개, 결과 폴더: %s\n", len(rc.Targets), dir)

	switch {
	case af.ucsm && op == "create":
		return ucsmAccountCreate(rc, env, af, dir, p)
	case af.ucsm:
		return ucsmAccountChange(rc, env, af, dir, p)
	case op == "create":
		return runAccountCreate(rc, env, af, dir, workers, p)
	}
	return runAccountChange(rc, env, af, dir, workers, p)
}

// runAccountCreate: 점검 → 목록 → Y/n → 생성·검증·롤백.
func runAccountCreate(rc *runContext, env *acctEnv, af *accountFlags, dir string, workers int, p painter) error {
	n := len(rc.Targets)
	groups := make([][]*acctHost, n) // 호스트마다 계정 칸 (1순위가 첫 칸)
	authStop := rc.Conf.AuthFailStop
	fmt.Printf("\n[1/2] 점검 중 (읽기 전용, 1순위 계정 → 안 되면 2순위 계정 1회)\n")
	sweepHosts(n, workers, authStop,
		func(i int) hostOutcome {
			groups[i] = env.probeCreateAll(rc.Targets[i])
			h := groups[i][0]
			// 2순위로 붙는 것이 이 작업의 정상 상황이므로 차단기에는 로그인 성공으로 센다.
			switch {
			case h.Status == StatusAuthFail:
				return outAuthFail
			case h.Target.Err != "":
				return outNone
			case h.Source != "":
				return outLoggedIn
			}
			return outOther
		},
		func(i int) {
			groups[i] = newCreateHosts(rc.Targets[i], env.accounts())
			for _, h := range groups[i] {
				h.Status = StatusSkippedAuthStop
			}
		})

	hosts := flattenAcct(groups)
	need := needCreateGroups(groups)
	fmt.Println()
	tot := map[string]int{}
	for _, g := range groups {
		tot[g[0].Status]++
	}
	fmt.Printf("점검 결과: 1순위 계정으로 접속 %d대, 2순위로만 접속(생성 대상) %d대, 그 밖 %d대\n",
		tot[acctPrimaryOK], tot[acctNeedCreate], n-tot[acctPrimaryOK]-tot[acctNeedCreate])
	if specs := env.accounts(); len(specs) > 1 {
		t2 := map[string]int{}
		for _, g := range groups {
			t2[g[1].Status]++
		}
		fmt.Printf("두 번째 계정 %s(%s): 이미 있음 %d대, 생성 대상 %d대\n", specs[1].User, specs[1].Role, t2[acctPresent], t2[acctNeedCreate])
	}
	if len(need) == 0 {
		writeAcctReport(os.Stdout, hosts, af.listMax, p)
		path, werr := writeAcctResult(dir, hosts)
		fmt.Printf("\n생성할 호스트가 없습니다. 결과: %s\n", path)
		return werr
	}
	what := createWhat(need)
	fmt.Printf("\n만들 계정: %s\n", what)
	printAcctTargets(os.Stdout, flattenAcct(need), af.listMax)

	if !env.dryRun {
		ok, err := confirmCreate(os.Stdin, os.Stdout, what, af)
		if err != nil || !ok {
			path, _ := writeAcctResult(dir, hosts)
			fmt.Printf("생성하지 않고 종료합니다. 점검 결과: %s\n", path)
			return err
		}
	}
	fmt.Printf("\n[2/2] %s\n", map[bool]string{true: "dry-run: 보낼 계획만 확인 (쓰기 없음)", false: "계정 생성 · 로그인 검증 · 실패 서버 롤백"}[env.dryRun])
	createHosts(env, need, workers)
	fmt.Println()
	writeAcctReport(os.Stdout, flattenAcct(need), af.listMax, p)
	path, werr := writeAcctResult(dir, hosts)
	fmt.Printf("\n결과 파일: %s\n", path)
	printAcctDisclaimer(os.Stdout, "create", env.dryRun)
	return werr
}

// flattenAcct 는 호스트별 계정 칸을 한 줄로 폅니다 (결과 파일·요약용).
func flattenAcct(groups [][]*acctHost) []*acctHost {
	var out []*acctHost
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// needCreateGroups 는 호스트마다 생성 대상(NEED_CREATE) 칸만 남깁니다 (없는 호스트는 뺌).
func needCreateGroups(groups [][]*acctHost) [][]*acctHost {
	var out [][]*acctHost
	for _, g := range groups {
		var keep []*acctHost
		for _, h := range g {
			if h != nil && h.Status == acctNeedCreate {
				keep = append(keep, h)
			}
		}
		if len(keep) > 0 {
			out = append(out, keep)
		}
	}
	return out
}

// createWhat 은 확인 질문에 넣을 "계정(권한) N대" 목록입니다 (계정 순서는 처음 나온 순서).
func createWhat(groups [][]*acctHost) string {
	cnt := map[string]int{}
	role := map[string]string{}
	var order []string
	for _, h := range flattenAcct(groups) {
		if _, ok := cnt[h.User]; !ok {
			order = append(order, h.User)
			role[h.User] = roleAdministrator
			if h.spec != nil {
				role[h.User] = h.spec.Role
			}
		}
		cnt[h.User]++
	}
	parts := make([]string, 0, len(order))
	for _, u := range order {
		parts = append(parts, fmt.Sprintf("%s(%s) %d대", sanitizeDetail(u), role[u], cnt[u]))
	}
	return strings.Join(parts, ", ")
}

// acctSpecsText 는 설정 요약용 "계정(권한)" 목록입니다.
func acctSpecsText(specs []acctSpec) string {
	parts := make([]string, 0, len(specs))
	for _, s := range specs {
		parts = append(parts, s.User+"("+s.Role+")")
	}
	return strings.Join(parts, ", ")
}

// createHosts 는 대상 호스트들에 계정을 만듭니다 (웜업 + 병렬, 호스트마다 admin 로그인 1회).
func createHosts(env *acctEnv, need [][]*acctHost, workers int) {
	runAcctPool(len(need), workers, acctWarmup,
		func(i int) string {
			env.createGroup(need[i])
			for _, h := range need[i] {
				if isAcctFailure(h.Status) {
					return h.Status
				}
			}
			return need[i][0].Status
		},
		func(i int) {
			for _, h := range need[i] {
				h.Status = acctSkippedWarmup
				h.Detail = "웜업(처음 10대)에서 실패가 많아 중단 — 원인 확인 후 다시 실행"
			}
		})
}

func printAcctTargets(w io.Writer, hs []*acctHost, max int) {
	for i, h := range hs {
		if max >= 0 && i >= max {
			fmt.Fprintf(w, "  ... 외 %d대\n", len(hs)-max)
			return
		}
		fmt.Fprintf(w, "  %-28s %-12s %s\n", h.name(), orDefault(sanitizeDetail(h.Vendor), "-"), orDefault(sanitizeDetail(h.Model), "-"))
	}
}

// confirmCreate 는 생성 여부를 묻습니다 (기본 Y: 빈 입력도 생성). 터미널이 아니면 -stdin-ok 없이는 묻지 않고 N.
// what 은 만들 계정 목록 (createWhat, 예: "admin1(Administrator) 3대, ops1(Operator) 5대").
func confirmCreate(in *os.File, w io.Writer, what string, af *accountFlags) (bool, error) {
	if af.noPrompt {
		fmt.Fprintln(w, "\n-no-prompt 이므로 묻지 않고 종료합니다 (아무것도 만들지 않음).")
		return false, nil
	}
	if !af.stdinOK && !stdinIsTerminal(in) {
		fmt.Fprintf(w, "\n%s (아무것도 만들지 않음).\n", msgNonInteractive)
		return false, nil
	}
	if stdinIsTerminal(in) {
		ttyFlushInput(int(in.Fd())) // 점검 중 눌러 둔 Enter 가 기본 Y 로 읽히지 않게 비운다
	}
	prompt := fmt.Sprintf("\n계정을 만들까요? [%s] 재부팅·BIOS 변경은 없습니다. [Y/n]: ", what)
	return askYesNo(in, w, prompt, true), nil
}

// runAccountChange: 대상 확인 → 새 비밀번호 입력 → y 확인 → 변경·검증·롤백 → pass.enc 안내.
func runAccountChange(rc *runContext, env *acctEnv, af *accountFlags, dir string, workers int, p painter) error {
	n := len(rc.Targets)
	hosts := make([]*acctHost, n)
	for i, t := range rc.Targets {
		hosts[i] = newAcctHost(t, "change")
		hosts[i].User = rc.Conf.User
	}
	if !env.dryRun {
		ok, err := acquireNewPassword(rc, env, af, n)
		if err != nil || !ok {
			return err
		}
	}
	fmt.Printf("\n비밀번호 변경 · 새 비밀번호 로그인 검증 · 실패 서버 롤백%s\n", map[bool]string{true: " (dry-run: 전송 없음)", false: ""}[env.dryRun])
	runAcctPool(n, workers, acctWarmup,
		func(i int) string { env.changeOnHost(hosts[i]); return hosts[i].Status },
		func(i int) {
			hosts[i].Status = acctSkippedWarmup
			hosts[i].Detail = "웜업(처음 10대)에서 실패가 많아 중단 — 원인 확인 후 다시 실행"
		})
	fmt.Println()
	writeAcctReport(os.Stdout, hosts, af.listMax, p)
	path, werr := writeAcctResult(dir, hosts)
	fmt.Printf("\n결과 파일: %s\n", path)
	if !env.dryRun {
		printEncryptGuide(os.Stdout, hosts, rc.Conf)
	}
	printAcctDisclaimer(os.Stdout, "change", env.dryRun)
	return werr
}

// acquireNewPassword 는 change 의 새 비밀번호를 터미널에서 직접 입력받고 진행 여부(y/N, 기본 N)를 묻습니다.
// 입력한 비밀번호는 env.newPass 에만 둡니다. false 면 아무것도 바꾸지 않고 끝냅니다.
func acquireNewPassword(rc *runContext, env *acctEnv, af *accountFlags, n int) (bool, error) {
	if af.noPrompt {
		fmt.Println("\n-no-prompt 이므로 묻지 않고 종료합니다 (아무것도 바꾸지 않음).")
		return false, nil
	}
	fmt.Printf("\n%d대의 계정 %q 비밀번호를 바꿉니다. 새 비밀번호를 입력하십시오.\n", n, rc.Conf.User)
	pw, err := readNewPassword(os.Stdin, os.Stdout)
	if err != nil {
		return false, err
	}
	env.newPass = pw
	if !askYesNo(os.Stdin, os.Stdout, fmt.Sprintf("\n%d대의 %s 비밀번호를 지금 바꿉니다. 계속할까요? [y/N]: ", n, rc.Conf.User), false) {
		fmt.Println("바꾸지 않고 종료합니다.")
		return false, nil
	}
	return true, nil
}

// printEncryptGuide 는 변경 후 pass.enc 를 새 비밀번호로 바꾸는 명령을 csh 에서 그대로 복사해 쓸 수 있게 출력합니다.
// pass.enc 는 자동 갱신하지 않으며, 명령에는 비밀번호를 넣지 않습니다 (encrypt.sh 가 직접 입력받음).
func printEncryptGuide(w io.Writer, hosts []*acctHost, conf *Config) {
	ok, bad := 0, 0
	for _, h := range hosts {
		switch h.Status {
		case acctChanged:
			ok++
		case acctVerifyFailRB, acctRollbackFailed, acctVerifyUnknown, acctRejected, acctNoAccount, acctSkippedWarmup, StatusAuthFail:
			bad++
		}
	}
	if ok == 0 {
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "※ pass.enc 는 자동으로 바꾸지 않았습니다. 새 비밀번호로 바꾸려면 아래 명령을 복사해 실행하십시오 (csh/tcsh/bash 공통, 비밀번호는 실행 중 직접 입력):")
	fmt.Fprintf(w, "    cd %s; bash encrypt.sh\n", cshQuote(wd))
	if bad > 0 {
		fmt.Fprintf(w, "※ 변경되지 않은 호스트 %d대는 이전 비밀번호가 그대로입니다. pass.enc 를 바꾸면 그 호스트는 점검에서 AUTH_FAIL 이 됩니다.\n", bad)
		fmt.Fprintln(w, "  전부 성공한 뒤에 바꾸거나, 실패 호스트를 먼저 정리하십시오 (결과 파일의 result 열 참고).")
	}
}

// printAcctDisclaimer 는 쓰기 작업 뒤의 확인 안내입니다 (git-upload 문서 표준의 Disclaimer).
func printAcctDisclaimer(w io.Writer, op string, dry bool) {
	if dry {
		fmt.Fprintln(w, "\n※ dry-run 이므로 BMC 에는 아무것도 쓰지 않았습니다.")
		return
	}
	fmt.Fprintln(w, "\n※ 주의: 이 도구는 참고용(보조 도구)입니다. 계정을 "+map[string]string{"create": "만든", "change": "바꾼"}[op]+" 뒤에는 랜덤한 서버 몇 대를 직접 골라 실제로 로그인되는지 확인하십시오.")
}
