// work.go - v0.8.0 gossh 작업 기록 (TUI s / CLI auto_setup s) 의 순수 로직·저장소·실행. 화면은 work_tui.go.
//
//   - 호스트 붙여넣기 해석(workParseHosts — gossh 와 같은 구분·범위 표기), /tmp/<소문자 3자리> 이름(workNewName)
//   - 기록 보관 <AUTO_SETUP_DIR>/work/<user>/ (디렉터리 0700·파일 0600)
//     index.jsonl   요약(영구) — 한 줄 = 실행 하나 또는 메모 하나. 추가만 하고 고쳐 쓰지 않는다(마지막 메모가 유효).
//     동시 실행: O_APPEND 로 한 줄을 write 한 번에 쓴다(잠금 없음 — 같은 파일의 O_APPEND write 는 리눅스에서 섞이지 않음).
//     lists/<xxx>.txt 호스트 목록 사본 (/tmp 파일이 지워져도 재실행 때 다시 만듦)
//     out/<id>.log.gz 전체 출력 (7일 지나면 삭제, user 당 200MB 넘으면 오래된 것부터 — 데몬 하루 한 번 + 화면 열 때)
//   - 실행 경로(workPlan), gossh/ssh 명령(workLocalArgv/workOS6Argv), script 래퍼(workScript), 출력 판정(judgeWork)
//   - os6_mgmt 원격 클라이언트: 기록은 os8_mgmt 의 같은 저장소로 중계 (remoteWorkStore ↔ `auto_setup work-relay`, 기존 remoteRun 사용)
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	workKeep       = 7 * 24 * time.Hour // 전체 출력·/tmp 파일 보관 기간
	workRelayCap   = 5 << 20            // os6_mgmt → os8_mgmt 로 보내는 압축 출력 상한 (넘으면 앞/뒤만)
	workRelayChunk = 56 << 10           // 중계 한 번에 싣는 바이트 (인코딩 후 약 90KB — 원격 셸 인자 한 개 128KB 제한 안)
	workRelayMax   = 32 << 20           // 중계 파일 하나 최대 (rec·out)
	workMaxHosts   = 100000             // 붙여넣기 범위 표기 확장 상한
	workDangerFlag = "-dnlgjawkrdjqghkrdls"
)

// workTmpDir: 호스트 목록 파일(/tmp/<3자리>)을 만드는 곳 (테스트에서 바꾼다)
var workTmpDir = "/tmp"

// workUserCap: user 당 전체 출력 상한 (압축 크기, 확정 200MB — 테스트만 줄인다)
var workUserCap int64 = 200 << 20

// workRand: 이름·id 의 임의 글자 (TUI goroutine 에서만 사용, 테스트에서 고정 가능)
var workRand = rand.New(rand.NewSource(time.Now().UnixNano()))

var (
	workNameRe  = regexp.MustCompile(`^[a-z]{3}$`)
	workIDRe    = regexp.MustCompile(`^\d{8}-\d{6}-[0-9a-f]{4}$`)
	workRangeRe = regexp.MustCompile(`^(.*?)\[([^\]]+)\](.*)$`)
)

// ---------------------------------------------------------------- 기록 형식

// workRes: 실행 결과 요약. 성공 호스트는 목록 - Fail - Unr 이라 수만 둔다.
type workRes struct {
	Ok    int      `json:"ok"`
	Fail  []string `json:"fail,omitempty"`  // 실패: ERROR: 종료코드 N 등 · 사용자 취소 · 결과 없음
	Unr   []string `json:"unr,omitempty"`   // 접속불가 (SSH 접속 실패·DNS 조회 실패·세션 생성 실패)
	NoRun bool     `json:"norun,omitempty"` // gossh 가 호스트를 하나도 실행하지 않음 (위험 명령 거부·y/N 취소·파일 오류) — Fail 에 전체
}

// workRec: index.jsonl 한 줄. T=run(실행) | memo(파일 메모)
type workRec struct {
	T      string   `json:"t"`
	ID     string   `json:"id,omitempty"`
	File   string   `json:"file"`             // /tmp 아래 이름 (소문자 3자리)
	Parent string   `json:"parent,omitempty"` // 파생 원본 파일 (실패·접속불가만 재실행으로 만든 목록)
	At     int64    `json:"at,omitempty"`
	TTY    string   `json:"tty,omitempty"`    // 실행한 터미널 (pts/3)
	Server string   `json:"server,omitempty"` // 실행한 서버 호스트명
	Side   string   `json:"side,omitempty"`   // os8 | os6 | os6 전량
	Cwd    string   `json:"cwd,omitempty"`
	Cmd    string   `json:"cmd,omitempty"`
	N      int      `json:"n,omitempty"`
	RC     int      `json:"rc"`
	Res    *workRes `json:"res,omitempty"`
	O6     *workRes `json:"o6,omitempty"`     // 접속불가 호스트의 os6_mgmt 경유 재실행 결과
	O6Note string   `json:"o6note,omitempty"` // os6 재실행을 못 한 사유
	Memo   string   `json:"memo,omitempty"`
}

// workDB: 한 user 의 기록 전체 (Load 결과)
type workDB struct {
	Runs  []*workRec          `json:"runs"`  // index 순서 (오래된 것부터)
	Memo  map[string]string   `json:"memo"`  // 파일 → 메모
	Lists map[string][]string `json:"lists"` // 파일 → 호스트 목록 사본
}

// workGroup: 화면의 파일 묶음
type workGroup struct {
	File, Parent string
	Hosts        []string
	Memo         string
	Runs         []*workRec // 최신이 앞
	Last         int64
}

// groups: 파일별 묶음 — 최근에 실행한 파일이 위, 묶음 안도 최신이 위
func (db *workDB) groups() []workGroup {
	idx := map[string]int{}
	var gs []workGroup
	for i := len(db.Runs) - 1; i >= 0; i-- {
		r := db.Runs[i]
		k, ok := idx[r.File]
		if !ok {
			k = len(gs)
			idx[r.File] = k
			gs = append(gs, workGroup{File: r.File, Hosts: db.Lists[r.File], Memo: db.Memo[r.File]})
		}
		g := &gs[k]
		g.Runs = append(g.Runs, r)
		if r.Parent != "" {
			g.Parent = r.Parent
		}
		if r.At > g.Last {
			g.Last = r.At
		}
	}
	for i := range gs {
		sort.SliceStable(gs[i].Runs, func(a, b int) bool { return gs[i].Runs[a].At > gs[i].Runs[b].At })
	}
	sort.SliceStable(gs, func(a, b int) bool { return gs[a].Last > gs[b].Last })
	return gs
}

// sameList: hosts 와 집합이 같은 기존 목록 (최근 것 우선), 없으면 ""
func (db *workDB) sameList(hosts []string) string {
	want := workSet(hosts)
	for _, g := range db.groups() {
		l := db.Lists[g.File]
		if len(l) != len(want) {
			continue
		}
		same := true
		for _, h := range l {
			if !want[h] {
				same = false
				break
			}
		}
		if same {
			return g.File
		}
	}
	return ""
}

// used: 이 user 의 기록에 있는 파일 이름
func (db *workDB) used(name string) bool {
	if _, ok := db.Lists[name]; ok {
		return true
	}
	for _, r := range db.Runs {
		if r.File == name || r.Parent == name {
			return true
		}
	}
	return false
}

// search: host 가 들어 있던 실행 (시간순). 정확히 같은 이름 또는 짧은 이름(첫 '.' 앞)이 같으면 일치.
func (db *workDB) search(host string) []*workRec {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	sh := shortHost(host)
	has := map[string]bool{}
	for f, l := range db.Lists {
		for _, h := range l {
			if h == host || (sh != "" && shortHost(h) == sh) {
				has[f] = true
				break
			}
		}
	}
	var out []*workRec
	for _, r := range db.Runs {
		if has[r.File] {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].At < out[b].At })
	return out
}

// workMatchHost: 목록에서 검색어와 맞는 호스트 이름 (정확히 같은 이름 우선, 없으면 짧은 이름), 없으면 검색어 그대로
func workMatchHost(list []string, q string) string {
	sq := shortHost(q)
	for _, h := range list {
		if h == q {
			return h
		}
	}
	for _, h := range list {
		if sq != "" && shortHost(h) == sq {
			return h
		}
	}
	return q
}

// workSet: 목록 → 집합
func workSet(hosts []string) map[string]bool {
	m := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		m[h] = true
	}
	return m
}

// workRetryHosts: "실패·접속불가만 재실행" 대상 — 실패 + (os6 재실행을 했으면 그 결과의 실패·접속불가, 아니면 접속불가). 목록 순서.
func workRetryHosts(r *workRec, list []string) []string {
	if r.Res == nil {
		return nil
	}
	bad := workSet(r.Res.Fail)
	if r.O6 != nil {
		for _, h := range append(append([]string{}, r.O6.Fail...), r.O6.Unr...) {
			bad[h] = true
		}
	} else {
		for _, h := range r.Res.Unr {
			bad[h] = true
		}
	}
	var out []string
	for _, h := range list {
		if bad[h] {
			out = append(out, h)
			delete(bad, h)
		}
	}
	for _, h := range append(append([]string{}, r.Res.Fail...), r.Res.Unr...) { // 목록 사본에 없는 이름(이론상 없음)도 빠뜨리지 않음
		if bad[h] {
			out = append(out, h)
			delete(bad, h)
		}
	}
	return out
}

// workHostStatus: 실행 r 에서 호스트 h 의 결과 한 마디
func workHostStatus(r *workRec, h string) string {
	if r.Res == nil {
		return "-"
	}
	in := func(l []string) bool {
		for _, x := range l {
			if x == h {
				return true
			}
		}
		return false
	}
	switch {
	case r.Res.NoRun:
		return "실행 안 됨"
	case in(r.Res.Fail):
		return "실패"
	case in(r.Res.Unr):
		if r.O6 != nil {
			switch {
			case in(r.O6.Fail):
				return "접속불가 → os6 실패"
			case in(r.O6.Unr):
				return "접속불가 → os6 접속불가"
			}
			return "접속불가 → os6 성공"
		}
		return "접속불가"
	}
	return "성공"
}

// commaN: 1198 → 1,198
func commaN(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// workResCounts: "성공 18 실패 1 접속불가 2" (0 인 실패·접속불가는 생략)
func workResCounts(r *workRes) string {
	s := "성공 " + commaN(r.Ok)
	if len(r.Fail) > 0 {
		s += " 실패 " + commaN(len(r.Fail))
	}
	if len(r.Unr) > 0 {
		s += " 접속불가 " + commaN(len(r.Unr))
	}
	return s
}

// workResText: 실행 줄의 결과 — `성공 18 접속불가 2(os6 재실행 → 성공 2)`, 실행 안 됨이면 `실행 안 됨 (rc=1)`
func workResText(r *workRec) string {
	if r.Res == nil {
		return "결과 없음"
	}
	if r.Res.NoRun {
		return fmt.Sprintf("실행 안 됨 (rc=%d)", r.RC)
	}
	s := workResCounts(r.Res)
	switch {
	case r.O6 != nil:
		s += "(os6 재실행 → " + workResCounts(r.O6) + ")"
	case r.O6Note != "" && len(r.Res.Unr) > 0:
		s += "(" + r.O6Note + ")"
	}
	return s
}

// workResBad: 아직 성공하지 못한 호스트가 있으면 true (빨강 표시)
func workResBad(r *workRec) bool {
	if r.Res == nil || r.Res.NoRun || len(r.Res.Fail) > 0 {
		return true
	}
	if len(r.Res.Unr) == 0 {
		return false
	}
	return r.O6 == nil || len(r.O6.Fail)+len(r.O6.Unr) > 0
}

// ---------------------------------------------------------------- 붙여넣기·이름

// workParseHosts: 붙여넣은 글 → 호스트 목록. gossh 의 호스트 파일 해석과 같다: 공백·쉼표·줄바꿈 구분, # 로 시작하는 줄 무시,
// svr[001-020] 범위 표기 확장. 중복은 처음 것만 남기고 순서는 유지한다.
func workParseHosts(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, tok := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
			for _, h := range workExpand(tok, 0) {
				if !seen[h] && len(out) < workMaxHosts {
					seen[h] = true
					out = append(out, h)
				}
			}
		}
	}
	return out
}

// workExpand: gossh expandHostLine 과 같은 범위 표기 확장 (svr[01-03] → svr01 svr02 svr03, 숫자가 아니면 그대로)
func workExpand(tok string, depth int) []string {
	m := workRangeRe.FindStringSubmatch(tok)
	if m == nil || depth > 8 {
		return []string{tok}
	}
	pre, body, suf := m[1], m[2], m[3]
	var out []string
	for _, item := range strings.Split(body, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if i := strings.Index(item, "-"); i > 0 {
			a, err1 := strconv.Atoi(item[:i])
			b, err2 := strconv.Atoi(item[i+1:])
			if err1 == nil && err2 == nil {
				if a > b {
					a, b = b, a
				}
				pad := 0
				if len(item[:i]) > 1 && item[0] == '0' {
					pad = i
				}
				for n := a; n <= b && len(out) < workMaxHosts; n++ {
					out = append(out, workExpand(pre+fmt.Sprintf("%0*d", pad, n)+suf, depth+1)...)
				}
				continue
			}
		}
		out = append(out, workExpand(pre+item+suf, depth+1)...)
	}
	return out
}

// workNewName: /tmp 에 없고 이 user 의 기록에도 없는 소문자 3자리 이름
func workNewName(db *workDB) (string, error) {
	taken := func(n string) bool {
		if db != nil && db.used(n) {
			return true
		}
		_, err := os.Lstat(filepath.Join(workTmpDir, n))
		return err == nil || !os.IsNotExist(err)
	}
	name := func(k int) string {
		return string([]byte{byte('a' + k/676), byte('a' + k/26%26), byte('a' + k%26)})
	}
	const all = 26 * 26 * 26
	for i := 0; i < 200; i++ {
		if n := name(workRand.Intn(all)); !taken(n) {
			return n, nil
		}
	}
	start := workRand.Intn(all)
	for k := 0; k < all; k++ {
		if n := name((start + k) % all); !taken(n) {
			return n, nil
		}
	}
	return "", errors.New("사용할 수 있는 /tmp/<3자리> 이름이 없습니다")
}

// workNewID: 실행 id (시각 + 임의 4자리)
func workNewID(now time.Time) string {
	return now.Format("20060102-150405") + fmt.Sprintf("-%04x", workRand.Intn(1<<16))
}

// workWriteTmp: 목록 파일을 /tmp/<name> 으로 (0644). excl 이면 이미 있을 때 실패.
func workWriteTmp(name string, hosts []string, excl bool) error {
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if excl {
		flag = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(filepath.Join(workTmpDir, name), flag, 0644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(strings.Join(hosts, "\n") + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// workTmpMatches: /tmp/<name> 이 있고 내용(해석한 호스트 목록)이 hosts 와 같으면 (있음, 같음)
func workTmpMatches(name string, hosts []string) (exists, same bool) {
	b, err := os.ReadFile(filepath.Join(workTmpDir, name))
	if err != nil {
		_, serr := os.Lstat(filepath.Join(workTmpDir, name))
		return serr == nil, false
	}
	got := workParseHosts(string(b))
	if len(got) != len(hosts) {
		return true, false
	}
	for i := range got {
		if got[i] != hosts[i] {
			return true, false
		}
	}
	return true, true
}

// ---------------------------------------------------------------- 실행 경로

// workPlanT: 실행 경로
type workPlanT struct {
	Side   string // 기록·화면 표시: os8 | os6 | os6 전량
	Remote bool   // 전량 os6_mgmt 경유 (ssh -t)
	Retry  bool   // os8 실행 후 접속불가 호스트만 os6_mgmt 경유로 자동 재실행
	Note   string // os6 재실행을 못 하는 사유 (기록·화면)
	Ask    string // 실행 전에 물을 것 (전량 os6 대상인데 os6_mgmt 미설정)
}

const workNoOS6 = "os6_mgmt 미설정"

// workPlan: n 대를 어디서 실행할지 (수정계획서 §4-2, 9장 4)
//   - os6_mgmt 원격 클라이언트: 대수와 상관없이 이 서버(os6)의 gossh 로 바로 실행 (기록은 os8 로 중계)
//   - os8_mgmt: n ≤ work_os6_over(빈 값 1000) → os8 gossh + 접속불가만 os6_mgmt 경유 재실행, 초과 → 전량 os6_mgmt 경유
//   - os6_mgmt/os6_gossh 미설정이면 os6 재실행·전량 os6 불가 → os8 로만 (전량 대상이면 실행 전 확인)
func workPlan(n int, client bool) workPlanT {
	if client {
		return workPlanT{Side: "os6"}
	}
	os6 := os6_mgmt != "" && os6_gossh != ""
	over := workOS6Over()
	if n > over {
		if os6 {
			return workPlanT{Side: "os6 전량", Remote: true}
		}
		return workPlanT{Side: "os8", Note: workNoOS6,
			Ask: fmt.Sprintf("%s대는 기준 %s대를 넘지만 os6_mgmt 가 설정되지 않아 os8 gossh 로 실행합니다 — 계속? [y/n]", commaN(n), commaN(over))}
	}
	if os6 {
		return workPlanT{Side: "os8", Retry: true}
	}
	return workPlanT{Side: "os8", Note: workNoOS6}
}

// workGosshBin: 이 서버에서 실행할 gossh — os8_mgmt 는 PATH 의 gossh, os6_mgmt 원격 클라이언트는 os6_gossh(비면 PATH 의 gossh)
func workGosshBin(client bool) string {
	if client && os6_gossh != "" {
		return os6_gossh
	}
	return "gossh"
}

// workDanger: gossh 가 위험 작업으로 거부하는 명령인지 (gossh main.go 의 isDangerous 와 같은 단어)
func workDanger(cmd string) bool {
	l := strings.ToLower(cmd)
	for _, w := range []string{"reboot", "poweroff", "shutdown", "halt", "init 0", "init 6", "ddc"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

// workLocalArgv: 이 서버의 gossh — gossh -w <파일> [위험작업옵션] '<명령>' (명령은 인자 하나로 그대로)
func workLocalArgv(bin, file, cmd string, danger bool) []string {
	argv := []string{bin, "-w", file}
	if danger {
		argv = append(argv, workDangerFlag)
	}
	return append(argv, cmd)
}

// workOS6Inner: os6_mgmt 에서 bash 가 실행할 한 줄 — base64(gzip(목록)) 를 원격 mktemp 파일로 복원 → os6 gossh -w <그 파일> → 정리.
// gossh 인자에는 원격 파일 경로와 사용자 명령만 들어간다 (base64 가 gossh 의 위험어 검사에 걸리지 않게). 줄바꿈 없음.
// 사용자 명령도 base64 로 실어 원격 변수로 복원한다 — 로그인 셸이 tcsh 면 작은따옴표 안의 `!` 도 history 치환되어
// ("Event not found") 명령이 깨지기 때문 (base64 글자에는 ! 가 없음).
func workOS6Inner(hosts []string, cmd string, danger bool) string {
	var zb bytes.Buffer
	zw := gzip.NewWriter(&zb)
	zw.Write([]byte(strings.Join(hosts, "\n") + "\n"))
	zw.Close()
	b64 := base64.StdEncoding.EncodeToString(zb.Bytes())
	c64 := base64.StdEncoding.EncodeToString([]byte(cmd))
	g := shq(os6_gossh) + " -w $f"
	if danger {
		g += " " + workDangerFlag
	}
	return "f=$(mktemp) || exit 97; echo " + b64 + " | base64 -d | gzip -dc > $f; c=$(echo " + c64 + " | base64 -d); " + g + ` "$c"` +
		"; r=$?; rm -f $f ${f}_*; exit $r"
}

// workOS6Argv: os6_mgmt 경유 실행 — ssh -t (원격 tty: 출력·색·위험어 y/N 확인 그대로), 로그인 셸이 csh 여도 remoteBash 로 bash 실행
func workOS6Argv(hosts []string, cmd string, danger bool) []string {
	return []string{"ssh", "-t", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", os6_mgmt, remoteBash(workOS6Inner(hosts, cmd, danger))}
}

// workShellLine: script -c 에 넘길 /bin/sh 한 줄 — 인자를 하나씩 인용해 실행하고 종료코드를 rcPath 에 쓴다.
// (RHEL6 util-linux-ng 2.17 의 script 에는 -e(종료코드 전달)가 없어 래퍼 셸에서 따로 남긴다)
func workShellLine(argv []string, rcPath string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shq(a)
	}
	return strings.Join(q, " ") + "; echo $? > " + shq(rcPath)
}

// workScriptEnv: script 는 -c 명령을 $SHELL -c 로 실행하므로 로그인 셸(csh 등)과 상관없이 /bin/sh 로 고정
func workScriptEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, "SHELL=") {
			out = append(out, e)
		}
	}
	return append(out, "SHELL=/bin/sh")
}

// workScript: argv 를 `script -q -f -c` 로 감싸 지금 터미널에서 실행한다 (tty 유지 — 색·진행률·위험어 y/N 그대로, 출력은 logPath 에도).
// 종료코드를 돌려준다 (알 수 없으면 -1). 테스트에서 가짜로 교체.
var workScript = realWorkScript

func realWorkScript(argv []string, logPath string) (int, error) {
	rcPath := logPath + ".rc"
	// 종료코드 파일은 미리 0600 으로 만들어 둔다 (O_EXCL) — logPath 는 /tmp 라 이름이 보이므로, 남이 같은 이름의
	// 심볼릭·하드 링크를 먼저 두면 root 의 `echo $? >` 가 그 대상을 덮어쓰게 된다 (/tmp 의 sticky 비트로 우리 파일은 못 바꿈).
	rf, err := os.OpenFile(rcPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return -1, err
	}
	rf.Close()
	defer os.Remove(rcPath)
	cmd := exec.Command("script", "-q", "-f", "-c", workShellLine(argv, rcPath), logPath)
	cmd.Env = workScriptEnv(os.Environ())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return -1, err
		}
	}
	b, err := os.ReadFile(rcPath)
	if err != nil {
		return -1, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return -1, nil
	}
	return n, nil
}

// stripScriptLog: script 로그의 머리 `Script started on …` 줄과 꼬리 `Script done on …` 줄을 뗀다 (util-linux 2.17·2.32 모두)
func stripScriptLog(b []byte) []byte {
	if bytes.HasPrefix(b, []byte("Script started on ")) {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		} else {
			b = nil
		}
	}
	t := bytes.TrimRight(b, "\r\n")
	if i := bytes.LastIndex(t, []byte("\nScript done on ")); i >= 0 && bytes.IndexByte(t[i+1:], '\n') < 0 {
		b = b[:i+1]
	} else if bytes.HasPrefix(t, []byte("Script done on ")) && bytes.IndexByte(t, '\n') < 0 {
		b = nil
	}
	return b
}

// ---------------------------------------------------------------- 출력 판정

// workResFiles: gossh 가 -w <파일> 옆에 남기는 결과 파일 (접속불가·Refused·사용자 취소). 없으면 빈 목록.
func workResFiles(path string) map[string][]string {
	m := map[string][]string{}
	for k, suf := range map[string]string{"off": "_res_off", "refused": "_res_refsed", "cancel": "_res_cancel"} {
		if b, err := os.ReadFile(path + suf); err == nil {
			m[k] = strings.Fields(string(b))
		}
	}
	return m
}

// workConnFail: 접속 단계 실패 줄 (gossh printPdshStyle: "ERROR: SSH 접속 실패: …" / "ERROR: DNS 조회 실패: …" / "ERROR: 세션 생성 실패: …")
func workConnFail(t string) bool {
	t = strings.TrimPrefix(t, "ERROR: ")
	return strings.HasPrefix(t, "SSH 접속 실패") || strings.HasPrefix(t, "DNS 조회 실패") || strings.HasPrefix(t, "세션 생성 실패")
}

// judgeWork: gossh 출력(script 로그, 색 포함) → 호스트별 성공/실패/접속불가. 근거는 gossh main.go:
//   - 명령을 실행한 호스트는 종료코드와 상관없이 "정상 접속 가능" 에 세고, 출력이 없으면(예 `true`) 아무 줄도 찍지 않는다
//     → 요약 블록(` 정상 접속 가능 : N 대`)까지 나온 정상 종료 실행에서 줄이 하나도 없는 호스트 = 성공(출력 없음).
//   - 종료코드≠0 이면 stderr 에 `host: ERROR: 종료코드 N` → 실패. Ctrl+C 로 취소된 호스트는 `host: 사용자 취소` → 실패.
//   - 접속 실패는 `host: ERROR: SSH 접속 실패|DNS 조회 실패|세션 생성 실패: …` → 접속불가. 단 gossh 는 1차가 끝난 뒤 접속불가만
//     한 번 더 시도(`재검증 중(…)`)하므로, 최종 접속불가는 결과 파일(<파일>_res_off·_res_refsed)이 기준이다.
//     결과 파일을 못 쓰는 경우(os6_mgmt 경유)에는 실패 줄 수로 본다: 재검증이 있었는데 실패 줄이 1번뿐이면 재검증에서 접속된 것.
//   - 요약 블록이 없으면(위험 명령 거부·y/N 취소·중단) 줄이 없는 호스트는 결과 없음(실패), 아무 줄도 없으면 NoRun.
//   - Ctrl+C 세 번(전체 중단)이면 요약 블록은 나오지만 아직 시작하지 않은 호스트는 아무 줄도 없이 끝난다
//     (` 전체 중단됨 : 미실행 N 대`, _res_cancel 에도 없음) → 이때 줄이 없는 호스트는 성공이 아니라 결과 없음(실패).
//
// files 는 workResFiles 결과 (os6_mgmt 경유면 nil).
func judgeWork(out string, hosts []string, files map[string][]string) workRes {
	g := parseGossh(out, hosts)
	complete, retried, aborted := false, false, false
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		if i := strings.LastIndexByte(l, '\r'); i >= 0 {
			l = l[i+1:]
		}
		l = strings.TrimSpace(stripANSI(l))
		switch {
		case strings.HasPrefix(l, "정상 접속 가능 :"):
			complete = true
		case strings.HasPrefix(l, "재검증 중("):
			retried = true
		case strings.HasPrefix(l, "전체 중단됨 :"):
			aborted = true
		}
	}
	const (
		sNone = iota
		sOK
		sFail
		sUnr
	)
	state := map[string]int{}
	fails := map[string]int{}
	for _, e := range g.Ev {
		t := strings.TrimSpace(e.Text)
		switch {
		case workConnFail(t):
			state[e.Host] = sUnr
			fails[e.Host]++
		case strings.HasPrefix(t, "ERROR: ") || t == "사용자 취소":
			state[e.Host] = sFail
		default:
			if state[e.Host] != sFail {
				state[e.Host] = sOK
			}
		}
	}
	useFiles := complete && files != nil
	unrF := workSet(append(append([]string{}, files["off"]...), files["refused"]...))
	canF := workSet(files["cancel"])
	var r workRes
	for _, h := range hosts {
		s := state[h]
		if useFiles {
			switch {
			case unrF[h]:
				s = sUnr
			case canF[h]:
				s = sFail
			case s == sUnr: // 재검증에서 접속됨 (출력 없음)
				s = sOK
			}
		} else if s == sUnr && complete && retried && fails[h] == 1 {
			s = sOK
		}
		switch {
		case s == sOK || (s == sNone && complete && !aborted):
			r.Ok++
		case s == sUnr:
			r.Unr = append(r.Unr, h)
		default:
			r.Fail = append(r.Fail, h)
		}
	}
	r.NoRun = !complete && len(g.Ev) == 0
	return r
}

// ---------------------------------------------------------------- 압축

func gzipBytes(b []byte) []byte {
	var zb bytes.Buffer
	zw := gzip.NewWriter(&zb)
	zw.Write(b)
	zw.Close()
	return zb.Bytes()
}

func gunzipBytes(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// workCapGz: raw 를 압축해 limit 이하로 — 넘으면 앞·뒤만 남기고 가운데를 생략 표시로 바꾼다
func workCapGz(raw []byte, limit int) []byte {
	gz := gzipBytes(raw)
	if len(gz) <= limit {
		return gz
	}
	keep := int(float64(len(raw)) * float64(limit) / float64(len(gz)) * 0.9)
	for keep > 1024 {
		h, t := keep/2, keep-keep/2
		var b bytes.Buffer
		b.Write(raw[:h])
		fmt.Fprintf(&b, "\n\n[auto_setup] 출력이 커서 가운데 %d바이트를 생략했습니다 (os8_mgmt 보관 상한 %dMB)\n\n", len(raw)-h-t, limit>>20)
		b.Write(raw[len(raw)-t:])
		if gz = gzipBytes(b.Bytes()); len(gz) <= limit {
			return gz
		}
		keep /= 2
	}
	return gzipBytes([]byte("[auto_setup] 출력이 너무 커서 보관하지 않았습니다\n"))
}

// ---------------------------------------------------------------- 저장소

// workStore: 기록 저장소 — os8_mgmt(로컬 파일) 또는 os6_mgmt 원격 클라이언트(os8 로 중계)
type workStore interface {
	Load() (*workDB, error)
	AddRun(rec workRec, hosts []string, gz []byte) error // hosts = rec.File 의 목록 (사본이 없을 때만 저장), gz = 압축 출력 (nil = 없음)
	SetMemo(file, memo string) error
	Output(id string) ([]byte, error) // 압축 출력 (없으면 os.ErrNotExist)
}

// workUserDir: <AUTO_SETUP_DIR>/work/<user> (user 는 검증된 이름만)
func workUserDir(user string) (string, error) {
	if !validChangeUser(user) {
		return "", errors.New("user 이름이 올바르지 않음")
	}
	return filepath.Join(dataDir(), "work", user), nil
}

type localWorkStore struct{ dir string }

func (s localWorkStore) index() string            { return filepath.Join(s.dir, "index.jsonl") }
func (s localWorkStore) list(f string) string     { return filepath.Join(s.dir, "lists", f+".txt") }
func (s localWorkStore) outPath(id string) string { return filepath.Join(s.dir, "out", id+".log.gz") }

func (s localWorkStore) Load() (*workDB, error) {
	db := &workDB{Memo: map[string]string{}, Lists: map[string][]string{}}
	b, err := os.ReadFile(s.index())
	if err != nil && !os.IsNotExist(err) {
		return db, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r workRec
		if json.Unmarshal([]byte(line), &r) != nil || !workNameRe.MatchString(r.File) {
			continue // 깨진 줄은 건너뜀 (동시 기록 중 잘린 줄 등)
		}
		switch r.T {
		case "run":
			if workIDRe.MatchString(r.ID) {
				rr := r
				db.Runs = append(db.Runs, &rr)
			}
		case "memo":
			db.Memo[r.File] = r.Memo
		}
	}
	for k, v := range db.Memo {
		if v == "" {
			delete(db.Memo, k)
		}
	}
	for _, r := range db.Runs {
		if _, ok := db.Lists[r.File]; ok {
			continue
		}
		if lb, err := os.ReadFile(s.list(r.File)); err == nil {
			db.Lists[r.File] = strings.Fields(string(lb))
		} else {
			db.Lists[r.File] = nil
		}
	}
	return db, nil
}

// workWriteFile: tmp → rename 으로 0600 파일 저장
func workWriteFile(p string, b []byte) error {
	// 임시 이름은 호출마다 고유하게 (한 프로세스 안의 동시 저장이 같은 .tmp<pid> 를 밟아 rename 이 실패하는 일 방지)
	f, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".tmp"+strconv.Itoa(os.Getpid())+"_")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(b)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	return os.Rename(tmp, p)
}

// appendLine: index 에 JSON 한 줄을 write 한 번으로 추가 (O_APPEND — 동시 추가해도 줄이 섞이지 않음)
func (s localWorkStore) appendLine(r workRec) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.index(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

func (s localWorkStore) AddRun(rec workRec, hosts []string, gz []byte) error {
	if err := mkdir0700(s.dir, filepath.Join(s.dir, "lists"), filepath.Join(s.dir, "out")); err != nil {
		return err
	}
	rec.T = "run"
	if _, err := os.Stat(s.list(rec.File)); os.IsNotExist(err) && hosts != nil {
		if err := workWriteFile(s.list(rec.File), []byte(strings.Join(hosts, "\n")+"\n")); err != nil {
			return err
		}
	}
	if gz != nil {
		if err := workWriteFile(s.outPath(rec.ID), gz); err != nil {
			return err
		}
	}
	return s.appendLine(rec)
}

func (s localWorkStore) SetMemo(file, memo string) error {
	if err := mkdir0700(s.dir); err != nil {
		return err
	}
	return s.appendLine(workRec{T: "memo", File: file, Memo: memo})
}

func (s localWorkStore) Output(id string) ([]byte, error) {
	if !workIDRe.MatchString(id) {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(s.outPath(id))
}

// ---------------------------------------------------------------- 정리

// workCleanOut: 전체 출력 정리 — 7일 지난 것 삭제, 남은 합이 200MB 를 넘으면 오래된 것부터. 중계·임시 조각은 하루 지나면 삭제.
func workCleanOut(dir string, now time.Time) (removed int) {
	type ent struct {
		p    string
		t    time.Time
		size int64
	}
	var keep []ent
	var total int64
	ents, _ := os.ReadDir(filepath.Join(dir, "out"))
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		p := filepath.Join(dir, "out", e.Name())
		if !strings.HasSuffix(e.Name(), ".log.gz") {
			if now.Sub(fi.ModTime()) > 24*time.Hour { // 저장 도중 남은 .tmp 조각
				os.Remove(p)
			}
			continue
		}
		if now.Sub(fi.ModTime()) > workKeep {
			if os.Remove(p) == nil {
				removed++
			}
			continue
		}
		keep = append(keep, ent{p, fi.ModTime(), fi.Size()})
		total += fi.Size()
	}
	sort.Slice(keep, func(a, b int) bool { return keep[a].t.Before(keep[b].t) })
	for i := 0; total > workUserCap && i < len(keep); i++ {
		if os.Remove(keep[i].p) == nil {
			total -= keep[i].size
			removed++
		}
	}
	rents, _ := os.ReadDir(filepath.Join(dir, "relay"))
	for _, e := range rents {
		if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > 24*time.Hour {
			os.Remove(filepath.Join(dir, "relay", e.Name()))
		}
	}
	return removed
}

// workCleanTmp: 우리가 만든 /tmp/<3자리> 중 7일 지난 것 삭제 — 이 서버에서 실행한 기록에 있고, 마지막 실행과 파일 수정이 모두 7일 전이며,
// 내용이 lists 사본과 같을 때만. gossh 가 옆에 만든 결과 파일(<파일>_res_off 등 정확한 이름)도 함께 지운다.
func workCleanTmp(db *workDB, now time.Time) (removed int) {
	if db == nil {
		return 0
	}
	host, _ := os.Hostname()
	last := map[string]int64{}
	for _, r := range db.Runs {
		if r.Server == host && r.At > last[r.File] {
			last[r.File] = r.At
		}
	}
	for f, at := range last {
		if now.Sub(time.Unix(at, 0)) <= workKeep || !workNameRe.MatchString(f) {
			continue
		}
		p := filepath.Join(workTmpDir, f)
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() || now.Sub(fi.ModTime()) <= workKeep {
			continue
		}
		list, ok := db.Lists[f]
		if !ok || list == nil {
			continue
		}
		if _, same := workTmpMatches(f, list); !same {
			continue
		}
		if os.Remove(p) == nil {
			removed++
			for _, suf := range []string{"_res_off", "_res_refsed", "_os_install", "_nosvrauto", "_res_cancel"} {
				os.Remove(p + suf)
			}
		}
	}
	return removed
}

// workCleanAll: 데몬 tick(하루 한 번) — 모든 user 의 출력·/tmp 정리
func workCleanAll(now time.Time) {
	ents, err := os.ReadDir(filepath.Join(dataDir(), "work"))
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() || !validChangeUser(e.Name()) {
			continue
		}
		dir := filepath.Join(dataDir(), "work", e.Name())
		n := workCleanOut(dir, now)
		db, _ := localWorkStore{dir}.Load()
		n += workCleanTmp(db, now)
		if n > 0 {
			logf("gossh 작업 기록 정리: user=%s %d개 삭제", e.Name(), n)
		}
	}
}

// ---------------------------------------------------------------- os6_mgmt → os8_mgmt 중계

// wkB32: 중계 인자 인코딩 — d t w z 를 뺀 32글자 base32. gossh 원샷 명령에 실리므로 gossh 의 위험어(reboot·poweroff·shutdown·halt·
// init 0/6·ddc)와 /user/ 경로가 우연히 만들어지지 않는다 (base64 로 실으면 큰 조각에서 "ddc" 등이 거의 반드시 나와 gossh 가 거부).
var wkB32 = base32.NewEncoding("0123456789abcefghijklmnopqrsuvxy").WithPadding(base32.NoPadding)

func wkEnc(s string) string { return wkB32.EncodeToString([]byte(s)) }

func wkDec(s string) (string, error) {
	b, err := wkB32.DecodeString(s)
	return string(b), err
}

// workRemoteRun: os8_mgmt 에서 auto_setup 을 실행하는 통로 (client.go remoteRun — 테스트에서 교체)
var workRemoteRun = remoteRun

// remoteWorkStore: os6_mgmt 원격 클라이언트의 저장소 — os8_mgmt 의 `auto_setup work-relay <op> <인코딩 인자…>` 로 읽고 쓴다
type remoteWorkStore struct{ user string }

func (r remoteWorkStore) call(op string, args ...string) (string, error) {
	enc := []string{"work-relay", op}
	for _, a := range append([]string{r.user}, args...) {
		enc = append(enc, wkEnc(a))
	}
	res, err := workRemoteRun(enc)
	if err != nil {
		return "", err
	}
	if res.RC != 0 {
		return "", remoteFail("work-relay "+op, res)
	}
	return res.Stdout, nil
}

func (r remoteWorkStore) Load() (*workDB, error) {
	out, err := r.call("load")
	if err != nil {
		return nil, err
	}
	db := &workDB{}
	if err := json.Unmarshal([]byte(out), db); err != nil {
		return nil, fmt.Errorf("os8_mgmt 기록 해석 실패: %v", err)
	}
	if db.Memo == nil {
		db.Memo = map[string]string{}
	}
	if db.Lists == nil {
		db.Lists = map[string][]string{}
	}
	return db, nil
}

// upload: b 를 조각으로 나눠 os8 의 relay/<id>.<kind> 에 이어 붙인다
func (r remoteWorkStore) upload(id, kind string, b []byte) error {
	for off := 0; off < len(b); off += workRelayChunk {
		end := off + workRelayChunk
		if end > len(b) {
			end = len(b)
		}
		if _, err := r.call("put", id, kind, strconv.Itoa(off), string(b[off:end])); err != nil {
			return err
		}
	}
	return nil
}

// workRelayRec: 중계로 보내는 실행 기록 (+ 목록 사본)
type workRelayRec struct {
	Rec   workRec  `json:"rec"`
	Hosts []string `json:"hosts"`
}

func (r remoteWorkStore) AddRun(rec workRec, hosts []string, gz []byte) error {
	rec.T = "run"
	b, err := json.Marshal(workRelayRec{Rec: rec, Hosts: hosts})
	if err != nil {
		return err
	}
	if err := r.upload(rec.ID, "rec", gzipBytes(b)); err != nil {
		return err
	}
	if len(gz) > 0 {
		if err := r.upload(rec.ID, "out", gz); err != nil {
			return err
		}
	}
	_, err = r.call("commit", rec.ID)
	return err
}

func (r remoteWorkStore) SetMemo(file, memo string) error {
	_, err := r.call("memo", file, memo)
	return err
}

func (r remoteWorkStore) Output(id string) ([]byte, error) {
	out, err := r.call("out", id)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, os.ErrNotExist
	}
	return []byte(out), nil
}

// cmdWorkRelay: os8_mgmt 쪽 `auto_setup work-relay <op> <user> …` (인자는 wkEnc). os6_mgmt 원격 클라이언트의 gossh 작업 기록을 받는다.
//
//	load  <user>                       기록 전체(JSON) 출력
//	put   <user> <id> <rec|out> <off> <data>  relay/<id>.<kind> 의 off 위치에 이어 붙임 (같은 조각 재전송은 무시)
//	commit <user> <id>                 relay 의 rec(+out) 을 로컬 저장소에 기록하고 조각 삭제
//	memo  <user> <file> <memo>         메모 추가
//	out   <user> <id>                  압축 출력 그대로 출력 (없으면 종료코드 1)
func cmdWorkRelay(args []string) int {
	fail := func(f string, a ...interface{}) int {
		fmt.Fprintf(os.Stderr, "[X] work-relay: "+f+"\n", a...)
		return 1
	}
	if len(args) < 2 {
		return fail("인자 부족")
	}
	op := args[0]
	v := make([]string, len(args)-1)
	for i, a := range args[1:] {
		s, err := wkDec(a)
		if err != nil {
			return fail("인자 형식 오류")
		}
		v[i] = s
	}
	dir, err := workUserDir(v[0])
	if err != nil {
		return fail("%v", err)
	}
	st := localWorkStore{dir}
	need := func(n int) bool { return len(v) == n }
	switch op {
	case "load":
		if !need(1) {
			return fail("인자 수")
		}
		db, err := st.Load()
		if err != nil {
			return fail("%v", err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(db); err != nil {
			return fail("%v", err)
		}
		return 0
	case "put":
		if !need(5) || !workIDRe.MatchString(v[1]) || (v[2] != "rec" && v[2] != "out") {
			return fail("인자 오류")
		}
		off, err := strconv.Atoi(v[3])
		if err != nil || off < 0 || off+len(v[4]) > workRelayMax {
			return fail("위치·크기 오류")
		}
		if err := mkdir0700(dir, filepath.Join(dir, "relay")); err != nil {
			return fail("%v", err)
		}
		p := filepath.Join(dir, "relay", v[1]+"."+v[2])
		var size int64
		if fi, err := os.Stat(p); err == nil {
			size = fi.Size()
		}
		switch {
		case size == int64(off+len(v[4])): // 같은 조각 재전송
			return 0
		case size != int64(off):
			return fail("조각 순서 오류 (받은 %d, 요청 %d)", size, off)
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			return fail("%v", err)
		}
		_, werr := f.WriteString(v[4])
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return fail("%v", werr)
		}
		return 0
	case "commit":
		if !need(2) || !workIDRe.MatchString(v[1]) {
			return fail("인자 오류")
		}
		rp := filepath.Join(dir, "relay", v[1]+".rec")
		outP := filepath.Join(dir, "relay", v[1]+".out")
		zb, err := os.ReadFile(rp)
		if err != nil {
			return fail("기록 조각 없음")
		}
		b, err := gunzipBytes(zb)
		if err != nil {
			return fail("기록 해석 실패")
		}
		var x workRelayRec
		if json.Unmarshal(b, &x) != nil || x.Rec.ID != v[1] || !workNameRe.MatchString(x.Rec.File) ||
			(x.Rec.Parent != "" && !workNameRe.MatchString(x.Rec.Parent)) {
			return fail("기록 형식 오류")
		}
		gz, err := os.ReadFile(outP)
		if err != nil {
			gz = nil
		}
		if err := st.AddRun(x.Rec, x.Hosts, gz); err != nil {
			return fail("%v", err)
		}
		os.Remove(rp)
		os.Remove(outP)
		logf("gossh 작업 %s 기록 수신 (os6_mgmt 중계): user=%s %s %d대 — %s", x.Rec.ID, v[0], x.Rec.File, x.Rec.N, workResText(&x.Rec))
		return 0
	case "memo":
		if !need(3) || !workNameRe.MatchString(v[1]) {
			return fail("인자 오류")
		}
		if err := st.SetMemo(v[1], v[2]); err != nil {
			return fail("%v", err)
		}
		return 0
	case "out":
		if !need(2) {
			return fail("인자 수")
		}
		b, err := st.Output(v[1])
		if err != nil {
			return fail("출력 없음")
		}
		os.Stdout.Write(b)
		return 0
	}
	return fail("알 수 없는 명령 %q", op)
}

// ---------------------------------------------------------------- 실행 한 건

// workRunReq: 실행 하나 (화면에서 확정한 것)
type workRunReq struct {
	User   string
	File   string // /tmp 아래 이름 — 이미 만들어져 있어야 함 (workPrepareTmp)
	Parent string
	Hosts  []string
	Cmd    string
	Danger bool
	Plan   workPlanT
	Client bool // os6_mgmt 원격 클라이언트 (기록은 os8 로 중계, 출력 상한 5MB)
}

// workTTY: 이 프로세스의 터미널 (pts/3), 모르면 ""
func workTTY() string {
	p, err := os.Readlink("/proc/self/fd/0")
	if err != nil || !strings.HasPrefix(p, "/dev/") {
		return ""
	}
	return strings.TrimPrefix(p, "/dev/")
}

// workRunOnce: argv 를 script 로 실행하고 (머리·꼬리를 뗀 로그, 종료코드) 를 돌려준다
func workRunOnce(argv []string) ([]byte, int, error) {
	f, err := os.CreateTemp("", "as_work_*.log") // 0600
	if err != nil {
		return nil, -1, err
	}
	p := f.Name()
	f.Close()
	defer os.Remove(p)
	rc, err := workScript(argv, p)
	b, _ := os.ReadFile(p)
	return stripScriptLog(b), rc, err
}

// workExecute: 터미널에서 실행(TUI 밖, out = 터미널) → 판정 → (경로 규칙에 따라) 접속불가만 os6 재실행 → 기록.
// 돌려주는 msg 는 TUI 로 돌아갔을 때 안내 줄에 보일 한 줄.
func workExecute(req workRunReq, store workStore, fallback workStore, out io.Writer, now func() time.Time) (workRec, string, bool) {
	start := now()
	path := filepath.Join(workTmpDir, req.File)
	rec := workRec{T: "run", ID: workNewID(start), File: req.File, Parent: req.Parent, At: start.Unix(), TTY: workTTY(),
		Side: req.Plan.Side, Cmd: req.Cmd, N: len(req.Hosts)}
	rec.Server, _ = os.Hostname()
	rec.Cwd, _ = os.Getwd()

	var argv []string
	if req.Plan.Remote {
		argv = workOS6Argv(req.Hosts, req.Cmd, req.Danger)
		fmt.Fprintf(out, "[auto_setup] gossh 작업 %s (%s대) — os6_mgmt(%s) 경유로 전량 실행: %s -w <원격 임시 파일> %s\n",
			path, commaN(len(req.Hosts)), os6_mgmt, os6_gossh, shq(req.Cmd))
	} else {
		argv = workLocalArgv(workGosshBin(req.Client), path, req.Cmd, req.Danger)
		fmt.Fprintf(out, "[auto_setup] gossh 작업 %s (%s대) — %s: %s -w %s %s\n", req.File, commaN(len(req.Hosts)), req.Plan.Side,
			argv[0], path, shq(req.Cmd))
	}
	log1, rc, err := workRunOnce(argv)
	if err != nil {
		fmt.Fprintf(out, "[X] 실행 실패: %v\n", err)
		return rec, "[X] 실행 실패: " + err.Error(), false
	}
	rec.RC = rc
	var files map[string][]string
	if !req.Plan.Remote {
		files = workResFiles(path)
	}
	res := judgeWork(string(log1), req.Hosts, files)
	rec.Res = &res
	all := log1
	if len(res.Unr) > 0 {
		switch {
		case req.Plan.Retry:
			fmt.Fprintf(out, "\n[auto_setup] 접속불가 %s대 → os6_mgmt 경유로 재실행\n", commaN(len(res.Unr)))
			log2, _, err2 := workRunOnce(workOS6Argv(res.Unr, req.Cmd, req.Danger))
			all = append(append(append([]byte{}, log1...), fmt.Sprintf("\n[auto_setup] 접속불가 %d대 → os6_mgmt 경유로 재실행\n", len(res.Unr))...), log2...)
			if err2 != nil {
				rec.O6Note = "os6 재실행 실패"
				fmt.Fprintf(out, "[X] os6_mgmt 경유 재실행 실패: %v\n", err2)
			} else {
				o6 := judgeWork(string(log2), res.Unr, nil)
				rec.O6 = &o6
			}
		case req.Plan.Note != "":
			rec.O6Note = req.Plan.Note
			fmt.Fprintf(out, "\n[auto_setup] 접속불가 %s대 — %s (os6_mgmt 경유 재실행 안 함)\n", commaN(len(res.Unr)), req.Plan.Note)
		}
	}
	var gz []byte
	if req.Client {
		gz = workCapGz(all, workRelayCap)
		fmt.Fprintf(out, "[auto_setup] os8_mgmt(%s) 로 기록 전송 중…\n", os8_mgmt)
	} else {
		gz = gzipBytes(all)
	}
	summary := fmt.Sprintf("%s: %s", req.File, workResText(&rec))
	msg := summary
	saved := true
	if err := store.AddRun(rec, req.Hosts, gz); err != nil {
		saved = false
		msg = "[X] 기록 저장 실패: " + err.Error()
		if fallback != nil {
			if ferr := fallback.AddRun(rec, req.Hosts, gz); ferr == nil {
				msg = "[X] os8_mgmt 로 기록 전송 실패 — 이 서버(os6_mgmt)의 " + filepath.Join(dataDir(), "work", req.User) + " 에 보관: " + err.Error()
			}
		}
	}
	logf("gossh 작업 %s: user=%s %s %s %d대 rc=%d — %s", rec.ID, req.User, req.File, rec.Side, rec.N, rec.RC, workResText(&rec))
	fmt.Fprintf(out, "\n[auto_setup] 결과 %s\n", summary)
	if !saved {
		fmt.Fprintln(out, msg)
	}
	return rec, msg, saved
}
