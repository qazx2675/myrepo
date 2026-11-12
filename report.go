package main

// -run 화면의 리포트(p 키): 벤더별 진행 보고 글을 만든다. 계획서_report.md 참고.
// 이 파일은 화면·키와 무관한 순수 로직(벤더 판정, 글 만들기, 스냅샷, 코멘트 해석)만 둔다.
// buildReport·parseComment 는 파일·시계·화면을 쓰지 않는다.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ---------------------------------------------------------------------------
// 벤더 판정
// ---------------------------------------------------------------------------

// vendorOf 는 호스트 이름이 맞는 첫 벤더 규칙의 이름을 돌려준다. 맞는 규칙이 없으면 '기타'.
// 패턴(parseConf 가 이미 소문자로 바꿈)과 호스트 이름을 모두 소문자로 바꿔 path.Match 로 비교한다.
func vendorOf(host string, rules []vendorRule) string {
	h := strings.ToLower(host)
	for _, r := range rules {
		for _, p := range r.Patterns {
			if ok, err := path.Match(strings.ToLower(p), h); err == nil && ok {
				return r.Name
			}
		}
	}
	return vendorOther
}

// vendorOrder 는 설정 파일 순서의 벤더 이름(buildReport 의 order 인자). '기타' 는 넣지 않는다.
func vendorOrder(rules []vendorRule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, r.Name)
	}
	return out
}

// ---------------------------------------------------------------------------
// 자료 구조
// ---------------------------------------------------------------------------

// repHost 는 리포트에 쓰는 호스트 한 대의 값. 스냅샷에도 그대로 저장한다.
type repHost struct {
	Host     string `json:"host"`
	Vendor   string `json:"vendor"` // 저장할 때의 벤더(참고용)
	Status   string `json:"status"`
	StressOK bool   `json:"stress_ok"` // stress-ng 열이 O
}

// repSnapshot 은 상태 폴더의 report_YYYYMMDD.json. 방식 1 의 기준이 된다.
type repSnapshot struct {
	Version int       `json:"version"` // 1
	Date    string    `json:"date"`    // "2026-11-12"
	Made    time.Time `json:"made"`
	Hosts   []repHost `json:"hosts"` // 호스트파일 순서
}

const repSnapshotVersion = 1

// repBase 는 전일대비의 기준. 방식 1(저장본)·방식 2(코멘트)가 같은 구조를 만든다.
type repBase struct {
	Label   string                    // 화면용: "저장본 2026-11-11" / "코멘트"
	Vendors map[string]*repBaseVendor // 키 = 벤더 이름 소문자
}

type repBaseVendor struct {
	OSDone, LoadDone, Unreach *int     // nil = 전일 정보 없음
	Names                     []string // 접속불가 호스트 이름
	HasNames                  bool     // 이름 명단을 알고 있음
}

func intp(n int) *int { return &n }

// newRepHosts 는 state.json 의 호스트 행(runState.Hosts)을 리포트용 호스트로 바꾼다. 제외·포기 호스트도 넣는다.
func newRepHosts(rows []hostRow, rules []vendorRule) []repHost {
	out := make([]repHost, 0, len(rows))
	for _, r := range rows {
		out = append(out, repHost{Host: r.Host, Vendor: vendorOf(r.Host, rules), Status: r.Status, StressOK: r.Stress == "O"})
	}
	return out
}

// ---------------------------------------------------------------------------
// 리포트 글
// ---------------------------------------------------------------------------

// repCounts 는 벤더 하나의 대수(계획서 3.5).
// U 는 접속불가 대수이고 디스크미인식 호스트를 포함한다. Unreach 는 그 이름을 호스트파일 순서로 담고,
// UnreachNoDisk[i] 는 Unreach[i] 가 디스크미인식이면 참이다.
type repCounts struct {
	N, D, I, B, U, Stopped int // A 는 D 와 같다
	Unreach                []string
	UnreachNoDisk          []bool
}

// buildReport 는 벤더별 리포트 글을 만든다. 같은 입력이면 같은 글이 나온다.
//
// 접속불가서버 대수(U)에는 디스크미인식 호스트도 들어간다. 명단에서는 그 이름 뒤에 ` 디스크 미인식` 을 적는다.
// 벤더 하나라도 "접속불가 0대이고 조치완료 호스트가 있음" 이면 글이 두 본이다.
// a 는 그 벤더에 `접속불가서버 0대 (전일대비 -N대)` 줄과 조치완료 줄을 적은 본, b 는 그 벤더의 접속불가 블록(줄 3 이하)을 적지 않은 본이다.
// 그런 벤더가 없으면 글은 하나이고 b 는 빈 문자열이다.
// cur 의 Vendor 가 order 에도 '기타' 에도 없으면 '기타' 로 센다.
func buildReport(cur []repHost, order []string, base *repBase, today time.Time, etaDays int) (a, b string) {
	names := append(append([]string{}, order...), vendorOther)
	idx := map[string]int{}
	for i, n := range names {
		idx[strings.ToLower(n)] = i
	}
	counts := make([]repCounts, len(names))
	curNames := map[string]bool{}
	for _, h := range cur {
		curNames[strings.ToLower(h.Host)] = true
		i, ok := idx[strings.ToLower(h.Vendor)]
		if !ok {
			i = len(names) - 1
		}
		c := &counts[i]
		c.N++
		switch h.Status {
		case stDone:
			c.D++
			if h.StressOK {
				c.B++
			}
		case stInstall:
			c.I++
		case stUnreach:
			c.U++
			c.Unreach = append(c.Unreach, h.Host)
			c.UnreachNoDisk = append(c.UnreachNoDisk, false)
		case stNoDisk: // 완료가 아니다. 접속불가서버에 넣고 이름 뒤에 '디스크 미인식' 을 적는다
			c.U++
			c.Unreach = append(c.Unreach, h.Host)
			c.UnreachNoDisk = append(c.UnreachNoDisk, true)
		case stStopped:
			c.Stopped++
		}
	}
	eta := today.AddDate(0, 0, etaDays)
	etaText := fmt.Sprintf("%d/%d", int(eta.Month()), eta.Day())

	var blocksA, blocksB []string
	two := false
	for i, name := range names {
		c := counts[i]
		if c.N == 0 {
			continue
		}
		var bv *repBaseVendor
		if base != nil && base.Vendors != nil {
			bv = base.Vendors[strings.ToLower(name)]
		}
		if bv == nil {
			bv = &repBaseVendor{}
		}
		head := []string{name + ":"}

		// 줄 1
		l1 := fmt.Sprintf("    OS설치 %d대 중 %d대 완료, %d대 OS 설치 중", c.N, c.D, c.I)
		if c.Stopped > 0 {
			l1 += fmt.Sprintf(", %d대 설치정지", c.Stopped)
		}
		l1 += " (" + diffText(c.D, bv.OSDone, "전일대비 ") + ")"
		head = append(head, l1)

		// 줄 2: A = D. 완료는 A = N 이고 B = A 일 때만.
		tail := etaText + " 완료예정"
		if c.D == c.N && c.B == c.D {
			tail = "완료"
		}
		head = append(head, fmt.Sprintf("    부하테스트 %d대 중 %d대 완료, %s, %s", c.D, c.B, diffText(c.B, bv.LoadDone, "전일대비 "), tail))

		// 조치완료: 기준의 접속불가 이름 중 현재 목록에 있고 현재 접속불가가 아닌 것(기준의 이름 순서)
		var cleared []string
		if bv.HasNames {
			nowUnreach := map[string]bool{}
			for _, u := range c.Unreach {
				nowUnreach[strings.ToLower(u)] = true
			}
			seen := map[string]bool{}
			for _, n := range bv.Names {
				k := strings.ToLower(n)
				if seen[k] || !curNames[k] || nowUnreach[k] {
					continue
				}
				seen[k] = true
				cleared = append(cleared, n)
			}
		}
		// 접속불가 블록(줄 3 이하)
		var block []string
		if c.U > 0 || len(cleared) > 0 {
			block = append(block, "    접속불가서버 "+strconv.Itoa(c.U)+"대 ("+diffText(c.U, bv.Unreach, "전일대비 ")+")")
			for _, n := range cleared {
				block = append(block, "        - "+n+" 조치완료")
			}
			if c.U > 0 {
				block = append(block, "        접속불가")
				for i, u := range c.Unreach {
					line := "          - " + u
					if c.UnreachNoDisk[i] {
						line += " 디스크 미인식"
					}
					block = append(block, line)
				}
			}
		}
		blocksA = append(blocksA, strings.Join(append(append([]string{}, head...), block...), "\n"))
		if c.U == 0 && len(cleared) > 0 {
			two = true
			blocksB = append(blocksB, strings.Join(head, "\n"))
		} else {
			blocksB = append(blocksB, strings.Join(append(append([]string{}, head...), block...), "\n"))
		}
	}
	if len(blocksA) == 0 {
		return "", ""
	}
	a = strings.Join(blocksA, "\n\n") + "\n"
	if two {
		b = strings.Join(blocksB, "\n\n") + "\n"
	}
	return a, b
}

// diffText 는 전일대비 글자. 기준이 nil 이면 '전일 정보 없음'. 늘 부호를 붙인다(+5대, -2대, +0대).
// label 은 수 앞에 붙는 글("전일대비 " 또는 "").  줄 1·3 은 괄호 안에 label 이 들어가므로 nil 일 때는 label 없이 문구만 쓴다.
func diffText(now int, before *int, label string) string {
	if before == nil {
		return "전일 정보 없음"
	}
	d := now - *before
	sign := "+"
	if d < 0 {
		sign = "-"
		d = -d
	}
	return label + sign + strconv.Itoa(d) + "대"
}

// ---------------------------------------------------------------------------
// 스냅샷
// ---------------------------------------------------------------------------

var (
	errNoSnapshot    = errors.New("전일 저장본이 없습니다. 2(코멘트 입력)를 쓰십시오.")
	errSnapshotNewer = errors.New("이 바이너리보다 새 형식의 저장본입니다")
	snapNameRe       = regexp.MustCompile(`^report_(\d{8})\.json$`)
)

// snapshotFileName 은 report_YYYYMMDD.json. 날짜는 t 의 지역 날짜다.
func snapshotFileName(t time.Time) string { return "report_" + t.Format("20060102") + ".json" }

// reportTextFileName 은 report_YYYYMMDD.txt(본 A) 또는 report_YYYYMMDD_B.txt(본 B).
func reportTextFileName(t time.Time, versionB bool) string {
	if versionB {
		return "report_" + t.Format("20060102") + "_B.txt"
	}
	return "report_" + t.Format("20060102") + ".txt"
}

// makeSnapshot 은 현재 호스트 목록으로 오늘 스냅샷을 만든다.
func makeSnapshot(cur []repHost, now time.Time) repSnapshot {
	return repSnapshot{Version: repSnapshotVersion, Date: now.Format("2006-01-02"), Made: now, Hosts: append([]repHost{}, cur...)}
}

// writeSnapshot 은 dir 에 report_YYYYMMDD.json 을 쓴다(같은 날 파일은 덮어쓴다). 쓴 경로를 돌려준다.
func writeSnapshot(dir string, snap repSnapshot, now time.Time) (string, error) {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, snapshotFileName(now))
	return p, writeFileAtomic(p, append(data, '\n'))
}

// readSnapshot 은 스냅샷 파일을 읽는다. version 이 1 보다 크면 errSnapshotNewer.
func readSnapshot(p string) (*repSnapshot, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var s repSnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Version > repSnapshotVersion {
		return nil, errSnapshotNewer
	}
	if s.Version < 1 {
		return nil, errors.New("version 이 없거나 올바르지 않습니다")
	}
	return &s, nil
}

// pickSnapshot 은 dir 에서 날짜가 today(지역 날짜)보다 앞인 report_YYYYMMDD.json 중 가장 최근 파일을 고른다.
// 날짜는 파일 이름에서 읽는다. 없으면 errNoSnapshot. 오늘·미래 날짜 파일은 쓰지 않는다.
func pickSnapshot(dir string, today time.Time) (p string, date time.Time, err error) {
	ents, e := os.ReadDir(dir)
	if e != nil && !os.IsNotExist(e) {
		return "", time.Time{}, e
	}
	best := ""
	for _, en := range ents {
		m := snapNameRe.FindStringSubmatch(en.Name())
		if m == nil || en.IsDir() {
			continue
		}
		d, perr := time.ParseInLocation("20060102", m[1], today.Location())
		if perr != nil || m[1] >= today.Format("20060102") {
			continue
		}
		if best == "" || m[1] > best {
			best, date = m[1], d
		}
	}
	if best == "" {
		return "", time.Time{}, errNoSnapshot
	}
	return filepath.Join(dir, "report_"+best+".json"), date, nil
}

// diffHostLists 는 두 호스트 목록을 이름(소문자) 집합으로 비교한다. 순서는 보지 않는다.
func diffHostLists(snap, cur []repHost) (onlySnap, onlyCur int) {
	s, c := map[string]bool{}, map[string]bool{}
	for _, h := range snap {
		s[strings.ToLower(h.Host)] = true
	}
	for _, h := range cur {
		c[strings.ToLower(h.Host)] = true
	}
	for k := range s {
		if !c[k] {
			onlySnap++
		}
	}
	for k := range c {
		if !s[k] {
			onlyCur++
		}
	}
	return
}

// baseFromSnapshot 은 저장본에서 기준을 만든다. 호스트 목록이 현재와 다르면 오류(대수 차이 문구)다.
// 벤더는 저장된 vendor 가 아니라 지금 규칙(rules)으로 다시 나눈다.
func baseFromSnapshot(snap *repSnapshot, cur []repHost, rules []vendorRule) (*repBase, error) {
	if a, b := diffHostLists(snap.Hosts, cur); a != 0 || b != 0 {
		return nil, fmt.Errorf("호스트 목록이 다릅니다. 저장본 %s %d대, 현재 %d대. 저장본에만 있는 호스트 %d대, 현재에만 있는 호스트 %d대. 2(코멘트 입력)를 쓰십시오.",
			snap.Date, len(snap.Hosts), len(cur), a, b)
	}
	rb := &repBase{Label: "저장본 " + snap.Date, Vendors: map[string]*repBaseVendor{}}
	osd, load, unr := map[string]int{}, map[string]int{}, map[string]int{}
	for _, h := range snap.Hosts {
		k := strings.ToLower(vendorOf(h.Host, rules))
		v := rb.Vendors[k]
		if v == nil {
			v = &repBaseVendor{HasNames: true}
			rb.Vendors[k] = v
		}
		switch h.Status {
		case stDone:
			osd[k]++
			if h.StressOK {
				load[k]++
			}
		case stUnreach, stNoDisk: // 디스크미인식도 접속불가서버로 센다(오늘의 규칙과 같다)
			unr[k]++
			v.Names = append(v.Names, h.Host)
		}
	}
	for k, v := range rb.Vendors {
		v.OSDone, v.LoadDone, v.Unreach = intp(osd[k]), intp(load[k]), intp(unr[k])
	}
	return rb, nil
}

// loadBaseline 은 방식 1 전체: 상태 폴더에서 전일 저장본을 골라 읽고 기준을 만든다.
// daysAgo 는 저장본 날짜가 오늘보다 며칠 앞인지(1 이면 어제). 오류 문구는 그대로 화면에 보일 수 있다.
func loadBaseline(dir string, cur []repHost, rules []vendorRule, today time.Time) (rb *repBase, daysAgo int, err error) {
	p, date, err := pickSnapshot(dir, today)
	if err != nil {
		return nil, 0, err
	}
	snap, err := readSnapshot(p)
	if err != nil {
		if errors.Is(err, errSnapshotNewer) {
			return nil, 0, err
		}
		return nil, 0, fmt.Errorf("저장본 %s 을(를) 읽을 수 없습니다: %v. 2(코멘트 입력)를 쓰십시오.", filepath.Base(p), err)
	}
	rb, err = baseFromSnapshot(snap, cur, rules)
	if err != nil {
		return nil, 0, err
	}
	t0 := time.Date(today.Year(), today.Month(), today.Day(), 12, 0, 0, 0, time.UTC)
	d0 := time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, time.UTC)
	return rb, int(t0.Sub(d0).Hours()/24 + 0.5), nil
}

// ---------------------------------------------------------------------------
// 코멘트 해석(방식 2)
// ---------------------------------------------------------------------------

const (
	commentMaxLines = 2000
	commentMaxBytes = 256 * 1024
)

// checkCommentSize 는 코멘트가 2000줄·256KB 를 넘으면 오류를 돌려준다. parseComment 자체는 길이로 실패하지 않는다.
func checkCommentSize(text string) error {
	if len(text) > commentMaxBytes {
		return fmt.Errorf("코멘트가 너무 깁니다(256KB 까지)")
	}
	if strings.Count(text, "\n")+1 > commentMaxLines {
		return fmt.Errorf("코멘트가 너무 깁니다(2000줄까지)")
	}
	return nil
}

var (
	reCountDone    = regexp.MustCompile(`(\d+)대완료`)
	reCount        = regexp.MustCompile(`(\d+)대`)
	reHostToken    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	reParen        = regexp.MustCompile(`\([^)]*\)?`)
	reNameSplit    = regexp.MustCompile(`[,\s]+`)
	commentBullets = "-•·*"
)

// normalizeCommentText 는 전각 숫자·영문·기호를 반각으로, 탭·전각 공백을 공백으로 바꾸고 \r 을 뗀다.
func normalizeCommentText(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '\r':
			continue
		case r == '\t' || r == '　' || r == ' ':
			sb.WriteRune(' ')
		case r >= 0xFF01 && r <= 0xFF5E:
			sb.WriteRune(r - 0xFEE0)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func compactSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// cutBefore 는 s 에서 sep 이 처음 나오는 곳 앞까지만 돌려준다(없으면 s).
func cutBefore(s, sep string) string {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i]
	}
	return s
}

type commentBlock struct {
	name             string // 설정의 벤더 이름(known 에 맞췄으면 그 이름, 아니면 코멘트의 글자)
	known            bool
	os, load, number *int
	names            []string
}

// parseComment 는 전날 리포트 글(사람이 고쳤을 수 있다)을 느슨하게 읽어 기준을 만든다.
// known 은 설정의 벤더 이름과 '기타'. 둘째 값은 화면에 보일 알림이다.
// 어떤 글에도 오류로 끝나지 않으며 panic 하지 않는다. 읽을 수 없는 줄은 버린다.
func parseComment(text string, known []string) (*repBase, []string) {
	rb := &repBase{Label: "코멘트", Vendors: map[string]*repBaseVendor{}}
	var notes []string
	canon := map[string]string{} // 공백 없앤 소문자 → 설정의 이름
	for _, k := range known {
		canon[strings.ToLower(compactSpaces(k))] = k
	}
	var blocks []*commentBlock // 첫 등장 순서
	byKey := map[string]*commentBlock{}
	var cur *commentBlock
	roster := false
	unknownSeen := map[string]bool{}

	for _, line := range strings.Split(normalizeCommentText(text), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.ContainsRune(commentBullets, []rune(line)[0]) {
			line = strings.TrimSpace(string([]rune(line)[1:]))
		}
		if line == "" {
			continue
		}
		cp := compactSpaces(line)
		up := strings.ToUpper(cp)

		// 벤더 제목(known 의 이름과 같은 줄, ':' 는 있어도 없어도 된다)
		if cn, ok := canon[strings.ToLower(strings.TrimSuffix(cp, ":"))]; ok {
			cur = startBlock(&blocks, byKey, cn, true, &notes, unknownSeen)
			roster = false
			continue
		}
		switch {
		case strings.HasPrefix(up, "OS설치"):
			if cur != nil {
				if m := reCountDone.FindStringSubmatch(cutBefore(cp, "전일대비")); m != nil {
					cur.os = atoiSafe(m[1])
				}
			}
			continue
		case strings.HasPrefix(cp, "부하테스트"):
			if cur != nil {
				if m := reCountDone.FindStringSubmatch(cutBefore(cp, "전일대비")); m != nil {
					cur.load = atoiSafe(m[1])
				}
			}
			continue
		case strings.HasPrefix(cp, "접속불가서버"):
			if cur != nil {
				if m := reCount.FindStringSubmatch(cutBefore(cp, "전일대비")); m != nil {
					cur.number = atoiSafe(m[1])
				}
				roster = true
			}
			continue
		case strings.Contains(cp, "조치완료"):
			continue
		case cp == "접속불가" || cp == "접속불가:":
			if cur != nil {
				roster = true
			}
			continue
		}
		// 벤더 제목: ':' 로 끝나고 숫자 붙은 '대' 가 없다
		if strings.HasSuffix(cp, ":") && len(cp) > 1 && !reCount.MatchString(cp) {
			name := strings.TrimSpace(strings.TrimSuffix(line, ":"))
			name = strings.TrimSpace(strings.TrimSuffix(name, ":"))
			if name != "" {
				cn, ok := canon[strings.ToLower(strings.TrimSuffix(cp, ":"))]
				if !ok {
					cn = name
				}
				cur = startBlock(&blocks, byKey, cn, ok, &notes, unknownSeen)
				roster = false
				continue
			}
		}
		// 명단 줄
		if cur != nil && roster {
			cur.names = append(cur.names, rosterNames(line)...)
		}
	}

	for _, b := range blocks {
		if !b.known {
			continue
		}
		v := &repBaseVendor{OSDone: b.os, LoadDone: b.load, Names: b.names}
		switch {
		case b.number != nil:
			v.Unreach = b.number
			v.HasNames = len(b.names) > 0 || *b.number == 0
			if len(b.names) > 0 && len(b.names) != *b.number {
				notes = append(notes, fmt.Sprintf("%s: 접속불가 숫자 %d대와 이름 %d개가 다릅니다", b.name, *b.number, len(b.names)))
			}
		default:
			v.Unreach = intp(len(b.names))
			v.HasNames = true
		}
		if len(v.Names) == 0 {
			v.Names = nil
		}
		rb.Vendors[strings.ToLower(b.name)] = v
	}
	return rb, notes
}

// startBlock 은 벤더 제목을 만났을 때 새 블록을 연다. 같은 벤더의 앞 블록이 있으면 덮고 알린다.
func startBlock(blocks *[]*commentBlock, byKey map[string]*commentBlock, name string, known bool, notes *[]string, unknownSeen map[string]bool) *commentBlock {
	k := strings.ToLower(name)
	if known {
		if _, dup := byKey[k]; dup {
			*notes = append(*notes, fmt.Sprintf("%s: 블록이 두 번 나와 뒤의 블록 값을 씁니다", name))
		}
	} else if !unknownSeen[k] {
		unknownSeen[k] = true
		*notes = append(*notes, "설정에 없는 벤더: "+name)
	}
	nb := &commentBlock{name: name, known: known}
	if old, dup := byKey[k]; dup && known {
		for i, b := range *blocks {
			if b == old {
				(*blocks)[i] = nb
			}
		}
	} else {
		*blocks = append(*blocks, nb)
	}
	byKey[k] = nb
	return nb
}

// rosterNames 는 명단 줄에서 접속불가 이름을 뽑는다. 줄의 첫 토막이 호스트 이름 모양이어야 명단 줄로 본다.
// 괄호 안 글자는 버린다. 줄 끝의 `디스크 미인식` 도 이름 모양이 아니라서 버린다. 쉼표·공백으로 나눈 토막 중 [A-Za-z0-9._-]+ 전체와 맞는 것이 이름이다.
func rosterNames(line string) []string {
	toks := reNameSplit.Split(strings.TrimSpace(reParen.ReplaceAllString(line, " ")), -1)
	if len(toks) == 0 || !reHostToken.MatchString(toks[0]) {
		return nil
	}
	var out []string
	for _, t := range toks {
		if reHostToken.MatchString(t) {
			out = append(out, t)
		}
	}
	return out
}

// atoiSafe 는 숫자 문자열(\d+)을 *int 로. 너무 커서 못 읽으면 nil.
func atoiSafe(s string) *int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

// unknownCommentNames 는 기준의 접속불가 이름 중 현재 호스트 목록에 없는 것의 수(확인 화면의 `현재 목록에 없는 이름 <n>개`).
func unknownCommentNames(base *repBase, cur []repHost) int {
	if base == nil {
		return 0
	}
	have := map[string]bool{}
	for _, h := range cur {
		have[strings.ToLower(h.Host)] = true
	}
	seen := map[string]bool{}
	n := 0
	keys := make([]string, 0, len(base.Vendors))
	for k := range base.Vendors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, nm := range base.Vendors[k].Names {
			l := strings.ToLower(nm)
			if !have[l] && !seen[l] {
				seen[l] = true
				n++
			}
		}
	}
	return n
}
