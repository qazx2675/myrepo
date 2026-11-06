package main

// stress-test -inspect: AWX 템플릿·인벤토리 조사 (읽기 전용 — GET 만 쓰고 PATCH/POST 는 하지 않는다)
//
//	stress-test -inspect [-user <u> | -conf <파일>] [템플릿 ID/이름 ...]
//
// 종료코드: 0 정상, 1 점검 항목 불일치·경고, 2 설정·접속·API 오류.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// surveyQ 는 템플릿 survey 질문 하나다.
type surveyQ struct {
	Variable     string      `json:"variable"`
	QuestionName string      `json:"question_name"`
	Type         string      `json:"type"`
	Required     bool        `json:"required"`
	Default      interface{} `json:"default"`
	Choices      interface{} `json:"choices"` // 줄바꿈으로 이은 문자열 또는 배열
	Min          interface{} `json:"min"`
	Max          interface{} `json:"max"`
}

// invInfo 는 인벤토리 목록의 한 항목이다.
type invInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// surveySpec 은 템플릿 survey 질문 전체를 돌려준다. survey 가 없으면 빈 목록.
func (c *awxClient) surveySpec(tmplID int) ([]surveyQ, error) {
	var s struct {
		Spec []surveyQ `json:"spec"`
	}
	if err := c.do(http.MethodGet, fmt.Sprintf("/api/v2/job_templates/%d/survey_spec/", tmplID), nil, &s); err != nil {
		return nil, err
	}
	return s.Spec, nil
}

// listInventories 는 인벤토리 전체를 id 순으로 돌려준다.
func (c *awxClient) listInventories() ([]invInfo, error) {
	var l struct {
		Results []invInfo `json:"results"`
	}
	if err := c.do(http.MethodGet, "/api/v2/inventories/?page_size=200&order_by=id", nil, &l); err != nil {
		return nil, err
	}
	sort.Slice(l.Results, func(i, j int) bool { return l.Results[i].ID < l.Results[j].ID })
	return l.Results, nil
}

// inventorySources 는 인벤토리의 소스 목록(id 오름차순, 필드 전체)이다.
func (c *awxClient) inventorySources(invID int) ([]map[string]interface{}, error) {
	var l struct {
		Results []map[string]interface{} `json:"results"`
	}
	if err := c.do(http.MethodGet, fmt.Sprintf("/api/v2/inventories/%d/inventory_sources/?page_size=200&order_by=id", invID), nil, &l); err != nil {
		return nil, err
	}
	return l.Results, nil
}

// getSource 는 인벤토리 소스 하나(필드 전체)다.
func (c *awxClient) getSource(id int) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := c.do(http.MethodGet, fmt.Sprintf("/api/v2/inventory_sources/%d/", id), nil, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// 점검 결과 종류
const (
	fOK   = iota // 정상
	fOKY         // 통과지만 확인 필요 (노란 체크, 종료코드에 영향 없음)
	fInfo        // 참고
	fNote        // 경고 표시만 (종료코드에 영향 없음)
	fWarn        // 경고 (종료코드 1)
	fBad         // 불일치 (종료코드 1)
)

type finding struct {
	kind int
	msg  string
}

// valText 는 survey 값(default/min/max 등)을 한 줄 글자로 만든다.
func valText(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.ReplaceAll(x, "\n", `\n`)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case []interface{}:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = valText(e)
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprint(v)
}

// choicesText 는 choices 를 쉼표로 이은 글자로 만든다 (줄바꿈 문자열·배열 모두).
func choicesText(v interface{}) string {
	if s, ok := v.(string); ok {
		var parts []string
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				parts = append(parts, l)
			}
		}
		return strings.Join(parts, ",")
	}
	return valText(v)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	if r := []rune(s); len(r) > 30 {
		return string(r[:30]) + "…"
	}
	return s
}

// surveyRows 는 survey 변수 표의 행이다.
func surveyRows(spec []surveyQ) [][]string {
	var rows [][]string
	for _, q := range spec {
		mm := "-"
		if lo, hi := valText(q.Min), valText(q.Max); lo != "" || hi != "" {
			mm = lo + "/" + hi
		}
		rows = append(rows, []string{q.Variable, dash(q.QuestionName), dash(q.Type), strconv.FormatBool(q.Required), dash(valText(q.Default)), dash(choicesText(q.Choices)), mm})
	}
	return rows
}

// endKeyHint 는 종료 시각 survey 변수가 어떤 형태로 전달되는지 설명한다.
func endKeyHint(typ string) string {
	switch typ {
	case "integer":
		return "숫자로 보냄"
	case "float":
		return "숫자(실수)로 보냄"
	}
	return "입력한 글자 그대로 보냄"
}

// checkKeys 는 conf 의 키가 survey 에 있는지, 필수 survey 항목이 conf 로 채워지는지 점검한다.
// role 은 cpu / stress. endKeys 는 stress 에서만 쓴다.
func checkKeys(role string, tc TemplateConf, endKeys []string, surveyOn bool, spec []surveyQ) []finding {
	byVar := map[string]surveyQ{}
	for _, q := range spec {
		byVar[q.Variable] = q
	}
	covered := map[string]bool{}
	var fs []finding
	add := func(k int, format string, a ...interface{}) { fs = append(fs, finding{k, fmt.Sprintf(format, a...)}) }

	keys := 0
	look := func(label, key string, end bool) {
		keys++
		covered[key] = true
		q, ok := byVar[key]
		switch {
		case !ok && !end:
			add(fBad, "%s = %s 가 survey 에 없습니다 — hostname 목록이 필요 없으면 conf 에서 %s 값을 비우세요(인벤토리 전체 대상)", label, key, label)
		case !ok:
			add(fBad, "%s = %s 가 survey 에 없습니다", label, key)
		case end:
			add(fOK, "%s %s: survey type %s → %s", label, key, q.Type, endKeyHint(q.Type))
		default:
			add(fOK, "%s = %s: survey 에 있음 (type %s)", label, key, q.Type)
		}
	}
	if tc.HostnameKey == "" {
		add(fInfo, "%s_hostname_key 가 비어 있음 — hostname 목록을 넘기지 않음 (인벤토리 전체 대상)", role)
	} else {
		look(role+"_hostname_key", tc.HostnameKey, false)
	}
	if role == stageStress {
		for _, k := range endKeys {
			look("stress_end_keys", k, true)
		}
	}
	kv, _ := parseKeyValues(tc.ExtraVars)
	for k := range kv {
		covered[k] = true
	}
	if !surveyOn && keys > 0 {
		add(fWarn, "survey 가 꺼져 있습니다(survey_enabled=false) — conf 의 키를 launch 때 받지 못할 수 있습니다")
	}
	for _, q := range spec {
		if q.Required && !covered[q.Variable] {
			def := ""
			if d := valText(q.Default); d != "" {
				def = fmt.Sprintf(" (survey 기본값 %s)", dash(d))
			}
			add(fWarn, "필수 survey 항목이 conf 에 없음: %s%s — %s_extra_vars 에 %s=<값> 으로 넣으세요", q.Variable, def, role, q.Variable)
		}
	}
	return fs
}

// hostCandidates 는 hostname 목록을 받을 만한 survey 변수(이름에 host 가 들어간 것)다.
func hostCandidates(spec []surveyQ) []string {
	var res []string
	for _, q := range spec {
		if strings.Contains(strings.ToLower(q.Variable), "host") {
			res = append(res, q.Variable)
		}
	}
	return res
}

// endRank 는 종료 시각 변수 이름의 순서(연0 월1 일2 시3 분4, 모르면 -1)다.
func endRank(name string) int {
	n := strings.ToLower(name)
	for i, w := range []string{"year", "month", "day", "hour", "min"} {
		if strings.Contains(n, w) {
			return i
		}
	}
	return -1
}

// endCandidates 는 이름이 end_ 로 시작하는 survey 변수다. 5개이고 연월일시분이 모두 구분되면 그 순서로 정렬한다.
func endCandidates(spec []surveyQ) []string {
	var res []string
	for _, q := range spec {
		if strings.HasPrefix(q.Variable, "end_") {
			res = append(res, q.Variable)
		}
	}
	if len(res) == 5 {
		seen := map[int]bool{}
		for _, v := range res {
			seen[endRank(v)] = true
		}
		if len(seen) == 5 && !seen[-1] {
			sort.SliceStable(res, func(i, j int) bool { return endRank(res[i]) < endRank(res[j]) })
		}
	}
	return res
}

// suggestLines 는 conf 에 넣을 줄 예시다.
func suggestLines(role, tmplName string, spec []surveyQ) []string {
	lines := []string{fmt.Sprintf("%s_template = %s", role, tmplName)}
	switch hc := hostCandidates(spec); len(hc) {
	case 0:
		lines = append(lines, fmt.Sprintf("# %s_hostname_key =   (host 가 들어간 survey 변수가 없음 — 비우면 hostname 목록을 넘기지 않음)", role))
	case 1:
		lines = append(lines, fmt.Sprintf("%s_hostname_key = %s", role, hc[0]))
	default:
		lines = append(lines, fmt.Sprintf("%s_hostname_key = %s   # 후보: %s", role, hc[0], strings.Join(hc, ", ")))
	}
	if role == stageStress {
		switch ec := endCandidates(spec); {
		case len(ec) == 0:
			lines = append(lines, "# stress_end_keys =   (end_ 로 시작하는 survey 변수가 없음)")
		case len(ec) == 5:
			lines = append(lines, "stress_end_keys = "+strings.Join(ec, ","))
		default:
			lines = append(lines, fmt.Sprintf("stress_end_keys = %s   # 5개여야 함 (end_ 변수 %d개 검출)", strings.Join(ec, ","), len(ec)))
		}
	}
	return lines
}

// templateRoles 는 템플릿이 conf 의 cpu_template / stress_template 인지 판정한다.
func templateRoles(cfg *Config, t *jobTemplate) []string {
	var roles []string
	for _, r := range []struct {
		role string
		tc   TemplateConf
	}{{stageCPU, cfg.CPU}, {stageStress, cfg.Stress}} {
		if r.tc.Template != "" && (r.tc.Template == strconv.Itoa(t.ID) || r.tc.Template == t.Name) {
			roles = append(roles, r.role)
		}
	}
	return roles
}

func (c *Config) templateConfFor(role string) TemplateConf {
	if role == stageStress {
		return c.Stress
	}
	return c.CPU
}

// inspector 는 -inspect 한 번의 상태다.
type inspector struct {
	w      io.Writer
	cfg    *Config
	c      *awxClient
	bad    bool    // 불일치·경고 있음 → 종료코드 1
	user   invInfo // username 인벤토리 (matchUserInventory)
	userEx bool    // 이름이 username 과 정확히 같음
	apiErr bool    // 템플릿 조회 등 API 오류 → 종료코드 2
}

func (n *inspector) pf(format string, a ...interface{}) { fmt.Fprintf(n.w, format+"\n", a...) }

func (n *inspector) show(indent string, fs []finding) {
	for _, f := range fs {
		var m string
		switch f.kind {
		case fOK:
			m = markOK
		case fOKY:
			m = markOKY
		case fInfo:
			m = markInfo
		case fNote, fWarn:
			m = markWarn
		default:
			m = markFail
		}
		if f.kind >= fWarn {
			n.bad = true
		}
		n.pf("%s%s %s", indent, m, f.msg)
	}
}

func (n *inspector) fail(format string, a ...interface{}) {
	n.apiErr = true
	n.pf("%s "+format, append([]interface{}{markFail}, a...)...)
}

func indentLines(s, ind string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString(ind + strings.TrimRight(l, " ") + "\n")
	}
	return b.String()
}

// template 은 템플릿 하나를 조사하고 conf 제안 줄을 돌려준다.
func (n *inspector) template(arg string) []string {
	t0, err := n.c.resolveTemplate(arg)
	if err != nil {
		n.pf("\n===== 템플릿 %s =====", arg)
		n.fail("템플릿 조회 실패: %v", err)
		return nil
	}
	t, err := n.c.getTemplate(t0.ID) // 목록 응답에는 없는 필드가 있어 다시 조회
	if err != nil {
		n.pf("\n===== 템플릿 %s =====", arg)
		n.fail("템플릿 조회 실패: %v", err)
		return nil
	}
	roles := templateRoles(n.cfg, t)
	label := "conf 의 cpu_template / stress_template 아님"
	if len(roles) > 0 {
		label = "conf: " + strings.Join(roles, ",") + "_template"
	}
	n.pf("\n===== 템플릿 %s (id %d)  [%s] =====", t.Name, t.ID, label)

	def := n.cfg.DefaultInventory
	switch {
	case len(roles) == 1 && roles[0] == stageStress:
		// stress-ng 템플릿은 인벤토리를 바꾸지 않고 개인 인벤토리를 그대로 쓴다
		msg := fmt.Sprintf("현재 인벤토리: %s (id %d) — stress-ng 템플릿은 인벤토리를 바꾸지 않음", t.invName(), t.Inventory)
		switch {
		case n.user.ID != 0 && t.Inventory == n.user.ID && n.userEx:
			n.show("  ", []finding{{fOK, msg + ", username 인벤토리"}})
		case n.user.ID != 0 && t.Inventory == n.user.ID:
			n.show("  ", []finding{{fOKY, msg + fmt.Sprintf(", 이름에 username(%s)이 들어간 인벤토리", n.cfg.Username)}})
		default:
			n.show("  ", []finding{{fNote, msg + fmt.Sprintf(" — username(%s) 인벤토리가 아님: 등록한 호스트가 실행 대상에 없을 수 있음", n.cfg.Username)}})
		}
	case t.invName() == def:
		n.show("  ", []finding{{fOK, fmt.Sprintf("현재 인벤토리: %s (id %d) — default_inventory 와 같음", t.invName(), t.Inventory)}})
	default:
		n.show("  ", []finding{{fWarn, fmt.Sprintf("현재 인벤토리: %s (id %d) — default_inventory(%s) 가 아님: 다른 사용자가 사용 중일 수 있음", t.invName(), t.Inventory, def)}})
	}
	n.pf("  ask_inventory_on_launch: %v", t.AskInventory)
	spec, err := n.c.surveySpec(t.ID)
	if err != nil {
		n.fail("survey 조회 실패: %v", err)
		return nil
	}
	if t.SurveyEnabled {
		n.pf("  survey: 사용 (변수 %d개)", len(spec))
	} else {
		n.pf("  survey: 사용 안 함 (변수 %d개 정의됨)", len(spec))
	}
	if len(spec) > 0 {
		n.pf("%s", strings.TrimRight(indentLines(fmtTable([]string{"variable", "question_name", "type", "required", "default", "choices", "min/max"}, surveyRows(spec)), "    "), "\n"))
	}
	if ev := strings.TrimSpace(t.ExtraVars); ev != "" && ev != "{}" && ev != "---" {
		n.pf("  템플릿 extra_vars:\n%s", strings.TrimRight(indentLines(ev, "    "), "\n"))
	}

	var sug []string
	if len(roles) == 0 {
		n.pf("  (conf 템플릿이 아니어서 키 점검·제안은 생략)")
		return nil
	}
	n.pf("  conf 키 점검:")
	for _, r := range roles {
		n.show("    ", checkKeys(r, n.cfg.templateConfFor(r), n.cfg.StressEndKeys, t.SurveyEnabled, spec))
		sug = append(sug, suggestLines(r, t.Name, spec)...)
	}
	return sug
}

// inventories 는 인벤토리 목록과 default_inventory / username 존재 여부를 점검하고 username 인벤토리 ID 를 돌려준다.
func (n *inspector) inventories(invs []invInfo) (userID int) {
	n.pf("\n===== 인벤토리 =====")
	var rows [][]string
	defFound := false
	for _, v := range invs {
		var marks []string
		if v.Name == n.cfg.DefaultInventory {
			marks = append(marks, "default_inventory")
			defFound = true
		}
		if n.user.ID != 0 && v.ID == n.user.ID {
			marks = append(marks, "username")
		}
		rows = append(rows, []string{strconv.Itoa(v.ID), v.Name, strings.Join(marks, ", ")})
	}
	n.pf("%s", strings.TrimRight(indentLines(fmtTable([]string{"id", "name", "conf"}, rows), "  "), "\n"))
	if defFound {
		n.show("  ", []finding{{fOK, fmt.Sprintf("default_inventory(%s) 인벤토리가 있음", n.cfg.DefaultInventory)}})
	} else {
		n.show("  ", []finding{{fBad, fmt.Sprintf("default_inventory(%s) 인벤토리가 AWX 에 없음", n.cfg.DefaultInventory)}})
	}
	userID = n.user.ID
	_, _, cands := matchUserInventory(invs, n.cfg.Username)
	switch {
	case userID != 0 && n.userEx:
		n.show("  ", []finding{{fOK, fmt.Sprintf("username(%s) 인벤토리가 있음 (id %d) — CPU 템플릿 실행 때 붙일 인벤토리", n.cfg.Username, userID)}})
	case userID != 0:
		n.show("  ", []finding{{fOKY, fmt.Sprintf("username(%s) 이 이름에 들어간 인벤토리 %s (id %d) 를 씀 — CPU 템플릿 실행 때 붙일 인벤토리", n.cfg.Username, n.user.Name, userID)}})
	case len(cands) > 1:
		n.show("  ", []finding{{fBad, fmt.Sprintf("이름에 username(%s)이 들어간 인벤토리가 여러 개라 고를 수 없음: %s", n.cfg.Username, strings.Join(cands, ", "))}})
	default:
		n.show("  ", []finding{{fBad, fmt.Sprintf("username(%s) 이거나 이름에 그것이 들어간 인벤토리가 AWX 에 없음 — CPU 템플릿 실행 때 바꿀 인벤토리", n.cfg.Username)}})
	}
	return userID
}

// sources 는 conf inventory(또는 inventory_source 가 속한 인벤토리)의 소스 목록을 보이고 username 인벤토리와 같은지 본다.
func (n *inspector) sources(invs []invInfo, userID int) {
	n.pf("\n===== 인벤토리 소스 (동기화 대상) =====")
	cfg := n.cfg
	fixed := 0
	invID := 0
	if cfg.InventorySource != "" {
		id, err := strconv.Atoi(cfg.InventorySource)
		if err != nil {
			n.show("  ", []finding{{fBad, fmt.Sprintf("inventory_source(%s)는 숫자 ID여야 합니다", cfg.InventorySource)}})
			return
		}
		fixed = id
		s, err := n.c.getSource(id)
		if err != nil {
			n.show("  ", []finding{{fBad, fmt.Sprintf("inventory_source(%d) 조회 실패: %v", id, err)}})
			return
		}
		invID = numField(s, "inventory")
	} else {
		id, err := strconv.Atoi(cfg.Inventory)
		if err != nil {
			n.show("  ", []finding{{fBad, fmt.Sprintf("inventory(%s)는 숫자 ID여야 합니다", cfg.Inventory)}})
			return
		}
		invID = id
	}
	if invID == 0 {
		n.show("  ", []finding{{fBad, "동기화 인벤토리를 알 수 없습니다"}})
		return
	}
	invName := fmt.Sprintf("id %d", invID)
	for _, v := range invs {
		if v.ID == invID {
			invName = fmt.Sprintf("%s (id %d)", v.Name, v.ID)
		}
	}
	n.pf("  동기화 인벤토리: %s", invName)
	list, err := n.c.inventorySources(invID)
	if err != nil {
		n.show("  ", []finding{{fBad, fmt.Sprintf("소스 목록 조회 실패: %v", err)}})
		return
	}
	if len(list) == 0 {
		n.show("  ", []finding{{fBad, fmt.Sprintf("인벤토리(%d)에 등록된 소스가 없음", invID)}})
		return
	}
	used := fixed
	if used == 0 {
		used = numField(list[0], "id")
	}
	var rows [][]string
	var usedSrc map[string]interface{}
	for _, s := range list {
		id := numField(s, "id")
		mark := ""
		if id == used {
			mark = "사용"
			usedSrc = s
		}
		rows = append(rows, []string{strconv.Itoa(id), dash(valText(s["name"])), dash(valText(s["source"])), dash(valText(s[cfg.SourceField])), dash(valText(s["overwrite"])), dash(valText(s["overwrite_vars"])), mark})
	}
	n.pf("%s", strings.TrimRight(indentLines(fmtTable([]string{"id", "name", "source", cfg.SourceField, "overwrite", "overwrite_vars", "사용"}, rows), "  "), "\n"))
	how := "inventory 의 첫 소스"
	if fixed != 0 {
		how = "inventory_source 로 고정"
	}
	switch {
	case usedSrc == nil:
		n.show("  ", []finding{{fBad, fmt.Sprintf("inventory_source(%d) 가 이 인벤토리의 소스 목록에 없음", used)}})
	default:
		if _, ok := usedSrc[cfg.SourceField]; !ok {
			n.show("  ", []finding{{fBad, fmt.Sprintf("사용할 소스(%d)에 source_field(%s) 필드가 없음", used, cfg.SourceField)}})
		} else {
			n.show("  ", []finding{{fOK, fmt.Sprintf("사용할 소스: %d (%s), 저장 필드 %s", used, how, cfg.SourceField)}})
		}
	}
	if userID != 0 {
		if invID == userID {
			n.show("  ", []finding{{fOK, fmt.Sprintf("동기화 인벤토리와 username(%s) 인벤토리가 같음", cfg.Username)}})
		} else {
			n.show("  ", []finding{{fNote, fmt.Sprintf("동기화 인벤토리(inventory=%d)와 실행 때 템플릿에 붙일 인벤토리(username=%s, id %d)가 다릅니다 — 등록한 호스트가 실행 대상에 없을 수 있음", invID, cfg.Username, userID)}})
		}
	}
}

// inspectRun 은 -inspect 본체다. 출력은 w, 종료코드는 0/1/2.
func inspectRun(w io.Writer, cfg *Config, client *awxClient, args []string) int {
	n := &inspector{w: w, cfg: cfg, c: client}
	if len(args) == 0 {
		for _, t := range []string{cfg.CPU.Template, cfg.Stress.Template} {
			if t != "" {
				args = append(args, t)
			}
		}
	}
	if len(args) == 0 {
		n.pf("%s 조사할 템플릿이 없습니다 — 인자로 템플릿 ID/이름을 주거나 conf 에 cpu_template / stress_template 을 넣으세요", markFail)
		return 2
	}
	invs, err := client.listInventories()
	if err != nil {
		n.pf("%s AWX 접속·조회 실패 (%s): %v", markFail, cfg.AWXURL, err)
		return 2
	}
	n.user, n.userEx, _ = matchUserInventory(invs, cfg.Username)
	var sug []string
	for _, a := range args {
		sug = append(sug, n.template(a)...)
	}
	userID := n.inventories(invs)
	n.sources(invs, userID)
	n.pf("\n===== conf 에 넣을 줄 (예시 — survey 에서 찾은 값) =====")
	if len(sug) == 0 {
		n.pf("  (제안할 줄 없음)")
	}
	for _, l := range sug {
		n.pf("  %s", l)
	}
	switch {
	case n.apiErr:
		n.pf("\n%s 조사 중 오류가 있었습니다 (종료코드 2)", markFail)
		return 2
	case n.bad:
		n.pf("\n%s 점검 항목에 불일치·경고가 있습니다 (종료코드 1)", markWarn)
		return 1
	}
	n.pf("\n%s 모든 점검 통과 (종료코드 0)", markOK)
	return 0
}

// runInspect 는 conf 를 찾아 읽고 AWX 접속 정보만 확인한 뒤 inspectRun 을 부른다.
func runInspect(userFlag, confFlag string, args []string) int {
	user := resolveUser(userFlag)
	if confFlag == "" && (user == "" || !userRe.MatchString(user)) {
		return fail2("사용자를 정할 수 없거나 형식이 올바르지 않습니다 (%q). -user, STRESS_USER 또는 -conf 를 확인하세요", user)
	}
	confPath, err := resolveConfPath(confFlag, user)
	if err != nil {
		return fail2("%v", err)
	}
	cfg, err := loadConfig(confPath)
	if err != nil {
		return fail2("%v", err)
	}
	var miss []string
	for _, k := range []struct{ name, v string }{{"awx_url", cfg.AWXURL}, {"username", cfg.Username}, {"password", cfg.Password}} {
		if strings.TrimSpace(k.v) == "" {
			miss = append(miss, k.name)
		}
	}
	if len(miss) > 0 {
		return fail2("설정 파일(%s)에 비어 있는 값이 있습니다: %s", confPath, strings.Join(miss, ", "))
	}
	if isTerminal(os.Stdout) {
		setColor()
	}
	fmt.Fprintf(os.Stdout, "%s -inspect (읽기 전용)  설정=%s  AWX=%s  username=%s  default_inventory=%s\n", markInfo, confPath, cfg.AWXURL, cfg.Username, cfg.DefaultInventory)
	return inspectRun(os.Stdout, cfg, newAWXClient(cfg.AWXURL, cfg.Username, cfg.Password, cfg.InsecureTLS, 10*time.Second), args)
}

// numField 는 JSON 객체의 숫자 필드를 int 로 읽는다 (없으면 0).
func numField(m map[string]interface{}, key string) int {
	f, _ := m[key].(float64)
	return int(f)
}
