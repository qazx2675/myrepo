// newhost.go - v0.8.0 신규 설치 양식(w 메뉴 2번 / CLI auto_setup newhost)의 순수 로직:
// 붙여넣기 줄 해석(hostname ip mac, 순서·구분자 무관, MAC 4형식 → 소문자 aa:bb:..), 목록 내 중복 검사,
// 템플릿 목록(conf newhost_template_1~9 + user 별 templates 파일)과 `+` 추가 저장, 지정 미니 언어
// (`svr001-020=1  spice*=mec  1-20=2  *=A`, 열 수정 `svr005 os=9.6`), 이력 추천, 12필드 줄 생성,
// 저장(기존 파일 백업 → 임시 파일 + rename, 이력 사본), AWX 실행 환경변수(프로파일을 고르면 nodeinfo N, 안 고르면 자동 응답 없음).
// 화면·키는 newhost_tui.go.
//
// 저장 위치: <AUTO_SETUP_DIR>/newhost/<user>/{templates, backup/<시각>.txt, history/<시각>.txt} (디렉터리 0700, 파일 0600).
// 01 의 후처리(1.1T→1200 sed, fix_mac 짝수 보정, infra 별칭)는 01 이 하므로 여기서는 하지 않는다.
package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const (
	newhostRemoteMsg = "신규 설치 양식은 os8_mgmt 에서 실행하세요"
	newhostNoDirMsg  = "신규 설치 양식: awx_dir 가 비었거나 없습니다 (/etc/auto_setup/auto_setup.conf)"
	newhostRangeMax  = 100000 // 범위 지정(svr001-020) 한 번에 만들 수 있는 이름 수
)

// 추천용 이력 읽기 상한 (느려지지 않게, 테스트에서 줄인다)
var (
	newhostHistMaxFiles       = 200       // 이력 파일 수 (최근 수정 순)
	newhostHistMaxBytes int64 = 256 << 10 // 파일 하나에서 읽는 최대 바이트
)

// newhostDirProblem: awx_dir 가 비었거나 디렉터리가 아니면 안내 문구, 정상이면 ""
func newhostDirProblem() string {
	if awx_dir == "" {
		return newhostNoDirMsg
	}
	if fi, err := os.Stat(awx_dir); err != nil || !fi.IsDir() {
		return newhostNoDirMsg
	}
	return ""
}

// newhostUserDir: <stateDir>/newhost/<user>
func newhostUserDir(stateDir, user string) string { return filepath.Join(stateDir, "newhost", user) }

// ---------------------------------------------------------------- 붙여넣기

// nhRow: 붙여넣은 한 줄 (Err = 형식 오류, Dup = 목록 내 중복 — nhCheckDups 가 채움)
type nhRow struct {
	Raw           string
	Host, IP, MAC string
	Err, Dup      string
}

// bad: 진행을 막는 사유 (없으면 "")
func (r nhRow) bad() string {
	if r.Err != "" {
		return r.Err
	}
	return r.Dup
}

var (
	nhIPishRe   = regexp.MustCompile(`^[0-9.]+$`)
	nhMacColRe  = regexp.MustCompile(`^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$`)
	nhMacDashRe = regexp.MustCompile(`^[0-9A-Fa-f]{2}(-[0-9A-Fa-f]{2}){5}$`)
	nhMacDotRe  = regexp.MustCompile(`^[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}\.[0-9A-Fa-f]{4}$`) // 시스코형
	nhMacBareRe = regexp.MustCompile(`^[0-9A-Fa-f]{12}$`)
	nhHostRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// nhNormMAC: MAC 4형식(aa:bb:.., aa-bb-.., aabb.ccdd.eeff, aabbccddeeff) → 소문자 aa:bb:cc:dd:ee:ff.
// bare = 구분자 없는 12자리였는지 (hostname 과 겹칠 수 있는 형식)
func nhNormMAC(s string) (mac string, bare, ok bool) {
	switch {
	case nhMacColRe.MatchString(s), nhMacDashRe.MatchString(s), nhMacDotRe.MatchString(s):
	case nhMacBareRe.MatchString(s):
		bare = true
	default:
		return "", false, false
	}
	h := strings.ToLower(strings.NewReplacer(":", "", "-", "", ".", "").Replace(s))
	parts := make([]string, 0, 6)
	for i := 0; i < 12; i += 2 {
		parts = append(parts, h[i:i+2])
	}
	return strings.Join(parts, ":"), bare, true
}

// nhParseLine: 한 줄 → hostname·ip·mac (구분자 공백·탭·쉼표, 순서 무관). 실패하면 Err 에 사유.
func nhParseLine(line string) nhRow {
	r := nhRow{Raw: strings.TrimSpace(line)}
	tok := strings.FieldsFunc(r.Raw, func(c rune) bool { return unicode.IsSpace(c) || c == ',' })
	if len(tok) != 3 {
		r.Err = fmt.Sprintf("값이 %d개 (hostname ip mac 3개 필요)", len(tok))
		return r
	}
	var ips, hosts []string
	type macTok struct {
		mac  string
		bare bool
		raw  string
	}
	var macs []macTok
	for _, t := range tok {
		if nhIPishRe.MatchString(t) && strings.Contains(t, ".") {
			if ip := net.ParseIP(t); ip == nil || ip.To4() == nil || strings.Count(t, ".") != 3 {
				r.Err = "IP 형식 오류: " + t
				return r
			}
			ips = append(ips, t)
			continue
		}
		if m, bare, ok := nhNormMAC(t); ok {
			macs = append(macs, macTok{m, bare, t})
			continue
		}
		hosts = append(hosts, t)
	}
	// 12자리 hex hostname: MAC 후보가 둘이면 구분자 있는 쪽을 MAC 으로, 구분자 없는 쪽을 hostname 으로
	if len(macs) == 2 && len(hosts) == 0 && macs[0].bare != macs[1].bare {
		if macs[0].bare {
			macs[0], macs[1] = macs[1], macs[0]
		}
		hosts, macs = []string{macs[1].raw}, macs[:1]
	}
	switch {
	case len(ips) == 0:
		r.Err = "IP 가 없음"
	case len(ips) > 1:
		r.Err = "IP 가 2개 이상"
	case len(macs) == 0:
		r.Err = "MAC 이 없음 (형식: aa:bb:cc:dd:ee:ff / aa-bb-.. / aabb.ccdd.eeff / aabbccddeeff)"
	case len(macs) > 1:
		r.Err = "MAC 과 hostname 을 구분할 수 없음 (MAC 에 : 를 넣어 주세요)"
	case len(hosts) != 1:
		r.Err = "hostname 이 없음"
	case !nhHostRe.MatchString(hosts[0]):
		r.Err = "hostname 형식 오류: " + hosts[0]
	}
	if r.Err != "" {
		return r
	}
	r.Host, r.IP, r.MAC = hosts[0], ips[0], macs[0].mac
	return r
}

// nhCheckDups: 형식이 맞는 행끼리 hostname(대소문자 무시)·IP·MAC 중복을 찾아 관련 행 모두의 Dup 에 적는다 (행 번호는 1부터)
func nhCheckDups(rows []nhRow) {
	for i := range rows {
		rows[i].Dup = ""
	}
	kinds := []struct {
		name string
		key  func(r nhRow) string
	}{
		{"hostname", func(r nhRow) string { return strings.ToLower(r.Host) }},
		{"IP", func(r nhRow) string { return r.IP }},
		{"MAC", func(r nhRow) string { return r.MAC }},
	}
	for _, k := range kinds {
		at := map[string][]int{}
		for i, r := range rows {
			if r.Err == "" {
				at[k.key(r)] = append(at[k.key(r)], i)
			}
		}
		for _, idx := range at {
			if len(idx) < 2 {
				continue
			}
			nums := make([]string, len(idx))
			for j, i := range idx {
				nums[j] = strconv.Itoa(i + 1)
			}
			msg := fmt.Sprintf("%s 중복 (행 %s)", k.name, strings.Join(nums, ","))
			for _, i := range idx {
				if rows[i].Dup != "" {
					rows[i].Dup += "; "
				}
				rows[i].Dup += msg
			}
		}
	}
}

// nhRowCmd: 붙여넣기 단계의 고치기 명령 `d 3`, `d 3-5,8`(삭제) / `e 3`(다시 입력). host 줄(값 3개, IP 의 .)과 겹치지 않는다.
var nhRowCmdRe = regexp.MustCompile(`^([dDeE])\s+([0-9][0-9,\s-]*)$`)

// nhParseRowCmd: 명령이면 (종류 'd'/'e', 행 번호들(1부터), 오류). 명령이 아니면 kind=0.
func nhParseRowCmd(line string, n int) (kind byte, nums []int, err error) {
	m := nhRowCmdRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return 0, nil, nil
	}
	kind = strings.ToLower(m[1])[0]
	seen := map[int]bool{}
	for _, part := range strings.FieldsFunc(m[2], func(c rune) bool { return c == ',' || unicode.IsSpace(c) }) {
		a, b, ok := nhNumRange(part)
		if !ok {
			return kind, nil, fmt.Errorf("행 번호 %q 를 읽을 수 없음", part)
		}
		for i := a; i <= b; i++ {
			if i < 1 || i > n {
				return kind, nil, fmt.Errorf("행 %d 은 없음 (1~%d)", i, n)
			}
			if !seen[i] {
				seen[i] = true
				nums = append(nums, i)
			}
		}
	}
	if kind == 'e' && len(nums) != 1 {
		return kind, nil, errors.New("e 는 행 하나만 (예: e 3)")
	}
	sort.Ints(nums)
	return kind, nums, nil
}

// nhNumRange: "5" 또는 "3-9" (작은 쪽이 앞) → 범위
func nhNumRange(s string) (a, b int, ok bool) {
	lo, hi := s, s
	if i := strings.IndexByte(s, '-'); i >= 0 {
		lo, hi = s[:i], s[i+1:]
	}
	x, err1 := strconv.Atoi(lo)
	y, err2 := strconv.Atoi(hi)
	if err1 != nil || err2 != nil || x > y || y-x > newhostRangeMax {
		return 0, 0, false
	}
	return x, y, true
}

// ---------------------------------------------------------------- 템플릿

// nhTpl: 화면에 보이는 템플릿 (번호는 목록 순서 +1: conf 1~9 순 → user 파일 순)
type nhTpl struct {
	NewhostTpl
	User bool // user 별 templates 파일에서 읽음
}

// Label: 표·지정 결과에 쓰는 짧은 표시 `3` 또는 `1 [A]`
func nhTplLabel(i int, t nhTpl) string {
	if t.Alias != "" {
		return fmt.Sprintf("%d [%s]", i+1, t.Alias)
	}
	return strconv.Itoa(i + 1)
}

// nhTplConflict: list 에 t 를 더할 수 없는 사유 (별칭이 다른 별칭·자동이름과 겹침, 같은 값이 이미 있음). 없으면 "".
func nhTplConflict(list []nhTpl, t NewhostTpl) string {
	for i, e := range list {
		if t.Alias != "" && strings.EqualFold(e.Alias, t.Alias) {
			return fmt.Sprintf("별칭 %s 이 %d 번과 겹침", t.Alias, i+1)
		}
		if t.Alias != "" && strings.EqualFold(e.Name(), t.Alias) {
			return fmt.Sprintf("별칭 %s 이 %d 번 이름과 같음", t.Alias, i+1)
		}
		if e.Name() == t.Name() {
			return fmt.Sprintf("같은 값의 템플릿이 이미 %d 번에 있음", i+1)
		}
	}
	return ""
}

// loadNewhostTemplates: conf 템플릿 + <stateDir>/newhost/<user>/templates. 잘못된 줄·겹치는 줄은 빼고 사유를 problems 로.
func loadNewhostTemplates(stateDir, user string) (list []nhTpl, problems []string) {
	valid, problems := parseNewhostTemplates()
	for _, t := range valid {
		list = append(list, nhTpl{NewhostTpl: t})
	}
	b, err := os.ReadFile(filepath.Join(newhostUserDir(stateDir, user), "templates"))
	if err != nil {
		return list, problems
	}
	for i, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		t, err := parseNewhostTpl(ln)
		if err == nil {
			if c := nhTplConflict(list, t); c != "" {
				err = errors.New(c)
			}
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("templates %d행: %v", i+1, err))
			continue
		}
		list = append(list, nhTpl{NewhostTpl: t, User: true})
	}
	return list, problems
}

// nhParsePlus: `+ sec bond sda 500 8.10 uefi [별칭]` → 템플릿 (값 검사는 parseNewhostTpl 과 같음)
func nhParsePlus(line string) (NewhostTpl, error) {
	f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "+"))
	if len(f) != 6 && len(f) != 7 {
		return NewhostTpl{}, errors.New("형식: + infra nic disk 용량 os boot [별칭]")
	}
	raw := strings.Join(f[:6], " ")
	if len(f) == 7 {
		raw = f[6] + "|" + raw
	}
	return parseNewhostTpl(raw)
}

// addNewhostTemplate: user 별 templates 파일에 Spec() 한 줄을 덧붙인다 (디렉터리 0700, 파일 0600). 겹치면 거부.
func addNewhostTemplate(stateDir, user string, list []nhTpl, t NewhostTpl) (string, error) {
	if !validChangeUser(user) {
		return "", errors.New("user 이름이 올바르지 않음")
	}
	if c := nhTplConflict(list, t); c != "" {
		return "", errors.New(c)
	}
	dir := newhostUserDir(stateDir, user)
	if err := mkdir0700(filepath.Join(stateDir, "newhost"), dir); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "templates")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(t.Spec() + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(p, 0600)
	}
	return p, err
}

// nhFindTpl: 지정 오른쪽 값(번호·별칭·자동이름, 대소문자 무시) → 목록 위치
func nhFindTpl(list []nhTpl, ref string) (int, bool) {
	if n, err := strconv.Atoi(ref); err == nil {
		if n >= 1 && n <= len(list) {
			return n - 1, true
		}
		return 0, false
	}
	for i, t := range list {
		if t.Alias != "" && strings.EqualFold(t.Alias, ref) {
			return i, true
		}
	}
	for i, t := range list {
		if strings.EqualFold(t.Name(), ref) {
			return i, true
		}
	}
	return 0, false
}

// ---------------------------------------------------------------- 값 · 지정

// 값 출처
const (
	nhSrcNone = iota // 값 없음 (빨강)
	nhSrcTpl         // 템플릿 지정
	nhSrcEdit        // 열 수정 (템플릿·추천 값에서 일부를 바꿈)
	nhSrcRec         // 이력 추천 (노랑)
)

// nhVals: 한 행의 infra nic disk 용량 os boot (+part) 와 출처 표시
type nhVals struct {
	T     NewhostTpl // Alias·No 는 쓰지 않음, 빈 필드 = 아직 없음
	Label string     // "1 [A]" / "수정" / "추천"
	Src   int
	From  string // 추천 근거 hostname
}

// nhComplete: 6개 값이 모두 있고 parseNewhostTpl 규칙에 맞음
func nhComplete(t NewhostTpl) bool {
	for _, v := range []string{t.Infra, t.NIC, t.Disk, t.Size, t.OS, t.Boot} {
		if v == "" {
			return false
		}
	}
	_, err := parseNewhostTpl(t.Spec())
	return err == nil
}

// nhColName: 열 이름(대소문자 무시, 용량=size) → 정규 이름, 아니면 ""
func nhColName(s string) string {
	switch c := strings.ToLower(s); c {
	case "infra", "nic", "disk", "size", "os", "boot", "part":
		return c
	case "용량":
		return "size"
	}
	return ""
}

// nhCheckCol: 열 하나의 값 검사 — parseNewhostTpl 과 같은 규칙을 쓰기 위해 나머지를 정상값으로 채워 검사하고 정규화된 값을 돌려준다
func nhCheckCol(col, val string) (string, error) {
	if col == "part" {
		return "", errors.New("part 는 disk 로 자동 결정됩니다 (sda → sda5, nvme → nvme0n1p8)")
	}
	if val == "" {
		return "", fmt.Errorf("%s 값이 비어 있음", col)
	}
	base := NewhostTpl{Infra: "x", NIC: "x", Disk: "sda", Size: "1", OS: "x", Boot: "uefi"}
	nhSetCol(&base, col, val)
	t, err := parseNewhostTpl(base.Spec())
	if err != nil {
		return "", err
	}
	return nhGetCol(t, col), nil
}

func nhSetCol(t *NewhostTpl, col, val string) {
	switch col {
	case "infra":
		t.Infra = val
	case "nic":
		t.NIC = val
	case "disk":
		t.Disk, t.Part = val, newhostPart(val) // disk 를 바꾸면 part 도 다시 정함
	case "size":
		t.Size = val
	case "os":
		t.OS = val
	case "boot":
		t.Boot = val
	}
}

func nhGetCol(t NewhostTpl, col string) string {
	switch col {
	case "infra":
		return t.Infra
	case "nic":
		return t.NIC
	case "disk":
		return t.Disk
	case "size":
		return t.Size
	case "os":
		return t.OS
	case "boot":
		return t.Boot
	}
	return ""
}

// nhOp: 지정 한 건 — 대상 + (템플릿) + (열 수정들)
type nhOp struct {
	target string
	tpl    int // -1 = 템플릿 지정 없음
	cols   [][2]string
}

var nhHostRangeRe = regexp.MustCompile(`^(.*?)([0-9]+)-(.*?)([0-9]+)$`)

// nhTargets: 대상 표현 → 행 위치들 (순서: 정확한 hostname → 행 번호(범위) → * → 글롭 → hostname 범위(자릿수 보존))
func nhTargets(rows []nhRow, target string) ([]int, error) {
	low := strings.ToLower(target)
	var out []int
	for i, r := range rows {
		if strings.ToLower(r.Host) == low {
			out = append(out, i)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	if a, b, ok := nhNumRange(target); ok && strings.Trim(target, "0123456789-") == "" {
		for i := a; i <= b && i <= len(rows); i++ {
			if i >= 1 {
				out = append(out, i-1)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("행 %s 은 없음 (1~%d)", target, len(rows))
		}
		return out, nil
	}
	if target == "*" {
		for i := range rows {
			out = append(out, i)
		}
		return out, nil
	}
	if strings.ContainsAny(target, "*?[") {
		if _, err := path.Match(low, ""); err != nil {
			return nil, fmt.Errorf("패턴 %q 오류", target)
		}
		for i, r := range rows {
			if ok, _ := path.Match(low, strings.ToLower(r.Host)); ok {
				out = append(out, i)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%q 에 맞는 행이 없음", target)
		}
		return out, nil
	}
	if m := nhHostRangeRe.FindStringSubmatch(low); m != nil && (m[3] == "" || m[3] == m[1]) {
		a, err1 := strconv.Atoi(m[2])
		b, err2 := strconv.Atoi(m[4])
		if err1 != nil || err2 != nil || len(m[2]) > 9 || len(m[4]) > 9 {
			return nil, fmt.Errorf("범위 %q 를 읽을 수 없음", target)
		}
		if a > b {
			return nil, fmt.Errorf("범위 %q 의 앞 번호가 더 큼", target)
		}
		if b-a+1 > newhostRangeMax {
			return nil, fmt.Errorf("범위 %q 가 너무 큼", target)
		}
		want := map[string]bool{}
		for n := a; n <= b; n++ {
			want[m[1]+fmt.Sprintf("%0*d", len(m[2]), n)] = true
		}
		for i, r := range rows {
			if want[strings.ToLower(r.Host)] {
				out = append(out, i)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%q 에 맞는 행이 없음", target)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%q 에 맞는 행이 없음", target)
}

// nhParseAssign: 지정 한 줄 → 지정 목록 (모두 검사한 뒤에만 적용하도록 먼저 파싱만 한다)
func nhParseAssign(line string, tpls []nhTpl) ([]nhOp, error) {
	var ops []nhOp
	cur := -1
	for _, tok := range strings.Fields(line) {
		i := strings.IndexByte(tok, '=')
		switch {
		case i == 0:
			return nil, fmt.Errorf("%q: = 앞에 대상이 필요", tok)
		case i < 0:
			ops = append(ops, nhOp{target: tok, tpl: -1})
			cur = len(ops) - 1
		default:
			left, right := tok[:i], tok[i+1:]
			if col := nhColName(left); col != "" {
				if cur < 0 {
					return nil, fmt.Errorf("%q: 열 수정 앞에 대상이 필요 (예: svr005 os=9.6)", tok)
				}
				v, err := nhCheckCol(col, right)
				if err != nil {
					return nil, fmt.Errorf("%s: %v", tok, err)
				}
				ops[cur].cols = append(ops[cur].cols, [2]string{col, v})
				continue
			}
			idx, ok := nhFindTpl(tpls, right)
			if !ok {
				return nil, fmt.Errorf("%q: 템플릿 %q 없음 (번호·별칭·이름)", tok, right)
			}
			ops = append(ops, nhOp{target: left, tpl: idx})
			cur = len(ops) - 1
		}
	}
	for _, op := range ops {
		if op.tpl < 0 && len(op.cols) == 0 {
			return nil, fmt.Errorf("%q: 지정할 템플릿(=번호) 또는 열(os=…)이 없음", op.target)
		}
	}
	return ops, nil
}

// ---------------------------------------------------------------- 이력 추천

// nhHist: 이력 한 줄에서 얻은 값
type nhHist struct {
	Host string
	T    NewhostTpl
}

// nhHistIndex: hostname → 가장 최근 값, 접두어 → 가장 최근 값
type nhHistIndex struct {
	byHost map[string]nhHist
	byPfx  map[string]nhHist
}

// nhPrefix: hostname 의 숫자 앞까지(소문자) — svr0012 → svr, spice-a01 → spice-a
func nhPrefix(h string) string {
	h = strings.ToLower(h)
	if i := strings.IndexAny(h, "0123456789"); i >= 0 {
		return h[:i]
	}
	return h
}

func nhAlnum(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }

// newhostHistFiles: 추천에 쓰는 이력 파일 (최근 수정 순, 상한 newhostHistMaxFiles)
//   - <stateDir>/newhost/*/history/*.txt, <stateDir>/newhost/*/backup/*.txt (모든 user)
//   - awxDir/*.txt (12필드 줄만 쓰인다)
func newhostHistFiles(stateDir, awxDir string) []string {
	var pats []string
	pats = append(pats, filepath.Join(stateDir, "newhost", "*", "history", "*.txt"), filepath.Join(stateDir, "newhost", "*", "backup", "*.txt"))
	if awxDir != "" {
		pats = append(pats, filepath.Join(awxDir, "*.txt"))
	}
	type fm struct {
		p string
		t time.Time
	}
	var all []fm
	for _, pat := range pats {
		ms, _ := filepath.Glob(pat)
		for _, p := range ms {
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				all = append(all, fm{p, fi.ModTime()})
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
	if len(all) > newhostHistMaxFiles {
		all = all[:newhostHistMaxFiles]
	}
	out := make([]string, len(all))
	for i, f := range all {
		out[i] = f.p
	}
	return out
}

// loadNewhostHistory: files(최근 순)를 읽어 색인을 만든다 — 오래된 것부터 넣어 같은 키는 최근 값이 남게 한다
// (한 파일 안에서는 뒤 줄이 더 최근). 값이 템플릿 규칙에 맞지 않는 줄(nodeinfo 원본의 1.1T 등)은 건너뛴다.
func loadNewhostHistory(files []string) *nhHistIndex {
	ix := &nhHistIndex{byHost: map[string]nhHist{}, byPfx: map[string]nhHist{}}
	for i := len(files) - 1; i >= 0; i-- {
		f, err := os.Open(files[i])
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(f, newhostHistMaxBytes))
		f.Close()
		for _, ln := range strings.Split(string(b), "\n") {
			fl := strings.Fields(ln)
			if len(fl) != 12 {
				continue
			}
			t, err := parseNewhostTpl(strings.Join([]string{fl[2], fl[6], fl[7], fl[9], fl[10], fl[11]}, " "))
			if err != nil {
				continue
			}
			h := nhHist{Host: fl[3], T: t}
			ix.byHost[strings.ToLower(fl[3])] = h
			if p := nhPrefix(fl[3]); p != "" {
				ix.byPfx[p] = h
			}
		}
	}
	return ix
}

// recommend: 같은 hostname 의 최근 값 → 없으면 접두어가 가장 길게 일치하는 최근 값.
// 짧은 접두어는 경계에서 끝날 때만 인정한다 (spice-a 에 spice·spice- 는 되고, svr 에 s 는 안 됨).
func (ix *nhHistIndex) recommend(host string) (nhHist, bool) {
	if ix == nil {
		return nhHist{}, false
	}
	if h, ok := ix.byHost[strings.ToLower(host)]; ok {
		return h, true
	}
	p := nhPrefix(host)
	for l := len(p); l > 0; l-- {
		q := p[:l]
		h, ok := ix.byPfx[q]
		if !ok {
			continue
		}
		if l == len(p) || !nhAlnum(q[l-1]) || !nhAlnum(p[l]) {
			return h, true
		}
	}
	return nhHist{}, false
}

// ---------------------------------------------------------------- 12필드 · 저장 · AWX

// nhLine: 01 의 ${user}.txt 한 줄 `tmp tmp infra hostname ip mac nic disk part 용량 os boot`
func nhLine(r nhRow, t NewhostTpl) string {
	return strings.Join([]string{"tmp", "tmp", t.Infra, r.Host, r.IP, r.MAC, t.NIC, t.Disk, t.Part, t.Size, t.OS, t.Boot}, " ")
}

// nhSaveResult: 저장 결과 경로 (Backup 은 기존 파일이 없었으면 ""). HistoryErr = 저장은 됐지만 이력 사본을 못 남긴 사유.
type nhSaveResult struct {
	Path, Backup, History string
	HistoryErr            string
}

// nhUniqueFile: dir/<stamp>.txt 를 새로 만든다 (같은 이름이 있으면 _2, _3 …), 0600
func nhUniqueFile(dir, stamp string, data []byte) (string, error) {
	for i := 1; i < 1000; i++ {
		name := stamp + ".txt"
		if i > 1 {
			name = fmt.Sprintf("%s_%d.txt", stamp, i)
		}
		p := filepath.Join(dir, name)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Chmod(p, 0600)
		}
		if err != nil {
			os.Remove(p)
			return "", err
		}
		return p, nil
	}
	return "", fmt.Errorf("%s 에 새 파일 이름을 만들 수 없음", dir)
}

// newhostSave: awxDir/<user>.txt 에 lines 를 저장한다.
//  1. 기존 파일이 있으면 <stateDir>/newhost/<user>/backup/<YYYYMMDD-HHMMSS>.txt 로 복사 (실패하면 아무것도 바꾸지 않음)
//  2. 같은 디렉터리 임시 파일에 쓰고 권한(기존 권한, 없으면 0644 — 01 이 읽어야 함)을 맞춘 뒤 rename (원자적)
//  3. 같은 내용을 history/<시각>.txt 에도 남긴다 (실패해도 저장은 유지 — HistoryErr 로 알림, err 은 nil)
func newhostSave(awxDir, stateDir, user string, lines []string, now time.Time) (nhSaveResult, error) {
	var res nhSaveResult
	if !validChangeUser(user) {
		return res, errors.New("user 이름이 올바르지 않음")
	}
	data := []byte(strings.Join(lines, "\n") + "\n")
	base := newhostUserDir(stateDir, user)
	if err := mkdir0700(filepath.Join(stateDir, "newhost"), base, filepath.Join(base, "backup"), filepath.Join(base, "history")); err != nil {
		return res, fmt.Errorf("기록 디렉터리 생성 실패: %v", err)
	}
	stamp := now.Format("20060102-150405")
	res.Path = filepath.Join(awxDir, user+".txt")
	dst := res.Path // 실제로 쓸 파일 — 심볼릭 링크면 링크를 유지하고 대상 파일을 바꾼다
	if real, err := filepath.EvalSymlinks(res.Path); err == nil {
		dst = real
	}
	perm := os.FileMode(0644)
	uid, gid := -1, -1 // 기존 소유자 유지 (rename 하면 실행 계정 소유가 되므로)
	if fi, err := os.Stat(dst); err == nil {
		perm = fi.Mode().Perm()
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(st.Uid), int(st.Gid)
		}
		old, err := os.ReadFile(res.Path)
		if err != nil {
			return res, fmt.Errorf("기존 %s 읽기 실패: %v", res.Path, err)
		}
		if res.Backup, err = nhUniqueFile(filepath.Join(base, "backup"), stamp, old); err != nil {
			return res, fmt.Errorf("백업 실패: %v", err)
		}
	} else if !os.IsNotExist(err) {
		return res, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+user+".txt.tmp-*")
	if err != nil {
		return res, err
	}
	tp := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tp, perm)
	}
	if err == nil && uid >= 0 {
		os.Chown(tp, uid, gid) // root 가 아니면 실패해도 소유자는 이미 실행 계정이므로 무시
	}
	if err == nil {
		err = os.Rename(tp, dst)
	}
	if err != nil {
		os.Remove(tp)
		return res, err
	}
	if res.History, err = nhUniqueFile(filepath.Join(base, "history"), stamp, data); err != nil {
		res.HistoryErr = err.Error()
	}
	return res, nil
}

// nhAwxReq: 저장 뒤 AWX 로 이어 갈 때 넘기는 값
type nhAwxReq struct {
	User       string
	Hosts      []string // 01 의 '등록 후 확인' 에 자동 입력할 hostname
	DNSChecked bool     // 양식 단계에서 DNS 점검을 실제로 했음 (DNS 비활성으로 생략했으면 false)
}

// newhostAwxEnv: 신규 설치 양식 → AWX 실행 환경. awxEnv 에 더해
//   - 프로파일을 골랐으면 nodeinfo 만 n 으로 바꿔 AWX_AUTO=1 (OS 등 나머지 자동 응답은 프로파일 그대로)
//   - 프로파일을 고르지 않으면(일반 실행) 자동 응답을 일절 넘기지 않는다 — nodeinfo 도 자동 N 이 아니라 01 이 묻는 대로
//     직접 응답한다 (nodeinfo 를 쓸 수 있도록)
//   - DNS 점검을 했으면 AWX_DNS_CHECKED=1 (01 은 표만 출력하고 묻지 않음)
func newhostAwxEnv(p *AwxProfile, user, verify string, dnsChecked bool) []string {
	var q *AwxProfile
	if p != nil {
		c := *p
		c.Nodeinfo = "n"
		q = &c
	}
	env := awxEnv(q, user, verify)
	if dnsChecked {
		env = append(env, "AWX_DNS_CHECKED=1")
	}
	return env
}

// newhostAwxNote: AWX 실행 직전 터미널에 찍는 안내 한 줄
func newhostAwxNote(p *AwxProfile) string {
	if p != nil {
		return fmt.Sprintf("[auto_setup] 신규 설치 양식: nodeinfo 는 N 으로 실행합니다 (프로파일 %d 의 nodeinfo=%s 와 무관)", p.No, p.Nodeinfo)
	}
	return "[auto_setup] 신규 설치 양식: 프로파일 없이 일반 실행합니다 — 01 의 nodeinfo 질문은 직접 응답하세요 (자동 N 아님)"
}
