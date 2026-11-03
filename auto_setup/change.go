// change.go - m 메뉴(v0.7.0): IP 변경·LDAP 변경·IP+LDAP 변경·원복·auto.appl 원복 흐름의 상태기계와 순수 로직
// (메뉴 가시성, 입력 정규화·검증, infra 목록, bindpw 마스킹, 키 처리). 엔진 실행·경유는 changerun.go,
// 기록은 changerec.go, 화면 문자열은 change_render.go. TUI 연결(m 키·안내 줄·도움말)은 tui.go 가 한다.
//
// 흐름: (메뉴 → kind) → [원복 종류] → user → [infra] → 입력(Ctrl+D) → [LDAP dry-run] → 미리보기 → yes → 실행 → 결과.
// 안전 규칙: 서비스 재시작 없음(LDAP 엔진 호출에는 항상 -no-restart), gossh 비밀번호 인자(-p) 없음, bindpw 는
// 화면·기록·로그·임시 파일·명령 인자 어디에도 남기지 않는다(엔진 출력은 들어오는 즉시 마스킹, DIFF 줄은 버림).
package main

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// changeKind: 메뉴 항목 종류
type changeKind int

const (
	kindIP          changeKind = iota + 1 // 1 IP 변경
	kindLDAP                              // 2 LDAP 변경
	kindBoth                              // 3 IP+LDAP 변경 (IP 먼저 → OK 호스트만 LDAP)
	kindRollback                          // 4 원복 (IP / LDAP / 둘 다 서브선택)
	kindApplRestore                       // 5 auto.appl 원복 (노드의 /root/auto_appl_restore.sh 만 실행)
)

func (k changeKind) title() string {
	switch k {
	case kindIP:
		return "IP 변경"
	case kindLDAP:
		return "LDAP 변경"
	case kindBoth:
		return "IP+LDAP 변경"
	case kindRollback:
		return "원복"
	case kindApplRestore:
		return "auto.appl 원복"
	}
	return "변경"
}

// changeMenuItem: m 메뉴 한 줄 (Key 는 계획서의 고정 번호 — 꺼진 항목이 있어도 번호는 그대로)
type changeMenuItem struct {
	Key   rune
	Kind  changeKind
	Label string
}

// changeMenuItems: 켜진 항목만 (1·2 는 각자, 3 은 둘 다 켜져야, 4 는 하나라도, 5 는 LDAP). 비면 m 키 무반응.
func changeMenuItems() []changeMenuItem {
	ip, ld := ipChangeEnabled(), ldapChangeEnabled()
	var out []changeMenuItem
	if ip {
		out = append(out, changeMenuItem{'1', kindIP, "IP 변경"})
	}
	if ld {
		out = append(out, changeMenuItem{'2', kindLDAP, "LDAP 변경"})
	}
	if ip && ld {
		out = append(out, changeMenuItem{'3', kindBoth, "IP+LDAP 변경 (IP 먼저, IP 실패 호스트는 LDAP 건너뜀)"})
	}
	if ip || ld {
		out = append(out, changeMenuItem{'4', kindRollback, "원복 (IP·LDAP·둘 다, 가장 최근 백업)"})
	}
	if ld {
		out = append(out, changeMenuItem{'5', kindApplRestore, "auto.appl 원복 (LDAP 적용 후 대기 중인 auto.appl 반영)"})
	}
	return out
}

// ---- 주입 가능한 환경 ----

// changeEnv: 변경 흐름의 외부 의존 (테스트에서 가짜로 바꾼다)
type changeEnv struct {
	Now      func() time.Time
	Exec     changeExecFn                          // 외부 명령 실행기 (nil = changeExec)
	StateDir string                                // 기록 위치 기준 (dataDir())
	Log      func(format string, a ...interface{}) // auto_setup.log 요약 한 줄 (nil = logf)

	Tracked map[string]string // 진행 중 job 의 호스트 → "job/yml" (미리보기 경고용)
	Routes  map[string]string // 호스트 → "" | local | os6 (스냅샷에서 알려진 경로)

	OS6Mgmt, OS6Gossh string // 비면 os6 경유 없음
	Gossh             string // os8 쪽 gossh (비면 "gossh")
	IPEngine          string // ip-change-engine 경로 (engineBin)
	LDAPEngine        string // ldap-config-engine 경로
	ConfDir           string // ip_change.conf · ldap_config.conf · assets.txt 디렉터리
	IPOn, LDAPOn      bool   // 원복 서브메뉴 가시성

	User       *oneoffState                            // newUserStep 결과 (menu·route·msg), nil = user 이름 직접 입력
	UserLookup func(route, sel string) (string, error) // 번호 → user 이름 (nil = route/info.sh)
}

// newChangeEnv: 실제 환경 (S4 가 m 메뉴에서 호출). snap 은 nil 이어도 된다.
func newChangeEnv(snap *Snapshot, user *oneoffState) *changeEnv {
	tr, rt := changeTracked(snap)
	return &changeEnv{
		Now: time.Now, Exec: changeExec, StateDir: dataDir(), Log: logf,
		Tracked: tr, Routes: rt,
		OS6Mgmt: os6_mgmt, OS6Gossh: os6_gossh, Gossh: "gossh",
		IPEngine: engineBin(ipEngineName), LDAPEngine: engineBin(ldapEngineName), ConfDir: changeConfDir(),
		IPOn: ipChangeEnabled(), LDAPOn: ldapChangeEnabled(), User: user,
	}
}

// changeTracked: 스냅샷 → (진행 중 job 호스트 → "job/yml", 호스트 → route). 종료된 job 은 경로만 참고한다.
func changeTracked(snap *Snapshot) (tracked, routes map[string]string) {
	tracked, routes = map[string]string{}, map[string]string{}
	if snap == nil {
		return
	}
	for _, j := range snap.Jobs {
		for _, g := range j.Groups {
			for _, h := range g.Hosts {
				if r := h.Route; r != "" && (routes[h.Host] == "" || !j.Closed) {
					routes[h.Host] = r
				}
				if !j.Closed {
					if _, ok := tracked[h.Host]; !ok {
						tracked[h.Host] = j.ID + "/" + g.Yml
					}
				}
			}
		}
	}
	return
}

// ---- 상태 ----

const (
	csFatal   = iota // 시작 불가 안내 (엔진 없음 등) — Esc 로 닫기
	csRbSel          // 원복 종류 선택
	csUser           // user 선택
	csInfra          // infra 선택 (LDAP 계열)
	csInput          // 대상 입력 (Ctrl+D 완료)
	csDry            // LDAP dry-run 조회 중 (취소: Ctrl+X)
	csPreview        // 미리보기 + yes 입력
	csRun            // 실행 중 (취소: Ctrl+X → y)
	csResult         // 결과 (스크롤, q/Esc 닫기)
)

// ipEntry: IP 변경 입력 한 줄 (GW = 새 IP 의 마지막 옥텟 1)
type ipEntry struct{ Host, IP, GW string }

type rbOption struct {
	key      rune
	ip, ldap bool
	label    string
}

type changeState struct {
	env  *changeEnv
	kind changeKind
	mask *secretMask

	step      int
	in        []rune
	msg       string
	fatal     string
	CloseMsg  string // handleKey 가 closed=true 를 돌려줄 때 메인 화면에 보일 안내 (비면 없음)
	scroll    int
	maxScroll int // 마지막 렌더에서 계산한 최대 스크롤

	rbOpts       []rbOption
	rbIP, rbLDAP bool

	user   string
	infras []string
	infra  string

	prefilled bool            // 입력이 기록 자동 채움 그대로 (첫 입력·붙여넣기면 대체)
	recOS6    map[string]bool // 기록에서 os6 경유로 처리됐던 호스트 (원복·auto.appl 원복 때 처음부터 os6)
	entries   []ipEntry
	hosts     []string
	warns     []string

	dry     map[string]*chgHost // LDAP dry-run 결과
	dryOS6  map[string]bool     // dry-run 에서 os6 경유로 응답한 호스트 → 실제 적용도 처음부터 os6
	job     *changeJob
	started time.Time

	cancelAsk bool
	results   []*chgHost
	cancelled bool
	recPath   string
	recErr    string
}

// newChangeState: kind 흐름 시작 (S4 가 m 메뉴 선택 뒤 호출)
func newChangeState(env *changeEnv, kind changeKind) *changeState {
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Exec == nil {
		env.Exec = changeExec
	}
	if env.Log == nil {
		env.Log = logf
	}
	if env.Gossh == "" {
		env.Gossh = "gossh"
	}
	c := &changeState{env: env, kind: kind, mask: loadSecretMask(filepath.Join(env.ConfDir, "ldap_config.conf")), step: csUser}
	if kind == kindRollback {
		if env.IPOn {
			c.rbOpts = append(c.rbOpts, rbOption{'1', true, false, "IP 원복"})
		}
		if env.LDAPOn {
			c.rbOpts = append(c.rbOpts, rbOption{'2', false, true, "LDAP 원복"})
		}
		if env.IPOn && env.LDAPOn {
			c.rbOpts = append(c.rbOpts, rbOption{'3', true, true, "IP+LDAP 원복"})
		}
		if len(c.rbOpts) == 0 {
			c.setFatal("원복 메뉴가 꺼져 있습니다 (enable_ip_change / enable_ldap_change)")
			return c
		}
		c.step = csRbSel
		return c
	}
	c.msg = c.userMsg()
	c.checkEngines()
	c.applyForcedUser()
	return c
}

// userMsg: newUserStep 의 안내 (user 메뉴를 못 찾았을 때)
func (c *changeState) userMsg() string {
	if c.env.User != nil {
		return c.env.User.msg
	}
	return ""
}

func (c *changeState) setFatal(m string) { c.step, c.fatal = csFatal, m }

func (c *changeState) usesIP() bool {
	return c.kind == kindIP || c.kind == kindBoth || (c.kind == kindRollback && c.rbIP)
}

func (c *changeState) usesLDAPEngine() bool {
	return c.kind == kindLDAP || c.kind == kindBoth || (c.kind == kindRollback && c.rbLDAP)
}

// needsInfra: infra 선택이 필요한 흐름 (LDAP 적용 계열만 — 원복은 infra 불필요)
func (c *changeState) needsInfra() bool { return c.kind == kindLDAP || c.kind == kindBoth }

// checkEngines: 필요한 엔진이 없으면 시작하지 않는다
func (c *changeState) checkEngines() {
	switch {
	case c.usesIP() && c.env.IPEngine == "":
		c.setFatal(ipEngineName + " 을 찾을 수 없습니다 (auto_setup.conf 의 ip_engine 또는 auto_setup 과 같은 디렉터리·PATH)")
	case c.usesLDAPEngine() && c.env.LDAPEngine == "":
		c.setFatal(ldapEngineName + " 을 찾을 수 없습니다 (auto_setup.conf 의 ldap_engine 또는 auto_setup 과 같은 디렉터리·PATH)")
	}
}

// running: 외부 프로세스(dry-run 포함)가 도는 중 — S4 는 이때 빠른 tick 으로 tick() 을 부른다
func (c *changeState) running() bool { return c.job != nil }

// recordPath: 이번 실행의 기록 파일 (없으면 "")
func (c *changeState) recordPath() string { return c.recPath }

// ---- 키 처리 ----

// handleKey: 키 이벤트 처리. 흐름을 닫아야 하면 true (CloseMsg 에 안내가 있을 수 있음).
func (c *changeState) handleKey(ev keyEv) (closed bool) {
	switch c.step {
	case csFatal:
		return ev.k == kEsc || ev.k == kEnter || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q'))
	case csRbSel:
		if ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')) {
			return true
		}
		if ev.k == kRune {
			for _, o := range c.rbOpts {
				if o.key == ev.r {
					c.rbIP, c.rbLDAP, c.step, c.msg = o.ip, o.ldap, csUser, c.userMsg()
					c.checkEngines()
					c.applyForcedUser()
					return false
				}
			}
		}
	case csUser:
		return c.userKey(ev)
	case csInfra:
		return c.infraKey(ev)
	case csInput:
		return c.inputKey(ev)
	case csDry, csRun:
		c.runKey(ev)
	case csPreview:
		return c.previewKey(ev)
	case csResult:
		return c.resultKey(ev)
	}
	return false
}

// editLine: 한 줄 편집 (Backspace·일반 문자). 처리했으면 true.
func (c *changeState) editLine(ev keyEv) bool {
	switch {
	case ev.k == kRune && ev.r == runeBackspace:
		if n := len(c.in); n > 0 {
			c.in = c.in[:n-1]
		}
		return true
	case ev.k == kRune && ev.r >= 0x20 && ev.r != 0x7f:
		c.in = append(c.in, ev.r)
		return true
	}
	return false
}

func (c *changeState) userKey(ev keyEv) bool {
	switch {
	case ev.k == kEsc || (ev.k == kRune && ev.r == 'q' && len(c.in) == 0):
		return true
	case ev.k == kEnter:
		line := strings.TrimSpace(string(c.in))
		if line == "" {
			return false
		}
		user := line
		if u := c.env.User; u != nil && u.menu != nil {
			look := c.env.UserLookup
			if look == nil {
				look = func(route, sel string) (string, error) {
					return oneoffCmdOut(awx_dir, filepath.Join(route, "info.sh"), sel)
				}
			}
			out, err := look(u.route, line)
			if err != nil {
				c.msg, c.in = "[X] user 조회 실패 — 번호를 다시 입력하세요", nil
				return false
			}
			user = strings.TrimSpace(out)
		}
		if !validChangeUser(user) {
			c.msg, c.in = "[X] user 이름이 올바르지 않습니다 (영문·숫자·. _ - 만) — 다시 입력하세요", nil
			return false
		}
		c.user, c.in, c.msg = user, nil, ""
		c.afterUser()
	default:
		c.editLine(ev)
	}
	return false
}

// afterUser: user 가 정해진 뒤의 단계 (LDAP 계열이면 infra 선택, 아니면 대상 입력)
func (c *changeState) afterUser() {
	if c.needsInfra() {
		c.step = csInfra
		names, err := ldapInfraNames(filepath.Join(c.env.ConfDir, "ldap_config.conf"))
		c.infras = names
		if err != nil || len(names) == 0 {
			c.msg = "ldap_config.conf 에 infra.<이름>.* 항목이 없습니다 (" + filepath.Join(c.env.ConfDir, "ldap_config.conf") + ") — Esc 로 닫기"
		}
		return
	}
	c.enterInput()
}

// applyForcedUser: auto_setup -user <이름> 으로 띄웠으면 user 단계를 건너뜀 (user 단계이고 치명 오류가 아닐 때만)
func (c *changeState) applyForcedUser() {
	if forceUser == "" || c.step != csUser || !validChangeUser(forceUser) {
		return
	}
	c.user, c.in, c.msg = forceUser, nil, ""
	c.afterUser()
}

func (c *changeState) infraKey(ev keyEv) bool {
	switch {
	case ev.k == kEsc || (ev.k == kRune && ev.r == 'q' && len(c.in) == 0):
		return true
	case ev.k == kEnter:
		line := strings.TrimSpace(string(c.in))
		c.in = nil
		n := 0
		for _, r := range line {
			if r < '0' || r > '9' {
				n = -1
				break
			}
			n = n*10 + int(r-'0')
			if n > 1000 {
				break
			}
		}
		if n < 1 || n > len(c.infras) {
			if len(c.infras) > 0 {
				c.msg = "[X] 목록의 번호를 입력하세요"
			}
			return false
		}
		c.infra, c.msg = c.infras[n-1], ""
		c.enterInput()
	default:
		c.editLine(ev)
	}
	return false
}

// enterInput: 입력 단계로. 원복·auto.appl 원복은 user 기록으로 자동 채운다.
func (c *changeState) enterInput() {
	c.step, c.in, c.prefilled, c.msg = csInput, nil, false, ""
	var want []string
	switch {
	case c.kind == kindApplRestore:
		want = []string{"appl"}
	case c.kind == kindRollback:
		if c.rbIP {
			want = append(want, "ip")
		}
		if c.rbLDAP {
			want = append(want, "ldap")
		}
	default:
		return
	}
	hosts, os6 := map[string]bool{}, map[string]bool{}
	var order []string
	for _, w := range want {
		hs, o6 := changeAutofill(c.env.StateDir, c.user, w)
		for _, h := range hs {
			if !hosts[h] {
				hosts[h] = true
				order = append(order, h)
			}
		}
		for h := range o6 {
			os6[h] = true
		}
	}
	c.recOS6 = os6
	if len(order) == 0 {
		c.msg = "user " + c.user + " 의 기록에 대상 호스트가 없습니다 — 호스트를 직접 입력하세요"
		return
	}
	c.in, c.prefilled = []rune(strings.Join(order, "\n")), true
}

func (c *changeState) inputKey(ev keyEv) bool {
	switch {
	case ev.k == kEsc || (ev.k == kRune && ev.r == 'q' && len(c.in) == 0):
		return true
	case ev.k == kRune && ev.r == runeCtrlD:
		c.finishInput()
		return false
	case ev.k == kRune && ev.r == runeBackspace:
		c.prefilled = false // 자동 채움 목록을 편집
		c.editLine(ev)
		return false
	}
	if c.prefilled && (ev.k == kEnter || (ev.k == kRune && (ev.r >= 0x20 || ev.r == '\t'))) {
		c.in, c.prefilled = nil, false // 새로 입력·붙여넣기 → 자동 채움 목록을 대체
	}
	switch {
	case ev.k == kEnter:
		c.in = append(c.in, '\n')
	case ev.k == kRune && ev.r == '\t':
		c.in = append(c.in, '\t')
	default:
		c.editLine(ev)
	}
	return false
}

// finishInput: Ctrl+D — 입력 검증 → (LDAP 계열) dry-run 또는 미리보기
func (c *changeState) finishInput() {
	raw := string(c.in)
	c.warns = nil
	switch c.kind {
	case kindIP, kindBoth:
		c.entries, c.warns = parseIPInput(raw)
		c.hosts = nil
		for _, e := range c.entries {
			c.hosts = append(c.hosts, e.Host)
		}
	default:
		c.hosts, c.warns = parseHostInput(raw)
	}
	if len(c.hosts) == 0 {
		c.msg = "유효한 대상이 없습니다 — 다시 입력하세요"
		if len(c.warns) > 0 {
			c.msg += " (" + c.warns[0] + ")"
		}
		return
	}
	c.msg, c.scroll = "", 0
	if c.kind == kindLDAP || c.kind == kindBoth {
		c.startDry()
		return
	}
	c.step, c.in = csPreview, nil
}

func (c *changeState) previewKey(ev keyEv) bool {
	switch {
	case ev.k == kEsc:
		c.CloseMsg = c.kind.title() + " 을(를) 취소했습니다"
		return true
	case ev.k == kUp:
		if c.scroll > 0 {
			c.scroll--
		}
	case ev.k == kDown:
		if c.scroll < c.maxScroll {
			c.scroll++
		}
	case ev.k == kPgUp:
		c.scroll = rMax(c.scroll-10, 0)
	case ev.k == kPgDn:
		c.scroll = rMin(c.scroll+10, c.maxScroll)
	case ev.k == kEnter:
		if string(c.in) != "yes" { // 정확히 yes 만 진행
			c.CloseMsg = c.kind.title() + " 을(를) 취소했습니다 (yes 가 아님 — 아무것도 바꾸지 않음)"
			return true
		}
		c.in = nil
		c.startRun()
	default:
		c.editLine(ev)
	}
	return false
}

func (c *changeState) runKey(ev keyEv) {
	if c.cancelAsk {
		c.cancelAsk = false
		if ev.k == kRune && (ev.r == 'y' || ev.r == 'Y') && c.job != nil {
			c.job.cancel()
			c.msg = "취소 요청을 보냈습니다 — 종료를 기다리는 중"
		} else {
			c.msg = ""
		}
		return
	}
	switch {
	case ev.k == kRune && ev.r == runeCtrlX:
		if c.job != nil && !c.job.finished() {
			c.cancelAsk, c.msg = true, ""
		}
	case ev.k == kEsc || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')):
		c.msg = "실행 중에는 닫을 수 없습니다 — 취소는 Ctrl+X"
	}
}

func (c *changeState) resultKey(ev keyEv) bool {
	switch {
	case ev.k == kEsc || ev.k == kEnter || (ev.k == kRune && (ev.r == 'q' || ev.r == 'Q')):
		return true
	case ev.k == kUp || (ev.k == kRune && ev.r == 'k'):
		if c.scroll > 0 {
			c.scroll--
		}
	case ev.k == kDown || (ev.k == kRune && ev.r == 'j'):
		if c.scroll < c.maxScroll {
			c.scroll++
		}
	case ev.k == kRune && ev.r == ' ', ev.k == kPgDn:
		c.scroll = rMin(c.scroll+10, c.maxScroll)
	case ev.k == kRune && ev.r == 'b', ev.k == kPgUp:
		c.scroll = rMax(c.scroll-10, 0)
	}
	return false
}

// ---- 실행 연결 ----

func (c *changeState) startDry() {
	p := changePlan{kind: c.kind, dry: true, infra: c.infra, hosts: c.hosts, forceOS6: c.knownOS6(nil)}
	c.job, c.started, c.step, c.cancelAsk = startChangeJob(c.env, c.mask, p), c.env.Now(), csDry, false
}

func (c *changeState) startRun() {
	p := changePlan{kind: c.kind, infra: c.infra, rbIP: c.rbIP, rbLDAP: c.rbLDAP,
		entries: c.entries, hosts: c.hosts, forceOS6: c.knownOS6(c.dryOS6), user: c.user}
	if c.kind == kindRollback || c.kind == kindApplRestore {
		for h := range c.recOS6 {
			p.forceOS6[h] = true
		}
	}
	c.started, c.step, c.cancelAsk, c.scroll = c.env.Now(), csRun, false, 0
	c.job = startChangeJob(c.env, c.mask, p)
}

// knownOS6: 스냅샷 route=os6 + extra (os6 설정이 없으면 changerun 이 무시)
func (c *changeState) knownOS6(extra map[string]bool) map[string]bool {
	m := map[string]bool{}
	for _, h := range c.hosts {
		if c.env.Routes[h] == "os6" || extra[h] {
			m[h] = true
		}
	}
	return m
}

// tick: 실행 진행 반영 — 끝난 작업을 수거해 다음 단계로 (이벤트·tick 마다 호출)
func (c *changeState) tick() {
	if c.job == nil || !c.job.finished() {
		return
	}
	j := c.job
	c.job, c.cancelAsk = nil, false
	switch c.step {
	case csDry:
		if j.wasCancelled() {
			c.step, c.msg = csInput, "미리보기 조회를 취소했습니다 (아무것도 바꾸지 않음)"
			return
		}
		c.dry, c.dryOS6 = map[string]*chgHost{}, map[string]bool{}
		for _, h := range j.results() {
			c.dry[h.Host] = h
			if h.Route == "os6" && h.LDAPResult != rsUnreach {
				c.dryOS6[h.Host] = true
			}
		}
		c.step, c.in, c.msg, c.scroll = csPreview, nil, "", 0
	case csRun:
		c.results, c.cancelled = j.results(), j.wasCancelled()
		c.recPath, c.recErr = j.recPath, j.recErr
		c.step, c.msg, c.scroll = csResult, "", 0
	}
}

// shutdown: TUI 종료 시 — 실행 중이면 취소하고(프로세스 그룹 SIGTERM→3초 뒤 SIGKILL) 끝날 때까지 잠깐 기다려 기록·임시 파일을 정리
func (c *changeState) shutdown() {
	if c.job == nil {
		return
	}
	c.job.cancel()
	select {
	case <-c.job.done:
	case <-time.After(oneoffKillGap + 2*time.Second):
	}
	c.tick()
}

// ---- 입력 정규화 (순수) ----

var ipSepRe = regexp.MustCompile(`->|[\s,=]+`)

// parseIPInput: "hostname ip" 줄 (공백·탭·쉼표·=·-> 구분 허용). 잘못된 줄은 사유와 함께 제외,
// 같은 호스트는 마지막 값이 우선(경고), 같은 새 IP 가 여러 호스트에 있으면 경고. 순서는 처음 나온 순.
func parseIPInput(raw string) (entries []ipEntry, warns []string) {
	idx := map[string]int{}
	byIP := map[string]string{}
	for n, ln := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		f := strings.Fields(ipSepRe.ReplaceAllString(ln, " "))
		if len(f) != 2 {
			warns = append(warns, lineNo(n)+"형식 오류 (hostname ip): "+clip(ln))
			continue
		}
		h, ip := f[0], f[1]
		if !hostRe.MatchString(h) {
			warns = append(warns, lineNo(n)+"hostname 이 올바르지 않음: "+clip(h))
			continue
		}
		if !validIPv4(ip) {
			warns = append(warns, lineNo(n)+"IPv4 가 아님: "+clip(ip))
			continue
		}
		e := ipEntry{Host: h, IP: ip, GW: ipGateway(ip)}
		if i, dup := idx[h]; dup {
			warns = append(warns, lineNo(n)+h+" 중복 — 마지막 값 "+ip+" 사용 (이전 "+entries[i].IP+")")
			entries[i] = e
			continue
		}
		idx[h] = len(entries)
		entries = append(entries, e)
	}
	for _, e := range entries {
		if o, ok := byIP[e.IP]; ok {
			warns = append(warns, "새 IP "+e.IP+" 가 "+o+" 와 "+e.Host+" 에 겹침")
			continue
		}
		byIP[e.IP] = e.Host
	}
	return
}

// parseHostInput: 호스트 목록 (normalizeHosts 와 같은 구분) + hostname 검증
func parseHostInput(raw string) (hosts, warns []string) {
	for _, h := range normalizeHosts(raw) {
		if strings.HasPrefix(h, "#") {
			continue
		}
		if !hostRe.MatchString(h) {
			warns = append(warns, "hostname 이 올바르지 않아 제외: "+clip(h))
			continue
		}
		hosts = append(hosts, h)
	}
	return
}

func lineNo(n int) string { return strconv.Itoa(n+1) + "번째 줄: " }

// clip: 화면·기록에 옮길 입력·출력 조각 (제어문자 제거, 아주 긴 값만 줄임 — 폭 맞춤은 화면이 줄바꿈으로 한다)
func clip(s string) string {
	s = cleanText(s)
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + " (이하 생략)"
	}
	return s
}

// validIPv4: 점 4개 10진수 (0~255, 앞자리 0 금지)
func validIPv4(s string) bool {
	p := strings.Split(s, ".")
	if len(p) != 4 {
		return false
	}
	for _, x := range p {
		if x == "" || len(x) > 3 || (len(x) > 1 && x[0] == '0') {
			return false
		}
		for _, r := range x {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return net.ParseIP(s).To4() != nil
}

// ipGateway: 엔진과 같은 규칙 — 새 IP 의 마지막 옥텟을 1 로
func ipGateway(ip string) string {
	if i := strings.LastIndexByte(ip, '.'); i > 0 {
		return ip[:i] + ".1"
	}
	return ""
}

// ---- ldap_config.conf: infra 목록 · 비밀값 ----

// ldapInfraNames: infra.<이름>.<키> 의 이름 집합 (정렬)
func ldapInfraNames(path string) ([]string, error) {
	kv, err := readFlatConf(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for k := range kv {
		p := strings.Split(k, ".")
		if len(p) >= 3 && p[0] == "infra" && p[1] != "" && !seen[p[1]] {
			seen[p[1]] = true
			out = append(out, p[1])
		}
	}
	sort.Strings(out)
	return out, nil
}

// readFlatConf: key=value (빈 줄·# 무시). 엔진의 config.Load 와 같은 해석.
func readFlatConf(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	kv := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if k, v, ok := strings.Cut(s, "="); ok {
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return kv, sc.Err()
}

// secretMask: 엔진 출력·기록에 들어갈 문자열의 비밀값 마스킹.
// ① ldap_config.conf 의 비밀 키(bindpw·authtok·password·passwd·secret 포함 키) 값 그대로 → ****
// ② 키 패턴(bindpw / *authtok / password / passwd / secret 다음의 값) → ****  (노드의 기존 값처럼 conf 에 없는 값도 가림)
type secretMask struct{ vals []string }

var secretKeyRe = regexp.MustCompile(`(?i)([A-Za-z_]*(?:bindpw|authtok|password|passwd|secret))(\s*[=:]\s*|\s+)("[^"]*"|'[^']*'|[^\s"',;|]+)`)

func loadSecretMask(path string) *secretMask {
	m := &secretMask{}
	kv, err := readFlatConf(path)
	if err != nil {
		return m
	}
	seen := map[string]bool{}
	for k, v := range kv {
		lk := strings.ToLower(k)
		if !(strings.Contains(lk, "bindpw") || strings.Contains(lk, "authtok") || strings.Contains(lk, "password") ||
			strings.Contains(lk, "passwd") || strings.Contains(lk, "secret")) {
			continue
		}
		for _, x := range []string{v, strings.Trim(v, `"'`)} {
			if x != "" && !seen[x] {
				seen[x] = true
				m.vals = append(m.vals, x)
			}
		}
	}
	sort.Slice(m.vals, func(a, b int) bool { return len(m.vals[a]) > len(m.vals[b]) })
	return m
}

func (m *secretMask) apply(s string) string {
	if m != nil {
		for _, v := range m.vals {
			s = strings.ReplaceAll(s, v, "****")
		}
	}
	return secretKeyRe.ReplaceAllString(s, "${1}${2}****")
}

// ---- assets.txt: 기록용 site 조회 (엔진과 같은 규칙: 이름 그대로 → ev01~03 접미사 제거) ----

func assetSites(path string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimRight(l, " \t\r")
		if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		if h, s, ok := strings.Cut(l, "\t"); ok && strings.TrimSpace(h) != "" {
			out[strings.TrimSpace(h)] = strings.TrimSpace(s)
		}
	}
	return out
}

func siteOf(sites map[string]string, h string) string {
	if s, ok := sites[h]; ok {
		return s
	}
	for _, suf := range []string{"ev01", "ev02", "ev03"} {
		if b, ok := strings.CutSuffix(h, suf); ok && b != "" {
			if s, ok := sites[b]; ok {
				return s
			}
		}
	}
	return ""
}
